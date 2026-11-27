package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Terru03/google-photos-takeout-helper/tools/internal/blocksize"
)

const (
	saveEvery = int64(256 * 1024 * 1024)
)

// blockSizes sets the read size for the first-pass coarse and reverse copies.
// Large blocks amortize slow-disk read latency; small blocks lose less data per
// failed read on badly damaged zones. Bad ranges are always re-read by the
// refine passes at 64 KiB, 4 KiB and 512 bytes, so changing these between
// resumed runs is safe.
type blockSizes struct {
	coarse  int64
	reverse int64
}

func parseBlockSizes(coarseValue, reverseValue string) (blockSizes, error) {
	coarse, err := blocksize.Parse(coarseValue)
	if err != nil {
		return blockSizes{}, fmt.Errorf("-coarse-block: %w", err)
	}
	if strings.TrimSpace(reverseValue) == "" {
		return blockSizes{coarse: coarse, reverse: coarse}, nil
	}
	reverse, err := blocksize.Parse(reverseValue)
	if err != nil {
		return blockSizes{}, fmt.Errorf("-reverse-block: %w", err)
	}
	return blockSizes{coarse: coarse, reverse: reverse}, nil
}

type sourceReader interface {
	io.ReaderAt
	io.Closer
}

type byteRange struct {
	Start int64 `json:"start"`
	Size  int64 `json:"size"`
}

type rescueState struct {
	Version       int         `json:"version"`
	Source        string      `json:"source"`
	SourceSize    int64       `json:"source_size"`
	SourceModTime int64       `json:"source_mod_time"`
	Target        string      `json:"target"`
	Phase         string      `json:"phase"`
	Offset        int64       `json:"offset"`
	BlockSize     int64       `json:"block_size"`
	Ranges        []byteRange `json:"ranges"`
	NextRanges    []byteRange `json:"next_ranges"`
	RangeIndex    int         `json:"range_index"`
	RangeOffset   int64       `json:"range_offset"`
	ReverseOffset int64       `json:"reverse_offset"`
	FinalBad      []byteRange `json:"final_bad_ranges"`
}

type progressState struct {
	phase    atomic.Value
	position atomic.Int64
	total    atomic.Int64
	read     atomic.Int64
	badBytes atomic.Int64
}

func main() {
	sourceArg := flag.String("source", "", "source file")
	targetArg := flag.String("target", "", "target file")
	stateArg := flag.String("state", "", "state file")
	reverseArg := flag.Bool("reverse", false, "copy remaining bytes from end to front")
	readTimeoutArg := flag.Duration("read-timeout", 5*time.Second, "max time for one source read")
	coarseBlockArg := flag.String("coarse-block", blocksize.Default, "first-pass read size, 64K to 1G (use 64K for badly damaged disks)")
	reverseBlockArg := flag.String("reverse-block", "", "reverse-pass read size, 64K to 1G (default: same as -coarse-block)")
	flag.Parse()

	if *sourceArg == "" || *targetArg == "" {
		fail("source and target are required")
	}
	blocks, err := parseBlockSizes(*coarseBlockArg, *reverseBlockArg)
	if err != nil {
		fail(err.Error())
	}

	source, err := filepath.Abs(*sourceArg)
	check(err)
	target, err := filepath.Abs(*targetArg)
	check(err)
	if strings.EqualFold(source, target) {
		fail("source and target must differ")
	}

	statePath := *stateArg
	if statePath == "" {
		statePath = target + ".rescue-state.json"
	}
	statePath, err = filepath.Abs(statePath)
	check(err)
	logPath := target + ".rescue.log"

	sourceInfo, err := os.Stat(source)
	check(err)
	if !sourceInfo.Mode().IsRegular() {
		fail("source is not a regular file")
	}

	src, err := openRescueSource(source, *readTimeoutArg)
	check(err)
	defer src.Close()

	state, resumed, err := loadOrMakeState(statePath, source, target, sourceInfo, blocks.coarse)
	check(err)
	if *reverseArg {
		if state.Phase != "coarse" && state.Phase != "reverse" {
			fail("reverse can start only during coarse phase")
		}
		if state.Phase == "coarse" {
			state.Phase = "reverse"
			state.ReverseOffset = state.SourceSize
		}
	}

	var dst *os.File
	if resumed {
		dst, err = os.OpenFile(target, os.O_RDWR, 0)
	} else {
		dst, err = os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	}
	check(err)
	defer dst.Close()

	targetInfo, err := dst.Stat()
	check(err)
	if resumed && targetInfo.Size() != sourceInfo.Size() {
		fail("target size does not match saved state")
	}
	if !resumed {
		check(dst.Truncate(sourceInfo.Size()))
	}
	check(saveState(statePath, state))

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	check(err)
	defer logFile.Close()
	logLine(logFile, "start source=%q target=%q size=%d resumed=%t phase=%s coarse_block=%d reverse_block=%d", source, target, sourceInfo.Size(), resumed, state.Phase, blocks.coarse, blocks.reverse)

	var progress progressState
	progress.phase.Store(state.Phase)
	progress.total.Store(sourceInfo.Size())
	progress.position.Store(state.Offset)

	stop := make(chan struct{})
	done := make(chan struct{})
	go showProgress(&progress, stop, done)

	var stopAsked atomic.Bool
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		stopAsked.Store(true)
		fmt.Println("Stop asked. Save state after current read.")
	}()

	err = runRescue(src, dst, statePath, logFile, state, &progress, &stopAsked, blocks)
	close(stop)
	<-done
	if err != nil {
		check(saveState(statePath, state))
		logLine(logFile, "stop phase=%s error=%q", state.Phase, err)
		fail(err.Error())
	}

	check(dst.Sync())
	check(saveState(statePath, state))
	logLine(logFile, "done final_bad_ranges=%d final_bad_bytes=%d", len(state.FinalBad), sumRanges(state.FinalBad))

	fmt.Printf("DONE target=%s\n", target)
	fmt.Printf("Final bad ranges: %d\n", len(state.FinalBad))
	fmt.Printf("Final bad bytes: %d\n", sumRanges(state.FinalBad))
	fmt.Printf("State: %s\n", statePath)
	fmt.Printf("Log: %s\n", logPath)
}

