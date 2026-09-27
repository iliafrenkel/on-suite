# Adding an app

*For a developer writing a new ON app. Read [Architecture](architecture.md)
first; this page is the practical checklist.*

An app is one package under `internal/apps/` that implements `app.App`,
plus one registration line in `registeredApps()` and a few tests that pin
the registered-app list (see [step 5](#5-register-it)). The smallest real
app is ON Paste — keep
[`internal/apps/paste/paste.go`](../../internal/apps/paste/paste.go) open as
you read.

## The App interface

From [`internal/platform/app/app.go`](../../internal/platform/app/app.go):

```go
type App interface {
	Meta() Meta
	// Migrations holds this app's .sql files at the root of the returned FS.
	Migrations() fs.FS
	// Mount registers routes. Everything registered through Router.Handle
	// requires authentication; anonymous access is opt-in via Router.Public.
	Mount(r *Router, deps Deps)
}

type Meta struct {
	ID      string // URL prefix, migration namespace, table prefix
	Name    string // "ON " followed by one word
	Summary string // one short line for the dashboard
	Order   int    // position in the app switcher; ties break alphabetically
}
```

`Meta.Validate` runs at startup and refuses an app whose:

- `ID` isn't 2–16 lowercase letters or digits starting with a letter
  (`^[a-z][a-z0-9]{1,15}$`),
- `Name` doesn't look like `ON Paste` (`^ON [A-Z][A-Za-z]+$`), or
- `Summary` is empty.

`NewRegistry` also rejects a duplicate ID, and `Registry.Mount` refuses an
app that has no templates or mounts no routes.

The ID does three jobs at once: the app lives at `/<id>/`, its migrations are
recorded as `<id>:0001`, and every table it owns is named `<id>_…`. Choose it
carefully — changing it later means migrating data.

## A minimal app

Here is ON Hello, a complete app in three files. Registered in `main.go`, it
builds, serves `/hello/` and passes the test in [step 7](#7-tests).

`internal/apps/hello/hello.go`:

```go
// Package hello implements ON Hello, the smallest possible ON Suite app.
package hello

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

//go:embed templates/*.html
var templateFiles embed.FS

// App is ON Hello. main constructs it before the platform exists; everything
// it needs arrives in Mount.
type App struct {
	deps app.Deps
}

// New returns the app for registration in cmd/onsuite.
func New() *App { return &App{} }

func (a *App) Meta() app.Meta {
	return app.Meta{
		ID:      "hello",    // URL prefix, migration namespace, table prefix
		Name:    "ON Hello", // always "ON " + one capitalised word
		Summary: "Say hello to whoever is signed in.",
		Order:   50, // position in the app switcher
	}
}

func (a *App) Migrations() fs.FS { return sub(migrationFiles, "migrations") }

// Templates is optional on the interface but required in practice:
// Registry.Mount refuses to start an app without it.
func (a *App) Templates() fs.FS { return sub(templateFiles, "templates") }

func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	r.HandleFunc("GET /{$}", a.index) // served at /hello/, sign-in required
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	u, _ := web.UserFrom(r.Context()) // always set: Handle requires a user
	page := a.deps.Page(r, "Hello")
	page.Data = map[string]any{"Name": u.Username}
	if err := a.deps.Render.Page(w, http.StatusOK, "hello/index", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

func sub(fsys fs.FS, dir string) fs.FS {
	s, err := fs.Sub(fsys, dir)
	if err != nil {
		panic("hello: embedded " + dir + " missing: " + err.Error()) // unreachable
	}
	return s
}
```

`internal/apps/hello/migrations/0001_greetings.sql`:

```sql
-- Every table starts with the app id, so it cannot collide with another app's.
CREATE TABLE hello_greetings (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    message    TEXT    NOT NULL,
    created_at TEXT    NOT NULL -- db.FormatTime, never time.RFC3339Nano
) STRICT;
```

`internal/apps/hello/templates/index.html`:

```html
{{define "content"}}
<div class="stack">
	<h1>Hello, {{.Data.Name}}</h1>
</div>
{{end}}
```

## Step by step

### 1. Migrations

- Files sit at the root of the FS `Migrations()` returns, named
  `0001_some_name.sql`, `0002_…`. `db.Collect` rejects any other file name,
  a duplicate number or an empty file.
- Each file runs once, in a transaction, at startup. Never edit a migration
  that has shipped — add the next number instead. There are no down
  migrations.
- Prefix every table and index with the app id, and give every column that
  references `users (id)` an `ON DELETE CASCADE`.
  `TestEveryUserColumnCascadesFromUsers` in
  [`cmd/onsuite/schema_test.go`](../../cmd/onsuite/schema_test.go) fails
  otherwise.
- Store timestamps as TEXT written with `db.FormatTime`
  ([Storage](architecture.md#storage) explains why).

### 2. Templates and static files

- Embed `templates/*.html` and expose them through `Templates() fs.FS`. Each
  page file defines a `content` block, and optionally a `head` block for an
  extra stylesheet or script; `base.html` supplies the rest of the page.
- A page is rendered by name, `<id>/<file name without .html>` — `hello/index`
  above, `paste/index` for
  [`internal/apps/paste/templates/index.html`](../../internal/apps/paste/templates/index.html).
- Files named `*.partial.html` aren't pages: they hold `{{define}}` blocks
  that every page in the app can use.
- For HTMX swaps, define named blocks in the page file and render one with
  `deps.Render.Fragment(w, status, "<id>/<page>", "<block>", data)`.
- Forms need the CSRF field:
  `<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">`.
  HTMX requests get the token from `<body>` automatically.
- Styles go in `internal/ui/static/app.css`, in a new section, with classes
  prefixed by the app id (`hello-…`). Add the prefix to `appClassPattern` in
  `internal/ui/templates_test.go` so platform pages can't borrow your
  classes.
- If you need JavaScript, embed one file under `static/` and serve it from a
  route, as `script` in
  [`internal/apps/flash/flash.go`](../../internal/apps/flash/flash.go) does.
  No inline script — the CSP blocks it.

### 3. Routes

- Patterns are Go 1.22+ `ServeMux` patterns relative to the app:
  `"GET /{id}"` becomes `GET /hello/{id}`. Use `"GET /{$}"` for the app's
  landing page.
- `r.Handle` / `r.HandleFunc` require a signed-in user; the guard runs before
  your handler, so `web.UserFrom` always succeeds there.
- `r.Public` / `r.PublicFunc` make a route anonymous. Use them only for
  content meant to be shared — ON Paste's `GET /s/{slug}` behind an
  unguessable slug is the model. Every call is a deliberate, greppable
  decision.
- `ServeMux` panics at startup on ambiguous patterns. ON Paste's `Mount`
  explains the verb-first shape (`/edit/{id}`, not `/{id}/edit`) that avoids
  them.
- Request bodies are capped at 1 MiB (`web.DefaultMaxBodyBytes`). A route
  that takes uploads raises its own limit with `r.RegisterBodyLimit`.
- Scope every query to the signed-in user. For something that belongs to
  another user, answer 404 — not 403, which would confirm it exists.

### 4. A store, and the clock

Put SQL in a `Store` built from `deps.DB` in `Mount`. If the app reads the
time, the store owns the clock:

```go
func NewStore(handle *sql.DB) *Store {
	return &Store{db: handle, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the time source, for tests.
func (st *Store) SetClock(now func() time.Time) { st.now = now }
```

In `Mount`, call `a.store.SetClock(deps.Now)` when `deps.Now` is non-nil
(only tests set it). Everything else in the app reads time through the
store. `TestAppsReadTheirStoreClock` in
[`internal/arch/arch_test.go`](../../internal/arch/arch_test.go) lists the
exact files allowed to call `time.Now` — add your `store.go` to its `want`
list.

### 5. Register it

Add the import and one line to `registeredApps()` in
[`cmd/onsuite/main.go`](../../cmd/onsuite/main.go):

```go
func registeredApps() []app.App {
	return []app.App{
		flash.New(),
		hello.New(),
		notes.New(),
		paste.New(),
		reader.New(),
	}
}
```

Two tests in
[`cmd/onsuite/database_test.go`](../../cmd/onsuite/database_test.go) pin the
list of registered apps and will fail until you add yours:
`TestOpenDatabaseSkipsMigrationsForADisabledApp` and
`TestOpenDatabaseExportBuildsAConfigLiteralSoDisablingNeverApplies`.

Give the app an icon in `internal/ui/icons.go`, keyed by its ID. Without one
the sidebar and dashboard fall back to a generic tile.

### 6. Optional capabilities

Implement any of these on `*App` and the platform picks them up by type
assertion. Add a compile-time check, because a misspelt method otherwise
fails silently:

```go
var (
	_ app.Exporter  = (*App)(nil)
	_ app.Stater    = (*App)(nil)
	_ app.Scheduler = (*App)(nil)
)
```

| Interface | Method | Gives you |
| --- | --- | --- |
| `app.Exporter` | `Export(ctx, handle *sql.DB, userID int64) (any, error)` | a section, keyed by app ID, in `onsuite export <name>` |
| `app.Stater` | `Stats(ctx, handle *sql.DB) ([]app.Stat, error)` | a card on the admin dashboard |
| `app.Scheduler` | `Jobs(deps app.Deps) []app.Job` | background jobs, listed and runnable at `/admin/jobs` |

`Export` and `Stats` receive the database rather than using your `Mount`
state, so they work from the command line without an HTTP stack — build a
fresh store from `handle`, as ON Paste's
[`export.go`](../../internal/apps/paste/export.go) does. An export is a
portable file, so leave secrets such as share slugs out of it.

`Jobs` runs after `Mount`, so the closures can use the store you built
there. An `app.Job` has a `Name`, a one-line `Description`, an interval
(`Every`; zero registers it without scheduling it) and a
`Run func(context.Context) error`. See `Jobs` in
[`internal/apps/flash/flash.go`](../../internal/apps/flash/flash.go).

### 7. Tests

Write store tests against a real SQLite file and handler tests with
`internal/apptest` — [Testing](testing.md) shows both. A first handler test
for ON Hello:

```go
func TestIndexGreetsTheSignedInUser(t *testing.T) {
	s := apptest.NewServer(t, hello.New(), func(*sql.DB) struct{} { return struct{}{} })

	doc := s.Get(t, s.Alice, "/hello/")
	if got := htmlassert.Text(doc.MustHave("h1")); got != "Hello, alice" {
		t.Errorf("heading = %q, want %q", got, "Hello, alice")
	}
}
```

## The tests that will hold you to it

| Test | Fails when |
| --- | --- |
| `TestAppsDoNotImportEachOther` | your app imports another app |
| `TestPlatformDoesNotImportApps` | a platform package imports your app |
| `TestAppsReadTheirStoreClock` | app code calls `time.Now`/`Since`/`Until` outside the listed store files |
| `TestRFC3339IsContained` | you format a stored time with `time.RFC3339`/`RFC3339Nano` |
| `TestEveryUserColumnCascadesFromUsers` | a `user_id` column lacks `ON DELETE CASCADE` |
| `TestReadabilityIsContained`, `TestFSRSIsContained` | you import go-readability or go-fsrs outside their one allowed file |
| `Meta.Validate`, `Registry.Mount` (at startup) | bad ID or name, no templates, no routes |

**Next:** [Testing](testing.md)
