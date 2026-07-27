package fixer

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RunReportSummary struct {
	TotalMedia                   int   `json:"totalMedia"`
	JSONSidecarsFound            int   `json:"jsonSidecarsFound"`
	Matched                      int   `json:"matched"`
	MatchedCleanly               int   `json:"matchedCleanly"`
	MatchedWithFallback          int   `json:"matchedWithFallback"`
	Unmatched                    int   `json:"unmatched"`
	Ambiguous                    int   `json:"ambiguous"`
	OutputMedia                  int   `json:"outputMedia"`
	MetadataWritten              int   `json:"metadataWritten"`
	XMPSidecarsWritten           int   `json:"xmpSidecarsWritten"`
	MetadataVerified             int   `json:"metadataVerified"`
	MetadataVerificationFailures int   `json:"metadataVerificationFailures"`
	DuplicatesLinked             int   `json:"duplicatesLinked"`
	DuplicatesCopied             int   `json:"duplicatesCopied"`
	DuplicatesReused             int   `json:"duplicatesReused"`
	Resumed                      int   `json:"resumed"`
	ExistingSkipped              int   `json:"existingSkipped"`
	ConflictsFound               int   `json:"conflictsFound"`
	PartnerSidecarUsed           int   `json:"partnerSidecarUsed"`
	ApproxDedupBytesSaved        int64 `json:"approxDedupBytesSaved"`
	SuspiciousDates              int   `json:"suspiciousDates"`
	Errors                       int   `json:"errors"`
}

type SourceCleanupStatus string

const (
	SourceCleanupStatusSkippedDisabled  SourceCleanupStatus = "skipped-disabled"
	SourceCleanupStatusSkippedDryRun    SourceCleanupStatus = "skipped-dry-run"
	SourceCleanupStatusSkippedCancelled SourceCleanupStatus = "skipped-cancelled"
	SourceCleanupStatusSkippedProblems  SourceCleanupStatus = "skipped-problems"
	SourceCleanupStatusDeleted          SourceCleanupStatus = "deleted"
	SourceCleanupStatusFailed           SourceCleanupStatus = "failed"
)

type SourceCleanupResult struct {
	Enabled bool                `json:"enabled"`
	Status  SourceCleanupStatus `json:"status,omitempty"`
	Path    string              `json:"path,omitempty"`
	Reason  string              `json:"reason,omitempty"`
	Error   string              `json:"error,omitempty"`
}

type RunReport struct {
	mu              sync.Mutex              `json:"-"`
	StartedAt       time.Time               `json:"startedAt"`
	FinishedAt      time.Time               `json:"finishedAt"`
	SourceRoot      string                  `json:"sourceRoot"`
	OutputRoot      string                  `json:"outputRoot"`
	ReportDir       string                  `json:"reportDir,omitempty"`
	LogDir          string                  `json:"logDir,omitempty"`
	Options         ProcessOptions          `json:"options"`
	Summary         RunReportSummary        `json:"summary"`
	MotionPhotoPass *MotionPhotoPassResult  `json:"motionPhotoPass,omitempty"`
	SourceCleanup   *SourceCleanupResult    `json:"sourceCleanup,omitempty"`
	SuspiciousDates []SuspiciousDateFinding `json:"suspiciousDates,omitempty"`
	Records         []ProcessRecord         `json:"records"`
}

