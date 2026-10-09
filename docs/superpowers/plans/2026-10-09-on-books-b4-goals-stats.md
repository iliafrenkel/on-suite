# ON Books B4 — Goals and stats Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give ON Books a yearly reading goal and a Stats page: books finished, pages read, average rating, format split, longest and shortest book for a chosen year, an all-time row, and two server-rendered bar charts — with the year's goal ("12 of 30 · 2 ahead") at the top of the Reading shelf.

**Architecture:** One new table, `books_goals` (migration 0006). The store counts: `Goal` is a target plus the readings finished that year (one SQL query); `Stats` loads every finished reading and every progress row once and counts in Go — `PagesRead` is a pure function of one reading's progress rows, so the spec's pages-read rules are table-tested on their own. A new page, `GET /books/stats?year=`, draws the numbers with ON Focus's chart technique (positioned `<rect>`s, a figures table as the text alternative), and plain form posts set, change and remove the goal (`POST /books/goal`, `POST /books/goal/clear`), so the page works without JavaScript. The Reading shelf's list gets a read-only goal card linking to the page; it lives inside `#books-list`, so it follows every list swap.

**Tech Stack:** Go 1.22+ `ServeMux`, `html/template`, htmx 2 (only for the existing panes), SQLite via `modernc.org/sqlite`, inline SVG.

