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
// markers, collapsing whitespace. An unclosed marker runs to the end.
//
// Books differs from Later here: the notes, quotes and comments columns
// join their entries with newlines, so a snippet can run from one entry
// into the next. Only the line holding the first match is kept, with an
// ellipsis where a line was dropped.
func snippetParts(s string) []snippetPart {
	s = oneEntry(s)
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

// oneEntry keeps the newline-separated line that holds the first
// SnippetOpen, marking dropped lines with an ellipsis (unless the text
// already begins or ends with one). Without a newline, or a marker, it is s itself.
func oneEntry(s string) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	if !strings.Contains(s, SnippetOpen) {
		return s
	}
	lines := strings.Split(s, "\n")
	k := 0
	for i, l := range lines {
		if strings.Contains(l, SnippetOpen) {
			k = i
			break
		}
	}
	line := lines[k]
	if k > 0 && !strings.HasPrefix(strings.TrimSpace(line), "…") {
		line = "…" + line
	}
	if k < len(lines)-1 && !strings.HasSuffix(strings.TrimSpace(line), "…") {
		line += "…"
	}
	return line
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

// newSnippet is a row's snippet line, or nil when the filter matched only
// what the row shows.
func newSnippet(it ListItem) *snippetView {
	if it.Match == MatchBook {
		return nil
	}
	return &snippetView{In: matchLabels[it.Match], Parts: snippetParts(it.Snippet)}
}
