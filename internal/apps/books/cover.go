package books

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// MaxCoverBytes bounds a cover, however it arrives: a book cover is a
// small picture, and an image this size is already far more than the
// pane draws.
const MaxCoverBytes = 2 << 20

// Where a cover came from (books_covers.source).
const (
	CoverFromOL  = "ol"
	CoverUpload  = "upload"
	CoverFromURL = "url"
)

// Cover is a stored cover. Version changes whenever the cover does.
type Cover struct {
	ContentType string
	Bytes       []byte
	Version     string
}

// coverVersion turns a cover's fetched_at into the short token its URL
// carries (?v=…). "" means no cover.
func coverVersion(fetchedAt string) string {
	if fetchedAt == "" {
		return ""
	}
	t, err := parseTime(fetchedAt)
	if err != nil {
		return ""
	}
	return strconv.FormatInt(t.UnixNano(), 36)
}

// SetCover stores (or replaces) one of userID's books' cover. The caller
// has already checked the bytes are a cover type and size.
func (st *Store) SetCover(ctx context.Context, userID, id int64, contentType string, data []byte, source string) error {
	switch source {
	case CoverFromOL, CoverUpload, CoverFromURL:
	default:
		return ErrInvalid
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin set cover: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO books_covers (book_id, content_type, bytes, source, fetched_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (book_id) DO UPDATE SET content_type = excluded.content_type,
			bytes = excluded.bytes, source = excluded.source, fetched_at = excluded.fetched_at`,
		id, contentType, data, source, formatTime(st.now())); err != nil {
		return fmt.Errorf("books: set cover: %w", err)
	}
	return tx.Commit()
}

// Cover returns one of userID's books' cover; ErrNotFound when the book
// has none, or isn't theirs.
func (st *Store) Cover(ctx context.Context, userID, id int64) (Cover, error) {
	var c Cover
	var fetched string
	err := st.db.QueryRowContext(ctx, `
		SELECT c.content_type, c.bytes, c.fetched_at
		  FROM books_covers c JOIN books_books b ON b.id = c.book_id
		 WHERE b.id = ? AND b.user_id = ?`, id, userID).Scan(&c.ContentType, &c.Bytes, &fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return Cover{}, ErrNotFound
	}
	if err != nil {
		return Cover{}, fmt.Errorf("books: cover: %w", err)
	}
	c.Version = coverVersion(fetched)
	return c, nil
}

// RemoveCover drops a book's cover, so it shows its spine again.
func (st *Store) RemoveCover(ctx context.Context, userID, id int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin remove cover: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM books_covers WHERE book_id = ?`, id); err != nil {
		return fmt.Errorf("books: remove cover: %w", err)
	}
	return tx.Commit()
}
