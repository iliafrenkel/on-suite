package later

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// This file, highlight_render.go and static/highlight.js are the highlight
// unit: highlights on a document's plain text, knowing nothing about
// articles beyond "a document ID and its text", so the unit can move to the
// platform when ON Books needs it (spec: "The highlight unit"). Nothing here
// checks ownership: callers load the document owner-scoped first.

// Highlight is a span of a document's text, with an optional comment.
type Highlight struct {
	ID, DocID int64
	// Start and End are code-point offsets into the document text; End is
	// exclusive (spec: "Offsets are code points").
	Start, End           int
	Quote, Comment       string
	CreatedAt, UpdatedAt time.Time
}

// ErrOverlap is a new highlight that shares text with a stored one.
var ErrOverlap = errors.New("later: highlight overlaps another")

// ValidSpan reports whether start..end is a non-blank range of text whose
// code points are exactly quote.
func ValidSpan(text string, start, end int, quote string) bool {
	return validSpan([]rune(text), start, end, quote)
}

func validSpan(text []rune, start, end int, quote string) bool {
	if start < 0 || end <= start || end > len(text) || strings.TrimSpace(quote) == "" {
		return false
	}
	return string(text[start:end]) == quote
}

// cleanText is how comments (and Later's article note) are stored: trimmed,
// with browser CRLFs as LFs. A textarea drops one leading newline, so
// trimming also keeps an edit round trip exact.
func cleanText(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}

const highlightColumns = `id, article_id, start_offset, end_offset, quote, comment, created_at, updated_at`

func scanHighlight(row rowScanner) (Highlight, error) {
	var h Highlight
	var created, updated string
	if err := row.Scan(&h.ID, &h.DocID, &h.Start, &h.End, &h.Quote, &h.Comment, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Highlight{}, ErrNotFound
		}
		return Highlight{}, fmt.Errorf("later: load highlight: %w", err)
	}
	var err error
	if h.CreatedAt, err = db.ParseTime(created); err != nil {
		return Highlight{}, err
	}
	if h.UpdatedAt, err = db.ParseTime(updated); err != nil {
		return Highlight{}, err
	}
	return h, nil
}

// AddHighlight stores a highlight on document docID, whose text is text.
func (st *Store) AddHighlight(ctx context.Context, docID int64, text string, start, end int, quote, comment string) (Highlight, error) {
	if !ValidSpan(text, start, end, quote) {
		return Highlight{}, ErrInvalid
	}
	now := st.now()
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return Highlight{}, fmt.Errorf("later: begin highlight: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var clash bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM later_highlights
		                WHERE article_id = ? AND start_offset < ? AND ? < end_offset)`,
		docID, end, start).Scan(&clash); err != nil {
		return Highlight{}, fmt.Errorf("later: check overlap: %w", err)
	}
	if clash {
		return Highlight{}, ErrOverlap
	}
	h := Highlight{DocID: docID, Start: start, End: end, Quote: quote, Comment: cleanText(comment), CreatedAt: now, UpdatedAt: now}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO later_highlights (article_id, start_offset, end_offset, quote, comment, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		docID, start, end, quote, h.Comment, db.FormatTime(now), db.FormatTime(now))
	if err != nil {
		return Highlight{}, fmt.Errorf("later: save highlight: %w", err)
	}
	if h.ID, err = res.LastInsertId(); err != nil {
		return Highlight{}, fmt.Errorf("later: save highlight id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Highlight{}, fmt.Errorf("later: commit highlight: %w", err)
	}
	return h, nil
}

// Highlights lists document docID's highlights in text order.
func (st *Store) Highlights(ctx context.Context, docID int64) ([]Highlight, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+highlightColumns+` FROM later_highlights WHERE article_id = ? ORDER BY start_offset`, docID)
	if err != nil {
		return nil, fmt.Errorf("later: list highlights: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Highlight
	for rows.Next() {
		h, err := scanHighlight(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("later: list highlights: %w", err)
	}
	return out, nil
}

// SetHighlightComment replaces a highlight's comment; blank removes it.
func (st *Store) SetHighlightComment(ctx context.Context, docID, id int64, comment string) error {
	return st.exec(ctx, "highlight comment", `
		UPDATE later_highlights SET comment = ?, updated_at = ? WHERE id = ? AND article_id = ?`,
		cleanText(comment), db.FormatTime(st.now()), id, docID)
}

// DeleteHighlight removes one highlight of document docID.
func (st *Store) DeleteHighlight(ctx context.Context, docID, id int64) error {
	return st.exec(ctx, "delete highlight",
		`DELETE FROM later_highlights WHERE id = ? AND article_id = ?`, id, docID)
}
