package gui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

func (s *guiState) buildSetupTab() fyne.CanvasObject {
	modeFolderButton := secondaryButton("Already extracted folder", func() {
		s.mode = modeFolder
		s.refreshTabs(0)
	})
	modeBatchButton := secondaryButton("Batch ZIP processing", func() {
		s.mode = modeBatch
		s.refreshTabs(0)
	})
	if s.mode == modeFolder {
		modeFolderButton.Importance = widget.HighImportance
	} else {
		modeBatchButton.Importance = widget.HighImportance
	}

	sourceSection := s.buildSourceSection()
	outputSection := pathPickerRow("OUTPUT FOLDER", s.outputPath, "Change folder", s.selectOutputFolder)
	workSection := fyne.CanvasObject(nil)
	if s.mode == modeBatch {
		workSection = pathPickerRow("TEMPORARY WORK FOLDER", s.workPath, "Change folder", s.selectWorkFolder)
	}

	warnings := []fyne.CanvasObject{}
	if s.sameDriveWarning() {
		warnings = append(warnings, warningBanner("Output folder has the same drive as the source ZIPs. This is fine, but may be slower. A different drive is recommended for large exports."))
	}
	if s.resumeFound() {
		warnings = append(warnings, infoBanner("Previous session found. Resume from where you left off, or start over from scratch."))
		warnings = append(warnings, container.NewHBox(
			secondaryButton("Resume", func() {
				s.reprocess = false
				s.startProcessing()
			}),
			secondaryButton("Start over", func() {
				s.reprocess = true
				s.startProcessing()
			}),
		))
	}

	safeOptions := card("SAFE OPTIONS",
		checkboxGrid(
			s.check("Extract metadata into photos", &s.options.WriteMetadata),
			s.check("Verify metadata after writing", &s.options.VerifyWrites),
			s.check("Restore .MOV extension", &s.options.RestoreMOVExtension),
			s.check("Remove duplicate copies", &s.options.Deduplicate),
			s.check("Create month subfolders", &s.options.MonthSubfolders),
			s.performAlbumsCheck(),
		),
	)

	advanced := widget.NewAccordion(
		widget.NewAccordionItem("Advanced options", container.NewVBox(
			checkboxGrid(
				s.check("Dry run", &s.options.DryRun),
				s.check("Create Windows Motion Photos", &s.options.CreateMotionPhotos),
				s.check("Keep live-video copies", &s.options.KeepLiveVideo),
				s.check("Keep temp folder if an error occurs", &s.keepTempOnErr),
			),
		)),
	)
	advanced.CloseAll()

	start := primaryButton("Run preflight check", s.runPreflight)

	items := []fyne.CanvasObject{
		infoBanner("Your original ZIP files are never touched. This app reads your Takeout export and writes fixed photos to a separate output folder you choose."),
		card("MODE", container.NewGridWithColumns(2, modeFolderButton, modeBatchButton)),
		sourceSection,
		outputSection,
	}
	if workSection != nil {
		items = append(items, workSection)
	}
	items = append(items, warnings...)
	items = append(items, safeOptions, advanced, start)
	if s.mode == modeBatch && len(s.zipRoots) == 0 {
		items = append(items, container.NewGridWithColumns(2,
			emptyCard("No ZIP sources added", "Add a folder or ZIP file above to get started."),
			emptyCard("No reports yet", "Run a job to generate a report."),
		))
	}

	return container.NewVScroll(container.NewVBox(items...))
}

func (s *guiState) buildSourceSection() fyne.CanvasObject {
	if s.mode == modeFolder {
		return pathPickerRow("SOURCE FOLDER", s.inputPath, "Choose folder", s.selectInputFolder)
	}

	chips := container.NewVBox()
	if len(s.zipRoots) == 0 {
		chips.Add(fieldBox("No ZIP sources added"))
	} else {
		for _, root := range s.zipRoots {
			root := root
			label := root
			if len(label) > 80 {
				label = fmt.Sprintf("...%s", label[len(label)-77:])
			}
			button := secondaryButton(label, func() {
				s.selectedRoot = root
				s.refreshTabs(0)
			})
			if root == s.selectedRoot {
				button.Importance = widget.HighImportance
			}
			chips.Add(button)
		}
	}

	return card("SOURCE FOLDERS",
		chips,
		container.NewHBox(
			folderButton("Add folder", s.addZipFolder),
			secondaryButton("Add ZIP files", s.addZipFiles),
			secondaryButton("Remove selected", s.removeSelectedRoot),
			layout.NewSpacer(),
			smallText(fmt.Sprintf("%d selected", len(s.zipRoots))),
		),
	)
}

func (s *guiState) check(label string, target *bool) *widget.Check {
	check := widget.NewCheck(label, func(value bool) {
		*target = value
		s.savePreferences()
	})
	check.SetChecked(*target)
	return check
}

func (s *guiState) performAlbumsCheck() *widget.Check {
	check := widget.NewCheck("Perform album structure", func(value bool) {
		s.options.IgnoreAlbums = !value
		s.savePreferences()
	})
	check.SetChecked(!s.options.IgnoreAlbums)
	return check
}
