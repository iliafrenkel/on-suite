// Package htmlassert parses HTML in tests and asserts on its structure.
//
// It exists so handler tests never compare markup as strings. A string
// comparison breaks whenever a class name or whitespace changes, which trains
// you to ignore failures; a structural assertion breaks only when the meaning
// changes.
//
// This package must only ever be imported from _test.go files. The
// architecture test in internal/arch enforces that.
package htmlassert

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// Doc is a parsed document.
type Doc struct {
	t    *testing.T
	root *html.Node
}

// Parse parses body, failing the test if it is not valid HTML.
func Parse(t *testing.T, body string) *Doc {
	t.Helper()
	root, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("htmlassert: parse: %v", err)
	}
	return &Doc{t: t, root: root}
}

// Query returns the first element matching the selector, or nil.
//
// Supported: "tag", ".class", "#id", "[attr]", "[attr=value]", a tag with one
// of those appended (`a.shell-mark`, `input[name=user]`), and descendant
// combinations separated by spaces (`nav.shell-nav a`).
func (d *Doc) Query(selector string) *html.Node {
	matches := d.QueryAll(selector)
	if len(matches) == 0 {
		return nil
	}
	return matches[0]
}

// QueryAll returns every element matching the selector, in document order.
func (d *Doc) QueryAll(selector string) []*html.Node {
	parts := strings.Fields(selector)
	if len(parts) == 0 {
		d.t.Fatal("htmlassert: empty selector")
	}
	compiled := make([]matcher, len(parts))
	for i, part := range parts {
		compiled[i] = parseSelector(d.t, part)
	}
	last := compiled[len(compiled)-1]
	ancestors := compiled[:len(compiled)-1]

	// One walk in document order, so results are ordered without sorting.
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && last.matches(n) && ancestorsSatisfied(n.Parent, ancestors) {
			out = append(out, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(d.root)
	return out
}

// ancestorsSatisfied walks up from n looking for the ancestor selectors in
// right-to-left order, the way a CSS descendant combinator resolves.
func ancestorsSatisfied(n *html.Node, sels []matcher) bool {
	if len(sels) == 0 {
		return true
	}
	i := len(sels) - 1
	for cur := n; cur != nil; cur = cur.Parent {
		if cur.Type == html.ElementNode && sels[i].matches(cur) {
			i--
			if i < 0 {
				return true
			}
		}
	}
	return false
}

// MustHave asserts at least one element matches, and returns the first.
func (d *Doc) MustHave(selector string) *html.Node {
	d.t.Helper()
	n := d.Query(selector)
	if n == nil {
		d.t.Fatalf("htmlassert: no element matches %q", selector)
	}
	return n
}

// MustNotHave asserts nothing matches. Used for the negative cases that
// matter most: a logged-out page must not contain a logout button.
func (d *Doc) MustNotHave(selector string) {
	d.t.Helper()
	if n := d.Query(selector); n != nil {
		d.t.Fatalf("htmlassert: %q matched but should not exist (found <%s>)", selector, n.Data)
	}
}

// Text is the concatenated text content of a node, with runs of whitespace
// collapsed so assertions do not depend on template indentation.
func Text(n *html.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

// Attr returns an attribute value and whether it was present.
func Attr(n *html.Node, name string) (string, bool) {
	if n == nil {
		return "", false
	}
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}

// Text on the document is shorthand for the whole body.
func (d *Doc) Text() string { return Text(d.root) }

// matcher is one compiled selector component.
type matcher struct {
	tag   string
	attr  string
	value string
	kind  selectorKind
}

type selectorKind int

const (
	byTag selectorKind = iota
	byClass
	byID
	byAttrPresent
	byAttrValue
)

func parseSelector(t *testing.T, selector string) matcher {
	t.Helper()
	s := strings.TrimSpace(selector)
	if s == "" {
		t.Fatal("htmlassert: empty selector")
	}

	// Split an optional leading tag name from the qualifier.
	i := strings.IndexAny(s, ".#[")
	if i < 0 {
		return matcher{tag: s, kind: byTag}
	}
	tag, qualifier := s[:i], s[i:]

	switch {
	case strings.HasPrefix(qualifier, "."):
		return matcher{tag: tag, attr: "class", value: qualifier[1:], kind: byClass}
	case strings.HasPrefix(qualifier, "#"):
		return matcher{tag: tag, attr: "id", value: qualifier[1:], kind: byID}
	case strings.HasPrefix(qualifier, "[") && strings.HasSuffix(qualifier, "]"):
		inner := qualifier[1 : len(qualifier)-1]
		name, val, found := strings.Cut(inner, "=")
		if !found {
			return matcher{tag: tag, attr: name, kind: byAttrPresent}
		}
		return matcher{tag: tag, attr: name, value: strings.Trim(val, `"'`), kind: byAttrValue}
	}
	t.Fatalf("htmlassert: unsupported selector %q", selector)
	return matcher{}
}

func (m matcher) matches(n *html.Node) bool {
	if m.tag != "" && n.Data != m.tag {
		return false
	}
	switch m.kind {
	case byTag:
		return true
	case byClass:
		classes, _ := Attr(n, "class")
		for _, c := range strings.Fields(classes) {
			if c == m.value {
				return true
			}
		}
		return false
	case byID, byAttrValue:
		got, ok := Attr(n, m.attr)
		return ok && got == m.value
	case byAttrPresent:
		_, ok := Attr(n, m.attr)
		return ok
	}
	return false
}
