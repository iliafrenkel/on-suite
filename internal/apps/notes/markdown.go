package notes

import (
	"html"
	"html/template"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// codePattern finds `code` spans. Its content is verbatim — spec §10 — so it
// is pulled out and rendered before anything else gets a chance to see it.
var codePattern = regexp.MustCompile("`([^`]+)`")

// inlinePattern covers every other inline construct in spec §10, tried in
// this order at each position — Go's regexp, despite being RE2 (no
// backtracking, so this text can never trigger the catastrophic-backtracking
// blowup a hand-written pattern risks with a backtracking engine), still
// resolves alternation the way a backtracking engine would: leftmost, and
// among equal starting points, the first alternative listed that matches.
// Bold must be listed before italic for exactly that reason, or "**bold**"
// would be read as an italic span starting one character in.
var inlinePattern = regexp.MustCompile(
	`\*\*([^*]+)\*\*` + // 1: bold
		`|\*([^*]+)\*` + // 2: italic
		`|~~([^~]+)~~` + // 3: strike
		`|\[([^\]]+)\]\(([^)]+)\)` + // 4,5: link text, url
		// 6: bare autolink. The body alternates plain characters with whole
		// balanced "(...)" groups, so a URL's own single-level nested paren
		// (Wikipedia/MSDN URLs routinely have one, e.g. .../Foo_(bar)) stays
		// part of the match, while a paren that merely wraps the URL in
		// surrounding prose ("(see https://example.com)") still ends the
		// match at its own unmatched ")", same as before — issue #69.
		`|(https?://(?:\([^\s<>"')\]]*\)|[^\s<>"')\]])+)` +
		`|([#@][\p{L}\p{N}_]+)`, // 7: #tag / @mention
)

// Render turns inline Markdown into HTML — spec §10. Supported: **bold**,
// *italic*, `code`, ~~strike~~, [text](url), bare http(s) autolinks, and
// #tag/@tag chips linking to a literal search. Everything else — block
// constructs, unmatched markers, anything else — is literal text: the tree
// is already the list structure, so block-level Markdown has no meaning
// here.
//
// Output is assembled from html.EscapeString-escaped fragments and a small,
// fixed set of hand-built trusted tags, never from the input directly, so a
// parsing bug can produce wrong-looking output but never inject markup —
// there is no code path from user text to an unescaped byte in the result.
func Render(s string) template.HTML {
	return renderMarkdown(s, true)
}

// RenderShared is Render for the public share page (spec §15: "no link
// leads anywhere into the owner's private tree"). #tag/@mention chips
// still render — they keep their visual styling — but as an inert
// <span class="outline-tag">, not an <a href="/notes/search?...">:
// /notes/search is an authenticated route, so a link there from a page an
// anonymous visitor can reach is a dead end into a login wall, not the
// private tree itself, but it still violates the no-link invariant. Used
// only by share.go's nestShared and handlers.go's viewShared — every other
// caller renders the ordinary, authenticated outline and should keep using
// Render so tags there stay real links into /notes/search.
func RenderShared(s string) template.HTML {
	return renderMarkdown(s, false)
}

// renderMarkdown is Render and RenderShared's shared implementation.
// linkTags controls only how a #tag/@mention chip is rendered (writeTag);
// every other construct is identical either way.
func renderMarkdown(s string, linkTags bool) template.HTML {
	var b strings.Builder
	renderCodeSpans(&b, s, linkTags)
	return template.HTML(b.String())
}

// renderCodeSpans splits out `code` spans and renders everything between
// them through renderInline. A code span's own content never reaches
// renderInline, which is what makes it verbatim.
func renderCodeSpans(b *strings.Builder, s string, linkTags bool) {
	last := 0
	for _, m := range codePattern.FindAllStringSubmatchIndex(s, -1) {
		renderInline(b, s[last:m[0]], linkTags)
		b.WriteString("<code>")
		b.WriteString(html.EscapeString(s[m[2]:m[3]]))
		b.WriteString("</code>")
		last = m[1]
	}
	renderInline(b, s[last:], linkTags)
}

// renderInline handles everything inlinePattern matches, in one left-to-right
// pass, escaping the literal text in between.
func renderInline(b *strings.Builder, s string, linkTags bool) {
	last := 0
	for _, m := range inlinePattern.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(html.EscapeString(s[last:m[0]]))
		switch {
		case m[2] >= 0: // bold
			b.WriteString("<strong>")
			b.WriteString(html.EscapeString(s[m[2]:m[3]]))
			b.WriteString("</strong>")
		case m[4] >= 0: // italic
			b.WriteString("<em>")
			b.WriteString(html.EscapeString(s[m[4]:m[5]]))
			b.WriteString("</em>")
		case m[6] >= 0: // strike
			b.WriteString("<s>")
			b.WriteString(html.EscapeString(s[m[6]:m[7]]))
			b.WriteString("</s>")
		case m[8] >= 0: // [text](url)
			writeLink(b, s[m[8]:m[9]], s[m[10]:m[11]], s[m[0]:m[1]])
		case m[12] >= 0: // bare autolink — always http(s) by construction of
			// its own pattern, so writeLink's rejection branch is dead code
			// here; source is passed for signature uniformity, not because
			// it can be reached.
			writeLink(b, s[m[12]:m[13]], s[m[12]:m[13]], s[m[12]:m[13]])
		case m[14] >= 0: // #tag / @mention
			// Issue #68: RE2 has no lookbehind, so the left-boundary check
			// CommonMark-style flanking would give this construct is done
			// here instead of in inlinePattern. Without it, "#"/"@" preceded
			// by a word character reads as a chip mid-word — most visibly,
			// an email address's domain ("ilia@example.com") renders as a
			// bogus mention.
			if !tagHasLeftBoundary(s, m[14]) {
				b.WriteString(html.EscapeString(s[m[14]:m[15]]))
				break
			}
			writeTag(b, s[m[14]:m[15]], linkTags)
		}
		last = m[1]
	}
	b.WriteString(html.EscapeString(s[last:]))
}

