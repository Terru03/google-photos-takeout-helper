package fixer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type AlbumCleanupStatus string

const (
	AlbumCleanupStatusSkipped AlbumCleanupStatus = "skipped"
	AlbumCleanupStatusDryRun  AlbumCleanupStatus = "dry-run"
	AlbumCleanupStatusDone    AlbumCleanupStatus = "completed"
	AlbumCleanupStatusFailed  AlbumCleanupStatus = "failed"
)

type AlbumCleanupResult struct {
	Enabled               bool               `json:"enabled"`
	Status                AlbumCleanupStatus `json:"status"`
	TimelineHashes        int                `json:"timelineHashes"`
	AlbumFilesScanned     int                `json:"albumFilesScanned"`
	DuplicateFilesRemoved int                `json:"duplicateFilesRemoved"`
	EmptyDirsRemoved      int                `json:"emptyDirsRemoved"`
	BytesMatched          int64              `json:"bytesMatched"`
	ReportPath            string             `json:"reportPath,omitempty"`
	Errors                []string           `json:"errors,omitempty"`
}

func CleanupDuplicateAlbumFiles(outputRoot string, options ProcessOptions) (AlbumCleanupResult, error) {
	options = options.Normalized()
	result := AlbumCleanupResult{
		Enabled: options.AlbumMode == AlbumModeUniqueOnly,
		Status:  AlbumCleanupStatusSkipped,
	}
	if options.AlbumMode != AlbumModeUniqueOnly {
		return result, nil
	}
	if options.DryRun {
		result.Status = AlbumCleanupStatusDryRun
	}

	outputAbs, err := filepath.Abs(outputRoot)
	if err != nil {
		result.Status = AlbumCleanupStatusFailed
		return result, err
	}
	statePath := filepath.Join(outputAbs, ".gtf", "state.jsonl")
	stateStore, err := OpenStateStore(statePath)
	if err != nil {
		result.Status = AlbumCleanupStatusFailed
		return result, err
	}
	defer func() {
		if closeErr := stateStore.Close(); closeErr != nil {
			Log(LoggerWarn, "Close state store after album cleanup: %v", closeErr)
		}
	}()

	timelineHashes := make(map[string]struct{})
	records := stateStore.Records()
	for _, record := range records {
		if !record.Successful() || record.SourceHash == "" || !recordIsTimeline(record) {
			continue
		}
		if record.OutputPath == "" || !FileExists(record.OutputPath) {
			continue
		}
		timelineHashes[record.SourceHash] = struct{}{}
	}
	result.TimelineHashes = len(timelineHashes)

	dirsToPrune := make(map[string]struct{})
	for _, record := range records {
		if !record.Successful() || record.SourceHash == "" || record.OutputPath == "" || recordIsTimeline(record) {
			continue
		}
		result.AlbumFilesScanned++
		if _, ok := timelineHashes[record.SourceHash]; !ok {
			continue
		}
		outputPath, safe, err := safeOutputChild(outputAbs, record.OutputPath)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		if !safe || !FileExists(outputPath) {
			continue
		}
		info, err := os.Stat(outputPath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("stat album duplicate %s: %v", outputPath, err))
			continue
		}
		if info.IsDir() {
			continue
		}
		result.BytesMatched += info.Size()
		if options.DryRun {
			result.DuplicateFilesRemoved++
			continue
		}
		if err := os.Remove(outputPath); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("remove album duplicate %s: %v", outputPath, err))
			continue
		}
		result.DuplicateFilesRemoved++
		dirsToPrune[filepath.Dir(outputPath)] = struct{}{}
		for _, sidecar := range cleanupSidecarPaths(outputPath) {
			sidecarPath, sidecarSafe, err := safeOutputChild(outputAbs, sidecar)
			if err != nil {
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			if sidecarSafe && FileExists(sidecarPath) {
				if err := os.Remove(sidecarPath); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("remove album sidecar %s: %v", sidecarPath, err))
				}
			}
		}
	}

	if !options.DryRun {
		result.EmptyDirsRemoved += pruneAlbumDirs(outputAbs, dirsToPrune, &result)
	}
	if result.Status != AlbumCleanupStatusDryRun {
		result.Status = AlbumCleanupStatusDone
	}
	if len(result.Errors) > 0 {
		result.Status = AlbumCleanupStatusFailed
	}
	reportPath, reportErr := writeAlbumCleanupReport(outputAbs, result)
	if reportErr != nil {
		result.Status = AlbumCleanupStatusFailed
		result.Errors = append(result.Errors, reportErr.Error())
		return result, reportErr
	}
	result.ReportPath = reportPath
	if len(result.Errors) > 0 {
		return result, errors.New(strings.Join(result.Errors, "; "))
	}
	return result, nil
}

