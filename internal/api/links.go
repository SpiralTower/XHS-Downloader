package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var (
	ErrUnsupportedLink = errors.New("unsupported Xiaohongshu link")

	detailLinkPattern = regexp.MustCompile(`(?i)(https?://)?(www\.)?(xiaohongshu\.com|rednote\.com)/(explore/[^[:space:]]+|discovery/item/[^[:space:]]+|user/profile/[a-z0-9]+/[^[:space:]]+)`)
	shortLinkPattern  = regexp.MustCompile(`(?i)(https?://)?(www\.)?xhslink\.(?:com|cn)/[^[:space:]]+`)
)

type resolvedLink struct {
	URL    string
	NoteID string
}

func resolveLink(ctx context.Context, raw string, client *http.Client, headers http.Header) (resolvedLink, error) {
	candidate, short := firstSupportedCandidate(raw)
	if candidate == "" {
		return resolvedLink{}, ErrUnsupportedLink
	}
	candidate = ensureHTTPS(candidate)

	if short {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, candidate, nil)
		if err != nil {
			return resolvedLink{}, ErrUnsupportedLink
		}
		copyHeadersForURL(request.Header, headers, request.URL)
		response, err := withStrictLinkRedirects(client).Do(request)
		if err != nil {
			return resolvedLink{}, fmt.Errorf("resolve short link: %w", err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		response.Body.Close()
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusBadRequest {
			return resolvedLink{}, fmt.Errorf("resolve short link: upstream returned %s", response.Status)
		}
		if response.Request == nil || response.Request.URL == nil {
			return resolvedLink{}, ErrUnsupportedLink
		}
		candidate = response.Request.URL.String()
	}
	if !isDetailURL(candidate) {
		return resolvedLink{}, ErrUnsupportedLink
	}

	id, err := noteIDFromURL(candidate)
	if err != nil {
		return resolvedLink{}, err
	}
	return resolvedLink{URL: candidate, NoteID: id}, nil
}

func firstSupportedCandidate(raw string) (string, bool) {
	for _, field := range strings.Fields(raw) {
		if match := shortLinkPattern.FindString(field); match != "" {
			return trimLinkPunctuation(match), true
		}
		if match := detailLinkPattern.FindString(field); match != "" {
			return trimLinkPunctuation(match), false
		}
	}
	return "", false
}

func trimLinkPunctuation(value string) string {
	return strings.TrimRight(value, "\"'<>\\^`{|}，。；！？、【】《》)]")
}

func ensureHTTPS(value string) string {
	if len(value) >= len("http://") && strings.EqualFold(value[:len("http://")], "http://") {
		return "https://" + value[len("http://"):]
	}
	if len(value) >= len("https://") && strings.EqualFold(value[:len("https://")], "https://") {
		return "https://" + value[len("https://"):]
	}
	return "https://" + value
}

func isDetailURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" || parsed.User != nil {
		return false
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "xiaohongshu.com" && host != "www.xiaohongshu.com" && host != "rednote.com" && host != "www.rednote.com" {
		return false
	}
	_, err = noteIDFromURL(value)
	return err == nil
}

func isShortURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" || parsed.User != nil {
		return false
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return (host == "xhslink.com" || host == "www.xhslink.com" || host == "xhslink.cn" || host == "www.xhslink.cn") && parsed.EscapedPath() != "" && parsed.EscapedPath() != "/"
}

func isSupportedLinkURL(value *url.URL) bool {
	if value == nil {
		return false
	}
	return isShortURL(value.String()) || isDetailURL(value.String())
}

func noteIDFromURL(value string) (string, error) {
	parsed, err := url.Parse(ensureHTTPS(value))
	if err != nil {
		return "", ErrUnsupportedLink
	}
	parts := strings.FieldsFunc(parsed.EscapedPath(), func(r rune) bool { return r == '/' })
	if len(parts) == 2 && parts[0] == "explore" {
		return unescapeID(parts[1])
	}
	if len(parts) == 3 && parts[0] == "discovery" && parts[1] == "item" {
		return unescapeID(parts[2])
	}
	if len(parts) >= 4 && parts[0] == "user" && parts[1] == "profile" {
		return unescapeID(parts[len(parts)-1])
	}
	return "", ErrUnsupportedLink
}

func unescapeID(value string) (string, error) {
	id, err := url.PathUnescape(value)
	if err != nil || strings.TrimSpace(id) == "" || strings.ContainsAny(id, "/?#") {
		return "", ErrUnsupportedLink
	}
	return id, nil
}

func copyHeaders(destination, source http.Header) {
	for key, values := range source {
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}
