package books_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// noon is local noon on day (YYYY-MM-DD). Tests keyed to a local day use
// noon, never a time near midnight, so they hold in any zone (#521).
func noon(day string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		panic(err)
	}
	return t.Add(12 * time.Hour)
}

// finished adds one of userID's books as already read, finished on day.
func finished(t *testing.T, f *fixture, userID int64, title string, pages int, day string) int64 {
	t.Helper()
	nb := onShelf(title, books.ShelfRead)
	nb.Pages, nb.FinishedOn = pages, day
	return addBook(t, f, userID, nb)
}

// readAgain reads book id again: started at noon on from, finished on to.
func readAgain(t *testing.T, f *fixture, userID, id int64, from, to string) {
	t.Helper()
	ctx := context.Background()
	f.now = noon(from)
	if err := f.store.StartReading(ctx, userID, id); err != nil {
		t.Fatal(err)
	}
	f.now = noon(to)
	if err := f.store.FinishReading(ctx, userID, id, to, 0); err != nil {
		t.Fatal(err)
	}
}

func goal(t *testing.T, f *fixture, userID int64, year int) books.Goal {
	t.Helper()
	g, err := f.store.Goal(context.Background(), userID, year)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestExpectedIsTheTargetSoFarRoundedDown(t *testing.T) {
	tests := []struct {
		day    string
		target int
		want   int
	}{
		{"2026-01-01", 30, 0},    // 30 × 1 ÷ 365
		{"2026-01-13", 30, 1},    // 30 × 13 ÷ 365 = 1.07
		{"2026-01-12", 30, 0},    // 0.99
		{"2026-07-02", 365, 183}, // day 183
		{"2026-12-31", 30, 30},
		{"2024-12-31", 12, 12},   // a leap year has 366 days
		{"2024-07-01", 366, 183}, // day 183 of 366
		{"2026-10-10", 0, 0},
	}
	for _, tt := range tests {
		if got := books.Expected(tt.target, noon(tt.day)); got != tt.want {
			t.Errorf("Expected(%d, %s) = %d, want %d", tt.target, tt.day, got, tt.want)
		}
	}
}

func TestPaceSaysAheadOrBehind(t *testing.T) {
	today := noon("2026-07-02") // day 183 of 365: 30 × 183 ÷ 365 = 15 expected
	tests := []struct {
		name string
		g    books.Goal
		want string
	}{
		{"no goal", books.Goal{Year: 2026, Done: 4}, ""},
		{"ahead", books.Goal{Year: 2026, Target: 30, Done: 17}, "2 ahead"},
		{"behind", books.Goal{Year: 2026, Target: 30, Done: 14}, "1 behind"},
		{"on track", books.Goal{Year: 2026, Target: 30, Done: 15}, "on track"},
		{"reached", books.Goal{Year: 2026, Target: 30, Done: 30}, "goal reached"},
		{"past it", books.Goal{Year: 2026, Target: 30, Done: 31}, "goal reached"},
		{"a past year, short", books.Goal{Year: 2025, Target: 30, Done: 26}, "4 short"},
		{"a past year, reached", books.Goal{Year: 2025, Target: 30, Done: 30}, "goal reached"},
		{"next year", books.Goal{Year: 2027, Target: 30}, ""},
	}
	for _, tt := range tests {
		if got := tt.g.Pace(today); got != tt.want {
			t.Errorf("%s: Pace = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestGoalIsSetChangedAndCleared(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if g := goal(t, f, f.alice.ID, 2026); g != (books.Goal{Year: 2026}) {
		t.Errorf("no goal yet = %+v, want only the year", g)
	}
	if err := f.store.SetGoal(ctx, f.alice.ID, 2026, 30); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetGoal(ctx, f.alice.ID, 2026, 24); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetGoal(ctx, f.alice.ID, 2027, 40); err != nil {
		t.Fatal(err)
	}
	if g := goal(t, f, f.alice.ID, 2026); g.Target != 24 {
		t.Errorf("2026 goal = %d, want 24 after changing it", g.Target)
	}
	if g := goal(t, f, f.bob.ID, 2026); g.Target != 0 {
		t.Errorf("Bob's 2026 goal = %d, want none: goals are per user", g.Target)
	}
	if err := f.store.ClearGoal(ctx, f.alice.ID, 2026); err != nil {
		t.Fatal(err)
	}
	if err := f.store.ClearGoal(ctx, f.alice.ID, 2026); err != nil {
		t.Errorf("clearing a goal twice = %v, want nothing to happen", err)
	}
	if g := goal(t, f, f.alice.ID, 2026); g.Target != 0 {
		t.Errorf("2026 goal = %d after clearing it, want none", g.Target)
	}
	if g := goal(t, f, f.alice.ID, 2027); g.Target != 40 {
		t.Errorf("2027 goal = %d, want 40: clearing 2026 leaves it", g.Target)
	}
}

func TestSetGoalRefusesBadTargets(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, target := range []int{0, -1, books.MaxGoal + 1} {
		err := f.store.SetGoal(ctx, f.alice.ID, 2026, target)
		wantRefusal(t, fmt.Sprint("a goal of ", target), err, "Enter a goal from 1 to 1000 books.")
	}
	for _, year := range []int{books.MinYear - 1, 10000} {
		if err := f.store.SetGoal(ctx, f.alice.ID, year, 10); !errors.Is(err, books.ErrInvalid) {
			t.Errorf("a goal for %d = %v, want ErrInvalid", year, err)
		}
	}
	if err := f.store.SetGoal(ctx, f.alice.ID, 2026, books.MaxGoal); err != nil {
		t.Errorf("a goal of MaxGoal = %v, want it accepted", err)
	}
}

func TestGoalCountsTheYearsFinishedReadings(t *testing.T) {
	f := newFixture(t) // today is 10 October 2026 in Melbourne
	ctx := context.Background()
	uid := f.alice.ID
	if err := f.store.SetGoal(ctx, uid, 2026, 12); err != nil {
		t.Fatal(err)
	}
	finished(t, f, uid, "First day", 0, "2026-01-01")
	finished(t, f, uid, "Last year's last day", 0, "2025-12-31")
	dune := finished(t, f, uid, "Dune", 0, "2026-02-01")
	readAgain(t, f, uid, dune, "2026-05-01", "2026-05-20") // a re-read counts again
	f.now = noon("2026-10-10")
	dnf := addBook(t, f, uid, onShelf("Abandoned", books.ShelfReading))
	if err := f.store.MarkDNF(ctx, uid, dnf, "", 0); err != nil { // DNF never counts
		t.Fatal(err)
	}
	addBook(t, f, uid, onShelf("Still reading", books.ShelfReading))
	undated := addBook(t, f, uid, onShelf("Imported", books.ShelfWant)) // undated never counts
	if _, err := f.db.Exec(`INSERT INTO books_readings (book_id, status, created_at) VALUES (?, 'finished', ?)`,
		undated, db.FormatTime(f.now)); err != nil {
		t.Fatal(err)
	}
	finished(t, f, f.bob.ID, "Bob's book", 0, "2026-03-01")

	if g := goal(t, f, uid, 2026); g.Target != 12 || g.Done != 3 {
		t.Errorf("2026 = %+v, want 3 of 12 (1 Jan, Dune, Dune again)", g)
	}
	if g := goal(t, f, uid, 2025); g.Target != 0 || g.Done != 1 {
		t.Errorf("2025 = %+v, want 1 finished and no goal", g)
	}
}
