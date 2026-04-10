package fixer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDuplicateFileCopiesContents(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	dest := filepath.Join(root, "dest.txt")

	if err := os.WriteFile(source, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := DuplicateFile(source, dest); err != nil {
		t.Fatalf("DuplicateFile returned error: %v", err)
	}

	if got := readFileString(t, dest); got != "hello" {
		t.Fatalf("expected duplicated file content %q, got %q", "hello", got)
	}
}

func TestLinkDuplicateReturnsErrorForMissingSource(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "missing-dir", "dest.txt")

	status, err := LinkDuplicate(filepath.Join(root, "missing.txt"), dest, false)
	if err == nil {
		t.Fatal("expected LinkDuplicate to fail for missing source")
	}
	if status != OperationError {
		t.Fatalf("expected status %s, got %s", OperationError, status)
	}
}

func TestMakeUniquePathAddsSuffix(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "image.jpg")
	if err := os.WriteFile(path, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}

	unique := MakeUniquePath(path)
	if unique == path {
		t.Fatal("expected a unique path different from the original")
	}
	requireContains(t, unique, "image (2).jpg")
}
