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
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
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
	alpha, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Alpha", "", flash.DefaultDeckColor)
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
	beta, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Beta", "", flash.DefaultDeckColor)
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

	// Per-deck rows: sorted by name (Alpha, then Beta), each with its own
	// mastered / due / reviews-in-30-days numbers.
	texts := func(sel string) []string {
		var out []string
		for _, n := range doc.QueryAll(sel) {
			out = append(out, htmlassert.Text(n))
		}
		return out
	}
	if names := texts(".flash-load-row .flash-load-name"); strings.Join(names, ",") != "Alpha,Beta" {
		t.Fatalf("row names = %v, want [Alpha Beta]", names)
	}
	if got := texts(".flash-load-row .flash-load-mastered"); strings.Join(got, ",") != "1,0" {
		t.Errorf("mastered = %v, want [1 0]", got)
	}
	if got := texts(".flash-load-row .flash-load-due"); strings.Join(got, ",") != "all done,1 due" {
		t.Errorf("due = %v, want [all done, 1 due]", got)
	}
	if got := texts(".flash-load-row .flash-load-reviews-count"); strings.Join(got, ",") != "3,1" {
		t.Errorf("reviews = %v, want [3 1]", got)
	}
}

// TestStatsPageMarksSnoozedDeckInPerDeckTable proves the per-deck table
// gives some visible sign that a deck is snoozed: its row is marked "taking
// a break" while an active deck's is not. It does not assert a due-count
// value — the row's Due now comes from DeckSummary.ReviewNow, which is
// zeroed for a snoozed deck just like the deck list's own badge, so there is
// no divergence left to check here.
func TestStatsPageMarksSnoozedDeckInPerDeckTable(t *testing.T) {
	s := apptest.NewServer(t, flash.New(), flash.NewStore)
	ctx := t.Context()

	if _, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Active", "", flash.DefaultDeckColor); err != nil {
		t.Fatal(err)
	}
	snoozed, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Snoozed", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(24 * time.Hour)
	if _, err := s.Store.SnoozeDeck(ctx, s.Alice.User.ID, snoozed.ID, future); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/stats")
	rows := doc.QueryAll(".flash-load-row")
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
	if strings.Contains(activeRow, "taking a break") {
		t.Errorf("active deck's row wrongly marked as taking a break: %q", activeRow)
	}
	if !strings.Contains(snoozedRow, "taking a break") {
		t.Errorf("snoozed deck's row not marked as taking a break: %q", snoozedRow)
	}
}

func TestStatsRenderInsideTheHomeLayout(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/flash/stats")
	doc.MustHave("#deck-list")
	doc.MustHave("#deck-detail .flash-stats")
	row := doc.MustHave(".flash-load-row")
	if href, _ := htmlassert.Attr(row, "href"); href != "/flash/"+itoa(deck.ID) {
		t.Errorf("stats row href = %q, want the deck", href)
	}
	if target, _ := htmlassert.Attr(row, "hx-target"); target != "#deck-detail" {
		t.Errorf("stats row hx-target = %q", target)
	}
	btn := doc.MustHave(`.flash-home-toolbar a[href="/flash/stats"]`)
	if target, _ := htmlassert.Attr(btn, "hx-target"); target != "#deck-detail" {
		t.Errorf("toolbar Stats hx-target = %q, want the pane", target)
	}
}

// TestStatsRowsSortByName: the per-deck rows are sorted by name, not in
// ListDecks' newest-first order. Decks are created in an order that is
// neither alphabetical nor its reverse, so either mistake is caught.
func TestStatsRowsSortByName(t *testing.T) {
	s := newServer(t)
	for _, name := range []string{"Zebra", "Apple", "Mango"} {
		if _, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, name, "", flash.DefaultDeckColor); err != nil {
			t.Fatal(err)
		}
	}
	doc := s.Get(t, s.Alice, "/flash/stats")
	var names []string
	for _, n := range doc.QueryAll(".flash-load-row .flash-load-name") {
		names = append(names, htmlassert.Text(n))
	}
	if got := strings.Join(names, ","); got != "Apple,Mango,Zebra" {
		t.Errorf("row names = %v, want [Apple Mango Zebra]", names)
	}
}

// TestStatsHTMXPaneMatchesFullPage: the HTMX response (pane plus
// out-of-band deck list) shows exactly what the full page does, both in
// the stats pane and in the deck list.
func TestStatsHTMXPaneMatchesFullPage(t *testing.T) {
	s := newServer(t)
	ctx := t.Context()
	t0 := time.Now().UTC().Add(-3 * time.Hour)
	for i, name := range []string{"Beta", "Alpha", "Gamma"} {
		d, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, name, "", flash.DefaultDeckColor)
		if err != nil {
			t.Fatal(err)
		}
		for j := 0; j <= i; j++ {
			c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, d.ID, flash.CardTypeBasic, "front", "back", "")
			if err != nil {
				t.Fatal(err)
			}
			if j < i {
				if _, err := s.Store.GradeCard(ctx, s.Alice.User.ID, c.ID, flash.RatingGood, t0); err != nil {
					t.Fatal(err)
				}
			}
		}
		if name == "Gamma" {
			if _, err := s.Store.SnoozeDeck(ctx, s.Alice.User.ID, d.ID, time.Now().Add(24*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
	}

	full := s.Get(t, s.Alice, "/flash/stats")
	req := httpGet(t, "/flash/stats")
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HTMX GET /flash/stats = %d; body: %s", rec.Code, rec.Body.String())
	}
	frag := htmlassert.Parse(t, rec.Body.String())

	for _, sel := range []string{".flash-stats", "#deck-list"} {
		if got, want := htmlassert.Text(frag.MustHave(sel)), htmlassert.Text(full.MustHave(sel)); got != want {
			t.Errorf("%s differs:\n HTMX %q\n full %q", sel, got, want)
		}
	}
	if n := len(full.QueryAll(".flash-load-row")); n != 3 {
		t.Errorf("full page has %d stats rows, want 3", n)
	}

	// #352: the HTMX response also carries the deck list as an
	// out-of-band swap, and marks #flash-detail-open checked (the pane is
	// open), matching every other deck-detail HTMX fragment.
	list := frag.MustHave("#deck-list")
	if _, ok := htmlassert.Attr(list, "hx-swap-oob"); !ok {
		t.Error("#deck-list in the stats HTMX fragment is not marked hx-swap-oob")
	}
	open := frag.MustHave("#flash-detail-open")
	if _, checked := htmlassert.Attr(open, "checked"); !checked {
		t.Error("#flash-detail-open in the stats HTMX fragment is not checked")
	}
}
