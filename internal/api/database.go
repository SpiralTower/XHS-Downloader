package api

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	sqliteDriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const (
	maxParseRuns               = 10_000
	sqliteStartupRetryAttempts = 6
)

const databaseSchemaV1 = `
CREATE TABLE app_settings (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	revision INTEGER NOT NULL CHECK (revision > 0),
	public_enabled INTEGER NOT NULL CHECK (public_enabled IN (0, 1)),
	save_text INTEGER NOT NULL CHECK (save_text IN (0, 1)),
	save_images INTEGER NOT NULL CHECK (save_images IN (0, 1)),
	save_videos INTEGER NOT NULL CHECK (save_videos IN (0, 1)),
	refetch_existing INTEGER NOT NULL CHECK (refetch_existing IN (0, 1)),
	default_cookie TEXT,
	default_proxy TEXT,
	updated_at INTEGER NOT NULL
) STRICT;

INSERT INTO app_settings (
	id, revision, public_enabled, save_text, save_images, save_videos,
	refetch_existing, default_cookie, default_proxy, updated_at
) VALUES (1, 1, 1, 0, 0, 0, 1, NULL, NULL, 0);

CREATE TABLE admin_sessions (
	token_hash BLOB PRIMARY KEY CHECK (length(token_hash) = 32),
	credential_fingerprint BLOB NOT NULL CHECK (length(credential_fingerprint) = 32),
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	last_seen_at INTEGER NOT NULL
) STRICT;

CREATE INDEX admin_sessions_expiry_idx ON admin_sessions(expires_at);

CREATE TABLE works (
	id INTEGER PRIMARY KEY,
	platform_id TEXT NOT NULL UNIQUE CHECK (length(platform_id) BETWEEN 1 AND 256),
	first_seen_at INTEGER NOT NULL,
	last_seen_at INTEGER NOT NULL
) STRICT;

CREATE TABLE work_versions (
	id INTEGER PRIMARY KEY,
	work_id INTEGER NOT NULL REFERENCES works(id) ON DELETE CASCADE,
	version_number INTEGER NOT NULL CHECK (version_number > 0),
	content_hash BLOB NOT NULL CHECK (length(content_hash) = 32),
	captured_at INTEGER NOT NULL,
	data_json TEXT NOT NULL CHECK (json_valid(data_json)),
	UNIQUE(work_id, version_number),
	UNIQUE(work_id, content_hash)
) STRICT;

CREATE INDEX work_versions_latest_idx
	ON work_versions(work_id, version_number DESC);

CREATE TABLE work_cache_scopes (
	work_id INTEGER NOT NULL REFERENCES works(id) ON DELETE CASCADE,
	cache_scope BLOB NOT NULL CHECK (length(cache_scope) = 32),
	version_id INTEGER NOT NULL REFERENCES work_versions(id) ON DELETE CASCADE,
	observed_at INTEGER NOT NULL,
	PRIMARY KEY(work_id, cache_scope)
) STRICT;

CREATE TABLE parse_runs (
	id INTEGER PRIMARY KEY,
	requested_url TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
	source TEXT NOT NULL CHECK (source IN ('', 'fetched', 'cache', 'skipped')),
	cookie_source TEXT NOT NULL CHECK (cookie_source IN ('none', 'default', 'override', 'disabled')),
	proxy_source TEXT NOT NULL CHECK (proxy_source IN ('none', 'default', 'override', 'disabled')),
	started_at INTEGER NOT NULL,
	finished_at INTEGER,
	work_id INTEGER REFERENCES works(id) ON DELETE SET NULL,
	version_id INTEGER REFERENCES work_versions(id) ON DELETE SET NULL,
	error TEXT
) STRICT;

CREATE INDEX parse_runs_history_idx ON parse_runs(id DESC);
CREATE INDEX parse_runs_work_idx ON parse_runs(work_id, id DESC);

CREATE TABLE version_resources (
	id INTEGER PRIMARY KEY,
	version_id INTEGER NOT NULL REFERENCES work_versions(id) ON DELETE CASCADE,
	kind TEXT NOT NULL CHECK (kind IN ('text', 'image', 'video')),
	ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
	remote_url TEXT NOT NULL DEFAULT '',
	save_status TEXT NOT NULL CHECK (save_status IN ('pending', 'disabled', 'stored', 'failed')),
	relative_path TEXT,
	mime_type TEXT,
	size_bytes INTEGER CHECK (size_bytes IS NULL OR size_bytes >= 0),
	sha256 TEXT,
	save_error TEXT,
	UNIQUE(version_id, kind, ordinal)
) STRICT;

CREATE TABLE legacy_download_records (
	platform_id TEXT PRIMARY KEY CHECK (length(platform_id) BETWEEN 1 AND 256),
	downloaded_at INTEGER NOT NULL,
	source TEXT NOT NULL
) STRICT;
`

