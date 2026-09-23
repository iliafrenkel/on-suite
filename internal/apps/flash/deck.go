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
	// DefaultNewCardsPerDay must match migrations/0005_deck_pace_snooze.sql's
	// "new_cards_per_day INTEGER NOT NULL DEFAULT 20" — CreateDeck (below)
	// sets it explicitly on the Deck value it returns, rather than relying
	// on the DB default to also show up there: CreateDeck never re-fetches
	// the row after inserting it, so without this the returned Deck's
	// NewCardsPerDay would be Go's zero value (0) instead of 20 until the
	// next DeckByID/ListDecks call. Found by plan validation.
	DefaultNewCardsPerDay = 20
)

// DeckColors is the fixed palette a deck's owner picks from, in the order
// the swatch picker shows them. The names — not hex values — are stored:
// templates turn one into a deck-c-<name> class and app.css decides what
// that looks like in light and dark mode (UI overhaul spec §1.2).
var DeckColors = []string{"teal", "blue", "purple", "pink", "coral", "amber", "green", "gray"}

// DefaultDeckColor must match migrations/0010_deck_color.sql's column
// default, for the same reason DefaultNewCardsPerDay must match 0005's:
// CreateDeck never re-reads the row it inserts.
const DefaultDeckColor = "teal"

// ValidDeckColor reports whether c is one of DeckColors, exactly (the
// comparison is case-sensitive and does not trim).
func ValidDeckColor(c string) bool {
	for _, name := range DeckColors {
		if c == name {
			return true
		}
	}
	return false
}

// deckColumns is the column list every query feeding scanDeckRow selects,
// in scanDeckRow's own Scan order. One constant, so adding a column can't
// leave one of the three SELECTs (DeckByID, ListDecks, share.go's
// deckByIDIgnoringOwner) behind.
const deckColumns = `id, user_id, name, description, created_at, new_cards_per_day, reviews_per_day, snoozed_until, color`

// Deck is one flash-card deck.
type Deck struct {
	ID          int64
	UserID      int64
	Name        string
	Description string
	CreatedAt   time.Time

	NewCardsPerDay int
	ReviewsPerDay  *int       // nil = unlimited
	SnoozedUntil   *time.Time // nil = not snoozed
	// Color is one of DeckColors.
	Color string
}

// IsSnoozed reports whether the deck is hidden from the review queue at now.
func (d Deck) IsSnoozed(now time.Time) bool {
	return d.SnoozedUntil != nil && d.SnoozedUntil.After(now)
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

	d := Deck{UserID: userID, Name: name, Description: description, CreatedAt: st.now(), NewCardsPerDay: DefaultNewCardsPerDay, Color: DefaultDeckColor}
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
		`SELECT `+deckColumns+` FROM flash_decks WHERE id = ? AND user_id = ?`, id, userID))
}

// ListDecks returns userID's decks, newest first.
func (st *Store) ListDecks(ctx context.Context, userID int64) ([]Deck, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+deckColumns+` FROM flash_decks WHERE user_id = ?
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
		d             Deck
		createdAt     string
		reviewsPerDay sql.NullInt64
		snoozedUntil  sql.NullString
	)
	err := row.Scan(&d.ID, &d.UserID, &d.Name, &d.Description, &createdAt,
		&d.NewCardsPerDay, &reviewsPerDay, &snoozedUntil, &d.Color)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Deck{}, sql.ErrNoRows // translated by scanDeck
	case err != nil:
		return Deck{}, fmt.Errorf("flash: scan deck: %w", err)
	}
	if d.CreatedAt, err = parseTime(createdAt); err != nil {
		return Deck{}, err
	}
	if reviewsPerDay.Valid {
		n := int(reviewsPerDay.Int64)
		d.ReviewsPerDay = &n
	}
	if snoozedUntil.Valid {
		t, err := parseTime(snoozedUntil.String)
		if err != nil {
			return Deck{}, err
		}
		d.SnoozedUntil = &t
	}
	return d, nil
}

// ValidateDeckSettings checks a deck's pace fields.
func ValidateDeckSettings(newCardsPerDay int, reviewsPerDay *int) error {
	if newCardsPerDay < 0 {
		return fmt.Errorf("%w: new cards per day cannot be negative", ErrInvalid)
	}
	if reviewsPerDay != nil && *reviewsPerDay < 0 {
		return fmt.Errorf("%w: reviews per day cannot be negative", ErrInvalid)
	}
	return nil
}

// UpdateDeckSettings overwrites userID's own deck's pace. reviewsPerDay of
// nil means unlimited.
func (st *Store) UpdateDeckSettings(ctx context.Context, userID, id int64, newCardsPerDay int, reviewsPerDay *int) (Deck, error) {
	if err := ValidateDeckSettings(newCardsPerDay, reviewsPerDay); err != nil {
		return Deck{}, err
	}
	var reviewsArg any
	if reviewsPerDay != nil {
		reviewsArg = *reviewsPerDay
	}
	res, err := st.db.ExecContext(ctx,
		`UPDATE flash_decks SET new_cards_per_day = ?, reviews_per_day = ? WHERE id = ? AND user_id = ?`,
		newCardsPerDay, reviewsArg, id, userID)
	if err != nil {
		return Deck{}, fmt.Errorf("flash: update deck settings: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Deck{}, fmt.Errorf("flash: update deck settings: %w", err)
	}
	if n == 0 {
		return Deck{}, ErrNotFound
	}
	return st.DeckByID(ctx, userID, id)
}

// SnoozeDeck hides userID's own deck from the review queue until until.
func (st *Store) SnoozeDeck(ctx context.Context, userID, id int64, until time.Time) (Deck, error) {
	res, err := st.db.ExecContext(ctx,
		`UPDATE flash_decks SET snoozed_until = ? WHERE id = ? AND user_id = ?`,
		formatTime(until), id, userID)
	if err != nil {
		return Deck{}, fmt.Errorf("flash: snooze deck: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Deck{}, fmt.Errorf("flash: snooze deck: %w", err)
	}
	if n == 0 {
		return Deck{}, ErrNotFound
	}
	return st.DeckByID(ctx, userID, id)
}

// UnsnoozeDeck clears userID's own deck's snooze immediately.
func (st *Store) UnsnoozeDeck(ctx context.Context, userID, id int64) (Deck, error) {
	res, err := st.db.ExecContext(ctx,
		`UPDATE flash_decks SET snoozed_until = NULL WHERE id = ? AND user_id = ?`,
		id, userID)
	if err != nil {
		return Deck{}, fmt.Errorf("flash: unsnooze deck: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Deck{}, fmt.Errorf("flash: unsnooze deck: %w", err)
	}
	if n == 0 {
		return Deck{}, ErrNotFound
	}
	return st.DeckByID(ctx, userID, id)
}

// SetDeckColor changes userID's own deck's colour.
func (st *Store) SetDeckColor(ctx context.Context, userID, id int64, color string) (Deck, error) {
	if !ValidDeckColor(color) {
		return Deck{}, fmt.Errorf("%w: %q is not a deck colour", ErrInvalid, color)
	}
	res, err := st.db.ExecContext(ctx,
		`UPDATE flash_decks SET color = ? WHERE id = ? AND user_id = ?`, color, id, userID)
	if err != nil {
		return Deck{}, fmt.Errorf("flash: set deck colour: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Deck{}, fmt.Errorf("flash: set deck colour: %w", err)
	}
	if n == 0 {
		return Deck{}, ErrNotFound
	}
	return st.DeckByID(ctx, userID, id)
}
