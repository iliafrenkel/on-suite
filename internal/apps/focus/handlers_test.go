package focus_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

type server = apptest.Server[*focus.Store]

// newServer mounts ON Focus with its own store as the test's handle.
func newServer(t *testing.T) *server {
	t.Helper()
	return apptest.NewServer(t, focus.New(), focus.NewStore)
}

func TestFocusRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/focus/", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /focus/ anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

func TestIndexShowsTheEmptyState(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/focus/")
	doc.MustHave(".focus-page")
	doc.MustHave(".focus-empty")
	doc.MustHave(`a[href="/focus/new"]`)
}

func seedTimer(t *testing.T, s *server, userID int64, in focus.TimerInput) focus.Timer {
	t.Helper()
	tm, err := s.Store.CreateTimer(context.Background(), userID, in)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func single(name string, minutes int) focus.TimerInput {
	in := focus.DefaultInput()
	in.Name, in.FocusMinutes = name, minutes
	return in
}

func TestIndexShowsTilesInOrder(t *testing.T) {
	s := newServer(t)
	deep := validIntervals() // Deep work, blue, 50/10 × 4, long 30 every 2
	seedTimer(t, s, s.Alice.User.ID, single("Daily Reflection", 15))
	deepTimer := seedTimer(t, s, s.Alice.User.ID, deep)
	seedTimer(t, s, s.Bob.User.ID, single("Bob's timer", 5))

	doc := s.Get(t, s.Alice, "/focus/")
	doc.MustNotHave(".focus-empty")
	tiles := doc.QueryAll(".focus-tile")
	if len(tiles) != 2 {
		t.Fatalf("got %d tiles, want 2", len(tiles))
	}
	var gotNames []string
	for _, n := range doc.QueryAll(".focus-tile-name") {
		gotNames = append(gotNames, htmlassert.Text(n))
	}
	if strings.Join(gotNames, "|") != "Daily Reflection|Deep work" {
		t.Errorf("tile names = %v", gotNames)
	}
	if class, _ := htmlassert.Attr(tiles[1], "class"); !strings.Contains(class, "swatch-c-blue") {
		t.Errorf("Deep work tile class = %q, want swatch-c-blue", class)
	}
	summaries := doc.QueryAll(".focus-tile-summary")
	if got := htmlassert.Text(summaries[1]); got != "50 / 10 × 4 · long 30" {
		t.Errorf("summary = %q", got)
	}
	pills := doc.QueryAll(".focus-pill")
	if got := htmlassert.Text(pills[0]); got != "15 min" {
		t.Errorf("single pill = %q, want 15 min", got)
	}
	if got := htmlassert.Text(pills[1]); got != "4h 10m" { // 4×50 + 10 + 30 + 10
		t.Errorf("intervals pill = %q, want 4h 10m", got)
	}
	doc.MustHave(`a[href="/focus/run/` + itoa(deepTimer.ID) + `"]`)
}

func TestDuplicateFromTheHomePage(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
	s.Submit(t, s.Alice, "/focus/timers/"+itoa(tm.ID)+"/duplicate", url.Values{}, "/focus/")
	ts, err := s.Store.Timers(context.Background(), s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 2 || ts[1].Name != "Reading (copy)" {
		t.Errorf("after duplicate: %v", names(ts))
	}
}

func TestDeleteFromTheHomePage(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
	s.Submit(t, s.Alice, "/focus/timers/"+itoa(tm.ID)+"/delete", url.Values{}, "/focus/")
	ts, err := s.Store.Timers(context.Background(), s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 0 {
		t.Errorf("after delete: %v", names(ts))
	}
}

func TestDuplicateAndDeleteAreNotFoundForSomeoneElse(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Mine", 30))
	for _, action := range []string{"duplicate", "delete"} {
		rec := s.Post(t, s.Bob, "/focus/timers/"+itoa(tm.ID)+"/"+action, url.Values{})
		if rec.Code != http.StatusNotFound {
			t.Errorf("bob %s = %d, want 404", action, rec.Code)
		}
	}
	if rec := s.Post(t, s.Alice, "/focus/timers/abc/delete", url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("non-numeric id = %d, want 404", rec.Code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
