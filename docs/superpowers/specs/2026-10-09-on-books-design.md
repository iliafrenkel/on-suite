# ON Books — design

- **Status:** approved design, 2026-10-09
- **Tracking:** milestone "ON Books", parent issue #486, phases #487–#491

## Why

I read a lot and I'd like to read even more. Goodreads does the tracking
well, but I don't need the social side, and I'd rather my reading history
lived in my own suite. ON Books keeps track of what I'm reading, what I've
read, what I want to read, and what I thought of it.

It is deliberately light: a reading log with a little metadata, not a
library catalogue.

Single user today; per-user scoping as in every other app.

## Decisions at a glance

| Topic | Decision |
|---|---|
| Adding books | Open Library search (title, author, ISBN); manual entry always available and the fallback when OL fails |
| Metadata | Copied from OL once at save time, never synced: title, subtitle, authors, year, pages, ISBN, OL IDs, description |
| Covers | Fetched once, stored as BLOBs in SQLite (like Flash/Later/Reader); upload or image URL as alternatives; generated spine when missing |
| Shelves | Want to read · Reading · Read · DNF — derived from readings, not stored; plus tags |
| Re-reads | A book has many readings; each has its own dates, format, status and progress |
| Format | Per reading, optional: paper / ebook / audio |
| Series | Two optional fields on the book (name, number); books in a series link to each other |
| Progress | Page, or % for audio; every update kept as history |
| Rating and review | One per book (matches Goodreads) |
| Notes | Dated entries with an optional page |
| Quotes | Separate list: text, optional page, optional comment. Not shared with Later's highlights |
| Goal | Yearly target, number of books only |
| Stats | Per-year page plus an all-time row; server-rendered SVG |
| Layout | List+detail split view, like Later |
| Import | Goodreads CSV; covers filled in by a background job; "Find cover" button |
| Export | `onsuite export` JSON + `books-export.md` |

## Architecture

A new app, `internal/apps/books`, ID `books`, name "ON Books", tables
`books_*`, mounted at `/books/`. Same shape as ON Later: Go templates with
htmx, a small `static/books.js` for keyboard navigation and dialogs, a
`Store` that owns the clock, plus `Exporter` and `Stater`. Registered in
`registeredApps()`, with the tests that pin the app list updated per
[adding-an-app.md](../../developers/adding-an-app.md).

- **Open Library client** (`openlibrary.go`) — search JSON → candidates, and
  cover URL → bytes. All requests go through `internal/platform/webfetch`
  (SSRF guards, ~5s timeout, body caps), with an `ON-Books` User-Agent token.
  The base URL is a field so tests point it at `httptest` servers.
- **Generated spine** — books without a cover render a plain spine: title
  (and author) on a `swatch-c-*` background, the colour picked by a stable
  hash of the title. Pure template/CSS, no stored image.
- **Cover backfill** — a job on `internal/platform/jobs`, so it shows in
  /admin/jobs with Run now (B5).

### Quotes are not Later's highlights

The Later spec planned to lift its highlight unit to the platform for Books.
It doesn't fit: Later's highlights are code-point offsets into a stored
article text, and Books has no text — a quote is typed in from a paper book.
Books keeps its own simple quotes table. If #497 later wants a cross-app
"all highlights" view, it can read both tables then. Later's highlight unit
stays where it is.

## Data model

All tables are prefixed `books_`. `books_books`, `books_tags` and
`books_goals` carry `user_id`; child tables are scoped through their book.
Timestamps use `db.FormatTime` (30-character UTC). Calendar dates
(`started_on`, `finished_on`) are `YYYY-MM-DD` in the user's local day, as
the suite has done since v2.1.0.