**Spec:** [docs/superpowers/specs/2026-10-09-on-books-design.md](../specs/2026-10-09-on-books-design.md) — "Data model" (`books_goals`), "Derived values" (pages read, books finished), "Stats (B4)", "Testing", "Phases" (B4 row). Builds on B3 ([plan](2026-10-09-on-books-b3-notes-quotes.md), #581). Issue #490.

## Global Constraints

Decisions Ilia made on 2026-10-09 (binding; recorded in the spec's "Stats (B4)" by this plan's own PR):

- **One B4 PR.** The tasks below are separate commits on branch `feat/books-b4-goals-stats` in the worktree `../on-suite-books-b4`. Eight code-and-docs tasks, smaller than B2's twelve.
- **Stats is a separate page**, `/books/stats`, as `/reader/stats` is: a link back to the books, and a **Stats** link at the bottom of the Books sidebar. It must work at phone width.
- **The goal card on the Reading shelf is read-only** — "12 of 30 · 2 ahead" — and links to Stats. It shows only when the current year has a goal. The goal is set, changed and removed only on the Stats page, and that works without JavaScript.
- **#576 is out of scope.** Pages read are the spec's sum of positive deltas, so a repeated progress row adds 0; `TestPagesRead` pins that ("a repeated value adds nothing (#576)"). `closeReading` is not changed.

Defaults chosen while planning — **ask Ilia to confirm** (each is a small change if he wants another):

- **Goal range:** a whole number from 1 to `MaxGoal = 1000` books. Anything else — 0, 1001, "abc", empty — is refused inline: "Enter a goal from 1 to 1000 books." (422 without JavaScript, the box keeps what was typed).
- **Year picker:** a row of year links (no `<select>`, so no JavaScript and one click), from the earliest year with a dated finish or a progress update — this year when there is none — to **next year**, so next year's goal can be set in December. `?year=` accepts `MinYear = 1900` to next year (a year before the first one still shows, and the picker reaches back to it); anything else is a 404. A goal form posting a year outside that range is a 400.
- **Pace wording** (lowercase, after a "·"): this year — "2 ahead", "1 behind", "on track" against `Expected` (target × day-of-year ÷ days-in-year, rounded down, day-of-year counted from 1 on 1 January, so 31 December expects the whole target); any year once the target is met — "goal reached"; a past year short of it — "4 short"; a future year — nothing ("0 of 10").
- **Pages-read reading of the spec:** a reading's first progress row counts from page 0 (logging page 50 on a new reading is 50 pages), and deltas are between *consecutive* rows, as the spec says — not above a high-water mark — so after going back from 200 to 150, reading on to 180 counts 30. A DNF reading's pages count; only a finish adds the remainder.
- **All-time row:** books finished counts every finished reading, **including undated** (imported) ones, which count toward no year; pages read and average rating (over every rated book ever finished). Format split and longest/shortest are per year only.
- **Average rating:** the book's own rating (ratings are per book), averaged over the *distinct* rated books finished in the year — a book read twice counts once — shown to one decimal place with a star ("4.3 ★"), "—" when none is rated.
- **Format split:** finished readings that year by format, in the order Paper, Ebook, Audiobook, Not set; formats with none are left out; nothing at all is shown with no finishes.
- **Longest / shortest:** among the year's finished books with a page count; a tie goes to the book finished first. Shortest is left out when it is the same book as longest. Each links to the book.
- **Empty states:** charts with nothing to draw say "No books finished in 2026." and "No dated finishes yet."; tiles show 0 and "—".
- **Charts:** at least twelve bar slots, so one year of history isn't a page-wide bar; year ticks are "’25" (four digits don't fit a slot at phone width); a figures table under each chart as the text alternative (Reader's #242 lesson).
- **Page title** "Reading stats", as Reader's; the back link is "← Books".

From the spec:

- `books_goals` — `user_id`, `year`, `target`; PK `(user_id, year)`. Per user, as every table.
- **Books finished in a year** — readings with status `finished` and `finished_on` in that year. A re-read counts again; DNF never counts; undated readings don't count toward any year.
- **Pages read** — the sum of positive deltas between consecutive progress rows of a reading, dated by `recorded_at` in the user's local day; finishing a reading with a page count adds the remainder on the finish date; going backwards counts nothing; percent converts through the page count, and counts nothing without one. Re-reads are separate readings and count again.
- Expected = target × day-of-year ÷ days-in-year, rounded down.
- Charts are server-rendered SVG as in ON Focus (`internal/apps/focus/handlers_history.go`'s `buildChart`); `chartBar`/`buildChart` here say they mirror it. Apps never import each other (`internal/arch` enforces it).
- Calendar days are local (server TZ, `time.Local`), suite-wide. Code reads time only through the store's `now()`.
- CSP: no inline `<script>`, no `style=""`. App CSS goes at the end of the "ON Books" section of `internal/ui/static/app.css` (the last section of the file), classes prefixed `books-`.
- **B5 owns export and the admin card.** Goals join the `onsuite export` JSON in B5; B4 adds nothing to `Exporter`/`Stater` (Books implements neither yet). Screenshots are B5's job too.
- Full check must stay green on every commit:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- Open the PR with `env -u GH_TOKEN gh …`; never merge.

## Open issues in the ON Books milestone that touch B4

- **#576** (Did not finish with the prefilled page adds a duplicate progress row). Out of scope by Ilia's decision: the pages-read maths already ignores it (a repeated value adds 0), and `TestPagesRead` says so. Leave #576 open for the "days read" kind of figure it warns about; B4 has none.
- **#578** (saving in the book pane scrolls it to the top) — untouched: B4 adds no book-pane controls.

## Lessons from earlier plans (read before starting)

- staticcheck U1000 fails on an unexported helper or struct field added before its first use. Every helper here arrives in the task that first uses it: `goalView`/`viewGoal` in Task 5 (not Task 4), `listView.Goal` and `shelfGoal` in Task 6. Exported names (`PagesRead`, `Expected`) are exempt but are introduced with their tests anyway.
- `internal/htmlassert` supports one qualifier per selector and no attribute value with spaces, no `^=`, no pseudo-classes: `#books-year rect` works; `svg[aria-label="Books finished…"]`, `a[href^="/books/b/"]` and `figure:first-of-type` do not (the trial hit all three). Check a second condition with `htmlassert.Attr`. Plain `svg` also matches the icons in the shell — scope it (`.books-chart svg`).
- `go vet` rejects unkeyed composite literals of another package's struct types in `_test` packages — every `books.Goal{…}`, `books.FormatCount{…}`, `books.YearCount{…}` here is keyed.
- `TestMain` pins `time.Local` to Melbourne. The fixture's default clock (15:00 UTC on 9 October 2026) is already 10 October there. Day-keyed tests here use **local noon** (`noon("2026-03-01")`, Task 1's helper), never a time near midnight — the #521 lesson — except `TestStatsCountPagesOnTheLocalDay`, which pins an exact instant on purpose to prove the local day is used.
- Test helper names must not collide with the package's existing ones: `readBook` and `texts` already exist (`notes_view_test.go`), so the stats tests use `finishedOn` and reuse `texts`.
- The store's `checkDay` refuses a finish date after "today", so a test that finishes a reading on a date moves the fixture clock (`f.now`) to that date first.
- Templates defined in a page file (`stats.html`'s `goal`, `stat-tiles`, `stats-chart`) are that page's own; `panes.partial.html` is shared with every Books page, so nothing in it may take those names.
- gofmt re-aligns a struct when a longer field or comment joins it: where that happens the plan shows the whole struct (`listView`).
- This plan's code was trial-run on 2026-10-09: written and tested task by task in a scratch branch, then a script applied every "Create", "replace … with" and "Append" block of this document, in order, to a fresh branch from `main`, ran each task's Step 2 (recording the failure written there) and its tests, vet and staticcheck at each commit, and the full check at the end; the result matched the scratch tree. A browser walk through the seeded demo at desktop and phone width (Task 9's list) is what made the charts keep at least twelve slots, the year ticks short ("’26"), and the goal card's bar a full-width line.

## File map

| File | Responsibility |
|---|---|
| `internal/apps/books/migrations/0006_goals.sql` | `books_goals` |
| `internal/apps/books/goal.go` | `MaxGoal`, `MinYear`, `Goal`, `Expected`, `Goal.Pace`, `yearSpan`, `Store.Goal`, `SetGoal`, `ClearGoal` |
| `internal/apps/books/pages.go` | `Step`, `ReadingLog`, `PagesRead` (spec "Derived values") |
| `internal/apps/books/stats.go` | `BookPages`, `FormatCount`, `YearCount`, `YearStats`, `AllTime`, `Stats`, `Store.Stats`, `finishes`, `readingLogs`, `summarise`, `mean` |
| `internal/apps/books/statspage.go` | `parseYear`, `statsURL`, the `stats`/`setGoal`/`clearGoal` handlers, `goalView`/`viewGoal`, `statsView`/`viewStats`, tiles, `buildChart` (mirrors Focus's) |
| `internal/apps/books/templates/stats.html` | the Stats page: year links, `goal`, `goal-form`, `stat-tiles`, `stats-chart` |
| `internal/apps/books/books.go` | routes |
| `internal/apps/books/view.go`, `handlers.go` | `listView.Goal`; `shelfGoal` in `renderPanes` |
| `internal/apps/books/templates/panes.partial.html` | sidebar Stats link; the goal card at the top of the list |
| `internal/ui/static/app.css` | Stats page, goal box, goal card |
| `internal/apps/books/*_test.go` | `goal_test.go`, `pages_test.go`, `stats_test.go`, `stats_view_test.go`, `goal_view_test.go`, `goal_card_test.go` |
| `docs/screenshots/seed/books.go`, `seed_test.go` | a goal for the demo |
| `docs/user/books.md`, `AGENTS.md` | guide, app list |

---

### Task 0: Worktree and branch

- [ ] **Step 1: Create the worktree**

```bash
cd /Users/iliaf/src/WEB/on-suite
git fetch origin
git worktree add ../on-suite-books-b4 -b feat/books-b4-goals-stats origin/main
cd ../on-suite-books-b4
go build ./cmd/onsuite && rm -f onsuite
```
Expected: builds with no output. All later commands run in `../on-suite-books-b4`. `origin/main` must already include this plan's PR (it carries the spec's B4 decisions that Task 8's guide text matches).

---

### Task 1: Yearly goal in the store

**Files:**
- Create: `internal/apps/books/migrations/0006_goals.sql`, `internal/apps/books/goal.go`
- Test: `internal/apps/books/goal_test.go`

**Interfaces:**
- Consumes: `Refusal`, `ErrInvalid`, `Store.db`, `StartReading`, `FinishReading`, `MarkDNF`, `Create` (existing); test helpers `newFixture`, `addBook`, `onShelf`, `wantRefusal` (store_test.go, library_test.go, notes_test.go).
- Produces:
  - `const MaxGoal = 1000`, `const MinYear = 1900`
  - `type Goal struct { Year, Target, Done int }` — `Target` 0: no goal
  - `func Expected(target int, day time.Time) int` — `day` is a local date
  - `func (g Goal) Pace(today time.Time) string` — "2 ahead", "1 behind", "on track", "goal reached", "4 short", ""
  - `func yearSpan(year int) (string, string)` — "2026-01-01", "2026-12-31"
  - `(*Store).Goal(ctx, userID int64, year int) (Goal, error)`, `SetGoal(ctx, userID int64, year, target int) error` (a `*Refusal` for a bad target, `ErrInvalid` for a year outside `MinYear`–9999), `ClearGoal(ctx, userID int64, year int) error`
  - test helpers: `noon(day string) time.Time` (local noon), `finished(t, f, userID, title, pages, day) int64` (a book already read, finished on day), `readAgain(t, f, userID, id, from, to)`, `goal(t, f, userID, year) books.Goal`

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/goal_test.go`:

```go
package books_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// noon is local noon on day (YYYY-MM-DD). Tests keyed to a local day use
// noon, never a time near midnight, so they hold in any zone (#521).
func noon(day string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		panic(err)
	}
	return t.Add(12 * time.Hour)
}

// finished adds one of userID's books as already read, finished on day.
func finished(t *testing.T, f *fixture, userID int64, title string, pages int, day string) int64 {
	t.Helper()
	nb := onShelf(title, books.ShelfRead)
	nb.Pages, nb.FinishedOn = pages, day
	return addBook(t, f, userID, nb)
}

// readAgain reads book id again: started at noon on from, finished on to.
func readAgain(t *testing.T, f *fixture, userID, id int64, from, to string) {
	t.Helper()
	ctx := context.Background()
	f.now = noon(from)
	if err := f.store.StartReading(ctx, userID, id); err != nil {
		t.Fatal(err)
	}
	f.now = noon(to)
	if err := f.store.FinishReading(ctx, userID, id, to, 0); err != nil {
		t.Fatal(err)
	}
}

func goal(t *testing.T, f *fixture, userID int64, year int) books.Goal {
	t.Helper()
	g, err := f.store.Goal(context.Background(), userID, year)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestExpectedIsTheTargetSoFarRoundedDown(t *testing.T) {
	tests := []struct {
		day    string
		target int
		want   int
	}{
		{"2026-01-01", 30, 0},    // 30 × 1 ÷ 365
		{"2026-01-13", 30, 1},    // 30 × 13 ÷ 365 = 1.07
		{"2026-01-12", 30, 0},    // 0.99
		{"2026-07-02", 365, 183}, // day 183
		{"2026-12-31", 30, 30},
		{"2024-12-31", 12, 12},   // a leap year has 366 days
		{"2024-07-01", 366, 183}, // day 183 of 366
		{"2026-10-10", 0, 0},
	}
	for _, tt := range tests {
		if got := books.Expected(tt.target, noon(tt.day)); got != tt.want {
			t.Errorf("Expected(%d, %s) = %d, want %d", tt.target, tt.day, got, tt.want)
		}
	}
}

func TestPaceSaysAheadOrBehind(t *testing.T) {
	today := noon("2026-07-02") // day 183 of 365: 30 × 183 ÷ 365 = 15 expected
	tests := []struct {
		name string
		g    books.Goal
		want string
	}{
		{"no goal", books.Goal{Year: 2026, Done: 4}, ""},
		{"ahead", books.Goal{Year: 2026, Target: 30, Done: 17}, "2 ahead"},
		{"behind", books.Goal{Year: 2026, Target: 30, Done: 14}, "1 behind"},
		{"on track", books.Goal{Year: 2026, Target: 30, Done: 15}, "on track"},
		{"reached", books.Goal{Year: 2026, Target: 30, Done: 30}, "goal reached"},
		{"past it", books.Goal{Year: 2026, Target: 30, Done: 31}, "goal reached"},
		{"a past year, short", books.Goal{Year: 2025, Target: 30, Done: 26}, "4 short"},
		{"a past year, reached", books.Goal{Year: 2025, Target: 30, Done: 30}, "goal reached"},
		{"next year", books.Goal{Year: 2027, Target: 30}, ""},
	}
	for _, tt := range tests {
		if got := tt.g.Pace(today); got != tt.want {
			t.Errorf("%s: Pace = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestGoalIsSetChangedAndCleared(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if g := goal(t, f, f.alice.ID, 2026); g != (books.Goal{Year: 2026}) {
		t.Errorf("no goal yet = %+v, want only the year", g)
	}
	if err := f.store.SetGoal(ctx, f.alice.ID, 2026, 30); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetGoal(ctx, f.alice.ID, 2026, 24); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetGoal(ctx, f.alice.ID, 2027, 40); err != nil {
		t.Fatal(err)
	}
	if g := goal(t, f, f.alice.ID, 2026); g.Target != 24 {
		t.Errorf("2026 goal = %d, want 24 after changing it", g.Target)
	}
	if g := goal(t, f, f.bob.ID, 2026); g.Target != 0 {
		t.Errorf("Bob's 2026 goal = %d, want none: goals are per user", g.Target)
	}
	if err := f.store.ClearGoal(ctx, f.alice.ID, 2026); err != nil {
		t.Fatal(err)
	}
	if err := f.store.ClearGoal(ctx, f.alice.ID, 2026); err != nil {
		t.Errorf("clearing a goal twice = %v, want nothing to happen", err)
	}
	if g := goal(t, f, f.alice.ID, 2026); g.Target != 0 {
		t.Errorf("2026 goal = %d after clearing it, want none", g.Target)
	}
	if g := goal(t, f, f.alice.ID, 2027); g.Target != 40 {
		t.Errorf("2027 goal = %d, want 40: clearing 2026 leaves it", g.Target)
	}
}

func TestSetGoalRefusesBadTargets(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, target := range []int{0, -1, books.MaxGoal + 1} {
		err := f.store.SetGoal(ctx, f.alice.ID, 2026, target)
		wantRefusal(t, fmt.Sprint("a goal of ", target), err, "Enter a goal from 1 to 1000 books.")
	}
	for _, year := range []int{books.MinYear - 1, 10000} {
		if err := f.store.SetGoal(ctx, f.alice.ID, year, 10); !errors.Is(err, books.ErrInvalid) {
			t.Errorf("a goal for %d = %v, want ErrInvalid", year, err)
		}
	}
	if err := f.store.SetGoal(ctx, f.alice.ID, 2026, books.MaxGoal); err != nil {
		t.Errorf("a goal of MaxGoal = %v, want it accepted", err)
	}
}

func TestGoalCountsTheYearsFinishedReadings(t *testing.T) {
	f := newFixture(t) // today is 10 October 2026 in Melbourne
	ctx := context.Background()
	uid := f.alice.ID
	if err := f.store.SetGoal(ctx, uid, 2026, 12); err != nil {
		t.Fatal(err)
	}
	finished(t, f, uid, "First day", 0, "2026-01-01")
	finished(t, f, uid, "Last year's last day", 0, "2025-12-31")
	dune := finished(t, f, uid, "Dune", 0, "2026-02-01")
	readAgain(t, f, uid, dune, "2026-05-01", "2026-05-20") // a re-read counts again
	f.now = noon("2026-10-10")
	dnf := addBook(t, f, uid, onShelf("Abandoned", books.ShelfReading))
	if err := f.store.MarkDNF(ctx, uid, dnf, "", 0); err != nil { // DNF never counts
		t.Fatal(err)
	}
	addBook(t, f, uid, onShelf("Still reading", books.ShelfReading))
	undated := addBook(t, f, uid, onShelf("Imported", books.ShelfWant)) // undated never counts
	if _, err := f.db.Exec(`INSERT INTO books_readings (book_id, status, created_at) VALUES (?, 'finished', ?)`,
		undated, db.FormatTime(f.now)); err != nil {
		t.Fatal(err)
	}
	finished(t, f, f.bob.ID, "Bob's book", 0, "2026-03-01")

	if g := goal(t, f, uid, 2026); g.Target != 12 || g.Done != 3 {
		t.Errorf("2026 = %+v, want 3 of 12 (1 Jan, Dune, Dune again)", g)
	}
	if g := goal(t, f, uid, 2025); g.Target != 0 || g.Done != 1 {
		t.Errorf("2025 = %+v, want 1 finished and no goal", g)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — the package doesn't build: `undefined: books.Expected`, `undefined: books.Goal`, `f.store.SetGoal undefined`, and so on.

- [ ] **Step 3: Add the table and the store methods**

Create `internal/apps/books/migrations/0006_goals.sql`:

```sql
-- A yearly reading goal (spec "Data model": books_goals): a number of
-- books, one per user per year. No row means no goal for that year.
CREATE TABLE books_goals (
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    year    INTEGER NOT NULL,
    target  INTEGER NOT NULL CHECK (target > 0),
    PRIMARY KEY (user_id, year)
) STRICT;
```

Create `internal/apps/books/goal.go`:

```go
package books

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// MaxGoal is the largest yearly goal, in books (decided 2026-10-09 while
// planning B4): room for any real reader, and a typo of 30000 is refused.
const MaxGoal = 1000

// MinYear is the earliest year the Stats page shows or takes a goal for.
const MinYear = 1900

// Goal is a year's reading goal and how far along it is (spec "Stats
// (B4)"). Target is 0 when no goal is set for the year; Done is the
// readings finished in it (spec "Derived values": a re-read counts again,
// DNF and undated readings never do).
type Goal struct {
	Year, Target, Done int
}

// Expected is how many books of target should be finished by the end of
// day, a local date (spec "Stats (B4)"): target × day-of-year ÷
// days-in-year, rounded down.
func Expected(target int, day time.Time) int {
	days := time.Date(day.Year(), time.December, 31, 12, 0, 0, 0, time.UTC).YearDay()
	return target * day.YearDay() / days
}

// Pace says how g stands on today, a local date: "goal reached" once it is
// met; for this year "2 ahead", "1 behind" or "on track" against Expected;
// for a past year "4 short"; nothing for a future year or with no goal
// (decided 2026-10-09 while planning B4).
func (g Goal) Pace(today time.Time) string {
	switch {
	case g.Target == 0:
		return ""
	case g.Done >= g.Target:
		return "goal reached"
	case g.Year < today.Year():
		return strconv.Itoa(g.Target-g.Done) + " short"
	case g.Year > today.Year():
		return ""
	}
	switch d := g.Done - Expected(g.Target, today); {
	case d > 0:
		return strconv.Itoa(d) + " ahead"
	case d < 0:
		return strconv.Itoa(-d) + " behind"
	}
	return "on track"
}

// yearSpan is a year's first and last day, YYYY-MM-DD: finished_on
// BETWEEN them is "finished in that year".
func yearSpan(year int) (string, string) {
	return fmt.Sprintf("%04d-01-01", year), fmt.Sprintf("%04d-12-31", year)
}

// Goal is userID's goal for year with the books finished in it so far.
func (st *Store) Goal(ctx context.Context, userID int64, year int) (Goal, error) {
	g := Goal{Year: year}
	from, to := yearSpan(year)
	err := st.db.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT target FROM books_goals WHERE user_id = ? AND year = ?), 0),
		       (SELECT count(*) FROM books_readings r JOIN books_books b ON b.id = r.book_id
		         WHERE b.user_id = ? AND r.status = 'finished' AND r.finished_on BETWEEN ? AND ?)`,
		userID, year, userID, from, to).Scan(&g.Target, &g.Done)
	if err != nil {
		return Goal{}, fmt.Errorf("books: goal: %w", err)
	}
	return g, nil
}

// SetGoal sets or changes userID's goal for year: target books, from 1 to
// MaxGoal (a Refusal otherwise). The year comes from a hidden field, so
// one outside MinYear–9999 is ErrInvalid.
func (st *Store) SetGoal(ctx context.Context, userID int64, year, target int) error {
	if year < MinYear || year > 9999 {
		return ErrInvalid
	}
	if target < 1 || target > MaxGoal {
		return &Refusal{Msg: fmt.Sprintf("Enter a goal from 1 to %d books.", MaxGoal)}
	}
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO books_goals (user_id, year, target) VALUES (?, ?, ?)
		ON CONFLICT (user_id, year) DO UPDATE SET target = excluded.target`,
		userID, year, target); err != nil {
		return fmt.Errorf("books: set goal: %w", err)
	}
	return nil
}

// ClearGoal removes userID's goal for year; with none set it does nothing.
func (st *Store) ClearGoal(ctx context.Context, userID int64, year int) error {
	if _, err := st.db.ExecContext(ctx,
		`DELETE FROM books_goals WHERE user_id = ? AND year = ?`, userID, year); err != nil {
		return fmt.Errorf("books: clear goal: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./internal/apps/books/
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./internal/apps/books/
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): yearly reading goal in the store (#490)"
```

---

### Task 2: Pages-read maths

A pure function, so the spec's rules are tested on their own, table by table. The store feeds it in Task 3.

**Files:**
- Create: `internal/apps/books/pages.go`
- Test: `internal/apps/books/pages_test.go`

**Interfaces:**
- Consumes: `Unit`, `UnitPage`, `UnitPercent`, `Progress.In` (progress.go); `Status`, `StatusReading`, `StatusFinished`, `StatusDNF` (library.go).
- Produces:
  - `type Step struct { Unit Unit; Value int; Day string }` — `Day` is the local YYYY-MM-DD of `recorded_at`
  - `type ReadingLog struct { Pages int; Status Status; FinishedOn string; Steps []Step }` — steps oldest first
  - `func PagesRead(r ReadingLog) map[string]int` — pages per local day; days with nothing are absent

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/pages_test.go`:

```go
package books_test

import (
	"maps"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

func page(day string, n int) books.Step {
	return books.Step{Unit: books.UnitPage, Value: n, Day: day}
}

func percent(day string, n int) books.Step {
	return books.Step{Unit: books.UnitPercent, Value: n, Day: day}
}

func TestPagesRead(t *testing.T) {
	const d1, d2, d3 = "2026-03-01", "2026-03-02", "2026-03-03"
	tests := []struct {
		name string
		r    books.ReadingLog
		want map[string]int
	}{
		{"no progress", books.ReadingLog{Pages: 300, Status: books.StatusReading}, map[string]int{}},
		{"deltas from page 0, by day",
			books.ReadingLog{Pages: 300, Status: books.StatusReading, Steps: []books.Step{page(d1, 40), page(d1, 70), page(d2, 120)}},
			map[string]int{d1: 70, d2: 50}},
		{"going backwards counts nothing; reading on from there counts",
			books.ReadingLog{Pages: 300, Status: books.StatusReading, Steps: []books.Step{page(d1, 100), page(d2, 60), page(d3, 90)}},
			map[string]int{d1: 100, d3: 30}},
		{"a repeated value adds nothing (#576)",
			books.ReadingLog{Pages: 300, Status: books.StatusReading, Steps: []books.Step{page(d1, 100), page(d2, 100)}},
			map[string]int{d1: 100}},
		{"finishing adds the remainder on the finish date",
			books.ReadingLog{Pages: 300, Status: books.StatusFinished, FinishedOn: d3, Steps: []books.Step{page(d1, 250)}},
			map[string]int{d1: 250, d3: 50}},
		{"finishing with no progress is the whole book",
			books.ReadingLog{Pages: 300, Status: books.StatusFinished, FinishedOn: d2},
			map[string]int{d2: 300}},
		{"finishing at the last page adds nothing more",
			books.ReadingLog{Pages: 300, Status: books.StatusFinished, FinishedOn: d2, Steps: []books.Step{page(d1, 300)}},
			map[string]int{d1: 300}},
		{"finishing without a page count adds nothing",
			books.ReadingLog{Status: books.StatusFinished, FinishedOn: d2},
			map[string]int{}},
		{"an undated finish adds nothing",
			books.ReadingLog{Pages: 300, Status: books.StatusFinished},
			map[string]int{}},
		{"did not finish: the pages read, no remainder",
			books.ReadingLog{Pages: 300, Status: books.StatusDNF, FinishedOn: d2, Steps: []books.Step{page(d1, 80)}},
			map[string]int{d1: 80}},
		{"percent converts through the page count",
			books.ReadingLog{Pages: 400, Status: books.StatusFinished, FinishedOn: d3, Steps: []books.Step{percent(d1, 25), percent(d2, 50)}},
			map[string]int{d1: 100, d2: 100, d3: 200}},
		{"percent without a page count counts nothing",
			books.ReadingLog{Status: books.StatusReading, Steps: []books.Step{percent(d1, 25), percent(d2, 50)}},
			map[string]int{}},
		{"a format change mid-reading: pages, then percent",
			books.ReadingLog{Pages: 200, Status: books.StatusReading, Steps: []books.Step{page(d1, 50), percent(d2, 50)}},
			map[string]int{d1: 50, d2: 50}},
	}
	for _, tt := range tests {
		if got := books.PagesRead(tt.r); !maps.Equal(got, tt.want) {
			t.Errorf("%s: PagesRead = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestPagesReadCountsEachReadingFromTheStart(t *testing.T) {
	// A re-read is its own reading: it starts again from page 0, so its
	// pages count again rather than against the first reading's last page.
	first := books.ReadingLog{Pages: 100, Status: books.StatusFinished, FinishedOn: "2025-05-01",
		Steps: []books.Step{page("2025-04-01", 60)}}
	again := books.ReadingLog{Pages: 100, Status: books.StatusReading,
		Steps: []books.Step{page("2026-02-01", 30)}}
	if got := books.PagesRead(first); !maps.Equal(got, map[string]int{"2025-04-01": 60, "2025-05-01": 40}) {
		t.Errorf("first reading = %v", got)
	}
	if got := books.PagesRead(again); !maps.Equal(got, map[string]int{"2026-02-01": 30}) {
		t.Errorf("re-read = %v, want 30 pages from page 0", got)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -run PagesRead -count=1`
Expected: FAIL — `undefined: books.Step`, `undefined: books.ReadingLog`, `undefined: books.PagesRead`.

- [ ] **Step 3: Write `PagesRead`**

It reuses `Progress.In` for the percent → pages conversion: it already answers 0 for a percentage without a page count, which is exactly "counts no pages otherwise".

Create `internal/apps/books/pages.go`:

```go
package books

// Step is one progress row as the pages-read maths sees it: where the
// reading stood, in the unit it was recorded in, and the local day
// (YYYY-MM-DD) it was recorded on.
type Step struct {
	Unit  Unit
	Value int
	Day   string
}

// ReadingLog is one reading as the pages-read maths sees it.
type ReadingLog struct {
	Pages      int    // the book's page count; 0 when unknown
	Status     Status // how it ended; StatusReading while it goes on
	FinishedOn string // YYYY-MM-DD; "" while reading, and for an undated reading
	Steps      []Step // its progress, oldest first
}

// PagesRead is how many pages one reading covered on each local day (spec
// "Derived values"): the positive deltas between consecutive progress rows,
// starting from page 0, each dated by the day of the later row. Finishing
// with a page count adds the remainder — the last page minus the last
// recorded page — on the finish date. Going backwards counts nothing, and
// neither does a repeated value. A percentage converts through the page
// count, and counts no pages without one. Days with nothing are absent.
func PagesRead(r ReadingLog) map[string]int {
	out := map[string]int{}
	last := 0
	for _, s := range r.Steps {
		page := Progress{Unit: s.Unit, Value: s.Value}.In(UnitPage, r.Pages)
		if d := page - last; d > 0 {
			out[s.Day] += d
		}
		last = page
	}
	if r.Status == StatusFinished && r.FinishedOn != "" && r.Pages > 0 {
		if d := r.Pages - last; d > 0 {
			out[r.FinishedOn] += d
		}
	}
	return out
}
```

- [ ] **Step 4: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./internal/apps/books/
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./internal/apps/books/
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): pages-read maths (#490)"
```

---

### Task 3: Reading stats in the store

**Files:**
- Create: `internal/apps/books/stats.go`
- Test: `internal/apps/books/stats_test.go`

**Interfaces:**
- Consumes: `PagesRead`, `ReadingLog`, `Step` (Task 2); `scanProgress`, `Formats`, `dayLayout`, `Store.now` (existing); test helpers `noon`, `finished` (Task 1), `withPages` (notes_test.go).
- Produces:
  - `type BookPages struct { ID int64; Title string; Pages int }`
  - `type FormatCount struct { Format string; N int }`, `type YearCount struct { Year, N int }`
  - `type YearStats struct { Year, Finished, Pages int; Rating float64; Formats []FormatCount; Longest, Shortest BookPages; ByMonth [12]int }`
  - `type AllTime struct { Finished, Pages int; Rating float64; ByYear []YearCount; First int }`
  - `type Stats struct { Year YearStats; All AllTime }`
  - `(*Store).Stats(ctx, userID int64, year int) (Stats, error)`
  - test helpers: `startOn`, `progressOn`, `finishOn`, `rate`, `stats`

- [ ] **Step 1: Write the failing tests**

The big test builds one library that touches every rule — a re-read in percent, a repeated progress value, a DNF with pages, a book with no page count, an undated import, another user's book — and checks each number with the sum written out.

Create `internal/apps/books/stats_test.go`:

```go
package books_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// startOn starts reading book id at noon on day, in format ("": not set).
func startOn(t *testing.T, f *fixture, userID, id int64, format, day string) {
	t.Helper()
	ctx := context.Background()
	f.now = noon(day)
	if err := f.store.StartReading(ctx, userID, id); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetFormat(ctx, userID, id, format); err != nil {
		t.Fatal(err)
	}
}

// progressOn records progress on the reading of book id at noon on day.
func progressOn(t *testing.T, f *fixture, userID, id int64, day string, value int) {
	t.Helper()
	f.now = noon(day)
	if err := f.store.RecordProgress(context.Background(), userID, id, value); err != nil {
		t.Fatal(err)
	}
}

// finishOn finishes the reading of book id on day, at noon that day.
func finishOn(t *testing.T, f *fixture, userID, id int64, day string) {
	t.Helper()
	f.now = noon(day)
	if err := f.store.FinishReading(context.Background(), userID, id, day, 0); err != nil {
		t.Fatal(err)
	}
}

// rate gives book id a rating.
func rate(t *testing.T, f *fixture, userID, id int64, rating int) {
	t.Helper()
	if err := f.store.SetRating(context.Background(), userID, id, rating); err != nil {
		t.Fatal(err)
	}
}

func stats(t *testing.T, f *fixture, userID int64, year int) books.Stats {
	t.Helper()
	s, err := f.store.Stats(context.Background(), userID, year)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStatsCountTheYearAndAllTime(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	uid := f.alice.ID

	f.now = noon("2025-06-01")
	old := finished(t, f, uid, "Old", 200, "2025-06-01")
	rate(t, f, uid, old, 3)

	f.now = noon("2026-02-10")
	dune := finished(t, f, uid, "Dune", 600, "2026-02-10") // no progress: all 600 on the day
	rate(t, f, uid, dune, 4)

	emma := withPages(t, f, "Emma", 300)
	startOn(t, f, uid, emma, "ebook", "2026-03-01")
	progressOn(t, f, uid, emma, "2026-03-01", 100)
	progressOn(t, f, uid, emma, "2026-03-02", 100) // a repeated value adds nothing (#576)
	progressOn(t, f, uid, emma, "2026-03-03", 250)
	finishOn(t, f, uid, emma, "2026-03-05") // the last 50
	rate(t, f, uid, emma, 5)

	f.now = noon("2026-03-20")
	finished(t, f, uid, "Short", 90, "2026-03-20") // unrated

	f.now = noon("2026-04-01")
	unpaged := finished(t, f, uid, "Unpaged", 0, "2026-04-01")
	rate(t, f, uid, unpaged, 2)

	startOn(t, f, uid, dune, "audio", "2026-08-01") // a re-read, in percent
	progressOn(t, f, uid, dune, "2026-08-02", 50)   // 300 of 600 pages
	finishOn(t, f, uid, dune, "2026-08-20")         // and the other 300

	long := withPages(t, f, "Long", 1000)
	startOn(t, f, uid, long, "paper", "2026-09-01")
	progressOn(t, f, uid, long, "2026-09-02", 120) // pages count; the DNF doesn't
	f.now = noon("2026-09-05")
	if err := f.store.MarkDNF(ctx, uid, long, "2026-09-05", 0); err != nil {
		t.Fatal(err)
	}

	ancient := withPages(t, f, "Ancient", 400) // an imported read with no date
	rate(t, f, uid, ancient, 1)
	if _, err := f.db.Exec(`INSERT INTO books_readings (book_id, status, created_at) VALUES (?, 'finished', ?)`,
		ancient, db.FormatTime(f.now)); err != nil {
		t.Fatal(err)
	}
	f.now = noon("2026-03-01")
	finished(t, f, f.bob.ID, "Bob's book", 300, "2026-03-01")
	f.now = noon("2026-10-10")

	s := stats(t, f, uid, 2026)
	y := s.Year
	if y.Year != 2026 || y.Finished != 5 {
		t.Errorf("2026 finished = %d, want 5 (Dune twice, Emma, Short, Unpaged)", y.Finished)
	}
	if y.Pages != 1710 {
		t.Errorf("2026 pages = %d, want 600 + 300 + 90 + 600 + 120 = 1710", y.Pages)
	}
	if want := 11.0 / 3; y.Rating != want {
		t.Errorf("2026 rating = %v, want %v (Dune 4, Emma 5, Unpaged 2; each book once)", y.Rating, want)
	}
	wantFormats := []books.FormatCount{{Format: "ebook", N: 1}, {Format: "audio", N: 1}, {Format: "", N: 3}}
	if !slices.Equal(y.Formats, wantFormats) {
		t.Errorf("2026 formats = %v, want %v", y.Formats, wantFormats)
	}
	if y.Longest.ID != dune || y.Longest.Pages != 600 || y.Longest.Title != "Dune" {
		t.Errorf("longest = %+v, want Dune, 600", y.Longest)
	}
	if y.Shortest.Title != "Short" || y.Shortest.Pages != 90 {
		t.Errorf("shortest = %+v, want Short, 90 (a book with no page count isn't one)", y.Shortest)
	}
	if want := [12]int{0, 1, 2, 1, 0, 0, 0, 1}; y.ByMonth != want {
		t.Errorf("by month = %v, want %v", y.ByMonth, want)
	}

	a := s.All
	if a.Finished != 7 {
		t.Errorf("all-time finished = %d, want 7 (2026's 5, Old, and Ancient with no date)", a.Finished)
	}
	if a.Pages != 1910 {
		t.Errorf("all-time pages = %d, want 1710 + Old's 200", a.Pages)
	}
	if a.Rating != 3 {
		t.Errorf("all-time rating = %v, want 3 (4, 5, 2, 3, 1)", a.Rating)
	}
	if want := []books.YearCount{{Year: 2025, N: 1}, {Year: 2026, N: 5}}; !slices.Equal(a.ByYear, want) {
		t.Errorf("by year = %v, want %v", a.ByYear, want)
	}
	if a.First != 2025 {
		t.Errorf("first year = %d, want 2025", a.First)
	}

	if past := stats(t, f, uid, 2025).Year; past.Finished != 1 || past.Pages != 200 || past.Rating != 3 {
		t.Errorf("2025 = %+v, want Old: 1 book, 200 pages, rated 3", past)
	}
}

func TestStatsCountPagesOnTheLocalDay(t *testing.T) {
	f := newFixture(t)
	uid := f.alice.ID
	id := withPages(t, f, "Dune", 600)
	startOn(t, f, uid, id, "paper", "2025-12-30")
	// 14:00 UTC on 31 December is 1 January in Melbourne (TestMain's zone).
	f.now = time.Date(2025, 12, 31, 14, 0, 0, 0, time.UTC)
	if err := f.store.RecordProgress(context.Background(), uid, id, 40); err != nil {
		t.Fatal(err)
	}
	f.now = noon("2026-10-10")
	if got := stats(t, f, uid, 2025).Year.Pages; got != 0 {
		t.Errorf("2025 pages = %d, want 0: the update was on 1 January, local time", got)
	}
	if got := stats(t, f, uid, 2026).Year.Pages; got != 40 {
		t.Errorf("2026 pages = %d, want 40", got)
	}
	if got := stats(t, f, uid, 2026).All.First; got != 2026 {
		t.Errorf("first year = %d, want 2026 (the local day of the only update)", got)
	}
}

func TestStatsTiesGoToTheBookFinishedFirst(t *testing.T) {
	f := newFixture(t)
	uid := f.alice.ID
	finished(t, f, uid, "Later", 300, "2026-05-01")
	finished(t, f, uid, "Earlier", 300, "2026-02-01")
	y := stats(t, f, uid, 2026).Year
	if y.Longest.Title != "Earlier" || y.Shortest.Title != "Earlier" {
		t.Errorf("longest %q, shortest %q; want Earlier for both", y.Longest.Title, y.Shortest.Title)
	}
}

func TestStatsOfAnEmptyYear(t *testing.T) {
	f := newFixture(t) // 10 October 2026
	s := stats(t, f, f.alice.ID, 2026)
	if s.Year.Finished != 0 || s.Year.Pages != 0 || s.Year.Rating != 0 || s.Year.Formats != nil ||
		s.Year.Longest.ID != 0 || s.Year.Shortest.ID != 0 {
		t.Errorf("an empty year = %+v, want nothing counted", s.Year)
	}
	if s.All.ByYear != nil || s.All.First != 2026 || s.All.Finished != 0 {
		t.Errorf("all time with no books = %+v, want no years and this year first", s.All)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -run Stats -count=1`
Expected: FAIL — `undefined: books.Stats`, `f.store.Stats undefined`, `undefined: books.FormatCount`, `undefined: books.YearCount`.

- [ ] **Step 3: Write `Stats`**

Create `internal/apps/books/stats.go`:

```go
package books

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// BookPages is a book the Stats page names, with its length.
type BookPages struct {
	ID    int64
	Title string
	Pages int
}

// FormatCount is how many readings finished in a format ("" for not set).
type FormatCount struct {
	Format string
	N      int
}

// YearCount is how many readings finished in a year.
type YearCount struct{ Year, N int }

// YearStats is one year on the Stats page (spec "Stats (B4)").
type YearStats struct {
	Year     int
	Finished int // readings finished in the year; a re-read counts again
	Pages    int // PagesRead over the year's days
	// Rating is the mean rating of the distinct rated books finished in
	// the year; 0 when none is rated.
	Rating float64
	// Formats counts the year's finished readings by format, in Formats
	// order and then not set; a format with none is left out.
	Formats []FormatCount
	// Longest and Shortest are among the books finished in the year that
	// have a page count; a tie goes to the one finished first. Zero ID
	// when there is none.
	Longest, Shortest BookPages
	ByMonth           [12]int // finished readings, January first
}

// AllTime is the Stats page's all-time row.
type AllTime struct {
	Finished int     // every finished reading, dated or not
	Pages    int     // PagesRead over every day
	Rating   float64 // the mean rating of every rated book ever finished
	// ByYear is the dated finishes per year from the first one's year to
	// this year, oldest first, quiet years included; nil when there are none.
	ByYear []YearCount
	// First is the earliest year with a dated finish or a progress update;
	// this year when there is neither.
	First int
}

// Stats is everything the Stats page counts for one year.
type Stats struct {
	Year YearStats
	All  AllTime
}

// finish is one finished reading with what the stats need of its book.
type finish struct {
	BookID        int64
	Title         string
	Pages, Rating int
	Format        string
	FinishedOn    string // "" for an undated (imported) reading
}

// Stats counts userID's reading for year, and all time (spec "Stats
// (B4)", "Derived values"). The counting is done here in Go, over every
// finished reading and every progress row: one household's reading is
// small, and the pages-read rules don't fit SQL well.
func (st *Store) Stats(ctx context.Context, userID int64, year int) (Stats, error) {
	fs, err := st.finishes(ctx, userID)
	if err != nil {
		return Stats{}, err
	}
	logs, err := st.readingLogs(ctx, userID)
	if err != nil {
		return Stats{}, err
	}
	return summarise(fs, logs, year, st.now().Local().Year()), nil
}

// finishes is every finished reading of userID's, in finish order
// (undated first).
func (st *Store) finishes(ctx context.Context, userID int64) ([]finish, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT b.id, b.title, COALESCE(b.pages, 0), COALESCE(b.rating, 0), COALESCE(r.format, ''),
		       COALESCE(r.finished_on, '')
		  FROM books_readings r JOIN books_books b ON b.id = r.book_id
		 WHERE b.user_id = ? AND r.status = 'finished'
		 ORDER BY r.finished_on, r.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("books: finishes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []finish
	for rows.Next() {
		var f finish
		if err := rows.Scan(&f.BookID, &f.Title, &f.Pages, &f.Rating, &f.Format, &f.FinishedOn); err != nil {
			return nil, fmt.Errorf("books: scan finish: %w", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: finishes: %w", err)
	}
	return out, nil
}

// readingLogs is every reading of userID's with its progress, for
// PagesRead. Each row's day is its recorded_at in the local zone.
func (st *Store) readingLogs(ctx context.Context, userID int64) ([]ReadingLog, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT r.id, r.status, COALESCE(r.finished_on, ''), COALESCE(b.pages, 0), p.page, p.percent, p.recorded_at
		  FROM books_readings r JOIN books_books b ON b.id = r.book_id
		  LEFT JOIN books_progress p ON p.reading_id = r.id
		 WHERE b.user_id = ?
		 ORDER BY r.id, p.recorded_at, p.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("books: reading logs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ReadingLog
	var last int64
	for rows.Next() {
		var id int64
		var status, finishedOn string
		var pages int
		var page, percent sql.NullInt64
		var at sql.NullString
		if err := rows.Scan(&id, &status, &finishedOn, &pages, &page, &percent, &at); err != nil {
			return nil, fmt.Errorf("books: scan reading log: %w", err)
		}
		if len(out) == 0 || id != last {
			out = append(out, ReadingLog{Pages: pages, Status: Status(status), FinishedOn: finishedOn})
			last = id
		}
		p, err := scanProgress(page, percent, at)
		if err != nil {
			return nil, err
		}
		if p.Set() {
			r := &out[len(out)-1]
			r.Steps = append(r.Steps, Step{Unit: p.Unit, Value: p.Value, Day: p.RecordedAt.Local().Format(dayLayout)})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: reading logs: %w", err)
	}
	return out, nil
}

// summarise counts the finishes and the pages read for year and all time.
// thisYear ends ByYear.
func summarise(fs []finish, logs []ReadingLog, year, thisYear int) Stats {
	s := Stats{Year: YearStats{Year: year}, All: AllTime{First: thisYear}}
	yearRated, allRated := map[int64]int{}, map[int64]int{}
	formats := map[string]int{}
	perYear := map[int]int{}
	firstFinish := 0
	for _, f := range fs {
		s.All.Finished++
		if f.Rating > 0 {
			allRated[f.BookID] = f.Rating
		}
		day, err := time.Parse(dayLayout, f.FinishedOn)
		if err != nil { // undated: all time only
			continue
		}
		perYear[day.Year()]++
		if firstFinish == 0 || day.Year() < firstFinish {
			firstFinish = day.Year()
		}
		if day.Year() != year {
			continue
		}
		y := &s.Year
		y.Finished++
		y.ByMonth[day.Month()-1]++
		formats[f.Format]++
		if f.Rating > 0 {
			yearRated[f.BookID] = f.Rating
		}
		if f.Pages > 0 {
			book := BookPages{ID: f.BookID, Title: f.Title, Pages: f.Pages}
			if y.Longest.ID == 0 || f.Pages > y.Longest.Pages {
				y.Longest = book
			}
			if y.Shortest.ID == 0 || f.Pages < y.Shortest.Pages {
				y.Shortest = book
			}
		}
	}
	for _, f := range append(append([]string{}, Formats...), "") {
		if n := formats[f]; n > 0 {
			s.Year.Formats = append(s.Year.Formats, FormatCount{Format: f, N: n})
		}
	}
	s.Year.Rating, s.All.Rating = mean(yearRated), mean(allRated)
	if firstFinish != 0 {
		for y := firstFinish; y <= thisYear; y++ {
			s.All.ByYear = append(s.All.ByYear, YearCount{Year: y, N: perYear[y]})
		}
		s.All.First = min(s.All.First, firstFinish)
	}
	prefix := strconv.Itoa(year) + "-"
	for _, r := range logs {
		for day, n := range PagesRead(r) {
			s.All.Pages += n
			if strings.HasPrefix(day, prefix) {
				s.Year.Pages += n
			}
			if y, err := strconv.Atoi(day[:4]); err == nil {
				s.All.First = min(s.All.First, y)
			}
		}
	}
	return s
}

// mean is the average of a set of ratings, 0 for none.
func mean(ratings map[int64]int) float64 {
	if len(ratings) == 0 {
		return 0
	}
	sum := 0
	for _, r := range ratings {
		sum += r
	}
	return float64(sum) / float64(len(ratings))
}
```

- [ ] **Step 4: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./internal/apps/books/
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./internal/apps/books/
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): reading stats in the store (#490)"
```

---

### Task 4: The Stats page

The numbers and charts, the year links and the sidebar link. The goal joins the page in Task 5.

**Files:**
- Create: `internal/apps/books/statspage.go`, `internal/apps/books/templates/stats.html`
- Modify: `internal/apps/books/books.go`, `internal/apps/books/templates/panes.partial.html`, `internal/ui/static/app.css`
- Test: `internal/apps/books/stats_view_test.go`

**Interfaces:**
- Consumes: `Store.Stats`, `Stats`, `BookPages` (Task 3); `MinYear` (Task 1); `formatLabels`, `countText`, `listCtx.BookURL` (view.go); `a.userID`, `a.fail` (handlers.go); test helpers `newServer`, `add`, `titled` (handlers_test.go), `texts` (notes_view_test.go), `noon` (Task 1).
- Produces:
  - route `GET /books/stats` → `(*App).stats`; `?year=` empty is this year, `MinYear`–next year allowed, anything else 404
  - `func parseYear(s string, thisYear int) (int, bool)`, `func statsURL(year int) string` — "/books/stats?year=2026"
  - `func (a *App) renderStats(w, r, userID int64, year int)` (Task 5 widens it)
  - `type statsView struct { Year int; Years []yearLink; Tiles, Formats []statTile; Lengths []lengthLine; Months chartView; AllTiles []statTile; PerYear chartView }`, `func viewStats(s Stats, today time.Time) statsView`
  - `chartPoint`, `chartBar`, `chartView`, `buildChart` (at least `minSlots = 12` slots)
  - template `books/stats` with sections `#books-year` and `#books-all`; `.books-years` links; `.books-stat-tile`; `.books-stat-line`; `figure.books-chart` with `.books-chart-ticks` and `details.books-chart-figures`
  - sidebar link `.books-stats-link` to `/books/stats`
  - test helper: `finishedOn(t, s, userID, title, pages, day) int64`

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/stats_view_test.go`:

```go
package books_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// finishedOn adds one of userID's books as read, finished on day.
func finishedOn(t *testing.T, s *server, userID int64, title string, pages int, day string) int64 {
	t.Helper()
	nb := titled(title, "", books.ShelfRead)
	nb.Pages, nb.FinishedOn = pages, day
	return add(t, s, userID, nb)
}

func TestStatsPageShowsTheYear(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	uid := s.Alice.User.ID
	finishedOn(t, s, uid, "Old", 150, "2025-06-01")
	finishedOn(t, s, uid, "Infinite Jest", 1079, "2026-02-10")
	finishedOn(t, s, uid, "Emma", 300, "2026-03-05")
	finishedOn(t, s, uid, "Unpaged", 0, "2026-03-20")

	doc := s.Get(t, s.Alice, "/books/stats")
	if got := htmlassert.Text(doc.MustHave("h1")); got != "Reading stats" {
		t.Errorf("h1 = %q", got)
	}
	doc.MustHave(`a[href="/books/"]`)
	if got := texts(doc, ".books-years a"); !slices.Equal(got, []string{"2025", "2026", "2027"}) {
		t.Errorf("years = %v, want 2025 (the first finish) to 2027 (next year)", got)
	}
	if got := htmlassert.Text(doc.MustHave(`.books-years a[aria-current="page"]`)); got != "2026" {
		t.Errorf("current year = %q, want 2026", got)
	}
	tiles := texts(doc, ".books-stat-tile .value")
	if want := []string{"3", "1,379", "—", "4", "1,529", "—"}; !slices.Equal(tiles, want) {
		t.Errorf("tiles = %v, want %v (2026, then all time)", tiles, want)
	}
	lines := texts(doc, ".books-stat-line")
	want := []string{"Formats Not set 3", "Longest Infinite Jest · 1,079 pages", "Shortest Emma · 300 pages"}
	if !slices.Equal(lines, want) {
		t.Errorf("lines = %q, want %q", lines, want)
	}
	if href, _ := htmlassert.Attr(doc.MustHave(".books-stat-line a"), "href"); !strings.HasPrefix(href, "/books/b/") {
		t.Errorf("longest book links to %q, want the book", href)
	}

	if n := len(doc.QueryAll("#books-year rect")); n != 12 {
		t.Errorf("%d month bars, want 12", n)
	}
	if n := len(doc.QueryAll("#books-all rect")); n != 2 {
		t.Errorf("%d year bars, want 2 (2025 and 2026)", n)
	}
	ticks := texts(doc, ".books-chart-ticks li")
	if len(ticks) != 24 || !slices.Equal(ticks[:3], []string{"Jan", "Feb", "Mar"}) ||
		!slices.Equal(ticks[12:15], []string{"’25", "’26", ""}) {
		t.Errorf("ticks = %q, want the months, then 2025, 2026 and ten empty slots", ticks)
	}
	if got := texts(doc, ".books-chart-figures td"); !slices.Contains(got, "March") || !slices.Contains(got, "2025") {
		t.Errorf("figures = %v, want a row for March and one for 2025", got)
	}
	if label, _ := htmlassert.Attr(doc.MustHave("#books-year svg"), "aria-label"); label != "Books finished by month, 2026, peak 2" {
		t.Errorf("month chart's label = %q", label)
	}
	if title := htmlassert.Text(doc.MustHave("#books-year rect title")); title != "January: 0 books" {
		t.Errorf("first bar's title = %q", title)
	}
}

func TestStatsPageShowsAnotherYear(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	uid := s.Alice.User.ID
	finishedOn(t, s, uid, "Old", 150, "2025-06-01")
	finishedOn(t, s, uid, "Emma", 300, "2026-03-05")

	doc := s.Get(t, s.Alice, "/books/stats?year=2025")
	if got := htmlassert.Text(doc.MustHave(`.books-years a[aria-current="page"]`)); got != "2025" {
		t.Errorf("current year = %q, want 2025", got)
	}
	if got := texts(doc, ".books-stat-tile .value"); got[0] != "1" || got[1] != "150" {
		t.Errorf("2025 tiles = %v, want 1 book, 150 pages", got)
	}
	if got := htmlassert.Text(doc.MustHave("#books-year-head")); got != "2025" {
		t.Errorf("year heading = %q", got)
	}
	// A year before any reading still shows, with the picker reaching back to it.
	doc = s.Get(t, s.Alice, "/books/stats?year=2020")
	if got := texts(doc, ".books-years a"); got[0] != "2020" {
		t.Errorf("years = %v, want them to start at 2020", got)
	}
	doc.MustHave(".books-chart .empty")
}

func TestStatsPageRefusesYearsItCannotShow(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	for _, q := range []string{"abc", "1899", "2028", "2026.5"} {
		rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/books/stats?year="+q, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET /books/stats?year=%s = %d, want 404", q, rec.Code)
		}
	}
	s.Get(t, s.Alice, "/books/stats?year=2027") // next year: its goal can be set ahead
}

func TestStatsPageOnAnEmptyAccount(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	doc := s.Get(t, s.Alice, "/books/stats")
	if got := texts(doc, ".books-years a"); !slices.Equal(got, []string{"2026", "2027"}) {
		t.Errorf("years = %v, want this year and next", got)
	}
	empties := texts(doc, ".books-chart .empty")
	if !slices.Equal(empties, []string{"No books finished in 2026.", "No dated finishes yet."}) {
		t.Errorf("empty charts say %q", empties)
	}
	doc.MustNotHave(".books-stat-line")
	doc.MustNotHave(".books-chart svg")
}

func TestStatsPageIsPerUser(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	finishedOn(t, s, s.Bob.User.ID, "Bob's book", 100, "2026-03-05")
	doc := s.Get(t, s.Alice, "/books/stats")
	if got := texts(doc, ".books-stat-tile .value"); got[0] != "0" {
		t.Errorf("Alice's tiles = %v, want nothing of Bob's", got)
	}
	if strings.Contains(doc.Text(), "Bob's book") {
		t.Error("Alice's stats name Bob's book")
	}
}

func TestStatsPageRequiresSignIn(t *testing.T) {
	s := newServer(t)
	if rec := s.Do(t, nil, httptest.NewRequest("GET", "/books/stats", nil)); rec.Code != http.StatusSeeOther {
		t.Errorf("anonymous GET /books/stats = %d, want a 303 to the login page", rec.Code)
	}
}

func TestSidebarLinksToStats(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/books/")
	doc.MustHave(`.books-side a[href="/books/stats"]`)
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -run 'StatsPage|SidebarLinksToStats' -count=1`
Expected: FAIL — every test: `GET /books/stats = 404, want 200` (and `?year=2025`, `?year=2027`), `anonymous GET /books/stats = 404, want a 303 to the login page` (an unknown path is a 404 even signed out), and `no element matches ".books-side a[href=\"/books/stats\"]"`.

- [ ] **Step 3: Write the page**

Create `internal/apps/books/statspage.go`:

```go
package books

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// parseYear reads the Stats page's year: "" is this year; anything but a
// whole number from MinYear to next year is not ok. Next year is there so
// its goal can be set ahead (decided 2026-10-09 while planning B4).
func parseYear(s string, thisYear int) (int, bool) {
	if s == "" {
		return thisYear, true
	}
	y, err := strconv.Atoi(s)
	if err != nil || y < MinYear || y > thisYear+1 {
		return 0, false
	}
	return y, true
}

// statsURL is the Stats page for a year.
func statsURL(year int) string { return "/books/stats?year=" + strconv.Itoa(year) }

// stats is the Stats page (spec "Stats (B4)"), for ?year= or this year.
// A year it can't show is a 404, as a book that isn't there is.
func (a *App) stats(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	year, ok := parseYear(r.URL.Query().Get("year"), a.store.now().Local().Year())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	a.renderStats(w, r, uid, year)
}

// renderStats draws the Stats page for year.
func (a *App) renderStats(w http.ResponseWriter, r *http.Request, userID int64, year int) {
	s, err := a.store.Stats(r.Context(), userID, year)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	page := a.deps.Page(r, "Reading stats")
	page.Data = viewStats(s, a.store.now().Local())
	if err := a.deps.Render.Page(w, http.StatusOK, "books/stats", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// statsView is the Stats page.
type statsView struct {
	Year     int
	Years    []yearLink // the year picker
	Tiles    []statTile // the year's numbers
	Formats  []statTile // the year's finished readings by format
	Lengths  []lengthLine
	Months   chartView
	AllTiles []statTile
	PerYear  chartView
}

// yearLink is one year in the picker.
type yearLink struct {
	Year    int
	URL     string
	Current bool
}

type statTile struct{ Label, Value string }

// lengthLine is the year's longest or shortest book.
type lengthLine struct{ Label, Title, URL, Pages string }

// viewStats draws s as of today, a local time.
func viewStats(s Stats, today time.Time) statsView {
	y := s.Year
	v := statsView{Year: y.Year,
		Tiles:    statTiles(y.Finished, y.Pages, y.Rating),
		AllTiles: statTiles(s.All.Finished, s.All.Pages, s.All.Rating)}
	// The picker runs from the first year with any reading to next year
	// (decided 2026-10-09 while planning B4), and takes in a year typed
	// into the address bar before that.
	for year := min(s.All.First, y.Year); year <= today.Year()+1; year++ {
		v.Years = append(v.Years, yearLink{Year: year, URL: statsURL(year), Current: year == y.Year})
	}
	for _, f := range y.Formats {
		v.Formats = append(v.Formats, statTile{Label: formatLabels[f.Format], Value: strconv.Itoa(f.N)})
	}
	if y.Longest.ID != 0 {
		v.Lengths = append(v.Lengths, lengthOf("Longest", y.Longest))
	}
	if y.Shortest.ID != 0 && y.Shortest.ID != y.Longest.ID {
		v.Lengths = append(v.Lengths, lengthOf("Shortest", y.Shortest))
	}
	var months []chartPoint
	for i, n := range y.ByMonth {
		m := time.Month(i + 1)
		months = append(months, chartPoint{Tick: m.String()[:3], Name: m.String(), N: n})
	}
	v.Months = buildChart(months)
	var years []chartPoint
	for _, c := range s.All.ByYear {
		years = append(years, chartPoint{Tick: yearTick(c.Year), Name: strconv.Itoa(c.Year), N: c.N})
	}
	v.PerYear = buildChart(years)
	return v
}

// statTiles are a span's three headline numbers.
func statTiles(finished, pages int, rating float64) []statTile {
	return []statTile{
		{Label: "Books finished", Value: strconv.Itoa(finished)},
		{Label: "Pages read", Value: groupDigits(pages)},
		{Label: "Average rating", Value: ratingText(rating)},
	}
}

// ratingText is an average rating to one decimal place, "—" for none.
func ratingText(r float64) string {
	if r == 0 {
		return "—"
	}
	return strconv.FormatFloat(r, 'f', 1, 64) + " ★"
}

// groupDigits writes n with thousands separators: 12345 → "12,345".
func groupDigits(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func lengthOf(label string, b BookPages) lengthLine {
	return lengthLine{Label: label, Title: b.Title, URL: listCtx{Shelf: ShelfAll}.BookURL(b.ID),
		Pages: groupDigits(b.Pages) + " pages"}
}

// yearTick labels a year's bar "’24": a slot is a twelfth of the chart
// or less, and at phone width "2024" doesn't fit in one. The tooltip and
// the text alternative have the whole year.
func yearTick(year int) string { return fmt.Sprintf("’%02d", year%100) }

// chartPoint is one bar's data: its tick (the label under it), its name
// in the tooltip and the text alternative, and how many books it stands
// for.
type chartPoint struct {
	Tick, Name string
	N          int
}

// chartBar is one already-positioned bar: html/template can't do
// arithmetic. Mirrors ON Focus's chartBar (PATTERNS.md, "Cross-app
// mirroring").
type chartBar struct {
	X, Y, Width, Height float64
	Label               string
}

type chartView struct {
	Bars          []chartBar
	Ticks         []string     // under the bars, one per slot
	Points        []chartPoint // the text alternative
	Width, Height float64
	Peak          int
	Empty         bool
}

// minSlots is the fewest bar slots a chart has: twelve, a year's months,
// so a first year of reading isn't one bar the width of the page.
const minSlots = 12

// buildChart lays out one bar per point, each in an equal slot so the
// ticks below (an equal-width flex row) line up with them; with fewer
// points than minSlots the slots left over stay empty. Mirrors ON Focus's
// buildChart.
func buildChart(points []chartPoint) chartView {
	const (
		height = 100.0
		slot   = 10.0
		barW   = 7.0
	)
	slots := max(len(points), minSlots)
	out := chartView{Points: points, Height: height, Width: float64(slots) * slot}
	for _, p := range points {
		out.Peak = max(out.Peak, p.N)
	}
	out.Empty = out.Peak == 0
	for i, p := range points {
		h := 0.0
		if out.Peak > 0 {
			h = float64(p.N) / float64(out.Peak) * height
		}
		out.Bars = append(out.Bars, chartBar{
			X: float64(i)*slot + (slot-barW)/2, Y: height - h, Width: barW, Height: h,
			Label: p.Name + ": " + countText(p.N, "book", "books"),
		})
		out.Ticks = append(out.Ticks, p.Tick)
	}
	for len(out.Ticks) < slots {
		out.Ticks = append(out.Ticks, "")
	}
	return out
}
```

Create `internal/apps/books/templates/stats.html`:

```html
{{define "content"}}
{{$d := .Data}}
<div class="books-stats stack">
	<div class="books-stats-head">
		<h1>Reading stats</h1>
		<a class="button" href="/books/">← Books</a>
	</div>
	<nav class="books-years" aria-label="Year">
		{{range $d.Years}}<a class="books-year" href="{{.URL}}"{{if .Current}} aria-current="page"{{end}}>{{.Year}}</a>{{end}}
	</nav>

	<section class="books-stats-section" id="books-year" aria-labelledby="books-year-head">
		<h2 id="books-year-head">{{$d.Year}}</h2>
		{{template "stat-tiles" $d.Tiles}}
		{{with $d.Formats}}<p class="books-stat-line"><span class="books-stat-label">Formats</span> {{range $i, $f := .}}{{if $i}} · {{end}}{{$f.Label}} {{$f.Value}}{{end}}</p>{{end}}
		{{range $d.Lengths}}<p class="books-stat-line"><span class="books-stat-label">{{.Label}}</span> <a href="{{.URL}}">{{.Title}}</a> · {{.Pages}}</p>{{end}}
		{{template "stats-chart" (dict "Chart" $d.Months "Title" (printf "Books finished by month, %d" $d.Year) "Empty" (printf "No books finished in %d." $d.Year) "Head" "Month")}}
	</section>

	<section class="books-stats-section" id="books-all" aria-labelledby="books-all-head">
		<h2 id="books-all-head">All time</h2>
		{{template "stat-tiles" $d.AllTiles}}
		{{template "stats-chart" (dict "Chart" $d.PerYear "Title" "Books finished per year" "Empty" "No dated finishes yet." "Head" "Year")}}
	</section>
</div>
{{end}}

{{/* stat-tiles takes []statTile. */}}
{{define "stat-tiles"}}
<div class="books-stat-tiles">
	{{range .}}<div class="books-stat-tile"><div class="value">{{.Value}}</div><div class="label">{{.Label}}</div></div>{{end}}
</div>
{{end}}

{{/* stats-chart is one bar chart, server-rendered SVG as in ON Focus, with
     its ticks underneath and a table as its text alternative (Reader's
     #242 lesson). Pass a dict with Chart (a chartView), Title, Empty (what
     to say with nothing to draw) and Head (the table's first column). */}}
{{define "stats-chart"}}
<figure class="books-chart">
	<figcaption>{{.Title}}</figcaption>
	{{if .Chart.Empty}}
	<p class="empty">{{.Empty}}</p>
	{{else}}
	<svg viewBox="0 0 {{.Chart.Width}} {{.Chart.Height}}" role="img" aria-label="{{.Title}}, peak {{.Chart.Peak}}" preserveAspectRatio="none">
		{{range .Chart.Bars}}<rect x="{{.X}}" y="{{.Y}}" width="{{.Width}}" height="{{.Height}}" rx="1"><title>{{.Label}}</title></rect>{{end}}
	</svg>
	<ol class="books-chart-ticks" aria-hidden="true">{{range .Chart.Ticks}}<li>{{.}}</li>{{end}}</ol>
	<details class="books-chart-figures">
		<summary>Figures (text alternative to the chart)</summary>
		<table class="books-stats-table">
			<thead><tr><th>{{.Head}}</th><th>Books finished</th></tr></thead>
			<tbody>{{range .Chart.Points}}<tr><td>{{.Name}}</td><td>{{.N}}</td></tr>{{end}}</tbody>
		</table>
	</details>
	{{end}}
</figure>
{{end}}
```

In `internal/apps/books/books.go`, replace:

```go
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("GET /cover/{id}", a.cover)
```

with:

```go
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("GET /stats", a.stats)
	r.HandleFunc("GET /cover/{id}", a.cover)
```

The Stats link goes at the bottom of the sidebar (spec "Layout": shelves, tags, Stats). It is a plain link, not an htmx one: the Stats page isn't part of the panes.

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
		{{end}}
	</ul>
	{{end}}
</nav>
{{end}}
```

with:

```html
		{{end}}
	</ul>
	{{end}}
	<a class="books-stats-link" href="/books/stats">{{ticon "stats"}}Stats</a>
</nav>
{{end}}
```

Append to `internal/ui/static/app.css` (after a blank line):

```css
/* Stats (B4): a page of its own, as Reader's is; tiles and charts mirror
 * ON Focus's History. */
.books-stats-link { display: flex; align-items: center; gap: var(--s-2); margin-top: var(--s-4); padding: var(--s-1) var(--s-2); border-radius: var(--radius); color: var(--c-text); text-decoration: none; }
.books-stats-link:hover { background: var(--c-bg-subtle); }
.books-stats { max-width: 48rem; }
.books-stats-head { display: flex; flex-wrap: wrap; align-items: baseline; justify-content: space-between; gap: var(--s-3); }
.books-stats-head h1 { margin: 0; }
.books-years { display: flex; flex-wrap: wrap; gap: var(--s-1); }
.books-year { padding: var(--s-1) var(--s-2); border-radius: var(--radius); color: var(--c-text); text-decoration: none; font-variant-numeric: tabular-nums; }
.books-year:hover { background: var(--c-bg-subtle); }
.books-year[aria-current="page"] { background: var(--c-accent-bg); color: var(--c-accent); font-weight: 600; }
.books-stats-section h2 { margin: 0 0 var(--s-3); font-size: var(--fs-lg); }
.books-stat-tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(8rem, 1fr)); gap: var(--s-3); margin-bottom: var(--s-3); }
.books-stat-tile { background: var(--c-bg-subtle); border-radius: var(--radius); padding: var(--s-3); }
.books-stat-tile .value { font-size: var(--fs-xl); }
.books-stat-tile .label { color: var(--c-text-dim); font-size: var(--fs-xs); }
.books-stat-line { margin: 0 0 var(--s-2); overflow-wrap: anywhere; }
.books-stat-label { color: var(--c-text-dim); font-size: var(--fs-sm); margin-right: var(--s-1); }
.books-chart { margin: var(--s-4) 0 0; }
.books-chart figcaption { font-size: var(--fs-sm); color: var(--c-text-dim); margin-bottom: var(--s-2); }
.books-chart svg { width: 100%; height: 120px; display: block; overflow: visible; }
.books-chart rect { fill: var(--c-accent); }
.books-chart rect:hover { fill: var(--c-text); }
.books-chart .empty { color: var(--c-text-dim); }
.books-chart-ticks { display: flex; list-style: none; margin: var(--s-1) 0 0; padding: 0; }
.books-chart-ticks li { flex: 1 1 0; min-width: 0; overflow: hidden; text-align: center; color: var(--c-text-dim); font-size: var(--fs-xs); }
.books-chart-figures { margin-top: var(--s-2); font-size: var(--fs-sm); }
.books-stats-table { width: 100%; border-collapse: collapse; }
.books-stats-table th,
.books-stats-table td { text-align: left; padding: var(--s-1) var(--s-2); border-bottom: 1px solid var(--c-border); }
.books-stats-table th { color: var(--c-text-dim); font-weight: 500; }
```

- [ ] **Step 4: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./internal/apps/books/
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./internal/apps/books/
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): the Stats page (#490)"
```

---

### Task 5: Set the goal on the Stats page

**Files:**
- Modify: `internal/apps/books/statspage.go`, `internal/apps/books/templates/stats.html`, `internal/apps/books/books.go`, `internal/ui/static/app.css`
- Test: `internal/apps/books/goal_view_test.go`

**Interfaces:**
- Consumes: `Store.Goal`, `SetGoal`, `ClearGoal`, `Goal`, `Goal.Pace`, `MaxGoal`, `Refusal` (Task 1, store.go); `parseYear`, `statsURL`, `viewStats`, `statsView` (Task 4); test helpers `finishedOn` (Task 4), `noon` (Task 1).
- Produces:
  - routes `POST /books/goal` (`year`, `target`) → `(*App).setGoal` and `POST /books/goal/clear` (`year`) → `(*App).clearGoal`; both 303 to `statsURL(year)`; a refused target is the page again, 422, with the message in `#books-goal-error` and the "Change goal" disclosure open; a bad year is 400
  - `type goalDraft struct{ Error, Value string }`; `renderStats(w, r, userID int64, year, status int, d goalDraft)`
  - `type goalView struct { Year, Target int; Text, Pace string; Percent int; Value string; Max int; Error string }`, `func viewGoal(g Goal, today time.Time) goalView` — Task 6 uses both
  - `statsView.Goal`; template blocks `goal` and `goal-form` (`.books-goal-text`, `.books-goal-edit`, `#books-goal-target`)

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/goal_view_test.go`:

```go
package books_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// goalText is the Stats page's goal line: "2 of 30 · 21 behind".
func goalText(t *testing.T, s *server, path string) string {
	t.Helper()
	return htmlassert.Text(s.Get(t, s.Alice, path).MustHave(".books-goal-text"))
}

func TestSettingChangingAndRemovingAGoal(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10")) // day 283: 30 × 283 ÷ 365 = 23 expected
	uid := s.Alice.User.ID
	finishedOn(t, s, uid, "Dune", 600, "2026-02-01")
	finishedOn(t, s, uid, "Emma", 300, "2026-03-01")

	doc := s.Get(t, s.Alice, "/books/stats")
	doc.MustNotHave(".books-goal-text")
	doc.MustNotHave(".books-goal-edit")
	if v, _ := htmlassert.Attr(doc.MustHave(`.books-goal-form input[name="year"]`), "value"); v != "2026" {
		t.Errorf("goal form year = %q, want 2026", v)
	}

	s.Submit(t, s.Alice, "/books/goal", url.Values{"year": {"2026"}, "target": {"30"}}, "/books/stats?year=2026")
	if got := goalText(t, s, "/books/stats"); got != "2 of 30 · 21 behind" {
		t.Errorf("goal = %q, want 2 of 30 · 21 behind", got)
	}
	s.Submit(t, s.Alice, "/books/goal", url.Values{"year": {"2026"}, "target": {" 2 "}}, "/books/stats?year=2026")
	if got := goalText(t, s, "/books/stats"); got != "2 of 2 · goal reached" {
		t.Errorf("goal = %q, want 2 of 2 · goal reached", got)
	}
	s.Submit(t, s.Alice, "/books/goal/clear", url.Values{"year": {"2026"}}, "/books/stats?year=2026")
	s.Get(t, s.Alice, "/books/stats").MustNotHave(".books-goal-text")
}

func TestGoalsForOtherYears(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	finishedOn(t, s, s.Alice.User.ID, "Old", 100, "2025-06-01")
	s.Submit(t, s.Alice, "/books/goal", url.Values{"year": {"2025"}, "target": {"3"}}, "/books/stats?year=2025")
	s.Submit(t, s.Alice, "/books/goal", url.Values{"year": {"2027"}, "target": {"10"}}, "/books/stats?year=2027")
	if got := goalText(t, s, "/books/stats?year=2025"); got != "1 of 3 · 2 short" {
		t.Errorf("2025 goal = %q, want 1 of 3 · 2 short", got)
	}
	if got := goalText(t, s, "/books/stats?year=2027"); got != "0 of 10" {
		t.Errorf("2027 goal = %q, want 0 of 10 and no pace yet", got)
	}
	s.Get(t, s.Alice, "/books/stats").MustNotHave(".books-goal-text") // 2026 has none
}

func TestARefusedGoalComesBackInItsForm(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	s.Submit(t, s.Alice, "/books/goal", url.Values{"year": {"2026"}, "target": {"30"}}, "/books/stats?year=2026")
	for _, typed := range []string{"0", "1001", "abc", ""} {
		rec := s.Post(t, s.Alice, "/books/goal", url.Values{"year": {"2026"}, "target": {typed}})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("goal %q = %d, want 422", typed, rec.Code)
		}
		doc := htmlassert.Parse(t, rec.Body.String())
		if got := htmlassert.Text(doc.MustHave("#books-goal-error")); got != "Enter a goal from 1 to 1000 books." {
			t.Errorf("goal %q: message = %q", typed, got)
		}
		if _, open := htmlassert.Attr(doc.MustHave(".books-goal-edit"), "open"); !open {
			t.Errorf("goal %q: the Change goal box is closed, want it open on the message", typed)
		}
		if v, _ := htmlassert.Attr(doc.MustHave("#books-goal-target"), "value"); v != typed {
			t.Errorf("goal %q: the box holds %q, want what was typed", typed, v)
		}
	}
	if got := goalText(t, s, "/books/stats"); got != "0 of 30 · 23 behind" {
		t.Errorf("goal = %q after refusals, want it unchanged", got)
	}
}

func TestGoalFormsRefuseATamperedYear(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	for _, path := range []string{"/books/goal", "/books/goal/clear"} {
		for _, year := range []string{"1899", "2028", "abc"} {
			rec := s.Post(t, s.Alice, path, url.Values{"year": {year}, "target": {"10"}})
			if rec.Code != http.StatusBadRequest {
				t.Errorf("POST %s year %q = %d, want 400", path, year, rec.Code)
			}
		}
	}
}

func TestGoalsArePerUser(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	s.Submit(t, s.Bob, "/books/goal", url.Values{"year": {"2026"}, "target": {"30"}}, "/books/stats?year=2026")
	s.Get(t, s.Alice, "/books/stats").MustNotHave(".books-goal-text")
	s.Submit(t, s.Alice, "/books/goal/clear", url.Values{"year": {"2026"}}, "/books/stats?year=2026")
	doc := s.Get(t, s.Bob, "/books/stats")
	if got := htmlassert.Text(doc.MustHave(".books-goal-text")); got != "0 of 30 · 23 behind" {
		t.Errorf("Bob's goal = %q after Alice cleared hers, want it kept", got)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -run Goal -count=1`
Expected: FAIL — `no element matches ".books-goal-form input[name=\"year\"]"`, `POST /books/goal = 404, want 303` (no route yet), and `POST /books/goal year "1899" = 404, want 400` (and the other tampered years, for both routes). The store tests from Task 1 still pass.

- [ ] **Step 3: Add the handlers and the goal view**

In `internal/apps/books/statspage.go`, replace:

```go
import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)
```

with:

```go
import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)
```

In `internal/apps/books/statspage.go`, replace:

```go
// stats is the Stats page (spec "Stats (B4)"), for ?year= or this year.
// A year it can't show is a 404, as a book that isn't there is.
func (a *App) stats(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	year, ok := parseYear(r.URL.Query().Get("year"), a.store.now().Local().Year())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	a.renderStats(w, r, uid, year)
}

// renderStats draws the Stats page for year.
func (a *App) renderStats(w http.ResponseWriter, r *http.Request, userID int64, year int) {
	s, err := a.store.Stats(r.Context(), userID, year)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	page := a.deps.Page(r, "Reading stats")
	page.Data = viewStats(s, a.store.now().Local())
	if err := a.deps.Render.Page(w, http.StatusOK, "books/stats", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

```

with:

```go
// stats is the Stats page (spec "Stats (B4)"), for ?year= or this year.
// A year it can't show is a 404, as a book that isn't there is.
func (a *App) stats(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	year, ok := parseYear(r.URL.Query().Get("year"), a.store.now().Local().Year())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	a.renderStats(w, r, uid, year, http.StatusOK, goalDraft{})
}

// goalDraft is a goal the store refused, to show again in the goal form
// with its message and what was typed (spec "Errors": an inline message).
type goalDraft struct{ Error, Value string }

// renderStats draws the Stats page for year; a refused goal comes back
// in its form, with status 422.
func (a *App) renderStats(w http.ResponseWriter, r *http.Request, userID int64, year, status int, d goalDraft) {
	ctx := r.Context()
	s, err := a.store.Stats(ctx, userID, year)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	g, err := a.store.Goal(ctx, userID, year)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	today := a.store.now().Local()
	v := viewStats(s, today)
	v.Goal = viewGoal(g, today)
	if d.Error != "" {
		v.Goal.Error, v.Goal.Value = d.Error, d.Value
	}
	page := a.deps.Page(r, "Reading stats")
	page.Data = v
	if err := a.deps.Render.Page(w, status, "books/stats", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// goalYear reads a goal form's year; one the Stats page can't show is a
// tampered form, a 400.
func (a *App) goalYear(w http.ResponseWriter, r *http.Request) (int, bool) {
	year, ok := parseYear(r.PostFormValue("year"), a.store.now().Local().Year())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
	}
	return year, ok
}

// setGoal sets or changes the year's goal from the Stats page and goes
// back to it; a target the store refuses comes back inline, with what was
// typed. A plain form post: the page works without JavaScript.
func (a *App) setGoal(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	year, ok := a.goalYear(w, r)
	if !ok {
		return
	}
	typed := strings.TrimSpace(r.PostFormValue("target"))
	target, err := strconv.Atoi(typed)
	if err != nil {
		target = -1 // refused with the store's own message
	}
	err = a.store.SetGoal(r.Context(), uid, year, target)
	var ref *Refusal
	switch {
	case errors.As(err, &ref):
		a.renderStats(w, r, uid, year, http.StatusUnprocessableEntity, goalDraft{Error: ref.Msg, Value: typed})
		return
	case err != nil:
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, statsURL(year), http.StatusSeeOther)
}

// clearGoal removes the year's goal and goes back to the Stats page.
func (a *App) clearGoal(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	year, ok := a.goalYear(w, r)
	if !ok {
		return
	}
	if err := a.store.ClearGoal(r.Context(), uid, year); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, statsURL(year), http.StatusSeeOther)
}

// goalView is a year's goal: on the Stats page with its form, and as the
// read-only card at the top of the Reading shelf.
type goalView struct {
	Year    int
	Target  int    // 0: no goal
	Text    string // "12 of 30"
	Pace    string // "2 ahead", "goal reached"; "" for none
	Percent int    // the bar, 0–100
	Value   string // the target box
	Max     int
	Error   string
}

// viewGoal draws g as of today, a local time.
func viewGoal(g Goal, today time.Time) goalView {
	v := goalView{Year: g.Year, Target: g.Target, Max: MaxGoal}
	if g.Target > 0 {
		v.Text = strconv.Itoa(g.Done) + " of " + strconv.Itoa(g.Target)
		v.Pace = g.Pace(today)
		v.Percent = min(100, g.Done*100/g.Target)
		v.Value = strconv.Itoa(g.Target)
	}
	return v
}

```

In `internal/apps/books/statspage.go`, replace:

```go
	Years    []yearLink // the year picker
```

with:

```go
	Years    []yearLink // the year picker
	Goal     goalView
```

In `internal/apps/books/books.go`, replace:

```go
	r.HandleFunc("GET /stats", a.stats)
```

with:

```go
	r.HandleFunc("GET /stats", a.stats)
	r.HandleFunc("POST /goal", a.setGoal)
	r.HandleFunc("POST /goal/clear", a.clearGoal)
```

- [ ] **Step 4: Draw the goal**

The goal box sits between the year links and the year's numbers. With no goal the form shows straight away; with one, it waits in a "Change goal" disclosure (opened when a target was refused) beside "Remove goal". `.Shell` is the page's, for the CSRF token: inside `content`, `.` is the `render.Page`.

In `internal/apps/books/templates/stats.html`, replace:

```html
	</nav>

	<section class="books-stats-section" id="books-year"
```

with:

```html
	</nav>

	{{template "goal" (dict "Goal" $d.Goal "Shell" .Shell)}}

	<section class="books-stats-section" id="books-year"
```

In `internal/apps/books/templates/stats.html`, replace:

```html
{{/* stat-tiles takes []statTile. */}}
```

with:

```html
{{/* goal is the year's goal and its form (spec "Stats (B4)": set and
     edited inline, here only — the Reading shelf's card just shows it).
     Plain form posts, so it works without JavaScript. With a goal the form
     sits in a "Change goal" disclosure, opened when a target was refused.
     Pass a dict with Goal (a goalView) and Shell. */}}
{{define "goal"}}
<section class="books-goal" id="books-goal" aria-labelledby="books-goal-head">
	<h2 id="books-goal-head">Goal for {{.Goal.Year}}</h2>
	{{if .Goal.Target}}
	<p class="books-goal-text"><span class="books-goal-count">{{.Goal.Text}}</span>{{with .Goal.Pace}} · {{.}}{{end}}</p>
	<progress class="books-progress-bar" max="100" value="{{.Goal.Percent}}" aria-label="Goal progress">{{.Goal.Percent}}%</progress>
	<div class="books-goal-actions">
		<details class="books-goal-edit"{{if .Goal.Error}} open{{end}}>
			<summary class="button">Change goal</summary>
			{{template "goal-form" .}}
		</details>
		<form method="post" action="/books/goal/clear">
			<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
			<input type="hidden" name="year" value="{{.Goal.Year}}">
			<button type="submit" class="quiet">Remove goal</button>
		</form>
	</div>
	{{else}}
	<p class="books-hint">No goal for {{.Goal.Year}} yet. How many books would you like to read?</p>
	{{template "goal-form" .}}
	{{end}}
</section>
{{end}}

{{/* goal-form sets or changes a goal. novalidate, so the store's message
     shows rather than the browser's. Takes goal's dict. */}}
{{define "goal-form"}}
<form class="books-goal-form" method="post" action="/books/goal" novalidate>
	<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
	<input type="hidden" name="year" value="{{.Goal.Year}}">
	<label for="books-goal-target">Books to read in {{.Goal.Year}}</label>
	<div class="books-goal-row">
		<input id="books-goal-target" name="target" type="number" inputmode="numeric" min="1" max="{{.Goal.Max}}" value="{{.Goal.Value}}"{{if .Goal.Error}} aria-invalid="true" aria-describedby="books-goal-error"{{end}}>
		<button type="submit" class="primary">{{if .Goal.Target}}Save goal{{else}}Set goal{{end}}</button>
	</div>
	{{with .Goal.Error}}<p class="books-field-error" id="books-goal-error" role="alert">{{.}}</p>{{end}}
</form>
{{end}}

{{/* stat-tiles takes []statTile. */}}
```

Append to `internal/ui/static/app.css` (after a blank line):

```css
/* The goal on the Stats page, where it is set and changed. */
.books-goal { padding: var(--s-3); border: var(--border); border-radius: var(--radius); background: var(--c-bg-subtle); }
.books-goal h2 { margin: 0 0 var(--s-2); font-size: var(--fs-lg); }
.books-goal-text { margin: 0; }
.books-goal-count { font-size: var(--fs-xl); font-weight: 600; }
.books-goal .books-progress-bar { margin: var(--s-2) 0 var(--s-3); }
.books-goal-actions { display: flex; flex-wrap: wrap; align-items: flex-start; gap: var(--s-2); }
.books-goal-edit > summary { display: inline-flex; list-style: none; cursor: pointer; }
.books-goal-edit > summary::-webkit-details-marker { display: none; }
.books-goal-edit[open] { flex-basis: 100%; }
.books-goal-form { margin-top: var(--s-2); }
.books-goal-form label { margin: 0 0 var(--s-1); }
.books-goal-row { display: flex; flex-wrap: wrap; gap: var(--s-2); }
.books-goal-row input { width: 6rem; }
```

- [ ] **Step 5: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./internal/apps/books/
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./internal/apps/books/
```
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): set the yearly goal on the Stats page (#490)"
```

---

### Task 6: Goal card on the Reading shelf

**Files:**
- Modify: `internal/apps/books/view.go`, `internal/apps/books/handlers.go`, `internal/apps/books/templates/panes.partial.html`, `internal/ui/static/app.css`
- Test: `internal/apps/books/goal_card_test.go`

**Interfaces:**
- Consumes: `Store.Goal`, `SetGoal` (Task 1); `goalView`, `viewGoal` (Task 5); `renderPanes`, `listView` (existing); test helpers `finishedOn` (Task 4), `noon` (Task 1), `hx` (handlers_test.go).
- Produces:
  - `listView.Goal *goalView`; `func (a *App) shelfGoal(r *http.Request, userID int64, c listCtx) (*goalView, error)` — nil unless the list is the Reading shelf itself (no tag, no series) and this year has a goal
  - `a.books-goal-card` (inside `#books-list`, before the heading) with `.books-goal-card-head` ("2026 goal") and `.books-goal-card-text` ("2 of 30 · 21 behind"), linking to `/books/stats`; no form in it
  - test helper: `setGoal(t, s, userID, year, target)`

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/books/goal_card_test.go`:

```go
package books_test

import (
	"context"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// setGoal sets one of userID's goals through the store.
func setGoal(t *testing.T, s *server, userID int64, year, target int) {
	t.Helper()
	if err := s.Store.SetGoal(context.Background(), userID, year, target); err != nil {
		t.Fatal(err)
	}
}

func TestReadingShelfShowsThisYearsGoal(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10")) // 23 of 30 expected by now
	uid := s.Alice.User.ID
	finishedOn(t, s, uid, "Dune", 600, "2026-02-01")
	finishedOn(t, s, uid, "Emma", 300, "2026-03-01")
	setGoal(t, s, uid, 2026, 30)

	for _, doc := range []*htmlassert.Doc{
		s.Get(t, s.Alice, "/books/"),
		htmlassert.Parse(t, hx(t, s, "/books/?shelf=reading", "books-list")), // a shelf switch brings it too
	} {
		card := doc.MustHave("#books-list .books-goal-card")
		if href, _ := htmlassert.Attr(card, "href"); href != "/books/stats" {
			t.Errorf("goal card links to %q, want the Stats page", href)
		}
		if got := htmlassert.Text(doc.MustHave(".books-goal-card-text")); got != "2 of 30 · 21 behind" {
			t.Errorf("goal card = %q, want 2 of 30 · 21 behind", got)
		}
		if got := htmlassert.Text(doc.MustHave(".books-goal-card-head")); got != "2026 goal" {
			t.Errorf("goal card head = %q", got)
		}
		doc.MustNotHave(".books-goal-card form") // read-only: the goal is set on the Stats page
	}
}

func TestGoalCardIsOnlyOnTheReadingShelf(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	setGoal(t, s, s.Alice.User.ID, 2026, 30)
	for _, path := range []string{"/books/?shelf=want", "/books/?shelf=read", "/books/?shelf=all",
		"/books/?shelf=reading&tag=sf", "/books/?shelf=reading&series=Dune"} {
		s.Get(t, s.Alice, path).MustNotHave(".books-goal-card")
	}
}

func TestGoalCardNeedsAGoalForThisYear(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(noon("2026-10-10"))
	s.Get(t, s.Alice, "/books/").MustNotHave(".books-goal-card")
	setGoal(t, s, s.Alice.User.ID, 2027, 30)
	setGoal(t, s, s.Alice.User.ID, 2025, 30)
	s.Get(t, s.Alice, "/books/").MustNotHave(".books-goal-card")
	setGoal(t, s, s.Bob.User.ID, 2026, 30)
	s.Get(t, s.Alice, "/books/").MustNotHave(".books-goal-card")
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -run 'GoalCard|ReadingShelfShows' -count=1`
Expected: FAIL — `TestReadingShelfShowsThisYearsGoal`: `no element matches "#books-list .books-goal-card"`. The two "no card" tests already pass.

- [ ] **Step 3: Add the card**

The card lives inside `#books-list` so it comes with every list swap — a shelf switch, a filter, and the progress box's out-of-band list.

In `internal/apps/books/view.go`, replace:

```go
	Empty   string
	OOB     bool // swapped out of band, alongside a progress update
}
```

with:

```go
	Empty   string
	OOB     bool      // swapped out of band, alongside a progress update
	Goal    *goalView // this year's goal, at the top of the Reading shelf; nil for none
}
```

In `internal/apps/books/handlers.go`, replace:

```go
	title := listHeading(c)
	var bv bookView
```

with:

```go
	goal, err := a.shelfGoal(r, userID, c)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	title := listHeading(c)
	var bv bookView
```

In `internal/apps/books/handlers.go`, replace:

```go
		Sidebar: viewSidebar(c, counts, tags), List: viewList(items, c, opts.BookID), Book: bv}
```

with:

```go
		Sidebar: viewSidebar(c, counts, tags), List: viewList(items, c, opts.BookID), Book: bv}
	v.List.Goal = goal
```

Append to `internal/apps/books/handlers.go` (after a blank line):

```go
// shelfGoal is the goal card for list c: this year's goal at the top of
// the Reading shelf, when one is set (spec "Stats (B4)"). Read-only — it
// links to the Stats page, where the goal is set (decided 2026-10-09 while
// planning B4). A tag or series list isn't the shelf, so it has none.
func (a *App) shelfGoal(r *http.Request, userID int64, c listCtx) (*goalView, error) {
	if c.Shelf != ShelfReading || c.Tag != "" || c.Series != "" {
		return nil, nil
	}
	today := a.store.now().Local()
	g, err := a.store.Goal(r.Context(), userID, today.Year())
	if err != nil || g.Target == 0 {
		return nil, err
	}
	v := viewGoal(g, today)
	return &v, nil
}
```

In `internal/apps/books/templates/panes.partial.html`, replace:

```html
{{if .OOB}} hx-swap-oob="true"{{end}}>
	<h2 class="books-list-head">{{.Heading}}</h2>
```

with:

```html
{{if .OOB}} hx-swap-oob="true"{{end}}>
	{{with .Goal}}
	<a class="books-goal-card" href="/books/stats">
		<span class="books-goal-card-head">{{.Year}} goal</span>
		<span class="books-goal-card-text">{{.Text}}{{with .Pace}} · {{.}}{{end}}</span>
		<progress class="books-row-bar" max="100" value="{{.Percent}}" aria-hidden="true"></progress>
	</a>
	{{end}}
	<h2 class="books-list-head">{{.Heading}}</h2>
```

Append to `internal/ui/static/app.css` (after a blank line):

```css
/* The goal card at the top of the Reading shelf: read-only, a link to
 * the Stats page. */
.books-goal-card { display: flex; flex-wrap: wrap; align-items: center; gap: var(--s-1) var(--s-2); margin-bottom: var(--s-3); padding: var(--s-2) var(--s-3); border: var(--border); border-radius: var(--radius); background: var(--c-bg-subtle); color: var(--c-text); text-decoration: none; }
.books-goal-card:hover { border-color: var(--c-accent); }
.books-goal-card-head { color: var(--c-text-dim); font-size: var(--fs-xs); text-transform: uppercase; letter-spacing: 0.05em; flex-basis: 100%; }
.books-goal-card-text { font-weight: 600; }
.books-goal-card .books-row-bar { flex: 1 1 100%; width: auto; }
```

- [ ] **Step 4: Run the tests, vet and staticcheck**

```bash
go test ./internal/apps/books/... -count=1
go vet ./internal/apps/books/
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./internal/apps/books/
```
Expected: all pass — including every older handler test: the card is only on the Reading shelf with a goal, which no older test sets.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): goal card on the Reading shelf (#490)"
```

---

### Task 7: A goal for the demo

**Files:**
- Modify: `docs/screenshots/seed/books.go`, `docs/screenshots/seed/seed_test.go`

**Interfaces:**
- Consumes: `Store.SetGoal`, `Store.Goal` (Task 1).
- Produces: nothing for later tasks. The demo's three finished books (56, 40 and 20 days ago) against a goal of 24 make the Stats page and the card show something in any month.

- [ ] **Step 1: Write the failing test**

In `docs/screenshots/seed/seed_test.go`, replace:

```go
	// Flash: decks with cards, a review history and a streak.
```

with:

```go
	// ... and a goal for this year, which the Stats page and the Reading
	// shelf's card show.
	if g, err := bst.Goal(ctx, demo.ID, now.Local().Year()); err != nil || g.Target != 24 {
		t.Errorf("books goal = %+v, %v; want 24 books this year", g, err)
	}

	// Flash: decks with cards, a review history and a streak.
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./docs/screenshots/seed/ -count=1`
Expected: FAIL — `books goal = {Year:2026 Target:0 Done:3}, <nil>; want 24 books this year`.

- [ ] **Step 3: Seed the goal**

In `docs/screenshots/seed/books.go`, replace:

```go
// series, notes on two books and quotes from one, so each shelf, the book
// pane and the search have something to show. Quotes come only from a
// book long out of copyright. Dates are offsets from now, so nothing is
// ever in the future.
```

with:

```go
// series, notes on two books and quotes from one, and a goal for this
// year, so each shelf, the book pane, the search and the Stats page have
// something to show. Quotes come only from a book long out of copyright.
// Dates are offsets from now, so nothing is ever in the future.
```

In `docs/screenshots/seed/books.go`, replace:

```go
	st.SetClock(func() time.Time { return now })
	return nil
}
```

with:

```go
	st.SetClock(func() time.Time { return now })
	return st.SetGoal(ctx, userID, now.Local().Year(), 24)
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
git commit -m "docs(books): seed a reading goal for the demo (#490)"
```

---

### Task 8: User guide and app list

The spec already records B4's decisions: this plan's own PR added them to "Stats (B4)". Only the guide and the app list change here. Screenshots of the Stats page are B5's (spec "Phases").

**Files:**
- Modify: `docs/user/books.md`, `AGENTS.md`

- [ ] **Step 1: Update the guide**

In `docs/user/books.md`, replace:

```markdown
ON Books keeps every update you make. You only see the latest, but they're
what the reading stats will be counted from.
```

with:

```markdown
ON Books keeps every update you make. You only see the latest, but they're
what [your stats](#reading-goal-and-stats) count pages from.
```

In `docs/user/books.md`, replace:

```markdown
## Keyboard shortcuts
```

with:

```markdown
## Reading goal and stats

**Stats**, at the bottom of the sidebar, opens a page of numbers about your
reading. It shows this year to begin with; the years along the top switch
to another one.

For the year you get:

- **Books finished** — every time you finished a book that year. A book you
  read twice counts twice; books you didn't finish don't count.
- **Pages read** — worked out from your progress updates, on the days you
  made them. Going from page 120 to page 180 is 60 pages; going back
  counts nothing. When you finish a book, the pages after your last update
  count on the day you finished, so a book you finished without ever
  updating its progress counts in full. Progress kept in percent — an
  audiobook's, say — turns into pages through the book's page count; a
  book with no page count adds no pages.
- **Average rating** of the books you finished that year.
- How many you read on paper, as ebooks and as audiobooks, and the
  longest and shortest book you finished.
- A chart of the books you finished each month.

Under **All time** are the same three numbers for everything you've read,
and a chart of the books you finished each year. A book you added as read
without a date — imported, say — counts toward All time but not toward any
year.

### A yearly goal

Set a goal on the Stats page: type how many books you'd like to read that
year and click **Set goal**. The page then shows how you're doing — "12 of
30" — and whether you're ahead or behind: by the middle of the year you'd
expect to have read half your goal. **Change goal** and **Remove goal** do
what they say. You can set a goal for next year ahead of time, too.

While this year has a goal, it also shows at the top of the Reading shelf.
Click it to go to your stats.

## Keyboard shortcuts
```

- [ ] **Step 2: Update the app list**

In `AGENTS.md`, replace:

```markdown
Open Library search, progress, ratings and reviews, notes and quotes, and
full-text search, with stats to come),
```

with:

```markdown
Open Library search, progress, ratings and reviews, notes and quotes,
full-text search, and a yearly goal with reading stats),
```

- [ ] **Step 3: Run the full check and commit**

Run the full check from Global Constraints (the help tests load the guide).

```bash
git add docs AGENTS.md
git commit -m "docs(books): goal and stats in the guide (#490)"
```

---

### Task 9: Check it in a browser and open the PR

- [ ] **Step 1: Seed a demo directory and start the server**

```bash
SEED=$(mktemp -d)/books-b4
go run ./docs/screenshots/seed --data-dir $SEED
go build -o $SEED/onsuite ./cmd/onsuite
echo $SEED
```
Add a `.claude/launch.json` configuration named `onsuite-books` (in the main checkout, `/Users/iliaf/src/WEB/on-suite`) with `"runtimeExecutable": "<SEED>/onsuite"`, `"runtimeArgs": ["serve", "--addr", ":8096", "--data-dir", "<SEED>"]` and `"port": 8096`, start it with the preview tools and sign in as `demo` (password in `docs/screenshots/README.md`).

- [ ] **Step 2: Walk through it**

1. Reading shelf: the card at the top says "2026 goal", "3 of 24 · N behind" (N depends on the day: 24 × day-of-year ÷ 365, less 3) with a full-width bar under it; click it — the Stats page opens. Switch to Want to read and back: the card goes and comes back. A tag in the sidebar: no card.
2. Sidebar: **Stats** at the bottom, after the tags.
3. Stats page: the year links (this year highlighted, next year after it), the goal box, three tiles (3 books, pages, "4.7 ★"), "Formats Paper 1 · Ebook 2", Longest "The Remains of the Day · 258 pages" and Shortest "A Wizard of Earthsea · 183 pages", both links to the book; the month chart with month names lined up under the bars; All time with its tiles and a one-bar year chart that is one slot wide, ticked "’26"; each "Figures" disclosure opens a table. Hover a bar: "August: 2 books".
4. Goal: Change goal → type 1001 → Save goal: "Enter a goal from 1 to 1000 books." under the box, the box still open, 1001 still in it. Type 30 → Save: "3 of 30 · …". Remove goal → the form for a new one. Next year's link: "Goal for 2027", set 12 → "0 of 12" and no pace.
5. Turn JavaScript off and do step 4 again: the same.
6. Phone width (375px): no horizontal scroll on the Stats page or the shelf (`document.documentElement.scrollWidth` equals the window width); tiles wrap two to a row; tick labels don't overlap; the card fits; the Stats link shows on the shelves pane.
7. No console errors besides the expected 422 for the refused goal.

Fix anything found (with a test where one can be written), re-run the full check, commit.

- [ ] **Step 3: Remove the launch entry and push**

Remove the `onsuite-books` entry from `.claude/launch.json` (do not commit it), stop the server, then:

```bash
git push -u origin feat/books-b4-goals-stats
env -u GH_TOKEN gh pr create --title "feat(books): ON Books B4 — goals and stats (#490)" --body "$(cat <<'EOF'
B4 of ON Books. A yearly reading goal (`books_goals`, migration 0006), set, changed and removed on a new Stats page (`/books/stats`), with plain forms that work without JavaScript. The page shows a year — books finished, pages read, average rating, format split, longest and shortest book, books by month — and an all-time row with books per year, as server-rendered SVG with a figures table under each chart. The Reading shelf shows this year's goal as a read-only card ("12 of 30 · 2 ahead") linking to Stats. The sidebar has a Stats link.

Pages read follow the spec's "Derived values" (positive deltas between progress rows by local day, the remainder on the finish date); a repeated progress value adds nothing, so #576 doesn't affect them (it stays open).

Spec: docs/superpowers/specs/2026-10-09-on-books-design.md
Plan: docs/superpowers/plans/2026-10-09-on-books-b4-goals-stats.md
Closes #490.
EOF
)"
```
Never merge it.

---

## Self-review

- **Spec coverage.** `books_goals` → Task 1. Year picker, default current year → Task 4 (`parseYear`, `.books-years`). Goal card "N of T" with ahead/behind, expected rounded down → Task 1 (`Expected`, `Pace`, table-tested), Task 5 (Stats page), Task 6 (Reading shelf). Goal set and edited inline → Task 5. Books finished, pages read, average rating, format split, longest and shortest → Task 3 (store), Task 4 (page). All-time row → Tasks 3–4. Charts: books by month, books per year → Task 4. Pages read per "Derived values" (deltas, local day, finish remainder, backwards, percent with and without a page count, re-reads) → Task 2's table, Task 3's store tests. Books finished (re-reads again, DNF never, undated never) → Task 1's `TestGoalCountsTheYearsFinishedReadings`, Task 3. Store tests on real SQLite, handler tests with `apptest.Clock`, local-noon days → every task. User guide → Task 8.
- **Placeholders.** None: every code step has the code; every run step its command and expected result.
- **Types.** `Goal{Year, Target, Done}`, `goalView`/`viewGoal`, `statsView.Goal`, `listView.Goal *goalView`, `renderStats(w, r, userID, year, status, goalDraft)` from Task 5 on, `Stats{Year YearStats; All AllTime}`, `chartView.Ticks`/`Points` are used with the same names and types wherever they appear.

