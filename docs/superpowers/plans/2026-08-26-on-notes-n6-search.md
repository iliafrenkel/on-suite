# ON Notes N6 — Search and Tags Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Full-text search over every bullet's title and note, at `/notes/search?q=`, each hit showing its ancestor breadcrumb — the same route N4's `#tag`/`@mention` chips have linked to since they were built, which have 404ed until now.

**Architecture:** One FTS5 external-content virtual table (`notes_fts`), kept in sync by insert/update/delete triggers on `notes_nodes`, queried through one new `Store.Search` method. A small escaping helper turns free text into an FTS5 query that can never be a syntax error, regardless of what the user types. The search box itself is a plain `GET` form — no JS, no CSRF token needed — duplicated across the outline, due, and search pages so it's always reachable; `notes.js` adds exactly one binding, `/` to focus it.

**Tech Stack:** SQLite FTS5 (verified present in `modernc.org/sqlite v1.56.0`, already in `go.mod` — no new dependency), Go (`database/sql`, `strings`).

## Global Constraints

- No CGO, no new Go dependencies, no Node/npm/JS build step. FTS5 needs no library — it is compiled into the SQLite build already vendored.
- Migrations are forward-only: the FTS5 table and its triggers arrive in their own numbered migration.
- CSP: `script-src 'self'` with no `unsafe-inline`; `internal/ui/static/app.css` is the only stylesheet, and "apps must not introduce new colors" — use existing `--c-*` tokens only.
- `main` is branch-protected: work on a branch, open a PR, never push directly.
- The local `staticcheck` binary can silently exit 0 while printing internal-error noise; always run `go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...` instead of the bare `staticcheck` on `$PATH`.
- `internal/htmlassert` supports only descendant combinators and one qualifier per selector part.
- Every store operation takes `userID` and folds it into its `WHERE` clause — another user's node is *not found*, never *forbidden* (spec §5).
- Store tests run against a real SQLite file in a temp dir, per the existing `newFixture` convention in `store_test.go` — not `:memory:`.
- **Tags are not stored** (spec §12): `#tag`/`@mention` are a Markdown-rendering behaviour (N4) that links to a literal search for that exact string. This chunk answers that link; it adds no tags table and no tag-parsing logic of its own.
- **Search results honour the show-completed preference and exclude archived nodes** (spec §12) — but `archived_at` does not exist until N7. This chunk excludes done bullets (unless show-completed is on, mirroring N5's `/notes/due`); excluding archived ones is a follow-up for whichever of N6/N7 lands second. This is not a judgment call — the column simply doesn't exist yet.
- `Store` has no HTTP knowledge (its own package doc, unchanged by this chunk): `Store.Search` takes `showCompleted bool` as a plain argument, the same way `Store.Due`'s callers already decide that outside the store.

## File Structure

- Create: `internal/apps/notes/migrations/0003_search.sql` — the FTS5 table, its backfill, and its three triggers.
- Create: `internal/apps/notes/search.go` — `ftsQuery`, `Store.Search`, `SearchRow`, `searchView`.
- Create: `internal/apps/notes/search_test.go`.
- Create: `internal/apps/notes/templates/search.html`.
- Modify: `internal/apps/notes/handlers.go` — the `search` handler.
- Modify: `internal/apps/notes/app.go` — one new route.
- Modify: `internal/apps/notes/templates/outline.html`, `internal/apps/notes/templates/due.html` — a search box in each page's toolbar.
- Modify: `internal/ui/static/app.css` — the toolbar restructure, the search box, and the search results list.
- Modify: `internal/apps/notes/static/notes.js` — `/` focuses search.
- Modify: `internal/apps/notes/handlers_test.go`.
- Modify: `README.md`, `internal/apps/notes/notes.go` (package doc).

---

### Task 1: FTS5 schema and `Store.Search`

**Files:**
- Create: `internal/apps/notes/migrations/0003_search.sql`
- Create: `internal/apps/notes/search.go`
- Create: `internal/apps/notes/search_test.go`

**Interfaces:**
- Produces: `ftsQuery(q string) string`; `SearchRow{Node; Crumbs []Node}`; `(*Store).Search(ctx, userID int64, query string, showCompleted bool) ([]Node, error)`.

Pure store layer — no HTTP, no templates — with its own complete TDD cycle, exactly like N1's and N5's `due.go`.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/notes/search_test.go`:

```go
package notes_test

import (
	"context"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

func TestSearchFindsAMatchInTheTitle(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "buy milk")
	f.mk(t, notes.RootID, "call dentist")

	got, err := f.store.Search(ctx, f.alice.ID, "milk", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "buy milk" {
		t.Fatalf("Search(milk) = %+v, want just the milk bullet", got)
	}
}

func TestSearchFindsAMatchInTheNote(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "groceries")
	if err := f.store.SetText(ctx, f.alice.ID, n.ID, "groceries", "don't forget the oat milk"); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Search(ctx, f.alice.ID, "oat", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != n.ID {
		t.Fatalf("Search(oat) = %+v, want the groceries bullet", got)
	}
}

// TestSearchRequiresEveryWord: space-separated words are ANDed, not ORed —
// ftsQuery's job is to make that true regardless of what the words are.
func TestSearchRequiresEveryWord(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "budget report")
	f.mk(t, notes.RootID, "budget only")

	got, err := f.store.Search(ctx, f.alice.ID, "budget report", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "budget report" {
		t.Fatalf("Search(budget report) = %+v, want just the one with both words", got)
	}
}

// TestSearchTracksEdits proves the FTS5 triggers actually fire, not just
// that the initial backfill worked: a stale index would still find the old
// text, or miss the new.
func TestSearchTracksEdits(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "old title")

	if got, err := f.store.Search(ctx, f.alice.ID, "old", false); err != nil || len(got) != 1 {
		t.Fatalf("Search(old) before edit = %+v, %v", got, err)
	}

	if err := f.store.SetText(ctx, f.alice.ID, n.ID, "new title", ""); err != nil {
		t.Fatal(err)
	}

	if got, err := f.store.Search(ctx, f.alice.ID, "old", false); err != nil || len(got) != 0 {
		t.Fatalf("Search(old) after edit = %+v, %v; the index is stale", got, err)
	}
	if got, err := f.store.Search(ctx, f.alice.ID, "new", false); err != nil || len(got) != 1 {
		t.Fatalf("Search(new) after edit = %+v, %v", got, err)
	}
}

// TestSearchTracksDeletes proves the AD trigger removes a deleted bullet
// from the index itself, not merely that Search happens to filter it out
// some other way.
func TestSearchTracksDeletes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "temporary")

	if err := f.store.Delete(ctx, f.alice.ID, n.ID); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Search(ctx, f.alice.ID, "temporary", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Search after delete = %+v, want none", got)
	}
}

