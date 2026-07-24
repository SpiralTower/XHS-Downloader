package api

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type securityRoundTripFunc func(*http.Request) (*http.Response, error)

func (function securityRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestHTTPClientPoolReusesDefaultAndDoesNotCacheProxies(t *testing.T) {
	const responseHeaderTimeout = 3 * time.Second
	pool := newHTTPClientPool(responseHeaderTimeout, true)
	first, err := pool.Client(nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := pool.Client(nil)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("default HTTP client was not reused")
	}
	if first.Timeout != 0 {
		t.Fatalf("default HTTP client timeout = %s, want no total response timeout", first.Timeout)
	}
	transport, ok := first.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("default transport type = %T", first.Transport)
	}
	if transport.ResponseHeaderTimeout != responseHeaderTimeout {
		t.Fatalf("ResponseHeaderTimeout = %s, want %s", transport.ResponseHeaderTimeout, responseHeaderTimeout)
	}
	if transport.Proxy != nil {
		t.Fatal("default direct transport unexpectedly uses an environment proxy")
	}

	proxy := "http://127.0.0.1:8080"
	proxiedFirst, err := pool.Client(&proxy)
	if err != nil {
		t.Fatal(err)
	}
	proxiedSecond, err := pool.Client(&proxy)
	if err != nil {
		t.Fatal(err)
	}
	if proxiedFirst == proxiedSecond {
		t.Fatal("per-request proxy HTTP clients were unexpectedly cached")
	}
	proxiedFirst.CloseIdleConnections()
	proxiedSecond.CloseIdleConnections()

	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Client(nil); err == nil {
		t.Fatal("closed HTTP client pool accepted a new client")
	}
}

func TestProxyRejectsPrivateAddressUnlessExplicitlyAllowed(t *testing.T) {
	proxy := "http://127.0.0.1:8080"
	restricted := newHTTPClientPool(time.Second, false)
	defer restricted.Close()
	if _, err := restricted.Client(&proxy); err == nil {
		t.Fatal("private proxy was accepted without opt-in")
	}

	allowed := newHTTPClientPool(time.Second, true)
	defer allowed.Close()
	client, err := allowed.Client(&proxy)
	if err != nil {
		t.Fatalf("private proxy opt-in was rejected: %v", err)
	}
	client.CloseIdleConnections()
}

func TestValidatePublicProxyHostRejectsMixedDNSResult(t *testing.T) {
	proxyURL, err := url.Parse("http://proxy.example:8080")
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{
			net.ParseIP("93.184.216.34"),
			net.ParseIP("10.0.0.7"),
		}, nil
	}
	if err := validatePublicProxyHost(t.Context(), proxyURL, lookup); err == nil {
		t.Fatal("proxy host with a private DNS result was accepted")
	}
}

func TestValidatePublicProxyHostAcceptsPublicDNSResults(t *testing.T) {
	proxyURL, err := url.Parse("https://proxy.example:8443")
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{
			net.ParseIP("93.184.216.34"),
			net.ParseIP("2606:4700:4700::1111"),
		}, nil
	}
	if err := validatePublicProxyHost(t.Context(), proxyURL, lookup); err != nil {
		t.Fatalf("public proxy DNS results were rejected: %v", err)
	}
}

func TestPublicIPClassification(t *testing.T) {
	tests := []struct {
		address string
		public  bool
	}{
		{"8.8.8.8", true},
		{"93.184.216.34", true},
		{"2606:4700:4700::1111", true},
		{"0.0.0.0", false},
		{"10.1.2.3", false},
		{"100.64.0.1", false},
		{"127.0.0.1", false},
		{"169.254.1.2", false},
		{"172.16.0.1", false},
		{"192.168.1.1", false},
		{"192.0.2.1", false},
		{"198.18.0.1", false},
		{"198.51.100.1", false},
		{"203.0.113.1", false},
		{"224.0.0.1", false},
		{"255.255.255.255", false},
		{"::", false},
		{"::1", false},
		{"::ffff:127.0.0.1", false},
		{"64:ff9b::7f00:1", false},
		{"2001:2::1", false},
		{"2001:db8::1", false},
		{"3fff::1", false},
		{"5f00::1", false},
		{"fc00::1", false},
		{"fe80::1", false},
		{"fec0::1", false},
		{"ff02::1", false},
	}
	for _, test := range tests {
		t.Run(test.address, func(t *testing.T) {
			if got := isPublicIP(net.ParseIP(test.address)); got != test.public {
				t.Fatalf("isPublicIP(%s) = %t, want %t", test.address, got, test.public)
			}
		})
	}
}

