package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func starValues(doc *htmlassert.Doc) []string {
	var out []string
	for _, b := range doc.QueryAll(".books-star") {
		v, _ := htmlassert.Attr(b, "value")
		out = append(out, v)
	}
	return out
}

func TestStarsSetAndClearTheRating(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfRead))
	page := fmt.Sprintf("/books/b/%d?shelf=read", id)
	doc := s.Get(t, s.Alice, page)
	if got := fmt.Sprint(starValues(doc)); got != "[1 2 3 4 5]" {
		t.Errorf("unrated star values = %s", got)
	}
	doc.MustNotHave(".is-on")

	s.Submit(t, s.Alice, fmt.Sprintf("/books/rating/%d", id), url.Values{"shelf": {"read"}, "rating": {"4"}}, page)
	doc = s.Get(t, s.Alice, page)
	if got := fmt.Sprint(starValues(doc)); got != "[1 2 3 0 5]" {
		t.Errorf("rated-4 star values = %s, want the 4th to clear", got)
	}
	if n := len(doc.QueryAll(".is-on")); n != 4 {
		t.Errorf("%d stars on, want 4", n)
	}
	if v, _ := htmlassert.Attr(doc.QueryAll(".books-star")[3], "aria-label"); v != "Clear the rating (4 of 5)" {
		t.Errorf("4th star label = %q", v)
	}

	s.Submit(t, s.Alice, fmt.Sprintf("/books/rating/%d", id), url.Values{"shelf": {"read"}, "rating": {"0"}}, page)
	if b, _ := s.Store.Get(context.Background(), s.Alice.User.ID, id); b.Rating != 0 {
		t.Errorf("rating after clearing = %d", b.Rating)
	}
	if rec := s.Post(t, s.Alice, fmt.Sprintf("/books/rating/%d", id), url.Values{"rating": {"9"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("rating 9 = %d, want 400", rec.Code)
	}
	if rec := s.Post(t, s.Bob, fmt.Sprintf("/books/rating/%d", id), url.Values{"rating": {"1"}}); rec.Code != http.StatusNotFound {
		t.Errorf("Bob's rating = %d, want 404", rec.Code)
	}
}

func TestReviewIsWrittenAndShown(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfRead))
	page := fmt.Sprintf("/books/b/%d?shelf=read", id)
	doc := s.Get(t, s.Alice, page)
	doc.MustNotHave(".books-review-text")
	if got := htmlassert.Text(doc.MustHave(".books-review-edit summary")); got != "Write a review" {
		t.Errorf("summary = %q", got)
	}

	rec := s.PostHX(t, s.Alice, fmt.Sprintf("/books/review/%d", id), url.Values{"shelf": {"read"}, "review": {"So much **sand**.\n\nWorth it."}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx review = %d, want 200", rec.Code)
	}
	doc = htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#books-panes")
	if got := htmlassert.Text(doc.MustHave(".books-review-text strong")); got != "sand" {
		t.Errorf("bold = %q", got)
	}
	if n := len(doc.QueryAll(".books-review-text p")); n != 2 {
		t.Errorf("%d paragraphs, want 2", n)
	}
	if got := htmlassert.Text(doc.MustHave(".books-review-edit summary")); got != "Edit review" {
		t.Errorf("summary = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave("textarea#books-review-input")); got != "So much **sand**. Worth it." {
		t.Errorf("textarea = %q", got)
	}
	// Without JavaScript the form posts and comes back.
	s.Submit(t, s.Alice, fmt.Sprintf("/books/review/%d", id), url.Values{"shelf": {"read"}, "review": {""}}, page)
	s.Get(t, s.Alice, page).MustNotHave(".books-review-text")
	if rec := s.Post(t, s.Bob, fmt.Sprintf("/books/review/%d", id), url.Values{"review": {"x"}}); rec.Code != http.StatusNotFound {
		t.Errorf("Bob's review = %d, want 404", rec.Code)
	}
}
