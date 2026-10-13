# App Identity Colours Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every app its own colour, shown as a soft badge on its home tile and as a coloured glyph in the sidebar, with the active app's sidebar row tinted in its own colour.

**Architecture:** A map from app id to swatch name lives next to the icon map in `internal/ui/icons.go`. `ui.AppColorClass(id)` turns an id into the existing `swatch-c-<name>` class, and templates get it as `appColor`. The app icons switch from `var(--c-accent)` to `currentColor`. The home tile's icon wrapper and each sidebar item carry the class, so CSS can colour them from the swatch palette's `--swatch-soft` and a new `--swatch-line` variable. No Go type, handler or data changes.

**Tech Stack:** Go `html/template`, hand-drawn inline SVG, plain CSS in `internal/ui/static/app.css`. No build step.

**Spec:** [docs/superpowers/specs/2026-10-13-app-identity-colours-design.md](../specs/2026-10-13-app-identity-colours-design.md)

## Global Constraints

- **Colours are fixed by the spec.** notes→amber, paste→blue, reader→coral, later→purple, books→green, flash→pink, focus→teal, admin→gray, anything else→gray.
- **`--swatch-line` values are verbatim from spec §5.3.** These are the only new colour values. They live in the shared swatch block, which the spec approves as the one exception to app.css's "no new colours" rule.
- **Icon style is unchanged.** Line glyphs, `stroke-width="1.5"`, no new fills, no glyph redraws (warm-palette spec §3).
- **No `style=""` and no inline `<script>`.** The CSP forbids both, which is why colour arrives as a class.
- **Only the home page and sidebar change.** Nothing inside any app's own templates or CSS changes.
- **`internal/htmlassert` takes one qualifier per selector part.** Write `a.app-card div.swatch-c-blue`, never `.app-card-icon.swatch-c-blue`.
- **Full check stays green on every commit:**
  - `gofmt -l .` prints nothing
  - `go vet ./...`
  - `go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...`
  - `go mod tidy && git diff --exit-code go.mod go.sum`
  - `go test ./... -race -count=1`
- Work on a branch and open a PR. Never push to `main`, never merge.

## Review Focus

1. **A sidebar item without a swatch class.** If a future template forgets `appColor`, the item must still look like today: teal glyph, teal-tinted active row. Not invisible. Task 2 uses CSS fallbacks `var(--swatch-line, var(--c-accent))` and `var(--swatch-soft, var(--c-accent-bg))`.
2. **The fallback icon** (an unknown id or a coming-soon placeholder) must still render visibly once it's coloured gray. Task 1 pins `IconFor("some-future-app")` to use `currentColor` and no `var(--c-accent)`.
3. **The collapsed sidebar** shows glyphs only, so the coloured glyph is the only cue there. Task 3's manual check covers collapsed in both themes.
4. **Dark theme glyphs** must switch to the lighter `--swatch-line`. The dark override must be on the same `:root[data-theme="dark"] .swatch-c-*` selectors, or the light shade leaks into dark. Task 3 checks computed colours in dark.
5. **The active row on hover** behaves as today. The `:hover` rule (`.sidebar-item:not(.sidebar-item-disabled):hover`, specificity 0,3,0) beats `[aria-current]` (0,2,0), so hovering the active row shows `--c-bg-inset`, and moving off restores the app's soft tint. Task 2 changes neither selector, and Task 3 checks it by hovering.

---

### Task 1: Colour map, `appColor` template function, `currentColor` icons

**Files:**
- Modify: `internal/ui/icons.go`
- Modify: `internal/platform/render/render.go:108` (FuncMap)
- Test: `internal/ui/icons_test.go`
- Test: `cmd/onsuite/stack_test.go`
- Modify: `docs/developers/adding-an-app.md:252-253`

**Interfaces:**
- Produces: `func ui.AppColorClass(id string) string`, which returns `"swatch-c-<name>"`, or `"swatch-c-gray"` for an unknown id. Templates get it as `appColor`.
- Produces: every `ui.IconFor` SVG draws its glyph in `currentColor`.

- [ ] **Step 1: Write the failing tests in `internal/ui/icons_test.go`**

Append:

