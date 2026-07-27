package gui

import (
	"fmt"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
)

func (s *guiState) buildProgressTab() fyne.CanvasObject {
	var content fyne.CanvasObject
	switch s.progressPhase {
	case progressPreflight:
		content = s.buildPreflightScreen()
	case progressProcessing:
		content = s.buildProcessingScreen()
	case progressDone:
		content = s.buildDoneScreen()
	case progressError:
		content = s.buildErrorScreen()
	default:
		content = s.buildEmptyProgressScreen()
	}
	if s.progressScroll == nil {
		s.progressScroll = container.NewVScroll(content)
		return s.progressScroll
	}
	offset := s.progressScroll.Offset
	s.progressScroll.Content = content
	s.progressScroll.Refresh()
	s.progressScroll.ScrollToOffset(offset)
	return s.progressScroll
}

func (s *guiState) buildPreflightScreen() fyne.CanvasObject {
	status := badge("Ready", colGreenBg, colGreen)
	warnings := []fyne.CanvasObject{}
	stats := []fyne.CanvasObject{}
	workRootRows := []fyne.CanvasObject{}
	if s.preflight != nil {
		if len(s.preflight.Warnings) > 0 {
			status = badge("Warning", colYellowBg, colYellow)
			for _, warning := range s.preflight.Warnings {
				warnings = append(warnings, warningBanner(warning))
			}
		}
		stats = []fyne.CanvasObject{
			statCard(formatCount(s.preflight.ZipCount), "ZIP files found"),
			statCard(formatCount(s.preflight.EstimatedMediaFiles), "photos/videos estimated"),
			statCard(formatBytes(s.preflight.TotalZipSize), "total ZIP size"),
			statCard(formatBytes(s.preflight.LargestZipBytes), "largest ZIP"),
			statCard(formatBytes(s.preflight.OutputFreeBytes), "output drive free"),
		}
		for _, root := range s.preflight.WorkRoots {
			line := fmt.Sprintf("%s free, needs %s, type %s: %s",
				formatBytes(root.FreeBytes),
				formatBytes(root.RequiredBytes),
				root.Kind,
				root.Path,
			)
			if root.Usable {
				workRootRows = append(workRootRows, successBanner(line))
			} else {
				workRootRows = append(workRootRows, warningBanner(line))
			}
		}
	} else {
		stats = []fyne.CanvasObject{
			statCard("1", "input checked"),
			statCard(formatCount(s.folderEstimate), "photos/videos estimated"),
			statCard("-", "total ZIP size"),
			statCard("-", "largest ZIP"),
			statCard("ready", "output path"),
		}
	}
	if len(warnings) == 0 {
		warnings = append(warnings, successBanner("No blocking warnings found. Review the checklist and start processing when ready."))
	}

	checklist := card("WHAT WILL HAPPEN",
		successBanner("ZIP folders found"),
		successBanner("Extract work folder ready"),
		successBanner("Dedup manifest path ready"),
		successBanner("Copy/fix plan ready"),
		successBanner("Write-ready output"),
	)

	return container.NewVBox(
		container.NewBorder(nil, nil, sectionTitle("Preflight complete"), status),
		statGrid(stats...),
		card("WORK ROOTS", workRootRows...),
		container.NewVBox(warnings...),
		checklist,
		container.NewHBox(
			primaryButton("Start processing", s.startProcessing),
			secondaryButton("Back to setup", func() { s.refreshTabs(0) }),
		),
	)
}

func (s *guiState) buildProcessingScreen() fyne.CanvasObject {
	header := "Processing"
	if s.mode == modeBatch && s.batchTotal > 0 {
		index := s.batchIndex
		if index <= 0 {
			index = s.batchCompleted + 1
		}
		if index > s.batchTotal {
			index = s.batchTotal
		}
		header = fmt.Sprintf("Processing ZIP %d of %d", index, s.batchTotal)
	}
	overall := float64(s.fileProcessed)
	if s.fileTotal > 0 {
		overall = (float64(s.fileProcessed) / float64(s.fileTotal)) * 100
	}
	if s.mode == modeBatch && s.batchTotal > 0 {
		overall = (float64(s.batchCompleted) / float64(s.batchTotal)) * 100
	}

	currentZIP := cleanPathName(s.currentZip)
	currentFile := cleanPathName(s.currentFile)
	if s.currentFile == "" {
		currentFile = "Waiting for file progress"
	}

	return container.NewVBox(
		container.NewBorder(nil, nil, sectionTitle(header), badge("Running", colBlueBg, colBlue)),
		progressSection("OVERALL PROGRESS", overall),
		progressSection("CURRENT ZIP", filePercent(s.fileProcessed, s.fileTotal)),
		card("CURRENT FILE", fieldBox(currentFile), smallText("ZIP: "+currentZIP)),
		card("STAGE", stageRow(s.stage)),
		card("LOG",
			container.NewHBox(
				secondaryButton("Copy log", s.copyLogToClipboard),
				secondaryButton("Open log folder", func() { s.openPath(reportPath(s.outputPath, "logs")) }),
				layout.NewSpacer(),
			),
			s.logBox(),
		),
		container.NewHBox(
			dangerButton("Stop After Current ZIP", s.stopAfterCurrentZIP),
			dangerButton("Cancel Immediately", s.cancelImmediately),
			layout.NewSpacer(),
		),
		errorBanner("Stop After Current ZIP waits for current archive to finish. Cancel Immediately may stop file work mid-run."),
	)
}

