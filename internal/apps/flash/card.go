// internal/apps/flash/card.go
package flash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// CardTypeBasic is a plain front/back card.
	CardTypeBasic = "basic"
	// CardTypeCloze is a fill-in-the-blank card: Front carries the
	// {{c1::...}}-style markers, Back is unused (F1 stores and validates
	// this shape; parsing the markers for review is F2's concern).
	CardTypeCloze = "cloze"

	// MaxCardFieldBytes bounds Front, Back, and Notes independently.
	MaxCardFieldBytes = 16 << 10
)

// Card is one flash card belonging to a deck.
type Card struct {
	ID        int64
	DeckID    int64
	UserID    int64
	CardType  string
	Front     string
	Back      string
	Notes     string
	CreatedAt time.Time

	// ImageHash and AudioHash name a row in flash_media, or nil if the card
	// has no image/audio attached. See media.go/media_store.go.
	ImageHash *string
	AudioHash *string
}

func isKnownCardType(t string) bool {
	return t == CardTypeBasic || t == CardTypeCloze
}

// ValidateCard checks a card's user-supplied fields against the rules for
// its type. Exported because the handler reports these messages back to the
// user.
func ValidateCard(cardType, front, back string) error {
	if !isKnownCardType(cardType) {
		return fmt.Errorf("%w: %q is not a card type I know", ErrInvalid, cardType)
	}
	if strings.TrimSpace(front) == "" {
		return fmt.Errorf("%w: the card needs a front", ErrInvalid)
	}
	if !utf8.ValidString(front) || len(front) > MaxCardFieldBytes {
		return fmt.Errorf("%w: the front is invalid or too long", ErrInvalid)
	}
	if !utf8.ValidString(back) || len(back) > MaxCardFieldBytes {
		return fmt.Errorf("%w: the back is invalid or too long", ErrInvalid)
	}

	switch cardType {
	case CardTypeBasic:
		if strings.TrimSpace(back) == "" {
			return fmt.Errorf("%w: a basic card needs a back", ErrInvalid)
		}
	case CardTypeCloze:
		if !strings.Contains(front, "{{") || !strings.Contains(front, "}}") {
			return fmt.Errorf("%w: a cloze card's front needs at least one {{...}} deletion", ErrInvalid)
		}
		if strings.TrimSpace(back) != "" {
			return fmt.Errorf("%w: a cloze card's answer comes from its front; leave the back empty", ErrInvalid)
		}
	}
	return nil
}

func validateCardNotes(notes string) error {
	if !utf8.ValidString(notes) || len(notes) > MaxCardFieldBytes {
		return fmt.Errorf("%w: the notes are invalid or too long", ErrInvalid)
	}
	return nil
}

// CreateCard stores a new card in one of userID's own decks. It is
// SaveCardForm with no tags or media, so its deck check runs inside the
// same transaction as the insert.
func (st *Store) CreateCard(ctx context.Context, userID, deckID int64, cardType, front, back, notes string) (Card, error) {
	return st.SaveCardForm(ctx, userID, deckID, 0, CardForm{CardType: cardType, Front: front, Back: back, Notes: notes})
}

// insertCard is CreateCard's INSERT against either the handle or a caller's
// open transaction (see dbExecutor). It does no ownership check of its own.
func insertCard(ctx context.Context, exec dbExecutor, c Card) (int64, error) {
	var id int64
	err := exec.QueryRowContext(ctx,
		`INSERT INTO flash_cards (deck_id, user_id, card_type, front, back, notes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 RETURNING id`,
		c.DeckID, c.UserID, c.CardType, c.Front, c.Back, c.Notes, formatTime(c.CreatedAt),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("flash: create card: %w", err)
	}
	return id, nil
}

// UpdateCard overwrites userID's own card's editable fields.
func (st *Store) UpdateCard(ctx context.Context, userID, deckID, id int64, cardType, front, back, notes string) (Card, error) {
	if err := ValidateCard(cardType, front, back); err != nil {
		return Card{}, err
	}
	if err := validateCardNotes(notes); err != nil {
		return Card{}, err
	}
	if err := updateCardRow(ctx, st.db, userID, deckID, id, cardType, front, back, notes); err != nil {
		return Card{}, err
	}
	return st.CardByID(ctx, userID, deckID, id)
}

