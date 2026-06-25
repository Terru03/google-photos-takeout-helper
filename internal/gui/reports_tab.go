package gui

import (
	"fmt"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
)

func (s *guiState) buildReportsTab() fyne.CanvasObject {
	s.loadReportIfAvailable()
	if s.runReport == nil {
		return container.NewVBox(
			container.NewGridWithColumns(2,
				emptyCard("No ZIP sources added", "Setup will show selected sources."),
				emptyCard("No reports yet", "Run a job to generate a report."),
			),
			infoBanner("Reports are stored in .gtf next to your output library."),
		)
	}

	rows := []reportRow{
		{"Files processed", fmt.Sprintf("%d", s.runReport.Summary.TotalMedia)},
		{"Metadata written", fmt.Sprintf("%d", s.runReport.Summary.MetadataWritten)},
		{"Metadata verified", fmt.Sprintf("%d", s.runReport.Summary.MetadataVerified)},
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
				secondaryButton("Errors only", func() { s.openPath(reportPath(s.outputPath, "reports", "latest.txt")) }),
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
