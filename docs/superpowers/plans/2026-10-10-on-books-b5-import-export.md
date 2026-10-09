# ON Books B5 — Import and export Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring a Goodreads library into ON Books, fill in covers in the background (and on demand with **Find cover**), export everything as JSON (`onsuite export`) and as one Markdown file, give Books an admin card, and finish the book pane's open polish issues — with the user guide, README and screenshots to match.

**Architecture:** The import is two pure halves: `ParseGoodreads` reads the whole CSV into `ImportBook`s (any problem is an `*ImportError` naming the line), then `Store.Import` writes them in one transaction, skipping duplicates; imported books reach `books_search` through its existing insert trigger. Migration 0007 adds `cover_checked_at`; a job on the platform scheduler (`App.Jobs`, so it shows in /admin/jobs with Run now) looks up a few covers by ISBN per run, and the ⋯ menu's Find cover runs the same lookup at once, falling back to a title/author search. `Store.Export` reads each table once and is both the `app.Exporter` payload and the source of `books-export.md`. `App.Stats` is the `app.Stater` card. The polish is template/CSS, plus a scroll-keeping section in `books.js`.

**Tech Stack:** Go 1.22+ `ServeMux`, `encoding/csv`, `html/template`, htmx 2, SQLite via `modernc.org/sqlite`, `internal/platform/webfetch`, `internal/platform/jobs`.

