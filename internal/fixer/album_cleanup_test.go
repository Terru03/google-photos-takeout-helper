package fixer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupDuplicateAlbumFilesKeepsTimelineAndUniqueAlbumFiles(t *testing.T) {
	output := t.TempDir()
	timelinePath := filepath.Join(output, "Photos from 2024", "IMG_0001.jpg")
	albumDuplicatePath := filepath.Join(output, "Trip", "IMG_0001.jpg")
	albumUniquePath := filepath.Join(output, "Trip", "unique.jpg")
	emptyAlbumDuplicatePath := filepath.Join(output, "Only Duplicate", "IMG_0001.jpg")

	for _, dir := range []string{
		filepath.Dir(timelinePath),
		filepath.Dir(albumDuplicatePath),
		filepath.Dir(emptyAlbumDuplicatePath),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, timelinePath, "same")
	writeTestFile(t, albumDuplicatePath, "same")
	writeTestFile(t, albumUniquePath, "unique")
	writeTestFile(t, emptyAlbumDuplicatePath, "same")

	store, err := OpenStateStore(filepath.Join(output, ".gtf", "state.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []ProcessRecord{
		{
			SourceRelPath: "Photos from 2024/IMG_0001.jpg",
			SourceHash:    "hash-same",
			OutputPath:    timelinePath,
			Status:        OperationCopied,
		},
		{
			SourceRelPath: "Trip/IMG_0001.jpg",
			SourceHash:    "hash-same",
			OutputPath:    albumDuplicatePath,
			Status:        OperationHardlinked,
		},
		{
			SourceRelPath: "Trip/unique.jpg",
			SourceHash:    "hash-unique",
			OutputPath:    albumUniquePath,
			Status:        OperationCopied,
		},
		{
			SourceRelPath: "Only Duplicate/IMG_0001.jpg",
			SourceHash:    "hash-same",
			OutputPath:    emptyAlbumDuplicatePath,
			Status:        OperationCopied,
		},
	} {
		if err := store.Put(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := CleanupDuplicateAlbumFiles(output, ProcessOptions{AlbumMode: AlbumModeUniqueOnly})
	if err != nil {
		t.Fatal(err)
	}

	if result.DuplicateFilesRemoved != 2 {
		t.Fatalf("removed duplicates = %d, want 2", result.DuplicateFilesRemoved)
	}
	if !FileExists(timelinePath) {
		t.Fatal("timeline file should stay")
	}
	if FileExists(albumDuplicatePath) {
		t.Fatal("album duplicate should be removed")
	}
	if !FileExists(albumUniquePath) {
		t.Fatal("unique album file should stay")
	}
	if FileExists(filepath.Dir(emptyAlbumDuplicatePath)) {
		t.Fatal("empty album folder should be removed")
	}
	if !FileExists(result.ReportPath) {
		t.Fatal("cleanup report should be written")
	}
}

func TestCleanupDuplicateAlbumFilesDryRunDoesNotRemoveFiles(t *testing.T) {
	output := t.TempDir()
	timelinePath := filepath.Join(output, "Photos from 2024", "IMG_0001.jpg")
	albumPath := filepath.Join(output, "Trip", "IMG_0001.jpg")
	for _, dir := range []string{filepath.Dir(timelinePath), filepath.Dir(albumPath)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, timelinePath, "same")
	writeTestFile(t, albumPath, "same")

	store, err := OpenStateStore(filepath.Join(output, ".gtf", "state.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []ProcessRecord{
		{SourceRelPath: "Photos from 2024/IMG_0001.jpg", SourceHash: "hash-same", OutputPath: timelinePath, Status: OperationCopied},
		{SourceRelPath: "Trip/IMG_0001.jpg", SourceHash: "hash-same", OutputPath: albumPath, Status: OperationCopied},
	} {
		if err := store.Put(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := CleanupDuplicateAlbumFiles(output, ProcessOptions{AlbumMode: AlbumModeUniqueOnly, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != AlbumCleanupStatusDryRun {
		t.Fatalf("status = %s, want %s", result.Status, AlbumCleanupStatusDryRun)
	}
	if !FileExists(albumPath) {
		t.Fatal("dry run should not remove album file")
	}
}
