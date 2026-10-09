# ON Books B1a — Library and shelves (manual entry) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship ON Books as a registered app where you can add books by hand, see them on shelves (Reading · Want to read · Read · Did not finish · All) in a three-pane view, start, finish or abandon a reading, tag books, filter by title or author, and edit or delete a book.

**Architecture:** A new app package `internal/apps/books` shaped like ON Later and ON Focus: `books.go` (App/Meta/Mount), `store.go` (Store, clock, shared errors), `book.go` (book details, validation, ISBN), `library.go` (book CRUD, shelves, list), `reading.go` (start/finish/DNF), `tag.go` (tags), `spine.go` (generated spine colour), `view.go` (view models), `handlers.go`, `form.go`, `actions.go` (HTTP). The panes follow ON Reader's three-pane layout (`internal/apps/reader/templates/panes.partial.html`): htmx swaps targeted at `#books-list`, `#books-book` or `#books-panes`, `.pane-gutter` dividers with widths kept on `<html>`, and two drill-down checkboxes for narrow screens. A shelf is never stored: it is derived from the book's latest reading.

**Tech Stack:** Go 1.22+ `ServeMux`, `html/template`, htmx 2.0.10 (vendored), SQLite via `modernc.org/sqlite`, plain ES5-style JavaScript (no build step).

**Spec:** [docs/superpowers/specs/2026-10-09-on-books-design.md](../specs/2026-10-09-on-books-design.md) — "Architecture", "Data model" (`books_books`, `books_readings`, `books_tags`, `books_book_tags`, "Derived values"), "Screens" (Layout, Progress and finishing — the B1 parts), "Keyboard", "Errors", "Phases" (B1 row).

## Global Constraints

