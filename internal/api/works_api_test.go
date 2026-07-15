package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func seedWorksAPIWork(t *testing.T, app *App, platformID, title string) storedVersion {
	t.Helper()
	runID, err := app.store.beginParseRun(
		t.Context(), "https://example.invalid/fetched/"+platformID, "none", "none",
	)
	if err != nil {
		t.Fatal(err)
	}
	cacheScope := sha256.Sum256([]byte("works-api:" + platformID))
	version, err := app.store.persistFetchedVersion(
		t.Context(), runID, platformID, cacheScope, true,
		map[string]any{
			"作品ID": platformID, "作品标题": title, "作品类型": "图文",
			"下载地址": []string{}, "动图地址": []any{},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.db.Exec(`
		INSERT INTO version_resources(version_id, kind, ordinal, save_status)
		VALUES (?, 'text', 0, 'stored')
	`, version.ID); err != nil {
		t.Fatal(err)
	}
	return version
}

func completeWorksAPICacheHit(t *testing.T, app *App, version storedVersion) {
	t.Helper()
	runID, err := app.store.beginParseRun(
		t.Context(), "https://example.invalid/cache/"+version.PlatformID, "none", "none",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.completeCachedRun(t.Context(), runID, version); err != nil {
		t.Fatal(err)
	}
}

func TestPopularWorksHandlerSettingsCountsAndAccess(t *testing.T) {
	app := newAdminTestApp(t)
	version := seedWorksAPIWork(t, app, "popular-handler-work", "热门作品")
	completeWorksAPICacheHit(t, app, version)

	disabledRecorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		disabledRecorder,
		httptest.NewRequest(http.MethodGet, "/api/v1/popular-works", nil),
	)
	if disabledRecorder.Code != http.StatusOK {
		t.Fatalf("disabled popular status = %d, body = %s", disabledRecorder.Code, disabledRecorder.Body.String())
	}
	var disabled popularWorksResponse
	if err := json.Unmarshal(disabledRecorder.Body.Bytes(), &disabled); err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled || disabled.AllTime == nil || disabled.Recent30D == nil || disabled.Recent7D == nil ||
		len(disabled.AllTime) != 0 || len(disabled.Recent30D) != 0 || len(disabled.Recent7D) != 0 {
		t.Fatalf("disabled popular response = %#v", disabled)
	}

	if _, err := app.store.db.Exec(`UPDATE app_settings SET show_popular = 1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	assertPopular := func(recorder *httptest.ResponseRecorder) {
		t.Helper()
		if recorder.Code != http.StatusOK {
			t.Fatalf("popular status = %d, body = %s", recorder.Code, recorder.Body.String())
		}
		var response popularWorksResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if !response.Enabled {
			t.Fatalf("popular response disabled = %#v", response)
		}
		windows := []struct {
			name  string
			items []popularWorkView
		}{
			{name: "all_time", items: response.AllTime},
			{name: "recent_30d", items: response.Recent30D},
			{name: "recent_7d", items: response.Recent7D},
		}
		for _, window := range windows {
			if len(window.items) != 1 || window.items[0].PlatformID != version.PlatformID ||
				window.items[0].Title != "热门作品" ||
				window.items[0].WorkURL != canonicalWorkURL(version.PlatformID) ||
				window.items[0].ParseCount != 2 {
				t.Fatalf("%s popular items = %#v", window.name, window.items)
			}
		}
	}

	publicRecorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		publicRecorder,
		httptest.NewRequest(http.MethodGet, "/api/v1/popular-works", nil),
	)
	assertPopular(publicRecorder)

	cookie := loginAdmin(t, app)
	if _, err := app.store.db.Exec(`UPDATE app_settings SET public_enabled = 0 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	privateAnonymous := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		privateAnonymous,
		httptest.NewRequest(http.MethodGet, "/api/v1/popular-works", nil),
	)
	if privateAnonymous.Code != http.StatusUnauthorized ||
		!strings.Contains(privateAnonymous.Body.String(), `"code":"PUBLIC_ACCESS_DISABLED"`) {
		t.Fatalf("private anonymous popular = %d %s", privateAnonymous.Code, privateAnonymous.Body.String())
	}
	privateAdmin := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		privateAdmin,
		authenticatedRequest(http.MethodGet, "http://example.test/api/v1/popular-works", nil, cookie),
	)
	assertPopular(privateAdmin)
}

func TestAdminWorksHandlerRequiresLoginAndReturnsSavedTitle(t *testing.T) {
	app := newAdminTestApp(t)
	version := seedWorksAPIWork(t, app, "listed-handler-work", "列表作品")

	unauthorized := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		unauthorized,
		httptest.NewRequest(http.MethodGet, "/api/admin/v1/works", nil),
	)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized works = %d %s", unauthorized.Code, unauthorized.Body.String())
	}

	cookie := loginAdmin(t, app)
	recorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		recorder,
		authenticatedRequest(http.MethodGet, "http://example.test/api/admin/v1/works", nil, cookie),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("works status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var page adminWorkPage
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("works page = %#v", page)
	}
	item := page.Items[0]
	if item.ID != version.WorkID || item.PlatformID != version.PlatformID || item.ParseCount != 1 ||
		item.VersionCount != 1 || item.Title != "列表作品" {
		t.Fatalf("work item = %#v", item)
	}
}

