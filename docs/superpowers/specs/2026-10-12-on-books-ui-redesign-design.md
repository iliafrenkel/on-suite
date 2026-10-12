# ON Books UI redesign: design

Date: 2026-10-12. Mockups: [2026-10-12-on-books-ui-redesign-mockups.html](2026-10-12-on-books-ui-redesign-mockups.html).
This changes the Screens part of the [ON Books design](2026-10-09-on-books-design.md).
The data model, import/export, stats and goals stay as they are.

## Why

After a few days of real use, the three-pane UI feels overloaded:

- **Hidden actions.** Edit and Delete are hidden in a ⋯ menu you only find by clicking it.
- **No single place to edit.** You change tags and progress on the book itself, but everything else on a separate Edit page.
- **Mismatched controls.** Buttons of different sizes and styles are scattered and misaligned.
- **A busy book pane.** The progress box takes too much room, and the format control looks like a tag.

The goal is an interface that is **elegant and stays out of the way**.

## Decisions at a glance

- **Two panes, not three.** The sidebar (shelves and tags) stays. The right pane is a **fluid grid or a flat list** of books.
- **Book details is its own full-width page**, with no sidebar and a page bar holding a back link.
- **One editing rule.** *Facts about the book* (title, authors, series, cover, tags, description) are changed on the **Edit page**. *Your reading of it* (progress, format, finish/DNF, rating, review, notes, quotes, reading history) is changed **in place on the details page**.
- **One look for every action on the details page.** Each section has a small heading with at most one icon action on its right (**+** or ✎). Entries show ✎ / 🗑 icons on hover, always on touch screens.
- **View and sort are per-account preferences.** The sort options are *Default*, *Date added*, *Title* and *Author*, plus a direction toggle. *Default* is each shelf's natural order, the same as today.
- **Searching always shows the list.** While the search box has text, results show as the list (with match snippets), whatever the saved view. Clearing the search brings the saved view back.
- **On a phone the sidebar becomes a drawer.** The details page is the same single column on every screen size.
- **Add/Edit get a page bar and small fixes only.** Edit gains the Tags field and Find cover; Add is unchanged apart from the page bar.

## Home page

URL `/books/` with the existing query (`shelf`, `tag`, `series`, `q`). *Reading* stays the default shelf.

### Sidebar

From top to bottom:

- **Shelves with counts:** Reading · Want to read · Read · Did not finish · All books.
- **The goal card**, when a goal is set. It's a compact card: "2026 goal · 18 of 30", a thin bar, and the pace line. It links to Stats.
- **Tags.**
- **At the foot:** Stats, Import from Goodreads and Export as Markdown, as quiet links.

**Add book moves to the toolbar.** The sidebar keeps its resize gutter. The list/book gutter is removed.

### Toolbar

The toolbar sits above the grid or list and contains, in order:

1. **The heading:** the shelf, tag or series name, with its count in faint text.
2. **The search box**, which grows to fill the space. It searches books, notes and quotes, as today.
3. **The sort dropdown**, followed by the **direction toggle** (↓/↑).
4. **The view switch:** two icon buttons (grid ▦, list ☰) in a segmented control, with the current one highlighted.
5. **+ Add book**, the only primary button.

On narrow screens the toolbar wraps: the search gets its own row, and the drawer button (see *Phone*) comes first.

### Book tile (grid)

- **Cover** at 2:3 with a soft shadow, or the generated spine when there's no cover. A reading in progress also gets a thin progress bar along the cover's bottom edge.
- **Title**, clamped to two lines.
- **Author(s).**
- **A status line:**

  | Shelf | Shows |
  |---|---|
  | Reading | "Reading · 45%", or "Reading · p. 212 / 482" when progress is kept in pages (accent colour) |
  | Want to read | "Want to read" (dim) |
  | Read | the stars when rated, otherwise "Read" |
  | Did not finish | "Did not finish" (attention colour) |

