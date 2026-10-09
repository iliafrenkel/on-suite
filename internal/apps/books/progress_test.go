package books_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// inProgress adds one of Alice's books with pages pages (0: unknown) and
// starts it, so it has a reading in progress.
func inProgress(t *testing.T, f *fixture, title string, pages int) int64 {
	t.Helper()
	nb := onShelf(title, books.ShelfReading)
	nb.Pages = pages
	return addBook(t, f, f.alice.ID, nb)
}

func setFormat(t *testing.T, f *fixture, id int64, format string) {
	t.Helper()
	if _, err := f.db.Exec(`UPDATE books_readings SET format = ? WHERE book_id = ?`, format, id); err != nil {
		t.Fatal(err)
	}
}

func TestUnitFor(t *testing.T) {
	tests := []struct {
		format string
		pages  int
		want   books.Unit
	}{
		{"", 300, books.UnitPage},
		{"paper", 300, books.UnitPage},
		{"ebook", 300, books.UnitPage},
		{"audio", 300, books.UnitPercent},
		{"paper", 0, books.UnitPercent},
	}
	for _, tt := range tests {
		if got := books.UnitFor(tt.format, tt.pages); got != tt.want {
			t.Errorf("UnitFor(%q, %d) = %q, want %q", tt.format, tt.pages, got, tt.want)
		}
	}
}

func TestProgressConvertsBetweenUnits(t *testing.T) {
	page := books.Progress{Unit: books.UnitPage, Value: 150}
	percent := books.Progress{Unit: books.UnitPercent, Value: 40}
	tests := []struct {
		name string
		got  int
		want int
	}{
		{"page in pages", page.In(books.UnitPage, 300), 150},
		{"page in percent", page.In(books.UnitPercent, 300), 50},
		{"page in percent, no page count", page.In(books.UnitPercent, 0), 0},
		{"percent in pages", percent.In(books.UnitPage, 300), 120},
		{"percent in percent", percent.In(books.UnitPercent, 0), 40},
		{"nothing recorded", books.Progress{}.In(books.UnitPage, 300), 0},
		{"bar past the end", books.Progress{Unit: books.UnitPage, Value: 320}.Percent(300), 100},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

func TestRecordProgressInPages(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := inProgress(t, f, "Dune", 600)
	if p := getBook(t, f, f.alice.ID, id).Progress; p.Set() {
		t.Fatalf("new reading has progress %+v", p)
	}
	for _, page := range []int{40, 120} {
		f.now = f.now.Add(time.Hour)
		if err := f.store.RecordProgress(ctx, f.alice.ID, id, page); err != nil {
			t.Fatal(err)
		}
	}
	p := getBook(t, f, f.alice.ID, id).Progress
	if p.Unit != books.UnitPage || p.Value != 120 || !p.RecordedAt.Equal(f.now) || p.Percent(600) != 20 {
		t.Errorf("Progress = %+v, want page 120 at %v (20%%)", p, f.now)
	}
	var rows int
	if err := f.db.QueryRow(`SELECT count(*) FROM books_progress`).Scan(&rows); err != nil || rows != 2 {
		t.Errorf("progress rows = %d, %v; want both updates kept as history", rows, err)
	}
}

func TestRecordProgressInPercent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	audio := inProgress(t, f, "Dune", 600)
	setFormat(t, f, audio, "audio")
	unknown := inProgress(t, f, "Emma", 0)
	for _, id := range []int64{audio, unknown} {
		if err := f.store.RecordProgress(ctx, f.alice.ID, id, 35); err != nil {
			t.Fatal(err)
		}
		if p := getBook(t, f, f.alice.ID, id).Progress; p.Unit != books.UnitPercent || p.Value != 35 {
			t.Errorf("book %d progress = %+v, want 35%%", id, p)
		}
	}
}

func TestRecordProgressRefusesOutOfRange(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	paper := inProgress(t, f, "Dune", 600)
	audio := inProgress(t, f, "Emma", 300)
	setFormat(t, f, audio, "audio")
	tests := []struct {
		id    int64
		value int
		want  string
	}{
		{paper, -1, "Enter a page from 0 to 600."},
		{paper, 601, "Enter a page from 0 to 600."},
		{audio, 101, "Enter a percentage from 0 to 100."},
	}
	for _, tt := range tests {
		err := f.store.RecordProgress(ctx, f.alice.ID, tt.id, tt.value)
		var ref *books.Refusal
		if !errors.As(err, &ref) || ref.Msg != tt.want {
			t.Errorf("RecordProgress(%d) = %v, want a Refusal %q", tt.value, err, tt.want)
		}
	}
	if err := f.store.RecordProgress(ctx, f.alice.ID, paper, 600); err != nil {
		t.Errorf("the last page = %v, want it accepted", err)
	}
	if b := getBook(t, f, f.alice.ID, paper); b.Shelf != books.ShelfReading {
		t.Errorf("shelf after the last page = %q, want reading: reaching the end doesn't finish", b.Shelf)
	}
}

func TestRecordProgressNeedsAReadingInProgress(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	want := addBook(t, f, f.alice.ID, onShelf("Emma", books.ShelfWant))
	var ref *books.Refusal
	if err := f.store.RecordProgress(ctx, f.alice.ID, want, 10); !errors.As(err, &ref) || !strings.Contains(ref.Msg, "isn't being read") {
		t.Errorf("progress on a want-to-read book = %v, want a Refusal", err)
	}
	id := inProgress(t, f, "Dune", 600)
	if err := f.store.RecordProgress(ctx, f.bob.ID, id, 10); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's RecordProgress = %v, want ErrNotFound", err)
	}
}

func TestReadingShelfIsSortedByLatestProgress(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first := inProgress(t, f, "First started", 300)
	f.now = f.now.Add(time.Hour)
	inProgress(t, f, "Second started", 300)
	f.now = f.now.Add(time.Hour)
	if got := list(t, f, books.ListQuery{Shelf: books.ShelfReading}); !slices.Equal(got, []string{"Second started", "First started"}) {
		t.Fatalf("before any progress = %v, want the newest reading first", got)
	}
	if err := f.store.RecordProgress(ctx, f.alice.ID, first, 30); err != nil {
		t.Fatal(err)
	}
	items, err := f.store.List(ctx, f.alice.ID, books.ListQuery{Shelf: books.ShelfReading})
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(items); !slices.Equal(got, []string{"First started", "Second started"}) {
		t.Errorf("after progress = %v, want the book just updated first", got)
	}
	if it := items[0]; it.Pages != 300 || it.Progress.Value != 30 || it.Progress.Percent(it.Pages) != 10 {
		t.Errorf("row = %+v, want 300 pages at page 30 (10%%)", it)
	}
}

func TestDeletingABookTakesItsProgress(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := inProgress(t, f, "Dune", 600)
	if err := f.store.RecordProgress(ctx, f.alice.ID, id, 10); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Delete(ctx, f.alice.ID, id); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM books_progress`).Scan(&n); err != nil || n != 0 {
		t.Errorf("progress rows left = %d, %v; want 0", n, err)
	}
}
