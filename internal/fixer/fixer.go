package fixer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func Process(
	ctx context.Context,
	sourcePath string,
	outputPath string,
	progressCh chan<- Progress,
	options ProcessOptions,
) error {
	options = options.Normalized()

	if err := InitializeFileLogger(); err != nil {
		if LogHandler != nil {
			LogHandler(LoggerWarn, fmt.Sprintf("Failed to initialize file logger: %v", err))
		}
	} else {
		defer func() {
			if err := CloseFileLogger(); err != nil && LogHandler != nil {
				LogHandler(LoggerWarn, fmt.Sprintf("Failed to close file logger: %v", err))
			}
		}()
	}

	startTime := time.Now()
	defer func() {
		Log(LoggerInfo, "Total processing time: %s", time.Since(startTime).Round(time.Second))
		ClearCache()
	}()

	defer close(progressCh)

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := EnsureDir(outputPath); err != nil {
		return err
	}

	if options.WriteMetadata || options.VerifyWrites || options.RestoreMOVExtension {
		if err := InitializeExifTool(); err != nil {
			return err
		}
		defer CloseExifTool()
	}

	plans, err := DiscoverMediaPlan(sourcePath, options)
	if err != nil {
		return err
	}
	if len(plans) == 0 {
		return fmt.Errorf("no media files found in %s", sourcePath)
	}

	stateDir := filepath.Join(outputPath, ".gtf")
	if err := EnsureDir(stateDir); err != nil {
		return err
	}

	stateStore, err := OpenStateStore(filepath.Join(stateDir, "state.jsonl"))
	if err != nil {
		return err
	}
	defer stateStore.Close()

	report := NewRunReport(sourcePath, outputPath, options)
	defer func() {
		if err := report.Write(stateDir); err != nil {
			Log(LoggerError, "Failed to write audit report: %v", err)
		} else {
			Log(LoggerInfo, "Audit report written to %s", filepath.Join(stateDir, "reports", "latest.txt"))
		}
	}()

	progress := Progress{Total: len(plans)}
	progressCh <- progress

	for _, plan := range plans {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		record := processPlan(outputPath, plan, options, stateStore)
		if err := stateStore.Put(record); err != nil {
			Log(LoggerError, "Failed to persist state for %s: %v", plan.RelativePath, err)
		}
		report.Add(record)

		progress.Processed++
		progress.Current = plan.SourcePath
		progressCh <- progress
	}

	return nil
}

