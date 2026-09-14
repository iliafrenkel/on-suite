# Reader UI Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give ON Reader the same UI polish as Notes/Paste — a resizable three-pane layout (reading pane widest, gutters draggable, widths persisted), a top toolbar (filter pills + a "..." overflow menu), a slim middle toolbar above the article list, native `<dialog>` popups instead of always-visible inline forms and `hx-confirm`, and a shared toolbar-icon set.

**Architecture:** Pure UI/chrome pass. No new routes, no domain-logic changes, no Go view-model fields. Every existing form keeps its `method`/`action`/`hx-*` attributes and CSS classes exactly as today — only *where* it renders (inline vs. inside a `<dialog>`) and how it looks changes. `internal/ui/static/app.css` moves `.reader-panes` from a fixed CSS grid to a flex row with two draggable gutters; `internal/apps/reader/static/reader.js` gains gutter-drag, menu, and dialog wiring (same single-file-per-app convention as `notes.js`); one new Go file, `internal/ui/toolbar_icons.go`, supplies a shared `ticon` template func for every new toolbar/menu/dialog icon.

**Tech Stack:** Go 1.26 (`html/template`), vanilla ES5-style JS (no build step, no framework — matches `theme.js`/`notes.js`), htmx 2.0.10, plain CSS with custom properties. Native `<dialog>` (`showModal()`/`close()`), no polyfill (repo has no browserslist/JS build step; this is a deliberate plan-level choice — evergreen-browser only, consistent with the project's existing reliance on native `<details>`, `window.confirm`, and CSS `:has`-free selectors).

## Global Constraints

- `main` is protected — this plan executes on branch `feat/reader-ui-polish` (already created off latest `main`); open a PR when done, never push to `main` directly.
- Full check must stay green on every commit (mirrors CI, see [AGENTS.md](../../../AGENTS.md)):
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- Apps never import each other; the platform never imports an app; `internal/ui` must stay a leaf (no imports beyond stdlib/`html/template`). If any task touches package-level imports, run `go test ./internal/arch/...`.
- No new dependencies (`go.mod`/`go.sum` must not change).
- No new HTTP routes, no new fields on `indexView`/`listView`/`articleView`/`treeView` (per the spec's "no domain logic changes" — every existing form's `hx-*`/`method`/`action` and every existing Go-rendered field stays exactly as it is, only markup position and CSS/JS change).
- Existing test selectors that must keep resolving after every task: `nav.reader-tree`, `section.reader-list`, `article.reader-article`, `form.reader-add`, `form.reader-add-folder`, `#feed-url`, `#folder-name`, `.reader-filters a[aria-current=page]`, `.reader-candidates form`, `.reader-mark-all button`, `.reader-article-star`, `.reader-article-read-toggle`, `#reader-panes input[name=...]`, `hx-target="#reader-panes"`.
- Resizing is a desktop-width feature only; the existing mobile breakpoints (900px hides the reading pane, 640px collapses to single-column drill-down via `#reader-list-open`/`#reader-article-open`) keep working exactly as today.

---

### Task 1: Correct the merged spec's middle-toolbar section

The approved spec ([docs/superpowers/specs/2026-09-14-on-reader-ui-polish-design.md](../specs/2026-09-14-on-reader-ui-polish-design.md), section 4) says the middle toolbar holds a folder-select control and "refresh this feed." Neither exists in the codebase: the only folder-select is inside the Add-feed form (picks a folder for a *new* subscription, not a list filter), and there is no per-feed refresh route — only `POST /reader/refresh`, which refreshes every due feed. The user confirmed (2026-09-14) the middle toolbar should hold only **Mark all read**, the one action that is already per-list-scoped today. This task fixes the spec doc to match before implementation starts, so the design doc and the code never disagree.

**Files:**
- Modify: `docs/superpowers/specs/2026-09-14-on-reader-ui-polish-design.md`

- [ ] **Step 1: Replace the inaccurate section 4**

Replace the existing "## 4. Middle toolbar (above the article list pane)" section with:

```markdown
## 4. Middle toolbar (above the article list pane)

Only one action here: **Mark all read** for the currently open list, restyled
as a compact `.toolbar-btn` icon button. (Two ideas originally listed here —
a folder-select and a per-feed refresh — don't correspond to anything in the
codebase: the only folder-select today picks a folder for a *new*
subscription, and there is no per-feed refresh route, only
`POST /reader/refresh`, which refreshes every due feed. Corrected 2026-09-14,
before implementation, once the mismatch surfaced during planning.)
```

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/specs/2026-09-14-on-reader-ui-polish-design.md
git commit -m "docs(reader): correct middle-toolbar section to match the actual codebase"
```

---

### Task 2: Shared toolbar icon set

**Files:**
- Create: `internal/ui/toolbar_icons.go`
- Create: `internal/ui/toolbar_icons_test.go`
- Modify: `internal/platform/render/render.go:104-112`

**Interfaces:**
- Produces: `ui.ToolbarIconFor(name string) template.HTML`, exposed to every template as the func `ticon` (e.g. `{{ticon "plus"}}`). Unknown names return an empty string with no panic — a good default matches `ui.IconFor`'s own fallback-not-panic behavior.

- [ ] **Step 1: Write the failing test**

```go
// internal/ui/toolbar_icons_test.go
package ui_test

import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/ui"
)

func TestToolbarIconForKnownNames(t *testing.T) {
	names := []string{
		"more", "plus", "refresh", "import", "export", "folder",
		"keyboard", "stats", "close", "check", "star-filled",
		"star-outline", "external", "doc",
	}
	seen := map[string]bool{}
	for _, name := range names {
		got := string(ui.ToolbarIconFor(name))
		if !strings.Contains(got, "<svg") {
			t.Errorf("ToolbarIconFor(%q) = %q, want it to contain <svg", name, got)
		}
		if !strings.Contains(got, `class="toolbar-icon"`) {
			t.Errorf("ToolbarIconFor(%q) is missing class=\"toolbar-icon\"", name)
		}
		if seen[got] {
			t.Errorf("ToolbarIconFor(%q) duplicates an earlier icon", name)
		}
		seen[got] = true
	}
}

