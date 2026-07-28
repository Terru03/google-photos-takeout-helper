package fixer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBackfillIndexedSidecarsMovesAndUpdatesOldOutput(t *testing.T) {
	withFakeExifTool(t, map[string]string{
		"FAKE_EXIFTOOL_JSON": `[{"DateTimeOriginal":"2020:01:02 03:04:05","OffsetTimeOriginal":"+00:00","Title":"IMG_0001.jpg"}]`,
	})
	outputRoot := filepath.Join(t.TempDir(), "output")
	oldPath := filepath.Join(outputRoot, "Photos from 2024", "7 - July", "IMG_0001.jpg")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, oldPath, "image")

	runtimePaths, err := ResolveRuntimePaths(outputRoot)
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStateStore(filepath.Join(runtimePaths.StateDir, "state.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ProcessRecord{
		SourceID:      "zip-001",
		SourceRelPath: filepath.Join("Photos from 2024", "IMG_0001.jpg"),
		OutputPath:    oldPath,
		MatchStatus:   MatchStatusUnmatched,
		Status:        OperationCopiedWithoutMeta,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	index := NewSidecarIndex()
	if err := index.AddJSON(
		filepath.Join("Photos from 2024", "IMG_0001.jpg.json"),
		"takeout-002.zip::IMG_0001.jpg.json",
		[]byte(`{"title":"IMG_0001.jpg","photoTakenTime":{"timestamp":"1577934245"}}`),
	); err != nil {
		t.Fatal(err)
	}

	result, err := BackfillIndexedSidecars(context.Background(), outputRoot, index, ProcessOptions{
		WriteMetadata:   true,
		VerifyWrites:    true,
		AlbumMode:       AlbumModeTimelineOnly,
		MonthSubfolders: true,
		ConflictPolicy:  ConflictMerge,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Checked != 1 || result.Matched != 1 || result.Updated != 1 || result.Failed != 0 {
		t.Fatalf("unexpected backfill result: %#v", result)
	}

	newPath := filepath.Join(outputRoot, "Photos from 2020", "1 - January", "IMG_0001.jpg")
	if !FileExists(newPath) {
		t.Fatalf("new output missing: %s", newPath)
	}
	if FileExists(oldPath) {
		t.Fatalf("old output still exists: %s", oldPath)
	}

	store, err = OpenStateStore(filepath.Join(runtimePaths.StateDir, "state.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = store.Close()
	}()
	record, ok := store.GetForSource("zip-001", filepath.Join("Photos from 2024", "IMG_0001.jpg"))
	if !ok {
		t.Fatal("updated state record missing")
	}
	if record.MatchStatus != MatchStatusMatched || record.OutputPath != newPath || !record.MetadataVerified {
		t.Fatalf("bad updated record: %#v", record)
	}
}
