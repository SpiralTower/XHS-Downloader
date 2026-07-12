package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func testPNG() []byte {
	return append(
		[]byte("\x89PNG\r\n\x1a\n"),
		[]byte("\x00\x00\x00\x00IEND\xae\x42\x60\x82")...,
	)
}

func testWebP(payload string) []byte {
	content := make([]byte, 12+len(payload))
	copy(content[:4], "RIFF")
	binary.LittleEndian.PutUint32(content[4:8], uint32(len(content)-8))
	copy(content[8:12], "WEBP")
	copy(content[12:], payload)
	return content
}

func testGIF(payload string) []byte {
	content := append([]byte("GIF89a"), []byte(payload)...)
	return append(content, 0x3b)
}

func testBMP(payload string) []byte {
	content := make([]byte, 14+len(payload))
	copy(content[:2], "BM")
	binary.LittleEndian.PutUint32(content[2:6], uint32(len(content)))
	copy(content[14:], payload)
	return content
}

func testISOExtendedSize() []byte {
	const ftypSize = 24
	payload := []byte("extended")
	mdatSize := 8 + len(payload)
	content := make([]byte, ftypSize+mdatSize)
	binary.BigEndian.PutUint32(content[:4], 1)
	copy(content[4:8], "ftyp")
	binary.BigEndian.PutUint64(content[8:16], ftypSize)
	copy(content[16:20], "mp42")
	binary.BigEndian.PutUint32(content[ftypSize:ftypSize+4], uint32(mdatSize))
	copy(content[ftypSize+4:ftypSize+8], "mdat")
	copy(content[ftypSize+8:], payload)
	return content
}

func testISOZeroSizedTail() []byte {
	const ftypSize = 16
	payload := []byte("to-eof")
	content := make([]byte, ftypSize+8+len(payload))
	binary.BigEndian.PutUint32(content[:4], ftypSize)
	copy(content[4:8], "ftyp")
	copy(content[8:12], "isom")
	copy(content[ftypSize+4:ftypSize+8], "mdat")
	copy(content[ftypSize+8:], payload)
	return content
}

func TestValidatedMediaExtensionAcceptsStructurallyCompleteFiles(t *testing.T) {
	tests := []struct {
		name      string
		content   []byte
		expected  string
		extension string
	}{
		{"jpeg", testJPEG("complete"), "jpeg", "jpeg"},
		{"png", testPNG(), "jpeg", "png"},
		{"webp", testWebP("complete"), "jpeg", "webp"},
		{"gif", testGIF("complete"), "jpeg", "gif"},
		{"bmp", testBMP("complete"), "jpeg", "bmp"},
		{"mp4", testISOBaseMedia("mp42", "complete"), "mp4", "mp4"},
		{"avif", testISOBaseMedia("avif", "complete"), "jpeg", "avif"},
		{"heic", testISOBaseMedia("heic", "complete"), "jpeg", "heic"},
		{"extended mp4", testISOExtendedSize(), "mp4", "mp4"},
		{"zero-sized final box", testISOZeroSizedTail(), "mp4", "mp4"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "media")
			if err := os.WriteFile(path, test.content, 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := validatedMediaExtension(path, test.expected)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.extension {
				t.Fatalf("extension = %q, want %q", got, test.extension)
			}
		})
	}
}

func TestValidatedMediaExtensionRejectsTruncatedStructures(t *testing.T) {
	webpMismatch := testWebP("payload")
	binary.LittleEndian.PutUint32(webpMismatch[4:8], uint32(len(webpMismatch)+20))
	bmpMismatch := testBMP("payload")
	binary.LittleEndian.PutUint32(bmpMismatch[2:6], uint32(len(bmpMismatch)+20))
	ftypOnly := make([]byte, 16)
	binary.BigEndian.PutUint32(ftypOnly[:4], uint32(len(ftypOnly)))
	copy(ftypOnly[4:8], "ftyp")
	copy(ftypOnly[8:12], "mp42")
	ftypFree := make([]byte, 24)
	binary.BigEndian.PutUint32(ftypFree[:4], 16)
	copy(ftypFree[4:8], "ftyp")
	copy(ftypFree[8:12], "mp42")
	binary.BigEndian.PutUint32(ftypFree[16:20], 8)
	copy(ftypFree[20:24], "free")
	truncatedBox := testISOBaseMedia("mp42", "payload")
	binary.BigEndian.PutUint32(truncatedBox[16:20], uint32(len(truncatedBox)))

	tests := []struct {
		name     string
		content  []byte
		expected string
	}{
		{"jpeg magic only", []byte{0xff, 0xd8, 0xff}, "jpeg"},
		{"jpeg missing eoi", []byte{0xff, 0xd8, 0xff, 0xe0, 0x01}, "jpeg"},
		{"png signature only", []byte("\x89PNG\r\n\x1a\n"), "jpeg"},
		{"png missing iend", append([]byte("\x89PNG\r\n\x1a\n"), []byte("payload")...), "jpeg"},
		{"webp size mismatch", webpMismatch, "jpeg"},
		{"gif missing trailer", []byte("GIF89apayload"), "jpeg"},
		{"bmp size mismatch", bmpMismatch, "jpeg"},
		{"mp4 ftyp 12 bytes", append([]byte{0, 0, 0, 12}, []byte("ftypmp42")...), "mp4"},
		{"mp4 ftyp only", ftypOnly, "mp4"},
		{"mp4 ftyp plus free only", ftypFree, "mp4"},
		{"mp4 truncated box", truncatedBox[:len(truncatedBox)-2], "mp4"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "media")
			if err := os.WriteFile(path, test.content, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := validatedMediaExtension(path, test.expected); !errors.Is(err, errInvalidMedia) {
				t.Fatalf("validation error = %v, want errInvalidMedia", err)
			}
		})
	}
}

func TestExistingTruncatedMediaIsRemovedAndRedownloaded(t *testing.T) {
	root := t.TempDir()
	tempDir := filepath.Join(root, "Temp")
	downloadDir := filepath.Join(root, "Download")
	for _, directory := range []string{tempDir, downloadDir} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	task := downloadTask{
		url:       "https://cdn.example/recover.jpg",
		baseName:  "recover",
		extension: "jpeg",
	}
	target := filepath.Join(downloadDir, task.baseName+".jpeg")
	if err := os.WriteFile(target, []byte{0xff, 0xd8, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	replacement := testJPEG("replacement")
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
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
	if calls != 1 {
		t.Fatalf("RoundTrip calls = %d, want 1", calls)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, replacement) {
		t.Fatalf("downloaded content = %q, want %q", content, replacement)
	}
}
