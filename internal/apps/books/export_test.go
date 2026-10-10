package books

import (
	"context"
	"database/sql"
	"net"
	"net/netip"
	"strings"
	"time"

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

// SnippetPartsForTest renders snippetParts as text with each hit in [ ].
func SnippetPartsForTest(s string) string {
	var b strings.Builder
	for _, p := range snippetParts(s) {
		if p.Hit {
			b.WriteString("[" + p.Text + "]")
		} else {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// NoPauseForTest makes the cover backfill's pause between requests
// instant, counting how often it is asked for.
func (a *App) NoPauseForTest(count *int) {
	a.pause = func(context.Context, time.Duration) error { *count++; return nil }
}

// OLIDForTest, CoverIDTextForTest and EditFormMaxBytes expose the form's
// id cleaning and the edit form's body limit to the boundary tests.
func OLIDForTest(s string, kind byte) string { return olID(s, kind) }

func CoverIDTextForTest(s string) string { return coverIDText(s) }

const EditFormMaxBytes = editFormMaxBytes

// SetCoverTimeoutForTest shortens the bound on one cover fetch until the
// test ends. Tests that use it must not run in parallel.
func SetCoverTimeoutForTest(t interface {
	Helper()
	Cleanup(func())
}, d time.Duration) {
	t.Helper()
	old := coverTimeout
	coverTimeout = d
	t.Cleanup(func() { coverTimeout = old })
}
