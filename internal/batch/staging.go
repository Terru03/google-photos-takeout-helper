package batch

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
)

func CommitStagedReport(
	ctx context.Context,
	stagingRoot string,
	finalRoot string,
	report *fixer.RunReport,
	preferSymlink bool,
) error {
	if report == nil {
		return fmt.Errorf("staging commit needs run report")
	}
	stagingAbs, finalAbs, err := validateStagingCommitRoots(stagingRoot, finalRoot)
	if err != nil {
		return err
	}

	for _, record := range report.Records {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !record.Successful() || record.StagedPath == "" {
			continue
		}
		if err := commitOneStagedFile(stagingAbs, finalAbs, record.StagedPath, record.OutputPath); err != nil {
			return fmt.Errorf("commit %s: %w", record.OutputPath, err)
		}
		stagedXMP := record.StagedPath + ".xmp"
		if fixer.FileExists(stagedXMP) {
			if err := commitOneStagedFile(stagingAbs, finalAbs, stagedXMP, record.OutputPath+".xmp"); err != nil {
				return fmt.Errorf("commit %s: %w", record.OutputPath+".xmp", err)
			}
		}
	}

	for _, record := range report.Records {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !record.Successful() || record.StagedPath != "" || record.DuplicateOf == "" || record.OutputPath == "" {
			continue
		}
		if fixer.FileExists(record.OutputPath) {
			continue
		}
		if !pathWithin(finalAbs, record.OutputPath) || !pathWithin(finalAbs, record.DuplicateOf) {
			return fmt.Errorf("duplicate path escapes final output: %s", record.OutputPath)
		}
		if err := os.MkdirAll(filepath.Dir(record.OutputPath), 0o755); err != nil {
			return err
		}
		if _, err := fixer.LinkDuplicate(record.DuplicateOf, record.OutputPath, preferSymlink); err != nil {
			return fmt.Errorf("commit duplicate %s from %s: %w", record.OutputPath, record.DuplicateOf, err)
		}
		stagedXMP, mapErr := stagedPathForFinal(stagingAbs, finalAbs, record.OutputPath+".xmp")
		if mapErr != nil {
			return mapErr
		}
		if fixer.FileExists(stagedXMP) {
			if err := commitOneStagedFile(stagingAbs, finalAbs, stagedXMP, record.OutputPath+".xmp"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateStagingCommitRoots(stagingRoot string, finalRoot string) (string, string, error) {
	stagingAbs, err := filepath.Abs(stagingRoot)
	if err != nil {
		return "", "", err
	}
	finalAbs, err := filepath.Abs(finalRoot)
	if err != nil {
		return "", "", err
	}
	if strings.EqualFold(filepath.Clean(stagingAbs), filepath.Clean(finalAbs)) || pathsOverlap(stagingAbs, finalAbs) {
		return "", "", fmt.Errorf("staging and final output roots must be separate")
	}
	if !strings.HasPrefix(strings.ToLower(filepath.Base(stagingAbs)), "gtf-stage-") {
		return "", "", fmt.Errorf("staging folder lacks gtf-stage- prefix: %s", stagingAbs)
	}
	return stagingAbs, finalAbs, nil
}

func commitOneStagedFile(stagingRoot string, finalRoot string, stagedPath string, finalPath string) error {
	if !pathWithin(stagingRoot, stagedPath) {
		return fmt.Errorf("staged file escapes staging root: %s", stagedPath)
	}
	if !pathWithin(finalRoot, finalPath) {
		return fmt.Errorf("final file escapes output root: %s", finalPath)
	}
	if !fixer.FileExists(stagedPath) {
		return fmt.Errorf("staged file missing: %s", stagedPath)
	}
	if fixer.FileExists(finalPath) {
		same, err := filesHaveSameContent(stagedPath, finalPath)
		if err != nil {
			return err
		}
		if !same {
			return fmt.Errorf("final path already has different content: %s", finalPath)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return err
	}
	if err := os.Rename(stagedPath, finalPath); err == nil {
		return nil
	}
	if err := copyFileAtomic(stagedPath, finalPath); err != nil {
		return err
	}
	return os.Remove(stagedPath)
}

func copyFileAtomic(sourcePath string, finalPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer func() {
		_ = source.Close()
	}()
	info, err := source.Stat()
	if err != nil {
		return err
	}

	temp, err := os.CreateTemp(filepath.Dir(finalPath), ".gtf-commit-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	keepTemp := false
	defer func() {
		_ = temp.Close()
		if !keepTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := io.Copy(temp, source); err != nil {
		return err
	}
	if err := temp.Chmod(info.Mode()); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := preserveFileTimestamps(sourcePath, tempPath); err != nil {
		return err
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return err
	}
	keepTemp = true
	return nil
}

func filesHaveSameContent(left string, right string) (bool, error) {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return false, err
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		return false, err
	}
	if leftInfo.Size() != rightInfo.Size() {
		return false, nil
	}
	leftHash, err := fixer.HashFile(left)
	if err != nil {
		return false, err
	}
	rightHash, err := fixer.HashFile(right)
	if err != nil {
		return false, err
	}
	return leftHash == rightHash, nil
}

func stagedPathForFinal(stagingRoot string, finalRoot string, finalPath string) (string, error) {
	rel, err := filepath.Rel(finalRoot, finalPath)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("final path escapes output root: %s", finalPath)
	}
	return filepath.Join(stagingRoot, rel), nil
}

func cleanupStagingRoot(stagingRoot string, stagingParent string) error {
	stagingAbs, err := filepath.Abs(stagingRoot)
	if err != nil {
		return err
	}
	parentAbs, err := filepath.Abs(stagingParent)
	if err != nil {
		return err
	}
	if !pathWithin(parentAbs, stagingAbs) || strings.EqualFold(filepath.Clean(parentAbs), filepath.Clean(stagingAbs)) {
		return fmt.Errorf("refuse to delete staging folder outside staging root: %s", stagingAbs)
	}
	if !strings.HasPrefix(strings.ToLower(filepath.Base(stagingAbs)), "gtf-stage-") {
		return fmt.Errorf("refuse to delete staging folder without gtf-stage- prefix: %s", stagingAbs)
	}
	return os.RemoveAll(stagingAbs)
}
