package gui

import (
	"fmt"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
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

func TestReadOnlyLogEntryBlocksEditingKeys(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	entry := newReadOnlyLogEntry()
	entry.SetText("line one")
	entry.TypedRune('x')
	entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyBackspace})
	entry.TypedShortcut(&fyne.ShortcutPaste{Clipboard: app.Clipboard()})

	if entry.Text != "line one" {
		t.Fatalf("expected read-only log entry text to stay unchanged, got %q", entry.Text)
	}
}
