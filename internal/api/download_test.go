package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDownloadSelectedImageAndRecordStore(t *testing.T) {
	jpeg := testJPEG("fixture-image")
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/jpeg")
		_, _ = writer.Write(jpeg)
	}))
	defer server.Close()

	volume := filepath.Join(t.TempDir(), "Volume")
	data := map[string]any{
		"作品ID": "fixture123",
		"作品类型": "图文",
		"作品标题": "测试作品",
		"作者昵称": "测试作者",
		"发布时间": "2024-01-01_00:00:00",
		"下载地址": []string{server.URL + "/1", server.URL + "/2"},
		"动图地址": []any{nil, nil},
	}
	coordinator := newDownloadCoordinator(defaultDownloadConcurrency)
	if err := downloadWork(context.Background(), server.Client(), volume, data, []any{json.Number("2")}, coordinator); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(volume, "Download", "*_2.jpeg"))
	if err != nil || len(files) != 1 {
		t.Fatalf("downloaded files = %#v, err = %v", files, err)
	}
	if !strings.Contains(filepath.Base(files[0]), "fixture123") {
		t.Fatalf("download filename %q does not contain work ID", filepath.Base(files[0]))
	}
	if first, _ := filepath.Glob(filepath.Join(volume, "Download", "*_1.*")); len(first) != 0 {
		t.Fatalf("unexpected first image download = %#v", first)
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != len(jpeg) {
		t.Fatalf("downloaded size = %d, want %d", len(content), len(jpeg))
	}

	store, err := openRecordStore(volume)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add("fixture123"); err != nil {
		t.Fatal(err)
	}
	reopened, err := openRecordStore(volume)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.Has("fixture123") {
		t.Fatal("record was not persisted")
	}
}

func TestDownloadSeparatesStaticAndLivePhotoNames(t *testing.T) {
	jpeg := testJPEG("static")
	mp4 := testISOBaseMedia("isom", "live")
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/image":
			_, _ = writer.Write(jpeg)
		case "/live":
			_, _ = writer.Write(mp4)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	volume := filepath.Join(t.TempDir(), "Volume")
	data := map[string]any{
		"作品ID": "live123",
		"作品类型": "图文",
		"作品标题": "同名媒体",
		"作者昵称": "作者",
		"发布时间": "2024-01-01_00:00:00",
		"下载地址": []string{server.URL + "/image"},
		"动图地址": []any{server.URL + "/live"},
	}
	downloadDir := filepath.Join(volume, "Download")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	staticBase := downloadBaseName(data) + "_1"
	if err := os.WriteFile(filepath.Join(downloadDir, staticBase+".mp4"), mp4, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := downloadWork(context.Background(), server.Client(), volume, data, nil, newDownloadCoordinator(2)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{staticBase + ".jpeg", staticBase + "_live.mp4"} {
		path := filepath.Join(downloadDir, name)
		if exists, err := regularFileExists(path); err != nil || !exists {
			t.Fatalf("expected %s, exists = %v, err = %v", path, exists, err)
		}
		if !strings.Contains(name, "live123") {
			t.Fatalf("download filename %q does not contain work ID", name)
		}
	}
}

func TestDownloadCoordinatorLimitsAcrossWorks(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	jpeg := testJPEG("concurrent")
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		_, _ = writer.Write(jpeg)
	}))
	defer server.Close()

	volume := filepath.Join(t.TempDir(), "Volume")
	coordinator := newDownloadCoordinator(2)
	var wait sync.WaitGroup
	errorsChannel := make(chan error, 2)
	for work := 1; work <= 2; work++ {
		work := work
		wait.Add(1)
		go func() {
			defer wait.Done()
			data := map[string]any{
				"作品ID": "work-" + string(rune('0'+work)),
				"作品类型": "图文",
				"下载地址": []string{server.URL + "/1", server.URL + "/2", server.URL + "/3"},
				"动图地址": []any{nil, nil, nil},
			}
			errorsChannel <- downloadWork(context.Background(), server.Client(), volume, data, nil, coordinator)
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := maximum.Load(); got > 2 {
		t.Fatalf("maximum concurrent downloads = %d, want <= 2", got)
	}
	if got := maximum.Load(); got < 2 {
		t.Fatalf("maximum concurrent downloads = %d, test did not exercise concurrency", got)
	}
}

func TestDownloadCoordinatorSerializesSameWork(t *testing.T) {
	coordinator := newDownloadCoordinator(2)
	unlockFirst, err := coordinator.lockWork(context.Background(), "same-work")
	if err != nil {
		t.Fatal(err)
	}

	acquired := make(chan func(), 1)
	errs := make(chan error, 1)
	go func() {
		unlock, lockErr := coordinator.lockWork(context.Background(), "same-work")
		if lockErr != nil {
			errs <- lockErr
			return
		}
		acquired <- unlock
	}()

	select {
	case <-acquired:
		t.Fatal("same work lock was acquired concurrently")
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(50 * time.Millisecond):
	}
	unlockFirst()

	var unlockSecond func()
	select {
	case unlockSecond = <-acquired:
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for same work lock")
	}
	unlockSecond()

	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if len(coordinator.works) != 0 {
		t.Fatalf("work locks were not released: %#v", coordinator.works)
	}
}