const databaseSchemaV2 = `
ALTER TABLE app_settings
	ADD COLUMN show_popular INTEGER NOT NULL DEFAULT 0 CHECK (show_popular IN (0, 1));

ALTER TABLE works
	ADD COLUMN parse_count INTEGER NOT NULL DEFAULT 0 CHECK (parse_count >= 0);

ALTER TABLE works
	ADD COLUMN last_parsed_at INTEGER;

CREATE TABLE work_parse_daily (
	work_id INTEGER NOT NULL REFERENCES works(id) ON DELETE CASCADE,
	day_start INTEGER NOT NULL CHECK (day_start % 86400000 = 0),
	parse_count INTEGER NOT NULL CHECK (parse_count >= 0),
	PRIMARY KEY(work_id, day_start)
) STRICT;

CREATE INDEX works_last_parsed_idx
	ON works(last_parsed_at DESC, id DESC);

CREATE INDEX works_popular_idx
	ON works(parse_count DESC, last_parsed_at DESC, id DESC);

CREATE INDEX work_parse_daily_day_idx
	ON work_parse_daily(day_start, work_id);

UPDATE works
SET parse_count = (
		SELECT COUNT(*)
		FROM parse_runs r
		WHERE r.work_id = works.id
		  AND r.status = 'succeeded'
		  AND r.source IN ('fetched', 'cache')
	),
	last_parsed_at = (
		SELECT MAX(r.finished_at)
		FROM parse_runs r
		WHERE r.work_id = works.id
		  AND r.status = 'succeeded'
		  AND r.source IN ('fetched', 'cache')
	);

INSERT INTO work_parse_daily(work_id, day_start, parse_count)
SELECT work_id, (finished_at / 86400000) * 86400000, COUNT(*)
FROM parse_runs
WHERE work_id IS NOT NULL
  AND finished_at IS NOT NULL
  AND status = 'succeeded'
  AND source IN ('fetched', 'cache')
GROUP BY work_id, (finished_at / 86400000) * 86400000;
`

var (
	errNoCachedVersion          = errors.New("no cached work version")
	errSettingsRevisionConflict = errors.New("settings revision conflict")
)

type appStore struct {
	db      *sql.DB
	secrets *secretCipher
}

type runtimeSettings struct {
	Revision      int64
	Public        bool
	ShowPopular   bool
	SaveText      bool
	SaveImages    bool
	SaveVideos    bool
	Refetch       bool
	DefaultCookie *string
	DefaultProxy  *string
	UpdatedAt     time.Time
}

type secretMutation struct {
	Action string
	Value  string
}

type settingsUpdate struct {
	Revision      int64
	Public        *bool
	ShowPopular   *bool
	SaveText      *bool
	SaveImages    *bool
	SaveVideos    *bool
	Refetch       *bool
	DefaultCookie *secretMutation
	DefaultProxy  *secretMutation
}

type storedResource struct {
	ID           int64  `json:"id"`
	Kind         string `json:"kind"`
	Ordinal      int    `json:"ordinal"`
	RemoteURL    string `json:"remote_url"`
	SaveStatus   string `json:"save_status"`
	RelativePath string `json:"-"`
	MIMEType     string `json:"mime_type,omitempty"`
	SizeBytes    int64  `json:"size_bytes,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	SaveError    string `json:"save_error,omitempty"`
}

type storedVersion struct {
	ID            int64
	WorkID        int64
	PlatformID    string
	VersionNumber int64
	CapturedAt    time.Time
	Data          map[string]any
	Resources     []storedResource
}

type secretCipher struct {
	aead     cipher.AEAD
	scopeKey [sha256.Size]byte
}

func openAppStore(config Config) (*appStore, error) {
	if err := validateDatabasePath(config.VolumeDir, config.DatabasePath); err != nil {
		return nil, err
	}
	databasePath, err := filepath.Abs(config.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	dataDir := filepath.Dir(databasePath)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure database directory: %w", err)
	}
	if err := verifyWritableDirectory(dataDir); err != nil {
		return nil, err
	}

	secrets, err := loadOrCreateSecretCipher(config.SecretKeyPath, config.SecretKeyManaged)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", sqliteDSN(databasePath))
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &appStore{db: db, secrets: secrets}
	cleanup := func(cause error) (*appStore, error) {
		return nil, errors.Join(cause, db.Close())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := retrySQLiteStartup(ctx, func() error { return db.PingContext(ctx) }); err != nil {
		return cleanup(fmt.Errorf("ping sqlite database: %w", err))
	}
	if err := retrySQLiteStartup(ctx, func() error { return store.migrate(ctx) }); err != nil {
		return cleanup(err)
	}
	if err := retrySQLiteStartup(ctx, func() error { return store.recoverAndPruneParseRuns(ctx) }); err != nil {
		return cleanup(fmt.Errorf("recover parse runs: %w", err))
	}
	if err := retrySQLiteStartup(ctx, func() error {
		return store.importDownloadedJSON(ctx, filepath.Join(config.VolumeDir, "downloaded.json"))
	}); err != nil {
		return cleanup(err)
	}
	if err := retrySQLiteStartup(ctx, func() error {
		_, err := store.loadSettings(ctx)
		return err
	}); err != nil {
		return cleanup(fmt.Errorf("load application settings: %w", err))
	}
	if err := os.Chmod(databasePath, 0o600); err != nil {
		return cleanup(fmt.Errorf("secure database file: %w", err))
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	return store, nil
}

func sqliteDSN(path string) string {
	uri := &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := uri.Query()
	for _, pragma := range []string{
		"busy_timeout(5000)",
		"foreign_keys(1)",
		"journal_mode(WAL)",
		"synchronous(NORMAL)",
	} {
		query.Add("_pragma", pragma)
	}
	query.Set("_txlock", "immediate")
	query.Set("_time_integer_format", "unix_milli")
	query.Set("_dqs", "false")
	uri.RawQuery = query.Encode()
	return uri.String()
}

func retrySQLiteStartup(ctx context.Context, operation func() error) error {
	delay := 25 * time.Millisecond
	var lastErr error
	for attempt := 0; attempt < sqliteStartupRetryAttempts; attempt++ {
		if err := operation(); err == nil {
			return nil
		} else if !isSQLiteBusy(err) {
			return err
		} else {
			lastErr = err
		}
		if attempt == sqliteStartupRetryAttempts-1 {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return errors.Join(ctx.Err(), lastErr)
		case <-timer.C:
		}
		if delay < 400*time.Millisecond {
			delay *= 2
		}
	}
	return lastErr
}

func isSQLiteBusy(err error) bool {
	var sqliteErr *sqliteDriver.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	switch sqliteErr.Code() & 0xff {
	case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED:
		return true
	default:
		return false
	}
}

func (s *appStore) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			checksum TEXT NOT NULL,
			applied_at INTEGER NOT NULL
		) STRICT;
	`); err != nil {
		return fmt.Errorf("create schema migrations table: %w", err)
	}
	type databaseMigration struct {
		version int
		name    string
		schema  string
	}
	migrations := []databaseMigration{
		{version: 1, name: "initial application database", schema: databaseSchemaV1},
		{version: 2, name: "work statistics and popular display setting", schema: databaseSchemaV2},
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		SELECT version, checksum FROM schema_migrations ORDER BY version
	`)
	if err != nil {
		return fmt.Errorf("read schema migrations: %w", err)
	}
	nextVersion := 1
	for rows.Next() {
		var version int
		var existingChecksum string
		if err := rows.Scan(&version, &existingChecksum); err != nil {
			rows.Close()
			return fmt.Errorf("scan schema migration: %w", err)
		}
		if version != nextVersion {
			rows.Close()
			return fmt.Errorf("database migrations are not contiguous at version %d", nextVersion)
		}
		if version > len(migrations) {
			rows.Close()
			return fmt.Errorf("database migration %d is newer than the executable", version)
		}
		sum := sha256.Sum256([]byte(migrations[version-1].schema))
		if existingChecksum != hex.EncodeToString(sum[:]) {
			rows.Close()
			return fmt.Errorf("database migration %d checksum does not match the executable", version)
		}
		nextVersion++
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return fmt.Errorf("read schema migrations: %w", err)
	}
	for _, migration := range migrations[nextVersion-1:] {
		if migration.version != nextVersion {
			return fmt.Errorf("executable migrations are not contiguous at version %d", nextVersion)
		}
		if _, err := tx.ExecContext(ctx, migration.schema); err != nil {
			return fmt.Errorf("apply schema migration %d: %w", migration.version, err)
		}
		sum := sha256.Sum256([]byte(migration.schema))
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO schema_migrations(version, name, checksum, applied_at)
			VALUES (?, ?, ?, ?)
		`, migration.version, migration.name, hex.EncodeToString(sum[:]), unixMillis(time.Now())); err != nil {
			return fmt.Errorf("record schema migration %d: %w", migration.version, err)
		}
		nextVersion++
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migrations: %w", err)
	}
	return nil
}

