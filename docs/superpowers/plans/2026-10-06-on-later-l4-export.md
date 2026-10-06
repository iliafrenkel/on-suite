# ON Later L4 — export, Markdown download, admin card and screenshots: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close #485, the last ON Later phase: every article can be
downloaded as Markdown (`later-article.md`), `onsuite export` includes ON
Later, the admin page gets an ON Later card, and the user guide, README and
developer docs get ON Later screenshots and mentions.

**Architecture:** Builds on the merged L3 app (`internal/apps/later`). No
migrations. A small hand-written HTML→Markdown converter (`markdown.go`)
covers exactly the sanitiser's allowlist; `download.go` assembles the file
and serves `GET /later/a/{id}/markdown`. `export.go` implements
`app.Exporter`, and `stats.go` implements `app.Stater`. The screenshot seed
gets demo articles (original prose in `docs/screenshots/seed/fixtures/later/`)
so `capture` can shoot the list, search, reading view and Notes panel.

**Tech Stack:** Go, `golang.org/x/net/html` (already a dependency),
`html/template`, the existing `docs/screenshots` seed/capture tools. No new
JavaScript, no new dependency.

**Spec:** [2026-10-05-on-later-design.md](../specs/2026-10-05-on-later-design.md)
(sections "Export", "Admin card", "Docs", "Phases" — L4). **Issue:** #485
(closes it). **Previous plan:** [L3](2026-10-06-on-later-l3-tags-search.md).

**Decisions already made (Ilia):**
- The JSON export rewrites every image `src` in `content_html` from
  `/later/img/{hash}` back to the image's original URL, so the exported HTML
  reads on its own. There is no separate image list. Image bytes are not
  exported.
- In `later-article.md`, highlights go in a `## Highlights` section: each
  quote as a `>` blockquote, its comment as a paragraph under it. The
  article text follows under `## Article`, without inline marks.
- Screenshots: four guide shots (`later-list`, `later-search`,
  `later-reading`, `later-notes`) plus an ON Later section in the README
  with light/dark thumbnails. The hero is re-shot so the dashboard shows
  five apps.

**Decisions made in this plan (flag them in the PR):**
- The note and comments go into the Markdown file exactly as written, not
  escaped. They are the user's own words and may already be Markdown (a
  `1.` list, `*emphasis*`). Article text, titles and quotes are escaped.
- Article headings move down two levels in the Markdown (`<h1>` → `###`,
  capped at `######`), so they sit under the file's own `## Article`.
