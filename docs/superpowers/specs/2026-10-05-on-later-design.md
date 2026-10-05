# ON Later — design

- **Status:** approved design, 2026-10-05
- **Tracking:** milestone "ON Later", parent issue #481, phases #482–#485
  plus a new L0 sub-issue

## Why

ON Reader is built for catching up: skim a feed, open what looks good, move
on. Some writing deserves more than that — a long essay, an important
argument — and needs a calm place to read it properly, mark what matters and
write down what I think. ON Later is a private read-it-later app for that:
save a page, read it in a distraction-free view, highlight passages, comment
on them, and keep a note on the whole article.

Single user today; per-user scoping as in every other app.

## Decisions at a glance

| Topic | Decision |
|---|---|
| Ways in | URL box, bookmarklet, "Read later" button in ON Reader |
| Extraction failure | Save as link-only; user can paste the text in to make it readable |
| Images | Downloaded and kept for the life of the article (BLOBs in SQLite, like Reader/Flash) |
| Highlights | One colour, optional comment per highlight |
| Article note | Yes, one plain-text note per article |
| States | Unread → Reading → Archived; no favourites |
| Organisation | Tags (one-tag filter), FTS search, permanent delete (no trash) |
| Reading settings | Font size, column width, serif/sans — per user, server-side |
| List layout | Compact two-line rows with favicons |
| Notes UI | Slide-in Notes panel *and* a note box at the end of the article |
| Import | None |
| Export | `onsuite export` JSON + per-article Markdown download |
| Shared code with Reader | Moved into platform packages (L0) |
| Shared highlight model with ON Books | Designed as a liftable unit inside Later; moved to the platform when Books needs it |

## Architecture

### L0: shared platform packages

Later needs the same outbound-fetch, extraction, sanitising and favicon code
Reader has. Apps never import each other, so that code moves out of
`internal/apps/reader` into the platform:

- **`internal/platform/webfetch`** — the guarded HTTP client: `DenyPrivateAddr`
  checked against the resolved IP at dial time (DNS rebinding stays blocked),
  redirect and timeout limits, post-decompression body caps, and the
  `DenyAddr` test hook. The User-Agent product token becomes a constructor
  argument so each app identifies itself.
- **`internal/platform/article`** — `ExtractArticle` (go-readability), the
  bluemonday policies, image absolutising and rewriting. Rewriting takes a
  function from source URL to served URL, so Reader keeps its proxy paths and
  Later gets its own. `TestReadabilityIsContained` now
  pins go-readability to this package.
- **`internal/platform/favicon`** — favicon discovery only (page
  `<link rel>` and `/favicon.ico`). Storage stays in each app. Reader's
  homepage lookup and the #451/#455 repair rule stay in Reader — they are tied
  to its feed table, and ON Later always has the saved page's HTML, so
  `favicon.Discover` on that page is enough. The give-up rule lives in
  `webfetch.GivenUp`, shared with images.

**This is a deliberate exception to PATTERNS.md's "cross-app mirroring".**
That pattern is right for small domain logic like slug generation. This code
is large and security-critical: two drifting copies of SSRF guards and an
HTML sanitiser policy are worse than one shared package. The platform
packages import nothing from `internal/apps`, so the arch rules still hold.

L0 is a pure refactor: no behaviour change in Reader, and Reader's existing
tests pass unchanged (only import paths move).

### The app

`internal/apps/later`, laid out like the others: its own migrations
(`later:0001`…), templates, `static/later.js`, a `Store` that owns the clock,
plus `Exporter` and `Stater`. Registered in `registeredApps()`, with the
tests that pin the app list updated per
[adding-an-app.md](../../developers/adding-an-app.md).

### The highlight unit

Highlight storage, offset validation, `<mark>` rendering and the selection
JavaScript live in their own files (`highlight.go`, `highlight_render.go`,
`static/highlight.js`) with no dependency on articles, lists or tags beyond
"a document ID and its plain text". When ON Books arrives, this unit moves to
the platform (a Go package plus a shared script in `internal/ui/static`) and
both apps use it, each with its own table.

### Reader → Later

ON Reader's article view gets a "Read later" button: an HTMX POST of the
article URL to `/later/save`. A form posting to another app's route is not
an import, so the arch rules hold. The response swaps the button for
"Saved to Later ✓".

## Data model

All tables are prefixed `later_` and scoped by `user_id`. Timestamps use
`db.FormatTime` (30-character UTC).

- **`later_articles`**
  - `id`, `user_id`, `url` (normalised), unique `(user_id, url)`
  - `title`, `site_name`, `byline`, `site_host`
  - `content_html` — sanitised snapshot, image `src`s rewritten to
    `/later/img/{hash}`; empty for link-only
  - `content_text` — the plain text the highlight offsets index into, derived
    from `content_html` once at save time
  - `content` — `extracted` | `pasted` | `link_only`
  - `extract_error` — why a link-only item failed, shown in the reader
  - `word_count`
  - `state` — `unread` | `reading` | `archived`
  - `note` — plain text, may be empty
  - `progress` — 0–1 scroll position
  - `saved_at`, `opened_at`, `archived_at`, `updated_at`
