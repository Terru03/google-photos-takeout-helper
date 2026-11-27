package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var mediaExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".heic": true,
	".3gp": true, ".avi": true, ".m4v": true, ".mkv": true, ".mov": true, ".mp4": true,
}

var imageExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".heic": true,
}

type manifestSummary struct {
	TotalMedia                   int `json:"totalMedia"`
	OutputMedia                  int `json:"outputMedia"`
	Matched                      int `json:"matched"`
	Unmatched                    int `json:"unmatched"`
	Ambiguous                    int `json:"ambiguous"`
	MetadataWritten              int `json:"metadataWritten"`
	MetadataVerified             int `json:"metadataVerified"`
	MetadataVerificationFailures int `json:"metadataVerificationFailures"`
	Errors                       int `json:"errors"`
	SuspiciousDates              int `json:"suspiciousDates"`
}

type manifestEntry struct {
	ZipName string          `json:"zipName"`
	Status  string          `json:"status"`
	Summary manifestSummary `json:"summary"`
}

type motionItem struct {
	Status          string `json:"status"`
	SourceStillPath string `json:"sourceStillPath"`
	SourceVideoPath string `json:"sourceVideoPath"`
	StillSize       int64  `json:"stillSize"`
	VideoSize       int64  `json:"videoSize"`
}

type motionReport struct {
	TotalCandidatePairs     int          `json:"totalCandidatePairs"`
	MergedSuccessfully      int          `json:"mergedSuccessfully"`
	SkippedAlreadyMerged    int          `json:"skippedAlreadyMerged"`
	SkippedPreviousFailures int          `json:"skippedPreviousFailures"`
	SkippedMissingStill     int          `json:"skippedMissingStill"`
	SkippedMissingVideo     int          `json:"skippedMissingVideo"`
	FailedMotionPhotoCalls  int          `json:"failedMotionPhoto2Calls"`
	TimedOutMerges          int          `json:"timedOutMerges"`
	Items                   []motionItem `json:"items"`
}

type backfillReport struct {
	Checked   int `json:"checked"`
	Matched   int `json:"matched"`
	Updated   int `json:"updated"`
	Unmatched int `json:"unmatched"`
	Ambiguous int `json:"ambiguous"`
	Failed    int `json:"failed"`
}

type stateRecord struct {
	SourceID         string `json:"sourceId"`
	SourceRelPath    string `json:"sourceRelPath"`
	OutputPath       string `json:"outputPath"`
	Status           string `json:"status"`
	MatchStatus      string `json:"matchStatus"`
	FolderDateSource string `json:"folderDateSource"`
	FolderYear       int    `json:"folderYear"`
	FolderMonth      int    `json:"folderMonth"`
	MetadataWritten  bool   `json:"metadataWritten"`
	MetadataVerified bool   `json:"metadataVerified"`
}

type fileCounts struct {
	Files  int   `json:"files"`
	Media  int   `json:"media"`
	Images int   `json:"images"`
	Videos int   `json:"videos"`
	Bytes  int64 `json:"bytes"`
}

type stateSummary struct {
	Records                  int `json:"records"`
	UniqueOutputs            int `json:"uniqueOutputs"`
	MissingOutputsBefore     int `json:"missingOutputsBefore"`
	MissingOutputsAfter      int `json:"missingOutputsAfter"`
	MetadataWritten          int `json:"metadataWritten"`
	MetadataVerified         int `json:"metadataVerified"`
	Matched                  int `json:"matched"`
	Unmatched                int `json:"unmatched"`
	Ambiguous                int `json:"ambiguous"`
	FolderDateSourcePresent  int `json:"folderDateSourcePresent"`
	FolderDatePathConsistent int `json:"folderDatePathConsistent"`
}

type deletionRecord struct {
	PhotoPath          string `json:"photoPath"`
	VideoPath          string `json:"videoPath"`
	PhotoSize          int64  `json:"photoSize"`
	VideoSize          int64  `json:"videoSize"`
	EmbeddedOffset     int64  `json:"embeddedOffset"`
	PhotoSHA256        string `json:"photoSha256,omitempty"`
	VideoSHA256        string `json:"videoSha256"`
	EmbeddedSHA256     string `json:"embeddedSha256"`
	MotionMetadata     bool   `json:"validMotionMetadata"`
	PayloadExtractable bool   `json:"payloadExtractable"`
	ExactSizeMatch     bool   `json:"exactSizeMatch"`
	SharedVideoRefs    int    `json:"sharedVideoRefs"`
	Status             string `json:"status"`
	Error              string `json:"error,omitempty"`
}

