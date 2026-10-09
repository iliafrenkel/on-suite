package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func quoteIDs(t *testing.T, s *server, id int64) []int64 {
	t.Helper()
	qs, err := s.Store.Quotes(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	var out []int64
	for _, q := range qs {
		out = append(out, q.ID)
	}
	return out
}

func TestQuotesShowAsCards(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	id := readBook(t, s, "Dune", 600)
	if _, err := s.Store.AddQuote(ctx, uid, id, books.QuoteInput{Page: 8,
		Text: "I must not fear.\nFear is the mind-killer.", Comment: "The *litany*."}); err != nil {
		t.Fatal(err)
	}
	s.Clock.Advance(time.Hour)
	if _, err := s.Store.AddQuote(ctx, uid, id, books.QuoteInput{Text: "The spice must flow."}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.AddNote(ctx, uid, id, books.NoteInput{Body: "A note."}); err != nil {
		t.Fatal(err)
	}
	rec := s.Do(t, s.Alice, httptestGet(fmt.Sprintf("/books/b/%d", id)))
	doc := htmlassert.Parse(t, rec.Body.String())
	cards := doc.QueryAll(".books-quote-text")
	if len(cards) != 2 {
		t.Fatalf("%d quote cards, want 2", len(cards))
	}
	if got := cards[0].FirstChild.Data; got != "The spice must flow." {
		t.Errorf("newest quote = %q", got)
	}
	if got := cards[1].FirstChild.Data; got != "I must not fear.\nFear is the mind-killer." {
		t.Errorf("older quote = %q, want its line break kept", got)
	}
	if got := strings.Join(texts(doc, ".books-quote .books-entry-meta"), "|"); got != "p. 8" {
		t.Errorf("pages = %q, want only the older quote's", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-quote-comment em")); got != "litany" {
		t.Errorf("Markdown in a comment = %q", got)
	}
	if n := len(doc.QueryAll(".books-quote-comment")); n != 1 {
		t.Errorf("%d comments, want 1: no empty comment box", n)
	}
	doc.MustHave(".books-quote button[hx-confirm]")
	if n := len(doc.QueryAll(".books-quote button[hx-confirm]")); n != 2 {
		t.Errorf("%d delete buttons with a confirmation, want one per quote", n)
	}
	body := rec.Body.String()
	notes, quotes, history := strings.Index(body, `id="books-notes"`), strings.Index(body, `id="books-quotes"`), strings.Index(body, `id="books-history-head"`)
	if !(notes < quotes && quotes < history) {
		t.Errorf("section order: notes at %d, quotes at %d, history at %d; want Notes, Quotes, Reading history", notes, quotes, history)
	}
}

func TestAddingAQuote(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	path := fmt.Sprintf("/books/quotes/%d", id)
	s.Submit(t, s.Alice, path, url.Values{"shelf": {"read"}, "page": {"8"}, "text": {"Fear is the mind-killer."}, "comment": {"Litany."}},
		fmt.Sprintf("/books/b/%d?shelf=read", id))

	rec := postHXTo(t, s, path, "books-quotes", url.Values{"shelf": {"read"}, "text": {"The spice must flow."}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx add = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("section#books-quotes")
	if v, _ := htmlassert.Attr(doc.MustHave("#books-list"), "hx-swap-oob"); v != "true" {
		t.Errorf("list hx-swap-oob = %q, want the list out of band", v)
	}
	doc.MustNotHave("#books-panes")
	if got := strings.Join(texts(doc, ".books-quote-text"), "|"); got != "The spice must flow.|Fear is the mind-killer." {
		t.Errorf("quotes = %q", got)
	}
	qs, _ := s.Store.Quotes(context.Background(), s.Alice.User.ID, id)
	if len(qs) != 2 || qs[1].Page != 8 || qs[1].Comment != "Litany." {
		t.Errorf("stored quotes = %+v", qs)
	}
}

func TestARefusedQuoteComesBackInItsForm(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	path := fmt.Sprintf("/books/quotes/%d", id)
	rec := postHXTo(t, s, path, "books-quotes", url.Values{"shelf": {"read"}, "page": {"12"}, "text": {" "}, "comment": {"Keep me."}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx refusal = %d, want a 200 fragment", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if !isOpen(doc, "details#books-quote-new") {
		t.Error("the add-quote box is closed; want it open with the message")
	}
	if got := htmlassert.Text(doc.MustHave("#books-quote-new-error")); got != "Type the quote first." {
		t.Errorf("message = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave("textarea#books-quote-new-comment")); got != "Keep me." {
		t.Errorf("comment box = %q, want what was typed", got)
	}
	if v, _ := htmlassert.Attr(doc.MustHave("input#books-quote-new-page"), "value"); v != "12" {
		t.Errorf("page box = %q, want what was typed", v)
	}
	if rec := s.Post(t, s.Alice, path, url.Values{"text": {"x"}, "page": {"601"}}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("refused without JavaScript = %d, want 422", rec.Code)
	}
	if n := len(quoteIDs(t, s, id)); n != 0 {
		t.Errorf("%d quotes stored by refused posts", n)
	}
}

func TestEditingAndDeletingAQuoteInThePane(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	qid, err := s.Store.AddQuote(context.Background(), s.Alice.User.ID, id, books.QuoteInput{Text: "Draft.", Comment: "Hm."})
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", id))
	if got := htmlassert.Text(doc.MustHave(fmt.Sprintf("textarea#books-quote-%d-comment", qid))); got != "Hm." {
		t.Errorf("edit comment box = %q", got)
	}

	edit := fmt.Sprintf("/books/quotes/%d/%d", id, qid)
	s.Submit(t, s.Alice, edit, url.Values{"shelf": {"read"}, "page": {"3"}, "text": {"Final."}, "comment": {""}},
		fmt.Sprintf("/books/b/%d?shelf=read", id))
	qs, _ := s.Store.Quotes(context.Background(), s.Alice.User.ID, id)
	if len(qs) != 1 || qs[0].Text != "Final." || qs[0].Page != 3 || qs[0].Comment != "" {
		t.Errorf("edited quotes = %+v", qs)
	}

	rec := postHXTo(t, s, edit, "books-quotes", url.Values{"text": {"Final."}, "page": {"x"}})
	doc = htmlassert.Parse(t, rec.Body.String())
	if !isOpen(doc, ".books-entry-edit") {
		t.Error("the refused quote's Edit box is closed")
	}
	if got := htmlassert.Text(doc.MustHave(fmt.Sprintf("#books-quote-%d-error", qid))); got != "Enter a page from 1 to 600." {
		t.Errorf("message = %q", got)
	}

	rec = postHXTo(t, s, edit+"/delete", "books-quotes", url.Values{"shelf": {"read"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx delete = %d", rec.Code)
	}
	htmlassert.Parse(t, rec.Body.String()).MustNotHave(".books-quote")
	if n := len(quoteIDs(t, s, id)); n != 0 {
		t.Errorf("%d quotes after delete", n)
	}
}

func TestQuoteRoutesAreNotFoundForOthers(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	qid, err := s.Store.AddQuote(context.Background(), s.Alice.User.ID, id, books.QuoteInput{Text: "Mine."})
	if err != nil {
		t.Fatal(err)
	}
	other := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	for _, tt := range []struct {
		sess string
		path string
	}{
		{"bob", fmt.Sprintf("/books/quotes/%d", id)},
		{"bob", fmt.Sprintf("/books/quotes/%d/%d", id, qid)},
		{"bob", fmt.Sprintf("/books/quotes/%d/%d/delete", id, qid)},
		{"alice", fmt.Sprintf("/books/quotes/%d/%d", other, qid)},
		{"alice", fmt.Sprintf("/books/quotes/%d/%d/delete", other, qid)},
		{"alice", fmt.Sprintf("/books/quotes/%d/0", id)},
	} {
		sess := s.Alice
		if tt.sess == "bob" {
			sess = s.Bob
		}
		if rec := s.Post(t, sess, tt.path, url.Values{"text": {"x"}}); rec.Code != http.StatusNotFound {
			t.Errorf("%s POST %s = %d, want 404", tt.sess, tt.path, rec.Code)
		}
	}
	if qs, _ := s.Store.Quotes(context.Background(), s.Alice.User.ID, id); len(qs) != 1 || qs[0].Text != "Mine." {
		t.Errorf("quotes = %+v, want Alice's quote untouched", qs)
	}
}

func TestDeletingAQuoteWithoutJavaScript(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	qid, err := s.Store.AddQuote(context.Background(), s.Alice.User.ID, id, books.QuoteInput{Text: "Gone."})
	if err != nil {
		t.Fatal(err)
	}
	s.Submit(t, s.Alice, fmt.Sprintf("/books/quotes/%d/%d/delete", id, qid), url.Values{"shelf": {"read"}},
		fmt.Sprintf("/books/b/%d?shelf=read", id))
	if n := len(quoteIDs(t, s, id)); n != 0 {
		t.Errorf("%d quotes after delete", n)
	}
}
