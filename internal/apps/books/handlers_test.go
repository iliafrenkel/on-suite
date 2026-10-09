package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

type server = apptest.Server[*books.Store]

// newServer mounts ON Books with its own store as the test's handle.
func newServer(t *testing.T) *server {
	t.Helper()
	return apptest.NewServer(t, books.New(), books.NewStore)
}

func TestBooksRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/books/", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /books/ anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

func TestIndexRenders(t *testing.T) {
	s := newServer(t)
	s.Get(t, s.Alice, "/books/") // fails the test unless 200
}

// add creates one of userID's books through the store.
func add(t *testing.T, s *server, userID int64, nb books.NewBook) int64 {
	t.Helper()
	id, err := s.Store.Create(context.Background(), userID, nb)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func titled(title, authors string, shelf books.Shelf) books.NewBook {
	return books.NewBook{BookInput: books.BookInput{Title: title, Authors: authors}, Shelf: shelf}
}

// rowTitles is the list pane's titles, top to bottom.
func rowTitles(doc *htmlassert.Doc) []string {
	var out []string
	for _, n := range doc.QueryAll(".books-row-title") {
		out = append(out, htmlassert.Text(n))
	}
	return out
}

// isChecked says whether the checkbox with this id is checked.
func isChecked(t *testing.T, doc *htmlassert.Doc, id string) bool {
	t.Helper()
	n := doc.MustHave("input#" + id)
	_, ok := htmlassert.Attr(n, "checked")
	return ok
}

// hx performs an htmx GET aimed at target and returns the body.
func hx(t *testing.T, s *server, path, target string) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", target)
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx GET %s = %d; body: %s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestIndexOpensOnTheReadingShelf(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	add(t, s, uid, titled("Dune", "Frank Herbert", books.ShelfReading))
	add(t, s, uid, titled("Emma", "Jane Austen", books.ShelfWant))
	add(t, s, s.Bob.User.ID, titled("Bob's book", "", books.ShelfReading))

	doc := s.Get(t, s.Alice, "/books/")
	if got := rowTitles(doc); len(got) != 1 || got[0] != "Dune" {
		t.Errorf("rows = %v, want [Dune]", got)
	}
	current := doc.MustHave(`.books-side a[aria-current="page"]`)
	if text := htmlassert.Text(current); !strings.HasPrefix(text, "Reading") || !strings.HasSuffix(text, "1") {
		t.Errorf("current shelf link = %q, want Reading with count 1", text)
	}
	doc.MustHave(`a[href="/books/new"]`)
	doc.MustHave(".books-book .empty")
	if !isChecked(t, doc, "books-list-open") || isChecked(t, doc, "books-book-open") {
		t.Error("phone drill-down: want the list showing and no book open")
	}
}

func TestShelfAndFilterQueriesPickTheBooks(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	dispossessed := titled("The Dispossessed", "Ursula K. Le Guin", books.ShelfWant)
	dispossessed.Tags = []string{"sf"}
	add(t, s, uid, dispossessed)
	add(t, s, uid, titled("Emma", "Jane Austen", books.ShelfWant))
	add(t, s, uid, titled("A Wizard of Earthsea", "Ursula K. Le Guin", books.ShelfRead))

	tests := []struct {
		path string
		want string
	}{
		{"/books/?shelf=want", "Emma|The Dispossessed"},
		{"/books/?shelf=all&q=le+guin", "A Wizard of Earthsea|The Dispossessed"},
		{"/books/?shelf=all&tag=sf", "The Dispossessed"},
		{"/books/?shelf=nonsense", ""}, // unknown shelf → Reading, which is empty
	}
	for _, tt := range tests {
		got := rowTitles(s.Get(t, s.Alice, tt.path))
		slices.Sort(got)
		if strings.Join(got, "|") != tt.want {
			t.Errorf("%s rows = %v, want %s", tt.path, got, tt.want)
		}
	}
	doc := s.Get(t, s.Alice, "/books/?shelf=all&tag=sf")
	if text := htmlassert.Text(doc.MustHave(`.books-side a[aria-current="page"]`)); text != "sf" {
		t.Errorf("current sidebar link = %q, want the sf tag", text)
	}
	if v, _ := htmlassert.Attr(doc.MustHave("input#books-q"), "value"); v != "" {
		t.Errorf("filter box = %q, want empty", v)
	}
}

func TestEmptyShelfSaysSo(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/books/?shelf=dnf")
	doc.MustHave(".books-list .empty")
	doc.MustNotHave(".books-row")
}

func TestBookPaneShowsTheBook(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	nb := titled("Leviathan Wakes", "James S. A. Corey", books.ShelfReading)
	nb.SeriesName, nb.SeriesNumber, nb.Year, nb.Pages, nb.ISBN = "The Expanse", "1", 2011, 592, "0306406152"
	nb.Tags = []string{"sf"}
	id := add(t, s, s.Alice.User.ID, nb)

	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=reading", id))
	book := doc.MustHave("#books-book")
	if got := htmlassert.Text(doc.MustHave(".books-book h1")); got != "Leviathan Wakes" {
		t.Errorf("h1 = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-series")); got != "The Expanse #1" {
		t.Errorf("series = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-facts")); got != "2011 · 592 pages · ISBN 9780306406157" {
		t.Errorf("facts = %q", got)
	}
	if v, _ := htmlassert.Attr(book, "data-book-id"); v != fmt.Sprint(id) {
		t.Errorf("data-book-id = %q, want %d", v, id)
	}
	if !isChecked(t, doc, "books-book-open") {
		t.Error("books-book-open not checked with a book open")
	}
	doc.MustHave(".books-rows .is-active")
	if title := htmlassert.Text(doc.MustHave("title")); !strings.HasPrefix(title, "Leviathan Wakes") {
		t.Errorf("<title> = %q, want the book's title first", title)
	}
}

func TestSomeoneElsesBookIsNotFound(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))
	for _, path := range []string{fmt.Sprintf("/books/b/%d", id), "/books/b/999", "/books/b/x"} {
		if rec := s.Do(t, s.Bob, httptest.NewRequest("GET", path, nil)); rec.Code != http.StatusNotFound {
			t.Errorf("Bob GET %s = %d, want 404", path, rec.Code)
		}
	}
}

func TestHTMXListNavigationSwapsTheListOnly(t *testing.T) {
	s := newServer(t)
	add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	body := hx(t, s, "/books/?shelf=want", "books-list")
	doc := htmlassert.Parse(t, body)
	if got := rowTitles(doc); len(got) != 1 || got[0] != "Emma" {
		t.Errorf("rows = %v, want [Emma]", got)
	}
	for _, id := range []string{"books-side", "books-filter-ctx", "books-list-open", "books-book-open", "shell-crumb-tail"} {
		if v, ok := htmlassert.Attr(doc.MustHave("#"+id), "hx-swap-oob"); !ok || v != "true" {
			t.Errorf("#%s hx-swap-oob = %q, %v; want it swapped out of band", id, v, ok)
		}
	}
	doc.MustNotHave("#books-book")
	doc.MustNotHave("#books-panes")
}

func TestHTMXBookOpenSwapsTheBookOnly(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	doc := htmlassert.Parse(t, hx(t, s, fmt.Sprintf("/books/b/%d?shelf=want", id), "books-book"))
	doc.MustHave("#books-book h1")
	if !isChecked(t, doc, "books-book-open") {
		t.Error("book-swap did not check books-book-open out of band")
	}
	doc.MustNotHave("#books-list")
	doc.MustNotHave("#books-side")
}
