# ON Suite — App Identity Colours

**Date:** 2026-10-13
**Status:** Proposed
**Scope:** App icons on the home page and in the sidebar —
[icons.go](../../../internal/ui/icons.go),
[home.html](../../../internal/ui/templates/home.html),
[base.html](../../../internal/ui/templates/base.html) and the shared colour
swatch block in [app.css](../../../internal/ui/static/app.css). Nothing inside
any individual app changes.

## 1. Purpose

The home page was designed for three apps and now has seven, with more on the
way. Every tile looks almost the same: a small teal line glyph in the corner
of an otherwise identical card. The glyph is too small next to the title to
register first, so the eye goes to the text and every tile has to be read.

The goal is that each app can be recognised at a glance, from the home page
and from the sidebar, without reading its name — while the suite stays calm,
warm and easy on the eyes.

Recognition comes from two things: **colour** and **shape**. Colour is the
new ingredient here — each app gets one of its own. Shape is the glyph itself,
made large enough to register. Varying the badge's silhouette per app
(circle, hexagon, …) was considered and rejected: the silhouette would carry
no meaning about the app and gets harder to keep coherent as apps are added.

## 2. Relationship to earlier specs

[2026-09-01 Warm Palette & Icon Refresh](2026-09-01-warm-palette-icon-refresh-design.md)
§3 made every app icon a thin, single-colour (`--c-accent`) line icon so tiles
and toolbar icons share one style. This spec keeps everything from §3 —
line-only glyphs, `stroke-width: 1.5`, no new fills, hand-drawn inline SVG,
no icon library — and changes only the **colour** of app icons: from the one
suite accent to a per-app colour. Toolbar icons
([toolbar_icons.go](../../../internal/ui/toolbar_icons.go)) are untouched.

## 3. Colour assignment

Each app takes one colour from the existing shared swatch palette (the one ON
Flash decks and ON Focus timers already use):

| App       | Swatch  | Rationale                          |
|-----------|---------|------------------------------------|
| ON Notes  | amber   | legal-pad yellow                   |
| ON Paste  | blue    | code, terminals                    |
| ON Reader | coral   | the RSS orange                     |
| ON Later  | purple  | a calm "set aside" tone            |
| ON Books  | green   | library green                      |
| ON Flash  | pink    | the most energetic swatch, a bolt  |
| ON Focus  | teal    | calm; closest to the suite accent  |
| Admin     | gray    | structural, not an app             |

Any id without an entry (a coming-soon placeholder, the fallback icon) is
gray.

**Rule for future apps:** every registered app has its own colour, and no two
registered apps share one. The palette's eight swatches are all used by the
list above, so the next app adds a new swatch to the shared palette (light,
soft, dark-soft and line values, contrast-checked as in §6) rather than
reusing an existing one. Gray is reserved for Admin and placeholders.

## 4. Visual design

### 4.1 Home tiles

`.app-card-icon` becomes a **soft badge**:

- 2.5rem (40px) square, `border-radius` ~10px, filled with the app's
  `--swatch-soft`;
- the glyph centred inside it at ~26px, stroked in the app's `--swatch-line`
  (§5.3).

Everything else about `.app-card` — border, hover border, padding, title,
summary — is unchanged.

### 4.2 Sidebar

- Each item's glyph is stroked in its app's `--swatch-line`, at its current
  size, with **no badge** behind it.
- **Active app** (`[aria-current="page"]`): the row's background becomes the
  app's own `--swatch-soft` instead of `--c-accent-bg`, and the label colour
  becomes `--c-text` instead of `--c-accent`. Font weight stays as it is.
  This is the one place an app's colour shows outside the home page — a quiet
  reminder of which app you're in.
- Hover (`--c-bg-inset` background), collapsed mode (glyphs only) and
  disabled coming-soon items (50% opacity) behave exactly as today.
- Admin's shield glyph is gray, and its active row uses gray's soft tint.

### 4.3 Dark mode

- Badge and active-row background use the swatch palette's existing dark soft
  values (`:root[data-theme="dark"] .swatch-c-* { --swatch-soft: … }`).
- The glyph uses a lighter shade of the same hue (§5.3) so it stays crisp on
  both the dark badge and the dark sidebar.

## 5. Implementation

### 5.1 Colour lookup

A map from app id to swatch name lives next to the icon map in
[icons.go](../../../internal/ui/icons.go), keyed by app id for the same reason
icons are (a coming-soon placeholder with no code yet can still have one):

```go
var appColors = map[string]string{
	"notes": "amber", "paste": "blue", "reader": "coral", "later": "purple",
	"books": "green", "flash": "pink", "focus": "teal", "admin": "gray",
}

// AppColorClass returns the swatch-c-<name> class for an app id, or
// swatch-c-gray for anything without an entry.
func AppColorClass(id string) string
```

