package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
