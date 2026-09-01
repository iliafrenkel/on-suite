# Warm Palette & Icon Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the suite's cool slate/teal palette with a warm cream/teal/orange one, redraw every hand-drawn icon to one consistent thin-line style, and rebuild the ON Notes toolbar as real icon buttons instead of plain text links with glyph suffixes — including a new orange due-count badge on the Due button.

**Architecture:** This is a token-value swap plus a markup/CSS rebuild of one toolbar, following the existing "apps consume shared tokens" discipline in `app.css`. The one piece of new backend logic is a due-item count feeding the badge, wired through the existing HTMX out-of-band pattern that `show-completed-toggle` already uses.

**Tech Stack:** Go `html/template` (server-rendered), hand-written CSS custom properties, HTMX for partial swaps, vanilla JS (`notes.js`) for CSP-safe interactivity. No build step, no new dependencies.

## Global Constraints

- Apps must not introduce new colours or spacing values outside `internal/ui/static/app.css`'s `:root`/`[data-theme="dark"]` tokens (`app.css:1-6`).
- CSP is `script-src 'self'` with no `unsafe-inline` (`internal/platform/web/middleware.go:128`) — no inline `on*="..."` attributes; all JS must live in a `.js` file served same-origin.
- No new build tooling, no CDN dependencies, no icon library — icons stay hand-drawn inline SVG.
- Every icon uses `stroke-width: 1.5`, `stroke-linecap`/`stroke-linejoin: round`, `stroke="currentColor"` (or an explicit `var(--c-accent)`), no solid glyph fills except the documented "notes" bullet-dot exception (Task 2).
- `--c-accent-attention` (orange) is used only for due/overdue/attention badges — never on routine buttons.

---

### Task 1: Palette tokens

**Files:**
- Modify: `internal/ui/static/app.css:1-22` (light tokens + header comment)
- Modify: `internal/ui/static/app.css:93-106` (dark tokens)

**Interfaces:**
- Produces: every existing token name unchanged (`--c-bg`, `--c-bg-subtle`, `--c-bg-inset`, `--c-border`, `--c-border-firm`, `--c-text`, `--c-text-dim`, `--c-text-faint`, `--c-accent`, `--c-accent-bg`, `--c-danger`, `--c-danger-bg`), plus two new tokens: `--c-accent-attention`, `--c-accent-attention-bg`. Later tasks (2, 3, 4, 5, 6) consume these by name.

- [ ] **Step 1: Replace the light-mode token block and header comment**

Replace `app.css` lines 1–22:

```css
/* ON Suite — quiet, calm, and a little more spacious than it used to be.
 *
 * Everything is driven by the tokens in :root. Apps must not introduce new
 * colours or spacing values; they compose these. That constraint is what
 * makes four separately-built apps look like one suite.
 */

:root {
	/* Greys carry the interface; the accent is a muted slate/teal, used
	 * sparingly and on purpose. */
	--c-bg:          #ffffff;
	--c-bg-subtle:   #f4f3f1;
	--c-bg-inset:    #eceae6;
	--c-border:      #e2e0dc;
	--c-border-firm: #c9c7c1;
	--c-text:        #2b2a28;
	--c-text-dim:    #6b6a67;
	--c-text-faint:  #8a8983;
	--c-accent:      #4d7570;
	--c-accent-bg:   #e4ecea;
	--c-danger:      #a51d2d;
	--c-danger-bg:   #fbeaec;
```

with:

```css
/* ON Suite — warm and a little more alive than it used to be.
 *
 * Everything is driven by the tokens in :root. Apps must not introduce new
 * colours or spacing values; they compose these. That constraint is what
 * makes four separately-built apps look like one suite.
 */

:root {
	/* A warm cream base carries the interface. Teal is the everyday accent
	 * (links, buttons, active states); --c-accent-attention (orange) is
	 * deliberately separate and used only for things that need attention —
	 * a due/overdue badge — so it stays meaningful by staying rare. */
	--c-bg:          #faf3ea;
	--c-bg-subtle:   #f2e9db;
	--c-bg-inset:    #efe0cf;
	--c-border:      #e3d3bf;
	--c-border-firm: #c9b49a;
	--c-text:        #3a2f26;
	--c-text-dim:    #6b5d4f;
	--c-text-faint:  #948573;
	--c-accent:      #2f6f6a;
	--c-accent-bg:   #d9e8e6;
	--c-accent-attention:    #d9773f;
	--c-accent-attention-bg: #f7e4d5;
	--c-danger:      #a51d2d;
	--c-danger-bg:   #fbeaec;
```

- [ ] **Step 2: Replace the dark-mode token block**

Replace `app.css` lines 93–106 (the `:root[data-theme="dark"]` block; line numbers shift by +4 after Step 1, so locate by content, not line number):

