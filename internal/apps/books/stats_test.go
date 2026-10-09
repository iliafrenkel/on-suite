package books_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// startOn starts reading book id at noon on day, in format ("": not set).
func startOn(t *testing.T, f *fixture, userID, id int64, format, day string) {
	t.Helper()
	ctx := context.Background()
	f.now = noon(day)
	if err := f.store.StartReading(ctx, userID, id); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetFormat(ctx, userID, id, format); err != nil {
		t.Fatal(err)
	}
}

// progressOn records progress on the reading of book id at noon on day.
func progressOn(t *testing.T, f *fixture, userID, id int64, day string, value int) {
	t.Helper()
	f.now = noon(day)
	if err := f.store.RecordProgress(context.Background(), userID, id, value); err != nil {
		t.Fatal(err)
	}
}

// finishOn finishes the reading of book id on day, at noon that day.
func finishOn(t *testing.T, f *fixture, userID, id int64, day string) {
	t.Helper()
	f.now = noon(day)
	if err := f.store.FinishReading(context.Background(), userID, id, day, 0); err != nil {
		t.Fatal(err)
	}
}

// rate gives book id a rating.
func rate(t *testing.T, f *fixture, userID, id int64, rating int) {
	t.Helper()
	if err := f.store.SetRating(context.Background(), userID, id, rating); err != nil {
		t.Fatal(err)
	}
}

