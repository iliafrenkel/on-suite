// internal/apps/flash/deck_summary_batch_test.go
package flash_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// TestDeckSummariesMatchPerDeckReference pins the batched DeckSummaries to
// the single-deck path, deck by deck, over a mix that exercises every
// branch of the budget logic: an empty deck, a snoozed deck, a deck with
// no review limit next to one with a limit, and decks with and without
// counters for today (plus a counter from yesterday that must not count,
// and another user's deck that must not leak in).
//
// The reference, DeckSummariesPerDeckForTest, calls deckSummary per deck —
// the same summarise the batched path also ends in — so this test cross-
// checks the batched SQL against the single-deck queries. It does not by
// itself prove the budget arithmetic is right; TestDeckSummariesAgreeWithDueQueue
// (deck_summary_test.go) checks that independently, against DueQueue rather
// than against summarise.
func TestDeckSummariesMatchPerDeckReference(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	t0 := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

	mkDeck := func(owner int64, name string) flash.Deck {
		t.Helper()
		d, err := f.store.CreateDeck(ctx, owner, name, "")
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	mkCards := func(owner int64, d flash.Deck, n int) []flash.Card {
		t.Helper()
		var out []flash.Card
		for i := 0; i < n; i++ {
			c, err := f.store.CreateCard(ctx, owner, d.ID, flash.CardTypeBasic, fmt.Sprintf("%s %d", d.Name, i), "back", "")
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, c)
		}
		return out
	}
	grade := func(owner int64, cards []flash.Card, rating int, at time.Time) {
		t.Helper()
		for _, c := range cards {
			if _, err := f.store.GradeCard(ctx, owner, c.ID, rating, at); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Empty: no cards, no counters.
	mkDeck(f.alice.ID, "Empty")

	// Unlimited reviews, graded yesterday (counter on another day) so
	// today has no counter row but plenty is due.
	unlimited := mkDeck(f.alice.ID, "Unlimited")
	uc := mkCards(f.alice.ID, unlimited, 6)
	grade(f.alice.ID, uc[:4], flash.RatingAgain, t0.Add(-24*time.Hour))

	// Limited: review limit 2, new limit 3, graded today so today's
	// counter row exists and eats into both budgets.
	limited := mkDeck(f.alice.ID, "Limited")
	two := 2
	if _, err := f.store.UpdateDeckSettings(ctx, f.alice.ID, limited.ID, 3, &two); err != nil {
		t.Fatal(err)
	}
	lc := mkCards(f.alice.ID, limited, 8)
	grade(f.alice.ID, lc[:2], flash.RatingAgain, t0.Add(-2*24*time.Hour))
	grade(f.alice.ID, lc[2:5], flash.RatingGood, t0)
	grade(f.alice.ID, lc[:1], flash.RatingGood, t0.Add(-time.Hour)) // a review, not a new card

	// Mastered: cards graded Easy long ago, so some are in state "review".
	mastered := mkDeck(f.alice.ID, "Mastered")
	mc := mkCards(f.alice.ID, mastered, 3)
	grade(f.alice.ID, mc, flash.RatingEasy, t0.Add(-40*24*time.Hour))

	// Snoozed, with due cards and today's counters.
	snoozed := mkDeck(f.alice.ID, "Snoozed")
	sc := mkCards(f.alice.ID, snoozed, 4)
	grade(f.alice.ID, sc[:2], flash.RatingAgain, t0.Add(-time.Hour))
	if _, err := f.store.SnoozeDeck(ctx, f.alice.ID, snoozed.ID, t0.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}

	// Bob's deck: must not appear in, or bleed counts into, alice's.
	bobs := mkDeck(f.bob.ID, "Bob's")
	bc := mkCards(f.bob.ID, bobs, 3)
	grade(f.bob.ID, bc[:1], flash.RatingAgain, t0)

	for _, now := range []time.Time{t0, t0.Add(30 * time.Minute), t0.Add(3 * time.Hour), t0.Add(26 * time.Hour), t0.Add(72 * time.Hour)} {
		for _, user := range []int64{f.alice.ID, f.bob.ID} {
			got, err := f.store.DeckSummaries(ctx, user, now)
			if err != nil {
				t.Fatal(err)
			}
			want, err := f.store.DeckSummariesPerDeckForTest(ctx, user, now)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("user %d at %v:\n got  %+v\n want %+v", user, now, got, want)
			}
		}
	}
	// Sanity: the scenario really does cover what it claims to.
	sums, err := f.store.DeckSummaries(ctx, f.alice.ID, t0.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 5 {
		t.Fatalf("alice has %d summaries, want 5", len(sums))
	}
	if s := summaryFor(t, sums, snoozed.ID); !s.Snoozed || s.ReviewNow != 0 || s.DueTotal == 0 {
		t.Errorf("snoozed summary = %+v, want snoozed, due cards, nothing today", s)
	}
	if s := summaryFor(t, sums, limited.ID); s.DueTotal <= s.DueToday || s.NewToday != 0 {
		t.Errorf("limited summary = %+v, want reviews and new cards held back by today's limits", s)
	}
	if s := summaryFor(t, sums, mastered.ID); s.Mastered == 0 {
		t.Errorf("mastered summary = %+v, want mastered cards", s)
	}
}

// TestDeckSummariesQueryCountDoesNotGrowWithDecks pins #319: DeckSummaries
// runs a fixed number of statements however many decks there are.
func TestDeckSummariesQueryCountDoesNotGrowWithDecks(t *testing.T) {
	f, counter := newCountingFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

	count := func() int64 {
		t.Helper()
		counter.Store(0)
		if _, err := f.store.DeckSummaries(ctx, f.alice.ID, now); err != nil {
			t.Fatal(err)
		}
		return counter.Load()
	}
	addDeck := func(i int) {
		t.Helper()
		d, err := f.store.CreateDeck(ctx, f.alice.ID, fmt.Sprintf("Deck %d", i), "")
		if err != nil {
			t.Fatal(err)
		}
		c, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "front", "back", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.GradeCard(ctx, f.alice.ID, c.ID, flash.RatingAgain, now); err != nil {
			t.Fatal(err)
		}
	}

	addDeck(0)
	one := count()
	for i := 1; i < 6; i++ {
		addDeck(i)
	}
	six := count()
	if one != six {
		t.Errorf("DeckSummaries ran %d statements for 1 deck and %d for 6; want the same", one, six)
	}
	if six != 3 {
		t.Errorf("DeckSummaries ran %d statements, want 3 (decks, card aggregates, today's counters)", six)
	}
}

// TestQueueFrontAllDecksQueryCountDoesNotGrowWithDecks: the all-decks
// scope reads its summaries in DeckSummaries' fixed queries; only the head
// probes remain per deck, and here one new-card probe finds the head.
func TestQueueFrontAllDecksQueryCountDoesNotGrowWithDecks(t *testing.T) {
	f, counter := newCountingFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

	count := func() int64 {
		t.Helper()
		counter.Store(0)
		front, err := f.store.QueueFront(ctx, f.alice.ID, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		if !front.HasHead {
			t.Fatal("QueueFront found no head")
		}
		return counter.Load()
	}
	addDeck := func(i int) {
		t.Helper()
		d, err := f.store.CreateDeck(ctx, f.alice.ID, fmt.Sprintf("Deck %d", i), "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "front", "back", ""); err != nil {
			t.Fatal(err)
		}
	}

	addDeck(0)
	one := count()
	for i := 1; i < 6; i++ {
		addDeck(i)
	}
	if six := count(); one != six {
		t.Errorf("QueueFront ran %d statements for 1 deck and %d for 6; want the same", one, six)
	}
}

// countingConn exposes only driver.Conn's three methods, so database/sql
// cannot see the real connection's QueryerContext/ExecerContext and routes
// every statement through Prepare — the one place it is counted.
type countingConn struct {
	driver.Conn
	n *atomic.Int64
}

func (c countingConn) Prepare(query string) (driver.Stmt, error) {
	c.n.Add(1)
	return c.Conn.Prepare(query)
}

type countingDriver struct {
	inner driver.Driver
	n     *atomic.Int64
}

func (d countingDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return countingConn{Conn: conn, n: d.n}, nil
}

var (
	registerCounting sync.Once
	statementCount   atomic.Int64
)

// newCountingFixture is newFixture over a connection that counts every
// statement it runs. The counter is process-wide, so tests using it must
// not run in parallel.
//
// It otherwise duplicates newFixture (fixture.go) rather than sharing it,
// since the whole point here is the non-standard driver name — but the DSN
// itself comes from db.DSN, the same pragmas Open uses in production, so it
// can't drift out of sync with them (see db.DSN's own comment).
func newCountingFixture(t *testing.T) (*fixture, *atomic.Int64) {
	t.Helper()
	ctx := context.Background()
	registerCounting.Do(func() {
		probe, err := sql.Open("sqlite", "")
		if err != nil {
			panic(err)
		}
		sql.Register("sqlite-counting", countingDriver{inner: probe.Driver(), n: &statementCount})
		_ = probe.Close()
	})

	path := filepath.Join(t.TempDir(), "test.db")
	handle, err := sql.Open("sqlite-counting", db.DSN(path))
	if err != nil {
		t.Fatal(err)
	}
	handle.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = handle.Close() })

	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	appMigrations, err := db.Collect(flash.ID, flash.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(migrations, appMigrations...)); err != nil {
		t.Fatal(err)
	}
	users := auth.NewStore(handle)
	alice, err := users.CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := users.CreateUser(ctx, "bob", apptest.PasswordHash, false)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{store: flash.NewStore(handle), db: handle, alice: alice, bob: bob}, &statementCount
}