```css
:root[data-theme="dark"] {
	--c-bg:          #1c1d1f;
	--c-bg-subtle:   #242527;
	--c-bg-inset:    #2b2c2f;
	--c-border:      #34373a;
	--c-border-firm: #4a4d51;
	--c-text:        #e8e6e2;
	--c-text-dim:    #a3a29c;
	--c-text-faint:  #79786f;
	--c-accent:      #7fa39d;
	--c-accent-bg:   #233330;
	--c-danger:      #f08c95;
	--c-danger-bg:   #3a1e22;
}
```

with:

```css
:root[data-theme="dark"] {
	--c-bg:          #241d17;
	--c-bg-subtle:   #2b231b;
	--c-bg-inset:    #2e2621;
	--c-border:      #3d322a;
	--c-border-firm: #52443a;
	--c-text:        #efe6da;
	--c-text-dim:    #b8a996;
	--c-text-faint:  #8f8071;
	--c-accent:      #3d8983;
	--c-accent-bg:   #1e3532;
	--c-accent-attention:    #e08a52;
	--c-accent-attention-bg: #3d2a1c;
	--c-danger:      #f08c95;
	--c-danger-bg:   #3a1e22;
}
```

- [ ] **Step 3: Verify the build still succeeds**

Run: `go build ./...`
Expected: exits 0, no output (CSS is not compiled, but this confirms nothing else broke)

- [ ] **Step 4: Commit**

```bash
git add internal/ui/static/app.css
git commit -m "style: swap cool slate/teal palette for warm cream/teal/orange"
```

---

### Task 2: Icon style refresh (app-tile icons)

**Files:**
- Modify: `internal/ui/icons.go`
- Modify: `internal/ui/icons_test.go` (add a consistency test)

**Interfaces:**
- Consumes: `--c-accent`, `--c-accent-bg` tokens (Task 1)
- Produces: `IconFor(id string) template.HTML` unchanged signature; every icon's SVG now uses `stroke-width="1.5"` (except the "notes" bullet dots, a documented exception)

- [ ] **Step 1: Write the failing consistency test**

Add to `internal/ui/icons_test.go`:

```go
func TestIconStrokeWidthIsConsistent(t *testing.T) {
	for _, id := range []string{"paste", "notes", "reader", "admin", "flash"} {
		got := string(ui.IconFor(id))
		if strings.Contains(got, `stroke-width="1.8"`) {
			t.Errorf("IconFor(%q) still uses stroke-width 1.8, want the shared 1.5 line-icon weight", id)
		}
		if !strings.Contains(got, `stroke-width="1.5"`) {
			t.Errorf("IconFor(%q) has no stroke-width=1.5 stroke", id)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/... -run TestIconStrokeWidthIsConsistent -v`
Expected: FAIL — current icons use `stroke-width="1.8"`

- [ ] **Step 3: Redraw every icon at the shared stroke weight**

Replace the entire `icons` map body in `internal/ui/icons.go` (lines 9–35):

```go
var icons = map[string]template.HTML{
	"paste": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="var(--c-accent-bg)"/>
		<path d="M7 9h10M7 13h10M7 17h6" stroke="var(--c-accent)" stroke-width="1.5" stroke-linecap="round" fill="none"/>
	</svg>`,
	// The three dots are the one deliberate exception to "no solid fills":
	// at this scale a hollow circle reads as an empty ring, not a bullet
	// point, so the glyph itself would stop looking like notes/an outline.
	"notes": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="var(--c-accent-bg)"/>
		<circle cx="8" cy="9" r="1.3" fill="var(--c-accent)"/>
		<circle cx="8" cy="13" r="1.3" fill="var(--c-accent)"/>
		<circle cx="8" cy="17" r="1.3" fill="var(--c-accent)"/>
		<path d="M11.5 9h7M11.5 13h7M11.5 17h4" stroke="var(--c-accent)" stroke-width="1.5" stroke-linecap="round"/>
	</svg>`,
	"reader": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="var(--c-accent-bg)"/>
		<circle cx="7" cy="17" r="1.5" fill="var(--c-accent)"/>
		<path d="M7 12.5a4.5 4.5 0 0 1 4.5 4.5" stroke="var(--c-accent)" stroke-width="1.5" fill="none" stroke-linecap="round"/>
		<path d="M7 8a9 9 0 0 1 9 9" stroke="var(--c-accent)" stroke-width="1.5" fill="none" stroke-linecap="round"/>
	</svg>`,
	"admin": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="var(--c-accent-bg)"/>
		<path d="M12 6l5 2.2v3.4c0 3-2.1 5.2-5 6.4-2.9-1.2-5-3.4-5-6.4V8.2L12 6z" fill="none" stroke="var(--c-accent)" stroke-width="1.5" stroke-linejoin="round"/>
	</svg>`,
	// Was a solid-filled bolt; redrawn as an outline so it matches the other
	// four tiles instead of being the one duotone/filled glyph among them.
	"flash": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="var(--c-accent-bg)"/>
		<path d="M13 6 8 13h4l-1 5 6-8h-4l1-4z" fill="none" stroke="var(--c-accent)" stroke-width="1.5" stroke-linejoin="round" stroke-linecap="round"/>
	</svg>`,
}
```

`fallbackIcon` (the 2×2 logo-mark tile, lines 37–42) is unchanged — it is the brand mark, not an app icon, and is out of scope per the design spec's non-goals.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/... -v`
Expected: PASS — all of `TestIconForKnownApps`, `TestIconForUnknownAppFallsBackToTile`, `TestIconForIsDistinctPerApp`, `TestIconStrokeWidthIsConsistent`