func TestSearchDoesNotLeakAnotherUsersNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "alice only")
	f.mkFor(t, f.bob.ID, notes.RootID, "bob's secret")

	got, err := f.store.Search(ctx, f.alice.ID, "secret", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("alice's search found bob's node: %+v", got)
	}
}

func TestSearchExcludesDoneUnlessShowCompleted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "finished task")
	if err := f.store.SetDone(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}

	if got, err := f.store.Search(ctx, f.alice.ID, "finished", false); err != nil || len(got) != 0 {
		t.Fatalf("Search with showCompleted=false = %+v, %v; want none", got, err)
	}
	if got, err := f.store.Search(ctx, f.alice.ID, "finished", true); err != nil || len(got) != 1 {
		t.Fatalf("Search with showCompleted=true = %+v, %v; want the one hit", got, err)
	}
}

func TestSearchWithNoQueryReturnsNoResults(t *testing.T) {
	f := newFixture(t)
	f.mk(t, notes.RootID, "anything")

	got, err := f.store.Search(context.Background(), f.alice.ID, "   ", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Search(whitespace) = %+v, want none", got)
	}
}

// TestSearchHandlesFTS5SyntaxCharactersLiterally proves ftsQuery's escaping:
// none of these characters carry their usual FTS5 meaning here, and none of
// them should make Search return an error.
func TestSearchHandlesFTS5SyntaxCharactersLiterally(t *testing.T) {
	f := newFixture(t)
	f.mk(t, notes.RootID, `a "quoted" word`)

	for _, q := range []string{`"quoted"`, `budget* OR report`, `foo" bar`, `NOT`} {
		if _, err := f.store.Search(context.Background(), f.alice.ID, q, false); err != nil {
			t.Errorf("Search(%q) errored: %v", q, err)
		}
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/notes/... -run TestSearch -v`
Expected: FAIL — `Store.Search` does not exist, and the migration hasn't created `notes_fts`.

- [ ] **Step 3: Add the migration**

Create `internal/apps/notes/migrations/0003_search.sql`:

```sql
-- N6: full-text search over title and note — spec §12. FTS5 is verified
-- present in modernc.org/sqlite v1.56.0, already in go.mod, so this adds no
-- dependency.
CREATE VIRTUAL TABLE notes_fts USING fts5(
    title, note, content='notes_nodes', content_rowid='id', tokenize='unicode61'
);

-- External-content FTS5 tables are not backfilled automatically: every
-- bullet that already existed before this migration needs one explicit
-- insert, or it stays unsearchable until its next edit.
INSERT INTO notes_fts(rowid, title, note) SELECT id, title, note FROM notes_nodes;

-- Keeps notes_fts in step with notes_nodes from here on. The 'delete'
-- special command (INSERT INTO notes_fts(notes_fts, rowid, title, note)
-- VALUES ('delete', ...)) is FTS5's own documented way to remove a row from
-- an external-content index — a plain DELETE FROM notes_fts is not
-- supported the same way for a content= table.
CREATE TRIGGER notes_fts_ai AFTER INSERT ON notes_nodes BEGIN
    INSERT INTO notes_fts(rowid, title, note) VALUES (new.id, new.title, new.note);
END;

CREATE TRIGGER notes_fts_ad AFTER DELETE ON notes_nodes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, title, note) VALUES ('delete', old.id, old.title, old.note);
END;

CREATE TRIGGER notes_fts_au AFTER UPDATE ON notes_nodes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, title, note) VALUES ('delete', old.id, old.title, old.note);
    INSERT INTO notes_fts(rowid, title, note) VALUES (new.id, new.title, new.note);