- **Tags:** at most 3 pills, then a "+N" pill for the rest.
- **The date:** "Added 3 Oct", or "Finished 2 Sep" on the Read shelf.

The grid is `repeat(auto-fill, minmax(~8.5rem, 1fr))`, so the number of columns follows the width. Covers use `loading="lazy"`.

### Book row (list)

A small cover, then **title, author and up to 5 tag pills (+N)** in the middle, and on the right the **status line** (with the bar for a reading in progress) above **the date**.

When the search matched inside a review, note or quote, a third line in the middle shows the snippet, as today ("In a note: …the **Shrike** is…").

The whole tile or row is one link to the details page.

### Sorting

| Option | Natural direction | Order |
|---|---|---|
| **Default** | as today | **Reading:** by the latest progress · **Read:** by finish date · **Want to read:** by date added · **DNF / All:** by the latest change |
| Date added | newest first | `added_at` |
| Title | A–Z | the title, case-insensitive, ignoring a leading "The", "A" or "An" |
| Author | A–Z | the last word of the first author (text before the first comma), then the title |

- **The direction toggle reverses the natural direction.** Picking a new sort resets the direction to natural.
- **A series is always in reading order.** The sort controls are disabled while the list is filtered to a series.
- **Search keeps the chosen sort**, as today.
- Lists aren't paged (a personal library is a few hundred books at most), so the Title and Author orders can be computed in Go after the query if SQL gets awkward.

### Preferences

There's one new table, following Later's `later_prefs`:

```sql
-- migrations/0008_prefs.sql
CREATE TABLE books_prefs (
    user_id    INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    view       TEXT    NOT NULL CHECK (view IN ('grid', 'list')),
    sort       TEXT    NOT NULL CHECK (sort IN ('default', 'added', 'title', 'author')),
    reversed   INTEGER NOT NULL CHECK (reversed IN (0, 1)),
    updated_at TEXT    NOT NULL
) STRICT;
```

- **No row means the defaults:** grid view, Default sort, not reversed.
- **`POST /books/prefs`** (CSRF-checked) sets any of `view`, `sort` and `reversed`, and answers with the list (htmx) or a redirect back to `/books/?…` (no JavaScript).
- **View and sort live only in the preference, not in the URL.** They are account settings, so the back link and browser Back land on the same view anyway.
- **Deleting a user removes the row** through `ON DELETE CASCADE`, like the user's other Books data.

### Phone: the drawer

At the existing narrow breakpoint, the sidebar is hidden off-canvas:

- **A toolbar button opens it.** The button is the first thing in the toolbar and shows the current shelf or tag ("Reading ▾"). Opening the drawer slides the whole sidebar in from the left over a dimmed grid.
- **It works without JavaScript**, using the checkbox technique from PATTERNS.md: the button is a `<label>` for a visually hidden checkbox.
- **Closing it:** a tap on the dim layer (also a label) or Esc closes it. Picking a shelf or tag swaps the list and closes the drawer, because the swapped toggle checkbox comes back unchecked.
- **The old drill-down goes**, including both pane-toggle checkboxes and the "← Shelves" / "← Books" back labels.

## Book details page

