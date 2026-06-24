package batch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
)

func Run(ctx context.Context, options Options) (Result, error) {
	options = normalizeOptions(options)

	drives, driveErr := DetectDrives()
	if driveErr != nil {
		fixer.Log(fixer.LoggerWarn, "Drive scan failed: %v", driveErr)
	} else {
		for _, drive := range drives {
			fixer.Log(fixer.LoggerInfo, "Drive: %s", FormatDrive(drive))
		}
	}

	outputDir, err := resolveOutputDir(options, drives)
	if err != nil {
		return Result{}, err
	}
	options.OutputDir = outputDir

	zipRoots, err := resolveZipRoots(options, drives)
	if err != nil {
		return Result{}, err
	}
	options.ZipRoots = zipRoots

	zips, err := FindTakeoutZips(options.ZipRoots)
	if err != nil {
		return Result{}, err
	}
	if len(zips) == 0 {
		return Result{}, fmt.Errorf("no Google Takeout ZIP files found")
	}
	fixer.Log(fixer.LoggerInfo, "Found %d Takeout ZIP files", len(zips))

	workDir, err := resolveWorkDir(options, drives, zips)
	if err != nil {
		return Result{}, err
	}
	options.WorkDir = workDir

	if err := ValidateBatchPaths(options.ZipRoots, options.WorkDir, options.OutputDir, options.ProcessOptions.DryRun); err != nil {
		return Result{}, err
	}

	manifest, err := OpenManifest(manifestPath(options.OutputDir))
	if err != nil {
		return Result{}, err
	}
	defer func() {
		if closeErr := manifest.Close(); closeErr != nil {
			fixer.Log(fixer.LoggerWarn, "Close manifest: %v", closeErr)
		}
	}()

	result := Result{
		ManifestPath: manifest.Path(),
		OutputDir:    options.OutputDir,
		WorkDir:      options.WorkDir,
		ZipCount:     len(zips),
	}

	if options.ProcessOptions.DryRun {
		for _, item := range zips {
			if manifest.AlreadySuccessful(item) && !options.Reprocess {
				result.Skipped++
				continue
			}
			entry := manifestEntryFor(item, options.OutputDir, statusPlanned)
			entry.StartedAt = time.Now().UTC()
			entry.FinishedAt = entry.StartedAt
			if err := manifest.Append(entry); err != nil {
				return result, err
			}
			result.Planned++
			fixer.Log(fixer.LoggerInfo, "Dry run planned ZIP: %s", item.Path)
		}
		return result, nil
	}

	if err := os.MkdirAll(options.OutputDir, 0o755); err != nil {
		return result, err
	}
	if err := os.MkdirAll(options.WorkDir, 0o755); err != nil {
		return result, err
	}

	for _, item := range zips {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if manifest.AlreadySuccessful(item) && !options.Reprocess {
			entry := manifestEntryFor(item, options.OutputDir, statusSkippedResume)
			entry.StartedAt = time.Now().UTC()
			entry.FinishedAt = entry.StartedAt
			if err := manifest.Append(entry); err != nil {
				return result, err
			}
			result.Skipped++
			fixer.Log(fixer.LoggerInfo, "Skip already processed ZIP: %s", item.Path)
			continue
		}

		if err := runOneZip(ctx, options, item, manifest); err != nil {
			return result, err
		}
		result.Processed++
	}

	return result, nil
}

func normalizeOptions(options Options) Options {
	if options.SafetyMarginBytes == 0 {
		options.SafetyMarginBytes = defaultMarginBytes
	}
	if options.Process == nil {
		options.Process = fixer.Process
	}
	options.ProcessOptions = options.ProcessOptions.Normalized()
	options.ProcessOptions.DeleteSourceAfterSuccess = false
	return options
}

