package fixer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const stateSyncEvery = 128

type StateStore struct {
	path      string
	file      *os.File
	writer    *bufio.Writer
	mu        sync.RWMutex
	records   map[string]ProcessRecord
	hashIndex map[string]ProcessRecord
	pending   int
	lastSync  time.Time
	Warnings  []string
}

func OpenStateStore(path string) (*StateStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	store := &StateStore{
		path:      path,
		records:   make(map[string]ProcessRecord),
		hashIndex: make(map[string]ProcessRecord),
	}

	if existing, err := os.Open(path); err == nil {
		defer func() {
			if closeErr := existing.Close(); closeErr != nil {
				store.Warnings = append(store.Warnings, fmt.Sprintf("close state reader: %v", closeErr))
			}
		}()

		scanner := bufio.NewScanner(existing)
		scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			var record ProcessRecord
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				store.Warnings = append(store.Warnings, fmt.Sprintf("ignored corrupt state line %d: %v", lineNumber, err))
				continue
			}
			if record.SourceRelPath == "" {
				store.Warnings = append(store.Warnings, fmt.Sprintf("ignored state line %d with empty sourceRelPath", lineNumber))
				continue
			}
			store.records[record.StateKey()] = record
			if record.SourceHash != "" && record.Successful() && record.OutputPath != "" {
				if _, exists := store.hashIndex[record.SourceHash]; !exists {
					store.hashIndex[record.SourceHash] = record
				}
			}
		}
		if err := scanner.Err(); err != nil {
			store.Warnings = append(store.Warnings, fmt.Sprintf("state scan error: %v", err))
		}
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	store.file = file
	store.writer = bufio.NewWriterSize(file, 256*1024)
	store.lastSync = time.Now()
	return store, nil
}

func (s *StateStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file == nil {
		return nil
	}
	var firstErr error
	if s.writer != nil {
		if err := s.writer.Flush(); err != nil {
			firstErr = err
		}
	}
	if s.pending > 0 {
		if err := s.file.Sync(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := s.file.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	s.file = nil
	s.writer = nil
	return firstErr
}

func (s *StateStore) Get(sourceRelPath string) (ProcessRecord, bool) {
	return s.GetForSource("", sourceRelPath)
}

func (s *StateStore) GetForSource(sourceID string, sourceRelPath string) (ProcessRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := sourceRelPath
	if sourceID != "" {
		key = sourceID + "|" + sourceRelPath
	}
	record, ok := s.records[key]
	return record, ok
}

func (s *StateStore) Put(record ProcessRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if s.writer == nil {
		return fmt.Errorf("state store is closed")
	}
	if _, err := s.writer.Write(append(data, '\n')); err != nil {
		return err
	}
	s.pending++
	if s.pending >= stateSyncEvery || time.Since(s.lastSync) >= 2*time.Second {
		if err := s.flushLocked(); err != nil {
			return err
		}
	}

	s.records[record.StateKey()] = record
	if record.SourceHash != "" && record.Successful() && record.OutputPath != "" {
		if _, exists := s.hashIndex[record.SourceHash]; !exists {
			s.hashIndex[record.SourceHash] = record
		}
	}

	return nil
}

func (s *StateStore) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushLocked()
}

func (s *StateStore) flushLocked() error {
	if s.file == nil || s.writer == nil {
		return nil
	}
	if err := s.writer.Flush(); err != nil {
		return err
	}
	if err := s.file.Sync(); err != nil {
		return err
	}
	s.pending = 0
	s.lastSync = time.Now()
	return nil
}

func (s *StateStore) CanonicalByHash(hash string) (ProcessRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.hashIndex[hash]
	return record, ok
}

func (s *StateStore) Records() []ProcessRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := make([]ProcessRecord, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, record)
	}
	sort.SliceStable(records, func(i int, j int) bool {
		return records[i].SourceRelPath < records[j].SourceRelPath
	})
	return records
}
