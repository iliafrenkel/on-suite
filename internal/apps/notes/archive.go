package notes

import (
	"context"
	"fmt"
)

// ArchiveRow is one entry on /notes/archive: an archived subtree's root,
// plus its ancestor breadcrumb — the same shape Search and Due rows take,
// for the same reason: a node found outside the context of the tree it
// lives in needs its path spelled out to be legible on its own.
type ArchiveRow struct {
	Node
	Crumbs []Node
}

// archiveView is what /notes/archive renders.
type archiveView struct {
	Rows []ArchiveRow
	// CSRFToken is needed here, unlike searchView and DueGroups, because
	// this page's rows carry a real mutating form — Restore — and every
	// other one is read-only.
	CSRFToken string
}

// Archive returns userID's archived nodes with no archived ancestor —
// spec §13's "the roots of what was put away". A node nested under an
// already-archived ancestor is not listed here in its own right even if it
// happens to carry its own archived_at: restoring the ancestor is what
// brings the whole subtree back, this node included, so it has no restore
// action of its own to be listed for.
//
// n.parent_id NOT IN archived_below (store.go), not just a direct-parent
// check: a node can carry its own archived_at while sitting under an
// ancestor that is itself under a different archived ancestor still
// further up — A archived, B (child of A, not archived), C (child of B,
// archived) — and a direct-parent-only check would still list C alongside
// A, since C's own parent B is not archived. archived_below's recursive
// walk catches an archived ancestor at any depth, closing that gap —
// issue #109. "n.parent_id IS NULL OR ..." rather than relying on
// "NULL NOT IN (...)" to do the right thing on its own: SQL's NOT IN
// against a NULL operand evaluates to NULL, not true, which would
// wrongly exclude every top-level archived node.
//
// Ordered by archived_at, most recent first: the spec sets no ordering
// requirement, and "what did I put away most recently" is the natural way
// to scan a list like this one.
//
// julianday(n.archived_at), not a plain ORDER BY n.archived_at: formatTime
// (tree.go) writes RFC3339Nano, which strips a whole-second timestamp's
// trailing zero fraction, so "...T10:00:00Z" sorts after
// "...T10:00:00.5Z" under a byte-wise DESC compare ('Z' > '.') despite
// being chronologically earlier — issue #108. julianday parses the string
// as an actual instant rather than comparing bytes, so this orders
// correctly regardless of which timestamps in the table happen to land on
// a whole second.
func (st *Store) Archive(ctx context.Context, userID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE `+archivedBelowCTE+`
		 SELECT `+aliasNodeColumns("n")+`
		   FROM notes_nodes n
		  WHERE n.user_id = ? AND n.archived_at IS NOT NULL
		    AND (n.parent_id IS NULL OR n.parent_id NOT IN (SELECT id FROM archived_below))
		  ORDER BY julianday(n.archived_at) DESC`,
		userID, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("notes: archive: %w", err)
	}
	return collectNodes(rows, "archived nodes")
}
