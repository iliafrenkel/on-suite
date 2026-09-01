# ON Suite — Warm Palette &amp; Icon Refresh

**Date:** 2026-09-01
**Status:** Proposed
**Scope:** Shared color tokens and icon style in `internal/ui/static/app.css` /
`internal/ui/icons.go`, plus a toolbar redesign for ON Notes specifically
([outline.html](../../../internal/apps/notes/templates/outline.html),
[toolbar.partial.html](../../../internal/apps/notes/templates/toolbar.partial.html)).
Other apps (Paste, Reader, Admin, Flash) inherit the new tokens and icon style
automatically but get no bespoke screen changes in this pass.

## 1. Purpose

The suite's current look (established in
[2026-08-20-suite-visual-redesign-design.md](2026-08-20-suite-visual-redesign-design.md))
reads as too plain for something used every day: a barebones slate/teal
palette, toolbar actions styled as plain text links with `::after` glyphs
(`Due →`, `▾`) instead of real buttons, and icons that are inconsistent in
weight. This spec addresses all three, using the ON Notes toolbar as the
concrete redesign target since it's the clearest example of the problem.

This is a **revision**, not an extension, of two decisions from the prior
spec:

- **§3 Palette** is replaced outright (cool slate/teal → warm cream/teal/orange).
  The prior spec chose cool tones "for feeling calm and professional rather
  than cozy"; this pass explicitly chooses cozy instead, after comparing both
  during brainstorming.
- **§8 Icon style** is replaced for app-tile icons (duotone flat tiles → thin
  single-color line icons), so app tiles and toolbar icons share one visual
  language instead of two.

Everything else from the prior spec — the cookie-based `data-theme` toggle
mechanism, the token-only rule for apps, the sidebar/header/logo shell — is
unchanged and assumed already in place.

## 2. Palette

| Token | Light | Dark (warm) |
|---|---|---|
| `--c-bg` | `#faf3ea` | `#241d17` |
| `--c-bg-inset` | `#efe0cf` | `#2e2621` |
| `--c-text` | `#3a2f26` | `#efe6da` |
| `--c-accent` | `#2f6f6a` | `#3d8983` |
| `--c-accent-attention` *(new token)* | `#d9773f` | `#e08a52` |
| `--c-danger` / `--c-danger-bg` | unchanged | unchanged |

`--c-accent` (teal) is the default action color — links, active toggles,
primary buttons. `--c-accent-attention` (orange) is new and deliberately
narrow-scoped: it appears only on due/overdue/unread-style badges, never on
routine buttons, so it keeps signaling "needs attention" instead of becoming
just another button color.

Values above are starting points validated visually during brainstorming, not
final pixels — adjust lightness (keeping hue) if any pair fails WCAG AA
contrast during implementation.

## 3. Icon style

One shared spec for every hand-drawn inline SVG icon in the suite:

- Line icons only (no fills, no duotone), `stroke-width: 1.5`,
  `stroke-linecap`/`stroke-linejoin: round`
- `stroke="currentColor"` so icons inherit `--c-text` or `--c-accent` from
  context, same pattern as today
- Two size buckets: 16px for inline/toolbar icons, 24px for app tiles —
  same viewBox proportions at both sizes

Applies to:

- App-tile icons in [icons.go](../../../internal/ui/icons.go) (Notes, Paste,
  Reader, Admin, Flash) — redrawn from their current duotone-tile style to
  the thin-line style, still tinted with `var(--c-accent)`
- ON Notes toolbar/menu icons (new, see §4) — Due, Archive, Export, Import,
  Hide-completed, Shortcuts disclosure

No new icon library or build dependency — icons stay hand-drawn inline SVG,
consistent with the project's no-npm-build-step constraint.

## 4. ON Notes toolbar redesign

Replaces the current plain-text-link toolbar
(`outline.html` lines ~34–87, `toolbar.partial.html`) with real icon buttons:

- New/updated shared CSS (`.toolbar-btn` and friends in `app.css`): icon +
  label, comfortable padding, a quiet hover background
  (`--c-bg-inset`-derived), an active/pressed state using `--c-accent`
- A thin `.toolbar-sep` divider groups actions: **Due / Archive / Export**
  (view actions) separated from **Import** (data action); Hide-completed and
  Shortcuts sit at the end
- "Hide completed" keeps its pill shape and active-state highlight, but uses
  a real check icon (§3 style) instead of the current `::before` ☐/☑
  pseudo-element
- Import becomes an icon-button that triggers the file picker, replacing the
  current raw `Browse… / No file selected` native file input text
- Due gets an orange (`--c-accent-attention`) count badge when notes are due,
  the first real usage of the new attention token

Scope is ON Notes only: shared `.toolbar-btn` classes live in `app.css` so
other apps can adopt the same pattern later, but no other app's templates
change in this pass.

## 5. Non-goals

- Redesigning ON Paste's screens beyond the automatic token/icon inheritance
  (Reader, Admin, and Flash aren't built yet — still placeholder cards per
  the prior spec)
- Changing the theme-toggle mechanism, sidebar, header, or logo mark from the
  prior spec
- A full accessibility/contrast audit beyond the note in §2
- Introducing an icon library or build-tooling dependency

## 6. Success criteria

1. Light theme shows the cream/teal palette; dark theme shows the warm
   charcoal-brown variant; both use `--c-accent-attention` orange only on
   due/attention badges, nowhere else.
2. Every icon in the app (tiles and toolbar) uses the same stroke weight and
   line style — no mixed duotone/line-icon inconsistency remains.
3. The ON Notes toolbar shows real icon+label buttons with hover/active
   states, grouped by a visible separator, replacing today's plain text
   links and glyph characters.
4. "Hide completed" and "Import" no longer rely on Unicode pseudo-elements or
   raw native file-input text.
5. `internal/apps/paste` has zero template diff — it inherits the new look
   purely through `app.css` and `icons.go`.
6. `go build ./cmd/onsuite` still builds with no new dependencies.
