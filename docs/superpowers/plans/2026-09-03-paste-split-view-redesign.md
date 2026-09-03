# ON Paste Split-View Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace ON Paste's separate list page and detail page with one page: a snippet list on the left and a view/edit pane on the right, navigated via HTMX partial swaps, with a unified toolbar matching ON Notes' style.

**Architecture:** One Go template (`internal/apps/paste/templates/index.html`) renders the whole page. `GET /paste/` and `GET /paste/{id}` render the same page, differing only in which snippet (if any) is selected. Every selection, edit, save, share, and delete happens via an `hx-get`/`hx-post` request targeting the `#detail` pane; each request still has a real `href`/`action` too, so the app keeps working with JavaScript disabled (server issues a full-page render or a redirect instead of a fragment). List rows that may have changed (title, preview, "shared" tag) ride along as an `hx-swap-oob` fragment, the same convention ON Notes already uses.

**Tech Stack:** Go `html/template`, htmx (already vendored), hand-drawn inline SVG icons, the existing `internal/platform/render` and `internal/platform/web` packages. No new dependencies.

## Global Constraints

- No JS framework, no build step: everything is server-rendered Go templates plus the already-vendored `htmx.min.js` and `theme.js`.
- Every HTMX-enhanced link/button keeps a working plain `href`/`action`/`method` so the app functions with JavaScript disabled (progressive enhancement, matching `internal/apps/notes`).
- Reuse existing CSS custom properties and the `.toolbar-btn`/`.toolbar-icon` classes from `internal/ui/static/app.css:898-970` rather than inventing new ones.
- Snippet bodies are arbitrary user text rendered through `html/template` — never bypass escaping (see `TestSnippetBodyIsEscapedInTheView` in `internal/apps/paste/handlers_test.go`).
- Deferred, do not build in this plan: search/filter on the list, a two-step "arm then confirm" delete.

---

### Task 1: `Store.Update`

Adds the one piece of storage logic that doesn't exist yet: saving edits to a snippet. Everything else in this plan is templates and handlers built on the existing `Store`.

**Files:**
- Modify: `internal/apps/paste/store.go`
- Test: `internal/apps/paste/store_test.go`

**Interfaces:**
- Produces: `func (st *Store) Update(ctx context.Context, userID, id int64, title, language, body string) (Snippet, error)` — validates like `Create`, returns `ErrNotFound` if the row doesn't exist or isn't `userID`'s, otherwise returns the updated `Snippet`. Leaves `ShareSlug` and `CreatedAt` untouched.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/paste/store_test.go` (after `TestCreateAndFetch`, matching its style):

```go
func TestUpdateChangesFields(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	created, err := f.store.Create(ctx, f.alice.ID, "Original", "go", "package main\n")
	if err != nil {
		t.Fatal(err)
	}

	updated, err := f.store.Update(ctx, f.alice.ID, created.ID, "  Renamed  ", "python", "print(1)\n")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Title != "Renamed" {
		t.Errorf("Title = %q, want trimmed \"Renamed\"", updated.Title)
	}
	if updated.Language != "python" {
		t.Errorf("Language = %q", updated.Language)
	}
	if updated.Body != "print(1)\n" {
		t.Errorf("Body = %q", updated.Body)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("CreatedAt changed: got %v, want %v", updated.CreatedAt, created.CreatedAt)
	}

	fetched, err := f.store.ByID(ctx, f.alice.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Title != "Renamed" || fetched.Body != "print(1)\n" {
		t.Error("the update was not actually persisted")
	}
}

func TestUpdatePreservesShareSlug(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	created, err := f.store.Create(ctx, f.alice.ID, "t", "go", "x\n")
	if err != nil {
		t.Fatal(err)
	}
	slug, err := f.store.Share(ctx, f.alice.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := f.store.Update(ctx, f.alice.ID, created.ID, "t2", "go", "y\n")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ShareSlug != slug {
		t.Errorf("ShareSlug = %q, want it preserved as %q", updated.ShareSlug, slug)
	}
}

func TestUpdateRejectsSomeoneElsesSnippet(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	created, err := f.store.Create(ctx, f.alice.ID, "alice's", "go", "secret\n")
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.store.Update(ctx, f.bob.ID, created.ID, "hijacked", "go", "x\n")
	if !errors.Is(err, paste.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}

	fetched, err := f.store.ByID(ctx, f.alice.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Title != "alice's" {
		t.Error("bob's update was applied to alice's snippet")
	}
}

func TestUpdateRejectsInvalidInput(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	created, err := f.store.Create(ctx, f.alice.ID, "t", "go", "x\n")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.Update(ctx, f.alice.ID, created.ID, "t", "go", "   \n"); !errors.Is(err, paste.ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid for an empty body", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/paste/... -run TestUpdate -v`
Expected: FAIL — `f.store.Update undefined (type *paste.Store has no field or method Update)`

- [ ] **Step 3: Implement `Store.Update`**

In `internal/apps/paste/store.go`, add this method directly after `Create` (after line 173):

```go
// Update overwrites userID's own snippet's editable fields: title, language,
// and body. It never touches ShareSlug or CreatedAt — sharing and editing are
// independent actions, and preserving the timestamp is what lets a snippet's
// "saved <time>" caption keep meaning the original save.
func (st *Store) Update(ctx context.Context, userID, id int64, title, language, body string) (Snippet, error) {
	title = strings.TrimSpace(title)
	if err := Validate(title, body); err != nil {
		return Snippet{}, err
	}

	res, err := st.db.ExecContext(ctx,
		`UPDATE paste_snippets SET title = ?, language = ?, body = ? WHERE id = ? AND user_id = ?`,
		title, language, body, id, userID)
	if err != nil {
		return Snippet{}, fmt.Errorf("paste: update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Snippet{}, fmt.Errorf("paste: update: %w", err)
	}
	if n == 0 {
		return Snippet{}, ErrNotFound
	}
	return st.ByID(ctx, userID, id)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/paste/... -run TestUpdate -v`
Expected: PASS (all four new tests)

- [ ] **Step 5: Commit**

```bash
git add internal/apps/paste/store.go internal/apps/paste/store_test.go
git commit -m "paste: add Store.Update for editing a saved snippet"
```

