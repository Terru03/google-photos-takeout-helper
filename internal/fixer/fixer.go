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

	if err := ValidateProcessPaths(sourcePath, outputPath); err != nil {
		return err
	}

	finalOutputRoot := outputPath
	if strings.TrimSpace(options.FinalOutputRoot) != "" {
		finalOutputRoot = options.FinalOutputRoot
	}
	runtimeRoot := finalOutputRoot
	if strings.TrimSpace(options.RuntimeRoot) != "" {
		runtimeRoot = options.RuntimeRoot
	}

	runtimePaths, err := ResolveRuntimePaths(runtimeRoot)
	if err != nil {
		return err
	}

	if logFilePath, err := InitializeFileLogger(runtimePaths.LogDir); err != nil {
		if handler := getLogHandler(); handler != nil {
			handler(LoggerWarn, fmt.Sprintf("Failed to initialize file logger: %v", err))
		}
	} else {
		Log(LoggerInfo, "File log: %s", logFilePath)
		defer func() {
			if err := CloseFileLogger(); err != nil && getLogHandler() != nil {
				getLogHandler()(LoggerWarn, fmt.Sprintf("Failed to close file logger: %v", err))
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

	exifInfo, err := ValidateProcessingDependencies(options)
	if err != nil {
		return err
	}
	if exifInfo != nil {
		Log(LoggerInfo, "Using ExifTool %s from %s", exifInfo.Version, exifInfo.Path)
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

	if err := EnsureDir(runtimePaths.StateDir); err != nil {
		return err
	}

	stateStore, err := OpenStateStore(filepath.Join(runtimePaths.StateDir, "state.jsonl"))
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := stateStore.Close(); closeErr != nil {
			Log(LoggerWarn, "Failed to close state store: %v", closeErr)
		}
	}()
	for _, warning := range stateStore.Warnings {
		Log(LoggerWarn, "%s", warning)
	}

	report := NewRunReport(sourcePath, finalOutputRoot, options)
	if sidecarCount, err := CountJSONSidecars(sourcePath); err != nil {
		Log(LoggerWarn, "Failed to count JSON sidecars: %v", err)
	} else {
		report.SetJSONSidecarsFound(sidecarCount)
	}
	defer func() {
		if err := report.Write(runtimePaths.StateDir); err != nil {
			Log(LoggerError, "Failed to write audit report: %v", err)
		} else {
			Log(LoggerInfo, "Audit report written to %s", filepath.Join(runtimePaths.StateDir, "reports", "latest.txt"))
		}
	}()

	progress := Progress{Total: len(plans)}
	progressCh <- progress

	for _, plan := range plans {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		record, suspiciousDates := processPlanToRoots(outputPath, finalOutputRoot, plan, options, stateStore)
		if err := stateStore.Put(record); err != nil {
			Log(LoggerError, "Failed to persist state for %s: %v", plan.RelativePath, err)
		}
		report.Add(record)
		report.AddSuspiciousDates(suspiciousDates)

		progress.Processed++
		progress.Current = plan.SourcePath
		progressCh <- progress
	}

	if err := stateStore.Flush(); err != nil {
		return fmt.Errorf("flush processing state: %w", err)
	}

	sourceCleanupResult := CleanupSourceRootAfterSuccess(ctx, sourcePath, outputPath, report, options)
	if sourceCleanupResult.Enabled {
		report.SetSourceCleanup(sourceCleanupResult)
		switch sourceCleanupResult.Status {
		case SourceCleanupStatusDeleted:
			Log(LoggerInfo, "Deleted input folder after clean run: %s", sourceCleanupResult.Path)
		case SourceCleanupStatusFailed:
			Log(LoggerError, "Input cleanup failed: %s", sourceCleanupResult.Error)
		default:
			Log(LoggerInfo, "Kept input folder: %s", sourceCleanupResult.Reason)
		}
	}

	return nil
}

func processPlan(
	outputRoot string,
	plan MediaPlan,
	options ProcessOptions,
	stateStore *StateStore,
) (ProcessRecord, []SuspiciousDateFinding) {
	return processPlanToRoots(outputRoot, outputRoot, plan, options, stateStore)
}

func processPlanToRoots(
	workingOutputRoot string,
	finalOutputRoot string,
	plan MediaPlan,
	options ProcessOptions,
	stateStore *StateStore,
) (ProcessRecord, []SuspiciousDateFinding) {
	record := ProcessRecord{
		SourceID:           options.SourceID,
		SourcePath:         plan.SourcePath,
		SourceRelPath:      plan.RelativePath,
		SidecarPath:        plan.SidecarPath,
		PartnerPath:        plan.PartnerPath,
		PartnerRelPath:     plan.PartnerRelPath,
		MatchStatus:        plan.MatchStatus,
		MatchStrategy:      plan.MatchStrategy,
		MatchCandidates:    plan.MatchCandidates,
		UsedPartnerSidecar: plan.MatchStrategy == MatchStrategyPartner && plan.SidecarPath != "" && plan.PartnerPath != "",
		UpdatedAt:          time.Now().UTC(),
	}

	finalOutputDir, dateSelection, err := resolveOutputDirWithDateSource(finalOutputRoot, plan, options)
	if err != nil {
		record.Status = OperationError
		record.Error = err.Error()
		return record, nil
	}
	if dateSelection.Source != "" {
		record.FolderDateSource = dateSelection.Source
		record.FolderYear = dateSelection.Time.UTC().Year()
		if dateSelection.MonthKnown {
			record.FolderMonth = int(dateSelection.Time.UTC().Month())
		}
	}
	if options.Verbose && dateSelection.Source != "" {
		Log(
			LoggerInfo,
			"Date source %s: %s %s -> %s",
			plan.RelativePath,
			dateSelection.Source,
			dateSelection.Time.UTC().Format(time.RFC3339),
			filepath.Base(finalOutputDir),
		)
	}

	outputName := resolveOutputName(plan, options)
	finalDestPath := filepath.Join(finalOutputDir, outputName)
	workingDestPath, err := mapOutputPath(finalOutputRoot, workingOutputRoot, finalDestPath)
	if err != nil {
		record.Status = OperationError
		record.Error = err.Error()
		return record, nil
	}

	if previous, ok := stateStore.GetForSource(options.SourceID, plan.RelativePath); ok && previous.Successful() &&
		strings.EqualFold(filepath.Clean(previous.OutputPath), filepath.Clean(finalDestPath)) &&
		previous.OutputPath != "" && FileExists(previous.OutputPath) {
		previous.Status = OperationSkippedResume
		previous.UpdatedAt = time.Now().UTC()
		return previous, detectSuspiciousDates(plan, previous.OutputPath)
	}

	sourceInfo, err := os.Stat(plan.SourcePath)
	if err != nil {
		record.Status = OperationError
		record.Error = err.Error()
		return record, nil
	}
	record.SourceSize = sourceInfo.Size()

	sourceHash, err := HashFile(plan.SourcePath)
	if err != nil {
		record.Status = OperationError
		record.Error = err.Error()
		return record, nil
	}
	record.SourceHash = sourceHash

	if existingHash, samePath := existingFileHash(finalDestPath); samePath {
		if existingHash == sourceHash {
			record.OutputPath = finalDestPath
			record.Status = OperationSkippedExisting
			return record, detectSuspiciousDates(plan, record.OutputPath)
		}
		finalDestPath = makeUniqueAcrossRoots(finalDestPath, finalOutputRoot, workingOutputRoot)
		workingDestPath, err = mapOutputPath(finalOutputRoot, workingOutputRoot, finalDestPath)
		if err != nil {
			record.Status = OperationError
			record.Error = err.Error()
			return record, nil
		}
	} else if FileExists(workingDestPath) {
		finalDestPath = makeUniqueAcrossRoots(finalDestPath, finalOutputRoot, workingOutputRoot)
		workingDestPath, err = mapOutputPath(finalOutputRoot, workingOutputRoot, finalDestPath)
		if err != nil {
			record.Status = OperationError
			record.Error = err.Error()
			return record, nil
		}
	}

	suspiciousDates := detectSuspiciousDates(plan, finalDestPath)

	if options.Deduplicate {
		if canonical, ok := stateStore.CanonicalByHash(sourceHash); ok &&
			canonical.OutputPath != "" &&
			!strings.EqualFold(filepath.Clean(canonical.OutputPath), filepath.Clean(finalDestPath)) &&
			(FileExists(canonical.OutputPath) || FileExists(canonical.StagedPath)) {
			record.OutputPath = finalDestPath
			record.DuplicateOf = canonical.OutputPath

			if options.DryRun {
				record.Status = OperationDryRun
				return record, suspiciousDates
			}

			if err := EnsureDir(filepath.Dir(workingDestPath)); err != nil {
				record.Status = OperationError
				record.Error = err.Error()
				return record, suspiciousDates
			}

			if sameOutputRoot(workingOutputRoot, finalOutputRoot) {
				status, linkErr := LinkDuplicate(canonical.OutputPath, workingDestPath, options.UseSymlinks)
				record.Status = status
				if linkErr != nil {
					record.Status = OperationError
					record.Error = linkErr.Error()
				}
			} else {
				record.Status = OperationHardlinked
			}
			if record.Status != OperationError && options.WriteXMPSidecars && plan.MatchStatus == MatchStatusMatched && plan.Metadata != nil {
				metadataResult, err := WriteMetadataXMPSidecar(workingDestPath, *plan.Metadata, options.ConflictPolicy)
				record.Conflicts = metadataResult.Conflicts
				record.MetadataWritten = metadataResult.MetadataWritten
				record.UsedXMPSidecar = metadataResult.UsedXMPSidecar
				if err != nil {
					record.Error = joinProblem(record.Error, fmt.Sprintf("XMP sidecar write failed: %v", err))
				}
			}
			return record, suspiciousDates
		}
	}

	record.OutputPath = finalDestPath
	if !sameOutputRoot(workingOutputRoot, finalOutputRoot) {
		record.StagedPath = workingDestPath
	}
	if options.DryRun {
		record.Status = OperationDryRun
		return record, suspiciousDates
	}

	if err := EnsureDir(filepath.Dir(workingDestPath)); err != nil {
		record.Status = OperationError
		record.Error = err.Error()
		return record, suspiciousDates
	}

	if err := DuplicateFile(plan.SourcePath, workingDestPath); err != nil {
		record.Status = OperationError
		record.Error = err.Error()
		return record, suspiciousDates
	}

	if (options.WriteMetadata || options.WriteXMPSidecars) && plan.MatchStatus == MatchStatusMatched && plan.Metadata != nil {
		if options.WriteMetadata {
			metadataResult, err := ApplyMetadata(workingDestPath, *plan.Metadata, options.ConflictPolicy)
			record.Conflicts = metadataResult.Conflicts
			record.MetadataWritten = metadataResult.MetadataWritten

			if err != nil {
				record.Error = joinProblem(record.Error, fmt.Sprintf("metadata write failed: %v", err))
				if restoreErr := DuplicateFile(plan.SourcePath, workingDestPath); restoreErr != nil {
					record.Status = OperationError
					record.Error = joinProblem(record.Error, fmt.Sprintf("restore source after metadata failure: %v", restoreErr))
					return record, suspiciousDates
				}
				fallbackResult, fallbackErr := WriteMetadataXMPSidecar(workingDestPath, *plan.Metadata, options.ConflictPolicy)
				if fallbackErr != nil {
					record.Error = joinProblem(record.Error, fmt.Sprintf("XMP metadata fallback failed: %v", fallbackErr))
				} else if fallbackResult.MetadataWritten {
					record.MetadataWritten = true
					record.UsedXMPSidecar = true
					if len(record.Conflicts) == 0 {
						record.Conflicts = fallbackResult.Conflicts
					}
				}
			}

			if options.VerifyWrites && metadataResult.MetadataWritten {
				if err := VerifyMetadata(workingDestPath, metadataResult.MetadataPlan); err != nil {
					record.Error = joinProblem(record.Error, fmt.Sprintf("verification failed: %v", err))
					if restoreErr := DuplicateFile(plan.SourcePath, workingDestPath); restoreErr != nil {
						record.Status = OperationError
						record.Error = joinProblem(record.Error, fmt.Sprintf("restore source after verification failure: %v", restoreErr))
						return record, suspiciousDates
					}
					record.MetadataWritten = false
				} else {
					record.MetadataVerified = true
				}
			}
		}

		if options.WriteXMPSidecars && !record.UsedXMPSidecar {
			metadataResult, err := WriteMetadataXMPSidecar(workingDestPath, *plan.Metadata, options.ConflictPolicy)
			if len(record.Conflicts) == 0 {
				record.Conflicts = metadataResult.Conflicts
			}
			record.MetadataWritten = record.MetadataWritten || metadataResult.MetadataWritten
			record.UsedXMPSidecar = metadataResult.UsedXMPSidecar
			if err != nil {
				record.Error = joinProblem(record.Error, fmt.Sprintf("XMP sidecar write failed: %v", err))
			}
		}
	}

	switch {
	case record.MetadataWritten:
		record.Status = OperationCopiedWithMetadata
	case options.WriteMetadata || options.WriteXMPSidecars:
		record.Status = OperationCopiedWithoutMeta
	default:
		record.Status = OperationCopied
	}

	return record, suspiciousDates
}

func sameOutputRoot(left string, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func mapOutputPath(finalRoot string, workingRoot string, finalPath string) (string, error) {
	if sameOutputRoot(finalRoot, workingRoot) {
		return finalPath, nil
	}
	rel, err := filepath.Rel(finalRoot, finalPath)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output path escapes final root: %s", finalPath)
	}
	return filepath.Join(workingRoot, rel), nil
}

func makeUniqueAcrossRoots(path string, finalRoot string, workingRoot string) string {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)
	for index := 2; ; index++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, index, ext))
		workingCandidate, err := mapOutputPath(finalRoot, workingRoot, candidate)
		if err != nil {
			continue
		}
		if !FileExists(candidate) && !FileExists(workingCandidate) {
			return candidate
		}
	}
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
	sourceExt := strings.ToLower(filepath.Ext(fileName))
	if _, isImage := imageExtensions[sourceExt]; isImage {
		if actualExt, ok := DetectActualImageExtension(plan.SourcePath); ok &&
			!equivalentImageExtensions(sourceExt, actualExt) {
			fileName = strings.TrimSuffix(fileName, filepath.Ext(fileName)) + actualExt
		}
	}
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

func equivalentImageExtensions(left string, right string) bool {
	left = strings.ToLower(strings.TrimSpace(left))
	right = strings.ToLower(strings.TrimSpace(right))
	if left == right {
		return true
	}
	return (left == ".jpg" || left == ".jpeg") && (right == ".jpg" || right == ".jpeg")
}

func joinProblem(current string, next string) string {
	if current == "" {
		return next
	}
	return current + "; " + next
}
