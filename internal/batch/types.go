package batch

import (
	"context"
	"time"

	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
)

const (
	statusPending       = "pending"
	statusExtracting    = "extracting"
	statusProcessing    = "processing"
	statusCompleted     = "completed"
	statusFailed        = "failed"
	statusInterrupted   = "interrupted"
	defaultOutputSubdir = "Google_Photos_Final"
	defaultWorkSubdir   = "GTF_Work"
	defaultMarginBytes  = int64(25 * 1024 * 1024 * 1024)
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
	ZipName        string                  `json:"zipName"`
	ZipPath        string                  `json:"zipPath"`
	ZipSize        int64                   `json:"zipSize"`
	ZipModified    time.Time               `json:"zipModified"`
	ZipFingerprint string                  `json:"zipFingerprint"`
	SourceDrive    string                  `json:"sourceDrive"`
	Status         string                  `json:"status"`
	StartTime      time.Time               `json:"startTime,omitempty"`
	EndTime        time.Time               `json:"endTime,omitempty"`
	ReportPath     string                  `json:"reportPath,omitempty"`
	ExtractedRoot  string                  `json:"extractedRoot,omitempty"`
	OutputFolder   string                  `json:"outputFolder"`
	Error          string                  `json:"error,omitempty"`
	Summary        *fixer.RunReportSummary `json:"summary,omitempty"`
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
	PreflightOnly     bool
	Reprocess         bool
	SafetyMarginBytes int64
	ProcessOptions    fixer.ProcessOptions
	Prompt            PromptFunc
	Process           ProcessFunc
	Progress          func(BatchProgress)
	StopAfterCurrent  func() bool
}

type Result struct {
	ManifestPath string
	OutputDir    string
	WorkDir      string
	ZipCount     int
	Processed    int
	Skipped      int
	Planned      int
	Failed       int
	Stopped      bool
	Preflight    *PreflightReport
}

type BatchProgress struct {
	CurrentZip    string
	Completed     int
	Total         int
	FileProcessed int
	FileTotal     int
	CurrentFile   string
	LatestError   string
	ReportPath    string
}

type PreflightReport struct {
	ZipCount              int      `json:"zipCount"`
	TotalZipSize          int64    `json:"totalZipSize"`
	OutputFreeBytes       int64    `json:"outputFreeBytes"`
	WorkFreeBytes         int64    `json:"workFreeBytes"`
	EstimatedMinWorkBytes int64    `json:"estimatedMinWorkBytes"`
	OutputDir             string   `json:"outputDir"`
	WorkDir               string   `json:"workDir"`
	ManifestPath          string   `json:"manifestPath"`
	StatePath             string   `json:"statePath"`
	Warnings              []string `json:"warnings,omitempty"`
	ZipPaths              []string `json:"zipPaths"`
}