- **`later_images`** — content-addressed: `hash` (PK), `source_url`,
  `content_type`, `bytes` BLOB, `fetched_at`, `error_count`, `last_error`,
  `next_attempt_at`.
- **`later_article_images`** — `(article_id, hash)`. Two articles can share
  an image; deleting an article removes only images nothing else links to
  (the lesson of Reader's migration 0005). Images never expire while linked.
- **`later_favicons`** — per `site_host`: bytes, content type, fetch state,
  following the platform favicon package's give-up rules.
- **`later_highlights`** — `id`, `article_id`, `start`, `end` (code-point
  offsets into `content_text`), `quote`, `comment`, `created_at`,
  `updated_at`. No overlapping highlights within an article.
- **`later_tags`** (`id`, `user_id`, `name` unique per user, lowercase) and
  **`later_article_tags`** (`article_id`, `tag_id`). Unused tags are removed
  when their last article loses them.
- **`later_search`** — FTS5 over title, `content_text`, highlight quotes and
  comments, and the note; kept in step in the same transactions that change
  those fields.
- **`later_prefs`** — per user: `font` (`serif`|`sans`), `size` (1–5),
  `width` (`narrow`|`medium`|`wide`).

The snapshot is immutable once an article is readable, so highlight offsets
never go stale. The one change allowed — pasting text into a link-only
article — happens before any highlight can exist. The stored `quote` is a
belt-and-braces check: if it doesn't match `content_text[start:end]` at
render time, the highlight is still listed in the Notes panel but not drawn
inline.

### Offsets are code points

JavaScript strings count UTF-16 units; Go counts bytes. Offsets are defined
as **Unicode code points** into `content_text`, and both sides count them
that way (`Array.from`/iteration in JS, `[]rune` in Go). `content_text` is
produced on the server by one function, and the client computes offsets over
the text nodes of the rendered article body, which that function mirrors
(block boundaries become `\n`). Tests cover emoji, Cyrillic, Hebrew and
combining marks.

## Saving

Three entry points, one handler: `POST /later/save` with `url` and optional
`tags`.

1. **URL box** at the top of the list.
2. **Bookmarklet** — opens a small popup at `GET /later/save?url=…`. The GET
   only renders a confirmation form (title if known, tag field, focused
   **Save** button) — it never saves, so a link elsewhere can't save on the
   user's behalf. After saving, the popup shows "Saved ✓" and closes itself
   (static script, CSP-clean). The list page shows the bookmarklet with
   drag-to-bookmarks instructions.
3. **ON Reader's "Read later" button** (above).

Save steps:

1. **Normalise the URL**: drop the `#fragment` and known tracking parameters
   (`utm_*`, `fbclid`, `gclid`, `mc_cid`, `mc_eid`, `ref_src`). If the user
   already saved it, go to the existing article (HTMX: say so).
2. **Fetch and extract** synchronously through `webfetch` + `article`, with a
   15-second limit — the same shape as Reader's fetch-on-add, so the user
   knows the outcome immediately.
3. **Success** → store the snapshot, `content_text`, word count; link images;
   queue image downloads in the background (bounded concurrency, detached
   context); queue the site's favicon.
4. **Extraction fails or the fetch fails** → save as `link_only` with the
   page `<title>` if one was seen (else the URL) and `extract_error`.

**Images.** Background download after save; a periodic job retries failures
with backoff up to a fixed attempt cap. If the article is opened before an
image is stored, `/later/img/{hash}` fetches it on demand and stores it.
Image route responses are `private` with a long max-age (content-addressed).

**Paste text.** A link-only article shows a textarea. Input is plain text;
blank lines split paragraphs; each paragraph is HTML-escaped into `<p>`.
Nothing to sanitise. The article becomes `content = pasted` and is readable
and highlightable.

## Reading view

Full-screen, without the suite chrome: a slim top bar (← Later, title, time
left, **Aa**, **Notes · N**, **Archive**, ⋯), a thin progress line, the
article column, and comments in the right margin on wide screens. On narrow
screens comments collapse to a 💬 marker that opens below the paragraph.

- **Server-rendered.** The handler inserts `<mark>` elements into the
  snapshot at stored offsets by walking the HTML token stream, so an article
  with highlights renders with no JavaScript and is testable from Go.
- **Creating a highlight** (`highlight.js`): on selection inside the article
  body, compute start/end code-point offsets, show a popover (**Highlight** /
  **Highlight + comment**), POST offsets, quote and comment via HTMX. The
  server checks the quote against `content_text`, refuses overlaps and
  out-of-bounds ranges, saves, and returns the re-rendered article body plus
  an out-of-band Notes panel update. Clicking a highlight edits its comment
  or deletes it.
- **Position.** `later.js` posts progress, debounced every few seconds and on
  `pagehide`; on open the page scrolls back to it.
- **State.** The first open moves Unread → Reading. **Archive** moves to
  Archived; archived articles can be moved back to Unread.
- **Aa settings.** Font (serif/sans), size (5 steps), width
  (narrow/medium/wide), saved to `later_prefs` and applied as CSS classes —
  no inline `style=` under the CSP.
- **Notes.** The slide-in Notes panel holds the article note and every
  highlight with its comment (click to scroll to it). The same note also
  appears as a box after the last paragraph. Each saves on blur via HTMX and
  the response refreshes the other copy out-of-band.
- **Link-only.** The view shows "Couldn't extract this page" with the reason,
  **Open original**, and the paste-text box.
- **⋯ menu.** Edit tags, Download as Markdown, Open original, Delete.

## The list

`/later/` — a page within the normal suite shell.

- **Save box** (URL + optional tags) and the bookmarklet link.
- **Tabs with counts:** Unread (newest saved first), Reading (most recently
  opened first), Archived (most recently archived first).
- **Tag chips** filter by one tag, combined with the current tab.
- **Rows** (compact, two lines, like ON Paste): favicon, title; site,
  reading time, tag pills, highlight count, a small progress bar.
  Link-only rows carry an orange "link only — add text" pill.
- **Row ⋯ menu:** Archive/Unarchive, Edit tags, Delete.
- **Paging:** 50 rows, then "Load more" (HTMX append).
- **Search:** FTS5 across all states; results use the same rows plus a
  snippet saying where it matched ("in a highlight: …").
- **Delete:** confirmation dialog ("Delete permanently? Highlights and notes
  go too."), then one transaction removes the article, highlights, tag links,
  image links, orphaned images and search rows. No trash, no undo.

## Export

- **`onsuite export`** — `Exporter` returns articles (URL, title, state,
  timestamps, `content_html`, note), highlights with comments, and tags.
  Image bytes are not included; source URLs are.
- **Download as Markdown** — `later-article.md` (generic name, as elsewhere
  in the suite): title, source URL, the note, highlights with comments, then
  the article text. A small hand-written converter handles exactly the
  sanitiser's allowlist (headings, paragraphs, lists, blockquotes, links,
  emphasis, code, images as links to their source). No new dependency.

## Admin card

`Stater`: articles by state, number of highlights, total stored image bytes.

## Errors

Follow PATTERNS.md's error-surfacing patterns.

- Save failures (bad URL, blocked address) show inline in the save form. A
  fetch or extraction failure is not an error to the user — the item is
  saved as link-only and says why.
- Highlight rejections (overlap, quote mismatch, out of range) show a short
  notice by the popover.
- Image and favicon failures are logged; the page renders without them.
- Missing or foreign article IDs are 404, the same as everywhere else.

## Testing

- Store tests against a real SQLite file in a temp dir; handler tests with
  `apptest.Clock`.
- Fetch/extract/image tests use `httptest` servers with the `DenyAddr` hook,
  as Reader's tests do; one test exercises the real guard.
- Table-driven tests for URL normalisation, `content_text` derivation,
  `<mark>` insertion (nesting, element boundaries, multi-byte text), offset
  validation and the Markdown converter.
- Delete test proves shared images survive and orphans go.
- Arch test: Later does not import Reader; go-readability contained to
  `internal/platform/article`.
- L0 is correct when Reader's existing tests pass without edits beyond
  import paths.

## Docs

- `docs/user/later.md` (served at `/help`) with screenshots, the README app
  list and hero, and the app list in AGENTS.md.

## Phases

One PR each, in order.

| Phase | Issue | Scope |
|---|---|---|
| L0 | new sub-issue of #481 | Move fetch, extraction/sanitising and favicon code to `internal/platform/{webfetch,article,favicon}`; Reader uses them; no behaviour change |
| L1 | #482 Save and read | App skeleton; save (URL box, bookmarklet, Reader button); images and favicons; link-only + paste text; list with tabs and rows; reading view with Aa settings, progress, archive; delete |
| L2 | #483 Highlights and comments | Highlight unit; comments; article note; Notes panel and end-of-article box |
| L3 | #484 Organise and search | Tags; FTS search |
| L4 | #485 Export | JSON export, Markdown download, admin card, user guide and screenshots |

If L1's plan gets too large, the reading view splits into its own PR.

## Out of scope

Import (Pocket/Instapaper), highlight colours, favourites, a trash, PDF/EPUB,
browser-captured page HTML (a possible later answer to paywalls), the phone
share sheet and PWA (Integrations milestone).
