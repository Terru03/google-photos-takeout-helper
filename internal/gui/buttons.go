package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type buttonVariant int

const (
	buttonPrimary buttonVariant = iota
	buttonSecondary
	buttonDanger
	buttonTab
)

type polishedButton struct {
	widget.BaseWidget

	Text     string
	Icon     fyne.Resource
	OnTapped func()

	variant  buttonVariant
	selected bool
	disabled bool
	hovered  bool
	pressed  bool
	focused  bool
	minWidth float32
}

var _ fyne.Focusable = (*polishedButton)(nil)
var _ fyne.Tappable = (*polishedButton)(nil)
var _ desktop.Hoverable = (*polishedButton)(nil)
var _ desktop.Mouseable = (*polishedButton)(nil)
var _ desktop.Cursorable = (*polishedButton)(nil)

func newPolishedButton(text string, variant buttonVariant, tap func()) *polishedButton {
	button := &polishedButton{
		Text:     text,
		OnTapped: tap,
		variant:  variant,
		minWidth: 96,
	}
	button.ExtendBaseWidget(button)
	return button
}

func primaryButton(text string, tap func()) *polishedButton {
	button := newPolishedButton(text, buttonPrimary, tap)
	button.minWidth = 132
	return button
}

func secondaryButton(text string, tap func()) *polishedButton {
	return newPolishedButton(text, buttonSecondary, tap)
}

func dangerButton(text string, tap func()) *polishedButton {
	button := newPolishedButton(text, buttonDanger, tap)
	button.minWidth = 132
	return button
}

func choiceButton(text string, selected bool, tap func()) *polishedButton {
	button := secondaryButton(text, tap)
	button.selected = selected
	return button
}

func folderButton(label string, tap func()) *polishedButton {
	button := secondaryButton(label, tap)
	button.Icon = theme.FolderOpenIcon()
	return button
}

func (b *polishedButton) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = buttonRadius
	label := canvas.NewText(b.Text, colText)
	label.TextSize = buttonTextSize
	label.TextStyle = fyne.TextStyle{Bold: true}
	label.Alignment = fyne.TextAlignCenter
	objects := []fyne.CanvasObject{bg}
	var icon *canvas.Image
	if b.Icon != nil {
		icon = canvas.NewImageFromResource(b.Icon)
		icon.FillMode = canvas.ImageFillContain
		objects = append(objects, icon)
	}
	objects = append(objects, label)
	renderer := &polishedButtonRenderer{
		button:  b,
		bg:      bg,
		icon:    icon,
		label:   label,
		objects: objects,
	}
	renderer.Refresh()
	return renderer
}

