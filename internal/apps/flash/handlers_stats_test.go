// internal/apps/flash/handlers_stats_test.go
package flash_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestStatsPageZeroStateForBrandNewAccount(t *testing.T) {
	s := apptest.NewServer(t, flash.New(), flash.NewStore)
	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/stats", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /flash/stats = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Streak") || !strings.Contains(body, "Cards due") {
		t.Errorf("stats page missing expected tile labels; body: %s", body)
	}
	if !strings.Contains(body, "No decks yet.") {
		t.Errorf("stats page should show the empty per-deck state; body: %s", body)
	}
}

func TestStatsPageRequiresSignIn(t *testing.T) {
	s := apptest.NewServer(t, flash.New(), flash.NewStore)
	rec := s.Do(t, nil, httptest.NewRequest(http.MethodGet, "/flash/stats", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("GET /flash/stats with no session = 200, want a redirect/401")
	}
}

func TestStatsPageShowsPopulatedData(t *testing.T) {
	s := apptest.NewServer(t, flash.New(), flash.NewStore)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.GradeCard(t.Context(), s.Alice.User.ID, card.ID, flash.RatingGood, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/stats", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /flash/stats = %d; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Spanish") {
		t.Errorf("stats page should list the Spanish deck in the per-deck table; body: %s", rec.Body.String())
	}
}

// TestStatsPageRendersConsistentNumbers seeds two decks with known, distinct
// mastered/due/review counts, then asserts both the top-line tiles AND the
// per-deck table cells show the expected numbers — not just that a deck name
// appears somewhere in the body. This is the test that would catch a handler
// wiring the wrong store call into the wrong tile, or a template rendering
// the wrong field in a column.
func TestStatsPageRendersConsistentNumbers(t *testing.T) {
	s := apptest.NewServer(t, flash.New(), flash.NewStore)
	ctx := t.Context()

	// Alpha (sorts first): one card graded three times with Good, spaced
	// out enough to graduate it out of learning into "review" — mastered,
	// and its resulting due date lands well beyond "today".
	alpha, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	cardAlpha, err := s.Store.CreateCard(ctx, s.Alice.User.ID, alpha.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	// The handler's own store (a.store, created inside App.Mount) is a
	// separate *Store instance from s.Store here — both wrap the same
	// database handle, but SetClock on one does not affect the other's
	// notion of "now". So instead of faking the clock, times are anchored
	// to the real wall clock, far enough in the past that everything
	// graded below reads as "due" or "not due" the same way whether the
	// handler's real now() is called a millisecond or a few seconds after
	// this test computes t0.
	t0 := time.Now().UTC().Add(-3 * time.Hour)
	for _, at := range []time.Time{t0, t0.Add(20 * time.Minute), t0.Add(40 * time.Minute)} {
		if _, err := s.Store.GradeCard(ctx, s.Alice.User.ID, cardAlpha.ID, flash.RatingGood, at); err != nil {
			t.Fatal(err)
		}
	}

	// Beta (sorts second): one card graded once with Good — still in
	// learning, not mastered — whose short learning-step due date has
	// already passed by the time the page is viewed.
	beta, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Beta", "")
	if err != nil {
		t.Fatal(err)
	}
	cardBeta, err := s.Store.CreateCard(ctx, s.Alice.User.ID, beta.ID, flash.CardTypeBasic, "c", "d", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.GradeCard(ctx, s.Alice.User.ID, cardBeta.ID, flash.RatingGood, t0); err != nil {
		t.Fatal(err)
	}

	// Beta's card is now due (its short learning-step due date, minutes
	// after t0, is comfortably in the past); Alpha's is not (it graduated
	// into "review" with a due date days out). The handler's real now()
	// call, whenever it happens, sees the same picture.
	doc := s.Get(t, s.Alice, "/flash/stats")

	// Top-line tiles: 1 mastered (Alpha's card), 1 due (Beta's card; Alpha's
	// is now scheduled far in the future).
	tileValues := doc.QueryAll(".flash-stat-tile .value")
	if len(tileValues) != 4 {
		t.Fatalf("len(tile values) = %d, want 4", len(tileValues))
	}
	if got := htmlassert.Text(tileValues[2]); got != "1" {
		t.Errorf("mastered tile = %q, want %q", got, "1")
	}
	if got := htmlassert.Text(tileValues[3]); got != "1" {
		t.Errorf("due tile = %q, want %q", got, "1")
	}

	// Per-deck table: rows come out sorted by name (Alpha, then Beta), each
	// with its own Mastered/Due/Reviews(30 days) numbers.
	cells := doc.QueryAll(".flash-per-deck-table tbody tr td")
	if len(cells) != 8 {
		t.Fatalf("len(cells) = %d, want 8 (2 rows x 4 columns)", len(cells))
	}
	want := []string{
		"Alpha", "1", "0", "3", // mastered, not due, 3 reviews
		"Beta", "0", "1", "1", // not mastered, due, 1 review
	}
	var got []string
	for _, c := range cells {
		got = append(got, htmlassert.Text(c))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("cell[%d] = %q, want %q (full row values: %v)", i, got[i], w, got)
		}
	}
}

// TestStatsPageMarksSnoozedDeckInPerDeckTable proves the per-deck table
// gives some visible sign that a snoozed deck's Due count doesn't add up
// against the account-wide "Cards due" tile the same way an active deck's
// does (CardsDueToday excludes snoozed decks; PerDeckLoad's per-deck Due
// deliberately does not).
func TestStatsPageMarksSnoozedDeckInPerDeckTable(t *testing.T) {
	s := apptest.NewServer(t, flash.New(), flash.NewStore)
	ctx := t.Context()

	if _, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Active", ""); err != nil {
		t.Fatal(err)
	}
	snoozed, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Snoozed", "")
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(24 * time.Hour)
	if _, err := s.Store.SnoozeDeck(ctx, s.Alice.User.ID, snoozed.ID, future); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/stats")
	rows := doc.QueryAll(".flash-per-deck-table tbody tr")
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}

	var activeRow, snoozedRow string
	for _, r := range rows {
		text := htmlassert.Text(r)
		if strings.Contains(text, "Active") {
			activeRow = text
		}
		if strings.Contains(text, "Snoozed") {
			snoozedRow = text
		}
	}
	if strings.Contains(activeRow, "snoozed") {
		t.Errorf("active deck's row wrongly marked as snoozed: %q", activeRow)
	}
	if !strings.Contains(snoozedRow, "snoozed") {
		t.Errorf("snoozed deck's row not marked as snoozed: %q", snoozedRow)
	}
}
