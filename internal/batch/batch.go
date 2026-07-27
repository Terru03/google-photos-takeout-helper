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

	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
)

func Run(ctx context.Context, options Options) (Result, error) {
	options = normalizeOptions(options)
	if err := validateProcessingDependencies(options); err != nil {
		return Result{}, err
	}

	if options.PreflightOnly {
		report, err := Preflight(options)
		result := Result{
			ManifestPath:     report.ManifestPath,
			OutputDir:        report.OutputDir,
			StagingOutputDir: report.StagingOutputDir,
			WorkDir:          report.WorkDir,
			WorkDirs:         report.WorkDirs,
			ZipCount:         report.ZipCount,
			Preflight:        &report,
		}
		return result, err
	}

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

	workDirs, err := resolveWorkDirs(options, drives, zips)
	if err != nil {
		return Result{}, err
	}
	options.WorkDirs = workDirs
	options.WorkDir = firstWorkDir(workDirs)

	if err := ValidateBatchPathSetWithStaging(options.ZipRoots, options.WorkDirs, options.OutputDir, options.StagingOutputDir, false); err != nil {
		return Result{}, err
	}
	if err := ensureWorkRoots(options.WorkDirs); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(options.StagingOutputDir) != "" {
		stagingAbs, err := filepath.Abs(options.StagingOutputDir)
		if err != nil {
			return Result{}, err
		}
		options.StagingOutputDir = stagingAbs
		if err := os.MkdirAll(options.StagingOutputDir, 0o755); err != nil {
			return Result{}, fmt.Errorf("create staging output folder: %w", err)
		}
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
		ManifestPath:     manifest.Path(),
		OutputDir:        options.OutputDir,
		StagingOutputDir: options.StagingOutputDir,
		WorkDir:          options.WorkDir,
		WorkDirs:         options.WorkDirs,
		ZipCount:         len(zips),
	}
	if legacyCount := manifest.LegacySuccessfulCount(zips); legacyCount > 0 && !options.Reprocess {
		return result, fmt.Errorf(
			"output has %d ZIPs completed by older workflow; use clean output folder, move old output aside, or pass --reprocess only if mixing old and new files is wanted",
			legacyCount,
		)
	}
	if err := manifest.MarkInterrupted(zips); err != nil {
		return result, err
	}

	if len(zips) > 1 && options.ProcessOptions.SidecarIndex == nil {
		fixer.Log(fixer.LoggerInfo, "Index Google JSON sidecars across %d ZIP files", len(zips))
		sidecarIndex, warnings, indexErr := buildGlobalSidecarIndex(ctx, zips, func(completed int, total int, item ZipItem) {
			emitProgress(options, BatchProgress{
				CurrentZip:   item.Path,
				CurrentIndex: completed,
				Phase:        "index",
				Completed:    completed,
				Total:        total,
				ReportPath:   manifest.Path(),
			})
		})
		if indexErr != nil {
			return result, indexErr
		}
		options.ProcessOptions.SidecarIndex = sidecarIndex
		result.IndexedSidecars = sidecarIndex.Count()
		result.SidecarIndexWarnings = warnings
		fixer.Log(
			fixer.LoggerInfo,
			"JSON sidecar index ready: %d sidecars, %d warnings",
			result.IndexedSidecars,
			result.SidecarIndexWarnings,
		)
	}

	if err := os.MkdirAll(options.OutputDir, 0o755); err != nil {
		return result, err
	}
	for index, item := range zips {
		currentIndex := index + 1
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if shouldSkipZip(item, options.SkipZipNames) {
			result.Skipped++
			fixer.Log(fixer.LoggerWarn, "Skip requested ZIP: %s", item.Path)
			continue
		}
		if manifest.AlreadySuccessful(item) && !options.Reprocess {
			result.Skipped++
			fixer.Log(fixer.LoggerInfo, "Skip already processed ZIP: %s", item.Path)
			continue
		}

		emitProgress(options, BatchProgress{
			CurrentZip:   item.Path,
			CurrentIndex: currentIndex,
			Completed:    result.Processed + result.CompletedWithReview + result.Skipped + result.Failed,
			Total:        len(zips),
			ReportPath:   manifest.Path(),
		})

		completedBefore := result.Processed + result.CompletedWithReview + result.Skipped + result.Failed
		status, err := runOneZip(ctx, options, item, manifest, drives, currentIndex, len(zips), completedBefore)
		if err != nil {
			result.Failed++
			fixer.Log(fixer.LoggerError, "ZIP failed: %s: %v", item.Path, err)
			emitProgress(options, BatchProgress{
				CurrentZip:   item.Path,
				CurrentIndex: currentIndex,
				Completed:    result.Processed + result.CompletedWithReview + result.Skipped + result.Failed,
				Total:        len(zips),
				LatestError:  err.Error(),
				ReportPath:   manifest.Path(),
			})
		} else if status == statusCompletedReview {
			result.CompletedWithReview++
		} else {
			result.Processed++
		}
		emitProgress(options, BatchProgress{
			CurrentZip:   item.Path,
			CurrentIndex: currentIndex,
			Completed:    result.Processed + result.CompletedWithReview + result.Skipped + result.Failed,
			Total:        len(zips),
			ReportPath:   manifest.Path(),
			LatestError:  "",
		})
		if options.StopAfterCurrent != nil && options.StopAfterCurrent() {
			result.Stopped = true
			break
		}
	}

	if result.Stopped {
		return result, nil
	}
	if result.Failed > 0 {
		return result, fmt.Errorf("%d ZIP file(s) failed; rerun to retry failed or interrupted ZIPs", result.Failed)
	}
	if options.ProcessOptions.Normalized().AlbumMode == fixer.AlbumModeUniqueOnly {
		fixer.Log(fixer.LoggerInfo, "Album cleanup: remove exact duplicates already present in timeline")
		cleanup, cleanupErr := fixer.CleanupDuplicateAlbumFiles(options.OutputDir, options.ProcessOptions)
		result.AlbumCleanup = &cleanup
		if cleanupErr != nil {
			return result, fmt.Errorf("album cleanup failed: %w", cleanupErr)
		}
		fixer.Log(
			fixer.LoggerInfo,
			"Album cleanup finished: %d duplicate file(s), %d empty folder(s), report %s",
			cleanup.DuplicateFilesRemoved,
			cleanup.EmptyDirsRemoved,
			cleanup.ReportPath,
		)
	}
	if options.ProcessOptions.CreateMotionPhotos {
		fixer.Log(fixer.LoggerInfo, "Phase 2: merge motion photos in final library")
		mergeReport, mergeErr := fixer.MergeMotionLibrary(ctx, fixer.MotionMergeOptions{
			LibraryRoot: options.OutputDir,
			ToolPath:    options.MotionToolPath,
			RetryFailed: options.RetryFailedMotion,
			Timeout:     options.MotionMergeTimeout,
		})
		result.MotionMerge = &mergeReport
		if mergeErr != nil {
			return result, fmt.Errorf("motion merge pass: %w", mergeErr)
		}
		if mergeReport.FailedMotionPhotoCalls > 0 || mergeReport.TimedOutMerges > 0 {
			fixer.Log(
				fixer.LoggerWarn,
				"Motion merge finished with failures=%d timeouts=%d; report %s",
				mergeReport.FailedMotionPhotoCalls,
				mergeReport.TimedOutMerges,
				mergeReport.ReportPath,
			)
		}
	}
	return result, nil
}

