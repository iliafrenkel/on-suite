package notes

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
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
