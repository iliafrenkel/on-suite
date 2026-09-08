# Shell breadcrumb + tab title OOB sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After any htmx navigation in ON Notes (zoom) or ON Paste
(select/new/edit/delete), the shell's top breadcrumb and the document
`<title>` describe the same thing the URL and main content now do —
closing issue #205, which affects both apps today (not just Notes).

**Architecture:** A single shared template, `shell-crumb-tail`, is
extracted from `base.html`'s existing breadcrumb markup (pure extraction,
no visible change to a full page load) and wrapped in a stable
`#shell-crumb-tail` span. Each app's fragment-rendering path already has
(or gains, this plan) the same `a.deps.Page(r, title)` call its full-page
path uses — cheap, request/cookie-derived, no I/O — and feeds
`Title`/`Shell` into `shell-crumb-tail` via the `dict` template helper this
codebase already uses elsewhere, emitting it as an `hx-swap-oob="true"`
block alongside each app's other OOB elements (Notes' `outline-heading`,
Paste's list/pane-toggle).

**Tech Stack:** Go `html/template`, htmx 2.0.10 (vendored), the existing
`dict` template helper (`internal/platform/render/render.go:109`), the
existing Go test harness (`htmlassert`, each app's own `newServer`/`s.Do`
style helpers).

## Global Constraints

- `main` is protected — work on a branch, open a PR, never push directly to
  `main`.
- Full check must stay green: `gofmt -l .` (empty), `go vet ./...`,
  `go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...`, `go mod tidy`
  (no diff), `go test ./... -race -count=1`.
- No CGO, no Node/npm/JS build step, no new platform dependency, **no new
  platform Go API** — `a.deps.Page`/`app.NewPage` are already fully
  sufficient and must not be modified.
- Commit subjects: Conventional Commits `type(scope): summary`; scope
  `platform` for the base.html/app.css change, `notes` for the Notes
  hookup, `paste` for the Paste hookup.
- The `shell-crumb-tail` extraction in `base.html` must not change the
  rendered output of a full page load (pure extraction) — verified by the
  existing `TestShellHasBreadcrumbsSidebarAndFooter`
  (`internal/platform/render/render_test.go`) continuing to pass.
- Only the connectivity-indicator-adjacent shell chrome must stay
  untouched by any OOB swap this plan adds — no OOB emission may re-render
  `.shell-user`/`.conn-indicator` (`base.html:41-43`).
- Spec: [docs/superpowers/specs/2026-09-08-shell-crumb-oob-sync-design.md](../specs/2026-09-08-shell-crumb-oob-sync-design.md)

---

### Task 1: Extract `shell-crumb-tail` and fix its flex-layout wrapping

**Files:**
- Modify: `internal/ui/templates/base.html:32-39` (the `shell-crumbs` nav
  block, plus a new `{{define "shell-crumb-tail"}}` placed directly above
  `{{define "shell"}}`)
- Modify: `internal/ui/static/app.css` (after the existing `.shell-crumbs`
  rule, currently `internal/ui/static/app.css:209-219`)
- Test: `internal/platform/render/render_test.go`
  (`TestShellHasBreadcrumbsSidebarAndFooter`)

**Interfaces:**
- Consumes: `render.Page` (`Title string`, `Shell render.Shell`), unchanged
  — this task only moves existing template markup, it does not touch
  `render.Page`/`render.Shell`'s Go definitions.
- Produces: a template named `shell-crumb-tail`, callable from any other
  template in the same parsed set (every app's own templates are cloned
  from this same base — see `internal/platform/render/render.go`'s
  `addPage`) with a `dict`-built value carrying exactly two keys,
  `"Title"` (string) and `"Shell"` (`render.Shell`). A `<span
  id="shell-crumb-tail">` wraps its full-page-render call site with no
  `hx-swap-oob`; later tasks add `hx-swap-oob="true"` copies of that same
  span from each app's own fragment response. `#shell-crumb-tail { display:
  contents; }` in `app.css` is what later tasks' OOB spans rely on to avoid
  a flex-gap regression.

- [ ] **Step 1: Extend the existing platform-level test to pin the new id**

In `internal/platform/render/render_test.go`, inside
`TestShellHasBreadcrumbsSidebarAndFooter`, immediately after the existing
block:

```go
	// Breadcrumbs: Home / ON Paste / New snippet.
	crumbs := doc.Text()
	for _, want := range []string{"Home", "ON Paste", "New snippet"} {
		if !strings.Contains(crumbs, want) {
			t.Errorf("breadcrumb text missing %q; got %q", want, crumbs)
		}
	}
