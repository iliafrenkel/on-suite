# ON Suite — ON Notes

**Date:** 2026-08-25
**Status:** Proposed
**Scope:** A hierarchical outliner app at `/notes/` — one infinite tree per
user, keyboard-driven, with zoom, done state, due dates, search, archiving,
Markdown import/export and public read-only share links. It is an app under
`internal/apps/notes`, so it changes nothing about the platform.

## 1. Purpose

ON Notes is an outliner in the Workflowy mould: every note is a bullet, every
bullet can have children, and the tree is the only organising structure. It
serves two use cases at once — keeping notes, and tracking work — because in
an outliner they are the same activity. A bullet with a due date is a task; a
bullet without one is a note; both live in the same tree and move between
those roles by adding or clearing a date.

The design goal that drives everything below is **it has to feel like an
outliner, not like a form**. Pressing Enter has to produce a new bullet with
the caret in it, immediately. That single requirement is what forces the
client/server split in §7; everything else follows the platform's existing
grain.

## 2. Constraints

Each of these is an existing rule of the project, and each drives a specific
decision.

| Constraint | Decision |
|---|---|
| HTMX is the only JS; no framework, no build step | The app ships one hand-written, un-bundled `notes.js`. It implements no behaviour of its own — it re-issues the same requests the no-JS forms issue (§7). |
| Strict CSP, no inline `<script>` | `notes.js` is served from the app's own embedded FS at `/notes/notes.js`. `script-src 'self'` already permits this, the same way ON Paste serves `/paste/highlight.css`. No CSP change. |
| No new dependencies ([CONTRIBUTING.md](../../../CONTRIBUTING.md)) | FTS5 for search is present in `modernc.org/sqlite v1.56.0`, already in `go.mod` — verified, not assumed. Markdown is a small hand-written inline renderer, not a library. |
| Apps never import each other; the platform never imports an app ([arch_test.go](../../../internal/arch/arch_test.go)) | Everything lives in `internal/apps/notes`. The only platform-facing surface is `app.App` plus the optional `Exporter` and `Stater` capabilities. |
| Migrations are forward-only | Each build chunk (§18) adds its own columns with its own numbered migration rather than the schema arriving whole. |
| One SQLite file, `SetMaxOpenConns(1)` | There is no write concurrency to design around. This is what makes contiguous integer sibling ordering the right choice over LexoRank (§4). |
| Routing is default-deny | Exactly one route is `Public`: the share page. Everything else goes through `Handle`. |

## 3. Decisions taken during brainstorming

| Question | Decision | Rejected |
|---|---|---|
| How instant does editing feel? | Hybrid: text editing is local with debounced autosave; structural operations round-trip to the server, which stays the source of truth. | A client-owned tree with optimistic sync (largest JS surface in the suite, and most behaviour becomes untestable from Go). A pure HTMX round-trip per keystroke (types like a form). |
| Shape of the tree | One infinite tree per user. Every node is identical: a title line plus an optional secondary note. Top-level bullets are the user's "notebooks"; zoom is how you focus. | Title-only nodes. Multiple named documents. |
| How far does task management go? | Done and due dates on the bullet, plus one cross-tree `/notes/due` view. | In-place only (a date set three levels deep is invisible). A full task layer with recurrence and priorities. |
| Finding things | Full-text search with ancestor breadcrumbs. `#tag` is highlighted and clickable, but clicking runs a literal search — there is no tags table. | First-class parsed tags (a second schema that can drift from the text that created it). |
| Sharing | Public read-only link to a subtree, mirroring ON Paste. | Account-to-account sharing (permission checks on every read, write, move and search — the most expensive feature in the app). |
| Portability | Markdown export and import, plus the JSON `Exporter`. | Export-only. Markdown-only (would leave ON Notes out of `onsuite export`). |

## 4. Data model

One table. `done_at`, `due_on`, `archived_at` and `share_slug` arrive in later
chunks via `ALTER TABLE ... ADD COLUMN`; they are shown here so the finished
shape is visible in one place.