func runOneZip(ctx context.Context, options Options, item ZipItem, manifest *Manifest) error {
	needBytes := requiredWorkBytes(item, options.SafetyMarginBytes)
	freeBytes, err := FreeSpace(options.WorkDir)
	if err != nil {
		return fmt.Errorf("check free space for %s: %w", options.WorkDir, err)
	}
	if freeBytes < needBytes {
		return fmt.Errorf("work folder %s needs %s free for %s, only %s free",
			options.WorkDir,
			fixer.FormatBytes(needBytes),
			item.Name,
			fixer.FormatBytes(freeBytes),
		)
	}

	tempDir, err := os.MkdirTemp(options.WorkDir, "gtf-zip-")
	if err != nil {
		return err
	}

	startedAt := time.Now().UTC()
	startEntry := manifestEntryFor(item, options.OutputDir, statusStarted)
	startEntry.StartedAt = startedAt
	startEntry.ExtractedTempPath = tempDir
	if err := manifest.Append(startEntry); err != nil {
		return err
	}

	fixer.Log(fixer.LoggerInfo, "Extract ZIP: %s", item.Path)
	if err := ExtractZip(item.Path, tempDir); err != nil {
		return finishFailedZip(manifest, startEntry, tempDir, options, statusError, "", nil, fmt.Errorf("extract ZIP: %w", err))
	}

	googlePhotosDir, err := LocateGooglePhotosFolder(tempDir)
	if err != nil {
		return finishFailedZip(manifest, startEntry, tempDir, options, statusError, "", nil, err)
	}

	progressCh := make(chan fixer.Progress)
	errCh := make(chan error, 1)
	go func() {
		errCh <- options.Process(ctx, googlePhotosDir, options.OutputDir, progressCh, options.ProcessOptions)
	}()
	for progress := range progressCh {
		if progress.Total == 0 {
			continue
		}
		fixer.Log(
			fixer.LoggerInfo,
			"ZIP %s progress %d/%d %s",
			item.Name,
			progress.Processed,
			progress.Total,
			filepath.Base(progress.Current),
		)
	}
	processErr := <-errCh

	reportPath, report, reportErr := loadLatestReport(options.OutputDir)
	if processErr != nil {
		return finishFailedZip(manifest, startEntry, tempDir, options, statusError, reportPath, report, processErr)
	}
	if reportErr != nil {
		return finishFailedZip(manifest, startEntry, tempDir, options, statusError, reportPath, report, reportErr)
	}
	if reportNeedsReview(report) {
		err := fmt.Errorf("run needs review: unmatched=%d ambiguous=%d errors=%d",
			report.Summary.Unmatched,
			report.Summary.Ambiguous,
			report.Summary.Errors,
		)
		if askContinue(options, err) {
			if !options.KeepTempOnError {
				if cleanupErr := cleanupTempExtract(tempDir, options.WorkDir); cleanupErr != nil {
					return finishFailedZip(manifest, startEntry, tempDir, options, statusError, reportPath, report, cleanupErr)
				}
			}
			entry := startEntry
			entry.Status = statusNeedsReview
			entry.FinishedAt = time.Now().UTC()
			entry.ReportPath = reportPath
			entry.Summary = &report.Summary
			entry.Error = err.Error()
			return manifest.Append(entry)
		}
		return finishFailedZip(manifest, startEntry, tempDir, options, statusNeedsReview, reportPath, report, err)
	}

	if err := cleanupTempExtract(tempDir, options.WorkDir); err != nil {
		return finishFailedZip(manifest, startEntry, tempDir, options, statusError, reportPath, report, err)
	}

	entry := startEntry
	entry.Status = statusSuccess
	entry.FinishedAt = time.Now().UTC()
	entry.ReportPath = reportPath
	entry.Summary = &report.Summary
	if err := manifest.Append(entry); err != nil {
		return err
	}
	fixer.Log(fixer.LoggerInfo, "Finished ZIP: %s", item.Name)
	return nil
}

func finishFailedZip(
	manifest *Manifest,
	entry ManifestEntry,
	tempDir string,
	options Options,
	status string,
	reportPath string,
	report *fixer.RunReport,
	runErr error,
) error {
	entry.Status = status
	entry.FinishedAt = time.Now().UTC()
	entry.ReportPath = reportPath
	if report != nil {
		entry.Summary = &report.Summary
	}
	if runErr != nil {
		entry.Error = runErr.Error()
	}
	if !options.KeepTempOnError {
		if cleanupErr := cleanupTempExtract(tempDir, options.WorkDir); cleanupErr != nil {
			entry.Error = strings.TrimSpace(entry.Error + "; cleanup temp: " + cleanupErr.Error())
		}
	}
	if err := manifest.Append(entry); err != nil {
		return err
	}
	if options.AskOnAmbiguous && options.Prompt != nil && askContinue(options, runErr) {
		return nil
	}
	return runErr
}

