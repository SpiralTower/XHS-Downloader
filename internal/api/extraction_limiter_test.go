package api

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPublicExtractionGateRateLimitsAndResets(t *testing.T) {
	gate := newPublicExtractionGate(2, 3, 2)
	now := time.Now().UTC()
	for attempt := 0; attempt < 2; attempt++ {
		if retryAfter, allowed := gate.reserve("192.0.2.10", now); !allowed || retryAfter != 0 {
			t.Fatalf("host attempt %d = allowed:%t retry:%s", attempt+1, allowed, retryAfter)
		}
	}
	if retryAfter, allowed := gate.reserve("192.0.2.10", now); allowed || retryAfter <= 0 {
		t.Fatalf("per-host limit = allowed:%t retry:%s", allowed, retryAfter)
	}
	if retryAfter, allowed := gate.reserve("192.0.2.11", now); !allowed || retryAfter != 0 {
		t.Fatalf("neighbor request = allowed:%t retry:%s", allowed, retryAfter)
	}
	if retryAfter, allowed := gate.reserve("192.0.2.12", now); allowed || retryAfter <= 0 {
		t.Fatalf("global limit = allowed:%t retry:%s", allowed, retryAfter)
	}
	if retryAfter, allowed := gate.reserve(
		"192.0.2.10", now.Add(publicExtractionRateWindow),
	); !allowed || retryAfter != 0 {
		t.Fatalf("expired window = allowed:%t retry:%s", allowed, retryAfter)
	}
}

func TestPublicExtractionGateConcurrencyIsNonQueueing(t *testing.T) {
	gate := newPublicExtractionGate(10, 100, 1)
	release, retryAfter, allowed := gate.acquire()
	if !allowed || retryAfter != 0 {
		t.Fatalf("first admission = allowed:%t retry:%s", allowed, retryAfter)
	}
	if _, retryAfter, allowed = gate.acquire(); allowed || retryAfter <= 0 {
		t.Fatalf("busy admission = allowed:%t retry:%s", allowed, retryAfter)
	}
	release()
	release, retryAfter, allowed = gate.acquire()
	if !allowed || retryAfter != 0 {
		t.Fatalf("released admission = allowed:%t retry:%s", allowed, retryAfter)
	}
	release()
}