END;
```

- [ ] **Step 4: Add `search.go`**

Create `internal/apps/notes/search.go`:

```go
package notes

import (
	"context"
	"fmt"
	"strings"
)

// SearchRow is one hit in /notes/search: a node plus its ancestor
// breadcrumb, outermost first — spec §12: "each hit renders as the
// matching bullet plus its ancestor breadcrumb".
type SearchRow struct {
	Node
	Crumbs []Node
}

// searchView is what /notes/search renders.
type searchView struct {
	Query string
	Rows  []SearchRow
}

// ftsQuery turns free text into an FTS5 MATCH expression that can never be
// a syntax error. Each word becomes its own quoted phrase — doubling any
// embedded '"' the way FTS5's string literals require — so a user typing an
// operator FTS5 would otherwise interpret (AND, OR, NOT, *, :, an
// unbalanced quote) always searches for that literal text instead of
// breaking the query. Space-separated quoted phrases are ANDed by FTS5's
// own default, so a multi-word search requires every word to appear
// somewhere in the bullet, not necessarily adjacent to the others.
func ftsQuery(q string) string {
	words := strings.Fields(q)
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
	}
	return strings.Join(quoted, " ")
}

// Search runs a full-text search over title and note across userID's whole
// tree — spec §12. An empty query (nothing left after ftsQuery) returns no
// rows rather than asking FTS5 to MATCH an empty string, which is a syntax
// error of its own. Results are ordered by FTS5's own relevance rank, and,
// like Store.Due, honour showCompleted: a done bullet that matches is
// excluded unless the preference is on. Archived nodes are not excluded —
// archived_at does not exist until N7 (see this plan's Global Constraints).
func (st *Store) Search(ctx context.Context, userID int64, query string, showCompleted bool) ([]Node, error) {
	q := ftsQuery(query)
	if q == "" {
		return nil, nil
	}
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+aliasNodeColumns("n")+`
		   FROM notes_fts f
		   JOIN notes_nodes n ON n.id = f.rowid
		  WHERE f MATCH ? AND n.user_id = ? AND (? OR n.done_at IS NULL)
		  ORDER BY f.rank`,
		q, userID, showCompleted)
	if err != nil {
		return nil, fmt.Errorf("notes: search: %w", err)
	}
	return collectNodes(rows, "search results")
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, all of it — old and new.

