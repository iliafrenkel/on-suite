# Notes: context-menu row controls

## Problem

Each note row currently reserves roughly half its width for controls
(move up/down, indent/outdent, add-bullet, due-date chip/input, delete)
that are invisible until hover (`app.css:905-917`). The row layout is
cramped, and the space is wasted whenever the row isn't hovered.

## Goal

Adopt a Workflowy-style row: a small `···` button on the far left opens a
context menu holding every action; a subset of actions also appears as a
hover-only overlay that floats over the row (not reserving layout space)
for fast mouse access. No backend changes — every action already has a
working endpoint; this is purely a template/CSS reorganization.

## Row layout

Before:

```
[done] [chevron] [dot] [title/note — flex:1] [move↑ move↓ indent outdent +] [due chip] [delete ×]
```

After:

```
[···] [chevron] [dot] [title/note — flex:1] [due chip, if set]
```

with a hover-only overlay floating over the row's right edge (see below).

- **`···` button** — always visible, leftmost (where the done checkbox
  used to sit). Opens the context menu.
- **Chevron & dot** — unchanged (chevron stays hover-reveal, only shown
  when the row has children; dot stays always-visible zoom-in link).
- **Title/note text** — unchanged, still `flex: 1; min-width: 0`.
- **Due chip** — unchanged: always visible when a due date is set.

## Context menu (`···`)

Comprehensive list — the fallback for touch devices and the home for
less-frequent actions:

- Mark done / not done
- Edit due date (a date input, submits on change — same behavior as
  today's `.outline-due-input`, just relocated)
- Move up
- Move down
- Indent
- Outdent
- Add bullet
- Delete (keeps its `hx-confirm`)

Implementation: reuse the existing `<details>/<summary>` popover pattern
from the settings gear menu (`base.html:39-58`, CSS `app.css:214-239`).
No JS required for open/close; works under the CSP (no inline scripts);
keyboard-accessible for free via native `<details>` behavior.

Each menu item is a `<button>`/`<input>` inside the row's existing
`<form class="outline-main">`, posting to the same endpoints already used
today (`/notes/{id}/done`, `/move`, `/indent`, `/outdent`, `/due`,
`/delete`) with the same `hx-target="#outline" hx-swap="innerHTML"`.

Open question to resolve during implementation: whether `<details>`
should auto-close after a menu action submits (via `hx-swap` replacing
the row) or whether it's fine left as default browser behavior. Try the
naive version first (no extra JS); only add closing behavior (e.g.
`hx-on::after-request`) if leaving it open reads as broken in practice.

## Hover overlay (quick access)

Duplicates 4 of the structural actions from the menu, for mouse users who
don't want to open the menu each time:

- Move up
- Move down
- Indent
- Outdent
- Add bullet

Shown via `:hover`/`:focus-within` on `.outline-row`, same trigger as
today. Key change from today: positioned with `position: absolute; right:
0; top: 0` (or similar) so it overlays the row instead of sitting in the
flex flow — it must not reserve width when hidden, unlike the current
`opacity: 0` approach which keeps the space reserved.

Still uses `opacity`, not `visibility`, so the buttons remain
keyboard-focusable (per the existing comment in `app.css:905`).

## Explicitly out of scope / unchanged

- No new Go handlers — all actions reuse existing endpoints in
  `internal/apps/notes/handlers.go`.
- No changes to keyboard shortcuts (`notes.js:210-303`) — they call
  endpoints directly and don't depend on menu/overlay markup.
- No changes to chevron/dot behavior.
- Mobile/touch: covered by the menu being comprehensive; no separate
  tap-to-reveal-overlay behavior is being built.

## Files involved

- `internal/apps/notes/templates/outline.html` (rows 105-202) — row
  markup restructuring.
- `internal/ui/static/app.css` (roughly lines 662-945) — layout, hover
  overlay positioning, menu styling (extending the `.shell-settings*`
  pattern for a `.outline-menu*` variant).