func reportNeedsReview(report *fixer.RunReport) bool {
	if report == nil {
		return true
	}
	return report.Summary.Unmatched > 0 || report.Summary.Ambiguous > 0 || report.Summary.Errors > 0
}

func loadLatestReport(outputDir string) (string, *fixer.RunReport, error) {
	reportDir := filepath.Join(outputDir, ".gtf", "reports")
	latestJSON := filepath.Join(reportDir, "latest.json")
	data, err := os.ReadFile(latestJSON)
	if err != nil {
		return "", nil, err
	}
	var report fixer.RunReport
	if err := json.Unmarshal(data, &report); err != nil {
		return "", nil, err
	}

	reportPath := filepath.Join(reportDir, "latest.txt")
	if timestamped, ok := newestReportJSON(reportDir); ok {
		textPath := strings.TrimSuffix(timestamped, ".json") + ".txt"
		if _, err := os.Stat(textPath); err == nil {
			reportPath = textPath
		} else {
			reportPath = timestamped
		}
	}
	return reportPath, &report, nil
}

func newestReportJSON(reportDir string) (string, bool) {
	entries, err := os.ReadDir(reportDir)
	if err != nil {
		return "", false
	}
	type reportFile struct {
		path    string
		modTime time.Time
	}
	var reports []reportFile
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "latest.json" || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		reports = append(reports, reportFile{
			path:    filepath.Join(reportDir, entry.Name()),
			modTime: info.ModTime(),
		})
	}
	sort.SliceStable(reports, func(i int, j int) bool {
		return reports[i].modTime.After(reports[j].modTime)
	})
	if len(reports) == 0 {
		return "", false
	}
	return reports[0].path, true
}

func resolveOutputDir(options Options, drives []DriveInfo) (string, error) {
	if strings.TrimSpace(options.OutputDir) != "" {
		return filepath.Abs(options.OutputDir)
	}
	if !options.AutoDrives {
		return "", fmt.Errorf("output folder is required")
	}
	if drive, ok, err := SelectOutputDrive(drives); err != nil {
		return "", err
	} else if ok {
		return filepath.Join(drive.Root, defaultOutputSubdir), nil
	}
	if options.AskOnAmbiguous && options.Prompt != nil {
		drive, err := promptDrive(options, "Choose final output drive", drives, false)
		if err != nil {
			return "", err
		}
		return filepath.Join(drive.Root, defaultOutputSubdir), nil
	}
	return "", fmt.Errorf("cannot choose final output drive; pass --output or use --ask-on-ambiguous")
}

func resolveZipRoots(options Options, drives []DriveInfo) ([]string, error) {
	if len(options.ZipRoots) > 0 {
		return absPaths(options.ZipRoots)
	}
	if !options.AutoDrives {
		return nil, fmt.Errorf("at least one --zip-root is required")
	}

	candidates := drivesExceptOutput(drives, options.OutputDir)
	if len(candidates) == 1 {
		return []string{candidates[0].Root}, nil
	}
	if options.AskOnAmbiguous && options.Prompt != nil {
		chosen, err := promptDrives(options, "Choose ZIP storage drive(s)", candidates, true)
		if err != nil {
			return nil, err
		}
		roots := make([]string, 0, len(chosen))
		for _, drive := range chosen {
			roots = append(roots, drive.Root)
		}
		return roots, nil
	}
	return nil, fmt.Errorf("cannot choose ZIP storage drive; pass one or more --zip-root values or use --ask-on-ambiguous")
}

