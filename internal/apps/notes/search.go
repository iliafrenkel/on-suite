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
	// ShowCompleted is spec §12's preference, read once per request so the
	// toolbar's toggle can show its own opposite action — issue #88: unlike
	// /notes/due (which excludes done nodes unconditionally, no preference
	// to toggle), Search actually takes showCompleted, so a completed match
	// was silently unfindable here with no way to see or change why.
	ShowCompleted bool
}

// ftsQuery turns free text into an FTS5 MATCH expression that can never be
// a syntax error. Each word becomes its own quoted phrase — doubling any
// embedded '"' the way FTS5's string literals require — so a user typing an
// operator FTS5 would otherwise interpret (AND, OR, NOT, *, :, an
// unbalanced quote) always searches for that literal text instead of
// breaking the query. Space-separated quoted phrases are ANDed by FTS5's
// own default, so a multi-word search requires every word to appear
// somewhere in the bullet, not necessarily adjacent to the others.
//
// The trailing * after each phrase's closing quote is FTS5's own prefix
// operator — with prefix indexing enabled (0006_search_prefix.sql) this
// matches any token *starting with* the word, not only an exact token, so
// a live-as-you-type filter reacts before a word is finished. A literal '*'
// the user typed lands inside the quotes, where it is inert phrase text,
// not this operator — see TestSearchHandlesFTS5SyntaxCharactersLiterally.
func ftsQuery(q string) string {
	words := strings.Fields(q)
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"*`
	}
	return strings.Join(quoted, " ")
}

// matchedIDsSubquery is a JOIN-free "which ids match" fragment: notes_fts's
// rowid is exactly notes_nodes.id (external-content table), so no join to
// notes_nodes is needed just to test membership. It carries no user_id
// filter of its own — every caller embeds it inside a WHERE that already
// has its own "user_id = ?", and "id IN (<possibly other users' ids too>)
// AND user_id = ?" is still correct, since a given id belongs to exactly
// one user regardless of what else this subquery happens to return.
//
// Takes exactly one ? — the query string ftsQuery already built.
const matchedIDsSubquery = `id IN (SELECT rowid FROM notes_fts WHERE notes_fts MATCH ?)`

// searchTerms splits a raw query into the literal words used for display
// highlighting (highlight.go) — deliberately not ftsQuery's escaped/quoted
// FTS5 syntax, since these are matched against rendered text with plain
// substring comparison, not sent to SQLite.
func searchTerms(query string) []string {
	return strings.Fields(query)
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
