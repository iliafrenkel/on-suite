# ON Notes N4 — Markdown Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render inline Markdown in every bullet's title and note — bold, italic, code, strike, links, autolinked URLs, and `#tag`/`@tag` chips linking to search — shown when the field isn't focused, with the raw source shown (and editable) while it is.

**Architecture:** One pure Go function (`Render(s string) template.HTML`) does all the parsing and escaping — spec §10's "exactly one Markdown implementation, in Go, unit-testable." Each bullet already has a raw-text `<input>` (unchanged since N2); this chunk adds a sibling `<span>` holding the rendered HTML, and a pure-CSS `:has()` rule swaps which one is visible based on whether that specific input has focus — no JavaScript is needed for the swap itself. `notes.js` gains exactly one new job: after every autosave, splice the freshly-rendered HTML back into that sibling span via HTMX's out-of-band swap, so what's shown updates without a page reload.

**Tech Stack:** Go (`regexp`, `html`, `html/template`) — no new dependency, no Markdown library. HTMX out-of-band swaps (`hx-swap-oob`, already vendored, first genuine use of this specific mechanism in the codebase). CSS `:has()` (baseline-supported in every evergreen browser since 2023; this project targets no older browser anywhere else in its design).

## Global Constraints

- No CGO, no new Go dependencies (the Markdown renderer is hand-rolled — `regexp`/`html`/`html/template` are all standard library), no Node/npm/JS build step.
- CSP: `script-src 'self'` with no `unsafe-inline`; no new inline scripts or handlers. `internal/ui/static/app.css` is the only stylesheet — "Everything is driven by the tokens in `:root`. Apps must not introduce new colors" (its own header comment) — use existing `--c-*` tokens only.
- `main` is branch-protected: work on a branch, open a PR, never push directly.
- The local `staticcheck` binary can silently exit 0 while printing internal-error noise; always run `go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...` (matches CI) instead of the bare `staticcheck` on `$PATH`.
- `internal/htmlassert` supports only descendant combinators and one qualifier per selector part.
- Go's `regexp` package is RE2: matching is guaranteed linear-time, so there is no catastrophic-backtracking risk from user-controlled bullet text feeding a hand-written pattern, unlike a backtracking engine.
- Spec §10 decision made with the user before this plan was written: `#tag`/`@tag` chips link to `/notes/search?q=...` now, even though that route doesn't exist until N6 — it 404s until then and starts working with zero N4 changes once N6 lands. Do not defer the link or render the chip as inert.
- Scope decision: only bullet rows (the outline list) render Markdown. The breadcrumb and the zoomed heading (`{{.Data.Root.DisplayTitle}}`) stay plain text, as today — see "Follow-ups" below.

## The mechanism this chunk relies on

A bullet's title/note `<input>` never becomes unreachable: it is always present, always in the tab order, and its raw source is always what gets submitted — no behaviour here is JS-only in a way that would break the no-JS fallback N2/N3 proved. What changes for a JS-enabled browser:

