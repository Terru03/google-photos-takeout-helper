package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

var tabLabels = []string{"Setup", "Progress", "Reports", "Options"}

func (s *guiState) appChrome(content fyne.CanvasObject) fyne.CanvasObject {
	top := container.NewVBox(appHeader(), s.tabRow())
	return container.NewPadded(container.NewBorder(top, nil, nil, nil, content))
}

func appHeader() fyne.CanvasObject {
	title := canvas.NewText("Google Photos Takeout Helper", colText)
	title.TextSize = 16
	title.TextStyle = fyne.TextStyle{Bold: true}
	subtitle := smallText("Safe local fixer for large Google Photos exports")
	safety := badge("Source ZIPs are never deleted", colGreenBg, colGreen)
	return coloredPanel(colTitleBar, colBorder, cardRadius, container.NewBorder(nil, nil, container.NewVBox(title, subtitle), safety))
}

func (s *guiState) tabRow() fyne.CanvasObject {
	objects := make([]fyne.CanvasObject, 0, len(tabLabels))
	for index, label := range tabLabels {
		index := index
		button := newPolishedButton(label, buttonTab, func() {
			s.refreshTabs(index)
		})
		button.minWidth = 110
		button.SetSelected(index == s.selectedTab)
		objects = append(objects, button)
	}
	return coloredPanel(colPanelDark, colBorderSoft, cardRadius, container.NewGridWithColumns(len(objects), objects...))
}
