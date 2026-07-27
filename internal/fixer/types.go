package fixer

import (
	"fmt"
	"time"
)

type Progress struct {
	Total     int
	Processed int
	Current   string
}

type ConflictPolicy string

const (
	ConflictPreferJSON     ConflictPolicy = "prefer-json"
	ConflictPreferEmbedded ConflictPolicy = "prefer-embedded"
	ConflictMerge          ConflictPolicy = "merge"
)

func ParseConflictPolicy(value string) (ConflictPolicy, error) {
	switch ConflictPolicy(value) {
	case "", ConflictPreferJSON:
		return ConflictPreferJSON, nil
	case ConflictPreferEmbedded:
		return ConflictPreferEmbedded, nil
	case ConflictMerge:
		return ConflictMerge, nil
	default:
		return "", fmt.Errorf("unknown conflict policy %q", value)
	}
}

type ProcessOptions struct {
	UseSymlinks              bool
	WriteMetadata            bool
	WriteXMPSidecars         bool
	MonthSubfolders          bool
	AlbumMode                AlbumMode
	IgnoreAlbums             bool
	Flatten                  bool
	CreateMotionPhotos       bool
	KeepLiveVideo            bool
	DeleteSourceAfterSuccess bool
	RestoreMOVExtension      bool
	Deduplicate              bool
	DryRun                   bool
	VerifyWrites             bool
	ConflictPolicy           ConflictPolicy
	Verbose                  bool
	RuntimeRoot              string `json:"-"`
	FinalOutputRoot          string `json:"-"`
	SourceID                 string `json:"-"`
}

func DefaultProcessOptions() ProcessOptions {
	return ProcessOptions{
		WriteMetadata:       true,
		RestoreMOVExtension: true,
		Deduplicate:         true,
		VerifyWrites:        true,
		ConflictPolicy:      ConflictMerge,
		AlbumMode:           AlbumModeUniqueOnly,
	}
}

func (o ProcessOptions) Normalized() ProcessOptions {
	if o.ConflictPolicy == "" {
		o.ConflictPolicy = ConflictPreferJSON
	}
	if o.AlbumMode == "" {
		if o.IgnoreAlbums {
			o.AlbumMode = AlbumModeTimelineOnly
		} else {
			o.AlbumMode = AlbumModeAll
		}
	}
	switch o.AlbumMode {
	case AlbumModeTimelineOnly:
		o.IgnoreAlbums = true
	case AlbumModeUniqueOnly, AlbumModeAll:
		o.IgnoreAlbums = false
	default:
		o.AlbumMode = AlbumModeAll
		o.IgnoreAlbums = false
	}
	if !o.WriteMetadata {
		o.VerifyWrites = false
	}
	return o
}

type AlbumMode string

const (
	AlbumModeAll          AlbumMode = "all"
	AlbumModeTimelineOnly AlbumMode = "timeline-only"
	AlbumModeUniqueOnly   AlbumMode = "unique-only"
)

func ParseAlbumMode(value string) (AlbumMode, error) {
	switch AlbumMode(value) {
	case "", AlbumModeUniqueOnly:
		return AlbumModeUniqueOnly, nil
	case AlbumModeAll:
		return AlbumModeAll, nil
	case AlbumModeTimelineOnly:
		return AlbumModeTimelineOnly, nil
	default:
		return "", fmt.Errorf("unknown album mode %q", value)
	}
}

type MetadataOutputMode string

const (
	MetadataOutputFile MetadataOutputMode = "file"
	MetadataOutputXMP  MetadataOutputMode = "xmp"
	MetadataOutputBoth MetadataOutputMode = "both"
	MetadataOutputNone MetadataOutputMode = "none"
)

func ParseMetadataOutputMode(value string) (MetadataOutputMode, error) {
	switch MetadataOutputMode(value) {
	case "", MetadataOutputFile:
		return MetadataOutputFile, nil
	case MetadataOutputXMP:
		return MetadataOutputXMP, nil
	case MetadataOutputBoth:
		return MetadataOutputBoth, nil
	case MetadataOutputNone:
		return MetadataOutputNone, nil
	default:
		return "", fmt.Errorf("unknown metadata output mode %q", value)
	}
}

func (mode MetadataOutputMode) WritesFiles() bool {
	return mode == MetadataOutputFile || mode == MetadataOutputBoth
}

func (mode MetadataOutputMode) WritesXMPSidecars() bool {
	return mode == MetadataOutputXMP || mode == MetadataOutputBoth
}

