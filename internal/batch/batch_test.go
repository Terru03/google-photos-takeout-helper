package batch

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
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

func TestManifestDoesNotSilentlySkipOlderWorkflow(t *testing.T) {
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
	defer func() {
		_ = manifest.Close()
	}()

	entry := manifestEntryFor(item, root, statusCompletedReview)
	entry.WorkflowVersion = 1
	if err := manifest.Append(entry); err != nil {
		t.Fatal(err)
	}
	if manifest.AlreadySuccessful(item) {
		t.Fatal("older workflow must not count as safe resume")
	}
	if got := manifest.LegacySuccessfulCount([]ZipItem{item}); got != 1 {
		t.Fatalf("legacy successful count = %d, want 1", got)
	}
}

func TestPreflightWarnsWhenMotionPhotoToolIsMissing(t *testing.T) {
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
		"Takeout/Google Photos/Photos from 2024/IMG_0001.jpg": "image",
	})

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})
	t.Setenv("PATH", "")

	report, err := Preflight(Options{
		ZipRoots:          []string{zipRoot},
		WorkDir:           work,
		OutputDir:         output,
		SafetyMarginBytes: 1,
		ProcessOptions: fixer.ProcessOptions{
			CreateMotionPhotos: true,
		},
	})
	if err != nil {
		t.Fatalf("phase 1 preflight must not fail without MotionPhoto2: %v", err)
	}
	if report.MotionPhotoToolFound {
		t.Fatal("missing MotionPhoto2 reported as found")
	}
	motionWarningFound := false
	for _, warning := range report.Warnings {
		if strings.Contains(warning, "MotionPhoto2") {
			motionWarningFound = true
			break
		}
	}
	if !motionWarningFound {
		t.Fatalf("expected MotionPhoto2 warning, got %#v", report.Warnings)
	}
	assertNoTempDirs(t, work)
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

func TestFindTakeoutZipsAcceptsOneZipPath(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "takeout-one.zip")
	writeTestZip(t, zipPath, map[string]string{
		"Takeout/Google Photos/Photos from 2024/IMG_0001.jpg": "image",
	})
	zips, err := FindTakeoutZips([]string{zipPath})
	if err != nil {
		t.Fatal(err)
	}
	if len(zips) != 1 || zips[0].Path != zipPath {
		t.Fatalf("one-ZIP path not used: %#v", zips)
	}
}

