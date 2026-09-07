# ON Notes: search as inline filter + highlighting

## Problem

Search (`/notes/search`) is a separate full-page results list: a flat `<li>`
per hit with an ancestor breadcrumb, no highlighting of the matched term, and
no indication of whether a hit matched the title or the note body (issue
[#86](https://github.com/iliafrenkel/on-suite/issues/86)). Getting to a result
means leaving whatever you were looking at (Outline, Due, or Archive) and
losing your place.

## Goals

- Search becomes a live filter over the page you're already on (Outline, Due,
  or Archive), not a separate page.
- On Outline, filtering shows the matched note's ancestor path (so a deep
  match stays legible) without pulling in unrelated siblings or descendants
  that didn't match.
- On Due and Archive, filtering narrows the existing flat breadcrumb list.
- Matches are visually highlighted, and it's clear whether a hit is in the
  title or the note body — directly resolving #86.
- Filtering feels responsive: partial words match as you type, not just
  whole completed words.

## Non-goals (explicitly deferred)

- Any change to what's indexed (still title + note text only; no tags,
  due dates, etc. as searchable fields).
- Saved searches, search history, or search operators (`AND`/`OR`/quoted
  phrases beyond the existing per-word tokenization).
- Sharing/bookmarking a filtered Due or Archive view beyond basic
  `hx-push-url` query-string support (no separate short-link feature).

## Design

### Data flow

**FTS5 schema change.** The current `notes_fts` table has no prefix index, so
`MATCH` only finds whole-word tokens. A new migration recreates the virtual
table with prefix indexing enabled and rebuilds it
(`INSERT INTO notes_fts(notes_fts) VALUES('rebuild')`), so a query like `cat`
matches `category` while still being typed. `ftsQuery` changes to build a
prefix-matching query (each token becomes a quoted-prefix term, e.g. `"cat"*`)
instead of an exact-token match.

**Outline (tree filter).** Given a query, run the (now prefix-aware) FTS5
match scoped to the current zoom subtree and user, producing a set of
matching node IDs. Walk ancestors of every match (existing `AncestorsMany`
recursive CTE) to build a *keep set* = matches ∪ their ancestors. Render the
outline as usual, but:
- Skip any row not in the keep set.
- Force a row into its expanded state (ignore its stored `collapsed` flag) if
  it's present only to give ancestor context for a match below it.
- A row's own children that didn't match, and aren't themselves an ancestor
  of a match, are not shown — only the path down to each match, not full
  subtrees either up or down.

**Due / Archive (flat filter).** These are already flat lists of `Node` +
ancestor `Crumbs` (no tree nesting to preserve). Filtering just adds the same
prefix FTS5 match as a condition on the existing `Store.Due` / `Store.Archive`
queries — no ancestor-walk needed, since every row already carries its own
breadcrumb.

### Highlighting

Rows are already rendered through the existing Markdown-ish `Render()`
pipeline (bold/italic/code/tag-and-mention chips) before reaching the
template. Highlighting is applied **after** that rendering, walking the
resulting HTML and wrapping matches only in actual text content — never
inside a tag or an attribute (e.g. `href="..."`) — in
`<mark class="notes-search-hit">`. This is deliberately chosen over feeding
raw text through SQLite FTS5's own `highlight()` function first: wrapping raw
text before Markdown rendering risks corrupting `**bold**`/`` `code` ``
syntax that straddles a match boundary. Post-render, text-node-only wrapping
avoids that at the cost of a small amount of custom HTML-walking code.

A row counts as matching in the note body (not just the title) when the raw
note text contains a hit even if the title doesn't.

**Due/Archive note-only snippet.** Since these rows show title only (no note
body), a note-only match adds a short excerpt of raw note text around the
first hit, rendered and highlighted the same way, truncated with an ellipsis
on either side if cut off — so it's visible *why* the row matched, addressing
the exact gap #86 called out.

### Wiring (HTMX)

The shared toolbar search box (`toolbar.partial.html`) changes from a plain
`GET` form pointed at `/notes/search` to `hx-get` against whichever page it's
currently on, `hx-trigger="input changed delay:300ms"`,
`hx-target` set to that page's list container (`#outline`, and new fragment
targets added for Due — mirroring Archive's existing `#archive-list`
fragment — since Due doesn't currently have one), and `hx-push-url="true"` so
the `?q=` filter survives refresh and back/forward navigation.

`/notes/search`, its handler, and its template are deleted outright — the
toolbar box on each page now filters that page in place instead of
navigating away. A plain (non-HTMX) page load with `?q=` in the URL must
still render the filtered view correctly, since this is progressive
enhancement over a real query parameter, not a client-only feature.

### Edge cases

- Empty query: normal unfiltered view, rows honour their real `collapsed`
  state again.
- No matches: an inline "No notes match" message replaces the list, styled
  consistently per page (not a full-page empty state).
- "Show completed" preference still applies while filtering.
- Archived nodes/subtrees stay excluded from the Outline filter, same as
  today.
- Filtering is scoped to the current zoom subtree on Outline — it does not
  search outside whatever subtree you're currently zoomed into.
- The `/` keybinding (focus search box) and a clear (×) button both continue
  to work against the new inline behaviour.

### Styling

`<mark class="notes-search-hit">` uses an accent tint from the existing warm
palette (cream/teal/orange) rather than the browser-default `<mark>` yellow —
a small, palette-only addition to `internal/ui/static/app.css`.

## Testing

- **Store-level:** prefix matching returns expected nodes; Outline's
  ancestor keep-set is correct across tree shapes (deep nesting, multiple
  matches sharing an ancestor, a match at a top-level node); archived and
  show-completed filtering still respected during a filtered query.
- **Highlighter:** matches wrapped correctly in plain text and around
  existing tags (bold, code, tag/mention chips, links); case-insensitive;
  prefix matches; multiple query tokens; no false wraps inside HTML
  attributes.
- **HTTP/handler, per page (Outline/Due/Archive):** `?q=` filters and
  highlights correctly; empty-match state; `hx-push-url` present; a plain
  (non-HTMX) full page load with `?q=` renders the same filtered result.
- **Removal:** `/notes/search` route, handler, and template are gone, with no
  dangling links to it from nav or the toolbar partial.
