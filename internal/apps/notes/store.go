package notes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
//
// aliasNodeColumns below parses this by splitting on ", " — that separator must stay exactly ", ".
const nodeColumns = `id, user_id, parent_id, position, title, note, collapsed, created_at, updated_at, done_at, due_on, archived_at`

// The recursive CTEs below need the same list qualified by a table alias, in
// the same order. Writing it out again would put the column order in three
// places, and every later chunk that adds a column — done_at, due_on,
// archived_at, share_slug — would have to edit all three in step, with a
// runtime scan error as the only warning if it missed one.
var (
	// parentColumns is the list as the "p" row of Ancestors' upward walk.
	parentColumns = aliasNodeColumns("p")
	// childColumns is the list as the "c" row of Outline's descent.
	childColumns = aliasNodeColumns("c")
)

// archivedBelowCTE names every one of a user's node ids that is archived,
// or sits anywhere under an archived ancestor — spec §13: "an archived
// node and its subtree disappear from ... search results" (§12), and
// "archived nodes are excluded" from /notes/due (§11). Search and Due both
// match against notes_nodes directly rather than descending from a root
// the way Outline does, so unlike Outline — whose recursive walk simply
// never reaches an archived node's children in the first place — they
// need this table of ids to exclude explicitly. It takes two ?
// placeholders, both the caller's user_id.
//
// UNION, not UNION ALL: an id already found cannot be found again, which
// is what makes this safe against a cycle a bug might have introduced —
// with a finite, deduplicated id space, the recursion has nowhere left to
// go once every reachable id is already in it, the same guarantee
// Outline's own MaxDepth cap gives its own descent.
const archivedBelowCTE = `
	archived_below(id) AS (
	    SELECT id FROM notes_nodes WHERE user_id = ? AND archived_at IS NOT NULL
	  UNION
	    SELECT c.id FROM notes_nodes c JOIN archived_below a ON c.parent_id = a.id
	     WHERE c.user_id = ?
	)`

// aliasNodeColumns prefixes each of nodeColumns with a table alias.
func aliasNodeColumns(alias string) string {
	cols := strings.Split(nodeColumns, ", ")
	for i, col := range cols {
		cols[i] = alias + "." + col
	}
	return strings.Join(cols, ", ")
}

// ByID fetches one of userID's own nodes.
//
// A write that decides what to do from what it read calls Ops.ByID instead, so
// that the read and the write are the same transaction. Calling this from
// inside a Do closure waits for the connection that transaction is holding,
// and waits for ever.
func (st *Store) ByID(ctx context.Context, userID, id int64) (Node, error) {
	return nodeByID(ctx, st.db, userID, id)
}

// querier is the read surface shared by *sql.DB and *sql.Tx, so that nodeByID
// has one implementation rather than one per caller. It exists for that read
// alone: every other read against a transaction takes a *sql.Tx outright, so
// that the compiler keeps it out of reach of the connection pool.
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