type SuspiciousDateFinding struct {
	SourcePath        string `json:"sourcePath"`
	OutputPath        string `json:"outputPath,omitempty"`
	JSONTimestamp     string `json:"jsonTimestamp,omitempty"`
	EmbeddedTimestamp string `json:"embeddedTimestamp,omitempty"`
	Reason            string `json:"reason"`
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
		if isFallbackMatch(record.MatchStrategy) {
			r.Summary.MatchedWithFallback++
		} else {
			r.Summary.MatchedCleanly++
		}
	case MatchStatusUnmatched:
		r.Summary.Unmatched++
	case MatchStatusAmbiguous:
		r.Summary.Ambiguous++
	}

	if record.MetadataWritten {
		r.Summary.MetadataWritten++
	}
	if record.MetadataWritten && record.UsedXMPSidecar {
		r.Summary.XMPSidecarsWritten++
	}
	if record.MetadataVerified {
		r.Summary.MetadataVerified++
	}
	if strings.Contains(record.Error, "verification failed") {
		r.Summary.MetadataVerificationFailures++
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
		r.Summary.DuplicatesReused++
		r.Summary.DuplicatesLinked++
		r.Summary.ApproxDedupBytesSaved += record.SourceSize
	case OperationDuplicateCopied:
		r.Summary.DuplicatesReused++
		r.Summary.DuplicatesCopied++
	case OperationError:
		r.Summary.Errors++
	}
	if record.OutputPath != "" && record.Successful() && record.Status != OperationDryRun {
		r.Summary.OutputMedia++
	}

	if record.Error != "" && record.Status != OperationError {
		r.Summary.Errors++
	}
}

func (r *RunReport) SetJSONSidecarsFound(count int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.Summary.JSONSidecarsFound = count
}

func (r *RunReport) AddSuspiciousDates(findings []SuspiciousDateFinding) {
	if len(findings) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.SuspiciousDates = append(r.SuspiciousDates, findings...)
	r.Summary.SuspiciousDates += len(findings)
}

func (r *RunReport) SetMotionPhotoPass(result MotionPhotoPassResult) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.MotionPhotoPass = &result
	if result.Error != "" {
		r.Summary.Errors++
	}
	r.Summary.Errors += result.CleanupErrors
}

func (r *RunReport) SetSourceCleanup(result SourceCleanupResult) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.SourceCleanup = &result
	if result.Error != "" {
		r.Summary.Errors++
	}
}

func (r *RunReport) CanDeleteSourceRoot() (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.Summary.Unmatched > 0 || r.Summary.Ambiguous > 0 || r.Summary.Errors > 0 {
		return false, fmt.Sprintf(
			"kept input because summary has unmatched=%d ambiguous=%d errors=%d",
			r.Summary.Unmatched,
			r.Summary.Ambiguous,
			r.Summary.Errors,
		)
	}
	return true, ""
}

func (r *RunReport) Write(baseDir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.FinishedAt = time.Now().UTC()

	reportDir := filepath.Join(baseDir, "reports")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return err
	}
	r.ReportDir = reportDir
	r.LogDir = filepath.Join(baseDir, "logs")

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
	if err := r.writeSuspiciousDatesCSV(reportDir); err != nil {
		return err
	}
	if err := r.writeReviewCSV(reportDir); err != nil {
		return err
	}

	return nil
}

