package later

import (
	"net"
	"net/netip"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// AllowPrivateFetchesForTest lets the client reach loopback only, so tests
// can use httptest origins; mirrors Reader's own hook. Call after Mount.
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

// SetSaveTimeoutForTest overrides the fetch-and-extract deadline.
func SetSaveTimeoutForTest(d time.Duration) (restore func()) {
	prev := saveTimeout
	saveTimeout = d
	return func() { saveTimeout = prev }
}
