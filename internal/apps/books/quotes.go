package books

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Quote is a passage copied out of a book (spec "Data model":
// books_quotes): plain text, its line breaks kept, with an optional page
// and an optional Markdown comment.
type Quote struct {
	ID                   int64
	Page                 int // 0 when not given
	Text                 string
	Comment              string
	CreatedAt, UpdatedAt time.Time
}

// QuoteInput is a quote as typed; Page as in NoteInput.
type QuoteInput struct {
	Page          int
	Text, Comment string
}

// checkQuote cleans a quote and refuses one with no text, or with text or
// a comment over MaxQuoteRunes.
func checkQuote(in QuoteInput) (QuoteInput, error) {
	in.Text, in.Comment = cleanText(in.Text), cleanText(in.Comment)
	if in.Text == "" {
		return in, &Refusal{Msg: "Type the quote first."}
	}
	if err := checkLength(in.Text, MaxQuoteRunes, "the quote"); err != nil {
		return in, err
	}
	return in, checkLength(in.Comment, MaxQuoteRunes, "your comment")
}

// AddQuote adds a quote to one of userID's books.
func (st *Store) AddQuote(ctx context.Context, userID, bookID int64, in QuoteInput) (int64, error) {
	in, err := checkQuote(in)
	if err != nil {
		return 0, err
	}
	var id int64
	err = st.inBook(ctx, userID, bookID, in.Page, func(tx *sql.Tx, now string) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO books_quotes (book_id, page, text, comment, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			bookID, nullInt(in.Page), in.Text, in.Comment, now, now)
		if err != nil {
			return fmt.Errorf("books: add quote: %w", err)
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

// UpdateQuote changes a quote's page, text and comment.
func (st *Store) UpdateQuote(ctx context.Context, userID, bookID, quoteID int64, in QuoteInput) error {
	in, err := checkQuote(in)
	if err != nil {
		return err
	}
	return st.inBook(ctx, userID, bookID, in.Page, func(tx *sql.Tx, now string) error {
		return execOne(ctx, tx, `UPDATE books_quotes SET page = ?, text = ?, comment = ?, updated_at = ? WHERE id = ? AND book_id = ?`,
			nullInt(in.Page), in.Text, in.Comment, now, quoteID, bookID)
	})
}

// DeleteQuote removes a quote for good.
func (st *Store) DeleteQuote(ctx context.Context, userID, bookID, quoteID int64) error {
	return st.inBook(ctx, userID, bookID, 0, func(tx *sql.Tx, _ string) error {
		return execOne(ctx, tx, `DELETE FROM books_quotes WHERE id = ? AND book_id = ?`, quoteID, bookID)
	})
}

// Quotes is a book's quotes, newest first. A book that isn't userID's has
// none.
func (st *Store) Quotes(ctx context.Context, userID, bookID int64) ([]Quote, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT q.id, q.page, q.text, q.comment, q.created_at, q.updated_at
		  FROM books_quotes q JOIN books_books b ON b.id = q.book_id
		 WHERE b.id = ? AND b.user_id = ?
		 ORDER BY q.created_at DESC, q.id DESC`, bookID, userID)
	if err != nil {
		return nil, fmt.Errorf("books: quotes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Quote
	for rows.Next() {
		var q Quote
		var page sql.NullInt64
		var created, updated string
		if err := rows.Scan(&q.ID, &page, &q.Text, &q.Comment, &created, &updated); err != nil {
			return nil, fmt.Errorf("books: scan quote: %w", err)
		}
		q.Page = int(page.Int64)
		if q.CreatedAt, q.UpdatedAt, err = parseStamps(created, updated); err != nil {
			return nil, fmt.Errorf("books: quote %d: %w", q.ID, err)
		}
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: quotes: %w", err)
	}
	return out, nil
}
