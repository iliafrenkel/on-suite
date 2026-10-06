package later

import (
	"regexp"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// mdConverter turns a sanitised snapshot into Markdown for "Download as
// Markdown" (spec: "Export"). It handles exactly what
// internal/platform/article's sanitiser lets through — headings,
// paragraphs, lists, blockquotes, links, emphasis, code, tables and images
// as links to their source. Anything else contributes its text and nothing
// more. Hand-written on purpose: the spec rules out a new dependency.
type mdConverter struct {
	// image maps an <img src> to the URL the Markdown links to; "" keeps
	// only the alt text.
	image func(src string) string
	// shift moves headings down this many levels (capped at ######), so an
	// article's <h1> sits under the downloaded file's own headings.
	shift int
	// inLink is set while rendering a link's children: CommonMark forbids
	// links inside links, so an image there renders as its alt text only.
	inLink bool
}

// htmlToMarkdown converts a sanitised HTML fragment to Markdown blocks
// separated by blank lines, without a trailing newline.
func htmlToMarkdown(fragment string, image func(src string) string, shift int) string {
	ctx := &xhtml.Node{Type: xhtml.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := xhtml.ParseFragment(strings.NewReader(fragment), ctx)
	if err != nil {
		return ""
	}
	root := &xhtml.Node{Type: xhtml.ElementNode, Data: "body", DataAtom: atom.Body}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	c := mdConverter{image: image, shift: shift}
	return strings.Join(c.blocks(root), "\n\n")
}

// mdIsBlock is text.go's block set, minus <br>: here a <br> is a line
// break inside a paragraph, not the end of one.
func mdIsBlock(n *xhtml.Node) bool {
	return n.Type == xhtml.ElementNode && n.DataAtom != atom.Br && isBlockElement(n.DataAtom)
}

// blocks renders parent's children as Markdown blocks. Each run of inline
// content between block elements becomes one paragraph.
func (c mdConverter) blocks(parent *xhtml.Node) []string {
	var out []string
	var run strings.Builder
	flush := func() {
		if p := mdParagraph(run.String()); p != "" {
			out = append(out, p)
		}
		run.Reset()
	}
	for n := parent.FirstChild; n != nil; n = n.NextSibling {
		if mdIsBlock(n) {
			flush()
			out = append(out, c.block(n)...)
			continue
		}
		run.WriteString(c.inline(n))
	}
	flush()
	return out
}

func (c mdConverter) block(n *xhtml.Node) []string {
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		text := mdOneLine(c.inlineChildren(n))
		if text == "" {
			return nil
		}
		level := min(6, int(n.Data[1]-'0')+c.shift)
		return []string{strings.Repeat("#", level) + " " + text}
	case atom.Hr:
		return []string{"---"}
	case atom.Pre:
		return []string{mdFence(mdText(n))}
	case atom.Blockquote:
		inner := strings.Join(c.blocks(n), "\n\n")
		if inner == "" {
			return nil
		}
		return []string{mdPrefix(inner, "> ", ">")}
	case atom.Ul, atom.Ol:
		return c.list(n)
	case atom.Dt:
		text := mdOneLine(c.inlineChildren(n))
		if text == "" {
			return nil
		}
		return []string{"**" + text + "**"}
	case atom.Table:
		return c.table(n)
	default: // p, div, figure, figcaption, dd, an <li> or <tr> outside its parent…
		return c.blocks(n)
	}
}

// list renders <ul>/<ol>. Only <li> children count: the sanitiser's output
// has nothing else there but whitespace.
func (c mdConverter) list(n *xhtml.Node) []string {
	var items []string
	num := 0
	for li := n.FirstChild; li != nil; li = li.NextSibling {
		if li.Type != xhtml.ElementNode || li.DataAtom != atom.Li {
			continue
		}
		marker := "- "
		if n.DataAtom == atom.Ol {
			num++
			marker = strconv.Itoa(num) + ". "
		}
		body := strings.Join(c.blocks(li), "\n\n")
		items = append(items, strings.TrimRight(marker+mdIndent(body, len(marker)), " "))
	}
	if len(items) == 0 {
		return nil
	}
	return []string{strings.Join(items, "\n")}
}

// table renders a GFM pipe table, the first row as its header, with the
// caption (if any) as a paragraph before it.
func (c mdConverter) table(n *xhtml.Node) []string {
	var out []string
	var rows [][]string
	var walk func(*xhtml.Node)
	walk = func(p *xhtml.Node) {
		for ch := p.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type != xhtml.ElementNode {
				continue
			}
			switch ch.DataAtom {
			case atom.Caption:
				out = append(out, c.blocks(ch)...)
			case atom.Thead, atom.Tbody, atom.Tfoot:
				walk(ch)
			case atom.Tr:
				var cells []string
				for td := ch.FirstChild; td != nil; td = td.NextSibling {
					if td.Type == xhtml.ElementNode && (td.DataAtom == atom.Td || td.DataAtom == atom.Th) {
						cell := mdOneLine(strings.ReplaceAll(strings.Join(c.blocks(td), " "), "\\\n", " "))
						cells = append(cells, strings.ReplaceAll(cell, "|", `\|`))
					}
				}
				rows = append(rows, cells)
			}
		}
	}
	walk(n)

	width := 0
	for _, r := range rows {
		width = max(width, len(r))
	}
	if width == 0 {
		return out
	}
	line := func(cells []string) string {
		for len(cells) < width {
			cells = append(cells, "")
		}
		return "| " + strings.Join(cells, " | ") + " |"
	}
	lines := []string{line(rows[0]), "|" + strings.Repeat(" --- |", width)}
	for _, r := range rows[1:] {
		lines = append(lines, line(r))
	}
	return append(out, strings.Join(lines, "\n"))
}

