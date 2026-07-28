package fixer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SidecarBackfillRecord struct {
	SourceRelPath string      `json:"sourceRelPath"`
	OldOutputPath string      `json:"oldOutputPath"`
	NewOutputPath string      `json:"newOutputPath,omitempty"`
	SidecarPath   string      `json:"sidecarPath,omitempty"`
	OldStatus     MatchStatus `json:"oldStatus"`
	NewStatus     MatchStatus `json:"newStatus"`
	Updated       bool        `json:"updated"`
	Error         string      `json:"error,omitempty"`
}

type SidecarBackfillResult struct {
	StartedAt  time.Time               `json:"startedAt"`
	FinishedAt time.Time               `json:"finishedAt"`
	Checked    int                     `json:"checked"`
	Matched    int                     `json:"matched"`
	Updated    int                     `json:"updated"`
	Unmatched  int                     `json:"unmatched"`
	Ambiguous  int                     `json:"ambiguous"`
	Failed     int                     `json:"failed"`
	ReportPath string                  `json:"reportPath,omitempty"`
	Records    []SidecarBackfillRecord `json:"records,omitempty"`
}

func BackfillIndexedSidecars(
	ctx context.Context,
	outputRoot string,
	index *SidecarIndex,
	options ProcessOptions,
) (SidecarBackfillResult, error) {
	result := SidecarBackfillResult{StartedAt: time.Now().UTC()}
	if index == nil || index.Count() == 0 {
		result.FinishedAt = time.Now().UTC()
		return result, nil
	}
	options = options.Normalized()
	if options.DryRun {
		result.FinishedAt = time.Now().UTC()
		return result, nil
	}

	runtimePaths, err := ResolveRuntimePaths(outputRoot)
	if err != nil {
		return result, err
	}
	store, err := OpenStateStore(filepath.Join(runtimePaths.StateDir, "state.jsonl"))
	if err != nil {
		return result, err
	}
	defer func() {
		_ = store.Close()
	}()

	if options.WriteMetadata {
		if _, err := ValidateProcessingDependencies(options); err != nil {
			return result, err
		}
		if err := InitializeExifTool(); err != nil {
			return result, err
		}
		defer CloseExifTool()
	}

	for _, oldRecord := range store.Records() {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if oldRecord.MatchStatus == MatchStatusMatched ||
			!oldRecord.Successful() ||
			strings.TrimSpace(oldRecord.OutputPath) == "" ||
			!FileExists(oldRecord.OutputPath) {
			continue
		}

		result.Checked++
		plan := resolveIndexedRecordPlan(outputRoot, oldRecord.SourceRelPath, index)
		if plan.MatchStatus != MatchStatusMatched && oldRecord.PartnerRelPath != "" {
			partnerPlan := resolveIndexedRecordPlan(outputRoot, oldRecord.PartnerRelPath, index)
			if partnerPlan.MatchStatus == MatchStatusMatched {
				plan.SidecarPath = partnerPlan.SidecarPath
				plan.Metadata = partnerPlan.Metadata
				plan.MatchStatus = MatchStatusMatched
				plan.MatchStrategy = MatchStrategyPartner
				plan.MatchCandidates = partnerPlan.MatchCandidates
			}
		}

		audit := SidecarBackfillRecord{
			SourceRelPath: oldRecord.SourceRelPath,
			OldOutputPath: oldRecord.OutputPath,
			SidecarPath:   plan.SidecarPath,
			OldStatus:     oldRecord.MatchStatus,
			NewStatus:     plan.MatchStatus,
		}
		switch plan.MatchStatus {
		case MatchStatusUnmatched:
			result.Unmatched++
			result.Records = append(result.Records, audit)
			continue
		case MatchStatusAmbiguous:
			result.Ambiguous++
			result.Records = append(result.Records, audit)
			continue
		}

		result.Matched++
		updated, updateErr := backfillRecord(outputRoot, oldRecord, plan, options)
		if updateErr != nil {
			result.Failed++
			audit.Error = updateErr.Error()
			result.Records = append(result.Records, audit)
			continue
		}
		if err := store.Put(updated); err != nil {
			result.Failed++
			audit.Error = err.Error()
			result.Records = append(result.Records, audit)
			continue
		}
		result.Updated++
		audit.Updated = true
		audit.NewOutputPath = updated.OutputPath
		result.Records = append(result.Records, audit)
	}
	if err := store.Flush(); err != nil {
		return result, err
	}

	result.FinishedAt = time.Now().UTC()
	reportPath, err := writeSidecarBackfillReport(runtimePaths.StateDir, result)
	if err != nil {
		return result, err
	}
	result.ReportPath = reportPath
	return result, nil
}

func resolveIndexedRecordPlan(outputRoot string, relativePath string, index *SidecarIndex) MediaPlan {
	relativePath = filepath.Clean(relativePath)
	relativeDir := filepath.Dir(relativePath)
	if relativeDir == "." {
		relativeDir = ""
	}
	topLevelDir := relativePath
	if relativeDir != "" {
		topLevelDir = strings.Split(relativeDir, string(filepath.Separator))[0]
	}
	isYearFolder, _ := IsYearFolder(topLevelDir)
	fakeRoot := filepath.Join(outputRoot, ".gtf", "sidecar-match")
	fakePath := filepath.Join(fakeRoot, relativePath)
	bundle := &folderCatalog{
		relativeDir:  relativeDir,
		topLevelDir:  topLevelDir,
		isYearFolder: isYearFolder,
		sidecars:     index.candidatesForDir(relativeDir),
	}
	return resolveDirectMatch(fakeRoot, bundle, fakePath)
}