// tagHasLeftBoundary reports whether the rune preceding index i in s allows a
// #tag/@mention to start there: the beginning of the text, or a rune that
// isn't itself a word character. i is always a match start returned by
// inlinePattern, so it never splits a rune.
func tagHasLeftBoundary(s string, i int) bool {
	if i == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
}

// writeLink is the only place that can produce an <a href> to somewhere
// other than this app's own search. A scheme other than http/https —
// javascript: above all — renders as source, the whole matched "[text](url)"
// (or bare URL) exactly as written, rather than any anchor — per spec §10,
// and rather than just the label, which would silently discard the rest of
// what the user typed. target="_blank" with rel="noopener noreferrer" is
// deliberate: a bullet's link is to something outside the outline, and
// noopener prevents the classic tab-nabbing hole where the opened page
// reaches back through window.opener.
func writeLink(b *strings.Builder, text, href, source string) {
	scheme := strings.ToLower(href)
	if !strings.HasPrefix(scheme, "http://") && !strings.HasPrefix(scheme, "https://") {
		b.WriteString(html.EscapeString(source))
		return
	}
	b.WriteString(`<a href="`)
	b.WriteString(html.EscapeString(href))
	b.WriteString(`" target="_blank" rel="noopener noreferrer">`)
	b.WriteString(html.EscapeString(text))
	b.WriteString(`</a>`)
}

// writeTag renders a #tag or @mention as a chip — spec §10, §13. There is
// no tags table: this is a rendering and linking behaviour of the Markdown
// renderer alone. When linkTags is true (Render, the ordinary authenticated
// outline) the chip links to a literal search for that exact string,
// /notes/search — that route does not exist until N6, so until then this
// 404s, and it starts working with no further change once N6 ships. When
// linkTags is false (RenderShared, the public share page) the chip keeps
// its "outline-tag" styling but renders as an inert <span>, not a link —
// spec §15's "no link leads anywhere into the owner's private tree", which
// /notes/search would violate for an anonymous visitor even though it only
// dead-ends them at a login wall rather than exposing private data.
func writeTag(b *strings.Builder, tag string, linkTags bool) {
	if !linkTags {
		b.WriteString(`<span class="outline-tag">`)
		b.WriteString(html.EscapeString(tag))
		b.WriteString(`</span>`)
		return
	}
	b.WriteString(`<a class="outline-tag" href="/notes/search?q=`)
	b.WriteString(url.QueryEscape(tag))
	b.WriteString(`">`)
	b.WriteString(html.EscapeString(tag))
	b.WriteString(`</a>`)
}
