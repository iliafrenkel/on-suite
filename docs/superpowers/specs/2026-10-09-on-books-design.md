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
| Layout | Three-pane split view (sidebar, list, book), like ON Reader |
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
- **Pages read in a period** — within a reading, each progress row counts
  only the pages above the highest page that reading had reached so far
  (its high-water mark, starting at 0): `max(0, page − mark)`, dated by
  `recorded_at` in the user's local day, and the mark then rises to the
  page. Going backwards counts nothing, and neither does climbing back up
  to the old mark: 100 → 250 (a typo) → 150 → 180 counts 250, not 280.
  Finishing a reading with a page count adds the pages from the mark to the
  last page on the finish date (none if it is already there).
  Percent-based readings convert through the page count when there is one,
  and count no pages otherwise. Each re-read is a new reading with its own
  mark from 0.
- **Books finished in a year** — readings with status `finished` and
  `finished_on` in that year. A re-read counts again; DNF never counts.
  Imported readings with no date don't count toward any year.

## Screens

### Layout

A three-pane list+detail view, like ON Reader (sidebar, list, book), with its resizable gutters; one pane at a time on phones.

- **Sidebar** — **Add book** button; shelves with counts (Reading · Want to
  read · Read · DNF · All); tags; Stats. *Reading* is the default shelf.
- **List pane** — a filter box (title/author in B1, full-text from B3), then
  compact two-line rows: small cover (or mini spine), title, `author ·
  series #n`, and — when the filter matched inside a review, note or
  quote — a third line with the match ("Quote: …the spice must flow…").
  The right side depends on the shelf:
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
    (Markdown, edited in a "Write a review" / "Edit review" disclosure).
  - Notes — dated entries, newest first, each with its page if one was
    given ("p. 112"); a "+ Add note" disclosure, and Edit and Delete on
    each.
  - Quotes — cards with text, page and comment, newest first; a "+ Add
    quote" disclosure, and Edit and Delete on each.
  - Reading history — every reading with dates, format and status; each
    one's dates and format editable, and each deletable.
  - A ⋯ menu: Edit details (which also changes the cover), Find cover (B5), Delete.

### Add book

The Add book page (`/books/new`) starts with **Find it on Open Library**: a
plain search box (title, author or ISBN). Results come back on the page as
rows — cover thumbnail, title, author, year, **Use this**. **Use this**
reloads the page with the book form pre-filled from the result (and the
work's description); the person checks it and picks a shelf: *Want to
read* / *Reading now* / *Already read*. Saving creates the book (and a
reading if needed) and fetches its cover. The manual form is always right
there under the search; when Open Library fails or finds nothing, the page
says so and starts the form with what was typed.

Thumbnails in search results are proxied through `/books/olcover/{coverID}`:
Open Library's cover URLs redirect to archive.org storage hosts, which a
CSP would have to allow by wildcard, so the suite's `img-src 'self'` stays
as it is. Only digits reach the proxy, never a URL.

(Decided 2026-10-09 while planning B1b: search on the page rather than in a
dialog, and proxied thumbnails rather than a wider CSP.)

### Progress and finishing

- The progress input saves on Enter via htmx and updates the bar in the
  book pane and the list row (out-of-band swap).
- Reaching the last page does not finish the book. **Finish** opens a small
  dialog: finish date (default today, editable) and an optional rating.
  **Did not finish** is the same with an optional page instead of a rating.
- **Start reading** creates a reading dated today with the format of the
  book's previous reading, if any.
- The format pill in the reading box changes the current reading's format.
  Finishing without a rating keeps the book's rating, so a re-read needn't
  rate it again; the Did not finish page is kept as the reading's last
  progress. Percentages are whole numbers.

(Decided 2026-10-09 while planning B2: B2 is one PR. The book pane shows
only the current progress — the input, the bar and when it was last
updated; every update is still kept in `books_progress` for B4, but there
is no progress history list. A reading's dates and format can be edited,
its status can't: deleting a reading is how a wrong status is put right.
The review is edited in a `<details>` disclosure in the book pane, which
works without JavaScript, and rendered by an inline Markdown renderer that
mirrors ON Notes' (apps don't share code; goldmark stays contained to the
help pages), with blank-line paragraphs on top. The series link opens the
list filtered to the series (`?series=`), in series order.)

