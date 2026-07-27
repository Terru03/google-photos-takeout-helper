package fixer

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime/debug"
	"strings"
)

var exifToolVersionPattern = regexp.MustCompile(`^\d+(\.\d+)+$`)

type ExifToolInfo struct {
	Path    string
	Version string
}

var exifToolPathOverride string

func SafeGo(name string, fn func()) {
	go func() {
		defer RecoverPanic(name)
		fn()
	}()
}

func RecoverPanic(component string) {
	if recovered := recover(); recovered != nil {
		Log(LoggerError, "Panic recovered in %s: %v\n%s", component, recovered, string(debug.Stack()))
	}
}

func ValidateProcessingDependencies(options ProcessOptions) (*ExifToolInfo, error) {
	if !needsExifTool(options) {
		return nil, nil
	}
	return DetectExifTool()
}

func needsExifTool(options ProcessOptions) bool {
	options = options.Normalized()
	return options.WriteMetadata ||
		options.VerifyWrites ||
		options.RestoreMOVExtension ||
		options.MonthSubfolders ||
		options.AlbumMode == AlbumModeTimelineOnly
}

func DetectExifTool() (*ExifToolInfo, error) {
	path, err := exec.LookPath(getExifToolPath())
	if err != nil {
		return nil, fmt.Errorf("ExifTool is required for metadata, verification, and MOV restoration. Install it or use a bundled release: %w", err)
	}

	output, err := newHiddenCommand(path, "-ver").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ExifTool was found at %s but could not be executed: %w: %s", path, err, strings.TrimSpace(string(output)))
	}

	version := ""
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if exifToolVersionPattern.MatchString(line) {
			version = line
		}
	}
	if version == "" {
		version = strings.TrimSpace(string(output))
	}
	if version == "" {
		return nil, fmt.Errorf("ExifTool was found at %s but returned an empty version string", path)
	}

	return &ExifToolInfo{
		Path:    path,
		Version: version,
	}, nil
}