type auditReport struct {
	StartedAt           time.Time        `json:"startedAt"`
	FinishedAt          time.Time        `json:"finishedAt"`
	LibraryRoot         string           `json:"libraryRoot"`
	SourceZipRoot       string           `json:"sourceZipRoot"`
	DeleteRequested     bool             `json:"deleteRequested"`
	SourceZipCount      int              `json:"sourceZipCount"`
	TerminalZipCount    int              `json:"terminalZipCount"`
	MissingTerminalZips []string         `json:"missingTerminalZips"`
	ExtraTerminalZips   []string         `json:"extraTerminalZips"`
	TerminalStatuses    map[string]int   `json:"terminalStatuses"`
	ManifestTotals      manifestSummary  `json:"manifestTotals"`
	OutputBefore        fileCounts       `json:"outputBefore"`
	OutputAfter         fileCounts       `json:"outputAfter"`
	State               stateSummary     `json:"state"`
	Backfill            backfillReport   `json:"sidecarBackfill"`
	Motion              map[string]int   `json:"motion"`
	PairAuditCounts     map[string]int   `json:"pairAuditCounts"`
	DeletedVideos       int              `json:"deletedVideos"`
	DeletedBytes        int64            `json:"deletedBytes"`
	DeletionRecords     []deletionRecord `json:"deletionRecords"`
	ReportPath          string           `json:"reportPath"`
	CSVPath             string           `json:"csvPath"`
}

type pairResult struct {
	record deletionRecord
	reason string
}

func main() {
	library := flag.String("library", "", "output library root")
	sourceZips := flag.String("source-zips", "", "read-only source ZIP folder")
	deleteExact := flag.Bool("delete", false, "delete only byte-proven redundant video files")
	applyPlan := flag.String("apply-plan", "", "apply one fresh dry audit without rescanning every full payload")
	pairPlanFrom := flag.String("pair-plan-from", "", "reuse ZIP, state, and file checks while rebuilding pair proof")
	diagnoseMissing := flag.String("diagnose-missing-plan", "", "list old state output paths missing after an audit")
	workers := flag.Int("workers", max(1, min(4, runtime.NumCPU()/2)), "pair audit workers")
	flag.Parse()

	if strings.TrimSpace(*library) == "" || strings.TrimSpace(*sourceZips) == "" {
		fatalf("-library and -source-zips are required")
	}
	libraryRoot, err := filepath.Abs(*library)
	check(err)
	sourceRoot, err := filepath.Abs(*sourceZips)
	check(err)
	if sameOrChild(sourceRoot, libraryRoot) || sameOrChild(libraryRoot, sourceRoot) {
		fatalf("source ZIP root and library root must be separate")
	}
	if strings.TrimSpace(*applyPlan) != "" {
		check(applyAuditPlan(libraryRoot, sourceRoot, *applyPlan))
		return
	}
	if strings.TrimSpace(*pairPlanFrom) != "" {
		check(rebuildPairPlan(libraryRoot, sourceRoot, *pairPlanFrom, max(1, *workers)))
		return
	}
	if strings.TrimSpace(*diagnoseMissing) != "" {
		check(diagnoseMissingState(libraryRoot, *diagnoseMissing))
		return
	}

	reportsDir := filepath.Join(libraryRoot, ".gtf", "reports")
	check(os.MkdirAll(reportsDir, 0o755))
	stamp := time.Now().Format("20060102-150405")
	reportPath := filepath.Join(reportsDir, "motion-video-deletion-audit-"+stamp+".json")
	csvPath := filepath.Join(reportsDir, "motion-video-deletion-audit-"+stamp+".csv")

	audit := auditReport{
		StartedAt:        time.Now().UTC(),
		LibraryRoot:      libraryRoot,
		SourceZipRoot:    sourceRoot,
		DeleteRequested:  *deleteExact,
		TerminalStatuses: map[string]int{},
		Motion:           map[string]int{},
		PairAuditCounts:  map[string]int{},
		ReportPath:       reportPath,
		CSVPath:          csvPath,
	}

	sourceNames, err := readSourceZipNames(sourceRoot)
	check(err)
	audit.SourceZipCount = len(sourceNames)
	latestManifest, totals, statuses, err := readManifest(filepath.Join(libraryRoot, ".gtf", "batch", "manifest.jsonl"))
	check(err)
	audit.ManifestTotals = totals
	audit.TerminalStatuses = statuses
	terminalNames := map[string]bool{}
	for name, entry := range latestManifest {
		if entry.Status == "completed" || entry.Status == "completed_with_review" {
			terminalNames[normalizeRecoveredZip(name)] = true
			audit.TerminalZipCount++
		}
	}
	for name := range sourceNames {
		if !terminalNames[name] {
			audit.MissingTerminalZips = append(audit.MissingTerminalZips, name)
		}
	}
	for name := range terminalNames {
		if !sourceNames[name] {
			audit.ExtraTerminalZips = append(audit.ExtraTerminalZips, name)
		}
	}
	sort.Strings(audit.MissingTerminalZips)
	sort.Strings(audit.ExtraTerminalZips)

	audit.OutputBefore, err = countOutput(libraryRoot)
	check(err)
	stateRecords, state, err := readState(filepath.Join(libraryRoot, ".gtf", "state.jsonl"), nil)
	check(err)
	audit.State = state
	_ = stateRecords
	audit.Backfill, err = readBackfill(filepath.Join(reportsDir, "sidecar-backfill-latest.json"))
	check(err)

	motion, err := readMotion(filepath.Join(reportsDir, "motion-merge-report.json"))
	check(err)
	audit.Motion = map[string]int{
		"candidatePairs":   motion.TotalCandidatePairs,
		"merged":           motion.MergedSuccessfully,
		"alreadyMerged":    motion.SkippedAlreadyMerged,
		"previousFailures": motion.SkippedPreviousFailures,
		"missingStill":     motion.SkippedMissingStill,
		"missingVideo":     motion.SkippedMissingVideo,
		"failed":           motion.FailedMotionPhotoCalls,
		"timedOut":         motion.TimedOutMerges,
	}

	results := auditPairs(libraryRoot, motion.Items, max(1, *workers))
	for _, result := range results {
		audit.PairAuditCounts[result.reason]++
		if result.reason == "exact-byte-match" {
			audit.DeletionRecords = append(audit.DeletionRecords, result.record)
		}
	}

	check(writeAudit(reportPath, audit))
	check(writeAudit(filepath.Join(reportsDir, "motion-video-deletion-audit-latest.json"), audit))

	if *deleteExact {
		check(deleteCandidates(libraryRoot, csvPath, &audit))
	}

	audit.OutputAfter, err = countOutput(libraryRoot)
	check(err)
	deletedSet := map[string]bool{}
	for _, record := range audit.DeletionRecords {
		if record.Status == "deleted" {
			deletedSet[cleanKey(record.VideoPath)] = true
		}
	}
	_, stateAfter, err := readState(filepath.Join(libraryRoot, ".gtf", "state.jsonl"), deletedSet)
	check(err)
	audit.State.MissingOutputsAfter = stateAfter.MissingOutputsAfter
	audit.FinishedAt = time.Now().UTC()
	check(writeAudit(reportPath, audit))
	check(writeAudit(filepath.Join(reportsDir, "motion-video-deletion-audit-latest.json"), audit))

	fmt.Printf("AUDIT DONE zips=%d/%d media_before=%d exact=%d deleted=%d bytes=%d media_after=%d missing_before=%d missing_after=%d report=%s\n",
		audit.TerminalZipCount, audit.SourceZipCount, audit.OutputBefore.Media,
		audit.PairAuditCounts["exact-byte-match"], audit.DeletedVideos, audit.DeletedBytes,
		audit.OutputAfter.Media, audit.State.MissingOutputsBefore, audit.State.MissingOutputsAfter, reportPath)
}