### Notes, quotes and search

- A note is Markdown, drawn as the review is. A quote's text is plain
  text with its line breaks kept; its comment is Markdown.
- A page is optional: a whole number from 1 to the book's page count, or
  any positive number when the book has none. A bad page, or a note or
  quote with no text, is refused inline, inside its form, with what was
  typed; a note is capped at 20,000 characters, a quote's text and its
  comment at 5,000 each.
- The filter box searches `books_search`: every word must match, each as
  a prefix, so the list narrows while a word is still being typed. It
  searches within the shelf, tag or series on screen, and keeps that
  list's order.

(Decided 2026-10-09 while planning B3: B3 is one PR. Notes and quotes
are edited and deleted inline — an Edit `<details>` disclosure and a
Delete button through the confirm dialog, like the reading history — and
everything works without JavaScript. With htmx a note or quote form swaps
only its own section, and the list out of band. The book pane stacks
Notes, then Quotes, then Reading history. A filtered row that matched
inside a review, note or quote gets a third line — a label and the
highlighted words, as in ON Later's search; a match on the title,
subtitle, authors or series alone shows none.)

### Keyboard

As in Later and Reader: `j`/`k` move through the list, `/` focuses the
filter, `a` opens Add book, `p` focuses the progress input, `n` opens the
add-note box and `q` the add-quote box, `Esc` closes dialogs.

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

(Decided 2026-10-09 while planning B4: B4 is one PR. Stats is a page of
its own, `/books/stats`, as `/reader/stats` is, with a link back to the
books and a Stats link at the bottom of the sidebar; it works at phone
width. The year picker is a row of year links, from the first year with
any reading to next year, so next year's goal can be set ahead. The goal
is set, changed and removed only on the Stats page, with plain forms that
work without JavaScript. The card on the Reading shelf — "12 of 30 · 2
ahead" — is read-only, shows only when this year has a goal, and links to
Stats. Ahead or behind is for this year; a past year says "4 short" or
"goal reached", a future year shows no pace. The all-time row counts
every finished reading, dated or not; an undated one counts toward no
year and adds no pages. Format split and longest and shortest are per
year. Pages read are counted by a high-water mark (see "Derived
values"), chosen so a corrected typo isn't counted twice; a repeated
progress value adds nothing, so #576 doesn't touch them. The plan's
defaults — goal of 1–1000 books, the year range, the pace wording,
all-time counting, average rating over distinct books, format order,
ties, empty states and chart slots — were confirmed the same day.)

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

- OL search failures (timeout, 5xx, bad JSON) show inline on the Add book page and
  pre-fill the form with what was typed.
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
| B1a | #487 Library and shelves (part 1) | App skeleton; books, readings and tags tables; manual entry on an Add book page; generated spines; the three panes; Start reading / Finish / DNF; tags; edit and delete; title/author filter |
| B1b | #487 Library and shelves (part 2) | Open Library search on the Add book page; covers (`books_covers`): fetched on save from a pick, upload, image address, remove; covers in the list and book pane; thumbnail proxy |
| B2 | #488 Reading progress and history | Progress input and history; format; re-reads and the reading history list; rating and review; series links |
| B3 | #489 Notes and quotes | Dated notes; quotes; FTS search |
| B4 | #490 Goals and stats | Yearly goal; stats page; goal card on the Reading shelf |
| B5 | #491 Import and export | Goodreads import; cover backfill job and Find cover; JSON and Markdown export; admin card; user guide and screenshots |

B1 was split into B1a and B1b while planning (2026-10-09).

## Out of scope

Social features and recommendations; syncing metadata with OL after saving;
editions, multiple copies, ownership and lending; a pages goal; per-reading
ratings; importers other than Goodreads; a cross-app quotes view (#497);
barcode scanning (#497's mobile work).
