# Notes Context-Menu Row Controls Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace each Notes outline row's wide, hover-reveal control strip with a small always-visible "···" button that opens a context menu holding every action, plus a hover-only floating overlay duplicating the four structural actions (move up/down, indent/outdent, add bullet) for fast mouse access — freeing up row width for the title/note text.

**Architecture:** This is a server-rendered Go app (`html/template` + HTMX, no client framework, no build step). The change is confined to one template (`internal/apps/notes/templates/outline.html`), one stylesheet (`internal/ui/static/app.css`), and their matching Go tests (`internal/apps/notes/handlers_test.go`). No new Go handlers, no new routes, no JS changes — every action already has a working endpoint; this plan only moves/duplicates existing buttons and reuses the `<details>/<summary>` popover pattern already proven by the settings-gear menu in `internal/ui/templates/base.html:39-58`.

**Tech Stack:** Go 1.26, `html/template`, HTMX (progressive enhancement via `formaction` + `hx-post` pairs), plain CSS (no Tailwind, no preprocessor), `modernc.org/sqlite`.

## Global Constraints

- No new Go handlers or routes — every menu/overlay button reuses an existing endpoint (`/notes/{id}/done`, `/move`, `/indent`, `/outdent`, `/due`, `/delete`, `/notes/{id}/collapse`, `/notes/new`).
- Every button that carries `formaction` must also carry a matching `hx-post` (same URL), `hx-target="#outline"`, and `hx-swap="innerHTML"` — enforced by the existing test `TestEveryStructuralButtonMirrorsItsFormactionAsHTMX` (`internal/apps/notes/handlers_test.go:1252`). Do not weaken or delete that test.
- No JS changes in `internal/apps/notes/static/notes.js` — verified during planning that every JS selector this refactor touches (`button.outline-done`, `button[formaction$="/indent"]`, `button[formaction$="/outdent"]`, `button[name="dir"][value=...]`, `button.outline-chevron`) is a class/attribute selector, not a DOM-position dependency, so it keeps matching after buttons move into the menu/overlay.
- Delete keeps its confirmation dialog (`hx-confirm`), and that confirmation must apply **only** to the delete action, never to its neighboring buttons — this was previously guaranteed by giving delete its own `<form>`; this plan instead puts `hx-confirm` directly on the delete `<button>`, which htmx scopes to that element's own request regardless of which form it lives in.
- Keep the CSP-safe pattern already used for the settings menu: no inline `<script>`, no `style="..."` attributes, `<details>/<summary>` for open/close state — enforced by the existing `TestOutlineUsesNoInlineStyles` (`internal/apps/notes/handlers_test.go:433-442`), which fails if any `style=` attribute appears in the rendered outline. The new menu/overlay markup must not introduce one.
- `gofmt` and `go vet ./...` must be clean; the full suite (`go test ./... -race`) must pass before each commit that changes Go files.

---

## File Map

- Modify: `internal/apps/notes/templates/outline.html` (template `outline-rows`, lines 105-202) — row markup restructuring.
- Modify: `internal/ui/static/app.css` (roughly lines 662-946) — layout, new `.outline-menu*` popover styles, `.outline-overlay` hover styles, removal of now-unused `.outline-actions`/`.outline-delete` layout rules.
- Modify: `internal/apps/notes/handlers_test.go` — replace `TestDeleteIsItsOwnConfirmedForm` (lines 1321-1342) with a test matching the new delete-button-level confirm; add two new tests asserting the menu and overlay each hold the right buttons.

No other files change. `internal/apps/notes/static/notes.js` is read-only for this plan (verified, not touched).

---

### Task 1: Restructure the outline row template and its tests

**Files:**
- Modify: `internal/apps/notes/templates/outline.html:105-202`
- Modify: `internal/apps/notes/handlers_test.go` (add two tests, replace one)
- Test: `internal/apps/notes/handlers_test.go` (same file — this package tests HTML output directly)

