package fixer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type StateStore struct {
	path      string
	file      *os.File
	mu        sync.RWMutex
	records   map[string]ProcessRecord
	hashIndex map[string]ProcessRecord
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
			store.records[record.SourceRelPath] = record
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
	return store, nil
}

func (s *StateStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}

func (s *StateStore) Get(sourceRelPath string) (ProcessRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[sourceRelPath]
	return record, ok
}

func (s *StateStore) Put(record ProcessRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := s.file.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := s.file.Sync(); err != nil {
		return err
	}

	s.records[record.SourceRelPath] = record
	if record.SourceHash != "" && record.Successful() && record.OutputPath != "" {
		if _, exists := s.hashIndex[record.SourceHash]; !exists {
			s.hashIndex[record.SourceHash] = record
		}
	}

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