func (b *polishedButton) Cursor() desktop.Cursor {
	if b.disabled {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

func (b *polishedButton) Disable() {
	b.disabled = true
	b.pressed = false
	b.Refresh()
}

func (b *polishedButton) Enable() {
	b.disabled = false
	b.Refresh()
}

func (b *polishedButton) Disabled() bool {
	return b.disabled
}

func (b *polishedButton) SetSelected(selected bool) {
	b.selected = selected
	b.Refresh()
}

func (b *polishedButton) FocusGained() {
	b.focused = true
	b.Refresh()
}

func (b *polishedButton) FocusLost() {
	b.focused = false
	b.pressed = false
	b.Refresh()
}

func (b *polishedButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
}

func (b *polishedButton) MouseMoved(*desktop.MouseEvent) {
}

func (b *polishedButton) MouseOut() {
	b.hovered = false
	b.pressed = false
	b.Refresh()
}

func (b *polishedButton) MouseDown(event *desktop.MouseEvent) {
	if b.disabled || event.Button != desktop.MouseButtonPrimary {
		return
	}
	b.focusSelf()
	b.pressed = true
	b.Refresh()
}

func (b *polishedButton) MouseUp(*desktop.MouseEvent) {
	if b.pressed {
		b.pressed = false
		b.Refresh()
	}
}

func (b *polishedButton) Tapped(*fyne.PointEvent) {
	if b.disabled {
		return
	}
	b.focusSelf()
	if b.OnTapped != nil {
		b.OnTapped()
	}
}

func (b *polishedButton) TypedRune(rune) {
}

func (b *polishedButton) TypedKey(event *fyne.KeyEvent) {
	if event.Name == fyne.KeySpace || event.Name == fyne.KeyReturn || event.Name == fyne.KeyEnter {
		b.Tapped(nil)
	}
}

func (b *polishedButton) focusSelf() {
	if fyne.CurrentApp() == nil {
		return
	}
	if canvas := fyne.CurrentApp().Driver().CanvasForObject(b); canvas != nil {
		canvas.Focus(b)
	}
}

func (b *polishedButton) buttonColors() (color.Color, color.Color, color.Color) {
	fill := colField
	stroke := colBorder
	foreground := colText

	switch b.variant {
	case buttonPrimary:
		fill = colBlueBg
		stroke = colBlue
	case buttonDanger:
		fill = colRedBg
		stroke = colRed
	case buttonTab:
		fill = colTitleBar
		stroke = colBorderSoft
	default:
		fill = colField
		stroke = colBorder
	}

	if b.selected {
		fill = colPanel
		stroke = colBlue
		foreground = colWhite
	}
	if b.hovered && !b.disabled {
		switch b.variant {
		case buttonPrimary:
			fill = colBlueHover
		case buttonDanger:
			fill = colRedHover
		default:
			fill = colFieldHover
		}
	}
	if b.pressed && !b.disabled {
		switch b.variant {
		case buttonPrimary:
			fill = colBluePress
		case buttonDanger:
			fill = colRedPress
		default:
			fill = colFieldPress
		}
	}
	if b.focused && !b.disabled {
		stroke = colBlue
	}
	if b.disabled {
		fill = colPanelDark
		stroke = colBorderSoft
		foreground = colMuted
	}
	return fill, stroke, foreground
}

type polishedButtonRenderer struct {
	button  *polishedButton
	bg      *canvas.Rectangle
	icon    *canvas.Image
	label   *canvas.Text
	objects []fyne.CanvasObject
}

func (r *polishedButtonRenderer) Destroy() {
}

func (r *polishedButtonRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	iconSize := float32(0)
	gap := float32(0)
	if r.icon != nil {
		iconSize = 14
		gap = 7
	}
	labelText := fitButtonText(r.button.Text, size.Width-(buttonPadX*2)-iconSize-gap, buttonTextSize, r.label.TextStyle)
	if r.label.Text != labelText {
		r.label.Text = labelText
		r.label.Refresh()
	}
	labelSize := fyne.MeasureText(labelText, buttonTextSize, r.label.TextStyle)
	totalWidth := labelSize.Width + iconSize + gap
	startX := (size.Width - totalWidth) / 2
	if startX < 2 {
		startX = 2
	}
	if r.icon != nil {
		r.icon.Resize(fyne.NewSquareSize(iconSize))
		r.icon.Move(fyne.NewPos(startX, (size.Height-iconSize)/2))
	}
	r.label.Resize(labelSize)
	r.label.Move(fyne.NewPos(startX+iconSize+gap, (size.Height-labelSize.Height)/2-1))
}

func (r *polishedButtonRenderer) MinSize() fyne.Size {
	width := fyne.MeasureText(r.button.Text, buttonTextSize, fyne.TextStyle{Bold: true}).Width + buttonPadX*2
	if r.button.Icon != nil {
		width += 21
	}
	if width < r.button.minWidth {
		width = r.button.minWidth
	}
	if width > 260 {
		width = 260
	}
	return fyne.NewSize(width, buttonHeight)
}

func (r *polishedButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *polishedButtonRenderer) Refresh() {
	fill, stroke, foreground := r.button.buttonColors()
	r.bg.FillColor = fill
	r.bg.StrokeColor = stroke
	if r.button.focused && !r.button.disabled {
		r.bg.StrokeWidth = 2
	} else {
		r.bg.StrokeWidth = 1
	}
	r.bg.CornerRadius = buttonRadius
	r.label.Color = foreground
	r.label.TextSize = buttonTextSize
	r.label.TextStyle = fyne.TextStyle{Bold: true}
	if r.icon != nil {
		r.icon.Resource = r.button.Icon
		r.icon.Refresh()
	}
	r.Layout(r.button.Size())
	r.bg.Refresh()
	r.label.Refresh()
}

func fitButtonText(text string, maxWidth float32, size float32, style fyne.TextStyle) string {
	if maxWidth <= 0 {
		return "..."
	}
	if fyne.MeasureText(text, size, style).Width <= maxWidth {
		return text
	}
	runes := []rune(text)
	if len(runes) <= 3 {
		return text
	}
	low := 0
	high := len(runes)
	for low < high {
		mid := (low + high + 1) / 2
		candidate := string(runes[:mid]) + "..."
		if fyne.MeasureText(candidate, size, style).Width <= maxWidth {
			low = mid
		} else {
			high = mid - 1
		}
	}
	if low <= 0 {
		return "..."
	}
	return string(runes[:low]) + "..."
}
