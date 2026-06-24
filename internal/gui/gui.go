package gui

import (
	"context"
	"fmt"
	"image/color"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
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

	inputPathEntry := widget.NewEntry()
	inputPathEntry.Disable()
	outputPathEntry := widget.NewEntry()
	outputPathEntry.Disable()

	reportPath := ""
	var cancelFn context.CancelFunc
	var cancelButton *widget.Button
	var openOutputButton *widget.Button
	var openReportButton *widget.Button
	var startButton *widget.Button
	var inputButton *widget.Button
	var outputButton *widget.Button
	var hugeModeButton *widget.Button
	var addZipRootButton *widget.Button
	var workFolderButton *widget.Button
	var scanDrivesButton *widget.Button
	var recommendedButton *widget.Button
	var dryRunPresetButton *widget.Button

	logEntry := widget.NewMultiLineEntry()
	const maxVisibleLogLines = 300
	visibleLogLines := make([]string, 0, maxVisibleLogLines)
	var logUpdating bool
	logEntry.OnChanged = func(_ string) {
		if logUpdating {
			return
		}
		logUpdating = true
		logEntry.SetText(strings.Join(visibleLogLines, "\n") + "\n")
		logUpdating = false
	}

	logCh := make(chan string, 1000)
	fixer.SetLogHandler(func(level fixer.LogLevel, message string) {
		logCh <- fmt.Sprintf("[%s] %s", level, message)
	})

	updateSelectionSummary := func() {
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
			zipText := "not selected"
			if len(batchZipRoots) > 0 {
				zipText = strings.Join(batchZipRoots, "; ")
			}
			workText := "auto"
			if batchWorkPath != "" {
				workText = batchWorkPath
			}
			modeText = fmt.Sprintf("Huge Takeout Mode\nZIP roots: %s\nWork: %s", zipText, workText)
		}
		selectionSummary.SetText(fmt.Sprintf("Mode: %s\nInput: %s\nOutput: %s", modeText, inputText, outputText))
		inputPathEntry.SetText(inputText)
		outputPathEntry.SetText(outputText)
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

	hugeModeButton = widget.NewButton("Huge Takeout Mode: Off", func() {
		hugeTakeoutMode = !hugeTakeoutMode
		if hugeTakeoutMode {
			hugeModeButton.SetText("Huge Takeout Mode: On")
			fixer.Log(fixer.LoggerWarn, "Huge Takeout Mode keeps ZIP files, writes normal folders/files, and uses a separate temp work folder.")
		} else {
			hugeModeButton.SetText("Huge Takeout Mode: Off")
		}
		updateSelectionSummary()
	})

	addZipRootButton = widget.NewButtonWithIcon("Add ZIP Folder", theme.FolderOpenIcon(), func() {
		dir, err := zenity.SelectFile(zenity.Title("Select Folder With Takeout ZIPs"), zenity.Directory())
		if err != nil {
			return
		}
		batchZipRoots = append(batchZipRoots, dir)
		hugeTakeoutMode = true
		hugeModeButton.SetText("Huge Takeout Mode: On")
		fixer.Log(fixer.LoggerInfo, "Added ZIP root: %s", dir)
		updateSelectionSummary()
	})

	workFolderButton = widget.NewButtonWithIcon("Select Work Folder", theme.FolderOpenIcon(), func() {
		dir, err := zenity.SelectFile(zenity.Title("Select Temporary Work Folder"), zenity.Directory())
		if err != nil {
			return
		}
		batchWorkPath = dir
		hugeTakeoutMode = true
		hugeModeButton.SetText("Huge Takeout Mode: On")
		fixer.Log(fixer.LoggerInfo, "Work folder: %s", dir)
		updateSelectionSummary()
	})

	scanDrivesButton = widget.NewButton("Scan Drives", func() {
		drives, err := batch.DetectDrives()
		if err != nil {
			fixer.Log(fixer.LoggerError, "Drive scan failed: %v", err)
			return
		}
		if len(drives) == 0 {
			fixer.Log(fixer.LoggerWarn, "No drives found")
			return
		}
		for _, drive := range drives {
			fixer.Log(fixer.LoggerInfo, "Drive: %s", batch.FormatDrive(drive))
		}
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

	setControlsEnabled := func(enabled bool) {
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
		toggle(workFolderButton)
		toggle(scanDrivesButton)
		toggle(startButton)
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
		} else {
			conflictPolicySelect.Disable()
		}
	}

	startButton = widget.NewButtonWithIcon("Start Processing", theme.MediaPlayIcon(), func() {
		if hugeTakeoutMode {
			if len(batchZipRoots) == 0 {
				fixer.Log(fixer.LoggerError, "Add at least one ZIP folder for Huge Takeout Mode")
				return
			}
			options := currentOptions()
			options.DeleteSourceAfterSuccess = false

			savePreferences()
			setControlsEnabled(false)
			progressBar.SetValue(0)
			progressLabel.SetText("Preparing ZIP batch...")
			reportPath = ""
			if outputPath != "" {
				reportPath = filepath.Join(outputPath, ".gtf", "batch_manifest.jsonl")
			}

			ctx, cancel := context.WithCancel(context.Background())
			cancelFn = cancel
			cancelButton.Enable()

			fixer.SafeGo("gui-batch-process", func() {
				result, err := batch.Run(ctx, batch.Options{
					ZipRoots:        batchZipRoots,
					WorkDir:         batchWorkPath,
					OutputDir:       outputPath,
					AutoDrives:      true,
					AskOnAmbiguous:  false,
					KeepTempOnError: true,
					ProcessOptions:  options,
				})

				fyne.Do(func() {
					if err != nil && ctx.Err() == nil {
						fixer.Log(fixer.LoggerError, "Batch error: %s", err.Error())
						progressLabel.SetText("Batch stopped")
					} else if ctx.Err() != nil {
						fixer.Log(fixer.LoggerInfo, "Cancelled")
						progressLabel.SetText("Cancelled")
					} else {
						outputPath = result.OutputDir
						batchWorkPath = result.WorkDir
						reportPath = result.ManifestPath
						fixer.Log(fixer.LoggerInfo, "Batch done: processed=%d skipped=%d planned=%d", result.Processed, result.Skipped, result.Planned)
						progressLabel.SetText(fmt.Sprintf("Batch done: %d processed, %d skipped", result.Processed, result.Skipped))
						updateSelectionSummary()
						if outputPath != "" {
							openOutputButton.Enable()
						}
						if reportPath != "" {
							openReportButton.Enable()
						}
					}
					cancelButton.Disable()
					cancelFn = nil
					setControlsEnabled(true)
				})
			})
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

				logUpdating = true
				logEntry.SetText(strings.Join(visibleLogLines, "\n") + "\n")
				logUpdating = false
				logEntry.CursorRow = len(visibleLogLines)
				logEntry.CursorColumn = 0
				logEntry.Refresh()
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

	updateSelectionSummary()
	updateCheckboxStates()
	updateDependencyStatus()
	uiReady = true

	folderButtons := container.NewGridWithColumns(2, inputButton, outputButton)
	batchModeControls := container.NewGridWithColumns(4, hugeModeButton, addZipRootButton, workFolderButton, scanDrivesButton)
	pathPreview := container.NewVBox(
		widget.NewLabel("Google Takeout folder"),
		inputPathEntry,
		widget.NewLabel("Fixed output folder"),
		outputPathEntry,
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
	startCancelRow := container.NewGridWithColumns(2, startButton, cancelButton)
	postRunRow := container.NewGridWithColumns(2, openOutputButton, openReportButton)

	separator := canvas.NewRectangle(color.RGBA{R: 60, G: 60, B: 60, A: 100})
	separator.SetMinSize(fyne.NewSize(1, 1))

	topContent := container.NewVBox(
		hintLabel,
		dependencyLabel,
		batchModeControls,
		folderButtons,
		selectionSummary,
		pathPreview,
		container.NewPadded(separator),
		presetRow,
		checkBoxRow,
		conflictPolicyRow,
		container.NewPadded(separator),
		startCancelRow,
		postRunRow,
		progressBar,
		progressLabel,
	)

	topScroll := container.NewVScroll(topContent)
	topScroll.SetMinSize(fyne.NewSize(0, 280))
	logPanel := container.NewBorder(widget.NewLabel("Log"), nil, nil, nil, logEntry)
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
