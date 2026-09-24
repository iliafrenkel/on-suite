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

// RevokeShare cancels one of fromUserID's own pending offers, scoped to
// deckID: the route this comes from names both the deck and the share
// (POST /{deckID}/share/{shareID}/revoke), and a share whose actual deck_id
// doesn't match the URL's deckID is treated as not found — the {deckID}
// segment isn't decorative, it's part of the identity being checked. Only
// a pending row can be revoked — an already-adopted, declined, or
// previously-revoked row returns ErrInvalid.
func (st *Store) RevokeShare(ctx context.Context, fromUserID, deckID, shareID int64) error {
	return st.resolveShare(ctx, shareID, fromUserID, "from_user_id", ShareStatusRevoked, &deckID)
}

// DeclineShare dismisses one of toUserID's own pending offers without
// adopting it. A declined offer never blocks a later fresh ShareDeck call
// from the same creator to the same recipient.
func (st *Store) DeclineShare(ctx context.Context, toUserID, shareID int64) error {
	return st.resolveShare(ctx, shareID, toUserID, "to_user_id", ShareStatusDeclined, nil)
}

// resolveShare moves a pending row (owned by userID via ownerColumn, either
// "from_user_id" or "to_user_id") to newStatus. It reports ErrNotFound if
// the row doesn't exist, isn't userID's, or (when wantDeckID is non-nil)
// belongs to a different deck than the caller named, and ErrInvalid if it
// exists, is userID's, and is for the right deck but is no longer pending.
func (st *Store) resolveShare(ctx context.Context, shareID, userID int64, ownerColumn, newStatus string, wantDeckID *int64) error {
	var ownerID, deckID int64
	err := st.db.QueryRowContext(ctx,
		`SELECT `+ownerColumn+`, deck_id FROM flash_shares WHERE id = ?`, shareID).Scan(&ownerID, &deckID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return fmt.Errorf("flash: resolve share: %w", err)
	case ownerID != userID:
		return ErrNotFound
	case wantDeckID != nil && deckID != *wantDeckID:
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

// shareCardSource is one card read from the source deck during adoption or
// merge — just the fields that get copied, plus the ID that becomes the new
// card's origin_card_id.
type shareCardSource struct {
	ID        int64
	CardType  string
	Front     string
	Back      string
	Notes     string
	ImageHash *string
	AudioHash *string
}

// AdoptResult is what AdoptShare did, so callers can word a notice ("N new
// cards added to Y" vs. "Y is now in your decks") without a separate
// lookup of the share it just resolved.
type AdoptResult struct {
	Deck Deck
	// Merged is true when this offer was a re-share of a (deck, creator,
	// recipient) triple already adopted earlier — cards went into that
	// existing deck rather than a brand-new one.
	Merged bool
	// CardsCopied is how many source cards were actually copied: the full
	// source deck's card count for a first-time adoption, or just the
	// cards not already represented (by origin_card_id) for a merge —
	// which can be 0 if nothing new was added since the last adoption.
	CardsCopied int
}

// AdoptShare resolves one of toUserID's own pending offers (shareID). If no
// earlier offer for the same (deck, creator, recipient) triple has ever been
// adopted, this is a first-time adoption: a brand-new deck is created for
// toUserID and every source card is copied into it. If an earlier offer for
// that triple was already adopted, this is a merge: cards are copied into
// that same existing deck, skipping any source card already represented
// there (by origin_card_id) — so cards and progress the recipient already
// has are never touched. Either way, no FSRS review state is ever copied:
// every copied card starts brand new in the recipient's review queue.
func (st *Store) AdoptShare(ctx context.Context, toUserID, shareID int64) (AdoptResult, error) {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return AdoptResult{}, fmt.Errorf("flash: adopt share: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	sh, err := scanShare(tx.QueryRowContext(ctx,
		`SELECT id, deck_id, from_user_id, to_user_id, status, adopted_deck_id, created_at, responded_at
		 FROM flash_shares WHERE id = ?`, shareID))
	if err != nil {
		return AdoptResult{}, err
	}
	if sh.ToUserID != toUserID {
		return AdoptResult{}, ErrNotFound
	}
	if sh.Status != ShareStatusPending {
		return AdoptResult{}, fmt.Errorf("%w: this share has already been resolved", ErrInvalid)
	}

	src, err := deckByIDIgnoringOwner(ctx, tx, sh.DeckID)
	if err != nil {
		return AdoptResult{}, err
	}

	priorAdoptedDeckID, err := priorAdoptedDeck(ctx, tx, sh.DeckID, sh.FromUserID, sh.ToUserID)
	if err != nil {
		return AdoptResult{}, err
	}
	merged := priorAdoptedDeckID != nil

	var targetDeckID int64
	if priorAdoptedDeckID != nil {
		targetDeckID = *priorAdoptedDeckID
	} else {
		err = tx.QueryRowContext(ctx,
			`INSERT INTO flash_decks (user_id, name, description, created_at, color)
			 VALUES (?, ?, ?, ?, ?)
			 RETURNING id`,
			toUserID, src.Name, src.Description, formatTime(st.now()), src.Color,
		).Scan(&targetDeckID)
		if err != nil {
			if isUniqueViolation(err) {
				return AdoptResult{}, fmt.Errorf("%w: you already have a deck named %q", ErrInvalid, src.Name)
			}
			return AdoptResult{}, fmt.Errorf("flash: adopt share: %w", err)
		}
	}

	existingOrigins, err := originCardIDsInDeck(ctx, tx, targetDeckID)
	if err != nil {
		return AdoptResult{}, err
	}

	sourceCards, err := cardsInDeckIgnoringOwner(ctx, tx, sh.DeckID)
	if err != nil {
		return AdoptResult{}, err
	}
	cardsCopied := 0
	for _, sc := range sourceCards {
		if existingOrigins[sc.ID] {
			continue
		}
		tags, err := tagsForCardIgnoringOwner(ctx, tx, sc.ID)
		if err != nil {
			return AdoptResult{}, err
		}

		var newCardID int64
		err = tx.QueryRowContext(ctx,
			`INSERT INTO flash_cards (deck_id, user_id, card_type, front, back, notes, created_at, image_hash, audio_hash, origin_card_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 RETURNING id`,
			targetDeckID, toUserID, sc.CardType, sc.Front, sc.Back, sc.Notes, formatTime(st.now()), sc.ImageHash, sc.AudioHash, sc.ID,
		).Scan(&newCardID)
		if err != nil {
			return AdoptResult{}, fmt.Errorf("flash: adopt share: copy card: %w", err)
		}
		if err := upsertCardTags(ctx, tx, toUserID, newCardID, tags); err != nil {
			return AdoptResult{}, err
		}
		cardsCopied++
	}

	// The status guard here isn't reachable through this same function call —
	// the pending check above already ruled out anything but a pending row —
	// but it closes the same window resolveShare's UPDATE closes: two
	// concurrent adopts of the same share racing between that check and this
	// UPDATE. Without "AND status = 'pending'", the loser would still report
	// success and silently duplicate the copied deck/cards; with it, the
	// loser's UPDATE affects 0 rows and the whole transaction rolls back.
	res, err := tx.ExecContext(ctx,
		`UPDATE flash_shares SET status = ?, adopted_deck_id = ?, responded_at = ? WHERE id = ? AND status = ?`,
		ShareStatusAdopted, targetDeckID, formatTime(st.now()), shareID, ShareStatusPending)
	if err != nil {
		return AdoptResult{}, fmt.Errorf("flash: adopt share: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return AdoptResult{}, fmt.Errorf("flash: adopt share: %w", err)
	}
	if n == 0 {
		return AdoptResult{}, fmt.Errorf("%w: this share has already been resolved", ErrInvalid)
	}

	if err := tx.Commit(); err != nil {
		return AdoptResult{}, fmt.Errorf("flash: adopt share: %w", err)
	}
	d, err := st.DeckByID(ctx, toUserID, targetDeckID)
	if err != nil {
		return AdoptResult{}, err
	}
	return AdoptResult{Deck: d, Merged: merged, CardsCopied: cardsCopied}, nil
}

// deckByIDIgnoringOwner reads a deck regardless of who owns it — used inside
// AdoptShare, where the recipient legitimately needs to read the source
// deck's name/description despite not owning it, because they hold a valid
// pending share for it (checked by the caller before this is called).
// SharePreview needs the same unowned read for the same reason, but does it
// with its own inline query rather than calling this helper.
func deckByIDIgnoringOwner(ctx context.Context, tx *sql.Tx, id int64) (Deck, error) {
	return scanDeck(tx.QueryRowContext(ctx,
		`SELECT `+deckColumns+` FROM flash_decks WHERE id = ?`, id))
}

// cardsInDeckIgnoringOwner reads every card in deckID regardless of owner —
// same justification as deckByIDIgnoringOwner.
func cardsInDeckIgnoringOwner(ctx context.Context, tx *sql.Tx, deckID int64) ([]shareCardSource, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, card_type, front, back, notes, image_hash, audio_hash
		 FROM flash_cards WHERE deck_id = ?`, deckID)
	if err != nil {
		return nil, fmt.Errorf("flash: cards in deck: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []shareCardSource
	for rows.Next() {
		var (
			sc        shareCardSource
			imageHash sql.NullString
			audioHash sql.NullString
		)
		if err := rows.Scan(&sc.ID, &sc.CardType, &sc.Front, &sc.Back, &sc.Notes, &imageHash, &audioHash); err != nil {
			return nil, fmt.Errorf("flash: cards in deck: %w", err)
		}
		if imageHash.Valid {
			sc.ImageHash = &imageHash.String
		}
		if audioHash.Valid {
			sc.AudioHash = &audioHash.String
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// tagsForCardIgnoringOwner reads cardID's tag names regardless of owner —
// same justification as deckByIDIgnoringOwner. Unlike TagsForCard, it does
// not scope by user_id: the source card belongs to the creator, not the
// recipient calling AdoptShare.
func tagsForCardIgnoringOwner(ctx context.Context, tx *sql.Tx, cardID int64) ([]string, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT t.name FROM flash_tags t
		 JOIN flash_card_tags ct ON ct.tag_id = t.id
		 WHERE ct.card_id = ?`, cardID)
	if err != nil {
		return nil, fmt.Errorf("flash: tags for card: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("flash: tags for card: %w", err)
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// originCardIDsInDeck returns the set of source-card IDs already
// represented in deckID (via origin_card_id) — cards in this set are
// skipped during a merge because the recipient already has them.
func originCardIDsInDeck(ctx context.Context, tx *sql.Tx, deckID int64) (map[int64]bool, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT origin_card_id FROM flash_cards WHERE deck_id = ? AND origin_card_id IS NOT NULL`, deckID)
	if err != nil {
		return nil, fmt.Errorf("flash: origin card ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("flash: origin card ids: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

// priorAdoptedDeck finds the most recent already-adopted share for the same
// (deck, creator, recipient) triple, if any. Its presence is what makes the
// current pending share a merge offer rather than a first-time adoption.
func priorAdoptedDeck(ctx context.Context, tx *sql.Tx, deckID, fromUserID, toUserID int64) (*int64, error) {
	var id sql.NullInt64
	err := tx.QueryRowContext(ctx,
		`SELECT adopted_deck_id FROM flash_shares
		 WHERE deck_id = ? AND from_user_id = ? AND to_user_id = ? AND status = ?
		 ORDER BY id DESC LIMIT 1`,
		deckID, fromUserID, toUserID, ShareStatusAdopted).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("flash: prior adopted deck: %w", err)
	case !id.Valid:
		// Reachable since migration 0008: adopted_deck_id is
		// ON DELETE SET NULL, so a recipient who deletes their own adopted
		// copy leaves this row's status at "adopted" with adopted_deck_id
		// now NULL. Treating that the same as "no prior adoption" is
		// exactly right — a later re-share of the same triple should
		// behave as a fresh first-time adoption, not a merge into a deck
		// that no longer exists.
		return nil, nil
	}
	v := id.Int64
	return &v, nil
}

// SharesForDeck lists every share offer (any status) fromUserID has made
// for one of their own decks, newest first — the creator's "Shared with"
// list.
func (st *Store) SharesForDeck(ctx context.Context, fromUserID, deckID int64) ([]Share, error) {
	if _, err := st.DeckByID(ctx, fromUserID, deckID); err != nil {
		return nil, err
	}
	rows, err := st.db.QueryContext(ctx,
		`SELECT id, deck_id, from_user_id, to_user_id, status, adopted_deck_id, created_at, responded_at
		 FROM flash_shares
		 WHERE deck_id = ? AND from_user_id = ?
		 ORDER BY created_at DESC, id DESC`, deckID, fromUserID)
	if err != nil {
		return nil, fmt.Errorf("flash: shares for deck: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Share
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// ShareOffer is one pending share addressed to a recipient, enriched with
// what a gift row and its preview pane need to display: the source deck's
// name, and — if a merge offer — how many source cards the recipient doesn't
// have yet.
type ShareOffer struct {
	Share
	DeckName  string
	DeckColor string // the source deck's colour, for the gift row
	// PriorAdoptedDeckID is nil for a first-time offer ("New deck") and
	// non-nil for a merge offer (an earlier offer for the same triple was
	// already adopted into that deck).
	PriorAdoptedDeckID *int64
	// NewCardCount is meaningful only when PriorAdoptedDeckID is non-nil.
	NewCardCount int
}

// SharesForRecipient lists every pending offer addressed to toUserID, newest
// first — the recipient's gift rows.
func (st *Store) SharesForRecipient(ctx context.Context, toUserID int64) ([]ShareOffer, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT s.id, s.deck_id, s.from_user_id, s.to_user_id, s.status, s.adopted_deck_id, s.created_at, s.responded_at,
		        d.name, d.color,
		        (SELECT ad.adopted_deck_id FROM flash_shares ad
		          WHERE ad.deck_id = s.deck_id AND ad.from_user_id = s.from_user_id AND ad.to_user_id = s.to_user_id
		            AND ad.status = ?
		          ORDER BY ad.id DESC LIMIT 1)
		   FROM flash_shares s
		   JOIN flash_decks d ON d.id = s.deck_id
		  WHERE s.to_user_id = ? AND s.status = ?
		  ORDER BY s.created_at DESC, s.id DESC`,
		ShareStatusAdopted, toUserID, ShareStatusPending)
	if err != nil {
		return nil, fmt.Errorf("flash: shares for recipient: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ShareOffer
	for rows.Next() {
		var (
			o                  ShareOffer
			createdAt          string
			adoptedDeckID      sql.NullInt64
			respondedAt        sql.NullString
			priorAdoptedDeckID sql.NullInt64
		)
		err := rows.Scan(&o.ID, &o.DeckID, &o.FromUserID, &o.ToUserID, &o.Status, &adoptedDeckID, &createdAt, &respondedAt,
			&o.DeckName, &o.DeckColor, &priorAdoptedDeckID)
		if err != nil {
			return nil, fmt.Errorf("flash: shares for recipient: %w", err)
		}
		if o.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if adoptedDeckID.Valid {
			id := adoptedDeckID.Int64
			o.AdoptedDeckID = &id
		}
		if respondedAt.Valid {
			t, err := parseTime(respondedAt.String)
			if err != nil {
				return nil, err
			}
			o.RespondedAt = &t
		}
		if priorAdoptedDeckID.Valid {
			id := priorAdoptedDeckID.Int64
			o.PriorAdoptedDeckID = &id
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	for i := range out {
		if out[i].PriorAdoptedDeckID == nil {
			continue
		}
		n, err := st.newCardCount(ctx, out[i].DeckID, *out[i].PriorAdoptedDeckID)
		if err != nil {
			return nil, err
		}
		out[i].NewCardCount = n
	}

	return out, nil
}

// newCardCount counts source deck cards not yet represented (by
// origin_card_id) in targetDeckID — computed fresh each time rather than
// stored, so it's always accurate even as more cards are adopted or deleted
// between offers.
func (st *Store) newCardCount(ctx context.Context, sourceDeckID, targetDeckID int64) (int, error) {
	var n int
	err := st.db.QueryRowContext(ctx,
		`SELECT count(*) FROM flash_cards sc
		 WHERE sc.deck_id = ?
		   AND NOT EXISTS (
		     SELECT 1 FROM flash_cards tc WHERE tc.deck_id = ? AND tc.origin_card_id = sc.id
		   )`, sourceDeckID, targetDeckID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("flash: new card count: %w", err)
	}
	return n, nil
}

// sharePreviewSamples is how many cards a gift deck's preview shows.
const sharePreviewSamples = 4

// SharePreview is what the recipient sees before adopting a share: the
// source deck's name, description and colour, how many cards they would
// get, and a few of them. Deck is the *source* deck — someone else's — so
// only its display fields are meant to be shown.
type SharePreview struct {
	Offer     ShareOffer
	Deck      Deck
	CardCount int
	Samples   []Card
}

// SharePreview loads the preview of one of toUserID's pending offers. Any
// other share id — missing, addressed to someone else, or already
// resolved — is ErrNotFound. That guard is SharesForRecipient itself (it
// only returns pending offers addressed to toUserID), and it runs before
// the source deck is read without an owner check.
func (st *Store) SharePreview(ctx context.Context, toUserID, shareID int64) (SharePreview, error) {
	offers, err := st.SharesForRecipient(ctx, toUserID)
	if err != nil {
		return SharePreview{}, err
	}
	var p SharePreview
	found := false
	for _, o := range offers {
		if o.ID == shareID {
			p.Offer, found = o, true
			break
		}
	}
	if !found {
		return SharePreview{}, ErrNotFound
	}

	p.Deck, err = scanDeck(st.db.QueryRowContext(ctx,
		`SELECT `+deckColumns+` FROM flash_decks WHERE id = ?`, p.Offer.DeckID))
	if err != nil {
		return SharePreview{}, err
	}

	// A merge only brings the cards not already adopted; a first-time
	// adopt brings them all. Same NOT EXISTS rule as newCardCount.
	where := `c.deck_id = ?`
	args := []any{p.Offer.DeckID}
	if p.Offer.PriorAdoptedDeckID != nil {
		where += ` AND NOT EXISTS (SELECT 1 FROM flash_cards tc WHERE tc.deck_id = ? AND tc.origin_card_id = c.id)`
		args = append(args, *p.Offer.PriorAdoptedDeckID)
	}
	if err := st.db.QueryRowContext(ctx,
		`SELECT count(*) FROM flash_cards c WHERE `+where, args...).Scan(&p.CardCount); err != nil {
		return SharePreview{}, fmt.Errorf("flash: share preview: %w", err)
	}
	rows, err := st.db.QueryContext(ctx,
		`SELECT c.id, c.deck_id, c.user_id, c.card_type, c.front, c.back, c.notes, c.created_at, c.image_hash, c.audio_hash
		   FROM flash_cards c WHERE `+where+`
		  ORDER BY c.created_at ASC, c.id ASC LIMIT ?`,
		append(args, sharePreviewSamples)...)
	if err != nil {
		return SharePreview{}, fmt.Errorf("flash: share preview: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		c, err := scanCardRow(rows)
		if err != nil {
			return SharePreview{}, err
		}
		p.Samples = append(p.Samples, c)
	}
	return p, rows.Err()
}
