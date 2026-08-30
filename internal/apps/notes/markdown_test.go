package notes_test

import (
	"html/template"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

func TestRenderPlainTextIsEscaped(t *testing.T) {
	got := string(notes.Render(`<script>alert(1)</script>`))
	if strings.Contains(got, "<script>") {
		t.Errorf("got %q, script tag was not escaped", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("got %q, want the escaped form somewhere in it", got)
	}
}

func TestRenderEmptyStringIsEmpty(t *testing.T) {
	if got := notes.Render(""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestRenderBold(t *testing.T) {
	got := string(notes.Render("a **bold** word"))
	if !strings.Contains(got, "<strong>bold</strong>") {
		t.Errorf("got %q", got)
	}
}

func TestRenderItalic(t *testing.T) {
	got := string(notes.Render("a *italic* word"))
	if !strings.Contains(got, "<em>italic</em>") {
		t.Errorf("got %q", got)
	}
}

// TestRenderBoldBeatsItalic: ** must not be read as two adjacent * markers.
func TestRenderBoldBeatsItalic(t *testing.T) {
	got := string(notes.Render("**bold**"))
	if got != "<strong>bold</strong>" {
		t.Errorf("got %q, want exactly one <strong>", got)
	}
}

func TestRenderStrike(t *testing.T) {
	got := string(notes.Render("~~gone~~"))
	if got != "<s>gone</s>" {
		t.Errorf("got %q", got)
	}
}

// TestRenderCodeIsVerbatim: markers inside a code span are not markdown.
func TestRenderCodeIsVerbatim(t *testing.T) {
	got := string(notes.Render("`**not bold**`"))
	if got != "<code>**not bold**</code>" {
		t.Errorf("got %q", got)
	}
}

func TestRenderLinkWithHTTPSScheme(t *testing.T) {
	got := string(notes.Render("[docs](https://example.com/a)"))
	want := `<a href="https://example.com/a" target="_blank" rel="noopener noreferrer">docs</a>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRenderLinkWithBadSchemeIsLiteral: spec §10 — only http/https produce an
// anchor; javascript: above all must render as inert, literal text — the
// whole "[text](url)" source, not just the label. (The URL here has no
// parenthesis in it deliberately: the url-capture group stops at the first
// unescaped ")", so a URL containing its own nested paren — rare in
// practice — is a separate, documented parsing limitation this test does
// not exercise; see the plan's self-review notes for why it is a display
// quirk and never a security issue.)
func TestRenderLinkWithBadSchemeIsLiteral(t *testing.T) {
	got := string(notes.Render(`[click](javascript:alert)`))
	if strings.Contains(got, "<a") {
		t.Errorf("got %q, a javascript: URL produced an anchor", got)
	}
	if got != `[click](javascript:alert)` {
		t.Errorf("got %q, want the whole literal source preserved, not just the label", got)
	}
}

func TestRenderLinkTextIsEscaped(t *testing.T) {
	got := string(notes.Render(`[<b>x</b>](https://example.com)`))
	if strings.Contains(got, "<b>x</b>") {
		t.Errorf("got %q, link text was not escaped", got)
	}
	if !strings.Contains(got, "&lt;b&gt;x&lt;/b&gt;") {
		t.Errorf("got %q", got)
	}
}

func TestRenderBareURLIsAutolinked(t *testing.T) {
	got := string(notes.Render("see https://example.com now"))
	if !strings.Contains(got, `<a href="https://example.com" target="_blank" rel="noopener noreferrer">https://example.com</a>`) {
		t.Errorf("got %q", got)
	}
	if !strings.HasPrefix(got, "see ") || !strings.HasSuffix(got, " now") {
		t.Errorf("got %q, surrounding text should be untouched", got)
	}
}

// TestRenderBareURLWithOwnParenIsNotTruncated is issue #69: a bare autolink's
// own single-level nested paren (common in Wikipedia/MSDN URLs) must stay
// part of the link, not truncate the match at the first ")".
func TestRenderBareURLWithOwnParenIsNotTruncated(t *testing.T) {
	got := string(notes.Render("see https://en.wikipedia.org/wiki/Foo_(bar) ok"))
	want := `<a href="https://en.wikipedia.org/wiki/Foo_(bar)" target="_blank" rel="noopener noreferrer">https://en.wikipedia.org/wiki/Foo_(bar)</a>`
	if !strings.Contains(got, want) {
		t.Errorf("got %q, want it to contain %q", got, want)
	}
	if !strings.HasSuffix(got, " ok") {
		t.Errorf("got %q, trailing text should be untouched", got)
	}
}

// TestRenderBareURLWrappedInProseParenIsNotExtended: a paren that merely
// wraps the URL in surrounding prose, rather than belonging to the URL
// itself, must still end the match — unchanged from before #69.
func TestRenderBareURLWrappedInProseParenIsNotExtended(t *testing.T) {
	got := string(notes.Render("(see https://example.com)"))
	want := `<a href="https://example.com" target="_blank" rel="noopener noreferrer">https://example.com</a>`
	if !strings.Contains(got, want) {
		t.Errorf("got %q, want it to contain %q", got, want)
	}
	if !strings.HasSuffix(got, "</a>)") {
		t.Errorf("got %q, the wrapping \")\" should stay outside the link", got)
	}
}

func TestRenderTagChip(t *testing.T) {
	got := string(notes.Render("check #urgent today"))
	if !strings.Contains(got, `<a class="outline-tag" href="/notes/search?q=%23urgent">#urgent</a>`) {
		t.Errorf("got %q", got)
	}
}

func TestRenderMentionChip(t *testing.T) {
	got := string(notes.Render("ping @alice"))
	if !strings.Contains(got, `<a class="outline-tag" href="/notes/search?q=%40alice">@alice</a>`) {
		t.Errorf("got %q", got)
	}
}

func TestRenderBareHashIsLiteral(t *testing.T) {
	got := string(notes.Render("a # b"))
	if strings.Contains(got, "outline-tag") {
		t.Errorf("got %q, a lone # should not become a chip", got)
	}
}

func TestRenderMultipleTagsInOneString(t *testing.T) {
	got := string(notes.Render("#a #b"))
	if strings.Count(got, "outline-tag") != 2 {
		t.Errorf("got %q, want two chips", got)
	}
}

// TestRenderTagStopsAtPunctuation: the tag body is word characters only, so
// trailing punctuation stays outside the chip.
func TestRenderTagStopsAtPunctuation(t *testing.T) {
	got := string(notes.Render("check #tag."))
	if !strings.Contains(got, `<a class="outline-tag" href="/notes/search?q=%23tag">#tag</a>.`) {
		t.Errorf("got %q", got)
	}
}

// TestRenderMentionInEmailIsLiteral is issue #68: an email address's domain
// looks exactly like a #tag/@mention chip's own pattern, so a boundary check
// is what tells "ilia@example.com" apart from "ping @alice".
func TestRenderMentionInEmailIsLiteral(t *testing.T) {
	got := string(notes.Render("mail me at ilia@example.com"))
	if strings.Contains(got, "outline-tag") {
		t.Errorf("got %q, an email address should not produce a mention chip", got)
	}
	if got != "mail me at ilia@example.com" {
		t.Errorf("got %q, want the literal source unchanged", got)
	}
}

// TestRenderTagMidWordIsLiteral is issue #68: a #/@ preceded by a word
// character is part of that word, not a tag/mention on its own.
func TestRenderTagMidWordIsLiteral(t *testing.T) {
	for _, s := range []string{"a#b", "x@y"} {
		got := string(notes.Render(s))
		if strings.Contains(got, "outline-tag") {
			t.Errorf("Render(%q) = %q, want no chip mid-word", s, got)
		}
		if got != s {
			t.Errorf("Render(%q) = %q, want the literal source unchanged", s, got)
		}
	}
}

// TestRenderUnclosedMarkerIsLiteral: a marker with no matching close is not
// markdown — it renders as the literal characters the user typed.
func TestRenderUnclosedMarkerIsLiteral(t *testing.T) {
	got := string(notes.Render("**not closed"))
	if got != "**not closed" {
		t.Errorf("got %q, want the literal source", got)
	}
}

func TestRenderMultipleConstructsInOneString(t *testing.T) {
	got := string(notes.Render("**a** and *b* and `c` and ~~d~~"))
	want := "<strong>a</strong> and <em>b</em> and <code>c</code> and <s>d</s>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRenderThroughARealTemplateIsNotDoubleEscaped: Render's return type is
// template.HTML, which html/template trusts verbatim. If Render ever
// regressed to returning a plain string, this would catch the entities
// coming out double-escaped.
func TestRenderThroughARealTemplateIsNotDoubleEscaped(t *testing.T) {
	tmpl := template.Must(template.New("x").Parse(`{{.}}`))
	var b strings.Builder
	if err := tmpl.Execute(&b, notes.Render("**bold**")); err != nil {
		t.Fatal(err)
	}
	if b.String() != "<strong>bold</strong>" {
		t.Errorf("got %q", b.String())
	}
}
