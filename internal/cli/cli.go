/*
Google Photos Takeout Helper - A tool to clean and organize Google Photos Takeout exports
Copyright (C) 2026 feloex

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Terru03/google-photos-takeout-helper/internal/batch"
	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
	version "github.com/Terru03/google-photos-takeout-helper/internal/version"
)

type stringListFlag []string

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func Main() {
	defer fixer.RecoverPanic("cli-main")

	// Handle logs from the fixer package by printing them
	fixer.SetLogHandler(func(level fixer.LogLevel, message string) {
		fmt.Printf("[%s] %s\n", level, message)
	})

	defaults := fixer.DefaultProcessOptions()

	// Command-line flags
	showVersion := flag.Bool("version", false, "Show current version")
	inputPath := flag.String("input", "", "Path to Google takeout directory")
	outputPath := flag.String("output", "", "Path to output directory")
	batchZips := flag.Bool("batch-zips", false, "Process huge Google Takeout ZIP exports one ZIP at a time")
	oneZip := flag.String("one-zip", "", "Process one Takeout ZIP for a debug or test run")
	var zipRoots stringListFlag
	flag.Var(&zipRoots, "zip-root", "Path to a Takeout ZIP file or folder containing Takeout ZIP files; may be repeated")
	var skipZipNames stringListFlag
	flag.Var(&skipZipNames, "skip-zip", "ZIP name or full path to index for sidecars but skip during extract; may be repeated")
	var workPaths stringListFlag
	flag.Var(&workPaths, "work", "Temporary work folder used for extracting one ZIP at a time; may be repeated")
	workPool := flag.String("work-pool", "", "Semicolon-separated temporary work folders used when repeated --work flags are not convenient")
	stagingOutput := flag.String("staging-output", "", "SSD folder for this ZIP output before commit to final output")
	autoDrives := flag.Bool("auto-drives", false, "Scan Windows drives and choose safe defaults where clear")
	askOnAmbiguous := flag.Bool("ask-on-ambiguous", false, "Ask before choosing ambiguous drives or continuing after a problem report")
	keepTempOnError := flag.Bool("keep-temp-on-error", false, "Keep temporary extracted files when a ZIP fails or needs review")
	keepLiveVideo := flag.Bool("keep-live-video", false, "Keep standalone live-video files after MotionPhoto2 embeds them")
	preflightOnly := flag.Bool("preflight-only", false, "Scan batch ZIPs and print space/path warnings without extracting or processing")
	reprocessBatch := flag.Bool("reprocess", false, "Reprocess ZIPs even if the batch manifest already marks them successful")
	profileValue := flag.String("profile", "", "Output profile: recommended-safe, audit-only, immich")
	albumModeValue := flag.String("album-mode", "", "Album output mode: unique-only, timeline-only, all")
	timelineOnly := flag.Bool("timeline-only", false, "Build main timeline only from Date Taken")
	albumsSeparate := flag.Bool("albums-separate", false, "Keep albums under output\\Albums")
	useSymlinks := flag.Bool("symlink", false, "Use symlinks inside of albums instead of duplicating images")
	skipMetadata := flag.Bool("skip-metadata", !defaults.WriteMetadata, "Skip writing metadata to files")
	metadataModeValue := flag.String("metadata-mode", "", "Metadata output mode: file, xmp, both, none")
	ignoreAlbums := flag.Bool("ignore-albums", false, "Ignore all album folders")
	monthSubfolders := flag.Bool("month-subfolders", false, "Create month subfolders like \"1 - January\" inside year folders")
	flatten := flag.Bool("flatten", false, "Put all media files directly in the output folder without year/album subfolders")
	createMotionPhotos := flag.Bool("motion-photos", false, "Create Windows-viewable Samsung/Google Motion Photos with MotionPhoto2 after processing")
	mergeMotionPass := flag.Bool("merge-motion-pass", false, "Run only motion photo merge on finished library")
	libraryPath := flag.String("library", "", "Finished library path for --merge-motion-pass")
	motionPhotoTool := flag.String("motionphoto2", "", "Path to motionphoto2 executable")
	retryFailedMotion := flag.Bool("retry-failed-motion", false, "Retry failed and timed-out motion photo pairs")
	motionMergeTimeout := flag.Duration("motion-merge-timeout", 2*time.Minute, "Timeout for one motion photo pair")
	deleteSource := flag.Bool("delete-source", false, "Delete the original input folder after a fully clean run with zero unmatched, ambiguous, or error records")
	restoreMOV := flag.Bool("restore-mov", defaults.RestoreMOVExtension, "Restore .MOV file extension in case the Major Brand EXIF field says \"Apple QuickTime (.MOV/QT)\"")
	dryRun := flag.Bool("dry-run", false, "Plan the run and generate an audit report without writing files")
	verifyWrites := flag.Bool("verify", defaults.VerifyWrites, "Verify written metadata by reading it back with ExifTool")
	noDeduplicate := flag.Bool("no-deduplicate", !defaults.Deduplicate, "Keep duplicate files instead of linking or reusing exact matches")
	conflictPolicyValue := flag.String("conflict-policy", string(defaults.ConflictPolicy), "How to handle conflicts between embedded metadata and Takeout JSON: prefer-json, prefer-embedded, merge")
	verbose := flag.Bool("verbose", false, "Write per-file diagnostic logs, including selected date source")
	debug := flag.Bool("debug", false, "Alias for --verbose")

	flag.Parse()
	keepLiveVideoProvided := boolFlagProvided("keep-live-video")

	if *showVersion {
		fmt.Println(version.Tag)
		return
	}
	if *timelineOnly && *albumsSeparate {
		fmt.Println("Error: --timeline-only and --albums-separate cannot be used together")
		os.Exit(1)
	}

	if *flatten && *useSymlinks {
		fmt.Println("Error: --flatten and --symlink cannot be used together")
		os.Exit(1)
	}
	if *flatten && *ignoreAlbums {
		fmt.Println("Error: --flatten and --ignore-albums cannot be used together")
		os.Exit(1)
	}
	if *flatten && *monthSubfolders {
		fmt.Println("Error: --flatten and --month-subfolders cannot be used together")
		os.Exit(1)
	}
	if *useSymlinks && *ignoreAlbums {
		fmt.Println("Error: --symlink and --ignore-albums cannot be used together")
		os.Exit(1)
	}

	conflictPolicy, err := fixer.ParseConflictPolicy(*conflictPolicyValue)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	options := fixer.ProcessOptions{
		UseSymlinks:              *useSymlinks,
		WriteMetadata:            !*skipMetadata,
		WriteXMPSidecars:         false,
		AlbumMode:                defaults.AlbumMode,
		Flatten:                  *flatten,
		IgnoreAlbums:             *ignoreAlbums,
		MonthSubfolders:          *monthSubfolders,
		CreateMotionPhotos:       *createMotionPhotos,
		KeepLiveVideo:            *keepLiveVideo,
		DeleteSourceAfterSuccess: *deleteSource,
		RestoreMOVExtension:      *restoreMOV,
		Deduplicate:              !*noDeduplicate,
		DryRun:                   *dryRun,
		VerifyWrites:             *verifyWrites,
		ConflictPolicy:           conflictPolicy,
		Verbose:                  *verbose || *debug,
	}
	if strings.TrimSpace(*profileValue) != "" {
		if err := applyOutputProfile(*profileValue, &options); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	}
	if strings.TrimSpace(*albumModeValue) != "" {
		mode, err := fixer.ParseAlbumMode(strings.ToLower(strings.TrimSpace(*albumModeValue)))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		options.AlbumMode = mode
	} else if *ignoreAlbums {
		options.AlbumMode = fixer.AlbumModeTimelineOnly
	}
	if *timelineOnly {
		options.AlbumMode = fixer.AlbumModeTimelineOnly
		options.MonthSubfolders = true
	}
	if *albumsSeparate {
		options.AlbumMode = fixer.AlbumModeAll
		options.MonthSubfolders = true
	}
	if strings.TrimSpace(*metadataModeValue) != "" {
		mode, err := fixer.ParseMetadataOutputMode(strings.ToLower(strings.TrimSpace(*metadataModeValue)))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		options.WriteMetadata = mode.WritesFiles()
		options.WriteXMPSidecars = mode.WritesXMPSidecars()
		if !options.WriteMetadata {
			options.VerifyWrites = false
		}
	}
	options = options.Normalized()
	if options.Flatten && options.AlbumMode != fixer.AlbumModeAll {
		fmt.Println("Error: --flatten can only be used with --album-mode all")
		os.Exit(1)
	}

	if *mergeMotionPass {
		library := strings.TrimSpace(*libraryPath)
		if library == "" {
			library = strings.TrimSpace(*inputPath)
		}
		if library == "" {
			library = strings.TrimSpace(*outputPath)
		}
		if library == "" {
			fmt.Println("Error: --library is required for --merge-motion-pass")
			os.Exit(1)
		}
		runMotionMergeMode(library, *motionPhotoTool, *retryFailedMotion, *motionMergeTimeout)
		return
	}

	if strings.TrimSpace(*oneZip) != "" {
		*batchZips = true
		zipRoots = stringListFlag{strings.TrimSpace(*oneZip)}
	}
	if *batchZips {
		if strings.TrimSpace(*albumModeValue) == "" && !*timelineOnly && !*albumsSeparate && !*ignoreAlbums {
			options.AlbumMode = fixer.AlbumModeTimelineOnly
			options.MonthSubfolders = true
		}
		workDirs := ParseWorkRoots([]string(workPaths), *workPool)
		if !keepLiveVideoProvided {
			options.KeepLiveVideo = true
		}
		runBatchZIPMode(batch.Options{
			ZipRoots:           []string(zipRoots),
			SkipZipNames:       []string(skipZipNames),
			WorkDir:            firstCLIWorkRoot(workDirs),
			WorkDirs:           workDirs,
			OutputDir:          *outputPath,
			StagingOutputDir:   *stagingOutput,
			MotionToolPath:     *motionPhotoTool,
			RetryFailedMotion:  *retryFailedMotion,
			MotionMergeTimeout: *motionMergeTimeout,
			AutoDrives:         *autoDrives,
			AskOnAmbiguous:     *askOnAmbiguous || *autoDrives,
			KeepTempOnError:    *keepTempOnError,
			PreflightOnly:      *preflightOnly,
			Reprocess:          *reprocessBatch,
			ProcessOptions:     options,
			Prompt:             cliPrompt,
		})
		return
	}

	if *inputPath == "" || *outputPath == "" {
		fmt.Println("Error: --input and --output are required")
		flag.Usage()
		os.Exit(1)
	}

	progressCh := make(chan fixer.Progress)

	if err := fixer.ValidateProcessPaths(*inputPath, *outputPath); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	if exifInfo, err := fixer.ValidateProcessingDependencies(options); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	} else if exifInfo != nil {
		fmt.Printf("Using ExifTool %s from %s\n", exifInfo.Version, exifInfo.Path)
	}
	errCh := make(chan error, 1)
	fixer.SafeGo("cli-process", func() {
		errCh <- fixer.Process(context.Background(), *inputPath, *outputPath, progressCh, options)
	})

	lastProgress := time.Time{}
	for p := range progressCh {
		if p.Processed == 0 {
			continue
		}
		if time.Since(lastProgress) < 250*time.Millisecond && p.Processed < p.Total {
			continue
		}
		lastProgress = time.Now()

		percentageFinished := math.Round(float64(p.Processed) / float64(p.Total) * 100)

		fmt.Printf("[%3.0f%%] %2d/%2d - %s\n",
			percentageFinished,
			p.Processed,
			p.Total,
			filepath.Base(p.Current),
		)
	}
	if err := <-errCh; err != nil {
		fmt.Printf("Error during processing: %v\n", err)
		os.Exit(1)
	}
	if options.CreateMotionPhotos {
		runMotionMergeMode(*outputPath, *motionPhotoTool, *retryFailedMotion, *motionMergeTimeout)
		return
	}

	fmt.Println("\nDone")
}

func runMotionMergeMode(library string, toolPath string, retryFailed bool, timeout time.Duration) {
	report, err := fixer.MergeMotionLibrary(context.Background(), fixer.MotionMergeOptions{
		LibraryRoot: library,
		ToolPath:    toolPath,
		RetryFailed: retryFailed,
		Timeout:     timeout,
		Progress: func(progress fixer.MotionMergeProgress) {
			fmt.Printf(
				"Motion %d/%d %s - %s\n",
				progress.Processed,
				progress.Total,
				progress.Status,
				filepath.Base(progress.StillPath),
			)
		},
	})
	if err != nil {
		fmt.Printf("Error during motion merge: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf(
		"Motion merge done: candidates=%d merged=%d already=%d failed=%d timed-out=%d\n",
		report.TotalCandidatePairs,
		report.MergedSuccessfully,
		report.SkippedAlreadyMerged,
		report.FailedMotionPhotoCalls,
		report.TimedOutMerges,
	)
	fmt.Printf("Motion report: %s\n", report.ReportPath)
}

func ParseWorkRoots(workFlags []string, workPool string) []string {
	roots := make([]string, 0, len(workFlags))
	for _, value := range workFlags {
		value = strings.TrimSpace(value)
		if value != "" {
			roots = append(roots, value)
		}
	}
	for _, value := range strings.Split(workPool, ";") {
		value = strings.TrimSpace(value)
		if value != "" {
			roots = append(roots, value)
		}
	}
	return roots
}

func firstCLIWorkRoot(workDirs []string) string {
	for _, workDir := range workDirs {
		if strings.TrimSpace(workDir) != "" {
			return workDir
		}
	}
	return ""
}

func runBatchZIPMode(options batch.Options) {
	if options.ProcessOptions.DeleteSourceAfterSuccess {
		fmt.Println("Batch ZIP mode ignores --delete-source because ZIP sources must never be deleted")
		options.ProcessOptions.DeleteSourceAfterSuccess = false
	}

	if !options.ProcessOptions.DryRun {
		if exifInfo, err := fixer.ValidateProcessingDependencies(options.ProcessOptions); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		} else if exifInfo != nil {
			fmt.Printf("Using ExifTool %s from %s\n", exifInfo.Version, exifInfo.Path)
		}
	}

	result, err := batch.Run(context.Background(), options)
	if err != nil {
		printAlbumCleanupSummary(result)
		fmt.Printf("Error during batch ZIP processing: %v\n", err)
		os.Exit(1)
	}
	if result.Preflight != nil {
		fmt.Println(batch.FormatPreflightReport(*result.Preflight))
		return
	}

	fmt.Printf("\nBatch done\n")
	fmt.Printf("ZIPs found: %d\n", result.ZipCount)
	fmt.Printf("Processed: %d\n", result.Processed+result.CompletedWithReview)
	fmt.Printf("Completed clean: %d\n", result.Processed)
	fmt.Printf("Completed with review: %d\n", result.CompletedWithReview)
	fmt.Printf("Skipped: %d\n", result.Skipped)
	fmt.Printf("Failed: %d\n", result.Failed)
	fmt.Printf("Planned: %d\n", result.Planned)
	fmt.Printf("Output: %s\n", result.OutputDir)
	if result.StagingOutputDir != "" {
		fmt.Printf("Staging output: %s\n", result.StagingOutputDir)
	}
	if len(result.WorkDirs) > 0 {
		fmt.Printf("Work roots:\n")
		for _, workDir := range result.WorkDirs {
			fmt.Printf("  - %s\n", workDir)
		}
	} else if result.WorkDir != "" {
		fmt.Printf("Work: %s\n", result.WorkDir)
	}
	printAlbumCleanupSummary(result)
	if result.MotionMerge != nil {
		fmt.Printf(
			"Motion merge: candidates=%d merged=%d failed=%d timed-out=%d\n",
			result.MotionMerge.TotalCandidatePairs,
			result.MotionMerge.MergedSuccessfully,
			result.MotionMerge.FailedMotionPhotoCalls,
			result.MotionMerge.TimedOutMerges,
		)
		fmt.Printf("Motion report: %s\n", result.MotionMerge.ReportPath)
	}
	fmt.Printf("Manifest: %s\n", result.ManifestPath)
}

func printAlbumCleanupSummary(result batch.Result) {
	if result.AlbumCleanup == nil || !result.AlbumCleanup.Enabled {
		return
	}
	fmt.Printf("Album cleanup: %s, removed %d duplicate album file(s), removed %d empty folder(s)\n",
		result.AlbumCleanup.Status,
		result.AlbumCleanup.DuplicateFilesRemoved,
		result.AlbumCleanup.EmptyDirsRemoved,
	)
	if result.AlbumCleanup.ReportPath != "" {
		fmt.Printf("Album cleanup report: %s\n", result.AlbumCleanup.ReportPath)
	}
}

func boolFlagProvided(name string) bool {
	provided := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			provided = true
		}
	})
	return provided
}

func applyOutputProfile(name string, options *fixer.ProcessOptions) error {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "recommended-safe", "safe", "recommended":
		options.WriteMetadata = true
		options.WriteXMPSidecars = false
		options.VerifyWrites = true
		options.RestoreMOVExtension = true
		options.Deduplicate = true
		options.DryRun = false
		options.DeleteSourceAfterSuccess = false
		options.ConflictPolicy = fixer.ConflictMerge
		options.AlbumMode = fixer.AlbumModeUniqueOnly
	case "audit-only", "audit", "dry-run":
		options.WriteMetadata = false
		options.WriteXMPSidecars = false
		options.VerifyWrites = false
		options.RestoreMOVExtension = false
		options.Deduplicate = true
		options.DryRun = true
		options.DeleteSourceAfterSuccess = false
		options.AlbumMode = fixer.AlbumModeUniqueOnly
	case "immich", "immich-ready":
		options.WriteMetadata = true
		options.WriteXMPSidecars = true
		options.VerifyWrites = true
		options.RestoreMOVExtension = true
		options.Deduplicate = true
		options.KeepLiveVideo = true
		options.DryRun = false
		options.DeleteSourceAfterSuccess = false
		options.ConflictPolicy = fixer.ConflictMerge
		options.AlbumMode = fixer.AlbumModeUniqueOnly
	default:
		return fmt.Errorf("unknown profile %q", name)
	}
	return nil
}

func cliPrompt(question string, choices []string, allowMultiple bool) (string, error) {
	fmt.Println(question)
	for _, choice := range choices {
		fmt.Println(choice)
	}
	if allowMultiple {
		fmt.Print("Enter number, drive letter, or comma list: ")
	} else {
		fmt.Print("Enter number or drive letter: ")
	}
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(answer), nil
}