**Interfaces:**
- Consumes: existing Go struct `outlineRow` (`internal/apps/notes/view.go:35-59`) — fields used: `.ID`, `.CSRFToken`, `.RootID`, `.Done`, `.HasChildren`, `.Collapsed`, `.DisplayTitle`, `.Title`, `.Note`, `.DueOn`, `.Overdue`, `.Position`, `.Last`, `.Depth`, `.Children`. No changes to this struct.
- Produces: row markup with class names `outline-menu`, `outline-menu-toggle`, `outline-menu-list`, `outline-menu-due`, `outline-menu-delete`, `outline-overlay` — Task 2 (CSS) depends on these exact class names.

- [ ] **Step 1: Write the failing tests**

Open `internal/apps/notes/handlers_test.go` and replace `TestDeleteIsItsOwnConfirmedForm` (currently lines 1317-1342) with:

```go
// TestDeleteButtonIsIndividuallyConfirmed. hx-confirm lives on the delete
// button itself, not on a wrapping form: htmx scopes hx-confirm to the
// element that issues the request, so putting it on the button (rather
// than an ancestor) is what keeps every other button in the same menu from
// asking for confirmation too.
func TestDeleteButtonIsIndividuallyConfirmed(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "Projects")

	doc := s.get(t, s.alice, "/notes/")
	btn := doc.MustHave(`button[formaction=/notes/` + itoa(id) + `/delete]`)
	if _, ok := htmlassert.Attr(btn, "hx-confirm"); !ok {
		t.Error("the delete button asks for no confirmation")
	}
	if got, _ := htmlassert.Attr(btn, "hx-post"); got != "/notes/"+itoa(id)+"/delete" {
		t.Errorf("delete button hx-post = %q", got)
	}
	if got, _ := htmlassert.Attr(btn, "hx-target"); got != "#outline" {
		t.Errorf("delete button hx-target = %q, want #outline", got)
	}
	if cls, _ := htmlassert.Attr(btn, "class"); !strings.Contains(cls, "outline-menu-delete") {
		t.Errorf("the delete button's class is %q", cls)
	}

	// Neighboring menu buttons must not inherit the confirmation.
	for _, sel := range []string{
		`button[formaction=/notes/` + itoa(id) + `/done]`,
		`button[value=up]`,
	} {
		b := doc.MustHave(sel)
		if _, ok := htmlassert.Attr(b, "hx-confirm"); ok {
			t.Errorf("%s unexpectedly asks for confirmation", sel)
		}
	}
}

// TestOutlineMenuHoldsEveryAction. The "···" menu is the comprehensive,
// touch-reachable home for every row action — including the four that also
// get a hover-overlay shortcut — so nothing is unreachable without a mouse
// hovering the row.
func TestOutlineMenuHoldsEveryAction(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "Projects")

	doc := s.get(t, s.alice, "/notes/")
	menu := doc.MustHave(".outline-menu")
	summary := doc.MustHave(".outline-menu-toggle")
	if tag := summary.Data; tag != "summary" {
		t.Errorf("menu toggle is a <%s>, want <summary>", tag)
	}
	if !within(summary, menu) {
		t.Error("the toggle is not inside .outline-menu")
	}
	list := doc.MustHave(".outline-menu-list")
	if !within(list, menu) {
		t.Error(".outline-menu-list is not inside .outline-menu")
	}

	for _, sel := range []string{
		`button.outline-done`,
		`input.outline-due-input`,
		`button[value=up]`,
		`button[value=down]`,
		`button[formaction=/notes/` + itoa(id) + `/indent]`,
		`button[formaction=/notes/` + itoa(id) + `/outdent]`,
		`button[formaction=/notes/new]`,
		`button.outline-menu-delete`,
	} {
		found := false
		for _, n := range doc.QueryAll(sel) {
			if within(n, list) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("menu is missing %s", sel)
		}
	}
}

// TestHoverOverlayDuplicatesStructuralActions. The overlay is the
// fast-mouse-access shortcut for the four actions used often enough to
// justify a hover target — it must not include done, due-date editing, or
// delete, which stay menu-only.
func TestHoverOverlayDuplicatesStructuralActions(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "Projects")

	doc := s.get(t, s.alice, "/notes/")
	overlay := doc.MustHave(".outline-overlay")

	for _, sel := range []string{
		`button[value=up]`,
		`button[value=down]`,
		`button[formaction=/notes/` + itoa(id) + `/indent]`,
		`button[formaction=/notes/` + itoa(id) + `/outdent]`,
		`button[formaction=/notes/new]`,
	} {
		found := false
		for _, n := range doc.QueryAll(sel) {
			if within(n, overlay) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("overlay is missing %s", sel)
		}
	}
	for _, sel := range []string{`button.outline-done`, `input.outline-due-input`, `button.outline-menu-delete`} {
		for _, n := range doc.QueryAll(sel) {
			if within(n, overlay) {
				t.Errorf("%s must not be in the hover overlay", sel)
			}
		}
	}
}
```