```sql
CREATE TABLE notes_nodes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users (id)       ON DELETE CASCADE,
    -- NULL means top level. The self-reference gives subtree deletion for
    -- free, because the platform opens SQLite with foreign_keys=ON.
    parent_id   INTEGER          REFERENCES notes_nodes (id) ON DELETE CASCADE,
    -- Contiguous within a parent: 0..n-1, no gaps. See I1 below.
    position    INTEGER NOT NULL,
    title       TEXT    NOT NULL,
    -- The secondary line under the bullet. "" when absent, never NULL.
    note        TEXT    NOT NULL,
    collapsed   INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL,

    -- N5
    done_at     TEXT,
    due_on      TEXT,           -- 'YYYY-MM-DD', a date not an instant
    -- N7
    archived_at TEXT,
    -- N9. UNIQUE permits many NULLs: SQLite treats NULLs as distinct.
    share_slug  TEXT UNIQUE
) STRICT;

CREATE INDEX notes_nodes_user_parent_pos_idx
    ON notes_nodes (user_id, parent_id, position);
```

**`user_id` is on every node, not only on roots.** Every query filters by
owner directly, so ownership is never established by walking up a tree — a
walk that a bug in the move code could break. The cost is one denormalised
column and invariant I2.

**Sibling order is a contiguous integer.** Inserting in the middle rewrites
the positions of the following siblings inside the same transaction. Fractional
ordering (LexoRank and friends) avoids that rewrite, and was rejected: with
`SetMaxOpenConns(1)` there is no write concurrency to protect, sibling counts
here are in the tens, and "positions are exactly 0..n-1" is an invariant a test
can assert after every operation. LexoRank's weaker invariant — "positions are
distinct and ordered" — stays true in the presence of bugs that contiguity
would catch.

### Invariants

Enforced by the store, asserted by tests after every operation:

- **I1** — the children of any parent have positions exactly `0..n-1`.
- **I2** — a node's parent, when it has one, has the same `user_id`.
- **I3** — no node is its own ancestor.
- **I4** — no node is deeper than `MaxDepth` (64).

I3 is the one that matters most. Reparenting a node under its own descendant
detaches a cycle from the root, and nothing in the UI can then reach it to fix
it — the data is lost while still being present. I4 exists so that a
pathological tree cannot exhaust the stack in the recursive template.

## 5. Tree operations

The store is the whole of the tree logic. Each of these is one transaction and
re-establishes I1–I4 before it commits.

| Operation | Behaviour |
|---|---|
| `Create(userID, parentID, afterPos, title)` | Insert as a child of `parentID` at `afterPos+1`, shifting later siblings. |
| `SetText(userID, id, title, note)` | Update both text fields and `updated_at`. |
| `Indent(userID, id)` | The node and its subtree become the last child of its previous sibling. No-op at `position 0`. Rejected if it would violate I4. |
| `Outdent(userID, id)` | The node and its subtree become the next sibling of its former parent. Its former following siblings stay where they are. No-op at top level. |
| `MoveUp` / `MoveDown` | Swap `position` with the adjacent sibling. |
| `Move(userID, id, newParentID, newPos)` | The general reparent. Runs the I3 ancestor check via recursive CTE, then renumbers both the old and the new parent's children. `Indent` and `Outdent` are expressed in terms of it. |
| `Delete(userID, id)` | Deletes the node; the subtree goes with it by cascade. Renumbers the remaining siblings. |
| `SetCollapsed(userID, id, bool)` | Persisted, because the outline load depends on it (§6). |

Every operation takes `userID` and includes it in its `WHERE` clause. A node
belonging to another user is not "forbidden", it is *not found* — the same
posture the platform already takes for a non-admin hitting `/admin/`.

## 6. Loading and rendering

**Zoom is the URL.** `/notes/` shows the top level; `/notes/{id}` shows that
node's children as the outline, with its ancestors as a breadcrumb. Both are
the same query — the root is the `parent_id IS NULL` case.

**The load is bounded by collapse state, not by a page size.** A recursive CTE
starts at the zoom root's children and stops descending at any node whose
`collapsed` is set, carrying `depth` (capped at `MaxDepth`) and a `has_children`
flag so a collapsed node still renders its expand arrow. The payload is
therefore always exactly what is on screen. This is why `collapsed` is
persisted rather than kept in the browser: it is load-bearing for the query,
not just a UI nicety.

There is no lazy-loading-on-expand and no arbitrary node limit. Expanding a
node is a normal request that re-renders the outline with one more level
visible.

