# ON Books B2 — Reading progress and history Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let people record where they are in a book (page, or percent for audio), see it as a bar in the book pane and the list, set each reading's format, rate a book and write a Markdown review, see and correct every reading of a book, and follow a book's series to the rest of it.

**Architecture:** One new table, `books_progress` (migration 0003), holds every progress update; the latest row of the latest reading is "the progress" and joins into `Get` and `List` (`progressJoin`). Store methods live in small files: `progress.go` (units, `RecordProgress`), `rating.go` (`SetRating`, `SetReview`), `history.go` (`SetFormat`, `Readings`, `UpdateReading`, `DeleteReading`); `FinishReading` gains a rating and `MarkDNF` a stopping place. The book pane gets a progress box that htmx swaps on its own (with the list out of band), a format pill menu, star buttons, a review disclosure, and the reading history — all plain form posts that work without JavaScript, as every Books control does. `renderPanes` takes a `paneOpts` so a refused progress update shows inside the box. A `series` list filter joins the list context. Reviews render through `RenderReview`, a mirror of ON Notes' inline Markdown renderer.

**Tech Stack:** Go 1.22+ `ServeMux`, `html/template`, htmx 2, SQLite via `modernc.org/sqlite`.

**Spec:** [docs/superpowers/specs/2026-10-09-on-books-design.md](../specs/2026-10-09-on-books-design.md) — "Data model" (`books_progress`, `books_readings`, rating/review, series), "Derived values", "Screens → Layout / Book pane / Progress and finishing / Keyboard", "Errors", "Phases" (B2 row). Builds on B1a ([plan](2026-10-09-on-books-b1a-library.md), #570) and B1b ([plan](2026-10-09-on-books-b1b-openlibrary.md), #572).

## Global Constraints

Decisions Ilia made on 2026-10-09 (binding; recorded in the spec in Task 10):

- **One B2 PR**, not split into B2a/B2b. The tasks below are separate commits on one branch.
- **Progress UI: bar only.** The book pane shows the current progress — the input, the bar, and when it was last updated. Every update is still stored in `books_progress` as history for B4's stats, but there is **no progress history list** in the UI.
- **Editing a past reading: dates and format only.** A reading's status is not editable; deleting a reading is how a wrong status is fixed. Every reading in the history list is editable and deletable.
- **Review editing is a `<details>` "Edit review" disclosure in the book pane** with a textarea. It works without JavaScript; with JavaScript htmx swaps the panes. It renders with the suite's existing Markdown approach: ON Notes' inline renderer, mirrored (apps never import each other; `goldmark` is contained to `internal/platform/help` by `TestGoldmarkIsContained`). **No new dependency.**
- **Settled with Ilia after the trial run (2026-10-09):** reviews render inline Markdown plus paragraphs and line breaks (no lists or headings); rating and review show on every book, Want to read included; finishing without a rating keeps the book's rating; the series count shows only from two books ("The Expanse #1" alone, "The Expanse #1 · 2 books").

From the spec:

- Progress is a page, or a percent for audio and for books with no page count (`UnitFor`). Percents are whole numbers 0–100; pages 0–the page count. Each update is one `books_progress` row with exactly one of `page`/`percent` set; the latest row is the current progress.
- The progress input saves on Enter via htmx and updates the bar in the book pane **and** the list row (the list comes back out of band). Reaching the last page does not finish the book.
- Bad progress (negative, past the page count, percent over 100, not a number, empty) is an inline message inside the progress box: "Enter a page from 0 to N." / "Enter a percentage from 0 to 100." htmx gets it as a 200 fragment; without JavaScript it is a 422 page.
- **Finish** takes an optional rating (1–5); **Did not finish** an optional page (or percent) where it stopped, recorded as the reading's last progress. Finishing without a rating keeps the book's rating.
- **Start reading** carries over the previous reading's format (already done in B1a). "Read again" on a read book (already done).
- Derived values stay as they are: one active reading per book (`books_readings_one_active`); the shelf comes from the latest reading by `created_at`.
- Reading shelf: sorted by latest progress (a reading with none by when it began); its rows show a progress bar and %. Read shelf rows show stars and the finish date.
- Rating: one per book, 1–5, `NULL` for none. Stars click to set and click again to clear — five submit buttons, so it works without JavaScript.
- Series link: "The Expanse #3 · 9 books" under the author; clicking filters the list to the series (`?series=`, the whole name, case-insensitive), in series-number order.
- Keyboard: `p` focuses the progress input.
- Missing or someone else's book or reading → 404, as everywhere. A tampered value from a button or select (rating 9, format "vinyl") → 400.
- CSP: no inline `<script>`, no `style=""` — bars are `<progress>` elements. App CSS goes in the "ON Books" section at the end of `internal/ui/static/app.css`, classes prefixed `books-`. Every new POST form has a real `action` and an identical `hx-post` (PATTERNS.md).
- Full check must stay green on every commit:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- Work on branch `feat/books-b2-progress` in the worktree `../on-suite-books-b2`, never on `main`. Open the PR with `env -u GH_TOKEN gh …`; never merge.

## Lessons from earlier plans (read before starting)

- staticcheck U1000 fails on an unexported helper (or struct field) added before its first use — every helper here comes in the task that first uses it (`active.startedOn` arrives in Task 2 with `closeReading`). staticcheck S1016 wants a type conversion instead of a struct literal copying identical fields — which is why `listCtx` and `ListQuery` keep the same fields in the same order (`ListQuery(c)`).
- `internal/htmlassert` supports one qualifier per selector: `#books-list .is-active` works, `.books-row.is-active` does not — and a two-class selector in `MustNotHave` passes without checking anything. Descendant selectors and `[attr="value"]` work; use `htmlassert.Attr` for a second condition.
- `go vet` rejects unkeyed composite literals of another package's struct types in `_test` packages.
- `TestMain` pins `time.Local` to Melbourne for this package; the fixture's default clock is already the next day there. A finish date after the store's `Today` is refused, so tests move the clock forward before finishing a reading on a later day.
- When order depends on timestamps (the Reading shelf), advance the pinned clock between steps — equal timestamps fall back to the reading id.
- Inside `{{range}}` in a template, `$` is the template's data, not the row.
- `docs/screenshots/seed` is compiled by `go vet ./...`: changing a store signature it calls (Task 2) means updating it in the same commit.
- gofmt re-aligns a whole struct when a longer field joins it; where that happens the plan shows the whole struct.
- Tests reach a loopback httptest server only through the app's test hook (`export_test.go`); B2 needs none.
- This plan's code was trial-run in a scratch worktree on 2026-10-09: Tasks 1–10 applied as written, each task's tests plus `go vet ./...` and staticcheck green at its own commit, the full check green at the end, and the seeded demo checked in a browser (progress over htmx with the list reordering, the format menu, stars, the series link, review and history editing, phone width). The plan's code steps were then re-applied mechanically, task by task, to a fresh worktree from `origin/main` and produced exactly the trial's tree at every task. That run is where the `#books-list .is-active` selector, the clock moves in `readTwice`, the outlined format pill and the "Esc closes any open disclosure in the book pane" rule came from.

## File map

| File | Responsibility |
|---|---|
| `internal/apps/books/migrations/0003_progress.sql` | `books_progress` |
| `internal/apps/books/progress.go` | `Unit`, `UnitFor`, `Progress`, `progressJoin`, `scanProgress`, `activeReading`, `checkProgress`, `insertProgress`, `RecordProgress` |
| `internal/apps/books/reading.go` | `FinishReading` with a rating, `MarkDNF` with a stopping place |
| `internal/apps/books/rating.go` | `MaxReviewRunes`, `checkRating`, `SetRating`, `SetReview` |
| `internal/apps/books/history.go` | `Formats`, `checkFormat`, `SetFormat`, `Readings`, `ReadingEdit`, `UpdateReading`, `DeleteReading` |
| `internal/apps/books/library.go` | progress, rating, format and series count on `Book`/`ListItem`; `ListQuery.Series`; list order |
| `internal/apps/books/markdown.go` | `RenderReview` (mirrors ON Notes' inline renderer) |
| `internal/apps/books/view.go` | series context and link; progress box, row bars and stars; format and rating choices; star buttons; review; history |
| `internal/apps/books/handlers.go` | `paneOpts`, `renderPanes` (progress-swap), readings for the pane |
| `internal/apps/books/actions.go` | `formInt`, `progress`, `setFormat`, `setRating`, `setReview`, `readingID`, `editReading`, `deleteReading` |
| `internal/apps/books/books.go` | routes |
| `internal/apps/books/templates/panes.partial.html` | series link and context; progress box; format menu; Finish rating / DNF page; rating; review; history |
| `internal/apps/books/static/books.js` | `series` in the synced context; `p`; Esc for every disclosure in the book pane |
| `internal/ui/static/app.css` | progress, stars, format pill, rating, review, history |
| `internal/apps/books/*_test.go` | tests |
| `docs/screenshots/seed/books.go`, `seed_test.go` | demo progress, formats, ratings, a review, a second series book |
| `docs/user/books.md`, the spec, `AGENTS.md` | guide, decisions, app list |

---

### Task 0: Worktree and branch

- [ ] **Step 1: Create the worktree**

```bash
cd /Users/iliaf/src/WEB/on-suite
git fetch origin
git worktree add ../on-suite-books-b2 -b feat/books-b2-progress origin/main
cd ../on-suite-books-b2
go build ./cmd/onsuite && rm -f onsuite
```
Expected: builds with no output. All later commands run in `../on-suite-books-b2`.

---

### Task 1: Progress in the store

**Files:**
- Create: `internal/apps/books/migrations/0003_progress.sql`, `internal/apps/books/progress.go`
- Modify: `internal/apps/books/library.go` (`Book`, `Get`, `ListItem`, `listOrder`, `List`)
- Test: `internal/apps/books/progress_test.go`

**Interfaces:**
- Consumes: `touch`, `formatTime`, `parseTime`, `Refusal`, `ErrNotFound` (store.go); `latestJoin`, `shelfExpr` (library.go); test helpers `newFixture`, `addBook`, `onShelf`, `getBook` (library_test.go/store_test.go), `list`, `titles` (shelf_test.go).
- Produces:
  - `type Unit string`; `UnitPage Unit = "page"`, `UnitPercent Unit = "percent"`; `func UnitFor(format string, pages int) Unit`
  - `type Progress struct { Unit Unit; Value int; RecordedAt time.Time }` with `Set() bool`, `In(u Unit, pages int) int`, `Percent(pages int) int` (0–100)
  - `(*Store).RecordProgress(ctx context.Context, userID, id int64, value int) error` — `*Refusal` for a value out of range or no reading in progress
  - `Book.Progress Progress` (the latest reading's latest row); `ListItem.Pages int`, `ListItem.Format string`, `ListItem.Progress Progress`
  - unexported, used by later tasks: `progressJoin` (SQL, alias `p`), `scanProgress(page, percent sql.NullInt64, at sql.NullString) (Progress, error)`, `type active struct{ id int64; format string; pages int }`, `activeReading(ctx, tx *sql.Tx, id int64) (active, error)`, `checkProgress(u Unit, pages, value int) error`, `insertProgress(ctx, tx *sql.Tx, readingID int64, u Unit, value int, at string) error`
  - test helpers: `inProgress(t, f, title string, pages int) int64` (Alice's book, on the go), `setFormat(t, f, id int64, format string)` (raw SQL)

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/progress_test.go`:

```go
package books_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// inProgress adds one of Alice's books with pages pages (0: unknown) and
// starts it, so it has a reading in progress.
func inProgress(t *testing.T, f *fixture, title string, pages int) int64 {
	t.Helper()
	nb := onShelf(title, books.ShelfReading)
	nb.Pages = pages
	return addBook(t, f, f.alice.ID, nb)
}

func setFormat(t *testing.T, f *fixture, id int64, format string) {
	t.Helper()
	if _, err := f.db.Exec(`UPDATE books_readings SET format = ? WHERE book_id = ?`, format, id); err != nil {
		t.Fatal(err)
	}
}

func TestUnitFor(t *testing.T) {
	tests := []struct {
		format string
		pages  int
		want   books.Unit
	}{
		{"", 300, books.UnitPage},
		{"paper", 300, books.UnitPage},
		{"ebook", 300, books.UnitPage},
		{"audio", 300, books.UnitPercent},
		{"paper", 0, books.UnitPercent},
	}
	for _, tt := range tests {
		if got := books.UnitFor(tt.format, tt.pages); got != tt.want {
			t.Errorf("UnitFor(%q, %d) = %q, want %q", tt.format, tt.pages, got, tt.want)
		}
	}
}

func TestProgressConvertsBetweenUnits(t *testing.T) {
	page := books.Progress{Unit: books.UnitPage, Value: 150}
	percent := books.Progress{Unit: books.UnitPercent, Value: 40}
	tests := []struct {
		name string
		got  int
		want int
	}{
		{"page in pages", page.In(books.UnitPage, 300), 150},
		{"page in percent", page.In(books.UnitPercent, 300), 50},
		{"page in percent, no page count", page.In(books.UnitPercent, 0), 0},
		{"percent in pages", percent.In(books.UnitPage, 300), 120},
		{"percent in percent", percent.In(books.UnitPercent, 0), 40},
		{"nothing recorded", books.Progress{}.In(books.UnitPage, 300), 0},
		{"bar past the end", books.Progress{Unit: books.UnitPage, Value: 320}.Percent(300), 100},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

func TestRecordProgressInPages(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := inProgress(t, f, "Dune", 600)
	if p := getBook(t, f, f.alice.ID, id).Progress; p.Set() {
		t.Fatalf("new reading has progress %+v", p)
	}
	for _, page := range []int{40, 120} {
		f.now = f.now.Add(time.Hour)
		if err := f.store.RecordProgress(ctx, f.alice.ID, id, page); err != nil {
			t.Fatal(err)
		}
	}
	p := getBook(t, f, f.alice.ID, id).Progress
	if p.Unit != books.UnitPage || p.Value != 120 || !p.RecordedAt.Equal(f.now) || p.Percent(600) != 20 {
		t.Errorf("Progress = %+v, want page 120 at %v (20%%)", p, f.now)
	}
	var rows int
	if err := f.db.QueryRow(`SELECT count(*) FROM books_progress`).Scan(&rows); err != nil || rows != 2 {
		t.Errorf("progress rows = %d, %v; want both updates kept as history", rows, err)
	}
}

func TestRecordProgressInPercent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	audio := inProgress(t, f, "Dune", 600)
	setFormat(t, f, audio, "audio")
	unknown := inProgress(t, f, "Emma", 0)
	for _, id := range []int64{audio, unknown} {
		if err := f.store.RecordProgress(ctx, f.alice.ID, id, 35); err != nil {
			t.Fatal(err)
		}
		if p := getBook(t, f, f.alice.ID, id).Progress; p.Unit != books.UnitPercent || p.Value != 35 {
			t.Errorf("book %d progress = %+v, want 35%%", id, p)
		}
	}
}

func TestRecordProgressRefusesOutOfRange(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	paper := inProgress(t, f, "Dune", 600)
	audio := inProgress(t, f, "Emma", 300)
	setFormat(t, f, audio, "audio")
	tests := []struct {
		id    int64
		value int
		want  string
	}{
		{paper, -1, "Enter a page from 0 to 600."},
		{paper, 601, "Enter a page from 0 to 600."},
		{audio, 101, "Enter a percentage from 0 to 100."},
	}
	for _, tt := range tests {
		err := f.store.RecordProgress(ctx, f.alice.ID, tt.id, tt.value)
		var ref *books.Refusal
		if !errors.As(err, &ref) || ref.Msg != tt.want {
			t.Errorf("RecordProgress(%d) = %v, want a Refusal %q", tt.value, err, tt.want)
		}
	}
	if err := f.store.RecordProgress(ctx, f.alice.ID, paper, 600); err != nil {
		t.Errorf("the last page = %v, want it accepted", err)
	}
	if b := getBook(t, f, f.alice.ID, paper); b.Shelf != books.ShelfReading {
		t.Errorf("shelf after the last page = %q, want reading: reaching the end doesn't finish", b.Shelf)
	}
}

func TestRecordProgressNeedsAReadingInProgress(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	want := addBook(t, f, f.alice.ID, onShelf("Emma", books.ShelfWant))
	var ref *books.Refusal
	if err := f.store.RecordProgress(ctx, f.alice.ID, want, 10); !errors.As(err, &ref) || !strings.Contains(ref.Msg, "isn't being read") {
		t.Errorf("progress on a want-to-read book = %v, want a Refusal", err)
	}
	id := inProgress(t, f, "Dune", 600)
	if err := f.store.RecordProgress(ctx, f.bob.ID, id, 10); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's RecordProgress = %v, want ErrNotFound", err)
	}
}

func TestReadingShelfIsSortedByLatestProgress(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first := inProgress(t, f, "First started", 300)
	f.now = f.now.Add(time.Hour)
	inProgress(t, f, "Second started", 300)
	f.now = f.now.Add(time.Hour)
	if got := list(t, f, books.ListQuery{Shelf: books.ShelfReading}); !slices.Equal(got, []string{"Second started", "First started"}) {
		t.Fatalf("before any progress = %v, want the newest reading first", got)
	}
	if err := f.store.RecordProgress(ctx, f.alice.ID, first, 30); err != nil {
		t.Fatal(err)
	}
	items, err := f.store.List(ctx, f.alice.ID, books.ListQuery{Shelf: books.ShelfReading})
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(items); !slices.Equal(got, []string{"First started", "Second started"}) {
		t.Errorf("after progress = %v, want the book just updated first", got)
	}
	if it := items[0]; it.Pages != 300 || it.Progress.Value != 30 || it.Progress.Percent(it.Pages) != 10 {
		t.Errorf("row = %+v, want 300 pages at page 30 (10%%)", it)
	}
}

func TestDeletingABookTakesItsProgress(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := inProgress(t, f, "Dune", 600)
	if err := f.store.RecordProgress(ctx, f.alice.ID, id, 10); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Delete(ctx, f.alice.ID, id); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM books_progress`).Scan(&n); err != nil || n != 0 {
		t.Errorf("progress rows left = %d, %v; want 0", n, err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `undefined: books.UnitFor`, `books.Progress`, `f.store.RecordProgress` and friends.

- [ ] **Step 3: Write the migration**

Create `internal/apps/books/migrations/0003_progress.sql`:

```sql
-- Where a reading stands, every time it is updated (spec "Data model"):
-- the latest row is the current progress, the rest is history for the
-- stats (B4). A reading's unit follows its format — percent for audio, or
-- when the book has no page count; pages otherwise — so each row sets
-- exactly one of page and percent. Rows go with their reading.
CREATE TABLE books_progress (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    reading_id  INTEGER NOT NULL REFERENCES books_readings (id) ON DELETE CASCADE,
    page        INTEGER CHECK (page >= 0),
    percent     INTEGER CHECK (percent BETWEEN 0 AND 100),
    recorded_at TEXT    NOT NULL,
    CHECK ((page IS NULL) <> (percent IS NULL))
) STRICT;

CREATE INDEX books_progress_reading_idx ON books_progress (reading_id, recorded_at);
```

- [ ] **Step 4: Write `progress.go`**

Create `internal/apps/books/progress.go`:

```go
package books

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Unit is what a reading's progress is counted in.
type Unit string

// Progress units (spec "Data model": books_progress).
const (
	UnitPage    Unit = "page"
	UnitPercent Unit = "percent"
)

// UnitFor is a reading's unit: audio is counted in percent, and so is any
// book without a page count; everything else in pages.
func UnitFor(format string, pages int) Unit {
	if format == "audio" || pages <= 0 {
		return UnitPercent
	}
	return UnitPage
}

// Progress is a reading's latest progress row. Unit is the unit that row
// was recorded in, which is not always the reading's unit now — its format
// or the book's page count may have changed since — so read it through In.
type Progress struct {
	Unit       Unit // "" when nothing has been recorded
	Value      int
	RecordedAt time.Time
}

// Set reports whether any progress has been recorded.
func (p Progress) Set() bool { return p.Unit != "" }

// In is the progress in unit u for a book of pages pages. Converting
// between pages and percent needs a page count; without one it is 0.
func (p Progress) In(u Unit, pages int) int {
	switch {
	case !p.Set():
		return 0
	case p.Unit == u:
		return p.Value
	case pages <= 0:
		return 0
	case u == UnitPercent:
		return p.Value * 100 / pages
	default:
		return p.Value * pages / 100
	}
}

// Percent is how far through the book p is, 0–100, for a progress bar.
func (p Progress) Percent(pages int) int { return min(100, max(0, p.In(UnitPercent, pages))) }

// progressJoin attaches p, the latest progress row of r (see latestJoin).
const progressJoin = `LEFT JOIN books_progress p ON p.id = (
	SELECT id FROM books_progress WHERE reading_id = r.id ORDER BY recorded_at DESC, id DESC LIMIT 1)`

// scanProgress turns p's nullable columns into a Progress.
func scanProgress(page, percent sql.NullInt64, at sql.NullString) (Progress, error) {
	var p Progress
	switch {
	case page.Valid:
		p.Unit, p.Value = UnitPage, int(page.Int64)
	case percent.Valid:
		p.Unit, p.Value = UnitPercent, int(percent.Int64)
	default:
		return Progress{}, nil
	}
	t, err := parseTime(at.String)
	if err != nil {
		return Progress{}, fmt.Errorf("books: recorded_at: %w", err)
	}
	p.RecordedAt = t
	return p, nil
}

// active is a book's reading in progress, with what its unit depends on.
type active struct {
	id     int64
	format string
	pages  int
}

// activeReading finds the reading in progress of book id inside tx; a
// Refusal when there is none.
func activeReading(ctx context.Context, tx *sql.Tx, id int64) (active, error) {
	var a active
	err := tx.QueryRowContext(ctx, `
		SELECT r.id, COALESCE(r.format, ''), COALESCE(b.pages, 0)
		  FROM books_readings r JOIN books_books b ON b.id = r.book_id
		 WHERE r.book_id = ? AND r.status = 'reading'`, id).Scan(&a.id, &a.format, &a.pages)
	if errors.Is(err, sql.ErrNoRows) {
		return active{}, &Refusal{Msg: "This book isn't being read right now."}
	}
	if err != nil {
		return active{}, fmt.Errorf("books: reading in progress: %w", err)
	}
	return a, nil
}

// checkProgress refuses a value outside its unit's range (spec "Errors":
// negative, past the page count, percent over 100).
func checkProgress(u Unit, pages, value int) error {
	if u == UnitPercent {
		if value < 0 || value > 100 {
			return &Refusal{Msg: "Enter a percentage from 0 to 100."}
		}
		return nil
	}
	if value < 0 || value > pages {
		return &Refusal{Msg: fmt.Sprintf("Enter a page from 0 to %d.", pages)}
	}
	return nil
}

// insertProgress adds a progress row in unit u.
func insertProgress(ctx context.Context, tx *sql.Tx, readingID int64, u Unit, value int, at string) error {
	var page, percent any
	if u == UnitPage {
		page = value
	} else {
		percent = value
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO books_progress (reading_id, page, percent, recorded_at) VALUES (?, ?, ?, ?)`,
		readingID, page, percent, at); err != nil {
		return fmt.Errorf("books: record progress: %w", err)
	}
	return nil
}

// RecordProgress notes where the reading in progress stands: a page, or a
// percentage for audio and for books with no page count (UnitFor). Every
// call adds a row — the history B4's stats count pages from (spec "Derived
// values") — and the newest one is the current progress. Reaching the last
// page doesn't finish the book (spec "Progress and finishing").
func (st *Store) RecordProgress(ctx context.Context, userID, id int64, value int) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin progress: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	a, err := activeReading(ctx, tx, id)
	if err != nil {
		return err
	}
	u := UnitFor(a.format, a.pages)
	if err := checkProgress(u, a.pages, value); err != nil {
		return err
	}
	if err := insertProgress(ctx, tx, a.id, u, value, formatTime(st.now())); err != nil {
		return err
	}
	return tx.Commit()
}
```

- [ ] **Step 5: Join the latest progress into `Get` and `List`, and sort Reading by it**

In `internal/apps/books/library.go`, replace:

```go
type Reading struct {
	ID         int64
	Status     Status
	Format     string // "", "paper", "ebook" or "audio" (set from B2)
	StartedOn  string
	FinishedOn string
}
```

with:

```go
type Reading struct {
	ID         int64
	Status     Status
	Format     string // "", "paper", "ebook" or "audio"
	StartedOn  string
	FinishedOn string
}
```

In `internal/apps/books/library.go`, replace:

```go
	Review             string // Markdown (set from B2)
	Tags               []string
	Shelf              Shelf
	Latest             Reading // zero ID before the first reading
	CoverVersion       string  // "" when the book has no cover
	AddedAt, UpdatedAt time.Time
}

```

with:

```go
	Review             string // Markdown (set from B2)
	Tags               []string
	Shelf              Shelf
	Latest             Reading  // zero ID before the first reading
	Progress           Progress // the latest reading's latest progress
	CoverVersion       string   // "" when the book has no cover
	AddedAt, UpdatedAt time.Time
}

```

In `internal/apps/books/library.go`, replace:

```go
// Get returns one of userID's books with its tags and latest reading.
func (st *Store) Get(ctx context.Context, userID, id int64) (Book, error) {
	var b Book
	var year, pages, rating, rid sql.NullInt64
	var isbn, status, format, started, finished, cover sql.NullString
	var added, updated, shelf string
	err := st.db.QueryRowContext(ctx, `
		SELECT b.id, b.title, b.subtitle, b.authors, b.year, b.pages, b.isbn13, b.series_name,
		       b.series_number, b.description, b.rating, b.review, b.added_at, b.updated_at,
		       r.id, r.status, r.format, r.started_on, r.finished_on, `+shelfExpr+`, c.fetched_at
		  FROM books_books b `+latestJoin+`
		  LEFT JOIN books_covers c ON c.book_id = b.id
		 WHERE b.id = ? AND b.user_id = ?`, id, userID).Scan(
		&b.ID, &b.Title, &b.Subtitle, &b.Authors, &year, &pages, &isbn, &b.SeriesName,
		&b.SeriesNumber, &b.Description, &rating, &b.Review, &added, &updated,
		&rid, &status, &format, &started, &finished, &shelf, &cover)
	if errors.Is(err, sql.ErrNoRows) {
		return Book{}, ErrNotFound
	}
```

with:

```go
// Get returns one of userID's books with its tags and latest reading.
func (st *Store) Get(ctx context.Context, userID, id int64) (Book, error) {
	var b Book
	var year, pages, rating, rid, atPage, atPercent sql.NullInt64
	var isbn, status, format, started, finished, cover, recorded sql.NullString
	var added, updated, shelf string
	err := st.db.QueryRowContext(ctx, `
		SELECT b.id, b.title, b.subtitle, b.authors, b.year, b.pages, b.isbn13, b.series_name,
		       b.series_number, b.description, b.rating, b.review, b.added_at, b.updated_at,
		       r.id, r.status, r.format, r.started_on, r.finished_on, `+shelfExpr+`, c.fetched_at,
		       p.page, p.percent, p.recorded_at
		  FROM books_books b `+latestJoin+`
		  `+progressJoin+`
		  LEFT JOIN books_covers c ON c.book_id = b.id
		 WHERE b.id = ? AND b.user_id = ?`, id, userID).Scan(
		&b.ID, &b.Title, &b.Subtitle, &b.Authors, &year, &pages, &isbn, &b.SeriesName,
		&b.SeriesNumber, &b.Description, &rating, &b.Review, &added, &updated,
		&rid, &status, &format, &started, &finished, &shelf, &cover,
		&atPage, &atPercent, &recorded)
	if errors.Is(err, sql.ErrNoRows) {
		return Book{}, ErrNotFound
	}
```

In `internal/apps/books/library.go`, replace:

```go
		StartedOn: started.String, FinishedOn: finished.String}
	b.Shelf = Shelf(shelf)
	b.CoverVersion = coverVersion(cover.String)
	if b.AddedAt, err = parseTime(added); err != nil {
		return Book{}, fmt.Errorf("books: added_at: %w", err)
	}
```

with:

```go
		StartedOn: started.String, FinishedOn: finished.String}
	b.Shelf = Shelf(shelf)
	b.CoverVersion = coverVersion(cover.String)
	if b.Progress, err = scanProgress(atPage, atPercent, recorded); err != nil {
		return Book{}, err
	}
	if b.AddedAt, err = parseTime(added); err != nil {
		return Book{}, fmt.Errorf("books: added_at: %w", err)
	}
```

In `internal/apps/books/library.go`, replace:

```go
type ListItem struct {
	ID                                       int64
	Title, Authors, SeriesName, SeriesNumber string
	Shelf                                    Shelf
	StartedOn, FinishedOn                    string // the latest reading's
	AddedAt                                  time.Time
	CoverVersion                             string // "" when the book has no cover
}
```

with:

```go
type ListItem struct {
	ID                                       int64
	Title, Authors, SeriesName, SeriesNumber string
	Pages                                    int
	Shelf                                    Shelf
	StartedOn, FinishedOn, Format            string   // the latest reading's
	Progress                                 Progress // the latest reading's latest progress
	AddedAt                                  time.Time
	CoverVersion                             string // "" when the book has no cover
}
```

In `internal/apps/books/library.go`, replace:

```go
	Q     string
}

// listOrder is each shelf's sort (spec "Layout"): Reading by start (B2
// switches it to latest progress), Read by finish, Want to read by date
// added, DNF and All by the latest change. NULL dates sort last.
func listOrder(s Shelf) string {
	switch s {
	case ShelfReading:
		return `r.started_on DESC, r.id DESC`
	case ShelfRead:
		return `r.finished_on DESC, r.id DESC`
	case ShelfWant:
```

with:

```go
	Q     string
}

// listOrder is each shelf's sort (spec "Layout"): Reading by the latest
// progress (a reading with none yet by when it started), Read by finish,
// Want to read by date added, DNF and All by the latest change. NULL dates
// sort last.
func listOrder(s Shelf) string {
	switch s {
	case ShelfReading:
		return `COALESCE(p.recorded_at, r.created_at) DESC, r.id DESC`
	case ShelfRead:
		return `r.finished_on DESC, r.id DESC`
	case ShelfWant:
```

In `internal/apps/books/library.go`, replace:

```go
		args = append(args, pat, pat, pat, pat)
	}
	rows, err := st.db.QueryContext(ctx, `
		SELECT b.id, b.title, b.authors, b.series_name, b.series_number, b.added_at,
		       r.started_on, r.finished_on, `+shelfExpr+`, c.fetched_at
		  FROM books_books b `+latestJoin+`
		  LEFT JOIN books_covers c ON c.book_id = b.id
		 WHERE `+strings.Join(where, " AND ")+`
		 ORDER BY `+listOrder(q.Shelf), args...)
```

with:

```go
		args = append(args, pat, pat, pat, pat)
	}
	rows, err := st.db.QueryContext(ctx, `
		SELECT b.id, b.title, b.authors, b.series_name, b.series_number, b.pages, b.added_at,
		       r.started_on, r.finished_on, r.format, `+shelfExpr+`, c.fetched_at,
		       p.page, p.percent, p.recorded_at
		  FROM books_books b `+latestJoin+`
		  `+progressJoin+`
		  LEFT JOIN books_covers c ON c.book_id = b.id
		 WHERE `+strings.Join(where, " AND ")+`
		 ORDER BY `+listOrder(q.Shelf), args...)
```

In `internal/apps/books/library.go`, replace:

```go
	for rows.Next() {
		var it ListItem
		var added, shelf string
		var started, finished, cover sql.NullString
		if err := rows.Scan(&it.ID, &it.Title, &it.Authors, &it.SeriesName, &it.SeriesNumber, &added,
			&started, &finished, &shelf, &cover); err != nil {
			return nil, fmt.Errorf("books: scan list: %w", err)
		}
		if it.AddedAt, err = parseTime(added); err != nil {
			return nil, fmt.Errorf("books: added_at: %w", err)
		}
		it.StartedOn, it.FinishedOn, it.Shelf = started.String, finished.String, Shelf(shelf)
		it.CoverVersion = coverVersion(cover.String)
		out = append(out, it)
	}
```

with:

```go
	for rows.Next() {
		var it ListItem
		var added, shelf string
		var pages, atPage, atPercent sql.NullInt64
		var started, finished, format, cover, recorded sql.NullString
		if err := rows.Scan(&it.ID, &it.Title, &it.Authors, &it.SeriesName, &it.SeriesNumber, &pages, &added,
			&started, &finished, &format, &shelf, &cover, &atPage, &atPercent, &recorded); err != nil {
			return nil, fmt.Errorf("books: scan list: %w", err)
		}
		if it.AddedAt, err = parseTime(added); err != nil {
			return nil, fmt.Errorf("books: added_at: %w", err)
		}
		if it.Progress, err = scanProgress(atPage, atPercent, recorded); err != nil {
			return nil, err
		}
		it.Pages = int(pages.Int64)
		it.StartedOn, it.FinishedOn, it.Format, it.Shelf = started.String, finished.String, format.String, Shelf(shelf)
		it.CoverVersion = coverVersion(cover.String)
		out = append(out, it)
	}
```

- [ ] **Step 6: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
```
Expected: all pass, no output from vet or staticcheck. (`TestListShowsOneShelfInItsOrder` still passes: with no progress, Reading falls back to when each reading began.)

- [ ] **Step 7: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): reading progress in the store (#488)"
```

---

### Task 2: Rate on finish, a stopping place on DNF, rating and review in the store

**Files:**
- Create: `internal/apps/books/rating.go`
- Modify: `internal/apps/books/reading.go` (`FinishReading`, `MarkDNF`, `closeReading`), `internal/apps/books/progress.go` (`active.startedOn`), `internal/apps/books/library.go` (`Book` comments, `ListItem.Rating`, `List`), `internal/apps/books/actions.go` (`formInt`, `finish`, `dnf`), `docs/screenshots/seed/books.go` (new signatures)
- Test: `internal/apps/books/rating_test.go`, `internal/apps/books/shelf_test.go` (call sites), `internal/apps/books/actions_test.go`

**Interfaces:**
- Consumes: Task 1's `activeReading`, `UnitFor`, `checkProgress`, `insertProgress`, `inProgress`; `nullInt` (library.go); `act` (actions.go); test helpers `newServer`, `add`, `titled` (handlers_test.go).
- Produces:
  - `(*Store).FinishReading(ctx, userID, id int64, day string, rating int) error` — rating 1–5 rates the book, 0 keeps its rating, anything else `ErrInvalid`
  - `(*Store).MarkDNF(ctx, userID, id int64, day string, at int) error` — `at` ≠ 0 is recorded as a last progress row in the reading's unit (checked like any progress)
  - `const MaxReviewRunes = 20000`; `(*Store).SetRating(ctx, userID, id int64, rating int) error` (0 clears; outside 0–5 `ErrInvalid`); `(*Store).SetReview(ctx, userID, id int64, review string) error` (CRLF → LF, trimmed; too long → `*Refusal`)
  - `ListItem.Rating int`; `active.startedOn string`
  - unexported: `checkRating(rating int) error`; `formInt(r *http.Request, name string) int` — 0 for an empty field, -1 for one that isn't a number
  - form fields: `rating` on `POST /books/finish/{id}`, `at` on `POST /books/dnf/{id}`

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/rating_test.go`:

```go
package books_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

func TestFinishCanRateTheBook(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := inProgress(t, f, "Dune", 600)
	if err := f.store.FinishReading(ctx, f.alice.ID, id, "", 4); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Rating != 4 || b.Shelf != books.ShelfRead {
		t.Errorf("after finishing with 4 stars: rating %d, shelf %q", b.Rating, b.Shelf)
	}
	// A re-read finished without a rating keeps the one it had.
	f.now = f.now.Add(time.Minute)
	if err := f.store.StartReading(ctx, f.alice.ID, id); err != nil {
		t.Fatal(err)
	}
	if err := f.store.FinishReading(ctx, f.alice.ID, id, "", 0); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Rating != 4 {
		t.Errorf("rating after an unrated re-read = %d, want 4 kept", b.Rating)
	}
}

func TestFinishRefusesABadRating(t *testing.T) {
	f := newFixture(t)
	id := inProgress(t, f, "Dune", 600)
	for _, rating := range []int{-1, 6} {
		if err := f.store.FinishReading(context.Background(), f.alice.ID, id, "", rating); !errors.Is(err, books.ErrInvalid) {
			t.Errorf("FinishReading rating %d = %v, want ErrInvalid", rating, err)
		}
	}
	if b := getBook(t, f, f.alice.ID, id); b.Shelf != books.ShelfReading {
		t.Errorf("shelf = %q after refused finishes, want reading", b.Shelf)
	}
}

func TestDNFCanSayWhereItStopped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := inProgress(t, f, "Infinite Jest", 1079)
	var ref *books.Refusal
	if err := f.store.MarkDNF(ctx, f.alice.ID, id, "", 2000); !errors.As(err, &ref) || ref.Msg != "Enter a page from 0 to 1079." {
		t.Errorf("DNF past the last page = %v, want a Refusal", err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Shelf != books.ShelfReading || b.Progress.Set() {
		t.Fatalf("after a refused DNF: shelf %q, progress %+v; want nothing changed", b.Shelf, b.Progress)
	}
	if err := f.store.MarkDNF(ctx, f.alice.ID, id, "", 250); err != nil {
		t.Fatal(err)
	}
	b := getBook(t, f, f.alice.ID, id)
	if b.Shelf != books.ShelfDNF || b.Progress.Unit != books.UnitPage || b.Progress.Value != 250 {
		t.Errorf("after DNF at 250: shelf %q, progress %+v", b.Shelf, b.Progress)
	}
}

func TestSetRating(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfRead))
	if err := f.store.SetRating(ctx, f.alice.ID, id, 5); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Rating != 5 {
		t.Errorf("rating = %d, want 5", b.Rating)
	}
	items, err := f.store.List(ctx, f.alice.ID, books.ListQuery{Shelf: books.ShelfRead})
	if err != nil || len(items) != 1 || items[0].Rating != 5 {
		t.Errorf("List = %+v, %v; want the rating on the row", items, err)
	}
	if err := f.store.SetRating(ctx, f.alice.ID, id, 0); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Rating != 0 {
		t.Errorf("rating after clearing = %d, want 0", b.Rating)
	}
	if err := f.store.SetRating(ctx, f.alice.ID, id, 6); !errors.Is(err, books.ErrInvalid) {
		t.Errorf("SetRating(6) = %v, want ErrInvalid", err)
	}
	if err := f.store.SetRating(ctx, f.bob.ID, id, 3); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's SetRating = %v, want ErrNotFound", err)
	}
}

func TestSetReview(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfRead))
	if err := f.store.SetReview(ctx, f.alice.ID, id, "  Sand.\r\n\r\nSo much **sand**.  \n"); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Review != "Sand.\n\nSo much **sand**." {
		t.Errorf("review = %q", b.Review)
	}
	var ref *books.Refusal
	if err := f.store.SetReview(ctx, f.alice.ID, id, strings.Repeat("a", books.MaxReviewRunes+1)); !errors.As(err, &ref) {
		t.Errorf("an over-long review = %v, want a Refusal", err)
	}
	if err := f.store.SetReview(ctx, f.bob.ID, id, "mine now"); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's SetReview = %v, want ErrNotFound", err)
	}
	if err := f.store.SetReview(ctx, f.alice.ID, id, ""); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Review != "" {
		t.Errorf("review after clearing = %q", b.Review)
	}
}
```

The existing store tests call the old signatures. Update every call in `shelf_test.go`:

In `internal/apps/books/shelf_test.go`, replace:

```go
		}
	}
	step("start", func() error { return f.store.StartReading(ctx, f.alice.ID, id) }, books.ShelfReading)
	step("finish", func() error { return f.store.FinishReading(ctx, f.alice.ID, id, "") }, books.ShelfRead)
	step("re-read", func() error { return f.store.StartReading(ctx, f.alice.ID, id) }, books.ShelfReading)
	step("give up", func() error { return f.store.MarkDNF(ctx, f.alice.ID, id, "") }, books.ShelfDNF)
}

