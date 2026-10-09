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

func titles(items []books.ListItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

func list(t *testing.T, f *fixture, q books.ListQuery) []string {
	t.Helper()
	items, err := f.store.List(context.Background(), f.alice.ID, q)
	if err != nil {
		t.Fatal(err)
	}
	return titles(items)
}

func TestShelfFollowsTheLatestReading(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfWant))
	step := func(name string, do func() error, want books.Shelf) {
		t.Helper()
		f.now = f.now.Add(time.Minute)
		if err := do(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if b := getBook(t, f, f.alice.ID, id); b.Shelf != want {
			t.Errorf("after %s, shelf = %q, want %q", name, b.Shelf, want)
		}
	}
	step("start", func() error { return f.store.StartReading(ctx, f.alice.ID, id) }, books.ShelfReading)
	step("finish", func() error { return f.store.FinishReading(ctx, f.alice.ID, id, "", 0) }, books.ShelfRead)
	step("re-read", func() error { return f.store.StartReading(ctx, f.alice.ID, id) }, books.ShelfReading)
	step("give up", func() error { return f.store.MarkDNF(ctx, f.alice.ID, id, "", 0) }, books.ShelfDNF)
}

func TestStartRefusesASecondReadingInProgress(t *testing.T) {
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfReading))
	err := f.store.StartReading(context.Background(), f.alice.ID, id)
	var ref *books.Refusal
	if !errors.As(err, &ref) {
		t.Errorf("second StartReading = %v, want a Refusal", err)
	}
}

