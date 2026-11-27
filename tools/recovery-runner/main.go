package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Terru03/google-photos-takeout-helper/tools/internal/blocksize"
)

var errDeferZip = errors.New("defer ZIP")

type rescueState struct {
	Phase    string            `json:"phase"`
	FinalBad []json.RawMessage `json:"final_bad_ranges"`
}

type manifestEntry struct {
	ZipPath string `json:"zipPath"`
	Status  string `json:"status"`
}

type config struct {
	sourceDir   string
	recoveryDir string
	outputDir   string
	workDir     string
	stagingDir  string
	rescueTool  string
	cliTool     string
	motionTool  string
	startAt     int
	stopAfter   int
	stallLimit  time.Duration
	zipLimit    time.Duration
	stopGrace   time.Duration
	readTimeout time.Duration
	motionLimit time.Duration
	// Optional rescue-file block sizes; empty means use its defaults.
	coarseBlock  string
	reverseBlock string
}

type zipJob struct {
	Index     int    `json:"index"`
	Total     int    `json:"total"`
	Source    string `json:"source"`
	Attempt   int    `json:"attempt"`
	LastError string `json:"lastError,omitempty"`
}

type queueResult struct {
	Processed int
	Deferred  []zipJob
}

type deferredReport struct {
	CreatedAt time.Time `json:"createdAt"`
	Processed int       `json:"processed"`
	Deferred  []zipJob  `json:"deferred"`
}

type watchOptions struct {
	StallTimeout time.Duration
	MaxRuntime   time.Duration
	StopGrace    time.Duration
	PollInterval time.Duration
}

type watchdogError struct {
	Reason      string
	Limit       time.Duration
	KillError   error
	ProcessExit bool
}

func (err *watchdogError) Error() string {
	message := fmt.Sprintf("command watchdog: %s after %s", err.Reason, err.Limit)
	if err.KillError != nil {
		message += ": kill: " + err.KillError.Error()
	}
	if !err.ProcessExit {
		message += ": child did not exit"
	}
	return message
}

type progressTracker struct {
	mu         sync.Mutex
	marker     string
	lastChange time.Time
}

