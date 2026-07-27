package batch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
)

func Preflight(options Options) (PreflightReport, error) {
	options = normalizeOptions(options)
	if err := validateProcessingDependencies(options); err != nil {
		return PreflightReport{}, err
	}

	drives, driveErr := DetectDrives()
	if driveErr != nil {
		fixer.Log(fixer.LoggerWarn, "Drive scan failed: %v", driveErr)
	}

	outputDir, err := resolveOutputDir(options, drives)
	if err != nil {
		return PreflightReport{}, err
	}
	options.OutputDir = outputDir
	if strings.TrimSpace(options.StagingOutputDir) != "" {
		stagingOutputDir, err := filepath.Abs(options.StagingOutputDir)
		if err != nil {
			return PreflightReport{}, err
		}
		options.StagingOutputDir = stagingOutputDir
	}

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
	workDirs, err := resolveWorkDirs(workOptions, drives, zips)
	if err != nil {
		return PreflightReport{}, err
	}
	options.WorkDirs = workDirs
	options.WorkDir = firstWorkDir(workDirs)

	report := PreflightReport{
		ZipCount:              len(zips),
		EstimatedMinWorkBytes: maxRequiredWorkBytes(zips, options.SafetyMarginBytes),
		OutputDir:             options.OutputDir,
		StagingOutputDir:      options.StagingOutputDir,
		WorkDir:               options.WorkDir,
		ZipRoots:              append([]string(nil), options.ZipRoots...),
		WorkDirs:              append([]string(nil), options.WorkDirs...),
		ManifestPath:          manifestPath(options.OutputDir),
		StatePath:             filepath.Join(options.OutputDir, ".gtf", "state.jsonl"),
		ZipPaths:              make([]string, 0, len(zips)),
		MotionMergeEnabled:    options.ProcessOptions.CreateMotionPhotos,
	}
	if motionPath, motionErr := fixer.ResolveMotionPhotoTool(options.MotionToolPath); motionErr == nil {
		report.MotionPhotoToolFound = true
		report.MotionPhotoToolPath = motionPath
	} else if report.MotionMergeEnabled {
		return report, motionErr
	}
	for _, item := range zips {
		report.TotalZipSize += item.SizeBytes
		report.EstimatedMediaFiles += item.MediaFiles
		if item.SizeBytes > report.LargestZipBytes {
			report.LargestZipBytes = item.SizeBytes
		}
		report.ZipPaths = append(report.ZipPaths, item.Path)
	}
	if manifest, manifestErr := OpenManifest(report.ManifestPath); manifestErr != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("cannot inspect existing manifest: %v", manifestErr))
	} else {
		report.LegacyCompletedZips = manifest.LegacySuccessfulCount(zips)
		if closeErr := manifest.Close(); closeErr != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("cannot close existing manifest: %v", closeErr))
		}
		if report.LegacyCompletedZips > 0 {
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"%d ZIPs were completed by older workflow; use clean output folder or move old output aside",
				report.LegacyCompletedZips,
			))
		}
	}

	report.OutputFreeBytes, err = freeSpaceForPath(options.OutputDir)
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("cannot check output free space: %v", err))
	}
	if options.StagingOutputDir != "" {
		report.StagingFreeBytes, err = freeSpaceForPath(options.StagingOutputDir)
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("cannot check staging output free space: %v", err))
		}
	}
	report.WorkFreeBytes, err = freeSpaceForPath(options.WorkDir)
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("cannot check work free space: %v", err))
	}
	report.WorkRoots = workRootReports(options.WorkDirs, drives, report.EstimatedMinWorkBytes)

	report.Warnings = append(report.Warnings, collectPathWarnings(options.ZipRoots, options.WorkDirs, options.OutputDir)...)
	if options.StagingOutputDir != "" {
		if pathsOverlap(options.StagingOutputDir, options.OutputDir) {
			report.Warnings = append(report.Warnings, "staging output overlaps final output")
		}
		for _, workDir := range options.WorkDirs {
			if pathsOverlap(options.StagingOutputDir, workDir) {
				report.Warnings = append(report.Warnings, "staging output overlaps work folder")
			}
		}
		if report.StagingFreeBytes > 0 && report.StagingFreeBytes < report.LargestZipBytes {
			report.Warnings = append(report.Warnings, fmt.Sprintf("staging output has %s free; largest ZIP is %s",
				fixer.FormatBytes(report.StagingFreeBytes),
				fixer.FormatBytes(report.LargestZipBytes),
			))
		}
	}
	if report.WorkFreeBytes > 0 && report.WorkFreeBytes < report.EstimatedMinWorkBytes {
		report.Warnings = append(report.Warnings, fmt.Sprintf("work drive has %s free; estimated minimum is %s",
			fixer.FormatBytes(report.WorkFreeBytes),
			fixer.FormatBytes(report.EstimatedMinWorkBytes),
		))
	}
	if onlyHDDWorkRoots(report.WorkRoots) {
		report.Warnings = append(report.Warnings, "all detected work roots are HDD; an SSD/NVMe work root may be faster")
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
	fmt.Fprintf(&b, "Photos/videos estimated: %d\n", report.EstimatedMediaFiles)
	fmt.Fprintf(&b, "Total ZIP size: %s\n", fixer.FormatBytes(report.TotalZipSize))
	fmt.Fprintf(&b, "Largest ZIP: %s\n", fixer.FormatBytes(report.LargestZipBytes))
	fmt.Fprintf(&b, "Output free: %s\n", fixer.FormatBytes(report.OutputFreeBytes))
	if report.StagingOutputDir != "" {
		fmt.Fprintf(&b, "Staging output free: %s\n", fixer.FormatBytes(report.StagingFreeBytes))
	}
	fmt.Fprintf(&b, "Work free: %s\n", fixer.FormatBytes(report.WorkFreeBytes))
	fmt.Fprintf(&b, "Largest ZIP work requirement: %s\n", fixer.FormatBytes(report.EstimatedMinWorkBytes))
	fmt.Fprintf(&b, "Output: %s\n", report.OutputDir)
	if report.StagingOutputDir != "" {
		fmt.Fprintf(&b, "Staging output: %s\n", report.StagingOutputDir)
	}
	fmt.Fprintf(&b, "Motion merge after ZIPs: %t\n", report.MotionMergeEnabled)
	fmt.Fprintf(&b, "MotionPhoto2 found: %t", report.MotionPhotoToolFound)
	if report.MotionPhotoToolPath != "" {
		fmt.Fprintf(&b, " (%s)", report.MotionPhotoToolPath)
	}
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "ZIP roots:\n")
	for _, root := range report.ZipRoots {
		fmt.Fprintf(&b, "  - %s\n", root)
	}
	fmt.Fprintf(&b, "Work roots:\n")
	for _, root := range report.WorkRoots {
		status := "usable"
		if !root.Usable {
			status = "not usable"
		}
		fmt.Fprintf(&b, "  - %s free=%s required=%s type=%s status=%s",
			root.Path,
			fixer.FormatBytes(root.FreeBytes),
			fixer.FormatBytes(root.RequiredBytes),
			root.Kind,
			status,
		)
		if root.Warning != "" {
			fmt.Fprintf(&b, " warning=%s", root.Warning)
		}
		fmt.Fprintf(&b, "\n")
	}
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

func collectPathWarnings(zipRoots []string, workDirs []string, outputDir string) []string {
	var warnings []string
	outputAbs, outputErr := filepath.Abs(outputDir)
	workAbsPaths := make([]string, 0, len(workDirs))
	for _, workDir := range workDirs {
		workAbs, workErr := filepath.Abs(workDir)
		if workErr != nil {
			warnings = append(warnings, fmt.Sprintf("cannot resolve work folder %s: %v", workDir, workErr))
			continue
		}
		if outputErr == nil && pathsOverlap(outputAbs, workAbs) {
			warnings = append(warnings, fmt.Sprintf("output and work folders overlap: %s", workDir))
		}
		for _, existing := range workAbsPaths {
			if pathsOverlap(existing, workAbs) {
				warnings = append(warnings, fmt.Sprintf("work folders overlap: %s and %s", existing, workDir))
			}
		}
		workAbsPaths = append(workAbsPaths, workAbs)
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
		for _, workAbs := range workAbsPaths {
			if pathsOverlap(rootAbs, workAbs) {
				warnings = append(warnings, fmt.Sprintf("ZIP source and work folder overlap: %s", root))
			}
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
