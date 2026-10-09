package books_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// filtered is Alice's books on shelf matching q.
func filtered(t *testing.T, f *fixture, shelf books.Shelf, q string) []books.ListItem {
	t.Helper()
	items, err := f.store.List(context.Background(), f.alice.ID, books.ListQuery{Shelf: shelf, Q: q})
	if err != nil {
		t.Fatalf("List(%q) = %v", q, err)
	}
	return items
}

// shelfOfNotes is a small library with something written in each place
// the filter searches.
func shelfOfNotes(t *testing.T, f *fixture) (dune, emma int64) {
	t.Helper()
	ctx := context.Background()
	nb := onShelf("Dune", books.ShelfRead)
	nb.Authors = "Frank Herbert"
	dune = addBook(t, f, f.alice.ID, nb)
	emma = addBook(t, f, f.alice.ID, onShelf("Emma", books.ShelfWant))
	if err := f.store.SetReview(ctx, f.alice.ID, dune, "So much sand, and all of it **worth** it."); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddNote(ctx, f.alice.ID, emma, books.NoteInput{Body: "Mr Knightley is right about the picnic."}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddQuote(ctx, f.alice.ID, dune, books.QuoteInput{Text: "The spice must flow.",
		Comment: "Said by nobody in the book, as it happens."}); err != nil {
		t.Fatal(err)
	}
	return dune, emma
}

func TestFilterSearchesEverythingWritten(t *testing.T) {
	f := newFixture(t)
	shelfOfNotes(t, f)
	bob := addBook(t, f, f.bob.ID, onShelf("Bob's book", books.ShelfWant))
	if _, err := f.store.AddNote(context.Background(), f.bob.ID, bob, books.NoteInput{Body: "The spice, again."}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		q       string
		title   string
		in      books.MatchIn
		snippet string
	}{
		{"herbert", "Dune", books.MatchBook, ""},
		{"spice", "Dune", books.MatchQuote, "The \x02spice\x03 must flow."},
		{"nobody", "Dune", books.MatchComment, "Said by \x02nobody\x03 in the book, as it happens."},
		{"knight", "Emma", books.MatchNote, "Mr \x02Knightley\x03 is right about the picnic."},
		{"sand worth", "Dune", books.MatchReview, "So much \x02sand\x03, and all of it **\x02worth\x03** it."},
		{"SPICE", "Dune", books.MatchQuote, "The \x02spice\x03 must flow."},
	}
	for _, tt := range tests {
		items := filtered(t, f, books.ShelfAll, tt.q)
		if len(items) != 1 {
			t.Errorf("%q matched %v, want just %s", tt.q, titles(items), tt.title)
			continue
		}
		if it := items[0]; it.Title != tt.title || it.Match != tt.in || it.Snippet != tt.snippet {
			t.Errorf("%q = %s in %q: %q; want %s in %q: %q", tt.q, it.Title, it.Match, it.Snippet, tt.title, tt.in, tt.snippet)
		}
	}
	if got := filtered(t, f, books.ShelfAll, "picnic spice"); len(got) != 0 {
		t.Errorf("words from two books matched %v: every word must match the same book", titles(got))
	}
	if got := filtered(t, f, books.ShelfAll, ""); len(got) != 2 || got[0].Snippet != "" {
		t.Errorf("no filter = %+v, want both books without snippets", got)
	}
}

func TestFilterStaysWithinTheList(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, emma := shelfOfNotes(t, f)
	persuasion := addBook(t, f, f.alice.ID, onShelf("Persuasion", books.ShelfWant))
	if _, err := f.store.AddNote(ctx, f.alice.ID, persuasion, books.NoteInput{Body: "A picnic of sorts."}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetTags(ctx, f.alice.ID, emma, []string{"austen"}); err != nil {
		t.Fatal(err)
	}
	if got := titles(filtered(t, f, books.ShelfWant, "picnic")); !slices.Equal(got, []string{"Persuasion", "Emma"}) {
		t.Errorf("Want to read for picnic = %v, want the shelf's own order (newest added first)", got)
	}
	if got := titles(filtered(t, f, books.ShelfRead, "picnic")); len(got) != 0 {
		t.Errorf("Read for picnic = %v, want none: Emma and Persuasion aren't read", got)
	}
	items, err := f.store.List(ctx, f.alice.ID, books.ListQuery{Shelf: books.ShelfAll, Tag: "austen", Q: "picnic"})
	if err != nil || !slices.Equal(titles(items), []string{"Emma"}) {
		t.Errorf("tag austen for picnic = %v, %v; want Emma", titles(items), err)
	}
}

func TestTheSearchIndexFollowsEveryChange(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dune, emma := shelfOfNotes(t, f)
	found := func(q string) []string { return titles(filtered(t, f, books.ShelfAll, q)) }

	if err := f.store.Update(ctx, f.alice.ID, dune, books.BookInput{Title: "Children of Dune", SeriesName: "Dune Chronicles"}); err != nil {
		t.Fatal(err)
	}
	if got := found("children"); !slices.Equal(got, []string{"Children of Dune"}) {
		t.Errorf("new title: %v", got)
	}
	if got := found("herbert"); len(got) != 0 {
		t.Errorf("removed author still matches %v", got)
	}
	if got := found("chronicles"); len(got) != 1 {
		t.Errorf("series: %v", got)
	}

	ns := notes(t, f, f.alice.ID, emma)
	if err := f.store.UpdateNote(ctx, f.alice.ID, emma, ns[0].ID, books.NoteInput{Body: "Box Hill."}); err != nil {
		t.Fatal(err)
	}
	if got := found("picnic"); len(got) != 0 {
		t.Errorf("edited-away note still matches %v", got)
	}
	if got := found("box hill"); !slices.Equal(got, []string{"Emma"}) {
		t.Errorf("edited note: %v", got)
	}
	if err := f.store.DeleteNote(ctx, f.alice.ID, emma, ns[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := found("box"); len(got) != 0 {
		t.Errorf("deleted note still matches %v", got)
	}

	qs := quotes(t, f, f.alice.ID, dune)
	if err := f.store.UpdateQuote(ctx, f.alice.ID, dune, qs[0].ID, books.QuoteInput{Text: "Fear is the mind-killer."}); err != nil {
		t.Fatal(err)
	}
	if got := found("nobody"); len(got) != 0 {
		t.Errorf("removed comment still matches %v", got)
	}
	if got := found("mind"); len(got) != 1 {
		t.Errorf("edited quote: %v", got)
	}
	if err := f.store.DeleteQuote(ctx, f.alice.ID, dune, qs[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := found("fear"); len(got) != 0 {
		t.Errorf("deleted quote still matches %v", got)
	}
	if err := f.store.SetReview(ctx, f.alice.ID, dune, ""); err != nil {
		t.Fatal(err)
	}
	if got := found("sand"); len(got) != 0 {
		t.Errorf("removed review still matches %v", got)
	}

	if err := f.store.Delete(ctx, f.alice.ID, emma); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := f.db.QueryRow(`SELECT count(*) FROM books_search`).Scan(&rows); err != nil || rows != 1 {
		t.Errorf("index rows = %d, %v; want 1 once Emma is gone", rows, err)
	}
}

func TestFilterIgnoresSearchSyntax(t *testing.T) {
	f := newFixture(t)
	shelfOfNotes(t, f)
	// Dune's text says "and" (the review), Emma's doesn't; no text says "title".
	tests := []struct {
		q    string
		want []string
	}{
		{`*`, nil},
		{`-dune`, []string{"Dune"}},
		{`AND`, []string{"Dune"}},
		{`title:dune`, nil},
		{`"`, nil},
		{`(`, nil},
		{`NEAR(a b)`, nil},
		{`100%`, nil},
	}
	for _, tt := range tests {
		items, err := f.store.List(context.Background(), f.alice.ID, books.ListQuery{Shelf: books.ShelfAll, Q: tt.q})
		if err != nil {
			t.Errorf("List(%q) = %v, want no error", tt.q, err)
			continue
		}
		if got := titles(items); !slices.Equal(got, tt.want) && !(len(got) == 0 && len(tt.want) == 0) {
			t.Errorf("List(%q) = %v, want %v", tt.q, got, tt.want)
		}
	}
}

// TestTheIndexIsBuiltForExistingBooks applies the migrations up to
// 0004, writes a book with a note and a quote, then applies the rest: the
// index must hold what was there before it.
func TestTheIndexIsBuiltForExistingBooks(t *testing.T) {
	ctx := context.Background()
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	ms, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	app, err := db.Collect(books.ID, books.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	var before []db.Migration
	for _, m := range app {
		if m.ID <= "0004" {
			before = append(before, m)
		}
	}
	if _, err := db.Apply(ctx, handle, append(ms, before...)); err != nil {
		t.Fatal(err)
	}
	u, err := auth.NewStore(handle).CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}
	st := books.NewStore(handle)
	id, err := st.Create(ctx, u.ID, books.NewBook{BookInput: books.BookInput{Title: "Dune"}, Shelf: books.ShelfWant})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddNote(ctx, u.ID, id, books.NoteInput{Body: "Sandworms."}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddQuote(ctx, u.ID, id, books.QuoteInput{Text: "The spice must flow.", Comment: "Melange."}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(ms, app...)); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"dune", "sandworms", "spice", "melange"} {
		items, err := st.List(ctx, u.ID, books.ListQuery{Shelf: books.ShelfAll, Q: q})
		if err != nil || len(items) != 1 {
			t.Errorf("after the migration, %q found %d books, %v; want Dune", q, len(items), err)
		}
	}
}
