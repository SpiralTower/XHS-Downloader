package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestChunkedShort206KeepsValidatedPartAndContinues(t *testing.T) {
	root := t.TempDir()
	tempDir := filepath.Join(root, "Temp")
	downloadDir := filepath.Join(root, "Download")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	task := downloadTask{url: "https://cdn.example/chunked-short.jpg", baseName: "chunked-short", extension: "jpeg"}
	content := testJPEG("chunked-short-response")
	initialOffset := 6
	partial, metadataPath := preparePartialState(t, tempDir, task, content[:initialOffset], partialMetadata{
		URL:  task.url,
		ETag: "\"stable\"",
	})
	firstRemainderLength := 5
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		offset := initialOffset
		if calls == 2 {
			info, err := os.Stat(partial)
			if err != nil {
				t.Fatal(err)
			}
			offset = int(info.Size())
		}
		if got := request.Header.Get("Range"); got != fmt.Sprintf("bytes=%d-", offset) {
			t.Fatalf("Range = %q, want offset %d", got, offset)
		}
		headers := make(http.Header)
		headers.Set("ETag", "\"stable\"")
		headers.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(content)-1, len(content)))
		remainder := content[offset:]
		if calls == 1 {
			remainder = remainder[:firstRemainderLength]
		}
		return testDownloadResponse(
			request,
			http.StatusPartialContent,
			headers,
			io.NopCloser(bytes.NewReader(remainder)),
			-1,
		), nil
	})}

	err := downloadFile(context.Background(), client, mediaHeaders(), tempDir, downloadDir, task, 0, true)
	if err == nil {
		t.Fatal("short chunked 206 was finalized")
	}
	if _, statErr := os.Stat(metadataPath); statErr != nil {
		t.Fatalf("resume metadata was not retained: %v", statErr)
	}
	info, statErr := os.Stat(partial)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if want := int64(initialOffset + firstRemainderLength); info.Size() != want {
		t.Fatalf("partial size = %d, want %d", info.Size(), want)
	}
	if files, _ := filepath.Glob(filepath.Join(downloadDir, "*")); len(files) != 0 {
		t.Fatalf("short response finalized files = %#v", files)
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

func TestChunkedLong206IsClearedAndNeverFinalized(t *testing.T) {
	root := t.TempDir()
	tempDir := filepath.Join(root, "Temp")
	downloadDir := filepath.Join(root, "Download")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	task := downloadTask{url: "https://cdn.example/chunked-long.jpg", baseName: "chunked-long", extension: "jpeg"}
	content := testJPEG("chunked-long-response")
	offset := 6
	partial, metadataPath := preparePartialState(t, tempDir, task, content[:offset], partialMetadata{
		URL:  task.url,
		ETag: "\"stable\"",
	})
	body := append(append([]byte(nil), content[offset:]...), []byte("unexpected-extra")...)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		headers := make(http.Header)
		headers.Set("ETag", "\"stable\"")
		headers.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(content)-1, len(content)))
		return testDownloadResponse(
			request,
			http.StatusPartialContent,
			headers,
			io.NopCloser(bytes.NewReader(body)),
			-1,
		), nil
	})}

	if err := downloadFile(context.Background(), client, mediaHeaders(), tempDir, downloadDir, task, 0, true); err == nil {
		t.Fatal("long chunked 206 was accepted")
	}
	for _, path := range []string{partial, metadataPath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists, err = %v", path, err)
		}
	}
	if files, _ := filepath.Glob(filepath.Join(downloadDir, "*")); len(files) != 0 {
		t.Fatalf("long response finalized files = %#v", files)
	}
}

func TestFullResponseContentLengthMismatchNeverFinalizes(t *testing.T) {
	content := testJPEG("declared-length")
	tests := []struct {
		name          string
		declared      int64
		body          []byte
		keepPartial   bool
		withValidator bool
	}{
		{
			name:          "short stable response",
			declared:      int64(len(content)),
			body:          content[:len(content)-3],
			keepPartial:   true,
			withValidator: true,
		},
		{
			name:     "short unvalidated response",
			declared: int64(len(content)),
			body:     content[:len(content)-3],
		},
		{
			name:          "long response",
			declared:      int64(len(content) - 2),
			body:          content,
			withValidator: true,
		},
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
				url:       "https://cdn.example/content-length.jpg",
				baseName:  "content-length",
				extension: "jpeg",
			}
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				headers := make(http.Header)
				if test.withValidator {
					headers.Set("ETag", "\"stable\"")
				}
				return testDownloadResponse(
					request,
					http.StatusOK,
					headers,
					io.NopCloser(bytes.NewReader(test.body)),
					test.declared,
				), nil
			})}
			if err := downloadFile(context.Background(), client, mediaHeaders(), tempDir, downloadDir, task, 0, true); err == nil {
				t.Fatal("Content-Length mismatch was accepted")
			}
			partial := filepath.Join(tempDir, task.baseName+"."+task.extension+".part")
			_, statErr := os.Stat(partial)
			if test.keepPartial {
				if statErr != nil {
					t.Fatalf("short validated part was not retained: %v", statErr)
				}
			} else if !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("untrusted mismatched part still exists, err = %v", statErr)
			}
			if files, _ := filepath.Glob(filepath.Join(downloadDir, "*")); len(files) != 0 {
				t.Fatalf("mismatched response finalized files = %#v", files)
			}
		})
	}
}
