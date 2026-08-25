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

// Children returns a parent's direct children in position order. parentID may
// be RootID for the top level.
func (st *Store) Children(ctx context.Context, userID, parentID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+nodeColumns+`
		   FROM notes_nodes WHERE user_id = ? AND parent_id IS ?
		  ORDER BY position`, userID, parentArg(parentID))
	if err != nil {
		return nil, fmt.Errorf("notes: children of %d: %w", parentID, err)
	}
	return collectNodes(rows, "children")
}

// Ancestors returns the path from the top level down to id's parent — the
// breadcrumb above a zoomed outline — outermost first. It is empty for a
// top-level node, and empty for a node that does not exist or is not userID's:
// a caller that needs to tell those apart calls ByID.
func (st *Store) Ancestors(ctx context.Context, userID, id int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE up AS (
		     SELECT `+nodeColumns+`, 0 AS d
		       FROM notes_nodes WHERE id = ? AND user_id = ?
		   UNION ALL
		     SELECT p.id, p.user_id, p.parent_id, p.position, p.title, p.note,
		            p.collapsed, p.created_at, p.updated_at, u.d + 1
		       FROM notes_nodes p JOIN up u ON p.id = u.parent_id
		      WHERE u.d < ?
		 )
		 SELECT `+nodeColumns+` FROM up WHERE d > 0 ORDER BY d DESC`,
		id, userID, MaxDepth)
	if err != nil {
		return nil, fmt.Errorf("notes: ancestors of %d: %w", id, err)
	}
	return collectNodes(rows, "ancestors")
}

// collectNodes drains rows into Nodes and closes them.
func collectNodes(rows *sql.Rows, what string) ([]Node, error) {
	defer func() { _ = rows.Close() }()

	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notes: %s: %w", what, err)
	}
	return out, nil
}
