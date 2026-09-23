// internal/apps/flash/import_store.go
package flash

import (
	"context"
	"fmt"
)

// ImportDeck creates a new deck for userID and populates it with cards, all
// inside one transaction: if any insert fails, nothing is left behind. cards
// must already be validated (see ParseImport) — ImportDeck only persists,
// it does not re-validate field contents.
func (st *Store) ImportDeck(ctx context.Context, userID int64, name, description string, cards []parsedCard) (Deck, error) {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return Deck{}, fmt.Errorf("flash: import deck: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	d := Deck{UserID: userID, Name: name, Description: description, CreatedAt: st.now(), NewCardsPerDay: DefaultNewCardsPerDay, Color: DefaultDeckColor}
	err = tx.QueryRowContext(ctx,
		`INSERT INTO flash_decks (user_id, name, description, created_at)
		 VALUES (?, ?, ?, ?)
		 RETURNING id`,
		d.UserID, d.Name, d.Description, formatTime(d.CreatedAt),
	).Scan(&d.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return Deck{}, fmt.Errorf("%w: you already have a deck named %q", ErrInvalid, name)
		}
		return Deck{}, fmt.Errorf("flash: import deck: %w", err)
	}

	for _, c := range cards {
		// A card's image/audio URL is only recorded here, never fetched: an
		// actual network fetch must not run inside this transaction (flash's
		// SQLite handle allows only one connection at a time, so a slow
		// outbound call here would block every other request in the app for
		// its duration). ensureMediaURL just hashes the URL and inserts a
		// metadata-only row if one doesn't already exist; the real fetch
		// happens lazily, the first time the media is requested (see
		// media_fetch.go, handlers_media.go).
		var imageHash, audioHash *string
		if c.ImageURL != "" {
			h, err := ensureMediaURL(ctx, tx, MediaKindImage, c.ImageURL)
			if err != nil {
				return Deck{}, err
			}
			imageHash = &h
		}
		if c.AudioURL != "" {
			h, err := ensureMediaURL(ctx, tx, MediaKindAudio, c.AudioURL)
			if err != nil {
				return Deck{}, err
			}
			audioHash = &h
		}

		var cardID int64
		err = tx.QueryRowContext(ctx,
			`INSERT INTO flash_cards (deck_id, user_id, card_type, front, back, notes, created_at, image_hash, audio_hash)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			 RETURNING id`,
			d.ID, userID, c.CardType, c.Front, c.Back, c.Notes, formatTime(st.now()), imageHash, audioHash,
		).Scan(&cardID)
		if err != nil {
			return Deck{}, fmt.Errorf("flash: import deck: %w", err)
		}
		// upsertCardTags (tag.go) is SetCardTags' insert logic, run against
		// the transaction ImportDeck already holds. SetCardTags cannot be
		// called directly here: it opens (and commits) its own transaction
		// via st.db, and flash's SQLite handle is opened with a single
		// connection (internal/platform/db.Open sets MaxOpenConns(1)), so a
		// second BeginTx from inside an already-open transaction would
		// deadlock waiting for a connection the first transaction is still
		// holding.
		if err := upsertCardTags(ctx, tx, userID, cardID, c.Tags); err != nil {
			return Deck{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return Deck{}, fmt.Errorf("flash: import deck: %w", err)
	}
	return d, nil
}
