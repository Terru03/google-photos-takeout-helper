package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
)

func (s *guiState) buildOptionsTab() fyne.CanvasObject {
	recommended := primaryButton("Recommended", func() {
		s.options.WriteMetadata = true
		s.options.VerifyWrites = true
		s.options.RestoreMOVExtension = true
		s.options.Deduplicate = true
		s.options.DryRun = false
		s.options.ConflictPolicy = fixer.ConflictMerge
		s.options.DeleteSourceAfterSuccess = false
		s.savePreferences()
		s.refreshTabs(3)
	})
	dryRun := secondaryButton("Dry run", func() {
		s.options.WriteMetadata = true
		s.options.VerifyWrites = true
		s.options.RestoreMOVExtension = true
		s.options.Deduplicate = true
		s.options.DryRun = true
		s.options.ConflictPolicy = fixer.ConflictMerge
		s.options.DeleteSourceAfterSuccess = false
		s.savePreferences()
		s.refreshTabs(3)
	})

	preserveAlbums := !s.options.IgnoreAlbums
	preserveCheck := widget.NewCheck("Ignore album folder structure", func(value bool) {
		s.options.IgnoreAlbums = value
		s.savePreferences()
	})
	preserveCheck.SetChecked(s.options.IgnoreAlbums)

	conflict := widget.NewSelect([]string{"Merge", "Prefer Google sidecar", "Prefer embedded"}, func(value string) {
		switch value {
		case "Prefer Google sidecar":
			s.options.ConflictPolicy = fixer.ConflictPreferJSON
		case "Prefer embedded":
			s.options.ConflictPolicy = fixer.ConflictPreferEmbedded
		default:
			s.options.ConflictPolicy = fixer.ConflictMerge
		}
		s.savePreferences()
	})
	switch s.options.ConflictPolicy {
	case fixer.ConflictPreferJSON:
		conflict.SetSelected("Prefer Google sidecar")
	case fixer.ConflictPreferEmbedded:
		conflict.SetSelected("Prefer embedded")
	default:
		conflict.SetSelected("Merge")
	}

	sourceDelete := s.check("Delete source folder after a clean run", &s.options.DeleteSourceAfterSuccess)
	keepTemp := s.check("Keep temp folder if an error occurs", &s.keepTempOnErr)
	destructive := widget.NewAccordion(widget.NewAccordionItem("Advanced danger options", coloredPanel(colRedBg, colRed, cardRadius, container.NewVBox(
		checkboxGrid(sourceDelete),
		errorBanner("This can remove an extracted source folder after a clean folder-mode run. Batch ZIP mode ignores this, and source ZIP files are never deleted."),
	))))
	destructive.CloseAll()

	outputGrid := checkboxGrid(
		s.check("Extract metadata into photos", &s.options.WriteMetadata),
		s.check("Remove duplicate copies", &s.options.Deduplicate),
		s.check("Create month subfolders", &s.options.MonthSubfolders),
		preserveCheck,
		s.check("Verify metadata after writing", &s.options.VerifyWrites),
		s.check("Restore .MOV extension", &s.options.RestoreMOVExtension),
		s.check("Flatten all albums into one folder", &s.options.Flatten),
		s.check("Use symlinks for album duplicates", &s.options.UseSymlinks),
	)

	return container.NewVScroll(container.NewVBox(
		card("QUICK PRESETS", container.NewGridWithColumns(2, recommended, dryRun)),
		card("OUTPUT ORGANISATION", outputGrid, smallText(boolNote("Album folders preserved", preserveAlbums))),
		card("MOTION & LIVE PHOTOS",
			checkboxGrid(
				s.check("Create Windows Motion Photos", &s.options.CreateMotionPhotos),
				s.check("Keep live-video copies", &s.options.KeepLiveVideo),
			),
		),
		card("METADATA CONFLICT POLICY", conflict),
		destructive,
		card("DEVELOPER / DIAGNOSTIC",
			checkboxGrid(
				s.check("Dry run", &s.options.DryRun),
				keepTemp,
			),
			smallText("Logs and reports are written to .gtf beside output."),
		),
	))
}

func boolNote(label string, value bool) string {
	if value {
		return label + ": on"
	}
	return label + ": off"
}