## 7. The client/server contract

**The server is complete before any JS exists.** Chunk N2 (§18) delivers every
structural operation as a plain form that works with JavaScript disabled. N3
adds `notes.js`, which implements no behaviour of its own: it binds keys and
issues the same requests those forms issue. This is what keeps the app
testable in a project that has no Node and never will — the JS layer has no
logic of its own to leave untested.

**Structural requests carry the focused bullet's text.** Typing is local with a
debounced autosave, so the naive design has a lost-update race: type, press Tab
before the debounce fires, the server re-renders, the last keystrokes are gone.
Instead every structural POST carries `focus_id`, `title` and `note` for the
focused bullet, and the server performs the text update and the structural
operation **in one transaction**. There is only ever one write, so the race
cannot occur.

**The outline is re-rendered whole.** Every structural response returns the
full visible outline and swaps `#outline`. Computing the smallest affected
subtree per operation is a real optimisation and a real source of bugs; the
payload is already bounded by collapse state (§6), so there is nothing to win.

**Caret restoration** is the one genuinely fiddly piece. Before a structural
request `notes.js` records `{nodeID, field, offset}`; on `htmx:afterSwap` it
restores the caret. It lives in one small named function rather than being
spread across handlers, because it is the part most likely to need revisiting.

## 8. Keyboard

| Key | Action |
|---|---|
| `Enter` | New sibling below, splitting the text at the caret |
| `Shift+Enter` | Move to the note line under the current bullet |
| `Tab` / `Shift+Tab` | Indent / outdent |
| `Backspace` at the start of an empty bullet | Delete it, caret to the end of the previous bullet |
| `Up` / `Down` | Move the caret between bullets |
| `Cmd+Shift+Up` / `Down` | Move the bullet among its siblings |
| `Cmd+Enter` | Toggle done |
| `Cmd+.` | Toggle collapse |
| `Esc` | Leave editing; pressed again, zoom out one level |
| `/` (when not editing) | Focus search |
| Click the bullet dot | Zoom in |

`Cmd` is `Ctrl` off macOS. Bindings are fixed in this version; making them
configurable is out of scope (§20).

## 9. Routes

Mutations sit one path segment deeper than the zoom URL, so no pattern
overlaps another. This warrants care: `ServeMux` panics at startup on several
plausible-looking patterns, as the route-map comment in
[paste.go](../../../internal/apps/paste/paste.go) records. Literal segments
outrank `{id}`, so `/notes/search` and a node with id 42 coexist.

| Route | Chunk |
|---|---|
| `GET /notes/{$}` — outline at the root | N2 |
| `GET /notes/{id}` — outline zoomed to a node | N2 |
| `GET /notes/notes.js` | N3 |
| `POST /notes/new` | N2 |
| `POST /notes/{id}/text` | N2 |
| `POST /notes/{id}/indent`, `/outdent`, `/move`, `/delete`, `/collapse` | N2 |
| `POST /notes/{id}/done`, `POST /notes/{id}/due` | N5 |
| `GET /notes/due` | N5 |
| `GET /notes/search?q=` | N6 |
| `POST /notes/{id}/archive`, `GET /notes/archive` | N7 |
| `GET /notes/export`, `POST /notes/import` | N8 |
| `POST /notes/{id}/share`, `POST /notes/{id}/unshare` | N9 |
| `GET /notes/s/{slug}` — **the only `Public` route** | N9 |

## 10. Markdown

Inline only. Block-level Markdown is meaningless here because the tree *is*
the list structure.

Supported: `**bold**`, `*italic*`, `` `code` ``, `~~strike~~`,
`[text](url)`, bare `http(s)` URLs autolinked, and `#tag` / `@tag` rendered as
chips linking to a search for that literal string. Everything else is literal
text.

A bullet shows **rendered Markdown when it is not focused and raw source when
it is**. The rendering happens in Go and comes back in the autosave response,
which `notes.js` swaps in on blur. There is therefore exactly one Markdown
implementation, it lives in Go, and it is unit-testable.

**Link URLs are scheme-checked.** Only `http` and `https` produce an anchor;
anything else — `javascript:` above all — renders as literal text. Output is
assembled from `html/template`-escaped fragments, so the renderer cannot inject
markup even if the parser has a bug.

