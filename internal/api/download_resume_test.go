package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testJPEG(payload string) []byte {
	content := append([]byte{0xff, 0xd8, 0xff, 0xe0}, []byte(payload)...)
	return append(content, 0xff, 0xd9)
}

func testISOBaseMedia(brand, payload string) []byte {
	const ftypSize = 16
	mdatSize := 8 + len(payload)
	content := make([]byte, ftypSize+mdatSize)
	binary.BigEndian.PutUint32(content[:4], ftypSize)
	copy(content[4:8], "ftyp")
	copy(content[8:12], brand)
	binary.BigEndian.PutUint32(content[ftypSize:ftypSize+4], uint32(mdatSize))
	copy(content[ftypSize+4:ftypSize+8], "mdat")
	copy(content[ftypSize+8:], payload)
	return content
}

func testDownloadResponse(request *http.Request, status int, headers http.Header, body io.ReadCloser, length int64) *http.Response {
	if headers == nil {
		headers = make(http.Header)
	}
	return &http.Response{
		StatusCode:    status,
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:        headers,
		Body:          body,
		ContentLength: length,
		Request:       request,
	}
}

func preparePartialState(t *testing.T, tempDir string, task downloadTask, prefix []byte, metadata partialMetadata) (string, string) {
	t.Helper()
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(tempDir, task.baseName+"."+task.extension+".part")
	metadataPath := partial + ".json"
	if err := os.WriteFile(partial, prefix, 0o644); err != nil {
		t.Fatal(err)
	}
	if metadata.validator() != "" {
		if err := persistPartialMetadata(metadataPath, metadata); err != nil {
			t.Fatal(err)
		}
	}
	return partial, metadataPath
}

func TestDownloadResumesOnlyMatchingValidatedRange(t *testing.T) {
	root := t.TempDir()
	tempDir := filepath.Join(root, "Temp")
	downloadDir := filepath.Join(root, "Download")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	task := downloadTask{url: "https://cdn.example/media.jpg", baseName: "resume", extension: "jpeg"}
	content := testJPEG("validated-resume")
	offset := 7
	partial, metadataPath := preparePartialState(t, tempDir, task, content[:offset], partialMetadata{
		URL:  task.url,
		ETag: "\"version-1\"",
	})

	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if got := request.Header.Get("Range"); got != fmt.Sprintf("bytes=%d-", offset) {
			t.Fatalf("Range = %q", got)
		}
		if got := request.Header.Get("If-Range"); got != "\"version-1\"" {
			t.Fatalf("If-Range = %q", got)
		}
		headers := make(http.Header)
		headers.Set("ETag", "\"version-1\"")
		headers.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(content)-1, len(content)))
		remainder := content[offset:]
		return testDownloadResponse(
			request,
			http.StatusPartialContent,
			headers,
			io.NopCloser(bytes.NewReader(remainder)),
			int64(len(remainder)),
		), nil
	})}

	if err := downloadFile(context.Background(), client, mediaHeaders(), tempDir, downloadDir, task, 0, true); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("RoundTrip calls = %d, want 1", calls)
	}
	downloaded, err := os.ReadFile(filepath.Join(downloadDir, task.baseName+".jpeg"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(downloaded, content) {
		t.Fatalf("downloaded content = %q, want %q", downloaded, content)
	}
	for _, path := range []string{partial, metadataPath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists, err = %v", path, err)
		}
	}
}

