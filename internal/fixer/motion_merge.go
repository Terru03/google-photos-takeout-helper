package fixer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type MotionMergeOptions struct {
	LibraryRoot string
	ToolPath    string
	RetryFailed bool
	Timeout     time.Duration
	Progress    func(MotionMergeProgress)
}

type MotionMergeProgress struct {
	Processed int
	Total     int
	StillPath string
	VideoPath string
	Status    string
}

type MotionMergeItem struct {
	Key             string        `json:"key"`
	Status          string        `json:"status"`
	SourceStillPath string        `json:"sourceStillPath,omitempty"`
	SourceVideoPath string        `json:"sourceVideoPath,omitempty"`
	OutputPath      string        `json:"outputPath,omitempty"`
	Error           string        `json:"error,omitempty"`
	Duration        time.Duration `json:"duration"`
	UpdatedAt       time.Time     `json:"updatedAt"`
}

type MotionMergeReport struct {
	StartedAt               time.Time         `json:"startedAt"`
	FinishedAt              time.Time         `json:"finishedAt"`
	LibraryRoot             string            `json:"libraryRoot"`
	ToolPath                string            `json:"toolPath"`
	ReportPath              string            `json:"reportPath"`
	StatePath               string            `json:"statePath"`
	TotalCandidatePairs     int               `json:"totalCandidatePairs"`
	MergedSuccessfully      int               `json:"mergedSuccessfully"`
	SkippedAlreadyMerged    int               `json:"skippedAlreadyMerged"`
	SkippedPreviousFailures int               `json:"skippedPreviousFailures"`
	SkippedMissingStill     int               `json:"skippedMissingStill"`
	SkippedMissingVideo     int               `json:"skippedMissingVideo"`
	FailedMotionPhotoCalls  int               `json:"failedMotionPhoto2Calls"`
	TimedOutMerges          int               `json:"timedOutMerges"`
	Items                   []MotionMergeItem `json:"items"`
}

type motionMergeCandidate struct {
	key       string
	stillPath string
	videoPath string
}

const (
	motionMergeStatusMerged          = "merged"
	motionMergeStatusAlreadyMerged   = "already-merged"
	motionMergeStatusMissingStill    = "missing-still"
	motionMergeStatusMissingVideo    = "missing-video"
	motionMergeStatusFailed          = "failed"
	motionMergeStatusTimedOut        = "timed-out"
	motionMergeStatusPreviousFailure = "skipped-previous-failure"
)