func runRescue(src sourceReader, dst *os.File, statePath string, logFile *os.File, state *rescueState, progress *progressState, stopAsked *atomic.Bool, blocks blockSizes) error {
	if state.Phase == "coarse" {
		if err := runCoarse(src, dst, statePath, logFile, state, progress, stopAsked, blocks.coarse); err != nil {
			return err
		}
	}
	if state.Phase == "reverse" {
		if err := runReverse(src, dst, statePath, logFile, state, progress, stopAsked, blocks.reverse); err != nil {
			return err
		}
	}

	for _, nextBlock := range []int64{64 * 1024, 4 * 1024, 512} {
		if state.Phase == "done" {
			break
		}
		if state.BlockSize != nextBlock {
			continue
		}
		if err := runRefine(src, dst, statePath, logFile, state, progress, stopAsked); err != nil {
			return err
		}
	}
	return nil
}

func runReverse(src sourceReader, dst *os.File, statePath string, logFile *os.File, state *rescueState, progress *progressState, stopAsked *atomic.Bool, reverseBlock int64) error {
	if state.ReverseOffset == 0 {
		state.ReverseOffset = state.SourceSize
	}
	buf := make([]byte, reverseBlock)
	zero := make([]byte, reverseBlock)
	lastSave := state.SourceSize - state.ReverseOffset
	progress.phase.Store("reverse")
	progress.position.Store(state.SourceSize - state.ReverseOffset)
	progress.total.Store(state.SourceSize - state.Offset)

	for state.ReverseOffset > state.Offset {
		if stopAsked.Load() {
			return errors.New("stopped by user")
		}

		want := min64(reverseBlock, state.ReverseOffset-state.Offset)
		at := state.ReverseOffset - want
		n, readErr := src.ReadAt(buf[:want], at)
		progress.read.Add(int64(n))

		if readErr == nil || (readErr == io.EOF && int64(n) == want) {
			if err := writeFullAt(dst, buf[:want], at); err != nil {
				return fmt.Errorf("target reverse write failed at %d: %w", at, err)
			}
		} else {
			if err := writeFullAt(dst, zero[:want], at); err != nil {
				return fmt.Errorf("target reverse zero-fill failed at %d: %w", at, err)
			}
			r := byteRange{Start: at, Size: want}
			state.Ranges = append(state.Ranges, r)
			progress.badBytes.Add(want)
			logLine(logFile, "reverse_read_error start=%d size=%d read=%d error=%q", at, want, n, readErr)
			fmt.Printf("BAD reverse range start=%d size=%d error=%v\n", at, want, readErr)
		}

		state.ReverseOffset = at
		processed := state.SourceSize - state.ReverseOffset
		progress.position.Store(processed)
		if processed-lastSave >= saveEvery || readErr != nil {
			if err := dst.Sync(); err != nil {
				return err
			}
			if err := saveState(statePath, state); err != nil {
				return err
			}
			lastSave = processed
		}
	}

	state.Phase = "refine"
	state.BlockSize = 64 * 1024
	state.RangeIndex = 0
	state.RangeOffset = 0
	state.NextRanges = nil
	progress.phase.Store("refine-65536")
	progress.position.Store(0)
	progress.total.Store(sumRanges(state.Ranges))
	return saveState(statePath, state)
}