func main() {
	var cfg config
	flag.StringVar(&cfg.sourceDir, "source-dir", "", "folder with original Takeout ZIPs")
	flag.StringVar(&cfg.recoveryDir, "recovery-dir", "", "folder for one recovered ZIP")
	flag.StringVar(&cfg.outputDir, "output", "", "final photo output")
	flag.StringVar(&cfg.workDir, "work", "", "ZIP extract work folder")
	flag.StringVar(&cfg.stagingDir, "staging-output", "", "staging output folder")
	flag.StringVar(&cfg.rescueTool, "rescue-tool", "", "rescue-file executable")
	flag.StringVar(&cfg.cliTool, "gtf-cli", "", "gtf-cli executable")
	flag.StringVar(&cfg.motionTool, "motionphoto2", "", "MotionPhoto2 executable")
	flag.IntVar(&cfg.startAt, "start", 1, "first sorted ZIP number")
	flag.IntVar(&cfg.stopAfter, "stop-after", 0, "stop after this many ZIPs; zero means all")
	flag.DurationVar(&cfg.stallLimit, "rescue-stall-limit", 5*time.Minute, "defer ZIP after no rescue progress")
	flag.DurationVar(&cfg.zipLimit, "rescue-zip-limit", 45*time.Minute, "defer ZIP after total rescue time")
	flag.DurationVar(&cfg.stopGrace, "rescue-stop-grace", 2*time.Minute, "time allowed for a stopped rescue process to exit")
	flag.DurationVar(&cfg.readTimeout, "rescue-read-timeout", 90*time.Second, "timeout passed to one rescue read")
	flag.StringVar(&cfg.coarseBlock, "rescue-coarse-block", "", "rescue-file -coarse-block, 64K to 1G (default: rescue-file default, 64M)")
	flag.StringVar(&cfg.reverseBlock, "rescue-reverse-block", "", "rescue-file -reverse-block, 64K to 1G (default: same as coarse block)")
	flag.DurationVar(&cfg.motionLimit, "motion-pair-timeout", 2*time.Minute, "maximum time for one MotionPhoto2 pair")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config) error {
	var err error
	cfg.sourceDir, err = cleanRequiredDir(cfg.sourceDir, "source-dir")
	if err != nil {
		return err
	}
	cfg.recoveryDir, err = cleanRequiredDir(cfg.recoveryDir, "recovery-dir")
	if err != nil {
		return err
	}
	cfg.outputDir, err = cleanRequiredDir(cfg.outputDir, "output")
	if err != nil {
		return err
	}
	cfg.workDir, err = cleanRequiredDir(cfg.workDir, "work")
	if err != nil {
		return err
	}
	cfg.stagingDir, err = cleanRequiredDir(cfg.stagingDir, "staging-output")
	if err != nil {
		return err
	}
	for name, path := range map[string]*string{
		"rescue-tool":  &cfg.rescueTool,
		"gtf-cli":      &cfg.cliTool,
		"motionphoto2": &cfg.motionTool,
	} {
		*path, err = cleanRequiredFile(*path, name)
		if err != nil {
			return err
		}
	}
	if cfg.startAt < 1 {
		return errors.New("start must be at least 1")
	}
	if cfg.stallLimit <= 0 {
		return errors.New("rescue-stall-limit must be positive")
	}
	if cfg.zipLimit <= 0 {
		return errors.New("rescue-zip-limit must be positive")
	}
	if cfg.stopGrace <= 0 {
		return errors.New("rescue-stop-grace must be positive")
	}
	if cfg.readTimeout <= 0 {
		return errors.New("rescue-read-timeout must be positive")
	}
	if err := validateBlockFlags(&cfg); err != nil {
		return err
	}
	if cfg.motionLimit <= 0 {
		return errors.New("motion-pair-timeout must be positive")
	}

	sources, err := findSourceZips(cfg.sourceDir)
	if err != nil {
		return err
	}
	if cfg.startAt > len(sources) {
		return fmt.Errorf("start %d exceeds ZIP count %d", cfg.startAt, len(sources))
	}

	archiveDir := filepath.Join(cfg.outputDir, ".gtf", "recovery")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return err
	}
	logPath := filepath.Join(archiveDir, "runner-"+time.Now().UTC().Format("20060102-150405")+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	out := io.MultiWriter(os.Stdout, logFile)

	jobs := make([]zipJob, 0, len(sources)-cfg.startAt+1)
	var postponed []zipJob
	for index := cfg.startAt - 1; index < len(sources); index++ {
		job := zipJob{
			Index:   index + 1,
			Total:   len(sources),
			Source:  sources[index],
			Attempt: 1,
		}
		target := filepath.Join(
			cfg.recoveryDir,
			strings.TrimSuffix(filepath.Base(job.Source), filepath.Ext(job.Source))+".recovered.zip",
		)
		if state, stateErr := loadRescueState(target + ".rescue-state.json"); stateErr == nil && state.Phase == "reverse" {
			job.Attempt = 2
			job.LastError = "saved reverse rescue state"
			postponed = append(postponed, job)
			fmt.Fprintf(
				out,
				"POSTPONE %d/%d %s saved_phase=reverse\n",
				job.Index,
				job.Total,
				filepath.Base(job.Source),
			)
			continue
		}
		jobs = append(jobs, job)
	}
	jobs = append(jobs, postponed...)
	result, err := runDeferredQueue(
		ctx,
		out,
		jobs,
		cfg.stopAfter,
		func(ctx context.Context, job zipJob) error {
			return processZip(ctx, cfg, archiveDir, out, job)
		},
	)
	if err != nil {
		return err
	}
	if cfg.stopAfter == 0 && len(result.Deferred) == 0 {
		fmt.Fprintln(out, "MOTION FINAL START after all ZIP files")
		if err := runCommand(ctx, out, cfg.cliTool, buildMotionMergeArgs(cfg)...); err != nil {
			return fmt.Errorf("final motion merge: %w", err)
		}
		fmt.Fprintln(out, "MOTION FINAL DONE")
	}
	reportPath := filepath.Join(
		archiveDir,
		"queue-"+time.Now().UTC().Format("20060102-150405")+".json",
	)
	report := deferredReport{
		CreatedAt: time.Now().UTC(),
		Processed: result.Processed,
		Deferred:  result.Deferred,
	}
	if err := writeJSONFile(reportPath, report); err != nil {
		return err
	}
	fmt.Fprintf(
		out,
		"QUEUE DONE processed=%d deferred=%d report=%s log=%s\n",
		result.Processed,
		len(result.Deferred),
		reportPath,
		logPath,
	)
	return nil
}

