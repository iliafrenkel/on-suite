package books_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

func readings(t *testing.T, f *fixture, id int64) []books.Reading {
	t.Helper()
	rs, err := f.store.Readings(context.Background(), f.alice.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

// readTwice is a book Alice finished on 2026-10-02 and is reading again
// since 2026-10-05, with paper as the format of both readings.
func readTwice(t *testing.T, f *fixture) int64 {
	t.Helper()
	ctx := context.Background()
	f.now = time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	id := inProgress(t, f, "Dune", 600)
	if err := f.store.SetFormat(ctx, f.alice.ID, id, "paper"); err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	if err := f.store.FinishReading(ctx, f.alice.ID, id, "2026-10-02", 0); err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)
	if err := f.store.StartReading(ctx, f.alice.ID, id); err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC)
	return id
}

func TestReadingsAreTheHistoryNewestFirst(t *testing.T) {
	f := newFixture(t)
	id := readTwice(t, f)
	rs := readings(t, f, id)
	if len(rs) != 2 {
		t.Fatalf("readings = %+v, want 2", rs)
	}
	now, before := rs[0], rs[1]
	if now.Status != books.StatusReading || now.StartedOn != "2026-10-05" || now.Format != "paper" {
		t.Errorf("newest = %+v, want reading since 2026-10-05 on paper (carried over)", now)
	}
	if before.Status != books.StatusFinished || before.StartedOn != "2026-10-01" || before.FinishedOn != "2026-10-02" {
		t.Errorf("older = %+v, want finished 2026-10-01 → 2026-10-02", before)
	}
	if rs, err := f.store.Readings(context.Background(), f.bob.ID, id); err != nil || len(rs) != 0 {
		t.Errorf("Bob's Readings = %+v, %v; want none", rs, err)
	}
}

func TestSetFormatChangesTheReadingInProgress(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := readTwice(t, f)
	if err := f.store.SetFormat(ctx, f.alice.ID, id, "audio"); err != nil {
		t.Fatal(err)
	}
	rs := readings(t, f, id)
	if rs[0].Format != "audio" || rs[1].Format != "paper" {
		t.Errorf("formats = %q, %q; want audio now, paper before", rs[0].Format, rs[1].Format)
	}
	if err := f.store.SetFormat(ctx, f.alice.ID, id, "vinyl"); !errors.Is(err, books.ErrInvalid) {
		t.Errorf("SetFormat(vinyl) = %v, want ErrInvalid", err)
	}
	want := addBook(t, f, f.alice.ID, onShelf("Emma", books.ShelfWant))
	var ref *books.Refusal
	if err := f.store.SetFormat(ctx, f.alice.ID, want, "ebook"); !errors.As(err, &ref) {
		t.Errorf("SetFormat with nothing in progress = %v, want a Refusal", err)
	}
	if err := f.store.SetFormat(ctx, f.bob.ID, id, "ebook"); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's SetFormat = %v, want ErrNotFound", err)
	}
}

func TestUpdateReadingChangesDatesAndFormat(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := readTwice(t, f)
	rs := readings(t, f, id)
	past, current := rs[1].ID, rs[0].ID
	if err := f.store.UpdateReading(ctx, f.alice.ID, id, past, books.ReadingEdit{
		StartedOn: "2026-09-20", FinishedOn: "2026-09-28", Format: "ebook"}); err != nil {
		t.Fatal(err)
	}
	// A reading in progress keeps no finish date, whatever is sent.
	if err := f.store.UpdateReading(ctx, f.alice.ID, id, current, books.ReadingEdit{
		StartedOn: "", FinishedOn: "2026-10-08", Format: ""}); err != nil {
		t.Fatal(err)
	}
	rs = readings(t, f, id)
	if got := rs[1]; got.StartedOn != "2026-09-20" || got.FinishedOn != "2026-09-28" || got.Format != "ebook" || got.Status != books.StatusFinished {
		t.Errorf("past reading = %+v", got)
	}
	if got := rs[0]; got.StartedOn != "" || got.FinishedOn != "" || got.Format != "" || got.Status != books.StatusReading {
		t.Errorf("current reading = %+v, want no dates, no format, still reading", got)
	}
}