func TestStartRefusesASecondReadingInProgress(t *testing.T) {
```

with:

```go
		}
	}
	step("start", func() error { return f.store.StartReading(ctx, f.alice.ID, id) }, books.ShelfReading)
	step("finish", func() error { return f.store.FinishReading(ctx, f.alice.ID, id, "", 0) }, books.ShelfRead)
	step("re-read", func() error { return f.store.StartReading(ctx, f.alice.ID, id) }, books.ShelfReading)
	step("give up", func() error { return f.store.MarkDNF(ctx, f.alice.ID, id, "", 0) }, books.ShelfDNF)
}

func TestStartRefusesASecondReadingInProgress(t *testing.T) {
```

In `internal/apps/books/shelf_test.go`, replace:

```go
		"2026-10-10": "in the future",
		"soon":       "Enter a date",
	} {
		err := f.store.FinishReading(ctx, f.alice.ID, id, day)
		var ref *books.Refusal
		if !errors.As(err, &ref) || !strings.Contains(ref.Msg, want) {
			t.Errorf("FinishReading(%q) = %v, want a Refusal saying %q", day, err, want)
		}
	}
	if err := f.store.FinishReading(ctx, f.alice.ID, id, "2026-10-05"); err != nil {
		t.Fatal(err)
	}
	b := getBook(t, f, f.alice.ID, id)
```

with:

```go
		"2026-10-10": "in the future",
		"soon":       "Enter a date",
	} {
		err := f.store.FinishReading(ctx, f.alice.ID, id, day, 0)
		var ref *books.Refusal
		if !errors.As(err, &ref) || !strings.Contains(ref.Msg, want) {
			t.Errorf("FinishReading(%q) = %v, want a Refusal saying %q", day, err, want)
		}
	}
	if err := f.store.FinishReading(ctx, f.alice.ID, id, "2026-10-05", 0); err != nil {
		t.Fatal(err)
	}
	b := getBook(t, f, f.alice.ID, id)
```

In `internal/apps/books/shelf_test.go`, replace:

```go
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfWant))
	var ref *books.Refusal
	if err := f.store.MarkDNF(context.Background(), f.alice.ID, id, ""); !errors.As(err, &ref) {
		t.Errorf("MarkDNF on a want-to-read book = %v, want a Refusal", err)
	}
}
```

with:

```go
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfWant))
	var ref *books.Refusal
	if err := f.store.MarkDNF(context.Background(), f.alice.ID, id, "", 0); !errors.As(err, &ref) {
		t.Errorf("MarkDNF on a want-to-read book = %v, want a Refusal", err)
	}
}
```

In `internal/apps/books/shelf_test.go`, replace:

```go
	if err := f.store.StartReading(ctx, f.bob.ID, want); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's StartReading = %v, want ErrNotFound", err)
	}
	if err := f.store.FinishReading(ctx, f.bob.ID, reading, ""); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's FinishReading = %v, want ErrNotFound", err)
	}
}
```

with:

```go
	if err := f.store.StartReading(ctx, f.bob.ID, want); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's StartReading = %v, want ErrNotFound", err)
	}
	if err := f.store.FinishReading(ctx, f.bob.ID, reading, "", 0); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's FinishReading = %v, want ErrNotFound", err)
	}
}
```

In `internal/apps/books/shelf_test.go`, replace:

```go
		addBook(t, f, f.alice.ID, nb)
	}
	gone := addBook(t, f, f.alice.ID, onShelf("Gave up", books.ShelfReading))
	if err := f.store.MarkDNF(ctx, f.alice.ID, gone, ""); err != nil {
		t.Fatal(err)
	}

```

with:

```go
		addBook(t, f, f.alice.ID, nb)
	}
	gone := addBook(t, f, f.alice.ID, onShelf("Gave up", books.ShelfReading))
	if err := f.store.MarkDNF(ctx, f.alice.ID, gone, "", 0); err != nil {
		t.Fatal(err)
	}

```

Add two handler tests at the end of `internal/apps/books/actions_test.go`:

In `internal/apps/books/actions_test.go`, replace:

```go
		t.Errorf("HX-Replace-Url = %q, want %q", got, want)
	}
}
```

with:

```go
		t.Errorf("HX-Replace-Url = %q, want %q", got, want)
	}
}

func TestFinishWithARatingAndDNFWithAPage(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	nb := titled("Dune", "", books.ShelfReading)
	nb.Pages = 600
	dune := add(t, s, uid, nb)
	nb.Title = "Infinite Jest"
	jest := add(t, s, uid, nb)
	s.Submit(t, s.Alice, fmt.Sprintf("/books/finish/%d", dune), url.Values{"shelf": {"reading"}, "rating": {"4"}},
		fmt.Sprintf("/books/b/%d?shelf=reading", dune))
	s.Submit(t, s.Alice, fmt.Sprintf("/books/dnf/%d", jest), url.Values{"shelf": {"reading"}, "at": {"120"}},
		fmt.Sprintf("/books/b/%d?shelf=reading", jest))
	ctx := context.Background()
	if b, _ := s.Store.Get(ctx, uid, dune); b.Rating != 4 || b.Shelf != books.ShelfRead {
		t.Errorf("Dune: rating %d, shelf %q; want 4, read", b.Rating, b.Shelf)
	}
	if b, _ := s.Store.Get(ctx, uid, jest); b.Progress.Value != 120 || b.Shelf != books.ShelfDNF {
		t.Errorf("Infinite Jest: progress %+v, shelf %q; want page 120, dnf", b.Progress, b.Shelf)
	}
}

func TestFinishWithATamperedRatingIsABadRequest(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfReading))
	rec := s.Post(t, s.Alice, fmt.Sprintf("/books/finish/%d", id), url.Values{"shelf": {"reading"}, "rating": {"ten"}})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("finish with rating=ten = %d, want 400", rec.Code)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `too many arguments in call to f.store.FinishReading`, `f.store.SetRating undefined`.

- [ ] **Step 3: Write `rating.go`**

Create `internal/apps/books/rating.go`:

