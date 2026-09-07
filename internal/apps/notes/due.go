package notes

import (
	"context"
	"fmt"
	"html/template"
	"time"
)

// DueRow is one entry in /notes/due: a node plus its ancestor breadcrumb,
// outermost first — spec §11 says each hit shows its ancestor path so a
// result three levels deep is legible on its own. Overdue is set by
// GroupByDue, once, rather than computed in the template — the same reason
// outlineRow.Overdue exists.
//
// TitleHTML is DisplayTitle run through highlightPlainText — escaped, and
// with any filter match wrapped in <mark>, but never through Render: the
// title is a link (see DisplayTitleHTML's own doc comment on why that
// stays plain), so this only ever adds <mark>, never anything Render could
// produce. It equals plain escaped DisplayTitle when there is no filter.
// Snippet is a highlighted excerpt of Note, set only when the filter
// matched there and not in the title — issue #86's "which field matched".
type DueRow struct {
	Node
	Crumbs    []Node
	Overdue   bool
	TitleHTML template.HTML
	Snippet   template.HTML
}

// dueView is what /notes/due renders — Groups wraps GroupByDue's own
// buckets so the template can also read Query/SearchAction, which DueGroups
// itself has no reason to carry.
type dueView struct {
	Groups       DueGroups
	Query        string
	SearchAction string
}

// DueGroups is /notes/due's four buckets, spec §11's Overdue / Today / This
// week / Later — in that display order.
type DueGroups struct {
	Overdue, Today, ThisWeek, Later []DueRow
}

type dueSection struct {
	Title string
	Rows  []DueRow
}

// Sections lists the four groups in display order, for the template to
// range over instead of hardcoding four separate blocks.
func (g DueGroups) Sections() []dueSection {
	return []dueSection{
		{"Overdue", g.Overdue},
		{"Today", g.Today},
		{"This week", g.ThisWeek},
		{"Later", g.Later},
	}
}

// GroupByDue buckets rows, which must already have DueOn set (Store.Due
// only returns those), against today — spec §11: comparison is against the
// server's local date, a single-household deployment in one timezone. "This
// week" runs through the sixth day from today inclusive: a week that starts
// today, not a calendar week, so what counts as "this week" does not jump
// around depending on what day it is.
func GroupByDue(rows []DueRow, today time.Time) DueGroups {
	todayStr := today.Format("2006-01-02")
	weekEnd := today.AddDate(0, 0, 6).Format("2006-01-02")

	var g DueGroups
	for _, row := range rows {
		switch {
		case row.DueOn < todayStr:
			row.Overdue = true
			g.Overdue = append(g.Overdue, row)
		case row.DueOn == todayStr:
			g.Today = append(g.Today, row)
		case row.DueOn <= weekEnd:
			g.ThisWeek = append(g.ThisWeek, row)
		default:
			g.Later = append(g.Later, row)
		}
	}
	return g
}

// DueBadgeCount reports how many rows are overdue or due today — the
// "needs attention now" subset of GroupByDue's four buckets, without
// needing the ancestor crumbs DueRow carries for the /notes/due page
// itself. Used for the toolbar's due-count badge (outline.html).
func DueBadgeCount(rows []Node, today time.Time) int {
	todayStr := today.Format("2006-01-02")
	count := 0
	for _, n := range rows {
		if n.DueOn <= todayStr {
			count++
		}
	}
	return count
}

// Due returns every one of userID's nodes with a due date set, excluding
// done ones and archived ones — spec §11's "done and archived nodes are
// excluded". A node that sits under an archived ancestor is excluded too,
// via archivedBelowCTE (store.go), shared with Store.Search — spec §13's
// subtree rule applies here exactly as it does there. Ordered by due_on so
// GroupByDue only has to bucket, never sort.
//
// query, when non-empty, additionally requires a title/note match — the
// filter behind /notes/due's own search box. "" matches every row, the same
// as calling this before that feature existed.
func (st *Store) Due(ctx context.Context, userID int64, query string) ([]Node, error) {
	matchClause := ""
	args := []any{userID, userID, userID}
	if q := ftsQuery(query); q != "" {
		matchClause = " AND " + matchedIDsSubquery
		args = append(args, q)
	}
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE `+archivedBelowCTE+`
		 SELECT `+nodeColumns+`
		   FROM notes_nodes
		  WHERE user_id = ? AND due_on IS NOT NULL AND done_at IS NULL
		    AND id NOT IN (SELECT id FROM archived_below)`+matchClause+`
		  ORDER BY due_on`, args...)
	if err != nil {
		return nil, fmt.Errorf("notes: due nodes: %w", err)
	}
	return collectNodes(rows, "due nodes")
}
