# ON Notes: cross-fade transition on zoom in/out Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Zooming into a note and zooming back out (breadcrumb, "All notes",
browser back/forward) gets a smooth CSS cross-fade instead of a hard cut,
without touching any other outline action's speed.

**Architecture:** Three existing zoom links in `outline.html` gain
`hx-get`/`hx-target="#outline"`/`hx-swap="innerHTML transition:true"`/
`hx-push-url="true"` (htmx 2.0.10, already vendored, already wired to the
existing `renderOutlineOrFragment` fragment path — no server changes). A
single CSS block in `app.css` names `#outline` as a view-transition region
and overrides the browser's default animation with a plain opacity
cross-fade, disabled under `prefers-reduced-motion: reduce`.

**Tech Stack:** Go `html/template`, htmx 2.0.10 (vendored), plain CSS (View
Transitions API), existing Go test harness (`htmlassert`, `newServer` test
helpers in `internal/apps/notes/handlers_test.go`).

## Global Constraints

- `main` is protected — work on a branch, open a PR, never push directly to
  `main`.
- Full check must stay green: `gofmt -l .` (empty), `go vet ./...`,
  `go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...`, `go mod tidy`
  (no diff), `go test ./... -race -count=1`.
- No CGO, no Node/npm/JS build step, no new platform dependency.
- Commit subjects: Conventional Commits `type(scope): summary`, scope
  `notes` for this feature.
- Only the two named zoom link sites and the "All notes" link get
  `transition:true`; every other `#outline`-swapping action in
  `outline.html` (indent, outdent, done, archive, delete, move, new,
  search-as-you-type) must be left byte-for-byte unchanged.
- Spec: [docs/superpowers/specs/2026-09-08-notes-zoom-transition-design.md](../specs/2026-09-08-notes-zoom-transition-design.md)

---

### Task 1: HTMX-wire the three zoom links

**Files:**
- Modify: `internal/apps/notes/templates/outline.html:104` ("All notes" link)
- Modify: `internal/apps/notes/templates/outline.html:110` (ancestor
  breadcrumb link)
- Modify: `internal/apps/notes/templates/outline.html:328-331` (bullet dot
  zoom-in link)
- Test: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: existing route `GET /notes/{id}` and `GET /notes/` (both already
  branch on `web.IsHTMX`/`web.IsHTMXHistoryRestore` in
  `renderOutlineOrFragment`, `internal/apps/notes/handlers.go:86-96` —
  unchanged by this task).
- Produces: three `<a>` elements in the rendered outline page each carrying
  `hx-get`, `hx-target="#outline"`, `hx-swap="innerHTML transition:true"`,
  `hx-push-url="true"`, alongside their existing `href` (kept for
  progressive enhancement — no-JS/no-htmx still works as a plain navigation).
  Later tasks (CSS) depend on the swap target staying `#outline` and the
  swap modifier staying exactly the string `"innerHTML transition:true"`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/notes/handlers_test.go` (near the existing
`TestBulletDotZoomsIn` / `TestBreadcrumbAncestorLinksStayPlainText` tests,
around line 635):

```go
// TestBulletDotZoomsInWithHTMXTransition pins the htmx wiring the cross-fade
// transition depends on: same route as before, now also an in-place htmx
// swap so the CSS view-transition on #outline can fire.
func TestBulletDotZoomsInWithHTMXTransition(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "Projects")

	doc := s.Get(t, s.Alice, "/notes/")
	dot := doc.MustHave("a.outline-dot")
	if got, _ := htmlassert.Attr(dot, "hx-get"); got != "/notes/"+itoa(id) {
		t.Errorf("bullet dot hx-get = %q, want /notes/%d", got, id)
	}
	if got, _ := htmlassert.Attr(dot, "hx-target"); got != "#outline" {
		t.Errorf("bullet dot hx-target = %q, want #outline", got)
	}
	if got, _ := htmlassert.Attr(dot, "hx-swap"); got != "innerHTML transition:true" {
		t.Errorf("bullet dot hx-swap = %q, want %q", got, "innerHTML transition:true")
	}
	if got, _ := htmlassert.Attr(dot, "hx-push-url"); got != "true" {
		t.Errorf("bullet dot hx-push-url = %q, want true", got)
	}
}