```go
func TestAppColorClassForKnownApps(t *testing.T) {
	want := map[string]string{
		"notes":  "swatch-c-amber",
		"paste":  "swatch-c-blue",
		"reader": "swatch-c-coral",
		"later":  "swatch-c-purple",
		"books":  "swatch-c-green",
		"flash":  "swatch-c-pink",
		"focus":  "swatch-c-teal",
		"admin":  "swatch-c-gray",
	}
	for id, w := range want {
		if got := ui.AppColorClass(id); got != w {
			t.Errorf("AppColorClass(%q) = %q, want %q", id, got, w)
		}
	}
}

func TestAppColorClassFallsBackToGray(t *testing.T) {
	if got := ui.AppColorClass("some-future-app"); got != "swatch-c-gray" {
		t.Errorf("AppColorClass(unknown) = %q, want swatch-c-gray", got)
	}
}

// Icons take their colour from CSS (the swatch class on their container),
// so none may hard-code the suite accent any more. var(--c-accent-bg) as a
// CSS fallback is fine; the bare accent is not.
func TestIconsDrawInCurrentColor(t *testing.T) {
	for _, id := range []string{"paste", "notes", "reader", "flash", "admin", "later", "focus", "books", "some-future-app"} {
		got := string(ui.IconFor(id))
		if strings.Contains(got, "var(--c-accent)") {
			t.Errorf("IconFor(%q) still uses var(--c-accent)", id)
		}
		if !strings.Contains(got, "currentColor") {
			t.Errorf("IconFor(%q) has no currentColor", id)
		}
	}
}
```

- [ ] **Step 2: Write the failing registry test in `cmd/onsuite/stack_test.go`**

Append after `TestComingSoonAppsExcludesRegisteredApps`:

```go
// Spec 2026-10-13-app-identity-colours §3: every registered app has a
// colour of its own (not the gray fallback), and no two share one — Admin's
// gray included — so apps stay recognisable at a glance as more are added.
func TestEveryRegisteredAppHasItsOwnColour(t *testing.T) {
	gray := ui.AppColorClass("admin")
	owner := map[string]string{gray: "admin"}
	for _, a := range registeredApps() {
		id := a.Meta().ID
		c := ui.AppColorClass(id)
		if c == gray {
			t.Errorf("%q has no colour in internal/ui/icons.go's appColors; give it an unused swatch (add one to the shared palette if all are taken)", id)
			continue
		}
		if prev, dup := owner[c]; dup {
			t.Errorf("%q and %q both use %s; every app needs its own colour", prev, id, c)
			continue
		}
		owner[c] = id
	}
}
```

- [ ] **Step 3: Run the tests and confirm they fail**

Run: `go test ./internal/ui/ ./cmd/onsuite/ -run 'AppColor|CurrentColor|OwnColour' -v`
Expected: build failure, `undefined: ui.AppColorClass`.

- [ ] **Step 4: Implement in `internal/ui/icons.go`**

1. Update the file's top comment to say it covers colours too:

```go
// Icon and colour lookup for the sidebar and home-page cards. Both are looked
// up by app id rather than carried on app.Meta, so a "coming soon"
// placeholder for an app that does not exist yet in code can still get one.
```

2. In every entry of `icons`:
   - Delete the `<rect x="2" y="2" width="20" height="20" rx="5" fill="none"/>` line. It has no stroke, so it never rendered.
   - Replace every `var(--c-accent)` with `currentColor`.
   - Leave all other attributes and paths exactly as they are.

   For example, `paste` becomes:

```go
	"paste": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<path d="M7 9h10M7 13h10M7 17h6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" fill="none"/>
	</svg>`,
```

   Keep the comments above `notes`, `later`, `books`, `focus` and `flash`. Edit the `flash` comment's "matches the other four tiles" only if it now reads wrong. It doesn't need changing.

3. Replace `fallbackIcon` with:

```go
const fallbackIcon = `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
	<rect x="1" y="1" width="10" height="10" rx="3" fill="currentColor"/>
	<rect x="13" y="1" width="10" height="10" rx="3" fill="var(--swatch-soft, var(--c-accent-bg))"/>
	<rect x="1" y="13" width="10" height="10" rx="3" fill="var(--swatch-soft, var(--c-accent-bg))"/>
	<rect x="13" y="13" width="10" height="10" rx="3" fill="currentColor"/>
</svg>`
```

4. Add after `IconFor`:

```go
// appColors gives each app one swatch from the shared palette (app.css,
// "Colour swatches") so it can be recognised at a glance on the home page and
// in the sidebar. Every registered app needs its own, unused entry —
// TestEveryRegisteredAppHasItsOwnColour in cmd/onsuite enforces it. When all
// swatches are taken, add a new one to the palette rather than reuse one.
// Gray is reserved for Admin and anything without an entry. See
// docs/superpowers/specs/2026-10-13-app-identity-colours-design.md.
var appColors = map[string]string{
	"notes":  "amber",
	"paste":  "blue",
	"reader": "coral",
	"later":  "purple",
	"books":  "green",
	"flash":  "pink",
	"focus":  "teal",
	"admin":  "gray",
}

