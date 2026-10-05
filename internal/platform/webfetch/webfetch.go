// Package webfetch is the suite's one way to make outbound HTTP requests
// to publisher-controlled URLs: an SSRF guard checked against the resolved
// address at dial time, a redirect cap with per-hop scheme checks, and body
// caps applied after decompression. Apps that fetch the open web use it
// rather than net/http directly, so those guards live in one place.
package webfetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

// Body size caps for callers to pass as GetOptions.MaxBytes. Applied after
// decompression, which is where a compression bomb would otherwise land.
const (
	MaxPageBytes  = 2 << 20
	MaxImageBytes = 5 << 20
	// MaxFaviconBytes bounds a favicon fetch. Favicons are tiny; this just
	// caps a misbehaving server, the same role MaxImageBytes plays for
	// article images.
	MaxFaviconBytes = 64 << 10
)

const (
	maxRedirects   = 5
	requestTimeout = 30 * time.Second
)

// ErrBlockedAddress is a fetch refused because it resolved to an address this
// server must not reach: loopback, link-local, RFC1918 and friends.
var ErrBlockedAddress = errors.New("webfetch: blocked address")

// blockedPrefixes are ranges netip's own predicates do not cover.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT, and where Tailscale lives
	netip.MustParsePrefix("192.0.0.0/24"),  // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking
	netip.MustParsePrefix("192.0.2.0/24"),  // TEST-NET-1
}

// DenyPrivateAddr refuses an address this server has no business connecting to.
//
// It takes the address the dialer is about to connect to, not a hostname. That
// is the whole point: a name that resolves to a public address when the feed is
// added and to 127.0.0.1 when it is polled — DNS rebinding — is still refused,
// because the check happens against the resolved IP at connect time.
func DenyPrivateAddr(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: unparseable dial address %q", ErrBlockedAddress, address)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("%w: %q is not an IP", ErrBlockedAddress, host)
	}
	ip = ip.Unmap()

	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, ip)
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return fmt.Errorf("%w: %s in %s", ErrBlockedAddress, ip, p)
		}
	}
	return nil
}

// Client is the one HTTP client an app makes outbound requests with. Feed
// polling, article extraction and the image proxy all share it, so there is
// one place the guards live.
type Client struct {
	http *http.Client
	cfg  Config

	// DenyAddr decides whether a resolved address may be connected to. It is a
	// field rather than a package function because httptest listens on
	// 127.0.0.1, which the default guard refuses by construction: tests inject
	// a permissive variant, while TestDefaultClientRefusesPrivateAddresses
	// exercises the real one.
	DenyAddr func(address string) error
}

// Config is what differs between the apps that use a Client.
type Config struct {
	// UserAgent identifies the app to publishers, e.g.
	// "onsuite/v2.1.0 (ON Reader; +https://github.com/iliafrenkel/on-suite)".
	UserAgent string
	// DefaultAccept is sent when GetOptions.Accept is empty.
	DefaultAccept string
	// DefaultMaxBytes bounds the body when GetOptions.MaxBytes is zero.
	DefaultMaxBytes int64
}

// New returns a client with every guard in place.
func New(cfg Config) *Client {
	c := &Client{cfg: cfg, DenyAddr: DenyPrivateAddr}

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			return c.DenyAddr(address)
		},
	}

	c.http = &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
			MaxIdleConnsPerHost:   2,
			// Compression stays on; the size cap is applied to the decoded
			// stream, so a bomb is truncated rather than expanded.
			DisableCompression: false,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("webfetch: more than %d redirects", maxRedirects)
			}
			// The scheme is re-checked on every hop; the address guard runs in
			// the dialer, so it covers each hop already.
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("webfetch: redirect to %q scheme", req.URL.Scheme)
			}
			return nil
		},
	}
	return c
}

// GetOptions carries the per-call knobs.
type GetOptions struct {
	// ETag and LastModified turn the request into a conditional GET.
	ETag         string
	LastModified string
	// MaxBytes bounds the body. Zero means Config.DefaultMaxBytes.
	MaxBytes int64
	// Accept is the Accept header. Empty sends Config.DefaultAccept.
	Accept string
}

// Response is what a fetch produced.
type Response struct {
	Status       int
	Body         []byte
	ETag         string
	LastModified string
	ContentType  string
	FinalURL     string
	NotModified  bool
}

// Get fetches one URL under every guard this client carries.
func (c *Client) Get(ctx context.Context, rawURL string, opts GetOptions) (*Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("webfetch: parse %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("webfetch: refusing %q scheme", u.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("webfetch: build request: %w", err)
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	if opts.Accept != "" {
		req.Header.Set("Accept", opts.Accept)
	} else if c.cfg.DefaultAccept != "" {
		req.Header.Set("Accept", c.cfg.DefaultAccept)
	}
	if opts.ETag != "" {
		req.Header.Set("If-None-Match", opts.ETag)
	}
	if opts.LastModified != "" {
		req.Header.Set("If-Modified-Since", opts.LastModified)
	}

	res, err := c.http.Do(req)
	if err != nil {
		// Unwrap so errors.Is(err, ErrBlockedAddress) works through
		// url.Error and net.OpError.
		return nil, fmt.Errorf("webfetch: fetch %s: %w", u.Redacted(), err)
	}
	defer func() { _ = res.Body.Close() }()

	out := &Response{
		Status:       res.StatusCode,
		ETag:         res.Header.Get("ETag"),
		LastModified: res.Header.Get("Last-Modified"),
		ContentType:  res.Header.Get("Content-Type"),
		FinalURL:     res.Request.URL.String(),
		NotModified:  res.StatusCode == http.StatusNotModified,
	}
	if out.NotModified {
		return out, nil
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return out, fmt.Errorf("webfetch: %s returned %s", u.Redacted(), res.Status)
	}

	limit := opts.MaxBytes
	if limit <= 0 {
		limit = c.cfg.DefaultMaxBytes
	}
	if limit <= 0 {
		limit = MaxPageBytes
	}
	// Read one byte past the limit so an oversize body can be told apart from
	// one that exactly fills it: io.LimitReader alone returns exactly `limit`
	// bytes with a nil error either way, which would otherwise let a
	// truncated body sail through as if it were complete. That matters most
	// for images: a truncated one still sniffs a confident content-type from
	// its leading magic bytes and would be cached as valid, corrupt, forever.
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return out, fmt.Errorf("webfetch: read body from %s: %w", u.Redacted(), err)
	}
	if int64(len(body)) > limit {
		return out, fmt.Errorf("webfetch: response exceeds %d byte limit", limit)
	}
	out.Body = body
	return out, nil
}
