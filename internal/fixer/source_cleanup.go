package fixer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func CleanupSourceRootAfterSuccess(
	ctx context.Context,
	sourcePath string,
	outputPath string,
	report *RunReport,
	options ProcessOptions,
) SourceCleanupResult {
	options = options.Normalized()

	result := SourceCleanupResult{
		Enabled: options.DeleteSourceAfterSuccess,
		Status:  SourceCleanupStatusSkippedDisabled,
		Path:    sourcePath,
	}
	if !options.DeleteSourceAfterSuccess {
		return result
	}
	if options.DryRun {
		result.Status = SourceCleanupStatusSkippedDryRun
		result.Reason = "dry run keeps input untouched"
		return result
	}
	if ctx.Err() != nil {
		result.Status = SourceCleanupStatusSkippedCancelled
		result.Reason = "run was cancelled"
		return result
	}

	allowed, reason := report.CanDeleteSourceRoot()
	if !allowed {
		result.Status = SourceCleanupStatusSkippedProblems
		result.Reason = reason
		return result
	}

	sourceAbs, err := resolveSafeSourceCleanupPath(sourcePath, outputPath)
	if err != nil {
		result.Status = SourceCleanupStatusFailed
		result.Error = err.Error()
		return result
	}
	result.Path = sourceAbs

	if err := os.RemoveAll(sourceAbs); err != nil {
		result.Status = SourceCleanupStatusFailed
		result.Error = fmt.Sprintf("delete input folder %s: %v", sourceAbs, err)
		return result
	}

	result.Status = SourceCleanupStatusDeleted
	result.Reason = "deleted after clean run"
	return result
}

func resolveSafeSourceCleanupPath(sourcePath string, outputPath string) (string, error) {
	if err := ValidateProcessPaths(sourcePath, outputPath); err != nil {
		return "", err
	}

	sourceAbs, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", fmt.Errorf("resolve input folder: %w", err)
	}

	cleanSource := filepath.Clean(sourceAbs)
	volumeName := filepath.VolumeName(cleanSource)
	if volumeName != "" {
		volumeRoot := filepath.Clean(volumeName + string(filepath.Separator))
		if strings.EqualFold(cleanSource, volumeRoot) {
			return "", fmt.Errorf("refuse to delete drive root %s", cleanSource)
		}
	}
	if filepath.Dir(cleanSource) == cleanSource {
		return "", fmt.Errorf("refuse to delete root-like path %s", cleanSource)
	}
	if strings.TrimSpace(filepath.Base(cleanSource)) == "" {
		return "", fmt.Errorf("refuse to delete empty input path")
	}

	return cleanSource, nil
}