func TestAdminResourceContentAuthenticationStreamingAndPathSafety(t *testing.T) {
	app := newAdminTestApp(t)
	version := seedWorksAPIWork(t, app, "thumbnail-handler-work", "缩略图作品")
	payload := []byte("stored-thumbnail-image")
	digest := sha256.Sum256(payload)
	digestHex := hex.EncodeToString(digest[:])
	relativePath := filepath.ToSlash(filepath.Join("Download", version.PlatformID, "v1", "image_001.jpeg"))
	absolutePath := filepath.Join(app.config.VolumeDir, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolutePath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	insertImage := func(ordinal int, storedPath string) int64 {
		t.Helper()
		result, err := app.store.db.Exec(`
			INSERT INTO version_resources(
				version_id, kind, ordinal, save_status, relative_path,
				mime_type, size_bytes, sha256
			) VALUES (?, 'image', ?, 'stored', ?, 'image/jpeg', ?, ?)
		`, version.ID, ordinal, storedPath, len(payload), digestHex)
		if err != nil {
			t.Fatal(err)
		}
		resourceID, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return resourceID
	}
	resourceID := insertImage(1, relativePath)
	target := "/api/admin/v1/resources/" + strconv.FormatInt(resourceID, 10) + "/content"

	unauthorized := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		unauthorized,
		httptest.NewRequest(http.MethodGet, target, nil),
	)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized thumbnail = %d %s", unauthorized.Code, unauthorized.Body.String())
	}

	cookie := loginAdmin(t, app)
	stored := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		stored,
		authenticatedRequest(http.MethodGet, "http://example.test"+target, nil, cookie),
	)
	wantETag := `"` + digestHex + `"`
	if stored.Code != http.StatusOK || !bytes.Equal(stored.Body.Bytes(), payload) ||
		stored.Header().Get("Content-Type") != "image/jpeg" || stored.Header().Get("ETag") != wantETag {
		t.Fatalf("stored thumbnail = %d headers:%v body:%q", stored.Code, stored.Header(), stored.Body.Bytes())
	}

	notModifiedRequest := authenticatedRequest(http.MethodGet, "http://example.test"+target, nil, cookie)
	notModifiedRequest.Header.Set("If-None-Match", wantETag)
	notModified := httptest.NewRecorder()
	app.Handler().ServeHTTP(notModified, notModifiedRequest)
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 {
		t.Fatalf("not modified thumbnail = %d %q", notModified.Code, notModified.Body.Bytes())
	}

	outsidePath := filepath.Join(filepath.Dir(app.config.VolumeDir), "outside-thumbnail.jpeg")
	if err := os.WriteFile(outsidePath, []byte("outside-volume"), 0o600); err != nil {
		t.Fatal(err)
	}
	unsafeResources := []struct {
		name string
		id   int64
	}{
		{name: "absolute", id: insertImage(2, outsidePath)},
		{name: "traversal", id: insertImage(3, "../outside-thumbnail.jpeg")},
	}
	for _, unsafeResource := range unsafeResources {
		t.Run(unsafeResource.name, func(t *testing.T) {
			unsafeTarget := "/api/admin/v1/resources/" + strconv.FormatInt(unsafeResource.id, 10) + "/content"
			recorder := httptest.NewRecorder()
			app.Handler().ServeHTTP(
				recorder,
				authenticatedRequest(http.MethodGet, "http://example.test"+unsafeTarget, nil, cookie),
			)
			if recorder.Code != http.StatusNotFound ||
				!strings.Contains(recorder.Body.String(), `"code":"RESOURCE_NOT_FOUND"`) {
				t.Fatalf("unsafe thumbnail = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