---

### Task 2: Split-view skeleton — list pane, view mode, routing

Replaces the separate list and view pages with one page and one handler. After this task, browsing snippets (selecting one from the list, seeing it in the pane, direct-linking to `/paste/{id}`) works end to end over HTMX and without it. Creating, editing, deleting, sharing still work exactly as today (full-page redirects) — those get their HTMX upgrades in later tasks.

**Files:**
- Create: `internal/apps/paste/templates/index.html`
- Delete: `internal/apps/paste/templates/list.html`, `internal/apps/paste/templates/view.html`
- Modify: `internal/apps/paste/handlers.go`
- Modify: `internal/apps/paste/paste.go:77,80`
- Modify: `internal/ui/static/app.css`
- Test: `internal/apps/paste/handlers_test.go`

**Interfaces:**
- Produces (used by Tasks 3–5):
  - `type detailView struct { Mode string; Snippet Snippet; Highlight template.HTML; Language string; ShareURL, RawURL, CSRFToken string; TitleValue, LanguageValue, BodyValue string; Languages []Language; Error string }`
  - `const modeView = "view"` (Tasks 3–4 add `modeNew`, `modeEdit`)
  - `type listFragment struct { Items []listItem; ActiveID int64; OOB bool }`
  - `type indexView struct { List listFragment; Detail detailView }`
  - `func (a *App) viewDetail(r *http.Request, s Snippet) detailView`
  - `func (a *App) listItems(ctx context.Context, userID int64) ([]listItem, error)`
  - `func (a *App) renderIndex(w http.ResponseWriter, r *http.Request, userID int64, status int, detail detailView)` — full page or, over HTMX, just the `detail-body` fragment.
  - Template blocks in `paste/index`: `content`, `list-items`, `detail-body`, `detail-view`, `detail-empty` (Tasks 3–4 add `detail-new`, `detail-edit`, and the combined `detail-with-list`).

- [ ] **Step 1: Write the failing tests**

These replace the list/view-specific tests in `internal/apps/paste/handlers_test.go`. Delete `TestNewFormRenders`'s neighbours are untouched; specifically **remove** nothing yet (Task 3 touches `TestNewFormRenders`) — just add the following new tests anywhere after `TestCreateThenView`:

```go
func TestIndexShowsListAndEmptyDetail(t *testing.T) {
	s := newServer(t)
	s.createSnippet(t, s.Alice, "First", "go", "package a\n")
	s.createSnippet(t, s.Alice, "Second", "python", "print(1)\n")

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	rows := doc.QueryAll(".snippet-row")
	if len(rows) != 2 {
		t.Fatalf("got %d snippet rows, want 2", len(rows))
	}
	if doc.Query(".snippet-row-active") != nil {
		t.Error("nothing is selected, but a row is marked active")
	}
	doc.MustHave(".paste-detail-empty")
}

func TestIndexSelectsSnippetAndMarksActiveRow(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "My config", "yaml", "key: value\n")
	s.createSnippet(t, s.Alice, "Other", "go", "package b\n")

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := htmlassert.Text(doc.MustHave("h1")); got != "My config" {
		t.Errorf("title = %q", got)
	}
	doc.MustHave(".chroma")

	active := doc.MustHave(".snippet-row-active")
	if !strings.Contains(htmlassert.Text(active), "My config") {
		t.Errorf("the active row is not the selected snippet: %q", htmlassert.Text(active))
	}
	// It is still one page: the list must be present alongside the detail.
	if len(doc.QueryAll(".snippet-row")) != 2 {
		t.Error("the list pane is missing on the detail page")
	}
}

// TestSelectingOverHTMXReturnsOnlyTheFragment: a fragment response must not
// repeat the shell (nav, sidebar) — only what belongs inside #detail.
func TestSelectingOverHTMXReturnsOnlyTheFragment(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "My config", "yaml", "key: value\n")

	req := httptest.NewRequest("GET", "/paste/"+itoa(id), nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "shell-bar") || strings.Contains(body, "app-sidebar") {
		t.Error("an HTMX fragment response repeated the page shell")
	}
	if !strings.Contains(body, "My config") {
		t.Error("the fragment does not contain the snippet")
	}
}

func TestIndexingSomeoneElsesSnippetIs404(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "alice's", "go", "secret\n")

	rec := s.Do(t, s.Bob, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Error("the snippet body leaked to another user")
	}
}
```

`TestViewRejectsNonNumericAndMissingIDs`, `TestSnippetBodyIsEscapedInTheView`, and `TestHighlightStylesheetIsServedAndCacheable` already exercise `GET /paste/{id}` and need no changes — they will now be routed through `index` instead of the old `view`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/paste/... -run 'TestIndex|TestSelectingOverHTMX' -v`
Expected: FAIL (compile error — `.snippet-row`, `.paste-detail-empty` etc. don't exist yet; `a.index` isn't wired up)

- [ ] **Step 3: Write `internal/apps/paste/templates/index.html`**

```html
{{define "head"}}
<link rel="stylesheet" href="/paste/highlight.css">
{{end}}

{{/* detail-body dispatches on the pane's mode. It is also the whole HTMX
     response for a plain selection (GET /paste/{id} or /paste/{id}/edit
     over HTMX): exactly what belongs inside #detail, nothing more. Tasks 3
     and 4 add the "new" and "edit" branches alongside their blocks. */}}
{{define "detail-body"}}
{{if eq .Mode "view"}}{{template "detail-view" .}}
{{else}}{{template "detail-empty" .}}
{{end}}
{{end}}

{{/* detail-with-list is the whole HTMX response for anything that can also
     change a row in the list beside it: creating, saving, sharing,
     unsharing, or deleting a snippet (Tasks 3-5). The list rides along out
     of band, the same convention internal/apps/notes uses for its toolbar
     toggle. */}}
{{define "detail-with-list"}}{{template "detail-body" .Detail}}{{template "list-items" .List}}{{end}}

{{define "detail-empty"}}
<p class="dim paste-detail-empty">Select a snippet to view it.</p>
{{end}}

