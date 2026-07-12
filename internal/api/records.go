package api

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type recordStore struct {
	mu   sync.RWMutex
	path string
	ids  map[string]string
}

func openRecordStore(volumeDir string) (*recordStore, error) {
	if err := os.MkdirAll(volumeDir, 0o755); err != nil {
		return nil, err
	}
	store := &recordStore{
		path: filepath.Join(volumeDir, "downloaded.json"),
		ids:  make(map[string]string),
	}
	content, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if len(content) > 0 {
		if err := json.Unmarshal(content, &store.ids); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (s *recordStore) Has(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.ids[id]
	return ok
}

func (s *recordStore) Add(id string) error {
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]string, len(s.ids)+1)
	for existingID, downloadedAt := range s.ids {
		next[existingID] = downloadedAt
	}
	next[id] = time.Now().UTC().Format(time.RFC3339)
	content, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	temporary := s.path + ".tmp"
	defer func() { _ = os.Remove(temporary) }()
	if err := os.WriteFile(temporary, append(content, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporary, s.path); err != nil {
		return err
	}
	s.ids = next
	return nil
}
