package gui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestErrorScreenShowsOnlyRealError(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	state := &guiState{
		mode:        modeBatch,
		zipRoots:    []string{`D:\Takeout_Zips`},
		latestError: "MotionPhoto2 missing",
	}
	body := canvasObjectText(state.buildErrorScreen())

	if !strings.Contains(body, state.latestError) {
		t.Fatalf("real error missing from screen: %q", body)
	}
	for _, falseMessage := range []string{
		"No ZIP files found",
		"Not enough space",
		"ExifTool not found",
		"No ZIP sources added",
		"Previous session found",
	} {
		if strings.Contains(body, falseMessage) {
			t.Fatalf("false error %q shown with real error: %q", falseMessage, body)
		}
	}
}

func canvasObjectText(object fyne.CanvasObject) string {
	switch value := object.(type) {
	case *fyne.Container:
		parts := make([]string, 0, len(value.Objects))
		for _, child := range value.Objects {
			parts = append(parts, canvasObjectText(child))
		}
		return strings.Join(parts, "\n")
	case *container.Scroll:
		return canvasObjectText(value.Content)
	case *widget.Label:
		return value.Text
	case *canvas.Text:
		return value.Text
	default:
		return ""
	}
}
