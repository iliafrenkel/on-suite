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

// CreateCard stores a new card in one of userID's own decks.
func (st *Store) CreateCard(ctx context.Context, userID, deckID int64, cardType, front, back, notes string) (Card, error) {
	if err := ValidateCard(cardType, front, back); err != nil {
		return Card{}, err
	}
	if err := validateCardNotes(notes); err != nil {
		return Card{}, err
	}
	if _, err := st.DeckByID(ctx, userID, deckID); err != nil {
		return Card{}, err
	}

	c := Card{DeckID: deckID, UserID: userID, CardType: cardType, Front: front, Back: back, Notes: notes, CreatedAt: st.now()}
	err := st.db.QueryRowContext(ctx,
		`INSERT INTO flash_cards (deck_id, user_id, card_type, front, back, notes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 RETURNING id`,
		c.DeckID, c.UserID, c.CardType, c.Front, c.Back, c.Notes, formatTime(c.CreatedAt),
	).Scan(&c.ID)
	if err != nil {
		return Card{}, fmt.Errorf("flash: create card: %w", err)
	}
	return c, nil
}

// UpdateCard overwrites userID's own card's editable fields.
func (st *Store) UpdateCard(ctx context.Context, userID, deckID, id int64, cardType, front, back, notes string) (Card, error) {
	if err := ValidateCard(cardType, front, back); err != nil {
		return Card{}, err
	}
	if err := validateCardNotes(notes); err != nil {
		return Card{}, err
	}

	res, err := st.db.ExecContext(ctx,
		`UPDATE flash_cards SET card_type = ?, front = ?, back = ?, notes = ?
		 WHERE id = ? AND deck_id = ? AND user_id = ?`,
		cardType, front, back, notes, id, deckID, userID)
	if err != nil {
		return Card{}, fmt.Errorf("flash: update card: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Card{}, fmt.Errorf("flash: update card: %w", err)
	}
	if n == 0 {
		return Card{}, ErrNotFound
	}
	return st.CardByID(ctx, userID, deckID, id)
}

// CardByID fetches one of userID's own cards, scoped to its deck.
func (st *Store) CardByID(ctx context.Context, userID, deckID, id int64) (Card, error) {
	return scanCard(st.db.QueryRowContext(ctx,
		`SELECT id, deck_id, user_id, card_type, front, back, notes, created_at
		 FROM flash_cards WHERE id = ? AND deck_id = ? AND user_id = ?`, id, deckID, userID))
}

// ListCards returns userID's cards in one deck, newest first.
func (st *Store) ListCards(ctx context.Context, userID, deckID int64) ([]Card, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT id, deck_id, user_id, card_type, front, back, notes, created_at
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
	)
	err := row.Scan(&c.ID, &c.DeckID, &c.UserID, &c.CardType, &c.Front, &c.Back, &c.Notes, &createdAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Card{}, sql.ErrNoRows // translated by scanCard
	case err != nil:
		return Card{}, fmt.Errorf("flash: scan card: %w", err)
	}
	if c.CreatedAt, err = parseTime(createdAt); err != nil {
		return Card{}, err
	}
	return c, nil
}
