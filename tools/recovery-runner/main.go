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
	"syscall"
	"time"
)

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

	done := 0
	for index := cfg.startAt - 1; index < len(sources); index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if cfg.stopAfter > 0 && done >= cfg.stopAfter {
			break
		}
		source := sources[index]
		target := filepath.Join(
			cfg.recoveryDir,
			strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))+".recovered.zip",
		)
		if err := requireChildPath(cfg.recoveryDir, target); err != nil {
			return err
		}

		archiveState := filepath.Join(archiveDir, filepath.Base(target)+".rescue-state.json")
		if !fileExists(target) && fileExists(archiveState) && manifestSuccessful(cfg.outputDir, target) {
			fmt.Fprintf(out, "SKIP done %d/%d %s\n", index+1, len(sources), filepath.Base(source))
			continue
		}
		if err := requireEmptyDir(cfg.workDir); err != nil {
			return err
		}
		if err := requireEmptyDir(cfg.stagingDir); err != nil {
			return err
		}

		fmt.Fprintf(out, "START %d/%d %s\n", index+1, len(sources), filepath.Base(source))
		statePath := target + ".rescue-state.json"
		state, stateErr := loadRescueState(statePath)
		switch {
		case stateErr == nil && state.Phase == "done" && fileExists(target):
			fmt.Fprintf(out, "RESCUE already done bad_ranges=%d\n", len(state.FinalBad))
		case errors.Is(stateErr, os.ErrNotExist) && !fileExists(target):
			if err := runCommand(ctx, out, cfg.rescueTool, "--source", source, "--target", target, "--read-timeout", "5s"); err != nil {
				return fmt.Errorf("rescue %s: %w", filepath.Base(source), err)
			}
		case stateErr == nil && fileExists(target):
			if err := runCommand(ctx, out, cfg.rescueTool, "--source", source, "--target", target, "--read-timeout", "5s"); err != nil {
				return fmt.Errorf("resume rescue %s: %w", filepath.Base(source), err)
			}
		case stateErr != nil && !errors.Is(stateErr, os.ErrNotExist):
			return fmt.Errorf("read rescue state for %s: %w", filepath.Base(source), stateErr)
		default:
			return fmt.Errorf("target/state mismatch for %s", filepath.Base(source))
		}

		state, err = loadRescueState(statePath)
		if err != nil {
			return err
		}
		if state.Phase != "done" || len(state.FinalBad) != 0 {
			return fmt.Errorf(
				"rescue not clean for %s: phase=%s bad_ranges=%d; keep recovered file for review",
				filepath.Base(source),
				state.Phase,
				len(state.FinalBad),
			)
		}

		fmt.Fprintf(out, "PROCESS %d/%d %s\n", index+1, len(sources), filepath.Base(target))
		if err := runCommand(
			ctx,
			out,
			cfg.cliTool,
			"--one-zip", target,
			"--work", cfg.workDir,
			"--staging-output", cfg.stagingDir,
			"--output", cfg.outputDir,
			"--timeline-only",
			"--metadata-mode", "file",
			"--verify",
			"--conflict-policy", "merge",
			"--motion-photos",
			"--motionphoto2", cfg.motionTool,
		); err != nil {
			return fmt.Errorf("process %s: %w", filepath.Base(source), err)
		}
		if !manifestSuccessful(cfg.outputDir, target) {
			return fmt.Errorf("manifest does not mark %s complete", filepath.Base(target))
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

		done++
		fmt.Fprintf(out, "DONE %d/%d %s\n", index+1, len(sources), filepath.Base(source))
	}
	fmt.Fprintf(out, "QUEUE DONE processed=%d log=%s\n", done, logPath)
	return nil
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