func resolveWorkDir(options Options, drives []DriveInfo, zips []ZipItem) (string, error) {
	if strings.TrimSpace(options.WorkDir) != "" {
		return filepath.Abs(options.WorkDir)
	}
	if options.ProcessOptions.DryRun && !options.AutoDrives && !options.AskOnAmbiguous {
		return "", nil
	}

	requiredBytes := maxRequiredWorkBytes(zips, options.SafetyMarginBytes)
	if options.AutoDrives {
		if drive, ok := SelectWorkDrive(drives, requiredBytes, options.OutputDir); ok {
			return filepath.Join(drive.Root, defaultWorkSubdir), nil
		}
	}
	if options.AskOnAmbiguous && options.Prompt != nil {
		candidates := drivesWithSpace(drivesExceptOutput(drives, options.OutputDir), requiredBytes)
		drive, err := promptDrive(options, "Choose temporary work drive", candidates, false)
		if err != nil {
			return "", err
		}
		return filepath.Join(drive.Root, defaultWorkSubdir), nil
	}
	return "", fmt.Errorf("cannot choose temporary work folder with %s free; pass --work or use --ask-on-ambiguous",
		fixer.FormatBytes(requiredBytes),
	)
}

func drivesExceptOutput(drives []DriveInfo, outputDir string) []DriveInfo {
	outputRoot := DriveRoot(outputDir)
	candidates := make([]DriveInfo, 0, len(drives))
	for _, drive := range drives {
		if outputRoot != "" && strings.EqualFold(drive.Letter, outputRoot) {
			continue
		}
		candidates = append(candidates, drive)
	}
	return candidates
}

func drivesWithSpace(drives []DriveInfo, requiredBytes int64) []DriveInfo {
	candidates := make([]DriveInfo, 0, len(drives))
	for _, drive := range drives {
		if requiredBytes <= 0 || drive.FreeBytes >= requiredBytes {
			candidates = append(candidates, drive)
		}
	}
	return candidates
}

func absPaths(paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		out = append(out, abs)
	}
	return out, nil
}

func promptDrive(options Options, question string, drives []DriveInfo, allowMultiple bool) (DriveInfo, error) {
	chosen, err := promptDrives(options, question, drives, allowMultiple)
	if err != nil {
		return DriveInfo{}, err
	}
	if len(chosen) == 0 {
		return DriveInfo{}, fmt.Errorf("no drive selected")
	}
	return chosen[0], nil
}

func promptDrives(options Options, question string, drives []DriveInfo, allowMultiple bool) ([]DriveInfo, error) {
	if len(drives) == 0 {
		return nil, fmt.Errorf("no drive choices available")
	}
	choices := make([]string, 0, len(drives))
	for index, drive := range drives {
		choices = append(choices, fmt.Sprintf("%d. %s", index+1, FormatDrive(drive)))
	}
	answer, err := options.Prompt(question, choices, allowMultiple)
	if err != nil {
		return nil, err
	}
	return parseDriveChoices(answer, drives, allowMultiple)
}

func parseDriveChoices(answer string, drives []DriveInfo, allowMultiple bool) ([]DriveInfo, error) {
	fields := splitChoiceAnswer(answer)
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty choice")
	}
	if !allowMultiple && len(fields) > 1 {
		return nil, fmt.Errorf("choose one drive")
	}
	chosen := make([]DriveInfo, 0, len(fields))
	seen := make(map[int]struct{})
	for _, field := range fields {
		index, err := parseDriveChoice(field, drives)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[index]; ok {
			continue
		}
		seen[index] = struct{}{}
		chosen = append(chosen, drives[index])
	}
	return chosen, nil
}

func splitChoiceAnswer(answer string) []string {
	parts := strings.FieldsFunc(answer, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseDriveChoice(value string, drives []DriveInfo) (int, error) {
	if index, err := strconv.Atoi(value); err == nil {
		if index < 1 || index > len(drives) {
			return 0, fmt.Errorf("drive choice %d out of range", index)
		}
		return index - 1, nil
	}
	normalized := strings.ToUpper(strings.TrimSuffix(value, string(filepath.Separator)))
	for index, drive := range drives {
		if strings.EqualFold(normalized, drive.Letter) || strings.EqualFold(normalized, strings.TrimSuffix(drive.Root, string(filepath.Separator))) {
			return index, nil
		}
	}
	return 0, fmt.Errorf("unknown drive choice %q", value)
}

func askContinue(options Options, reason error) bool {
	if !options.AskOnAmbiguous || options.Prompt == nil {
		return false
	}
	answer, err := options.Prompt(reason.Error()+" Continue with next ZIP?", []string{"no", "yes"}, false)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(answer), "yes") || strings.EqualFold(strings.TrimSpace(answer), "y") || strings.TrimSpace(answer) == "2"
}