func TestUpdateReadingChecksItsInput(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := readTwice(t, f)
	past := readings(t, f, id)[1].ID
	var ref *books.Refusal
	for _, ed := range []books.ReadingEdit{
		{StartedOn: "2026-09-28", FinishedOn: "2026-09-20"},
		{StartedOn: "2026-10-20"},
		{FinishedOn: "yesterday"},
	} {
		if err := f.store.UpdateReading(ctx, f.alice.ID, id, past, ed); !errors.As(err, &ref) {
			t.Errorf("UpdateReading(%+v) = %v, want a Refusal", ed, err)
		}
	}
	if err := f.store.UpdateReading(ctx, f.alice.ID, id, past, books.ReadingEdit{Format: "scroll"}); !errors.Is(err, books.ErrInvalid) {
		t.Errorf("format scroll = %v, want ErrInvalid", err)
	}
	other := addBook(t, f, f.alice.ID, onShelf("Emma", books.ShelfWant))
	if err := f.store.UpdateReading(ctx, f.alice.ID, other, past, books.ReadingEdit{}); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("a reading through another book = %v, want ErrNotFound", err)
	}
	if err := f.store.UpdateReading(ctx, f.bob.ID, id, past, books.ReadingEdit{}); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's UpdateReading = %v, want ErrNotFound", err)
	}
	if got := readings(t, f, id)[1]; got.StartedOn != "2026-10-01" || got.FinishedOn != "2026-10-02" {
		t.Errorf("past reading changed by refused edits: %+v", got)
	}
}

func TestDeleteReadingMovesTheShelf(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := readTwice(t, f)
	if err := f.store.RecordProgress(ctx, f.alice.ID, id, 50); err != nil {
		t.Fatal(err)
	}
	rs := readings(t, f, id)
	if err := f.store.DeleteReading(ctx, f.bob.ID, id, rs[0].ID); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's DeleteReading = %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteReading(ctx, f.alice.ID, id, rs[0].ID); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Shelf != books.ShelfRead || b.Progress.Set() {
		t.Errorf("after deleting the re-read: shelf %q, progress %+v; want read, none", b.Shelf, b.Progress)
	}
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM books_progress`).Scan(&n); err != nil || n != 0 {
		t.Errorf("progress rows = %d, %v; want the deleted reading's gone", n, err)
	}
	if err := f.store.DeleteReading(ctx, f.alice.ID, id, rs[1].ID); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Shelf != books.ShelfWant {
		t.Errorf("with no readings left, shelf = %q, want want", b.Shelf)
	}
	if err := f.store.DeleteReading(ctx, f.alice.ID, id, rs[1].ID); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("deleting it again = %v, want ErrNotFound", err)
	}
}

func TestUpdateReadingInProgressKeepsStartAndFormatButNoFinish(t *testing.T) {
	f := newFixture(t)
	id := readTwice(t, f)
	current := readings(t, f, id)[0].ID
	if err := f.store.UpdateReading(context.Background(), f.alice.ID, id, current, books.ReadingEdit{
		StartedOn: "2026-10-04", FinishedOn: "", Format: "ebook"}); err != nil {
		t.Fatal(err)
	}
	got := readings(t, f, id)[0]
	if got.StartedOn != "2026-10-04" || got.Format != "ebook" || got.FinishedOn != "" || got.Status != books.StatusReading {
		t.Errorf("current reading = %+v, want started 2026-10-04 on ebook, no finish, still reading", got)
	}
}

func TestSetFormatEmptyClearsTheFormat(t *testing.T) {
	f := newFixture(t)
	id := readTwice(t, f)
	if err := f.store.SetFormat(context.Background(), f.alice.ID, id, ""); err != nil {
		t.Fatal(err)
	}
	rs := readings(t, f, id)
	if rs[0].Format != "" || rs[1].Format != "paper" {
		t.Errorf("formats = %q, %q; want cleared now, paper before", rs[0].Format, rs[1].Format)
	}
}
