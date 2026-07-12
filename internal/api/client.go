package api

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultUserAgent   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36 Edg/143.0.0.0"
	proxyLookupTimeout = 5 * time.Second
)

var nonPublicIPPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/29"),
	netip.MustParsePrefix("192.0.0.8/32"),
	netip.MustParsePrefix("192.0.0.170/31"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:10::/28"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

type clientFactory func(proxy *string) (*http.Client, error)

type lookupIPFunc func(context.Context, string, string) ([]net.IP, error)

type dialContextFunc func(context.Context, string, string) (net.Conn, error)

type pooledHTTPClient struct {
	client    *http.Client
	transport *http.Transport
}

type httpClientPool struct {
	mu                    sync.Mutex
	defaultClient         *pooledHTTPClient
	responseHeaderTimeout time.Duration
	allowPrivateProxy     bool
	closed                bool
}

func newHTTPClientPool(responseHeaderTimeout time.Duration, allowPrivateProxy bool) *httpClientPool {
	return &httpClientPool{
		responseHeaderTimeout: responseHeaderTimeout,
		allowPrivateProxy:     allowPrivateProxy,
	}
}

func (p *httpClientPool) Client(proxy *string) (*http.Client, error) {
	proxyURL, err := parseProxy(proxy, p.allowPrivateProxy)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errors.New("http client pool is closed")
	}
	if proxyURL != nil {
		client, _ := newHTTPClient(proxyURL, p.responseHeaderTimeout, p.allowPrivateProxy)
		return client, nil
	}
	if p.defaultClient != nil {
		return p.defaultClient.client, nil
	}

	client, transport := newHTTPClient(nil, p.responseHeaderTimeout, false)
	p.defaultClient = &pooledHTTPClient{client: client, transport: transport}
	return client, nil
}

func (p *httpClientPool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	client := p.defaultClient
	p.defaultClient = nil
	p.mu.Unlock()

	if client != nil {
		client.transport.CloseIdleConnections()
	}
	return nil
}

func parseProxy(proxy *string, allowPrivateProxy bool) (*url.URL, error) {
	if proxy == nil {
		return nil, nil
	}
	raw := strings.TrimSpace(*proxy)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (strings.ToLower(parsed.Scheme) != "http" && strings.ToLower(parsed.Scheme) != "https") {
		return nil, errors.New("proxy must be an http or https URL")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if allowPrivateProxy {
		return parsed, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), proxyLookupTimeout)
	defer cancel()
	if err := validatePublicProxyHost(ctx, parsed, net.DefaultResolver.LookupIP); err != nil {
		return nil, err
	}
	return parsed, nil
}

func validatePublicProxyHost(ctx context.Context, proxyURL *url.URL, lookup lookupIPFunc) error {
	addresses, err := lookupHostIPs(ctx, proxyURL.Hostname(), lookup)
	if err != nil {
		return fmt.Errorf("resolve proxy host: %w", err)
	}
	if len(addresses) == 0 {
		return errors.New("proxy host did not resolve to an IP address")
	}
	for _, address := range addresses {
		if !isPublicIP(address) {
			return fmt.Errorf("proxy host resolves to non-public IP address %s", address)
		}
	}
	return nil
}

func newHTTPClient(proxy *url.URL, responseHeaderTimeout time.Duration, allowPrivateProxy bool) (*http.Client, *http.Transport) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	transport.Proxy = nil
	if proxy != nil {
		transport.Proxy = http.ProxyURL(proxy)
	}
	if proxy == nil || !allowPrivateProxy {
		dialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		transport.DialContext = newPublicDialContext(net.DefaultResolver.LookupIP, dialer.DialContext)
	}
	return &http.Client{
		Transport:     transport,
		CheckRedirect: linkAwareRedirectPolicy,
	}, transport
}

func newPublicDialContext(lookup lookupIPFunc, dial dialContextFunc) dialContextFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("split dial address: %w", err)
		}
		addresses, err := lookupHostIPs(ctx, host, lookup)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", host, err)
		}

		var dialErrors []error
		publicAddresses := 0
		for _, ip := range addresses {
			if !isPublicIP(ip) || !ipMatchesNetwork(ip, network) {
				continue
			}
			publicAddresses++
			resolvedAddress := net.JoinHostPort(ip.String(), port)
			connection, dialErr := dial(ctx, network, resolvedAddress)
			if dialErr == nil {
				return connection, nil
			}
			dialErrors = append(dialErrors, dialErr)
		}
		if publicAddresses == 0 {
			return nil, fmt.Errorf("refusing to dial %s: host did not resolve to a public IP address", host)
		}
		return nil, fmt.Errorf("dial %s: %w", host, errors.Join(dialErrors...))
	}
}

