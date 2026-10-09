package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestTheFormatMenuSetsTheReadingsFormat(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=reading", id))
	if got := htmlassert.Text(doc.MustHave(".books-format-pill")); got != "Format not set" {
		t.Errorf("pill = %q", got)
	}
	var values []string
	for _, b := range doc.QueryAll(".books-format button") {
		v, _ := htmlassert.Attr(b, "value")
		values = append(values, v)
	}
	if fmt.Sprint(values) != "[paper ebook audio ]" {
		t.Errorf("format buttons = %q", values)
	}

	s.Submit(t, s.Alice, fmt.Sprintf("/books/format/%d", id), url.Values{"shelf": {"reading"}, "format": {"audio"}},
		fmt.Sprintf("/books/b/%d?shelf=reading", id))
	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=reading", id))
	if got := htmlassert.Text(doc.MustHave(".books-format-pill")); got != "Audiobook" {
		t.Errorf("pill after choosing audio = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(`.books-format button[aria-current="true"]`)); got != "Audiobook" {
		t.Errorf("current choice = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-progress-of")); got != "%" {
		t.Errorf("progress unit after audio = %q, want %%", got)
	}

	if rec := s.Post(t, s.Alice, fmt.Sprintf("/books/format/%d", id), url.Values{"format": {"vinyl"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("format vinyl = %d, want 400", rec.Code)
	}
	if rec := s.Post(t, s.Bob, fmt.Sprintf("/books/format/%d", id), url.Values{"format": {"paper"}}); rec.Code != http.StatusNotFound {
		t.Errorf("Bob's format = %d, want 404", rec.Code)
	}
}

func TestFinishOffersARatingAndDNFAPlace(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	ctx := context.Background()
	if err := s.Store.SetRating(ctx, s.Alice.User.ID, id, 3); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.RecordProgress(ctx, s.Alice.User.ID, id, 200); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", id))
	doc.MustHave(`select[name="rating"]`)
	if n := len(doc.QueryAll(`select[name="rating"] option`)); n != 6 {
		t.Errorf("%d rating options, want No rating and 1–5", n)
	}
	if v, _ := htmlassert.Attr(doc.MustHave(`option[selected]`), "value"); v != "3" {
		t.Errorf("selected rating = %q, want the book's 3", v)
	}
	for _, f := range doc.QueryAll("details.books-close form") {
		if _, ok := htmlassert.Attr(f, "novalidate"); !ok {
			t.Errorf("close form %q lacks novalidate", htmlassert.Text(f))
		}
	}
	at := doc.MustHave(`input[name="at"]`)
	if v, _ := htmlassert.Attr(at, "max"); v != "600" {
		t.Errorf("DNF page max = %q, want 600", v)
	}
	if v, _ := htmlassert.Attr(at, "value"); v != "200" {
		t.Errorf("DNF page = %q, want the current page 200", v)
	}
}
