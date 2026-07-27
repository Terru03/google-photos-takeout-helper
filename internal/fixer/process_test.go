package fixer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

	record, _ := processPlan(outputRoot, MediaPlan{
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

func TestProcessPlanRestoresOriginalBytesAndWritesXMPSidecarWhenMetadataWriteFails(t *testing.T) {
	root := t.TempDir()
	outputRoot := filepath.Join(root, "output")
	sourcePath := filepath.Join(root, "Photos from 2024", "IMG_0001.jpg")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, sourcePath, "clean-original")

	outputPath := filepath.Join(outputRoot, "Photos from 2024", "IMG_0001.jpg")
	withFakeExifTool(t, map[string]string{
		"FAKE_EXIFTOOL_MUTATE_FILE": outputPath,
		"FAKE_EXIFTOOL_EXIT_CODE":   "7",
	})

	stateStore, err := OpenStateStore(filepath.Join(outputRoot, ".gtf", "state.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := stateStore.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	})

	metadata := imageMetadata{Title: "Real title"}
	record, _ := processPlan(outputRoot, MediaPlan{
		SourcePath:   sourcePath,
		RelativePath: filepath.Join("Photos from 2024", "IMG_0001.jpg"),
		RelativeDir:  "Photos from 2024",
		TopLevelDir:  "Photos from 2024",
		OutputName:   "IMG_0001.jpg",
		IsYearFolder: true,
		MatchStatus:  MatchStatusMatched,
		Metadata:     &metadata,
	}, ProcessOptions{WriteMetadata: true}, stateStore)

	if record.Status != OperationCopiedWithMetadata {
		t.Fatalf("status = %s, want %s", record.Status, OperationCopiedWithMetadata)
	}
	if !strings.Contains(record.Error, "metadata write failed") {
		t.Fatalf("missing metadata error: %q", record.Error)
	}
	if got := readFileString(t, outputPath); got != "clean-original" {
		t.Fatalf("failed metadata write changed output bytes: %q", got)
	}
	if !record.MetadataWritten || !record.UsedXMPSidecar {
		t.Fatalf("metadata fallback missing: %#v", record)
	}
	if !FileExists(outputPath + ".xmp") {
		t.Fatal("expected XMP metadata fallback")
	}
}

func TestResolveOutputNameCorrectsPNGStoredAsJPG(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "photo.jpg")
	pngHeader := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(sourcePath, pngHeader, 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveOutputName(MediaPlan{
		SourcePath: sourcePath,
		OutputName: "photo.jpg",
	}, ProcessOptions{})
	if got != "photo.png" {
		t.Fatalf("output name = %q, want photo.png", got)
	}
}

func TestResolveOutputNameCorrectsJPEGStoredAsHEIC(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "photo.HEIC")
	jpegHeader := []byte{0xff, 0xd8, 0xff, 0xe0}
	if err := os.WriteFile(sourcePath, jpegHeader, 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveOutputName(MediaPlan{
		SourcePath: sourcePath,
		OutputName: "photo.HEIC",
	}, ProcessOptions{})
	if got != "photo.jpg" {
		t.Fatalf("output name = %q, want photo.jpg", got)
	}
}

func TestProcessPhaseOneKeepsMotionPairWithoutCallingMotionPhoto2(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "motionphoto.args")
	sourceRoot := filepath.Join(t.TempDir(), "Google Photos")
	yearDir := filepath.Join(sourceRoot, "Photos from 2024")
	if err := os.MkdirAll(yearDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(yearDir, "IMG_0001.jpg"), "image")
	writeTestFile(t, filepath.Join(yearDir, "IMG_0001.mp4"), "video")

	outputRoot := filepath.Join(t.TempDir(), "fixed")
	imageOutput := filepath.Join(outputRoot, "Photos from 2024", "IMG_0001.jpg")
	videoOutput := filepath.Join(outputRoot, "Photos from 2024", "IMG_0001.mp4")
	withFakeMotionPhotoTool(t, map[string]string{
		"FAKE_MOTIONPHOTO_ARGS_FILE": argsFile,
		"FAKE_MOTIONPHOTO_APPEND_TO": imageOutput,
	})

	progressCh := make(chan Progress)
	errCh := make(chan error, 1)

	SafeGo("process-motionphoto-test", func() {
		errCh <- Process(context.Background(), sourceRoot, outputRoot, progressCh, ProcessOptions{
			CreateMotionPhotos: true,
		})
	})

	for range progressCh {
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	if !FileExists(imageOutput) {
		t.Fatal("expected still image to stay in phase one")
	}
	if !FileExists(videoOutput) {
		t.Fatal("expected motion video to stay for later merge")
	}
	if FileExists(argsFile) {
		t.Fatal("phase one must not call MotionPhoto2")
	}
}

func TestProcessWritesSuspiciousDatesCSV(t *testing.T) {
	sourceRoot := filepath.Join(t.TempDir(), "Google Photos")
	yearDir := filepath.Join(sourceRoot, "Photos from 2024")
	if err := os.MkdirAll(yearDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(yearDir, "IMG_0002.jpg"), "image")
	writeTestFile(t, filepath.Join(yearDir, "IMG_0002.jpg.json"), `{}`)

	outputRoot := filepath.Join(t.TempDir(), "fixed")
	progressCh := make(chan Progress)
	errCh := make(chan error, 1)

	SafeGo("process-suspicious-date-test", func() {
		errCh <- Process(context.Background(), sourceRoot, outputRoot, progressCh, ProcessOptions{})
	})

	for range progressCh {
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	csvText := readFileString(t, filepath.Join(outputRoot, ".gtf", "reports", "suspicious_dates.csv"))
	requireContains(t, csvText, "source_path,output_path,json_timestamp,embedded_timestamp,reason")
	requireContains(t, csvText, "missing JSON timestamp")
}

func TestProcessDeletesSourceFolderAfterCleanRun(t *testing.T) {
	sourceRoot := filepath.Join(t.TempDir(), "Google Photos")
	yearDir := filepath.Join(sourceRoot, "Photos from 2024")
	if err := os.MkdirAll(yearDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(yearDir, "IMG_0001.jpg"), "image")
	writeTestFile(t, filepath.Join(yearDir, "IMG_0001.jpg.json"), `{}`)

	outputRoot := filepath.Join(t.TempDir(), "fixed")
	progressCh := make(chan Progress)
	errCh := make(chan error, 1)

	SafeGo("process-delete-source-test", func() {
		errCh <- Process(context.Background(), sourceRoot, outputRoot, progressCh, ProcessOptions{
			DeleteSourceAfterSuccess: true,
		})
	})

	for range progressCh {
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	if FileExists(sourceRoot) {
		t.Fatal("expected input folder to be deleted after clean run")
	}
	if !FileExists(filepath.Join(outputRoot, "Photos from 2024", "IMG_0001.jpg")) {
		t.Fatal("expected output file to exist after source cleanup")
	}
}

func TestProcessKeepsSourceFolderWhenMatchesAreIncomplete(t *testing.T) {
	sourceRoot := filepath.Join(t.TempDir(), "Google Photos")
	yearDir := filepath.Join(sourceRoot, "Photos from 2024")
	if err := os.MkdirAll(yearDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(yearDir, "IMG_9999.jpg"), "image")

	outputRoot := filepath.Join(t.TempDir(), "fixed")
	progressCh := make(chan Progress)
	errCh := make(chan error, 1)

	SafeGo("process-keep-source-test", func() {
		errCh <- Process(context.Background(), sourceRoot, outputRoot, progressCh, ProcessOptions{
			DeleteSourceAfterSuccess: true,
		})
	})

	for range progressCh {
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	if !FileExists(sourceRoot) {
		t.Fatal("expected input folder to stay when run has unmatched media")
	}
}
