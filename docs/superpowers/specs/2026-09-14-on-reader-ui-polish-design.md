# ON Reader UI polish design

Status: approved 2026-09-14. Purely a UI/chrome pass over the ON Reader
frontend; no changes to routes, storage, or the read/star/extraction domain
logic described in
[2026-09-09-on-reader-design.md](2026-09-09-on-reader-design.md).

ON Reader shipped functionally complete through R1–R7, but its UI never got
the polish pass Notes and Paste already have: fixed-width panes, inline forms
and links instead of a toolbar, and native `confirm()` dialogs instead of
proper modals. This spec brings Reader's chrome in line with the rest of the
suite: a resizable three-pane layout, a top toolbar (filters + an overflow
menu), a middle toolbar for per-feed actions, and native `<dialog>` popups for
the actions that used to be inline forms.

## 1. Decisions

| Decision | Choice |
|---|---|
| Pane count | Keep the existing three panes: tree, article list, reading pane |
| Pane sizing | Draggable gutters between panes, JS-driven, not CSS `resize` |
| Pane persistence | Widths saved to `localStorage`, restored on load, per browser |
| Reading pane default | Largest by default (`flex: 1`), tree and list get fixed starting widths |
| Mobile behaviour | Unchanged — the existing CSS-only single-column drill-down below ~640px still applies; resizing is a desktop-width feature only |
| Overflow actions | Add feed, Import/Export OPML, New folder, Refresh all, Reading stats, Keyboard shortcuts move behind a "..." menu in the top toolbar |
| Dialogs | Native `<dialog>` (`showModal()`/`close()`), no library, replacing inline forms and native `confirm()` |
| Icons | New shared toolbar-icon partial reused across Reader/Notes/Paste, replacing ad hoc inline SVG per template |
| Filters | All / Unread / Started as toggle-button pills in the top toolbar (existing filter semantics, new presentation) |

## 2. Layout: resizable three-pane grid

Today `.reader-panes` (`internal/ui/static/app.css` ~1649) is a fixed CSS
grid: `grid-template-columns: minmax(12rem,16rem) minmax(16rem,24rem) 1fr`.
This is replaced with a flex row plus two draggable gutters:

```
[ tree pane ] | gutter | [ article list pane ] | gutter | [ reading pane ]
   ~14rem                    ~20rem                        flex: 1
```

- A new `resizable-panes.js` module attaches `pointerdown` on each
  `.pane-gutter`, then `pointermove`/`pointerup` on `document` for the drag.
  Each pane's width is clamped: tree 10–24rem, list 14–32rem. The reading
  pane always absorbs the remainder (`flex: 1`), so the constraint "reading
  pane is always largest" holds by construction — the module never lets tree
  or list width exceed the reading pane's remaining space.
- On drop, both pane widths are written to
  `localStorage['reader.paneWidths'] = {tree: <px>, list: <px>}`. On load, the
  script reads this before first paint (inline `<script>` at the top of
  `panes.partial.html`, guarded by `try/catch` since `localStorage` can throw
  or be absent) and applies widths as inline styles, falling back to the CSS
  defaults if nothing is stored or the value is malformed.
- New CSS: `.resizable-panes` (flex container), `.pane-gutter` (thin
  draggable divider, `cursor: col-resize`, existing theme border colour),
  `.pane-gutter:hover`/`.pane-gutter.dragging` for visual feedback.
- The existing mobile breakpoints (`~900px` hides the reading pane,
  `~640px` collapses to the checkbox-driven single-column drill-down) are
  untouched: below those widths the flex/gutter layout simply isn't rendered
  interactively — gutters are hidden via the same media queries.

This is new infrastructure (no resizable-pane code exists anywhere in the
repo today), written generically enough that Notes or Paste could adopt the
same `resizable-panes.js` + `.resizable-panes` pair later if wanted, but nothing
in this pass modifies Notes or Paste templates.

## 3. Top toolbar

A new toolbar spanning the full width above all three panes, in
`panes.partial.html`:

- **Left:** filter pills — All / Unread / Started — as compact toggle
  buttons (`.toolbar-btn` style, one marked active via `aria-pressed`),
  replacing whatever filter control exists today. Selecting one issues the
  same HTMX request the current filter mechanism uses; only the presentation
  changes.
