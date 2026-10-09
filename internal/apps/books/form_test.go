package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestNewFormOffersTheShelves(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/books/new")
	doc.MustHave(`input[name="title"]`)
	if n := len(doc.QueryAll(`input[name="add_to"]`)); n != 3 {
		t.Errorf("%d add_to choices, want 3 (want, reading, read)", n)
	}
	doc.MustHave(`input[name="finished_on"]`)
	doc.MustHave(`input[name="tags"]`)
}

func TestCreateAddsTheBookAndOpensIt(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	s.Submit(t, s.Alice, "/books/new", url.Values{
		"title": {"Leviathan Wakes"}, "authors": {"James S. A. Corey"}, "year": {"2011"}, "pages": {"592"},
		"series_name": {"The Expanse"}, "series_number": {"1"}, "add_to": {"reading"}, "tags": {"SF, space"},
	}, "/books/b/1?shelf=reading")
	b, err := s.Store.Get(context.Background(), s.Alice.User.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Leviathan Wakes" || b.Pages != 592 || b.SeriesNumber != "1" || b.Shelf != books.ShelfReading {
		t.Errorf("created %+v on %q", b.BookInput, b.Shelf)
	}
	if !slices.Equal(b.Tags, []string{"sf", "space"}) {
		t.Errorf("tags = %v", b.Tags)
	}
}

func TestCreateAlreadyReadUsesTheDate(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	s.Submit(t, s.Alice, "/books/new", url.Values{
		"title": {"Emma"}, "add_to": {"read"}, "finished_on": {"2026-09-01"},
	}, "/books/b/1?shelf=read")
	b, _ := s.Store.Get(context.Background(), s.Alice.User.ID, 1)
	if b.Latest.FinishedOn != "2026-09-01" {
		t.Errorf("finished %q, want 2026-09-01", b.Latest.FinishedOn)
	}
}

func TestCreateWithMistakesComesBackWithMessages(t *testing.T) {
	s := newServer(t)
	rec := s.Post(t, s.Alice, "/books/new", url.Values{
		"title": {""}, "year": {"abc"}, "isbn": {"12345"}, "authors": {"Someone"}, "add_to": {"want"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST = %d, want 422", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	for _, field := range []string{"title", "year", "isbn"} {
		doc.MustHave("#books-" + field + "-error")
	}
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name="year"]`), "value"); v != "abc" {
		t.Errorf("year echoed as %q, want what was typed", v)
	}
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name="authors"]`), "value"); v != "Someone" {
		t.Errorf("authors echoed as %q", v)
	}
	if items, _ := s.Store.List(context.Background(), s.Alice.User.ID, books.ListQuery{}); len(items) != 0 {
		t.Errorf("a rejected form saved %d books", len(items))
	}
}

func TestCreateRefusesAFutureFinishDate(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	rec := s.Post(t, s.Alice, "/books/new", url.Values{
		"title": {"Emma"}, "add_to": {"read"}, "finished_on": {"2027-01-01"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST = %d, want 422", rec.Code)
	}
	htmlassert.Parse(t, rec.Body.String()).MustHave("#books-finished_on-error")
}

func TestEditFormShowsTheBookAndSaves(t *testing.T) {
	s := newServer(t)
	nb := titled("Dune", "Frank Herbert", books.ShelfReading)
	nb.Pages = 412
	id := add(t, s, s.Alice.User.ID, nb)

	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/edit/%d?shelf=all&q=dune", id))
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name="pages"]`), "value"); v != "412" {
		t.Errorf("pages = %q, want 412", v)
	}
	doc.MustNotHave(`input[name="add_to"]`)
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name="q"]`), "value"); v != "dune" {
		t.Errorf("hidden q = %q, want the list context carried", v)
	}

	s.Submit(t, s.Alice, fmt.Sprintf("/books/edit/%d", id), url.Values{
		"title": {"Dune"}, "authors": {"Frank Herbert"}, "pages": {"896"}, "shelf": {"all"}, "q": {"dune"},
	}, fmt.Sprintf("/books/b/%d?q=dune&shelf=all", id))
	if b, _ := s.Store.Get(context.Background(), s.Alice.User.ID, id); b.Pages != 896 {
		t.Errorf("pages = %d after edit, want 896", b.Pages)
	}
}

func TestEditingSomeoneElsesBookIsNotFound(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))
	path := fmt.Sprintf("/books/edit/%d", id)
	if rec := s.Do(t, s.Bob, httptestGet(path)); rec.Code != http.StatusNotFound {
		t.Errorf("Bob GET %s = %d, want 404", path, rec.Code)
	}
	if rec := s.Post(t, s.Bob, path, url.Values{"title": {"Mine"}}); rec.Code != http.StatusNotFound {
		t.Errorf("Bob POST %s = %d, want 404", path, rec.Code)
	}
}

func TestBookPaneLinksToEdit(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=want", id))
	doc.MustHave(fmt.Sprintf(`a[href="/books/edit/%d?shelf=want"]`, id))
}