func (s *appStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *appStore) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("database is unavailable")
	}
	return s.db.PingContext(ctx)
}

func loadOrCreateSecretCipher(path string, managed bool) (*secretCipher, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve secret key path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return nil, fmt.Errorf("create secret key directory: %w", err)
	}
	key, err := os.ReadFile(absolute)
	if errors.Is(err, os.ErrNotExist) && managed {
		key, err = publishSecretKey(absolute)
	}
	if err != nil {
		return nil, fmt.Errorf("read settings encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("settings encryption key must contain exactly 32 bytes, got %d", len(key))
	}
	if managed {
		if err := os.Chmod(absolute, 0o600); err != nil {
			return nil, fmt.Errorf("secure settings encryption key: %w", err)
		}
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize settings encryption: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize settings encryption envelope: %w", err)
	}
	secrets := &secretCipher{aead: aead}
	copy(secrets.scopeKey[:], key)
	return secrets, nil
}

func publishSecretKey(path string) ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate settings encryption key: %w", err)
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".secrets-key-*")
	if err != nil {
		return nil, fmt.Errorf("create temporary settings encryption key: %w", err)
	}
	temporaryPath := temporary.Name()
	temporaryPresent := true
	defer func() {
		if temporaryPresent {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return nil, fmt.Errorf("secure temporary settings encryption key: %w", err)
	}
	written, writeErr := temporary.Write(key)
	if writeErr == nil && written != len(key) {
		writeErr = io.ErrShortWrite
	}
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return nil, fmt.Errorf("persist temporary settings encryption key: %w", err)
	}

	if err := os.Link(temporaryPath, path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("publish settings encryption key: %w", err)
		}
		winner, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("read published settings encryption key: %w", readErr)
		}
		return winner, nil
	}
	if err := syncDirectory(directory); err != nil {
		return nil, fmt.Errorf("sync settings encryption key directory: %w", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		return nil, fmt.Errorf("remove temporary settings encryption key: %w", err)
	}
	temporaryPresent = false
	if err := syncDirectory(directory); err != nil {
		return nil, fmt.Errorf("sync settings encryption key cleanup: %w", err)
	}
	return key, nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func (c *secretCipher) encrypt(label, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(value), []byte(label))
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (c *secretCipher) decrypt(label, envelope string) (*string, error) {
	if envelope == "" {
		return nil, nil
	}
	content, err := base64.RawStdEncoding.DecodeString(envelope)
	if err != nil {
		return nil, errors.New("encrypted setting is malformed")
	}
	nonceSize := c.aead.NonceSize()
	if len(content) <= nonceSize {
		return nil, errors.New("encrypted setting is truncated")
	}
	plain, err := c.aead.Open(nil, content[:nonceSize], content[nonceSize:], []byte(label))
	if err != nil {
		return nil, errors.New("encrypted setting cannot be decrypted with the configured key")
	}
	value := string(plain)
	return &value, nil
}

func (c *secretCipher) cacheScope(cookie, proxy *string, resolvedURL string) [sha256.Size]byte {
	mac := hmac.New(sha256.New, c.scopeKey[:])
	_, _ = mac.Write([]byte("xhs-cache-scope-v2"))
	writeValue := func(value *string) {
		if value == nil {
			_, _ = mac.Write([]byte{0})
			return
		}
		_, _ = mac.Write([]byte{1})
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(*value)))
		_, _ = mac.Write(length[:])
		_, _ = mac.Write([]byte(*value))
	}
	writeValue(cookie)
	writeValue(proxy)
	authorizationContext := normalizedCacheAuthorizationURL(resolvedURL)
	writeValue(&authorizationContext)
	var scope [sha256.Size]byte
	copy(scope[:], mac.Sum(nil))
	return scope
}
func normalizedCacheAuthorizationURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return strings.SplitN(raw, "#", 2)[0]
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" && !(scheme == "https" && port == "443") {
		host += ":" + port
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	query := parsed.RawQuery
	if values, parseErr := url.ParseQuery(query); parseErr == nil {
		query = values.Encode()
	}
	normalized := scheme + "://" + host + path
	if query != "" {
		normalized += "?" + query
	}
	return normalized
}