func backfillRecord(
	outputRoot string,
	record ProcessRecord,
	plan MediaPlan,
	options ProcessOptions,
) (ProcessRecord, error) {
	if plan.Metadata == nil {
		return record, fmt.Errorf("matched sidecar has no usable metadata")
	}
	currentPath := record.OutputPath
	desiredDir, dateSelection, err := resolveOutputDirWithDateSource(outputRoot, plan, options)
	if err != nil {
		return record, err
	}
	desiredPath := filepath.Join(desiredDir, filepath.Base(currentPath))
	if !strings.EqualFold(filepath.Clean(desiredPath), filepath.Clean(currentPath)) && FileExists(desiredPath) {
		desiredPath = MakeUniquePath(desiredPath)
	}
	if err := EnsureDir(filepath.Dir(desiredPath)); err != nil {
		return record, err
	}

	tempFile, err := os.CreateTemp(filepath.Dir(currentPath), ".gtf-backfill-*"+filepath.Ext(currentPath))
	if err != nil {
		return record, err
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return record, err
	}
	_ = os.Remove(tempPath)
	defer func() {
		_ = os.Remove(tempPath)
		_ = os.Remove(tempPath + ".xmp")
	}()
	if err := DuplicateFile(currentPath, tempPath); err != nil {
		return record, err
	}

	metadataResult := MetadataApplyResult{}
	if options.WriteMetadata {
		metadataResult, err = ApplyMetadata(tempPath, *plan.Metadata, options.ConflictPolicy)
		if err != nil {
			return record, fmt.Errorf("write metadata: %w", err)
		}
		if options.VerifyWrites && metadataResult.MetadataWritten {
			if err := VerifyMetadata(tempPath, metadataResult.MetadataPlan); err != nil {
				return record, fmt.Errorf("verify metadata: %w", err)
			}
		}
	}
	if options.WriteXMPSidecars {
		xmpResult, xmpErr := WriteMetadataXMPSidecar(tempPath, *plan.Metadata, options.ConflictPolicy)
		if xmpErr != nil {
			return record, fmt.Errorf("write XMP sidecar: %w", xmpErr)
		}
		metadataResult.MetadataWritten = metadataResult.MetadataWritten || xmpResult.MetadataWritten
		metadataResult.UsedXMPSidecar = xmpResult.UsedXMPSidecar
		if len(metadataResult.Conflicts) == 0 {
			metadataResult.Conflicts = xmpResult.Conflicts
		}
	}

	if strings.EqualFold(filepath.Clean(currentPath), filepath.Clean(desiredPath)) {
		if err := os.Rename(tempPath, currentPath); err != nil {
			return record, fmt.Errorf("replace output: %w", err)
		}
	} else {
		if err := os.Rename(tempPath, desiredPath); err != nil {
			return record, fmt.Errorf("move updated output: %w", err)
		}
		if err := os.Remove(currentPath); err != nil {
			_ = os.Remove(desiredPath)
			return record, fmt.Errorf("remove old output after move: %w", err)
		}
		moveOptionalSidecar(currentPath+".xmp", desiredPath+".xmp")
	}
	if FileExists(tempPath + ".xmp") {
		if err := os.Rename(tempPath+".xmp", desiredPath+".xmp"); err != nil {
			return record, fmt.Errorf("move new XMP sidecar: %w", err)
		}
	}

	record.SidecarPath = plan.SidecarPath
	record.MatchStatus = MatchStatusMatched
	record.MatchStrategy = plan.MatchStrategy
	record.MatchCandidates = plan.MatchCandidates
	record.UsedPartnerSidecar = plan.MatchStrategy == MatchStrategyPartner
	record.OutputPath = desiredPath
	record.StagedPath = ""
	record.MetadataWritten = metadataResult.MetadataWritten
	record.MetadataVerified = options.VerifyWrites && metadataResult.MetadataWritten && !metadataResult.UsedXMPSidecar
	record.UsedXMPSidecar = metadataResult.UsedXMPSidecar
	record.Conflicts = metadataResult.Conflicts
	record.Error = ""
	record.UpdatedAt = time.Now().UTC()
	if metadataResult.MetadataWritten {
		record.Status = OperationCopiedWithMetadata
	}
	if dateSelection.Source != "" {
		record.FolderDateSource = dateSelection.Source
		record.FolderYear = dateSelection.Time.UTC().Year()
		if dateSelection.MonthKnown {
			record.FolderMonth = int(dateSelection.Time.UTC().Month())
		}
	}
	return record, nil
}

func moveOptionalSidecar(oldPath string, newPath string) {
	if !FileExists(oldPath) || strings.EqualFold(filepath.Clean(oldPath), filepath.Clean(newPath)) {
		return
	}
	if FileExists(newPath) {
		return
	}
	_ = os.Rename(oldPath, newPath)
}

func writeSidecarBackfillReport(stateDir string, result SidecarBackfillResult) (string, error) {
	reportDir := filepath.Join(stateDir, "reports")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", err
	}
	timestamped := filepath.Join(reportDir, "sidecar-backfill-"+result.FinishedAt.Format("20060102-150405")+".json")
	result.ReportPath = timestamped
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(timestamped, data, 0o644); err != nil {
		return "", err
	}
	latest := filepath.Join(reportDir, "sidecar-backfill-latest.json")
	if err := os.WriteFile(latest, data, 0o644); err != nil {
		return "", err
	}
	return timestamped, nil
}