**Spec:** [docs/superpowers/specs/2026-10-09-on-books-design.md](../specs/2026-10-09-on-books-design.md) — "Import (B5)", "Covers after import", "Export (B5)", "Admin card", "Errors", "Testing", "Docs", "Phases" (B5 row). Builds on B4 ([plan](2026-10-09-on-books-b4-goals-stats.md), #584). Issue #491.

**The PR closes #491, #566, #567, #568, #578 and #579.**

## Defaults to confirm

Chosen while planning (2026-10-10); each is one constant or one line to change.

1. **Upload cap: `MaxImportBytes` = 10 MB** (the route's body cap is that plus 1 MiB). A few thousand books with long reviews is a few MB. A bigger file gets "That file is larger than 10 MB." on the page; one past the route cap gets the suite's 413 page.
2. **Backfill job: every 5 minutes, 25 books a run, 1 second between requests** ("fetch book covers"). Open Library allows 100 cover-by-ISBN lookups per 5 minutes per address, so this stays well inside it even with Find cover in use: ~300 covers an hour. A 404 (or an answer that isn't an image) marks the book checked; any other failure (timeout, 5xx) stops the run with an error on /admin/jobs and leaves the book to be tried next time.
3. **Duplicates: ISBN-13 against ISBN-13; when either the row or the existing book has no ISBN, title + authors ignoring case, spaces and punctuation.** The spec says "same title and authors (case-insensitive) when there is no ISBN"; the trial run showed that reading it as "when the *row* has no ISBN" re-imports every book typed in by hand without one (the demo's Piranesi and Remains of the Day), and that plain case-insensitive matching misses "James S.A. Corey" (Goodreads) against "James S. A. Corey" (Open Library). A row with an ISBN and a book with a *different* ISBN are two editions, and both are kept.
4. **Goodreads columns are found by header name**, and only `Title`, `Author` and `Exclusive Shelf` are required, so older exports with extra columns still import. The spec's "Other Bookshelves" is Goodreads' `Bookshelves` column; `read`, `currently-reading` and `to-read` are dropped from it (they are shelves here, not tags).
5. **An exclusive shelf of the person's own** (e.g. `did-not-finish`, `abandoned`) → no reading (Want to read) and the shelf name as a tag. Nothing is lost, and nothing is guessed. See Open questions.
6. **Series from the title:** `Leviathan Wakes (The Expanse, #1)` → title "Leviathan Wakes", series "The Expanse" #1 (Goodreads' own format; numbers like 2.5 work). Titles with two series or a range (`#1-3`) are left whole. Not in the spec's mapping, but otherwise every series book's title carries the series and the series links don't work.
7. **Dates:** `Date Added` → `added_at` at local midnight of that day; `updated_at` is the later of Date Added and Date Read (so "latest change" on All/DNF follows the reading, not the import). Imported readings' `created_at` is the import time; the earlier, undated readings are inserted first so the dated one is latest.
8. **Read Count:** `n − 1` earlier finished, undated readings for *read* and *currently-reading* (a re-read in progress counts the reads before it); ignored for *to-read*. More than `MaxReadCount` = 100 is refused as a broken file. Only the main reading gets the Binding's format.
9. **Year:** Original Publication Year, else Year Published, whichever is 1–9999 (Goodreads gives ancient books negative years: The Odyssey's −700 falls back to 1999).
10. **A bad row refuses the whole file** with `Line N: …` (a bad rating, date, Read Count, or a field over the book limits) — the spec's "parse errors name the line". A review over 20,000 characters is one of those.
11. **Find cover is shown only while the book has no cover** (it would otherwise silently replace an upload). It tries the ISBN first, then — when there is no ISBN *or* Open Library has no cover for it — a title + first-author search. "None found" marks the book checked and says so in the banner; Open Library not answering says that instead. The title search gets the 10-second image timeout (the trial saw Open Library's field search take 9s).
12. **The backfill never touches `updated_at`** and never replaces a cover that appeared meanwhile; **removing a cover by hand sets `cover_checked_at`** (or the job would bring it back), and **saving a new ISBN clears it** (so the job looks again).
13. **JSON shape** (`apps.books` in `onsuite export`): `{"books": [...], "goals": [{"year", "target"}]}`; each book has its details, `shelf`, `tags`, `readings` (oldest first, each with its `progress` rows: `page` or `percent`, `recorded_at`), `notes`, `quotes`, `added_at`, `updated_at`. No cover bytes, no `cover_checked_at`. Empty lists are `[]`, not `null`.
14. **Markdown layout** (`books-export.md`): `# My books`, a line with the date and count, the goals, then `## Title` per book **alphabetically (ignoring case)**: subtitle in italics, a list of details (Author, Series, First published, Pages, ISBN, Shelf, Tags, Rating as stars, Added), then `### Readings` (newest first, as the book pane), `### Review`, `### Notes` (`**9 Oct 2026 · p. 112**` then the note), `### Quotes` (a blockquote, `— p. 8`, then the comment). The Open Library description is left out (it isn't the person's writing; it is in the JSON).
15. **Sidebar links** under Stats: "Import from Goodreads" (`/books/import`) and "Export as Markdown" (`/books/export`, with `download`).
16. **#566:** a description over 300 characters, or with 4+ line breaks, is clamped to 4 lines with a **More / Less** toggle (the checkbox technique, no JavaScript); shorter ones show no toggle.
17. **#568:** each shelf count carries visually hidden words — the link reads "Want to read, 2 books".
18. **#579:** the five stars sit in a `nowrap` group (`.books-star-row`), so in a narrow pane they wrap *together* under the label instead of the fifth star wrapping alone. Checked at 1024px: the pane is 240px and all five stars are on one line under "Your rating".
19. **#578: keep the scroll in `books.js`** rather than narrower swap targets. Every action that swaps `#books-panes` (stars, review, tags, format, readings, Find cover) can move the shelf counts, the list and the book at once, so narrowing each would mean a new swap block with two or three out-of-band companions per action; the script is ~40 lines, covers future actions for free, and keeps the progress/notes/quotes swaps as they are. It restores the book pane's scroll when the same book is still open, and the list's when the same list is.

## Open questions (the spec was silent; picked something, worth a look)

- **A Goodreads "did-not-finish"-style shelf:** should a shelf named `dnf`, `did-not-finish` or `abandoned` make a DNF reading instead of a tag (default 5)?
- **Private Notes:** Goodreads exports a `Private Notes` column. The spec doesn't map it; it could become a book note. Left out.
- **Description in the Markdown** (default 14) — in or out?
- **Open Library's search was timing out** from this machine during the trial (both the Add page's and Find cover's); covers by ISBN worked. Nothing to change in the code — the banner says "Open Library didn't answer" — but if it keeps happening, Find cover without an ISBN will often fail.

## Global Constraints

- **One B5 PR.** The tasks below are separate commits on branch `feat/books-b5-import-export` in the worktree `../on-suite-books-b5`. Ilia pauses after every task.
- Apps never import each other (`internal/arch` enforces it); Books imports only `internal/platform/*`. `humanBytes` mirrors ON Later's (PATTERNS.md "Cross-app mirroring").
- Every query is scoped by `user_id` (the job and the admin card are the only all-users readers); a foreign or missing id is a 404. Every POST carries the CSRF token; with JavaScript off every form still works (plain posts, 303 back, 422 with the message on a refusal).
- CSP: no inline `<script>`, no `style=""`. App CSS goes at the end of the "ON Books" section of `internal/ui/static/app.css` (the last section of the file), classes prefixed `books-`.
- Timestamps through `formatTime` (`db.FormatTime`); dates `YYYY-MM-DD` in the local day (`time.Local`); app code reads time only through the store's `now()` (`TestAppsReadTheirStoreClock`). Handler tests move time with `s.Clock`.
- Errors follow PATTERNS.md: a refused action is the banner over the panes (`*Refusal` through `act`), a refused form comes back inline with what was sent; Open Library failures are logged and shown, never a 500.
- Download filenames are generic (`books-export.md`), never derived from a title.
- Full check must stay green on every commit:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- Open the PR with `env -u GH_TOKEN gh …`; never merge.

## Lessons from earlier plans (read before starting)

- staticcheck U1000 fails on an unexported helper added before its first use; ST1005 fails on an error string that starts with a capital or ends with punctuation — so the Goodreads row mapper returns a *message string* for the person, and only `ImportError` (whose `Error()` is the message) is an error.
- `internal/htmlassert` supports one qualifier per selector: `p.is-clamped` and `input#books-import-file` work; `.books-description-text.is-clamped` and `input#x[name="file"]` don't. Check a second condition with `htmlassert.Attr`.
- `go vet` rejects unkeyed composite literals of another package's struct types in `_test` packages: every `books.ImportBook{…}` is keyed.
- `TestMain` pins `time.Local` to Melbourne; the store fixture's clock is 15:00 UTC on 9 October 2026 (10 October locally). Day-keyed handler tests use `noon("…")`.
- Helper names must not collide: `texts`, `attr`, `noon`, `add`, `titled`, `getBook`, `coverSource`, `postMultipart` exist already. The new ones are `grFixture`, `grHeader`, `grLine`, `parse`, `importFixture`, `bookTitled`, `goodOmens`, `postImport`, `newBooksApp`, `withISBN`, `checkedAt`, `runBackfill`, `getBookAt`, `findCover`, `exportDoc`, `exportFor`.
- The database has one connection: close a query's rows before the next statement (`Store.eachRow`, `libraryKeys`).
- This plan's code was trial-run on 2026-10-10: written and tested in a scratch tree, then a script applied every "Create" and "replace … with" block of this document, in order, to a fresh worktree from `main`, ran each task's tests, and the full check at the end. A browser pass over a seeded server (importing the fixture, the Markdown download, Run now on the job against the real Open Library, the polish at 1024px and 375px) is what changed the duplicate rule (default 3: either side without an ISBN, and loose name matching) and Find cover's search (first author, longer timeout).

## File map

| File | Responsibility |
|---|---|
| `internal/apps/books/goodreads.go` | `MaxImportBytes`, `MaxReadCount`, `ImportBook`, `ImportError`, `ParseGoodreads` and the column mapping |
| `internal/apps/books/testdata/goodreads_library_export.csv` | the fixture: a real export header and awkward rows |
| `internal/apps/books/import.go` | `ImportResult`, `Store.Import`, duplicate keys, `importBook`, `localDay` |
| `internal/apps/books/migrations/0007_cover_checked.sql` | `cover_checked_at` |
| `internal/apps/books/backfill.go` | `ErrNoCover`, `OpenLibrary.CoverByISBN`, `CoverCandidate`, `Store.CoverCandidates`/`FillCover`/`MarkCoverChecked`, `App.BackfillCovers`, `sleep`, `App.Jobs` |
| `internal/apps/books/findcover.go` | `OpenLibrary.FindCoverID`, `lookUpCover`, the `findCover` action |
| `internal/apps/books/importpage.go`, `templates/import.html` | the Import page (`GET`/`POST /books/import`) |
| `internal/apps/books/export.go` | the export payload types, `App.Export`, `Store.Export`, `Store.eachRow` |
| `internal/apps/books/download.go` | `exportMarkdown`, `GET /books/export` |
| `internal/apps/books/admin.go` | `App.Stats`, `Store.AdminStats`, `humanBytes` |
| `internal/apps/books/books.go`, `cover.go`, `library.go`, `view.go` | routes, the pause hook; `RemoveCover`/`Update` and `cover_checked_at`; `DescriptionLong` |
| `internal/apps/books/templates/panes.partial.html` | sidebar links, Find cover, shelf counts, description, star row |
| `internal/apps/books/static/books.js` | keep the panes' scroll across a swap (#578) |
| `internal/ui/static/app.css` | import page, sidebar links, description clamp, star row |
| `internal/apps/books/*_test.go` | `goodreads_test.go`, `import_test.go`, `backfill_test.go`, `findcover_test.go`, `import_view_test.go`, `export_json_test.go`, `download_test.go`, `admin_test.go`, `polish_test.go`; `export_test.go` and `openlibrary_test.go` gain hooks and fake endpoints |
| `docs/user/books.md`, `README.md`, `AGENTS.md`, `docs/screenshots/*` | guide, app section and hero (#567), app list, screenshots |

---

### Task 0: Worktree and branch

- [ ] **Step 1: Create the worktree**

```bash
cd /Users/iliaf/src/WEB/on-suite
git fetch origin
git worktree add ../on-suite-books-b5 -b feat/books-b5-import-export origin/main
cd ../on-suite-books-b5
go build ./cmd/onsuite && rm -f onsuite
```
Expected: builds with no output. All later commands run in `../on-suite-books-b5`. `origin/main` must already include this plan's PR.

---

### Task 1: Read a Goodreads export

A pure parser: no database, no HTTP. It reads the whole file before anything is written (spec "Import (B5)"), maps every row, and refuses the file with the first problem, naming its line.

**Files:**
- Create: `internal/apps/books/goodreads.go`, `internal/apps/books/testdata/goodreads_library_export.csv`
- Test: `internal/apps/books/goodreads_test.go`

**Interfaces:**
- Consumes: `BookInput` (`Normalize`, `Validate`, `FieldErrors`), `ISBN13`, `ParseTags`, `Shelf*`, `MaxPages`, `MaxYear`, `MinYear`, `MaxReviewRunes`, `dayLayout`, `ErrInvalid` (existing).
- Produces:
  - `const MaxImportBytes = 10 << 20`, `const MaxReadCount = 100`
  - `type ImportBook struct { Line int; BookInput; Rating int; Review string; Shelf Shelf; FinishedOn, StartedOn string; EarlierReads int; Format string; Tags []string; AddedOn string }` — `BookInput` normalized and valid; `Shelf` is `ShelfRead`, `ShelfReading` or `ShelfWant`; dates `YYYY-MM-DD` or ""
  - `type ImportError struct { Line int; Msg string }` — `Error()` is `"Line 4: …"` (just `Msg` when `Line` is 0); unwraps to `ErrInvalid`
  - `func ParseGoodreads(r io.Reader) ([]ImportBook, error)`
  - test helpers `grFixture(t) []byte`, `grHeader`, `grLine(title, rating, binding, dateRead, shelves, exclusive, review, readCount string) string`, `parse(t, csv) []books.ImportBook`

The fixture's rows, by line: 2 Leviathan Wakes (series in the title, `="…"` ISBNs, a `<br/>` review with `&amp;`, Read Count 2, Paperback, shelves "sf, space"); 3 Piranesi (currently-reading, **Kindle Edition**, **rating 0**); 4 Good Omens (**a quoted comma** in the title, two authors, **empty `=""` ISBNs**, to-read); 5–8 The Remains of the Day (a review with **quoted line breaks and quotes**, Audible Audio); 9 Good Omens again (a duplicate within the file, no page count); 10 The Odyssey (Original Publication Year **−700**, a "classics" shelf); 11 Infinite Jest (a **shelf of its own**, `did-not-finish`). The header is the real one from Goodreads' "Export Library" (24 columns, `Book Id` … `Owned Copies`).

- [ ] **Step 1: Write the fixture and the failing tests**

Create `internal/apps/books/testdata/goodreads_library_export.csv`:

```csv
Book Id,Title,Author,Author l-f,Additional Authors,ISBN,ISBN13,My Rating,Average Rating,Publisher,Binding,Number of Pages,Year Published,Original Publication Year,Date Read,Date Added,Bookshelves,Bookshelves with positions,Exclusive Shelf,My Review,Spoiler,Private Notes,Read Count,Owned Copies
8855321,"Leviathan Wakes (The Expanse, #1)",James S.A. Corey,"Corey, James S.A.",,"=""0316129089""","=""9780316129084""",4,4.27,Orbit,Paperback,592,2011,2011,2024/03/14,2024/02/01,"sf, space","sf (#3), space (#1)",read,"Great fun.<br/><br/>Loved <i>Miller</i> &amp; Holden.",,,2,0
50202953,Piranesi,Susanna Clarke,"Clarke, Susanna",,"=""1635575648""","=""9781635575637""",0,4.21,Bloomsbury Publishing,Kindle Edition,272,2020,2020,,2025/01/05,currently-reading,currently-reading (#1),currently-reading,,,,1,0
12067,"Good Omens: The Nice and Accurate Prophecies of Agnes Nutter, Witch",Terry Pratchett,"Pratchett, Terry",Neil Gaiman,"=""""","=""""",0,4.25,William Morrow,Mass Market Paperback,491,2006,1990,,2025/06/30,to-read,to-read (#4),to-read,,,,0,0
28921,The Remains of the Day,Kazuo Ishiguro,"Ishiguro, Kazuo",,"=""0679731725""","=""9780679731726""",5,4.14,Vintage,Audible Audio,245,1993,1989,2023/11/02,2023/10/01,favourites,favourites (#1),read,"Stevens on ""dignity"".
So quiet, so sad.

The best ending.",,,1,0
12068,"Good Omens: The Nice and Accurate Prophecies of Agnes Nutter, Witch",Terry Pratchett,"Pratchett, Terry",Neil Gaiman,"=""""","=""""",0,4.25,Gollancz,Hardcover,,2014,1990,,2025/07/01,to-read,to-read (#5),to-read,,,,0,0
1381,The Odyssey,Homer,"Homer, ",Robert Fagles,"=""0140268863""","=""9780140268867""",0,3.80,Penguin Classics,Paperback,541,1999,-700,,2022/08/15,"to-read, classics","to-read (#9), classics (#2)",to-read,,,,0,0
6759,Infinite Jest,David Foster Wallace,"Wallace, David Foster",,"=""0316066524""","=""9780316066525""",2,4.30,Little Brown,Paperback,1079,2006,1996,,2021/04/02,did-not-finish,did-not-finish (#1),did-not-finish,,,,0,0
```

Create `internal/apps/books/goodreads_test.go`:

```go
package books_test

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// grFixture is a real Goodreads "Export Library" header with rows that
// are awkward on purpose (spec "Testing"): quoted commas and line breaks,
// ="…" ISBNs and an empty one, Read Count 2, a Kindle and an Audible
// binding, rating 0, a series in a title, a negative year, a shelf of the
// person's own, and the same book twice.
func grFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/goodreads_library_export.csv")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

const grHeader = "Book Id,Title,Author,Author l-f,Additional Authors,ISBN,ISBN13,My Rating,Average Rating,Publisher,Binding,Number of Pages,Year Published,Original Publication Year,Date Read,Date Added,Bookshelves,Bookshelves with positions,Exclusive Shelf,My Review,Spoiler,Private Notes,Read Count,Owned Copies\n"

// grLine is one row of the export with the columns a test cares about;
// the rest are Goodreads' usual.
func grLine(title, rating, binding, dateRead, shelves, exclusive, review, readCount string) string {
	q := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	return strings.Join([]string{"1", q(title), "Ann Author", q("Author, Ann"), "", `"="""""`, `"="""""`, rating, "4.00",
		"Pub", binding, "300", "2001", "2000", dateRead, "2024/01/02", q(shelves), "", exclusive, q(review), "", "", readCount, "0"}, ",") + "\n"
}

func parse(t *testing.T, csv string) []books.ImportBook {
	t.Helper()
	rows, err := books.ParseGoodreads(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestParseGoodreadsMapsTheFixture(t *testing.T) {
	rows, err := books.ParseGoodreads(strings.NewReader(string(grFixture(t))))
	if err != nil {
		t.Fatal(err)
	}
	goodOmens := books.ImportBook{Line: 4, BookInput: books.BookInput{
		Title: "Good Omens: The Nice and Accurate Prophecies of Agnes Nutter, Witch", Authors: "Terry Pratchett, Neil Gaiman",
		Year: 1990, Pages: 491}, Shelf: books.ShelfWant, AddedOn: "2025-06-30"}
	want := []books.ImportBook{
		{Line: 2, BookInput: books.BookInput{Title: "Leviathan Wakes", Authors: "James S.A. Corey", Year: 2011, Pages: 592,
			ISBN: "9780316129084", SeriesName: "The Expanse", SeriesNumber: "1"},
			Rating: 4, Review: "Great fun.\n\nLoved Miller & Holden.", Shelf: books.ShelfRead, FinishedOn: "2024-03-14",
			EarlierReads: 1, Format: "paper", Tags: []string{"sf", "space"}, AddedOn: "2024-02-01"},
		{Line: 3, BookInput: books.BookInput{Title: "Piranesi", Authors: "Susanna Clarke", Year: 2020, Pages: 272, ISBN: "9781635575637"},
			Shelf: books.ShelfReading, StartedOn: "2025-01-05", Format: "ebook", AddedOn: "2025-01-05"},
		goodOmens,
		{Line: 5, BookInput: books.BookInput{Title: "The Remains of the Day", Authors: "Kazuo Ishiguro", Year: 1989, Pages: 245,
			ISBN: "9780679731726"}, Rating: 5, Review: "Stevens on \"dignity\".\nSo quiet, so sad.\n\nThe best ending.",
			Shelf: books.ShelfRead, FinishedOn: "2023-11-02", Format: "audio", Tags: []string{"favourites"}, AddedOn: "2023-10-01"},
		{Line: 9, BookInput: books.BookInput{Title: goodOmens.Title, Authors: goodOmens.Authors, Year: 1990},
			Shelf: books.ShelfWant, AddedOn: "2025-07-01"},
		{Line: 10, BookInput: books.BookInput{Title: "The Odyssey", Authors: "Homer, Robert Fagles", Year: 1999, Pages: 541,
			ISBN: "9780140268867"}, Shelf: books.ShelfWant, Tags: []string{"classics"}, AddedOn: "2022-08-15"},
		{Line: 11, BookInput: books.BookInput{Title: "Infinite Jest", Authors: "David Foster Wallace", Year: 1996, Pages: 1079,
			ISBN: "9780316066525"}, Rating: 2, Shelf: books.ShelfWant, Tags: []string{"did-not-finish"}, AddedOn: "2021-04-02"},
	}
	if len(rows) != len(want) {
		t.Fatalf("%d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for i := range want {
		if !reflect.DeepEqual(rows[i], want[i]) {
			t.Errorf("row %d:\n got %+v\nwant %+v", i, rows[i], want[i])
		}
	}
}

func TestParseGoodreadsBindings(t *testing.T) {
	tests := map[string]string{
		"Kindle Edition": "ebook", "ebook": "ebook", "Nook": "ebook",
		"Audible Audio": "audio", "Audio CD": "audio", "Audiobook": "audio", "MP3 CD": "audio",
		"Paperback": "paper", "Hardcover": "paper", "Mass Market Paperback": "paper",
		"Library Binding": "", "Unknown Binding": "", "": "",
	}
	for binding, want := range tests {
		rows := parse(t, grHeader+grLine("A Book", "0", binding, "2024/05/01", "", "read", "", "1"))
		if rows[0].Format != want {
			t.Errorf("Binding %q = format %q, want %q", binding, rows[0].Format, want)
		}
	}
	// A book still to read has no reading, so no format.
	if rows := parse(t, grHeader+grLine("A Book", "0", "Paperback", "", "", "to-read", "", "0")); rows[0].Format != "" {
		t.Errorf("to-read format = %q, want none", rows[0].Format)
	}
}

func TestParseGoodreadsReadCounts(t *testing.T) {
	tests := []struct {
		exclusive, count string
		want             int
	}{
		{"read", "", 0},
		{"read", "0", 0}, // Goodreads says 0 for some books on read
		{"read", "1", 0},
		{"read", "3", 2},
		{"currently-reading", "2", 1}, // read once before, and again now
		{"to-read", "2", 0},           // no readings on Want to read
	}
	for _, tt := range tests {
		rows := parse(t, grHeader+grLine("A Book", "0", "", "", "", tt.exclusive, "", tt.count))
		if rows[0].EarlierReads != tt.want {
			t.Errorf("%s with Read Count %q: %d earlier readings, want %d", tt.exclusive, tt.count, rows[0].EarlierReads, tt.want)
		}
	}
}

func TestParseGoodreadsReviews(t *testing.T) {
	tests := map[string]string{
		"One.<br/>Two.<br />Three.<BR>Four.":     "One.\nTwo.\nThree.\nFour.",
		"<b>Bold</b> and <a href=\"x\">link</a>": "Bold and link",
		"Fish &amp; chips &lt;3":                 "Fish & chips <3",
		"  <br/>Trimmed<br/>  ":                  "Trimmed",
	}
	for review, want := range tests {
		rows := parse(t, grHeader+grLine("A Book", "0", "", "", "", "read", review, "1"))
		if rows[0].Review != want {
			t.Errorf("review %q = %q, want %q", review, rows[0].Review, want)
		}
	}
}

func TestParseGoodreadsTitlesAndTags(t *testing.T) {
	tests := []struct {
		title                  string
		wantTitle, series, num string
	}{
		{"Leviathan Wakes (The Expanse, #1)", "Leviathan Wakes", "The Expanse", "1"},
		{"Edgedancer (The Stormlight Archive, #2.5)", "Edgedancer", "The Stormlight Archive", "2.5"},
		{"Mort (Discworld, #4; Death, #1)", "Mort (Discworld, #4; Death, #1)", "", ""},
		{"The Expanse Omnibus (The Expanse, #1-3)", "The Expanse Omnibus (The Expanse, #1-3)", "", ""},
		{"Dune", "Dune", "", ""},
	}
	for _, tt := range tests {
		b := parse(t, grHeader+grLine(tt.title, "0", "", "", "", "to-read", "", "0"))[0]
		if b.Title != tt.wantTitle || b.SeriesName != tt.series || b.SeriesNumber != tt.num {
			t.Errorf("%q = %q, %q #%q", tt.title, b.Title, b.SeriesName, b.SeriesNumber)
		}
	}
	b := parse(t, grHeader+grLine("A Book", "0", "", "", "Read, to-read, SF, currently-reading, Book Club, sf", "to-read", "", "0"))[0]
	if want := []string{"sf", "book club"}; !reflect.DeepEqual(b.Tags, want) {
		t.Errorf("tags = %q, want %q: Goodreads' own shelves aren't tags", b.Tags, want)
	}
}

func TestParseGoodreadsRefusesBadFiles(t *testing.T) {
	tests := []struct {
		name, csv, want string
	}{
		{"empty", "", "That file is empty."},
		{"not an export", "Name,Email\nAnn,ann@example.com\n",
			"That doesn't look like a Goodreads library export: it has no “Title” column."},
		{"header only", grHeader, "That file has no books in it."},
		{"no title", grHeader + grLine("", "0", "", "", "", "to-read", "", "0"), "Line 2: Title: Enter the book's title."},
		{"bad rating", grHeader + grLine("A", "0", "", "", "", "read", "", "1") + grLine("B", "7", "", "", "", "read", "", "1"),
			"Line 3: My Rating must be a whole number from 0 to 5."},
		{"bad date", grHeader + grLine("A", "0", "", "14/03/2024", "", "read", "", "1"),
			"Line 2: Date Read must be a date like 2024/03/14."},
		{"bad read count", grHeader + grLine("A", "0", "", "", "", "read", "", "many"),
			"Line 2: Read Count must be a whole number from 0 to 100."},
		{"too many reads", grHeader + grLine("A", "0", "", "", "", "read", "", "101"),
			"Line 2: Read Count must be a whole number from 0 to 100."},
		{"short row", grHeader + "1,Dune,Frank Herbert\n", "Line 2: this line can't be read as CSV (wrong number of fields)."},
		{"stray quote", grHeader + grLine("A", "0", "", "", "", "read", "", "1") + "1,\"Du\"ne,x\n",
			`Line 3: this line can't be read as CSV (extraneous or missing " in quoted-field).`},
	}
	for _, tt := range tests {
		_, err := books.ParseGoodreads(strings.NewReader(tt.csv))
		var ie *books.ImportError
		if !errors.As(err, &ie) || ie.Error() != tt.want {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
		if !errors.Is(err, books.ErrInvalid) {
			t.Errorf("%s: err is not ErrInvalid", tt.name)
		}
	}
}

func TestParseGoodreadsReadsAByteOrderMark(t *testing.T) {
	rows := parse(t, "\uFEFF"+grHeader+grLine("Dune", "0", "", "", "", "to-read", "", "0"))
	if len(rows) != 1 || rows[0].Title != "Dune" {
		t.Errorf("rows = %+v, want Dune", rows)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — the package doesn't build: `undefined: books.ParseGoodreads`, `undefined: books.ImportBook`, `undefined: books.ImportError`.

- [ ] **Step 3: Write the parser**

Create `internal/apps/books/goodreads.go`:

```go
package books

import (
	"encoding/csv"
	"errors"
	"fmt"
	"html"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxImportBytes bounds an uploaded Goodreads export (decided 2026-10-10
// while planning B5): a library of a few thousand books with long
// reviews is a few megabytes, so ten is room to spare.
const MaxImportBytes = 10 << 20

// MaxReadCount is the largest Read Count a row may give. More is a broken
// file, not a reader: each read past the first becomes a reading.
const MaxReadCount = 100

// ImportBook is one row of a Goodreads "Export Library" CSV, mapped to ON
// Books (spec "Import (B5)"). BookInput is already normalized and valid.
type ImportBook struct {
	Line int // where the row starts in the file, for messages
	BookInput
	Rating int    // 1–5, 0 for none
	Review string // plain text, line breaks kept
	// Shelf is where the book goes: ShelfRead (a finished reading dated
	// FinishedOn), ShelfReading (a reading started on StartedOn) or
	// ShelfWant (no reading).
	Shelf      Shelf
	FinishedOn string // YYYY-MM-DD, "" when Goodreads has no Date Read
	StartedOn  string // YYYY-MM-DD, the Date Added of a book being read
	// EarlierReads is how many finished readings with no dates come
	// before the main one: Read Count less one.
	EarlierReads int
	Format       string   // the main reading's: "", "paper", "ebook" or "audio"
	Tags         []string // clean
	AddedOn      string   // YYYY-MM-DD, "" when the row has no Date Added
}

// ImportError is a file the import refuses. Line is where in the file,
// 0 for the file as a whole; Error is the message shown to the person.
type ImportError struct {
	Line int
	Msg  string
}

func (e *ImportError) Error() string {
	if e.Line == 0 {
		return e.Msg
	}
	return fmt.Sprintf("Line %d: %s", e.Line, e.Msg)
}

// Unwrap makes an ImportError an ErrInvalid for errors.Is.
func (e *ImportError) Unwrap() error { return ErrInvalid }

// Goodreads' export columns this import reads, by header name: the
// header row is looked up, not counted, so an older export with extra
// columns reads the same.
const (
	grTitle      = "Title"
	grAuthor     = "Author"
	grMoreAuth   = "Additional Authors"
	grISBN       = "ISBN"
	grISBN13     = "ISBN13"
	grRating     = "My Rating"
	grBinding    = "Binding"
	grPages      = "Number of Pages"
	grYear       = "Year Published"
	grOrigYear   = "Original Publication Year"
	grDateRead   = "Date Read"
	grDateAdded  = "Date Added"
	grShelves    = "Bookshelves"
	grExclusive  = "Exclusive Shelf"
	grReview     = "My Review"
	grReadCount  = "Read Count"
	grDateLayout = "2006/01/02"
)

// grSeries is Goodreads' way of putting a series into a title:
// "Leviathan Wakes (The Expanse, #1)". A title with several series, or a
// number that isn't one ("#1-3"), is left as it is.
var grSeries = regexp.MustCompile(`^(.+?)\s*\(([^()#;]+?),\s*#(\d+(?:\.\d+)?)\)$`)

// grBindings maps a Binding to a format where it is obvious (spec "Import
// (B5)"); anything else is no format.
var grBindings = map[string]string{
	"kindle edition": "ebook", "ebook": "ebook", "nook": "ebook",
	"audible audio": "audio", "audio cd": "audio", "audiobook": "audio", "audio cassette": "audio", "mp3 cd": "audio",
	"paperback": "paper", "hardcover": "paper", "mass market paperback": "paper",
}

// grBuiltIn are Goodreads' three built-in exclusive shelves, which become
// shelves here, never tags.
var grBuiltIn = map[string]Shelf{"read": ShelfRead, "currently-reading": ShelfReading, "to-read": ShelfWant}

// fieldLabels names a book field by its Goodreads column, for messages.
var fieldLabels = map[string]string{"title": grTitle, "subtitle": grTitle, "authors": grAuthor,
	"year": grOrigYear, "pages": grPages, "isbn": grISBN13, "series_name": grTitle, "series_number": grTitle}

// ParseGoodreads reads a whole Goodreads library export before anything is
// written (spec "Import (B5)"), so a bad file leaves nothing behind. Any
// problem is an *ImportError naming the line it is on.
func ParseGoodreads(r io.Reader) ([]ImportBook, error) {
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, &ImportError{Msg: "That file is empty."}
	}
	if err != nil {
		return nil, csvError(err)
	}
	cols := map[string]int{}
	for i, name := range header {
		cols[strings.TrimSpace(strings.TrimPrefix(name, "\uFEFF"))] = i
	}
	for _, need := range []string{grTitle, grAuthor, grExclusive} {
		if _, ok := cols[need]; !ok {
			return nil, &ImportError{Msg: "That doesn't look like a Goodreads library export: it has no “" + need + "” column."}
		}
	}
	var out []ImportBook
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, csvError(err)
		}
		line, _ := cr.FieldPos(0)
		get := func(name string) string {
			if i, ok := cols[name]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		b, msg := grRow(get)
		if msg != "" {
			return nil, &ImportError{Line: line, Msg: msg}
		}
		b.Line = line
		out = append(out, b)
	}
	if len(out) == 0 {
		return nil, &ImportError{Msg: "That file has no books in it."}
	}
	return out, nil
}

// csvError words encoding/csv's errors for people.
func csvError(err error) error {
	var pe *csv.ParseError
	if errors.As(err, &pe) {
		return &ImportError{Line: pe.Line, Msg: "this line can't be read as CSV (" + pe.Err.Error() + ")."}
	}
	return &ImportError{Msg: "That file can't be read."}
}

// grRow maps one row (spec "Import (B5)": the mapping). get returns a
// column's trimmed text, "" when the file has no such column. A message
// is for the person, and means the row can't be imported.
func grRow(get func(string) string) (ImportBook, string) {
	var b ImportBook
	b.Title, b.SeriesName, b.SeriesNumber = grTitleSeries(get(grTitle))
	b.Authors = get(grAuthor)
	if more := get(grMoreAuth); more != "" {
		b.Authors += ", " + more
	}
	b.ISBN = grISBNOf(get(grISBN13), get(grISBN))
	b.Year = grYearOf(get(grOrigYear), get(grYear))
	var msg string
	if b.Pages, msg = grNumber(get(grPages), grPages, MaxPages); msg != "" {
		return b, msg
	}
	b.BookInput = b.BookInput.Normalize()
	if errs := b.BookInput.Validate(); errs != nil {
		return b, fieldMessage(errs)
	}

	if b.Rating, msg = grNumber(get(grRating), grRating, 5); msg != "" {
		return b, msg
	}
	b.Review = grReviewText(get(grReview))
	if utf8.RuneCountInString(b.Review) > MaxReviewRunes {
		return b, fmt.Sprintf("%s: Keep it to %d characters or fewer.", grReview, MaxReviewRunes)
	}
	if b.AddedOn, msg = grDate(get(grDateAdded), grDateAdded); msg != "" {
		return b, msg
	}
	read, msg := grDate(get(grDateRead), grDateRead)
	if msg != "" {
		return b, msg
	}
	count, msg := grNumber(get(grReadCount), grReadCount, MaxReadCount)
	if msg != "" {
		return b, msg
	}

	exclusive := strings.ToLower(get(grExclusive))
	shelf, builtIn := grBuiltIn[exclusive]
	if !builtIn {
		shelf = ShelfWant // a shelf of the person's own: no reading, and a tag
	}
	b.Shelf = shelf
	switch shelf {
	case ShelfRead:
		b.FinishedOn = read
	case ShelfReading:
		b.StartedOn = b.AddedOn
	}
	if shelf != ShelfWant {
		b.EarlierReads = max(0, count-1)
		b.Format = grBindings[strings.ToLower(get(grBinding))]
	}

	var tags []string
	for _, name := range strings.Split(get(grShelves), ",") {
		if _, ok := grBuiltIn[strings.ToLower(strings.TrimSpace(name))]; !ok {
			tags = append(tags, name)
		}
	}
	if !builtIn && exclusive != "" {
		tags = append(tags, exclusive)
	}
	b.Tags = ParseTags(strings.Join(tags, ","))
	return b, ""
}

// grTitleSeries splits "Leviathan Wakes (The Expanse, #1)" into the title,
// the series and its number.
func grTitleSeries(s string) (title, series, number string) {
	if m := grSeries.FindStringSubmatch(s); m != nil {
		return m[1], m[2], m[3]
	}
	return s, "", ""
}

// grISBNOf is the row's ISBN-13: its ISBN13, else its ISBN converted, else
// none. Goodreads wraps both as ="…" so spreadsheets keep the digits.
func grISBNOf(isbn13, isbn10 string) string {
	for _, s := range []string{isbn13, isbn10} {
		s = strings.TrimSuffix(strings.TrimPrefix(s, `="`), `"`)
		if v, ok := ISBN13(s); ok && s != "" {
			return v
		}
	}
	return ""
}

// grYearOf is the Original Publication Year, else the Year Published,
// whichever is a year ON Books keeps (1 to MaxYear: Goodreads gives
// ancient books negative years).
func grYearOf(years ...string) int {
	for _, s := range years {
		if y, err := strconv.Atoi(s); err == nil && y >= 1 && y <= MaxYear {
			return y
		}
	}
	return 0
}

// grNumber reads a whole number from 0 to most; "" is 0.
func grNumber(s, column string, most int) (int, string) {
	if s == "" {
		return 0, ""
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > most {
		return 0, fmt.Sprintf("%s must be a whole number from 0 to %d.", column, most)
	}
	return n, ""
}

// grDate turns Goodreads' 2024/03/14 into 2024-03-14; "" stays "".
func grDate(s, column string) (string, string) {
	if s == "" {
		return "", ""
	}
	t, err := time.Parse(grDateLayout, s)
	if err != nil || t.Year() < MinYear {
		return "", column + " must be a date like 2024/03/14."
	}
	return t.Format(dayLayout), ""
}

var (
	grBreak = regexp.MustCompile(`(?i)<br\s*/?>`)
	grTag   = regexp.MustCompile(`<[^>]*>`)
)

// grReviewText turns a review's HTML into text (spec "Import (B5)"): <br/>
// becomes a line break, every other tag goes, and entities are decoded.
func grReviewText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = grBreak.ReplaceAllString(s, "\n")
	s = grTag.ReplaceAllString(s, "")
	return strings.TrimSpace(html.UnescapeString(s))
}

// fieldMessage is the first of a book's field errors, by field name so
// the message is always the same one, named by its Goodreads column.
func fieldMessage(errs FieldErrors) string {
	fields := make([]string, 0, len(errs))
	for f := range errs {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	return fieldLabels[fields[0]] + ": " + errs[fields[0]]
}
```

- [ ] **Step 4: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./internal/apps/books/
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./internal/apps/books/
```
Expected: all pass, the older tests too.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): read a Goodreads library export (#491)"
```

---

### Task 2: Import into the library

One transaction for the whole file. Duplicates are skipped against the library and against earlier rows (default 3). The insert trigger of `books_search` (migration 0005) indexes each book, review included, as it is written.

**Files:**
- Create: `internal/apps/books/import.go`
- Test: `internal/apps/books/import_test.go`

**Interfaces:**
- Consumes: `ImportBook`, `ParseGoodreads` (Task 1); `linkTags`, `nullInt`, `nullText`, `formatTime`, `dayLayout`, `Store.now` (existing); test helpers `newFixture`, `addBook`, `onShelf`, `getBook` (existing), `grFixture` (Task 1).
- Produces:
  - `type ImportResult struct { Imported int; Skipped []string }` — skipped titles in file order
  - `(*Store).Import(ctx, userID int64, rows []ImportBook) (ImportResult, error)`
  - `func localDay(day string) time.Time` — local midnight of a valid `YYYY-MM-DD`
  - test helpers `importFixture(t, f, userID) books.ImportResult`, `bookTitled(t, f, userID, title) int64`, `const goodOmens`

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/import_test.go`:

```go
package books_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// importFixture parses the Goodreads fixture and imports it for userID.
func importFixture(t *testing.T, f *fixture, userID int64) books.ImportResult {
	t.Helper()
	rows, err := books.ParseGoodreads(bytes.NewReader(grFixture(t)))
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.store.Import(context.Background(), userID, rows)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// bookTitled is the id of userID's one book listed under title.
func bookTitled(t *testing.T, f *fixture, userID int64, title string) int64 {
	t.Helper()
	items, err := f.store.List(context.Background(), userID, books.ListQuery{Shelf: books.ShelfAll})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Title == title {
			return it.ID
		}
	}
	t.Fatalf("no book %q in %+v", title, items)
	return 0
}

const goodOmens = "Good Omens: The Nice and Accurate Prophecies of Agnes Nutter, Witch"

func TestImportAddsTheBooksWithTheirReadings(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	uid := f.alice.ID
	res := importFixture(t, f, uid)
	if res.Imported != 6 || !reflect.DeepEqual(res.Skipped, []string{goodOmens}) {
		t.Errorf("result = %+v, want 6 imported and the second Good Omens skipped", res)
	}
	counts, err := f.store.ShelfCounts(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if want := (map[books.Shelf]int{books.ShelfRead: 2, books.ShelfReading: 1, books.ShelfWant: 3, books.ShelfAll: 6}); !reflect.DeepEqual(counts, want) {
		t.Errorf("shelf counts = %v, want %v", counts, want)
	}

	lw := getBook(t, f, uid, bookTitled(t, f, uid, "Leviathan Wakes"))
	if lw.SeriesName != "The Expanse" || lw.SeriesNumber != "1" || lw.ISBN != "9780316129084" || lw.Rating != 4 ||
		lw.Review != "Great fun.\n\nLoved Miller & Holden." || !reflect.DeepEqual(lw.Tags, []string{"sf", "space"}) {
		t.Errorf("Leviathan Wakes = %+v", lw)
	}
	if want := time.Date(2024, 2, 1, 0, 0, 0, 0, time.Local); !lw.AddedAt.Equal(want) {
		t.Errorf("added at %v, want the local start of Date Added, %v", lw.AddedAt, want)
	}
	if want := time.Date(2024, 3, 14, 0, 0, 0, 0, time.Local); !lw.UpdatedAt.Equal(want) {
		t.Errorf("updated at %v, want its Date Read, %v", lw.UpdatedAt, want)
	}
	rs, err := f.store.Readings(ctx, uid, lw.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].Status != books.StatusFinished || rs[0].FinishedOn != "2024-03-14" || rs[0].Format != "paper" ||
		rs[1].Status != books.StatusFinished || rs[1].FinishedOn != "" || rs[1].StartedOn != "" || rs[1].Format != "" {
		t.Errorf("readings = %+v, want the dated paper one, then one with no dates (Read Count 2)", rs)
	}

	p := getBook(t, f, uid, bookTitled(t, f, uid, "Piranesi"))
	if p.Shelf != books.ShelfReading || p.Latest.StartedOn != "2025-01-05" || p.Latest.Format != "ebook" || p.Rating != 0 {
		t.Errorf("Piranesi = shelf %s, %+v, rating %d; want reading since its Date Added, as an ebook, no rating", p.Shelf, p.Latest, p.Rating)
	}
	ij := getBook(t, f, uid, bookTitled(t, f, uid, "Infinite Jest"))
	if ij.Shelf != books.ShelfWant || !reflect.DeepEqual(ij.Tags, []string{"did-not-finish"}) {
		t.Errorf("Infinite Jest = %s %v, want Want to read, tagged with its own shelf", ij.Shelf, ij.Tags)
	}
	if items, err := f.store.List(ctx, f.bob.ID, books.ListQuery{Shelf: books.ShelfAll}); err != nil || len(items) != 0 {
		t.Errorf("Bob's books = %+v, %v; want none", items, err)
	}
}

func TestImportedBooksAreSearchable(t *testing.T) {
	f := newFixture(t)
	importFixture(t, f, f.alice.ID)
	for q, want := range map[string]books.MatchIn{"holden": books.MatchReview, "fagles": books.MatchBook, "expanse": books.MatchBook} {
		items, err := f.store.List(context.Background(), f.alice.ID, books.ListQuery{Shelf: books.ShelfAll, Q: q})
		if err != nil || len(items) != 1 || items[0].Match != want {
			t.Errorf("search %q = %+v, %v; want one book matched in %q", q, items, err, want)
		}
	}
}

func TestImportSkipsBooksAlreadyInTheLibrary(t *testing.T) {
	f := newFixture(t)
	uid := f.alice.ID
	byISBN := onShelf("Leviathan Wakes: The Expanse 1", books.ShelfRead) // another title, same ISBN
	byISBN.ISBN = "978-0-316-12908-4"
	addBook(t, f, uid, byISBN)
	byName := onShelf("GOOD OMENS: the nice and accurate prophecies of agnes nutter, witch", books.ShelfWant)
	byName.Authors = "terry pratchett & neil gaiman"
	addBook(t, f, uid, byName)
	// A row with an ISBN matches by ISBN alone: the same title with
	// another ISBN is another edition, and is imported.
	otherEdition := onShelf("The Remains of the Day", books.ShelfWant)
	otherEdition.Authors, otherEdition.ISBN = "Kazuo Ishiguro", "9780571258246"
	addBook(t, f, uid, otherEdition)
	addBook(t, f, f.bob.ID, onShelf("Piranesi", books.ShelfWant)) // Bob's don't count

	res := importFixture(t, f, uid)
	if res.Imported != 4 || !reflect.DeepEqual(res.Skipped, []string{"Leviathan Wakes", goodOmens, goodOmens}) {
		t.Errorf("result = %+v, want 4 imported, Leviathan Wakes and both Good Omens skipped", res)
	}
	// A book typed in without an ISBN matches a row with one by its
	// title and authors.
	f2 := newFixture(t)
	bare := onShelf("the remains of the day", books.ShelfRead)
	bare.Authors = "KAZUO  ISHIGURO."
	addBook(t, f2, f2.alice.ID, bare)
	if res := importFixture(t, f2, f2.alice.ID); len(res.Skipped) != 2 || res.Skipped[0] != "The Remains of the Day" {
		t.Errorf("result = %+v, want The Remains of the Day skipped (and the second Good Omens)", res)
	}

	again := importFixture(t, f, uid)
	if again.Imported != 0 || len(again.Skipped) != 7 {
		t.Errorf("importing the same file again = %+v, want everything skipped", again)
	}
}

func TestImportIsAllOrNothing(t *testing.T) {
	f := newFixture(t)
	rows := []books.ImportBook{
		{BookInput: books.BookInput{Title: "Fine"}, Shelf: books.ShelfWant},
		{BookInput: books.BookInput{Title: "Broken"}, Shelf: books.ShelfWant, Rating: 9}, // the schema refuses it
	}
	if _, err := f.store.Import(context.Background(), f.alice.ID, rows); err == nil {
		t.Fatal("import with a bad row succeeded, want an error")
	}
	if counts, _ := f.store.ShelfCounts(context.Background(), f.alice.ID); counts[books.ShelfAll] != 0 {
		t.Errorf("%d books after a failed import, want none", counts[books.ShelfAll])
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — the package doesn't build: `f.store.Import undefined`, `undefined: books.ImportResult`.

- [ ] **Step 3: Write the import**

Create `internal/apps/books/import.go`:

```go
package books

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// ImportResult is what an import did: how many books it added, and the
// titles of the ones it skipped as already there, in file order.
type ImportResult struct {
	Imported int
	Skipped  []string
}

// Import adds rows (ParseGoodreads) to userID's library in one
// transaction, so it is all or nothing (spec "Import (B5)"). A row is
// skipped when the library — or an earlier row — already has the book:
// the same ISBN-13; or, when either of the two has no ISBN, the same
// title and authors ignoring case, spaces and punctuation (decided 2026-10-10 while planning B5,
// so a book typed in without an ISBN isn't imported a second time).
// Imported books reach books_search through its insert trigger, like any
// other book.
func (st *Store) Import(ctx context.Context, userID int64, rows []ImportBook) (ImportResult, error) {
	var res ImportResult
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("books: begin import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	have, err := libraryKeys(ctx, tx, userID)
	if err != nil {
		return res, err
	}
	now := formatTime(st.now())
	for _, b := range rows {
		keys := bookKeys(b.BookInput)
		if have.has(keys, b.ISBN != "") {
			res.Skipped = append(res.Skipped, b.Title)
			continue
		}
		have.add(keys, b.ISBN != "")
		if err := importBook(ctx, tx, userID, b, now); err != nil {
			return ImportResult{}, err
		}
		res.Imported++
	}
	if err := tx.Commit(); err != nil {
		return ImportResult{}, fmt.Errorf("books: commit import: %w", err)
	}
	return res, nil
}

// bookKey is what a duplicate is recognised by: an ISBN-13, and a title
// with its authors, ignoring case, spaces and punctuation — Goodreads
// writes "James S.A. Corey" where Open Library has "James S. A. Corey".
type bookKey struct{ isbn, name string }

func bookKeys(in BookInput) bookKey {
	return bookKey{isbn: in.ISBN, name: looseText(in.Title) + "\x00" + looseText(in.Authors)}
}

// looseText is s lowercased, with only its letters and digits.
func looseText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

// keySet is the books already there: their ISBNs, the names of them all,
// and the names of those with no ISBN.
type keySet struct{ isbns, names, bare map[string]bool }

func (k keySet) has(b bookKey, hasISBN bool) bool {
	if hasISBN {
		return k.isbns[b.isbn] || k.bare[b.name]
	}
	return k.names[b.name]
}

func (k keySet) add(b bookKey, hasISBN bool) {
	k.names[b.name] = true
	if hasISBN {
		k.isbns[b.isbn] = true
	} else {
		k.bare[b.name] = true
	}
}

// libraryKeys is the keySet of every book userID has. The rows are closed
// before the import writes: the database has one connection.
func libraryKeys(ctx context.Context, tx *sql.Tx, userID int64) (keySet, error) {
	have := keySet{isbns: map[string]bool{}, names: map[string]bool{}, bare: map[string]bool{}}
	rows, err := tx.QueryContext(ctx,
		`SELECT title, authors, COALESCE(isbn13, '') FROM books_books WHERE user_id = ?`, userID)
	if err != nil {
		return have, fmt.Errorf("books: import library: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var in BookInput
		if err := rows.Scan(&in.Title, &in.Authors, &in.ISBN); err != nil {
			return have, fmt.Errorf("books: scan import library: %w", err)
		}
		have.add(bookKeys(in), in.ISBN != "")
	}
	if err := rows.Err(); err != nil {
		return have, fmt.Errorf("books: import library: %w", err)
	}
	return have, nil
}

// importBook writes one row: the book, its readings and its tags. Earlier
// reads go in first, so the main reading is the latest and decides the
// shelf (latestJoin breaks the created_at tie by id).
func importBook(ctx context.Context, tx *sql.Tx, userID int64, b ImportBook, now string) error {
	added, changed := now, now
	if b.AddedOn != "" {
		added, changed = formatTime(localDay(b.AddedOn)), formatTime(localDay(max(b.AddedOn, b.FinishedOn)))
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO books_books (user_id, title, subtitle, authors, year, pages, isbn13,
			series_name, series_number, rating, review, added_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, b.Title, b.Subtitle, b.Authors, nullInt(b.Year), nullInt(b.Pages), nullText(b.ISBN),
		b.SeriesName, b.SeriesNumber, nullInt(b.Rating), b.Review, added, changed)
	if err != nil {
		return fmt.Errorf("books: import %q: %w", b.Title, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("books: import %q: %w", b.Title, err)
	}
	for range b.EarlierReads {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO books_readings (book_id, status, created_at) VALUES (?, 'finished', ?)`, id, now); err != nil {
			return fmt.Errorf("books: import an earlier reading: %w", err)
		}
	}
	switch b.Shelf {
	case ShelfRead:
		_, err = tx.ExecContext(ctx, `INSERT INTO books_readings (book_id, status, format, finished_on, created_at)
			VALUES (?, 'finished', ?, ?, ?)`, id, nullText(b.Format), nullText(b.FinishedOn), now)
	case ShelfReading:
		_, err = tx.ExecContext(ctx, `INSERT INTO books_readings (book_id, status, format, started_on, created_at)
			VALUES (?, 'reading', ?, ?, ?)`, id, nullText(b.Format), nullText(b.StartedOn), now)
	}
	if err != nil {
		return fmt.Errorf("books: import a reading: %w", err)
	}
	return linkTags(ctx, tx, userID, id, b.Tags)
}

// localDay is the start of a YYYY-MM-DD day in the server's zone, which
// is where the suite's days are (#424). day is already a valid date.
func localDay(day string) time.Time {
	t, _ := time.ParseInLocation(dayLayout, day, time.Local)
	return t
}
```

- [ ] **Step 4: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./internal/apps/books/
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./internal/apps/books/
```
Expected: all pass, the older tests too.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): import a Goodreads library in one transaction (#491)"
```

---

### Task 3: Cover backfill job

Migration 0007 adds `cover_checked_at`. The job looks up covers by ISBN for books with none and no look yet, a few per run (default 2), and shows in /admin/jobs with Run now because it is an `app.Scheduler` job. Removing a cover marks the book checked; a new ISBN clears the mark (default 12).

**Files:**
- Create: `internal/apps/books/migrations/0007_cover_checked.sql`, `internal/apps/books/backfill.go`
- Modify: `internal/apps/books/books.go`, `internal/apps/books/cover.go`, `internal/apps/books/library.go`
- Test: `internal/apps/books/backfill_test.go`; `internal/apps/books/export_test.go` (a pause hook), `internal/apps/books/openlibrary_test.go` (covers by ISBN in the fake)

**Interfaces:**
- Consumes: `OpenLibrary` (`Web`, `Covers`), `coverTimeout`, `coverType`, `MaxCoverBytes`, `ISBN13`, `Store.SetCover`/`RemoveCover`/`Update`, `CoverFromOL` (existing); `UseOpenLibraryForTest`, `DBForTest`, `fakeOpenLibrary`, `newOpenLibrary`, `onePNG`, `coverSource`, `add`, `titled` (existing test code).
- Produces:
  - `var ErrNoCover`; `(*OpenLibrary).CoverByISBN(ctx, isbn string) (contentType string, data []byte, err error)` — `ErrNoCover` for a 404 or a non-image, `ErrInvalid` for a bad ISBN
  - `type CoverCandidate struct { UserID, ID int64; ISBN string }`; `(*Store).CoverCandidates(ctx, limit int) ([]CoverCandidate, error)`, `FillCover(ctx, userID, id int64, contentType string, data []byte) error`, `MarkCoverChecked(ctx, userID, id int64) error`
  - `(*App).BackfillCovers(ctx, batch int) (int, error)`; `(*App).Jobs(app.Deps) []app.Job` — one job, "fetch book covers", every 5 minutes
  - `App.pause func(context.Context, time.Duration) error` (default `sleep`); test hook `(*App).NoPauseForTest(count *int)`
  - test helpers `newBooksApp(t) (*server, *books.App, *int)`, `withISBN`, `checkedAt`, `runBackfill`, `getBookAt`

- [ ] **Step 1: Write the failing tests**

The fake Open Library learns three ISBNs; any other is its mux's 404, which is how Open Library says "no cover" with `default=false`.

In `internal/apps/books/openlibrary_test.go`, replace:

```go
	}
	mux.HandleFunc("GET /b/id/10226290-S.jpg", png)
	mux.HandleFunc("GET /b/id/10226290-M.jpg", png)
	mux.HandleFunc("GET /b/id/666-M.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("<html><body>not an image</body></html>"))
```

with:

```go
	}
	mux.HandleFunc("GET /b/id/10226290-S.jpg", png)
	mux.HandleFunc("GET /b/id/10226290-M.jpg", png)
	// Covers by ISBN (B5): one found, one down, one that isn't an image;
	// any other ISBN is the mux's own 404, Open Library's "no cover".
	mux.HandleFunc("GET /b/isbn/9781635575637-M.jpg", png)
	mux.HandleFunc("GET /b/isbn/9780306406157-M.jpg", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	})
	mux.HandleFunc("GET /b/isbn/9780547928227-M.jpg", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>not an image</body></html>"))
	})
	mux.HandleFunc("GET /b/id/666-M.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("<html><body>not an image</body></html>"))
```

In `internal/apps/books/export_test.go`, replace:

```go
package books

import (
	"database/sql"
	"net"
	"net/netip"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)
```

with:

```go
package books

import (
	"context"
	"database/sql"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)
```

In `internal/apps/books/export_test.go`, replace:

```go
	}
	return b.String()
}
```

with:

```go
	}
	return b.String()
}

// NoPauseForTest makes the cover backfill's pause between requests
// instant, counting how often it is asked for.
func (a *App) NoPauseForTest(count *int) {
	a.pause = func(context.Context, time.Duration) error { *count++; return nil }
}
```

Create `internal/apps/books/backfill_test.go`:

```go
package books_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// newBooksApp is newServer with the app itself, its Open Library client
// pointed at fakeOpenLibrary and the backfill's pause made instant
// (counted in pauses).
func newBooksApp(t *testing.T) (*server, *books.App, *int) {
	t.Helper()
	a := books.New()
	s := apptest.NewServer(t, a, books.NewStore)
	a.UseOpenLibraryForTest(fakeOpenLibrary(t).URL)
	pauses := new(int)
	a.NoPauseForTest(pauses)
	return s, a, pauses
}

// withISBN adds one of userID's books with an ISBN to read.
func withISBN(t *testing.T, s *server, userID int64, title, isbn string) int64 {
	t.Helper()
	nb := titled(title, "", books.ShelfWant)
	nb.ISBN = isbn
	return add(t, s, userID, nb)
}

// checkedAt is a book's cover_checked_at, "" for NULL.
func checkedAt(t *testing.T, s *server, id int64) string {
	t.Helper()
	var at sql.NullString
	if err := s.Store.DBForTest().QueryRow(`SELECT cover_checked_at FROM books_books WHERE id = ?`, id).Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at.String
}

func runBackfill(t *testing.T, a *books.App) error {
	t.Helper()
	jobs := a.Jobs(app.Deps{})
	if len(jobs) != 1 || jobs[0].Name != "fetch book covers" {
		t.Fatalf("jobs = %+v, want the cover backfill", jobs)
	}
	return jobs[0].Run(context.Background())
}

func TestBooksRegistersTheCoverBackfill(t *testing.T) {
	jobs := books.New().Jobs(app.Deps{}) // callable before Mount
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1", len(jobs))
	}
	j := jobs[0]
	if j.Every != 5*time.Minute || j.Description == "" || j.Run == nil {
		t.Errorf("job = %q every %v, description %q", j.Name, j.Every, j.Description)
	}
}

func TestBackfillFetchesMissingCoversByISBN(t *testing.T) {
	s, a, pauses := newBooksApp(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	found := withISBN(t, s, uid, "Piranesi", "9781635575637")
	missing := withISBN(t, s, uid, "Leviathan Wakes", "9780316129084")
	none := add(t, s, uid, titled("No ISBN", "", books.ShelfWant))
	uploaded := withISBN(t, s, uid, "Uploaded", "9781635575637")
	if err := s.Store.SetCover(ctx, uid, uploaded, "image/gif", []byte("GIF89a"), books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	notImage := withISBN(t, s, s.Bob.User.ID, "The Hobbit", "9780547928227") // any user's
	before := getBookAt(t, s, uid, found).UpdatedAt
	s.Clock.Advance(time.Hour)

	if err := runBackfill(t, a); err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.Cover(ctx, uid, found)
	if err != nil || !bytes.Equal(c.Bytes, onePNG) || coverSource(t, s, found) != books.CoverFromOL {
		t.Errorf("Piranesi's cover = %d bytes, %v, source %q; want Open Library's", len(c.Bytes), err, coverSource(t, s, found))
	}
	if checkedAt(t, s, found) != "" {
		t.Error("a found cover marked the book checked")
	}
	if after := getBookAt(t, s, uid, found).UpdatedAt; !after.Equal(before) {
		t.Errorf("updated_at moved from %v to %v: a background cover is not the person's change", before, after)
	}
	for name, id := range map[string]int64{"a 404": missing, "not an image": notImage} {
		if checkedAt(t, s, id) == "" {
			t.Errorf("%s: book not marked checked", name)
		}
	}
	if checkedAt(t, s, none) != "" || coverSource(t, s, uploaded) != books.CoverUpload {
		t.Error("the backfill touched a book with no ISBN, or one with a cover")
	}
	if *pauses != 2 {
		t.Errorf("%d pauses for three lookups, want one between each", *pauses)
	}
	if cs, err := s.Store.CoverCandidates(ctx, 25); err != nil || len(cs) != 0 {
		t.Errorf("candidates after a run = %+v, %v; want none", cs, err)
	}
}

func TestBackfillStopsWhenOpenLibraryFails(t *testing.T) {
	s, a, _ := newBooksApp(t)
	down := withISBN(t, s, s.Alice.User.ID, "Down", "9780306406157")
	if err := runBackfill(t, a); err == nil {
		t.Error("run with Open Library down = nil, want its error on the jobs page")
	}
	if checkedAt(t, s, down) != "" {
		t.Error("a failed lookup marked the book checked; it should be tried again")
	}
}

func TestBackfillLeavesRemovedCoversAlone(t *testing.T) {
	s, _, _ := newBooksApp(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	id := withISBN(t, s, uid, "Piranesi", "9781635575637")
	if err := s.Store.SetCover(ctx, uid, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.RemoveCover(ctx, uid, id); err != nil {
		t.Fatal(err)
	}
	if checkedAt(t, s, id) == "" {
		t.Fatal("removing a cover didn't mark the book checked; the backfill would bring it back")
	}
	in := getBookAt(t, s, uid, id).BookInput
	if err := s.Store.Update(ctx, uid, id, in); err != nil {
		t.Fatal(err)
	}
	if checkedAt(t, s, id) == "" {
		t.Error("saving the same ISBN cleared cover_checked_at")
	}
	in.ISBN = "9780316129084"
	if err := s.Store.Update(ctx, uid, id, in); err != nil {
		t.Fatal(err)
	}
	if checkedAt(t, s, id) != "" {
		t.Error("a new ISBN kept cover_checked_at; the backfill should look again")
	}
}

func TestCoverByISBNSaysWhenThereIsNone(t *testing.T) {
	ol := newOpenLibrary(t)
	ctx := context.Background()
	if ct, data, err := ol.CoverByISBN(ctx, "9781635575637"); err != nil || ct != "image/png" || !bytes.Equal(data, onePNG) {
		t.Errorf("found = %q, %d bytes, %v", ct, len(data), err)
	}
	for _, isbn := range []string{"9780316129084", "9780547928227"} {
		if _, _, err := ol.CoverByISBN(ctx, isbn); !errors.Is(err, books.ErrNoCover) {
			t.Errorf("%s = %v, want ErrNoCover", isbn, err)
		}
	}
	if _, _, err := ol.CoverByISBN(ctx, "9780306406157"); err == nil || errors.Is(err, books.ErrNoCover) {
		t.Errorf("a 503 = %v, want an error that isn't ErrNoCover", err)
	}
	if _, _, err := ol.CoverByISBN(ctx, "../etc"); !errors.Is(err, books.ErrInvalid) {
		t.Errorf("a bad ISBN = %v, want ErrInvalid", err)
	}
}

// getBookAt is one of userID's books through the server's store.
func getBookAt(t *testing.T, s *server, userID, id int64) books.Book {
	t.Helper()
	b, err := s.Store.Get(context.Background(), userID, id)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — the package doesn't build: `a.NoPauseForTest undefined`, `undefined: books.ErrNoCover`, `a.Jobs undefined`, `s.Store.CoverCandidates undefined`.

- [ ] **Step 3: Add the column, the lookup and the job**

Create `internal/apps/books/migrations/0007_cover_checked.sql`:

```sql
-- B5 (spec "Data model": cover_checked_at): when the cover backfill job,
-- or Find cover, last looked for a book's cover on Open Library and found
-- none — or when its cover was removed by hand — so the job doesn't try
-- the book again and again. NULL means it is worth a look.
ALTER TABLE books_books ADD COLUMN cover_checked_at TEXT;
```

Create `internal/apps/books/backfill.go`:

```go
package books

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// The cover backfill (spec "Covers after import"), decided 2026-10-10
// while planning B5: Open Library allows 100 cover lookups by ISBN per
// five minutes from one address, so a run every five minutes takes 25
// books, a second apart — about 300 an hour, well inside the limit even
// with Find cover clicked meanwhile.
const (
	coverBackfillEvery = 5 * time.Minute
	coverBackfillBatch = 25
	coverBackfillPause = time.Second
)

// ErrNoCover is Open Library having no cover for a book: a 404, or an
// answer that isn't a cover image.
var ErrNoCover = errors.New("books: open library has no cover")

// CoverByISBN fetches the medium cover Open Library has for an ISBN-13.
// default=false makes a missing cover a 404, which is ErrNoCover; any
// other failure (a timeout, a 5xx) is an error worth trying again.
func (o *OpenLibrary) CoverByISBN(ctx context.Context, isbn string) (string, []byte, error) {
	if v, ok := ISBN13(isbn); !ok || v != isbn {
		return "", nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, coverTimeout)
	defer cancel()
	res, err := o.Web.Get(ctx, fmt.Sprintf("%s/b/isbn/%s-M.jpg?default=false", o.Covers, isbn),
		webfetch.GetOptions{Accept: "image/*", MaxBytes: MaxCoverBytes})
	if res != nil && res.Status == http.StatusNotFound {
		return "", nil, ErrNoCover
	}
	if err != nil {
		return "", nil, err
	}
	ct := http.DetectContentType(res.Body)
	if !coverType(ct) {
		return "", nil, ErrNoCover
	}
	return ct, res.Body, nil
}

// CoverCandidate is a book the backfill should look for a cover for.
type CoverCandidate struct {
	UserID, ID int64
	ISBN       string
}

// CoverCandidates is up to limit books, any user's, with an ISBN, no
// cover, and no look for one yet — oldest first.
func (st *Store) CoverCandidates(ctx context.Context, limit int) ([]CoverCandidate, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT b.user_id, b.id, b.isbn13 FROM books_books b
		 WHERE b.isbn13 IS NOT NULL AND b.cover_checked_at IS NULL
		   AND NOT EXISTS (SELECT 1 FROM books_covers c WHERE c.book_id = b.id)
		 ORDER BY b.id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("books: cover candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []CoverCandidate
	for rows.Next() {
		var c CoverCandidate
		if err := rows.Scan(&c.UserID, &c.ID, &c.ISBN); err != nil {
			return nil, fmt.Errorf("books: scan cover candidate: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: cover candidates: %w", err)
	}
	return out, nil
}

// FillCover stores a cover the backfill found, unless the book has one by
// now (an upload wins). It leaves updated_at alone: a background fetch
// isn't a change the person made, so it doesn't reorder their shelves.
func (st *Store) FillCover(ctx context.Context, userID, id int64, contentType string, data []byte) error {
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO books_covers (book_id, content_type, bytes, source, fetched_at)
		SELECT id, ?, ?, 'ol', ? FROM books_books WHERE id = ? AND user_id = ?
		ON CONFLICT (book_id) DO NOTHING`,
		contentType, data, formatTime(st.now()), id, userID); err != nil {
		return fmt.Errorf("books: fill cover: %w", err)
	}
	return nil
}

// MarkCoverChecked records that Open Library had no cover for a book, so
// the backfill leaves it alone.
func (st *Store) MarkCoverChecked(ctx context.Context, userID, id int64) error {
	if _, err := st.db.ExecContext(ctx, `UPDATE books_books SET cover_checked_at = ? WHERE id = ? AND user_id = ?`,
		formatTime(st.now()), id, userID); err != nil {
		return fmt.Errorf("books: mark cover checked: %w", err)
	}
	return nil
}

// BackfillCovers looks for up to batch missing covers by ISBN, pausing
// between requests. A miss marks the book checked; any other failure
// stops the run and is its error — Open Library is down, so the rest can
// wait for the next run. It returns how many covers it stored.
func (a *App) BackfillCovers(ctx context.Context, batch int) (int, error) {
	cs, err := a.store.CoverCandidates(ctx, batch)
	if err != nil {
		return 0, err
	}
	stored := 0
	for i, c := range cs {
		if i > 0 {
			if err := a.pause(ctx, coverBackfillPause); err != nil {
				return stored, err
			}
		}
		ct, data, err := a.ol.CoverByISBN(ctx, c.ISBN)
		switch {
		case errors.Is(err, ErrNoCover) || errors.Is(err, ErrInvalid):
			err = a.store.MarkCoverChecked(ctx, c.UserID, c.ID)
		case err == nil:
			if err = a.store.FillCover(ctx, c.UserID, c.ID, ct, data); err == nil {
				stored++
			}
		}
		if err != nil {
			return stored, err
		}
	}
	return stored, nil
}

// sleep waits d, or until ctx is done: the backfill's pause between
// requests. Tests replace it (App.pause).
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Jobs implements app.Scheduler: the cover backfill shows in /admin/jobs
// with Run now. RegisterJobs runs after Mount, and the closure reads the
// app only when it runs.
func (a *App) Jobs(app.Deps) []app.Job {
	return []app.Job{{
		Name:        "fetch book covers",
		Description: "Looks up covers on Open Library for books with an ISBN and no cover, a few at a time — books imported from Goodreads, say.",
		Every:       coverBackfillEvery,
		Run: func(ctx context.Context) error {
			n, err := a.BackfillCovers(ctx, coverBackfillBatch)
			if n > 0 {
				a.deps.Log.Info("books stored covers", "count", n)
			}
			return err
		},
	}}
}
```

In `internal/apps/books/books.go`, replace:

```go
package books

import (
	"embed"
	"io/fs"
	"net/http"
```

with:

```go
package books

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
```

In `internal/apps/books/books.go`, replace:

```go
	// thumbSem bounds concurrent thumbnail fetches: a results page asks for
	// up to ten at once, and Open Library is a free service.
	thumbSem chan struct{}
}

// New returns the app for registration in cmd/onsuite.
```

with:

```go
	// thumbSem bounds concurrent thumbnail fetches: a results page asks for
	// up to ten at once, and Open Library is a free service.
	thumbSem chan struct{}
	// pause waits between the cover backfill's requests (sleep; tests
	// make it instant).
	pause func(context.Context, time.Duration) error
}

// New returns the app for registration in cmd/onsuite.
```

In `internal/apps/books/books.go`, replace:

```go
	})
	a.ol = &OpenLibrary{Web: a.web, Base: "https://openlibrary.org", Covers: "https://covers.openlibrary.org", Timeout: 5 * time.Second}
	a.thumbSem = make(chan struct{}, 4)
	r.HandleFunc("GET /{$}", a.index)
	r.HandleFunc("GET /b/{id}", a.book)
	r.HandleFunc("GET /new", a.newForm)
```

with:

```go
	})
	a.ol = &OpenLibrary{Web: a.web, Base: "https://openlibrary.org", Covers: "https://covers.openlibrary.org", Timeout: 5 * time.Second}
	a.thumbSem = make(chan struct{}, 4)
	a.pause = sleep
	r.HandleFunc("GET /{$}", a.index)
	r.HandleFunc("GET /b/{id}", a.book)
	r.HandleFunc("GET /new", a.newForm)
```

In `internal/apps/books/cover.go`, replace:

```go
	return c, nil
}

// RemoveCover drops a book's cover, so it shows its spine again.
func (st *Store) RemoveCover(ctx context.Context, userID, id int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
```

with:

```go
	return c, nil
}

// RemoveCover drops a book's cover, so it shows its spine again, and
// marks it checked so the cover backfill doesn't bring it back.
func (st *Store) RemoveCover(ctx context.Context, userID, id int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
```

In `internal/apps/books/cover.go`, replace:

```go
	if _, err := tx.ExecContext(ctx, `DELETE FROM books_covers WHERE book_id = ?`, id); err != nil {
		return fmt.Errorf("books: remove cover: %w", err)
	}
	return tx.Commit()
}
```

with:

```go
	if _, err := tx.ExecContext(ctx, `DELETE FROM books_covers WHERE book_id = ?`, id); err != nil {
		return fmt.Errorf("books: remove cover: %w", err)
	}
	// A cover taken off by hand stays off: the backfill skips the book.
	if _, err := tx.ExecContext(ctx, `UPDATE books_books SET cover_checked_at = ? WHERE id = ?`,
		formatTime(st.now()), id); err != nil {
		return fmt.Errorf("books: remove cover: %w", err)
	}
	return tx.Commit()
}
```

In `internal/apps/books/library.go`, replace:

```go
	return b, nil
}

// Update replaces a book's details. Readings and tags are untouched.
func (st *Store) Update(ctx context.Context, userID, id int64, in BookInput) error {
	in = in.Normalize()
	if errs := in.Validate(); errs != nil {
```

with:

```go
	return b, nil
}

// Update replaces a book's details. Readings and tags are untouched. A
// new ISBN clears cover_checked_at, so the backfill looks again.
func (st *Store) Update(ctx context.Context, userID, id int64, in BookInput) error {
	in = in.Normalize()
	if errs := in.Validate(); errs != nil {
```

In `internal/apps/books/library.go`, replace:

```go
	res, err := st.db.ExecContext(ctx, `
		UPDATE books_books
		   SET title = ?, subtitle = ?, authors = ?, year = ?, pages = ?, isbn13 = ?,
		       series_name = ?, series_number = ?, description = ?, updated_at = ?
		 WHERE id = ? AND user_id = ?`,
		in.Title, in.Subtitle, in.Authors, nullInt(in.Year), nullInt(in.Pages), nullText(in.ISBN),
		in.SeriesName, in.SeriesNumber, in.Description, formatTime(st.now()), id, userID)
	if err != nil {
		return fmt.Errorf("books: update: %w", err)
	}
```

with:

```go
	res, err := st.db.ExecContext(ctx, `
		UPDATE books_books
		   SET title = ?, subtitle = ?, authors = ?, year = ?, pages = ?, isbn13 = ?,
		       series_name = ?, series_number = ?, description = ?, updated_at = ?,
		       cover_checked_at = CASE WHEN isbn13 IS ? THEN cover_checked_at END
		 WHERE id = ? AND user_id = ?`,
		in.Title, in.Subtitle, in.Authors, nullInt(in.Year), nullInt(in.Pages), nullText(in.ISBN),
		in.SeriesName, in.SeriesNumber, in.Description, formatTime(st.now()), nullText(in.ISBN), id, userID)
	if err != nil {
		return fmt.Errorf("books: update: %w", err)
	}
```

- [ ] **Step 4: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./internal/apps/books/
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./internal/apps/books/
```
Expected: all pass. `TestEveryUserColumnCascadesFromUsers` (cmd/onsuite) is unaffected: the new column isn't a user reference.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): fetch missing covers in the background (#491)"
```

---

### Task 4: Find cover

A ⋯ menu item, shown while the book has no cover (default 11), that runs the lookup now: by ISBN, then by title and first author. It goes through `act`, so a refusal is the banner over the panes (200 fragment for htmx, 422 page without JavaScript) and success redraws the panes with the cover.

**Files:**
- Create: `internal/apps/books/findcover.go`
- Modify: `internal/apps/books/books.go`, `internal/apps/books/templates/panes.partial.html`
- Test: `internal/apps/books/findcover_test.go`; `internal/apps/books/openlibrary_test.go` (search by title)

**Interfaces:**
- Consumes: `CoverByISBN`, `ErrNoCover`, `MarkCoverChecked` (Task 3); `OpenLibrary.Cover`, `olDoc`, `maxJSONBytes`, `act`, `Refusal`, `Store.Get`/`SetCover` (existing); `newBooksApp`, `withISBN`, `checkedAt` (Task 3).
- Produces: `(*OpenLibrary).FindCoverID(ctx, title, authors string) (int64, error)`; `POST /books/find-cover/{id}`; test helper `findCover(t, s, id) *htmlassert.Doc`.

- [ ] **Step 1: Write the failing tests**

In `internal/apps/books/openlibrary_test.go`, replace:

```go
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search.json", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("q") {
		case "broken":
			http.Error(w, "down", http.StatusServiceUnavailable)
```

with:

```go
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search.json", func(w http.ResponseWriter, r *http.Request) {
		// Find cover's search, by title (B5): "Piranesi" finds the usual
		// answer, "Broken" fails, anything else finds nothing.
		switch r.URL.Query().Get("title") {
		case "":
		case "Piranesi":
			_, _ = w.Write([]byte(searchJSON))
			return
		case "Broken":
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		default:
			_, _ = w.Write([]byte(`{"docs":[{"key":"/works/OL9W","title":"No cover"}]}`))
			return
		}
		switch r.URL.Query().Get("q") {
		case "broken":
			http.Error(w, "down", http.StatusServiceUnavailable)
```

Create `internal/apps/books/findcover_test.go`:

```go
package books_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func findCover(t *testing.T, s *server, id int64) *htmlassert.Doc {
	t.Helper()
	rec := s.PostHX(t, s.Alice, fmt.Sprintf("/books/find-cover/%d", id), url.Values{"shelf": {"want"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("Find cover = %d; body: %s", rec.Code, rec.Body.String())
	}
	return htmlassert.Parse(t, rec.Body.String())
}

func TestFindCoverIsInTheMenuOfABookWithout(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	id := add(t, s, uid, titled("Piranesi", "", books.ShelfWant))
	path := fmt.Sprintf("/books/b/%d?shelf=want", id)
	doc := s.Get(t, s.Alice, path)
	btn := doc.MustHave(".books-menu button[hx-post]")
	if got, _ := htmlassert.Attr(btn, "hx-post"); got != fmt.Sprintf("/books/find-cover/%d", id) {
		t.Errorf("first menu button posts to %q, want Find cover", got)
	}
	if got := attr(t, doc, ".books-menu form", "action"); got != fmt.Sprintf("/books/find-cover/%d", id) {
		t.Errorf("its form posts to %q without JavaScript", got)
	}
	if err := s.Store.SetCover(context.Background(), uid, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	for _, text := range texts(s.Get(t, s.Alice, path), ".books-menu button") {
		if text == "Find cover" {
			t.Error("a book with a cover still offers Find cover")
		}
	}
}

func TestFindCoverByISBNThenByTitle(t *testing.T) {
	s, _, _ := newBooksApp(t)
	uid := s.Alice.User.ID
	byISBN := withISBN(t, s, uid, "Anything", "9781635575637")
	byTitle := add(t, s, uid, titled("Piranesi", "Susanna Clarke", books.ShelfWant))
	// An ISBN Open Library has no cover for falls back to the title.
	fallback := withISBN(t, s, uid, "Piranesi", "9780316129084")
	for name, id := range map[string]int64{"by ISBN": byISBN, "by title": byTitle, "ISBN, then title": fallback} {
		doc := findCover(t, s, id)
		c, err := s.Store.Cover(context.Background(), uid, id)
		if err != nil || !bytes.Equal(c.Bytes, onePNG) || coverSource(t, s, id) != books.CoverFromOL {
			t.Errorf("%s: cover = %d bytes, %v", name, len(c.Bytes), err)
		}
		doc.MustHave("#books-book img.books-cover")
		doc.MustNotHave(".books-banner")
	}
}

func TestFindCoverSaysWhenItCant(t *testing.T) {
	s, _, _ := newBooksApp(t)
	uid := s.Alice.User.ID
	nothing := add(t, s, uid, titled("Nothing like it", "", books.ShelfWant))
	broken := add(t, s, uid, titled("Broken", "", books.ShelfWant))
	tests := []struct {
		id   int64
		want string
	}{
		{nothing, "Open Library has no cover for this book. You can add one with Edit details."},
		{broken, "Open Library didn't answer. Try again in a minute."},
	}
	for _, tt := range tests {
		doc := findCover(t, s, tt.id)
		if got := htmlassert.Text(doc.MustHave(".books-banner")); got != tt.want {
			t.Errorf("banner = %q, want %q", got, tt.want)
		}
	}
	if checkedAt(t, s, nothing) == "" || checkedAt(t, s, broken) != "" {
		t.Error("only a book Open Library has no cover for is marked checked")
	}
	// Without JavaScript the banner comes on the page, as a 422.
	rec := s.Post(t, s.Alice, fmt.Sprintf("/books/find-cover/%d", nothing), url.Values{"shelf": {"want"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("no-JS Find cover with none = %d, want 422", rec.Code)
	}
}

func TestFindCoverOfSomeoneElsesBookIsNotFound(t *testing.T) {
	s, _, _ := newBooksApp(t)
	id := withISBN(t, s, s.Bob.User.ID, "Piranesi", "9781635575637")
	rec := s.PostHX(t, s.Alice, fmt.Sprintf("/books/find-cover/%d", id), url.Values{})
	if rec.Code != http.StatusNotFound {
		t.Errorf("Alice's Find cover on Bob's book = %d, want 404", rec.Code)
	}
	if _, err := s.Store.Cover(context.Background(), s.Bob.User.ID, id); err == nil {
		t.Error("Bob's book got a cover")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `TestFindCoverIsInTheMenuOfABookWithout` finds no `.books-menu button[hx-post]` posting to Find cover; the Find cover posts are 404s (no route yet).

- [ ] **Step 3: Add the action and the menu item**

Create `internal/apps/books/findcover.go`:

```go
package books

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// FindCoverID asks Open Library for a book by title and its first author
// and returns the cover id of the first match that has one; ErrNoCover
// when none does. It is Find cover's way in for a book with no ISBN. A
// search by field is slower than the Add page's, so it gets an image
// fetch's time (the trial run saw a two-author search take 9s).
func (o *OpenLibrary) FindCoverID(ctx context.Context, title, authors string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, coverTimeout)
	defer cancel()
	v := url.Values{"title": {title}, "fields": {"cover_i"}, "limit": {"5"}}
	if first, _, _ := strings.Cut(authors, ","); strings.TrimSpace(first) != "" {
		v.Set("author", strings.TrimSpace(first))
	}
	res, err := o.Web.Get(ctx, o.Base+"/search.json?"+v.Encode(),
		webfetch.GetOptions{Accept: "application/json", MaxBytes: maxJSONBytes})
	if err != nil {
		return 0, fmt.Errorf("books: open library search: %w", err)
	}
	var body struct {
		Docs []olDoc `json:"docs"`
	}
	if err := json.Unmarshal(res.Body, &body); err != nil {
		return 0, fmt.Errorf("books: open library search: %w", err)
	}
	for _, d := range body.Docs {
		if d.CoverI > 0 {
			return d.CoverI, nil
		}
	}
	return 0, ErrNoCover
}

// lookUpCover is Find cover's lookup (spec "Covers after import"): the
// backfill's, by ISBN, then — when there is no ISBN, or Open Library has
// no cover for it — a search by title and author.
func (a *App) lookUpCover(ctx context.Context, b Book) (string, []byte, error) {
	if b.ISBN != "" {
		ct, data, err := a.ol.CoverByISBN(ctx, b.ISBN)
		if !errors.Is(err, ErrNoCover) {
			return ct, data, err
		}
	}
	coverID, err := a.ol.FindCoverID(ctx, b.Title, b.Authors)
	if err != nil {
		return "", nil, err
	}
	ct, data, err := a.ol.Cover(ctx, coverID, "M")
	if err == nil && !coverType(ct) {
		err = ErrNoCover
	}
	return ct, data, err
}

// findCover is the ⋯ menu's Find cover: the lookup, now. Finding none, or
// Open Library not answering, is a Refusal — the banner over the panes,
// as for any other action (spec "Errors").
func (a *App) findCover(r *http.Request, userID, id int64) error {
	ctx := r.Context()
	b, err := a.store.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	ct, data, err := a.lookUpCover(ctx, b)
	switch {
	case errors.Is(err, ErrNoCover):
		if err := a.store.MarkCoverChecked(ctx, userID, id); err != nil {
			return err
		}
		return &Refusal{Msg: "Open Library has no cover for this book. You can add one with Edit details."}
	case err != nil:
		a.deps.Log.Info("books find cover failed", "book", id, "error", err)
		return &Refusal{Msg: "Open Library didn't answer. Try again in a minute."}
	}
	return a.store.SetCover(ctx, userID, id, ct, data, CoverFromOL)
}
```

In `internal/apps/books/books.go`, replace:

```go
	r.HandleFunc("POST /quotes/{id}/{qid}/delete", a.act(a.deleteQuote, false))
	r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("GET /stats", a.stats)
	r.HandleFunc("POST /goal", a.setGoal)
	r.HandleFunc("POST /goal/clear", a.clearGoal)
```

with:

```go
	r.HandleFunc("POST /quotes/{id}/{qid}/delete", a.act(a.deleteQuote, false))
	r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("POST /find-cover/{id}", a.act(a.findCover, false))
	r.HandleFunc("GET /stats", a.stats)
	r.HandleFunc("POST /goal", a.setGoal)
	r.HandleFunc("POST /goal/clear", a.clearGoal)
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
{{define "field-error"}}{{with .Error}}<p class="books-field-error" id="books-{{$.Name}}-error">{{.}}</p>{{end}}{{end}}

{{/* book-menu is the book pane's ⋯ menu: a no-JS <details> disclosure
     (PATTERNS.md). Delete goes through the confirm dialog (books.js);
     without JavaScript it simply submits. Takes a bookView. */}}
{{define "book-menu"}}
<details class="outline-menu books-menu">
	<summary class="outline-menu-toggle quiet" aria-label="Book actions">{{ticon "more"}}</summary>
	<div class="outline-menu-list outline-menu-list-end">
		<a class="books-edit-link" href="{{.Ctx.EditURL .ID}}">Edit details</a>
		<form method="post" action="/books/delete/{{.ID}}">
			{{template "post-ctx" .}}
			<button type="submit" class="outline-menu-delete"
```

with:

```html
{{define "field-error"}}{{with .Error}}<p class="books-field-error" id="books-{{$.Name}}-error">{{.}}</p>{{end}}{{end}}

{{/* book-menu is the book pane's ⋯ menu: a no-JS <details> disclosure
     (PATTERNS.md). Find cover shows while the book has none (B5). Delete goes through the confirm dialog (books.js);
     without JavaScript it simply submits. Takes a bookView. */}}
{{define "book-menu"}}
<details class="outline-menu books-menu">
	<summary class="outline-menu-toggle quiet" aria-label="Book actions">{{ticon "more"}}</summary>
	<div class="outline-menu-list outline-menu-list-end">
		<a class="books-edit-link" href="{{.Ctx.EditURL .ID}}">Edit details</a>
		{{if not .Cover}}
		<form method="post" action="/books/find-cover/{{.ID}}">
			{{template "post-ctx" .}}
			<button type="submit" hx-post="/books/find-cover/{{.ID}}" hx-target="#books-panes" hx-swap="outerHTML">Find cover</button>
		</form>
		{{end}}
		<form method="post" action="/books/delete/{{.ID}}">
			{{template "post-ctx" .}}
			<button type="submit" class="outline-menu-delete"
```

- [ ] **Step 4: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./internal/apps/books/
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./internal/apps/books/
```
Expected: all pass, the older tests too.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): Find cover in the book menu (#491)"
```

---

### Task 5: The Import page

`GET /books/import` is a page of its own (as Stats is) with the steps and a plain multipart form; `POST /books/import` parses, imports and shows the summary — or the refusal, inline, as a 422, with nothing imported. Linked from the sidebar under Stats (default 15).

**Files:**
- Create: `internal/apps/books/importpage.go`, `internal/apps/books/templates/import.html`
- Modify: `internal/apps/books/books.go`, `internal/apps/books/templates/panes.partial.html`, `internal/ui/static/app.css`
- Test: `internal/apps/books/import_view_test.go`

**Interfaces:**
- Consumes: `ParseGoodreads`, `ImportError`, `MaxImportBytes` (Task 1); `Store.Import` (Task 2); `countText`, `fail`, `userID`, `Deps.Page`, `Render.Page` (existing); `grFixture`, `grHeader`, `grLine`, `goodOmens`, `texts`, `attr`, `add`, `titled` (test code).
- Produces: `GET /books/import`, `POST /books/import` (body cap `importBodyMaxBytes = MaxImportBytes + 1 MiB`); the `books/import` page; `.books-side-link` (the sidebar link style Task 7 reuses); test helper `postImport(t, s, sess, filename, content)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/import_view_test.go`:

```go
package books_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// postImport uploads content as the Import form does without JavaScript:
// multipart, the CSRF token as a field, the file as "file" (none when
// name is "").
func postImport(t *testing.T, s *server, sess *apptest.Session, name string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField(web.CSRFFormField, s.CSRFToken(t, sess)); err != nil {
		t.Fatal(err)
	}
	if name != "" {
		part, err := mw.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/books/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return s.Do(t, sess, req)
}

func TestImportPageIsLinkedAndTakesAFile(t *testing.T) {
	s := newServer(t)
	s.Get(t, s.Alice, "/books/").MustHave(`.books-side a[href="/books/import"]`)
	doc := s.Get(t, s.Alice, "/books/import")
	if got := attr(t, doc, "form.books-import-form", "enctype"); got != "multipart/form-data" {
		t.Errorf("enctype = %q, want multipart/form-data", got)
	}
	if got := attr(t, doc, "input#books-import-file", "name"); got != "file" {
		t.Errorf("file input name = %q, want file", got)
	}
	doc.MustHave(`a[href="/books/"]`)
	doc.MustNotHave(".books-import-result")
}

func TestImportUploadShowsWhatItDid(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	add(t, s, uid, titled("Infinite Jest", "David Foster Wallace", books.ShelfRead))
	rec := postImport(t, s, s.Alice, "goodreads_library_export.csv", grFixture(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /books/import = %d; body: %s", rec.Code, rec.Body.String())
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	// Infinite Jest is already here, typed in without an ISBN: the row
	// matches it by title and author.
	if got := htmlassert.Text(doc.MustHave(".books-import-count")); got != "Imported 5 books." {
		t.Errorf("summary = %q", got)
	}
	if got := texts(doc, ".books-import-skipped li"); len(got) != 2 || got[0] != goodOmens || got[1] != "Infinite Jest" {
		t.Errorf("skipped = %q, want the second Good Omens and Infinite Jest", got)
	}
	if !strings.Contains(htmlassert.Text(doc.MustHave(".books-import-result")), "Skipped 2 books already in your library") {
		t.Errorf("result = %q", htmlassert.Text(doc.MustHave(".books-import-result")))
	}
	counts, err := s.Store.ShelfCounts(context.Background(), uid)
	if err != nil || counts[books.ShelfAll] != 6 {
		t.Errorf("Alice has %v books, %v; want 6", counts, err)
	}
	if counts, _ := s.Store.ShelfCounts(context.Background(), s.Bob.User.ID); counts[books.ShelfAll] != 0 {
		t.Errorf("Bob has %d books, want none", counts[books.ShelfAll])
	}
}

func TestImportUploadRefusesWhatItCantUse(t *testing.T) {
	s := newServer(t)
	tests := []struct {
		name, file string
		content    []byte
		want       string
	}{
		{"no file", "", nil, "Choose your Goodreads export first."},
		{"not an export", "people.csv", []byte("Name,Email\nAnn,ann@example.com\n"),
			"That doesn't look like a Goodreads library export: it has no “Title” column."},
		{"a bad row", "goodreads.csv", []byte(grHeader + grLine("A Book", "9", "", "", "", "read", "", "1")),
			"Line 2: My Rating must be a whole number from 0 to 5."},
		{"too big", "huge.csv", bytes.Repeat([]byte("x"), books.MaxImportBytes+1), "That file is larger than 10 MB."},
	}
	for _, tt := range tests {
		rec := postImport(t, s, s.Alice, tt.file, tt.content)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422", tt.name, rec.Code)
			continue
		}
		doc := htmlassert.Parse(t, rec.Body.String())
		if got := htmlassert.Text(doc.MustHave("#books-import-error")); got != tt.want {
			t.Errorf("%s: error = %q, want %q", tt.name, got, tt.want)
		}
		if got := attr(t, doc, "#books-import-file", "aria-describedby"); got != "books-import-error" {
			t.Errorf("%s: the file input isn't described by the error", tt.name)
		}
	}
	if counts, _ := s.Store.ShelfCounts(context.Background(), s.Alice.User.ID); counts[books.ShelfAll] != 0 {
		t.Errorf("%d books after refused imports, want none", counts[books.ShelfAll])
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `GET /books/import = 404`; the sidebar has no `a[href="/books/import"]`.

- [ ] **Step 3: Add the page**

Create `internal/apps/books/importpage.go`:

```go
package books

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
)

// importBodyMaxBytes is the import form's body budget: the file and the
// multipart overhead (the suite default is 1 MiB).
const importBodyMaxBytes = MaxImportBytes + 1<<20

// importView is the Import page: the upload form, and after an upload
// either what the import did or why it didn't happen.
type importView struct {
	MaxMB    int
	Error    string
	Done     bool
	Imported string   // "Imported 6 books."
	Skipped  string   // "Skipped 2 books already in your library:"; "" for none
	Titles   []string // the skipped books' titles
}

// importPage is the Import page (spec "Import (B5)").
func (a *App) importPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	a.renderImport(w, r, http.StatusOK, importView{})
}

func (a *App) renderImport(w http.ResponseWriter, r *http.Request, status int, v importView) {
	v.MaxMB = MaxImportBytes >> 20
	page := a.deps.Page(r, "Import from Goodreads")
	page.Data = v
	if err := a.deps.Render.Page(w, status, "books/import", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// importUpload imports an uploaded Goodreads export and shows what it
// did. A file it can't use comes back on the page with the reason (a 422),
// and nothing is imported. A plain form post: no JavaScript needed.
//
// No http.MaxBytesReader here: CSRF.Middleware has already parsed the
// multipart form looking for its token, under this route's own body cap
// (RegisterBodyLimit in Mount). MaxImportBytes is checked against the
// upload's reported size, as ON Reader's OPML import does.
func (a *App) importUpload(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	refuse := func(msg string) { a.renderImport(w, r, http.StatusUnprocessableEntity, importView{Error: msg}) }
	file, header, err := r.FormFile("file")
	if err != nil {
		refuse("Choose your Goodreads export first.")
		return
	}
	defer func() { _ = file.Close() }()
	if header.Size > MaxImportBytes {
		refuse("That file is larger than " + strconv.Itoa(MaxImportBytes>>20) + " MB.")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxImportBytes+1))
	if err != nil || len(data) > MaxImportBytes {
		refuse("That upload could not be read.")
		return
	}
	rows, err := ParseGoodreads(bytes.NewReader(data))
	var ie *ImportError
	if errors.As(err, &ie) {
		refuse(ie.Error())
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	res, err := a.store.Import(r.Context(), uid, rows)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("books imported from goodreads", "imported", res.Imported, "skipped", len(res.Skipped))
	v := importView{Done: true, Imported: "Imported " + countText(res.Imported, "book", "books") + ".", Titles: res.Skipped}
	if n := len(res.Skipped); n > 0 {
		v.Skipped = "Skipped " + countText(n, "book", "books") + " already in your library:"
	}
	a.renderImport(w, r, http.StatusOK, v)
}
```

Create `internal/apps/books/templates/import.html`:

```html
{{define "content"}}
{{$d := .Data}}
<div class="books-import stack">
	<div class="books-import-head">
		<h1>Import from Goodreads</h1>
		<a class="button" href="/books/">← Books</a>
	</div>
	{{if $d.Done}}
	<section class="books-import-result" role="status" aria-label="Import summary">
		<p class="books-import-count">{{$d.Imported}}</p>
		{{with $d.Skipped}}<p>{{.}}</p>{{end}}
		{{with $d.Titles}}<ul class="books-import-skipped">{{range .}}<li>{{.}}</li>{{end}}</ul>{{end}}
		<p><a href="/books/?shelf=all">See all your books</a></p>
	</section>
	{{end}}
	<p>In Goodreads, open <strong>My Books</strong>, choose <strong>Import and export</strong> under Tools, and click <strong>Export Library</strong>. Download the file when it's ready and choose it here.</p>
	<p>Each book comes with its shelf, your rating and review, the day you added it and the day you read it, and its other shelves as tags. A book already in your library — the same ISBN, or the same title and author — is skipped, so importing a file twice adds nothing new. Covers follow over the next few hours.</p>
	<form class="books-import-form stack" method="post" action="/books/import" enctype="multipart/form-data">
		<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
		<div class="field">
			<label for="books-import-file">Goodreads export (a .csv file, up to {{$d.MaxMB}} MB)</label>
			<input id="books-import-file" name="file" type="file" accept=".csv,text/csv"{{if $d.Error}} aria-invalid="true" aria-describedby="books-import-error"{{end}}>
			{{with $d.Error}}<p class="books-field-error" id="books-import-error" role="alert">{{.}}</p>{{end}}
		</div>
		<div><button class="primary" type="submit">Import</button></div>
	</form>
</div>
{{end}}
```

In `internal/apps/books/books.go`, replace:

```go
	r.HandleFunc("GET /stats", a.stats)
	r.HandleFunc("POST /goal", a.setGoal)
	r.HandleFunc("POST /goal/clear", a.clearGoal)
	r.HandleFunc("GET /cover/{id}", a.cover)
	r.HandleFunc("GET /olcover/{id}", a.olThumb)
	r.HandleFunc("GET /books.js", a.script("books.js"))
```

with:

```go
	r.HandleFunc("GET /stats", a.stats)
	r.HandleFunc("POST /goal", a.setGoal)
	r.HandleFunc("POST /goal/clear", a.clearGoal)
	r.HandleFunc("GET /import", a.importPage)
	r.RegisterBodyLimit("POST /import", importBodyMaxBytes)
	r.HandleFunc("POST /import", a.importUpload)
	r.HandleFunc("GET /cover/{id}", a.cover)
	r.HandleFunc("GET /olcover/{id}", a.olThumb)
	r.HandleFunc("GET /books.js", a.script("books.js"))
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
	</ul>
	{{end}}
	<a class="books-stats-link" href="/books/stats">{{ticon "stats"}}Stats</a>
</nav>
{{end}}
```

with:

```html
	</ul>
	{{end}}
	<a class="books-stats-link" href="/books/stats">{{ticon "stats"}}Stats</a>
	<a class="books-side-link" href="/books/import">{{ticon "import"}}Import from Goodreads</a>
</nav>
{{end}}
```

In `internal/ui/static/app.css`, replace:

```css
.books-goal-card-head { color: var(--c-text-dim); font-size: var(--fs-xs); text-transform: uppercase; letter-spacing: 0.05em; flex-basis: 100%; }
.books-goal-card-text { font-weight: 600; }
.books-goal-card .books-row-bar { flex: 1 1 100%; width: auto; }
```

with:

```css
.books-goal-card-head { color: var(--c-text-dim); font-size: var(--fs-xs); text-transform: uppercase; letter-spacing: 0.05em; flex-basis: 100%; }
.books-goal-card-text { font-weight: 600; }
.books-goal-card .books-row-bar { flex: 1 1 100%; width: auto; }

/* Import (B5): a page of its own, as Stats is, linked under Stats in the
 * sidebar. */
.books-side-link { display: flex; align-items: center; gap: var(--s-2); padding: var(--s-1) var(--s-2); border-radius: var(--radius); color: var(--c-text); text-decoration: none; }
.books-side-link:hover { background: var(--c-bg-subtle); }
.books-import { max-width: var(--measure); }
.books-import-head { display: flex; flex-wrap: wrap; align-items: baseline; justify-content: space-between; gap: var(--s-3); }
.books-import-head h1 { margin: 0; }
.books-import-result { padding: var(--s-3); border: var(--border); border-radius: var(--radius); background: var(--c-bg-subtle); }
.books-import-result p { margin: 0 0 var(--s-2); }
.books-import-result p:last-child { margin-bottom: 0; }
.books-import-count { font-weight: 600; }
.books-import-skipped { margin: 0 0 var(--s-2); padding-left: var(--s-4); overflow-wrap: anywhere; }
.books-import-form input[type="file"] { max-width: 100%; }
```

- [ ] **Step 4: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./internal/apps/books/
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./internal/apps/books/
```
Expected: all pass. The "too big" case uploads 10 MB + 1 byte; it is under the route's cap, so the page's own message shows.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): import page for a Goodreads export (#491)"
```

---

### Task 6: `onsuite export`

`App.Export` makes Books an `app.Exporter`, so `onsuite export` includes it with no platform change. `Store.Export` reads each of the seven tables once, closing its rows before the next (one connection), and puts them together by book (default 13).

**Files:**
- Create: `internal/apps/books/export.go`
- Test: `internal/apps/books/export_json_test.go`

**Interfaces:**
- Consumes: `shelfExpr`, `latestJoin`, `parseTime`, `parseStamps`, `Status`, `Shelf` (existing); `importFixture` (Task 2).
- Produces: `(*App).Export(ctx, *sql.DB, userID int64) (any, error)`; `(*Store).Export(ctx, userID int64) (exportPayload, error)`; the unexported `exportPayload`, `exportedBook`, `exportedReading`, `exportedProgress`, `exportedNote`, `exportedQuote`, `exportedGoal` that Task 7 draws the Markdown from; `(*Store).eachRow(ctx, what, query string, args []any, scan func(*sql.Rows) error) error`.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/export_json_test.go`:

```go
package books_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// exportDoc is the JSON onsuite export writes for ON Books, read back
// the way a person restoring from it would.
type exportDoc struct {
	Books []struct {
		Title, Authors, ISBN13, Shelf, Review string
		SeriesName                            string `json:"series_name"`
		Rating                                int
		Tags                                  []string
		Readings                              []struct {
			Status, Format string
			StartedOn      string `json:"started_on"`
			FinishedOn     string `json:"finished_on"`
			Progress       []struct{ Page, Percent *int }
		}
		Notes []struct {
			Page int
			Body string
		}
		Quotes []struct {
			Page          int
			Text, Comment string
		}
	}
	Goals []struct{ Year, Target int }
}

func exportFor(t *testing.T, f *fixture, userID int64) (exportDoc, string) {
	t.Helper()
	var e app.Exporter = books.New()
	data, err := e.Export(context.Background(), f.db, userID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var doc exportDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc, string(raw)
}

func TestExportHasEverythingButCovers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	uid := f.alice.ID
	nb := onShelf("Dune", books.ShelfReading)
	nb.Authors, nb.Pages, nb.ISBN, nb.Tags = "Frank Herbert", 600, "9780441013593", []string{"sf", "classics"}
	id := addBook(t, f, uid, nb)
	if err := f.store.RecordProgress(ctx, uid, id, 120); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetRating(ctx, uid, id, 5); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetReview(ctx, uid, id, "The *spice*."); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddNote(ctx, uid, id, books.NoteInput{Page: 112, Body: "Paul and the box."}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddQuote(ctx, uid, id, books.QuoteInput{Text: "Fear is the mind-killer.", Comment: "The litany."}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCover(ctx, uid, id, "image/png", onePNG, books.CoverUpload); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetGoal(ctx, uid, 2026, 24); err != nil {
		t.Fatal(err)
	}
	addBook(t, f, f.bob.ID, onShelf("Bob's book", books.ShelfWant))

	doc, raw := exportFor(t, f, uid)
	if len(doc.Books) != 1 {
		t.Fatalf("%d books, want Alice's one: %s", len(doc.Books), raw)
	}
	b := doc.Books[0]
	if b.Title != "Dune" || b.Authors != "Frank Herbert" || b.ISBN13 != "9780441013593" || b.Shelf != "reading" ||
		b.Rating != 5 || b.Review != "The *spice*." || len(b.Tags) != 2 || b.Tags[0] != "classics" {
		t.Errorf("book = %+v", b)
	}
	if len(b.Readings) != 1 || b.Readings[0].Status != "reading" || b.Readings[0].StartedOn != "2026-10-10" ||
		len(b.Readings[0].Progress) != 1 || b.Readings[0].Progress[0].Page == nil || *b.Readings[0].Progress[0].Page != 120 ||
		b.Readings[0].Progress[0].Percent != nil {
		t.Errorf("readings = %+v", b.Readings)
	}
	if len(b.Notes) != 1 || b.Notes[0].Page != 112 || len(b.Quotes) != 1 || b.Quotes[0].Comment != "The litany." {
		t.Errorf("notes = %+v, quotes = %+v", b.Notes, b.Quotes)
	}
	if len(doc.Goals) != 1 || doc.Goals[0].Year != 2026 || doc.Goals[0].Target != 24 {
		t.Errorf("goals = %+v", doc.Goals)
	}
	var keys map[string]any
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		t.Fatal(err)
	}
	book := keys["books"].([]any)[0].(map[string]any)
	for _, k := range []string{"cover", "covers", "bytes", "cover_checked_at"} {
		if _, ok := book[k]; ok {
			t.Errorf("an exported book has %q; covers stay out of the export", k)
		}
	}
}

func TestExportOfAnEmptyLibraryIsEmptyLists(t *testing.T) {
	f := newFixture(t)
	if _, raw := exportFor(t, f, f.alice.ID); raw != `{"books":[],"goals":[]}` {
		t.Errorf("empty export = %s", raw)
	}
}

func TestExportKeepsUndatedImportedReadings(t *testing.T) {
	f := newFixture(t)
	importFixture(t, f, f.alice.ID)
	doc, _ := exportFor(t, f, f.alice.ID)
	for _, b := range doc.Books {
		if b.Title != "Leviathan Wakes" {
			continue
		}
		if len(b.Readings) != 2 || b.Readings[0].FinishedOn != "" || b.Readings[1].FinishedOn != "2024-03-14" ||
			b.SeriesName != "The Expanse" {
			t.Errorf("Leviathan Wakes = %+v, want the undated reading first, then the dated one", b)
		}
		return
	}
	t.Error("Leviathan Wakes isn't in the export")
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — the package doesn't build: `*books.App does not implement app.Exporter (missing method Export)`.

- [ ] **Step 3: Write the export**

Create `internal/apps/books/export.go`:

```go
package books

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// The shapes `onsuite export` writes for ON Books (spec "Export (B5)"),
// declared apart from the store's types so the backup format changes only
// when someone edits this file, as ON Later's are. Cover bytes are left
// out; the Markdown download (exportMarkdown) is drawn from the same
// payload.
type exportedProgress struct {
	Page       *int      `json:"page,omitempty"`
	Percent    *int      `json:"percent,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
}

type exportedReading struct {
	Status     Status             `json:"status"`
	Format     string             `json:"format,omitempty"`
	StartedOn  string             `json:"started_on,omitempty"`
	FinishedOn string             `json:"finished_on,omitempty"`
	CreatedAt  time.Time          `json:"created_at"`
	Progress   []exportedProgress `json:"progress"`
}

type exportedNote struct {
	Page      int       `json:"page,omitempty"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type exportedQuote struct {
	Page      int       `json:"page,omitempty"`
	Text      string    `json:"text"`
	Comment   string    `json:"comment,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type exportedBook struct {
	Title        string            `json:"title"`
	Subtitle     string            `json:"subtitle,omitempty"`
	Authors      string            `json:"authors,omitempty"`
	Year         int               `json:"year,omitempty"`
	Pages        int               `json:"pages,omitempty"`
	ISBN13       string            `json:"isbn13,omitempty"`
	OLWorkID     string            `json:"ol_work_id,omitempty"`
	OLEditionID  string            `json:"ol_edition_id,omitempty"`
	Description  string            `json:"description,omitempty"`
	SeriesName   string            `json:"series_name,omitempty"`
	SeriesNumber string            `json:"series_number,omitempty"`
	Rating       int               `json:"rating,omitempty"`
	Review       string            `json:"review,omitempty"`
	Shelf        Shelf             `json:"shelf"`
	Tags         []string          `json:"tags"`
	Readings     []exportedReading `json:"readings"` // oldest first
	Notes        []exportedNote    `json:"notes"`    // oldest first
	Quotes       []exportedQuote   `json:"quotes"`   // oldest first
	AddedAt      time.Time         `json:"added_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

type exportedGoal struct {
	Year   int `json:"year"`
	Target int `json:"target"`
}

type exportPayload struct {
	Books []exportedBook `json:"books"` // oldest added first
	Goals []exportedGoal `json:"goals"`
}

// Export implements app.Exporter, joining ON Books to onsuite export's
// whole-account JSON backup.
func (a *App) Export(ctx context.Context, handle *sql.DB, userID int64) (any, error) {
	return NewStore(handle).Export(ctx, userID)
}

// eachRow runs query and scan on every row, closing the rows before it
// returns: the database has one connection, and Export queries again.
func (st *Store) eachRow(ctx context.Context, what, query string, args []any, scan func(*sql.Rows) error) error {
	rows, err := st.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("books: export %s: %w", what, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return fmt.Errorf("books: export %s: %w", what, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("books: export %s: %w", what, err)
	}
	return nil
}

// Export gathers everything userID keeps in ON Books (spec "Export
// (B5)"): each table read once, then put together by book.
func (st *Store) Export(ctx context.Context, userID int64) (exportPayload, error) {
	uid := []any{userID}
	out := exportPayload{Books: []exportedBook{}, Goals: []exportedGoal{}}

	progress := map[int64][]exportedProgress{}
	err := st.eachRow(ctx, "progress", `
		SELECT p.reading_id, p.page, p.percent, p.recorded_at
		  FROM books_progress p JOIN books_readings r ON r.id = p.reading_id JOIN books_books b ON b.id = r.book_id
		 WHERE b.user_id = ? ORDER BY p.recorded_at, p.id`, uid, func(rows *sql.Rows) error {
		var rid int64
		var page, percent sql.NullInt64
		var at string
		if err := rows.Scan(&rid, &page, &percent, &at); err != nil {
			return err
		}
		p := exportedProgress{}
		if page.Valid {
			n := int(page.Int64)
			p.Page = &n
		}
		if percent.Valid {
			n := int(percent.Int64)
			p.Percent = &n
		}
		var err error
		p.RecordedAt, err = parseTime(at)
		progress[rid] = append(progress[rid], p)
		return err
	})
	if err != nil {
		return out, err
	}

	readings := map[int64][]exportedReading{}
	err = st.eachRow(ctx, "readings", `
		SELECT r.id, r.book_id, r.status, COALESCE(r.format, ''), COALESCE(r.started_on, ''),
		       COALESCE(r.finished_on, ''), r.created_at
		  FROM books_readings r JOIN books_books b ON b.id = r.book_id
		 WHERE b.user_id = ? ORDER BY r.created_at, r.id`, uid, func(rows *sql.Rows) error {
		var rid, bid int64
		var rd exportedReading
		var created string
		if err := rows.Scan(&rid, &bid, &rd.Status, &rd.Format, &rd.StartedOn, &rd.FinishedOn, &created); err != nil {
			return err
		}
		var err error
		rd.CreatedAt, err = parseTime(created)
		rd.Progress = append([]exportedProgress{}, progress[rid]...)
		readings[bid] = append(readings[bid], rd)
		return err
	})
	if err != nil {
		return out, err
	}

	notes := map[int64][]exportedNote{}
	err = st.eachRow(ctx, "notes", `
		SELECT n.book_id, COALESCE(n.page, 0), n.body, n.created_at, n.updated_at
		  FROM books_notes n JOIN books_books b ON b.id = n.book_id
		 WHERE b.user_id = ? ORDER BY n.created_at, n.id`, uid, func(rows *sql.Rows) error {
		var bid int64
		var n exportedNote
		var created, updated string
		if err := rows.Scan(&bid, &n.Page, &n.Body, &created, &updated); err != nil {
			return err
		}
		var err error
		n.CreatedAt, n.UpdatedAt, err = parseStamps(created, updated)
		notes[bid] = append(notes[bid], n)
		return err
	})
	if err != nil {
		return out, err
	}

	quotes := map[int64][]exportedQuote{}
	err = st.eachRow(ctx, "quotes", `
		SELECT q.book_id, COALESCE(q.page, 0), q.text, q.comment, q.created_at, q.updated_at
		  FROM books_quotes q JOIN books_books b ON b.id = q.book_id
		 WHERE b.user_id = ? ORDER BY q.created_at, q.id`, uid, func(rows *sql.Rows) error {
		var bid int64
		var q exportedQuote
		var created, updated string
		if err := rows.Scan(&bid, &q.Page, &q.Text, &q.Comment, &created, &updated); err != nil {
			return err
		}
		var err error
		q.CreatedAt, q.UpdatedAt, err = parseStamps(created, updated)
		quotes[bid] = append(quotes[bid], q)
		return err
	})
	if err != nil {
		return out, err
	}

	tags := map[int64][]string{}
	err = st.eachRow(ctx, "tags", `
		SELECT x.book_id, t.name FROM books_book_tags x JOIN books_tags t ON t.id = x.tag_id
		 WHERE t.user_id = ? ORDER BY t.name`, uid, func(rows *sql.Rows) error {
		var bid int64
		var name string
		err := rows.Scan(&bid, &name)
		tags[bid] = append(tags[bid], name)
		return err
	})
	if err != nil {
		return out, err
	}

	err = st.eachRow(ctx, "books", `
		SELECT b.id, b.title, b.subtitle, b.authors, COALESCE(b.year, 0), COALESCE(b.pages, 0),
		       COALESCE(b.isbn13, ''), b.ol_work_id, b.ol_edition_id, b.description, b.series_name,
		       b.series_number, COALESCE(b.rating, 0), b.review, `+shelfExpr+`, b.added_at, b.updated_at
		  FROM books_books b `+latestJoin+`
		 WHERE b.user_id = ? ORDER BY b.added_at, b.id`, uid, func(rows *sql.Rows) error {
		var id int64
		var b exportedBook
		var added, updated string
		if err := rows.Scan(&id, &b.Title, &b.Subtitle, &b.Authors, &b.Year, &b.Pages, &b.ISBN13, &b.OLWorkID,
			&b.OLEditionID, &b.Description, &b.SeriesName, &b.SeriesNumber, &b.Rating, &b.Review, &b.Shelf,
			&added, &updated); err != nil {
			return err
		}
		var err error
		b.AddedAt, b.UpdatedAt, err = parseStamps(added, updated)
		b.Tags = append([]string{}, tags[id]...)
		b.Readings = append([]exportedReading{}, readings[id]...)
		b.Notes = append([]exportedNote{}, notes[id]...)
		b.Quotes = append([]exportedQuote{}, quotes[id]...)
		out.Books = append(out.Books, b)
		return err
	})
	if err != nil {
		return out, err
	}

	err = st.eachRow(ctx, "goals", `SELECT year, target FROM books_goals WHERE user_id = ? ORDER BY year`, uid,
		func(rows *sql.Rows) error {
			var g exportedGoal
			err := rows.Scan(&g.Year, &g.Target)
			out.Goals = append(out.Goals, g)
			return err
		})
	return out, err
}
```

- [ ] **Step 4: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./internal/apps/books/
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./internal/apps/books/
```
Expected: all pass; `go test ./cmd/onsuite/ -run Export` passes too (its export now has a `books` key).

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): join onsuite export (#491)"
```

---

### Task 7: Export as Markdown

The whole library as one download, `books-export.md` (default 14), drawn from Task 6's payload. Linked in the sidebar under Import.

**Files:**
- Create: `internal/apps/books/download.go`
- Modify: `internal/apps/books/books.go`, `internal/apps/books/templates/panes.partial.html`
- Test: `internal/apps/books/download_test.go`

**Interfaces:**
- Consumes: `Store.Export`, `exportPayload`, `exportedBook` (Task 6); `seriesText`, `numText`, `stars`, `countText`, `pageText`, `readingDates`, `statusLabels`, `formatLabels`, `Shelf.Label`, `ShowDay`, `Store.Today` (existing); `.books-side-link` (Task 5); `noon` (goal_test.go).
- Produces: `func exportMarkdown(p exportPayload, today string) string`; `GET /books/export`.

- [ ] **Step 1: Write the failing tests**

The whole file is compared, so its layout is pinned.

Create `internal/apps/books/download_test.go`:

```go
package books_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// wantMarkdown is the download of the library TestMarkdownDownload
// builds: books by title, each with its details, readings (newest
// first), rating and review, notes and quotes.
const wantMarkdown = `# My books

Exported from ON Books on 9 Oct 2026: 2 books.

Reading goals — 2026: 24 books.

## Dune

*Book One*

- Author: Frank Herbert
- Series: Dune #1
- First published: 1965
- Pages: 600
- ISBN: 9780441013593
- Shelf: Read
- Tags: classics, sf
- Rating: ★★★★★ (5 of 5)
- Added: 1 Oct 2026

### Readings

- Read · Paper · 1 Oct 2026 – 9 Oct 2026

### Review

The *spice*.

Second paragraph.

### Notes

**9 Oct 2026 · p. 112**

Paul and the box.

### Quotes

> Fear is the mind-killer.
> Fear is the little-death.

— p. 8

The **litany**.

## emma

- Shelf: Want to read
- Added: 9 Oct 2026
`

func TestMarkdownDownload(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	s.Clock.Set(noon("2026-10-01"))
	nb := titled("Dune", "Frank Herbert", books.ShelfReading)
	nb.Subtitle, nb.Year, nb.Pages, nb.ISBN = "Book One", 1965, 600, "9780441013593"
	nb.SeriesName, nb.SeriesNumber, nb.Tags = "Dune", "1", []string{"sf", "classics"}
	id := add(t, s, uid, nb)
	if err := s.Store.SetFormat(ctx, uid, id, "paper"); err != nil {
		t.Fatal(err)
	}
	s.Clock.Set(noon("2026-10-09"))
	add(t, s, uid, titled("emma", "", books.ShelfWant)) // sorted ignoring case
	add(t, s, s.Bob.User.ID, titled("Bob's book", "", books.ShelfWant))
	if err := s.Store.FinishReading(ctx, uid, id, "2026-10-09", 5); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetReview(ctx, uid, id, "The *spice*.\n\nSecond paragraph."); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.AddNote(ctx, uid, id, books.NoteInput{Page: 112, Body: "Paul and the box."}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.AddQuote(ctx, uid, id, books.QuoteInput{Page: 8,
		Text: "Fear is the mind-killer.\nFear is the little-death.", Comment: "The **litany**."}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetGoal(ctx, uid, 2026, 24); err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/books/export", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /books/export = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="books-export.md"` {
		t.Errorf("Content-Disposition = %q, want the generic books-export.md", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Body.String(); got != wantMarkdown {
		t.Errorf("download =\n%s\nwant\n%s", got, wantMarkdown)
	}
}

func TestMarkdownDownloadIsLinked(t *testing.T) {
	s := newServer(t)
	link := s.Get(t, s.Alice, "/books/").MustHave(`.books-side a[href="/books/export"]`)
	if _, ok := htmlassert.Attr(link, "download"); !ok {
		t.Error("the Export link has no download attribute")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `GET /books/export = 404`; no `.books-side a[href="/books/export"]`.

- [ ] **Step 3: Write the download**

Create `internal/apps/books/download.go`:

```go
package books

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// exportMarkdown is the whole library as one Markdown file (spec "Export
// (B5)"): a section per book, by title — its details, readings, rating and
// review, notes and quotes. today is the date it says it was made
// ("10 Oct 2026"). The OL description is left out: the file is the
// person's own reading and writing (decided 2026-10-10 while planning B5).
func exportMarkdown(p exportPayload, today string) string {
	var b strings.Builder
	b.WriteString("# My books\n\nExported from ON Books on " + today + ": " + countText(len(p.Books), "book", "books") + ".\n")
	if len(p.Goals) > 0 {
		var goals []string
		for _, g := range p.Goals {
			goals = append(goals, strconv.Itoa(g.Year)+": "+countText(g.Target, "book", "books"))
		}
		b.WriteString("\nReading goals — " + strings.Join(goals, " · ") + ".\n")
	}
	list := slices.Clone(p.Books)
	slices.SortStableFunc(list, func(x, y exportedBook) int {
		return strings.Compare(strings.ToLower(x.Title), strings.ToLower(y.Title))
	})
	for _, bk := range list {
		writeBook(&b, bk)
	}
	return b.String()
}

func writeBook(b *strings.Builder, bk exportedBook) {
	b.WriteString("\n## " + bk.Title + "\n\n")
	if bk.Subtitle != "" {
		b.WriteString("*" + bk.Subtitle + "*\n\n")
	}
	item := func(label, value string) {
		if value != "" {
			b.WriteString("- " + label + ": " + value + "\n")
		}
	}
	item("Author", bk.Authors)
	item("Series", seriesText(bk.SeriesName, bk.SeriesNumber))
	item("First published", numText(bk.Year))
	item("Pages", numText(bk.Pages))
	item("ISBN", bk.ISBN13)
	item("Shelf", bk.Shelf.Label())
	item("Tags", strings.Join(bk.Tags, ", "))
	if bk.Rating > 0 {
		item("Rating", stars(bk.Rating)+" ("+strconv.Itoa(bk.Rating)+" of 5)")
	}
	item("Added", bk.AddedAt.Local().Format("2 Jan 2006"))

	if len(bk.Readings) > 0 {
		b.WriteString("\n### Readings\n\n")
		for i := len(bk.Readings) - 1; i >= 0; i-- { // newest first, as the book pane
			rd := bk.Readings[i]
			parts := []string{statusLabels[rd.Status]}
			if rd.Format != "" {
				parts = append(parts, formatLabels[rd.Format])
			}
			parts = append(parts, readingDates(Reading{StartedOn: rd.StartedOn, FinishedOn: rd.FinishedOn}))
			b.WriteString("- " + strings.Join(parts, " · ") + "\n")
		}
	}
	if bk.Review != "" {
		b.WriteString("\n### Review\n\n" + bk.Review + "\n")
	}
	if len(bk.Notes) > 0 {
		b.WriteString("\n### Notes\n")
		for _, n := range bk.Notes {
			head := n.CreatedAt.Local().Format("2 Jan 2006")
			if n.Page > 0 {
				head += " · " + pageText(n.Page)
			}
			b.WriteString("\n**" + head + "**\n\n" + n.Body + "\n")
		}
	}
	if len(bk.Quotes) > 0 {
		b.WriteString("\n### Quotes\n")
		for _, q := range bk.Quotes {
			b.WriteString("\n> " + strings.ReplaceAll(q.Text, "\n", "\n> ") + "\n")
			if q.Page > 0 {
				b.WriteString("\n— " + pageText(q.Page) + "\n")
			}
			if q.Comment != "" {
				b.WriteString("\n" + q.Comment + "\n")
			}
		}
	}
}

// download is the sidebar's Export as Markdown: the whole library as
// books-export.md. The name is generic on purpose, as notes-export.md is:
// never derived from a title.
func (a *App) download(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	p, err := a.store.Export(r.Context(), uid)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="books-export.md"`)
	_, _ = w.Write([]byte(exportMarkdown(p, ShowDay(a.store.Today()))))
}
```

In `internal/apps/books/books.go`, replace:

```go
	r.HandleFunc("GET /import", a.importPage)
	r.RegisterBodyLimit("POST /import", importBodyMaxBytes)
	r.HandleFunc("POST /import", a.importUpload)
	r.HandleFunc("GET /cover/{id}", a.cover)
	r.HandleFunc("GET /olcover/{id}", a.olThumb)
	r.HandleFunc("GET /books.js", a.script("books.js"))
```

with:

```go
	r.HandleFunc("GET /import", a.importPage)
	r.RegisterBodyLimit("POST /import", importBodyMaxBytes)
	r.HandleFunc("POST /import", a.importUpload)
	r.HandleFunc("GET /export", a.download)
	r.HandleFunc("GET /cover/{id}", a.cover)
	r.HandleFunc("GET /olcover/{id}", a.olThumb)
	r.HandleFunc("GET /books.js", a.script("books.js"))
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
	{{end}}
	<a class="books-stats-link" href="/books/stats">{{ticon "stats"}}Stats</a>
	<a class="books-side-link" href="/books/import">{{ticon "import"}}Import from Goodreads</a>
</nav>
{{end}}
```

with:

```html
	{{end}}
	<a class="books-stats-link" href="/books/stats">{{ticon "stats"}}Stats</a>
	<a class="books-side-link" href="/books/import">{{ticon "import"}}Import from Goodreads</a>
	<a class="books-side-link" href="/books/export" download>{{ticon "export"}}Export as Markdown</a>
</nav>
{{end}}
```

- [ ] **Step 4: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./internal/apps/books/
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./internal/apps/books/
```
Expected: all pass, the older tests too.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): export the library as Markdown (#491)"
```

---

### Task 8: Admin card

`App.Stats` makes Books an `app.Stater`: books per shelf, readings, notes, quotes and the stored cover bytes, across every user (spec "Admin card"). The store's own `Stats` is a user's reading stats, so this one is `AdminStats`.

**Files:**
- Create: `internal/apps/books/admin.go`
- Test: `internal/apps/books/admin_test.go`

**Interfaces:**
- Consumes: `shelfExpr`, `latestJoin` (existing); `importFixture` (Task 2).
- Produces: `(*App).Stats(ctx, *sql.DB) ([]app.Stat, error)`; `(*Store).AdminStats(ctx) ([]app.Stat, error)`; `humanBytes(int64) string` (mirrors ON Later's).

- [ ] **Step 1: Write the failing test**

Create `internal/apps/books/admin_test.go`:

```go
package books_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

func TestAdminCardCountsEveryUsersBooks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	importFixture(t, f, f.alice.ID) // 2 read (3 readings), 1 reading, 3 want
	id := addBook(t, f, f.bob.ID, onShelf("Dune", books.ShelfReading))
	if err := f.store.MarkDNF(ctx, f.bob.ID, id, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddNote(ctx, f.bob.ID, id, books.NoteInput{Body: "Too slow."}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddQuote(ctx, f.bob.ID, id, books.QuoteInput{Text: "Fear is the mind-killer."}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCover(ctx, f.bob.ID, id, "image/png", bytes.Repeat(onePNG, 128), books.CoverUpload); err != nil {
		t.Fatal(err)
	}

	var s app.Stater = books.New()
	stats, err := s.Stats(ctx, f.db)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, st := range stats {
		got[st.Label] = st.Value
	}
	want := map[string]string{"Books": "7", "Reading": "1", "Want to read": "3", "Read": "2", "Did not finish": "1",
		"Readings": "5", "Notes": "1", "Quotes": "1", "Stored covers": "2.0 KiB"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("admin card = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — the package doesn't build: `*books.App does not implement app.Stater (missing method Stats)`.

- [ ] **Step 3: Write the card**

Create `internal/apps/books/admin.go`:

```go
package books

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// Stats implements app.Stater: ON Books' card on the admin page.
func (a *App) Stats(ctx context.Context, handle *sql.DB) ([]app.Stat, error) {
	return NewStore(handle).AdminStats(ctx)
}

// AdminStats describes ON Books across every user (spec "Admin card"):
// books per shelf, readings, notes, quotes, and how much the stored covers
// weigh. (Stats is the reading stats of one user.)
func (st *Store) AdminStats(ctx context.Context) ([]app.Stat, error) {
	var total, reading, want, read, dnf, readings, notes, quotes, coverBytes int64
	if err := st.db.QueryRowContext(ctx, `
		SELECT count(*), coalesce(sum(shelf = 'reading'), 0), coalesce(sum(shelf = 'want'), 0),
		       coalesce(sum(shelf = 'read'), 0), coalesce(sum(shelf = 'dnf'), 0)
		  FROM (SELECT `+shelfExpr+` AS shelf FROM books_books b `+latestJoin+`)`).Scan(
		&total, &reading, &want, &read, &dnf); err != nil {
		return nil, fmt.Errorf("books: admin stats: %w", err)
	}
	if err := st.db.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM books_readings), (SELECT count(*) FROM books_notes),
		       (SELECT count(*) FROM books_quotes), (SELECT coalesce(sum(length(bytes)), 0) FROM books_covers)`).Scan(
		&readings, &notes, &quotes, &coverBytes); err != nil {
		return nil, fmt.Errorf("books: admin stats: %w", err)
	}
	n := func(v int64) string { return strconv.FormatInt(v, 10) }
	return []app.Stat{
		{Label: "Books", Value: n(total)},
		{Label: "Reading", Value: n(reading)},
		{Label: "Want to read", Value: n(want)},
		{Label: "Read", Value: n(read)},
		{Label: "Did not finish", Value: n(dnf)},
		{Label: "Readings", Value: n(readings)},
		{Label: "Notes", Value: n(notes)},
		{Label: "Quotes", Value: n(quotes)},
		{Label: "Stored covers", Value: humanBytes(coverBytes), Hint: "cover images kept with the books, for everyone"},
	}, nil
}

// humanBytes renders a byte count the way a person reads one. It mirrors
// ON Later's own humanBytes: apps never import each other, so this is an
// independent copy (PATTERNS.md, "Cross-app mirroring").
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
```

- [ ] **Step 4: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./internal/apps/books/
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./internal/apps/books/
```
Expected: all pass, the older tests too.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): admin card (#491)"
```

---

### Task 9: Book pane polish — #566, #568, #579

Three small template/CSS fixes (defaults 16–18): a long description folds behind More (checkbox technique, PATTERNS.md), shelf counts read as words, and the stars wrap as one.

**Files:**
- Modify: `internal/apps/books/view.go`, `internal/apps/books/templates/panes.partial.html`, `internal/ui/static/app.css`, `internal/apps/books/handlers_test.go`
- Test: `internal/apps/books/polish_test.go`

**Interfaces:**
- Consumes: `bookView`, `viewBook`, `.visually-hidden` (existing).
- Produces: `bookView.DescriptionLong bool`, `func longDescription(string) bool`; the `description` template; `.books-star-row`.

- [ ] **Step 1: Write the failing tests**

`TestIndexOpensOnTheReadingShelf` pinned the old run-together text ("Reading1"); it now wants the words.

In `internal/apps/books/handlers_test.go`, replace:

```go
		t.Errorf("rows = %v, want [Dune]", got)
	}
	current := doc.MustHave(`.books-side a[aria-current="page"]`)
	if text := htmlassert.Text(current); !strings.HasPrefix(text, "Reading") || !strings.HasSuffix(text, "1") {
		t.Errorf("current shelf link = %q, want Reading with count 1", text)
	}
	doc.MustHave(`a[href="/books/new"]`)
	doc.MustHave(".books-book .empty")
```

with:

```go
		t.Errorf("rows = %v, want [Dune]", got)
	}
	current := doc.MustHave(`.books-side a[aria-current="page"]`)
	if text := htmlassert.Text(current); text != "Reading, 1 book" {
		t.Errorf("current shelf link = %q, want Reading with count 1, read out as \"Reading, 1 book\" (#568)", text)
	}
	doc.MustHave(`a[href="/books/new"]`)
	doc.MustHave(".books-book .empty")
```

Create `internal/apps/books/polish_test.go`:

```go
package books_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestLongDescriptionsFold(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	short := titled("Short", "", books.ShelfWant)
	short.Description = "A detective and a ship's officer."
	long := titled("Long", "", books.ShelfWant)
	long.Description = strings.Repeat("A long description. ", 20) // 400 characters
	lines := titled("Lines", "", books.ShelfWant)
	lines.Description = "One\nTwo\nThree\nFour\nFive"
	for _, nb := range []books.NewBook{long, lines} {
		doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=want", add(t, s, uid, nb)))
		doc.MustHave("input#books-description-more")
		doc.MustHave("p.is-clamped")
		if got := attr(t, doc, "label.books-description-more", "for"); got != "books-description-more" {
			t.Errorf("%s: More label is for %q", nb.Title, got)
		}
	}
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=want", add(t, s, uid, short)))
	if got := htmlassert.Text(doc.MustHave(".books-description-text")); got != short.Description {
		t.Errorf("short description = %q", got)
	}
	doc.MustNotHave("input#books-description-more")
	doc.MustNotHave(".is-clamped")
}

func TestShelfCountsReadAsWords(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	add(t, s, uid, titled("Emma", "", books.ShelfWant))
	add(t, s, uid, titled("Persuasion", "", books.ShelfWant))
	got := texts(s.Get(t, s.Alice, "/books/?shelf=want"), ".books-shelves .books-shelf")
	want := []string{"Reading", "Want to read, 2 books", "Read", "Did not finish", "All books, 2 books"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("shelf links = %q, want %q (#568)", got, want)
	}
}

func TestRatingStarsWrapTogether(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfRead))
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=read", id))
	if n := len(doc.QueryAll(".books-star-row .books-star")); n != 5 {
		t.Errorf("%d stars in .books-star-row, want all 5 (#579)", n)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — no `input#books-description-more`; shelf links read "Want to read2"; no `.books-star-row`.

- [ ] **Step 3: Fix them**

In `internal/apps/books/view.go`, replace:

```go
	"strconv"
	"strings"
	"unicode"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
)
```

with:

```go
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
)
```

In `internal/apps/books/view.go`, replace:

```go
	SeriesURL                        string   // the list of the book's series
	Facts                            []string // "2011", "592 pages", "ISBN 978…"
	Description                      string
	Spine                            string
	Cover                            string // the stored cover; "" draws the spine
	ShelfLabel                       string
```

with:

```go
	SeriesURL                        string   // the list of the book's series
	Facts                            []string // "2011", "592 pages", "ISBN 978…"
	Description                      string
	DescriptionLong                  bool // held to a few lines, with a More toggle (#566)
	Spine                            string
	Cover                            string // the stored cover; "" draws the spine
	ShelfLabel                       string
```

In `internal/apps/books/view.go`, replace:

```go
		Description: b.Description, Spine: SpineColor(b.Title), Cover: coverURL(b.ID, b.CoverVersion),
		ShelfLabel: b.Shelf.Label(), Tags: b.Tags, TagsValue: strings.Join(b.Tags, ", "), Today: today, Pages: b.Pages,
		Rating: b.Rating, Stars: starButtons(b.Rating), Review: b.Review, ReviewHTML: RenderReview(b.Review), Ctx: c}
	if b.SeriesName != "" {
		v.Series = seriesText(b.SeriesName, b.SeriesNumber)
		if b.SeriesBooks > 1 { // the count only says something once there are two
```

with:

```go
		Description: b.Description, Spine: SpineColor(b.Title), Cover: coverURL(b.ID, b.CoverVersion),
		ShelfLabel: b.Shelf.Label(), Tags: b.Tags, TagsValue: strings.Join(b.Tags, ", "), Today: today, Pages: b.Pages,
		Rating: b.Rating, Stars: starButtons(b.Rating), Review: b.Review, ReviewHTML: RenderReview(b.Review), Ctx: c}
	v.DescriptionLong = longDescription(b.Description)
	if b.SeriesName != "" {
		v.Series = seriesText(b.SeriesName, b.SeriesNumber)
		if b.SeriesBooks > 1 { // the count only says something once there are two
```

In `internal/apps/books/view.go`, replace:

```go
func (c listCtx) EditURL(id int64) string {
	return "/books/edit/" + strconv.FormatInt(id, 10) + "?" + c.Query()
}
```

with:

```go
func (c listCtx) EditURL(id int64) string {
	return "/books/edit/" + strconv.FormatInt(id, 10) + "?" + c.Query()
}

// longDescription says whether a description is long enough to fold
// (#566): more than about four lines of the pane's width, or more than
// four lines of its own.
func longDescription(s string) bool {
	return utf8.RuneCountInString(s) > 300 || strings.Count(s, "\n") >= 4
}
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
	<a class="button primary books-add" href="/books/new">{{ticon "plus"}}Add book</a>
	<ul class="books-shelves">
		{{range .Sidebar.Shelves}}
		<li><a class="books-shelf" href="{{.URL}}" hx-get="{{.URL}}" hx-target="#books-list" hx-swap="outerHTML" hx-push-url="true"{{if .Current}} aria-current="page"{{end}}>{{.Label}}<span class="books-count">{{if .Count}}{{.Count}}{{end}}</span></a></li>
		{{end}}
	</ul>
	{{with .Sidebar.Tags}}
```

with:

```html
	<a class="button primary books-add" href="/books/new">{{ticon "plus"}}Add book</a>
	<ul class="books-shelves">
		{{range .Sidebar.Shelves}}
		<li><a class="books-shelf" href="{{.URL}}" hx-get="{{.URL}}" hx-target="#books-list" hx-swap="outerHTML" hx-push-url="true"{{if .Current}} aria-current="page"{{end}}>{{.Label}}<span class="books-count">{{if .Count}}<span class="visually-hidden">, </span>{{.Count}}<span class="visually-hidden"> {{if eq .Count 1}}book{{else}}books{{end}}</span>{{end}}</span></a></li>
		{{end}}
	</ul>
	{{with .Sidebar.Tags}}
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
	{{template "reading-box" .}}
	{{template "rating" .}}
	{{template "review" .}}
	{{with .Description}}<div class="books-description">{{.}}</div>{{end}}
	{{template "tags-form" .}}
	{{template "notes" .}}
	{{template "quotes" .}}
```

with:

```html
	{{template "reading-box" .}}
	{{template "rating" .}}
	{{template "review" .}}
	{{template "description" .}}
	{{template "tags-form" .}}
	{{template "notes" .}}
	{{template "quotes" .}}
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html

{{/* rating is the book's stars (spec "Book pane"): five submit buttons,
     so it works without JavaScript; the current rating's own star clears
     it. Takes a bookView. */}}
{{define "rating"}}
<form class="books-rating" method="post" action="/books/rating/{{.ID}}"
      hx-post="/books/rating/{{.ID}}" hx-target="#books-panes" hx-swap="outerHTML">
	{{template "post-ctx" .}}
	<span class="books-rating-label">{{if .Rating}}Your rating{{else}}Rate it{{end}}</span>
	{{range .Stars}}<button type="submit" class="books-star{{if .On}} is-on{{end}}" name="rating" value="{{.Value}}" aria-label="{{.Label}}" title="{{.Label}}">{{if .On}}{{ticon "star-filled"}}{{else}}{{ticon "star-outline"}}{{end}}</button>{{end}}
</form>
{{end}}
```

with:

```html

{{/* rating is the book's stars (spec "Book pane"): five submit buttons,
     so it works without JavaScript; the current rating's own star clears
     it. The stars wrap together, never one by one, in a narrow pane
     (#579). Takes a bookView. */}}
{{define "rating"}}
<form class="books-rating" method="post" action="/books/rating/{{.ID}}"
      hx-post="/books/rating/{{.ID}}" hx-target="#books-panes" hx-swap="outerHTML">
	{{template "post-ctx" .}}
	<span class="books-rating-label">{{if .Rating}}Your rating{{else}}Rate it{{end}}</span>
	<span class="books-star-row">{{range .Stars}}<button type="submit" class="books-star{{if .On}} is-on{{end}}" name="rating" value="{{.Value}}" aria-label="{{.Label}}" title="{{.Label}}">{{if .On}}{{ticon "star-filled"}}{{else}}{{ticon "star-outline"}}{{end}}</button>{{end}}</span>
</form>
{{end}}
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
</section>
{{end}}

{{/* tags-form edits a book's tags in place. Takes a bookView. */}}
{{define "tags-form"}}
<form class="books-tags-form" method="post" action="/books/tags/{{.ID}}"
```

with:

```html
</section>
{{end}}

{{/* description is the Open Library description, held to four lines
     when it is long, with a More / Less toggle (#566): the checkbox
     technique (PATTERNS.md), so it needs no JavaScript. Takes a bookView. */}}
{{define "description"}}
{{with .Description}}
<div class="books-description">
	{{if $.DescriptionLong}}<input type="checkbox" id="books-description-more" class="visually-hidden books-description-toggle">{{end}}
	<p class="books-description-text{{if $.DescriptionLong}} is-clamped{{end}}">{{.}}</p>
	{{if $.DescriptionLong}}<label class="books-description-more" for="books-description-more"><span class="books-more-label">More</span><span class="books-less-label">Less</span></label>{{end}}
</div>
{{end}}
{{end}}

{{/* tags-form edits a book's tags in place. Takes a bookView. */}}
{{define "tags-form"}}
<form class="books-tags-form" method="post" action="/books/tags/{{.ID}}"
```

In `internal/ui/static/app.css`, replace:

```css
.books-pill { padding: 0 var(--s-2); border-radius: 999px; background: var(--c-bg-subtle); color: var(--c-text-dim); font-size: var(--fs-xs); }
.books-pill-shelf { background: var(--c-accent-bg); color: var(--c-accent); }
.books-description { max-width: 38rem; margin-bottom: var(--s-4); color: var(--c-text-dim); white-space: pre-line; }

/* Narrow screens: Reader's drill-down, with Books' ids. The back control
 * is a <label> for the checkbox, so it works with no script. */
```

with:

```css
.books-pill { padding: 0 var(--s-2); border-radius: 999px; background: var(--c-bg-subtle); color: var(--c-text-dim); font-size: var(--fs-xs); }
.books-pill-shelf { background: var(--c-accent-bg); color: var(--c-accent); }
.books-description { max-width: 38rem; margin-bottom: var(--s-4); color: var(--c-text-dim); white-space: pre-line; }
/* A long description is held to four lines until More is ticked (#566). */
.books-description-text { margin: 0; }
.books-description-text.is-clamped { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 4; overflow: hidden; }
.books-description-toggle:checked ~ .books-description-text { display: block; -webkit-line-clamp: none; overflow: visible; }
.books-description-more { display: inline-block; margin: var(--s-1) 0 0; color: var(--c-accent); font-size: var(--fs-sm); cursor: pointer; }
.books-less-label,
.books-description-toggle:checked ~ .books-description-more .books-more-label { display: none; }
.books-description-toggle:checked ~ .books-description-more .books-less-label { display: inline; }
.books-description-toggle:focus-visible ~ .books-description-more { outline: var(--ring); }

/* Narrow screens: Reader's drill-down, with Books' ids. The back control
 * is a <label> for the checkbox, so it works with no script. */
```

In `internal/ui/static/app.css`, replace:

```css
/* Rating and review */
.books-rating { display: flex; flex-wrap: wrap; align-items: center; gap: var(--s-1); margin-bottom: var(--s-3); }
.books-rating-label { margin-right: var(--s-1); color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-star { display: inline-flex; padding: var(--s-1); border: 0; background: none; color: var(--c-text-faint); cursor: pointer; }
.books-star:hover,
.books-star.is-on { color: var(--c-accent); }
```

with:

```css
/* Rating and review */
.books-rating { display: flex; flex-wrap: wrap; align-items: center; gap: var(--s-1); margin-bottom: var(--s-3); }
.books-rating-label { margin-right: var(--s-1); color: var(--c-text-dim); font-size: var(--fs-sm); }
/* The five stars wrap as one, below the label, in a narrow pane (#579). */
.books-star-row { display: inline-flex; flex-wrap: nowrap; gap: var(--s-1); }
.books-star { display: inline-flex; padding: var(--s-1); border: 0; background: none; color: var(--c-text-faint); cursor: pointer; }
.books-star:hover,
.books-star.is-on { color: var(--c-accent); }
```

- [ ] **Step 4: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./internal/apps/books/
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./internal/apps/books/
```
Expected: all pass, the older tests too.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books internal/ui/static/app.css
git commit -m "fix(books): fold long descriptions, spoken shelf counts, stars that wrap together (#566, #568, #579)"
```

---

### Task 10: Keep the book pane's scroll — #578

Script only (default 19). There is no JavaScript test harness in the suite, so the check is in the browser.

**Files:**
- Modify: `internal/apps/books/static/books.js`

- [ ] **Step 1: Add the section**

In `internal/apps/books/static/books.js`, replace:

```js
// ON Books' script: the delete confirmation, resizable panes, keyboard
// shortcuts and the open book's row highlight. Vanilla and
// CSP-clean; the sections mirror reader.js's (apps never share app
// scripts).
(function () {
```

with:

```js
// ON Books' script: the delete confirmation, resizable panes, keyboard
// shortcuts, the open book's row highlight and the panes' scroll
// positions across a swap. Vanilla and
// CSP-clean; the sections mirror reader.js's (apps never share app
// scripts).
(function () {
```

In `internal/apps/books/static/books.js`, replace:

```js
	// no afterSettle (#456).
	document.addEventListener("htmx:historyRestore", initResizablePanes);

	// --- The open book's row ------------------------------------------------
	//
	// A full render marks it on the server; these two cover what the server
```

with:

```js
	// no afterSettle (#456).
	document.addEventListener("htmx:historyRestore", initResizablePanes);

	// --- Keep the panes where they were (#578) -------------------------------
	//
	// A change made in the book pane — a star, the review, the tags, the
	// format, a reading — swaps the whole panes (#books-panes), and new
	// panes start scrolled to the top: the control just used would jump
	// out of sight. Remember where the book pane and the list were, and put
	// them back when the same book, and the same list, are still showing.
	// Smaller swap targets would do it too, but every one of those actions
	// can move the shelf counts, the list and the book at once.
	function listKey() {
		var list = document.getElementById("books-list");
		if (!list) return "";
		return ["shelf", "tag", "q", "series"].map(function (k) {
			return list.getAttribute("data-" + k) || "";
		}).join("\u0000");
	}

	var keptScroll = null;
	document.addEventListener("htmx:beforeSwap", function (e) {
		keptScroll = null;
		if (!e.detail.shouldSwap || !e.detail.target || e.detail.target.id !== "books-panes") return;
		var book = document.getElementById("books-book");
		var listPane = document.querySelector(".books-listpane");
		keptScroll = {
			bookId: (book && book.getAttribute("data-book-id")) || "",
			book: book ? book.scrollTop : 0,
			listKey: listKey(),
			list: listPane ? listPane.scrollTop : 0,
		};
	});
	document.addEventListener("htmx:afterSwap", function () {
		if (!keptScroll) return;
		var kept = keptScroll;
		keptScroll = null;
		var book = document.getElementById("books-book");
		if (book && kept.bookId !== "" && book.getAttribute("data-book-id") === kept.bookId) book.scrollTop = kept.book;
		var listPane = document.querySelector(".books-listpane");
		if (listPane && listKey() === kept.listKey) listPane.scrollTop = kept.list;
	});

	// --- The open book's row ------------------------------------------------
	//
	// A full render marks it on the server; these two cover what the server
```

- [ ] **Step 2: Check it in a browser**

Seed and start a server as in Task 12 Step 1, open `/books/b/1?shelf=reading`, scroll the book pane down to the stars, click one: the pane stays where it was (in the console, `document.getElementById('books-book').scrollTop` is the same before and after; without this task it jumps back to 0). Save the tags, edit a reading: the same. Open another book from the list: it starts at the top. At 375px wide, the same on the book pane.

- [ ] **Step 3: Run the full check and commit**

```bash
go test ./internal/apps/books/... -count=1
git add internal/apps/books/static/books.js
git commit -m "fix(books): keep the book pane's scroll across a save (#578)"
```

---

### Task 11: Guide, README, app list and screenshots — #567

**Files:**
- Modify: `docs/user/books.md`, `README.md`, `AGENTS.md`, `docs/screenshots/capture/shots.go`, `docs/screenshots/seed/seed_test.go`
- Create (generated): `docs/user/images/books-{panes,menu,add,stats,import}.png`, `docs/images/app-books-{light,dark}.png`; re-shot: `docs/images/hero-{light,dark}.png` (the dashboard now has seven cards)

- [ ] **Step 1: Add the shots**

Book 1 of the seed is Leviathan Wakes; `TestShotIDsPointAtTheIntendedItems` learns to check Books' IDs.

In `docs/screenshots/seed/seed_test.go`, replace:

```go
	"flash":  {1: "Japanese travel phrases", 2: "F1 circuits"},
	"later":  {1: "The case for reading slowly"},
	"focus":  {1: "Deep work"},
}

// shotIDRe finds the seeded IDs in shots.go URLs: /paste/3, /notes/25,
// /reader/item/10, /flash/2/cards/, /flash/review/2, /later/a/1, /focus/run/1.
var shotIDRe = regexp.MustCompile(`URL: "/(paste|notes|reader/item|flash(?:/review)?|later/a|focus/run)/(\d+)`)

func TestShotIDsPointAtTheIntendedItems(t *testing.T) {
	src, err := os.ReadFile("../capture/shots.go")
```

with:

```go
	"flash":  {1: "Japanese travel phrases", 2: "F1 circuits"},
	"later":  {1: "The case for reading slowly"},
	"focus":  {1: "Deep work"},
	"books":  {1: "Leviathan Wakes"},
}

// shotIDRe finds the seeded IDs in shots.go URLs: /paste/3, /notes/25,
// /reader/item/10, /flash/2/cards/, /flash/review/2, /later/a/1, /focus/run/1,
// /books/b/1.
var shotIDRe = regexp.MustCompile(`URL: "/(paste|notes|reader/item|flash(?:/review)?|later/a|focus/run|books/b)/(\d+)`)

func TestShotIDsPointAtTheIntendedItems(t *testing.T) {
	src, err := os.ReadFile("../capture/shots.go")
```

In `docs/screenshots/seed/seed_test.go`, replace:

```go
			tm, err := focus.NewStore(handle).Timer(ctx, demo.ID, id)
			return tm.Name, err
		},
		"flash": func(id int64) (string, error) {
			d, err := flash.NewStore(handle).DeckByID(ctx, demo.ID, id)
			return d.Name, err
```

with:

```go
			tm, err := focus.NewStore(handle).Timer(ctx, demo.ID, id)
			return tm.Name, err
		},
		"books": func(id int64) (string, error) {
			b, err := books.NewStore(handle).Get(ctx, demo.ID, id)
			return b.Title, err
		},
		"flash": func(id int64) (string, error) {
			d, err := flash.NewStore(handle).DeckByID(ctx, demo.ID, id)
			return d.Name, err
```

In `docs/screenshots/capture/shots.go`, replace:

```go
	{Name: "docs/user/images/focus-running.png", URL: "/focus/run/1", Height: 660, Setup: focusRunning},
	{Name: "docs/user/images/focus-history.png", URL: "/focus/history", Height: 1000},

	// docs/user/admin.md
	// Look only: nothing here adds a user, resets a password or presses
	// Run now, so the seeded accounts and job history stay as they are.
```

with:

```go
	{Name: "docs/user/images/focus-running.png", URL: "/focus/run/1", Height: 660, Setup: focusRunning},
	{Name: "docs/user/images/focus-history.png", URL: "/focus/history", Height: 1000},

	// docs/user/books.md
	// Book 1 is Leviathan Wakes, on the Reading shelf in the seed. Nothing
	// here imports a file or presses Find cover, so no request reaches
	// Open Library and the library stays as seeded.
	{Name: "docs/user/images/books-panes.png", URL: "/books/b/1?shelf=reading", Height: 760},
	{Name: "docs/user/images/books-menu.png", URL: "/books/b/1?shelf=reading", Height: 420, Setup: `
		document.querySelector('details.books-menu').open = true;`},
	{Name: "docs/user/images/books-add.png", URL: "/books/new", Height: 720},
	{Name: "docs/user/images/books-stats.png", URL: "/books/stats", Height: 980},
	{Name: "docs/user/images/books-import.png", URL: "/books/import", Height: 560},

	// docs/user/admin.md
	// Look only: nothing here adds a user, resets a password or presses
	// Run now, so the seeded accounts and job history stay as they are.
```

In `docs/screenshots/capture/shots.go`, replace:

```go

	// README.md — the hero and one thumbnail per app, light and dark, at the
	// default 1280×800 (thumbnails show at about half width).
	// The hero is the dashboard, cropped: it shows all six apps at a glance.
	{Name: "docs/images/hero-light.png", URL: "/", Height: 490},
	{Name: "docs/images/hero-dark.png", URL: "/", Height: 490, Theme: "dark"},
	{Name: "docs/images/app-paste-light.png", URL: "/paste/7"},
```

with:

```go

	// README.md — the hero and one thumbnail per app, light and dark, at the
	// default 1280×800 (thumbnails show at about half width).
	// The hero is the dashboard, cropped: it shows all seven apps at a glance.
	{Name: "docs/images/hero-light.png", URL: "/", Height: 490},
	{Name: "docs/images/hero-dark.png", URL: "/", Height: 490, Theme: "dark"},
	{Name: "docs/images/app-paste-light.png", URL: "/paste/7"},
```

In `docs/screenshots/capture/shots.go`, replace:

```go
	{Name: "docs/images/app-reader-dark.png", URL: "/reader/item/10?scope=all&filter=all", Theme: "dark", Setup: readerScrollToActive},
	{Name: "docs/images/app-later-light.png", URL: "/later/a/1"},
	{Name: "docs/images/app-later-dark.png", URL: "/later/a/1", Theme: "dark"},
	{Name: "docs/images/app-flash-light.png", URL: "/flash/2/cards/"},
	{Name: "docs/images/app-flash-dark.png", URL: "/flash/2/cards/", Theme: "dark"},
	{Name: "docs/images/app-focus-light.png", URL: "/focus/run/1", Setup: focusRunning},
```

with:

```go
	{Name: "docs/images/app-reader-dark.png", URL: "/reader/item/10?scope=all&filter=all", Theme: "dark", Setup: readerScrollToActive},
	{Name: "docs/images/app-later-light.png", URL: "/later/a/1"},
	{Name: "docs/images/app-later-dark.png", URL: "/later/a/1", Theme: "dark"},
	{Name: "docs/images/app-books-light.png", URL: "/books/b/1?shelf=reading"},
	{Name: "docs/images/app-books-dark.png", URL: "/books/b/1?shelf=reading", Theme: "dark"},
	{Name: "docs/images/app-flash-light.png", URL: "/flash/2/cards/"},
	{Name: "docs/images/app-flash-dark.png", URL: "/flash/2/cards/", Theme: "dark"},
	{Name: "docs/images/app-focus-light.png", URL: "/focus/run/1", Setup: focusRunning},
```

- [ ] **Step 2: Update the guide, the README and the app list**

The README section goes after ON Flash, in the dashboard's order.

In `docs/user/books.md`, replace:

```markdown
what you want to read next and what you gave up on. No friends, no feed —
just your own books.

## Shelves

Every book sits on one shelf:
```

with:

```markdown
what you want to read next and what you gave up on. No friends, no feed —
just your own books.

![ON Books: the shelves, the books you're reading, and one of them open](images/books-panes.png)

## Shelves

Every book sits on one shelf:
```

In `docs/user/books.md`, replace:

```markdown
yourself. Only the title is required. An ISBN can be the 10- or 13-digit
kind, with or without hyphens; ON Books keeps it as 13 digits.

Then choose where the book goes:

- **Want to read** — it waits on that shelf.
```

with:

```markdown
yourself. Only the title is required. An ISBN can be the 10- or 13-digit
kind, with or without hyphens; ON Books keeps it as 13 digits.

![The Add book page: an Open Library search above the book form](images/books-add.png)

Then choose where the book goes:

- **Want to read** — it waits on that shelf.
```

In `docs/user/books.md`, replace:

```markdown
back to the spine. ON Books keeps its own copy of every cover, so they show
even if the original disappears.

## Reading a book

Pick a book in the list to open it on the right.

- **Start reading** starts it today and moves it to Reading.
- **Finish** asks for the day you finished — today, unless you change it —
```

with:

```markdown
back to the spine. ON Books keeps its own copy of every cover, so they show
even if the original disappears.

A book without a cover offers **⋯ → Find cover**, which asks Open Library
for one straight away: by its ISBN, or by its title and author when it has
no ISBN or Open Library has no cover for it. If there's none to be had, ON
Books says so, and you can add one yourself with **Edit details**.

![The ⋯ menu of a book: Edit details, Find cover and Delete](images/books-menu.png)

Books with an ISBN but no cover — books you imported, say — get theirs in
the background, a few every few minutes, so a big import fills in over a
few hours. A cover you remove stays removed.

## Reading a book

Pick a book in the list to open it on the right. A long description is
cut short after a few lines; **More** shows the rest.

- **Start reading** starts it today and moves it to Reading.
- **Finish** asks for the day you finished — today, unless you change it —
```

In `docs/user/books.md`, replace:

```markdown
author and the rest; **Delete** removes the book for good, with its
readings, notes and quotes.

## Reading goal and stats

**Stats**, at the bottom of the sidebar, opens a page of numbers about your
```

with:

```markdown
author and the rest; **Delete** removes the book for good, with its
readings, notes and quotes.

## Importing from Goodreads

**Import from Goodreads**, at the bottom of the sidebar, brings your
Goodreads library over in one go. In Goodreads, open **My Books**, choose
**Import and export** under Tools, and click **Export Library**. Download
the file when it's ready, choose it on the import page and click
**Import**. Files up to 10 MB work — a few thousand books.

![The import page, with the steps and the upload box](images/books-import.png)

Each book comes over with:

- its shelf: *read* books are finished on the day you read them,
  *currently-reading* books are being read since the day you added them,
  and *to-read* books wait on Want to read. A book you read more than once
  gets an earlier reading, without dates, for each time before.
- your rating (a book you didn't rate has none) and your review;
- the day you added it, the number of pages, the year it first came out
  and its ISBN;
- its series, when Goodreads puts one in the title — "Leviathan Wakes
  (The Expanse, #1)" becomes Leviathan Wakes, The Expanse #1;
- its format, when the binding says — a Kindle edition is an ebook, an
  Audible one an audiobook, a paperback or hardcover paper;
- your other Goodreads shelves, as tags. A shelf of your own that takes
  the place of read or to-read, like *did-not-finish*, becomes a tag too,
  and the book goes on Want to read.

A book that's already in your library is skipped: the same ISBN, or —
when one of the two has no ISBN — the same title and author. So importing
the same file twice adds nothing new. When it's done, the page says how
many books came in, and which ones were skipped.

If something in the file can't be read, nothing is imported, and the page
says which line of the file is wrong.

## Exporting your books

**Export as Markdown**, at the bottom of the sidebar, downloads your whole
library as one file, `books-export.md`: every book with its details, your
readings, rating and review, notes and quotes, in alphabetical order.
Markdown is plain text, so it opens in any text editor and outlives any
app.

ON Books is also part of the [complete export](admin.md#exporting-someones-data)
an admin can make of everything you keep in ON Suite. Covers aren't in
either.

## Reading goal and stats

**Stats**, at the bottom of the sidebar, opens a page of numbers about your
```

In `docs/user/books.md`, replace:

```markdown
  longest and shortest book you finished.
- A chart of the books you finished each month.

Under **All time** are the same three numbers for everything you've read,
and a chart of the books you finished each year. A book you added as read
without a date — imported, say — counts toward All time but not toward any
```

with:

```markdown
  longest and shortest book you finished.
- A chart of the books you finished each month.

![The Stats page: this year's goal, numbers and a chart of books finished by month](images/books-stats.png)

Under **All time** are the same three numbers for everything you've read,
and a chart of the books you finished each year. A book you added as read
without a date — imported, say — counts toward All time but not toward any
```

In `README.md`, replace:

```markdown
<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/images/hero-dark.png">
    <img src="docs/images/hero-light.png" alt="The ON Suite dashboard, with a card for each app: ON Notes, ON Paste, ON Reader, ON Later, ON Flash and ON Focus" width="100%">
  </picture>
</p>
```

with:

```markdown
<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/images/hero-dark.png">
    <img src="docs/images/hero-light.png" alt="The ON Suite dashboard, with a card for each app: ON Notes, ON Paste, ON Reader, ON Later, ON Flash, ON Books and ON Focus" width="100%">
  </picture>
</p>
```

In `README.md`, replace:

```markdown
But I wanted something simple, something I can host myself, and something
that is _all in one place_.

ON Suite is a handful of small apps — notes, snippets, news feeds, flash
cards and focus timers — all on one private website. It is built for a small
group of people: a family, a group of friends, a small team. It can probably
support a few hundred people, maybe even several thousand. But it is not an
alternative to SaaS products like Gmail. Accounts are created by an admin, there is no
```

with:

```markdown
But I wanted something simple, something I can host myself, and something
that is _all in one place_.

ON Suite is a handful of small apps — notes, snippets, news feeds, a
read-it-later shelf, a reading log, flash cards and focus timers — all on
one private website. It is built for a small
group of people: a family, a group of friends, a small team. It can probably
support a few hundred people, maybe even several thousand. But it is not an
alternative to SaaS products like Gmail. Accounts are created by an admin, there is no
```

In `README.md`, replace:

```markdown

## The apps

Sign in once and move freely between six apps.

### ON Notes
```

with:

```markdown

## The apps

Sign in once and move freely between seven apps.

### ON Notes
```

In `README.md`, replace:

```markdown
someone else in your ON Suite instance.
[Read the ON Flash guide →](docs/user/flash.md)

### ON Focus

<p align="center">
```

with:

```markdown
someone else in your ON Suite instance.
[Read the ON Flash guide →](docs/user/flash.md)

### ON Books

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/images/app-books-dark.png">
    <img src="docs/images/app-books-light.png" alt="ON Books: shelves on the left, the books being read in the middle and Leviathan Wakes open on the right, with its progress and notes">
  </picture>
</p>

A private reading log: what you're reading, what you've read and what's
next. Find books on Open Library, track your progress page by page, rate
and review them, keep notes and quotes, and set a yearly goal. Bring your
library over from Goodreads, and download it all as Markdown.
[Read the ON Books guide →](docs/user/books.md)

### ON Focus

<p align="center">
```

In `AGENTS.md`, replace:

```markdown
extraction, and reading-stats), **ON Later** (a read-it-later app: save
pages, read them in a calm view, highlight, comment and tag them, export as
Markdown), **ON Books** (a private reading log: shelves, readings, tags,
Open Library search, progress, ratings and reviews, notes and quotes, and
full-text search, and a yearly goal with reading stats),
**ON Flash** (flash cards with FSRS review, import from
AI-written Markdown/JSON, media, household sharing and stats), and **ON
Focus** (saved, reusable focus timers — single blocks or Pomodoro-style
```

with:

```markdown
extraction, and reading-stats), **ON Later** (a read-it-later app: save
pages, read them in a calm view, highlight, comment and tag them, export as
Markdown), **ON Books** (a private reading log: shelves, readings, tags,
Open Library search, progress, ratings and reviews, notes and quotes,
full-text search, a yearly goal with reading stats, Goodreads import with
covers fetched in the background, and JSON and Markdown export),
**ON Flash** (flash cards with FSRS review, import from
AI-written Markdown/JSON, media, household sharing and stats), and **ON
Focus** (saved, reusable focus timers — single blocks or Pomodoro-style
```

- [ ] **Step 3: Shoot the screenshots**

Per [docs/screenshots/README.md](../../screenshots/README.md), with the version of the last release in the footer:

```bash
VERSION=4.1.0
SEED=$(mktemp -d)/demo
go run ./docs/screenshots/seed --data-dir $SEED
go build -ldflags "-X main.version=$VERSION" -o $SEED/onsuite ./cmd/onsuite
$SEED/onsuite serve --addr :8308 --data-dir $SEED &
SERVER=$!
until curl -fs http://localhost:8308/healthz >/dev/null; do sleep 0.2; done
go run ./docs/screenshots/capture --session-file $SEED/demo-session \
  --only books-panes.png,books-menu.png,books-add.png,books-stats.png,books-import.png,app-books-light.png,app-books-dark.png,hero-light.png,hero-dark.png
kill $SERVER
```
Expected: nine `wrote …` lines. Look at each image: the menu open with Find cover in `books-menu.png`, the goal card and Leviathan Wakes open in `books-panes.png`, seven cards in the hero.

- [ ] **Step 4: Run the full check and commit**

Run the full check from Global Constraints (`docs` checks every image link; the help tests load the guide).

```bash
git add docs README.md AGENTS.md
git commit -m "docs(books): import, Find cover and export in the guide; README section and screenshots (#491, #567)"
```

---

### Task 12: Check it in a browser and open the PR

- [ ] **Step 1: Seed a demo directory and start the server**

```bash
SEED=$(mktemp -d)/books-b5
go run ./docs/screenshots/seed --data-dir $SEED
go build -o $SEED/onsuite ./cmd/onsuite
echo $SEED
```
Add a `.claude/launch.json` configuration named `onsuite-books` (in the main checkout) running `<SEED>/onsuite serve --addr :8096 --data-dir <SEED>` on port 8096, start it with the preview tools and sign in as `demo` (password in `docs/screenshots/README.md`).

- [ ] **Step 2: Walk through it**

1. Sidebar: Stats, Import from Goodreads, Export as Markdown, in that order; a screen reader's text of a shelf link is "Want to read, 3 books".
2. Import page: choose `internal/apps/books/testdata/goodreads_library_export.csv` → "Imported 2 books." (Good Omens and The Odyssey) and five skipped: Leviathan Wakes, Piranesi, The Remains of the Day and Infinite Jest (the demo has them, without ISBNs) and the second Good Omens. Searching "fagles" finds The Odyssey. A non-CSV file: the message under the box, nothing imported.
3. /admin/jobs: "fetch book covers" is listed; Run now → after a few seconds The Odyssey has a cover (real Open Library); Good Omens, with no ISBN, doesn't.
4. Good Omens → ⋯ → Find cover: a cover, or the "didn't answer"/"no cover" banner. A book with a cover has no Find cover.
5. Export as Markdown downloads `books-export.md`; skim it.
6. A book with a long description (edit one and paste a few paragraphs): four lines and More; More shows the rest, Less folds it.
7. At 1024px the book pane is ~240px: the five stars are on one line under "Your rating". Click a star with the pane scrolled down: it stays put (#578).
8. At 375px: no horizontal scroll on the panes, the import page or the book pane; the stars and the More toggle fit.
9. `onsuite export demo --data-dir $SEED | jq '.apps.books.books | length'` is the library's size; /admin/ shows the ON Books card.
10. No console errors.

Fix anything found (with a test where one can be written), re-run the full check, commit.

- [ ] **Step 3: Remove the launch entry and push**

Remove the `onsuite-books` entry from `.claude/launch.json` (do not commit it), stop the server, then:

```bash
git push -u origin feat/books-b5-import-export
env -u GH_TOKEN gh pr create --title "feat(books): ON Books B5 — import and export (#491)" --body "$(cat <<'EOF'
B5 of ON Books, the last phase. Goodreads CSV import (`/books/import`): parsed in full first, one transaction, errors name the line; shelves, readings (Read Count > 1 adds undated earlier ones), rating, review, dates, series from the title, binding → format, other shelves → tags; duplicates skipped by ISBN, or by title and author (ignoring case, spaces and punctuation) when either side has no ISBN. A background job (`fetch book covers`, every 5 minutes, 25 books, in /admin/jobs with Run now) fetches covers by ISBN; migration 0007 adds `cover_checked_at` so a miss isn't retried. **Find cover** in the ⋯ menu does the same at once, falling back to a title/author search. `onsuite export` now includes Books (no cover bytes), the sidebar's **Export as Markdown** downloads `books-export.md`, and the admin page has a Books card.

Polish: long descriptions fold behind More (#566), shelf counts read as words (#568), the book pane keeps its scroll after a save (#578), the stars wrap together in a narrow pane (#579). User guide, README section and hero, AGENTS.md, screenshots (#567).

Spec: docs/superpowers/specs/2026-10-09-on-books-design.md
Plan: docs/superpowers/plans/2026-10-10-on-books-b5-import-export.md
Closes #491, #566, #567, #568, #578, #579.
EOF
)"
```
Never merge it.

---

## Self-review

- **Spec coverage.** Import page taking a Goodreads export → Tasks 1, 2, 5. Parsed fully before writing, one transaction, errors name the line → `ParseGoodreads` + `TestParseGoodreadsRefusesBadFiles`, `TestImportIsAllOrNothing`, the 422 page. Mapping: Title, Author + Additional Authors, ISBN13 → ISBN, pages, original → published year, rating 0 → none, review `<br/>`/tags/entities, Exclusive Shelf → readings, Read Count, Bookshelves → tags, Binding → format, Date Added, `="…"` → `TestParseGoodreadsMapsTheFixture` and its table tests. Duplicates (library and file) → `TestImportSkipsBooksAlreadyInTheLibrary`. Summary with counts and skipped titles → Task 5. FTS → `TestImportedBooksAreSearchable`. Backfill job (ISBN, no cover, not checked, a few per run, pause, miss sets `cover_checked_at`) → Task 3. Find cover (same lookup, title/author without an ISBN) → Task 4. `onsuite export` with books, readings, progress, rating, review, notes, quotes, tags, goals, no covers → Task 6. `books-export.md` → Task 7. Admin card → Task 8. Errors: Open Library failures shown and logged → Tasks 3–4; 404 for foreign ids → `TestFindCoverOfSomeoneElsesBookIsNotFound`. Testing: real SQLite, `apptest.Clock`, fixture with the listed awkward rows → Tasks 1–2. Docs and screenshots → Task 11. #566/#568/#579 → Task 9, #578 → Task 10, #567 → Task 11.
- **Placeholders.** None: every code step has the code; every run step its command and expected result.
- **Types.** `ImportBook`, `ImportError`, `ImportResult`, `CoverCandidate`, `ErrNoCover`, `exportPayload` and the test helpers are used with the same names and signatures wherever they appear.
