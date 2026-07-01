package gui

import (
	"fmt"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
)

func TestLogTextForDisplayKeepsManyLines(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%02d", i+1)
	}

	body := logTextForDisplay(lines)

	if !strings.Contains(body, "line-01") || !strings.Contains(body, "line-20") {
		t.Fatalf("expected log text to include first and last lines, got %q", body)
	}
	if strings.Count(body, "\n") != 19 {
		t.Fatalf("expected all lines in log text, got %q", body)
	}
}

func TestLogScrollAtBottom(t *testing.T) {
	viewport := fyne.NewSize(400, 100)
	content := fyne.NewSize(800, 500)

	if logScrollAtBottom(fyne.NewPos(0, 50), viewport, content) {
		t.Fatal("expected log to pause following when user scrolled up")
	}
	if !logScrollAtBottom(fyne.NewPos(0, 400), viewport, content) {
		t.Fatal("expected log to follow when user is at bottom")
	}
	if !logScrollAtBottom(fyne.NewPos(0, 0), viewport, fyne.NewSize(400, 80)) {
		t.Fatal("expected short log content to count as bottom")
	}
}
