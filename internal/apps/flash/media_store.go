// internal/apps/flash/media_store.go
package flash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Media is one cached image or audio clip.
type Media struct {
	Hash        string
	Kind        string
	ContentType string
	Bytes       []byte
	SourceURL   string
	FetchedAt   time.Time
	ErrorCount  int
	LastError   string
}

// Cached reports whether the bytes are in hand. A URL-attached row exists
// from the moment something references its source URL; the bytes arrive on
// first view. An uploaded row is Cached from the moment it's created.
func (m Media) Cached() bool { return len(m.Bytes) > 0 }

// dbExecutor is satisfied by both *sql.DB and *sql.Tx, so the ensure/upsert
// helpers below can run either as their own statement or inside a caller's
// already-open transaction (e.g. ImportDeck's) — the same reasoning tag.go's
// upsertCardTags documents for the same SetMaxOpenConns(1) constraint.
type dbExecutor interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// mediaColumns is every flash_media column a Media carries, in scanMedia's
// order.
const mediaColumns = `hash, kind, content_type, bytes, source_url, fetched_at, error_count, last_error`

// MediaByHash loads one media record, whoever's it is. An unknown hash is
// ErrNotFound. The media route uses MediaForUser instead; this is for
// tests and internal checks that aren't answering a viewer.
func (st *Store) MediaByHash(ctx context.Context, hash string) (Media, error) {
	return mediaByHash(ctx, st.db, hash)
}

func mediaByHash(ctx context.Context, exec dbExecutor, hash string) (Media, error) {
	return scanMedia(exec.QueryRowContext(ctx,
		`SELECT `+mediaColumns+` FROM flash_media WHERE hash = ?`, hash))
}

// MediaForUser loads one media record, but only if one of userID's own
// cards uses it as its image or its sound. Anything else — a hash no card
// of theirs uses, or no such hash at all — is the same ErrNotFound, so the
// media route can't tell anyone what another account has attached
// (#302.4). A deck adopted from someone else counts: AdoptShare copies the
// sharer's hashes into the recipient's own cards.
func (st *Store) MediaForUser(ctx context.Context, userID int64, hash string) (Media, error) {
	return scanMedia(st.db.QueryRowContext(ctx,
		`SELECT `+mediaColumns+` FROM flash_media
		  WHERE hash = ?
		    AND EXISTS (SELECT 1 FROM flash_cards c
		                 WHERE c.user_id = ?
		                   AND (c.image_hash = flash_media.hash OR c.audio_hash = flash_media.hash))`,
		hash, userID))
}

func scanMedia(row rowScanner) (Media, error) {
	var (
		m         Media
		bytes     []byte
		sourceURL sql.NullString
		fetched   sql.NullString
	)
	err := row.Scan(&m.Hash, &m.Kind, &m.ContentType, &bytes, &sourceURL, &fetched, &m.ErrorCount, &m.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return Media{}, ErrNotFound
	}
	if err != nil {
		return Media{}, fmt.Errorf("flash: load media: %w", err)
	}
	m.Bytes = bytes
	if sourceURL.Valid {
		m.SourceURL = sourceURL.String
	}
	if fetched.Valid {
		if m.FetchedAt, err = parseTime(fetched.String); err != nil {
			return Media{}, err
		}
	}
	return m, nil
}

// EnsureMediaURL records that something references a URL-attached media
// item, without fetching it, and returns the hash it should be referenced
// by. Safe to call repeatedly for the same URL: a second call is a no-op
// (INSERT OR IGNORE), so multiple cards referencing the same URL share one
// row.
func (st *Store) EnsureMediaURL(ctx context.Context, kind, sourceURL string) (string, error) {
	return ensureMediaURL(ctx, st.db, kind, sourceURL)
}

func ensureMediaURL(ctx context.Context, exec dbExecutor, kind, sourceURL string) (string, error) {
	hash := urlHash(kind, sourceURL)
	if _, err := exec.ExecContext(ctx,
		`INSERT OR IGNORE INTO flash_media (hash, kind, source_url) VALUES (?, ?, ?)`,
		hash, kind, sourceURL); err != nil {
		return "", fmt.Errorf("flash: ensure media: %w", err)
	}
	return hash, nil
}

// SaveMediaUpload stores an uploaded file's bytes immediately, keyed by the
// content's own hash, and returns that hash. Safe to call repeatedly for
// identical bytes: a second call is a no-op (INSERT OR IGNORE), so
// uploading the same file twice reuses one row.
//
// The row it leaves is attached to nothing, so the next PurgeOrphanMedia
// may delete it. The card form uses AttachCardUpload, which stores and
// attaches in one transaction. This is kept for tests that seed media.
func (st *Store) SaveMediaUpload(ctx context.Context, kind, contentType string, data []byte, now time.Time) (string, error) {
	return saveMediaUpload(ctx, st.db, kind, contentType, data, now)
}