func TestShouldSkipZipMatchesNameOrFullPath(t *testing.T) {
	item := ZipItem{
		Name: "takeout-005.zip",
		Path: filepath.Join(`D:\Takeout_Zips`, "takeout-005.zip"),
	}
	if !shouldSkipZip(item, []string{"TAKEOUT-005.ZIP"}) {
		t.Fatal("ZIP name did not match skip list")
	}
	if !shouldSkipZip(item, []string{item.Path}) {
		t.Fatal("ZIP path did not match skip list")
	}
	if shouldSkipZip(item, []string{"takeout-006.zip"}) {
		t.Fatal("wrong ZIP matched skip list")
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

func TestRunReportsStableCurrentZipIndexAfterFailures(t *testing.T) {
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
	for _, name := range []string{"takeout-001.zip", "takeout-002.zip", "takeout-003.zip"} {
		writeTestZip(t, filepath.Join(zipRoot, name), map[string]string{
			"Takeout/Google Photos/Photos from 2024/IMG_0001.jpg": "image",
		})
	}

	var progressEvents []BatchProgress
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
		},
		Process: func(_ context.Context, _ string, outputPath string, progressCh chan<- fixer.Progress, _ fixer.ProcessOptions) error {
			defer close(progressCh)
			called++
			if called <= 2 {
				return fmt.Errorf("forced fail %d", called)
			}
			writeCleanReport(t, outputPath)
			return nil
		},
		Progress: func(progress BatchProgress) {
			if progress.CurrentZip != "" {
				progressEvents = append(progressEvents, progress)
			}
		},
	})
	if err == nil {
		t.Fatal("expected batch error after forced failures")
	}
	if !strings.Contains(err.Error(), "2 ZIP file(s) failed") {
		t.Fatalf("unexpected batch error: %v", err)
	}

	seenThird := false
	for _, event := range progressEvents {
		if filepath.Base(event.CurrentZip) != "takeout-003.zip" {
			continue
		}
		seenThird = true
		if event.CurrentIndex != 3 {
			t.Fatalf("third ZIP index = %d, want 3 in event %+v", event.CurrentIndex, event)
		}
		if event.Total != 3 {
			t.Fatalf("third ZIP total = %d, want 3 in event %+v", event.Total, event)
		}
	}
	if !seenThird {
		t.Fatalf("did not see progress for third ZIP in %+v", progressEvents)
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

func TestWorkRootStatusesUseExistingParentForNewFolder(t *testing.T) {
	root := t.TempDir()
	newWorkRoot := filepath.Join(root, "new", "work")
	statuses := workRootStatuses([]string{newWorkRoot}, nil, 1)
	if len(statuses) != 1 {
		t.Fatalf("got %d statuses", len(statuses))
	}
	if statuses[0].Err != nil {
		t.Fatalf("new work root probe failed: %v", statuses[0].Err)
	}
	if !statuses[0].Usable || statuses[0].FreeBytes <= 0 {
		t.Fatalf("new work root should use parent free space: %#v", statuses[0])
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

func TestRunFinishesPhaseOneBeforeMissingMotionToolError(t *testing.T) {
	root := t.TempDir()
	zipRoot := filepath.Join(root, "zips")
	work := filepath.Join(root, "work")
	output := filepath.Join(root, "output")
	for _, dir := range []string{zipRoot, work} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestZip(t, filepath.Join(zipRoot, "takeout-001.zip"), map[string]string{
		"Takeout/Google Photos/Photos from 2024/IMG_0001.jpg": "image",
	})

	called := 0
	result, err := Run(context.Background(), Options{
		ZipRoots:          []string{zipRoot},
		WorkDir:           work,
		OutputDir:         output,
		MotionToolPath:    filepath.Join(root, "missing-motionphoto2.exe"),
		SafetyMarginBytes: 1,
		ProcessOptions: fixer.ProcessOptions{
			CreateMotionPhotos:  true,
			WriteMetadata:       false,
			VerifyWrites:        false,
			RestoreMOVExtension: false,
		},
		Process: func(_ context.Context, _ string, outputPath string, progressCh chan<- fixer.Progress, processOptions fixer.ProcessOptions) error {
			defer close(progressCh)
			called++
			if processOptions.CreateMotionPhotos {
				t.Fatal("phase 1 received motion merge option")
			}
			writeCleanReport(t, outputPath)
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "MotionPhoto2") {
		t.Fatalf("expected missing motion tool after phase 1, got %v", err)
	}
	if called != 1 || result.Processed != 1 || result.Failed != 0 {
		t.Fatalf("phase 1 did not finish cleanly: calls=%d result=%#v", called, result)
	}
	assertNoTempDirs(t, work)
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

func TestRunStagesCommitsCleansAndSkipsRerun(t *testing.T) {
	root := t.TempDir()
	zipRoot := filepath.Join(root, "zips")
	work := filepath.Join(root, "work")
	staging := filepath.Join(root, "staging")
	output := filepath.Join(root, "output")
	for _, dir := range []string{zipRoot, work, staging} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestZip(t, filepath.Join(zipRoot, "takeout-001.zip"), map[string]string{
		"Takeout/Google Photos/Photos from 2024/IMG_0001.jpg": "image",
	})

	called := 0
	options := Options{
		ZipRoots:          []string{zipRoot},
		WorkDir:           work,
		StagingOutputDir:  staging,
		OutputDir:         output,
		SafetyMarginBytes: 1,
		ProcessOptions: fixer.ProcessOptions{
			WriteMetadata:       false,
			VerifyWrites:        false,
			RestoreMOVExtension: false,
		},
		Process: func(_ context.Context, _ string, stagingRoot string, progressCh chan<- fixer.Progress, processOptions fixer.ProcessOptions) error {
			defer close(progressCh)
			called++
			stagedPath := filepath.Join(stagingRoot, "Photos from 2024", "IMG_0001.jpg")
			finalPath := filepath.Join(processOptions.FinalOutputRoot, "Photos from 2024", "IMG_0001.jpg")
			if err := os.MkdirAll(filepath.Dir(stagedPath), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(stagedPath, []byte("fixed"), 0o644); err != nil {
				return err
			}
			writeStagingReport(t, processOptions.RuntimeRoot, fixer.ProcessRecord{
				SourceID:      processOptions.SourceID,
				SourceRelPath: "Photos from 2024/IMG_0001.jpg",
				OutputPath:    finalPath,
				StagedPath:    stagedPath,
				Status:        fixer.OperationCopied,
			})
			return nil
		},
	}

	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Processed != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	finalPath := filepath.Join(output, "Photos from 2024", "IMG_0001.jpg")
	if got, err := os.ReadFile(finalPath); err != nil || string(got) != "fixed" {
		t.Fatalf("staged file not committed: %q %v", string(got), err)
	}
	assertNoTempDirs(t, work)
	assertNoStageDirs(t, staging)

	result, err = Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if called != 1 || result.Skipped != 1 {
		t.Fatalf("completed staging run did not resume-skip: calls=%d result=%#v", called, result)
	}
}

func TestRunKeepsStagingFolderWhenCommitFails(t *testing.T) {
	root := t.TempDir()
	zipRoot := filepath.Join(root, "zips")
	work := filepath.Join(root, "work")
	staging := filepath.Join(root, "staging")
	output := filepath.Join(root, "output")
	for _, dir := range []string{zipRoot, work, staging} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestZip(t, filepath.Join(zipRoot, "takeout-001.zip"), map[string]string{
		"Takeout/Google Photos/Photos from 2024/IMG_0001.jpg": "image",
	})

	result, err := Run(context.Background(), Options{
		ZipRoots:          []string{zipRoot},
		WorkDir:           work,
		StagingOutputDir:  staging,
		OutputDir:         output,
		SafetyMarginBytes: 1,
		ProcessOptions: fixer.ProcessOptions{
			WriteMetadata:       false,
			VerifyWrites:        false,
			RestoreMOVExtension: false,
		},
		Process: func(_ context.Context, _ string, stagingRoot string, progressCh chan<- fixer.Progress, processOptions fixer.ProcessOptions) error {
			defer close(progressCh)
			stagedPath := filepath.Join(stagingRoot, "IMG_0001.jpg")
			if err := os.WriteFile(stagedPath, []byte("fixed"), 0o644); err != nil {
				return err
			}
			writeStagingReport(t, processOptions.RuntimeRoot, fixer.ProcessRecord{
				SourceRelPath: "IMG_0001.jpg",
				OutputPath:    filepath.Join(root, "outside", "IMG_0001.jpg"),
				StagedPath:    stagedPath,
				Status:        fixer.OperationCopied,
			})
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected staging commit failure")
	}
	if result.Failed != 1 {
		t.Fatalf("expected one failed ZIP: %#v", result)
	}
	stageDirs := stageDirs(t, staging)
	if len(stageDirs) != 1 {
		t.Fatalf("failed commit should keep one staging folder, got %v", stageDirs)
	}
	if !fixer.FileExists(filepath.Join(stageDirs[0], "IMG_0001.jpg")) {
		t.Fatal("failed commit lost staged file")
	}
	assertNoTempDirs(t, work)
	entry := readLatestManifestEntry(t, manifestPath(output))
	if entry.Status != statusFailed || entry.StagingRoot == "" {
		t.Fatalf("failed staging manifest wrong: %#v", entry)
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

func TestRunBlocksOlderWorkflowManifestWithoutReprocess(t *testing.T) {
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
	entry := manifestEntryFor(item, output, statusCompleted)
	entry.WorkflowVersion = 1
	if err := manifest.Append(entry); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Close(); err != nil {
		t.Fatal(err)
	}

	called := 0
	_, err = Run(context.Background(), Options{
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
	if err == nil || !strings.Contains(err.Error(), "older workflow") {
		t.Fatalf("expected older workflow guard, got %v", err)
	}
	if called != 0 {
		t.Fatalf("process ran %d times", called)
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

func writeStagingReport(t *testing.T, runtimeRoot string, record fixer.ProcessRecord) {
	t.Helper()
	reportDir := filepath.Join(runtimeRoot, ".gtf", "reports")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	report := fixer.RunReport{
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
		OutputRoot: runtimeRoot,
		Summary: fixer.RunReportSummary{
			TotalMedia:  1,
			Matched:     1,
			OutputMedia: 1,
		},
		Records: []fixer.ProcessRecord{record},
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

func assertNoStageDirs(t *testing.T, stagingRoot string) {
	t.Helper()
	if dirs := stageDirs(t, stagingRoot); len(dirs) != 0 {
		t.Fatalf("expected staging cleanup, found %v", dirs)
	}
}

func stageDirs(t *testing.T, stagingRoot string) []string {
	t.Helper()
	entries, err := os.ReadDir(stagingRoot)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "gtf-stage-") {
			paths = append(paths, filepath.Join(stagingRoot, entry.Name()))
		}
	}
	return paths
}
