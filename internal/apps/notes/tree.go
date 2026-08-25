package notes

import (
	"context"
	"database/sql"
	"errors"
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

// SetCollapsed records whether a bullet's children are hidden. It is stored
// rather than kept in the browser because Outline stops descending at a
// collapsed node: the flag decides what the server sends, not just what the
// page shows.
func (st *Store) SetCollapsed(ctx context.Context, userID, id int64, collapsed bool) error {
	return st.update(ctx,
		`UPDATE notes_nodes SET collapsed = ?, updated_at = ?
		  WHERE id = ? AND user_id = ?`,
		collapsed, formatTime(st.now()), id, userID)
}

// SetText replaces a bullet's title and note.
//
// Trailing spaces are stripped but leading ones are not: an outline is written
// in prose, and a leading space is sometimes deliberate, while a trailing one
// never is.
func (st *Store) SetText(ctx context.Context, userID, id int64, title, note string) error {
	title = strings.TrimRight(title, " \t")
	if err := Validate(title, note); err != nil {
		return err
	}
	return st.update(ctx,
		`UPDATE notes_nodes SET title = ?, note = ?, updated_at = ?
		  WHERE id = ? AND user_id = ?`,
		title, note, formatTime(st.now()), id, userID)
}

// update runs a single-row UPDATE and turns "nothing matched" into
// ErrNotFound, which covers both "no such node" and "not yours".
func (st *Store) update(ctx context.Context, query string, args ...any) error {
	res, err := st.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("notes: update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("notes: update: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a bullet and everything under it, then closes the gap it
// left among its siblings.
func (st *Store) Delete(ctx context.Context, userID, id int64) error {
	return st.tx(ctx, func(tx *sql.Tx) error {
		var (
			parent sql.NullInt64
			pos    int
		)
		err := tx.QueryRowContext(ctx,
			`SELECT parent_id, position FROM notes_nodes WHERE id = ? AND user_id = ?`,
			id, userID).Scan(&parent, &pos)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("notes: delete: %w", err)
		}

		// The subtree goes with it: notes_nodes.parent_id is ON DELETE
		// CASCADE, and the platform opens SQLite with foreign_keys=ON.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM notes_nodes WHERE id = ? AND user_id = ?`, id, userID); err != nil {
			return fmt.Errorf("notes: delete: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE notes_nodes SET position = position - 1
			  WHERE user_id = ? AND parent_id IS ? AND position > ?`,
			userID, parentArg(parent.Int64), pos); err != nil {
			return fmt.Errorf("notes: delete: close the gap: %w", err)
		}
		return nil
	})
}

// Move reparents a bullet, taking its subtree with it, and places it at newPos
// among its new siblings. newPos is clamped into range. newParentID may be
// RootID.
//
// This is the one operation that can corrupt the tree. Moving a bullet inside
// its own subtree detaches a cycle that no outline can reach: the rows are
// still in the table but gone from the app, and nothing in the UI can undo it.
// Both guards below therefore run inside the same transaction as the move,
// not before it.
func (st *Store) Move(ctx context.Context, userID, id, newParentID int64, newPos int) error {
	if id == newParentID {
		return ErrCycle
	}
	return st.tx(ctx, func(tx *sql.Tx) error {
		var (
			oldParent sql.NullInt64
			oldPos    int
		)
		err := tx.QueryRowContext(ctx,
			`SELECT parent_id, position FROM notes_nodes WHERE id = ? AND user_id = ?`,
			id, userID).Scan(&oldParent, &oldPos)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("notes: move: %w", err)
		}

		if newParentID != RootID {
			inside, err := isDescendant(ctx, tx, userID, newParentID, id)
			if err != nil {
				return err
			}
			if inside {
				return ErrCycle
			}
			// depthOf doubles as the ownership check on the new parent.
			parentDepth, err := depthOf(ctx, tx, userID, newParentID)
			if err != nil {
				return err
			}
			height, err := heightOf(ctx, tx, userID, id)
			if err != nil {
				return err
			}
			if parentDepth+1+height > MaxDepth {
				return ErrTooDeep
			}
		}

		// Close the gap the bullet leaves behind. It is excluded from both
		// shifts by id, because a move within one parent would otherwise drag
		// the bullet along with its own siblings.
		if _, err := tx.ExecContext(ctx,
			`UPDATE notes_nodes SET position = position - 1
			  WHERE user_id = ? AND parent_id IS ? AND position > ? AND id != ?`,
			userID, parentArg(oldParent.Int64), oldPos, id); err != nil {
			return fmt.Errorf("notes: move: close the gap: %w", err)
		}

		n, err := countChildren(ctx, tx, userID, newParentID)
		if err != nil {
			return err
		}
		if newParentID == oldParent.Int64 {
			n-- // the bullet is still counted among its own new siblings
		}
		idx := clamp(newPos, 0, n)

		if _, err := tx.ExecContext(ctx,
			`UPDATE notes_nodes SET position = position + 1
			  WHERE user_id = ? AND parent_id IS ? AND position >= ? AND id != ?`,
			userID, parentArg(newParentID), idx, id); err != nil {
			return fmt.Errorf("notes: move: open a gap: %w", err)
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE notes_nodes SET parent_id = ?, position = ?, updated_at = ?
			  WHERE id = ? AND user_id = ?`,
			parentArg(newParentID), idx, formatTime(st.now()), id, userID); err != nil {
			return fmt.Errorf("notes: move: %w", err)
		}
		return nil
	})
}

// heightOf reports how many levels of descendants a node has: 0 for a leaf.
// It returns ErrNotFound on the same terms as depthOf.
func heightOf(ctx context.Context, tx *sql.Tx, userID, id int64) (int, error) {
	var height sql.NullInt64
	err := tx.QueryRowContext(ctx,
		`WITH RECURSIVE down(id, d) AS (
		     SELECT id, 0 FROM notes_nodes WHERE id = ? AND user_id = ?
		   UNION ALL
		     SELECT c.id, n.d + 1
		       FROM notes_nodes c JOIN down n ON c.parent_id = n.id
		      WHERE n.d < ?
		 )
		 SELECT max(d) FROM down`, id, userID, MaxDepth).Scan(&height)
	if err != nil {
		return 0, fmt.Errorf("notes: height of %d: %w", id, err)
	}
	if !height.Valid {
		return 0, ErrNotFound
	}
	return int(height.Int64), nil
}

// isDescendant reports whether candidate sits anywhere inside root's subtree,
// by walking up from candidate and looking for root. Walking up is bounded by
// MaxDepth; walking down would be bounded only by the size of the subtree.
func isDescendant(ctx context.Context, tx *sql.Tx, userID, candidate, root int64) (bool, error) {
	var found int
	err := tx.QueryRowContext(ctx,
		`WITH RECURSIVE up(id, parent_id, d) AS (
		     SELECT id, parent_id, 0 FROM notes_nodes WHERE id = ? AND user_id = ?
		   UNION ALL
		     SELECT p.id, p.parent_id, u.d + 1
		       FROM notes_nodes p JOIN up u ON p.id = u.parent_id
		      WHERE u.d < ?
		 )
		 SELECT count(*) FROM up WHERE id = ?`,
		candidate, userID, MaxDepth, root).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("notes: cycle check: %w", err)
	}
	return found > 0, nil
}
