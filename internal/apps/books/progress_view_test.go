package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// postHXTo posts Alice's form as htmx does, aimed at target.
func postHXTo(t *testing.T, s *server, path, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set(web.CSRFFormField, s.CSRFToken(t, s.Alice))
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", target)
	return s.Do(t, s.Alice, req)
}

// readingBook adds one of Alice's books, on the go, with pages pages.
func readingBook(t *testing.T, s *server, title string, pages int) int64 {
	t.Helper()
	nb := titled(title, "", books.ShelfReading)
	nb.Pages = pages
	return add(t, s, s.Alice.User.ID, nb)
}

func TestTheProgressBoxIsInTheUnitOfTheReading(t *testing.T) {
	s := newServer(t)
	paper := readingBook(t, s, "Dune", 600)
	audio := readingBook(t, s, "Emma", 300)
	if err := s.Store.SetFormat(context.Background(), s.Alice.User.ID, audio, "audio"); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", paper))
	if _, ok := htmlassert.Attr(doc.MustHave("form.books-progress-form"), "novalidate"); !ok {
		t.Error("progress form lacks novalidate: the server's message must show, not the browser's")
	}
	input := doc.MustHave("input#books-progress-input")
	if v, _ := htmlassert.Attr(input, "max"); v != "600" {
		t.Errorf("paper max = %q, want 600", v)
	}
	if got := htmlassert.Text(doc.MustHave(".books-progress-form label")); got != "Page" {
		t.Errorf("paper label = %q, want Page", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-progress-note")); got != "No progress yet." {
		t.Errorf("note = %q", got)
	}
	if got := attr(t, doc, "form.books-progress-form", "hx-target"); got != "#books-reading" {
		t.Errorf("progress form hx-target = %q, want the whole Reading section", got)
	}
	if got := attr(t, doc, "button.books-progress-save", "aria-label"); got != "Save progress" {
		t.Errorf("save button aria-label = %q: the iOS number pad has no Enter, so it needs a named button", got)
	}

	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", audio))
	if v, _ := htmlassert.Attr(doc.MustHave("input#books-progress-input"), "max"); v != "100" {
		t.Errorf("audio max = %q, want 100", v)
	}
	if got := htmlassert.Text(doc.MustHave(".books-progress-of")); got != "%" {
		t.Errorf("audio unit = %q, want %%", got)
	}

	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	if err := s.Store.RecordProgress(context.Background(), s.Alice.User.ID, audio, 40); err != nil {
		t.Fatal(err)
	}
	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", audio))
	if got := htmlassert.Text(doc.MustHave(".books-progress-note")); got != "updated 9 Oct 2026" {
		t.Errorf("percent note = %q, want just the date: the box says the %% already", got)
	}

	want := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", want)).MustNotHave("#books-progress")
}

func TestProgressOverHTMXSwapsTheBox(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	id := readingBook(t, s, "Dune", 600)
	rec := postHXTo(t, s, fmt.Sprintf("/books/progress/%d", id), "books-reading", url.Values{"shelf": {"reading"}, "at": {"120"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx progress = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("section#books-reading")
	doc.MustNotHave("#books-book")
	if v, _ := htmlassert.Attr(doc.MustHave("input#books-progress-input"), "value"); v != "120" {
		t.Errorf("input value = %q, want 120", v)
	}
	if v, _ := htmlassert.Attr(doc.MustHave("progress.books-progress-bar"), "value"); v != "20" {
		t.Errorf("bar = %q, want 20", v)
	}
	if got := htmlassert.Text(doc.MustHave(".books-progress-note")); got != "20% · updated 9 Oct 2026" {
		t.Errorf("note = %q", got)
	}
	doc.MustNotHave("#books-list") // no list beside the book any more
}

func TestProgressWithoutJavaScriptComesBackToTheBook(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	s.Submit(t, s.Alice, fmt.Sprintf("/books/progress/%d", id), url.Values{"shelf": {"reading"}, "at": {"300"}},
		fmt.Sprintf("/books/b/%d?shelf=reading", id))
	if b, _ := s.Store.Get(context.Background(), s.Alice.User.ID, id); b.Progress.Value != 300 {
		t.Errorf("progress = %+v, want page 300", b.Progress)
	}
}

func TestBadProgressIsRefusedInsideTheBox(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	path := fmt.Sprintf("/books/progress/%d", id)
	for _, typed := range []string{"700", "-3", "lots", ""} {
		rec := postHXTo(t, s, path, "books-reading", url.Values{"shelf": {"reading"}, "at": {typed}})
		if rec.Code != http.StatusOK {
			t.Fatalf("htmx refused progress %q = %d, want 200 so htmx swaps it", typed, rec.Code)
		}
		doc := htmlassert.Parse(t, rec.Body.String())
		if got := htmlassert.Text(doc.MustHave("#books-progress-error")); got != "Enter a page from 0 to 600." {
			t.Errorf("%q: error = %q", typed, got)
		}
		if v, _ := htmlassert.Attr(doc.MustHave("input#books-progress-input"), "value"); v != typed {
			t.Errorf("%q: input keeps %q, want what was typed", typed, v)
		}
	}
	rec := s.Post(t, s.Alice, path, url.Values{"shelf": {"reading"}, "at": {"700"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("refused progress without JavaScript = %d, want 422", rec.Code)
	}
	htmlassert.Parse(t, rec.Body.String()).MustHave("#books-progress-error")
	if b, _ := s.Store.Get(context.Background(), s.Alice.User.ID, id); b.Progress.Set() {
		t.Errorf("progress = %+v after refusals, want none", b.Progress)
	}
}

// Did not finish… offers the saved page as where it stopped, never a
// refused one still in the progress box (#600).
func TestARefusedProgressLeavesDidNotFinishAtTheSavedPage(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	path := fmt.Sprintf("/books/progress/%d", id)
	s.Submit(t, s.Alice, path, url.Values{"shelf": {"reading"}, "at": {"212"}}, fmt.Sprintf("/books/b/%d?shelf=reading", id))
	doc := htmlassert.Parse(t, postHXTo(t, s, path, "books-reading", url.Values{"shelf": {"reading"}, "at": {"700"}}).Body.String())
	if v, _ := htmlassert.Attr(doc.MustHave("input#books-progress-input"), "value"); v != "700" {
		t.Errorf("progress box = %q, want the refused 700 kept", v)
	}
	if v, _ := htmlassert.Attr(doc.MustHave("input#books-dnf-at"), "value"); v != "212" {
		t.Errorf("Did not finish… stopped at = %q, want the saved 212", v)
	}
}

func TestProgressOnSomeoneElsesBookIsNotFound(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	if rec := s.Post(t, s.Bob, fmt.Sprintf("/books/progress/%d", id), url.Values{"at": {"10"}}); rec.Code != http.StatusNotFound {
		t.Errorf("Bob's progress = %d, want 404", rec.Code)
	}
}

func TestRowsShowProgressAndStars(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	uid := s.Alice.User.ID
	id := readingBook(t, s, "Dune", 600)
	readingBook(t, s, "Emma", 300)
	s.Clock.Advance(time.Minute)
	if err := s.Store.RecordProgress(context.Background(), uid, id, 150); err != nil {
		t.Fatal(err)
	}
	read := add(t, s, uid, titled("Ulysses", "", books.ShelfRead))
	if err := s.Store.SetRating(context.Background(), uid, read, 4); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/books/?shelf=reading")
	if got := htmlassert.Text(doc.MustHave(".books-item-status")); got != "Reading · p. 150 / 600" {
		t.Errorf("first reading tile = %q, want Dune's page (the one with progress first)", got)
	}
	if n := len(doc.QueryAll("progress.books-item-cover-bar")); n != 1 {
		t.Errorf("%d cover bars, want 1: no bar before any progress", n)
	}
	doc = s.Get(t, s.Alice, "/books/?shelf=read")
	starsEl := doc.MustHave(".books-stars")
	if got := htmlassert.Text(starsEl); got != "★★★★☆" {
		t.Errorf("stars = %q", got)
	}
	if v, _ := htmlassert.Attr(starsEl, "aria-label"); v != "Rated 4 of 5" {
		t.Errorf("stars label = %q", v)
	}
}

func TestProgressSwapsTheWholeReadingSection(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	rec := postHXTo(t, s, fmt.Sprintf("/books/progress/%d", id), "books-reading", url.Values{"shelf": {"reading"}, "at": {"212"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx progress = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("section#books-reading")
	doc.MustNotHave("#books-book")
	if got := attr(t, doc, "input#books-dnf-at", "value"); got != "212" {
		t.Errorf("Did not finish… stops at %q, want the page just saved, 212", got)
	}
}