// siblingAt fetches the child of parentID sitting at a given position. Only
// Ops.siblingAt reaches it, so it takes the transaction directly, alongside
// depthOf, heightOf, countChildren and isDescendant.
func siblingAt(ctx context.Context, tx *sql.Tx, userID, parentID int64, pos int) (Node, error) {
	n, err := scanNode(tx.QueryRowContext(ctx,
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
		n          Node
		parent     sql.NullInt64
		createdAt  string
		updatedAt  string
		doneAt     sql.NullString
		dueOn      sql.NullString
		archivedAt sql.NullString
	)
	dest := append([]any{
		&n.ID, &n.UserID, &parent, &n.Position, &n.Title, &n.Note,
		&n.Collapsed, &createdAt, &updatedAt, &doneAt, &dueOn, &archivedAt,
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
	n.Done = doneAt.Valid
	// sql.NullString's String is "" when Valid is false, which is already
	// DueOn's own "none" sentinel — no extra branch needed.
	n.DueOn = dueOn.String
	n.Archived = archivedAt.Valid

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
// be RootID for the top level. It is empty both for a parent with no
// children and for a parent that does not exist or is not userID's: a caller
// that needs to tell those apart calls ByID.
//
// Calling it from inside a Do closure waits for the connection that
// transaction is holding, and waits for ever, so a render belongs after Do has
// returned rather than inside it.
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
//
// The walk up is filtered by user_id at every step, for the reason given on
// Outline: a breadcrumb must not be able to display another household's text,
// even if invariant I2 has been broken.
//
// Calling it from inside a Do closure waits for the connection that
// transaction is holding, and waits for ever, so a render belongs after Do has
// returned rather than inside it.
func (st *Store) Ancestors(ctx context.Context, userID, id int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE up AS (
		     SELECT `+nodeColumns+`, 0 AS d
		       FROM notes_nodes WHERE id = ? AND user_id = ?
		   UNION ALL
		     SELECT `+parentColumns+`, u.d + 1
		       FROM notes_nodes p JOIN up u ON p.id = u.parent_id
		      WHERE p.user_id = u.user_id AND u.d < ?
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
// Depth relative to rootID and HasChildren set. rootID may be RootID. It is
// empty both for a root with nothing visible under it and for a root that
// does not exist or is not userID's: a caller that needs to tell those apart
// calls ByID.
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
//
// An archived node, and everything under it, is kept out of tree the same
// way a collapsed node's children are: the recursive step's own WHERE
// clause excludes it, so nothing can ever join onto it as a parent —
// spec §13. This is not a preference the way hideDone's showCompleted is
// (view.go): there is no "show archived" toggle anywhere in the design, so
// exclusion belongs in the query itself rather than in a post-filter that
// would need one.
//
// Both the descent and the has_children test match on the owner as well as on
// the parent, rather than leaning on invariant I2 to supply it. I2 is an
// application invariant and not a schema constraint — parent_id is a plain
// foreign key, not a composite (parent_id, user_id) one — so a bug, a manual
// repair or a later import path could break it, and an unfiltered join would
// then put another household's bullets on this user's screen. Matching on
// user_id turns that into a subtree that simply does not render. It is also
// what makes both of them index seeks: parent_id alone is not a prefix of
// notes_nodes_user_parent_pos_idx.
//
// Calling it from inside a Do closure waits for the connection that
// transaction is holding, and waits for ever, so a render belongs after Do has
// returned rather than inside it.
//
// The archived_at exclusion above applies to the ROWS this returns, not to
// rootID itself: nothing here checks whether rootID is archived, so a caller
// that descends from an archived root still gets back its non-archived
// children exactly as if the root were ordinary. That is deliberate, not an
// oversight — there is no route that reaches an archived id except by URL
// (archive.html's own entry point links straight at it), so treating that as
// "the user asked to look inside this archived bullet" and rendering its
// still-visible contents is a reasonable affordance, not a bug to close by
// re-filtering on rootID. The handler layer is responsible for telling the
// two cases apart for display: renderOutline fetches rootID via ByID (which
// is not archived-filtered) and the outline template shows a banner when
// that root's Archived is true. A future consumer that walks a subtree from
// an arbitrary root — N9's share pages are the known case — will need to make
// this same choice explicitly rather than assuming one behavior or the
// other.
func (st *Store) Outline(ctx context.Context, userID, rootID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE tree AS (
		     SELECT `+nodeColumns+`, 0 AS depth, printf('%08d', position) AS path
		       FROM notes_nodes
		      WHERE user_id = ? AND parent_id IS ? AND archived_at IS NULL
		   UNION ALL
		     SELECT `+childColumns+`,
		            t.depth + 1, t.path || '/' || printf('%08d', c.position)
		       FROM notes_nodes c JOIN tree t ON c.parent_id = t.id
		      WHERE c.user_id = t.user_id AND c.archived_at IS NULL
		        AND t.collapsed = 0 AND t.depth + 1 <= ?
		 )
		 SELECT `+nodeColumns+`, depth,
		        EXISTS (SELECT 1 FROM notes_nodes k
		                 WHERE k.user_id = tree.user_id AND k.parent_id = tree.id
		                   AND k.archived_at IS NULL)
		   FROM tree ORDER BY path`,
		userID, parentArg(rootID), MaxDepth)
	if err != nil {
		return nil, fmt.Errorf("notes: outline of %d: %w", rootID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Node
	for rows.Next() {
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
