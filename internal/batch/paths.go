package batch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ValidateBatchPaths(zipRoots []string, workDir string, outputDir string, dryRun bool) error {
	var workDirs []string
	if strings.TrimSpace(workDir) != "" {
		workDirs = []string{workDir}
	}
	return ValidateBatchPathSet(zipRoots, workDirs, outputDir, dryRun)
}

func ValidateBatchPathSet(zipRoots []string, workDirs []string, outputDir string, dryRun bool) error {
	return ValidateBatchPathSetWithStaging(zipRoots, workDirs, outputDir, "", dryRun)
}

func ValidateBatchPathSetWithStaging(zipRoots []string, workDirs []string, outputDir string, stagingOutputDir string, dryRun bool) error {
	if strings.TrimSpace(outputDir) == "" {
		return fmt.Errorf("output folder is required")
	}
	if !dryRun && len(nonEmptyStrings(workDirs)) == 0 {
		return fmt.Errorf("at least one work folder is required")
	}
	if len(zipRoots) == 0 {
		return fmt.Errorf("at least one ZIP root is required")
	}

	outputAbs, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("resolve output folder: %w", err)
	}
	stagingAbs := ""
	if strings.TrimSpace(stagingOutputDir) != "" {
		stagingAbs, err = filepath.Abs(stagingOutputDir)
		if err != nil {
			return fmt.Errorf("resolve staging output folder: %w", err)
		}
		if pathsOverlap(outputAbs, stagingAbs) {
			return fmt.Errorf("staging output folder and final output folder must be separate")
		}
	}
	workAbsPaths := make([]string, 0, len(workDirs))
	for _, workDir := range workDirs {
		if strings.TrimSpace(workDir) == "" {
			continue
		}
		workAbs, err := filepath.Abs(workDir)
		if err != nil {
			return fmt.Errorf("resolve work folder: %w", err)
		}
		if pathsOverlap(outputAbs, workAbs) {
			return fmt.Errorf("work folder and output folder must be separate")
		}
		if stagingAbs != "" && pathsOverlap(stagingAbs, workAbs) {
			return fmt.Errorf("work folder and staging output folder must be separate")
		}
		for _, existing := range workAbsPaths {
			if pathsOverlap(existing, workAbs) {
				return fmt.Errorf("work folders must be separate: %s and %s", existing, workDir)
			}
		}
		workAbsPaths = append(workAbsPaths, workAbs)
	}

	for _, root := range zipRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		info, err := os.Stat(root)
		if err != nil {
			return fmt.Errorf("zip root %s: %w", root, err)
		}
		if !info.IsDir() && !LooksLikeTakeoutZip(root) {
			return fmt.Errorf("zip root %s is not a directory or Takeout ZIP", root)
		}
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			return fmt.Errorf("resolve zip root %s: %w", root, err)
		}
		if pathsOverlap(rootAbs, outputAbs) {
			return fmt.Errorf("ZIP source root and output folder must be separate: %s", root)
		}
		if stagingAbs != "" && pathsOverlap(rootAbs, stagingAbs) {
			return fmt.Errorf("ZIP source root and staging output folder must be separate: %s", root)
		}
		if !dryRun {
			for _, workAbs := range workAbsPaths {
				if pathsOverlap(rootAbs, workAbs) {
					return fmt.Errorf("ZIP source root and work folder must be separate: %s", root)
				}
			}
		}
	}
	return nil
}

func cleanupTempExtract(tempDir string, workDir string) error {
	tempAbs, err := filepath.Abs(tempDir)
	if err != nil {
		return err
	}
	workAbs, err := filepath.Abs(workDir)
	if err != nil {
		return err
	}
	if strings.EqualFold(filepath.Clean(tempAbs), filepath.Clean(workAbs)) {
		return fmt.Errorf("refuse to delete work folder root %s", tempAbs)
	}
	if !pathWithin(workAbs, tempAbs) {
		return fmt.Errorf("refuse to delete temp folder outside work folder: %s", tempAbs)
	}
	if !strings.HasPrefix(filepath.Base(tempAbs), "gtf-zip-") {
		return fmt.Errorf("refuse to delete temp folder without gtf-zip- prefix: %s", tempAbs)
	}
	return os.RemoveAll(tempAbs)
}

func pathsOverlap(left string, right string) bool {
	leftAbs, err := filepath.Abs(left)
	if err != nil {
		return false
	}
	rightAbs, err := filepath.Abs(right)
	if err != nil {
		return false
	}
	leftClean := filepath.Clean(leftAbs)
	rightClean := filepath.Clean(rightAbs)
	return strings.EqualFold(leftClean, rightClean) ||
		pathWithin(leftClean, rightClean) ||
		pathWithin(rightClean, leftClean)
}
