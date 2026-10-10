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
// (YYYY-MM-DD; "" is today) and, when rating is 1–5, rates the book (spec
// "Progress and finishing"). Rating 0 leaves the book's rating as it is, so
// a re-read needn't rate it again.
func (st *Store) FinishReading(ctx context.Context, userID, id int64, day string, rating int) error {
	return st.closeReading(ctx, userID, id, day, StatusFinished, rating, 0)
}

// MarkDNF closes the reading in progress as not finished on day. at, when
// not 0, is where it stopped, in the reading's unit (UnitFor); it is kept
// as the reading's last progress.
func (st *Store) MarkDNF(ctx context.Context, userID, id int64, day string, at int) error {
	return st.closeReading(ctx, userID, id, day, StatusDNF, 0, at)
}

// closeReading ends the reading in progress, rating the book and recording
// a last progress row on the way when asked to. The day must be a real
// date, not in the future and not before the reading started.
func (st *Store) closeReading(ctx context.Context, userID, id int64, day string, to Status, rating, at int) error {
	if err := checkRating(rating); err != nil {
		return err
	}
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
	a, err := activeReading(ctx, tx, id)
	if err != nil {
		return err
	}
	if a.startedOn != "" && day < a.startedOn {
		return &Refusal{Msg: "That's before you started reading it (" + ShowDay(a.startedOn) + ")."}
	}
	if at != 0 {
		u := UnitFor(a.format, a.pages)
		if err := checkProgress(u, a.pages, at); err != nil {
			return err
		}
		same, err := latestProgressIs(ctx, tx, a.id, u, at)
		if err != nil {
			return err
		}
		if !same { // the prefilled "Stopped at" repeats the last row: not new reading
			if err := insertProgress(ctx, tx, a.id, u, at, formatTime(st.now())); err != nil {
				return err
			}
		}
	}
	if rating != 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE books_books SET rating = ? WHERE id = ?`, rating, id); err != nil {
			return fmt.Errorf("books: rate on finish: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE books_readings SET status = ?, finished_on = ? WHERE id = ?`, string(to), day, a.id); err != nil {
		return fmt.Errorf("books: close reading: %w", err)
	}
	return tx.Commit()
}

// latestProgressIs reports whether the reading's newest progress row holds
// value in unit u.
func latestProgressIs(ctx context.Context, tx *sql.Tx, readingID int64, u Unit, value int) (bool, error) {
	var page, percent sql.NullInt64
	err := tx.QueryRowContext(ctx, `
		SELECT page, percent FROM books_progress
		 WHERE reading_id = ? ORDER BY recorded_at DESC, id DESC LIMIT 1`, readingID).Scan(&page, &percent)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("books: latest progress: %w", err)
	}
	if u == UnitPage {
		return page.Valid && int(page.Int64) == value, nil
	}
	return percent.Valid && int(percent.Int64) == value, nil
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
