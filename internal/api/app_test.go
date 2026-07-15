package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	app, err := New(Config{
		VolumeDir:       filepath.Join(t.TempDir(), "Volume"),
		WebDistDir:      filepath.Join(t.TempDir(), "dist"),
		MaxUpstreamBody: 2 << 20,
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	enablePublicTestAccess(t, app)
	t.Cleanup(func() { _ = app.Close() })
	return app
}

func enablePublicTestAccess(t *testing.T, app *App) {
	t.Helper()
	if _, err := app.store.db.Exec(`
		UPDATE app_settings
		SET public_enabled = 1, refetch_existing = 1
		WHERE id = 1
	`); err != nil {
		t.Fatal(err)
	}
}

func TestHealthAndValidation(t *testing.T) {
	app := newTestApp(t)

	health := httptest.NewRecorder()
	app.Handler().ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), `"status":"ok"`) {
		t.Fatalf("health response = %d %s", health.Code, health.Body.String())
	}

	invalid := httptest.NewRecorder()
	app.Handler().ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/xhs/detail", strings.NewReader(`{}`)))
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("validation status = %d, body = %s", invalid.Code, invalid.Body.String())
	}

	unsupported := httptest.NewRecorder()
	app.Handler().ServeHTTP(unsupported, httptest.NewRequest(http.MethodPost, "/xhs/detail", strings.NewReader(`{"url":"https://example.com/item"}`)))
	if unsupported.Code != http.StatusOK || !strings.Contains(unsupported.Body.String(), "提取小红书作品链接失败") {
		t.Fatalf("unsupported response = %d %s", unsupported.Code, unsupported.Body.String())
	}
}

func TestDetailContractWithFixture(t *testing.T) {
	app := newTestApp(t)
	fixture, err := os.ReadFile(filepath.Join("testdata", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	app.clientFactory = func(_ *string) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(fixture)),
				Request:    request,
			}, nil
		})}, nil
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/xhs/detail", strings.NewReader(`{"url":"https://www.xiaohongshu.com/explore/fixture123","download":false}`))
	request.Header.Set("Content-Type", "application/json")
	app.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response ExtractResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Message != "获取小红书作品数据成功" {
		t.Fatalf("message = %q", response.Message)
	}
	if response.Params.Download || response.Params.URL == "" {
		t.Fatalf("params = %#v", response.Params)
	}
	if got := stringValue(response.Data["作品ID"]); got != "fixture123" {
		t.Fatalf("作品ID = %q, body = %s", got, recorder.Body.String())
	}
}

func TestSkipRecordedWork(t *testing.T) {
	app := newTestApp(t)
	if err := app.records.Add("fixture123"); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/xhs/detail", strings.NewReader(`{"url":"https://www.xiaohongshu.com/explore/fixture123","skip":true}`))
	app.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "存在下载记录") {
		t.Fatalf("skip response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestServesSPAIndexAndFallback(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<main>web-console</main>"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, err := New(Config{VolumeDir: filepath.Join(root, "Volume"), WebDistDir: dist}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/some/client/route", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "web-console") {
		t.Fatalf("SPA response = %d %s", recorder.Code, recorder.Body.String())
	}
}

type delayedReadCloser struct {
	content []byte
	delay   time.Duration
	delayed bool
}

func (r *delayedReadCloser) Read(buffer []byte) (int, error) {
	if !r.delayed {
		time.Sleep(r.delay)
		r.delayed = true
	}
	if len(r.content) == 0 {
		return 0, io.EOF
	}
	length := copy(buffer, r.content)
	r.content = r.content[length:]
	return length, nil
}

func (r *delayedReadCloser) Close() error { return nil }

func TestRequestBodyTooLargeReturns413(t *testing.T) {
	root := t.TempDir()
	app, err := New(Config{
		VolumeDir:    filepath.Join(root, "Volume"),
		WebDistDir:   filepath.Join(root, "dist"),
		MaxBodyBytes: 64,
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	enablePublicTestAccess(t, app)

	body := `{"url":"https://www.xiaohongshu.com/explore/` + strings.Repeat("x", 128) + `"}`
	recorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/xhs/detail", strings.NewReader(body)))
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestPageTimeoutDoesNotLimitMediaStreaming(t *testing.T) {
	root := t.TempDir()
	app, err := New(Config{
		VolumeDir:       filepath.Join(root, "Volume"),
		WebDistDir:      filepath.Join(root, "dist"),
		RequestTimeout:  10 * time.Millisecond,
		MaxUpstreamBody: 2 << 20,
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	enablePublicTestAccess(t, app)

	fixture, err := os.ReadFile(filepath.Join("testdata", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	jpeg := testJPEG("slow-image")
	mp4 := testISOBaseMedia("isom", "slow-live")
	app.clientFactory = func(_ *string) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body := io.ReadCloser(io.NopCloser(bytes.NewReader(fixture)))
			if request.URL.Host != "www.xiaohongshu.com" {
				content := jpeg
				if strings.Contains(request.URL.Path, "live-1.mp4") {
					content = mp4
				}
				body = &delayedReadCloser{content: content, delay: 40 * time.Millisecond}
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       body,
				Request:    request,
			}, nil
		})}, nil
	}
	enabled := true
	if _, err := app.store.updateSettings(t.Context(), settingsUpdate{
		Revision: 1, SaveImages: &enabled, SaveVideos: &enabled,
	}); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/xhs/detail",
		strings.NewReader(`{"url":"https://www.xiaohongshu.com/explore/fixture123","download":true,"index":[1]}`),
	)
	app.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "下载错误") {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	files, err := filepath.Glob(filepath.Join(root, "Volume", "Download", "fixture123", "v1", "*"))
	if err != nil || len(files) != 2 {
		t.Fatalf("downloaded files = %#v, err = %v", files, err)
	}
}

func TestPageFetchHonorsRequestTimeout(t *testing.T) {
	root := t.TempDir()
	app, err := New(Config{
		VolumeDir:      filepath.Join(root, "Volume"),
		WebDistDir:     filepath.Join(root, "dist"),
		RequestTimeout: 20 * time.Millisecond,
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	enablePublicTestAccess(t, app)

	app.clientFactory = func(_ *string) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		})}, nil
	}
	started := time.Now()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/xhs/detail",
		strings.NewReader(`{"url":"https://www.xiaohongshu.com/explore/fixture123"}`),
	)
	app.Handler().ServeHTTP(recorder, request)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("page fetch elapsed = %s, timeout was not enforced", elapsed)
	}
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "获取小红书作品数据失败") {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}