func processZip(ctx context.Context, cfg config, archiveDir string, out io.Writer, job zipJob) error {
	source := job.Source
	target := filepath.Join(
		cfg.recoveryDir,
		strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))+".recovered.zip",
	)
	if err := requireChildPath(cfg.recoveryDir, target); err != nil {
		return err
	}

	archiveState := filepath.Join(archiveDir, filepath.Base(target)+".rescue-state.json")
	if !fileExists(target) && fileExists(archiveState) && manifestSuccessful(cfg.outputDir, target) {
		fmt.Fprintf(out, "SKIP done %d/%d %s\n", job.Index, job.Total, filepath.Base(source))
		return nil
	}
	if err := requireEmptyDir(cfg.workDir); err != nil {
		return err
	}
	if err := requireEmptyDir(cfg.stagingDir); err != nil {
		return err
	}

	fmt.Fprintf(
		out,
		"START %d/%d attempt=%d %s\n",
		job.Index,
		job.Total,
		job.Attempt,
		filepath.Base(source),
	)
	statePath := target + ".rescue-state.json"
	state, stateErr := loadRescueState(statePath)
	switch {
	case stateErr == nil && state.Phase == "done" && fileExists(target):
		fmt.Fprintf(out, "RESCUE already done bad_ranges=%d\n", len(state.FinalBad))
	case errors.Is(stateErr, os.ErrNotExist) && !fileExists(target):
		if err := runRescueCommand(ctx, cfg, out, job, source, target, "", false); err != nil {
			return classifyRescueError(source, err)
		}
	case stateErr == nil && fileExists(target):
		reverse := job.Attempt > 1 && state.Phase == "coarse"
		if err := runRescueCommand(ctx, cfg, out, job, source, target, state.Phase, reverse); err != nil {
			return classifyRescueError(source, err)
		}
	case stateErr != nil && !errors.Is(stateErr, os.ErrNotExist):
		return fmt.Errorf("%w: read rescue state for %s: %v", errDeferZip, filepath.Base(source), stateErr)
	default:
		return fmt.Errorf("%w: target/state mismatch for %s", errDeferZip, filepath.Base(source))
	}

	state, err := loadRescueState(statePath)
	if err != nil {
		return fmt.Errorf("%w: load final rescue state for %s: %v", errDeferZip, filepath.Base(source), err)
	}
	if state.Phase != "done" {
		return fmt.Errorf(
			"%w: rescue incomplete for %s: phase=%s",
			errDeferZip,
			filepath.Base(source),
			state.Phase,
		)
	}
	if len(state.FinalBad) != 0 {
		fmt.Fprintf(
			out,
			"RESCUE review %s bad_ranges=%d; try salvage process\n",
			filepath.Base(source),
			len(state.FinalBad),
		)
	}

	fmt.Fprintf(out, "PROCESS %d/%d %s\n", job.Index, job.Total, filepath.Base(target))
	if err := runCommand(
		ctx,
		out,
		cfg.cliTool,
		buildZipProcessArgs(cfg, target)...,
	); err != nil {
		cleanupErr := clearTemporaryDirs(cfg.workDir, cfg.stagingDir)
		if cleanupErr != nil {
			return fmt.Errorf("process %s: %w; cleanup: %v", filepath.Base(source), err, cleanupErr)
		}
		return fmt.Errorf("%w: process %s: %v", errDeferZip, filepath.Base(source), err)
	}
	if !manifestSuccessful(cfg.outputDir, target) {
		if err := clearTemporaryDirs(cfg.workDir, cfg.stagingDir); err != nil {
			return err
		}
		return fmt.Errorf("%w: manifest does not mark %s complete", errDeferZip, filepath.Base(target))
	}

	for _, path := range []string{statePath, target + ".rescue.log"} {
		if err := copyFile(path, filepath.Join(archiveDir, filepath.Base(path))); err != nil {
			return err
		}
	}
	for _, path := range []string{target, statePath, target + ".rescue.log"} {
		if err := requireChildPath(cfg.recoveryDir, path); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	fmt.Fprintf(out, "DONE %d/%d %s\n", job.Index, job.Total, filepath.Base(source))
	return nil
}

