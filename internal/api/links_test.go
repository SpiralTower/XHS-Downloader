package api

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestDirectLinkResolution(t *testing.T) {
	client := &http.Client{Timeout: time.Second}
	tests := []struct {
		input   string
		id      string
		wantURL string
	}{
		{
			input:   "分享 https://www.xiaohongshu.com/explore/note123?xsec_token=abc。",
			id:      "note123",
			wantURL: "https://www.xiaohongshu.com/explore/note123?xsec_token=abc",
		},
		{
			input:   "https://www.rednote.com/discovery/item/red456?source=web",
			id:      "red456",
			wantURL: "https://www.rednote.com/discovery/item/red456?source=web",
		},
		{
			input:   "https://www.xiaohongshu.com/user/profile/user123/note789?xsec_token=abc",
			id:      "note789",
			wantURL: "https://www.xiaohongshu.com/user/profile/user123/note789?xsec_token=abc",
		},
		{
			input:   "http://www.xiaohongshu.com/explore/http-note?source=share",
			id:      "http-note",
			wantURL: "https://www.xiaohongshu.com/explore/http-note?source=share",
		},
	}
	for _, test := range tests {
		resolved, err := resolveLink(context.Background(), test.input, client, nil)
		if err != nil {
			t.Fatalf("resolveLink(%q) error = %v", test.input, err)
		}
		if resolved.NoteID != test.id || resolved.URL != test.wantURL {
			t.Fatalf("resolveLink(%q) = %#v, want ID %q URL %q", test.input, resolved, test.id, test.wantURL)
		}
	}
}

func TestIsDetailURLRequiresHTTPSAndSupportedEndpoint(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"https://www.xiaohongshu.com/explore/note", true},
		{"https://rednote.com/discovery/item/note", true},
		{"https://xiaohongshu.com:443/explore/note", true},
		{"http://www.xiaohongshu.com/explore/note", false},
		{"https://www.xiaohongshu.com:8443/explore/note", false},
		{"https://user@www.xiaohongshu.com/explore/note", false},
		{"https://xiaohongshu.com.evil.example/explore/note", false},
		{"https://www.xiaohongshu.com/not-a-note/note", false},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			if got := isDetailURL(test.value); got != test.want {
				t.Fatalf("isDetailURL(%q) = %t, want %t", test.value, got, test.want)
			}
		})
	}
}

func TestRejectsUnsupportedLink(t *testing.T) {
	client := &http.Client{Timeout: time.Second}
	if _, err := resolveLink(context.Background(), "https://example.com/explore/note", client, nil); err == nil {
		t.Fatal("resolveLink() accepted unsupported host")
	}
}

func TestIsShortURLAcceptsComAndCn(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"https://xhslink.com/abc", true},
		{"https://www.xhslink.com/abc", true},
		{"https://xhslink.cn/abc", true},
		{"https://www.xhslink.cn/abc", true},
		{"https://xhslink.com/", false},
		{"http://xhslink.cn/abc", false},
		{"https://xhslink.com.evil.example/abc", false},
		{"https://xhslink.cn:8443/abc", false},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			if got := isShortURL(test.value); got != test.want {
				t.Fatalf("isShortURL(%q) = %t, want %t", test.value, got, test.want)
			}
		})
	}
}

func TestFirstSupportedCandidateRecognizesCnShortLinks(t *testing.T) {
	candidate, short := firstSupportedCandidate("复制打开 https://xhslink.cn/shareCode 看看")
	if !short || candidate != "https://xhslink.cn/shareCode" {
		t.Fatalf("firstSupportedCandidate() = %q short=%t", candidate, short)
	}
	candidate, short = firstSupportedCandidate("www.xhslink.cn/shareCode")
	if !short || candidate != "www.xhslink.cn/shareCode" {
		t.Fatalf("firstSupportedCandidate(www) = %q short=%t", candidate, short)
	}
}