- [ ] **Step 5: Commit**

```bash
git add internal/ui/icons.go internal/ui/icons_test.go
git commit -m "style: redraw app-tile icons at one consistent thin-line stroke weight"
```

---

### Task 3: Toolbar CSS foundation

**Files:**
- Modify: `internal/ui/static/app.css:866-922` (the existing `.toolbar-btn` block and its glyph-suffix rules)

**Interfaces:**
- Consumes: `--c-accent-attention`, `--c-accent-attention-bg` (Task 1)
- Produces: CSS classes `.toolbar-icon`, `.toolbar-sep`, `.toolbar-due-badge`, `.notes-import-label` for Tasks 5 and 6's markup to use. Removes `#show-completed-toggle::before` and `.toolbar-btn-nav::after`/`.toolbar-btn-disclosure::after` (superseded by real icons in Tasks 5–6).

- [ ] **Step 1: Replace the toolbar-btn block and remove the glyph-suffix rules**

Replace `app.css` lines 866–922 (from `.toolbar-btn {` through the end of `.toolbar-btn-disclosure::after`):

```css
.toolbar-btn {
	display: inline-flex;
	align-items: center;
	gap: var(--s-1);
	padding: var(--s-1) var(--s-2);
	border: 1px solid transparent;
	border-radius: var(--radius);
	font-size: var(--fs-sm);
	font-weight: 400;
	color: var(--c-text-dim);
	text-decoration: none;
	background: none;
	cursor: pointer;
	line-height: 1.4;
	transition: background 0.15s, color 0.15s, border-color 0.15s;
}

.toolbar-btn:hover {
	color: var(--c-text);
	background: var(--c-bg-subtle);
}

/* Active toggle state */
.toolbar-btn-active,
.notes-help-menu[open] .notes-help-toggle {
	color: var(--c-accent);
	background: var(--c-accent-bg);
	border-color: transparent;
	font-weight: 500;
}

.toolbar-btn-active:hover {
	color: var(--c-accent);
	background: var(--c-accent-bg);
}

/* Every toolbar icon shares one size/weight, set once here rather than per
 * icon — the whole point of Task 2's redraw is that these no longer need
 * per-icon tuning. */
.toolbar-icon {
	width: 1rem;
	height: 1rem;
	flex-shrink: 0;
	stroke: currentColor;
	stroke-width: 1.5;
	stroke-linecap: round;
	stroke-linejoin: round;
	fill: none;
}

/* A thin divider between a toolbar's action groups — see outline.html's
 * .notes-toolbar-actions, which puts one between the view actions (Due,
 * Archive, Export) and the data action (Import). */
.toolbar-sep {
	width: 1px;
	align-self: stretch;
	background: var(--c-border);
	margin: 0 var(--s-1);
}

/* The Due button's due-count badge. Orange (--c-accent-attention) is used
 * nowhere else in the toolbar, so its appearance here stays meaningful.
 * :empty, not a Go-side conditional class, because the OOB-swapped copy
 * (renderOutlineFragment) and the full-page copy must look identical
 * whether or not there happen to be any due items right now. */
.toolbar-due-badge {
	display: inline-block;
	background: var(--c-accent-attention);
	color: #fff;
	border-radius: 999px;
	font-size: var(--fs-2xs);
	font-weight: 600;
	line-height: 1.5;
	padding: 0 0.45em;
	margin-left: 0.1em;
}
.toolbar-due-badge:empty {
	display: none;
}

/* Import's icon-button trigger for the hidden file input — see
 * outline.html and notes.js's initImportAutoSubmit. Shares .toolbar-btn's
 * look via that class on the <label> itself; this just makes the label
 * behave like the button it visually is. */
.notes-import-label {
	cursor: pointer;
}
```

- [ ] **Step 2: Remove the now-superseded checkbox-prefix rule**

Delete these lines from `app.css` (immediately above the block replaced in Step 1):

```css
/* Checkbox prefix for toggle button */
#show-completed-toggle::before {
	content: "☐ ";
}
#show-completed-toggle.toolbar-btn-active::before {
	content: "☑ ";
}
```

