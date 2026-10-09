package books

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// StartReading begins a new reading of a book today: its first, or a
// re-read. It carries over the previous reading's format (spec "Progress
// and finishing"). A book is read once at a time, so starting while a
// reading is in progress is a Refusal.
func (st *Store) StartReading(ctx context.Context, userID, id int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin start: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	var active int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM books_readings WHERE book_id = ? AND status = 'reading'`, id).Scan(&active); err != nil {
		return fmt.Errorf("books: start: %w", err)
	}
	if active > 0 {
		return &Refusal{Msg: "You're already reading this book."}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO books_readings (book_id, status, format, started_on, created_at)
		VALUES (?, 'reading',
		        (SELECT format FROM books_readings WHERE book_id = ? ORDER BY created_at DESC, id DESC LIMIT 1),
		        ?, ?)`,
		id, id, st.Today(), formatTime(st.now())); err != nil {
		return fmt.Errorf("books: start: %w", err)
	}
	return tx.Commit()
}

// FinishReading closes the reading in progress as finished on day
// (YYYY-MM-DD; "" is today).
func (st *Store) FinishReading(ctx context.Context, userID, id int64, day string) error {
	return st.closeReading(ctx, userID, id, day, StatusFinished)
}

// MarkDNF closes the reading in progress as not finished on day.
func (st *Store) MarkDNF(ctx context.Context, userID, id int64, day string) error {
	return st.closeReading(ctx, userID, id, day, StatusDNF)
}

// closeReading ends the reading in progress. The day must be a real date,
// not in the future and not before the reading started.
func (st *Store) closeReading(ctx context.Context, userID, id int64, day string, to Status) error {
	if day == "" {
		day = st.Today()
	}
	if err := st.checkDay(day); err != nil {
		return err
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin close reading: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	var rid int64
	var started sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT id, started_on FROM books_readings WHERE book_id = ? AND status = 'reading'`, id).Scan(&rid, &started)
	if errors.Is(err, sql.ErrNoRows) {
		return &Refusal{Msg: "This book isn't being read right now."}
	}
	if err != nil {
		return fmt.Errorf("books: close reading: %w", err)
	}
	if started.Valid && day < started.String {
		return &Refusal{Msg: "That's before you started reading it (" + ShowDay(started.String) + ")."}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE books_readings SET status = ?, finished_on = ? WHERE id = ?`, string(to), day, rid); err != nil {
		return fmt.Errorf("books: close reading: %w", err)
	}
	return tx.Commit()
}

// ShowDay formats a stored date for people: "2026-10-09" → "9 Oct 2026".
// Anything else comes back unchanged.
func ShowDay(day string) string {
	t, err := time.Parse(dayLayout, day)
	if err != nil {
		return day
	}
	return t.Format("2 Jan 2006")
}