- App ID `books`, name `ON Books`, summary `Keep track of what you read and what you thought of it.`, `Order: 45`; tables prefixed `books_`; routes under `/books/`.
- Shelves, in sidebar order: `reading` "Reading", `want` "Want to read", `read` "Read", `dnf` "Did not finish", `all` "All books". Default shelf `reading`.
- Reading statuses: `reading`, `finished`, `dnf`. At most one `reading` reading per book (store check + partial unique index).
- Shelf of a book: no readings → `want`; else the latest reading (by `created_at`, then `id`) decides: `reading` → `reading`, `finished` → `read`, `dnf` → `dnf`.
- Calendar dates (`started_on`, `finished_on`) are `YYYY-MM-DD` in the server's local day (`time.Local`, #424), never in the future. Finish/DNF dates may not be before the reading's start.
- Limits: title and subtitle ≤ 300 characters (title required); authors ≤ 300; series name ≤ 200; series number ≤ 10 (needs a series name); description ≤ 10 000; year 1–9999 or empty; pages 1–100 000 or empty; ISBN empty or a valid ISBN-10/13 (stored as ISBN-13).
- Tags: comma-separated, lowercased, ≤ 40 characters each, ≤ 20 per book (mirrors ON Later's `ParseTags`). Unused tags are deleted.
- Every `user_id` column: `REFERENCES users (id) ON DELETE CASCADE`. Timestamps TEXT via `db.FormatTime`. Tables `STRICT`.
- Missing or someone else's book → 404. Form validation → 422 re-render with messages next to fields. A refused action (bad date, already reading) → the panes re-render with a banner: 200 for htmx, 422 without JavaScript.
- CSP: no inline `<script>`, no `style=""`. Spine colours arrive as `swatch-c-<name>` classes (the suite palette: `teal blue purple pink coral amber green gray`).
- App CSS goes in `internal/ui/static/app.css` in a new "ON Books" section; every class prefixed `books-` (plus shared `pane-gutter`, `toolbar-btn`, `outline-menu*`, `swatch-c-*`, `notice`, `field`, `button`, `empty`, `dialog-actions`).
- URLs that carry the list context are built whole in Go (`listCtx.ListURL/BookURL/EditURL`) and dropped into `href`/`hx-get` as one value — never `?{{.Query}}`, which `html/template` would percent-encode.
- Full check must stay green on every commit:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- Work on branch `feat/books-b1a-library` in a worktree, never on `main`. Open the PR with `env -u GH_TOKEN gh …`; never merge.

## Lessons from earlier plans (read before starting)

- staticcheck U1000 fails on an unexported helper added before its first caller — every helper here is introduced in the task that first uses it.
- `internal/htmlassert` supports one qualifier per selector (`input#books-book-open` is fine, `input#x[checked]` is not). Descendant selectors (`.books-side a`) work. Use `htmlassert.Attr` for a second condition.
- `go vet` rejects unkeyed composite literals of another package's struct types in `_test` packages — always write `books.BookInput{Title: …}`.
- Keep `<dialog>` elements out of `.stack` containers (`.stack > * + *` breaks a modal's `margin:auto`).
- `internal/platform/help/handlers_test.go` pins the help sidebar link count.
- This plan's code was trial-run in a scratch worktree on 2026-10-09 (Tasks 1–9 applied as written): gofmt, vet, staticcheck and `go test ./... -race` all green, and the panes, Finish flow and phone layout checked in a browser.

## Deviations from the spec (decided while planning)

- **B1 is split in two PRs.** B1a (this plan): everything in #487 except Open Library and stored covers — books are added by hand and every book shows its generated spine. B1b: Open Library search, cover fetch/upload/URL, `books_covers`. The spec's phase table is updated in Task 9.
- **Add book is a page** (`/books/new`), not a dialog, in B1a: with no search there is nothing for a dialog to hold. B1b adds the search dialog in front of it; picking a result opens this same page pre-filled.
- **Text columns are `NOT NULL DEFAULT ''`** instead of nullable (subtitle, authors, series name/number, description, review, OL ids). Only `year`, `pages`, `isbn13`, `rating`, `format` and the dates are nullable. Same meaning, simpler scanning.
- **Finish and Did not finish are `<details>` disclosures** holding the date field (PATTERNS.md "No-JS `<details>` disclosure menu"), not dialogs: they work without JavaScript. The rating in the Finish step and the optional DNF page come with B2.
- **List navigation keeps the open book.** Changing shelf, tag or filter swaps only the list (and the sidebar out of band); the book pane stays as it was. books.js moves the row highlight.
- The list pane's sort for *Reading* is by start date until B2 adds progress ("latest progress" in the spec).
- The `p` shortcut (focus the progress input) comes with B2.

## File map

| File | Responsibility |
|---|---|
| `internal/apps/books/books.go` | `App`, `Meta`, `Templates`, `Mount`, script route |
| `internal/apps/books/store.go` | `ID`, `Migrations`, errors, `Refusal`, `Store`, clock, `Today`, `touch`, `checkDay`, time wrappers |
| `internal/apps/books/book.go` | `BookInput`, `Normalize`, `Validate`, `FieldErrors`, `ValidationError`, `ISBN13` |
| `internal/apps/books/library.go` | `Shelf`, `Status`, `Reading`, `Book`, `NewBook`, `Create`, `Get`, `Update`, `Delete`, `ListItem`, `ListQuery`, `List`, `ShelfCounts` |
| `internal/apps/books/reading.go` | `StartReading`, `FinishReading`, `MarkDNF`, `ShowDay` |
| `internal/apps/books/tag.go` | `ParseTags`, tag linking and clean-up, `SetTags`, `BookTags`, `TagNames` |
| `internal/apps/books/spine.go` | `Colors`, `SpineColor` |
| `internal/apps/books/view.go` | `listCtx`, shelf labels, sidebar/list/book view models |
| `internal/apps/books/handlers.go` | helpers, `index`, `book`, `renderPanes` |
| `internal/apps/books/form.go` | add/edit form parsing, view and handlers |
| `internal/apps/books/actions.go` | start, finish, DNF, tags, delete |
| `internal/apps/books/migrations/0001_library.sql` | schema |
| `internal/apps/books/templates/{index,form}.html`, `panes.partial.html` | pages and shared blocks |
| `internal/apps/books/static/books.js` | delete confirmation, resizable panes, keyboard, row highlight |
| `internal/apps/books/*_test.go` | tests |
| `internal/ui/static/app.css` | "ON Books" section |
| `cmd/onsuite/main.go`, `cmd/onsuite/database_test.go` | registration and pinned app lists |
| `internal/arch/arch_test.go`, `internal/ui/templates_test.go`, `internal/ui/icons.go`, `internal/ui/icons_test.go` | platform lists |
| `internal/platform/help/pages.go`, `internal/platform/help/handlers_test.go`, `docs/user/books.md`, `docs/user/index.md` | user guide |
| `docs/screenshots/seed/{seed.go,books.go,seed_test.go}` | demo books |
| `AGENTS.md`, `docs/developers/index.md`, `docs/developers/repository-layout.md`, the spec | app lists, phase table |

---

### Task 0: Worktree and branch

- [ ] **Step 1: Create the worktree**

```bash
cd /Users/iliaf/src/WEB/on-suite
git fetch origin
git worktree add ../on-suite-books-b1a -b feat/books-b1a-library origin/main
cd ../on-suite-books-b1a
go build ./cmd/onsuite
```
Expected: builds with no output. All later commands run in `../on-suite-books-b1a`.

---

### Task 1: App skeleton, schema and registration

**Files:**
- Create: `internal/apps/books/books.go`, `store.go`, `handlers.go`, `main_test.go`, `handlers_test.go`, `store_test.go`
- Create: `internal/apps/books/migrations/0001_library.sql`
- Create: `internal/apps/books/templates/index.html`, `internal/apps/books/static/books.js`
- Create: `docs/user/books.md`
- Modify: `cmd/onsuite/main.go`, `cmd/onsuite/database_test.go:54-55,163-164`, `internal/arch/arch_test.go:~581`, `internal/ui/templates_test.go:12-14`, `internal/ui/icons.go`, `internal/ui/icons_test.go:11,35,45`, `internal/platform/help/pages.go:~38`, `internal/platform/help/handlers_test.go:31-32`, `docs/user/index.md`, `docs/screenshots/seed/seed.go:73`

**Interfaces:**
- Produces: `books.ID = "books"`, `books.Migrations() fs.FS`, `books.New() *App`, `books.NewStore(*sql.DB) *Store`, `(*Store).SetClock(func() time.Time)`, `(*Store).Today() string`, `books.ErrNotFound`, `books.ErrInvalid`, `(*App).userID`. Template page `books/index`. Test fixture `newFixture(t) *fixture` with fields `store *books.Store`, `db *sql.DB`, `alice`, `bob auth.User`, `now time.Time` (2026-10-09 15:00 UTC = 2026-10-10 02:00 in Melbourne). Test server type `server = apptest.Server[*books.Store]`, `newServer(t) *server`.

- [ ] **Step 1: Write the failing handler test**

`internal/apps/books/main_test.go`:

```go
package books_test

import (
	"os"
	"testing"
	"time"
	_ "time/tzdata" // LoadLocation must not depend on the test machine's zoneinfo
)

// TestMain pins the process's local zone to one well east of UTC, as
// Reader's and Flash's tests do, so the local day ON Books dates readings
// with (#424) differs from the UTC day and the tests prove which one is used.
func TestMain(m *testing.M) {
	loc, err := time.LoadLocation("Australia/Melbourne")
	if err != nil {
		panic(err)
	}
	time.Local = loc
	os.Exit(m.Run())
}
```

`internal/apps/books/handlers_test.go`:

```go
package books_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/apptest"
)

type server = apptest.Server[*books.Store]

// newServer mounts ON Books with its own store as the test's handle.
func newServer(t *testing.T) *server {
	t.Helper()
	return apptest.NewServer(t, books.New(), books.NewStore)
}

func TestBooksRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/books/", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /books/ anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

func TestIndexRenders(t *testing.T) {
	s := newServer(t)
	s.Get(t, s.Alice, "/books/") // fails the test unless 200
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — no Go files / package not found.

- [ ] **Step 3: Write the migration**

`internal/apps/books/migrations/0001_library.sql`:

```sql
-- One row per book a user keeps. Details are typed in (or, from B1b, copied
-- from Open Library once at save time) and never synced. Text columns are ''
-- when unknown; numbers and the ISBN are NULL. isbn13 is always 13 digits:
-- an ISBN-10 is converted on input. rating and review are set from B2.
CREATE TABLE books_books (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title         TEXT    NOT NULL,
    subtitle      TEXT    NOT NULL DEFAULT '',
    authors       TEXT    NOT NULL DEFAULT '',
    year          INTEGER,
    pages         INTEGER,
    isbn13        TEXT,
    ol_work_id    TEXT    NOT NULL DEFAULT '',
    ol_edition_id TEXT    NOT NULL DEFAULT '',
    description   TEXT    NOT NULL DEFAULT '',
    series_name   TEXT    NOT NULL DEFAULT '',
    series_number TEXT    NOT NULL DEFAULT '',
    rating        INTEGER CHECK (rating BETWEEN 1 AND 5),
    review        TEXT    NOT NULL DEFAULT '',
    added_at      TEXT    NOT NULL,
    updated_at    TEXT    NOT NULL
) STRICT;

CREATE INDEX books_books_user_idx ON books_books (user_id, added_at);

-- One row per time through a book. The latest reading decides the book's
-- shelf (no readings = Want to read), so the shelf is never stored. Dates
-- are YYYY-MM-DD in the server's local day; both may be NULL for readings
-- imported without dates (B5). format is set from B2.
CREATE TABLE books_readings (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id     INTEGER NOT NULL REFERENCES books_books (id) ON DELETE CASCADE,
    status      TEXT    NOT NULL CHECK (status IN ('reading', 'finished', 'dnf')),
    format      TEXT    CHECK (format IN ('paper', 'ebook', 'audio')),
    started_on  TEXT,
    finished_on TEXT,
    created_at  TEXT    NOT NULL,
    CHECK (status <> 'reading' OR finished_on IS NULL)
) STRICT;

CREATE INDEX books_readings_book_idx ON books_readings (book_id, created_at);

-- A book is read once at a time.
CREATE UNIQUE INDEX books_readings_one_active ON books_readings (book_id) WHERE status = 'reading';

-- Tags, as in ON Later: lowercase names, unique per user, removed when the
-- last book loses them.
CREATE TABLE books_tags (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name    TEXT    NOT NULL,
    UNIQUE (user_id, name)
) STRICT;

CREATE TABLE books_book_tags (
    book_id INTEGER NOT NULL REFERENCES books_books (id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES books_tags (id) ON DELETE CASCADE,
    PRIMARY KEY (book_id, tag_id)
) STRICT;

CREATE INDEX books_book_tags_tag_idx ON books_book_tags (tag_id);
```

- [ ] **Step 4: Write `store.go` (skeleton — queries come in Tasks 3 and 4)**

```go
// Package books implements ON Books, a private reading log: what you are
// reading, what you have read, what you want to read and what you gave up
// on. Spec: docs/superpowers/specs/2026-10-09-on-books-design.md.
package books

import (
	"database/sql"
	"embed"
	"errors"
	"io/fs"
	"time"
)

// ID is the app id: URL prefix, migration namespace, table prefix.
const ID = "books"

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations is this app's schema, for the platform and for store tests.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		panic("books: embedded migrations missing: " + err.Error()) // unreachable
	}
	return sub
}

var (
	// ErrNotFound is a missing row or somebody else's — indistinguishable
	// on purpose, so a handler answers 404 for both.
	ErrNotFound = errors.New("books: not found")
	// ErrInvalid is a request the store refuses.
	ErrInvalid = errors.New("books: invalid")
)

// dayLayout is a calendar date column's format (AGENTS.md: "Date-only
// columns use 2006-01-02").
const dayLayout = "2006-01-02"

// Store is every SQL query ON Books runs.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// NewStore returns a store on handle reading the real clock in UTC.
func NewStore(handle *sql.DB) *Store {
	return &Store{db: handle, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the time source, for tests.
func (st *Store) SetClock(now func() time.Time) { st.now = now }

// Today is the server's local date, YYYY-MM-DD: days follow time.Local,
// set by TZ, everywhere in the suite (#424).
func (st *Store) Today() string { return st.now().Local().Format(dayLayout) }
```

- [ ] **Step 5: Write `books.go`**

```go
package books

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

var _ app.App = (*App)(nil)

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed static/*.js
var scriptFiles embed.FS

// App is ON Books.
type App struct {
	store *Store
	deps  app.Deps
}

// New returns the app for registration in cmd/onsuite.
func New() *App { return &App{} }

func (a *App) Meta() app.Meta {
	return app.Meta{
		ID:      ID,
		Name:    "ON Books",
		Summary: "Keep track of what you read and what you thought of it.",
		Order:   45,
	}
}

func (a *App) Migrations() fs.FS { return Migrations() }

func (a *App) Templates() fs.FS {
	sub, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		panic("books: embedded templates missing: " + err.Error()) // unreachable
	}
	return sub
}

func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	a.store = NewStore(deps.DB)
	if deps.Now != nil {
		a.store.SetClock(deps.Now)
	}
	r.HandleFunc("GET /{$}", a.index)
	r.HandleFunc("GET /books.js", a.script("books.js"))
}

// script serves an embedded script behind the same sign-in requirement as
// every other route, as ON Later's later.js is.
func (a *App) script(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, scriptFiles, "static/"+name)
	}
}
```

- [ ] **Step 6: Write `handlers.go` with a placeholder index**

```go
package books

import (
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// userID is the signed-in user; every route is registered with HandleFunc.
func (a *App) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u, ok := web.UserFrom(r.Context())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusUnauthorized)
		return 0, false
	}
	return u.ID, true
}

// index is a placeholder until Task 5 draws the panes.
func (a *App) index(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	page := a.deps.Page(r, "Reading")
	if err := a.deps.Render.Page(w, http.StatusOK, "books/index", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
```

- [ ] **Step 7: Write the template and the script placeholder**

`internal/apps/books/templates/index.html`:

```html
{{define "head"}}<script src="/books/books.js" defer></script>{{end}}

{{define "content"}}<h1>ON Books</h1>{{end}}
```

`internal/apps/books/static/books.js`:

```js
// ON Books' script (Tasks 7 and 8 of the B1a plan fill it in).
"use strict";
```

- [ ] **Step 8: Write the user guide stub**

`docs/user/books.md`:

```markdown
# ON Books

ON Books is a private reading log: what you're reading, what you've read,
what you want to read next and what you gave up on. No friends, no feed —
just your own books.
```
(Task 9 writes the rest of the B1a guide.)

In `docs/user/index.md`, add a bullet for ON Books to the app list (after ON Later's), in the same shape as the others:

```markdown
- **[ON Books](books.md)** — a private reading log: what you're reading, what you've read and what's next.
```
and to the guide list near the end (after ON Later's):

```markdown
- [ON Books](books.md) — keeping track of your reading.
```

- [ ] **Step 9: Register the app and update every pinned list**

`cmd/onsuite/main.go` — add the import `"github.com/iliafrenkel/on-suite/internal/apps/books"` (first in the apps group) and `books.New(),` as the first line of `registeredApps()`.

`cmd/onsuite/database_test.go`:
- line ~54: `if len(ids) != 6 {` with message `"NavItems() = %v, want exactly books, flash, focus, later, notes, and paste"`.
- line ~163: `if len(registry.NavItems()) != 7 {` with message `"NavItems() has %d entries, want 7 (books, flash, focus, later, notes, paste, reader)"`.

`internal/arch/arch_test.go` — in `TestAppsReadTheirStoreClock`'s `want`, add `"internal/apps/books/store.go",` as the first entry (the list is sorted).

`internal/ui/templates_test.go:12-14`:

```go
// appClassPattern matches a class name owned by one app's CSS section in
// app.css (reader-*, notes-*, paste-*, flash-*, later-*, focus-*, books-*).
var appClassPattern = regexp.MustCompile(`^(reader|notes|paste|flash|later|focus|books)-`)
```

`internal/ui/icons.go` — add after the `"later"` entry:

```go
	// An open book: two pages meeting at the spine.
	"books": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="none"/>
		<path d="M12 8.5C10.4 7.3 8.6 6.9 6.5 7v9.5c2.1-.1 3.9.3 5.5 1.5 1.6-1.2 3.4-1.6 5.5-1.5V7c-2.1-.1-3.9.3-5.5 1.5zM12 8.5V18" fill="none" stroke="var(--c-accent)" stroke-width="1.5" stroke-linejoin="round"/>
	</svg>`,
```

`internal/ui/icons_test.go` — add `"books"` to each of the three `[]string{…}` id lists (lines 11, 35, 45):

```bash
perl -pi -e 's/"later", "focus"\}/"later", "focus", "books"}/' internal/ui/icons_test.go
grep -c '"books"' internal/ui/icons_test.go
```
Expected: `3`.

`internal/platform/help/pages.go` — in `order`, add `{"books", "ON Books"},` after `{"later", "ON Later"},`.

`internal/platform/help/handlers_test.go:31-32` — the sidebar now has 9 links:

```go
	if got := len(d.QueryAll(".help-nav a")); got != 9 {
		t.Errorf("sidebar has %d links, want 9", got)
```

`docs/screenshots/seed/seed.go:73` — add `books.New(),` first in `app.NewRegistry(...)` and the import `"github.com/iliafrenkel/on-suite/internal/apps/books"`. Seeding demo books comes in Task 9.

- [ ] **Step 10: Add the store test fixture and a schema test**

`internal/apps/books/store_test.go`:

```go
package books_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// fixture is a migrated database with two users and a clock the test moves.
// The default now, 15:00 UTC on 9 October 2026, is already 10 October in
// Melbourne (TestMain's zone), so a test that sees "2026-10-10" knows the
// local day was used.
type fixture struct {
	store *books.Store
	db    *sql.DB
	alice auth.User
	bob   auth.User
	now   time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	appMigrations, err := db.Collect(books.ID, books.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(migrations, appMigrations...)); err != nil {
		t.Fatal(err)
	}

	users := auth.NewStore(handle)
	alice, err := users.CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := users.CreateUser(ctx, "bob", apptest.PasswordHash, false)
	if err != nil {
		t.Fatal(err)
	}
	st := books.NewStore(handle)
	f := &fixture{store: st, db: handle, alice: alice, bob: bob, now: time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)}
	st.SetClock(func() time.Time { return f.now })
	return f
}

func TestTodayIsTheLocalDay(t *testing.T) {
	f := newFixture(t)
	if got := f.store.Today(); got != "2026-10-10" {
		t.Errorf("Today() = %q, want 2026-10-10 (Melbourne, not UTC)", got)
	}
}

func TestSchemaAllowsOneReadingInProgressPerBook(t *testing.T) {
	f := newFixture(t)
	now := db.FormatTime(f.now)
	res, err := f.db.Exec(`INSERT INTO books_books (user_id, title, added_at, updated_at) VALUES (?, 'x', ?, ?)`,
		f.alice.ID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	insert := `INSERT INTO books_readings (book_id, status, created_at) VALUES (?, 'reading', ?)`
	if _, err := f.db.Exec(insert, id, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(insert, id, now); err == nil {
		t.Fatal("inserted a second reading in progress, want a unique-index failure")
	}
}
```
(`f.bob` is first read in Task 3; a field set in a composite literal doesn't trip staticcheck.)

- [ ] **Step 11: Run the focused tests, then the full check**

Run: `go test ./internal/apps/books/... ./cmd/onsuite/... ./internal/arch/... ./internal/ui/... ./internal/platform/help/... ./docs/... -count=1`
Expected: PASS (including `TestEveryUserColumnCascadesFromUsers` and `TestBuildStackServesHelpForEveryRegisteredApp`).

Then run the full check from Global Constraints. Expected: no output from gofmt, all tests PASS.

- [ ] **Step 12: Commit**

```bash
git add -A
git commit -m "feat(books): app skeleton, schema and registration (#487)"
```

---

### Task 2: Book details, validation and ISBNs

**Files:**
- Create: `internal/apps/books/book.go`
- Test: `internal/apps/books/book_test.go`

**Interfaces:**
- Produces:
  - `const MaxTitleRunes = 300`, `MaxAuthorsRunes = 300`, `MaxSeriesRunes = 200`, `MaxSeriesNumberRunes = 10`, `MaxDescriptionRunes = 10000`, `MaxPages = 100000`, `MaxYear = 9999`
  - `type BookInput struct { Title, Subtitle, Authors string; Year, Pages int; ISBN, SeriesName, SeriesNumber, Description string }` (0 = unknown for Year/Pages)
  - `func (in BookInput) Normalize() BookInput`
  - `type FieldErrors map[string]string` — keys are form field names: `title subtitle authors year pages isbn series_name series_number description`
  - `func (in BookInput) Validate() FieldErrors` (call on normalized input; nil when valid)
  - `type ValidationError struct{ Fields FieldErrors }` with `Error() string` and `Unwrap() error` returning `ErrInvalid`
  - `func ISBN13(s string) (string, bool)`

- [ ] **Step 1: Write the failing tests**

`internal/apps/books/book_test.go`:

```go
package books_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

func TestNormalizeTidiesFields(t *testing.T) {
	got := books.BookInput{
		Title:        "  The \n Dispossessed ",
		Authors:      "Ursula K.\tLe Guin",
		ISBN:         " 0-306-40615-2 ",
		SeriesName:   " Hainish  Cycle ",
		SeriesNumber: " 6 ",
		Description:  "  One.\r\nTwo.  ",
	}.Normalize()
	want := books.BookInput{
		Title:        "The Dispossessed",
		Authors:      "Ursula K. Le Guin",
		ISBN:         "9780306406157",
		SeriesName:   "Hainish Cycle",
		SeriesNumber: "6",
		Description:  "One.\nTwo.",
	}
	if got != want {
		t.Errorf("Normalize() = %+v\nwant %+v", got, want)
	}
}

func TestNormalizeKeepsAnInvalidISBNForValidateToReport(t *testing.T) {
	if got := (books.BookInput{ISBN: " 12345 "}).Normalize().ISBN; got != "12345" {
		t.Errorf("ISBN = %q, want 12345", got)
	}
}

func TestValidateAcceptsAMinimalBook(t *testing.T) {
	if errs := (books.BookInput{Title: "Piranesi"}).Normalize().Validate(); errs != nil {
		t.Errorf("Validate() = %v, want nil", errs)
	}
}

func TestValidateRejectsBadInput(t *testing.T) {
	long := func(n int) string { return strings.Repeat("é", n) }
	tests := []struct {
		name  string
		in    books.BookInput
		field string
	}{
		{"no title", books.BookInput{Title: "  "}, "title"},
		{"long title", books.BookInput{Title: long(301)}, "title"},
		{"long subtitle", books.BookInput{Title: "x", Subtitle: long(301)}, "subtitle"},
		{"long authors", books.BookInput{Title: "x", Authors: long(301)}, "authors"},
		{"negative year", books.BookInput{Title: "x", Year: -1}, "year"},
		{"far year", books.BookInput{Title: "x", Year: 10000}, "year"},
		{"negative pages", books.BookInput{Title: "x", Pages: -5}, "pages"},
		{"huge pages", books.BookInput{Title: "x", Pages: 100001}, "pages"},
		{"bad isbn", books.BookInput{Title: "x", ISBN: "12345"}, "isbn"},
		{"long series", books.BookInput{Title: "x", SeriesName: long(201)}, "series_name"},
		{"long number", books.BookInput{Title: "x", SeriesName: "s", SeriesNumber: long(11)}, "series_number"},
		{"number without series", books.BookInput{Title: "x", SeriesNumber: "2"}, "series_name"},
		{"long description", books.BookInput{Title: "x", Description: long(10001)}, "description"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.in.Normalize().Validate()
			if errs[tt.field] == "" {
				t.Errorf("Validate() = %v, want a message for %q", errs, tt.field)
			}
		})
	}
}

func TestValidationErrorIsErrInvalid(t *testing.T) {
	var err error = &books.ValidationError{Fields: books.FieldErrors{"title": "x"}}
	if !errors.Is(err, books.ErrInvalid) {
		t.Error("ValidationError does not unwrap to ErrInvalid")
	}
}

func TestISBN13(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"9780306406157", "9780306406157", true},
		{"978-0-306-40615-7", "9780306406157", true},
		{"0306406152", "9780306406157", true},
		{"0-8044-2957-X", "9780804429573", true},
		{"080442957x", "9780804429573", true},
		{"9780306406158", "", false}, // wrong check digit
		{"0306406153", "", false},    // wrong check digit
		{"1234567890123", "", false}, // not a 978/979 prefix
		{"X306406152", "", false},    // X only as the last ISBN-10 digit
		{"978030640615", "", false},  // twelve digits
		{"ISBN 0306406152", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := books.ISBN13(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ISBN13(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -run 'Normalize|Validat|ISBN' -count=1`
Expected: FAIL — `undefined: books.BookInput`.

- [ ] **Step 3: Write `book.go`**

```go
package books

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits on a book's fields, in characters after Normalize (spec "Data
// model"; this is a reading log, not a catalogue).
const (
	MaxTitleRunes        = 300
	MaxAuthorsRunes      = 300
	MaxSeriesRunes       = 200
	MaxSeriesNumberRunes = 10
	MaxDescriptionRunes  = 10000
	MaxPages             = 100000
	MaxYear              = 9999
)

// BookInput is a book's own details, as the form (and, from B1b, Open
// Library) supplies them. Year and Pages are 0 when unknown.
type BookInput struct {
	Title, Subtitle, Authors string
	Year, Pages              int
	// ISBN is as typed until Normalize turns a valid one into ISBN-13.
	ISBN                     string
	SeriesName, SeriesNumber string
	Description              string
}

// FieldErrors maps a form field name to what is wrong with it.
type FieldErrors map[string]string

// ValidationError is a book the store refuses, with a message per field.
type ValidationError struct{ Fields FieldErrors }

func (e *ValidationError) Error() string { return fmt.Sprintf("books: invalid book: %v", e.Fields) }

// Unwrap makes a ValidationError an ErrInvalid for errors.Is.
func (e *ValidationError) Unwrap() error { return ErrInvalid }

// Normalize tidies what was typed: one-line fields lose newlines and runs
// of spaces, the description keeps its line breaks, and a valid ISBN-10 or
// ISBN-13 becomes plain ISBN-13 digits. An invalid ISBN is left (trimmed)
// for Validate to report.
func (in BookInput) Normalize() BookInput {
	in.Title = oneLine(in.Title)
	in.Subtitle = oneLine(in.Subtitle)
	in.Authors = oneLine(in.Authors)
	in.SeriesName = oneLine(in.SeriesName)
	in.SeriesNumber = oneLine(in.SeriesNumber)
	in.ISBN = strings.TrimSpace(in.ISBN)
	if isbn, ok := ISBN13(in.ISBN); ok {
		in.ISBN = isbn
	}
	in.Description = strings.TrimSpace(strings.ReplaceAll(in.Description, "\r\n", "\n"))
	return in
}

// oneLine turns control characters into spaces and collapses whitespace.
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// Validate checks normalized input; nil means it is fine.
func (in BookInput) Validate() FieldErrors {
	errs := FieldErrors{}
	tooLong := func(field, value string, max int) {
		if utf8.RuneCountInString(value) > max {
			errs[field] = fmt.Sprintf("Keep it to %d characters or fewer.", max)
		}
	}
	if in.Title == "" {
		errs["title"] = "Enter the book's title."
	} else {
		tooLong("title", in.Title, MaxTitleRunes)
	}
	tooLong("subtitle", in.Subtitle, MaxTitleRunes)
	tooLong("authors", in.Authors, MaxAuthorsRunes)
	if in.Year < 0 || in.Year > MaxYear {
		errs["year"] = fmt.Sprintf("Enter a year from 1 to %d, or leave it empty.", MaxYear)
	}
	if in.Pages < 0 || in.Pages > MaxPages {
		errs["pages"] = fmt.Sprintf("Enter a page count from 1 to %d, or leave it empty.", MaxPages)
	}
	if in.ISBN != "" {
		if _, ok := ISBN13(in.ISBN); !ok {
			errs["isbn"] = "That isn't a valid ISBN-10 or ISBN-13."
		}
	}
	tooLong("series_name", in.SeriesName, MaxSeriesRunes)
	tooLong("series_number", in.SeriesNumber, MaxSeriesNumberRunes)
	if in.SeriesNumber != "" && in.SeriesName == "" {
		errs["series_name"] = "Name the series this number belongs to."
	}
	tooLong("description", in.Description, MaxDescriptionRunes)
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// ISBN13 returns s as a 13-digit ISBN if, once spaces and hyphens are
// dropped, it is a valid ISBN-13 (978/979 prefix, right check digit) or a
// valid ISBN-10, which is converted (spec: "ISBN-10 converted on input").
func ISBN13(s string) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ' || r == '-':
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == 'X' || r == 'x':
			b.WriteByte('X')
		default:
			return "", false
		}
	}
	d := b.String()
	switch len(d) {
	case 13:
		if strings.Contains(d, "X") || !(strings.HasPrefix(d, "978") || strings.HasPrefix(d, "979")) {
			return "", false
		}
		if checkDigit13(d[:12]) != d[12] {
			return "", false
		}
		return d, true
	case 10:
		if strings.Contains(d[:9], "X") {
			return "", false
		}
		sum := 0
		for i := 0; i < 10; i++ {
			v := int(d[i] - '0')
			if d[i] == 'X' {
				v = 10
			}
			sum += v * (10 - i)
		}
		if sum%11 != 0 {
			return "", false
		}
		body := "978" + d[:9]
		return body + string(checkDigit13(body)), true
	}
	return "", false
}

// checkDigit13 is the ISBN-13 check digit of twelve digits: weights 1 and 3
// alternating, then whatever brings the sum to a multiple of ten.
func checkDigit13(twelve string) byte {
	sum := 0
	for i := 0; i < 12; i++ {
		v := int(twelve[i] - '0')
		if i%2 == 1 {
			v *= 3
		}
		sum += v
	}
	return byte('0' + (10-sum%10)%10)
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/apps/books/... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/books/book.go internal/apps/books/book_test.go
git commit -m "feat(books): book details, validation and ISBN-13 (#487)"
```

---

### Task 3: Store — books and tags

**Files:**
- Modify: `internal/apps/books/store.go` (time wrappers, `Refusal`, `checkDay`, `touch`)
- Create: `internal/apps/books/library.go`, `internal/apps/books/tag.go`
- Test: `internal/apps/books/library_test.go`, `internal/apps/books/tag_test.go`

**Interfaces:**
- Consumes: `BookInput`, `Normalize`, `Validate`, `ValidationError` (Task 2); `Store`, `Today`, `dayLayout` (Task 1).
- Produces:
  - `type Shelf string`; `ShelfReading = "reading"`, `ShelfWant = "want"`, `ShelfRead = "read"`, `ShelfDNF = "dnf"`, `ShelfAll = "all"`; `var Shelves = []Shelf{ShelfReading, ShelfWant, ShelfRead, ShelfDNF, ShelfAll}`; `func ParseShelf(s string) (Shelf, bool)`
  - `type Status string`; `StatusReading = "reading"`, `StatusFinished = "finished"`, `StatusDNF = "dnf"`
  - `type Reading struct { ID int64; Status Status; Format, StartedOn, FinishedOn string }`
  - `type Book struct { ID int64; BookInput; Rating int; Review string; Tags []string; Shelf Shelf; Latest Reading; AddedAt, UpdatedAt time.Time }`
  - `type NewBook struct { BookInput; Shelf Shelf; FinishedOn string; Tags []string }`
  - `(*Store).Create(ctx, userID int64, nb NewBook) (int64, error)`, `Get(ctx, userID, id int64) (Book, error)`, `Update(ctx, userID, id int64, in BookInput) error`, `Delete(ctx, userID, id int64) error`
  - `type Refusal struct{ Msg string }` (unwraps to `ErrInvalid`; `Msg` is shown to people)
  - `func ParseTags(raw string) []string`, `const MaxTagRunes = 40`, `const MaxTags = 20`
  - `(*Store).SetTags(ctx, userID, id int64, names []string) error`, `BookTags(ctx, userID, id int64) ([]string, error)`, `TagNames(ctx, userID int64) ([]string, error)`
  - unexported, for Task 4: `latestJoin`, `shelfExpr` SQL fragments; `(*Store).touch(ctx, tx, userID, id) error`; `(*Store).checkDay(day string) error`; `formatTime`, `parseTime`.

- [ ] **Step 1: Write the failing tests**

`internal/apps/books/library_test.go`:

```go
package books_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// addBook creates a book or fails the test.
func addBook(t *testing.T, f *fixture, userID int64, nb books.NewBook) int64 {
	t.Helper()
	id, err := f.store.Create(context.Background(), userID, nb)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// onShelf is a minimal new book for a shelf.
func onShelf(title string, shelf books.Shelf) books.NewBook {
	return books.NewBook{BookInput: books.BookInput{Title: title}, Shelf: shelf}
}

func getBook(t *testing.T, f *fixture, userID, id int64) books.Book {
	t.Helper()
	b, err := f.store.Get(context.Background(), userID, id)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCreateWantToReadHasNoReading(t *testing.T) {
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, books.NewBook{
		BookInput: books.BookInput{Title: " Piranesi ", Authors: "Susanna Clarke", Year: 2020, Pages: 272},
		Shelf:     books.ShelfWant,
		Tags:      []string{"Fantasy", "#fantasy", "favourites"},
	})
	b := getBook(t, f, f.alice.ID, id)
	if b.Title != "Piranesi" || b.Authors != "Susanna Clarke" || b.Year != 2020 || b.Pages != 272 {
		t.Errorf("details = %+v", b.BookInput)
	}
	if b.Shelf != books.ShelfWant || b.Latest.ID != 0 {
		t.Errorf("shelf = %q, latest = %+v; want want and no reading", b.Shelf, b.Latest)
	}
	if !slices.Equal(b.Tags, []string{"fantasy", "favourites"}) {
		t.Errorf("tags = %v, want [fantasy favourites]", b.Tags)
	}
	if !b.AddedAt.Equal(f.now) || !b.UpdatedAt.Equal(f.now) {
		t.Errorf("added %v, updated %v; want both %v", b.AddedAt, b.UpdatedAt, f.now)
	}
}

func TestCreateReadingStartsOnTheLocalDay(t *testing.T) {
	f := newFixture(t)
	b := getBook(t, f, f.alice.ID, addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfReading)))
	if b.Shelf != books.ShelfReading || b.Latest.Status != books.StatusReading || b.Latest.StartedOn != "2026-10-10" {
		t.Errorf("shelf %q, latest %+v; want reading since 2026-10-10", b.Shelf, b.Latest)
	}
}

func TestCreateAlreadyReadUsesTheFinishDate(t *testing.T) {
	f := newFixture(t)
	nb := onShelf("Emma", books.ShelfRead)
	nb.FinishedOn = "2026-09-30"
	b := getBook(t, f, f.alice.ID, addBook(t, f, f.alice.ID, nb))
	if b.Shelf != books.ShelfRead || b.Latest.Status != books.StatusFinished ||
		b.Latest.FinishedOn != "2026-09-30" || b.Latest.StartedOn != "" {
		t.Errorf("shelf %q, latest %+v; want read, finished 2026-09-30, no start", b.Shelf, b.Latest)
	}
	today := getBook(t, f, f.alice.ID, addBook(t, f, f.alice.ID, onShelf("Ulysses", books.ShelfRead)))
	if today.Latest.FinishedOn != "2026-10-10" {
		t.Errorf("finished %q with no date, want today 2026-10-10", today.Latest.FinishedOn)
	}
}

func TestCreateRefusesBadFinishDates(t *testing.T) {
	f := newFixture(t)
	for _, day := range []string{"2026-10-11", "2026-02-30", "yesterday"} {
		nb := onShelf("Emma", books.ShelfRead)
		nb.FinishedOn = day
		_, err := f.store.Create(context.Background(), f.alice.ID, nb)
		var ref *books.Refusal
		if !errors.As(err, &ref) || ref.Msg == "" || !errors.Is(err, books.ErrInvalid) {
			t.Errorf("Create with finish %q: err = %v, want a Refusal", day, err)
		}
	}
}

func TestCreateRejectsBadDetails(t *testing.T) {
	f := newFixture(t)
	_, err := f.store.Create(context.Background(), f.alice.ID, onShelf("  ", books.ShelfWant))
	var verr *books.ValidationError
	if !errors.As(err, &verr) || verr.Fields["title"] == "" {
		t.Errorf("err = %v, want a ValidationError for title", err)
	}
}

func TestCreateRejectsAShelfYouCannotAddTo(t *testing.T) {
	f := newFixture(t)
	for _, shelf := range []books.Shelf{books.ShelfAll, books.ShelfDNF, "lent"} {
		if _, err := f.store.Create(context.Background(), f.alice.ID, onShelf("x", shelf)); !errors.Is(err, books.ErrInvalid) {
			t.Errorf("Create on %q: err = %v, want ErrInvalid", shelf, err)
		}
	}
}

func TestCreateStoresISBN13(t *testing.T) {
	f := newFixture(t)
	nb := onShelf("Some book", books.ShelfWant)
	nb.ISBN = "0-306-40615-2"
	if b := getBook(t, f, f.alice.ID, addBook(t, f, f.alice.ID, nb)); b.ISBN != "9780306406157" {
		t.Errorf("ISBN = %q, want 9780306406157", b.ISBN)
	}
}

func TestGetIsScopedToItsOwner(t *testing.T) {
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfWant))
	if _, err := f.store.Get(context.Background(), f.bob.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's Get = %v, want ErrNotFound", err)
	}
}

func TestUpdateChangesDetails(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfReading))
	f.now = f.now.Add(time.Hour)
	in := books.BookInput{Title: "Dune", Authors: "Frank Herbert", SeriesName: "Dune", SeriesNumber: "1", Pages: 412}
	if err := f.store.Update(ctx, f.alice.ID, id, in); err != nil {
		t.Fatal(err)
	}
	b := getBook(t, f, f.alice.ID, id)
	if b.Authors != "Frank Herbert" || b.SeriesNumber != "1" || b.Pages != 412 || !b.UpdatedAt.Equal(f.now) {
		t.Errorf("after update: %+v, updated %v", b.BookInput, b.UpdatedAt)
	}
	if b.Shelf != books.ShelfReading {
		t.Errorf("shelf = %q; editing details must not touch readings", b.Shelf)
	}
	if err := f.store.Update(ctx, f.bob.ID, id, in); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's Update = %v, want ErrNotFound", err)
	}
	var verr *books.ValidationError
	if err := f.store.Update(ctx, f.alice.ID, id, books.BookInput{}); !errors.As(err, &verr) {
		t.Errorf("Update with no title = %v, want a ValidationError", err)
	}
}

func TestDeleteTakesReadingsAndUnusedTags(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first := onShelf("Dune", books.ShelfReading)
	first.Tags = []string{"sf", "classics"}
	id := addBook(t, f, f.alice.ID, first)
	second := onShelf("Hyperion", books.ShelfWant)
	second.Tags = []string{"sf"}
	addBook(t, f, f.alice.ID, second)

	if err := f.store.Delete(ctx, f.bob.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's Delete = %v, want ErrNotFound", err)
	}
	if err := f.store.Delete(ctx, f.alice.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Get(ctx, f.alice.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
	var readings int
	if err := f.db.QueryRow(`SELECT count(*) FROM books_readings WHERE book_id = ?`, id).Scan(&readings); err != nil || readings != 0 {
		t.Errorf("readings left = %d, %v; want 0", readings, err)
	}
	tags, err := f.store.TagNames(ctx, f.alice.ID)
	if err != nil || !slices.Equal(tags, []string{"sf"}) {
		t.Errorf("TagNames = %v, %v; want [sf]", tags, err)
	}
}
```
`internal/apps/books/tag_test.go`:

```go
package books_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

func TestParseTags(t *testing.T) {
	tests := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{"SF, classics", []string{"sf", "classics"}},
		{" #sf ,, sf, Science  Fiction ", []string{"sf", "science fiction"}},
		{"a\tb, c\nd", []string{"a b", "c d"}},
		{strings.Repeat("x", 45), []string{strings.Repeat("x", 40)}},
	}
	for _, tt := range tests {
		if got := books.ParseTags(tt.raw); !slices.Equal(got, tt.want) {
			t.Errorf("ParseTags(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
	many := strings.Repeat("t,", 30)
	for i := 0; i < 30; i++ {
		many += string(rune('a'+i%26)) + string(rune('a'+i/26)) + ","
	}
	if got := books.ParseTags(many); len(got) != books.MaxTags {
		t.Errorf("ParseTags kept %d tags, want %d", len(got), books.MaxTags)
	}
}

func TestSetTagsReplacesTheBooksTags(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	nb := onShelf("Dune", books.ShelfWant)
	nb.Tags = []string{"sf", "classics"}
	id := addBook(t, f, f.alice.ID, nb)

	if err := f.store.SetTags(ctx, f.alice.ID, id, []string{"Desert", "sf"}); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.BookTags(ctx, f.alice.ID, id)
	if err != nil || !slices.Equal(got, []string{"desert", "sf"}) {
		t.Errorf("BookTags = %v, %v; want [desert sf]", got, err)
	}
	all, err := f.store.TagNames(ctx, f.alice.ID)
	if err != nil || !slices.Equal(all, []string{"desert", "sf"}) {
		t.Errorf("TagNames = %v, %v; want [desert sf] (classics unused, so gone)", all, err)
	}
}

func TestSetTagsIsScopedToItsOwner(t *testing.T) {
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfWant))
	if err := f.store.SetTags(context.Background(), f.bob.ID, id, []string{"x"}); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's SetTags = %v, want ErrNotFound", err)
	}
	if names, _ := f.store.TagNames(context.Background(), f.bob.ID); len(names) != 0 {
		t.Errorf("Bob has tags %v after a refused SetTags", names)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `undefined: books.NewBook` and friends.

- [ ] **Step 3: Add the shared helpers to `store.go`**

Add to the imports `"context"`, `"fmt"` and `"github.com/iliafrenkel/on-suite/internal/platform/db"`, then append:

```go
func formatTime(t time.Time) string { return db.FormatTime(t) }

func parseTime(s string) (time.Time, error) { return db.ParseTime(s) }

// Refusal is an action the store won't take for a reason the person can
// fix — a finish date before the start, a second reading at once. Msg is
// shown to them as is.
type Refusal struct{ Msg string }

func (e *Refusal) Error() string { return "books: " + e.Msg }

// Unwrap makes a Refusal an ErrInvalid for errors.Is.
func (e *Refusal) Unwrap() error { return ErrInvalid }

// checkDay accepts a YYYY-MM-DD date that is not after today.
func (st *Store) checkDay(day string) error {
	if _, err := time.Parse(dayLayout, day); err != nil {
		return &Refusal{Msg: "Enter a date like " + st.Today() + "."}
	}
	if day > st.Today() {
		return &Refusal{Msg: "That date is in the future."}
	}
	return nil
}

// touch bumps a book's updated_at inside tx and is the owner check: it is
// ErrNotFound for a missing or someone else's book. It runs inside the
// transaction, as ON Later's SetTags does (#294): with one connection, a
// check before it could race a delete.
func (st *Store) touch(ctx context.Context, tx *sql.Tx, userID, id int64) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE books_books SET updated_at = ? WHERE id = ? AND user_id = ?`,
		formatTime(st.now()), id, userID)
	if err != nil {
		return fmt.Errorf("books: touch: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 4: Write `tag.go`**

```go
package books

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode"
)

// MaxTagRunes bounds one tag name; longer names are cut, not refused.
const MaxTagRunes = 40

// MaxTags bounds how many tags one book keeps; the rest are dropped.
const MaxTags = 20

// ParseTags turns a comma-separated tags field into clean names, in the
// order typed: control characters become spaces, whitespace collapses,
// leading '#' go, names are lowercased and cut to MaxTagRunes, blanks and
// repeats are skipped, and at most MaxTags are kept. It mirrors ON Later's
// own ParseTags — apps never import each other, so this is an independent
// copy (PATTERNS.md "Cross-app mirroring").
func ParseTags(raw string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		part = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, part)
		name := strings.ToLower(strings.Join(strings.Fields(part), " "))
		name = strings.TrimSpace(strings.TrimLeft(name, "#"))
		if r := []rune(name); len(r) > MaxTagRunes {
			name = strings.TrimSpace(string(r[:MaxTagRunes]))
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
		if len(out) == MaxTags {
			break
		}
	}
	return out
}

// cleanTags applies ParseTags to names that may not have come through it.
func cleanTags(names []string) []string { return ParseTags(strings.Join(names, ",")) }

// linkTags finds or creates each of userID's tags and links bookID to
// them, inside the caller's transaction. names must already be clean.
func linkTags(ctx context.Context, tx *sql.Tx, userID, bookID int64, names []string) error {
	for _, name := range names {
		var tagID int64
		// DO UPDATE (a no-op) rather than DO NOTHING, so RETURNING gives the
		// id of an existing tag too.
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO books_tags (user_id, name) VALUES (?, ?)
			ON CONFLICT (user_id, name) DO UPDATE SET name = excluded.name
			RETURNING id`, userID, name).Scan(&tagID); err != nil {
			return fmt.Errorf("books: tag %q: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO books_book_tags (book_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			bookID, tagID); err != nil {
			return fmt.Errorf("books: link tag %q: %w", name, err)
		}
	}
	return nil
}

// gcTags deletes userID's tags that no book uses any more.
func gcTags(ctx context.Context, tx *sql.Tx, userID int64) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM books_tags
		 WHERE user_id = ?
		   AND NOT EXISTS (SELECT 1 FROM books_book_tags WHERE tag_id = books_tags.id)`, userID); err != nil {
		return fmt.Errorf("books: remove unused tags: %w", err)
	}
	return nil
}

// SetTags replaces a book's tags with names (cleaned here).
func (st *Store) SetTags(ctx context.Context, userID, id int64, names []string) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin set tags: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM books_book_tags WHERE book_id = ?`, id); err != nil {
		return fmt.Errorf("books: clear tags: %w", err)
	}
	if err := linkTags(ctx, tx, userID, id, cleanTags(names)); err != nil {
		return err
	}
	if err := gcTags(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// BookTags returns the names of a book's tags, alphabetically; none for a
// missing or someone else's book.
func (st *Store) BookTags(ctx context.Context, userID, id int64) ([]string, error) {
	return st.names(ctx, `
		SELECT t.name FROM books_book_tags x
		  JOIN books_tags t ON t.id = x.tag_id
		  JOIN books_books b ON b.id = x.book_id
		 WHERE b.id = ? AND b.user_id = ?
		 ORDER BY t.name`, id, userID)
}

// TagNames returns every tag userID has, alphabetically.
func (st *Store) TagNames(ctx context.Context, userID int64) ([]string, error) {
	return st.names(ctx, `SELECT name FROM books_tags WHERE user_id = ? ORDER BY name`, userID)
}

func (st *Store) names(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := st.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("books: tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("books: scan tag: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: tags: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 5: Write `library.go`**

```go
package books

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Shelf is where a book sits. It is never stored: it follows from the
// book's latest reading (spec "Derived values").
type Shelf string

// The shelves, as the sidebar lists them.
const (
	ShelfReading Shelf = "reading"
	ShelfWant    Shelf = "want"
	ShelfRead    Shelf = "read"
	ShelfDNF     Shelf = "dnf"
	ShelfAll     Shelf = "all"
)

// Shelves is every shelf in sidebar order.
var Shelves = []Shelf{ShelfReading, ShelfWant, ShelfRead, ShelfDNF, ShelfAll}

// ParseShelf maps a stored or posted name to its Shelf.
func ParseShelf(s string) (Shelf, bool) {
	for _, sh := range Shelves {
		if string(sh) == s {
			return sh, true
		}
	}
	return "", false
}

// Status is where one reading stands.
type Status string

// Reading statuses.
const (
	StatusReading  Status = "reading"
	StatusFinished Status = "finished"
	StatusDNF      Status = "dnf"
)

// Reading is one time through a book. Dates are YYYY-MM-DD in the server's
// local day, "" when unknown.
type Reading struct {
	ID         int64
	Status     Status
	Format     string // "", "paper", "ebook" or "audio" (set from B2)
	StartedOn  string
	FinishedOn string
}

// Book is one book with its tags and the reading that decides its shelf.
type Book struct {
	ID int64
	BookInput
	Rating             int    // 1–5, 0 for none (set from B2)
	Review             string // Markdown (set from B2)
	Tags               []string
	Shelf              Shelf
	Latest             Reading // zero ID before the first reading
	AddedAt, UpdatedAt time.Time
}

// NewBook is a book being added: its details, where it goes and its tags.
type NewBook struct {
	BookInput
	// Shelf is ShelfWant (no reading), ShelfReading (a reading started
	// today) or ShelfRead (a finished reading with no start date).
	Shelf Shelf
	// FinishedOn is the ShelfRead finish date, YYYY-MM-DD; "" means today.
	FinishedOn string
	Tags       []string // raw names; Create cleans them
}

// latestJoin attaches each book's latest reading as r — the one that
// decides its shelf. Ties on created_at go to the newer id.
const latestJoin = `LEFT JOIN books_readings r ON r.id = (
	SELECT id FROM books_readings WHERE book_id = b.id ORDER BY created_at DESC, id DESC LIMIT 1)`

// shelfExpr is a book's shelf from r (see latestJoin): the one place the
// shelf rule is written down in SQL.
const shelfExpr = `CASE WHEN r.id IS NULL THEN 'want'
	WHEN r.status = 'reading' THEN 'reading'
	WHEN r.status = 'finished' THEN 'read'
	ELSE 'dnf' END`

// nullInt and nullText store 0 and "" as NULL.
func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Create adds a book, its first reading if it goes on Reading or Read, and
// its tags, in one transaction. Details failing Validate are a
// *ValidationError, a bad finish date a *Refusal, any other shelf
// ErrInvalid.
func (st *Store) Create(ctx context.Context, userID int64, nb NewBook) (int64, error) {
	in := nb.BookInput.Normalize()
	if errs := in.Validate(); errs != nil {
		return 0, &ValidationError{Fields: errs}
	}
	finished := ""
	switch nb.Shelf {
	case ShelfWant, ShelfReading:
	case ShelfRead:
		finished = nb.FinishedOn
		if finished == "" {
			finished = st.Today()
		}
		if err := st.checkDay(finished); err != nil {
			return 0, err
		}
	default:
		return 0, ErrInvalid
	}

	now := formatTime(st.now())
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("books: begin create: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO books_books (user_id, title, subtitle, authors, year, pages, isbn13,
			series_name, series_number, description, added_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, in.Title, in.Subtitle, in.Authors, nullInt(in.Year), nullInt(in.Pages), nullText(in.ISBN),
		in.SeriesName, in.SeriesNumber, in.Description, now, now)
	if err != nil {
		return 0, fmt.Errorf("books: create: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("books: create: %w", err)
	}
	switch nb.Shelf {
	case ShelfReading:
		_, err = tx.ExecContext(ctx,
			`INSERT INTO books_readings (book_id, status, started_on, created_at) VALUES (?, 'reading', ?, ?)`,
			id, st.Today(), now)
	case ShelfRead:
		_, err = tx.ExecContext(ctx,
			`INSERT INTO books_readings (book_id, status, finished_on, created_at) VALUES (?, 'finished', ?, ?)`,
			id, finished, now)
	}
	if err != nil {
		return 0, fmt.Errorf("books: first reading: %w", err)
	}
	if err := linkTags(ctx, tx, userID, id, cleanTags(nb.Tags)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("books: commit create: %w", err)
	}
	return id, nil
}

// Get returns one of userID's books with its tags and latest reading.
func (st *Store) Get(ctx context.Context, userID, id int64) (Book, error) {
	var b Book
	var year, pages, rating, rid sql.NullInt64
	var isbn, status, format, started, finished sql.NullString
	var added, updated, shelf string
	err := st.db.QueryRowContext(ctx, `
		SELECT b.id, b.title, b.subtitle, b.authors, b.year, b.pages, b.isbn13, b.series_name,
		       b.series_number, b.description, b.rating, b.review, b.added_at, b.updated_at,
		       r.id, r.status, r.format, r.started_on, r.finished_on, `+shelfExpr+`
		  FROM books_books b `+latestJoin+`
		 WHERE b.id = ? AND b.user_id = ?`, id, userID).Scan(
		&b.ID, &b.Title, &b.Subtitle, &b.Authors, &year, &pages, &isbn, &b.SeriesName,
		&b.SeriesNumber, &b.Description, &rating, &b.Review, &added, &updated,
		&rid, &status, &format, &started, &finished, &shelf)
	if errors.Is(err, sql.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	if err != nil {
		return Book{}, fmt.Errorf("books: get: %w", err)
	}
	b.Year, b.Pages, b.Rating = int(year.Int64), int(pages.Int64), int(rating.Int64)
	b.ISBN = isbn.String
	b.Latest = Reading{ID: rid.Int64, Status: Status(status.String), Format: format.String,
		StartedOn: started.String, FinishedOn: finished.String}
	b.Shelf = Shelf(shelf)
	if b.AddedAt, err = parseTime(added); err != nil {
		return Book{}, fmt.Errorf("books: added_at: %w", err)
	}
	if b.UpdatedAt, err = parseTime(updated); err != nil {
		return Book{}, fmt.Errorf("books: updated_at: %w", err)
	}
	if b.Tags, err = st.BookTags(ctx, userID, id); err != nil {
		return Book{}, err
	}
	return b, nil
}

// Update replaces a book's details. Readings and tags are untouched.
func (st *Store) Update(ctx context.Context, userID, id int64, in BookInput) error {
	in = in.Normalize()
	if errs := in.Validate(); errs != nil {
		return &ValidationError{Fields: errs}
	}
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
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a book for good, with its readings and tag links (ON
// DELETE CASCADE), and any tag nothing else uses.
func (st *Store) Delete(ctx context.Context, userID, id int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `DELETE FROM books_books WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("books: delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if err := gcTags(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}
```

- [ ] **Step 6: Run the tests, then the full check**

Run: `go test ./internal/apps/books/... -count=1`
Expected: PASS. Then the full check from Global Constraints.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): store books and tags (#487)"
```

---

### Task 4: Store — shelves, the list and readings

**Files:**
- Modify: `internal/apps/books/library.go` (list and counts)
- Create: `internal/apps/books/reading.go`
- Test: `internal/apps/books/shelf_test.go`

**Interfaces:**
- Consumes: `latestJoin`, `shelfExpr`, `touch`, `checkDay`, `Refusal`, `Today`, `formatTime`, `parseTime` (Task 3).
- Produces:
  - `type ListItem struct { ID int64; Title, Authors, SeriesName, SeriesNumber string; Shelf Shelf; StartedOn, FinishedOn string; AddedAt time.Time }`
  - `type ListQuery struct { Shelf Shelf; Tag, Q string }` — `Shelf` "" or `ShelfAll` means every shelf; `Tag` matched case-insensitively; `Q` matched against title, subtitle, authors and series name.
  - `(*Store).List(ctx, userID int64, q ListQuery) ([]ListItem, error)`
  - `(*Store).ShelfCounts(ctx, userID int64) (map[Shelf]int, error)` — includes `ShelfAll`
  - `(*Store).StartReading(ctx, userID, id int64) error`, `FinishReading(ctx, userID, id int64, day string) error`, `MarkDNF(ctx, userID, id int64, day string) error` (`day` "" = today)
  - `func ShowDay(day string) string` — "2026-10-09" → "9 Oct 2026"

- [ ] **Step 1: Write the failing tests**

`internal/apps/books/shelf_test.go`:

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

func titles(items []books.ListItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

func list(t *testing.T, f *fixture, q books.ListQuery) []string {
	t.Helper()
	items, err := f.store.List(context.Background(), f.alice.ID, q)
	if err != nil {
		t.Fatal(err)
	}
	return titles(items)
}

func TestShelfFollowsTheLatestReading(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfWant))
	step := func(name string, do func() error, want books.Shelf) {
		t.Helper()
		f.now = f.now.Add(time.Minute)
		if err := do(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if b := getBook(t, f, f.alice.ID, id); b.Shelf != want {
			t.Errorf("after %s, shelf = %q, want %q", name, b.Shelf, want)
		}
	}
	step("start", func() error { return f.store.StartReading(ctx, f.alice.ID, id) }, books.ShelfReading)
	step("finish", func() error { return f.store.FinishReading(ctx, f.alice.ID, id, "") }, books.ShelfRead)
	step("re-read", func() error { return f.store.StartReading(ctx, f.alice.ID, id) }, books.ShelfReading)
	step("give up", func() error { return f.store.MarkDNF(ctx, f.alice.ID, id, "") }, books.ShelfDNF)
}

func TestStartRefusesASecondReadingInProgress(t *testing.T) {
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfReading))
	err := f.store.StartReading(context.Background(), f.alice.ID, id)
	var ref *books.Refusal
	if !errors.As(err, &ref) {
		t.Errorf("second StartReading = %v, want a Refusal", err)
	}
}

func TestStartCarriesOverThePreviousFormat(t *testing.T) {
	f := newFixture(t)
	nb := onShelf("Dune", books.ShelfRead)
	id := addBook(t, f, f.alice.ID, nb)
	if _, err := f.db.Exec(`UPDATE books_readings SET format = 'audio' WHERE book_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Minute)
	if err := f.store.StartReading(context.Background(), f.alice.ID, id); err != nil {
		t.Fatal(err)
	}
	if b := getBook(t, f, f.alice.ID, id); b.Latest.Format != "audio" || b.Latest.StartedOn != "2026-10-10" {
		t.Errorf("re-read = %+v, want audio, started 2026-10-10", b.Latest)
	}
}

func TestFinishUsesTheGivenDayAndChecksIt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.now = time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC) // 1 Oct in Melbourne
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfReading))
	f.now = time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC)

	for day, want := range map[string]string{
		"2026-09-30": "before you started",
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
	if b.Latest.Status != books.StatusFinished || b.Latest.StartedOn != "2026-10-01" || b.Latest.FinishedOn != "2026-10-05" {
		t.Errorf("latest = %+v, want finished 2026-10-01 → 2026-10-05", b.Latest)
	}
}

func TestFinishWithNothingInProgressIsRefused(t *testing.T) {
	f := newFixture(t)
	id := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfWant))
	var ref *books.Refusal
	if err := f.store.MarkDNF(context.Background(), f.alice.ID, id, ""); !errors.As(err, &ref) {
		t.Errorf("MarkDNF on a want-to-read book = %v, want a Refusal", err)
	}
}

func TestReadingActionsAreScopedToTheOwner(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	want := addBook(t, f, f.alice.ID, onShelf("Dune", books.ShelfWant))
	reading := addBook(t, f, f.alice.ID, onShelf("Emma", books.ShelfReading))
	if err := f.store.StartReading(ctx, f.bob.ID, want); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's StartReading = %v, want ErrNotFound", err)
	}
	if err := f.store.FinishReading(ctx, f.bob.ID, reading, ""); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Bob's FinishReading = %v, want ErrNotFound", err)
	}
}

func TestListShowsOneShelfInItsOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// Want to read: newest added first.
	addBook(t, f, f.alice.ID, onShelf("Want A", books.ShelfWant))
	f.now = f.now.Add(time.Hour)
	addBook(t, f, f.alice.ID, onShelf("Want B", books.ShelfWant))
	// Reading: latest start first.
	f.now = time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	addBook(t, f, f.alice.ID, onShelf("Reading A", books.ShelfReading))
	f.now = time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	addBook(t, f, f.alice.ID, onShelf("Reading B", books.ShelfReading))
	// Read: latest finish first, whatever order they were added in.
	f.now = time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC)
	for title, day := range map[string]string{"Read A": "2026-09-01", "Read B": "2026-09-20"} {
		nb := onShelf(title, books.ShelfRead)
		nb.FinishedOn = day
		addBook(t, f, f.alice.ID, nb)
	}
	gone := addBook(t, f, f.alice.ID, onShelf("Gave up", books.ShelfReading))
	if err := f.store.MarkDNF(ctx, f.alice.ID, gone, ""); err != nil {
		t.Fatal(err)
	}

	for shelf, want := range map[books.Shelf][]string{
		books.ShelfWant:    {"Want B", "Want A"},
		books.ShelfReading: {"Reading B", "Reading A"},
		books.ShelfRead:    {"Read B", "Read A"},
		books.ShelfDNF:     {"Gave up"},
	} {
		if got := list(t, f, books.ListQuery{Shelf: shelf}); !slices.Equal(got, want) {
			t.Errorf("List(%s) = %v, want %v", shelf, got, want)
		}
	}
	if got := list(t, f, books.ListQuery{Shelf: books.ShelfAll}); len(got) != 7 {
		t.Errorf("List(all) = %v, want all 7 books", got)
	}
	counts, err := f.store.ShelfCounts(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[books.Shelf]int{books.ShelfWant: 2, books.ShelfReading: 2, books.ShelfRead: 2, books.ShelfDNF: 1, books.ShelfAll: 7}
	for shelf, n := range want {
		if counts[shelf] != n {
			t.Errorf("ShelfCounts()[%s] = %d, want %d", shelf, counts[shelf], n)
		}
	}
}

func TestListFiltersByTagAndText(t *testing.T) {
	f := newFixture(t)
	dispossessed := onShelf("The Dispossessed", books.ShelfWant)
	dispossessed.Authors = "Ursula K. Le Guin"
	dispossessed.Tags = []string{"sf"}
	addBook(t, f, f.alice.ID, dispossessed)
	earthsea := onShelf("A Wizard of Earthsea", books.ShelfRead)
	earthsea.Authors = "Ursula K. Le Guin"
	earthsea.SeriesName = "Earthsea"
	addBook(t, f, f.alice.ID, earthsea)
	addBook(t, f, f.alice.ID, onShelf("100% Real", books.ShelfWant))
	addBook(t, f, f.alice.ID, onShelf("1000 Years", books.ShelfWant))
	addBook(t, f, f.bob.ID, onShelf("Bob's book about Le Guin", books.ShelfWant))

	tests := []struct {
		q    books.ListQuery
		want []string
	}{
		{books.ListQuery{Shelf: books.ShelfAll, Q: "le guin"}, []string{"A Wizard of Earthsea", "The Dispossessed"}},
		{books.ListQuery{Shelf: books.ShelfWant, Q: "le guin"}, []string{"The Dispossessed"}},
		{books.ListQuery{Shelf: books.ShelfAll, Q: "earthsea"}, []string{"A Wizard of Earthsea"}},
		{books.ListQuery{Shelf: books.ShelfAll, Q: "100%"}, []string{"100% Real"}},
		{books.ListQuery{Shelf: books.ShelfAll, Tag: "SF"}, []string{"The Dispossessed"}},
		{books.ListQuery{Shelf: books.ShelfAll, Tag: "nope"}, nil},
	}
	for _, tt := range tests {
		got := list(t, f, tt.q)
		slices.Sort(got)
		if !slices.Equal(got, tt.want) {
			t.Errorf("List(%+v) = %v, want %v", tt.q, got, tt.want)
		}
	}
}

func TestShowDay(t *testing.T) {
	if got := books.ShowDay("2026-10-09"); got != "9 Oct 2026" {
		t.Errorf("ShowDay = %q, want 9 Oct 2026", got)
	}
	if got := books.ShowDay("garbage"); got != "garbage" {
		t.Errorf("ShowDay(garbage) = %q, want it unchanged", got)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `undefined: books.ListQuery` and friends.

- [ ] **Step 3: Add the list and counts to `library.go`**

Add `"strings"` to the imports, then append:

```go
// ListItem is the slice of a book a list row needs.
type ListItem struct {
	ID                                       int64
	Title, Authors, SeriesName, SeriesNumber string
	Shelf                                    Shelf
	StartedOn, FinishedOn                    string // the latest reading's
	AddedAt                                  time.Time
}

// ListQuery picks the books a list shows. Shelf "" or ShelfAll is every
// shelf; Tag is one tag name; Q matches title, subtitle, authors or series
// name (SQLite LIKE: case-insensitive for ASCII).
type ListQuery struct {
	Shelf Shelf
	Tag   string
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
		return `b.added_at DESC, b.id DESC`
	default:
		return `b.updated_at DESC, b.id DESC`
	}
}

// likeEscape makes s match itself literally in a LIKE ... ESCAPE '\'.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// List returns userID's books matching q, in the shelf's order.
func (st *Store) List(ctx context.Context, userID int64, q ListQuery) ([]ListItem, error) {
	where := []string{"b.user_id = ?"}
	args := []any{userID}
	if q.Shelf != "" && q.Shelf != ShelfAll {
		where = append(where, shelfExpr+" = ?")
		args = append(args, string(q.Shelf))
	}
	if tag := strings.ToLower(strings.TrimSpace(q.Tag)); tag != "" {
		where = append(where, `EXISTS (SELECT 1 FROM books_book_tags x JOIN books_tags t ON t.id = x.tag_id
			WHERE x.book_id = b.id AND t.name = ?)`)
		args = append(args, tag)
	}
	if text := strings.TrimSpace(q.Q); text != "" {
		pat := "%" + likeEscape(text) + "%"
		where = append(where, `(b.title LIKE ? ESCAPE '\' OR b.subtitle LIKE ? ESCAPE '\'
			OR b.authors LIKE ? ESCAPE '\' OR b.series_name LIKE ? ESCAPE '\')`)
		args = append(args, pat, pat, pat, pat)
	}
	rows, err := st.db.QueryContext(ctx, `
		SELECT b.id, b.title, b.authors, b.series_name, b.series_number, b.added_at,
		       r.started_on, r.finished_on, `+shelfExpr+`
		  FROM books_books b `+latestJoin+`
		 WHERE `+strings.Join(where, " AND ")+`
		 ORDER BY `+listOrder(q.Shelf), args...)
	if err != nil {
		return nil, fmt.Errorf("books: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ListItem
	for rows.Next() {
		var it ListItem
		var added, shelf string
		var started, finished sql.NullString
		if err := rows.Scan(&it.ID, &it.Title, &it.Authors, &it.SeriesName, &it.SeriesNumber, &added,
			&started, &finished, &shelf); err != nil {
			return nil, fmt.Errorf("books: scan list: %w", err)
		}
		if it.AddedAt, err = parseTime(added); err != nil {
			return nil, fmt.Errorf("books: added_at: %w", err)
		}
		it.StartedOn, it.FinishedOn, it.Shelf = started.String, finished.String, Shelf(shelf)
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: list: %w", err)
	}
	return out, nil
}

// ShelfCounts returns how many of userID's books are on each shelf, with
// ShelfAll the total. Empty shelves are absent (zero).
func (st *Store) ShelfCounts(ctx context.Context, userID int64) (map[Shelf]int, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT `+shelfExpr+` AS shelf, count(*)
		  FROM books_books b `+latestJoin+`
		 WHERE b.user_id = ?
		 GROUP BY shelf`, userID)
	if err != nil {
		return nil, fmt.Errorf("books: shelf counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	counts := map[Shelf]int{}
	for rows.Next() {
		var shelf string
		var n int
		if err := rows.Scan(&shelf, &n); err != nil {
			return nil, fmt.Errorf("books: scan shelf count: %w", err)
		}
		counts[Shelf(shelf)] = n
		counts[ShelfAll] += n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: shelf counts: %w", err)
	}
	return counts, nil
}
```

- [ ] **Step 4: Write `reading.go`**

```go
package books

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// StartReading begins a new reading of a book today: its first, or a
// re-read. It carries over the previous reading's format (spec "Progress
// and finishing"). A book is read once at a time, so starting while a
// reading is in progress is a Refusal.
func (st *Store) StartReading(ctx context.Context, userID, id int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin start: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := st.touch(ctx, tx, userID, id); err != nil {
		return err
	}
	var active int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM books_readings WHERE book_id = ? AND status = 'reading'`, id).Scan(&active); err != nil {
		return fmt.Errorf("books: start: %w", err)
	}
	if active > 0 {
		return &Refusal{Msg: "You're already reading this book."}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO books_readings (book_id, status, format, started_on, created_at)
		VALUES (?, 'reading',
		        (SELECT format FROM books_readings WHERE book_id = ? ORDER BY created_at DESC, id DESC LIMIT 1),
		        ?, ?)`,
		id, id, st.Today(), formatTime(st.now())); err != nil {
		return fmt.Errorf("books: start: %w", err)
	}
	return tx.Commit()
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
	if err := st.checkDay(day); err != nil {
		return err
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("books: begin close reading: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
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
}

// ShowDay formats a stored date for people: "2026-10-09" → "9 Oct 2026".
// Anything else comes back unchanged.
func ShowDay(day string) string {
	t, err := time.Parse(dayLayout, day)
	if err != nil {
		return day
	}
	return t.Format("2 Jan 2006")
}
```

- [ ] **Step 5: Run the tests, then the full check**

Run: `go test ./internal/apps/books/... -count=1`
Expected: PASS. Then the full check from Global Constraints.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/books
git commit -m "feat(books): shelves, the book list and readings (#487)"
```

---
### Task 5: The three panes

**Files:**
- Create: `internal/apps/books/spine.go`, `internal/apps/books/view.go`, `internal/apps/books/templates/panes.partial.html`
- Modify: `internal/apps/books/handlers.go` (replace the placeholder index), `internal/apps/books/books.go` (route), `internal/apps/books/templates/index.html`
- Modify: `internal/ui/static/app.css` (new "ON Books" section at the end of the file)
- Test: `internal/apps/books/spine_test.go`, `internal/apps/books/handlers_test.go`

**Interfaces:**
- Consumes: `List`, `ShelfCounts`, `TagNames`, `Get`, `Today`, `ShowDay`, `Shelves`, `ParseShelf` (Tasks 3–4).
- Produces:
  - `var Colors = []string{"teal", "blue", "purple", "pink", "coral", "amber", "green", "gray"}`, `func SpineColor(title string) string`
  - `func (s Shelf) Label() string`
  - `type listCtx struct { Shelf Shelf; Tag, Q string }`, `ctxFrom(get func(string) string) listCtx`, methods `Query() string`, `ListURL() string`, `BookURL(id int64) string`
  - view models `panesView`, `sidebarView`, `listView`, `rowView`, `bookView` (fields below); `viewBook(b Book, c listCtx, today string) bookView`
  - `(*App).pathID`, `(*App).fail`, `(*App).renderPanes(w, r, userID int64, c listCtx, bookID int64, errMsg string)`
  - Routes `GET /books/` and `GET /books/b/{id}`; htmx blocks `panes-oob` (target `#books-panes`), `list-swap` (target `#books-list`), `book-swap` (target `#books-book`); shared blocks `ctx-fields`, `back`, `pane-toggle`
  - Element ids: `books-panes`, `books-panes-row`, `books-side`, `books-list`, `books-book`, `books-q`, `books-filter-ctx`, `books-list-open`, `books-book-open`

- [ ] **Step 1: Write the failing tests**

`internal/apps/books/spine_test.go`:

```go
package books_test

import (
	"slices"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

func TestSpineColorIsStableAndFromThePalette(t *testing.T) {
	c := books.SpineColor("Piranesi")
	if c != books.SpineColor("piranesi") || c != books.SpineColor("Piranesi") {
		t.Errorf("SpineColor is not stable across calls and case")
	}
	seen := map[string]bool{}
	for _, title := range []string{"Piranesi", "Dune", "Leviathan Wakes", "The Dispossessed", "Emma", "Middlemarch", "Ulysses", "Beloved"} {
		color := books.SpineColor(title)
		if !slices.Contains(books.Colors, color) {
			t.Errorf("SpineColor(%q) = %q, not in the palette", title, color)
		}
		seen[color] = true
	}
	if len(seen) < 3 {
		t.Errorf("eight titles got only %d colours: %v", len(seen), seen)
	}
}
```

Add to `internal/apps/books/handlers_test.go` (keep the two existing tests; extend its imports with `"context"`, `"fmt"`, `"strings"`, `"time"` and `"github.com/iliafrenkel/on-suite/internal/htmlassert"`):

```go
// add creates one of userID's books through the store.
func add(t *testing.T, s *server, userID int64, nb books.NewBook) int64 {
	t.Helper()
	id, err := s.Store.Create(context.Background(), userID, nb)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func titled(title, authors string, shelf books.Shelf) books.NewBook {
	return books.NewBook{BookInput: books.BookInput{Title: title, Authors: authors}, Shelf: shelf}
}

// rowTitles is the list pane's titles, top to bottom.
func rowTitles(doc *htmlassert.Doc) []string {
	var out []string
	for _, n := range doc.QueryAll(".books-row-title") {
		out = append(out, htmlassert.Text(n))
	}
	return out
}

// isChecked says whether the checkbox with this id is checked.
func isChecked(t *testing.T, doc *htmlassert.Doc, id string) bool {
	t.Helper()
	n := doc.MustHave("input#" + id)
	_, ok := htmlassert.Attr(n, "checked")
	return ok
}

// hx performs an htmx GET aimed at target and returns the body.
func hx(t *testing.T, s *server, path, target string) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", target)
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx GET %s = %d; body: %s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestIndexOpensOnTheReadingShelf(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	add(t, s, uid, titled("Dune", "Frank Herbert", books.ShelfReading))
	add(t, s, uid, titled("Emma", "Jane Austen", books.ShelfWant))
	add(t, s, s.Bob.User.ID, titled("Bob's book", "", books.ShelfReading))

	doc := s.Get(t, s.Alice, "/books/")
	if got := rowTitles(doc); len(got) != 1 || got[0] != "Dune" {
		t.Errorf("rows = %v, want [Dune]", got)
	}
	current := doc.MustHave(`.books-side a[aria-current="page"]`)
	if text := htmlassert.Text(current); !strings.HasPrefix(text, "Reading") || !strings.HasSuffix(text, "1") {
		t.Errorf("current shelf link = %q, want Reading with count 1", text)
	}
	doc.MustHave(`a[href="/books/new"]`)
	doc.MustHave(".books-book .empty")
	if !isChecked(t, doc, "books-list-open") || isChecked(t, doc, "books-book-open") {
		t.Error("phone drill-down: want the list showing and no book open")
	}
}

func TestShelfAndFilterQueriesPickTheBooks(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	dispossessed := titled("The Dispossessed", "Ursula K. Le Guin", books.ShelfWant)
	dispossessed.Tags = []string{"sf"}
	add(t, s, uid, dispossessed)
	add(t, s, uid, titled("Emma", "Jane Austen", books.ShelfWant))
	add(t, s, uid, titled("A Wizard of Earthsea", "Ursula K. Le Guin", books.ShelfRead))

	tests := []struct {
		path string
		want string
	}{
		{"/books/?shelf=want", "Emma|The Dispossessed"},
		{"/books/?shelf=all&q=le+guin", "A Wizard of Earthsea|The Dispossessed"},
		{"/books/?shelf=all&tag=sf", "The Dispossessed"},
		{"/books/?shelf=nonsense", ""}, // unknown shelf → Reading, which is empty
	}
	for _, tt := range tests {
		got := rowTitles(s.Get(t, s.Alice, tt.path))
		slices.Sort(got)
		if strings.Join(got, "|") != tt.want {
			t.Errorf("%s rows = %v, want %s", tt.path, got, tt.want)
		}
	}
	doc := s.Get(t, s.Alice, "/books/?shelf=all&tag=sf")
	if text := htmlassert.Text(doc.MustHave(`.books-side a[aria-current="page"]`)); text != "sf" {
		t.Errorf("current sidebar link = %q, want the sf tag", text)
	}
	if v, _ := htmlassert.Attr(doc.MustHave("input#books-q"), "value"); v != "" {
		t.Errorf("filter box = %q, want empty", v)
	}
}

func TestEmptyShelfSaysSo(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/books/?shelf=dnf")
	doc.MustHave(".books-list .empty")
	doc.MustNotHave(".books-row")
}

func TestBookPaneShowsTheBook(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	nb := titled("Leviathan Wakes", "James S. A. Corey", books.ShelfReading)
	nb.SeriesName, nb.SeriesNumber, nb.Year, nb.Pages, nb.ISBN = "The Expanse", "1", 2011, 592, "0306406152"
	nb.Tags = []string{"sf"}
	id := add(t, s, s.Alice.User.ID, nb)

	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=reading", id))
	book := doc.MustHave("#books-book")
	if got := htmlassert.Text(doc.MustHave(".books-book h1")); got != "Leviathan Wakes" {
		t.Errorf("h1 = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-series")); got != "The Expanse #1" {
		t.Errorf("series = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-facts")); got != "2011 · 592 pages · ISBN 9780306406157" {
		t.Errorf("facts = %q", got)
	}
	if v, _ := htmlassert.Attr(book, "data-book-id"); v != fmt.Sprint(id) {
		t.Errorf("data-book-id = %q, want %d", v, id)
	}
	if !isChecked(t, doc, "books-book-open") {
		t.Error("books-book-open not checked with a book open")
	}
	doc.MustHave(".books-rows .is-active")
	if title := htmlassert.Text(doc.MustHave("title")); !strings.HasPrefix(title, "Leviathan Wakes") {
		t.Errorf("<title> = %q, want the book's title first", title)
	}
}

func TestSomeoneElsesBookIsNotFound(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))
	for _, path := range []string{fmt.Sprintf("/books/b/%d", id), "/books/b/999", "/books/b/x"} {
		if rec := s.Do(t, s.Bob, httptest.NewRequest("GET", path, nil)); rec.Code != http.StatusNotFound {
			t.Errorf("Bob GET %s = %d, want 404", path, rec.Code)
		}
	}
}

func TestHTMXListNavigationSwapsTheListOnly(t *testing.T) {
	s := newServer(t)
	add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	body := hx(t, s, "/books/?shelf=want", "books-list")
	doc := htmlassert.Parse(t, body)
	if got := rowTitles(doc); len(got) != 1 || got[0] != "Emma" {
		t.Errorf("rows = %v, want [Emma]", got)
	}
	for _, id := range []string{"books-side", "books-filter-ctx", "books-list-open", "books-book-open", "shell-crumb-tail"} {
		if v, ok := htmlassert.Attr(doc.MustHave("#"+id), "hx-swap-oob"); !ok || v != "true" {
			t.Errorf("#%s hx-swap-oob = %q, %v; want it swapped out of band", id, v, ok)
		}
	}
	doc.MustNotHave("#books-book")
	doc.MustNotHave("#books-panes")
}

func TestHTMXBookOpenSwapsTheBookOnly(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	doc := htmlassert.Parse(t, hx(t, s, fmt.Sprintf("/books/b/%d?shelf=want", id), "books-book"))
	doc.MustHave("#books-book h1")
	if !isChecked(t, doc, "books-book-open") {
		t.Error("book-swap did not check books-book-open out of band")
	}
	doc.MustNotHave("#books-list")
	doc.MustNotHave("#books-side")
}
```
Also add `"slices"` to the test file's imports.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — `undefined: books.SpineColor`, and the handler tests fail on missing elements.

- [ ] **Step 3: Write `spine.go`**

```go
package books

import (
	"hash/fnv"
	"strings"
)

// Colors is the suite's swatch palette (internal/ui/static/app.css's
// swatch-c-* classes). It mirrors ON Focus's own list — apps never import
// each other, so each keeps its copy; the CSS is the shared part.
var Colors = []string{"teal", "blue", "purple", "pink", "coral", "amber", "green", "gray"}

// SpineColor picks the colour of a book's generated spine from its title,
// so the same book always looks the same (spec "Architecture": generated
// spine).
func SpineColor(title string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(title)))
	return Colors[h.Sum32()%uint32(len(Colors))]
}
```

- [ ] **Step 4: Write `view.go`**

```go
package books

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
)

var shelfLabels = map[Shelf]string{
	ShelfReading: "Reading",
	ShelfWant:    "Want to read",
	ShelfRead:    "Read",
	ShelfDNF:     "Did not finish",
	ShelfAll:     "All books",
}

// Label is how a shelf is named on screen.
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
// default shelf (spec "Layout").
func ctxFrom(get func(string) string) listCtx {
	sh, ok := ParseShelf(get("shelf"))
	if !ok {
		sh = ShelfReading
	}
	return listCtx{Shelf: sh, Tag: strings.ToLower(strings.TrimSpace(get("tag"))), Q: strings.TrimSpace(get("q"))}
}

// Query is the context as a query string. Templates use the URL methods
// below rather than this: a whole URL dropped into href is left alone by
// html/template, a query string after a literal "?" gets percent-encoded.
func (c listCtx) Query() string {
	v := url.Values{}
	v.Set("shelf", string(c.Shelf))
	if c.Tag != "" {
		v.Set("tag", c.Tag)
	}
	if c.Q != "" {
		v.Set("q", c.Q)
	}
	return v.Encode()
}

// ListURL is this list.
func (c listCtx) ListURL() string { return "/books/?" + c.Query() }

// BookURL is a book opened from this list.
func (c listCtx) BookURL(id int64) string {
	return "/books/b/" + strconv.FormatInt(id, 10) + "?" + c.Query()
}

// panesView is everything the three panes draw. Title and Shell are for
// fragments, which have no render.Page around them.
type panesView struct {
	Title   string
	Shell   render.Shell
	Ctx     listCtx
	Error   string // the banner, for a refused action
	Sidebar sidebarView
	List    listView
	Book    bookView
}

type shelfLink struct {
	Label   string
	URL     string
	Count   int
	Current bool
}

type tagLink struct {
	Name    string
	URL     string
	Current bool
}

type sidebarView struct {
	Shelves []shelfLink
	Tags    []tagLink
}

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
		v.Tags = append(v.Tags, tagLink{Name: name, URL: to.ListURL(), Current: c.Tag == name})
	}
	return v
}

type rowView struct {
	ID      int64
	URL     string
	Title   string
	Byline  string // "Authors · Series #3"
	Note    string // what the book's shelf says about it: "Started 3 Oct 2026"
	Spine   string // swatch colour name
	Initial string
	Active  bool
}

type listView struct {
	Heading string
	Rows    []rowView
	Empty   string
}

func listHeading(c listCtx) string {
	if c.Tag != "" {
		return "Tagged “" + c.Tag + "”"
	}
	return c.Shelf.Label()
}

func viewList(items []ListItem, c listCtx, openID int64) listView {
	v := listView{Heading: listHeading(c)}
	for _, it := range items {
		v.Rows = append(v.Rows, rowView{ID: it.ID, URL: c.BookURL(it.ID), Title: it.Title,
			Byline: byline(it.Authors, seriesText(it.SeriesName, it.SeriesNumber)), Note: rowNote(it),
			Spine: SpineColor(it.Title), Initial: initial(it.Title), Active: it.ID == openID})
	}
	if len(v.Rows) == 0 {
		v.Empty = emptyText(c)
	}
	return v
}

func byline(authors, series string) string {
	var parts []string
	for _, p := range []string{authors, series} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

func seriesText(name, number string) string {
	switch {
	case name == "":
		return ""
	case number == "":
		return name
	}
	return name + " #" + number
}

// rowNote is the right-hand side of a row. B2 replaces the Reading and
// Read notes with a progress bar and stars.
func rowNote(it ListItem) string {
	switch it.Shelf {
	case ShelfReading:
		if it.StartedOn != "" {
			return "Started " + ShowDay(it.StartedOn)
		}
		return "Reading"
	case ShelfRead:
		if it.FinishedOn != "" {
			return "Finished " + ShowDay(it.FinishedOn)
		}
		return "Read"
	case ShelfDNF:
		return "Did not finish"
	}
	return "Added " + it.AddedAt.Local().Format("2 Jan 2006")
}

// initial is the first letter or digit of a title, for the mini spine.
func initial(title string) string {
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return strings.ToUpper(string(r))
		}
	}
	return "?"
}

func emptyText(c listCtx) string {
	switch {
	case c.Q != "":
		return "No books match “" + c.Q + "”."
	case c.Tag != "":
		return "No books tagged “" + c.Tag + "”."
	}
	switch c.Shelf {
	case ShelfReading:
		return "Nothing on the go. Start something from Want to read, or add a book."
	case ShelfWant:
		return "Nothing waiting. Add a book you'd like to read."
	case ShelfRead:
		return "No finished books yet."
	case ShelfDNF:
		return "Nothing abandoned."
	}
	return "No books yet. Click Add book to start."
}

// bookView is the book pane. Selected is false when no book is open.
type bookView struct {
	Selected                         bool
	ID                               int64
	Title, Subtitle, Authors, Series string
	Facts                            []string // "2011", "592 pages", "ISBN 978…"
	Description                      string
	Spine, Initial                   string
	ShelfLabel                       string
	Tags                             []string
	Ctx                              listCtx
	Shell                            render.Shell
}

// viewBook draws a book; today is for the reading box's date fields
// (Task 7).
func viewBook(b Book, c listCtx, today string) bookView {
	v := bookView{Selected: true, ID: b.ID, Title: b.Title, Subtitle: b.Subtitle, Authors: b.Authors,
		Series: seriesText(b.SeriesName, b.SeriesNumber), Description: b.Description,
		Spine: SpineColor(b.Title), Initial: initial(b.Title), ShelfLabel: b.Shelf.Label(),
		Tags: b.Tags, Ctx: c}
	if b.Year > 0 {
		v.Facts = append(v.Facts, strconv.Itoa(b.Year))
	}
	if b.Pages > 0 {
		v.Facts = append(v.Facts, strconv.Itoa(b.Pages)+" pages")
	}
	if b.ISBN != "" {
		v.Facts = append(v.Facts, "ISBN "+b.ISBN)
	}
	return v
}
```

- [ ] **Step 5: Replace `handlers.go`**

```go
package books

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// userID is the signed-in user; every route is registered with HandleFunc.
func (a *App) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u, ok := web.UserFrom(r.Context())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusUnauthorized)
		return 0, false
	}
	return u.ID, true
}

// pathID parses the {id} path segment; anything but a positive integer is a
// 404, the same as a book that isn't there.
func (a *App) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// fail maps a store error to its response: someone else's row is a 404,
// bad input a 400, anything else a logged 500.
func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		a.deps.Errors.Status(w, r, http.StatusNotFound)
	case errors.Is(err, ErrInvalid):
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
	default:
		a.deps.Errors.Internal(w, r, err)
	}
}

// index is a list with no book open.
func (a *App) index(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	a.renderPanes(w, r, uid, ctxFrom(r.FormValue), 0, "")
}

// book is a list with one book open.
func (a *App) book(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
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
		a.fail(w, r, err)
		return
	}
	tags, err := a.store.TagNames(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	items, err := a.store.List(ctx, userID, ListQuery(c)) // same fields, by design
	if err != nil {
		a.fail(w, r, err)
		return
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
		switch web.HTMXTarget(r) {
		case "books-list":
			block = "list-swap"
		case "books-book":
			block = "book-swap"
		}
		if err := a.deps.Render.Fragment(w, http.StatusOK, "books/index", block, v); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	status := http.StatusOK
	if errMsg != "" {
		status = http.StatusUnprocessableEntity
	}
	page.Data = v
	if err := a.deps.Render.Page(w, status, "books/index", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
```

In `books.go`'s `Mount`, add after the index route:

```go
	r.HandleFunc("GET /b/{id}", a.book)
```

- [ ] **Step 6: Write the templates**

`internal/apps/books/templates/index.html`:

```html
{{define "head"}}<script src="/books/books.js" defer></script>{{end}}

{{define "content"}}{{template "panes" .Data}}{{end}}
```

`internal/apps/books/templates/panes.partial.html`:

```html
{{/* ON Books' three panes — shelves and tags, the list, the book — on ON
     Reader's layout (internal/apps/reader/templates/panes.partial.html).
     Apps never import each other, so these blocks are Books' own. Every
     block here takes a panesView (or the part named in its comment). */}}

{{/* The <title> and crumb ride along with every fragment, so the page
     chrome follows the panes (#205). */}}
{{define "shell-oob"}}<title>{{if .Title}}{{.Title}} · {{end}}ON Suite</title><span id="shell-crumb-tail" hx-swap-oob="true">{{template "shell-crumb-tail" (dict "Title" .Title "Shell" .Shell)}}</span>{{end}}

{{/* panes-oob answers anything that changes a book: the whole panes. */}}
{{define "panes-oob"}}{{template "shell-oob" .}}{{template "panes" .}}{{end}}

{{/* list-swap answers a shelf, tag or filter change (target #books-list).
     The sidebar comes along out of band — its highlight and links follow
     the list, and unlike Reader's tree it holds nothing worth keeping — as
     do the filter's hidden fields and the phone drill-down boxes. The book
     pane is left as it is; books.js moves the row highlight. */}}
{{define "list-swap"}}{{template "shell-oob" .}}{{template "list" .List}}{{template "sidebar" (dict "Sidebar" .Sidebar "OOB" true)}}{{template "filter-ctx" (dict "Ctx" .Ctx "OOB" true)}}{{template "pane-toggle" (dict "ID" "books-list-open" "Label" "Shelves" "Checked" true "OOB" true)}}{{template "pane-toggle" (dict "ID" "books-book-open" "Label" "Books" "Checked" false "OOB" true)}}{{end}}

{{/* book-swap answers opening a book (target #books-book). The checkbox
     out of band is what makes a phone drill into the book. */}}
{{define "book-swap"}}{{template "shell-oob" .}}{{template "book" .Book}}{{template "pane-toggle" (dict "ID" "books-book-open" "Label" "Books" "Checked" true "OOB" true)}}{{end}}

{{define "panes"}}
<div class="books-panes" id="books-panes">
	{{with .Error}}<p class="notice notice-error books-banner" role="alert">{{.}}</p>{{end}}
	<div class="books-panes-row" id="books-panes-row">
		{{/* The narrow-screen drill-down (Reader's technique): the list is
		     always "open" over the sidebar, a book over the list. Siblings
		     of the panes so the CSS reaches them with ~; the back labels in
		     the panes uncheck them. */}}
		{{template "pane-toggle" (dict "ID" "books-list-open" "Label" "Shelves" "Checked" true "OOB" false)}}
		{{template "pane-toggle" (dict "ID" "books-book-open" "Label" "Books" "Checked" .Book.Selected "OOB" false)}}
		{{template "sidebar" (dict "Sidebar" .Sidebar "OOB" false)}}
		<div class="pane-gutter" data-gutter-for="side" role="separator"
		     aria-orientation="vertical" aria-label="Resize the shelves" tabindex="0"></div>
		<section class="books-listpane" aria-label="Books">
			{{template "back" (dict "For" "books-list-open" "Label" "Shelves")}}
			{{/* The filter lives outside #books-list so a list swap never
			     replaces the box being typed in (PATTERNS.md, SyncSearch). */}}
			<form class="books-filter" method="get" action="/books/" role="search">
				{{template "filter-ctx" (dict "Ctx" .Ctx "OOB" false)}}
				<label class="visually-hidden" for="books-q">Filter by title or author</label>
				<input type="search" id="books-q" name="q" value="{{.Ctx.Q}}" placeholder="Filter by title or author…"
				       hx-get="/books/" hx-target="#books-list" hx-swap="outerHTML"
				       hx-include="closest form" hx-trigger="input changed delay:300ms, search"
				       hx-replace-url="true">
			</form>
			{{template "list" .List}}
		</section>
		<div class="pane-gutter" data-gutter-for="list" role="separator"
		     aria-orientation="vertical" aria-label="Resize the book pane" tabindex="0"></div>
		{{template "book" .Book}}
	</div>
</div>
{{end}}

{{define "pane-toggle"}}<input type="checkbox" id="{{.ID}}" class="visually-hidden books-pane-toggle"{{if .Checked}} checked{{end}}{{if .OOB}} hx-swap-oob="true"{{end}} aria-label="{{.Label}}">{{end}}

{{define "back"}}<label class="toolbar-btn books-back-btn" for="{{.For}}">{{ticon "arrow-left"}}{{.Label}}</label>{{end}}

{{/* filter-ctx is the shelf and tag the filter box searches within. */}}
{{define "filter-ctx"}}<span id="books-filter-ctx"{{if .OOB}} hx-swap-oob="true"{{end}}><input type="hidden" name="shelf" value="{{.Ctx.Shelf}}">{{with .Ctx.Tag}}<input type="hidden" name="tag" value="{{.}}">{{end}}</span>{{end}}

{{/* ctx-fields is a listCtx as hidden fields, for POSTs. */}}
{{define "ctx-fields"}}<input type="hidden" name="shelf" value="{{.Shelf}}">{{with .Tag}}<input type="hidden" name="tag" value="{{.}}">{{end}}{{with .Q}}<input type="hidden" name="q" value="{{.}}">{{end}}{{end}}

{{define "sidebar"}}
<nav class="books-side" id="books-side" aria-label="Shelves"{{if .OOB}} hx-swap-oob="true"{{end}}>
	<a class="button primary books-add" href="/books/new">{{ticon "plus"}}Add book</a>
	<ul class="books-shelves">
		{{range .Sidebar.Shelves}}
		<li><a class="books-shelf" href="{{.URL}}" hx-get="{{.URL}}" hx-target="#books-list" hx-swap="outerHTML" hx-push-url="true"{{if .Current}} aria-current="page"{{end}}>{{.Label}}<span class="books-count">{{if .Count}}{{.Count}}{{end}}</span></a></li>
		{{end}}
	</ul>
	{{with .Sidebar.Tags}}
	<h2 class="books-side-head">Tags</h2>
	<ul class="books-shelves">
		{{range .}}
		<li><a class="books-shelf" href="{{.URL}}" hx-get="{{.URL}}" hx-target="#books-list" hx-swap="outerHTML" hx-push-url="true"{{if .Current}} aria-current="page"{{end}}>{{.Name}}</a></li>
		{{end}}
	</ul>
	{{end}}
</nav>
{{end}}

{{/* list takes a listView. */}}
{{define "list"}}
<div class="books-list" id="books-list">
	<h2 class="books-list-head">{{.Heading}}</h2>
	{{if .Rows}}
	<ul class="books-rows">
		{{range .Rows}}
		<li class="books-row{{if .Active}} is-active{{end}}" data-book-id="{{.ID}}">
			<a class="books-row-link" href="{{.URL}}" hx-get="{{.URL}}" hx-target="#books-book" hx-swap="outerHTML" hx-push-url="true">
				<span class="books-mini-spine swatch-c-{{.Spine}}" aria-hidden="true">{{.Initial}}</span>
				<span class="books-row-text">
					<span class="books-row-title">{{.Title}}</span>
					{{with .Byline}}<span class="books-row-byline">{{.}}</span>{{end}}
					<span class="books-row-note">{{.Note}}</span>
				</span>
			</a>
		</li>
		{{end}}
	</ul>
	{{else}}
	<p class="empty">{{.Empty}}</p>
	{{end}}
</div>
{{end}}

{{/* book takes a bookView. */}}
{{define "book"}}
<section class="books-book" id="books-book" aria-label="Book"{{if .Selected}} data-book-id="{{.ID}}"{{end}}>
{{if .Selected}}
	{{template "back" (dict "For" "books-book-open" "Label" "Books")}}
	<header class="books-book-head">
		<div class="books-spine swatch-c-{{.Spine}}" aria-hidden="true"><span>{{.Title}}</span></div>
		<div class="books-book-titles">
			<h1>{{.Title}}</h1>
			{{with .Subtitle}}<p class="books-subtitle">{{.}}</p>{{end}}
			{{with .Authors}}<p class="books-authors">{{.}}</p>{{end}}
			{{with .Series}}<p class="books-series">{{.}}</p>{{end}}
			{{with .Facts}}<p class="books-facts">{{range $i, $f := .}}{{if $i}} · {{end}}{{$f}}{{end}}</p>{{end}}
			<p class="books-pills"><span class="books-pill books-pill-shelf">{{.ShelfLabel}}</span>{{range .Tags}} <span class="books-pill">{{.}}</span>{{end}}</p>
		</div>
	</header>
	{{with .Description}}<div class="books-description">{{.}}</div>{{end}}
{{else}}
	<p class="empty">Pick a book, or add one.</p>
{{end}}
</section>
{{end}}
```

- [ ] **Step 7: Add the ON Books CSS section**

Append to `internal/ui/static/app.css`:

```css
/* ---- ON Books ----------------------------------------------------------
 * Three panes — shelves, list, book — on ON Reader's layout: the same
 * height arithmetic, .pane-gutter dividers whose widths live in custom
 * properties on <html> (books.js), and two drill-down checkboxes for narrow
 * screens. Spec: docs/superpowers/specs/2026-10-09-on-books-design.md. */
main:has(.books-panes) { max-width: none; padding: 0; }

.books-panes {
	--books-chrome: calc(4rem + var(--s-6) * 2);
	display: flex;
	flex-direction: column;
	height: calc(100vh - var(--books-chrome));
	height: calc(100dvh - var(--books-chrome));
	min-height: 0;
	overflow: hidden;
}
.books-banner { margin: var(--s-2) var(--s-3) 0; }
.books-panes-row { display: flex; flex: 1 1 auto; min-height: 0; overflow: hidden; }
.books-side { flex: 0 0 var(--books-side-w, 14rem); }
.books-listpane { flex: 0 0 var(--books-list-w, 22rem); display: flex; flex-direction: column; }
.books-book { flex: 1 1 auto; }
.books-side,
.books-listpane,
.books-book { min-width: 0; min-height: 0; overflow-y: auto; padding: var(--s-3); background: var(--c-bg); }
.books-side,
.books-listpane { border-right: 1px solid var(--c-border); }

/* Sidebar */
.books-add { display: flex; align-items: center; justify-content: center; gap: var(--s-1); margin-bottom: var(--s-3); }
.books-shelves,
.books-rows { list-style: none; margin: 0; padding: 0; }
.books-shelf { display: flex; justify-content: space-between; gap: var(--s-2); padding: var(--s-1) var(--s-2); border-radius: var(--radius); color: var(--c-text); text-decoration: none; }
.books-shelf:hover { background: var(--c-bg-subtle); }
.books-shelf[aria-current="page"] { background: var(--c-accent-bg); color: var(--c-accent); font-weight: 600; }
.books-count { color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-side-head { margin: var(--s-4) 0 var(--s-1); padding: 0 var(--s-2); font-size: var(--fs-xs); letter-spacing: 0.05em; text-transform: uppercase; color: var(--c-text-dim); }

/* List */
.books-filter { margin-bottom: var(--s-3); }
.books-filter input[type="search"] { width: 100%; }
.books-list { flex: 1 1 auto; }
.books-list-head { margin: 0 0 var(--s-2); font-size: var(--fs-base); }
.books-row-link { display: flex; align-items: center; gap: var(--s-3); padding: var(--s-2); border-radius: var(--radius); color: var(--c-text); text-decoration: none; }
.books-row-link:hover { background: var(--c-bg-subtle); }
.books-row.is-active .books-row-link { background: var(--c-accent-bg); }
.books-mini-spine { flex: 0 0 auto; display: grid; place-items: center; width: 2rem; height: 3rem; border-left: 4px solid var(--swatch); border-radius: 2px; background: var(--swatch-soft); color: var(--swatch); font-weight: 700; }
.books-row-text { display: flex; flex-direction: column; flex: 1 1 auto; min-width: 0; }
.books-row-title { font-weight: 600; }
.books-row-title,
.books-row-byline,
.books-row-note { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.books-row-byline,
.books-row-note { color: var(--c-text-dim); font-size: var(--fs-sm); }

/* Book */
.books-book-head { display: flex; flex-wrap: wrap; align-items: flex-start; gap: var(--s-4); margin-bottom: var(--s-4); }
.books-spine { flex: 0 0 auto; display: flex; align-items: flex-end; width: 6rem; height: 9rem; padding: var(--s-2); overflow: hidden; border-left: 8px solid var(--swatch); border-radius: 3px; background: var(--swatch-soft); color: var(--c-text); font-size: var(--fs-xs); font-weight: 600; line-height: 1.2; overflow-wrap: anywhere; }
.books-book-titles { flex: 1 1 12rem; min-width: 0; }
.books-book-titles h1 { margin: 0; font-size: var(--fs-xl); }
.books-subtitle { margin: var(--s-1) 0 0; color: var(--c-text-dim); }
.books-authors { margin: var(--s-2) 0 0; font-weight: 600; }
.books-series,
.books-facts { margin: var(--s-1) 0 0; color: var(--c-text-dim); font-size: var(--fs-sm); }
.books-pills { display: flex; flex-wrap: wrap; gap: var(--s-1); margin: var(--s-2) 0 0; }
.books-pill { padding: 0 var(--s-2); border-radius: 999px; background: var(--c-bg-subtle); color: var(--c-text-dim); font-size: var(--fs-xs); }
.books-pill-shelf { background: var(--c-accent-bg); color: var(--c-accent); }
.books-description { max-width: 38rem; margin-bottom: var(--s-4); color: var(--c-text-dim); white-space: pre-line; }

/* Narrow screens: Reader's drill-down, with Books' ids. The back control
 * is a <label> for the checkbox, so it works with no script. */
.books-back-btn { display: none; margin-bottom: var(--s-2); }
@media (min-width: 901px) {
	.books-pane-toggle { display: none; }
}
#books-list-open:focus-visible ~ .books-listpane .books-back-btn,
#books-book-open:focus-visible ~ .books-book .books-back-btn { outline: var(--ring); }
@media (max-width: 900px) {
	.books-panes-row > .pane-gutter { display: none; }
	.books-side { flex: 0 0 12rem; }
	.books-listpane { flex: 1 1 auto; }
	.books-book { display: none; }
	#books-book-open:checked ~ .books-listpane { display: none; }
	#books-book-open:checked ~ .books-book { display: block; }
	#books-book-open:checked ~ .books-book .books-back-btn { display: inline-flex; }
}
@media (max-width: 640px) {
	.books-panes { --books-chrome: calc(8rem + var(--s-4) * 2); }
	.books-side { flex: 1 1 100%; }
	.books-listpane { display: none; }
	#books-list-open:checked ~ .books-side { display: none; }
	#books-list-open:checked ~ .books-listpane { display: flex; }
	#books-list-open:checked ~ .books-listpane .books-back-btn { display: inline-flex; }
	#books-book-open:checked ~ .books-listpane { display: none; }
}
```

- [ ] **Step 8: Run the tests, then the full check**

Run: `go test ./internal/apps/books/... ./internal/ui/... -count=1`
Expected: PASS. Then the full check from Global Constraints.

- [ ] **Step 9: Commit**

```bash
git add -A internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): three-pane shelves, list and book view (#487)"
```

---

### Task 6: Add and edit a book

**Files:**
- Create: `internal/apps/books/form.go`, `internal/apps/books/templates/form.html`
- Modify: `internal/apps/books/books.go` (routes), `internal/apps/books/view.go` (`EditURL`), `internal/apps/books/templates/panes.partial.html` (book menu), `internal/ui/static/app.css`
- Test: `internal/apps/books/form_test.go`

**Interfaces:**
- Consumes: `Create`, `Update`, `Get`, `NewBook`, `ValidationError`, `Refusal`, `ParseTags`, `ParseShelf`, `listCtx`, `ctxFrom`, `pathID`, `fail` (Tasks 2–5).
- Produces: routes `GET/POST /books/new`, `GET/POST /books/edit/{id}`; `func (c listCtx) EditURL(id int64) string`; template page `books/form`; block `book-menu` (the book pane's ⋯ menu, Task 7 adds Delete); form field names `title subtitle authors year pages isbn series_name series_number description add_to finished_on tags`.

- [ ] **Step 1: Write the failing tests**

`internal/apps/books/form_test.go`:

```go
package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestNewFormOffersTheShelves(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/books/new")
	doc.MustHave(`input[name="title"]`)
	if n := len(doc.QueryAll(`input[name="add_to"]`)); n != 3 {
		t.Errorf("%d add_to choices, want 3 (want, reading, read)", n)
	}
	doc.MustHave(`input[name="finished_on"]`)
	doc.MustHave(`input[name="tags"]`)
}

func TestCreateAddsTheBookAndOpensIt(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	s.Submit(t, s.Alice, "/books/new", url.Values{
		"title": {"Leviathan Wakes"}, "authors": {"James S. A. Corey"}, "year": {"2011"}, "pages": {"592"},
		"series_name": {"The Expanse"}, "series_number": {"1"}, "add_to": {"reading"}, "tags": {"SF, space"},
	}, "/books/b/1?shelf=reading")
	b, err := s.Store.Get(context.Background(), s.Alice.User.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Leviathan Wakes" || b.Pages != 592 || b.SeriesNumber != "1" || b.Shelf != books.ShelfReading {
		t.Errorf("created %+v on %q", b.BookInput, b.Shelf)
	}
	if !slices.Equal(b.Tags, []string{"sf", "space"}) {
		t.Errorf("tags = %v", b.Tags)
	}
}

func TestCreateAlreadyReadUsesTheDate(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	s.Submit(t, s.Alice, "/books/new", url.Values{
		"title": {"Emma"}, "add_to": {"read"}, "finished_on": {"2026-09-01"},
	}, "/books/b/1?shelf=read")
	b, _ := s.Store.Get(context.Background(), s.Alice.User.ID, 1)
	if b.Latest.FinishedOn != "2026-09-01" {
		t.Errorf("finished %q, want 2026-09-01", b.Latest.FinishedOn)
	}
}

func TestCreateWithMistakesComesBackWithMessages(t *testing.T) {
	s := newServer(t)
	rec := s.Post(t, s.Alice, "/books/new", url.Values{
		"title": {""}, "year": {"abc"}, "isbn": {"12345"}, "authors": {"Someone"}, "add_to": {"want"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST = %d, want 422", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	for _, field := range []string{"title", "year", "isbn"} {
		doc.MustHave("#books-" + field + "-error")
	}
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name="year"]`), "value"); v != "abc" {
		t.Errorf("year echoed as %q, want what was typed", v)
	}
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name="authors"]`), "value"); v != "Someone" {
		t.Errorf("authors echoed as %q", v)
	}
	if items, _ := s.Store.List(context.Background(), s.Alice.User.ID, books.ListQuery{}); len(items) != 0 {
		t.Errorf("a rejected form saved %d books", len(items))
	}
}

func TestCreateRefusesAFutureFinishDate(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	rec := s.Post(t, s.Alice, "/books/new", url.Values{
		"title": {"Emma"}, "add_to": {"read"}, "finished_on": {"2027-01-01"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST = %d, want 422", rec.Code)
	}
	htmlassert.Parse(t, rec.Body.String()).MustHave("#books-finished_on-error")
}

func TestEditFormShowsTheBookAndSaves(t *testing.T) {
	s := newServer(t)
	nb := titled("Dune", "Frank Herbert", books.ShelfReading)
	nb.Pages = 412
	id := add(t, s, s.Alice.User.ID, nb)

	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/edit/%d?shelf=all&q=dune", id))
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name="pages"]`), "value"); v != "412" {
		t.Errorf("pages = %q, want 412", v)
	}
	doc.MustNotHave(`input[name="add_to"]`)
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name="q"]`), "value"); v != "dune" {
		t.Errorf("hidden q = %q, want the list context carried", v)
	}

	s.Submit(t, s.Alice, fmt.Sprintf("/books/edit/%d", id), url.Values{
		"title": {"Dune"}, "authors": {"Frank Herbert"}, "pages": {"896"}, "shelf": {"all"}, "q": {"dune"},
	}, fmt.Sprintf("/books/b/%d?q=dune&shelf=all", id))
	if b, _ := s.Store.Get(context.Background(), s.Alice.User.ID, id); b.Pages != 896 {
		t.Errorf("pages = %d after edit, want 896", b.Pages)
	}
}

func TestEditingSomeoneElsesBookIsNotFound(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))
	path := fmt.Sprintf("/books/edit/%d", id)
	if rec := s.Do(t, s.Bob, httptestGet(path)); rec.Code != http.StatusNotFound {
		t.Errorf("Bob GET %s = %d, want 404", path, rec.Code)
	}
	if rec := s.Post(t, s.Bob, path, url.Values{"title": {"Mine"}}); rec.Code != http.StatusNotFound {
		t.Errorf("Bob POST %s = %d, want 404", path, rec.Code)
	}
}

