# Braun Folio Visual Redesign (Phase 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the **Braun Folio** visual identity across ON Suite's shared design system, shell chrome, Home, Login, and flagship showcases on ON Focus (Dieter Rams precision instrument) and ON Later (Quanta editorial reading room).

**Architecture:** Retire the whole-UI Palette and Font switchers in favor of one opinionated Light/Dark stone-and-obsidian palette and a four-role self-hosted typography system (`IBM Plex Serif`, `IBM Plex Sans`, `Source Serif 4`, `IBM Plex Mono`). Apply crisp `1px` architectural hairlines, `2px` precision corners, restrained 45° top-right chamfers, and CSS-token-driven geometric SVG illustrations strictly within `internal/ui` and the `focus`/`later` templates, preserving zero-import leaf boundaries and strict CSP.

**Tech Stack:** Go (`html/template`, `embed`), pure CSS custom properties (`internal/ui/static/app.css`), vanilla JS (`internal/ui/static/theme.js`), HTMX, self-hosted `.woff2` fonts (SIL OFL 1.1).

**Spec:** [docs/superpowers/specs/2026-11-23-braun-folio-visual-redesign-design.md](../specs/2026-11-23-braun-folio-visual-redesign-design.md)

## Global Constraints

- Zero new Go dependencies; no Node, no npm, no JS build step.
- Strict CSP: zero inline `<script>` tags and zero inline `style="..."` attributes in any HTML template or SVG illustration; all SVG colors use `var(--c-*)` CSS custom properties or CSS classes.
- Package boundaries: `internal/ui` remains a zero-import embedded leaf; apps never import each other; platform never imports apps (`go test ./internal/arch/...` must stay green).
- Self-hosted fonts only: all `.woff2` files live under `internal/ui/static/fonts/` with Latin and Cyrillic subsets; no external CDN links.
- WCAG AA contrast (4.5:1+ for normal text, 3:1+ for large text/UI graphics) in both Light and Dark themes; `color-scheme: light` and `color-scheme: dark` explicitly declared.
- Keyboard accessibility & reduced motion: `:focus-visible` remains clearly visible on every control including chamfered buttons; transitions respect `@media (prefers-reduced-motion: reduce)`.
- Progressive enhancement: forms, `<details>` menus, and navigation work without JavaScript.
- Branch discipline: all commits go on `feat/braun-folio-design` (never directly to `main`), using Conventional Commits.

## Review Focus

1. **Stale browser cookies (`onsuite_palette`, `onsuite_font`)**: Returning users whose browsers still send old palette/font cookies must receive clean 200 responses with no errors, no stray `data-palette`/`data-font` attributes, and intact `data-theme` behavior (pinned in Task 1).
2. **Focus ring clipping on chamfered controls**: `clip-path: polygon(...)` on `.primary` buttons can clip standard `outline`; `:focus-visible` must use an unclipped inset/drop-shadow focus treatment so keyboard focus is unmistakably visible (pinned in Task 2).
3. **Strict CSP compliance on new inline SVGs**: The new Home hero, Login art plate, Shell emblem, and Focus dial SVGs must contain zero `style=` attributes and zero `<script>` elements (pinned in Tasks 3, 4, and 5).
4. **RTL / non-Latin drop-cap safety in ON Later**: The editorial `::first-letter` drop-cap must not corrupt RTL (`dir="rtl"`) or link-only articles; scope it strictly to `.later-article:not([dir="rtl"]) .later-body > p:first-of-type::first-letter` and verify link-only views stay unaffected (pinned in Task 5).
5. **ON Focus JS hooks & swatch compatibility**: Adding `.focus-chassis` and the outer radial tick ring to `focus/templates/run.html` must preserve every `data-focus-*` attribute, `#focus-run-config`, `#focus-run-dialog`, and `.swatch-c-*` class so timer execution and chimes remain 100% functional (pinned in Task 5).

---

### Task 1: Retire Whole-UI Palette & Font Switchers (Server, JS & Tests)