func runCoarse(src sourceReader, dst *os.File, statePath string, logFile *os.File, state *rescueState, progress *progressState, stopAsked *atomic.Bool, coarseBlock int64) error {
	buf := make([]byte, coarseBlock)
	zero := make([]byte, coarseBlock)
	lastSave := state.Offset

	for state.Offset < state.SourceSize {
		if stopAsked.Load() {
			return errors.New("stopped by user")
		}

		want := min64(coarseBlock, state.SourceSize-state.Offset)
		n, readErr := src.ReadAt(buf[:want], state.Offset)
		progress.read.Add(int64(n))

		if readErr == nil || (readErr == io.EOF && int64(n) == want) {
			if err := writeFullAt(dst, buf[:want], state.Offset); err != nil {
				return fmt.Errorf("target write failed at %d: %w", state.Offset, err)
			}
		} else {
			if err := writeFullAt(dst, zero[:want], state.Offset); err != nil {
				return fmt.Errorf("target zero-fill failed at %d: %w", state.Offset, err)
			}
			r := byteRange{Start: state.Offset, Size: want}
			state.Ranges = append(state.Ranges, r)
			progress.badBytes.Add(want)
			logLine(logFile, "coarse_read_error start=%d size=%d read=%d error=%q", state.Offset, want, n, readErr)
			fmt.Printf("BAD coarse range start=%d size=%d error=%v\n", state.Offset, want, readErr)
		}

		state.Offset += want
		progress.position.Store(state.Offset)
		if state.Offset-lastSave >= saveEvery || readErr != nil {
			if err := dst.Sync(); err != nil {
				return err
			}
			if err := saveState(statePath, state); err != nil {
				return err
			}
			lastSave = state.Offset
		}
	}

	state.Phase = "refine"
	state.BlockSize = 64 * 1024
	state.Offset = 0
	state.RangeIndex = 0
	state.RangeOffset = 0
	state.NextRanges = nil
	progress.phase.Store("refine-65536")
	progress.position.Store(0)
	progress.total.Store(sumRanges(state.Ranges))
	return saveState(statePath, state)
}

