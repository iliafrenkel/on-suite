package later

import (
	"context"
	"fmt"
	"strings"
)

// ftsQuery turns free text into an FTS5 MATCH expression that can never be a
// syntax error.
//
// Mirrors ON Reader's ftsQuery (internal/apps/reader/search.go), itself a
// copy of ON Notes' — copied rather than shared because apps never import
// each other; see "Cross-app mirroring" in PATTERNS.md.
//
// Each word becomes its own quoted phrase, doubling any embedded quote, so
// anything a person types — an operator like AND, a bare quote, a
// parenthesis — lands inside the quotes as inert phrase text instead of
// breaking the query. The trailing * is FTS5's prefix operator, which is
// what makes a live filter match while a word is still being typed.
func ftsQuery(q string) string {
	words := strings.Fields(q)
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"*`
	}
	return strings.Join(quoted, " ")
}

// MatchIn says where a search result matched, for its snippet's label
// (spec: "a snippet saying where it matched").
type MatchIn string

// Where a result can match. A title-only match has no snippet: the row
// already shows the title.
const (
	MatchTitle     MatchIn = ""
	MatchHighlight MatchIn = "highlight" // a highlight's quote or comment
	MatchNote      MatchIn = "note"
	MatchText      MatchIn = "text"
)

// SnippetOpen and SnippetClose wrap each matched word in a snippet. Control
// characters never occur in content_text, quotes, comments or notes the way
// the user sees them, so they can't be confused with real text.
const (
	SnippetOpen  = "\x02"
	SnippetClose = "\x03"
)

// SearchHit is one search result: its list row plus where it matched.
type SearchHit struct {
	ListItem
	In      MatchIn
	Snippet string // "" for MatchTitle
}

// Search finds userID's articles in any state matching query, best match
// first, narrowed to tag unless it is "". A query with no words finds
// nothing.
func (st *Store) Search(ctx context.Context, userID int64, query, tag string, offset, limit int) ([]SearchHit, error) {
	match := ftsQuery(query)
	if match == "" {
		return nil, nil
	}
	// Two steps. The inner "page" query ranks every match and cuts the
	// requested page, touching no snippet. The outer query then builds the
	// three snippets for just that page's rows. snippet() on a long body is
	// costly, and one query computes it for every match before sorting and
	// LIMIT; with the platform's single DB connection and a search box that
	// fires on every keystroke, that stalled the whole suite. The outer
	// MATCH is required for snippet() to work.
	//
	// later_search is named, never aliased: the driver resolves MATCH,
	// snippet() and bm25() against the real name. Column numbers: 0 title,
	// 1 body, 2 highlights, 3 note. bm25 weights favour the title, then
	// what the reader wrote, then the text.
	rows, err := st.db.QueryContext(ctx, `
		WITH page AS (
		  SELECT later_search.rowid AS id,
		         bm25(later_search, 10.0, 1.0, 4.0, 4.0) AS score
		    FROM later_search
		    JOIN later_articles a ON a.id = later_search.rowid
		   WHERE later_search MATCH ? AND a.user_id = ? AND `+tagFilter+`
		   ORDER BY score, a.id DESC
		   LIMIT ? OFFSET ?)
		SELECT `+listSelect+`,
		       snippet(later_search, 2, char(2), char(3), '…', 16),
		       snippet(later_search, 3, char(2), char(3), '…', 16),
		       snippet(later_search, 1, char(2), char(3), '…', 16)
		  FROM page
		  JOIN later_search ON later_search.rowid = page.id
		  JOIN later_articles a ON a.id = page.id`+listJoins+`
		 WHERE later_search MATCH ?
		 ORDER BY page.score, a.id DESC`, match, userID, tag, tag, limit, offset, match)
	if err != nil {
		return nil, fmt.Errorf("later: search: %w", err)
	}
	defer func() { _ = rows.Close() }()
	now := st.now()
	var hits []SearchHit
	for rows.Next() {
		var inHighlights, inNote, inText string
		it, err := scanListItem(rows, now, &inHighlights, &inNote, &inText)
		if err != nil {
			return nil, err
		}
		hit := SearchHit{ListItem: it}
		// snippet() returns a column's opening words even when it didn't
		// match, so a column matched only if its snippet has a marker.
		switch {
		case strings.Contains(inHighlights, SnippetOpen):
			hit.In, hit.Snippet = MatchHighlight, inHighlights
		case strings.Contains(inNote, SnippetOpen):
			hit.In, hit.Snippet = MatchNote, inNote
		case strings.Contains(inText, SnippetOpen):
			hit.In, hit.Snippet = MatchText, inText
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("later: search: %w", err)
	}
	return hits, nil
}
