package notes

import (
	"bytes"
	"html"
	"html/template"
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// span is a byte-offset match, [start, end), into the text it was found in.
type span struct{ start, end int }

// matchSpans finds every case-insensitive, non-overlapping occurrence of
// any of terms in text, using strings.EqualFold rather than a
// lowercase-and-compare: some Unicode case folds change a string's byte
// length (ß, Kelvin sign, ...), and slicing text at offsets computed from a
// separately-lowercased copy would then point at the wrong bytes.
// EqualFold's own rune-by-rune comparison has no such mismatch, at the cost
// of an extremely rare, harmless false negative: a term that only matches
// via a length-changing fold at a position that also shifts the term's own
// byte length (no realistic note content exercises this).
//
// terms is assumed already lower-cased by the caller (searchTerms's output
// is not — callers pass it through here as-is; matching against it with
// EqualFold makes that unnecessary, since EqualFold ignores case on both
// sides). Where two terms both match at the same position (e.g. "cat" and
// "category"), the longest wins, so a query for both can never produce a
// double-wrapped or truncated span for the same text.
//
// Every match FTS5 finds via matchedIDsSubquery corresponds, for this
// project's tokenize='unicode61' with no diacritic folding, to a real
// contiguous substring of the original text — so a row the database
// selected as a match will always have at least one span here too.
func matchSpans(text string, terms []string) []span {
	if len(terms) == 0 {
		return nil
	}
	var spans []span
	for i := 0; i < len(text); {
		matchLen := 0
		for _, term := range terms {
			if term == "" {
				continue
			}
			end := i + len(term)
			if end <= len(text) && strings.EqualFold(text[i:end], term) && len(term) > matchLen {
				matchLen = len(term)
			}
		}
		if matchLen == 0 {
			i++
			continue
		}
		spans = append(spans, span{i, i + matchLen})
		i += matchLen
	}
	return spans
}

// highlight wraps every match in rendered — HTML already produced by
// Render — in <mark class="notes-search-hit">. It parses rendered as an
// HTML fragment, walks it, and re-serializes it, touching only text nodes:
// a match can never land inside a tag name or an attribute value (a link's
// href, for instance), the way a plain string-replace risks.
//
// This runs on already-rendered HTML rather than on raw Markdown source
// deliberately: a term wrapped in <mark> before Markdown rendering could
// straddle a **bold**/`code` delimiter and stop that construct from
// triggering at all. Doing it after, on the rendered tree's own text,
// cannot corrupt markup Render has already decided on — see this feature's
// design doc, docs/superpowers/specs/2026-09-07-on-notes-search-filter-design.md.
func highlight(rendered template.HTML, terms []string) template.HTML {
	if len(terms) == 0 {
		return rendered
	}

	nodes, err := xhtml.ParseFragment(strings.NewReader(string(rendered)),
		&xhtml.Node{Type: xhtml.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		// rendered is always Render's own output — well-formed by
		// construction — so a parse failure here would be a bug in Render,
		// not bad user input. Falling back to the unhighlighted original is
		// safer than losing the row's content entirely.
		return rendered
	}

	// Wrap fragment nodes in a temporary container so they have a parent
	// for the InsertBefore operations in splitTextNode: ParseFragment can
	// return a bare text node at the top level (e.g. rendered is plain text
	// with no wrapping tag at all), and such a node's own .Parent is nil —
	// splitTextNode's parent.InsertBefore(...) would nil-pointer-panic on
	// it without something to attach it to first. container is never
	// itself rendered into the output below; it exists only to give every
	// top-level node a real parent.
	container := &xhtml.Node{Type: xhtml.ElementNode, Data: "div"}
	for _, n := range nodes {
		container.AppendChild(n)
	}

	for c := container.FirstChild; c != nil; {
		next := c.NextSibling
		highlightNode(c, terms)
		c = next
	}

	var buf bytes.Buffer
	for c := container.FirstChild; c != nil; c = c.NextSibling {
		if err := xhtml.Render(&buf, c); err != nil {
			return rendered
		}
	}
	return template.HTML(buf.String())
}

// highlightNode walks n and its descendants, splicing <mark> elements in
// place of any text node that contains a match.
func highlightNode(n *xhtml.Node, terms []string) {
	if n.Type == xhtml.TextNode {
		splitTextNode(n, terms)
		return
	}
	// A child's own NextSibling changes as splitTextNode inserts nodes
	// beside it, so the next pointer is captured before recursing into it.
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		highlightNode(c, terms)
		c = next
	}
}

// splitTextNode replaces text node n with a run of text and <mark>
// siblings wherever matchSpans finds a match, then removes n itself.
func splitTextNode(n *xhtml.Node, terms []string) {
	text := n.Data
	spans := matchSpans(text, terms)
	if len(spans) == 0 {
		return
	}

	parent, before := n.Parent, n
	insert := func(newNode *xhtml.Node) { parent.InsertBefore(newNode, before) }

	last := 0
	for _, sp := range spans {
		if sp.start > last {
			insert(&xhtml.Node{Type: xhtml.TextNode, Data: text[last:sp.start]})
		}
		// DataAtom is left at its zero value: "mark" has no entry in
		// golang.org/x/net/html/atom's table (generated only from the tags
		// its parser needs to recognize by fast lookup), but html.Render
		// serializes an element from its Data string, not DataAtom — so an
		// unset DataAtom here does not affect the output.
		mark := &xhtml.Node{Type: xhtml.ElementNode, Data: "mark",
			Attr: []xhtml.Attribute{{Key: "class", Val: "notes-search-hit"}}}
		mark.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: text[sp.start:sp.end]})
		insert(mark)
		last = sp.end
	}
	if last < len(text) {
		insert(&xhtml.Node{Type: xhtml.TextNode, Data: text[last:]})
	}
	parent.RemoveChild(n)
}