{{define "detail-view"}}
<div class="stack" id="detail-view">
	<div class="notes-toolbar">
		<div>
			<h1>{{.Snippet.DisplayTitle}}</h1>
			<p class="faint">
				{{.Language}} · {{.Snippet.LinesLabel}} · saved
				<time datetime="{{.Snippet.CreatedAt.Format "2006-01-02T15:04:05Z07:00"}}" title="{{.Snippet.CreatedAt.Format "2 Jan 2006 15:04"}}">{{.Snippet.CreatedAt.Format "2 Jan 2006 15:04"}}</time>
			</p>
		</div>
		<div class="notes-toolbar-actions">
			<a class="toolbar-btn paste-back-btn" href="/paste/" hx-get="/paste/" hx-target="#detail" hx-push-url="true"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M19 12H5"/><path d="M12 19l-7-7 7-7"/></svg>Back</a>
			<a class="toolbar-btn" href="{{.RawURL}}"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M4 4h16v16H4z"/><path d="M8 9h8"/><path d="M8 13h8"/><path d="M8 17h5"/></svg>Raw</a>
			<button type="button" class="toolbar-btn" data-copy-raw="{{.RawURL}}"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><rect x="9" y="9" width="11" height="11" rx="1"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/></svg>Copy</button>
			{{if .Snippet.Shared}}
			<form method="post" action="/paste/{{.Snippet.ID}}/unshare">
				<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
				<button type="submit" class="toolbar-btn"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/><line x1="8.59" y1="13.51" x2="15.42" y2="17.49"/><line x1="15.41" y1="6.51" x2="8.59" y2="10.49"/></svg>Stop sharing</button>
			</form>
			{{else}}
			<form method="post" action="/paste/{{.Snippet.ID}}/share">
				<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
				<button type="submit" class="toolbar-btn"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/><line x1="8.59" y1="13.51" x2="15.42" y2="17.49"/><line x1="15.41" y1="6.51" x2="8.59" y2="10.49"/></svg>Share</button>
			</form>
			{{end}}
			<form method="post" action="/paste/{{.Snippet.ID}}/delete"
			      data-confirm="Delete “{{.Snippet.DisplayTitle}}”? This cannot be undone.">
				<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
				<button type="submit" class="toolbar-btn danger"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M3 6h18"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/></svg>Delete</button>
			</form>
		</div>
	</div>

	{{with .ShareURL}}
	<div class="notice row">
		<span>Anyone with this link can read this snippet: <a href="{{.}}">{{.}}</a></span>
		<button type="button" class="button" data-copy-link="{{.}}">Copy link</button>
	</div>
	{{end}}

	<div class="scroll-x snippet-body">{{.Highlight}}</div>
</div>
{{end}}

{{define "list-items"}}
<ul class="snippet-list" id="snippet-list"{{if .OOB}} hx-swap-oob="true"{{end}}>
	{{if .Items}}
	{{range .Items}}
	{{$s := .Snippet}}
	<li>
		<a class="snippet-row{{if eq $s.ID $.ActiveID}} snippet-row-active{{end}}" href="/paste/{{$s.ID}}" hx-get="/paste/{{$s.ID}}" hx-target="#detail" hx-push-url="true">
			<div class="row snippet-row-head">
				<span>{{$s.DisplayTitle}}</span>
				{{if $s.Shared}}<span class="tag" title="Shared with a public link">shared</span>{{end}}
			</div>
			<p class="snippet-preview">{{.Preview}}</p>
			<p class="snippet-meta">
				{{.Language}} · {{$s.LinesLabel}} ·
				<time datetime="{{$s.CreatedAt.Format "2006-01-02T15:04:05Z07:00"}}" title="{{$s.CreatedAt.Format "2 Jan 2006 15:04"}}">{{$s.CreatedAt.Format "2 Jan 2006 15:04"}}</time>
			</p>
		</a>
	</li>
	{{end}}
	{{else}}
	<li class="dim">No snippets yet.</li>
	{{end}}
</ul>
{{end}}

{{define "content"}}
<div class="paste-shell">
	{{/* A CSS-only mobile toggle: checked whenever a snippet (or a form) is
	     open, so app.css can show the detail pane in place of the list below
	     the narrow-viewport breakpoint without any JS. Task 3/4/5 responses
	     replace this element (its id, via hx-swap-oob) whenever Mode changes,
	     the same way they replace #snippet-list. */}}
	<input type="checkbox" id="paste-detail-open" class="visually-hidden"{{if .Data.Detail.Mode}} checked{{end}} aria-hidden="true" tabindex="-1">
	<div class="paste-list-pane">
		<div class="row list-head">
			<h1>Snippets</h1>
			<a class="toolbar-btn" href="/paste/new"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14"/><path d="M5 12h14"/></svg>New</a>
		</div>
		{{template "list-items" .Data.List}}
	</div>
	<div id="detail" class="paste-detail-pane">
		{{template "detail-body" .Data.Detail}}
	</div>
</div>
{{end}}
```

- [ ] **Step 4: Replace the list/view/index sections of `internal/apps/paste/handlers.go`**

Remove the `list` function (old lines 191–215) and the `view` function (old lines 102–128). In their place, add:

```go
// paneMode values for detailView.Mode. modeView is added here; Task 3 adds
// modeNew, Task 4 adds modeEdit.
const modeView = "view"

// detailView is what the detail pane renders, in any mode. Fields below
// Language belong to the edit/new forms (Tasks 3-4); they are zero-valued in
// view mode.
type detailView struct {
	Mode      string
	Snippet   Snippet
	Highlight template.HTML
	Language  string
	ShareURL  string
	RawURL    string
	CSRFToken string

	TitleValue    string
	LanguageValue string
	BodyValue     string
	Languages     []Language
	Error         string
}

// listFragment is what "list-items" renders. OOB marks a response that rides
// along with a detail-pane update rather than the initial page load.
type listFragment struct {
	Items    []listItem
	ActiveID int64
	OOB      bool
}

// indexView is the whole split-view page: the list plus whichever thing the
// detail pane is showing.
type indexView struct {
	List   listFragment
	Detail detailView
}