func shouldSkipZip(item ZipItem, names []string) bool {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.EqualFold(filepath.Base(name), item.Name) ||
			strings.EqualFold(filepath.Clean(name), filepath.Clean(item.Path)) {
			return true
		}
	}
	return false
}

func validateProcessingDependencies(options Options) error {
	if _, err := fixer.ValidateProcessingDependencies(options.ProcessOptions); err != nil {
		return err
	}
	return nil
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

func runOneZip(
	ctx context.Context,
	options Options,
	item ZipItem,
	manifest *Manifest,
	drives []DriveInfo,
	currentIndex int,
	total int,
	completedBefore int,
) (string, error) {
	workRoot, err := chooseWorkRootForZip(item, options.WorkDirs, drives, options.SafetyMarginBytes)
	if err != nil {
		return "", err
	}

	tempDir, err := os.MkdirTemp(workRoot.Path, "gtf-zip-")
	if err != nil {
		return "", err
	}
	stagingRoot := ""
	if strings.TrimSpace(options.StagingOutputDir) != "" {
		stagingRoot, err = os.MkdirTemp(options.StagingOutputDir, "gtf-stage-")
		if err != nil {
			_ = cleanupTempExtract(tempDir, workRoot.Path)
			return "", fmt.Errorf("create ZIP staging output: %w", err)
		}
	}

	startedAt := time.Now().UTC()
	startEntry := manifestEntryFor(item, options.OutputDir, statusPending)
	startEntry.StartTime = startedAt
	startEntry.WorkRoot = workRoot.Path
	startEntry.ExtractedRoot = tempDir
	startEntry.StagingRoot = stagingRoot
	if err := manifest.Append(startEntry); err != nil {
		if stagingRoot != "" {
			_ = cleanupStagingRoot(stagingRoot, options.StagingOutputDir)
		}
		_ = cleanupTempExtract(tempDir, workRoot.Path)
		return "", err
	}
	emitProgress(options, BatchProgress{
		CurrentZip:   item.Path,
		CurrentIndex: currentIndex,
		Phase:        "extract",
		Completed:    completedBefore,
		Total:        total,
		ReportPath:   manifest.Path(),
		WorkRoot:     workRoot.Path,
	})

	fixer.Log(fixer.LoggerInfo, "Extract ZIP: %s", item.Path)
	extractEntry := startEntry
	extractEntry.Status = statusExtracting
	if err := manifest.Append(extractEntry); err != nil {
		return "", err
	}
	lastLog := time.Time{}
	lastExtractEmit := time.Time{}
	if err := ExtractZipWithProgress(item.Path, tempDir, func(progress ExtractProgress) {
		displayFile := ""
		shouldLog := time.Since(lastLog) >= 5*time.Second
		if shouldLog {
			lastLog = time.Now()
			displayFile = progress.CurrentFile
		}
		shouldEmit := time.Since(lastExtractEmit) >= 250*time.Millisecond ||
			(progress.TotalFiles > 0 && progress.ProcessedFiles >= progress.TotalFiles)
		if shouldEmit {
			lastExtractEmit = time.Now()
			emitProgress(options, BatchProgress{
				CurrentZip:    item.Path,
				CurrentIndex:  currentIndex,
				Phase:         "extract",
				Completed:     completedBefore,
				Total:         total,
				FileProcessed: progress.ProcessedFiles,
				FileTotal:     progress.TotalFiles,
				CurrentFile:   displayFile,
				CurrentBytes:  progress.CurrentBytes,
				TotalBytes:    progress.TotalBytes,
				ReportPath:    manifest.Path(),
				WorkRoot:      workRoot.Path,
			})
		}
		if shouldLog {
			if progress.TotalBytes > 0 && progress.CurrentBytes > 0 {
				fixer.Log(
					fixer.LoggerInfo,
					"Extract %s %d/%d %s (%s/%s)",
					item.Name,
					progress.ProcessedFiles,
					progress.TotalFiles,
					filepath.Base(progress.CurrentFile),
					fixer.FormatBytes(progress.CurrentBytes),
					fixer.FormatBytes(progress.TotalBytes),
				)
			} else {
				fixer.Log(
					fixer.LoggerInfo,
					"Extract %s %d/%d %s",
					item.Name,
					progress.ProcessedFiles,
					progress.TotalFiles,
					filepath.Base(progress.CurrentFile),
				)
			}
		}
	}); err != nil {
		return "", finishFailedZip(manifest, startEntry, options, "", nil, fmt.Errorf("extract ZIP: %w", err))
	}

	googlePhotosDir, err := LocateGooglePhotosFolder(tempDir)
	if err != nil {
		return "", finishFailedZip(manifest, startEntry, options, "", nil, err)
	}

	processingEntry := startEntry
	processingEntry.Status = statusProcessing
	processingEntry.GooglePhotosRoot = googlePhotosDir
	if err := manifest.Append(processingEntry); err != nil {
		return "", err
	}

	progressCh := make(chan fixer.Progress)
	errCh := make(chan error, 1)
	processOutputRoot := options.OutputDir
	processOptions := options.ProcessOptions
	processOptions.CreateMotionPhotos = false
	processOptions.SourceID = item.Fingerprint
	if stagingRoot != "" {
		processOutputRoot = stagingRoot
		processOptions.RuntimeRoot = options.OutputDir
		processOptions.FinalOutputRoot = options.OutputDir
	}
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				safeCloseProgress(progressCh)
				errCh <- fmt.Errorf("process panic: %v", recovered)
			}
		}()
		errCh <- options.Process(ctx, googlePhotosDir, processOutputRoot, progressCh, processOptions)
	}()
	lastProcessLog := time.Time{}
	lastProcessEmit := time.Time{}
	for progress := range progressCh {
		if progress.Total == 0 {
			continue
		}
		if time.Since(lastProcessLog) >= 5*time.Second || progress.Processed >= progress.Total {
			lastProcessLog = time.Now()
			fixer.Log(
				fixer.LoggerInfo,
				"ZIP %s progress %d/%d %s",
				item.Name,
				progress.Processed,
				progress.Total,
				filepath.Base(progress.Current),
			)
		}
		if time.Since(lastProcessEmit) >= 250*time.Millisecond || progress.Processed >= progress.Total {
			lastProcessEmit = time.Now()
			emitProgress(options, BatchProgress{
				CurrentZip:    item.Path,
				CurrentIndex:  currentIndex,
				Phase:         "process",
				Completed:     completedBefore,
				Total:         total,
				FileProcessed: progress.Processed,
				FileTotal:     progress.Total,
				CurrentFile:   progress.Current,
				ReportPath:    manifest.Path(),
				WorkRoot:      workRoot.Path,
			})
		}
	}
	processErr := <-errCh

	reportPath, report, reportErr := loadLatestReport(options.OutputDir)
	if processErr != nil {
		return "", finishFailedZip(manifest, processingEntry, options, reportPath, report, processErr)
	}
	if reportErr != nil {
		return "", finishFailedZip(manifest, processingEntry, options, reportPath, report, reportErr)
	}
	if stagingRoot != "" {
		fixer.Log(fixer.LoggerInfo, "Commit staged ZIP output: %s -> %s", stagingRoot, options.OutputDir)
		emitProgress(options, BatchProgress{
			CurrentZip:   item.Path,
			CurrentIndex: currentIndex,
			Phase:        "commit",
			Completed:    completedBefore,
			Total:        total,
			ReportPath:   manifest.Path(),
			WorkRoot:     workRoot.Path,
		})
		if err := CommitStagedReport(ctx, stagingRoot, options.OutputDir, report, options.ProcessOptions.UseSymlinks); err != nil {
			return "", finishFailedZip(manifest, processingEntry, options, reportPath, report, fmt.Errorf("commit staged output: %w", err))
		}
		if err := cleanupStagingRoot(stagingRoot, options.StagingOutputDir); err != nil {
			return "", finishFailedZip(manifest, processingEntry, options, reportPath, report, fmt.Errorf("clean staging output: %w", err))
		}
	}

	if err := cleanupTempExtract(tempDir, workRoot.Path); err != nil {
		return "", finishFailedZip(manifest, processingEntry, options, reportPath, report, err)
	}

	entry := processingEntry
	entry.Status = completedStatusForReport(report)
	entry.EndTime = time.Now().UTC()
	entry.ReportPath = reportPath
	entry.Summary = &report.Summary
	if err := manifest.Append(entry); err != nil {
		return "", err
	}
	if entry.Status == statusCompletedReview {
		fixer.Log(fixer.LoggerWarn, "Finished ZIP with review items: %s", item.Name)
	} else {
		fixer.Log(fixer.LoggerInfo, "Finished ZIP: %s", item.Name)
	}
	return entry.Status, nil
}

