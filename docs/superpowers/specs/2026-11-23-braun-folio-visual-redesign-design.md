# Braun Folio — ON Suite Visual Redesign (Phase 1)

**Date:** 2026-11-23  
**Status:** Draft for review  
**Scope:** Shared design system, shell chrome, Home, Login, and flagship showcases on ON Focus and ON Later.

---

## 1. Purpose & Design Intent

ON Suite's functionality and UX are well aligned, but its current visual presentation reads as polite and utilitarian. Supporting an open matrix of whole-UI fonts (`Sans`, `Serif`, `Mono`, `Mixed`) and multiple palettes (`Warm`, `Ink`) forced the interface to stay visually neutral.

This redesign gives ON Suite a single, unmistakable visual identity — **Braun Folio** — that makes a strong first impression without compromising daily usability:

- **Precision industrial geometry (Dieter Rams / Romotow):** Honest surfaces, crisp `1px` architectural hairline grids, recessed instrument wells, tight `2px` corners, and a restrained **45° chamfered top-right corner** used sparingly as the suite's signature silhouette.
- **Editorial typographic intelligence (Quanta Magazine):** A deliberate four-role typographic hierarchy where display serif, industrial UI sans, calm reading serif, and tabular instrument mono each have one clear job.
- **Intimate household craft (Day One):** Warm alabaster stone in Light mode and warm obsidian in Dark mode, with bespoke geometric SVG illustrations on the Home and Login plates that adapt automatically to the active theme.
- **Two harmonious modes in one suite:** Tool/instrument apps (showcased by **ON Focus**) feel like precision Dieter Rams hardware, while long-form reading surfaces (showcased by **ON Later**) recede into calm editorial print.

---

## 2. Visual System

### 2.1 Signature Palette (Light & Dark)

The whole-UI `Warm` / `Ink` palette switcher is retired in favor of one opinionated **Braun Folio** palette in both Light and Dark themes. Every text/surface and accent/surface pairing must maintain WCAG AA contrast (at least 4.5:1 for normal text and 3:1 for large text / UI graphics).

| Token | Role | Light (Alabaster Stone) | Dark (Warm Obsidian) |
|---|---|---|---|
| `--c-bg` | Page canvas | `#f4f1ea` | `#151412` |
| `--c-bg-subtle` | Elevated surface (cards, header, sidebar, panels) | `#fbf9f5` | `#1d1b18` |
| `--c-bg-inset` | Recessed instrument wells, inputs, code blocks | `#ebe6dc` | `#26231f` |
| `--c-border` | Architectural hairline divider | `#d7d0c3` | `#332f29` |
| `--c-border-firm` | Emphasized frame & control borders | `#9f9687` | `#575148` |
| `--c-text` | Primary ink | `#1c1a17` | `#f0ece3` |
| `--c-text-dim` | Secondary prose & labels | `#5c564c` | `#aba497` |
| `--c-text-faint` | Metadata, indices, subtle captions | `#6b6459` | `#8c8579` |
| `--c-accent` | Everyday working accent (Deep Verdigris) | `#1c665d` | `#4eb8aa` |
| `--c-accent-bg` | Verdigris tint (active nav, tags, focus wash) | `#dfe9e7` | `#1b302d` |
| `--c-accent-attention` | Signal accent (Braun Signal Vermilion) | `#c23d15` | `#f06437` |
| `--c-accent-attention-bg` | Signal tint (LED halos, due/overdue chips) | `#f6e2db` | `#3a2118` |
| `--c-danger` | Destructive actions & errors | `#a51d2d` | `#f08c95` |
| `--c-danger-bg` | Danger tint | `#fbeaec` | `#3a1e22` |

**Signal discipline:** `--c-accent` (Deep Verdigris) carries everyday links, active navigation rails, focus rings, and reading kickers. `--c-accent-attention` (Braun Signal Vermilion) is strictly reserved for primary action CTAs, active/running Focus indicators, focal illustration accents, and due/overdue attention badges.

**Per-app harmonized markers:** Each of the 7 apps keeps a distinct color marker in the sidebar and Home directory, tuned to harmonize with the stone/obsidian palette:
- Paste: `#1c665d` / `#4eb8aa`
- Notes: `#b07016` / `#e3a84a`
- Reader: `#2a5d8a` / `#68a4d9`
- Later: `#7a4c8c` / `#be8fd1`
- Books: `#8c4a2e` / `#d98c68`
- Flash: `#3d7548` / `#7ac488`
- Focus: `#c23d15` / `#f06437`

### 2.2 Four-Role Typography System

All fonts are self-hosted `.woff2` files in `internal/ui/static/fonts/` with Latin and Cyrillic subsets (no external CDN requests).

| Token | Typeface | Weights | Role |
|---|---|---|---|
| `--font-display` | `IBM Plex Serif` | 400, 500, 600, 400 italic | Page `<h1>` titles, Home/Login hero headings, article & book titles, italic display accents |
| `--font-ui` | `IBM Plex Sans` | 400, 500, 600 | Body default, navigation, buttons, forms, lists, tables, sidebars, settings |
| `--font-body` | `Source Serif 4` | 400, 600, 400 italic | Long-form reading prose (ON Later, ON Reader, ON Books notes/quotes, Help, ON Notes outlines) |
| `--font-mono` | `IBM Plex Mono` | 400, 500, 600 | Uppercase tracked kickers (`01 / CODE`, `6 MIN READ`), Focus timer readouts (`tabular-nums`), timestamps, keyboard hints, code blocks |