func (s *appStore) loadSettings(ctx context.Context) (runtimeSettings, error) {
	return s.scanSettings(s.db.QueryRowContext(ctx, `
		SELECT revision, public_enabled, show_popular, save_text, save_images, save_videos,
		       refetch_existing, COALESCE(default_cookie, ''), COALESCE(default_proxy, ''), updated_at
		FROM app_settings WHERE id = 1
	`))
}

type rowScanner interface {
	Scan(...any) error
}

func (s *appStore) scanSettings(row rowScanner) (runtimeSettings, error) {
	var settings runtimeSettings
	var public, showPopular, text, images, videos, refetch int
	var cookieEnvelope, proxyEnvelope string
	var updatedAt int64
	if err := row.Scan(
		&settings.Revision, &public, &showPopular, &text, &images, &videos, &refetch,
		&cookieEnvelope, &proxyEnvelope, &updatedAt,
	); err != nil {
		return runtimeSettings{}, err
	}
	cookie, err := s.secrets.decrypt("default_cookie", cookieEnvelope)
	if err != nil {
		return runtimeSettings{}, err
	}
	proxy, err := s.secrets.decrypt("default_proxy", proxyEnvelope)
	if err != nil {
		return runtimeSettings{}, err
	}
	settings.Public = public != 0
	settings.ShowPopular = showPopular != 0
	settings.SaveText = text != 0
	settings.SaveImages = images != 0
	settings.SaveVideos = videos != 0
	settings.Refetch = refetch != 0
	settings.DefaultCookie = cookie
	settings.DefaultProxy = proxy
	settings.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	return settings, nil
}

func (s *appStore) updateSettings(ctx context.Context, update settingsUpdate) (runtimeSettings, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return runtimeSettings{}, err
	}
	defer tx.Rollback()
	var revision int64
	var public, showPopular, text, images, videos, refetch int
	var cookieEnvelope, proxyEnvelope string
	if err := tx.QueryRowContext(ctx, `
		SELECT revision, public_enabled, show_popular, save_text, save_images, save_videos,
		       refetch_existing, COALESCE(default_cookie, ''), COALESCE(default_proxy, '')
		FROM app_settings WHERE id = 1
	`).Scan(
		&revision, &public, &showPopular, &text, &images, &videos, &refetch,
		&cookieEnvelope, &proxyEnvelope,
	); err != nil {
		return runtimeSettings{}, err
	}
	if update.Revision != revision {
		return runtimeSettings{}, errSettingsRevisionConflict
	}
	applyBool := func(target *int, value *bool) {
		if value == nil {
			return
		}
		if *value {
			*target = 1
		} else {
			*target = 0
		}
	}
	applyBool(&public, update.Public)
	applyBool(&showPopular, update.ShowPopular)
	applyBool(&text, update.SaveText)
	applyBool(&images, update.SaveImages)
	applyBool(&videos, update.SaveVideos)
	applyBool(&refetch, update.Refetch)
	cookieEnvelope, err = s.applySecretMutation("default_cookie", cookieEnvelope, update.DefaultCookie)
	if err != nil {
		return runtimeSettings{}, err
	}
	proxyEnvelope, err = s.applySecretMutation("default_proxy", proxyEnvelope, update.DefaultProxy)
	if err != nil {
		return runtimeSettings{}, err
	}
	now := unixMillis(time.Now())
	result, err := tx.ExecContext(ctx, `
		UPDATE app_settings
		SET revision = revision + 1,
		    public_enabled = ?, show_popular = ?, save_text = ?, save_images = ?, save_videos = ?,
		    refetch_existing = ?, default_cookie = NULLIF(?, ''),
		    default_proxy = NULLIF(?, ''), updated_at = ?
		WHERE id = 1 AND revision = ?
	`, public, showPopular, text, images, videos, refetch, cookieEnvelope, proxyEnvelope, now, revision)
	if err != nil {
		return runtimeSettings{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return runtimeSettings{}, err
	}
	if affected != 1 {
		return runtimeSettings{}, errSettingsRevisionConflict
	}
	if err := tx.Commit(); err != nil {
		return runtimeSettings{}, err
	}
	return s.loadSettings(ctx)
}