// spaceRun is HTML's collapsible whitespace. Text nodes collapse it to one
// space so that only a <br> starts a new line in a paragraph.
var spaceRun = regexp.MustCompile(`\s+`)

func (c mdConverter) inline(n *xhtml.Node) string {
	switch n.Type {
	case xhtml.TextNode:
		return mdEscape(spaceRun.ReplaceAllString(n.Data, " "))
	case xhtml.ElementNode:
	default:
		return ""
	}
	switch n.DataAtom {
	case atom.Br:
		return "\n"
	case atom.Img:
		alt := mdEscape(strings.Join(strings.Fields(mdAttr(n, "alt")), " "))
		if c.inLink {
			if alt == "" {
				alt = "Image"
			}
			return alt
		}
		href := c.image(mdAttr(n, "src"))
		if href == "" {
			return alt
		}
		if alt == "" {
			alt = "Image"
		}
		return "[" + alt + "](" + mdLink(href) + ")"
	case atom.A:
		inner := c
		inner.inLink = true
		text := inner.inlineChildren(n)
		href := mdAttr(n, "href")
		if strings.TrimSpace(text) == "" || href == "" {
			return text
		}
		return "[" + text + "](" + mdLink(href) + ")"
	case atom.Em, atom.I:
		return mdWrap(c.inlineChildren(n), "*")
	case atom.Strong, atom.B:
		return mdWrap(c.inlineChildren(n), "**")
	case atom.S, atom.Del:
		return mdWrap(c.inlineChildren(n), "~~")
	case atom.Code, atom.Kbd, atom.Samp, atom.Var:
		return mdCodeSpan(mdText(n))
	case atom.Q:
		return "“" + c.inlineChildren(n) + "”"
	default: // u, ins, sub, sup, small, mark, cite, abbr, time, span, and blocks inside inline
		return c.inlineChildren(n)
	}
}

func (c mdConverter) inlineChildren(n *xhtml.Node) string {
	var b strings.Builder
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		b.WriteString(c.inline(ch))
	}
	return b.String()
}

func mdAttr(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// mdText is n's text, raw: for code, which is never escaped.
func mdText(n *xhtml.Node) string {
	if n.Type == xhtml.TextNode {
		return n.Data
	}
	var b strings.Builder
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		b.WriteString(mdText(ch))
	}
	return b.String()
}

var mdEscaper = strings.NewReplacer(`\`, `\\`, "`", "\\`", `*`, `\*`, `_`, `\_`,
	`[`, `\[`, `]`, `\]`, `<`, `\<`, `~`, `\~`)

// mdEscape makes plain text literal inside Markdown. Characters that only
// matter at the start of a line are mdLineStart's job.
func mdEscape(s string) string { return mdEscaper.Replace(s) }

var mdOrderedStart = regexp.MustCompile(`^\d+[.)](\s|$)`)

// mdLineStart escapes what would turn a line into a heading, quote, list,
// setext underline or table row.
func mdLineStart(l string) string {
	if mdOrderedStart.MatchString(l) {
		i := strings.IndexAny(l, ".)")
		return l[:i] + `\` + l[i:]
	}
	switch l[0] {
	case '#', '>', '-', '+', '=', '|':
		return `\` + l
	}
	return l
}

// mdParagraph tidies a run of inline Markdown into one paragraph.
func mdParagraph(run string) string {
	var lines []string
	for _, l := range strings.Split(run, "\n") {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			// A literal "[" is always escaped, so "![" can only be a "!" in
			// the text right before a link, which Markdown would show as an
			// image.
			l = strings.ReplaceAll(l, "![", "\\![")
			lines = append(lines, mdLineStart(l))
		}
	}
	return strings.Join(lines, "\\\n")
}

// mdOneLine collapses inline Markdown onto one line, for headings and cells.
func mdOneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// mdWrap puts mark around s, keeping s's outer spaces outside it, since
// "* word*" is not emphasis.
func mdWrap(s, mark string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return s
	}
	lead := s[:strings.Index(s, t)]
	return lead + mark + t + mark + s[len(lead)+len(t):]
}

// mdCodeSpan fences inline code with one more backtick than its longest run.
func mdCodeSpan(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}

// mdFence is a fenced code block, its fence longer than any run inside.
func mdFence(code string) string {
	code = strings.TrimSuffix(code, "\n")
	fence := "```"
	for strings.Contains(code, fence) {
		fence += "`"
	}
	return fence + "\n" + code + "\n" + fence
}

// mdLink makes a URL safe inside Markdown's (…). The sanitiser has already
// limited links to http, https and mailto.
var mdLink = strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E").Replace

// mdIndent indents every line after the first by n spaces (empty lines stay
// empty), for a list item's continuation.
func mdIndent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] != "" {
			lines[i] = pad + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// mdPrefix prefixes every line, using emptyPrefix for empty ones.
func mdPrefix(s, prefix, emptyPrefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = emptyPrefix
		} else {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}