- [ ] **Step 6: Full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/notes/migrations/0003_search.sql internal/apps/notes/search.go internal/apps/notes/search_test.go
git commit -m "notes: add FTS5 search over title and note"
```

---

### Task 2: `/notes/search`

**Files:**
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/app.go`
- Create: `internal/apps/notes/templates/search.html`
- Modify: `internal/ui/static/app.css`
- Modify: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `(*Store).Search`, `SearchRow`, `searchView` (Task 1); `showCompletedFrom` (N5, `prefs.go`); `Store.Ancestors` (N1).
- Produces: `(*App).search`, mounted at `GET /notes/search`.

At the end of this task, `/notes/search?q=...` works end to end reached by a direct URL — including every `#tag`/`@mention` chip N4 already rendered. The toolbar UI that reaches it from other pages is Task 3's.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/notes/handlers_test.go`:

```go
func TestSearchFindsABulletAndShowsItsBreadcrumb(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.alice, notes.RootID, "Projects")
	child := s.seed(t, s.alice, parent, "AtBudget report")

	doc := s.get(t, s.alice, "/notes/search?q=budget")
	if !strings.Contains(doc.Text(), "Projects") {
		t.Error("the hit's ancestor breadcrumb is missing")
	}
	link := doc.MustHave(`a[href=/notes/` + itoa(child) + `]`)
	if got := htmlassert.Text(link); got != "AtBudget report" {
		t.Errorf("search hit link text = %q", got)
	}
}

func TestSearchWithNoQueryShowsNoResults(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "anything")

	doc := s.get(t, s.alice, "/notes/search")
	doc.MustNotHave(".notes-search-item")
}

func TestSearchWithNoMatchesSaysSo(t *testing.T) {
	s := newServer(t)
	doc := s.get(t, s.alice, "/notes/search?q=nonexistent")
	if !strings.Contains(doc.Text(), "No matches") {
		t.Error("an empty result set shows no feedback")
	}
}

func TestSearchDoesNotRenderAnotherUsersNodes(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.bob, notes.RootID, "bob's secret plan")

	doc := s.get(t, s.alice, "/notes/search?q=secret")
	doc.MustNotHave(".notes-search-item")
}

func TestSearchRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.do(t, nil, httptest.NewRequest("GET", "/notes/search", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /notes/search anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

// TestTagChipNowResolves closes N4's own follow-up: the #tag chip has
// always linked to /notes/search?q=..., 404ing until this chunk existed to
// answer it.
func TestTagChipNowResolves(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "check #urgent today")

	doc := s.get(t, s.alice, "/notes/")
	chip := doc.MustHave(".outline-tag")
	href, _ := htmlassert.Attr(chip, "href")

	rec := s.do(t, s.alice, httptest.NewRequest("GET", href, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", href, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "check") {
		t.Error("following the tag chip does not find the bullet that produced it")
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/notes/... -run 'TestSearch|TagChipNowResolves' -v`
Expected: FAIL — `GET /notes/search` does not exist yet, and the tag chip still 404s.

- [ ] **Step 3: Add the handler and route**

Modify `internal/apps/notes/handlers.go`. Add `"strings"` is already imported; add after `dueList`:

