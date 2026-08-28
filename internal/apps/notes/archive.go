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

// Archive returns userID's archived nodes whose own parent is not itself
// archived — spec §13's "the roots of what was put away". A node nested
// under an already-archived ancestor is not listed here in its own right
// even if it happens to carry its own archived_at: restoring the ancestor
// is what brings the whole subtree back, this node included, so it has no
// restore action of its own to be listed for.
//
// The left join tests the parent's own archived_at without excluding a
// top-level node, whose parent_id is NULL and so has no parent row to join
// to at all: p.archived_at is then NULL too, and SQL's "NULL IS NULL" is
// true, which is exactly the "no parent, or a parent that is not archived"
// this needs — a NULL parent_id cannot be mistaken for an archived one.
//
// Ordered by archived_at, most recent first: the spec sets no ordering
// requirement, and "what did I put away most recently" is the natural way
// to scan a list like this one.
func (st *Store) Archive(ctx context.Context, userID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+aliasNodeColumns("n")+`
		   FROM notes_nodes n
		   LEFT JOIN notes_nodes p ON p.id = n.parent_id AND p.user_id = n.user_id
		  WHERE n.user_id = ? AND n.archived_at IS NOT NULL
		    AND p.archived_at IS NULL
		  ORDER BY n.archived_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("notes: archive: %w", err)
	}
	return collectNodes(rows, "archived nodes")
}