Unused bundled font files (`Inter`, `JetBrains Mono`, `iA Writer Duo S`) are removed from `internal/ui/static/fonts/` and `VENDOR.md` is updated to document the IBM Plex family provenance and license (SIL OFL 1.1).

### 2.3 Display Settings Simplification

- **Keep:** `Theme` (`Light` / `Dark`) in the header user menu (`data-theme` on `<html>`, `onsuite_theme` cookie).
- **Remove:** Whole-UI `Palette` (`Warm` / `Ink`) and `Font` (`Sans` / `Serif` / `Mono` / `Mixed`) switches from `base.html`, `theme.js`, `prefs.go`, and `render.Shell`. Existing `onsuite_palette` and `onsuite_font` cookies in users' browsers are ignored cleanly without errors.
- **Preserve:** ON Later's own reading-view `Aa` settings (Font / Size / Width / Align) remain untouched.

### 2.4 Signature Geometry & Focus Accessibility

- **Precision corners:** `--radius` tightens from `6px` to `2px` across inputs, secondary buttons, panels, and pills.
- **Hairline grids:** Lists, directories, and toolbars use crisp `1px solid var(--c-border)` dividers rather than heavy drop shadows.
- **Restrained 45° chamfer:** A top-right clipped corner (`clip-path: polygon(0 0, calc(100% - var(--chamfer)) 0, 100% var(--chamfer), 100% 100%, 0 100%)`) is applied exclusively to:
  1. Primary CTA buttons (`button.primary`, `.button.primary`) — `--chamfer: 8px`
  2. Architectural hero/login plates (`.home-hero`, `.login-plate`) — `--chamfer: 22px`
  3. The ON Focus active timer chassis (`.focus-chassis`) — `--chamfer: 24px`
- **Focus rings on chamfered controls:** Because `clip-path` clips standard `outline` on the element itself in some browsers, chamfered buttons use a `:focus-visible` treatment that remains fully visible (removing or pairing with an inset box-shadow ring `box-shadow: inset 0 0 0 2px var(--c-bg), inset 0 0 0 4px var(--c-accent)` alongside a high-contrast background shift, or wrapping outline via `filter: drop-shadow`), verified in keyboard navigation.
- **Reduced motion:** All hover/layout transitions are wrapped or overridden under `@media (prefers-reduced-motion: reduce)`.

---

## 3. Shell, Home & Login

### 3.1 Shared Shell (`base.html`, `favicon.svg`, `app.css`)

- **Architectural Emblem:** Replace the 4-square logo in `base.html` and `favicon.svg` with the chamfered geometric frame containing a Verdigris square (`var(--c-accent)`) and Vermilion signal circle (`var(--c-accent-attention)`).
- **Header Bar (`.shell-bar`):** Elevated `--c-bg-subtle` strip with a `1px` bottom hairline, uppercase tracked `ON SUITE` wordmark, vertical divider into `IBM Plex Mono` uppercase breadcrumbs, a Braun-style recessed connectivity status pill (`ONLINE` + status LED dot driven by existing `data-status` states), and a crisp `2px`-bordered user menu trigger.
- **Collapsible Sidebar (`.app-sidebar`):** `1px` right hairline, recessed top toggle control, numbered monospaced app indices (`01`–`07`), crisp square app-color markers, and a `2px` Verdigris left indicator rail (`border-left-color: var(--c-accent)`) on `aria-current="page"`. Collapsed state hides labels and indices cleanly while keeping icons/markers centered.

### 3.2 Home Page (`home.html`)

- **Chamfered Hero Plate (`.home-hero`):** Two-column architectural banner (collapsing to single column on narrow viewports) with a monospaced kicker (`HOUSEHOLD SUITE · N INSTRUMENTS`) preceded by a short Vermilion rule, an `IBM Plex Serif` display headline with italic emphasis, a concise subtitle, and a **bespoke inline SVG geometric composition** (cantilevered overlapping planes on a hairline grid with an orbital arc and Vermilion focal disc). Every SVG fill/stroke uses CSS custom properties (`var(--c-*)`) so Light and Dark modes work automatically under strict CSP.
- **Hairline App Directory (`.home-grid`):** Replaces the separate rounded card grid with a unified `1px`-hairline architectural grid (`gap: 1px; background: var(--c-border); border: 1px solid var(--c-border)`). Each cell displays a monospaced index + short category tag, `IBM Plex Serif` app title, concise summary, and a bottom harmonized app-color accent rule that widens on hover.

### 3.3 Login Page (`login.html`)

