# ON Notes: search as inline filter + highlighting — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace ON Notes' separate `/notes/search` results page with a live, highlighted, in-place filter on the Outline, Due, and Archive pages, closing issue #86.

**Architecture:** A shared `Store.Search`-powered match set drives three independent filters: Outline trims its flat pre-order rows down to matches-plus-their-ancestors (new `filterToMatches`) and highlights matched rows in place; Due and Archive add an optional FTS5 match condition to their existing flat queries. All three reuse one post-render, tag-aware highlighter (`highlight.go`, built on `golang.org/x/net/html` — already an allowed dependency) so a match can never land inside a tag or attribute. The toolbar's search box becomes an HTMX-driven live filter that reuses each page's own GET route (branching on `HX-Request`, the same shape every structural mutation already uses), with a plain GET fallback via `?q=` for JavaScript-off use.

**Tech Stack:** Go (`net/http`, `html/template`), SQLite/FTS5 via `modernc.org/sqlite`, HTMX (vendored), `golang.org/x/net/html` (already a permitted dependency, previously used only from test code).

## Global Constraints

- No CGO, no new dependencies — `golang.org/x/net` is already in `go.mod` and on the project's permitted-dependency list (`AGENTS.md`).
- Migrations are forward-only; the next free migration number for `internal/apps/notes/migrations/` is `0006`.
- Follow the project's commit convention: `type(notes): summary`, e.g. `feat(notes): filter results client-side`. Commit after each task (or each step, where noted) — never batch unrelated tasks into one commit.
- Every new/changed piece of production code needs a passing test before the task is considered done — this plan follows TDD: write the failing test, watch it fail, implement, watch it pass.
- Design reference: [`docs/superpowers/specs/2026-09-07-on-notes-search-filter-design.md`](../specs/2026-09-07-on-notes-search-filter-design.md). Two implementation refinements beyond that doc, decided during planning (both consistent with its stated behaviour, just more specific about mechanism):
  - **Outline's keep-set is computed in memory**, not via a second `AncestorsMany` round trip: `Store.Outline` is queried once with collapse-truncation disabled (`ignoreCollapsed`), and a new `filterToMatches` walks the resulting flat, depth-ordered slice the same way `nest`/`hideDone` already do, marking a row's whole ancestor chain kept as soon as one of its descendants matches. This is naturally scoped to the current zoom (only nodes already in that flat slice can ever be looked up) and avoids fetching ancestors that lie outside it.
  - **A structural mutation or "show completed" toggle performed while a filter is active resets that filter** (the resulting HTMX fragment re-renders unfiltered), rather than threading `?q=` through every mutation route's hidden fields. This falls out of reading the filter query directly off each fragment-rendering request's own URL (a POST to `/notes/{id}/done` never carries `?q=`) rather than needing new code — a deliberate scope boundary, not an oversight; note it if reviewing this plan.

---

## Task 1: FTS5 prefix matching in the store layer

**Files:**
- Create: `internal/apps/notes/migrations/0006_search_prefix.sql`
- Modify: `internal/apps/notes/search.go`
- Test: `internal/apps/notes/search_test.go`

**Interfaces:**
- Produces: `ftsQuery(q string) string` (existing, behaviour changed — now builds prefix queries), `matchedIDsSubquery` (new package-level `const string`, an `id IN (...)` fragment usable by any query already filtering on `user_id`), `searchTerms(query string) []string` (new — splits a raw query into the literal words used for display highlighting, unrelated to FTS syntax).

- [ ] **Step 1: Write the failing test for prefix matching**

Add to `internal/apps/notes/search_test.go`:

```go
// TestSearchMatchesAPrefixOfALongerWord is this feature's whole point: a
// live filter needs to react while a word is still being typed, not only
// once it exactly matches a token — spec's "add prefix matching".
func TestSearchMatchesAPrefixOfALongerWord(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "buy categorically fresh milk")

	got, err := f.store.Search(ctx, f.alice.ID, "categ", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("Search(categ) = %+v, want the one bullet whose word starts with it", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/apps/notes/... -run TestSearchMatchesAPrefixOfALongerWord -v`
Expected: FAIL — `Search(categ)` returns zero rows, because today's FTS5 index has no prefix support and `ftsQuery` builds an exact-token query.

- [ ] **Step 3: Add the prefix-index migration**

Create `internal/apps/notes/migrations/0006_search_prefix.sql`:

```sql
-- Enables FTS5 prefix indexing so a live filter matches while a word is
-- still being typed ("categ" matching "categorically"), not only once a
-- whole token is complete. FTS5's tokenize=/prefix= configuration cannot be
-- altered on an existing virtual table, so notes_fts is dropped and
-- recreated under the same name. The AFTER INSERT/DELETE/UPDATE triggers on
-- notes_nodes (0003_search.sql) reference notes_fts by name only, not by
-- any binding that could break across this — they keep working unchanged
-- once it exists again below.
DROP TABLE notes_fts;

CREATE VIRTUAL TABLE notes_fts USING fts5(
    title, note, content='notes_nodes', content_rowid='id',
    tokenize='unicode61', prefix='2 3 4'
);

INSERT INTO notes_fts(rowid, title, note) SELECT id, title, note FROM notes_nodes;
```

- [ ] **Step 4: Update `ftsQuery` to build prefix queries**

In `internal/apps/notes/search.go`, change `ftsQuery`:

```go
// ftsQuery turns free text into an FTS5 MATCH expression that can never be
// a syntax error. Each word becomes its own quoted phrase — doubling any
// embedded '"' the way FTS5's string literals require — so a user typing an
// operator FTS5 would otherwise interpret (AND, OR, NOT, *, :, an
// unbalanced quote) always searches for that literal text instead of
// breaking the query. Space-separated quoted phrases are ANDed by FTS5's
// own default, so a multi-word search requires every word to appear
// somewhere in the bullet, not necessarily adjacent to the others.
//
// The trailing * after each phrase's closing quote is FTS5's own prefix
// operator — with prefix indexing enabled (0006_search_prefix.sql) this
// matches any token *starting with* the word, not only an exact token, so
// a live-as-you-type filter reacts before a word is finished. A literal '*'
// the user typed lands inside the quotes, where it is inert phrase text,
// not this operator — see TestSearchHandlesFTS5SyntaxCharactersLiterally.
func ftsQuery(q string) string {
	words := strings.Fields(q)
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"*`
	}
	return strings.Join(quoted, " ")
}
```

- [ ] **Step 5: Add `matchedIDsSubquery` and `searchTerms`**

Still in `internal/apps/notes/search.go`, add below `ftsQuery`:

```go
// matchedIDsSubquery is a JOIN-free "which ids match" fragment: notes_fts's
// rowid is exactly notes_nodes.id (external-content table), so no join to
// notes_nodes is needed just to test membership. It carries no user_id
// filter of its own — every caller embeds it inside a WHERE that already
// has its own "user_id = ?", and "id IN (<possibly other users' ids too>)
// AND user_id = ?" is still correct, since a given id belongs to exactly
// one user regardless of what else this subquery happens to return.
//
// Takes exactly one ? — the query string ftsQuery already built.
const matchedIDsSubquery = `id IN (SELECT rowid FROM notes_fts WHERE notes_fts MATCH ?)`

