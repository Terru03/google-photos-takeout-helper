package fixer

import (
	"path/filepath"
	"testing"
)

func TestRunReportWritesFinalProofCountsAndReviewCSV(t *testing.T) {
	report := NewRunReport("input", "output", ProcessOptions{
		WriteMetadata:    true,
		WriteXMPSidecars: true,
	})
	report.SetJSONSidecarsFound(3)
	report.Add(ProcessRecord{
		SourceRelPath:    filepath.ToSlash(filepath.Join("Photos from 2024", "clean.jpg")),
		OutputPath:       filepath.Join("output", "Photos from 2024", "clean.jpg"),
		MatchStatus:      MatchStatusMatched,
		MatchStrategy:    MatchStrategyExactName,
		Status:           OperationCopiedWithMetadata,
		MetadataWritten:  true,
		UsedXMPSidecar:   true,
		MetadataVerified: true,
	})
	report.Add(ProcessRecord{
		SourceRelPath: filepath.ToSlash(filepath.Join("Photos from 2024", "fallback.mp4")),
		OutputPath:    filepath.Join("output", "Photos from 2024", "fallback.mp4"),
		MatchStatus:   MatchStatusMatched,
		MatchStrategy: MatchStrategyPartner,
		Status:        OperationCopied,
	})
	report.Add(ProcessRecord{
		SourceRelPath:   filepath.ToSlash(filepath.Join("Photos from 2024", "bad.jpg")),
		OutputPath:      filepath.Join("output", "Photos from 2024", "bad.jpg"),
		MatchStatus:     MatchStatusAmbiguous,
		MatchCandidates: []string{"a.json", "b.json"},
		Status:          OperationCopied,
		Error:           "verification failed: capture time mismatch",
	})

	runRoot := t.TempDir()
	if err := report.Write(runRoot); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	text := readFileString(t, filepath.Join(runRoot, "reports", "latest.txt"))
	requireContains(t, text, "JSON sidecars found: 3")
	requireContains(t, text, "Matched cleanly: 1")
	requireContains(t, text, "Matched with fallback: 1")
	requireContains(t, text, "Output media count: 3")
	requireContains(t, text, "XMP sidecars written: 1")
	requireContains(t, text, "Verification failures: 1")
	requireContains(t, text, "Review CSV:")

	review := readFileString(t, filepath.Join(runRoot, "reports", "review.csv"))
	requireContains(t, review, "source_rel_path,match_status,match_strategy,status")
	requireContains(t, review, "bad.jpg")
	requireContains(t, review, "a.json|b.json")
}