```

add:

```go
	// The app-name and title segments live in their own stable-id span, so
	// a fragment response can later OOB-swap just that (issue #205).
	tail := doc.MustHave("#shell-crumb-tail")
	tailText := htmlassert.Text(tail)
	for _, want := range []string{"ON Paste", "New snippet"} {
		if !strings.Contains(tailText, want) {
			t.Errorf("shell-crumb-tail text missing %q; got %q", want, tailText)
		}
	}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/platform/render/... -run TestShellHasBreadcrumbsSidebarAndFooter -v`
Expected: FAIL — `doc.MustHave("#shell-crumb-tail")` finds nothing, since
that id doesn't exist yet.

- [ ] **Step 3: Extract the template and fix the wrapping**

In `internal/ui/templates/base.html`, change:

```html
	<nav class="shell-crumbs" aria-label="Breadcrumb">
		<a href="/">Home</a>
		{{if .Shell.ActiveAppName}}
		<span aria-hidden="true">/</span>
		{{if and .Title .Shell.LoggedIn}}<a href="{{.Shell.ActiveAppPath}}" title="{{.Shell.ActiveAppName}}">{{.Shell.ActiveAppName}}</a>{{else}}<span title="{{.Shell.ActiveAppName}}">{{.Shell.ActiveAppName}}</span>{{end}}
		{{end}}
		{{if .Title}}<span aria-hidden="true">/</span><span title="{{.Title}}">{{.Title}}</span>{{end}}
	</nav>
```

to:

```html
	<nav class="shell-crumbs" aria-label="Breadcrumb">
		<a href="/">Home</a>
		<span id="shell-crumb-tail">{{template "shell-crumb-tail" .}}</span>
	</nav>
```

Then, directly above `{{define "shell"}}` in the same file, add:

```html
{{define "shell-crumb-tail"}}
{{if .Shell.ActiveAppName}}
<span aria-hidden="true">/</span>
{{if and .Title .Shell.LoggedIn}}<a href="{{.Shell.ActiveAppPath}}" title="{{.Shell.ActiveAppName}}">{{.Shell.ActiveAppName}}</a>{{else}}<span title="{{.Shell.ActiveAppName}}">{{.Shell.ActiveAppName}}</span>{{end}}
{{end}}
{{if .Title}}<span aria-hidden="true">/</span><span title="{{.Title}}">{{.Title}}</span>{{end}}
{{end}}
```

In `internal/ui/static/app.css`, immediately after the existing block:

```css
.shell-crumbs {
	display: flex;
	align-items: center;
	gap: var(--s-2);
	flex: 1;
	min-width: 0;
	font-size: var(--fs-sm);
	color: var(--c-text-dim);
	overflow-x: auto;
	white-space: nowrap;
}
```

add:

```css
/* Invisible to layout, so its children (the app-name and title segments)
 * stay direct flex items of .shell-crumbs and keep its gap between them —
 * issue #205. Without this, wrapping them in a span for OOB-swap targeting
 * would collapse that gap, the same class of regression #outline-heading
 * hit without its own .stack class (see the zoom-transition PR). */
#shell-crumb-tail {
	display: contents;
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/platform/render/... -run TestShellHasBreadcrumbsSidebarAndFooter -v`
Expected: PASS

- [ ] **Step 5: Run the full platform/render and ui package tests, plus both apps' suites, to check for regressions**

Run: `go test ./internal/platform/render/... ./internal/ui/... ./internal/apps/notes/... ./internal/apps/paste/... -race -count=1`
Expected: PASS — this is a pure markup extraction, so every existing test
that renders any page in any app (which is most of both apps' test suites)
must still pass unchanged.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/templates/base.html internal/ui/static/app.css internal/platform/render/render_test.go
git commit -m "refactor(platform): extract shell-crumb-tail for OOB reuse (issue #205)"
```

---

### Task 2: Wire ON Notes' zoom to keep the shell crumb and title in sync

**Files:**
- Modify: `internal/apps/notes/view.go` (`outlineView` struct)
- Modify: `internal/apps/notes/handlers.go` (`renderOutlineFragment`,
  currently `internal/apps/notes/handlers.go:189-227`)