// searchTerms splits a raw query into the literal words used for display
// highlighting (highlight.go) — deliberately not ftsQuery's escaped/quoted
// FTS5 syntax, since these are matched against rendered text with plain
// substring comparison, not sent to SQLite.
func searchTerms(query string) []string {
	return strings.Fields(query)
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/apps/notes/... -run TestSearch -v`
Expected: PASS for every `TestSearch*` test, including the new one. `TestSearchHandlesFTS5SyntaxCharactersLiterally` must still pass unchanged — the literal `*`/`OR`/unbalanced-quote characters it feeds in still land inside `ftsQuery`'s quotes, so nothing there gains new meaning.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/notes/migrations/0006_search_prefix.sql internal/apps/notes/search.go internal/apps/notes/search_test.go
git commit -m "feat(notes): match a prefix of a longer word while filtering"
```

---

## Task 2: The highlighter

**Files:**
- Create: `internal/apps/notes/highlight.go`
- Test: `internal/apps/notes/highlight_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `matchSpans(text string, terms []string) []span` (package-private `span{start, end int}`, byte offsets into `text`), `highlight(rendered template.HTML, terms []string) template.HTML`, `highlightPlainText(text string, terms []string) template.HTML`, `noteSnippet(note string, terms []string) template.HTML`. Later tasks call all four by these exact names.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/notes/highlight_test.go`:

```go
package notes

import (
	"html/template"
	"strings"
	"testing"
)

func TestMatchSpansFindsACaseInsensitiveMatch(t *testing.T) {
	spans := matchSpans("Buy Milk", []string{"milk"})
	if len(spans) != 1 || spans[0].start != 4 || spans[0].end != 8 {
		t.Fatalf("matchSpans = %+v, want one span covering \"Milk\"", spans)
	}
}

func TestMatchSpansPicksTheLongestOverlappingTerm(t *testing.T) {
	// "cat" and "category" both start at the same position; wrapping "cat"
	// alone first would leave "egory" behind as unmatched, or double-wrap.
	spans := matchSpans("a category", []string{"cat", "category"})
	if len(spans) != 1 || spans[0].end-spans[0].start != len("category") {
		t.Fatalf("matchSpans = %+v, want one span covering the whole word", spans)
	}
}

func TestMatchSpansReturnsNoneForEmptyTerms(t *testing.T) {
	if spans := matchSpans("anything", nil); spans != nil {
		t.Fatalf("matchSpans with no terms = %+v, want nil", spans)
	}
}

func TestHighlightWrapsAPlainTextMatch(t *testing.T) {
	got := highlight(template.HTML("buy milk today"), []string{"milk"})
	if !strings.Contains(string(got), `<mark class="notes-search-hit">milk</mark>`) {
		t.Errorf("highlight = %q, want a wrapped mark", got)
	}
}

// TestHighlightNeverMatchesInsideATag is the whole reason this runs on the
// rendered tree rather than as a string replace: an href is not visible
// text, and a false match there must never become a mark.
func TestHighlightNeverMatchesInsideATag(t *testing.T) {
	rendered := template.HTML(`<a href="https://milk.example.com">buy stuff</a>`)
	got := highlight(rendered, []string{"milk"})
	if strings.Contains(string(got), "<mark") {
		t.Errorf("highlight = %q, matched inside the href", got)
	}
	if !strings.Contains(string(got), `href="https://milk.example.com"`) {
		t.Errorf("highlight = %q, corrupted the href", got)
	}
}

// TestHighlightWorksAroundExistingTags proves a match spanning across what
// was already <strong>/<em>/etc. still gets wrapped without breaking that
// markup — Render's own output, not raw Markdown source, is what this sees.
func TestHighlightWorksAroundExistingTags(t *testing.T) {
	rendered := template.HTML(`buy <strong>whole milk</strong> today`)
	got := highlight(rendered, []string{"milk"})
	if !strings.Contains(string(got), `<strong>whole <mark class="notes-search-hit">milk</mark></strong>`) {
		t.Errorf("highlight = %q, want the mark nested inside strong", got)
	}
}

func TestHighlightWithNoTermsReturnsInputUnchanged(t *testing.T) {
	rendered := template.HTML(`<strong>hi</strong>`)
	if got := highlight(rendered, nil); got != rendered {
		t.Errorf("highlight with no terms = %q, want the input unchanged", got)
	}
}

func TestHighlightPlainTextEscapesAndWraps(t *testing.T) {
	got := highlightPlainText(`<b>milk</b> & eggs`, []string{"milk"})
	want := `&lt;b&gt;<mark class="notes-search-hit">milk</mark>&lt;/b&gt; &amp; eggs`
	if string(got) != want {
		t.Errorf("highlightPlainText = %q, want %q", got, want)
	}
}

func TestHighlightPlainTextWithNoMatchJustEscapes(t *testing.T) {
	got := highlightPlainText("plain <text>", nil)
	if string(got) != "plain &lt;text&gt;" {
		t.Errorf("highlightPlainText = %q, want escaped with no marks", got)
	}
}

func TestNoteSnippetIsEmptyWithNoMatch(t *testing.T) {
	if got := noteSnippet("nothing relevant here", []string{"milk"}); got != "" {
		t.Errorf("noteSnippet with no match = %q, want empty", got)
	}
}

func TestNoteSnippetHighlightsAndTruncates(t *testing.T) {
	long := strings.Repeat("x", 80) + " milk " + strings.Repeat("y", 80)
	got := noteSnippet(long, []string{"milk"})
	s := string(got)
	if !strings.Contains(s, `<mark class="notes-search-hit">milk</mark>`) {
		t.Errorf("noteSnippet = %q, want the match highlighted", s)
	}
	if !strings.HasPrefix(s, "…") || !strings.HasSuffix(s, "…") {
		t.Errorf("noteSnippet = %q, want ellipses on both sides", s)
	}
	if len(s) >= len(long) {
		t.Errorf("noteSnippet did not truncate: got %d bytes from a %d-byte note", len(s), len(long))
	}
}

func TestNoteSnippetKeepsShortNoteWholeWithNoEllipsis(t *testing.T) {
	got := noteSnippet("short milk note", []string{"milk"})
	s := string(got)
	if strings.Contains(s, "…") {
		t.Errorf("noteSnippet = %q, a short note should not be truncated", s)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/... -run 'TestMatchSpans|TestHighlight|TestNoteSnippet' -v`
Expected: FAIL to compile — `matchSpans`, `highlight`, `highlightPlainText`, `noteSnippet` do not exist yet.

- [ ] **Step 3: Implement the highlighter**

Create `internal/apps/notes/highlight.go`:

```go
package notes

import (
	"bytes"
	"html"
	"html/template"
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// span is a byte-offset match, [start, end), into the text it was found in.
type span struct{ start, end int }

// matchSpans finds every case-insensitive, non-overlapping occurrence of
// any of terms in text, using strings.EqualFold rather than a
// lowercase-and-compare: some Unicode case folds change a string's byte
// length (ß, Kelvin sign, ...), and slicing text at offsets computed from a
// separately-lowercased copy would then point at the wrong bytes.
// EqualFold's own rune-by-rune comparison has no such mismatch, at the cost
// of an extremely rare, harmless false negative: a term that only matches
// via a length-changing fold at a position that also shifts the term's own
// byte length (no realistic note content exercises this).
//
// terms is assumed already lower-cased by the caller (searchTerms's output
// is not — callers pass it through here as-is; matching against it with
// EqualFold makes that unnecessary, since EqualFold ignores case on both
// sides). Where two terms both match at the same position (e.g. "cat" and
// "category"), the longest wins, so a query for both can never produce a
// double-wrapped or truncated span for the same text.
//
// Every match FTS5 finds via matchedIDsSubquery corresponds, for this
// project's tokenize='unicode61' with no diacritic folding, to a real
// contiguous substring of the original text — so a row the database
// selected as a match will always have at least one span here too.
func matchSpans(text string, terms []string) []span {
	if len(terms) == 0 {
		return nil
	}
	var spans []span
	for i := 0; i < len(text); {
		matchLen := 0
		for _, term := range terms {
			if term == "" {
				continue
			}
			end := i + len(term)
			if end <= len(text) && strings.EqualFold(text[i:end], term) && len(term) > matchLen {
				matchLen = len(term)
			}
		}
		if matchLen == 0 {
			i++
			continue
		}
		spans = append(spans, span{i, i + matchLen})
		i += matchLen
	}
	return spans
}

// highlight wraps every match in rendered — HTML already produced by
// Render — in <mark class="notes-search-hit">. It parses rendered as an
// HTML fragment, walks it, and re-serializes it, touching only text nodes:
// a match can never land inside a tag name or an attribute value (a link's
// href, for instance), the way a plain string-replace risks.
//
// This runs on already-rendered HTML rather than on raw Markdown source
// deliberately: a term wrapped in <mark> before Markdown rendering could
// straddle a **bold**/`code` delimiter and stop that construct from
// triggering at all. Doing it after, on the rendered tree's own text,
// cannot corrupt markup Render has already decided on — see this feature's
// design doc, docs/superpowers/specs/2026-09-07-on-notes-search-filter-design.md.
func highlight(rendered template.HTML, terms []string) template.HTML {
	if len(terms) == 0 {
		return rendered
	}

	nodes, err := xhtml.ParseFragment(strings.NewReader(string(rendered)),
		&xhtml.Node{Type: xhtml.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		// rendered is always Render's own output — well-formed by
		// construction — so a parse failure here would be a bug in Render,
		// not bad user input. Falling back to the unhighlighted original is
		// safer than losing the row's content entirely.
		return rendered
	}
	for _, n := range nodes {
		highlightNode(n, terms)
	}

	var buf bytes.Buffer
	for _, n := range nodes {
		if err := xhtml.Render(&buf, n); err != nil {
			return rendered
		}
	}
	return template.HTML(buf.String())
}

// highlightNode walks n and its descendants, splicing <mark> elements in
// place of any text node that contains a match.
func highlightNode(n *xhtml.Node, terms []string) {
	if n.Type == xhtml.TextNode {
		splitTextNode(n, terms)
		return
	}
	// A child's own NextSibling changes as splitTextNode inserts nodes
	// beside it, so the next pointer is captured before recursing into it.
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		highlightNode(c, terms)
		c = next
	}
}

// splitTextNode replaces text node n with a run of text and <mark>
// siblings wherever matchSpans finds a match, then removes n itself.
func splitTextNode(n *xhtml.Node, terms []string) {
	text := n.Data
	spans := matchSpans(text, terms)
	if len(spans) == 0 {
		return
	}

	parent, before := n.Parent, n
	insert := func(newNode *xhtml.Node) { parent.InsertBefore(newNode, before) }

	last := 0
	for _, sp := range spans {
		if sp.start > last {
			insert(&xhtml.Node{Type: xhtml.TextNode, Data: text[last:sp.start]})
		}
		// DataAtom is left at its zero value: "mark" has no entry in
		// golang.org/x/net/html/atom's table (generated only from the tags
		// its parser needs to recognize by fast lookup), but html.Render
		// serializes an element from its Data string, not DataAtom — so an
		// unset DataAtom here does not affect the output.
		mark := &xhtml.Node{Type: xhtml.ElementNode, Data: "mark",
			Attr: []xhtml.Attribute{{Key: "class", Val: "notes-search-hit"}}}
		mark.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: text[sp.start:sp.end]})
		insert(mark)
		last = sp.end
	}
	if last < len(text) {
		insert(&xhtml.Node{Type: xhtml.TextNode, Data: text[last:]})
	}
	parent.RemoveChild(n)
}

// highlightPlainText escapes text and wraps every match in
// <mark class="notes-search-hit"> — the Due/Archive row title's own
// version of highlight, simpler than that one because there is no existing
// markup to preserve: a Due/Archive row's title is deliberately never run
// through Render (DisplayTitleHTML's own doc comment explains why — nesting
// an <a> the title's Markdown might itself produce, inside the <a> the row
// already is, is invalid HTML), so this only ever emits escaped text and
// <mark>, never anything Render would have produced.
func highlightPlainText(text string, terms []string) template.HTML {
	spans := matchSpans(text, terms)
	if len(spans) == 0 {
		return template.HTML(html.EscapeString(text))
	}
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(html.EscapeString(text[last:sp.start]))
		b.WriteString(`<mark class="notes-search-hit">`)
		b.WriteString(html.EscapeString(text[sp.start:sp.end]))
		b.WriteString(`</mark>`)
		last = sp.end
	}
	b.WriteString(html.EscapeString(text[last:]))
	return template.HTML(b.String())
}

// snippetContextRunes bounds how much raw text surrounds the first match in
// a Due/Archive row's note-only snippet (issue #86: some sign of why a row
// with no title match is in the results) — a short excerpt, not the note.
const snippetContextRunes = 40

// noteSnippet extracts a window of raw text around the first match of any
// term in note, renders it (spec's Markdown pipeline) and highlights it —
// empty when nothing in note matches, which is also the caller's own signal
// that this row's match was in the title instead, and no snippet is needed.
func noteSnippet(note string, terms []string) template.HTML {
	spans := matchSpans(note, terms)
	if len(spans) == 0 {
		return ""
	}

	runes := []rune(note)
	matchStartRune := len([]rune(note[:spans[0].start]))
	matchEndRune := len([]rune(note[:spans[0].end]))

	start := matchStartRune - snippetContextRunes
	truncatedBefore := start > 0
	if start < 0 {
		start = 0
	}
	end := matchEndRune + snippetContextRunes
	truncatedAfter := end < len(runes)
	if end > len(runes) {
		end = len(runes)
	}

	window := string(runes[start:end])
	if truncatedBefore {
		window = "…" + window
	}
	if truncatedAfter {
		window += "…"
	}
	return highlight(Render(window), terms)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -run 'TestMatchSpans|TestHighlight|TestNoteSnippet' -v`
Expected: PASS for all of them.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/notes/highlight.go internal/apps/notes/highlight_test.go
git commit -m "feat(notes): highlight matched search terms in rendered text"
```

---

## Task 3: `Store.Outline` gains `ignoreCollapsed`

**Files:**
- Modify: `internal/apps/notes/store.go:379-428`
- Test: `internal/apps/notes/store_test.go`, `internal/apps/notes/tree_test.go`

**Interfaces:**
- Produces: `Store.Outline(ctx, userID, rootID int64, showCompleted, ignoreCollapsed bool) ([]Node, error)` — a new trailing `bool` parameter. `false` reproduces every existing caller's current behaviour exactly.

- [ ] **Step 1: Write the failing test**

Add to `internal/apps/notes/store_test.go` (near the other `Outline` tests):

```go
// TestOutlineIgnoreCollapsedRevealsChildrenOfACollapsedNode is what the
// search-as-filter feature needs Store.Outline for: a match under a
// collapsed ancestor must still be fetched, so the handler layer can decide
// what to keep rather than never seeing it in the first place.
func TestOutlineIgnoreCollapsedRevealsChildrenOfACollapsedNode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent")
	child := f.mk(t, parent.ID, "child")
	if err := f.store.SetCollapsed(ctx, f.alice.ID, parent.ID, true); err != nil {
		t.Fatal(err)
	}

	normal, err := f.store.Outline(ctx, f.alice.ID, notes.RootID, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(normal) != 1 {
		t.Fatalf("Outline(ignoreCollapsed=false) = %+v, want just the collapsed parent", normal)
	}

	all, err := f.store.Outline(ctx, f.alice.ID, notes.RootID, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[1].ID != child.ID {
		t.Fatalf("Outline(ignoreCollapsed=true) = %+v, want the parent and its child", all)
	}
}
```

If `SetCollapsed` is not the store method's real name, use whatever `collapse.go`/`tree.go` already exposes for setting a node's `collapsed` flag (check `Ops.SetCollapsed`/`Store`'s own wrapper before writing this step — search for `func.*Collapsed` in `internal/apps/notes/*.go`).

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/apps/notes/... -run TestOutlineIgnoreCollapsed -v`
Expected: FAIL to compile — `Store.Outline` does not yet accept a fourth argument.

- [ ] **Step 3: Add the parameter**

In `internal/apps/notes/store.go`, change `Outline`'s signature and the recursive step's WHERE clause:

```go
func (st *Store) Outline(ctx context.Context, userID, rootID int64, showCompleted, ignoreCollapsed bool) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE tree AS (
		     SELECT `+nodeColumns+`, 0 AS depth, printf('%08d', position) AS path
		       FROM notes_nodes
		      WHERE user_id = ? AND parent_id IS ? AND archived_at IS NULL
		   UNION ALL
		     SELECT `+childColumns+`,
		            t.depth + 1, t.path || '/' || printf('%08d', c.position)
		       FROM notes_nodes c JOIN tree t ON c.parent_id = t.id
		      WHERE c.user_id = t.user_id AND c.archived_at IS NULL
		        AND (? OR t.collapsed = 0) AND t.depth + 1 <= ?
		 )
		 SELECT `+nodeColumns+`, depth,
		        EXISTS (SELECT 1 FROM notes_nodes k
		                 WHERE k.user_id = tree.user_id AND k.parent_id = tree.id
		                   AND k.archived_at IS NULL AND (? OR k.done_at IS NULL)),
		        (SELECT count(*) FROM notes_nodes k
		          WHERE k.user_id = tree.user_id AND k.parent_id = tree.id
		            AND k.archived_at IS NULL) AS child_count,
		        (SELECT count(*) FROM notes_nodes k
		          WHERE k.user_id = tree.user_id AND k.parent_id = tree.id
		            AND k.archived_at IS NULL AND k.done_at IS NOT NULL) AS done_child_count
		   FROM tree ORDER BY path`,
		userID, parentArg(rootID), ignoreCollapsed, MaxDepth, showCompleted)
	if err != nil {
		return nil, fmt.Errorf("notes: outline of %d: %w", rootID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Node
	for rows.Next() {
		var (
			depth          int
			hasChildren    bool
			childCount     int
			doneChildCount int
		)
		n, err := scanNode(rows, &depth, &hasChildren, &childCount, &doneChildCount)
		if err != nil {
			return nil, err
		}
		n.Depth, n.HasChildren, n.ChildCount, n.DoneChildCount = depth, hasChildren, childCount, doneChildCount
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notes: outline: %w", err)
	}
	return out, nil
}
```

Also extend the method's doc comment (the large comment block already above `Outline` in `store.go`) with one paragraph:

```go
// ignoreCollapsed, when true, descends through a collapsed node instead of
// stopping at it — spec: search-as-filter fetches a whole zoomed subtree so
// filterToMatches (view.go) can decide what survives, rather than having a
// collapsed ancestor hide a match from this query before that decision ever
// runs. Every caller not filtering passes false, reproducing this method's
// original behaviour exactly.
```

- [ ] **Step 4: Update every existing call site**

`Store.Outline` is called with three arguments in these places; add a trailing `, false` to each (they are all testing or rendering ordinary, non-filtered behaviour):

- `internal/apps/notes/handlers.go:122` and `:147` — these two are rewritten in Task 5 anyway (they gain real filtering logic there), so for *this* task just append `, false` to keep the build green in between tasks.
- `internal/apps/notes/store_test.go:298,316,332,347,360,441,563,593,620,650,682` — append `, false`.
- `internal/apps/notes/store_test.go:661` — this one already passes `true` for `showCompleted`; append `, false` for the new `ignoreCollapsed` argument (so the call becomes `f.store.Outline(ctx, f.alice.ID, notes.RootID, true, false)`).
- `internal/apps/notes/tree_test.go:876,976,1015,1228,1391` — append `, false`.

- [ ] **Step 5: Run the full package test suite to verify everything compiles and passes**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS. (Task 5 will later change `handlers.go`'s two call sites again to pass real filtering — that's expected and fine.)

- [ ] **Step 6: Commit**

```bash
git add internal/apps/notes/store.go internal/apps/notes/store_test.go internal/apps/notes/tree_test.go internal/apps/notes/handlers.go
git commit -m "feat(notes): let Outline descend through a collapsed node on request"
```

---

## Task 4: `filterToMatches` and highlighting in `nest`

**Files:**
- Modify: `internal/apps/notes/view.go`
- Test: `internal/apps/notes/view_test.go`

**Interfaces:**
- Consumes: `highlight(template.HTML, []string) template.HTML` (Task 2), `MaxDepth` (existing constant).
- Produces: `filterToMatches(flat []Node, matched map[int64]bool) []Node`; `nest`'s signature grows to `nest(flat []Node, root int64, csrfToken, today string, matched map[int64]bool, terms []string) []*outlineRow`; `idSet(nodes []Node) map[int64]bool` (a small helper, sibling to the existing `idsOf` in `handlers.go` — put it in `view.go` since it is view-layer plumbing, not HTTP plumbing).

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/notes/view_test.go`. First, check the file's existing `flat`/`lvl` helper (used by the other `nest` tests) and reuse it rather than redefining it.

```go
func TestFilterToMatchesKeepsOnlyMatchesAndTheirAncestors(t *testing.T) {
	// a
	//   a1
	//     a1x   <- matches
	//   a2      <- no match, no matching descendant: dropped
	rows := flat(
		lvl{0, "a"}, lvl{1, "a1"}, lvl{2, "a1x"}, lvl{1, "a2"},
	)
	var a1xID int64
	for _, n := range rows {
		if n.Title == "a1x" {
			a1xID = n.ID
		}
	}

	kept := filterToMatches(rows, map[int64]bool{a1xID: true})
	if len(kept) != 3 {
		t.Fatalf("filterToMatches kept %d rows, want 3 (a, a1, a1x): %+v", len(kept), kept)
	}
	for _, title := range []string{"a", "a1", "a1x"} {
		found := false
		for _, n := range kept {
			if n.Title == title {
				found = true
			}
		}
		if !found {
			t.Errorf("filterToMatches dropped %q, which should have been kept", title)
		}
	}
}

func TestFilterToMatchesWithNilMatchedReturnsInputUnchanged(t *testing.T) {
	rows := flat(lvl{0, "a"}, lvl{1, "a1"})
	got := filterToMatches(rows, nil)
	if len(got) != len(rows) {
		t.Fatalf("filterToMatches(nil) changed the length: got %d, want %d", len(got), len(rows))
	}
}

// TestFilterToMatchesExpandsACollapsedAncestor is the chevron-icon fix: an
// ancestor kept only for context, whose real Collapsed was true, must not
// still claim to be collapsed once its matching child is being shown right
// below it in this same response.
func TestFilterToMatchesExpandsACollapsedAncestor(t *testing.T) {
	rows := flat(lvl{0, "a"}, lvl{1, "a1"})
	rows[0].Collapsed = true
	var a1ID int64
	for _, n := range rows {
		if n.Title == "a1" {
			a1ID = n.ID
		}
	}

	kept := filterToMatches(rows, map[int64]bool{a1ID: true})
	for _, n := range kept {
		if n.Title == "a" && n.Collapsed {
			t.Error("the ancestor still reports Collapsed = true despite its match being shown")
		}
	}
}

func TestNestHighlightsAMatchedRow(t *testing.T) {
	n := Node{ID: 1, Title: "buy milk", Depth: 0}
	rows := nest([]Node{n}, RootID, "tok", "9999-12-31", map[int64]bool{1: true}, []string{"milk"})
	if !strings.Contains(string(rows[0].RenderedTitle), `<mark class="notes-search-hit">milk</mark>`) {
		t.Errorf("RenderedTitle = %q, want the match highlighted", rows[0].RenderedTitle)
	}
}

func TestNestDoesNotHighlightAnUnmatchedAncestorRow(t *testing.T) {
	n := Node{ID: 1, Title: "buy milk", Depth: 0}
	// matched is non-nil (filtering is active) but this row's own id isn't
	// in it — it's present only as an ancestor of some other match.
	rows := nest([]Node{n}, RootID, "tok", "9999-12-31", map[int64]bool{999: true}, []string{"milk"})
	if strings.Contains(string(rows[0].RenderedTitle), "<mark") {
		t.Errorf("RenderedTitle = %q, an ancestor-only row should not be highlighted", rows[0].RenderedTitle)
	}
}

func TestIdSetBuildsAMembershipMapFromNodeIDs(t *testing.T) {
	set := idSet([]Node{{ID: 3}, {ID: 7}})
	if !set[3] || !set[7] || set[5] {
		t.Errorf("idSet = %+v, want {3:true, 7:true}", set)
	}
}
```

Add `"strings"` to this test file's imports if not already present.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/apps/notes/... -run 'TestFilterToMatches|TestNestHighlights|TestNestDoesNotHighlight|TestIdSet' -v`
Expected: FAIL to compile — none of `filterToMatches`, `idSet`, or `nest`'s new parameters exist yet.

- [ ] **Step 3: Implement `filterToMatches`, `idSet`, and `nest`'s highlighting**

In `internal/apps/notes/view.go`, change `nest`:

```go
func nest(flat []Node, root int64, csrfToken, today string, matched map[int64]bool, terms []string) []*outlineRow {
	var top []*outlineRow

	// open is the ancestor chain of the row most recently added: open[d] is
	// that chain's row at depth d, so a row at depth d attaches to open[d-1].
	// It holds pointers, not indices into a slice that append may move.
	open := make([]*outlineRow, 0, MaxDepth+1)

	for _, n := range flat {
		titleHTML, noteHTML := Render(n.Title), Render(n.Note)
		if matched != nil && matched[n.ID] {
			titleHTML = highlight(titleHTML, terms)
			noteHTML = highlight(noteHTML, terms)
		}
		row := &outlineRow{
			Node: n, RootID: root, CSRFToken: csrfToken,
			RenderedTitle: titleHTML,
			RenderedNote:  noteHTML,
			Overdue:       n.DueOn != "" && n.DueOn < today,
		}

		switch d := n.Depth; {
		case d == 0:
			top = append(top, row)
			open = open[:0]
		case d > 0 && d <= len(open):
			parent := open[d-1]
			parent.Children = append(parent.Children, row)
			open = open[:d]
		default:
			// A depth that skips a level, or a negative one. Leaving open
			// untouched is what makes this row's own descendants fall into
			// this branch too.
			continue
		}
		open = append(open, row)
	}

	markLast(top)
	return top
}

// filterToMatches drops every row from Outline's own flat pre-order slice
// that is neither itself in matched nor an ancestor of one — the
// search-as-filter behaviour: a hit's ancestor path stays visible for
// context, but an unrelated sibling subtree does not. matched is nil for
// "not filtering", in which case flat is returned unchanged.
//
// A kept row that has a kept child has its own Collapsed forced false:
// Store.Outline is queried with ignoreCollapsed when filtering, so a match
// under a collapsed ancestor is not silently absent from flat in the first
// place — but outline-rows.html's chevron/dot icon still reads a row's own
// Collapsed field, and without this a collapsed ancestor would show its
// "collapsed" icon directly above a child this same response is about to
// render right below it.
func filterToMatches(flat []Node, matched map[int64]bool) []Node {
	if matched == nil {
		return flat
	}

	keep := make([]bool, len(flat))
	hasKeptChild := make([]bool, len(flat))
	// open holds the ancestor chain's indices into flat — the same
	// technique nest uses above to attach a row to its parent, relying on
	// the same guarantee: a parent immediately precedes its subtree, and
	// depth rises by at most one from a row to the next.
	open := make([]int, 0, MaxDepth+1)

	for i, n := range flat {
		switch d := n.Depth; {
		case d == 0:
			open = open[:0]
		case d > 0 && d <= len(open):
			open = open[:d]
		default:
			continue
		}
		if matched[n.ID] {
			keep[i] = true
			for _, a := range open {
				keep[a] = true
				hasKeptChild[a] = true
			}
		}
		open = append(open, i)
	}

	out := make([]Node, 0, len(flat))
	for i, n := range flat {
		if !keep[i] {
			continue
		}
		if hasKeptChild[i] {
			n.Collapsed = false
		}
		out = append(out, n)
	}
	return out
}

// idSet builds a membership map from a slice of nodes' ids — the shape
// filterToMatches and nest's own matched parameter want, out of whatever
// Store.Search just returned.
func idSet(nodes []Node) map[int64]bool {
	set := make(map[int64]bool, len(nodes))
	for _, n := range nodes {
		set[n.ID] = true
	}
	return set
}
```

- [ ] **Step 4: Update every existing call site of `nest`**

Add trailing `, nil, nil` to every call in `internal/apps/notes/view_test.go` at lines 53, 68, 89, 112, 125, 148, 158, 172, 185 (each currently ends `..., "tok", "9999-12-31")` or similar — the new call ends `..., "9999-12-31", nil, nil)`).

Leave `internal/apps/notes/handlers.go:129` and `:166` alone for now — Task 5 rewrites both properly.

- [ ] **Step 5: Run the full package test suite**

Run: `go test ./internal/apps/notes/... -v`
Expected: Everything compiles; the new tests pass. `handlers.go` will not yet compile cleanly against `nest`'s new signature — that's fine, Task 5 fixes it next; if this session's toolchain refuses to run `go test` at all with a compile error elsewhere in the package, temporarily append `, nil, nil` to `handlers.go:129` and `:166` too, confirm this task's tests pass, then let Task 5 replace those two lines properly.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/notes/view.go internal/apps/notes/view_test.go
git commit -m "feat(notes): keep a match's ancestor path and highlight the hit"
```

---

## Task 5: Wire the Outline page

**Files:**
- Modify: `internal/apps/notes/handlers.go:53-170`
- Modify: `internal/apps/notes/view.go` (add `Query`/`SearchAction` fields to `outlineView`)
- Modify: `internal/apps/notes/templates/toolbar.partial.html`
- Modify: `internal/apps/notes/templates/outline.html`
- Test: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `Store.Outline(..., ignoreCollapsed)` (Task 3), `filterToMatches`, `nest(..., matched, terms)`, `idSet` (Task 4), `searchTerms` (Task 1), `Store.Search` (existing).
- Produces: `outlineView.Query string`, `outlineView.SearchAction string`; the shared `notes-search-box` partial now takes `Action`/`Target` dict keys in addition to the existing `Query`/`Autofocus`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/notes/handlers_test.go`:

```go
// TestOutlineFilterKeepsAMatchAndItsAncestorOnly is the filter's core
// behaviour: a match's ancestor path stays, an unrelated sibling subtree
// does not.
func TestOutlineFilterKeepsAMatchAndItsAncestorOnly(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.Alice, notes.RootID, "Projects")
	s.seed(t, s.Alice, parent, "Budget report")
	s.seed(t, s.Alice, notes.RootID, "unrelated top-level bullet")

	doc := s.Get(t, s.Alice, "/notes/?q=budget")
	if !strings.Contains(doc.Text(), "Projects") {
		t.Error("the match's ancestor is missing")
	}
	if strings.Contains(doc.Text(), "unrelated top-level bullet") {
		t.Error("an unrelated sibling subtree leaked into the filtered view")
	}
}

// TestOutlineFilterHighlightsTheMatch guards the actual visible <mark>.
func TestOutlineFilterHighlightsTheMatch(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "buy milk today")

	doc := s.Get(t, s.Alice, "/notes/?q=milk")
	mark := doc.MustHave("mark.notes-search-hit")
	if got := htmlassert.Text(mark); !strings.EqualFold(got, "milk") {
		t.Errorf("highlighted text = %q, want milk", got)
	}
}

func TestOutlineFilterWithNoMatchesSaysSo(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "anything")

	doc := s.Get(t, s.Alice, "/notes/?q=nonexistent")
	if !strings.Contains(doc.Text(), "No notes match") {
		t.Error("an empty filtered result shows no feedback")
	}
}

func TestOutlineFilterOverHTMXRendersOnlyTheFragment(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "buy milk")

	req := httptest.NewRequest("GET", "/notes/?q=milk", nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<html") || strings.Contains(body, "<!DOCTYPE") {
		t.Errorf("an HTMX filter request got a full page, not a fragment: %q", body)
	}
	if !strings.Contains(body, "milk") {
		t.Error("the fragment does not contain the match")
	}
}

// TestOutlineSearchBoxTargetsOutlineOverHTMX pins the wiring the live
// filter depends on.
func TestOutlineSearchBoxTargetsOutlineOverHTMX(t *testing.T) {
	s := newServer(t)
	in := s.Get(t, s.Alice, "/notes/").MustHave("#notes-search-input")
	if got, _ := htmlassert.Attr(in, "hx-get"); got != "/notes/" {
		t.Errorf("search box hx-get = %q, want /notes/", got)
	}
	if got, _ := htmlassert.Attr(in, "hx-target"); got != "#outline" {
		t.Errorf("search box hx-target = %q, want #outline", got)
	}
}

// TestOutlineFilterScopedToTheCurrentZoom: a match outside the zoomed
// subtree must not appear, even though Store.Search itself searches the
// whole tree — the handler's own intersection with the fetched subtree is
// what scopes it.
func TestOutlineFilterScopedToTheCurrentZoom(t *testing.T) {
	s := newServer(t)
	zoomRoot := s.seed(t, s.Alice, notes.RootID, "Zoomed root")
	s.seed(t, s.Alice, notes.RootID, "outside milk bullet")

	doc := s.Get(t, s.Alice, "/notes/"+itoa(zoomRoot)+"?q=milk")
	doc.MustNotHave(".outline-item")
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/apps/notes/... -run TestOutlineFilter -v`
Expected: FAIL — `?q=` is not read by the outline handler at all yet, so nothing is filtered and no `<mark>` appears.

- [ ] **Step 3: Add `Query`/`SearchAction` to `outlineView`**

In `internal/apps/notes/view.go`, add two fields to `outlineView` (next to the existing `ShowCompleted`):

```go
	// Query is the current filter, "" when not filtering — spec: search as
	// an inline filter. SearchAction is the toolbar search box's own
	// hx-get/action target: outlinePath(rootID), so filtering never leaves
	// whatever zoom the page is already on.
	Query        string
	SearchAction string
```

- [ ] **Step 4: Rewrite the toolbar's `notes-search-box` partial**

In `internal/apps/notes/templates/toolbar.partial.html`, replace the `notes-search-box` define:

```html
{{/* notes-search-box is the toolbar search box, shared by every ON Notes
     page's toolbar (outline, due, archive) — issue #89. It is also this
     feature's live filter: hx-get targets Action (each page's own GET
     route) with hx-target Target (that page's own list container), so
     typing re-renders just that list in place; hx-push-url keeps the
     filter in the URL, so a refresh or the back button restores it.
     "search" as an hx-trigger event, alongside "input changed delay:300ms",
     fires immediately when a user clicks the native <input type=search>'s
     own clear (x) button, rather than waiting out the debounce.

     Action/Target vary per page and are passed via dict at each call site,
     alongside the existing Query/Autofocus (search.html alone used to
     prefill the last query and autofocus; that page is gone as of this
     feature, but every other page still wants neither). role="search" is
     the landmark WCAG asks for alongside the input's own aria-label. */}}
{{define "notes-search-box"}}<form method="get" action="{{.Action}}" class="notes-search" role="search">
	<input type="search" id="notes-search-input" name="q" value="{{.Query}}"
	       placeholder="Search…" aria-label="Search notes"{{if .Autofocus}} autofocus{{end}}
	       hx-get="{{.Action}}" hx-target="{{.Target}}" hx-swap="innerHTML"
	       hx-trigger="input changed delay:300ms, search" hx-push-url="true">
