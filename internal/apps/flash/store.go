// Package flash implements ON Flash, a flash-card app for learning anything
// by generating cards elsewhere (an LLM, mostly) and reviewing them here.
//
// It depends only on internal/platform/*. It never imports another app, and
// no platform package imports it: the whole coupling is the app.App
// interface plus one line in cmd/onsuite/main.go.
package flash

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// ID is the app id: the URL prefix, the migration namespace, and the prefix
// on every table this app owns.
const ID = "flash"

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations returns this app's schema with filenames at the root, which is
// what db.Collect expects.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		// Unreachable: the path is a compile-time constant checked by go:embed.
		panic("flash: embedded migrations missing: " + err.Error())
	}
	return sub
}

var (
	// ErrNotFound covers both "no such row" and "not yours". They are
	// deliberately indistinguishable: returning a 403 for someone else's
	// deck or card would confirm that it exists.
	ErrNotFound = errors.New("flash: not found")
	ErrInvalid  = errors.New("flash: invalid input")
)

// Store is all the SQL for ON Flash. It has no HTTP knowledge, so it can be
// tested against a real database on its own.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// NewStore wraps an open database handle.
func NewStore(handle *sql.DB) *Store {
	return &Store{db: handle, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the time source, for tests.
func (st *Store) SetClock(now func() time.Time) { st.now = now }

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// isUniqueViolation avoids importing the driver package just to read an error
// code. Matching on the message is unattractive but keeps this package free
// of a driver dependency, and the substring is stable in SQLite. Mirrors
// internal/platform/auth/store.go's own isUniqueViolation; that package
// cannot be imported here for this alone (apps never import each other, and
// this is platform-internal), so it is an independent implementation with
// the same justification.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// Timestamps match the platform's convention, db.TimeLayout: UTC with
// exactly nine fractional digits, which sorts chronologically as text even
// within one second (#356) — the property ORDER BY created_at, due_at <= ?
// and min(due_at) all rely on.
func formatTime(t time.Time) string { return db.FormatTime(t) }

func parseTime(s string) (time.Time, error) {
	t, err := db.ParseTime(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("flash: parse timestamp %q: %w", s, err)
	}
	return t, nil
}