func (s *guiState) buildDoneScreen() fyne.CanvasObject {
	outputText := s.outputPath
	if outputText == "" {
		outputText = "selected output folder"
	}
	stats := []fyne.CanvasObject{
		statCard(reportNumber(s.runReport, "files"), "files processed"),
		statCard(reportNumber(s.runReport, "metadata"), "metadata written"),
		statCard(reportNumber(s.runReport, "duplicates"), "duplicates removed"),
		statCard(reportNumber(s.runReport, "dates"), "suspicious dates"),
		statCard(reportNumber(s.runReport, "errors"), "errors"),
		statCard(reportNumber(s.runReport, "saved"), "saved space"),
	}
	return container.NewVBox(
		successBanner("All ZIPs processed successfully. Your fixed photos are in "+outputText+"."),
		container.NewGridWithColumns(3, stats...),
		warningBanner("Suspicious dates need review before you trust archive dates. Open the CSV from Reports."),
		container.NewHBox(
			secondaryButton("Open output folder", func() { s.openPath(s.outputPath) }),
			secondaryButton("Open final report", func() { s.openPath(reportPath(s.outputPath, "reports", "latest.txt")) }),
			secondaryButton("Start new job", func() {
				s.progressPhase = progressEmpty
				s.preflight = nil
				s.result = nil
				s.runReport = nil
				s.refreshTabs(0)
			}),
		),
	)
}

func (s *guiState) buildErrorScreen() fyne.CanvasObject {
	message := s.latestError
	if message == "" {
		message = "Unknown error."
	}
	items := []fyne.CanvasObject{
		sectionTitle("Preflight cannot start"),
		errorBanner(message),
		infoBanner("No source ZIP was changed. Fix error above, then run preflight again."),
	}
	actions := []fyne.CanvasObject{
		secondaryButton("Back to setup", func() { s.refreshTabs(0) }),
	}
	if s.resumeFound() {
		items = append(items, infoBanner("Previous session found. Resume from last safe ZIP, or reprocess selected ZIPs."))
		actions = append([]fyne.CanvasObject{
			secondaryButton("Resume", func() {
				s.reprocess = false
				s.startProcessing()
			}),
			secondaryButton("Start over", func() {
				s.reprocess = true
				s.startProcessing()
			}),
		}, actions...)
	}
	items = append(items, container.NewHBox(actions...))
	return container.NewVBox(items...)
}

func (s *guiState) buildEmptyProgressScreen() fyne.CanvasObject {
	return container.NewVBox(
		infoBanner("Run a preflight check first. Nothing is copied or changed until processing starts."),
		container.NewGridWithColumns(2,
			emptyCard("No ZIP sources added", "Add sources in Setup for batch mode."),
			emptyCard("No reports yet", "Run a job to generate a report."),
		),
		secondaryButton("Go to setup", func() { s.refreshTabs(0) }),
	)
}

func filePercent(done int, total int) float64 {
	if total <= 0 {
		return 0
	}
	return (float64(done) / float64(total)) * 100
}

func reportNumber(report *fixer.RunReport, name string) string {
	if report == nil {
		return "-"
	}
	switch name {
	case "files":
		return fmt.Sprintf("%d", report.Summary.TotalMedia)
	case "metadata":
		return fmt.Sprintf("%d", report.Summary.MetadataWritten)
	case "duplicates":
		return fmt.Sprintf("%d", report.Summary.DuplicatesLinked+report.Summary.DuplicatesCopied)
	case "dates":
		return fmt.Sprintf("%d", report.Summary.SuspiciousDates)
	case "errors":
		return fmt.Sprintf("%d", report.Summary.Errors)
	case "saved":
		return fixer.FormatBytes(report.Summary.ApproxDedupBytesSaved)
	default:
		return "-"
	}
}

func currentFileBase(path string) string {
	if path == "" {
		return "none"
	}
	return filepath.Base(path)
}
