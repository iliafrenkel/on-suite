package reader

import (
	"net"
	"net/netip"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// AllowPrivateFetchesForTest lets this app's HTTP client reach loopback, so
// handler tests can point it at an httptest origin. Production never calls it;
// the real guard is what TestDefaultClientRefusesPrivateAddresses in
// client_test.go exercises.
//
// Only loopback: every other address is still refused, so a test can never
// reach the real internet by accident — fixtures' site URLs (example.com and
// friends) are real hosts, and adding a feed now reads its site's homepage
// for a favicon (#451).
//
// It must be called after Mount, which is where a.client is built —
// apptest.NewServer has already mounted by the time it hands the App back.
//
// This lives in a _test.go file (the standard Go export_test.go idiom) so it
// is compiled into the test binary — where package reader_test can still call
// it, since Go links internal and external test files together — but never
// into the production binary.
func (a *App) AllowPrivateFetchesForTest() {
	a.client.DenyAddr = func(address string) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return webfetch.ErrBlockedAddress
		}
		if ip, err := netip.ParseAddr(host); err == nil && ip.Unmap().IsLoopback() {
			return nil
		}
		return webfetch.ErrBlockedAddress
	}
}

// SetDiscoveryTimeoutForTest overrides resolveFeedURL's overall discovery
// deadline (production default: 25s) so a test proving the deadline is
// enforced doesn't need a real 25-second sleep. It returns a func that
// restores the previous value — call it (typically via defer) so one test's
// override cannot leak into another.
func SetDiscoveryTimeoutForTest(d time.Duration) (restore func()) {
	prev := discoveryTimeout
	discoveryTimeout = d
	return func() { discoveryTimeout = prev }
}

// SetFetchOnAddTimeoutForTest overrides the synchronous fetch-on-add deadline
// (production default: 10s) so a test proving a slow origin degrades like a
// failing one doesn't need a real 10-second sleep. It returns a func that
// restores the previous value — call it (typically via defer) so one test's
// override cannot leak into another.
func SetFetchOnAddTimeoutForTest(d time.Duration) (restore func()) {
	prev := fetchOnAddTimeout
	fetchOnAddTimeout = d
	return func() { fetchOnAddTimeout = prev }
}

// MaxImageFetchAttemptsForTest and ImageRetryBackoffForTest mirror imgproxy.go's
// unexported maxImageFetchAttempts/imageRetryBackoff, so a test can assert
// against the real thresholds rather than a hardcoded copy that could
// silently drift out of sync with them.
const (
	MaxImageFetchAttemptsForTest = maxImageFetchAttempts
	ImageRetryBackoffForTest     = imageRetryBackoff
)

// FullArticleRetryBackoffForTest mirrors handlers.go's unexported
// fullArticleRetryBackoff, for the same reason as the two constants above.
const FullArticleRetryBackoffForTest = fullArticleRetryBackoff
