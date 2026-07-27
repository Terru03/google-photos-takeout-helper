package batch

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
)

func TestBuildGlobalSidecarIndexMatchesMediaFromDifferentZip(t *testing.T) {
	root := t.TempDir()
	zipRoot := filepath.Join(root, "zips")
	if err := os.MkdirAll(zipRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestZip(t, filepath.Join(zipRoot, "takeout-001.zip"), map[string]string{
		"Takeout/Google Photos/Photos from 2006/PIC_0008.JPG": "image",
	})
	writeTestZip(t, filepath.Join(zipRoot, "takeout-002.zip"), map[string]string{
		"Takeout/Google Photos/Photos from 2006/PIC_0008.JPG.supplemental-metadata.json": `{"title":"PIC_0008.JPG","photoTakenTime":{"timestamp":"1165449600"}}`,
	})

	zips, err := FindTakeoutZips([]string{zipRoot})
	if err != nil {
		t.Fatal(err)
	}
	index, warnings, err := buildGlobalSidecarIndex(context.Background(), zips, nil)
	if err != nil {
		t.Fatal(err)
	}
	if warnings != 0 {
		t.Fatalf("warnings = %d, want 0", warnings)
	}
	if index.Count() != 1 {
		t.Fatalf("sidecars = %d, want 1", index.Count())
	}

	sourceRoot := filepath.Join(root, "Google Photos")
	yearDir := filepath.Join(sourceRoot, "Photos from 2006")
	if err := os.MkdirAll(yearDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(yearDir, "PIC_0008.JPG"), []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}

	plans, err := fixer.DiscoverMediaPlan(sourceRoot, fixer.ProcessOptions{SidecarIndex: index})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].MatchStatus != fixer.MatchStatusMatched {
		t.Fatalf("cross-ZIP match failed: %#v", plans)
	}
}