func TestDownloadRetainsValidatedInterruptedPartAndResumes(t *testing.T) {
	root := t.TempDir()
	tempDir := filepath.Join(root, "Temp")
	downloadDir := filepath.Join(root, "Download")
	for _, directory := range []string{tempDir, downloadDir} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	task := downloadTask{url: "https://cdn.example/interrupted.jpg", baseName: "interrupted", extension: "jpeg"}
	content := testJPEG("resume-after-interruption")
	prefixLength := 9
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		headers := make(http.Header)
		headers.Set("ETag", "\"stable\"")
		if calls == 1 {
			return testDownloadResponse(
				request,
				http.StatusOK,
				headers,
				&failingReadCloser{content: append([]byte(nil), content[:prefixLength]...)},
				int64(len(content)),
			), nil
		}
		if got := request.Header.Get("If-Range"); got != "\"stable\"" {
			t.Fatalf("If-Range = %q", got)
		}
		headers.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", prefixLength, len(content)-1, len(content)))
		remainder := content[prefixLength:]
		return testDownloadResponse(
			request,
			http.StatusPartialContent,
			headers,
			io.NopCloser(bytes.NewReader(remainder)),
			int64(len(remainder)),
		), nil
	})}

	if err := downloadFile(context.Background(), client, mediaHeaders(), tempDir, downloadDir, task, 0, true); err == nil {
		t.Fatal("interrupted response was accepted")
	}
	partial := filepath.Join(tempDir, task.baseName+"."+task.extension+".part")
	if info, err := os.Stat(partial); err != nil || info.Size() != int64(prefixLength) {
		t.Fatalf("partial size = %v, err = %v", info, err)
	}
	if _, err := os.Stat(partial + ".json"); err != nil {
		t.Fatalf("resume sidecar missing: %v", err)
	}

	if err := downloadFile(context.Background(), client, mediaHeaders(), tempDir, downloadDir, task, 0, true); err != nil {
		t.Fatal(err)
	}
	downloaded, err := os.ReadFile(filepath.Join(downloadDir, task.baseName+".jpeg"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(downloaded, content) {
		t.Fatalf("downloaded content = %q, want %q", downloaded, content)
	}
}

type failingReadCloser struct {
	content []byte
	failed  bool
}

func (r *failingReadCloser) Read(buffer []byte) (int, error) {
	if len(r.content) > 0 {
		length := copy(buffer, r.content)
		r.content = r.content[length:]
		return length, nil
	}
	if !r.failed {
		r.failed = true
		return 0, errors.New("connection interrupted")
	}
	return 0, io.EOF
}

func (r *failingReadCloser) Close() error { return nil }

func TestUnsafeResumeResponsesRestartFromBeginning(t *testing.T) {
	content := testJPEG("replacement-resource")
	tests := []struct {
		name          string
		firstStatus   int
		contentRange  string
		responseETag  string
		firstBodyData []byte
	}{
		{
			name:          "wrong content range",
			firstStatus:   http.StatusPartialContent,
			contentRange:  fmt.Sprintf("bytes 1-%d/%d", len(content)-1, len(content)),
			responseETag:  "\"old\"",
			firstBodyData: content[1:],
		},
		{
			name:          "missing response validator",
			firstStatus:   http.StatusPartialContent,
			contentRange:  fmt.Sprintf("bytes 6-%d/%d", len(content)-1, len(content)),
			firstBodyData: content[6:],
		},
		{
			name:        "range not satisfiable",
			firstStatus: http.StatusRequestedRangeNotSatisfiable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			tempDir := filepath.Join(root, "Temp")
			downloadDir := filepath.Join(root, "Download")
			if err := os.MkdirAll(downloadDir, 0o755); err != nil {
				t.Fatal(err)
			}
			task := downloadTask{url: "https://cdn.example/restart.jpg", baseName: "restart", extension: "jpeg"}
			prefix := testJPEG("x")[:6]
			preparePartialState(t, tempDir, task, prefix, partialMetadata{URL: task.url, ETag: "\"old\""})

			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					if request.Header.Get("Range") == "" || request.Header.Get("If-Range") != "\"old\"" {
						t.Fatalf("first resume headers = %#v", request.Header)
					}
					headers := make(http.Header)
					if test.contentRange != "" {
						headers.Set("Content-Range", test.contentRange)
					}
					if test.responseETag != "" {
						headers.Set("ETag", test.responseETag)
					}
					return testDownloadResponse(
						request,
						test.firstStatus,
						headers,
						io.NopCloser(bytes.NewReader(test.firstBodyData)),
						int64(len(test.firstBodyData)),
					), nil
				}
				if request.Header.Get("Range") != "" || request.Header.Get("If-Range") != "" {
					t.Fatalf("retry leaked range headers: %#v", request.Header)
				}
				headers := make(http.Header)
				headers.Set("ETag", "\"new\"")
				return testDownloadResponse(
					request,
					http.StatusOK,
					headers,
					io.NopCloser(bytes.NewReader(content)),
					int64(len(content)),
				), nil
			})}

			if err := downloadFile(context.Background(), client, mediaHeaders(), tempDir, downloadDir, task, 0, true); err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatalf("RoundTrip calls = %d, want 2", calls)
			}
			downloaded, err := os.ReadFile(filepath.Join(downloadDir, task.baseName+".jpeg"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(downloaded, content) {
				t.Fatalf("downloaded content = %q, want clean replacement %q", downloaded, content)
			}
		})
	}
}

