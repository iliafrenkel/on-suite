package notes

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// Stats reports instance-wide counts for the admin page — spec's N10
// build-order item, "Stater admin card". Like ON Paste's own Stats
// (internal/apps/paste/store.go) this counts across every user's tree,
// not just one account's: the admin page is a whole-instance view, the
// same reason that function's own query carries no user_id filter.
//
// Overdue mirrors GroupByDue's own definition (due.go): due, not done,
// not archived, strictly before today. It is computed here as a flat
// WHERE clause rather than by walking archivedBelowCTE the way the due
// list itself does for a doubly-nested archived ancestor (store.go) — a
// dashboard count is an approximation by nature, and the gap this leaves
// (a due bullet whose direct parent is not archived, but some ancestor
// further up is) is judged not worth a recursive CTE in a stats query
// nobody reads for precision.
func (st *Store) Stats(ctx context.Context) ([]app.Stat, error) {
	today := st.now().Format("2006-01-02")
	var (
		total, done, overdue, archived, shared int64
		newest                                 sql.NullString
	)
	err := st.db.QueryRowContext(ctx,
		`SELECT count(*),
		        coalesce(sum(CASE WHEN done_at IS NOT NULL THEN 1 ELSE 0 END), 0),
		        coalesce(sum(CASE WHEN due_on IS NOT NULL AND due_on < ?
		                           AND done_at IS NULL AND archived_at IS NULL
		                      THEN 1 ELSE 0 END), 0),
		        coalesce(sum(CASE WHEN archived_at IS NOT NULL THEN 1 ELSE 0 END), 0),
		        coalesce(sum(CASE WHEN share_slug IS NOT NULL THEN 1 ELSE 0 END), 0),
		        max(created_at)
		   FROM notes_nodes`, today).
		Scan(&total, &done, &overdue, &archived, &shared, &newest)
	if err != nil {
		return nil, fmt.Errorf("notes: stats: %w", err)
	}

	newestLabel := "never"
	if newest.Valid {
		t, err := parseTime(newest.String)
		if err != nil {
			return nil, err
		}
		newestLabel = t.Format("2006-01-02 15:04 MST")
	}

	return []app.Stat{
		{Label: "Bullets", Value: strconv.FormatInt(total, 10)},
		{Label: "Done", Value: strconv.FormatInt(done, 10)},
		{Label: "Overdue", Value: strconv.FormatInt(overdue, 10),
			Hint: "due, not done, not archived"},
		{Label: "Archived", Value: strconv.FormatInt(archived, 10)},
		{Label: "Shared", Value: strconv.FormatInt(shared, 10),
			Hint: "readable by anyone holding the link"},
		{Label: "Newest bullet", Value: newestLabel},
	}, nil
}