func (s *appStore) applySecretMutation(label, current string, mutation *secretMutation) (string, error) {
	if mutation == nil || mutation.Action == "" || mutation.Action == "keep" {
		return current, nil
	}
	switch mutation.Action {
	case "clear":
		return "", nil
	case "replace":
		return s.secrets.encrypt(label, mutation.Value)
	default:
		return "", fmt.Errorf("unsupported secret action %q", mutation.Action)
	}
}

func (s *appStore) createSession(ctx context.Context, tokenHash, credentialFingerprint []byte, createdAt, expiresAt time.Time) error {
	if len(tokenHash) != sha256.Size || len(credentialFingerprint) != sha256.Size {
		return errors.New("invalid session hash length")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO admin_sessions(
			token_hash, credential_fingerprint, created_at, expires_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?)
	`, tokenHash, credentialFingerprint, unixMillis(createdAt), unixMillis(expiresAt), unixMillis(createdAt))
	return err
}

func (s *appStore) sessionValid(ctx context.Context, tokenHash, credentialFingerprint []byte, now time.Time) (time.Time, bool, error) {
	var expiresAt int64
	err := s.db.QueryRowContext(ctx, `
		SELECT expires_at FROM admin_sessions
		WHERE token_hash = ? AND credential_fingerprint = ? AND expires_at > ?
	`, tokenHash, credentialFingerprint, unixMillis(now)).Scan(&expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	_, _ = s.db.ExecContext(ctx,
		"UPDATE admin_sessions SET last_seen_at = ? WHERE token_hash = ? AND last_seen_at < ?",
		unixMillis(now), tokenHash, unixMillis(now.Add(-5*time.Minute)),
	)
	return time.UnixMilli(expiresAt).UTC(), true, nil
}

func (s *appStore) deleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM admin_sessions WHERE token_hash = ?", tokenHash)
	return err
}

func (s *appStore) purgeExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM admin_sessions WHERE expires_at <= ?", unixMillis(now))
	return err
}

func (s *appStore) beginParseRun(ctx context.Context, requestedURL, cookieSource, proxySource string) (int64, error) {
	if len(requestedURL) == 0 || len(requestedURL) > maxRequestedURLBytes {
		return 0, errors.New("requested URL length is invalid")
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO parse_runs(
			requested_url, status, source, cookie_source, proxy_source, started_at
		) VALUES (?, 'running', '', ?, ?, ?)
	`, requestedURL, cookieSource, proxySource, unixMillis(time.Now()))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

type contextExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *appStore) recoverAndPruneParseRuns(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE parse_runs
		SET status = 'failed', finished_at = ?, error = 'SERVICE_RESTARTED'
		WHERE status = 'running'
	`, unixMillis(time.Now())); err != nil {
		return err
	}
	if err := pruneCompletedParseRuns(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func pruneCompletedParseRuns(ctx context.Context, executor contextExecutor) error {
	_, err := executor.ExecContext(ctx, `
		DELETE FROM parse_runs
		WHERE status <> 'running'
		  AND id NOT IN (
			SELECT id FROM parse_runs
			WHERE status <> 'running'
			ORDER BY id DESC
			LIMIT ?
		  )
	`, maxParseRuns)
	return err
}

func sanitizeHistoryErrorCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 64 {
		return "EXTRACTION_FAILED"
	}
	for _, character := range code {
		if (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '_' {
			return "EXTRACTION_FAILED"
		}
	}
	return code
}

func completeSuccessfulParseRun(
	ctx context.Context,
	tx *sql.Tx,
	runID int64,
	source string,
	workID, versionID int64,
	finishedAt time.Time,
) (bool, error) {
	finishedAt = finishedAt.UTC()
	finishedMillis := unixMillis(finishedAt)
	result, err := tx.ExecContext(ctx, `
		UPDATE parse_runs
		SET status = 'succeeded', source = ?, finished_at = ?,
		    work_id = ?, version_id = ?, error = NULL
		WHERE id = ? AND status = 'running'
	`, source, finishedMillis, workID, versionID, runID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	if affected != 1 {
		return false, fmt.Errorf("complete parse run %d: updated %d rows", runID, affected)
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE works
		SET parse_count = parse_count + 1,
		    last_parsed_at = CASE
		        WHEN last_parsed_at IS NULL OR last_parsed_at < ? THEN ?
		        ELSE last_parsed_at
		    END
		WHERE id = ?
	`, finishedMillis, finishedMillis, workID)
	if err != nil {
		return false, err
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected != 1 {
		return false, fmt.Errorf("complete parse run %d: work %d does not exist", runID, workID)
	}
	dayStart := time.Date(
		finishedAt.Year(), finishedAt.Month(), finishedAt.Day(), 0, 0, 0, 0, time.UTC,
	)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO work_parse_daily(work_id, day_start, parse_count)
		VALUES (?, ?, 1)
		ON CONFLICT(work_id, day_start) DO UPDATE SET
			parse_count = work_parse_daily.parse_count + 1
	`, workID, unixMillis(dayStart)); err != nil {
		return false, err
	}
	return true, nil
}

func (s *appStore) failParseRun(ctx context.Context, runID int64, errorCode string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE parse_runs SET status = 'failed', finished_at = ?, error = ?
		WHERE id = ? AND status = 'running'
	`, unixMillis(time.Now()), sanitizeHistoryErrorCode(errorCode), runID); err != nil {
		return err
	}
	if err := pruneCompletedParseRuns(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *appStore) completeSkippedRun(ctx context.Context, runID int64, platformID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE parse_runs
		SET status = 'succeeded', source = 'skipped', finished_at = ?,
		    work_id = (SELECT id FROM works WHERE platform_id = ?),
		    version_id = (
			    SELECT v.id FROM work_versions v
			    JOIN works w ON w.id = v.work_id
			    WHERE w.platform_id = ? ORDER BY v.version_number DESC LIMIT 1
		    ), error = NULL
		WHERE id = ? AND status = 'running'
	`, unixMillis(time.Now()), platformID, platformID, runID); err != nil {
		return err
	}
	if err := pruneCompletedParseRuns(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *appStore) latestVersion(
	ctx context.Context,
	platformID string,
	cacheScope [sha256.Size]byte,
) (storedVersion, error) {
	var version storedVersion
	var capturedAt int64
	var dataJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT v.id, v.work_id, w.platform_id, v.version_number, v.captured_at, v.data_json
		FROM works w
		JOIN work_cache_scopes scope ON scope.work_id = w.id
		JOIN work_versions v ON v.id = scope.version_id
		WHERE w.platform_id = ? AND scope.cache_scope = ?
		LIMIT 1
	`, platformID, cacheScope[:]).Scan(
		&version.ID, &version.WorkID, &version.PlatformID, &version.VersionNumber,
		&capturedAt, &dataJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return storedVersion{}, errNoCachedVersion
	}
	if err != nil {
		return storedVersion{}, err
	}
	if err := json.Unmarshal([]byte(dataJSON), &version.Data); err != nil {
		return storedVersion{}, fmt.Errorf("decode cached work version: %w", err)
	}
	version.CapturedAt = time.UnixMilli(capturedAt).UTC()
	resources, err := s.loadVersionResources(ctx, version.ID)
	if err != nil {
		return storedVersion{}, err
	}
	version.Resources = resources
	return version, nil
}

