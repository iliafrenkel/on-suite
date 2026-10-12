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

func TestBookPageLinksToEdit(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=want", id))
	doc.MustHave(fmt.Sprintf(`a[href="/books/edit/%d?shelf=want"]`, id))
}

func TestEditSavesTags(t *testing.T) {
	s := newServer(t)
	nb := titled("Dune", "", books.ShelfWant)
	nb.Tags = []string{"sf"}
	id := add(t, s, s.Alice.User.ID, nb)
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/edit/%d?shelf=want", id))
	if got := attr(t, doc, "input#books-tags", "value"); got != "sf" {
		t.Errorf("tags box = %q, want the book's tags", got)
	}
	edit := func(tags string) []string {
		t.Helper()
		s.Submit(t, s.Alice, fmt.Sprintf("/books/edit/%d", id), url.Values{"title": {"Dune"}, "shelf": {"want"}, "tags": {tags}},
			fmt.Sprintf("/books/b/%d?shelf=want", id))
		got, err := s.Store.BookTags(context.Background(), s.Alice.User.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := edit("Space, sf, classics"); !slices.Equal(got, []string{"classics", "sf", "space"}) {
		t.Errorf("tags = %v, want [classics sf space]", got)
	}
	if got := edit("  "); len(got) != 0 {
		t.Errorf("tags after clearing the box = %v, want none", got)
	}
	if names, _ := s.Store.TagNames(context.Background(), s.Alice.User.ID); len(names) != 0 {
		t.Errorf("tag names = %v, want none: a tag nobody uses goes", names)
	}
}

// A refused edit comes back with what was typed, tags included, and the
// page bar still goes back to the book under its saved title.
func TestARefusedEditKeepsTheTagsAndTheWayBack(t *testing.T) {
	s := newServer(t)
	nb := titled("Dune", "", books.ShelfWant)
	nb.Tags = []string{"sf"}
	id := add(t, s, s.Alice.User.ID, nb)
	rec := s.Post(t, s.Alice, fmt.Sprintf("/books/edit/%d", id), url.Values{
		"title": {""}, "shelf": {"want"}, "q": {"du"}, "tags": {"sf, desert"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("edit without a title = %d, want 422", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := attr(t, doc, "input#books-tags", "value"); got != "sf, desert" {
		t.Errorf("tags box = %q after the 422, want what was typed", got)
	}
	back := doc.MustHave(".books-page-bar .books-back")
	if href, _ := htmlassert.Attr(back, "href"); href != fmt.Sprintf("/books/b/%d?q=du&shelf=want", id) {
		t.Errorf("back link after the 422 = %q, want the book page", href)
	}
	if got := htmlassert.Text(back); got != "Dune" {
		t.Errorf("back link text after the 422 = %q, want the saved title, not the empty box", got)
	}
}

func TestEditLeavesTheShelfAlone(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfReading))
	s.Submit(t, s.Alice, fmt.Sprintf("/books/edit/%d", id), url.Values{
		"title": {"Dune, revised"}, "shelf": {"reading"}, "tags": {"sf"},
		"add_to": {"read"}, // a tampered form: the edit form has no such field
	}, fmt.Sprintf("/books/b/%d?shelf=reading", id))
	b, err := s.Store.Get(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Dune, revised" || !slices.Equal(b.Tags, []string{"sf"}) {
		t.Fatalf("title %q, tags %v; the edit was not saved", b.Title, b.Tags)
	}
	if b.Shelf != books.ShelfReading {
		t.Errorf("shelf = %q, want reading unchanged", b.Shelf)
	}
}

func TestAddAndEditHaveAPageBar(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))

	doc := s.Get(t, s.Alice, "/books/new?shelf=all&tag=sf")
	back := doc.MustHave(".books-page-bar .books-back")
	if href, _ := htmlassert.Attr(back, "href"); href != "/books/?shelf=all&tag=sf" {
		t.Errorf("Add's back link = %q, want the list it came from", href)
	}
	if got := htmlassert.Text(back); got != "Tagged “sf”" {
		t.Errorf("Add's back link text = %q, want the list's heading", got)
	}
	if got := attr(t, doc, ".books-form-actions a", "href"); got != "/books/?shelf=all&tag=sf" {
		t.Errorf("Add's Cancel = %q, want the same place as the back link", got)
	}
	doc.MustNotHave(".books-page-actions") // no right-hand actions

	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/edit/%d?shelf=want&q=du", id))
	back = doc.MustHave(".books-page-bar .books-back")
	want := fmt.Sprintf("/books/b/%d?q=du&shelf=want", id)
	if href, _ := htmlassert.Attr(back, "href"); href != want {
		t.Errorf("Edit's back link = %q, want the book page %q", href, want)
	}
	if got := htmlassert.Text(back); got != "Dune" {
		t.Errorf("Edit's back link text = %q, want the book's title", got)
	}
	if got := attr(t, doc, ".books-form-actions a", "href"); got != want {
		t.Errorf("Edit's Cancel = %q, want %q", got, want)
	}
	doc.MustNotHave(".books-page-actions")
}

func TestAddKeepsItsListButNotItsSearch(t *testing.T) {
	s, _ := newServerWithOL(t)
	// The toolbar's Add link leaves the list's search behind.
	doc := s.Get(t, s.Alice, "/books/?shelf=all&tag=sf&q=dune")
	if got := attr(t, doc, "a#books-add", "href"); got != "/books/new?shelf=all&tag=sf" {
		t.Errorf("Add link = %q, want the tag list without its search", got)
	}
	// The Open Library search and a pick keep the list.
	doc = s.Get(t, s.Alice, "/books/new?shelf=all&tag=sf&q=piranesi")
	if got := attr(t, doc, `.books-search-form input[name="tag"]`, "value"); got != "sf" {
		t.Errorf("search form's tag = %q, want the list carried", got)
	}
	// Its only q is its own box: a hidden one from the list would post two.
	if qs := doc.QueryAll(`.books-search-form input[name="q"]`); len(qs) != 1 {
		t.Errorf("search form has %d q fields, want only its search box", len(qs))
	} else if id, _ := htmlassert.Attr(qs[0], "id"); id != "books-search-q" {
		t.Errorf("search form's q is #%s, want the search box", id)
	}
	use, err := url.Parse(attr(t, doc, "a.books-result-use", "href"))
	if err != nil {
		t.Fatal(err)
	}
	if q := use.Query(); q.Get("shelf") != "all" || q.Get("tag") != "sf" || q.Has("q") {
		t.Errorf("Use this = %s, want the list's shelf and tag and no q", use)
	}
	doc = s.Get(t, s.Alice, use.String())
	if got := attr(t, doc, ".books-back", "href"); got != "/books/?shelf=all&tag=sf" {
		t.Errorf("back link after a pick = %q", got)
	}
	doc.MustNotHave(`form.books-form input[name="q"]`) // the list's search never posts with the book
	if got := attr(t, doc, `form.books-form input[name="tag"]`, "value"); got != "sf" {
		t.Errorf("Add form's tag field = %q, want the list carried", got)
	}
	// A refused add comes back to the same list.
	rec := s.Post(t, s.Alice, "/books/new", url.Values{"title": {""}, "add_to": {"want"}, "shelf": {"all"}, "tag": {"sf"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("refused add = %d, want 422", rec.Code)
	}
	if got := attr(t, htmlassert.Parse(t, rec.Body.String()), ".books-back", "href"); got != "/books/?shelf=all&tag=sf" {
		t.Errorf("back link after a refused add = %q", got)
	}
}