- [ ] **Step 3: Verify the build still succeeds**

Run: `go build ./...`
Expected: exits 0

- [ ] **Step 4: Commit**

```bash
git add internal/ui/static/app.css
git commit -m "style: rebuild toolbar-btn CSS for real icon buttons, drop glyph-suffix hacks"
```

---

### Task 4: Due-count badge backend

**Files:**
- Modify: `internal/apps/notes/due.go` (add `DueBadgeCount`)
- Create: `internal/apps/notes/due_test.go` additions (same file, new tests)
- Modify: `internal/apps/notes/view.go` (add `DueCount` field)
- Modify: `internal/apps/notes/handlers.go` (`renderOutline`, `renderOutlineFragment`)
- Modify: `internal/apps/notes/handlers_test.go` (`assertOnlyToggleIsOOB` whitelist + new tests)

**Interfaces:**
- Produces: `notes.DueBadgeCount(rows []Node, today time.Time) int`; `outlineView.DueCount int` field, populated by both outline-rendering handlers.
- Consumes: `Store.Due(ctx, userID) ([]Node, error)` (already exists, `due.go:75`); `Node.DueOn string` field.

- [ ] **Step 1: Write the failing test for DueBadgeCount**

Add to `internal/apps/notes/due_test.go`:

```go
func TestDueBadgeCountCountsOverdueAndToday(t *testing.T) {
	today := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	rows := []notes.Node{
		{ID: 1, DueOn: "2026-03-01"}, // overdue
		{ID: 2, DueOn: "2026-03-10"}, // today
		{ID: 3, DueOn: "2026-03-14"}, // this week, not counted
		{ID: 4, DueOn: "2026-04-01"}, // later, not counted
	}

	got := notes.DueBadgeCount(rows, today)
	if got != 2 {
		t.Errorf("DueBadgeCount = %d, want 2 (overdue + today only)", got)
	}
}

func TestDueBadgeCountOfNoRowsIsZero(t *testing.T) {
	got := notes.DueBadgeCount(nil, time.Now())
	if got != 0 {
		t.Errorf("DueBadgeCount(nil) = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/apps/notes/... -run TestDueBadgeCount -v`
Expected: FAIL with "undefined: notes.DueBadgeCount"

- [ ] **Step 3: Implement DueBadgeCount**

Add to `internal/apps/notes/due.go`, after `GroupByDue` (after line 67):

```go
// DueBadgeCount reports how many rows are overdue or due today — the
// "needs attention now" subset of GroupByDue's four buckets, without
// needing the ancestor crumbs DueRow carries for the /notes/due page
// itself. Used for the toolbar's due-count badge (outline.html).
func DueBadgeCount(rows []Node, today time.Time) int {
	todayStr := today.Format("2006-01-02")
	count := 0
	for _, n := range rows {
		if n.DueOn <= todayStr {
			count++
		}
	}
	return count
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/apps/notes/... -run TestDueBadgeCount -v`
Expected: PASS

- [ ] **Step 5: Add DueCount to outlineView**

In `internal/apps/notes/view.go`, add a field after `ShareURL` (end of the `outlineView` struct, before the closing `}` on line 36):

```go
	// DueCount is DueBadgeCount's result, computed once per request for the
	// toolbar's Due button badge — see renderOutline/renderOutlineFragment
	// (handlers.go).
	DueCount int
```

- [ ] **Step 6: Wire DueCount into renderOutline**

In `internal/apps/notes/handlers.go`, in `renderOutline` (starts line 85), insert before the `flat, err := a.store.Outline(...)` line (line 115):

```go
	dueRows, err := a.store.Due(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view.DueCount = DueBadgeCount(dueRows, time.Now())

```

- [ ] **Step 7: Wire DueCount into renderOutlineFragment**

In the same file, in `renderOutlineFragment` (starts line 139), replace:

```go
func (a *App) renderOutlineFragment(w http.ResponseWriter, r *http.Request, userID, rootID int64, showCompleted bool) {
	flat, err := a.store.Outline(r.Context(), userID, rootID, showCompleted)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	visible := hideDone(flat, showCompleted)
	view := outlineView{
		CSRFToken:     web.CSRFToken(r.Context()),
		Root:          Node{ID: rootID},
		ShowCompleted: showCompleted,
		HiddenCount:   len(flat) - len(visible),
		OOB:           true,
	}
```

with:

```go
func (a *App) renderOutlineFragment(w http.ResponseWriter, r *http.Request, userID, rootID int64, showCompleted bool) {
	flat, err := a.store.Outline(r.Context(), userID, rootID, showCompleted)
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
		HiddenCount:   len(flat) - len(visible),
		DueCount:      DueBadgeCount(dueRows, time.Now()),
		OOB:           true,
	}
```

