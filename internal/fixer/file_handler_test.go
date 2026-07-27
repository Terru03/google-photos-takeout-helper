package fixer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFormatMonthFolderName(t *testing.T) {
	tests := map[int]string{
		1:  "1 - January",
		2:  "2 - February",
		3:  "3 - March",
		4:  "4 - April",
		5:  "5 - May",
		6:  "6 - June",
		7:  "7 - July",
		8:  "8 - August",
		9:  "9 - September",
		10: "10 - October",
		11: "11 - November",
		12: "12 - December",
	}

	for month, want := range tests {
		if got := FormatMonthFolderName(month); got != want {
			t.Fatalf("month %d: expected %q, got %q", month, want, got)
		}
	}
}

func TestResolveOutputDirUsesEmbeddedDateBeforeFileModifiedDate(t *testing.T) {
	withFakeExifTool(t, map[string]string{
		"FAKE_EXIFTOOL_JSON": `[{"DateTimeOriginal":"2026:02:13 16:56:23","XMP:CreateDate":"2026-02-13T16:56:23","Photoshop:DateCreated":"2026:02:13 16:56:23"}]`,
	})

	root := t.TempDir()
	sourcePath := filepath.Join(root, "Photos from 2026", "IMG_6664(1).heic")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, sourcePath, "heic")
	july := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(sourcePath, july, july); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveOutputDir(filepath.Join(root, "out"), MediaPlan{
		SourcePath:   sourcePath,
		RelativeDir:  "Photos from 2026",
		RelativePath: filepath.Join("Photos from 2026", "IMG_6664(1).heic"),
		OutputName:   "IMG_6664(1).heic",
		IsYearFolder: true,
	}, ProcessOptions{MonthSubfolders: true})
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(root, "out", "Photos from 2026", "2 - February")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTimelineOnlyUsesSidecarYearAndMonthNotSourceFolder(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "Photos from 2026", "IMG_0001.jpg")
	sidecarPath := sourcePath + ".json"
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, sourcePath, "image")
	writeTestFile(t, sidecarPath, `{"title":"IMG_0001.jpg","photoTakenTime":{"timestamp":"1581724800"}}`)
	july := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(sourcePath, july, july); err != nil {
		t.Fatal(err)
	}
	metadata, err := ReadJSONMetadata(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ResolveOutputDir(filepath.Join(root, "out"), MediaPlan{
		SourcePath:   sourcePath,
		SidecarPath:  sidecarPath,
		RelativeDir:  "Photos from 2026",
		RelativePath: filepath.Join("Photos from 2026", "IMG_0001.jpg"),
		OutputName:   "IMG_0001.jpg",
		IsYearFolder: true,
		Metadata:     &metadata,
	}, ProcessOptions{AlbumMode: AlbumModeTimelineOnly})
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(root, "out", "Photos from 2020", "2 - February")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestAlbumOutputStaysUnderAlbumsFolder(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "Trip to Rome", "IMG_0001.jpg")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, sourcePath, "image")

	got, err := ResolveOutputDir(filepath.Join(root, "out"), MediaPlan{
		SourcePath:   sourcePath,
		RelativeDir:  "Trip to Rome",
		RelativePath: filepath.Join("Trip to Rome", "IMG_0001.jpg"),
		OutputName:   "IMG_0001.jpg",
	}, ProcessOptions{AlbumMode: AlbumModeAll})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "out", "Albums", "Trip to Rome")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
