package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/Terru03/google-photos-takeout-helper/internal/batch"
	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
	version "github.com/Terru03/google-photos-takeout-helper/internal/version"
	"github.com/ncruces/zenity"
)

const (
	modeFolder = "folder"
	modeBatch  = "batch"

	progressEmpty      = "empty"
	progressPreflight  = "preflight"
	progressProcessing = "processing"
	progressDone       = "done"
	progressError      = "error"

	maxUILogLines = 5000
)

type guiState struct {
	app    fyne.App
	window fyne.Window

	mode          string
	inputPath     string
	outputPath    string
	workPath      string
	workPaths     []string
	stagingPath   string
	selectedWork  string
	zipRoots      []string
	selectedRoot  string
	options       fixer.ProcessOptions
	keepTempOnErr bool
	reprocess     bool

	progressPhase  string
	latestError    string
	preflight      *batch.PreflightReport
	folderEstimate int
	result         *batch.Result
	runReport      *fixer.RunReport

	currentZip     string
	batchIndex     int
	batchCompleted int
	batchTotal     int
	fileProcessed  int
	fileTotal      int
	currentFile    string
	stage          string
	logLines       []string
	logLabel       *widget.Label
	logScroll      *container.Scroll
	logAutoScroll  bool
	progressScroll *container.Scroll
	cancel         context.CancelFunc
	stopAfterZip   atomic.Bool
	selectedTab    int
}

func Main() {
	defer fixer.RecoverPanic("gui-main")

	defaults := fixer.DefaultProcessOptions()
	prefs, prefsErr := fixer.LoadPreferences()
	if prefs.Options != (fixer.ProcessOptions{}) {
		defaults = prefs.Options
	}
	defaults = defaults.Normalized()
	if defaults.ConflictPolicy == "" {
		defaults.ConflictPolicy = fixer.ConflictMerge
	}
	if prefs.Options == (fixer.ProcessOptions{}) {
		defaults.KeepLiveVideo = true
		defaults.AlbumMode = fixer.AlbumModeUniqueOnly
		defaults.IgnoreAlbums = false
	}
	defaults.DeleteSourceAfterSuccess = false

	a := app.New()
	a.Settings().SetTheme(helperTheme{})
	a.SetIcon(resourceGoogleTakeoutFixerPng)

	w := a.NewWindow("Google Photos Takeout Helper " + version.Tag)
	w.Resize(fyne.NewSize(860, 760))

	state := &guiState{
		app:           a,
		window:        w,
		mode:          modeFolder,
		inputPath:     prefs.LastInputPath,
		outputPath:    prefs.LastOutputPath,
		stagingPath:   prefs.LastStagingPath,
		zipRoots:      append([]string(nil), prefs.ZipRoots...),
		workPaths:     append([]string(nil), prefs.WorkPaths...),
		options:       defaults,
		keepTempOnErr: true,
		progressPhase: progressEmpty,
		stage:         "Extract",
		logAutoScroll: true,
	}
	if len(state.zipRoots) == 0 && looksLikeZipSourceFolder(prefs.LastInputPath) {
		state.zipRoots = []string{prefs.LastInputPath}
	}
	if len(state.zipRoots) > 0 {
		state.mode = modeBatch
		state.options.AlbumMode = fixer.AlbumModeTimelineOnly
		state.options.IgnoreAlbums = true
		state.options.MonthSubfolders = true
		state.options.CreateMotionPhotos = false
	}
	if len(state.workPaths) == 0 && fixer.FileExists(`C:\Takeout_Incoming`) {
		state.workPaths = []string{`C:\Takeout_Incoming`}
	}
	if state.stagingPath == "" {
		state.stagingPath = `C:\Takeout_Staging`
	}
	state.workPath = state.primaryWorkPath()

	fixer.SetLogHandler(func(level fixer.LogLevel, message string) {
		state.appendLog(fmt.Sprintf("[%s] %s", level, message))
	})
	if prefsErr != nil {
		fixer.Log(fixer.LoggerWarn, "Could not load preferences: %v", prefsErr)
	}
	state.loadReportIfAvailable()
	state.refreshTabs(0)
	w.ShowAndRun()
}

