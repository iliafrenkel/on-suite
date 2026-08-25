package notes

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// tx runs fn inside a transaction, rolling back unless fn returns nil.
//
// Every mutation goes through it. A tree operation that half-applies leaves an
// outline that no later operation can repair: positions with a gap in them
// make every subsequent clamp land one place off, silently.
func (st *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	handle, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("notes: begin transaction: %w", err)
	}
	defer func() { _ = handle.Rollback() }() // a no-op after a successful Commit

	if err := fn(handle); err != nil {
		return err
	}
	if err := handle.Commit(); err != nil {
		return fmt.Errorf("notes: commit: %w", err)
	}
	return nil
}

// parentArg converts the RootID sentinel to the NULL the column actually
// stores. It is always paired with "parent_id IS ?", which SQLite treats as =
// for a non-NULL value and as a NULL test otherwise — so one query shape
// covers both top-level and nested nodes, with no branching anywhere.
func parentArg(parentID int64) any {
	if parentID == RootID {
		return nil
	}
	return parentID
}

// Create inserts a new bullet as a child of parentID, which may be RootID.
//
// afterPos is the position of the sibling to insert after: -1 inserts first,
// and anything at or past the last sibling appends. Out-of-range values are
// clamped rather than rejected, because "after the bullet I am looking at" is
// still the caller's intent when the tree has moved underneath them.
func (st *Store) Create(ctx context.Context, userID, parentID int64, afterPos int, title, note string) (Node, error) {
	title = strings.TrimRight(title, " \t")
	if err := Validate(title, note); err != nil {
		return Node{}, err
	}

	out := Node{UserID: userID, ParentID: parentID, Title: title, Note: note}
	err := st.tx(ctx, func(tx *sql.Tx) error {
		if parentID != RootID {
			// depthOf also answers "does this parent exist and is it yours",
			// so there is no separate ownership query.
			depth, err := depthOf(ctx, tx, userID, parentID)
			if err != nil {
				return err
			}
			if depth+1 > MaxDepth {
				return ErrTooDeep
			}
		}

		n, err := countChildren(ctx, tx, userID, parentID)
		if err != nil {
			return err
		}
		idx := clamp(afterPos+1, 0, n)

		if _, err := tx.ExecContext(ctx,
			`UPDATE notes_nodes SET position = position + 1
			  WHERE user_id = ? AND parent_id IS ? AND position >= ?`,
			userID, parentArg(parentID), idx); err != nil {
			return fmt.Errorf("notes: create: shift siblings: %w", err)
		}

		now := st.now()
		out.Position, out.CreatedAt, out.UpdatedAt = idx, now, now
		err = tx.QueryRowContext(ctx,
			`INSERT INTO notes_nodes
			     (user_id, parent_id, position, title, note, collapsed, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, 0, ?, ?)
			 RETURNING id`,
			userID, parentArg(parentID), idx, title, note,
			formatTime(now), formatTime(now)).Scan(&out.ID)
		if err != nil {
			return fmt.Errorf("notes: create: %w", err)
		}
		return nil
	})
	if err != nil {
		return Node{}, err
	}
	return out, nil
}

// depthOf reports how deep a node sits, counting a top-level node as 0. It
// returns ErrNotFound for a node that does not exist or is not userID's.
//
// The walk is capped at MaxDepth so that a tree which has somehow acquired a
// cycle returns an answer instead of running forever.
func depthOf(ctx context.Context, tx *sql.Tx, userID, id int64) (int, error) {
	var depth sql.NullInt64
	err := tx.QueryRowContext(ctx,
		`WITH RECURSIVE up(id, parent_id, d) AS (
		     SELECT id, parent_id, 0 FROM notes_nodes WHERE id = ? AND user_id = ?
		   UNION ALL
		     SELECT p.id, p.parent_id, u.d + 1
		       FROM notes_nodes p JOIN up u ON p.id = u.parent_id
		      WHERE u.d < ?
		 )
		 SELECT max(d) FROM up`, id, userID, MaxDepth).Scan(&depth)
	if err != nil {
		return 0, fmt.Errorf("notes: depth of %d: %w", id, err)
	}
	// max() over an empty CTE is one row containing NULL, not zero rows.
	if !depth.Valid {
		return 0, ErrNotFound
	}
	return int(depth.Int64), nil
}

// countChildren counts a parent's direct children.
func countChildren(ctx context.Context, tx *sql.Tx, userID, parentID int64) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM notes_nodes WHERE user_id = ? AND parent_id IS ?`,
		userID, parentArg(parentID)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("notes: count children of %d: %w", parentID, err)
	}
	return n, nil
}

// clamp bounds v to [lo, hi].
func clamp(v, lo, hi int) int { return min(max(v, lo), hi) }

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
