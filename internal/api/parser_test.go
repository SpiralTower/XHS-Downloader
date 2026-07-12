package api

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseInitialStateAndExtractWork(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("testdata", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	note, err := parseInitialState(string(content), "fixture123")
	if err != nil {
		t.Fatalf("parseInitialState() error = %v", err)
	}
	data, err := extractWork(note, "https://www.xiaohongshu.com/explore/fixture123", "fixture123")
	if err != nil {
		t.Fatalf("extractWork() error = %v", err)
	}
	if got := stringValue(data["作品ID"]); got != "fixture123" {
		t.Fatalf("作品ID = %q", got)
	}
	if got := stringValue(data["作品类型"]); got != "图文" {
		t.Fatalf("作品类型 = %q", got)
	}
	if got := stringValue(data["作品标签"]); got != "测试 Go" {
		t.Fatalf("作品标签 = %q", got)
	}
	urls, ok := data["下载地址"].([]string)
	if !ok || len(urls) != 2 {
		t.Fatalf("下载地址 = %#v", data["下载地址"])
	}
	if want := "https://ci.xiaohongshu.com/fixture-image-1?imageView2/format/jpeg"; urls[0] != want {
		t.Fatalf("first media URL = %q, want %q", urls[0], want)
	}
	lives, ok := data["动图地址"].([]any)
	if !ok || len(lives) != 2 || stringValue(lives[0]) == "" || lives[1] != nil {
		t.Fatalf("动图地址 = %#v", data["动图地址"])
	}
}

func TestParsePhoneInitialState(t *testing.T) {
	html := "<script>window.__INITIAL_STATE__={\"noteData\":{\"data\":{\"noteData\":{\"noteId\":\"phone-note\",\"type\":\"normal\",\"imageList\":[{\"url\":\"https://example.invalid/a/b\"}]}}}}</script>"
	note, err := parseInitialState(html, "phone-note")
	if err != nil {
		t.Fatal(err)
	}
	if got := stringValue(note["noteId"]); got != "phone-note" {
		t.Fatalf("noteId = %q", got)
	}
}

func TestVideoURLSelection(t *testing.T) {
	note := map[string]any{
		"video": map[string]any{
			"media": map[string]any{
				"stream": map[string]any{
					"h264": []any{
						map[string]any{"height": 720, "masterUrl": "https://example.invalid/720.mp4"},
						map[string]any{"height": 1080, "backupUrls": []any{"https://example.invalid/1080.mp4"}},
					},
				},
			},
		},
	}
	urls := videoURLs(note, "resolution")
	if len(urls) != 1 || urls[0] != "https://example.invalid/1080.mp4" {
		t.Fatalf("videoURLs() = %#v", urls)
	}
}

func TestParseInitialStateRequiresRealAssignmentAndExpectedID(t *testing.T) {
	notAssignments := []string{
		"<script>window.__INITIAL_STATE___={\"noteData\":{\"data\":{\"noteData\":{\"noteId\":\"expected\"}}}}</script>",
		"<script>const fake = \"window.__INITIAL_STATE__={\\\"noteData\\\":{\\\"data\\\":{\\\"noteData\\\":{\\\"noteId\\\":\\\"expected\\\"}}}}\";</script>",
	}
	for _, notAssignment := range notAssignments {
		if _, err := parseInitialState(notAssignment, "expected"); !errors.Is(err, ErrNoteDataNotFound) {
			t.Fatalf("parseInitialState() error = %v, want ErrNoteDataNotFound", err)
		}
	}

	mismatched := "<script>window.__INITIAL_STATE__={\"noteData\":{\"data\":{\"noteData\":{\"noteId\":\"other\"}}}}</script>"
	if _, err := parseInitialState(mismatched, "expected"); !errors.Is(err, ErrNoteDataNotFound) {
		t.Fatalf("parseInitialState() mismatch error = %v, want ErrNoteDataNotFound", err)
	}

	multiple := "<script>window.__INITIAL_STATE__={\"noteData\":{\"data\":{\"noteData\":{\"noteId\":\"expected\"}}}}</script>" +
		"<script>window.__INITIAL_STATE__={\"noteData\":{\"data\":{\"noteData\":{\"noteId\":\"other\"}}}}</script>"
	note, err := parseInitialState(multiple, "expected")
	if err != nil {
		t.Fatal(err)
	}
	if got := stringValue(note["noteId"]); got != "expected" {
		t.Fatalf("noteId = %q", got)
	}
}

func TestImageTokenUsesOnlyRealCDNTokenSegment(t *testing.T) {
	tests := map[string]string{
		"https://sns-webpic-qc.xhscdn.com/202603010101/0a1b2c3d4e5f/fixture-token!nd_dft_wlteh_webp_3": "fixture-token",
		"https://sns-webpic-qc.xhscdn.com/notes_pre_post/fixture-token!nd_dft_wlteh_webp_3":            "",
		"https://sns-webpic-qc.xhscdn.com/202603010101/not-a-hash!/fixture-token":                      "",
	}
	for raw, want := range tests {
		if got := imageToken(raw); got != want {
			t.Errorf("imageToken(%q) = %q, want %q", raw, got, want)
		}
	}
}
