package notes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Ops is the store's write API bound to a single transaction. It exists so a
// caller can save a bullet's text and restructure the tree as one write —
// spec §7 — rather than as two that another tab can interleave with.
//
// Every method on Store that changes anything is a thin wrapper around the
// method of the same name here, so the two are never allowed to drift.
//
// It deliberately holds no *Store. The clock is copied in rather than reached
// through one, so that no code inside the write API can touch st.db while the
// transaction is open — the deadlock described on Do is then a compile error
// here rather than a rule to remember.
type Ops struct {
	tx *sql.Tx
	// now is the store's clock, copied at Do time so that tests can replace
	// it. See Store.SetClock.
	now func() time.Time
}

// Do runs fn inside one transaction, committing if it returns nil and rolling
// back otherwise.
//
// Every mutation goes through it. A tree operation that half-applies leaves an
// outline that no later operation can repair: positions with a gap in them
// make every subsequent clamp land one place off, silently.
//
// fn must reach the database only through the Ops it is handed. The platform
// opens SQLite with SetMaxOpenConns(1), so a closure that calls a Store method
// instead waits for a connection the transaction is holding, and waits for
// ever. Ops itself cannot make that mistake — it holds no *Store — but a
// closure that captures one still can, which is why a render belongs after Do
// has returned rather than inside fn.
func (st *Store) Do(ctx context.Context, fn func(*Ops) error) error {
	handle, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("notes: begin transaction: %w", err)
	}
	defer func() { _ = handle.Rollback() }() // a no-op after a successful Commit

	if err := fn(&Ops{tx: handle, now: st.now}); err != nil {
		return err
	}
	if err := handle.Commit(); err != nil {
		return fmt.Errorf("notes: commit: %w", err)
	}
	return nil
}

// ByID fetches one of userID's own nodes, through the transaction, so that a
// write decided from what it read cannot be reading a stale tree.
func (o *Ops) ByID(ctx context.Context, userID, id int64) (Node, error) {
	return nodeByID(ctx, o.tx, userID, id)
}

// siblingAt fetches the child of parentID sitting at a given position.
func (o *Ops) siblingAt(ctx context.Context, userID, parentID int64, pos int) (Node, error) {
	return siblingAt(ctx, o.tx, userID, parentID, pos)
}

// trimTitle applies notes' one title-normalization rule: trailing spaces and
// tabs are stripped, leading ones are not — an outline is written in prose,
// and a leading space is sometimes deliberate, while a trailing one never is.
//
// Every write path that saves a title calls this rather than trimming
// inline, including handlers.go's setText, which renders the Markdown for
// its HTMX response before the write reaches the store — issue #72: two
// independent copies of this rule could silently drift if it ever changed.
func trimTitle(title string) string {
	return strings.TrimRight(title, " \t")
}

// Create inserts a new bullet as a child of parentID, which may be RootID.
//
// afterPos is the position of the sibling to insert after: -1 inserts first,
// and anything at or past the last sibling appends. Out-of-range values are
// clamped rather than rejected, because "after the bullet I am looking at" is
// still the caller's intent when the tree has moved underneath them.
//
// The title is trimmed on the right only, for the reason given on SetText.
func (o *Ops) Create(ctx context.Context, userID, parentID int64, afterPos int, title, note string) (Node, error) {
	title = trimTitle(title)
	if err := Validate(title, note); err != nil {
		return Node{}, err
	}

	out := Node{UserID: userID, ParentID: parentID, Title: title, Note: note}
	if parentID != RootID {
		// depthOf also answers "does this parent exist and is it yours", so
		// there is no separate ownership query.
		depth, err := depthOf(ctx, o.tx, userID, parentID)
		if err != nil {
			return Node{}, err
		}
		if depth+1 > MaxDepth {
			return Node{}, ErrTooDeep
		}
	}

	n, err := countChildren(ctx, o.tx, userID, parentID)
	if err != nil {
		return Node{}, err
	}
	idx := clamp(afterPos+1, 0, n)

	if _, err := o.tx.ExecContext(ctx,
		`UPDATE notes_nodes SET position = position + 1
		  WHERE user_id = ? AND parent_id IS ? AND position >= ?`,
		userID, parentArg(parentID), idx); err != nil {
		return Node{}, fmt.Errorf("notes: create: shift siblings: %w", err)
	}

	now := o.now()
	out.Position, out.CreatedAt, out.UpdatedAt = idx, now, now
	err = o.tx.QueryRowContext(ctx,
		`INSERT INTO notes_nodes
		     (user_id, parent_id, position, title, note, collapsed, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 0, ?, ?)
		 RETURNING id`,
		userID, parentArg(parentID), idx, title, note,
		formatTime(now), formatTime(now)).Scan(&out.ID)
	if err != nil {
		return Node{}, fmt.Errorf("notes: create: %w", err)
	}
	return out, nil
}

