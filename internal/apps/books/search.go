package books

import "strings"

// ftsQuery turns free text into an FTS5 MATCH expression that can never be
// a syntax error.
//
// Mirrors ON Later's ftsQuery (internal/apps/later/search.go), itself a
// copy of ON Reader's and ON Notes' — copied rather than shared because
// apps never import each other; see "Cross-app mirroring" in PATTERNS.md.
//
// Each word becomes its own quoted phrase, doubling any embedded quote, so
// anything a person types — an operator like AND, a bare quote, a
// parenthesis — lands inside the quotes as inert phrase text instead of
// breaking the query. The trailing * is FTS5's prefix operator, which is
// what makes the filter match while a word is still being typed.
func ftsQuery(q string) string {
	words := strings.Fields(q)
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"*`
	}
	return strings.Join(quoted, " ")
}

// MatchIn says where a filtered book matched, for its snippet's label
// (decided 2026-10-09: a third line on the row with the match).
type MatchIn string

// Where a book can match. A match on what the row already shows — title,
// subtitle, authors, series — has no snippet.
const (
	MatchBook    MatchIn = ""
	MatchQuote   MatchIn = "quote"
	MatchComment MatchIn = "comment"
	MatchNote    MatchIn = "note"
	MatchReview  MatchIn = "review"
)

// SnippetOpen and SnippetClose wrap each matched word in a snippet, as in
// ON Later. Control characters never occur in what people type into a
// form, so they can't be confused with real text.
const (
	SnippetOpen  = "\x02"
	SnippetClose = "\x03"
)

// searchJoin attaches the index to a list query. books_search is named,
// never aliased: the driver resolves MATCH and snippet() against the real
// name.
const searchJoin = `JOIN books_search ON books_search.rowid = b.id`

// snippetCols are a filtered list's four snippets, in the order matchOf
// takes them. Column numbers: 0 title, 1 subtitle, 2 authors, 3 series,
// 4 review, 5 notes, 6 quotes, 7 comments. Twelve tokens fit a list row.
const snippetCols = `snippet(books_search, 6, char(2), char(3), '…', 12),
		       snippet(books_search, 7, char(2), char(3), '…', 12),
		       snippet(books_search, 5, char(2), char(3), '…', 12),
		       snippet(books_search, 4, char(2), char(3), '…', 12)`

// noSnippets stands in for snippetCols in an unfiltered list.
const noSnippets = `'', '', '', ''`

// matchOf picks the snippet a row shows: the first of the quote, comment,
// note and review snippets that holds a match. snippet() returns a
// column's opening words even when it didn't match, so a column matched
// only if its snippet has a marker. None is MatchBook, with no snippet.
func matchOf(quote, comment, note, review string) (MatchIn, string) {
	for _, c := range []struct {
		in      MatchIn
		snippet string
	}{{MatchQuote, quote}, {MatchComment, comment}, {MatchNote, note}, {MatchReview, review}} {
		if strings.Contains(c.snippet, SnippetOpen) {
			return c.in, c.snippet
		}
	}
	return MatchBook, ""
}