func (s *guiState) refreshTabs(selected int) {
	if selected < 0 {
		selected = 0
	}
	if selected >= tabCount {
		selected = tabCount - 1
	}
	s.selectedTab = selected

	var content fyne.CanvasObject
	switch selected {
	case 1:
		content = s.buildProgressTab()
	case 2:
		content = s.buildReportsTab()
	case 3:
		content = s.buildOptionsTab()
	default:
		content = s.buildSetupTab()
	}
	if s.window != nil {
		s.window.SetContent(s.appChrome(content))
	}
}

func (s *guiState) currentOptions() fixer.ProcessOptions {
	opts := s.options
	if opts.ConflictPolicy == "" {
		opts.ConflictPolicy = fixer.ConflictMerge
	}
	return opts.Normalized()
}

func (s *guiState) savePreferences() {
	if err := fixer.SavePreferences(fixer.Preferences{
		LastInputPath:   s.inputPath,
		LastOutputPath:  s.outputPath,
		LastStagingPath: s.stagingPath,
		ZipRoots:        append([]string(nil), s.zipRoots...),
		WorkPaths:       append([]string(nil), s.workPaths...),
		Options:         s.currentOptions(),
	}); err != nil {
		fixer.Log(fixer.LoggerWarn, "Could not save preferences: %v", err)
	}
}

func (s *guiState) appendLog(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	fyne.Do(func() {
		s.logLines = append(s.logLines, line)
		if len(s.logLines) > maxUILogLines {
			s.logLines = s.logLines[len(s.logLines)-maxUILogLines:]
		}
		s.updateLogView()
	})
}

func (s *guiState) selectInputFolder() {
	dir, err := zenity.SelectFile(zenity.Title("Select Google Photos folder"), zenity.Directory())
	if err != nil {
		return
	}
	s.inputPath = dir
	if s.outputPath == "" {
		s.outputPath = filepath.Join(filepath.Dir(dir), filepath.Base(dir)+" Fixed")
	}
	s.mode = modeFolder
	s.savePreferences()
	s.refreshTabs(0)
}

func (s *guiState) addZipFolder() {
	dir, err := zenity.SelectFile(zenity.Title("Select folder with Takeout ZIPs"), zenity.Directory())
	if err != nil {
		return
	}
	s.addZipRoot(dir)
}

func (s *guiState) addZipFiles() {
	files, err := zenity.SelectFileMultiple(
		zenity.Title("Select Takeout ZIP file(s)"),
		zenity.FileFilters{{Name: "ZIP files", Patterns: []string{"*.zip"}}},
	)
	if err != nil {
		return
	}
	for _, file := range files {
		s.addZipRoot(file)
	}
}

func (s *guiState) addZipRoot(root string) {
	root = strings.TrimSpace(root)
	if root == "" {
		return
	}
	for _, existing := range s.zipRoots {
		if strings.EqualFold(filepath.Clean(existing), filepath.Clean(root)) {
			return
		}
	}
	s.zipRoots = append(s.zipRoots, root)
	s.selectedRoot = root
	s.mode = modeBatch
	s.options.AlbumMode = fixer.AlbumModeTimelineOnly
	s.options.IgnoreAlbums = true
	s.options.MonthSubfolders = true
	s.inputPath = root
	s.savePreferences()
	s.refreshTabs(0)
}

func (s *guiState) removeSelectedRoot() {
	if s.selectedRoot == "" {
		return
	}
	filtered := s.zipRoots[:0]
	for _, root := range s.zipRoots {
		if !strings.EqualFold(filepath.Clean(root), filepath.Clean(s.selectedRoot)) {
			filtered = append(filtered, root)
		}
	}
	s.zipRoots = filtered
	s.selectedRoot = ""
	s.savePreferences()
	s.refreshTabs(0)
}

func (s *guiState) selectOutputFolder() {
	dir, err := zenity.SelectFile(zenity.Title("Select final output folder"), zenity.Directory())
	if err != nil {
		return
	}
	s.outputPath = dir
	s.savePreferences()
	s.loadReportIfAvailable()
	s.refreshTabs(0)
}

func (s *guiState) selectWorkFolder() {
	dir, err := zenity.SelectFile(zenity.Title("Select temporary work folder"), zenity.Directory())
	if err != nil {
		return
	}
	s.addWorkRoot(dir)
}

func (s *guiState) selectStagingFolder() {
	dir, err := zenity.SelectFile(zenity.Title("Select SSD staging output folder"), zenity.Directory())
	if err != nil {
		return
	}
	s.stagingPath = dir
	s.savePreferences()
	s.refreshTabs(0)
}