func TestResumeReceivingFullResponseTruncatesOldPart(t *testing.T) {
	root := t.TempDir()
	tempDir := filepath.Join(root, "Temp")
	downloadDir := filepath.Join(root, "Download")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	task := downloadTask{url: "https://cdn.example/changed.jpg", baseName: "changed", extension: "jpeg"}
	preparePartialState(t, tempDir, task, testJPEG("old-part"), partialMetadata{URL: task.url, ETag: "\"old\""})
	replacement := testJPEG("new-resource")

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Range") == "" {
			t.Fatal("resume request did not include Range")
		}
		headers := make(http.Header)
		headers.Set("ETag", "\"new\"")
		return testDownloadResponse(
			request,
			http.StatusOK,
			headers,
			io.NopCloser(bytes.NewReader(replacement)),
			int64(len(replacement)),
		), nil
	})}
	if err := downloadFile(context.Background(), client, mediaHeaders(), tempDir, downloadDir, task, 0, true); err != nil {
		t.Fatal(err)
	}
	downloaded, err := os.ReadFile(filepath.Join(downloadDir, task.baseName+".jpeg"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(downloaded, replacement) {
		t.Fatalf("downloaded content = %q, want %q", downloaded, replacement)
	}
}

func TestLegacyPartWithoutValidatorRestartsWithoutRange(t *testing.T) {
	root := t.TempDir()
	tempDir := filepath.Join(root, "Temp")
	downloadDir := filepath.Join(root, "Download")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	task := downloadTask{url: "https://cdn.example/legacy.jpg", baseName: "legacy", extension: "jpeg"}
	preparePartialState(t, tempDir, task, testJPEG("legacy-part"), partialMetadata{})
	replacement := testJPEG("clean-download")

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Range") != "" || request.Header.Get("If-Range") != "" {
			t.Fatalf("legacy part was resumed: %#v", request.Header)
		}
		return testDownloadResponse(
			request,
			http.StatusOK,
			nil,
			io.NopCloser(bytes.NewReader(replacement)),
			int64(len(replacement)),
		), nil
	})}
	if err := downloadFile(context.Background(), client, mediaHeaders(), tempDir, downloadDir, task, 0, true); err != nil {
		t.Fatal(err)
	}
	downloaded, err := os.ReadFile(filepath.Join(downloadDir, task.baseName+".jpeg"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(downloaded, replacement) {
		t.Fatalf("downloaded content = %q, want %q", downloaded, replacement)
	}
}

func TestDownloadRejectsInvalidOrMismatchedCompletedResponses(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		expected  string
		content   []byte
		contentTy string
	}{
		{"no content", http.StatusNoContent, "jpeg", nil, ""},
		{"empty", http.StatusOK, "jpeg", nil, "image/jpeg"},
		{"html", http.StatusOK, "jpeg", []byte("<html>upstream error</html>"), "text/html"},
		{"unknown", http.StatusOK, "jpeg", []byte("not-media"), "application/octet-stream"},
		{"video as image", http.StatusOK, "jpeg", testISOBaseMedia("mp42", "video"), "video/mp4"},
		{"image as video", http.StatusOK, "mp4", testJPEG("image"), "image/jpeg"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			tempDir := filepath.Join(root, "Temp")
			downloadDir := filepath.Join(root, "Download")
			for _, directory := range []string{tempDir, downloadDir} {
				if err := os.MkdirAll(directory, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			task := downloadTask{
				url:       "https://cdn.example/invalid",
				baseName:  strings.ReplaceAll(test.name, " ", "-"),
				extension: test.expected,
			}
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				headers := make(http.Header)
				if test.contentTy != "" {
					headers.Set("Content-Type", test.contentTy)
				}
				headers.Set("ETag", "\"invalid\"")
				return testDownloadResponse(
					request,
					test.status,
					headers,
					io.NopCloser(bytes.NewReader(test.content)),
					int64(len(test.content)),
				), nil
			})}
			if err := downloadFile(context.Background(), client, mediaHeaders(), tempDir, downloadDir, task, 0, true); err == nil {
				t.Fatal("invalid completed response was accepted")
			}
			if files, _ := filepath.Glob(filepath.Join(downloadDir, "*")); len(files) != 0 {
				t.Fatalf("invalid completed files = %#v", files)
			}
			if files, _ := filepath.Glob(filepath.Join(tempDir, "*")); len(files) != 0 {
				t.Fatalf("invalid temporary files = %#v", files)
			}
		})
	}
}

func TestDetectedMediaExtensionRecognizesCommonBrands(t *testing.T) {
	tests := map[string]string{
		"avif": "avif",
		"heic": "heic",
		"mif1": "heic",
		"isom": "mp4",
		"iso6": "mp4",
		"mp41": "mp4",
		"mp42": "mp4",
		"avc1": "mp4",
		"MSNV": "mp4",
		"qt  ": "mp4",
	}
	for brand, want := range tests {
		if got := detectedMediaExtension(testISOBaseMedia(brand, "payload")); got != want {
			t.Errorf("brand %q detected as %q, want %q", brand, got, want)
		}
	}
}
