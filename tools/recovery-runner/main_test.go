package main

import (
	"os"
	"path/filepath"
	"testing"
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