These new tests call a small helper, `within(node, ancestor *html.Node) bool`, that does not exist yet. Add it near the bottom of `handlers_test.go` (next to the other test helpers such as `itoa`):

```go
// within reports whether node is ancestor or a descendant of it — used to
// check which container (menu vs. overlay) a given button actually lives
// in, since both contain buttons matching the same CSS selectors.
func within(node, ancestor *html.Node) bool {
	for n := node; n != nil; n = n.Parent {
		if n == ancestor {
			return true
		}
	}
	return false
}
```

`internal/apps/notes/handlers_test.go`'s existing `doc.QueryAll(...)` and `doc.MustHave(...)` calls (e.g. line 547) already return `*html.Node` from `golang.org/x/net/html` — confirmed by reading `internal/htmlassert/htmlassert.go:49` (`func (d *Doc) QueryAll(selector string) []*html.Node`) and `:95` (`func (d *Doc) MustHave(selector string) *html.Node`). Add `"golang.org/x/net/html"` to the import block at the top of `handlers_test.go` (it's already an indirect dependency via `htmlassert`, so no `go.mod`/`go.sum` change is needed):

```go
import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/apps/notes/... -run 'TestDeleteButtonIsIndividuallyConfirmed|TestOutlineMenuHoldsEveryAction|TestHoverOverlayDuplicatesStructuralActions' -v
```

Expected: all three FAIL against the current (unmodified) template:
- `TestDeleteButtonIsIndividuallyConfirmed` fails with "the delete button asks for no confirmation" — today `hx-confirm` sits on the wrapping `<form class="outline-delete">`, not on the button itself.
- `TestOutlineMenuHoldsEveryAction` and `TestHoverOverlayDuplicatesStructuralActions` fail with "menu is missing ..." / "overlay is missing ..." errors, since `.outline-menu` and `.outline-overlay` don't exist in the current template yet.

If instead the package fails to *compile* (e.g. an unused import, or a typo in `within`), fix the compile error first, then re-run to confirm these are the actual failures before moving on.

- [ ] **Step 3: Rewrite the row template**

Replace the row block in `internal/apps/notes/templates/outline.html` (the `<li class="outline-item">...</li>` body inside `{{define "outline-rows"}}`, currently lines 108-199) with:

