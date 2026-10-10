package books_test

import (
	"context"
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

// count is one number out of the database.
func count(t *testing.T, f *fixture, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// #569: a refused Create must leave no book, reading or tag behind.
func TestRefusedCreatesWriteNothing(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		name string
		nb   books.NewBook
	}{
		{"no title", books.NewBook{BookInput: books.BookInput{Title: " "}, Shelf: books.ShelfReading, Tags: []string{"sf"}}},
		{"future finish date", books.NewBook{BookInput: books.BookInput{Title: "Emma"}, Shelf: books.ShelfRead, FinishedOn: "2026-10-11", Tags: []string{"sf"}}},
		{"impossible finish date", books.NewBook{BookInput: books.BookInput{Title: "Emma"}, Shelf: books.ShelfRead, FinishedOn: "2026-02-30", Tags: []string{"sf"}}},
		{"a shelf you can't add to", books.NewBook{BookInput: books.BookInput{Title: "Emma"}, Shelf: books.ShelfDNF, Tags: []string{"sf"}}},
		{"an unknown shelf", books.NewBook{BookInput: books.BookInput{Title: "Emma"}, Shelf: "lent", Tags: []string{"sf"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := f.store.Create(context.Background(), f.alice.ID, tt.nb); err == nil {
				t.Fatal("Create succeeded, want a refusal")
			}
			for _, table := range []string{"books_books", "books_readings", "books_tags", "books_book_tags"} {
				if n := count(t, f, `SELECT count(*) FROM `+table); n != 0 {
					t.Errorf("%s has %d rows after a refused Create, want 0", table, n)
				}
			}
		})
	}
}

func TestTwoUsersCanUseTheSameTagName(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mine := onShelf("Dune", books.ShelfWant)
	mine.Tags = []string{"sf"}
	theirs := onShelf("Hyperion", books.ShelfWant)
	theirs.Tags = []string{"sf"}
	aliceBook := addBook(t, f, f.alice.ID, mine)
	bobBook := addBook(t, f, f.bob.ID, theirs)

	for _, u := range []struct {
		name string
		id   int64
		book int64
	}{{"alice", f.alice.ID, aliceBook}, {"bob", f.bob.ID, bobBook}} {
		got, err := f.store.BookTags(ctx, u.id, u.book)
		if err != nil || !slices.Equal(got, []string{"sf"}) {
			t.Errorf("%s's book tags = %v, %v; want [sf]", u.name, got, err)
		}
	}
	if n := count(t, f, `SELECT count(*) FROM books_tags WHERE name = 'sf'`); n != 2 {
		t.Errorf("%d tag rows named sf, want one per user", n)
	}
	// Dropping the tag from Alice's book must not take Bob's with it.
	if err := f.store.SetTags(ctx, f.alice.ID, aliceBook, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.store.BookTags(ctx, f.bob.ID, bobBook); !slices.Equal(got, []string{"sf"}) {
		t.Errorf("Bob's tags = %v after Alice dropped hers, want [sf]", got)
	}
	if got, _ := f.store.TagNames(ctx, f.alice.ID); len(got) != 0 {
		t.Errorf("Alice still has tags %v", got)
	}
}

func TestSetTagsBumpsUpdatedAt(t *testing.T) {
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfWant))
	f.now = f.now.Add(2 * time.Hour)
	if err := f.store.SetTags(context.Background(), f.alice.ID, id, []string{"sf"}); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); !b.UpdatedAt.Equal(f.now) {
		t.Errorf("updated_at = %v after SetTags, want %v", b.UpdatedAt, f.now)
	}
}

func TestReadShelfListsUndatedFinishesLast(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	undated := addBook(t, f, f.alice.ID, onShelf("Undated", books.ShelfRead))
	f.now = f.now.Add(time.Hour)
	older := onShelf("Older", books.ShelfRead)
	older.FinishedOn = "2026-01-05"
	addBook(t, f, f.alice.ID, older)
	newer := onShelf("Newer", books.ShelfRead)
	newer.FinishedOn = "2026-03-05"
	addBook(t, f, f.alice.ID, newer)
	// The Goodreads import can leave a finished reading with no date.
	if _, err := f.db.Exec(`UPDATE books_readings SET finished_on = NULL WHERE book_id = ?`, undated); err != nil {
		t.Fatal(err)
	}
	items, err := f.store.List(ctx, f.alice.ID, books.ListQuery{Shelf: books.ShelfRead})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := titles(items), []string{"Newer", "Older", "Undated"}; !slices.Equal(got, want) {
		t.Errorf("Read shelf = %v, want %v (undated last)", got, want)
	}
}