func (r *RunReport) writeSuspiciousDatesCSV(reportDir string) error {
	file, err := os.Create(filepath.Join(reportDir, "suspicious_dates.csv"))
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"source_path", "output_path", "json_timestamp", "embedded_timestamp", "reason"}); err != nil {
		return err
	}
	for _, finding := range r.SuspiciousDates {
		if err := writer.Write([]string{
			finding.SourcePath,
			finding.OutputPath,
			finding.JSONTimestamp,
			finding.EmbeddedTimestamp,
			finding.Reason,
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func (r *RunReport) writeReviewCSV(reportDir string) error {
	file, err := os.Create(filepath.Join(reportDir, "review.csv"))
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	writer := csv.NewWriter(file)
	if err := writer.Write([]string{
		"source_rel_path",
		"match_status",
		"match_strategy",
		"status",
		"folder_date_source",
		"folder_year",
		"folder_month",
		"output_path",
		"sidecar_path",
		"partner_rel_path",
		"match_candidates",
		"conflicts",
		"error",
	}); err != nil {
		return err
	}
	for _, record := range r.Records {
		if !needsReview(record) {
			continue
		}
		if err := writer.Write([]string{
			record.SourceRelPath,
			string(record.MatchStatus),
			string(record.MatchStrategy),
			string(record.Status),
			record.FolderDateSource,
			strconv.Itoa(record.FolderYear),
			strconv.Itoa(record.FolderMonth),
			record.OutputPath,
			record.SidecarPath,
			record.PartnerRelPath,
			strings.Join(record.MatchCandidates, "|"),
			strings.Join(conflictFields(record.Conflicts), "|"),
			record.Error,
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func (r *RunReport) toText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Google Photos Takeout Helper Audit Report\n")
	fmt.Fprintf(&b, "Started: %s\n", r.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "Finished: %s\n", r.FinishedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "Source: %s\n", r.SourceRoot)
	fmt.Fprintf(&b, "Output: %s\n", r.OutputRoot)
	fmt.Fprintf(&b, "Metadata output: %s\n", MetadataOutputModeForOptions(r.Options))
	if r.MotionPhotoPass != nil && r.MotionPhotoPass.Enabled {
		fmt.Fprintf(&b, "Motion photo pass: %s", r.MotionPhotoPass.Status)
		if r.MotionPhotoPass.ToolPath != "" {
			fmt.Fprintf(&b, " (%s)", r.MotionPhotoPass.ToolPath)
		}
		if r.MotionPhotoPass.Error != "" {
			fmt.Fprintf(&b, " error=%s", r.MotionPhotoPass.Error)
		}
		fmt.Fprintln(&b)
		if r.MotionPhotoPass.StandaloneVideoCandidates > 0 {
			fmt.Fprintf(
				&b,
				"Motion photo counts: pairs=%d embedded=%d videos_kept=%d videos_deleted=%d videos_skipped=%d failures=%d candidates=%d\n",
				r.MotionPhotoPass.PairsDetected,
				r.MotionPhotoPass.EmbeddedSuccessfully,
				r.MotionPhotoPass.StandaloneVideosKept,
				r.MotionPhotoPass.StandaloneVideosDeleted,
				r.MotionPhotoPass.StandaloneVideosSkipped,
				r.MotionPhotoPass.Failures,
				r.MotionPhotoPass.StandaloneVideoCandidates,
			)
		}
	}
	if r.SourceCleanup != nil && r.SourceCleanup.Enabled {
		fmt.Fprintf(&b, "Source cleanup: %s", r.SourceCleanup.Status)
		if r.SourceCleanup.Path != "" {
			fmt.Fprintf(&b, " (%s)", r.SourceCleanup.Path)
		}
		if r.SourceCleanup.Reason != "" {
			fmt.Fprintf(&b, " reason=%s", r.SourceCleanup.Reason)
		}
		if r.SourceCleanup.Error != "" {
			fmt.Fprintf(&b, " error=%s", r.SourceCleanup.Error)
		}
		fmt.Fprintln(&b)
	}
	fmt.Fprintf(&b, "\nSummary\n")
	fmt.Fprintf(&b, "  Input media found: %d\n", r.Summary.TotalMedia)
	fmt.Fprintf(&b, "  JSON sidecars found: %d\n", r.Summary.JSONSidecarsFound)
	fmt.Fprintf(&b, "  Matched cleanly: %d\n", r.Summary.MatchedCleanly)
	fmt.Fprintf(&b, "  Matched with fallback: %d\n", r.Summary.MatchedWithFallback)
	fmt.Fprintf(&b, "  Matched total: %d\n", r.Summary.Matched)
	fmt.Fprintf(&b, "  Unmatched: %d\n", r.Summary.Unmatched)
	fmt.Fprintf(&b, "  Ambiguous: %d\n", r.Summary.Ambiguous)
	fmt.Fprintf(&b, "  Output media count: %d\n", r.Summary.OutputMedia)
	fmt.Fprintf(&b, "  Metadata written: %d\n", r.Summary.MetadataWritten)
	fmt.Fprintf(&b, "  XMP sidecars written: %d\n", r.Summary.XMPSidecarsWritten)
	fmt.Fprintf(&b, "  Metadata verified: %d\n", r.Summary.MetadataVerified)
	fmt.Fprintf(&b, "  Verification failures: %d\n", r.Summary.MetadataVerificationFailures)
	fmt.Fprintf(&b, "  Duplicates reused: %d\n", r.Summary.DuplicatesReused)
	fmt.Fprintf(&b, "  Duplicate links created: %d\n", r.Summary.DuplicatesLinked)
	fmt.Fprintf(&b, "  Duplicate copies kept: %d\n", r.Summary.DuplicatesCopied)
	fmt.Fprintf(&b, "  Approx dedup space saved: %s\n", FormatBytes(r.Summary.ApproxDedupBytesSaved))
	fmt.Fprintf(&b, "  Deduplication: exact SHA-256 duplicates only; near duplicates are not removed.\n")
	fmt.Fprintf(&b, "  Resumed/skipped from state: %d\n", r.Summary.Resumed)
	fmt.Fprintf(&b, "  Existing files skipped: %d\n", r.Summary.ExistingSkipped)
	fmt.Fprintf(&b, "  Partner sidecar matches: %d\n", r.Summary.PartnerSidecarUsed)
	fmt.Fprintf(&b, "  Conflicts found: %d\n", r.Summary.ConflictsFound)
	fmt.Fprintf(&b, "  Suspicious date rows: %d\n", r.Summary.SuspiciousDates)
	fmt.Fprintf(&b, "  Errors: %d\n", r.Summary.Errors)
	if r.MotionPhotoPass != nil {
		fmt.Fprintf(&b, "  Motion photos detected: %d\n", r.MotionPhotoPass.PairsDetected)
		fmt.Fprintf(&b, "  Motion photos rebuilt: %d\n", r.MotionPhotoPass.EmbeddedSuccessfully)
	} else {
		fmt.Fprintf(&b, "  Motion photos detected: 0\n")
		fmt.Fprintf(&b, "  Motion photos rebuilt: 0\n")
	}
	if r.Summary.DuplicatesLinked > 0 {
		fmt.Fprintf(&b, "  Note: hardlinks save disk space, but Explorer can still show near full size.\n")
	}

	fmt.Fprintf(&b, "\nProblem Files\n")
	problems := 0
	for _, record := range r.Records {
		if record.MatchStatus == MatchStatusMatched && record.Error == "" && len(record.Conflicts) == 0 {
			continue
		}
		problems++
		fmt.Fprintf(&b, "  - %s [%s]", record.SourceRelPath, record.MatchStatus)
		if record.PartnerRelPath != "" {
			fmt.Fprintf(&b, " partner=%s", record.PartnerRelPath)
		}
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

	fmt.Fprintf(&b, "\nArtifacts\n")
	if r.ReportDir != "" {
		fmt.Fprintf(&b, "  Reports: %s\n", r.ReportDir)
		fmt.Fprintf(&b, "  Latest text: %s\n", filepath.Join(r.ReportDir, "latest.txt"))
		fmt.Fprintf(&b, "  Latest JSON: %s\n", filepath.Join(r.ReportDir, "latest.json"))
		fmt.Fprintf(&b, "  Review CSV: %s\n", filepath.Join(r.ReportDir, "review.csv"))
	}
	if r.LogDir != "" {
		fmt.Fprintf(&b, "  Logs: %s\n", r.LogDir)
	}

	return b.String()
}

func isFallbackMatch(strategy MatchStrategy) bool {
	switch strategy {
	case MatchStrategyExactName, MatchStrategyJSONTitle:
		return false
	default:
		return true
	}
}

func needsReview(record ProcessRecord) bool {
	return record.MatchStatus != MatchStatusMatched ||
		record.Error != "" ||
		len(record.Conflicts) > 0
}

func conflictFields(conflicts []MetadataFieldConflict) []string {
	fields := make([]string, 0, len(conflicts))
	for _, conflict := range conflicts {
		fields = append(fields, conflict.Field)
	}
	return fields
}
