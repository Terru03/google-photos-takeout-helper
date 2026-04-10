package fixer

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type StateStore struct {
	path      string
	file      *os.File
	mu        sync.Mutex
	records   map[string]ProcessRecord
	hashIndex map[string]ProcessRecord
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
		scanner := bufio.NewScanner(existing)
		for scanner.Scan() {
			var record ProcessRecord
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				continue
			}
			store.records[record.SourceRelPath] = record
			if record.SourceHash != "" && record.Successful() && record.OutputPath != "" {
				if _, exists := store.hashIndex[record.SourceHash]; !exists {
					store.hashIndex[record.SourceHash] = record
				}
			}
		}
		_ = existing.Close()
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.hashIndex[hash]
	return record, ok
}
