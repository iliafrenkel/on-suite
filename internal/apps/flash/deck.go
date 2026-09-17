// internal/apps/flash/deck.go
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
	// MaxDeckNameRunes bounds a deck name so the list page stays readable.
	MaxDeckNameRunes = 120
	// MaxDeckDescriptionRunes bounds the optional description.
	MaxDeckDescriptionRunes = 500
)

// Deck is one flash-card deck.
type Deck struct {
	ID          int64
	UserID      int64
	Name        string
	Description string
	CreatedAt   time.Time
}

// ValidateDeck checks a deck's user-supplied fields. Exported because the
// handler reports these messages back to the user.
func ValidateDeck(name, description string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: the deck needs a name", ErrInvalid)
	}
	if utf8.RuneCountInString(name) > MaxDeckNameRunes {
		return fmt.Errorf("%w: the name is longer than %d characters", ErrInvalid, MaxDeckNameRunes)
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("%w: the name is not valid UTF-8", ErrInvalid)
	}
	if utf8.RuneCountInString(description) > MaxDeckDescriptionRunes {
		return fmt.Errorf("%w: the description is longer than %d characters", ErrInvalid, MaxDeckDescriptionRunes)
	}
	if !utf8.ValidString(description) {
		return fmt.Errorf("%w: the description is not valid UTF-8", ErrInvalid)
	}
	return nil
}

// CreateDeck stores a new deck.
func (st *Store) CreateDeck(ctx context.Context, userID int64, name, description string) (Deck, error) {
	name = strings.TrimSpace(name)
	if err := ValidateDeck(name, description); err != nil {
		return Deck{}, err
	}

	d := Deck{UserID: userID, Name: name, Description: description, CreatedAt: st.now()}
	err := st.db.QueryRowContext(ctx,
		`INSERT INTO flash_decks (user_id, name, description, created_at)
		 VALUES (?, ?, ?, ?)
		 RETURNING id`,
		d.UserID, d.Name, d.Description, formatTime(d.CreatedAt),
	).Scan(&d.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return Deck{}, fmt.Errorf("%w: you already have a deck named %q", ErrInvalid, name)
		}
		return Deck{}, fmt.Errorf("flash: create deck: %w", err)
	}
	return d, nil
}

// UpdateDeck overwrites userID's own deck's editable fields.
func (st *Store) UpdateDeck(ctx context.Context, userID, id int64, name, description string) (Deck, error) {
	name = strings.TrimSpace(name)
	if err := ValidateDeck(name, description); err != nil {
		return Deck{}, err
	}

	res, err := st.db.ExecContext(ctx,
		`UPDATE flash_decks SET name = ?, description = ? WHERE id = ? AND user_id = ?`,
		name, description, id, userID)
	if err != nil {
		if isUniqueViolation(err) {
			return Deck{}, fmt.Errorf("%w: you already have a deck named %q", ErrInvalid, name)
		}
		return Deck{}, fmt.Errorf("flash: update deck: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Deck{}, fmt.Errorf("flash: update deck: %w", err)
	}
	if n == 0 {
		return Deck{}, ErrNotFound
	}
	return st.DeckByID(ctx, userID, id)
}

// DeckByID fetches one of userID's own decks.
func (st *Store) DeckByID(ctx context.Context, userID, id int64) (Deck, error) {
	return scanDeck(st.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, description, created_at
		 FROM flash_decks WHERE id = ? AND user_id = ?`, id, userID))
}

// ListDecks returns userID's decks, newest first.
func (st *Store) ListDecks(ctx context.Context, userID int64) ([]Deck, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT id, user_id, name, description, created_at
		 FROM flash_decks WHERE user_id = ?
		 ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("flash: list decks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Deck
	for rows.Next() {
		d, err := scanDeckRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flash: list decks: %w", err)
	}
	return out, nil
}

// DeleteDeck removes one of userID's own decks. Its cards go with it via
// ON DELETE CASCADE once Task 3 adds flash_cards.
func (st *Store) DeleteDeck(ctx context.Context, userID, id int64) error {
	res, err := st.db.ExecContext(ctx,
		`DELETE FROM flash_decks WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("flash: delete deck: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("flash: delete deck: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanDeck(row *sql.Row) (Deck, error) {
	d, err := scanDeckRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Deck{}, ErrNotFound
	}
	return d, err
}

func scanDeckRow(row rowScanner) (Deck, error) {
	var (
		d         Deck
		createdAt string
	)
	err := row.Scan(&d.ID, &d.UserID, &d.Name, &d.Description, &createdAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Deck{}, sql.ErrNoRows // translated by scanDeck
	case err != nil:
		return Deck{}, fmt.Errorf("flash: scan deck: %w", err)
	}
	if d.CreatedAt, err = parseTime(createdAt); err != nil {
		return Deck{}, err
	}
	return d, nil
}