- [ ] **Step 8: Update assertOnlyToggleIsOOB to allow the due badge too**

In `internal/apps/notes/handlers_test.go`, replace the function (lines 449–462):

```go
// assertOnlyToggleIsOOB asserts that the show-completed toggle is the only
// element anywhere in body carrying hx-swap-oob. A structural fragment
// legitimately marks that one toolbar button out of band — see
// renderOutlineFragment — but nothing else should ever be: an accidental
// hx-swap-oob on, say, .outline-list or an .outline-row would make htmx
// silently strip that chunk out of the response before swapping it in.
func assertOnlyToggleIsOOB(t *testing.T, body string) {
	t.Helper()
	for _, n := range htmlassert.Parse(t, body).QueryAll("[hx-swap-oob]") {
		if id, _ := htmlassert.Attr(n, "id"); id != "show-completed-toggle" {
			t.Errorf("unexpected hx-swap-oob element (id=%q); only show-completed-toggle may be out of band", id)
		}
	}
}
```

with:

```go
// assertOnlyToggleIsOOB asserts that the show-completed toggle and the
// due-count badge are the only elements anywhere in body carrying
// hx-swap-oob. Both toolbar elements legitimately get marked out of band —
// see renderOutlineFragment — but nothing else should ever be: an
// accidental hx-swap-oob on, say, .outline-list or an .outline-row would
// make htmx silently strip that chunk out of the response before swapping
// it in.
func assertOnlyToggleIsOOB(t *testing.T, body string) {
	t.Helper()
	allowed := map[string]bool{"show-completed-toggle": true, "due-badge": true}
	for _, n := range htmlassert.Parse(t, body).QueryAll("[hx-swap-oob]") {
		if id, _ := htmlassert.Attr(n, "id"); !allowed[id] {
			t.Errorf("unexpected hx-swap-oob element (id=%q); only show-completed-toggle and due-badge may be out of band", id)
		}
	}
}
```

- [ ] **Step 9: Write a failing test for the badge appearing and updating**

Add to `internal/apps/notes/handlers_test.go`:

```go
// TestDueBadgeShowsOverdueAndTodayCount is spec-driven: the toolbar's Due
// button shows a count of items needing attention now (overdue + today),
// on both the full page and the HTMX fragment a structural op returns.
func TestDueBadgeShowsOverdueAndTodayCount(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	overdue := s.seed(t, s.Alice, notes.RootID, "overdue task")
	if err := s.Store.SetDue(ctx, s.Alice.User.ID, overdue, "2000-01-01"); err != nil {
		t.Fatal(err)
	}
	other := s.seed(t, s.Alice, notes.RootID, "no date")

	doc := s.Get(t, s.Alice, "/notes/")
	badge := doc.MustHave("#due-badge")
	if got := htmlassert.Text(badge); got != "1" {
		t.Errorf("due badge = %q, want \"1\"", got)
	}

	// A structural fragment carries the same badge, out of band, so it
	// stays in sync after e.g. an indent that doesn't touch due dates.
	frag := s.PostHX(t, s.Alice, "/notes/"+itoa(other)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(other)}, "title": {"no date"}, "note": {""},
	}).Body.String()
	fragBadge := htmlassert.Parse(t, frag).MustHave("#due-badge")
	if got := htmlassert.Text(fragBadge); got != "1" {
		t.Errorf("fragment due badge = %q, want \"1\"", got)
	}
	if got, ok := htmlassert.Attr(fragBadge, "hx-swap-oob"); !ok || got != "true" {
		t.Errorf("fragment due badge hx-swap-oob = %q, ok=%v, want \"true\"", got, ok)
	}
}

// TestDueBadgeIsEmptyWithNothingDue: the badge element is always present
// (so an OOB swap can always find it and clear it) but empty, so
// .toolbar-due-badge:empty hides it.
func TestDueBadgeIsEmptyWithNothingDue(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.Alice, notes.RootID, "no date")

	badge := s.Get(t, s.Alice, "/notes/").MustHave("#due-badge")
	if got := htmlassert.Text(badge); got != "" {
		t.Errorf("due badge = %q, want empty", got)
	}
}

// TestDueBadgeExcludesFutureItems: only overdue + today count, not "this
// week" or "later" — the badge would otherwise be on almost all the time,
// diluting the orange accent's meaning.
func TestDueBadgeExcludesFutureItems(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.Alice, notes.RootID, "next month")
	future := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	if err := s.Store.SetDue(context.Background(), s.Alice.User.ID, id, future); err != nil {
		t.Fatal(err)
	}

	badge := s.Get(t, s.Alice, "/notes/").MustHave("#due-badge")
	if got := htmlassert.Text(badge); got != "" {
		t.Errorf("due badge = %q, want empty — the only due item is a month out", got)
	}
}
```