func lookupHostIPs(ctx context.Context, host string, lookup lookupIPFunc) ([]net.IP, error) {
	if host == "" {
		return nil, errors.New("host is empty")
	}
	if literal := net.ParseIP(strings.TrimSuffix(host, ".")); literal != nil {
		return []net.IP{literal}, nil
	}
	return lookup(ctx, "ip", host)
}

func ipMatchesNetwork(ip net.IP, network string) bool {
	switch network {
	case "tcp4":
		return ip.To4() != nil
	case "tcp6":
		return ip.To4() == nil
	default:
		return true
	}
}

func isPublicIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range nonPublicIPPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func linkAwareRedirectPolicy(request *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("too many redirects")
	}
	if request == nil || request.URL == nil {
		return errors.New("redirect URL is missing")
	}
	if !isDetailURL(request.URL.String()) {
		request.Header.Del("Cookie")
	}
	if !strings.EqualFold(request.URL.Scheme, "https") {
		return fmt.Errorf("refusing redirect to non-HTTPS URL %s", request.URL.Redacted())
	}
	if len(via) > 0 && isDetailURL(via[0].URL.String()) && !isDetailURL(request.URL.String()) {
		return fmt.Errorf("refusing detail redirect outside supported detail hosts: %s", request.URL.Redacted())
	}
	if len(via) > 0 && isSupportedLinkURL(via[0].URL) && !isSupportedLinkURL(request.URL) {
		return fmt.Errorf("refusing redirect to unsupported URL %s", request.URL.Redacted())
	}
	return nil
}

func strictLinkRedirectPolicy(request *http.Request, via []*http.Request) error {
	if err := linkAwareRedirectPolicy(request, via); err != nil {
		return err
	}
	if !isSupportedLinkURL(request.URL) {
		return fmt.Errorf("refusing redirect to unsupported URL %s", request.URL.Redacted())
	}
	return nil
}

func withStrictLinkRedirects(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	clone := *client
	previousPolicy := client.CheckRedirect
	clone.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if err := strictLinkRedirectPolicy(request, via); err != nil {
			return err
		}
		if previousPolicy != nil {
			return previousPolicy(request, via)
		}
		return nil
	}
	return &clone
}

func requestHeaders(cookie *string) http.Header {
	headers := make(http.Header)
	headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	headers.Set("Referer", "https://www.xiaohongshu.com/explore")
	headers.Set("User-Agent", defaultUserAgent)
	if cookie != nil {
		headers.Set("Cookie", *cookie)
	}
	return headers
}

func fetchPage(ctx context.Context, client *http.Client, target string, headers http.Header, maxBytes int64) (string, error) {
	client = withStrictLinkRedirects(client)
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return "", err
		}
		copyHeadersForURL(request.Header, headers, request.URL)
		response, err := client.Do(request)
		if err != nil {
			lastErr = err
		} else {
			body, readErr := readLimitedBody(response.Body, maxBytes)
			response.Body.Close()
			if readErr != nil {
				return "", readErr
			}
			if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
				return string(body), nil
			}
			lastErr = fmt.Errorf("upstream returned %s", response.Status)
			if response.StatusCode < http.StatusInternalServerError && response.StatusCode != http.StatusTooManyRequests {
				return "", lastErr
			}
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 250 * time.Millisecond):
			}
		}
	}
	return "", fmt.Errorf("fetch page: %w", lastErr)
}

func copyHeadersForURL(destination, source http.Header, target *url.URL) {
	allowCookie := target != nil && isDetailURL(target.String())
	for key, values := range source {
		if strings.EqualFold(key, "Cookie") && !allowCookie {
			continue
		}
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

func readLimitedBody(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = 16 << 20
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("upstream response exceeds %d bytes", maxBytes)
	}
	return body, nil
}

func mediaHeaders() http.Header {
	headers := make(http.Header)
	headers.Set("Accept", "*/*")
	headers.Set("Accept-Encoding", "identity")
	headers.Set("Referer", "https://www.xiaohongshu.com/")
	headers.Set("User-Agent", defaultUserAgent)
	return headers
}

func normalizedURL(value string) string {
	return strings.ReplaceAll(value, "\\/", "/")
}
