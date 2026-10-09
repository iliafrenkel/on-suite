package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// readTwiceOverHTTP is a book Alice read in October (paper, 1st to 2nd)
// and has been reading again since the 5th; today is the 9th.
func readTwiceOverHTTP(t *testing.T, s *server) int64 {
	t.Helper()
	ctx := context.Background()
	uid := s.Alice.User.ID
	s.Clock.Set(time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC))
	id := readingBook(t, s, "Dune", 600)
	if err := s.Store.SetFormat(ctx, uid, id, "paper"); err != nil {
		t.Fatal(err)
	}
	s.Clock.Set(time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC))
	if err := s.Store.FinishReading(ctx, uid, id, "2026-10-02", 0); err != nil {
		t.Fatal(err)
	}
	s.Clock.Set(time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC))
	if err := s.Store.StartReading(ctx, uid, id); err != nil {
		t.Fatal(err)
	}
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	return id
}

func historyIDs(t *testing.T, s *server, id int64) []int64 {
	t.Helper()
	rs, err := s.Store.Readings(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	var out []int64
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}

func TestTheBookPaneListsEveryReading(t *testing.T) {
	s := newServer(t)
	id := readTwiceOverHTTP(t, s)
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", id))
	var lines []string
	for _, n := range doc.QueryAll(".books-history-line") {
		lines = append(lines, htmlassert.Text(n))
	}
	want := []string{"Reading From 5 Oct 2026 · Paper", "Read 1 Oct 2026 – 2 Oct 2026 · Paper"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Errorf("history = %q, want %q", lines, want)
	}
	if n := len(doc.QueryAll(`.books-history-item input[name="finished_on"]`)); n != 1 {
		t.Errorf("%d finish-date fields, want 1: the reading in progress has none", n)
	}
	if n := len(doc.QueryAll(".books-history-item button[hx-confirm]")); n != 2 {
		t.Errorf("%d delete buttons, want one per reading", n)
	}

	want0 := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", want0)).MustNotHave(".books-history")
}

func TestEditingAReading(t *testing.T) {
	s := newServer(t)
	id := readTwiceOverHTTP(t, s)
	past := historyIDs(t, s, id)[1]
	path := fmt.Sprintf("/books/readings/%d/%d", id, past)
	s.Submit(t, s.Alice, path, url.Values{"shelf": {"read"}, "started_on": {"2026-09-20"}, "finished_on": {"2026-09-28"}, "format": {"ebook"}},
		fmt.Sprintf("/books/b/%d?shelf=read", id))
	rs, _ := s.Store.Readings(context.Background(), s.Alice.User.ID, id)
	if got := rs[1]; got.StartedOn != "2026-09-20" || got.FinishedOn != "2026-09-28" || got.Format != "ebook" {
		t.Errorf("edited reading = %+v", got)
	}

	form := url.Values{"shelf": {"read"}, "started_on": {"2026-09-28"}, "finished_on": {"2026-09-20"}}
	if rec := s.Post(t, s.Alice, path, form); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("finish before start = %d, want 422", rec.Code)
	}
	rec := s.PostHX(t, s.Alice, path, url.Values{"shelf": {"read"}, "started_on": {"2026-09-28"}, "finished_on": {"2026-09-20"}})
	if got := htmlassert.Text(htmlassert.Parse(t, rec.Body.String()).MustHave(".books-banner")); got != "The finish date is before the start." {
		t.Errorf("banner = %q", got)
	}
}

func TestDeletingAReading(t *testing.T) {
	s := newServer(t)
	id := readTwiceOverHTTP(t, s)
	current := historyIDs(t, s, id)[0]
	s.Submit(t, s.Alice, fmt.Sprintf("/books/readings/%d/%d/delete", id, current), url.Values{"shelf": {"reading"}},
		fmt.Sprintf("/books/b/%d?shelf=reading", id))
	if got := shelfOf(t, s, id); got != books.ShelfRead {
		t.Errorf("shelf after deleting the re-read = %q, want read", got)
	}
	if n := len(historyIDs(t, s, id)); n != 1 {
		t.Errorf("%d readings left, want 1", n)
	}
}

func TestReadingRoutesAreNotFoundForOthers(t *testing.T) {
	s := newServer(t)
	id := readTwiceOverHTTP(t, s)
	rid := historyIDs(t, s, id)[0]
	other := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	for _, tt := range []struct {
		sess string
		path string
	}{
		{"bob", fmt.Sprintf("/books/readings/%d/%d", id, rid)},
		{"bob", fmt.Sprintf("/books/readings/%d/%d/delete", id, rid)},
		{"alice", fmt.Sprintf("/books/readings/%d/%d/delete", other, rid)},
		{"alice", fmt.Sprintf("/books/readings/%d/x", id)},
	} {
		sess := s.Alice
		if tt.sess == "bob" {
			sess = s.Bob
		}
		if rec := s.Post(t, sess, tt.path, url.Values{}); rec.Code != http.StatusNotFound {
			t.Errorf("%s POST %s = %d, want 404", tt.sess, tt.path, rec.Code)
		}
	}
	if n := len(historyIDs(t, s, id)); n != 2 {
		t.Errorf("%d readings, want both still there", n)
	}
}
