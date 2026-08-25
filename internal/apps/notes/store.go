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
//
// A write that decides what to do from what it read calls Ops.ByID instead, so
// that the read and the write are the same transaction.
func (st *Store) ByID(ctx context.Context, userID, id int64) (Node, error) {
	return nodeByID(ctx, st.db, userID, id)
}

// querier is the read surface shared by *sql.DB and *sql.Tx, so that the two
// single-node reads below have one implementation each rather than one per
// caller. Nothing else needs it: Ops is the only reason a read runs against a
// transaction.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// nodeByID is the SQL behind Store.ByID and Ops.ByID.
func nodeByID(ctx context.Context, q querier, userID, id int64) (Node, error) {
	n, err := scanNode(q.QueryRowContext(ctx,
		`SELECT `+nodeColumns+` FROM notes_nodes WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	return n, err
}

// siblingAt fetches the child of parentID sitting at a given position. It is
// reached through Ops.siblingAt, which supplies the transaction.
func siblingAt(ctx context.Context, q querier, userID, parentID int64, pos int) (Node, error) {
	n, err := scanNode(q.QueryRowContext(ctx,
		`SELECT `+nodeColumns+`
		   FROM notes_nodes WHERE user_id = ? AND parent_id IS ? AND position = ?`,
		userID, parentArg(parentID), pos))
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

// Outline returns everything visible under rootID, in document order, with
// Depth relative to rootID and HasChildren set. rootID may be RootID.
//
// The result is flat rather than nested: the caller renders indentation from
// Depth, which keeps rendering non-recursive and makes this method's output
// trivial to assert in a test.
//
// The walk stops at any collapsed node, so the result is always exactly what
// is on screen — that is why collapsed is stored rather than kept in the
// browser. It is additionally capped at MaxDepth, so a tree that has somehow
// acquired a cycle returns a truncated outline instead of hanging.
//
// The ordering trick is the path column: each row carries its ancestors'
// positions as fixed-width text, so plain lexicographic ORDER BY produces
// pre-order — a parent immediately before its subtree, siblings by position.
func (st *Store) Outline(ctx context.Context, userID, rootID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE tree AS (
		     SELECT `+nodeColumns+`, 0 AS depth, printf('%08d', position) AS path
		       FROM notes_nodes
		      WHERE user_id = ? AND parent_id IS ?
		   UNION ALL
		     SELECT c.id, c.user_id, c.parent_id, c.position, c.title, c.note,
		            c.collapsed, c.created_at, c.updated_at,
		            t.depth + 1, t.path || '/' || printf('%08d', c.position)
		       FROM notes_nodes c JOIN tree t ON c.parent_id = t.id
		      WHERE t.collapsed = 0 AND t.depth + 1 <= ?
		 )
		 SELECT `+nodeColumns+`, depth,
		        EXISTS (SELECT 1 FROM notes_nodes k WHERE k.parent_id = tree.id)
		   FROM tree ORDER BY path`,
		userID, parentArg(rootID), MaxDepth)
	if err != nil {
		return nil, fmt.Errorf("notes: outline of %d: %w", rootID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Node
	for rows.Next() {
		// The EXISTS subquery needs no user_id filter: a child always has the
		// same owner as its parent (invariant I2).
		var (
			depth       int
			hasChildren bool
		)
		n, err := scanNode(rows, &depth, &hasChildren)
		if err != nil {
			return nil, err
		}
		n.Depth, n.HasChildren = depth, hasChildren
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notes: outline: %w", err)
	}
	return out, nil
}