## 11. Done and due dates

**Done** sets `done_at`. A done bullet renders struck through, and is hidden
along with its whole subtree unless "show completed" is on. Completing a parent
does **not** complete its children — hiding them is a display decision, and
recording them as finished would destroy information that cannot be recovered
when the parent is un-completed.

"Show completed" is a preference cookie written by `notes.js` and read by the
handlers, following the pattern the theme and font switchers already use
([theme.js](../../../internal/ui/static/theme.js)) — the server only reads it,
so there is no endpoint and no CSRF surface.

**Due** sets `due_on`, a date rather than an instant, rendered as a chip on the
bullet that turns red once the date is past. `/notes/due` lists every node with
a due date across the whole tree, grouped **Overdue / Today / This week /
Later**, each row showing its ancestor breadcrumb and linking to its place in
the outline. Done and archived nodes are excluded. Comparison is against the
server's local date; this is a single-household deployment in one timezone, and
introducing per-user timezones to compare two dates is not worth it.

## 12. Search and tags

An FTS5 external-content table over `title` and `note`, kept in sync by
insert/update/delete triggers:

```sql
CREATE VIRTUAL TABLE notes_fts USING fts5(
    title, note, content='notes_nodes', content_rowid='id', tokenize='unicode61'
);
```

FTS5 is verified present in `modernc.org/sqlite v1.56.0`, the version already
in `go.mod`, so search adds no dependency.

Results join back to `notes_nodes` to filter by `user_id`, exclude archived
nodes, and honour the show-completed preference. Each hit renders as the
matching bullet plus its ancestor breadcrumb, computed by a recursive CTE
walking upward, so a result three levels deep is legible on its own.

Tags are **not stored**. `#tag` is a rendering and linking behaviour of the
Markdown renderer (§10); clicking one runs a literal full-text search. A tag
therefore cannot fall out of sync with the text that created it, and renaming
one is a search-and-edit rather than a schema operation.

## 13. Archiving

`archived_at` on the node. An archived node and its subtree disappear from the
outline, from search results and from share pages. `/notes/archive` lists
archived nodes whose parent is not itself archived — the roots of what was put
away — with a restore action.

Archiving is distinct from done: done means finished, archived means out of the
way. A node can be either, both, or neither.

## 14. Export and import

**Markdown** is the human format. One `- ` line per node, two spaces of indent
per level, the note as unbulleted indented lines beneath, `[x]` for done and a
trailing `@YYYY-MM-DD` for a due date:

```markdown
- Software projects
  Various software development projects that I'm involved in
  - AtBudget
    - Project objectives [x]
    - API @2026-09-01
```

`GET /notes/export` downloads the whole tree; `GET /notes/export?root={id}`
downloads a subtree. `POST /notes/import` parses the same format into a chosen
parent. Because done state and due dates are encoded, a document round-trips.

The parser is shared with **paste-a-multi-line-block-into-a-bullet**, which
creates a subtree from indented text — the same code path, reached from the
editor instead of from a file.

Import is bounded: a maximum upload size and a maximum node count, both
rejected with a clear error rather than being truncated silently.

**JSON** comes from implementing `app.Exporter`, which joins ON Notes to
`onsuite export` — the suite's whole-account backup — for the cost of one
method. Markdown is lossy by design; JSON is the safety net.

## 15. Sharing

`share_slug` on a node. `POST /notes/{id}/share` mints an unguessable slug;
`GET /notes/s/{slug}` renders that node and its descendants as a read-only page
requiring no login. Archived and completed nodes are excluded, there is no
breadcrumb above the shared root, and no link leads anywhere into the owner's
private tree.

Re-sharing mints a **fresh** slug, so a revoked link can never come back —
the same rule ON Paste already applies to snippets.

Account-to-account sharing is deliberately not in this spec (§20). Nothing
here forecloses it.

## 16. Layout

A centred column of roughly 46rem in the shell's content area — no second
sidebar; the suite shell already provides navigation. Each row is a bullet dot
(click to zoom), a collapse chevron that appears on hover, the title, and the
note line beneath in muted, smaller text. Vertical guide lines mark nesting
depth. The breadcrumb sits above the outline when zoomed.

