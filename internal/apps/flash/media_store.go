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

// MediaByHash loads one media record. An unknown hash is ErrNotFound.
func (st *Store) MediaByHash(ctx context.Context, hash string) (Media, error) {
	return mediaByHash(ctx, st.db, hash)
}

func mediaByHash(ctx context.Context, exec dbExecutor, hash string) (Media, error) {
	var (
		m         Media
		bytes     []byte
		sourceURL sql.NullString
		fetched   sql.NullString
	)
	err := exec.QueryRowContext(ctx,
		`SELECT hash, kind, content_type, bytes, source_url, fetched_at, error_count, last_error
		 FROM flash_media WHERE hash = ?`, hash,
	).Scan(&m.Hash, &m.Kind, &m.ContentType, &bytes, &sourceURL, &fetched, &m.ErrorCount, &m.LastError)
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
	hash := urlHash(sourceURL)
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
func (st *Store) SaveMediaUpload(ctx context.Context, kind, contentType string, data []byte, now time.Time) (string, error) {
	hash := contentHash(data)
	if _, err := st.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO flash_media (hash, kind, content_type, bytes, fetched_at) VALUES (?, ?, ?, ?, ?)`,
		hash, kind, contentType, data, formatTime(now)); err != nil {
		return "", fmt.Errorf("flash: save media upload: %w", err)
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
