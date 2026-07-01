package batch

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
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

	reviewEntry := manifestEntryFor(item, t.TempDir(), statusCompletedReview)
	reviewEntry.ZipFingerprint = item.Fingerprint
	if err := reopened.Append(reviewEntry); err != nil {
		t.Fatal(err)
	}
	if !reopened.AlreadySuccessful(item) {
		t.Fatal("expected completed_with_review ZIP to be successful for resume")
	}
}

func TestFindTakeoutZipsIgnoresIncompleteDownloads(t *testing.T) {
	root := t.TempDir()
	writeTestZip(t, filepath.Join(root, "takeout-001.zip"), map[string]string{
		"Google Photos/IMG_0001.jpg": "image",
	})
	if err := os.WriteFile(filepath.Join(root, "broken-book.zip"), []byte("not a real zip"), 0o644); err != nil {
		t.Fatal(err)
	}
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

func TestExtractZipSanitizesWindowsUnsafePathComponents(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "takeout-unsafe-components.zip")
	writeTestZip(t, zipPath, map[string]string{
		`Takeout/Google Photos/Adolf Munkel Weg - Day 4 FSD /DJI_0341.MP4`: "video",
		`Takeout/Google Photos/Album with trailing dot./IMG_0001.JPG`:      "image",
		`Takeout/Google Photos/AUX/CON?.JPG`:                               "image",
	})

	extractRoot := filepath.Join(root, "extract")
	if err := ExtractZip(zipPath, extractRoot); err != nil {
		t.Fatal(err)
	}

	expected := []string{
		filepath.Join(extractRoot, "Takeout", "Google Photos", "Adolf Munkel Weg - Day 4 FSD", "DJI_0341.MP4"),
		filepath.Join(extractRoot, "Takeout", "Google Photos", "Album with trailing dot", "IMG_0001.JPG"),
		filepath.Join(extractRoot, "Takeout", "Google Photos", "_AUX", "CON_.JPG"),
	}
	for _, path := range expected {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected sanitized file %s: %v", path, err)
		}
	}
}

func TestSafeZipEntryNameRejectsUnsafePaths(t *testing.T) {
	unsafeNames := []string{
		`../escape.jpg`,
		`Takeout/../escape.jpg`,
		`/Takeout/Google Photos/IMG.jpg`,
		`C:/Takeout/Google Photos/IMG.jpg`,
	}
	for _, name := range unsafeNames {
		if _, err := safeZipEntryName(name); err == nil {
			t.Fatalf("expected %q to be rejected", name)
		}
	}
}