func TestBookPaneLinksToEdit(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d?shelf=want", id))
	doc.MustHave(fmt.Sprintf(`a[href="/books/edit/%d?shelf=want"]`, id))
}
```

Add to `handlers_test.go`:

```go
func httptestGet(path string) *http.Request { return httptest.NewRequest("GET", path, nil) }
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -run 'Form|Create|Edit' -count=1`
Expected: FAIL — 404s for `/books/new` and `/books/edit/…`.

- [ ] **Step 3: Add `EditURL` to `view.go`**

```go
// EditURL is a book's edit page, coming back to this list afterwards.
func (c listCtx) EditURL(id int64) string {
	return "/books/edit/" + strconv.FormatInt(id, 10) + "?" + c.Query()
}
```

- [ ] **Step 4: Write `form.go`**

```go
package books

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// formValues is what the book form shows: the raw text, so a mistyped
// number comes back exactly as typed. AddTo, FinishedOn and Tags are for a
// new book only.
type formValues struct {
	Title, Subtitle, Authors, Year, Pages, ISBN string
	SeriesName, SeriesNumber, Description       string
	AddTo, FinishedOn, Tags                     string
}

func valuesOf(in BookInput) formValues {
	return formValues{Title: in.Title, Subtitle: in.Subtitle, Authors: in.Authors,
		Year: numText(in.Year), Pages: numText(in.Pages), ISBN: in.ISBN,
		SeriesName: in.SeriesName, SeriesNumber: in.SeriesNumber, Description: in.Description}
}

