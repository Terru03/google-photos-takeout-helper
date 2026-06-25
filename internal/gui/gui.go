package gui

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/feloex/GoogleTakeoutFixer/internal/batch"
	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
	version "github.com/feloex/GoogleTakeoutFixer/internal/version"
	"github.com/ncruces/zenity"
)

func Main() {
	defer fixer.RecoverPanic("gui-main")

	defaults := fixer.DefaultProcessOptions()
	prefs, prefsErr := fixer.LoadPreferences()
	if prefs.Options != (fixer.ProcessOptions{}) {
		defaults = prefs.Options
	}

	inputPath := prefs.LastInputPath
	outputPath := prefs.LastOutputPath
	hugeTakeoutMode := false
	batchZipRoots := []string{}
	batchWorkPath := ""
	detectedDrives := []batch.DriveInfo{}
	keepTempOnError := true
	keepLiveVideo := true
	if prefs.Options != (fixer.ProcessOptions{}) {
		keepLiveVideo = defaults.KeepLiveVideo
	}
	preflightOnly := false

	a := app.New()
	a.SetIcon(resourceGoogleTakeoutFixerPng)
	w := a.NewWindow("GoogleTakeoutFixer " + version.Tag)
	w.Resize(fyne.NewSize(680, 560))

	var (
		useSymlinks              = defaults.UseSymlinks
		writeMetadata            = defaults.WriteMetadata
		flatten                  = defaults.Flatten
		ignoreAlbums             = defaults.IgnoreAlbums
		monthSubfolders          = defaults.MonthSubfolders
		createMotionPhotos       = defaults.CreateMotionPhotos
		keepLiveVideos           = keepLiveVideo
		deleteSourceAfterSuccess = defaults.DeleteSourceAfterSuccess
		restoreMOVExtension      = defaults.RestoreMOVExtension
		deduplicate              = defaults.Deduplicate
		dryRun                   = defaults.DryRun
		verifyWrites             = defaults.VerifyWrites
		conflictPolicy           = defaults.ConflictPolicy
	)

	if conflictPolicy == "" {
		conflictPolicy = fixer.ConflictMerge
	}

	uiReady := false
	var updateCheckboxStates func()

	currentOptions := func() fixer.ProcessOptions {
		return fixer.ProcessOptions{
			UseSymlinks:              useSymlinks,
			WriteMetadata:            writeMetadata,
			MonthSubfolders:          monthSubfolders,
			IgnoreAlbums:             ignoreAlbums,
			Flatten:                  flatten,
			CreateMotionPhotos:       createMotionPhotos,
			KeepLiveVideo:            keepLiveVideos,
			DeleteSourceAfterSuccess: deleteSourceAfterSuccess,
			RestoreMOVExtension:      restoreMOVExtension,
			Deduplicate:              deduplicate,
			DryRun:                   dryRun,
			VerifyWrites:             verifyWrites,
			ConflictPolicy:           conflictPolicy,
		}
	}

	savePreferences := func() {
		if err := fixer.SavePreferences(fixer.Preferences{
			LastInputPath:  inputPath,
			LastOutputPath: outputPath,
			Options:        currentOptions(),
		}); err != nil {
			fixer.Log(fixer.LoggerWarn, "Could not save preferences: %v", err)
		}
	}

	progressLabel := widget.NewLabel("Ready to start")
	progressLabel.Truncation = fyne.TextTruncateEllipsis
	progressBar := widget.NewProgressBar()
	selectionSummary := widget.NewLabel("")
	selectionSummary.Wrapping = fyne.TextWrapWord
	dependencyLabel := widget.NewLabel("")
	dependencyLabel.Wrapping = fyne.TextWrapWord
	hintLabel := widget.NewRichTextFromMarkdown("Recommended for a first run: keep `Write metadata`, `Deduplicate exact copies`, and `Verify metadata after write` enabled. Use `Dry run with audit report` first if you want a trust check before writing files.")
	hintLabel.Wrapping = fyne.TextWrapWord

	inputPathLabel := widget.NewLabel("not selected")
	inputPathLabel.Wrapping = fyne.TextWrapWord
	outputPathLabel := widget.NewLabel("not selected")
	outputPathLabel.Wrapping = fyne.TextWrapWord
	workPathLabel := widget.NewLabel("auto")
	workPathLabel.Wrapping = fyne.TextWrapWord
	batchCurrentZipLabel := widget.NewLabel("Current ZIP: none")
	batchCurrentZipLabel.Wrapping = fyne.TextWrapWord
	batchCountLabel := widget.NewLabel("ZIPs: 0 / 0")
	batchFileProgressLabel := widget.NewLabel("Current file: none")
	batchFileProgressLabel.Wrapping = fyne.TextWrapWord
	batchErrorLabel := widget.NewLabel("Latest error: none")
	batchErrorLabel.Wrapping = fyne.TextWrapWord
	batchReportLabel := widget.NewLabel("Report: not ready")
	batchReportLabel.Wrapping = fyne.TextWrapWord
	batchSourcesBox := container.NewVBox()
	driveChoicesBox := container.NewVBox()

	reportPath := ""
	var cancelFn context.CancelFunc
	var cancelButton *widget.Button
	var openOutputButton *widget.Button
	var openReportButton *widget.Button
	var startButton *widget.Button
	var preflightButton *widget.Button
	var stopAfterCurrentButton *widget.Button
	var inputButton *widget.Button
	var outputButton *widget.Button
	var hugeModeButton *widget.Button
	var addZipRootButton *widget.Button
	var addZipFileButton *widget.Button
	var workFolderButton *widget.Button
	var scanDrivesButton *widget.Button
	var scanSourcesButton *widget.Button
	var clearSourcesButton *widget.Button
	var recommendedButton *widget.Button
	var dryRunPresetButton *widget.Button
	var keepTempOnErrorCheckbox *widget.Check
	var keepLiveVideoCheckbox *widget.Check
	var preflightOnlyCheckbox *widget.Check

	logLabel := widget.NewLabel("Logs will appear here...")
	logLabel.Wrapping = fyne.TextWrapWord
	const maxVisibleLogLines = 300
	visibleLogLines := make([]string, 0, maxVisibleLogLines)

	logCh := make(chan string, 1000)
	fixer.SetLogHandler(func(level fixer.LogLevel, message string) {
		logCh <- fmt.Sprintf("[%s] %s", level, message)
	})

	var updateSelectionSummary func()
	var refreshDriveChoices func()

	setHugeMode := func(enabled bool) {
		hugeTakeoutMode = enabled
		if hugeModeButton != nil {
			if hugeTakeoutMode {
				hugeModeButton.SetText("Mode: Huge ZIP Batch")
			} else {
				hugeModeButton.SetText("Mode: Single Folder")
			}
		}
		if startButton != nil {
			if hugeTakeoutMode {
				startButton.SetText("Start Batch")
			} else {
				startButton.SetText("Start Processing")
			}
		}
	}

	hasBatchRoot := func(root string) bool {
		root = filepath.Clean(strings.TrimSpace(root))
		for _, existing := range batchZipRoots {
			if strings.EqualFold(filepath.Clean(existing), root) {
				return true
			}
		}
		return false
	}

	addBatchRoot := func(root string) {
		root = strings.TrimSpace(root)
		if root == "" || hasBatchRoot(root) {
			return
		}
		batchZipRoots = append(batchZipRoots, root)
		setHugeMode(true)
		fixer.Log(fixer.LoggerInfo, "Added source to scan: %s", root)
	}

	removeBatchRoot := func(root string) {
		root = filepath.Clean(strings.TrimSpace(root))
		filtered := batchZipRoots[:0]
		for _, existing := range batchZipRoots {
			if !strings.EqualFold(filepath.Clean(existing), root) {
				filtered = append(filtered, existing)
			}
		}
		batchZipRoots = filtered
	}

	var refreshBatchSources func()
	refreshBatchSources = func() {
		batchSourcesBox.Objects = nil
		if len(batchZipRoots) == 0 {
			empty := widget.NewLabel("No batch sources yet. Add one or more ZIP folders, ZIP files, or drives below.")
			empty.Wrapping = fyne.TextWrapWord
			batchSourcesBox.Add(empty)
		}
		for _, source := range batchZipRoots {
			source := source
			label := widget.NewLabel(source)
			label.Wrapping = fyne.TextWrapWord
			removeButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
				removeBatchRoot(source)
				refreshBatchSources()
				if refreshDriveChoices != nil {
					refreshDriveChoices()
				}
				if updateSelectionSummary != nil {
					updateSelectionSummary()
				}
			})
			removeButton.Importance = widget.LowImportance
			batchSourcesBox.Add(container.NewBorder(nil, nil, nil, removeButton, label))
		}
		batchSourcesBox.Refresh()
		if updateSelectionSummary != nil {
			updateSelectionSummary()
		}
	}

	refreshDriveChoices = func() {
		driveChoicesBox.Objects = nil
		if len(detectedDrives) == 0 {
			empty := widget.NewLabel("Click Scan This PC Drives to show drive choices here.")
			empty.Wrapping = fyne.TextWrapWord
			driveChoicesBox.Add(empty)
		}
		for _, drive := range detectedDrives {
			drive := drive
			check := widget.NewCheck(batch.FormatDrive(drive), func(checked bool) {
				if checked {
					addBatchRoot(drive.Root)
				} else {
					removeBatchRoot(drive.Root)
				}
				refreshBatchSources()
			})
			check.SetChecked(hasBatchRoot(drive.Root))
			driveChoicesBox.Add(check)
		}
		driveChoicesBox.Refresh()
	}

	updateSelectionSummary = func() {
		inputText := "not selected"
		if inputPath != "" {
			inputText = inputPath
		}
		outputText := "not selected"
		if outputPath != "" {
			outputText = outputPath
		}
		modeText := "Standard folder mode"
		if hugeTakeoutMode {
			zipText := "0 selected"
			if len(batchZipRoots) > 0 {
				zipText = fmt.Sprintf("%d selected", len(batchZipRoots))
			}
			workText := "auto"
			if batchWorkPath != "" {
				workText = batchWorkPath
			}
			modeText = fmt.Sprintf("Huge ZIP batch mode\nSources: %s\nWork: %s", zipText, workText)
		}
		selectionSummary.SetText(fmt.Sprintf("Mode: %s\nInput: %s\nOutput: %s", modeText, inputText, outputText))
		inputPathLabel.SetText(inputText)
		outputPathLabel.SetText(outputText)
		if batchWorkPath == "" {
			workPathLabel.SetText("auto: fastest detected drive with enough free space")
		} else {
			workPathLabel.SetText(batchWorkPath)
		}
	}

	updateDependencyStatus := func() {
		options := currentOptions()
		info, err := fixer.ValidateProcessingDependencies(options)
		switch {
		case err != nil:
			dependencyLabel.SetText("Dependency check: " + err.Error())
			return
		}

		motionInfo, motionErr := fixer.ValidateMotionPhotoDependencies(options)
		switch {
		case motionErr != nil:
			dependencyLabel.SetText("Dependency check: " + motionErr.Error())
		case info == nil && motionInfo == nil:
			dependencyLabel.SetText("Dependency check: no external tools needed for the current option set.")
		case info != nil && motionInfo != nil:
			dependencyLabel.SetText(fmt.Sprintf("Dependency check: ExifTool %s ready (%s) | MotionPhoto2 ready (%s)", info.Version, info.Path, motionInfo.Path))
		case info != nil:
			dependencyLabel.SetText(fmt.Sprintf("Dependency check: ExifTool %s ready (%s)", info.Version, info.Path))
		case motionInfo != nil:
			dependencyLabel.SetText(fmt.Sprintf("Dependency check: MotionPhoto2 ready (%s)", motionInfo.Path))
		}
	}

	openInExplorer := func(path string) {
		if path == "" {
			return
		}
		if err := exec.Command("explorer.exe", path).Start(); err != nil {
			fixer.Log(fixer.LoggerError, "Could not open %s: %v", path, err)
		}
	}

	setSuggestedOutput := func() {
		if inputPath == "" || outputPath != "" {
			return
		}
		outputPath = filepath.Join(filepath.Dir(inputPath), filepath.Base(inputPath)+" Fixed")
		outputButton.SetText("Output: " + filepath.Base(outputPath))
	}

	inputButton = widget.NewButtonWithIcon("Select Google Takeout Folder", theme.FolderOpenIcon(), func() {
		dir, err := zenity.SelectFile(zenity.Title("Select Google Takeout Folder"), zenity.Directory())
		if err != nil {
			return
		}
		inputPath = dir
		inputButton.SetText("Input: " + filepath.Base(inputPath))
		setSuggestedOutput()
		updateSelectionSummary()
		savePreferences()
	})

	outputButton = widget.NewButtonWithIcon("Select Output Folder", theme.FolderOpenIcon(), func() {
		dir, err := zenity.SelectFile(zenity.Title("Select Output Folder"), zenity.Directory())
		if err != nil {
			return
		}
		outputPath = dir
		outputButton.SetText("Output: " + filepath.Base(outputPath))
		updateSelectionSummary()
		savePreferences()
	})

	hugeModeButton = widget.NewButton("Mode: Single Folder", func() {
		setHugeMode(!hugeTakeoutMode)
		if hugeTakeoutMode {
			fixer.Log(fixer.LoggerWarn, "Huge ZIP batch mode scans only the sources you select. ZIP files are kept and final output stays unzipped.")
		}
		updateSelectionSummary()
	})

	addZipRootButton = widget.NewButtonWithIcon("Add Folder", theme.FolderOpenIcon(), func() {
		dir, err := zenity.SelectFile(zenity.Title("Select Folder With Takeout ZIPs"), zenity.Directory())
		if err != nil {
			return
		}
		addBatchRoot(dir)
		refreshBatchSources()
		if refreshDriveChoices != nil {
			refreshDriveChoices()
		}
	})

	addZipFileButton = widget.NewButtonWithIcon("Add ZIP File", theme.FileIcon(), func() {
		files, err := zenity.SelectFileMultiple(
			zenity.Title("Select Takeout ZIP File(s)"),
			zenity.FileFilters{
				{"ZIP files", []string{"*.zip"}, false},
			},
		)
		if err != nil {
			return
		}
		for _, file := range files {
			addBatchRoot(file)
		}
		refreshBatchSources()
	})

	workFolderButton = widget.NewButtonWithIcon("Select Work Folder", theme.FolderOpenIcon(), func() {
		dir, err := zenity.SelectFile(zenity.Title("Select Temporary Work Folder"), zenity.Directory())
		if err != nil {
			return
		}
		batchWorkPath = dir
		setHugeMode(true)
		fixer.Log(fixer.LoggerInfo, "Work folder: %s", dir)
		updateSelectionSummary()
	})

	scanDrivesButton = widget.NewButton("Scan This PC Drives", func() {
		drives, err := batch.DetectDrives()
		if err != nil {
			fixer.Log(fixer.LoggerError, "Drive scan failed: %v", err)
			return
		}
		detectedDrives = drives
		refreshDriveChoices()
		if len(drives) == 0 {
			fixer.Log(fixer.LoggerWarn, "No drives found")
			return
		}
		for _, drive := range drives {
			fixer.Log(fixer.LoggerInfo, "Drive: %s", batch.FormatDrive(drive))
		}
	})

	scanSourcesButton = widget.NewButton("Scan Selected Sources", func() {
		if len(batchZipRoots) == 0 {
			fixer.Log(fixer.LoggerError, "Add at least one batch source first")
			return
		}
		zips, err := batch.FindTakeoutZips(batchZipRoots)
		if err != nil {
			fixer.Log(fixer.LoggerError, "Source scan failed: %v", err)
			return
		}
		fixer.Log(fixer.LoggerInfo, "Selected sources contain %d Takeout ZIP files", len(zips))
		for _, item := range zips {
			fixer.Log(fixer.LoggerInfo, "ZIP: %s (%s)", item.Path, fixer.FormatBytes(item.SizeBytes))
		}
	})

	clearSourcesButton = widget.NewButtonWithIcon("Clear Sources", theme.ContentClearIcon(), func() {
		batchZipRoots = nil
		refreshBatchSources()
		refreshDriveChoices()
		updateSelectionSummary()
	})

	useLinksCheckbox := widget.NewCheck("Use symlinks for albums", func(value bool) {
		useSymlinks = value
		if !uiReady {
			return
		}
		updateCheckboxStates()
		savePreferences()
	})
	useLinksCheckbox.SetChecked(useSymlinks)

	writeMetadataCheckbox := widget.NewCheck("Write metadata", func(value bool) {
		writeMetadata = value
		if !uiReady {
			return
		}
		updateDependencyStatus()
		savePreferences()
	})
	writeMetadataCheckbox.SetChecked(writeMetadata)

	ignoreAlbumsCheckbox := widget.NewCheck("Ignore album folders", func(value bool) {
		ignoreAlbums = value
		if !uiReady {
			return
		}
		updateCheckboxStates()
		savePreferences()
	})
	ignoreAlbumsCheckbox.SetChecked(ignoreAlbums)

	monthSubfoldersCheckbox := widget.NewCheck("Create month subfolders", func(value bool) {
		monthSubfolders = value
		if !uiReady {
			return
		}
		updateCheckboxStates()
		savePreferences()
	})
	monthSubfoldersCheckbox.SetChecked(monthSubfolders)

	flattenCheckbox := widget.NewCheck("Flatten album structure", func(value bool) {
		flatten = value
		if !uiReady {
			return
		}
		updateCheckboxStates()
		savePreferences()
	})
	flattenCheckbox.SetChecked(flatten)

	createMotionPhotosCheckbox := widget.NewCheck("Create Windows Motion Photos (MotionPhoto2)", func(value bool) {
		createMotionPhotos = value
		if !uiReady {
			return
		}
		updateDependencyStatus()
		savePreferences()
	})
	createMotionPhotosCheckbox.SetChecked(createMotionPhotos)

	deleteSourceCheckbox := widget.NewCheck("Delete input folder after clean run", func(value bool) {
		deleteSourceAfterSuccess = value
		if !uiReady {
			return
		}
		savePreferences()
	})
	deleteSourceCheckbox.SetChecked(deleteSourceAfterSuccess)

	restoreMOVExtensionCheckbox := widget.NewCheck("Restore .MOV file extension", func(value bool) {
		restoreMOVExtension = value
		if !uiReady {
			return
		}
		updateDependencyStatus()
		savePreferences()
	})
	restoreMOVExtensionCheckbox.SetChecked(restoreMOVExtension)

	deduplicateCheckbox := widget.NewCheck("Deduplicate exact copies", func(value bool) {
		deduplicate = value
		if !uiReady {
			return
		}
		savePreferences()
	})
	deduplicateCheckbox.SetChecked(deduplicate)

	dryRunCheckbox := widget.NewCheck("Dry run with audit report", func(value bool) {
		dryRun = value
		if !uiReady {
			return
		}
		savePreferences()
	})
	dryRunCheckbox.SetChecked(dryRun)

	preflightOnlyCheckbox = widget.NewCheck("Preflight only", func(value bool) {
		preflightOnly = value
		if !uiReady {
			return
		}
		setHugeMode(true)
	})
	preflightOnlyCheckbox.SetChecked(preflightOnly)

	keepTempOnErrorCheckbox = widget.NewCheck("Keep temp on error", func(value bool) {
		keepTempOnError = value
		if !uiReady {
			return
		}
		setHugeMode(true)
	})
	keepTempOnErrorCheckbox.SetChecked(keepTempOnError)

	keepLiveVideoCheckbox = widget.NewCheck("Keep live videos", func(value bool) {
		keepLiveVideos = value
		if !uiReady {
			return
		}
		setHugeMode(true)
		savePreferences()
	})
	keepLiveVideoCheckbox.SetChecked(keepLiveVideos)

	verifyWritesCheckbox := widget.NewCheck("Verify metadata after write", func(value bool) {
		verifyWrites = value
		if !uiReady {
			return
		}
		updateDependencyStatus()
		savePreferences()
	})
	verifyWritesCheckbox.SetChecked(verifyWrites)

	conflictPolicySelect := widget.NewSelect(
		[]string{
			string(fixer.ConflictPreferJSON),
			string(fixer.ConflictPreferEmbedded),
			string(fixer.ConflictMerge),
		},
		func(value string) {
			parsed, err := fixer.ParseConflictPolicy(value)
			if err != nil {
				return
			}
			conflictPolicy = parsed
			if !uiReady {
				return
			}
			savePreferences()
		},
	)
	conflictPolicySelect.SetSelected(string(conflictPolicy))

	updateCheckboxStates = func() {
		setEnabled := func(cb *widget.Check, enabled bool) {
			if enabled {
				cb.Enable()
			} else {
				cb.Disable()
			}
		}

		setEnabled(useLinksCheckbox, !ignoreAlbums && !flatten)
		setEnabled(ignoreAlbumsCheckbox, !useSymlinks && !flatten)
		setEnabled(flattenCheckbox, !useSymlinks && !ignoreAlbums && !monthSubfolders)
		setEnabled(monthSubfoldersCheckbox, !flatten)
	}

	applyRecommendedMode := func(dryRunOnly bool) {
		writeMetadataCheckbox.SetChecked(true)
		deduplicateCheckbox.SetChecked(true)
		verifyWritesCheckbox.SetChecked(true)
		restoreMOVExtensionCheckbox.SetChecked(true)
		dryRunCheckbox.SetChecked(dryRunOnly)
		conflictPolicySelect.SetSelected(string(fixer.ConflictMerge))
	}

	recommendedButton = widget.NewButtonWithIcon("Recommended Safe Mode", theme.ConfirmIcon(), func() {
		applyRecommendedMode(false)
		fixer.Log(fixer.LoggerInfo, "Applied recommended settings")
	})

	dryRunPresetButton = widget.NewButtonWithIcon("Audit Only", theme.VisibilityIcon(), func() {
		applyRecommendedMode(true)
		fixer.Log(fixer.LoggerInfo, "Configured for dry run audit")
	})

	var setControlsEnabled func(bool)
	var stopAfterCurrentZip atomic.Bool
	runBatch := func(forcePreflight bool) {
		if len(batchZipRoots) == 0 {
			fixer.Log(fixer.LoggerError, "Add at least one ZIP folder for Huge Takeout Mode")
			return
		}

		setHugeMode(true)
		options := currentOptions()
		options.DeleteSourceAfterSuccess = false
		options.KeepLiveVideo = keepLiveVideos

		preflightRun := forcePreflight || preflightOnly
		savePreferences()
		setControlsEnabled(false)
		stopAfterCurrentZip.Store(false)
		progressBar.SetValue(0)
		if preflightRun {
			progressLabel.SetText("Running preflight...")
		} else {
			progressLabel.SetText("Preparing ZIP batch...")
		}
		batchCurrentZipLabel.SetText("Current ZIP: none")
		batchCountLabel.SetText("ZIPs: 0 / 0")
		batchFileProgressLabel.SetText("Current file: none")
		batchErrorLabel.SetText("Latest error: none")
		reportPath = ""
		if outputPath != "" {
			reportPath = filepath.Join(outputPath, ".gtf", "batch", "manifest.jsonl")
			if preflightRun {
				reportPath = filepath.Join(outputPath, ".gtf", "batch", "preflight_latest.txt")
			}
			batchReportLabel.SetText("Report: " + reportPath)
		} else {
			batchReportLabel.SetText("Report: not ready")
		}

		ctx := context.Background()
		if !preflightRun {
			stopAfterCurrentButton.Enable()
		}
		cancelButton.Disable()
		fixer.Log(fixer.LoggerInfo, "Batch will scan %d selected source(s)", len(batchZipRoots))
		for _, source := range batchZipRoots {
			fixer.Log(fixer.LoggerInfo, "Scan source: %s", source)
		}

		fixer.SafeGo("gui-batch-process", func() {
			result, err := batch.Run(ctx, batch.Options{
				ZipRoots:        batchZipRoots,
				WorkDir:         batchWorkPath,
				OutputDir:       outputPath,
				AutoDrives:      true,
				AskOnAmbiguous:  false,
				KeepTempOnError: keepTempOnError,
				PreflightOnly:   preflightRun,
				ProcessOptions:  options,
				StopAfterCurrent: func() bool {
					return stopAfterCurrentZip.Load()
				},
				Progress: func(progress batch.BatchProgress) {
					fyne.Do(func() {
						if progress.CurrentZip != "" {
							batchCurrentZipLabel.SetText("Current ZIP: " + filepath.Base(progress.CurrentZip))
						}
						if progress.Total > 0 {
							batchCountLabel.SetText(fmt.Sprintf("ZIPs: %d / %d", progress.Completed, progress.Total))
						}
						if progress.FileTotal > 0 {
							progressBar.Max = float64(progress.FileTotal)
							progressBar.SetValue(float64(progress.FileProcessed))
							batchFileProgressLabel.SetText(fmt.Sprintf("Current file: %d / %d - %s",
								progress.FileProcessed,
								progress.FileTotal,
								filepath.Base(progress.CurrentFile),
							))
							progressLabel.SetText(batchFileProgressLabel.Text)
						}
						if progress.LatestError != "" {
							batchErrorLabel.SetText("Latest error: " + progress.LatestError)
						}
						if progress.ReportPath != "" {
							reportPath = progress.ReportPath
							batchReportLabel.SetText("Report: " + reportPath)
						}
					})
				},
			})

			fyne.Do(func() {
				if result.OutputDir != "" {
					outputPath = result.OutputDir
				}
				if result.WorkDir != "" {
					batchWorkPath = result.WorkDir
				}
				if result.ManifestPath != "" {
					reportPath = result.ManifestPath
				}
				if result.Preflight != nil {
					reportPath = filepath.Join(result.OutputDir, ".gtf", "batch", "preflight_latest.txt")
					batchReportLabel.SetText("Report: " + reportPath)
					for _, line := range strings.Split(batch.FormatPreflightReport(*result.Preflight), "\n") {
						line = strings.TrimSpace(line)
						if line != "" {
							fixer.Log(fixer.LoggerInfo, "%s", line)
						}
					}
					progressLabel.SetText(fmt.Sprintf("Preflight done: %d ZIPs", result.ZipCount))
				} else if err != nil {
					fixer.Log(fixer.LoggerError, "Batch error: %s", err.Error())
					batchErrorLabel.SetText("Latest error: " + err.Error())
					progressLabel.SetText("Batch stopped with error")
				} else if result.Stopped {
					fixer.Log(fixer.LoggerInfo, "Stopped after current ZIP")
					progressLabel.SetText("Stopped after current ZIP")
				} else {
					fixer.Log(fixer.LoggerInfo, "Batch done: processed=%d skipped=%d planned=%d", result.Processed, result.Skipped, result.Planned)
					progressLabel.SetText(fmt.Sprintf("Batch done: %d processed, %d skipped", result.Processed, result.Skipped))
				}
				updateSelectionSummary()
				batchReportLabel.SetText("Report: " + reportPath)
				if outputPath != "" {
					openOutputButton.Enable()
				}
				if reportPath != "" {
					openReportButton.Enable()
				}
				stopAfterCurrentButton.Disable()
				cancelFn = nil
				setControlsEnabled(true)
			})
		})
	}

	setControlsEnabled = func(enabled bool) {
		toggle := func(btn interface {
			Enable()
			Disable()
		}) {
			if enabled {
				btn.Enable()
			} else {
				btn.Disable()
			}
		}

		toggle(inputButton)
		toggle(outputButton)
		toggle(hugeModeButton)
		toggle(addZipRootButton)
		toggle(addZipFileButton)
		toggle(workFolderButton)
		toggle(scanDrivesButton)
		toggle(scanSourcesButton)
		toggle(clearSourcesButton)
		toggle(startButton)
		toggle(preflightButton)
		toggle(stopAfterCurrentButton)
		toggle(recommendedButton)
		toggle(dryRunPresetButton)
		toggle(openOutputButton)
		toggle(openReportButton)
		toggle(useLinksCheckbox)
		toggle(writeMetadataCheckbox)
		toggle(ignoreAlbumsCheckbox)
		toggle(monthSubfoldersCheckbox)
		toggle(flattenCheckbox)
		toggle(createMotionPhotosCheckbox)
		toggle(deleteSourceCheckbox)
		toggle(restoreMOVExtensionCheckbox)
		toggle(deduplicateCheckbox)
		toggle(dryRunCheckbox)
		toggle(preflightOnlyCheckbox)
		toggle(keepTempOnErrorCheckbox)
		toggle(keepLiveVideoCheckbox)
		toggle(verifyWritesCheckbox)
		if enabled {
			conflictPolicySelect.Enable()
			updateCheckboxStates()
			if outputPath == "" {
				openOutputButton.Disable()
			}
			if reportPath == "" {
				openReportButton.Disable()
			}
			stopAfterCurrentButton.Disable()
		} else {
			conflictPolicySelect.Disable()
		}
	}

	startButton = widget.NewButtonWithIcon("Start Processing", theme.MediaPlayIcon(), func() {
		if hugeTakeoutMode {
			runBatch(false)
			return
		}

		if err := fixer.ValidateProcessPaths(inputPath, outputPath); err != nil {
			fixer.Log(fixer.LoggerError, "%v", err)
			return
		}

		options := currentOptions()
		exifInfo, err := fixer.ValidateProcessingDependencies(options)
		if err != nil {
			fixer.Log(fixer.LoggerError, "%v", err)
			updateDependencyStatus()
			return
		}
		if exifInfo != nil {
			fixer.Log(fixer.LoggerInfo, "Using ExifTool %s from %s", exifInfo.Version, exifInfo.Path)
		}
		motionInfo, err := fixer.ValidateMotionPhotoDependencies(options)
		if err != nil {
			fixer.Log(fixer.LoggerError, "%v", err)
			updateDependencyStatus()
			return
		}
		if motionInfo != nil {
			fixer.Log(fixer.LoggerInfo, "Using MotionPhoto2 from %s", motionInfo.Path)
		}

		savePreferences()
		setControlsEnabled(false)
		progressBar.SetValue(0)
		progressLabel.SetText("Preparing media plan...")
		reportPath = filepath.Join(outputPath, ".gtf", "reports", "latest.txt")

		ctx, cancel := context.WithCancel(context.Background())
		cancelFn = cancel
		cancelButton.Enable()
		progressCh := make(chan fixer.Progress)

		fixer.SafeGo("gui-process", func() {
			if err := fixer.Process(ctx, inputPath, outputPath, progressCh, options); err != nil && ctx.Err() == nil {
				fyne.Do(func() {
					fixer.Log(fixer.LoggerError, "Error: %s", err.Error())
				})
			}
		})

		fixer.SafeGo("gui-progress", func() {
			var lastUpdate time.Time
			var lastProgress fixer.Progress

			for progress := range progressCh {
				lastProgress = progress
				if time.Since(lastUpdate) < 100*time.Millisecond && progress.Processed < progress.Total {
					continue
				}
				lastUpdate = time.Now()

				percentage := 0.0
				if progress.Total > 0 {
					percentage = (float64(progress.Processed) / float64(progress.Total)) * 100.0
				}

				text := fmt.Sprintf("[%.2f%%] %d/%d - %s", percentage, progress.Processed, progress.Total, filepath.Base(progress.Current))
				fyne.Do(func() {
					progressBar.Max = float64(max(progress.Total, 1))
					progressBar.SetValue(float64(progress.Processed))
					progressLabel.SetText(text)
				})
			}

			fyne.Do(func() {
				if lastProgress.Total > 0 {
					progressBar.Max = float64(lastProgress.Total)
					progressBar.SetValue(float64(lastProgress.Processed))
				}

				if ctx.Err() != nil {
					fixer.Log(fixer.LoggerInfo, "Cancelled")
					progressLabel.SetText("Cancelled")
				} else {
					fixer.Log(fixer.LoggerInfo, "Done")
					fixer.Log(fixer.LoggerInfo, "Audit report: %s", reportPath)
					progressLabel.SetText(fmt.Sprintf("Finished processing %d files", lastProgress.Processed))
				}

				cancelButton.Disable()
				cancelFn = nil
				setControlsEnabled(true)
			})
		})
	})

	preflightButton = widget.NewButtonWithIcon("Preflight", theme.VisibilityIcon(), func() {
		runBatch(true)
	})

	stopAfterCurrentButton = widget.NewButtonWithIcon("Stop After Current ZIP", theme.CancelIcon(), func() {
		stopAfterCurrentZip.Store(true)
		fixer.Log(fixer.LoggerInfo, "Will stop after current ZIP finishes")
		progressLabel.SetText("Will stop after current ZIP")
		stopAfterCurrentButton.Disable()
	})
	stopAfterCurrentButton.Disable()

	cancelButton = widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() {
		if cancelFn == nil {
			return
		}
		fixer.Log(fixer.LoggerInfo, "Cancelling...")
		cancelButton.Disable()
		cancelFn()
	})
	cancelButton.Disable()

	openOutputButton = widget.NewButtonWithIcon("Open Output Folder", theme.FolderOpenIcon(), func() {
		openInExplorer(outputPath)
	})
	openOutputButton.Disable()

	openReportButton = widget.NewButtonWithIcon("Open Latest Report", theme.DocumentIcon(), func() {
		openInExplorer(reportPath)
	})
	openReportButton.Disable()

	fixer.SafeGo("gui-log-drain", func() {
		for logMsg := range logCh {
			newLogs := []string{logMsg}
			for len(logCh) > 0 {
				newLogs = append(newLogs, <-logCh)
			}

			fyne.Do(func() {
				visibleLogLines = append(visibleLogLines, newLogs...)
				if len(visibleLogLines) > maxVisibleLogLines {
					visibleLogLines = visibleLogLines[len(visibleLogLines)-maxVisibleLogLines:]
				}

				logLabel.SetText(strings.Join(visibleLogLines, "\n"))
				logLabel.Refresh()
			})
			time.Sleep(100 * time.Millisecond)
		}
	})

	if prefsErr != nil {
		fixer.Log(fixer.LoggerWarn, "Could not load preferences: %v", prefsErr)
	}
	fixer.Log(fixer.LoggerInfo, "Logs will appear here...")

	if inputPath != "" {
		inputButton.SetText("Input: " + filepath.Base(inputPath))
	}
	if outputPath != "" {
		outputButton.SetText("Output: " + filepath.Base(outputPath))
		openOutputButton.Enable()
	}

	refreshBatchSources()
	refreshDriveChoices()
	updateSelectionSummary()
	updateCheckboxStates()
	updateDependencyStatus()
	uiReady = true

	sectionLabel := func(text string) *widget.Label {
		label := widget.NewLabel(text)
		label.TextStyle = fyne.TextStyle{Bold: true}
		label.Alignment = fyne.TextAlignLeading
		return label
	}

	modeRow := container.NewGridWithColumns(2, hugeModeButton, scanDrivesButton)
	standardFolders := container.NewVBox(
		sectionLabel("Single folder input"),
		inputButton,
		inputPathLabel,
	)
	batchSourceActions := container.NewGridWithColumns(2, addZipRootButton, addZipFileButton)
	batchSourceMaintenance := container.NewGridWithColumns(2, scanSourcesButton, clearSourcesButton)
	batchOptionRow := container.NewGridWithColumns(3, preflightOnlyCheckbox, keepTempOnErrorCheckbox, keepLiveVideoCheckbox)
	sourceScroll := container.NewVScroll(batchSourcesBox)
	sourceScroll.SetMinSize(fyne.NewSize(0, 94))
	driveScroll := container.NewVScroll(driveChoicesBox)
	driveScroll.SetMinSize(fyne.NewSize(0, 130))
	batchFolders := container.NewVBox(
		sectionLabel("Batch ZIP sources"),
		widget.NewLabel("Choose exact folders, ZIP files, or This PC drives to scan. Nothing else gets scanned."),
		batchSourceActions,
		batchSourceMaintenance,
		batchOptionRow,
		sourceScroll,
		sectionLabel("This PC drives"),
		driveScroll,
		sectionLabel("Batch status"),
		batchCurrentZipLabel,
		batchCountLabel,
		batchFileProgressLabel,
		batchErrorLabel,
		batchReportLabel,
	)
	destinationPanel := container.NewVBox(
		sectionLabel("Final output"),
		outputButton,
		outputPathLabel,
		sectionLabel("Temp work folder"),
		workFolderButton,
		workPathLabel,
	)
	checkBoxRow := container.NewGridWithColumns(
		2,
		useLinksCheckbox,
		writeMetadataCheckbox,
		ignoreAlbumsCheckbox,
		monthSubfoldersCheckbox,
		flattenCheckbox,
		createMotionPhotosCheckbox,
		restoreMOVExtensionCheckbox,
		deduplicateCheckbox,
		dryRunCheckbox,
		verifyWritesCheckbox,
		deleteSourceCheckbox,
	)
	conflictPolicyRow := container.NewGridWithColumns(2, widget.NewLabel("Conflict policy"), conflictPolicySelect)
	presetRow := container.NewGridWithColumns(2, recommendedButton, dryRunPresetButton)
	startCancelRow := container.NewGridWithColumns(4, preflightButton, startButton, stopAfterCurrentButton, cancelButton)
	postRunRow := container.NewGridWithColumns(2, openOutputButton, openReportButton)

	separator := func() fyne.CanvasObject {
		return container.NewPadded(widget.NewSeparator())
	}

	topContent := container.NewVBox(
		hintLabel,
		dependencyLabel,
		sectionLabel("Mode"),
		modeRow,
		selectionSummary,
		separator(),
		standardFolders,
		separator(),
		batchFolders,
		separator(),
		destinationPanel,
		separator(),
		presetRow,
		checkBoxRow,
		conflictPolicyRow,
		separator(),
		startCancelRow,
		postRunRow,
		progressBar,
		progressLabel,
	)

	topScroll := container.NewVScroll(topContent)
	topScroll.SetMinSize(fyne.NewSize(0, 280))
	logPanel := container.NewBorder(sectionLabel("Log"), nil, nil, nil, container.NewVScroll(logLabel))
	mainContent := container.NewVSplit(topScroll, logPanel)
	mainContent.Offset = 0.68
	w.SetContent(container.NewPadded(mainContent))
	w.ShowAndRun()
}

func max(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