Confirm `notes.Node`, `itoa`, `s.Store`, `s.Alice.User.ID` are already used elsewhere in this test file (they are — `itoa` and this exact fixture pattern appear throughout `handlers_test.go`).

- [ ] **Step 10: Run tests to verify they fail correctly, then implement, then pass**

Run: `go test ./internal/apps/notes/... -run "TestDueBadge" -v`
Expected: FAIL — `#due-badge` doesn't exist yet (Task 5 adds the template define; this test intentionally waits for that). Note this as a cross-task dependency: Steps 1–8 of this task compile and test cleanly on their own (`DueBadgeCount`, `DueCount` field, handler wiring, `assertOnlyToggleIsOOB`), but Step 9's new tests only pass once Task 5's template define exists. Leave them failing and continue to Task 5; return here to confirm PASS after Task 5's Step 3.

- [ ] **Step 11: Commit**

```bash
git add internal/apps/notes/due.go internal/apps/notes/due_test.go internal/apps/notes/view.go internal/apps/notes/handlers.go internal/apps/notes/handlers_test.go
git commit -m "feat(notes): compute a due/overdue count for the toolbar's due badge"
```

---

### Task 5: Toolbar partial — real check icon and due-badge template

**Files:**
- Modify: `internal/apps/notes/templates/toolbar.partial.html`

**Interfaces:**
- Consumes: `.toolbar-icon`, `.toolbar-due-badge` CSS classes (Task 3); `outlineView.DueCount`, `.OOB` (Task 4)
- Produces: template define `"due-badge"`, callable as `{{template "due-badge" .}}` (view struct directly) or `{{template "due-badge" .Data}}` (from a page-context template) — same calling convention `"show-completed-toggle"` already uses.

- [ ] **Step 1: Replace show-completed-toggle's pseudo-element checkbox with a real icon**

Replace the `show-completed-toggle` define in `toolbar.partial.html`:

```gotemplate
{{define "show-completed-toggle"}}<button type="submit" id="show-completed-toggle" class="toolbar-btn{{if .ShowCompleted}} toolbar-btn-active{{end}}"{{if .OOB}} hx-swap-oob="true"{{end}}
        name="show_completed" value="{{if .ShowCompleted}}0{{else}}1{{end}}">{{if .ShowCompleted}}Hide completed{{else}}Show completed{{end}}</button>{{end}}
```

with:

```gotemplate
{{define "show-completed-toggle"}}<button type="submit" id="show-completed-toggle" class="toolbar-btn{{if .ShowCompleted}} toolbar-btn-active{{end}}"{{if .OOB}} hx-swap-oob="true"{{end}}
        name="show_completed" value="{{if .ShowCompleted}}0{{else}}1{{end}}"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M4 12l5 5L20 6"/></svg>{{if .ShowCompleted}}Hide completed{{else}}Show completed{{end}}</button>{{end}}
```

- [ ] **Step 2: Add the due-badge template define**

Add to `toolbar.partial.html`, after the `show-completed-toggle` define:

```gotemplate
{{/* due-badge is the toolbar Due button's count of overdue+today items
     (outlineView.DueCount, notes.DueBadgeCount). Always rendered, even at
     zero, so an out-of-band swap can always find #due-badge to update —
     .toolbar-due-badge:empty (app.css) hides it when there's nothing to
     show. Same OOB discipline as show-completed-toggle: the fragment sets
     .OOB, a full-page render does not. */}}
{{define "due-badge"}}<span id="due-badge" class="toolbar-due-badge"{{if .OOB}} hx-swap-oob="true"{{end}}>{{if .DueCount}}{{.DueCount}}{{end}}</span>{{end}}
```

- [ ] **Step 3: Build and run the notes package tests (including the pending Task 4 due-badge tests)**

Run: `go build ./... && go test ./internal/apps/notes/... -v`
Expected: build succeeds; `TestDueBadgeShowsOverdueAndTodayCount`, `TestDueBadgeIsEmptyWithNothingDue`, `TestDueBadgeExcludesFutureItems` still FAIL — Task 6 hasn't wired `{{template "due-badge" ...}}` into `outline.html`'s Due button yet, so `#due-badge` still doesn't render on the page. This is expected; continue to Task 6.

- [ ] **Step 4: Commit**

```bash
git add internal/apps/notes/templates/toolbar.partial.html
git commit -m "feat(notes): add due-badge template and a real check icon for show-completed-toggle"
```

---

### Task 6: Outline toolbar markup — icon buttons, separator, icon-button import

**Files:**
- Modify: `internal/apps/notes/templates/outline.html:36-63` (the toolbar block)

**Interfaces:**
- Consumes: `.toolbar-icon`, `.toolbar-sep`, `.notes-import-label` CSS (Task 3); `"due-badge"` template define (Task 5); `.Data.CSRFToken`, `.Data.Root.ID` (existing `outlineView` fields)
- Produces: new element id `notes-import-file` on the (now visually hidden) file input, for Task 7's `notes.js` to bind to.