func readSourceZipNames(root string) (map[string]bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			names[strings.ToLower(entry.Name())] = true
		}
	}
	return names, nil
}

func readManifest(path string) (map[string]manifestEntry, manifestSummary, map[string]int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, manifestSummary{}, nil, err
	}
	defer file.Close()
	latest := map[string]manifestEntry{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var entry manifestEntry
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry.ZipName != "" {
			latest[strings.ToLower(entry.ZipName)] = entry
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, manifestSummary{}, nil, err
	}
	var totals manifestSummary
	statuses := map[string]int{}
	for _, entry := range latest {
		statuses[entry.Status]++
		if entry.Status != "completed" && entry.Status != "completed_with_review" {
			continue
		}
		totals.TotalMedia += entry.Summary.TotalMedia
		totals.OutputMedia += entry.Summary.OutputMedia
		totals.Matched += entry.Summary.Matched
		totals.Unmatched += entry.Summary.Unmatched
		totals.Ambiguous += entry.Summary.Ambiguous
		totals.MetadataWritten += entry.Summary.MetadataWritten
		totals.MetadataVerified += entry.Summary.MetadataVerified
		totals.MetadataVerificationFailures += entry.Summary.MetadataVerificationFailures
		totals.Errors += entry.Summary.Errors
		totals.SuspiciousDates += entry.Summary.SuspiciousDates
	}
	return latest, totals, statuses, nil
}

func normalizeRecoveredZip(name string) string {
	name = strings.ToLower(filepath.Base(name))
	return strings.TrimSuffix(name, ".recovered.zip") + ".zip"
}

func countOutput(root string) (fileCounts, error) {
	var counts fileCounts
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && strings.EqualFold(entry.Name(), ".gtf") {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		counts.Files++
		counts.Bytes += info.Size()
		ext := strings.ToLower(filepath.Ext(path))
		if mediaExtensions[ext] {
			counts.Media++
			if imageExtensions[ext] {
				counts.Images++
			} else {
				counts.Videos++
			}
		}
		return nil
	})
	return counts, err
}

func readState(path string, allowedMissing map[string]bool) (map[string]stateRecord, stateSummary, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, stateSummary{}, err
	}
	defer file.Close()
	latest := map[string]stateRecord{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var record stateRecord
		if json.Unmarshal(scanner.Bytes(), &record) != nil || record.SourceRelPath == "" {
			continue
		}
		latest[strings.ToLower(record.SourceID+"|"+record.SourceRelPath)] = record
	}
	if err := scanner.Err(); err != nil {
		return nil, stateSummary{}, err
	}
	outputs := map[string]string{}
	var summary stateSummary
	summary.Records = len(latest)
	for _, record := range latest {
		if record.OutputPath != "" {
			outputs[cleanKey(record.OutputPath)] = record.OutputPath
		}
		if record.MetadataWritten {
			summary.MetadataWritten++
		}
		if record.MetadataVerified {
			summary.MetadataVerified++
		}
		switch record.MatchStatus {
		case "matched":
			summary.Matched++
		case "unmatched":
			summary.Unmatched++
		case "ambiguous":
			summary.Ambiguous++
		}
		if record.FolderDateSource != "" {
			summary.FolderDateSourcePresent++
		}
		if folderPathMatches(record) {
			summary.FolderDatePathConsistent++
		}
	}
	summary.UniqueOutputs = len(outputs)
	for key, path := range outputs {
		if _, err := os.Stat(path); err != nil {
			summary.MissingOutputsBefore++
			if allowedMissing == nil || !allowedMissing[key] {
				summary.MissingOutputsAfter++
			}
		}
	}
	return latest, summary, nil
}