- Modify: `internal/apps/notes/templates/outline.html:32` (`outline-swap`)
- Test: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `shell-crumb-tail` (Task 1) via `dict "Title" .Title "Shell"
  .Shell`; `a.deps.Page(r, title) render.Page` (pre-existing,
  `internal/platform/app/app.go:103`); `view.Zoomed`/`view.Root` (already
  set by the existing `if rootID != RootID` block in
  `renderOutlineFragment`, added by the zoom-transition PR's Task 1.5 —
  unchanged by this task, just read from).
- Produces: `outlineView.Title string` and `outlineView.Shell
  render.Shell`, populated only by `renderOutlineFragment` (a full-page
  render via `renderOutline` continues to route `Title`/`Shell` through
  `render.Page` directly, as it always has — those two fields on
  `outlineView` are read by nothing except the new OOB block in
  `outline-swap`, and `renderOutline` never uses `outline-swap`).

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/notes/handlers_test.go` (near the existing
`TestZoomingViaHTMXUpdatesTheBreadcrumb`/
`TestZoomingOutViaHTMXShowsTheUnzoomedHeading`):

```go
// TestZoomingViaHTMXUpdatesTheShellCrumb is issue #205's Notes half: the
// shell's own top breadcrumb (outside #outline-heading entirely) must also
// follow an htmx zoom, via the shared shell-crumb-tail OOB block.
func TestZoomingViaHTMXUpdatesTheShellCrumb(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "Projects")

	req := httptest.NewRequest("GET", "/notes/"+itoa(id), nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	tail := htmlassert.Parse(t, body).MustHave("#shell-crumb-tail")
	if got, _ := htmlassert.Attr(tail, "hx-swap-oob"); got != "true" {
		t.Errorf("shell-crumb-tail hx-swap-oob = %q, want true", got)
	}
	if !strings.Contains(htmlassert.Text(tail), "Projects") {
		t.Error("shell-crumb-tail does not show the new zoom root's title")
	}
}

// TestZoomingOutViaHTMXShowsPlainShellCrumb is the zoom-out counterpart:
// the shell crumb tail must collapse back to just the app name, not keep
// showing the note title from the level the user just left.
func TestZoomingOutViaHTMXShowsPlainShellCrumb(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "Projects")

	req := httptest.NewRequest("GET", "/notes/", nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	tail := htmlassert.Parse(t, body).MustHave("#shell-crumb-tail")
	if got, _ := htmlassert.Attr(tail, "hx-swap-oob"); got != "true" {
		t.Errorf("shell-crumb-tail hx-swap-oob = %q, want true", got)
	}
	tailText := htmlassert.Text(tail)
	if strings.Contains(tailText, "Projects") {
		t.Error("shell-crumb-tail still shows the previous zoom root's title")
	}
	if !strings.Contains(tailText, "ON Notes") {
		t.Error("shell-crumb-tail does not show the app name at the top level")
	}
}

