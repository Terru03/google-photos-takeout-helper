package fixer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProcessCreatesReportAndStateWithFakeExifTool(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	withFakeExifTool(t, map[string]string{
		"FAKE_EXIFTOOL_ARGS_FILE": argsFile,
		"FAKE_EXIFTOOL_JSON":      `[{"DateTimeOriginal":"2023:12:31 16:00:00","OffsetTimeOriginal":"-08:00","Title":"IMG_0001.jpg","Description":"sample import","GPSLatitude":37.3318,"GPSLongitude":-122.0312}]`,
	})

	sourceRoot := filepath.Join(t.TempDir(), "Google Photos")
	yearDir := filepath.Join(sourceRoot, "Photos from 2024")
	if err := os.MkdirAll(yearDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(yearDir, "IMG_0001.jpg"), "image")
	writeTestFile(t, filepath.Join(yearDir, "IMG_0001.jpg.json"), `{"title":"IMG_0001.jpg","description":"sample import","photoTakenTime":{"timestamp":"1704067200"},"geoData":{"latitude":37.3318,"longitude":-122.0312,"altitude":10}}`)

	outputRoot := filepath.Join(t.TempDir(), "fixed")
	progressCh := make(chan Progress)
	errCh := make(chan error, 1)

	SafeGo("process-test", func() {
		errCh <- Process(context.Background(), sourceRoot, outputRoot, progressCh, ProcessOptions{
			WriteMetadata:       true,
			Deduplicate:         true,
			VerifyWrites:        true,
			RestoreMOVExtension: true,
			ConflictPolicy:      ConflictMerge,
		})
	})

	for range progressCh {
		// Drain progress until Process closes the channel.
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	if !FileExists(filepath.Join(outputRoot, ".gtf", "state.jsonl")) {
		t.Fatal("expected state file to be created")
	}
	if !FileExists(filepath.Join(outputRoot, ".gtf", "reports", "latest.txt")) {
		t.Fatal("expected latest report to be created")
	}
	if !FileExists(filepath.Join(outputRoot, "Photos from 2024", "IMG_0001.jpg")) {
		t.Fatal("expected processed output file to be created")
	}
	args := readFileString(t, argsFile)
	requireContains(t, args, "-AllDates=2023:12:31 16:00:00")
	requireContains(t, args, "-GPSLatitude=37.3318000")
}

func TestProcessPlanDeduplicatesAgainstExistingState(t *testing.T) {
	root := t.TempDir()
	outputRoot := filepath.Join(root, "output")
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	sourceA := filepath.Join(root, "a.jpg")
	sourceB := filepath.Join(root, "album", "a.jpg")
	if err := os.MkdirAll(filepath.Dir(sourceB), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, sourceA, "same")
	writeTestFile(t, sourceB, "same")

	stateStore, err := OpenStateStore(filepath.Join(outputRoot, ".gtf", "state.jsonl"))
	if err != nil {
		t.Fatalf("OpenStateStore returned error: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := stateStore.Close(); closeErr != nil {
			t.Fatalf("Close returned error: %v", closeErr)
		}
	})

	hash, err := HashFile(sourceA)
	if err != nil {
		t.Fatal(err)
	}

	canonicalOutput := filepath.Join(outputRoot, "Photos from 2024", "a.jpg")
	if err := os.MkdirAll(filepath.Dir(canonicalOutput), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, canonicalOutput, "same")

	if err := stateStore.Put(ProcessRecord{
		SourceRelPath: "Photos from 2024/a.jpg",
		SourceHash:    hash,
		OutputPath:    canonicalOutput,
		Status:        OperationCopied,
	}); err != nil {
		t.Fatalf("Put returned error: %v", err)
	}

	record := processPlan(outputRoot, MediaPlan{
		SourcePath:   sourceB,
		RelativePath: filepath.ToSlash(filepath.Join("Album", "a.jpg")),
		RelativeDir:  "Album",
		OutputName:   "a.jpg",
	}, ProcessOptions{Deduplicate: true}, stateStore)

	if record.Status != OperationHardlinked && record.Status != OperationSymlinked && record.Status != OperationDuplicateCopied {
		t.Fatalf("expected duplicate linking/copy status, got %s", record.Status)
	}
	if record.DuplicateOf != canonicalOutput {
		t.Fatalf("expected duplicate to reference canonical output %q, got %q", canonicalOutput, record.DuplicateOf)
	}
}