func folderPathMatches(record stateRecord) bool {
	if record.OutputPath == "" || record.FolderYear == 0 {
		return false
	}
	clean := strings.ToLower(filepath.Clean(record.OutputPath))
	yearPart := strings.ToLower(fmt.Sprintf("photos from %04d", record.FolderYear))
	if !strings.Contains(clean, yearPart) {
		return false
	}
	if record.FolderMonth == 0 {
		return strings.Contains(clean, strings.ToLower("Unknown month"))
	}
	monthPrefix := string(filepath.Separator) + strconv.Itoa(record.FolderMonth) + " - "
	return strings.Contains(clean, strings.ToLower(monthPrefix))
}

func readBackfill(path string) (backfillReport, error) {
	var report backfillReport
	data, err := os.ReadFile(path)
	if err != nil {
		return report, err
	}
	err = json.Unmarshal(data, &report)
	return report, err
}

func readMotion(path string) (motionReport, error) {
	var report motionReport
	data, err := os.ReadFile(path)
	if err != nil {
		return report, err
	}
	err = json.Unmarshal(data, &report)
	return report, err
}

func auditPairs(library string, items []motionItem, workers int) []pairResult {
	videoRefs := map[string]int{}
	for _, item := range items {
		if item.SourceVideoPath != "" {
			videoRefs[cleanKey(item.SourceVideoPath)]++
		}
	}
	jobs := make(chan motionItem)
	results := make(chan pairResult)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for item := range jobs {
				results <- auditPair(library, item, videoRefs[cleanKey(item.SourceVideoPath)])
			}
		}()
	}
	go func() {
		for _, item := range items {
			jobs <- item
		}
		close(jobs)
		wait.Wait()
		close(results)
	}()
	out := make([]pairResult, 0, len(items))
	for result := range results {
		out = append(out, result)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].record.VideoPath < out[j].record.VideoPath })
	return out
}

func auditPair(library string, item motionItem, refs int) pairResult {
	record := deletionRecord{
		PhotoPath: item.SourceStillPath, VideoPath: item.SourceVideoPath,
		PhotoSize: item.StillSize, VideoSize: item.VideoSize, SharedVideoRefs: refs,
		Status: "kept",
	}
	if item.Status != "merged" && item.Status != "already-merged" {
		return pairResult{record, "not-merged"}
	}
	if !safeLibraryPath(library, item.SourceStillPath) || !safeLibraryPath(library, item.SourceVideoPath) {
		return pairResult{record, "unsafe-path"}
	}
	if refs != 1 {
		return pairResult{record, "shared-video"}
	}
	if !sameStem(item.SourceStillPath, item.SourceVideoPath) {
		return pairResult{record, "ambiguous-stem"}
	}
	stillInfo, err := os.Stat(item.SourceStillPath)
	if err != nil {
		return pairResult{record, "missing-still"}
	}
	videoInfo, err := os.Stat(item.SourceVideoPath)
	if err != nil {
		return pairResult{record, "missing-video"}
	}
	record.PhotoSize = stillInfo.Size()
	record.VideoSize = videoInfo.Size()
	if item.StillSize != 0 && item.StillSize != stillInfo.Size() {
		return pairResult{record, "changed-still"}
	}
	if item.VideoSize != 0 && item.VideoSize != videoInfo.Size() {
		return pairResult{record, "changed-video"}
	}
	valid, embeddedLength, err := readMotionMetadata(item.SourceStillPath)
	if err != nil || !valid {
		return pairResult{record, "bad-motion-metadata"}
	}
	record.MotionMetadata = true
	if videoInfo.Size() <= 0 || stillInfo.Size() <= videoInfo.Size() {
		return pairResult{record, "bad-payload-size"}
	}
	if strings.EqualFold(filepath.Ext(item.SourceStillPath), ".heic") && embeddedLength != videoInfo.Size() {
		return pairResult{record, "embedded-size-differs"}
	}
	equal, offset, videoHash, embeddedHash, err := findExactPayload(item.SourceStillPath, item.SourceVideoPath, videoInfo.Size(), embeddedLength)
	if err != nil {
		record.Error = err.Error()
		return pairResult{record, "compare-error"}
	}
	if !equal {
		return pairResult{record, "embedded-bytes-differ"}
	}
	record.EmbeddedOffset = offset
	record.PayloadExtractable = true
	record.ExactSizeMatch = true
	record.VideoSHA256 = videoHash
	record.EmbeddedSHA256 = embeddedHash
	record.Status = "proved"
	return pairResult{record, "exact-byte-match"}
}

