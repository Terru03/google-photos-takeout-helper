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

func TestDiscoverMediaPlanPairsSupplementalMetadataDuplicateOrdinals(t *testing.T) {
	root := t.TempDir()
	albumDir := filepath.Join(root, "Photos from 2005")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(albumDir, "Copy of Sony(045).jpg"), "first")
	writeTestFile(t, filepath.Join(albumDir, "Copy of Sony(045)(1).jpg"), "second")
	writeTestFile(t, filepath.Join(albumDir, "Copy of Sony(045).jpg.supplemental-metadata.json"), `{"title":"Copy of Sony(045).jpg","photoTakenTime":{"timestamp":"1128010000"}}`)
	writeTestFile(t, filepath.Join(albumDir, "Copy of Sony(045).jpg.supplemental-metadata(1).json"), `{"title":"Copy of Sony(045).jpg","photoTakenTime":{"timestamp":"1128020000"}}`)

	plans, err := DiscoverMediaPlan(root, ProcessOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 {
		t.Fatalf("plans = %d, want 2", len(plans))
	}

	for _, plan := range plans {
		if plan.MatchStatus != MatchStatusMatched {
			t.Fatalf("%s status = %s, want matched", plan.FileName, plan.MatchStatus)
		}
		switch plan.FileName {
		case "Copy of Sony(045).jpg":
			if filepath.Base(plan.SidecarPath) != "Copy of Sony(045).jpg.supplemental-metadata.json" {
				t.Fatalf("base file got wrong sidecar %q", plan.SidecarPath)
			}
		case "Copy of Sony(045)(1).jpg":
			if filepath.Base(plan.SidecarPath) != "Copy of Sony(045).jpg.supplemental-metadata(1).json" {
				t.Fatalf("copy file got wrong sidecar %q", plan.SidecarPath)
			}
		default:
			t.Fatalf("unexpected media %q", plan.FileName)
		}
	}
}

func TestDiscoverMediaPlanUsesSidecarIndexFromOtherZip(t *testing.T) {
	root := t.TempDir()
	albumDir := filepath.Join(root, "Photos from 2006")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(albumDir, "PIC_0008.JPG"), "image")

	index := NewSidecarIndex()
	if err := index.AddJSON(
		filepath.Join("Photos from 2006", "PIC_0008.JPG.supplemental-metadata.json"),
		"takeout-005.zip::Takeout/Google Photos/Photos from 2006/PIC_0008.JPG.supplemental-metadata.json",
		[]byte(`{"title":"PIC_0008.JPG","photoTakenTime":{"timestamp":"1165449600"}}`),
	); err != nil {
		t.Fatal(err)
	}

	plans, err := DiscoverMediaPlan(root, ProcessOptions{SidecarIndex: index})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans = %d, want 1", len(plans))
	}
	if plans[0].MatchStatus != MatchStatusMatched {
		t.Fatalf("status = %s, want matched", plans[0].MatchStatus)
	}
	if plans[0].Metadata == nil {
		t.Fatal("global sidecar metadata missing")
	}
	timestamp, err := plans[0].Metadata.PhotoTakenTimestamp()
	if err != nil {
		t.Fatal(err)
	}
	if got := timestamp.UTC().Format("2006-01-02"); got != "2006-12-07" {
		t.Fatalf("date = %s, want 2006-12-07", got)
	}
}

func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
