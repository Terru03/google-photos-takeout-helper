package fixer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type MotionPhotoToolInfo struct {
	Path string `json:"path"`
}

type MotionPhotoPassStatus string

const (
	MotionPhotoPassStatusSkippedDisabled MotionPhotoPassStatus = "skipped-disabled"
	MotionPhotoPassStatusSkippedDryRun   MotionPhotoPassStatus = "skipped-dry-run"
	MotionPhotoPassStatusCompleted       MotionPhotoPassStatus = "completed"
	MotionPhotoPassStatusFailed          MotionPhotoPassStatus = "failed"
)

type MotionPhotoPassResult struct {
	Enabled                   bool                  `json:"enabled"`
	Status                    MotionPhotoPassStatus `json:"status,omitempty"`
	ToolPath                  string                `json:"toolPath,omitempty"`
	StandaloneVideoCandidates int                   `json:"standaloneVideoCandidates,omitempty"`
	StandaloneVideosDeleted   int                   `json:"standaloneVideosDeleted,omitempty"`
	StandaloneVideosSkipped   int                   `json:"standaloneVideosSkipped,omitempty"`
	CleanupErrors             int                   `json:"cleanupErrors,omitempty"`
	Error                     string                `json:"error,omitempty"`
}

var motionPhotoToolPathOverride string

type motionPhotoCleanupTarget struct {
	videoOutputPath          string
	imageOutputPath          string
	imageHashBefore          string
	deleteWithoutImageChange bool
}

func getMotionPhotoToolPath() string {
	if strings.TrimSpace(motionPhotoToolPathOverride) != "" {
		return motionPhotoToolPathOverride
	}

	toolName := "motionphoto2"
	if exePath, err := os.Executable(); err == nil && strings.EqualFold(filepath.Ext(exePath), ".exe") {
		toolName = "motionphoto2.exe"
	}

	candidates := make([]string, 0, 3)
	exePath, err := os.Executable()
	if err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exePath), toolName))
	}
	if workingDir, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(workingDir, toolName),
			filepath.Join(workingDir, "dist", toolName),
		)
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		cleanCandidate := strings.ToLower(filepath.Clean(candidate))
		if _, exists := seen[cleanCandidate]; exists {
			continue
		}
		seen[cleanCandidate] = struct{}{}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return "motionphoto2"
}

func DetectMotionPhotoTool() (*MotionPhotoToolInfo, error) {
	candidate := getMotionPhotoToolPath()
	if filepath.IsAbs(candidate) {
		if _, err := os.Stat(candidate); err == nil {
			return &MotionPhotoToolInfo{Path: candidate}, nil
		}
	}

	path, err := exec.LookPath(candidate)
	if err != nil {
		return nil, fmt.Errorf("MotionPhoto2 is required to create Windows-viewable motion photos. Place motionphoto2 next to GoogleTakeoutFixer or add it to PATH: %w", err)
	}

	return &MotionPhotoToolInfo{Path: path}, nil
}

func ValidateMotionPhotoDependencies(options ProcessOptions) (*MotionPhotoToolInfo, error) {
	options = options.Normalized()
	if !options.CreateMotionPhotos || options.DryRun {
		return nil, nil
	}

	return DetectMotionPhotoTool()
}

func RunMotionPhotoPass(targets []motionPhotoCleanupTarget, options ProcessOptions) MotionPhotoPassResult {
	options = options.Normalized()

	result := MotionPhotoPassResult{
		Enabled: options.CreateMotionPhotos,
		Status:  MotionPhotoPassStatusSkippedDisabled,
	}
	if !options.CreateMotionPhotos {
		return result
	}
	if options.DryRun {
		result.Status = MotionPhotoPassStatusSkippedDryRun
		return result
	}

	info, err := DetectMotionPhotoTool()
	if err != nil {
		result.Status = MotionPhotoPassStatusFailed
		result.Error = err.Error()
		return result
	}
	result.ToolPath = info.Path

	if len(targets) == 0 {
		result.Status = MotionPhotoPassStatusCompleted
		return result
	}

	failures := make([]string, 0)
	for index := range targets {
		if err := runMotionPhotoPair(info.Path, &targets[index]); err != nil {
			failures = append(failures, err.Error())
		}
	}

	if len(failures) > 0 {
		result.Status = MotionPhotoPassStatusFailed
		if len(failures) == 1 {
			result.Error = failures[0]
		} else {
			result.Error = fmt.Sprintf("%d motion photo pair(s) failed; first error: %s", len(failures), failures[0])
		}
		return result
	}

	result.Status = MotionPhotoPassStatusCompleted
	return result
}

