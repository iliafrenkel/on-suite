package books_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func attr(t *testing.T, doc *htmlassert.Doc, selector, name string) string {
	t.Helper()
	v, _ := htmlassert.Attr(doc.MustHave(selector), name)
	return v
}

func TestAddPageSearchesOpenLibrary(t *testing.T) {
	s, _ := newServerWithOL(t)
	doc := s.Get(t, s.Alice, "/books/new?q=piranesi")
	if n := len(doc.QueryAll(".books-result")); n != 2 {
		t.Fatalf("%d results, want 2", n)
	}
	if got := htmlassert.Text(doc.MustHave(".books-result-title")); got != "Piranesi" {
		t.Errorf("first result = %q", got)
	}
	if got := attr(t, doc, "img.books-result-cover", "src"); got != "/books/olcover/10226290" {
		t.Errorf("thumbnail src = %q", got)
	}
	if got := attr(t, doc, "a.books-result-use", "aria-label"); got != "Use Piranesi" {
		t.Errorf("Use this aria-label = %q", got)
	}
	use, err := url.Parse(attr(t, doc, "a.books-result-use", "href"))
	if err != nil {
		t.Fatal(err)
	}
	q := use.Query()
	if use.Path != "/books/new" || q.Get("pick") != "1" || q.Get("title") != "Piranesi" ||
		q.Get("authors") != "Susanna Clarke" || q.Get("ol_work") != "OL20893680W" ||
		q.Get("ol_edition") != "OL28300471M" || q.Get("cover") != "10226290" || q.Get("isbn") != "9781526622440" {
		t.Errorf("Use this = %s", use)
	}
	if got := attr(t, doc, "input#books-search-q", "value"); got != "piranesi" {
		t.Errorf("search box = %q", got)
	}
	if got := attr(t, doc, "input#books-title", "value"); got != "" {
		t.Errorf("title = %q; the form stays empty while there are results to pick", got)
	}
}

func TestAFailedSearchFallsBackToTheForm(t *testing.T) {
	s, _ := newServerWithOL(t)
	doc := s.Get(t, s.Alice, "/books/new?q=broken")
	doc.MustHave(".books-search .notice-error")
	if got := attr(t, doc, "input#books-title", "value"); got != "broken" {
		t.Errorf("title = %q, want what was typed", got)
	}

	doc = s.Get(t, s.Alice, "/books/new?q=0-306-40615-2") // finds nothing
	doc.MustHave(".books-search .empty")
	if got := attr(t, doc, "input#books-isbn", "value"); got != "9780306406157" {
		t.Errorf("isbn = %q, want the typed ISBN as ISBN-13", got)
	}
	if got := attr(t, doc, "input#books-title", "value"); got != "" {
		t.Errorf("title = %q, want empty when an ISBN was typed", got)
	}
}

func TestPickingAResultFillsTheForm(t *testing.T) {
	s, _ := newServerWithOL(t)
	pick := url.Values{"pick": {"1"}, "title": {"Piranesi"}, "authors": {"Susanna Clarke"}, "year": {"2020"},
		"pages": {"272"}, "isbn": {"9781526622440"}, "ol_work": {"OL20893680W"}, "ol_edition": {"OL28300471M"},
		"cover": {"10226290"}}
	doc := s.Get(t, s.Alice, "/books/new?"+pick.Encode())
	for id, want := range map[string]string{"books-title": "Piranesi", "books-authors": "Susanna Clarke",
		"books-year": "2020", "books-pages": "272", "books-isbn": "9781526622440"} {
		if got := attr(t, doc, "input#"+id, "value"); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
	if got := htmlassert.Text(doc.MustHave("textarea#books-description")); !strings.HasPrefix(got, "Piranesi's house is no ordinary building.") {
		t.Errorf("description = %q, want Open Library's", got)
	}
	for name, want := range map[string]string{"ol_work": "OL20893680W", "ol_edition": "OL28300471M", "cover_id": "10226290"} {
		if got := attr(t, doc, `input[name="`+name+`"]`, "value"); got != want {
			t.Errorf("hidden %s = %q, want %q", name, got, want)
		}
	}
	if got := attr(t, doc, ".books-picked img", "src"); got != "/books/olcover/10226290" {
		t.Errorf("picked cover = %q", got)
	}

	doc = s.Get(t, s.Alice, "/books/new?pick=1&title=X&ol_work=..%2Fx&cover=12a")
	if attr(t, doc, `input[name="ol_work"]`, "value") != "" || attr(t, doc, `input[name="cover_id"]`, "value") != "" {
		t.Error("junk ids were carried into the form")
	}
	doc.MustNotHave(".books-picked")
}

func TestAddingAPickedBookFetchesItsCover(t *testing.T) {
	s, _ := newServerWithOL(t)
	s.Submit(t, s.Alice, "/books/new", url.Values{"title": {"Piranesi"}, "add_to": {"want"},
		"ol_work": {"OL20893680W"}, "ol_edition": {"OL28300471M"}, "cover_id": {"10226290"}}, "/books/b/1?shelf=want")
	c, err := s.Store.Cover(context.Background(), s.Alice.User.ID, 1)
	if err != nil || c.ContentType != "image/png" {
		t.Errorf("cover = %q, %v; want the fetched PNG", c.ContentType, err)
	}
}

func TestACoverThatWontComeStillAddsTheBook(t *testing.T) {
	s, _ := newServerWithOL(t)
	for i, cover := range []string{"666", "42"} { // HTML posing as a cover; missing
		s.Submit(t, s.Alice, "/books/new", url.Values{"title": {"Book " + cover}, "add_to": {"want"}, "cover_id": {cover}},
			fmt.Sprintf("/books/b/%d?shelf=want", i+1))
		if _, err := s.Store.Cover(context.Background(), s.Alice.User.ID, int64(i+1)); !errors.Is(err, books.ErrNotFound) {
			t.Errorf("cover %s: Cover = %v, want none", cover, err)
		}
	}
}
