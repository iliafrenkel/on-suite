package books

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Formats is every reading format, in the order menus offer them.
var Formats = []string{"paper", "ebook", "audio"}

// checkFormat accepts "" (not said) or one of Formats. Formats come from
// buttons and selects, so anything else is ErrInvalid.
func checkFormat(format string) error {
	if format == "" {
		return nil
	}
	for _, f := range Formats {
		if f == format {
			return nil
		}
	}
	return ErrInvalid
}

// SetFormat changes the format of the reading in progress. Its progress
// unit follows (UnitFor); rows already recorded keep theirs.
func (st *Store) SetFormat(ctx context.Context, userID, id int64, format string) error {
	if err := checkFormat(format); err != nil {
		return err
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin set format: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	a, err := activeReading(ctx, tx, id)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE books_readings SET format = ? WHERE id = ?`, nullText(format), a.id); err != nil {
		return fmt.Errorf("books: set format: %w", err)
	}
	return tx.Commit()
}

// Readings is a book's reading history, newest first (spec "Book pane":
// every reading with dates, format and status). A book that isn't userID's
// has none.
func (st *Store) Readings(ctx context.Context, userID, id int64) ([]Reading, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT r.id, r.status, r.format, r.started_on, r.finished_on
		  FROM books_readings r JOIN books_books b ON b.id = r.book_id
		 WHERE b.id = ? AND b.user_id = ?
		 ORDER BY r.created_at DESC, r.id DESC`, id, userID)
	if err != nil {
		return nil, fmt.Errorf("books: readings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Reading
	for rows.Next() {
		var rd Reading
		var status string
		var format, started, finished sql.NullString
		if err := rows.Scan(&rd.ID, &status, &format, &started, &finished); err != nil {
			return nil, fmt.Errorf("books: scan reading: %w", err)
		}
		rd.Status, rd.Format, rd.StartedOn, rd.FinishedOn = Status(status), format.String, started.String, finished.String
		out = append(out, rd)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: readings: %w", err)
	}
	return out, nil
}

// ReadingEdit is what can be changed about a reading: its dates and format
// (decided 2026-10-09). Its status can't be — deleting a reading is how a
// wrong one is put right. Dates are YYYY-MM-DD, "" for unknown.
type ReadingEdit struct {
	StartedOn, FinishedOn string
	Format                string
}

// UpdateReading changes one of a book's readings. A reading in progress
// has no finish date, so FinishedOn is ignored for it. Dates can't be in
// the future, and the finish can't come before the start.
func (st *Store) UpdateReading(ctx context.Context, userID, id, readingID int64, ed ReadingEdit) error {
	if err := checkFormat(ed.Format); err != nil {
		return err
	}
	for _, day := range []string{ed.StartedOn, ed.FinishedOn} {
		if day == "" {
			continue
		}
		if err := st.checkDay(day); err != nil {
			return err
		}
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin update reading: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	var status string
	err = tx.QueryRowContext(ctx,
		`SELECT status FROM books_readings WHERE id = ? AND book_id = ?`, readingID, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("books: update reading: %w", err)
	}
	if Status(status) == StatusReading {
		ed.FinishedOn = ""
	}
	if ed.StartedOn != "" && ed.FinishedOn != "" && ed.FinishedOn < ed.StartedOn {
		return &Refusal{Msg: "The finish date is before the start."}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE books_readings SET started_on = ?, finished_on = ?, format = ? WHERE id = ?`,
		nullText(ed.StartedOn), nullText(ed.FinishedOn), nullText(ed.Format), readingID); err != nil {
		return fmt.Errorf("books: update reading: %w", err)
	}
	return tx.Commit()
}

// DeleteReading removes one of a book's readings and its progress (ON
// DELETE CASCADE). The book's shelf follows whatever reading is now the
// latest — Want to read when none is left.
func (st *Store) DeleteReading(ctx context.Context, userID, id, readingID int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin delete reading: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM books_readings WHERE id = ? AND book_id = ?`, readingID, id)
	if err != nil {
		return fmt.Errorf("books: delete reading: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}
