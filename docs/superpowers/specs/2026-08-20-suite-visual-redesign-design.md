# ON Suite — Visual Redesign

**Date:** 2026-08-20
**Status:** Proposed
**Scope:** Shared shell (`internal/ui`) and the home page. Does not redesign any
individual app's screens; ON Paste inherits the new tokens automatically and
gets no bespoke changes.

## 1. Purpose

The platform and ON Paste (Projects 0 and 1, see
[2026-08-18-on-suite-platform-design.md](2026-08-18-on-suite-platform-design.md))
are functionally complete but visually minimal: a bare top bar, a bullet-list
home page, system fonts, and dark mode that only follows the OS setting. This
spec makes the suite look intentionally designed — without touching its
architecture, its "no build step" constraint, or any app's actual features.

Everything here is additive to the existing token system in
[app.css](../../../internal/ui/static/app.css): the file's own header comment —
"apps must not introduce new colours or spacing values; they compose these" —
stays true. This redesign changes what the shared tokens *are*, not the rule
that apps only consume them.

## 2. Constraints

Decided during brainstorming; each drives a specific decision below.

| Constraint | Decision |
|---|---|
| No JS build step, no CDN dependencies | Theme/font switching and sidebar state use a small hand-written vanilla-JS file, embedded like `htmx.min.js`. New fonts are self-hosted `.woff2`, embedded the same way — no `fonts.googleapis.com` at runtime. |
| Single shared shell, apps don't redefine it | This spec only touches `internal/ui/templates/base.html`, `internal/ui/templates/home.html`, and `internal/ui/static/app.css`. No app template changes. |
| Existing keyboard-first behaviour | Visual density loosens (more whitespace, larger targets) but no keyboard shortcut, focus behaviour, or interaction changes. |
| One app built so far (ON Paste); three specced, not built | Home page shows real + "coming soon" cards today so the grid doesn't need rework when ON Notes/Reader/Flash ship. |

## 3. Palette

Grey carries the interface; one muted accent is used sparingly, same
philosophy as today — only the hues change. Cool slate/teal, chosen over a
warm sand/clay and a lavender/sage option during brainstorming for feeling
calm and professional rather than either cozy or playful.

| Token | Light | Dark |
|---|---|---|
| `--c-bg` | `#ffffff` | `#1c1d1f` |
| `--c-bg-subtle` | `#f4f3f1` | `#242527` |
| `--c-bg-inset` | `#eceae6` | `#2b2c2f` |
| `--c-border` | `#e2e0dc` | `#34373a` |
| `--c-border-firm` | `#c9c7c1` | `#4a4d51` |
| `--c-text` | `#2b2a28` | `#e8e6e2` |
| `--c-text-dim` | `#6b6a67` | `#a3a29c` |
| `--c-text-faint` | `#8a8983` | `#79786f` |
| `--c-accent` | `#5c8580` | `#7fa39d` |
| `--c-accent-bg` | `#e4ecea` | `#233330` |
| `--c-danger` / `--c-danger-bg` | unchanged | unchanged |

These are starting values validated visually during brainstorming, not final
pixels — if any pair fails WCAG AA contrast when implemented, adjust
lightness while keeping the hue.

## 4. Theming mechanism

Dark mode becomes a real user-facing toggle rather than following
`prefers-color-scheme` alone (default: light).

- Preference stored server-side as a cookie (e.g. `theme=light|dark`) so the
  server renders the correct `<html data-theme="...">` on first response — no
  flash of the wrong theme.
- `app.css` keys its dark-mode block off `[data-theme="dark"]` instead of (or
  in addition to, as a fallback for a first-ever visit with no cookie yet)
  `prefers-color-scheme: dark`.
- Toggled from the settings menu (see §6); the small vanilla-JS handler flips
  the attribute immediately and sets the cookie, so there's no full-page
  reload on toggle.

## 5. Typography

Three pairings ship in a runtime switcher, all self-hosted:

| Pairing | Heading / UI | Body | Code | Character |
|---|---|---|---|---|
| Default | Inter | Inter | JetBrains Mono | Clean, modern, safe |
| Alternate | Literata | Literata | JetBrains Mono | Warm, book-like, single serif throughout |
| Alternate | Space Grotesk | Public Sans | JetBrains Mono | Slightly quirky headings over a plain, legible body |

Mechanism mirrors theming: a `font` cookie sets `data-font` on `<html>`,
`app.css` maps `data-font` values to `--font-ui`/`--font-body` tokens (today's
single `--font-ui` token splits into a UI/heading token and a body token so
the two can differ within a pairing). `--font-mono` stays JetBrains Mono
across all three — code should look the same regardless of prose font.

Font files (Inter, Literata, Space Grotesk, Public Sans, JetBrains Mono; woff2,
the specific weights used in the mockups) are vendored under
`internal/ui/static/fonts/` and `go:embed`'d, following the exact pattern
`htmx.min.js` already uses.