func TestDNFUsesTheGivenDay(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC))
	id := add(t, s, s.Alice.User.ID, titled("Infinite Jest", "", books.ShelfReading))
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	s.Submit(t, s.Alice, fmt.Sprintf("/books/dnf/%d", id), url.Values{"shelf": {"reading"}, "day": {"2026-10-05"}},
		fmt.Sprintf("/books/b/%d?shelf=reading", id))
	b, err := s.Store.Get(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if b.Shelf != books.ShelfDNF || b.Latest.FinishedOn != "2026-10-05" {
		t.Errorf("shelf %q, finished %q; want dnf on 2026-10-05", b.Shelf, b.Latest.FinishedOn)
	}
	// A day before the reading started, or in the future, is refused and changes nothing.
	again := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfReading))
	for _, day := range []string{"2026-09-30", "2026-10-11"} {
		rec := s.PostHX(t, s.Alice, fmt.Sprintf("/books/dnf/%d", again), url.Values{"shelf": {"reading"}, "day": {day}})
		if rec.Code != http.StatusOK {
			t.Fatalf("DNF on %s = %d, want the pane back with a banner", day, rec.Code)
		}
		htmlassert.Parse(t, rec.Body.String()).MustHave(".books-banner")
		if got := shelfOf(t, s, again); got != books.ShelfReading {
			t.Errorf("DNF on %s moved the book to %q", day, got)
		}
	}
}