URL `/books/b/{id}`, now a full page. It carries the list context (`shelf`, `tag`, `series`, `q`) in its query so it can link back. The content is a single centred column, `max-width: 48rem` (the mockup's 760px). It is the same layout on a phone, except that the header's cover and titles stack.

### Page bar

- **Left:** "← Reading" (or the tag, series or "All books" heading), which returns to `/books/?<ctx>`.
- **Right:** two quiet icon buttons with `aria-label` and `title`:
  - **✎ Edit details** goes to `/books/edit/{id}?<ctx>`.
  - **🗑 Delete** opens the existing confirm dialog, then returns to the list.

The ⋯ book menu is removed. Find cover moves to the Edit page.

### Header

The header is the same as today: cover or spine, title, subtitle, authors, the series link, and facts (`year · pages · ISBN`). Then come **read-only pills**: the shelf pill first (accent), then the tags.

- **Tag pills link** to `/books/?tag=…`.
- **The series link** goes to `/books/?series=…`.
- **The tags form is removed** from this page.

### Sections

Each section is a `<section>` with:

- **A top hairline** (`--c-border`).
- **A small uppercase heading** in `--c-text-faint`, with a count where it has one ("Notes · 2").
- **At most one action, on the right of the heading:** a round **+**, or a ✎ icon.

Sections appear in this order:

1. **Reading**
   - **Reading in progress:** "Since 28 Aug" and a **format dropdown**. That's a native `<select>` showing an icon and a chevron, so it reads as a control, not a tag. It saves on change through htmx; without JavaScript a small Save button shows inside `<noscript>`.
   - **Progress:** a thin bar, then the row "Page [212] of 482 · 44% · updated yesterday", with a **small number box and a small ✓ save button**. Enter also saves. The button is needed because the iOS number pad has no Enter key.
   - **Finishing:** small **Finish…** (primary) and **Did not finish…** buttons, right-aligned on the same row. Each opens a small popover holding today's fields: the date, then a rating (Finish) or the stopping point (DNF).
   - **With no reading in progress**, the section shows just a small primary **Start reading / Read again / Start again** button (today's `StartLabel`).
2. **Your take**
   - **The stars**, as today: click to set, click the current star to clear.
   - **The rendered review.**
   - **The ✎ action** in the heading turns the review into a textarea in place (a `<details>` disclosure). With no review yet, a quiet "Write a review" link sits under the stars and opens the same disclosure.
3. **About** shows the Open Library description, clamped, with More/Less as today. The section is omitted when there's no description.
4. **Notes · n**, with a **+** action.
   - **+** opens the add form at the top of the section.
   - **Each entry** shows its meta line ("10 Oct · p. 198") and its text.
   - **✎ / 🗑 icons** sit at the entry's top-right. They're visible on `:hover` and `:focus-within`, and always when `(hover: none)`.
   - **✎ turns the entry into its form in place.** 🗑 goes through the confirm dialog.
5. **Quotes · n** works the same as Notes. Quotes keep their blockquote styling and the page under them.
6. **Reading history · n** is a `<details>`, **collapsed by default**. Each reading has ✎ / 🗑 icons in the same pattern. ✎ opens today's date and format form in place. Its heading is a plain `<h2>`, and its toggle is a chevron lifted into the heading row like Notes' +, because a heading inside `<summary>` is lost to heading navigation in some screen readers (#603).
   - On a touch screen (`(hover: none)`) the entries' ✎ / 🗑 are 44px, the usual touch target (#603).

### Swaps

These are smaller than today's, because there's no list on the page any more:

- **Progress, rating, review, notes and quotes** each swap only their own section. The `*-swap` blocks lose their out-of-band list.
- **Start, finish, DNF, format, and editing or deleting a reading** swap the whole details page body, since the shelf pill in the header changes too.
- **Deleting the book** redirects (`HX-Redirect` for htmx) to `/books/?<ctx>`.
- **Every form still works without JavaScript** (post, then redirect back to the details page with its ctx).

## Add and Edit pages

- **Both get the page bar.** Edit shows "← *Book title*" back to the details page (keeping the ctx). Add shows "← *list heading*" back to the list. The Cancel link goes to the same place. Neither page has right-hand actions.
- **Edit gains the Tags field.** It is currently Add-only, inside the `{{if $d.New}}` block. `update` saves tags with the existing `SetTags`. The `POST /books/tags/{id}` route and the `tags-form` template are removed.
- **Edit's Cover fieldset gains a Find cover button** while the book has no cover. It posts to the existing `/books/find-cover/{id}` and comes back to the Edit page with the result as a notice ("Found a cover" or "Open Library has no cover for this book").
- **Cosmetics only otherwise:** the same column width and section-heading style as the details page, and the Save/Cancel row aligned. The Open Library search on Add is unchanged.

## Keyboard

- **Home:**
  - `j`/`k` move a highlight through the tiles or rows in DOM order.
  - `Enter` or `o` opens the highlighted book.
  - `/` focuses search, and `a` goes to Add book.
- **Details page:**
  - `p` focuses the progress box, `n` opens the add-note form, `q` the add-quote form, and `e` goes to Edit.
  - `Esc` closes an open disclosure or popover first. With nothing open, it goes back to the list. The Reading history is a section, not a popup: Esc leaves it open and goes back to the list (decided 2026-10-12, #600).

Keys are ignored while typing, as today.

## What goes away

- **The three-pane layout:** the list/book gutter and its saved width, and the phone drill-down checkboxes.
- **Panes-only code in `books.js`:** the row highlight and scroll restore across panes swaps.
- **The ⋯ book menu**, the tags form and route, and **list-swap's** out-of-band book-pane bookkeeping.
- **The `panes-oob`, `book-swap` and `list-swap` blocks**, in their current form. Home keeps a list swap (shelf, tag, search, sort and view changes swap the list, toolbar heading and sidebar). The details page gets its own page and section blocks.

## What stays

- **Tokens only**, so no new colours or spacing values. Icons come from `ticon`, adding `edit`, `trash` and `grid`/`list` if they're missing.
- **htmx with no-JavaScript fallbacks**, CSP-clean scripts, and the single confirm dialog.
- **The shell crumb and `<title>` out-of-band sync** (#205) on every swap that changes page.

## Errors

- **A bad `view`, `sort` or `reversed` value** on `POST /books/prefs` gets a 400 and is not stored.
- **A missing or foreign book on the details or Edit page** gets a 404, as today.
- **A Find cover lookup failure** shows on Edit as a notice and is not a page error.

## Testing

- **Store:** prefs read and write per user, with defaults when there's no row. Each sort, natural and reversed, on each shelf. A series ignores the sort. The Title sort's article rule and the Author sort's surname rule are table-driven.
- **Handlers:**
  - `POST /books/prefs` validates and persists, and answers htmx and no-JS requests.
  - The home page renders grid or list from the preference, and the list while `q` is set.
  - The details page renders its sections and a back link carrying the ctx.
  - Edit saves tags, and Find cover from Edit comes back to Edit.
  - The removed routes 404.
- **htmlassert:**
  - Every icon-only button has an accessible name.
  - The drawer toggle and dim layer are labels for the same checkbox.
  - The details page has no ⋯ menu and no tags form.
- **A manual browser pass** at desktop and phone widths, in light and dark: the drawer, the hover icons (and that they show on touch and keyboard focus), the popovers, and the Enter-less progress save on a phone.

## Docs

- **`docs/user/books.md`:** rewrite *The library* and *A book* for the two-pane home and the details page.
- **Screenshots:** replace `books-panes.png` and `books-menu.png` with `books-home.png` (grid), `books-home-list.png` and `books-book.png`. Retake `books-add.png` if the page bar changes it noticeably.
- **The README hero**, if it shows Books.

## Phases

Two PRs, in order:

| Phase | Scope |
|---|---|
| UI1 | `books_prefs` and `POST /books/prefs`; the two-pane home with toolbar, grid/list, sort, search-shows-list and the phone drawer; `/books/b/{id}` becomes a full page with the page bar (Edit/Delete icons, back link). Its body is today's book pane minus the ⋯ menu, so nothing is lost in between. |
| UI2 | The details page sections (Reading, Your take, About, Notes, Quotes, History), the new swaps and hover icons; Tags and Find cover on Edit; the page bar on Add/Edit; keyboard updates; the user guide and screenshots. |

## Out of scope

- Paging or virtual scrolling of the grid.
- Per-shelf sort memory.
- Changing the stats, import or export pages beyond what the sidebar links need.
- Bulk actions (multi-select, bulk tag).
- Drag-to-reorder.
