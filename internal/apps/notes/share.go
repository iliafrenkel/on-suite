package notes

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
)

// Share mints a fresh, unguessable slug for id and returns it — spec §15.
//
// Re-sharing always generates a new slug rather than reusing the old one,
// so a link that was revoked can never come back — the same rule ON
// Paste already applies to snippets (internal/apps/paste/store.go's own
// Share).
func (o *Ops) Share(ctx context.Context, userID, id int64) (string, error) {
	slug, err := newShareSlug()
	if err != nil {
		return "", err
	}
	if err := o.update(ctx, "share",
		`UPDATE notes_nodes SET share_slug = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		slug, formatTime(o.now()), id, userID); err != nil {
		return "", err
	}
	return slug, nil
}

// Unshare revokes id's public link.
func (o *Ops) Unshare(ctx context.Context, userID, id int64) error {
	return o.update(ctx, "unshare",
		`UPDATE notes_nodes SET share_slug = NULL, updated_at = ? WHERE id = ? AND user_id = ?`,
		formatTime(o.now()), id, userID)
}

// Share mints a fresh slug for id, in a transaction of its own. See
// Ops.Share.
func (st *Store) Share(ctx context.Context, userID, id int64) (string, error) {
	var slug string
	err := st.Do(ctx, func(o *Ops) error {
		s, err := o.Share(ctx, userID, id)
		slug = s
		return err
	})
	return slug, err
}

// Unshare revokes id's public link, in a transaction of its own. See
// Ops.Unshare.
func (st *Store) Unshare(ctx context.Context, userID, id int64) error {
	return st.Do(ctx, func(o *Ops) error { return o.Unshare(ctx, userID, id) })
}

// ByShareSlug fetches a node by its public slug, with no owner check —
// possessing the slug is the authorisation, exactly as
// internal/apps/paste/store.go's own ByShareSlug documents. "" and any
// slug nobody has minted both answer ErrNotFound, indistinguishably.
func (st *Store) ByShareSlug(ctx context.Context, slug string) (Node, error) {
	if slug == "" {
		return Node{}, ErrNotFound
	}
	n, err := scanNode(st.db.QueryRowContext(ctx,
		`SELECT `+nodeColumns+` FROM notes_nodes WHERE share_slug = ?`, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	return n, err
}

// newShareSlug mirrors internal/apps/paste/store.go's own newShareSlug
// exactly; apps never import each other, so this is an independent
// implementation with the same justification, not a shared symbol.
func newShareSlug() (string, error) {
	buf := make([]byte, shareSlugBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("notes: generate share slug: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// SharedSubtree returns every non-archived, non-done descendant of rootID,
// in pre-order with Depth relative to rootID — the query behind
// GET /notes/s/{slug} (Task 4). rootID is never RootID here: a slug always
// names one real, already-shared node, unlike Outline/Export which also
// handle the implicit top level.
//
// Unlike Outline (store.go) this does not stop at a collapsed node — per
// this plan's resolved design question, a share link means "here's
// everything under this bullet," and the page has no interactivity to
// un-collapse anything anyway. Unlike Export (export.go) it DOES exclude
// archived and done descendants — spec §13/§15 — because unlike a backup,
// a share page is something the owner is showing another person, and
// showing them "finished" or "put away" content there is exactly what
// those two states mean to hide elsewhere in the app. The root itself is
// deliberately not filtered here: the caller already resolved it via
// ByShareSlug, and per this plan's own resolved design question, sharing
// stays live even if the root is later archived or done, the same way a
// direct zoom to an archived node already renders rather than 404s.
//
// The owner-matching mirrors Outline's and Export's own, for the same
// defence-in-depth reason given on both: parent_id is a plain foreign key,
// not a composite (parent_id, user_id) one, so matching user_id at every
// step of the descent is what keeps a broken invariant I2 from leaking
// another household's bullets onto a share page.
func (st *Store) SharedSubtree(ctx context.Context, userID, rootID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE tree AS (
		     SELECT `+nodeColumns+`, 0 AS depth, printf('%08d', position) AS path
		       FROM notes_nodes
		      WHERE user_id = ? AND parent_id = ? AND archived_at IS NULL AND done_at IS NULL
		   UNION ALL
		     SELECT `+childColumns+`,
		            t.depth + 1, t.path || '/' || printf('%08d', c.position)
		       FROM notes_nodes c JOIN tree t ON c.parent_id = t.id
		      WHERE c.user_id = t.user_id AND c.archived_at IS NULL AND c.done_at IS NULL
		        AND t.depth + 1 <= ?
		 )
		 SELECT `+nodeColumns+`, depth FROM tree ORDER BY path`,
		userID, rootID, MaxDepth)
	if err != nil {
		return nil, fmt.Errorf("notes: shared subtree of %d: %w", rootID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Node
	for rows.Next() {
		var depth int
		n, err := scanNode(rows, &depth)
		if err != nil {
			return nil, err
		}
		n.Depth = depth
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notes: shared subtree: %w", err)
	}
	return out, nil
}

// sharedRow is one bullet on the public share page — a small, deliberate
// subset of outline.html's outlineRow (view.go): no RootID, no
// CSRFToken, no Last, no Overdue, because the shared page has no forms,
// no move buttons and no due-date urgency styling at all. Duplicating
// nest's own tree-building shape here (nestShared below) rather than
// generalising nest() to serve both is a deliberate YAGNI call: the two
// row types diverge enough (outlineRow carries five fields sharedRow has
// no use for) that a shared generic would need type parameters or extra
// options for a single caller, which is not simpler than two ~15-line
// functions.
type sharedRow struct {
	Node
	Children      []*sharedRow
	RenderedTitle template.HTML
	RenderedNote  template.HTML
}

// nestShared turns SharedSubtree's flat pre-order slice into the tree the
// shared page template renders as nested <ul>s — see nest (view.go),
// whose ordering assumption (a parent immediately precedes its subtree,
// depth rises by at most one row to the next) this relies on identically.
func nestShared(flat []Node) []*sharedRow {
	var top []*sharedRow
	open := make([]*sharedRow, 0, MaxDepth+1)

	for _, n := range flat {
		row := &sharedRow{Node: n, RenderedTitle: Render(n.Title), RenderedNote: Render(n.Note)}

		switch d := n.Depth; {
		case d == 0:
			top = append(top, row)
			open = open[:0]
		case d > 0 && d <= len(open):
			parent := open[d-1]
			parent.Children = append(parent.Children, row)
			open = open[:d]
		default:
			continue
		}
		open = append(open, row)
	}
	return top
}

// sharedView is what templates/shared.html renders.
type sharedView struct {
	Root Node
	Rows []*sharedRow
}