func stats(t *testing.T, f *fixture, userID int64, year int) books.Stats {
	t.Helper()
	s, err := f.store.Stats(context.Background(), userID, year)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStatsCountTheYearAndAllTime(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	uid := f.alice.ID

	f.now = noon("2025-06-01")
	old := finished(t, f, uid, "Old", 200, "2025-06-01")
	rate(t, f, uid, old, 3)

	f.now = noon("2026-02-10")
	dune := finished(t, f, uid, "Dune", 600, "2026-02-10") // no progress: all 600 on the day
	rate(t, f, uid, dune, 4)

	emma := withPages(t, f, "Emma", 300)
	startOn(t, f, uid, emma, "ebook", "2026-03-01")
	progressOn(t, f, uid, emma, "2026-03-01", 100)
	progressOn(t, f, uid, emma, "2026-03-02", 100) // a repeated value adds nothing (#576)
	progressOn(t, f, uid, emma, "2026-03-03", 250)
	finishOn(t, f, uid, emma, "2026-03-05") // the last 50
	rate(t, f, uid, emma, 5)

	f.now = noon("2026-03-20")
	finished(t, f, uid, "Short", 90, "2026-03-20") // unrated

	f.now = noon("2026-04-01")
	unpaged := finished(t, f, uid, "Unpaged", 0, "2026-04-01")
	rate(t, f, uid, unpaged, 2)

	startOn(t, f, uid, dune, "audio", "2026-08-01") // a re-read, in percent
	progressOn(t, f, uid, dune, "2026-08-02", 50)   // 300 of 600 pages
	finishOn(t, f, uid, dune, "2026-08-20")         // and the other 300

	long := withPages(t, f, "Long", 1000)
	startOn(t, f, uid, long, "paper", "2026-09-01")
	progressOn(t, f, uid, long, "2026-09-02", 120) // pages count; the DNF doesn't
	f.now = noon("2026-09-05")
	if err := f.store.MarkDNF(ctx, uid, long, "2026-09-05", 0); err != nil {
		t.Fatal(err)
	}

	ancient := withPages(t, f, "Ancient", 400) // an imported read with no date
	rate(t, f, uid, ancient, 1)
	if _, err := f.db.Exec(`INSERT INTO books_readings (book_id, status, created_at) VALUES (?, 'finished', ?)`,
		ancient, db.FormatTime(f.now)); err != nil {
		t.Fatal(err)
	}
	f.now = noon("2026-03-01")
	finished(t, f, f.bob.ID, "Bob's book", 300, "2026-03-01")
	f.now = noon("2026-10-10")

	s := stats(t, f, uid, 2026)
	y := s.Year
	if y.Year != 2026 || y.Finished != 5 {
		t.Errorf("2026 finished = %d, want 5 (Dune twice, Emma, Short, Unpaged)", y.Finished)
	}
	if y.Pages != 1710 {
		t.Errorf("2026 pages = %d, want 600 + 300 + 90 + 600 + 120 = 1710", y.Pages)
	}
	if want := 11.0 / 3; y.Rating != want {
		t.Errorf("2026 rating = %v, want %v (Dune 4, Emma 5, Unpaged 2; each book once)", y.Rating, want)
	}
	wantFormats := []books.FormatCount{{Format: "ebook", N: 1}, {Format: "audio", N: 1}, {Format: "", N: 3}}
	if !slices.Equal(y.Formats, wantFormats) {
		t.Errorf("2026 formats = %v, want %v", y.Formats, wantFormats)
	}
	if y.Longest.ID != dune || y.Longest.Pages != 600 || y.Longest.Title != "Dune" {
		t.Errorf("longest = %+v, want Dune, 600", y.Longest)
	}
	if y.Shortest.Title != "Short" || y.Shortest.Pages != 90 {
		t.Errorf("shortest = %+v, want Short, 90 (a book with no page count isn't one)", y.Shortest)
	}
	if want := [12]int{0, 1, 2, 1, 0, 0, 0, 1}; y.ByMonth != want {
		t.Errorf("by month = %v, want %v", y.ByMonth, want)
	}

	a := s.All
	if a.Finished != 7 {
		t.Errorf("all-time finished = %d, want 7 (2026's 5, Old, and Ancient with no date)", a.Finished)
	}
	if a.Pages != 1910 {
		t.Errorf("all-time pages = %d, want 1710 + Old's 200", a.Pages)
	}
	if a.Rating != 3 {
		t.Errorf("all-time rating = %v, want 3 (4, 5, 2, 3, 1)", a.Rating)
	}
	if want := []books.YearCount{{Year: 2025, N: 1}, {Year: 2026, N: 5}}; !slices.Equal(a.ByYear, want) {
		t.Errorf("by year = %v, want %v", a.ByYear, want)
	}
	if a.First != 2025 {
		t.Errorf("first year = %d, want 2025", a.First)
	}

	if past := stats(t, f, uid, 2025).Year; past.Finished != 1 || past.Pages != 200 || past.Rating != 3 {
		t.Errorf("2025 = %+v, want Old: 1 book, 200 pages, rated 3", past)
	}
}

func TestStatsCountPagesOnTheLocalDay(t *testing.T) {
	f := newFixture(t)
	uid := f.alice.ID
	id := withPages(t, f, "Dune", 600)
	startOn(t, f, uid, id, "paper", "2025-12-30")
	// 14:00 UTC on 31 December is 1 January in Melbourne (TestMain's zone).
	f.now = time.Date(2025, 12, 31, 14, 0, 0, 0, time.UTC)
	if err := f.store.RecordProgress(context.Background(), uid, id, 40); err != nil {
		t.Fatal(err)
	}
	f.now = noon("2026-10-10")
	if got := stats(t, f, uid, 2025).Year.Pages; got != 0 {
		t.Errorf("2025 pages = %d, want 0: the update was on 1 January, local time", got)
	}
	if got := stats(t, f, uid, 2026).Year.Pages; got != 40 {
		t.Errorf("2026 pages = %d, want 40", got)
	}
	if got := stats(t, f, uid, 2026).All.First; got != 2026 {
		t.Errorf("first year = %d, want 2026 (the local day of the only update)", got)
	}
}

func TestStatsTiesGoToTheBookFinishedFirst(t *testing.T) {
	f := newFixture(t)
	uid := f.alice.ID
	finished(t, f, uid, "Later", 300, "2026-05-01")
	finished(t, f, uid, "Earlier", 300, "2026-02-01")
	y := stats(t, f, uid, 2026).Year
	if y.Longest.Title != "Earlier" || y.Shortest.Title != "Earlier" {
		t.Errorf("longest %q, shortest %q; want Earlier for both", y.Longest.Title, y.Shortest.Title)
	}
}

func TestStatsOfAnEmptyYear(t *testing.T) {
	f := newFixture(t) // 10 October 2026
	s := stats(t, f, f.alice.ID, 2026)
	if s.Year.Finished != 0 || s.Year.Pages != 0 || s.Year.Rating != 0 || s.Year.Formats != nil ||
		s.Year.Longest.ID != 0 || s.Year.Shortest.ID != 0 {
		t.Errorf("an empty year = %+v, want nothing counted", s.Year)
	}
	if s.All.ByYear != nil || s.All.First != 2026 || s.All.Finished != 0 {
		t.Errorf("all time with no books = %+v, want no years and this year first", s.All)
	}
}
