package books_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func shelfOf(t *testing.T, s *server, id int64) books.Shelf {
	t.Helper()
	b, err := s.Store.Get(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	return b.Shelf
}

func TestStartMovesABookToReading(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	s.Submit(t, s.Alice, fmt.Sprintf("/books/start/%d", id), url.Values{"shelf": {"want"}},
		fmt.Sprintf("/books/b/%d?shelf=want", id))
	if got := shelfOf(t, s, id); got != books.ShelfReading {
		t.Errorf("shelf = %q, want reading", got)
	}
}

func TestActionsOverHTMXReturnThePanes(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	rec := s.PostHX(t, s.Alice, fmt.Sprintf("/books/start/%d", id), url.Values{"shelf": {"want"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx start = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#books-panes")
	if got := htmlassert.Text(doc.MustHave(".books-book h1")); got != "Emma" {
		t.Errorf("open book = %q, want Emma still open", got)
	}
	if rows := rowTitles(doc); len(rows) != 0 {
		t.Errorf("Want to read still lists %v after starting it", rows)
	}
}

func TestFinishUsesTheGivenDay(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC))
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfReading))
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	s.Submit(t, s.Alice, fmt.Sprintf("/books/finish/%d", id), url.Values{"shelf": {"reading"}, "day": {"2026-10-05"}},
		fmt.Sprintf("/books/b/%d?shelf=reading", id))
	b, _ := s.Store.Get(context.Background(), s.Alice.User.ID, id)
	if b.Shelf != books.ShelfRead || b.Latest.FinishedOn != "2026-10-05" {
		t.Errorf("after finish: shelf %q, latest %+v", b.Shelf, b.Latest)
	}
}

func TestARefusedFinishShowsTheBanner(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC))
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfReading))
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	path := fmt.Sprintf("/books/finish/%d", id)
	form := func() url.Values { return url.Values{"shelf": {"reading"}, "day": {"2026-09-01"}} }

	rec := s.PostHX(t, s.Alice, path, form())
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx refused finish = %d, want 200 so htmx swaps it", rec.Code)
	}
	banner := htmlassert.Text(htmlassert.Parse(t, rec.Body.String()).MustHave(".books-banner"))
	if !strings.Contains(banner, "before you started") {
		t.Errorf("banner = %q", banner)
	}
	if rec := s.Post(t, s.Alice, path, form()); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("refused finish without JavaScript = %d, want 422", rec.Code)
	}
	if got := shelfOf(t, s, id); got != books.ShelfReading {
		t.Errorf("shelf = %q after a refused finish, want reading", got)
	}
}

func TestDidNotFinish(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Infinite Jest", "", books.ShelfReading))
	s.Submit(t, s.Alice, fmt.Sprintf("/books/dnf/%d", id), url.Values{"shelf": {"reading"}},
		fmt.Sprintf("/books/b/%d?shelf=reading", id))
	if got := shelfOf(t, s, id); got != books.ShelfDNF {
		t.Errorf("shelf = %q, want dnf", got)
	}
}

func TestSetTagsFromTheBookPane(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))
	s.Submit(t, s.Alice, fmt.Sprintf("/books/tags/%d", id), url.Values{"shelf": {"want"}, "tags": {"Space, sf"}},
		fmt.Sprintf("/books/b/%d?shelf=want", id))
	tags, err := s.Store.BookTags(context.Background(), s.Alice.User.ID, id)
	if err != nil || !slices.Equal(tags, []string{"sf", "space"}) {
		t.Errorf("tags = %v, %v; want [sf space]", tags, err)
	}
}

func TestDeleteReturnsToTheList(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))
	s.Submit(t, s.Alice, fmt.Sprintf("/books/delete/%d", id), url.Values{"shelf": {"want"}, "q": {"du"}},
		"/books/?q=du&shelf=want")
	if _, err := s.Store.Get(context.Background(), s.Alice.User.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Get after delete = %v, want ErrNotFound", err)
	}
}

func TestActionsOnSomeoneElsesBookAreNotFound(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfReading))
	for _, action := range []string{"start", "finish", "dnf", "tags", "delete"} {
		path := fmt.Sprintf("/books/%s/%d", action, id)
		if rec := s.Post(t, s.Bob, path, url.Values{}); rec.Code != http.StatusNotFound {
			t.Errorf("Bob POST %s = %d, want 404", path, rec.Code)
		}
	}
	if got := shelfOf(t, s, id); got != books.ShelfReading {
		t.Errorf("Bob changed Alice's book: shelf %q", got)
	}
}

func TestBookPaneOffersTheRightReadingActions(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	uid := s.Alice.User.ID
	want := add(t, s, uid, titled("Emma", "", books.ShelfWant))
	reading := add(t, s, uid, titled("Dune", "", books.ShelfReading))
	read := add(t, s, uid, titled("Ulysses", "", books.ShelfRead))

	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", want))
	if got := htmlassert.Text(doc.MustHave(".books-start")); got != "Start reading" {
		t.Errorf("want-to-read button = %q", got)
	}
	doc.MustNotHave("details.books-close")

	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", reading))
	if n := len(doc.QueryAll("details.books-close")); n != 2 {
		t.Errorf("%d close disclosures, want Finish and Did not finish", n)
	}
	day := doc.MustHave(`input[name="day"]`)
	if v, _ := htmlassert.Attr(day, "max"); v != "2026-10-09" {
		t.Errorf("day max = %q, want today 2026-10-09", v)
	}
	if v, _ := htmlassert.Attr(day, "min"); v != "2026-10-09" {
		t.Errorf("day min = %q, want the start 2026-10-09", v)
	}
	doc.MustHave("button[hx-confirm]")
	doc.MustHave("#books-confirm-dialog")

	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", read))
	if got := htmlassert.Text(doc.MustHave(".books-start")); got != "Read again" {
		t.Errorf("read button = %q, want Read again", got)
	}
}
