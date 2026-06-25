package gui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
)

func TestPolishedButtonInteractionStates(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	tapped := 0
	button := primaryButton("Run preflight check", func() {
		tapped++
	})
	window := test.NewWindow(button)
	defer window.Close()

	button.MouseIn(&desktop.MouseEvent{})
	if !button.hovered {
		t.Fatal("button should track hover state")
	}

	button.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary})
	if !button.pressed {
		t.Fatal("button should track press state")
	}
	button.MouseUp(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary})
	if button.pressed {
		t.Fatal("button should clear press state")
	}

	button.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	if tapped != 1 {
		t.Fatalf("keyboard tap count = %d, want 1", tapped)
	}

	button.Disable()
	button.Tapped(nil)
	if tapped != 1 {
		t.Fatalf("disabled tap count = %d, want 1", tapped)
	}
}

func TestFitButtonText(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	got := fitButtonText("C:/very/long/google/takeout/source/folder/name", 80, buttonTextSize, fyne.TextStyle{Bold: true})
	if got == "" || got == "C:/very/long/google/takeout/source/folder/name" {
		t.Fatalf("fitButtonText did not shorten text: %q", got)
	}
}