// Create inserts a new bullet in a transaction of its own. See Ops.Create.
func (st *Store) Create(ctx context.Context, userID, parentID int64, afterPos int, title, note string) (Node, error) {
	var out Node
	err := st.Do(ctx, func(o *Ops) error {
		var err error
		out, err = o.Create(ctx, userID, parentID, afterPos, title, note)
		return err
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
//
// Like heightOf, the recursive join below carries no user_id predicate, and
// that is harmless rather than deliberate: the anchor row is already scoped
// to userID, and every way this walk could be wrong under a broken I2 comes
// out fail-safe, since the only thing either function hands back is an int
// that at worst trips ErrTooDeep.
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
func (o *Ops) SetCollapsed(ctx context.Context, userID, id int64, collapsed bool) error {
	return o.update(ctx, "set collapsed",
		`UPDATE notes_nodes SET collapsed = ?, updated_at = ?
		  WHERE id = ? AND user_id = ?`,
		collapsed, formatTime(o.now()), id, userID)
}

// SetCollapsed records a bullet's collapse state in a transaction of its own.
// See Ops.SetCollapsed.
func (st *Store) SetCollapsed(ctx context.Context, userID, id int64, collapsed bool) error {
	return st.Do(ctx, func(o *Ops) error { return o.SetCollapsed(ctx, userID, id, collapsed) })
}

// SetDone marks a bullet done or not. Completing a parent does not complete
// its children — spec §11 — so this only ever touches the one row; hiding
// a done bullet's subtree is a display decision made in view.go, not
// something recorded here.
func (o *Ops) SetDone(ctx context.Context, userID, id int64, done bool) error {
	doneAt := sql.NullString{}
	if done {
		doneAt = sql.NullString{String: formatTime(o.now()), Valid: true}
	}
	return o.update(ctx, "set done",
		`UPDATE notes_nodes SET done_at = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		doneAt, formatTime(o.now()), id, userID)
}

// SetDone marks a bullet done or not, in a transaction of its own. See
// Ops.SetDone.
func (st *Store) SetDone(ctx context.Context, userID, id int64, done bool) error {
	return st.Do(ctx, func(o *Ops) error { return o.SetDone(ctx, userID, id, done) })
}

// SetDue sets or clears a bullet's due date. due is "" to clear, or a
// validated 'YYYY-MM-DD' string — see ValidateDue.
func (o *Ops) SetDue(ctx context.Context, userID, id int64, due string) error {
	if err := ValidateDue(due); err != nil {
		return err
	}
	var arg any
	if due != "" {
		arg = due
	}
	return o.update(ctx, "set due",
		`UPDATE notes_nodes SET due_on = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		arg, formatTime(o.now()), id, userID)
}

// SetDue sets or clears a bullet's due date, in a transaction of its own.
// See Ops.SetDue.
func (st *Store) SetDue(ctx context.Context, userID, id int64, due string) error {
	return st.Do(ctx, func(o *Ops) error { return o.SetDue(ctx, userID, id, due) })
}

// SetArchived marks a bullet archived, or restores it — spec §13. Like
// SetDone, this only ever touches the one row: an archived node's subtree
// disappearing from the outline, search and due list (Task 2) is a display
// decision made in the queries that build those views, never something
// recorded on every descendant.
func (o *Ops) SetArchived(ctx context.Context, userID, id int64, archived bool) error {
	archivedAt := sql.NullString{}
	if archived {
		archivedAt = sql.NullString{String: formatTime(o.now()), Valid: true}
	}
	return o.update(ctx, "set archived",
		`UPDATE notes_nodes SET archived_at = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		archivedAt, formatTime(o.now()), id, userID)
}

// SetArchived marks a bullet archived, or restores it, in a transaction of
// its own. See Ops.SetArchived.
func (st *Store) SetArchived(ctx context.Context, userID, id int64, archived bool) error {
	return st.Do(ctx, func(o *Ops) error { return o.SetArchived(ctx, userID, id, archived) })
}

// SetText replaces a bullet's title and note.
//
// Trailing spaces are stripped but leading ones are not: an outline is written
// in prose, and a leading space is sometimes deliberate, while a trailing one
// never is.
func (o *Ops) SetText(ctx context.Context, userID, id int64, title, note string) error {
	title = trimTitle(title)
	if err := Validate(title, note); err != nil {
		return err
	}
	return o.update(ctx, "set text",
		`UPDATE notes_nodes SET title = ?, note = ?, updated_at = ?
		  WHERE id = ? AND user_id = ?`,
		title, note, formatTime(o.now()), id, userID)
}

// SetText replaces a bullet's text in a transaction of its own. A handler
// saving text alongside a structural operation calls Do and uses Ops.SetText
// instead, so that the two land as one write. See Ops.SetText.
func (st *Store) SetText(ctx context.Context, userID, id int64, title, note string) error {
	return st.Do(ctx, func(o *Ops) error { return o.SetText(ctx, userID, id, title, note) })
}

// update runs a single-row UPDATE and turns "nothing matched" into
// ErrNotFound, which covers both "no such node" and "not yours".
//
// op names the calling operation, so that a database failure here reads like
// every other write in this package — "notes: set text: ..." rather than a
// "notes: update: ..." that two callers share and a log cannot tell apart.
func (o *Ops) update(ctx context.Context, op, query string, args ...any) error {
	res, err := o.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("notes: %s: %w", op, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("notes: %s: %w", op, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a bullet and everything under it, then closes the gap it
// left among its siblings.
func (o *Ops) Delete(ctx context.Context, userID, id int64) error {
	var (
		parent sql.NullInt64
		pos    int
	)
	err := o.tx.QueryRowContext(ctx,
		`SELECT parent_id, position FROM notes_nodes WHERE id = ? AND user_id = ?`,
		id, userID).Scan(&parent, &pos)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("notes: delete: %w", err)
	}

	// The subtree goes with it: notes_nodes.parent_id is ON DELETE CASCADE,
	// and the platform opens SQLite with foreign_keys=ON.
	if _, err := o.tx.ExecContext(ctx,
		`DELETE FROM notes_nodes WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		return fmt.Errorf("notes: delete: %w", err)
	}
	if _, err := o.tx.ExecContext(ctx,
		`UPDATE notes_nodes SET position = position - 1
		  WHERE user_id = ? AND parent_id IS ? AND position > ?`,
		userID, parentArg(parent.Int64), pos); err != nil {
		return fmt.Errorf("notes: delete: close the gap: %w", err)
	}
	return nil
}

// Delete removes a bullet and its subtree in a transaction of its own. See
// Ops.Delete.
func (st *Store) Delete(ctx context.Context, userID, id int64) error {
	return st.Do(ctx, func(o *Ops) error { return o.Delete(ctx, userID, id) })
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
func (o *Ops) Move(ctx context.Context, userID, id, newParentID int64, newPos int) error {
	if id == newParentID {
		return ErrCycle
	}

	var (
		oldParent sql.NullInt64
		oldPos    int
	)
	err := o.tx.QueryRowContext(ctx,
		`SELECT parent_id, position FROM notes_nodes WHERE id = ? AND user_id = ?`,
		id, userID).Scan(&oldParent, &oldPos)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("notes: move: %w", err)
	}

	if newParentID != RootID {
		inside, err := isDescendant(ctx, o.tx, userID, newParentID, id)
		if err != nil {
			return err
		}
		if inside {
			return ErrCycle
		}
		// depthOf doubles as the ownership check on the new parent.
		parentDepth, err := depthOf(ctx, o.tx, userID, newParentID)
		if err != nil {
			return err
		}
		height, err := heightOf(ctx, o.tx, userID, id)
		if err != nil {
			return err
		}
		if parentDepth+1+height > MaxDepth {
			return ErrTooDeep
		}
	}

	// Close the gap the bullet leaves behind.
	//
	// The "id != ?" on this shift and the next is belt and braces rather
	// than load-bearing. This predicate cannot match the moving row in
	// any case, since its position is exactly oldPos; and any increment
	// the next shift applied to it would be overwritten by the final
	// update below. They stay because a future change to either
	// predicate would make them matter, and they cost one comparison.
	if _, err := o.tx.ExecContext(ctx,
		`UPDATE notes_nodes SET position = position - 1
		  WHERE user_id = ? AND parent_id IS ? AND position > ? AND id != ?`,
		userID, parentArg(oldParent.Int64), oldPos, id); err != nil {
		return fmt.Errorf("notes: move: close the gap: %w", err)
	}

	n, err := countChildren(ctx, o.tx, userID, newParentID)
	if err != nil {
		return err
	}
	if newParentID == oldParent.Int64 {
		n-- // the bullet is still counted among its own new siblings
	}
	idx := clamp(newPos, 0, n)

	if _, err := o.tx.ExecContext(ctx,
		`UPDATE notes_nodes SET position = position + 1
		  WHERE user_id = ? AND parent_id IS ? AND position >= ? AND id != ?`,
		userID, parentArg(newParentID), idx, id); err != nil {
		return fmt.Errorf("notes: move: open a gap: %w", err)
	}

	if _, err := o.tx.ExecContext(ctx,
		`UPDATE notes_nodes SET parent_id = ?, position = ?, updated_at = ?
		  WHERE id = ? AND user_id = ?`,
		parentArg(newParentID), idx, formatTime(o.now()), id, userID); err != nil {
		return fmt.Errorf("notes: move: %w", err)
	}
	return nil
}

// Move reparents a bullet in a transaction of its own. See Ops.Move.
func (st *Store) Move(ctx context.Context, userID, id, newParentID int64, newPos int) error {
	return st.Do(ctx, func(o *Ops) error { return o.Move(ctx, userID, id, newParentID, newPos) })
}

// heightOf reports how many levels of descendants a node has: 0 for a leaf.
// It returns ErrNotFound on the same terms as depthOf, and its recursive join
// is unfiltered for the same fail-safe reason: see the note on depthOf.
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
//
// The recursive join below is deliberately unfiltered by user_id, and adding
// one would be a regression, not a cleanup. This walk exists to refuse a move
// that would create a cycle; ownership is already enforced at the anchor row
// (WHERE id = ? AND user_id = ?), and the join's only remaining job is to
// travel far enough to actually reach root if root is there. Stopping the
// join at an ownership boundary would make the walk return false for a
// candidate that genuinely is inside root's subtree — which is to say, it
// would let through the exact cycle this check exists to prevent.
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

// maxPosition means "past the last sibling". Move clamps, so this expresses
// "append" without a second code path.
const maxPosition = 1 << 30

// Indent makes a bullet the last child of the sibling above it.
//
// A bullet that is already first among its siblings has nowhere to go, and
// that is not an error: the caller is a keypress, not a command, and Tab on
// the first line of an outline should do nothing rather than complain.
//
// Reading the bullet and moving it happen in the one transaction, so the tree
// cannot change underneath the decision of which sibling to move it under.
func (o *Ops) Indent(ctx context.Context, userID, id int64) error {
	n, err := o.ByID(ctx, userID, id)
	if err != nil {
		return err
	}
	if n.Position == 0 {
		return nil
	}
	prev, err := o.siblingAt(ctx, userID, n.ParentID, n.Position-1)
	if err != nil {
		return err
	}
	return o.Move(ctx, userID, id, prev.ID, maxPosition)
}

// Indent makes a bullet the last child of the sibling above it, in a
// transaction of its own. See Ops.Indent.
func (st *Store) Indent(ctx context.Context, userID, id int64) error {
	return st.Do(ctx, func(o *Ops) error { return o.Indent(ctx, userID, id) })
}

// Outdent makes a bullet the next sibling of its own parent.
//
// Its former following siblings stay under that parent, closing the gap it
// leaves behind. Some outliners instead adopt them as children of the
// outdented bullet; this is the simpler rule and the one that never reparents
// a bullet the user was not looking at.
//
// Both reads and the move happen in the one transaction, so the parent it
// reads is the parent it moves the bullet out from under.
func (o *Ops) Outdent(ctx context.Context, userID, id int64) error {
	n, err := o.ByID(ctx, userID, id)
	if err != nil {
		return err
	}
	if n.ParentID == RootID {
		return nil
	}
	parent, err := o.ByID(ctx, userID, n.ParentID)
	if err != nil {
		return err
	}
	return o.Move(ctx, userID, id, parent.ParentID, parent.Position+1)
}

// Outdent makes a bullet the next sibling of its own parent, in a transaction
// of its own. See Ops.Outdent.
func (st *Store) Outdent(ctx context.Context, userID, id int64) error {
	return st.Do(ctx, func(o *Ops) error { return o.Outdent(ctx, userID, id) })
}

// MoveUp swaps a bullet with the sibling above it, or does nothing if it is
// already first.
func (o *Ops) MoveUp(ctx context.Context, userID, id int64) error {
	n, err := o.ByID(ctx, userID, id)
	if err != nil {
		return err
	}
	if n.Position == 0 {
		return nil
	}
	return o.Move(ctx, userID, id, n.ParentID, n.Position-1)
}

// MoveUp swaps a bullet with the sibling above it, in a transaction of its
// own. See Ops.MoveUp.
func (st *Store) MoveUp(ctx context.Context, userID, id int64) error {
	return st.Do(ctx, func(o *Ops) error { return o.MoveUp(ctx, userID, id) })
}

// MoveDown swaps a bullet with the sibling below it. A bullet that is already
// last needs no special case: Move clamps the target position back to where
// the bullet already is.
func (o *Ops) MoveDown(ctx context.Context, userID, id int64) error {
	n, err := o.ByID(ctx, userID, id)
	if err != nil {
		return err
	}
	return o.Move(ctx, userID, id, n.ParentID, n.Position+1)
}

// MoveDown swaps a bullet with the sibling below it, in a transaction of its
// own. See Ops.MoveDown.
func (st *Store) MoveDown(ctx context.Context, userID, id int64) error {
	return st.Do(ctx, func(o *Ops) error { return o.MoveDown(ctx, userID, id) })
}