func processPlan(
	outputRoot string,
	plan MediaPlan,
	options ProcessOptions,
	stateStore *StateStore,
) ProcessRecord {
	record := ProcessRecord{
		SourcePath:         plan.SourcePath,
		SourceRelPath:      plan.RelativePath,
		SidecarPath:        plan.SidecarPath,
		MatchStatus:        plan.MatchStatus,
		MatchStrategy:      plan.MatchStrategy,
		MatchCandidates:    plan.MatchCandidates,
		UsedPartnerSidecar: plan.PartnerPath != "",
		UpdatedAt:          time.Now().UTC(),
	}

	outputDir, err := ResolveOutputDir(outputRoot, plan, options)
	if err != nil {
		record.Status = OperationError
		record.Error = err.Error()
		return record
	}

	outputName := resolveOutputName(plan, options)
	destPath := filepath.Join(outputDir, outputName)

	if previous, ok := stateStore.Get(plan.RelativePath); ok && previous.Successful() &&
		strings.EqualFold(filepath.Clean(previous.OutputPath), filepath.Clean(destPath)) &&
		previous.OutputPath != "" && FileExists(previous.OutputPath) {
		previous.Status = OperationSkippedResume
		previous.UpdatedAt = time.Now().UTC()
		return previous
	}

	sourceInfo, err := os.Stat(plan.SourcePath)
	if err != nil {
		record.Status = OperationError
		record.Error = err.Error()
		return record
	}
	record.SourceSize = sourceInfo.Size()

	sourceHash, err := HashFile(plan.SourcePath)
	if err != nil {
		record.Status = OperationError
		record.Error = err.Error()
		return record
	}
	record.SourceHash = sourceHash

	if existingHash, samePath := existingFileHash(destPath); samePath {
		if existingHash == sourceHash {
			record.OutputPath = destPath
			record.Status = OperationSkippedExisting
			return record
		}
		destPath = MakeUniquePath(destPath)
	}

	if options.Deduplicate {
		if canonical, ok := stateStore.CanonicalByHash(sourceHash); ok &&
			canonical.OutputPath != "" &&
			!strings.EqualFold(filepath.Clean(canonical.OutputPath), filepath.Clean(destPath)) &&
			FileExists(canonical.OutputPath) {
			record.OutputPath = destPath
			record.DuplicateOf = canonical.OutputPath

			if options.DryRun {
				record.Status = OperationDryRun
				return record
			}

			if err := EnsureDir(filepath.Dir(destPath)); err != nil {
				record.Status = OperationError
				record.Error = err.Error()
				return record
			}

			status, err := LinkDuplicate(canonical.OutputPath, destPath, options.UseSymlinks)
			record.Status = status
			if err != nil {
				record.Status = OperationError
				record.Error = err.Error()
			}
			return record
		}
	}

	record.OutputPath = destPath
	if options.DryRun {
		record.Status = OperationDryRun
		return record
	}

	if err := EnsureDir(filepath.Dir(destPath)); err != nil {
		record.Status = OperationError
		record.Error = err.Error()
		return record
	}

	if err := DuplicateFile(plan.SourcePath, destPath); err != nil {
		record.Status = OperationError
		record.Error = err.Error()
		return record
	}

	if options.WriteMetadata && plan.MatchStatus == MatchStatusMatched && plan.Metadata != nil {
		metadataResult, err := ApplyMetadata(destPath, *plan.Metadata, options.ConflictPolicy)
		record.Conflicts = metadataResult.Conflicts
		record.MetadataWritten = metadataResult.MetadataWritten
		record.UsedXMPSidecar = metadataResult.UsedXMPSidecar

		if err != nil {
			record.Error = joinProblem(record.Error, fmt.Sprintf("metadata write failed: %v", err))
		}

		if options.VerifyWrites && metadataResult.MetadataWritten {
			if err := VerifyMetadata(destPath, metadataResult.MetadataPlan); err != nil {
				record.Error = joinProblem(record.Error, fmt.Sprintf("verification failed: %v", err))
			} else {
				record.MetadataVerified = true
			}
		}
	}

	switch {
	case record.MetadataWritten:
		record.Status = OperationCopiedWithMetadata
	case options.WriteMetadata:
		record.Status = OperationCopiedWithoutMeta
	default:
		record.Status = OperationCopied
	}

	return record
}

func existingFileHash(path string) (string, bool) {
	if !FileExists(path) {
		return "", false
	}
	hash, err := HashFile(path)
	if err != nil {
		return "", true
	}
	return hash, true
}

func resolveOutputName(plan MediaPlan, options ProcessOptions) string {
	fileName := plan.OutputName
	if !options.RestoreMOVExtension || !strings.EqualFold(filepath.Ext(fileName), ".mp4") {
		return fileName
	}

	majorBrand, err := GetMajorBrand(plan.SourcePath)
	if err != nil {
		return fileName
	}
	if strings.HasPrefix(majorBrand, "Apple QuickTime") {
		ext := filepath.Ext(fileName)
		if ext == ".MP4" {
			return strings.TrimSuffix(fileName, ext) + ".MOV"
		}
		return strings.TrimSuffix(fileName, ext) + ".mov"
	}
	return fileName
}

func joinProblem(current string, next string) string {
	if current == "" {
		return next
	}
	return current + "; " + next
}
