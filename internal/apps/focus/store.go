// Package focus implements ON Focus: saved, reusable focus timers (single
// blocks or Pomodoro-style intervals) and, from F3, a history of sessions.
package focus

import (
	"database/sql"
	"embed"
	"errors"
	"io/fs"
	"time"
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
