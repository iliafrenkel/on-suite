# ON Reader — Targeted swaps for list navigation (issue #453)

## Problem

Selecting a feed, All or Starred, changing the filter, searching, or
"Mark all read" all swap the whole `#reader-panes` (`outerHTML`). The visible
flicker comes from the resizable pane widths: `reader.js` keeps them as inline
custom properties on `#reader-panes-row`, which is inside the swapped region.
The new markup arrives without them and JS can only restore them at
`htmx:afterSettle`. Measured with stored widths 300/420px:

| t     | event  | tree / list |
|-------|--------|-------------|
| 0ms   | click  | 300 / 420   |
| 20ms  | swap   | 240 / 320 (CSS defaults) |
| 50ms  | settle | 300 / 420   |

Roughly two frames at the wrong widths, with every pane reflowing twice. The
full swap also reopens collapsed folders (`<details open>` is always
rendered), resets the tree's scroll position and rebuilds every favicon.

## Goal

The frequent navigation actions replace only what they change. The tree DOM
is never replaced by them, so pane widths, collapsed folders, tree scroll and
favicons are untouched. The rare tree-editing actions keep the full swap, but
no longer jump either.

## Scope

**Targeted** (new `list-swap` response):

- tree feed links
- the All / Starred nav buttons
- the Unread / Starred / All filter pills
- the search box
- the Mark all read form, including its "older than" buttons

**Unchanged full `panes-oob` swap:** subscribe, unsubscribe, rename, move,
create/delete folder, refresh (all and per feed), OPML import, the hide-read
toggle, and the chooser. No-JS requests and htmx history restores get the full
page as today.

**Out of scope:** opening an article (already targeted via `article-swap`) and
the dead-favicon flash on full swaps (filed as a separate issue).

## Design

### Routing: one endpoint, two responses, chosen by `HX-Target`

htmx sends the id of the swap target in the `HX-Target` request header. The
targeted controls change their `hx-target` from `#reader-panes` to
`#reader-list`, still `hx-swap="outerHTML"`. At the end of `renderPanes`, for
an htmx request that is not a history restore:

- `HX-Target: reader-list` renders the `list-swap` block
- anything else renders `panes-oob`, as today

URLs, routes and handlers do not change. Add `web.HTMXTarget(r) string` next to
`web.IsHTMX` in `internal/platform/web`.

### `list-swap` contents

Main target:

- the list `<section>`, which now has `id="reader-list"`

Out of band:

- `<title>` and `shell-crumb-tail`, exactly as in `panes-oob`
- the toolbar (`id="reader-toolbar"`), swapped whole: the filter pill hrefs
  depend on the selected list
- the banner (`id="reader-banner"`). It is now always rendered, empty when
  there is no error or notice, so a targeted swap clears a stale message
- the article pane, empty for a new list. `article` gains an `OOB` flag, the
  same way `row` has one
- both pane-state checkboxes (`#reader-list-open`, `#reader-article-open`), so
  the narrow layout still drills down into the list
- `counts-oob` (existing)
- `#reader-tree-hidden-all` (see Hide read)

### Tree highlight sync (`reader.js`)

The list section carries the state it was rendered for:
`data-scope="all|starred|feed"`, `data-sub="<id>"` (0 when no feed is
selected), `data-filter` and `data-q`. Feed rows get `id="reader-sub-<id>"`; folders get
`id="reader-folder-<id>"`.

One function, `syncTree()`, runs on `htmx:afterSettle` when `#reader-list` was
the target. It:

- moves `is-active` to the matching `.reader-sub` row, or clears it for All
  and Starred
- moves `toolbar-btn-active` between the All and Starred nav buttons
- applies the hide-read removals below

The server stays the source of truth. JS only reflects what the list says.

### Keeping the tree honest

The tree and the dialogs are not re-rendered by `list-swap`, which causes two
problems, both handled in `reader.js`:

- **Stale list context.** Their forms carry the list on screen in hidden
  `reader-ctx` fields (`scope`, `sub`, `filter`, `q`). After "open feed A,
  click All, rename feed B" the POST would re-render feed A. `syncTree()`
  therefore copies `data-scope`, `data-sub`, `data-filter` and `data-q` into
  those hidden inputs under `#reader-panes`, except inside `#reader-article`,
  whose forms carry their own context (including `view`).
- **A feed the tree lacks.** A list swap can remove a feed (hide read) but
  never add one back. An `htmx:configRequest` listener sends
  `X-Reader-Tree: <ids the tree has>` on list requests. If the server's tree
  has a subscription not in that list, it answers with `panes-oob` plus
  `HX-Retarget: #reader-panes` and `HX-Reswap: outerHTML`, so the full panes
  replace the tree. An absent header keeps the plain list swap.

### Hide read

With hide-read on, today's full swap drops fully read feeds from the tree when
the reader navigates away from them (`filterUnread` keeps only the active one)
and shows "Everything is read" once nothing is left. The targeted swap keeps
that behaviour as follows:

- `viewTree` also computes the subscription and folder ids the filtered tree
  dropped. The list renders them as `data-hidden-subs` and
  `data-hidden-folders` (space-separated ids, empty when hide-read is off).
- `syncTree()` removes each listed `#reader-sub-<id>` and
  `#reader-folder-<id>` that is still in the DOM.
- The "Everything is read" paragraph is always rendered as
  `#reader-tree-hidden-all`, with `hidden` when it does not apply, and rides
  along in `list-swap` out of band. Check that `.empty`'s `display` does not
  override `[hidden]`.

OOB `delete` swaps were rejected for this. htmx 2.0.10 calls `console.error`
and fires `htmx:error` for every OOB element with no target. The server cannot
know which rows the browser still has, so it would have to re-send deletes for
rows that are already gone on every click.

### Pane widths

`reader.js` sets `--reader-tree-w` / `--reader-list-w` on
`document.documentElement` instead of `#reader-panes-row`. The CSS reads them
through `var(..., default)` on the panes, so inheritance from `<html>` works
unchanged. No swap can wipe them, so the remaining full swaps do not jump
either. The `afterSettle` re-init still re-binds the gutter listeners (the
gutters are inside the swapped row), but it no longer re-applies widths or
adds another `DESKTOP_QUERY` change listener on every swap. That listener is
registered once.

## Testing

Handler tests (`apptest` + `htmlassert`) in `handlers_test.go`:

- a feed GET with `HX-Request` and `HX-Target: reader-list` returns
  `#reader-list` plus the OOB title, crumb, toolbar, banner, article,
  checkboxes and counts, and no `#reader-panes`
- the same GET without the target header still returns `panes-oob`
- Mark all read with `HX-Target: reader-list` returns `list-swap` with
  updated counts
- with hide-read on, `data-hidden-subs` / `data-hidden-folders` list exactly
  the dropped ids, and `#reader-tree-hidden-all` is unhidden when everything
  is read
- the list section's `data-scope` / `data-sub` match the request
- the targeted controls render `hx-target="#reader-list"`; tree-editing
  controls still target `#reader-panes`

There is no JS test harness, so check in the browser on a local instance:

- with stored widths, tree/list widths stay at the stored values throughout a
  feed click and a Mark all read (the frame probe from the investigation)
- the `.reader-tree` element is the same DOM node before and after a feed
  click
- a collapsed folder stays collapsed
- the highlight moves correctly between feeds, All and Starred
- hide-read removes the right rows and shows the note
- the console stays clean
- the narrow (phone) layout still drills down into the list