func buildZipProcessArgs(cfg config, target string) []string {
	return []string{
		"--one-zip", target,
		"--work", cfg.workDir,
		"--staging-output", cfg.stagingDir,
		"--output", cfg.outputDir,
		"--timeline-only",
		"--metadata-mode", "file",
		"--verify",
		"--conflict-policy", "merge",
	}
}

func buildMotionMergeArgs(cfg config) []string {
	return []string{
		"--merge-motion-pass",
		"--library", cfg.outputDir,
		"--motionphoto2", cfg.motionTool,
		"--motion-merge-timeout", cfg.motionLimit.String(),
	}
}

func runDeferredQueue(
	ctx context.Context,
	out io.Writer,
	jobs []zipJob,
	stopAfter int,
	execute func(context.Context, zipJob) error,
) (queueResult, error) {
	var result queueResult
	var retry []zipJob
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if stopAfter > 0 && result.Processed >= stopAfter {
			break
		}
		if job.Attempt < 1 {
			job.Attempt = 1
		}
		err := execute(ctx, job)
		switch {
		case err == nil:
			result.Processed++
		case errors.Is(err, errDeferZip):
			if job.Attempt >= 2 {
				job.LastError = err.Error()
				result.Deferred = append(result.Deferred, job)
				fmt.Fprintf(
					out,
					"FINAL DEFER %d/%d %s reason=%q\n",
					job.Index,
					job.Total,
					filepath.Base(job.Source),
					err,
				)
				continue
			}
			job.Attempt = 2
			job.LastError = err.Error()
			retry = append(retry, job)
			fmt.Fprintf(
				out,
				"DEFER %d/%d %s reason=%q\n",
				job.Index,
				job.Total,
				filepath.Base(job.Source),
				err,
			)
		default:
			return result, err
		}
	}

	for _, job := range retry {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if stopAfter > 0 && result.Processed >= stopAfter {
			break
		}
		err := execute(ctx, job)
		switch {
		case err == nil:
			result.Processed++
		case errors.Is(err, errDeferZip):
			job.LastError = err.Error()
			result.Deferred = append(result.Deferred, job)
			fmt.Fprintf(
				out,
				"FINAL DEFER %d/%d %s reason=%q\n",
				job.Index,
				job.Total,
				filepath.Base(job.Source),
				err,
			)
		default:
			return result, err
		}
	}
	return result, nil
}

// buildRescueArgs builds the rescue-file command line. Block sizes are forwarded
// only when set, so rescue-file keeps its own default otherwise.
func buildRescueArgs(cfg config, source string, target string, reverse bool) []string {
	args := []string{
		"--source", source,
		"--target", target,
		"--read-timeout", cfg.readTimeout.String(),
	}
	if cfg.coarseBlock != "" {
		args = append(args, "--coarse-block", cfg.coarseBlock)
	}
	if cfg.reverseBlock != "" {
		args = append(args, "--reverse-block", cfg.reverseBlock)
	}
	if reverse {
		args = append(args, "--reverse")
	}
	return args
}