func (s *guiState) addWorkRoot(root string) {
	root = strings.TrimSpace(root)
	if root == "" {
		return
	}
	for _, existing := range s.workPaths {
		if strings.EqualFold(filepath.Clean(existing), filepath.Clean(root)) {
			s.selectedWork = existing
			return
		}
	}
	s.workPaths = append(s.workPaths, root)
	s.selectedWork = root
	s.workPath = s.primaryWorkPath()
	s.mode = modeBatch
	s.savePreferences()
	s.refreshTabs(0)
}

func (s *guiState) removeSelectedWorkRoot() {
	if s.selectedWork == "" {
		return
	}
	filtered := s.workPaths[:0]
	for _, root := range s.workPaths {
		if !strings.EqualFold(filepath.Clean(root), filepath.Clean(s.selectedWork)) {
			filtered = append(filtered, root)
		}
	}
	s.workPaths = filtered
	s.selectedWork = ""
	s.workPath = s.primaryWorkPath()
	s.savePreferences()
	s.refreshTabs(0)
}

func (s *guiState) moveSelectedWorkRoot(delta int) {
	if s.selectedWork == "" || delta == 0 {
		return
	}
	index := -1
	for i, root := range s.workPaths {
		if strings.EqualFold(filepath.Clean(root), filepath.Clean(s.selectedWork)) {
			index = i
			break
		}
	}
	next := index + delta
	if index < 0 || next < 0 || next >= len(s.workPaths) {
		return
	}
	s.workPaths[index], s.workPaths[next] = s.workPaths[next], s.workPaths[index]
	s.workPath = s.primaryWorkPath()
	s.savePreferences()
	s.refreshTabs(0)
}

func looksLikeZipSourceFolder(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.Contains(strings.ToLower(entry.Name()), "takeout") &&
			strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			return true
		}
	}
	return false
}