func (s *appStore) persistFetchedVersion(
	ctx context.Context,
	runID int64,
	platformID string,
	cacheScope [sha256.Size]byte,
	cacheable bool,
	data map[string]any,
) (storedVersion, error) {
	content, err := json.Marshal(data)
	if err != nil {
		return storedVersion{}, fmt.Errorf("encode work version: %w", err)
	}
	hash := sha256.Sum256(content)
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storedVersion{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO works(platform_id, first_seen_at, last_seen_at)
		VALUES (?, ?, ?)
		ON CONFLICT(platform_id) DO UPDATE SET last_seen_at = excluded.last_seen_at
	`, platformID, unixMillis(now), unixMillis(now))
	if err != nil {
		return storedVersion{}, err
	}
	var workID int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM works WHERE platform_id = ?", platformID).Scan(&workID); err != nil {
		return storedVersion{}, err
	}
	var versionID, versionNumber, capturedAt int64
	err = tx.QueryRowContext(ctx, `
		SELECT id, version_number, captured_at
		FROM work_versions WHERE work_id = ? AND content_hash = ?
	`, workID, hash[:]).Scan(&versionID, &versionNumber, &capturedAt)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.QueryRowContext(ctx,
			"SELECT COALESCE(MAX(version_number), 0) + 1 FROM work_versions WHERE work_id = ?",
			workID,
		).Scan(&versionNumber); err != nil {
			return storedVersion{}, err
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO work_versions(
				work_id, version_number, content_hash, captured_at, data_json
			) VALUES (?, ?, ?, ?, ?)
		`, workID, versionNumber, hash[:], unixMillis(now), string(content))
		if err != nil {
			return storedVersion{}, err
		}
		versionID, err = result.LastInsertId()
		if err != nil {
			return storedVersion{}, err
		}
		capturedAt = unixMillis(now)
		for _, resource := range resourcesFromData(data) {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO version_resources(version_id, kind, ordinal, remote_url, save_status)
				VALUES (?, ?, ?, ?, 'pending')
			`, versionID, resource.Kind, resource.Ordinal, resource.RemoteURL); err != nil {
				return storedVersion{}, err
			}
		}
	} else if err != nil {
		return storedVersion{}, err
	}
	if cacheable {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO work_cache_scopes(work_id, cache_scope, version_id, observed_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(work_id, cache_scope) DO UPDATE SET
				version_id = excluded.version_id,
				observed_at = excluded.observed_at
		`, workID, cacheScope[:], versionID, unixMillis(now)); err != nil {
			return storedVersion{}, err
		}
	}
	if _, err := completeSuccessfulParseRun(
		ctx, tx, runID, "fetched", workID, versionID, now,
	); err != nil {
		return storedVersion{}, err
	}
	if err := pruneCompletedParseRuns(ctx, tx); err != nil {
		return storedVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return storedVersion{}, err
	}
	resources, err := s.loadVersionResources(ctx, versionID)
	if err != nil {
		return storedVersion{}, err
	}
	return storedVersion{
		ID: versionID, WorkID: workID, PlatformID: platformID,
		VersionNumber: versionNumber, CapturedAt: time.UnixMilli(capturedAt).UTC(),
		Data: data, Resources: resources,
	}, nil
}