func finishFailedZip(
	manifest *Manifest,
	entry ManifestEntry,
	options Options,
	reportPath string,
	report *fixer.RunReport,
	runErr error,
) error {
	failedFromStatus := entry.Status
	entry.Status = statusFailed
	entry.EndTime = time.Now().UTC()
	entry.ReportPath = reportPath
	if report != nil {
		entry.Summary = &report.Summary
	}
	if runErr != nil {
		entry.Error = runErr.Error()
	}
	if entry.ExtractedRoot != "" {
		if options.KeepTempOnError {
			entry.Error = strings.TrimSpace(entry.Error + "; temp kept at " + entry.ExtractedRoot)
		} else if entry.WorkRoot != "" {
			if cleanupErr := cleanupTempExtract(entry.ExtractedRoot, entry.WorkRoot); cleanupErr != nil {
				entry.Error = strings.TrimSpace(entry.Error + "; temp cleanup failed: " + cleanupErr.Error())
			}
		}
	}
	if failedFromStatus == statusPending && entry.StagingRoot != "" && options.StagingOutputDir != "" {
		if cleanupErr := cleanupStagingRoot(entry.StagingRoot, options.StagingOutputDir); cleanupErr != nil {
			entry.Error = strings.TrimSpace(entry.Error + "; staging cleanup failed: " + cleanupErr.Error())
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

func emitProgress(options Options, progress BatchProgress) {
	if options.Progress != nil {
		options.Progress(progress)
	}
}

func completedStatusForReport(report *fixer.RunReport) string {
	if reportNeedsReview(report) {
		return statusCompletedReview
	}
	return statusCompleted
}

func reportNeedsReview(report *fixer.RunReport) bool {
	if report == nil {
		return true
	}
	return report.Summary.Unmatched > 0 ||
		report.Summary.Ambiguous > 0 ||
		report.Summary.Errors > 0 ||
		report.Summary.SuspiciousDates > 0 ||
		report.Summary.ConflictsFound > 0 ||
		report.Summary.MetadataVerificationFailures > 0
}

func safeCloseProgress(progressCh chan<- fixer.Progress) {
	defer func() {
		_ = recover()
	}()
	close(progressCh)
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
