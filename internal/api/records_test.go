package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecordStoreAddPersistenceFailureDoesNotPolluteMemory(t *testing.T) {
	volume := t.TempDir()
	store, err := openRecordStore(volume)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add("persisted"); err != nil {
		t.Fatal(err)
	}

	failingPath := filepath.Join(volume, "rename-target")
	if err := os.Mkdir(failingPath, 0o755); err != nil {
		t.Fatal(err)
	}
	store.path = failingPath

	if err := store.Add("not-persisted"); err == nil {
		t.Fatal("Add() error = nil, want persistence failure")
	}
	if !store.Has("persisted") {
		t.Fatal("existing record was removed after persistence failure")
	}
	if store.Has("not-persisted") {
		t.Fatal("failed record polluted in-memory state")
	}
	if _, err := os.Stat(failingPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file was not cleaned up: %v", err)
	}
}