</form>{{end}}
```

- [ ] **Step 5: Wire the Outline handler**

In `internal/apps/notes/handlers.go`, replace `outline`, `outlineZoomed`, `renderOutline`, and `renderOutlineFragment`. Both functions below still call `a.store.Due(r.Context(), userID)` with its current two-argument signature — Task 7 adds a third `query` argument to `Store.Due` and updates these same two call sites to pass `""` (always the unfiltered due-count, regardless of any outline filter); do not add that argument here yet, or the package will not build until Task 7 lands.

```go
// outline renders the top-level outline.
func (a *App) outline(w http.ResponseWriter, r *http.Request) {
	a.renderOutlineOrFragment(w, r, RootID)
}

// outlineZoomed renders the outline rooted at one node — spec §6: zoom is the
// URL, and the only difference from the top level is which node the recursive
// query starts at.
func (a *App) outlineZoomed(w http.ResponseWriter, r *http.Request) {
	id, ok := a.nodeID(w, r)
	if !ok {
		return
	}
	a.renderOutlineOrFragment(w, r, id)
}

// renderOutlineOrFragment is outline/outlineZoomed's shared body. A plain
// GET renders the whole page; the toolbar's search box drives its live
// filter through this very same route over HTMX (hx-get, targeting
// #outline) — the same GET-request-branches-on-HX-Request shape every
// structural mutation route already uses (mutateThen), just reached by GET
// instead of POST.
func (a *App) renderOutlineOrFragment(w http.ResponseWriter, r *http.Request, rootID int64) {
	if web.IsHTMX(r) {
		userID, ok := a.userID(w, r)
		if !ok {
			return
		}
		a.renderOutlineFragment(w, r, userID, rootID, showCompletedFrom(r))
		return
	}
	a.renderOutline(w, r, rootID)
}