```go
// search runs spec §12's full-text search across the whole tree. An empty
// query shows just the search box, with nothing to list — there is nothing
// sensible to prefill a fresh search with, unlike the outline's own empty
// bullet.
func (a *App) search(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	query := r.URL.Query().Get("q")

	var rows []SearchRow
	if strings.TrimSpace(query) != "" {
		nodes, err := a.store.Search(r.Context(), userID, query, showCompletedFrom(r))
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		rows = make([]SearchRow, len(nodes))
		for i, n := range nodes {
			crumbs, err := a.store.Ancestors(r.Context(), userID, n.ID)
			if err != nil {
				a.deps.Errors.Internal(w, r, err)
				return
			}
			rows[i] = SearchRow{Node: n, Crumbs: crumbs}
		}
	}

	page := a.deps.Page(r, "Search")
	page.Data = searchView{Query: query, Rows: rows}
	a.render(w, r, http.StatusOK, "notes/search", page)
}
```

Modify `internal/apps/notes/app.go`. Add a route to `Mount`, and a line to the route-map comment above it, alongside `GET /notes/due`:

```go
//	GET  /notes/search       likewise a literal segment; N6's search
```

```go
	r.HandleFunc("GET /search", a.search)
```

- [ ] **Step 4: Add `search.html`**

Create `internal/apps/notes/templates/search.html`:

```html
{{define "content"}}
<div class="stack notes">
	<div class="notes-toolbar">
		<form method="get" action="/notes/search" class="notes-search">
			<input type="search" id="notes-search-input" name="q" value="{{.Data.Query}}"
			       placeholder="Search…" aria-label="Search notes" autofocus>
		</form>
		<a href="/notes/">All notes</a>
	</div>
	<h1>Search</h1>
	{{if .Data.Query}}
	{{if .Data.Rows}}
	<ul class="notes-search-list">
		{{range .Data.Rows}}
		<li class="notes-search-item">
			{{if .Crumbs}}
			<span class="notes-search-crumbs">
				{{range .Crumbs}}{{.DisplayTitle}} / {{end}}
			</span>
			{{end}}
			<a href="/notes/{{.ID}}" class="notes-search-title">{{.DisplayTitle}}</a>
		</li>
		{{end}}
	</ul>
	{{else}}
	<p class="dim">No matches for &ldquo;{{.Data.Query}}&rdquo;.</p>
	{{end}}
	{{end}}
</div>
{{end}}
```