// AppColorClass returns the swatch-c-<name> class carrying an app's colour,
// or swatch-c-gray for an id without one. A class rather than a style
// attribute because the CSP forbids style="".
func AppColorClass(id string) string {
	if c, ok := appColors[id]; ok {
		return "swatch-c-" + c
	}
	return "swatch-c-gray"
}
```

- [ ] **Step 5: Register the template function in `internal/platform/render/render.go`**

In the `funcs: template.FuncMap{` literal, add a line after `"icon": ui.IconFor,`:

```go
			"appColor":  ui.AppColorClass,
```

Run `gofmt -w internal/platform/render/render.go` so the map's alignment stays tidy.

- [ ] **Step 6: Run the tests and confirm they pass**

Run: `go test ./internal/ui/ ./cmd/onsuite/ ./internal/platform/render/ -v -run 'Icon|AppColor|OwnColour|Render|Page'`
Expected: PASS. That includes the existing `TestIconForIsDistinctPerApp` and `TestIconStrokeWidthIsConsistent`.

- [ ] **Step 7: Update `docs/developers/adding-an-app.md`**

Replace lines 252–253:

```markdown
Give the app an icon in `internal/ui/icons.go`, keyed by its ID. Without one
the sidebar and dashboard fall back to a generic tile.
```

with:

```markdown
Give the app an icon and a colour in `internal/ui/icons.go`, both keyed by
its ID. Without an icon the sidebar and dashboard fall back to a generic
tile. The colour must be a swatch no other app uses. If every swatch in
app.css's shared palette is taken, add a new one (light, soft, dark-soft and
`--swatch-line` values, each at least 3:1 against the surfaces it sits on)
rather than reuse one.
```

Then add a row to the "tests that will hold you to it" table, after `TestBuildStackServesHelpForEveryRegisteredApp`:

```markdown
| `TestEveryRegisteredAppHasItsOwnColour` | your app has no colour in `internal/ui/icons.go`, or shares one with another app |
```

- [ ] **Step 8: Run the full check, then commit**

Run the five full-check commands from Global Constraints. All must pass.

```bash
git add internal/ui/icons.go internal/ui/icons_test.go internal/platform/render/render.go cmd/onsuite/stack_test.go docs/developers/adding-an-app.md
git commit -m "feat(ui): a colour of its own for every app; icons draw in currentColor"
```

At this point icons render in the inherited text colour, because nothing sets one yet. Task 2 fixes that. Don't stop the branch here.

---

### Task 2: Templates and CSS for the soft badge and coloured sidebar

**Files:**
- Modify: `internal/ui/templates/home.html:10`
- Modify: `internal/ui/templates/base.html:114,116,127`
- Modify: `internal/ui/static/app.css`: `.sidebar-item` rules (~443–461), `.app-card-icon` (~640), and the swatch block (~2645–2666)
- Test: `cmd/onsuite/stack_test.go`
- Test: `internal/platform/render/render_test.go`

**Interfaces:**
- Consumes: the `appColor` template function and the `currentColor` icons from Task 1.
- Produces: `.app-card-icon` and `.sidebar-item` elements that carry `swatch-c-*`, plus the `--swatch-line` CSS variable on every `.swatch-c-*` class.

- [ ] **Step 1: Write the failing home-page test in `cmd/onsuite/stack_test.go`**

`testHomeHandler` registers a fake app with id `paste`, so its tile must be blue. Append:

```go
func TestHomeCardIconCarriesTheAppColour(t *testing.T) {
	rec := httptest.NewRecorder()
	testHomeHandler(t).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	doc := htmlassert.Parse(t, rec.Body.String())
	badge := doc.MustHave("a.app-card div.swatch-c-blue")
	if class, _ := htmlassert.Attr(badge, "class"); !strings.Contains(" "+class+" ", " app-card-icon ") {
		t.Errorf("paste's swatch-c-blue element has class %q, want it on the app-card-icon badge", class)
	}
}
```

- [ ] **Step 2: Write the failing sidebar test in `internal/platform/render/render_test.go`**

Append. It uses `testRenderer(t)` and the same shape as `TestPageRendersADocumentWithTheShell`:

```go
// Spec 2026-10-13-app-identity-colours §4.2: each sidebar item carries its
// app's swatch class (Admin's is gray), so CSS can colour the glyph and tint
// the active row.
func TestSidebarItemsCarryTheirAppColour(t *testing.T) {
	r := testRenderer(t)
	rec := httptest.NewRecorder()

	err := r.Page(rec, http.StatusOK, "error", render.Page{
		Title: "Not found",
		Shell: render.Shell{
			LoggedIn:  true,
			IsAdmin:   true,
			Username:  "ilia",
			CSRFToken: "tok123",
			ActiveApp: "notes",
			Apps: []render.NavItem{
				{ID: "paste", Name: "ON Paste", Path: "/paste/"},
				{ID: "notes", Name: "ON Notes", Path: "/notes/"},
			},
		},
		Data: map[string]any{"Status": 404, "Title": "Not found", "Message": "no such page"},
	})
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	doc := htmlassert.Parse(t, rec.Body.String())

	for sel, want := range map[string]string{
		"nav.shell-nav a.swatch-c-blue":  "ON Paste",
		"nav.shell-nav a.swatch-c-amber": "ON Notes",
		"nav.shell-nav a.swatch-c-gray":  "Admin",
	} {
		if got := htmlassert.Text(doc.MustHave(sel)); got != want {
			t.Errorf("%s text = %q, want %q", sel, got, want)
		}
	}
	if _, ok := htmlassert.Attr(doc.MustHave("nav.shell-nav a.swatch-c-amber"), "aria-current"); !ok {
		t.Error("the active app's item (notes, amber) is not marked aria-current")
	}
}
```

Before relying on it, check that `render.Shell` has an `IsAdmin` field (it's at `internal/platform/render/render.go:38`) and that the `error` page renders the shell, as the existing test shows.

- [ ] **Step 3: Run the tests and confirm they fail**

Run: `go test ./cmd/onsuite/ ./internal/platform/render/ -run 'CarriesTheAppColour|CarryTheirAppColour' -v`
Expected: FAIL with an htmlassert "no element matches" error for the `swatch-c-*` selectors.

- [ ] **Step 4: Update the templates**

`internal/ui/templates/home.html` line 10:

```html
			<div class="app-card-icon {{appColor .ID}}">{{icon .ID}}</div>
