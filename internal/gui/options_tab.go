package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
)

func (s *guiState) buildOptionsTab() fyne.CanvasObject {
	recommended := primaryButton("Recommended Safe Mode", func() {
		s.options.WriteMetadata = true
		s.options.WriteXMPSidecars = false
		s.options.VerifyWrites = true
		s.options.RestoreMOVExtension = true
		s.options.Deduplicate = true
		s.options.AlbumMode = fixer.AlbumModeUniqueOnly
		s.options.IgnoreAlbums = false
		s.options.DryRun = false
		s.options.ConflictPolicy = fixer.ConflictMerge
		s.options.DeleteSourceAfterSuccess = false
		s.savePreferences()
		s.refreshTabs(3)
	})
	dryRun := secondaryButton("Audit Only", func() {
		s.options.WriteMetadata = false
		s.options.WriteXMPSidecars = false
		s.options.VerifyWrites = false
		s.options.RestoreMOVExtension = false
		s.options.Deduplicate = true
		s.options.DryRun = true
		s.options.ConflictPolicy = fixer.ConflictMerge
		s.options.DeleteSourceAfterSuccess = false
		s.savePreferences()
		s.refreshTabs(3)
	})
	immich := secondaryButton("Immich-ready", func() {
		s.options.WriteMetadata = true
		s.options.WriteXMPSidecars = true
		s.options.VerifyWrites = true
		s.options.RestoreMOVExtension = true
		s.options.Deduplicate = true
		s.options.KeepLiveVideo = true
		s.options.AlbumMode = fixer.AlbumModeUniqueOnly
		s.options.IgnoreAlbums = false
		s.options.DryRun = false
		s.options.ConflictPolicy = fixer.ConflictMerge
		s.options.DeleteSourceAfterSuccess = false
		s.savePreferences()
		s.refreshTabs(3)
	})

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
		s.check("Write metadata into files", &s.options.WriteMetadata),
		s.check("Write XMP sidecars", &s.options.WriteXMPSidecars),
		s.check("Remove duplicate copies", &s.options.Deduplicate),
		s.check("Create month subfolders", &s.options.MonthSubfolders),
		s.check("Verify metadata after writing", &s.options.VerifyWrites),
		s.check("Restore .MOV extension", &s.options.RestoreMOVExtension),
		s.check("Flatten all albums into one folder", &s.options.Flatten),
		s.check("Use symlinks for album duplicates", &s.options.UseSymlinks),
	)

	return container.NewVScroll(container.NewVBox(
		card("QUICK PRESETS", container.NewGridWithColumns(3, recommended, dryRun, immich)),
		card("OUTPUT ORGANISATION",
			outputGrid,
			sectionTitle("Album mode"),
			s.albumModeSelect(),
			smallText(albumModeHelp(s.currentOptions().AlbumMode)),
		),
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

func (s *guiState) albumModeSelect() *widget.Select {
	labels := []string{
		"Timeline + unique album files",
		"Timeline only",
		"All album folders",
	}
	selectBox := widget.NewSelect(labels, func(value string) {
		switch value {
		case "Timeline only":
			s.options.AlbumMode = fixer.AlbumModeTimelineOnly
			s.options.IgnoreAlbums = true
		case "All album folders":
			s.options.AlbumMode = fixer.AlbumModeAll
			s.options.IgnoreAlbums = false
		default:
			s.options.AlbumMode = fixer.AlbumModeUniqueOnly
			s.options.IgnoreAlbums = false
		}
		s.savePreferences()
	})
	switch s.currentOptions().AlbumMode {
	case fixer.AlbumModeTimelineOnly:
		selectBox.SetSelected("Timeline only")
	case fixer.AlbumModeAll:
		selectBox.SetSelected("All album folders")
	default:
		selectBox.SetSelected("Timeline + unique album files")
	}
	return selectBox
}

func albumModeHelp(mode fixer.AlbumMode) string {
	switch mode {
	case fixer.AlbumModeTimelineOnly:
		return "Only Photos from YYYY folders are written."
	case fixer.AlbumModeAll:
		return "Timeline and all album folders are written."
	default:
		return "Timeline is kept. Album files with exact copies in timeline are removed at end."
	}
}

func boolNote(label string, value bool) string {
	if value {
		return label + ": on"
	}
	return label + ": off"
}
