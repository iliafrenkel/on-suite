package notes

import (
	"context"
	"fmt"
	"time"
)

// DueRow is one entry in /notes/due: a node plus its ancestor breadcrumb,
// outermost first — spec §11 says each hit shows its ancestor path so a
// result three levels deep is legible on its own. Overdue is set by
// GroupByDue, once, rather than computed in the template — the same reason
// outlineRow.Overdue exists.
type DueRow struct {
	Node
	Crumbs  []Node
	Overdue bool
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

// Due returns every one of userID's nodes with a due date set, excluding
// done ones and archived ones — spec §11's "done and archived nodes are
// excluded". A node that sits under an archived ancestor is excluded too,
// via archivedBelowCTE (store.go), shared with Store.Search — spec §13's
// subtree rule applies here exactly as it does there. Ordered by due_on so
// GroupByDue only has to bucket, never sort.
func (st *Store) Due(ctx context.Context, userID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE `+archivedBelowCTE+`
		 SELECT `+nodeColumns+`
		   FROM notes_nodes
		  WHERE user_id = ? AND due_on IS NOT NULL AND done_at IS NULL
		    AND id NOT IN (SELECT id FROM archived_below)
		  ORDER BY due_on`, userID, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("notes: due nodes: %w", err)
	}
	return collectNodes(rows, "due nodes")
}