func numText(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// formView is the Add/Edit book page.
type formView struct {
	New     bool
	Action  string
	Heading string
	Submit  string
	Cancel  string
	Values  formValues
	Errors  FieldErrors
	Today   string // the latest finish date the form allows
	Ctx     listCtx
}

// parseForm reads a posted book form: the input, the raw values to echo
// back, and a message per number field that isn't a whole number.
func parseForm(get func(string) string) (BookInput, formValues, FieldErrors) {
	v := formValues{Title: get("title"), Subtitle: get("subtitle"), Authors: get("authors"),
		Year: get("year"), Pages: get("pages"), ISBN: get("isbn"),
		SeriesName: get("series_name"), SeriesNumber: get("series_number"), Description: get("description"),
		AddTo: get("add_to"), FinishedOn: strings.TrimSpace(get("finished_on")), Tags: get("tags")}
	in := BookInput{Title: v.Title, Subtitle: v.Subtitle, Authors: v.Authors, ISBN: v.ISBN,
		SeriesName: v.SeriesName, SeriesNumber: v.SeriesNumber, Description: v.Description}
	errs := FieldErrors{}
	number := func(field, raw string, dst *int) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			errs[field] = "Enter a whole number, or leave it empty."
			return
		}
		*dst = n
	}
	number("year", v.Year, &in.Year)
	number("pages", v.Pages, &in.Pages)
	return in, v, errs
}

