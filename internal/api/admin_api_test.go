package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testAdminPassword = "test-only-strong-password"

func newAdminTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	app, err := New(Config{
		VolumeDir:         filepath.Join(root, "Volume"),
		WebDistDir:        filepath.Join(root, "dist"),
		AdminUsername:     "admin",
		AdminPassword:     testAdminPassword,
		AllowPrivateProxy: true,
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	return app
}

func loginAdmin(t *testing.T, app *App) *http.Cookie {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"http://example.test/api/admin/v1/auth/session",
		strings.NewReader(`{"username":"admin","password":"test-only-strong-password"}`),
	)
	request.Header.Set("Origin", "http://example.test")
	app.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %#v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != adminSessionCookie || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie = %#v", cookie)
	}
	return cookie
}

func authenticatedRequest(method, target string, body io.Reader, cookie *http.Cookie) *http.Request {
	request := httptest.NewRequest(method, target, body)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	return request
}

func TestAdminSessionSettingsEncryptionAndPublicBoundary(t *testing.T) {
	app := newAdminTestApp(t)

	unauthorized := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		unauthorized,
		httptest.NewRequest(http.MethodGet, "/api/admin/v1/settings", nil),
	)
	if unauthorized.Code != http.StatusUnauthorized ||
		!strings.Contains(unauthorized.Body.String(), `"code":"AUTHENTICATION_REQUIRED"`) {
		t.Fatalf("unauthorized settings = %d %s", unauthorized.Code, unauthorized.Body.String())
	}

	crossOrigin := httptest.NewRecorder()
	crossOriginRequest := httptest.NewRequest(
		http.MethodPost,
		"http://example.test/api/admin/v1/auth/session",
		strings.NewReader(`{"username":"admin","password":"test-only-strong-password"}`),
	)
	crossOriginRequest.Header.Set("Origin", "https://evil.example")
	app.Handler().ServeHTTP(crossOrigin, crossOriginRequest)
	if crossOrigin.Code != http.StatusForbidden {
		t.Fatalf("cross-origin login = %d %s", crossOrigin.Code, crossOrigin.Body.String())
	}

	cookie := loginAdmin(t, app)
	settingsRecorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		settingsRecorder,
		authenticatedRequest(
			http.MethodGet, "http://example.test/api/admin/v1/settings", nil, cookie,
		),
	)
	if settingsRecorder.Code != http.StatusOK {
		t.Fatalf("settings = %d %s", settingsRecorder.Code, settingsRecorder.Body.String())
	}
	var defaults settingsResponse
	if err := json.Unmarshal(settingsRecorder.Body.Bytes(), &defaults); err != nil {
		t.Fatal(err)
	}
	if defaults.Revision != 1 || !defaults.Public || defaults.ShowPopular || defaults.Save.Text ||
		defaults.Save.Images || defaults.Save.Videos || !defaults.Refetch {
		t.Fatalf("default settings = %#v", defaults)
	}

	secretCookie := "web_session=super-secret"
	secretProxy := "http://proxy-user:proxy-pass@127.0.0.1:8080"
	patchRecorder := httptest.NewRecorder()
	patchRequest := authenticatedRequest(
		http.MethodPatch,
		"http://example.test/api/admin/v1/settings",
		strings.NewReader(`{
			"revision":1,
			"public":false,
			"show_popular":true,
			"save":{"text":true,"images":true,"videos":false},
			"refetch":false,
			"default_cookie":{"action":"replace","value":"web_session=super-secret"},
			"default_proxy":{"action":"replace","value":"http://proxy-user:proxy-pass@127.0.0.1:8080"}
		}`),
		cookie,
	)
	patchRequest.Header.Set("Origin", "http://example.test")
	app.Handler().ServeHTTP(patchRecorder, patchRequest)
	if patchRecorder.Code != http.StatusOK {
		t.Fatalf("patch settings = %d %s", patchRecorder.Code, patchRecorder.Body.String())
	}
	body := patchRecorder.Body.String()
	if strings.Contains(body, secretCookie) || strings.Contains(body, "proxy-user") ||
		strings.Contains(body, "proxy-pass") {
		t.Fatalf("settings response leaked a secret: %s", body)
	}
	if !strings.Contains(body, `"display":"http://127.0.0.1:8080"`) {
		t.Fatalf("proxy display was not safely redacted: %s", body)
	}
	if !strings.Contains(body, `"show_popular":true`) {
		t.Fatalf("popular setting was not updated: %s", body)
	}

	var encryptedCookie, encryptedProxy string
	if err := app.store.db.QueryRow(
		"SELECT default_cookie, default_proxy FROM app_settings WHERE id = 1",
	).Scan(&encryptedCookie, &encryptedProxy); err != nil {
		t.Fatal(err)
	}
	if encryptedCookie == secretCookie || encryptedProxy == secretProxy ||
		strings.Contains(encryptedCookie, "super-secret") || strings.Contains(encryptedProxy, "proxy-pass") {
		t.Fatal("application settings were stored without encryption")
	}

	staleRecorder := httptest.NewRecorder()
	staleRequest := authenticatedRequest(
		http.MethodPatch,
		"http://example.test/api/admin/v1/settings",
		strings.NewReader(`{"revision":1,"default_cookie":{"action":"replace","value":""}}`),
		cookie,
	)
	staleRequest.Header.Set("Origin", "http://example.test")
	app.Handler().ServeHTTP(staleRecorder, staleRequest)
	if staleRecorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty replace = %d %s", staleRecorder.Code, staleRecorder.Body.String())
	}

	access := httptest.NewRecorder()
	app.Handler().ServeHTTP(access, httptest.NewRequest(http.MethodGet, "/api/v1/access", nil))
	if access.Code != http.StatusOK ||
		!strings.Contains(access.Body.String(), `"public":false`) ||
		!strings.Contains(access.Body.String(), `"can_extract":false`) {
		t.Fatalf("anonymous access = %d %s", access.Code, access.Body.String())
	}
	privateV1 := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		privateV1,
		httptest.NewRequest(http.MethodPost, "/api/v1/extractions", strings.NewReader(`{"url":"x"}`)),
	)
	if privateV1.Code != http.StatusUnauthorized {
		t.Fatalf("private v1 extraction = %d %s", privateV1.Code, privateV1.Body.String())
	}
	privateLegacy := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		privateLegacy,
		httptest.NewRequest(http.MethodPost, "/xhs/detail", strings.NewReader(`{"url":"x"}`)),
	)
	if privateLegacy.Code != http.StatusUnauthorized {
		t.Fatalf("private legacy extraction = %d %s", privateLegacy.Code, privateLegacy.Body.String())
	}
	for _, path := range []string{"/api/v1/extractions", "/xhs/detail"} {
		recorder := httptest.NewRecorder()
		request := authenticatedRequest(
			http.MethodPost,
			"http://example.test"+path,
			strings.NewReader(`{"url":"https://www.xiaohongshu.com/explore/fixture123"}`),
			cookie,
		)
		request.Header.Set("Origin", "http://evil.example")
		app.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("cross-origin private extraction %s = %d %s", path, recorder.Code, recorder.Body.String())
		}
	}

	logout := httptest.NewRecorder()
	logoutRequest := authenticatedRequest(
		http.MethodDelete, "http://example.test/api/admin/v1/auth/session", nil, cookie,
	)
	logoutRequest.Header.Set("Origin", "http://example.test")
	app.Handler().ServeHTTP(logout, logoutRequest)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout = %d %s", logout.Code, logout.Body.String())
	}
	status := httptest.NewRecorder()
	app.Handler().ServeHTTP(
		status,
		authenticatedRequest(http.MethodGet, "/api/admin/v1/auth/session", nil, cookie),
	)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"authenticated":false`) {
		t.Fatalf("logged-out session = %d %s", status.Code, status.Body.String())
	}
}

func TestAdminLoginRateLimit(t *testing.T) {
	app := newAdminTestApp(t)
	for attempt := 1; attempt <= 6; attempt++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodPost,
			"http://example.test/api/admin/v1/auth/session",
			strings.NewReader(`{"username":"admin","password":"wrong"}`),
		)
		request.Header.Set("Origin", "http://example.test")
		request.RemoteAddr = "192.0.2.1:4000"
		app.Handler().ServeHTTP(recorder, request)
		expected := http.StatusUnauthorized
		if attempt == 6 {
			expected = http.StatusTooManyRequests
		}
		if recorder.Code != expected {
			t.Fatalf("attempt %d = %d %s, want %d", attempt, recorder.Code, recorder.Body.String(), expected)
		}
	}
}

func TestAdminLoginLimiterIsHostScopedAndBounded(t *testing.T) {
	app := newAdminTestApp(t)
	for attempt := 0; attempt < 100; attempt++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodPost,
			"http://example.test/api/admin/v1/auth/session",
			strings.NewReader(fmt.Sprintf(`{"username":"random-%d","password":"wrong"}`, attempt)),
		)
		request.Header.Set("Origin", "http://example.test")
		request.RemoteAddr = "192.0.2.5:4000"
		app.Handler().ServeHTTP(recorder, request)
	}
	app.loginLimiter.mu.Lock()
	entries := len(app.loginLimiter.attempts)
	app.loginLimiter.mu.Unlock()
	if entries != 1 {
		t.Fatalf("random usernames created %d limiter entries, want one remote-host entry", entries)
	}

	limiter := newAdminLoginLimiter()
	now := time.Now()
	for index := 0; index < maxLoginLimiterEntries+100; index++ {
		limiter.reserve(fmt.Sprintf("198.51.100.%d", index), now.Add(time.Duration(index)*time.Nanosecond))
	}
	limiter.mu.Lock()
	entries = len(limiter.attempts)
	limiter.mu.Unlock()
	if entries > maxLoginLimiterEntries {
		t.Fatalf("limiter entries = %d, max = %d", entries, maxLoginLimiterEntries)
	}
	if !limiter.reserve("new-host", now.Add(6*time.Minute)) {
		t.Fatal("expired limiter entries were not cleared")
	}
}

func TestRequiredAdminPasswordFailsStartup(t *testing.T) {
	_, err := New(Config{
		VolumeDir:             filepath.Join(t.TempDir(), "Volume"),
		AdminPasswordRequired: true,
	}, log.New(io.Discard, "", 0))
	if err == nil || !strings.Contains(err.Error(), "XHS_ADMIN_PASSWORD") {
		t.Fatalf("New() error = %v, want explicit admin password error", err)
	}
}
func TestAdminLoginLimiterConcurrentReservationsAreAtomic(t *testing.T) {
	const workers = 100
	limiter := newAdminLoginLimiter()
	now := time.Now()
	start := make(chan struct{})
	var wait sync.WaitGroup
	var accepted atomic.Int64
	wait.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer wait.Done()
			<-start
			if limiter.reserve("192.0.2.10", now) {
				accepted.Add(1)
			}
		}()
	}
	close(start)
	wait.Wait()
	if got := accepted.Load(); got != 5 {
		t.Fatalf("concurrent accepted attempts = %d, want 5", got)
	}
}