func TestSafeZipEntryNameSanitizesWindowsComponents(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{
			name: `Takeout/Google Photos/Folder with space /IMG.jpg`,
			want: filepath.Join("Takeout", "Google Photos", "Folder with space", "IMG.jpg"),
		},
		{
			name: `Takeout/Google Photos/Folder with dot./IMG.jpg`,
			want: filepath.Join("Takeout", "Google Photos", "Folder with dot", "IMG.jpg"),
		},
		{
			name: `Takeout/Google Photos/bad<name>|album?/IMG*.jpg`,
			want: filepath.Join("Takeout", "Google Photos", "bad_name__album_", "IMG_.jpg"),
		},
		{
			name: `Takeout/Google Photos/CON/COM1.txt`,
			want: filepath.Join("Takeout", "Google Photos", "_CON", "_COM1.txt"),
		},
		{
			name: `Takeout/Google Photos/Photos from 2024/IMG_0001.jpg`,
			want: filepath.Join("Takeout", "Google Photos", "Photos from 2024", "IMG_0001.jpg"),
		},
	}
	for _, tt := range tests {
		got, err := safeZipEntryName(tt.name)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if got != tt.want {
			t.Fatalf("%s: got %q want %q", tt.name, got, tt.want)
		}
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

func TestValidateBatchPathSetRejectsOverlappingWorkFolders(t *testing.T) {
	root := t.TempDir()
	zipRoot := filepath.Join(root, "zips")
	output := filepath.Join(root, "output")
	work := filepath.Join(root, "work")
	nestedWork := filepath.Join(work, "nested")
	if err := os.MkdirAll(zipRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ValidateBatchPathSet([]string{zipRoot}, []string{work, nestedWork}, output, false); err == nil {
		t.Fatal("expected overlapping work folders to fail")
	}
}

func TestSelectBestWorkRootPrefersSSDThenFreeSpace(t *testing.T) {
	statuses := []workRootStatus{
		{Path: `D:\Takeout_Incoming`, Kind: DriveKindHDD, FreeBytes: 900, RequiredBytes: 100, Usable: true},
		{Path: `C:\Takeout_Incoming`, Kind: DriveKindSSD, FreeBytes: 200, RequiredBytes: 100, Usable: true},
		{Path: `E:\Takeout_Incoming`, Kind: DriveKindSSD, FreeBytes: 300, RequiredBytes: 100, Usable: true},
	}

	got, ok := selectBestWorkRoot(statuses)
	if !ok {
		t.Fatal("expected a work root")
	}
	if got.Path != `E:\Takeout_Incoming` {
		t.Fatalf("got %s", got.Path)
	}
}

func TestSelectBestWorkRootFailsWhenNoRootHasSpace(t *testing.T) {
	statuses := []workRootStatus{
		{Path: `C:\Takeout_Incoming`, Kind: DriveKindSSD, FreeBytes: 99, RequiredBytes: 100, Usable: false},
		{Path: `E:\Takeout_Incoming`, Kind: DriveKindHDD, FreeBytes: 50, RequiredBytes: 100, Usable: false},
	}

	if _, ok := selectBestWorkRoot(statuses); ok {
		t.Fatal("expected no usable work root")
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

func TestResolveWorkDirsDoesNotDuplicateLegacyWorkDir(t *testing.T) {
	root := t.TempDir()
	workA := filepath.Join(root, "work-a")
	workB := filepath.Join(root, "work-b")

	got, err := resolveWorkDirs(Options{
		WorkDir:  workA,
		WorkDirs: []string{workA, workB},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{workA, workB}
	for i := range want {
		want[i], err = filepath.Abs(want[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestRunCompletesWithReviewCleansTempAndSkipsRerun(t *testing.T) {
	root := t.TempDir()
	zipRoot := filepath.Join(root, "zips")
	workA := filepath.Join(root, "work-a")
	workB := filepath.Join(root, "work-b")
	output := filepath.Join(root, "output")
	if err := os.MkdirAll(zipRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workB, 0o755); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(zipRoot, "takeout-001.zip")
	writeTestZip(t, zipPath, map[string]string{
		"Takeout/Google Photos/Photos from 2024/IMG_0001.jpg": "image",
	})

	called := 0
	options := Options{
		ZipRoots:          []string{zipRoot},
		WorkDirs:          []string{workA, workB},
		OutputDir:         output,
		SafetyMarginBytes: 1,
		ProcessOptions: fixer.ProcessOptions{
			WriteMetadata:       false,
			VerifyWrites:        false,
			RestoreMOVExtension: false,
			Deduplicate:         true,
		},
		Process: func(_ context.Context, _ string, outputPath string, progressCh chan<- fixer.Progress, _ fixer.ProcessOptions) error {
			defer close(progressCh)
			called++
			writeReviewReport(t, outputPath)
			return nil
		},
	}

	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.CompletedWithReview != 1 || result.Processed != 0 || result.Failed != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	assertNoTempDirs(t, workA)
	assertNoTempDirs(t, workB)

	entry := readLatestManifestEntry(t, manifestPath(output))
	if entry.Status != statusCompletedReview {
		t.Fatalf("got status %q", entry.Status)
	}
	if entry.WorkRoot == "" {
		t.Fatal("expected manifest to record work root")
	}
	if entry.ExtractedRoot == "" {
		t.Fatal("expected manifest to record temp extraction root")
	}

	result, err = Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("expected rerun to skip completed_with_review ZIP, got %d process calls", called)
	}
	if result.Skipped != 1 {
		t.Fatalf("expected one skipped ZIP, got %d", result.Skipped)
	}
}

func TestRunFailsBadZipExtractionAndCleansTemp(t *testing.T) {
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

	writeTestZip(t, filepath.Join(zipRoot, "takeout-001.zip"), map[string]string{
		"../escape.jpg": "image",
	})

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
			t.Fatal("process should not run after extraction failure")
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected extraction failure")
	}
	if result.Failed != 1 {
		t.Fatalf("expected one failed ZIP, got %#v", result)
	}
	assertNoTempDirs(t, work)
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
	writeTestZip(t, filepath.Join(zipRoot, "takeout-archive.zip"), map[string]string{
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
	writeReport(t, outputPath, fixer.RunReportSummary{
		TotalMedia: 1,
		Matched:    1,
	})
}

func writeReviewReport(t *testing.T, outputPath string) {
	t.Helper()
	writeReport(t, outputPath, fixer.RunReportSummary{
		TotalMedia:                   10,
		Matched:                      4,
		Unmatched:                    3,
		Ambiguous:                    2,
		SuspiciousDates:              1,
		ConflictsFound:               1,
		Errors:                       1,
		MetadataVerificationFailures: 1,
	})
}

func writeReport(t *testing.T, outputPath string, summary fixer.RunReportSummary) {
	t.Helper()
	reportDir := filepath.Join(outputPath, ".gtf", "reports")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	report := fixer.RunReport{
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
		OutputRoot: outputPath,
		Summary:    summary,
	}
	data, err := json.Marshal(&report)
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

func readLatestManifestEntry(t *testing.T, path string) ManifestEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		t.Fatal("manifest is empty")
	}
	var entry ManifestEntry
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

func assertNoTempDirs(t *testing.T, workDir string) {
	t.Helper()
	entries, err := os.ReadDir(workDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "gtf-zip-") {
			t.Fatalf("expected temp work folder to be cleaned, found %s", filepath.Join(workDir, entry.Name()))
		}
	}
}