**Files:**
- Modify: `internal/platform/render/render.go:45-55`
- Modify: `internal/platform/web/prefs.go:1-56`
- Modify: `internal/platform/web/errors.go:65-78`
- Modify: `internal/platform/web/login.go:260-275`
- Modify: `internal/platform/app/app.go:120-135`
- Modify: `internal/ui/templates/base.html:1-106`
- Modify: `internal/ui/static/theme.js:1-70,310-323`
- Test: `internal/platform/web/prefs_test.go`, `internal/platform/app/app_test.go`, `internal/platform/render/render_test.go`, `internal/platform/web/login_test.go`

**Interfaces:**
- Consumes: Existing `web.ThemeFrom`, `web.SidebarCollapsedFrom`, `render.Shell`.
- Produces: Simplified `render.Shell` (carrying `Theme` and `SidebarCollapsed`, without `Font` or `Palette`) and `<html>` carrying only `data-theme="{{.Shell.Theme}}"`.

- [ ] **Step 1: Update tests to assert the simplified Display preferences (no `data-font` / `data-palette`, stale cookies harmless)**

Update `internal/platform/render/render_test.go` so the user-menu test asserts `[data-theme-switch]` exists and `[data-font-switch]` / `[data-palette-switch]` do not exist, and the `<html>` attribute test asserts `data-theme` is set while `data-font` and `data-palette` are absent:

```go
// In internal/platform/render/render_test.go (Display settings test):
doc.MustHave(panel + " [data-theme-switch]")
doc.MustNotHave(panel + " [data-font-switch]")
doc.MustNotHave(panel + " [data-palette-switch]")

// In internal/platform/render/render_test.go (<html> attributes test):
if v, ok := htmlassert.Attr(doc.MustHave("html"), "data-theme"); !ok || v != "dark" {
	t.Errorf("data-theme = %q (ok=%v), want dark", v, ok)
}
if _, ok := htmlassert.Attr(doc.MustHave("html"), "data-font"); ok {
	t.Error("<html> must not carry data-font")
}
if _, ok := htmlassert.Attr(doc.MustHave("html"), "data-palette"); ok {
	t.Error("<html> must not carry data-palette")
}
```

In `internal/platform/web/login_test.go`, update the cookie test so sending stale `onsuite_palette=ink` and `onsuite_font=serif` cookies alongside `onsuite_theme=dark` succeeds with 200, sets `data-theme="dark"`, and omits `data-palette` and `data-font`. Remove `TestFontFrom` and `TestPaletteFrom` from `internal/platform/web/prefs_test.go` and remove `Font`/`Palette` assertions from `internal/platform/app/app_test.go`.

- [ ] **Step 2: Run affected tests to verify they fail before implementation**

Run: `go test ./internal/platform/web/... ./internal/platform/render/... ./internal/platform/app/...`
Expected: FAIL (templates still render `data-font`/`data-palette` and switchers).

- [ ] **Step 3: Remove `Font` and `Palette` from Go code, `base.html`, and `theme.js`**

1. `internal/platform/render/render.go`: Remove `Font string` and `Palette string` from `type Shell struct`.
2. `internal/platform/web/prefs.go`: Remove `FontCookieName`, `PaletteCookieName`, `FontFrom`, and `PaletteFrom`.
3. `internal/platform/app/app.go`, `internal/platform/web/errors.go`, `internal/platform/web/login.go`: Remove `Font:` and `Palette:` field assignments when building `render.Shell`.
4. `internal/ui/templates/base.html`:
   - Change line 2 to `<html lang="en" data-theme="{{.Shell.Theme}}">`.
   - Remove the `Palette` and `Font` `.field` blocks from `.shell-menu-settings`, keeping the `Theme` (`Light` / `Dark`) block.
