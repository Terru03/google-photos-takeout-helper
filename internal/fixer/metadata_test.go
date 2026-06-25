package fixer

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDetectExifToolReturnsVersion(t *testing.T) {
	toolPath := withFakeExifTool(t, nil)

	info, err := DetectExifTool()
	if err != nil {
		t.Fatalf("DetectExifTool returned error: %v", err)
	}
	if info.Path != toolPath {
		t.Fatalf("expected tool path %q, got %q", toolPath, info.Path)
	}
	if info.Version != "12.70" {
		t.Fatalf("expected version 12.70, got %q", info.Version)
	}
}

func TestApplyMetadataWritesPhotoArgs(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	withFakeExifTool(t, map[string]string{
		"FAKE_EXIFTOOL_ARGS_FILE": argsFile,
	})

	filePath := filepath.Join(t.TempDir(), "image.jpg")
	if err := os.WriteFile(filePath, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}

	meta := imageMetadata{
		Title:       "Sunset",
		Description: "At the beach",
		PhotoTakenTime: takeoutTimestamp{
			Timestamp: "1704067200",
		},
		GeoData: takeoutGeoData{
			Latitude:  37.3318,
			Longitude: -122.0312,
			Altitude:  10,
		},
	}

	result, err := ApplyMetadata(filePath, meta, ConflictPreferJSON)
	if err != nil {
		t.Fatalf("ApplyMetadata returned error: %v", err)
	}
	if !result.MetadataWritten {
		t.Fatal("expected metadata to be marked as written")
	}

	args := readFileString(t, argsFile)
	requireContains(t, args, "-AllDates=2023:12:31 16:00:00")
	requireContains(t, args, "-Title=Sunset")
	requireContains(t, args, "-GPSLongitude=122.0312000")
}

func TestReadEmbeddedMetadataParsesExifToolJSON(t *testing.T) {
	withFakeExifTool(t, map[string]string{
		"FAKE_EXIFTOOL_JSON": `[{"DateTimeOriginal":"2024:01:02 03:04:05","OffsetTimeOriginal":"+02:00","Title":"Hello","Description":"World","GPSLatitude":11.5,"GPSLongitude":22.25,"GPSAltitude":33}]`,
	})

	meta, err := ReadEmbeddedMetadata("ignored.jpg")
	if err != nil {
		t.Fatalf("ReadEmbeddedMetadata returned error: %v", err)
	}
	if meta.Title != "Hello" || meta.Description != "World" {
		t.Fatalf("unexpected embedded metadata: %+v", meta)
	}
	if meta.GPS.Latitude != 11.5 || meta.GPS.Longitude != 22.25 {
		t.Fatalf("unexpected GPS metadata: %+v", meta.GPS)
	}
	if meta.CaptureTime.IsZero() {
		t.Fatal("expected embedded capture time to be parsed")
	}
}

func TestReadEmbeddedMetadataPrefersCreationDateForVideo(t *testing.T) {
	withFakeExifTool(t, map[string]string{
		"FAKE_EXIFTOOL_JSON": `[{"CreateDate":"2025:05:15 11:04:43+03:00","CreationDate":"2025:05:15 14:04:43+03:00"}]`,
	})

	meta, err := ReadEmbeddedMetadata("ignored.mp4")
	if err != nil {
		t.Fatalf("ReadEmbeddedMetadata returned error: %v", err)
	}

	want := time.Date(2025, 5, 15, 11, 4, 43, 0, time.UTC)
	if !meta.CaptureTime.UTC().Equal(want) {
		t.Fatalf("expected capture time %s, got %s", want.Format(time.RFC3339), meta.CaptureTime.UTC().Format(time.RFC3339))
	}
}

func TestApplyMetadataWritesVideoArgsWithoutQuickTimeGPSCoordinates(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	withFakeExifTool(t, map[string]string{
		"FAKE_EXIFTOOL_ARGS_FILE": argsFile,
	})

	filePath := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(filePath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}

	meta := imageMetadata{
		Title: "Clip",
		PhotoTakenTime: takeoutTimestamp{
			Timestamp: "1704067200",
		},
		GeoData: takeoutGeoData{
			Latitude:  37.3318,
			Longitude: -122.0312,
			Altitude:  10,
		},
	}

	result, err := ApplyMetadata(filePath, meta, ConflictPreferJSON)
	if err != nil {
		t.Fatalf("ApplyMetadata returned error: %v", err)
	}
	if !result.MetadataWritten {
		t.Fatal("expected metadata to be marked as written")
	}

	args := readFileString(t, argsFile)
	requireContains(t, args, "-api QuickTimeUTC")
	requireContains(t, args, "-Keys:CreationDate=2023:12:31 16:00:00-08:00")
	requireContains(t, args, "-XMP-exif:GPSLatitude=37.3318000")
	if strings.Contains(args, "-Keys:GPSCoordinates=") {
		t.Fatalf("did not expect QuickTime GPSCoordinates write in args: %s", args)
	}
}

func TestNewExifToolCommandUsesArgFileOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific argfile behavior")
	}

	withFakeExifTool(t, nil)

	cmd, cleanup, err := newExifToolCommand("-Title=Veneția ț ș ä", `C:\tmp\Veneția\file.jpg`)
	if err != nil {
		t.Fatalf("newExifToolCommand returned error: %v", err)
	}

	if len(cmd.Args) != 3 || cmd.Args[1] != "-@" {
		t.Fatalf("expected argfile invocation, got %v", cmd.Args)
	}

	argFile := cmd.Args[2]
	body, err := os.ReadFile(argFile)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error: %v", argFile, err)
	}
	requireContains(t, string(body), "-Title=Veneția ț ș ä")
	requireContains(t, string(body), `C:\tmp\Veneția\file.jpg`)

	cleanup()
	if _, err := os.Stat(argFile); !os.IsNotExist(err) {
		t.Fatalf("expected arg file to be removed, stat err=%v", err)
	}
}

func TestBuildMetadataPlanHonorsConflictPolicies(t *testing.T) {
	withFakeExifTool(t, map[string]string{
		"FAKE_EXIFTOOL_JSON": `[{"DateTimeOriginal":"2024:01:02 03:04:05","OffsetTimeOriginal":"+00:00","Title":"Embedded title","Description":"Embedded description","GPSLatitude":11.5,"GPSLongitude":22.25}]`,
	})

	filePath := filepath.Join(t.TempDir(), "image.jpg")
	if err := os.WriteFile(filePath, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}

	meta := imageMetadata{
		Title:       "JSON title",
		Description: "JSON description",
		PhotoTakenTime: takeoutTimestamp{
			Timestamp: strconvFromUnix(t, time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)),
		},
		GeoData: takeoutGeoData{Latitude: 51.5, Longitude: 0.12},
	}

	preferEmbedded, err := buildMetadataPlan(meta, ConflictPreferEmbedded, filePath)
	if err != nil {
		t.Fatalf("buildMetadataPlan prefer embedded returned error: %v", err)
	}
	if preferEmbedded.WriteTitle || preferEmbedded.WriteDescription {
		t.Fatal("prefer-embedded should not overwrite existing title/description")
	}

	mergePlan, err := buildMetadataPlan(meta, ConflictMerge, filePath)
	if err != nil {
		t.Fatalf("buildMetadataPlan merge returned error: %v", err)
	}
	if mergePlan.WriteTitle || mergePlan.WriteDescription {
		t.Fatal("merge should preserve existing title/description when already present")
	}
	if !mergePlan.WriteGPS || !mergePlan.WriteTimestamp {
		t.Fatal("merge should still write JSON GPS and timestamp")
	}

	preferJSON, err := buildMetadataPlan(meta, ConflictPreferJSON, filePath)
	if err != nil {
		t.Fatalf("buildMetadataPlan prefer json returned error: %v", err)
	}
	if !preferJSON.WriteTitle || !preferJSON.WriteDescription {
		t.Fatal("prefer-json should overwrite title/description")
	}
}

func TestDetectSuspiciousDatesReportsReasons(t *testing.T) {
	withFakeExifTool(t, map[string]string{
		"FAKE_EXIFTOOL_JSON": `[{"DateTimeOriginal":"2024:01:01 00:00:00"}]`,
	})

	sourcePath := filepath.Join(t.TempDir(), "IMG_0001.jpg")
	writeTestFile(t, sourcePath, "image")

	meta := imageMetadata{
		PhotoTakenTime: takeoutTimestamp{Timestamp: "946684799"},
	}
	findings := detectSuspiciousDates(MediaPlan{
		SourcePath: sourcePath,
		Metadata:   &meta,
	}, filepath.Join(t.TempDir(), "out.jpg"))
	if len(findings) != 1 {
		t.Fatalf("expected one finding row, got %d", len(findings))
	}
	requireContains(t, findings[0].Reason, "timestamp before 2000")
	requireContains(t, findings[0].Reason, "JSON timestamp differs from embedded timestamp by more than 24h")
	requireContains(t, findings[0].Reason, "timezone guessed as UTC")

	futureMeta := imageMetadata{
		PhotoTakenTime: takeoutTimestamp{Timestamp: fmt.Sprintf("%d", time.Now().UTC().Add(24*time.Hour).Unix())},
	}
	futureFindings := detectSuspiciousDates(MediaPlan{
		SourcePath: sourcePath,
		Metadata:   &futureMeta,
	}, filepath.Join(t.TempDir(), "future.jpg"))
	if len(futureFindings) != 1 {
		t.Fatalf("expected one future finding row, got %d", len(futureFindings))
	}
	requireContains(t, futureFindings[0].Reason, "timestamp in the future")
}

func strconvFromUnix(t *testing.T, ts time.Time) string {
	t.Helper()
	return fmt.Sprintf("%d", ts.Unix())
}