```html
	<li class="outline-item">
		<div class="outline-row{{if .Done}} outline-row-done{{end}}" data-id="{{.ID}}">
			<form method="post" action="/notes/{{.ID}}/text" class="outline-main">
				<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
				<input type="hidden" name="root" value="{{.RootID}}">
				<input type="hidden" name="focus_id" value="{{.ID}}">

				<details class="outline-menu">
					<summary class="outline-menu-toggle quiet" aria-label="Bullet actions">&#8943;</summary>
					<div class="outline-menu-list">
						<button type="submit" class="quiet outline-done"
						        formaction="/notes/{{.ID}}/done"
						        hx-post="/notes/{{.ID}}/done" hx-target="#outline" hx-swap="innerHTML"
						        name="done" value="{{if .Done}}0{{else}}1{{end}}"
						        aria-pressed="{{if .Done}}true{{else}}false{{end}}"
						        >{{if .Done}}&#9745; Mark not done{{else}}&#9744; Mark done{{end}}</button>

						<label class="outline-menu-due">
							<span>Due date</span>
							<input type="date" class="outline-due-input" name="due" value="{{.DueOn}}"
							       aria-label="Due date"
							       hx-post="/notes/{{.ID}}/due" hx-target="#outline" hx-swap="innerHTML"
							       hx-trigger="change">
						</label>

						<button type="submit" class="quiet" formaction="/notes/{{.ID}}/move"
						        hx-post="/notes/{{.ID}}/move" hx-target="#outline" hx-swap="innerHTML"
						        name="dir" value="up" aria-label="Move up"
						        {{if eq .Position 0}}disabled{{end}}>&uarr; Move up</button>
						<button type="submit" class="quiet" formaction="/notes/{{.ID}}/move"
						        hx-post="/notes/{{.ID}}/move" hx-target="#outline" hx-swap="innerHTML"
						        name="dir" value="down" aria-label="Move down"
						        {{if .Last}}disabled{{end}}>&darr; Move down</button>
						<button type="submit" class="quiet" formaction="/notes/{{.ID}}/indent"
						        hx-post="/notes/{{.ID}}/indent" hx-target="#outline" hx-swap="innerHTML"
						        aria-label="Indent"
						        {{if eq .Position 0}}disabled{{end}}>&#8677; Indent</button>
						<button type="submit" class="quiet" formaction="/notes/{{.ID}}/outdent"
						        hx-post="/notes/{{.ID}}/outdent" hx-target="#outline" hx-swap="innerHTML"
						        aria-label="Outdent"
						        {{if eq .Depth 0}}disabled{{end}}>&#8676; Outdent</button>
						<button type="submit" class="quiet" formaction="/notes/new"
						        hx-post="/notes/new" hx-target="#outline" hx-swap="innerHTML"
						        aria-label="Add a bullet below">+ Add bullet</button>

						<button type="submit" class="quiet outline-menu-delete" formaction="/notes/{{.ID}}/delete"
						        hx-post="/notes/{{.ID}}/delete" hx-target="#outline" hx-swap="innerHTML"
						        hx-confirm="Delete &#8220;{{.DisplayTitle}}&#8221; and everything under it? This cannot be undone."
						        aria-label="Delete">&times; Delete</button>
					</div>
				</details>

				{{if .HasChildren}}
				<button type="submit" class="outline-chevron quiet"
				        formaction="/notes/{{.ID}}/collapse"
				        hx-post="/notes/{{.ID}}/collapse" hx-target="#outline" hx-swap="innerHTML"
				        name="collapsed" value="{{if .Collapsed}}0{{else}}1{{end}}"
				        aria-expanded="{{if .Collapsed}}false{{else}}true{{end}}"
				        aria-label="{{if .Collapsed}}Expand{{else}}Collapse{{end}}">{{if .Collapsed}}&#9656;{{else}}&#9662;{{end}}</button>
				{{else}}
				<span class="outline-chevron" aria-hidden="true"></span>
				{{end}}

				<a class="outline-dot{{if .Collapsed}} outline-dot-full{{end}}"
				   href="/notes/{{.ID}}"
				   aria-label="Zoom in to {{.DisplayTitle}}">&bull;</a>

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

				{{$row := .}}
				{{with .DueOn}}
				<a class="outline-due-chip{{if $row.Overdue}} outline-due-overdue{{end}}"
				   href="/notes/due">{{.}}</a>
				{{end}}

				<span class="outline-overlay">
					<button type="submit" class="quiet" formaction="/notes/{{.ID}}/move"
					        hx-post="/notes/{{.ID}}/move" hx-target="#outline" hx-swap="innerHTML"
					        name="dir" value="up" aria-label="Move up"
					        {{if eq .Position 0}}disabled{{end}}>&uarr;</button>
					<button type="submit" class="quiet" formaction="/notes/{{.ID}}/move"
					        hx-post="/notes/{{.ID}}/move" hx-target="#outline" hx-swap="innerHTML"
					        name="dir" value="down" aria-label="Move down"
					        {{if .Last}}disabled{{end}}>&darr;</button>
					<button type="submit" class="quiet" formaction="/notes/{{.ID}}/indent"
					        hx-post="/notes/{{.ID}}/indent" hx-target="#outline" hx-swap="innerHTML"
					        aria-label="Indent"
					        {{if eq .Position 0}}disabled{{end}}>&#8677;</button>
					<button type="submit" class="quiet" formaction="/notes/{{.ID}}/outdent"
					        hx-post="/notes/{{.ID}}/outdent" hx-target="#outline" hx-swap="innerHTML"
					        aria-label="Outdent"
					        {{if eq .Depth 0}}disabled{{end}}>&#8676;</button>
					<button type="submit" class="quiet" formaction="/notes/new"
					        hx-post="/notes/new" hx-target="#outline" hx-swap="innerHTML"
					        aria-label="Add a bullet below">+</button>
				</span>
			</form>
		</div>

		{{with .Children}}{{template "outline-rows" .}}{{end}}
	</li>
```