5. `internal/ui/static/theme.js`: Remove `initFontSwitch()`, `initPaletteSwitch()`, and their calls at the bottom of the IIFE.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/platform/web/... ./internal/platform/render/... ./internal/platform/app/... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/platform/render/ internal/platform/web/ internal/platform/app/ internal/ui/templates/base.html internal/ui/static/theme.js
git commit -m "refactor(platform): retire whole-UI palette and font switchers"
```

---

### Task 2: Self-Host IBM Plex Fonts & Implement Braun Folio Tokens, Typography & Controls

**Files:**
- Create: `internal/ui/static/fonts/ibm-plex-serif-{400,500,600,400i}-{latin,cyrillic}.woff2`
- Create: `internal/ui/static/fonts/ibm-plex-sans-{400,500,600}-{latin,cyrillic}.woff2`
- Create: `internal/ui/static/fonts/ibm-plex-mono-{400,500,600}-{latin,cyrillic}.woff2`
- Delete: `internal/ui/static/fonts/inter*.woff2`, `internal/ui/static/fonts/jetbrains-mono*.woff2`, `internal/ui/static/fonts/ia-writer-duo-s-*.woff2`
- Modify: `internal/ui/static/VENDOR.md`
- Modify: `internal/ui/static/app.css:1-285,574-646,4045-4056`
- Test: `internal/ui/fonts_test.go`, `internal/ui/color_scheme_test.go`

**Interfaces:**
- Consumes: Clean `data-theme="light|dark"` on `<html>` from Task 1.
- Produces: Complete Braun Folio CSS custom properties (`--c-bg`, `--c-bg-subtle`, `--c-bg-inset`, `--c-border`, `--c-border-firm`, `--c-text`, `--c-text-dim`, `--c-text-faint`, `--c-accent`, `--c-accent-bg`, `--c-accent-attention`, `--c-accent-attention-bg`, `--c-danger`, `--c-danger-bg`, `--font-display`, `--font-ui`, `--font-body`, `--font-mono`, `--radius: 2px`, `--chamfer-sm: 8px`, `--chamfer-lg: 22px`), chamfered `.primary` button styling with unclipped `:focus-visible`, and harmonized app color tokens.

- [ ] **Step 1: Update `internal/ui/fonts_test.go` to pin the four-role Braun Folio typography and unclipped chamfer focus rule**

Replace `internal/ui/fonts_test.go` with tests verifying:
1. All `@font-face` `url("/static/fonts/...")` references in `app.css` resolve to existing files in `ui.Static()`.
2. `body` uses `var(--font-ui)`, `h1` uses `var(--font-display)`, reading surfaces use `var(--font-body)`, and `code, pre, kbd` use `var(--font-mono)`.
3. No `[data-font=` or `[data-palette=` blocks remain in `app.css`.
4. Primary chamfered buttons define a `:focus-visible` rule with an unclipped focus indicator (`box-shadow` or `filter`).

```go
package ui_test

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/ui"
)

func TestTypographyRolesAndBundledFonts(t *testing.T) {
	src, err := fs.ReadFile(ui.Static(), "app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(src)

	if strings.Contains(css, `[data-font=`) || strings.Contains(css, `[data-palette=`) {
		t.Error("app.css must not contain retired data-font or data-palette selectors")
	}

	checks := []struct {
		name string
		re   string
	}{
		{"body uses --font-ui", `(?s)\nbody \{[^}]*var\(--font-ui\)`},
		{"headings use --font-display", `(?s)\nh1[^{]*\{[^}]*var\(--font-display\)`},
		{"reading surfaces use --font-body", `(?s)font-family:\s*var\(--font-body\)`},
		{"code uses --font-mono", `(?s)code,\s*pre,\s*kbd\s*\{[^}]*var\(--font-mono\)`},
		{"primary button has unclipped focus-visible", `(?s)button\.primary:focus-visible[^{]*\{[^}]*box-shadow:`},
	}
	for _, c := range checks {
		if !regexp.MustCompile(c.re).MatchString(css) {
			t.Errorf("app.css missing contract: %s", c.name)
		}
	}

	urlRe := regexp.MustCompile(`url\("/static/(fonts/[^"]+)"\)`)
	matches := urlRe.FindAllStringSubmatch(css, -1)
	if len(matches) == 0 {
		t.Fatal("app.css declares no self-hosted font URLs")
	}
	for _, m := range matches {
		if _, err := fs.Stat(ui.Static(), m[1]); err != nil {
			t.Errorf("missing bundled font file %q: %v", m[1], err)
		}
	}
}
```

- [ ] **Step 2: Run `internal/ui` tests to verify the new test fails**

Run: `go test ./internal/ui/... -v`
Expected: FAIL (old `data-font`/`data-palette` selectors present, `--font-display` missing).

- [ ] **Step 3: Bundle IBM Plex `.woff2` font files and remove unused fonts**

Download official SIL OFL 1.1 `.woff2` subsets (Latin + Cyrillic) for:
- `IBM Plex Serif` (400, 500, 600, 400 italic)
- `IBM Plex Sans` (400, 500, 600)
- `IBM Plex Mono` (400, 500, 600)

Save them under `internal/ui/static/fonts/`, remove unused `inter*.woff2`, `jetbrains-mono*.woff2`, and `ia-writer-duo-s-*.woff2`, and document the bundled fonts and source/license in `internal/ui/static/VENDOR.md`. Keep all `source-serif-4*.woff2` files.

- [ ] **Step 4: Update `internal/ui/static/app.css` tokens, `@font-face`, typography, buttons & reduced-motion**

1. Replace `:root` and `:root[data-theme="dark"]` with the exact Braun Folio palette tokens from Spec §2.1, preserving `color-scheme: light` and `color-scheme: dark`.
2. Remove `[data-font=...]`, `[data-palette="ink"]`, and `:root[data-palette="ink"] .focus-runner` blocks.
3. Declare `@font-face` rules for `IBM Plex Serif`, `IBM Plex Sans`, `IBM Plex Mono` (Latin + Cyrillic `unicode-range`s) alongside existing `Source Serif 4`.
4. Set:
   - `--font-display: 'IBM Plex Serif', Georgia, "Times New Roman", serif;`
   - `--font-ui: 'IBM Plex Sans', system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;`
   - `--font-body: 'Source Serif 4', Georgia, "Times New Roman", serif;`
   - `--font-mono: 'IBM Plex Mono', ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;`
   - `--radius: 2px;`
   - `--chamfer-sm: 8px;`
   - `--chamfer-lg: 22px;`
5. Set `h1` (and display headings) to `font-family: var(--font-display); font-weight: 500; letter-spacing: -0.02em;`.
6. Update form controls and buttons (`app.css:574-646`):
   - Inputs use `background: var(--c-bg-inset); border: 1px solid var(--c-border); border-radius: var(--radius);` shifting to `background: var(--c-bg-subtle); border-color: var(--c-accent);` on focus.
   - `button.primary, .button.primary` uses `background: var(--c-accent-attention); color: #fff; border-color: transparent; font-family: var(--font-mono); font-size: var(--fs-xs); font-weight: 600; letter-spacing: 0.08em; text-transform: uppercase; clip-path: polygon(0 0, calc(100% - var(--chamfer-sm)) 0, 100% var(--chamfer-sm), 100% 100%, 0 100%);`
   - Add unclipped focus ring for chamfered primary buttons:
     ```css
     button.primary:focus-visible,
     .button.primary:focus-visible {
         outline: none;
         box-shadow: inset 0 0 0 2px var(--c-bg), inset 0 0 0 4px var(--c-text);
     }
     ```
7. Update harmonized `.app-color-*` values to Spec §2.1 and add `@media (prefers-reduced-motion: reduce)` resetting transitions.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/... ./internal/arch/... -count=1 -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/
git commit -m "feat(ui): add Braun Folio palette, IBM Plex typography, and precision controls"
```

---

### Task 3: Architectural Shell, Chamfered Mark, Favicon & Sidebar

**Files:**
- Modify: `internal/ui/static/favicon.svg`
- Modify: `internal/ui/templates/base.html:49-145`
- Modify: `internal/ui/static/app.css:286-573,1912-1982`
- Test: `internal/platform/render/render_test.go`, `internal/ui/templates_test.go`

**Interfaces:**
- Consumes: Braun Folio tokens and typography from Task 2.
- Produces: Chamfered ON Suite SVG emblem, updated `favicon.svg`, monospaced `.shell-crumbs`, recessed `.conn-indicator` pill, and architectural `.app-sidebar` with numbered indices and 2px Verdigris active rail.

- [ ] **Step 1: Add render/template tests pinning the new shell emblem, CSP compliance, and sidebar structure**

In `internal/ui/templates_test.go` (or a new test function in it), verify that none of the embedded templates under `internal/ui/templates/` contain inline `style=` attributes or inline `<script>` tags (excluding `<script src=...>` and `<script type="application/json">`).
In `internal/platform/render/render_test.go`, verify `.shell-logo svg polygon` exists (chamfered mark) and `.app-sidebar .sidebar-item` renders with clean structure.

- [ ] **Step 2: Run tests to verify new assertions fail**

Run: `go test ./internal/ui/... ./internal/platform/render/... -count=1`
Expected: FAIL (`.shell-logo svg polygon` not yet present).

- [ ] **Step 3: Update `favicon.svg`, `base.html`, and Shell/Sidebar CSS in `app.css`**

1. `internal/ui/static/favicon.svg`: Replace the 4-square SVG with the chamfered emblem (`<polygon points="2,2 17,2 22,7 22,22 2,22" fill="none" stroke="#1c1a17" stroke-width="1.8"/>`, Verdigris `<rect x="6" y="6" width="6" height="6" fill="#1c665d"/>`, Vermilion `<circle cx="15.5" cy="15.5" r="3" fill="#c23d15"/>`).
2. `internal/ui/templates/base.html`:
   - Update `.shell-logo` SVG to the chamfered emblem using `stroke="var(--c-text)"`, `fill="var(--c-accent)"`, `fill="var(--c-accent-attention)"` (no inline `style=` attributes).
   - Keep `#shell-crumb-tail` and all existing `data-*` attributes (`data-conn-indicator`, `data-shell-user-menu`, `data-sidebar`, `data-sidebar-toggle`) intact.
   - Add a `<span class="conn-label" aria-hidden="true">Online</span>` inside `.conn-indicator` (hidden on very narrow viewports) so it renders as the Braun recessed status pill.
3. `internal/ui/static/app.css` (lines 286–573):
   - Style `.shell-bar` on `--c-bg-subtle` with `1px` bottom border, uppercase tracked `.shell-mark`, vertical hairline separator before monospaced uppercase `.shell-crumbs`, and recessed `.conn-indicator` pill.
   - Style `.app-sidebar` on `--c-bg-subtle` with `1px` right hairline, crisp `2px` toggle button, and `.sidebar-item` with `border-left: 2px solid transparent;` turning `border-left-color: var(--c-accent); background: var(--c-accent-bg);` on `[aria-current="page"]`.
   - Ensure collapsed (`[data-collapsed]`) and narrow-viewport rules remain clean and aligned.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/... ./internal/platform/render/... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/static/favicon.svg internal/ui/templates/base.html internal/ui/static/app.css internal/ui/templates_test.go internal/platform/render/render_test.go
git commit -m "feat(ui): redesign shell bar, chamfered emblem, and architectural sidebar"
```

---

### Task 4: Home Hero Plate, Geometric SVG Illustration, Hairline Directory & Split-Plate Login

**Files:**
- Modify: `internal/ui/templates/home.html`
- Modify: `internal/ui/templates/login.html`
- Modify: `internal/ui/static/app.css:694-738` (and Login plate rules + narrow viewport overrides)
- Test: `internal/platform/render/render_test.go`, `internal/platform/web/login_test.go`

**Interfaces:**
- Consumes: Braun Folio tokens, `--chamfer-lg`, and `.primary` button styling from Task 2.
- Produces: `.home-hero` chamfered architectural banner with inline CSS-token-driven SVG art, `.home-grid` 1px hairline app directory, and `.login-plate` split architectural login screen.

- [ ] **Step 1: Add HTML structure tests for Home hero/directory and Login split plate**

In `internal/platform/render/render_test.go`, assert `home.html` renders `.home-hero`, `.home-hero-art svg`, and `.home-grid .app-card`.
In `internal/platform/web/login_test.go`, assert `login.html` renders `.login-plate`, `.login-art svg`, and `.login-form-pane form[action="/login"]` with `#username`, `#password`, and `button.primary[type="submit"]`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/platform/render/... ./internal/platform/web/... -count=1`
Expected: FAIL (`.home-hero` and `.login-plate` not yet in templates).

- [ ] **Step 3: Implement `home.html`, `login.html`, and their Braun Folio CSS**

1. `internal/ui/templates/home.html`:
   - Add `.home-hero` containing the monospaced kicker (`Household Suite`), `<h1>` in `IBM Plex Serif` with italic emphasis (`Quiet tools, <em>exact</em> proportion.`), concise subtitle, and the bespoke cantilevered-plane SVG illustration (using CSS classes like `.art-grid-line`, `.art-plane-back`, `.art-plane-front`, `.art-signal` — zero inline `style=` attributes).
   - Keep `.home-grid` and `.app-card` structure (so existing links and `appColor .ID` classes continue to work), adding an expanding bottom `.app-card-rule` accent bar.
2. `internal/ui/templates/login.html`:
   - Wrap the page in `.login-stage > .login-plate` with `.login-art` (obsidian art panel with ON Suite emblem, cantilevered-plane + orbital-ring SVG illustration, and tagline) on the left and `.login-form-pane` (existing `{{with .Data.Error}}` notice, CSRF input, `next` input, `username`, `password`, and `button.primary`) on the right.
3. `internal/ui/static/app.css`:
   - Style `.home-hero` with `--chamfer-lg` clip-path, `1px solid var(--c-border-firm)`, two-column grid collapsing to 1 column under `780px`.
   - Style `.home-grid` as a unified `1px` hairline architectural grid (`gap: 1px; background: var(--c-border); border: 1px solid var(--c-border);`) with `IBM Plex Serif` card titles and hover-expanding `.app-card-rule`.
   - Style `.login-plate` with `--chamfer-lg` clip-path, dark obsidian `.login-art` (`#1b1917` light / `#0f0e0d` dark), and calm `.login-form-pane`, collapsing to a single column on mobile.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/... ./internal/platform/render/... ./internal/platform/web/... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/templates/home.html internal/ui/templates/login.html internal/ui/static/app.css internal/platform/render/render_test.go internal/platform/web/login_test.go
git commit -m "feat(ui): add Braun Folio Home hero, hairline directory, and split-plate Login"
```

---

### Task 5: Flagship Showcase — ON Focus (Dieter Rams Instrument) & ON Later (Quanta Editorial)

**Files:**
- Modify: `internal/apps/focus/templates/run.html`
- Modify: `internal/apps/later/templates/article.html`
- Modify: `internal/ui/static/app.css:3677-3847,3896-4217`
- Test: `internal/apps/focus/handler_test.go`, `internal/apps/later/handler_test.go`

**Interfaces:**
- Consumes: Braun Folio tokens, `--chamfer-lg`, `--font-display`, `--font-body`, `--font-mono`, and `.primary` button styling from Task 2.
- Produces: Chamfered `.focus-chassis` precision dial runner in ON Focus and Quanta-style editorial article header/prose/notes styling in ON Later, preserving 100% of existing JS hooks.

- [ ] **Step 1: Add handler/HTML tests pinning the new Focus chassis/dial markup, Later editorial header, and zero inline styles**

In `internal/apps/focus/handler_test.go`, add a test asserting `GET /focus/run/...` renders `.focus-chassis`, `.focus-ring-ticks`, `.focus-ring-well`, all existing `data-focus-*` hooks (`data-focus-bar`, `data-focus-digits`, `data-focus-pause`), and zero `style=` attributes.
In `internal/apps/later/handler_test.go`, add a test asserting `GET /later/a/...` renders `.later-article-header`, `.later-article-kicker`, `.later-article-title`, `.later-article-meta`, and zero `style=` attributes.

- [ ] **Step 2: Run focus and later tests to verify the new assertions fail**

Run: `go test ./internal/apps/focus/... ./internal/apps/later/... -count=1`
Expected: FAIL (`.focus-chassis` and `.later-article-kicker` not yet rendered).

- [ ] **Step 3: Update `focus/templates/run.html`, `later/templates/article.html`, and their sections in `app.css`**

1. `internal/apps/focus/templates/run.html`:
   - Wrap `.focus-ring` and `.focus-controls` inside `.focus-chassis` with `.focus-chassis-head` (monospaced label + `.focus-led-row` containing `.focus-led`).
   - Inside `.focus-ring-svg` (`viewBox="0 0 240 240"`), add `<circle class="focus-ring-ticks" cx="120" cy="120" r="114"></circle>` outside `.focus-ring-track` and `.focus-ring-bar` (`cx="120" cy="120" r="104"`, keeping `pathLength="1000" stroke-dashoffset="1000" data-focus-bar`).
   - Add `.focus-ring-well` around `.focus-ring-inner` and quiet `.focus-keys` `<kbd>` hints below `.focus-controls`.
   - Keep every `data-focus-*` attribute, `#focus-run-config`, `#focus-run-dialog`, and `.swatch-c-{{$d.Color}}` class intact.
2. `internal/apps/later/templates/article.html`:
   - Add `.later-article-kicker` above `.later-article-title` displaying site + read-time in monospaced uppercase, and wrap `.later-article-meta` + `.later-article-tags` cleanly inside `.later-article-meta-bar`.
   - Keep every `data-later-*` attribute, `later-font-*`, `later-size-*`, `later-width-*`, `later-align-*`, and link-only branch intact.
3. `internal/ui/static/app.css`:
   - Style `.focus-chassis` (chamfered `--c-bg-subtle`, `1px solid var(--c-border-firm)`), `.focus-ring-ticks` (`stroke-dasharray: 1.5 10.44`), `.focus-ring-well` (recessed circular well), tabular `IBM Plex Mono` `.focus-digits`, rectangular `.focus-dot` bars, and `.focus-led` Vermilion indicator.
   - Style `.later-topbar` (hairline bar + `2px` Verdigris `.later-progress`), `.later-article-kicker` (short Vermilion prefix rule + tracked mono), `IBM Plex Serif` `.later-article-title`, double-ruled `.later-article-meta-bar`, `Source Serif 4` `.later-body` with `.later-article:not([dir="rtl"]) .later-body > p:first-of-type::first-letter` drop-cap, warm amber highlight marks, Verdigris hairline blockquotes, and left-accented Notes drawer cards.

- [ ] **Step 4: Run focus and later tests to verify they pass**

Run: `go test ./internal/apps/focus/... ./internal/apps/later/... -race -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/apps/focus/templates/run.html internal/apps/later/templates/article.html internal/ui/static/app.css internal/apps/focus/handler_test.go internal/apps/later/handler_test.go
git commit -m "feat(ui): apply Braun Folio showcase styling to ON Focus and ON Later"
```

---

### Task 6: Docs Cleanup, Full CI Verification & Screenshot Refresh

**Files:**
- Modify: `docs/user/index.md` (and any guide under `docs/user/` referencing the retired Palette or Font display switches)
- Regenerate (if headless Chrome is available): `docs/screenshots/` assets

**Interfaces:**
- Consumes: All changes from Tasks 1–5.
- Produces: Green full CI suite, updated user docs, and verified Light/Dark visual polish.

- [ ] **Step 1: Search `docs/user/` for references to Palette or whole-UI Font switches and update them**

Run: `git grep -n -i -E "palette|warm|ink|font" docs/user/`
Update any text describing the Display menu so it accurately describes the `Light` / `Dark` theme toggle (keeping ON Later's own reading-font docs intact).

- [ ] **Step 2: Run the full CI verification suite**

```bash
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
go mod tidy && git diff --exit-code go.mod go.sum
go test ./... -race -count=1
```
Expected: `gofmt -l .` prints nothing; all checks and tests PASS.

- [ ] **Step 3: Build binary and visually verify Light/Dark + responsive layout (and regenerate screenshots if Chrome/CDP is installed)**

Run: `go build ./cmd/onsuite`
Verify Home, Login, ON Focus, ON Later, and a spot-check of Paste/Notes/Reader/Books/Flash in Light and Dark mode. If `docs/screenshots` capture works in this environment, refresh screenshots; otherwise note it clearly for the PR description.

- [ ] **Step 4: Commit docs/screenshot updates (if any)**

```bash
git add docs/
git commit -m "docs: update user guide for Braun Folio theme settings"
```
