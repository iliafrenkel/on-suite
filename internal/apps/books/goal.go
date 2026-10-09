package books

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// MaxGoal is the largest yearly goal, in books (decided 2026-10-09 while
// planning B4): room for any real reader, and a typo of 30000 is refused.
const MaxGoal = 1000

// MinYear is the earliest year the Stats page shows or takes a goal for.
const MinYear = 1900

// Goal is a year's reading goal and how far along it is (spec "Stats
// (B4)"). Target is 0 when no goal is set for the year; Done is the
// readings finished in it (spec "Derived values": a re-read counts again,
// DNF and undated readings never do).
type Goal struct {
	Year, Target, Done int
}

// Expected is how many books of target should be finished by the end of
// day, a local date (spec "Stats (B4)"): target × day-of-year ÷
// days-in-year, rounded down.
func Expected(target int, day time.Time) int {
	days := time.Date(day.Year(), time.December, 31, 12, 0, 0, 0, time.UTC).YearDay()
	return target * day.YearDay() / days
}

// Pace says how g stands on today, a local date: "goal reached" once it is
// met; for this year "2 ahead", "1 behind" or "on track" against Expected;
// for a past year "4 short"; nothing for a future year or with no goal
// (decided 2026-10-09 while planning B4).
func (g Goal) Pace(today time.Time) string {
	switch {
	case g.Target == 0:
		return ""
	case g.Done >= g.Target:
		return "goal reached"
	case g.Year < today.Year():
		return strconv.Itoa(g.Target-g.Done) + " short"
	case g.Year > today.Year():
		return ""
	}
	switch d := g.Done - Expected(g.Target, today); {
	case d > 0:
		return strconv.Itoa(d) + " ahead"
	case d < 0:
		return strconv.Itoa(-d) + " behind"
	}
	return "on track"
}

// yearSpan is a year's first and last day, YYYY-MM-DD: finished_on
// BETWEEN them is "finished in that year".
func yearSpan(year int) (string, string) {
	return fmt.Sprintf("%04d-01-01", year), fmt.Sprintf("%04d-12-31", year)
}

// Goal is userID's goal for year with the books finished in it so far.
func (st *Store) Goal(ctx context.Context, userID int64, year int) (Goal, error) {
	g := Goal{Year: year}
	from, to := yearSpan(year)
	err := st.db.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT target FROM books_goals WHERE user_id = ? AND year = ?), 0),
		       (SELECT count(*) FROM books_readings r JOIN books_books b ON b.id = r.book_id
		         WHERE b.user_id = ? AND r.status = 'finished' AND r.finished_on BETWEEN ? AND ?)`,
		userID, year, userID, from, to).Scan(&g.Target, &g.Done)
	if err != nil {
		return Goal{}, fmt.Errorf("books: goal: %w", err)
	}
	return g, nil
}

// SetGoal sets or changes userID's goal for year: target books, from 1 to
// MaxGoal (a Refusal otherwise). The year comes from a hidden field, so
// one outside MinYear–9999 is ErrInvalid.
func (st *Store) SetGoal(ctx context.Context, userID int64, year, target int) error {
	if year < MinYear || year > 9999 {
		return ErrInvalid
	}
	if target < 1 || target > MaxGoal {
		return &Refusal{Msg: fmt.Sprintf("Enter a goal from 1 to %d books.", MaxGoal)}
	}
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO books_goals (user_id, year, target) VALUES (?, ?, ?)
		ON CONFLICT (user_id, year) DO UPDATE SET target = excluded.target`,
		userID, year, target); err != nil {
		return fmt.Errorf("books: set goal: %w", err)
	}
	return nil
}

// ClearGoal removes userID's goal for year; with none set it does nothing.
func (st *Store) ClearGoal(ctx context.Context, userID int64, year int) error {
	if _, err := st.db.ExecContext(ctx,
		`DELETE FROM books_goals WHERE user_id = ? AND year = ?`, userID, year); err != nil {
		return fmt.Errorf("books: clear goal: %w", err)
	}
	return nil
}
