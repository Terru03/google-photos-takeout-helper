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
var yearSuffixRegexp = regexp.MustCompile(`(\d{4})$`)
var filenameDateRegexp = regexp.MustCompile(`([12][0-9]{3})[-_.]?([01][0-9])[-_.]?([0-3][0-9])`)

type folderDateSelection struct {
	Time       time.Time
	Source     string
	MonthKnown bool
}

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

func DetectActualImageExtension(path string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer func() {
		_ = file.Close()
	}()

	var header [32]byte
	count, err := io.ReadFull(file, header[:])
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", false
	}
	data := header[:count]

	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return ".jpg", true
	}
	if len(data) >= 8 &&
		data[0] == 0x89 &&
		string(data[1:4]) == "PNG" &&
		data[4] == 0x0d &&
		data[5] == 0x0a &&
		data[6] == 0x1a &&
		data[7] == 0x0a {
		return ".png", true
	}
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		switch strings.ToLower(string(data[8:12])) {
		case "heic", "heix", "hevc", "hevx", "heim", "heis", "hevm", "hevs":
			return ".heic", true
		}
	}

	return "", false
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

	return nil
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
	selection, err := selectFolderDate(MediaPlan{
		SourcePath:  sourcePath,
		SidecarPath: sidecarPath,
	})
	if err != nil {
		return 0, err
	}
	return int(selection.Time.UTC().Month()), nil
}

func selectFolderDate(plan MediaPlan) (folderDateSelection, error) {
	if timestamp, ok := planPhotoTakenTimestamp(plan); ok {
		return folderDateSelection{
			Time:       timestamp,
			Source:     "google-sidecar-photoTakenTime",
			MonthKnown: true,
		}, nil
	}

	if embedded, err := ReadEmbeddedMetadata(plan.SourcePath); err == nil && !embedded.CaptureTime.IsZero() {
		source := "embedded"
		if embedded.CaptureSource != "" {
			source += ":" + embedded.CaptureSource
		}
		return folderDateSelection{
			Time:       embedded.CaptureTime,
			Source:     source,
			MonthKnown: true,
		}, nil
	}

	timelineYear, hasTimelineYear := timelineFolderYear(plan)
	if filenameDate, ok := dateFromFilename(plan); ok && (!hasTimelineYear || filenameDate.Year() == timelineYear) {
		return folderDateSelection{
			Time:       filenameDate,
			Source:     "filename-date",
			MonthKnown: true,
		}, nil
	}

	if hasTimelineYear {
		return folderDateSelection{
			Time:       time.Date(timelineYear, time.January, 1, 12, 0, 0, 0, time.UTC),
			Source:     "google-timeline-folder-year",
			MonthKnown: false,
		}, nil
	}

	sourcePath := plan.SourcePath
	info, err := os.Stat(sourcePath)
	if err != nil {
		return folderDateSelection{}, err
	}
	return folderDateSelection{
		Time:       info.ModTime(),
		Source:     "file-modified",
		MonthKnown: true,
	}, nil
}

func timelineFolderYear(plan MediaPlan) (int, bool) {
	dirName := strings.TrimSpace(plan.TopLevelDir)
	if dirName == "" {
		relativePath := filepath.ToSlash(plan.RelativePath)
		if separator := strings.Index(relativePath, "/"); separator >= 0 {
			dirName = relativePath[:separator]
		}
	}
	if dirName == "" {
		return 0, false
	}
	isYearFolder, err := IsYearFolder(dirName)
	if err != nil || !isYearFolder {
		return 0, false
	}
	match := yearSuffixRegexp.FindStringSubmatch(dirName)
	if len(match) != 2 {
		return 0, false
	}
	year, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}
	return year, true
}

func dateFromFilename(plan MediaPlan) (time.Time, bool) {
	names := []string{
		plan.OutputName,
		plan.FileName,
		filepath.Base(plan.SourcePath),
	}
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		for _, indexes := range filenameDateRegexp.FindAllStringSubmatchIndex(name, -1) {
			if len(indexes) != 8 {
				continue
			}
			start := indexes[0]
			end := indexes[1]
			if start > 0 && name[start-1] >= '0' && name[start-1] <= '9' {
				continue
			}
			if end < len(name) && name[end] >= '0' && name[end] <= '9' {
				continue
			}

			year, yearErr := strconv.Atoi(name[indexes[2]:indexes[3]])
			month, monthErr := strconv.Atoi(name[indexes[4]:indexes[5]])
			day, dayErr := strconv.Atoi(name[indexes[6]:indexes[7]])
			if yearErr != nil || monthErr != nil || dayErr != nil {
				continue
			}
			candidate := time.Date(year, time.Month(month), day, 12, 0, 0, 0, time.UTC)
			if candidate.Year() == year && int(candidate.Month()) == month && candidate.Day() == day {
				return candidate, true
			}
		}
	}
	return time.Time{}, false
}

func planPhotoTakenTimestamp(plan MediaPlan) (time.Time, bool) {
	if plan.Metadata != nil {
		if timestamp, err := plan.Metadata.PhotoTakenTimestamp(); err == nil {
			return timestamp, true
		}
	}
	if plan.SidecarPath == "" {
		return time.Time{}, false
	}
	metadata, err := ReadJSONMetadata(plan.SidecarPath)
	if err != nil {
		return time.Time{}, false
	}
	timestamp, err := metadata.PhotoTakenTimestamp()
	if err != nil {
		return time.Time{}, false
	}
	return timestamp, true
}

func FormatMonthFolderName(month int) string {
	if month < int(time.January) || month > int(time.December) {
		return strconv.Itoa(month)
	}

	return fmt.Sprintf("%d - %s", month, time.Month(month).String())
}

func ResolveOutputDir(outputRoot string, plan MediaPlan, options ProcessOptions) (string, error) {
	dir, _, err := resolveOutputDirWithDateSource(outputRoot, plan, options)
	return dir, err
}

func resolveOutputDirWithDateSource(outputRoot string, plan MediaPlan, options ProcessOptions) (string, folderDateSelection, error) {
	options = options.Normalized()
	if options.Flatten {
		return outputRoot, folderDateSelection{}, nil
	}

	if !plan.IsYearFolder {
		targetDir := filepath.Join(outputRoot, "Albums")
		if plan.RelativeDir != "" {
			targetDir = filepath.Join(targetDir, plan.RelativeDir)
		}
		return targetDir, folderDateSelection{}, nil
	}

	needsDateLayout := options.MonthSubfolders || options.AlbumMode == AlbumModeTimelineOnly
	if !needsDateLayout {
		targetDir := outputRoot
		if plan.RelativeDir != "" {
			targetDir = filepath.Join(targetDir, plan.RelativeDir)
		}
		return targetDir, folderDateSelection{}, nil
	}

	selection, err := selectFolderDate(plan)
	if err != nil {
		return "", folderDateSelection{}, err
	}
	targetDir := filepath.Join(outputRoot, fmt.Sprintf("Photos from %04d", selection.Time.UTC().Year()))
	if !selection.MonthKnown {
		return filepath.Join(targetDir, "Unknown month"), selection, nil
	}
	month := int(selection.Time.UTC().Month())
	return filepath.Join(targetDir, FormatMonthFolderName(month)), selection, nil
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
