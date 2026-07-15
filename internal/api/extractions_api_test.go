package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func fixtureClientFactory(
	t *testing.T,
	fixture []byte,
	pageRequests *int,
	observedCookies *[]string,
	mu *sync.Mutex,
) clientFactory {
	t.Helper()
	return func(_ *string) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Host == "www.xiaohongshu.com" {
				mu.Lock()
				*pageRequests++
				*observedCookies = append(*observedCookies, request.Header.Get("Cookie"))
				mu.Unlock()
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(fixture)),
				Request:    request,
			}, nil
		})}, nil
	}
}

func postExtraction(t *testing.T, app *App, body string) *httptest.ResponseRecorder {
	return postExtractionWithCookie(t, app, body, nil)
}

func postExtractionWithCookie(
	t *testing.T,
	app *App,
	body string,
	cookie *http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/extractions", strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	app.Handler().ServeHTTP(recorder, request)
	return recorder
}

func TestExtractionVersionsCanonicalizeLinksAndUseCache(t *testing.T) {
	app := newAdminTestApp(t)
	cookie := loginAdmin(t, app)
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		return postExtractionWithCookie(t, app, body, cookie)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	var (
		pageRequests    int
		observedCookies []string
		mu              sync.Mutex
	)
	app.clientFactory = fixtureClientFactory(
		t, fixture, &pageRequests, &observedCookies, &mu,
	)

	secretCookie := "web_session=request-secret"
	first := post(`{
		"url":"https://www.xiaohongshu.com/explore/fixture123?xsec_token=one",
		"connection":{"cookie":"web_session=request-secret"}
	}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first extraction = %d %s", first.Code, first.Body.String())
	}
	if strings.Contains(first.Body.String(), secretCookie) {
		t.Fatalf("extraction response leaked request Cookie: %s", first.Body.String())
	}
	var firstResponse extractionV1Response
	if err := json.Unmarshal(first.Body.Bytes(), &firstResponse); err != nil {
		t.Fatal(err)
	}
	if firstResponse.Source != "fetched" || firstResponse.Version.Number != 1 ||
		firstResponse.Connection.CookieSource != "override" {
		t.Fatalf("first response = %#v", firstResponse)
	}
	if got := stringValue(firstResponse.Data["作品链接"]); got != canonicalWorkURL("fixture123") {
		t.Fatalf("canonical 作品链接 = %q", got)
	}
	for _, resource := range firstResponse.Version.Resources {
		if resource.SaveStatus != "disabled" {
			t.Fatalf("default resource status = %#v", resource)
		}
	}
	disabled := false
	if _, err := app.store.updateSettings(t.Context(), settingsUpdate{
		Revision: 1, Refetch: &disabled,
	}); err != nil {
		t.Fatal(err)
	}

	second := post(`{
		"url":"https://www.xiaohongshu.com/discovery/item/fixture123?xsec_token=two",
		"connection":{"cookie":"web_session=request-secret"}
	}`)
	if second.Code != http.StatusOK {
		t.Fatalf("second extraction = %d %s", second.Code, second.Body.String())
	}
	var secondResponse extractionV1Response
	if err := json.Unmarshal(second.Body.Bytes(), &secondResponse); err != nil {
		t.Fatal(err)
	}
	if secondResponse.Source != "fetched" || secondResponse.Version.ID != firstResponse.Version.ID {
		t.Fatalf("override request was cached or duplicated the version: %#v", secondResponse)
	}
	var versionCount int
	if err := app.store.db.QueryRow("SELECT COUNT(*) FROM work_versions").Scan(&versionCount); err != nil {
		t.Fatal(err)
	}
	if versionCount != 1 {
		t.Fatalf("work version count = %d, want canonical deduplication", versionCount)
	}
	var scopeCount int
	if err := app.store.db.QueryRow("SELECT COUNT(*) FROM work_cache_scopes").Scan(&scopeCount); err != nil {
		t.Fatal(err)
	}
	if scopeCount != 0 {
		t.Fatalf("override requests created %d persistent cache scopes", scopeCount)
	}
	third := post(`{
		"url":"https://www.xiaohongshu.com/explore/fixture123?xsec_token=three",
		"connection":{"cookie":null}
	}`)
	if third.Code != http.StatusOK {
		t.Fatalf("cached extraction = %d %s", third.Code, third.Body.String())
	}
	var thirdResponse extractionV1Response
	if err := json.Unmarshal(third.Body.Bytes(), &thirdResponse); err != nil {
		t.Fatal(err)
	}
	if thirdResponse.Source != "fetched" ||
		thirdResponse.Connection.CookieSource != "disabled" ||
		thirdResponse.Version.ID != firstResponse.Version.ID {
		t.Fatalf("cross-scope response = %#v", thirdResponse)
	}
	fourth := post(`{
		"url":"https://www.xiaohongshu.com/explore/fixture123?xsec_token=four",
		"connection":{"cookie":null}
	}`)
	if fourth.Code != http.StatusOK {
		t.Fatalf("second disabled override extraction = %d %s", fourth.Code, fourth.Body.String())
	}
	var fourthResponse extractionV1Response
	if err := json.Unmarshal(fourth.Body.Bytes(), &fourthResponse); err != nil {
		t.Fatal(err)
	}
	if fourthResponse.Source != "fetched" || fourthResponse.Version.ID != thirdResponse.Version.ID {
		t.Fatalf("disabled override was cached or duplicated the version: %#v", fourthResponse)
	}
	if err := app.store.db.QueryRow("SELECT COUNT(*) FROM work_cache_scopes").Scan(&scopeCount); err != nil {
		t.Fatal(err)
	}
	if scopeCount != 0 {
		t.Fatalf("request overrides created %d persistent cache scopes", scopeCount)
	}
	fifth := post(`{"url":"https://www.xiaohongshu.com/explore/fixture123"}`)
	if fifth.Code != http.StatusOK {
		t.Fatalf("first no-connection extraction = %d %s", fifth.Code, fifth.Body.String())
	}
	var fifthResponse extractionV1Response
	if err := json.Unmarshal(fifth.Body.Bytes(), &fifthResponse); err != nil {
		t.Fatal(err)
	}
	if fifthResponse.Source != "fetched" || fifthResponse.Version.ID != firstResponse.Version.ID {
		t.Fatalf("first no-connection response = %#v", fifthResponse)
	}
	sixth := post(`{"url":"https://www.xiaohongshu.com/explore/fixture123"}`)
	if sixth.Code != http.StatusOK {
		t.Fatalf("cached no-connection extraction = %d %s", sixth.Code, sixth.Body.String())
	}
	var sixthResponse extractionV1Response
	if err := json.Unmarshal(sixth.Body.Bytes(), &sixthResponse); err != nil {
		t.Fatal(err)
	}
	if sixthResponse.Source != "cache" || sixthResponse.Version.ID != fifthResponse.Version.ID {
		t.Fatalf("no-connection cache response = %#v", sixthResponse)
	}
	if err := app.store.db.QueryRow("SELECT COUNT(*) FROM work_cache_scopes").Scan(&scopeCount); err != nil {
		t.Fatal(err)
	}
	if scopeCount != 1 {
		t.Fatalf("no-connection requests created %d cache scopes, want 1", scopeCount)
	}

	mu.Lock()
	requests := pageRequests
	cookies := append([]string(nil), observedCookies...)
	mu.Unlock()
	if requests != 5 {
		t.Fatalf("page requests = %d, cache request reached upstream", requests)
	}
	if len(cookies) != 5 || cookies[0] != secretCookie || cookies[1] != secretCookie || cookies[2] != "" || cookies[3] != "" || cookies[4] != "" {
		t.Fatalf("observed upstream Cookies = %#v", cookies)
	}
	var runCount int
	if err := app.store.db.QueryRow("SELECT COUNT(*) FROM parse_runs").Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 6 {
		t.Fatalf("parse run count = %d, want one run per request", runCount)
	}
}

func TestAnonymousConnectionOverridesAreForbidden(t *testing.T) {
	app := newTestApp(t)
	app.clientFactory = func(*string) (*http.Client, error) {
		t.Fatal("forbidden connection override reached the client factory")
		return nil, nil
	}
	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "v1 cookie", path: "/api/v1/extractions", body: `{"url":"https://www.xiaohongshu.com/explore/fixture123","connection":{"cookie":"session=attacker"}}`},
		{name: "v1 cookie disabled", path: "/api/v1/extractions", body: `{"url":"https://www.xiaohongshu.com/explore/fixture123","connection":{"cookie":null}}`},
		{name: "v1 proxy", path: "/api/v1/extractions", body: `{"url":"https://www.xiaohongshu.com/explore/fixture123","connection":{"proxy":"http://203.0.113.10:8080"}}`},
		{name: "v1 proxy disabled", path: "/api/v1/extractions", body: `{"url":"https://www.xiaohongshu.com/explore/fixture123","connection":{"proxy":null}}`},
		{name: "legacy cookie", path: "/xhs/detail", body: `{"url":"https://www.xiaohongshu.com/explore/fixture123","cookie":"session=attacker"}`},
		{name: "legacy proxy", path: "/xhs/detail", body: `{"url":"https://www.xiaohongshu.com/explore/fixture123","proxy":"http://203.0.113.10:8080"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			app.Handler().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden ||
				!strings.Contains(recorder.Body.String(), `"code":"CONNECTION_OVERRIDE_FORBIDDEN"`) {
				t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
	var runCount int
	if err := app.store.db.QueryRow("SELECT COUNT(*) FROM parse_runs").Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 0 {
		t.Fatalf("forbidden overrides created %d parse runs", runCount)
	}
}

func TestAuthenticatedExtractionRequiresSameOriginInPublicMode(t *testing.T) {
	app := newAdminTestApp(t)
	enablePublicTestAccess(t, app)
	cookie := loginAdmin(t, app)
	app.clientFactory = func(*string) (*http.Client, error) {
		t.Fatal("cross-origin authenticated extraction reached the client factory")
		return nil, nil
	}
	recorder := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"http://example.test/api/v1/extractions",
		strings.NewReader(`{"url":"https://www.xiaohongshu.com/explore/fixture123"}`),
		cookie,
	)
	request.Header.Set("Origin", "https://evil.example")
	app.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden ||
		!strings.Contains(recorder.Body.String(), `"code":"ORIGIN_NOT_ALLOWED"`) {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	var runCount int
	if err := app.store.db.QueryRow("SELECT COUNT(*) FROM parse_runs").Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 0 {
		t.Fatalf("cross-origin authenticated request created %d parse runs", runCount)
	}
}

func TestExtractionHistoryAndWorkDetail(t *testing.T) {
	app := newAdminTestApp(t)
	enablePublicTestAccess(t, app)
	fixture, err := os.ReadFile(filepath.Join("testdata", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	var pageRequests int
	var cookies []string
	var mu sync.Mutex
	app.clientFactory = fixtureClientFactory(t, fixture, &pageRequests, &cookies, &mu)
	if response := postExtraction(t, app, `{
		"url":"https://www.xiaohongshu.com/explore/fixture123"
	}`); response.Code != http.StatusOK {
		t.Fatalf("extraction = %d %s", response.Code, response.Body.String())
	}
	cookie := loginAdmin(t, app)

	historyRecorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		historyRecorder,
		authenticatedRequest(
			http.MethodGet, "http://example.test/api/admin/v1/history?limit=1", nil, cookie,
		),
	)
	if historyRecorder.Code != http.StatusOK {
		t.Fatalf("history = %d %s", historyRecorder.Code, historyRecorder.Body.String())
	}
	var history historyPage
	if err := json.Unmarshal(historyRecorder.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Items) != 1 || history.Items[0].Work == nil || history.Items[0].Version == nil {
		t.Fatalf("history page = %#v", history)
	}

	workRecorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		workRecorder,
		authenticatedRequest(
			http.MethodGet,
			"http://example.test/api/admin/v1/works/"+jsonNumber(history.Items[0].Work.ID),
			nil,
			cookie,
		),
	)
	if workRecorder.Code != http.StatusOK {
		t.Fatalf("work detail = %d %s", workRecorder.Code, workRecorder.Body.String())
	}
	if strings.Contains(workRecorder.Body.String(), "local_path") ||
		strings.Contains(workRecorder.Body.String(), "relative_path") {
		t.Fatalf("work detail leaked a local storage path: %s", workRecorder.Body.String())
	}
	var detail workDetail
	if err := json.Unmarshal(workRecorder.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Work.PlatformID != "fixture123" || len(detail.Versions) != 1 ||
		len(detail.Versions[0].Resources) == 0 {
		t.Fatalf("work detail = %#v", detail)
	}
}

func TestExtractionRejectsOversizedURLAndStoresOnlyErrorCode(t *testing.T) {
	app := newTestApp(t)
	oversized := strings.Repeat("x", maxRequestedURLBytes+1)
	response := postExtraction(t, app, `{"url":"`+oversized+`"}`)
	if response.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(response.Body.String(), `"code":"URL_TOO_LONG"`) {
		t.Fatalf("oversized URL = %d %s", response.Code, response.Body.String())
	}
	var count int
	if err := app.store.db.QueryRow("SELECT COUNT(*) FROM parse_runs").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("oversized URL created %d parse runs", count)
	}

	failed := postExtraction(t, app, `{"url":"https://example.com/not-supported"}`)
	if failed.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unsupported URL = %d %s", failed.Code, failed.Body.String())
	}
	var historyError string
	if err := app.store.db.QueryRow(
		"SELECT error FROM parse_runs ORDER BY id DESC LIMIT 1",
	).Scan(&historyError); err != nil {
		t.Fatal(err)
	}
	if historyError != "UNSUPPORTED_LINK" {
		t.Fatalf("history error = %q, want stable code", historyError)
	}
}

func TestLegacyDownloadIsOptInAndAdminBounded(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	app := newTestApp(t)
	var pageRequests int
	var cookies []string
	var mu sync.Mutex
	app.clientFactory = fixtureClientFactory(t, fixture, &pageRequests, &cookies, &mu)
	enabled := true
	if _, err := app.store.updateSettings(t.Context(), settingsUpdate{
		Revision: 1, SaveText: &enabled,
	}); err != nil {
		t.Fatal(err)
	}
	requestLegacy := func(download bool) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodPost,
			"/xhs/detail",
			strings.NewReader(`{"url":"https://www.xiaohongshu.com/explore/fixture123","download":`+
				strconv.FormatBool(download)+`}`),
		)
		app.Handler().ServeHTTP(recorder, request)
		return recorder
	}
	if response := requestLegacy(false); response.Code != http.StatusOK {
		t.Fatalf("legacy opt-out = %d %s", response.Code, response.Body.String())
	}
	textPath := filepath.Join(app.config.VolumeDir, "Download", "fixture123", "v1", "work.json")
	if _, err := os.Stat(textPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy download=false created text artifact: %v", err)
	}
	if response := requestLegacy(true); response.Code != http.StatusOK {
		t.Fatalf("legacy opt-in = %d %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(textPath); err != nil {
		t.Fatalf("legacy download=true did not create admin-enabled text artifact: %v", err)
	}

	blocked := newTestApp(t)
	blocked.clientFactory = fixtureClientFactory(t, fixture, &pageRequests, &cookies, &mu)
	recorder := httptest.NewRecorder()
	blocked.Handler().ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodPost,
			"/xhs/detail",
			strings.NewReader(`{"url":"https://www.xiaohongshu.com/explore/fixture123","download":true}`),
		),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("admin-disabled legacy request = %d %s", recorder.Code, recorder.Body.String())
	}
	blockedText := filepath.Join(blocked.config.VolumeDir, "Download", "fixture123", "v1", "work.json")
	if _, err := os.Stat(blockedText); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy request bypassed admin save policy: %v", err)
	}
}

func jsonNumber(value int64) string {
	content, _ := json.Marshal(value)
	return string(content)
}
