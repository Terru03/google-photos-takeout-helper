package batch

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
)

type Manifest struct {
	path    string
	file    *os.File
	success map[string]ManifestEntry
}

func OpenManifest(path string) (*Manifest, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	manifest := &Manifest{
		path:    path,
		success: make(map[string]ManifestEntry),
	}

	if existing, err := os.Open(path); err == nil {
		scanner := bufio.NewScanner(existing)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			var entry ManifestEntry
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				continue
			}
			if entry.ZipFingerprint == "" {
				continue
			}
			if entry.Status == statusSuccess {
				manifest.success[entry.ZipFingerprint] = entry
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
	_, ok := m.success[item.Fingerprint]
	return ok
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
	if entry.Status == statusSuccess && entry.ZipFingerprint != "" {
		m.success[entry.ZipFingerprint] = entry
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
	return filepath.Join(outputDir, ".gtf", "batch_manifest.jsonl")
}

func manifestEntryFor(item ZipItem, outputDir string, status string) ManifestEntry {
	return ManifestEntry{
		ZipName:        item.Name,
		ZipPath:        item.Path,
		ZipFingerprint: item.Fingerprint,
		SourceDrive:    item.SourceDrive,
		Status:         status,
		OutputFolder:   outputDir,
	}
}
