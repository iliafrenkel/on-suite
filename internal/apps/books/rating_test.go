package books_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

func TestFinishCanRateTheBook(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := inProgress(t, f, "Dune", 600)
	if err := f.store.FinishReading(ctx, f.alice.ID, id, "", 4); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Rating != 4 || b.Shelf != books.ShelfRead {
		t.Errorf("after finishing with 4 stars: rating %d, shelf %q", b.Rating, b.Shelf)
	}
	// A re-read finished without a rating keeps the one it had.
	f.now = f.now.Add(time.Minute)
	if err := f.store.StartReading(ctx, f.alice.ID, id); err != nil {
		t.Fatal(err)
	}
	if err := f.store.FinishReading(ctx, f.alice.ID, id, "", 0); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Rating != 4 {
		t.Errorf("rating after an unrated re-read = %d, want 4 kept", b.Rating)
	}
}

func TestFinishRefusesABadRating(t *testing.T) {
	f := newFixture(t)
	id := inProgress(t, f, "Dune", 600)
	for _, rating := range []int{-1, 6} {
		if err := f.store.FinishReading(context.Background(), f.alice.ID, id, "", rating); !errors.Is(err, books.ErrInvalid) {
			t.Errorf("FinishReading rating %d = %v, want ErrInvalid", rating, err)
		}
	}
	if b := getBook(t, f, f.alice.ID, id); b.Shelf != books.ShelfReading {
		t.Errorf("shelf = %q after refused finishes, want reading", b.Shelf)
	}
}

func TestDNFCanSayWhereItStopped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := inProgress(t, f, "Infinite Jest", 1079)
	var ref *books.Refusal
	if err := f.store.MarkDNF(ctx, f.alice.ID, id, "", 2000); !errors.As(err, &ref) || ref.Msg != "Enter a page from 0 to 1079." {
		t.Errorf("DNF past the last page = %v, want a Refusal", err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Shelf != books.ShelfReading || b.Progress.Set() {
		t.Fatalf("after a refused DNF: shelf %q, progress %+v; want nothing changed", b.Shelf, b.Progress)
	}
	if err := f.store.MarkDNF(ctx, f.alice.ID, id, "", 250); err != nil {
		t.Fatal(err)
	}
	b := getBook(t, f, f.alice.ID, id)
	if b.Shelf != books.ShelfDNF || b.Progress.Unit != books.UnitPage || b.Progress.Value != 250 {
		t.Errorf("after DNF at 250: shelf %q, progress %+v", b.Shelf, b.Progress)
	}
}

func TestSetRating(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfRead))
	if err := f.store.SetRating(ctx, f.alice.ID, id, 5); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Rating != 5 {
		t.Errorf("rating = %d, want 5", b.Rating)
	}
	items, err := f.store.List(ctx, f.alice.ID, books.ListQuery{Shelf: books.ShelfRead})
	if err != nil || len(items) != 1 || items[0].Rating != 5 {
		t.Errorf("List = %+v, %v; want the rating on the row", items, err)
	}
	if err := f.store.SetRating(ctx, f.alice.ID, id, 0); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Rating != 0 {
		t.Errorf("rating after clearing = %d, want 0", b.Rating)
	}
	if err := f.store.SetRating(ctx, f.alice.ID, id, 6); !errors.Is(err, books.ErrInvalid) {
		t.Errorf("SetRating(6) = %v, want ErrInvalid", err)
	}
	if err := f.store.SetRating(ctx, f.bob.ID, id, 3); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's SetRating = %v, want ErrNotFound", err)
	}
}

func TestSetReview(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfRead))
	if err := f.store.SetReview(ctx, f.alice.ID, id, "  Sand.\r\n\r\nSo much **sand**.  \n"); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Review != "Sand.\n\nSo much **sand**." {
		t.Errorf("review = %q", b.Review)
	}
	var ref *books.Refusal
	if err := f.store.SetReview(ctx, f.alice.ID, id, strings.Repeat("a", books.MaxReviewRunes+1)); !errors.As(err, &ref) {
		t.Errorf("an over-long review = %v, want a Refusal", err)
	}
	if err := f.store.SetReview(ctx, f.bob.ID, id, "mine now"); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's SetReview = %v, want ErrNotFound", err)
	}
	if err := f.store.SetReview(ctx, f.alice.ID, id, ""); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Review != "" {
		t.Errorf("review after clearing = %q", b.Review)
	}
}

func TestDNFAtTheLatestProgressAddsNoRow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	count := func() int {
		var n int
		if err := f.db.QueryRow(`SELECT count(*) FROM books_progress`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	id := inProgress(t, f, "Infinite Jest", 1079)
	if err := f.store.RecordProgress(ctx, f.alice.ID, id, 250); err != nil {
		t.Fatal(err)
	}
	if err := f.store.MarkDNF(ctx, f.alice.ID, id, "", 250); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 1 {
		t.Errorf("progress rows after DNF at the same page = %d, want 1", n)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Shelf != books.ShelfDNF || b.Progress.Value != 250 {
		t.Errorf("after DNF: shelf %q, progress %+v; want dnf at 250", b.Shelf, b.Progress)
	}

	other := inProgress(t, f, "Emma", 500)
	if err := f.store.RecordProgress(ctx, f.alice.ID, other, 100); err != nil {
		t.Fatal(err)
	}
	if err := f.store.MarkDNF(ctx, f.alice.ID, other, "", 120); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 3 {
		t.Errorf("progress rows after DNF at a different page = %d, want 3", n)
	}
}
