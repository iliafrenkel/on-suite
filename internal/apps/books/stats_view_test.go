package books_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// finishedOn adds one of userID's books as read, finished on day.
func finishedOn(t *testing.T, s *server, userID int64, title string, pages int, day string) int64 {
	t.Helper()
	nb := titled(title, "", books.ShelfRead)
	nb.Pages, nb.FinishedOn = pages, day
	return add(t, s, userID, nb)
}

func TestStatsPageShowsTheYear(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	uid := s.Alice.User.ID
	finishedOn(t, s, uid, "Old", 150, "2025-06-01")
	finishedOn(t, s, uid, "Infinite Jest", 1079, "2026-02-10")
	finishedOn(t, s, uid, "Emma", 300, "2026-03-05")
	finishedOn(t, s, uid, "Unpaged", 0, "2026-03-20")

	doc := s.Get(t, s.Alice, "/books/stats")
	if got := htmlassert.Text(doc.MustHave("h1")); got != "Reading stats" {
		t.Errorf("h1 = %q", got)
	}
	doc.MustHave(`a[href="/books/"]`)
	if got := texts(doc, ".books-years a"); !slices.Equal(got, []string{"2025", "2026", "2027"}) {
		t.Errorf("years = %v, want 2025 (the first finish) to 2027 (next year)", got)
	}
	if got := htmlassert.Text(doc.MustHave(`.books-years a[aria-current="page"]`)); got != "2026" {
		t.Errorf("current year = %q, want 2026", got)
	}
	tiles := texts(doc, ".books-stat-tile .value")
	if want := []string{"3", "1,379", "—", "4", "1,529", "—"}; !slices.Equal(tiles, want) {
		t.Errorf("tiles = %v, want %v (2026, then all time)", tiles, want)
	}
	lines := texts(doc, ".books-stat-line")
	want := []string{"Formats Not set 3", "Longest Infinite Jest · 1,079 pages", "Shortest Emma · 300 pages"}
	if !slices.Equal(lines, want) {
		t.Errorf("lines = %q, want %q", lines, want)
	}
	if href, _ := htmlassert.Attr(doc.MustHave(".books-stat-line a"), "href"); !strings.HasPrefix(href, "/books/b/") {
		t.Errorf("longest book links to %q, want the book", href)
	}

	if n := len(doc.QueryAll("#books-year rect")); n != 12 {
		t.Errorf("%d month bars, want 12", n)
	}
	if n := len(doc.QueryAll("#books-all rect")); n != 2 {
		t.Errorf("%d year bars, want 2 (2025 and 2026)", n)
	}
	ticks := texts(doc, ".books-chart-ticks li")
	if len(ticks) != 24 || !slices.Equal(ticks[:3], []string{"Jan", "Feb", "Mar"}) ||
		!slices.Equal(ticks[12:15], []string{"’25", "’26", ""}) {
		t.Errorf("ticks = %q, want the months, then 2025, 2026 and ten empty slots", ticks)
	}
	if got := texts(doc, ".books-chart-figures td"); !slices.Contains(got, "March") || !slices.Contains(got, "2025") {
		t.Errorf("figures = %v, want a row for March and one for 2025", got)
	}
	if label, _ := htmlassert.Attr(doc.MustHave("#books-year svg"), "aria-label"); label != "Books finished by month, 2026, peak 2" {
		t.Errorf("month chart's label = %q", label)
	}
	if title := htmlassert.Text(doc.MustHave("#books-year rect title")); title != "January: 0 books" {
		t.Errorf("first bar's title = %q", title)
	}
}

func TestStatsPageShowsAnotherYear(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	uid := s.Alice.User.ID
	finishedOn(t, s, uid, "Old", 150, "2025-06-01")
	finishedOn(t, s, uid, "Emma", 300, "2026-03-05")

	doc := s.Get(t, s.Alice, "/books/stats?year=2025")
	if got := htmlassert.Text(doc.MustHave(`.books-years a[aria-current="page"]`)); got != "2025" {
		t.Errorf("current year = %q, want 2025", got)
	}
	if got := texts(doc, ".books-stat-tile .value"); got[0] != "1" || got[1] != "150" {
		t.Errorf("2025 tiles = %v, want 1 book, 150 pages", got)
	}
	if got := htmlassert.Text(doc.MustHave("#books-year-head")); got != "2025" {
		t.Errorf("year heading = %q", got)
	}
	// A year before any reading still shows, with the picker reaching back to it.
	doc = s.Get(t, s.Alice, "/books/stats?year=2020")
	if got := texts(doc, ".books-years a"); got[0] != "2020" {
		t.Errorf("years = %v, want them to start at 2020", got)
	}
	doc.MustHave(".books-chart .empty")
}

func TestStatsPageRefusesYearsItCannotShow(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	for _, q := range []string{"abc", "1899", "2028", "2026.5"} {
		rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/books/stats?year="+q, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET /books/stats?year=%s = %d, want 404", q, rec.Code)
		}
	}
	s.Get(t, s.Alice, "/books/stats?year=2027") // next year: its goal can be set ahead
}

func TestStatsPageOnAnEmptyAccount(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	doc := s.Get(t, s.Alice, "/books/stats")
	if got := texts(doc, ".books-years a"); !slices.Equal(got, []string{"2026", "2027"}) {
		t.Errorf("years = %v, want this year and next", got)
	}
	empties := texts(doc, ".books-chart .empty")
	if !slices.Equal(empties, []string{"No books finished in 2026.", "No dated finishes yet."}) {
		t.Errorf("empty charts say %q", empties)
	}
	doc.MustNotHave(".books-stat-line")
	doc.MustNotHave(".books-chart svg")
}

func TestStatsPageIsPerUser(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	finishedOn(t, s, s.Bob.User.ID, "Bob's book", 100, "2026-03-05")
	doc := s.Get(t, s.Alice, "/books/stats")
	if got := texts(doc, ".books-stat-tile .value"); got[0] != "0" {
		t.Errorf("Alice's tiles = %v, want nothing of Bob's", got)
	}
	if strings.Contains(doc.Text(), "Bob's book") {
		t.Error("Alice's stats name Bob's book")
	}
}

func TestStatsPageRequiresSignIn(t *testing.T) {
	s := newServer(t)
	if rec := s.Do(t, nil, httptest.NewRequest("GET", "/books/stats", nil)); rec.Code != http.StatusSeeOther {
		t.Errorf("anonymous GET /books/stats = %d, want a 303 to the login page", rec.Code)
	}
}

func TestSidebarLinksToStats(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/books/")
	doc.MustHave(`.books-side a[href="/books/stats"]`)
}
