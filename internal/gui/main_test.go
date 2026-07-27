package gui

import (
	"testing"

	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
)

func TestBatchModeDefaultsKeepSavedMotionPhotoSetting(t *testing.T) {
	options := applyBatchModeDefaults(fixer.ProcessOptions{
		CreateMotionPhotos: true,
	})

	if !options.CreateMotionPhotos {
		t.Fatal("batch defaults turned off saved motion photo setting")
	}
	if options.AlbumMode != fixer.AlbumModeTimelineOnly || !options.IgnoreAlbums || !options.MonthSubfolders {
		t.Fatalf("batch layout defaults missing: %#v", options)
	}
}