func blockLabel(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// validateBlockFlags checks block sizes once at startup, so a typo stops the
// runner immediately instead of failing and deferring every ZIP in the queue.
func validateBlockFlags(cfg *config) error {
	for _, block := range []struct {
		name  string
		value *string
	}{
		{"rescue-coarse-block", &cfg.coarseBlock},
		{"rescue-reverse-block", &cfg.reverseBlock},
	} {
		*block.value = strings.TrimSpace(*block.value)
		if *block.value == "" {
			continue
		}
		if _, err := blocksize.Parse(*block.value); err != nil {
			return fmt.Errorf("%s: %w", block.name, err)
		}
	}
	return nil
}

func runRescueCommand(
	ctx context.Context,
	cfg config,
	out io.Writer,
	job zipJob,
	source string,
	target string,
	statePhase string,
	reverse bool,
) error {
	mode := "forward"
	if statePhase != "" {
		mode = "resume-" + statePhase
	}
	args := buildRescueArgs(cfg, source, target, reverse)
	if reverse {
		mode = "reverse"
	}
	fmt.Fprintf(
		out,
		"RESCUE attempt=%d mode=%s stall_limit=%s zip_limit=%s read_timeout=%s coarse_block=%s reverse_block=%s\n",
		job.Attempt,
		mode,
		cfg.stallLimit,
		cfg.zipLimit,
		cfg.readTimeout,
		blockLabel(cfg.coarseBlock, blocksize.Default),
		blockLabel(cfg.reverseBlock, blockLabel(cfg.coarseBlock, blocksize.Default)),
	)
	fmt.Fprintf(out, "RUN %s %s\n", cfg.rescueTool, strings.Join(args, " "))
	command := exec.Command(cfg.rescueTool, args...)
	return runCommandWatched(
		ctx,
		out,
		command,
		watchOptions{
			StallTimeout: cfg.stallLimit,
			MaxRuntime:   cfg.zipLimit,
			StopGrace:    cfg.stopGrace,
			PollInterval: time.Second,
		},
	)
}

func classifyRescueError(source string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var watchErr *watchdogError
	if errors.As(err, &watchErr) && !watchErr.ProcessExit {
		return fmt.Errorf(
			"rescue process did not stop for %s; queue halted to prevent duplicate disk reads: %w",
			filepath.Base(source),
			err,
		)
	}
	return fmt.Errorf("%w: rescue %s: %v", errDeferZip, filepath.Base(source), err)
}

func runCommandWatched(
	ctx context.Context,
	out io.Writer,
	command *exec.Cmd,
	options watchOptions,
) error {
	if options.StallTimeout <= 0 || options.MaxRuntime <= 0 || options.StopGrace <= 0 {
		return errors.New("watch limits must be positive")
	}
	if options.PollInterval <= 0 {
		options.PollInterval = time.Second
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}

	started := time.Now()
	tracker := &progressTracker{lastChange: started}
	var outputMu sync.Mutex
	var pumps sync.WaitGroup
	pumps.Add(2)
	go pumpCommandOutput(stdout, out, &outputMu, tracker, &pumps)
	go pumpCommandOutput(stderr, out, &outputMu, tracker, &pumps)

	wait := make(chan error, 1)
	go func() {
		waitErr := command.Wait()
		pumps.Wait()
		wait <- waitErr
	}()

	ticker := time.NewTicker(options.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case waitErr := <-wait:
			return waitErr
		case <-ctx.Done():
			stopErr := stopWatchedCommand(command, wait, "context_cancel", 0, options.StopGrace)
			if !stopErr.ProcessExit {
				return stopErr
			}
			return ctx.Err()
		case now := <-ticker.C:
			if now.Sub(started) >= options.MaxRuntime {
				return stopWatchedCommand(command, wait, "runtime_limit", options.MaxRuntime, options.StopGrace)
			}
			if now.Sub(tracker.lastMoved()) >= options.StallTimeout {
				return stopWatchedCommand(command, wait, "no_progress", options.StallTimeout, options.StopGrace)
			}
		}
	}
}

func stopWatchedCommand(
	command *exec.Cmd,
	wait <-chan error,
	reason string,
	limit time.Duration,
	grace time.Duration,
) *watchdogError {
	killErr := command.Process.Kill()
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-wait:
		return &watchdogError{
			Reason:      reason,
			Limit:       limit,
			KillError:   killErr,
			ProcessExit: true,
		}
	case <-timer.C:
		return &watchdogError{
			Reason:      reason,
			Limit:       limit,
			KillError:   killErr,
			ProcessExit: false,
		}
	}
}

