package fixer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type RunReportSummary struct {
	TotalMedia         int `json:"totalMedia"`
	Matched            int `json:"matched"`
	Unmatched          int `json:"unmatched"`
	Ambiguous          int `json:"ambiguous"`
	MetadataWritten    int `json:"metadataWritten"`
	MetadataVerified   int `json:"metadataVerified"`
	DuplicatesLinked   int `json:"duplicatesLinked"`
	DuplicatesCopied   int `json:"duplicatesCopied"`
	Resumed            int `json:"resumed"`
	ExistingSkipped    int `json:"existingSkipped"`
	ConflictsFound     int `json:"conflictsFound"`
	PartnerSidecarUsed int `json:"partnerSidecarUsed"`
	Errors             int `json:"errors"`
}

type RunReport struct {
	mu         sync.Mutex
	StartedAt  time.Time        `json:"startedAt"`
	FinishedAt time.Time        `json:"finishedAt"`
	SourceRoot string           `json:"sourceRoot"`
	OutputRoot string           `json:"outputRoot"`
	Options    ProcessOptions   `json:"options"`
	Summary    RunReportSummary `json:"summary"`
	Records    []ProcessRecord  `json:"records"`
}

func NewRunReport(sourceRoot string, outputRoot string, options ProcessOptions) *RunReport {
	return &RunReport{
		StartedAt:  time.Now().UTC(),
		SourceRoot: sourceRoot,
		OutputRoot: outputRoot,
		Options:    options,
	}
}

func (r *RunReport) Add(record ProcessRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.Records = append(r.Records, record)
	r.Summary.TotalMedia++
	switch record.MatchStatus {
	case MatchStatusMatched:
		r.Summary.Matched++
	case MatchStatusUnmatched:
		r.Summary.Unmatched++
	case MatchStatusAmbiguous:
		r.Summary.Ambiguous++
	}

	if record.MetadataWritten {
		r.Summary.MetadataWritten++
	}
	if record.MetadataVerified {
		r.Summary.MetadataVerified++
	}
	if len(record.Conflicts) > 0 {
		r.Summary.ConflictsFound += len(record.Conflicts)
	}
	if record.UsedPartnerSidecar {
		r.Summary.PartnerSidecarUsed++
	}

	switch record.Status {
	case OperationSkippedResume:
		r.Summary.Resumed++
	case OperationSkippedExisting:
		r.Summary.ExistingSkipped++
	case OperationHardlinked, OperationSymlinked:
		r.Summary.DuplicatesLinked++
	case OperationDuplicateCopied:
		r.Summary.DuplicatesCopied++
	case OperationError:
		r.Summary.Errors++
	}

	if record.Error != "" && record.Status != OperationError {
		r.Summary.Errors++
	}
}

func (r *RunReport) Write(baseDir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.FinishedAt = time.Now().UTC()

	reportDir := filepath.Join(baseDir, "reports")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return err
	}

	stamp := r.FinishedAt.Format("2006-01-02_15-04-05")
	jsonPath := filepath.Join(reportDir, stamp+".json")
	textPath := filepath.Join(reportDir, stamp+".txt")
	latestJSON := filepath.Join(reportDir, "latest.json")
	latestText := filepath.Join(reportDir, "latest.txt")

	jsonBytes, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, jsonBytes, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(latestJSON, jsonBytes, 0o644); err != nil {
		return err
	}

	textBody := r.toText()
	if err := os.WriteFile(textPath, []byte(textBody), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(latestText, []byte(textBody), 0o644); err != nil {
		return err
	}

	return nil
}

func (r *RunReport) toText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "GoogleTakeoutFixer Audit Report\n")
	fmt.Fprintf(&b, "Started: %s\n", r.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "Finished: %s\n", r.FinishedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "Source: %s\n", r.SourceRoot)
	fmt.Fprintf(&b, "Output: %s\n", r.OutputRoot)
	fmt.Fprintf(&b, "\nSummary\n")
	fmt.Fprintf(&b, "  Total media: %d\n", r.Summary.TotalMedia)
	fmt.Fprintf(&b, "  Matched: %d\n", r.Summary.Matched)
	fmt.Fprintf(&b, "  Unmatched: %d\n", r.Summary.Unmatched)
	fmt.Fprintf(&b, "  Ambiguous: %d\n", r.Summary.Ambiguous)
	fmt.Fprintf(&b, "  Metadata written: %d\n", r.Summary.MetadataWritten)
	fmt.Fprintf(&b, "  Metadata verified: %d\n", r.Summary.MetadataVerified)
	fmt.Fprintf(&b, "  Duplicate links created: %d\n", r.Summary.DuplicatesLinked)
	fmt.Fprintf(&b, "  Duplicate copies kept: %d\n", r.Summary.DuplicatesCopied)
	fmt.Fprintf(&b, "  Resumed/skipped from state: %d\n", r.Summary.Resumed)
	fmt.Fprintf(&b, "  Existing files skipped: %d\n", r.Summary.ExistingSkipped)
	fmt.Fprintf(&b, "  Partner sidecar matches: %d\n", r.Summary.PartnerSidecarUsed)
	fmt.Fprintf(&b, "  Conflicts found: %d\n", r.Summary.ConflictsFound)
	fmt.Fprintf(&b, "  Errors: %d\n", r.Summary.Errors)

	fmt.Fprintf(&b, "\nProblem Files\n")
	problems := 0
	for _, record := range r.Records {
		if record.MatchStatus == MatchStatusMatched && record.Error == "" && len(record.Conflicts) == 0 {
			continue
		}
		problems++
		fmt.Fprintf(&b, "  - %s [%s]", record.SourceRelPath, record.MatchStatus)
		if record.Error != "" {
			fmt.Fprintf(&b, " error=%s", record.Error)
		}
		if len(record.Conflicts) > 0 {
			fields := make([]string, 0, len(record.Conflicts))
			for _, conflict := range record.Conflicts {
				fields = append(fields, conflict.Field)
			}
			fmt.Fprintf(&b, " conflicts=%s", strings.Join(fields, ","))
		}
		fmt.Fprintln(&b)
	}
	if problems == 0 {
		fmt.Fprintf(&b, "  None\n")
	}

	return b.String()
}