func validMotionMetadata(path string) (bool, error) {
	valid, _, err := readMotionMetadata(path)
	return valid, err
}

func readMotionMetadata(path string) (bool, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, 0, err
	}
	defer file.Close()
	const limit = 32 * 1024 * 1024
	const overlap = 2048
	buffer := make([]byte, 256*1024)
	window := make([]byte, 0, len(buffer)+overlap)
	var readTotal int64
	var hasMotion, hasVersion, hasSemantic, hasVideoMime bool
	var embeddedLength int64
	for readTotal < limit {
		want := int64(len(buffer))
		if limit-readTotal < want {
			want = limit - readTotal
		}
		count, readErr := file.Read(buffer[:want])
		if count > 0 {
			window = append(window, buffer[:count]...)
			hasMotion = hasMotion || bytes.Contains(window, []byte(`GCamera:MotionPhoto="1"`)) || bytes.Contains(window, []byte(`<GCamera:MotionPhoto>1<`))
			hasVersion = hasVersion || bytes.Contains(window, []byte(`GCamera:MotionPhotoVersion="1"`)) || bytes.Contains(window, []byte(`<GCamera:MotionPhotoVersion>1<`))
			hasSemantic = hasSemantic || bytes.Contains(window, []byte(`Item:Semantic="MotionPhoto"`))
			hasVideoMime = hasVideoMime || bytes.Contains(window, []byte(`Item:Mime="video/quicktime"`)) || bytes.Contains(window, []byte(`Item:Mime="video/mp4"`))
			if embeddedLength == 0 {
				embeddedLength = motionItemLength(window)
			}
			if hasMotion && hasVersion && hasSemantic && hasVideoMime && embeddedLength > 0 {
				return true, embeddedLength, nil
			}
			if len(window) > overlap {
				window = append(window[:0], window[len(window)-overlap:]...)
			}
			readTotal += int64(count)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return false, 0, readErr
		}
	}
	return hasMotion && hasVersion && hasSemantic && hasVideoMime, embeddedLength, nil
}

func motionItemLength(data []byte) int64 {
	semantic := []byte(`Item:Semantic="MotionPhoto"`)
	index := bytes.Index(data, semantic)
	if index < 0 {
		return 0
	}
	end := index + 2048
	if end > len(data) {
		end = len(data)
	}
	section := data[index:end]
	marker := []byte(`Item:Length="`)
	lengthIndex := bytes.Index(section, marker)
	if lengthIndex < 0 {
		return 0
	}
	start := lengthIndex + len(marker)
	finish := start
	for finish < len(section) && section[finish] >= '0' && section[finish] <= '9' {
		finish++
	}
	if finish == start {
		return 0
	}
	value, _ := strconv.ParseInt(string(section[start:finish]), 10, 64)
	return value
}

func findExactPayload(stillPath, videoPath string, size, embeddedLength int64) (bool, int64, string, string, error) {
	extension := strings.ToLower(filepath.Ext(stillPath))
	var offsets []int64
	var err error
	if extension == ".jpg" || extension == ".jpeg" {
		offsets, err = samsungPayloadOffsets(stillPath, size)
	} else {
		if embeddedLength != size {
			return false, 0, "", "", nil
		}
		offsets, err = videoHeaderOffsets(stillPath, videoPath)
	}
	if err != nil {
		return false, 0, "", "", err
	}
	for _, offset := range offsets {
		equal, videoHash, embeddedHash, compareErr := compareAt(stillPath, videoPath, offset, size)
		if compareErr != nil {
			return false, 0, "", "", compareErr
		}
		if equal {
			return true, offset, videoHash, embeddedHash, nil
		}
	}
	return false, 0, "", "", nil
}

func samsungPayloadOffsets(path string, videoSize int64) ([]int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	const margin = int64(4 * 1024 * 1024)
	boundary := info.Size() - videoSize
	start := boundary - margin
	if start < 0 {
		start = 0
	}
	end := boundary + margin
	if end > info.Size() {
		end = info.Size()
	}
	data := make([]byte, end-start)
	if _, err := file.ReadAt(data, start); err != nil && err != io.EOF {
		return nil, err
	}
	marker := []byte("MotionPhoto_Data")
	var offsets []int64
	for search := 0; search < len(data); {
		index := bytes.Index(data[search:], marker)
		if index < 0 {
			break
		}
		offset := start + int64(search+index+len(marker))
		if offset >= 0 && offset+videoSize <= info.Size() {
			offsets = append(offsets, offset)
		}
		search += index + len(marker)
	}
	return offsets, nil
}