- The Markdown file's header lists `Source`, `Author` (when known),
  `Saved` (the server's local date) and `Tags` (when any).
- Link-only articles download with no `## Article` section.
- `<br>` becomes a CommonMark backslash hard break; `<pre>` becomes a
  fenced block; tables become GFM pipe tables with the first row as the
  header; `<u>`, `<sub>`, `<sup>`, `<mark>`, `<small>`, `<cite>`, `<abbr>`,
  `<time>`, `<span>` keep only their text.
- Admin card: **Articles**, **Unread**, **Reading**, **Archived**,
  **Highlights**, **Stored images** (bytes, via a mirror of Paste's
  `humanBytes`).
- The demo seed has no images and no favicon URLs, so the demo server never
  fetches anything.

## Global Constraints

- Everything from L1a/L1b/L2/L3's Global Constraints still binds: `later_`
  prefixes, `db.FormatTime`/`ParseTime`, time only via `Store.now()`, no
  import of another app, owner scoping (404 for someone else's), no inline
  `<script>`/`style=` attributes, no `template.HTML` added, no new
  dependencies, full check green after every task:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- Download file name is exactly `later-article.md`, never derived from the
  title (suite rule: generic export filenames).
- Exported times are `time.Time` / `*time.Time` JSON fields (as Flash's
  export). Do **not** use `time.RFC3339`/`RFC3339Nano` anywhere:
  `TestRFC3339IsContained` pins the files allowed to.
- `internal/platform/db` sets `SetMaxOpenConns(1)`: close a `*sql.Rows`
  before running another query (load all rows into a slice first).
- htmlassert selectors take **one** qualifier per compound
  (`a[href=…]`, never `a.x[href=…]`).
- User guides stay plain Markdown: no raw HTML, no `../` links.
- Demo prose in the seed is original writing (no copied articles).
- Commits: Conventional Commits, scope `later` (seed/screenshots/docs:
  `docs(later)`).

## File structure (new or substantially changed)

```
internal/apps/later/
  markdown.go                     mdConverter, htmlToMarkdown and md* helpers (pure)
  markdown_internal_test.go       table tests for htmlToMarkdown (package later)
  download.go                     articleMarkdown, imageLink, App.markdown handler
  download_internal_test.go       golden tests for articleMarkdown (package later)
  download_test.go                handler tests for GET /later/a/{id}/markdown
  images_store.go                 + Store.ImageSources
  export.go                       exported* types, exportHTML, Store.Export, App.Export
  export_store_test.go            store/export tests
  stats.go                        Store.Stats, App.Stats, humanBytes (mirror of Paste's)
  stats_test.go                   store tests for Stats
  later.go                        route GET /a/{id}/markdown; Exporter/Stater assertions
  templates/article.html          ⋯ menu: Download as Markdown
cmd/onsuite/export_test.go        assert the "later" key
docs/screenshots/seed/
  later.go                        seedLater, laterFixtures, runeIndex
  fixtures/later/*.html           five demo articles
  seed.go                         call seedLater
  seed_test.go                    Later checks; shotIDs/shotIDRe know later/a
docs/screenshots/capture/shots.go later guide shots, README thumbnails
docs/user/later.md                screenshots, "Downloading an article", export sentence
docs/user/admin.md                ON Later numbers, download images job, export sentence
docs/user/{notes,paste,reader}.md export sentence
docs/self-hosting/deploying.md    export paragraph
README.md                         ON Later section, hero alt, "five apps"
AGENTS.md                         app list
docs/developers/index.md          app list
docs/developers/repository-layout.md  later/ in the tree
```

---

### Task 1: The HTML → Markdown converter

**Files:**
- Create: `internal/apps/later/markdown.go`
- Test: `internal/apps/later/markdown_internal_test.go`

**Interfaces:**
- Consumes: `isBlockElement(atom.Atom) bool` from `text.go` (block-level
  set shared with `plainText`).
- Produces (package-private, used by Task 2):
  - `htmlToMarkdown(fragment string, image func(src string) string, shift int) string`
    — Markdown blocks joined by `"\n\n"`, no trailing newline; `""` for an
    empty fragment. `image` maps an `<img src>` to the link target (`""`
    keeps only the alt text). `shift` moves headings down that many levels,
    capped at 6.
  - `mdEscape(s string) string` — backslash-escapes `\ ` `` ` `` `* _ [ ] < ~`.
  - `mdParagraph(run string) string` — one paragraph from inline Markdown:
    whitespace collapsed per line, empty lines dropped, line starts escaped,
    lines joined by a backslash hard break (`"\\\n"`).
  - `mdPrefix(s, prefix, emptyPrefix string) string` — prefixes every line
    (`emptyPrefix` for empty lines), used for blockquotes.

- [ ] **Step 1: Write the failing test**

Create `internal/apps/later/markdown_internal_test.go`:

```go
package later

import "testing"

func TestHTMLToMarkdown(t *testing.T) {
	images := map[string]string{"/later/img/abc": "https://e.example/cat.jpg"}
	image := func(src string) string { return images[src] }
	cases := []struct {
		name, html string
		shift      int
		want       string
	}{
		{"empty", ``, 0, ``},
		{"paragraphs", `<p>One</p><p>Two</p>`, 0, "One\n\nTwo"},
		{"whitespace collapses", "<p>  a\n  b  </p>", 0, "a b"},
		{"headings", `<h1>Title</h1><h3>Sub <em>part</em></h3>`, 0, "# Title\n\n### Sub *part*"},
		{"headings shift and cap", `<h1>A</h1><h5>B</h5>`, 2, "### A\n\n###### B"},
		{"emphasis keeps spaces outside", `<p>a<em> b </em>c</p>`, 0, "a *b* c"},
		{"strong and strike", `<p><strong>bold</strong> <b>b</b> <del>gone</del> <s>x</s></p>`, 0, "**bold** **b** ~~gone~~ ~~x~~"},
		{"link", `<p>See <a href="https://e.example/a_(b)" rel="nofollow">here</a>.</p>`, 0, "See [here](https://e.example/a_%28b%29)."},
		{"link without text is dropped", `<p><a href="https://e.example/"></a>x</p>`, 0, "x"},
		{"inline escaping", `<p>a*b_c [d] \e &lt;f&gt; ~g</p>`, 0, `a\*b\_c \[d\] \\e \<f> \~g`},
		{"line starts escaped", `<p># not a heading</p><p>1. not a list</p><p>- nor this</p>`, 0, "\\# not a heading\n\n1\\. not a list\n\n\\- nor this"},
		{"br is a hard break", `<p>line one<br>line two</p>`, 0, "line one\\\nline two"},
		{"inline code", `<p>Run <code>go test</code> and <kbd>Ctrl</kbd></p>`, 0, "Run `go test` and `Ctrl`"},
		{"code containing a backtick", "<p><code>a`b</code></p>", 0, "``a`b``"},
		{"pre is fenced and unescaped", "<pre><code>func main() {\n\tprintln(\"*\")\n}\n</code></pre>", 0, "```\nfunc main() {\n\tprintln(\"*\")\n}\n```"},
		{"blockquote", `<blockquote><p>One</p><p>Two</p></blockquote>`, 0, "> One\n>\n> Two"},
		{"unordered list", `<ul><li>One</li><li>Two <em>b</em></li></ul>`, 0, "- One\n- Two *b*"},
		{"nested ordered list", `<ol><li>First<ul><li>Inner</li></ul></li><li>Second</li></ol>`, 0, "1. First\n\n   - Inner\n2. Second"},
		{"definition list", `<dl><dt>Term</dt><dd>Meaning</dd></dl>`, 0, "**Term**\n\nMeaning"},
		{"table", `<table><caption>Scores</caption><thead><tr><th>Name</th><th>Pts</th></tr></thead><tbody><tr><td>A|B</td><td>3</td></tr><tr><td>C</td></tr></tbody></table>`, 0,
			"Scores\n\n| Name | Pts |\n| --- | --- |\n| A\\|B | 3 |\n| C |  |"},
		{"figure with image", `<figure><img src="/later/img/abc" alt="A cat"><figcaption>Our cat</figcaption></figure>`, 0, "[A cat](https://e.example/cat.jpg)\n\nOur cat"},
		{"image without alt", `<p><img src="/later/img/abc"></p>`, 0, "[Image](https://e.example/cat.jpg)"},
		{"unknown image keeps only its alt", `<p><img src="/later/img/zzz" alt="">Text <img src="/later/img/zzz" alt="Gone"></p>`, 0, "Text Gone"},
		{"text-only inline elements", `<p><q>Hi</q> <span>there</span> <abbr title="x">HTML</abbr> <sup>2</sup></p>`, 0, "“Hi” there HTML 2"},
		{"rule", `<p>a</p><hr><p>b</p>`, 0, "a\n\n---\n\nb"},
		{"div mixes inline and blocks", `<div>Loose text<p>Para</p>tail</div>`, 0, "Loose text\n\nPara\n\ntail"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := htmlToMarkdown(c.html, image, c.shift); got != c.want {
				t.Errorf("htmlToMarkdown(%q)\n got: %q\nwant: %q", c.html, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/apps/later/ -run TestHTMLToMarkdown -count=1`
Expected: FAIL — `undefined: htmlToMarkdown`.

- [ ] **Step 3: Write the converter**

Create `internal/apps/later/markdown.go`:

```go
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
						cell := mdOneLine(strings.Join(c.blocks(td), " "))
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
		href := c.image(mdAttr(n, "src"))
		if href == "" {
			return alt
		}
		if alt == "" {
			alt = "Image"
		}
		return "[" + alt + "](" + mdLink(href) + ")"
	case atom.A:
		text := c.inlineChildren(n)
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/apps/later/ -run TestHTMLToMarkdown -count=1 -v`
Expected: PASS for every subtest. If a case fails, fix the converter, not
the expectation — unless the expectation is wrong Markdown (check it in a
CommonMark renderer such as https://spec.commonmark.org/dingus/ before
changing it, and say why in the commit).

- [ ] **Step 5: Full check and commit**

```bash
git add internal/apps/later/markdown.go internal/apps/later/markdown_internal_test.go
git commit -m "feat(later): convert saved article HTML to Markdown"
```

---

### Task 2: Download as Markdown

**Files:**
- Create: `internal/apps/later/download.go`
- Create: `internal/apps/later/download_internal_test.go`
- Create: `internal/apps/later/download_test.go`
- Modify: `internal/apps/later/images_store.go` (add `ImageSources`)
- Modify: `internal/apps/later/later.go` (route)
- Modify: `internal/apps/later/templates/article.html` (⋯ menu link)
- Modify: `docs/user/later.md`

**Interfaces:**
- Consumes: `htmlToMarkdown`, `mdEscape`, `mdParagraph`, `mdPrefix`
  (Task 1); `Store.Article`, `Store.ArticleTags(ctx, userID, id)`,
  `Store.Highlights(ctx, docID)`, `ImagePathPrefix` (`"/later/img/"`).
- Produces:
  - `func (st *Store) ImageSources(ctx context.Context, articleID int64) (map[string]string, error)`
    — hash → source URL for one article; no ownership check (Task 3 uses it).
  - `func imageLink(sources map[string]string) func(src string) string`
  - `func articleMarkdown(art Article, tags []string, hls []Highlight, sources map[string]string) string`
  - Route `GET /later/a/{id}/markdown`.

- [ ] **Step 1: Write the failing golden test**

Create `internal/apps/later/download_internal_test.go`:

```go
package later

import (
	"testing"
	"time"
)

func TestArticleMarkdown(t *testing.T) {
	saved := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	day := saved.Local().Format("2 January 2006")
	art := Article{
		URL:         "https://essays.example.com/reading-slowly",
		Title:       "Reading *slowly*",
		Byline:      "Mara Lind",
		Content:     ContentExtracted,
		Note:        "Stop skimming.\n\n1. Mark three things",
		ContentHTML: `<h1>Intro</h1><p>Read <em>slowly</em>.</p><p><img src="/later/img/h1" alt="A desk"></p>`,
		SavedAt:     saved,
	}
	hls := []Highlight{
		{Quote: "Read slowly.", Comment: "Yes."},
		{Quote: "1. not a list"},
	}
	sources := map[string]string{"h1": "https://essays.example.com/desk.jpg"}

	want := "# Reading \\*slowly\\*\n\n" +
		"Source: <https://essays.example.com/reading-slowly>\\\n" +
		"Author: Mara Lind\\\n" +
		"Saved: " + day + "\\\n" +
		"Tags: essays, reading\n\n" +
		"## Note\n\n" +
		"Stop skimming.\n\n1. Mark three things\n\n" +
		"## Highlights\n\n" +
		"> Read slowly.\n\n" +
		"Yes.\n\n" +
		"> 1\\. not a list\n\n" +
		"## Article\n\n" +
		"### Intro\n\n" +
		"Read *slowly*.\n\n" +
		"[A desk](https://essays.example.com/desk.jpg)\n"
	if got := articleMarkdown(art, []string{"essays", "reading"}, hls, sources); got != want {
		t.Errorf("articleMarkdown\n got: %q\nwant: %q", got, want)
	}
}

func TestArticleMarkdownLinkOnly(t *testing.T) {
	saved := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	art := Article{URL: "https://news.example.com/libraries", Title: "Libraries", Content: ContentLinkOnly, SavedAt: saved}
	want := "# Libraries\n\n" +
		"Source: <https://news.example.com/libraries>\\\n" +
		"Saved: " + saved.Local().Format("2 January 2006") + "\n"
	if got := articleMarkdown(art, nil, nil, nil); got != want {
		t.Errorf("articleMarkdown\n got: %q\nwant: %q", got, want)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/apps/later/ -run TestArticleMarkdown -count=1`
Expected: FAIL — `undefined: articleMarkdown`.

- [ ] **Step 3: Add `Store.ImageSources`**

Append to `internal/apps/later/images_store.go`:

```go
// ImageSources maps each of an article's image hashes to the URL it was
// downloaded from. Like Highlights it doesn't check ownership: callers load
// the article owner-scoped first.
func (st *Store) ImageSources(ctx context.Context, articleID int64) (map[string]string, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT i.hash, i.src_url FROM later_article_images ai
		  JOIN later_images i ON i.hash = ai.hash
		 WHERE ai.article_id = ?`, articleID)
	if err != nil {
		return nil, fmt.Errorf("later: image sources: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var hash, src string
		if err := rows.Scan(&hash, &src); err != nil {
			return nil, fmt.Errorf("later: scan image source: %w", err)
		}
		out[hash] = src
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("later: image sources: %w", err)
	}
	return out, nil
}
```

(`images_store.go` already imports `context` and `fmt`; check and add if not.)

- [ ] **Step 4: Write `download.go`**

Create `internal/apps/later/download.go`:

```go
package later

import (
	"net/http"
	"strings"
)

// imageLink maps a snapshot's /later/img/{hash} src to the image's source
// URL, for Markdown's image links; "" when the hash isn't the article's.
func imageLink(sources map[string]string) func(src string) string {
	return func(src string) string {
		if hash, ok := strings.CutPrefix(src, ImagePathPrefix); ok {
			return sources[hash]
		}
		return ""
	}
}

// articleMarkdown is the "Download as Markdown" file (spec: "Export"):
// title, where it came from, the note, the highlights with their comments,
// then the article. The note and comments go in as written — they are the
// user's own words and may already be Markdown — while everything taken
// from the page is escaped.
func articleMarkdown(art Article, tags []string, hls []Highlight, sources map[string]string) string {
	parts := []string{"# " + mdEscape(mdOneLine(art.Title))}

	meta := []string{"Source: <" + art.URL + ">"}
	if b := mdOneLine(art.Byline); b != "" {
		meta = append(meta, "Author: "+mdEscape(b))
	}
	meta = append(meta, "Saved: "+art.SavedAt.Local().Format("2 January 2006"))
	if len(tags) > 0 {
		meta = append(meta, "Tags: "+mdEscape(strings.Join(tags, ", ")))
	}
	parts = append(parts, strings.Join(meta, "\\\n"))

	if art.Note != "" {
		parts = append(parts, "## Note", art.Note)
	}
	if len(hls) > 0 {
		parts = append(parts, "## Highlights")
		for _, h := range hls {
			parts = append(parts, mdPrefix(mdParagraph(mdEscape(h.Quote)), "> ", ">"))
			if h.Comment != "" {
				parts = append(parts, h.Comment)
			}
		}
	}
	if body := htmlToMarkdown(art.ContentHTML, imageLink(sources), 2); body != "" {
		parts = append(parts, "## Article", body)
	}
	return strings.Join(parts, "\n\n") + "\n"
}

// markdown downloads one article as later-article.md: a generic name, as
// everywhere in the suite, never one built from the title. Downloading
// isn't reading, so it doesn't mark the article opened.
func (a *App) markdown(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	art, err := a.store.Article(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	tags, err := a.store.ArticleTags(r.Context(), userID, art.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	hls, err := a.store.Highlights(r.Context(), art.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	sources, err := a.store.ImageSources(r.Context(), art.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="later-article.md"`)
	_, _ = w.Write([]byte(articleMarkdown(art, tags, hls, sources)))
}
```

- [ ] **Step 5: Run the golden tests**

Run: `go test ./internal/apps/later/ -run TestArticleMarkdown -count=1`
Expected: PASS.

- [ ] **Step 6: Write the failing handler tests**

Create `internal/apps/later/download_test.go`:

```go
package later_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestDownloadMarkdown(t *testing.T) {
	s := newServer(t)
	ctx, uid := context.Background(), s.Alice.User.ID
	art := seed(t, s, uid, later.NewArticle{
		URL: "https://essays.example.com/slow", Title: "Reading slowly",
		ContentHTML: "<p>The quick brown fox</p><p><img src=\"/later/img/h1\" alt=\"Fox\"></p>",
		Images:      map[string]string{"h1": "https://essays.example.com/fox.jpg"},
		Tags:        []string{"essays"},
	})
	if _, err := s.Store.AddHighlight(ctx, art.ID, art.ContentText, 4, 9, "quick", "Nice word."); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetNote(ctx, uid, art.ID, "Worth a re-read."); err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", fmt.Sprintf("/later/a/%d/markdown", art.ID), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET markdown = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="later-article.md"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"# Reading slowly\n",
		"Source: <https://essays.example.com/slow>",
		"Tags: essays",
		"## Note\n\nWorth a re-read.",
		"## Highlights\n\n> quick\n\nNice word.",
		"## Article\n\nThe quick brown fox\n\n[Fox](https://essays.example.com/fox.jpg)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("markdown is missing %q:\n%s", want, body)
		}
	}

	// Downloading isn't reading: the article stays unread.
	got, err := s.Store.Article(ctx, uid, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != later.StateUnread {
		t.Errorf("state after download = %q, want unread", got.State)
	}
}

func TestDownloadMarkdownIsOwnerScoped(t *testing.T) {
	s := newServer(t)
	art := seed(t, s, s.Alice.User.ID, later.NewArticle{URL: "https://a.example/1", Title: "Mine", ContentHTML: words(5)})
	path := fmt.Sprintf("/later/a/%d/markdown", art.ID)
	if rec := s.Do(t, s.Bob, httptest.NewRequest("GET", path, nil)); rec.Code != http.StatusNotFound {
		t.Errorf("Bob GET %s = %d, want 404", path, rec.Code)
	}
	if rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/later/a/999/markdown", nil)); rec.Code != http.StatusNotFound {
		t.Errorf("missing article = %d, want 404", rec.Code)
	}
	if rec := s.Do(t, nil, httptest.NewRequest("GET", path, nil)); rec.Code != http.StatusSeeOther {
		t.Errorf("anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

func TestReadingViewMenuOffersMarkdownDownload(t *testing.T) {
	s := newServer(t)
	art := seed(t, s, s.Alice.User.ID, later.NewArticle{URL: "https://a.example/1", Title: "Mine", ContentHTML: words(5)})
	doc := s.Get(t, s.Alice, fmt.Sprintf("/later/a/%d", art.ID))
	link := doc.MustHave(fmt.Sprintf(`a[href="/later/a/%d/markdown"]`, art.ID))
	if _, ok := htmlassert.Attr(link, "download"); !ok {
		t.Error("Download as Markdown link has no download attribute")
	}
	if got := strings.TrimSpace(htmlassert.Text(link)); got != "Download as Markdown" {
		t.Errorf("link text = %q", got)
	}
}
```

- [ ] **Step 7: Run them to verify they fail**

Run: `go test ./internal/apps/later/ -run 'TestDownloadMarkdown|TestReadingViewMenuOffersMarkdownDownload' -count=1`
Expected: FAIL — 404 for the route, and no link in the menu.

- [ ] **Step 8: Add the route and the menu link**

In `internal/apps/later/later.go`, `Mount`, after the `POST /a/{id}/tags` line:

```go
	r.HandleFunc("GET /a/{id}/markdown", a.markdown)
```

In `internal/apps/later/templates/article.html`, in the ⋯ menu, between the
tags `</form>` and the `{{/* The href is safe … */}}` comment:

```html
				<a href="/later/a/{{$d.ID}}/markdown" download>Download as Markdown</a>
```

- [ ] **Step 9: Run the tests**

Run: `go test ./internal/apps/later/ -count=1`
Expected: PASS.

- [ ] **Step 10: Update the user guide**

In `docs/user/later.md`, "Reading an article", replace the ⋯ bullet:

```markdown
- **⋯** — change the article's **Tags**, **Download as Markdown**, **Open
  original** to see the page on its own site, or **Delete** the article.
```

Add a section before "## Deleting an article":

```markdown
## Downloading an article

To keep a copy outside ON Suite, open the article, choose **⋯**, then
**Download as Markdown**. You get a file called `later-article.md` with
the title, the original address, your note, your highlights with their
comments, and then the article itself. Pictures become links to where
they came from. Markdown is plain text, so any text editor opens it, and
note-taking apps such as Obsidian show it formatted.
```

- [ ] **Step 11: Full check and commit**

```bash
git add internal/apps/later docs/user/later.md
git commit -m "feat(later): download an article as Markdown"
```

---

### Task 3: `onsuite export`

**Files:**
- Create: `internal/apps/later/export.go`
- Create: `internal/apps/later/export_store_test.go`
- Modify: `internal/apps/later/later.go` (interface assertion)
- Modify: `cmd/onsuite/export_test.go`
- Modify: `docs/user/later.md`, `docs/user/admin.md`, `docs/user/notes.md`,
  `docs/user/paste.md`, `docs/user/reader.md`, `docs/self-hosting/deploying.md`

**Interfaces:**
- Consumes: `Store.ImageSources` (Task 2), `scanArticle`, `articleColumns`,
  `Store.Highlights`, `Store.ArticleTags`, `Store.TagNames`, `ImagePathPrefix`.
- Produces: `func (a *App) Export(ctx context.Context, handle *sql.DB, userID int64) (any, error)`;
  `func (st *Store) Export(ctx context.Context, userID int64) (exportPayload, error)`;
  `func exportHTML(contentHTML string, sources map[string]string) string`.

- [ ] **Step 1: Write the failing test**

Create `internal/apps/later/export_store_test.go`:

```go
package later_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
)

// exported mirrors the JSON shape onsuite export writes for ON Later.
type exported struct {
	Articles []struct {
		URL          string     `json:"url"`
		Title        string     `json:"title"`
		State        string     `json:"state"`
		Content      string     `json:"content"`
		ExtractError string     `json:"extract_error"`
		ContentHTML  string     `json:"content_html"`
		Note         string     `json:"note"`
		Tags         []string   `json:"tags"`
		SavedAt      time.Time  `json:"saved_at"`
		OpenedAt     *time.Time `json:"opened_at"`
		ArchivedAt   *time.Time `json:"archived_at"`
		Highlights   []struct {
			Quote   string `json:"quote"`
			Comment string `json:"comment"`
			Start   int    `json:"start"`
			End     int    `json:"end"`
		} `json:"highlights"`
	} `json:"articles"`
	Tags []string `json:"tags"`
}

func exportFor(t *testing.T, f *fixture, userID int64) (exported, string) {
	t.Helper()
	payload, err := later.New().Export(context.Background(), f.db, userID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("export payload does not marshal: %v", err)
	}
	var out exported
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out, string(raw)
}

func TestExportCarriesArticlesHighlightsNotesAndTags(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	chart, _, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{
		URL: "https://a.example/chart", Title: "Chart",
		ContentHTML: `<p>See the chart.</p><p><img src="/later/img/h1" alt="Chart"></p>`,
		Images:      map[string]string{"h1": "https://a.example/chart.png?w=1&h=2"},
		Tags:        []string{"data", "work"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddHighlight(ctx, chart.ID, chart.ContentText, 4, 13, "the chart", "Look again."); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetNote(ctx, f.alice.ID, chart.ID, "A note."); err != nil {
		t.Fatal(err)
	}
	f.tick()
	if err := f.store.MarkOpened(ctx, f.alice.ID, chart.ID); err != nil {
		t.Fatal(err)
	}
	f.tick()
	if _, _, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{
		URL: "https://a.example/paywall", Title: "Paywalled", ExtractError: "Couldn't find an article on the page.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.store.Save(ctx, f.bob.ID, later.NewArticle{URL: "https://b.example/1", Title: "Bob's", ContentHTML: "<p>x</p>"}); err != nil {
		t.Fatal(err)
	}

	out, raw := exportFor(t, f, f.alice.ID)
	if len(out.Articles) != 2 {
		t.Fatalf("got %d articles, want Alice's 2:\n%s", len(out.Articles), raw)
	}
	if strings.Contains(raw, "Bob's") {
		t.Error("Alice's export contains Bob's article")
	}

	a := out.Articles[0] // oldest first
	if a.URL != "https://a.example/chart" || a.Title != "Chart" || a.State != "reading" || a.Content != "extracted" {
		t.Errorf("first article = %+v", a)
	}
	if a.Note != "A note." {
		t.Errorf("note = %q", a.Note)
	}
	if strings.Join(a.Tags, ",") != "data,work" {
		t.Errorf("tags = %v", a.Tags)
	}
	if !strings.Contains(a.ContentHTML, `src="https://a.example/chart.png?w=1&amp;h=2"`) || strings.Contains(a.ContentHTML, "/later/img/") {
		t.Errorf("content_html images not pointed back at their sources: %s", a.ContentHTML)
	}
	if len(a.Highlights) != 1 || a.Highlights[0].Quote != "the chart" || a.Highlights[0].Comment != "Look again." ||
		a.Highlights[0].Start != 4 || a.Highlights[0].End != 13 {
		t.Errorf("highlights = %+v", a.Highlights)
	}
	if !a.SavedAt.Equal(time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)) || a.OpenedAt == nil || a.ArchivedAt != nil {
		t.Errorf("times: saved %v opened %v archived %v", a.SavedAt, a.OpenedAt, a.ArchivedAt)
	}

	b := out.Articles[1]
	if b.Content != "link_only" || b.ExtractError == "" || b.ContentHTML != "" {
		t.Errorf("link-only article = %+v", b)
	}
	if b.Tags == nil || b.Highlights == nil {
		t.Error("an article without tags or highlights exports null instead of []")
	}
	if strings.Join(out.Tags, ",") != "data,work" {
		t.Errorf("top-level tags = %v", out.Tags)
	}
}

func TestExportOfAnEmptyAccount(t *testing.T) {
	f := newFixture(t)
	_, raw := exportFor(t, f, f.alice.ID)
	if raw != `{"articles":[],"tags":[]}` {
		t.Errorf("empty export = %s", raw)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/apps/later/ -run TestExport -count=1`
Expected: FAIL — `later.New().Export undefined`.

- [ ] **Step 3: Write `export.go`**

Create `internal/apps/later/export.go`:

```go
package later

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"strings"
	"time"
)

// exportedArticle, exportedHighlight and exportPayload are the shapes
// `onsuite export` writes. They are declared here, apart from the store's
// types, so the backup format changes only when someone edits this file.
type exportedHighlight struct {
	Quote     string    `json:"quote"`
	Comment   string    `json:"comment,omitempty"`
	Start     int       `json:"start"` // code-point offsets into the article's text
	End       int       `json:"end"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type exportedArticle struct {
	URL          string              `json:"url"`
	Title        string              `json:"title"`
	SiteName     string              `json:"site_name,omitempty"`
	Byline       string              `json:"byline,omitempty"`
	State        State               `json:"state"`
	Content      Content             `json:"content"`
	ExtractError string              `json:"extract_error,omitempty"`
	ContentHTML  string              `json:"content_html,omitempty"`
	Note         string              `json:"note,omitempty"`
	Tags         []string            `json:"tags"`
	Highlights   []exportedHighlight `json:"highlights"`
	SavedAt      time.Time           `json:"saved_at"`
	OpenedAt     *time.Time          `json:"opened_at,omitempty"`
	ArchivedAt   *time.Time          `json:"archived_at,omitempty"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

type exportPayload struct {
	Articles []exportedArticle `json:"articles"`
	Tags     []string          `json:"tags"`
}

// Export implements app.Exporter, joining ON Later to onsuite export's
// whole-account JSON backup. Image bytes are left out; each image's
// source URL is kept in the article's HTML (spec: "Export").
func (a *App) Export(ctx context.Context, handle *sql.DB, userID int64) (any, error) {
	return NewStore(handle).Export(ctx, userID)
}

// Export gathers one user's articles, oldest saved first.
func (st *Store) Export(ctx context.Context, userID int64) (exportPayload, error) {
	arts, err := st.allArticles(ctx, userID)
	if err != nil {
		return exportPayload{}, err
	}
	out := exportPayload{Articles: []exportedArticle{}}
	for _, art := range arts {
		tags, err := st.ArticleTags(ctx, userID, art.ID)
		if err != nil {
			return exportPayload{}, err
		}
		hls, err := st.Highlights(ctx, art.ID)
		if err != nil {
			return exportPayload{}, err
		}
		sources, err := st.ImageSources(ctx, art.ID)
		if err != nil {
			return exportPayload{}, err
		}
		e := exportedArticle{
			URL: art.URL, Title: art.Title, SiteName: art.SiteName, Byline: art.Byline,
			State: art.State, Content: art.Content, ExtractError: art.ExtractError,
			ContentHTML: exportHTML(art.ContentHTML, sources), Note: art.Note,
			Tags: append([]string{}, tags...), Highlights: []exportedHighlight{},
			SavedAt: art.SavedAt, OpenedAt: optionalTime(art.OpenedAt),
			ArchivedAt: optionalTime(art.ArchivedAt), UpdatedAt: art.UpdatedAt,
		}
		for _, h := range hls {
			e.Highlights = append(e.Highlights, exportedHighlight{
				Quote: h.Quote, Comment: h.Comment, Start: h.Start, End: h.End,
				CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt,
			})
		}
		out.Articles = append(out.Articles, e)
	}
	tags, err := st.TagNames(ctx, userID)
	if err != nil {
		return exportPayload{}, err
	}
	out.Tags = append([]string{}, tags...)
	return out, nil
}

// allArticles loads every one of userID's articles, closing its rows
// before returning: the database has one connection, and Export queries
// again per article.
func (st *Store) allArticles(ctx context.Context, userID int64) ([]Article, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+articleColumns+` FROM later_articles WHERE user_id = ? ORDER BY saved_at, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("later: export articles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Article
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("later: export articles: %w", err)
	}
	return out, nil
}

// exportHTML points a snapshot's images back at their sources, so an
// exported article reads on its own, without ON Suite. Matching the quoted
// attribute value keeps every other byte of the snapshot as stored.
func exportHTML(contentHTML string, sources map[string]string) string {
	if len(sources) == 0 {
		return contentHTML
	}
	pairs := make([]string, 0, 2*len(sources))
	for hash, src := range sources {
		pairs = append(pairs, `"`+ImagePathPrefix+hash+`"`, `"`+html.EscapeString(src)+`"`)
	}
	return strings.NewReplacer(pairs...).Replace(contentHTML)
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
```

If `scanArticle`'s first error path (`sql.ErrNoRows` → `ErrNotFound`) is
never hit from `rows.Next()`, that is fine; it is only reachable from
`QueryRow`.

In `internal/apps/later/later.go`, extend the interface assertions:

```go
var (
	_ app.App       = (*App)(nil)
	_ app.Scheduler = (*App)(nil)
	_ app.Exporter  = (*App)(nil)
)
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/apps/later/ -run TestExport -count=1`
Expected: PASS.

- [ ] **Step 5: Pin it in the command's test**

In `cmd/onsuite/export_test.go`, `TestExportCmdWritesJSONToStdout`, after
the `paste` check:

```go
	if _, ok := doc.Apps["later"]; !ok {
		t.Errorf("no later key in apps: %v", doc.Apps)
	}
```

Run: `go test ./cmd/onsuite/ -run TestExportCmd -count=1`
Expected: PASS.

- [ ] **Step 6: Update the docs**

The sentence "export your ON Notes, ON Paste, ON Reader and ON Flash data"
appears in `docs/user/notes.md`, `docs/user/paste.md`,
`docs/user/reader.md` (and the admin guide's "This writes one person's ON
Notes, ON Paste, ON Reader and ON Flash data"). Make each list read
"ON Notes, ON Paste, ON Reader, ON Later and ON Flash" (re-wrap the line;
`grep -rn "ON Reader and ON Flash" docs/` finds them all).

In `docs/user/admin.md`, "Exporting someone's data", after the sentence
about ON Flash pictures, add:

```markdown
ON Later articles come with their text, highlights, comments, notes and
tags; their pictures aren't included, only the web address each one came
from.
```

In `docs/self-hosting/deploying.md`, "Exporting your data", extend the
paragraph's list of what is not included:

```markdown
Plain JSON, readable without this software. Shared ON Notes bullets keep
their share link in the file, so treat the file as private. ON Paste's
share links aren't included, and neither are the files uploaded to ON Flash
cards (cards keep the web address of any picture or sound added from one)
or the pictures in ON Later articles (each keeps the address it came from);
use a snapshot if you need a fully restorable copy.
```

In `docs/user/later.md`, at the end of "Downloading an article":

```markdown
The admin can also export all your ON Notes, ON Paste, ON Reader, ON Later
and ON Flash data as a single file for you. Ask them if you'd like a
complete copy (the [Admin guide](admin.md#exporting-someones-data)
explains how).
```

- [ ] **Step 7: Full check and commit**

```bash
git add internal/apps/later cmd/onsuite/export_test.go docs/user docs/self-hosting/deploying.md
git commit -m "feat(later): include ON Later in onsuite export"
```

---

### Task 4: The admin card

**Files:**
- Create: `internal/apps/later/stats.go`
- Create: `internal/apps/later/stats_test.go`
- Modify: `internal/apps/later/later.go` (interface assertion)
- Modify: `docs/user/admin.md`

**Interfaces:**
- Consumes: `app.Stat{Label, Value, Hint string}`.
- Produces: `func (a *App) Stats(ctx context.Context, handle *sql.DB) ([]app.Stat, error)`;
  `func (st *Store) Stats(ctx context.Context) ([]app.Stat, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/apps/later/stats_test.go`:

```go
package later_test

import (
	"context"
	"slices"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

const storedImagesHint = "pictures kept with saved articles, for everyone"

func TestStatsCountEveryonesArticles(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	pic, _, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{
		URL: "https://a.example/pic", Title: "Pic", ContentHTML: "<p>body</p>",
		Images: map[string]string{"h1": "https://a.example/1.png", "h2": "https://a.example/2.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveImageBytes(ctx, "h1", "image/png", make([]byte, 2048)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddHighlight(ctx, pic.ID, pic.ContentText, 0, 4, "body", ""); err != nil {
		t.Fatal(err)
	}
	reading := f.save(t, "https://a.example/reading", "Reading")
	if err := f.store.MarkOpened(ctx, f.alice.ID, reading.ID); err != nil {
		t.Fatal(err)
	}
	done := f.save(t, "https://a.example/done", "Done")
	if err := f.store.SetState(ctx, f.alice.ID, done.ID, later.StateArchived); err != nil {
		t.Fatal(err)
	}
	bobs, _, err := f.store.Save(ctx, f.bob.ID, later.NewArticle{URL: "https://b.example/1", Title: "Bob's", ContentHTML: "<p>body</p>"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddHighlight(ctx, bobs.ID, bobs.ContentText, 0, 4, "body", "Mine."); err != nil {
		t.Fatal(err)
	}

	got, err := later.New().Stats(ctx, f.db)
	if err != nil {
		t.Fatal(err)
	}
	want := []app.Stat{
		{Label: "Articles", Value: "4"},
		{Label: "Unread", Value: "2"},
		{Label: "Reading", Value: "1"},
		{Label: "Archived", Value: "1"},
		{Label: "Highlights", Value: "2"},
		{Label: "Stored images", Value: "2.0 KiB", Hint: storedImagesHint},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Stats =\n%v\nwant\n%v", got, want)
	}
}

func TestStatsOfAnEmptyInstall(t *testing.T) {
	f := newFixture(t)
	got, err := f.store.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []app.Stat{
		{Label: "Articles", Value: "0"},
		{Label: "Unread", Value: "0"},
		{Label: "Reading", Value: "0"},
		{Label: "Archived", Value: "0"},
		{Label: "Highlights", Value: "0"},
		{Label: "Stored images", Value: "0 B", Hint: storedImagesHint},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Stats =\n%v\nwant\n%v", got, want)
	}
}
```

(Check `SaveImageBytes`'s signature in `images_store.go` —
`(ctx, hash, contentType string, b []byte) error` at the time of writing.)

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/apps/later/ -run TestStats -count=1`
Expected: FAIL — `later.New().Stats undefined`.

- [ ] **Step 3: Write `stats.go`**

Create `internal/apps/later/stats.go`:

```go
package later

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// Stats implements app.Stater: ON Later's card on the admin page.
func (a *App) Stats(ctx context.Context, handle *sql.DB) ([]app.Stat, error) {
	return NewStore(handle).Stats(ctx)
}

// Stats describes ON Later across every user (spec: "Admin card"):
// articles by state, highlights, and how much the kept images weigh.
func (st *Store) Stats(ctx context.Context) ([]app.Stat, error) {
	var total, unread, reading, archived, highlights, imageBytes int64
	if err := st.db.QueryRowContext(ctx, `
		SELECT count(*),
		       coalesce(sum(state = 'unread'), 0),
		       coalesce(sum(state = 'reading'), 0),
		       coalesce(sum(state = 'archived'), 0)
		  FROM later_articles`).Scan(&total, &unread, &reading, &archived); err != nil {
		return nil, fmt.Errorf("later: stats articles: %w", err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT count(*) FROM later_highlights`).Scan(&highlights); err != nil {
		return nil, fmt.Errorf("later: stats highlights: %w", err)
	}
	if err := st.db.QueryRowContext(ctx,
		`SELECT coalesce(sum(length(bytes)), 0) FROM later_images`).Scan(&imageBytes); err != nil {
		return nil, fmt.Errorf("later: stats images: %w", err)
	}
	n := func(v int64) string { return strconv.FormatInt(v, 10) }
	return []app.Stat{
		{Label: "Articles", Value: n(total)},
		{Label: "Unread", Value: n(unread)},
		{Label: "Reading", Value: n(reading)},
		{Label: "Archived", Value: n(archived)},
		{Label: "Highlights", Value: n(highlights)},
		{Label: "Stored images", Value: humanBytes(imageBytes), Hint: "pictures kept with saved articles, for everyone"},
	}, nil
}

// humanBytes renders a byte count the way a person reads one. It mirrors
// ON Paste's own humanBytes: apps never import each other, so this is an
// independent copy (PATTERNS.md, "Cross-app mirroring").
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
```

In `internal/apps/later/later.go`, add `_ app.Stater = (*App)(nil)` to the
assertion block.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/apps/later/ -run TestStats -count=1`
Expected: PASS.

- [ ] **Step 5: Update the admin guide**

In `docs/user/admin.md`, "What the app numbers mean", after the ON Reader
bullet:

```markdown
- **ON Later** — **Articles** saved by everyone, split into **Unread**,
  **Reading** and **Archived**, the number of **Highlights**, and **Stored
  images** (how much space the pictures kept with saved articles take).
```

In the jobs list, after **purge orphan media and tags**, add the job ON
Later has had since L1 but the guide never listed:

```markdown
- **download images** — every 10 minutes, downloads the pictures in saved
  ON Later articles that haven't been stored yet, so an article keeps its
  pictures even if the original site removes them.
```

- [ ] **Step 6: Full check and commit**

```bash
git add internal/apps/later docs/user/admin.md
git commit -m "feat(later): ON Later card on the admin page"
```

---

### Task 5: Demo articles, screenshots and the app lists

**Files:**
- Create: `docs/screenshots/seed/later.go`
- Create: `docs/screenshots/seed/fixtures/later/reading-slowly.html`,
  `sourdough.html`, `spring-sky.html`, `short-memory.html`, `small-software.html`
- Modify: `docs/screenshots/seed/seed.go`, `docs/screenshots/seed/seed_test.go`
- Modify: `docs/screenshots/capture/shots.go`
- Create (generated): `docs/user/images/later-list.png`, `later-search.png`,
  `later-reading.png`, `later-notes.png`; `docs/images/app-later-light.png`,
  `app-later-dark.png`
- Re-shoot: `docs/images/hero-light.png`, `hero-dark.png`,
  `docs/user/images/dashboard.png`
- Modify: `docs/user/later.md`, `README.md`, `AGENTS.md`,
  `docs/developers/index.md`, `docs/developers/repository-layout.md`

**Interfaces:**
- Consumes: `later.Store` — `Save`, `AddHighlight(ctx, docID, text, start, end, quote, comment)`,
  `SetNote`, `MarkOpened`, `SetProgress`, `SetState`, `SetClock`,
  `Counts(ctx, userID, tag) (map[State]int, error)`, `ImagesToFetch(ctx, limit)`,
  `Article`, `Highlights`; seed's `at`, `day`, `fixtures`.
- Produces: demo data where article ID 1 is "The case for reading slowly"
  (reading, three highlights, a note).

- [ ] **Step 1: Write the failing seed test**

In `docs/screenshots/seed/seed_test.go`, `TestSeedFillsEveryApp`, before the
Flash block, add (import `later`):

```go
	// Later: articles in every state, one link-only, highlights and a
	// note — and nothing for the image job to fetch: the demo must never
	// reach the network.
	ls := later.NewStore(handle)
	counts, err := ls.Counts(ctx, demo.ID, "")
	if err != nil || counts[later.StateUnread] < 3 || counts[later.StateReading] < 1 || counts[later.StateArchived] < 1 {
		t.Errorf("later counts = %v, %v; want unread >= 3, reading >= 1, archived >= 1", counts, err)
	}
	if hl, err := ls.Highlights(ctx, 1); err != nil || len(hl) < 3 {
		t.Errorf("later article 1 highlights = %d, %v; want >= 3", len(hl), err)
	}
	if imgs, err := ls.ImagesToFetch(ctx, 100); err != nil || len(imgs) != 0 {
		t.Errorf("later images to fetch = %d, %v; want none", len(imgs), err)
	}
```

Extend the shot-ID map and pattern:

```go
var shotIDs = map[string]map[int64]string{
	"paste":  {3: "Home server docker-compose", 7: "Japan trip packing list", 8: "Retry with backoff"},
	"notes":  {25: "Before we go"},
	"reader": {10: "The Orionids peak this month: how to watch"},
	"flash":  {1: "Japanese travel phrases", 2: "F1 circuits"},
	"later":  {1: "The case for reading slowly"},
}

// shotIDRe finds the seeded IDs in shots.go URLs: /paste/3, /notes/25,
// /reader/item/10, /flash/2/cards/, /flash/review/2, /later/a/1.
var shotIDRe = regexp.MustCompile(`URL: "/(paste|notes|reader/item|flash(?:/review)?|later/a)/(\d+)`)
```

and add to `lookup` in `TestShotIDsPointAtTheIntendedItems`:

```go
		"later": func(id int64) (string, error) {
			a, err := later.NewStore(handle).Article(ctx, demo.ID, id)
			return a.Title, err
		},
```

Run: `go test ./docs/screenshots/seed/ -count=1`
Expected: FAIL — later counts are zero.

- [ ] **Step 2: Write the fixtures**

Create `docs/screenshots/seed/fixtures/later/reading-slowly.html`:

```html
<p>Most of what I read in a day, I read the way I cross a busy road: quickly, looking both ways, glad to reach the other side. Headlines, messages, the first paragraph of something a friend sent. That kind of reading has its place. It is how we keep up.</p>
<p>But some writing asks for more. A long essay, a careful argument, a story that takes its time. Read at headline speed, it slides past and leaves nothing behind. You finish it and could not say what it was about.</p>
<h2>Slow is a skill</h2>
<p>Reading slowly is not the same as reading badly. It means stopping at a sentence that surprised you and asking why. It means going back a paragraph when the thread slips. It means letting a good line sit for a moment before moving on.</p>
<p>None of this comes naturally on a screen built for scrolling. The page wants you to keep going; the next thing is always one flick away. <strong>A calm place to read is the first thing slow reading needs.</strong></p>
<blockquote><p>The point of reading is not to finish. It is to be changed a little by what you read.</p></blockquote>
<h2>Marking what matters</h2>
<p>The second thing is a pencil. Underlining a passage is a small act of attention: it tells your future self that this mattered, and it makes you decide whether it did. A note in the margin turns reading into a conversation.</p>
<p>Later, those marks are what you come back to. Not the whole article, but the three sentences that stayed with you and what you thought about them at the time.</p>
<h2>Try it this week</h2>
<ul><li>Pick one long piece you have been putting off.</li><li>Read it in one sitting, somewhere quiet.</li><li>Mark no more than three passages, and write one line about each.</li></ul>
<p>Then put it away. A week later, read only your marks. You may be surprised how much of the article comes back with them.</p>
```

`sourdough.html`:

```html
<p>My first sourdough starter died in a week. I fed it when I remembered, kept it somewhere cold, and expected bread by Saturday. The second one lived, mostly because I stopped expecting anything.</p>
<p>A starter is flour, water and time. The flour and water are easy. The time is the hard part: a few days of feeding at the same hour, watching for bubbles, smelling it change from paste to something sour and alive.</p>
<p>What it taught me is patience of a practical kind. Not waiting and hoping, but doing the same small thing every day and trusting it to add up. The bread, when it finally came, was flat and dense and the best thing I had baked.</p>
<p>These days the starter lives in the fridge and gets fed on Sundays. It has outlived two ovens. I still check it for bubbles, out of habit and a little affection.</p>
```

`spring-sky.html`:

```html
<p>Spring evenings are a good time to start looking up. The nights are still long, the air is often clear, and some of the easiest patterns in the sky are high overhead after dark.</p>
<p>Start with something you can find without a chart. The brightest planet is usually the first point of light to appear in the west after sunset. Once you have it, look for the bright stars around it and learn them one at a time.</p>
<p>Give your eyes twenty minutes away from screens and street lights. Faint stars appear one by one as your eyes adjust, and a sky that looked empty fills up.</p>
<p>Binoculars help more than most people expect. Sweep slowly along the band of the Milky Way and you will see clusters that no naked eye can separate.</p>
```

`short-memory.html`:

```html
<p>Every coach I have had said the same thing: the last point is gone. You cannot win it back by thinking about it. The players who do well in long matches are the ones who forget quickly.</p>
<p>That sounds simple and is not. A missed easy ball sticks. You replay it while the next serve is already in the air, and lose that point too.</p>
<p>The trick most good players use is a small routine between points: wipe the hand on the table, look at the racket, breathe out, decide on the next serve. It gives the mind something to do that is not the last mistake.</p>
<p>Short memory is not the same as not caring. It is caring about the right point, which is always the next one.</p>
```

`small-software.html`:

```html
<p>Small software is software one person can hold in their head. It does a few things, does them well, and can be read in an afternoon.</p>
<p>It is easy to underrate. Small tools rarely look impressive in a demo, and they do not grow a team around them. But they are the ones still running ten years later, because nobody was ever afraid to change them.</p>
<h2>Three habits</h2>
<ol><li>Say no to the feature that only one person wants once.</li><li>Keep one way of doing each thing.</li><li>Write down why, not just what.</li></ol>
<p>None of these are clever. They are a kind of patience with your own code: choosing to leave it smaller than you could make it.</p>
```

- [ ] **Step 3: Write `seedLater`**

Create `docs/screenshots/seed/later.go`:

```go
package main

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
)

// laterFixture is one demo article. body names a file under fixtures/later
// ("" saves it link-only, with extractErr). Times are days before now;
// zero means never. highlights are {quote, comment} pairs, each quote found
// in the article's text.
type laterFixture struct {
	url, title, site, byline, body, extractErr string
	tags                                       []string
	saved, opened, archived                    float64
	note                                       string
	highlights                                 [][2]string
}

// laterFixtures are saved in this order, so "The case for reading slowly"
// is article 1, the one the reading-view shots open (see shotIDs in
// seed_test.go). Nothing here has images or a favicon URL: the demo server
// must never fetch from the network.
var laterFixtures = []laterFixture{
	{
		url: "https://essays.example.com/reading-slowly", title: "The case for reading slowly",
		site: "Quiet Essays", byline: "Mara Lind", body: "reading-slowly.html",
		tags: []string{"essays", "reading"}, saved: 6, opened: 0.5,
		note: "Good reminder to stop skimming. Three marks per article feels like the right limit — any more and nothing stands out.",
		highlights: [][2]string{
			{"Reading slowly is not the same as reading badly.", "This is the whole argument in one line."},
			{"A calm place to read is the first thing slow reading needs.", ""},
			{"Underlining a passage is a small act of attention", "Patience with a pencil in hand. Try it with the next long essay."},
		},
	},
	{
		url: "https://dev.example.com/small-software", title: "Notes on building small software",
		site: "Dev Notes", byline: "Lena Hart", body: "small-software.html",
		tags: []string{"essays", "work"}, saved: 20, opened: 15, archived: 10,
		note: "Re-read when tempted to add another setting. Patience over features.",
		highlights: [][2]string{
			{"Small software is software one person can hold in their head.", "A good test for anything I build at home."},
			{"Keep one way of doing each thing.", ""},
		},
	},
	{
		url: "https://astro.example.net/spring-sky", title: "A beginner's guide to the spring night sky",
		site: "Backyard Astronomy", byline: "Priya Nair", body: "spring-sky.html",
		tags: []string{"science"}, saved: 3,
		highlights: [][2]string{
			{"Give your eyes twenty minutes away from screens and street lights.", "Try this from the back yard on Friday."},
		},
	},
	{
		url: "https://news.example.com/2026/city-libraries", title: "The future of city libraries",
		extractErr: "Couldn't find an article on the page: it asks you to sign in first.",
		tags:       []string{"reading"}, saved: 2,
	},
	{
		url: "https://kitchen.example.org/sourdough-patience", title: "What a sourdough starter taught me about patience",
		site: "Home Kitchen", byline: "Tom Avery", body: "sourdough.html",
		tags: []string{"cooking"}, saved: 1,
	},
	{
		url: "https://club.example.com/short-memory", title: "Why table tennis rewards a short memory",
		site: "Club Notes", byline: "Sam Okafor", body: "short-memory.html",
		tags: []string{"sport"}, saved: 0.2,
	},
}

func seedLater(ctx context.Context, st *later.Store, userID int64, now time.Time) error {
	ago := func(days float64) time.Time { return now.Add(-time.Duration(days * float64(day))) }
	for _, f := range laterFixtures {
		html := ""
		if f.body != "" {
			b, err := fixtures.ReadFile("fixtures/later/" + f.body)
			if err != nil {
				return err
			}
			html = string(b)
		}
		st.SetClock(at(ago(f.saved)))
		art, _, err := st.Save(ctx, userID, later.NewArticle{
			URL: f.url, Title: f.title, SiteName: f.site, Byline: f.byline,
			ContentHTML: html, ExtractError: f.extractErr, Tags: f.tags,
		})
		if err != nil {
			return err
		}
		for _, h := range f.highlights {
			start, ok := runeIndex(art.ContentText, h[0])
			if !ok {
				return fmt.Errorf("seed: %q is not in %q", h[0], f.title)
			}
			end := start + utf8.RuneCountInString(h[0])
			if _, err := st.AddHighlight(ctx, art.ID, art.ContentText, start, end, h[0], h[1]); err != nil {
				return err
			}
		}
		if f.note != "" {
			if err := st.SetNote(ctx, userID, art.ID, f.note); err != nil {
				return err
			}
		}
		if f.opened > 0 {
			st.SetClock(at(ago(f.opened)))
			if err := st.MarkOpened(ctx, userID, art.ID); err != nil {
				return err
			}
		}
		if f.archived > 0 {
			st.SetClock(at(ago(f.archived)))
			if err := st.SetState(ctx, userID, art.ID, later.StateArchived); err != nil {
				return err
			}
		}
	}
	return nil
}

// runeIndex is quote's offset in text in code points, the unit highlight
// offsets use.
func runeIndex(text, quote string) (int, bool) {
	i := strings.Index(text, quote)
	if i < 0 {
		return 0, false
	}
	return utf8.RuneCountInString(text[:i]), true
}
```

In `docs/screenshots/seed/seed.go`, add a step after `seedReader`:

```go
		func() error { return seedLater(ctx, later.NewStore(handle), demo.ID, now) },
```

and extend the package doc comment's content list if it names apps.

- [ ] **Step 4: Run the seed tests**

Run: `go test ./docs/screenshots/seed/ -count=1`
Expected: PASS (`TestShotIDsPointAtTheIntendedItems` passes before any
`later/a` URL exists in shots.go; it is pinned in the next step).

- [ ] **Step 5: Add the shots**

In `docs/screenshots/capture/shots.go`, after the Reader guide shots:

```go
	// docs/user/later.md
	// Article 1 is already Reading in the seed, so opening it changes no
	// tab. The search shot loads the results page directly (?q=).
	{Name: "docs/user/images/later-list.png", URL: "/later/", Height: 560},
	{Name: "docs/user/images/later-search.png", URL: "/later/?q=patience", Height: 520},
	{Name: "docs/user/images/later-reading.png", URL: "/later/a/1", Height: 760},
	{Name: "docs/user/images/later-notes.png", URL: "/later/a/1", Height: 760, Setup: `
		document.getElementById('later-notes-open').checked = true;
		await new Promise(r => setTimeout(r, 600));`},
```

and in the README block, after the Reader thumbnails:

```go
	{Name: "docs/images/app-later-light.png", URL: "/later/a/1"},
	{Name: "docs/images/app-later-dark.png", URL: "/later/a/1", Theme: "dark"},
```

Update the hero comment: "it shows all five apps at a glance."

Run: `go test ./docs/screenshots/... -count=1`
Expected: PASS.

- [ ] **Step 6: Capture**

Follow [docs/screenshots/README.md](../../screenshots/README.md), with the
version of the latest release (`git describe --tags --abbrev=0`, no leading
`v`) and `--only`:

```bash
VERSION=$(git describe --tags --abbrev=0 | sed 's/^v//')
SEED=$(mktemp -d)/demo
go run ./docs/screenshots/seed --data-dir $SEED
go build -ldflags "-X main.version=$VERSION" -o $SEED/onsuite ./cmd/onsuite
$SEED/onsuite serve --addr :8308 --data-dir $SEED &
SERVER=$!
until curl -fs http://localhost:8308/healthz >/dev/null; do sleep 0.2; done
go run ./docs/screenshots/capture --session-file $SEED/demo-session \
  --only later-list.png,later-search.png,later-reading.png,later-notes.png,app-later-light.png,app-later-dark.png,hero-light.png,hero-dark.png,dashboard.png
kill $SERVER
```

Look at every PNG (Read tool). Re-shoot with a different `Height` if
anything is cut off — in particular the hero (440px) must show all five
dashboard cards; raise its `Height` if the fifth card wraps below.
`later-reading.png` must show highlights and margin comments;
`later-notes.png` the open panel; `later-search.png` result rows with "In a
highlight" / "In your note" / "In the text" snippets. If `dashboard.png`
didn't change in a way a reader would notice, `git checkout` it.

- [ ] **Step 7: Put the screenshots in the guide**

In `docs/user/later.md`:

- At the end of "Finding your articles":
  `![The ON Later list: Unread, Reading and Archived tabs, tag chips, and rows with reading time, tags and a link-only article](images/later-list.png)`
- At the end of "Searching":
  `![Search results for "patience", each with a line showing whether it matched in a highlight, the note or the text](images/later-search.png)`
- After the top-bar list in "Reading an article":
  `![An article open in ON Later, with highlighted passages and their comments in the margin](images/later-reading.png)`
- After the first paragraph of "Highlights and notes":
  `![The Notes panel open beside the article, with the note at the top and every highlight listed below it](images/later-notes.png)`

- [ ] **Step 8: README, AGENTS and developer docs**

`README.md`:
- Hero alt: `The ON Suite dashboard, with a card for each app: ON Notes, ON Paste, ON Reader, ON Later and ON Flash`
- "notes, snippets, news feeds and flash cards" → "notes, snippets, news
  feeds, saved articles and flash cards".
- "Sign in once and move freely between four apps." → "five apps".
- A section between ON Reader and ON Flash:

```markdown
### ON Later

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/images/app-later-dark.png">
    <img src="docs/images/app-later-light.png" alt="ON Later: an essay open in a calm reading view, with highlighted passages and comments in the margin">
  </picture>
</p>

A private read-it-later shelf for writing that deserves more than a skim.
Save a page from its address, a bookmarklet or ON Reader, and ON Later
keeps a clean copy — text and pictures — to read in a quiet view.
Highlight passages, comment on them, keep a note on the whole article, and
download it all as Markdown.
[Read the ON Later guide →](docs/user/later.md)
```

`AGENTS.md`, "What this is": after the ON Reader entry, add
`**ON Later** (a read-it-later app: save pages, read them in a calm view,
highlight, comment and tag them, export as Markdown),` — re-wrap the
paragraph so the list still reads as one sentence ending with ON Flash.

`docs/developers/index.md`: "ON Paste, ON Notes, ON Reader and ON Flash" →
"ON Paste, ON Notes, ON Reader, ON Later and ON Flash".

`docs/developers/repository-layout.md`: in the tree, between `flash/`'s
`testdata/` line and `notes/`:

```text
│   │   ├── later/                ON Later: read-it-later — save, reading view, highlights, tags, search, export
│   │   │   ├── migrations/
│   │   │   ├── static/           later.js, highlight.js
│   │   │   └── templates/
```

- [ ] **Step 9: Full check and commit**

```bash
git add docs/screenshots docs/user docs/images README.md AGENTS.md docs/developers
git commit -m "docs(later): demo articles, screenshots and ON Later in the app lists"
```

---

## After the last task

- Run the app (`.claude/launch.json` / the `run` skill) against the scratch
  data dir and check in the browser, light and dark, desktop and phone
  width: the ⋯ menu shows **Download as Markdown** and the download is
  `later-article.md` with the expected content (open it); a link-only
  article downloads without `## Article`; `/admin/` shows the ON Later card;
  `/help/later` shows the new screenshots and section.
- Run `./onsuite export <user> --data-dir <dir>` against a database with
  ON Later articles and read the `later` section.
- PR: `feat(later): ON Later L4 — export, Markdown download and admin card (#485)`,
  body `Closes #485`, listing the "Decisions made in this plan" above. With
  L4 merged every phase of #481 is done; ask Ilia whether to close #481 and
  the "ON Later" milestone.
