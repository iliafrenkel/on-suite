package books_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// withPages is one of Alice's Want to read books with a page count (0:
// unknown).
func withPages(t *testing.T, f *fixture, title string, pages int) int64 {
	t.Helper()
	nb := onShelf(title, books.ShelfWant)
	nb.Pages = pages
	return addBook(t, f, f.alice.ID, nb)
}

func notes(t *testing.T, f *fixture, userID, id int64) []books.Note {
	t.Helper()
	ns, err := f.store.Notes(context.Background(), userID, id)
	if err != nil {
		t.Fatal(err)
	}
	return ns
}

func quotes(t *testing.T, f *fixture, userID, id int64) []books.Quote {
	t.Helper()
	qs, err := f.store.Quotes(context.Background(), userID, id)
	if err != nil {
		t.Fatal(err)
	}
	return qs
}

// wantRefusal fails the test unless err is a Refusal saying msg.
func wantRefusal(t *testing.T, what string, err error, msg string) {
	t.Helper()
	var ref *books.Refusal
	if !errors.As(err, &ref) || ref.Msg != msg {
		t.Errorf("%s = %v, want a Refusal %q", what, err, msg)
	}
}

func TestNotesAreDatedAndNewestFirst(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := withPages(t, f, "Dune", 600)
	first := f.now
	if _, err := f.store.AddNote(ctx, f.alice.ID, id, books.NoteInput{Page: 112, Body: "  The *spice*.\r\nAgain.  "}); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	if _, err := f.store.AddNote(ctx, f.alice.ID, id, books.NoteInput{Body: "No page."}); err != nil {
		t.Fatal(err)
	}
	ns := notes(t, f, f.alice.ID, id)
	if len(ns) != 2 {
		t.Fatalf("%d notes, want 2", len(ns))
	}
	if ns[0].Body != "No page." || ns[0].Page != 0 || !ns[0].CreatedAt.Equal(f.now) {
		t.Errorf("newest note = %+v", ns[0])
	}
	if ns[1].Body != "The *spice*.\nAgain." || ns[1].Page != 112 || !ns[1].CreatedAt.Equal(first) {
		t.Errorf("first note = %+v, want its text tidied and page 112", ns[1])
	}
	if b := getBook(t, f, f.alice.ID, id); !b.UpdatedAt.Equal(f.now) {
		t.Errorf("book updated_at = %v, want %v: a note is a change", b.UpdatedAt, f.now)
	}
	if got := notes(t, f, f.bob.ID, id); len(got) != 0 {
		t.Errorf("Bob sees %d of Alice's notes", len(got))
	}
}

func TestNotesRefuseBadInput(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dune := withPages(t, f, "Dune", 600)
	unknown := withPages(t, f, "Emma", 0)
	tests := []struct {
		name string
		id   int64
		in   books.NoteInput
		want string
	}{
		{"empty", dune, books.NoteInput{Body: " \n "}, "Write something in the note first."},
		{"too long", dune, books.NoteInput{Body: strings.Repeat("x", books.MaxNoteRunes+1)}, "Keep your note to 20000 characters or fewer."},
		{"page past the end", dune, books.NoteInput{Page: 601, Body: "x"}, "Enter a page from 1 to 600."},
		{"not a page", dune, books.NoteInput{Page: -1, Body: "x"}, "Enter a page from 1 to 600."},
		{"not a page, no page count", unknown, books.NoteInput{Page: -1, Body: "x"}, "Enter a page number of 1 or more."},
	}
	for _, tt := range tests {
		_, err := f.store.AddNote(ctx, f.alice.ID, tt.id, tt.in)
		wantRefusal(t, tt.name, err, tt.want)
	}
	if n := len(notes(t, f, f.alice.ID, dune)); n != 0 {
		t.Errorf("%d notes saved by refused adds", n)
	}
	if _, err := f.store.AddNote(ctx, f.alice.ID, unknown, books.NoteInput{Page: 5000, Body: "x"}); err != nil {
		t.Errorf("any page of a book with no page count = %v, want it accepted", err)
	}
	if _, err := f.store.AddNote(ctx, f.alice.ID, dune, books.NoteInput{Page: 600, Body: "x"}); err != nil {
		t.Errorf("the last page = %v, want it accepted", err)
	}
	if _, err := f.store.AddNote(ctx, f.bob.ID, dune, books.NoteInput{Body: "x"}); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob adding a note to Alice's book = %v, want ErrNotFound", err)
	}
}

