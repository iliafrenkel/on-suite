package later

import (
	"sort"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// RenderHighlights returns fragment with every highlight that still matches
// text wrapped in <mark> elements, so a highlighted article renders with no
// JavaScript (spec: "Reading view"). It walks the same parse ContentText
// does, so the text it counts is exactly the text the offsets index into.
// A highlight crossing element boundaries becomes one <mark> per text node
// it touches; the first carries id="later-h-{ID}" for links to it.
func RenderHighlights(fragment, text string, hs []Highlight) string {
	spans := drawable(text, hs)
	if len(spans) == 0 {
		return fragment
	}
	ctx := &xhtml.Node{Type: xhtml.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := xhtml.ParseFragment(strings.NewReader(fragment), ctx)
	if err != nil {
		return fragment
	}
	// A holder, so top-level text nodes have a parent to be split inside.
	root := &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	m := marker{spans: spans, started: map[int64]bool{}}
	m.walk(root)
	var b strings.Builder
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if err := xhtml.Render(&b, c); err != nil {
			return fragment
		}
	}
	return b.String()
}

// drawable is hs that still match text, in text order, without overlaps
// (the store refuses those; this keeps a bad row from breaking the page).
func drawable(text string, hs []Highlight) []Highlight {
	runes := []rune(text)
	var out []Highlight
	for _, h := range hs {
		if validSpan(runes, h.Start, h.End, h.Quote) {
			out = append(out, h)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	kept := out[:0]
	for _, h := range out {
		if len(kept) > 0 && h.Start < kept[len(kept)-1].End {
			continue
		}
		kept = append(kept, h)
	}
	return kept
}

type marker struct {
	spans   []Highlight
	pos     int // code points of text seen so far
	started map[int64]bool
}

func (m *marker) walk(n *xhtml.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling // c may be replaced
		if c.Type == xhtml.TextNode {
			m.text(c)
		} else {
			m.walk(c)
		}
		c = next
	}
}

// tableSections hold only whitespace text after parsing; a <mark> there
// would be foster-parented out of the table when the browser re-parses.
var tableSections = map[atom.Atom]bool{
	atom.Table: true, atom.Thead: true, atom.Tbody: true, atom.Tfoot: true, atom.Tr: true, atom.Colgroup: true,
}

// text splits one text node into plain and marked pieces.
func (m *marker) text(t *xhtml.Node) {
	runes := []rune(t.Data)
	start, end := m.pos, m.pos+len(runes)
	m.pos = end
	if tableSections[t.Parent.DataAtom] {
		return
	}
	var pieces []*xhtml.Node
	cur := start
	for _, h := range m.spans {
		if h.End <= cur || h.Start >= end {
			continue
		}
		s, e := max(h.Start, cur), min(h.End, end)
		if s > cur {
			pieces = append(pieces, &xhtml.Node{Type: xhtml.TextNode, Data: string(runes[cur-start : s-start])})
		}
		pieces = append(pieces, markNode(h, string(runes[s-start:e-start]), !m.started[h.ID], e == h.End))
		m.started[h.ID] = true
		cur = e
	}
	if len(pieces) == 0 {
		return
	}
	if cur < end {
		pieces = append(pieces, &xhtml.Node{Type: xhtml.TextNode, Data: string(runes[cur-start:])})
	}
	for _, p := range pieces {
		t.Parent.InsertBefore(p, t)
	}
	t.Parent.RemoveChild(t)
}

func markNode(h Highlight, text string, first, last bool) *xhtml.Node {
	class := "later-hl"
	if h.Comment != "" {
		class += " later-hl-commented"
	}
	if last {
		class += " later-hl-end"
	}
	attrs := []xhtml.Attribute{{Key: "class", Val: class}, {Key: "data-highlight-id", Val: strconv.FormatInt(h.ID, 10)}}
	if first {
		attrs = append(attrs, xhtml.Attribute{Key: "id", Val: "later-h-" + strconv.FormatInt(h.ID, 10)})
	}
	n := &xhtml.Node{Type: xhtml.ElementNode, Data: "mark", DataAtom: atom.Mark, Attr: attrs}
	n.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: text})
	return n
}
