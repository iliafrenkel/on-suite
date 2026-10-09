// Package books implements ON Books, a private reading log: what you are
// reading, what you have read, what you want to read and what you gave up
// on. Spec: docs/superpowers/specs/2026-10-09-on-books-design.md.
package books

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// ID is the app id: URL prefix, migration namespace, table prefix.
const ID = "books"

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations is this app's schema, for the platform and for store tests.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		panic("books: embedded migrations missing: " + err.Error()) // unreachable
	}
	return sub
}

var (
	// ErrNotFound is a missing row or somebody else's — indistinguishable
	// on purpose, so a handler answers 404 for both.
	ErrNotFound = errors.New("books: not found")
	// ErrInvalid is a request the store refuses.
	ErrInvalid = errors.New("books: invalid")
)

// dayLayout is a calendar date column's format (AGENTS.md: "Date-only
// columns use 2006-01-02").
const dayLayout = "2006-01-02"

// Store is every SQL query ON Books runs.
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

// Today is the server's local date, YYYY-MM-DD: days follow time.Local,
// set by TZ, everywhere in the suite (#424).
func (st *Store) Today() string { return st.now().Local().Format(dayLayout) }

func formatTime(t time.Time) string { return db.FormatTime(t) }

func parseTime(s string) (time.Time, error) { return db.ParseTime(s) }

// Refusal is an action the store won't take for a reason the person can
// fix — a finish date before the start, a second reading at once. Msg is
// shown to them as is.
type Refusal struct{ Msg string }

func (e *Refusal) Error() string { return "books: " + e.Msg }

// Unwrap makes a Refusal an ErrInvalid for errors.Is.
func (e *Refusal) Unwrap() error { return ErrInvalid }

// checkDay accepts a YYYY-MM-DD date from MinYear on that is not after
// today. The floor catches a year typed half way, which browsers save as
// 0026-03-01.
func (st *Store) checkDay(day string) error {
	if _, err := time.Parse(dayLayout, day); err != nil {
		return &Refusal{Msg: "Enter a date like " + st.Today() + "."}
	}
	if day > st.Today() {
		return &Refusal{Msg: "That date is in the future."}
	}
	if day < strconv.Itoa(MinYear) {
		return &Refusal{Msg: fmt.Sprintf("Enter a date from %d on.", MinYear)}
	}
	return nil
}

// touch bumps a book's updated_at inside tx and is the owner check: it is
// ErrNotFound for a missing or someone else's book. It runs inside the
// transaction, as ON Later's SetTags does (#294): with one connection, a
// check before it could race a delete.
func (st *Store) touch(ctx context.Context, tx *sql.Tx, userID, id int64) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE books_books SET updated_at = ? WHERE id = ? AND user_id = ?`,
		formatTime(st.now()), id, userID)
	if err != nil {
		return fmt.Errorf("books: touch: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