func TestEditingAndDeletingANote(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dune := withPages(t, f, "Dune", 600)
	emma := withPages(t, f, "Emma", 300)
	nid, err := f.store.AddNote(ctx, f.alice.ID, dune, books.NoteInput{Page: 10, Body: "Draft."})
	if err != nil {
		t.Fatal(err)
	}
	added := f.now
	f.now = f.now.Add(time.Hour)
	if err := f.store.UpdateNote(ctx, f.alice.ID, dune, nid, books.NoteInput{Body: "Better."}); err != nil {
		t.Fatal(err)
	}
	n := notes(t, f, f.alice.ID, dune)[0]
	if n.Body != "Better." || n.Page != 0 || !n.CreatedAt.Equal(added) || !n.UpdatedAt.Equal(f.now) {
		t.Errorf("edited note = %+v, want new text, no page, the old date", n)
	}
	err = f.store.UpdateNote(ctx, f.alice.ID, dune, nid, books.NoteInput{Body: ""})
	wantRefusal(t, "emptying a note", err, "Write something in the note first.")

	for _, tt := range []struct {
		name         string
		user, bookID int64
	}{
		{"Bob", f.bob.ID, dune},
		{"another book", f.alice.ID, emma},
	} {
		if err := f.store.UpdateNote(ctx, tt.user, tt.bookID, nid, books.NoteInput{Body: "x"}); !errors.Is(err, books.ErrNotFound) {
			t.Errorf("update via %s = %v, want ErrNotFound", tt.name, err)
		}
		if err := f.store.DeleteNote(ctx, tt.user, tt.bookID, nid); !errors.Is(err, books.ErrNotFound) {
			t.Errorf("delete via %s = %v, want ErrNotFound", tt.name, err)
		}
	}
	if err := f.store.DeleteNote(ctx, f.alice.ID, dune, nid); err != nil {
		t.Fatal(err)
	}
	if n := len(notes(t, f, f.alice.ID, dune)); n != 0 {
		t.Errorf("%d notes after deleting the only one", n)
	}
}

func TestQuotesKeepTheirLineBreaks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := withPages(t, f, "Dune", 600)
	if _, err := f.store.AddQuote(ctx, f.alice.ID, id, books.QuoteInput{Page: 8,
		Text: "I must not fear.\r\nFear is the mind-killer.\n", Comment: " The litany. "}); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	if _, err := f.store.AddQuote(ctx, f.alice.ID, id, books.QuoteInput{Text: "The spice must flow."}); err != nil {
		t.Fatal(err)
	}
	qs := quotes(t, f, f.alice.ID, id)
	if len(qs) != 2 {
		t.Fatalf("%d quotes, want 2", len(qs))
	}
	if qs[0].Text != "The spice must flow." || qs[0].Page != 0 || qs[0].Comment != "" {
		t.Errorf("newest quote = %+v", qs[0])
	}
	if qs[1].Text != "I must not fear.\nFear is the mind-killer." || qs[1].Page != 8 || qs[1].Comment != "The litany." {
		t.Errorf("first quote = %+v", qs[1])
	}
	if got := quotes(t, f, f.bob.ID, id); len(got) != 0 {
		t.Errorf("Bob sees %d of Alice's quotes", len(got))
	}
}

func TestQuotesRefuseBadInput(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := withPages(t, f, "Dune", 600)
	tests := []struct {
		name string
		in   books.QuoteInput
		want string
	}{
		{"no text", books.QuoteInput{Comment: "Only a comment."}, "Type the quote first."},
		{"text too long", books.QuoteInput{Text: strings.Repeat("x", books.MaxQuoteRunes+1)}, "Keep the quote to 5000 characters or fewer."},
		{"comment too long", books.QuoteInput{Text: "x", Comment: strings.Repeat("x", books.MaxQuoteRunes+1)}, "Keep your comment to 5000 characters or fewer."},
		{"page past the end", books.QuoteInput{Page: 601, Text: "x"}, "Enter a page from 1 to 600."},
	}
	for _, tt := range tests {
		_, err := f.store.AddQuote(ctx, f.alice.ID, id, tt.in)
		wantRefusal(t, tt.name, err, tt.want)
	}
	if n := len(quotes(t, f, f.alice.ID, id)); n != 0 {
		t.Errorf("%d quotes saved by refused adds", n)
	}
}

func TestEditingAndDeletingAQuote(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dune := withPages(t, f, "Dune", 600)
	qid, err := f.store.AddQuote(ctx, f.alice.ID, dune, books.QuoteInput{Text: "Draft."})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateQuote(ctx, f.alice.ID, dune, qid, books.QuoteInput{Page: 3, Text: "Better.", Comment: "Why."}); err != nil {
		t.Fatal(err)
	}
	if q := quotes(t, f, f.alice.ID, dune)[0]; q.Text != "Better." || q.Page != 3 || q.Comment != "Why." {
		t.Errorf("edited quote = %+v", q)
	}
	if err := f.store.UpdateQuote(ctx, f.bob.ID, dune, qid, books.QuoteInput{Text: "x"}); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's update = %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteQuote(ctx, f.bob.ID, dune, qid); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's delete = %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteQuote(ctx, f.alice.ID, dune, qid+1); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("deleting a quote that isn't there = %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteQuote(ctx, f.alice.ID, dune, qid); err != nil {
		t.Fatal(err)
	}
	if n := len(quotes(t, f, f.alice.ID, dune)); n != 0 {
		t.Errorf("%d quotes after deleting the only one", n)
	}
}

func TestDeletingABookTakesItsNotesAndQuotes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := withPages(t, f, "Dune", 600)
	if _, err := f.store.AddNote(ctx, f.alice.ID, id, books.NoteInput{Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddQuote(ctx, f.alice.ID, id, books.QuoteInput{Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Delete(ctx, f.alice.ID, id); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"books_notes", "books_quotes"} {
		var n int
		if err := f.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s has %d rows after the book went, %v", table, n, err)
		}
	}
}
