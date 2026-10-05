# ON Later L2 — highlights, comments and the article note: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close #483: select text in the reading view to highlight it, with
an optional comment; click a highlight to edit its comment or delete it; a
plain-text note on the whole article; a slide-in Notes panel holding the
note and every highlight; the same note as a box after the last paragraph;
comments in the right margin on wide screens and a 💬 marker on narrow
ones; a highlight count on list rows.

**Architecture:** Builds on the merged L1 app (`internal/apps/later`). One
new migration (`0004_highlights.sql`). The *highlight unit* — storage and
offset validation (`highlight.go`), `<mark>` rendering
(`highlight_render.go`) and the selection script (`static/highlight.js`) —
knows only "a document ID and its plain text", so it can move to the
platform when ON Books needs it. Highlights are drawn on the server by
walking the parsed snapshot, so an article with highlights renders with no
JavaScript. Creating, editing and deleting highlights are HTMX form posts
that swap the article body and refresh the Notes panel, margin and count
out of band. The Notes panel is the checkbox-driven CSS pattern (opens
without JavaScript); the note saves on blur and refreshes its other copy
out of band.

**Tech Stack:** Go, SQLite, `html/template`, `golang.org/x/net/html`,
HTMX, plain JS (`later.js`, `highlight.js`).

**Spec:** [2026-10-05-on-later-design.md](../specs/2026-10-05-on-later-design.md)
(sections "The highlight unit", "Offsets are code points", "Reading view").
**Issue:** #483 (closes it). **Previous plan:**
[L1b](2026-10-05-on-later-l1b-reading-and-saving.md).

**Decisions already made (Ilia):**
- "Notes · N" counts highlights only; the note doesn't add to N.
- Deleting a highlight asks first (the app's confirm dialog) only when it
  has a comment; a bare highlight goes straight away.
- On narrow screens a commented highlight shows a 💬 marker; tapping the
  highlight opens the same small popover used for editing, placed right
  under it, showing the comment with Save/Delete. One UI for both sizes.
- No length limit on comments or the note beyond the platform's request
  body cap.

**Deviations from the spec (call them out in the PR):**
- `content_text` has no `\n` at block boundaries. L1a already settled that
  it is exactly the browser's `textContent` of the snapshot (see
  `ContentText` in `text.go`), which lets the client count offsets with a
  DOM `Range` and mirror nothing. L2 keeps that.
- `<mark>` insertion walks the **parsed tree** (the same
  `html.ParseFragment` `ContentText` uses), not the raw token stream, so the
  text the marks index into is by construction the text offsets were
  computed over (parser fix-ups like foster-parenting included).
- The columns are `start_offset`/`end_offset`: `END` is an SQL keyword.
- Highlight edit/delete identify the highlight by a form field
  (`highlight`), not a path segment, because HTMX captures `hx-post` when
  it processes an element; one server-rendered popover serves every
  highlight.

## Global Constraints

- Everything from L1a/L1b's Global Constraints still binds: `later_`
  prefixes, STRICT tables, `… ON DELETE CASCADE`, `db.FormatTime`/`ParseTime`,
  time only via `Store.now()`, no import of another app, owner scoping (404
  for someone else's), no inline `<script>`/`style=` attributes (setting
  `el.style.*` from a static script is fine — Reader does it), CSS in
  app.css's ON Later section with `later-` classes, no new dependencies,
  full check green after every task:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- **Offsets are Unicode code points** into `content_text`, end exclusive.
  Go counts with `[]rune`; JS with `Array.from(string)`. Never UTF-16
  units, never bytes.
- The snapshot (`content_html`, `content_text`) is never changed by L2.
- The highlight unit (`highlight.go`, `highlight_render.go`,
  `static/highlight.js`) must not reference articles, lists, tags, prefs or
  states. Its store methods take a document ID and the document's text;
  **callers must load the article owner-scoped first** and pass `art.ID`
  and `art.ContentText`.
- Comments and the note are stored trimmed, with `\r\n` normalised to
  `\n` (`cleanText`); blank means none. A textarea drops one leading
  newline, so the trimming also keeps round trips exact.
- One highlight colour: background `var(--c-accent-attention-bg)`, text
  colour inherited. Both themes already define that token.
- A `[hidden]` element that also has a class setting `display` needs an
  explicit `[hidden] { display: none; }` rule for that class.
- Highlight rejections are **422 `text/plain`** with a one-line message,
  which the popover shows as text. Missing/foreign article or highlight →
  404, as everywhere else.
- Commits: Conventional Commits, scope `later`.

## File structure (new or substantially changed)

```
internal/apps/later/
  migrations/0004_highlights.sql  later_highlights
  highlight.go                    Highlight, ValidSpan, cleanText, ErrOverlap, store methods
  highlight_render.go             RenderHighlights: <mark> insertion
  highlight_test.go               store + offset tests
  highlight_render_test.go        table-driven <mark> tests
  notes.go                        note handler, highlight handlers, highlight swap
  notes_test.go                   handler tests for this plan
  store.go                        SetNote; ListItem.Highlights
  handlers.go                     articleView gains Highlights, Note, OOB; renderArticle split
  later.go                        routes, embed static/*.js
  templates/article.html          Notes toggle + panel, end note box, popovers, margin, blocks
  templates/index.html            highlight pill; delete wording
  static/highlight.js             selection -> offsets, new/edit popovers
  static/later.js                 confirm via htmx:confirm, margin layout, panel links
internal/ui/static/app.css        ON Later section
docs/user/later.md                "Highlights and notes"
```

---

### Task 1: The highlight store and the article note

**Files:**
- Create: `internal/apps/later/migrations/0004_highlights.sql`, `internal/apps/later/highlight.go`, `internal/apps/later/highlight_test.go`
- Modify: `internal/apps/later/store.go` (`SetNote`)

**Interfaces — Produces:**
```go
type Highlight struct {
	ID, DocID            int64
	Start, End           int // code points into the document text; End exclusive
	Quote, Comment       string
	CreatedAt, UpdatedAt time.Time
}
var ErrOverlap = errors.New("later: highlight overlaps another")
func ValidSpan(text string, start, end int, quote string) bool
func (h Highlight) Matches(text string) bool
func cleanText(s string) string // trim, \r\n -> \n
func (st *Store) AddHighlight(ctx context.Context, docID int64, text string, start, end int, quote, comment string) (Highlight, error)
	// ErrInvalid when !ValidSpan; ErrOverlap when it overlaps a stored one
func (st *Store) Highlights(ctx context.Context, docID int64) ([]Highlight, error) // by Start
func (st *Store) SetHighlightComment(ctx context.Context, docID, id int64, comment string) error // ErrNotFound
func (st *Store) DeleteHighlight(ctx context.Context, docID, id int64) error                     // ErrNotFound
func (st *Store) SetNote(ctx context.Context, userID, id int64, note string) error              // ErrNotFound
```
Overlap rule (half-open ranges): `[s1,e1)` and `[s2,e2)` overlap iff
`s1 < e2 && s2 < e1`. Touching (`e1 == s2`) is fine.

- [ ] **Step 1: Migration** `0004_highlights.sql`:
```sql
-- Highlights index into later_articles.content_text by Unicode code point,
-- end exclusive (spec: "Offsets are code points"). The snapshot never
-- changes, so offsets never go stale; quote is a belt-and-braces check.
CREATE TABLE later_highlights (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    article_id   INTEGER NOT NULL REFERENCES later_articles (id) ON DELETE CASCADE,
    start_offset INTEGER NOT NULL CHECK (start_offset >= 0),
    end_offset   INTEGER NOT NULL CHECK (end_offset > start_offset),
    quote        TEXT    NOT NULL,
    comment      TEXT    NOT NULL DEFAULT '',
    created_at   TEXT    NOT NULL,
    updated_at   TEXT    NOT NULL
) STRICT;

CREATE INDEX later_highlights_article_idx ON later_highlights (article_id, start_offset);
```

