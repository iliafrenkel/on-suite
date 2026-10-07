# ON Focus F1 — Saved timers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship ON Focus as a registered app where you can create, edit, duplicate, delete and reorder named timers (single or intervals) shown as colour tiles; ▶ opens a placeholder running page listing the timer's phases.

**Architecture:** A new app package `internal/apps/focus` shaped like ON Later: `focus.go` (App/Meta/Mount), `store.go` (all SQL, owns the clock), `timer.go` (input, validation, palette, chimes), `phases.go` (phase-list expansion and length formatting), `handlers.go` + `form.go` (HTTP), Go templates, and one small `static/home.js` for the delete confirmation and drag-to-reorder. Flash's `deck-c-*` palette classes are renamed to suite-wide `swatch-c-*` first so Focus can use them.

**Tech Stack:** Go 1.22+ `ServeMux`, `html/template`, htmx 2.0.10 (vendored), SQLite via `modernc.org/sqlite`, plain ES5-style JavaScript (no build step).

**Spec:** [docs/superpowers/specs/2026-10-07-on-focus-design.md](../specs/2026-10-07-on-focus-design.md) — sections "Swatch rename", "Data model", "Phase list", "Pages and routes" (Home, Timer form), "Phases" (F1 row).

## Global Constraints

- App ID `focus`, name `ON Focus`, summary `Focus timers you set up once and reuse.`, `Order: 50`; tables prefixed `focus_`; routes under `/focus/`.
- Palette names, in picker order: `teal blue purple pink coral amber green gray`; default `teal`.
- Chimes: `bell` (default), `bowl`, `soft`, `silent`.
- Limits: name 1–80 characters after trimming; focus 1–180 min; break 1–60; long break 1–60; rounds 1–12; long break every 1–`rounds`.
- Defaults: kind `single`, focus 25, break 5, long break 15, rounds 4, long break every 4, auto-advance on, keep history on.
- Interval columns are NULL for `single`, required for `intervals` (store + SQL CHECK).
- Every `user_id` column: `REFERENCES users (id) ON DELETE CASCADE`. Timestamps TEXT via `db.FormatTime`. Tables `STRICT`.
- Missing or someone else's timer → 404. Bad input on a JSON-less POST (order) → 400. Form validation → 422 re-render with messages next to fields.
- CSP: no inline `<script>` and no `style=""` attributes. Colours arrive as `swatch-c-<name>` classes.
- App CSS goes in `internal/ui/static/app.css` in a new "ON Focus" section; every class prefixed `focus-` (plus the shared `swatch-c-*`).
- Never edit a shipped migration (Flash's `0010_deck_color.sql` keeps its comment as-is).
- Full check must stay green on every commit:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- Work on branch `feat/focus-f1-saved-timers` in a worktree, never on `main`. Open a PR as `iliafrenkel` (`env -u GH_TOKEN gh …`); never merge.

## Deviations from the spec (decided while planning)

- The **today strip** needs the stats queries that F3 builds, so it moves wholly to F3 instead of showing zeros in F1. Task 11 updates the spec's phase table to match.
- The home page needs a little JavaScript (delete confirmation, drag-to-reorder), so F1 adds `static/home.js`. `focus.js` stays the running page's script (F2).
- The chime ▶ preview button needs Web Audio, so it lands with the chimes in F2. F1's form has the chime select only.
- `docs/user/focus.md` must exist for the app to start (`help.Load`), so F1 ships a short guide covering timers; F4 completes it with the running view, history and screenshots.

## File map

| File | Responsibility |
|---|---|
| `internal/ui/static/app.css` | rename `deck-c-*`/`--deck*` → `swatch-c-*`/`--swatch*`; new "ON Focus" section |
| `internal/apps/flash/**` | follow the rename (templates, two Go comments, tests) |
| `internal/apps/focus/focus.go` | `App`, `Meta`, `Templates`, `Mount`, script route |
| `internal/apps/focus/store.go` | `ID`, `Migrations`, errors, `Store`, timer CRUD, duplicate, reorder |
| `internal/apps/focus/timer.go` | `Kind`, `Colors`, `Chimes`, `TimerInput`, `Normalize`, `Validate`, `FieldErrors`, `ValidationError` |
| `internal/apps/focus/phases.go` | `Phase`, `Phases`, `TotalSeconds`, `Summary`, `FormatLength` |
| `internal/apps/focus/form.go` | form parsing and the form view model |
| `internal/apps/focus/handlers.go` | every HTTP handler |
| `internal/apps/focus/migrations/0001_timers.sql`, `0002_sessions.sql` | schema |
| `internal/apps/focus/templates/{index,form,run}.html`, `focus.partial.html` | pages and shared blocks |
| `internal/apps/focus/static/home.js` | delete confirmation, drag-to-reorder |
| `internal/apps/focus/*_test.go` | tests |
| `cmd/onsuite/main.go`, `cmd/onsuite/database_test.go` | registration and pinned app lists |
| `internal/arch/arch_test.go`, `internal/ui/templates_test.go`, `internal/ui/icons.go`, `internal/ui/icons_test.go` | platform lists |
| `internal/platform/help/pages.go`, `docs/user/focus.md`, `docs/user/index.md` | user guide |
| `docs/screenshots/seed/{seed.go,focus.go,seed_test.go}` | demo timers |
| `AGENTS.md`, `docs/developers/index.md`, `docs/developers/repository-layout.md` | app lists |

---

### Task 0: Worktree and branch

- [ ] **Step 1: Create the worktree**

```bash
cd /Users/iliaf/src/WEB/on-suite
git fetch origin
git worktree add ../on-suite-focus-f1 -b feat/focus-f1-saved-timers origin/main
cd ../on-suite-focus-f1
go build ./cmd/onsuite
```
Expected: builds with no output. All later commands run in `../on-suite-focus-f1`.

---

### Task 1: Rename Flash's palette classes to `swatch-c-*`

**Files:**
- Modify: `internal/ui/static/app.css` (the "ON Flash: home screen" palette block near line 2650 and every `var(--deck…)` use)
- Modify: `internal/apps/flash/templates/*.html`, `internal/apps/flash/deck.go:31`, `internal/apps/flash/card_view.go:17`, `internal/apps/flash/handlers_decks.go` (comment), `internal/apps/flash/handlers_*_test.go`

**Interfaces:**
- Produces: CSS classes `.swatch-c-teal` … `.swatch-c-gray`, each setting `--swatch` and `--swatch-soft` (dark-mode overrides included). Focus's templates use these from Task 7 on.

- [ ] **Step 1: Point the tests at the new names first**

```bash
perl -pi -e 's/deck-c-/swatch-c-/g' internal/apps/flash/handlers_import_test.go internal/apps/flash/handlers_review_test.go internal/apps/flash/handlers_decks_test.go
go test ./internal/apps/flash/... -count=1 2>&1 | tail -5
```
Expected: FAIL — e.g. `.flash-review class = "… deck-c-purple …", want it to contain swatch-c-purple`.

- [ ] **Step 2: Rename everywhere else (not the shipped migration)**

```bash
files=$(grep -rlE 'deck-c-|--deck' internal/ui/static/app.css internal/apps/flash --include='*.css' --include='*.html' --include='*.go' | grep -v '/migrations/')
perl -pi -e 's/deck-c-/swatch-c-/g; s/--deck-soft/--swatch-soft/g; s/--deck(?![-\w])/--swatch/g' $files
grep -rnE 'deck-c-|--deck' internal cmd docs/developers docs/user | grep -v '/migrations/'
```
Expected: the final grep prints nothing.

- [ ] **Step 3: Reword the palette comment so it's suite-wide**

In `internal/ui/static/app.css`, the comment above `.swatch-c-teal` now reads "…a deck's colour arrives as a swatch-c-<name> class that sets --swatch/--swatch-soft…". Replace that comment block with:

```css
/* ---- Colour swatches (shared: ON Flash decks, ON Focus timers) ----------
 * The CSP forbids style="", so a user-picked colour arrives as a
 * swatch-c-<name> class that sets --swatch/--swatch-soft for everything
 * inside it. Apps keep their own copy of the name list (they never import
 * each other); these classes are the shared part. */
```
Keep the `/* ---- ON Flash: home screen (UI overhaul U1) ---- … Spec: …` header line(s) above it intact if they introduce other Flash rules; only the palette explanation moves into the new comment.

- [ ] **Step 4: Run Flash and UI tests**

Run: `go test ./internal/apps/flash/... ./internal/ui/... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A internal/ui/static/app.css internal/apps/flash
git commit -m "refactor(ui): rename deck-c-* palette to suite-wide swatch-c-* (#493)"
```

---

### Task 2: ON Focus app skeleton, schema and registration

**Files:**
- Create: `internal/apps/focus/focus.go`, `internal/apps/focus/store.go`, `internal/apps/focus/handlers.go`
- Create: `internal/apps/focus/migrations/0001_timers.sql`, `internal/apps/focus/migrations/0002_sessions.sql`
- Create: `internal/apps/focus/templates/index.html`, `internal/apps/focus/templates/focus.partial.html`
- Create: `internal/apps/focus/static/home.js` (placeholder comment only in this task)
- Create: `internal/apps/focus/handlers_test.go`, `internal/apps/focus/store_test.go`
- Create: `docs/user/focus.md`
- Modify: `cmd/onsuite/main.go`, `cmd/onsuite/database_test.go:54-55,163-164`, `internal/arch/arch_test.go:~583`, `internal/ui/templates_test.go:13-14`, `internal/ui/icons.go`, `internal/ui/icons_test.go`, `internal/platform/help/pages.go:~38`, `docs/screenshots/seed/seed.go:72`

**Interfaces:**
- Produces: `focus.ID = "focus"`, `focus.Migrations() fs.FS`, `focus.New() *App`, `focus.NewStore(*sql.DB) *Store`, `(*Store).SetClock(func() time.Time)`, `focus.ErrNotFound`, `focus.ErrInvalid`, `(*App).userID`, `(*App).pathID`, `(*App).fail`, `(*App).render`. Template page `focus/index`, partial block `focus-confirm`.

- [ ] **Step 1: Write the failing handler test**

`internal/apps/focus/handlers_test.go`:

```go
package focus_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
	"github.com/iliafrenkel/on-suite/internal/apptest"
)

type server = apptest.Server[*focus.Store]

// newServer mounts ON Focus with its own store as the test's handle.
func newServer(t *testing.T) *server {
	t.Helper()
	return apptest.NewServer(t, focus.New(), focus.NewStore)
}

func TestFocusRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/focus/", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /focus/ anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

func TestIndexShowsTheEmptyState(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/focus/")
	doc.MustHave(".focus-page")
	doc.MustHave(".focus-empty")
	doc.MustHave(`a[href="/focus/new"]`)
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/apps/focus/... -count=1`
Expected: FAIL — `package github.com/iliafrenkel/on-suite/internal/apps/focus` is not in std / no Go files.

- [ ] **Step 3: Write the migrations**

`internal/apps/focus/migrations/0001_timers.sql`:

```sql
-- One row per saved timer. A timer is either one block of focus time
-- (kind 'single') or Pomodoro-style intervals (kind 'intervals'); the
-- interval columns are NULL for single timers and required for intervals.
-- Colour and chime are names, not values: app.css (swatch-c-*) and the
-- running page decide what they look and sound like.
CREATE TABLE focus_timers (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id            INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name               TEXT    NOT NULL,
    color              TEXT    NOT NULL DEFAULT 'teal',
    kind               TEXT    NOT NULL CHECK (kind IN ('single', 'intervals')),
    focus_minutes      INTEGER NOT NULL,
    break_minutes      INTEGER,
    long_break_minutes INTEGER,
    rounds             INTEGER,
    long_break_every   INTEGER,
    auto_advance       INTEGER NOT NULL DEFAULT 1,
    chime              TEXT    NOT NULL DEFAULT 'bell',
    keep_history       INTEGER NOT NULL DEFAULT 1,
    position           INTEGER NOT NULL,
    created_at         TEXT    NOT NULL,
    updated_at         TEXT    NOT NULL,
    CHECK (
        (kind = 'single' AND break_minutes IS NULL AND long_break_minutes IS NULL
            AND rounds IS NULL AND long_break_every IS NULL)
     OR (kind = 'intervals' AND break_minutes IS NOT NULL AND long_break_minutes IS NOT NULL
            AND rounds IS NOT NULL AND long_break_every IS NOT NULL)
    )
) STRICT;

CREATE INDEX focus_timers_user_idx ON focus_timers (user_id, position);
```

`internal/apps/focus/migrations/0002_sessions.sql`:

```sql
-- One row per recorded session (written from F3 on). timer_name and color
-- are copied at session start so history outlives the timer; client_id is
-- the browser's random id for the session, so a retried or doubled POST is
-- stored once.
CREATE TABLE focus_sessions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    timer_id      INTEGER REFERENCES focus_timers (id) ON DELETE SET NULL,
    timer_name    TEXT    NOT NULL,
    color         TEXT    NOT NULL,
    client_id     TEXT    NOT NULL,
    started_at    TEXT    NOT NULL,
    ended_at      TEXT    NOT NULL,
    focus_seconds INTEGER NOT NULL CHECK (focus_seconds >= 60),
    rounds_done   INTEGER NOT NULL,
    completed     INTEGER NOT NULL,
    UNIQUE (user_id, client_id)
) STRICT;

CREATE INDEX focus_sessions_started_idx ON focus_sessions (user_id, started_at);
```

- [ ] **Step 4: Write `store.go` (skeleton only — CRUD comes in Task 5)**

```go
// Package focus implements ON Focus: saved, reusable focus timers (single
// blocks or Pomodoro-style intervals) and, from F3, a history of sessions.
package focus

import (
	"database/sql"
	"embed"
	"errors"
	"io/fs"
	"time"
)

// ID is the app id: URL prefix, migration namespace, table prefix.
const ID = "focus"

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations is this app's schema, for the platform and for store tests.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		panic("focus: embedded migrations missing: " + err.Error()) // unreachable
	}
	return sub
}

var (
	// ErrNotFound is a missing row or somebody else's — indistinguishable
	// on purpose, so a handler answers 404 for both.
	ErrNotFound = errors.New("focus: not found")
	// ErrInvalid is a request the store refuses.
	ErrInvalid = errors.New("focus: invalid")
)

// Store is every SQL query ON Focus runs.
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
```

- [ ] **Step 5: Write `focus.go`**

```go
package focus

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

// App is ON Focus.
type App struct {
	store *Store
	deps  app.Deps
}

// New returns the app for registration in cmd/onsuite.
func New() *App { return &App{} }

func (a *App) Meta() app.Meta {
	return app.Meta{
		ID:      ID,
		Name:    "ON Focus",
		Summary: "Focus timers you set up once and reuse.",
		Order:   50,
	}
}

func (a *App) Migrations() fs.FS { return Migrations() }

func (a *App) Templates() fs.FS {
	sub, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		panic("focus: embedded templates missing: " + err.Error()) // unreachable
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
	r.HandleFunc("GET /home.js", a.script("home.js"))
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

- [ ] **Step 6: Write `handlers.go` with the shared helpers and an empty index**

```go
package focus

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
// 404, the same as a timer that isn't there.
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

// render draws a full page with the shell around it.
func (a *App) render(w http.ResponseWriter, r *http.Request, status int, name, title string, data any) {
	page := a.deps.Page(r, title)
	page.Data = data
	if err := a.deps.Render.Page(w, status, name, page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// tileView is one timer tile on the home page.
type tileView struct {
	ID      int64
	Name    string
	Color   string
	Summary string // "15 min" or "50 / 10 × 4 · long 30"
	Total   string // the pill: "15 min", "3h 20m"
}

type indexView struct {
	Tiles []tileView
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	a.render(w, r, http.StatusOK, "focus/index", "Timers", indexView{})
}
```

- [ ] **Step 7: Write the templates and the script placeholder**

`internal/apps/focus/templates/focus.partial.html`:

```html
{{/* focus-confirm is the app's confirmation dialog, driven by home.js for
     forms marked data-focus-confirm (the dialog later.js uses). Without
     JavaScript the form simply submits. */}}
{{define "focus-confirm"}}
<dialog id="focus-confirm-dialog" class="focus-dialog">
	<p id="focus-confirm-message"></p>
	<div class="dialog-actions">
		<button type="button" id="focus-confirm-ok" class="danger">Delete</button>
		<button type="button" id="focus-confirm-cancel">Cancel</button>
	</div>
</dialog>
{{end}}
```

`internal/apps/focus/templates/index.html`:

```html
{{define "head"}}<script src="/focus/home.js" defer></script>{{end}}

{{define "content"}}
<div class="focus-page stack">
	<div class="focus-toolbar">
		<h1>Timers</h1>
		<a class="button primary" href="/focus/new">+ New timer</a>
	</div>
	{{if .Data.Tiles}}
	{{else}}
	<div class="focus-empty stack">
		<p>No timers yet. Make one for anything you want to give your full attention to — a 15-minute reflection, a 50-minute deep-work block with breaks.</p>
		<p><a class="button primary" href="/focus/new">Create your first timer</a></p>
	</div>
	{{end}}
	{{template "focus-confirm"}}
</div>
{{end}}
```

`internal/apps/focus/static/home.js`:

```js
// ON Focus's home-page script: the delete confirmation and drag-to-reorder
// (Tasks 7 and 9 of the F1 plan fill it in).
"use strict";
```

- [ ] **Step 8: Write the user guide stub**

`docs/user/focus.md`:

```markdown
# ON Focus

ON Focus is a calm focus timer. Set a timer up once — a 15-minute "Daily
Reflection", a 50-minute "Deep work" block with breaks — and start it
whenever you need it.

## Your timers

Your timers appear as coloured tiles on the ON Focus home page. Click
**+ New timer** to make one.
```
(Task 11 fills in the rest of the F1 guide.)

- [ ] **Step 9: Register the app and update every pinned list**

`cmd/onsuite/main.go` — add the import `"github.com/iliafrenkel/on-suite/internal/apps/focus"` and the line `focus.New(),` between `flash.New(),` and `later.New(),` in `registeredApps()`.

`cmd/onsuite/database_test.go`:
- line ~54: `if len(ids) != 5 {` and message `"NavItems() = %v, want exactly flash, focus, later, notes, and paste"`.
- line ~163: `if len(registry.NavItems()) != 6 {` and message `"NavItems() has %d entries, want 6 (flash, focus, later, notes, paste, reader)"`.

`internal/arch/arch_test.go` — in `TestAppsReadTheirStoreClock`'s `want`, add `"internal/apps/focus/store.go",` after the flash line (the list is sorted).

`internal/ui/templates_test.go:13-14`:

```go
// app.css (reader-*, notes-*, paste-*, flash-*, later-*, focus-*).
var appClassPattern = regexp.MustCompile(`^(reader|notes|paste|flash|later|focus)-`)
```

`internal/ui/icons.go` — add after `"later"`:

```go
	// A stopwatch: a ring with the crown on top.
	"focus": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="none"/>
		<circle cx="12" cy="13.5" r="5" fill="none" stroke="var(--c-accent)" stroke-width="1.5"/>
		<path d="M12 13.5V11M10.5 6.5h3" stroke="var(--c-accent)" stroke-width="1.5" stroke-linecap="round"/>
	</svg>`,
```

`internal/ui/icons_test.go` — add `"focus"` to each of the four `[]string{…}` id lists.

`internal/platform/help/pages.go` — in `order`, add `{"focus", "ON Focus"},` after `{"flash", "ON Flash"},`.

`docs/screenshots/seed/seed.go:72` — add `focus.New(),` to `app.NewRegistry(...)` (after `flash.New(),`) and the import. Seeding demo timers comes in Task 11.

- [ ] **Step 10: Add a schema test for the CHECK constraint**

`internal/apps/focus/store_test.go`:

```go
package focus_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// fixture is a migrated database with two users and a clock the test moves.
type fixture struct {
	store *focus.Store
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
	appMigrations, err := db.Collect(focus.ID, focus.Migrations())
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
	st := focus.NewStore(handle)
	f := &fixture{store: st, db: handle, alice: alice, bob: bob, now: time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)}
	st.SetClock(func() time.Time { return f.now })
	return f
}

func TestSchemaRejectsASingleTimerWithIntervalColumns(t *testing.T) {
	f := newFixture(t)
	now := db.FormatTime(f.now)
	_, err := f.db.Exec(`INSERT INTO focus_timers
		(user_id, name, kind, focus_minutes, rounds, position, created_at, updated_at)
		VALUES (?, 'x', 'single', 15, 4, 0, ?, ?)`, f.alice.ID, now, now)
	if err == nil {
		t.Fatal("inserted a single timer with rounds set, want a CHECK failure")
	}
}

func TestSchemaRejectsIntervalsWithoutIntervalColumns(t *testing.T) {
	f := newFixture(t)
	now := db.FormatTime(f.now)
	_, err := f.db.Exec(`INSERT INTO focus_timers
		(user_id, name, kind, focus_minutes, position, created_at, updated_at)
		VALUES (?, 'x', 'intervals', 50, 0, ?, ?)`, f.alice.ID, now, now)
	if err == nil {
		t.Fatal("inserted an intervals timer with no break/rounds, want a CHECK failure")
	}
}
```
(`f.bob` is first read in Task 5, which also adds the `tick` helper; a struct field set in a composite literal doesn't trip staticcheck's unused check.)

- [ ] **Step 11: Run the focused tests, then the full check**

Run: `go test ./internal/apps/focus/... ./cmd/onsuite/... ./internal/arch/... ./internal/ui/... ./internal/platform/help/... ./docs/... -count=1`
Expected: PASS (including `TestEveryUserColumnCascadesFromUsers` and `TestBuildStackServesHelpForEveryRegisteredApp`).

Then run the full check from Global Constraints. Expected: no output from gofmt, all tests PASS.

- [ ] **Step 12: Commit**

```bash
git add -A
git commit -m "feat(focus): app skeleton, schema and registration (#493)"
```

---

### Task 3: Timer input, palette, chimes and validation

**Files:**
- Create: `internal/apps/focus/timer.go`
- Test: `internal/apps/focus/timer_test.go`

**Interfaces:**
- Produces:
  - `type Kind string`; `KindSingle Kind = "single"`, `KindIntervals Kind = "intervals"`
  - `var Colors = []string{"teal", "blue", "purple", "pink", "coral", "amber", "green", "gray"}`; `const DefaultColor = "teal"`
  - `var Chimes = []string{"bell", "bowl", "soft", "silent"}`; `const DefaultChime = "bell"`
  - `const MaxNameRunes = 80`
  - `type TimerInput struct { Name, Color string; Kind Kind; FocusMinutes, BreakMinutes, LongBreakMinutes, Rounds, LongBreakEvery int; AutoAdvance, KeepHistory bool; Chime string }`
  - `func DefaultInput() TimerInput`
  - `func (in TimerInput) Normalize() TimerInput`
  - `type FieldErrors map[string]string` — keys are form field names: `name color kind focus break long_break rounds long_break_every chime`
  - `func (in TimerInput) Validate() FieldErrors` (call on normalized input; nil when valid)
  - `type ValidationError struct{ Fields FieldErrors }` with `Error() string` and `Unwrap() error` returning `ErrInvalid`

- [ ] **Step 1: Write the failing tests**

`internal/apps/focus/timer_test.go`:

```go
package focus_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
)

func validSingle() focus.TimerInput {
	in := focus.DefaultInput()
	in.Name = "Daily Reflection"
	in.FocusMinutes = 15
	return in
}

func validIntervals() focus.TimerInput {
	return focus.TimerInput{
		Name: "Deep work", Color: "blue", Kind: focus.KindIntervals,
		FocusMinutes: 50, BreakMinutes: 10, LongBreakMinutes: 30, Rounds: 4, LongBreakEvery: 2,
		AutoAdvance: true, KeepHistory: true, Chime: "bowl",
	}
}

func TestDefaultInput(t *testing.T) {
	in := focus.DefaultInput()
	if in.Kind != focus.KindSingle || in.Color != "teal" || in.Chime != "bell" ||
		in.FocusMinutes != 25 || in.BreakMinutes != 5 || in.LongBreakMinutes != 15 ||
		in.Rounds != 4 || in.LongBreakEvery != 4 || !in.AutoAdvance || !in.KeepHistory || in.Name != "" {
		t.Errorf("DefaultInput() = %+v", in)
	}
}

func TestNormalizeTrimsTheNameAndClearsIntervalsForSingle(t *testing.T) {
	in := focus.DefaultInput()
	in.Name = "  Reading \n"
	got := in.Normalize()
	if got.Name != "Reading" {
		t.Errorf("Name = %q, want %q", got.Name, "Reading")
	}
	if got.BreakMinutes != 0 || got.LongBreakMinutes != 0 || got.Rounds != 0 || got.LongBreakEvery != 0 {
		t.Errorf("single timer kept interval fields: %+v", got)
	}
	iv := validIntervals().Normalize()
	if iv.Rounds != 4 || iv.BreakMinutes != 10 {
		t.Errorf("Normalize cleared an intervals timer's fields: %+v", iv)
	}
}

func TestValidateAcceptsGoodInput(t *testing.T) {
	for name, in := range map[string]focus.TimerInput{"single": validSingle(), "intervals": validIntervals()} {
		if errs := in.Normalize().Validate(); errs != nil {
			t.Errorf("%s: Validate() = %v, want nil", name, errs)
		}
	}
}

func TestValidateRejectsBadInput(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(*focus.TimerInput)
		field string
	}{
		{"empty name", func(in *focus.TimerInput) { in.Name = "   " }, "name"},
		{"long name", func(in *focus.TimerInput) { in.Name = strings.Repeat("é", 81) }, "name"},
		{"unknown colour", func(in *focus.TimerInput) { in.Color = "Teal" }, "color"},
		{"unknown kind", func(in *focus.TimerInput) { in.Kind = "loop" }, "kind"},
		{"focus zero", func(in *focus.TimerInput) { in.FocusMinutes = 0 }, "focus"},
		{"focus too long", func(in *focus.TimerInput) { in.FocusMinutes = 181 }, "focus"},
		{"break zero", func(in *focus.TimerInput) { in.BreakMinutes = 0 }, "break"},
		{"break too long", func(in *focus.TimerInput) { in.BreakMinutes = 61 }, "break"},
		{"long break too long", func(in *focus.TimerInput) { in.LongBreakMinutes = 61 }, "long_break"},
		{"rounds zero", func(in *focus.TimerInput) { in.Rounds = 0 }, "rounds"},
		{"rounds too many", func(in *focus.TimerInput) { in.Rounds = 13 }, "rounds"},
		{"long every zero", func(in *focus.TimerInput) { in.LongBreakEvery = 0 }, "long_break_every"},
		{"long every past rounds", func(in *focus.TimerInput) { in.LongBreakEvery = 5 }, "long_break_every"},
		{"unknown chime", func(in *focus.TimerInput) { in.Chime = "gong" }, "chime"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := validIntervals()
			tt.edit(&in)
			errs := in.Normalize().Validate()
			if errs[tt.field] == "" {
				t.Errorf("Validate() = %v, want an error for %q", errs, tt.field)
			}
		})
	}
}

func TestValidateAllowsEightyCharacterNames(t *testing.T) {
	in := validSingle()
	in.Name = strings.Repeat("é", 80) // 80 runes, 160 bytes
	if errs := in.Normalize().Validate(); errs != nil {
		t.Errorf("Validate() = %v, want nil", errs)
	}
}

func TestValidateIgnoresIntervalFieldsOnSingleTimers(t *testing.T) {
	in := validSingle()
	in.Rounds = 99 // hidden in the form; Normalize clears it
	if errs := in.Normalize().Validate(); errs != nil {
		t.Errorf("Validate() = %v, want nil", errs)
	}
}

func TestValidationErrorIsErrInvalid(t *testing.T) {
	var err error = &focus.ValidationError{Fields: focus.FieldErrors{"name": "x"}}
	if !errors.Is(err, focus.ErrInvalid) {
		t.Error("ValidationError does not unwrap to ErrInvalid")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/focus/ -run 'TestDefaultInput|TestNormalize|TestValidat' -count=1`
Expected: FAIL — `undefined: focus.DefaultInput` (and friends).

- [ ] **Step 3: Write `timer.go`**

```go
package focus

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Kind is a timer's shape.
type Kind string

// The two kinds of timer (spec: "Timer kinds").
const (
	KindSingle    Kind = "single"
	KindIntervals Kind = "intervals"
)

// Colors is the palette a timer's owner picks from, in picker order. It
// mirrors ON Flash's DeckColors (apps never import each other); the
// swatch-c-* classes in app.css are the shared part.
var Colors = []string{"teal", "blue", "purple", "pink", "coral", "amber", "green", "gray"}

// DefaultColor must match migrations/0001_timers.sql's column default.
const DefaultColor = "teal"

// Chimes are the phase-end sounds the running page knows how to play.
var Chimes = []string{"bell", "bowl", "soft", "silent"}

// DefaultChime must match migrations/0001_timers.sql's column default.
const DefaultChime = "bell"

// MaxNameRunes caps a timer's name, counted in characters, not bytes.
const MaxNameRunes = 80

// Limits, in minutes or rounds.
const (
	maxFocusMinutes = 180
	maxBreakMinutes = 60
	maxRounds       = 12
)

// TimerInput is everything a person sets on a timer. Interval fields are
// ignored (and cleared by Normalize) for single timers.
type TimerInput struct {
	Name             string
	Color            string
	Kind             Kind
	FocusMinutes     int
	BreakMinutes     int
	LongBreakMinutes int
	Rounds           int
	LongBreakEvery   int
	AutoAdvance      bool
	KeepHistory      bool
	Chime            string
}

// DefaultInput is what the New timer form starts with. The interval
// fields are filled in so switching the form to Intervals shows sensible
// numbers.
func DefaultInput() TimerInput {
	return TimerInput{
		Color: DefaultColor, Kind: KindSingle,
		FocusMinutes: 25, BreakMinutes: 5, LongBreakMinutes: 15, Rounds: 4, LongBreakEvery: 4,
		AutoAdvance: true, KeepHistory: true, Chime: DefaultChime,
	}
}

// Normalize trims the name and clears interval fields on a single timer.
func (in TimerInput) Normalize() TimerInput {
	in.Name = strings.TrimSpace(in.Name)
	if in.Kind == KindSingle {
		in.BreakMinutes, in.LongBreakMinutes, in.Rounds, in.LongBreakEvery = 0, 0, 0, 0
	}
	return in
}

// FieldErrors maps a form field name to the message shown next to it.
type FieldErrors map[string]string

// Validate checks normalized input against the spec's limits. It returns
// nil when everything is fine.
func (in TimerInput) Validate() FieldErrors {
	errs := FieldErrors{}
	switch n := utf8.RuneCountInString(in.Name); {
	case n == 0:
		errs["name"] = "Give the timer a name."
	case n > MaxNameRunes:
		errs["name"] = fmt.Sprintf("Keep the name to %d characters or fewer.", MaxNameRunes)
	}
	if !slices.Contains(Colors, in.Color) {
		errs["color"] = "Pick one of the colours shown."
	}
	if in.Kind != KindSingle && in.Kind != KindIntervals {
		errs["kind"] = "Pick Single block or Intervals."
	}
	if in.FocusMinutes < 1 || in.FocusMinutes > maxFocusMinutes {
		errs["focus"] = fmt.Sprintf("Focus length must be between 1 and %d minutes.", maxFocusMinutes)
	}
	if in.Kind == KindIntervals {
		if in.BreakMinutes < 1 || in.BreakMinutes > maxBreakMinutes {
			errs["break"] = fmt.Sprintf("Break length must be between 1 and %d minutes.", maxBreakMinutes)
		}
		if in.LongBreakMinutes < 1 || in.LongBreakMinutes > maxBreakMinutes {
			errs["long_break"] = fmt.Sprintf("Long break length must be between 1 and %d minutes.", maxBreakMinutes)
		}
		if in.Rounds < 1 || in.Rounds > maxRounds {
			errs["rounds"] = fmt.Sprintf("Rounds must be between 1 and %d.", maxRounds)
		}
		if in.LongBreakEvery < 1 || (in.Rounds >= 1 && in.LongBreakEvery > in.Rounds) {
			errs["long_break_every"] = "Long break every must be between 1 and the number of rounds."
		}
	}
	if !slices.Contains(Chimes, in.Chime) {
		errs["chime"] = "Pick one of the chimes listed."
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// ValidationError is ErrInvalid with a message per form field, so a
// handler can re-render the form with each message next to its field.
type ValidationError struct{ Fields FieldErrors }

func (e *ValidationError) Error() string { return "focus: invalid timer" }

func (e *ValidationError) Unwrap() error { return ErrInvalid }
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/apps/focus/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/focus/timer.go internal/apps/focus/timer_test.go
git commit -m "feat(focus): timer input, palette, chimes and validation (#493)"
```

---

### Task 4: Phase list and length formatting

**Files:**
- Create: `internal/apps/focus/phases.go`
- Test: `internal/apps/focus/phases_test.go`

**Interfaces:**
- Consumes: `TimerInput`, `KindSingle`, `KindIntervals` (Task 3); `Timer` (Task 5 — this task defines `Phases` on `TimerInput` so it doesn't depend on Task 5; Task 5's `Timer` embeds `TimerInput`).
- Produces:
  - `type PhaseKind string`; `PhaseFocus = "focus"`, `PhaseBreak = "break"`, `PhaseLongBreak = "long_break"`
  - `type Phase struct { Kind PhaseKind \`json:"kind"\`; Seconds int \`json:"seconds"\`; Round int \`json:"round"\` }` — `Round` is the focus round, or for a break the round just finished
  - `func Phases(in TimerInput) []Phase`
  - `func TotalSeconds(phases []Phase) int`
  - `func FormatLength(seconds int) string` — `"15 min"`, `"1h"`, `"3h 20m"`
  - `func Summary(in TimerInput) string` — `"15 min"`, `"50 / 10 × 4"`, `"50 / 10 × 4 · long 30"`
  - `func (p Phase) Label(rounds int) string` — `"Focus"` (rounds 0, a single timer), `"Focus · round 2 of 4"`, `"Short break"`, `"Long break"`

- [ ] **Step 1: Write the failing tests**

`internal/apps/focus/phases_test.go`:

```go
package focus_test

import (
	"reflect"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
)

func intervals(focusMin, breakMin, longMin, rounds, every int) focus.TimerInput {
	return focus.TimerInput{
		Name: "x", Color: "teal", Kind: focus.KindIntervals, Chime: "bell",
		FocusMinutes: focusMin, BreakMinutes: breakMin, LongBreakMinutes: longMin,
		Rounds: rounds, LongBreakEvery: every,
	}
}

func TestPhasesSingle(t *testing.T) {
	in := focus.TimerInput{Kind: focus.KindSingle, FocusMinutes: 15}
	want := []focus.Phase{{Kind: focus.PhaseFocus, Seconds: 900, Round: 1}}
	if got := focus.Phases(in); !reflect.DeepEqual(got, want) {
		t.Errorf("Phases = %+v, want %+v", got, want)
	}
}

func TestPhasesIntervalsWithLongBreaks(t *testing.T) {
	got := focus.Phases(intervals(50, 10, 30, 4, 2))
	F, B, L := focus.PhaseFocus, focus.PhaseBreak, focus.PhaseLongBreak
	want := []focus.Phase{
		{F, 3000, 1}, {B, 600, 1}, {F, 3000, 2}, {L, 1800, 2},
		{F, 3000, 3}, {B, 600, 3}, {F, 3000, 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Phases =\n%+v\nwant\n%+v", got, want)
	}
}

func TestPhasesLongBreakEveryRoundsMeansNoLongBreak(t *testing.T) {
	for _, p := range focus.Phases(intervals(25, 5, 15, 4, 4)) {
		if p.Kind == focus.PhaseLongBreak {
			t.Fatalf("got a long break with long_break_every == rounds: %+v", p)
		}
	}
}

func TestPhasesOneRoundHasNoBreak(t *testing.T) {
	got := focus.Phases(intervals(25, 5, 15, 1, 1))
	want := []focus.Phase{{Kind: focus.PhaseFocus, Seconds: 1500, Round: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Phases = %+v, want %+v", got, want)
	}
}

func TestPhasesNeverEndOnABreak(t *testing.T) {
	for rounds := 1; rounds <= 12; rounds++ {
		for every := 1; every <= rounds; every++ {
			ps := focus.Phases(intervals(25, 5, 15, rounds, every))
			if last := ps[len(ps)-1]; last.Kind != focus.PhaseFocus || last.Round != rounds {
				t.Fatalf("rounds=%d every=%d: last phase %+v", rounds, every, last)
			}
			if len(ps) != 2*rounds-1 {
				t.Fatalf("rounds=%d: %d phases, want %d", rounds, len(ps), 2*rounds-1)
			}
		}
	}
}

func TestFormatLength(t *testing.T) {
	for secs, want := range map[int]string{
		60: "1 min", 900: "15 min", 3540: "59 min", 3600: "1h", 4200: "1h 10m", 12000: "3h 20m", 90: "2 min",
	} {
		if got := focus.FormatLength(secs); got != want {
			t.Errorf("FormatLength(%d) = %q, want %q", secs, got, want)
		}
	}
}

func TestSummary(t *testing.T) {
	tests := []struct {
		in   focus.TimerInput
		want string
	}{
		{focus.TimerInput{Kind: focus.KindSingle, FocusMinutes: 15}, "15 min"},
		{intervals(50, 10, 30, 4, 2), "50 / 10 × 4 · long 30"},
		{intervals(25, 5, 15, 4, 4), "25 / 5 × 4"},
	}
	for _, tt := range tests {
		if got := focus.Summary(tt.in); got != tt.want {
			t.Errorf("Summary(%+v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPhaseLabel(t *testing.T) {
	tests := []struct {
		p      focus.Phase
		rounds int
		want   string
	}{
		{focus.Phase{Kind: focus.PhaseFocus, Round: 1}, 0, "Focus"},
		{focus.Phase{Kind: focus.PhaseFocus, Round: 2}, 4, "Focus · round 2 of 4"},
		{focus.Phase{Kind: focus.PhaseBreak, Round: 1}, 4, "Short break"},
		{focus.Phase{Kind: focus.PhaseLongBreak, Round: 2}, 4, "Long break"},
	}
	for _, tt := range tests {
		if got := tt.p.Label(tt.rounds); got != tt.want {
			t.Errorf("Label(%+v, %d) = %q, want %q", tt.p, tt.rounds, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/focus/ -run 'TestPhases|TestFormatLength|TestSummary|TestPhaseLabel' -count=1`
Expected: FAIL — `undefined: focus.Phase`.

- [ ] **Step 3: Write `phases.go`**

```go
package focus

import (
	"fmt"
	"strconv"
)

// PhaseKind is what a phase of a running timer is for.
type PhaseKind string

// The three kinds of phase.
const (
	PhaseFocus     PhaseKind = "focus"
	PhaseBreak     PhaseKind = "break"
	PhaseLongBreak PhaseKind = "long_break"
)

// Phase is one stretch of a running timer. Round is the focus round, or
// for a break the round just finished. The JSON tags are what the running
// page (F2) reads.
type Phase struct {
	Kind    PhaseKind `json:"kind"`
	Seconds int       `json:"seconds"`
	Round   int       `json:"round"`
}

// Phases expands a timer into the phases it runs through (spec: "Phase
// list"). A single timer is one focus phase. Intervals alternate focus and
// breaks, with a long break after every LongBreakEvery-th round, and never
// end on a break — so LongBreakEvery == Rounds means no long break at all.
func Phases(in TimerInput) []Phase {
	if in.Kind != KindIntervals {
		return []Phase{{Kind: PhaseFocus, Seconds: in.FocusMinutes * 60, Round: 1}}
	}
	out := make([]Phase, 0, 2*in.Rounds-1)
	for round := 1; round <= in.Rounds; round++ {
		if round > 1 {
			prev := round - 1
			if in.LongBreakEvery > 0 && prev%in.LongBreakEvery == 0 {
				out = append(out, Phase{Kind: PhaseLongBreak, Seconds: in.LongBreakMinutes * 60, Round: prev})
			} else {
				out = append(out, Phase{Kind: PhaseBreak, Seconds: in.BreakMinutes * 60, Round: prev})
			}
		}
		out = append(out, Phase{Kind: PhaseFocus, Seconds: in.FocusMinutes * 60, Round: round})
	}
	return out
}

// TotalSeconds is the whole length of a run, breaks included.
func TotalSeconds(phases []Phase) int {
	total := 0
	for _, p := range phases {
		total += p.Seconds
	}
	return total
}

// FormatLength renders a duration the way the tiles show it: "15 min"
// under an hour (rounded up to a whole minute), "1h" or "3h 20m" above.
func FormatLength(seconds int) string {
	minutes := (seconds + 59) / 60
	if minutes < 60 {
		return strconv.Itoa(minutes) + " min"
	}
	h, m := minutes/60, minutes%60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// Summary is a tile's second line: "15 min", "25 / 5 × 4", or with a long
// break that actually happens, "50 / 10 × 4 · long 30".
func Summary(in TimerInput) string {
	if in.Kind != KindIntervals {
		return strconv.Itoa(in.FocusMinutes) + " min"
	}
	s := fmt.Sprintf("%d / %d × %d", in.FocusMinutes, in.BreakMinutes, in.Rounds)
	if in.LongBreakEvery < in.Rounds {
		s += fmt.Sprintf(" · long %d", in.LongBreakMinutes)
	}
	return s
}

// Label names a phase for people. rounds is the timer's round count, 0 for
// a single timer (whose one phase is just "Focus").
func (p Phase) Label(rounds int) string {
	switch p.Kind {
	case PhaseBreak:
		return "Short break"
	case PhaseLongBreak:
		return "Long break"
	}
	if rounds == 0 {
		return "Focus"
	}
	return fmt.Sprintf("Focus · round %d of %d", p.Round, rounds)
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/apps/focus/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/focus/phases.go internal/apps/focus/phases_test.go
git commit -m "feat(focus): expand timers into phases; summaries and lengths (#493)"
```

---

### Task 5: Store — create, read, list, update, delete

**Files:**
- Modify: `internal/apps/focus/store.go`
- Test: `internal/apps/focus/store_test.go`

**Interfaces:**
- Consumes: `TimerInput`, `Normalize`, `Validate`, `ValidationError` (Task 3).
- Produces:
  - `type Timer struct { ID, UserID int64; TimerInput; Position int; CreatedAt, UpdatedAt time.Time }` (embeds `TimerInput`, so `t.Name`, `t.Kind`, `Phases(t.TimerInput)` work)
  - `func (st *Store) CreateTimer(ctx context.Context, userID int64, in TimerInput) (Timer, error)` — new timers go last
  - `func (st *Store) Timer(ctx context.Context, userID, id int64) (Timer, error)` — `ErrNotFound` for missing or foreign
  - `func (st *Store) Timers(ctx context.Context, userID int64) ([]Timer, error)` — by `position, id`
  - `func (st *Store) UpdateTimer(ctx context.Context, userID, id int64, in TimerInput) (Timer, error)` — keeps position
  - `func (st *Store) DeleteTimer(ctx context.Context, userID, id int64) error`
  - Invalid input returns `*ValidationError` (`errors.Is(err, ErrInvalid)`).

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/focus/store_test.go` (add `"errors"` to its imports):

```go
func (f *fixture) tick() { f.now = f.now.Add(time.Minute) }

func (f *fixture) create(t *testing.T, userID int64, name string) focus.Timer {
	t.Helper()
	in := focus.DefaultInput()
	in.Name = name
	tm, err := f.store.CreateTimer(context.Background(), userID, in)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func names(ts []focus.Timer) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Name)
	}
	return out
}

func TestCreateTimerStoresEveryField(t *testing.T) {
	f := newFixture(t)
	in := validIntervals()
	in.Name = "  Deep work "
	got, err := f.store.CreateTimer(context.Background(), f.alice.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	back, err := f.store.Timer(context.Background(), f.alice.ID, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := in.Normalize()
	if back.TimerInput != want {
		t.Errorf("stored %+v, want %+v", back.TimerInput, want)
	}
	if !back.CreatedAt.Equal(f.now) || !back.UpdatedAt.Equal(f.now) || back.UserID != f.alice.ID {
		t.Errorf("timestamps/user = %v %v %d", back.CreatedAt, back.UpdatedAt, back.UserID)
	}
}

func TestCreateTimerSingleStoresNullIntervals(t *testing.T) {
	f := newFixture(t)
	tm := f.create(t, f.alice.ID, "Reading")
	var rounds sql.NullInt64
	if err := f.db.QueryRow(`SELECT rounds FROM focus_timers WHERE id = ?`, tm.ID).Scan(&rounds); err != nil {
		t.Fatal(err)
	}
	if rounds.Valid {
		t.Errorf("rounds = %d, want NULL for a single timer", rounds.Int64)
	}
}

func TestCreateTimerAppendsInOrder(t *testing.T) {
	f := newFixture(t)
	f.create(t, f.alice.ID, "A")
	f.create(t, f.alice.ID, "B")
	f.create(t, f.bob.ID, "Bob's")
	f.create(t, f.alice.ID, "C")
	ts, err := f.store.Timers(context.Background(), f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(ts); !slices.Equal(got, []string{"A", "B", "C"}) {
		t.Errorf("Timers = %v, want [A B C]", got)
	}
}

func TestCreateTimerRejectsInvalidInput(t *testing.T) {
	f := newFixture(t)
	in := focus.DefaultInput() // no name
	_, err := f.store.CreateTimer(context.Background(), f.alice.ID, in)
	var ve *focus.ValidationError
	if !errors.As(err, &ve) || ve.Fields["name"] == "" || !errors.Is(err, focus.ErrInvalid) {
		t.Fatalf("CreateTimer(no name) = %v, want a ValidationError for name", err)
	}
}

func TestTimerIsNotFoundForSomeoneElse(t *testing.T) {
	f := newFixture(t)
	tm := f.create(t, f.alice.ID, "Mine")
	if _, err := f.store.Timer(context.Background(), f.bob.ID, tm.ID); !errors.Is(err, focus.ErrNotFound) {
		t.Errorf("bob reading alice's timer = %v, want ErrNotFound", err)
	}
	if _, err := f.store.Timer(context.Background(), f.alice.ID, 9999); !errors.Is(err, focus.ErrNotFound) {
		t.Errorf("missing timer = %v, want ErrNotFound", err)
	}
}

func TestUpdateTimerChangesFieldsAndKeepsPosition(t *testing.T) {
	f := newFixture(t)
	f.create(t, f.alice.ID, "First")
	tm := f.create(t, f.alice.ID, "Second")
	f.tick()
	in := validIntervals()
	got, err := f.store.UpdateTimer(context.Background(), f.alice.ID, tm.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.TimerInput != in.Normalize() || got.Position != tm.Position {
		t.Errorf("updated = %+v (pos %d), want %+v (pos %d)", got.TimerInput, got.Position, in, tm.Position)
	}
	if !got.UpdatedAt.Equal(f.now) || !got.CreatedAt.Equal(tm.CreatedAt) {
		t.Errorf("updated_at %v created_at %v", got.UpdatedAt, got.CreatedAt)
	}
}

func TestUpdateTimerToSingleClearsIntervals(t *testing.T) {
	f := newFixture(t)
	tm, err := f.store.CreateTimer(context.Background(), f.alice.ID, validIntervals())
	if err != nil {
		t.Fatal(err)
	}
	in := tm.TimerInput
	in.Kind = focus.KindSingle
	got, err := f.store.UpdateTimer(context.Background(), f.alice.ID, tm.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Rounds != 0 || got.BreakMinutes != 0 {
		t.Errorf("single timer kept intervals: %+v", got.TimerInput)
	}
}

func TestUpdateTimerRefusesSomeoneElsesAndInvalid(t *testing.T) {
	f := newFixture(t)
	tm := f.create(t, f.alice.ID, "Mine")
	if _, err := f.store.UpdateTimer(context.Background(), f.bob.ID, tm.ID, validSingle()); !errors.Is(err, focus.ErrNotFound) {
		t.Errorf("bob updating = %v, want ErrNotFound", err)
	}
	bad := validSingle()
	bad.FocusMinutes = 0
	if _, err := f.store.UpdateTimer(context.Background(), f.alice.ID, tm.ID, bad); !errors.Is(err, focus.ErrInvalid) {
		t.Errorf("invalid update = %v, want ErrInvalid", err)
	}
}

func TestDeleteTimer(t *testing.T) {
	f := newFixture(t)
	tm := f.create(t, f.alice.ID, "Gone")
	if err := f.store.DeleteTimer(context.Background(), f.bob.ID, tm.ID); !errors.Is(err, focus.ErrNotFound) {
		t.Errorf("bob deleting = %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteTimer(context.Background(), f.alice.ID, tm.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Timer(context.Background(), f.alice.ID, tm.ID); !errors.Is(err, focus.ErrNotFound) {
		t.Errorf("after delete = %v, want ErrNotFound", err)
	}
}
```
Also add `"slices"` to the imports.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/focus/ -count=1`
Expected: FAIL — `f.store.CreateTimer undefined`.

- [ ] **Step 3: Implement the CRUD in `store.go`**

Add imports `"context"`, `"fmt"` and `"github.com/iliafrenkel/on-suite/internal/platform/db"`, then:

```go
// Timer is one saved timer.
type Timer struct {
	ID, UserID int64
	TimerInput
	Position             int
	CreatedAt, UpdatedAt time.Time
}

// timerColumns is every column scanTimer reads, in order.
const timerColumns = `id, user_id, name, color, kind, focus_minutes, break_minutes,
	long_break_minutes, rounds, long_break_every, auto_advance, chime, keep_history,
	position, created_at, updated_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanTimer(row rowScanner) (Timer, error) {
	var t Timer
	var brk, long, rounds, every sql.NullInt64
	var created, updated string
	err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.Color, &t.Kind, &t.FocusMinutes, &brk,
		&long, &rounds, &every, &t.AutoAdvance, &t.Chime, &t.KeepHistory,
		&t.Position, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Timer{}, ErrNotFound
	}
	if err != nil {
		return Timer{}, fmt.Errorf("focus: load timer: %w", err)
	}
	t.BreakMinutes, t.LongBreakMinutes = int(brk.Int64), int(long.Int64)
	t.Rounds, t.LongBreakEvery = int(rounds.Int64), int(every.Int64)
	if t.CreatedAt, err = db.ParseTime(created); err != nil {
		return Timer{}, fmt.Errorf("focus: timer created_at: %w", err)
	}
	if t.UpdatedAt, err = db.ParseTime(updated); err != nil {
		return Timer{}, fmt.Errorf("focus: timer updated_at: %w", err)
	}
	return t, nil
}

// checked normalizes and validates input, the one gate every write goes
// through.
func checked(in TimerInput) (TimerInput, error) {
	in = in.Normalize()
	if errs := in.Validate(); errs != nil {
		return in, &ValidationError{Fields: errs}
	}
	return in, nil
}

// nullable stores 0 as NULL: a single timer's interval columns.
func nullable(n int) sql.NullInt64 { return sql.NullInt64{Int64: int64(n), Valid: n != 0} }

// flag binds a bool as SQLite's 0/1.
func flag(b bool) int {
	if b {
		return 1
	}
	return 0
}

// CreateTimer stores a new timer at the end of the user's list.
func (st *Store) CreateTimer(ctx context.Context, userID int64, in TimerInput) (Timer, error) {
	in, err := checked(in)
	if err != nil {
		return Timer{}, err
	}
	now := db.FormatTime(st.now())
	res, err := st.db.ExecContext(ctx, `
		INSERT INTO focus_timers (user_id, name, color, kind, focus_minutes, break_minutes,
			long_break_minutes, rounds, long_break_every, auto_advance, chime, keep_history,
			position, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			(SELECT COALESCE(MAX(position), -1) + 1 FROM focus_timers WHERE user_id = ?), ?, ?)`,
		userID, in.Name, in.Color, in.Kind, in.FocusMinutes, nullable(in.BreakMinutes),
		nullable(in.LongBreakMinutes), nullable(in.Rounds), nullable(in.LongBreakEvery),
		flag(in.AutoAdvance), in.Chime, flag(in.KeepHistory), userID, now, now)
	if err != nil {
		return Timer{}, fmt.Errorf("focus: create timer: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Timer{}, fmt.Errorf("focus: create timer: %w", err)
	}
	return st.Timer(ctx, userID, id)
}

// Timer loads one of the user's timers.
func (st *Store) Timer(ctx context.Context, userID, id int64) (Timer, error) {
	return scanTimer(st.db.QueryRowContext(ctx,
		`SELECT `+timerColumns+` FROM focus_timers WHERE user_id = ? AND id = ?`, userID, id))
}

// Timers lists the user's timers in tile order.
func (st *Store) Timers(ctx context.Context, userID int64) ([]Timer, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT `+timerColumns+` FROM focus_timers WHERE user_id = ? ORDER BY position, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("focus: list timers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Timer
	for rows.Next() {
		t, err := scanTimer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("focus: list timers: %w", err)
	}
	return out, nil
}

// UpdateTimer replaces every setting of one of the user's timers; its
// place in the list doesn't change.
func (st *Store) UpdateTimer(ctx context.Context, userID, id int64, in TimerInput) (Timer, error) {
	in, err := checked(in)
	if err != nil {
		return Timer{}, err
	}
	res, err := st.db.ExecContext(ctx, `
		UPDATE focus_timers SET name = ?, color = ?, kind = ?, focus_minutes = ?,
			break_minutes = ?, long_break_minutes = ?, rounds = ?, long_break_every = ?,
			auto_advance = ?, chime = ?, keep_history = ?, updated_at = ?
		WHERE user_id = ? AND id = ?`,
		in.Name, in.Color, in.Kind, in.FocusMinutes, nullable(in.BreakMinutes),
		nullable(in.LongBreakMinutes), nullable(in.Rounds), nullable(in.LongBreakEvery),
		flag(in.AutoAdvance), in.Chime, flag(in.KeepHistory), db.FormatTime(st.now()), userID, id)
	if err != nil {
		return Timer{}, fmt.Errorf("focus: update timer: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return Timer{}, fmt.Errorf("focus: update timer: %w", err)
	} else if n == 0 {
		return Timer{}, ErrNotFound
	}
	return st.Timer(ctx, userID, id)
}

// DeleteTimer removes one of the user's timers. Its recorded sessions stay
// (timer_id becomes NULL; they keep their own copy of the name and colour).
func (st *Store) DeleteTimer(ctx context.Context, userID, id int64) error {
	res, err := st.db.ExecContext(ctx, `DELETE FROM focus_timers WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return fmt.Errorf("focus: delete timer: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("focus: delete timer: %w", err)
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}
```
`Kind` is a `string` type, so it binds and scans directly.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/apps/focus/ -count=1 -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/focus/store.go internal/apps/focus/store_test.go
git commit -m "feat(focus): store timers — create, read, list, update, delete (#493)"
```

---

### Task 6: Store — duplicate and reorder

**Files:**
- Modify: `internal/apps/focus/store.go`
- Test: `internal/apps/focus/store_test.go`

**Interfaces:**
- Consumes: `Timer`, `CreateTimer`, `Timer`, `Timers` (Task 5).
- Produces:
  - `func (st *Store) DuplicateTimer(ctx context.Context, userID, id int64) (Timer, error)` — copy named `"<name> (copy)"` (truncated so it fits `MaxNameRunes`), placed right after the original
  - `func (st *Store) ReorderTimers(ctx context.Context, userID int64, ids []int64) error` — `ids` must be exactly the user's timers, each once, else `ErrInvalid`

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/focus/store_test.go` (add `"strings"` and `"unicode/utf8"` imports):

```go
func timerIDs(ts []focus.Timer) []int64 {
	var out []int64
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}

func TestDuplicateTimerPlacesTheCopyAfterTheOriginal(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.create(t, f.alice.ID, "A")
	orig, err := f.store.CreateTimer(ctx, f.alice.ID, validIntervals())
	if err != nil {
		t.Fatal(err)
	}
	c := f.create(t, f.alice.ID, "C")
	dup, err := f.store.DuplicateTimer(ctx, f.alice.ID, orig.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantIn := orig.TimerInput
	wantIn.Name = "Deep work (copy)"
	if dup.TimerInput != wantIn {
		t.Errorf("copy = %+v, want %+v", dup.TimerInput, wantIn)
	}
	ts, err := f.store.Timers(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := timerIDs(ts), []int64{a.ID, orig.ID, dup.ID, c.ID}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestDuplicateTimerKeepsLongNamesWithinTheLimit(t *testing.T) {
	f := newFixture(t)
	orig := f.create(t, f.alice.ID, strings.Repeat("é", 80))
	dup, err := f.store.DuplicateTimer(context.Background(), f.alice.ID, orig.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(dup.Name); n != 80 || !strings.HasSuffix(dup.Name, " (copy)") {
		t.Errorf("copy name %q has %d characters, want 80 ending in (copy)", dup.Name, n)
	}
}

func TestDuplicateTimerIsNotFoundForSomeoneElse(t *testing.T) {
	f := newFixture(t)
	orig := f.create(t, f.alice.ID, "Mine")
	if _, err := f.store.DuplicateTimer(context.Background(), f.bob.ID, orig.ID); !errors.Is(err, focus.ErrNotFound) {
		t.Errorf("bob duplicating = %v, want ErrNotFound", err)
	}
}

func TestReorderTimers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, b, c := f.create(t, f.alice.ID, "A"), f.create(t, f.alice.ID, "B"), f.create(t, f.alice.ID, "C")
	if err := f.store.ReorderTimers(ctx, f.alice.ID, []int64{c.ID, a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	ts, err := f.store.Timers(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(ts); !slices.Equal(got, []string{"C", "A", "B"}) {
		t.Errorf("order = %v, want [C A B]", got)
	}
}

func TestReorderTimersRefusesAnythingButTheExactSet(t *testing.T) {
	f := newFixture(t)
	a, b := f.create(t, f.alice.ID, "A"), f.create(t, f.alice.ID, "B")
	other := f.create(t, f.bob.ID, "Bob's")
	for name, ids := range map[string][]int64{
		"missing one": {a.ID},
		"duplicate":   {a.ID, a.ID},
		"foreign":     {a.ID, b.ID, other.ID},
		"swapped in":  {a.ID, other.ID},
		"empty":       {},
	} {
		if err := f.store.ReorderTimers(context.Background(), f.alice.ID, ids); !errors.Is(err, focus.ErrInvalid) {
			t.Errorf("%s: ReorderTimers = %v, want ErrInvalid", name, err)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/focus/ -run 'Duplicate|Reorder' -count=1`
Expected: FAIL — `f.store.DuplicateTimer undefined`.

- [ ] **Step 3: Implement both in `store.go`**

Add `"slices"` and `"unicode/utf8"` to the imports, then:

```go
const copySuffix = " (copy)"

// copyName is name with " (copy)" on the end, shortened first if needed so
// the result still fits MaxNameRunes.
func copyName(name string) string {
	room := MaxNameRunes - utf8.RuneCountInString(copySuffix)
	if r := []rune(name); len(r) > room {
		name = string(r[:room])
	}
	return name + copySuffix
}

// DuplicateTimer copies one of the user's timers, placing the copy right
// after the original.
func (st *Store) DuplicateTimer(ctx context.Context, userID, id int64) (Timer, error) {
	orig, err := st.Timer(ctx, userID, id)
	if err != nil {
		return Timer{}, err
	}
	in, err := checked(TimerInput{
		Name: copyName(orig.Name), Color: orig.Color, Kind: orig.Kind,
		FocusMinutes: orig.FocusMinutes, BreakMinutes: orig.BreakMinutes,
		LongBreakMinutes: orig.LongBreakMinutes, Rounds: orig.Rounds,
		LongBreakEvery: orig.LongBreakEvery, AutoAdvance: orig.AutoAdvance,
		KeepHistory: orig.KeepHistory, Chime: orig.Chime,
	})
	if err != nil {
		return Timer{}, err
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return Timer{}, fmt.Errorf("focus: duplicate timer: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE focus_timers SET position = position + 1 WHERE user_id = ? AND position > ?`,
		userID, orig.Position); err != nil {
		return Timer{}, fmt.Errorf("focus: duplicate timer: %w", err)
	}
	now := db.FormatTime(st.now())
	res, err := tx.ExecContext(ctx, `
		INSERT INTO focus_timers (user_id, name, color, kind, focus_minutes, break_minutes,
			long_break_minutes, rounds, long_break_every, auto_advance, chime, keep_history,
			position, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, in.Name, in.Color, in.Kind, in.FocusMinutes, nullable(in.BreakMinutes),
		nullable(in.LongBreakMinutes), nullable(in.Rounds), nullable(in.LongBreakEvery),
		flag(in.AutoAdvance), in.Chime, flag(in.KeepHistory), orig.Position+1, now, now)
	if err != nil {
		return Timer{}, fmt.Errorf("focus: duplicate timer: %w", err)
	}
	newID, err := res.LastInsertId()
	if err != nil {
		return Timer{}, fmt.Errorf("focus: duplicate timer: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Timer{}, fmt.Errorf("focus: duplicate timer: %w", err)
	}
	return st.Timer(ctx, userID, newID)
}

// ReorderTimers sets the tile order. ids must name every one of the user's
// timers exactly once — anything else (a stale page, someone else's id) is
// refused rather than half-applied.
func (st *Store) ReorderTimers(ctx context.Context, userID int64, ids []int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("focus: reorder timers: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM focus_timers WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("focus: reorder timers: %w", err)
	}
	var have []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("focus: reorder timers: %w", err)
		}
		have = append(have, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("focus: reorder timers: %w", err)
	}
	_ = rows.Close()
	want := slices.Clone(ids)
	slices.Sort(have)
	slices.Sort(want)
	if len(want) == 0 || !slices.Equal(have, want) {
		return fmt.Errorf("%w: order must list each timer once", ErrInvalid)
	}
	for pos, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE focus_timers SET position = ? WHERE user_id = ? AND id = ?`, pos, userID, id); err != nil {
			return fmt.Errorf("focus: reorder timers: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("focus: reorder timers: %w", err)
	}
	return nil
}
```
(The duplicate check is covered by `slices.Equal` on the sorted lists: `{a, a}` sorted is not equal to `{a, b}`.)

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/apps/focus/ -count=1 -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/focus/store.go internal/apps/focus/store_test.go
git commit -m "feat(focus): duplicate and reorder timers (#493)"
```

---

### Task 7: Home page tiles, duplicate and delete

**Files:**
- Modify: `internal/apps/focus/handlers.go`, `internal/apps/focus/focus.go` (routes), `internal/apps/focus/templates/index.html`, `internal/apps/focus/static/home.js`, `internal/ui/static/app.css` (new "ON Focus" section at the end of the file)
- Test: `internal/apps/focus/handlers_test.go`

**Interfaces:**
- Consumes: `Timers`, `DuplicateTimer`, `DeleteTimer` (Tasks 5–6); `Summary`, `Phases`, `TotalSeconds`, `FormatLength` (Task 4).
- Produces: routes `POST /focus/timers/{id}/duplicate` and `POST /focus/timers/{id}/delete` (both 303 → `/focus/`); tile markup `li.focus-tile.swatch-c-<color>[data-id]` inside `ul.focus-tiles[data-focus-tiles]`, used by Task 9's drag code.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/focus/handlers_test.go` (add imports `"context"`, `"net/url"`, `"strings"`, `"github.com/iliafrenkel/on-suite/internal/htmlassert"`):

```go
func seedTimer(t *testing.T, s *server, userID int64, in focus.TimerInput) focus.Timer {
	t.Helper()
	tm, err := s.Store.CreateTimer(context.Background(), userID, in)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func single(name string, minutes int) focus.TimerInput {
	in := focus.DefaultInput()
	in.Name, in.FocusMinutes = name, minutes
	return in
}

func TestIndexShowsTilesInOrder(t *testing.T) {
	s := newServer(t)
	deep := validIntervals() // Deep work, blue, 50/10 × 4, long 30 every 2
	seedTimer(t, s, s.Alice.User.ID, single("Daily Reflection", 15))
	deepTimer := seedTimer(t, s, s.Alice.User.ID, deep)
	seedTimer(t, s, s.Bob.User.ID, single("Bob's timer", 5))

	doc := s.Get(t, s.Alice, "/focus/")
	doc.MustNotHave(".focus-empty")
	tiles := doc.QueryAll(".focus-tile")
	if len(tiles) != 2 {
		t.Fatalf("got %d tiles, want 2", len(tiles))
	}
	var gotNames []string
	for _, n := range doc.QueryAll(".focus-tile-name") {
		gotNames = append(gotNames, htmlassert.Text(n))
	}
	if strings.Join(gotNames, "|") != "Daily Reflection|Deep work" {
		t.Errorf("tile names = %v", gotNames)
	}
	if class, _ := htmlassert.Attr(tiles[1], "class"); !strings.Contains(class, "swatch-c-blue") {
		t.Errorf("Deep work tile class = %q, want swatch-c-blue", class)
	}
	summaries := doc.QueryAll(".focus-tile-summary")
	if got := htmlassert.Text(summaries[1]); got != "50 / 10 × 4 · long 30" {
		t.Errorf("summary = %q", got)
	}
	pills := doc.QueryAll(".focus-pill")
	if got := htmlassert.Text(pills[0]); got != "15 min" {
		t.Errorf("single pill = %q, want 15 min", got)
	}
	if got := htmlassert.Text(pills[1]); got != "4h 10m" { // 4×50 + 10 + 30 + 10
		t.Errorf("intervals pill = %q, want 4h 10m", got)
	}
	doc.MustHave(`a[href="/focus/run/` + itoa(deepTimer.ID) + `"]`)
}

func TestDuplicateFromTheHomePage(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
	s.Submit(t, s.Alice, "/focus/timers/"+itoa(tm.ID)+"/duplicate", url.Values{}, "/focus/")
	ts, err := s.Store.Timers(context.Background(), s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 2 || ts[1].Name != "Reading (copy)" {
		t.Errorf("after duplicate: %v", names(ts))
	}
}

func TestDeleteFromTheHomePage(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
	s.Submit(t, s.Alice, "/focus/timers/"+itoa(tm.ID)+"/delete", url.Values{}, "/focus/")
	ts, err := s.Store.Timers(context.Background(), s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 0 {
		t.Errorf("after delete: %v", names(ts))
	}
}

func TestDuplicateAndDeleteAreNotFoundForSomeoneElse(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Mine", 30))
	for _, action := range []string{"duplicate", "delete"} {
		rec := s.Post(t, s.Bob, "/focus/timers/"+itoa(tm.ID)+"/"+action, url.Values{})
		if rec.Code != http.StatusNotFound {
			t.Errorf("bob %s = %d, want 404", action, rec.Code)
		}
	}
	if rec := s.Post(t, s.Alice, "/focus/timers/abc/delete", url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("non-numeric id = %d, want 404", rec.Code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
```
Add `"strconv"` to the imports. `validIntervals` and `names` come from the other `_test.go` files in the same package.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/focus/ -run 'TestIndex|TestDuplicateFrom|TestDeleteFrom|TestDuplicateAndDelete' -count=1`
Expected: FAIL — no `.focus-tile`, and 404s from unregistered routes.

- [ ] **Step 3: Fill the index handler and add duplicate/delete**

In `handlers.go`, replace `index` and add:

```go
func newTile(t Timer) tileView {
	return tileView{
		ID: t.ID, Name: t.Name, Color: t.Color,
		Summary: Summary(t.TimerInput),
		Total:   FormatLength(TotalSeconds(Phases(t.TimerInput))),
	}
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	timers, err := a.store.Timers(r.Context(), userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	view := indexView{}
	for _, t := range timers {
		view.Tiles = append(view.Tiles, newTile(t))
	}
	a.render(w, r, http.StatusOK, "focus/index", "Timers", view)
}

func (a *App) duplicate(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if _, err := a.store.DuplicateTimer(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/focus/", http.StatusSeeOther)
}

func (a *App) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteTimer(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/focus/", http.StatusSeeOther)
}
```

In `focus.go` `Mount`, after the index route:

```go
	r.HandleFunc("POST /timers/{id}/duplicate", a.duplicate)
	r.HandleFunc("POST /timers/{id}/delete", a.delete)
```

- [ ] **Step 4: Render the tiles**

In `templates/index.html`, replace the empty `{{if .Data.Tiles}}` branch with:

```html
	{{if .Data.Tiles}}
	<ul class="focus-tiles" data-focus-tiles>
		{{range .Data.Tiles}}
		<li class="focus-tile swatch-c-{{.Color}}" data-id="{{.ID}}" draggable="true">
			<div class="focus-tile-head">
				<a class="focus-tile-name" href="/focus/timers/{{.ID}}/edit" draggable="false">{{.Name}}</a>
				<details class="focus-menu">
					<summary aria-label="Actions for {{.Name}}">⋯</summary>
					<div class="focus-menu-panel stack">
						<a href="/focus/timers/{{.ID}}/edit">Edit</a>
						<form method="post" action="/focus/timers/{{.ID}}/duplicate">
							<input type="hidden" name="{{csrfField}}" value="{{$.Shell.CSRFToken}}">
							<button type="submit">Duplicate</button>
						</form>
						<form method="post" action="/focus/timers/{{.ID}}/delete" data-focus-confirm="Delete “{{.Name}}”? Sessions already in your history are kept.">
							<input type="hidden" name="{{csrfField}}" value="{{$.Shell.CSRFToken}}">
							<button class="danger" type="submit">Delete</button>
						</form>
					</div>
				</details>
			</div>
			<span class="focus-tile-summary">{{.Summary}}</span>
			<div class="focus-tile-foot">
				<span class="focus-pill">{{.Total}}</span>
				<a class="focus-play" href="/focus/run/{{.ID}}" draggable="false" aria-label="Start {{.Name}}">▶</a>
			</div>
		</li>
		{{end}}
	</ul>
	{{else}}
```
(The name links to Edit, which Task 8 adds; the link 404s until then, which no test follows.)

- [ ] **Step 5: Fill the confirmation in `home.js`**

Replace `internal/apps/focus/static/home.js` with:

```js
// ON Focus's home-page script. Forms marked data-focus-confirm ask first,
// in the app's own dialog (later.js's pattern); without JavaScript the form
// simply submits. Drag-to-reorder is added in Task 9.
"use strict";

(function () {
	// confirmThen asks message in the app's dialog and calls onOK on OK.
	function confirmThen(message, onOK) {
		var dialog = document.getElementById("focus-confirm-dialog");
		document.getElementById("focus-confirm-message").textContent = message;

		// Listeners are tied to this one opening of the dialog, so a
		// cancelled confirmation can never fire later (the bug reader.js
		// documents).
		var controller = new AbortController();
		dialog.addEventListener("close", function () { controller.abort(); }, { once: true });
		document.getElementById("focus-confirm-ok").addEventListener("click", function () {
			dialog.close();
			onOK();
		}, { signal: controller.signal });
		document.getElementById("focus-confirm-cancel").addEventListener("click", function () {
			dialog.close();
		}, { signal: controller.signal });
		dialog.showModal();
	}

	document.addEventListener("submit", function (e) {
		var form = e.target;
		if (!(form instanceof HTMLFormElement) || !form.dataset.focusConfirm) return;
		if (form.dataset.focusConfirmed === "1") return;

		var dialog = document.getElementById("focus-confirm-dialog");
		if (!dialog || typeof dialog.showModal !== "function") return;

		e.preventDefault();
		confirmThen(form.dataset.focusConfirm, function () {
			form.dataset.focusConfirmed = "1";
			form.requestSubmit();
		});
	});
})();
```

- [ ] **Step 6: Add the CSS**

Append to `internal/ui/static/app.css`:

```css
/* ---- ON Focus ------------------------------------------------------------
 * Spec: docs/superpowers/specs/2026-10-07-on-focus-design.md. A timer's
 * colour arrives as a swatch-c-<name> class (see "Colour swatches"). */

.focus-page { max-width: 64rem; }
.focus-toolbar { display: flex; align-items: center; justify-content: space-between; gap: var(--s-3); }
.focus-toolbar h1 { margin: 0; }
.focus-empty { padding: var(--s-5); background: var(--c-bg-subtle); border-radius: var(--radius); color: var(--c-text-dim); }

.focus-tiles {
	list-style: none;
	margin: 0;
	padding: 0;
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(12rem, 1fr));
	gap: var(--s-3);
}
.focus-tile {
	display: flex;
	flex-direction: column;
	gap: var(--s-1);
	padding: var(--s-3);
	background: var(--c-bg);
	border: var(--border);
	border-top: 4px solid var(--swatch);
	border-radius: var(--radius);
	cursor: grab;
}
.focus-tile-dragging { opacity: .5; }
.focus-tile-head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--s-2); }
.focus-tile-name { font-weight: 600; color: var(--c-text); text-decoration: none; overflow-wrap: anywhere; }
.focus-tile-name:hover { text-decoration: underline; }
.focus-tile-summary { color: var(--c-text-dim); font-size: var(--fs-sm); }
.focus-tile-foot { display: flex; align-items: center; justify-content: space-between; margin-top: auto; padding-top: var(--s-2); }
.focus-pill { font-size: var(--fs-xs); color: var(--c-text-faint); border: var(--border); border-radius: 999px; padding: 0 var(--s-2); }
.focus-play {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 2.25rem;
	height: 2.25rem;
	border-radius: 50%;
	background: var(--swatch);
	color: #fff;
	text-decoration: none;
	font-size: var(--fs-sm);
}
.focus-play:hover { filter: brightness(1.1); }

.focus-menu { position: relative; }
.focus-menu > summary { list-style: none; cursor: pointer; padding: 0 var(--s-2); color: var(--c-text-faint); border-radius: var(--radius); }
.focus-menu > summary::-webkit-details-marker { display: none; }
.focus-menu > summary:hover { background: var(--c-bg-inset); color: var(--c-text); }
.focus-menu-panel {
	position: absolute;
	right: 0;
	top: calc(100% + .25rem);
	z-index: 2;
	min-width: 10rem;
	padding: var(--s-2);
	background: var(--c-bg);
	border: var(--border);
	border-radius: var(--radius);
	box-shadow: 0 6px 20px rgba(0,0,0,.12);
	cursor: default;
}
.focus-menu-panel form { margin: 0; }
.focus-menu-panel button, .focus-menu-panel a { display: block; width: 100%; text-align: left; }
```
(`--fs-sm`, `--fs-xs`, `--s-1`…`--s-5`, `--radius`, `--measure` and `--ring` are all existing tokens in `:root`.)

- [ ] **Step 7: Run the tests, then the full check**

Run: `go test ./internal/apps/focus/... ./internal/ui/... -count=1`
Expected: PASS. Then the full check from Global Constraints.

- [ ] **Step 8: Commit**

```bash
git add -A internal/apps/focus internal/ui/static/app.css
git commit -m "feat(focus): home page tiles with duplicate and delete (#493)"
```

---

### Task 8: New and edit timer form

**Files:**
- Create: `internal/apps/focus/form.go`, `internal/apps/focus/templates/form.html`
- Modify: `internal/apps/focus/handlers.go`, `internal/apps/focus/focus.go` (routes), `internal/apps/focus/templates/focus.partial.html`, `internal/ui/static/app.css`
- Test: `internal/apps/focus/form_test.go`

**Interfaces:**
- Consumes: `TimerInput`, `DefaultInput`, `FieldErrors`, `ValidationError`, `Colors`, `Chimes`, `KindSingle`, `KindIntervals` (Task 3); `CreateTimer`, `Timer`, `UpdateTimer` (Task 5).
- Produces: routes `GET /focus/new`, `POST /focus/timers`, `GET /focus/timers/{id}/edit`, `POST /focus/timers/{id}`. Form field names: `name color kind focus break long_break rounds long_break_every auto_advance chime keep_history`.

- [ ] **Step 1: Write the failing tests**

`internal/apps/focus/form_test.go`:

```go
package focus_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func formValues(kind string) url.Values {
	return url.Values{
		"name": {"Deep work"}, "color": {"purple"}, "kind": {kind},
		"focus": {"50"}, "break": {"10"}, "long_break": {"30"}, "rounds": {"4"}, "long_break_every": {"2"},
		"auto_advance": {"1"}, "chime": {"bowl"}, "keep_history": {"1"},
	}
}

func TestNewFormStartsWithDefaults(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/focus/new")
	doc.MustHave(`form.focus-form[action="/focus/timers"]`)
	doc.MustHave(`input[name="kind"][value="single"][checked]`)
	doc.MustHave(`input[name="color"][value="teal"][checked]`)
	doc.MustHave(`input[name="auto_advance"][checked]`)
	doc.MustHave(`input[name="keep_history"][checked]`)
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name="focus"]`), "value"); v != "25" {
		t.Errorf("focus default = %q, want 25", v)
	}
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name="rounds"]`), "value"); v != "4" {
		t.Errorf("rounds default = %q, want 4", v)
	}
	doc.MustHave(`option[value="bell"][selected]`)
	if n := len(doc.QueryAll(`input[name="color"]`)); n != 8 {
		t.Errorf("%d colour swatches, want 8", n)
	}
}

func TestCreateIntervalsTimer(t *testing.T) {
	s := newServer(t)
	s.Submit(t, s.Alice, "/focus/timers", formValues("intervals"), "/focus/")
	ts, err := s.Store.Timers(context.Background(), s.Alice.User.ID)
	if err != nil || len(ts) != 1 {
		t.Fatalf("timers = %v, %v", ts, err)
	}
	want := focus.TimerInput{
		Name: "Deep work", Color: "purple", Kind: focus.KindIntervals,
		FocusMinutes: 50, BreakMinutes: 10, LongBreakMinutes: 30, Rounds: 4, LongBreakEvery: 2,
		AutoAdvance: true, KeepHistory: true, Chime: "bowl",
	}
	if ts[0].TimerInput != want {
		t.Errorf("stored %+v, want %+v", ts[0].TimerInput, want)
	}
}

func TestCreateSingleTimerIgnoresHiddenIntervalFields(t *testing.T) {
	s := newServer(t)
	v := formValues("single")
	v.Set("rounds", "not a number") // hidden for single timers
	v.Del("auto_advance")
	v.Del("keep_history")
	s.Submit(t, s.Alice, "/focus/timers", v, "/focus/")
	ts, _ := s.Store.Timers(context.Background(), s.Alice.User.ID)
	if len(ts) != 1 || ts[0].Kind != focus.KindSingle || ts[0].Rounds != 0 || ts[0].KeepHistory || ts[0].AutoAdvance {
		t.Errorf("stored %+v", ts)
	}
}

func TestCreateRejectsBadInputAndKeepsWhatWasTyped(t *testing.T) {
	s := newServer(t)
	v := formValues("intervals")
	v.Set("name", "")
	v.Set("rounds", "lots")
	v.Set("long_break", "90")
	rec := s.Post(t, s.Alice, "/focus/timers", v)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST = %d, want 422", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	for id, want := range map[string]string{
		"focus-name-error":       "Give the timer a name.",
		"focus-rounds-error":     "Enter a whole number.",
		"focus-long_break-error": "Long break length must be between 1 and 60 minutes.",
	} {
		if got := htmlassert.Text(doc.MustHave("#" + id)); got != want {
			t.Errorf("#%s = %q, want %q", id, got, want)
		}
	}
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name="rounds"]`), "value"); v != "lots" {
		t.Errorf("rounds echoed as %q, want what was typed", v)
	}
	doc.MustHave(`input[name="color"][value="purple"][checked]`)
	if ts, _ := s.Store.Timers(context.Background(), s.Alice.User.ID); len(ts) != 0 {
		t.Errorf("stored a timer from invalid input: %v", ts)
	}
}

func TestEditFormShowsTheTimer(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, validIntervals())
	doc := s.Get(t, s.Alice, "/focus/timers/"+itoa(tm.ID)+"/edit")
	doc.MustHave(`form.focus-form[action="/focus/timers/` + itoa(tm.ID) + `"]`)
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name="name"]`), "value"); v != "Deep work" {
		t.Errorf("name = %q", v)
	}
	doc.MustHave(`input[name="kind"][value="intervals"][checked]`)
	doc.MustHave(`input[name="color"][value="blue"][checked]`)
	doc.MustHave(`option[value="bowl"][selected]`)
}

func TestEditFormForASingleTimerOffersDefaultIntervals(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
	doc := s.Get(t, s.Alice, "/focus/timers/"+itoa(tm.ID)+"/edit")
	if v, _ := htmlassert.Attr(doc.MustHave(`input[name="break"]`), "value"); v != "5" {
		t.Errorf("break = %q, want the default 5", v)
	}
}

func TestUpdateTimer(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
	v := formValues("single")
	v.Set("name", "Evening reading")
	v.Set("focus", "45")
	s.Submit(t, s.Alice, "/focus/timers/"+itoa(tm.ID), v, "/focus/")
	got, err := s.Store.Timer(context.Background(), s.Alice.User.ID, tm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Evening reading" || got.FocusMinutes != 45 || got.Color != "purple" {
		t.Errorf("updated = %+v", got.TimerInput)
	}
}

func TestUpdateRejectsBadInput(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
	v := formValues("single")
	v.Set("focus", "0")
	rec := s.Post(t, s.Alice, "/focus/timers/"+itoa(tm.ID), v)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Focus length must be between 1 and 180 minutes.") {
		t.Error("missing the focus error message")
	}
}

func TestEditAndUpdateAreNotFoundForSomeoneElse(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Mine", 30))
	rec := s.Do(t, s.Bob, httptestGet("/focus/timers/"+itoa(tm.ID)+"/edit"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("bob edit form = %d, want 404", rec.Code)
	}
	if rec := s.Post(t, s.Bob, "/focus/timers/"+itoa(tm.ID), formValues("single")); rec.Code != http.StatusNotFound {
		t.Errorf("bob update = %d, want 404", rec.Code)
	}
}
```
And in `handlers_test.go` add:

```go
func httptestGet(path string) *http.Request { return httptest.NewRequest("GET", path, nil) }
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/focus/ -run 'Form|Create|Update|EditAnd' -count=1`
Expected: FAIL — 404s for the unregistered routes.

- [ ] **Step 3: Write `form.go`**

```go
package focus

import (
	"strconv"
	"strings"
)

// option is one choice in the colour picker or the chime select.
type option struct {
	Name  string // the stored value
	Label string // what people see (and screen readers hear)
}

var colorOptions = func() []option {
	out := make([]option, len(Colors))
	for i, c := range Colors {
		out[i] = option{Name: c, Label: strings.ToUpper(c[:1]) + c[1:]}
	}
	return out
}()

var chimeLabels = map[string]string{"bell": "Bell", "bowl": "Singing bowl", "soft": "Soft tone", "silent": "Silent"}

var chimeOptions = func() []option {
	out := make([]option, len(Chimes))
	for i, c := range Chimes {
		out[i] = option{Name: c, Label: chimeLabels[c]}
	}
	return out
}()

// formValues is what the form shows: the raw text, so a mistyped number
// comes back exactly as typed.
type formValues struct {
	Name, Color, Kind, Chime                         string
	Focus, Break, LongBreak, Rounds, LongBreakEvery string
	AutoAdvance, KeepHistory                         bool
}

func valuesOf(in TimerInput) formValues {
	return formValues{
		Name: in.Name, Color: in.Color, Kind: string(in.Kind), Chime: in.Chime,
		Focus: strconv.Itoa(in.FocusMinutes), Break: strconv.Itoa(in.BreakMinutes),
		LongBreak: strconv.Itoa(in.LongBreakMinutes), Rounds: strconv.Itoa(in.Rounds),
		LongBreakEvery: strconv.Itoa(in.LongBreakEvery),
		AutoAdvance: in.AutoAdvance, KeepHistory: in.KeepHistory,
	}
}

// formView is the New/Edit timer page.
type formView struct {
	Action  string // where the form posts
	Heading string
	Submit  string
	Values  formValues
	Errors  FieldErrors
	Colors  []option
	Chimes  []option
}

func newFormView(action, heading, submit string, v formValues, errs FieldErrors) formView {
	return formView{Action: action, Heading: heading, Submit: submit, Values: v, Errors: errs,
		Colors: colorOptions, Chimes: chimeOptions}
}

// editValues is a saved timer as the form shows it. A single timer's
// interval fields show the defaults, so switching to Intervals starts from
// sensible numbers rather than zeros.
func editValues(t Timer) formValues {
	in := t.TimerInput
	if in.Kind == KindSingle {
		d := DefaultInput()
		in.BreakMinutes, in.LongBreakMinutes, in.Rounds, in.LongBreakEvery =
			d.BreakMinutes, d.LongBreakMinutes, d.Rounds, d.LongBreakEvery
	}
	return valuesOf(in)
}

// parseForm reads a posted timer form. It returns the input, the raw
// values to echo back, and an error per number field that isn't a whole
// number. Interval fields are only read for interval timers: they're hidden
// otherwise, so whatever they hold doesn't matter.
func parseForm(get func(string) string) (TimerInput, formValues, FieldErrors) {
	v := formValues{
		Name: get("name"), Color: get("color"), Kind: get("kind"), Chime: get("chime"),
		Focus: get("focus"), Break: get("break"), LongBreak: get("long_break"),
		Rounds: get("rounds"), LongBreakEvery: get("long_break_every"),
		AutoAdvance: get("auto_advance") == "1", KeepHistory: get("keep_history") == "1",
	}
	in := TimerInput{
		Name: v.Name, Color: v.Color, Kind: Kind(v.Kind), Chime: v.Chime,
		AutoAdvance: v.AutoAdvance, KeepHistory: v.KeepHistory,
	}
	errs := FieldErrors{}
	number := func(field, raw string, dst *int) {
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			errs[field] = "Enter a whole number."
			return
		}
		*dst = n
	}
	number("focus", v.Focus, &in.FocusMinutes)
	if in.Kind == KindIntervals {
		number("break", v.Break, &in.BreakMinutes)
		number("long_break", v.LongBreak, &in.LongBreakMinutes)
		number("rounds", v.Rounds, &in.Rounds)
		number("long_break_every", v.LongBreakEvery, &in.LongBreakEvery)
	}
	return in, v, errs
}

// merge adds b's messages for fields a has nothing to say about: a
// "whole number" complaint beats the range check of the same field.
func merge(a, b FieldErrors) FieldErrors {
	for k, msg := range b {
		if _, ok := a[k]; !ok {
			a[k] = msg
		}
	}
	return a
}
```

- [ ] **Step 4: Add the handlers**

Append to `handlers.go` (add `"fmt"` to imports):

```go
func (a *App) newForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	view := newFormView("/focus/timers", "New timer", "Create timer", valuesOf(DefaultInput()), nil)
	a.render(w, r, http.StatusOK, "focus/form", "New timer", view)
}

func (a *App) editForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	t, err := a.store.Timer(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	view := newFormView(fmt.Sprintf("/focus/timers/%d", id), "Edit timer", "Save changes", editValues(t), nil)
	a.render(w, r, http.StatusOK, "focus/form", "Edit timer", view)
}

func (a *App) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	in, values, errs := parseForm(r.PostFormValue)
	if len(errs) == 0 {
		_, err := a.store.CreateTimer(r.Context(), userID, in)
		if err == nil {
			http.Redirect(w, r, "/focus/", http.StatusSeeOther)
			return
		}
		var ve *ValidationError
		if !errors.As(err, &ve) {
			a.fail(w, r, err)
			return
		}
		errs = ve.Fields
	} else {
		errs = merge(errs, in.Normalize().Validate())
	}
	view := newFormView("/focus/timers", "New timer", "Create timer", values, errs)
	a.render(w, r, http.StatusUnprocessableEntity, "focus/form", "New timer", view)
}

func (a *App) update(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	// Someone else's timer is a 404 even when the form is also invalid.
	if _, err := a.store.Timer(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	in, values, errs := parseForm(r.PostFormValue)
	if len(errs) == 0 {
		_, err := a.store.UpdateTimer(r.Context(), userID, id, in)
		if err == nil {
			http.Redirect(w, r, "/focus/", http.StatusSeeOther)
			return
		}
		var ve *ValidationError
		if !errors.As(err, &ve) {
			a.fail(w, r, err)
			return
		}
		errs = ve.Fields
	} else {
		errs = merge(errs, in.Normalize().Validate())
	}
	view := newFormView(fmt.Sprintf("/focus/timers/%d", id), "Edit timer", "Save changes", values, errs)
	a.render(w, r, http.StatusUnprocessableEntity, "focus/form", "Edit timer", view)
}
```

In `focus.go` `Mount`, add (before the duplicate/delete lines):

```go
	r.HandleFunc("GET /new", a.newForm)
	r.HandleFunc("POST /timers", a.create)
	r.HandleFunc("GET /timers/{id}/edit", a.editForm)
	r.HandleFunc("POST /timers/{id}", a.update)
```

- [ ] **Step 5: Add the shared field blocks**

Append to `templates/focus.partial.html`:

```html
{{/* focus-number is one whole-number field. Pass a dict with Name (the
     form field), Label, Min, Max, Value, Error and an optional Hint. */}}
{{define "focus-number"}}
<div class="field focus-number">
	<label for="focus-{{.Name}}">{{.Label}}</label>
	<input id="focus-{{.Name}}" name="{{.Name}}" type="number" inputmode="numeric" min="{{.Min}}" max="{{.Max}}" step="1" value="{{.Value}}"{{if .Error}} aria-invalid="true" aria-describedby="focus-{{.Name}}-error"{{end}}>
	{{with .Hint}}<p class="focus-hint">{{.}}</p>{{end}}
	{{with .Error}}<p class="focus-field-error" id="focus-{{$.Name}}-error">{{.}}</p>{{end}}
</div>
{{end}}

{{/* focus-error is a field's message, for the fields focus-number doesn't
     draw. Pass a dict with Name and Error. */}}
{{define "focus-error"}}{{with .Error}}<p class="focus-field-error" id="focus-{{$.Name}}-error">{{.}}</p>{{end}}{{end}}
```

- [ ] **Step 6: Write `templates/form.html`**

```html
{{define "content"}}
{{$d := .Data}}
<form class="focus-form stack" method="post" action="{{$d.Action}}" novalidate>
	<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
	<h1>{{$d.Heading}}</h1>
	{{if $d.Errors}}<div class="notice notice-error" role="alert">Please fix the fields marked below.</div>{{end}}

	<div class="field">
		<label for="focus-name">Name</label>
		<input id="focus-name" name="name" type="text" maxlength="80" placeholder="Deep work" value="{{$d.Values.Name}}"{{if index $d.Errors "name"}} aria-invalid="true" aria-describedby="focus-name-error"{{end}}>
		{{template "focus-error" (dict "Name" "name" "Error" (index $d.Errors "name"))}}
	</div>

	<fieldset class="field focus-swatch-field">
		<legend>Colour</legend>
		<div class="focus-swatches">
			{{range $d.Colors}}
			<input type="radio" class="visually-hidden focus-swatch-input" name="color" value="{{.Name}}" id="focus-color-{{.Name}}"{{if eq .Name $d.Values.Color}} checked{{end}}>
			<label class="focus-swatch swatch-c-{{.Name}}" for="focus-color-{{.Name}}"><span class="visually-hidden">{{.Label}}</span></label>
			{{end}}
		</div>
		{{template "focus-error" (dict "Name" "color" "Error" (index $d.Errors "color"))}}
	</fieldset>

	<fieldset class="field focus-kind">
		<legend>Kind</legend>
		<label class="focus-choice"><input type="radio" name="kind" value="single"{{if eq $d.Values.Kind "single"}} checked{{end}}> Single block</label>
		<label class="focus-choice"><input type="radio" name="kind" value="intervals"{{if eq $d.Values.Kind "intervals"}} checked{{end}}> Intervals — focus and breaks, in rounds</label>
		{{template "focus-error" (dict "Name" "kind" "Error" (index $d.Errors "kind"))}}
	</fieldset>

	{{template "focus-number" (dict "Name" "focus" "Label" "Focus (minutes)" "Min" 1 "Max" 180 "Value" $d.Values.Focus "Error" (index $d.Errors "focus"))}}

	{{/* Shown only while Intervals is picked (CSS :has(), no script). */}}
	<fieldset class="focus-intervals stack">
		<legend class="visually-hidden">Intervals</legend>
		<div class="focus-number-row">
			{{template "focus-number" (dict "Name" "break" "Label" "Break (minutes)" "Min" 1 "Max" 60 "Value" $d.Values.Break "Error" (index $d.Errors "break"))}}
			{{template "focus-number" (dict "Name" "long_break" "Label" "Long break (minutes)" "Min" 1 "Max" 60 "Value" $d.Values.LongBreak "Error" (index $d.Errors "long_break"))}}
		</div>
		<div class="focus-number-row">
			{{template "focus-number" (dict "Name" "rounds" "Label" "Rounds" "Min" 1 "Max" 12 "Value" $d.Values.Rounds "Error" (index $d.Errors "rounds"))}}
			{{template "focus-number" (dict "Name" "long_break_every" "Label" "Long break every" "Min" 1 "Max" 12 "Value" $d.Values.LongBreakEvery "Error" (index $d.Errors "long_break_every") "Hint" "rounds. Set it to the number of rounds for no long break.")}}
		</div>
		<label class="focus-choice"><input type="checkbox" name="auto_advance" value="1"{{if $d.Values.AutoAdvance}} checked{{end}}> Start next phase automatically</label>
	</fieldset>

	<div class="field">
		<label for="focus-chime">Chime when a phase ends</label>
		<select id="focus-chime" name="chime"{{if index $d.Errors "chime"}} aria-invalid="true" aria-describedby="focus-chime-error"{{end}}>
			{{range $d.Chimes}}<option value="{{.Name}}"{{if eq .Name $d.Values.Chime}} selected{{end}}>{{.Label}}</option>{{end}}
		</select>
		{{template "focus-error" (dict "Name" "chime" "Error" (index $d.Errors "chime"))}}
	</div>

	<label class="focus-choice"><input type="checkbox" name="keep_history" value="1"{{if $d.Values.KeepHistory}} checked{{end}}> Keep history — record sessions of this timer</label>

	<div class="focus-form-actions">
		<button class="primary" type="submit">{{$d.Submit}}</button>
		<a href="/focus/">Cancel</a>
	</div>
</form>
{{end}}
```
Note: `index` on a nil `FieldErrors` map returns `""`, which is what the New form needs.

- [ ] **Step 7: Add the form CSS**

Append to the ON Focus section of `internal/ui/static/app.css`:

```css
.focus-form { max-width: var(--measure); }
.focus-form fieldset { border: 0; padding: 0; margin: 0; }
.focus-form legend { font-weight: 600; margin-bottom: var(--s-2); }
.focus-choice { display: flex; align-items: center; gap: var(--s-2); font-weight: 400; }
.focus-number input { max-width: 8rem; }
.focus-number-row { display: flex; flex-wrap: wrap; gap: var(--s-4); }
.focus-hint { margin: var(--s-1) 0 0; color: var(--c-text-faint); font-size: var(--fs-sm); }
.focus-field-error { margin: var(--s-1) 0 0; color: var(--c-danger); font-size: var(--fs-sm); }
.focus-form-actions { display: flex; align-items: center; gap: var(--s-3); }
/* Interval fields only matter for interval timers; no script needed. */
.focus-form:has(input[name="kind"][value="single"]:checked) .focus-intervals { display: none; }

.focus-swatches { display: flex; flex-wrap: wrap; gap: var(--s-2); }
.focus-swatch {
	width: 2.75rem;
	height: 2.75rem;
	margin: 0;
	border-radius: 50%;
	background: var(--swatch);
	border: 3px solid var(--c-bg);
	box-shadow: 0 0 0 1px var(--c-border-firm);
	cursor: pointer;
}
.focus-swatch-input:checked + .focus-swatch { box-shadow: 0 0 0 3px var(--c-text); }
.focus-swatch-input:focus-visible + .focus-swatch { outline: var(--ring); outline-offset: 3px; }
```
(The swatch rules mirror Flash's `.flash-swatch`; apps don't borrow each other's classes.)

- [ ] **Step 8: Run the tests, then the full check**

Run: `go test ./internal/apps/focus/... -count=1`
Expected: PASS. Then the full check.

- [ ] **Step 9: Commit**

```bash
git add -A internal/apps/focus internal/ui/static/app.css
git commit -m "feat(focus): new and edit timer form (#493)"
```

---

### Task 9: Drag tiles to reorder

**Files:**
- Modify: `internal/apps/focus/handlers.go`, `internal/apps/focus/focus.go`, `internal/apps/focus/static/home.js`
- Test: `internal/apps/focus/handlers_test.go`

**Interfaces:**
- Consumes: `ReorderTimers` (Task 6); tile markup `ul[data-focus-tiles] > li.focus-tile[data-id]` (Task 7).
- Produces: `POST /focus/timers/order` with form field `ids` = comma-separated timer ids → 204; bad ids → 400.

- [ ] **Step 1: Write the failing tests**

Append to `handlers_test.go`:

```go
func TestReorderSavesTheNewOrder(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	a, b, c := seedTimer(t, s, uid, single("A", 5)), seedTimer(t, s, uid, single("B", 5)), seedTimer(t, s, uid, single("C", 5))
	ids := itoa(c.ID) + "," + itoa(a.ID) + "," + itoa(b.ID)
	rec := s.PostHX(t, s.Alice, "/focus/timers/order", url.Values{"ids": {ids}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST order = %d, want 204; body %s", rec.Code, rec.Body.String())
	}
	ts, _ := s.Store.Timers(context.Background(), uid)
	if got := strings.Join(names(ts), ""); got != "CAB" {
		t.Errorf("order = %s, want CAB", got)
	}
}

func TestReorderRejectsBadIDs(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	a, b := seedTimer(t, s, uid, single("A", 5)), seedTimer(t, s, uid, single("B", 5))
	theirs := seedTimer(t, s, s.Bob.User.ID, single("Bob", 5))
	for _, ids := range []string{"", "x,y", itoa(a.ID), itoa(a.ID) + "," + itoa(theirs.ID), itoa(a.ID) + "," + itoa(b.ID) + ",-1"} {
		rec := s.PostHX(t, s.Alice, "/focus/timers/order", url.Values{"ids": {ids}})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("ids %q = %d, want 400", ids, rec.Code)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/focus/ -run TestReorder -count=1`
Expected: FAIL — `POST order = 404` (the `{id}` route answers 404 for "order", or 405).

- [ ] **Step 3: Add the handler and route**

Append to `handlers.go`:

```go
// order saves the tile order after a drag. ids is the comma-separated list
// home.js sends; anything unparseable is a 400, like a list the store
// refuses.
func (a *App) order(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	var ids []int64
	for _, part := range strings.Split(r.PostFormValue("ids"), ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || id <= 0 {
			a.deps.Errors.Status(w, r, http.StatusBadRequest)
			return
		}
		ids = append(ids, id)
	}
	if err := a.store.ReorderTimers(r.Context(), userID, ids); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```
Add `"strings"` to the imports. In `focus.go` `Mount`, add **before** `POST /timers/{id}` (order doesn't matter to `ServeMux` — the literal `order` is more specific than `{id}` — but keep it next to its sibling for readers):

```go
	r.HandleFunc("POST /timers/order", a.order)
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/apps/focus/ -count=1`
Expected: PASS.

- [ ] **Step 5: Add drag-to-reorder to `home.js`**

Inside the IIFE in `internal/apps/focus/static/home.js`, after the submit listener, add:

```js
	// Drag a tile to reorder. The new order is saved in the background; if
	// that fails, a notice says so (htmx-notices.js, PATTERNS.md).
	var tiles = document.querySelector("[data-focus-tiles]");
	if (tiles && window.htmx) {
		var dragged = null;
		var before = "";

		function order() {
			return Array.prototype.map.call(tiles.querySelectorAll(".focus-tile"), function (t) {
				return t.dataset.id;
			}).join(",");
		}

		tiles.addEventListener("dragstart", function (e) {
			var tile = e.target.closest && e.target.closest(".focus-tile");
			if (!tile) return;
			dragged = tile;
			before = order();
			tile.classList.add("focus-tile-dragging");
			e.dataTransfer.effectAllowed = "move";
			e.dataTransfer.setData("text/plain", tile.dataset.id);
		});

		tiles.addEventListener("dragover", function (e) {
			if (!dragged) return;
			e.preventDefault();
			var over = e.target.closest && e.target.closest(".focus-tile");
			if (!over || over === dragged) return;
			var box = over.getBoundingClientRect();
			var after = e.clientX - box.left > box.width / 2;
			tiles.insertBefore(dragged, after ? over.nextSibling : over);
		});

		tiles.addEventListener("drop", function (e) {
			if (dragged) e.preventDefault();
		});

		tiles.addEventListener("dragend", function () {
			if (!dragged) return;
			dragged.classList.remove("focus-tile-dragging");
			dragged = null;
			var now = order();
			if (now === before) return;
			htmx.ajax("POST", "/focus/timers/order", {
				source: document.body,
				swap: "none",
				values: { ids: now }
			});
		});

		function isOrder(evt) {
			var info = evt.detail && evt.detail.pathInfo;
			return info && info.requestPath === "/focus/timers/order";
		}
		document.body.addEventListener("htmx:responseError", function (evt) {
			if (!isOrder(evt)) return;
			OnSuite.notices.show(tiles, "focus-order-error", "Couldn't save the new order. Reload the page and try again.");
		});
		document.body.addEventListener("htmx:sendError", function (evt) {
			if (!isOrder(evt)) return;
			OnSuite.notices.show(tiles, "focus-order-error", "Couldn't save the new order — you seem to be offline.");
		});
		document.body.addEventListener("htmx:afterRequest", function (evt) {
			if (isOrder(evt) && evt.detail.successful) OnSuite.notices.clear("focus-order-error");
		});
	}
```

- [ ] **Step 6: Verify the drag in the browser pane**

The main checkout's (git-ignored) `.claude/launch.json` already has an `onsuite` entry: it runs `/tmp/onsuite-bin serve` on port 8080 with `ONSUITE_DATA_DIR=/tmp/onsuite-manual-verify`. Build the worktree's binary to that path and make a test account there:

```bash
go build -o /tmp/onsuite-bin ./cmd/onsuite
/tmp/onsuite-bin user add focustest --admin --data-dir /tmp/onsuite-manual-verify
```
(If `focustest` already exists, reuse it.) Record the test password you chose in your notes for this session, not in chat. Start it with the Browser pane's `preview_start` `{name: "onsuite"}`. Sign in, create three timers, drag the third tile to the front, reload: the order sticks. Check `read_console_messages` shows no errors and `read_network_requests` shows `POST /focus/timers/order` → 204.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/focus
git commit -m "feat(focus): drag tiles to reorder (#493)"
```

---

### Task 10: Placeholder running page

**Files:**
- Create: `internal/apps/focus/templates/run.html`
- Modify: `internal/apps/focus/handlers.go`, `internal/apps/focus/focus.go`
- Test: `internal/apps/focus/handlers_test.go`

**Interfaces:**
- Consumes: `Timer`, `Phases`, `Phase.Label`, `FormatLength` (Tasks 4–5).
- Produces: `GET /focus/run/{id}` rendering page `focus/run` with `.focus-run` and one `li` per phase. F2 replaces this page.

- [ ] **Step 1: Write the failing tests**

Append to `handlers_test.go`:

```go
func TestRunPageListsThePhases(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, validIntervals()) // 50/10 × 4, long 30 every 2
	doc := s.Get(t, s.Alice, "/focus/run/"+itoa(tm.ID))
	if got := htmlassert.Text(doc.MustHave(".focus-run h1")); got != "Deep work" {
		t.Errorf("heading = %q", got)
	}
	var got []string
	for _, li := range doc.QueryAll(".focus-run-phases li") {
		got = append(got, htmlassert.Text(li))
	}
	want := []string{
		"Focus · round 1 of 4 — 50 min", "Short break — 10 min",
		"Focus · round 2 of 4 — 50 min", "Long break — 30 min",
		"Focus · round 3 of 4 — 50 min", "Short break — 10 min",
		"Focus · round 4 of 4 — 50 min",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("phases =\n%v\nwant\n%v", got, want)
	}
}

func TestRunPageForASingleTimer(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Daily Reflection", 15))
	doc := s.Get(t, s.Alice, "/focus/run/"+itoa(tm.ID))
	lis := doc.QueryAll(".focus-run-phases li")
	if len(lis) != 1 || htmlassert.Text(lis[0]) != "Focus — 15 min" {
		t.Errorf("phases = %d items", len(lis))
	}
}

func TestRunPageIsNotFoundForSomeoneElse(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Mine", 15))
	if rec := s.Do(t, s.Bob, httptestGet("/focus/run/"+itoa(tm.ID))); rec.Code != http.StatusNotFound {
		t.Errorf("bob run page = %d, want 404", rec.Code)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/focus/ -run TestRunPage -count=1`
Expected: FAIL — `GET /focus/run/1 = 404`.

- [ ] **Step 3: Add the handler, route and template**

Append to `handlers.go`:

```go
// phaseRow is one line of the placeholder running page.
type phaseRow struct {
	Label  string
	Length string
}

type runView struct {
	ID     int64
	Name   string
	Phases []phaseRow
}

// run is F1's placeholder running page: the timer's phases, in order. F2
// replaces it with the real focus-mode view.
func (a *App) run(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	t, err := a.store.Timer(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	view := runView{ID: t.ID, Name: t.Name}
	for _, p := range Phases(t.TimerInput) {
		view.Phases = append(view.Phases, phaseRow{Label: p.Label(t.Rounds), Length: FormatLength(p.Seconds)})
	}
	a.render(w, r, http.StatusOK, "focus/run", t.Name, view)
}
```
(`t.Rounds` is 0 for single timers, so their phase reads "Focus".)

In `focus.go` `Mount`: `r.HandleFunc("GET /run/{id}", a.run)`.

`internal/apps/focus/templates/run.html`:

```html
{{define "content"}}
<div class="focus-run stack">
	<h1>{{.Data.Name}}</h1>
	<p class="focus-hint">The full-screen running view is coming next. Here's how this timer runs:</p>
	<ol class="focus-run-phases">
		{{range .Data.Phases}}<li>{{.Label}} — {{.Length}}</li>{{end}}
	</ol>
	<p><a href="/focus/timers/{{.Data.ID}}/edit">Edit this timer</a> · <a href="/focus/">Back to timers</a></p>
</div>
{{end}}
```

- [ ] **Step 4: Run the tests, then the full check**

Run: `go test ./internal/apps/focus/... -count=1`
Expected: PASS. Then the full check.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/focus
git commit -m "feat(focus): placeholder running page listing a timer's phases (#493)"
```

---

### Task 11: Demo timers, user guide and docs

**Files:**
- Create: `docs/screenshots/seed/focus.go`
- Modify: `docs/screenshots/seed/seed.go` (steps list), `docs/screenshots/seed/seed_test.go`
- Modify: `docs/user/focus.md`, `docs/user/index.md`, `AGENTS.md`, `docs/developers/index.md:7`, `docs/developers/repository-layout.md:~18`
- Modify: `docs/superpowers/specs/2026-10-07-on-focus-design.md` (phase table)

**Interfaces:**
- Consumes: `focus.NewStore`, `(*Store).SetClock`, `CreateTimer`, `DefaultInput`, `KindIntervals` (Tasks 2–5).

- [ ] **Step 1: Write the failing seed assertion**

In `docs/screenshots/seed/seed_test.go`, add the import `"github.com/iliafrenkel/on-suite/internal/apps/focus"` and, after the Later block in `TestSeedFillsEveryApp`:

```go
	// Focus: a few timers of both kinds.
	timers, err := focus.NewStore(handle).Timers(ctx, demo.ID)
	if err != nil || len(timers) < 4 {
		t.Errorf("focus timers = %d, %v; want >= 4", len(timers), err)
	}
```

Run: `go test ./docs/screenshots/seed/ -count=1`
Expected: FAIL — `focus timers = 0`.

- [ ] **Step 2: Seed the demo timers**

`docs/screenshots/seed/focus.go`:

```go
package main

import (
	"context"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
)

// seedFocus gives the demo account the timers the spec's mockups show.
func seedFocus(ctx context.Context, st *focus.Store, userID int64, now time.Time) error {
	st.SetClock(func() time.Time { return now })
	deep := focus.TimerInput{
		Name: "Deep work", Color: "teal", Kind: focus.KindIntervals,
		FocusMinutes: 50, BreakMinutes: 10, LongBreakMinutes: 30, Rounds: 4, LongBreakEvery: 2,
		AutoAdvance: true, KeepHistory: true, Chime: "bowl",
	}
	pomodoro := focus.TimerInput{
		Name: "Pomodoro", Color: "amber", Kind: focus.KindIntervals,
		FocusMinutes: 25, BreakMinutes: 5, LongBreakMinutes: 15, Rounds: 4, LongBreakEvery: 4,
		AutoAdvance: true, KeepHistory: true, Chime: "bell",
	}
	reflection := focus.DefaultInput()
	reflection.Name, reflection.Color, reflection.FocusMinutes, reflection.Chime = "Daily Reflection", "coral", 15, "soft"
	reading := focus.DefaultInput()
	reading.Name, reading.Color, reading.FocusMinutes = "Reading", "purple", 30
	for _, in := range []focus.TimerInput{deep, reflection, reading, pomodoro} {
		if _, err := st.CreateTimer(ctx, userID, in); err != nil {
			return err
		}
	}
	return nil
}
```
In `seed.go`'s `steps`, add after the Flash step:

```go
		func() error { return seedFocus(ctx, focus.NewStore(handle), demo.ID, now) },
```

Run: `go test ./docs/screenshots/seed/ -count=1`
Expected: PASS.

- [ ] **Step 3: Write the F1 user guide**

Replace `docs/user/focus.md` with:

```markdown
# ON Focus

ON Focus is a calm focus timer. Set a timer up once — a 15-minute "Daily
Reflection", a 50-minute "Deep work" block with breaks — and start it
whenever you need it.

## Your timers

Your timers appear as coloured tiles on the ON Focus home page. Each tile
shows the timer's name, how it runs (for example **15 min**, or
**50 / 10 × 4 · long 30** for four 50-minute rounds with 10-minute breaks
and a 30-minute long break) and how long the whole thing takes.

Click **▶** on a tile to start that timer.

## Making a timer

1. Click **+ New timer**.
2. Give it a **Name** and pick a **Colour**.
3. Choose its **Kind**:
   - **Single block** — one stretch of focus time, like a 15-minute
     reflection.
   - **Intervals** — focus and breaks in rounds, Pomodoro-style. Set the
     **Break** and **Long break** lengths, how many **Rounds**, and how
     often the long break comes (**Long break every** so many rounds). Set
     **Long break every** to the number of rounds if you don't want a long
     break. Untick **Start next phase automatically** if you'd rather click
     to start each break and round yourself.
4. Pick the **Chime** that plays when a phase ends, or **Silent**.
5. Leave **Keep history** ticked to record your sessions with this timer.
   Untick it for quick everyday timers you don't want in your history.
6. Click **Create timer**.

## Changing your timers

Click a timer's name, or **⋯** then **Edit**, to change it. **⋯** also has:

- **Duplicate** — makes a copy right after it, handy for a variation.
- **Delete** — removes the timer. Sessions already in your history stay
  there.

To change the order of your timers, drag a tile to where you want it.
```

- [ ] **Step 4: Update the app lists**

`docs/user/index.md`:
- "You sign in once and move freely between five apps:" → "six apps:", and add after the Flash bullet:
  `- **[ON Focus](focus.md)** — focus timers you set up once and reuse.`
- In "Getting help", after the Flash line: `- [ON Focus](focus.md) — making and running focus timers.`
- In "What ON Suite is", the sentence "ON Suite is one place for your notes, snippets, news feeds and study cards." → "…news feeds, study cards and focus timers."

`AGENTS.md` (the "What this is" paragraph): change "**ON Flash** (flash cards with FSRS review, …) are all registered today." to add, before "are all registered today": `, and **ON Focus** (saved, reusable focus timers — single blocks or Pomodoro-style intervals; the running view and history are still being built out)`. Keep the sentence grammatical ("…stats), and **ON Focus** (…) are all registered today.").

`docs/developers/index.md:7`: "ON Notes, ON Reader, ON Later and ON Flash" → "ON Notes, ON Reader, ON Later, ON Flash and ON Focus".

`docs/developers/repository-layout.md`: add after the `later/` line, matching its column alignment:
`│   │   ├── focus/                ON Focus: focus timers — saved timers (single or intervals); running view and history to come`
and under it, following the `later/` block's shape, a `static/` line: `home.js`.

- [ ] **Step 5: Bring the spec's phase table in line with this plan**

In `docs/superpowers/specs/2026-10-07-on-focus-design.md`, the F1 row becomes:

`| F1 | #493 Saved timers | Swatch rename; app skeleton and registration; both migrations; `Phases`; home with tiles; create, edit, duplicate, delete, reorder (home.js); short user guide. ▶ opens a placeholder running page |`

and the F3 row's scope starts with "Recording endpoint with beacon and Retry; today strip; …" (already true — leave it). Also, in "Running page" under "Pages and routes", nothing changes.

- [ ] **Step 6: Full check**

Run the full check from Global Constraints.
Expected: gofmt prints nothing; vet, staticcheck clean; `go.mod`/`go.sum` unchanged; all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add -A docs AGENTS.md
git commit -m "docs(focus): demo timers, F1 user guide and app lists (#493)"
```

---

### Task 12: Verify in the browser and open the PR

- [ ] **Step 1: Walk through the feature in the browser pane**

With the local server from Task 9 (rebuild first: `go build -o /tmp/onsuite-bin ./cmd/onsuite`, then `preview_stop` and `preview_start` `{name: "onsuite"}` again):
1. `/focus/` with no timers shows the empty state and the sidebar shows ON Focus with its stopwatch icon.
2. Create a single timer and an intervals timer; switching Kind shows/hides the interval fields without a reload.
3. Submit with an empty name and `rounds = lots`: 422 with messages next to both fields and the typed values kept.
4. Tiles show the right colour, summary and total; ⋯ → Duplicate puts the copy right after; ⋯ → Delete asks in the app's dialog first.
5. Drag reorder sticks after reload.
6. ▶ shows the phase list.
7. Toggle dark mode (theme toggle): tiles, swatches, form and dialog stay readable.
8. Narrow the window to 375px (`resize_window` preset `mobile`): tiles wrap to one column, nothing scrolls sideways. Reset with preset `desktop`.
9. Open ON Flash: deck colours still show on the home list, review stripe and swatch picker (the rename).

Take one screenshot of the home page and one of the form for the PR. Check `read_console_messages` for errors on each page.

- [ ] **Step 2: Final full check**

Run the full check from Global Constraints. Expected: all green.

- [ ] **Step 3: Push and open the PR as Ilia**

```bash
env -u GIT_SSH_COMMAND git push -u origin feat/focus-f1-saved-timers
env -u GH_TOKEN gh pr create --title "feat(focus): ON Focus F1 — saved timers" --body "$(cat <<'EOF'
Closes #493. Part of #492.

ON Focus is now a registered app. You can create, edit, duplicate, delete and drag-to-reorder named timers — single blocks or Pomodoro-style intervals — shown as colour tiles. ▶ opens a placeholder page listing the timer's phases; the real running view is F2 (#494).

- Flash's `deck-c-*` palette is renamed to suite-wide `swatch-c-*` (no visual change)
- Schema for timers and (F3's) sessions
- Phase-list expansion in Go, tested; F2's runner will read it
- Short user guide; F4 completes it

Deviation from the spec: the today strip moves wholly to F3 (it needs F3's stats queries), and the home page gets a small `home.js` for the delete dialog and drag-to-reorder.
EOF
)"
```
Then bind the PR with the ccd_pr tools (`get_status`, `bind_pr` if needed). Never merge.

- [ ] **Step 4: Clean up**

Stop the preview server. Leave the worktree in place until the PR is merged.
