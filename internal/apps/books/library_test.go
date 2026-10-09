package books_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// addBook creates a book or fails the test.
func addBook(t *testing.T, f *fixture, userID int64, nb books.NewBook) int64 {
	t.Helper()
	id, err := f.store.Create(context.Background(), userID, nb)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// onShelf is a minimal new book for a shelf.
func onShelf(title string, shelf books.Shelf) books.NewBook {
	return books.NewBook{BookInput: books.BookInput{Title: title}, Shelf: shelf}
}

func getBook(t *testing.T, f *fixture, userID, id int64) books.Book {
	t.Helper()
	b, err := f.store.Get(context.Background(), userID, id)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCreateWantToReadHasNoReading(t *testing.T) {
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, books.NewBook{
		BookInput: books.BookInput{Title: " Piranesi ", Authors: "Susanna Clarke", Year: 2020, Pages: 272},
		Shelf:     books.ShelfWant,
		Tags:      []string{"Fantasy", "#fantasy", "favourites"},
	})
	b := getBook(t, f, f.alice.ID, id)
	if b.Title != "Piranesi" || b.Authors != "Susanna Clarke" || b.Year != 2020 || b.Pages != 272 {
		t.Errorf("details = %+v", b.BookInput)
	}
	if b.Shelf != books.ShelfWant || b.Latest.ID != 0 {
		t.Errorf("shelf = %q, latest = %+v; want want and no reading", b.Shelf, b.Latest)
	}
	if !slices.Equal(b.Tags, []string{"fantasy", "favourites"}) {
		t.Errorf("tags = %v, want [fantasy favourites]", b.Tags)
	}
	if !b.AddedAt.Equal(f.now) || !b.UpdatedAt.Equal(f.now) {
		t.Errorf("added %v, updated %v; want both %v", b.AddedAt, b.UpdatedAt, f.now)
	}
}

func TestCreateReadingStartsOnTheLocalDay(t *testing.T) {
	f := newFixture(t)
	b := getBook(t, f, f.alice.ID, addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfReading)))
	if b.Shelf != books.ShelfReading || b.Latest.Status != books.StatusReading || b.Latest.StartedOn != "2026-10-10" {
		t.Errorf("shelf %q, latest %+v; want reading since 2026-10-10", b.Shelf, b.Latest)
	}
}

func TestCreateAlreadyReadUsesTheFinishDate(t *testing.T) {
	f := newFixture(t)
	nb := onShelf("Emma", books.ShelfRead)
	nb.FinishedOn = "2026-09-30"
	b := getBook(t, f, f.alice.ID, addBook(t, f, f.alice.ID, nb))
	if b.Shelf != books.ShelfRead || b.Latest.Status != books.StatusFinished ||
		b.Latest.FinishedOn != "2026-09-30" || b.Latest.StartedOn != "" {
		t.Errorf("shelf %q, latest %+v; want read, finished 2026-09-30, no start", b.Shelf, b.Latest)
	}
	today := getBook(t, f, f.alice.ID, addBook(t, f, f.alice.ID, onShelf("Ulysses", books.ShelfRead)))
	if today.Latest.FinishedOn != "2026-10-10" {
		t.Errorf("finished %q with no date, want today 2026-10-10", today.Latest.FinishedOn)
	}
}

func TestCreateRefusesBadFinishDates(t *testing.T) {
	f := newFixture(t)
	for _, day := range []string{"2026-10-11", "2026-02-30", "yesterday"} {
		nb := onShelf("Emma", books.ShelfRead)
		nb.FinishedOn = day
		_, err := f.store.Create(context.Background(), f.alice.ID, nb)
		var ref *books.Refusal
		if !errors.As(err, &ref) || ref.Msg == "" || !errors.Is(err, books.ErrInvalid) {
			t.Errorf("Create with finish %q: err = %v, want a Refusal", day, err)
		}
	}
}

func TestCreateRejectsBadDetails(t *testing.T) {
	f := newFixture(t)
	_, err := f.store.Create(context.Background(), f.alice.ID, onShelf("  ", books.ShelfWant))
	var verr *books.ValidationError
	if !errors.As(err, &verr) || verr.Fields["title"] == "" {
		t.Errorf("err = %v, want a ValidationError for title", err)
	}
}

func TestCreateRejectsAShelfYouCannotAddTo(t *testing.T) {
	f := newFixture(t)
	for _, shelf := range []books.Shelf{books.ShelfAll, books.ShelfDNF, "lent"} {
		if _, err := f.store.Create(context.Background(), f.alice.ID, onShelf("x", shelf)); !errors.Is(err, books.ErrInvalid) {
			t.Errorf("Create on %q: err = %v, want ErrInvalid", shelf, err)
		}
	}
}

func TestCreateStoresISBN13(t *testing.T) {
	f := newFixture(t)
	nb := onShelf("Some book", books.ShelfWant)
	nb.ISBN = "0-306-40615-2"
	if b := getBook(t, f, f.alice.ID, addBook(t, f, f.alice.ID, nb)); b.ISBN != "9780306406157" {
		t.Errorf("ISBN = %q, want 9780306406157", b.ISBN)
	}
}

func TestGetIsScopedToItsOwner(t *testing.T) {
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfWant))
	if _, err := f.store.Get(context.Background(), f.bob.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's Get = %v, want ErrNotFound", err)
	}
}

func TestUpdateChangesDetails(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfReading))
	f.now = f.now.Add(time.Hour)
	in := books.BookInput{Title: "Dune", Authors: "Frank Herbert", SeriesName: "Dune", SeriesNumber: "1", Pages: 412}
	if err := f.store.Update(ctx, f.alice.ID, id, in); err != nil {
		t.Fatal(err)
	}
	b := getBook(t, f, f.alice.ID, id)
	if b.Authors != "Frank Herbert" || b.SeriesNumber != "1" || b.Pages != 412 || !b.UpdatedAt.Equal(f.now) {
		t.Errorf("after update: %+v, updated %v", b.BookInput, b.UpdatedAt)
	}
	if b.Shelf != books.ShelfReading {
		t.Errorf("shelf = %q; editing details must not touch readings", b.Shelf)
	}
	if err := f.store.Update(ctx, f.bob.ID, id, in); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's Update = %v, want ErrNotFound", err)
	}
	var verr *books.ValidationError
	if err := f.store.Update(ctx, f.alice.ID, id, books.BookInput{}); !errors.As(err, &verr) {
		t.Errorf("Update with no title = %v, want a ValidationError", err)
	}
}

func TestDeleteTakesReadingsAndUnusedTags(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first := onShelf("Dune", books.ShelfReading)
	first.Tags = []string{"sf", "classics"}
	id := addBook(t, f, f.alice.ID, first)
	second := onShelf("Hyperion", books.ShelfWant)
	second.Tags = []string{"sf"}
	addBook(t, f, f.alice.ID, second)

	if err := f.store.Delete(ctx, f.bob.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's Delete = %v, want ErrNotFound", err)
	}
	if err := f.store.Delete(ctx, f.alice.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Get(ctx, f.alice.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
	var readings int
	if err := f.db.QueryRow(`SELECT count(*) FROM books_readings WHERE book_id = ?`, id).Scan(&readings); err != nil || readings != 0 {
		t.Errorf("readings left = %d, %v; want 0", readings, err)
	}
	tags, err := f.store.TagNames(ctx, f.alice.ID)
	if err != nil || !slices.Equal(tags, []string{"sf"}) {
		t.Errorf("TagNames = %v, %v; want [sf]", tags, err)
	}
}
