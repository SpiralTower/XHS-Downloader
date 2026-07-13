package api

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSQLiteInitializationLegacyImportAndRestart(t *testing.T) {
	root := t.TempDir()
	volume := filepath.Join(root, "Volume")
	if err := os.MkdirAll(volume, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(volume, "downloaded.json"),
		[]byte("{\"legacy-note\":\"2025-01-02T03:04:05Z\"}\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	config := Config{
		VolumeDir:  volume,
		WebDistDir: filepath.Join(root, "dist"),
	}
	app, err := New(config, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}

	var foreignKeys, busyTimeout int
	var journalMode string
	if err := app.store.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if err := app.store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if err := app.store.db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 || journalMode != "wal" || busyTimeout != 5000 {
		t.Fatalf("SQLite pragmas = foreign_keys:%d journal_mode:%q busy_timeout:%d",
			foreignKeys, journalMode, busyTimeout)
	}
	imported, err := app.store.hasLegacyDownload(t.Context(), "legacy-note")
	if err != nil || !imported {
		t.Fatalf("legacy import = %t, err = %v", imported, err)
	}
	var migrationCount int
	if err := app.store.db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if migrationCount != 1 {
		t.Fatalf("migration count = %d", migrationCount)
	}
	databasePath := app.config.DatabasePath
	keyPath := app.config.SecretKeyPath
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{databasePath, keyPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if permissions := info.Mode().Perm(); permissions != 0o600 {
			t.Fatalf("%s permissions = %o", path, permissions)
		}
	}

	restarted, err := New(config, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	defer restarted.Close()
	if err := restarted.store.db.QueryRow(
		"SELECT COUNT(*) FROM schema_migrations",
	).Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if migrationCount != 1 {
		t.Fatalf("migration count after restart = %d", migrationCount)
	}
	imported, err = restarted.store.hasLegacyDownload(t.Context(), "legacy-note")
	if err != nil || !imported {
		t.Fatalf("legacy import after restart = %t, err = %v", imported, err)
	}
}

func TestConcurrentSQLiteInitialization(t *testing.T) {
	const (
		rounds  = 6
		workers = 4
	)
	for round := 0; round < rounds; round++ {
		root := filepath.Join(t.TempDir(), fmt.Sprintf("round-%d", round))
		config := (Config{VolumeDir: filepath.Join(root, "Volume")}).withDefaults()
		start := make(chan struct{})
		results := make(chan *appStore, workers)
		failures := make(chan error, workers)
		var wait sync.WaitGroup
		for worker := 0; worker < workers; worker++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				store, err := openAppStore(config)
				if err != nil {
					failures <- err
					return
				}
				results <- store
			}()
		}
		close(start)
		wait.Wait()
		close(results)
		close(failures)
		for err := range failures {
			t.Errorf("round %d concurrent startup failed: %v", round, err)
		}
		stores := make([]*appStore, 0, workers)
		for store := range results {
			stores = append(stores, store)
		}
		if len(stores) != workers {
			for _, store := range stores {
				_ = store.Close()
			}
			t.Fatalf("round %d opened %d stores, want %d", round, len(stores), workers)
		}
		var migrationCount int
		if err := stores[0].db.QueryRow(
			"SELECT COUNT(*) FROM schema_migrations",
		).Scan(&migrationCount); err != nil {
			t.Fatal(err)
		}
		if migrationCount != 1 {
			t.Fatalf("round %d migration count = %d", round, migrationCount)
		}
		for _, store := range stores {
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
		}
		key, err := os.ReadFile(config.SecretKeyPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(key) != 32 {
			t.Fatalf("round %d secret key length = %d", round, len(key))
		}
	}
}

func TestParseRunsRecoveredAndPrunedOnRestart(t *testing.T) {
	root := t.TempDir()
	config := Config{
		VolumeDir:  filepath.Join(root, "Volume"),
		WebDistDir: filepath.Join(root, "dist"),
	}
	app, err := New(config, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.db.Exec(`
		WITH RECURSIVE numbers(value) AS (
			SELECT 1
			UNION ALL
			SELECT value + 1 FROM numbers WHERE value < ?
		)
		INSERT INTO parse_runs(
			requested_url, status, source, cookie_source, proxy_source,
			started_at, finished_at, error
		)
		SELECT 'https://example.invalid/' || value, 'failed', '',
		       'none', 'none', ?, ?, 'TEST_FAILURE'
		FROM numbers
	`, maxParseRuns+5, unixMillis(time.Now()), unixMillis(time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.db.Exec(`
		INSERT INTO parse_runs(
			requested_url, status, source, cookie_source, proxy_source, started_at
		) VALUES ('https://example.invalid/interrupted', 'running', '', 'none', 'none', ?)
	`, unixMillis(time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := New(config, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	var total, running, restartedFailures int
	if err := restarted.store.db.QueryRow("SELECT COUNT(*) FROM parse_runs").Scan(&total); err != nil {
		t.Fatal(err)
	}
	if err := restarted.store.db.QueryRow(
		"SELECT COUNT(*) FROM parse_runs WHERE status = 'running'",
	).Scan(&running); err != nil {
		t.Fatal(err)
	}
	if err := restarted.store.db.QueryRow(
		"SELECT COUNT(*) FROM parse_runs WHERE error = 'SERVICE_RESTARTED'",
	).Scan(&restartedFailures); err != nil {
		t.Fatal(err)
	}
	if total != maxParseRuns || running != 0 || restartedFailures != 1 {
		t.Fatalf("parse runs after restart = total:%d running:%d restarted:%d",
			total, running, restartedFailures)
	}
}
