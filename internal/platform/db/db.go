// Package db opens and maintains the single SQLite database backing the
// whole suite. It contains no application schema; migrations own that.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // registers the "sqlite" driver; pure Go, no CGO
)

// pragmas are applied per connection by the driver, via the DSN.
//
//	journal_mode(WAL)  readers never block the writer
//	busy_timeout(5000) wait up to 5s for a lock rather than failing at once
//	foreign_keys(1)    SQLite leaves FK enforcement OFF by default
const pragmas = "_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"

// Open opens the database at path, creating the file and its parent
// directory if they do not exist.
//
// MaxOpenConns(1) serialises every statement in the process. At ON Suite's
// scale the throughput cost is unmeasurable, and it eliminates
// "database is locked" as a class of failure. See spec §6.1: if writes ever
// become a bottleneck the fix is a second, read-only pool, which is a change
// contained entirely within this package.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	handle, err := sql.Open("sqlite", "file:"+path+"?"+pragmas)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	handle.SetMaxOpenConns(1)
	handle.SetMaxIdleConns(1)
	handle.SetConnMaxLifetime(0) // a single long-lived connection is what we want

	// sql.Open is lazy, so nothing above has touched the file yet. Ping forces
	// it, turning a bad path into an error here instead of at first query.
	if err := handle.Ping(); err != nil {
		_ = handle.Close()
		return nil, fmt.Errorf("ping %s: %w", path, err)
	}
	return handle, nil
}

// Checkpoint folds the write-ahead log back into the main database file and
// truncates it. Call it on shutdown, after writes have stopped, so the data
// directory is left in a tidy state for backup or copy.
func Checkpoint(ctx context.Context, handle *sql.DB) error {
	var busy, logFrames, checkpointed int
	err := handle.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").
		Scan(&busy, &logFrames, &checkpointed)
	if err != nil {
		return fmt.Errorf("wal checkpoint: %w", err)
	}
	if busy != 0 {
		return errors.New("wal checkpoint blocked by an active reader")
	}
	return nil
}

// BackupTo writes a consistent snapshot to dest while the database remains
// open and writable.
//
// Copying the file with cp, or with the sqlite3 CLI, is not safe against a
// live writer. VACUUM INTO is: SQLite builds the copy inside a read
// transaction. dest must not already exist.
func BackupTo(ctx context.Context, handle *sql.DB, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("backup destination %s already exists", dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", dest, err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	if _, err := handle.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		return fmt.Errorf("vacuum into %s: %w", dest, err)
	}
	return nil
}