func (s *guiState) normalizedWorkPaths() []string {
	paths := make([]string, 0, len(s.workPaths)+1)
	for _, path := range s.workPaths {
		path = strings.TrimSpace(path)
		if path != "" {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 && strings.TrimSpace(s.workPath) != "" {
		paths = append(paths, strings.TrimSpace(s.workPath))
	}
	return paths
}

func (s *guiState) primaryWorkPath() string {
	for _, path := range s.normalizedWorkPaths() {
		if strings.TrimSpace(path) != "" {
			return path
		}
	}
	return ""
}

func (s *guiState) runPreflight() {
	s.latestError = ""
	s.preflight = nil
	s.folderEstimate = 0
	s.progressPhase = progressPreflight
	s.stage = "Reports"
	s.savePreferences()

	if s.mode == modeBatch {
		if len(s.zipRoots) == 0 {
			s.setError("No ZIP files found in the selected source folders. Make sure the folders contain files named takeout-*.zip.")
			return
		}
		report, err := batch.Preflight(batch.Options{
			ZipRoots:          s.zipRoots,
			WorkDir:           s.primaryWorkPath(),
			WorkDirs:          s.normalizedWorkPaths(),
			OutputDir:         s.outputPath,
			StagingOutputDir:  s.stagingPath,
			AutoDrives:        s.outputPath == "" || len(s.normalizedWorkPaths()) == 0,
			KeepTempOnError:   s.keepTempOnErr,
			SafetyMarginBytes: 0,
			ProcessOptions:    s.currentOptions(),
		})
		if err != nil {
			s.setError(err.Error())
			return
		}
		s.preflight = &report
		if report.OutputDir != "" {
			s.outputPath = report.OutputDir
		}
		if report.WorkDir != "" {
			s.workPath = report.WorkDir
		}
		if len(report.WorkDirs) > 0 {
			s.workPaths = append([]string(nil), report.WorkDirs...)
			s.workPath = s.primaryWorkPath()
		}
		s.refreshTabs(1)
		return
	}

	if err := fixer.ValidateProcessPaths(s.inputPath, s.outputPath); err != nil {
		s.setError(err.Error())
		return
	}
	count, err := fixer.CountProcessableFiles(s.inputPath)
	if err != nil {
		s.setError(err.Error())
		return
	}
	if _, err := fixer.ValidateProcessingDependencies(s.currentOptions()); err != nil {
		s.setError(err.Error())
		return
	}
	s.folderEstimate = count
	s.refreshTabs(1)
}

func (s *guiState) startProcessing() {
	if s.mode == modeBatch {
		s.startBatchProcessing()
		return
	}
	s.startFolderProcessing()
}

func (s *guiState) startFolderProcessing() {
	if err := fixer.ValidateProcessPaths(s.inputPath, s.outputPath); err != nil {
		s.setError(err.Error())
		return
	}
	if _, err := fixer.ValidateProcessingDependencies(s.currentOptions()); err != nil {
		s.setError(err.Error())
		return
	}
	s.progressPhase = progressProcessing
	s.latestError = ""
	s.currentZip = "Extracted folder"
	s.currentFile = ""
	s.fileProcessed = 0
	s.fileTotal = 0
	s.stage = "Metadata"
	s.refreshTabs(1)

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	progressCh := make(chan fixer.Progress)
	errCh := make(chan error, 1)

	fixer.SafeGo("gui-folder-process", func() {
		errCh <- fixer.Process(ctx, s.inputPath, s.outputPath, progressCh, s.currentOptions())
	})

	fixer.SafeGo("gui-folder-progress", func() {
		lastRefresh := time.Time{}
		for progress := range progressCh {
			if time.Since(lastRefresh) < 250*time.Millisecond && progress.Processed < progress.Total {
				continue
			}
			lastRefresh = time.Now()
			progress := progress
			fyne.Do(func() {
				s.fileProcessed = progress.Processed
				s.fileTotal = progress.Total
				s.currentFile = progress.Current
				s.refreshTabs(1)
			})
		}
		err := <-errCh
		fyne.Do(func() {
			s.cancel = nil
			if err != nil && ctx.Err() == nil {
				s.setErrorNoRefresh(err.Error())
				s.refreshTabs(1)
				return
			}
			if ctx.Err() != nil {
				s.setErrorNoRefresh("Cancelled immediately. Reopen and resume from the output state if needed.")
				s.refreshTabs(1)
				return
			}
			if s.currentOptions().CreateMotionPhotos {
				s.stage = "Motion"
				mergeReport, mergeErr := fixer.MergeMotionLibrary(ctx, fixer.MotionMergeOptions{
					LibraryRoot: s.outputPath,
				})
				if mergeErr != nil {
					s.setErrorNoRefresh(mergeErr.Error())
					s.refreshTabs(1)
					return
				}
				s.appendLog(fmt.Sprintf(
					"[INFO] Motion merge done: %d merged, %d failed, %d timed out. Report: %s",
					mergeReport.MergedSuccessfully,
					mergeReport.FailedMotionPhotoCalls,
					mergeReport.TimedOutMerges,
					mergeReport.ReportPath,
				))
			}
			s.progressPhase = progressDone
			s.loadReportIfAvailable()
			s.refreshTabs(1)
		})
	})
}

func (s *guiState) startBatchProcessing() {
	if len(s.zipRoots) == 0 {
		s.setError("No ZIP files found in the selected source folders. Add a folder or ZIP file first.")
		return
	}
	s.progressPhase = progressProcessing
	s.latestError = ""
	s.batchCompleted = 0
	s.batchIndex = 0
	s.batchTotal = 0
	s.fileProcessed = 0
	s.fileTotal = 0
	s.currentFile = ""
	s.stage = "Extract"
	s.stopAfterZip.Store(false)
	s.refreshTabs(1)

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	options := s.currentOptions()
	options.DeleteSourceAfterSuccess = false

	fixer.SafeGo("gui-batch-process", func() {
		result, err := batch.Run(ctx, batch.Options{
			ZipRoots:         s.zipRoots,
			WorkDir:          s.primaryWorkPath(),
			WorkDirs:         s.normalizedWorkPaths(),
			OutputDir:        s.outputPath,
			StagingOutputDir: s.stagingPath,
			AutoDrives:       s.outputPath == "" || len(s.normalizedWorkPaths()) == 0,
			KeepTempOnError:  s.keepTempOnErr,
			Reprocess:        s.reprocess,
			ProcessOptions:   options,
			StopAfterCurrent: func() bool { return s.stopAfterZip.Load() },
			Progress: func(progress batch.BatchProgress) {
				fyne.Do(func() {
					if progress.CurrentZip != "" && !strings.EqualFold(filepath.Clean(progress.CurrentZip), filepath.Clean(s.currentZip)) {
						s.fileProcessed = 0
						s.fileTotal = 0
						s.currentFile = ""
					}
					if progress.CurrentZip != "" {
						s.currentZip = progress.CurrentZip
					}
					if progress.CurrentIndex > 0 {
						s.batchIndex = progress.CurrentIndex
					}
					if progress.Total > 0 {
						s.batchTotal = progress.Total
						s.batchCompleted = progress.Completed
					}
					if progress.Phase == "index" {
						s.stage = "JSON index"
					} else if progress.Phase == "extract" {
						s.stage = "Extract"
					} else if progress.Phase == "process" {
						s.stage = "Metadata"
					} else if progress.Phase == "commit" {
						s.stage = "Commit"
					}
					if progress.FileTotal > 0 {
						s.fileProcessed = progress.FileProcessed
						s.fileTotal = progress.FileTotal
						if strings.TrimSpace(progress.CurrentFile) != "" {
							s.currentFile = progress.CurrentFile
						}
					}
					if progress.LatestError != "" {
						s.latestError = progress.LatestError
					}
					s.refreshTabs(1)
				})
			},
		})
		fyne.Do(func() {
			s.cancel = nil
			s.result = &result
			if result.OutputDir != "" {
				s.outputPath = result.OutputDir
			}
			if result.WorkDir != "" {
				s.workPath = result.WorkDir
			}
			if len(result.WorkDirs) > 0 {
				s.workPaths = append([]string(nil), result.WorkDirs...)
				s.workPath = s.primaryWorkPath()
			}
			if err != nil && ctx.Err() == nil {
				s.setErrorNoRefresh(err.Error())
				s.refreshTabs(1)
				return
			}
			if ctx.Err() != nil {
				s.setErrorNoRefresh("Cancelled immediately. Stop After Current ZIP is safer for big exports.")
				s.refreshTabs(1)
				return
			}
			if result.Stopped {
				s.setErrorNoRefresh("Stopped after current ZIP. Run again to resume from the manifest.")
				s.refreshTabs(1)
				return
			}
			s.progressPhase = progressDone
			s.loadReportIfAvailable()
			s.refreshTabs(1)
		})
	})
}

func (s *guiState) setError(message string) {
	s.setErrorNoRefresh(message)
	s.refreshTabs(1)
}

func (s *guiState) setErrorNoRefresh(message string) {
	s.progressPhase = progressError
	s.latestError = message
	s.appendLog("[ERROR] " + message)
}

func (s *guiState) stopAfterCurrentZIP() {
	s.stopAfterZip.Store(true)
	s.appendLog("[INFO] Will stop after current ZIP finishes.")
	s.refreshTabs(1)
}

func (s *guiState) cancelImmediately() {
	if s.cancel == nil {
		return
	}
	s.appendLog("[WARN] Immediate cancel requested.")
	s.cancel()
}

func (s *guiState) openPath(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if err := exec.Command("explorer.exe", path).Start(); err != nil {
		fixer.Log(fixer.LoggerError, "Could not open %s: %v", path, err)
	}
}

func (s *guiState) loadReportIfAvailable() {
	if strings.TrimSpace(s.outputPath) == "" {
		return
	}
	path := reportPath(s.outputPath, "reports", "latest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var report fixer.RunReport
	if err := json.Unmarshal(data, &report); err != nil {
		return
	}
	s.runReport = &report
}

func (s *guiState) resumeFound() bool {
	if strings.TrimSpace(s.outputPath) == "" {
		return false
	}
	return fixer.FileExists(reportPath(s.outputPath, "batch", "manifest.jsonl")) ||
		fixer.FileExists(reportPath(s.outputPath, "state.jsonl"))
}

func (s *guiState) sameDriveWarning() bool {
	if strings.TrimSpace(s.outputPath) == "" {
		return false
	}
	outputRoot := strings.ToUpper(filepath.VolumeName(s.outputPath))
	if outputRoot == "" {
		return false
	}
	if s.mode == modeBatch {
		for _, root := range s.zipRoots {
			if strings.EqualFold(outputRoot, strings.ToUpper(filepath.VolumeName(root))) {
				return true
			}
		}
		return false
	}
	return strings.EqualFold(outputRoot, strings.ToUpper(filepath.VolumeName(s.inputPath)))
}

func (s *guiState) durationText() string {
	if s.runReport == nil || s.runReport.StartedAt.IsZero() || s.runReport.FinishedAt.IsZero() {
		return "-"
	}
	return s.runReport.FinishedAt.Sub(s.runReport.StartedAt).Round(time.Second).String()
}