// viewDetail builds the detail pane's view-mode data for one snippet.
func (a *App) viewDetail(r *http.Request, s Snippet) detailView {
	return detailView{
		Mode:      modeView,
		Snippet:   s,
		Highlight: Highlight(s.Body, s.Language),
		Language:  LanguageLabel(s.Language),
		RawURL:    "/paste/raw/" + strconv.FormatInt(s.ID, 10),
		ShareURL:  shareURL(s),
		CSRFToken: web.CSRFToken(r.Context()),
	}
}

// listItems loads userID's snippets as the list pane's rows.
func (a *App) listItems(ctx context.Context, userID int64) ([]listItem, error) {
	snippets, err := a.store.List(ctx, userID, 0)
	if err != nil {
		return nil, err
	}
	items := make([]listItem, 0, len(snippets))
	for _, s := range snippets {
		items = append(items, listItem{
			Snippet:  s,
			Preview:  preview(s.Body, previewRunes),
			Language: LanguageLabel(s.Language),
		})
	}
	return items, nil
}

// pageTitle is the <title> for a full-page render of the index, which
// depends on what the detail pane is showing.
func pageTitle(d detailView) string {
	switch d.Mode {
	case modeView:
		return d.Snippet.DisplayTitle()
	default:
		return "Snippets"
	}
}

// index renders the split-view page: the list on the left, and whichever
// snippet {id} selects (or nothing) on the right. It backs both GET /{$}
// (PathValue("id") is "") and GET /{id}.
func (a *App) index(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	var detail detailView
	if r.PathValue("id") != "" {
		id, ok := a.snippetID(w, r)
		if !ok {
			return
		}
		s, err := a.store.ByID(r.Context(), userID, id)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		detail = a.viewDetail(r, s)
	}

	a.renderIndex(w, r, userID, http.StatusOK, detail)
}