It's exposed to templates as `appColor`, alongside the existing `icon`
function. A class rather than an inline style, because the CSP forbids
`style=""` — the same reason the swatch palette is class-based.

### 5.2 Markup

- **Icons:** every `var(--c-accent)` in the app icon SVGs becomes
  `currentColor`, so the glyph takes its colour from CSS. The invisible frame
  `<rect … fill="none"/>` (it has no stroke and never renders) is removed.
  Glyph paths are otherwise unchanged. The fallback icon's
  `--c-accent`/`--c-accent-bg` squares become `currentColor` and
  `--swatch-soft`.
- **home.html:** `<div class="app-card-icon {{appColor .ID}}">{{icon .ID}}</div>`.
- **base.html:** each `sidebar-item` (enabled, disabled and Admin) gets
  `{{appColor .ID}}` (or `{{appColor "admin"}}`) added to its class list.

### 5.3 CSS

The shared swatch block gains one variable, `--swatch-line`: the glyph colour.
ON Flash and ON Focus don't read it, so they're unaffected.

| Swatch | Light `--swatch-line` | Dark `--swatch-line` |
|--------|-----------------------|----------------------|
| teal   | `#188D6A`             | `#5DCAA5`            |
| blue   | `#2C7BC9`             | `#85B7EB`            |
| purple | `#7067D0`             | `#AFA9EC`            |
| pink   | `#BF4870`             | `#ED93B1`            |
| coral  | `#C25029`             | `#F0997B`            |
| amber  | `#A76813`             | `#EF9F27`            |
| green  | `#558A1C`             | `#97C459`            |
| gray   | `#7A7973`             | `#B4B2A9`            |

Light values are each swatch's `--swatch` mixed 35% toward the darker stop of
its ramp; the raw `--swatch` value falls just under 3:1 on the soft tint for
teal and green. Dark values are the ramp's lighter stop.

New and changed rules:

- `.app-card-icon` — the badge in §4.1: size, radius, `background:
  var(--swatch-soft)`, `color: var(--swatch-line)`, flex-centred, inner
  `svg` sized to ~26px.
- `.sidebar-item svg` — `color: var(--swatch-line)`.
- `.sidebar-item[aria-current="page"]` — `background: var(--swatch-soft)`,
  `color: var(--c-text)`.

## 6. Contrast

Glyphs are graphical objects, so the bar is WCAG 1.4.11's 3:1, checked for
every swatch against every surface it sits on:

| Mode  | Surface                                     | Worst swatch | Ratio |
|-------|---------------------------------------------|--------------|-------|
| Light | own `--swatch-soft` (badge, active row)     | green        | 3.65  |
| Light | `--c-bg` `#fefefe` (sidebar)                | teal         | 4.12  |
| Light | `--c-bg-inset` `#efe0cf` (sidebar hover)    | teal         | 3.21  |
| Dark  | own dark `--swatch-soft`                    | amber / gray | 4.60  |
| Dark  | `--c-bg` `#241d17` (sidebar)                | pink         | 7.45  |

Active-row label text (`--c-text`) on the soft tints is at least 11:1 in light
and 7.6:1 in dark. The implementation PR re-runs this check against the final
CSS (the same technique as the #556 text-contrast audit) and records the
numbers.

## 7. Testing

- [icons_test.go](../../../internal/ui/icons_test.go): every known id maps to
  a `swatch-c-*` class; an unknown id maps to `swatch-c-gray`; no app icon
  references `--c-accent` any more; the existing distinct-icon and
  stroke-width tests keep passing.
- `cmd/onsuite`: a test next to `TestComingSoonAppsExcludesRegisteredApps`
  asserting every registered app has an entry in the colour map (i.e. isn't
  falling back to gray) and that no two registered apps share a colour. This
  is what makes §3's rule for future apps enforceable.
- Template/handler tests that render the home page or shell assert the badge
  and sidebar items carry their `swatch-c-*` class.
- Manual check of the home page and sidebar in light and dark, expanded and
  collapsed sidebar, at phone width.

## 8. Docs

- Re-capture `docs/user/images/dashboard.png` (the README hero) and any other
  screenshots whose sidebar is visible, with the existing
  `docs/screenshots/capture` tool.
- Add a one-line note to the warm-palette spec's §3 pointing here: app icon
  colour is now per app.

## 9. Out of scope

- Using app colours inside the apps themselves (shell bar, crumbs, page
  accents, buttons).
- Changing any glyph's drawing, or redrawing icons.
- Varying badge shape per app.
- Changing the ON Suite logo or favicon.

## 10. Success criteria

1. On the home page, each app is identifiable by its badge colour and glyph
   before reading its name.
2. The sidebar uses the same colours, and the active app's row is tinted in
   its own colour.
3. Every glyph meets 3:1 on every surface it sits on, in light and dark.
4. A new app can't be registered without its own, unused colour — a test
   fails.
5. The suite still reads as calm and warm: one soft tint per tile, line
   glyphs, no saturated fills.