// updateCardRow is UpdateCard's UPDATE on any dbExecutor. Its WHERE clause
// is the ownership check: a card that isn't userID's, or isn't in deckID,
// matches no row and is ErrNotFound.
func updateCardRow(ctx context.Context, exec dbExecutor, userID, deckID, id int64, cardType, front, back, notes string) error {
	res, err := exec.ExecContext(ctx,
		`UPDATE flash_cards SET card_type = ?, front = ?, back = ?, notes = ?
		 WHERE id = ? AND deck_id = ? AND user_id = ?`,
		cardType, front, back, notes, id, deckID, userID)
	if err != nil {
		return fmt.Errorf("flash: update card: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("flash: update card: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CardByID fetches one of userID's own cards, scoped to its deck.
func (st *Store) CardByID(ctx context.Context, userID, deckID, id int64) (Card, error) {
	return cardByID(ctx, st.db, userID, deckID, id)
}

func cardByID(ctx context.Context, exec dbExecutor, userID, deckID, id int64) (Card, error) {
	return scanCard(exec.QueryRowContext(ctx,
		`SELECT id, deck_id, user_id, card_type, front, back, notes, created_at, image_hash, audio_hash
		 FROM flash_cards WHERE id = ? AND deck_id = ? AND user_id = ?`, id, deckID, userID))
}

// deckOwned confirms deckID is one of userID's own decks, on any
// dbExecutor so SaveCardForm can check inside its transaction. As with
// cardOwner, only sql.ErrNoRows means ErrNotFound; anything else is a real
// error (#288).
func deckOwned(ctx context.Context, exec dbExecutor, userID, deckID int64) error {
	var one int
	err := exec.QueryRowContext(ctx,
		`SELECT 1 FROM flash_decks WHERE id = ? AND user_id = ?`, deckID, userID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("flash: deck owner: %w", err)
	}
	return nil
}

// CardUpload is one new image or sound for a card, already checked by the
// handler (size, sniffed content type).
type CardUpload struct {
	ContentType string
	Data        []byte
}

// CardForm is everything the card editor submits.
type CardForm struct {
	CardType, Front, Back, Notes string
	Tags                         []string
	Image, Audio                 *CardUpload // nil = no new file for that kind
	RemoveImage, RemoveAudio     bool        // ignored for a kind that has a new file (#328)
}

// SaveCardForm creates (cardID == 0) or updates one of userID's own cards in
// deckID from the card editor, replacing its tags and applying its media
// changes, all in one transaction (#363).
//
// The card form used to save in three steps — the card row, then its tags,
// then each media kind — each its own transaction, so a failure at any step
// after the first (a bad tag write, a failed upload) left a half-saved card:
// a new card with no tags, or edited text next to the old tags. Now either
// everything the form submitted lands, or nothing does.
//
// Every user-supplied field is validated before the transaction starts, so
// bad input is ErrInvalid with nothing written. The ownership checks —
// deckOwned for a new card, updateCardRow's WHERE clause for an existing
// one — run inside the transaction, never before it, for the reason
// SetCardTags documents (#294). Nothing in this method may use st.db once
// the transaction has started: with SetMaxOpenConns(1), that would deadlock
// waiting for the connection the transaction already holds.
//
// A new file for a kind always wins over that kind's Remove flag (#328).
func (st *Store) SaveCardForm(ctx context.Context, userID, deckID, cardID int64, f CardForm) (Card, error) {
	if err := ValidateCard(f.CardType, f.Front, f.Back); err != nil {
		return Card{}, err
	}
	if err := validateCardNotes(f.Notes); err != nil {
		return Card{}, err
	}
	if err := ValidateTagNames(f.Tags); err != nil {
		return Card{}, err
	}

	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return Card{}, fmt.Errorf("flash: save card: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	id := cardID
	if id == 0 {
		if err := deckOwned(ctx, tx, userID, deckID); err != nil {
			return Card{}, err
		}
		id, err = insertCard(ctx, tx, Card{
			DeckID: deckID, UserID: userID, CardType: f.CardType,
			Front: f.Front, Back: f.Back, Notes: f.Notes, CreatedAt: st.now(),
		})
		if err != nil {
			return Card{}, err
		}
	} else if err := updateCardRow(ctx, tx, userID, deckID, id, f.CardType, f.Front, f.Back, f.Notes); err != nil {
		return Card{}, err
	}

	if err := replaceCardTags(ctx, tx, userID, id, f.Tags); err != nil {
		return Card{}, err
	}

	media := []struct {
		kind   string
		upload *CardUpload
		remove bool
	}{
		{MediaKindImage, f.Image, f.RemoveImage},
		{MediaKindAudio, f.Audio, f.RemoveAudio},
	}
	for _, m := range media {
		switch {
		case m.upload != nil:
			if _, err := attachUpload(ctx, tx, st.now(), userID, deckID, id, m.kind, *m.upload); err != nil {
				return Card{}, err
			}
		case m.remove:
			if err := setCardMedia(ctx, tx, userID, deckID, id, m.kind, nil); err != nil {
				return Card{}, err
			}
		}
	}

	c, err := cardByID(ctx, tx, userID, deckID, id)
	if err != nil {
		return Card{}, err
	}
	if err := tx.Commit(); err != nil {
		return Card{}, fmt.Errorf("flash: save card: %w", err)
	}
	return c, nil
}

// ListCards returns userID's cards in one deck, newest first.
func (st *Store) ListCards(ctx context.Context, userID, deckID int64) ([]Card, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT id, deck_id, user_id, card_type, front, back, notes, created_at, image_hash, audio_hash
		 FROM flash_cards WHERE deck_id = ? AND user_id = ?
		 ORDER BY created_at DESC, id DESC`, deckID, userID)
	if err != nil {
		return nil, fmt.Errorf("flash: list cards: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Card
	for rows.Next() {
		c, err := scanCardRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flash: list cards: %w", err)
	}
	return out, nil
}

// MediaKindImage and MediaKindAudio select which of a card's two media
// columns SetCardMedia writes.
const (
	MediaKindImage = "image"
	MediaKindAudio = "audio"
)

// SetCardMedia sets or clears one of userID's own card's media hashes. hash
// of nil clears the attachment (e.g. a "remove image" request). It does not
// validate that hash names a real flash_media row — the foreign key does,
// and callers that create the row (ImportDeck, AttachCardUpload) do it in
// the same transaction as the attach, so PurgeOrphanMedia can never delete
// the row in between (#302.5).
func (st *Store) SetCardMedia(ctx context.Context, userID, deckID, cardID int64, kind string, hash *string) error {
	return setCardMedia(ctx, st.db, userID, deckID, cardID, kind, hash)
}

// setCardMedia is SetCardMedia against either the handle or a caller's open
// transaction (see dbExecutor).
func setCardMedia(ctx context.Context, exec dbExecutor, userID, deckID, cardID int64, kind string, hash *string) error {
	var column string
	switch kind {
	case MediaKindImage:
		column = "image_hash"
	case MediaKindAudio:
		column = "audio_hash"
	default:
		return fmt.Errorf("%w: %q is not a media kind I know", ErrInvalid, kind)
	}
	res, err := exec.ExecContext(ctx,
		`UPDATE flash_cards SET `+column+` = ? WHERE id = ? AND deck_id = ? AND user_id = ?`,
		hash, cardID, deckID, userID)
	if err != nil {
		return fmt.Errorf("flash: set card media: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("flash: set card media: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCard removes one of userID's own cards.
func (st *Store) DeleteCard(ctx context.Context, userID, deckID, id int64) error {
	res, err := st.db.ExecContext(ctx,
		`DELETE FROM flash_cards WHERE id = ? AND deck_id = ? AND user_id = ?`, id, deckID, userID)
	if err != nil {
		return fmt.Errorf("flash: delete card: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("flash: delete card: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanCard(row *sql.Row) (Card, error) {
	c, err := scanCardRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Card{}, ErrNotFound
	}
	return c, err
}

func scanCardRow(row rowScanner) (Card, error) {
	var (
		c         Card
		createdAt string
		imageHash sql.NullString
		audioHash sql.NullString
	)
	err := row.Scan(&c.ID, &c.DeckID, &c.UserID, &c.CardType, &c.Front, &c.Back, &c.Notes, &createdAt,
		&imageHash, &audioHash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Card{}, sql.ErrNoRows // translated by scanCard
	case err != nil:
		return Card{}, fmt.Errorf("flash: scan card: %w", err)
	}
	if c.CreatedAt, err = parseTime(createdAt); err != nil {
		return Card{}, err
	}
	if imageHash.Valid {
		c.ImageHash = &imageHash.String
	}
	if audioHash.Valid {
		c.AudioHash = &audioHash.String
	}
	return c, nil
}

// CardStatusNew and CardStatusDue are the corner labels a mini card in the
// cards grid can carry (UI overhaul spec §3).
const (
	CardStatusNew = "new" // userID has never reviewed it
	CardStatusDue = "due" // due for review at or before now
)

// CardStatuses returns the grid label for every card in one of userID's
// decks that has one; a card that is neither new nor due is absent.
func (st *Store) CardStatuses(ctx context.Context, userID, deckID int64, now time.Time) (map[int64]string, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT c.id,
		        CASE WHEN s.card_id IS NULL THEN ? WHEN s.due_at <= ? THEN ? ELSE '' END
		   FROM flash_cards c
		   LEFT JOIN flash_card_state s ON s.card_id = c.id AND s.user_id = c.user_id
		  WHERE c.deck_id = ? AND c.user_id = ?`,
		CardStatusNew, formatTime(now), CardStatusDue, deckID, userID)
	if err != nil {
		return nil, fmt.Errorf("flash: card statuses: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[int64]string{}
	for rows.Next() {
		var (
			id     int64
			status string
		)
		if err := rows.Scan(&id, &status); err != nil {
			return nil, fmt.Errorf("flash: card statuses: %w", err)
		}
		if status != "" {
			out[id] = status
		}
	}
	return out, rows.Err()
}