func runMotionPhotoPair(toolPath string, target *motionPhotoCleanupTarget) error {
	if !FileExists(target.imageOutputPath) {
		return fmt.Errorf("motion photo input image missing: %s", target.imageOutputPath)
	}
	if !FileExists(target.videoOutputPath) {
		return fmt.Errorf("motion photo input video missing: %s", target.videoOutputPath)
	}

	args := []string{
		"--input-image", target.imageOutputPath,
		"--input-video", target.videoOutputPath,
		"--output-file", target.imageOutputPath,
		"--overwrite",
	}

	output, err := newHiddenCommand(toolPath, args...).CombinedOutput()
	outputText := strings.TrimSpace(string(output))
	logMotionPhotoOutput(outputText)

	if isAlreadyMotionPhotoOutput(outputText) {
		target.deleteWithoutImageChange = true
		return nil
	}

	if err != nil {
		if outputText == "" {
			outputText = err.Error()
		} else {
			outputText = fmt.Sprintf("%v: %s", err, outputText)
		}
		return fmt.Errorf("%s + %s: %s", target.imageOutputPath, target.videoOutputPath, outputText)
	}

	return nil
}

func logMotionPhotoOutput(output string) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		Log(LoggerInfo, "MotionPhoto2: %s", line)
	}
}

func isAlreadyMotionPhotoOutput(output string) bool {
	return strings.Contains(strings.ToLower(output), "already a motion photo")
}

func BuildMotionPhotoCleanupTargets(plans []MediaPlan, stateStore *StateStore) []motionPhotoCleanupTarget {
	planByRelPath := make(map[string]MediaPlan, len(plans))
	for _, plan := range plans {
		planByRelPath[plan.RelativePath] = plan
	}

	targets := make([]motionPhotoCleanupTarget, 0)
	seen := make(map[string]struct{})
	for _, plan := range plans {
		if !plan.IsVideo || plan.PartnerRelPath == "" {
			continue
		}

		partnerPlan, ok := planByRelPath[plan.PartnerRelPath]
		if !ok || partnerPlan.IsVideo {
			continue
		}

		videoRecord, ok := stateStore.Get(plan.RelativePath)
		if !ok || !videoRecord.Successful() || videoRecord.OutputPath == "" || !FileExists(videoRecord.OutputPath) {
			continue
		}

		imageRecord, ok := stateStore.Get(plan.PartnerRelPath)
		if !ok || !imageRecord.Successful() || imageRecord.OutputPath == "" || !FileExists(imageRecord.OutputPath) {
			continue
		}

		imageHashBefore, err := HashFile(imageRecord.OutputPath)
		if err != nil {
			Log(LoggerWarn, "Skip live video cleanup for %s: cannot hash image: %v", imageRecord.OutputPath, err)
			continue
		}

		key := strings.ToLower(filepath.Clean(videoRecord.OutputPath)) + "|" + strings.ToLower(filepath.Clean(imageRecord.OutputPath))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		targets = append(targets, motionPhotoCleanupTarget{
			videoOutputPath: videoRecord.OutputPath,
			imageOutputPath: imageRecord.OutputPath,
			imageHashBefore: imageHashBefore,
		})
	}

	return targets
}

func CleanupEmbeddedMotionPhotoVideos(targets []motionPhotoCleanupTarget) (int, int, int) {
	deleted := 0
	skipped := 0
	cleanupErrors := 0

	for _, target := range targets {
		if !FileExists(target.videoOutputPath) || !FileExists(target.imageOutputPath) {
			skipped++
			continue
		}

		imageHashAfter, err := HashFile(target.imageOutputPath)
		if err != nil {
			cleanupErrors++
			Log(LoggerWarn, "Skip live video cleanup for %s: cannot hash image after motion pass: %v", target.imageOutputPath, err)
			continue
		}
		if imageHashAfter == target.imageHashBefore && !target.deleteWithoutImageChange {
			skipped++
			continue
		}

		if err := os.Remove(target.videoOutputPath); err != nil {
			cleanupErrors++
			Log(LoggerWarn, "Cannot delete live video copy %s: %v", target.videoOutputPath, err)
			continue
		}

		deleted++
		Log(LoggerInfo, "Delete live video copy after embed: %s", target.videoOutputPath)
	}

	return deleted, skipped, cleanupErrors
}

func ShouldCleanupEmbeddedMotionPhotoVideos(result MotionPhotoPassResult) bool {
	if !result.Enabled || result.ToolPath == "" {
		return false
	}

	switch result.Status {
	case MotionPhotoPassStatusSkippedDisabled, MotionPhotoPassStatusSkippedDryRun:
		return false
	default:
		return true
	}
}
