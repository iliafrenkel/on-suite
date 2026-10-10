package books_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// importFixture parses the Goodreads fixture and imports it for userID.
func importFixture(t *testing.T, f *fixture, userID int64) books.ImportResult {
	t.Helper()
	rows, err := books.ParseGoodreads(bytes.NewReader(grFixture(t)))
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.store.Import(context.Background(), userID, rows)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// bookTitled is the id of userID's one book listed under title.
func bookTitled(t *testing.T, f *fixture, userID int64, title string) int64 {
	t.Helper()
	items, err := f.store.List(context.Background(), userID, books.ListQuery{Shelf: books.ShelfAll})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Title == title {
			return it.ID
		}
	}
	t.Fatalf("no book %q in %+v", title, items)
	return 0
}

const goodOmens = "Good Omens: The Nice and Accurate Prophecies of Agnes Nutter, Witch"

func TestImportAddsTheBooksWithTheirReadings(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	uid := f.alice.ID
	res := importFixture(t, f, uid)
	if res.Imported != 6 || !reflect.DeepEqual(res.Skipped, []string{goodOmens}) {
		t.Errorf("result = %+v, want 6 imported and the second Good Omens skipped", res)
	}
	counts, err := f.store.ShelfCounts(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if want := (map[books.Shelf]int{books.ShelfRead: 2, books.ShelfReading: 1, books.ShelfWant: 2, books.ShelfDNF: 1, books.ShelfAll: 6}); !reflect.DeepEqual(counts, want) {
		t.Errorf("shelf counts = %v, want %v", counts, want)
	}

	lw := getBook(t, f, uid, bookTitled(t, f, uid, "Leviathan Wakes"))
	if lw.SeriesName != "The Expanse" || lw.SeriesNumber != "1" || lw.ISBN != "9780316129084" || lw.Rating != 4 ||
		lw.Review != "Great fun.\n\nLoved Miller & Holden." || !reflect.DeepEqual(lw.Tags, []string{"sf", "space"}) {
		t.Errorf("Leviathan Wakes = %+v", lw)
	}
	if want := time.Date(2024, 2, 1, 0, 0, 0, 0, time.Local); !lw.AddedAt.Equal(want) {
		t.Errorf("added at %v, want the local start of Date Added, %v", lw.AddedAt, want)
	}
	if want := time.Date(2024, 3, 14, 0, 0, 0, 0, time.Local); !lw.UpdatedAt.Equal(want) {
		t.Errorf("updated at %v, want its Date Read, %v", lw.UpdatedAt, want)
	}
	rs, err := f.store.Readings(ctx, uid, lw.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].Status != books.StatusFinished || rs[0].FinishedOn != "2024-03-14" || rs[0].Format != "paper" ||
		rs[1].Status != books.StatusFinished || rs[1].FinishedOn != "" || rs[1].StartedOn != "" || rs[1].Format != "" {
		t.Errorf("readings = %+v, want the dated paper one, then one with no dates (Read Count 2)", rs)
	}

	p := getBook(t, f, uid, bookTitled(t, f, uid, "Piranesi"))
	if p.Shelf != books.ShelfReading || p.Latest.StartedOn != "2025-01-05" || p.Latest.Format != "ebook" || p.Rating != 0 {
		t.Errorf("Piranesi = shelf %s, %+v, rating %d; want reading since its Date Added, as an ebook, no rating", p.Shelf, p.Latest, p.Rating)
	}
	ij := getBook(t, f, uid, bookTitled(t, f, uid, "Infinite Jest"))
	if ij.Shelf != books.ShelfDNF || ij.Latest.FinishedOn != "2021-06-01" || len(ij.Tags) != 0 {
		t.Errorf("Infinite Jest = %s %+v %v, want Did not finish on its Date Read, untagged", ij.Shelf, ij.Latest, ij.Tags)
	}
	ns, err := f.store.Notes(ctx, uid, lw.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 1 || ns[0].Body != "Lent to Sam in May." || !ns[0].CreatedAt.Equal(time.Date(2024, 2, 1, 0, 0, 0, 0, time.Local)) {
		t.Errorf("notes = %+v, want its Private Notes, dated Date Added", ns)
	}
	if items, err := f.store.List(ctx, f.bob.ID, books.ListQuery{Shelf: books.ShelfAll}); err != nil || len(items) != 0 {
		t.Errorf("Bob's books = %+v, %v; want none", items, err)
	}
}

func TestImportedBooksAreSearchable(t *testing.T) {
	f := newFixture(t)
	importFixture(t, f, f.alice.ID)
	for q, want := range map[string]books.MatchIn{"holden": books.MatchReview, "sam": books.MatchNote, "fagles": books.MatchBook, "expanse": books.MatchBook} {
		items, err := f.store.List(context.Background(), f.alice.ID, books.ListQuery{Shelf: books.ShelfAll, Q: q})
		if err != nil || len(items) != 1 || items[0].Match != want {
			t.Errorf("search %q = %+v, %v; want one book matched in %q", q, items, err, want)
		}
	}
}

func TestImportSkipsBooksAlreadyInTheLibrary(t *testing.T) {
	f := newFixture(t)
	uid := f.alice.ID
	byISBN := onShelf("Leviathan Wakes: The Expanse 1", books.ShelfRead) // another title, same ISBN
	byISBN.ISBN = "978-0-316-12908-4"
	addBook(t, f, uid, byISBN)
	byName := onShelf("GOOD OMENS: the nice and accurate prophecies of agnes nutter, witch", books.ShelfWant)
	byName.Authors = "terry pratchett & neil gaiman"
	addBook(t, f, uid, byName)
	// A row with an ISBN matches by ISBN alone: the same title with
	// another ISBN is another edition, and is imported.
	otherEdition := onShelf("The Remains of the Day", books.ShelfWant)
	otherEdition.Authors, otherEdition.ISBN = "Kazuo Ishiguro", "9780571258246"
	addBook(t, f, uid, otherEdition)
	addBook(t, f, f.bob.ID, onShelf("Piranesi", books.ShelfWant)) // Bob's don't count

	res := importFixture(t, f, uid)
	if res.Imported != 4 || !reflect.DeepEqual(res.Skipped, []string{"Leviathan Wakes", goodOmens, goodOmens}) {
		t.Errorf("result = %+v, want 4 imported, Leviathan Wakes and both Good Omens skipped", res)
	}
	// A book typed in without an ISBN matches a row with one by its
	// title and authors.
	f2 := newFixture(t)
	bare := onShelf("the remains of the day", books.ShelfRead)
	bare.Authors = "KAZUO  ISHIGURO."
	addBook(t, f2, f2.alice.ID, bare)
	if res := importFixture(t, f2, f2.alice.ID); len(res.Skipped) != 2 || res.Skipped[0] != "The Remains of the Day" {
		t.Errorf("result = %+v, want The Remains of the Day skipped (and the second Good Omens)", res)
	}

	again := importFixture(t, f, uid)
	if again.Imported != 0 || len(again.Skipped) != 7 {
		t.Errorf("importing the same file again = %+v, want everything skipped", again)
	}
}

func TestImportIsAllOrNothing(t *testing.T) {
	f := newFixture(t)
	rows := []books.ImportBook{
		{BookInput: books.BookInput{Title: "Fine"}, Shelf: books.ShelfWant},
		{BookInput: books.BookInput{Title: "Broken"}, Shelf: books.ShelfWant, Rating: 9}, // the schema refuses it
	}
	if _, err := f.store.Import(context.Background(), f.alice.ID, rows); err == nil {
		t.Fatal("import with a bad row succeeded, want an error")
	}
	if counts, _ := f.store.ShelfCounts(context.Background(), f.alice.ID); counts[books.ShelfAll] != 0 {
		t.Errorf("%d books after a failed import, want none", counts[books.ShelfAll])
	}
}
