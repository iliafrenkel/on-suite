package books

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Unit is what a reading's progress is counted in.
type Unit string

// Progress units (spec "Data model": books_progress).
const (
	UnitPage    Unit = "page"
	UnitPercent Unit = "percent"
)

// UnitFor is a reading's unit: audio is counted in percent, and so is any
// book without a page count; everything else in pages.
func UnitFor(format string, pages int) Unit {
	if format == "audio" || pages <= 0 {
		return UnitPercent
	}
	return UnitPage
}

// Progress is a reading's latest progress row. Unit is the unit that row
// was recorded in, which is not always the reading's unit now — its format
// or the book's page count may have changed since — so read it through In.
type Progress struct {
	Unit       Unit // "" when nothing has been recorded
	Value      int
	RecordedAt time.Time
}

// Set reports whether any progress has been recorded.
func (p Progress) Set() bool { return p.Unit != "" }

// In is the progress in unit u for a book of pages pages. Converting
// between pages and percent needs a page count; without one it is 0.
func (p Progress) In(u Unit, pages int) int {
	switch {
	case !p.Set():
		return 0
	case p.Unit == u:
		return p.Value
	case pages <= 0:
		return 0
	case u == UnitPercent:
		return p.Value * 100 / pages
	default:
		return p.Value * pages / 100
	}
}

// Percent is how far through the book p is, 0–100, for a progress bar.
func (p Progress) Percent(pages int) int { return min(100, max(0, p.In(UnitPercent, pages))) }

// progressJoin attaches p, the latest progress row of r (see latestJoin).
const progressJoin = `LEFT JOIN books_progress p ON p.id = (
	SELECT id FROM books_progress WHERE reading_id = r.id ORDER BY recorded_at DESC, id DESC LIMIT 1)`

// scanProgress turns p's nullable columns into a Progress.
func scanProgress(page, percent sql.NullInt64, at sql.NullString) (Progress, error) {
	var p Progress
	switch {
	case page.Valid:
		p.Unit, p.Value = UnitPage, int(page.Int64)
	case percent.Valid:
		p.Unit, p.Value = UnitPercent, int(percent.Int64)
	default:
		return Progress{}, nil
	}
	t, err := parseTime(at.String)
	if err != nil {
		return Progress{}, fmt.Errorf("books: recorded_at: %w", err)
	}
	p.RecordedAt = t
	return p, nil
}

// active is a book's reading in progress, with what its unit depends on.
type active struct {
	id     int64
	format string
	pages  int
}

// activeReading finds the reading in progress of book id inside tx; a
// Refusal when there is none.
func activeReading(ctx context.Context, tx *sql.Tx, id int64) (active, error) {
	var a active
	err := tx.QueryRowContext(ctx, `
		SELECT r.id, COALESCE(r.format, ''), COALESCE(b.pages, 0)
		  FROM books_readings r JOIN books_books b ON b.id = r.book_id
		 WHERE r.book_id = ? AND r.status = 'reading'`, id).Scan(&a.id, &a.format, &a.pages)
	if errors.Is(err, sql.ErrNoRows) {
		return active{}, &Refusal{Msg: "This book isn't being read right now."}
	}
	if err != nil {
		return active{}, fmt.Errorf("books: reading in progress: %w", err)
	}
	return a, nil
}

// checkProgress refuses a value outside its unit's range (spec "Errors":
// negative, past the page count, percent over 100).
func checkProgress(u Unit, pages, value int) error {
	if u == UnitPercent {
		if value < 0 || value > 100 {
			return &Refusal{Msg: "Enter a percentage from 0 to 100."}
		}
		return nil
	}
	if value < 0 || value > pages {
		return &Refusal{Msg: fmt.Sprintf("Enter a page from 0 to %d.", pages)}
	}
	return nil
}

// insertProgress adds a progress row in unit u.
func insertProgress(ctx context.Context, tx *sql.Tx, readingID int64, u Unit, value int, at string) error {
	var page, percent any
	if u == UnitPage {
		page = value
	} else {
		percent = value
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO books_progress (reading_id, page, percent, recorded_at) VALUES (?, ?, ?, ?)`,
		readingID, page, percent, at); err != nil {
		return fmt.Errorf("books: record progress: %w", err)
	}
	return nil
}

// RecordProgress notes where the reading in progress stands: a page, or a
// percentage for audio and for books with no page count (UnitFor). Every
// call adds a row — the history B4's stats count pages from (spec "Derived
// values") — and the newest one is the current progress. Reaching the last
// page doesn't finish the book (spec "Progress and finishing").
func (st *Store) RecordProgress(ctx context.Context, userID, id int64, value int) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin progress: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	a, err := activeReading(ctx, tx, id)
	if err != nil {
		return err
	}
	u := UnitFor(a.format, a.pages)
	if err := checkProgress(u, a.pages, value); err != nil {
		return err
	}
	if err := insertProgress(ctx, tx, a.id, u, value, formatTime(st.now())); err != nil {
		return err
	}
	return tx.Commit()
}