## 6. Layout shell

Replaces the current single-row `.shell-bar` in `base.html` with:

- **Header:** the new tile-grid logo mark (§8) + "ON Suite" wordmark on the
  left, an inline breadcrumb trail next to it (e.g. `Home / ON Paste / New
  paste`, generated from a small per-page breadcrumb list each handler already
  has enough context to populate), and on the right a settings menu (theme
  toggle + font switcher together, one dropdown) plus the existing user
  menu/logout.
- **Sidebar:** new. The *only* place for switching between apps — the app
  switcher nav currently inline in the top bar moves here and is removed from
  the header. Expand/collapse toggle; expanded shows a small duotone icon (§8)
  plus the app name, collapsed shows just the icon. State persisted via
  cookie so it stays how the user left it. Unbuilt apps (ON Notes, ON Reader,
  ON Flash) appear muted and non-clickable, same treatment as the home page
  cards.
- **Main content:** kept width-limited using the existing `--measure` (68ch)
  token — unchanged rationale, just applied consistently under the new
  layout.
- **Footer:** new, thin strip below the content area, small font, holding
  what the home page currently shows inline (`Version {{.Shell.Version}}`).
- **Density:** general spacing loosens from the current tight scale —
  concretely, more generous `main` padding and larger clickable areas in the
  sidebar/header — while keeping the same `--s-1`…`--s-6` scale and every
  existing keyboard/focus behaviour untouched.

## 7. Home page

`home.html`'s bullet list becomes a card grid. Each card: a duotone icon tile
(§8, accent-tinted) + app name + one-line summary (the same `Summary` field
already used today), linking to the app.

ON Notes, ON Reader and ON Flash — specced but not built — get muted,
non-clickable "coming soon" cards in the same grid, so it reads as a
deliberate 2×2 (or similar) layout today rather than one lonely card, and
needs no layout change when each app actually ships.

## 8. Logo mark and icon style

- **Logo:** a 2×2 grid of rounded tiles in two accent shades (chosen over a
  toggle-pill, a bracket/prompt mark, and a plain wordmark-with-dot during
  brainstorming) — doubles as the visual motif for the home page grid and the
  sidebar icons, so the same shape language shows up in three places instead
  of being a one-off mark.
- **App icons:** each app gets a small duotone icon (two accent tones, flat,
  in a soft rounded-square tile) — used at large size on home page cards and
  small size in the sidebar. Chosen over a plain line icon and an
  icon-plus-organic-blob-background option for best matching the logo's tile
  language without adding decoration that a line icon lacks or a blob adds.

## 9. Rollout / affected files

| File | Change |
|---|---|
| `internal/ui/static/app.css` | New palette tokens (§3), font tokens split for UI/body (§5), `[data-theme]`/`[data-font]` selectors, shell/sidebar/footer/card CSS, loosened spacing. |
| `internal/ui/templates/base.html` | Header with logo + breadcrumbs + settings menu, new sidebar markup, footer, `data-theme`/`data-font` on `<html>`. |
| `internal/ui/templates/home.html` | Card grid replacing the bullet list, including coming-soon placeholders. |
| `internal/ui/static/fonts/*.woff2` (new) | Vendored font files. |
| `internal/ui/static/theme.js` (new, name illustrative) | Theme/font toggle handlers, sidebar collapse handler, cookie writes. |
| Server-side (`platform/web` or `platform/render`) | Read/write the `theme` and `font` (and sidebar-state) cookies; set `data-theme`/`data-font` when rendering `base.html`. |

No changes to any file under `internal/apps/paste/` — it inherits the new
tokens through `app.css` alone.

## 10. Non-goals

- Redesigning ON Paste's own screens (list/new/view/shared) beyond what the
  shared tokens change automatically.
- Building ON Notes, ON Reader or ON Flash — their cards are visual
  placeholders only.
- Any accessibility audit beyond the contrast note in §3 — a full audit is
  out of scope for this pass.

## 11. Success criteria

1. Light is the default theme; a visible control switches to dark and the
   choice survives a reload with no flash of the wrong theme.
2. A visible control switches between the three font pairings; the choice
   survives a reload.
3. The header shows the new logo mark and a breadcrumb trail; the app
   switcher no longer appears in the header.
4. A left sidebar lists all four apps (one real, three muted/disabled),
   expand/collapse works, and the chosen state survives a reload.
5. The home page shows a card grid with a duotone icon, name, and summary per
   app, including non-clickable coming-soon cards for the unbuilt apps.
6. A thin footer with the version string appears below the content area on
   every page.
7. `internal/apps/paste` has zero diff; ON Paste's screens pick up the new
   look purely through `app.css`.
8. `go build ./cmd/onsuite` still produces the build with no new
   dependencies beyond the vendored font files.