func TestStartCarriesOverThePreviousFormat(t *testing.T) {
	f := newFixture(t)
	nb := onShelf("Dune", books.ShelfRead)
	id := addBook(t, f, f.alice.ID, nb)
	if _, err := f.db.Exec(`UPDATE books_readings SET format = 'audio' WHERE book_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Minute)
	if err := f.store.StartReading(context.Background(), f.alice.ID, id); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Latest.Format != "audio" || b.Latest.StartedOn != "2026-10-10" {
		t.Errorf("re-read = %+v, want audio, started 2026-10-10", b.Latest)
	}
}

func TestFinishUsesTheGivenDayAndChecksIt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.now = time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC) // 1 Oct in Melbourne
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfReading))
	f.now = time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC)

	for day, want := range map[string]string{
		"2026-09-30": "before you started",
		"2026-10-10": "in the future",
		"soon":       "Enter a date",
	} {
		err := f.store.FinishReading(ctx, f.alice.ID, id, day, 0)
		var ref *books.Refusal
		if !errors.As(err, &ref) || !strings.Contains(ref.Msg, want) {
			t.Errorf("FinishReading(%q) = %v, want a Refusal saying %q", day, err, want)
		}
	}
	if err := f.store.FinishReading(ctx, f.alice.ID, id, "2026-10-05", 0); err != nil {
		t.Fatal(err)
	}
	b := getBook(t, f, f.alice.ID, id)
	if b.Latest.Status != books.StatusFinished || b.Latest.StartedOn != "2026-10-01" || b.Latest.FinishedOn != "2026-10-05" {
		t.Errorf("latest = %+v, want finished 2026-10-01 → 2026-10-05", b.Latest)
	}
}

func TestDatesBeforeMinYearAreRefused(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.now = noon("2026-10-09")
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfReading))

	for _, day := range []string{"0026-03-01", "1899-12-31"} {
		var ref *books.Refusal
		err := f.store.FinishReading(ctx, f.alice.ID, id, day, 0)
		if !errors.As(err, &ref) || !strings.Contains(ref.Msg, "1900") {
			t.Errorf("FinishReading(%q) = %v, want a Refusal naming 1900", day, err)
		}
		nb := onShelf("Emma", books.ShelfRead)
		nb.FinishedOn = day
		if _, err := f.store.Create(ctx, f.alice.ID, nb); !errors.As(err, &ref) {
			t.Errorf("Add read on %q = %v, want a Refusal", day, err)
		}
	}
	nb := onShelf("Emma", books.ShelfRead)
	nb.FinishedOn = "1900-01-01"
	if _, err := f.store.Create(ctx, f.alice.ID, nb); err != nil {
		t.Errorf("Add read on 1900-01-01 = %v, want it accepted", err)
	}
}

func TestFinishWithNothingInProgressIsRefused(t *testing.T) {
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfWant))
	var ref *books.Refusal
	if err := f.store.MarkDNF(context.Background(), f.alice.ID, id, "", 0); !errors.As(err, &ref) {
		t.Errorf("MarkDNF on a want-to-read book = %v, want a Refusal", err)
	}
}

func TestReadingActionsAreScopedToTheOwner(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	want := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfWant))
	reading := addBook(t, f, f.alice.ID, onShelf("Emma", books.ShelfReading))
	if err := f.store.StartReading(ctx, f.bob.ID, want); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's StartReading = %v, want ErrNotFound", err)
	}
	if err := f.store.FinishReading(ctx, f.bob.ID, reading, "", 0); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's FinishReading = %v, want ErrNotFound", err)
	}
}

func TestListShowsOneShelfInItsOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// Want to read: newest added first.
	addBook(t, f, f.alice.ID, onShelf("Want A", books.ShelfWant))
	f.now = f.now.Add(time.Hour)
	addBook(t, f, f.alice.ID, onShelf("Want B", books.ShelfWant))
	// Reading: latest start first.
	f.now = time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	addBook(t, f, f.alice.ID, onShelf("Reading A", books.ShelfReading))
	f.now = time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	addBook(t, f, f.alice.ID, onShelf("Reading B", books.ShelfReading))
	// Read: latest finish first, whatever order they were added in.
	f.now = time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC)
	for title, day := range map[string]string{"Read A": "2026-09-01", "Read B": "2026-09-20"} {
		nb := onShelf(title, books.ShelfRead)
		nb.FinishedOn = day
		addBook(t, f, f.alice.ID, nb)
	}
	gone := addBook(t, f, f.alice.ID, onShelf("Gave up", books.ShelfReading))
	if err := f.store.MarkDNF(ctx, f.alice.ID, gone, "", 0); err != nil {
		t.Fatal(err)
	}

	for shelf, want := range map[books.Shelf][]string{
		books.ShelfWant:    {"Want B", "Want A"},
		books.ShelfReading: {"Reading B", "Reading A"},
		books.ShelfRead:    {"Read B", "Read A"},
		books.ShelfDNF:     {"Gave up"},
	} {
		if got := list(t, f, books.ListQuery{Shelf: shelf}); !slices.Equal(got, want) {
			t.Errorf("List(%s) = %v, want %v", shelf, got, want)
		}
	}
	if got := list(t, f, books.ListQuery{Shelf: books.ShelfAll}); len(got) != 7 {
		t.Errorf("List(all) = %v, want all 7 books", got)
	}
	counts, err := f.store.ShelfCounts(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[books.Shelf]int{books.ShelfWant: 2, books.ShelfReading: 2, books.ShelfRead: 2, books.ShelfDNF: 1, books.ShelfAll: 7}
	for shelf, n := range want {
		if counts[shelf] != n {
			t.Errorf("ShelfCounts()[%s] = %d, want %d", shelf, counts[shelf], n)
		}
	}
}

func TestListFiltersByTagAndText(t *testing.T) {
	f := newFixture(t)
	dispossessed := onShelf("The Dispossessed", books.ShelfWant)
	dispossessed.Authors = "Ursula K. Le Guin"
	dispossessed.Tags = []string{"sf"}
	addBook(t, f, f.alice.ID, dispossessed)
	earthsea := onShelf("A Wizard of Earthsea", books.ShelfRead)
	earthsea.Authors = "Ursula K. Le Guin"
	earthsea.SeriesName = "Earthsea"
	addBook(t, f, f.alice.ID, earthsea)
	addBook(t, f, f.alice.ID, onShelf("100% Real", books.ShelfWant))
	addBook(t, f, f.alice.ID, onShelf("1000 Years", books.ShelfWant))
	addBook(t, f, f.bob.ID, onShelf("Bob's book about Le Guin", books.ShelfWant))

	tests := []struct {
		q    books.ListQuery
		want []string
	}{
		{books.ListQuery{Shelf: books.ShelfAll, Q: "le guin"}, []string{"A Wizard of Earthsea", "The Dispossessed"}},
		{books.ListQuery{Shelf: books.ShelfWant, Q: "le guin"}, []string{"The Dispossessed"}},
		{books.ListQuery{Shelf: books.ShelfAll, Q: "earthsea"}, []string{"A Wizard of Earthsea"}},
		// Punctuation separates words, and a word matches as a prefix
		// (full-text search, B3).
		{books.ListQuery{Shelf: books.ShelfAll, Q: "100%"}, []string{"100% Real", "1000 Years"}},
		{books.ListQuery{Shelf: books.ShelfAll, Tag: "SF"}, []string{"The Dispossessed"}},
		{books.ListQuery{Shelf: books.ShelfAll, Tag: "nope"}, nil},
	}
	for _, tt := range tests {
		got := list(t, f, tt.q)
		slices.Sort(got)
		if !slices.Equal(got, tt.want) {
			t.Errorf("List(%+v) = %v, want %v", tt.q, got, tt.want)
		}
	}
}

func TestShowDay(t *testing.T) {
	if got := books.ShowDay("2026-10-09"); got != "9 Oct 2026" {
		t.Errorf("ShowDay = %q, want 9 Oct 2026", got)
	}
	if got := books.ShowDay("garbage"); got != "garbage" {
		t.Errorf("ShowDay(garbage) = %q, want it unchanged", got)
	}
}