func MergeMotionLibrary(ctx context.Context, options MotionMergeOptions) (MotionMergeReport, error) {
	started := time.Now().UTC()
	libraryRoot, err := filepath.Abs(strings.TrimSpace(options.LibraryRoot))
	if err != nil {
		return MotionMergeReport{}, err
	}
	info, err := os.Stat(libraryRoot)
	if err != nil {
		return MotionMergeReport{}, fmt.Errorf("motion merge library: %w", err)
	}
	if !info.IsDir() {
		return MotionMergeReport{}, fmt.Errorf("motion merge library must be folder")
	}
	if options.Timeout <= 0 {
		options.Timeout = 2 * time.Minute
	}

	toolPath, err := ResolveMotionPhotoTool(options.ToolPath)
	if err != nil {
		return MotionMergeReport{}, err
	}
	reportDir := filepath.Join(libraryRoot, ".gtf", "reports")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return MotionMergeReport{}, err
	}
	report := MotionMergeReport{
		StartedAt:   started,
		LibraryRoot: libraryRoot,
		ToolPath:    toolPath,
		ReportPath:  filepath.Join(reportDir, "motion-merge-report.json"),
		StatePath:   filepath.Join(libraryRoot, ".gtf", "motion-merge-state.jsonl"),
	}

	candidates, err := discoverMotionMergeCandidates(libraryRoot)
	if err != nil {
		return report, err
	}
	report.TotalCandidatePairs = len(candidates)
	previous := readMotionMergeState(report.StatePath)
	stateFile, err := os.OpenFile(report.StatePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return report, err
	}
	stateWriter := bufio.NewWriterSize(stateFile, 128*1024)
	defer func() {
		_ = stateWriter.Flush()
		_ = stateFile.Close()
	}()

	for index, candidate := range candidates {
		if ctx.Err() != nil {
			return report, ctx.Err()
		}
		item := MotionMergeItem{
			Key:             candidate.key,
			SourceStillPath: candidate.stillPath,
			SourceVideoPath: candidate.videoPath,
			OutputPath:      candidate.stillPath,
			UpdatedAt:       time.Now().UTC(),
		}

		if previousItem, ok := previous[candidate.key]; ok {
			switch previousItem.Status {
			case motionMergeStatusMerged, motionMergeStatusAlreadyMerged:
				item.Status = motionMergeStatusAlreadyMerged
				report.SkippedAlreadyMerged++
				report.Items = append(report.Items, item)
				emitMotionMergeProgress(options, index+1, len(candidates), item)
				continue
			case motionMergeStatusFailed, motionMergeStatusTimedOut:
				if !options.RetryFailed {
					item.Status = motionMergeStatusPreviousFailure
					item.Error = previousItem.Error
					report.SkippedPreviousFailures++
					report.Items = append(report.Items, item)
					emitMotionMergeProgress(options, index+1, len(candidates), item)
					continue
				}
			}
		}

		switch {
		case candidate.stillPath == "" || !FileExists(candidate.stillPath):
			item.Status = motionMergeStatusMissingStill
			report.SkippedMissingStill++
		case candidate.videoPath == "" || !FileExists(candidate.videoPath):
			item.Status = motionMergeStatusMissingVideo
			report.SkippedMissingVideo++
		default:
			runStarted := time.Now()
			pairCtx, cancel := context.WithTimeout(ctx, options.Timeout)
			output, runErr := newHiddenCommandContext(
				pairCtx,
				toolPath,
				"--input-image", candidate.stillPath,
				"--input-video", candidate.videoPath,
				"--overwrite",
			).CombinedOutput()
			cancel()
			item.Duration = time.Since(runStarted)
			outputText := strings.TrimSpace(string(output))
			logMotionPhotoOutput(outputText)
			switch {
			case errors.Is(pairCtx.Err(), context.DeadlineExceeded):
				item.Status = motionMergeStatusTimedOut
				item.Error = fmt.Sprintf("motionphoto2 timed out after %s", options.Timeout)
				report.TimedOutMerges++
			case isAlreadyMotionPhotoOutput(outputText):
				item.Status = motionMergeStatusAlreadyMerged
				report.SkippedAlreadyMerged++
			case runErr != nil:
				item.Status = motionMergeStatusFailed
				if outputText == "" {
					item.Error = runErr.Error()
				} else {
					item.Error = fmt.Sprintf("%v: %s", runErr, outputText)
				}
				report.FailedMotionPhotoCalls++
			default:
				item.Status = motionMergeStatusMerged
				report.MergedSuccessfully++
			}
		}

		item.UpdatedAt = time.Now().UTC()
		report.Items = append(report.Items, item)
		if err := appendMotionMergeState(stateWriter, item); err != nil {
			return report, err
		}
		if (index+1)%32 == 0 {
			if err := stateWriter.Flush(); err != nil {
				return report, err
			}
		}
		emitMotionMergeProgress(options, index+1, len(candidates), item)
	}

	if err := stateWriter.Flush(); err != nil {
		return report, err
	}
	if err := stateFile.Sync(); err != nil {
		return report, err
	}
	report.FinishedAt = time.Now().UTC()
	if err := writeMotionMergeReport(report); err != nil {
		return report, err
	}
	return report, nil
}

func ResolveMotionPhotoTool(requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		info, err := DetectMotionPhotoTool()
		if err != nil {
			return "", err
		}
		return info.Path, nil
	}
	path, err := exec.LookPath(requested)
	if err != nil {
		return "", fmt.Errorf("MotionPhoto2 not found at %s: %w", requested, err)
	}
	if err := validateMotionPhotoTool(path); err != nil {
		return "", err
	}
	return path, nil
}

