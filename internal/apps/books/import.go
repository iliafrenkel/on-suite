package books

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// ImportResult is what an import did: how many books it added, and the
// titles of the ones it skipped as already there, in file order.
type ImportResult struct {
	Imported int
	Skipped  []string
}

// Import adds rows (ParseGoodreads) to userID's library in one
// transaction, so it is all or nothing (spec "Import (B5)"). A row is
// skipped when the library — or an earlier row — already has the book:
// the same ISBN-13; or, when either of the two has no ISBN, the same
// title and authors ignoring case, spaces and punctuation (decided 2026-10-10 while planning B5,
// so a book typed in without an ISBN isn't imported a second time).
// Imported books reach books_search through its insert trigger, like any
// other book.
func (st *Store) Import(ctx context.Context, userID int64, rows []ImportBook) (ImportResult, error) {
	var res ImportResult
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("books: begin import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	have, err := libraryKeys(ctx, tx, userID)
	if err != nil {
		return res, err
	}
	now := formatTime(st.now())
	for _, b := range rows {
		keys := bookKeys(b.BookInput)
		if have.has(keys, b.ISBN != "") {
			res.Skipped = append(res.Skipped, b.Title)
			continue
		}
		have.add(keys, b.ISBN != "")
		if err := importBook(ctx, tx, userID, b, now); err != nil {
			return ImportResult{}, err
		}
		res.Imported++
	}
	if err := tx.Commit(); err != nil {
		return ImportResult{}, fmt.Errorf("books: commit import: %w", err)
	}
	return res, nil
}

// bookKey is what a duplicate is recognised by: an ISBN-13, and a title
// with its authors, ignoring case, spaces and punctuation — Goodreads
// writes "James S.A. Corey" where Open Library has "James S. A. Corey".
type bookKey struct{ isbn, name string }

func bookKeys(in BookInput) bookKey {
	return bookKey{isbn: in.ISBN, name: looseText(in.Title) + "\x00" + looseText(in.Authors)}
}

// looseText is s lowercased, with only its letters and digits.
func looseText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

// keySet is the books already there: their ISBNs, the names of them all,
// and the names of those with no ISBN.
type keySet struct{ isbns, names, bare map[string]bool }

func (k keySet) has(b bookKey, hasISBN bool) bool {
	if hasISBN {
		return k.isbns[b.isbn] || k.bare[b.name]
	}
	return k.names[b.name]
}

func (k keySet) add(b bookKey, hasISBN bool) {
	k.names[b.name] = true
	if hasISBN {
		k.isbns[b.isbn] = true
	} else {
		k.bare[b.name] = true
	}
}

// libraryKeys is the keySet of every book userID has. The rows are closed
// before the import writes: the database has one connection.
func libraryKeys(ctx context.Context, tx *sql.Tx, userID int64) (keySet, error) {
	have := keySet{isbns: map[string]bool{}, names: map[string]bool{}, bare: map[string]bool{}}
	rows, err := tx.QueryContext(ctx,
		`SELECT title, authors, COALESCE(isbn13, '') FROM books_books WHERE user_id = ?`, userID)
	if err != nil {
		return have, fmt.Errorf("books: import library: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var in BookInput
		if err := rows.Scan(&in.Title, &in.Authors, &in.ISBN); err != nil {
			return have, fmt.Errorf("books: scan import library: %w", err)
		}
		have.add(bookKeys(in), in.ISBN != "")
	}
	if err := rows.Err(); err != nil {
		return have, fmt.Errorf("books: import library: %w", err)
	}
	return have, nil
}

// importBook writes one row: the book, its readings, its note and its
// tags. Earlier
// reads go in first, so the main reading is the latest and decides the
// shelf (latestJoin breaks the created_at tie by id).
func importBook(ctx context.Context, tx *sql.Tx, userID int64, b ImportBook, now string) error {
	added, changed := now, now
	if b.AddedOn != "" {
		added, changed = formatTime(localDay(b.AddedOn)), formatTime(localDay(max(b.AddedOn, b.FinishedOn)))
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO books_books (user_id, title, subtitle, authors, year, pages, isbn13,
			series_name, series_number, rating, review, added_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, b.Title, b.Subtitle, b.Authors, nullInt(b.Year), nullInt(b.Pages), nullText(b.ISBN),
		b.SeriesName, b.SeriesNumber, nullInt(b.Rating), b.Review, added, changed)
	if err != nil {
		return fmt.Errorf("books: import %q: %w", b.Title, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("books: import %q: %w", b.Title, err)
	}
	for range b.EarlierReads {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO books_readings (book_id, status, created_at) VALUES (?, 'finished', ?)`, id, now); err != nil {
			return fmt.Errorf("books: import an earlier reading: %w", err)
		}
	}
	switch b.Shelf {
	case ShelfRead:
		_, err = tx.ExecContext(ctx, `INSERT INTO books_readings (book_id, status, format, finished_on, created_at)
			VALUES (?, 'finished', ?, ?, ?)`, id, nullText(b.Format), nullText(b.FinishedOn), now)
	case ShelfReading:
		_, err = tx.ExecContext(ctx, `INSERT INTO books_readings (book_id, status, format, started_on, created_at)
			VALUES (?, 'reading', ?, ?, ?)`, id, nullText(b.Format), nullText(b.StartedOn), now)
	case ShelfDNF:
		_, err = tx.ExecContext(ctx, `INSERT INTO books_readings (book_id, status, format, finished_on, created_at)
			VALUES (?, 'dnf', ?, ?, ?)`, id, nullText(b.Format), nullText(b.FinishedOn), now)
	}
	if err != nil {
		return fmt.Errorf("books: import a reading: %w", err)
	}
	// Private Notes become one note, dated when the book was added; the
	// notes' insert trigger puts it in books_search.
	if b.Note != "" {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO books_notes (book_id, body, created_at, updated_at) VALUES (?, ?, ?, ?)`,
			id, b.Note, added, added); err != nil {
			return fmt.Errorf("books: import a note: %w", err)
		}
	}
	return linkTags(ctx, tx, userID, id, b.Tags)
}

// localDay is the start of a YYYY-MM-DD day in the server's zone, which
// is where the suite's days are (#424). day is already a valid date.
func localDay(day string) time.Time {
	t, _ := time.ParseInLocation(dayLayout, day, time.Local)
	return t
}
