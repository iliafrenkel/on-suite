# ON Reader tree/list polish design

Status: approved 2026-09-15. A second small polish pass over the ON Reader
frontend, following
[2026-09-14-on-reader-ui-polish-design.md](2026-09-14-on-reader-ui-polish-design.md).
Five independent, small fixes to the feed tree and article list — no route,
storage, or domain-logic changes except one new cookie-backed preference and
one new POST endpoint for it.

## 1. Decisions

| # | Item | Choice |
|---|---|---|
| 1 | Feed indentation | CSS-only: indent `.reader-sub` relative to its folder |
| 2 | Copy feed URL | New "Feed URL" outline-menu item, client-side clipboard copy, inline "Copied!" confirmation |
| 3 | Hide read feeds | New toolbar toggle; cookie-persisted; empty folders hide too |
| 4 | Folder menu hover bug | CSS scoping fix only |
| 5 | Stale bold on read | Targeted OOB swap of the single list row, same technique as `counts-oob` |

## 2. Feed indentation

Today `.reader-sub` (a feed inside a folder, `internal/ui/static/app.css:1893-1900`)
and `.reader-node` (a root-level feed) share identical padding — nothing
visually distinguishes a feed nested in a folder from the folder header
itself. Fix: give `.reader-sub` additional `padding-left` (indented past the
folder `<summary>`'s own left edge — matching Notes' existing outline
indentation convention, see `PATTERNS.md`) in `app.css`. `.reader-node`
(root-level, no folder) is unaffected. Pure CSS; no template or Go changes.

## 3. "Feed URL" menu item

`Subscription.FeedURL` (`internal/apps/reader/store.go:90`) is already loaded
for every row the tree template ranges over, so no new data needs to reach
the template.

- Add a "Feed URL" button to each feed's outline menu, alongside the existing
  "Unsubscribe" (`panes.partial.html:199-210` for folder feeds, `:222-233`
  for root feeds — both copies get the new item).
- The button carries the URL in a `data-feed-url="{{.FeedURL}}"` attribute
  (safe: `FeedURL` is a stored, previously-validated feed address, not
  arbitrary user HTML) and a class (e.g. `reader-copy-feed-url`) `reader.js`
  binds a click handler to.
- Handler: `navigator.clipboard.writeText(url)`, then swaps the button's
  visible label to "Copied!" for ~1.5s before reverting — no toast, no new
  DOM element, matching how small a change this is. If the Clipboard API
  throws (insecure context, permission denied), the label falls back to
  "Couldn't copy" for the same interval rather than failing silently.
- Pure client-side action: no new route, no server round trip.

## 4. Hide feeds with no unread items

Follows the exact pattern Notes already uses for its show-completed
preference (`internal/apps/notes/handlers.go:625-671`): a plain-POST-settable,
non-secret, `HttpOnly`, long-`MaxAge` cookie, read back on every tree render.

- New cookie `reader_hide_read` (`"0"`/`"1"`), `Path: "/reader/"`, same
  `Secure`/`SameSite`/`MaxAge` conventions as `ShowCompletedCookie`.
- New handler `POST /reader/prefs` (mirroring Notes' `prefs`): validates the
  posted value, sets the cookie, then re-renders the panes (HTMX) or redirects
  back (non-HTMX/no-JS fallback).
- `viewTree` (`internal/apps/reader/view.go:127`) gains a `hideRead bool`
  parameter. When true:
  - Drop any `Subscription` from a folder's `Subs` or from `Root` when
    `counts.BySub[id] == 0`.
  - Drop a folder entirely once every one of its subs has been filtered out
    (confirmed with the user: an all-read folder disappears too, not just its
    feeds — keeps the tree fully decluttered).
  - `Empty` is computed the same way it is today, from the *filtered* result,
    so "no feeds yet" only shows when there really are none, not when
    everything is just hidden.
- Toolbar: a small icon toggle button in `.reader-filters`
  (`panes.partial.html:100-136`), alongside Unread/Starred/All, POSTing to
  `/reader/prefs` and reflecting the cookie's current value via the same
  active-state styling those pills use (`toolbar-btn-active`/`aria-pressed`).
- This is a display filter only — it never changes read/unread state, counts,
  or which feeds exist; it only decides which existing rows `viewTree` returns.

## 5. Folder outline-menu hover bug

Root cause: `.reader-folder:hover .outline-menu-toggle`
(`internal/ui/static/app.css:1948`) matches every `.outline-menu-toggle`
*inside* the folder's `<details>` box — which wraps the folder's own row
**and** its nested `<ul>` of feed rows — so hovering anywhere in an expanded
folder reveals every row's "..." toggle, not just the one under the pointer.

Fix: scope the folder's own toggle visibility to its header line instead of
the whole `<details>` box — key it off `.reader-folder > summary:hover` /
`:focus-within` (plus the existing `.reader-folder-menu[open]` rule, kept as
is for when the menu itself is open). This matches how `.reader-node` and
`.reader-sub` are already correctly scoped to their own row, and does not
touch those rules.

## 6. Mark-as-read visual update

`handlers.go:380-385` documents, correctly, why the list pane isn't
re-rendered wholesale when an article opens: under the Unread filter, a fresh
list would drop the just-opened item out from under the reader. That
constraint stays. The fix is a *targeted* single-row update, the same
technique `counts-oob` already uses for the sidebar numbers:

- Give each list row a stable id: `<li id="reader-row-{{.ID}}" class="reader-row...">`
  (`panes.partial.html:386`).
- `article-swap` (`panes.partial.html:409`) gains a third OOB fragment,
  `row-oob`, that re-renders just the opened item's `<li>` — same markup,
  `is-read` now present — with `hx-swap-oob="true"` targeting
  `#reader-row-{{.ID}}`.
- This needs the same `listItem` data the row template already renders from,
  so `article-swap`'s call site threads through the one item being read
  (already available where `SetRead` is called, `handlers.go:469`) rather
  than the whole list.
- Net effect: the row's text goes from bold to normal the instant an article
  is opened (mouse or the `o`/Enter keyboard shortcut — both go through the
  same `/reader/item/{id}` route), in place, without reordering or
  disappearing — the "lesser of two staleness problems" becomes no staleness
  for the one property (bold) users actually notice, while the documented
  Unread-filter concern (item vanishing) is untouched because the row itself
  is never removed by this swap.

## 7. What stays the same

- Every other read/unread/star/count mechanic, the `panes-oob`/`counts-oob`
  OOB conventions, and the mobile drill-down layout are untouched.
- No changes to feed polling, OPML, full-article extraction, or the domain
  model beyond the one new cookie preference.

## 8. Testing

- New Go handler test for `POST /reader/prefs`: sets the cookie, re-renders,
  toggling it hides an all-read folder/feed and restores it.
- `viewTree` unit tests: hideRead=true drops zero-unread subs and empty
  folders; hideRead=false is unchanged from today.
- Manual verification via the `run` skill / browser: confirm indentation,
  confirm "Feed URL" copies and shows "Copied!", confirm the hide-read toggle
  persists across a reload, confirm hovering one feed in an open folder no
  longer reveals every row's "..." toggle, confirm opening an article (click
  and keyboard) drops the bold on that row immediately without the row moving
  or disappearing under the Unread filter.