func videoHeaderOffsets(stillPath, videoPath string) ([]int64, error) {
	video, err := os.Open(videoPath)
	if err != nil {
		return nil, err
	}
	needle := make([]byte, 64)
	count, readErr := io.ReadFull(video, needle)
	_ = video.Close()
	if readErr != nil && readErr != io.ErrUnexpectedEOF {
		return nil, readErr
	}
	needle = needle[:count]
	if len(needle) < 16 {
		return nil, nil
	}
	still, err := os.Open(stillPath)
	if err != nil {
		return nil, err
	}
	defer still.Close()
	const chunkSize = 4 * 1024 * 1024
	buffer := make([]byte, chunkSize)
	overlap := make([]byte, 0, len(needle)-1)
	var base int64
	var offsets []int64
	for {
		count, readErr := still.Read(buffer)
		if count > 0 {
			window := append(overlap, buffer[:count]...)
			for search := 0; search < len(window); {
				index := bytes.Index(window[search:], needle)
				if index < 0 {
					break
				}
				offsets = append(offsets, base-int64(len(overlap))+int64(search+index))
				search += index + 1
			}
			keep := len(needle) - 1
			if keep > len(window) {
				keep = len(window)
			}
			overlap = append(overlap[:0], window[len(window)-keep:]...)
			base += int64(count)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return offsets, nil
}

func compareAt(stillPath, videoPath string, offset, size int64) (bool, string, string, error) {
	still, err := os.Open(stillPath)
	if err != nil {
		return false, "", "", err
	}
	defer still.Close()
	video, err := os.Open(videoPath)
	if err != nil {
		return false, "", "", err
	}
	defer video.Close()
	stillInfo, err := still.Stat()
	if err != nil {
		return false, "", "", err
	}
	if offset < 0 || stillInfo.Size() < offset+size {
		return false, "", "", errors.New("photo smaller than payload")
	}
	if _, err := still.Seek(offset, io.SeekStart); err != nil {
		return false, "", "", err
	}
	videoHash := sha256.New()
	embeddedHash := sha256.New()
	left := size
	stillBuf := make([]byte, 1024*1024)
	videoBuf := make([]byte, 1024*1024)
	for left > 0 {
		want := int64(len(stillBuf))
		if left < want {
			want = left
		}
		n1, err1 := io.ReadFull(still, stillBuf[:want])
		n2, err2 := io.ReadFull(video, videoBuf[:want])
		if err1 != nil || err2 != nil || n1 != n2 {
			return false, "", "", firstError(err1, err2)
		}
		if !bytes.Equal(stillBuf[:n1], videoBuf[:n2]) {
			return false, "", "", nil
		}
		_, _ = embeddedHash.Write(stillBuf[:n1])
		_, _ = videoHash.Write(videoBuf[:n2])
		left -= int64(n1)
	}
	return true, hex.EncodeToString(videoHash.Sum(nil)), hex.EncodeToString(embeddedHash.Sum(nil)), nil
}

func deleteCandidates(library, csvPath string, audit *auditReport) error {
	file, err := os.OpenFile(csvPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"photo_path", "video_path", "photo_size", "video_size", "embedded_offset", "video_sha256", "embedded_sha256", "status", "error"}); err != nil {
		return err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}

	for index := range audit.DeletionRecords {
		record := &audit.DeletionRecords[index]
		if !safeLibraryPath(library, record.PhotoPath) || !safeLibraryPath(library, record.VideoPath) {
			record.Status = "kept"
			record.Error = "path guard failed"
			continue
		}
		valid, err := validMotionMetadata(record.PhotoPath)
		if err != nil || !valid {
			record.Status = "kept"
			record.Error = "metadata recheck failed"
			continue
		}
		videoInfo, err := os.Stat(record.VideoPath)
		if err != nil || videoInfo.Size() != record.VideoSize {
			record.Status = "kept"
			record.Error = "video size recheck failed"
			continue
		}
		equal, videoHash, embeddedHash, err := compareAt(record.PhotoPath, record.VideoPath, record.EmbeddedOffset, record.VideoSize)
		if err != nil || !equal || videoHash != record.VideoSHA256 || embeddedHash != record.EmbeddedSHA256 {
			record.Status = "kept"
			record.Error = "byte/hash recheck failed"
			continue
		}
		if err := os.Remove(record.VideoPath); err != nil {
			record.Status = "delete-error"
			record.Error = err.Error()
		} else {
			record.Status = "deleted"
			audit.DeletedVideos++
			audit.DeletedBytes += record.VideoSize
		}
		if err := writer.Write([]string{record.PhotoPath, record.VideoPath, strconv.FormatInt(record.PhotoSize, 10), strconv.FormatInt(record.VideoSize, 10), strconv.FormatInt(record.EmbeddedOffset, 10), record.VideoSHA256, record.EmbeddedSHA256, record.Status, record.Error}); err != nil {
			return err
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			return err
		}
	}
	return nil
}

func applyAuditPlan(library, sourceRoot, planPath string) error {
	data, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	var audit auditReport
	if err := json.Unmarshal(data, &audit); err != nil {
		return err
	}
	if cleanKey(audit.LibraryRoot) != cleanKey(library) || cleanKey(audit.SourceZipRoot) != cleanKey(sourceRoot) {
		return errors.New("audit roots do not match requested roots")
	}
	if len(audit.MissingTerminalZips) != 0 || audit.SourceZipCount != audit.TerminalZipCount {
		return errors.New("audit has missing terminal ZIPs")
	}
	if audit.PairAuditCounts["exact-byte-match"] != len(audit.DeletionRecords) {
		return errors.New("audit exact match count does not match deletion records")
	}
	if audit.FinishedAt.IsZero() || time.Since(audit.FinishedAt) > 2*time.Hour {
		return errors.New("audit plan is not fresh")
	}
	audit.DeleteRequested = true
	audit.DeletedVideos = 0
	audit.DeletedBytes = 0
	file, err := os.OpenFile(audit.CSVPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"photo_path", "video_path", "photo_size", "video_size", "embedded_offset", "video_sha256", "embedded_sha256", "status", "error"}); err != nil {
		return err
	}
	writer.Flush()
	for index := range audit.DeletionRecords {
		record := &audit.DeletionRecords[index]
		record.Error = ""
		if record.Status != "proved" || !record.MotionMetadata || !record.PayloadExtractable || !record.ExactSizeMatch || record.SharedVideoRefs != 1 || record.VideoSHA256 == "" || record.VideoSHA256 != record.EmbeddedSHA256 {
			record.Status = "kept"
			record.Error = "proof fields failed"
			continue
		}
		if !safeLibraryPath(library, record.PhotoPath) || !safeLibraryPath(library, record.VideoPath) || !sameStem(record.PhotoPath, record.VideoPath) {
			record.Status = "kept"
			record.Error = "path guard failed"
			continue
		}
		photoInfo, photoErr := os.Stat(record.PhotoPath)
		videoInfo, videoErr := os.Stat(record.VideoPath)
		if photoErr != nil || videoErr != nil || photoInfo.Size() != record.PhotoSize || videoInfo.Size() != record.VideoSize {
			record.Status = "kept"
			record.Error = "size recheck failed"
			continue
		}
		valid, metaErr := validMotionMetadata(record.PhotoPath)
		if metaErr != nil || !valid {
			record.Status = "kept"
			record.Error = "metadata recheck failed"
			continue
		}
		equal, sampleErr := compareAtSample(record.PhotoPath, record.VideoPath, record.EmbeddedOffset, record.VideoSize)
		if sampleErr != nil || !equal {
			record.Status = "kept"
			record.Error = "payload sample recheck failed"
			continue
		}
		if err := os.Remove(record.VideoPath); err != nil {
			record.Status = "delete-error"
			record.Error = err.Error()
		} else {
			record.Status = "deleted"
			audit.DeletedVideos++
			audit.DeletedBytes += record.VideoSize
		}
		if err := writer.Write([]string{record.PhotoPath, record.VideoPath, strconv.FormatInt(record.PhotoSize, 10), strconv.FormatInt(record.VideoSize, 10), strconv.FormatInt(record.EmbeddedOffset, 10), record.VideoSHA256, record.EmbeddedSHA256, record.Status, record.Error}); err != nil {
			return err
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			return err
		}
	}
	audit.OutputAfter, err = countOutput(library)
	if err != nil {
		return err
	}
	deletedSet := map[string]bool{}
	for _, record := range audit.DeletionRecords {
		if record.Status == "deleted" {
			deletedSet[cleanKey(record.VideoPath)] = true
		}
	}
	_, stateAfter, err := readState(filepath.Join(library, ".gtf", "state.jsonl"), deletedSet)
	if err != nil {
		return err
	}
	audit.State.MissingOutputsAfter = stateAfter.MissingOutputsAfter
	audit.FinishedAt = time.Now().UTC()
	if err := writeAudit(audit.ReportPath, audit); err != nil {
		return err
	}
	latestPath := filepath.Join(library, ".gtf", "reports", "motion-video-deletion-audit-latest.json")
	if err := writeAudit(latestPath, audit); err != nil {
		return err
	}
	fmt.Printf("APPLY DONE exact=%d deleted=%d bytes=%d media_after=%d missing_after=%d report=%s\n", len(audit.DeletionRecords), audit.DeletedVideos, audit.DeletedBytes, audit.OutputAfter.Media, audit.State.MissingOutputsAfter, audit.ReportPath)
	return nil
}