- [ ] **Step 2: Failing tests** (`highlight_test.go`, package `later_test`, reusing `newFixture` and `countRows`):
```go
// saveDoc saves a readable article for userID; its ContentText is the
// paragraph's text.
func (f *fixture) saveDoc(t *testing.T, userID int64, pageURL, html string) later.Article {
	t.Helper()
	a, created, err := f.store.Save(context.Background(), userID, later.NewArticle{URL: pageURL, Title: "T", ContentHTML: html})
	if err != nil || !created {
		t.Fatalf("Save(%s) = %v, created %v", pageURL, err, created)
	}
	return a
}

func TestValidSpan(t *testing.T) {
	for _, c := range []struct {
		name       string
		text       string
		start, end int
		quote      string
		want       bool
	}{
		{"ascii", "Hello brave world", 6, 11, "brave", true},
		{"cyrillic", "Привет мир", 7, 10, "мир", true},
		{"hebrew", "שלום עולם", 5, 9, "עולם", true},
		{"emoji is one code point", "a👋b", 1, 2, "👋", true},
		{"utf-16 offsets are wrong", "👋👋x", 4, 5, "x", false},
		{"after two emoji", "👋👋x", 2, 3, "x", true},
		{"combining mark is its own code point", "éx", 0, 2, "é", true},
		{"half a combining sequence", "éx", 0, 1, "e", true},
		{"whole text", "abc", 0, 3, "abc", true},
		{"end past the text", "abc", 1, 4, "bc", false},
		{"negative start", "abc", -1, 2, "ab", false},
		{"empty range", "abc", 1, 1, "", false},
		{"reversed", "abc", 2, 1, "b", false},
		{"quote mismatch", "abc", 0, 2, "ax", false},
		{"blank quote", "a  b", 1, 3, "  ", false},
	} {
		if got := later.ValidSpan(c.text, c.start, c.end, c.quote); got != c.want {
			t.Errorf("%s: ValidSpan(%q, %d, %d, %q) = %v, want %v", c.name, c.text, c.start, c.end, c.quote, got, c.want)
		}
	}
}

func TestAddHighlightStoresAndListsInTextOrder(t *testing.T)
	// doc "<p>Hello brave new world</p>": add "world" [16,21) with comment " Nice \r\nidea ",
	// then "brave" [6,11) -> Highlights returns brave, world; world.Comment == "Nice \nidea";
	// fields Start/End/Quote/DocID right; CreatedAt == UpdatedAt == f.now
func TestAddHighlightRefusesOverlaps(t *testing.T)
	// stored "brave new" [6,15); [12,21) "new world" -> ErrOverlap; [8,10) inside -> ErrOverlap;
	// [0,21) around -> ErrOverlap; [0,6) "Hello " ok (touching at 6 is fine); [16,21) "world" ok
func TestAddHighlightRefusesBadSpans(t *testing.T)
	// [6,11) quote "brane" -> ErrInvalid; [16,99) -> ErrInvalid; nothing stored (countRows == 0)
func TestHighlightsBelongToTheirDocument(t *testing.T)
	// two docs of alice; highlight on A; Highlights(B) empty;
	// SetHighlightComment(B.ID, hA) and DeleteHighlight(B.ID, hA) -> ErrNotFound, hA untouched
func TestSetHighlightComment(t *testing.T)
	// advance f.now 1h; " Second\r\nthought " -> Comment "Second\nthought", UpdatedAt == new now;
	// "   " -> Comment ""
func TestDeleteHighlight(t *testing.T)
	// delete -> Highlights empty; deleting again -> ErrNotFound
func TestDeletingAnArticleRemovesItsHighlights(t *testing.T)
	// Store.Delete -> countRows(`SELECT count(*) FROM later_highlights`) == 0
func TestSetNote(t *testing.T)
	// "  First line\r\nsecond  " -> Article().Note == "First line\nsecond"; " \n " -> "";
	// bob on alice's article -> ErrNotFound, note unchanged
```

- [ ] **Step 3: Run → fail (compile).** `go test ./internal/apps/later/ -run 'Highlight|ValidSpan|SetNote' -v`

- [ ] **Step 4: Implement** `highlight.go`:
```go
package later

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// This file, highlight_render.go and static/highlight.js are the highlight
// unit: highlights on a document's plain text, knowing nothing about
// articles beyond "a document ID and its text", so the unit can move to the
// platform when ON Books needs it (spec: "The highlight unit"). Nothing here
// checks ownership: callers load the document owner-scoped first.

// Highlight is a span of a document's text, with an optional comment.
type Highlight struct {
	ID, DocID int64
	// Start and End are code-point offsets into the document text; End is
	// exclusive (spec: "Offsets are code points").
	Start, End           int
	Quote, Comment       string
	CreatedAt, UpdatedAt time.Time
}

// ErrOverlap is a new highlight that shares text with a stored one.
var ErrOverlap = errors.New("later: highlight overlaps another")

// ValidSpan reports whether start..end is a non-blank range of text whose
// code points are exactly quote.
func ValidSpan(text string, start, end int, quote string) bool {
	return validSpan([]rune(text), start, end, quote)
}

func validSpan(text []rune, start, end int, quote string) bool {
	if start < 0 || end <= start || end > len(text) || strings.TrimSpace(quote) == "" {
		return false
	}
	return string(text[start:end]) == quote
}

// Matches reports whether h still points at its quote in text. One that
// doesn't is listed but not drawn (spec: the stored quote is a
// belt-and-braces check).
func (h Highlight) Matches(text string) bool { return ValidSpan(text, h.Start, h.End, h.Quote) }

// cleanText is how comments (and Later's article note) are stored: trimmed,
// with browser CRLFs as LFs. A textarea drops one leading newline, so
// trimming also keeps an edit round trip exact.
func cleanText(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}

const highlightColumns = `id, article_id, start_offset, end_offset, quote, comment, created_at, updated_at`

func scanHighlight(row rowScanner) (Highlight, error) {
	var h Highlight
	var created, updated string
	if err := row.Scan(&h.ID, &h.DocID, &h.Start, &h.End, &h.Quote, &h.Comment, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Highlight{}, ErrNotFound
		}
		return Highlight{}, fmt.Errorf("later: load highlight: %w", err)
	}
	var err error
	if h.CreatedAt, err = db.ParseTime(created); err != nil {
		return Highlight{}, err
	}
	if h.UpdatedAt, err = db.ParseTime(updated); err != nil {
		return Highlight{}, err
	}
	return h, nil
}

// AddHighlight stores a highlight on document docID, whose text is text.
func (st *Store) AddHighlight(ctx context.Context, docID int64, text string, start, end int, quote, comment string) (Highlight, error) {
	if !ValidSpan(text, start, end, quote) {
		return Highlight{}, ErrInvalid
	}
	now := st.now()
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return Highlight{}, fmt.Errorf("later: begin highlight: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var clash bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM later_highlights
		                WHERE article_id = ? AND start_offset < ? AND ? < end_offset)`,
		docID, end, start).Scan(&clash); err != nil {
		return Highlight{}, fmt.Errorf("later: check overlap: %w", err)
	}
	if clash {
		return Highlight{}, ErrOverlap
	}
	h := Highlight{DocID: docID, Start: start, End: end, Quote: quote, Comment: cleanText(comment), CreatedAt: now, UpdatedAt: now}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO later_highlights (article_id, start_offset, end_offset, quote, comment, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		docID, start, end, quote, h.Comment, db.FormatTime(now), db.FormatTime(now))
	if err != nil {
		return Highlight{}, fmt.Errorf("later: save highlight: %w", err)
	}
	if h.ID, err = res.LastInsertId(); err != nil {
		return Highlight{}, fmt.Errorf("later: save highlight id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Highlight{}, fmt.Errorf("later: commit highlight: %w", err)
	}
	return h, nil
}

