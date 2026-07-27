package batch

import (
	"context"
	"time"

	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
)

const (
	statusPending          = "pending"
	statusExtracting       = "extracting"
	statusProcessing       = "processing"
	statusCompleted        = "completed"
	statusCompletedReview  = "completed_with_review"
	statusFailed           = "failed"
	statusInterrupted      = "interrupted"
	defaultOutputSubdir    = "Google_Photos_Final"
	defaultWorkSubdir      = "GTF_Work"
	defaultMarginBytes     = int64(25 * 1024 * 1024 * 1024)
	takeoutZipNameNeedle   = "takeout"
	currentWorkflowVersion = 2
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
	MediaFiles        int       `json:"mediaFiles"`
	ModTime           time.Time `json:"modTime"`
	Fingerprint       string    `json:"fingerprint"`
}

type ManifestEntry struct {
	WorkflowVersion  int                     `json:"workflowVersion,omitempty"`
	ZipName          string                  `json:"zipName"`
	ZipPath          string                  `json:"zipPath"`
	ZipSize          int64                   `json:"zipSize"`
	ZipModified      time.Time               `json:"zipModified"`
	ZipFingerprint   string                  `json:"zipFingerprint"`
	SourceDrive      string                  `json:"sourceDrive"`
	Status           string                  `json:"status"`
	StartTime        time.Time               `json:"startTime,omitempty"`
	EndTime          time.Time               `json:"endTime,omitempty"`
	ReportPath       string                  `json:"reportPath,omitempty"`
	WorkRoot         string                  `json:"workRoot,omitempty"`
	ExtractedRoot    string                  `json:"extractedRoot,omitempty"`
	GooglePhotosRoot string                  `json:"googlePhotosRoot,omitempty"`
	StagingRoot      string                  `json:"stagingRoot,omitempty"`
	OutputFolder     string                  `json:"outputFolder"`
	Error            string                  `json:"error,omitempty"`
	Summary          *fixer.RunReportSummary `json:"summary,omitempty"`
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
	ZipRoots           []string
	WorkDir            string
	WorkDirs           []string
	OutputDir          string
	StagingOutputDir   string
	MotionToolPath     string
	RetryFailedMotion  bool
	MotionMergeTimeout time.Duration
	AutoDrives         bool
	AskOnAmbiguous     bool
	KeepTempOnError    bool
	PreflightOnly      bool
	Reprocess          bool
	SafetyMarginBytes  int64
	ProcessOptions     fixer.ProcessOptions
	Prompt             PromptFunc
	Process            ProcessFunc
	Progress           func(BatchProgress)
	StopAfterCurrent   func() bool
}

type Result struct {
	ManifestPath        string
	OutputDir           string
	StagingOutputDir    string
	WorkDir             string
	WorkDirs            []string
	ZipCount            int
	Processed           int
	CompletedWithReview int
	Skipped             int
	Planned             int
	Failed              int
	Stopped             bool
	Preflight           *PreflightReport
	AlbumCleanup        *fixer.AlbumCleanupResult
	MotionMerge         *fixer.MotionMergeReport
}

type BatchProgress struct {
	CurrentZip    string
	CurrentIndex  int
	Phase         string
	Completed     int
	Total         int
	FileProcessed int
	FileTotal     int
	CurrentFile   string
	CurrentBytes  int64
	TotalBytes    int64
	LatestError   string
	ReportPath    string
	WorkRoot      string
}

type WorkRootReport struct {
	Path          string    `json:"path"`
	FreeBytes     int64     `json:"freeBytes"`
	RequiredBytes int64     `json:"requiredBytes"`
	Kind          DriveKind `json:"kind"`
	Usable        bool      `json:"usable"`
	Warning       string    `json:"warning,omitempty"`
}

type PreflightReport struct {
	ZipCount              int              `json:"zipCount"`
	EstimatedMediaFiles   int              `json:"estimatedMediaFiles"`
	TotalZipSize          int64            `json:"totalZipSize"`
	LargestZipBytes       int64            `json:"largestZipBytes"`
	OutputFreeBytes       int64            `json:"outputFreeBytes"`
	StagingFreeBytes      int64            `json:"stagingFreeBytes,omitempty"`
	WorkFreeBytes         int64            `json:"workFreeBytes"`
	EstimatedMinWorkBytes int64            `json:"estimatedMinWorkBytes"`
	OutputDir             string           `json:"outputDir"`
	StagingOutputDir      string           `json:"stagingOutputDir,omitempty"`
	WorkDir               string           `json:"workDir"`
	ZipRoots              []string         `json:"zipRoots"`
	WorkDirs              []string         `json:"workDirs"`
	WorkRoots             []WorkRootReport `json:"workRoots"`
	ManifestPath          string           `json:"manifestPath"`
	StatePath             string           `json:"statePath"`
	Warnings              []string         `json:"warnings,omitempty"`
	ZipPaths              []string         `json:"zipPaths"`
	MotionMergeEnabled    bool             `json:"motionMergeEnabled"`
	MotionPhotoToolFound  bool             `json:"motionPhotoToolFound"`
	MotionPhotoToolPath   string           `json:"motionPhotoToolPath,omitempty"`
	LegacyCompletedZips   int              `json:"legacyCompletedZips,omitempty"`
}
