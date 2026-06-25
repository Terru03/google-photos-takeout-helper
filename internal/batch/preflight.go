package batch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
)

func Preflight(options Options) (PreflightReport, error) {
	options = normalizeOptions(options)

	drives, driveErr := DetectDrives()
	if driveErr != nil {
		fixer.Log(fixer.LoggerWarn, "Drive scan failed: %v", driveErr)
	}

	outputDir, err := resolveOutputDir(options, drives)
	if err != nil {
		return PreflightReport{}, err
	}
	options.OutputDir = outputDir

	zipRoots, err := resolveZipRoots(options, drives)
	if err != nil {
		return PreflightReport{}, err
	}
	options.ZipRoots = zipRoots

	zips, err := FindTakeoutZips(options.ZipRoots)
	if err != nil {
		return PreflightReport{}, err
	}
	if len(zips) == 0 {
		return PreflightReport{}, fmt.Errorf("no ZIP files found")
	}

	workOptions := options
	workOptions.ProcessOptions.DryRun = false
	workDir, err := resolveWorkDir(workOptions, drives, zips)
	if err != nil {
		return PreflightReport{}, err
	}
	options.WorkDir = workDir

	report := PreflightReport{
		ZipCount:              len(zips),
		EstimatedMinWorkBytes: maxRequiredWorkBytes(zips, options.SafetyMarginBytes),
		OutputDir:             options.OutputDir,
		WorkDir:               options.WorkDir,
		ManifestPath:          manifestPath(options.OutputDir),
		StatePath:             filepath.Join(options.OutputDir, ".gtf", "state.jsonl"),
		ZipPaths:              make([]string, 0, len(zips)),
	}
	for _, item := range zips {
		report.TotalZipSize += item.SizeBytes
		report.ZipPaths = append(report.ZipPaths, item.Path)
	}

	report.OutputFreeBytes, err = freeSpaceForPath(options.OutputDir)
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("cannot check output free space: %v", err))
	}
	report.WorkFreeBytes, err = freeSpaceForPath(options.WorkDir)
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("cannot check work free space: %v", err))
	}

	report.Warnings = append(report.Warnings, collectPathWarnings(options.ZipRoots, options.WorkDir, options.OutputDir)...)
	if report.WorkFreeBytes > 0 && report.WorkFreeBytes < report.EstimatedMinWorkBytes {
		report.Warnings = append(report.Warnings, fmt.Sprintf("work drive has %s free; estimated minimum is %s",
			fixer.FormatBytes(report.WorkFreeBytes),
			fixer.FormatBytes(report.EstimatedMinWorkBytes),
		))
	}
	if report.OutputFreeBytes > 0 && report.OutputFreeBytes < report.TotalZipSize {
		report.Warnings = append(report.Warnings, fmt.Sprintf("output drive has %s free; ZIP files total %s before expansion",
			fixer.FormatBytes(report.OutputFreeBytes),
			fixer.FormatBytes(report.TotalZipSize),
		))
	}
	if fixer.FileExists(report.StatePath) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("existing fixer state found: %s", report.StatePath))
	}
	if err := writePreflightReport(report); err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("could not write preflight report: %v", err))
	}
	return report, nil
}

func writePreflightReport(report PreflightReport) error {
	dir := filepath.Join(report.OutputDir, ".gtf", "batch")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "preflight_latest.json"), data, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "preflight_latest.txt"), []byte(FormatPreflightReport(report)), 0o644)
}

func FormatPreflightReport(report PreflightReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Huge Takeout preflight\n")
	fmt.Fprintf(&b, "ZIP files: %d\n", report.ZipCount)
	fmt.Fprintf(&b, "Total ZIP size: %s\n", fixer.FormatBytes(report.TotalZipSize))
	fmt.Fprintf(&b, "Output free: %s\n", fixer.FormatBytes(report.OutputFreeBytes))
	fmt.Fprintf(&b, "Work free: %s\n", fixer.FormatBytes(report.WorkFreeBytes))
	fmt.Fprintf(&b, "Estimated minimum work space: %s\n", fixer.FormatBytes(report.EstimatedMinWorkBytes))
	fmt.Fprintf(&b, "Output: %s\n", report.OutputDir)
	fmt.Fprintf(&b, "Work: %s\n", report.WorkDir)
	fmt.Fprintf(&b, "Manifest: %s\n", report.ManifestPath)
	if len(report.Warnings) == 0 {
		fmt.Fprintf(&b, "Warnings: none\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Warnings:\n")
	for _, warning := range report.Warnings {
		fmt.Fprintf(&b, "  - %s\n", warning)
	}
	return b.String()
}

func collectPathWarnings(zipRoots []string, workDir string, outputDir string) []string {
	var warnings []string
	outputAbs, outputErr := filepath.Abs(outputDir)
	workAbs, workErr := filepath.Abs(workDir)
	if outputErr == nil && workErr == nil && pathsOverlap(outputAbs, workAbs) {
		warnings = append(warnings, "output and work folders overlap; choose separate folders")
	}
	outputDrive := DriveRoot(outputDir)
	for _, root := range zipRoots {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("cannot resolve ZIP root %s: %v", root, err))
			continue
		}
		if outputErr == nil && pathsOverlap(rootAbs, outputAbs) {
			warnings = append(warnings, fmt.Sprintf("ZIP source and output overlap: %s", root))
		}
		if workErr == nil && pathsOverlap(rootAbs, workAbs) {
			warnings = append(warnings, fmt.Sprintf("ZIP source and work folder overlap: %s", root))
		}
		if outputDrive != "" && strings.EqualFold(DriveRoot(rootAbs), outputDrive) {
			warnings = append(warnings, fmt.Sprintf("output drive is also used for ZIP storage: %s", outputDrive))
		}
	}
	return warnings
}

func freeSpaceForPath(path string) (int64, error) {
	probe, err := nearestExistingPath(path)
	if err != nil {
		return 0, err
	}
	return FreeSpace(probe)
}

func nearestExistingPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("empty path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("no existing parent for %s", path)
		}
		abs = parent
	}
}
