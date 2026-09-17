// internal/apps/flash/tag.go
package flash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// MaxTagNameRunes bounds a tag name.
const MaxTagNameRunes = 40

// Tag is one of userID's tags. Names are unique per user, not globally, so
// two accounts can each have their own "hard" tag without colliding.
type Tag struct {
	ID     int64
	UserID int64
	Name   string
}

// normalizeTagName trims and lowercases, so "Hard", "hard ", and "hard" are
// the same tag rather than three near-duplicates cluttering the filter list.
func normalizeTagName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// ValidateTagNames checks a set of user-supplied tag names before anything
// is persisted. Exported so handlers can validate tags up front, alongside
// ValidateCard, instead of discovering a bad tag name only after the card
// row has already been written.
func ValidateTagNames(names []string) error {
	for _, raw := range names {
		name := normalizeTagName(raw)
		if name == "" {
			continue
		}
		if len([]rune(name)) > MaxTagNameRunes {
			return fmt.Errorf("%w: tag %q is longer than %d characters", ErrInvalid, name, MaxTagNameRunes)
		}
	}
	return nil
}

// SetCardTags replaces cardID's whole tag set with names, creating any tag
// that does not exist yet for userID. It fails with ErrNotFound if the card
// is not userID's own, via the same DeckByID-style ownership check as
// CreateCard: cardOwnerCheck below.
func (st *Store) SetCardTags(ctx context.Context, userID, cardID int64, names []string) error {
	if _, err := st.cardOwnerCheck(ctx, userID, cardID); err != nil {
		return err
	}
	if err := ValidateTagNames(names); err != nil {
		return err
	}

	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("flash: set card tags: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM flash_card_tags WHERE card_id = ?`, cardID); err != nil {
		return fmt.Errorf("flash: set card tags: %w", err)
	}

	if err := upsertCardTags(ctx, tx, userID, cardID, names); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("flash: set card tags: %w", err)
	}
	return nil
}

// upsertCardTags finds-or-creates each of userID's tags by name and links
// cardID to them, using tx directly rather than opening its own transaction
// — so it can run either as SetCardTags' own transaction or inside a caller's
// existing one (e.g. ImportDeck's).
func upsertCardTags(ctx context.Context, tx *sql.Tx, userID, cardID int64, names []string) error {
	seen := make(map[string]bool)
	for _, raw := range names {
		name := normalizeTagName(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		var tagID int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM flash_tags WHERE user_id = ? AND name = ?`, userID, name).Scan(&tagID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if err := tx.QueryRowContext(ctx,
				`INSERT INTO flash_tags (user_id, name) VALUES (?, ?) RETURNING id`, userID, name,
			).Scan(&tagID); err != nil {
				return fmt.Errorf("flash: upsert tag %q: %w", name, err)
			}
		case err != nil:
			return fmt.Errorf("flash: upsert tag %q: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO flash_card_tags (card_id, tag_id) VALUES (?, ?)`, cardID, tagID); err != nil {
			return fmt.Errorf("flash: link tag %q: %w", name, err)
		}
	}
	return nil
}

// cardOwnerCheck confirms cardID belongs to userID, returning its deck id.
// A thin wrapper so tag methods do not need CardByID's full column list.
func (st *Store) cardOwnerCheck(ctx context.Context, userID, cardID int64) (int64, error) {
	var deckID int64
	err := st.db.QueryRowContext(ctx,
		`SELECT deck_id FROM flash_cards WHERE id = ? AND user_id = ?`, cardID, userID).Scan(&deckID)
	if err != nil {
		return 0, ErrNotFound
	}
	return deckID, nil
}

// TagsForCard returns cardID's tags, alphabetically.
func (st *Store) TagsForCard(ctx context.Context, userID, cardID int64) ([]Tag, error) {
	if _, err := st.cardOwnerCheck(ctx, userID, cardID); err != nil {
		return nil, err
	}
	rows, err := st.db.QueryContext(ctx,
		`SELECT t.id, t.user_id, t.name
		 FROM flash_tags t
		 JOIN flash_card_tags ct ON ct.tag_id = t.id
		 WHERE ct.card_id = ?
		 ORDER BY t.name ASC`, cardID)
	if err != nil {
		return nil, fmt.Errorf("flash: tags for card: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Tag
	for rows.Next() {
		var tg Tag
		if err := rows.Scan(&tg.ID, &tg.UserID, &tg.Name); err != nil {
			return nil, fmt.Errorf("flash: tags for card: %w", err)
		}
		out = append(out, tg)
	}
	return out, rows.Err()
}

// CardsByTag returns every one of userID's cards, across every deck, that
// carries tagName. This is Flash's cross-deck filter.
func (st *Store) CardsByTag(ctx context.Context, userID int64, tagName string) ([]Card, error) {
	name := normalizeTagName(tagName)
	rows, err := st.db.QueryContext(ctx,
		`SELECT c.id, c.deck_id, c.user_id, c.card_type, c.front, c.back, c.notes, c.created_at, c.image_hash, c.audio_hash
		 FROM flash_cards c
		 JOIN flash_card_tags ct ON ct.card_id = c.id
		 JOIN flash_tags t ON t.id = ct.tag_id
		 WHERE c.user_id = ? AND t.user_id = ? AND t.name = ?
		 ORDER BY c.created_at DESC, c.id DESC`, userID, userID, name)
	if err != nil {
		return nil, fmt.Errorf("flash: cards by tag: %w", err)
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
	return out, rows.Err()
}