Note what changed from the original: the standalone `<form class="outline-delete">` (previously a sibling of `.outline-main` at the `.outline-row` level) is gone — delete is now a plain button inside `.outline-main`'s `<details>` menu, carrying `hx-confirm` directly on itself, and reusing the form's existing `csrf_token`/`root`/`focus_id` hidden inputs instead of duplicating them.

- [ ] **Step 4: Run the full notes test suite**

```bash
go test ./internal/apps/notes/... -race -v 2>&1 | tail -80
```

Expected: all tests pass, including the three new/updated ones from Step 1. Pay particular attention to:
- `TestEveryStructuralButtonMirrorsItsFormactionAsHTMX` (still passes — every button, menu or overlay, still pairs `formaction` with matching `hx-post`/`hx-target`/`hx-swap`)
- `TestRowsCarryTheirNodeID`, `TestTextInputsAutosaveOverHTMX`, `TestEmptyOutlineFormIsHTMXWired` (unaffected, still pass)
- `TestDeleteRemovesTheBulletAndItsSubtree`, `TestDeleteRenumbersTheSurvivors`, `TestDeleteReturnsToTheZoomItCameFrom`, `TestDeletingAnotherUsersBulletIs404` (unaffected — they POST directly via `s.submit`/`s.post`, not through the rendered button)
- `TestStructuralRequestSavesTheFocusedText`, `TestFocusAndTargetCanDiffer` (unaffected)

If any test fails, read the failure message before changing anything else — don't guess.

- [ ] **Step 5: gofmt and vet**

```bash
gofmt -l internal/apps/notes/handlers_test.go
go vet ./internal/apps/notes/...
```

Expected: `gofmt -l` prints nothing (no output = already formatted); `go vet` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/notes/templates/outline.html internal/apps/notes/handlers_test.go
git commit -m "feat(notes): move row controls into a context menu + hover overlay"
```

---

### Task 2: Style the menu popover and hover overlay

**Files:**
- Modify: `internal/ui/static/app.css` (roughly lines 662-946)

**Interfaces:**
- Consumes: class names produced by Task 1 (`outline-menu`, `outline-menu-toggle`, `outline-menu-list`, `outline-menu-due`, `outline-menu-delete`, `outline-overlay`), plus the pre-existing `.shell-settings`/`.shell-settings-menu` pattern (`app.css:214-239`) as the visual reference for the popover.
- Produces: nothing consumed by later tasks — this is the last code task.

There is no automated test for CSS in this codebase (confirmed: no CSS/visual test tooling exists). Verification for this task is manual, via Task 3.

- [ ] **Step 1: Update `.outline-row` and remove the old done/actions/delete rules**

In `internal/ui/static/app.css`, find the `.outline-row` rule (currently lines 662-667):

```css
.outline-row {
	display: flex;
	align-items: flex-start;
	gap: var(--s-1);
	border-radius: var(--radius);
}
```

Replace it with:

```css
.outline-row {
	position: relative;
	display: flex;
	align-items: flex-start;
	gap: var(--s-1);
	border-radius: var(--radius);
}
```

Find the `.outline-done` rule and its hover state (currently lines 694-704):

```css
.outline-done {
	flex: none;
	width: 1.1rem;
	line-height: 1.7;
	text-align: center;
	padding: 0;
	border: none;
	background: none;
	color: var(--c-text-faint);
}
.outline-done:hover { color: var(--c-text); }
```

Delete this rule entirely — `.outline-done` now lives inside `.outline-menu-list`, which supplies its layout (Step 3 below); the old fixed-width icon-button styling no longer applies since it now renders as a full-width menu row with a text label.

Find the opacity/hover-reveal block (currently lines 905-917):

```css
/* opacity, not visibility: a visibility:hidden control cannot take focus, so
 * :focus-within would never fire and these would be unreachable from the
 * keyboard. */