func TestToolbarIconForUnknownNameIsEmpty(t *testing.T) {
	if got := ui.ToolbarIconFor("no-such-icon"); got != "" {
		t.Errorf("ToolbarIconFor(unknown) = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/... -run TestToolbarIcon -v`
Expected: FAIL — `undefined: ui.ToolbarIconFor`

- [ ] **Step 3: Write the implementation**

```go
// internal/ui/toolbar_icons.go
package ui

// Toolbar/menu/dialog icon lookup, shared by any app's toolbar buttons —
// today only ON Reader, but the map lives here (rather than in
// internal/apps/reader) so a later pass can point Notes/Paste's own inline
// toolbar SVGs at it too without duplicating markup (see PATTERNS.md).
//
// Each entry is a complete, ready-to-use <svg class="toolbar-icon" ...> —
// unlike IconFor's bare tile icons, these already carry the class that
// app.css's shared `.toolbar-icon` rule (size, stroke, currentColor) keys
// off, so a template just does {{ticon "plus"}} with nothing else to add.

import "html/template"

var toolbarIcons = map[string]template.HTML{
	"more": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<circle cx="5" cy="12" r="1.5" fill="currentColor" stroke="none"/>
		<circle cx="12" cy="12" r="1.5" fill="currentColor" stroke="none"/>
		<circle cx="19" cy="12" r="1.5" fill="currentColor" stroke="none"/>
	</svg>`,
	"plus": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M12 5v14M5 12h14"/>
	</svg>`,
	"refresh": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M20 11a8 8 0 1 0-2.3 5.7"/>
		<path d="M20 5v6h-6"/>
	</svg>`,
	"import": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M12 4v11M8 11l4 4 4-4"/>
		<path d="M4 18h16"/>
	</svg>`,
	"export": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M12 15V4M8 8l4-4 4 4"/>
		<path d="M4 18h16"/>
	</svg>`,
	"folder": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M4 6h5l2 2h9v10H4z"/>
	</svg>`,
	"keyboard": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M3 7h18v10H3z"/>
		<path d="M6 11h.01M9 11h.01M12 11h.01M15 11h.01M18 11h.01M7 14h10"/>
	</svg>`,
	"stats": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M5 20V10M12 20V4M19 20v-7"/>
	</svg>`,
	"close": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M6 6l12 12M18 6L6 18"/>
	</svg>`,
	// Mirrors Notes' show-completed-toggle checkmark
	// (internal/apps/notes/templates/toolbar.partial.html) — same glyph,
	// same meaning ("done"), kept visually identical across apps.
	"check": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M4 12l5 5L20 6"/>
	</svg>`,
	"star-filled": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M12 4l2.4 5.8L20.6 10l-4.6 4 1.4 6.2L12 17l-5.4 3.2L8 14l-4.6-4 6.2-.2z" fill="currentColor" stroke="none"/>
	</svg>`,
	"star-outline": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M12 4l2.4 5.8L20.6 10l-4.6 4 1.4 6.2L12 17l-5.4 3.2L8 14l-4.6-4 6.2-.2z"/>
	</svg>`,
	"external": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M9 6h9v9M18 6L7 17"/>
	</svg>`,
	"doc": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M7 3h7l4 4v14H7z"/>
		<path d="M14 3v4h4"/>
	</svg>`,
}

// ToolbarIconFor returns the markup for a known toolbar/menu/dialog icon
// name, or an empty string for anything else — unlike IconFor's tile
// fallback, there is no sensible generic glyph for an unnamed action, so a
// typo'd name renders as nothing rather than a misleading placeholder.
func ToolbarIconFor(name string) template.HTML {
	if svg, ok := toolbarIcons[name]; ok {
		return svg
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/... -run TestToolbarIcon -v`
Expected: PASS

- [ ] **Step 5: Register the `ticon` template func**

In `internal/platform/render/render.go`, the `funcs` map (around line 104-112) currently reads:

```go
	r := &Renderer{
		pages: make(map[string]*template.Template),
		funcs: template.FuncMap{
			"asset":     opts.AssetURL,
			"icon":      ui.IconFor,
			"dict":      dict,
			"csrfField": func() string { return opts.CSRFFieldName },
		},
	}
```

Add one entry:

```go
	r := &Renderer{
		pages: make(map[string]*template.Template),
		funcs: template.FuncMap{
			"asset":     opts.AssetURL,
			"icon":      ui.IconFor,
			"ticon":     ui.ToolbarIconFor,
			"dict":      dict,
			"csrfField": func() string { return opts.CSRFFieldName },
		},
	}
```

- [ ] **Step 6: Run the full check and the arch test**

Run:
```bash
gofmt -l . && go vet ./... && go test ./internal/ui/... ./internal/platform/render/... ./internal/arch/... -race -count=1
```
Expected: `gofmt -l .` prints nothing; all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/toolbar_icons.go internal/ui/toolbar_icons_test.go internal/platform/render/render.go
git commit -m "feat(ui): add a shared toolbar/menu/dialog icon set"
```

---

### Task 3: Flex-based resizable pane layout in CSS

**Files:**
- Modify: `internal/ui/static/app.css:1649-1656` (`.reader-panes`)
- Modify: `internal/ui/static/app.css:1836-1853` (900px breakpoint)
- Modify: `internal/ui/static/app.css:1863-1887` (640px breakpoint)

**Interfaces:**
- Produces: `.reader-panes` (outer vertical wrapper: toolbar + banner + row), `.reader-panes-row` (the horizontal flex row that used to be `.reader-panes`'s grid), `.pane-gutter` (draggable divider), CSS custom properties `--reader-tree-w`/`--reader-list-w` read by `.reader-tree`/`.reader-list`'s `flex-basis`. Task 4's template changes and Task 7's JS both depend on these exact class/property names.

- [ ] **Step 1: Replace `.reader-panes` (lines 1649-1656)**

Old:
```css
.reader-panes {
	--reader-chrome: calc(8rem + var(--s-6) * 2);
	display: grid;
	grid-template-columns: minmax(12rem, 16rem) minmax(16rem, 24rem) 1fr;
	height: calc(100vh - var(--reader-chrome));
	min-height: 0;
	overflow: hidden;
}
```

New:
```css
/* .reader-panes is now the outer vertical stack: the top toolbar, an
 * optional error/notice banner, then .reader-panes-row — the three-pane
 * flex row that used to be this element's own CSS grid. Splitting it this
 * way (rather than making .reader-panes itself the flex row) is what lets
 * the toolbar/banner sit above the resizable row without becoming grid/flex
 * items of it themselves. */
.reader-panes {
	--reader-chrome: calc(8rem + var(--s-6) * 2);
	display: flex;
	flex-direction: column;
	height: calc(100vh - var(--reader-chrome));
	min-height: 0;
	overflow: hidden;
}

.reader-panes-row {
	display: flex;
	flex: 1 1 auto;
	min-height: 0;
	overflow: hidden;
}

.reader-tree {
	flex: 0 0 var(--reader-tree-w, 15rem);
}

.reader-list {
	flex: 0 0 var(--reader-list-w, 20rem);
}

.reader-article {
	flex: 1 1 auto;
}

/* The draggable divider between two panes. A thin hit target (8px) with a
 * narrower visible bar (2px) centered in it, the same "generous hit target,
 * modest visible line" trick a native OS window splitter uses. */
.pane-gutter {
	flex: 0 0 8px;
	position: relative;
	cursor: col-resize;
	background: transparent;
}

.pane-gutter::after {
	content: "";
	position: absolute;
	inset: 0 3px;
	border-radius: 2px;
	background: var(--c-border);
}

.pane-gutter:hover::after,
.pane-gutter.is-dragging::after {
	background: var(--c-accent);
}

.pane-gutter:focus-visible {
	outline: var(--ring);
	outline-offset: -2px;
}
```

- [ ] **Step 2: Update the 900px breakpoint (lines 1836-1853)**

Old:
```css
@media (max-width: 900px) {
	.reader-panes {
		grid-template-columns: minmax(10rem, 14rem) 1fr;
	}
	.reader-article {
		display: none;
	}
	#reader-article-open:checked ~ .reader-list {
		display: none;
	}
	#reader-article-open:checked ~ .reader-article {
		display: block;
	}
	/* Only reachable — and only needed — once the list is covered up. */
	#reader-article-open:checked ~ .reader-article .reader-back-btn {
		display: inline-flex;
	}
}
```

New (adds gutter-hiding and a fixed tree width; `.reader-panes` no longer has `grid-template-columns` to override, so that line is replaced by the flex equivalent, and it stays in this block since 640px doesn't need to repeat it — the 640px query is more specific and comes later in the file, so its rules apply on top of whatever this block already set for widths that width also matches):
```css
@media (max-width: 900px) {
	.pane-gutter {
		display: none;
	}
	.reader-tree {
		flex: 0 0 12rem;
	}
	.reader-list {
		flex: 1 1 auto;
	}
	.reader-article {
		display: none;
	}
	#reader-article-open:checked ~ .reader-list {
		display: none;
	}
	#reader-article-open:checked ~ .reader-article {
		display: block;
	}
	/* Only reachable — and only needed — once the list is covered up. */
	#reader-article-open:checked ~ .reader-article .reader-back-btn {
		display: inline-flex;
	}
}
```

- [ ] **Step 3: Update the 640px breakpoint (lines 1863-1887)**

Old:
```css
@media (max-width: 640px) {
	.reader-panes {
		/* main's padding drops to --s-4 at this width. */
		--reader-chrome: calc(8rem + var(--s-4) * 2);
		grid-template-columns: 1fr;
	}
	/* One pane means the list is hidden until it is drilled into. Without
	 * this the "back to the feed tree" state showed the tree with the list
	 * still stacked under it. */
	.reader-list {
		display: none;
	}
	#reader-list-open:checked ~ .reader-tree {
		display: none;
	}
	#reader-list-open:checked ~ .reader-list {
		display: block;
	}
	#reader-list-open:checked ~ .reader-list .reader-back-btn {
		display: inline-flex;
	}
	#reader-article-open:checked ~ .reader-list {
		display: none;
	}
}
```

New (drops `grid-template-columns: 1fr`, since a lone visible flex item already fills the row; adds `flex-basis: 100%` for the tree, matching what was implicit under grid):
```css
@media (max-width: 640px) {
	.reader-panes {
		/* main's padding drops to --s-4 at this width. */
		--reader-chrome: calc(8rem + var(--s-4) * 2);
	}
	.reader-tree {
		flex: 1 1 100%;
	}
	/* One pane means the list is hidden until it is drilled into. Without
	 * this the "back to the feed tree" state showed the tree with the list
	 * still stacked under it. */
	.reader-list {
		display: none;
	}
	#reader-list-open:checked ~ .reader-tree {
		display: none;
	}
	#reader-list-open:checked ~ .reader-list {
		display: block;
	}
	#reader-list-open:checked ~ .reader-list .reader-back-btn {
		display: inline-flex;
	}
	#reader-article-open:checked ~ .reader-list {
		display: none;
	}
}
```

- [ ] **Step 4: Add toolbar/menu/dialog/banner CSS**

Add this block immediately after the `.pane-gutter:focus-visible` rule from Step 1 (still above the `@media (max-width: 900px)` block, so it's in the same unconditional section as the rest of `.reader-panes`'s base styles):

```css
.reader-toolbar {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--s-2);
	padding: var(--s-2) var(--s-3);
	border-bottom: 1px solid var(--c-border);
}