1. Each field's rendered form lives in a sibling `<span id="rendered-title-{id}">` (or `-note-`), positioned exactly over the input via `position: absolute; inset: 0`.
2. `.outline-field input { color: transparent; caret-color: ...; }` hides the input's own text by default, and `.outline-field:has(input:focus) input { color: inherit; }` restores it the instant that specific input is focused — a pure CSS rule, so focusing by mouse click *or* by Tab both work identically, with no JS needed for the swap. `:has(input:focus)`, not `:focus-within`, deliberately: a rendered link inside the overlay is itself focusable (see below), and `:focus-within` would also fire when *it* is tabbed to, immediately hiding the very link the user just reached.
3. `.outline-rendered { pointer-events: none; }` lets a click on blank rendered text pass straight through to the (invisible but present) input underneath, entering edit mode — except `.outline-rendered a { pointer-events: auto; }` re-enables clicking the rendered links and tag chips themselves, so they navigate instead of falling through to edit mode.
4. Without JS, `.outline-field:has(input:focus)` still works (it's CSS, not a script), so raw-source editing is unaffected; only the *content* of the rendered spans becomes stale until the next full-page load, which every non-HTMX `/text` submission already provides via its existing 303 redirect.
5. With JS, every autosave (`hx-trigger="input changed delay:600ms, blur changed"`, already wired by N3) gets an HTMX out-of-band swap response updating both rendered spans, so the display is current the moment focus actually leaves.

## File Structure

- Create: `internal/apps/notes/markdown.go` — `Render(s string) template.HTML`.
- Create: `internal/apps/notes/markdown_test.go` — the parser's test suite.
- Modify: `internal/apps/notes/view.go` — `outlineRow` gains `RenderedTitle`/`RenderedNote`; `nest` populates them.
- Modify: `internal/apps/notes/view_test.go` — cover the new fields.
- Modify: `internal/apps/notes/templates/outline.html` — the rendered overlay markup, and the shared OOB blocks `/text` reuses.
- Modify: `internal/ui/static/app.css` — the reveal mechanism and tag-chip styling, in the existing "Outline (ON Notes)" section.
- Modify: `internal/apps/notes/handlers.go` — `setText`'s HTMX branch returns the OOB fragment instead of 204.
- Modify: `internal/apps/notes/handlers_test.go` — new tests for the OOB response and the rendered markup.
- Modify: `internal/apps/notes/static/notes.js` — no code change is needed (see Task 3's note), but a comment is added explaining why.
- Modify: `README.md`, `internal/apps/notes/notes.go` (package doc).

---

### Task 1: The Markdown renderer

**Files:**
- Create: `internal/apps/notes/markdown.go`
- Create: `internal/apps/notes/markdown_test.go`

**Interfaces:**
- Produces: `func Render(s string) template.HTML` — the only symbol later tasks use from this file.

This task is pure Go with no dependency on anything else in the package (other than the `notes` package name it lives in) — it has a full, independent TDD cycle.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/notes/markdown_test.go`:

```go
package notes_test

import (
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
```

Add `"html/template"` to the import block at the top of the file, so it reads:

```go
import (
	"html/template"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/notes/... -run TestRender -v`
Expected: FAIL — `notes.Render` does not exist yet.

- [ ] **Step 3: Implement the renderer**

Create `internal/apps/notes/markdown.go`:

```go
package notes

import (
	"html"
	"html/template"
	"net/url"
	"regexp"
	"strings"
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
		`|(https?://[^\s<>"')\]]+)` + // 6: bare autolink
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
	var b strings.Builder
	renderCodeSpans(&b, s)
	return template.HTML(b.String())
}

// renderCodeSpans splits out `code` spans and renders everything between
// them through renderInline. A code span's own content never reaches
// renderInline, which is what makes it verbatim.
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

// renderInline handles everything inlinePattern matches, in one left-to-right
// pass, escaping the literal text in between.
func renderInline(b *strings.Builder, s string) {
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
			writeTag(b, s[m[14]:m[15]])
		}
		last = m[1]
	}
	b.WriteString(html.EscapeString(s[last:]))
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

// writeTag renders a #tag or @mention as a chip linking to a literal search
// for that exact string — spec §10, §13. There is no tags table: this is a
// rendering and linking behaviour of the Markdown renderer alone. The route
// it links to, /notes/search, does not exist until N6 — until then this 404s,
// and it starts working with no further change once N6 ships.
func writeTag(b *strings.Builder, tag string) {
	b.WriteString(`<a class="outline-tag" href="/notes/search?q=`)
	b.WriteString(url.QueryEscape(tag))
	b.WriteString(`">`)
	b.WriteString(html.EscapeString(tag))
	b.WriteString(`</a>`)
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/apps/notes/... -run TestRender -v`
Expected: PASS, all of it.

- [ ] **Step 5: Full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/notes/markdown.go internal/apps/notes/markdown_test.go
git commit -m "notes: add the inline Markdown renderer"
```

---

### Task 2: Wire rendering into the view and the template

**Files:**
- Modify: `internal/apps/notes/view.go`
- Modify: `internal/apps/notes/view_test.go`
- Modify: `internal/apps/notes/templates/outline.html`
- Modify: `internal/ui/static/app.css`
- Modify: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `Render(s string) template.HTML` (Task 1).
- Produces: `outlineRow.RenderedTitle`, `outlineRow.RenderedNote` (both `template.HTML`) — Task 3's handler change constructs an `outlineRow` value itself and reads these same two fields.

After this task, every full-page and N3-fragment render already shows rendered Markdown by default and raw source on focus, entirely without notes.js — because both paths already go through `nest`, and the CSS reveal mechanism needs no script. Only the *live update after an edit* (Task 3) is still missing.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/notes/view_test.go` (same package, `notes`, as the existing `nest` tests):

```go
func TestNestRendersMarkdownIntoEachRow(t *testing.T) {
	rows := nest([]Node{{ID: 1, Title: "**bold**", Note: "*italic*", Depth: 0}}, RootID, "tok")
	if got := string(rows[0].RenderedTitle); got != "<strong>bold</strong>" {
		t.Errorf("RenderedTitle = %q", got)
	}
	if got := string(rows[0].RenderedNote); got != "<em>italic</em>" {
		t.Errorf("RenderedNote = %q", got)
	}
}
```

Add to `internal/apps/notes/handlers_test.go`:

```go
// TestBulletRendersMarkdown covers the full path: a saved title with
// Markdown in it reaches the page as rendered HTML inside the overlay span,
// while the input underneath still carries the raw source untouched — the
// no-JS fallback keeps editing the literal text spec §7 already proved.
func TestBulletRendersMarkdown(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "**bold** and #tag")

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/notes/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `id="rendered-title-`+itoa(id)+`"`) {
		t.Fatalf("no rendered overlay for bullet %d in:\n%s", id, body)
	}
	doc := htmlassert.Parse(t, body)
	overlay := doc.MustHave(`#rendered-title-` + itoa(id))
	if got := htmlassert.Text(overlay); got != "bold and #tag" {
		t.Errorf("rendered overlay text = %q", got)
	}
	if n := len(doc.QueryAll(`#rendered-title-`+itoa(id)+` strong`)); n != 1 {
		t.Errorf("got %d <strong> in the overlay, want 1", n)
	}
	if n := len(doc.QueryAll(`#rendered-title-`+itoa(id)+` a`)); n != 1 {
		t.Errorf("got %d tag chip in the overlay, want 1", n)
	}

	// The raw <input> still carries the literal, unrendered source.
	raw := doc.MustHave("input.outline-title")
	if v, _ := htmlassert.Attr(raw, "value"); v != "**bold** and #tag" {
		t.Errorf("input value = %q, want the raw source untouched", v)
	}
}

// TestRenderedOverlayEscapesBulletText: the overlay is real HTML the browser
// parses, so it must never carry unescaped user text even when nothing in
// it looks like Markdown.
func TestRenderedOverlayEscapesBulletText(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, `<script>alert(1)</script>`)

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/notes/", nil))
	if strings.Contains(rec.Body.String(), "<script>alert(1)</script>") {
		t.Error("bullet text reached the rendered overlay unescaped")
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/notes/... -run 'Markdown|Rendered' -v`
Expected: FAIL — `outlineRow` has no `RenderedTitle`/`RenderedNote` field yet, and no `#rendered-title-*` element exists.

- [ ] **Step 3: Add the fields and populate them in `nest`**

Modify `internal/apps/notes/view.go`. In `outlineRow`, add two fields after `Children`:

```go
type outlineRow struct {
	// Node carries ID, Title, Note, Position, Depth, Collapsed and
	// HasChildren, all set by Store.Outline.
	Node
	// Last is true for the final row of a sibling list, which is what decides
	// whether "move down" renders disabled.
	Last bool
	// RootID is the zoom root this outline was loaded at, for the hidden
	// field that sends the mutation back to the right page. It is an id, not
	// a Node, which is the whole difference from outlineView.Root — hence the
	// different name.
	RootID    int64
	CSRFToken string
	Children  []*outlineRow
	// RenderedTitle and RenderedNote are Title and Note run through Render —
	// spec §10. The template shows these by default and the raw input only
	// while it has focus; computing them here, once, keeps that a template
	// concern rather than something outline-rows has to call out to.
	RenderedTitle template.HTML
	RenderedNote  template.HTML
}
```

Add `"html/template"` to the file's imports (it currently has none — add an import block):

```go
package notes

import "html/template"
```

In `nest`, change the row construction:

```go
		row := &outlineRow{Node: n, RootID: root, CSRFToken: csrfToken}
```

to:

```go
		row := &outlineRow{
			Node: n, RootID: root, CSRFToken: csrfToken,
			RenderedTitle: Render(n.Title),
			RenderedNote:  Render(n.Note),
		}
```

- [ ] **Step 4: Add the rendered overlay markup**

Modify `internal/apps/notes/templates/outline.html`. Replace the `outline-text` span inside `outline-rows` — currently:

```html
				<span class="outline-text">
					<input class="outline-title" type="text" name="title"
					       value="{{.Title}}" maxlength="2000" aria-label="Bullet"
					       hx-post="/notes/{{.ID}}/text"
					       hx-trigger="input changed delay:600ms, blur changed" hx-swap="none">
					<input class="outline-note" type="text" name="note"
					       value="{{.Note}}" maxlength="10000"
					       placeholder="note" aria-label="Note"
					       hx-post="/notes/{{.ID}}/text"
					       hx-trigger="input changed delay:600ms, blur changed" hx-swap="none">
				</span>
```

with:

```html
				<span class="outline-text">
					<span class="outline-field">
						{{template "rendered-title" .}}
						<input class="outline-title" type="text" name="title"
						       value="{{.Title}}" maxlength="2000" aria-label="Bullet"
						       hx-post="/notes/{{.ID}}/text"
						       hx-trigger="input changed delay:600ms, blur changed" hx-swap="none">
					</span>
					<span class="outline-field">
						{{template "rendered-note" .}}
						<input class="outline-note" type="text" name="note"
						       value="{{.Note}}" maxlength="10000"
						       placeholder="note" aria-label="Note"
						       hx-post="/notes/{{.ID}}/text"
						       hx-trigger="input changed delay:600ms, blur changed" hx-swap="none">
					</span>
				</span>
```

Add these two block definitions near the top of the file, after the `{{define "head"}}` block and before `{{define "content"}}`. Both take an `outlineRow` (read `.ID`, `.RenderedTitle`, `.RenderedNote`) and are shared between the normal row render above and Task 3's `/text` response, so the two can never drift apart:

```html
{{/* rendered-title and rendered-note are shared between a normal row render
     and setText's HTMX response (Task 3), so the markup can never drift
     between the two. hx-swap-oob is inert here — htmx only interprets it
     when processing an AJAX response, never while parsing a normally
     rendered page — and harmless to carry on every row for that reason. */}}
{{define "rendered-title"}}<span id="rendered-title-{{.ID}}" class="outline-rendered outline-title-rendered" hx-swap-oob="true">{{.RenderedTitle}}</span>{{end}}
{{define "rendered-note"}}<span id="rendered-note-{{.ID}}" class="outline-rendered outline-note-rendered" hx-swap-oob="true">{{.RenderedNote}}</span>{{end}}
```

- [ ] **Step 5: Add the reveal mechanism and chip styling**

Modify `internal/ui/static/app.css`. In the existing "Outline (ON Notes)" section, immediately after the block:

```css
input.outline-title { color: var(--c-text); line-height: 1.7; }
input.outline-note { font-size: var(--fs-sm); color: var(--c-text-dim); }
```

add:

```css
/* Markdown (N4): each field is [rendered overlay, then the raw input] in
 * that order, sharing one box. The input's own text is transparent by
 * default — caret-color keeps the caret itself visible — and the overlay
 * sits on top showing the rendered form. :has(input:focus), not
 * :focus-within: a rendered link is itself focusable (see .outline-rendered
 * a below), and :focus-within would also fire when *it* is tabbed to,
 * hiding the very link just reached. */
.outline-field { position: relative; }

.outline-field input { color: transparent; caret-color: var(--c-text); }
.outline-field:has(input:focus) input { color: inherit; }
.outline-field:has(input:focus) .outline-rendered { display: none; }

.outline-rendered {
	position: absolute;
	inset: 0;
	pointer-events: none; /* let a click on blank rendered text reach the input beneath it */
	overflow: hidden;
	white-space: nowrap;
	text-overflow: ellipsis;
}

/* A rendered link must stay clickable even though the overlay it lives in
 * exists mainly to let other clicks fall through to the editable input. */
.outline-rendered a { pointer-events: auto; }

.outline-title-rendered { color: var(--c-text); line-height: 1.7; }
.outline-note-rendered { font-size: var(--fs-sm); color: var(--c-text-dim); }

.outline-tag {
	background: var(--c-bg-inset);
	color: var(--c-text-dim);
	padding: 0 0.35em;
	border-radius: var(--radius);
	text-decoration: none;
}
.outline-tag:hover { color: var(--c-accent); }
```

`<code>`, `<strong>`, `<em>` and `<s>` need no new CSS: `code, pre, kbd { font-family: var(--font-mono); ... }` already exists globally, and the other three are native browser semantics.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, all of it — old and new.

- [ ] **Step 7: Full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 8: Manual QA — the reveal mechanism**

`gofmt`/`go vet`/tests cannot see CSS behaviour. With `go run ./cmd/onsuite serve` running:

1. Create a bullet titled `**bold** and a #tag`. Click away from it. Confirm it shows as rendered — bold text, and a pill-styled `#tag` — not raw asterisks.
2. Click into the bullet's title. Confirm it switches to the raw, editable text `**bold** and a #tag`, caret visible, and typing works normally.
3. Click away again (or press Tab out). Confirm it reverts to rendered.
4. Tab through the page until focus reaches the `#tag` chip itself (not by clicking into the title first). Confirm the chip stays visible and rendered — focusing it must not flip the field into raw-edit mode.
5. Click the `#tag` chip. Confirm it navigates to `/notes/search?q=%23tag` (a 404 today, until N6 — this is expected and was the explicit decision behind this plan).
6. Disable JavaScript (or use `curl`/view-source) and load the page again. Confirm the title's raw source is what's shown and editable — Markdown is simply not rendered without JS, which is correct per this task's design.

- [ ] **Step 9: Commit**

```bash
git add internal/apps/notes/view.go internal/apps/notes/view_test.go internal/apps/notes/templates/outline.html internal/ui/static/app.css internal/apps/notes/handlers_test.go
git commit -m "notes: render Markdown in the outline, raw source while focused"
```

---

### Task 3: Live update after an edit

**Files:**
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/handlers_test.go`
- Modify: `internal/apps/notes/templates/outline.html`
- Modify: `internal/apps/notes/static/notes.js`

**Interfaces:**
- Consumes: `Render` (Task 1), `outlineRow` (Task 2), the `rendered-title`/`rendered-note` blocks (Task 2).
- Produces: nothing new for later tasks — this is the last piece of behaviour N4 adds.

Right now `setText`'s HTMX branch answers 204 (N3): correct when nothing on screen needed to change, but a Markdown edit *does* change what should be shown once focus leaves. This task swaps that 204 for an HTMX out-of-band response carrying both fields' freshly rendered HTML.

- [ ] **Step 1: Write the failing test**

Add to `internal/apps/notes/handlers_test.go`:

```go
// TestSetTextRespondsWithRenderedMarkdownForHTMX supersedes N3's 204: once a
// field can show rendered Markdown, an edit has to update it, and there is
// no swap that can happen without a response body to swap in.
func TestSetTextRespondsWithRenderedMarkdownForHTMX(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "old")

	rec := s.postHX(t, s.alice, "/notes/"+itoa(id)+"/text", url.Values{
		"root": {"0"}, "focus_id": {itoa(id)}, "title": {"**new**"}, "note": {"*n*"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="rendered-title-`+itoa(id)+`"`) ||
		!strings.Contains(body, "<strong>new</strong>") {
		t.Errorf("title OOB fragment missing or wrong: %s", body)
	}
	if !strings.Contains(body, `id="rendered-note-`+itoa(id)+`"`) ||
		!strings.Contains(body, "<em>n</em>") {
		t.Errorf("note OOB fragment missing or wrong: %s", body)
	}
	if !strings.Contains(body, `hx-swap-oob="true"`) {
		t.Error("the response does not mark itself as an out-of-band swap")
	}

	n, err := s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "**new**" {
		t.Errorf("saved title = %q, want the raw source", n.Title)
	}
}
```

Remove `TestSetTextRespondsWithNoContentForHTMX` (added in the N3 plan) — this task replaces the behaviour it asserted. If it is still present in `handlers_test.go`, delete it entirely rather than leaving it to fail.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/notes/... -run 'SetTextRespondsWith' -v`
Expected: FAIL — the handler still answers 204.

- [ ] **Step 3: Add the combined OOB block and update `setText`**

Modify `internal/apps/notes/templates/outline.html`. Add this block right after the two from Task 2:

```html
{{/* text-update is setText's whole HTMX response: both fields' rendered
     forms, each already carrying hx-swap-oob, and nothing else — htmx
     applies out-of-band swaps found anywhere in a response regardless of
     the triggering element's own hx-swap, which is why the inputs
     themselves can stay hx-swap="none". */}}
{{define "text-update"}}{{template "rendered-title" .}}{{template "rendered-note" .}}{{end}}
```

Modify `internal/apps/notes/handlers.go`. Add `"strings"` to the import block:

```go
import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)
```

Replace `setText`'s body from the `title, note :=` reads onward — currently:

```go
	if err := a.store.SetText(r.Context(), userID, id, r.PostFormValue("title"), r.PostFormValue("note")); err != nil {
		a.fail(w, r, err)
		return
	}
	if web.IsHTMX(r) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, outlinePath(root), http.StatusSeeOther)
}
```

with:

```go
	// Trimmed exactly the way Ops.SetText trims title before saving, so the
	// Markdown rendered below matches what the database now holds.
	title := strings.TrimRight(r.PostFormValue("title"), " \t")
	note := r.PostFormValue("note")

	if err := a.store.SetText(r.Context(), userID, id, title, note); err != nil {
		a.fail(w, r, err)
		return
	}
	if web.IsHTMX(r) {
		row := &outlineRow{Node: Node{ID: id}, RenderedTitle: Render(title), RenderedNote: Render(note)}
		if err := a.deps.Render.Fragment(w, http.StatusOK, "notes/outline", "text-update", row); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	http.Redirect(w, r, outlinePath(root), http.StatusSeeOther)
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, all of it.

- [ ] **Step 5: Note why `notes.js` needs no change**

`notes.js` already sends every field's live value on every autosave (N3's `augmentRequest`), and out-of-band swaps are handled entirely by the `htmx` library the moment a response contains an `hx-swap-oob` element — there is no event to hook and nothing for a named function to do. Add one comment to `internal/apps/notes/static/notes.js`, directly above the `initFocusSync` function, so a future reader does not go looking for OOB-handling code that was never needed:

```js
	// N4 (Markdown) needs no code here: setText's response carries its own
	// hx-swap-oob elements, and htmx applies those on its own the moment a
	// response contains one, regardless of the triggering element's own
	// hx-swap. There is nothing for a named function to do.
```

- [ ] **Step 6: Full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 7: Manual QA — live update**

With `go run ./cmd/onsuite serve` running: click into a bullet, type `**hello**`, click away without reloading the page. Confirm it now shows rendered bold text immediately — no page reload, and the network tab shows exactly one request (the autosave `/text` POST), not a separate one for the swap.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/notes/handlers.go internal/apps/notes/handlers_test.go internal/apps/notes/templates/outline.html internal/apps/notes/static/notes.js
git commit -m "notes: update rendered Markdown live after an edit, via an HTMX OOB swap"
```

---

### Task 4: Docs and final verification

**Files:**
- Modify: `README.md`
- Modify: `internal/apps/notes/notes.go`

- [ ] **Step 1: Update README.md**

Replace, in the intro paragraph:

> **ON Notes** is a hierarchical outliner — one infinite tree per account, with zoom, collapse, every structural operation and a full keyboard layer: type, indent, reorder and delete without touching the mouse. Markdown, due dates and search are still being built out.

with:

> **ON Notes** is a hierarchical outliner — one infinite tree per account, with zoom, collapse, every structural operation, a full keyboard layer and inline Markdown (bold, links, `#tags`). Due dates and search are still being built out.

Replace, in the Status section:

> Work since then is per-app rather than per-phase. ON Notes is being built in
> ten small chunks under
> [`docs/superpowers/plans/2026-08-25-on-notes-design.md`](docs/superpowers/specs/2026-08-25-on-notes-design.md);
> N1 (schema and store), N2 (the outline) and N3 (the keyboard layer) are
> done.

with:

> Work since then is per-app rather than per-phase. ON Notes is being built in
> ten small chunks under
> [`docs/superpowers/plans/2026-08-25-on-notes-design.md`](docs/superpowers/specs/2026-08-25-on-notes-design.md);
> N1 (schema and store), N2 (the outline), N3 (the keyboard layer) and N4
> (Markdown) are done.

- [ ] **Step 2: Update the package doc comment**

Modify `internal/apps/notes/notes.go`. This comment was already slightly behind after N3 (it still named only N1/N2); bring it current for both:

```go
// The design is in docs/superpowers/specs/2026-08-25-on-notes-design.md. This
// package spans chunk N1 (schema and store) through N4 (Markdown): app.App,
// routes, templates, handlers, the keyboard layer in static/notes.js, and the
// inline Markdown renderer in markdown.go.
```

- [ ] **Step 3: Final full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add README.md internal/apps/notes/notes.go
git commit -m "notes: mark N4 (Markdown) done"
```

---

## Self-review notes for the executing agent

- **`writeLink`'s scheme check is deliberately simple.** It only ever accepts `http://`/`https://` (case-insensitive) and treats everything else — no scheme, `javascript:`, `mailto:`, `data:` — identically, as literal text. Do not special-case any other scheme; spec §10 names exactly `http`/`https` as the allowed pair.
- **A URL containing its own nested, unescaped `)` — inside `[text](url)`, not a bare autolink — is mis-captured**: the url-capture group `[^)]+` stops at the *first* `)`, which for something like `(javascript:alert(1))` is the one inside `alert(1)`, not the link syntax's own closing paren. This is a real, accepted parsing limitation, not a security issue: `writeLink`'s scheme check runs on whatever got captured, so a mis-captured "URL" that happens to still start with `javascript:` (or anything else non-http) still takes the literal-text branch, and even in the (harder to construct) case where mis-capture produced something starting with `http(s)://`, the value only ever reaches the page through `html.EscapeString` inside an `href="..."` attribute — there is no path from a wrong capture to executing script, only to a link pointing somewhere slightly different from what a perfect parser would have produced. Do not attempt a balanced-parenthesis fix; it adds real complexity for a case this app's own note-taking use will rarely produce.
- **The renderer disallows a literal `*`, `` ` ``, `~`, `]`, or `)` inside the construct it would otherwise close** (e.g. `**bold with * inside**` will not parse as one bold span). This is a real, intentional simplification, not a bug to fix: none of these inline constructs need to nest or contain their own delimiter for this app's use, and a character class that excludes the delimiter is what keeps the whole parser a single linear-time regex pass with no backtracking or lookahead.
- **`writeTag`'s href is built with `net/url.QueryEscape`, not string concatenation.** A tag body can only ever be `[\p{L}\p{N}_]+` (enforced by `inlinePattern` itself), so in practice this never needs to escape anything unusual — but using the real query-escaping function rather than assuming that is what keeps this correct if the tag character class is ever loosened later.
- **`:has()` is a real, current browser-support decision, not an oversight.** It has been supported in every evergreen browser since 2023 (Chrome 105+, Firefox 121+, Safari 15.4+), and nothing else in this project's design targets an older one. Do not add a `:focus-within` fallback "for safety" — it reintroduces exactly the tab-to-a-rendered-link bug `:has(input:focus)` exists to avoid (see the CSS comment in Task 2, Step 5).
- **`rendered-title`/`rendered-note` carry `hx-swap-oob="true"` even in a page that was never an HTMX response** (the normal full-page and N3-fragment renders). This is correct, not a leftover: htmx only interprets that attribute while processing an actual AJAX response, so it is inert markup the rest of the time, and having it always present is what lets Task 3 reuse the exact same two blocks for `setText`'s response instead of maintaining a second copy of this markup that could drift from the first.
- **Task 3 removes N3's `TestSetTextRespondsWithNoContentForHTMX`, not just supersedes it.** If that test is still present when Task 3 starts, delete it rather than leaving a permanently-failing assertion in the suite — 204 was correct behaviour before this chunk and is no longer.

## Follow-ups to file as issues

1. Breadcrumb links and the zoomed heading (`{{.Data.Root.DisplayTitle}}`) still show a bullet's raw Markdown source verbatim (e.g. a title of `**Projects**` reads literally, asterisks and all, once zoomed into) — this chunk deliberately scopes rendering to the outline rows only. Worth revisiting once there's a real need for a bullet's title to look consistent wherever it appears.
2. The rendered overlay truncates a long title/note with an ellipsis (`white-space: nowrap; text-overflow: ellipsis`) for visual consistency with the single-line `<input>` beneath it. If bullets routinely grow long enough for this to matter in practice, reconsider allowing the rendered form to wrap while the input stays single-line and scrollable — a design change, not a bug fix.
3. A rendered link/tag click races the same "unsaved edit elsewhere on the page" risk N3's self-review already accepted for breadcrumb navigation: any full page navigation can leave a pending, not-yet-fired autosave behind. No new mitigation is proposed here; it is the same accepted category of risk, now with one more way to trigger it.
