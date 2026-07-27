package batch

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Manifest struct {
	path   string
	file   *os.File
	latest map[string]ManifestEntry
}

func OpenManifest(path string) (*Manifest, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	manifest := &Manifest{
		path:   path,
		latest: make(map[string]ManifestEntry),
	}

	if existing, err := os.Open(path); err == nil {
		scanner := bufio.NewScanner(existing)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			var entry ManifestEntry
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				continue
			}
			entry = normalizeManifestEntry(entry)
			if entry.ZipFingerprint != "" {
				manifest.latest[entry.ZipFingerprint] = entry
			}
		}
		if closeErr := existing.Close(); closeErr != nil {
			return nil, closeErr
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	manifest.file = file
	return manifest, nil
}

func (m *Manifest) Path() string {
	return m.path
}

func (m *Manifest) AlreadySuccessful(item ZipItem) bool {
	entry, ok := m.latest[item.Fingerprint]
	return ok && entry.WorkflowVersion == currentWorkflowVersion && isSuccessfulStatus(entry.Status)
}

func (m *Manifest) LegacySuccessfulCount(items []ZipItem) int {
	count := 0
	for _, item := range items {
		entry, ok := m.latest[item.Fingerprint]
		if ok && entry.WorkflowVersion != currentWorkflowVersion && isSuccessfulStatus(entry.Status) {
			count++
		}
	}
	return count
}

func (m *Manifest) LastEntry(item ZipItem) (ManifestEntry, bool) {
	entry, ok := m.latest[item.Fingerprint]
	return entry, ok
}

func (m *Manifest) MarkInterrupted(items []ZipItem) error {
	for _, item := range items {
		entry, ok := m.LastEntry(item)
		if !ok || !isActiveStatus(entry.Status) {
			continue
		}
		entry.Status = statusInterrupted
		entry.EndTime = nowUTC()
		entry.Error = "previous run ended before this ZIP finished"
		if err := m.Append(entry); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manifest) Append(entry ManifestEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := m.file.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := m.file.Sync(); err != nil {
		return err
	}
	entry = normalizeManifestEntry(entry)
	if entry.ZipFingerprint != "" {
		m.latest[entry.ZipFingerprint] = entry
	}
	return nil
}

func (m *Manifest) Close() error {
	if m.file == nil {
		return nil
	}
	err := m.file.Close()
	m.file = nil
	return err
}

func manifestPath(outputDir string) string {
	return filepath.Join(outputDir, ".gtf", "batch", "manifest.jsonl")
}

func manifestEntryFor(item ZipItem, outputDir string, status string) ManifestEntry {
	return ManifestEntry{
		WorkflowVersion: currentWorkflowVersion,
		ZipName:         item.Name,
		ZipPath:         item.Path,
		ZipSize:         item.SizeBytes,
		ZipModified:     item.ModTime,
		ZipFingerprint:  item.Fingerprint,
		SourceDrive:     item.SourceDrive,
		Status:          status,
		OutputFolder:    outputDir,
	}
}

func isActiveStatus(status string) bool {
	return status == statusPending || status == statusExtracting || status == statusProcessing
}

func isSuccessfulStatus(status string) bool {
	return status == statusCompleted || status == statusCompletedReview
}

func normalizeManifestEntry(entry ManifestEntry) ManifestEntry {
	switch entry.Status {
	case "success":
		entry.Status = statusCompleted
	case "needs-review":
		entry.Status = statusCompletedReview
	case "error":
		entry.Status = statusFailed
	case "started", "planned":
		entry.Status = statusInterrupted
	}
	return entry
}

func nowUTC() time.Time {
	return time.Now().UTC()
}
