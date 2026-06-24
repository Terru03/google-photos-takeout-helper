package batch

import (
	"context"
	"time"

	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
)

const (
	statusStarted        = "started"
	statusPlanned        = "planned"
	statusSkippedResume  = "skipped-resume"
	statusSuccess        = "success"
	statusError          = "error"
	statusNeedsReview    = "needs-review"
	defaultOutputSubdir  = "Google_Photos_Final"
	defaultWorkSubdir    = "GTF_Work"
	defaultMarginBytes   = int64(25 * 1024 * 1024 * 1024)
	takeoutZipNameNeedle = "takeout"
)

type DriveKind string

const (
	DriveKindSSD     DriveKind = "SSD"
	DriveKindHDD     DriveKind = "HDD"
	DriveKindUnknown DriveKind = "unknown"
)

type DriveInfo struct {
	Root       string    `json:"root"`
	Letter     string    `json:"letter"`
	Label      string    `json:"label"`
	TotalBytes int64     `json:"totalBytes"`
	FreeBytes  int64     `json:"freeBytes"`
	Kind       DriveKind `json:"kind"`
	Model      string    `json:"model,omitempty"`
}

type ZipItem struct {
	Path              string    `json:"path"`
	Name              string    `json:"name"`
	SourceDrive       string    `json:"sourceDrive"`
	SizeBytes         int64     `json:"sizeBytes"`
	UncompressedBytes int64     `json:"uncompressedBytes"`
	ModTime           time.Time `json:"modTime"`
	Fingerprint       string    `json:"fingerprint"`
}

type ManifestEntry struct {
	ZipName           string                  `json:"zipName"`
	ZipPath           string                  `json:"zipPath"`
	ZipFingerprint    string                  `json:"zipFingerprint"`
	SourceDrive       string                  `json:"sourceDrive"`
	Status            string                  `json:"status"`
	StartedAt         time.Time               `json:"startedAt,omitempty"`
	FinishedAt        time.Time               `json:"finishedAt,omitempty"`
	ReportPath        string                  `json:"reportPath,omitempty"`
	ExtractedTempPath string                  `json:"extractedTempPath,omitempty"`
	OutputFolder      string                  `json:"outputFolder"`
	Error             string                  `json:"error,omitempty"`
	Summary           *fixer.RunReportSummary `json:"summary,omitempty"`
}

type PromptFunc func(question string, choices []string, allowMultiple bool) (string, error)

type ProcessFunc func(
	ctx context.Context,
	sourcePath string,
	outputPath string,
	progressCh chan<- fixer.Progress,
	options fixer.ProcessOptions,
) error

type Options struct {
	ZipRoots          []string
	WorkDir           string
	OutputDir         string
	AutoDrives        bool
	AskOnAmbiguous    bool
	KeepTempOnError   bool
	Reprocess         bool
	SafetyMarginBytes int64
	ProcessOptions    fixer.ProcessOptions
	Prompt            PromptFunc
	Process           ProcessFunc
}

type Result struct {
	ManifestPath string
	OutputDir    string
	WorkDir      string
	ZipCount     int
	Processed    int
	Skipped      int
	Planned      int
}
