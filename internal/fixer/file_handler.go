package fixer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var imageExtensions = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
	".png":  {},
	".heic": {},
}

var videoExtensions = map[string]struct{}{
	".3gp": {},
	".avi": {},
	".m4v": {},
	".mkv": {},
	".mov": {},
	".mp4": {},
}

var yearPartRegexp = regexp.MustCompile(`^\d{4}$`)

func ClearCache() {}

func ClearCacheDir(_ string) {}

func IsVideoFile(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	_, ok := videoExtensions[extension]
	return ok
}

func IsMediaFile(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	_, isImage := imageExtensions[extension]
	_, isVideo := videoExtensions[extension]
	return isImage || isVideo
}

func DuplicateFile(inputPath string, outputPath string) error {
	sourceInfo, err := os.Stat(inputPath)
	if err != nil {
		return err
	}

	sourceFile, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = sourceFile.Close()
	}()

	destFile, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, sourceInfo.Mode())
	if err != nil {
		return err
	}
	defer func() {
		_ = destFile.Close()
	}()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	return destFile.Sync()
}

func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = file.Close()
	}()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func MakeUniquePath(path string) string {
	if !FileExists(path) {
		return path
	}

	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)
	for index := 2; ; index++ {
		candidate := filepath.Join(dir, base+" ("+strconv.Itoa(index)+")"+ext)
		if !FileExists(candidate) {
			return candidate
		}
	}
}

func LinkDuplicate(existingPath string, destPath string, preferSymlink bool) (OperationStatus, error) {
	linkSteps := []func(string, string) error{
		os.Link,
		os.Symlink,
	}
	statuses := []OperationStatus{
		OperationHardlinked,
		OperationSymlinked,
	}
	if preferSymlink {
		linkSteps[0], linkSteps[1] = linkSteps[1], linkSteps[0]
		statuses[0], statuses[1] = statuses[1], statuses[0]
	}

	if err := linkSteps[0](existingPath, destPath); err == nil {
		return statuses[0], nil
	}
	if err := linkSteps[1](existingPath, destPath); err == nil {
		return statuses[1], nil
	}

	if err := DuplicateFile(existingPath, destPath); err != nil {
		return OperationError, err
	}

	return OperationDuplicateCopied, nil
}

func CountProcessableFiles(sourcePath string) (int, error) {
	plans, err := DiscoverMediaPlan(sourcePath, ProcessOptions{})
	if err != nil {
		return 0, err
	}
	if len(plans) == 0 {
		return 0, os.ErrNotExist
	}
	return len(plans), nil
}

func CountJSONSidecars(sourcePath string) (int, error) {
	count := 0
	err := filepath.WalkDir(sourcePath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if strings.EqualFold(d.Name(), ".gtf") || strings.EqualFold(d.Name(), "logs") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".json") {
			count++
		}
		return nil
	})
	return count, err
}

func DetectFileMonth(sourcePath string, sidecarPath string) (int, error) {
	if sidecarPath != "" {
		metadata, err := ReadJSONMetadata(sidecarPath)
		if err == nil {
			if timestamp, err := metadata.BestTimestamp(); err == nil {
				return int(timestamp.UTC().Month()), nil
			}
		}
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		return 0, err
	}
	return int(info.ModTime().Month()), nil
}

func FormatMonthFolderName(month int) string {
	if month < int(time.January) || month > int(time.December) {
		return strconv.Itoa(month)
	}

	return fmt.Sprintf("%d - %s", month, time.Month(month).String())
}

func ResolveOutputDir(outputRoot string, plan MediaPlan, options ProcessOptions) (string, error) {
	if options.Flatten {
		return outputRoot, nil
	}

	targetDir := outputRoot
	if plan.RelativeDir != "" {
		targetDir = filepath.Join(targetDir, plan.RelativeDir)
	}

	if !options.MonthSubfolders {
		return targetDir, nil
	}

	month, err := DetectFileMonth(plan.SourcePath, plan.SidecarPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(targetDir, FormatMonthFolderName(month)), nil
}

func IsYearFolder(dirName string) (bool, error) {
	yearPrefixes := []string{
		"Photos from ",
		"Photos of ",
		"Fotos von ",
		"Photos de ",
		"Foto del ",
		"Fotos de ",
		"Foto's van ",
		"Zdjęcia z ",
		"Фотографии из ",
		"Foton från ",
		"Bilder fra ",
		"Billeder fra ",
		"Fotoğraflar ",
		"Fotografie z ",
		"Fotók a ",
		"Φωτογραφίες από ",
		"Fotografii din ",
		"Foto dari ",
		"รูปภาพจาก ",
		"Ảnh từ ",
	}

	for _, prefix := range yearPrefixes {
		if strings.HasPrefix(dirName, prefix) {
			yearPart := strings.TrimPrefix(dirName, prefix)
			if yearPartRegexp.MatchString(yearPart) {
				return true, nil
			}
		}
	}
	return false, nil
}

func NormalizeTimestampString(value string) string {
	return strings.TrimSpace(value)
}

func TouchWithCaptureTime(path string, captureTime time.Time) error {
	return os.Chtimes(path, captureTime, captureTime)
}