// highlightPlainText escapes text and wraps every match in
// <mark class="notes-search-hit"> — the Due/Archive row title's own
// version of highlight, simpler than that one because there is no existing
// markup to preserve: a Due/Archive row's title is deliberately never run
// through Render (DisplayTitleHTML's own doc comment explains why — nesting
// an <a> the title's Markdown might itself produce, inside the <a> the row
// already is, is invalid HTML), so this only ever emits escaped text and
// <mark>, never anything Render would have produced.
func highlightPlainText(text string, terms []string) template.HTML {
	spans := matchSpans(text, terms)
	if len(spans) == 0 {
		return template.HTML(html.EscapeString(text))
	}
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(html.EscapeString(text[last:sp.start]))
		b.WriteString(`<mark class="notes-search-hit">`)
		b.WriteString(html.EscapeString(text[sp.start:sp.end]))
		b.WriteString(`</mark>`)
		last = sp.end
	}
	b.WriteString(html.EscapeString(text[last:]))
	return template.HTML(b.String())
}

// snippetContextRunes bounds how much raw text surrounds the first match in
// a Due/Archive row's note-only snippet (issue #86: some sign of why a row
// with no title match is in the results) — a short excerpt, not the note.
const snippetContextRunes = 40

// noteSnippet extracts a window of raw text around the first match of any
// term in note, renders it (spec's Markdown pipeline) and highlights it —
// empty when nothing in note matches, which is also the caller's own signal
// that this row's match was in the title instead, and no snippet is needed.
func noteSnippet(note string, terms []string) template.HTML {
	spans := matchSpans(note, terms)
	if len(spans) == 0 {
		return ""
	}

	runes := []rune(note)
	matchStartRune := len([]rune(note[:spans[0].start]))
	matchEndRune := len([]rune(note[:spans[0].end]))

	start := matchStartRune - snippetContextRunes
	truncatedBefore := start > 0
	if start < 0 {
		start = 0
	}
	end := matchEndRune + snippetContextRunes
	truncatedAfter := end < len(runes)
	if end > len(runes) {
		end = len(runes)
	}

	window := string(runes[start:end])
	if truncatedBefore {
		window = "…" + window
	}
	if truncatedAfter {
		window += "…"
	}
	return highlight(Render(window), terms)
}
