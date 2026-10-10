package books

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// Stats implements app.Stater: ON Books' card on the admin page.
func (a *App) Stats(ctx context.Context, handle *sql.DB) ([]app.Stat, error) {
	return NewStore(handle).AdminStats(ctx)
}

// AdminStats describes ON Books across every user (spec "Admin card"):
// books per shelf, readings, notes, quotes, and how much the stored covers
// weigh. (Stats is the reading stats of one user.)
func (st *Store) AdminStats(ctx context.Context) ([]app.Stat, error) {
	var total, reading, want, read, dnf, readings, notes, quotes, coverBytes int64
	if err := st.db.QueryRowContext(ctx, `
		SELECT count(*), coalesce(sum(shelf = 'reading'), 0), coalesce(sum(shelf = 'want'), 0),
		       coalesce(sum(shelf = 'read'), 0), coalesce(sum(shelf = 'dnf'), 0)
		  FROM (SELECT `+shelfExpr+` AS shelf FROM books_books b `+latestJoin+`)`).Scan(
		&total, &reading, &want, &read, &dnf); err != nil {
		return nil, fmt.Errorf("books: admin stats: %w", err)
	}
	if err := st.db.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM books_readings), (SELECT count(*) FROM books_notes),
		       (SELECT count(*) FROM books_quotes), (SELECT coalesce(sum(length(bytes)), 0) FROM books_covers)`).Scan(
		&readings, &notes, &quotes, &coverBytes); err != nil {
		return nil, fmt.Errorf("books: admin stats: %w", err)
	}
	n := func(v int64) string { return strconv.FormatInt(v, 10) }
	return []app.Stat{
		{Label: "Books", Value: n(total)},
		{Label: "Reading", Value: n(reading)},
		{Label: "Want to read", Value: n(want)},
		{Label: "Read", Value: n(read)},
		{Label: "Did not finish", Value: n(dnf)},
		{Label: "Readings", Value: n(readings)},
		{Label: "Notes", Value: n(notes)},
		{Label: "Quotes", Value: n(quotes)},
		{Label: "Stored covers", Value: humanBytes(coverBytes), Hint: "cover images kept with the books, for everyone"},
	}, nil
}

// humanBytes renders a byte count the way a person reads one. It mirrors
// ON Later's own humanBytes: apps never import each other, so this is an
// independent copy (PATTERNS.md, "Cross-app mirroring").
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