```

`internal/ui/templates/base.html` lines 114, 116 and 127:

```html
			<span class="sidebar-item sidebar-item-disabled {{appColor .ID}}" title="Coming soon">{{icon .ID}}<span class="sidebar-label">{{.Name}}</span></span>
```
```html
			<a class="sidebar-item {{appColor .ID}}" href="{{.Path}}"{{if eq .ID $.Shell.ActiveApp}} aria-current="page"{{end}}>{{icon .ID}}<span class="sidebar-label">{{.Name}}</span></a>
```
```html
			<a class="sidebar-item {{appColor "admin"}}" href="/admin/"{{if eq "admin" $.Shell.ActiveApp}} aria-current="page"{{end}}>{{icon "admin"}}<span class="sidebar-label">Admin</span></a>
```

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `go test ./cmd/onsuite/ ./internal/platform/render/ -v`
Expected: PASS, including all existing tests in both packages.

- [ ] **Step 6: Add `--swatch-line` to the shared swatch block in `internal/ui/static/app.css`**

Replace the block from `.swatch-c-teal   { --swatch: #1D9E75; …` through `:root[data-theme="dark"] .swatch-c-gray   { --swatch-soft: #444441; }` with the version below. The existing `--swatch` and `--swatch-soft` values are unchanged.

```css
.swatch-c-teal   { --swatch: #1D9E75; --swatch-soft: #E1F5EE; --swatch-line: #188D6A; }
.swatch-c-blue   { --swatch: #378ADD; --swatch-soft: #E6F1FB; --swatch-line: #2C7BC9; }
.swatch-c-purple { --swatch: #7F77DD; --swatch-soft: #EEEDFE; --swatch-line: #7067D0; }
.swatch-c-pink   { --swatch: #D4537E; --swatch-soft: #FBEAF0; --swatch-line: #BF4870; }
.swatch-c-coral  { --swatch: #D85A30; --swatch-soft: #FAECE7; --swatch-line: #C25029; }
.swatch-c-amber  { --swatch: #BA7517; --swatch-soft: #FAEEDA; --swatch-line: #A76813; }
.swatch-c-green  { --swatch: #639922; --swatch-soft: #EAF3DE; --swatch-line: #558A1C; }
.swatch-c-gray   { --swatch: #888780; --swatch-soft: #F1EFE8; --swatch-line: #7A7973; }
:root[data-theme="dark"] .swatch-c-teal   { --swatch-soft: #085041; --swatch-line: #5DCAA5; }
:root[data-theme="dark"] .swatch-c-blue   { --swatch-soft: #0C447C; --swatch-line: #85B7EB; }
:root[data-theme="dark"] .swatch-c-purple { --swatch-soft: #3C3489; --swatch-line: #AFA9EC; }
:root[data-theme="dark"] .swatch-c-pink   { --swatch-soft: #72243E; --swatch-line: #ED93B1; }
:root[data-theme="dark"] .swatch-c-coral  { --swatch-soft: #712B13; --swatch-line: #F0997B; }
:root[data-theme="dark"] .swatch-c-amber  { --swatch-soft: #633806; --swatch-line: #EF9F27; }
:root[data-theme="dark"] .swatch-c-green  { --swatch-soft: #27500A; --swatch-line: #97C459; }
:root[data-theme="dark"] .swatch-c-gray   { --swatch-soft: #444441; --swatch-line: #B4B2A9; }
```

Also extend the block's leading comment with one sentence:

```css
 * --swatch-line is the glyph colour for app icons (home badge, sidebar):
 * at least 3:1 on --swatch-soft and on the page/sidebar surfaces in both
 * themes. See docs/superpowers/specs/2026-10-13-app-identity-colours-design.md §6.
```

- [ ] **Step 7: Style the sidebar in `internal/ui/static/app.css`**

Directly after the `.sidebar-item { … }` rule, add:

```css
/* Each app's glyph in its own colour (spec 2026-10-13-app-identity-colours).
 * The fallbacks keep an item without a swatch class looking as it used to. */
.sidebar-item svg { flex: none; color: var(--swatch-line, var(--c-accent)); }
```

Replace the existing `[aria-current]` rule with the version below, keeping the same selector. The `:hover` rule is more specific and still wins while the row is hovered, exactly as today:

```css
/* The active app's row picks up its own soft tint; the label stays in the
 * normal text colour so the tint, not the text, carries the identity. */
.sidebar-item[aria-current="page"] {
	background: var(--swatch-soft, var(--c-accent-bg));
	color: var(--c-text);
	font-weight: 600;
}
```

- [ ] **Step 8: Style the home badge in `internal/ui/static/app.css`**

Replace `.app-card-icon { width: 2.5rem; height: 2.5rem; }` with:

```css
/* A soft badge in the app's own colour, so each tile is recognisable before
 * its name is read (spec 2026-10-13-app-identity-colours §4.1). The glyph is
 * still the thin line icon; only its colour and the tint behind it are per app. */
.app-card-icon {
	display: flex;
	align-items: center;
	justify-content: center;
	width: 2.5rem;
	height: 2.5rem;
	border-radius: calc(var(--radius) + var(--s-1));
	background: var(--swatch-soft, var(--c-accent-bg));
	color: var(--swatch-line, var(--c-accent));
}

.app-card-icon svg { width: 1.625rem; height: 1.625rem; }
```

- [ ] **Step 9: Run the full check, then commit**

Run the five full-check commands from Global Constraints. All must pass.

```bash
git add internal/ui/templates/home.html internal/ui/templates/base.html internal/ui/static/app.css cmd/onsuite/stack_test.go internal/platform/render/render_test.go
git commit -m "feat(ui): soft colour badges on home tiles, coloured sidebar glyphs and active tint"
```

---

### Task 3: Visual and contrast verification, screenshots, spec note

**Files:**
- Modify: `docs/user/images/*.png` (re-captured)
- Modify: `docs/superpowers/specs/2026-09-01-warm-palette-icon-refresh-design.md` (§3)

**Interfaces:**
- Consumes: everything from Tasks 1–2. Produces no code.

- [ ] **Step 1: Seed and start a demo server**

From the repository root (the same flow as `docs/screenshots/README.md`):

```bash
VERSION=5.1.0
SEED=$(mktemp -d)/demo
go run ./docs/screenshots/seed --data-dir $SEED
go build -ldflags "-X main.version=$VERSION" -o $SEED/onsuite ./cmd/onsuite
$SEED/onsuite serve --addr :8308 --data-dir $SEED &
SERVER=$!
until curl -fs http://localhost:8308/healthz >/dev/null; do sleep 0.2; done
```

Keep the server running through Step 3.

- [ ] **Step 2: Check the contrast figures against the live CSS**

Open `http://localhost:8308/` in the browser with the demo session. The seed writes `$SEED/demo-session`; set its value as the session cookie, the same way `docs/screenshots/capture` does. Then run this in the page, once in light theme and once in dark (toggle the theme from the user menu and reload):

```js
(() => {
  const lum = c => { const [r,g,b] = c.match(/\d+(\.\d+)?/g).slice(0,3).map(Number).map(v => { v/=255; return v <= 0.03928 ? v/12.92 : ((v+0.055)/1.055)**2.4; }); return 0.2126*r + 0.7152*g + 0.0722*b; };
  const cr = (a, b) => { const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p); return ((x + 0.05) / (y + 0.05)).toFixed(2); };
  const bgOf = el => { for (; el; el = el.parentElement) { const c = getComputedStyle(el).backgroundColor; if (!/rgba\(0, 0, 0, 0\)|transparent/.test(c)) return c; } return 'rgb(255,255,255)'; };
  return [...document.querySelectorAll('.app-card-icon, .sidebar-item')].map(el => {
    const fg = getComputedStyle(el.querySelector('svg')).color;
    return `${el.className.padEnd(48)} ${cr(fg, bgOf(el))}`;
  }).join('\n');
})()
```

Expected: every ratio is at least 3.00. Also navigate to an app (for example `/notes/`), hover its active sidebar row, and confirm it shows the usual hover background and returns to the app's soft tint when the pointer leaves. Record both tables (light and dark) for the PR description.

- [ ] **Step 3: Check by eye**

In both themes, check the home page, the expanded sidebar, the collapsed sidebar (sidebar toggle button), and phone width (≤ 600px).

Expected:
- Seven distinct badges.
- Admin's shield is gray.
- The active app's row is tinted in its own colour.
- No glyph is invisible or teal-by-fallback.

- [ ] **Step 4: Re-capture the screenshots**

```bash
go run ./docs/screenshots/capture --session-file $SEED/demo-session
kill $SERVER
git diff --stat docs/user/images
```

Look at `docs/user/images/dashboard.png` and at least one in-app shot (for example `notes-outline.png`) to confirm the new badge and sidebar colours. Every shot that shows the sidebar is expected to change. If a shot changed for any other reason, investigate before committing it.

- [ ] **Step 5: Note the change in the warm-palette spec**

In `docs/superpowers/specs/2026-09-01-warm-palette-icon-refresh-design.md` §3, after the bullet ending "still tinted with `var(--c-accent)`", add:

```markdown
  — **superseded for colour on 2026-10-13:** app-tile and sidebar icons now
  draw in `currentColor`, coloured per app; see
  [App Identity Colours](2026-10-13-app-identity-colours-design.md). Line
  style and stroke weight are unchanged.
```

- [ ] **Step 6: Commit**

```bash
git add docs/user/images docs/superpowers/specs/2026-09-01-warm-palette-icon-refresh-design.md
git commit -m "docs: re-capture screenshots with app identity colours; note in warm-palette spec"
```

- [ ] **Step 7: Open the PR**

Push the branch and open a PR. In the body:
- Summarise the change.
- Paste the light and dark contrast tables from Step 2.
- Embed or link the new `dashboard.png`.
- Link the spec.

Don't merge.
