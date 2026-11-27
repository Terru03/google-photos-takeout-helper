package main

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
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

	source := &countingSource{}
	state := &rescueState{
		Version:    1,
		SourceSize: sourceSize,
		Phase:      "coarse",
		BlockSize:  coarseBlock,
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
	)
	if err != nil {
		t.Fatal(err)
	}

	if source.readCalls != 1 {
		t.Fatalf("coarse rescue made %d reads for 8 MiB; want 1 large read", source.readCalls)
	}
}
