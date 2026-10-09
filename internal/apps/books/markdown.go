package books

import (
	"html"
	"html/template"
	"regexp"
	"strings"
)

// codePattern finds `code` spans, whose content is verbatim.
var codePattern = regexp.MustCompile("`([^`]+)`")

// inlinePattern is every other inline construct, tried in this order at
// each position: bold before italic, or "**bold**" would read as an italic
// span starting one character in. RE2, so no input can make it backtrack.
var inlinePattern = regexp.MustCompile(
	`\*\*([^*]+)\*\*` + // 1: bold
		`|\*([^*]+)\*` + // 2: italic
		`|~~([^~]+)~~` + // 3: strike
		`|\[([^\]]+)\]\(([^)]+)\)` + // 4,5: link text, url
		`|(https?://(?:\([^\s<>"')\]]*\)|[^\s<>"')\]])+)`, // 6: bare link
)

// RenderReview turns a review's Markdown into HTML (spec "Data model":
// review is Markdown). Inline, it mirrors ON Notes' renderer
// (internal/apps/notes/markdown.go) without Notes' #tag chips — apps never
// import each other, so this is an independent copy: **bold**, *italic*,
// `code`, ~~strike~~, [text](url) and bare http(s) links. On top of that a
// blank line starts a new paragraph and a single line break stays one.
// Everything else is literal text.
//
// The output is built from html.EscapeString-escaped pieces and a fixed
// set of tags, never from the input directly, so no review can inject
// markup.
func RenderReview(s string) template.HTML {
	var b strings.Builder
	for _, para := range paragraphs(s) {
		b.WriteString("<p>")
		for i, line := range para {
			if i > 0 {
				b.WriteString("<br>")
			}
			renderCodeSpans(&b, line)
		}
		b.WriteString("</p>")
	}
	return template.HTML(b.String())
}

// paragraphs splits s into runs of non-blank lines.
func paragraphs(s string) [][]string {
	var out [][]string
	var cur []string
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			if cur != nil {
				out, cur = append(out, cur), nil
			}
			continue
		}
		cur = append(cur, line)
	}
	if cur != nil {
		out = append(out, cur)
	}
	return out
}

// renderCodeSpans renders `code` spans verbatim and everything between
// them through renderInline.
func renderCodeSpans(b *strings.Builder, s string) {
	last := 0
	for _, m := range codePattern.FindAllStringSubmatchIndex(s, -1) {
		renderInline(b, s[last:m[0]])
		b.WriteString("<code>")
		b.WriteString(html.EscapeString(s[m[2]:m[3]]))
		b.WriteString("</code>")
		last = m[1]
	}
	renderInline(b, s[last:])
}

// renderInline handles everything inlinePattern matches in one
// left-to-right pass, escaping the literal text in between.
func renderInline(b *strings.Builder, s string) {
	last := 0
	for _, m := range inlinePattern.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(html.EscapeString(s[last:m[0]]))
		switch {
		case m[2] >= 0:
			b.WriteString("<strong>" + html.EscapeString(s[m[2]:m[3]]) + "</strong>")
		case m[4] >= 0:
			b.WriteString("<em>" + html.EscapeString(s[m[4]:m[5]]) + "</em>")
		case m[6] >= 0:
			b.WriteString("<s>" + html.EscapeString(s[m[6]:m[7]]) + "</s>")
		case m[8] >= 0:
			writeLink(b, s[m[8]:m[9]], s[m[10]:m[11]], s[m[0]:m[1]])
		case m[12] >= 0:
			writeLink(b, s[m[12]:m[13]], s[m[12]:m[13]], s[m[12]:m[13]])
		}
		last = m[1]
	}
	b.WriteString(html.EscapeString(s[last:]))
}

// writeLink is the only place a link is made: http and https only, opened
// in a new tab without a way back (noopener). Any other scheme —
// javascript: above all — is shown as the source text, as typed.
func writeLink(b *strings.Builder, text, href, source string) {
	scheme := strings.ToLower(href)
	if !strings.HasPrefix(scheme, "http://") && !strings.HasPrefix(scheme, "https://") {
		b.WriteString(html.EscapeString(source))
		return
	}
	b.WriteString(`<a href="` + html.EscapeString(href) + `" target="_blank" rel="noopener noreferrer">`)
	b.WriteString(html.EscapeString(text) + `</a>`)
}