func TestPublicDialContextPinsResolvedPublicIP(t *testing.T) {
	sentinel := errors.New("dial stopped")
	lookups := 0
	var dialed []string
	lookup := func(_ context.Context, network, host string) ([]net.IP, error) {
		lookups++
		if network != "ip" || host != "cdn.example" {
			t.Fatalf("lookup = (%q, %q)", network, host)
		}
		return []net.IP{
			net.ParseIP("10.0.0.9"),
			net.ParseIP("93.184.216.34"),
		}, nil
	}
	dial := func(_ context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" {
			t.Fatalf("dial network = %q", network)
		}
		dialed = append(dialed, address)
		return nil, sentinel
	}

	_, err := newPublicDialContext(lookup, dial)(t.Context(), "tcp", "cdn.example:443")
	if !errors.Is(err, sentinel) {
		t.Fatalf("dial error = %v, want sentinel", err)
	}
	if lookups != 1 {
		t.Fatalf("DNS lookup count = %d, want 1", lookups)
	}
	if len(dialed) != 1 || dialed[0] != "93.184.216.34:443" {
		t.Fatalf("dialed addresses = %#v, want pinned public IP", dialed)
	}
}

func TestPublicDialContextRejectsNonPublicDNSResults(t *testing.T) {
	dialCalled := false
	lookup := func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{
			net.ParseIP("127.0.0.1"),
			net.ParseIP("192.168.1.2"),
		}, nil
	}
	dial := func(context.Context, string, string) (net.Conn, error) {
		dialCalled = true
		return nil, nil
	}

	_, err := newPublicDialContext(lookup, dial)(t.Context(), "tcp", "private.example:80")
	if err == nil || !strings.Contains(err.Error(), "public IP") {
		t.Fatalf("private DNS result error = %v", err)
	}
	if dialCalled {
		t.Fatal("dial was attempted for a non-public DNS result")
	}
}

func TestStrictLinkRedirectPolicy(t *testing.T) {
	origin, err := http.NewRequest(http.MethodGet, "https://www.xiaohongshu.com/explore/origin", nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		target     string
		wantError  bool
		wantCookie bool
	}{
		{"detail", "https://www.rednote.com/discovery/item/next", false, true},
		{"short", "https://xhslink.com/next", true, false},
		{"short cn", "https://xhslink.cn/next", true, false},
		{"downgrade", "http://www.xiaohongshu.com/explore/next", true, false},
		{"external", "https://example.com/explore/next", true, false},
		{"nonstandard port", "https://www.xiaohongshu.com:8443/explore/next", true, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodGet, test.target, nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Cookie", "a=secret")
			err = strictLinkRedirectPolicy(request, []*http.Request{origin})
			if (err != nil) != test.wantError {
				t.Fatalf("strictLinkRedirectPolicy() error = %v, wantError %t", err, test.wantError)
			}
			if got := request.Header.Get("Cookie") != ""; got != test.wantCookie {
				t.Fatalf("Cookie retained = %t, want %t", got, test.wantCookie)
			}
		})
	}
}

func TestLinkAwareRedirectPolicyRequiresHTTPSForMedia(t *testing.T) {
	origin, err := http.NewRequest(http.MethodGet, "https://sns-img.example/media.jpg", nil)
	if err != nil {
		t.Fatal(err)
	}
	httpsTarget, err := http.NewRequest(http.MethodGet, "https://cdn.example/media.jpg", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := linkAwareRedirectPolicy(httpsTarget, []*http.Request{origin}); err != nil {
		t.Fatalf("HTTPS media redirect was rejected: %v", err)
	}

	httpTarget, err := http.NewRequest(http.MethodGet, "http://cdn.example/media.jpg", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := linkAwareRedirectPolicy(httpTarget, []*http.Request{origin}); err == nil {
		t.Fatal("media HTTPS downgrade redirect was accepted")
	}
}

func TestFetchPageOnlySendsCookieToSupportedDetailURL(t *testing.T) {
	var cookies []string
	client := &http.Client{Transport: securityRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		cookies = append(cookies, request.Header.Get("Cookie"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    request,
		}, nil
	})}
	cookie := "a=secret"
	headers := requestHeaders(&cookie)

	if _, err := fetchPage(t.Context(), client, "https://example.invalid/explore/note", headers, 1024); err != nil {
		t.Fatal(err)
	}
	if _, err := fetchPage(t.Context(), client, "https://www.xiaohongshu.com/explore/note", headers, 1024); err != nil {
		t.Fatal(err)
	}
	if len(cookies) != 2 || cookies[0] != "" || cookies[1] != cookie {
		t.Fatalf("observed Cookie headers = %#v", cookies)
	}
}

func TestMediaHeadersDisableAutomaticCompression(t *testing.T) {
	if got := mediaHeaders().Get("Accept-Encoding"); got != "identity" {
		t.Fatalf("Accept-Encoding = %q, want identity", got)
	}
}
