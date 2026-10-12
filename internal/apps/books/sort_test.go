package books_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// addedInTurn adds Alice's book, then moves the clock on a minute, so
// every book's added_at differs.
func addedInTurn(t *testing.T, f *fixture, nb books.NewBook) int64 {
	t.Helper()
	id := addBook(t, f, f.alice.ID, nb)
	f.now = f.now.Add(time.Minute)
	return id
}

func TestSortByDateAdded(t *testing.T) {
	f := newFixture(t)
	first := addedInTurn(t, f, onShelf("First", books.ShelfWant))
	addedInTurn(t, f, onShelf("Second", books.ShelfWant))
	addedInTurn(t, f, onShelf("Third", books.ShelfWant))
	// Tagging First bumps its updated_at, so the Default order of All
	// would put it on top. Date added must not.
	if err := f.store.SetTags(context.Background(), f.alice.ID, first, []string{"x"}); err != nil {
		t.Fatal(err)
	}
	if got := list(t, f, books.ListQuery{Shelf: books.ShelfAll}); got[0] != "First" {
		t.Fatalf("Default All = %v, want First on top (latest change)", got)
	}
	q := books.ListQuery{Shelf: books.ShelfAll, Sort: books.SortAdded}
	if got, want := list(t, f, q), []string{"Third", "Second", "First"}; !slices.Equal(got, want) {
		t.Errorf("Date added = %v, want %v (newest first)", got, want)
	}
	q.Reversed = true
	if got, want := list(t, f, q), []string{"First", "Second", "Third"}; !slices.Equal(got, want) {
		t.Errorf("Date added, reversed = %v, want %v", got, want)
	}
}

func TestSortByTitleSkipsALeadingArticle(t *testing.T) {
	f := newFixture(t)
	for _, title := range []string{"A Wizard of Earthsea", "Theodore Boone", "The Left Hand of Darkness",
		"An Instance of the Fingerpost", "Anathem"} {
		addedInTurn(t, f, onShelf(title, books.ShelfWant))
	}
	want := []string{"Anathem", "An Instance of the Fingerpost", "The Left Hand of Darkness", "Theodore Boone", "A Wizard of Earthsea"}
	q := books.ListQuery{Shelf: books.ShelfAll, Sort: books.SortTitle}
	if got := list(t, f, q); !slices.Equal(got, want) {
		t.Errorf("Title = %v, want %v (\"The\", \"A\", \"An\" skipped; \"Theodore\" kept)", got, want)
	}
	q.Reversed = true
	slices.Reverse(want)
	if got := list(t, f, q); !slices.Equal(got, want) {
		t.Errorf("Title, reversed = %v, want %v", got, want)
	}
}

func TestSortByAuthorSurname(t *testing.T) {
	f := newFixture(t)
	for _, b := range []struct{ title, authors string }{
		{"Dune", "Frank Herbert"},
		{"Good Omens", "Terry Pratchett, Neil Gaiman"},
		{"Anonymous diary", ""},
		{"The Dispossessed", "Ursula K. Le Guin"},
		{"Children of Dune", "Frank Herbert"},
	} {
		nb := onShelf(b.title, books.ShelfWant)
		nb.Authors = b.authors
		addedInTurn(t, f, nb)
	}
	q := books.ListQuery{Shelf: books.ShelfAll, Sort: books.SortAuthor}
	// guin < herbert < pratchett (the first author's last word); a tie goes
	// by title; no author is last.
	want := []string{"The Dispossessed", "Children of Dune", "Dune", "Good Omens", "Anonymous diary"}
	if got := list(t, f, q); !slices.Equal(got, want) {
		t.Errorf("Author = %v, want %v", got, want)
	}
	q.Reversed = true
	want = []string{"Good Omens", "Dune", "Children of Dune", "The Dispossessed", "Anonymous diary"}
	if got := list(t, f, q); !slices.Equal(got, want) {
		t.Errorf("Author, reversed = %v, want %v (still no author last)", got, want)
	}
}

func TestDefaultSortReversedKeepsUndatedLast(t *testing.T) {
	f := newFixture(t)
	undated := addBook(t, f, f.alice.ID, onShelf("Undated", books.ShelfRead))
	early := onShelf("Early", books.ShelfRead)
	early.FinishedOn = "2026-01-05"
	addBook(t, f, f.alice.ID, early)
	late := onShelf("Late", books.ShelfRead)
	late.FinishedOn = "2026-03-05"
	addBook(t, f, f.alice.ID, late)
	if _, err := f.db.Exec(`UPDATE books_readings SET finished_on = NULL WHERE book_id = ?`, undated); err != nil {
		t.Fatal(err)
	}
	q := books.ListQuery{Shelf: books.ShelfRead}
	if got, want := list(t, f, q), []string{"Late", "Early", "Undated"}; !slices.Equal(got, want) {
		t.Errorf("Read, Default = %v, want %v", got, want)
	}
	q.Reversed = true
	if got, want := list(t, f, q), []string{"Early", "Late", "Undated"}; !slices.Equal(got, want) {
		t.Errorf("Read, Default reversed = %v, want %v (undated stays last)", got, want)
	}
}

func TestASeriesIgnoresTheSort(t *testing.T) {
	f := newFixture(t)
	addBook(t, f, f.alice.ID, inSeries("Caliban's War", "The Expanse", "2"))
	addBook(t, f, f.alice.ID, inSeries("Leviathan Wakes", "The Expanse", "1"))
	for _, s := range books.Sorts {
		for _, reversed := range []bool{false, true} {
			got := list(t, f, books.ListQuery{Shelf: books.ShelfAll, Series: "The Expanse", Sort: s, Reversed: reversed})
			if want := []string{"Leviathan Wakes", "Caliban's War"}; !slices.Equal(got, want) {
				t.Errorf("series by %s (reversed %v) = %v, want reading order %v", s, reversed, got, want)
			}
		}
	}
}

func TestTagsByBook(t *testing.T) {
	f := newFixture(t)
	tagged := onShelf("Dune", books.ShelfWant)
	tagged.Tags = []string{"sf", "classics"}
	dune := addBook(t, f, f.alice.ID, tagged)
	plain := addBook(t, f, f.alice.ID, onShelf("Emma", books.ShelfWant))
	bobs := onShelf("Bob's", books.ShelfWant)
	bobs.Tags = []string{"secret"}
	bob := addBook(t, f, f.bob.ID, bobs)

	got, err := f.store.TagsByBook(context.Background(), f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got[dune], []string{"classics", "sf"}) {
		t.Errorf("Dune's tags = %v, want [classics sf]", got[dune])
	}
	if _, ok := got[plain]; ok {
		t.Error("a book with no tags has an entry")
	}
	if _, ok := got[bob]; ok {
		t.Error("Bob's book's tags came back for Alice")
	}
}
