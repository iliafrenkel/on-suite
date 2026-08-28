package notes

import (
	"context"
	"fmt"
	"strings"
)

// SearchRow is one hit in /notes/search: a node plus its ancestor
// breadcrumb, outermost first — spec §12: "each hit renders as the
// matching bullet plus its ancestor breadcrumb".
type SearchRow struct {
	Node
	Crumbs []Node
}

// searchView is what /notes/search renders.
type searchView struct {
	Query string
	Rows  []SearchRow
}

// ftsQuery turns free text into an FTS5 MATCH expression that can never be
// a syntax error. Each word becomes its own quoted phrase — doubling any
// embedded '"' the way FTS5's string literals require — so a user typing an
// operator FTS5 would otherwise interpret (AND, OR, NOT, *, :, an
// unbalanced quote) always searches for that literal text instead of
// breaking the query. Space-separated quoted phrases are ANDed by FTS5's
// own default, so a multi-word search requires every word to appear
// somewhere in the bullet, not necessarily adjacent to the others.
func ftsQuery(q string) string {
	words := strings.Fields(q)
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
	}
	return strings.Join(quoted, " ")
}

// Search runs a full-text search over title and note across userID's whole
// tree — spec §12. An empty query (nothing left after ftsQuery) returns no
// rows rather than asking FTS5 to MATCH an empty string, which is a syntax
// error of its own. Results are ordered by FTS5's own relevance rank, and,
// like Store.Due, honour showCompleted: a done bullet that matches is
// excluded unless the preference is on. A node that is archived, or that
// sits under an archived ancestor, is excluded unconditionally — spec
// §13 — via archivedBelowCTE (store.go), shared with Store.Due.
//
// The query references notes_fts by its real name rather than through a
// table alias: this driver's FTS5 support resolves MATCH and rank against
// an aliased virtual table with "no such column" errors, so both stay
// unaliased while notes_nodes is joined in as n.
func (st *Store) Search(ctx context.Context, userID int64, query string, showCompleted bool) ([]Node, error) {
	q := ftsQuery(query)
	if q == "" {
		return nil, nil
	}
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE `+archivedBelowCTE+`
		 SELECT `+aliasNodeColumns("n")+`
		   FROM notes_fts
		   JOIN notes_nodes n ON n.id = notes_fts.rowid
		  WHERE notes_fts MATCH ? AND n.user_id = ? AND (? OR n.done_at IS NULL)
		    AND n.id NOT IN (SELECT id FROM archived_below)
		  ORDER BY rank`,
		userID, userID, q, userID, showCompleted)
	if err != nil {
		return nil, fmt.Errorf("notes: search: %w", err)
	}
	return collectNodes(rows, "search results")
}
