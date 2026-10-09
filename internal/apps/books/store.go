// Package books implements ON Books, a private reading log: what you are
// reading, what you have read, what you want to read and what you gave up
// on. Spec: docs/superpowers/specs/2026-10-09-on-books-design.md.
package books

import (
	"database/sql"
	"embed"
	"errors"
	"io/fs"
	"time"
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