// merge adds b's messages for fields a has nothing to say about: a "whole
// number" complaint beats the range check of the same field.
func merge(a, b FieldErrors) FieldErrors {
	for k, msg := range b {
		if _, ok := a[k]; !ok {
			a[k] = msg
		}
	}
	return a
}

func (a *App) renderForm(w http.ResponseWriter, r *http.Request, status int, v formView) {
	page := a.deps.Page(r, v.Heading)
	page.Data = v
	if err := a.deps.Render.Page(w, status, "books/form", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

func (a *App) newBookForm(v formValues, errs FieldErrors) formView {
	return formView{New: true, Action: "/books/new", Heading: "Add a book", Submit: "Add book",
		Cancel: "/books/", Values: v, Errors: errs, Today: a.store.Today()}
}

func (a *App) newForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	a.renderForm(w, r, http.StatusOK, a.newBookForm(formValues{AddTo: string(ShelfWant)}, nil))
}

func (a *App) create(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	in, vals, errs := parseForm(r.PostFormValue)
	errs = merge(errs, in.Normalize().Validate())
	shelf, _ := ParseShelf(vals.AddTo)
	if len(errs) > 0 {
		a.renderForm(w, r, http.StatusUnprocessableEntity, a.newBookForm(vals, errs))
		return
	}
	id, err := a.store.Create(r.Context(), uid, NewBook{BookInput: in, Shelf: shelf,
		FinishedOn: vals.FinishedOn, Tags: ParseTags(vals.Tags)})
	var verr *ValidationError
	var ref *Refusal
	switch {
	case errors.As(err, &verr):
		a.renderForm(w, r, http.StatusUnprocessableEntity, a.newBookForm(vals, verr.Fields))
		return
	case errors.As(err, &ref):
		a.renderForm(w, r, http.StatusUnprocessableEntity, a.newBookForm(vals, FieldErrors{"finished_on": ref.Msg}))
		return
	case errors.Is(err, ErrInvalid):
		a.renderForm(w, r, http.StatusUnprocessableEntity, a.newBookForm(vals, FieldErrors{"add_to": "Pick where the book goes."}))
		return
	case err != nil:
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, listCtx{Shelf: shelf}.BookURL(id), http.StatusSeeOther)
}

func editBookForm(id int64, v formValues, errs FieldErrors, c listCtx) formView {
	return formView{Action: "/books/edit/" + strconv.FormatInt(id, 10), Heading: "Edit book", Submit: "Save",
		Cancel: c.BookURL(id), Values: v, Errors: errs, Ctx: c}
}

func (a *App) editForm(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	b, err := a.store.Get(r.Context(), uid, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderForm(w, r, http.StatusOK, editBookForm(id, valuesOf(b.BookInput), nil, ctxFrom(r.FormValue)))
}

func (a *App) update(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	c := ctxFrom(r.PostFormValue)
	in, vals, errs := parseForm(r.PostFormValue)
	errs = merge(errs, in.Normalize().Validate())
	if len(errs) > 0 {
		// Someone else's book is a 404 even when the form is wrong too.
		if _, err := a.store.Get(r.Context(), uid, id); err != nil {
			a.fail(w, r, err)
			return
		}
		a.renderForm(w, r, http.StatusUnprocessableEntity, editBookForm(id, vals, errs, c))
		return
	}
	if err := a.store.Update(r.Context(), uid, id, in); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, c.BookURL(id), http.StatusSeeOther)
}
```

In `books.go`'s `Mount`, add:

```go
	r.HandleFunc("GET /new", a.newForm)
	r.HandleFunc("POST /new", a.create)
	r.HandleFunc("GET /edit/{id}", a.editForm)
	r.HandleFunc("POST /edit/{id}", a.update)
```

- [ ] **Step 5: Write `templates/form.html`**

```html
{{define "content"}}
{{$d := .Data}}
<form class="books-form stack" method="post" action="{{$d.Action}}" novalidate>
	<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
	{{if not $d.New}}{{template "ctx-fields" $d.Ctx}}{{end}}
	<h1>{{$d.Heading}}</h1>
	{{if $d.Errors}}<div class="notice notice-error" role="alert">Please fix the fields marked below.</div>{{end}}

	{{template "text-field" (dict "Name" "title" "Label" "Title" "Value" $d.Values.Title "Error" (index $d.Errors "title"))}}
	{{template "text-field" (dict "Name" "subtitle" "Label" "Subtitle" "Value" $d.Values.Subtitle "Error" (index $d.Errors "subtitle"))}}
	{{template "text-field" (dict "Name" "authors" "Label" "Author(s)" "Placeholder" "Ursula K. Le Guin" "Value" $d.Values.Authors "Error" (index $d.Errors "authors"))}}
	<div class="books-form-row">
		{{template "text-field" (dict "Name" "year" "Label" "First published" "Mode" "numeric" "Value" $d.Values.Year "Error" (index $d.Errors "year"))}}
		{{template "text-field" (dict "Name" "pages" "Label" "Pages" "Mode" "numeric" "Value" $d.Values.Pages "Error" (index $d.Errors "pages"))}}
		{{template "text-field" (dict "Name" "isbn" "Label" "ISBN" "Value" $d.Values.ISBN "Error" (index $d.Errors "isbn"))}}
	</div>
	<div class="books-form-row">
		{{template "text-field" (dict "Name" "series_name" "Label" "Series" "Value" $d.Values.SeriesName "Error" (index $d.Errors "series_name"))}}
		{{template "text-field" (dict "Name" "series_number" "Label" "Number in series" "Value" $d.Values.SeriesNumber "Error" (index $d.Errors "series_number"))}}
	</div>
	<div class="field">
		<label for="books-description">Description</label>
		<textarea id="books-description" name="description" rows="5"{{if index $d.Errors "description"}} aria-invalid="true" aria-describedby="books-description-error"{{end}}>{{$d.Values.Description}}</textarea>
		{{template "field-error" (dict "Name" "description" "Error" (index $d.Errors "description"))}}
	</div>

	{{if $d.New}}
	<fieldset class="field books-add-to"{{if index $d.Errors "add_to"}} aria-describedby="books-add_to-error"{{end}}>
		<legend>Add it to</legend>
		<label class="books-choice"><input type="radio" name="add_to" value="want"{{if eq $d.Values.AddTo "want"}} checked{{end}}> Want to read</label>
		<label class="books-choice"><input type="radio" name="add_to" value="reading"{{if eq $d.Values.AddTo "reading"}} checked{{end}}> Reading now — started today</label>
		<label class="books-choice"><input type="radio" name="add_to" value="read"{{if eq $d.Values.AddTo "read"}} checked{{end}}> Already read</label>
		{{template "field-error" (dict "Name" "add_to" "Error" (index $d.Errors "add_to"))}}
	</fieldset>
	{{/* Shown only while "Already read" is picked (CSS :has(), no script). */}}
	<div class="field books-finished">
		<label for="books-finished_on">Finished on</label>
		<input id="books-finished_on" name="finished_on" type="date" max="{{$d.Today}}" value="{{if $d.Values.FinishedOn}}{{$d.Values.FinishedOn}}{{else}}{{$d.Today}}{{end}}"{{if index $d.Errors "finished_on"}} aria-invalid="true" aria-describedby="books-finished_on-error"{{end}}>
		{{template "field-error" (dict "Name" "finished_on" "Error" (index $d.Errors "finished_on"))}}
	</div>
	{{template "text-field" (dict "Name" "tags" "Label" "Tags" "Placeholder" "sf, favourites" "Value" $d.Values.Tags "Error" "")}}
	{{end}}

	<div class="books-form-actions">
		<button class="primary" type="submit">{{$d.Submit}}</button>
		<a href="{{$d.Cancel}}">Cancel</a>
	</div>
</form>
{{end}}
```

Append to `panes.partial.html` (form helpers, shared by every page of the app, plus the book menu):

```html
{{/* text-field is one labelled text input with its error. Pass a dict
     with Name, Label, Value, Error and optionally Placeholder and Mode
     (inputmode). */}}
{{define "text-field"}}
<div class="field">
	<label for="books-{{.Name}}">{{.Label}}</label>
	<input id="books-{{.Name}}" name="{{.Name}}" type="text"{{with .Mode}} inputmode="{{.}}"{{end}}{{with .Placeholder}} placeholder="{{.}}"{{end}} value="{{.Value}}"{{if .Error}} aria-invalid="true" aria-describedby="books-{{.Name}}-error"{{end}}>
	{{template "field-error" .}}
</div>
{{end}}

{{define "field-error"}}{{with .Error}}<p class="books-field-error" id="books-{{$.Name}}-error">{{.}}</p>{{end}}{{end}}

{{/* book-menu is the book pane's ⋯ menu: a no-JS <details> disclosure
     (PATTERNS.md). Takes a bookView. */}}
{{define "book-menu"}}
<details class="outline-menu books-menu">
	<summary class="outline-menu-toggle quiet" aria-label="Book actions">{{ticon "more"}}</summary>
	<div class="outline-menu-list outline-menu-list-end">
		<a href="{{.Ctx.EditURL .ID}}">Edit details</a>
	</div>
</details>
{{end}}
```

In the `book` block, put the menu at the end of the header — change

```html
		</div>
	</header>
```
to
```html
		</div>
		{{template "book-menu" .}}
	</header>
```

- [ ] **Step 6: Add the form CSS**

Append to the ON Books section of `internal/ui/static/app.css`:

```css
/* Add / edit form */
.books-form { max-width: var(--measure); padding: var(--s-4) 0; }
.books-form fieldset { border: 0; padding: 0; margin-inline: 0; min-inline-size: 0; }
.books-form legend { font-weight: 600; margin-bottom: var(--s-2); }
.books-form-row { display: flex; flex-wrap: wrap; gap: var(--s-3); }
.books-form-row .field { flex: 1 1 8rem; }
.books-choice { display: flex; align-items: center; gap: var(--s-2); margin-bottom: var(--s-2); font-size: var(--fs-base); color: var(--c-text); }
.books-field-error { margin: var(--s-1) 0 0; color: var(--c-danger); font-size: var(--fs-sm); }
.books-finished { display: none; }
.books-form:has(input[name="add_to"][value="read"]:checked) .books-finished { display: block; }
.books-form-actions { display: flex; align-items: center; gap: var(--s-3); }
.books-menu { margin-left: auto; }
```

- [ ] **Step 7: Run the tests, then the full check**

Run: `go test ./internal/apps/books/... ./internal/ui/... -count=1`
Expected: PASS. Then the full check from Global Constraints.

- [ ] **Step 8: Commit**

```bash
git add -A internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): add and edit a book by hand (#487)"
```

---
### Task 7: Start, finish, did not finish, tags and delete

**Files:**
- Create: `internal/apps/books/actions.go`
- Modify: `internal/apps/books/books.go` (routes), `internal/apps/books/view.go` (`bookView`, `viewBook`), `internal/apps/books/templates/panes.partial.html` (`book`, `book-menu`, new blocks), `internal/apps/books/static/books.js` (confirm dialog), `internal/ui/static/app.css`
- Test: `internal/apps/books/actions_test.go`

**Interfaces:**
- Consumes: `StartReading`, `FinishReading`, `MarkDNF`, `SetTags`, `Delete`, `Refusal`, `ParseTags` (Tasks 3–4); `renderPanes`, `ctxFrom`, `pathID`, `fail` (Task 5).
- Produces: routes `POST /books/start/{id}`, `/finish/{id}` (field `day`), `/dnf/{id}` (field `day`), `/tags/{id}` (field `tags`), `/delete/{id}` — each also takes the list context fields `shelf`, `tag`, `q`. htmx posts target `#books-panes`. Blocks `reading-box`, `close-reading`, `post-ctx`, `tags-form`, `confirm`. Ids `books-confirm-dialog`, `books-confirm-message`, `books-confirm-ok`. New `bookView` fields `TagsValue`, `Reading`, `StartedOn`, `MinDay`, `Today`, `StartLabel`.

- [ ] **Step 1: Write the failing tests**

`internal/apps/books/actions_test.go`:

```go
package books_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func shelfOf(t *testing.T, s *server, id int64) books.Shelf {
	t.Helper()
	b, err := s.Store.Get(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	return b.Shelf
}

func TestStartMovesABookToReading(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	s.Submit(t, s.Alice, fmt.Sprintf("/books/start/%d", id), url.Values{"shelf": {"want"}},
		fmt.Sprintf("/books/b/%d?shelf=want", id))
	if got := shelfOf(t, s, id); got != books.ShelfReading {
		t.Errorf("shelf = %q, want reading", got)
	}
}

func TestActionsOverHTMXReturnThePanes(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	rec := s.PostHX(t, s.Alice, fmt.Sprintf("/books/start/%d", id), url.Values{"shelf": {"want"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx start = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#books-panes")
	if got := htmlassert.Text(doc.MustHave(".books-book h1")); got != "Emma" {
		t.Errorf("open book = %q, want Emma still open", got)
	}
	if rows := rowTitles(doc); len(rows) != 0 {
		t.Errorf("Want to read still lists %v after starting it", rows)
	}
}

func TestFinishUsesTheGivenDay(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC))
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfReading))
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	s.Submit(t, s.Alice, fmt.Sprintf("/books/finish/%d", id), url.Values{"shelf": {"reading"}, "day": {"2026-10-05"}},
		fmt.Sprintf("/books/b/%d?shelf=reading", id))
	b, _ := s.Store.Get(context.Background(), s.Alice.User.ID, id)
	if b.Shelf != books.ShelfRead || b.Latest.FinishedOn != "2026-10-05" {
		t.Errorf("after finish: shelf %q, latest %+v", b.Shelf, b.Latest)
	}
}

func TestARefusedFinishShowsTheBanner(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC))
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfReading))
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	path := fmt.Sprintf("/books/finish/%d", id)
	form := func() url.Values { return url.Values{"shelf": {"reading"}, "day": {"2026-09-01"}} }

	rec := s.PostHX(t, s.Alice, path, form())
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx refused finish = %d, want 200 so htmx swaps it", rec.Code)
	}
	banner := htmlassert.Text(htmlassert.Parse(t, rec.Body.String()).MustHave(".books-banner"))
	if !strings.Contains(banner, "before you started") {
		t.Errorf("banner = %q", banner)
	}
	if rec := s.Post(t, s.Alice, path, form()); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("refused finish without JavaScript = %d, want 422", rec.Code)
	}
	if got := shelfOf(t, s, id); got != books.ShelfReading {
		t.Errorf("shelf = %q after a refused finish, want reading", got)
	}
}

func TestDidNotFinish(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Infinite Jest", "", books.ShelfReading))
	s.Submit(t, s.Alice, fmt.Sprintf("/books/dnf/%d", id), url.Values{"shelf": {"reading"}},
		fmt.Sprintf("/books/b/%d?shelf=reading", id))
	if got := shelfOf(t, s, id); got != books.ShelfDNF {
		t.Errorf("shelf = %q, want dnf", got)
	}
}

func TestSetTagsFromTheBookPane(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))
	s.Submit(t, s.Alice, fmt.Sprintf("/books/tags/%d", id), url.Values{"shelf": {"want"}, "tags": {"Space, sf"}},
		fmt.Sprintf("/books/b/%d?shelf=want", id))
	tags, err := s.Store.BookTags(context.Background(), s.Alice.User.ID, id)
	if err != nil || !slices.Equal(tags, []string{"sf", "space"}) {
		t.Errorf("tags = %v, %v; want [sf space]", tags, err)
	}
}

func TestDeleteReturnsToTheList(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfWant))
	s.Submit(t, s.Alice, fmt.Sprintf("/books/delete/%d", id), url.Values{"shelf": {"want"}, "q": {"du"}},
		"/books/?q=du&shelf=want")
	if _, err := s.Store.Get(context.Background(), s.Alice.User.ID, id); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Get after delete = %v, want ErrNotFound", err)
	}
}

func TestActionsOnSomeoneElsesBookAreNotFound(t *testing.T) {
	s := newServer(t)
	id := add(t, s, s.Alice.User.ID, titled("Dune", "", books.ShelfReading))
	for _, action := range []string{"start", "finish", "dnf", "tags", "delete"} {
		path := fmt.Sprintf("/books/%s/%d", action, id)
		if rec := s.Post(t, s.Bob, path, url.Values{}); rec.Code != http.StatusNotFound {
			t.Errorf("Bob POST %s = %d, want 404", path, rec.Code)
		}
	}
	if got := shelfOf(t, s, id); got != books.ShelfReading {
		t.Errorf("Bob changed Alice's book: shelf %q", got)
	}
}

func TestBookPaneOffersTheRightReadingActions(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC))
	uid := s.Alice.User.ID
	want := add(t, s, uid, titled("Emma", "", books.ShelfWant))
	reading := add(t, s, uid, titled("Dune", "", books.ShelfReading))
	read := add(t, s, uid, titled("Ulysses", "", books.ShelfRead))

	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", want))
	if got := htmlassert.Text(doc.MustHave(".books-start")); got != "Start reading" {
		t.Errorf("want-to-read button = %q", got)
	}
	doc.MustNotHave("details.books-close")

	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", reading))
	if n := len(doc.QueryAll("details.books-close")); n != 2 {
		t.Errorf("%d close disclosures, want Finish and Did not finish", n)
	}
	day := doc.MustHave(`input[name="day"]`)
	if v, _ := htmlassert.Attr(day, "max"); v != "2026-10-09" {
		t.Errorf("day max = %q, want today 2026-10-09", v)
	}
	if v, _ := htmlassert.Attr(day, "min"); v != "2026-10-09" {
		t.Errorf("day min = %q, want the start 2026-10-09", v)
	}
	doc.MustHave("button[hx-confirm]")
	doc.MustHave("#books-confirm-dialog")

	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", read))
	if got := htmlassert.Text(doc.MustHave(".books-start")); got != "Read again" {
		t.Errorf("read button = %q, want Read again", got)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/books/... -count=1`
Expected: FAIL — 404s for the action routes, missing `.books-start`.

- [ ] **Step 3: Write `actions.go`**

```go
package books

import (
	"errors"
	"net/http"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// change is one thing done to a book from the book pane.
type change func(r *http.Request, userID, id int64) error

// act runs a change and answers with the panes. htmx gets the whole panes
// (shelf counts, the list and the book can all move); without JavaScript
// it is a redirect back to the book — or to the list, once the book is
// gone. A Refusal is the banner over the panes rather than an error page.
func (a *App) act(do change, gone bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := a.userID(w, r)
		if !ok {
			return
		}
		id, ok := a.pathID(w, r)
		if !ok {
			return
		}
		c := ctxFrom(r.PostFormValue)
		err := do(r, uid, id)
		var ref *Refusal
		switch {
		case errors.As(err, &ref):
			a.renderPanes(w, r, uid, c, id, ref.Msg)
			return
		case err != nil:
			a.fail(w, r, err)
			return
		}
		open, target := id, c.BookURL(id)
		if gone {
			open, target = 0, c.ListURL()
		}
		if web.IsHTMX(r) {
			a.renderPanes(w, r, uid, c, open, "")
			return
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

func (a *App) start(r *http.Request, userID, id int64) error {
	return a.store.StartReading(r.Context(), userID, id)
}

func (a *App) finish(r *http.Request, userID, id int64) error {
	return a.store.FinishReading(r.Context(), userID, id, strings.TrimSpace(r.PostFormValue("day")))
}

func (a *App) dnf(r *http.Request, userID, id int64) error {
	return a.store.MarkDNF(r.Context(), userID, id, strings.TrimSpace(r.PostFormValue("day")))
}

func (a *App) setTags(r *http.Request, userID, id int64) error {
	return a.store.SetTags(r.Context(), userID, id, ParseTags(r.PostFormValue("tags")))
}

func (a *App) remove(r *http.Request, userID, id int64) error {
	return a.store.Delete(r.Context(), userID, id)
}
```

In `books.go`'s `Mount`, add:

```go
	r.HandleFunc("POST /start/{id}", a.act(a.start, false))
	r.HandleFunc("POST /finish/{id}", a.act(a.finish, false))
	r.HandleFunc("POST /dnf/{id}", a.act(a.dnf, false))
	r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
```

- [ ] **Step 4: Extend the book view**

In `view.go`, replace `bookView` and `viewBook` with:

```go
// bookView is the book pane. Selected is false when no book is open.
type bookView struct {
	Selected                         bool
	ID                               int64
	Title, Subtitle, Authors, Series string
	Facts                            []string // "2011", "592 pages", "ISBN 978…"
	Description                      string
	Spine, Initial                   string
	ShelfLabel                       string
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

// viewBook draws a book; today bounds the reading box's date fields.
func viewBook(b Book, c listCtx, today string) bookView {
	v := bookView{Selected: true, ID: b.ID, Title: b.Title, Subtitle: b.Subtitle, Authors: b.Authors,
		Series: seriesText(b.SeriesName, b.SeriesNumber), Description: b.Description,
		Spine: SpineColor(b.Title), Initial: initial(b.Title), ShelfLabel: b.Shelf.Label(),
		Tags: b.Tags, TagsValue: strings.Join(b.Tags, ", "), Today: today, Ctx: c}
	if b.Year > 0 {
		v.Facts = append(v.Facts, strconv.Itoa(b.Year))
	}
	if b.Pages > 0 {
		v.Facts = append(v.Facts, strconv.Itoa(b.Pages)+" pages")
	}
	if b.ISBN != "" {
		v.Facts = append(v.Facts, "ISBN "+b.ISBN)
	}
	switch {
	case b.Latest.Status == StatusReading:
		v.Reading = true
		v.MinDay = b.Latest.StartedOn
		if b.Latest.StartedOn != "" {
			v.StartedOn = ShowDay(b.Latest.StartedOn)
		}
	case b.Shelf == ShelfRead:
		v.StartLabel = "Read again"
	case b.Shelf == ShelfDNF:
		v.StartLabel = "Start again"
	default:
		v.StartLabel = "Start reading"
	}
	return v
}
```

- [ ] **Step 5: Update the templates**

In `panes.partial.html`, replace the `{{with .Description}}…{{end}}` line inside the `book` block with:

```html
	{{template "reading-box" .}}
	{{with .Description}}<div class="books-description">{{.}}</div>{{end}}
	{{template "tags-form" .}}
```

Replace the `book-menu` block with:

```html
{{/* book-menu is the book pane's ⋯ menu: a no-JS <details> disclosure
     (PATTERNS.md). Delete goes through the confirm dialog (books.js);
     without JavaScript it simply submits. Takes a bookView. */}}
{{define "book-menu"}}
<details class="outline-menu books-menu">
	<summary class="outline-menu-toggle quiet" aria-label="Book actions">{{ticon "more"}}</summary>
	<div class="outline-menu-list outline-menu-list-end">
		<a href="{{.Ctx.EditURL .ID}}">Edit details</a>
		<form method="post" action="/books/delete/{{.ID}}">
			{{template "post-ctx" .}}
			<button type="submit" class="outline-menu-delete"
			        hx-post="/books/delete/{{.ID}}" hx-target="#books-panes" hx-swap="outerHTML"
			        hx-confirm="Delete “{{.Title}}” for good? Its readings and tags go too.">Delete</button>
		</form>
	</div>
</details>
{{end}}
```

Append the new blocks:

```html
{{/* post-ctx is what every book-pane POST carries: the CSRF token and the
     list context. Takes a bookView. */}}
{{define "post-ctx"}}<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">{{template "ctx-fields" .Ctx}}{{end}}

{{/* reading-box is the current reading: Finish / Did not finish while one
     is in progress, otherwise a button to start one. Takes a bookView. */}}
{{define "reading-box"}}
<div class="books-reading">
	{{if .Reading}}
	<p class="books-reading-status">Reading{{with .StartedOn}} since {{.}}{{end}}</p>
	<div class="books-reading-actions">
		{{template "close-reading" (dict "Book" . "Action" "finish" "Label" "Finish" "DayLabel" "Finished on" "Submit" "Mark as read" "Primary" true)}}
		{{template "close-reading" (dict "Book" . "Action" "dnf" "Label" "Did not finish" "DayLabel" "Stopped on" "Submit" "Put it down" "Primary" false)}}
	</div>
	{{else}}
	<form method="post" action="/books/start/{{.ID}}" hx-post="/books/start/{{.ID}}" hx-target="#books-panes" hx-swap="outerHTML">
		{{template "post-ctx" .}}
		<button type="submit" class="primary books-start">{{.StartLabel}}</button>
	</form>
	{{end}}
</div>
{{end}}

{{/* close-reading is Finish or Did not finish: a no-JS <details>
     disclosure (PATTERNS.md) holding the date — today unless changed, never
     before the start — and the button that sends it. */}}
{{define "close-reading"}}
<details class="books-close">
	<summary class="button{{if .Primary}} primary{{end}}">{{.Label}}</summary>
	<form class="books-close-form" method="post" action="/books/{{.Action}}/{{.Book.ID}}"
	      hx-post="/books/{{.Action}}/{{.Book.ID}}" hx-target="#books-panes" hx-swap="outerHTML">
		{{template "post-ctx" .Book}}
		<label for="books-{{.Action}}-day">{{.DayLabel}}</label>
		<input id="books-{{.Action}}-day" name="day" type="date" value="{{.Book.Today}}" max="{{.Book.Today}}"{{with .Book.MinDay}} min="{{.}}"{{end}}>
		<button type="submit"{{if .Primary}} class="primary"{{end}}>{{.Submit}}</button>
	</form>
</details>
{{end}}

{{/* tags-form edits a book's tags in place. Takes a bookView. */}}
{{define "tags-form"}}
<form class="books-tags-form" method="post" action="/books/tags/{{.ID}}"
      hx-post="/books/tags/{{.ID}}" hx-target="#books-panes" hx-swap="outerHTML">
	{{template "post-ctx" .}}
	<label for="books-tags-input">Tags</label>
	<div class="books-tags-row">
		<input id="books-tags-input" name="tags" type="text" value="{{.TagsValue}}" placeholder="sf, favourites" autocomplete="off">
		<button type="submit">Save tags</button>
	</div>
</form>
{{end}}

{{/* confirm is the one dialog every hx-confirm in the panes goes through
     (books.js), instead of window.confirm. */}}
{{define "confirm"}}
<dialog id="books-confirm-dialog" class="books-dialog">
	<p id="books-confirm-message"></p>
	<div class="dialog-actions">
		<button type="button" id="books-confirm-ok" class="danger">Delete</button>
		<button type="button" class="books-dialog-cancel">Cancel</button>
	</div>
</dialog>
{{end}}
```

In the `panes` block, add `{{template "confirm"}}` just before the closing `</div>` of `.books-panes` (after `.books-panes-row`'s closing tag).

- [ ] **Step 6: Write the confirm dialog in `books.js`**

Replace `internal/apps/books/static/books.js` with:

```js
// ON Books' script: the delete confirmation, and (Task 8) resizable panes,
// keyboard shortcuts and the open book's row highlight. Vanilla and
// CSP-clean; the sections mirror reader.js's (apps never share app
// scripts).
(function () {
	"use strict";

	// --- Confirm dialog, replacing window.confirm for hx-confirm ----------
	//
	// Mirrors reader.js's, including the #551 fix: the OK listener belongs
	// to one opening through an AbortController and goes as soon as OK or
	// Cancel is pressed — not on "close", which Chrome can hold back in a
	// hidden tab, so a declined delete would otherwise fire with the next.
	var confirmController = null;
	document.addEventListener("htmx:confirm", function (e) {
		var panes = document.getElementById("books-panes");
		if (!panes || !e.target || !panes.contains(e.target)) return;
		var msg = e.target.getAttribute("hx-confirm");
		if (!msg) return;
		var dialog = document.getElementById("books-confirm-dialog");
		if (!dialog || typeof dialog.showModal !== "function") return; // htmx falls back to window.confirm

		e.preventDefault();
		document.getElementById("books-confirm-message").textContent = msg;
		var ok = document.getElementById("books-confirm-ok");
		var cancel = dialog.querySelector(".books-dialog-cancel");

		if (confirmController) confirmController.abort();
		var controller = confirmController = new AbortController();
		dialog.addEventListener("close", function () {
			if (dialog.open) return;
			controller.abort();
		}, { signal: controller.signal });
		cancel.addEventListener("click", function () {
			controller.abort();
			dialog.close();
		}, { signal: controller.signal });
		ok.addEventListener("click", function () {
			controller.abort();
			dialog.close();
			// true: skip the confirm gate, or htmx would ask again natively.
			e.detail.issueRequest(true);
		}, { signal: controller.signal });
		dialog.showModal();
	});
})();
```

- [ ] **Step 7: Add the CSS**

Append to the ON Books section of `internal/ui/static/app.css`:

```css
/* Reading box, tags, confirm dialog */
.books-reading { margin-bottom: var(--s-4); padding: var(--s-3); border: var(--border); border-radius: var(--radius); background: var(--c-bg-subtle); }
.books-reading-status { margin: 0 0 var(--s-2); font-weight: 600; }
.books-reading-actions { display: flex; flex-wrap: wrap; align-items: flex-start; gap: var(--s-2); }
.books-close > summary { display: inline-flex; list-style: none; cursor: pointer; }
.books-close > summary::-webkit-details-marker { display: none; }
.books-close-form { display: flex; flex-wrap: wrap; align-items: flex-end; gap: var(--s-2); margin-top: var(--s-2); }
.books-close-form label { margin: 0; flex-basis: 100%; }
.books-tags-form { max-width: 28rem; }
.books-tags-row { display: flex; gap: var(--s-2); }
.books-tags-row input { flex: 1 1 auto; min-width: 0; }
.books-tags-row button { white-space: nowrap; }
.books-dialog { width: min(28rem, calc(100vw - 2rem)); padding: 1rem; border: var(--border); border-radius: var(--radius); color: var(--c-text); background: var(--c-bg); }
.books-dialog::backdrop { background: rgba(0, 0, 0, 0.35); }
```

- [ ] **Step 8: Run the tests, then the full check**

Run: `go test ./internal/apps/books/... ./internal/ui/... -count=1`
Expected: PASS. Then the full check from Global Constraints.

- [ ] **Step 9: Commit**

```bash
git add -A internal/apps/books internal/ui/static/app.css
git commit -m "feat(books): start, finish, did not finish, tags and delete (#487)"
```

---

### Task 8: Resizable panes, keyboard shortcuts and the row highlight

**Files:**
- Modify: `internal/apps/books/static/books.js`

**Interfaces:**
- Consumes: ids `books-panes`, `books-panes-row`, `books-list`, `books-book` (with `data-book-id`), `books-q`; classes `.pane-gutter[data-gutter-for=side|list]`, `.books-side`, `.books-listpane`, `.books-row[data-book-id]`, `.books-row-link`, `details.books-close`, `details.books-menu` (Tasks 5–7).
- Produces: CSS custom properties `--books-side-w`, `--books-list-w` on `<html>`, stored under `localStorage["books.paneWidths"]`; keys `j`/`k` (next/previous book), `/` (filter), `a` (add a book), `Esc` (close an open Finish/DNF/⋯ disclosure; leave the filter).

There is no JavaScript test runner in this repository (no Node). This task is checked by reading it against reader.js and in the browser in Task 10.

- [ ] **Step 1: Add the sections**

In `books.js`, add after the confirm-dialog section, inside the IIFE:

```js
	// --- Resizable panes ---------------------------------------------------
	//
	// reader.js's, with Books' panes. Desktop only (the 900px breakpoint
	// where the CSS collapses the layout); below it the stored widths are
	// cleared rather than left to misapply. The widths live on <html>, not
	// on the row, so a full panes swap doesn't snap them back (#453).
	var PANE_STORE_KEY = "books.paneWidths";
	var PANE_MIN = { side: 10, list: 16 }; // rem
	var PANE_MAX = { side: 22, list: 36 }; // rem
	var DESKTOP_QUERY = window.matchMedia("(min-width: 901px)");

	function remToPx(rem) {
		var rootPx = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
		return rem * rootPx;
	}

	function clampPx(px, key) {
		return Math.min(remToPx(PANE_MAX[key]), Math.max(remToPx(PANE_MIN[key]), px));
	}

	function paneFor(row, key) {
		return row.querySelector(key === "side" ? ".books-side" : ".books-listpane");
	}

	function setWidth(key, px) {
		document.documentElement.style.setProperty("--books-" + key + "-w", clampPx(px, key) + "px");
	}

	function loadPaneWidths() {
		try {
			var parsed = JSON.parse(window.localStorage.getItem(PANE_STORE_KEY) || "null");
			if (!parsed || typeof parsed.side !== "number" || typeof parsed.list !== "number") return null;
			return parsed;
		} catch (e) {
			return null;
		}
	}

	function savePaneWidths(row) {
		try {
			window.localStorage.setItem(PANE_STORE_KEY, JSON.stringify({
				side: Math.round(paneFor(row, "side").getBoundingClientRect().width),
				list: Math.round(paneFor(row, "list").getBoundingClientRect().width),
			}));
		} catch (e) {
			// Private browsing or a full quota: the drag worked, it just won't be remembered.
		}
	}

	function syncPaneWidths() {
		var root = document.documentElement;
		if (!DESKTOP_QUERY.matches) {
			root.style.removeProperty("--books-side-w");
			root.style.removeProperty("--books-list-w");
			return;
		}
		var stored = loadPaneWidths();
		if (!stored) return;
		setWidth("side", stored.side);
		setWidth("list", stored.list);
	}

	function initResizablePanes() {
		var row = document.getElementById("books-panes-row");
		if (!row) return;
		var dragging = null; // { key, startX, startWidth }

		row.querySelectorAll(".pane-gutter").forEach(function (gutter) {
			var key = gutter.getAttribute("data-gutter-for");
			gutter.addEventListener("pointerdown", function (e) {
				if (!DESKTOP_QUERY.matches) return;
				dragging = { key: key, startX: e.clientX, startWidth: paneFor(row, key).getBoundingClientRect().width };
				gutter.classList.add("is-dragging");
				gutter.setPointerCapture(e.pointerId);
				e.preventDefault();
			});
			// The WAI-ARIA separator pattern: arrows nudge the pane.
			gutter.addEventListener("keydown", function (e) {
				if (!DESKTOP_QUERY.matches) return;
				if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
				setWidth(key, paneFor(row, key).getBoundingClientRect().width + (e.key === "ArrowRight" ? 16 : -16));
				savePaneWidths(row);
				e.preventDefault();
			});
		});

		row.addEventListener("pointermove", function (e) {
			if (!dragging) return;
			setWidth(dragging.key, dragging.startWidth + (e.clientX - dragging.startX));
		});
		function endDrag() {
			if (!dragging) return;
			dragging = null;
			row.querySelectorAll(".pane-gutter.is-dragging").forEach(function (g) {
				g.classList.remove("is-dragging");
			});
			savePaneWidths(row);
		}
		row.addEventListener("pointerup", endDrag);
		row.addEventListener("pointercancel", endDrag);
	}

	document.addEventListener("DOMContentLoaded", function () {
		syncPaneWidths();
		DESKTOP_QUERY.addEventListener("change", syncPaneWidths);
		initResizablePanes();
	});
	// A full panes swap (any change to a book) brings new gutters to bind.
	document.addEventListener("htmx:afterSettle", function (e) {
		if (e.target && e.target.id === "books-panes") initResizablePanes();
	});
	// Back/forward restores <body> from htmx's history cache: new gutters,
	// no afterSettle (#456).
	document.addEventListener("htmx:historyRestore", initResizablePanes);

	// --- The open book's row ------------------------------------------------
	//
	// A full render marks it on the server; these two cover what the server
	// doesn't see: a click that opens a book (only the book pane is swapped),
	// and a list swap, which leaves the book pane as it was.
	function markActive(id) {
		document.querySelectorAll(".books-row").forEach(function (li) {
			li.classList.toggle("is-active", id !== "" && li.getAttribute("data-book-id") === id);
		});
	}

	document.addEventListener("click", function (e) {
		var link = e.target.closest(".books-row-link");
		if (link) markActive(link.parentNode.getAttribute("data-book-id"));
	});

	document.addEventListener("htmx:afterSwap", function (e) {
		if (!e.target || e.target.id !== "books-list") return;
		var book = document.getElementById("books-book");
		markActive((book && book.getAttribute("data-book-id")) || "");
	});

	// --- Keyboard shortcuts --------------------------------------------------

	// Keys are ignored while typing, so "/" in the filter box is a slash.
	function isTyping(el) {
		if (!el) return false;
		var tag = el.tagName;
		return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable;
	}

	function step(delta) {
		var links = Array.prototype.slice.call(document.querySelectorAll(".books-row-link"));
		if (!links.length) return;
		var active = document.querySelector(".books-row.is-active .books-row-link");
		var i = active ? links.indexOf(active) : -1;
		var next = i === -1 ? 0 : Math.min(links.length - 1, Math.max(0, i + delta));
		if (next === i) return;
		links[next].click();
		links[next].scrollIntoView({ block: "nearest" });
	}

	document.addEventListener("keydown", function (e) {
		if (e.defaultPrevented || e.repeat || e.altKey || e.ctrlKey || e.metaKey) return;
		if (!document.getElementById("books-panes")) return;
		if (isTyping(e.target)) {
			if (e.key === "Escape" && e.target.id === "books-q") e.target.blur();
			return;
		}
		switch (e.key) {
		case "j":
			step(1);
			break;
		case "k":
			step(-1);
			break;
		case "/":
			var q = document.getElementById("books-q");
			if (!q) return;
			q.focus();
			q.select();
			break;
		case "a":
			window.location.href = "/books/new";
			break;
		case "Escape":
			document.querySelectorAll("details.books-close[open], details.books-menu[open]").forEach(function (d) {
				d.open = false;
			});
			return;
		default:
			return;
		}
		e.preventDefault();
	});
```

Also update the file's header comment to drop "(Task 8)".

- [ ] **Step 2: Check it builds and the suite is green**

Run: `go build ./... && go test ./internal/apps/books/... -count=1`
Expected: PASS (the script is embedded; a typo in it won't fail Go, so read the diff once against reader.js's resizable-panes section before committing).

- [ ] **Step 3: Commit**

```bash
git add internal/apps/books/static/books.js
git commit -m "feat(books): resizable panes, keyboard shortcuts and row highlight (#487)"
```

---

### Task 9: User guide, demo books and docs

**Files:**
- Modify: `docs/user/books.md`
- Create: `docs/screenshots/seed/books.go`
- Modify: `docs/screenshots/seed/seed.go` (step list), `docs/screenshots/seed/seed_test.go`
- Modify: `AGENTS.md`, `docs/developers/index.md`, `docs/developers/repository-layout.md`, `docs/superpowers/specs/2026-10-09-on-books-design.md`

**Interfaces:**
- Consumes: `books.NewStore`, `Create`, `StartReading`, `FinishReading`, `MarkDNF`, `ShelfCounts`, `NewBook`, `BookInput` (Tasks 3–4).
- Produces: `seedBooks(ctx, st *books.Store, userID int64, now time.Time) error`.

- [ ] **Step 1: Write the failing seed test**

In `docs/screenshots/seed/seed_test.go`, find the test that seeds a directory and checks each app's demo content (the one with the `later counts` and `focus timers` checks) and add, next to the focus checks:

```go
	bst := books.NewStore(handle)
	shelves, err := bst.ShelfCounts(ctx, demo.ID)
	if err != nil || shelves[books.ShelfReading] < 2 || shelves[books.ShelfWant] < 2 ||
		shelves[books.ShelfRead] < 2 || shelves[books.ShelfDNF] < 1 {
		t.Errorf("books shelves = %v, %v; want reading/want/read >= 2 and dnf >= 1", shelves, err)
	}
```
and the import `"github.com/iliafrenkel/on-suite/internal/apps/books"`.

Run: `go test ./docs/screenshots/seed/... -count=1`
Expected: FAIL — the books shelves are empty.

- [ ] **Step 2: Write `docs/screenshots/seed/books.go`**

```go
package main

import (
	"context"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

// seedBooks gives the demo account a small library across every shelf:
// two books on the go, two waiting, two finished and one put down, with a
// few tags and a series, so each shelf and the book pane have something to
// show. Dates are offsets from now, so nothing is ever in the future.
func seedBooks(ctx context.Context, st *books.Store, userID int64, now time.Time) error {
	at := func(daysAgo int) time.Time { return now.AddDate(0, 0, -daysAgo) }
	day := func(daysAgo int) string { return at(daysAgo).Local().Format("2006-01-02") }
	type seed struct {
		in       books.BookInput
		tags     []string
		started  int // days ago; -1 = never
		finished int // days ago; -1 = not finished
		dnf      bool
	}
	library := []seed{
		{books.BookInput{Title: "Leviathan Wakes", Authors: "James S. A. Corey", Year: 2011, Pages: 592,
			SeriesName: "The Expanse", SeriesNumber: "1",
			Description: "A detective and a ship's officer find the same missing woman at the edge of the solar system."},
			[]string{"sf", "space"}, 9, -1, false},
		{books.BookInput{Title: "Piranesi", Authors: "Susanna Clarke", Year: 2020, Pages: 272},
			[]string{"fantasy"}, 3, -1, false},
		{books.BookInput{Title: "The Dispossessed", Subtitle: "An Ambiguous Utopia", Authors: "Ursula K. Le Guin",
			Year: 1974, Pages: 387}, []string{"sf", "classics"}, -1, -1, false},
		{books.BookInput{Title: "Project Hail Mary", Authors: "Andy Weir", Year: 2021, Pages: 476},
			[]string{"sf", "space"}, -1, -1, false},
		{books.BookInput{Title: "A Wizard of Earthsea", Authors: "Ursula K. Le Guin", Year: 1968, Pages: 183,
			SeriesName: "Earthsea", SeriesNumber: "1"}, []string{"fantasy", "classics"}, 70, 56, false},
		{books.BookInput{Title: "The Remains of the Day", Authors: "Kazuo Ishiguro", Year: 1989, Pages: 258},
			nil, 35, 20, false},
		{books.BookInput{Title: "Infinite Jest", Authors: "David Foster Wallace", Year: 1996, Pages: 1079},
			nil, 120, 90, true},
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
		if b.finished < 0 {
			continue
		}
		st.SetClock(func() time.Time { return at(b.finished) })
		if b.dnf {
			err = st.MarkDNF(ctx, userID, id, day(b.finished))
		} else {
			err = st.FinishReading(ctx, userID, id, day(b.finished))
		}
		if err != nil {
			return err
		}
	}
	st.SetClock(func() time.Time { return now })
	return nil
}
```

In `seed.go`'s `steps`, add after the focus step:

```go
		func() error { return seedBooks(ctx, books.NewStore(handle), demo.ID, now) },
```

Run: `go test ./docs/screenshots/seed/... -count=1`
Expected: PASS.

- [ ] **Step 3: Write the B1a user guide**

Replace `docs/user/books.md` with:

```markdown
# ON Books

ON Books is a private reading log: what you're reading, what you've read,
what you want to read next and what you gave up on. No friends, no feed —
just your own books.

## Shelves

Every book sits on one shelf:

- **Reading** — books you've started. ON Books opens here.
- **Want to read** — books you haven't started yet.
- **Read** — books you finished.
- **Did not finish** — books you put down.

**All books** shows every shelf at once. The number next to each shelf is
how many books are on it.

You never move a book between shelves yourself: its shelf follows what you
do with it. Start reading it and it moves to Reading; finish it and it moves
to Read.

## Adding a book

Click **Add book** at the top of the sidebar. Only the title is required;
fill in as much else as you like — author, the year it first came out, the
number of pages, the ISBN, the series and its number in it, and a short
description. An ISBN can be the 10- or 13-digit kind, with or without
hyphens; ON Books keeps it as 13 digits.

Then choose where the book goes:

- **Want to read** — it waits on that shelf.
- **Reading now** — you started it today.
- **Already read** — pick the day you finished it.

You can add tags at the same time, separated by commas.

## Reading a book

Pick a book in the list to open it on the right.

- **Start reading** starts it today and moves it to Reading.
- **Finish** asks for the day you finished — today, unless you change it —
  and moves the book to Read.
- **Did not finish** works the same way and moves it to Did not finish.

A finished book offers **Read again**: that starts a new reading, and the
earlier one is kept. A book is read once at a time, so a book you're
reading can't be started again until you finish it or put it down.

## Tags

Tags are your own labels — *sf*, *book club*, *favourites*. Edit them in the
**Tags** box at the bottom of a book and click **Save tags**. Your tags are
listed under the shelves; click one to see every book with it. A tag nobody
uses any more disappears.

## Finding a book

Type in the box above the list to narrow it to books whose title,
subtitle, author or series contains what you typed. The filter stays as you
switch shelves; clear the box to see everything again.

## Editing and deleting

Open a book and use the **⋯** menu: **Edit details** changes the title,
author and the rest; **Delete** removes the book for good, with its
readings and tags.

## Keyboard shortcuts

- **j** / **k** — open the next / previous book in the list
- **/** — jump to the filter box
- **a** — add a book
- **Esc** — close the Finish or Did not finish box, or leave the filter

## On a phone

On a narrow screen ON Books shows one pane at a time: the list, then the
book. Use the back button at the top to go from a book to the list, and from
the list to the shelves.
```

- [ ] **Step 4: Update the app lists and the spec**

`AGENTS.md` — in "What this is", the sentence listing the registered apps: after the ON Later item, add

```markdown
**ON Books** (a private reading log: shelves, readings and tags, with Open
Library search, progress, notes and stats to come),
```
keeping the sentence's existing "and **ON Focus** (…) are all registered today." ending.

`docs/developers/index.md:7` — add ON Books to the list of apps ("ON Notes, ON Reader, ON Later, ON Books, ON Flash and ON Focus").

`docs/developers/repository-layout.md` — add a line in the apps tree, in alphabetical position (before `flash/`), in the same shape as the others:

```
│   │   ├── books/                ON Books: a private reading log — shelves, readings and tags; Open Library, progress and stats to come
```

`docs/superpowers/specs/2026-10-09-on-books-design.md` — in "Phases", replace the B1 row with two rows and the sentence under the table:

```markdown
| B1a | #487 Library and shelves (part 1) | App skeleton; books, readings and tags tables; manual entry on an Add book page; generated spines; the three panes; Start reading / Finish / DNF; tags; edit and delete; title/author filter |
| B1b | #487 Library and shelves (part 2) | Open Library search in an Add book dialog in front of the B1a form; covers (`books_covers`): fetch on save, upload, image URL |
```
and replace "If B1's plan gets too large, OL search and covers split into their own PR." with "B1 was split into B1a and B1b while planning (2026-10-09)."

- [ ] **Step 5: Run the full check**

Run the full check from Global Constraints.
Expected: no output from gofmt, all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add -A docs AGENTS.md
git commit -m "docs(books): user guide, demo library and app lists (#487)"
```

---

### Task 10: Check it in a browser and open the PR

- [ ] **Step 1: Seed a demo directory and start the server**

```bash
SEED=$(mktemp -d)/books-demo
go run ./docs/screenshots/seed --data-dir $SEED
go build -o $SEED/onsuite ./cmd/onsuite
echo $SEED
```
Add a `.claude/launch.json` configuration named `onsuite-books` (in the main checkout, which the preview tools read) with `"runtimeExecutable": "<SEED>/onsuite"`, `"runtimeArgs": ["serve", "--addr", ":8096", "--data-dir", "<SEED>"]` and `"port": 8096`, then start it with the preview tools and sign in as `demo` (password in `docs/screenshots/README.md`).

- [ ] **Step 2: Walk through the app**

Check each, at desktop width and at 375px (mobile preset), in light and dark:

1. `/books/` opens on Reading with Leviathan Wakes and Piranesi; counts 2 · 2 · 2 · 1 · 7.
2. Clicking shelves and tags swaps the list only; the URL follows; Back returns to the previous shelf.
3. Typing "le guin" in the filter narrows the list while typing; switching shelves keeps the filter.
4. Opening a book swaps only the book pane and highlights its row; `j`/`k` walk the list; `/` focuses the filter; `Esc` leaves it.
5. Finish → pick yesterday → the book moves to Read, counts update; a date before the start shows the banner.
6. Did not finish, Start reading, Read again, Save tags all work and keep the list context.
7. ⋯ → Delete asks in the Books dialog; Cancel then deleting another book does not delete the first (#551).
8. ⋯ → Edit details → Save returns to the same book and list.
9. Add book: a mistake comes back with messages; "Already read" shows the date field only when picked.
10. Gutters drag and keep their widths after a reload and after a Finish (full panes swap).
11. Phone: list first; back goes to the shelves; opening a book shows the book; back returns to the list.

Fix anything found (with a test where one can be written), re-run the full check, commit.

- [ ] **Step 3: Remove the launch entry and push**

Remove the `onsuite-books` entry from `.claude/launch.json` (do not commit it), stop the server, then:

```bash
git push -u origin feat/books-b1a-library
env -u GH_TOKEN gh pr create --title "feat(books): ON Books B1a — library and shelves (#487)" --body "$(cat <<'EOF'
First half of #487 (B1a in the plan): ON Books as a registered app with manual entry, shelves derived from readings, the three-pane view, start/finish/did not finish, tags, edit and delete, and a title/author filter. Open Library search and covers follow in B1b.

Spec: docs/superpowers/specs/2026-10-09-on-books-design.md
Plan: docs/superpowers/plans/2026-10-09-on-books-b1a-library.md
EOF
)"
```
Never merge it.
