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

func TestProcessRunsMotionPhotoPassWhenEnabled(t *testing.T) {
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

	args := readFileString(t, argsFile)
	requireContainsArg(t, args, "--input-image", imageOutput)
	requireContainsArg(t, args, "--input-video", videoOutput)
	requireContainsArg(t, args, "--output-file", imageOutput)
	requireContains(t, args, "--overwrite")
}

func TestProcessDeletesStandaloneMotionPhotoVideoAfterEmbed(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "motionphoto.args")

	sourceRoot := filepath.Join(t.TempDir(), "Google Photos")
	yearDir := filepath.Join(sourceRoot, "Photos from 2024")
	if err := os.MkdirAll(yearDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(yearDir, "PXL_0001.jpg"), "image")
	writeTestFile(t, filepath.Join(yearDir, "PXL_0001.mp4"), "video")

	outputRoot := filepath.Join(t.TempDir(), "fixed")
	imageOutput := filepath.Join(outputRoot, "Photos from 2024", "PXL_0001.jpg")
	withFakeMotionPhotoTool(t, map[string]string{
		"FAKE_MOTIONPHOTO_ARGS_FILE": argsFile,
		"FAKE_MOTIONPHOTO_APPEND_TO": imageOutput,
	})

	progressCh := make(chan Progress)
	errCh := make(chan error, 1)

	SafeGo("process-motionphoto-delete-test", func() {
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
		t.Fatal("expected motion photo image output to exist")
	}
	videoOutput := filepath.Join(outputRoot, "Photos from 2024", "PXL_0001.mp4")
	if FileExists(videoOutput) {
		t.Fatal("expected standalone live video to be deleted from output")
	}

	args := readFileString(t, argsFile)
	requireContainsArg(t, args, "--input-image", imageOutput)
	requireContainsArg(t, args, "--input-video", videoOutput)
	requireContainsArg(t, args, "--output-file", imageOutput)
}

func TestProcessDeletesStandaloneMotionPhotoVideoAfterPartialFailedEmbed(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "motionphoto.args")

	sourceRoot := filepath.Join(t.TempDir(), "Google Photos")
	yearDir := filepath.Join(sourceRoot, "Photos from 2024")
	if err := os.MkdirAll(yearDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(yearDir, "PXL_0001.jpg"), "image")
	writeTestFile(t, filepath.Join(yearDir, "PXL_0001.mp4"), "video")

	outputRoot := filepath.Join(t.TempDir(), "fixed")
	imageOutput := filepath.Join(outputRoot, "Photos from 2024", "PXL_0001.jpg")
	withFakeMotionPhotoTool(t, map[string]string{
		"FAKE_MOTIONPHOTO_ARGS_FILE": argsFile,
		"FAKE_MOTIONPHOTO_APPEND_TO": imageOutput,
		"FAKE_MOTIONPHOTO_EXIT_CODE": "1",
		"FAKE_MOTIONPHOTO_STDOUT":    "partial",
	})

	progressCh := make(chan Progress)
	errCh := make(chan error, 1)

	SafeGo("process-motionphoto-partial-delete-test", func() {
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
		t.Fatal("expected motion photo image output to exist")
	}
	videoOutput := filepath.Join(outputRoot, "Photos from 2024", "PXL_0001.mp4")
	if FileExists(videoOutput) {
		t.Fatal("expected standalone live video to be deleted after partial embed")
	}

	reportText := readFileString(t, filepath.Join(outputRoot, ".gtf", "reports", "latest.txt"))
	requireContains(t, reportText, "Motion photo pass: failed")
	requireContains(t, reportText, "Motion photo cleanup: deleted=1 skipped=0 errors=0 candidates=1")

	args := readFileString(t, argsFile)
	requireContainsArg(t, args, "--input-image", imageOutput)
	requireContainsArg(t, args, "--input-video", videoOutput)
	requireContainsArg(t, args, "--output-file", imageOutput)
}

func TestProcessDeletesStandaloneMotionPhotoVideoWhenImageAlreadyMotionPhoto(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "motionphoto.args")

	sourceRoot := filepath.Join(t.TempDir(), "Google Photos")
	yearDir := filepath.Join(sourceRoot, "Photos from 2024")
	if err := os.MkdirAll(yearDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(yearDir, "PXL_0001.jpg"), "image")
	writeTestFile(t, filepath.Join(yearDir, "PXL_0001.mp4"), "video")

	outputRoot := filepath.Join(t.TempDir(), "fixed")
	imageOutput := filepath.Join(outputRoot, "Photos from 2024", "PXL_0001.jpg")
	videoOutput := filepath.Join(outputRoot, "Photos from 2024", "PXL_0001.mp4")
	withFakeMotionPhotoTool(t, map[string]string{
		"FAKE_MOTIONPHOTO_ARGS_FILE": argsFile,
		"FAKE_MOTIONPHOTO_STDOUT":    "Input PXL_0001.jpg is already a motion photo, skipping muxing...",
	})

	progressCh := make(chan Progress)
	errCh := make(chan error, 1)

	SafeGo("process-motionphoto-already-embedded-test", func() {
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
		t.Fatal("expected motion photo image output to exist")
	}
	if FileExists(videoOutput) {
		t.Fatal("expected standalone live video to be deleted when image is already a motion photo")
	}

	args := readFileString(t, argsFile)
	requireContainsArg(t, args, "--input-image", imageOutput)
	requireContainsArg(t, args, "--input-video", videoOutput)
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
