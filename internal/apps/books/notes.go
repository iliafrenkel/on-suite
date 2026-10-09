package books

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxNoteRunes bounds a note; MaxQuoteRunes bounds a quote's text and its
// comment, each. Room for long ones, as MaxReviewRunes is for a review.
const (
	MaxNoteRunes  = 20000
	MaxQuoteRunes = 5000
)

// Note is a dated note on a book (spec "Data model": books_notes).
type Note struct {
	ID                   int64
	Page                 int    // 0 when not given
	Body                 string // Markdown
	CreatedAt, UpdatedAt time.Time
}

// NoteInput is a note as typed. Page 0 is no page; below 0 is a page the
// store refuses (the handler sends -1 for anything that isn't a positive
// whole number).
type NoteInput struct {
	Page int
	Body string
}

// cleanText tidies line endings and trims the ends, as SetReview does.
// Line breaks inside are kept.
func cleanText(s string) string { return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")) }

// checkLength refuses text over max runes, naming it ("your note").
func checkLength(s string, max int, what string) error {
	if utf8.RuneCountInString(s) > max {
		return &Refusal{Msg: fmt.Sprintf("Keep %s to %d characters or fewer.", what, max)}
	}
	return nil
}

// checkPage accepts page 0 (none) or a page of the book: 1 to its page
// count, or any positive page when it has none (decided 2026-10-09).
func checkPage(ctx context.Context, tx *sql.Tx, bookID int64, page int) error {
	if page == 0 {
		return nil
	}
	var pages sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT pages FROM books_books WHERE id = ?`, bookID).Scan(&pages); err != nil {
		return fmt.Errorf("books: check page: %w", err)
	}
	switch {
	case pages.Int64 > 0 && (page < 1 || page > int(pages.Int64)):
		return &Refusal{Msg: fmt.Sprintf("Enter a page from 1 to %d.", pages.Int64)}
	case page < 1:
		return &Refusal{Msg: "Enter a page number of 1 or more."}
	}
	return nil
}

// inBook runs write in one transaction after the owner check (touch, which
// also marks the book changed) and the page check, so a note or quote
// never lands on someone else's book or on a page it doesn't have. now is
// the write's timestamp.
func (st *Store) inBook(ctx context.Context, userID, bookID int64, page int, write func(tx *sql.Tx, now string) error) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, bookID); err != nil {
		return err
	}
	if err := checkPage(ctx, tx, bookID, page); err != nil {
		return err
	}
	if err := write(tx, formatTime(st.now())); err != nil {
		return err
	}
	return tx.Commit()
}

// execOne runs a statement that must change exactly one row: none is
// ErrNotFound — a note or quote that isn't on this book.
func execOne(ctx context.Context, tx *sql.Tx, query string, args ...any) error {
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("books: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("books: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// parseStamps parses a row's created_at and updated_at.
func parseStamps(created, updated string) (time.Time, time.Time, error) {
	c, err := parseTime(created)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("created_at: %w", err)
	}
	u, err := parseTime(updated)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("updated_at: %w", err)
	}
	return c, u, nil
}

// checkNote cleans a note and refuses an empty or over-long one.
func checkNote(in NoteInput) (NoteInput, error) {
	in.Body = cleanText(in.Body)
	if in.Body == "" {
		return in, &Refusal{Msg: "Write something in the note first."}
	}
	return in, checkLength(in.Body, MaxNoteRunes, "your note")
}

// AddNote adds a note to one of userID's books, dated now.
func (st *Store) AddNote(ctx context.Context, userID, bookID int64, in NoteInput) (int64, error) {
	in, err := checkNote(in)
	if err != nil {
		return 0, err
	}
	var id int64
	err = st.inBook(ctx, userID, bookID, in.Page, func(tx *sql.Tx, now string) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO books_notes (book_id, page, body, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			bookID, nullInt(in.Page), in.Body, now, now)
		if err != nil {
			return fmt.Errorf("books: add note: %w", err)
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

// UpdateNote changes a note's page and text; it keeps its date.
func (st *Store) UpdateNote(ctx context.Context, userID, bookID, noteID int64, in NoteInput) error {
	in, err := checkNote(in)
	if err != nil {
		return err
	}
	return st.inBook(ctx, userID, bookID, in.Page, func(tx *sql.Tx, now string) error {
		return execOne(ctx, tx, `UPDATE books_notes SET page = ?, body = ?, updated_at = ? WHERE id = ? AND book_id = ?`,
			nullInt(in.Page), in.Body, now, noteID, bookID)
	})
}

// DeleteNote removes a note for good.
func (st *Store) DeleteNote(ctx context.Context, userID, bookID, noteID int64) error {
	return st.inBook(ctx, userID, bookID, 0, func(tx *sql.Tx, _ string) error {
		return execOne(ctx, tx, `DELETE FROM books_notes WHERE id = ? AND book_id = ?`, noteID, bookID)
	})
}

// Notes is a book's notes, newest first (spec "Book pane"). A book that
// isn't userID's has none.
func (st *Store) Notes(ctx context.Context, userID, bookID int64) ([]Note, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT n.id, n.page, n.body, n.created_at, n.updated_at
		  FROM books_notes n JOIN books_books b ON b.id = n.book_id
		 WHERE b.id = ? AND b.user_id = ?
		 ORDER BY n.created_at DESC, n.id DESC`, bookID, userID)
	if err != nil {
		return nil, fmt.Errorf("books: notes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Note
	for rows.Next() {
		var n Note
		var page sql.NullInt64
		var created, updated string
		if err := rows.Scan(&n.ID, &page, &n.Body, &created, &updated); err != nil {
			return nil, fmt.Errorf("books: scan note: %w", err)
		}
		n.Page = int(page.Int64)
		if n.CreatedAt, n.UpdatedAt, err = parseStamps(created, updated); err != nil {
			return nil, fmt.Errorf("books: note %d: %w", n.ID, err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: notes: %w", err)
	}
	return out, nil
}
