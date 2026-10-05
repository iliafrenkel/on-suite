package later

import (
	"context"
	"net"
	"net/netip"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
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

// InsertHighlightForTest writes a highlight row without validation, to
// stand in for one whose quote no longer matches.
func (st *Store) InsertHighlightForTest(ctx context.Context, docID int64, start, end int, quote string) (int64, error) {
	now := db.FormatTime(st.now())
	res, err := st.db.ExecContext(ctx, `
		INSERT INTO later_highlights (article_id, start_offset, end_offset, quote, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, docID, start, end, quote, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
