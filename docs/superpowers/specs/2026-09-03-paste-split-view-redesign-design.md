# ON Paste: split-view redesign

## Problem

ON Paste currently has two separate full pages — a list (`internal/apps/paste/templates/list.html`) and a detail view (`internal/apps/paste/templates/view.html`) — reached only by full page navigation. Selecting a snippet means leaving the list; there's no toolbar, and actions (Raw, Copy, Share, Delete) are plain links/forms with no consistent visual language. ON Notes already established a "unified toolbar" pattern (inline SVG icons in `.toolbar-btn` pills, see `internal/apps/notes/templates/outline.html` and `internal/ui/static/app.css:898-970`) that Paste should adopt, and the split-view browsing pattern (list + detail, select-to-preview) doesn't exist anywhere in the codebase yet — this introduces it.

## Goals

- One page (`/paste`) with a list pane (left) and a view pane (right); selecting a snippet shows it immediately, no navigation.
- A unified toolbar above the view pane, styled like Notes' toolbar (inline SVG + label `.toolbar-btn`s).
- Inline edit mode in the view pane — no separate edit page.
- URLs stay meaningful: `/paste/{id}` is bookmarkable/shareable and browser back/forward works.
- New paste creation happens inside the view pane, not a separate page.
- Reasonable mobile behavior: list and detail don't both try to fit a narrow screen at once.

## Non-goals (explicitly deferred)

- Search/filter on the snippet list.
- Two-step "arm then confirm" delete — keep the existing `confirm()` dialog.
- Any change to sharing, highlighting, or storage logic (`store.go`, `highlight.go`, `export.go` are untouched).

## Design

### Page structure & routing

`GET /paste` and `GET /paste/{id}` both render one page: a shell with `.paste-list` (left, fixed width, existing item markup) and `#detail` (right, fills remaining space). `/paste` renders `#detail` empty/with a placeholder ("Select a snippet"); `/paste/{id}` renders it with that snippet, and marks the matching list row active (server-side, based on the requested `{id}`).

Every interactive element in the list pane and toolbar becomes an `hx-get` request targeting `#detail`, with `hx-push-url="true"` so the address bar tracks the current view. Each such link/button keeps a real `href`/`formaction` too, so it degrades to full-page navigation with JS disabled — the same progressive-enhancement branch Notes uses:

```go
if web.IsHTMX(r) {
    a.renderDetailFragment(w, r, ...)
    return
}
// full page render
```

(see `internal/apps/notes/handlers.go:285` for the existing pattern; `internal/platform/render/render.go:212` `Renderer.Fragment` renders the isolated block). This is a new use of `hx-get` + `hx-push-url` in this codebase — Notes only used `hx-post` with `hx-swap="none"`/`innerHTML` and no URL sync — but both are native htmx attributes already vendored (`internal/ui/static/htmx.min.js`).

### List pane

Header: "Snippets" + a `toolbar-btn` "+ New" button (SVG plus icon). Below: the existing `<ul class="snippet-list">` markup (title, `shared` tag, preview, language/lines/date), each row's link converted to `hx-get="/paste/{id}" hx-target="#detail" hx-push-url="true"` (href unchanged). The row matching the currently-open snippet gets a new `.snippet-row-active` class for a visible selected state. Empty state ("No snippets yet…") unchanged. No search/filter in this pass.

### View pane & toolbar

A `.notes-toolbar`-style bar sits above the content:

- **View mode** (default): title as plain text, meta line (language · lines · date) as today, toolbar buttons Edit / Raw / Copy / Share (or Unshare) / Delete — each an inline SVG (`.toolbar-icon`) + label, reusing `.toolbar-btn` from `internal/ui/static/app.css:898-970`. Content is the existing `.snippet-body` highlighted block.
- **Edit mode** (toggled by Edit, `hx-get` to an edit fragment for that same `{id}`, still targeting `#detail`): title becomes a text `<input>`, a language `<select>` appears (same options as the New Snippet form), content becomes a `<textarea>`. Toolbar swaps to Save / Cancel. Save is `hx-post` to the existing update path, targets `#detail`, and also updates the corresponding list row via `hx-swap-oob` (title/preview/date may have changed) — `hx-swap-oob` is an established pattern already used in Notes.
- No placeholder state when nothing is selected on desktop beyond a simple "Select a snippet to view it" message in `#detail`.

Delete keeps its existing `data-confirm` browser-`confirm()` flow, just moved into the toolbar. On success, `#detail` clears back to the placeholder and the deleted row is removed from the list pane (oob-swap or a full list-pane refresh — implementation's choice, list is small).

### New paste flow

The "+ New" button is `hx-get="/paste/new" hx-target="#detail" hx-push-url="true"`, rendering the same title/language/content form inside `#detail`, toolbar showing Save/Cancel. Save posts to the existing `/paste/new` handler; on success the new snippet becomes the active detail view and the list pane's `<ul>` is refreshed (oob-swap of the whole list is simplest and avoids sort-order edge cases, given the list's current size/complexity).

### Mobile / narrow viewport

Below the existing `640px` breakpoint (`internal/ui/static/app.css:1426`), the two-pane layout collapses to one column via CSS: list pane full-width, `#detail` hidden by default. Opening a snippet (or New) shows `#detail` full-width in place of the list, with a "← Back" toolbar button (visible only below the breakpoint) that returns to showing the list. No separate templates or JS branching — same DOM, CSS-driven visibility toggle plus one small conditional button.

## Testing

- Manual verification in-browser (this is server-rendered Go + htmx, no component test framework in place for these templates): select a snippet, edit and save it, create a new one, delete one, verify list stays in sync in each case, verify direct navigation to `/paste/{id}` and browser back/forward, verify the mobile breakpoint.
- Existing Go handler tests (if any) extended to cover the new fragment-rendering branches (`IsHTMX` true/false) for list, detail, edit, and new-paste handlers.