.outline-chevron,
.outline-actions,
.outline-delete { opacity: 0; }

.outline-row:hover .outline-chevron,
.outline-row:hover .outline-actions,
.outline-row:hover .outline-delete,
.outline-row:focus-within .outline-chevron,
.outline-row:focus-within .outline-actions,
.outline-row:focus-within .outline-delete { opacity: 1; }

.outline-actions { display: flex; flex: none; }

.outline-actions button {
	padding: 0 var(--s-1);
	border: none;
	background: none;
	color: var(--c-text-faint);
	line-height: 1.7;
}

.outline-actions button:hover:not([disabled]) {
	background: var(--c-bg-inset);
	color: var(--c-text);
}

.outline-actions button[disabled] { opacity: 0.3; cursor: default; }

.outline-delete { flex: none; }

.outline-delete button {
	padding: 0 var(--s-1);
	border: none;
	background: none;
	color: var(--c-text-faint);
	line-height: 1.7;
}

.outline-delete button:hover { background: var(--c-danger-bg); color: var(--c-danger); }
```

Replace the whole block with:

```css
/* opacity, not visibility: a visibility:hidden control cannot take focus, so
 * :focus-within would never fire and these would be unreachable from the
 * keyboard. */
.outline-chevron { opacity: 0; }

.outline-row:hover .outline-chevron,
.outline-row:focus-within .outline-chevron { opacity: 1; }

/* The overlay floats over the row's right edge instead of sitting in the
 * flex flow, so it reserves no width when hidden — unlike the old
 * opacity:0 approach, which kept the space reserved even though nothing
 * was visible there. */
.outline-overlay {
	position: absolute;
	right: 0;
	top: 0;
	z-index: 5;
	display: flex;
	opacity: 0;
	background: var(--c-bg);
	border: var(--border);
	border-radius: var(--radius);
	box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

.outline-row:hover .outline-overlay,
.outline-row:focus-within .outline-overlay { opacity: 1; }

.outline-overlay button {
	padding: 0 var(--s-1);
	border: none;
	background: none;
	color: var(--c-text-faint);
	line-height: 1.7;
}

.outline-overlay button:hover:not([disabled]) {
	background: var(--c-bg-inset);
	color: var(--c-text);
}

.outline-overlay button[disabled] { opacity: 0.3; cursor: default; }
```

- [ ] **Step 2: Add the menu popover styles**

Immediately after the `.outline-overlay` rules added in Step 1, add:

```css
.outline-menu {
	position: relative;
	flex: none;
}

.outline-menu-toggle {
	display: block;
	width: 1.1rem;
	line-height: 1.7;
	text-align: center;
	color: var(--c-text-faint);
	list-style: none;
	cursor: pointer;
}
.outline-menu-toggle::-webkit-details-marker { display: none; }
.outline-menu-toggle:hover { color: var(--c-text); }

.outline-menu-list {
	position: absolute;
	left: 0;
	top: calc(100% + var(--s-1));
	z-index: 10;
	display: flex;
	flex-direction: column;
	min-width: 11rem;
	padding: var(--s-2);
	background: var(--c-bg);
	border: var(--border);
	border-radius: var(--radius);
	box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12);
}

.outline-menu-list button {
	display: block;
	width: 100%;
	padding: var(--s-1) var(--s-2);
	border: none;
	border-radius: var(--radius);
	background: none;
	color: var(--c-text);
	text-align: left;
	line-height: 1.7;
}
.outline-menu-list button:hover:not([disabled]) { background: var(--c-bg-inset); }
.outline-menu-list button[disabled] { opacity: 0.4; cursor: default; }

.outline-menu-due {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--s-2);
	padding: var(--s-1) var(--s-2);
	font-size: var(--fs-sm);
	color: var(--c-text-dim);
}

