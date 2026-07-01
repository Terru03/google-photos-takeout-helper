package gui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
)

func coloredPanel(bg color.Color, border color.Color, radius float32, content fyne.CanvasObject) fyne.CanvasObject {
	rect := canvas.NewRectangle(bg)
	rect.StrokeColor = border
	rect.StrokeWidth = 1
	rect.CornerRadius = radius
	return container.NewStack(rect, container.NewPadded(content))
}

func card(title string, objects ...fyne.CanvasObject) fyne.CanvasObject {
	items := make([]fyne.CanvasObject, 0, len(objects)+1)
	if title != "" {
		items = append(items, sectionTitle(title))
	}
	items = append(items, objects...)
	return coloredPanel(colPanel, colBorder, cardRadius, container.NewVBox(items...))
}

func compactCard(objects ...fyne.CanvasObject) fyne.CanvasObject {
	return coloredPanel(colPanel, colBorder, cardRadius, container.NewVBox(objects...))
}

func fieldBox(text string) fyne.CanvasObject {
	if strings.TrimSpace(text) == "" {
		text = "Not selected"
	}
	label := widget.NewLabel(shortDisplayText(text, 96))
	label.Wrapping = fyne.TextWrapOff
	label.Truncation = fyne.TextTruncateEllipsis
	return coloredPanel(colField, colBorder, fieldRadius, label)
}

func sectionTitle(text string) fyne.CanvasObject {
	t := canvas.NewText(text, colText)
	t.TextStyle = fyne.TextStyle{Bold: true}
	t.TextSize = 12
	return t
}

func smallText(text string) fyne.CanvasObject {
	t := canvas.NewText(shortDisplayText(text, 96), colMuted)
	t.TextSize = 10
	return t
}

func badge(text string, bg color.Color, fg color.Color) fyne.CanvasObject {
	t := canvas.NewText(text, fg)
	t.TextSize = 10
	t.TextStyle = fyne.TextStyle{Bold: true}
	return coloredPanel(bg, bg, 6, container.NewPadded(t))
}

func infoBanner(text string) fyne.CanvasObject {
	return banner("[i]", text, colBlueBg, colBlue)
}

func warningBanner(text string) fyne.CanvasObject {
	return banner("[!]", text, colYellowBg, colYellow)
}

func errorBanner(text string) fyne.CanvasObject {
	return banner("[x]", text, colRedBg, colRed)
}

func successBanner(text string) fyne.CanvasObject {
	return banner("[ok]", text, colGreenBg, colGreen)
}

func banner(prefix string, text string, bg color.Color, border color.Color) fyne.CanvasObject {
	prefixText := canvas.NewText(prefix, border)
	prefixText.TextStyle = fyne.TextStyle{Bold: true}
	prefixText.TextSize = 11
	label := widget.NewLabel(wrapLongWords(text, 70))
	label.Wrapping = fyne.TextWrapWord
	return coloredPanel(bg, border, 6, container.NewBorder(nil, nil, prefixText, nil, label))
}

func statCard(value string, label string) fyne.CanvasObject {
	valueText := canvas.NewText(value, colText)
	valueText.TextSize = 20
	valueText.TextStyle = fyne.TextStyle{Bold: true}
	labelText := canvas.NewText(label, colMuted)
	labelText.TextSize = 10
	return coloredPanel(colPanelDark, colBorder, 6, container.NewVBox(valueText, labelText))
}

func statGrid(cards ...fyne.CanvasObject) fyne.CanvasObject {
	return container.NewGridWithColumns(5, cards...)
}

func pathPickerRow(label string, path string, buttonText string, tap func()) fyne.CanvasObject {
	button := secondaryButton(buttonText, tap)
	return card(label, container.NewBorder(nil, nil, nil, button, fieldBox(path)))
}

func checkboxGrid(checks ...*widget.Check) fyne.CanvasObject {
	objects := make([]fyne.CanvasObject, 0, len(checks))
	for _, check := range checks {
		objects = append(objects, check)
	}
	return container.NewGridWithColumns(2, objects...)
}

func progressSection(label string, value float64) fyne.CanvasObject {
	bar := widget.NewProgressBar()
	bar.Min = 0
	bar.Max = 100
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	bar.SetValue(value)
	return container.NewVBox(sectionTitle(label), bar)
}

func stageRow(active string) fyne.CanvasObject {
	stages := []string{"Extract", "Metadata", "Deduplicate", "Reports", "Cleanup"}
	items := make([]fyne.CanvasObject, 0, len(stages))
	for _, stage := range stages {
		if strings.EqualFold(stage, active) {
			items = append(items, badge(stage, colGreenBg, colGreen))
		} else {
			items = append(items, badge(stage, colField, colMuted))
		}
	}
	return container.NewHBox(items...)
}

