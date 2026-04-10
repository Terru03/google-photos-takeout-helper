package fixer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const appDirName = "GoogleTakeoutFixer"

type RuntimePaths struct {
	ConfigDir       string
	PreferencesPath string
	RunRoot         string
	StateDir        string
	LogDir          string
	ReportDir       string
}

func ResolveRuntimePaths(outputRoot string) (RuntimePaths, error) {
	configDir, err := resolveConfigDir()
	if err != nil {
		return RuntimePaths{}, err
	}

	paths := RuntimePaths{
		ConfigDir:       configDir,
		PreferencesPath: filepath.Join(configDir, "config.json"),
	}

	if outputRoot == "" {
		paths.RunRoot = filepath.Join(configDir, "runtime")
	} else {
		paths.RunRoot = filepath.Join(outputRoot, ".gtf")
	}

	paths.StateDir = paths.RunRoot
	paths.LogDir = filepath.Join(paths.RunRoot, "logs")
	paths.ReportDir = filepath.Join(paths.RunRoot, "reports")
	return paths, nil
}

func ValidateProcessPaths(sourcePath string, outputPath string) error {
	if strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(outputPath) == "" {
		return errors.New("input and output folders are required")
	}

	sourceAbs, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("resolve input path: %w", err)
	}
	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}

	sourceInfo, err := os.Stat(sourceAbs)
	if err != nil {
		return fmt.Errorf("input folder: %w", err)
	}
	if !sourceInfo.IsDir() {
		return errors.New("input path must be a directory")
	}

	if strings.EqualFold(filepath.Clean(sourceAbs), filepath.Clean(outputAbs)) {
		return errors.New("input and output folders must be different")
	}
	if isNestedPath(sourceAbs, outputAbs) {
		return errors.New("output folder must not be inside the input folder")
	}
	if isNestedPath(outputAbs, sourceAbs) {
		return errors.New("input folder must not be inside the output folder")
	}

	return nil
}

func isNestedPath(parent string, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func resolveConfigDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("GTF_CONFIG_DIR")); override != "" {
		return override, nil
	}

	root, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(root, appDirName), nil
}