func saveMediaUpload(ctx context.Context, exec dbExecutor, kind, contentType string, data []byte, now time.Time) (string, error) {
	hash := contentHash(data)
	if _, err := exec.ExecContext(ctx,
		`INSERT OR IGNORE INTO flash_media (hash, kind, content_type, bytes, fetched_at) VALUES (?, ?, ?, ?, ?)`,
		hash, kind, contentType, data, formatTime(now)); err != nil {
		return "", fmt.Errorf("flash: save media upload: %w", err)
	}
	return hash, nil
}

// AttachCardUpload stores an uploaded file and points one of userID's own
// card's image or audio column at it, in one transaction, and returns the
// file's hash.
//
// The single transaction is what keeps PurgeOrphanMedia safe (#302.5): as
// two statements, a purge landing between them deleted the just-stored
// row — or an orphan with the same bytes, which INSERT OR IGNORE had left
// in place — and the attach then failed its foreign key. SQLite runs one
// write transaction at a time (and flash has one connection), so the purge
// now runs wholly before this, where the INSERT re-creates anything it
// deleted, or wholly after, when the card already uses the row.
//
// kind must be MediaKindImage or MediaKindAudio (else ErrInvalid); a card
// that isn't userID's is ErrNotFound. Either way nothing is stored.
func (st *Store) AttachCardUpload(ctx context.Context, userID, deckID, cardID int64, kind, contentType string, data []byte) (string, error) {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("flash: attach upload: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	hash, err := saveMediaUpload(ctx, tx, kind, contentType, data, st.now())
	if err != nil {
		return "", err
	}
	if err := setCardMedia(ctx, tx, userID, deckID, cardID, kind, &hash); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("flash: attach upload: %w", err)
	}
	return hash, nil
}

// SaveMediaBytes caches a fetched media item and clears any recorded
// failure.
func (st *Store) SaveMediaBytes(ctx context.Context, hash, contentType string, data []byte, now time.Time) error {
	if _, err := st.db.ExecContext(ctx,
		`UPDATE flash_media SET bytes = ?, content_type = ?, fetched_at = ?, last_error = '', error_count = 0
		 WHERE hash = ?`,
		data, contentType, formatTime(now), hash); err != nil {
		return fmt.Errorf("flash: cache media: %w", err)
	}
	return nil
}

// SaveMediaFailure records that a fetch failed, so a dead URL is not
// re-fetched on every view.
func (st *Store) SaveMediaFailure(ctx context.Context, hash, msg string, now time.Time) error {
	if _, err := st.db.ExecContext(ctx,
		`UPDATE flash_media SET last_error = ?, error_count = error_count + 1, fetched_at = ?
		 WHERE hash = ?`,
		msg, formatTime(now), hash); err != nil {
		return fmt.Errorf("flash: record media failure: %w", err)
	}
	return nil
}

// PurgeOrphanMedia deletes cached images and sounds that no card uses any
// more: what is left behind by a replaced or removed attachment, a deleted
// card or deck, or a deleted account. It is one statement, mirroring
// internal/apps/reader's own PurgeOrphanImages (apps never import each
// other, so this is an independent implementation). A row shared by
// reference with an adopted copy stays as long as any card, anyone's, uses
// it (#302.5).
//
// It needs no grace period for a file that is still being attached: every
// path that creates a row attaches it in the same transaction
// (AttachCardUpload for the card form, ImportDeck for import-time URLs),
// and AdoptShare creates none — it copies hashes from cards that exist, and
// so are in use, inside its own transaction. EnsureMediaURL and
// SaveMediaUpload store without attaching; only tests call them.
//
// SQLite does not give the space back to the filesystem on DELETE: the
// freed pages go on the database's freelist and later writes reuse them.
// Like Reader, this runs no VACUUM; snapshots are compact anyway, because
// they are taken with VACUUM INTO (internal/platform/db).
func (st *Store) PurgeOrphanMedia(ctx context.Context) (int, error) {
	res, err := st.db.ExecContext(ctx, `
		DELETE FROM flash_media
		 WHERE NOT EXISTS (SELECT 1 FROM flash_cards c
		                    WHERE c.image_hash = flash_media.hash
		                       OR c.audio_hash = flash_media.hash)`)
	if err != nil {
		return 0, fmt.Errorf("flash: purge orphan media: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("flash: purge orphan media: %w", err)
	}
	return int(n), nil
}
