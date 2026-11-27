package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestFindSourceZipsSortsAndFilters(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"takeout-set-010.zip",
		"takeout-set-002.zip",
		"takeout-set-001.zip",
		"other.zip",
		"takeout-note.txt",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := findSourceZips(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 3 {
		t.Fatalf("ZIP count = %d, want 3", len(paths))
	}
	if filepath.Base(paths[0]) != "takeout-set-001.zip" ||
		filepath.Base(paths[1]) != "takeout-set-002.zip" ||
		filepath.Base(paths[2]) != "takeout-set-010.zip" {
		t.Fatalf("bad ZIP order: %#v", paths)
	}
}

func TestRequireChildPathBlocksRootAndEscape(t *testing.T) {
	root := filepath.Join(t.TempDir(), "recovery")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := requireChildPath(root, filepath.Join(root, "takeout.zip")); err != nil {
		t.Fatalf("safe child rejected: %v", err)
	}
	if err := requireChildPath(root, root); err == nil {
		t.Fatal("root path accepted")
	}
	if err := requireChildPath(root, filepath.Join(root, "..", "outside.zip")); err == nil {
		t.Fatal("escape path accepted")
	}
}

func TestLoadRescueStateTreatsNullBadRangesAsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"phase":"done","final_bad_ranges":null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := loadRescueState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != "done" || len(state.FinalBad) != 0 {
		t.Fatalf("bad state: %#v", state)
	}
}

func TestZipProcessArgsDoNotRunWholeLibraryMotionPass(t *testing.T) {
	cfg := config{
		workDir:    `C:\work`,
		stagingDir: `C:\staging`,
		outputDir:  `B:\output`,
		motionTool: `C:\motionphoto2.exe`,
	}
	args := buildZipProcessArgs(cfg, `F:\recovered.zip`)
	for _, arg := range args {
		if arg == "--motion-photos" || arg == "--motionphoto2" || arg == cfg.motionTool {
			t.Fatalf("per-ZIP args contain motion pass option %q: %#v", arg, args)
		}
	}
}

func TestMotionMergeArgsRunOneFinalPassWithPairTimeout(t *testing.T) {
	cfg := config{
		outputDir:   `B:\output`,
		motionTool:  `C:\motionphoto2.exe`,
		motionLimit: 45 * time.Second,
	}
	args := buildMotionMergeArgs(cfg)
	want := []string{
		"--merge-motion-pass",
		"--library", cfg.outputDir,
		"--motionphoto2", cfg.motionTool,
		"--motion-merge-timeout", "45s",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestRunCommandWatchedStopsRepeatedSameProgress(t *testing.T) {
	command := helperCommand(t, "stall")
	var out bytes.Buffer
	started := time.Now()
	err := runCommandWatched(
		context.Background(),
		&out,
		command,
		watchOptions{
			StallTimeout: 120 * time.Millisecond,
			MaxRuntime:   2 * time.Second,
			StopGrace:    2 * time.Second,
			PollInterval: 20 * time.Millisecond,
		},
	)
	var watchErr *watchdogError
	if !errors.As(err, &watchErr) {
		t.Fatalf("error = %v, want watchdogError; output=%q", err, out.String())
	}
	if watchErr.Reason != "no_progress" {
		t.Fatalf("reason = %q, want no_progress", watchErr.Reason)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("watchdog took %s", elapsed)
	}
}

func TestRunCommandWatchedAllowsMovingProgress(t *testing.T) {
	command := helperCommand(t, "progress")
	var out bytes.Buffer
	err := runCommandWatched(
		context.Background(),
		&out,
		command,
		watchOptions{
			StallTimeout: 500 * time.Millisecond,
			MaxRuntime:   2 * time.Second,
			StopGrace:    2 * time.Second,
			PollInterval: 20 * time.Millisecond,
		},
	)
	if err != nil {
		t.Fatalf("runCommandWatched: %v; output=%q", err, out.String())
	}
}

func TestRunDeferredQueueContinuesAndRetriesAtEnd(t *testing.T) {
	jobs := []zipJob{
		{Index: 45, Total: 67, Source: "zip45"},
		{Index: 46, Total: 67, Source: "zip46"},
	}
	var calls []string
	result, err := runDeferredQueue(
		context.Background(),
		ioDiscard{},
		jobs,
		0,
		func(_ context.Context, job zipJob) error {
			calls = append(calls, fmt.Sprintf("%s-%d", job.Source, job.Attempt))
			if job.Source == "zip45" {
				return fmt.Errorf("%w: disk read stuck", errDeferZip)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"zip45-1", "zip46-1", "zip45-2"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
	if result.Processed != 1 {
		t.Fatalf("processed = %d, want 1", result.Processed)
	}
	if len(result.Deferred) != 1 || result.Deferred[0].Source != "zip45" {
		t.Fatalf("deferred = %#v", result.Deferred)
	}
}

func TestRunDeferredQueueDoesNotRetryAttemptTwoAgain(t *testing.T) {
	jobs := []zipJob{
		{Index: 46, Total: 67, Source: "zip46", Attempt: 1},
		{Index: 45, Total: 67, Source: "zip45", Attempt: 2},
	}
	var calls []string
	result, err := runDeferredQueue(
		context.Background(),
		ioDiscard{},
		jobs,
		0,
		func(_ context.Context, job zipJob) error {
			calls = append(calls, fmt.Sprintf("%s-%d", job.Source, job.Attempt))
			if job.Source == "zip45" {
				return fmt.Errorf("%w: disk read stuck", errDeferZip)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"zip46-1", "zip45-2"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
	if result.Processed != 1 || len(result.Deferred) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestRecoveryRunnerHelperProcess(t *testing.T) {
	mode := os.Getenv("GTF_RUNNER_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "stall":
		for {
			fmt.Println("PROGRESS phase=coarse 1.00% pos=100 total=10000 speed=0.0 MiB/s bad_candidate_bytes=0")
			time.Sleep(20 * time.Millisecond)
		}
	case "progress":
		for position := 100; position <= 500; position += 100 {
			fmt.Printf(
				"PROGRESS phase=coarse 1.00%% pos=%d total=10000 speed=1.0 MiB/s bad_candidate_bytes=0\n",
				position,
			)
			time.Sleep(20 * time.Millisecond)
		}
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stderr, "unknown helper mode:", strconv.Quote(mode))
		os.Exit(2)
	}
}

func helperCommand(t *testing.T, mode string) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=TestRecoveryRunnerHelperProcess")
	command.Env = append(os.Environ(), "GTF_RUNNER_HELPER="+mode)
	return command
}

type ioDiscard struct{}

func (ioDiscard) Write(buffer []byte) (int, error) {
	return len(buffer), nil
}