func runRefine(src sourceReader, dst *os.File, statePath string, logFile *os.File, state *rescueState, progress *progressState, stopAsked *atomic.Bool) error {
	block := state.BlockSize
	buf := make([]byte, block)
	phaseTotal := sumRanges(state.Ranges)
	processedBefore := int64(0)
	for i := 0; i < state.RangeIndex && i < len(state.Ranges); i++ {
		processedBefore += state.Ranges[i].Size
	}
	progress.phase.Store(fmt.Sprintf("refine-%d", block))
	progress.total.Store(phaseTotal)
	progress.position.Store(processedBefore + state.RangeOffset)
	progress.badBytes.Store(sumRanges(state.NextRanges))
	lastSaveAt := processedBefore + state.RangeOffset

	for state.RangeIndex < len(state.Ranges) {
		if stopAsked.Load() {
			return errors.New("stopped by user")
		}

		current := state.Ranges[state.RangeIndex]
		for state.RangeOffset < current.Size {
			if stopAsked.Load() {
				return errors.New("stopped by user")
			}

			want := min64(block, current.Size-state.RangeOffset)
			at := current.Start + state.RangeOffset
			n, readErr := src.ReadAt(buf[:want], at)
			progress.read.Add(int64(n))

			if readErr == nil || (readErr == io.EOF && int64(n) == want) {
				if err := writeFullAt(dst, buf[:want], at); err != nil {
					return fmt.Errorf("target refine write failed at %d: %w", at, err)
				}
			} else {
				r := byteRange{Start: at, Size: want}
				state.NextRanges = append(state.NextRanges, r)
				progress.badBytes.Add(want)
				logLine(logFile, "refine_read_error block=%d start=%d size=%d read=%d error=%q", block, at, want, n, readErr)
				fmt.Printf("BAD refine block=%d start=%d size=%d error=%v\n", block, at, want, readErr)
			}

			state.RangeOffset += want
			position := processedBefore + state.RangeOffset
			progress.position.Store(position)
			if position-lastSaveAt >= saveEvery || readErr != nil {
				if err := saveState(statePath, state); err != nil {
					return err
				}
				lastSaveAt = position
			}
		}

		processedBefore += current.Size
		state.RangeIndex++
		state.RangeOffset = 0
		progress.position.Store(processedBefore)
	}

	if block == 512 {
		state.FinalBad = append([]byteRange(nil), state.NextRanges...)
		state.Phase = "done"
		state.Ranges = nil
		state.NextRanges = nil
		state.RangeIndex = 0
		state.RangeOffset = 0
		progress.phase.Store("done")
		progress.position.Store(state.SourceSize)
		progress.total.Store(state.SourceSize)
		progress.badBytes.Store(sumRanges(state.FinalBad))
		return saveState(statePath, state)
	}

	state.Ranges = append([]byteRange(nil), state.NextRanges...)
	state.NextRanges = nil
	state.RangeIndex = 0
	state.RangeOffset = 0
	if block == 64*1024 {
		state.BlockSize = 4 * 1024
	} else {
		state.BlockSize = 512
	}
	progress.phase.Store(fmt.Sprintf("refine-%d", state.BlockSize))
	progress.position.Store(0)
	progress.total.Store(sumRanges(state.Ranges))
	progress.badBytes.Store(0)
	return saveState(statePath, state)
}

func loadOrMakeState(path, source, target string, sourceInfo os.FileInfo, coarseBlock int64) (*rescueState, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &rescueState{
			Version:       1,
			Source:        source,
			SourceSize:    sourceInfo.Size(),
			SourceModTime: sourceInfo.ModTime().UnixNano(),
			Target:        target,
			Phase:         "coarse",
			BlockSize:     coarseBlock,
		}, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	var state rescueState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, false, err
	}
	if state.Version != 1 ||
		!strings.EqualFold(state.Source, source) ||
		!strings.EqualFold(state.Target, target) ||
		state.SourceSize != sourceInfo.Size() ||
		state.SourceModTime != sourceInfo.ModTime().UnixNano() {
		return nil, false, errors.New("saved state does not match source and target")
	}
	return &state, true, nil
}

func saveState(path string, state *rescueState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeFullAt(dst *os.File, data []byte, offset int64) error {
	for len(data) > 0 {
		n, err := dst.WriteAt(data, offset)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		offset += int64(n)
		data = data[n:]
	}
	return nil
}

func showProgress(progress *progressState, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	lastRead := int64(0)
	lastTime := time.Now()

	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			phase, _ := progress.phase.Load().(string)
			position := progress.position.Load()
			total := progress.total.Load()
			read := progress.read.Load()
			elapsed := now.Sub(lastTime).Seconds()
			speed := float64(read-lastRead) / elapsed
			percent := 100.0
			if total > 0 {
				percent = float64(position) * 100 / float64(total)
			}
			fmt.Printf("PROGRESS phase=%s %.2f%% pos=%d total=%d speed=%.1f MiB/s bad_candidate_bytes=%d\n",
				phase, percent, position, total, speed/(1024*1024), progress.badBytes.Load())
			lastRead = read
			lastTime = now
		}
	}
}

func logLine(file *os.File, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	fmt.Fprintf(file, "%s %s\n", time.Now().Format(time.RFC3339Nano), message)
	_ = file.Sync()
}

func sumRanges(ranges []byteRange) int64 {
	var total int64
	for _, r := range ranges {
		total += r.Size
	}
	return total
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, "ERROR:", message)
	os.Exit(1)
}
