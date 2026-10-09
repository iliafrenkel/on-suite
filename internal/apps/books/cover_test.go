package books_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// onePNG is the smallest thing http.DetectContentType calls an image/png
// (the same bytes Reader's image tests use).
var onePNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
}

func TestSetCoverAndReadItBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Piranesi", books.ShelfWant))
	if b := getBook(t, f, f.alice.ID, id); b.CoverVersion != "" {
		t.Fatalf("new book has cover version %q, want none", b.CoverVersion)
	}
	if err := f.store.SetCover(ctx, f.alice.ID, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	c, err := f.store.Cover(ctx, f.alice.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if c.ContentType != "image/png" || !bytes.Equal(c.Bytes, onePNG) || c.Version == "" {
		t.Errorf("Cover = %q, %d bytes, version %q", c.ContentType, len(c.Bytes), c.Version)
	}
	if b := getBook(t, f, f.alice.ID, id); b.CoverVersion != c.Version {
		t.Errorf("Book.CoverVersion = %q, want %q", b.CoverVersion, c.Version)
	}
	items, err := f.store.List(ctx, f.alice.ID, books.ListQuery{Shelf: books.ShelfAll})
	if err != nil || len(items) != 1 || items[0].CoverVersion != c.Version {
		t.Errorf("List = %+v, %v; want the cover version on the row", items, err)
	}
}

func TestReplacingACoverChangesItsVersion(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Piranesi", books.ShelfWant))
	if err := f.store.SetCover(ctx, f.alice.ID, id, "image/png", onePNG, books.CoverFromOL); err != nil {
		t.Fatal(err)
	}
	first, _ := f.store.Cover(ctx, f.alice.ID, id)
	f.now = f.now.Add(time.Second)
	if err := f.store.SetCover(ctx, f.alice.ID, id, "image/gif", []byte("GIF89a…"), books.CoverFromURL); err != nil {
		t.Fatal(err)
	}
	second, _ := f.store.Cover(ctx, f.alice.ID, id)
	if second.ContentType != "image/gif" || second.Version == first.Version {
		t.Errorf("after replacing: %q version %q (was %q)", second.ContentType, second.Version, first.Version)
	}
}

func TestCoversAreScopedToTheOwner(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Piranesi", books.ShelfWant))
	if _, err := f.store.Cover(ctx, f.alice.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Cover of a book without one = %v, want ErrNotFound", err)
	}
	if err := f.store.SetCover(ctx, f.bob.ID, id, "image/png", onePNG, books.CoverUpload); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's SetCover = %v, want ErrNotFound", err)
	}
	if err := f.store.SetCover(ctx, f.alice.ID, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Cover(ctx, f.bob.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's Cover = %v, want ErrNotFound", err)
	}
	if err := f.store.RemoveCover(ctx, f.bob.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's RemoveCover = %v, want ErrNotFound", err)
	}
}

func TestSetCoverRejectsAnUnknownSource(t *testing.T) {
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, onShelf("Piranesi", books.ShelfWant))
	if err := f.store.SetCover(context.Background(), f.alice.ID, id, "image/png", onePNG, "web"); !errors.Is(err, books.ErrInvalid) {
		t.Errorf("SetCover with source web = %v, want ErrInvalid", err)
	}
}

func TestRemoveCover(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Piranesi", books.ShelfWant))
	if err := f.store.SetCover(ctx, f.alice.ID, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RemoveCover(ctx, f.alice.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Cover(ctx, f.alice.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Cover after remove = %v, want ErrNotFound", err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.CoverVersion != "" {
		t.Errorf("CoverVersion after remove = %q", b.CoverVersion)
	}
}

func TestDeletingABookTakesItsCover(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Piranesi", books.ShelfWant))
	if err := f.store.SetCover(ctx, f.alice.ID, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Delete(ctx, f.alice.ID, id); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM books_covers`).Scan(&n); err != nil || n != 0 {
		t.Errorf("covers left = %d, %v; want 0", n, err)
	}
}

func TestCreateKeepsOnlyRealOpenLibraryIDs(t *testing.T) {
	f := newFixture(t)
	good := onShelf("Piranesi", books.ShelfWant)
	good.OLWorkID, good.OLEditionID = "OL20893680W", "OL28300471M"
	bad := onShelf("Other", books.ShelfWant)
	bad.OLWorkID, bad.OLEditionID = "OL28300471M", "/books/x" // an edition as the work, and junk
	for _, tt := range []struct {
		nb         books.NewBook
		work, edit string
	}{{good, "OL20893680W", "OL28300471M"}, {bad, "", ""}} {
		id := addBook(t, f, f.alice.ID, tt.nb)
		var work, edit string
		if err := f.db.QueryRow(`SELECT ol_work_id, ol_edition_id FROM books_books WHERE id = ?`, id).Scan(&work, &edit); err != nil {
			t.Fatal(err)
		}
		if work != tt.work || edit != tt.edit {
			t.Errorf("%s: stored %q / %q, want %q / %q", tt.nb.Title, work, edit, tt.work, tt.edit)
		}
	}
}