- **`books_books`**
  - `id`, `user_id`
  - `title`, `subtitle`, `authors` — one display string ("James S. A.
    Corey"); no authors table, author filtering is a text match
  - `year`, `pages` (nullable)
  - `isbn13` (nullable; ISBN-10 converted on input), `ol_work_id`,
    `ol_edition_id`
  - `description` — plain text from OL, may be empty
  - `series_name`, `series_number` (both nullable; number is text, so "2.5"
    works)
  - `rating` — 1–5, null for none
  - `review` — Markdown, may be empty
  - `cover_checked_at` (nullable, B5) — when the backfill job last looked
    for a cover and found none
  - `added_at`, `updated_at`
- **`books_covers`** — `book_id` (PK), `content_type`, `bytes` BLOB,
  `source` (`ol` | `upload` | `url`), `fetched_at`. One cover per book;
  deleted with the book. Served from `/books/cover/{id}` with long cache
  headers and an `updated_at`-based query string.
- **`books_readings`** — `id`, `book_id`, `status` (`reading` | `finished` |
  `dnf`), `format` (null | `paper` | `ebook` | `audio`), `started_on`,
  `finished_on` (both nullable — imported re-reads have no dates),
  `created_at`.
- **`books_progress`** — `id`, `reading_id`, `page` or `percent` (exactly
  one set), `recorded_at`. The history; the latest row is the current
  progress. A reading's unit follows its format: `audio` uses percent,
  everything else uses pages (percent when the book has no page count).
- **`books_tags`** (`id`, `user_id`, `name` unique per user, lowercase) and
  **`books_book_tags`** (`book_id`, `tag_id`). Unused tags are removed when
  their last book loses them — the same as Later.
- **`books_notes`** — `id`, `book_id`, `page` (nullable), `body` (Markdown),
  `created_at`, `updated_at`.
- **`books_quotes`** — `id`, `book_id`, `page` (nullable), `text`,
  `comment`, `created_at`, `updated_at`.
- **`books_goals`** — `user_id`, `year`, `target`; PK `(user_id, year)`.
- **`books_search`** (B3) — FTS5 over title, subtitle, authors, series name,
  review, note bodies, quote text and comments; kept in step in the same
  transactions that change those fields.

### Derived values

- **Shelf** — no readings → *Want to read*; otherwise the latest reading
  (by `created_at`) decides: `reading` → *Reading*, `finished` → *Read*,
  `dnf` → *DNF*. A book can only have one reading in `reading` status.
- **Pages read in a period** — the sum of positive deltas between consecutive
  progress rows of a reading, dated by `recorded_at` in the user's local day.
  Finishing a reading with a page count adds the remainder (last page minus
  last recorded page) on the finish date. Going backwards counts nothing.
  Percent-based readings convert through the page count when there is one,
  and count no pages otherwise.
- **Books finished in a year** — readings with status `finished` and
  `finished_on` in that year. A re-read counts again; DNF never counts.
  Imported readings with no date don't count toward any year.

## Screens

### Layout

A list+detail split view, like ON Later.

- **Sidebar** — **Add book** button; shelves with counts (Reading · Want to
  read · Read · DNF · All); tags; Stats. *Reading* is the default shelf.
- **List pane** — a filter box (title/author in B1, full-text from B3), then
  compact two-line rows: small cover (or mini spine), title, `author ·
  series #n`. The right side depends on the shelf:
  - Reading: progress bar and %, sorted by latest progress
  - Read: stars and finish date, sorted by finish date, newest first
  - Want to read: date added, newest first
  - DNF / All: date of the latest change
- **Book pane**
  - Header: cover, title/subtitle, authors, series link ("The Expanse #3 ·
    9 books" — clicking filters the list to the series), `year · pages ·
    ISBN`, tags, the OL description collapsed to a few lines.
  - **Current reading box** — progress input and bar, format pill, started
    date, **Finish** and **Did not finish** buttons. On a book with no
    active reading it shows **Start reading** instead (or **Read again**
    once it has been read).
  - Rating stars (click to set, click again to clear) and the review
    (Markdown, edit in place).
  - Notes — dated entries, newest first, with an add box.
  - Quotes — cards with text, page and comment.
  - Reading history — every reading with dates, format and status; each
    editable and deletable.
  - A ⋯ menu: Edit details, Change cover, Find cover (B5), Delete.

### Add book

A dialog with one search box. Results come back as rows: cover thumbnail,
title, author, year, **Add**. **Add** opens the book form pre-filled from the
result, with a shelf choice: *Want to read* / *Reading now* / *Already read*
(the last asks for a finish date). Saving creates the book (and a reading if
needed) and fetches the cover. A "Can't find it? Enter manually" link is
always visible; when OL fails, the dialog says so and offers the manual form
with what was typed.

Thumbnails in search results load directly from `covers.openlibrary.org` in
the browser; the CSP gets that one image origin. Only the saved cover is
fetched by the server.

### Progress and finishing

- The progress input saves on Enter via htmx and updates the bar in the
  book pane and the list row (out-of-band swap).
- Reaching the last page does not finish the book. **Finish** opens a small
  dialog: finish date (default today, editable) and an optional rating.
  **Did not finish** is the same with an optional page instead of a rating.
- **Start reading** creates a reading dated today with the format of the
  book's previous reading, if any.

### Keyboard

As in Later and Reader: `j`/`k` move through the list, `/` focuses the
filter, `a` opens Add book, `p` focuses the progress input, `Esc` closes
dialogs.

### Stats (B4)

- A year picker, defaulting to the current year.
- **Goal card** — "12 of 30", plus ahead/behind where expected = target ×
  day-of-year ÷ days-in-year (rounded down). The goal is set and edited
  inline. The same card sits at the top of the *Reading* shelf when a goal
  exists for the current year.
- Numbers: books finished, pages read, average rating (of books finished
  that year), format split, longest and shortest book.
- Charts, server-rendered SVG as in ON Focus: books by month for the year,
  and books per year all-time.

## Import (B5)

A Books import page takes a Goodreads "Export Library" CSV.

- The file is parsed fully before anything is written; the whole import is
  one transaction, so a bad file leaves nothing behind. Parse errors name
  the line.
- Mapping:
  - Title, Author (+ Additional Authors), ISBN13 (falling back to ISBN),
    Number of Pages, Original Publication Year (falling back to Year
    Published)
  - My Rating → rating (0 → none); My Review → review, with `<br/>` turned
    into line breaks and other tags stripped
  - Exclusive Shelf: `read` → a finished reading dated Date Read;
    `currently-reading` → a reading started on Date Added; `to-read` → no
    reading. Read Count > 1 adds earlier finished readings with no dates.
  - Other Bookshelves → tags
  - Binding → format where obvious (Kindle Edition → ebook, Audible
    Audio/Audio CD/Audiobook → audio, Paperback/Hardcover → paper);
    otherwise none
  - Date Added → `added_at`
  - ISBNs lose Goodreads' `="…"` wrapping.
- Duplicates are skipped: same ISBN13, or same title and authors
  (case-insensitive) when there is no ISBN.
- A summary page lists imported and skipped counts, with the skipped titles.

### Covers after import

- **Backfill job** — picks up books with an ISBN, no cover and no
  `cover_checked_at`, a few per run, with a pause between requests; tries
  the OL cover by ISBN. A miss sets `cover_checked_at` so the job doesn't
  retry it forever.
- **Find cover** — on the book page, runs the same lookup immediately,
  searching OL by title and author when there is no ISBN.

## Export (B5)

- **`onsuite export`** — `Exporter` returns books with readings, progress,
  rating, review, notes, quotes, tags and goals. Cover bytes are not
  included.
- **`books-export.md`** — a Markdown download (generic name, as elsewhere in
  the suite): per book, the details, readings, rating and review, notes and
  quotes.

## Admin card

`Stater`: books per shelf, readings, notes, quotes, total stored cover bytes.

## Errors

Follow PATTERNS.md's error-surfacing patterns.

- OL search failures (timeout, 5xx, bad JSON) show inline in the dialog and
  offer manual entry.
- Cover fetch failures are logged; the book shows its spine.
- Bad progress (negative, past the page count, percent over 100) and bad
  dates (finish before start) show an inline message.
- Uploaded covers are checked for an image content type and size cap.
- Missing or foreign IDs are 404, the same as everywhere else.

## Testing

- Store tests against a real SQLite file in a temp dir; handler tests with
  `apptest.Clock`.
- OL client with `httptest` servers: good JSON, malformed JSON, timeout,
  5xx, non-image cover. One test exercises the real `webfetch` guard.
- Table-driven tests for shelf derivation, the pages-read maths (going
  backwards, finishing, percent with and without page count, re-reads),
  goal ahead/behind, ISBN-10 → 13 conversion and the Goodreads row mapping.
  The CSV fixture includes awkward rows: quoted commas and newlines,
  `="…"` ISBNs, Read Count 2, Kindle binding, rating 0.
- Arch test: Books imports no other app.

## Docs

`docs/user/books.md` (served at `/help`) with screenshots, the README app
list and hero, and the app list in AGENTS.md.

## Phases

One PR each, in order.

| Phase | Issue | Scope |
|---|---|---|
| B1 | #487 Library and shelves | App skeleton; books, covers, readings, tags tables; OL search and manual entry; cover fetch/upload/URL and spine; shelves and the split view; Start reading / Finish / DNF; tags; edit and delete; title/author filter |
| B2 | #488 Reading progress and history | Progress input and history; format; re-reads and the reading history list; rating and review; series links |
| B3 | #489 Notes and quotes | Dated notes; quotes; FTS search |
| B4 | #490 Goals and stats | Yearly goal; stats page; goal card on the Reading shelf |
| B5 | #491 Import and export | Goodreads import; cover backfill job and Find cover; JSON and Markdown export; admin card; user guide and screenshots |

If B1's plan gets too large, OL search and covers split into their own PR.

## Out of scope

Social features and recommendations; syncing metadata with OL after saving;
editions, multiple copies, ownership and lending; a pages goal; per-reading
ratings; importers other than Goodreads; a cross-app quotes view (#497);
barcode scanning (#497's mobile work).
