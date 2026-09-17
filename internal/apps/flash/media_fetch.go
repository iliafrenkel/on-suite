// internal/apps/flash/media_fetch.go
package flash

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

// Body size caps, applied to the decoded stream so a compression bomb is
// truncated rather than expanded.
const (
	MaxImageFetchBytes = 5 << 20
	MaxAudioFetchBytes = 10 << 20
)

// mediaMaxBytes returns the fetch size cap for kind.
func mediaMaxBytes(kind string) int64 {
	if kind == MediaKindAudio {
		return MaxAudioFetchBytes
	}
	return MaxImageFetchBytes
}

const (
	maxMediaRedirects   = 5
	mediaRequestTimeout = 30 * time.Second
)

// ErrBlockedAddress is a fetch refused because it resolved to an address
// this server must not reach: loopback, link-local, RFC1918 and friends.
var ErrBlockedAddress = errors.New("flash: blocked address")

// blockedMediaPrefixes are ranges netip's own predicates do not cover.
var blockedMediaPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT, and where Tailscale lives
	netip.MustParsePrefix("192.0.0.0/24"),  // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking
	netip.MustParsePrefix("192.0.2.0/24"),  // TEST-NET-1
}

// denyPrivateMediaAddr refuses an address this server has no business
// connecting to. It takes the address the dialer is about to connect to, not
// a hostname — checking the resolved IP at connect time, not the name,
// defeats DNS rebinding (a name that resolves to a public address when the
// card is imported and to 127.0.0.1 when its media is later fetched).
// Mirrors internal/apps/reader's own DenyPrivateAddr; apps never import each
// other, so this is an independent copy with the same justification.
func denyPrivateMediaAddr(address string) error {
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
	for _, p := range blockedMediaPrefixes {
		if p.Contains(ip) {
			return fmt.Errorf("%w: %s in %s", ErrBlockedAddress, ip, p)
		}
	}
	return nil
}

// MediaClient is the one HTTP client Flash uses for outbound media fetches.
type MediaClient struct {
	http *http.Client
	ua   string

	// DenyAddr decides whether a resolved address may be connected to. It is
	// a field rather than a package function because httptest listens on
	// 127.0.0.1, which the default guard refuses by construction: tests
	// inject a permissive variant.
	DenyAddr func(address string) error
}

// NewMediaClient returns a client with every guard in place.
func NewMediaClient(version string) *MediaClient {
	c := &MediaClient{
		ua:       "onsuite/" + version + " (ON Flash; +https://github.com/iliafrenkel/on-suite)",
		DenyAddr: denyPrivateMediaAddr,
	}

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			return c.DenyAddr(address)
		},
	}

	c.http = &http.Client{
		Timeout: mediaRequestTimeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
			MaxIdleConnsPerHost:   2,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxMediaRedirects {
				return fmt.Errorf("flash: more than %d redirects", maxMediaRedirects)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("flash: redirect to %q scheme", req.URL.Scheme)
			}
			return nil
		},
	}
	return c
}

// Get fetches rawURL's body under every guard this client carries, capped at
// maxBytes (read one byte past the limit so a truncated body can be told
// apart from one that exactly fills it — a truncated image still sniffs a
// confident content-type from its leading bytes and would otherwise be
// cached as valid, corrupt, forever).
func (c *MediaClient) Get(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("flash: parse %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("flash: refusing %q scheme", u.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("flash: build request: %w", err)
	}
	req.Header.Set("User-Agent", c.ua)

	res, err := c.http.Do(req)
	if err != nil {
		// Unwrap so errors.Is(err, ErrBlockedAddress) works through
		// url.Error and net.OpError.
		return nil, fmt.Errorf("flash: fetch %s: %w", u.Redacted(), err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("flash: %s returned %s", u.Redacted(), res.Status)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("flash: read body from %s: %w", u.Redacted(), err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("flash: response exceeds %d byte limit", maxBytes)
	}
	return body, nil
}

// FetchAndCacheMedia fetches m's source URL, validates the sniffed
// content-type matches m.Kind, and caches the result via SaveMediaBytes on
// success. It does not record a failure on error — the caller (which knows
// the retry-backoff policy) decides whether and how to record one.
func (st *Store) FetchAndCacheMedia(ctx context.Context, client *MediaClient, m Media, now time.Time) (Media, error) {
	body, err := client.Get(ctx, m.SourceURL, mediaMaxBytes(m.Kind))
	if err != nil {
		return Media{}, err
	}

	// Sniff rather than trust: a publisher claiming image/png over an HTML
	// document is exactly how a proxy becomes an HTML-injection vector on
	// its own origin.
	ct := http.DetectContentType(body)
	if !contentTypeMatchesKind(ct, m.Kind) {
		return Media{}, fmt.Errorf("flash: response is not %s media (%s)", m.Kind, ct)
	}

	if err := st.SaveMediaBytes(ctx, m.Hash, ct, body, now); err != nil {
		return Media{}, err
	}
	m.ContentType = ct
	m.Bytes = body
	return m, nil
}
