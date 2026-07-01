package fixer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverMediaPlanMatchesDuplicateSuffixSidecar(t *testing.T) {
	root := t.TempDir()
	albumDir := filepath.Join(root, "Photos from 2020")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(albumDir, "IMG_0001(1).jpg"), "image")
	writeTestFile(t, filepath.Join(albumDir, "IMG_0001.jpg(1).json"), `{}`)

	plans, err := DiscoverMediaPlan(root, ProcessOptions{})
	if err != nil {
		t.Fatalf("DiscoverMediaPlan returned error: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}

	if plans[0].MatchStatus != MatchStatusMatched {
		t.Fatalf("expected matched status, got %s", plans[0].MatchStatus)
	}
	if plans[0].MatchStrategy != MatchStrategyNormalizedName {
		t.Fatalf("expected normalized-name strategy, got %s", plans[0].MatchStrategy)
	}
}

func TestDiscoverMediaPlanMatchesEditedVariantSidecar(t *testing.T) {
	root := t.TempDir()
	albumDir := filepath.Join(root, "Photos from 2020")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(albumDir, "IMG_0001-edited.jpg"), "image")
	writeTestFile(t, filepath.Join(albumDir, "IMG_0001.jpg.json"), `{"title":"IMG_0001.jpg"}`)

	plans, err := DiscoverMediaPlan(root, ProcessOptions{})
	if err != nil {
		t.Fatalf("DiscoverMediaPlan returned error: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}

	if plans[0].MatchStatus != MatchStatusMatched {
		t.Fatalf("expected matched status, got %s", plans[0].MatchStatus)
	}
	if plans[0].MatchStrategy != MatchStrategyNormalizedName {
		t.Fatalf("expected normalized-name strategy, got %s", plans[0].MatchStrategy)
	}
}

func TestDiscoverMediaPlanMatchesLongFilenameSidecar(t *testing.T) {
	root := t.TempDir()
	albumDir := filepath.Join(root, "Photos from 2020")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}

	longStem := "IMG_" + strings.Repeat("1234567890", 6)
	truncatedStem := longStem[:46]
	writeTestFile(t, filepath.Join(albumDir, longStem+".jpg"), "image")
	writeTestFile(t, filepath.Join(albumDir, truncatedStem+".jpg.json"), `{}`)

	plans, err := DiscoverMediaPlan(root, ProcessOptions{})
	if err != nil {
		t.Fatalf("DiscoverMediaPlan returned error: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}

	if plans[0].MatchStatus != MatchStatusMatched {
		t.Fatalf("expected matched status, got %s", plans[0].MatchStatus)
	}
	if plans[0].MatchStrategy != MatchStrategyNormalizedName {
		t.Fatalf("expected normalized-name strategy, got %s", plans[0].MatchStrategy)
	}
}

func TestDiscoverMediaPlanUsesPartnerSidecarForVideo(t *testing.T) {
	root := t.TempDir()
	albumDir := filepath.Join(root, "Photos from 2021")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(albumDir, "PXL_1234.jpg"), "image")
	writeTestFile(t, filepath.Join(albumDir, "PXL_1234.mp4"), "video")
	writeTestFile(t, filepath.Join(albumDir, "PXL_1234.jpg.json"), `{"title":"PXL_1234.jpg","photoTakenTime":{"timestamp":"1700000000"}}`)

	plans, err := DiscoverMediaPlan(root, ProcessOptions{})
	if err != nil {
		t.Fatalf("DiscoverMediaPlan returned error: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(plans))
	}

	var videoPlan *MediaPlan
	for i := range plans {
		if plans[i].IsVideo {
			videoPlan = &plans[i]
			break
		}
	}
	if videoPlan == nil {
		t.Fatal("expected a video plan")
	}
	if videoPlan.MatchStatus != MatchStatusMatched {
		t.Fatalf("expected matched status, got %s", videoPlan.MatchStatus)
	}
	if videoPlan.MatchStrategy != MatchStrategyPartner {
		t.Fatalf("expected partner-sidecar strategy, got %s", videoPlan.MatchStrategy)
	}
	if videoPlan.PartnerPath == "" {
		t.Fatal("expected partner path to be populated")
	}
}

func TestDiscoverMediaPlanUsesPartnerSidecarForImage(t *testing.T) {
	root := t.TempDir()
	albumDir := filepath.Join(root, "Photos from 2021")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(albumDir, "PXL_5678.jpg"), "image")
	writeTestFile(t, filepath.Join(albumDir, "PXL_5678.mp4"), "video")
	writeTestFile(t, filepath.Join(albumDir, "PXL_5678.mp4.json"), `{"title":"PXL_5678.mp4","photoTakenTime":{"timestamp":"1700000000"}}`)

	plans, err := DiscoverMediaPlan(root, ProcessOptions{})
	if err != nil {
		t.Fatalf("DiscoverMediaPlan returned error: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(plans))
	}

	var imagePlan *MediaPlan
	for i := range plans {
		if !plans[i].IsVideo {
			imagePlan = &plans[i]
			break
		}
	}
	if imagePlan == nil {
		t.Fatal("expected an image plan")
	}
	if imagePlan.MatchStatus != MatchStatusMatched {
		t.Fatalf("expected matched status, got %s", imagePlan.MatchStatus)
	}
	if imagePlan.MatchStrategy != MatchStrategyPartner {
		t.Fatalf("expected partner-sidecar strategy, got %s", imagePlan.MatchStrategy)
	}
	if imagePlan.PartnerPath == "" {
		t.Fatal("expected partner path to be populated")
	}
}

func TestDiscoverMediaPlanLinksPartnerMediaWithoutSidecar(t *testing.T) {
	root := t.TempDir()
	albumDir := filepath.Join(root, "Photos from 2023")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(albumDir, "MVIMG_0001.jpg"), "image")
	writeTestFile(t, filepath.Join(albumDir, "MVIMG_0001.mp4"), "video")

	plans, err := DiscoverMediaPlan(root, ProcessOptions{})
	if err != nil {
		t.Fatalf("DiscoverMediaPlan returned error: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(plans))
	}

	for i := range plans {
		if plans[i].PartnerPath == "" {
			t.Fatalf("expected partner path for %s", plans[i].FileName)
		}
		if plans[i].MatchStatus != MatchStatusUnmatched {
			t.Fatalf("expected unmatched status without sidecar for %s, got %s", plans[i].FileName, plans[i].MatchStatus)
		}
	}
}

func TestDiscoverMediaPlanMarksAmbiguousMatches(t *testing.T) {
	root := t.TempDir()
	albumDir := filepath.Join(root, "Photos from 2022")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(albumDir, "IMG_9999.jpg"), "image")
	writeTestFile(t, filepath.Join(albumDir, "candidate-a.json"), `{"title":"IMG_9999.jpg","photoTakenTime":{"timestamp":"1700000000"}}`)
	writeTestFile(t, filepath.Join(albumDir, "candidate-b.json"), `{"title":"IMG_9999.jpg","photoTakenTime":{"timestamp":"1700000000"}}`)

	plans, err := DiscoverMediaPlan(root, ProcessOptions{})
	if err != nil {
		t.Fatalf("DiscoverMediaPlan returned error: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	if plans[0].MatchStatus != MatchStatusAmbiguous {
		t.Fatalf("expected ambiguous status, got %s", plans[0].MatchStatus)
	}
	if len(plans[0].MatchCandidates) != 2 {
		t.Fatalf("expected 2 match candidates, got %d", len(plans[0].MatchCandidates))
	}
}

func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
