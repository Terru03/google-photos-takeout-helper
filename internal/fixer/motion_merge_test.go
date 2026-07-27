package fixer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMergeMotionLibraryScansWritesReportAndResumes(t *testing.T) {
	library := t.TempDir()
	stillPath := filepath.Join(library, "Photos from 2024", "1 - January", "PXL_0001.jpg")
	videoPath := filepath.Join(library, "Photos from 2024", "1 - January", "PXL_0001.mp4")
	if err := os.MkdirAll(filepath.Dir(stillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, stillPath, "still")
	writeTestFile(t, videoPath, "video")

	argsFile := filepath.Join(t.TempDir(), "motion.args")
	toolPath := withFakeMotionPhotoTool(t, map[string]string{
		"FAKE_MOTIONPHOTO_ARGS_FILE": argsFile,
	})
	report, err := MergeMotionLibrary(context.Background(), MotionMergeOptions{
		LibraryRoot: library,
		ToolPath:    toolPath,
		Timeout:     time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalCandidatePairs != 1 || report.MergedSuccessfully != 1 {
		t.Fatalf("unexpected first report: %#v", report)
	}
	if !FileExists(filepath.Join(library, ".gtf", "reports", "motion-merge-report.json")) {
		t.Fatal("motion merge report missing")
	}
	firstArgs := readFileString(t, argsFile)
	requireContainsArg(t, firstArgs, "--input-image", stillPath)
	requireContainsArg(t, firstArgs, "--input-video", videoPath)

	report, err = MergeMotionLibrary(context.Background(), MotionMergeOptions{
		LibraryRoot: library,
		ToolPath:    toolPath,
		Timeout:     time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.SkippedAlreadyMerged != 1 || report.MergedSuccessfully != 0 {
		t.Fatalf("rerun did not skip success: %#v", report)
	}
	if got := readFileString(t, argsFile); got != firstArgs {
		t.Fatal("rerun called MotionPhoto2 for merged pair")
	}
}

func TestMergeMotionLibraryTimeoutContinuesToNextPair(t *testing.T) {
	library := t.TempDir()
	dir := filepath.Join(library, "Photos from 2024", "1 - January")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"fast.jpg", "fast.mp4", "slow.jpg", "slow.mp4"} {
		writeTestFile(t, filepath.Join(dir, name), name)
	}
	toolPath := withFakeMotionPhotoTool(t, map[string]string{
		"FAKE_MOTIONPHOTO_SLEEP_MATCH":   "slow.jpg",
		"FAKE_MOTIONPHOTO_SLEEP_MS":      "1500",
		"FAKE_MOTIONPHOTO_SLEEP_SECONDS": "1.5",
	})

	report, err := MergeMotionLibrary(context.Background(), MotionMergeOptions{
		LibraryRoot: library,
		ToolPath:    toolPath,
		Timeout:     100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalCandidatePairs != 2 {
		t.Fatalf("candidate count = %d", report.TotalCandidatePairs)
	}
	if report.TimedOutMerges != 1 || report.MergedSuccessfully != 1 {
		t.Fatalf("merge did not continue after timeout: %#v", report)
	}
}

func TestMergeMotionLibraryReportsMissingVideoFromState(t *testing.T) {
	library := t.TempDir()
	stillPath := filepath.Join(library, "Photos from 2024", "PXL_0002.jpg")
	videoPath := filepath.Join(library, "Photos from 2024", "PXL_0002.mp4")
	if err := os.MkdirAll(filepath.Dir(stillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, stillPath, "still")
	store, err := OpenStateStore(filepath.Join(library, ".gtf", "state.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	stillRecord := ProcessRecord{
		SourceID:       "zip-1",
		SourceRelPath:  "Photos from 2024/PXL_0002.jpg",
		PartnerRelPath: "Photos from 2024/PXL_0002.mp4",
		OutputPath:     stillPath,
		Status:         OperationCopied,
	}
	videoRecord := ProcessRecord{
		SourceID:       "zip-1",
		SourceRelPath:  "Photos from 2024/PXL_0002.mp4",
		PartnerRelPath: "Photos from 2024/PXL_0002.jpg",
		OutputPath:     videoPath,
		Status:         OperationCopied,
	}
	if err := store.Put(stillRecord); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(videoRecord); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	toolPath := withFakeMotionPhotoTool(t, nil)
	report, err := MergeMotionLibrary(context.Background(), MotionMergeOptions{
		LibraryRoot: library,
		ToolPath:    toolPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalCandidatePairs != 1 || report.SkippedMissingVideo != 1 {
		t.Fatalf("missing video not reported: %#v", report)
	}
}