func (s *guiState) logBox() fyne.CanvasObject {
	scroll := s.ensureLogView()
	return coloredPanel(color.NRGBA{R: 0x17, G: 0x18, B: 0x16, A: 0xff}, colBorder, fieldRadius, scroll)
}

func (s *guiState) ensureLogView() *container.Scroll {
	if s.logLabel == nil {
		label := widget.NewLabel("")
		label.Wrapping = fyne.TextWrapOff
		label.TextStyle = fyne.TextStyle{Monospace: true}
		s.logLabel = label
	}
	if s.logScroll == nil {
		scroll := container.NewScroll(s.logLabel)
		scroll.SetMinSize(fyne.NewSize(0, 190))
		scroll.OnScrolled = func(position fyne.Position) {
			s.logAutoScroll = logScrollAtBottom(position, scroll.Size(), scroll.Content.MinSize())
		}
		s.logScroll = scroll
		if !s.logAutoScroll {
			s.logAutoScroll = true
		}
	}
	s.updateLogView()
	return s.logScroll
}

func (s *guiState) updateLogView() {
	if s.logLabel == nil {
		return
	}
	body := logTextForDisplay(s.logLines)
	if s.logLabel.Text == body {
		return
	}
	shouldFollow := s.logAutoScroll
	if s.logScroll != nil {
		shouldFollow = shouldFollow || logScrollAtBottom(s.logScroll.Offset, s.logScroll.Size(), s.logScroll.Content.MinSize())
	}
	s.logLabel.SetText(body)
	s.logLabel.Refresh()
	if shouldFollow && s.logScroll != nil && s.logScroll.Size().Height > 0 {
		s.logScroll.ScrollToBottom()
		s.logAutoScroll = true
	}
}

func (s *guiState) copyLogToClipboard() {
	if s.app == nil {
		return
	}
	s.app.Clipboard().SetContent(logTextForDisplay(s.logLines))
}

func logTextForDisplay(lines []string) string {
	if len(lines) == 0 {
		return "[info] Logs will appear here."
	}
	return strings.Join(lines, "\n")
}

func logScrollAtBottom(offset fyne.Position, viewport fyne.Size, content fyne.Size) bool {
	if viewport.Height <= 0 || content.Height <= viewport.Height {
		return true
	}
	return offset.Y+viewport.Height >= content.Height-8
}

func emptyCard(title string, body string) fyne.CanvasObject {
	titleText := canvas.NewText(title, colText)
	titleText.TextStyle = fyne.TextStyle{Bold: true}
	titleText.Alignment = fyne.TextAlignCenter
	titleText.TextSize = 12
	bodyText := widget.NewLabel(body)
	bodyText.Alignment = fyne.TextAlignCenter
	bodyText.Wrapping = fyne.TextWrapWord
	return coloredPanel(colPanelDark, color.NRGBA{R: 0x2b, G: 0x2d, B: 0x2a, A: 0xff}, cardRadius, container.NewVBox(layout.NewSpacer(), titleText, bodyText, layout.NewSpacer()))
}

func formatBytes(value int64) string {
	return fixer.FormatBytes(value)
}

func formatCount(value int) string {
	return fmt.Sprintf("%d", value)
}

func cleanPathName(path string) string {
	if strings.TrimSpace(path) == "" {
		return "not selected"
	}
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) {
		return shortDisplayText(path, 96)
	}
	return shortDisplayText(base, 96)
}

func shortDisplayText(text string, max int) string {
	runes := []rune(text)
	if max <= 0 || len(runes) <= max {
		return text
	}
	if max <= 6 {
		return string(runes[:max])
	}
	head := max / 2
	tail := max - head - 3
	return string(runes[:head]) + "..." + string(runes[len(runes)-tail:])
}

func wrapLongWords(text string, maxWord int) string {
	if maxWord <= 0 || text == "" {
		return text
	}
	var b strings.Builder
	wordLen := 0
	for _, r := range text {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			wordLen = 0
			b.WriteRune(r)
			continue
		}
		if r == '\\' || r == '/' {
			wordLen = 0
			b.WriteRune(r)
			b.WriteRune(' ')
			continue
		}
		if wordLen >= maxWord {
			b.WriteRune('\n')
			wordLen = 0
		}
		b.WriteRune(r)
		wordLen++
	}
	return b.String()
}

func reportPath(output string, parts ...string) string {
	if strings.TrimSpace(output) == "" {
		return ""
	}
	all := append([]string{output, ".gtf"}, parts...)
	return filepath.Join(all...)
}

func separator() fyne.CanvasObject {
	return widget.NewSeparator()
}