func recordIsTimeline(record ProcessRecord) bool {
	top := topLevelFromRelPath(record.SourceRelPath)
	ok, _ := IsYearFolder(top)
	return ok
}

func topLevelFromRelPath(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.Trim(path, "/")
	if path == "" {
		return ""
	}
	if index := strings.IndexByte(path, '/'); index >= 0 {
		return path[:index]
	}
	return path
}

func safeOutputChild(outputAbs string, child string) (string, bool, error) {
	childAbs, err := filepath.Abs(child)
	if err != nil {
		return "", false, err
	}
	outputClean := strings.ToLower(filepath.Clean(outputAbs))
	childClean := strings.ToLower(filepath.Clean(childAbs))
	if childClean == outputClean || !strings.HasPrefix(childClean, outputClean+string(filepath.Separator)) {
		return childAbs, false, nil
	}
	rel, err := filepath.Rel(outputAbs, childAbs)
	if err != nil {
		return "", false, err
	}
	if strings.HasPrefix(rel, ".gtf"+string(filepath.Separator)) || rel == ".gtf" {
		return childAbs, false, nil
	}
	return childAbs, true, nil
}

func cleanupSidecarPaths(path string) []string {
	return []string{path + ".xmp"}
}

func pruneAlbumDirs(outputAbs string, dirs map[string]struct{}, result *AlbumCleanupResult) int {
	ordered := make([]string, 0, len(dirs))
	for dir := range dirs {
		ordered = append(ordered, dir)
	}
	sort.SliceStable(ordered, func(i int, j int) bool {
		return len(ordered[i]) > len(ordered[j])
	})

	removed := 0
	for _, dir := range ordered {
		for {
			dirAbs, safe, err := safeOutputChild(outputAbs, dir)
			if err != nil {
				result.Errors = append(result.Errors, err.Error())
				break
			}
			if !safe || recordTopDirIsTimeline(outputAbs, dirAbs) {
				break
			}
			err = os.Remove(dirAbs)
			if err == nil {
				removed++
				dir = filepath.Dir(dirAbs)
				continue
			}
			if os.IsNotExist(err) || directoryNotEmpty(err) {
				break
			}
			result.Errors = append(result.Errors, fmt.Sprintf("remove empty album folder %s: %v", dirAbs, err))
			break
		}
	}
	return removed
}

func recordTopDirIsTimeline(outputAbs string, dirAbs string) bool {
	rel, err := filepath.Rel(outputAbs, dirAbs)
	if err != nil || rel == "." || rel == "" {
		return true
	}
	top := topLevelFromRelPath(rel)
	ok, _ := IsYearFolder(top)
	return ok
}

func directoryNotEmpty(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "directory not empty") ||
		strings.Contains(text, "not empty") ||
		strings.Contains(text, "the directory is not empty")
}

func writeAlbumCleanupReport(outputAbs string, result AlbumCleanupResult) (string, error) {
	reportDir := filepath.Join(outputAbs, ".gtf", "reports")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", err
	}
	latest := filepath.Join(reportDir, "album_cleanup_latest.json")
	result.ReportPath = latest
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(latest, data, 0o644); err != nil {
		return "", err
	}
	stamped := filepath.Join(reportDir, "album_cleanup_"+time.Now().Format("2006-01-02_15-04-05")+".json")
	if err := os.WriteFile(stamped, data, 0o644); err != nil {
		return "", err
	}
	return latest, nil
}
