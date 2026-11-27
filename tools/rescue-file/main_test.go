package main

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Terru03/google-photos-takeout-helper/tools/internal/blocksize"
)

type countingSource struct {
	readCalls int
}

func (source *countingSource) ReadAt(buffer []byte, _ int64) (int, error) {
	source.readCalls++
	return len(buffer), nil
}

func (source *countingSource) Close() error {
	return nil
}

func TestRunCoarseAmortizesSlowReadLatency(t *testing.T) {
	const sourceSize = int64(8 * 1024 * 1024)

	tempDir := t.TempDir()
	target, err := os.Create(filepath.Join(tempDir, "recovered.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if err := target.Truncate(sourceSize); err != nil {
		t.Fatal(err)
	}

	logFile, err := os.Create(filepath.Join(tempDir, "rescue.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()

	defaultBlocks, err := parseBlockSizes(blocksize.Default, "")
	if err != nil {
		t.Fatal(err)
	}
	source := &countingSource{}
	state := &rescueState{
		Version:    1,
		SourceSize: sourceSize,
		Phase:      "coarse",
		BlockSize:  defaultBlocks.coarse,
	}
	var progress progressState
	var stopAsked atomic.Bool

	err = runCoarse(
		source,
		target,
		filepath.Join(tempDir, "state.json"),
		logFile,
		state,
		&progress,
		&stopAsked,
		defaultBlocks.coarse,
	)
	if err != nil {
		t.Fatal(err)
	}

	if source.readCalls != 1 {
		t.Fatalf("coarse rescue made %d reads for 8 MiB; want 1 large read", source.readCalls)
	}
}

func TestParseBlockSizesReverseDefaultsToCoarse(t *testing.T) {
	blocks, err := parseBlockSizes("64K", "")
	if err != nil {
		t.Fatal(err)
	}
	if blocks.coarse != 64*1024 || blocks.reverse != 64*1024 {
		t.Fatalf("unexpected blocks: %#v", blocks)
	}
	blocks, err = parseBlockSizes("64M", "4M")
	if err != nil {
		t.Fatal(err)
	}
	if blocks.coarse != 64*1024*1024 || blocks.reverse != 4*1024*1024 {
		t.Fatalf("unexpected blocks: %#v", blocks)
	}
	if _, err := parseBlockSizes("64M", "1K"); err == nil {
		t.Fatal("invalid reverse block was accepted")
	}
}

// failingSource fails every read that touches [badStart, badStart+badSize).
type failingSource struct {
	badStart  int64
	badSize   int64
	readCalls int
}

func (source *failingSource) ReadAt(buffer []byte, offset int64) (int, error) {
	source.readCalls++
	end := offset + int64(len(buffer))
	if offset < source.badStart+source.badSize && end > source.badStart {
		return 0, errors.New("simulated bad sector")
	}
	for index := range buffer {
		buffer[index] = 0xAB
	}
	return len(buffer), nil
}

func (source *failingSource) Close() error {
	return nil
}

func runCoarseForTest(t *testing.T, source sourceReader, sourceSize int64, block int64) (*rescueState, *os.File) {
	t.Helper()
	tempDir := t.TempDir()
	target, err := os.Create(filepath.Join(tempDir, "recovered.zip"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.Close() })
	if err := target.Truncate(sourceSize); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(filepath.Join(tempDir, "rescue.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logFile.Close() })
	state := &rescueState{Version: 1, SourceSize: sourceSize, Phase: "coarse", BlockSize: block}
	var progress progressState
	var stopAsked atomic.Bool
	if err := runCoarse(source, target, filepath.Join(tempDir, "state.json"), logFile, state, &progress, &stopAsked, block); err != nil {
		t.Fatal(err)
	}
	return state, target
}

func TestRunCoarseSmallBlockLimitsBadRangeSize(t *testing.T) {
	const sourceSize = int64(8 * 1024 * 1024)
	const smallBlock = int64(64 * 1024)
	// One bad 512-byte sector in the middle of the source.
	source := &failingSource{badStart: 3*1024*1024 + 100, badSize: 512}

	state, _ := runCoarseForTest(t, source, sourceSize, smallBlock)

	if source.readCalls != int(sourceSize/smallBlock) {
		t.Fatalf("made %d reads; want %d", source.readCalls, sourceSize/smallBlock)
	}
	if len(state.Ranges) != 1 || state.Ranges[0].Size != smallBlock || state.Ranges[0].Start != 3*1024*1024 {
		t.Fatalf("bad range = %#v; want one 64 KiB range at 3 MiB", state.Ranges)
	}
}

func TestRunCoarseLargeBlockMarksWholeBlockForRefine(t *testing.T) {
	const sourceSize = int64(8 * 1024 * 1024)
	const largeBlock = int64(4 * 1024 * 1024)
	source := &failingSource{badStart: 3*1024*1024 + 100, badSize: 512}

	state, target := runCoarseForTest(t, source, sourceSize, largeBlock)

	if len(state.Ranges) != 1 || state.Ranges[0].Size != largeBlock || state.Ranges[0].Start != 0 {
		t.Fatalf("bad range = %#v; want the whole first 4 MiB block", state.Ranges)
	}
	// The good second block must be copied intact.
	buffer := make([]byte, 16)
	if _, err := target.ReadAt(buffer, 6*1024*1024); err != nil {
		t.Fatal(err)
	}
	for _, value := range buffer {
		if value != 0xAB {
			t.Fatalf("good block was not copied: %x", buffer)
		}
	}
	if state.Phase != "refine" || state.BlockSize != 64*1024 {
		t.Fatalf("coarse pass did not hand off to 64 KiB refine: phase=%s block=%d", state.Phase, state.BlockSize)
	}
}