// Highlights lists document docID's highlights in text order.
func (st *Store) Highlights(ctx context.Context, docID int64) ([]Highlight, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+highlightColumns+` FROM later_highlights WHERE article_id = ? ORDER BY start_offset`, docID)
	if err != nil {
		return nil, fmt.Errorf("later: list highlights: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Highlight
	for rows.Next() {
		h, err := scanHighlight(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("later: list highlights: %w", err)
	}
	return out, nil
}

// SetHighlightComment replaces a highlight's comment; blank removes it.
func (st *Store) SetHighlightComment(ctx context.Context, docID, id int64, comment string) error {
	return st.exec(ctx, "highlight comment", `
		UPDATE later_highlights SET comment = ?, updated_at = ? WHERE id = ? AND article_id = ?`,
		cleanText(comment), db.FormatTime(st.now()), id, docID)
}

// DeleteHighlight removes one highlight of document docID.
func (st *Store) DeleteHighlight(ctx context.Context, docID, id int64) error {
	return st.exec(ctx, "delete highlight",
		`DELETE FROM later_highlights WHERE id = ? AND article_id = ?`, id, docID)
}
```
`store.go`, next to `SetProgress`:
```go
// SetNote replaces the article's note (spec: one plain-text note per
// article); blank removes it.
func (st *Store) SetNote(ctx context.Context, userID, id int64, note string) error {
	return st.exec(ctx, "note", `
		UPDATE later_articles SET note = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		cleanText(note), db.FormatTime(st.now()), id, userID)
}
```
`Store.Delete` needs no change: `ON DELETE CASCADE` removes highlights
(the test proves it). Update its doc comment to say highlights go too.

- [ ] **Step 5: Run → pass; full check.**
- [ ] **Step 6: Commit** — `feat(later): store highlights and the article note`

---

### Task 2: Drawing highlights in the snapshot

**Files:**
- Create: `internal/apps/later/highlight_render.go`, `internal/apps/later/highlight_render_test.go`
- Modify: `internal/apps/later/handlers.go` (`articleView`, `renderArticle`), `internal/apps/later/templates/article.html` (`later-body` block), `internal/apps/later/handlers_test.go`, `internal/apps/later/export_test.go` (test hook), `internal/ui/static/app.css`

**Interfaces:**
- Consumes: `Highlight`, `validSpan`, `Store.Highlights` (Task 1).
- Produces:
```go
// RenderHighlights returns fragment with every highlight that still matches
// text wrapped in <mark> elements; fragment itself when none do.
func RenderHighlights(fragment, text string, hs []Highlight) string

// articleView gains:
Highlights []highlightView // text order
Note       string
OOB        bool // set on HTMX fragment responses (Tasks 3-5)

type highlightView struct {
	ID      int64
	Quote   string
	Comment string
	Drawn   bool // still matches the text, so it has a <mark id="later-h-{ID}">
}

func (a *App) buildArticleView(r *http.Request, userID int64, art Article, textError string) (articleView, error)
```
Mark markup, per piece of a highlight (a highlight crossing element
boundaries becomes several pieces):
```html
<mark class="later-hl[ later-hl-commented][ later-hl-end]" data-highlight-id="{ID}"[ id="later-h-{ID}"]>text</mark>
```
`id` only on the first piece; `later-hl-end` only on the last;
`later-hl-commented` on every piece when the comment isn't blank.
Attribute order is exactly `class`, `data-highlight-id`, `id`.

- [ ] **Step 1: Failing tests** (`highlight_render_test.go`):
```go
// hl builds a highlight over text[start:end] of fragment's ContentText.
func hl(fragment string, id int64, start, end int, comment string) later.Highlight {
	r := []rune(later.ContentText(fragment))
	return later.Highlight{ID: id, Start: start, End: end, Quote: string(r[start:end]), Comment: comment}
}

func TestRenderHighlights(t *testing.T) {
	const one = `class="later-hl later-hl-end" data-highlight-id="1" id="later-h-1"`
	for _, c := range []struct {
		name, in string
		hs       func(in string) []later.Highlight
		want     string
	}{
		{"none", `<p>Hello <em>brave</em> world</p>`,
			func(string) []later.Highlight { return nil },
			`<p>Hello <em>brave</em> world</p>`},
		{"inside one text node", `<p>Hello brave world</p>`,
			func(in string) []later.Highlight { return []later.Highlight{hl(in, 1, 6, 11, "")} },
			`<p>Hello <mark ` + one + `>brave</mark> world</p>`},
		{"across an inline element", `<p>one <em>two</em> three</p>`,
			func(in string) []later.Highlight { return []later.Highlight{hl(in, 1, 2, 10, "")} },
			`<p>on<mark class="later-hl" data-highlight-id="1" id="later-h-1">e </mark>` +
				`<em><mark class="later-hl" data-highlight-id="1">two</mark></em>` +
				`<mark class="later-hl later-hl-end" data-highlight-id="1"> th</mark>ree</p>`},
		{"across paragraphs", `<p>ab</p><p>cd</p>`,
			func(in string) []later.Highlight { return []later.Highlight{hl(in, 1, 1, 3, "")} },
			`<p>a<mark class="later-hl" data-highlight-id="1" id="later-h-1">b</mark></p>` +
				`<p><mark class="later-hl later-hl-end" data-highlight-id="1">c</mark>d</p>`},
		{"multi-byte text", `<p>Привет 👋 שלום</p>`,
			func(in string) []later.Highlight { return []later.Highlight{hl(in, 1, 7, 8, "")} },
			`<p>Привет <mark ` + one + `>👋</mark> שלום</p>`},
		{"entities count as one character", `<p>a &amp; b</p>`,
			func(in string) []later.Highlight { return []later.Highlight{hl(in, 1, 2, 3, "")} },
			`<p>a <mark ` + one + `>&amp;</mark> b</p>`},
		{"commented", `<p>Hello brave world</p>`,
			func(in string) []later.Highlight { return []later.Highlight{hl(in, 1, 6, 11, "why")} },
			`<p>Hello <mark class="later-hl later-hl-commented later-hl-end" data-highlight-id="1" id="later-h-1">brave</mark> world</p>`},
		{"two in one node, given out of order", `<p>Hello brave world</p>`,
			func(in string) []later.Highlight { return []later.Highlight{hl(in, 2, 12, 17, ""), hl(in, 1, 0, 5, "")} },
			`<p><mark ` + one + `>Hello</mark> brave <mark class="later-hl later-hl-end" data-highlight-id="2" id="later-h-2">world</mark></p>`},
		{"stale quote is not drawn", `<p>Hello brave world</p>`,
			func(string) []later.Highlight { return []later.Highlight{{ID: 1, Start: 6, End: 11, Quote: "brane"}} },
			`<p>Hello brave world</p>`},
		{"overlapping stored highlights: the later one is skipped", `<p>Hello brave world</p>`,
			func(in string) []later.Highlight { return []later.Highlight{hl(in, 1, 0, 11, ""), hl(in, 2, 6, 17, "")} },
			`<p><mark ` + one + `>Hello brave</mark> world</p>`},
	} {
		t.Run(c.name, func(t *testing.T) {
			text := later.ContentText(c.in)
			got := later.RenderHighlights(c.in, text, c.hs(c.in))
			if got != c.want {
				t.Errorf("RenderHighlights =\n%s\nwant\n%s", got, c.want)
			}
			if later.ContentText(got) != text {
				t.Errorf("drawing changed the text: %q, want %q", later.ContentText(got), text)
			}
		})
	}
}

func TestRenderHighlightsNeverPutsAMarkInATableSection(t *testing.T) {
	// The newlines are text children of <table>/<tbody>; a <mark> there would
	// be foster-parented out of the table by the browser, moving the text and
	// breaking every offset after it.
	in := "<table>\n<tr><td>a</td></tr>\n<tr><td>b</td></tr></table>"
	text := later.ContentText(in) // "\na\nb"
	got := later.RenderHighlights(in, text, []later.Highlight{hl(in, 1, 1, 4, "")})
	if n := strings.Count(got, "<mark"); n != 2 {
		t.Errorf("got %d marks, want 2 (one per cell): %s", n, got)
	}
	if later.ContentText(got) != text {
		t.Errorf("drawing changed the text: %q", later.ContentText(got))
	}
}
```
Handler test (`handlers_test.go`):
```go
func TestArticleDrawsItsHighlights(t *testing.T)
	// seedReadable ("<p>Hello reader</p>"); AddHighlight(a.ID, a.ContentText, 6, 12, "reader", "")
	// GET -> div#later-body.later-article-body; mark#later-h-{hid}[data-highlight-id={hid}] text "reader"
func TestArticleWithAStaleHighlightStillRenders(t *testing.T)
	// InsertHighlightForTest(a.ID, 6, 12, "nope") -> page 200, no mark, body text "Hello reader"
```
The store refuses stale rows, so tests make one through a hook in
`export_test.go` (package `later`, compiled only into tests):
```go
// InsertHighlightForTest writes a highlight row without validation, to
// stand in for one whose quote no longer matches.
func (st *Store) InsertHighlightForTest(ctx context.Context, docID int64, start, end int, quote string) (int64, error) {
	now := db.FormatTime(st.now())
	res, err := st.db.ExecContext(ctx, `
		INSERT INTO later_highlights (article_id, start_offset, end_offset, quote, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, docID, start, end, quote, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
```

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement** `highlight_render.go`:
```go
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
```
(`xhtml.Render` writes void elements as `<br/>`, `<img …/>`; browsers parse
those identically, and no test case above contains one. If staticcheck
objects to the `for` with a manual `next`, keep the shape — the node is
replaced during the loop.)

`handlers.go`: split the view building out of `renderArticle` into
`buildArticleView(r, userID, art, textError)` (prefs, options, sizes, and
now highlights and note), so Tasks 3–5's fragment responses reuse it.
In it:
```go
	hs, err := a.store.Highlights(r.Context(), art.ID)
	if err != nil {
		return articleView{}, err
	}
	runes := []rune(art.ContentText)
	for _, h := range hs {
		view.Highlights = append(view.Highlights, highlightView{
			ID: h.ID, Quote: h.Quote, Comment: h.Comment,
			Drawn: validSpan(runes, h.Start, h.End, h.Quote),
		})
	}
	view.Note = art.Note
	view.Body = template.HTML(RenderHighlights(art.ContentHTML, art.ContentText, hs))
```
(`drawable` may skip an overlapping row that `validSpan` accepts; that row
would then show as drawn but have no mark. The store never writes one, so
accept it — don't add machinery for it.)
Update the `Body` comment: it is `ContentHTML` (sanitised or pasted, as
before) or `RenderHighlights` over it, which re-serialises that parsed
tree and adds only `<mark>` elements whose attributes this package builds
from integers and fixed class names.

`article.html`: replace the body line with `{{template "later-body" $d}}`
and add the block:
```html
{{define "later-body"}}<div class="later-article-body" id="later-body">{{.Body}}</div>{{end}}
```
**No whitespace between the `div` tags and `{{.Body}}`** — `textContent`
of `#later-body` must equal `content_text`, and the client counts offsets
over it (Task 4).

CSS:
```css
.later-hl { background: var(--c-accent-attention-bg); color: inherit; border-radius: 2px; scroll-margin-top: 5rem; }
```

- [ ] **Step 4: Run → pass; full check; browser check:** add a highlight
with a store call (or SQL) to a real saved article, open it — the passage
is tinted in light and dark; `document.getElementById("later-body").textContent`
equals the stored `content_text` (compare in the console against a SQL
read).
- [ ] **Step 5: Commit** — `feat(later): draw highlights in the article`

---

### Task 3: The Notes panel and the article note

**Files:**
- Create: `internal/apps/later/notes.go`, `internal/apps/later/notes_test.go`
- Modify: `later.go` (route), `templates/article.html`, `internal/ui/static/app.css`, `static/later.js`, `docs/user/later.md`

**Interfaces:**
- Consumes: `Store.SetNote`, `articleView.Highlights/Note/OOB`, `buildArticleView` (Tasks 1–2).
- Produces:
  - `POST /later/a/{id}/note` (fields `note`, `copy` = `panel`|`end`)
    - HTMX → 200, fragment `later-note-saved`: the *other* copy's
      `textarea#later-note-{other}` with `hx-swap-oob="true"`, and
      `span#later-note-status-{copy}` "Saved" with `hx-swap-oob="true"`.
      It never re-sends the copy that posted (PATTERNS: don't swap out the
      input that sent the request).
    - plain form → 303 `/later/a/{id}`.
    - someone else's / missing → 404.
  - Template blocks used again by Tasks 4–6 (each honours `.OOB` by adding
    `hx-swap-oob="true"`): `later-notes-count` (`span#later-notes-count`,
    the number of highlights), `later-notes-list` (`ol#later-notes-list`).
  - Markup hooks: `input#later-notes-open.later-notes-open` (checkbox),
    `label.later-notes-toggle[for=later-notes-open]`,
    `aside#later-notes.later-notes`,
    `li.later-notes-item[data-highlight-id]` holding
    `.later-notes-quote` (an `a[href=#later-h-{id}]` when drawn, else a
    `span` plus `.later-notes-stale` "Not found in the text") and, when
    commented, `p.later-notes-comment`.

- [ ] **Step 1: Failing tests** (`notes_test.go`):
```go
func TestArticleHasTheNotesPanel(t *testing.T)
	// input#later-notes-open[type=checkbox]; label.later-notes-toggle[for=later-notes-open] text "Notes · 0";
	// aside#later-notes with form[action=/later/a/{id}/note] holding textarea#later-note-panel[name=note]
	// and input[name=copy][value=panel]; li.later-notes-empty
func TestArticleHasTheEndOfArticleNoteBox(t *testing.T)
	// .later-note-end form[action=/later/a/{id}/note] with textarea#later-note-end, input[name=copy][value=end];
	// both forms carry hx-post=/later/a/{id}/note, hx-trigger="change, submit", hx-swap="none"
func TestNoteIsShownInBothCopies(t *testing.T)
	// SetNote "Remember this" -> both textareas contain it
func TestNotesPanelListsHighlightsInTextOrder(t *testing.T)
	// doc "<p>Hello brave new world</p>"; highlights "world" (comment "Big") then "brave" ->
	// count "2"; items in order brave, world; brave's quote is a[href=#later-h-{id}]; world has p.later-notes-comment "Big"
func TestNotesPanelMarksAStaleHighlight(t *testing.T)
	// InsertHighlightForTest with quote "nope" -> span.later-notes-quote + .later-notes-stale, no a[href=#later-h-…]
func TestSavingTheNoteOverHTMXRefreshesTheOtherCopy(t *testing.T)
	// PostHX note=" Hi\r\nthere " copy=panel -> 200; Article().Note == "Hi\nthere";
	// body has textarea#later-note-end[hx-swap-oob=true] containing "Hi\nthere",
	// span#later-note-status-panel[hx-swap-oob=true] "Saved"; no #later-note-panel in the body
func TestSavingTheNoteWithoutJSRedirects(t *testing.T)
	// Post note=x copy=end -> 303 /later/a/{id}
func TestNoteOfAnotherUserIs404(t *testing.T)
func TestLinkOnlyArticleCanHaveANote(t *testing.T)
	// seedLinkOnly -> end box present; save works
```

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement**

`notes.go`:
```go
package later

import (
	"fmt"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// noteSavedView answers a note save: the copy that didn't post, refreshed,
// and a "Saved" next to the one that did.
type noteSavedView struct {
	D     articleView
	Copy  string // "panel" | "end": the copy that posted
	Other string
}

// setNote saves the article note from either of its two copies (the Notes
// panel and the box after the article; spec: "Notes").
func (a *App) setNote(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.SetNote(r.Context(), userID, id, r.PostFormValue("note")); err != nil {
		a.fail(w, r, err)
		return
	}
	if !web.IsHTMX(r) {
		http.Redirect(w, r, fmt.Sprintf("/later/a/%d", id), http.StatusSeeOther)
		return
	}
	art, err := a.store.Article(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	v := noteSavedView{D: articleView{ID: art.ID, Note: art.Note}, Copy: "end", Other: "panel"}
	if r.PostFormValue("copy") == "panel" {
		v.Copy, v.Other = "panel", "end"
	}
	if err := a.deps.Render.Fragment(w, http.StatusOK, "later/article", "later-note-saved", v); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
```
Route: `r.HandleFunc("POST /a/{id}/note", a.setNote)`.

`article.html` — the checkbox is the reader root's **first child**, so
`~` reaches both the sticky bar and the panel:
```html
	<input type="checkbox" id="later-notes-open" class="visually-hidden later-notes-open" aria-controls="later-notes">
	<div class="later-sticky">
	<div class="later-topbar">
		… ← Later, title, min left …
		<label class="later-notes-toggle" for="later-notes-open">Notes · {{template "later-notes-count" $d}}</label>
		… Aa, Archive, ⋯ as today …
```
After `</article>`, still inside `.later-reader`:
```html
	<aside class="later-notes stack" id="later-notes" aria-label="Notes">
		<div class="later-notes-head">
			<h2>Notes</h2>
			<label class="later-notes-close" for="later-notes-open" aria-label="Close notes">✕</label>
		</div>
		{{template "later-note-form" (dict "D" $d "Copy" "panel" "CSRF" $csrf)}}
		<h3>Highlights</h3>
		{{template "later-notes-list" $d}}
	</aside>
```
Inside `<article>`, after the body / link-only section:
```html
		<section class="later-note-end" aria-label="Your note">
			{{template "later-note-form" (dict "D" $d "Copy" "end" "CSRF" $csrf)}}
		</section>
```
Blocks:
```html
{{define "later-note-form"}}
<form class="later-note-form stack" method="post" action="/later/a/{{.D.ID}}/note"
      hx-post="/later/a/{{.D.ID}}/note" hx-trigger="change, submit" hx-swap="none">
	<input type="hidden" name="{{csrfField}}" value="{{.CSRF}}">
	<input type="hidden" name="copy" value="{{.Copy}}">
	<label for="later-note-{{.Copy}}">Your note on this article</label>
	{{template "later-note-text" (dict "D" .D "Copy" .Copy "OOB" false)}}
	<div class="later-note-actions">
		<button type="submit">Save note</button>
		<span class="later-note-status" id="later-note-status-{{.Copy}}" role="status"></span>
	</div>
</form>
{{end}}

{{define "later-note-text"}}<textarea id="later-note-{{.Copy}}" name="note" rows="{{if eq .Copy "panel"}}8{{else}}5{{end}}"{{if .OOB}} hx-swap-oob="true"{{end}}>{{.D.Note}}</textarea>{{end}}

{{define "later-note-saved"}}{{template "later-note-text" (dict "D" .D "Copy" .Other "OOB" true)}}<span class="later-note-status" id="later-note-status-{{.Copy}}" role="status" hx-swap-oob="true">Saved</span>{{end}}

{{define "later-notes-count"}}<span id="later-notes-count"{{if .OOB}} hx-swap-oob="true"{{end}}>{{len .Highlights}}</span>{{end}}

{{define "later-notes-list"}}
<ol class="later-notes-list" id="later-notes-list"{{if .OOB}} hx-swap-oob="true"{{end}}>
	{{range .Highlights}}
	<li class="later-notes-item" data-highlight-id="{{.ID}}">
		{{if .Drawn}}<a class="later-notes-quote" href="#later-h-{{.ID}}">{{.Quote}}</a>
		{{else}}<span class="later-notes-quote">{{.Quote}}</span> <span class="later-notes-stale">Not found in the text</span>{{end}}
		{{with .Comment}}<p class="later-notes-comment">{{.}}</p>{{end}}
	</li>
	{{else}}
	<li class="later-notes-empty">Select text in the article to highlight it.</li>
	{{end}}
</ol>
{{end}}
```
(`hx-trigger="change"` on the form catches the textarea's `change`, which
fires on blur when the text changed — the spec's "saves on blur". The
"Save note" button is the no-JS path and works with JS too.)

CSS:
```css
.later-notes-toggle { cursor: pointer; padding: .2rem .5rem; border: var(--border); border-radius: var(--radius); white-space: nowrap; }
.later-notes-open:focus-visible ~ .later-sticky .later-notes-toggle { outline: 2px solid var(--c-accent); outline-offset: 2px; }
.later-notes { position: fixed; inset: 0 0 0 auto; z-index: 3; width: min(24rem, 100vw); overflow-y: auto; padding: 1rem;
  background: var(--c-bg); border-left: var(--border); box-shadow: -6px 0 20px rgba(0,0,0,.12);
  transform: translateX(100%); visibility: hidden; transition: transform .2s ease, visibility .2s; }
.later-notes-open:checked ~ .later-notes { transform: none; visibility: visible; }
.later-notes-head { display: flex; align-items: center; justify-content: space-between; }
.later-notes-head h2 { margin: 0; font-size: var(--fs-lg); }
.later-notes h3 { margin: 0; font-size: var(--fs-base); }
.later-notes-close { cursor: pointer; padding: .2rem .5rem; }
.later-notes-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: .75rem; }
.later-notes-quote { display: block; padding-left: .5rem; border-left: 3px solid var(--c-accent-attention); color: inherit; text-decoration: none; }
.later-notes-comment { margin: .25rem 0 0 .75rem; font-size: var(--fs-sm); color: var(--c-text-dim); white-space: pre-wrap; }
.later-notes-stale, .later-notes-empty { font-size: var(--fs-sm); color: var(--c-text-faint); }
.later-note-form textarea { width: 100%; }
.later-note-actions { display: flex; gap: .5rem; align-items: center; }
.later-note-status { font-size: var(--fs-sm); color: var(--c-text-faint); }
.later-note-end { margin-top: 2.5rem; padding-top: 1.5rem; border-top: var(--border); }
```
and in the existing `prefers-reduced-motion` block if there is one (else
a new one next to these rules): `.later-notes { transition: none; }`.

`later.js` — on narrow screens a highlight link in the panel would leave
the panel covering the passage, so close it first:
```js
	// A highlight link in the Notes panel scrolls to the passage; on a
	// narrow screen the panel would cover it, so close the panel too.
	document.addEventListener("click", function (e) {
		var link = e.target instanceof Element && e.target.closest(".later-notes-quote[href]");
		if (!link || !window.matchMedia("(max-width: 640px)").matches) return;
		var open = document.getElementById("later-notes-open");
		if (open) open.checked = false;
	});
```
Also clear `#later-note-status-*` back to "" when its textarea gets
`input` again (so "Saved" doesn't linger over unsaved text).

Docs (`docs/user/later.md`), new section "Highlights and notes" (Task 4
and 5 extend it):
> **Notes** in the top bar opens the Notes panel. Write your thoughts on
> the whole article in the note at the top; it saves when you click
> elsewhere. The same note is at the end of the article, so you can write
> it as soon as you finish reading.

- [ ] **Step 4: Run → pass; full check; browser check:** the panel opens
and closes without JS (disable JS once) and with it; typing in one copy and
clicking away updates the other; reload keeps the note; 375px (the panel
fills the width); dark mode; keyboard: Tab reaches the toggle with a
visible focus ring and Space opens the panel.
- [ ] **Step 5: Commit** — `feat(later): article note and the Notes panel`

---

### Task 4: Creating highlights

**Files:**
- Create: `internal/apps/later/static/highlight.js`
- Modify: `notes.go` (handler + swap), `later.go` (route, embed, script route), `templates/article.html` (popover, swap block, script tag), `notes_test.go`, `internal/ui/static/app.css`, `docs/user/later.md`

**Interfaces:**
- Consumes: `Store.AddHighlight`, `ErrOverlap`, `ErrInvalid`, `buildArticleView`, the `later-body`/`later-notes-count`/`later-notes-list` blocks.
- Produces:
  - `POST /later/a/{id}/highlights` (fields `start`, `end`, `quote`, `comment`)
    - HTMX success → 200, fragment `later-highlight-swap` = `later-body`
      (the swap target) + `later-notes-count` and `later-notes-list` with
      `hx-swap-oob="true"` (Task 6 adds `later-margin`).
    - overlap → 422 `text/plain` `overlapMessage`; bad span, non-integer
      offsets or a link-only article → 422 `text/plain` `badSpanMessage`.
    - plain form success → 303 `/later/a/{id}`.
  - `func (a *App) renderHighlightSwap(w http.ResponseWriter, r *http.Request, userID int64, art Article)` — reused by Task 5.
  - `func (a *App) highlightRejected(w http.ResponseWriter, err error)` — 422 text/plain.
  - `GET /later/highlight.js`.
  - Popover markup `form#later-hl-new.later-popover` (below).

```go
const (
	overlapMessage = "That overlaps one of your highlights. Select a different passage."
	badSpanMessage = "Couldn't highlight that selection. Try selecting it again."
)
```

- [ ] **Step 1: Failing tests** (`notes_test.go`):
```go
// postHighlight posts a highlight over the given code points of a's text.
func postHighlight(t *testing.T, s *server, a later.Article, start, end int, comment string) *httptest.ResponseRecorder {
	t.Helper()
	r := []rune(a.ContentText)
	return s.PostHX(t, s.Alice, articlePath(a, "/highlights"), url.Values{
		"start": {strconv.Itoa(start)}, "end": {strconv.Itoa(end)},
		"quote": {string(r[start:end])}, "comment": {comment},
	})
}

func TestAddHighlightSwapsTheBodyAndRefreshesThePanel(t *testing.T)
	// doc "<p>Hello brave new world</p>", postHighlight 6..11 -> 200;
	// parsed body: div#later-body with mark[data-highlight-id] "brave";
	// span#later-notes-count[hx-swap-oob=true] "1"; ol#later-notes-list[hx-swap-oob=true] with the quote; no <html
func TestAddHighlightWithAComment(t *testing.T)
	// comment "Why?" -> stored; mark.later-hl-commented; p.later-notes-comment "Why?"
func TestAddHighlightRefusesAnOverlap(t *testing.T)
	// 6..15 then 12..21 -> 422, Content-Type text/plain, body == overlapMessage; still one highlight
func TestAddHighlightRefusesAMismatchedQuote(t *testing.T)
	// quote "brane" for 6..11 -> 422 badSpanMessage
func TestAddHighlightRefusesGarbageOffsets(t *testing.T)
	// start=abc -> 422 badSpanMessage
func TestAddHighlightOnALinkOnlyArticleIs422(t *testing.T)
func TestAddHighlightOnAnotherUsersArticleIs404(t *testing.T)
func TestAddHighlightWithoutHTMXRedirects(t *testing.T)
	// s.Post -> 303 /later/a/{id}
func TestArticleHasTheHighlightPopover(t *testing.T)
	// script[src=/later/highlight.js]; form#later-hl-new[hidden][hx-post=/later/a/{id}/highlights][hx-target=#later-body][hx-swap=outerHTML]
	// with input[name=start], input[name=end], input[name=quote], textarea[name=comment];
	// link-only article -> no form#later-hl-new
func TestHighlightScriptIsServed(t *testing.T)
	// GET /later/highlight.js -> 200, Content-Type text/javascript; charset=utf-8, body contains "later-hl-new"
```

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement**

`notes.go`:
```go
// addHighlight saves a highlight the reader selected (static/highlight.js
// computes the offsets) and answers with the redrawn article.
func (a *App) addHighlight(w http.ResponseWriter, r *http.Request) {
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
	start, err1 := strconv.Atoi(r.PostFormValue("start"))
	end, err2 := strconv.Atoi(r.PostFormValue("end"))
	if err1 != nil || err2 != nil {
		a.highlightRejected(w, ErrInvalid)
		return
	}
	if _, err := a.store.AddHighlight(r.Context(), art.ID, art.ContentText, start, end,
		r.PostFormValue("quote"), r.PostFormValue("comment")); err != nil {
		if errors.Is(err, ErrInvalid) || errors.Is(err, ErrOverlap) {
			a.highlightRejected(w, err)
			return
		}
		a.fail(w, r, err)
		return
	}
	a.renderHighlightSwap(w, r, userID, art)
}

// highlightRejected answers 422 with a line the popover shows as text
// (spec: "Highlight rejections … show a short notice by the popover").
func (a *App) highlightRejected(w http.ResponseWriter, err error) {
	msg := badSpanMessage
	if errors.Is(err, ErrOverlap) {
		msg = overlapMessage
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = io.WriteString(w, msg)
}

// renderHighlightSwap answers a highlight change: the redrawn body, plus the
// Notes panel's list and count out of band. Without HTMX it goes back to the
// article.
func (a *App) renderHighlightSwap(w http.ResponseWriter, r *http.Request, userID int64, art Article) {
	if !web.IsHTMX(r) {
		http.Redirect(w, r, fmt.Sprintf("/later/a/%d", art.ID), http.StatusSeeOther)
		return
	}
	view, err := a.buildArticleView(r, userID, art, "")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	view.OOB = true
	if err := a.deps.Render.Fragment(w, http.StatusOK, "later/article", "later-highlight-swap", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
```
Route: `r.HandleFunc("POST /a/{id}/highlights", a.addHighlight)`.
`later.go`: `//go:embed static/*.js`, and a `GET /highlight.js` route
serving `static/highlight.js` the way `script` serves `later.js` (make
`script` take the file name: `a.script("later.js")` returning a
`http.HandlerFunc`).

`article.html`:
- `head` block: add `<script src="/later/highlight.js" defer></script>`.
- the swap block:
```html
{{define "later-highlight-swap"}}{{template "later-body" .}}{{template "later-notes-count" .}}{{template "later-notes-list" .}}{{end}}
```
- the popover, after `</aside>` (outside `#later-body`, so the swap never
  replaces it), only when readable:
```html
	{{if not $d.LinkOnly}}
	<form id="later-hl-new" class="later-popover stack" hidden method="post" action="/later/a/{{$d.ID}}/highlights"
	      hx-post="/later/a/{{$d.ID}}/highlights" hx-target="#later-body" hx-swap="outerHTML">
		<input type="hidden" name="{{csrfField}}" value="{{$csrf}}">
		<input type="hidden" name="start">
		<input type="hidden" name="end">
		<input type="hidden" name="quote">
		<div class="stack" data-later-hl-comment hidden>
			<label for="later-hl-comment">Comment</label>
			<textarea id="later-hl-comment" name="comment" rows="3"></textarea>
		</div>
		<div class="later-popover-actions">
			<button class="primary" type="submit" data-later-hl-submit>Highlight</button>
			<button type="button" data-later-hl-with-comment>Highlight + comment</button>
		</div>
		<p class="later-hl-error" data-later-hl-error role="alert" hidden></p>
	</form>
	{{end}}
```

`static/highlight.js`:
```js
// ON Later's highlight unit, browser side (with highlight.go and
// highlight_render.go). It turns a selection inside #later-body into
// code-point offsets: a Range from the body's start to the selection
// counts exactly the text nodes ContentText concatenates, and Array.from
// counts code points the way Go's []rune does (spec: "Offsets are code
// points").
"use strict";

(function () {
	var form = document.getElementById("later-hl-new");
	if (!form) return;
	var commentBox = form.querySelector("[data-later-hl-comment]");
	var comment = form.querySelector('textarea[name="comment"]');
	var submit = form.querySelector("[data-later-hl-submit]");
	var withComment = form.querySelector("[data-later-hl-with-comment]");
	var error = form.querySelector("[data-later-hl-error]");
	var pressedAt = 0; // last pointerdown inside the popover
	var timer = null;

	function body() { return document.getElementById("later-body"); }

	function codePoints(root, node, offset) {
		var r = document.createRange();
		r.setStart(root, 0);
		r.setEnd(node, offset);
		return Array.from(r.toString()).length;
	}

	// selection is the current selection as {start, end, quote, rect} when
	// it lies wholly inside the article body, trimmed of white space at
	// either end; otherwise null.
	function selection() {
		var sel = window.getSelection();
		if (!sel || sel.rangeCount === 0 || sel.isCollapsed) return null;
		var range = sel.getRangeAt(0);
		var root = body();
		if (!root || !root.contains(range.startContainer) || !root.contains(range.endContainer)) return null;
		var chars = Array.from(range.toString());
		var lead = 0, trail = 0;
		while (lead < chars.length && /\s/.test(chars[lead])) lead++;
		while (trail < chars.length - lead && /\s/.test(chars[chars.length - 1 - trail])) trail++;
		if (lead + trail >= chars.length) return null;
		var start = codePoints(root, range.startContainer, range.startOffset) + lead;
		return {
			start: start,
			end: start + chars.length - lead - trail,
			quote: chars.slice(lead, chars.length - trail).join(""),
			rect: range.getBoundingClientRect()
		};
	}

	// place puts a popover just under rect (viewport coordinates), kept
	// inside the window.
	function place(el, rect) {
		var width = document.documentElement.clientWidth;
		el.style.top = (rect.bottom + window.scrollY + 8) + "px";
		el.style.left = (window.scrollX + Math.max(8, Math.min(rect.left, width - el.offsetWidth - 8))) + "px";
	}

	function open(s) {
		form.elements.start.value = String(s.start);
		form.elements.end.value = String(s.end);
		form.elements.quote.value = s.quote;
		comment.value = "";
		commentBox.hidden = true;
		withComment.hidden = false;
		submit.textContent = "Highlight";
		error.hidden = true;
		form.hidden = false;
		place(form, s.rect);
	}
	function close() { form.hidden = true; }

	document.addEventListener("selectionchange", function () {
		clearTimeout(timer);
		timer = setTimeout(function () {
			if (form.contains(document.activeElement) || Date.now() - pressedAt < 500) return;
			var s = selection();
			if (s) open(s); else close();
		}, 250);
	});
	form.addEventListener("pointerdown", function () { pressedAt = Date.now(); });
	// Keep the selection while the popover's buttons are pressed.
	form.addEventListener("mousedown", function (e) {
		if (!(e.target instanceof HTMLTextAreaElement)) e.preventDefault();
	});
	withComment.addEventListener("click", function () {
		commentBox.hidden = false;
		withComment.hidden = true;
		submit.textContent = "Save highlight";
		comment.focus();
	});
	document.addEventListener("keydown", function (e) {
		if (e.key === "Escape" && !form.hidden) close();
	});

	// After the post: close on success (the body has been redrawn), else say
	// why next to the buttons. A network failure has no 422 text.
	document.body.addEventListener("htmx:afterRequest", function (e) {
		if (e.detail.elt !== form) return;
		if (e.detail.successful) {
			close();
			var sel = window.getSelection();
			if (sel) sel.removeAllRanges();
			return;
		}
		var xhr = e.detail.xhr;
		error.textContent = (xhr && xhr.status === 422 && xhr.responseText) || "Couldn't save the highlight. Try again.";
		error.hidden = false;
	});
})();
```
(The vendored htmx is 2.0.10 (`internal/ui/static/VENDOR.md`): a 422
isn't swapped by default, which is what this relies on; and
`htmx:afterRequest`'s `detail.successful` is false for 4xx and network
errors.)

CSS:
```css
.later-popover { position: absolute; z-index: 4; width: min(20rem, calc(100vw - 1rem)); padding: .75rem;
  background: var(--c-bg); border: var(--border); border-radius: var(--radius); box-shadow: 0 6px 20px rgba(0,0,0,.15); }
.later-popover[hidden], .later-popover [hidden] { display: none; }
.later-popover textarea { width: 100%; }
.later-popover-actions { display: flex; flex-wrap: wrap; gap: .5rem; }
.later-hl-error { margin: 0; color: var(--c-danger); font-size: var(--fs-sm); }
```

Docs, in "Highlights and notes":
> Select a passage in the article and a small box appears under it.
> Click **Highlight** to mark it, or **Highlight + comment** to add a
> comment too. Highlights can't overlap. Every highlight is listed in the
> Notes panel; click one to jump to it.

- [ ] **Step 4: Run → pass; full check; browser check** on a real saved
article: select inside one paragraph, across a link and across two
paragraphs → each highlight lands exactly on the selected text (compare
the stored quote); text with an emoji or Cyrillic before the selection;
an overlapping selection shows the overlap message; Escape and clicking
elsewhere close the popover; the count and panel list update; touch
selection at 375px (device emulation) still shows the popover.
- [ ] **Step 5: Commit** — `feat(later): highlight a passage, with an optional comment`

---

### Task 5: Editing and deleting highlights

**Files:**
- Modify: `notes.go` (two handlers), `later.go` (routes), `templates/article.html` (edit popover; Edit buttons in `later-notes-list`), `static/highlight.js`, `static/later.js` (confirm via `htmx:confirm`), `notes_test.go`, `docs/user/later.md`

**Interfaces:**
- Consumes: `Store.SetHighlightComment`, `Store.DeleteHighlight`, `renderHighlightSwap`, `.later-notes-item` markup (Task 3).
- Produces:
  - `POST /later/a/{id}/highlights/comment` (fields `highlight`, `comment`) and
    `POST /later/a/{id}/highlights/delete` (field `highlight`) → the same
    answers as Task 4's success (swap or 303). A non-integer `highlight`,
    one on another article, or another user's article → 404.
  - `div#later-hl-edit.later-popover` with `[data-later-hl-quote]`,
    `form[data-later-hl-edit-form]` and `form[data-later-hl-delete-form]`.
  - Any element with `data-later-hl-open="{id}"` (panel Edit buttons; Task
    6's margin notes) or a `mark.later-hl` opens the edit popover for that
    highlight, anchored under the element clicked.

- [ ] **Step 1: Failing tests**
```go
func TestEditingAHighlightsCommentRedraws(t *testing.T)
	// add 6..11 bare; PostHX /highlights/comment highlight={hid} comment="Later thought" -> 200;
	// store comment; mark.later-hl-commented; ol#later-notes-list[hx-swap-oob=true] p.later-notes-comment "Later thought"
func TestClearingACommentRemovesIt(t *testing.T)
	// comment "   " -> stored "", no .later-hl-commented
func TestDeletingAHighlightRedraws(t *testing.T)
	// PostHX /highlights/delete -> no mark, count "0", li.later-notes-empty
func TestHighlightEditsAreScopedToTheArticle(t *testing.T)
	// alice's highlight id posted against alice's *other* article -> 404, untouched
func TestHighlightEditsOfAnotherUserAre404(t *testing.T)
func TestHighlightEditsRejectGarbageIDs(t *testing.T)
	// highlight=abc, highlight=0 -> 404
func TestArticleHasTheEditPopover(t *testing.T)
	// div#later-hl-edit[hidden] with form[hx-post=/later/a/{id}/highlights/comment][hx-target=#later-body]
	// (input[name=highlight], textarea[name=comment]) and form[hx-post=/later/a/{id}/highlights/delete]
	// (input[name=highlight]); each panel item has button[data-later-hl-open={hid}] "Edit"
```

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement**

`notes.go`:
```go
// highlightID parses the "highlight" form field; anything but a positive
// integer is a 404, like a highlight that isn't there.
func (a *App) highlightID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PostFormValue("highlight"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// changeHighlight loads the user's article and the highlight id, runs op on
// them, and answers with the redrawn article.
func (a *App) changeHighlight(op func(r *http.Request, art Article, hid int64) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		hid, ok := a.highlightID(w, r)
		if !ok {
			return
		}
		if err := op(r, art, hid); err != nil {
			a.fail(w, r, err)
			return
		}
		a.renderHighlightSwap(w, r, userID, art)
	}
}
```
Routes:
```go
	r.HandleFunc("POST /a/{id}/highlights/comment", a.changeHighlight(func(r *http.Request, art Article, hid int64) error {
		return a.store.SetHighlightComment(r.Context(), art.ID, hid, r.PostFormValue("comment"))
	}))
	r.HandleFunc("POST /a/{id}/highlights/delete", a.changeHighlight(func(r *http.Request, art Article, hid int64) error {
		return a.store.DeleteHighlight(r.Context(), art.ID, hid)
	}))
```

`article.html` — in `later-notes-list`, inside each item:
```html
		<button type="button" class="later-notes-edit" data-later-hl-open="{{.ID}}">Edit</button>
```
The edit popover, next to `#later-hl-new`:
```html
	{{if not $d.LinkOnly}}
	<div id="later-hl-edit" class="later-popover stack" hidden role="dialog" aria-label="Edit highlight">
		<p class="later-hl-quote" data-later-hl-quote></p>
		<form class="stack" method="post" action="/later/a/{{$d.ID}}/highlights/comment" data-later-hl-edit-form
		      hx-post="/later/a/{{$d.ID}}/highlights/comment" hx-target="#later-body" hx-swap="outerHTML">
			<input type="hidden" name="{{csrfField}}" value="{{$csrf}}">
			<input type="hidden" name="highlight">
			<label for="later-hl-edit-comment">Comment</label>
			<textarea id="later-hl-edit-comment" name="comment" rows="3"></textarea>
			<div class="later-popover-actions"><button class="primary" type="submit">Save</button></div>
		</form>
		<form method="post" action="/later/a/{{$d.ID}}/highlights/delete" data-later-hl-delete-form
		      hx-post="/later/a/{{$d.ID}}/highlights/delete" hx-target="#later-body" hx-swap="outerHTML">
			<input type="hidden" name="{{csrfField}}" value="{{$csrf}}">
			<input type="hidden" name="highlight">
			<button class="danger" type="submit">Delete highlight</button>
		</form>
		<p class="later-hl-error" data-later-hl-error role="alert" hidden></p>
	</div>
	{{end}}
```
(Drop `{{if not $d.LinkOnly}}` duplication by wrapping both popovers in
one `if`.)

`highlight.js` — the edit popover, inside the same IIFE after the
new-highlight code (it reuses `place` and `close`). The quote and comment
come from the Notes panel item, the one place they are rendered as text:
```js
	var edit = document.getElementById("later-hl-edit");
	if (edit) {
		var editForm = edit.querySelector("[data-later-hl-edit-form]");
		var deleteForm = edit.querySelector("[data-later-hl-delete-form]");
		var editError = edit.querySelector("[data-later-hl-error]");

		var openEdit = function (id, anchor) {
			var item = document.querySelector('.later-notes-item[data-highlight-id="' + id + '"]');
			if (!item) return;
			var quote = item.querySelector(".later-notes-quote");
			var note = item.querySelector(".later-notes-comment");
			edit.querySelector("[data-later-hl-quote]").textContent = quote ? quote.textContent : "";
			editForm.elements.highlight.value = id;
			deleteForm.elements.highlight.value = id;
			editForm.elements.comment.value = note ? note.textContent : "";
			// Only a highlight with a comment asks before it goes (Ilia's call).
			if (note) deleteForm.setAttribute("hx-confirm", "Delete this highlight and its comment?");
			else deleteForm.removeAttribute("hx-confirm");
			editError.hidden = true;
			close(); // the new-highlight popover
			edit.hidden = false;
			place(edit, anchor.getBoundingClientRect());
			editForm.elements.comment.focus();
		};

		document.addEventListener("click", function (e) {
			if (!(e.target instanceof Element)) return;
			var opener = e.target.closest("[data-later-hl-open], mark.later-hl");
			if (opener) {
				var sel = window.getSelection();
				if (opener.matches("mark") && sel && !sel.isCollapsed) return; // a drag-select starting in a mark
				openEdit(opener.dataset.laterHlOpen || opener.dataset.highlightId, opener);
				return;
			}
			if (!edit.hidden && !edit.contains(e.target)) edit.hidden = true;
		});
		document.addEventListener("keydown", function (e) {
			if (e.key === "Escape") edit.hidden = true;
		});
		document.body.addEventListener("htmx:afterRequest", function (e) {
			if (e.detail.elt !== editForm && e.detail.elt !== deleteForm) return;
			if (e.detail.successful) { edit.hidden = true; return; }
			editError.textContent = "Couldn't save that. Try again.";
			editError.hidden = false;
		});
	}
```
(htmx reads `hx-confirm` when the request is issued, so setting it per
opening works; verify that against the vendored htmx version.)

`later.js` — route `hx-confirm` through the app's dialog, sharing the code
with the existing `data-later-confirm` forms. Refactor the submit handler's
dialog part into `confirmThen(message, onOK)`, then:
```js
	// hx-confirm questions use the same dialog as data-later-confirm forms
	// (the pattern reader.js uses for its own dialog).
	document.addEventListener("htmx:confirm", function (e) {
		if (!e.detail.question) return;
		var dialog = document.getElementById("later-confirm-dialog");
		if (!dialog || typeof dialog.showModal !== "function") return; // htmx falls back to window.confirm
		e.preventDefault();
		confirmThen(e.detail.question, function () { e.detail.issueRequest(true); });
	});
```

CSS:
```css
.later-hl { cursor: pointer; }
.later-hl-quote { margin: 0; padding-left: .5rem; border-left: 3px solid var(--c-accent-attention); font-size: var(--fs-sm);
  color: var(--c-text-dim); display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; }
.later-notes-item { position: relative; }
.later-notes-edit { margin-top: .25rem; font-size: var(--fs-sm); }
```

Docs, in "Highlights and notes":
> Click a highlight (or **Edit** next to it in the Notes panel) to change
> its comment or delete it. Deleting a highlight that has a comment asks
> first.

- [ ] **Step 4: Run → pass; full check; browser check:** click a highlight
→ popover under it with its comment; save a new comment; clear it; delete
a bare highlight (no question) and a commented one (dialog; Cancel keeps
it); Edit from the panel; a drag-select that starts inside a highlight
doesn't open the edit popover; 375px; dark mode.
- [ ] **Step 5: Commit** — `feat(later): edit and delete highlights`

---

### Task 6: Margin comments, the 💬 marker and list counts

**Files:**
- Modify: `templates/article.html` (`later-margin` block, `later-highlight-swap`), `store.go` (`ListItem.Highlights`, `List`), `handlers.go` (`rowView.Highlights`), `templates/index.html`, `static/later.js`, `internal/ui/static/app.css`, `handlers_test.go`, `notes_test.go`, `store_test.go`, `docs/user/later.md`

**Interfaces:**
- Consumes: `highlightView.Drawn/Comment`, `#later-h-{id}`, `data-later-hl-open` (Task 5).
- Produces:
  - `div#later-margin.later-margin[aria-hidden=true]` inside
    `article.later-article`, one `p.later-margin-note[data-later-hl-open={id}]`
    per drawn, commented highlight, in text order; part of
    `later-highlight-swap` with `hx-swap-oob="true"`.
  - `.later-has-margin` on `#later-reader`, set by `later.js` when the
    window has room beside the column.
  - `ListItem.Highlights int`; `rowView.Highlights int`; row pill
    `span.later-pill.later-pill-hl` "1 highlight" / "N highlights".
  - Delete confirmations (article ⋯ and row ⋯) read
    `Delete permanently? Highlights and notes go too.` (spec: "The list").

- [ ] **Step 1: Failing tests**
```go
func TestArticleRendersMarginNotes(t *testing.T)            // notes_test.go
	// highlights "brave" (comment "A") and "world" (no comment) -> #later-margin[aria-hidden=true]
	// holds exactly one p.later-margin-note[data-later-hl-open={braveID}] "A"
func TestHighlightSwapRefreshesTheMargin(t *testing.T)      // notes_test.go
	// postHighlight with comment -> #later-margin[hx-swap-oob=true] with the note
func TestListCountsHighlights(t *testing.T)                 // store_test.go: List -> Highlights 2 / 0
func TestRowShowsTheHighlightCount(t *testing.T)            // handlers_test.go: "2 highlights", "1 highlight", none when 0
func TestDeleteConfirmationMentionsHighlights(t *testing.T) // handlers_test.go: both data-later-confirm values
```
(Update any existing test that asserts the old confirmation wording.)

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement**

`article.html`, inside `<article>` after the body:
```html
{{define "later-margin"}}
<div class="later-margin" id="later-margin" aria-hidden="true"{{if .OOB}} hx-swap-oob="true"{{end}}>
	{{range .Highlights}}{{if and .Drawn .Comment}}<p class="later-margin-note" data-later-hl-open="{{.ID}}">{{.Comment}}</p>{{end}}{{end}}
</div>
{{end}}
```
Call it as `{{template "later-margin" $d}}` (readable articles only) and
add `{{template "later-margin" .}}` to `later-highlight-swap`. The margin
is `aria-hidden` because the Notes panel already gives every comment to
assistive technology.

`store.go` `List`: add
`(SELECT count(*) FROM later_highlights h WHERE h.article_id = a.id)` to
the SELECT and scan it into `it.Highlights`. `handlers.go`: copy to
`rowView.Highlights`. `index.html` row meta, after the reading time:
```html
{{if .Highlights}}<span class="later-pill later-pill-hl">{{.Highlights}} {{if eq .Highlights 1}}highlight{{else}}highlights{{end}}</span>{{end}}
```

`later.js` — lay the margin notes out beside their highlights:
```js
	// Margin comments (spec: "comments in the right margin on wide
	// screens"): when the window has room beside the column, each comment
	// sits level with its highlight, pushed down if the one above runs long.
	// Otherwise CSS hides the margin and the 💬 marker shows instead.
	(function () {
		var reader = document.getElementById("later-reader");
		var article = reader && reader.querySelector(".later-article");
		if (!article) return;
		function layout() {
			var margin = document.getElementById("later-margin");
			if (!margin) return;
			var rem = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
			var room = document.documentElement.clientWidth - article.getBoundingClientRect().right;
			var fits = room >= 17 * rem; // 14rem notes + 2rem gap + 1rem edge
			reader.classList.toggle("later-has-margin", fits);
			if (!fits) return;
			var top0 = article.getBoundingClientRect().top;
			var next = 0;
			margin.querySelectorAll(".later-margin-note").forEach(function (n) {
				var mark = document.getElementById("later-h-" + n.dataset.laterHlOpen);
				n.hidden = !mark;
				if (!mark) return;
				var top = Math.max(mark.getBoundingClientRect().top - top0, next);
				n.style.top = top + "px";
				next = top + n.offsetHeight + 8;
			});
		}
		layout();
		window.addEventListener("load", layout);
		window.addEventListener("resize", layout);
		document.body.addEventListener("htmx:afterSettle", layout);
		// Aa changes and late images reflow the column.
		if (typeof ResizeObserver === "function") new ResizeObserver(layout).observe(article);
	})();
```

CSS:
```css
.later-reader .later-article { position: relative; }
.later-margin { display: none; }
.later-has-margin .later-margin { display: block; position: absolute; top: 0; left: calc(100% + 2rem); width: 14rem; }
.later-margin-note { position: absolute; left: 0; right: 0; margin: 0; padding-left: .5rem; border-left: 2px solid var(--c-accent-attention);
  font-size: var(--fs-sm); color: var(--c-text-dim); white-space: pre-wrap; cursor: pointer; }
.later-margin-note[hidden] { display: none; }
/* The 💬 marker for a commented highlight when there is no margin. CSS
   content is not part of textContent, so offsets don't move. */
.later-hl-commented.later-hl-end::after { content: "💬"; font-size: .75em; margin-left: .15em; }
.later-has-margin .later-hl-commented.later-hl-end::after { content: none; }
```
(Check `.later-reader .later-article`'s existing rule and merge rather
than repeat the selector.)

`index.html` and `article.html`: change both `data-later-confirm` values
to `Delete permanently? Highlights and notes go too.`

Docs, in "Highlights and notes":
> On a wide screen, comments sit in the margin beside their highlights;
> click one to edit it. On a narrow screen a 💬 after a highlight means it
> has a comment — tap the highlight to read it. The list shows how many
> highlights each article has.

And "Deleting an article": "Deleting removes its highlights and note too,
and can't be undone."

- [ ] **Step 4: Run → pass; full check; browser check:** at 1440px
comments line up with their highlights and don't overlap each other (two
commented highlights in one paragraph); switching Aa width to Wide moves
or hides the margin correctly; at 1024px and 375px the margin is hidden
and 💬 shows; clicking a margin note opens the edit popover; the list
shows the pill; dark mode.
- [ ] **Step 5: Commit** — `feat(later): margin comments and highlight counts`

---

## After the tasks

- Final whole-branch review, then a PR titled
  `feat(later): ON Later L2 — highlights, comments and the article note (#483)`
  that closes #483, listing the spec deviations above. Screenshots of a
  highlighted article (wide with margin, narrow with 💬) and the Notes
  panel in the PR body are welcome; user-guide screenshots are L4.
- L3 note for the next plan: `later_search` must index highlight quotes
  and comments and the note, kept in step in the transactions Task 1 added
  (`AddHighlight`, `SetHighlightComment`, `DeleteHighlight`, `SetNote`).
