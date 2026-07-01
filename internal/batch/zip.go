package batch

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
)

type ExtractProgress struct {
	ProcessedFiles int
	TotalFiles     int
	CurrentFile    string
	CurrentBytes   int64
	TotalBytes     int64
}

func FindTakeoutZips(roots []string) ([]ZipItem, error) {
	var items []ZipItem
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}

		info, err := os.Stat(root)
		if err != nil {
			return nil, fmt.Errorf("zip root %s: %w", root, err)
		}

		if !info.IsDir() {
			if looksLikeCompleteZip(root) {
				item, err := newZipItem(root, info)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			continue
		}

		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				if shouldSkipScanPath(path) {
					return nil
				}
				fixer.Log(fixer.LoggerWarn, "Skip %s: %v", path, walkErr)
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if shouldSkipScanDir(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !LooksLikeTakeoutZip(path) {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			item, err := newZipItem(path, info)
			if err != nil {
				fixer.Log(fixer.LoggerWarn, "Skip unreadable ZIP %s: %v", path, err)
				return nil
			}
			items = append(items, item)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.SliceStable(items, func(i int, j int) bool {
		return strings.ToLower(items[i].Path) < strings.ToLower(items[j].Path)
	})
	return items, nil
}

func LooksLikeTakeoutZip(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return looksLikeCompleteZip(path) && strings.Contains(base, takeoutZipNameNeedle)
}

func looksLikeCompleteZip(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if hasIncompleteDownloadSuffix(base) {
		return false
	}
	return filepath.Ext(base) == ".zip"
}

func hasIncompleteDownloadSuffix(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".crdownload", ".part", ".tmp", ".fdmdownload", ".moving":
		return true
	default:
		return false
	}
}

func shouldSkipScanPath(path string) bool {
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if shouldSkipScanDir(part) {
			return true
		}
	}
	return false
}

func shouldSkipScanDir(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "$recycle.bin", "system volume information", "$winreagent", ".spotlight-v100", ".trashes":
		return true
	default:
		return false
	}
}

func newZipItem(path string, info os.FileInfo) (ZipItem, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return ZipItem{}, err
	}
	uncompressedBytes, mediaFiles, err := zipStats(absPath)
	if err != nil {
		return ZipItem{}, fmt.Errorf("read ZIP %s: %w", absPath, err)
	}
	item := ZipItem{
		Path:              absPath,
		Name:              filepath.Base(absPath),
		SourceDrive:       sourceDrive(absPath),
		SizeBytes:         info.Size(),
		UncompressedBytes: uncompressedBytes,
		MediaFiles:        mediaFiles,
		ModTime:           info.ModTime().UTC(),
	}
	item.Fingerprint = zipFingerprint(item)
	return item, nil
}

func zipStats(path string) (int64, int, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		_ = reader.Close()
	}()

	const maxInt64 = uint64(1<<63 - 1)
	var total uint64
	mediaFiles := 0
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		total += file.UncompressedSize64
		if fixer.IsMediaFile(file.Name) {
			mediaFiles++
		}
		if total > maxInt64 {
			return 0, 0, fmt.Errorf("ZIP uncompressed size is too large")
		}
	}
	return int64(total), mediaFiles, nil
}

func ExtractZip(zipPath string, destDir string) error {
	return ExtractZipWithProgress(zipPath, destDir, nil)
}

func ExtractZipWithProgress(zipPath string, destDir string, onProgress func(ExtractProgress)) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = reader.Close()
	}()

	destAbs, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destAbs, 0o755); err != nil {
		return err
	}

	totalFiles := countExtractableZipEntries(reader.File)
	processedFiles := 0
	for _, file := range reader.File {
		name, err := safeZipEntryName(file.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}

		target := filepath.Join(destAbs, name)
		if !pathWithin(destAbs, target) {
			return fmt.Errorf("unsafe ZIP entry path %q", file.Name)
		}
		reportExtractProgress(onProgress, ExtractProgress{
			ProcessedFiles: processedFiles,
			TotalFiles:     totalFiles,
			CurrentFile:    name,
			TotalBytes:     int64(file.UncompressedSize64),
		})

		mode := file.FileInfo().Mode()
		if mode&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse to extract symlink ZIP entry %q", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		source, err := file.Open()
		if err != nil {
			return err
		}
		dest, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
		if err != nil {
			_ = source.Close()
			return err
		}
		progressSource := &extractProgressReader{
			reader: source,
			total:  int64(file.UncompressedSize64),
			report: func(bytesCopied int64) {
				reportExtractProgress(onProgress, ExtractProgress{
					ProcessedFiles: processedFiles,
					TotalFiles:     totalFiles,
					CurrentFile:    name,
					CurrentBytes:   bytesCopied,
					TotalBytes:     int64(file.UncompressedSize64),
				})
			},
		}
		_, copyErr := io.Copy(dest, progressSource)
		closeSourceErr := source.Close()
		closeDestErr := dest.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeSourceErr != nil {
			return closeSourceErr
		}
		if closeDestErr != nil {
			return closeDestErr
		}
		processedFiles++
		reportExtractProgress(onProgress, ExtractProgress{
			ProcessedFiles: processedFiles,
			TotalFiles:     totalFiles,
			CurrentFile:    name,
			CurrentBytes:   int64(file.UncompressedSize64),
			TotalBytes:     int64(file.UncompressedSize64),
		})
	}
	return nil
}

type extractProgressReader struct {
	reader     io.Reader
	total      int64
	copied     int64
	lastReport time.Time
	report     func(int64)
}

