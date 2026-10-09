package books

import (
	"database/sql"
	"net"
	"net/netip"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// UseOpenLibraryForTest points this app's Open Library client at base (an
// httptest server standing in for both openlibrary.org and
// covers.openlibrary.org) and lets its fetch client reach loopback — only
// loopback, so a test can never reach the real internet. Call it after
// Mount; apptest.NewServer has already mounted. It lives in a _test.go file
// (the export_test.go idiom Reader and Later use), so it never reaches the
// production binary.
func (a *App) UseOpenLibraryForTest(base string) {
	a.ol.Base, a.ol.Covers = base, base
	a.web.DenyAddr = func(address string) error {
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

// DBForTest is the store's handle, for tests that check a column no store
// method returns.
func (st *Store) DBForTest() *sql.DB { return st.db }
