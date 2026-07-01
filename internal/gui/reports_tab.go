package gui

import (
	"fmt"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
)

func (s *guiState) buildReportsTab() fyne.CanvasObject {
	s.loadReportIfAvailable()
	if s.runReport == nil {
		body := "Run a job to generate a report."
		actions := []fyne.CanvasObject{infoBanner("Reports are stored in .gtf next to your output library.")}
		if s.outputPath != "" {
			body = "No latest report file found in the selected output folder yet."
			actions = append(actions, secondaryButton("Open .gtf folder", func() { s.openPath(reportPath(s.outputPath)) }))
		}
		items := []fyne.CanvasObject{
			container.NewGridWithColumns(2,
				emptyCard("No ZIP sources added", "Setup will show selected sources."),
				emptyCard("No reports yet", body),
			),
		}
		items = append(items, actions...)
		return container.NewVBox(items...)
	}

	rows := []reportRow{
		{"Input media found", fmt.Sprintf("%d", s.runReport.Summary.TotalMedia)},
		{"JSON sidecars found", fmt.Sprintf("%d", s.runReport.Summary.JSONSidecarsFound)},
		{"Matched cleanly", fmt.Sprintf("%d", s.runReport.Summary.MatchedCleanly)},
		{"Matched with fallback", fmt.Sprintf("%d", s.runReport.Summary.MatchedWithFallback)},
		{"Unmatched", fmt.Sprintf("%d", s.runReport.Summary.Unmatched)},
		{"Ambiguous", fmt.Sprintf("%d", s.runReport.Summary.Ambiguous)},
		{"Output media count", fmt.Sprintf("%d", s.runReport.Summary.OutputMedia)},
		{"Metadata written", fmt.Sprintf("%d", s.runReport.Summary.MetadataWritten)},
		{"XMP sidecars written", fmt.Sprintf("%d", s.runReport.Summary.XMPSidecarsWritten)},
		{"Metadata verified", fmt.Sprintf("%d", s.runReport.Summary.MetadataVerified)},
		{"Verification failures", fmt.Sprintf("%d", s.runReport.Summary.MetadataVerificationFailures)},
		{"Duplicates removed", fmt.Sprintf("%d", s.runReport.Summary.DuplicatesLinked+s.runReport.Summary.DuplicatesCopied)},
		{"Space saved (dedupe)", fixer.FormatBytes(s.runReport.Summary.ApproxDedupBytesSaved)},
		{"Motion photos created", motionCreated(s.runReport)},
		{"Suspicious dates", fmt.Sprintf("%d", s.runReport.Summary.SuspiciousDates)},
		{"Errors", fmt.Sprintf("%d", s.runReport.Summary.Errors)},
		{"Duration", s.durationText()},
	}

	return container.NewVBox(
		sectionTitle("Latest run completed: "+s.runReport.FinishedAt.Format("Jan 02 2006 15:04")),
		card("REPORT SUMMARY", reportTable(rows)),
		card("SAVED REPORT FILES",
			container.NewGridWithColumns(2,
				secondaryButton("Full audit report", func() { s.openPath(reportPath(s.outputPath, "reports", "latest.txt")) }),
				secondaryButton("Suspicious dates CSV", func() { s.openPath(reportPath(s.outputPath, "reports", "suspicious_dates.csv")) }),
				secondaryButton("All records JSON", func() { s.openPath(reportPath(s.outputPath, "reports", "latest.json")) }),
				secondaryButton("Review CSV", func() { s.openPath(reportPath(s.outputPath, "reports", "review.csv")) }),
				secondaryButton("Open .gtf folder", func() { s.openPath(reportPath(s.outputPath)) }),
			),
		),
		infoBanner("Reports are stored in .gtf next to your output library."),
	)
}

type reportRow struct {
	label string
	value string
}

func reportTable(rows []reportRow) fyne.CanvasObject {
	table := widget.NewTable(
		func() (int, int) { return len(rows), 2 },
		func() fyne.CanvasObject { return widget.NewLabel("template") },
		func(id widget.TableCellID, object fyne.CanvasObject) {
			label := object.(*widget.Label)
			if id.Col == 0 {
				label.SetText(rows[id.Row].label)
				label.TextStyle = fyne.TextStyle{}
			} else {
				label.SetText(rows[id.Row].value)
				label.TextStyle = fyne.TextStyle{Bold: true}
				label.Alignment = fyne.TextAlignTrailing
			}
		},
	)
	table.SetColumnWidth(0, 240)
	table.SetColumnWidth(1, 180)
	scroll := container.NewVScroll(table)
	scroll.SetMinSize(fyne.NewSize(0, 245))
	return scroll
}

func motionCreated(report *fixer.RunReport) string {
	if report == nil || report.MotionPhotoPass == nil {
		return "0"
	}
	return fmt.Sprintf("%d", report.MotionPhotoPass.EmbeddedSuccessfully)
}

func reportFile(output string, name string) string {
	return filepath.Join(output, ".gtf", "reports", name)
}