func (s *appStore) completeCachedRun(ctx context.Context, runID int64, version storedVersion) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	if _, err := completeSuccessfulParseRun(
		ctx, tx, runID, "cache", version.WorkID, version.ID, now,
	); err != nil {
		return err
	}
	if err := pruneCompletedParseRuns(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func resourcesFromData(data map[string]any) []storedResource {
	urls, lives, err := mediaURLs(data)
	if err != nil {
		return nil
	}
	resources := make([]storedResource, 0, len(urls)+len(lives))
	if stringValue(data["作品类型"]) == "视频" {
		if coverURL := strings.TrimSpace(stringValue(data["封面地址"])); coverURL != "" {
			resources = append(resources, storedResource{
				Kind: "image", Ordinal: 0, RemoteURL: coverURL, SaveStatus: "pending",
			})
		}
		if len(urls) > 0 && strings.TrimSpace(urls[0]) != "" {
			resources = append(resources, storedResource{
				Kind: "video", Ordinal: 1, RemoteURL: urls[0], SaveStatus: "pending",
			})
		}
		return resources
	}
	for index, remoteURL := range urls {
		ordinal := index + 1
		if strings.TrimSpace(remoteURL) != "" {
			resources = append(resources, storedResource{
				Kind: "image", Ordinal: ordinal, RemoteURL: remoteURL, SaveStatus: "pending",
			})
		}
		if index < len(lives) {
			if liveURL := firstString(lives[index]); liveURL != "" {
				resources = append(resources, storedResource{
					Kind: "video", Ordinal: ordinal, RemoteURL: liveURL, SaveStatus: "pending",
				})
			}
		}
	}
	return resources
}

func (s *appStore) updateVersionResources(ctx context.Context, versionID int64, results []mediaPersistenceResult) ([]storedResource, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, result := range results {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO version_resources(
				version_id, kind, ordinal, remote_url, save_status,
				relative_path, mime_type, size_bytes, sha256, save_error
			) VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, NULLIF(?, ''), NULLIF(?, ''))
			ON CONFLICT(version_id, kind, ordinal) DO UPDATE SET
				remote_url = CASE WHEN excluded.remote_url = '' THEN version_resources.remote_url ELSE excluded.remote_url END,
				save_status = CASE
					WHEN version_resources.save_status = 'stored' AND excluded.save_status = 'disabled'
					THEN version_resources.save_status
					ELSE excluded.save_status
				END,
				relative_path = COALESCE(excluded.relative_path, version_resources.relative_path),
				mime_type = COALESCE(excluded.mime_type, version_resources.mime_type),
				size_bytes = COALESCE(excluded.size_bytes, version_resources.size_bytes),
				sha256 = COALESCE(excluded.sha256, version_resources.sha256),
				save_error = excluded.save_error
		`, versionID, result.Kind, result.Ordinal, strings.TrimSpace(result.RemoteURL), result.Status,
			result.RelativePath, result.MIMEType, nullablePositiveSize(result.SizeBytes), result.SHA256, stableSaveErrorCode(result.Status, result.Error))
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.loadVersionResources(ctx, versionID)
}

func stableSaveErrorCode(status, code string) string {
	if status != "failed" {
		return ""
	}
	switch code := strings.TrimSpace(code); code {
	case saveErrorStoragePathInvalid,
		saveErrorStoragePrepareFailed,
		saveErrorTextEncodeFailed,
		saveErrorTextWriteFailed,
		saveErrorMediaClientUnavailable,
		saveErrorMediaTooLarge,
		saveErrorMediaDownloadTimeout,
		saveErrorMediaDownloadCanceled,
		saveErrorMediaInvalid,
		saveErrorMediaDownloadFailed,
		saveErrorMediaMetadataFailed,
		saveErrorResourceFailed:
		return code
	default:
		return saveErrorResourceFailed
	}
}

func nullablePositiveSize(size int64) any {
	if size <= 0 {
		return nil
	}
	return size
}

func (s *appStore) loadVersionResources(ctx context.Context, versionID int64) ([]storedResource, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, ordinal, remote_url, save_status,
		       COALESCE(relative_path, ''), COALESCE(mime_type, ''),
		       COALESCE(size_bytes, 0), COALESCE(sha256, ''), COALESCE(save_error, '')
		FROM version_resources WHERE version_id = ?
		ORDER BY CASE kind WHEN 'text' THEN 0 WHEN 'image' THEN 1 ELSE 2 END, ordinal, id
	`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	resources := make([]storedResource, 0)
	for rows.Next() {
		var resource storedResource
		if err := rows.Scan(
			&resource.ID, &resource.Kind, &resource.Ordinal, &resource.RemoteURL,
			&resource.SaveStatus, &resource.RelativePath, &resource.MIMEType,
			&resource.SizeBytes, &resource.SHA256, &resource.SaveError,
		); err != nil {
			return nil, err
		}
		resources = append(resources, resource)
	}
	return resources, rows.Err()
}

func (s *appStore) storedImageResource(ctx context.Context, resourceID int64) (storedResource, error) {
	if resourceID < 1 {
		return storedResource{}, sql.ErrNoRows
	}
	var resource storedResource
	if err := s.db.QueryRowContext(ctx, `
		SELECT id, kind, ordinal, remote_url, save_status,
		       COALESCE(relative_path, ''), COALESCE(mime_type, ''),
		       COALESCE(size_bytes, 0), COALESCE(sha256, ''), COALESCE(save_error, '')
		FROM version_resources
		WHERE id = ? AND kind = 'image' AND save_status = 'stored'
		  AND relative_path IS NOT NULL AND relative_path <> ''
	`, resourceID).Scan(
		&resource.ID, &resource.Kind, &resource.Ordinal, &resource.RemoteURL,
		&resource.SaveStatus, &resource.RelativePath, &resource.MIMEType,
		&resource.SizeBytes, &resource.SHA256, &resource.SaveError,
	); err != nil {
		return storedResource{}, err
	}
	return resource, nil
}

func (s *appStore) hasLegacyDownload(ctx context.Context, platformID string) (bool, error) {
	var marker int
	err := s.db.QueryRowContext(ctx,
		"SELECT 1 FROM legacy_download_records WHERE platform_id = ?",
		platformID,
	).Scan(&marker)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *appStore) markLegacyDownload(ctx context.Context, platformID, source string, when time.Time) error {
	if strings.TrimSpace(platformID) == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO legacy_download_records(platform_id, downloaded_at, source)
		VALUES (?, ?, ?)
		ON CONFLICT(platform_id) DO UPDATE SET downloaded_at = excluded.downloaded_at, source = excluded.source
	`, platformID, unixMillis(when), source)
	return err
}

func (s *appStore) importDownloadedJSON(ctx context.Context, path string) error {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read legacy download records: %w", err)
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return nil
	}
	var records map[string]string
	if err := json.Unmarshal(content, &records); err != nil {
		return fmt.Errorf("decode legacy download records: %w", err)
	}
	fallback := time.Now().UTC()
	if info, err := os.Stat(path); err == nil {
		fallback = info.ModTime().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for platformID, rawTime := range records {
		platformID = strings.TrimSpace(platformID)
		if platformID == "" || len(platformID) > 256 {
			continue
		}
		downloadedAt := fallback
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(rawTime)); err == nil {
			downloadedAt = parsed.UTC()
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO legacy_download_records(platform_id, downloaded_at, source)
			VALUES (?, ?, 'downloaded.json')
		`, platformID, unixMillis(downloadedAt)); err != nil {
			return fmt.Errorf("import legacy download record %q: %w", platformID, err)
		}
	}
	return tx.Commit()
}

