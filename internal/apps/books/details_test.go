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

// fullBook is Alice's book with something in every section of its page: a
// reading in progress and an earlier one, a description, a rating and a
// review, a note and a quote.
func fullBook(t *testing.T, s *server) int64 {
	t.Helper()
	ctx := context.Background()
	uid := s.Alice.User.ID
	id := readTwiceOverHTTP(t, s)
	b, err := s.Store.Get(ctx, uid, id)
	if err != nil {
		t.Fatal(err)
	}
	in := b.BookInput
	in.Description = "Spice and sand."
	if err := s.Store.Update(ctx, uid, id, in); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetRating(ctx, uid, id, 4); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetReview(ctx, uid, id, "So much sand."); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.AddNote(ctx, uid, id, books.NoteInput{Body: "A note."}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.AddQuote(ctx, uid, id, books.QuoteInput{Text: "A quote."}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDetailsSectionsComeInSpecOrder(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", fullBook(t, s)))
	got := texts(doc, ".books-section-head")
	want := []string{"Reading", "Your take", "About", "Notes · 1", "Quotes · 1", "Reading history · 2"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("section headings = %q, want %q (spec \"Sections\")", got, want)
	}
	if isOpen(doc, "details#books-history") {
		t.Error("the reading history is open on a plain page load; want it collapsed")
	}
}

func TestDetailsIconButtonsHaveNames(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", fullBook(t, s)))
	icons := doc.QueryAll(".books-icon-btn")
	// The page bar's two, Save progress, Edit review, + / ✎ / 🗑 for the note
	// and for the quote, and ✎ / 🗑 for each of the two readings.
	if len(icons) != 14 {
		t.Errorf("%d icon buttons, want 14", len(icons))
	}
	for _, n := range icons {
		label, _ := htmlassert.Attr(n, "aria-label")
		title, _ := htmlassert.Attr(n, "title")
		if label == "" || title != label {
			t.Errorf("icon <%s> has aria-label %q and title %q; want a name, shown as its tooltip too", n.Data, label, title)
		}
	}
}

func TestTheBookPageHasNoTagsForm(t *testing.T) {
	s := newServer(t)
	nb := titled("Dune", "", books.ShelfWant)
	nb.Tags = []string{"book club", "sf"}
	id := add(t, s, s.Alice.User.ID, nb)
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=want", id))
	doc.MustNotHave(".books-tags-form")
	doc.MustNotHave(fmt.Sprintf(`form[action="/books/tags/%d"]`, id))
	var hrefs []string
	for _, a := range doc.QueryAll("a.books-pill-link") {
		href, _ := htmlassert.Attr(a, "href")
		hrefs = append(hrefs, htmlassert.Text(a)+" "+href)
	}
	if want := "book club /books/?shelf=all&tag=book+club|sf /books/?shelf=all&tag=sf"; strings.Join(hrefs, "|") != want {
		t.Errorf("tag pills = %q, want %q: each links to its list", hrefs, want)
	}
	if rec := s.Post(t, s.Alice, fmt.Sprintf("/books/tags/%d", id), url.Values{"tags": {"x"}}); rec.Code != http.StatusNotFound {
		t.Errorf("POST /books/tags = %d, want 404: tags are edited on the Edit page now", rec.Code)
	}
}
