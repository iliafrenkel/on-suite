package later

import "strings"

// snippetPart is a run of a search snippet; Hit runs are drawn as <mark>.
// Rendering parts, rather than converting the snippet to template.HTML,
// keeps every character escaped by html/template.
type snippetPart struct {
	Text string
	Hit  bool
}

// snippetParts splits a Search snippet at its SnippetOpen/SnippetClose
// markers, collapsing whitespace (the highlights column separates entries
// with newlines). An unclosed marker runs to the end.
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

// snippetView is the line under a search result saying where it matched.
type snippetView struct {
	In    string
	Parts []snippetPart
}

var matchLabels = map[MatchIn]string{
	MatchHighlight: "In a highlight:",
	MatchNote:      "In your note:",
	MatchText:      "In the text:",
}

// newSnippet is h's snippet line, or nil for a title-only match.
func newSnippet(h SearchHit) *snippetView {
	if h.In == MatchTitle {
		return nil
	}
	return &snippetView{In: matchLabels[h.In], Parts: snippetParts(h.Snippet)}
}