func TestSetTagsOverHTMX(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))
	rec := s.PostHX(t, s.Alice, fmt.Sprintf("/books/tags/%d", id), url.Values{"shelf": {"want"}, "tags": {"Space, sf"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx tags = %d, want 200", rec.Code)
	}
	if got, want := rec.Header().Get("HX-Replace-Url"), fmt.Sprintf("/books/b/%d?shelf=want", id); got != want {
		t.Errorf("HX-Replace-Url = %q, want %q", got, want)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#books-panes")
	if got := attr(t, doc, "#books-tags-input", "value"); got != "sf, space" {
		t.Errorf("tags box = %q, want the saved tags", got)
	}
	sidebar := strings.Join(texts(doc, ".books-side a"), "|")
	if !strings.Contains(sidebar, "sf") || !strings.Contains(sidebar, "space") {
		t.Errorf("sidebar = %q, want the new tags listed", sidebar)
	}
	if tags, _ := s.Store.BookTags(context.Background(), s.Alice.User.ID, id); !slices.Equal(tags, []string{"sf", "space"}) {
		t.Errorf("stored tags = %v", tags)
	}
}

func TestAnInvalidEditOfSomeoneElsesBookIsStillNotFound(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))
	// No title and a bad year: the form would bounce with a 422 for the owner.
	rec := s.Post(t, s.Bob, fmt.Sprintf("/books/edit/%d", id), url.Values{"title": {""}, "year": {"abc"}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("Bob's invalid edit = %d, want 404 (not a 422 that confirms the book exists)", rec.Code)
	}
	rec = s.Post(t, s.Alice, fmt.Sprintf("/books/edit/%d", id), url.Values{"title": {""}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("Alice's invalid edit = %d, want 422", rec.Code)
	}
}

func TestEditLeavesTagsAndShelfAlone(t *testing.T) {
	s := newServer(t)
	nb := titled("Dune", "", books.ShelfReading)
	nb.Tags = []string{"sf", "classics"}
	id := add(t, s, s.Alice.User.ID, nb)
	s.Submit(t, s.Alice, fmt.Sprintf("/books/edit/%d", id), url.Values{
		"title": {"Dune, revised"}, "shelf": {"reading"},
		// A tampered form: the edit form has no such fields.
		"tags": {"other"}, "add_to": {"read"},
	}, fmt.Sprintf("/books/b/%d?shelf=reading", id))
	b, err := s.Store.Get(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Dune, revised" {
		t.Fatalf("title = %q, the edit was not saved", b.Title)
	}
	if !slices.Equal(b.Tags, []string{"classics", "sf"}) || b.Shelf != books.ShelfReading {
		t.Errorf("tags %v, shelf %q; want [classics sf] and reading unchanged", b.Tags, b.Shelf)
	}
}

func TestCreateWithABadAddToComesBack(t *testing.T) {
	s := newServer(t)
	for _, addTo := range []string{"lent", "dnf", "all", ""} {
		rec := s.Post(t, s.Alice, "/books/new", url.Values{"title": {"Emma"}, "add_to": {addTo}})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("add_to %q = %d, want 422", addTo, rec.Code)
			continue
		}
		doc := htmlassert.Parse(t, rec.Body.String())
		if got := htmlassert.Text(doc.MustHave("#books-add_to-error")); got == "" {
			t.Errorf("add_to %q: empty message", addTo)
		}
		if got := attr(t, doc, `input[name="title"]`, "value"); got != "Emma" {
			t.Errorf("add_to %q: title echoed as %q", addTo, got)
		}
	}
	if items, _ := s.Store.List(context.Background(), s.Alice.User.ID, books.ListQuery{}); len(items) != 0 {
		t.Errorf("a refused form saved %d books", len(items))
	}
}

// Limits are inclusive: exactly the maximum passes, one more fails.
func TestFieldLimitsBoundaryTable(t *testing.T) {
	long := func(n int) string { return strings.Repeat("é", n) } // runes, not bytes
	tests := []struct {
		name  string
		in    books.BookInput
		field string
	}{
		{"title", books.BookInput{Title: long(books.MaxTitleRunes)}, "title"},
		{"subtitle", books.BookInput{Title: "x", Subtitle: long(books.MaxTitleRunes)}, "subtitle"},
		{"authors", books.BookInput{Title: "x", Authors: long(books.MaxAuthorsRunes)}, "authors"},
		{"series name", books.BookInput{Title: "x", SeriesName: long(books.MaxSeriesRunes)}, "series_name"},
		{"series number", books.BookInput{Title: "x", SeriesName: "s", SeriesNumber: long(books.MaxSeriesNumberRunes)}, "series_number"},
		{"description", books.BookInput{Title: "x", Description: long(books.MaxDescriptionRunes)}, "description"},
		{"year", books.BookInput{Title: "x", Year: books.MaxYear}, "year"},
		{"pages", books.BookInput{Title: "x", Pages: books.MaxPages}, "pages"},
	}
	over := map[string]books.BookInput{
		"title":         {Title: long(books.MaxTitleRunes + 1)},
		"subtitle":      {Title: "x", Subtitle: long(books.MaxTitleRunes + 1)},
		"authors":       {Title: "x", Authors: long(books.MaxAuthorsRunes + 1)},
		"series name":   {Title: "x", SeriesName: long(books.MaxSeriesRunes + 1)},
		"series number": {Title: "x", SeriesName: "s", SeriesNumber: long(books.MaxSeriesNumberRunes + 1)},
		"description":   {Title: "x", Description: long(books.MaxDescriptionRunes + 1)},
		"year":          {Title: "x", Year: books.MaxYear + 1},
		"pages":         {Title: "x", Pages: books.MaxPages + 1},
	}
	if books.MaxTitleRunes != 300 || books.MaxYear != 9999 || books.MaxPages != 100000 {
		t.Fatalf("limits changed (%d, %d, %d); the issue's boundaries are 300, 9999, 100000",
			books.MaxTitleRunes, books.MaxYear, books.MaxPages)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if errs := tt.in.Normalize().Validate(); errs != nil {
				t.Errorf("at the limit: Validate() = %v, want nil", errs)
			}
			if errs := over[tt.name].Normalize().Validate(); errs[tt.field] == "" {
				t.Errorf("one over the limit: Validate() = %v, want a message for %q", errs, tt.field)
			}
		})
	}
}

// The same boundaries through the form: year 9999 and 100000 pages save;
// one more, or zero, is a 422 with the message on the field.
func TestFormAcceptsTheLimitsAndRefusesOneMore(t *testing.T) {
	s := newServer(t)
	s.Submit(t, s.Alice, "/books/new", url.Values{
		"title": {strings.Repeat("t", books.MaxTitleRunes)}, "year": {"9999"}, "pages": {"100000"}, "add_to": {"want"},
	}, "/books/b/1?shelf=want")
	b, _ := s.Store.Get(context.Background(), s.Alice.User.ID, 1)
	if b.Year != 9999 || b.Pages != 100000 || len(b.Title) != books.MaxTitleRunes {
		t.Errorf("saved year %d, pages %d, title of %d chars", b.Year, b.Pages, len(b.Title))
	}
	for field, value := range map[string]string{"year": "10000", "pages": "100001", "title": strings.Repeat("t", books.MaxTitleRunes+1)} {
		form := url.Values{"title": {"Emma"}, "add_to": {"want"}}
		form.Set(field, value)
		rec := s.Post(t, s.Alice, "/books/new", form)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s=%.12s... = %d, want 422", field, value, rec.Code)
			continue
		}
		htmlassert.Parse(t, rec.Body.String()).MustHave("#books-" + field + "-error")
	}
}