Everything uses existing design tokens from
[app.css](../../../internal/ui/static/app.css). Font choice is already a
platform-level preference, so the monospace look of the Workflowy reference is
available through the existing switcher rather than being built into this app.

## 17. Testing

**Store tests** run against a real SQLite file in a temp directory, per the
convention established in [store_test.go](../../../internal/apps/paste/store_test.go) —
the bugs that matter here live in SQL, recursive CTEs and transaction
behaviour, which `:memory:` does not reproduce faithfully.

Every operation asserts I1–I4 afterwards. On top of the per-operation tests,
one test applies a **long randomised sequence** of operations to a tree and
checks the invariants still hold. Tree code fails on sequences of operations,
not on single ones: an indent that leaves a gap in `position` is invisible
until the third move after it.

**Handler tests** use [htmlassert](../../../internal/htmlassert/htmlassert.go),
covering every route including the ownership cases — another user's node must
be *not found*.

**`notes.js` has no tests, by design.** Every behaviour it triggers is a
request that a Go handler test already covers, because N2 delivers all of them
as forms before N3 binds them to keys. What remains untested is caret
restoration, which is why it is called out as the project's main risk (§19).

## 18. Build order

Each chunk is one branch, one PR, one working session, and leaves `main` green
and the app usable. Sizes are context budget, not effort.

| | Chunk | Contents | Size |
|---|---|---|---|
| **N1** | Schema + store | `notes_nodes`, every operation in §5, invariants I1–I4, the randomised-sequence test. No routes, no UI. | L |
| **N2** | Outline, no JS | The app package, routes, templates, zoom, breadcrumbs, collapse — every operation as a plain form. One line added to `registeredApps()`. Clunky but usable. | L |
| **N3** | Keyboard layer | `notes.js`, caret restoration, debounced autosave, text carried with structural operations. | M |
| **N4** | Markdown | The inline renderer and its tests; raw on focus, rendered on blur. | S |
| **N5** | Done + due | `done_at`, `due_on`, strikethrough, show-completed, the date chip, `/notes/due`. | M |
| **N6** | Search + tags | FTS5 table and triggers, `/notes/search` with breadcrumbs, clickable tags. | M |
| **N7** | Archive | `archived_at`, `/notes/archive`, restore. | S |
| **N8** | Export + import | Markdown serializer and parser, the JSON `Exporter`, paste-multiline-makes-a-subtree. | M |
| **N9** | Public sharing | `share_slug`, share and unshare, the one public route. | S |
| **N10** | Polish | `Stater` admin card, drag-to-move with a mouse, mobile layout, empty states. | M |

**N1–N3 are the irreducible core.** There is no useful stopping point between
"the store works" and "the keyboard works" — a half-built outliner is not a
usable anything. After N3 the app stands on its own and **N4–N9 are mutually
independent**: reorder them freely, or stop.

## 19. Risks

**Caret restoration across HTMX swaps (N3)** is the highest-risk work in the
app. It is the kind of code that passes every case you thought to try and
fails on the one you did not — a split at a Markdown boundary, a swap that
arrives while a second keystroke is in flight. Building N2 first is the
mitigation, not just an ordering preference: if N3 goes badly there is still a
working outliner underneath, and the retreat is to fewer swaps and more
full-page renders rather than to nothing.

**N2 builds UI that N3 largely hides.** The form buttons for indent, outdent
and move are the primary interface for exactly one chunk before becoming the
JS-off fallback and the surface every handler test drives. This is accepted
deliberately — it is what makes the server provably correct before the fiddly
layer goes on top — but it is real work that will not be looked at much
afterwards.

**Import is the only route that accepts a structured document from outside.**
It gets explicit size and node-count limits, and its parser gets adversarial
tests, because it is the one place where a malformed input becomes a tree.

## 20. Out of scope

Named here so they are decisions rather than omissions. None of them is
foreclosed by the schema above.

- Account-to-account sharing, and any form of collaborative or real-time editing
- Recurring due dates, priorities, start dates — anything that makes this a task manager rather than an outliner with dates
- Block-level Markdown, images, attachments
- Multiple documents or notebooks; backlinks and wiki-style links
- Configurable key bindings
- Touch drag-and-drop; mobile gets a readable, editable layout, not gesture parity
