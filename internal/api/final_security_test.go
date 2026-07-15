package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCacheScopeNormalizesAndIsolatesAuthorizationQuery(t *testing.T) {
	app := newTestApp(t)
	scopeA := app.store.secrets.cacheScope(nil, nil, "https://WWW.XIAOHONGSHU.COM:443/explore/note?source=web&xsec_token=A#ignored")
	scopeAReordered := app.store.secrets.cacheScope(nil, nil, "https://www.xiaohongshu.com/explore/note?xsec_token=A&source=web")
	scopeB := app.store.secrets.cacheScope(nil, nil, "https://www.xiaohongshu.com/explore/note?source=web&xsec_token=B")
	scopeNone := app.store.secrets.cacheScope(nil, nil, "https://www.xiaohongshu.com/explore/note")
	if scopeA != scopeAReordered {
		t.Fatal("equivalent query order or default HTTPS port changed cache scope")
	}
	if scopeA == scopeB || scopeA == scopeNone || scopeB == scopeNone {
		t.Fatal("different authorization query contexts shared a cache scope")
	}
	malformed := app.store.secrets.cacheScope(nil, nil, "https://www.xiaohongshu.com/explore/note?x=%zz")
	if malformed == scopeNone {
		t.Fatal("malformed raw query was silently merged with an empty query")
	}
}

func TestHealthReportsDatabaseUnavailableWithoutLeakingError(t *testing.T) {
	app := newTestApp(t)
	if err := app.store.db.Close(); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"code":"DATABASE_UNAVAILABLE"`) {
		t.Fatalf("health response = %d %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(strings.ToLower(recorder.Body.String()), "sql") {
		t.Fatalf("health response leaked database error: %s", recorder.Body.String())
	}
}

func TestVolumeLockRejectsOverlapAndAllowsRestart(t *testing.T) {
	root := t.TempDir()
	config := Config{VolumeDir: filepath.Join(root, "Volume"), WebDistDir: filepath.Join(root, "dist")}
	first, err := New(config, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(config, log.New(io.Discard, "", 0))
	if second != nil {
		_ = second.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "already in use by another application instance") {
		t.Fatalf("overlapping startup error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(config, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("restart after lock release: %v", err)
	}
	if err := restarted.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDatabasePathContainmentRejectsExternalAndSymlinkWithoutSideEffects(t *testing.T) {
	root := t.TempDir()
	volume := filepath.Join(root, "Volume")
	external := filepath.Join(root, "external")
	_, err := New(Config{VolumeDir: volume, DatabasePath: filepath.Join(external, "xhs.sqlite3")}, log.New(io.Discard, "", 0))
	if err == nil || !strings.Contains(err.Error(), "XHS_DATABASE_PATH") {
		t.Fatalf("external database path error = %v", err)
	}
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(volume, "Data")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	escapedChild := filepath.Join(external, "nested")
	_, err = New(Config{
		VolumeDir: volume, DatabasePath: filepath.Join(volume, "Data", "nested", "xhs.sqlite3"),
	}, log.New(io.Discard, "", 0))
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink database path error = %v", err)
	}
	if _, statErr := os.Stat(escapedChild); !os.IsNotExist(statErr) {
		t.Fatalf("containment rejection created outside directory: %v", statErr)
	}
}

func TestAdminCredentialFingerprintIsKeyedAndInvalidatesSessions(t *testing.T) {
	app := newAdminTestApp(t)
	cookie := loginAdmin(t, app)
	raw := sha256.Sum256([]byte("admin\x00" + testAdminPassword))
	if bytes.Equal(raw[:], app.adminFingerprint[:]) {
		t.Fatal("administrator fingerprint equals the legacy bare SHA-256")
	}
	var stored []byte
	if err := app.store.db.QueryRow("SELECT credential_fingerprint FROM admin_sessions LIMIT 1").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, app.adminFingerprint[:]) || bytes.Equal(stored, raw[:]) {
		t.Fatal("stored credential fingerprint is not the keyed application fingerprint")
	}
	tokenHash := sha256.Sum256([]byte(cookie.Value))
	changedPassword, _ := app.store.secrets.adminCredentialFingerprint("admin", "different-password")
	if _, valid, err := app.store.sessionValid(t.Context(), tokenHash[:], changedPassword[:], time.Now()); err != nil || valid {
		t.Fatalf("password-changed session valid = %t, err = %v", valid, err)
	}
	keyPath := filepath.Join(t.TempDir(), "other.key")
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x7f}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	otherSecrets, err := loadOrCreateSecretCipher(keyPath, false)
	if err != nil {
		t.Fatal(err)
	}
	changedKey, _ := otherSecrets.adminCredentialFingerprint("admin", testAdminPassword)
	if _, valid, err := app.store.sessionValid(t.Context(), tokenHash[:], changedKey[:], time.Now()); err != nil || valid {
		t.Fatalf("key-changed session valid = %t, err = %v", valid, err)
	}
}

func TestSupportedHistoryURLDropsAuthorizationQuery(t *testing.T) {
	app := newTestApp(t)
	fixture, err := os.ReadFile(filepath.Join("testdata", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	var pageRequests int
	var cookies []string
	var mu sync.Mutex
	app.clientFactory = fixtureClientFactory(t, fixture, &pageRequests, &cookies, &mu)
	secret := "token-that-must-not-be-stored"
	response := postExtraction(t, app, `{"url":"分享 https://www.xiaohongshu.com/explore/fixture123?xsec_token=`+secret+`&source=web#fragment"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("extraction = %d %s", response.Code, response.Body.String())
	}
	var requestedURL string
	if err := app.store.db.QueryRow("SELECT requested_url FROM parse_runs ORDER BY id DESC LIMIT 1").Scan(&requestedURL); err != nil {
		t.Fatal(err)
	}
	if requestedURL != "https://www.xiaohongshu.com/explore/fixture123" || strings.Contains(requestedURL, secret) {
		t.Fatalf("stored requested URL = %q", requestedURL)
	}
}