- [ ] **Step 1: Replace the toolbar-actions block**

Replace `outline.html` lines 36–63 (from `<div class="notes-toolbar">` through the `<summary>` opening tag of the Shortcuts disclosure):

```gotemplate
	<div class="notes-toolbar">
		{{template "notes-search-box" (dict "Query" "" "Autofocus" false)}}
		{{/* Grouped together so .notes-toolbar's own justify-content puts the
		     search box alone on the left and this cluster together on the
		     right, rather than spreading three items evenly. The prefs form
		     needs no class of its own: .notes-toolbar-actions already lays
		     this out. The extra nesting is invisible to the out-of-band swap
		     above: htmx finds #show-completed-toggle by id anywhere in the
		     document, whatever it is wrapped in. */}}
		<div class="notes-toolbar-actions">
			<a href="/notes/due" class="toolbar-btn toolbar-btn-nav"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 3"/></svg>Due{{template "due-badge" .Data}}</a>
			<a href="/notes/archive" class="toolbar-btn toolbar-btn-nav"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M21 8H3l2-4h14z"/><path d="M5 8v11h14V8"/><path d="M10 12h4"/></svg>Archive</a>
			<a href="/notes/export?root={{.Data.Root.ID}}" class="toolbar-btn toolbar-btn-nav"><svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3v12"/><path d="M7 10l5 5 5-5"/><path d="M5 21h14"/></svg>Export</a>
			<span class="toolbar-sep" aria-hidden="true"></span>
			<form method="post" action="/notes/import" enctype="multipart/form-data" class="notes-import"
			      hx-post="/notes/import" hx-target="#outline" hx-swap="innerHTML" hx-encoding="multipart/form-data">
				<input type="hidden" name="csrf_token" value="{{.Data.CSRFToken}}">
				<input type="hidden" name="root" value="{{.Data.Root.ID}}">
				{{/* The file input is visually hidden but stays in the layout and
				     the accessibility tree (.visually-hidden, app.css) — its
				     label is what's visible, styled as a toolbar-btn. Choosing
				     a file submits the form immediately (notes.js's
				     initImportAutoSubmit), so there is no separate "Import"
				     click step. */}}
				<label for="notes-import-file" class="toolbar-btn notes-import-label">
					<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 21V9"/><path d="M7 14l5-5 5 5"/><path d="M5 3h14"/></svg>Import
				</label>
				<input type="file" id="notes-import-file" name="file" accept=".md,text/markdown" aria-label="Import a Markdown file" required class="visually-hidden">
			</form>
			<form method="post" action="/notes/prefs"
			      hx-post="/notes/prefs" hx-target="#outline" hx-swap="innerHTML">
				<input type="hidden" name="csrf_token" value="{{.Data.CSRFToken}}">
				<input type="hidden" name="root" value="{{.Data.Root.ID}}">
				{{template "show-completed-toggle" .Data}}
			</form>
			<details class="notes-help-menu">
				<summary class="toolbar-btn toolbar-btn-disclosure notes-help-toggle" aria-label="Keyboard shortcuts">Shortcuts<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg></summary>
```

Everything after that `<summary>` line (the `notes-help-panel` block) is unchanged — this replacement stops right before it and picks back up at the existing `<div class="notes-help-panel">`.

- [ ] **Step 2: Remove the old submit-button markup and now-unneeded input styling**

Confirm the old `<input type="file" ... required><button type="submit" class="toolbar-btn">Import</button>` pair (previously two separate elements) is gone — Step 1's replacement already removes the old submit button entirely, since choosing a file now submits the form directly (Task 7).

Also remove the now-unused `.notes-import input[type="file"]` CSS rule (`app.css`, in the `.notes-import` block) — it styled the visible native file input's width, which no longer applies since the input is `.visually-hidden`:

Replace in `app.css`:

```css
.notes-import {
	display: inline-flex;
	align-items: center;
	gap: var(--s-1);
}
.notes-import input[type="file"] {
	font-size: var(--fs-sm);
	max-width: 10rem;
}
```

with:

```css
.notes-import {
	display: inline-flex;
	align-items: center;
	gap: var(--s-1);
}
```

- [ ] **Step 3: Run the full notes test suite**

Run: `go build ./... && go test ./internal/apps/notes/... -v`
Expected: PASS, including Task 4's `TestDueBadgeShowsOverdueAndTodayCount`, `TestDueBadgeIsEmptyWithNothingDue`, `TestDueBadgeExcludesFutureItems`, and Task 4's updated `TestRenderedOverlayIsNotOutOfBandOnAnOrdinaryRender` (via `assertOnlyToggleIsOOB`). If any import-related test fails because it asserted on the old two-element file-input/button markup, update that assertion to match the new `<label class="notes-import-label">` + hidden `<input id="notes-import-file">` structure — check for such tests with:

