package fixer

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStateStoreKeepsLatestRecordAndHashIndex(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state", "state.jsonl")

	store, err := OpenStateStore(statePath)
	if err != nil {
		t.Fatalf("OpenStateStore returned error: %v", err)
	}

	first := ProcessRecord{
		SourceRelPath: "Photos from 2020/IMG_1.jpg",
		OutputPath:    "out/IMG_1.jpg",
		SourceHash:    "hash-1",
		Status:        OperationCopied,
		UpdatedAt:     time.Now().UTC(),
	}
	second := first
	second.OutputPath = "out/IMG_1 (2).jpg"
	second.Status = OperationSkippedExisting
	second.UpdatedAt = second.UpdatedAt.Add(time.Second)

	if err := store.Put(first); err != nil {
		t.Fatalf("Put(first) returned error: %v", err)
	}
	if err := store.Put(second); err != nil {
		t.Fatalf("Put(second) returned error: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	reopened, err := OpenStateStore(statePath)
	if err != nil {
		t.Fatalf("OpenStateStore(reopen) returned error: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := reopened.Close(); closeErr != nil {
			t.Fatalf("Close returned error: %v", closeErr)
		}
	})

	record, ok := reopened.Get(first.SourceRelPath)
	if !ok {
		t.Fatal("expected record to exist after reopen")
	}
	if record.OutputPath != second.OutputPath {
		t.Fatalf("expected latest output path %q, got %q", second.OutputPath, record.OutputPath)
	}

	canonical, ok := reopened.CanonicalByHash("hash-1")
	if !ok {
		t.Fatal("expected canonical hash entry")
	}
	if canonical.SourceHash != "hash-1" {
		t.Fatalf("expected canonical hash hash-1, got %q", canonical.SourceHash)
	}
}
