package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestMigrationV2BackfillsRetainedSuccessfulRuns(t *testing.T) {
	config := (Config{VolumeDir: filepath.Join(t.TempDir(), "Volume")}).withDefaults()
	if err := os.MkdirAll(filepath.Dir(config.DatabasePath), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(config.DatabasePath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			checksum TEXT NOT NULL,
			applied_at INTEGER NOT NULL
		) STRICT;
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(databaseSchemaV1); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(databaseSchemaV1))
	if _, err := db.Exec(`
		INSERT INTO schema_migrations(version, name, checksum, applied_at)
		VALUES (1, 'initial application database', ?, 0)
	`, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, time.July, 10, 23, 45, 0, 0, time.UTC)
	last := time.Date(2026, time.July, 11, 1, 15, 0, 0, time.UTC)
	result, err := db.Exec(`
		INSERT INTO works(platform_id, first_seen_at, last_seen_at) VALUES ('legacy-work', ?, ?)
	`, unixMillis(first), unixMillis(last))
	if err != nil {
		t.Fatal(err)
	}
	workID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	versionHash := sha256.Sum256([]byte("legacy-version"))
	result, err = db.Exec(`
		INSERT INTO work_versions(work_id, version_number, content_hash, captured_at, data_json)
		VALUES (?, 1, ?, ?, '{"作品标题":"旧作品"}')
	`, workID, versionHash[:], unixMillis(first))
	if err != nil {
		t.Fatal(err)
	}
	versionID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	insertRun := func(status, source string, finished time.Time) {
		t.Helper()
		if _, err := db.Exec(`
			INSERT INTO parse_runs(
				requested_url, status, source, cookie_source, proxy_source,
				started_at, finished_at, work_id, version_id
			) VALUES ('https://example.invalid/legacy', ?, ?, 'none', 'none', ?, ?, ?, ?)
		`, status, source, unixMillis(finished.Add(-time.Second)), unixMillis(finished), workID, versionID); err != nil {
			t.Fatal(err)
		}
	}
	insertRun("succeeded", "fetched", first)
	insertRun("succeeded", "cache", last)
	insertRun("succeeded", "skipped", last.Add(time.Minute))
	insertRun("failed", "fetched", last.Add(2*time.Minute))
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := openAppStore(config)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var migrationCount int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if migrationCount != 2 {
		t.Fatalf("migration count = %d", migrationCount)
	}
	settings, err := store.loadSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if settings.ShowPopular {
		t.Fatal("show_popular should default to false")
	}
	if !settings.Public || !settings.Refetch {
		t.Fatalf("existing settings were overwritten: %#v", settings)
	}
	var parseCount, lastParsedAt int64
	if err := store.db.QueryRow(`
		SELECT parse_count, last_parsed_at FROM works WHERE id = ?
	`, workID).Scan(&parseCount, &lastParsedAt); err != nil {
		t.Fatal(err)
	}
	if parseCount != 2 || lastParsedAt != unixMillis(last) {
		t.Fatalf("backfilled work = count:%d last:%d", parseCount, lastParsedAt)
	}
	rows, err := store.db.Query(`
		SELECT day_start, parse_count FROM work_parse_daily WHERE work_id = ? ORDER BY day_start
	`, workID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	gotDaily := make(map[int64]int64)
	for rows.Next() {
		var day, count int64
		if err := rows.Scan(&day, &count); err != nil {
			t.Fatal(err)
		}
		gotDaily[day] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	firstDay := unixMillis(time.Date(2026, time.July, 10, 0, 0, 0, 0, time.UTC))
	lastDay := unixMillis(time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC))
	if len(gotDaily) != 2 || gotDaily[firstDay] != 1 || gotDaily[lastDay] != 1 {
		t.Fatalf("backfilled daily counts = %#v", gotDaily)
	}
}

func TestSuccessfulParseCountingIsAtomicAndIdempotent(t *testing.T) {
	app := newTestApp(t)
	ctx := t.Context()
	data := map[string]any{
		"作品ID": "counted-work", "作品标题": "计数作品", "作品类型": "图文",
		"下载地址": []string{}, "动图地址": []any{},
	}
	runID, err := app.store.beginParseRun(ctx, "https://example.invalid/fetched", "none", "none")
	if err != nil {
		t.Fatal(err)
	}
	version, err := app.store.persistFetchedVersion(ctx, runID, "counted-work", [sha256.Size]byte{}, true, data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.persistFetchedVersion(ctx, runID, "counted-work", [sha256.Size]byte{}, true, data); err != nil {
		t.Fatal(err)
	}

	const cachedRuns = 8
	runIDs := make([]int64, cachedRuns)
	for index := range runIDs {
		runIDs[index], err = app.store.beginParseRun(
			ctx, fmt.Sprintf("https://example.invalid/cache/%d", index), "none", "none",
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	var wait sync.WaitGroup
	errorsFound := make(chan error, cachedRuns)
	for _, cachedRunID := range runIDs {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := app.store.completeCachedRun(ctx, cachedRunID, version); err != nil {
				errorsFound <- err
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	if err := app.store.completeCachedRun(ctx, runIDs[0], version); err != nil {
		t.Fatal(err)
	}
	failedRun, err := app.store.beginParseRun(ctx, "https://example.invalid/failed", "none", "none")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.failParseRun(ctx, failedRun, "TEST_FAILURE"); err != nil {
		t.Fatal(err)
	}
	skippedRun, err := app.store.beginParseRun(ctx, "https://example.invalid/skipped", "none", "none")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.completeSkippedRun(ctx, skippedRun, "counted-work"); err != nil {
		t.Fatal(err)
	}

	var parseCount, dailyCount int64
	if err := app.store.db.QueryRow(`SELECT parse_count FROM works WHERE id = ?`, version.WorkID).Scan(&parseCount); err != nil {
		t.Fatal(err)
	}
	if err := app.store.db.QueryRow(`
		SELECT parse_count FROM work_parse_daily WHERE work_id = ? AND day_start = ?
	`, version.WorkID, unixMillis(time.Now().UTC().Truncate(24*time.Hour))).Scan(&dailyCount); err != nil {
		t.Fatal(err)
	}
	if parseCount != cachedRuns+1 || dailyCount != cachedRuns+1 {
		t.Fatalf("counts = total:%d daily:%d", parseCount, dailyCount)
	}
	if _, err := app.store.db.Exec(`DELETE FROM parse_runs`); err != nil {
		t.Fatal(err)
	}
	if err := app.store.db.QueryRow(`SELECT parse_count FROM works WHERE id = ?`, version.WorkID).Scan(&parseCount); err != nil {
		t.Fatal(err)
	}
	if parseCount != cachedRuns+1 {
		t.Fatalf("count after history deletion = %d", parseCount)
	}
}

func TestWorksListPaginatesZeroLastParsedAt(t *testing.T) {
	app := newTestApp(t)
	for index := 1; index <= 3; index++ {
		if _, err := app.store.db.Exec(`
			INSERT INTO works(platform_id, first_seen_at, last_seen_at)
			VALUES (?, ?, ?)
		`, fmt.Sprintf("legacy-%d", index), int64(index), int64(index)); err != nil {
			t.Fatal(err)
		}
	}
	first, err := app.store.listWorks(t.Context(), 2, workListCursor{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == nil || first.NextCursor.LastParsedAt != 0 {
		t.Fatalf("first zero-time page = %#v", first)
	}
	second, err := app.store.listWorks(t.Context(), 2, *first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != nil || second.Items[0].LastParsedAt != nil {
		t.Fatalf("second zero-time page = %#v", second)
	}
}

func TestWorksListPopularWindowsAndStoredThumbnail(t *testing.T) {
	app := newTestApp(t)
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	today := time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC)
	type seededWork struct {
		id      int64
		imageID int64
	}
	seed := func(platformID string, count int64, last time.Time, versions int, storedTitle string) seededWork {
		t.Helper()
		result, err := app.store.db.Exec(`
			INSERT INTO works(
				platform_id, first_seen_at, last_seen_at, parse_count, last_parsed_at
			) VALUES (?, ?, ?, ?, ?)
		`, platformID, unixMillis(last), unixMillis(last), count, unixMillis(last))
		if err != nil {
			t.Fatal(err)
		}
		workID, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		var imageID int64
		for number := 1; number <= versions; number++ {
			title := fmt.Sprintf("%s-v%d", platformID, number)
			hash := sha256.Sum256([]byte(title))
			result, err := app.store.db.Exec(`
				INSERT INTO work_versions(
					work_id, version_number, content_hash, captured_at, data_json
				) VALUES (?, ?, ?, ?, json_object('作品标题', ?))
			`, workID, number, hash[:], unixMillis(last.Add(time.Duration(number)*time.Minute)), title)
			if err != nil {
				t.Fatal(err)
			}
			versionID, err := result.LastInsertId()
			if err != nil {
				t.Fatal(err)
			}
			textStatus := "disabled"
			if title == storedTitle {
				textStatus = "stored"
			}
			if _, err := app.store.db.Exec(`
				INSERT INTO version_resources(version_id, kind, ordinal, save_status)
				VALUES (?, 'text', 0, ?)
			`, versionID, textStatus); err != nil {
				t.Fatal(err)
			}
			if number == 1 && platformID == "work-a" {
				result, err = app.store.db.Exec(`
					INSERT INTO version_resources(
						version_id, kind, ordinal, remote_url, save_status,
						relative_path, mime_type, size_bytes, sha256
					) VALUES (?, 'image', 1, 'https://example.invalid/a.jpg', 'stored',
					          'Download/work-a/v1/image_001.jpeg', 'image/jpeg', 12, 'abc')
				`, versionID)
				if err != nil {
					t.Fatal(err)
				}
				imageID, err = result.LastInsertId()
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		return seededWork{id: workID, imageID: imageID}
	}
	a := seed("work-a", 5, now.Add(-2*time.Hour), 2, "work-a-v1")
	b := seed("work-b", 5, now.Add(-time.Hour), 1, "")
	c := seed("work-c", 2, now.Add(-3*time.Hour), 1, "work-c-v1")
	insertDaily := func(workID int64, day time.Time, count int64) {
		t.Helper()
		if _, err := app.store.db.Exec(`
			INSERT INTO work_parse_daily(work_id, day_start, parse_count) VALUES (?, ?, ?)
		`, workID, unixMillis(day), count); err != nil {
			t.Fatal(err)
		}
	}
	insertDaily(a.id, today, 2)
	insertDaily(a.id, today.AddDate(0, 0, -7), 9)
	insertDaily(b.id, today.AddDate(0, 0, -6), 2)
	insertDaily(c.id, today, 1)

	firstPage, err := app.store.listWorks(t.Context(), 2, workListCursor{})
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Items) != 2 || firstPage.Items[0].PlatformID != "work-b" ||
		firstPage.Items[1].PlatformID != "work-a" || firstPage.NextCursor == nil {
		t.Fatalf("first works page = %#v", firstPage)
	}
	if firstPage.Items[0].Title != "" || firstPage.Items[0].ThumbnailResourceID != nil {
		t.Fatalf("unsaved work metadata leaked = %#v", firstPage.Items[0])
	}
	if firstPage.Items[1].Title != "work-a-v1" || firstPage.Items[1].VersionCount != 2 ||
		firstPage.Items[1].ThumbnailResourceID == nil || *firstPage.Items[1].ThumbnailResourceID != a.imageID {
		t.Fatalf("saved work metadata = %#v", firstPage.Items[1])
	}
	secondPage, err := app.store.listWorks(t.Context(), 2, *firstPage.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Items) != 1 || secondPage.Items[0].PlatformID != "work-c" || secondPage.NextCursor != nil {
		t.Fatalf("second works page = %#v", secondPage)
	}

	allTime, err := app.store.popularWorksAt(t.Context(), 0, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(allTime) != 3 || allTime[0].PlatformID != "work-b" || allTime[1].PlatformID != "work-a" {
		t.Fatalf("all-time popular = %#v", allTime)
	}
	if allTime[0].Title != "" || allTime[1].Title != "work-a-v1" {
		t.Fatalf("popular titles = %#v", allTime)
	}
	recent7, err := app.store.popularWorksAt(t.Context(), 7, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent7) != 3 || recent7[0].PlatformID != "work-b" ||
		recent7[1].PlatformID != "work-a" || recent7[1].ParseCount != 2 {
		t.Fatalf("recent 7 days = %#v", recent7)
	}
	recent30, err := app.store.popularWorksAt(t.Context(), 30, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent30) != 3 || recent30[0].PlatformID != "work-a" || recent30[0].ParseCount != 11 {
		t.Fatalf("recent 30 days = %#v", recent30)
	}
	resource, err := app.store.storedImageResource(t.Context(), a.imageID)
	if err != nil {
		t.Fatal(err)
	}
	if resource.RelativePath == "" || resource.MIMEType != "image/jpeg" {
		t.Fatalf("stored image resource = %#v", resource)
	}
	if _, err := app.store.storedImageResource(t.Context(), b.imageID); err != sql.ErrNoRows {
		t.Fatalf("missing stored image error = %v", err)
	}
}

func TestResourcesFromDataIncludesVideoCover(t *testing.T) {
	resources := resourcesFromData(map[string]any{
		"作品类型": "视频",
		"封面地址": "https://example.invalid/cover.jpg",
		"下载地址": []string{"https://example.invalid/video.mp4"},
		"动图地址": []any{nil},
	})
	if len(resources) != 2 || resources[0].Kind != "image" || resources[0].Ordinal != 0 ||
		resources[1].Kind != "video" || resources[1].Ordinal != 1 {
		t.Fatalf("video resources = %#v", resources)
	}
}