// renderIndex draws the whole split-view page, or — over HTMX — just the
// detail pane. Every handler that lands the user on some detail pane
// (selecting a snippet, opening the new-snippet form, opening the editor, or
// failing validation on either form) goes through here.
func (a *App) renderIndex(w http.ResponseWriter, r *http.Request, userID int64, status int, detail detailView) {
	items, err := a.listItems(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view := indexView{
		List:   listFragment{Items: items, ActiveID: detail.Snippet.ID},
		Detail: detail,
	}

	if web.IsHTMX(r) {
		if err := a.deps.Render.Fragment(w, status, "paste/index", "detail-body", view.Detail); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}

	page := a.deps.Page(r, pageTitle(detail))
	page.Data = view
	a.render(w, r, status, "paste/index", page)
}

// renderDetailWithList replies for anything that can also change a row in
// the list: creating, saving an edit, sharing, unsharing, or deleting a
// snippet (Tasks 3-5). Callers only reach this over HTMX — a non-HTMX
// request redirects instead, before renderDetailWithList is ever called.
func (a *App) renderDetailWithList(w http.ResponseWriter, r *http.Request, userID int64, status int, detail detailView) {
	items, err := a.listItems(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view := indexView{
		List:   listFragment{Items: items, ActiveID: detail.Snippet.ID, OOB: true},
		Detail: detail,
	}
	if err := a.deps.Render.Fragment(w, status, "paste/index", "detail-with-list", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
```

Add `"context"` to the import block at the top of `handlers.go` (it is not yet imported there).

- [ ] **Step 5: Update the routes in `internal/apps/paste/paste.go`**

Change lines 77 and 80 from:

```go
	r.HandleFunc("GET /{$}", a.list)
	r.HandleFunc("GET /new", a.newForm)
	r.HandleFunc("POST /new", a.create)
	r.HandleFunc("GET /{id}", a.view)
```

to:

```go
	r.HandleFunc("GET /{$}", a.index)
	r.HandleFunc("GET /new", a.newForm)
	r.HandleFunc("POST /new", a.create)
	r.HandleFunc("GET /{id}", a.index)
```

- [ ] **Step 6: Delete the old templates**

```bash
rm internal/apps/paste/templates/list.html internal/apps/paste/templates/view.html
```

- [ ] **Step 7: Add the split-view CSS**

In `internal/ui/static/app.css`, directly after the existing `.snippet-meta { ... }` rule (the block ending the section that starts at line 555), add:

```css
/* ---- ON Paste split view ------------------------------------------------ */

.paste-shell {
	display: flex;
	align-items: flex-start;
	gap: var(--s-5);
}

.paste-list-pane {
	flex: 0 0 20rem;
	min-width: 0;
}

.paste-detail-pane {
	flex: 1;
	min-width: 0;
}

.paste-detail-empty {
	padding: var(--s-4) 0;
}

.snippet-row {
	display: block;
	padding: var(--s-2);
	margin: 0 calc(var(--s-2) * -1);
	border-radius: var(--radius);
	text-decoration: none;
	color: inherit;
}
.snippet-row:hover { background: var(--c-bg-subtle); }
.snippet-row-active,
.snippet-row-active:hover {
	background: var(--c-accent-bg);
}
.snippet-row-active .snippet-row-head span:first-child { color: var(--c-accent); }

.paste-back-btn { display: none; }
```

At the end of the existing `@media (max-width: 640px) { ... }` block (after the `.home-grid` rule, before the block's closing brace), add:

```css

	/* Below this width the two panes can't sit side by side. #paste-detail-open
	 * is checked by the server whenever the detail pane has something to show
	 * (see index.html's "content" block) and replaced via hx-swap-oob whenever
	 * that changes, so this needs no JavaScript. */
	.paste-shell {
		flex-direction: column;
	}
	.paste-list-pane {
		flex: none;
		width: 100%;
	}
	.paste-detail-pane {
		flex: none;
		width: 100%;
		display: none;
	}
	#paste-detail-open:checked ~ .paste-list-pane {
		display: none;
	}
	#paste-detail-open:checked ~ .paste-detail-pane {
		display: block;
	}
	.paste-back-btn {
		display: inline-flex;
	}
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/apps/paste/... -v`
Expected: PASS — including the pre-existing tests that exercised `GET /paste/{id}` (`TestCreateThenView`, `TestViewingSomeoneElsesSnippetIs404`, `TestViewRejectsNonNumericAndMissingIDs`, `TestSnippetBodyIsEscapedInTheView`, `TestHighlightStylesheetIsServedAndCacheable`), now routed through `index`.

Also run the full suite once to catch anything referencing the deleted templates or old handler names: `go test ./...`

- [ ] **Step 9: Commit**

```bash
git add internal/apps/paste/templates/index.html internal/apps/paste/handlers.go \
        internal/apps/paste/paste.go internal/apps/paste/handlers_test.go \
        internal/ui/static/app.css
git rm internal/apps/paste/templates/list.html internal/apps/paste/templates/view.html
git commit -m "paste: merge the list and view pages into one split-view page"
```

---

### Task 3: New snippet inside the pane

Moves snippet creation from `internal/apps/paste/templates/new.html` into the `#detail` pane, as `detailView.Mode == modeNew`.

**Files:**
- Modify: `internal/apps/paste/templates/index.html`
- Delete: `internal/apps/paste/templates/new.html`
- Modify: `internal/apps/paste/handlers.go`
- Test: `internal/apps/paste/handlers_test.go`

**Interfaces:**
- Consumes: `detailView`, `renderIndex`, `renderDetailWithList`, `listFragment`, `viewDetail` from Task 2.
- Produces: `const modeNew = "new"`; `func (a *App) newDetail(r *http.Request, errMsg, title, language, body string) detailView`; template blocks `detail-new` and the `"new"` branch of `detail-body`.

- [ ] **Step 1: Update the failing tests**

`TestNewFormRenders` in `internal/apps/paste/handlers_test.go` currently asserts `doc.MustHave("select[name=language]")` etc. against a full page. Update it to also check it's inside the split-view shell, and add two new tests. Replace `TestNewFormRenders` with:

```go
func TestNewFormRendersInsideTheSplitView(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/new", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".paste-list-pane")
	doc.MustHave("#detail textarea[name=body]")
	doc.MustHave("#detail input[name=title]")
	doc.MustHave("#detail select[name=language]")
	doc.MustHave("input[name=" + web.CSRFFormField + "]")

	nav := doc.MustHave(`nav.shell-nav a[aria-current=page]`)
	if got := htmlassert.Text(nav); got != "ON Paste" {
		t.Errorf("the marked nav item is %q, want ON Paste", got)
	}
	if n := len(doc.QueryAll("select[name=language] option")); n < 10 {
		t.Errorf("the language picker has only %d options", n)
	}
}

func TestNewFormOverHTMXReturnsOnlyTheFragment(t *testing.T) {
	s := newServer(t)
	req := httptest.NewRequest("GET", "/paste/new", nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "shell-bar") {
		t.Error("an HTMX fragment response repeated the page shell")
	}
	htmlassert.Parse(t, "<html><body>"+body+"</body></html>").MustHave("textarea[name=body]")
}

// TestCreateOverHTMXUpdatesListAndDetailTogether: the new row must appear in
// the list at the same time the detail pane shows the new snippet.
func TestCreateOverHTMXUpdatesListAndDetailTogether(t *testing.T) {
	s := newServer(t)
	rec := s.PostHX(t, s.Alice, "/paste/new", url.Values{
		"title": {"Fresh"}, "language": {"go"}, "body": {"package c\n"},
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Fresh") {
		t.Error("the new snippet is not in the detail response")
	}
	if !strings.Contains(body, `hx-swap-oob="true"`) {
		t.Error("the list was not refreshed out of band")
	}
}
```

`internal/apptest.Server` already has a `PostHX` helper (see `internal/apptest/server.go`) built exactly for this: it attaches the session's CSRF token the same way `Post` does, and additionally sets `HX-Request: true`. Use it for every HTMX-flavored POST test in this plan instead of hand-building requests.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/paste/... -run 'TestNewForm|TestCreateOverHTMX' -v`
Expected: FAIL

- [ ] **Step 3: Add the `detail-new` block to `index.html`**

Change the `detail-body` block to add the new branch:

```html
{{define "detail-body"}}
{{if eq .Mode "view"}}{{template "detail-view" .}}
{{else if eq .Mode "new"}}{{template "detail-new" .}}
{{else}}{{template "detail-empty" .}}
{{end}}
{{end}}
```

Add, after the `detail-view` block:

```html
{{define "detail-new"}}
<form class="stack" id="detail-new" method="post" action="/paste/new" hx-post="/paste/new" hx-target="#detail" hx-swap="innerHTML">
	<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
	<div class="notes-toolbar">
		<input type="text" name="title" value="{{.TitleValue}}" placeholder="Untitled (optional)" autocomplete="off" spellcheck="false" class="paste-title-input">
		<div class="notes-toolbar-actions">
			<a class="toolbar-btn paste-back-btn" href="/paste/" hx-get="/paste/" hx-target="#detail" hx-push-url="true"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M19 12H5"/><path d="M12 19l-7-7 7-7"/></svg>Back</a>
			<button type="submit" class="toolbar-btn toolbar-btn-active"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2Z"/><path d="M17 21v-8H7v8"/><path d="M7 3v5h8"/></svg>Save</button>
			<a class="toolbar-btn" href="/paste/" hx-get="/paste/" hx-target="#detail" hx-push-url="true">Cancel</a>
		</div>
	</div>

	{{with .Error}}
	<div class="notice notice-error" role="alert">{{.}}</div>
	{{end}}

	<div class="field">
		<label for="new-language">Language</label>
		<select id="new-language" name="language">
			{{range .Languages}}
			<option value="{{.Value}}"{{if eq .Value $.LanguageValue}} selected{{end}}>{{.Label}}</option>
			{{end}}
		</select>
	</div>

	<div class="field">
		<label for="new-body">Snippet</label>
		<textarea id="new-body" name="body" rows="20" required spellcheck="false" autocapitalize="none">{{.BodyValue}}</textarea>
	</div>
</form>
{{end}}
```

In the `content` block, change the "+ New" link to swap in place:

```html
			<a class="toolbar-btn" href="/paste/new" hx-get="/paste/new" hx-target="#detail" hx-push-url="true"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14"/><path d="M5 12h14"/></svg>New</a>
```

- [ ] **Step 4: Update `newForm` and `create` in `handlers.go`**

Add the mode constant next to `modeView`:

```go
const modeNew = "new"
```

Add `pageTitle`'s new case:

```go
func pageTitle(d detailView) string {
	switch d.Mode {
	case modeView:
		return d.Snippet.DisplayTitle()
	case modeNew:
		return "New snippet"
	default:
		return "Snippets"
	}
}
```

Add a helper next to `viewDetail`:

```go
// newDetail builds the detail pane's data for the new-snippet form.
func (a *App) newDetail(r *http.Request, errMsg, title, language, body string) detailView {
	return detailView{
		Mode:          modeNew,
		TitleValue:    title,
		LanguageValue: language,
		BodyValue:     body,
		Languages:     Languages(),
		Error:         errMsg,
		CSRFToken:     web.CSRFToken(r.Context()),
	}
}
```

Replace `newForm` (it used to call `a.renderNew`):

```go
func (a *App) newForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	a.renderIndex(w, r, userID, http.StatusOK, a.newDetail(r, "", "", "", ""))
}
```

Replace `create`:

```go
func (a *App) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	title := r.PostFormValue("title")
	language := r.PostFormValue("language")
	body := r.PostFormValue("body")

	if !IsLanguage(language) {
		a.renderIndex(w, r, userID, http.StatusBadRequest, a.newDetail(r, "That is not a language I know.", title, language, body))
		return
	}
	if err := Validate(title, body); err != nil {
		a.renderIndex(w, r, userID, http.StatusBadRequest, a.newDetail(r, userMessage(err), title, language, body))
		return
	}

	s, err := a.store.Create(r.Context(), userID, title, language, body)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderIndex(w, r, userID, http.StatusBadRequest, a.newDetail(r, userMessage(err), title, language, body))
			return
		}
		a.deps.Errors.Internal(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/paste/"+strconv.FormatInt(s.ID, 10), http.StatusSeeOther)
		return
	}
	a.renderDetailWithList(w, r, userID, http.StatusCreated, a.viewDetail(r, s))
}
```

Now `renderNew` (the old form-drawing helper) and the `viewModel` type it used for the create form are dead for this path — but `viewModel` is still used by `viewShared`. Delete just the `renderNew` function (it is no longer called from anywhere).

- [ ] **Step 5: Delete the old template**

```bash
rm internal/apps/paste/templates/new.html
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/apps/paste/... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/apps/paste/templates/index.html internal/apps/paste/handlers.go \
        internal/apps/paste/handlers_test.go
git rm internal/apps/paste/templates/new.html
git commit -m "paste: create new snippets inside the detail pane"
```

---

### Task 4: Edit and save inside the pane

Adds the one piece of behavior ON Paste has never had: editing a saved snippet in place.

**Files:**
- Modify: `internal/apps/paste/templates/index.html`
- Modify: `internal/apps/paste/handlers.go`
- Modify: `internal/apps/paste/paste.go`
- Test: `internal/apps/paste/handlers_test.go`

**Interfaces:**
- Consumes: `Store.Update` (Task 1), `detailView`, `renderIndex`, `renderDetailWithList`, `viewDetail` (Task 2).
- Produces: `const modeEdit = "edit"`; `func (a *App) editForm(w http.ResponseWriter, r *http.Request)`; `func (a *App) update(w http.ResponseWriter, r *http.Request)`; `func (a *App) editDetail(r *http.Request, s Snippet, errMsg, title, language, body string) detailView`; routes `GET /{id}/edit`, `POST /{id}`; template blocks `detail-edit` and its `detail-body` branch; an Edit button in `detail-view`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/paste/handlers_test.go`:

```go
func TestEditFormRendersExistingValues(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "My config", "yaml", "key: value\n")

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/"+itoa(id)+"/edit", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if got, _ := htmlassert.Attr(doc.MustHave("input[name=title]"), "value"); got != "My config" {
		t.Errorf("title value = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave("textarea[name=body]")); !strings.Contains(got, "key: value") {
		t.Errorf("body = %q", got)
	}
	if _, ok := htmlassert.Attr(doc.MustHave("option[value=yaml][selected]"), "selected"); !ok {
		t.Error("the current language is not selected")
	}
}

func TestEditingSomeoneElsesSnippetIs404(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "alice's", "go", "secret\n")

	rec := s.Do(t, s.Bob, httptest.NewRequest("GET", "/paste/"+itoa(id)+"/edit", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestSaveEditUpdatesSnippetAndListRow(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "Original", "go", "package a\n")

	rec := s.Post(t, s.Alice, "/paste/"+itoa(id), url.Values{
		"title": {"Renamed"}, "language": {"python"}, "body": {"print(1)\n"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body: %s", rec.Code, rec.Body.String())
	}

	rec2 := s.Do(t, s.Alice, httptest.NewRequest("GET", "/paste/"+itoa(id), nil))
	doc := htmlassert.Parse(t, rec2.Body.String())
	if got := htmlassert.Text(doc.MustHave("h1")); got != "Renamed" {
		t.Errorf("title = %q", got)
	}
	if !strings.Contains(doc.Text(), "print(1)") {
		t.Error("the body was not saved")
	}
}

func TestSaveEditRejectsBadInputAndStaysInEditMode(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "Original", "go", "package a\n")

	rec := s.Post(t, s.Alice, "/paste/"+itoa(id), url.Values{
		"title": {"Original"}, "language": {"go"}, "body": {"   \n"},
	})
	if rec.Code == http.StatusSeeOther {
		t.Fatal("an empty body was accepted")
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".notice-error")
	doc.MustHave("textarea[name=body]")
}

func TestSaveEditRejectsSomeoneElsesSnippet(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "alice's", "go", "secret\n")

	rec := s.Post(t, s.Bob, "/paste/"+itoa(id), url.Values{
		"title": {"hijacked"}, "language": {"go"}, "body": {"x\n"},
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/paste/... -run 'TestEdit|TestSaveEdit' -v`
Expected: FAIL (404s — the routes don't exist yet)

- [ ] **Step 3: Add the `detail-edit` block and wire up Edit/Save/Cancel**

Change `detail-body` again:

```html
{{define "detail-body"}}
{{if eq .Mode "view"}}{{template "detail-view" .}}
{{else if eq .Mode "edit"}}{{template "detail-edit" .}}
{{else if eq .Mode "new"}}{{template "detail-new" .}}
{{else}}{{template "detail-empty" .}}
{{end}}
{{end}}
```

In `detail-view`'s toolbar, add an Edit button right after the Back button:

```html
			<a class="toolbar-btn" href="/paste/{{.Snippet.ID}}/edit" hx-get="/paste/{{.Snippet.ID}}/edit" hx-target="#detail" hx-push-url="true"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 20h9"/><path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4Z"/></svg>Edit</a>
```

Add, after `detail-new`:

```html
{{define "detail-edit"}}
<form class="stack" id="detail-edit" method="post" action="/paste/{{.Snippet.ID}}" hx-post="/paste/{{.Snippet.ID}}" hx-target="#detail" hx-swap="innerHTML">
	<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
	<div class="notes-toolbar">
		<input type="text" name="title" value="{{.TitleValue}}" placeholder="Untitled" autocomplete="off" spellcheck="false" class="paste-title-input">
		<div class="notes-toolbar-actions">
			<a class="toolbar-btn paste-back-btn" href="/paste/{{.Snippet.ID}}" hx-get="/paste/{{.Snippet.ID}}" hx-target="#detail" hx-push-url="true"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M19 12H5"/><path d="M12 19l-7-7 7-7"/></svg>Back</a>
			<button type="submit" class="toolbar-btn toolbar-btn-active"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2Z"/><path d="M17 21v-8H7v8"/><path d="M7 3v5h8"/></svg>Save</button>
			<a class="toolbar-btn" href="/paste/{{.Snippet.ID}}" hx-get="/paste/{{.Snippet.ID}}" hx-target="#detail" hx-push-url="true">Cancel</a>
		</div>
	</div>

	{{with .Error}}
	<div class="notice notice-error" role="alert">{{.}}</div>
	{{end}}

	<div class="field">
		<label for="edit-language-{{.Snippet.ID}}">Language</label>
		<select id="edit-language-{{.Snippet.ID}}" name="language">
			{{range .Languages}}
			<option value="{{.Value}}"{{if eq .Value $.LanguageValue}} selected{{end}}>{{.Label}}</option>
			{{end}}
		</select>
	</div>

	<div class="field">
		<label for="edit-body-{{.Snippet.ID}}">Snippet</label>
		<textarea id="edit-body-{{.Snippet.ID}}" name="body" rows="20" required spellcheck="false" autocapitalize="none">{{.BodyValue}}</textarea>
	</div>
</form>
{{end}}
```

- [ ] **Step 4: Add `editForm`, `update`, `editDetail`, and the route in `handlers.go`/`paste.go`**

In `handlers.go`, add the mode constant:

```go
const modeEdit = "edit"
```

Extend `pageTitle`:

```go
	case modeEdit:
		return "Edit " + d.Snippet.DisplayTitle()
```
(insert this `case` between the existing `modeView` and `modeNew` cases)

Add, after `viewDetail`:

```go
// editDetail builds the detail pane's data for editing an existing snippet.
func (a *App) editDetail(r *http.Request, s Snippet, errMsg, title, language, body string) detailView {
	return detailView{
		Mode:          modeEdit,
		Snippet:       s,
		TitleValue:    title,
		LanguageValue: language,
		BodyValue:     body,
		Languages:     Languages(),
		Error:         errMsg,
		CSRFToken:     web.CSRFToken(r.Context()),
	}
}

func (a *App) editForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.snippetID(w, r)
	if !ok {
		return
	}
	s, err := a.store.ByID(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderIndex(w, r, userID, http.StatusOK, a.editDetail(r, s, "", s.Title, s.Language, s.Body))
}

func (a *App) update(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.snippetID(w, r)
	if !ok {
		return
	}
	s, err := a.store.ByID(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}

	title := r.PostFormValue("title")
	language := r.PostFormValue("language")
	body := r.PostFormValue("body")

	if !IsLanguage(language) {
		a.renderIndex(w, r, userID, http.StatusBadRequest, a.editDetail(r, s, "That is not a language I know.", title, language, body))
		return
	}
	if err := Validate(title, body); err != nil {
		a.renderIndex(w, r, userID, http.StatusBadRequest, a.editDetail(r, s, userMessage(err), title, language, body))
		return
	}

	updated, err := a.store.Update(r.Context(), userID, id, title, language, body)
	if err != nil {
		a.fail(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/paste/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	a.renderDetailWithList(w, r, userID, http.StatusOK, a.viewDetail(r, updated))
}
```

In `internal/apps/paste/paste.go`, add two routes next to the existing `GET /{id}`:

```go
	r.HandleFunc("GET /{id}", a.index)
	r.HandleFunc("GET /{id}/edit", a.editForm)
	r.HandleFunc("POST /{id}", a.update)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/apps/paste/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/apps/paste/templates/index.html internal/apps/paste/handlers.go \
        internal/apps/paste/paste.go internal/apps/paste/handlers_test.go
git commit -m "paste: edit a saved snippet in place"
```

---

### Task 5: Share, unshare, and delete stay in sync with the list

Upgrades the three remaining actions (Share, Unshare, Delete) to update the list pane's row (the "shared" tag, or the row disappearing) in the same response, instead of a full redirect.

**Files:**
- Modify: `internal/apps/paste/handlers.go`
- Test: `internal/apps/paste/handlers_test.go`

**Interfaces:**
- Consumes: `renderDetailWithList`, `viewDetail`, `detailView{}` (empty struct = `detail-empty`) from Task 2.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/paste/handlers_test.go`:

```go
func TestShareOverHTMXUpdatesDetailAndListTag(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "My config", "yaml", "key: value\n")

	rec := s.PostHX(t, s.Alice, "/paste/"+itoa(id)+"/share", url.Values{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Stop sharing") {
		t.Error("the detail pane does not reflect the new shared state")
	}
	if !strings.Contains(body, `hx-swap-oob="true"`) || !strings.Contains(body, "shared") {
		t.Error("the list's \"shared\" tag was not refreshed")
	}
}

func TestDeleteOverHTMXClearsDetailAndRemovesRow(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "Doomed", "go", "package a\n")

	rec := s.PostHX(t, s.Alice, "/paste/"+itoa(id)+"/delete", url.Values{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "Doomed") {
		t.Error("the deleted snippet is still in the response")
	}
	doc := htmlassert.Parse(t, "<html><body>"+body+"</body></html>")
	doc.MustHave(".paste-detail-empty")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/paste/... -run 'TestShareOverHTMX|TestDeleteOverHTMX' -v`
Expected: FAIL

- [ ] **Step 3: Update `delete`, `share`, `unshare` in `handlers.go`**

Replace `delete`:

```go
func (a *App) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.snippetID(w, r)
	if !ok {
		return
	}

	if err := a.store.Delete(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("snippet deleted", "app", ID, "user_id", userID, "snippet_id", id)

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/paste/", http.StatusSeeOther)
		return
	}
	a.renderDetailWithList(w, r, userID, http.StatusOK, detailView{})
}
```

Replace `share`:

```go
func (a *App) share(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.snippetID(w, r)
	if !ok {
		return
	}

	if _, err := a.store.Share(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("snippet shared", "app", ID, "user_id", userID, "snippet_id", id)

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/paste/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	s, err := a.store.ByID(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderDetailWithList(w, r, userID, http.StatusOK, a.viewDetail(r, s))
}
```

Replace `unshare`:

```go
func (a *App) unshare(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.snippetID(w, r)
	if !ok {
		return
	}

	if err := a.store.Unshare(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("snippet unshared", "app", ID, "user_id", userID, "snippet_id", id)

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/paste/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	s, err := a.store.ByID(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderDetailWithList(w, r, userID, http.StatusOK, a.viewDetail(r, s))
}
```

Now enable HTMX on the three toolbar forms in `detail-view` (`index.html`): add `hx-post="..." hx-target="#detail" hx-swap="innerHTML"` matching each form's existing `action`, e.g.:

```html
			<form method="post" action="/paste/{{.Snippet.ID}}/unshare" hx-post="/paste/{{.Snippet.ID}}/unshare" hx-target="#detail" hx-swap="innerHTML">
```
```html
			<form method="post" action="/paste/{{.Snippet.ID}}/share" hx-post="/paste/{{.Snippet.ID}}/share" hx-target="#detail" hx-swap="innerHTML">
```
```html
			<form method="post" action="/paste/{{.Snippet.ID}}/delete" hx-post="/paste/{{.Snippet.ID}}/delete" hx-target="#detail" hx-swap="innerHTML"
			      data-confirm="Delete “{{.Snippet.DisplayTitle}}”? This cannot be undone.">
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/apps/paste/... -v`
Expected: PASS

- [ ] **Step 5: Run the full test suite**

Run: `go test ./...`
Expected: PASS — nothing outside `internal/apps/paste` should reference the deleted templates or old handler names, but this catches it if something does.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/paste/handlers.go internal/apps/paste/handlers_test.go
git commit -m "paste: keep the list in sync when sharing, unsharing, or deleting"
```

---

### Task 6: Manual verification in the browser

Everything above is exercised by `go test`, but nobody has looked at it yet. This task is the check the spec's "Testing" section calls for: click through the golden path and the mobile breakpoint before calling this done.

**Files:** none (verification only).

- [ ] **Step 1: Start the app**

Use whatever this repo's own run instructions are (check `AGENTS.md` / `CLAUDE.md` at the repo root, or `.claude/launch.json` if one already exists) to build and run the server against a local database, then sign in as an existing (or freshly created) user.

- [ ] **Step 2: Walk the desktop golden path**

In a browser at the app's `/paste/` page:
1. Confirm the list and an empty "Select a snippet…" pane render side by side.
2. Click "+ New", fill in a title/language/body, Save — confirm the new snippet appears selected in the pane and its row appears in the list, and the browser URL is now `/paste/{id}`.
3. Click Edit, change the title and body, Save — confirm the pane shows the new content and the list row's title/preview updated too.
4. Click Share — confirm the pane shows "Stop sharing" and a share link, and the list row now shows a "shared" tag. Click "Stop sharing" and confirm both disappear.
5. Click Raw and Copy — confirm Raw opens the plain-text view and Copy copies to the clipboard (paste it somewhere to check).
6. Click another row in the list — confirm the pane updates without a full page reload (watch the browser's network tab for an XHR, not a full navigation) and the URL updates.
7. Use the browser's Back button after two or three selections — confirm it steps back through the previously viewed snippets.
8. Click Delete on a snippet, confirm the browser's confirm dialog appears, confirm it — confirm the pane clears to "Select a snippet…" and the row disappears from the list.
9. Reload the page on a `/paste/{id}` URL directly (not via a client-side navigation) — confirm it renders correctly on a cold load.

- [ ] **Step 3: Check the no-JavaScript fallback**

Disable JavaScript in the browser's dev tools and repeat the create/edit/share/delete steps above. Every action should still work via full-page navigation (slower, but functional) — this is what the `href`/`action`/`method` attributes alongside every `hx-*` attribute are for.

- [ ] **Step 4: Check the mobile breakpoint**

Resize the browser below 640px width (or use dev tools' device emulation). Confirm:
1. Only the list is visible at first.
2. Selecting a snippet shows it full-width in place of the list, with a visible "Back" button in its toolbar.
3. Clicking Back returns to the list.

- [ ] **Step 5: Report**

Note anything that didn't match the spec (`docs/superpowers/specs/2026-09-03-paste-split-view-redesign-design.md`) so it can be triaged before merging — this task has no code changes to commit, only findings.

---

## Self-review notes

- Every requirement in the spec (single split-view page, unified toolbar, URL sync via `hx-push-url`, inline edit mode, new-paste-in-pane, mobile stacking with a Back button, search/two-step-delete deferred) is covered by a task above.
- `detailView`, `listFragment`, and `indexView` are introduced once in Task 2 and reused unchanged through Task 5; no field is renamed or duplicated under a different name later.
- The public share page (`internal/apps/paste/templates/shared.html`, `viewShared`, `rawShared`, and the `viewModel` type) is untouched throughout — it is out of scope for this redesign and continues to serve unauthenticated visitors exactly as before.
- Every HTMX-flavored POST test uses `internal/apptest.Server`'s existing `PostHX` helper (confirmed present in `internal/apptest/server.go`), so no test hand-rolls CSRF token plumbing.
