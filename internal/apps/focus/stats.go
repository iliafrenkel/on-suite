package focus

import (
	"context"
	"fmt"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// The local calendar (spec: "Weeks and days"): days in the server's local
// zone, weeks starting Monday. A session belongs to the day it started on.
// startOfDay mirrors ON Flash's (PATTERNS.md, "Cross-app mirroring"):
// walking days with AddDate keeps landing on midnight across a DST change.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Local().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

func startOfWeek(t time.Time) time.Time {
	day := startOfDay(t)
	sinceMonday := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -sinceMonday)
}

func startOfMonth(t time.Time) time.Time {
	y, m, _ := t.Local().Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.Local)
}

func startOfYear(t time.Time) time.Time {
	return time.Date(t.Local().Year(), 1, 1, 0, 0, 0, 0, time.Local)
}

// FormatFocus renders an amount of focus: "0m", "45m", "1h", "1h 40m".
// focus.js's focused agrees from a minute up.
func FormatFocus(seconds int) string {
	m := seconds / 60
	h, m := m/60, m%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// Totals is a user's focus, in seconds, over the calendar's spans.
type Totals struct {
	Today, Week, Month, Year int
	TodaySessions            int
	// Any is whether the user has recorded a session ever: the home page
	// shows its today strip only then (F3 plan).
	Any bool
}

// Totals adds up the user's focus today, this week, this month and this
// year, as of now. The week can begin last year (1 Jan on a Friday), so the
// scan starts at whichever of the two comes first.
func (st *Store) Totals(ctx context.Context, userID int64, now time.Time) (Totals, error) {
	day, week, month, year := startOfDay(now), startOfWeek(now), startOfMonth(now), startOfYear(now)
	from := year
	if week.Before(from) {
		from = week
	}
	var t Totals
	err := st.db.QueryRowContext(ctx, `
		SELECT coalesce(sum(CASE WHEN started_at >= ? THEN focus_seconds END), 0),
		       coalesce(sum(CASE WHEN started_at >= ? THEN 1 END), 0),
		       coalesce(sum(CASE WHEN started_at >= ? THEN focus_seconds END), 0),
		       coalesce(sum(CASE WHEN started_at >= ? THEN focus_seconds END), 0),
		       coalesce(sum(CASE WHEN started_at >= ? THEN focus_seconds END), 0),
		       EXISTS (SELECT 1 FROM focus_sessions WHERE user_id = ?)
		  FROM focus_sessions
		 WHERE user_id = ? AND started_at >= ?`,
		db.FormatTime(day), db.FormatTime(day), db.FormatTime(week), db.FormatTime(month),
		db.FormatTime(year), userID, userID, db.FormatTime(from),
	).Scan(&t.Today, &t.TodaySessions, &t.Week, &t.Month, &t.Year, &t.Any)
	if err != nil {
		return Totals{}, fmt.Errorf("focus: totals: %w", err)
	}
	return t, nil
}

// DayTotal is one local day's focus.
type DayTotal struct {
	Day     time.Time // local midnight
	Seconds int
}

// Daily is the user's focus per day for the last days days ending today,
// oldest first, including days with none — a chart that dropped quiet days
// would show a busier habit than the real one.
func (st *Store) Daily(ctx context.Context, userID int64, days int, now time.Time) ([]DayTotal, error) {
	if days <= 0 {
		return nil, nil
	}
	end := startOfDay(now)
	start := end.AddDate(0, 0, -(days - 1))
	rows, err := st.db.QueryContext(ctx, `
		SELECT started_at, focus_seconds FROM focus_sessions
		 WHERE user_id = ? AND started_at >= ? AND started_at < ?`,
		userID, db.FormatTime(start), db.FormatTime(end.AddDate(0, 0, 1)))
	if err != nil {
		return nil, fmt.Errorf("focus: daily: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byDay := map[string]int{}
	for rows.Next() {
		var started string
		var seconds int
		if err := rows.Scan(&started, &seconds); err != nil {
			return nil, fmt.Errorf("focus: daily: %w", err)
		}
		t, err := db.ParseTime(started)
		if err != nil {
			return nil, fmt.Errorf("focus: daily: %w", err)
		}
		byDay[startOfDay(t).Format("2006-01-02")] += seconds
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("focus: daily: %w", err)
	}
	out := make([]DayTotal, 0, days)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i)
		out = append(out, DayTotal{Day: d, Seconds: byDay[d.Format("2006-01-02")]})
	}
	return out, nil
}

// TimerTotal is one timer name's focus over a span.
type TimerTotal struct {
	Name, Color string
	Seconds     int
}

// ByTimer is the user's focus per timer name since since, largest first
// (spec: "History"). Sessions are grouped by the name they were recorded
// under, so a deleted timer still has its row. Color is that name's newest
// session's: with max(started_at) in the select, SQLite takes the bare
// color column from the row holding the max.
func (st *Store) ByTimer(ctx context.Context, userID int64, since time.Time) ([]TimerTotal, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT timer_name, color, sum(focus_seconds) AS total, max(started_at)
		  FROM focus_sessions
		 WHERE user_id = ? AND started_at >= ?
		 GROUP BY timer_name
		 ORDER BY total DESC, timer_name`,
		userID, db.FormatTime(since))
	if err != nil {
		return nil, fmt.Errorf("focus: by timer: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []TimerTotal
	for rows.Next() {
		var t TimerTotal
		var newest string
		if err := rows.Scan(&t.Name, &t.Color, &t.Seconds, &newest); err != nil {
			return nil, fmt.Errorf("focus: by timer: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("focus: by timer: %w", err)
	}
	return out, nil
}

// querySessions runs a SELECT of sessionColumns and reads every row.
func (st *Store) querySessions(ctx context.Context, query string, args ...any) ([]Session, error) {
	rows, err := st.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("focus: list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("focus: list sessions: %w", err)
	}
	return out, nil
}

// RecentSessions is one page of the user's sessions, newest first, and
// whether an older page exists. page counts from 1.
func (st *Store) RecentSessions(ctx context.Context, userID int64, page, perPage int) ([]Session, bool, error) {
	if page < 1 {
		page = 1
	}
	out, err := st.querySessions(ctx, `SELECT `+sessionColumns+` FROM focus_sessions
		 WHERE user_id = ? ORDER BY started_at DESC, id DESC LIMIT ? OFFSET ?`,
		userID, perPage+1, (page-1)*perPage)
	if err != nil {
		return nil, false, err
	}
	if len(out) > perPage {
		return out[:perPage], true, nil
	}
	return out, false, nil
}
