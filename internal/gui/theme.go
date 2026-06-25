package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

var (
	colBackground = color.NRGBA{R: 0x18, G: 0x18, B: 0x16, A: 0xff}
	colTitleBar   = color.NRGBA{R: 0x20, G: 0x20, B: 0x1e, A: 0xff}
	colPanel      = color.NRGBA{R: 0x26, G: 0x27, B: 0x24, A: 0xff}
	colPanelDark  = color.NRGBA{R: 0x20, G: 0x21, B: 0x1f, A: 0xff}
	colField      = color.NRGBA{R: 0x1c, G: 0x1d, B: 0x1b, A: 0xff}
	colFieldHover = color.NRGBA{R: 0x28, G: 0x2a, B: 0x26, A: 0xff}
	colFieldPress = color.NRGBA{R: 0x16, G: 0x17, B: 0x15, A: 0xff}
	colBorder     = color.NRGBA{R: 0x48, G: 0x49, B: 0x44, A: 0xff}
	colBorderSoft = color.NRGBA{R: 0x36, G: 0x37, B: 0x33, A: 0xff}
	colText       = color.NRGBA{R: 0xf0, G: 0xf0, B: 0xe8, A: 0xff}
	colMuted      = color.NRGBA{R: 0xa5, G: 0xa7, B: 0x9f, A: 0xff}
	colBlue       = color.NRGBA{R: 0x2f, G: 0x6f, B: 0xb7, A: 0xff}
	colBlueBg     = color.NRGBA{R: 0x1d, G: 0x3c, B: 0x60, A: 0xff}
	colBlueHover  = color.NRGBA{R: 0x27, G: 0x54, B: 0x81, A: 0xff}
	colBluePress  = color.NRGBA{R: 0x16, G: 0x2f, B: 0x4d, A: 0xff}
	colYellow     = color.NRGBA{R: 0xbd, G: 0x84, B: 0x11, A: 0xff}
	colYellowBg   = color.NRGBA{R: 0x55, G: 0x3f, B: 0x09, A: 0xff}
	colRed        = color.NRGBA{R: 0xd6, G: 0x4a, B: 0x43, A: 0xff}
	colRedBg      = color.NRGBA{R: 0x62, G: 0x22, B: 0x21, A: 0xff}
	colRedHover   = color.NRGBA{R: 0x80, G: 0x2d, B: 0x2a, A: 0xff}
	colRedPress   = color.NRGBA{R: 0x4b, G: 0x18, B: 0x17, A: 0xff}
	colGreen      = color.NRGBA{R: 0x45, G: 0xc4, B: 0x62, A: 0xff}
	colGreenBg    = color.NRGBA{R: 0x1d, G: 0x4b, B: 0x25, A: 0xff}
	colWhite      = color.NRGBA{R: 0xf2, G: 0xf0, B: 0xea, A: 0xff}
	colBlack      = color.NRGBA{R: 0x12, G: 0x12, B: 0x10, A: 0xff}
)

const (
	cardRadius     float32 = 8
	fieldRadius    float32 = 6
	buttonRadius   float32 = 6
	buttonHeight   float32 = 34
	buttonPadX     float32 = 13
	buttonTextSize float32 = 12
	tabCount               = 4
)

type helperTheme struct{}

func (helperTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return colBackground
	case theme.ColorNameHeaderBackground, theme.ColorNameMenuBackground:
		return colTitleBar
	case theme.ColorNameButton:
		return colField
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 0x2c, G: 0x2d, B: 0x2a, A: 0xff}
	case theme.ColorNameInputBackground:
		return colField
	case theme.ColorNameInputBorder, theme.ColorNameSeparator, theme.ColorNameShadow:
		return colBorder
	case theme.ColorNameForeground:
		return colText
	case theme.ColorNameDisabled, theme.ColorNamePlaceHolder:
		return colMuted
	case theme.ColorNamePrimary:
		return colWhite
	case theme.ColorNameForegroundOnPrimary:
		return colBlack
	case theme.ColorNameFocus, theme.ColorNameSelection, theme.ColorNameHover:
		return color.NRGBA{R: 0x35, G: 0x61, B: 0x8f, A: 0xff}
	case theme.ColorNameError:
		return colRed
	case theme.ColorNameWarning:
		return colYellow
	case theme.ColorNameSuccess:
		return colGreen
	default:
		return theme.DefaultTheme().Color(name, variant)
	}
}

func (helperTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (helperTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (helperTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 5
	case theme.SizeNameInnerPadding:
		return 5
	case theme.SizeNameText:
		return 12
	case theme.SizeNameSubHeadingText:
		return 12
	case theme.SizeNameHeadingText:
		return 13
	case theme.SizeNameCaptionText:
		return 10
	case theme.SizeNameInlineIcon:
		return 14
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 5
	case theme.SizeNameInputBorder:
		return 1
	default:
		return theme.DefaultTheme().Size(name)
	}
}
