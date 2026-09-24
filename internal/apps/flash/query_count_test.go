// internal/apps/flash/query_count_test.go
package flash_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// substringCountingConn counts Prepare calls whose query text contains
// substr — e.g. distinguishing auth.Store.ListAccounts' own SQL from
// everything else a request happens to run. Like deck_summary_batch_test.go's
// countingConn, it exposes only driver.Conn's three methods, so
// database/sql cannot see the real connection's QueryerContext/ExecerContext
// and routes every statement through Prepare, the one place it is counted.
type substringCountingConn struct {
	driver.Conn
	substr string
	n      *atomic.Int64
}

func (c substringCountingConn) Prepare(query string) (driver.Stmt, error) {
	if strings.Contains(query, c.substr) {
		c.n.Add(1)
	}
	return c.Conn.Prepare(query)
}

// substringCountingConnector opens connections wrapped in
// substringCountingConn, without registering a named database/sql driver:
// sql.OpenDB(connector) takes a driver.Connector directly, so each test gets
// its own counter without the process-wide registration (and "must not run
// in parallel" caveat) deck_summary_batch_test.go's countingDriver needs.
type substringCountingConnector struct {
	inner  driver.Driver
	dsn    string
	substr string
	n      *atomic.Int64
}

func (c substringCountingConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.inner.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return substringCountingConn{Conn: conn, substr: c.substr, n: c.n}, nil
}

func (c substringCountingConnector) Driver() driver.Driver { return c.inner }

// newQueryCountingServer builds the same flash handler stack every other
// handler test in this package uses, over a database whose connection counts
// how many times a query containing substr runs. It gives a handler test a
// cheap way to pin "at most once per request" without the Users dependency
// being an interface: app.Deps.Users is a concrete *auth.Store, so a fake
// can't be substituted for it, but the query it (or any store method) runs
// can still be counted at the driver level, the same technique
// deck_summary_batch_test.go already uses for DeckSummaries' own query
// count.
func newQueryCountingServer(t *testing.T, substr string) (*apptest.Server[*flash.Store], *atomic.Int64) {
	t.Helper()
	probe, err := sql.Open("sqlite", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = probe.Close() })

	var n atomic.Int64
	path := filepath.Join(t.TempDir(), "test.db")
	handle := sql.OpenDB(substringCountingConnector{inner: probe.Driver(), dsn: db.DSN(path), substr: substr, n: &n})
	handle.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = handle.Close() })

	s := apptest.NewServer(t, flash.New(), flash.NewStore, apptest.WithDatabase(handle))
	return s, &n
}

// listAccountsSubstring is the one clause unique to auth.Store.ListAccounts'
// own query (see internal/platform/auth/store.go) among everything else a
// flash request might run — nothing else selects from "users u".
const listAccountsSubstring = "FROM users u"

// TestShareDeckHandlerListsAccountsOnce pins #359: shareDeck used to run
// ListAccounts up to 3 times — validating to_user_id, shareContext for the
// re-rendered pane, and again inside buildDeckIndex to enrich the sharer's
// own pending gifts. Giving alice a pending offer of her own exercises that
// third call.
func TestShareDeckHandlerListsAccountsOnce(t *testing.T) {
	s, n := newQueryCountingServer(t, listAccountsSubstring)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	otherDeckID := createDeckHX(t, s, s.Bob, "French")
	s.PostHX(t, s.Bob, "/flash/"+strconv.FormatInt(otherDeckID, 10)+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Alice.User.ID, 10)}})
	n.Store(0)

	rec := s.PostHX(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})
	if rec.Code != 200 {
		t.Fatalf("share: %d; body: %s", rec.Code, rec.Body.String())
	}
	if got := n.Load(); got != 1 {
		t.Errorf("shareDeck ran ListAccounts %d times, want 1", got)
	}
}

// TestGiftPreviewListsAccountsOnce pins #359: a gift preview's own pending
// offer always gives buildDeckIndex an offer to enrich, so giftPreview used
// to always run ListAccounts twice — once for the "From" username, again to
// enrich the gift row in the out-of-band list.
func TestGiftPreviewListsAccountsOnce(t *testing.T) {
	s, n := newQueryCountingServer(t, listAccountsSubstring)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	shareID := shareToBob(t, s, deckID)
	n.Store(0)

	s.Get(t, s.Bob, "/flash/shared/"+shareID)
	if got := n.Load(); got != 1 {
		t.Errorf("giftPreview ran ListAccounts %d times, want 1", got)
	}
}

// TestAdoptShareHandlerListsAccountsOnce pins #359: adopting used to run
// ListAccounts for the sharer's username, again for shareContext's
// re-rendered pane, and a third time inside buildDeckIndex whenever another
// gift is still pending — set up here with a second offer from alice.
func TestAdoptShareHandlerListsAccountsOnce(t *testing.T) {
	s, n := newQueryCountingServer(t, listAccountsSubstring)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	shareID := shareToBob(t, s, deckID)
	otherDeckID := createDeckHX(t, s, s.Alice, "French")
	shareToBob(t, s, otherDeckID)
	n.Store(0)

	rec := s.PostHX(t, s.Bob, "/flash/shared/adopt", url.Values{"share_id": {shareID}})
	if rec.Code != 200 {
		t.Fatalf("adopt: %d; body: %s", rec.Code, rec.Body.String())
	}
	if got := n.Load(); got != 1 {
		t.Errorf("adoptShareHandler ran ListAccounts %d times, want 1", got)
	}
}
