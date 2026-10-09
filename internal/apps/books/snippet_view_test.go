package books_test

import (
	"context"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestFilteredRowsSayWhereTheyMatched(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	id := add(t, s, uid, titled("Dune", "Frank Herbert", books.ShelfWant))
	if _, err := s.Store.AddQuote(ctx, uid, id, books.QuoteInput{Text: "The <b>spice</b> must flow."}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.AddNote(ctx, uid, id, books.NoteInput{Body: "Sandworms."}); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/books/?shelf=all&q=spice")
	if got := htmlassert.Text(doc.MustHave(".books-row-snippet")); got != "Quote: The <b>spice</b> must flow." {
		t.Errorf("snippet = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-row-snippet mark")); got != "spice" {
		t.Errorf("highlight = %q", got)
	}
	doc.MustNotHave(".books-row-snippet b") // the quote's own markup is text

	doc = htmlassert.Parse(t, hx(t, s, "/books/?shelf=all&q=sandw", "books-list"))
	if got := htmlassert.Text(doc.MustHave(".books-snippet-in")); got != "Note:" {
		t.Errorf("label after typing part of a word = %q, want Note:", got)
	}

	for _, q := range []string{"dune", "herbert", ""} {
		doc = s.Get(t, s.Alice, "/books/?shelf=all&q="+q)
		if got := rowTitles(doc); len(got) != 1 {
			t.Errorf("q=%s rows = %v, want Dune", q, got)
		}
		doc.MustNotHave(".books-row-snippet")
	}
}

func TestTheFilterBoxSearchesEverything(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/books/")
	if got, _ := htmlassert.Attr(doc.MustHave("input#books-q"), "placeholder"); got != "Search books, notes and quotes…" {
		t.Errorf("placeholder = %q", got)
	}
}

func TestSnippetPartsKeepOneEntry(t *testing.T) {
	const o, c = books.SnippetOpen, books.SnippetClose
	tests := []struct {
		name, in, want string
	}{
		{"single line", "The " + o + "spice" + c + "  must	flow.", "The [spice] must flow."},
		{"middle of three", "First quote.\nThe " + o + "spice" + c + " must flow.\nThird.", "…The [spice] must flow.…"},
		{"first line", "The " + o + "spice" + c + " must flow.\nSecond quote…", "The [spice] must flow.…"},
		{"last line", "Earlier entry.\nThe " + o + "spice" + c + " must flow.", "…The [spice] must flow."},
		{"already ends with an ellipsis", "The " + o + "spice" + c + " must…\nSecond.", "The [spice] must…"},
		{"already starts with an ellipsis", "One.\n…" + o + "spice" + c + " must flow.", "…[spice] must flow."},
		{"no marker", "Just some text\nand more.", "Just some text and more."},
	}
	for _, tt := range tests {
		if got := books.SnippetPartsForTest(tt.in); got != tt.want {
			t.Errorf("%s: %q -> %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}