func (r *extractProgressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.copied += int64(n)
		if time.Since(r.lastReport) >= 2*time.Second || r.copied == r.total {
			r.lastReport = time.Now()
			r.report(r.copied)
		}
	}
	if err == io.EOF {
		r.report(r.copied)
	}
	return n, err
}

func countExtractableZipEntries(files []*zip.File) int {
	count := 0
	for _, file := range files {
		if file.FileInfo().Mode()&os.ModeSymlink != 0 {
			continue
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if strings.TrimSpace(file.Name) == "" {
			continue
		}
		count++
	}
	return count
}

func reportExtractProgress(onProgress func(ExtractProgress), progress ExtractProgress) {
	if onProgress != nil {
		onProgress(progress)
	}
}

func LocateGooglePhotosFolder(root string) (string, error) {
	direct := []string{
		filepath.Join(root, "Takeout", "Google Photos"),
		filepath.Join(root, "Google Photos"),
	}
	var candidates []string
	seen := make(map[string]struct{})
	for _, candidate := range direct {
		if isDir(candidate) {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return "", err
			}
			candidates = append(candidates, abs)
			seen[strings.ToLower(filepath.Clean(abs))] = struct{}{}
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) > 1 {
		return "", fmt.Errorf("more than one Google Photos folder found: %s", strings.Join(candidates, "; "))
	}

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		if entry.Name() != "Google Photos" {
			return nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		key := strings.ToLower(filepath.Clean(abs))
		if _, ok := seen[key]; ok {
			return nil
		}
		candidates = append(candidates, abs)
		seen[key] = struct{}{}
		return filepath.SkipDir
	})
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no Takeout\\Google Photos or Google Photos folder found in %s", root)
	}
	if len(candidates) > 1 {
		return "", fmt.Errorf("more than one Google Photos folder found: %s", strings.Join(candidates, "; "))
	}
	return candidates[0], nil
}

func requiredWorkBytes(item ZipItem, marginBytes int64) int64 {
	need := item.UncompressedBytes
	if item.SizeBytes > need {
		need = item.SizeBytes
	}
	if marginBytes <= 0 {
		marginBytes = defaultMarginBytes
	}
	return need + marginBytes
}

func maxRequiredWorkBytes(items []ZipItem, marginBytes int64) int64 {
	var maxNeed int64
	for _, item := range items {
		need := requiredWorkBytes(item, marginBytes)
		if need > maxNeed {
			maxNeed = need
		}
	}
	return maxNeed
}

func zipFingerprint(item ZipItem) string {
	return fmt.Sprintf("%s|%d|%d", strings.ToLower(filepath.Clean(item.Path)), item.SizeBytes, item.ModTime.UnixNano())
}

func sourceDrive(path string) string {
	volume := filepath.VolumeName(path)
	if volume != "" {
		return strings.ToUpper(volume)
	}
	if filepath.IsAbs(path) {
		return string(filepath.Separator)
	}
	return ""
}

func safeZipEntryName(name string) (string, error) {
	zipName := strings.ReplaceAll(name, "\\", "/")
	if strings.TrimSpace(zipName) == "" {
		return "", nil
	}
	if strings.HasPrefix(zipName, "/") || strings.HasPrefix(zipName, "//") {
		return "", fmt.Errorf("unsafe ZIP entry path %q", name)
	}

	rawParts := strings.Split(zipName, "/")
	parts := make([]string, 0, len(rawParts))
	for index, rawPart := range rawParts {
		if rawPart == "" {
			continue
		}
		if index == 0 && looksLikeWindowsDrivePart(rawPart) {
			return "", fmt.Errorf("unsafe ZIP entry path %q", name)
		}
		if isTraversalPart(rawPart) {
			return "", fmt.Errorf("unsafe ZIP entry path %q", name)
		}

		part := sanitizeWindowsPathPart(rawPart)
		if part == "" {
			part = "_"
		}
		parts = append(parts, part)
	}

	if len(parts) == 0 {
		return "", nil
	}

	clean := filepath.Join(parts...)
	if filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" || clean == "." || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe ZIP entry path %q", name)
	}
	return clean, nil
}

func looksLikeWindowsDrivePart(part string) bool {
	return len(part) >= 2 && part[1] == ':' &&
		((part[0] >= 'A' && part[0] <= 'Z') || (part[0] >= 'a' && part[0] <= 'z'))
}

func isTraversalPart(part string) bool {
	trimmed := strings.TrimSpace(part)
	return trimmed == "." || trimmed == ".."
}

func sanitizeWindowsPathPart(part string) string {
	part = strings.TrimRight(part, " .")
	var b strings.Builder
	for _, r := range part {
		if isWindowsInvalidPathRune(r) {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	safe := b.String()
	if isReservedWindowsDeviceName(safe) {
		return "_" + safe
	}
	return safe
}

func isWindowsInvalidPathRune(r rune) bool {
	if r >= 0 && r < 32 {
		return true
	}
	switch r {
	case '<', '>', ':', '"', '|', '?', '*':
		return true
	default:
		return false
	}
}

func isReservedWindowsDeviceName(part string) bool {
	base := part
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	base = strings.ToUpper(base)
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 {
		prefix := base[:3]
		suffix := base[3]
		return (prefix == "COM" || prefix == "LPT") && suffix >= '1' && suffix <= '9'
	}
	return false
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func pathWithin(parent string, child string) bool {
	parentAbs, err := filepath.Abs(parent)
	if err != nil {
		return false
	}
	childAbs, err := filepath.Abs(child)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(parentAbs, childAbs)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
