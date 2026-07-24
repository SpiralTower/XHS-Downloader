package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestShortLinkResolutionUsesFinalSupportedURL(t *testing.T) {
	cookie := "a=secret"
	var observedCookie string
	finalURL, err := url.Parse("https://www.xiaohongshu.com/explore/short-note?xsec_token=fixture")
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: securityRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		observedCookie = request.Header.Get("Cookie")
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    &http.Request{Method: http.MethodGet, URL: finalURL, Header: request.Header},
		}, nil
	})}
	resolved, err := resolveLink(context.Background(), "复制 https://xhslink.com/fixture", client, requestHeaders(&cookie))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.NoteID != "short-note" || resolved.URL != finalURL.String() {
		t.Fatalf("resolved = %#v", resolved)
	}
	if observedCookie != "" {
		t.Fatalf("short-link request leaked Cookie %q", observedCookie)
	}
}

func TestShortLinkResolutionAllowsOnlySupportedHTTPSRedirects(t *testing.T) {
	cookie := "a=secret"
	var observedURLs []string
	var observedCookies []string
	client := &http.Client{Transport: securityRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		observedURLs = append(observedURLs, request.URL.String())
		observedCookies = append(observedCookies, request.Header.Get("Cookie"))
		switch request.URL.String() {
		case "https://xhslink.com/fixture":
			return testRedirectResponse(request, "https://www.xhslink.com/next"), nil
		case "https://www.xhslink.com/next":
			return testRedirectResponse(request, "https://www.xiaohongshu.com/explore/final-note"), nil
		case "https://www.xiaohongshu.com/explore/final-note":
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("ok")),
				Request:    request,
			}, nil
		default:
			return nil, fmt.Errorf("unexpected URL %s", request.URL)
		}
	})}

	resolved, err := resolveLink(t.Context(), "复制 http://xhslink.com/fixture", client, requestHeaders(&cookie))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.URL != "https://www.xiaohongshu.com/explore/final-note" || resolved.NoteID != "final-note" {
		t.Fatalf("resolved = %#v", resolved)
	}
	wantURLs := []string{
		"https://xhslink.com/fixture",
		"https://www.xhslink.com/next",
		"https://www.xiaohongshu.com/explore/final-note",
	}
	if strings.Join(observedURLs, "\n") != strings.Join(wantURLs, "\n") {
		t.Fatalf("observed URLs = %#v, want %#v", observedURLs, wantURLs)
	}
	for index, observedCookie := range observedCookies {
		if observedCookie != "" {
			t.Fatalf("redirect hop %d leaked Cookie %q", index, observedCookie)
		}
	}
}

func TestShortLinkResolutionRejectsUnsafeRedirectHop(t *testing.T) {
	targets := []string{
		"http://www.xiaohongshu.com/explore/downgraded",
		"https://example.com/explore/external",
		"https://www.xiaohongshu.com:8443/explore/wrong-port",
	}
	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: securityRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls > 1 {
					t.Fatalf("unsafe redirect target was requested: %s", request.URL)
				}
				return testRedirectResponse(request, target), nil
			})}
			if _, err := resolveLink(t.Context(), "https://xhslink.com/fixture", client, nil); err == nil {
				t.Fatal("unsafe redirect was accepted")
			}
			if calls != 1 {
				t.Fatalf("RoundTrip calls = %d, want 1", calls)
			}
		})
	}
}

func TestDetailRedirectCannotLeaveSupportedDetailHosts(t *testing.T) {
	cookie := "a=secret"
	calls := 0
	client := &http.Client{Transport: securityRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls > 1 {
			t.Fatalf("unsafe detail redirect target was requested: %s", request.URL)
		}
		if request.Header.Get("Cookie") != cookie {
			t.Fatalf("initial detail Cookie = %q", request.Header.Get("Cookie"))
		}
		return testRedirectResponse(request, "https://xhslink.com/next"), nil
	})}
	request, err := http.NewRequest(http.MethodGet, "https://www.xiaohongshu.com/explore/origin", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Cookie", cookie)
	if _, err := withStrictLinkRedirects(client).Do(request); err == nil {
		t.Fatal("detail redirect outside supported detail hosts was accepted")
	}
	if calls != 1 {
		t.Fatalf("RoundTrip calls = %d, want 1", calls)
	}
}

func testRedirectResponse(request *http.Request, location string) *http.Response {
	headers := make(http.Header)
	headers.Set("Location", location)
	return &http.Response{
		StatusCode: http.StatusFound,
		Status:     "302 Found",
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader("redirect")),
		Request:    request,
	}
}

func TestCnShortLinkResolutionFollowsRedirectChain(t *testing.T) {
	cookie := "a=secret"
	observedURLs := make([]string, 0, 3)
	client := &http.Client{Transport: securityRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		observedURLs = append(observedURLs, request.URL.String())
		if request.Header.Get("Cookie") != "" {
			t.Fatalf("short-link hop leaked Cookie for %s", request.URL)
		}
		switch request.URL.String() {
		case "https://xhslink.cn/fixture":
			return testRedirectResponse(request, "https://www.xhslink.cn/next"), nil
		case "https://www.xhslink.cn/next":
			return testRedirectResponse(request, "https://www.xiaohongshu.com/explore/final-cn-note"), nil
		case "https://www.xiaohongshu.com/explore/final-cn-note":
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("ok")),
				Request:    request,
			}, nil
		default:
			return nil, fmt.Errorf("unexpected URL %s", request.URL)
		}
	})}

	resolved, err := resolveLink(t.Context(), "复制 https://xhslink.cn/fixture", client, requestHeaders(&cookie))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.URL != "https://www.xiaohongshu.com/explore/final-cn-note" || resolved.NoteID != "final-cn-note" {
		t.Fatalf("resolved = %#v", resolved)
	}
	wantURLs := []string{
		"https://xhslink.cn/fixture",
		"https://www.xhslink.cn/next",
		"https://www.xiaohongshu.com/explore/final-cn-note",
	}
	if strings.Join(observedURLs, "\n") != strings.Join(wantURLs, "\n") {
		t.Fatalf("observed URLs = %#v, want %#v", observedURLs, wantURLs)
	}
}
