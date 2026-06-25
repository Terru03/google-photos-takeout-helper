package batch

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
)

func TestSelectOutputDrivePrefersBackupBLabelThenBLetter(t *testing.T) {
	drives := []DriveInfo{
		{Root: `B:\`, Letter: "B:", Label: "Backup", FreeBytes: 4},
		{Root: `D:\`, Letter: "D:", Label: "Backup B", FreeBytes: 2},
	}
	drive, ok, err := SelectOutputDrive(drives)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || drive.Letter != "D:" {
		t.Fatalf("expected Backup B label to win, got %#v ok=%v", drive, ok)
	}

	drive, ok, err = SelectOutputDrive([]DriveInfo{{Root: `B:\`, Letter: "B:", Label: "Backup"}})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || drive.Letter != "B:" {
		t.Fatalf("expected B: to be selected, got %#v ok=%v", drive, ok)
	}
}

func TestSelectWorkDrivePrefersSSDWithEnoughSpace(t *testing.T) {
	drives := []DriveInfo{
		{Root: `D:\`, Letter: "D:", Kind: DriveKindHDD, FreeBytes: 10},
		{Root: `C:\`, Letter: "C:", Kind: DriveKindSSD, FreeBytes: 100},
		{Root: `E:\`, Letter: "E:", Kind: DriveKindSSD, FreeBytes: 5},
	}
	drive, ok := SelectWorkDrive(drives, 50, `B:\Google_Photos_Final`)
	if !ok || drive.Letter != "C:" {
		t.Fatalf("expected C: SSD, got %#v ok=%v", drive, ok)
	}
}

func TestManifestResumeLogic(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gtf", "batch", "manifest.jsonl")
	item := ZipItem{
		Name:      "takeout-001.zip",
		Path:      filepath.Join(root, "takeout-001.zip"),
		SizeBytes: 10,
		ModTime:   time.Unix(100, 0).UTC(),
	}
	item.Fingerprint = zipFingerprint(item)

	manifest, err := OpenManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Append(manifestEntryFor(item, t.TempDir(), statusCompleted)); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = reopened.Close()
	}()
	if !reopened.AlreadySuccessful(item) {
		t.Fatal("expected manifest to remember successful ZIP")
	}
}

func TestFindTakeoutZipsIgnoresIncompleteDownloads(t *testing.T) {
	root := t.TempDir()
	writeTestZip(t, filepath.Join(root, "takeout-001.zip"), map[string]string{
		"Google Photos/IMG_0001.jpg": "image",
	})
	if err := os.WriteFile(filepath.Join(root, "takeout-002.zip.crdownload"), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "takeout-003.part"), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	zips, err := FindTakeoutZips([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(zips) != 1 {
		t.Fatalf("expected one complete ZIP, got %d", len(zips))
	}
	if filepath.Base(zips[0].Path) != "takeout-001.zip" {
		t.Fatalf("unexpected ZIP found: %s", zips[0].Path)
	}
}

func TestLocateGooglePhotosFolderAfterExtract(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "takeout-001.zip")
	writeTestZip(t, zipPath, map[string]string{
		"Takeout/Google Photos/Photos from 2024/IMG_0001.jpg": "image",
	})

	extractRoot := filepath.Join(root, "extract")
	if err := ExtractZip(zipPath, extractRoot); err != nil {
		t.Fatal(err)
	}

	got, err := LocateGooglePhotosFolder(extractRoot)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(extractRoot, "Takeout", "Google Photos")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestValidateBatchPathsRejectsOverlap(t *testing.T) {
	root := t.TempDir()
	zipRoot := filepath.Join(root, "zips")
	output := filepath.Join(zipRoot, "final")
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(zipRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ValidateBatchPaths([]string{zipRoot}, work, output, false); err == nil {
		t.Fatal("expected ZIP/output overlap to fail")
	}

	output = filepath.Join(root, "final")
	work = filepath.Join(output, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBatchPaths([]string{zipRoot}, work, output, false); err == nil {
		t.Fatal("expected work/output overlap to fail")
	}
}

func TestRunNeverDeletesZipFile(t *testing.T) {
	root := t.TempDir()
	zipRoot := filepath.Join(root, "zips")
	work := filepath.Join(root, "work")
	output := filepath.Join(root, "output")
	if err := os.MkdirAll(zipRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(zipRoot, "takeout-001.zip")
	writeTestZip(t, zipPath, map[string]string{
		"Takeout/Google Photos/Photos from 2024/IMG_0001.jpg": "image",
	})

	called := 0
	_, err := Run(context.Background(), Options{
		ZipRoots:          []string{zipRoot},
		WorkDir:           work,
		OutputDir:         output,
		SafetyMarginBytes: 1,
		ProcessOptions: fixer.ProcessOptions{
			WriteMetadata:       false,
			VerifyWrites:        false,
			RestoreMOVExtension: false,
			Deduplicate:         true,
		},
		Process: func(_ context.Context, sourcePath string, outputPath string, progressCh chan<- fixer.Progress, _ fixer.ProcessOptions) error {
			defer close(progressCh)
			called++
			if filepath.Base(sourcePath) != "Google Photos" {
				t.Fatalf("expected Google Photos input, got %s", sourcePath)
			}
			writeCleanReport(t, outputPath)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("expected process to run once, got %d", called)
	}
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("ZIP file should still exist: %v", err)
	}
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected temp work folder to be cleaned, found %d entries", len(entries))
	}
}

func TestRunSkipsSuccessfulManifestEntry(t *testing.T) {
	root := t.TempDir()
	zipRoot := filepath.Join(root, "zips")
	work := filepath.Join(root, "work")
	output := filepath.Join(root, "output")
	if err := os.MkdirAll(zipRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(zipRoot, "takeout-001.zip")
	writeTestZip(t, zipPath, map[string]string{
		"Takeout/Google Photos/Photos from 2024/IMG_0001.jpg": "image",
	})
	info, err := os.Stat(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	item, err := newZipItem(zipPath, info)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := OpenManifest(manifestPath(output))
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Append(manifestEntryFor(item, output, statusCompleted)); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Close(); err != nil {
		t.Fatal(err)
	}

	called := 0
	result, err := Run(context.Background(), Options{
		ZipRoots:          []string{zipRoot},
		WorkDir:           work,
		OutputDir:         output,
		SafetyMarginBytes: 1,
		ProcessOptions: fixer.ProcessOptions{
			WriteMetadata:       false,
			VerifyWrites:        false,
			RestoreMOVExtension: false,
		},
		Process: func(_ context.Context, _ string, _ string, progressCh chan<- fixer.Progress, _ fixer.ProcessOptions) error {
			defer close(progressCh)
			called++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatalf("expected process skip, got %d calls", called)
	}
	if result.Skipped != 1 {
		t.Fatalf("expected one skipped ZIP, got %d", result.Skipped)
	}
}

func TestPreflightWarnsOnOverlapAndExistingState(t *testing.T) {
	root := t.TempDir()
	zipRoot := filepath.Join(root, "zips")
	output := filepath.Join(zipRoot, "output")
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(zipRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestZip(t, filepath.Join(zipRoot, "archive.zip"), map[string]string{
		"Takeout/Google Photos/Photos from 2024/IMG_0001.jpg": "image",
	})
	if err := os.MkdirAll(filepath.Join(output, ".gtf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, ".gtf", "state.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Preflight(Options{
		ZipRoots:          []string{zipRoot},
		WorkDir:           work,
		OutputDir:         output,
		SafetyMarginBytes: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ZipCount != 1 {
		t.Fatalf("expected one ZIP, got %d", report.ZipCount)
	}
	joined := strings.Join(report.Warnings, "\n")
	if !strings.Contains(joined, "ZIP source and output overlap") {
		t.Fatalf("expected overlap warning, got %q", joined)
	}
	if !strings.Contains(joined, "existing fixer state") {
		t.Fatalf("expected state warning, got %q", joined)
	}
}

func TestSkipWindowsSystemScanDirs(t *testing.T) {
	if !shouldSkipScanDir("System Volume Information") {
		t.Fatal("expected System Volume Information to be skipped")
	}
	if !shouldSkipScanPath(filepath.Join(`D:\`, "$RECYCLE.BIN", "S-1-5-18")) {
		t.Fatal("expected recycle bin subtree to be skipped")
	}
	if shouldSkipScanDir("Takeout_Zips") {
		t.Fatal("expected normal ZIP folder to be scanned")
	}
}

func writeTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, body := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeCleanReport(t *testing.T, outputPath string) {
	t.Helper()
	reportDir := filepath.Join(outputPath, ".gtf", "reports")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	report := fixer.RunReport{
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
		OutputRoot: outputPath,
		Summary: fixer.RunReportSummary{
			TotalMedia: 1,
			Matched:    1,
		},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "latest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "latest.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
}