.reader-filters {
	display: flex;
	gap: var(--s-1);
}

.reader-banner {
	padding: 0 var(--s-3);
	padding-top: var(--s-2);
}

.reader-banner .notice {
	margin-bottom: var(--s-2);
}

.reader-menu {
	position: relative;
}

.reader-menu-list {
	position: absolute;
	right: 0;
	top: calc(100% + var(--s-1));
	z-index: 20;
	display: flex;
	flex-direction: column;
	min-width: 12rem;
	padding: var(--s-1);
	background: var(--c-bg);
	border: var(--border);
	border-radius: var(--radius);
	box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12);
}

.reader-menu-item,
.reader-menu-item-form {
	display: block;
}

.reader-menu-item {
	display: flex;
	align-items: center;
	gap: var(--s-2);
	width: 100%;
	padding: var(--s-2);
	border: none;
	border-radius: var(--radius);
	background: none;
	color: var(--c-text);
	font-size: var(--fs-sm);
	text-decoration: none;
	text-align: left;
	cursor: pointer;
}

.reader-menu-item:hover {
	background: var(--c-bg-subtle);
}

.reader-list-toolbar {
	display: flex;
	justify-content: flex-end;
	padding: var(--s-2) var(--s-3) 0;
}

.reader-dialog {
	position: relative;
	width: min(28rem, calc(100vw - var(--s-6)));
	padding: var(--s-4);
	border: var(--border);
	border-radius: var(--radius);
	color: var(--c-text);
	background: var(--c-bg);
}

.reader-dialog::backdrop {
	background: rgba(0, 0, 0, 0.35);
}

.reader-dialog h2 {
	margin-top: 0;
}

.reader-dialog-close {
	position: absolute;
	top: var(--s-3);
	right: var(--s-3);
	padding: var(--s-1);
	border: none;
	background: none;
	color: var(--c-text-dim);
	cursor: pointer;
}

.dialog-actions {
	display: flex;
	justify-content: flex-end;
	gap: var(--s-2);
	margin-top: var(--s-4);
}
```

- [ ] **Step 5: Verify no stray CSS references remain**

Run:
```bash
grep -n "grid-template-columns" internal/ui/static/app.css | grep -i reader
```
Expected: no output (every `.reader-panes` grid reference is gone).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/static/app.css
git commit -m "feat(reader): flex-based resizable pane layout, toolbar/menu/dialog CSS"
```

---

### Task 4: Restructure `panes.partial.html` — toolbar, banner, gutters

This is a template-only restructuring: no Go field is added, removed, or renamed. `.Error`/`.Notice` move from being displayed inside the add-feed form (their only display site today) to a page-level banner — the same fields, the same `{{if .Error}}`/`{{if .Notice}}` guards, just rendered in one shared place instead of inside one specific form. The `.reader-filters` nav moves from inside `section.reader-list`'s header to the new top toolbar; its markup, classes, and `aria-current` logic are otherwise untouched.

**Files:**
- Modify: `internal/apps/reader/templates/panes.partial.html`
- Modify: `internal/apps/reader/handlers_test.go` (new assertions only — no existing test changes)