// renderOutline draws the outline rooted at rootID: the breadcrumb, the
// visible rows, and nothing else. RootID means the top level.
//
// Every query here runs on the pool, outside any transaction, which is the
// only safe place for them — see the warning on mutate.
func (a *App) renderOutline(w http.ResponseWriter, r *http.Request, rootID int64) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	showCompleted := showCompletedFrom(r)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	view := outlineView{
		CSRFToken:     web.CSRFToken(r.Context()),
		ShowCompleted: showCompleted,
		Query:         query,
		SearchAction:  outlinePath(rootID),
	}

	// An empty title leaves the shell's breadcrumb reading "Home / ON Notes",
	// which is what the top level is. A zoomed outline names its root.
	title := ""
	if rootID != RootID {
		root, err := a.store.ByID(r.Context(), userID, rootID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		crumbs, err := a.store.Ancestors(r.Context(), userID, rootID)
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		view.Root, view.Zoomed, view.Crumbs = root, true, crumbs
		if root.Shared() {
			view.ShareURL = "/notes/s/" + root.ShareSlug
		}
		title = root.DisplayTitle()
	}

	dueRows, err := a.store.Due(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view.DueCount = DueBadgeCount(dueRows, time.Now())

	flat, err := a.store.Outline(r.Context(), userID, rootID, showCompleted, query != "")
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	visible := hideDone(flat, showCompleted)

	var matched map[int64]bool
	if query != "" {
		matchedNodes, err := a.store.Search(r.Context(), userID, query, showCompleted)
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		matched = idSet(matchedNodes)
	} else {
		view.HiddenCount = len(flat) - len(visible)
	}
	visible = filterToMatches(visible, matched)
	view.Rows = nest(visible, rootID, view.CSRFToken, time.Now().Format("2006-01-02"), matched, searchTerms(query))

	page := a.deps.Page(r, title)
	page.Data = view
	a.render(w, r, http.StatusOK, "notes/outline", page)
}

// renderOutlineFragment re-renders #outline's own content for an HTMX swap.
// It shares renderOutline's query but not its shell: a structural response
// never changes which node the page is zoomed to, so the breadcrumb and
// heading stay exactly as the browser already has them, and there is no
// need to look the root node up — Root.ID is all outline-body reads, and
// the caller already has it as a plain int64.
//
// A structural mutation, or a "show completed" toggle, reaches this too
// (mutateThen, prefs.go), always without a ?q= on its own request URL — so
// performing one while a filter is active resets it, deliberately: see this
// plan's own note on that scope boundary.
//
// The response also carries the toolbar's show-completed toggle out of band:
// that button lives outside #outline, so the swap cannot reach it, and after
// a prefs toggle its label and value would otherwise stay stale.
func (a *App) renderOutlineFragment(w http.ResponseWriter, r *http.Request, userID, rootID int64, showCompleted bool) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	flat, err := a.store.Outline(r.Context(), userID, rootID, showCompleted, query != "")
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	dueRows, err := a.store.Due(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	visible := hideDone(flat, showCompleted)

	view := outlineView{
		CSRFToken:     web.CSRFToken(r.Context()),
		Root:          Node{ID: rootID},
		ShowCompleted: showCompleted,
		Query:         query,
		DueCount:      DueBadgeCount(dueRows, time.Now()),
		OOB:           true,
	}

	var matched map[int64]bool
	if query != "" {
		matchedNodes, err := a.store.Search(r.Context(), userID, query, showCompleted)
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		matched = idSet(matchedNodes)
	} else {
		view.HiddenCount = len(flat) - len(visible)
	}
	visible = filterToMatches(visible, matched)
	view.Rows = nest(visible, rootID, view.CSRFToken, time.Now().Format("2006-01-02"), matched, searchTerms(query))
	if err := a.deps.Render.Fragment(w, http.StatusOK, "notes/outline", "outline-swap", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
```

- [ ] **Step 6: Update `outline.html`**

Change the search box call site (currently `{{template "notes-search-box" (dict "Query" "" "Autofocus" false)}}`) to:

```html
{{template "notes-search-box" (dict "Query" .Data.Query "Autofocus" false "Action" .Data.SearchAction "Target" "#outline")}}
```

Change the `outline-body` block from:

```html
{{define "outline-body"}}
{{if .Rows}}
{{template "outline-rows" .Rows}}
{{else if .HiddenCount}}
{{template "outline-all-done" .}}
{{else}}
{{template "outline-first" .}}
{{end}}
{{end}}
```

to:

```html
{{define "outline-body"}}
{{if .Query}}
{{if .Rows}}
{{template "outline-rows" .Rows}}
{{else}}
<p class="dim">No notes match &ldquo;{{.Query}}&rdquo;.</p>
{{end}}
{{else if .Rows}}
{{template "outline-rows" .Rows}}
{{else if .HiddenCount}}
{{template "outline-all-done" .}}
{{else}}
{{template "outline-first" .}}
{{end}}
{{end}}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, including every pre-existing test (`TestOutlineToolbarHasASearchBox` still passes: `#notes-search-input`'s `name`/`form action`/`method`/`role` are unchanged, only new `hx-*` attributes were added).

- [ ] **Step 8: Commit**

```bash
git add internal/apps/notes/handlers.go internal/apps/notes/view.go internal/apps/notes/templates/toolbar.partial.html internal/apps/notes/templates/outline.html internal/apps/notes/handlers_test.go
git commit -m "feat(notes): filter the outline in place as you type"
```

---

## Task 6: Retarget the `#tag`/`@mention` link

**Files:**
- Modify: `internal/apps/notes/markdown.go:171-194`
- Modify: `internal/apps/notes/markdown_test.go`
- Modify: `internal/apps/notes/handlers_test.go:3479`

**Interfaces:**
- Consumes: nothing new — `writeTag` already exists; only its target URL and doc comments change.

- [ ] **Step 1: Update the failing assertions first**

In `internal/apps/notes/markdown_test.go`, at lines 140, 147, and 170, change each expected href from `/notes/search?q=...` to `/notes/?q=...`, e.g.:

```go
	if !strings.Contains(got, `<a class="outline-tag" href="/notes/?q=%23urgent">#urgent</a>`) {
```

```go
	if !strings.Contains(got, `<a class="outline-tag" href="/notes/?q=%40alice">@alice</a>`) {
```

```go
	if !strings.Contains(got, `<a class="outline-tag" href="/notes/?q=%23tag">#tag</a>.`) {
```

In `internal/apps/notes/handlers_test.go:3479`, change:

```go
	if href, _ := htmlassert.Attr(a, "href"); !strings.HasPrefix(href, "/notes/search?q=") {
		t.Errorf("outline-tag href = %q, want a /notes/search?q= link", href)
	}
```

to:

```go
	if href, _ := htmlassert.Attr(a, "href"); !strings.HasPrefix(href, "/notes/?q=") {
		t.Errorf("outline-tag href = %q, want a /notes/?q= link", href)
	}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/apps/notes/... -run 'TestRender|TestOutlineTagsStillLink' -v`
Expected: FAIL — `writeTag` still points at `/notes/search`.

- [ ] **Step 3: Update `writeTag` and its doc comments**

In `internal/apps/notes/markdown.go`, change `writeTag`'s body and comment:

```go
// writeTag renders a #tag or @mention as a chip — spec §10, §13. There is
// no tags table: this is a rendering and linking behaviour of the Markdown
// renderer alone. When linkTags is true (Render, the ordinary authenticated
// outline) the chip links to a literal filter for that exact string, the
// top-level outline with ?q= set — spec: search as an inline filter,
// replacing the old dedicated /notes/search results page. When linkTags is
// false (RenderShared, the public share page) the chip keeps its
// "outline-tag" styling but renders as an inert <span>, not a link —
// spec §15's "no link leads anywhere into the owner's private tree", which
// /notes/ would violate for an anonymous visitor even though it only
// dead-ends them at a login wall rather than exposing private data.
func writeTag(b *strings.Builder, tag string, linkTags bool) {
	if !linkTags {
		b.WriteString(`<span class="outline-tag">`)
		b.WriteString(html.EscapeString(tag))
		b.WriteString(`</span>`)
		return
	}
	b.WriteString(`<a class="outline-tag" href="/notes/?q=`)
	b.WriteString(url.QueryEscape(tag))
	b.WriteString(`">`)
	b.WriteString(html.EscapeString(tag))
	b.WriteString(`</a>`)
}
```

Update `RenderShared`'s doc comment two references to `/notes/search` to `/notes/` (same file, just above `RenderShared`):

```go
// RenderShared is Render for the public share page (spec §15: "no link
// leads anywhere into the owner's private tree"). #tag/@mention chips
// still render — they keep their visual styling — but as an inert
// <span class="outline-tag">, not an <a href="/notes/?q=...">: /notes/ is
// an authenticated route, so a link there from a page an anonymous
// visitor can reach is a dead end into a login wall, not the private tree
// itself, but it still violates the no-link invariant. Used only by
// share.go's nestShared and handlers.go's viewShared — every other caller
// renders the ordinary, authenticated outline and should keep using Render
// so tags there stay real links into the filtered outline.
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS. `TestTagChipNowResolves` needs no edit — it reads the chip's own rendered `href` rather than hardcoding it, and follows it directly.

- [ ] **Step 5: Update the stale prose comment in `share.go` (optional but keeps the codebase accurate)**

In `internal/apps/notes/share.go:209`, change the comment's `/notes/search` mention to `/notes/`.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/notes/markdown.go internal/apps/notes/markdown_test.go internal/apps/notes/handlers_test.go internal/apps/notes/share.go
git commit -m "fix(notes): point #tag/@mention chips at the new filtered outline"
```

---

## Task 7: Wire the Due page

**Files:**
- Modify: `internal/apps/notes/due.go`
- Modify: `internal/apps/notes/handlers.go:115,152,618-644`
- Modify: `internal/apps/notes/templates/due.html`
- Test: `internal/apps/notes/due_test.go`, `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `matchedIDsSubquery`, `ftsQuery`, `searchTerms` (Task 1); `highlightPlainText`, `noteSnippet` (Task 2).
- Produces: `Store.Due(ctx, userID int64, query string) ([]Node, error)` (query param added); `DueRow.TitleHTML template.HTML`, `DueRow.Snippet template.HTML` (new fields); `dueView` (new wrapper struct with `Groups DueGroups`, `Query string`, `SearchAction string`); `App.buildDueView`, `App.renderDueFragment` (new methods).

- [ ] **Step 1: Write the failing store-level test**

Add to `internal/apps/notes/due_test.go`:

```go
func TestDueWithAQueryOnlyReturnsMatchingRows(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	match := f.mk(t, notes.RootID, "buy milk")
	if err := f.store.SetDue(ctx, f.alice.ID, match.ID, "2026-01-01"); err != nil {
		t.Fatal(err)
	}
	other := f.mk(t, notes.RootID, "call dentist")
	if err := f.store.SetDue(ctx, f.alice.ID, other.ID, "2026-01-01"); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Due(ctx, f.alice.ID, "milk")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != match.ID {
		t.Fatalf("Due(query=milk) = %+v, want just the milk bullet", got)
	}
}

func TestDueWithNoQueryReturnsEverything(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "anything")
	if err := f.store.SetDue(ctx, f.alice.ID, n.ID, "2026-01-01"); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Due(ctx, f.alice.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("Due(query=\"\") = %+v, want the one due bullet", got)
	}
}
```

Check `Store`'s real method name for setting a due date (likely `SetDue`, in `due.go` or `tree.go`) before writing this — adjust if it differs.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/apps/notes/... -run TestDueWith -v`
Expected: FAIL to compile — `Store.Due` does not yet accept a second argument.

- [ ] **Step 3: Add the query parameter to `Store.Due`**

In `internal/apps/notes/due.go`, change `Due`:

```go
// Due returns every one of userID's nodes with a due date set, excluding
// done ones and archived ones — spec §11's "done and archived nodes are
// excluded". A node that sits under an archived ancestor is excluded too,
// via archivedBelowCTE (store.go), shared with Store.Search — spec §13's
// subtree rule applies here exactly as it does there. Ordered by due_on so
// GroupByDue only has to bucket, never sort.
//
// query, when non-empty, additionally requires a title/note match — the
// filter behind /notes/due's own search box. "" matches every row, the same
// as calling this before that feature existed.
func (st *Store) Due(ctx context.Context, userID int64, query string) ([]Node, error) {
	matchClause := ""
	args := []any{userID, userID, userID}
	if q := ftsQuery(query); q != "" {
		matchClause = " AND " + matchedIDsSubquery
		args = append(args, q)
	}
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE `+archivedBelowCTE+`
		 SELECT `+nodeColumns+`
		   FROM notes_nodes
		  WHERE user_id = ? AND due_on IS NOT NULL AND done_at IS NULL
		    AND id NOT IN (SELECT id FROM archived_below)`+matchClause+`
		  ORDER BY due_on`, args...)
	if err != nil {
		return nil, fmt.Errorf("notes: due nodes: %w", err)
	}
	return collectNodes(rows, "due nodes")
}
```

- [ ] **Step 4: Update every existing call site of `Store.Due`**

Append `, ""` to every call currently reading `f.store.Due(ctx, f.alice.ID)` in `internal/apps/notes/due_test.go` (lines 21, 41, 60, 81, 140, 163).

Leave `internal/apps/notes/handlers.go:115,152,625` alone for now — Step 8 below rewrites all three properly.

- [ ] **Step 5: Run the store-level tests to verify they pass**

Run: `go test ./internal/apps/notes/... -run TestDue -v`
Expected: PASS. (The package as a whole will not build yet, since `handlers.go` still calls the three-argument-less `Due` in two of its three call sites — that's fine, Step 8 fixes it.)

- [ ] **Step 6: Add `DueRow.TitleHTML`/`.Snippet` and the `dueView` wrapper**

In `internal/apps/notes/due.go`, change `DueRow` and add `dueView`:

```go
// DueRow is one entry in /notes/due: a node plus its ancestor breadcrumb,
// outermost first — spec §11 says each hit shows its ancestor path so a
// result three levels deep is legible on its own. Overdue is set by
// GroupByDue, once, rather than computed in the template — the same reason
// outlineRow.Overdue exists.
//
// TitleHTML is DisplayTitle run through highlightPlainText — escaped, and
// with any filter match wrapped in <mark>, but never through Render: the
// title is a link (see DisplayTitleHTML's own doc comment on why that
// stays plain), so this only ever adds <mark>, never anything Render could
// produce. It equals plain escaped DisplayTitle when there is no filter.
// Snippet is a highlighted excerpt of Note, set only when the filter
// matched there and not in the title — issue #86's "which field matched".
type DueRow struct {
	Node
	Crumbs    []Node
	Overdue   bool
	TitleHTML template.HTML
	Snippet   template.HTML
}

// dueView is what /notes/due renders — Groups wraps GroupByDue's own
// buckets so the template can also read Query/SearchAction, which DueGroups
// itself has no reason to carry.
type dueView struct {
	Groups       DueGroups
	Query        string
	SearchAction string
}
```

Add `"html/template"` to `internal/apps/notes/due.go`'s imports.

- [ ] **Step 7: Run `go build` to confirm the new fields compile**

Run: `go build ./internal/apps/notes/...`
Expected: still fails — `handlers.go` needs the rewrite below before anything using `Due`/`DueRow` compiles again.

- [ ] **Step 8: Rewrite the Due handler**

In `internal/apps/notes/handlers.go`:

1. Change the two toolbar-badge call sites (`renderOutline`/`renderOutlineFragment`, both currently `a.store.Due(r.Context(), userID)`) to `a.store.Due(r.Context(), userID, "")` — the badge always counts everything, regardless of any filter in the URL.

2. Replace `dueList` entirely, and add `buildDueView`/`renderDueFragment` beside it:

```go
// dueList renders every one of the user's due bullets, grouped by urgency —
// spec §11. The toolbar's search box drives its own live filter through
// this same route over HTMX, the same GET-branches-on-HX-Request shape
// renderOutlineOrFragment uses.
func (a *App) dueList(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	if web.IsHTMX(r) {
		a.renderDueFragment(w, r, userID)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	view, err := a.buildDueView(r.Context(), userID, query)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	page := a.deps.Page(r, "Due")
	page.Data = view
	a.render(w, r, http.StatusOK, "notes/due", page)
}

// renderDueFragment re-renders #due-list's own content for an HTMX swap —
// the equivalent of renderOutlineFragment/renderArchiveFragment.
func (a *App) renderDueFragment(w http.ResponseWriter, r *http.Request, userID int64) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	view, err := a.buildDueView(r.Context(), userID, query)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	if err := a.deps.Render.Fragment(w, http.StatusOK, "notes/due", "due-list", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// buildDueView is the query behind both of the above.
func (a *App) buildDueView(ctx context.Context, userID int64, query string) (dueView, error) {
	nodes, err := a.store.Due(ctx, userID, query)
	if err != nil {
		return dueView{}, err
	}
	crumbs, err := a.store.AncestorsMany(ctx, userID, idsOf(nodes))
	if err != nil {
		return dueView{}, err
	}

	terms := searchTerms(query)
	rows := make([]DueRow, len(nodes))
	for i, n := range nodes {
		rows[i] = DueRow{
			Node:      n,
			Crumbs:    crumbs[n.ID],
			TitleHTML: highlightPlainText(n.DisplayTitle(), terms),
			Snippet:   noteOnlySnippet(n, terms),
		}
	}
	return dueView{Groups: GroupByDue(rows, time.Now()), Query: query, SearchAction: "/notes/due"}, nil
}

// noteOnlySnippet is Due/Archive's issue #86 indicator: a highlighted
// excerpt of a row's note, shown only when the filter matched there and not
// in the title (a title match is already visible via TitleHTML above).
func noteOnlySnippet(n Node, terms []string) template.HTML {
	if len(terms) == 0 || len(matchSpans(n.Title, terms)) > 0 {
		return ""
	}
	return noteSnippet(n.Note, terms)
}
```

3. Add `"html/template"` to `handlers.go`'s imports if not already present (check first — `render.Page` etc. may already require it indirectly, but the import itself must be explicit).

- [ ] **Step 9: Update `due.html`**

Replace the whole file:

```html
{{define "content"}}
<div class="stack notes">
	{{/* /notes/due's own listing excludes done bullets unconditionally —
	     spec §11 — so, unlike the outline, this toolbar needs no
	     show-completed toggle and no .notes-toolbar-actions cluster. */}}
	<div class="notes-toolbar">
		{{template "notes-search-box" (dict "Query" .Data.Query "Autofocus" false "Action" .Data.SearchAction "Target" "#due-list")}}
		<div class="notes-toolbar-actions">
			<a href="/notes/archive" class="toolbar-btn toolbar-btn-nav"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M21 8H3l2-4h14z"/><path d="M5 8v11h14V8"/><path d="M10 12h4"/></svg>Archive</a>
			<a href="/notes/" class="toolbar-btn toolbar-btn-nav"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M15 6l-6 6 6 6"/></svg>All notes</a>
		</div>
	</div>
	<h1>Due</h1>
	{{/* #due-list is this feature's own swap target, the equivalent of
	     #outline and #archive-list: its content is a named block so an
	     HTMX filter re-render can replace just this, through
	     Renderer.Fragment, without touching the toolbar above it. */}}
	<div id="due-list">
		{{template "due-list" .Data}}
	</div>
</div>
{{end}}

{{define "due-list"}}
{{$empty := not (or .Groups.Overdue .Groups.Today .Groups.ThisWeek .Groups.Later)}}
{{if and .Query $empty}}
<p class="dim">No notes match &ldquo;{{.Query}}&rdquo;.</p>
{{else}}
{{range .Groups.Sections}}
{{if .Rows}}
<section class="notes-due-group">
	<h2>{{.Title}}</h2>
	<ul class="notes-due-list">
		{{range .Rows}}
		<li class="notes-due-item">
			{{if .Crumbs}}
			{{/* Not links — see Node.DisplayTitleHTML's doc comment
			     (issue #76) — so rendering Markdown here can't nest an
			     <a>. The row's own title just below stays on a
			     Markdown-free highlighted form: it IS a link, so a title
			     containing a link/autolink/#tag of its own would nest
			     anchors. */}}
			<span class="notes-due-crumbs">
				{{range .Crumbs}}<span class="notes-crumb-item" title="{{.DisplayTitle}}">{{.DisplayTitleHTML}}</span> / {{end}}
			</span>
			{{end}}
			<a href="/notes/{{.ID}}" class="notes-due-title">{{.TitleHTML}}</a>
			{{with .Snippet}}<p class="notes-search-snippet dim">{{.}}</p>{{end}}
			{{/* Issue #78: same non-colour signal as the outline's own
			     chip (outline.html's rendered-title) — the "Overdue"
			     section heading above also carries the meaning here, but
			     a chip read on its own (e.g. by a screen reader jumping
			     straight to it) still needs its own mark. */}}
			<span class="outline-due-chip{{if .Overdue}} outline-due-overdue{{end}}"{{if .Overdue}} aria-label="Overdue: {{.DueOn}}"{{end}}>{{if .Overdue}}&#33; {{end}}{{.DueOn}}</span>
		</li>
		{{end}}
	</ul>
</section>
{{end}}
{{end}}
{{if and (not .Query) $empty}}
<p class="dim">Nothing is due.</p>
{{end}}
{{end}}
{{end}}
```

- [ ] **Step 10: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, including `TestDueListGroupsAcrossTheWholeTree`, `TestDueListWithNothingDue`, and every other pre-existing Due test (`.Data.Sections`/`.Data.Overdue` etc. in the old template become `.Data.Groups.Sections`/`.Data.Groups.Overdue`, which is what the rewritten template above already does — no test changes should be required, since none of the existing tests reach into `.Data` at the Go level, only through rendered HTML).

- [ ] **Step 11: Add tests for the new filter and highlighting behaviour**

Add to `internal/apps/notes/handlers_test.go`:

```go
func TestDueFilterHighlightsATitleMatch(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "buy milk")
	s.Submit(t, s.Alice, "/notes/"+itoa(id)+"/due", url.Values{"root": {"0"}, "due": {"2026-01-01"}}, "/notes/")

	doc := s.Get(t, s.Alice, "/notes/due?q=milk")
	mark := doc.MustHave("mark.notes-search-hit")
	if got := htmlassert.Text(mark); !strings.EqualFold(got, "milk") {
		t.Errorf("highlighted text = %q, want milk", got)
	}
}

func TestDueFilterExcludesNonMatchingRows(t *testing.T) {
	s := newServer(t)
	match := s.seed(t, s.Alice, notes.RootID, "buy milk")
	s.Submit(t, s.Alice, "/notes/"+itoa(match)+"/due", url.Values{"root": {"0"}, "due": {"2026-01-01"}}, "/notes/")
	other := s.seed(t, s.Alice, notes.RootID, "call dentist")
	s.Submit(t, s.Alice, "/notes/"+itoa(other)+"/due", url.Values{"root": {"0"}, "due": {"2026-01-01"}}, "/notes/")

	doc := s.Get(t, s.Alice, "/notes/due?q=milk")
	if strings.Contains(doc.Text(), "call dentist") {
		t.Error("a non-matching row leaked into the filtered Due list")
	}
}

func TestDueFilterShowsASnippetForANoteOnlyMatch(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "groceries")
	s.Submit(t, s.Alice, "/notes/"+itoa(id)+"/due", url.Values{"root": {"0"}, "due": {"2026-01-01"}}, "/notes/")
	if err := s.Store.SetText(context.Background(), s.Alice.User.ID, id, "groceries", "don't forget the oat milk"); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/notes/due?q=milk")
	doc.MustHave(".notes-search-snippet mark.notes-search-hit")
}

func TestDueFilterWithNoMatchesSaysSo(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/notes/due?q=nonexistent")
	if !strings.Contains(doc.Text(), "No notes match") {
		t.Error("an empty filtered Due result shows no feedback")
	}
}
```

Check the exact route/field names for setting a due date via HTTP (`POST /notes/{id}/due`, field `due`) against `handlers.go`'s existing `due` handler before relying on this — adjust the `url.Values` if they differ.

- [ ] **Step 12: Run them to verify they pass**

Run: `go test ./internal/apps/notes/... -run TestDueFilter -v`
Expected: PASS.

- [ ] **Step 13: Commit**

```bash
git add internal/apps/notes/due.go internal/apps/notes/due_test.go internal/apps/notes/handlers.go internal/apps/notes/templates/due.html internal/apps/notes/handlers_test.go
git commit -m "feat(notes): filter the due list in place, with a note-match snippet"
```

---

## Task 8: Wire the Archive page

**Files:**
- Modify: `internal/apps/notes/archive.go`
- Modify: `internal/apps/notes/handlers.go:648-696,709-744`
- Modify: `internal/apps/notes/templates/archive.html`
- Test: `internal/apps/notes/archive_test.go`, `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: same as Task 7 (`matchedIDsSubquery`, `highlightPlainText`, `noteSnippet`, `noteOnlySnippet` from Task 7).
- Produces: `Store.Archive(ctx, userID int64, query string) ([]Node, error)` (query param added); `ArchiveRow.TitleHTML`, `ArchiveRow.Snippet`; `archiveView.Query`, `archiveView.SearchAction`; `App.buildArchiveView` and `App.archiveList`/`App.renderArchiveFragment` gain the query/HTMX branch.

- [ ] **Step 1: Write the failing store-level test**

Add to `internal/apps/notes/archive_test.go`:

```go
func TestArchiveWithAQueryOnlyReturnsMatchingRows(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	match := f.mk(t, notes.RootID, "old milk carton")
	if err := f.store.SetArchived(ctx, f.alice.ID, match.ID, true); err != nil {
		t.Fatal(err)
	}
	other := f.mk(t, notes.RootID, "old receipts")
	if err := f.store.SetArchived(ctx, f.alice.ID, other.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Archive(ctx, f.alice.ID, "milk")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != match.ID {
		t.Fatalf("Archive(query=milk) = %+v, want just the milk bullet", got)
	}
}

func TestArchiveWithNoQueryReturnsEverything(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "anything")
	if err := f.store.SetArchived(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Archive(ctx, f.alice.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("Archive(query=\"\") = %+v, want the one archived bullet", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/apps/notes/... -run TestArchiveWith -v`
Expected: FAIL to compile — `Store.Archive` does not yet accept a second argument.

- [ ] **Step 3: Add the query parameter to `Store.Archive`**

In `internal/apps/notes/archive.go`, change `Archive`:

```go
// query, when non-empty, additionally requires a title/note match — the
// filter behind /notes/archive's own search box. "" matches every row, the
// same as calling this before that feature existed. Unlike Store.Search,
// this never excludes archived content — these rows are archived by
// definition — so it cannot reuse Search itself, only ftsQuery/
// matchedIDsSubquery, the pieces that don't carry that exclusion.
func (st *Store) Archive(ctx context.Context, userID int64, query string) ([]Node, error) {
	matchClause := ""
	args := []any{userID, userID, userID}
	if q := ftsQuery(query); q != "" {
		matchClause = " AND " + matchedIDsSubquery
		args = append(args, q)
	}
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE `+archivedBelowCTE+`
		 SELECT `+aliasNodeColumns("n")+`
		   FROM notes_nodes n
		  WHERE n.user_id = ? AND n.archived_at IS NOT NULL
		    AND (n.parent_id IS NULL OR n.parent_id NOT IN (SELECT id FROM archived_below))`+matchClause+`
		  ORDER BY julianday(n.archived_at) DESC`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("notes: archive: %w", err)
	}
	return collectNodes(rows, "archived nodes")
}
```

(Leave the rest of the method's existing doc comment above this in place — only the signature/body and the new paragraph shown here change.)

- [ ] **Step 4: Update every existing call site of `Store.Archive`**

Append `, ""` to every call currently reading `f.store.Archive(ctx, f.alice.ID)` or `f.store.Archive(context.Background(), f.alice.ID)` in `internal/apps/notes/archive_test.go` (lines 20, 45, 67, 97, 129, 146, 159).

Leave `internal/apps/notes/handlers.go:683` alone for now — Step 6 rewrites it.

- [ ] **Step 5: Run the store-level tests to verify they pass**

Run: `go test ./internal/apps/notes/... -run TestArchive -v`
Expected: PASS. (Package build is still broken until Step 6 — expected.)

- [ ] **Step 6: Add `ArchiveRow.TitleHTML`/`.Snippet`, `archiveView.Query`/`.SearchAction`, and rewrite the handlers**

In `internal/apps/notes/archive.go`, change `ArchiveRow` and `archiveView`:

```go
// ArchiveRow is one entry on /notes/archive: an archived subtree's root,
// plus its ancestor breadcrumb — the same shape Search and Due rows take,
// for the same reason: a node found outside the context of the tree it
// lives in needs its path spelled out to be legible on its own.
//
// TitleHTML/Snippet are DueRow's own fields, for the same reason — see
// DueRow's doc comment (due.go).
type ArchiveRow struct {
	Node
	Crumbs    []Node
	TitleHTML template.HTML
	Snippet   template.HTML
}

// archiveView is what /notes/archive renders.
type archiveView struct {
	Rows []ArchiveRow
	// CSRFToken is needed here, unlike DueGroups, because this page's rows
	// carry a real mutating form — Restore — and Due's are read-only.
	CSRFToken    string
	Query        string
	SearchAction string
}
```

Add `"html/template"` to `internal/apps/notes/archive.go`'s imports.

In `internal/apps/notes/handlers.go`, replace `archiveList`, `renderArchiveFragment`, and `buildArchiveView`:

```go
// archiveList renders every one of the user's archived subtree roots —
// spec §13. The toolbar's search box drives its own live filter through
// this same route over HTMX, the same shape dueList/renderOutlineOrFragment
// use.
func (a *App) archiveList(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	if web.IsHTMX(r) {
		a.renderArchiveFragment(w, r, userID)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	view, err := a.buildArchiveView(r.Context(), userID, query)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view.CSRFToken = web.CSRFToken(r.Context())
	page := a.deps.Page(r, "Archive")
	page.Data = view
	a.render(w, r, http.StatusOK, "notes/archive", page)
}

// renderArchiveFragment re-renders /notes/archive's own list for an HTMX
// restore, or an HTMX filter — the equivalent of renderOutlineFragment, but
// targeting this page's own swap target instead of #outline. A restore's
// own POST never carries a ?q= of its own, so — like every structural
// mutation on the outline — performing one resets an active filter; see
// this plan's note on that scope boundary.
func (a *App) renderArchiveFragment(w http.ResponseWriter, r *http.Request, userID int64) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	view, err := a.buildArchiveView(r.Context(), userID, query)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view.CSRFToken = web.CSRFToken(r.Context())
	if err := a.deps.Render.Fragment(w, http.StatusOK, "notes/archive", "archive-list", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// buildArchiveView is the query behind both of the above: the full page and
// the HTMX fragment render exactly the same rows, so they share the one
// place that fetches them.
func (a *App) buildArchiveView(ctx context.Context, userID int64, query string) (archiveView, error) {
	nodes, err := a.store.Archive(ctx, userID, query)
	if err != nil {
		return archiveView{}, err
	}
	crumbs, err := a.store.AncestorsMany(ctx, userID, idsOf(nodes))
	if err != nil {
		return archiveView{}, err
	}

	terms := searchTerms(query)
	rows := make([]ArchiveRow, len(nodes))
	for i, n := range nodes {
		rows[i] = ArchiveRow{
			Node:      n,
			Crumbs:    crumbs[n.ID],
			TitleHTML: highlightPlainText(n.DisplayTitle(), terms),
			Snippet:   noteOnlySnippet(n, terms),
		}
	}
	return archiveView{Rows: rows, Query: query, SearchAction: "/notes/archive"}, nil
}
```

- [ ] **Step 7: Update `archive.html`**

Change the search box call site to:

```html
{{template "notes-search-box" (dict "Query" .Data.Query "Autofocus" false "Action" .Data.SearchAction "Target" "#archive-list")}}
```

Change the row's title link and the empty-state branch inside the `archive-list` define:

```html
			<a href="/notes/{{.ID}}" class="notes-archive-title">{{.TitleHTML}}</a>
			{{with .Snippet}}<p class="notes-search-snippet dim">{{.}}</p>{{end}}
```

```html
{{else if .Query}}
<p class="dim">No notes match &ldquo;{{.Query}}&rdquo;.</p>
{{else}}
<p class="dim">Nothing is archived.</p>
{{end}}
```

(replacing the existing bare `{{else}}<p class="dim">Nothing is archived.</p>{{end}}`.)

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, including every pre-existing Archive test — none of them read `.Data` at the Go level, only through rendered HTML, so the `archiveView`/`ArchiveRow` field additions and `Archive`'s new parameter should need no further test changes beyond Step 4's mechanical ones.

- [ ] **Step 9: Add tests for the new filter and highlighting behaviour**

Add to `internal/apps/notes/handlers_test.go`:

```go
func TestArchiveFilterHighlightsATitleMatch(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "old milk carton")
	s.Post(t, s.Alice, "/notes/"+itoa(id)+"/archive", url.Values{"root": {"0"}, "focus_id": {"0"}, "archived": {"1"}})

	doc := s.Get(t, s.Alice, "/notes/archive?q=milk")
	mark := doc.MustHave("mark.notes-search-hit")
	if got := htmlassert.Text(mark); !strings.EqualFold(got, "milk") {
		t.Errorf("highlighted text = %q, want milk", got)
	}
}

func TestArchiveFilterExcludesNonMatchingRows(t *testing.T) {
	s := newServer(t)
	match := s.seed(t, s.Alice, notes.RootID, "old milk carton")
	s.Post(t, s.Alice, "/notes/"+itoa(match)+"/archive", url.Values{"root": {"0"}, "focus_id": {"0"}, "archived": {"1"}})
	other := s.seed(t, s.Alice, notes.RootID, "old receipts")
	s.Post(t, s.Alice, "/notes/"+itoa(other)+"/archive", url.Values{"root": {"0"}, "focus_id": {"0"}, "archived": {"1"}})

	doc := s.Get(t, s.Alice, "/notes/archive?q=milk")
	if strings.Contains(doc.Text(), "old receipts") {
		t.Error("a non-matching row leaked into the filtered Archive list")
	}
}

func TestArchiveFilterWithNoMatchesSaysSo(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/notes/archive?q=nonexistent")
	if !strings.Contains(doc.Text(), "No notes match") {
		t.Error("an empty filtered Archive result shows no feedback")
	}
}
```

Check the exact `mutate`/archive route's expected form fields (`root`, `focus_id`, `archived`) against `handlers.go`'s existing `archive` handler and `mutateThen` before relying on this — adjust if they differ (see `TestArchiveHidesTheBulletAndRedirectsToTheOutline` in `handlers_test.go` for a known-working example of this exact POST).

- [ ] **Step 10: Run them to verify they pass**

Run: `go test ./internal/apps/notes/... -run TestArchiveFilter -v`
Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/apps/notes/archive.go internal/apps/notes/archive_test.go internal/apps/notes/handlers.go internal/apps/notes/templates/archive.html internal/apps/notes/handlers_test.go
git commit -m "feat(notes): filter the archive list in place, with a note-match snippet"
```

---

## Task 9: Remove `/notes/search`

**Files:**
- Modify: `internal/apps/notes/app.go:101,121`
- Modify: `internal/apps/notes/handlers.go` (remove `search`, simplify `prefsRedirectTarget`)
- Delete: `internal/apps/notes/templates/search.html`
- Delete: `internal/apps/notes/search.go`'s `SearchRow`/`searchView` (no longer rendered by anything)
- Modify: `internal/apps/notes/handlers_test.go` (migrate every `/notes/search`-based test onto the pages that now own that behaviour)

**Interfaces:**
- Consumes: everything from Tasks 1–8 — this task only removes now-dead code and moves test coverage, it adds nothing new.

- [ ] **Step 1: Remove the route and handler**

In `internal/apps/notes/app.go`, delete the `r.HandleFunc("GET /search", a.search)` line (and update the route-map comment above `Mount` that lists it, removing the `GET /notes/search likewise a literal segment; N6's search` line).

In `internal/apps/notes/handlers.go`, delete the whole `search` method (its doc comment through its closing brace — currently the block starting `// search runs spec §12's full-text search...`).

- [ ] **Step 2: Simplify `prefsRedirectTarget`**

`/notes/search` was the only reason this function branched on a `page` field. In `internal/apps/notes/handlers.go`, replace:

```go
// prefsRedirectTarget is where a non-HTMX prefs toggle sends the browser
// back to. The outline's own toggle is HTMX (handled above); /notes/search's
// plain-form toggle (issue #88) is not, since that page does no partial
// swapping of its own, so it needs a real redirect back to itself — with
// its query string preserved, or the toggle would silently reset the search.
//
// page is a closed enum read from a hidden field, not an arbitrary URL:
// a forged value can only ever select one of these known-safe destinations,
// never something open-redirect-shaped.
func prefsRedirectTarget(r *http.Request, root int64) string {
	switch r.PostFormValue("page") {
	case "search":
		q := r.PostFormValue("q")
		if q == "" {
			return "/notes/search"
		}
		return "/notes/search?q=" + url.QueryEscape(q)
	default:
		return outlinePath(root)
	}
}
```

with:

```go
// prefsRedirectTarget is where a non-HTMX prefs toggle sends the browser
// back to: the zoom the request came from. (Until /notes/search was
// removed, this also special-cased a redirect back to that page with its
// own query string preserved — issue #88 — since every other prefs toggle
// on this app is HTMX. There is no longer a non-HTMX page whose own toggle
// needs anywhere else to go.)
func prefsRedirectTarget(root int64) string {
	return outlinePath(root)
}
```

Update its one call site in the `prefs` handler from `prefsRedirectTarget(r, root)` to `prefsRedirectTarget(root)`.

Remove the `"net/url"` import from `handlers.go` if nothing else in the file still uses `url.QueryEscape`/`url.Values` etc. — check with `grep -n '\burl\.' internal/apps/notes/handlers.go` first.

- [ ] **Step 3: Delete the template**

```bash
rm internal/apps/notes/templates/search.html
```

- [ ] **Step 4: Remove `SearchRow`/`searchView`**

In `internal/apps/notes/search.go`, delete the `SearchRow` and `searchView` type declarations (nothing renders them anymore — `Store.Search` itself stays, still used by the Outline handler from Task 5).

- [ ] **Step 5: Migrate the old `/notes/search`-based tests**

In `internal/apps/notes/handlers_test.go`, the following tests hit `/notes/search` directly and must be rewritten to exercise the page that now owns that behaviour (mostly the Outline filter from Task 5, since that is where `Store.Search` itself is still used):

- `TestPrefsRedirectsBackToSearchWithItsQuery` (around line 2086): this test exercised the now-deleted `page=search` branch of `prefsRedirectTarget`. Delete it — there is no longer a non-HTMX page with its own query-preserving prefs toggle to test.
- `TestSearchHasAShowCompletedToggle` (around line 2113): the Outline page's own `#show-completed-toggle` already has full coverage elsewhere (`TestOutlinePageToggleIsNotOutOfBand`, `TestPrefsRespondsWithTheFreshValueOverHTMX`, etc.) and this scenario — a completed match hidden until the toggle is on — is Outline-specific behaviour now; delete this test rather than trying to preserve a `page=search` hidden field that no longer exists anywhere.
- `TestSearchFindsABulletAndShowsItsBreadcrumb` (around line 2599): rewrite to hit the outline filter:

  ```go
  func TestOutlineFilterFindsABulletAndShowsItsBreadcrumb(t *testing.T) {
  	s := newServer(t)
  	parent := s.seed(t, s.Alice, notes.RootID, "Projects")
  	child := s.seed(t, s.Alice, parent, "Budget report")

  	doc := s.Get(t, s.Alice, "/notes/?q=budget")
  	if !strings.Contains(doc.Text(), "Projects") {
  		t.Error("the hit's ancestor is missing")
  	}
  	link := doc.MustHave(`a.outline-dot[href=/notes/` + itoa(child) + `]`)
  	_ = link
  }
  ```

  (The outline's own row is not a single `<a>` wrapping the title the way Search's was — check `outline-rows`'s actual markup in `outline.html` and adjust the assertion to something that actually exists there, e.g. asserting the rendered title text appears via `doc.Text()` plus the row's `data-id` attribute, rather than assuming a specific selector. `TestOutlineFilterKeepsAMatchAndItsAncestorOnly` from Task 5 already covers this scenario more directly — consider deleting this rewritten test as redundant with that one instead of keeping both.)

- `TestSearchCrumbsRenderMarkdownButRowTitleStaysPlain` (around line 2622): this scenario (crumb spans render Markdown, the row's own title stays a link) is already true of the outline's breadcrumb elsewhere (`outline.html`'s `.outline-crumbs`) and is unrelated to filtering specifically — delete it; it duplicates coverage that already exists for the outline's own zoom breadcrumb.
- `TestSearchBoxPrefillsTheQueryAndAutofocuses` (around line 2644): delete — no page autofocuses its search box anymore (the design's approved behaviour: `Autofocus` stays `false` everywhere, since there is no dedicated landing page for search).
- `TestSearchWithNoQueryShowsNoResults` (around line 2655): delete — superseded by ordinary Outline rendering tests (an empty `?q=` behaves exactly like no `?q=` at all, already implied by every non-filter Outline test passing).
- `TestSearchWithWhitespaceOnlyQueryShowsNoResults` (around line 2668): rewrite as an Outline test:

  ```go
  func TestOutlineFilterWithWhitespaceOnlyQueryShowsNoFeedback(t *testing.T) {
  	s := newServer(t)
  	s.seed(t, s.Alice, notes.RootID, "anything")

  	doc := s.Get(t, s.Alice, "/notes/?q=%20%20")
  	if strings.Contains(doc.Text(), "No notes match") {
  		t.Error("a whitespace-only query rendered the no-matches message")
  	}
  }
  ```

- `TestSearchWithNoMatchesSaysSo` (around line 2679): delete — superseded by `TestOutlineFilterWithNoMatchesSaysSo` (Task 5).
- `TestSearchDoesNotRenderAnotherUsersNodes` (around line 2687): rewrite:

  ```go
  func TestOutlineFilterDoesNotRenderAnotherUsersNodes(t *testing.T) {
  	s := newServer(t)
  	s.seed(t, s.Bob, notes.RootID, "bob's secret plan")

  	doc := s.Get(t, s.Alice, "/notes/?q=secret")
  	if strings.Contains(doc.Text(), "secret") {
  		t.Error("alice's filtered outline shows bob's node")
  	}
  }
  ```

- `TestSearchRequiresSignIn` (around line 2695): delete — `TestNotesRequiresSignIn` already covers every GET route in this app including `/notes/`, and `/notes/search` no longer exists to require anything.
- `TestOutlineToolbarHasASearchBox` (around line 2726): keep, but its `action`-equals-`/notes/search` assertion no longer applies — remove that one assertion (the `form.notes-search`'s `action` is now `/notes/`, already covered by the new `TestOutlineSearchBoxTargetsOutlineOverHTMX` from Task 5); keep the rest (`name=q`, `method=get`, `role=search`) since those are unchanged.
- `TestDueToolbarHasASearchBox`, `TestArchiveToolbarHasASearchBox` (around lines 2745, 2771): keep as-is — both still just assert `#notes-search-input` exists on those pages, which remains true.

Run `grep -n '"/notes/search' internal/apps/notes/handlers_test.go` after this step to confirm nothing was missed.

- [ ] **Step 6: Run the full test suite**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS. `go vet ./internal/apps/notes/...` should also report nothing.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/notes/app.go internal/apps/notes/handlers.go internal/apps/notes/search.go internal/apps/notes/handlers_test.go
git rm internal/apps/notes/templates/search.html
git commit -m "refactor(notes): remove /notes/search, superseded by the inline filter"
```

---

## Task 10: Styling

**Files:**
- Modify: `internal/ui/static/app.css`

**Interfaces:** none — CSS only.

- [ ] **Step 1: Add the highlight and snippet styles**

In `internal/ui/static/app.css`, near the existing `.notes-search-*` rules (around line 1144), add:

```css
/* A match's highlight — issue #86 — uses the palette's own tinted-
 * background token (the same one active/selected chips already use, e.g.
 * .admin-tag-flag) rather than the browser's default yellow mark, so it
 * reads as "this app's own accent", not a generic <mark>. */
mark.notes-search-hit {
	background: var(--c-accent-bg);
	color: inherit;
	border-radius: 2px;
	padding: 0 0.15em;
}

.notes-search-snippet {
	margin: 0.25em 0 0;
	font-size: var(--fs-sm);
}
```

- [ ] **Step 2: Manually verify in a browser**

Since this is a pure CSS change with no automated visual test in this codebase's suite, start the app locally (check `AGENTS.md`/root `README.md` for the run command, typically `go run ./cmd/onsuite`), sign in, create a bullet, and type a matching filter into the outline's search box — confirm the match renders with a visibly tinted (not browser-default yellow) highlight in both light and dark theme (toggle via the app's own theme switcher).

- [ ] **Step 3: Commit**

```bash
git add internal/ui/static/app.css
git commit -m "style(notes): tint search-match highlights with the app's own palette"
```

---

## Task 11: Full verification

**Files:** none — this task only runs checks.

- [ ] **Step 1: Build the whole module**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 2: Vet the whole module**

Run: `go vet ./...`
Expected: no findings.

- [ ] **Step 3: Run the full test suite**

Run: `go test ./... -count=1`
Expected: every package PASSes, including `internal/arch` (which enforces the app-boundary and test-only-package rules this plan's use of `golang.org/x/net/html` from production code — `highlight.go` — must not violate; if `internal/arch`'s tests fail here, read what they actually assert before assuming this plan is wrong, since a production package importing `golang.org/x/net/html` for the first time is exactly the kind of change that boundary test exists to catch or clear).

- [ ] **Step 4: Confirm no stray references to the removed page remain**

Run: `grep -rn "notes/search" internal/apps/notes internal/ui 2>/dev/null`
Expected: no output (aside from anything intentionally left in this feature's own design doc/plan, which live under `docs/`, not `internal/`).

- [ ] **Step 5: Manual smoke test**

Start the app locally, and for each of Outline, Due, and Archive:
1. Create at least one bullet whose title matches a test query and one whose note (not title) matches.
2. Type the query into that page's search box and confirm: the list narrows live (no page reload), the match is highlighted, a note-only match shows its snippet (Due/Archive) or its highlighted note text (Outline), and clearing the box restores the full list.
3. On Outline specifically: collapse a bullet that has a matching descendant, then filter for that descendant's text, and confirm the descendant appears (collapse is bypassed while filtering) with its ancestor shown as plain context above it.
4. Reload the page with the filter's `?q=` still in the URL and confirm it renders the same filtered result without JavaScript-driven re-fetching (i.e. the plain-GET fallback works).

- [ ] **Step 6: No commit for this task** — it is verification only. If any step above surfaces a real defect, fix it as part of the task where it belongs (re-open that task's own commit history with a follow-up commit) rather than bundling an unrelated fix into this verification pass.