func rebuildPairPlan(library, sourceRoot, oldPlanPath string, workers int) error {
	data, err := os.ReadFile(oldPlanPath)
	if err != nil {
		return err
	}
	var audit auditReport
	if err := json.Unmarshal(data, &audit); err != nil {
		return err
	}
	if cleanKey(audit.LibraryRoot) != cleanKey(library) || cleanKey(audit.SourceZipRoot) != cleanKey(sourceRoot) {
		return errors.New("old audit roots do not match requested roots")
	}
	if len(audit.MissingTerminalZips) != 0 || audit.SourceZipCount != audit.TerminalZipCount {
		return errors.New("old audit has missing terminal ZIPs")
	}
	reportDir := filepath.Join(library, ".gtf", "reports")
	stamp := time.Now().Format("20060102-150405")
	audit.ReportPath = filepath.Join(reportDir, "motion-video-deletion-audit-"+stamp+".json")
	audit.CSVPath = filepath.Join(reportDir, "motion-video-deletion-audit-"+stamp+".csv")
	audit.StartedAt = time.Now().UTC()
	audit.FinishedAt = time.Time{}
	audit.DeleteRequested = false
	audit.DeletedVideos = 0
	audit.DeletedBytes = 0
	audit.DeletionRecords = nil
	audit.PairAuditCounts = map[string]int{}
	motion, err := readMotion(filepath.Join(reportDir, "motion-merge-report.json"))
	if err != nil {
		return err
	}
	results := auditPairs(library, motion.Items, workers)
	for _, result := range results {
		audit.PairAuditCounts[result.reason]++
		if result.reason == "exact-byte-match" {
			audit.DeletionRecords = append(audit.DeletionRecords, result.record)
		}
	}
	audit.FinishedAt = time.Now().UTC()
	if err := writeAudit(audit.ReportPath, audit); err != nil {
		return err
	}
	latestPath := filepath.Join(reportDir, "motion-video-deletion-audit-latest.json")
	if err := writeAudit(latestPath, audit); err != nil {
		return err
	}
	fmt.Printf("PAIR PLAN DONE pairs=%d exact=%d report=%s\n", len(motion.Items), len(audit.DeletionRecords), audit.ReportPath)
	return nil
}