func MetadataOutputModeForOptions(options ProcessOptions) MetadataOutputMode {
	switch {
	case options.WriteMetadata && options.WriteXMPSidecars:
		return MetadataOutputBoth
	case options.WriteXMPSidecars:
		return MetadataOutputXMP
	case options.WriteMetadata:
		return MetadataOutputFile
	default:
		return MetadataOutputNone
	}
}

type MatchStatus string

const (
	MatchStatusMatched   MatchStatus = "matched"
	MatchStatusUnmatched MatchStatus = "unmatched"
	MatchStatusAmbiguous MatchStatus = "ambiguous"
)

type MatchStrategy string

const (
	MatchStrategyExactName      MatchStrategy = "exact-name"
	MatchStrategyJSONTitle      MatchStrategy = "json-title"
	MatchStrategyNormalizedName MatchStrategy = "normalized-name"
	MatchStrategyPrefix         MatchStrategy = "prefix"
	MatchStrategyPartner        MatchStrategy = "partner-sidecar"
)

type OperationStatus string

const (
	OperationSkippedResume      OperationStatus = "skipped-resume"
	OperationSkippedExisting    OperationStatus = "skipped-existing"
	OperationDryRun             OperationStatus = "dry-run"
	OperationCopied             OperationStatus = "copied"
	OperationCopiedWithMetadata OperationStatus = "copied-with-metadata"
	OperationCopiedWithoutMeta  OperationStatus = "copied-without-metadata"
	OperationHardlinked         OperationStatus = "hard-linked"
	OperationSymlinked          OperationStatus = "symlinked"
	OperationDuplicateCopied    OperationStatus = "duplicate-copied"
	OperationError              OperationStatus = "error"
)

type MediaPlan struct {
	SourcePath        string
	RelativePath      string
	RelativeDir       string
	TopLevelDir       string
	FileName          string
	OutputName        string
	SidecarPath       string
	PartnerPath       string
	PartnerRelPath    string
	MatchStatus       MatchStatus
	MatchStrategy     MatchStrategy
	MatchCandidates   []string
	MatchScore        int
	IsYearFolder      bool
	IsVideo           bool
	Metadata          *imageMetadata
	MetadataLoadError string
}

type MetadataFieldConflict struct {
	Field         string `json:"field"`
	JSONValue     string `json:"jsonValue,omitempty"`
	EmbeddedValue string `json:"embeddedValue,omitempty"`
}

type ProcessRecord struct {
	SourceID           string                  `json:"sourceId,omitempty"`
	SourcePath         string                  `json:"sourcePath"`
	SourceRelPath      string                  `json:"sourceRelPath"`
	SourceHash         string                  `json:"sourceHash,omitempty"`
	SourceSize         int64                   `json:"sourceSize,omitempty"`
	SidecarPath        string                  `json:"sidecarPath,omitempty"`
	PartnerPath        string                  `json:"partnerPath,omitempty"`
	PartnerRelPath     string                  `json:"partnerRelPath,omitempty"`
	OutputPath         string                  `json:"outputPath,omitempty"`
	StagedPath         string                  `json:"stagedPath,omitempty"`
	MatchStatus        MatchStatus             `json:"matchStatus"`
	MatchStrategy      MatchStrategy           `json:"matchStrategy,omitempty"`
	MatchCandidates    []string                `json:"matchCandidates,omitempty"`
	UsedPartnerSidecar bool                    `json:"usedPartnerSidecar,omitempty"`
	Status             OperationStatus         `json:"status"`
	DuplicateOf        string                  `json:"duplicateOf,omitempty"`
	MetadataWritten    bool                    `json:"metadataWritten,omitempty"`
	MetadataVerified   bool                    `json:"metadataVerified,omitempty"`
	UsedXMPSidecar     bool                    `json:"usedXMPSidecar,omitempty"`
	Conflicts          []MetadataFieldConflict `json:"conflicts,omitempty"`
	Error              string                  `json:"error,omitempty"`
	UpdatedAt          time.Time               `json:"updatedAt"`
}

func (r ProcessRecord) StateKey() string {
	if r.SourceID == "" {
		return r.SourceRelPath
	}
	return r.SourceID + "|" + r.SourceRelPath
}

func (r ProcessRecord) Successful() bool {
	switch r.Status {
	case OperationSkippedResume, OperationSkippedExisting, OperationDryRun, OperationCopied,
		OperationCopiedWithMetadata, OperationCopiedWithoutMeta, OperationHardlinked,
		OperationSymlinked, OperationDuplicateCopied:
		return true
	default:
		return false
	}
}