.outline-menu-delete:hover { background: var(--c-danger-bg); color: var(--c-danger); }
```

- [ ] **Step 3: Commit**

```bash
git add internal/ui/static/app.css
git commit -m "style(notes): style the row context menu and hover overlay"
```

---

### Task 3: Manual verification and cleanup pass

**Files:** none modified — this task runs the app and checks behavior end-to-end.

**Interfaces:** none — terminal task.

- [ ] **Step 1: Build, create an account, and start the server against scratch data**

`onsuite user add` reads the password from stdin: on a real terminal it prompts twice with echo disabled, but when stdin isn't a terminal (e.g. piped) it reads a single line, unattended (`cmd/onsuite/user.go`'s `readPassword`). So for a scripted review setup:

```bash
go build ./cmd/onsuite
echo "reviewpass123" | ./onsuite user add reviewer --admin --data-dir /tmp/notes-menu-review
```

Then start the server in a separate terminal (or with `run_in_background`):

```bash
./onsuite serve --data-dir /tmp/notes-menu-review --addr 127.0.0.1:8099
```

- [ ] **Step 2: Open the browser and log in**

Open `http://127.0.0.1:8099/notes/`, log in as `reviewer`, and create 3-4 bullets with varying states: one plain, one with a due date, one marked done, one with a child (to get a chevron).

- [ ] **Step 3: Verify the row layout**

Confirm visually:
- The `···` button sits at the far left of every row and is visible without hovering.
- The row no longer reserves a visible gap on the right when not hovered — title/note text now uses the freed width.
- The due-date chip (on the bullet that has one) is still always visible.
- Hovering a row reveals a small floating toolbar (move up/down, indent/outdent, +) over the row's right edge, and it disappears when the mouse leaves — check that it does not shift other rows' layout when it appears/disappears (it shouldn't, since it's `position: absolute`).

- [ ] **Step 4: Verify the menu**

Click `···` on a row. Confirm the dropdown shows: Mark done/not done, a due-date input, Move up, Move down, Indent, Outdent, Add bullet, Delete — in that order, each individually clickable, and that clicking one (e.g. Mark done) performs the action and the row re-renders correctly (check the `<details>` after re-render: if it re-opens in a jarring way or stays stuck open, note it — the design doc flagged this as an open question to resolve here; if it's mildly awkward but not broken, leave it as-is per YAGNI, don't add JS to force-close it unless it's clearly bad).

Click Delete: confirm the browser's native confirm dialog appears with the expected message, and clicking Cancel leaves the bullet in place while clicking OK deletes it and its children.

- [ ] **Step 5: Verify keyboard shortcuts still work**

With focus in a bullet's title field, try: Tab (indent), Shift+Tab (outdent), Cmd/Ctrl+Enter (toggle done), Cmd/Ctrl+Shift+ArrowUp/Down (move), Cmd/Ctrl+. (collapse, on the bullet with a child), Enter (split/new bullet), Backspace on an empty bullet (delete it). All should behave exactly as before, since `notes.js` was not modified — this step is confirming that claim, not implementing anything.

- [ ] **Step 6: Check light and dark theme**

Toggle the theme via the settings gear (top right) and repeat a quick look at the menu and overlay in dark mode — confirm `var(--c-bg)`, `var(--c-border)`, etc. produce a readable popover in both themes (they should, since every color used is an existing CSS variable already theme-aware elsewhere in the file).

- [ ] **Step 7: Run the full project test suite one more time**

```bash
go build ./... && go vet ./... && go test ./... -race
```

Expected: clean build, no vet warnings, all tests pass.

- [ ] **Step 8: Stop the review server and clean up scratch data**

```bash
rm -rf /tmp/notes-menu-review
```

- [ ] **Step 9: Update the design spec's open question, if resolved**

If Step 4 revealed a clear answer to the `<details>` auto-close question, add a short note to `docs/superpowers/specs/2026-08-27-notes-context-menu-design.md`'s "Open question" paragraph recording what was observed and decided. If it's a non-issue, note that too, in one sentence.

```bash
git add docs/superpowers/specs/2026-08-27-notes-context-menu-design.md
git commit -m "docs: record notes context-menu auto-close observation" --allow-empty
```

(Use `--allow-empty` only if Step 9 turned out to need no file change — otherwise omit that flag and let the commit carry the actual diff.)

---

## Out of scope (explicitly, from the design spec)

- No new Go handlers or routes.
- No changes to `internal/apps/notes/static/notes.js`.
- No changes to chevron/dot behavior.
- No separate tap-to-reveal-overlay behavior for touch — touch users rely on the menu, which already holds every action.