func TestAnonymousExtractionRateLimitCoversBothEndpoints(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		path string
		body string
	}{
		{
			name: "v1",
			path: "/api/v1/extractions",
			body: `{"url":"https://www.xiaohongshu.com/explore/fixture123"}`,
		},
		{
			name: "legacy",
			path: "/xhs/detail",
			body: `{"url":"https://www.xiaohongshu.com/explore/fixture123"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := newTestApp(t)
			app.publicExtractions = newPublicExtractionGate(1, 10, 2)
			var pageRequests int
			var cookies []string
			var mu sync.Mutex
			app.clientFactory = fixtureClientFactory(t, fixture, &pageRequests, &cookies, &mu)
			post := func() *httptest.ResponseRecorder {
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(
					http.MethodPost, test.path, strings.NewReader(test.body),
				)
				request.RemoteAddr = "192.0.2.25:41000"
				request.Header.Set("Content-Type", "application/json")
				app.Handler().ServeHTTP(recorder, request)
				return recorder
			}
			if first := post(); first.Code != http.StatusOK {
				t.Fatalf("first request = %d %s", first.Code, first.Body.String())
			}
			second := post()
			if second.Code != http.StatusTooManyRequests ||
				!strings.Contains(second.Body.String(), `"code":"EXTRACTION_RATE_LIMITED"`) ||
				second.Header().Get("Retry-After") == "" {
				t.Fatalf("limited request = %d %s headers=%v", second.Code, second.Body.String(), second.Header())
			}
			mu.Lock()
			requests := pageRequests
			mu.Unlock()
			if requests != 1 {
				t.Fatalf("upstream page requests = %d, want 1", requests)
			}
			var runCount int
			if err := app.store.db.QueryRow("SELECT COUNT(*) FROM parse_runs").Scan(&runCount); err != nil {
				t.Fatal(err)
			}
			if runCount != 1 {
				t.Fatalf("parse runs = %d, want 1", runCount)
			}
		})
	}
}

func TestAnonymousExtractionConcurrencyRejectsBeforeNewParseRun(t *testing.T) {
	app := newTestApp(t)
	app.publicExtractions = newPublicExtractionGate(10, 100, 1)
	fixture, err := os.ReadFile(filepath.Join("testdata", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 1)
	unblock := make(chan struct{})
	var unblockOnce sync.Once
	releaseUpstream := func() {
		unblockOnce.Do(func() { close(unblock) })
	}
	defer releaseUpstream()
	app.clientFactory = func(*string) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Host == "www.xiaohongshu.com" {
				select {
				case entered <- struct{}{}:
				default:
				}
				<-unblock
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
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/extractions",
			strings.NewReader(`{"url":"https://www.xiaohongshu.com/explore/fixture123"}`),
		)
		request.RemoteAddr = "192.0.2.30:42000"
		app.Handler().ServeHTTP(recorder, request)
		firstDone <- recorder
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first extraction did not reach the blocking upstream")
	}

	second := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/extractions",
		strings.NewReader(`{"url":"https://www.xiaohongshu.com/explore/fixture456"}`),
	)
	secondRequest.RemoteAddr = "192.0.2.31:42001"
	app.Handler().ServeHTTP(second, secondRequest)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("concurrent request = %d %s", second.Code, second.Body.String())
	}
	var runCount int
	if err := app.store.db.QueryRow("SELECT COUNT(*) FROM parse_runs").Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 1 {
		t.Fatalf("concurrency rejection created parse runs: %d", runCount)
	}
	releaseUpstream()
	select {
	case first := <-firstDone:
		if first.Code != http.StatusOK {
			t.Fatalf("first request = %d %s", first.Code, first.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first extraction did not finish after releasing upstream")
	}
}

func TestAuthenticatedExtractionHasIndependentCapacity(t *testing.T) {
	app := newAdminTestApp(t)
	cookie := loginAdmin(t, app)
	app.publicExtractions = newPublicExtractionGate(1, 1, 1)
	release, _, allowed := app.publicExtractions.acquire()
	if !allowed {
		t.Fatal("could not occupy anonymous extraction capacity")
	}
	defer release()

	fixture, err := os.ReadFile(filepath.Join("testdata", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	var pageRequests int
	var cookies []string
	var mu sync.Mutex
	app.clientFactory = fixtureClientFactory(t, fixture, &pageRequests, &cookies, &mu)
	response := postExtractionWithCookie(t, app, `{
		"url":"https://www.xiaohongshu.com/explore/fixture123",
		"connection":{"cookie":"web_session=admin-override"}
	}`, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated extraction = %d %s", response.Code, response.Body.String())
	}
}

type blockingRequestBody struct {
	reader  *strings.Reader
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (b *blockingRequestBody) Read(buffer []byte) (int, error) {
	b.once.Do(func() {
		close(b.entered)
		<-b.release
	})
	return b.reader.Read(buffer)
}

func (b *blockingRequestBody) Close() error { return nil }

func TestAnonymousExtractionAcquiresConcurrencyAfterBodyValidation(t *testing.T) {
	app := newTestApp(t)
	app.publicExtractions = newPublicExtractionGate(10, 100, 1)
	fixture, err := os.ReadFile(filepath.Join("testdata", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	var pageRequests int
	var cookies []string
	var mu sync.Mutex
	app.clientFactory = fixtureClientFactory(t, fixture, &pageRequests, &cookies, &mu)

	bodyEntered := make(chan struct{})
	bodyRelease := make(chan struct{})
	var releaseOnce sync.Once
	releaseBody := func() {
		releaseOnce.Do(func() { close(bodyRelease) })
	}
	defer releaseBody()

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/extractions",
			&blockingRequestBody{
				reader: strings.NewReader(
					`{"url":"https://www.xiaohongshu.com/explore/fixture123"}`,
				),
				entered: bodyEntered,
				release: bodyRelease,
			},
		)
		request.RemoteAddr = "192.0.2.50:43000"
		app.Handler().ServeHTTP(recorder, request)
		firstDone <- recorder
	}()
	select {
	case <-bodyEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("first extraction did not start reading its request body")
	}

	second := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/extractions",
		strings.NewReader(`{"url":"https://www.xiaohongshu.com/explore/fixture123"}`),
	)
	secondRequest.RemoteAddr = "192.0.2.51:43001"
	app.Handler().ServeHTTP(second, secondRequest)
	if second.Code != http.StatusOK {
		t.Fatalf("validated request was blocked by a slow request body: %d %s", second.Code, second.Body.String())
	}

	releaseBody()
	select {
	case first := <-firstDone:
		if first.Code != http.StatusOK {
			t.Fatalf("slow-body request = %d %s", first.Code, first.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("slow-body request did not finish after releasing its body")
	}
}
