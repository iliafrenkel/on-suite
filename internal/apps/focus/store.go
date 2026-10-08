// Package focus implements ON Focus: saved, reusable focus timers (single
// blocks or Pomodoro-style intervals) and a history of the sessions run with them.
package focus

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// ID is the app id: URL prefix, migration namespace, table prefix.
const ID = "focus"

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations is this app's schema, for the platform and for store tests.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		panic("focus: embedded migrations missing: " + err.Error()) // unreachable
	}
	return sub
}

var (
	// ErrNotFound is a missing row or somebody else's — indistinguishable
	// on purpose, so a handler answers 404 for both.
	ErrNotFound = errors.New("focus: not found")
	// ErrInvalid is a request the store refuses.
	ErrInvalid = errors.New("focus: invalid")
)

// Store is every SQL query ON Focus runs.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// NewStore returns a store on handle reading the real clock in UTC.
func NewStore(handle *sql.DB) *Store {
	return &Store{db: handle, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the time source, for tests.
func (st *Store) SetClock(now func() time.Time) { st.now = now }

// Timer is one saved timer.
type Timer struct {
	ID, UserID int64
	TimerInput
	Position             int
	CreatedAt, UpdatedAt time.Time
}

// timerColumns is every column scanTimer reads, in order.
const timerColumns = `id, user_id, name, color, kind, focus_minutes, break_minutes,
	long_break_minutes, rounds, long_break_every, auto_advance, chime, keep_history,
	position, created_at, updated_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanTimer(row rowScanner) (Timer, error) {
	var t Timer
	var brk, long, rounds, every sql.NullInt64
	var created, updated string
	err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.Color, &t.Kind, &t.FocusMinutes, &brk,
		&long, &rounds, &every, &t.AutoAdvance, &t.Chime, &t.KeepHistory,
		&t.Position, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Timer{}, ErrNotFound
	}
	if err != nil {
		return Timer{}, fmt.Errorf("focus: load timer: %w", err)
	}
	t.BreakMinutes, t.LongBreakMinutes = int(brk.Int64), int(long.Int64)
	t.Rounds, t.LongBreakEvery = int(rounds.Int64), int(every.Int64)
	if t.CreatedAt, err = db.ParseTime(created); err != nil {
		return Timer{}, fmt.Errorf("focus: timer created_at: %w", err)
	}
	if t.UpdatedAt, err = db.ParseTime(updated); err != nil {
		return Timer{}, fmt.Errorf("focus: timer updated_at: %w", err)
	}
	return t, nil
}

// checked normalizes and validates input, the one gate every write goes
// through.
func checked(in TimerInput) (TimerInput, error) {
	in = in.Normalize()
	if errs := in.Validate(); errs != nil {
		return in, &ValidationError{Fields: errs}
	}
	return in, nil
}

// nullable stores 0 as NULL: a single timer's interval columns.
func nullable(n int) sql.NullInt64 { return sql.NullInt64{Int64: int64(n), Valid: n != 0} }

// flag binds a bool as SQLite's 0/1.
func flag(b bool) int {
	if b {
		return 1
	}
	return 0
}

// CreateTimer stores a new timer at the end of the user's list.
func (st *Store) CreateTimer(ctx context.Context, userID int64, in TimerInput) (Timer, error) {
	in, err := checked(in)
	if err != nil {
		return Timer{}, err
	}
	now := db.FormatTime(st.now())
	res, err := st.db.ExecContext(ctx, `
		INSERT INTO focus_timers (user_id, name, color, kind, focus_minutes, break_minutes,
			long_break_minutes, rounds, long_break_every, auto_advance, chime, keep_history,
			position, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			(SELECT COALESCE(MAX(position), -1) + 1 FROM focus_timers WHERE user_id = ?), ?, ?)`,
		userID, in.Name, in.Color, in.Kind, in.FocusMinutes, nullable(in.BreakMinutes),
		nullable(in.LongBreakMinutes), nullable(in.Rounds), nullable(in.LongBreakEvery),
		flag(in.AutoAdvance), in.Chime, flag(in.KeepHistory), userID, now, now)
	if err != nil {
		return Timer{}, fmt.Errorf("focus: create timer: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Timer{}, fmt.Errorf("focus: create timer: %w", err)
	}
	return st.Timer(ctx, userID, id)
}

// Timer loads one of the user's timers.
func (st *Store) Timer(ctx context.Context, userID, id int64) (Timer, error) {
	return scanTimer(st.db.QueryRowContext(ctx,
		`SELECT `+timerColumns+` FROM focus_timers WHERE user_id = ? AND id = ?`, userID, id))
}

// Timers lists the user's timers in tile order.
func (st *Store) Timers(ctx context.Context, userID int64) ([]Timer, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+timerColumns+` FROM focus_timers WHERE user_id = ? ORDER BY position, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("focus: list timers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Timer
	for rows.Next() {
		t, err := scanTimer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("focus: list timers: %w", err)
	}
	return out, nil
}

// UpdateTimer replaces every setting of one of the user's timers; its
// place in the list doesn't change.
func (st *Store) UpdateTimer(ctx context.Context, userID, id int64, in TimerInput) (Timer, error) {
	in, err := checked(in)
	if err != nil {
		return Timer{}, err
	}
	res, err := st.db.ExecContext(ctx, `
		UPDATE focus_timers SET name = ?, color = ?, kind = ?, focus_minutes = ?,
			break_minutes = ?, long_break_minutes = ?, rounds = ?, long_break_every = ?,
			auto_advance = ?, chime = ?, keep_history = ?, updated_at = ?
		WHERE user_id = ? AND id = ?`,
		in.Name, in.Color, in.Kind, in.FocusMinutes, nullable(in.BreakMinutes),
		nullable(in.LongBreakMinutes), nullable(in.Rounds), nullable(in.LongBreakEvery),
		flag(in.AutoAdvance), in.Chime, flag(in.KeepHistory), db.FormatTime(st.now()), userID, id)
	if err != nil {
		return Timer{}, fmt.Errorf("focus: update timer: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return Timer{}, fmt.Errorf("focus: update timer: %w", err)
	} else if n == 0 {
		return Timer{}, ErrNotFound
	}
	return st.Timer(ctx, userID, id)
}

// DeleteTimer removes one of the user's timers. Its recorded sessions stay
// (timer_id becomes NULL; they keep their own copy of the name and colour).
func (st *Store) DeleteTimer(ctx context.Context, userID, id int64) error {
	res, err := st.db.ExecContext(ctx, `DELETE FROM focus_timers WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return fmt.Errorf("focus: delete timer: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("focus: delete timer: %w", err)
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

const copySuffix = " (copy)"

// copyName is name with " (copy)" on the end, shortened first if needed so
// the result still fits MaxNameRunes.
func copyName(name string) string {
	room := MaxNameRunes - utf8.RuneCountInString(copySuffix)
	if r := []rune(name); len(r) > room {
		name = string(r[:room])
	}
	return name + copySuffix
}

// DuplicateTimer copies one of the user's timers, placing the copy right
// after the original.
func (st *Store) DuplicateTimer(ctx context.Context, userID, id int64) (Timer, error) {
	orig, err := st.Timer(ctx, userID, id)
	if err != nil {
		return Timer{}, err
	}
	in, err := checked(TimerInput{
		Name: copyName(orig.Name), Color: orig.Color, Kind: orig.Kind,
		FocusMinutes: orig.FocusMinutes, BreakMinutes: orig.BreakMinutes,
		LongBreakMinutes: orig.LongBreakMinutes, Rounds: orig.Rounds,
		LongBreakEvery: orig.LongBreakEvery, AutoAdvance: orig.AutoAdvance,
		KeepHistory: orig.KeepHistory, Chime: orig.Chime,
	})
	if err != nil {
		return Timer{}, err
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return Timer{}, fmt.Errorf("focus: duplicate timer: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE focus_timers SET position = position + 1 WHERE user_id = ? AND position > ?`,
		userID, orig.Position); err != nil {
		return Timer{}, fmt.Errorf("focus: duplicate timer: %w", err)
	}
	now := db.FormatTime(st.now())
	res, err := tx.ExecContext(ctx, `
		INSERT INTO focus_timers (user_id, name, color, kind, focus_minutes, break_minutes,
			long_break_minutes, rounds, long_break_every, auto_advance, chime, keep_history,
			position, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, in.Name, in.Color, in.Kind, in.FocusMinutes, nullable(in.BreakMinutes),
		nullable(in.LongBreakMinutes), nullable(in.Rounds), nullable(in.LongBreakEvery),
		flag(in.AutoAdvance), in.Chime, flag(in.KeepHistory), orig.Position+1, now, now)
	if err != nil {
		return Timer{}, fmt.Errorf("focus: duplicate timer: %w", err)
	}
	newID, err := res.LastInsertId()
	if err != nil {
		return Timer{}, fmt.Errorf("focus: duplicate timer: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Timer{}, fmt.Errorf("focus: duplicate timer: %w", err)
	}
	return st.Timer(ctx, userID, newID)
}

// timerOrder is the user's timer ids in tile order, read inside tx.
func timerOrder(ctx context.Context, tx *sql.Tx, userID int64) ([]int64, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM focus_timers WHERE user_id = ? ORDER BY position, id`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// setOrder stores ids, in order, as the user's tile positions.
func setOrder(ctx context.Context, tx *sql.Tx, userID int64, ids []int64) error {
	for pos, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE focus_timers SET position = ? WHERE user_id = ? AND id = ?`, pos, userID, id); err != nil {
			return err
		}
	}
	return nil
}

// ReorderTimers sets the tile order. ids must name every one of the user's
// timers exactly once — anything else (a stale page, someone else's id) is
// refused rather than half-applied.
func (st *Store) ReorderTimers(ctx context.Context, userID int64, ids []int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("focus: reorder timers: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	have, err := timerOrder(ctx, tx, userID)
	if err != nil {
		return fmt.Errorf("focus: reorder timers: %w", err)
	}
	want := slices.Clone(ids)
	slices.Sort(have)
	slices.Sort(want)
	if len(want) == 0 || !slices.Equal(have, want) {
		return fmt.Errorf("%w: order must list each timer once", ErrInvalid)
	}
	if err := setOrder(ctx, tx, userID, ids); err != nil {
		return fmt.Errorf("focus: reorder timers: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("focus: reorder timers: %w", err)
	}
	return nil
}

// MoveTimer swaps one of the user's timers with its neighbour: by is -1
// for earlier, +1 for later (#539). A timer already at that end stays put
// — a stale page can ask for that, and it isn't worth an error.
func (st *Store) MoveTimer(ctx context.Context, userID, id int64, by int) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("focus: move timer: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	ids, err := timerOrder(ctx, tx, userID)
	if err != nil {
		return fmt.Errorf("focus: move timer: %w", err)
	}
	i := slices.Index(ids, id)
	if i < 0 {
		return ErrNotFound
	}
	j := i + by
	if j < 0 || j >= len(ids) {
		return nil
	}
	ids[i], ids[j] = ids[j], ids[i]
	if err := setOrder(ctx, tx, userID, ids); err != nil {
		return fmt.Errorf("focus: move timer: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("focus: move timer: %w", err)
	}
	return nil
}
