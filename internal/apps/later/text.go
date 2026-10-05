package later

import (
	"html"
	"math"
	"regexp"
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// plainText concatenates the text nodes of an HTML fragment, writing sep
// after every element. With sep "" it is exactly the browser's textContent
// of the rendered fragment.
func plainText(fragment, sep string) string {
	ctx := &xhtml.Node{Type: xhtml.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := xhtml.ParseFragment(strings.NewReader(fragment), ctx)
	if err != nil {
		return ""
	}
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == xhtml.ElementNode {
			b.WriteString(sep)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return b.String()
}

// ContentText is the text highlight offsets (L2) index into: the snapshot's
// text nodes, concatenated, exactly as a browser's textContent reports them.
// Keeping it that simple is what lets the client compute the same offsets
// without mirroring any rules.
func ContentText(fragment string) string { return plainText(fragment, "") }

// WordCount counts words with element boundaries as separators, so the end
// of one paragraph and the start of the next are two words, not one.
func WordCount(fragment string) int { return len(strings.Fields(plainText(fragment, " "))) }

// wordsPerMinute is a comfortable reading pace for considered reading.
const wordsPerMinute = 230

// ReadingMinutes is the reading time shown in lists and the reader.
func ReadingMinutes(words int) int {
	return max(1, int(math.Ceil(float64(words)/wordsPerMinute)))
}

var blankLines = regexp.MustCompile(`\n\s*\n`)

// PastedHTML turns text the user pasted into a snapshot: one <p> per
// paragraph (blank-line separated), everything escaped. Nothing in it needs
// sanitising, because none of it is markup.
func PastedHTML(text string) string {
	var b strings.Builder
	for _, para := range blankLines.Split(strings.ReplaceAll(text, "\r\n", "\n"), -1) {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		b.WriteString("<p>")
		b.WriteString(html.EscapeString(para))
		b.WriteString("</p>")
	}
	return b.String()
}
