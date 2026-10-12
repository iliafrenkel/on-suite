package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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
	if got := attr(t, doc, "form.books-rating", "hx-target"); got != "#books-take" {
		t.Errorf("rating form hx-target = %q, want Your take", got)
	}

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
	if got := htmlassert.Text(doc.MustHave("#books-take-head")); got != "Your take" {
		t.Errorf("section heading = %q", got)
	}
	// With no review, the one opener is the quiet link under the stars and
	// the heading has no icon (decided 2026-10-12).
	if got := htmlassert.Text(doc.MustHave(".books-review-edit summary")); got != "Write a review" {
		t.Errorf("summary = %q", got)
	}
	doc.MustNotHave("#books-take .books-section-action")

	rec := postHXTo(t, s, fmt.Sprintf("/books/review/%d", id), "books-take",
		url.Values{"shelf": {"read"}, "review": {"So much **sand**.\n\nWorth it."}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx review = %d, want 200", rec.Code)
	}
	doc = htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("section#books-take")
	doc.MustNotHave("#books-book") // just its own section (spec "Swaps")
	if got := htmlassert.Text(doc.MustHave(".books-review-text strong")); got != "sand" {
		t.Errorf("bold = %q", got)
	}
	if n := len(doc.QueryAll(".books-review-text p")); n != 2 {
		t.Errorf("%d paragraphs, want 2", n)
	}
	// With a review, the opener is the ✎ in the heading.
	if got := attr(t, doc, ".books-review-edit summary", "aria-label"); got != "Edit review" {
		t.Errorf("summary aria-label = %q, want Edit review", got)
	}
	doc.MustHave("#books-take .books-section-action")
	doc.MustNotHave(".books-review-write")
	if got := htmlassert.Text(doc.MustHave("textarea#books-review-input")); got != "So much **sand**. Worth it." {
		t.Errorf("textarea = %q", got)
	}
	if got := attr(t, doc, "form.books-review-form", "hx-target"); got != "#books-take" {
		t.Errorf("review form hx-target = %q", got)
	}
	// Without JavaScript the form posts and comes back.
	s.Submit(t, s.Alice, fmt.Sprintf("/books/review/%d", id), url.Values{"shelf": {"read"}, "review": {""}}, page)
	s.Get(t, s.Alice, page).MustNotHave(".books-review-text")
	if rec := s.Post(t, s.Bob, fmt.Sprintf("/books/review/%d", id), url.Values{"review": {"x"}}); rec.Code != http.StatusNotFound {
		t.Errorf("Bob's review = %d, want 404", rec.Code)
	}
}

func TestRatingOverHTMXRefreshesTheFinishRating(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	if err := s.Store.SetRating(context.Background(), s.Alice.User.ID, id, 3); err != nil {
		t.Fatal(err)
	}
	rec := postHXTo(t, s, fmt.Sprintf("/books/rating/%d", id), "books-take", url.Values{"shelf": {"reading"}, "rating": {"5"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx rating = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("section#books-take")
	doc.MustNotHave("#books-book")
	if n := len(doc.QueryAll("#books-take .is-on")); n != 5 {
		t.Errorf("%d stars on, want 5", n)
	}
	if got := attr(t, doc, "section#books-reading", "hx-swap-oob"); got != "true" {
		t.Errorf("Reading section hx-swap-oob = %q, want it along out of band", got)
	}
	if got := attr(t, doc, "select#books-finish-rating option[selected]", "value"); got != "5" {
		t.Errorf("Finish… rating = %q, want the new 5: the old 3 would overwrite it on finishing", got)
	}

	read := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfRead))
	rec = postHXTo(t, s, fmt.Sprintf("/books/rating/%d", read), "books-take", url.Values{"shelf": {"read"}, "rating": {"4"}})
	htmlassert.Parse(t, rec.Body.String()).MustNotHave("#books-reading") // nothing to finish, nothing to refresh
}

func TestARefusedReviewShowsTheBannerOnThePage(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfRead))
	rec := postHXTo(t, s, fmt.Sprintf("/books/review/%d", id), "books-take",
		url.Values{"shelf": {"read"}, "review": {strings.Repeat("a", books.MaxReviewRunes+1)}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx refused review = %d, want 200 so htmx swaps it", rec.Code)
	}
	if got := rec.Header().Get("HX-Retarget"); got != "#books-book" {
		t.Errorf("HX-Retarget = %q, want #books-book: the banner sits above the sections", got)
	}
	if got := rec.Header().Get("HX-Reswap"); got != "outerHTML" {
		t.Errorf("HX-Reswap = %q, want outerHTML", got)
	}
	htmlassert.Parse(t, rec.Body.String()).MustHave("#books-book .books-banner")
}