func unixMillis(value time.Time) int64 {
	return value.UTC().UnixMilli()
}

func parseCursor(value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseInt(value, 10, 64)
	if err != nil || cursor < 1 {
		return 0, errors.New("cursor must be a positive run ID")
	}
	return cursor, nil
}
func validateDatabasePath(volumeDir, databasePath string) error {
	volumePath, err := filepath.Abs(volumeDir)
	if err != nil {
		return fmt.Errorf("resolve volume path: %w", err)
	}
	databaseFile, err := filepath.Abs(databasePath)
	if err != nil {
		return fmt.Errorf("resolve database path: %w", err)
	}
	if !pathContainedBy(volumePath, databaseFile) || databaseFile == volumePath {
		return errors.New("XHS_DATABASE_PATH must be located inside XHS_VOLUME_DIR")
	}
	if err := os.MkdirAll(volumePath, 0o755); err != nil {
		return fmt.Errorf("create volume directory: %w", err)
	}
	realVolume, err := filepath.EvalSymlinks(volumePath)
	if err != nil {
		return fmt.Errorf("resolve volume symlinks: %w", err)
	}
	databaseDir := filepath.Dir(databaseFile)
	ancestor, err := nearestExistingAncestor(databaseDir)
	if err != nil {
		return fmt.Errorf("inspect database directory ancestors: %w", err)
	}
	realAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return fmt.Errorf("resolve database directory ancestor symlinks: %w", err)
	}
	if !pathContainedBy(realVolume, realAncestor) {
		return errors.New("XHS_DATABASE_PATH escapes XHS_VOLUME_DIR through a symbolic link")
	}
	if _, err := os.Lstat(databaseFile); err == nil {
		realDatabaseFile, resolveErr := filepath.EvalSymlinks(databaseFile)
		if resolveErr != nil {
			return fmt.Errorf("resolve database file symlinks: %w", resolveErr)
		}
		if !pathContainedBy(realVolume, realDatabaseFile) {
			return errors.New("XHS_DATABASE_PATH escapes XHS_VOLUME_DIR through a symbolic link")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect database path: %w", err)
	}
	if err := os.MkdirAll(databaseDir, 0o700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	realDatabaseDir, err := filepath.EvalSymlinks(databaseDir)
	if err != nil {
		return fmt.Errorf("resolve database directory symlinks: %w", err)
	}
	if !pathContainedBy(realVolume, realDatabaseDir) {
		return errors.New("XHS_DATABASE_PATH escapes XHS_VOLUME_DIR through a symbolic link")
	}
	return nil
}

func nearestExistingAncestor(path string) (string, error) {
	current := path
	for {
		if _, err := os.Lstat(current); err == nil {
			return current, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("no existing database path ancestor")
		}
		current = parent
	}
}

func pathContainedBy(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func (c *secretCipher) adminCredentialFingerprint(username, password string) ([sha256.Size]byte, bool) {
	if strings.TrimSpace(username) == "" || password == "" {
		return [sha256.Size]byte{}, false
	}
	mac := hmac.New(sha256.New, c.scopeKey[:])
	_, _ = mac.Write([]byte("xhs-admin-credential-v1"))
	writeValue := func(value string) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = mac.Write(length[:])
		_, _ = mac.Write([]byte(value))
	}
	writeValue(username)
	writeValue(password)
	var fingerprint [sha256.Size]byte
	copy(fingerprint[:], mac.Sum(nil))
	return fingerprint, true
}