type missingStateOutput struct {
	ExpectedPath  string   `json:"expectedPath"`
	SourceRelPath string   `json:"sourceRelPath"`
	Status        string   `json:"status"`
	Alternatives  []string `json:"sameStemAlternatives,omitempty"`
}

func diagnoseMissingState(library, auditPath string) error {
	data, err := os.ReadFile(auditPath)
	if err != nil {
		return err
	}
	var audit auditReport
	if err := json.Unmarshal(data, &audit); err != nil {
		return err
	}
	if cleanKey(audit.LibraryRoot) != cleanKey(library) {
		return errors.New("audit library root does not match")
	}
	deleted := map[string]bool{}
	for _, record := range audit.DeletionRecords {
		if record.Status == "deleted" {
			deleted[cleanKey(record.VideoPath)] = true
		}
	}
	state, _, err := readState(filepath.Join(library, ".gtf", "state.jsonl"), deleted)
	if err != nil {
		return err
	}
	byOutput := map[string]stateRecord{}
	for _, record := range state {
		if record.OutputPath != "" {
			byOutput[cleanKey(record.OutputPath)] = record
		}
	}
	missing := make([]missingStateOutput, 0)
	for key, record := range byOutput {
		if deleted[key] {
			continue
		}
		if _, err := os.Stat(record.OutputPath); err == nil {
			continue
		}
		missing = append(missing, missingStateOutput{
			ExpectedPath: record.OutputPath, SourceRelPath: record.SourceRelPath,
			Status: record.Status, Alternatives: sameStemAlternatives(record.OutputPath),
		})
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].ExpectedPath < missing[j].ExpectedPath })
	report := struct {
		CreatedAt time.Time            `json:"createdAt"`
		Count     int                  `json:"count"`
		Items     []missingStateOutput `json:"items"`
	}{time.Now().UTC(), len(missing), missing}
	outPath := filepath.Join(library, ".gtf", "reports", "state-missing-output-report.json")
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		return err
	}
	fmt.Printf("MISSING STATE DONE count=%d report=%s\n", len(missing), outPath)
	return nil
}

func sameStemAlternatives(path string) []string {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return nil
	}
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	var alternatives []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		entryStem := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if strings.EqualFold(stem, entryStem) {
			alternatives = append(alternatives, filepath.Join(filepath.Dir(path), entry.Name()))
		}
	}
	sort.Strings(alternatives)
	return alternatives
}

func compareAtSample(stillPath, videoPath string, embeddedOffset, size int64) (bool, error) {
	still, err := os.Open(stillPath)
	if err != nil {
		return false, err
	}
	defer still.Close()
	video, err := os.Open(videoPath)
	if err != nil {
		return false, err
	}
	defer video.Close()
	stillInfo, err := still.Stat()
	if err != nil {
		return false, err
	}
	if size <= 0 || stillInfo.Size() <= size {
		return false, errors.New("bad sample size")
	}
	chunk := int64(1024 * 1024)
	if size < chunk {
		chunk = size
	}
	checks := []int64{0}
	if size > chunk {
		checks = append(checks, size-chunk)
	}
	left := make([]byte, chunk)
	right := make([]byte, chunk)
	base := embeddedOffset
	if base < 0 || base+size > stillInfo.Size() {
		return false, errors.New("sample offset outside photo")
	}
	for _, offset := range checks {
		if _, err := still.ReadAt(left, base+offset); err != nil {
			return false, err
		}
		if _, err := video.ReadAt(right, offset); err != nil {
			return false, err
		}
		if !bytes.Equal(left, right) {
			return false, nil
		}
	}
	return true, nil
}

func writeAudit(path string, report auditReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(temp, path)
}

func sameStem(left, right string) bool {
	leftBase := strings.TrimSuffix(filepath.Base(left), filepath.Ext(left))
	rightBase := strings.TrimSuffix(filepath.Base(right), filepath.Ext(right))
	return strings.EqualFold(leftBase, rightBase)
}

func safeLibraryPath(library, path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil || !sameOrChild(abs, library) {
		return false
	}
	rel, err := filepath.Rel(library, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	first := strings.Split(filepath.Clean(rel), string(filepath.Separator))[0]
	return !strings.EqualFold(first, ".gtf")
}

func sameOrChild(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func cleanKey(path string) string { return strings.ToLower(filepath.Clean(path)) }

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return io.ErrUnexpectedEOF
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func check(err error) {
	if err != nil {
		fatalf("%v", err)
	}
}
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
