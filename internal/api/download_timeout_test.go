package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type blockingReadCloser struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{closed: make(chan struct{})}
}

func (r *blockingReadCloser) Read([]byte) (int, error) {
	<-r.closed
	return 0, errors.New("body closed")
}

func (r *blockingReadCloser) Close() error {
	r.once.Do(func() { close(r.closed) })
	return nil
}

func TestDownloadIdleTimeoutReleasesGlobalSlot(t *testing.T) {
	volume := filepath.Join(t.TempDir(), "Volume")
	data := map[string]any{
		"作品ID": "idle-timeout",
		"作品类型": "图文",
		"下载地址": []string{"https://cdn.example/stalled.jpg"},
		"动图地址": []any{nil},
	}
	coordinator := newDownloadCoordinator(1, downloadLimits{
		totalTimeout: time.Second,
		idleTimeout:  25 * time.Millisecond,
	})
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		headers := make(http.Header)
		headers.Set("ETag", "\"stalled\"")
		return testDownloadResponse(request, http.StatusOK, headers, newBlockingReadCloser(), -1), nil
	})}

	started := time.Now()
	err := downloadWork(context.Background(), client, volume, data, nil, coordinator)
	if !errors.Is(err, errDownloadIdleTimeout) {
		t.Fatalf("downloadWork() error = %v, want idle timeout", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("idle timeout elapsed = %s", elapsed)
	}
	if got := len(coordinator.slots); got != 0 {
		t.Fatalf("occupied download slots = %d, want 0", got)
	}
	if files, _ := filepath.Glob(filepath.Join(volume, "Temp", "*.part")); len(files) != 0 {
		t.Fatalf("zero-length stalled partial files = %#v", files)
	}
}

func TestDownloadTotalTimeoutStartsAfterSlotAcquisition(t *testing.T) {
	volume := filepath.Join(t.TempDir(), "Volume")
	data := map[string]any{
		"作品ID": "slot-wait",
		"作品类型": "图文",
		"下载地址": []string{"https://cdn.example/quick.jpg"},
		"动图地址": []any{nil},
	}
	coordinator := newDownloadCoordinator(1, downloadLimits{
		totalTimeout: 30 * time.Millisecond,
		idleTimeout:  time.Second,
	})
	coordinator.slots <- struct{}{}
	content := testJPEG("quick-after-wait")
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return testDownloadResponse(
			request,
			http.StatusOK,
			nil,
			io.NopCloser(bytes.NewReader(content)),
			int64(len(content)),
		), nil
	})}

	result := make(chan error, 1)
	go func() {
		result <- downloadWork(context.Background(), client, volume, data, nil, coordinator)
	}()
	time.Sleep(50 * time.Millisecond)
	select {
	case err := <-result:
		t.Fatalf("download returned while still waiting for slot: %v", err)
	default:
	}
	<-coordinator.slots

	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("download did not finish after slot became available")
	}
	if got := len(coordinator.slots); got != 0 {
		t.Fatalf("occupied download slots = %d, want 0", got)
	}
}

func TestDownloadTotalTimeoutCancelsHeaderWaitAndReleasesSlot(t *testing.T) {
	volume := filepath.Join(t.TempDir(), "Volume")
	data := map[string]any{
		"作品ID": "total-timeout",
		"作品类型": "图文",
		"下载地址": []string{"https://cdn.example/no-headers.jpg"},
		"动图地址": []any{nil},
	}
	coordinator := newDownloadCoordinator(1, downloadLimits{
		totalTimeout: 25 * time.Millisecond,
		idleTimeout:  time.Second,
	})
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}

	err := downloadWork(context.Background(), client, volume, data, nil, coordinator)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("downloadWork() error = %v, want deadline exceeded", err)
	}
	if got := len(coordinator.slots); got != 0 {
		t.Fatalf("occupied download slots = %d, want 0", got)
	}
}

func TestImageAndLivePhotoIndexesStayAlignedWhenMediaTokenIsInvalid(t *testing.T) {
	images := []any{
		map[string]any{
			"url": "https://sns-webpic-qc.xhscdn.com/invalid/token",
			"stream": map[string]any{"h264": []any{
				map[string]any{"masterUrl": "https://cdn.example/live-1.mp4"},
			}},
		},
		map[string]any{
			"url": "https://sns-webpic-qc.xhscdn.com/202603010101/0a1b2c3d/valid-token!format",
			"stream": map[string]any{"h264": []any{
				map[string]any{"masterUrl": "https://cdn.example/live-2.mp4"},
			}},
		},
	}
	media, lives := imageURLs(images, "jpeg")
	if len(media) != 2 || len(lives) != 2 {
		t.Fatalf("media/lives lengths = %d/%d", len(media), len(lives))
	}
	if media[0] != "" || !strings.Contains(media[1], "valid-token") {
		t.Fatalf("media = %#v", media)
	}
	if firstString(lives[0]) != "https://cdn.example/live-1.mp4" ||
		firstString(lives[1]) != "https://cdn.example/live-2.mp4" {
		t.Fatalf("lives = %#v", lives)
	}

	volume := filepath.Join(t.TempDir(), "Volume")
	data := map[string]any{
		"作品ID": "aligned",
		"作品类型": "图文",
		"下载地址": media,
		"动图地址": lives,
	}
	calls := 0
	content := testISOBaseMedia("mp42", "live-one")
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.String() != "https://cdn.example/live-1.mp4" {
			t.Fatalf("requested URL = %s", request.URL)
		}
		return testDownloadResponse(
			request,
			http.StatusOK,
			nil,
			io.NopCloser(bytes.NewReader(content)),
			int64(len(content)),
		), nil
	})}
	if err := downloadWork(
		context.Background(),
		client,
		volume,
		data,
		[]any{json.Number("1")},
		newDownloadCoordinator(1),
	); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("RoundTrip calls = %d, want 1", calls)
	}
	expected := filepath.Join(volume, "Download", "aligned_1_live.mp4")
	if downloaded, err := os.ReadFile(expected); err != nil || !bytes.Equal(downloaded, content) {
		t.Fatalf("downloaded livePhoto = %q, err = %v", downloaded, err)
	}
	if files, _ := filepath.Glob(filepath.Join(volume, "Download", "*_2*")); len(files) != 0 {
		t.Fatalf("unexpected second-index downloads = %#v", files)
	}
}

func TestNormalizedMediaRequestURLRequiresHTTPFamilyAndUpgradesHTTPS(t *testing.T) {
	got, err := normalizedMediaRequestURL("http://cdn.example/media.jpg#fragment")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://cdn.example/media.jpg" {
		t.Fatalf("normalized URL = %q", got)
	}
	for _, raw := range []string{
		"ftp://cdn.example/media.jpg",
		"file:///tmp/media.jpg",
		"https://user:password@cdn.example/media.jpg",
		"not-a-url",
	} {
		if _, err := normalizedMediaRequestURL(raw); err == nil {
			t.Errorf("normalizedMediaRequestURL(%q) accepted unsafe URL", raw)
		}
	}
}