// TestAllNotesLinkUsesHTMXTransition covers the top-level "All notes"
// breadcrumb link, which zooms back out to the root.
func TestAllNotesLinkUsesHTMXTransition(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "Projects")

	doc := s.Get(t, s.Alice, "/notes/"+itoa(id))
	link := doc.MustHave("nav.outline-crumbs a")
	if got, _ := htmlassert.Attr(link, "hx-get"); got != "/notes/" {
		t.Errorf("All notes link hx-get = %q, want /notes/", got)
	}
	if got, _ := htmlassert.Attr(link, "hx-target"); got != "#outline" {
		t.Errorf("All notes link hx-target = %q, want #outline", got)
	}
	if got, _ := htmlassert.Attr(link, "hx-swap"); got != "innerHTML transition:true" {
		t.Errorf("All notes link hx-swap = %q, want %q", got, "innerHTML transition:true")
	}
	if got, _ := htmlassert.Attr(link, "hx-push-url"); got != "true" {
		t.Errorf("All notes link hx-push-url = %q, want true", got)
	}
}

// TestAncestorCrumbLinkUsesHTMXTransition covers an ancestor breadcrumb link
// (not the top-level "All notes" one, not the current/leaf crumb).
func TestAncestorCrumbLinkUsesHTMXTransition(t *testing.T) {
	s := newServer(t)
	projects := s.seed(t, s.Alice, notes.RootID, "Projects")
	child := s.seed(t, s.Alice, projects, "child")

	doc := s.Get(t, s.Alice, "/notes/"+itoa(child))
	links := doc.QueryAll("nav.outline-crumbs a")
	if len(links) < 2 {
		t.Fatalf("got %d breadcrumb links, want at least 2", len(links))
	}
	ancestor := links[1]
	if got, _ := htmlassert.Attr(ancestor, "hx-get"); got != "/notes/"+itoa(projects) {
		t.Errorf("ancestor crumb hx-get = %q, want /notes/%d", got, projects)
	}
	if got, _ := htmlassert.Attr(ancestor, "hx-target"); got != "#outline" {
		t.Errorf("ancestor crumb hx-target = %q, want #outline", got)
	}
	if got, _ := htmlassert.Attr(ancestor, "hx-swap"); got != "innerHTML transition:true" {
		t.Errorf("ancestor crumb hx-swap = %q, want %q", got, "innerHTML transition:true")
	}
	if got, _ := htmlassert.Attr(ancestor, "hx-push-url"); got != "true" {
		t.Errorf("ancestor crumb hx-push-url = %q, want true", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/... -run 'HTMXTransition' -v`
Expected: FAIL — all three tests fail on the `hx-get`/`hx-target`/`hx-swap`/
`hx-push-url` assertions (empty string, since the attributes don't exist
yet).

- [ ] **Step 3: Wire the "All notes" link**

In `internal/apps/notes/templates/outline.html`, change line 104 from:

```html
		<a href="/notes/">All notes</a>
```

to:

```html
		<a href="/notes/" hx-get="/notes/" hx-target="#outline"
		   hx-swap="innerHTML transition:true" hx-push-url="true">All notes</a>
```

- [ ] **Step 4: Wire the ancestor breadcrumb link**

Change line 110 from:

```html
		<a href="/notes/{{.ID}}" title="{{.DisplayTitle}}">{{.DisplayTitle}}</a>
```

to:

```html
		<a href="/notes/{{.ID}}" title="{{.DisplayTitle}}"
		   hx-get="/notes/{{.ID}}" hx-target="#outline"
		   hx-swap="innerHTML transition:true" hx-push-url="true">{{.DisplayTitle}}</a>
```

- [ ] **Step 5: Wire the bullet dot zoom-in link**

Change lines 328-331 from:

```html
				<a class="outline-dot{{if .Collapsed}} outline-dot-full{{end}}"
				   href="/notes/{{.ID}}"
				   draggable="false"
				   aria-label="Zoom in to {{.DisplayTitle}}"><svg class="icon-dot" width="16" height="16" viewBox="0 0 16 16">{{if .Collapsed}}<circle class="icon-dot-halo" cx="8" cy="8" r="8"/><circle cx="8" cy="8" r="4" fill="currentColor"/>{{else}}<circle cx="8" cy="8" r="4" fill="currentColor"/>{{end}}</svg></a>
```

to:

```html
				<a class="outline-dot{{if .Collapsed}} outline-dot-full{{end}}"
				   href="/notes/{{.ID}}"
				   hx-get="/notes/{{.ID}}" hx-target="#outline"
				   hx-swap="innerHTML transition:true" hx-push-url="true"
				   draggable="false"
				   aria-label="Zoom in to {{.DisplayTitle}}"><svg class="icon-dot" width="16" height="16" viewBox="0 0 16 16">{{if .Collapsed}}<circle class="icon-dot-halo" cx="8" cy="8" r="8"/><circle cx="8" cy="8" r="4" fill="currentColor"/>{{else}}<circle cx="8" cy="8" r="4" fill="currentColor"/>{{end}}</svg></a>
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -run 'HTMXTransition' -v`
Expected: PASS (all three tests)

- [ ] **Step 7: Run the full notes package test suite to check for regressions**

Run: `go test ./internal/apps/notes/... -race -count=1 -v`
Expected: PASS — in particular `TestBulletDotZoomsIn`,
`TestBreadcrumbAncestorLinksStayPlainText`, `TestTopLevelHasNoBreadcrumb`,
`TestZoomingIntoAnotherUsersNodeIs404`, `TestZoomingIntoNonsenseIs404`, and
every indent/outdent/done/archive/delete/move/new test must still pass
unchanged, confirming those links were not touched.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/notes/templates/outline.html internal/apps/notes/handlers_test.go
git commit -m "feat(notes): wire zoom links through htmx for a view transition"
```

---

### Task 1.5: OOB-swap the breadcrumb and heading on an htmx zoom

> Added after Task 1 and Task 2 were implemented and reviewed: Task 2's live
> browser verification surfaced a real functional gap that neither the spec
> nor Task 1's task-scoped review caught. `outline.html`'s own doc comment
> (`internal/apps/notes/templates/outline.html:135-138`, unchanged before
> this task) states the breadcrumb and heading "never change as a side
> effect of a structural op" — true before Task 1, because no structural
> route ever changed which node the page was zoomed to. Task 1 made the zoom
> links themselves reach that same shared code path
> (`renderOutlineFragment`), which was never built to update the breadcrumb
> or `<h1>` — so after Task 1 alone, an htmx zoom updates the URL and the row
> list but leaves the breadcrumb trail and heading showing the previous
> location. This task closes that gap using the same out-of-band swap
> pattern already used for the toolbar's show-completed toggle and due badge
> in this same response.

**Files:**
- Modify: `internal/apps/notes/templates/outline.html:100-133` (extract the
  existing zoomed/unzoomed breadcrumb+banners+heading block into a
  `{{define "outline-heading"}}` template, wrapped by a stable
  `id="outline-heading"` container)
- Modify: `internal/apps/notes/templates/outline.html:32` (`outline-swap`
  definition, to add the OOB heading block)
- Modify: `internal/apps/notes/handlers.go:187-227` (`renderOutlineFragment`,
  to compute `Root`/`Zoomed`/`Crumbs`/`ShareURL` the same way
  `renderOutline` already does at `handlers.go:118-137`)
- Test: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `a.store.ByID(ctx, userID, rootID) (Node, error)` and
  `a.store.Ancestors(ctx, userID, rootID) ([]Node, error)` (both already
  used by `renderOutline`, `handlers.go:122-127`); `a.fail(w, r, err)` for
  the `ByID` error path (`handlers.go:41-50`, maps `ErrNotFound` to 404);
  `outlineView` fields `Root Node`, `Zoomed bool`, `Crumbs []Node`,
  `ShareURL string` (`view.go:6-45`, all pre-existing, unchanged shapes).
- Produces: a `<div id="outline-heading">` wrapper, present in both the
  full-page render and every htmx fragment response (as an OOB swap in the
  fragment case), so later code can rely on that id existing exactly once
  per rendered page.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/notes/handlers_test.go` (near the existing zoom
tests):

```go
// TestZoomingViaHTMXUpdatesTheBreadcrumb is the regression test for the gap
// Task 2's live verification found: an htmx zoom must refresh the
// breadcrumb/heading via OOB swap, not just the row list.
func TestZoomingViaHTMXUpdatesTheBreadcrumb(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.Alice, notes.RootID, "Projects")
	child := s.seed(t, s.Alice, parent, "Sub-project")

	req := httptest.NewRequest("GET", "/notes/"+itoa(child), nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="outline-heading" hx-swap-oob="true"`) {
		t.Error("htmx zoom response is missing the OOB heading swap")
	}
	if !strings.Contains(body, "Sub-project") {
		t.Error("OOB heading does not show the new zoom root's title")
	}
	if !strings.Contains(body, "Projects") {
		t.Error("OOB heading does not show the new zoom root's ancestor crumb")
	}
}

// TestZoomingOutViaHTMXShowsTheUnzoomedHeading covers the other direction:
// zooming back to the top level over htmx must replace the OOB heading with
// the plain "Notes" title, not leave the previous zoom's heading in place.
func TestZoomingOutViaHTMXShowsTheUnzoomedHeading(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "Projects")

	req := httptest.NewRequest("GET", "/notes/", nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="outline-heading" hx-swap-oob="true"`) {
		t.Error("htmx zoom-out response is missing the OOB heading swap")
	}
	if !strings.Contains(body, "<h1>Notes</h1>") {
		t.Error("OOB heading does not show the unzoomed title")
	}
	_ = id
}

// TestMutationFragmentAlsoCarriesTheHeadingUnchanged is the counterpart to
// the existing TestMutationFragmentCarriesTheToggleUnchanged: a structural
// op must still emit the OOB heading (so the pattern is uniform across
// every fragment response), and it must describe the same root the
// request came in on, not some other one.
func TestMutationFragmentAlsoCarriesTheHeadingUnchanged(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.Alice, notes.RootID, "Projects")
	child := s.seed(t, s.Alice, parent, "a")

	req := httptest.NewRequest("POST", "/notes/"+itoa(child)+"/indent",
		strings.NewReader(url.Values{"root": {itoa(parent)}, web.CSRFFormField: {s.CSRFToken(t, s.Alice)}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)

	body := rec.Body.String()
	if !strings.Contains(body, `id="outline-heading" hx-swap-oob="true"`) {
		t.Error("structural-op fragment is missing the OOB heading swap")
	}
	if !strings.Contains(body, "Projects") {
		t.Error("OOB heading does not describe the root the request came in on")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/... -run 'HeadingUnchanged|UpdatesTheBreadcrumb|ShowsTheUnzoomedHeading' -v`
Expected: FAIL — none of the three responses currently contain
`id="outline-heading"` at all (that id doesn't exist yet).

- [ ] **Step 3: Extract the heading block into its own template**

In `internal/apps/notes/templates/outline.html`, replace lines 102-133 (from
`{{if .Data.Zoomed}}` through the matching `{{else}}<h1>Notes</h1>{{end}}`)
with:

```html
	<div id="outline-heading">
	{{template "outline-heading" .Data}}
	</div>
```

Then add a new top-level definition (place it directly above the existing
`{{define "outline-body"}}` block) containing exactly the block you just
removed, with every `.Data.` prefix stripped (since it now receives `.Data`
directly as its dot):

```html
{{define "outline-heading"}}
{{if .Zoomed}}
<nav class="outline-crumbs" aria-label="Outline breadcrumb">
	<a href="/notes/" hx-get="/notes/" hx-target="#outline"
	   hx-swap="innerHTML transition:true" hx-push-url="true">All notes</a>
	{{range .Crumbs}}
	<span aria-hidden="true">/</span>
	{{/* Stays on DisplayTitle, not DisplayTitleHTML: this IS a link, so a
	     title containing its own link/autolink/#tag would nest anchors —
	     issue #65's own text on the tension. */}}
	<a href="/notes/{{.ID}}" title="{{.DisplayTitle}}"
	   hx-get="/notes/{{.ID}}" hx-target="#outline"
	   hx-swap="innerHTML transition:true" hx-push-url="true">{{.DisplayTitle}}</a>
	{{end}}
	<span aria-hidden="true">/</span>
	{{/* Not a link — see DisplayTitleHTML's own doc comment (issue #65) —
	     so rendering the title's Markdown here can't nest an <a>. */}}
	<span class="outline-crumb-current" title="{{.Root.DisplayTitle}}">{{.Root.DisplayTitleHTML}}</span>
</nav>
{{if .Root.Archived}}
<p class="notes-archived-banner">This bullet is archived. Its still-visible children can be edited here, but consider <a href="/notes/archive">restoring it</a> first.</p>
{{end}}
{{if .Root.Shared}}
<div class="notice row">
	<span>Anyone with this link can read this bullet and its subtree: <a href="{{.ShareURL}}">{{.ShareURL}}</a></span>
	<button type="button" class="button" data-copy-link="{{.ShareURL}}">Copy link</button>
</div>
{{end}}
<h1>{{.Root.DisplayTitleHTML}}</h1>
{{with .Root.Note}}<p class="dim">{{.}}</p>{{end}}
{{else}}
<h1>Notes</h1>
{{end}}
{{end}}
```

- [ ] **Step 4: Add the OOB heading to the fragment response**

Change line 32 (the `outline-swap` definition) from:

```html
{{define "outline-swap"}}{{template "outline-body" .}}{{template "show-completed-toggle" .}}{{template "due-badge" .}}{{end}}
```

to:

```html
{{define "outline-swap"}}<div id="outline-heading" hx-swap-oob="true">{{template "outline-heading" .}}</div>{{template "outline-body" .}}{{template "show-completed-toggle" .}}{{template "due-badge" .}}{{end}}
```

(`outline-swap`'s caller is `renderOutlineFragment`, which is always an
htmx/OOB response — never the full-page render — so `hx-swap-oob="true"`
here is unconditional, exactly like the existing `show-completed-toggle`/
`due-badge` OOB blocks it now sits beside.)

- [ ] **Step 5: Populate Root/Zoomed/Crumbs/ShareURL in renderOutlineFragment**

In `internal/apps/notes/handlers.go`, change `renderOutlineFragment`
(currently lines 187-227) from:

```go
func (a *App) renderOutlineFragment(w http.ResponseWriter, r *http.Request, userID, rootID int64, showCompleted bool) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	flat, err := a.store.Outline(r.Context(), userID, rootID, showCompleted, query != "")
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	dueRows, err := a.store.Due(r.Context(), userID, "")
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
```

to:

```go
func (a *App) renderOutlineFragment(w http.ResponseWriter, r *http.Request, userID, rootID int64, showCompleted bool) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	view := outlineView{
		CSRFToken:     web.CSRFToken(r.Context()),
		Root:          Node{ID: rootID},
		ShowCompleted: showCompleted,
		Query:         query,
		OOB:           true,
	}
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
	}

	flat, err := a.store.Outline(r.Context(), userID, rootID, showCompleted, query != "")
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	dueRows, err := a.store.Due(r.Context(), userID, "")
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	visible := hideDone(flat, showCompleted)
	view.DueCount = DueBadgeCount(dueRows, time.Now())
```

(the rest of the function — `matched`/`view.HiddenCount`/`view.Rows`/the
final `a.deps.Render.Fragment` call — is unchanged; only reorder so `view`
exists before the new `if rootID != RootID` block populates it, and drop
the old struct-literal fields now set above instead:
`ShowCompleted`/`Query`/`OOB` stay in the literal, `DueCount` moves to the
assignment shown above since it's computed after `dueRows`, same as
before).

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -run 'HeadingUnchanged|UpdatesTheBreadcrumb|ShowsTheUnzoomedHeading' -v`
Expected: PASS (all three tests)

- [ ] **Step 7: Run the full notes package test suite to check for regressions**

Run: `go test ./internal/apps/notes/... -race -count=1 -v`
Expected: PASS — in particular every existing zoom, breadcrumb, search-filter,
and structural-mutation test (`TestZoomingIntoAnotherUsersNodeIs404`,
`TestBreadcrumbAncestorLinksStayPlainText`, `TestOutlineFilterOverHTMXRendersOnlyTheFragment`,
`TestMutationFragmentCarriesTheToggleUnchanged`, and the rest) must still
pass — this template refactor must not change any rendered content, only
add the new wrapper/OOB block around it.

- [ ] **Step 8: Run the full check battery**

Run: `gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go mod tidy && git diff --exit-code go.mod go.sum && go test ./... -race -count=1`
Expected: every command clean/passing, no diff from `go mod tidy`.

- [ ] **Step 9: Commit**

```bash
git add internal/apps/notes/templates/outline.html internal/apps/notes/handlers.go internal/apps/notes/handlers_test.go
git commit -m "fix(notes): keep the breadcrumb and heading in sync on an htmx zoom"
```

---

### Task 2: Add the cross-fade CSS and verify it live

**Files:**
- Modify: `internal/ui/static/app.css` (insert after line 756, the
  `.notes { max-width: 46rem; }` rule, before `.outline-crumbs`)

**Interfaces:**
- Consumes: `#outline` element id (from
  `internal/apps/notes/templates/outline.html:136`,
  `<div id="outline" class="outline">`, unchanged) and the
  `hx-swap="innerHTML transition:true"` modifier wired in Task 1 — this CSS
  has no effect without that modifier present.
- Produces: nothing consumed by later tasks — this is the terminal styling
  layer.

This task has no Go-level automated test (it's a CSS-only visual effect);
verification is the existing Go test suite staying green (proving no markup
regression) plus a manual live check in the browser, both performed as
explicit steps below.

- [ ] **Step 1: Add the cross-fade CSS**

In `internal/ui/static/app.css`, after line 756
(`.notes { max-width: 46rem; }`) and before `.outline-crumbs {`, insert:

```css

/* Cross-fade the outline when zooming in/out (row dot, breadcrumb, "All
 * notes", and browser back/forward via hx-push-url) — issue-free everywhere
 * else, since only the three zoom links carry `transition:true` on their
 * hx-swap. This is the reusable pattern for Paste/Flash/Reader: name a
 * swapped region with view-transition-name, override its
 * ::view-transition-old/new rule for the desired effect, and add
 * `transition:true` to just the hx-swap attributes that should animate. */
#outline {
	view-transition-name: outline;
}
::view-transition-old(outline),
::view-transition-new(outline) {
	animation-duration: 0.18s;
	animation-timing-function: ease;
}
@media (prefers-reduced-motion: reduce) {
	::view-transition-old(outline),
	::view-transition-new(outline) {
		animation-duration: 0.001s;
	}
}
```

- [ ] **Step 2: Run gofmt/vet/staticcheck sanity (CSS is embedded, but templates/Go must still build)**

Run: `gofmt -l . && go vet ./... && go build ./cmd/onsuite`
Expected: `gofmt -l .` prints nothing; `go vet` and the build succeed with no
output/errors.

- [ ] **Step 3: Run the full test suite**

Run: `go test ./... -race -count=1`
Expected: PASS, no regressions (this step only sanity-checks that embedding
the changed CSS didn't break anything — CSS content itself isn't asserted by
any Go test).

- [ ] **Step 4: Build a local binary for manual verification**

```bash
go build -o /tmp/onsuite-zoom-verify ./cmd/onsuite
mkdir -p /tmp/onsuite-zoom-verify-data
echo verifypass123 | /tmp/onsuite-zoom-verify user add verifyuser --admin --data-dir /tmp/onsuite-zoom-verify-data
/tmp/onsuite-zoom-verify serve --data-dir /tmp/onsuite-zoom-verify-data --addr 127.0.0.1:8099 &
```

Expected: the server starts listening on `127.0.0.1:8099`.

- [ ] **Step 5: Manually verify the transition in the browser**

Using the Browser tool:
1. Navigate to `http://127.0.0.1:8099/login`, sign in as `verifyuser` /
   `verifypass123`.
2. Go to ON Notes, create two or three top-level bullets with the toolbar's
   "new" control, and one nested child under the first bullet.
3. Click the parent bullet's dot to zoom in — confirm the outline content
   visibly cross-fades rather than cutting instantly, and the URL updates to
   `/notes/{id}`.
4. Click "All notes" in the breadcrumb — confirm the same cross-fade zooming
   back out, URL returns to `/notes/`.
5. Zoom in again, then use the browser back button, then forward — note
   whether the transition still animates on history navigation or falls back
   to an instant swap (either is acceptable per the spec's documented open
   question; record which one actually happens).
6. Confirm indent/outdent/done/collapse/delete/new remain visually instant
   (no fade) — these links were not touched in Task 1.
7. Use `resize_window` or OS-level `prefers-reduced-motion: reduce`
   emulation (`mcp__Claude_Browser__resize_window` accepts `colorScheme`
   only, not reduced-motion directly — instead use
   `mcp__Claude_Browser__javascript_tool` to confirm
   `matchMedia('(prefers-reduced-motion: reduce)')` support exists, and
   visually re-check step 3 with the OS/browser reduced-motion setting
   toggled on if the test environment allows it) — confirm the transition is
   effectively instant under reduced motion.

- [ ] **Step 6: Stop the verification server**

```bash
kill %1
rm -rf /tmp/onsuite-zoom-verify /tmp/onsuite-zoom-verify-data
```

- [ ] **Step 7: Commit**

```bash
git add internal/ui/static/app.css
git commit -m "feat(notes): cross-fade the outline on zoom via CSS view transitions"
```

---

### Task 3: Open the PR

**Files:** none (process step)

- [ ] **Step 1: Push the branch and open a PR**

```bash
git push -u origin <branch-name>
gh pr create --title "feat(notes): cross-fade transition on zoom in/out" --body "$(cat <<'EOF'
## Summary
- Wires the three zoom links (bullet dot, "All notes", ancestor breadcrumb)
  through htmx (`hx-get`/`hx-target="#outline"`/
  `hx-swap="innerHTML transition:true"`/`hx-push-url="true"`), reusing the
  existing fragment-rendering route with no server changes.
- Adds a CSS view-transition (plain opacity cross-fade, ~180ms) scoped to
  `#outline`, disabled under `prefers-reduced-motion: reduce`.
- Every other outline action (indent, outdent, done, archive, delete, move,
  new) is untouched and stays instant.

Spec: docs/superpowers/specs/2026-09-08-notes-zoom-transition-design.md

## Test plan
- [x] `go test ./... -race -count=1` passes
- [x] Manually verified zoom in/out cross-fades in the browser
- [x] Manually verified other outline actions remain instant
- [ ] Reviewer confirms the feel/timing is right
EOF
)"
```

Expected: PR opened against `main`.
