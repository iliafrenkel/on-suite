# ON Notes on phones: denser outline, easier zoom — design

Date: 2026-11-24. Status: approved by Ilia.

## Why

On a phone (375px) the outline's text column shrinks to nothing by level 4.
Measured on the v6.0.0 Braun Folio build:

| level | text starts at | text width |
|-------|----------------|------------|
| 1     | 140px          | 219px      |
| 2     | 213px          | 146px      |
| 3     | 286px          | 73px       |
| 4     | 359px          | 0px        |

The space goes to the per-level indent (~73px: `.outline-item .outline-list`
margin-left 3.625rem + padding-left 0.875rem, sized so the guide line sits
under the parent dot) and to ~72px of leading controls on every row
(`…` menu, chevron, dot — always visible on touch via `@media (hover: none)`).
The right edge is only ~16px.

On a phone Ilia mostly reads, ticks bullets off, adds due dates, and
sometimes adds quick bullets. Restructuring happens on desktop.

## Goals

- Readable text at 4+ levels on a 375px screen.
- Zooming into a bullet is easy to hit with a finger.
- Done / due stay one tap away via the `…` menu.

## Non-goals

- Any visual change on desktop.
- Touch gestures (swipe-to-done, action sheets).
- A depth cap on indentation.
- Changes to the suite-wide app rail.
- A "Zoom in" item in the `…` menu (considered and declined: the larger tap
  area is enough).

## Anti-goals

- Horizontal page scroll anywhere in the outline.
- A tap landing on the wrong control (dot vs chevron vs text).

## Approaches considered

- **A. Phone-only CSS density (picked).** Smaller indent, `…` menu moved to
  the row's right end, outline text one step smaller. Cheap, desktop
  untouched, keeps every existing control.
- **B. A plus a depth cap** past level 3, leaning on zoom. Most room, but deep
  levels get hard to read. Not picked.
- **C. Touch gestures** (swipe to done, dot opens an action sheet). Most
  app-like, but lots of new JS, conflicts with scrolling, and the dot would
  stop zooming. Not picked.

## Design

All of it applies only at the suite's phone breakpoint, `@media (max-width: 780px)`.

1. **Indent.** Per-level indent drops from ~73px to ~20px. The guide line
   (border-left on the nested list) stays, positioned under the parent's dot
   given the new control layout.
2. **`…` menu on the right.** The menu moves to the end of the row, after the
   text, so only chevron + dot precede the text. Its drop-down must open
   anchored to the right edge (like `.outline-menu-list-end`) so it never
   overflows the screen.
3. **Font.** Outline title text goes down one step from the base size; notes
   and chips scale with it.
4. **Zoom tap area.** The dot gets a ~44px finger target via an invisible
   hit area (e.g. a pseudo-element) that spills into the indent space and
   costs no layout width. The chevron's own hit area must not overlap it, so
   a near-miss doesn't collapse the row.

## Left to the builder

- Exact sizes (indent, font step, hit-area geometry), as long as level-4 text
  on a 375px viewport ends up roughly 150px+ wide.
- How the menu is moved. The `<details class="outline-menu">` is a flex child
  of `.outline-main` alongside the chevron, dot and `.outline-text`, so CSS
  `order` on the existing markup is preferred over a template change (the
  markup order matters for #57's Enter-submits-append button and #74's
  due-form association — see the comments in `outline.html`).
- Checking that drag-to-move (mouse-only) and the due-date input inside the
  menu still work at phone width.
