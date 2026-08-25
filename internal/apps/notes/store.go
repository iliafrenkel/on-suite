package notes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Store is all the SQL for ON Notes. It has no HTTP knowledge, so it can be
// tested against a real database on its own.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(handle *sql.DB) *Store {
	return &Store{db: handle, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the time source, for tests.
func (st *Store) SetClock(now func() time.Time) { st.now = now }

// nodeColumns is every column a Node is scanned from, in scan order. It lives
// in one place because half a dozen queries select exactly this list, and a
// column added to one of them and not the others is a silent scan error.
const nodeColumns = `id, user_id, parent_id, position, title, note, collapsed, created_at, updated_at`

// ByID fetches one of userID's own nodes.
func (st *Store) ByID(ctx context.Context, userID, id int64) (Node, error) {
	n, err := scanNode(st.db.QueryRowContext(ctx,
		`SELECT `+nodeColumns+` FROM notes_nodes WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	return n, err
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface{ Scan(dest ...any) error }

// scanNode reads the nodeColumns list. extra takes destinations for any
// columns a query appends after that list — Outline's depth and has_children
// — so there is only ever one place that knows the column order.
func scanNode(row rowScanner, extra ...any) (Node, error) {
	var (
		n         Node
		parent    sql.NullInt64
		createdAt string
		updatedAt string
	)
	dest := append([]any{
		&n.ID, &n.UserID, &parent, &n.Position, &n.Title, &n.Note,
		&n.Collapsed, &createdAt, &updatedAt,
	}, extra...)

	err := row.Scan(dest...)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Node{}, sql.ErrNoRows // translated to ErrNotFound by the caller
	case err != nil:
		return Node{}, fmt.Errorf("notes: scan: %w", err)
	}
	// Valid is false for a top-level node, and Int64 is then 0, which is
	// exactly RootID.
	n.ParentID = parent.Int64

	if n.CreatedAt, err = parseTime(createdAt); err != nil {
		return Node{}, err
	}
	if n.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Node{}, err
	}
	return n, nil
}

// Timestamps match the platform's convention: RFC 3339 nanoseconds in UTC,
// which sorts chronologically as text.
func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("notes: parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}
