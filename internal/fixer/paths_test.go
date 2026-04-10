package fixer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateProcessPathsRejectsNestedOutput(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "Google Photos")
	output := filepath.Join(input, "Fixed")
	if err := os.MkdirAll(input, 0o755); err != nil {
		t.Fatal(err)
	}

	err := ValidateProcessPaths(input, output)
	if err == nil {
		t.Fatal("expected nested output validation to fail")
	}
}

func TestOpenStateStoreReportsCorruptLines(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.jsonl")
	if err := os.WriteFile(statePath, []byte("{bad json}\n{\"sourceRelPath\":\"ok\",\"status\":\"copied\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := OpenStateStore(statePath)
	if err != nil {
		t.Fatalf("OpenStateStore returned error: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("Close returned error: %v", closeErr)
		}
	})

	if len(store.Warnings) == 0 {
		t.Fatal("expected corrupt state warnings to be recorded")
	}
	if _, ok := store.Get("ok"); !ok {
		t.Fatal("expected valid state record to be loaded")
	}
}