func pumpCommandOutput(
	input io.Reader,
	out io.Writer,
	outputMu *sync.Mutex,
	tracker *progressTracker,
	done *sync.WaitGroup,
) {
	defer done.Done()
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		tracker.observe(line, time.Now())
		outputMu.Lock()
		fmt.Fprintln(out, line)
		outputMu.Unlock()
	}
	if err := scanner.Err(); err != nil {
		outputMu.Lock()
		fmt.Fprintln(out, "OUTPUT ERROR:", err)
		outputMu.Unlock()
	}
}

func (tracker *progressTracker) observe(line string, now time.Time) {
	marker, ok := rescueProgressMarker(line)
	if !ok {
		return
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if marker != tracker.marker {
		tracker.marker = marker
		tracker.lastChange = now
	}
}

func (tracker *progressTracker) lastMoved() time.Time {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.lastChange
}

func rescueProgressMarker(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "PROGRESS ") {
		return "", false
	}
	phase := ""
	position := ""
	badBytes := ""
	for _, field := range strings.Fields(line) {
		switch {
		case strings.HasPrefix(field, "phase="):
			phase = field
		case strings.HasPrefix(field, "pos="):
			position = field
		case strings.HasPrefix(field, "bad_candidate_bytes="):
			badBytes = field
		}
	}
	if phase == "" || position == "" {
		return "", false
	}
	return strings.Join([]string{phase, position, badBytes}, "|"), true
}

func findSourceZips(sourceDir string) ([]string, error) {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.HasPrefix(name, "takeout-") && strings.HasSuffix(name, ".zip") {
			paths = append(paths, filepath.Join(sourceDir, entry.Name()))
		}
	}
	sort.Slice(paths, func(i int, j int) bool {
		return strings.ToLower(filepath.Base(paths[i])) < strings.ToLower(filepath.Base(paths[j]))
	})
	if len(paths) == 0 {
		return nil, errors.New("no Takeout ZIPs found")
	}
	return paths, nil
}

func cleanRequiredDir(path string, name string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a folder: %s", name, abs)
	}
	return filepath.Clean(abs), nil
}

func cleanRequiredFile(path string, name string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a file: %s", name, abs)
	}
	return filepath.Clean(abs), nil
}

func requireChildPath(root string, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("unsafe path outside recovery folder: %s", path)
	}
	return nil
}

func requireEmptyDir(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("folder must be empty before next ZIP: %s", path)
	}
	return nil
}

func loadRescueState(path string) (rescueState, error) {
	var state rescueState
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	return state, json.Unmarshal(data, &state)
}

func runCommand(ctx context.Context, out io.Writer, name string, args ...string) error {
	fmt.Fprintf(out, "RUN %s %s\n", name, strings.Join(args, " "))
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = out
	command.Stderr = out
	if err := command.Run(); err != nil {
		return err
	}
	return nil
}

func manifestSuccessful(outputDir string, zipPath string) bool {
	path := filepath.Join(outputDir, ".gtf", "batch", "manifest.jsonl")
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	status := ""
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var entry manifestEntry
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && strings.EqualFold(filepath.Clean(entry.ZipPath), filepath.Clean(zipPath)) {
			status = entry.Status
		}
	}
	return status == "completed" || status == "completed_with_review"
}

func copyFile(source string, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func clearTemporaryDirs(paths ...string) error {
	for _, path := range paths {
		root, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		root = filepath.Clean(root)
		volumeRoot := filepath.VolumeName(root) + string(filepath.Separator)
		if strings.EqualFold(root, volumeRoot) {
			return fmt.Errorf("refuse to clear volume root: %s", root)
		}
		info, err := os.Stat(root)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("temporary path is not a folder: %s", root)
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			child := filepath.Join(root, entry.Name())
			if err := requireChildPath(root, child); err != nil {
				return err
			}
			if err := os.RemoveAll(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
