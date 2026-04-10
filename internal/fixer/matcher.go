package fixer

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	duplicateBeforeExtRE = regexp.MustCompile(`^(.*)\((\d+)\)(\.[^.]+)$`)
	duplicateAfterExtRE  = regexp.MustCompile(`^(.*)(\.[^.]+)\((\d+)\)$`)
	trailingDashDigitsRE = regexp.MustCompile(`^(.*)-(\d+)$`)
)

type folderSidecarCandidate struct {
	path      string
	name      string
	meta      *imageMetadata
	title     string
	nameKeys  map[string]struct{}
	titleKeys map[string]struct{}
}

type folderCatalog struct {
	dirPath      string
	relativeDir  string
	topLevelDir  string
	isYearFolder bool
	media        []string
	sidecars     []folderSidecarCandidate
}

func DiscoverMediaPlan(sourceRoot string, options ProcessOptions) ([]MediaPlan, error) {
	folders := make(map[string]*folderCatalog)

	err := filepath.WalkDir(sourceRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			if strings.EqualFold(d.Name(), ".gtf") || strings.EqualFold(d.Name(), "logs") {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		relDir := filepath.Dir(relPath)
		if relDir == "." {
			relDir = ""
		}

		topLevelDir := relPath
		if relDir != "" {
			topLevelDir = strings.Split(relDir, string(filepath.Separator))[0]
		}
		isYearFolder, _ := IsYearFolder(topLevelDir)

		if options.IgnoreAlbums && !isYearFolder {
			return nil
		}

		bundle, ok := folders[relDir]
		if !ok {
			bundle = &folderCatalog{
				dirPath:      filepath.Dir(path),
				relativeDir:  relDir,
				topLevelDir:  topLevelDir,
				isYearFolder: isYearFolder,
			}
			folders[relDir] = bundle
		}

		switch {
		case IsMediaFile(path):
			bundle.media = append(bundle.media, path)
		case strings.EqualFold(filepath.Ext(path), ".json"):
			bundle.sidecars = append(bundle.sidecars, buildSidecarCandidate(path))
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	dirKeys := make([]string, 0, len(folders))
	for dir := range folders {
		dirKeys = append(dirKeys, dir)
	}
	sort.Strings(dirKeys)

	var plans []MediaPlan
	for _, dirKey := range dirKeys {
		bundle := folders[dirKey]
		sort.Strings(bundle.media)
		plans = append(plans, resolveFolderPlans(sourceRoot, bundle)...)
	}

	sort.SliceStable(plans, func(i int, j int) bool {
		if plans[i].IsYearFolder != plans[j].IsYearFolder {
			return plans[i].IsYearFolder
		}
		return plans[i].RelativePath < plans[j].RelativePath
	})

	return plans, nil
}

func buildSidecarCandidate(path string) folderSidecarCandidate {
	candidate := folderSidecarCandidate{
		path:      path,
		name:      filepath.Base(path),
		nameKeys:  keysToSet(buildNameKeys(filepath.Base(path))),
		titleKeys: make(map[string]struct{}),
	}

	if meta, err := ReadJSONMetadata(path); err == nil {
		candidate.meta = &meta
		candidate.title = strings.TrimSpace(meta.BestTitle())
		candidate.titleKeys = keysToSet(buildNameKeys(candidate.title))
	}

	return candidate
}

func resolveFolderPlans(sourceRoot string, bundle *folderCatalog) []MediaPlan {
	plans := make([]MediaPlan, 0, len(bundle.media))
	byNormalizedImageKey := make(map[string]MediaPlan)

	for _, mediaPath := range bundle.media {
		plan := resolveDirectMatch(sourceRoot, bundle, mediaPath)
		plans = append(plans, plan)
		if !plan.IsVideo && plan.MatchStatus == MatchStatusMatched && plan.SidecarPath != "" {
			for _, key := range buildNameKeys(plan.FileName) {
				if _, exists := byNormalizedImageKey[key]; !exists {
					byNormalizedImageKey[key] = plan
				}
			}
		}
	}

	for i, plan := range plans {
		if !plan.IsVideo || plan.MatchStatus == MatchStatusMatched {
			continue
		}

		for _, key := range buildNameKeys(plan.FileName) {
			if partner, ok := byNormalizedImageKey[key]; ok {
				plan.SidecarPath = partner.SidecarPath
				plan.PartnerPath = partner.SourcePath
				plan.Metadata = partner.Metadata
				plan.MatchStatus = MatchStatusMatched
				plan.MatchStrategy = MatchStrategyPartner
				plan.MatchCandidates = []string{partner.SidecarPath}
				plans[i] = plan
				break
			}
		}
	}

	return plans
}

func resolveDirectMatch(sourceRoot string, bundle *folderCatalog, mediaPath string) MediaPlan {
	relPath, _ := filepath.Rel(sourceRoot, mediaPath)
	plan := MediaPlan{
		SourcePath:   mediaPath,
		RelativePath: relPath,
		RelativeDir:  bundle.relativeDir,
		TopLevelDir:  bundle.topLevelDir,
		FileName:     filepath.Base(mediaPath),
		OutputName:   filepath.Base(mediaPath),
		IsYearFolder: bundle.isYearFolder,
		IsVideo:      IsVideoFile(mediaPath),
		MatchStatus:  MatchStatusUnmatched,
	}

	mediaKeys := keysToSet(buildNameKeys(plan.FileName))
	mediaNameLower := strings.ToLower(plan.FileName)

	bestScore := 0
	bestIndexes := make([]int, 0, 1)

	for idx, candidate := range bundle.sidecars {
		score := matchScore(mediaNameLower, mediaKeys, candidate)
		if score <= 0 {
			continue
		}
		if score > bestScore {
			bestScore = score
			bestIndexes = []int{idx}
			continue
		}
		if score == bestScore {
			bestIndexes = append(bestIndexes, idx)
		}
	}

	if len(bestIndexes) == 0 {
		return plan
	}

	plan.MatchScore = bestScore
	if len(bestIndexes) > 1 {
		plan.MatchStatus = MatchStatusAmbiguous
		plan.MatchCandidates = make([]string, 0, len(bestIndexes))
		for _, idx := range bestIndexes {
			plan.MatchCandidates = append(plan.MatchCandidates, bundle.sidecars[idx].path)
		}
		sort.Strings(plan.MatchCandidates)
		return plan
	}

	candidate := bundle.sidecars[bestIndexes[0]]
	plan.SidecarPath = candidate.path
	plan.Metadata = candidate.meta
	plan.MatchStatus = MatchStatusMatched
	plan.MatchCandidates = []string{candidate.path}
	switch {
	case strings.EqualFold(candidate.name, plan.FileName+".json"):
		plan.MatchStrategy = MatchStrategyExactName
	case candidate.title != "" && strings.EqualFold(candidate.title, plan.FileName):
		plan.MatchStrategy = MatchStrategyJSONTitle
	case sharesAnyKey(mediaKeys, candidate.titleKeys) || sharesAnyKey(mediaKeys, candidate.nameKeys):
		plan.MatchStrategy = MatchStrategyNormalizedName
	default:
		plan.MatchStrategy = MatchStrategyPrefix
	}

	return plan
}

func matchScore(mediaNameLower string, mediaKeys map[string]struct{}, candidate folderSidecarCandidate) int {
	if strings.EqualFold(candidate.name, mediaNameLower+".json") {
		return 1200
	}

	if candidate.title != "" && strings.EqualFold(candidate.title, mediaNameLower) {
		return 1100
	}

	mediaExt := semanticExtension(mediaNameLower)
	titleExt := semanticExtension(candidate.title)
	nameExt := semanticExtension(candidate.name)

	if titleExt == mediaExt && sharesAnyKey(mediaKeys, candidate.titleKeys) {
		return 1000
	}

	if nameExt == mediaExt && sharesAnyKey(mediaKeys, candidate.nameKeys) {
		return 900
	}

	candidateTitle := candidate.title
	if candidateTitle == "" {
		candidateTitle = strings.TrimSuffix(candidate.name, filepath.Ext(candidate.name))
	}

	candidateExt := strings.ToLower(filepath.Ext(candidateTitle))
	if mediaExt == "" || candidateExt == "" || mediaExt != candidateExt {
		return 0
	}

	commonPrefix := commonPrefixLen(strings.ToLower(stripJSONSuffix(candidateTitle)), mediaNameLower)
	if commonPrefix >= 20 && (len(candidateTitle) >= 46 || len(mediaNameLower) >= 46) {
		return 700 + commonPrefix
	}

	return 0
}

func buildNameKeys(name string) []string {
	normalized := strings.ToLower(strings.TrimSpace(stripJSONSuffix(name)))
	if normalized == "" {
		return nil
	}

	keys := []string{normalized}

	ext := strings.ToLower(filepath.Ext(normalized))
	stem := strings.TrimSuffix(normalized, ext)
	keys = appendUniqueKey(keys, stem)

	if strings.Contains(stem, "-edited") {
		keys = appendUniqueKey(keys, strings.Replace(stem, "-edited", "", 1)+ext)
		keys = appendUniqueKey(keys, strings.Replace(stem, "-edited", "", 1))
	}

	if trailingDashDigitsRE.MatchString(stem) {
		match := trailingDashDigitsRE.FindStringSubmatch(stem)
		keys = appendUniqueKey(keys, match[1]+ext)
		keys = appendUniqueKey(keys, match[1])
	}

	if duplicateBeforeExtRE.MatchString(normalized) {
		match := duplicateBeforeExtRE.FindStringSubmatch(normalized)
		keys = appendUniqueKey(keys, match[1]+match[3])
		keys = appendUniqueKey(keys, match[1]+match[3]+"("+match[2]+")")
	}

	if duplicateAfterExtRE.MatchString(normalized) {
		match := duplicateAfterExtRE.FindStringSubmatch(normalized)
		keys = appendUniqueKey(keys, match[1]+"("+match[3]+")"+match[2])
		keys = appendUniqueKey(keys, match[1]+match[2])
	}

	if len(normalized) > 46 {
		keys = appendUniqueKey(keys, normalized[:46])
		if ext != "" && len(stem) > 46 {
			keys = appendUniqueKey(keys, stem[:46]+ext)
		}
	}

	return keys
}

func stripJSONSuffix(name string) string {
	if strings.EqualFold(filepath.Ext(name), ".json") {
		return strings.TrimSuffix(name, filepath.Ext(name))
	}
	return name
}

func keysToSet(keys []string) map[string]struct{} {
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		set[key] = struct{}{}
	}
	return set
}

func sharesAnyKey(left map[string]struct{}, right map[string]struct{}) bool {
	for key := range left {
		if _, ok := right[key]; ok {
			return true
		}
	}
	return false
}

func appendUniqueKey(keys []string, key string) []string {
	key = strings.TrimSpace(key)
	if key == "" {
		return keys
	}
	for _, existing := range keys {
		if existing == key {
			return keys
		}
	}
	return append(keys, key)
}

func commonPrefixLen(left string, right string) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	count := 0
	for i := 0; i < limit; i++ {
		if left[i] != right[i] {
			break
		}
		count++
	}
	return count
}

func semanticExtension(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(stripJSONSuffix(name)))
	if normalized == "" {
		return ""
	}
	if duplicateAfterExtRE.MatchString(normalized) {
		match := duplicateAfterExtRE.FindStringSubmatch(normalized)
		return match[2]
	}
	return filepath.Ext(normalized)
}