- **Right:** a single "..." icon button (`.toolbar-btn`) that toggles a
  dropdown menu (plain CSS/JS — a `<div class="menu" hidden>` toggled on
  click, closed on outside-click or Escape). Menu items:
  - Add feed… → opens the add-feed `<dialog>`
  - Import OPML… → opens the import `<dialog>`
  - Export OPML → direct download link, no dialog
  - New folder… → opens the new-folder `<dialog>`
  - Refresh all feeds → same action as today's refresh button
  - Reading stats → navigates to `/reader/stats`
  - Keyboard shortcuts → opens a `<dialog>` (converted from the current
    `<details>` block, same content)

## 4. Middle toolbar (above the article list pane)

Only one action here: **Mark all read** for the currently open list, restyled
as a compact `.toolbar-btn` icon button. (Two ideas originally listed here —
a folder-select and a per-feed refresh — don't correspond to anything in the
codebase: the only folder-select today picks a folder for a *new*
subscription, and there is no per-feed refresh route, only
`POST /reader/refresh`, which refreshes every due feed. Corrected 2026-09-14,
before implementation, once the mismatch surfaced during planning.)

## 5. Dialogs

Four native `<dialog>` elements replace inline forms and `confirm()`:

- **Add feed** — URL input + folder picker (today's inline form, moved
  as-is into a dialog body)
- **Import OPML** — file input (today's inline link/form, moved into a
  dialog)
- **New folder** — name input (today's inline form, moved into a dialog)
- **Keyboard shortcuts** — static help content (today's `<details>`,
  converted to a dialog)

Delete/unsubscribe actions, which currently use `hx-confirm` (native
`window.confirm`), get a shared confirm `<dialog>` instead — one reusable
component (`confirm-dialog.partial.html` or similar) parameterised by message
and the HTMX attributes to fire on confirm, so this isn't four bespoke
confirm dialogs.

All dialogs open via `showModal()` and close via a Cancel button, an "X", the
Escape key (native `<dialog>` behaviour), or form submission. Amended
2026-09-14, after implementation, to match the built behaviour: every dialog
form's HTMX request does a full `outerHTML` swap of `#reader-panes`, and that
swap closes the dialog on *any* response, success or failure — there is no
inline-error, stays-open state. Validation errors and notices instead surface
as a page-level `.reader-banner` (`.Error` / `.Notice` set on the view model
and rendered above the panes), the same banner used for non-dialog actions.

One carve-out to "closes unconditionally": the add-feed dialog, when a pasted
site turns out to offer several feeds, re-renders with `data-reopen` set and
its discovery chooser inside — `reader.js`'s `htmx:afterSwap` listener sees
that attribute and calls `showModal()` again, so to the user the dialog
appears to have stayed open through the extra step rather than closed and
reopened. This is not the stays-open-on-error state the paragraph above rules
out; the swap still happened, the dialog still closed, reader.js just
reopened it immediately for this one flow.

## 6. Icons

A new shared toolbar-icon source — extending `internal/ui/icons.go` or a new
sibling file — holds the SVGs reused across the toolbars: refresh, add
(plus), menu-dots, filter/pill check, star, external-link, folder,
import/export (arrow-down/up), trash, close (X). Reader's new toolbars
consume this; Notes and Paste keep their existing inline SVGs for now
(no template changes outside Reader in this pass), but the new shared
source is written so a future pass can point their icons at it too.

## 7. What stays the same

- Article list ↔ reading pane HTMX flow and OOB swap conventions
  (`panes-oob`, `detail-with-list`-style fragments)
- Keyboard shortcuts (j/k/o/m/s/r) and their behaviour — only their
  discoverability moves (details → dialog)
- Read/unread/star state model, feed polling, full-article extraction,
  OPML format — no domain logic changes
- Reading-stats page (`stats.html`) — unchanged, just reachable from the
  new menu instead of an inline link

## 8. Testing

- Existing Go integration tests for Reader routes/handlers are unaffected
  (no route/handler changes — this is templates, CSS, and two new JS
  modules).
- Manual verification via the `run` skill / browser: drag each gutter and
  confirm persistence across a reload; open and submit each dialog; confirm
  the "..." menu closes on outside-click/Escape; confirm mobile breakpoints
  still collapse to the existing single-column drill-down with gutters
  hidden.