**Interfaces:**
- Produces: `{{define "reader-toolbar"}}` (new, takes `listView` as `.`), `#reader-panes-row` (new id, the flex row), `.pane-gutter` elements with `data-gutter-for="tree"|"list"` (consumed by Task 7's JS). `{{define "panes"}}` still receives `indexView` as `.` and is still targeted by every `hx-target="#reader-panes"` form.
- Consumes: `ui.ToolbarIconFor` via the `{{ticon "name"}}` func from Task 2.

- [ ] **Step 1: Replace `{{define "panes"}}` (lines 54-69)**

Old:
```html
{{define "panes"}}
<div class="reader-panes" id="reader-panes">
  {{/* Two hidden checkboxes drive the narrow-viewport drill-down, the same
       technique #paste-detail-open uses. They are siblings of the three panes
       so the CSS can reach the panes with ~, and the server sets them: a list
       shown checks the first, an open article the second. The back labels
       inside the list and article panes uncheck them again. */}}
  <input type="checkbox" id="reader-list-open" class="visually-hidden"
         {{if .List.Selected}}checked{{end}} aria-hidden="true" tabindex="-1">
  <input type="checkbox" id="reader-article-open" class="visually-hidden"
         {{if .Article.Selected}}checked{{end}} aria-hidden="true" tabindex="-1">
  {{template "tree" .}}
  {{template "list" .List}}
  {{template "article" .Article}}
</div>
{{end}}
```

New:
```html
{{define "panes"}}
<div class="reader-panes" id="reader-panes">
  {{template "reader-toolbar" .List}}
  {{if or .Error .Notice}}
  <div class="reader-banner">
    {{if .Error}}<p class="notice notice-error" role="alert">{{.Error}}</p>{{end}}
    {{if .Notice}}<p class="notice" role="status">{{.Notice}}</p>{{end}}
  </div>
  {{end}}
  <div class="reader-panes-row" id="reader-panes-row">
    {{/* Two hidden checkboxes drive the narrow-viewport drill-down, the same
         technique #paste-detail-open uses. They are siblings of the three
         panes (and now the gutters) so the CSS can reach the panes with ~,
         and the server sets them: a list shown checks the first, an open
         article the second. The back labels inside the list and article
         panes uncheck them again. */}}
    <input type="checkbox" id="reader-list-open" class="visually-hidden"
           {{if .List.Selected}}checked{{end}} aria-hidden="true" tabindex="-1">
    <input type="checkbox" id="reader-article-open" class="visually-hidden"
           {{if .Article.Selected}}checked{{end}} aria-hidden="true" tabindex="-1">
    {{template "tree" .}}
    <div class="pane-gutter" data-gutter-for="tree" role="separator"
         aria-orientation="vertical" aria-label="Resize the feed list" tabindex="0"></div>
    {{template "list" .List}}
    <div class="pane-gutter" data-gutter-for="list" role="separator"
         aria-orientation="vertical" aria-label="Resize the reading pane" tabindex="0"></div>
    {{template "article" .Article}}
  </div>
  {{template "reader-dialogs" .}}
</div>
{{end}}
```

(`{{template "reader-dialogs" .}}` is a forward reference to Task 5's define — leaving it in place now means Task 5 only has to add the define, not touch `panes` again.)

- [ ] **Step 2: Add the `reader-toolbar` define**

Add immediately before `{{define "tree"}}` (i.e. right after the new `panes` define from Step 1):

```html
{{/* reader-toolbar spans the full width above all three panes: the filter
     pills (moved here from the list pane's own header — same classes, same
     .Filter/.BasePath/.Query logic, just relocated) on the left, and the
     "..." overflow menu — everything that used to be always-visible forms
     and links inside the tree pane — on the right. Receives .List
     (listView) as its root, exactly as "list" itself does, since every
     field it needs (BasePath, Filter, Query, Shell, Scope, SubID) already
     lives there. */}}
{{define "reader-toolbar"}}
<div class="reader-toolbar">
  <nav class="reader-filters" aria-label="Filter">
    <a href="{{.BasePath}}?filter=unread{{if .Query}}&amp;q={{.Query}}{{end}}" hx-get="{{.BasePath}}?filter=unread{{if .Query}}&amp;q={{.Query}}{{end}}"
       hx-target="#reader-panes" hx-swap="outerHTML" hx-push-url="true"
       class="toolbar-btn{{if eq .Filter "unread"}} toolbar-btn-active{{end}}"
       {{if eq .Filter "unread"}}aria-current="page"{{end}}>Unread</a>
    <a href="{{.BasePath}}?filter=starred{{if .Query}}&amp;q={{.Query}}{{end}}" hx-get="{{.BasePath}}?filter=starred{{if .Query}}&amp;q={{.Query}}{{end}}"
       hx-target="#reader-panes" hx-swap="outerHTML" hx-push-url="true"
       class="toolbar-btn{{if eq .Filter "starred"}} toolbar-btn-active{{end}}"
       {{if eq .Filter "starred"}}aria-current="page"{{end}}>Starred</a>
    <a href="{{.BasePath}}?filter=all{{if .Query}}&amp;q={{.Query}}{{end}}" hx-get="{{.BasePath}}?filter=all{{if .Query}}&amp;q={{.Query}}{{end}}"
       hx-target="#reader-panes" hx-swap="outerHTML" hx-push-url="true"
       class="toolbar-btn{{if eq .Filter "all"}} toolbar-btn-active{{end}}"
       {{if eq .Filter "all"}}aria-current="page"{{end}}>All</a>
  </nav>
  <div class="reader-menu">
    <button type="button" class="toolbar-btn reader-menu-toggle" aria-haspopup="true"
            aria-expanded="false" aria-controls="reader-menu-list">
      {{ticon "more"}}<span class="visually-hidden">More actions</span>
    </button>
    <div class="reader-menu-list" id="reader-menu-list" role="menu" hidden>
      <button type="button" class="reader-menu-item" data-open-dialog="add-feed-dialog" role="menuitem">{{ticon "plus"}}Add feed…</button>
      <button type="button" class="reader-menu-item" data-open-dialog="import-opml-dialog" role="menuitem">{{ticon "import"}}Import OPML…</button>
      <a class="reader-menu-item" href="/reader/opml" download role="menuitem">{{ticon "export"}}Export OPML</a>
      <button type="button" class="reader-menu-item" data-open-dialog="new-folder-dialog" role="menuitem">{{ticon "folder"}}New folder…</button>
      <form method="post" action="/reader/refresh" class="reader-menu-item-form"
            hx-post="/reader/refresh" hx-target="#reader-panes" hx-swap="outerHTML">
        {{template "reader-ctx" .}}
        <button type="submit" class="reader-menu-item reader-refresh" role="menuitem">{{ticon "refresh"}}Refresh all feeds</button>
      </form>
      <a class="reader-menu-item" href="/reader/stats" role="menuitem">{{ticon "stats"}}Reading stats</a>
      <button type="button" class="reader-menu-item" data-open-dialog="shortcuts-dialog" role="menuitem">{{ticon "keyboard"}}Keyboard shortcuts</button>
    </div>
  </div>
</div>
{{end}}
```

- [ ] **Step 3: Remove the filter nav from `{{define "list"}}`'s header**

In the `list` define, remove this block (currently right after the `reader-back` call and `<h2>`):

```html
    <nav class="reader-filters" aria-label="Filter">
      <a href="{{.BasePath}}?filter=unread{{if .Query}}&amp;q={{.Query}}{{end}}" hx-get="{{.BasePath}}?filter=unread{{if .Query}}&amp;q={{.Query}}{{end}}"
         hx-target="#reader-panes" hx-swap="outerHTML" hx-push-url="true"
         class="{{if eq .Filter "unread"}}is-active{{end}}"
         {{if eq .Filter "unread"}}aria-current="page"{{end}}>Unread</a>
      <a href="{{.BasePath}}?filter=starred{{if .Query}}&amp;q={{.Query}}{{end}}" hx-get="{{.BasePath}}?filter=starred{{if .Query}}&amp;q={{.Query}}{{end}}"
         hx-target="#reader-panes" hx-swap="outerHTML" hx-push-url="true"
         class="{{if eq .Filter "starred"}}is-active{{end}}"
         {{if eq .Filter "starred"}}aria-current="page"{{end}}>Starred</a>
      <a href="{{.BasePath}}?filter=all{{if .Query}}&amp;q={{.Query}}{{end}}" hx-get="{{.BasePath}}?filter=all{{if .Query}}&amp;q={{.Query}}{{end}}"
         hx-target="#reader-panes" hx-swap="outerHTML" hx-push-url="true"
         class="{{if eq .Filter "all"}}is-active{{end}}"
         {{if eq .Filter "all"}}aria-current="page"{{end}}>All</a>
    </nav>
```

(Task 6 replaces the rest of this header block; leaving the header otherwise as-is for now keeps this task focused on the toolbar/banner/gutter move.)

- [ ] **Step 4: Write new assertions in `handlers_test.go`**

Add this test (anywhere alongside the other `Test...` funcs, e.g. after `TestPlainGetOfAnItemRendersAWholePage`):

```go
// TestTopToolbarHoldsFiltersAndMenu pins the UI-polish move of the filter
// pills out of the list pane's own header into a page-level toolbar, and
// the new "..." overflow menu that replaced the tree pane's always-visible
// forms/links.
func TestTopToolbarHoldsFiltersAndMenu(t *testing.T) {
	s := newServer(t)
	doc := htmlassert.Parse(t, s.Get(t, s.Alice, "/reader/").Body.String())

	doc.MustHave(".reader-toolbar")
	doc.MustHave(".reader-toolbar .reader-filters")
	active := doc.MustHave(".reader-filters a[aria-current=page]")
	if got := strings.TrimSpace(htmlassert.Text(active)); got != "All" {
		t.Errorf("default active filter = %q, want All", got)
	}

	for _, id := range []string{"add-feed-dialog", "new-folder-dialog", "import-opml-dialog", "shortcuts-dialog"} {
		doc.MustHave("[data-open-dialog=" + id + "]")
	}
	doc.MustHave(".reader-menu-list a[href=\"/reader/opml\"][download]")
	doc.MustHave(".reader-menu-list a[href=\"/reader/stats\"]")
}

// TestPaneGuttersSitBetweenTheThreePanes pins the resizable-layout markup:
// two gutters, each naming which pane it resizes, both inside the new
// #reader-panes-row wrapper alongside the three panes themselves.
func TestPaneGuttersSitBetweenTheThreePanes(t *testing.T) {
	s := newServer(t)
	doc := htmlassert.Parse(t, s.Get(t, s.Alice, "/reader/").Body.String())

	doc.MustHave("#reader-panes-row")
	if got := len(doc.QueryAll("#reader-panes-row .pane-gutter")); got != 2 {
		t.Fatalf("#reader-panes-row has %d .pane-gutter elements, want 2", got)
	}
	treeGutter := doc.MustHave(`.pane-gutter[data-gutter-for="tree"]`)
	if got, _ := htmlassert.Attr(treeGutter, "role"); got != "separator" {
		t.Errorf("tree gutter role = %q, want separator", got)
	}
}
```

`internal/htmlassert/htmlassert.go` only exposes `Query`/`QueryAll`/`MustHave`/`MustNotHave` as methods on `*Doc` (the whole parsed document), not on an individual `*html.Node` — so every lookup above goes through `doc.*` with a full (possibly descendant) selector, exactly as the rest of this file already does, rather than re-querying inside a previously-found node.

Add `"strings"` to the import block if it is not already imported in this file (it already is, per existing uses like `strings.TrimSpace`).

- [ ] **Step 5: Run the new tests, then the whole package**

Run:
```bash
go test ./internal/apps/reader/... -run "TestTopToolbarHoldsFiltersAndMenu|TestPaneGuttersSitBetweenTheThreePanes" -v
```
Expected: FAIL until Task 5 adds the dialog markup these tests also check (`data-open-dialog` targets) — that is expected at this point, since `{{template "reader-dialogs" .}}` from Step 1 has no define yet. Confirm the failure is specifically about the missing dialogs, not the toolbar/gutter assertions, by temporarily commenting out the `for _, id := range` loop and the two `.reader-menu-list a[...]` lines, running again, and confirming those two tests now pass; then restore the full test as written; it will pass once Task 5 lands.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/reader/templates/panes.partial.html internal/apps/reader/handlers_test.go
git commit -m "feat(reader): top toolbar (filters + menu shell), banner, resizable-pane gutters"
```

---

### Task 5: Move forms into native `<dialog>` elements

Every form below keeps its exact `method`/`action`/`hx-*` attributes and CSS class from today — this task only moves *where* each renders (from always-visible inline markup into a `<dialog>`) and adds the trigger wiring the menu buttons from Task 4 already reference via `data-open-dialog`.

**Files:**
- Modify: `internal/apps/reader/templates/panes.partial.html`
- Modify: `internal/apps/reader/handlers_test.go`

**Interfaces:**
- Produces: `{{define "reader-dialogs"}}` (new; receives `indexView` as `.`), dialogs `#add-feed-dialog`, `#new-folder-dialog`, `#import-opml-dialog`, `#shortcuts-dialog`, `#reader-confirm-dialog`. `#add-feed-dialog` carries `data-reopen` when `.Candidates` is non-empty — the only case where a dialog must reopen itself after a swap, since `.Candidates` is an unambiguous field (only the subscribe flow ever sets it), unlike the shared `.Error`/`.Notice` fields other handlers also set.
- Consumes: `.Candidates`, `.SelectedFolderID`, `.Tree.Folders` (all already on `indexView`, unchanged), `{{ticon "..."}}` from Task 2.

- [ ] **Step 1: Remove the four inline blocks from `{{define "tree"}}`**

Remove, from the `tree` define: the `<form class="reader-add" ...>` block, the `{{if .Candidates}}...{{end}}` block, the `<form class="reader-add-folder" ...>` block, the `<form class="reader-opml" ...>` block plus its neighboring `<a href="/reader/opml" download>Export OPML</a>` and `<a href="/reader/stats">Reading stats</a>` lines, the `<form ... class="reader-refresh">` refresh form, and the `<details class="reader-shortcuts">...</details>` block. What remains in `tree` after this step is just: the opening `<nav>` tag, the `<ul class="reader-nodes">` (All/Starred), the empty-state paragraph, and the folders/root subscription lists — i.e. `tree` becomes pure feed/folder navigation, nothing else.

- [ ] **Step 2: Add the `reader-dialogs` define**

Add after the `tree` define (before `{{define "list"}}`):

```html
{{/* reader-dialogs holds every native <dialog> the top toolbar's "..." menu
     opens, plus the shared delete/unsubscribe confirm dialog. Each form
     inside keeps the exact method/action/hx-* it had when it lived inline
     in the tree pane — only its container changed. Receives the whole
     indexView as "." (same root "panes" itself has), since the add-feed
     dialog needs .Candidates/.SelectedFolderID/.Tree.Folders and the others
     need .List for reader-ctx. */}}
{{define "reader-dialogs"}}
<dialog id="add-feed-dialog" class="reader-dialog"{{if .Candidates}} data-reopen{{end}}>
  <button type="button" class="reader-dialog-close" aria-label="Close">{{ticon "close"}}</button>
  {{if .Candidates}}
    <h2>Choose a feed</h2>
    <div class="reader-candidates">
      <p>That page offers more than one feed. Which one?</p>
      {{range .Candidates}}
        <form method="post" action="/reader/subscribe"
              hx-post="/reader/subscribe" hx-target="#reader-panes" hx-swap="outerHTML">
          {{template "reader-ctx" $.List}}
          <input type="hidden" name="url" value="{{.URL}}">
          {{if $.SelectedFolderID}}<input type="hidden" name="folder_id" value="{{$.SelectedFolderID}}">{{end}}
          <button type="submit">{{if .Title}}{{.Title}}{{else}}{{.URL}}{{end}}</button>
        </form>
      {{end}}
    </div>
  {{else}}
    <h2>Add a feed</h2>
    <form class="reader-add" method="post" action="/reader/subscribe"
          hx-post="/reader/subscribe" hx-target="#reader-panes" hx-swap="outerHTML">
      {{template "reader-ctx" .List}}
      <label for="feed-url">Feed address</label>
      <input id="feed-url" name="url" type="url" placeholder="https://example.com/feed.xml" required>
      <label for="feed-folder">Folder</label>
      <select id="feed-folder" name="folder_id">
        <option value="">(no folder)</option>
        {{range .Tree.Folders}}
          <option value="{{.ID}}">{{.Name}}</option>
        {{end}}
      </select>
      <div class="dialog-actions">
        <button type="submit" class="button">Add</button>
        <button type="button" class="reader-dialog-cancel">Cancel</button>
      </div>
    </form>
  {{end}}
</dialog>

<dialog id="new-folder-dialog" class="reader-dialog">
  <button type="button" class="reader-dialog-close" aria-label="Close">{{ticon "close"}}</button>
  <h2>New folder</h2>
  <form class="reader-add-folder" method="post" action="/reader/folder"
        hx-post="/reader/folder" hx-target="#reader-panes" hx-swap="outerHTML">
    {{template "reader-ctx" .List}}
    <label for="folder-name">Folder name</label>
    <input id="folder-name" name="name" type="text" placeholder="Folder name" required>
    <div class="dialog-actions">
      <button type="submit" class="button">Create folder</button>
      <button type="button" class="reader-dialog-cancel">Cancel</button>
    </div>
  </form>
</dialog>

<dialog id="import-opml-dialog" class="reader-dialog">
  <button type="button" class="reader-dialog-close" aria-label="Close">{{ticon "close"}}</button>
  <h2>Import OPML</h2>
  <form class="reader-opml" method="post" action="/reader/opml"
        enctype="multipart/form-data"
        hx-post="/reader/opml" hx-encoding="multipart/form-data"
        hx-target="#reader-panes" hx-swap="outerHTML">
    {{template "reader-ctx" .List}}
    <label for="opml-file">OPML file</label>
    <input id="opml-file" name="file" type="file" accept=".opml,.xml,text/xml,text/x-opml" required>
    <div class="dialog-actions">
      <button type="submit" class="button">Import</button>
      <button type="button" class="reader-dialog-cancel">Cancel</button>
    </div>
  </form>
</dialog>

<dialog id="shortcuts-dialog" class="reader-dialog">
  <button type="button" class="reader-dialog-close" aria-label="Close">{{ticon "close"}}</button>
  <h2>Keyboard shortcuts</h2>
  <dl>
    <dt>j / k</dt><dd>next / previous article</dd>
    <dt>o / Enter</dt><dd>open</dd>
    <dt>m</dt><dd>mark read or unread</dd>
    <dt>s</dt><dd>star</dd>
    <dt>r</dt><dd>refresh feeds</dd>
    <dt>/</dt><dd>search</dd>
  </dl>
</dialog>

{{/* Shared by every delete/unsubscribe button below, all of which already
     carry hx-confirm — reader.js intercepts htmx:confirm for anything
     inside #reader-panes and opens this instead of letting htmx fall back
     to window.confirm. Its message is filled in by JS, not by the
     template, since one dialog serves every hx-confirm button. */}}
<dialog id="reader-confirm-dialog" class="reader-dialog">
  <p id="reader-confirm-message"></p>
  <div class="dialog-actions">
    <button type="button" id="reader-confirm-ok" class="button">Confirm</button>
    <button type="button" class="reader-dialog-cancel">Cancel</button>
  </div>
</dialog>
{{end}}
```

- [ ] **Step 3: Run the tests written in Task 4, plus the existing suite**

Run:
```bash
go test ./internal/apps/reader/... -v 2>&1 | tail -80
```
Expected: `TestTopToolbarHoldsFiltersAndMenu` and `TestPaneGuttersSitBetweenTheThreePanes` now PASS. Watch specifically for these previously-passing tests, which must still PASS unchanged: `TestChooserPreservesTheSelectedFolder` (checks `.reader-candidates form`), any test asserting `form.reader-add-folder`, `#feed-url`, `#folder-name`, `nav.reader-tree`.

- [ ] **Step 4: Add a test pinning the candidates-dialog reopen flag**

Add to `handlers_test.go`, near `TestChooserPreservesTheSelectedFolder`:

```go
// TestCandidatesDialogCarriesReopenFlag pins the one case where a dialog
// must reopen itself after an outerHTML swap: when the subscribe flow comes
// back with more than one candidate feed, #add-feed-dialog must carry
// data-reopen so reader.js's htmx:afterSwap handler calls showModal() again
// — otherwise the chooser would render into a closed, invisible dialog.
func TestCandidatesDialogCarriesReopenFlag(t *testing.T) {
	s := newServer(t)
	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{"url": {"not a url"}})
	doc := htmlassert.Parse(t, rec.Body.String())
	dialog := doc.MustHave("#add-feed-dialog")
	if _, ok := htmlassert.Attr(dialog, "data-reopen"); ok {
		t.Error("add-feed-dialog carries data-reopen on a plain validation error, want it reserved for .Candidates only")
	}
}
```

Run: `go test ./internal/apps/reader/... -run TestCandidatesDialogCarriesReopenFlag -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/reader/templates/panes.partial.html internal/apps/reader/handlers_test.go
git commit -m "feat(reader): move add-feed, new-folder, import-opml, and shortcuts into dialogs"
```

---

### Task 6: Middle toolbar (Mark all read) and reading-pane icon buttons

**Files:**
- Modify: `internal/apps/reader/templates/panes.partial.html` (`list` and `article` defines)

**Interfaces:**
- Consumes: `{{ticon "..."}}` from Task 2. No Go changes — every form/button keeps its existing class, `action`, and `hx-*` attributes.

- [ ] **Step 1: Add the middle toolbar and restyle Mark all read**

In `{{define "list"}}`, the header currently (after Task 4 Step 3 removed the filter nav) reads:

```html
  <header class="reader-list-head">
    {{template "reader-back" (dict "For" "reader-list-open" "Label" "Feeds")}}
    <h2>{{.Title}}</h2>
    <form method="get" action="{{.BasePath}}" class="reader-search" role="search">
      <input type="hidden" name="filter" value="{{.Filter}}">
      <input type="search" id="reader-search-input" name="q" value="{{.Query}}"
             placeholder="Search articles…" aria-label="Search articles"
             hx-get="{{.BasePath}}" hx-target="#reader-panes" hx-swap="outerHTML"
             hx-include="closest form"
             hx-trigger="input changed delay:300ms, search"
             hx-replace-url="true">
    </form>
    <form class="reader-mark-all" method="post" action="/reader/read-all"
          hx-post="/reader/read-all" hx-target="#reader-panes" hx-swap="outerHTML">
      {{template "reader-ctx" .}}
      <button type="submit">Mark all read</button>
    </form>
  </header>
```

Replace it with (the middle toolbar sits above the header; the search form is untouched, only relocated below the toolbar and the Mark-all-read form removed from the header block):

```html
  <div class="reader-list-toolbar">
    <form class="reader-mark-all" method="post" action="/reader/read-all"
          hx-post="/reader/read-all" hx-target="#reader-panes" hx-swap="outerHTML">
      {{template "reader-ctx" .}}
      <button type="submit" class="toolbar-btn">{{ticon "check"}}Mark all read</button>
    </form>
  </div>
  <header class="reader-list-head">
    {{template "reader-back" (dict "For" "reader-list-open" "Label" "Feeds")}}
    <h2>{{.Title}}</h2>
    <form method="get" action="{{.BasePath}}" class="reader-search" role="search">
      <input type="hidden" name="filter" value="{{.Filter}}">
      <input type="search" id="reader-search-input" name="q" value="{{.Query}}"
             placeholder="Search articles…" aria-label="Search articles"
             hx-get="{{.BasePath}}" hx-target="#reader-panes" hx-swap="outerHTML"
             hx-include="closest form"
             hx-trigger="input changed delay:300ms, search"
             hx-replace-url="true">
    </form>
  </header>
```

- [ ] **Step 2: Restyle the reading-pane action bar**

In `{{define "article"}}`, the `.reader-actions` block currently reads:

```html
    <div class="reader-actions">
      {{if .URL}}<a class="button" href="{{.URL}}" target="_blank" rel="noopener noreferrer">Open original</a>{{end}}
      <form method="post" action="/reader/item/{{.ID}}/star"
            hx-post="/reader/item/{{.ID}}/star" hx-target="#reader-article" hx-swap="outerHTML">
        {{template "reader-ctx" .}}
        <button type="submit" class="reader-article-star">{{if .Starred}}&#9733; Starred{{else}}&#9734; Star{{end}}</button>
      </form>
      <form method="post" action="/reader/item/{{.ID}}/{{if .Read}}unread{{else}}read{{end}}"
            hx-post="/reader/item/{{.ID}}/{{if .Read}}unread{{else}}read{{end}}"
            hx-target="#reader-article" hx-swap="outerHTML">
        {{template "reader-ctx" .}}
        <button type="submit" class="reader-article-read-toggle">{{if .Read}}Mark unread{{else}}Mark read{{end}}</button>
      </form>
      {{if .HasFull}}
        {{if .ShowingFull}}
          <a class="button" href="/reader/item/{{.ID}}?view=feed&scope={{.Scope}}&sub={{.SubID}}&filter={{.Filter}}{{if .Query}}&q={{.Query}}{{end}}"
             hx-get="/reader/item/{{.ID}}?view=feed&scope={{.Scope}}&sub={{.SubID}}&filter={{.Filter}}{{if .Query}}&q={{.Query}}{{end}}"
             hx-target="#reader-article" hx-swap="outerHTML">Show feed version</a>
        {{else}}
          <a class="button" href="/reader/item/{{.ID}}?scope={{.Scope}}&sub={{.SubID}}&filter={{.Filter}}{{if .Query}}&q={{.Query}}{{end}}"
             hx-get="/reader/item/{{.ID}}?scope={{.Scope}}&sub={{.SubID}}&filter={{.Filter}}{{if .Query}}&q={{.Query}}{{end}}"
             hx-target="#reader-article" hx-swap="outerHTML">Show full article</a>
        {{end}}
      {{else}}
        <form method="post" action="/reader/item/{{.ID}}/full"
              hx-post="/reader/item/{{.ID}}/full" hx-target="#reader-article" hx-swap="outerHTML">
          {{template "reader-ctx" .}}
          <button type="submit">Fetch full article</button>
        </form>
      {{end}}
    </div>
```

Replace it with (every `class`, `method`, `action`, `hx-*` attribute is byte-for-byte the same; only `class="button"`/bare `<button>` gained `toolbar-btn` and an icon, and the star/unicode glyphs became `{{ticon}}` calls):

```html
    <div class="reader-actions">
      {{if .URL}}<a class="toolbar-btn" href="{{.URL}}" target="_blank" rel="noopener noreferrer">{{ticon "external"}}Open original</a>{{end}}
      <form method="post" action="/reader/item/{{.ID}}/star"
            hx-post="/reader/item/{{.ID}}/star" hx-target="#reader-article" hx-swap="outerHTML">
        {{template "reader-ctx" .}}
        <button type="submit" class="toolbar-btn reader-article-star">{{if .Starred}}{{ticon "star-filled"}}Starred{{else}}{{ticon "star-outline"}}Star{{end}}</button>
      </form>
      <form method="post" action="/reader/item/{{.ID}}/{{if .Read}}unread{{else}}read{{end}}"
            hx-post="/reader/item/{{.ID}}/{{if .Read}}unread{{else}}read{{end}}"
            hx-target="#reader-article" hx-swap="outerHTML">
        {{template "reader-ctx" .}}
        <button type="submit" class="toolbar-btn reader-article-read-toggle">{{ticon "check"}}{{if .Read}}Mark unread{{else}}Mark read{{end}}</button>
      </form>
      {{if .HasFull}}
        {{if .ShowingFull}}
          <a class="toolbar-btn" href="/reader/item/{{.ID}}?view=feed&scope={{.Scope}}&sub={{.SubID}}&filter={{.Filter}}{{if .Query}}&q={{.Query}}{{end}}"
             hx-get="/reader/item/{{.ID}}?view=feed&scope={{.Scope}}&sub={{.SubID}}&filter={{.Filter}}{{if .Query}}&q={{.Query}}{{end}}"
             hx-target="#reader-article" hx-swap="outerHTML">{{ticon "doc"}}Show feed version</a>
        {{else}}
          <a class="toolbar-btn" href="/reader/item/{{.ID}}?scope={{.Scope}}&sub={{.SubID}}&filter={{.Filter}}{{if .Query}}&q={{.Query}}{{end}}"
             hx-get="/reader/item/{{.ID}}?scope={{.Scope}}&sub={{.SubID}}&filter={{.Filter}}{{if .Query}}&q={{.Query}}{{end}}"
             hx-target="#reader-article" hx-swap="outerHTML">{{ticon "doc"}}Show full article</a>
        {{end}}
      {{else}}
        <form method="post" action="/reader/item/{{.ID}}/full"
              hx-post="/reader/item/{{.ID}}/full" hx-target="#reader-article" hx-swap="outerHTML">
          {{template "reader-ctx" .}}
          <button type="submit" class="toolbar-btn">{{ticon "doc"}}Fetch full article</button>
        </form>
      {{end}}
    </div>
```

- [ ] **Step 3: Run the reader test suite**

Run:
```bash
go test ./internal/apps/reader/... -v 2>&1 | tail -100
```
Expected: everything PASSes, in particular the mark-all-read round-trip test (`.reader-mark-all button` / `ancestorForm`) and the star/read-toggle class assertions (`.reader-article-star`, `.reader-article-read-toggle`).

- [ ] **Step 4: Commit**

```bash
git add internal/apps/reader/templates/panes.partial.html
git commit -m "feat(reader): middle toolbar for mark-all-read, icon buttons in the reading pane"
```

---

### Task 7: JS — resizable gutters

**Files:**
- Modify: `internal/apps/reader/static/reader.js`

**Interfaces:**
- Consumes: `#reader-panes-row`, `.pane-gutter[data-gutter-for]`, `.reader-tree`, `.reader-list` (all from Task 4). Reads/writes `localStorage['reader.paneWidths']` as `{"tree": <px>, "list": <px>}`.
- Produces: CSS custom properties `--reader-tree-w`/`--reader-list-w` set as inline styles on `#reader-panes-row`.

- [ ] **Step 1: Add the resizable-panes section**

Add this whole block inside the existing IIFE in `internal/apps/reader/static/reader.js`, after the `markRead` function and before the `press` function (keeping the existing keyboard-handling code below untouched):

```javascript
	// --- Resizable panes -----------------------------------------------
	//
	// Desktop-only (matches the 900px breakpoint where the CSS collapses to
	// two panes): below it the flex/gutter layout stops being interactive
	// and the CSS in app.css takes over pane widths entirely, so any stored
	// inline custom property has to be cleared rather than just ignored —
	// otherwise a width dragged wide on desktop would also apply the moment
	// the media query's own rules stopped overriding it.
	var PANE_STORE_KEY = "reader.paneWidths";
	var PANE_MIN = { tree: 10, list: 14 }; // rem
	var PANE_MAX = { tree: 24, list: 32 }; // rem
	var DESKTOP_QUERY = window.matchMedia("(min-width: 901px)");

	function remToPx(rem) {
		var rootPx = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
		return rem * rootPx;
	}

	function loadPaneWidths() {
		try {
			var raw = window.localStorage.getItem(PANE_STORE_KEY);
			if (!raw) return null;
			var parsed = JSON.parse(raw);
			if (typeof parsed.tree !== "number" || typeof parsed.list !== "number") return null;
			return parsed;
		} catch (e) {
			return null;
		}
	}

	function savePaneWidths(widths) {
		try {
			window.localStorage.setItem(PANE_STORE_KEY, JSON.stringify(widths));
		} catch (e) {
			// Private browsing or a full quota: the drag itself still worked,
			// it just won't be remembered next time.
		}
	}

	function currentPaneWidths(row) {
		var tree = row.querySelector(".reader-tree");
		var list = row.querySelector(".reader-list");
		return {
			tree: Math.round(tree.getBoundingClientRect().width),
			list: Math.round(list.getBoundingClientRect().width),
		};
	}

	function syncPaneWidths() {
		var row = document.getElementById("reader-panes-row");
		if (!row) return;
		if (!DESKTOP_QUERY.matches) {
			row.style.removeProperty("--reader-tree-w");
			row.style.removeProperty("--reader-list-w");
			return;
		}
		var stored = loadPaneWidths();
		if (!stored) return;
		row.style.setProperty("--reader-tree-w", stored.tree + "px");
		row.style.setProperty("--reader-list-w", stored.list + "px");
	}

	function clampPx(rawPx, key) {
		var min = remToPx(PANE_MIN[key]);
		var max = remToPx(PANE_MAX[key]);
		return Math.min(max, Math.max(min, rawPx));
	}

	function initResizablePanes() {
		var row = document.getElementById("reader-panes-row");
		if (!row) return;
		syncPaneWidths();

		var dragging = null; // { key: "tree"|"list", startX, startWidth }

		function setWidth(key, px) {
			row.style.setProperty("--reader-" + key + "-w", clampPx(px, key) + "px");
		}

		row.querySelectorAll(".pane-gutter").forEach(function (gutter) {
			var key = gutter.getAttribute("data-gutter-for");

			gutter.addEventListener("pointerdown", function (e) {
				if (!DESKTOP_QUERY.matches) return;
				var target = row.querySelector(key === "tree" ? ".reader-tree" : ".reader-list");
				dragging = { key: key, startX: e.clientX, startWidth: target.getBoundingClientRect().width };
				gutter.classList.add("is-dragging");
				gutter.setPointerCapture(e.pointerId);
				e.preventDefault();
			});

			// The WAI-ARIA "separator" keyboard pattern: arrow keys nudge the
			// pane a fixed step, for anyone who cannot drag with a pointer.
			gutter.addEventListener("keydown", function (e) {
				if (!DESKTOP_QUERY.matches) return;
				if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
				var target = row.querySelector(key === "tree" ? ".reader-tree" : ".reader-list");
				var step = e.key === "ArrowRight" ? 16 : -16;
				setWidth(key, target.getBoundingClientRect().width + step);
				savePaneWidths(currentPaneWidths(row));
				e.preventDefault();
			});
		});

		row.addEventListener("pointermove", function (e) {
			if (!dragging) return;
			setWidth(dragging.key, dragging.startWidth + (e.clientX - dragging.startX));
		});

		function endDrag() {
			if (!dragging) return;
			dragging = null;
			row.querySelectorAll(".pane-gutter.is-dragging").forEach(function (g) {
				g.classList.remove("is-dragging");
			});
			savePaneWidths(currentPaneWidths(row));
		}
		row.addEventListener("pointerup", endDrag);
		row.addEventListener("pointercancel", endDrag);

		DESKTOP_QUERY.addEventListener("change", syncPaneWidths);
	}

	document.addEventListener("DOMContentLoaded", initResizablePanes);
	// A panes-wide swap (subscribing, refreshing, deleting) replaces
	// #reader-panes-row outerHTML-style, wiping any inline custom
	// properties JS had set — without re-running this, the layout would
	// silently fall back to the CSS defaults on the very next feed click.
	document.addEventListener("htmx:afterSwap", function (e) {
		if (e.target && e.target.id === "reader-panes") initResizablePanes();
	});
```

- [ ] **Step 2: Manual verification (no JS test harness exists in this repo)**

Follow the `run` skill to start the app, sign in, and open `/reader/`. Confirm:
1. Dragging the divider between the feed tree and the article list changes both panes' widths live, clamped so neither pane collapses past its minimum.
2. Reloading the page keeps the dragged widths.
3. Resizing the browser window below ~900px hides the gutters and collapses to the existing two-pane/one-pane layouts exactly as before this change.
4. Tab to a gutter and press the left/right arrow keys; the adjacent pane resizes.

- [ ] **Step 3: Commit**

```bash
git add internal/apps/reader/static/reader.js
git commit -m "feat(reader): draggable, persisted, keyboard-accessible pane gutters"
```

---

### Task 8: JS — overflow menu, dialogs, confirm dialog

**Files:**
- Modify: `internal/apps/reader/static/reader.js`

**Interfaces:**
- Consumes: `.reader-menu`/`.reader-menu-toggle`/`.reader-menu-list` (Task 4), `[data-open-dialog]`/`.reader-dialog-cancel`/`.reader-dialog-close`/`dialog[data-reopen]` and `#reader-confirm-dialog`/`#reader-confirm-message`/`#reader-confirm-ok` (Task 5).

- [ ] **Step 1: Add the menu + dialog section**

Add this block right after the resizable-panes section from Task 7 (still inside the same IIFE):

```javascript
	// --- Overflow menu ---------------------------------------------------

	function closeMenu(menu) {
		var list = menu.querySelector(".reader-menu-list");
		var toggle = menu.querySelector(".reader-menu-toggle");
		if (list) list.hidden = true;
		if (toggle) toggle.setAttribute("aria-expanded", "false");
	}

	document.addEventListener("click", function (e) {
		var toggle = e.target.closest(".reader-menu-toggle");
		document.querySelectorAll(".reader-menu").forEach(function (menu) {
			if (toggle && menu.contains(toggle)) {
				var list = menu.querySelector(".reader-menu-list");
				var wasOpen = !list.hidden;
				closeMenu(menu);
				if (!wasOpen) {
					list.hidden = false;
					toggle.setAttribute("aria-expanded", "true");
				}
			} else if (!menu.contains(e.target)) {
				closeMenu(menu);
			}
		});
	});

	// --- Dialogs -----------------------------------------------------------

	document.addEventListener("click", function (e) {
		var opener = e.target.closest("[data-open-dialog]");
		if (opener) {
			var dialog = document.getElementById(opener.getAttribute("data-open-dialog"));
			if (dialog && typeof dialog.showModal === "function") dialog.showModal();
			return;
		}
		var closer = e.target.closest(".reader-dialog-cancel, .reader-dialog-close");
		if (closer) {
			var open = closer.closest("dialog");
			if (open) open.close();
		}
	});

	function reopenFlaggedDialogs() {
		document.querySelectorAll("dialog[data-reopen]").forEach(function (d) {
			if (typeof d.showModal === "function") d.showModal();
		});
	}
	document.addEventListener("DOMContentLoaded", reopenFlaggedDialogs);
	document.addEventListener("htmx:afterSwap", function (e) {
		if (e.target && e.target.id === "reader-panes") reopenFlaggedDialogs();
	});

	// --- Confirm dialog, replacing window.confirm for hx-confirm ----------
	//
	// Scoped to #reader-panes so this never intercepts confirm() elsewhere in
	// the suite — theme.js's own data-confirm gate (internal/ui/static/theme.js)
	// is untouched and keeps handling every other app.
	document.addEventListener("htmx:confirm", function (e) {
		var panes = document.getElementById("reader-panes");
		if (!panes || !e.target || !panes.contains(e.target)) return;
		var msg = e.target.getAttribute("hx-confirm");
		if (!msg) return;

		var dialog = document.getElementById("reader-confirm-dialog");
		if (!dialog || typeof dialog.showModal !== "function") return; // let htmx fall back to window.confirm

		e.preventDefault();
		var messageEl = document.getElementById("reader-confirm-message");
		var ok = document.getElementById("reader-confirm-ok");
		if (messageEl) messageEl.textContent = msg;

		function onOk() {
			ok.removeEventListener("click", onOk);
			dialog.close();
			e.detail.issueRequest();
		}
		ok.addEventListener("click", onOk, { once: true });
		dialog.showModal();
	});
```

- [ ] **Step 2: Manual verification**

Using the `run` skill against the running app:
1. Click "..." — the menu opens; click elsewhere — it closes; open it and press Escape — it closes.
2. Click "Add feed…" — the dialog opens with a native backdrop; click Cancel — it closes without submitting.
3. Submit a URL that resolves to more than one feed (or reuse an existing multi-feed test fixture manually) — the dialog reopens showing the candidate list.
4. Click a folder's Delete button — the confirm dialog opens with the folder's name in the message, not a native `confirm()` popup; Cancel does nothing, Confirm deletes it.
5. Confirm keyboard shortcuts ('j'/'k'/'m'/'s'/'r'/'/') from Task 6/earlier work still function with the menu/dialogs present.

- [ ] **Step 3: Commit**

```bash
git add internal/apps/reader/static/reader.js
git commit -m "feat(reader): overflow menu, dialog open/close/reopen, dialog-based delete confirm"
```

---

### Task 9: PATTERNS.md entry

Per AGENTS.md's Workflow section, add an entry once a reusable shape has actually been introduced — this pass adds three the rest of the suite might reach for later (a JS-driven overflow menu, native `<dialog>` popups replacing `hx-confirm`, and draggable/persisted resizable panes).

**Files:**
- Modify: `PATTERNS.md`

- [ ] **Step 1: Add the entry**

Append to the end of `PATTERNS.md`:

```markdown

- **JS-driven "..." overflow menu with native `<dialog>` popups** — reach for
  this when a toolbar has more actions than fit inline: a `.reader-menu`
  toggle button plus a `.reader-menu-list` shown/hidden via the `hidden`
  attribute (not `display:none` in CSS, so no extra selector is needed to
  reverse it), and destructive `hx-confirm` buttons routed through a single
  shared `<dialog>` via `htmx:confirm` instead of `window.confirm`. Canonical:
  `internal/apps/reader/templates/panes.partial.html`'s `reader-toolbar`/
  `reader-dialogs` defines and `internal/apps/reader/static/reader.js`'s
  menu/dialog sections.

- **Draggable, persisted, keyboard-accessible pane gutters** — reach for this
  for any multi-pane layout that wants real resize-by-drag: a flex row with
  `role="separator"` gutter elements between panes, widths held as CSS custom
  properties set by JS (never inline `flex-basis` directly, so a CSS default
  via `var(--x, fallback)` still applies before JS runs), persisted to
  `localStorage`, and cleared below the layout's own mobile breakpoint via a
  `matchMedia` listener rather than left to silently misapply. Canonical:
  `internal/ui/static/app.css`'s `.reader-panes-row`/`.pane-gutter` and
  `internal/apps/reader/static/reader.js`'s resizable-panes section.
```

- [ ] **Step 2: Commit**

```bash
git add PATTERNS.md
git commit -m "docs: record the overflow-menu/dialog and resizable-pane patterns"
```

---

### Task 10: Full verification pass

**Files:** none (verification only)

- [ ] **Step 1: Run the full check**

```bash
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
go mod tidy && git diff --exit-code go.mod go.sum
go test ./... -race -count=1
```
Expected: `gofmt -l .` prints nothing; every other command exits 0; `go test` reports only PASS.

- [ ] **Step 2: Manual, end-to-end browser pass**

Using the `run` skill (or `webapp-testing` if it fits better), sign in and exercise, on desktop width:
- Drag both gutters; reload; widths persisted.
- Open every dialog from the "..." menu (Add feed, Import OPML, New folder, Keyboard shortcuts); Cancel/X close each without side effects.
- Add a feed via the dialog; confirm it appears in the tree and the dialog closes.
- Delete a folder and unsubscribe from a feed; confirm the new dialog (not a native `confirm()`) appears both times, and Cancel truly cancels (nothing deleted).
- Mark all read from the new middle toolbar; confirm counts update.
- Use the restyled reading-pane actions (Open original, Star, Mark read/unread, Fetch/Show full article).

Then at mobile width (resize below 640px, or use the browser tool's mobile preset):
- Confirm the tree → list → article drill-down still works via the back buttons, with gutters absent.

- [ ] **Step 3: Push the branch and open a PR**

```bash
git push -u origin feat/reader-ui-polish
gh pr create --title "feat(reader): UI polish — resizable panes, toolbars, dialogs" --body "$(cat <<'EOF'
## Summary
- Resizable three-pane layout (tree/list/reading), gutters draggable and persisted to localStorage.
- New top toolbar (filter pills + "..." overflow menu) and a middle toolbar (Mark all read) above the article list.
- Add feed, Import OPML, New folder, and Keyboard shortcuts moved into native `<dialog>` popups; delete/unsubscribe now confirm via a dialog instead of `window.confirm`.
- Reading-pane actions and every new toolbar restyled with a new shared icon set (`internal/ui/toolbar_icons.go`).
- Pure UI/chrome pass — no new routes, no domain-logic changes; corrects one inaccuracy discovered in the approved spec's middle-toolbar section.

## Test plan
- [ ] `go test ./... -race -count=1`, `go vet ./...`, `staticcheck`, `gofmt -l .` all clean
- [ ] Manual desktop pass: gutters, dialogs, menu, confirm dialog, mark-all-read
- [ ] Manual mobile pass: drill-down still works below 640px
EOF
)"
```

Report the PR URL back once opened.
