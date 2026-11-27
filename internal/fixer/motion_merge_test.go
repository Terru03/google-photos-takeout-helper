package fixer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	if !report.Completed {
		t.Fatal("finished pass not marked completed")
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
		"FAKE_MOTIONPHOTO_SLEEP_MS":      "20000",
		"FAKE_MOTIONPHOTO_SLEEP_SECONDS": "20",
	})

	// The fake tool is a .cmd script; starting cmd.exe can take ~1s on a
	// loaded machine, so the timeout must leave room for the fast pair.
	// The elapsed check below still proves the 20s sleeper was killed.
	started := time.Now()
	report, err := MergeMotionLibrary(context.Background(), MotionMergeOptions{
		LibraryRoot: library,
		ToolPath:    toolPath,
		Timeout:     3 * time.Second,
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
	if elapsed := time.Since(started); elapsed > 12*time.Second {
		t.Fatalf("motion timeout left child process alive for %s", elapsed)
	}
}

func TestMergeMotionLibraryRerunsPairAfterOutputChanges(t *testing.T) {
	library := t.TempDir()
	dir := filepath.Join(library, "Photos from 2024", "1 - January")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stillPath := filepath.Join(dir, "changed.jpg")
	videoPath := filepath.Join(dir, "changed.mp4")
	writeTestFile(t, stillPath, "still")
	writeTestFile(t, videoPath, "video")

	argsFile := filepath.Join(t.TempDir(), "motion.args")
	toolPath := withFakeMotionPhotoTool(t, map[string]string{
		"FAKE_MOTIONPHOTO_ARGS_FILE": argsFile,
	})
	options := MotionMergeOptions{
		LibraryRoot: library,
		ToolPath:    toolPath,
		Timeout:     time.Second,
	}
	if _, err := MergeMotionLibrary(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	firstArgs := readFileString(t, argsFile)

	writeTestFile(t, stillPath, "new still data")
	report, err := MergeMotionLibrary(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if report.MergedSuccessfully != 1 || report.SkippedAlreadyMerged != 0 {
		t.Fatalf("changed pair was not merged again: %#v", report)
	}
	if got := readFileString(t, argsFile); got == firstArgs {
		t.Fatal("changed pair did not call MotionPhoto2 again")
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

func writeLegacyMotionMergeState(t *testing.T, library string, items ...MotionMergeItem) {
	t.Helper()
	statePath := filepath.Join(library, ".gtf", "motion-merge-state.jsonl")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	var lines []byte
	for _, item := range items {
		data, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, append(data, '\n')...)
	}
	if err := os.WriteFile(statePath, lines, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMergeMotionLibrarySkipsLegacyMergedPathRecord(t *testing.T) {
	library := t.TempDir()
	dir := filepath.Join(library, "Photos from 2024", "1 - January")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stillPath := filepath.Join(dir, "legacy.jpg")
	videoPath := filepath.Join(dir, "legacy.mp4")
	writeTestFile(t, stillPath, "still")
	writeTestFile(t, videoPath, "video")
	// Older builds wrote the same path key but no file fingerprint.
	writeLegacyMotionMergeState(t, library, MotionMergeItem{
		Key:    "path|" + motionPairPathKey(stillPath, videoPath),
		Status: motionMergeStatusMerged,
	})

	argsFile := filepath.Join(t.TempDir(), "motion.args")
	toolPath := withFakeMotionPhotoTool(t, map[string]string{
		"FAKE_MOTIONPHOTO_ARGS_FILE": argsFile,
	})
	options := MotionMergeOptions{LibraryRoot: library, ToolPath: toolPath, Timeout: time.Second}
	report, err := MergeMotionLibrary(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if report.SkippedAlreadyMerged != 1 || report.MergedSuccessfully != 0 {
		t.Fatalf("legacy merged pair was not skipped: %#v", report)
	}
	if FileExists(argsFile) {
		t.Fatal("legacy merged pair called MotionPhoto2 again")
	}
	if item := report.Items[0]; !motionMergeHasFileState(item) {
		t.Fatalf("legacy pair did not get a file fingerprint: %#v", item)
	}

	// Once migrated, a changed still is detected and merged again.
	writeTestFile(t, stillPath, "rewritten still")
	report, err = MergeMotionLibrary(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if report.MergedSuccessfully != 1 {
		t.Fatalf("changed pair was not merged after migration: %#v", report)
	}
}

func TestMergeMotionLibrarySkipsLegacyStateKeyRecord(t *testing.T) {
	library := t.TempDir()
	stillPath := filepath.Join(library, "Photos from 2024", "PXL_0003.jpg")
	videoPath := filepath.Join(library, "Photos from 2024", "PXL_0003.mp4")
	if err := os.MkdirAll(filepath.Dir(stillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, stillPath, "still")
	writeTestFile(t, videoPath, "video")
	store, err := OpenStateStore(filepath.Join(library, ".gtf", "state.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	stillRecord := ProcessRecord{
		SourceID:       "zip-1",
		SourceRelPath:  "Photos from 2024/PXL_0003.jpg",
		PartnerRelPath: "Photos from 2024/PXL_0003.mp4",
		OutputPath:     stillPath,
		Status:         OperationCopied,
	}
	videoRecord := ProcessRecord{
		SourceID:       "zip-1",
		SourceRelPath:  "Photos from 2024/PXL_0003.mp4",
		PartnerRelPath: "Photos from 2024/PXL_0003.jpg",
		OutputPath:     videoPath,
		Status:         OperationCopied,
	}
	for _, record := range []ProcessRecord{stillRecord, videoRecord} {
		if err := store.Put(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	writeLegacyMotionMergeState(t, library, MotionMergeItem{
		Key:    legacyMotionPairStateKey(stillRecord, videoRecord),
		Status: motionMergeStatusMerged,
	})

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
	if report.MergedSuccessfully != 0 || report.SkippedAlreadyMerged != 1 || FileExists(argsFile) {
		t.Fatalf("pair merged by older build was merged again: %#v", report)
	}
}

func TestMergeMotionLibraryPersistsStateAfterEachPair(t *testing.T) {
	library := t.TempDir()
	dir := filepath.Join(library, "Photos from 2024", "1 - January")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		writeTestFile(t, filepath.Join(dir, name+".jpg"), "still")
		writeTestFile(t, filepath.Join(dir, name+".mp4"), "video")
	}
	statePath := filepath.Join(library, ".gtf", "motion-merge-state.jsonl")
	var linesAfterFirst int
	_, err := MergeMotionLibrary(context.Background(), MotionMergeOptions{
		LibraryRoot: library,
		ToolPath:    withFakeMotionPhotoTool(t, nil),
		Timeout:     time.Second,
		Progress: func(progress MotionMergeProgress) {
			if progress.Processed == 1 {
				linesAfterFirst = strings.Count(readFileString(t, statePath), "\n")
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if linesAfterFirst != 1 {
		t.Fatalf("state after first pair has %d lines on disk, want 1", linesAfterFirst)
	}
}

func TestMergeMotionLibraryWritesPartialReportWhenCancelled(t *testing.T) {
	library := t.TempDir()
	dir := filepath.Join(library, "Photos from 2024", "1 - January")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		writeTestFile(t, filepath.Join(dir, name+".jpg"), "still")
		writeTestFile(t, filepath.Join(dir, name+".mp4"), "video")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := MergeMotionLibrary(ctx, MotionMergeOptions{
		LibraryRoot: library,
		ToolPath:    withFakeMotionPhotoTool(t, nil),
		Timeout:     time.Second,
		Progress: func(progress MotionMergeProgress) {
			if progress.Processed == 1 {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancel error, got %v", err)
	}
	var saved MotionMergeReport
	data := readFileString(t, filepath.Join(library, ".gtf", "reports", "motion-merge-report.json"))
	if err := json.Unmarshal([]byte(data), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Completed || saved.Error == "" || saved.MergedSuccessfully != 1 || len(saved.Items) != 1 {
		t.Fatalf("partial report not written correctly: %#v", saved)
	}
}
