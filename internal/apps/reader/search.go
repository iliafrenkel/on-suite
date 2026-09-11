package reader

import (
	"strings"

	"golang.org/x/net/html"
)

// SearchText turns stored article HTML into the plain text the index holds.
//
// Tags become word boundaries rather than disappearing: without that,
// "<p>Hello</p><p>world</p>" indexes as one token and neither word is findable.
func SearchText(fragment string) string {
	if strings.TrimSpace(fragment) == "" {
		return ""
	}
	doc, err := html.Parse(strings.NewReader(fragment))
	if err != nil {
		// Unreachable in practice — html.Parse does not fail on arbitrary
		// input — but returning the raw string here would index markup, so
		// return nothing instead and let the reindex pass try again.
		return ""
	}

	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			return
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style":
				// Neither survives the sanitizer, but indexing either would be
				// nonsense if one ever did.
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			b.WriteByte(' ')
		}
	}
	walk(doc)

	return strings.Join(strings.Fields(b.String()), " ")
}

// ftsQuery turns free text into an FTS5 MATCH expression that can never be a
// syntax error.
//
// Mirrors ON Notes' own ftsQuery (internal/apps/notes/search.go) — copied
// rather than shared because apps never import each other; see "Cross-app
// mirroring" in PATTERNS.md.
//
// Each word becomes its own quoted phrase, doubling any embedded quote, so
// anything a person types — an operator like AND, a bare quote, a parenthesis
// — lands inside the quotes as inert phrase text instead of breaking the
// query. The trailing * is FTS5's prefix operator, which is what makes a live
// filter match while a word is still being typed.
func ftsQuery(q string) string {
	words := strings.Fields(q)
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"*`
	}
	return strings.Join(quoted, " ")
}