// TestZoomedPageShowsShellCrumbTail pins the shell-crumb-tail extraction
// (Task 1) as behavior-preserving for a real (non-htmx) zoomed page load in
// this app specifically, not just the platform's own synthetic
// render_test.go fixture.
func TestZoomedPageShowsShellCrumbTail(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "Projects")

	doc := s.Get(t, s.Alice, "/notes/"+itoa(id))
	tail := doc.MustHave("#shell-crumb-tail")
	if _, ok := htmlassert.Attr(tail, "hx-swap-oob"); ok {
		t.Error("a full page load's shell-crumb-tail carries hx-swap-oob")
	}
	tailText := htmlassert.Text(tail)
	for _, want := range []string{"ON Notes", "Projects"} {
		if !strings.Contains(tailText, want) {
			t.Errorf("shell-crumb-tail missing %q; got %q", want, tailText)
		}
	}
}
```

Also update the existing OOB-id allowlist helper in the same file:

```go
func assertOnlyKnownOOBIsOOB(t *testing.T, body string) {
	t.Helper()
	allowed := map[string]bool{"show-completed-toggle": true, "due-badge": true, "outline-heading": true}
	for _, n := range htmlassert.Parse(t, body).QueryAll("[hx-swap-oob]") {
		if id, _ := htmlassert.Attr(n, "id"); !allowed[id] {
			t.Errorf("unexpected hx-swap-oob element (id=%q); only show-completed-toggle, due-badge, and outline-heading may be out of band", id)
		}
	}
}
```

becomes:

```go
func assertOnlyKnownOOBIsOOB(t *testing.T, body string) {
	t.Helper()
	allowed := map[string]bool{"show-completed-toggle": true, "due-badge": true, "outline-heading": true, "shell-crumb-tail": true}
	for _, n := range htmlassert.Parse(t, body).QueryAll("[hx-swap-oob]") {
		if id, _ := htmlassert.Attr(n, "id"); !allowed[id] {
			t.Errorf("unexpected hx-swap-oob element (id=%q); only show-completed-toggle, due-badge, outline-heading, and shell-crumb-tail may be out of band", id)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/... -run 'ShellCrumb' -v`
Expected: FAIL — all three new tests fail (`#shell-crumb-tail` not found,
or found with no `hx-swap-oob` on the fragment path).

- [ ] **Step 3: Add `Title`/`Shell` to `outlineView`**

In `internal/apps/notes/view.go`, change the import line from:

```go
import "html/template"
```

to:

```go
import (
	"html/template"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
)
```

Then add, at the end of the `outlineView` struct (after `SearchAction
string`):

```go
	// Title and Shell are only populated by renderOutlineFragment, for the
	// shell-crumb-tail OOB block outline-swap emits — a full page render's
	// shell crumb comes from render.Page directly (via app.Deps.Page),
	// which outlineView never touches. See handlers.go.
	Title string
	Shell render.Shell
```

- [ ] **Step 4: Populate them in `renderOutlineFragment`**

In `internal/apps/notes/handlers.go`, `renderOutlineFragment` currently
reads (after the `if rootID != RootID { ... }` block that sets
`view.Root`/`view.Zoomed`/`view.Crumbs`/`view.ShareURL`):

```go
	flat, err := a.store.Outline(r.Context(), userID, rootID, showCompleted, query != "")
```

Insert immediately before that line (still inside the function, after the
`if rootID != RootID` block's closing `}`):

```go
	title := ""
	if view.Zoomed {
		title = view.Root.DisplayTitle()
	}
	page := a.deps.Page(r, title)
	view.Title, view.Shell = page.Title, page.Shell

```

- [ ] **Step 5: Wire the OOB block and unify the `<title>` source**

In `internal/apps/notes/templates/outline.html`, change line 32 from:

```html
{{define "outline-swap"}}<title>{{if .Zoomed}}{{.Root.DisplayTitle}} · {{end}}ON Suite</title><div id="outline-heading" class="stack" hx-swap-oob="true">{{template "outline-heading" .}}</div>{{template "outline-body" .}}{{template "show-completed-toggle" .}}{{template "due-badge" .}}{{end}}
```

to:

```html
{{define "outline-swap"}}<title>{{if .Title}}{{.Title}} · {{end}}ON Suite</title><span id="shell-crumb-tail" hx-swap-oob="true">{{template "shell-crumb-tail" (dict "Title" .Title "Shell" .Shell)}}</span><div id="outline-heading" class="stack" hx-swap-oob="true">{{template "outline-heading" .}}</div>{{template "outline-body" .}}{{template "show-completed-toggle" .}}{{template "due-badge" .}}{{end}}
```

(The `<title>` tag now sources from the Go-computed `.Title` field instead
of the inline `{{if .Zoomed}}{{.Root.DisplayTitle}}{{end}}` conditional —
same value, computed once, also used by the new OOB block, so the two can
never drift apart.)

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -run 'ShellCrumb' -v`
Expected: PASS (all three new tests)

- [ ] **Step 7: Run the full notes package test suite to check for regressions**

Run: `go test ./internal/apps/notes/... -race -count=1 -v`
Expected: PASS — in particular every existing zoom, breadcrumb,
`outline-heading`, and OOB-allowlist test must still pass, confirming the
`<title>` source change is behavior-identical and no other OOB id was
accidentally introduced.

- [ ] **Step 8: Run the full check battery**

Run: `gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go mod tidy && git diff --exit-code go.mod go.sum && go test ./... -race -count=1`
Expected: every command clean/passing, no diff from `go mod tidy`.

- [ ] **Step 9: Commit**

```bash
git add internal/apps/notes/view.go internal/apps/notes/handlers.go internal/apps/notes/templates/outline.html internal/apps/notes/handlers_test.go
git commit -m "fix(notes): keep the shell breadcrumb and title in sync on an htmx zoom"
```

---

### Task 3: Wire ON Paste's snippet navigation to keep the shell crumb and title in sync

**Files:**
- Modify: `internal/apps/paste/handlers.go` (`indexView` struct,
  `renderIndex`'s htmx branch, `renderDetailWithList`)
- Modify: `internal/apps/paste/templates/index.html:24` (`detail-with-list`)
- Test: `internal/apps/paste/handlers_test.go`

**Interfaces:**
- Consumes: `shell-crumb-tail` (Task 1) via `dict "Title" .Title "Shell"
  .Shell`; the existing `pageTitle(detail detailView) string` helper
  (`internal/apps/paste/handlers.go:325`, already used by the full-page
  path); `a.deps.Page(r, title) render.Page` (pre-existing).
- Produces: `indexView.Title string` and `indexView.Shell render.Shell`,
  populated by both fragment-rendering functions. Nothing later depends on
  these beyond `detail-with-list`'s own OOB block — this is the last task
  in the plan.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/paste/handlers_test.go` (near the existing
`TestSelectingOverHTMXReturnsOnlyTheFragment`):

```go
// TestSelectingOverHTMXUpdatesTheShellCrumbAndTitle is issue #205's Paste
// half: selecting a different snippet over HTMX must refresh the shell's
// top breadcrumb and the document title, not just the detail pane.
func TestSelectingOverHTMXUpdatesTheShellCrumbAndTitle(t *testing.T) {
	s := newServer(t)
	id := s.createSnippet(t, s.Alice, "My config", "yaml", "key: value\n")

	req := httptest.NewRequest("GET", "/paste/"+itoa(id), nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<title>My config · ON Suite</title>") {
		t.Error("the fragment does not carry the new document title")
	}
	tail := htmlassert.Parse(t, body).MustHave("#shell-crumb-tail")
	if got, _ := htmlassert.Attr(tail, "hx-swap-oob"); got != "true" {
		t.Errorf("shell-crumb-tail hx-swap-oob = %q, want true", got)
	}
	if !strings.Contains(htmlassert.Text(tail), "My config") {
		t.Error("shell-crumb-tail does not show the selected snippet's title")
	}
}

// TestNewFormOverHTMXShowsPlaceholderTitle covers pageTitle's "new" branch
// reaching the shell crumb the same way the view/edit branches do.
func TestNewFormOverHTMXShowsPlaceholderTitle(t *testing.T) {
	s := newServer(t)
	req := httptest.NewRequest("GET", "/paste/new", nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	tail := htmlassert.Parse(t, rec.Body.String()).MustHave("#shell-crumb-tail")
	if !strings.Contains(htmlassert.Text(tail), "New snippet") {
		t.Error("shell-crumb-tail does not show the new-snippet placeholder title")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/paste/... -run 'ShellCrumb|PlaceholderTitle' -v`
Expected: FAIL — `#shell-crumb-tail` not found in either fragment response,
and no `<title>` element present.

- [ ] **Step 3: Add `Title`/`Shell` to `indexView`**

In `internal/apps/paste/handlers.go`, change:

```go
type indexView struct {
	List   listFragment
	Detail detailView
}
```

to:

```go
type indexView struct {
	List   listFragment
	Detail detailView
	// Title and Shell are only populated by the two HTMX fragment paths
	// below, for the shell-crumb-tail OOB block detail-with-list emits — a
	// full page render's shell crumb comes from render.Page directly (via
	// app.Deps.Page), which indexView never touches.
	Title string
	Shell render.Shell
}
```

- [ ] **Step 4: Populate them in both fragment-rendering functions**

In `renderIndex` (`internal/apps/paste/handlers.go`), the htmx branch
currently reads:

```go
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		view := indexView{
			List:   listFragment{Items: items, ActiveID: detail.Snippet.ID, OOB: true},
			Detail: detail,
		}
```

Add immediately after that struct literal, still inside the `if` block,
before the existing comment about `responseHandling`:

```go
		page := a.deps.Page(r, pageTitle(detail))
		view.Title, view.Shell = page.Title, page.Shell
```

In `renderDetailWithList`, currently:

```go
	view := indexView{
		List:   listFragment{Items: items, ActiveID: detail.Snippet.ID, OOB: true},
		Detail: detail,
	}
	if err := a.deps.Render.Fragment(w, status, "paste/index", "detail-with-list", view); err != nil {
```

Add between the struct literal and the `Fragment` call:

```go
	view := indexView{
		List:   listFragment{Items: items, ActiveID: detail.Snippet.ID, OOB: true},
		Detail: detail,
	}
	page := a.deps.Page(r, pageTitle(detail))
	view.Title, view.Shell = page.Title, page.Shell
	if err := a.deps.Render.Fragment(w, status, "paste/index", "detail-with-list", view); err != nil {
```

- [ ] **Step 5: Wire the OOB block in `detail-with-list`**

In `internal/apps/paste/templates/index.html`, change:

```html
{{define "detail-with-list"}}{{template "detail-body" .Detail}}{{template "list-items" .List}}<input type="checkbox" id="paste-detail-open" class="visually-hidden" hx-swap-oob="true"{{if .Detail.Mode}} checked{{end}} aria-hidden="true" tabindex="-1">{{end}}
```

to:

```html
{{define "detail-with-list"}}<title>{{if .Title}}{{.Title}} · {{end}}ON Suite</title><span id="shell-crumb-tail" hx-swap-oob="true">{{template "shell-crumb-tail" (dict "Title" .Title "Shell" .Shell)}}</span>{{template "detail-body" .Detail}}{{template "list-items" .List}}<input type="checkbox" id="paste-detail-open" class="visually-hidden" hx-swap-oob="true"{{if .Detail.Mode}} checked{{end}} aria-hidden="true" tabindex="-1">{{end}}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/apps/paste/... -run 'ShellCrumb|PlaceholderTitle' -v`
Expected: PASS (both new tests)

- [ ] **Step 7: Run the full paste package test suite to check for regressions**

Run: `go test ./internal/apps/paste/... -race -count=1 -v`
Expected: PASS — in particular every existing selecting/create/edit/
share/delete-over-HTMX test must still pass, since `detail-with-list`'s
existing content (`detail-body`, `list-items`, the pane-toggle checkbox) is
unchanged, only prefixed with the two new elements.

- [ ] **Step 8: Run the full check battery**

Run: `gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go mod tidy && git diff --exit-code go.mod go.sum && go test ./... -race -count=1`
Expected: every command clean/passing, no diff from `go mod tidy`.

- [ ] **Step 9: Commit**

```bash
git add internal/apps/paste/handlers.go internal/apps/paste/templates/index.html internal/apps/paste/handlers_test.go
git commit -m "fix(paste): keep the shell breadcrumb and title in sync on an htmx navigation"
```

---

### Task 4: Manual browser verification and PR

**Files:** none (verification + process step)

- [ ] **Step 1: Build a local binary and start a server**

```bash
go build -o /tmp/onsuite-crumb-verify ./cmd/onsuite
mkdir -p /tmp/onsuite-crumb-verify-data
echo verifypass123 | /tmp/onsuite-crumb-verify user add verifyuser --admin --data-dir /tmp/onsuite-crumb-verify-data
/tmp/onsuite-crumb-verify serve --data-dir /tmp/onsuite-crumb-verify-data --addr 127.0.0.1:8097 &
```

- [ ] **Step 2: Verify ON Notes in the browser**

Using the Browser tool: log in, create a bullet with a nested child, zoom
in via the row dot — confirm the shell's top breadcrumb (`Home / ON Notes /
...`) now shows the zoomed note's title, not just the in-page breadcrumb;
zoom out via "All notes" or the shell crumb's own "ON Notes" link — confirm
it collapses back to `Home / ON Notes`. Confirm no visible layout shift or
spacing change in the shell bar (the `display: contents` fix).

- [ ] **Step 3: Verify ON Paste in the browser**

Create two snippets, click between them in the list — confirm the shell
breadcrumb updates to each snippet's title, and the browser tab title
updates too. Open the editor and the "New" form — confirm "Edit ..." and
"New snippet" show correctly in the shell breadcrumb.

- [ ] **Step 4: Stop the verification server**

```bash
kill %1
rm -rf /tmp/onsuite-crumb-verify /tmp/onsuite-crumb-verify-data
```

- [ ] **Step 5: Push and update the PR**

```bash
git push
```

(Branch `shell-crumb-oob-sync` already has an open PR, #206, carrying the
spec — pushing adds these commits to it. Update the PR title/description
to reflect the finished implementation, same as the zoom-transition PR's
own finishing step.)