func discoverMotionMergeCandidates(libraryRoot string) ([]motionMergeCandidate, error) {
	candidates := make([]motionMergeCandidate, 0)
	seenPaths := make(map[string]struct{})

	stateStore, err := OpenStateStore(filepath.Join(libraryRoot, ".gtf", "state.jsonl"))
	if err == nil {
		records := stateStore.Records()
		_ = stateStore.Close()
		byKey := make(map[string]ProcessRecord, len(records))
		for _, record := range records {
			byKey[record.StateKey()] = record
		}
		for _, record := range records {
			if record.PartnerRelPath == "" {
				continue
			}
			partnerKey := record.PartnerRelPath
			if record.SourceID != "" {
				partnerKey = record.SourceID + "|" + record.PartnerRelPath
			}
			partner, ok := byKey[partnerKey]
			if !ok || IsVideoFile(record.SourceRelPath) == IsVideoFile(partner.SourceRelPath) {
				continue
			}
			stillRecord, videoRecord := record, partner
			if IsVideoFile(record.SourceRelPath) {
				stillRecord, videoRecord = partner, record
			}
			stillPath := expectedMotionOutputPath(stillRecord, videoRecord)
			videoPath := expectedMotionOutputPath(videoRecord, stillRecord)
			pathKey := motionPairPathKey(stillPath, videoPath)
			if _, ok := seenPaths[pathKey]; ok {
				continue
			}
			seenPaths[pathKey] = struct{}{}
			candidates = append(candidates, motionMergeCandidate{
				key:       motionPairStateKey(stillRecord, videoRecord),
				stillPath: stillPath,
				videoPath: videoPath,
			})
		}
	}

	type bucket struct {
		images []string
		videos []string
	}
	buckets := make(map[string]*bucket)
	err = filepath.WalkDir(libraryRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if strings.EqualFold(entry.Name(), ".gtf") {
				return filepath.SkipDir
			}
			return nil
		}
		if !IsMediaFile(path) {
			return nil
		}
		stem := normalizedMediaStem(filepath.Base(path))
		if stem == "" {
			return nil
		}
		key := strings.ToLower(filepath.Clean(filepath.Dir(path))) + "|" + stem
		group := buckets[key]
		if group == nil {
			group = &bucket{}
			buckets[key] = group
		}
		if IsVideoFile(path) {
			group.videos = append(group.videos, path)
		} else {
			group.images = append(group.images, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, group := range buckets {
		sort.Strings(group.images)
		sort.Strings(group.videos)
		pairs := len(group.images)
		if len(group.videos) < pairs {
			pairs = len(group.videos)
		}
		for index := 0; index < pairs; index++ {
			stillPath := group.images[index]
			videoPath := group.videos[index]
			pathKey := motionPairPathKey(stillPath, videoPath)
			if _, ok := seenPaths[pathKey]; ok {
				continue
			}
			seenPaths[pathKey] = struct{}{}
			candidates = append(candidates, motionMergeCandidate{
				key:       "path|" + pathKey,
				stillPath: stillPath,
				videoPath: videoPath,
			})
		}
	}
	sort.SliceStable(candidates, func(i int, j int) bool {
		return candidates[i].key < candidates[j].key
	})
	return candidates, nil
}

func expectedMotionOutputPath(record ProcessRecord, partner ProcessRecord) string {
	if record.OutputPath != "" {
		return record.OutputPath
	}
	if partner.OutputPath != "" {
		return filepath.Join(filepath.Dir(partner.OutputPath), filepath.Base(record.SourceRelPath))
	}
	return record.SourcePath
}

func motionPairStateKey(still ProcessRecord, video ProcessRecord) string {
	return "state|" + strings.ToLower(still.SourceID+"|"+still.SourceRelPath+"|"+video.SourceRelPath)
}

func motionPairPathKey(stillPath string, videoPath string) string {
	return strings.ToLower(filepath.Clean(stillPath) + "|" + filepath.Clean(videoPath))
}

func readMotionMergeState(path string) map[string]MotionMergeItem {
	latest := make(map[string]MotionMergeItem)
	file, err := os.Open(path)
	if err != nil {
		return latest
	}
	defer func() {
		_ = file.Close()
	}()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var item MotionMergeItem
		if json.Unmarshal(scanner.Bytes(), &item) == nil && item.Key != "" {
			latest[item.Key] = item
		}
	}
	return latest
}

func appendMotionMergeState(writer *bufio.Writer, item MotionMergeItem) error {
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	_, err = writer.Write(append(data, '\n'))
	return err
}

func writeMotionMergeReport(report MotionMergeReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(report.ReportPath, data, 0o644)
}

func emitMotionMergeProgress(options MotionMergeOptions, processed int, total int, item MotionMergeItem) {
	if options.Progress == nil {
		return
	}
	options.Progress(MotionMergeProgress{
		Processed: processed,
		Total:     total,
		StillPath: item.SourceStillPath,
		VideoPath: item.SourceVideoPath,
		Status:    item.Status,
	})
}