- **Split Architectural Plate (`.login-plate`):** Centered chamfered two-column plate (stacks cleanly on mobile):
  - **Art Plate (left):** Deep obsidian surface (`#1b1917` in Light, `#0f0e0d` in Dark) featuring the ON Suite emblem, a bespoke geometric cantilevered-plane + orbital-ring SVG illustration, and an `IBM Plex Serif` headline.
  - **Sign-in Pane (right):** Monospaced `AUTHENTICATION` kicker, `IBM Plex Serif` `Sign in` heading, crisp `2px` recessed inputs (`--c-bg-inset`), existing CSRF/error-notice markup preserved, and a chamfered Signal Vermilion `Sign in` button.

---

## 4. Flagship Apps Showcase (Phase 1)

All seven apps automatically inherit the Braun Folio palette, four-role typography, `2px` controls, chamfered primary actions, and updated shell chrome. Two apps receive tailored flagship styling in Phase 1 to establish the two design poles of the suite:

### 4.1 ON Focus — Dieter Rams Precision Instrument (`focus/templates/run.html`, `app.css`)

- **Instrument Chassis (`.focus-chassis`):** Wraps the running timer in a centered chamfered `--c-bg-subtle` panel with a `1px solid var(--c-border-firm)` border and a monospaced status header showing the active interval readout and a Vermilion `RUNNING` LED indicator.
- **Precision Dial:** Enhances the existing `.focus-ring-svg` with an outer radial hairline tick circle (`stroke-dasharray`), a sharp `butt`-capped progress arc, and a recessed circular instrument well (`--c-bg-inset`) housing the `IBM Plex Serif` timer name, large tabular `IBM Plex Mono` clock digits, uppercase Verdigris phase label, and rectangular segmented interval bars (replacing round dots).
- **Tactile Controls:** Chamfered Signal Vermilion primary button (`Pause` / `Start`), crisp `2px` secondary `Skip` / `Restart` buttons, and quiet monospaced `<kbd>` shortcut hints below.
- All existing JS hooks (`data-focus-*`, `#focus-run-config`, `#focus-run-dialog`, swatch color classes) remain unchanged.

### 4.2 ON Later — Quanta Editorial Reading Room (`later/templates/article.html`, `app.css`)

- **Sticky Reading Bar:** Hairline topbar with monospaced `← LATER` back link, serif truncated title, crisp `2px` utility chips (`N min left`, `Notes · N`, `Aa`), chamfered `Archive` primary action, and a `2px` Verdigris reading-progress bar directly underneath.
- **Editorial Article Header:** Monospaced site + read-time kicker preceded by a short Vermilion accent rule, generous `IBM Plex Serif` article headline, and a metadata/tags strip framed by a firm top hairline and subtle bottom hairline.
- **Reading Prose & Margin Notes:** Calm `Source Serif 4` body measure (`--measure`), an editorial drop-cap on the first paragraph of Latin-script articles (scoped safely so RTL/non-Latin and link-only views stay clean), warm amber underline-tinted highlight marks, Verdigris hairline italic blockquotes, and crisp left-accented note cards inside the existing Notes drawer.
- All existing JS hooks (`data-later-*`, `later-font-*`, `later-size-*`, `later-width-*`, `later-align-*`, highlight/notes markup) remain unchanged.

---

## 5. Architectural Constraints & Compatibility

- **Zero new Go dependencies** and **no Node/npm/JS build step**.
- **Strict CSP preserved:** Zero inline `<script>` tags and zero inline `style="..."` attributes in any template; SVG illustrations use CSS classes and `var(--c-*)` custom properties.
- **Package boundaries preserved:** `internal/ui` remains a zero-import embedded leaf (verified by `go test ./internal/arch/...`).
- **Progressive enhancement preserved:** All navigation, forms, `<details>` menus, and reading/timer fallbacks continue to work without JavaScript.

---

## 6. Verification & Rollout

### Verification Plan
1. **Unit & contract tests:**
   - Update `internal/ui/fonts_test.go` to verify the four-role typography tokens (`--font-display`, `--font-ui`, `--font-body`, `--font-mono`) and that `body` uses `--font-ui`, headings use `--font-display`, and reading surfaces use `--font-body`.
   - Keep `internal/ui/color_scheme_test.go` passing (`color-scheme: light` and `color-scheme: dark`).
   - Update platform/template tests affected by removing `data-palette` / `data-font` and updating `base.html`, `home.html`, and `login.html`.
2. **Full CI suite:**
   - `gofmt -l .` (empty)
   - `go vet ./...`
   - `go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...`
   - `go mod tidy && git diff --exit-code go.mod go.sum`
   - `go test ./... -race -count=1`
3. **Visual & screenshot verification:**
   - Verify Light and Dark modes, desktop and narrow viewports, keyboard focus visibility, and contrast across Home, Login, ON Focus, ON Later, and a spot-check of the remaining 5 apps.
   - Regenerate user-guide screenshots via `docs/screenshots` once visual implementation is complete.

### Phased Rollout
- **Phase 1 (this spec):** Braun Folio visual system, self-hosted IBM Plex fonts, display-menu simplification, Shell, Home, Login, and flagship showcases on ON Focus and ON Later.
- **Phase 2 (future follow-ups):** Dedicated per-app visual polish passes for ON Paste, ON Notes, ON Reader, ON Books, and ON Flash on top of the Braun Folio design system.