(`{{.Data.Query}}` inside the paragraph and the input's `value` are both auto-escaped by `html/template` — a query containing `<script>` or a literal `"` renders safely either way.)

- [ ] **Step 5: Add the CSS**

Modify `internal/ui/static/app.css`. Add after the due-list CSS (from N5) — this is deliberately named `notes-search-*`, not reusing `notes-due-*`, even though the two lists look identical: each chunk owns its own small CSS additions rather than reaching back into a previous chunk's file:

```css
.notes-search input[type="search"] {
	font: inherit;
	font-size: var(--fs-sm);
	color: var(--c-text);
	background: var(--c-bg-inset);
	border: none;
	border-radius: var(--radius);
	padding: var(--s-1) var(--s-2);
	width: 12rem;
}

.notes-search-list { margin: 0; padding: 0; list-style: none; }
.notes-search-item {
	display: flex;
	align-items: baseline;
	gap: var(--s-2);
	padding: var(--s-1) 0;
	border-bottom: 1px solid var(--c-border);
}
.notes-search-crumbs { font-size: var(--fs-sm); color: var(--c-text-faint); }
.notes-search-title { flex: 1; min-width: 0; color: var(--c-text); text-decoration: none; }
.notes-search-title:hover { color: var(--c-accent); }
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, all of it.

- [ ] **Step 7: Full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/notes/handlers.go internal/apps/notes/app.go internal/apps/notes/templates/search.html internal/ui/static/app.css internal/apps/notes/handlers_test.go
git commit -m "notes: add GET /notes/search"
```

---

### Task 3: Reaching search from every page

**Files:**
- Modify: `internal/apps/notes/templates/outline.html`
- Modify: `internal/apps/notes/templates/due.html`
- Modify: `internal/ui/static/app.css`
- Modify: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `GET /notes/search` (Task 2).
- Produces: an `<input id="notes-search-input">` present on the outline, due, and search pages — the exact id Task 4's keyboard binding looks up.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/notes/handlers_test.go`:

```go
func TestOutlineToolbarHasASearchBox(t *testing.T) {
	s := newServer(t)
	doc := s.get(t, s.alice, "/notes/")
	in := doc.MustHave("#notes-search-input")
	if got, _ := htmlassert.Attr(in, "name"); got != "q" {
		t.Errorf("search input name = %q, want q", got)
	}
	form := doc.MustHave("form.notes-search")
	if got, _ := htmlassert.Attr(form, "action"); got != "/notes/search" {
		t.Errorf("search form action = %q", got)
	}
	if got, _ := htmlassert.Attr(form, "method"); !strings.EqualFold(got, "get") {
		t.Errorf("search form method = %q, want get (no CSRF token needed)", got)
	}
}

func TestDueToolbarHasASearchBox(t *testing.T) {
	s := newServer(t)
	s.get(t, s.alice, "/notes/due").MustHave("#notes-search-input")
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/notes/... -run 'ToolbarHasASearchBox' -v`
Expected: FAIL — neither page has the search box yet.

- [ ] **Step 3: Restructure the outline's toolbar**

Modify `internal/apps/notes/templates/outline.html`. Replace:

```html
	<div class="notes-toolbar">
		<a href="/notes/due">Due</a>
		{{/* No class of its own: .notes-toolbar already lays this out, and
		     the form has nothing to style that the toolbar rule does not
		     cover. Its button is styled by .quiet, like every other
		     button in the outline. */}}
		<form method="post" action="/notes/prefs"
		      hx-post="/notes/prefs" hx-target="#outline" hx-swap="innerHTML">
			<input type="hidden" name="csrf_token" value="{{.Data.CSRFToken}}">
			<input type="hidden" name="root" value="{{.Data.Root.ID}}">
			{{template "show-completed-toggle" .Data}}
		</form>
	</div>
```

with:

```html
	<div class="notes-toolbar">
		<form method="get" action="/notes/search" class="notes-search">
			<input type="search" id="notes-search-input" name="q"
			       placeholder="Search…" aria-label="Search notes">
		</form>
		{{/* Grouped together so .notes-toolbar's own justify-content puts the
		     search box alone on the left and this cluster together on the
		     right, rather than spreading three items evenly. Neither form
		     needs a class of its own: .notes-toolbar-actions already lays
		     this out, and each button is styled by .quiet. */}}
		<div class="notes-toolbar-actions">
			<a href="/notes/due">Due</a>
			<form method="post" action="/notes/prefs"
			      hx-post="/notes/prefs" hx-target="#outline" hx-swap="innerHTML">
				<input type="hidden" name="csrf_token" value="{{.Data.CSRFToken}}">
				<input type="hidden" name="root" value="{{.Data.Root.ID}}">
				{{template "show-completed-toggle" .Data}}
			</form>
		</div>
	</div>
```

- [ ] **Step 4: Add the search box to `due.html`**

Modify `internal/apps/notes/templates/due.html`. Replace:

```html
{{define "content"}}
<div class="stack notes">
	<h1>Due</h1>
```

with:

```html
{{define "content"}}
<div class="stack notes">
	<div class="notes-toolbar">
		<form method="get" action="/notes/search" class="notes-search">
			<input type="search" id="notes-search-input" name="q"
			       placeholder="Search…" aria-label="Search notes">
		</form>
		<a href="/notes/">All notes</a>
	</div>
	<h1>Due</h1>
```

(`/notes/due`'s own listing already excludes done bullets unconditionally — spec §11 — so, unlike the outline, it needs no show-completed toggle of its own.)

- [ ] **Step 5: Update the toolbar CSS**

Modify `internal/ui/static/app.css`. Replace:

```css
.notes-toolbar {
	display: flex;
	justify-content: flex-end;
	/* The toolbar holds more than one control from N5 on — the Due link and
	 * the show-completed toggle — and they must not touch. */
	gap: var(--s-3);
	font-size: var(--fs-sm);
}
```

with:

```css
.notes-toolbar {
	display: flex;
	justify-content: space-between;
	align-items: center;
	gap: var(--s-3);
	font-size: var(--fs-sm);
}

/* Groups the Due link and the show-completed toggle on the right of the
 * outline's toolbar, so N6's search box can sit alone on the left. */
.notes-toolbar-actions {
	display: flex;
	align-items: center;
	gap: var(--s-3);
}
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, all of it.

- [ ] **Step 7: Full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/notes/templates/outline.html internal/apps/notes/templates/due.html internal/ui/static/app.css internal/apps/notes/handlers_test.go
git commit -m "notes: put a search box in the outline and due toolbars"
```

---

### Task 4: Keyboard — `/` focuses search

**Files:**
- Modify: `internal/apps/notes/static/notes.js`

**Interfaces:**
- Consumes: `#notes-search-input` (Task 3), `isOutlineField` (existing, N3).

**Scope decision:** `notes.js`'s whole keyboard module is guarded by `initKeyboard`'s existing check for `#outline` (`if (!document.getElementById("outline")) return;`), so it only ever runs on the outline page — never on `/notes/due` or `/notes/search`, which have no editable outline to bind Tab/Enter/arrows to in the first place. `/` therefore only focuses search when pressed from the outline page. Extending the keyboard module to also load on those other pages, just for this one binding, would need a second init path for no other reason — not worth it for what is a convenience, not a core requirement.

- [ ] **Step 1: Add the binding**

Modify `internal/apps/notes/static/notes.js`. In `handleKeydown`, add a check right after the `Escape` check and before the `isOutlineField` early return — `/` only takes over when the user is not already typing somewhere it would otherwise just insert a literal "/", which includes the search box itself:

```js
	function handleKeydown(e) {
		if (e.key === "Escape") {
			handleEscape();
			return;
		}
		if (e.key === "/" && !isOutlineField(e.target) && e.target.id !== "notes-search-input") {
			var search = document.getElementById("notes-search-input");
			if (search) {
				e.preventDefault();
				search.focus();
			}
			return;
		}

		var el = e.target;
		if (!isOutlineField(el)) return;
```

- [ ] **Step 2: Manual QA**

`notes.js` has no automated tests, by design (spec §17) — every request it issues is one a Go handler test already covers, and this binding issues no request at all. With `go run ./cmd/onsuite serve` running:

1. On the outline page, with nothing focused (click somewhere blank, or press `Escape` first), press `/`. Confirm the search box gains focus and the caret is in it.
2. Focus a bullet's title and type a sentence containing a literal `/` character. Confirm it types normally — editing a bullet must never be interrupted by this binding.
3. Click into the search box itself and type a query containing a `/` (e.g. "a/b"). Confirm the `/` types normally there too.
4. Submit a search, then from the results page press `/`. Confirm nothing happens (per this task's scope decision) — the search page has no `#outline`, so the whole keyboard module never initialises there.

- [ ] **Step 3: Commit**

```bash
git add internal/apps/notes/static/notes.js
git commit -m "notes: bind / to focus search"
```

---

### Task 5: Docs and final verification

**Files:**
- Modify: `README.md`
- Modify: `internal/apps/notes/notes.go`

- [ ] **Step 1: Update README.md**

Replace, in the intro paragraph:

> **ON Notes** is a hierarchical outliner — one infinite tree per account, with zoom, collapse, every structural operation, a full keyboard layer, inline Markdown (bold, links, `#tags`), and done/due tracking with a cross-tree due-date view. Search is still being built out.

with:

> **ON Notes** is a hierarchical outliner — one infinite tree per account, with zoom, collapse, every structural operation, a full keyboard layer, inline Markdown (bold, links, `#tags`), done/due tracking with a cross-tree due-date view, and full-text search with ancestor breadcrumbs.

Replace, in the Status section:

> Work since then is per-app rather than per-phase. ON Notes is being built in
> ten small chunks under
> [`docs/superpowers/plans/2026-08-25-on-notes-design.md`](docs/superpowers/specs/2026-08-25-on-notes-design.md);
> N1 (schema and store), N2 (the outline), N3 (the keyboard layer), N4
> (Markdown) and N5 (done + due dates) are done.

with:

> Work since then is per-app rather than per-phase. ON Notes is being built in
> ten small chunks under
> [`docs/superpowers/plans/2026-08-25-on-notes-design.md`](docs/superpowers/specs/2026-08-25-on-notes-design.md);
> N1 (schema and store), N2 (the outline), N3 (the keyboard layer), N4
> (Markdown), N5 (done + due dates) and N6 (search and tags) are done.

- [ ] **Step 2: Update the package doc comment**

Modify `internal/apps/notes/notes.go`:

```go
// The design is in docs/superpowers/specs/2026-08-25-on-notes-design.md. This
// package spans chunk N1 (schema and store) through N6 (search): app.App,
// routes, templates, handlers, the keyboard layer in static/notes.js, the
// inline Markdown renderer in markdown.go, task tracking in tree.go,
// prefs.go and due.go, and full-text search in search.go.
```

- [ ] **Step 3: Final full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 4: Manual browser pass**

With `go run ./cmd/onsuite serve` running: create a few bullets, including one with a `#tag`; search for a word that appears in a note but not a title, and confirm the hit and its breadcrumb; click the `#tag` chip from the outline and confirm it lands on the same search result; mark a bullet done and confirm it drops out of search until "show completed" is on; press `/` from the outline to confirm the keyboard binding.

- [ ] **Step 5: Commit**

```bash
git add README.md internal/apps/notes/notes.go
git commit -m "notes: mark N6 (search and tags) done"
```

---

## Self-review notes for the executing agent

- **`ftsQuery` quotes every word individually rather than quoting the whole query as one phrase.** Quoting the whole input would turn a two-word search into an exact-adjacent-phrase match ("budget report" would not match "budget: report" or "the report on budget"), which is a much narrower and less expected search experience than "every word appears somewhere" — the behaviour `TestSearchRequiresEveryWord` pins down. Per-word quoting still eliminates every FTS5 syntax-error risk, since each individual word becomes an opaque literal.
- **`Store.Search` takes `showCompleted` as a parameter, matching `renderOutlineFragment`'s established shape from N5 — it does not read `showCompletedFrom(r)` itself.** `Store` has no HTTP knowledge at all (not even indirectly, unlike `renderOutlineFragment`, which is in `handlers.go` and could read a cookie if it broke its own layering); the handler is the only place a cookie can be read, and it is passed down as a plain bool.
- **`/notes/due`'s toolbar gets no show-completed toggle.** This is not an oversight carried over from Task 3 — `Store.Due` already excludes done bullets unconditionally (spec §11's "done and archived nodes are excluded" for that specific view), so there is nothing for a toggle on that page to do.
- **The keyboard scope decision in Task 4 is deliberate, not a shortcut.** `/` only works from the outline page. Do not "fix" this by weakening `initKeyboard`'s `#outline` guard or adding a second keyboard-init path for `due.html`/`search.html` — neither page has anything else for the keyboard module to do, and the guard exists precisely so the module's other bindings (Tab, arrows, Enter, Backspace) never attach to a page with no outline to act on.
- **`notes-search-*` CSS classes are new, not a reuse of N5's `notes-due-*` ones**, even though the two lists render identically. Each chunk's plan has consistently kept its own CSS additions inside the file it's already touching rather than reaching back into an earlier chunk's; this one line of visual duplication is the smaller cost compared to coupling N6's list styling to N5's class names.

## Follow-ups to file as issues

1. `/notes/search` excludes done bullets but not archived ones — spec §12 asks for both, but `archived_at` does not exist until N7. Whichever of N6/N7 lands second should add `AND n.archived_at IS NULL` to `Store.Search`'s query (and the equivalent to `Store.Due`'s, per N5's own follow-up of the same shape).
2. Search results show only each hit's title (via `DisplayTitle`), never a highlighted snippet of the matching text or an indication of whether the match was in the title or the note. Spec §12 doesn't ask for either; worth a look if search result legibility turns out to matter more in practice than a bare title suggests.
3. `/` only focuses search from the outline page (see this plan's self-review notes and Task 4's scope decision). If `due.html`/`search.html` grow real keyboard needs of their own later, revisit whether `initKeyboard` should attach more broadly at that point — not before.
