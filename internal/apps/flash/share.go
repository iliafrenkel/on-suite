// internal/apps/flash/share.go
package flash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Share statuses. A row starts pending and moves to exactly one of the other
// three; it never moves again after that.
const (
	ShareStatusPending  = "pending"
	ShareStatusAdopted  = "adopted"
	ShareStatusDeclined = "declined"
	ShareStatusRevoked  = "revoked"
)

// Share is one share offer from one account's deck to another. Multiple rows
// can exist for the same (DeckID, FromUserID, ToUserID) triple over time: a
// re-share after adoption inserts a new pending row for the same triple,
// which AdoptShare (see the next task) recognizes as a merge offer by
// finding an earlier row for that triple already in ShareStatusAdopted.
type Share struct {
	ID            int64
	DeckID        int64
	FromUserID    int64
	ToUserID      int64
	Status        string
	AdoptedDeckID *int64
	CreatedAt     time.Time
	RespondedAt   *time.Time
}

// ShareDeck offers deckID (one of fromUserID's own) to toUserID. Re-sharing
// while an offer to the same recipient is still pending is a no-op that
// returns the existing row, so re-clicking Share doesn't create duplicate
// entries in the recipient's list; sharing again after that offer has been
// adopted, declined, or revoked always inserts a fresh row.
func (st *Store) ShareDeck(ctx context.Context, fromUserID, deckID, toUserID int64) (Share, error) {
	if fromUserID == toUserID {
		return Share{}, fmt.Errorf("%w: you can't share a deck with yourself", ErrInvalid)
	}
	if _, err := st.DeckByID(ctx, fromUserID, deckID); err != nil {
		return Share{}, err
	}

	existing, err := st.pendingShare(ctx, deckID, fromUserID, toUserID)
	switch {
	case err == nil:
		return existing, nil
	case !errors.Is(err, ErrNotFound):
		return Share{}, err
	}

	sh := Share{DeckID: deckID, FromUserID: fromUserID, ToUserID: toUserID, Status: ShareStatusPending, CreatedAt: st.now()}
	err = st.db.QueryRowContext(ctx,
		`INSERT INTO flash_shares (deck_id, from_user_id, to_user_id, status, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 RETURNING id`,
		sh.DeckID, sh.FromUserID, sh.ToUserID, sh.Status, formatTime(sh.CreatedAt),
	).Scan(&sh.ID)
	if err != nil {
		return Share{}, fmt.Errorf("flash: share deck: %w", err)
	}
	return sh, nil
}

func (st *Store) pendingShare(ctx context.Context, deckID, fromUserID, toUserID int64) (Share, error) {
	return scanShare(st.db.QueryRowContext(ctx,
		`SELECT id, deck_id, from_user_id, to_user_id, status, adopted_deck_id, created_at, responded_at
		 FROM flash_shares
		 WHERE deck_id = ? AND from_user_id = ? AND to_user_id = ? AND status = ?`,
		deckID, fromUserID, toUserID, ShareStatusPending))
}

// RevokeShare cancels one of fromUserID's own pending offers. Only a
// pending row can be revoked — an already-adopted, declined, or
// previously-revoked row returns ErrInvalid.
func (st *Store) RevokeShare(ctx context.Context, fromUserID, shareID int64) error {
	return st.resolveShare(ctx, shareID, fromUserID, "from_user_id", ShareStatusRevoked)
}

// DeclineShare dismisses one of toUserID's own pending offers without
// adopting it. A declined offer never blocks a later fresh ShareDeck call
// from the same creator to the same recipient.
func (st *Store) DeclineShare(ctx context.Context, toUserID, shareID int64) error {
	return st.resolveShare(ctx, shareID, toUserID, "to_user_id", ShareStatusDeclined)
}

// resolveShare moves a pending row (owned by userID via ownerColumn, either
// "from_user_id" or "to_user_id") to newStatus. It reports ErrNotFound if
// the row doesn't exist or isn't userID's, and ErrInvalid if it exists and
// is userID's but is no longer pending.
func (st *Store) resolveShare(ctx context.Context, shareID, userID int64, ownerColumn, newStatus string) error {
	var ownerID int64
	err := st.db.QueryRowContext(ctx,
		`SELECT `+ownerColumn+` FROM flash_shares WHERE id = ?`, shareID).Scan(&ownerID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return fmt.Errorf("flash: resolve share: %w", err)
	case ownerID != userID:
		return ErrNotFound
	}

	res, err := st.db.ExecContext(ctx,
		`UPDATE flash_shares SET status = ?, responded_at = ? WHERE id = ? AND status = ?`,
		newStatus, formatTime(st.now()), shareID, ShareStatusPending)
	if err != nil {
		return fmt.Errorf("flash: resolve share: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("flash: resolve share: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: this share has already been resolved", ErrInvalid)
	}
	return nil
}

func scanShare(row rowScanner) (Share, error) {
	var (
		sh            Share
		createdAt     string
		adoptedDeckID sql.NullInt64
		respondedAt   sql.NullString
	)
	err := row.Scan(&sh.ID, &sh.DeckID, &sh.FromUserID, &sh.ToUserID, &sh.Status, &adoptedDeckID, &createdAt, &respondedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Share{}, ErrNotFound
	case err != nil:
		return Share{}, fmt.Errorf("flash: scan share: %w", err)
	}
	if sh.CreatedAt, err = parseTime(createdAt); err != nil {
		return Share{}, err
	}
	if adoptedDeckID.Valid {
		id := adoptedDeckID.Int64
		sh.AdoptedDeckID = &id
	}
	if respondedAt.Valid {
		t, err := parseTime(respondedAt.String)
		if err != nil {
			return Share{}, err
		}
		sh.RespondedAt = &t
	}
	return sh, nil
}