```go
package books

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxReviewRunes bounds a review: room for a long one, not for a book.
const MaxReviewRunes = 20000

// checkRating accepts 0 (no rating) to 5. Ratings come from buttons and a
// select, never typed, so anything else is a tampered form: ErrInvalid.
func checkRating(rating int) error {
	if rating < 0 || rating > 5 {
		return ErrInvalid
	}
	return nil
}

// SetRating rates one of userID's books 1–5, or clears its rating with 0.
// One rating per book, not per reading (spec "Decisions at a glance").
func (st *Store) SetRating(ctx context.Context, userID, id int64, rating int) error {
	if err := checkRating(rating); err != nil {
		return err
	}
	res, err := st.db.ExecContext(ctx,
		`UPDATE books_books SET rating = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		nullInt(rating), formatTime(st.now()), id, userID)
	if err != nil {
		return fmt.Errorf("books: set rating: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetReview replaces a book's review, Markdown as typed (line endings
// tidied, ends trimmed). "" removes it.
func (st *Store) SetReview(ctx context.Context, userID, id int64, review string) error {
	review = strings.TrimSpace(strings.ReplaceAll(review, "\r\n", "\n"))
	if utf8.RuneCountInString(review) > MaxReviewRunes {
		return &Refusal{Msg: fmt.Sprintf("Keep your review to %d characters or fewer.", MaxReviewRunes)}
	}
	res, err := st.db.ExecContext(ctx,
		`UPDATE books_books SET review = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		review, formatTime(st.now()), id, userID)
	if err != nil {
		return fmt.Errorf("books: set review: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 4: Give the reading in progress its start date**

In `internal/apps/books/progress.go`, replace:

```go

// active is a book's reading in progress, with what its unit depends on.
type active struct {
	id     int64
	format string
	pages  int
}

// activeReading finds the reading in progress of book id inside tx; a
```

with:

```go

// active is a book's reading in progress, with what its unit depends on.
type active struct {
	id        int64
	startedOn string
	format    string
	pages     int
}

// activeReading finds the reading in progress of book id inside tx; a
```

In `internal/apps/books/progress.go`, replace:

```go
func activeReading(ctx context.Context, tx *sql.Tx, id int64) (active, error) {
	var a active
	err := tx.QueryRowContext(ctx, `
		SELECT r.id, COALESCE(r.format, ''), COALESCE(b.pages, 0)
		  FROM books_readings r JOIN books_books b ON b.id = r.book_id
		 WHERE r.book_id = ? AND r.status = 'reading'`, id).Scan(&a.id, &a.format, &a.pages)
	if errors.Is(err, sql.ErrNoRows) {
		return active{}, &Refusal{Msg: "This book isn't being read right now."}
	}
```

with:

```go
func activeReading(ctx context.Context, tx *sql.Tx, id int64) (active, error) {
	var a active
	err := tx.QueryRowContext(ctx, `
		SELECT r.id, COALESCE(r.started_on, ''), COALESCE(r.format, ''), COALESCE(b.pages, 0)
		  FROM books_readings r JOIN books_books b ON b.id = r.book_id
		 WHERE r.book_id = ? AND r.status = 'reading'`, id).Scan(&a.id, &a.startedOn, &a.format, &a.pages)
	if errors.Is(err, sql.ErrNoRows) {
		return active{}, &Refusal{Msg: "This book isn't being read right now."}
	}
```

- [ ] **Step 5: Finish with a rating, DNF with a stopping place**

In `internal/apps/books/reading.go`, replace:

```go

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)
```

with:

```go

import (
	"context"
	"fmt"
	"time"
)
```

In `internal/apps/books/reading.go`, replace:

```go
}

// FinishReading closes the reading in progress as finished on day
// (YYYY-MM-DD; "" is today).
func (st *Store) FinishReading(ctx context.Context, userID, id int64, day string) error {
	return st.closeReading(ctx, userID, id, day, StatusFinished)
}

// MarkDNF closes the reading in progress as not finished on day.
func (st *Store) MarkDNF(ctx context.Context, userID, id int64, day string) error {
	return st.closeReading(ctx, userID, id, day, StatusDNF)
}

// closeReading ends the reading in progress. The day must be a real date,
// not in the future and not before the reading started.
func (st *Store) closeReading(ctx context.Context, userID, id int64, day string, to Status) error {
	if day == "" {
		day = st.Today()
	}
```

with:

```go
}

// FinishReading closes the reading in progress as finished on day
// (YYYY-MM-DD; "" is today) and, when rating is 1–5, rates the book (spec
// "Progress and finishing"). Rating 0 leaves the book's rating as it is, so
// a re-read needn't rate it again.
func (st *Store) FinishReading(ctx context.Context, userID, id int64, day string, rating int) error {
	return st.closeReading(ctx, userID, id, day, StatusFinished, rating, 0)
}

// MarkDNF closes the reading in progress as not finished on day. at, when
// not 0, is where it stopped, in the reading's unit (UnitFor); it is kept
// as the reading's last progress.
func (st *Store) MarkDNF(ctx context.Context, userID, id int64, day string, at int) error {
	return st.closeReading(ctx, userID, id, day, StatusDNF, 0, at)
}

// closeReading ends the reading in progress, rating the book and recording
// a last progress row on the way when asked to. The day must be a real
// date, not in the future and not before the reading started.
func (st *Store) closeReading(ctx context.Context, userID, id int64, day string, to Status, rating, at int) error {
	if err := checkRating(rating); err != nil {
		return err
	}
	if day == "" {
		day = st.Today()
	}
```

In `internal/apps/books/reading.go`, replace:

```go
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	var rid int64
	var started sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT id, started_on FROM books_readings WHERE book_id = ? AND status = 'reading'`, id).Scan(&rid, &started)
	if errors.Is(err, sql.ErrNoRows) {
		return &Refusal{Msg: "This book isn't being read right now."}
	}
	if err != nil {
		return fmt.Errorf("books: close reading: %w", err)
	}
	if started.Valid && day < started.String {
		return &Refusal{Msg: "That's before you started reading it (" + ShowDay(started.String) + ")."}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE books_readings SET status = ?, finished_on = ? WHERE id = ?`, string(to), day, rid); err != nil {
		return fmt.Errorf("books: close reading: %w", err)
	}
	return tx.Commit()
```

with:

```go
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	a, err := activeReading(ctx, tx, id)
	if err != nil {
		return err
	}
	if a.startedOn != "" && day < a.startedOn {
		return &Refusal{Msg: "That's before you started reading it (" + ShowDay(a.startedOn) + ")."}
	}
	if at != 0 {
		u := UnitFor(a.format, a.pages)
		if err := checkProgress(u, a.pages, at); err != nil {
			return err
		}
		if err := insertProgress(ctx, tx, a.id, u, at, formatTime(st.now())); err != nil {
			return err
		}
	}
	if rating != 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE books_books SET rating = ? WHERE id = ?`, rating, id); err != nil {
			return fmt.Errorf("books: rate on finish: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE books_readings SET status = ?, finished_on = ? WHERE id = ?`, string(to), day, a.id); err != nil {
		return fmt.Errorf("books: close reading: %w", err)
	}
	return tx.Commit()
```

- [ ] **Step 6: The rating on list rows**

In `internal/apps/books/library.go`, replace:

```go
type Book struct {
	ID int64
	BookInput
	Rating             int    // 1–5, 0 for none (set from B2)
	Review             string // Markdown (set from B2)
	Tags               []string
	Shelf              Shelf
	Latest             Reading  // zero ID before the first reading
```

with:

```go
type Book struct {
	ID int64
	BookInput
	Rating             int    // 1–5, 0 for none
	Review             string // Markdown, "" for none
	Tags               []string
	Shelf              Shelf
	Latest             Reading  // zero ID before the first reading
```

In `internal/apps/books/library.go`, replace:

```go
type ListItem struct {
	ID                                       int64
	Title, Authors, SeriesName, SeriesNumber string
	Pages                                    int
	Shelf                                    Shelf
	StartedOn, FinishedOn, Format            string   // the latest reading's
	Progress                                 Progress // the latest reading's latest progress
```

with:

```go
type ListItem struct {
	ID                                       int64
	Title, Authors, SeriesName, SeriesNumber string
	Pages, Rating                            int
	Shelf                                    Shelf
	StartedOn, FinishedOn, Format            string   // the latest reading's
	Progress                                 Progress // the latest reading's latest progress
```

In `internal/apps/books/library.go`, replace:

```go
		args = append(args, pat, pat, pat, pat)
	}
	rows, err := st.db.QueryContext(ctx, `
		SELECT b.id, b.title, b.authors, b.series_name, b.series_number, b.pages, b.added_at,
		       r.started_on, r.finished_on, r.format, `+shelfExpr+`, c.fetched_at,
		       p.page, p.percent, p.recorded_at
		  FROM books_books b `+latestJoin+`
```

with:

```go
		args = append(args, pat, pat, pat, pat)
	}
	rows, err := st.db.QueryContext(ctx, `
		SELECT b.id, b.title, b.authors, b.series_name, b.series_number, b.pages, b.rating, b.added_at,
		       r.started_on, r.finished_on, r.format, `+shelfExpr+`, c.fetched_at,
		       p.page, p.percent, p.recorded_at
		  FROM books_books b `+latestJoin+`
```

In `internal/apps/books/library.go`, replace:

```go
	for rows.Next() {
		var it ListItem
		var added, shelf string
		var pages, atPage, atPercent sql.NullInt64
		var started, finished, format, cover, recorded sql.NullString
		if err := rows.Scan(&it.ID, &it.Title, &it.Authors, &it.SeriesName, &it.SeriesNumber, &pages, &added,
			&started, &finished, &format, &shelf, &cover, &atPage, &atPercent, &recorded); err != nil {
			return nil, fmt.Errorf("books: scan list: %w", err)
		}
```

with:

```go
	for rows.Next() {
		var it ListItem
		var added, shelf string
		var pages, rating, atPage, atPercent sql.NullInt64
		var started, finished, format, cover, recorded sql.NullString
		if err := rows.Scan(&it.ID, &it.Title, &it.Authors, &it.SeriesName, &it.SeriesNumber, &pages, &rating, &added,
			&started, &finished, &format, &shelf, &cover, &atPage, &atPercent, &recorded); err != nil {
			return nil, fmt.Errorf("books: scan list: %w", err)
		}
```

In `internal/apps/books/library.go`, replace:

```go
		if it.Progress, err = scanProgress(atPage, atPercent, recorded); err != nil {
			return nil, err
		}
		it.Pages = int(pages.Int64)
		it.StartedOn, it.FinishedOn, it.Format, it.Shelf = started.String, finished.String, format.String, Shelf(shelf)
		it.CoverVersion = coverVersion(cover.String)
		out = append(out, it)
```

with:

```go
		if it.Progress, err = scanProgress(atPage, atPercent, recorded); err != nil {
			return nil, err
		}
		it.Pages, it.Rating = int(pages.Int64), int(rating.Int64)
		it.StartedOn, it.FinishedOn, it.Format, it.Shelf = started.String, finished.String, format.String, Shelf(shelf)
		it.CoverVersion = coverVersion(cover.String)
		out = append(out, it)
```

- [ ] **Step 7: Read the new fields in the handlers**

In `internal/apps/books/actions.go`, replace:

```go
import (
	"errors"
	"net/http"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
```

with:

```go
import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
```

In `internal/apps/books/actions.go`, replace:

```go
	return a.store.StartReading(r.Context(), userID, id)
}

func (a *App) finish(r *http.Request, userID, id int64) error {
	return a.store.FinishReading(r.Context(), userID, id, strings.TrimSpace(r.PostFormValue("day")))
}

func (a *App) dnf(r *http.Request, userID, id int64) error {
	return a.store.MarkDNF(r.Context(), userID, id, strings.TrimSpace(r.PostFormValue("day")))
}

func (a *App) setTags(r *http.Request, userID, id int64) error {
```

with:

```go
	return a.store.StartReading(r.Context(), userID, id)
}

// formInt reads an optional whole-number field: 0 when it is empty, -1
// when it isn't a number — outside every range the store accepts, so a
// typo comes back as the store's own message (or a 400 for a field no
// person types into).
func formInt(r *http.Request, name string) int {
	s := strings.TrimSpace(r.PostFormValue(name))
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}

func (a *App) finish(r *http.Request, userID, id int64) error {
	return a.store.FinishReading(r.Context(), userID, id, strings.TrimSpace(r.PostFormValue("day")), formInt(r, "rating"))
}

func (a *App) dnf(r *http.Request, userID, id int64) error {
	return a.store.MarkDNF(r.Context(), userID, id, strings.TrimSpace(r.PostFormValue("day")), formInt(r, "at"))
}

func (a *App) setTags(r *http.Request, userID, id int64) error {
```

- [ ] **Step 8: Keep the screenshot seed compiling**

In `docs/screenshots/seed/books.go`, replace:

```go
		}
		st.SetClock(func() time.Time { return at(b.finished) })
		if b.dnf {
			err = st.MarkDNF(ctx, userID, id, day(b.finished))
		} else {
			err = st.FinishReading(ctx, userID, id, day(b.finished))
		}
		if err != nil {
			return err
```

with:

```go
		}
		st.SetClock(func() time.Time { return at(b.finished) })
		if b.dnf {
			err = st.MarkDNF(ctx, userID, id, day(b.finished), 0)
		} else {
			err = st.FinishReading(ctx, userID, id, day(b.finished), 0)
		}
		if err != nil {
			return err
```

- [ ] **Step 9: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... ./docs/... -count=1
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
```
Expected: all pass, no output from vet or staticcheck.

- [ ] **Step 10: Commit**

```bash
git add internal/apps/books docs/screenshots/seed
git commit -m "feat(books): rate on finish, say where a DNF stopped, rating and review in the store (#488)"
```

---

### Task 3: Reading history and format in the store

**Files:**
- Create: `internal/apps/books/history.go`
- Test: `internal/apps/books/history_test.go`

**Interfaces:**
- Consumes: `touch`, `checkDay`, `nullText`, `activeReading`, `Reading`, `Status*`, `Refusal`, `ErrNotFound`, `ErrInvalid`; test helpers `inProgress`, `newFixture`, `addBook`, `onShelf`, `getBook`.
- Produces:
  - `var Formats = []string{"paper", "ebook", "audio"}`; unexported `checkFormat(format string) error` ("" or one of `Formats`, else `ErrInvalid`)
  - `(*Store).SetFormat(ctx, userID, id int64, format string) error` — the reading in progress; `*Refusal` when there is none
  - `(*Store).Readings(ctx, userID, id int64) ([]Reading, error)` — newest first by `created_at`; none for someone else's book
  - `type ReadingEdit struct { StartedOn, FinishedOn string; Format string }`
  - `(*Store).UpdateReading(ctx, userID, id, readingID int64, ed ReadingEdit) error` — dates and format only; a reading in progress keeps no finish date; future dates and a finish before the start are `*Refusal`s; a reading of another book is `ErrNotFound`
  - `(*Store).DeleteReading(ctx, userID, id, readingID int64) error` — its progress goes too (ON DELETE CASCADE)
  - test helpers: `readings(t, f, id) []books.Reading`, `readTwice(t, f) int64` (finished 1–2 Oct on paper, re-reading since 5 Oct; clock left at 9 Oct)

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/history_test.go`:

```go
package books_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

func readings(t *testing.T, f *fixture, id int64) []books.Reading {
	t.Helper()
	rs, err := f.store.Readings(context.Background(), f.alice.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

// readTwice is a book Alice finished on 2026-10-02 and is reading again
// since 2026-10-05, with paper as the format of both readings.
func readTwice(t *testing.T, f *fixture) int64 {
	t.Helper()
	ctx := context.Background()
	f.now = time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	id := inProgress(t, f, "Dune", 600)
	if err := f.store.SetFormat(ctx, f.alice.ID, id, "paper"); err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	if err := f.store.FinishReading(ctx, f.alice.ID, id, "2026-10-02", 0); err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)
	if err := f.store.StartReading(ctx, f.alice.ID, id); err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC)
	return id
}

func TestReadingsAreTheHistoryNewestFirst(t *testing.T) {
	f := newFixture(t)
	id := readTwice(t, f)
	rs := readings(t, f, id)
	if len(rs) != 2 {
		t.Fatalf("readings = %+v, want 2", rs)
	}
	now, before := rs[0], rs[1]
	if now.Status != books.StatusReading || now.StartedOn != "2026-10-05" || now.Format != "paper" {
		t.Errorf("newest = %+v, want reading since 2026-10-05 on paper (carried over)", now)
	}
	if before.Status != books.StatusFinished || before.StartedOn != "2026-10-01" || before.FinishedOn != "2026-10-02" {
		t.Errorf("older = %+v, want finished 2026-10-01 → 2026-10-02", before)
	}
	if rs, err := f.store.Readings(context.Background(), f.bob.ID, id); err != nil || len(rs) != 0 {
		t.Errorf("Bob's Readings = %+v, %v; want none", rs, err)
	}
}

func TestSetFormatChangesTheReadingInProgress(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := readTwice(t, f)
	if err := f.store.SetFormat(ctx, f.alice.ID, id, "audio"); err != nil {
		t.Fatal(err)
	}
	rs := readings(t, f, id)
	if rs[0].Format != "audio" || rs[1].Format != "paper" {
		t.Errorf("formats = %q, %q; want audio now, paper before", rs[0].Format, rs[1].Format)
	}
	if err := f.store.SetFormat(ctx, f.alice.ID, id, "vinyl"); !errors.Is(err, books.ErrInvalid) {
		t.Errorf("SetFormat(vinyl) = %v, want ErrInvalid", err)
	}
	want := addBook(t, f, f.alice.ID, onShelf("Emma", books.ShelfWant))
	var ref *books.Refusal
	if err := f.store.SetFormat(ctx, f.alice.ID, want, "ebook"); !errors.As(err, &ref) {
		t.Errorf("SetFormat with nothing in progress = %v, want a Refusal", err)
	}
	if err := f.store.SetFormat(ctx, f.bob.ID, id, "ebook"); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's SetFormat = %v, want ErrNotFound", err)
	}
}

func TestUpdateReadingChangesDatesAndFormat(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := readTwice(t, f)
	rs := readings(t, f, id)
	past, current := rs[1].ID, rs[0].ID
	if err := f.store.UpdateReading(ctx, f.alice.ID, id, past, books.ReadingEdit{
		StartedOn: "2026-09-20", FinishedOn: "2026-09-28", Format: "ebook"}); err != nil {
		t.Fatal(err)
	}
	// A reading in progress keeps no finish date, whatever is sent.
	if err := f.store.UpdateReading(ctx, f.alice.ID, id, current, books.ReadingEdit{
		StartedOn: "", FinishedOn: "2026-10-08", Format: ""}); err != nil {
		t.Fatal(err)
	}
	rs = readings(t, f, id)
	if got := rs[1]; got.StartedOn != "2026-09-20" || got.FinishedOn != "2026-09-28" || got.Format != "ebook" || got.Status != books.StatusFinished {
		t.Errorf("past reading = %+v", got)
	}
	if got := rs[0]; got.StartedOn != "" || got.FinishedOn != "" || got.Format != "" || got.Status != books.StatusReading {
		t.Errorf("current reading = %+v, want no dates, no format, still reading", got)
	}
}

func TestUpdateReadingChecksItsInput(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := readTwice(t, f)
	past := readings(t, f, id)[1].ID
	var ref *books.Refusal
	for _, ed := range []books.ReadingEdit{
		{StartedOn: "2026-09-28", FinishedOn: "2026-09-20"},
		{StartedOn: "2026-10-20"},
		{FinishedOn: "yesterday"},
	} {
		if err := f.store.UpdateReading(ctx, f.alice.ID, id, past, ed); !errors.As(err, &ref) {
			t.Errorf("UpdateReading(%+v) = %v, want a Refusal", ed, err)
		}
	}
	if err := f.store.UpdateReading(ctx, f.alice.ID, id, past, books.ReadingEdit{Format: "scroll"}); !errors.Is(err, books.ErrInvalid) {
		t.Errorf("format scroll = %v, want ErrInvalid", err)
	}
	other := addBook(t, f, f.alice.ID, onShelf("Emma", books.ShelfWant))
	if err := f.store.UpdateReading(ctx, f.alice.ID, other, past, books.ReadingEdit{}); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("a reading through another book = %v, want ErrNotFound", err)
	}
	if err := f.store.UpdateReading(ctx, f.bob.ID, id, past, books.ReadingEdit{}); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's UpdateReading = %v, want ErrNotFound", err)
	}
	if got := readings(t, f, id)[1]; got.StartedOn != "2026-10-01" || got.FinishedOn != "2026-10-02" {
		t.Errorf("past reading changed by refused edits: %+v", got)
	}
}

func TestDeleteReadingMovesTheShelf(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := readTwice(t, f)
	if err := f.store.RecordProgress(ctx, f.alice.ID, id, 50); err != nil {
		t.Fatal(err)
	}
	rs := readings(t, f, id)
	if err := f.store.DeleteReading(ctx, f.bob.ID, id, rs[0].ID); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's DeleteReading = %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteReading(ctx, f.alice.ID, id, rs[0].ID); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Shelf != books.ShelfRead || b.Progress.Set() {
		t.Errorf("after deleting the re-read: shelf %q, progress %+v; want read, none", b.Shelf, b.Progress)
	}
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM books_progress`).Scan(&n); err != nil || n != 0 {
		t.Errorf("progress rows = %d, %v; want the deleted reading's gone", n, err)
	}
	if err := f.store.DeleteReading(ctx, f.alice.ID, id, rs[1].ID); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Shelf != books.ShelfWant {
		t.Errorf("with no readings left, shelf = %q, want want", b.Shelf)
	}
	if err := f.store.DeleteReading(ctx, f.alice.ID, id, rs[1].ID); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("deleting it again = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `f.store.SetFormat undefined`, `undefined: books.ReadingEdit`.

- [ ] **Step 3: Write `history.go`**

Create `internal/apps/books/history.go`:

```go
package books

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Formats is every reading format, in the order menus offer them.
var Formats = []string{"paper", "ebook", "audio"}

// checkFormat accepts "" (not said) or one of Formats. Formats come from
// buttons and selects, so anything else is ErrInvalid.
func checkFormat(format string) error {
	if format == "" {
		return nil
	}
	for _, f := range Formats {
		if f == format {
			return nil
		}
	}
	return ErrInvalid
}

// SetFormat changes the format of the reading in progress. Its progress
// unit follows (UnitFor); rows already recorded keep theirs.
func (st *Store) SetFormat(ctx context.Context, userID, id int64, format string) error {
	if err := checkFormat(format); err != nil {
		return err
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin set format: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	a, err := activeReading(ctx, tx, id)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE books_readings SET format = ? WHERE id = ?`, nullText(format), a.id); err != nil {
		return fmt.Errorf("books: set format: %w", err)
	}
	return tx.Commit()
}

// Readings is a book's reading history, newest first (spec "Book pane":
// every reading with dates, format and status). A book that isn't userID's
// has none.
func (st *Store) Readings(ctx context.Context, userID, id int64) ([]Reading, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT r.id, r.status, r.format, r.started_on, r.finished_on
		  FROM books_readings r JOIN books_books b ON b.id = r.book_id
		 WHERE b.id = ? AND b.user_id = ?
		 ORDER BY r.created_at DESC, r.id DESC`, id, userID)
	if err != nil {
		return nil, fmt.Errorf("books: readings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Reading
	for rows.Next() {
		var rd Reading
		var status string
		var format, started, finished sql.NullString
		if err := rows.Scan(&rd.ID, &status, &format, &started, &finished); err != nil {
			return nil, fmt.Errorf("books: scan reading: %w", err)
		}
		rd.Status, rd.Format, rd.StartedOn, rd.FinishedOn = Status(status), format.String, started.String, finished.String
		out = append(out, rd)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: readings: %w", err)
	}
	return out, nil
}

// ReadingEdit is what can be changed about a reading: its dates and format
// (decided 2026-10-09). Its status can't be — deleting a reading is how a
// wrong one is put right. Dates are YYYY-MM-DD, "" for unknown.
type ReadingEdit struct {
	StartedOn, FinishedOn string
	Format                string
}

// UpdateReading changes one of a book's readings. A reading in progress
// has no finish date, so FinishedOn is ignored for it. Dates can't be in
// the future, and the finish can't come before the start.
func (st *Store) UpdateReading(ctx context.Context, userID, id, readingID int64, ed ReadingEdit) error {
	if err := checkFormat(ed.Format); err != nil {
		return err
	}
	for _, day := range []string{ed.StartedOn, ed.FinishedOn} {
		if day == "" {
			continue
		}
		if err := st.checkDay(day); err != nil {
			return err
		}
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin update reading: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	var status string
	err = tx.QueryRowContext(ctx,
		`SELECT status FROM books_readings WHERE id = ? AND book_id = ?`, readingID, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("books: update reading: %w", err)
	}
	if Status(status) == StatusReading {
		ed.FinishedOn = ""
	}
	if ed.StartedOn != "" && ed.FinishedOn != "" && ed.FinishedOn < ed.StartedOn {
		return &Refusal{Msg: "The finish date is before the start."}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE books_readings SET started_on = ?, finished_on = ?, format = ? WHERE id = ?`,
		nullText(ed.StartedOn), nullText(ed.FinishedOn), nullText(ed.Format), readingID); err != nil {
		return fmt.Errorf("books: update reading: %w", err)
	}
	return tx.Commit()
}

// DeleteReading removes one of a book's readings and its progress (ON
// DELETE CASCADE). The book's shelf follows whatever reading is now the
// latest — Want to read when none is left.
func (st *Store) DeleteReading(ctx context.Context, userID, id, readingID int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin delete reading: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM books_readings WHERE id = ? AND book_id = ?`, readingID, id)
	if err != nil {
		return fmt.Errorf("books: delete reading: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}
```

- [ ] **Step 4: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): reading history and format in the store (#488)"
```

---

### Task 4: Series links filter the list to the series

**Files:**
- Modify: `internal/apps/books/library.go` (`Book.SeriesBooks`, `Get`, `ListQuery`, `listOrder`, `List`), `internal/apps/books/view.go` (`listCtx`, `ctxFrom`, `Query`, `viewSidebar`, `listHeading`, `emptyText`, `bookView`, `viewBook`, `countText`), `internal/apps/books/templates/panes.partial.html` (`filter-ctx`, `ctx-fields`, `list`, `book`), `internal/apps/books/static/books.js` (`syncBookContext`)
- Test: `internal/apps/books/series_test.go`, `internal/apps/books/handlers_test.go` (the series line now carries the count)

**Interfaces:**
- Consumes: `seriesText`, `listCtx.ListURL`, `hx`, `rowTitles`, `add`, `titled`, `list`.
- Produces:
  - `ListQuery.Series string` and `listCtx.Series string` — the last field of both, so `ListQuery(c)` still converts; query/form parameter `series`
  - `Book.SeriesBooks int` — how many of the user's books share the series name (case-insensitive), 0 for none
  - `listOrder(q ListQuery) string` (was `listOrder(s Shelf)`): a series list sorts by `CAST(series_number AS REAL)`, then the number text, then title
  - `bookView.Series` is now "The Expanse #1 · 2 books" (no count while the series has one book: "The Expanse #1"); `bookView.SeriesURL` is `/books/?series=…&shelf=all`
  - unexported `countText(n int, one, many string) string`
  - `#books-list` carries `data-series`; books.js copies `series` into the book pane's hidden fields and Edit link like `tag` and `q`

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/series_test.go`:

```go
package books_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// inSeries is a want-to-read book in a series.
func inSeries(title, series, number string) books.NewBook {
	nb := onShelf(title, books.ShelfWant)
	nb.SeriesName, nb.SeriesNumber = series, number
	return nb
}

func TestListASeriesInReadingOrder(t *testing.T) {
	f := newFixture(t)
	for _, nb := range []books.NewBook{
		inSeries("Caliban's War", "The Expanse", "2"),
		inSeries("The Churn", "the expanse", "2.5"),
		inSeries("Abaddon's Gate", "The Expanse", "3"),
		inSeries("Leviathan Wakes", "The Expanse", "1"),
		inSeries("Gods of Risk", "The Expanse", ""),
		inSeries("A Wizard of Earthsea", "Earthsea", "1"),
	} {
		addBook(t, f, f.alice.ID, nb)
	}
	addBook(t, f, f.bob.ID, inSeries("Bob's Expanse", "The Expanse", "4"))

	got := list(t, f, books.ListQuery{Shelf: books.ShelfAll, Series: "The Expanse"})
	want := []string{"Gods of Risk", "Leviathan Wakes", "Caliban's War", "The Churn", "Abaddon's Gate"}
	if !slices.Equal(got, want) {
		t.Errorf("series list = %v, want %v", got, want)
	}
	if got := list(t, f, books.ListQuery{Shelf: books.ShelfReading, Series: "The Expanse"}); len(got) != 0 {
		t.Errorf("series on the Reading shelf = %v, want none: the shelf still applies", got)
	}
}

func TestABookKnowsHowManyAreInItsSeries(t *testing.T) {
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, inSeries("Leviathan Wakes", "The Expanse", "1"))
	addBook(t, f, f.alice.ID, inSeries("Caliban's War", "THE EXPANSE", "2"))
	addBook(t, f, f.bob.ID, inSeries("Bob's Expanse", "The Expanse", "3"))
	alone := addBook(t, f, f.alice.ID, onShelf("Piranesi", books.ShelfWant))
	if n := getBook(t, f, f.alice.ID, id).SeriesBooks; n != 2 {
		t.Errorf("SeriesBooks = %d, want 2 (Alice's, any case)", n)
	}
	if n := getBook(t, f, f.alice.ID, alone).SeriesBooks; n != 0 {
		t.Errorf("SeriesBooks of a book in no series = %d, want 0", n)
	}
}

func TestTheSeriesLinkFiltersTheList(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	nb := titled("Leviathan Wakes", "James S. A. Corey", books.ShelfReading)
	nb.SeriesName, nb.SeriesNumber = "The Expanse", "1"
	id := add(t, s, uid, nb)
	nb = titled("Caliban's War", "James S. A. Corey", books.ShelfWant)
	nb.SeriesName, nb.SeriesNumber = "The Expanse", "2"
	add(t, s, uid, nb)
	add(t, s, uid, titled("Piranesi", "Susanna Clarke", books.ShelfWant))

	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=reading", id))
	link := doc.MustHave(".books-series a")
	if got := htmlassert.Text(link); got != "The Expanse #1 · 2 books" {
		t.Errorf("series link = %q", got)
	}
	href, _ := htmlassert.Attr(link, "href")
	if want := "/books/?series=The+Expanse&shelf=all"; href != want {
		t.Errorf("series link href = %q, want %q", href, want)
	}
	if v, _ := htmlassert.Attr(link, "hx-target"); v != "#books-list" {
		t.Errorf("series link hx-target = %q, want the list", v)
	}

	doc = htmlassert.Parse(t, hx(t, s, href, "books-list"))
	if got := rowTitles(doc); !slices.Equal(got, []string{"Leviathan Wakes", "Caliban's War"}) {
		t.Errorf("series rows = %v", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-list-head")); got != "Series “The Expanse”" {
		t.Errorf("heading = %q", got)
	}
	doc.MustNotHave(`.books-side a[aria-current="page"]`)
	if v, _ := htmlassert.Attr(doc.MustHave("#books-list"), "data-series"); v != "The Expanse" {
		t.Errorf("data-series = %q", v)
	}
	if v, _ := htmlassert.Attr(doc.MustHave(`#books-filter-ctx input[name="series"]`), "value"); v != "The Expanse" {
		t.Errorf("the filter's series field = %q", v)
	}

	// A book opened from the series list posts back into it.
	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=all&series=The+Expanse", id))
	if v, _ := htmlassert.Attr(doc.MustHave(`.books-tags-form input[name="series"]`), "value"); v != "The Expanse" {
		t.Errorf("tags form series field = %q", v)
	}
}

func TestABookInNoSeriesHasNoSeriesLink(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", id)).MustNotHave(".books-series")
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `unknown field Series in struct literal of type books.ListQuery`.

- [ ] **Step 3: The series filter and count in the store**

In `internal/apps/books/library.go`, replace:

```go
	Latest             Reading  // zero ID before the first reading
	Progress           Progress // the latest reading's latest progress
	CoverVersion       string   // "" when the book has no cover
	AddedAt, UpdatedAt time.Time
}

```

with:

```go
	Latest             Reading  // zero ID before the first reading
	Progress           Progress // the latest reading's latest progress
	CoverVersion       string   // "" when the book has no cover
	SeriesBooks        int      // how many of the user's books are in its series (0: none)
	AddedAt, UpdatedAt time.Time
}

```

In `internal/apps/books/library.go`, replace:

```go
		SELECT b.id, b.title, b.subtitle, b.authors, b.year, b.pages, b.isbn13, b.series_name,
		       b.series_number, b.description, b.rating, b.review, b.added_at, b.updated_at,
		       r.id, r.status, r.format, r.started_on, r.finished_on, `+shelfExpr+`, c.fetched_at,
		       p.page, p.percent, p.recorded_at
		  FROM books_books b `+latestJoin+`
		  `+progressJoin+`
		  LEFT JOIN books_covers c ON c.book_id = b.id
```

with:

```go
		SELECT b.id, b.title, b.subtitle, b.authors, b.year, b.pages, b.isbn13, b.series_name,
		       b.series_number, b.description, b.rating, b.review, b.added_at, b.updated_at,
		       r.id, r.status, r.format, r.started_on, r.finished_on, `+shelfExpr+`, c.fetched_at,
		       p.page, p.percent, p.recorded_at,
		       (SELECT count(*) FROM books_books s WHERE s.user_id = b.user_id AND b.series_name <> ''
		           AND s.series_name = b.series_name COLLATE NOCASE)
		  FROM books_books b `+latestJoin+`
		  `+progressJoin+`
		  LEFT JOIN books_covers c ON c.book_id = b.id
```

In `internal/apps/books/library.go`, replace:

```go
		&b.ID, &b.Title, &b.Subtitle, &b.Authors, &year, &pages, &isbn, &b.SeriesName,
		&b.SeriesNumber, &b.Description, &rating, &b.Review, &added, &updated,
		&rid, &status, &format, &started, &finished, &shelf, &cover,
		&atPage, &atPercent, &recorded)
	if errors.Is(err, sql.ErrNoRows) {
		return Book{}, ErrNotFound
	}
```

with:

```go
		&b.ID, &b.Title, &b.Subtitle, &b.Authors, &year, &pages, &isbn, &b.SeriesName,
		&b.SeriesNumber, &b.Description, &rating, &b.Review, &added, &updated,
		&rid, &status, &format, &started, &finished, &shelf, &cover,
		&atPage, &atPercent, &recorded, &b.SeriesBooks)
	if errors.Is(err, sql.ErrNoRows) {
		return Book{}, ErrNotFound
	}
```

In `internal/apps/books/library.go`, replace:

```go

// ListQuery picks the books a list shows. Shelf "" or ShelfAll is every
// shelf; Tag is one tag name; Q matches title, subtitle, authors or series
// name (SQLite LIKE: case-insensitive for ASCII).
type ListQuery struct {
	Shelf Shelf
	Tag   string
	Q     string
}

// listOrder is each shelf's sort (spec "Layout"): Reading by the latest
// progress (a reading with none yet by when it started), Read by finish,
// Want to read by date added, DNF and All by the latest change. NULL dates
// sort last.
func listOrder(s Shelf) string {
	switch s {
	case ShelfReading:
		return `COALESCE(p.recorded_at, r.created_at) DESC, r.id DESC`
	case ShelfRead:
```

with:

```go

// ListQuery picks the books a list shows. Shelf "" or ShelfAll is every
// shelf; Tag is one tag name; Q matches title, subtitle, authors or series
// name (SQLite LIKE: case-insensitive for ASCII); Series is one series'
// name, matched whole and ignoring case.
type ListQuery struct {
	Shelf  Shelf
	Tag    string
	Q      string
	Series string
}

// listOrder is each list's sort (spec "Layout"): a series in reading order
// (by number, as a number where it is one: "2.5" sits between 2 and 3);
// otherwise by shelf — Reading by the latest progress (a reading with none
// yet by when it started), Read by finish, Want to read by date added, DNF
// and All by the latest change. NULL dates sort last.
func listOrder(q ListQuery) string {
	if q.Series != "" {
		return `CAST(b.series_number AS REAL), b.series_number, b.title, b.id`
	}
	switch q.Shelf {
	case ShelfReading:
		return `COALESCE(p.recorded_at, r.created_at) DESC, r.id DESC`
	case ShelfRead:
```

In `internal/apps/books/library.go`, replace:

```go
			WHERE x.book_id = b.id AND t.name = ?)`)
		args = append(args, tag)
	}
	if text := strings.TrimSpace(q.Q); text != "" {
		pat := "%" + likeEscape(text) + "%"
		where = append(where, `(b.title LIKE ? ESCAPE '\' OR b.subtitle LIKE ? ESCAPE '\'
```

with:

```go
			WHERE x.book_id = b.id AND t.name = ?)`)
		args = append(args, tag)
	}
	if series := strings.TrimSpace(q.Series); series != "" {
		where = append(where, `b.series_name = ? COLLATE NOCASE`)
		args = append(args, series)
	}
	if text := strings.TrimSpace(q.Q); text != "" {
		pat := "%" + likeEscape(text) + "%"
		where = append(where, `(b.title LIKE ? ESCAPE '\' OR b.subtitle LIKE ? ESCAPE '\'
```

In `internal/apps/books/library.go`, replace:

```go
		  `+progressJoin+`
		  LEFT JOIN books_covers c ON c.book_id = b.id
		 WHERE `+strings.Join(where, " AND ")+`
		 ORDER BY `+listOrder(q.Shelf), args...)
	if err != nil {
		return nil, fmt.Errorf("books: list: %w", err)
	}
```

with:

```go
		  `+progressJoin+`
		  LEFT JOIN books_covers c ON c.book_id = b.id
		 WHERE `+strings.Join(where, " AND ")+`
		 ORDER BY `+listOrder(q), args...)
	if err != nil {
		return nil, fmt.Errorf("books: list: %w", err)
	}
```

- [ ] **Step 4: The series in the list context, heading and book pane**

In `internal/apps/books/view.go`, replace:

```go
func (s Shelf) Label() string { return shelfLabels[s] }

// listCtx is the list the panes show: a shelf, optionally narrowed to one
// tag and a title/author filter. GETs carry it in the query string and
// POSTs in hidden fields (ctx-fields), so whatever a change re-renders
// comes back to the same list — the lesson of Reader's reader-ctx.
type listCtx struct {
	Shelf Shelf
	Tag   string
	Q     string
}

// ctxFrom reads a list context; a missing or unknown shelf is Reading, the
```

with:

```go
func (s Shelf) Label() string { return shelfLabels[s] }

// listCtx is the list the panes show: a shelf, optionally narrowed to one
// tag, one series and a title/author filter. GETs carry it in the query
// string and POSTs in hidden fields (ctx-fields), so whatever a change
// re-renders comes back to the same list — the lesson of Reader's
// reader-ctx. Its fields are ListQuery's, in the same order.
type listCtx struct {
	Shelf  Shelf
	Tag    string
	Q      string
	Series string
}

// ctxFrom reads a list context; a missing or unknown shelf is Reading, the
```

In `internal/apps/books/view.go`, replace:

```go
	if !ok {
		sh = ShelfReading
	}
	return listCtx{Shelf: sh, Tag: strings.ToLower(strings.TrimSpace(get("tag"))), Q: strings.TrimSpace(get("q"))}
}

// Query is the context as a query string. Templates use the URL methods
```

with:

```go
	if !ok {
		sh = ShelfReading
	}
	return listCtx{Shelf: sh, Tag: strings.ToLower(strings.TrimSpace(get("tag"))), Q: strings.TrimSpace(get("q")),
		Series: strings.TrimSpace(get("series"))}
}

// Query is the context as a query string. Templates use the URL methods
```

In `internal/apps/books/view.go`, replace:

```go
	}
	if c.Q != "" {
		v.Set("q", c.Q)
	}
	return v.Encode()
}
```

with:

```go
	}
	if c.Q != "" {
		v.Set("q", c.Q)
	}
	if c.Series != "" {
		v.Set("series", c.Series)
	}
	return v.Encode()
}
```

In `internal/apps/books/view.go`, replace:

```go

// viewSidebar keeps the filter text on every link: the filter box sits
// outside the list and keeps showing what was typed, so the lists it leads
// to keep applying it.
func viewSidebar(c listCtx, counts map[Shelf]int, tags []string) sidebarView {
	var v sidebarView
	for _, s := range Shelves {
		to := listCtx{Shelf: s, Q: c.Q}
		v.Shelves = append(v.Shelves, shelfLink{Label: s.Label(), URL: to.ListURL(), Count: counts[s],
			Current: c.Tag == "" && c.Shelf == s})
	}
	for _, name := range tags {
		to := listCtx{Shelf: ShelfAll, Tag: name, Q: c.Q}
```

with:

```go

// viewSidebar keeps the filter text on every link: the filter box sits
// outside the list and keeps showing what was typed, so the lists it leads
// to keep applying it. A tag or series list highlights no shelf.
func viewSidebar(c listCtx, counts map[Shelf]int, tags []string) sidebarView {
	var v sidebarView
	for _, s := range Shelves {
		to := listCtx{Shelf: s, Q: c.Q}
		v.Shelves = append(v.Shelves, shelfLink{Label: s.Label(), URL: to.ListURL(), Count: counts[s],
			Current: c.Tag == "" && c.Series == "" && c.Shelf == s})
	}
	for _, name := range tags {
		to := listCtx{Shelf: ShelfAll, Tag: name, Q: c.Q}
```

In `internal/apps/books/view.go`, replace:

```go
}

func listHeading(c listCtx) string {
	if c.Tag != "" {
		return "Tagged “" + c.Tag + "”"
	}
	return c.Shelf.Label()
```

with:

```go
}

func listHeading(c listCtx) string {
	switch {
	case c.Series != "":
		return "Series “" + c.Series + "”"
	case c.Tag != "":
		return "Tagged “" + c.Tag + "”"
	}
	return c.Shelf.Label()
```

In `internal/apps/books/view.go`, replace:

```go
	return "Added " + it.AddedAt.Local().Format("2 Jan 2006")
}

// initial is the first letter or digit of a title, for the mini spine.
func initial(title string) string {
	for _, r := range title {
```

with:

```go
	return "Added " + it.AddedAt.Local().Format("2 Jan 2006")
}

// countText is "1 book" or "9 books".
func countText(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// initial is the first letter or digit of a title, for the mini spine.
func initial(title string) string {
	for _, r := range title {
```

In `internal/apps/books/view.go`, replace:

```go
	switch {
	case c.Q != "":
		return "No books match “" + c.Q + "”."
	case c.Tag != "":
		return "No books tagged “" + c.Tag + "”."
	}
```

with:

```go
	switch {
	case c.Q != "":
		return "No books match “" + c.Q + "”."
	case c.Series != "":
		return "No books in the series “" + c.Series + "”."
	case c.Tag != "":
		return "No books tagged “" + c.Tag + "”."
	}
```

In `internal/apps/books/view.go`, replace:

```go
type bookView struct {
	Selected                         bool
	ID                               int64
	Title, Subtitle, Authors, Series string
	Facts                            []string // "2011", "592 pages", "ISBN 978…"
	Description                      string
	Spine                            string
```

with:

```go
type bookView struct {
	Selected                         bool
	ID                               int64
	Title, Subtitle, Authors, Series string   // Series: "The Expanse #3 · 9 books"
	SeriesURL                        string   // the list of the book's series
	Facts                            []string // "2011", "592 pages", "ISBN 978…"
	Description                      string
	Spine                            string
```

In `internal/apps/books/view.go`, replace:

```go
// viewBook draws a book; today bounds the reading box's date fields.
func viewBook(b Book, c listCtx, today string) bookView {
	v := bookView{Selected: true, ID: b.ID, Title: b.Title, Subtitle: b.Subtitle, Authors: b.Authors,
		Series: seriesText(b.SeriesName, b.SeriesNumber), Description: b.Description,
		Spine: SpineColor(b.Title), Cover: coverURL(b.ID, b.CoverVersion), ShelfLabel: b.Shelf.Label(),
		Tags: b.Tags, TagsValue: strings.Join(b.Tags, ", "), Today: today, Ctx: c}
	if b.Year > 0 {
		v.Facts = append(v.Facts, strconv.Itoa(b.Year))
	}
```

with:

```go
// viewBook draws a book; today bounds the reading box's date fields.
func viewBook(b Book, c listCtx, today string) bookView {
	v := bookView{Selected: true, ID: b.ID, Title: b.Title, Subtitle: b.Subtitle, Authors: b.Authors,
		Description: b.Description, Spine: SpineColor(b.Title), Cover: coverURL(b.ID, b.CoverVersion),
		ShelfLabel: b.Shelf.Label(), Tags: b.Tags, TagsValue: strings.Join(b.Tags, ", "), Today: today, Ctx: c}
	if b.SeriesName != "" {
		v.Series = seriesText(b.SeriesName, b.SeriesNumber)
		if b.SeriesBooks > 1 { // the count only says something once there are two
			v.Series += " · " + countText(b.SeriesBooks, "book", "books")
		}
		v.SeriesURL = listCtx{Shelf: ShelfAll, Series: b.SeriesName}.ListURL()
	}
	if b.Year > 0 {
		v.Facts = append(v.Facts, strconv.Itoa(b.Year))
	}
```

- [ ] **Step 5: Carry the series through the templates**

In `internal/apps/books/templates/panes.partial.html`, replace:

```html

{{define "back"}}<label class="toolbar-btn books-back-btn" for="{{.For}}">{{ticon "arrow-left"}}{{.Label}}</label>{{end}}

{{/* filter-ctx is the shelf and tag the filter box searches within. */}}
{{define "filter-ctx"}}<span id="books-filter-ctx"{{if .OOB}} hx-swap-oob="true"{{end}}><input type="hidden" name="shelf" value="{{.Ctx.Shelf}}">{{with .Ctx.Tag}}<input type="hidden" name="tag" value="{{.}}">{{end}}</span>{{end}}

{{/* ctx-fields is a listCtx as hidden fields, for POSTs. */}}
{{define "ctx-fields"}}<input type="hidden" name="shelf" value="{{.Shelf}}">{{with .Tag}}<input type="hidden" name="tag" value="{{.}}">{{end}}{{with .Q}}<input type="hidden" name="q" value="{{.}}">{{end}}{{end}}

{{define "sidebar"}}
<nav class="books-side" id="books-side" aria-label="Shelves"{{if .OOB}} hx-swap-oob="true"{{end}}>
```

with:

```html

{{define "back"}}<label class="toolbar-btn books-back-btn" for="{{.For}}">{{ticon "arrow-left"}}{{.Label}}</label>{{end}}

{{/* filter-ctx is the shelf, tag and series the filter box searches
     within. */}}
{{define "filter-ctx"}}<span id="books-filter-ctx"{{if .OOB}} hx-swap-oob="true"{{end}}><input type="hidden" name="shelf" value="{{.Ctx.Shelf}}">{{with .Ctx.Tag}}<input type="hidden" name="tag" value="{{.}}">{{end}}{{with .Ctx.Series}}<input type="hidden" name="series" value="{{.}}">{{end}}</span>{{end}}

{{/* ctx-fields is a listCtx as hidden fields, for POSTs. */}}
{{define "ctx-fields"}}<input type="hidden" name="shelf" value="{{.Shelf}}">{{with .Tag}}<input type="hidden" name="tag" value="{{.}}">{{end}}{{with .Q}}<input type="hidden" name="q" value="{{.}}">{{end}}{{with .Series}}<input type="hidden" name="series" value="{{.}}">{{end}}{{end}}

{{define "sidebar"}}
<nav class="books-side" id="books-side" aria-label="Shelves"{{if .OOB}} hx-swap-oob="true"{{end}}>
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html

{{/* list takes a listView. */}}
{{define "list"}}
<div class="books-list" id="books-list" data-shelf="{{.Ctx.Shelf}}" data-tag="{{.Ctx.Tag}}" data-q="{{.Ctx.Q}}">
	<h2 class="books-list-head">{{.Heading}}</h2>
	{{if .Rows}}
	<ul class="books-rows">
```

with:

```html

{{/* list takes a listView. */}}
{{define "list"}}
<div class="books-list" id="books-list" data-shelf="{{.Ctx.Shelf}}" data-tag="{{.Ctx.Tag}}" data-q="{{.Ctx.Q}}" data-series="{{.Ctx.Series}}">
	<h2 class="books-list-head">{{.Heading}}</h2>
	{{if .Rows}}
	<ul class="books-rows">
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
			<h1>{{.Title}}</h1>
			{{with .Subtitle}}<p class="books-subtitle">{{.}}</p>{{end}}
			{{with .Authors}}<p class="books-authors">{{.}}</p>{{end}}
			{{with .Series}}<p class="books-series">{{.}}</p>{{end}}
			{{with .Facts}}<p class="books-facts">{{range $i, $f := .}}{{if $i}} · {{end}}{{$f}}{{end}}</p>{{end}}
			<p class="books-pills"><span class="books-pill books-pill-shelf">{{.ShelfLabel}}</span>{{range .Tags}} <span class="books-pill">{{.}}</span>{{end}}</p>
		</div>
```

with:

```html
			<h1>{{.Title}}</h1>
			{{with .Subtitle}}<p class="books-subtitle">{{.}}</p>{{end}}
			{{with .Authors}}<p class="books-authors">{{.}}</p>{{end}}
			{{with .Series}}<p class="books-series"><a href="{{$.SeriesURL}}" hx-get="{{$.SeriesURL}}" hx-target="#books-list" hx-swap="outerHTML" hx-push-url="true">{{.}}</a></p>{{end}}
			{{with .Facts}}<p class="books-facts">{{range $i, $f := .}}{{if $i}} · {{end}}{{$f}}{{end}}</p>{{end}}
			<p class="books-pills"><span class="books-pill books-pill-shelf">{{.ShelfLabel}}</span>{{range .Tags}} <span class="books-pill">{{.}}</span>{{end}}</p>
		</div>
```

- [ ] **Step 6: Keep the book pane's context in step after a list swap**

In `internal/apps/books/static/books.js`, replace:

```js
		syncBookContext(e.target, book);
	});

	// A list swap leaves the book pane alone, so its hidden shelf/tag/q
	// fields and Edit link would keep POSTing and returning to the previous
	// list while the address bar shows the new one. Copy the new list's
	// context (data-* on #books-list) into them, as reader.js does for its
	// own panes. tag and q are only rendered when non-empty, so they are
	// created and removed here too.
	function syncBookContext(list, book) {
		if (!book) return;
		var ctx = {
			shelf: list.getAttribute("data-shelf") || "",
			tag: list.getAttribute("data-tag") || "",
			q: list.getAttribute("data-q") || "",
		};
		book.querySelectorAll("input[name=shelf]").forEach(function (shelf) {
			shelf.value = ctx.shelf;
			["tag", "q"].forEach(function (name) {
				var field = shelf.parentNode.querySelector("input[name=" + name + "]");
				if (!ctx[name]) {
					if (field) field.remove();
```

with:

```js
		syncBookContext(e.target, book);
	});

	// A list swap leaves the book pane alone, so its hidden shelf/tag/q/
	// series fields and Edit link would keep POSTing and returning to the
	// previous list while the address bar shows the new one. Copy the new
	// list's context (data-* on #books-list) into them, as reader.js does
	// for its own panes. tag, q and series are only rendered when
	// non-empty, so they are created and removed here too.
	var OPTIONAL_CTX = ["tag", "q", "series"];
	function syncBookContext(list, book) {
		if (!book) return;
		var ctx = {
			shelf: list.getAttribute("data-shelf") || "",
			tag: list.getAttribute("data-tag") || "",
			q: list.getAttribute("data-q") || "",
			series: list.getAttribute("data-series") || "",
		};
		book.querySelectorAll("input[name=shelf]").forEach(function (shelf) {
			shelf.value = ctx.shelf;
			OPTIONAL_CTX.forEach(function (name) {
				var field = shelf.parentNode.querySelector("input[name=" + name + "]");
				if (!ctx[name]) {
					if (field) field.remove();
```

In `internal/apps/books/static/books.js`, replace:

```js
			var safeId = encodeURIComponent(id);
			var qs = new URLSearchParams();
			qs.set("shelf", ctx.shelf);
			if (ctx.tag) qs.set("tag", ctx.tag);
			if (ctx.q) qs.set("q", ctx.q);
			edit.setAttribute("href", "/books/edit/" + safeId + "?" + qs.toString());
		}
	}
```

with:

```js
			var safeId = encodeURIComponent(id);
			var qs = new URLSearchParams();
			qs.set("shelf", ctx.shelf);
			OPTIONAL_CTX.forEach(function (name) {
				if (ctx[name]) qs.set(name, ctx[name]);
			});
			edit.setAttribute("href", "/books/edit/" + safeId + "?" + qs.toString());
		}
	}
```

- [ ] **Step 7: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
```
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): series links filter the list to the series (#488)"
```

---

### Task 5: The progress box, and progress and stars on list rows

**Files:**
- Modify: `internal/apps/books/handlers.go` (`paneOpts`, `renderPanes`, `index`, `book`), `internal/apps/books/actions.go` (`act`, `progress`), `internal/apps/books/books.go` (route), `internal/apps/books/view.go` (`rowView`, `listView`, `viewList`, `rowNote`, `stars`, `bookView`, `viewBook`, `progressView`, `viewProgress`), `internal/apps/books/templates/panes.partial.html` (`progress-swap`, `list`, `reading-box`, `progress`), `internal/apps/books/static/books.js` (`p`, Esc), `internal/ui/static/app.css`
- Test: `internal/apps/books/progress_view_test.go`

**Interfaces:**
- Consumes: Task 1's `Progress`, `UnitFor`, `RecordProgress`, `ListItem.Progress/Pages`; Task 2's `formInt`, `ListItem.Rating`, `SetRating`; Task 3's `SetFormat`; `act`, `ctxFrom`, `web.IsHTMX`, `web.HTMXTarget`.
- Produces:
  - `type paneOpts struct { BookID int64; Banner, ProgressError, ProgressInput string }`; `(*App).renderPanes(w, r, userID int64, c listCtx, opts paneOpts)` — every caller passes `paneOpts`; target `#books-progress` renders block `progress-swap` with `listView.OOB = true`; a refusal in `Banner` or `ProgressError` makes a full page 422
  - route `POST /books/progress/{id}`, field `at` (empty or not a number is refused like an out-of-range value)
  - `type progressView struct { Unit Unit; Value string; Max, Percent int; Note, Error string }`; `viewProgress(b Book) progressView`; `bookView.Progress`
  - `listView.OOB bool`; `rowView.Bar bool`, `rowView.Percent int`, `rowView.Rating int`, `rowView.Stars string`; `stars(rating int) string` ("★★★★☆", "" for none)
  - templates: `progress` (takes a bookView; `#books-progress`, `input#books-progress-input`), `progress-swap`
  - test helpers: `postHXTo(t, s, path, target string, form url.Values)`, `readingBook(t, s, title string, pages int) int64`

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/progress_view_test.go`:

```go
package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// postHXTo posts Alice's form as htmx does, aimed at target.
func postHXTo(t *testing.T, s *server, path, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set(web.CSRFFormField, s.CSRFToken(t, s.Alice))
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", target)
	return s.Do(t, s.Alice, req)
}

// readingBook adds one of Alice's books, on the go, with pages pages.
func readingBook(t *testing.T, s *server, title string, pages int) int64 {
	t.Helper()
	nb := titled(title, "", books.ShelfReading)
	nb.Pages = pages
	return add(t, s, s.Alice.User.ID, nb)
}

func TestTheProgressBoxIsInTheUnitOfTheReading(t *testing.T) {
	s := newServer(t)
	paper := readingBook(t, s, "Dune", 600)
	audio := readingBook(t, s, "Emma", 300)
	if err := s.Store.SetFormat(context.Background(), s.Alice.User.ID, audio, "audio"); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", paper))
	input := doc.MustHave("input#books-progress-input")
	if v, _ := htmlassert.Attr(input, "max"); v != "600" {
		t.Errorf("paper max = %q, want 600", v)
	}
	if got := htmlassert.Text(doc.MustHave(".books-progress-form label")); got != "Page" {
		t.Errorf("paper label = %q, want Page", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-progress-note")); got != "No progress yet." {
		t.Errorf("note = %q", got)
	}

	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", audio))
	if v, _ := htmlassert.Attr(doc.MustHave("input#books-progress-input"), "max"); v != "100" {
		t.Errorf("audio max = %q, want 100", v)
	}
	if got := htmlassert.Text(doc.MustHave(".books-progress-of")); got != "%" {
		t.Errorf("audio unit = %q, want %%", got)
	}

	want := add(t, s, s.Alice.User.ID, titled("Piranesi", "", books.ShelfWant))
	s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", want)).MustNotHave("#books-progress")
}

func TestProgressOverHTMXSwapsTheBoxAndTheList(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	id := readingBook(t, s, "Dune", 600)
	rec := postHXTo(t, s, fmt.Sprintf("/books/progress/%d", id), "books-progress", url.Values{"shelf": {"reading"}, "at": {"120"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx progress = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustNotHave("#books-panes")
	if v, _ := htmlassert.Attr(doc.MustHave("input#books-progress-input"), "value"); v != "120" {
		t.Errorf("input value = %q, want 120", v)
	}
	if v, _ := htmlassert.Attr(doc.MustHave("progress.books-progress-bar"), "value"); v != "20" {
		t.Errorf("bar = %q, want 20", v)
	}
	if got := htmlassert.Text(doc.MustHave(".books-progress-note")); got != "20% · updated 9 Oct 2026" {
		t.Errorf("note = %q", got)
	}
	list := doc.MustHave("#books-list")
	if v, _ := htmlassert.Attr(list, "hx-swap-oob"); v != "true" {
		t.Errorf("list hx-swap-oob = %q, want the list out of band", v)
	}
	if v, _ := htmlassert.Attr(doc.MustHave("progress.books-row-bar"), "value"); v != "20" {
		t.Errorf("row bar = %q, want 20", v)
	}
	if got := htmlassert.Text(doc.MustHave(".books-row-note")); got != "20%" {
		t.Errorf("row note = %q, want 20%%", got)
	}
	doc.MustHave("#books-list .is-active")
}

func TestProgressWithoutJavaScriptComesBackToTheBook(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	s.Submit(t, s.Alice, fmt.Sprintf("/books/progress/%d", id), url.Values{"shelf": {"reading"}, "at": {"300"}},
		fmt.Sprintf("/books/b/%d?shelf=reading", id))
	if b, _ := s.Store.Get(context.Background(), s.Alice.User.ID, id); b.Progress.Value != 300 {
		t.Errorf("progress = %+v, want page 300", b.Progress)
	}
}

func TestBadProgressIsRefusedInsideTheBox(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	path := fmt.Sprintf("/books/progress/%d", id)
	for _, typed := range []string{"700", "-3", "lots", ""} {
		rec := postHXTo(t, s, path, "books-progress", url.Values{"shelf": {"reading"}, "at": {typed}})
		if rec.Code != http.StatusOK {
			t.Fatalf("htmx refused progress %q = %d, want 200 so htmx swaps it", typed, rec.Code)
		}
		doc := htmlassert.Parse(t, rec.Body.String())
		if got := htmlassert.Text(doc.MustHave("#books-progress-error")); got != "Enter a page from 0 to 600." {
			t.Errorf("%q: error = %q", typed, got)
		}
		if v, _ := htmlassert.Attr(doc.MustHave("input#books-progress-input"), "value"); v != typed {
			t.Errorf("%q: input keeps %q, want what was typed", typed, v)
		}
	}
	rec := s.Post(t, s.Alice, path, url.Values{"shelf": {"reading"}, "at": {"700"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("refused progress without JavaScript = %d, want 422", rec.Code)
	}
	htmlassert.Parse(t, rec.Body.String()).MustHave("#books-progress-error")
	if b, _ := s.Store.Get(context.Background(), s.Alice.User.ID, id); b.Progress.Set() {
		t.Errorf("progress = %+v after refusals, want none", b.Progress)
	}
}

func TestProgressOnSomeoneElsesBookIsNotFound(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	if rec := s.Post(t, s.Bob, fmt.Sprintf("/books/progress/%d", id), url.Values{"at": {"10"}}); rec.Code != http.StatusNotFound {
		t.Errorf("Bob's progress = %d, want 404", rec.Code)
	}
}

func TestRowsShowProgressAndStars(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	uid := s.Alice.User.ID
	id := readingBook(t, s, "Dune", 600)
	readingBook(t, s, "Emma", 300)
	s.Clock.Advance(time.Minute)
	if err := s.Store.RecordProgress(context.Background(), uid, id, 150); err != nil {
		t.Fatal(err)
	}
	read := add(t, s, uid, titled("Ulysses", "", books.ShelfRead))
	if err := s.Store.SetRating(context.Background(), uid, read, 4); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/books/?shelf=reading")
	if got := htmlassert.Text(doc.MustHave(".books-row-note")); got != "25%" {
		t.Errorf("first reading row = %q, want 25%% (the one with progress first)", got)
	}
	if n := len(doc.QueryAll("progress.books-row-bar")); n != 1 {
		t.Errorf("%d row bars, want 1: no bar before any progress", n)
	}
	doc = s.Get(t, s.Alice, "/books/?shelf=read")
	starsEl := doc.MustHave(".books-stars")
	if got := htmlassert.Text(starsEl); got != "★★★★☆" {
		t.Errorf("stars = %q", got)
	}
	if v, _ := htmlassert.Attr(starsEl, "aria-label"); v != "Rated 4 of 5" {
		t.Errorf("stars label = %q", v)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `htmlassert: no element matches "input#books-progress-input"` and `htmx progress = 404, want 200` (no route yet).

- [ ] **Step 3: `paneOpts` and the progress-swap block in `renderPanes`**

In `internal/apps/books/handlers.go`, replace:

```go
	if !ok {
		return
	}
	a.renderPanes(w, r, uid, ctxFrom(r.FormValue), 0, "")
}

// book is a list with one book open.
```

with:

```go
	if !ok {
		return
	}
	a.renderPanes(w, r, uid, ctxFrom(r.FormValue), paneOpts{})
}

// book is a list with one book open.
```

In `internal/apps/books/handlers.go`, replace:

```go
	if !ok {
		return
	}
	a.renderPanes(w, r, uid, ctxFrom(r.FormValue), id, "")
}

// renderPanes draws the panes for list c with book bookID (0: none) open
// and errMsg (if any) in the banner. A normal request gets the whole page —
// 422 when there is an error, so a refused form post without JavaScript
// isn't a 200. An htmx request gets the block for what it targeted, as
// Reader's renderPanes does (#453): #books-list → list-swap (the list and
// its out-of-band companions, the book pane untouched), #books-book →
// book-swap, anything else (#books-panes) → the whole panes. Fragments are
// always 200: htmx's default responseHandling only swaps 2xx/3xx.
func (a *App) renderPanes(w http.ResponseWriter, r *http.Request, userID int64, c listCtx, bookID int64, errMsg string) {
	ctx := r.Context()
	counts, err := a.store.ShelfCounts(ctx, userID)
	if err != nil {
```

with:

```go
	if !ok {
		return
	}
	a.renderPanes(w, r, uid, ctxFrom(r.FormValue), paneOpts{BookID: id})
}

// paneOpts is what a render shows besides list c: the open book (0: none)
// and a refusal — in the banner, or for a progress update inside the
// progress box, with what was typed (spec "Errors": an inline message).
type paneOpts struct {
	BookID        int64
	Banner        string
	ProgressError string
	ProgressInput string
}

// renderPanes draws the panes for list c as opts says. A normal request
// gets the whole page — 422 when there is a refusal, so a refused form post
// without JavaScript isn't a 200. An htmx request gets the block for what
// it targeted, as Reader's renderPanes does (#453): #books-list → list-swap
// (the list and its out-of-band companions, the book pane untouched),
// #books-book → book-swap, #books-progress → progress-swap (the box, and
// the list out of band), anything else (#books-panes) → the whole panes.
// Fragments are always 200: htmx's default responseHandling only swaps
// 2xx/3xx.
func (a *App) renderPanes(w http.ResponseWriter, r *http.Request, userID int64, c listCtx, opts paneOpts) {
	ctx := r.Context()
	counts, err := a.store.ShelfCounts(ctx, userID)
	if err != nil {
```

In `internal/apps/books/handlers.go`, replace:

```go
	}
	title := listHeading(c)
	var bv bookView
	if bookID != 0 {
		b, err := a.store.Get(ctx, userID, bookID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		bv = viewBook(b, c, a.store.Today())
		title = b.Title
	}
	page := a.deps.Page(r, title)
	bv.Shell = page.Shell
	v := panesView{Title: page.Title, Shell: page.Shell, Ctx: c, Error: errMsg,
		Sidebar: viewSidebar(c, counts, tags), List: viewList(items, c, bookID), Book: bv}

	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		block := "panes-oob"
```

with:

```go
	}
	title := listHeading(c)
	var bv bookView
	if opts.BookID != 0 {
		b, err := a.store.Get(ctx, userID, opts.BookID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		bv = viewBook(b, c, a.store.Today())
		title = b.Title
		if opts.ProgressError != "" {
			bv.Progress.Error, bv.Progress.Value = opts.ProgressError, opts.ProgressInput
		}
	}
	page := a.deps.Page(r, title)
	bv.Shell = page.Shell
	v := panesView{Title: page.Title, Shell: page.Shell, Ctx: c, Error: opts.Banner,
		Sidebar: viewSidebar(c, counts, tags), List: viewList(items, c, opts.BookID), Book: bv}

	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		block := "panes-oob"
```

In `internal/apps/books/handlers.go`, replace:

```go
			block = "list-swap"
		case "books-book":
			block = "book-swap"
		}
		if err := a.deps.Render.Fragment(w, http.StatusOK, "books/index", block, v); err != nil {
			a.deps.Errors.Internal(w, r, err)
```

with:

```go
			block = "list-swap"
		case "books-book":
			block = "book-swap"
		case "books-progress":
			block = "progress-swap"
			v.List.OOB = true
		}
		if err := a.deps.Render.Fragment(w, http.StatusOK, "books/index", block, v); err != nil {
			a.deps.Errors.Internal(w, r, err)
```

In `internal/apps/books/handlers.go`, replace:

```go
		return
	}
	status := http.StatusOK
	if errMsg != "" {
		status = http.StatusUnprocessableEntity
	}
	page.Data = v
```

with:

```go
		return
	}
	status := http.StatusOK
	if opts.Banner != "" || opts.ProgressError != "" {
		status = http.StatusUnprocessableEntity
	}
	page.Data = v
```

- [ ] **Step 4: The progress handler**

In `internal/apps/books/actions.go`, replace:

```go
		var ref *Refusal
		switch {
		case errors.As(err, &ref):
			a.renderPanes(w, r, uid, c, id, ref.Msg)
			return
		case err != nil:
			a.fail(w, r, err)
```

with:

```go
		var ref *Refusal
		switch {
		case errors.As(err, &ref):
			a.renderPanes(w, r, uid, c, paneOpts{BookID: id, Banner: ref.Msg})
			return
		case err != nil:
			a.fail(w, r, err)
```

In `internal/apps/books/actions.go`, replace:

```go
			// The form posted from the address bar's book; say where the panes
			// now stand (the list, once the book is gone).
			w.Header().Set("HX-Replace-Url", target)
			a.renderPanes(w, r, uid, c, open, "")
			return
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

func (a *App) start(r *http.Request, userID, id int64) error {
```

with:

```go
			// The form posted from the address bar's book; say where the panes
			// now stand (the list, once the book is gone).
			w.Header().Set("HX-Replace-Url", target)
			a.renderPanes(w, r, uid, c, paneOpts{BookID: open})
			return
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

// progress records where the reading in progress stands. htmx aims it at
// the progress box, so the answer is the box with the list out of band;
// a value the store refuses comes back inside the box with what was typed
// (spec "Errors"). Nothing typed is refused like any other bad value.
func (a *App) progress(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	c := ctxFrom(r.PostFormValue)
	typed := strings.TrimSpace(r.PostFormValue("at"))
	at := formInt(r, "at")
	if typed == "" {
		at = -1
	}
	err := a.store.RecordProgress(r.Context(), uid, id, at)
	var ref *Refusal
	switch {
	case errors.As(err, &ref):
		a.renderPanes(w, r, uid, c, paneOpts{BookID: id, ProgressError: ref.Msg, ProgressInput: typed})
		return
	case err != nil:
		a.fail(w, r, err)
		return
	}
	if web.IsHTMX(r) {
		a.renderPanes(w, r, uid, c, paneOpts{BookID: id})
		return
	}
	http.Redirect(w, r, c.BookURL(id), http.StatusSeeOther)
}

func (a *App) start(r *http.Request, userID, id int64) error {
```

In `internal/apps/books/books.go`, replace:

```go
	r.HandleFunc("POST /start/{id}", a.act(a.start, false))
	r.HandleFunc("POST /finish/{id}", a.act(a.finish, false))
	r.HandleFunc("POST /dnf/{id}", a.act(a.dnf, false))
	r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("GET /cover/{id}", a.cover)
```

with:

```go
	r.HandleFunc("POST /start/{id}", a.act(a.start, false))
	r.HandleFunc("POST /finish/{id}", a.act(a.finish, false))
	r.HandleFunc("POST /dnf/{id}", a.act(a.dnf, false))
	r.HandleFunc("POST /progress/{id}", a.progress)
	r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("GET /cover/{id}", a.cover)
```

- [ ] **Step 5: Progress and stars in the view models**

In `internal/apps/books/view.go`, replace:

```go
	URL     string
	Title   string
	Byline  string // "Authors · Series #3"
	Note    string // what the book's shelf says about it: "Started 3 Oct 2026"
	Spine   string // swatch colour name
	Initial string
	Cover   string // the stored cover; "" draws the mini spine
```

with:

```go
	URL     string
	Title   string
	Byline  string // "Authors · Series #3"
	Note    string // what the book's shelf says about it: "Started 3 Oct 2026", "20%"
	Bar     bool   // a reading with progress: draw Percent as a bar before Note
	Percent int
	Rating  int    // 1–5 on a read book, 0 otherwise
	Stars   string // Rating drawn: "★★★★☆"
	Spine   string // swatch colour name
	Initial string
	Cover   string // the stored cover; "" draws the mini spine
```

In `internal/apps/books/view.go`, replace:

```go
	Heading string
	Rows    []rowView
	Empty   string
}

func listHeading(c listCtx) string {
```

with:

```go
	Heading string
	Rows    []rowView
	Empty   string
	OOB     bool // swapped out of band, alongside a progress update
}

func listHeading(c listCtx) string {
```

In `internal/apps/books/view.go`, replace:

```go
func viewList(items []ListItem, c listCtx, openID int64) listView {
	v := listView{Ctx: c, Heading: listHeading(c)}
	for _, it := range items {
		v.Rows = append(v.Rows, rowView{ID: it.ID, URL: c.BookURL(it.ID), Title: it.Title,
			Byline: byline(it.Authors, seriesText(it.SeriesName, it.SeriesNumber)), Note: rowNote(it),
			Spine: SpineColor(it.Title), Initial: initial(it.Title), Cover: coverURL(it.ID, it.CoverVersion), Active: it.ID == openID})
	}
	if len(v.Rows) == 0 {
		v.Empty = emptyText(c)
```

with:

```go
func viewList(items []ListItem, c listCtx, openID int64) listView {
	v := listView{Ctx: c, Heading: listHeading(c)}
	for _, it := range items {
		row := rowView{ID: it.ID, URL: c.BookURL(it.ID), Title: it.Title,
			Byline: byline(it.Authors, seriesText(it.SeriesName, it.SeriesNumber)), Note: rowNote(it),
			Spine: SpineColor(it.Title), Initial: initial(it.Title), Cover: coverURL(it.ID, it.CoverVersion), Active: it.ID == openID}
		switch it.Shelf {
		case ShelfReading:
			row.Bar, row.Percent = it.Progress.Set(), it.Progress.Percent(it.Pages)
		case ShelfRead:
			row.Rating, row.Stars = it.Rating, stars(it.Rating)
		}
		v.Rows = append(v.Rows, row)
	}
	if len(v.Rows) == 0 {
		v.Empty = emptyText(c)
```

In `internal/apps/books/view.go`, replace:

```go
	return "/books/cover/" + strconv.FormatInt(id, 10) + "?v=" + version
}

// rowNote is the right-hand side of a row. B2 replaces the Reading and
// Read notes with a progress bar and stars.
func rowNote(it ListItem) string {
	switch it.Shelf {
	case ShelfReading:
		if it.StartedOn != "" {
			return "Started " + ShowDay(it.StartedOn)
		}
```

with:

```go
	return "/books/cover/" + strconv.FormatInt(id, 10) + "?v=" + version
}

// rowNote is the text on the right-hand side of a row (spec "Layout"):
// how far through a book being read is, when a read book was finished.
func rowNote(it ListItem) string {
	switch it.Shelf {
	case ShelfReading:
		if it.Progress.Set() {
			return strconv.Itoa(it.Progress.Percent(it.Pages)) + "%"
		}
		if it.StartedOn != "" {
			return "Started " + ShowDay(it.StartedOn)
		}
```

In `internal/apps/books/view.go`, replace:

```go
		return "Did not finish"
	}
	return "Added " + it.AddedAt.Local().Format("2 Jan 2006")
}

// countText is "1 book" or "9 books".
```

with:

```go
		return "Did not finish"
	}
	return "Added " + it.AddedAt.Local().Format("2 Jan 2006")
}

// stars draws a 1–5 rating as five stars, "" for none.
func stars(rating int) string {
	if rating < 1 || rating > 5 {
		return ""
	}
	return strings.Repeat("★", rating) + strings.Repeat("☆", 5-rating)
}

// countText is "1 book" or "9 books".
```

In `internal/apps/books/view.go`, replace:

```go
	Tags                             []string
	TagsValue                        string // the tags box: "classics, sf"
	// The reading box.
	Reading    bool   // a reading is in progress
	StartedOn  string // "3 Oct 2026"; "" when unknown
	MinDay     string // the earliest finish date allowed (the start), YYYY-MM-DD
	Today      string // the latest date allowed, YYYY-MM-DD
	StartLabel string // "Start reading", "Read again" or "Start again"
	Ctx        listCtx
	Shell      render.Shell
}
```

with:

```go
	Tags                             []string
	TagsValue                        string // the tags box: "classics, sf"
	// The reading box.
	Reading    bool         // a reading is in progress
	Progress   progressView // its progress, when Reading
	StartedOn  string       // "3 Oct 2026"; "" when unknown
	MinDay     string       // the earliest finish date allowed (the start), YYYY-MM-DD
	Today      string       // the latest date allowed, YYYY-MM-DD
	StartLabel string       // "Start reading", "Read again" or "Start again"
	Ctx        listCtx
	Shell      render.Shell
}
```

In `internal/apps/books/view.go`, replace:

```go
	switch {
	case b.Latest.Status == StatusReading:
		v.Reading = true
		v.MinDay = b.Latest.StartedOn
		if b.Latest.StartedOn != "" {
			v.StartedOn = ShowDay(b.Latest.StartedOn)
```

with:

```go
	switch {
	case b.Latest.Status == StatusReading:
		v.Reading = true
		v.Progress = viewProgress(b)
		v.MinDay = b.Latest.StartedOn
		if b.Latest.StartedOn != "" {
			v.StartedOn = ShowDay(b.Latest.StartedOn)
```

In `internal/apps/books/view.go`, replace:

```go
	return v
}

// EditURL is a book's edit page, coming back to this list afterwards.
func (c listCtx) EditURL(id int64) string {
	return "/books/edit/" + strconv.FormatInt(id, 10) + "?" + c.Query()
```

with:

```go
	return v
}

// progressView is the progress box: an input in the reading's unit, a
// bar and a note. Error and a typed Value come from a refused update.
type progressView struct {
	Unit    Unit
	Value   string // the input: the current progress in Unit ("" for none yet)
	Max     int    // the book's pages, or 100 for percent
	Percent int    // the bar
	Note    string // "20% · updated 9 Oct 2026"
	Error   string
}

// viewProgress is the progress box of b's reading in progress, in the unit
// it is counted in now (UnitFor); an older row in the other unit converts.
func viewProgress(b Book) progressView {
	u := UnitFor(b.Latest.Format, b.Pages)
	v := progressView{Unit: u, Max: 100, Percent: b.Progress.Percent(b.Pages), Note: "No progress yet."}
	if u == UnitPage {
		v.Max = b.Pages
	}
	if b.Progress.Set() {
		v.Value = strconv.Itoa(b.Progress.In(u, b.Pages))
		v.Note = strconv.Itoa(v.Percent) + "% · updated " + b.Progress.RecordedAt.Local().Format("2 Jan 2006")
	}
	return v
}

// EditURL is a book's edit page, coming back to this list afterwards.
func (c listCtx) EditURL(id int64) string {
	return "/books/edit/" + strconv.FormatInt(id, 10) + "?" + c.Query()
```

- [ ] **Step 6: The progress box, the row bar and stars**

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
     do the filter's hidden fields and the phone drill-down boxes. The book
     pane is left as it is; books.js moves the row highlight. */}}
{{define "list-swap"}}{{template "shell-oob" .}}{{template "list" .List}}{{template "sidebar" (dict "Sidebar" .Sidebar "OOB" true)}}{{template "filter-ctx" (dict "Ctx" .Ctx "OOB" true)}}{{template "pane-toggle" (dict "ID" "books-list-open" "Label" "Shelves" "Checked" true "OOB" true)}}{{template "pane-toggle" (dict "ID" "books-book-open" "Label" "Books" "Checked" false "OOB" true)}}{{end}}

{{/* book-swap answers opening a book (target #books-book). The checkbox
     out of band is what makes a phone drill into the book. */}}
```

with:

```html
     do the filter's hidden fields and the phone drill-down boxes. The book
     pane is left as it is; books.js moves the row highlight. */}}
{{define "list-swap"}}{{template "shell-oob" .}}{{template "list" .List}}{{template "sidebar" (dict "Sidebar" .Sidebar "OOB" true)}}{{template "filter-ctx" (dict "Ctx" .Ctx "OOB" true)}}{{template "pane-toggle" (dict "ID" "books-list-open" "Label" "Shelves" "Checked" true "OOB" true)}}{{template "pane-toggle" (dict "ID" "books-book-open" "Label" "Books" "Checked" false "OOB" true)}}{{end}}

{{/* progress-swap answers a progress update (target #books-progress):
     the box, and the list out of band so the row's bar and the Reading
     shelf's order follow (spec "Progress and finishing"). */}}
{{define "progress-swap"}}{{template "progress" .Book}}{{template "list" .List}}{{end}}

{{/* book-swap answers opening a book (target #books-book). The checkbox
     out of band is what makes a phone drill into the book. */}}
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html

{{/* list takes a listView. */}}
{{define "list"}}
<div class="books-list" id="books-list" data-shelf="{{.Ctx.Shelf}}" data-tag="{{.Ctx.Tag}}" data-q="{{.Ctx.Q}}" data-series="{{.Ctx.Series}}">
	<h2 class="books-list-head">{{.Heading}}</h2>
	{{if .Rows}}
	<ul class="books-rows">
```

with:

```html

{{/* list takes a listView. */}}
{{define "list"}}
<div class="books-list" id="books-list" data-shelf="{{.Ctx.Shelf}}" data-tag="{{.Ctx.Tag}}" data-q="{{.Ctx.Q}}" data-series="{{.Ctx.Series}}"{{if .OOB}} hx-swap-oob="true"{{end}}>
	<h2 class="books-list-head">{{.Heading}}</h2>
	{{if .Rows}}
	<ul class="books-rows">
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
				<span class="books-row-text">
					<span class="books-row-title">{{.Title}}</span>
					{{with .Byline}}<span class="books-row-byline">{{.}}</span>{{end}}
					<span class="books-row-note">{{.Note}}</span>
				</span>
			</a>
		</li>
```

with:

```html
				<span class="books-row-text">
					<span class="books-row-title">{{.Title}}</span>
					{{with .Byline}}<span class="books-row-byline">{{.}}</span>{{end}}
					<span class="books-row-note">{{if .Bar}}<progress class="books-row-bar" max="100" value="{{.Percent}}" aria-hidden="true"></progress> {{end}}{{if .Stars}}<span class="books-stars" role="img" aria-label="Rated {{.Rating}} of 5">{{.Stars}}</span> {{end}}{{.Note}}</span>
				</span>
			</a>
		</li>
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
<div class="books-reading">
	{{if .Reading}}
	<p class="books-reading-status">Reading{{with .StartedOn}} since {{.}}{{end}}</p>
	<div class="books-reading-actions">
		{{template "close-reading" (dict "Book" . "Action" "finish" "Label" "Finish" "DayLabel" "Finished on" "Submit" "Mark as read" "Primary" true)}}
		{{template "close-reading" (dict "Book" . "Action" "dnf" "Label" "Did not finish" "DayLabel" "Stopped on" "Submit" "Put it down" "Primary" false)}}
```

with:

```html
<div class="books-reading">
	{{if .Reading}}
	<p class="books-reading-status">Reading{{with .StartedOn}} since {{.}}{{end}}</p>
	{{template "progress" .}}
	<div class="books-reading-actions">
		{{template "close-reading" (dict "Book" . "Action" "finish" "Label" "Finish" "DayLabel" "Finished on" "Submit" "Mark as read" "Primary" true)}}
		{{template "close-reading" (dict "Book" . "Action" "dnf" "Label" "Did not finish" "DayLabel" "Stopped on" "Submit" "Put it down" "Primary" false)}}
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
		<button type="submit" class="primary books-start">{{.StartLabel}}</button>
	</form>
	{{end}}
</div>
{{end}}

```

with:

```html
		<button type="submit" class="primary books-start">{{.StartLabel}}</button>
	</form>
	{{end}}
</div>
{{end}}

{{/* progress is the progress box of a reading in progress: the input
     saves on Enter — htmx swaps this box and the list (progress-swap);
     without JavaScript it posts and comes back. Takes a bookView. */}}
{{define "progress"}}
<div class="books-progress" id="books-progress">
	<form class="books-progress-form" method="post" action="/books/progress/{{.ID}}"
	      hx-post="/books/progress/{{.ID}}" hx-target="#books-progress" hx-swap="outerHTML">
		{{template "post-ctx" .}}
		<label for="books-progress-input">{{if eq .Progress.Unit "page"}}Page{{else}}Percent read{{end}}</label>
		<div class="books-progress-row">
			<input id="books-progress-input" name="at" type="number" inputmode="numeric" min="0" max="{{.Progress.Max}}" value="{{.Progress.Value}}" required{{if .Progress.Error}} aria-invalid="true" aria-describedby="books-progress-error"{{end}}>
			<span class="books-progress-of">{{if eq .Progress.Unit "page"}}of {{.Progress.Max}}{{else}}%{{end}}</span>
			<button type="submit">Save</button>
		</div>
		{{with .Progress.Error}}<p class="books-field-error" id="books-progress-error" role="alert">{{.}}</p>{{end}}
	</form>
	<progress class="books-progress-bar" max="100" value="{{.Progress.Percent}}" aria-label="Progress">{{.Progress.Percent}}%</progress>
	<p class="books-progress-note">{{.Progress.Note}}</p>
</div>
{{end}}

```

- [ ] **Step 7: `p` focuses the progress input; Esc leaves it**

In `internal/apps/books/static/books.js`, replace:

```js
		if (!document.getElementById("books-panes")) return;
		if (isTyping(e.target)) {
			if (e.key === "Escape") {
				if (e.target.id === "books-q") {
					e.target.blur();
				} else {
					// Esc from the date field of an open Finish/DNF disclosure (or
```

with:

```js
		if (!document.getElementById("books-panes")) return;
		if (isTyping(e.target)) {
			if (e.key === "Escape") {
				if (e.target.id === "books-q" || e.target.id === "books-progress-input") {
					e.target.blur();
				} else {
					// Esc from the date field of an open Finish/DNF disclosure (or
```

In `internal/apps/books/static/books.js`, replace:

```js
		case "a":
			window.location.href = "/books/new";
			break;
		case "Escape":
			document.querySelectorAll("details.books-close[open], details.books-menu[open]").forEach(function (d) {
				d.open = false;
```

with:

```js
		case "a":
			window.location.href = "/books/new";
			break;
		case "p":
			var progress = document.getElementById("books-progress-input");
			if (!progress) return;
			progress.focus();
			progress.select();
			break;
		case "Escape":
			document.querySelectorAll("details.books-close[open], details.books-menu[open]").forEach(function (d) {
				d.open = false;
```

- [ ] **Step 8: Styles**

In `internal/ui/static/app.css`, replace:

```css
.books-tags-row { display: flex; gap: var(--s-2); }
.books-tags-row input { flex: 1 1 auto; min-width: 0; }
.books-tags-row button { white-space: nowrap; }
.books-dialog { width: min(28rem, calc(100vw - 2rem)); padding: 1rem; border: var(--border); border-radius: var(--radius); color: var(--c-text); background: var(--c-bg); }
.books-dialog::backdrop { background: rgba(0, 0, 0, 0.35); }

```

with:

```css
.books-tags-row { display: flex; gap: var(--s-2); }
.books-tags-row input { flex: 1 1 auto; min-width: 0; }
.books-tags-row button { white-space: nowrap; }
/* Progress: the box in the reading box, and the bar on a Reading row */
.books-progress { margin-bottom: var(--s-3); }
.books-progress-row { display: flex; flex-wrap: wrap; align-items: center; gap: var(--s-2); }
.books-progress-row input { width: 6rem; }
.books-progress-of { color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-progress-bar,
.books-row-bar { appearance: none; border: 0; border-radius: 999px; overflow: hidden; background: var(--c-bg-inset); }
.books-progress-bar { display: block; width: 100%; max-width: 24rem; height: 6px; margin-top: var(--s-2); }
.books-row-bar { display: inline-block; width: 3rem; height: 4px; vertical-align: middle; }
.books-progress-bar::-webkit-progress-bar,
.books-row-bar::-webkit-progress-bar { background: var(--c-bg-inset); }
.books-progress-bar::-webkit-progress-value,
.books-row-bar::-webkit-progress-value { background: var(--c-accent); }
.books-progress-bar::-moz-progress-bar,
.books-row-bar::-moz-progress-bar { background: var(--c-accent); }
.books-progress-note { margin: var(--s-1) 0 0; color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-stars { color: var(--c-accent); letter-spacing: 0.05em; }
.books-dialog { width: min(28rem, calc(100vw - 2rem)); padding: 1rem; border: var(--border); border-radius: var(--radius); color: var(--c-text); background: var(--c-bg); }
.books-dialog::backdrop { background: rgba(0, 0, 0, 0.35); }

```

- [ ] **Step 9: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
```
Expected: all pass.

- [ ] **Step 10: Commit**

```bash
git add internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): progress box, progress bars and stars in the list (#488)"
```

---

### Task 6: The format menu, a rating on Finish, a stopping place on Did not finish

**Files:**
- Modify: `internal/apps/books/view.go` (`bookView`, `choice`, `formatLabels`, `formatChoices`, `ratingChoices`, `viewBook`), `internal/apps/books/actions.go` (`setFormat`), `internal/apps/books/books.go` (route), `internal/apps/books/templates/panes.partial.html` (`reading-box`, `format-menu`, `close-reading`), `internal/apps/books/static/books.js` (Esc), `internal/ui/static/app.css`
- Test: `internal/apps/books/reading_box_test.go`

**Interfaces:**
- Consumes: Task 2's `FinishReading`/`MarkDNF` form fields `rating`/`at`; Task 3's `Formats`, `SetFormat`; Task 5's `progressView`, `stars`, `readingBook`.
- Produces:
  - `type choice struct { Value, Label string; Current bool }`; `formatLabels map[string]string` ("paper" → "Paper", "ebook" → "Ebook", "audio" → "Audiobook", "" → "Not set"); `formatChoices(current string) []choice`; `ratingChoices(current int) []choice` (5 down to 1)
  - `bookView.FormatLabel string`, `FormatChoices []choice`, `Rating int`, `RatingChoices []choice`
  - route `POST /books/format/{id}`, field `format`
  - template `format-menu` (takes a bookView); the Finish disclosure has `select#books-finish-rating[name=rating]`, the DNF one `input#books-dnf-at[name=at]`
  - books.js: Esc closes any open `<details>` in `#books-book`

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/reading_box_test.go`:

```go
package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestTheFormatMenuSetsTheReadingsFormat(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=reading", id))
	if got := htmlassert.Text(doc.MustHave(".books-format-pill")); got != "Format not set" {
		t.Errorf("pill = %q", got)
	}
	var values []string
	for _, b := range doc.QueryAll(".books-format button") {
		v, _ := htmlassert.Attr(b, "value")
		values = append(values, v)
	}
	if fmt.Sprint(values) != "[paper ebook audio ]" {
		t.Errorf("format buttons = %q", values)
	}

	s.Submit(t, s.Alice, fmt.Sprintf("/books/format/%d", id), url.Values{"shelf": {"reading"}, "format": {"audio"}},
		fmt.Sprintf("/books/b/%d?shelf=reading", id))
	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=reading", id))
	if got := htmlassert.Text(doc.MustHave(".books-format-pill")); got != "Audiobook" {
		t.Errorf("pill after choosing audio = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(`.books-format button[aria-current="true"]`)); got != "Audiobook" {
		t.Errorf("current choice = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-progress-of")); got != "%" {
		t.Errorf("progress unit after audio = %q, want %%", got)
	}

	if rec := s.Post(t, s.Alice, fmt.Sprintf("/books/format/%d", id), url.Values{"format": {"vinyl"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("format vinyl = %d, want 400", rec.Code)
	}
	if rec := s.Post(t, s.Bob, fmt.Sprintf("/books/format/%d", id), url.Values{"format": {"paper"}}); rec.Code != http.StatusNotFound {
		t.Errorf("Bob's format = %d, want 404", rec.Code)
	}
}

func TestFinishOffersARatingAndDNFAPlace(t *testing.T) {
	s := newServer(t)
	id := readingBook(t, s, "Dune", 600)
	ctx := context.Background()
	if err := s.Store.SetRating(ctx, s.Alice.User.ID, id, 3); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.RecordProgress(ctx, s.Alice.User.ID, id, 200); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", id))
	doc.MustHave(`select[name="rating"]`)
	if n := len(doc.QueryAll(`select[name="rating"] option`)); n != 6 {
		t.Errorf("%d rating options, want No rating and 1–5", n)
	}
	if v, _ := htmlassert.Attr(doc.MustHave(`option[selected]`), "value"); v != "3" {
		t.Errorf("selected rating = %q, want the book's 3", v)
	}
	at := doc.MustHave(`input[name="at"]`)
	if v, _ := htmlassert.Attr(at, "max"); v != "600" {
		t.Errorf("DNF page max = %q, want 600", v)
	}
	if v, _ := htmlassert.Attr(at, "value"); v != "200" {
		t.Errorf("DNF page = %q, want the current page 200", v)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `htmlassert: no element matches ".books-format-pill"`.

- [ ] **Step 3: Format and rating choices in the view model**

In `internal/apps/books/view.go`, replace:

```go
	Tags                             []string
	TagsValue                        string // the tags box: "classics, sf"
	// The reading box.
	Reading    bool         // a reading is in progress
	Progress   progressView // its progress, when Reading
	StartedOn  string       // "3 Oct 2026"; "" when unknown
	MinDay     string       // the earliest finish date allowed (the start), YYYY-MM-DD
	Today      string       // the latest date allowed, YYYY-MM-DD
	StartLabel string       // "Start reading", "Read again" or "Start again"
	Ctx        listCtx
	Shell      render.Shell
}

// viewBook draws a book; today bounds the reading box's date fields.
func viewBook(b Book, c listCtx, today string) bookView {
	v := bookView{Selected: true, ID: b.ID, Title: b.Title, Subtitle: b.Subtitle, Authors: b.Authors,
		Description: b.Description, Spine: SpineColor(b.Title), Cover: coverURL(b.ID, b.CoverVersion),
		ShelfLabel: b.Shelf.Label(), Tags: b.Tags, TagsValue: strings.Join(b.Tags, ", "), Today: today, Ctx: c}
	if b.SeriesName != "" {
		v.Series = seriesText(b.SeriesName, b.SeriesNumber)
		if b.SeriesBooks > 1 { // the count only says something once there are two
			v.Series += " · " + countText(b.SeriesBooks, "book", "books")
		}
		v.SeriesURL = listCtx{Shelf: ShelfAll, Series: b.SeriesName}.ListURL()
```

with:

```go
	Tags                             []string
	TagsValue                        string // the tags box: "classics, sf"
	// The reading box.
	Reading       bool         // a reading is in progress
	Progress      progressView // its progress, when Reading
	FormatLabel   string       // its format, for the pill: "Paper", "Format not set"
	FormatChoices []choice     // the format menu
	StartedOn     string       // "3 Oct 2026"; "" when unknown
	MinDay        string       // the earliest finish date allowed (the start), YYYY-MM-DD
	Today         string       // the latest date allowed, YYYY-MM-DD
	StartLabel    string       // "Start reading", "Read again" or "Start again"
	Rating        int          // 1–5, 0 for none
	RatingChoices []choice     // the Finish step's rating select
	Ctx           listCtx
	Shell         render.Shell
}

// choice is one option of a menu or select.
type choice struct {
	Value, Label string
	Current      bool
}

var formatLabels = map[string]string{"paper": "Paper", "ebook": "Ebook", "audio": "Audiobook", "": "Not set"}

// formatChoices is the format menu: every format, then "not set".
func formatChoices(current string) []choice {
	var out []choice
	for _, f := range Formats {
		out = append(out, choice{Value: f, Label: formatLabels[f], Current: f == current})
	}
	return append(out, choice{Value: "", Label: formatLabels[""], Current: current == ""})
}

// ratingChoices is the Finish step's rating select, best first; the book's
// rating is picked already, so finishing a re-read keeps it unless changed.
func ratingChoices(current int) []choice {
	var out []choice
	for n := 5; n >= 1; n-- {
		out = append(out, choice{Value: strconv.Itoa(n), Label: stars(n) + " " + strconv.Itoa(n) + " of 5", Current: n == current})
	}
	return out
}

// viewBook draws a book; today bounds the reading box's date fields.
func viewBook(b Book, c listCtx, today string) bookView {
	v := bookView{Selected: true, ID: b.ID, Title: b.Title, Subtitle: b.Subtitle, Authors: b.Authors,
		Description: b.Description, Spine: SpineColor(b.Title), Cover: coverURL(b.ID, b.CoverVersion),
		ShelfLabel: b.Shelf.Label(), Tags: b.Tags, TagsValue: strings.Join(b.Tags, ", "), Today: today,
		Rating: b.Rating, Ctx: c}
	if b.SeriesName != "" {
		v.Series = seriesText(b.SeriesName, b.SeriesNumber)
		if b.SeriesBooks > 1 { // the count only says something once there are two
			v.Series += " · " + countText(b.SeriesBooks, "book", "books")
		}
		v.SeriesURL = listCtx{Shelf: ShelfAll, Series: b.SeriesName}.ListURL()
```

In `internal/apps/books/view.go`, replace:

```go
	case b.Latest.Status == StatusReading:
		v.Reading = true
		v.Progress = viewProgress(b)
		v.MinDay = b.Latest.StartedOn
		if b.Latest.StartedOn != "" {
			v.StartedOn = ShowDay(b.Latest.StartedOn)
```

with:

```go
	case b.Latest.Status == StatusReading:
		v.Reading = true
		v.Progress = viewProgress(b)
		v.FormatLabel, v.FormatChoices = "Format not set", formatChoices(b.Latest.Format)
		if b.Latest.Format != "" {
			v.FormatLabel = formatLabels[b.Latest.Format]
		}
		v.RatingChoices = ratingChoices(b.Rating)
		v.MinDay = b.Latest.StartedOn
		if b.Latest.StartedOn != "" {
			v.StartedOn = ShowDay(b.Latest.StartedOn)
```

- [ ] **Step 4: The format route**

In `internal/apps/books/actions.go`, replace:

```go
	return a.store.MarkDNF(r.Context(), userID, id, strings.TrimSpace(r.PostFormValue("day")), formInt(r, "at"))
}

func (a *App) setTags(r *http.Request, userID, id int64) error {
	return a.store.SetTags(r.Context(), userID, id, ParseTags(r.PostFormValue("tags")))
}
```

with:

```go
	return a.store.MarkDNF(r.Context(), userID, id, strings.TrimSpace(r.PostFormValue("day")), formInt(r, "at"))
}

func (a *App) setFormat(r *http.Request, userID, id int64) error {
	return a.store.SetFormat(r.Context(), userID, id, r.PostFormValue("format"))
}

func (a *App) setTags(r *http.Request, userID, id int64) error {
	return a.store.SetTags(r.Context(), userID, id, ParseTags(r.PostFormValue("tags")))
}
```

In `internal/apps/books/books.go`, replace:

```go
	r.HandleFunc("POST /finish/{id}", a.act(a.finish, false))
	r.HandleFunc("POST /dnf/{id}", a.act(a.dnf, false))
	r.HandleFunc("POST /progress/{id}", a.progress)
	r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("GET /cover/{id}", a.cover)
```

with:

```go
	r.HandleFunc("POST /finish/{id}", a.act(a.finish, false))
	r.HandleFunc("POST /dnf/{id}", a.act(a.dnf, false))
	r.HandleFunc("POST /progress/{id}", a.progress)
	r.HandleFunc("POST /format/{id}", a.act(a.setFormat, false))
	r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("GET /cover/{id}", a.cover)
```

- [ ] **Step 5: The format menu and the Finish / Did not finish fields**

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
{{define "reading-box"}}
<div class="books-reading">
	{{if .Reading}}
	<p class="books-reading-status">Reading{{with .StartedOn}} since {{.}}{{end}}</p>
	{{template "progress" .}}
	<div class="books-reading-actions">
		{{template "close-reading" (dict "Book" . "Action" "finish" "Label" "Finish" "DayLabel" "Finished on" "Submit" "Mark as read" "Primary" true)}}
```

with:

```html
{{define "reading-box"}}
<div class="books-reading">
	{{if .Reading}}
	<div class="books-reading-head">
		<p class="books-reading-status">Reading{{with .StartedOn}} since {{.}}{{end}}</p>
		{{template "format-menu" .}}
	</div>
	{{template "progress" .}}
	<div class="books-reading-actions">
		{{template "close-reading" (dict "Book" . "Action" "finish" "Label" "Finish" "DayLabel" "Finished on" "Submit" "Mark as read" "Primary" true)}}
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
	</form>
	{{end}}
</div>
{{end}}

{{/* progress is the progress box of a reading in progress: the input
```

with:

```html
	</form>
	{{end}}
</div>
{{end}}

{{/* format-menu sets the format of the reading in progress: a no-JS
     <details> disclosure (PATTERNS.md) whose summary is the format pill.
     Takes a bookView. */}}
{{define "format-menu"}}
<details class="outline-menu books-format">
	<summary class="books-pill books-format-pill" aria-label="Format: {{.FormatLabel}}">{{.FormatLabel}}</summary>
	<form class="outline-menu-list outline-menu-list-end" method="post" action="/books/format/{{.ID}}"
	      hx-post="/books/format/{{.ID}}" hx-target="#books-panes" hx-swap="outerHTML">
		{{template "post-ctx" .}}
		{{range .FormatChoices}}<button type="submit" name="format" value="{{.Value}}"{{if .Current}} aria-current="true"{{end}}>{{.Label}}</button>{{end}}
	</form>
</details>
{{end}}

{{/* progress is the progress box of a reading in progress: the input
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html

{{/* close-reading is Finish or Did not finish: a no-JS <details>
     disclosure (PATTERNS.md) holding the date — today unless changed, never
     before the start — and the button that sends it. */}}
{{define "close-reading"}}
<details class="books-close">
	<summary class="button{{if .Primary}} primary{{end}}">{{.Label}}</summary>
```

with:

```html

{{/* close-reading is Finish or Did not finish: a no-JS <details>
     disclosure (PATTERNS.md) holding the date — today unless changed, never
     before the start — then an optional rating (Finish) or the place it
     stopped (Did not finish), and the button that sends it. */}}
{{define "close-reading"}}
<details class="books-close">
	<summary class="button{{if .Primary}} primary{{end}}">{{.Label}}</summary>
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
		{{template "post-ctx" .Book}}
		<label for="books-{{.Action}}-day">{{.DayLabel}}</label>
		<input id="books-{{.Action}}-day" name="day" type="date" value="{{.Book.Today}}" max="{{.Book.Today}}"{{with .Book.MinDay}} min="{{.}}"{{end}}>
		<button type="submit"{{if .Primary}} class="primary"{{end}}>{{.Submit}}</button>
	</form>
</details>
```

with:

```html
		{{template "post-ctx" .Book}}
		<label for="books-{{.Action}}-day">{{.DayLabel}}</label>
		<input id="books-{{.Action}}-day" name="day" type="date" value="{{.Book.Today}}" max="{{.Book.Today}}"{{with .Book.MinDay}} min="{{.}}"{{end}}>
		{{if eq .Action "finish"}}
		<label for="books-finish-rating">Rating (optional)</label>
		<select id="books-finish-rating" name="rating">
			<option value="">No rating</option>
			{{range .Book.RatingChoices}}<option value="{{.Value}}"{{if .Current}} selected{{end}}>{{.Label}}</option>{{end}}
		</select>
		{{else}}
		<label for="books-dnf-at">Stopped at {{if eq .Book.Progress.Unit "page"}}page{{else}}percent{{end}} (optional)</label>
		<input id="books-dnf-at" name="at" type="number" inputmode="numeric" min="0" max="{{.Book.Progress.Max}}" value="{{.Book.Progress.Value}}">
		{{end}}
		<button type="submit"{{if .Primary}} class="primary"{{end}}>{{.Submit}}</button>
	</form>
</details>
```

- [ ] **Step 6: Esc closes any disclosure in the book pane**

In `internal/apps/books/static/books.js`, replace:

```js
				if (e.target.id === "books-q" || e.target.id === "books-progress-input") {
					e.target.blur();
				} else {
					// Esc from the date field of an open Finish/DNF disclosure (or
					// the menu) closes it and returns focus to its summary.
					var open = e.target.closest("details.books-close[open], details.books-menu[open]");
					if (open) {
						open.open = false;
						var summary = open.querySelector("summary");
```

with:

```js
				if (e.target.id === "books-q" || e.target.id === "books-progress-input") {
					e.target.blur();
				} else {
					// Esc from a field in an open disclosure of the book pane
					// (Finish, Did not finish, the menus) closes it and returns
					// focus to its summary.
					var open = e.target.closest("#books-book details[open]");
					if (open) {
						open.open = false;
						var summary = open.querySelector("summary");
```

In `internal/apps/books/static/books.js`, replace:

```js
			progress.select();
			break;
		case "Escape":
			document.querySelectorAll("details.books-close[open], details.books-menu[open]").forEach(function (d) {
				d.open = false;
			});
			return;
```

with:

```js
			progress.select();
			break;
		case "Escape":
			document.querySelectorAll("#books-book details[open]").forEach(function (d) {
				d.open = false;
			});
			return;
```

- [ ] **Step 7: Styles**

In `internal/ui/static/app.css`, replace:

```css
/* Reading box, tags, confirm dialog */
.books-reading { margin-bottom: var(--s-4); padding: var(--s-3); border: var(--border); border-radius: var(--radius); background: var(--c-bg-subtle); }
.books-reading-status { margin: 0 0 var(--s-2); font-weight: 600; }
.books-reading-actions { display: flex; flex-wrap: wrap; align-items: flex-start; gap: var(--s-2); }
.books-close > summary { display: inline-flex; list-style: none; cursor: pointer; }
.books-close > summary::-webkit-details-marker { display: none; }
```

with:

```css
/* Reading box, tags, confirm dialog */
.books-reading { margin-bottom: var(--s-4); padding: var(--s-3); border: var(--border); border-radius: var(--radius); background: var(--c-bg-subtle); }
.books-reading-status { margin: 0 0 var(--s-2); font-weight: 600; }
.books-reading-head { display: flex; flex-wrap: wrap; align-items: baseline; gap: var(--s-2); }
.books-format > summary { list-style: none; cursor: pointer; }
.books-format > summary::-webkit-details-marker { display: none; }
/* An outlined pill: the reading box is the same colour as a plain one. */
.books-format-pill { box-shadow: inset 0 0 0 1px var(--c-border-firm); }
.books-format-pill:hover { color: var(--c-text); }
.books-format button[aria-current="true"] { font-weight: 600; }
.books-reading-actions { display: flex; flex-wrap: wrap; align-items: flex-start; gap: var(--s-2); }
.books-close > summary { display: inline-flex; list-style: none; cursor: pointer; }
.books-close > summary::-webkit-details-marker { display: none; }
```

- [ ] **Step 8: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
```
Expected: all pass.

- [ ] **Step 9: Commit**

```bash
git add internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): format menu, rating on Finish, stopping place on Did not finish (#488)"
```

---

### Task 7: Rating stars and a Markdown review in the book pane

**Files:**
- Create: `internal/apps/books/markdown.go`
- Modify: `internal/apps/books/view.go` (imports, `bookView`, `starButton`, `starButtons`, `viewBook`), `internal/apps/books/actions.go` (`setRating`, `setReview`), `internal/apps/books/books.go` (routes), `internal/apps/books/templates/panes.partial.html` (`book`, `rating`, `review`), `internal/ui/static/app.css`
- Test: `internal/apps/books/markdown_test.go`, `internal/apps/books/rating_view_test.go`

**Interfaces:**
- Consumes: Task 2's `SetRating`, `SetReview`, `formInt`; `Book.Rating`, `Book.Review`; `ticon` ("star-filled", "star-outline").
- Produces:
  - `func RenderReview(s string) template.HTML` — escaped; `**bold**`, `*italic*`, `` `code` ``, `~~strike~~`, `[text](http…)`, bare http(s) links; blank line = new `<p>`, single newline = `<br>`
  - `type starButton struct { Value int; Label string; On bool }`; `starButtons(rating int) []starButton` (the current rating's star posts 0)
  - `bookView.Stars []starButton`, `bookView.Review string`, `bookView.ReviewHTML template.HTML`
  - routes `POST /books/rating/{id}` (field `rating`), `POST /books/review/{id}` (field `review`)
  - templates `rating`, `review` (take a bookView)

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/markdown_test.go`:

```go
package books_test

import (
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

func TestRenderReview(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"paragraphs and line breaks", "One\ntwo\n\n\nThree", "<p>One<br>two</p><p>Three</p>"},
		{"inline", "**Loved** it, *mostly* — ~~not~~ `x*y`",
			"<p><strong>Loved</strong> it, <em>mostly</em> — <s>not</s> <code>x*y</code></p>"},
		{"link", "[OL](https://openlibrary.org/works/OL1W)",
			`<p><a href="https://openlibrary.org/works/OL1W" target="_blank" rel="noopener noreferrer">OL</a></p>`},
		{"bare link", "see https://example.com/a_(b) too",
			`<p>see <a href="https://example.com/a_(b)" target="_blank" rel="noopener noreferrer">https://example.com/a_(b)</a> too</p>`},
		{"no javascript links", "[x](javascript:alert(1))", "<p>[x](javascript:alert(1))</p>"},
		{"html is text", "<script>alert(1)</script> & co", "<p>&lt;script&gt;alert(1)&lt;/script&gt; &amp; co</p>"},
		{"windows line ends", "a\r\n\r\nb", "<p>a</p><p>b</p>"},
	}
	for _, tt := range tests {
		if got := string(books.RenderReview(tt.in)); got != tt.want {
			t.Errorf("%s: RenderReview(%q) =\n %s\nwant\n %s", tt.name, tt.in, got, tt.want)
		}
	}
}
```

Create `internal/apps/books/rating_view_test.go`:

```go
package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func starValues(doc *htmlassert.Doc) []string {
	var out []string
	for _, b := range doc.QueryAll(".books-star") {
		v, _ := htmlassert.Attr(b, "value")
		out = append(out, v)
	}
	return out
}

func TestStarsSetAndClearTheRating(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfRead))
	page := fmt.Sprintf("/books/b/%d?shelf=read", id)
	doc := s.Get(t, s.Alice, page)
	if got := fmt.Sprint(starValues(doc)); got != "[1 2 3 4 5]" {
		t.Errorf("unrated star values = %s", got)
	}
	doc.MustNotHave(".is-on")

	s.Submit(t, s.Alice, fmt.Sprintf("/books/rating/%d", id), url.Values{"shelf": {"read"}, "rating": {"4"}}, page)
	doc = s.Get(t, s.Alice, page)
	if got := fmt.Sprint(starValues(doc)); got != "[1 2 3 0 5]" {
		t.Errorf("rated-4 star values = %s, want the 4th to clear", got)
	}
	if n := len(doc.QueryAll(".is-on")); n != 4 {
		t.Errorf("%d stars on, want 4", n)
	}
	if v, _ := htmlassert.Attr(doc.QueryAll(".books-star")[3], "aria-label"); v != "Clear the rating (4 of 5)" {
		t.Errorf("4th star label = %q", v)
	}

	s.Submit(t, s.Alice, fmt.Sprintf("/books/rating/%d", id), url.Values{"shelf": {"read"}, "rating": {"0"}}, page)
	if b, _ := s.Store.Get(context.Background(), s.Alice.User.ID, id); b.Rating != 0 {
		t.Errorf("rating after clearing = %d", b.Rating)
	}
	if rec := s.Post(t, s.Alice, fmt.Sprintf("/books/rating/%d", id), url.Values{"rating": {"9"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("rating 9 = %d, want 400", rec.Code)
	}
	if rec := s.Post(t, s.Bob, fmt.Sprintf("/books/rating/%d", id), url.Values{"rating": {"1"}}); rec.Code != http.StatusNotFound {
		t.Errorf("Bob's rating = %d, want 404", rec.Code)
	}
}

func TestReviewIsWrittenAndShown(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfRead))
	page := fmt.Sprintf("/books/b/%d?shelf=read", id)
	doc := s.Get(t, s.Alice, page)
	doc.MustNotHave(".books-review-text")
	if got := htmlassert.Text(doc.MustHave(".books-review-edit summary")); got != "Write a review" {
		t.Errorf("summary = %q", got)
	}

	rec := s.PostHX(t, s.Alice, fmt.Sprintf("/books/review/%d", id), url.Values{"shelf": {"read"}, "review": {"So much **sand**.\n\nWorth it."}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx review = %d, want 200", rec.Code)
	}
	doc = htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#books-panes")
	if got := htmlassert.Text(doc.MustHave(".books-review-text strong")); got != "sand" {
		t.Errorf("bold = %q", got)
	}
	if n := len(doc.QueryAll(".books-review-text p")); n != 2 {
		t.Errorf("%d paragraphs, want 2", n)
	}
	if got := htmlassert.Text(doc.MustHave(".books-review-edit summary")); got != "Edit review" {
		t.Errorf("summary = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave("textarea#books-review-input")); got != "So much **sand**. Worth it." {
		t.Errorf("textarea = %q", got)
	}
	// Without JavaScript the form posts and comes back.
	s.Submit(t, s.Alice, fmt.Sprintf("/books/review/%d", id), url.Values{"shelf": {"read"}, "review": {""}}, page)
	s.Get(t, s.Alice, page).MustNotHave(".books-review-text")
	if rec := s.Post(t, s.Bob, fmt.Sprintf("/books/review/%d", id), url.Values{"review": {"x"}}); rec.Code != http.StatusNotFound {
		t.Errorf("Bob's review = %d, want 404", rec.Code)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `undefined: books.RenderReview`.

- [ ] **Step 3: Write `markdown.go`**

Create `internal/apps/books/markdown.go`:

```go
package books

import (
	"html"
	"html/template"
	"regexp"
	"strings"
)

// codePattern finds `code` spans, whose content is verbatim.
var codePattern = regexp.MustCompile("`([^`]+)`")

// inlinePattern is every other inline construct, tried in this order at
// each position: bold before italic, or "**bold**" would read as an italic
// span starting one character in. RE2, so no input can make it backtrack.
var inlinePattern = regexp.MustCompile(
	`\*\*([^*]+)\*\*` + // 1: bold
		`|\*([^*]+)\*` + // 2: italic
		`|~~([^~]+)~~` + // 3: strike
		`|\[([^\]]+)\]\(([^)]+)\)` + // 4,5: link text, url
		`|(https?://(?:\([^\s<>"')\]]*\)|[^\s<>"')\]])+)`, // 6: bare link
)

// RenderReview turns a review's Markdown into HTML (spec "Data model":
// review is Markdown). Inline, it mirrors ON Notes' renderer
// (internal/apps/notes/markdown.go) without Notes' #tag chips — apps never
// import each other, so this is an independent copy: **bold**, *italic*,
// `code`, ~~strike~~, [text](url) and bare http(s) links. On top of that a
// blank line starts a new paragraph and a single line break stays one.
// Everything else is literal text.
//
// The output is built from html.EscapeString-escaped pieces and a fixed
// set of tags, never from the input directly, so no review can inject
// markup.
func RenderReview(s string) template.HTML {
	var b strings.Builder
	for _, para := range paragraphs(s) {
		b.WriteString("<p>")
		for i, line := range para {
			if i > 0 {
				b.WriteString("<br>")
			}
			renderCodeSpans(&b, line)
		}
		b.WriteString("</p>")
	}
	return template.HTML(b.String())
}

// paragraphs splits s into runs of non-blank lines.
func paragraphs(s string) [][]string {
	var out [][]string
	var cur []string
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			if cur != nil {
				out, cur = append(out, cur), nil
			}
			continue
		}
		cur = append(cur, line)
	}
	if cur != nil {
		out = append(out, cur)
	}
	return out
}

// renderCodeSpans renders `code` spans verbatim and everything between
// them through renderInline.
func renderCodeSpans(b *strings.Builder, s string) {
	last := 0
	for _, m := range codePattern.FindAllStringSubmatchIndex(s, -1) {
		renderInline(b, s[last:m[0]])
		b.WriteString("<code>")
		b.WriteString(html.EscapeString(s[m[2]:m[3]]))
		b.WriteString("</code>")
		last = m[1]
	}
	renderInline(b, s[last:])
}

// renderInline handles everything inlinePattern matches in one
// left-to-right pass, escaping the literal text in between.
func renderInline(b *strings.Builder, s string) {
	last := 0
	for _, m := range inlinePattern.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(html.EscapeString(s[last:m[0]]))
		switch {
		case m[2] >= 0:
			b.WriteString("<strong>" + html.EscapeString(s[m[2]:m[3]]) + "</strong>")
		case m[4] >= 0:
			b.WriteString("<em>" + html.EscapeString(s[m[4]:m[5]]) + "</em>")
		case m[6] >= 0:
			b.WriteString("<s>" + html.EscapeString(s[m[6]:m[7]]) + "</s>")
		case m[8] >= 0:
			writeLink(b, s[m[8]:m[9]], s[m[10]:m[11]], s[m[0]:m[1]])
		case m[12] >= 0:
			writeLink(b, s[m[12]:m[13]], s[m[12]:m[13]], s[m[12]:m[13]])
		}
		last = m[1]
	}
	b.WriteString(html.EscapeString(s[last:]))
}

// writeLink is the only place a link is made: http and https only, opened
// in a new tab without a way back (noopener). Any other scheme —
// javascript: above all — is shown as the source text, as typed.
func writeLink(b *strings.Builder, text, href, source string) {
	scheme := strings.ToLower(href)
	if !strings.HasPrefix(scheme, "http://") && !strings.HasPrefix(scheme, "https://") {
		b.WriteString(html.EscapeString(source))
		return
	}
	b.WriteString(`<a href="` + html.EscapeString(href) + `" target="_blank" rel="noopener noreferrer">`)
	b.WriteString(html.EscapeString(text) + `</a>`)
}
```

- [ ] **Step 4: Stars and the review in the view model**

In `internal/apps/books/view.go`, replace:

```go
package books

import (
	"net/url"
	"strconv"
	"strings"
```

with:

```go
package books

import (
	"html/template"
	"net/url"
	"strconv"
	"strings"
```

In `internal/apps/books/view.go`, replace:

```go
	StartLabel    string       // "Start reading", "Read again" or "Start again"
	Rating        int          // 1–5, 0 for none
	RatingChoices []choice     // the Finish step's rating select
	Ctx           listCtx
	Shell         render.Shell
}
```

with:

```go
	StartLabel    string       // "Start reading", "Read again" or "Start again"
	Rating        int          // 1–5, 0 for none
	RatingChoices []choice     // the Finish step's rating select
	Stars         []starButton // the rating buttons
	Review        string       // Markdown, for the edit box
	ReviewHTML    template.HTML
	Ctx           listCtx
	Shell         render.Shell
}
```

In `internal/apps/books/view.go`, replace:

```go
	return append(out, choice{Value: "", Label: formatLabels[""], Current: current == ""})
}

// ratingChoices is the Finish step's rating select, best first; the book's
// rating is picked already, so finishing a re-read keeps it unless changed.
func ratingChoices(current int) []choice {
```

with:

```go
	return append(out, choice{Value: "", Label: formatLabels[""], Current: current == ""})
}

// starButton is one of the book pane's five rating buttons.
type starButton struct {
	Value int // what clicking it sets: its number, or 0 to clear
	Label string
	On    bool // drawn filled
}

// starButtons are the book pane's rating (spec "Book pane": click to set,
// click again to clear): star n sets the rating to n, except the current
// rating's own star, which clears it.
func starButtons(rating int) []starButton {
	var out []starButton
	for n := 1; n <= 5; n++ {
		b := starButton{Value: n, Label: "Rate it " + strconv.Itoa(n) + " of 5", On: n <= rating}
		if n == rating {
			b.Value, b.Label = 0, "Clear the rating ("+strconv.Itoa(n)+" of 5)"
		}
		out = append(out, b)
	}
	return out
}

// ratingChoices is the Finish step's rating select, best first; the book's
// rating is picked already, so finishing a re-read keeps it unless changed.
func ratingChoices(current int) []choice {
```

In `internal/apps/books/view.go`, replace:

```go
	v := bookView{Selected: true, ID: b.ID, Title: b.Title, Subtitle: b.Subtitle, Authors: b.Authors,
		Description: b.Description, Spine: SpineColor(b.Title), Cover: coverURL(b.ID, b.CoverVersion),
		ShelfLabel: b.Shelf.Label(), Tags: b.Tags, TagsValue: strings.Join(b.Tags, ", "), Today: today,
		Rating: b.Rating, Ctx: c}
	if b.SeriesName != "" {
		v.Series = seriesText(b.SeriesName, b.SeriesNumber)
		if b.SeriesBooks > 1 { // the count only says something once there are two
			v.Series += " · " + countText(b.SeriesBooks, "book", "books")
		}
		v.SeriesURL = listCtx{Shelf: ShelfAll, Series: b.SeriesName}.ListURL()
```

with:

```go
	v := bookView{Selected: true, ID: b.ID, Title: b.Title, Subtitle: b.Subtitle, Authors: b.Authors,
		Description: b.Description, Spine: SpineColor(b.Title), Cover: coverURL(b.ID, b.CoverVersion),
		ShelfLabel: b.Shelf.Label(), Tags: b.Tags, TagsValue: strings.Join(b.Tags, ", "), Today: today,
		Rating: b.Rating, Stars: starButtons(b.Rating), Review: b.Review, ReviewHTML: RenderReview(b.Review), Ctx: c}
	if b.SeriesName != "" {
		v.Series = seriesText(b.SeriesName, b.SeriesNumber)
		if b.SeriesBooks > 1 { // the count only says something once there are two
			v.Series += " · " + countText(b.SeriesBooks, "book", "books")
		}
		v.SeriesURL = listCtx{Shelf: ShelfAll, Series: b.SeriesName}.ListURL()
```

- [ ] **Step 5: The rating and review routes**

In `internal/apps/books/actions.go`, replace:

```go
	return a.store.SetFormat(r.Context(), userID, id, r.PostFormValue("format"))
}

func (a *App) setTags(r *http.Request, userID, id int64) error {
	return a.store.SetTags(r.Context(), userID, id, ParseTags(r.PostFormValue("tags")))
}
```

with:

```go
	return a.store.SetFormat(r.Context(), userID, id, r.PostFormValue("format"))
}

func (a *App) setRating(r *http.Request, userID, id int64) error {
	return a.store.SetRating(r.Context(), userID, id, formInt(r, "rating"))
}

func (a *App) setReview(r *http.Request, userID, id int64) error {
	return a.store.SetReview(r.Context(), userID, id, r.PostFormValue("review"))
}

func (a *App) setTags(r *http.Request, userID, id int64) error {
	return a.store.SetTags(r.Context(), userID, id, ParseTags(r.PostFormValue("tags")))
}
```

In `internal/apps/books/books.go`, replace:

```go
	r.HandleFunc("POST /dnf/{id}", a.act(a.dnf, false))
	r.HandleFunc("POST /progress/{id}", a.progress)
	r.HandleFunc("POST /format/{id}", a.act(a.setFormat, false))
	r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("GET /cover/{id}", a.cover)
```

with:

```go
	r.HandleFunc("POST /dnf/{id}", a.act(a.dnf, false))
	r.HandleFunc("POST /progress/{id}", a.progress)
	r.HandleFunc("POST /format/{id}", a.act(a.setFormat, false))
	r.HandleFunc("POST /rating/{id}", a.act(a.setRating, false))
	r.HandleFunc("POST /review/{id}", a.act(a.setReview, false))
	r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("GET /cover/{id}", a.cover)
```

- [ ] **Step 6: The rating and review in the book pane**

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
		{{template "book-menu" .}}
	</header>
	{{template "reading-box" .}}
	{{with .Description}}<div class="books-description">{{.}}</div>{{end}}
	{{template "tags-form" .}}
{{else}}
```

with:

```html
		{{template "book-menu" .}}
	</header>
	{{template "reading-box" .}}
	{{template "rating" .}}
	{{template "review" .}}
	{{with .Description}}<div class="books-description">{{.}}</div>{{end}}
	{{template "tags-form" .}}
{{else}}
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
</details>
{{end}}

{{/* tags-form edits a book's tags in place. Takes a bookView. */}}
{{define "tags-form"}}
<form class="books-tags-form" method="post" action="/books/tags/{{.ID}}"
```

with:

```html
</details>
{{end}}

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

{{/* review is the book's review, rendered, and a no-JS <details> with a
     textarea to write or edit it (decided 2026-10-09); htmx swaps the panes
     on save. Takes a bookView. */}}
{{define "review"}}
<section class="books-review" aria-label="Review">
	{{with .ReviewHTML}}<div class="books-review-text">{{.}}</div>{{end}}
	<details class="books-review-edit">
		<summary class="button">{{if .Review}}Edit review{{else}}Write a review{{end}}</summary>
		<form class="books-review-form" method="post" action="/books/review/{{.ID}}"
		      hx-post="/books/review/{{.ID}}" hx-target="#books-panes" hx-swap="outerHTML">
			{{template "post-ctx" .}}
			<label for="books-review-input">Review</label>
			<textarea id="books-review-input" name="review" rows="8">{{.Review}}</textarea>
			<p class="books-hint">Markdown works: **bold**, *italic*, [a link](https://…). A blank line starts a new paragraph.</p>
			<button type="submit" class="primary">Save review</button>
		</form>
	</details>
</section>
{{end}}

{{/* tags-form edits a book's tags in place. Takes a bookView. */}}
{{define "tags-form"}}
<form class="books-tags-form" method="post" action="/books/tags/{{.ID}}"
```

- [ ] **Step 7: Styles**

In `internal/ui/static/app.css`, replace:

```css
.books-row-bar::-moz-progress-bar { background: var(--c-accent); }
.books-progress-note { margin: var(--s-1) 0 0; color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-stars { color: var(--c-accent); letter-spacing: 0.05em; }
.books-dialog { width: min(28rem, calc(100vw - 2rem)); padding: 1rem; border: var(--border); border-radius: var(--radius); color: var(--c-text); background: var(--c-bg); }
.books-dialog::backdrop { background: rgba(0, 0, 0, 0.35); }

```

with:

```css
.books-row-bar::-moz-progress-bar { background: var(--c-accent); }
.books-progress-note { margin: var(--s-1) 0 0; color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-stars { color: var(--c-accent); letter-spacing: 0.05em; }

/* Rating and review */
.books-rating { display: flex; flex-wrap: wrap; align-items: center; gap: var(--s-1); margin-bottom: var(--s-3); }
.books-rating-label { margin-right: var(--s-1); color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-star { display: inline-flex; padding: var(--s-1); border: 0; background: none; color: var(--c-text-faint); cursor: pointer; }
.books-star:hover,
.books-star.is-on { color: var(--c-accent); }
.books-review { max-width: 38rem; margin-bottom: var(--s-4); }
.books-review-text p { margin: 0 0 var(--s-2); }
.books-review-edit > summary { display: inline-flex; list-style: none; cursor: pointer; }
.books-review-edit > summary::-webkit-details-marker { display: none; }
.books-review-form { display: flex; flex-direction: column; gap: var(--s-2); margin-top: var(--s-2); }
.books-review-form label { margin: 0; }
.books-review-form textarea { width: 100%; }
.books-review-form button { align-self: flex-start; }
.books-hint { margin: 0; color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-dialog { width: min(28rem, calc(100vw - 2rem)); padding: 1rem; border: var(--border); border-radius: var(--radius); color: var(--c-text); background: var(--c-bg); }
.books-dialog::backdrop { background: rgba(0, 0, 0, 0.35); }

```

- [ ] **Step 8: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... ./internal/arch/... -count=1
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
```
Expected: all pass (the arch tests confirm Books still imports no other app and no goldmark).

- [ ] **Step 9: Commit**

```bash
git add internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): rating stars and a Markdown review in the book pane (#488)"
```

---

### Task 8: Reading history in the book pane

**Files:**
- Modify: `internal/apps/books/handlers.go` (`renderPanes` loads the readings), `internal/apps/books/view.go` (`bookView.History`, `statusLabels`, `historyView`, `viewHistory`, `readingDates`), `internal/apps/books/actions.go` (`readingID`, `editReading`, `deleteReading`), `internal/apps/books/books.go` (routes), `internal/apps/books/templates/panes.partial.html` (`book`, `history`), `internal/ui/static/app.css`
- Test: `internal/apps/books/history_view_test.go`

**Interfaces:**
- Consumes: Task 3's `Readings`, `ReadingEdit`, `UpdateReading`, `DeleteReading`; Task 6's `choice`, `formatChoices`, `formatLabels`; `ShowDay`; `act`; test helpers `readingBook`, `shelfOf`.
- Produces:
  - `type historyView struct { ID int64; Status, Dates, Format string; InProgress bool; FinishLabel, StartedOn, FinishedOn string; Formats []choice }`; `viewHistory(rs []Reading) []historyView`; `readingDates(rd Reading) string`; `statusLabels map[Status]string`
  - `bookView.History []historyView`
  - routes `POST /books/readings/{id}/{rid}` (fields `started_on`, `finished_on`, `format`) and `POST /books/readings/{id}/{rid}/delete`; `{id}` is the book, `{rid}` the reading
  - `readingID(r *http.Request) (int64, error)` — `ErrNotFound` for a bad `{rid}`
  - template `history` (takes a bookView)

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/history_view_test.go`:

```go
package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// readTwiceOverHTTP is a book Alice read in October (paper, 1st to 2nd)
// and has been reading again since the 5th; today is the 9th.
func readTwiceOverHTTP(t *testing.T, s *server) int64 {
	t.Helper()
	ctx := context.Background()
	uid := s.Alice.User.ID
	s.Clock.Set(time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC))
	id := readingBook(t, s, "Dune", 600)
	if err := s.Store.SetFormat(ctx, uid, id, "paper"); err != nil {
		t.Fatal(err)
	}
	s.Clock.Set(time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC))
	if err := s.Store.FinishReading(ctx, uid, id, "2026-10-02", 0); err != nil {
		t.Fatal(err)
	}
	s.Clock.Set(time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC))
	if err := s.Store.StartReading(ctx, uid, id); err != nil {
		t.Fatal(err)
	}
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	return id
}

func historyIDs(t *testing.T, s *server, id int64) []int64 {
	t.Helper()
	rs, err := s.Store.Readings(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	var out []int64
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}

func TestTheBookPaneListsEveryReading(t *testing.T) {
	s := newServer(t)
	id := readTwiceOverHTTP(t, s)
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", id))
	var lines []string
	for _, n := range doc.QueryAll(".books-history-line") {
		lines = append(lines, htmlassert.Text(n))
	}
	want := []string{"Reading From 5 Oct 2026 · Paper", "Read 1 Oct 2026 – 2 Oct 2026 · Paper"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Errorf("history = %q, want %q", lines, want)
	}
	if n := len(doc.QueryAll(`.books-history-item input[name="finished_on"]`)); n != 1 {
		t.Errorf("%d finish-date fields, want 1: the reading in progress has none", n)
	}
	if n := len(doc.QueryAll(".books-history-item button[hx-confirm]")); n != 2 {
		t.Errorf("%d delete buttons, want one per reading", n)
	}

	want0 := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", want0)).MustNotHave(".books-history")
}

func TestEditingAReading(t *testing.T) {
	s := newServer(t)
	id := readTwiceOverHTTP(t, s)
	past := historyIDs(t, s, id)[1]
	path := fmt.Sprintf("/books/readings/%d/%d", id, past)
	s.Submit(t, s.Alice, path, url.Values{"shelf": {"read"}, "started_on": {"2026-09-20"}, "finished_on": {"2026-09-28"}, "format": {"ebook"}},
		fmt.Sprintf("/books/b/%d?shelf=read", id))
	rs, _ := s.Store.Readings(context.Background(), s.Alice.User.ID, id)
	if got := rs[1]; got.StartedOn != "2026-09-20" || got.FinishedOn != "2026-09-28" || got.Format != "ebook" {
		t.Errorf("edited reading = %+v", got)
	}

	form := url.Values{"shelf": {"read"}, "started_on": {"2026-09-28"}, "finished_on": {"2026-09-20"}}
	if rec := s.Post(t, s.Alice, path, form); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("finish before start = %d, want 422", rec.Code)
	}
	rec := s.PostHX(t, s.Alice, path, url.Values{"shelf": {"read"}, "started_on": {"2026-09-28"}, "finished_on": {"2026-09-20"}})
	if got := htmlassert.Text(htmlassert.Parse(t, rec.Body.String()).MustHave(".books-banner")); got != "The finish date is before the start." {
		t.Errorf("banner = %q", got)
	}
}

func TestDeletingAReading(t *testing.T) {
	s := newServer(t)
	id := readTwiceOverHTTP(t, s)
	current := historyIDs(t, s, id)[0]
	s.Submit(t, s.Alice, fmt.Sprintf("/books/readings/%d/%d/delete", id, current), url.Values{"shelf": {"reading"}},
		fmt.Sprintf("/books/b/%d?shelf=reading", id))
	if got := shelfOf(t, s, id); got != books.ShelfRead {
		t.Errorf("shelf after deleting the re-read = %q, want read", got)
	}
	if n := len(historyIDs(t, s, id)); n != 1 {
		t.Errorf("%d readings left, want 1", n)
	}
}

func TestReadingRoutesAreNotFoundForOthers(t *testing.T) {
	s := newServer(t)
	id := readTwiceOverHTTP(t, s)
	rid := historyIDs(t, s, id)[0]
	other := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	for _, tt := range []struct {
		sess string
		path string
	}{
		{"bob", fmt.Sprintf("/books/readings/%d/%d", id, rid)},
		{"bob", fmt.Sprintf("/books/readings/%d/%d/delete", id, rid)},
		{"alice", fmt.Sprintf("/books/readings/%d/%d/delete", other, rid)},
		{"alice", fmt.Sprintf("/books/readings/%d/x", id)},
	} {
		sess := s.Alice
		if tt.sess == "bob" {
			sess = s.Bob
		}
		if rec := s.Post(t, sess, tt.path, url.Values{}); rec.Code != http.StatusNotFound {
			t.Errorf("%s POST %s = %d, want 404", tt.sess, tt.path, rec.Code)
		}
	}
	if n := len(historyIDs(t, s, id)); n != 2 {
		t.Errorf("%d readings, want both still there", n)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — no `.books-history-line` elements, and `POST /books/readings/… = 404, want 303` (no route yet).

- [ ] **Step 3: Load the readings for the open book**

In `internal/apps/books/handlers.go`, replace:

```go
			a.fail(w, r, err)
			return
		}
		bv = viewBook(b, c, a.store.Today())
		title = b.Title
		if opts.ProgressError != "" {
			bv.Progress.Error, bv.Progress.Value = opts.ProgressError, opts.ProgressInput
```

with:

```go
			a.fail(w, r, err)
			return
		}
		rs, err := a.store.Readings(ctx, userID, opts.BookID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		bv = viewBook(b, c, a.store.Today())
		bv.History = viewHistory(rs)
		title = b.Title
		if opts.ProgressError != "" {
			bv.Progress.Error, bv.Progress.Value = opts.ProgressError, opts.ProgressInput
```

- [ ] **Step 4: The history view model**

In `internal/apps/books/view.go`, replace:

```go
	Stars         []starButton // the rating buttons
	Review        string       // Markdown, for the edit box
	ReviewHTML    template.HTML
	Ctx           listCtx
	Shell         render.Shell
}
```

with:

```go
	Stars         []starButton // the rating buttons
	Review        string       // Markdown, for the edit box
	ReviewHTML    template.HTML
	History       []historyView // every reading, newest first
	Ctx           listCtx
	Shell         render.Shell
}
```

In `internal/apps/books/view.go`, replace:

```go
	return v
}

// progressView is the progress box: an input in the reading's unit, a
// bar and a note. Error and a typed Value come from a refused update.
type progressView struct {
```

with:

```go
	return v
}

var statusLabels = map[Status]string{StatusReading: "Reading", StatusFinished: "Read", StatusDNF: "Did not finish"}

// historyView is one reading in the book pane's history, with what its
// edit form needs.
type historyView struct {
	ID          int64
	Status      string // "Reading", "Read", "Did not finish"
	Dates       string // "3 Oct 2026 – 9 Oct 2026"
	Format      string // "Paper"; "" when not set
	InProgress  bool   // no finish date to edit
	FinishLabel string // "Finished on" or "Stopped on"
	StartedOn   string // YYYY-MM-DD for the date inputs
	FinishedOn  string
	Formats     []choice
}

func viewHistory(rs []Reading) []historyView {
	var out []historyView
	for _, rd := range rs {
		h := historyView{ID: rd.ID, Status: statusLabels[rd.Status], Dates: readingDates(rd),
			InProgress: rd.Status == StatusReading, FinishLabel: "Finished on",
			StartedOn: rd.StartedOn, FinishedOn: rd.FinishedOn, Formats: formatChoices(rd.Format)}
		if rd.Format != "" {
			h.Format = formatLabels[rd.Format]
		}
		if rd.Status == StatusDNF {
			h.FinishLabel = "Stopped on"
		}
		out = append(out, h)
	}
	return out
}

// readingDates is a reading's dates for people; imported readings may
// have neither (B5).
func readingDates(rd Reading) string {
	switch {
	case rd.StartedOn != "" && rd.FinishedOn != "":
		return ShowDay(rd.StartedOn) + " – " + ShowDay(rd.FinishedOn)
	case rd.StartedOn != "":
		return "From " + ShowDay(rd.StartedOn)
	case rd.FinishedOn != "":
		return "Until " + ShowDay(rd.FinishedOn)
	}
	return "No dates"
}

// progressView is the progress box: an input in the reading's unit, a
// bar and a note. Error and a typed Value come from a refused update.
type progressView struct {
```

- [ ] **Step 5: The reading routes**

In `internal/apps/books/actions.go`, replace:

```go
	return a.store.SetReview(r.Context(), userID, id, r.PostFormValue("review"))
}

func (a *App) setTags(r *http.Request, userID, id int64) error {
	return a.store.SetTags(r.Context(), userID, id, ParseTags(r.PostFormValue("tags")))
}
```

with:

```go
	return a.store.SetReview(r.Context(), userID, id, r.PostFormValue("review"))
}

// readingID is the {rid} path segment. Anything but a positive integer is
// ErrNotFound, so act answers 404, as for a reading that isn't there.
func readingID(r *http.Request) (int64, error) {
	rid, err := strconv.ParseInt(r.PathValue("rid"), 10, 64)
	if err != nil || rid <= 0 {
		return 0, ErrNotFound
	}
	return rid, nil
}

func (a *App) editReading(r *http.Request, userID, id int64) error {
	rid, err := readingID(r)
	if err != nil {
		return err
	}
	return a.store.UpdateReading(r.Context(), userID, id, rid, ReadingEdit{
		StartedOn:  strings.TrimSpace(r.PostFormValue("started_on")),
		FinishedOn: strings.TrimSpace(r.PostFormValue("finished_on")),
		Format:     r.PostFormValue("format"),
	})
}

func (a *App) deleteReading(r *http.Request, userID, id int64) error {
	rid, err := readingID(r)
	if err != nil {
		return err
	}
	return a.store.DeleteReading(r.Context(), userID, id, rid)
}

func (a *App) setTags(r *http.Request, userID, id int64) error {
	return a.store.SetTags(r.Context(), userID, id, ParseTags(r.PostFormValue("tags")))
}
```

In `internal/apps/books/books.go`, replace:

```go
	r.HandleFunc("POST /format/{id}", a.act(a.setFormat, false))
	r.HandleFunc("POST /rating/{id}", a.act(a.setRating, false))
	r.HandleFunc("POST /review/{id}", a.act(a.setReview, false))
	r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("GET /cover/{id}", a.cover)
```

with:

```go
	r.HandleFunc("POST /format/{id}", a.act(a.setFormat, false))
	r.HandleFunc("POST /rating/{id}", a.act(a.setRating, false))
	r.HandleFunc("POST /review/{id}", a.act(a.setReview, false))
	r.HandleFunc("POST /readings/{id}/{rid}", a.act(a.editReading, false))
	r.HandleFunc("POST /readings/{id}/{rid}/delete", a.act(a.deleteReading, false))
	r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("GET /cover/{id}", a.cover)
```

- [ ] **Step 6: The history in the book pane**

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
	{{template "review" .}}
	{{with .Description}}<div class="books-description">{{.}}</div>{{end}}
	{{template "tags-form" .}}
{{else}}
	<p class="empty">Pick a book, or add one.</p>
{{end}}
```

with:

```html
	{{template "review" .}}
	{{with .Description}}<div class="books-description">{{.}}</div>{{end}}
	{{template "tags-form" .}}
	{{template "history" .}}
{{else}}
	<p class="empty">Pick a book, or add one.</p>
{{end}}
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
</form>
{{end}}

{{/* confirm is the one dialog every hx-confirm in the panes goes through
     (books.js), instead of window.confirm. */}}
{{define "confirm"}}
```

with:

```html
</form>
{{end}}

{{/* history is every reading of the book, newest first (spec "Book
     pane"), each with a no-JS <details> to change its dates and format,
     and Delete (decided 2026-10-09: a reading's status isn't editable;
     deleting it is how a wrong one is put right). Takes a bookView. */}}
{{define "history"}}
{{with .History}}
<section class="books-history" aria-labelledby="books-history-head">
	<h2 class="books-section-head" id="books-history-head">Reading history</h2>
	<ul class="books-history-list">
		{{range .}}
		<li class="books-history-item">
			<p class="books-history-line"><span class="books-pill">{{.Status}}</span> {{.Dates}}{{with .Format}} · {{.}}{{end}}</p>
			<div class="books-history-actions">
				<details class="books-reading-edit">
					<summary class="button quiet">Edit</summary>
					<form class="books-reading-edit-form" method="post" action="/books/readings/{{$.ID}}/{{.ID}}"
					      hx-post="/books/readings/{{$.ID}}/{{.ID}}" hx-target="#books-panes" hx-swap="outerHTML">
						{{template "post-ctx" $}}
						<label for="books-r{{.ID}}-started">Started on</label>
						<input id="books-r{{.ID}}-started" name="started_on" type="date" value="{{.StartedOn}}" max="{{$.Today}}">
						{{if not .InProgress}}
						<label for="books-r{{.ID}}-finished">{{.FinishLabel}}</label>
						<input id="books-r{{.ID}}-finished" name="finished_on" type="date" value="{{.FinishedOn}}" max="{{$.Today}}">
						{{end}}
						<label for="books-r{{.ID}}-format">Format</label>
						<select id="books-r{{.ID}}-format" name="format">
							{{range .Formats}}<option value="{{.Value}}"{{if .Current}} selected{{end}}>{{.Label}}</option>{{end}}
						</select>
						<button type="submit" class="primary">Save</button>
					</form>
				</details>
				<form method="post" action="/books/readings/{{$.ID}}/{{.ID}}/delete">
					{{template "post-ctx" $}}
					<button type="submit" class="quiet"
					        hx-post="/books/readings/{{$.ID}}/{{.ID}}/delete" hx-target="#books-panes" hx-swap="outerHTML"
					        hx-confirm="Delete this reading? Its progress goes with it.">Delete</button>
				</form>
			</div>
		</li>
		{{end}}
	</ul>
</section>
{{end}}
{{end}}

{{/* confirm is the one dialog every hx-confirm in the panes goes through
     (books.js), instead of window.confirm. */}}
{{define "confirm"}}
```

- [ ] **Step 7: Styles**

In `internal/ui/static/app.css`, replace:

```css
.books-review-form textarea { width: 100%; }
.books-review-form button { align-self: flex-start; }
.books-hint { margin: 0; color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-dialog { width: min(28rem, calc(100vw - 2rem)); padding: 1rem; border: var(--border); border-radius: var(--radius); color: var(--c-text); background: var(--c-bg); }
.books-dialog::backdrop { background: rgba(0, 0, 0, 0.35); }

```

with:

```css
.books-review-form textarea { width: 100%; }
.books-review-form button { align-self: flex-start; }
.books-hint { margin: 0; color: var(--c-text-dim); font-size: var(--fs-sm); }

/* Reading history */
.books-history { max-width: 38rem; margin-top: var(--s-4); }
.books-section-head { margin: 0 0 var(--s-2); font-size: var(--fs-base); }
.books-history-list { list-style: none; margin: 0; padding: 0; }
.books-history-item { display: flex; flex-wrap: wrap; align-items: flex-start; gap: var(--s-2); padding: var(--s-2) 0; border-bottom: 1px solid var(--c-border); }
.books-history-line { flex: 1 1 14rem; margin: 0; }
.books-history-actions { display: flex; flex-wrap: wrap; align-items: flex-start; gap: var(--s-2); }
.books-reading-edit > summary { display: inline-flex; list-style: none; cursor: pointer; }
.books-reading-edit > summary::-webkit-details-marker { display: none; }
.books-reading-edit-form { display: flex; flex-wrap: wrap; align-items: flex-end; gap: var(--s-2); margin-top: var(--s-2); }
.books-reading-edit-form label { margin: 0; flex-basis: 100%; }
.books-dialog { width: min(28rem, calc(100vw - 2rem)); padding: 1rem; border: var(--border); border-radius: var(--radius); color: var(--c-text); background: var(--c-bg); }
.books-dialog::backdrop { background: rgba(0, 0, 0, 0.35); }

```

- [ ] **Step 8: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
```
Expected: all pass.

- [ ] **Step 9: Commit**

```bash
git add internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): reading history in the book pane, each reading editable and deletable (#488)"
```

---

### Task 9: Demo data for the screenshots

The screenshot seed (`docs/screenshots/seed`) builds the demo account the browser check and B5's screenshots use. Give its books progress, formats, ratings and a review, and a second Expanse book so the series link says "2 books".

**Files:**
- Modify: `docs/screenshots/seed/books.go`, `docs/screenshots/seed/seed_test.go`

**Interfaces:**
- Consumes: `StartReading`, `SetFormat`, `RecordProgress`, `FinishReading`, `MarkDNF`, `SetReview`, `List`, `ListItem.Progress`, `ListItem.Rating`.
- Produces: nothing for later tasks.

- [ ] **Step 1: Write the failing test**

In `docs/screenshots/seed/seed_test.go`, replace:

```go
		shelves[books.ShelfRead] < 2 || shelves[books.ShelfDNF] < 1 {
		t.Errorf("books shelves = %v, %v; want reading/want/read >= 2 and dnf >= 1", shelves, err)
	}

	// Flash: decks with cards, a review history and a streak.
	fs := flash.NewStore(handle)
```

with:

```go
		shelves[books.ShelfRead] < 2 || shelves[books.ShelfDNF] < 1 {
		t.Errorf("books shelves = %v, %v; want reading/want/read >= 2 and dnf >= 1", shelves, err)
	}
	// ... with progress on every book being read and stars on the read ones.
	onTheGo, err := bst.List(ctx, demo.ID, books.ListQuery{Shelf: books.ShelfReading})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range onTheGo {
		if !it.Progress.Set() {
			t.Errorf("reading %q has no progress", it.Title)
		}
	}
	read, err := bst.List(ctx, demo.ID, books.ListQuery{Shelf: books.ShelfRead})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range read {
		if it.Rating == 0 {
			t.Errorf("read %q has no rating", it.Title)
		}
	}

	// Flash: decks with cards, a review history and a streak.
	fs := flash.NewStore(handle)
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./docs/screenshots/seed/ -count=1`
Expected: FAIL — `reading "Leviathan Wakes" has no progress`, `read "A Wizard of Earthsea" has no rating`.

- [ ] **Step 3: Seed the reading data**

Replace the whole of `docs/screenshots/seed/books.go` with:

```go
package main

import (
	"context"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// seedBooks gives the demo account a small library across every shelf:
// two books on the go (one on paper, one an audiobook, each with a few
// days of progress), three waiting, two finished and rated (one with a
// review) and one put down part-way, with a few tags and two books of a
// series, so each shelf and the book pane have something to show. Dates
// are offsets from now, so nothing is ever in the future.
func seedBooks(ctx context.Context, st *books.Store, userID int64, now time.Time) error {
	at := func(daysAgo int) time.Time { return now.AddDate(0, 0, -daysAgo) }
	day := func(daysAgo int) string { return at(daysAgo).Local().Format("2006-01-02") }
	type seed struct {
		in       books.BookInput
		tags     []string
		started  int    // days ago; -1 = never
		format   string // of the reading
		progress []int  // one update a day, ending yesterday: pages, or percent for audio
		finished int    // days ago; -1 = not finished
		dnf      bool
		stopped  int // where a DNF stopped; 0 = not said
		rating   int
		review   string
	}
	library := []seed{
		{in: books.BookInput{Title: "Leviathan Wakes", Authors: "James S. A. Corey", Year: 2011, Pages: 592,
			SeriesName: "The Expanse", SeriesNumber: "1",
			Description: "A detective and a ship's officer find the same missing woman at the edge of the solar system."},
			tags: []string{"sf", "space"}, started: 9, format: "paper", progress: []int{48, 120, 205, 260, 344}, finished: -1},
		{in: books.BookInput{Title: "Piranesi", Authors: "Susanna Clarke", Year: 2020, Pages: 272},
			tags: []string{"fantasy"}, started: 3, format: "audio", progress: []int{18, 41}, finished: -1},
		{in: books.BookInput{Title: "The Dispossessed", Subtitle: "An Ambiguous Utopia", Authors: "Ursula K. Le Guin",
			Year: 1974, Pages: 387}, tags: []string{"sf", "classics"}, started: -1, finished: -1},
		{in: books.BookInput{Title: "Project Hail Mary", Authors: "Andy Weir", Year: 2021, Pages: 476},
			tags: []string{"sf", "space"}, started: -1, finished: -1},
		{in: books.BookInput{Title: "Caliban's War", Authors: "James S. A. Corey", Year: 2012, Pages: 595,
			SeriesName: "The Expanse", SeriesNumber: "2"}, tags: []string{"sf", "space"}, started: -1, finished: -1},
		{in: books.BookInput{Title: "A Wizard of Earthsea", Authors: "Ursula K. Le Guin", Year: 1968, Pages: 183,
			SeriesName: "Earthsea", SeriesNumber: "1"}, tags: []string{"fantasy", "classics"},
			started: 70, format: "ebook", finished: 56, rating: 5,
			review: "Short, strange and **wise**. Ged learns that the shadow he runs from is his own.\n\nBetter on a second reading."},
		{in: books.BookInput{Title: "The Remains of the Day", Authors: "Kazuo Ishiguro", Year: 1989, Pages: 258},
			started: 35, format: "paper", finished: 20, rating: 4},
		{in: books.BookInput{Title: "Infinite Jest", Authors: "David Foster Wallace", Year: 1996, Pages: 1079},
			started: 120, format: "paper", finished: 90, dnf: true, stopped: 312},
	}
	for i, b := range library {
		// Added in this order, a day apart, before anything was started.
		st.SetClock(func() time.Time { return at(150 - i) })
		id, err := st.Create(ctx, userID, books.NewBook{BookInput: b.in, Shelf: books.ShelfWant, Tags: b.tags})
		if err != nil {
			return err
		}
		if b.started < 0 {
			continue
		}
		st.SetClock(func() time.Time { return at(b.started) })
		if err := st.StartReading(ctx, userID, id); err != nil {
			return err
		}
		if err := st.SetFormat(ctx, userID, id, b.format); err != nil {
			return err
		}
		for j, value := range b.progress {
			st.SetClock(func() time.Time { return at(len(b.progress) - j) })
			if err := st.RecordProgress(ctx, userID, id, value); err != nil {
				return err
			}
		}
		if b.finished < 0 {
			continue
		}
		st.SetClock(func() time.Time { return at(b.finished) })
		if b.dnf {
			err = st.MarkDNF(ctx, userID, id, day(b.finished), b.stopped)
		} else {
			err = st.FinishReading(ctx, userID, id, day(b.finished), b.rating)
		}
		if err != nil {
			return err
		}
		if b.review != "" {
			if err := st.SetReview(ctx, userID, id, b.review); err != nil {
				return err
			}
		}
	}
	st.SetClock(func() time.Time { return now })
	return nil
}
```

- [ ] **Step 4: Run the test, vet and staticcheck**

```bash
go test ./docs/... -count=1
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add docs/screenshots/seed
git commit -m "docs(books): seed progress, formats, ratings and a review for the demo (#488)"
```

---

### Task 10: User guide, spec and app list

**Files:**
- Modify: `docs/user/books.md`, `docs/superpowers/specs/2026-10-09-on-books-design.md`, `AGENTS.md`

- [ ] **Step 1: Update the guide**

In `docs/user/books.md`, replace:

````markdown

- **Start reading** starts it today and moves it to Reading.
- **Finish** asks for the day you finished — today, unless you change it —
  and moves the book to Read.
- **Did not finish** works the same way and moves it to Did not finish.

A finished book offers **Read again**, and a book you put down offers
**Start again**: either starts a new reading, and the earlier one is kept.
You read a book one time at a time: while you're reading it there's no start
button; finish it or put it down first.

## Tags

````

with:

````markdown

- **Start reading** starts it today and moves it to Reading.
- **Finish** asks for the day you finished — today, unless you change it —
  and, if you like, a rating; then it moves the book to Read.
- **Did not finish** asks for the day you stopped and, if you like, the
  page you got to, and moves the book to Did not finish.

A finished book offers **Read again**, and a book you put down offers
**Start again**: either starts a new reading, and the earlier one is kept.
You read a book one time at a time: while you're reading it there's no start
button; finish it or put it down first.

### Progress

While you're reading a book, type the page you're on into the **Page** box
and press Enter. The bar under it, and the book's row in the list, show how
far through you are. Books on the Reading shelf are listed with the one you
read most recently at the top.

For an audiobook — or a book with no page count — the box asks for a
percentage instead. Reaching the last page doesn't finish the book: click
**Finish** when you're done.

ON Books keeps every update you make. You only see the latest, but they're
what the reading stats will be counted from.

### Format

The pill next to "Reading since…" says how you're reading the book: on
paper, as an ebook, or as an audiobook. Click it to change it. A new
reading starts with the format of the one before.

## Rating and review

Click a star under the reading box to rate a book from one to five; click
the same star again to clear it. You can also rate a book as you finish it.
The Read shelf shows each book's stars and the day you finished it.

**Write a review** opens a box for what you thought. You can use a little
Markdown: `**bold**`, `*italic*`, `[a link](https://example.com)`, and a
blank line to start a new paragraph. **Save review** keeps it; **Edit
review** changes it later. A book has one rating and one review, however
many times you read it.

## Reading history

At the bottom of a book is every time you've read it, newest first: when
you started and finished, the format, and how it ended. **Edit** changes a
reading's dates and format. To fix a reading that ended the wrong way —
marked as read when you gave up, say — **Delete** it; the book's shelf
follows whichever reading is left.

## Series

If a book is part of a series, its series and number show under the
author — "The Expanse #1 · 2 books". Click it to list the series in
order. Add a series name and number with **⋯ → Edit details**.

## Tags

````

In `docs/user/books.md`, replace:

````markdown
- **j** / **k** — open the next / previous book in the list
- **/** — jump to the filter box
- **a** — add a book
- **Esc** — close the Finish or Did not finish box, or leave the filter

## On a phone

````

with:

````markdown
- **j** / **k** — open the next / previous book in the list
- **/** — jump to the filter box
- **a** — add a book
- **p** — jump to the progress box of the book you're reading
- **Esc** — close an open box in the book (Finish, Did not finish, a
  menu, the review), or leave the filter or the progress box

## On a phone

````

- [ ] **Step 2: Record the decisions in the spec**

In `docs/superpowers/specs/2026-10-09-on-books-design.md`, replace:

````markdown
    active reading it shows **Start reading** instead (or **Read again**
    once it has been read).
  - Rating stars (click to set, click again to clear) and the review
    (Markdown, edit in place).
  - Notes — dated entries, newest first, with an add box.
  - Quotes — cards with text, page and comment.
  - Reading history — every reading with dates, format and status; each
    editable and deletable.
  - A ⋯ menu: Edit details (which also changes the cover), Find cover (B5), Delete.

### Add book
````

with:

````markdown
    active reading it shows **Start reading** instead (or **Read again**
    once it has been read).
  - Rating stars (click to set, click again to clear) and the review
    (Markdown, edited in a "Write a review" / "Edit review" disclosure).
  - Notes — dated entries, newest first, with an add box.
  - Quotes — cards with text, page and comment.
  - Reading history — every reading with dates, format and status; each
    one's dates and format editable, and each deletable.
  - A ⋯ menu: Edit details (which also changes the cover), Find cover (B5), Delete.

### Add book
````

In `docs/superpowers/specs/2026-10-09-on-books-design.md`, replace:

````markdown
  **Did not finish** is the same with an optional page instead of a rating.
- **Start reading** creates a reading dated today with the format of the
  book's previous reading, if any.

### Keyboard

````

with:

````markdown
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

### Keyboard

````

- [ ] **Step 3: Update the app list**

In `AGENTS.md`, replace:

````markdown
Reader** (a feed reader, with search, OPML import/export, full-article
extraction, and reading-stats), **ON Later** (a read-it-later app: save
pages, read them in a calm view, highlight, comment and tag them, export as
Markdown), **ON Books** (a private reading log: shelves, readings and tags,
with Open Library search, progress, notes and stats to come),
**ON Flash** (flash cards with FSRS review, import from
AI-written Markdown/JSON, media, household sharing and stats), and **ON
Focus** (saved, reusable focus timers — single blocks or Pomodoro-style
````

with:

````markdown
Reader** (a feed reader, with search, OPML import/export, full-article
extraction, and reading-stats), **ON Later** (a read-it-later app: save
pages, read them in a calm view, highlight, comment and tag them, export as
Markdown), **ON Books** (a private reading log: shelves, readings, tags,
Open Library search, progress, ratings and reviews, with notes and stats
to come),
**ON Flash** (flash cards with FSRS review, import from
AI-written Markdown/JSON, media, household sharing and stats), and **ON
Focus** (saved, reusable focus timers — single blocks or Pomodoro-style
````

- [ ] **Step 4: Run the full check and commit**

Run the full check from Global Constraints (the help tests load the guide).

```bash
git add docs AGENTS.md
git commit -m "docs(books): progress, ratings, reviews and reading history in the guide and spec (#488)"
```

---

### Task 11: Check it in a browser and open the PR

- [ ] **Step 1: Seed a demo directory and start the server**

```bash
SEED=$(mktemp -d)/books-b2
go run ./docs/screenshots/seed --data-dir $SEED
go build -o $SEED/onsuite ./cmd/onsuite
echo $SEED
```
Add a `.claude/launch.json` configuration named `onsuite-books` (in the main checkout, `/Users/iliaf/src/WEB/on-suite`) with `"runtimeExecutable": "<SEED>/onsuite"`, `"runtimeArgs": ["serve", "--addr", ":8096", "--data-dir", "<SEED>"]` and `"port": 8096`, start it with the preview tools and sign in as `demo` (password in `docs/screenshots/README.md`).

- [ ] **Step 2: Walk through it**

1. Reading shelf: both rows show a bar and a percentage; Leviathan Wakes shows "Paper", Piranesi "Audiobook" in the reading box.
2. Open Leviathan Wakes, press `p`, type a page, Enter: the box and the row update without a page load, the row moves to the top, focus stays in the input, no console errors. Type 9999 and Enter: "Enter a page from 0 to 592." inside the box, what was typed kept.
3. Click the format pill → Audiobook: the box asks for a percentage, converted from the page.
4. Click the 4th star, then the 4th star again: set, then cleared.
5. Click "The Expanse #1 · 2 books": the list shows the series in order, no shelf highlighted; open Caliban's War and back.
6. Read shelf: stars and finish dates. A Wizard of Earthsea: the review renders (bold, two paragraphs); Edit review → change → Save review.
7. Reading history: Edit a reading's dates and format → Save; a finish before the start shows the banner; Delete a reading (confirm dialog) → the shelf follows.
8. Finish a book with a rating; Did not finish another at a page: the rating and the DNF page are kept.
9. Turn JavaScript off (or post a form by hand) for progress, a star and the review: each comes back to the book.
10. Esc closes the format menu, the Finish/DNF boxes, the review editor and a reading's Edit box.
11. Phone width: the reading box, rating, review and history don't overflow.

Fix anything found (with a test where one can be written), re-run the full check, commit.

- [ ] **Step 3: Remove the launch entry and push**

Remove the `onsuite-books` entry from `.claude/launch.json` (do not commit it), stop the server, then:

```bash
git push -u origin feat/books-b2-progress
env -u GH_TOKEN gh pr create --title "feat(books): ON Books B2 — reading progress and history (#488)" --body "$(cat <<'EOF'
B2 of ON Books. Record progress by page (or percent for audiobooks and books without a page count) and see it as a bar in the book pane and the Reading list; set each reading's format; rate a book with stars (or as you finish it) and write a Markdown review; note where a DNF stopped; see every reading of a book and edit its dates and format or delete it; follow a book's series link to the rest of the series. Every progress update is kept for B4's stats; the UI shows only the latest.

Decisions from 2026-10-09 are recorded in the spec: one PR, bar-only progress UI, readings editable by dates and format only, review edited in a disclosure and rendered by a mirror of ON Notes' inline Markdown.

Spec: docs/superpowers/specs/2026-10-09-on-books-design.md
Plan: docs/superpowers/plans/2026-10-09-on-books-b2-progress-history.md
Closes #488.
EOF
)"
```
Never merge it.