func fixtureMediaClientFactory(fixture, jpeg, mp4 []byte, pageRequests *int) clientFactory {
	return func(_ *string) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			content := jpeg
			contentType := "image/jpeg"
			if request.URL.Host == "www.xiaohongshu.com" {
				*pageRequests++
				content = fixture
				contentType = "text/html; charset=utf-8"
			} else if strings.Contains(request.URL.Path, "live") {
				content = mp4
				contentType = "video/mp4"
			}
			headers := make(http.Header)
			headers.Set("Content-Type", contentType)
			return &http.Response{
				StatusCode: http.StatusOK, Status: "200 OK", Header: headers,
				Body: io.NopCloser(bytes.NewReader(content)), ContentLength: int64(len(content)), Request: request,
			}, nil
		})}, nil
	}
}

func TestCachedVersionCanPersistMediaAfterPolicyEnabled(t *testing.T) {
	app := newTestApp(t)
	fixture, err := os.ReadFile(filepath.Join("testdata", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	jpeg := testJPEG("cached-image")
	mp4 := testISOBaseMedia("isom", "cached-live")
	pageRequests := 0
	app.clientFactory = fixtureMediaClientFactory(fixture, jpeg, mp4, &pageRequests)
	first := postExtraction(t, app, `{"url":"https://www.xiaohongshu.com/explore/fixture123"}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first extraction = %d %s", first.Code, first.Body.String())
	}
	enabled, disabled := true, false
	if _, err := app.store.updateSettings(t.Context(), settingsUpdate{
		Revision: 1, Refetch: &disabled, SaveImages: &enabled, SaveVideos: &enabled,
	}); err != nil {
		t.Fatal(err)
	}
	second := postExtraction(t, app, `{"url":"https://www.xiaohongshu.com/explore/fixture123"}`)
	if second.Code != http.StatusOK {
		t.Fatalf("cached extraction = %d %s", second.Code, second.Body.String())
	}
	var response extractionV1Response
	if err := json.Unmarshal(second.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Source != "cache" || pageRequests != 1 {
		t.Fatalf("cached response source=%q pageRequests=%d", response.Source, pageRequests)
	}
	stored := 0
	for _, resource := range response.Version.Resources {
		if resource.Kind != "text" && resource.SaveStatus == "saved" {
			stored++
		}
	}
	if stored != 3 {
		t.Fatalf("stored cached media = %d, resources = %#v", stored, response.Version.Resources)
	}
}

func TestLegacyMediaFailureReturnsOnlyStableCodeAndLogsCause(t *testing.T) {
	root := t.TempDir()
	fixture, err := os.ReadFile(filepath.Join("testdata", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	jpeg := testJPEG("too-large")
	mp4 := testISOBaseMedia("isom", "unused")
	var logs bytes.Buffer
	app, err := New(Config{
		VolumeDir: filepath.Join(root, "Volume"), WebDistDir: filepath.Join(root, "dist"),
		MaxMediaBytes: int64(len(jpeg) - 1), AllowPrivateProxy: true,
	}, log.New(&logs, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	enablePublicTestAccess(t, app)
	pageRequests := 0
	app.clientFactory = fixtureMediaClientFactory(fixture, jpeg, mp4, &pageRequests)
	enabled := true
	if _, err := app.store.updateSettings(t.Context(), settingsUpdate{Revision: 1, SaveImages: &enabled}); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(recorder, httptest.NewRequest(
		http.MethodPost, "/xhs/detail",
		strings.NewReader(`{"url":"https://www.xiaohongshu.com/explore/fixture123","download":true,"index":[1]}`),
	))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"下载错误":"MEDIA_TOO_LARGE"`) {
		t.Fatalf("legacy failure response = %d %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "media exceeds") || !strings.Contains(logs.String(), "media exceeds") {
		t.Fatalf("raw cause response=%s logs=%s", recorder.Body.String(), logs.String())
	}
	var saveError string
	if err := app.store.db.QueryRow("SELECT save_error FROM version_resources WHERE save_status = 'failed' LIMIT 1").Scan(&saveError); err != nil {
		t.Fatal(err)
	}
	if saveError != saveErrorMediaTooLarge {
		t.Fatalf("stored save_error = %q", saveError)
	}
	noDownload := httptest.NewRecorder()
	app.Handler().ServeHTTP(noDownload, httptest.NewRequest(
		http.MethodPost, "/xhs/detail",
		strings.NewReader(`{"url":"https://www.xiaohongshu.com/explore/fixture123","download":false}`),
	))
	if strings.Contains(noDownload.Body.String(), "下载错误") {
		t.Fatalf("download=false response contained 下载错误: %s", noDownload.Body.String())
	}
}

func TestMediaLimitRejectsChunkedAndExistingOversizedFiles(t *testing.T) {
	content := testJPEG("oversized-stream")
	limit := int64(len(content) - 1)
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing=%t", existing), func(t *testing.T) {
			root := t.TempDir()
			tempDir, downloadDir := filepath.Join(root, "Temp"), filepath.Join(root, "Download")
			if err := os.MkdirAll(tempDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(downloadDir, 0o755); err != nil {
				t.Fatal(err)
			}
			task := downloadTask{url: "https://cdn.example/large.jpg", baseName: "large", extension: "jpeg"}
			if existing {
				if err := os.WriteFile(filepath.Join(downloadDir, "large.jpeg"), content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if existing {
					t.Fatal("existing completed file unexpectedly reached upstream")
				}
				return &http.Response{
					StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
					Body: io.NopCloser(bytes.NewReader(content)), ContentLength: -1, Request: request,
				}, nil
			})}
			err := downloadFile(context.Background(), client, mediaHeaders(), tempDir, downloadDir, task, 0, true, limit)
			if !errors.Is(err, errMediaTooLarge) {
				t.Fatalf("download error = %v", err)
			}
			if existing {
				if _, err := os.Stat(filepath.Join(downloadDir, "large.jpeg")); err != nil {
					t.Fatalf("oversized existing file was removed: %v", err)
				}
			}
		})
	}
}

func TestLegacyProxyFailureRedactsConnectionValues(t *testing.T) {
	app := newAdminTestApp(t)
	cookie := loginAdmin(t, app)
	secretProxy := "http://user:proxy-secret@127.0.0.1:8080"
	secretCookie := "web_session=cookie-secret"
	app.clientFactory = func(proxy *string) (*http.Client, error) {
		return nil, fmt.Errorf("cannot use proxy %s", *proxy)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost, "/xhs/detail",
		strings.NewReader(`{"url":"https://www.xiaohongshu.com/explore/fixture123","cookie":"`+secretCookie+`","proxy":"`+secretProxy+`"}`),
	)
	request.AddCookie(cookie)
	app.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("proxy failure = %d %s", recorder.Code, recorder.Body.String())
	}
	for _, secret := range []string{"user", "proxy-secret", "cookie-secret", secretProxy, secretCookie} {
		if strings.Contains(recorder.Body.String(), secret) {
			t.Fatalf("proxy failure leaked %q: %s", secret, recorder.Body.String())
		}
	}
}
