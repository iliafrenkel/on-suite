package later

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// Stats implements app.Stater: ON Later's card on the admin page.
func (a *App) Stats(ctx context.Context, handle *sql.DB) ([]app.Stat, error) {
	return NewStore(handle).Stats(ctx)
}

// Stats describes ON Later across every user (spec: "Admin card"):
// articles by state, highlights, and how much the kept images weigh.
func (st *Store) Stats(ctx context.Context) ([]app.Stat, error) {
	var total, unread, reading, archived, highlights, imageBytes int64
	if err := st.db.QueryRowContext(ctx, `
		SELECT count(*),
		       coalesce(sum(state = 'unread'), 0),
		       coalesce(sum(state = 'reading'), 0),
		       coalesce(sum(state = 'archived'), 0)
		  FROM later_articles`).Scan(&total, &unread, &reading, &archived); err != nil {
		return nil, fmt.Errorf("later: stats articles: %w", err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT count(*) FROM later_highlights`).Scan(&highlights); err != nil {
		return nil, fmt.Errorf("later: stats highlights: %w", err)
	}
	if err := st.db.QueryRowContext(ctx,
		`SELECT coalesce(sum(length(bytes)), 0) FROM later_images`).Scan(&imageBytes); err != nil {
		return nil, fmt.Errorf("later: stats images: %w", err)
	}
	n := func(v int64) string { return strconv.FormatInt(v, 10) }
	return []app.Stat{
		{Label: "Articles", Value: n(total)},
		{Label: "Unread", Value: n(unread)},
		{Label: "Reading", Value: n(reading)},
		{Label: "Archived", Value: n(archived)},
		{Label: "Highlights", Value: n(highlights)},
		{Label: "Stored images", Value: humanBytes(imageBytes), Hint: "pictures kept with saved articles, for everyone"},
	}, nil
}

// humanBytes renders a byte count the way a person reads one. It mirrors
// ON Paste's own humanBytes: apps never import each other, so this is an
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