Run: `grep -n "notes-import\|Browse\|type=\"file\"" internal/apps/notes/handlers_test.go`

If any exist, update them to query `#notes-import-file` and `.notes-import-label` instead of the removed submit button.

- [ ] **Step 4: Commit**

```bash
git add internal/apps/notes/templates/outline.html internal/ui/static/app.css internal/apps/notes/handlers_test.go
git commit -m "feat(notes): rebuild the outline toolbar as icon buttons with a due badge"
```

---

### Task 7: Import auto-submit (CSP-safe)

**Files:**
- Modify: `internal/apps/notes/static/notes.js`

**Interfaces:**
- Consumes: `#notes-import-file` (Task 6)
- Produces: `initImportAutoSubmit()`, called at the bottom of the IIFE alongside the file's other `init*()` functions.

- [ ] **Step 1: Add the handler, following the file's existing init-function pattern**

Add to `internal/apps/notes/static/notes.js`, immediately before the closing `initDragToMove();\n})();` call block (i.e. as a new function ahead of the final call list, with its own call added to that list):

```javascript
	// Choosing a file submits the form immediately — there is no visible
	// native file input to show a separate "Import" button next to (CSP has
	// no unsafe-inline, so this can't be an inline onchange attribute; see
	// outline.html's notes-import form).
	function initImportAutoSubmit() {
		var input = document.getElementById("notes-import-file");
		if (!input) return;
		input.addEventListener("change", function () {
			if (input.files.length > 0) {
				input.form.requestSubmit();
			}
		});
	}
```

Then update the call list at the very end of the file:

```javascript
	initFocusSync();
	initKeyboard();
	initPaste();
	initPasteErrors();
	initDragToMove();
	initImportAutoSubmit();
})();
```

- [ ] **Step 2: Verify no test coverage regresses**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS (this file has no Go test harness of its own; it's exercised indirectly through any import-related handler tests, which Task 6's Step 3 already confirmed pass)

- [ ] **Step 3: Commit**

```bash
git add internal/apps/notes/static/notes.js
git commit -m "feat(notes): auto-submit the import form when a file is chosen"
```

---

### Task 8: End-to-end verification

**Files:** none (verification only)

- [ ] **Step 1: Full test suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass, no vet warnings

- [ ] **Step 2: gofmt check**

Run: `gofmt -l .`
Expected: no output (nothing unformatted)

- [ ] **Step 3: Confirm ON Paste has zero diff (success criterion 5)**

Run: `git diff main --stat -- internal/apps/paste`
Expected: no output — Paste inherits the new palette/icons purely through `app.css` and `icons.go`, with no template changes of its own.

- [ ] **Step 4: Manual browser check**

Start the app (see the project's existing run instructions — `AGENTS.md` or `cmd/onsuite`), sign in, and visually confirm:
- The home page and sidebar show the new warm cream background and redrawn icons in both light and dark theme (toggle via the settings menu)
- ON Notes' toolbar shows icon+label buttons with a visible separator before Import
- Setting a due date in the past on any bullet makes the Due button's badge show a count; toggling "Hide completed" does not change that count; indenting/outdenting a bullet keeps the badge in sync without a full reload
- Choosing a file in Import triggers the upload immediately, no separate click needed
- "Hide completed"/"Show completed" shows a check icon, not a Unicode ☐/☑

- [ ] **Step 5: Push the branch (already pushed — this step is a final sanity check)**

Run: `git log --oneline origin/design/warm-palette-icon-refresh..design/warm-palette-icon-refresh`
Expected: shows every commit from Tasks 1–7, none of them already on `origin`

- [ ] **Step 6: Open the pull request**

```bash
git push
gh pr create --title "Warm palette & icon refresh (ON Notes toolbar)" --body "$(cat <<'EOF'
## Summary
- Swaps the cool slate/teal palette for a warm cream base with teal as the everyday accent and a new orange (--c-accent-attention) reserved for due/attention badges
- Redraws every app-tile icon at one consistent thin-line stroke weight (1.5px)
- Rebuilds the ON Notes toolbar as real icon buttons (Due/Archive/Export/Import/Hide-completed/Shortcuts), replacing plain-text links with `::after` glyphs
- Adds a due/overdue count badge on the Due button, kept in sync via the existing HTMX out-of-band pattern

Spec: docs/superpowers/specs/2026-09-01-warm-palette-icon-refresh-design.md

## Test plan
- [ ] `go build ./... && go vet ./... && go test ./...` all pass
- [ ] `gofmt -l .` reports nothing
- [ ] Manual check: light/dark theme, toolbar icons, due badge live-updates, import auto-submit, show/hide-completed check icon
EOF
)"
```
