package fixer

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type SidecarIndex struct {
	byDir map[string][]folderSidecarCandidate
	count int
}

func NewSidecarIndex() *SidecarIndex {
	return &SidecarIndex{
		byDir: make(map[string][]folderSidecarCandidate),
	}
}

func (index *SidecarIndex) AddJSON(relativePath string, displayPath string, body []byte) error {
	if index == nil {
		return fmt.Errorf("sidecar index is nil")
	}
	relativePath = filepath.Clean(strings.TrimSpace(relativePath))
	if relativePath == "" || relativePath == "." {
		return fmt.Errorf("sidecar path is empty")
	}
	if !strings.EqualFold(filepath.Ext(relativePath), ".json") {
		return fmt.Errorf("sidecar path is not JSON: %s", relativePath)
	}

	var metadata imageMetadata
	metadataErr := json.Unmarshal(body, &metadata)
	candidate := folderSidecarCandidate{
		path:      strings.TrimSpace(displayPath),
		name:      filepath.Base(relativePath),
		nameKeys:  keysToSet(buildNameKeys(filepath.Base(relativePath))),
		titleKeys: make(map[string]struct{}),
	}
	if candidate.path == "" {
		candidate.path = relativePath
	}
	if metadataErr == nil {
		candidate.meta = &metadata
		candidate.title = strings.TrimSpace(metadata.BestTitle())
		candidate.titleKeys = keysToSet(buildNameKeys(candidate.title))
	}

	dirKey := sidecarDirKey(filepath.Dir(relativePath))
	index.byDir[dirKey] = append(index.byDir[dirKey], candidate)
	index.count++
	return metadataErr
}

func (index *SidecarIndex) Count() int {
	if index == nil {
		return 0
	}
	return index.count
}

func (index *SidecarIndex) candidatesForDir(relativeDir string) []folderSidecarCandidate {
	if index == nil {
		return nil
	}
	candidates := index.byDir[sidecarDirKey(relativeDir)]
	return append([]folderSidecarCandidate(nil), candidates...)
}

func sidecarDirKey(relativeDir string) string {
	clean := filepath.Clean(strings.TrimSpace(relativeDir))
	if clean == "." {
		clean = ""
	}
	return strings.ToLower(clean)
}
