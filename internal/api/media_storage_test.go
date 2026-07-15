package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPersistVersionResourcesHonorsIndependentPolicies(t *testing.T) {
	jpeg := testJPEG("stored-image")
	mp4 := testISOBaseMedia("isom", "disabled-live")
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "live") {
			writer.Header().Set("Content-Type", "video/mp4")
			_, _ = writer.Write(mp4)
			return
		}
		writer.Header().Set("Content-Type", "image/jpeg")
		_, _ = writer.Write(jpeg)
	}))
	defer server.Close()

	volume := filepath.Join(t.TempDir(), "Volume")
	data := map[string]any{
		"作品ID": "fixture123",
		"作品类型": "图文",
		"下载地址": []string{server.URL + "/image"},
		"动图地址": []any{server.URL + "/live"},
		"封面地址": server.URL + "/image",
		"作品标题": "测试作品",
		"作品描述": "文案内容",
	}
	results := persistVersionResources(
		context.Background(),
		server.Client(),
		volume,
		"fixture123",
		2,
		data,
		mediaPersistencePolicy{Text: true, Images: true, Videos: false},
		newDownloadCoordinator(2),
	)
	if len(results) != 3 {
		t.Fatalf("results = %#v", results)
	}

	byKind := make(map[string]mediaPersistenceResult)
	for _, result := range results {
		byKind[result.Kind] = result
	}
	for _, kind := range []string{"text", "image"} {
		result := byKind[kind]
		if result.Status != "stored" || result.RelativePath == "" || result.SizeBytes <= 0 || result.SHA256 == "" {
			t.Fatalf("%s result = %#v", kind, result)
		}
		if filepath.IsAbs(result.RelativePath) || strings.Contains(result.RelativePath, "..") {
			t.Fatalf("unsafe relative path = %q", result.RelativePath)
		}
		if _, err := os.Stat(filepath.Join(volume, filepath.FromSlash(result.RelativePath))); err != nil {
			t.Fatalf("stat %s: %v", kind, err)
		}
	}
	if result := byKind["image"]; result.Ordinal != 1 {
		t.Fatalf("image result = %#v", result)
	}
	if result := byKind["video"]; result.Status != "disabled" || result.RelativePath != "" {
		t.Fatalf("video result = %#v", result)
	}
	if files, _ := filepath.Glob(filepath.Join(volume, "Download", "fixture123", "v2", "*.mp4")); len(files) != 0 {
		t.Fatalf("video files = %#v", files)
	}
}

func TestPersistVersionResourcesStoresVideoWithoutImages(t *testing.T) {
	mp4 := testISOBaseMedia("isom", "video-only")
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = writer.Write(mp4)
	}))
	defer server.Close()

	volume := filepath.Join(t.TempDir(), "Volume")
	results := persistVersionResources(
		context.Background(),
		server.Client(),
		volume,
		"video123",
		1,
		map[string]any{
			"作品ID": "video123",
			"作品类型": "视频",
			"下载地址": []string{server.URL + "/video"},
			"动图地址": []any{nil},
		},
		mediaPersistencePolicy{Videos: true},
		newDownloadCoordinator(1),
	)
	if len(results) != 2 || results[0].Status != "disabled" || results[1].Kind != "video" || results[1].Status != "stored" {
		t.Fatalf("results = %#v", results)
	}
	if results[1].MIMEType != "video/mp4" || results[1].RelativePath == "" {
		t.Fatalf("video result = %#v", results[1])
	}
}

func TestPersistVersionResourcesStoresVideoCoverAndVideo(t *testing.T) {
	jpeg := testJPEG("video-cover")
	mp4 := testISOBaseMedia("isom", "video-only")
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/cover" {
			writer.Header().Set("Content-Type", "image/jpeg")
			_, _ = writer.Write(jpeg)
			return
		}
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = writer.Write(mp4)
	}))
	defer server.Close()

	volume := filepath.Join(t.TempDir(), "Volume")
	results := persistVersionResources(
		context.Background(),
		server.Client(),
		volume,
		"video123-cover",
		1,
		map[string]any{
			"作品ID": "video123-cover",
			"作品类型": "视频",
			"下载地址": []string{server.URL + "/video"},
			"动图地址": []any{nil},
			"封面地址": server.URL + "/cover",
		},
		mediaPersistencePolicy{Images: true, Videos: true},
		newDownloadCoordinator(2),
	)
	if len(results) != 3 || results[0].Status != "disabled" {
		t.Fatalf("results = %#v", results)
	}
	cover, video := results[1], results[2]
	if cover.Kind != "image" || cover.Ordinal != 0 || cover.Status != "stored" || cover.MIMEType != "image/jpeg" || cover.RelativePath == "" {
		t.Fatalf("cover result = %#v", cover)
	}
	if video.Kind != "video" || video.Ordinal != 1 || video.Status != "stored" || video.MIMEType != "video/mp4" || video.RelativePath == "" {
		t.Fatalf("video result = %#v", video)
	}
	if filepath.Base(cover.RelativePath) != "cover_000.jpeg" {
		t.Fatalf("cover path = %q", cover.RelativePath)
	}
	if filepath.Base(video.RelativePath) != "video_001.mp4" {
		t.Fatalf("video path = %q", video.RelativePath)
	}
}

func TestPersistVersionResourcesRecordsDisabledVideoCover(t *testing.T) {
	mp4 := testISOBaseMedia("isom", "video-only")
	var coverRequested atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/cover" {
			coverRequested.Store(true)
			http.Error(writer, "cover should not be requested", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = writer.Write(mp4)
	}))
	defer server.Close()

	results := persistVersionResources(
		context.Background(),
		server.Client(),
		filepath.Join(t.TempDir(), "Volume"),
		"video-disabled-cover",
		1,
		map[string]any{
			"作品ID": "video-disabled-cover",
			"作品类型": "视频",
			"下载地址": []string{server.URL + "/video"},
			"动图地址": []any{nil},
			"封面地址": server.URL + "/cover",
		},
		mediaPersistencePolicy{Images: false, Videos: true},
		newDownloadCoordinator(1),
	)
	if len(results) != 3 {
		t.Fatalf("results = %#v", results)
	}
	cover, video := results[1], results[2]
	if cover.Kind != "image" || cover.Ordinal != 0 || cover.Status != "disabled" || cover.RelativePath != "" {
		t.Fatalf("cover result = %#v", cover)
	}
	if video.Kind != "video" || video.Ordinal != 1 || video.Status != "stored" {
		t.Fatalf("video result = %#v", video)
	}
	if coverRequested.Load() {
		t.Fatal("disabled cover was downloaded")
	}
}
