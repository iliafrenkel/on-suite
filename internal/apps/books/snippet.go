package books

import "strings"

// snippetPart is a run of a search snippet; Hit runs are drawn as <mark>.
// Rendering parts, rather than converting the snippet to template.HTML,
// keeps every character escaped by html/template.
//
// Mirrors ON Later's snippetParts (internal/apps/later/snippet.go); apps
// never import each other, so this is an independent copy.
type snippetPart struct {
	Text string
	Hit  bool
}

// snippetParts splits a List snippet at its SnippetOpen/SnippetClose
// markers, collapsing whitespace (the notes, quotes and comments columns
// separate entries with newlines). An unclosed marker runs to the end.
func snippetParts(s string) []snippetPart {
	s = strings.Join(strings.Fields(s), " ")
	var parts []snippetPart
	for s != "" {
		i := strings.Index(s, SnippetOpen)
		if i < 0 {
			parts = append(parts, snippetPart{Text: s})
			break
		}
		if i > 0 {
			parts = append(parts, snippetPart{Text: s[:i]})
		}
		s = s[i+len(SnippetOpen):]
		j := strings.Index(s, SnippetClose)
		if j < 0 {
			j = len(s)
		}
		if j > 0 {
			parts = append(parts, snippetPart{Text: s[:j], Hit: true})
		}
		s = s[min(len(s), j+len(SnippetClose)):]
	}
	return parts
}

// snippetView is a filtered row's third line: where the filter matched
// and the words around it (decided 2026-10-09).
type snippetView struct {
	In    string
	Parts []snippetPart
}

var matchLabels = map[MatchIn]string{
	MatchQuote:   "Quote:",
	MatchComment: "Quote comment:",
	MatchNote:    "Note:",
	MatchReview:  "Review:",
}

// newSnippet is it's snippet line, or nil when the filter matched only
// what the row already shows.
func newSnippet(it ListItem) *snippetView {
	if it.Match == MatchBook {
		return nil
	}
	return &snippetView{In: matchLabels[it.Match], Parts: snippetParts(it.Snippet)}
}
