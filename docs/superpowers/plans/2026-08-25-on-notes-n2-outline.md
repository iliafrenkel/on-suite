# ON Notes N2 — Outline, no JS — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mount ON Notes at `/notes/` as a working outliner — zoom, breadcrumbs,
collapse, and every structural operation — with JavaScript disabled.

**Architecture:** The app package gains an `app.App` implementation, one
template, and nine routes. Reads go through N1's `Store` (`ByID`, `Ancestors`,
`Outline`); every write goes through one handler helper that opens a single
`Store.Do` transaction, saves the focused bullet's text, and performs the
structural operation — spec §7's "there is only ever one write". The flat
pre-order slice `Outline` returns is turned into a nested `<ul>` tree by a pure
function, because the CSP forbids the inline `style` attribute a depth-based
indent would otherwise need. Every mutation answers with a 303 back to the zoom
URL it came from.

**Tech Stack:** Go 1.25 stdlib only — `net/http` `ServeMux`, `html/template`,
`database/sql` over `modernc.org/sqlite`. No new dependencies. No JavaScript is
added by this chunk.

**Spec:** [docs/superpowers/specs/2026-08-25-on-notes-design.md](../specs/2026-08-25-on-notes-design.md),
chunk N2 of §18. Sections that bind this plan: §6 (loading and rendering), §7
(the client/server contract), §9 (routes), §16 (layout), §17 (testing), §19
(risks).

**Depends on:** N1, merged as [#44](https://github.com/iliafrenkel/on-suite/pull/44).
Every store method this plan calls already exists and is tested.

---

## Global Constraints

Every task's requirements implicitly include this section.

- **No new dependencies.** `go.mod` and `go.sum` must be byte-identical at the
  end of this chunk. ([CONTRIBUTING.md](../../../CONTRIBUTING.md))
- **No CGO, no Node, no build step.** This chunk adds no JavaScript at all.
- **Strict CSP:** `default-src 'self'; script-src 'self'; style-src 'self'`.
  There is no `unsafe-inline`, so **an inline `style=` attribute does not
  apply**. Indentation must come from real markup nesting, never from a style
  attribute or a `--depth` custom property set inline.
- **Apps never import each other; the platform never imports an app.** Enforced
  by [internal/arch/arch_test.go](../../../internal/arch/arch_test.go).
  `internal/apps/notes` may import `internal/platform/*`; nothing may import it
  except `cmd/onsuite`.
- **Routing is default-deny.** Every route in this chunk is registered with
  `Handle`/`HandleFunc`. **No route in N2 is `Public`** — the single public
  route in the whole app is the share page, and that arrives in N9.
- **`SetMaxOpenConns(1)`.** Nothing inside a `Store.Do` closure may touch the
  database except through the `*Ops` it is handed. A closure that calls
  `a.store.Outline(...)`, `a.store.Indent(...)` or any other `*Store` method
  waits for the connection its own transaction is holding, and waits for ever —
  the server has no write timeout, so that is a frozen process, not a failed
  request. Render **after** `Do` returns.
- **Ownership errors are 404, never 403.** `ErrNotFound` covers both "no such
  node" and "not yours", and the handler must not distinguish them.
- **Design tokens only.** New CSS composes the variables already in
  [internal/ui/static/app.css](../../../internal/ui/static/app.css). No new
  colours, no new spacing values.
- **Naming:** `app.Meta{ID: "notes", Name: "ON Notes", Summary: "Organise notes
  and tasks in one outline.", Order: 10}` — copied verbatim from spec §3a.
- **Every command below runs from the repository root.** The full check is
  `gofmt -l .` (must print nothing), `go vet ./...`,
  `go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...`, and
  `go test ./... -race -count=1`.
- **Use the pinned staticcheck, not the one on `$PATH`.** The `staticcheck`
  binary installed on this machine is older than the local Go toolchain: it
  prints `internal error in importing …` for half of the standard library and
  then **exits 0**, so a `&&` chain reads it as a pass while it has checked
  nothing. The `go run …@v0.8.0` form above is what CI runs
  ([.github/workflows/ci.yml](../../../.github/workflows/ci.yml)) and what
  [AGENTS.md](../../../AGENTS.md) documents.
- **staticcheck's U1000 covers test files.** An unexported function *or a
  method on an unexported type* that no test calls yet is a failure, not a
  warning. That is why each helper in this plan lands in the task that first
  uses it, and why Task 1 does not run staticcheck.
- **Commit after every task.** `main` is branch-protected: this chunk is one
  branch, `feat/notes-n2-outline`, and one PR. Never push to `main`.

---

## The request contract

Spec §7 is the single most important requirement in this chunk, so it is
written out here once, in full, as the shape every task must match.

Every structural POST carries the focused bullet's text, and the server applies
the text update and the structural operation **in one transaction**. Without
this, a user who types and then presses a structural button loses the
keystrokes that landed after the last save — in N3, after the last debounce.

| Field | Meaning |
|---|---|
| `csrf_token` | the platform's token, on every POST |
| `root` | the id of the zoom root the request was issued from; absent or `0` means the top level. It is the redirect target and nothing else. |
| `focus_id` | the bullet the caret was in. Absent means "no text to save". Present-but-not-a-positive-integer is a malformed request (400). |
| `title`, `note` | the focused bullet's text, saved to `focus_id` before the structural operation |

`focus_id` is deliberately independent of the `{id}` in the path. In N2 they
always coincide, because each row is its own form; in N3 they diverge the first
time someone clicks another row's chevron while the caret is in this one.

| Route | Method | Extra fields |
|---|---|---|
| `/notes/{$}` | GET | — |
| `/notes/{id}` | GET | — |
| `/notes/new` | POST | `new_title` — the text for the new bullet |
| `/notes/{id}/text` | POST | — |
| `/notes/{id}/indent` | POST | — |
| `/notes/{id}/outdent` | POST | — |
| `/notes/{id}/move` | POST | `dir` — exactly `up` or `down` |
| `/notes/{id}/collapse` | POST | `collapsed` — exactly `1` or `0` |
| `/notes/{id}/delete` | POST | — |

Two deliberate exceptions, both stated here so they are decisions rather than
inconsistencies:

- **`/notes/{id}/text` ignores `focus_id`.** Its subject is the path id; it is
  the one route whose target and focus cannot differ. The row form still sends
  the hidden `focus_id` field, because the same form's other buttons need it.
- **`/notes/{id}/delete` is a separate `<form>` in the template**, so it can
  carry `data-confirm` (a form-level attribute the platform's `theme.js`
  already handles) without confirming every other button in the row. It
  therefore sends no `focus_id`. The route still accepts one — N3 will send it.

`POST /notes/new` is Enter-with-a-split, ahead of the keyboard that will use it:
`title` is what stays on the focused bullet, `new_title` is what moves to the
new one. N2's `+` button is the degenerate case where `new_title` is empty.
Placement: **immediately after `focus_id` among its siblings**; with no
`focus_id`, as the **last child of `root`**.

---

## File Structure

**Created:**

| File | Responsibility |
|---|---|
| `internal/apps/notes/app.go` | The `App` type: `Meta`, `Migrations`, `Templates`, `Mount`, and the route map. Nothing else. |
| `internal/apps/notes/view.go` | The template's view model, and `nest` — the pure function turning `Outline`'s flat slice into a tree. No HTTP, no SQL. |
| `internal/apps/notes/handlers.go` | Every handler, the form parsing, and the one-transaction mutation seam. |
| `internal/apps/notes/templates/outline.html` | The only template. |
| `internal/apps/notes/view_test.go` | **`package notes`** (internal) — `nest` is unexported. See Task 2. |
| `internal/apps/notes/handlers_test.go` | `package notes_test` — the whole-stack harness and every route test. |

**Modified:**

| File | Change |
|---|---|
| `cmd/onsuite/main.go` | one line: `notes.New()` in `registeredApps()` |
| `cmd/onsuite/stack.go` | remove `notes` from `comingSoonApps` |
| `cmd/onsuite/stack_test.go` | the coming-soon assertion loses `"ON Notes"` |
| `internal/ui/static/app.css` | one new `/* ---- Outline ---- */` section |
| `README.md`, `AGENTS.md` | ON Notes is no longer future work |

**Not in this chunk**, named so they are decisions: no `notes.js` (N3), no
Markdown rendering (N4), no `app.Exporter` (N8), no `app.Stater` (N10), no
shared test-server package (see "Follow-ups" at the end).

---

## Task 1: The app skeleton and its registration

The walking skeleton: `/notes/` returns a real page inside the suite shell, the
app appears in the switcher, and the test harness exists. There are no bullets
yet.

**Files:**
- Create: `internal/apps/notes/app.go`
- Create: `internal/apps/notes/templates/outline.html`
- Create: `internal/apps/notes/handlers.go`
- Create: `internal/apps/notes/handlers_test.go`
- Modify: `cmd/onsuite/main.go`
- Modify: `cmd/onsuite/stack.go`
- Modify: `cmd/onsuite/stack_test.go`

**Interfaces:**
- Consumes: `app.App`, `app.Meta`, `app.Deps`, `app.Router` from
  `internal/platform/app`; `notes.ID`, `notes.Migrations()`, `notes.NewStore`
  from N1.
- Produces: `notes.New() *notes.App`, and `(*App).userID`, `(*App).fail`,
  `(*App).render` used by every later task.

- [ ] **Step 1: Write the failing test**

Create `internal/apps/notes/handlers_test.go`. This harness is a close relative
of `internal/apps/paste/handlers_test.go` — read that file first; the shape is
deliberate and duplicating it is deliberate too (see "Follow-ups").

```go
package notes_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

const testPassword = "a-sufficiently-long-password"

// server is the whole stack over a real database file, with two accounts.
// A real file, not ":memory:", for the reason given in N1's store tests: the
// bugs worth catching live in SQLite's own behaviour.
type server struct {
	handler http.Handler
	store   *notes.Store
	alice   *session
	bob     *session
}

// session holds one signed-in browser's cookies.
type session struct {
	user    auth.User
	cookies []*http.Cookie
}

func newServer(t *testing.T) *server {
	t.Helper()
	ctx := context.Background()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	registry, err := app.NewRegistry(notes.New())
	if err != nil {
		t.Fatal(err)
	}

	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	appMigrations, err := registry.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(migrations, appMigrations...)); err != nil {
		t.Fatal(err)
	}

	users := auth.NewStore(handle)
	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatal(err)
	}
	rend, err := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL})
	if err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.DiscardHandler)
	errs := web.NewErrors(rend, log)
	csrf := web.NewCSRF(false, errs)
	authn := web.NewAuth(web.AuthOptions{
		Users: users, Render: rend, Errors: errs, CSRF: csrf, Log: log, Secure: false,
	})

	mux := http.NewServeMux()
	authn.Routes(mux, nil)
	if err := registry.Mount(mux, app.Deps{
		DB: handle, Render: rend, Users: users, Errors: errs, Log: log,
	}, authn.RequireUser); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	mux.Handle("/", http.HandlerFunc(errs.NotFound))

	s := &server{
		handler: web.Stack(mux, log, errs, csrf, authn),
		store:   notes.NewStore(handle),
	}

	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alice", "bob"} {
		u, err := users.CreateUser(ctx, name, hash, false)
		if err != nil {
			t.Fatal(err)
		}
		sess := s.logIn(t, u)
		if name == "alice" {
			s.alice = sess
		} else {
			s.bob = sess
		}
	}
	return s
}

// do issues a request carrying a session's cookies.
func (s *server) do(t *testing.T, sess *session, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	if sess != nil {
		for _, c := range sess.cookies {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

// get fetches a page and fails the test unless it is a 200.
func (s *server) get(t *testing.T, sess *session, path string) *htmlassert.Doc {
	t.Helper()
	rec := s.do(t, sess, httptest.NewRequest("GET", path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200; body: %s", path, rec.Code, rec.Body.String())
	}
	return htmlassert.Parse(t, rec.Body.String())
}

// post submits a form with the session's CSRF token attached.
func (s *server) post(t *testing.T, sess *session, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set(web.CSRFFormField, s.csrfToken(t, sess))
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.do(t, sess, req)
}

// submit posts a structural request and asserts it redirected to wantLocation.
// Almost every write test in this file goes through it, because "did it come
// back to the outline I was looking at" is half of what each one checks.
func (s *server) submit(t *testing.T, sess *session, path string, form url.Values, wantLocation string) {
	t.Helper()
	rec := s.post(t, sess, path, form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d, want 303; body: %s", path, rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != wantLocation {
		t.Fatalf("POST %s redirected to %q, want %q", path, got, wantLocation)
	}
}

func (s *server) csrfToken(t *testing.T, sess *session) string {
	t.Helper()
	for _, c := range sess.cookies {
		if c.Name == web.CSRFCookieName {
			return c.Value
		}
	}
	t.Fatal("session has no CSRF cookie")
	return ""
}

// logIn performs a real login so the tests use genuine session cookies.
func (s *server) logIn(t *testing.T, u auth.User) *session {
	t.Helper()

	page := s.do(t, nil, httptest.NewRequest("GET", "/login", nil))
	var csrfCookie *http.Cookie
	for _, c := range page.Result().Cookies() {
		if c.Name == web.CSRFCookieName {
			csrfCookie = c
		}
	}
	if csrfCookie == nil {
		t.Fatal("GET /login issued no CSRF cookie")
	}

	form := url.Values{
		"username":        {u.Username},
		"password":        {testPassword},
		web.CSRFFormField: {csrfCookie.Value},
	}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := s.do(t, &session{cookies: []*http.Cookie{csrfCookie}}, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login for %s = %d", u.Username, rec.Code)
	}
	return &session{user: u, cookies: rec.Result().Cookies()}
}

// seed creates a bullet straight through the store, for tests about reading.
// Tests about writing go through the routes instead.
func (s *server) seed(t *testing.T, sess *session, parentID int64, title string) int64 {
	t.Helper()
	n, err := s.store.Create(context.Background(), sess.user.ID, parentID, 1<<30, title, "")
	if err != nil {
		t.Fatalf("seeding %q: %v", title, err)
	}
	return n.ID
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// ---- tests ----------------------------------------------------------------

// TestNotesRequiresSignIn confirms the default-deny router covers every route
// this chunk adds. A route accidentally registered with Public would show up
// here as a 200 instead of a redirect to the login page.
func TestNotesRequiresSignIn(t *testing.T) {
	s := newServer(t)

	for _, path := range []string{"/notes/", "/notes/1"} {
		rec := s.do(t, nil, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusSeeOther {
			t.Errorf("GET %s anonymous = %d, want a 303 to the login page", path, rec.Code)
		}
	}
}

func TestOutlineRendersInsideTheShell(t *testing.T) {
	s := newServer(t)
	doc := s.get(t, s.alice, "/notes/")

	doc.MustHave("#outline")

	// The router marks the active app; the shell reads it. If this fails the
	// app is serving pages but is not part of the suite.
	nav := doc.MustHave(`nav.shell-nav a[aria-current=page]`)
	if got := htmlassert.Text(nav); got != "ON Notes" {
		t.Errorf("the marked nav item is %q, want ON Notes", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/apps/notes/ -run 'TestNotes|TestOutline' -count=1`

Expected: a compile failure — `undefined: notes.New`.

- [ ] **Step 3: Create the app type**

Create `internal/apps/notes/app.go`:

```go
package notes

import (
	"embed"
	"io/fs"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// App is ON Notes. It is constructed before the platform exists, in the
// registration slice in main, and receives everything it needs in Mount.
//
// The compile-time assertion is here rather than left implicit because the
// registry takes an interface: a method with the wrong signature would
// otherwise fail at the call site in main, several packages away from the
// mistake.
var _ app.App = (*App)(nil)

//go:embed templates/*.html
var templateFiles embed.FS

type App struct {
	store *Store
	deps  app.Deps
}

// New returns the app for registration.
func New() *App { return &App{} }

// Meta is copied verbatim from the design's §3a. ID doubles as the URL
// prefix, the migration namespace and this app's table prefix, so it is the
// constant the rest of the package already uses.
func (a *App) Meta() app.Meta {
	return app.Meta{
		ID:      ID,
		Name:    "ON Notes",
		Summary: "Organise notes and tasks in one outline.",
		Order:   10,
	}
}

func (a *App) Migrations() fs.FS { return Migrations() }

func (a *App) Templates() fs.FS {
	sub, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		// Unreachable: a compile-time constant path checked by go:embed.
		panic("notes: embedded templates missing: " + err.Error())
	}
	return sub
}

// Mount wires the app up. Everything here goes through Handle, so every route
// requires a signed-in user. ON Notes has exactly one public route in the
// finished design — the share page — and it arrives in N9; until then a
// PublicFunc call in this file is a bug.
//
// Route map. ServeMux panics at startup on conflicting patterns, and several
// plausible-looking alternatives to these do conflict, so read this before
// changing one:
//
//	GET  /notes/{$}          the top-level outline; {$} matches only the
//	                         trailing-slash form, so it cannot collide with
//	                         the {id} pattern below
//	GET  /notes/{id}         the outline zoomed to one node. {id} never
//	                         matches an empty segment, which is what keeps it
//	                         and {$} disjoint
//	POST /notes/new          a literal segment, and literals outrank {id}
//	POST /notes/{id}/text    and the seven other mutations: two segments
//	                         deeper than the zoom URL, so no pattern in this
//	                         list is a prefix of another
func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	a.store = NewStore(deps.DB)

	r.HandleFunc("GET /{$}", a.outline)
}
```

- [ ] **Step 4: Create the handler and its helpers**

Create `internal/apps/notes/handlers.go`. Later tasks add to this file. Only
what Task 1 actually calls goes in now — an unexported function with no caller
is a `staticcheck` U1000 failure on an otherwise-green commit, so each helper
lands in the task that first uses it.

```go
package notes

import (
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// userID is the signed-in user's id. Every route is registered with Handle,
// so a missing user is a programming error rather than a bad request.
func (a *App) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u, ok := web.UserFrom(r.Context())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusUnauthorized)
		return 0, false
	}
	return u.ID, true
}

func (a *App) render(w http.ResponseWriter, r *http.Request, status int, name string, page render.Page) {
	if err := a.deps.Render.Page(w, status, name, page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// outline renders the top-level outline. Task 3 gives it something to draw.
func (a *App) outline(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	page := a.deps.Page(r, "")
	a.render(w, r, http.StatusOK, "notes/outline", page)
}
```

- [ ] **Step 5: Create the template**

Create `internal/apps/notes/templates/outline.html`. Task 3 fills the outline
in; for now it is the empty container the test asserts on.

```html
{{define "content"}}
<div class="stack notes">
	<h1>Notes</h1>
	<div id="outline" class="outline"></div>
</div>
{{end}}
```

- [ ] **Step 6: Register the app**

In `cmd/onsuite/main.go`, add the import and the one line:

```go
	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/apps/paste"
```

```go
func registeredApps() []app.App {
	return []app.App{
		notes.New(),
		paste.New(),
	}
}
```

In `cmd/onsuite/stack.go`, drop the `notes` entry from `comingSoonApps` — the
comment above it already says to do this the day the app is registered:

```go
var comingSoonApps = []struct {
	ID      string
	Name    string
	Summary string
}{
	{ID: "reader", Name: "ON Reader", Summary: "An RSS reader for the feeds you follow."},
	{ID: "flash", Name: "ON Flash", Summary: "Flash cards for spaced repetition."},
}
```

That breaks `TestHomePageShowsRealCardAndNamesComingSoonApps` in
`cmd/onsuite/stack_test.go`, which asserts the home page names all three. The
test is right and the list it checks is now stale, so update the list:

```go
	text := doc.Text()
	for _, name := range []string{"ON Reader", "ON Flash"} {
		if !strings.Contains(text, name) {
			t.Errorf("coming-soon app %q is not named anywhere on the page", name)
		}
	}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/ ./cmd/onsuite/ -count=1`

Expected: PASS for both packages.

- [ ] **Step 8: Run the check, without staticcheck**

Run: `gofmt -l . && go vet ./... && go test ./... -race -count=1`

Expected: `gofmt` prints nothing; every package passes. `internal/arch` passing
here is the real news: it proves the new app broke no import boundary.

**Do not add staticcheck to this step.** The harness above defines `post`,
`submit`, `seed`, `csrfToken` and `itoa`, and Task 1's two tests use none of
them — staticcheck reports U1000 for an unused function *or method* in a test
file just as it does in production code, so it would fail here and go on
failing until Task 6. The first full check including staticcheck is Task 6's
Step 7, by which point every helper has a caller.

- [ ] **Step 9: Commit**

```bash
git add internal/apps/notes cmd/onsuite && git commit -m "notes: mount the app with an empty outline"
```

---

## Task 2: `nest` and the row model

`Outline` returns a flat pre-order slice carrying `Depth`. The template needs
real `<ul>` nesting, because the CSP leaves no way to express indentation as a
per-row style. This task is the pure function between the two, and nothing
else: no HTTP, no SQL.

**Files:**
- Create: `internal/apps/notes/view.go`
- Create: `internal/apps/notes/view_test.go`

**Interfaces:**
- Consumes: `Node` (with `Depth` and `HasChildren` set), `MaxDepth`, `RootID`.
- Produces: `outlineView`, `outlineRow`, and
  `nest(flat []Node, root int64, csrfToken string) []*outlineRow`, used by
  Task 3's renderer and Task 3's template.

**A note on the test file's package.** `view_test.go` is `package notes`, not
`package notes_test` — the only internal test file in this app. `nest` is a
rendering detail; exporting it to reach it from outside would put a template
helper in the store's public API, and its most valuable case (a `Depth` that
skips a level) cannot be produced through the store at all. This is the
standard Go answer for a pure unexported helper, and it is called out here so
it reads as a decision rather than an inconsistency with the four
`notes_test` files beside it.

- [ ] **Step 1: Write the failing test**

Create `internal/apps/notes/view_test.go`:

```go
// This file is deliberately "package notes", unlike every other test file in
// this directory: nest is unexported, it is a rendering detail that does not
// belong in the store's public API, and its most interesting case cannot be
// produced through the store at all.
package notes

import (
	"strings"
	"testing"
)

// draw renders a nested row tree as indented text, so a test can state the
// shape it expects instead of walking pointers. A row that is last among its
// siblings is marked with a trailing "*".
func draw(rows []*outlineRow) string {
	var b strings.Builder
	var walk func([]*outlineRow, int)
	walk = func(rows []*outlineRow, depth int) {
		for _, r := range rows {
			b.WriteString(strings.Repeat("  ", depth))
			b.WriteString(r.Title)
			if r.Last {
				b.WriteString("*")
			}
			b.WriteString("\n")
			walk(r.Children, depth+1)
		}
	}
	walk(rows, 0)
	return b.String()
}

// flat builds the kind of slice Outline returns: pre-order, each row carrying
// its depth.
func flat(spec ...struct {
	depth int
	title string
}) []Node {
	out := make([]Node, 0, len(spec))
	for i, s := range spec {
		out = append(out, Node{ID: int64(i + 1), Title: s.title, Depth: s.depth})
	}
	return out
}

type lvl = struct {
	depth int
	title string
}

func TestNestBuildsTheTree(t *testing.T) {
	rows := nest(flat(
		lvl{0, "a"},
		lvl{1, "a1"},
		lvl{2, "a1x"},
		lvl{1, "a2"},
		lvl{0, "b"},
	), RootID, "tok")

	want := "a\n  a1\n    a1x*\n  a2*\nb*\n"
	if got := draw(rows); got != want {
		t.Errorf("nest built\n%s\nwant\n%s", got, want)
	}
}

func TestNestMarksTheLastSiblingAtEveryLevel(t *testing.T) {
	rows := nest(flat(
		lvl{0, "a"},
		lvl{1, "a1"},
		lvl{1, "a2"},
	), RootID, "tok")

	if rows[0].Last != true {
		t.Error("the only top-level row is not marked last")
	}
	if rows[0].Children[0].Last {
		t.Error("a1 is marked last but a2 follows it")
	}
	if !rows[0].Children[1].Last {
		t.Error("a2 is not marked last")
	}
}

// TestNestStampsEveryRow: each row is exactly the inputs of one form, so it
// carries the hidden fields that form needs rather than reaching for a shared
// parent the template has no way to address from inside a recursive block.
func TestNestStampsEveryRow(t *testing.T) {
	rows := nest(flat(lvl{0, "a"}, lvl{1, "a1"}), 42, "tok")

	var seen int
	var walk func([]*outlineRow)
	walk = func(rows []*outlineRow) {
		for _, r := range rows {
			seen++
			if r.RootID != 42 {
				t.Errorf("%q carries root %d, want 42", r.Title, r.RootID)
			}
			if r.CSRFToken != "tok" {
				t.Errorf("%q carries token %q, want tok", r.Title, r.CSRFToken)
			}
			walk(r.Children)
		}
	}
	walk(rows)
	if seen != 2 {
		t.Errorf("walked %d rows, want 2", seen)
	}
}

// TestNestDropsARowWithNoParent is the case the store cannot produce and the
// renderer must survive: a depth that skips a level has no correct parent, and
// guessing one would put a bullet somewhere the user never left it.
func TestNestDropsARowWithNoParent(t *testing.T) {
	rows := nest(flat(
		lvl{0, "a"},
		lvl{2, "orphan"},
		lvl{3, "orphan's child"},
		lvl{1, "a1"},
	), RootID, "tok")

	want := "a\n  a1*\n"
	if got := draw(rows); got != want {
		t.Errorf("nest built\n%s\nwant\n%s", got, want)
	}
}

func TestNestOfNothingIsNothing(t *testing.T) {
	if rows := nest(nil, RootID, "tok"); len(rows) != 0 {
		t.Errorf("nest(nil) returned %d rows", len(rows))
	}
}

// TestNestHandlesTheDeepestPermittedOutline: MaxDepth is the cap Outline
// enforces, so nest has to reach it without reallocating its way into a wrong
// answer.
func TestNestHandlesTheDeepestPermittedOutline(t *testing.T) {
	spec := make([]lvl, 0, MaxDepth+1)
	for d := 0; d <= MaxDepth; d++ {
		spec = append(spec, lvl{d, "d" + string(rune('0'+d%10))})
	}
	rows := nest(flat(spec...), RootID, "tok")

	depth := 0
	for cur := rows; len(cur) > 0; cur = cur[0].Children {
		depth++
	}
	if depth != MaxDepth+1 {
		t.Errorf("the tree is %d deep, want %d", depth, MaxDepth+1)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/apps/notes/ -run TestNest -count=1`

Expected: a compile failure — `undefined: nest`, `undefined: outlineRow`.

- [ ] **Step 3: Write the implementation**

Create `internal/apps/notes/view.go`:

```go
package notes

// outlineView is what the outline template renders.
type outlineView struct {
	// Root is the node the outline is zoomed to. At the top level it is the
	// zero Node, whose ID is RootID — so the template can write
	// .Root.ID into a hidden field without a special case.
	Root   Node
	Zoomed bool
	// Crumbs are Root's ancestors, outermost first. Empty unless Zoomed.
	Crumbs []Node
	// Rows is the visible outline, nested. Empty means the outline shows one
	// empty bullet instead — spec §6.
	Rows      []*outlineRow
	CSRFToken string
}

// outlineRow is one bullet, and exactly the inputs of the one form that edits
// it. Root and CSRFToken are stamped onto every row rather than read from a
// shared parent because the template renders rows through a block that calls
// itself, and a recursive block can only be handed one argument: the slice of
// children. Denormalising two fields is the cheaper half of that trade.
type outlineRow struct {
	// Node carries ID, Title, Note, Position, Depth, Collapsed and
	// HasChildren, all set by Store.Outline.
	Node
	// Last is true for the final row of a sibling list, which is what decides
	// whether "move down" renders disabled.
	Last bool
	// RootID is the zoom root this outline was loaded at, for the hidden
	// field that sends the mutation back to the right page. It is an id, not
	// a Node, which is the whole difference from outlineView.Root — hence the
	// different name.
	RootID    int64
	CSRFToken string
	Children  []*outlineRow
}

// nest turns Outline's flat pre-order slice into the tree the template renders
// as nested <ul>s.
//
// The nesting is not decoration. The CSP has no unsafe-inline, so a row cannot
// carry its indentation in a style attribute and cannot set a --depth custom
// property inline either; real markup nesting is what lets the stylesheet
// alone produce the indent and the vertical guide lines.
//
// It relies on exactly the ordering Outline guarantees: a parent immediately
// precedes its subtree, and depth rises by at most one from a row to the next.
// A row that breaks that has no correct parent — and inventing one would put a
// bullet somewhere the user never left it — so it is dropped, along with
// everything that would have hung beneath it.
func nest(flat []Node, root int64, csrfToken string) []*outlineRow {
	var top []*outlineRow

	// open is the ancestor chain of the row most recently added: open[d] is
	// that chain's row at depth d, so a row at depth d attaches to open[d-1].
	// It holds pointers, not indices into a slice that append may move.
	open := make([]*outlineRow, 0, MaxDepth+1)

	for _, n := range flat {
		row := &outlineRow{Node: n, RootID: root, CSRFToken: csrfToken}

		switch d := n.Depth; {
		case d == 0:
			top = append(top, row)
			open = open[:0]
		case d <= len(open):
			parent := open[d-1]
			parent.Children = append(parent.Children, row)
			open = open[:d]
		default:
			// A depth that skips a level. Leaving open untouched is what makes
			// this row's own descendants fall into this branch too.
			continue
		}
		open = append(open, row)
	}

	markLast(top)
	return top
}

// markLast flags the final row of every sibling list.
func markLast(rows []*outlineRow) {
	for i, r := range rows {
		r.Last = i == len(rows)-1
		markLast(r.Children)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/ -run TestNest -count=1 -v`

Expected: PASS, six tests.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/notes && git commit -m "notes: nest the flat outline into a row tree"
```

---

## Task 3: Rendering the outline

`GET /notes/` shows the user's real tree, nested, with its collapse state and
its per-row controls. Nothing is writable yet — the buttons exist and the
routes behind them arrive in Tasks 5–9 — so this task's tests seed through the
store and assert on markup.

**Files:**
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/templates/outline.html`
- Modify: `internal/ui/static/app.css`
- Modify: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `nest`, `outlineView`, `outlineRow` from Task 2; `Store.Outline`.
- Produces: `(*App).renderOutline(w, r, rootID int64)`, used by Task 4.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/handlers_test.go`:

```go
func TestOutlineRendersTheTreeNested(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.alice, notes.RootID, "Projects")
	s.seed(t, s.alice, parent, "AtBudget")
	s.seed(t, s.alice, notes.RootID, "Reading")

	doc := s.get(t, s.alice, "/notes/")

	// Two top-level bullets, and one of them has a nested list under it.
	// htmlassert has descendant selectors but no child combinator, so
	// "top level" is expressed as "every bullet, less the nested ones".
	all := doc.QueryAll(".outline-item")
	nested := doc.QueryAll(".outline-item .outline-item")
	if len(all)-len(nested) != 2 {
		t.Errorf("got %d top-level bullets, want 2", len(all)-len(nested))
	}
	if len(nested) != 1 {
		t.Fatalf("got %d nested bullets, want 1", len(nested))
	}

	// Every bullet's text is an input, so the page is already editable.
	titles := doc.QueryAll("input.outline-title")
	if len(titles) != 3 {
		t.Fatalf("got %d title inputs, want 3", len(titles))
	}
	var got []string
	for _, in := range titles {
		v, _ := htmlassert.Attr(in, "value")
		got = append(got, v)
	}
	want := []string{"Projects", "AtBudget", "Reading"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("bullet %d is %q, want %q (pre-order: %v)", i, got[i], want[i], got)
		}
	}
}

func TestOutlineRendersAnotherUsersTreeNowhere(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.bob, notes.RootID, "bob's secret")

	doc := s.get(t, s.alice, "/notes/")
	if strings.Contains(doc.Text(), "bob's secret") {
		t.Error("another user's bullet is on the page")
	}
	if n := len(doc.QueryAll("input.outline-title")); n != 0 {
		t.Errorf("alice's empty outline rendered %d bullets", n)
	}
}

// TestCollapsedBulletHidesItsChildren is spec §6: the payload is bounded by
// collapse state, so a collapsed subtree is not merely hidden by CSS — it is
// not in the response at all.
func TestCollapsedBulletHidesItsChildren(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.alice, notes.RootID, "Projects")
	s.seed(t, s.alice, parent, "AtBudget")

	if err := s.store.SetCollapsed(context.Background(), s.alice.user.ID, parent, true); err != nil {
		t.Fatal(err)
	}

	doc := s.get(t, s.alice, "/notes/")
	if strings.Contains(doc.Text(), "AtBudget") {
		t.Error("a collapsed bullet's child is in the response")
	}
	if _, ok := htmlassert.Attr(doc.MustHave(".outline-chevron"), "aria-expanded"); !ok {
		t.Error("a collapsed bullet renders no expand control")
	}
}

// TestBulletControlsAreDisabledWhereTheOperationIsANoOp. The store treats all
// four as no-ops rather than errors, so this is honesty rather than
// enforcement: a button that cannot do anything should not look like it can.
func TestBulletControlsAreDisabledAtTheEdges(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "first")
	s.seed(t, s.alice, notes.RootID, "second")

	doc := s.get(t, s.alice, "/notes/")

	// Two flat bullets, so document order is outline order: [0] is first,
	// [1] is second.
	ups := doc.QueryAll(`button[value=up]`)
	downs := doc.QueryAll(`button[value=down]`)
	if len(ups) != 2 || len(downs) != 2 {
		t.Fatalf("got %d move-up and %d move-down buttons, want 2 of each", len(ups), len(downs))
	}
	if _, ok := htmlassert.Attr(ups[0], "disabled"); !ok {
		t.Error("the first bullet's move-up is not disabled")
	}
	if _, ok := htmlassert.Attr(downs[0], "disabled"); ok {
		t.Error("the first bullet's move-down is disabled but a sibling follows it")
	}
	if _, ok := htmlassert.Attr(ups[1], "disabled"); ok {
		t.Error("the second bullet's move-up is disabled but a sibling precedes it")
	}
	if _, ok := htmlassert.Attr(downs[1], "disabled"); !ok {
		t.Error("the last bullet's move-down is not disabled")
	}
}

// TestOutlineUsesNoInlineStyles. The CSP has no unsafe-inline: a style
// attribute would simply not apply, so indentation must come from nesting.
func TestOutlineUsesNoInlineStyles(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.alice, notes.RootID, "Projects")
	s.seed(t, s.alice, parent, "AtBudget")

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/notes/", nil))
	if strings.Contains(rec.Body.String(), "style=") {
		t.Error("the outline contains an inline style attribute, which the CSP blocks")
	}
}

func TestOutlineEscapesBulletText(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, `<script>alert(1)</script>`)

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/notes/", nil))
	if strings.Contains(rec.Body.String(), "<script>alert(1)</script>") {
		t.Error("bullet text reached the page unescaped")
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	v, _ := htmlassert.Attr(doc.MustHave("input.outline-title"), "value")
	if v != `<script>alert(1)</script>` {
		t.Errorf("the round-tripped title is %q", v)
	}
}
```

Two things about `htmlassert` that shape every selector in this plan, because
getting them wrong produces a test that silently matches nothing:

- It supports **descendant** combinators (`.a .b`) but **not** the child
  combinator `>`. A `>` in a selector is parsed as a tag name called `>` and
  matches nothing at all.
- A selector part takes **one** qualifier. `button[name=dir][value=up]` does
  not work; `button[value=up]` does.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/ -run TestOutline -count=1`

Expected: FAIL — `htmlassert: no element matches "input.outline-title"`,
because the template still renders an empty container.

- [ ] **Step 3: Write the renderer**

In `internal/apps/notes/handlers.go`, add `"errors"` to the imports, add the
error mapper, and replace the placeholder `outline` handler:

```go
// fail maps a store error onto a response.
//
// ErrNotFound is a 404 whether the bullet is missing or simply someone
// else's: a 403 would confirm that it exists. ErrCycle and ErrTooDeep are
// requests that are well-formed but not satisfiable against this tree, which
// is what 400 means here.
func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		a.deps.Errors.Status(w, r, http.StatusNotFound)
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrCycle), errors.Is(err, ErrTooDeep):
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
	default:
		a.deps.Errors.Internal(w, r, err)
	}
}

// outline renders the top-level outline.
func (a *App) outline(w http.ResponseWriter, r *http.Request) {
	a.renderOutline(w, r, RootID)
}

// renderOutline draws the outline rooted at rootID: the breadcrumb, the
// visible rows, and nothing else. RootID means the top level.
//
// Every query here runs on the pool, outside any transaction, which is the
// only safe place for them — see the warning on mutate.
func (a *App) renderOutline(w http.ResponseWriter, r *http.Request, rootID int64) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	view := outlineView{CSRFToken: web.CSRFToken(r.Context())}

	// An empty title leaves the shell's breadcrumb reading "Home / ON Notes",
	// which is what the top level is. A zoomed outline names its root.
	title := ""
	if rootID != RootID {
		root, err := a.store.ByID(r.Context(), userID, rootID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		crumbs, err := a.store.Ancestors(r.Context(), userID, rootID)
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		view.Root, view.Zoomed, view.Crumbs = root, true, crumbs
		title = root.DisplayTitle()
	}

	flat, err := a.store.Outline(r.Context(), userID, rootID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view.Rows = nest(flat, rootID, view.CSRFToken)

	page := a.deps.Page(r, title)
	page.Data = view
	a.render(w, r, http.StatusOK, "notes/outline", page)
}
```

- [ ] **Step 4: Write the template**

Replace `internal/apps/notes/templates/outline.html` entirely. The breadcrumb
and the empty-outline bullet arrive in Tasks 4 and 6; this is the rows.

```html
{{define "content"}}
<div class="stack notes">
	<h1>Notes</h1>

	{{/* #outline is the swap target N3 will replace on every structural
	     response, which is why the rows live in a block of their own. */}}
	<div id="outline" class="outline">
		{{with .Data.Rows}}{{template "outline-rows" .}}{{end}}
	</div>
</div>
{{end}}

{{/* outline-rows renders one sibling list and calls itself for each row's
     children. Its argument is the slice, and nothing else — which is why each
     row carries its own Root and CSRFToken. */}}
{{define "outline-rows"}}
<ul class="outline-list">
	{{range .}}
	<li class="outline-item">
		<div class="outline-row">
			<form method="post" action="/notes/{{.ID}}/text" class="outline-main">
				<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
				<input type="hidden" name="root" value="{{.RootID}}">
				<input type="hidden" name="focus_id" value="{{.ID}}">

				{{if .HasChildren}}
				<button type="submit" class="outline-chevron quiet"
				        formaction="/notes/{{.ID}}/collapse"
				        name="collapsed" value="{{if .Collapsed}}0{{else}}1{{end}}"
				        aria-expanded="{{if .Collapsed}}false{{else}}true{{end}}"
				        aria-label="{{if .Collapsed}}Expand{{else}}Collapse{{end}}">{{if .Collapsed}}&#9656;{{else}}&#9662;{{end}}</button>
				{{else}}
				<span class="outline-chevron" aria-hidden="true"></span>
				{{end}}

				<a class="outline-dot{{if .Collapsed}} outline-dot-full{{end}}"
				   href="/notes/{{.ID}}"
				   aria-label="Zoom in to {{.DisplayTitle}}">&bull;</a>

				<span class="outline-text">
					<input class="outline-title" type="text" name="title"
					       value="{{.Title}}" maxlength="2000" aria-label="Bullet">
					<input class="outline-note" type="text" name="note"
					       value="{{.Note}}" maxlength="10000"
					       placeholder="note" aria-label="Note">
				</span>

				<span class="outline-actions">
					<button type="submit" class="quiet" formaction="/notes/{{.ID}}/move"
					        name="dir" value="up" aria-label="Move up"
					        {{if eq .Position 0}}disabled{{end}}>&uarr;</button>
					<button type="submit" class="quiet" formaction="/notes/{{.ID}}/move"
					        name="dir" value="down" aria-label="Move down"
					        {{if .Last}}disabled{{end}}>&darr;</button>
					<button type="submit" class="quiet" formaction="/notes/{{.ID}}/indent"
					        aria-label="Indent"
					        {{if eq .Position 0}}disabled{{end}}>&#8677;</button>
					<button type="submit" class="quiet" formaction="/notes/{{.ID}}/outdent"
					        aria-label="Outdent"
					        {{if eq .Depth 0}}disabled{{end}}>&#8676;</button>
					<button type="submit" class="quiet" formaction="/notes/new"
					        aria-label="Add a bullet below">+</button>
				</span>
			</form>
		</div>

		{{with .Children}}{{template "outline-rows" .}}{{end}}
	</li>
	{{end}}
</ul>
{{end}}
```

Three things in there are load-bearing and easy to undo by accident:

1. **`maxlength` on both inputs** matches `MaxTitleRunes` and `MaxNoteRunes`.
   It is what keeps the validation 400 unreachable from a real browser, which
   is why the handlers can answer it with the platform's error page instead of
   re-rendering the form with the user's text.
2. **`formaction` on each submit button** is how one form serves eight
   destinations. The clicked button's own `name`/`value` is the only one
   submitted, which is why `dir` and `collapsed` can live on buttons while
   `focus_id` has to be a hidden input.
3. **`outdent` is disabled at `Depth 0`.** A direct child of the zoom root has
   nowhere to go that stays on screen. The store would happily do it; this is
   the UI declining to offer a move whose result is invisible.

- [ ] **Step 5: Add the stylesheet section**

Append to `internal/ui/static/app.css`, **before** the `/* ---- Narrow
viewports ---- */` media query at the end of the file — that block is the last
thing in the file by design:

```css
/* ---- Outline (ON Notes) ------------------------------------------------- */

/* Spec §16: a single centred column. The suite shell already carries the
 * navigation, so the outline gets no second sidebar. */
.notes { max-width: 46rem; }

.outline-list { margin: 0; padding: 0; list-style: none; }

/* The nesting is the indentation. The CSP has no unsafe-inline, so a row
 * cannot carry its depth in a style attribute — see nest() in view.go — and
 * a nested list is what produces both the indent and the guide line. */
.outline-item .outline-list {
	margin-left: var(--s-4);
	padding-left: var(--s-3);
	border-left: 1px solid var(--c-border);
}

.outline-row {
	display: flex;
	align-items: flex-start;
	gap: var(--s-1);
	border-radius: var(--radius);
}

.outline-row:hover { background: var(--c-bg-subtle); }

.outline-main {
	display: flex;
	align-items: flex-start;
	gap: var(--s-1);
	flex: 1;
	min-width: 0;
}

.outline-chevron,
.outline-dot {
	flex: none;
	width: 1.1rem;
	line-height: 1.7;
	text-align: center;
	color: var(--c-text-faint);
}

button.outline-chevron {
	padding: 0;
	border: none;
	background: none;
}

.outline-dot { text-decoration: none; }
.outline-dot:hover { color: var(--c-accent); }

/* A collapsed bullet keeps its subtree out of the response entirely, so the
 * dot gets a halo: the bullet is not as empty as it looks. */
.outline-dot-full {
	background: var(--c-bg-inset);
	border-radius: 50%;
}

.outline-text { flex: 1; min-width: 0; }

/* The two inputs are the bullet — there is no separate display mode in this
 * chunk — so they are styled as text, not as fields. */
.outline-title,
.outline-note {
	width: 100%;
	padding: 0;
	border: none;
	border-radius: 0;
	background: none;
	font: inherit;
}

.outline-title { color: var(--c-text); line-height: 1.7; }
.outline-note { font-size: var(--fs-sm); color: var(--c-text-dim); }

/* The underline moves to the whole text column so the title and its note read
 * as one bullet while either is being edited. */
.outline-title:focus,
.outline-note:focus { outline: none; }
.outline-text:focus-within { box-shadow: inset 0 -1px 0 var(--c-accent); }

/* opacity, not visibility: a visibility:hidden control cannot take focus, so
 * :focus-within would never fire and these would be unreachable from the
 * keyboard. */
.outline-chevron,
.outline-actions { opacity: 0; }

.outline-row:hover .outline-chevron,
.outline-row:hover .outline-actions,
.outline-row:focus-within .outline-chevron,
.outline-row:focus-within .outline-actions { opacity: 1; }

.outline-actions { display: flex; flex: none; }

.outline-actions button {
	padding: 0 var(--s-1);
	border: none;
	background: none;
	color: var(--c-text-faint);
	line-height: 1.7;
}

.outline-actions button:hover:not([disabled]) {
	background: var(--c-bg-inset);
	color: var(--c-text);
}

.outline-actions button[disabled] { opacity: 0.3; cursor: default; }
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/ -count=1`

Expected: PASS.

- [ ] **Step 7: Confirm no dependency crept in**

Run: `git diff --stat go.mod go.sum`

Expected: no output. `golang.org/x/net` is already required.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/notes internal/ui && git commit -m "notes: render the outline as a nested list"
```

---

## Task 4: Zoom and breadcrumbs

Spec §6: zoom is the URL. `/notes/{id}` shows that node's children as the
outline, with its ancestors above it.

**Files:**
- Modify: `internal/apps/notes/app.go`
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/templates/outline.html`
- Modify: `internal/ui/static/app.css`
- Modify: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `renderOutline` from Task 3; `Store.Ancestors`.
- Produces: `(*App).nodeID(w, r) (int64, bool)`, used by nothing else — the
  mutation seam parses its own ids — but the pattern is the same.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/handlers_test.go`:

```go
func TestZoomShowsOnlyTheSubtree(t *testing.T) {
	s := newServer(t)
	projects := s.seed(t, s.alice, notes.RootID, "Projects")
	s.seed(t, s.alice, projects, "AtBudget")
	s.seed(t, s.alice, notes.RootID, "Reading")

	doc := s.get(t, s.alice, "/notes/"+itoa(projects))

	text := doc.Text()
	if !strings.Contains(text, "Projects") {
		t.Error("the zoom root is not named on its own page")
	}
	if strings.Contains(text, "Reading") {
		t.Error("a sibling of the zoom root is on the page")
	}
	titles := doc.QueryAll("input.outline-title")
	if len(titles) != 1 {
		t.Fatalf("got %d bullets, want just the one child", len(titles))
	}
	if v, _ := htmlassert.Attr(titles[0], "value"); v != "AtBudget" {
		t.Errorf("the visible bullet is %q, want AtBudget", v)
	}
}

func TestZoomRendersTheBreadcrumb(t *testing.T) {
	s := newServer(t)
	projects := s.seed(t, s.alice, notes.RootID, "Projects")
	budget := s.seed(t, s.alice, projects, "AtBudget")
	api := s.seed(t, s.alice, budget, "API")

	doc := s.get(t, s.alice, "/notes/"+itoa(api))
	crumbs := doc.MustHave("nav.outline-crumbs")

	// Outermost first, and every ancestor is a link back to its own zoom.
	links := doc.QueryAll("nav.outline-crumbs a")
	var got []string
	for _, l := range links {
		got = append(got, htmlassert.Text(l))
	}
	want := []string{"All notes", "Projects", "AtBudget"}
	if len(got) != len(want) {
		t.Fatalf("breadcrumb links are %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("breadcrumb link %d is %q, want %q", i, got[i], want[i])
		}
	}
	if href, _ := htmlassert.Attr(links[1], "href"); href != "/notes/"+itoa(projects) {
		t.Errorf("the Projects crumb points at %q", href)
	}
	// The node you are on is not a link to where you already are.
	if !strings.Contains(htmlassert.Text(crumbs), "API") {
		t.Error("the breadcrumb does not name the current root")
	}
}

func TestTopLevelHasNoBreadcrumb(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "Projects")

	s.get(t, s.alice, "/notes/").MustNotHave("nav.outline-crumbs")
}

func TestZoomingIntoAnotherUsersNodeIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.bob, notes.RootID, "bob's secret")

	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/notes/"+itoa(id), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "bob's secret") {
		t.Error("another user's bullet leaked through the zoom route")
	}
}

func TestZoomingIntoNonsenseIs404(t *testing.T) {
	s := newServer(t)
	for _, path := range []string{"/notes/0", "/notes/-1", "/notes/abc", "/notes/999999"} {
		rec := s.do(t, s.alice, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
}

func TestBulletDotZoomsIn(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "Projects")

	doc := s.get(t, s.alice, "/notes/")
	href, _ := htmlassert.Attr(doc.MustHave("a.outline-dot"), "href")
	if href != "/notes/"+itoa(id) {
		t.Errorf("the bullet dot points at %q, want /notes/%d", href, id)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/ -run 'TestZoom|TestTopLevel|TestBulletDot' -count=1`

Expected: FAIL — `/notes/{id}` is not registered, so the mux's catch-all
answers 404 for the two-segment paths and `TestZoomShowsOnlyTheSubtree` fails
on the status.

- [ ] **Step 3: Register the route**

In `internal/apps/notes/app.go`, add to `Mount`:

```go
	r.HandleFunc("GET /{$}", a.outline)
	r.HandleFunc("GET /{id}", a.outlineZoomed)
```

- [ ] **Step 4: Write the handler**

In `internal/apps/notes/handlers.go`, add beside `outline`:

```go
// nodeID parses the {id} wildcard. A path that is not a positive integer is a
// 404 rather than a 400: from outside, "there is nothing at that address" is
// the same answer either way, and it is the true one.
func (a *App) nodeID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// outlineZoomed renders the outline rooted at one node — spec §6: zoom is the
// URL, and the only difference from the top level is which node the recursive
// query starts at.
func (a *App) outlineZoomed(w http.ResponseWriter, r *http.Request) {
	id, ok := a.nodeID(w, r)
	if !ok {
		return
	}
	a.renderOutline(w, r, id)
}
```

- [ ] **Step 5: Add the breadcrumb to the template**

In `internal/apps/notes/templates/outline.html`, replace the `<h1>Notes</h1>`
line inside `{{define "content"}}` with:

```html
	{{if .Data.Zoomed}}
	<nav class="outline-crumbs" aria-label="Outline breadcrumb">
		<a href="/notes/">All notes</a>
		{{range .Data.Crumbs}}
		<span aria-hidden="true">/</span>
		<a href="/notes/{{.ID}}">{{.DisplayTitle}}</a>
		{{end}}
		<span aria-hidden="true">/</span>
		<span class="outline-crumb-current">{{.Data.Root.DisplayTitle}}</span>
	</nav>
	<h1>{{.Data.Root.DisplayTitle}}</h1>
	{{with .Data.Root.Note}}<p class="dim">{{.}}</p>{{end}}
	{{else}}
	<h1>Notes</h1>
	{{end}}
```

- [ ] **Step 6: Style the breadcrumb**

In `internal/ui/static/app.css`, add at the top of the outline section, right
after the `.notes` rule:

```css
.outline-crumbs {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: var(--s-2);
	font-size: var(--fs-sm);
	color: var(--c-text-dim);
}

.outline-crumbs a { color: var(--c-text-dim); text-decoration: none; }
.outline-crumbs a:hover { color: var(--c-accent); }
.outline-crumb-current { color: var(--c-text); font-weight: 500; }
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/ -count=1`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/notes internal/ui && git commit -m "notes: zoom into a bullet with a breadcrumb above it"
```

---

## Task 5: The mutation seam, and `POST /notes/{id}/text`

This is the task the whole chunk turns on. Everything that writes goes through
one helper that opens exactly one transaction, saves the focused bullet's text,
and then performs the structural operation. `POST /notes/{id}/text` is the
first route through it — and, as the contract table says, the one route whose
subject is its own path id.

**Files:**
- Modify: `internal/apps/notes/app.go`
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `Store.Do`, `Ops.SetText`, `Store.SetText` from N1.
- Produces:
  - `type mutation struct{ UserID, NodeID, FocusID, Root int64 }`
  - `func (a *App) mutate(w http.ResponseWriter, r *http.Request, op func(context.Context, *Ops, mutation) error)`
  - `func formID(r *http.Request, name string) (int64, bool)`

  Tasks 6–9 each add one handler that is a two-line call to `mutate`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/handlers_test.go`:

```go
func TestSetTextSavesTheBullet(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "old")

	s.submit(t, s.alice, "/notes/"+itoa(id)+"/text", url.Values{
		"root": {"0"}, "focus_id": {itoa(id)},
		"title": {"new"}, "note": {"a note"},
	}, "/notes/")

	n, err := s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "new" || n.Note != "a note" {
		t.Errorf("saved %q / %q, want new / a note", n.Title, n.Note)
	}
}

func TestSetTextReturnsToTheZoomItCameFrom(t *testing.T) {
	s := newServer(t)
	root := s.seed(t, s.alice, notes.RootID, "Projects")
	child := s.seed(t, s.alice, root, "AtBudget")

	s.submit(t, s.alice, "/notes/"+itoa(child)+"/text", url.Values{
		"root": {itoa(root)}, "title": {"renamed"}, "note": {""},
	}, "/notes/"+itoa(root))
}

func TestSetTextOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.bob, notes.RootID, "bob's")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/text", url.Values{
		"root": {"0"}, "title": {"stolen"}, "note": {""},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	n, err := s.store.ByID(context.Background(), s.bob.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "bob's" {
		t.Errorf("bob's bullet is now %q", n.Title)
	}
}

func TestMutationsWithoutACSRFTokenAreRejected(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "Projects")

	form := url.Values{"root": {"0"}, "title": {"changed"}, "note": {""}}
	req := httptest.NewRequest("POST", "/notes/"+itoa(id)+"/text", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := s.do(t, s.alice, req)

	if rec.Code == http.StatusSeeOther {
		t.Fatal("a POST with no CSRF token succeeded")
	}
	n, err := s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "Projects" {
		t.Errorf("the bullet was changed to %q", n.Title)
	}
}

func TestMalformedRootIsRejected(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "Projects")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/text", url.Values{
		"root": {"not-a-number"}, "title": {"changed"}, "note": {""},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestOversizeTextIsRejected: unreachable from a browser, because the inputs
// carry maxlength. This is the backstop for anything that is not a browser.
func TestOversizeTextIsRejected(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "Projects")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/text", url.Values{
		"root": {"0"}, "title": {strings.Repeat("a", notes.MaxTitleRunes+1)}, "note": {""},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/ -run 'TestSetText|TestMutations|TestMalformed|TestOversize' -count=1`

Expected: FAIL — the route is not registered, so every POST is a 404 from the
mux's catch-all.

- [ ] **Step 3: Register the route**

In `internal/apps/notes/app.go`, add to `Mount`:

```go
	r.HandleFunc("POST /{id}/text", a.setText)
```

- [ ] **Step 4: Write the seam**

Add to `internal/apps/notes/handlers.go`. Note the new `context` import.

```go
// outlinePath is the zoom URL for a root, and the only place in this package
// where a path is built from an id. It is always "/notes/" followed by
// decimal digits, so a redirect through it can never leave the site.
func outlinePath(root int64) string {
	if root == RootID {
		return "/notes/"
	}
	return "/notes/" + strconv.FormatInt(root, 10)
}

// mutation is a structural request already parsed: the fields spec §7 defines,
// plus the ids the route supplies.
type mutation struct {
	UserID int64
	// NodeID is the {id} in the path — the bullet the operation acts on. It
	// is RootID on POST /notes/new, which has no {id}.
	NodeID int64
	// FocusID is the bullet the caret was in, or RootID for "none". It is
	// deliberately independent of NodeID: in this chunk they always coincide,
	// because each row is its own form, but from N3 a click on one bullet's
	// control while the caret is in another makes them differ.
	FocusID int64
	// Root is the zoom the request was issued from, and is used for nothing
	// but the redirect.
	Root int64
}

// mutate is spec §7's single write.
//
// It saves the focused bullet's text and performs the structural operation
// inside one transaction, so the two cannot interleave with anything and
// cannot half-apply. Without it there are two writes, and a user who types and
// then presses Tab loses whatever landed after the last save.
//
// op must reach the database only through the *Ops it is handed. The platform
// opens SQLite with SetMaxOpenConns(1): a closure that calls a *Store method —
// a.store.Outline, a.store.Indent, anything — waits for the connection its own
// transaction is holding, and waits for ever. There is no write timeout in the
// server, so that is a frozen process rather than a failed request. Rendering
// and redirecting therefore happen out here, after Do has returned.
func (a *App) mutate(w http.ResponseWriter, r *http.Request, op func(context.Context, *Ops, mutation) error) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	// Everything the request carries is read up front, before the transaction
	// opens, so nothing inside it depends on parsing that might fail.
	root, ok := formID(r, "root")
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	focusID, ok := formID(r, "focus_id")
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	title, note := r.PostFormValue("title"), r.PostFormValue("note")

	// A path with no {id} — POST /notes/new — leaves NodeID at RootID. Every
	// other route's pattern guarantees the wildcard is there, and a value that
	// is present but not a positive integer is a 404, as it is for a GET.
	nodeID := RootID
	if raw := r.PathValue("id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			a.deps.Errors.Status(w, r, http.StatusNotFound)
			return
		}
		nodeID = id
	}

	m := mutation{UserID: userID, NodeID: nodeID, FocusID: focusID, Root: root}
	ctx := r.Context()

	err := a.store.Do(ctx, func(o *Ops) error {
		if m.FocusID != RootID {
			if err := o.SetText(ctx, m.UserID, m.FocusID, title, note); err != nil {
				return err
			}
		}
		return op(ctx, o, m)
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}

	http.Redirect(w, r, outlinePath(root), http.StatusSeeOther)
}

// formID reads a form field as a node id.
//
// An absent or empty field is RootID, which every caller reads as "none". A
// field that is present but is not a positive integer is a malformed request,
// reported rather than silently treated as absent: for focus_id, treating it
// as absent would drop the user's text on the floor without saying so.
func formID(r *http.Request, name string) (int64, bool) {
	raw := r.PostFormValue(name)
	if raw == "" {
		return RootID, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return RootID, false
	}
	return id, true
}

// setText saves one bullet's text.
//
// It is the one route that does not go through mutate, and the one that
// ignores focus_id: its subject is the path id, so target and focus cannot
// differ, and there is only one write to make. The row form still sends the
// hidden focus_id field, because the same form's other buttons need it.
func (a *App) setText(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.nodeID(w, r)
	if !ok {
		return
	}
	root, ok := formID(r, "root")
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	if err := a.store.SetText(r.Context(), userID, id, r.PostFormValue("title"), r.PostFormValue("note")); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, outlinePath(root), http.StatusSeeOther)
}
```

Add `"context"` and `"strconv"` to the file's imports — `nodeID` from Task 4
already brought in `strconv`, so check before adding it twice.

Note that `mutate` is unused until Task 6. `staticcheck` reports U1000 for an
unused unexported function, so **Step 5 below runs the tests, not the full
check** — the full check runs at the end of Task 6, once `create` uses it. If
the task order changes, keep `mutate` and its first caller in the same commit.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/ -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/notes && git commit -m "notes: save a bullet's text, and the one-transaction seam"
```

---

## Task 6: `POST /notes/new`

Creating a bullet, and the empty outline's first one. This is the first route
through `mutate`, and the one whose shape is built for N3: `title` is what
stays behind on the focused bullet, `new_title` is what moves to the new one.

**Files:**
- Modify: `internal/apps/notes/app.go`
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/templates/outline.html`
- Modify: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `mutate`, `mutation` from Task 5; `Ops.Create`, `Ops.ByID`, and the
  package constant `maxPosition` (`1 << 30`, already in `tree.go`), which
  `Ops.Create` clamps to the end of a sibling list.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/handlers_test.go`. The two helpers come first:
they belong beside `seed` and `itoa` at the top of the file, not down among the
tests, and every task after this one uses them.

```go
// titlesAt reads a sibling list's titles in order, which is what almost every
// assertion about creating, moving and deleting is really about.
func (s *server) titlesAt(t *testing.T, sess *session, parentID int64) []string {
	t.Helper()
	children, err := s.store.Children(context.Background(), sess.user.ID, parentID)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(children))
	for _, c := range children {
		out = append(out, c.Title)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestEmptyOutlineOffersOneBullet is spec §6: a new user's first keystroke
// lands in the outline, not on a "create your first note" button.
func TestEmptyOutlineOffersOneBullet(t *testing.T) {
	s := newServer(t)
	doc := s.get(t, s.alice, "/notes/")

	form := doc.MustHave(`form[action=/notes/new]`)
	if _, ok := htmlassert.Attr(form, "action"); !ok {
		t.Fatal("the empty outline offers no way to start")
	}
	in := doc.MustHave(`input[name=new_title]`)
	if _, ok := htmlassert.Attr(in, "autofocus"); !ok {
		t.Error("the first bullet is not focused")
	}
	doc.MustNotHave(`input[name=title]`)
}

func TestCreateFromTheEmptyOutline(t *testing.T) {
	s := newServer(t)

	s.submit(t, s.alice, "/notes/new", url.Values{
		"root": {"0"}, "new_title": {"first"},
	}, "/notes/")

	children, err := s.store.Children(context.Background(), s.alice.user.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].Title != "first" {
		t.Fatalf("children = %+v, want one bullet titled first", children)
	}
}

// TestCreateWithNoFocusAppendsToTheZoomRoot: the empty-outline form carries a
// root and no focus, and the bullet has to land inside the zoom the user is
// looking at, not at the top level.
func TestCreateWithNoFocusAppendsToTheZoomRoot(t *testing.T) {
	s := newServer(t)
	root := s.seed(t, s.alice, notes.RootID, "Projects")

	s.submit(t, s.alice, "/notes/new", url.Values{
		"root": {itoa(root)}, "new_title": {"AtBudget"},
	}, "/notes/"+itoa(root))

	children, err := s.store.Children(context.Background(), s.alice.user.ID, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].Title != "AtBudget" {
		t.Fatalf("children of the zoom root = %+v", children)
	}
}

// TestCreateAfterTheFocusedBullet is Enter: the new bullet is the focused
// one's next sibling, not the last child of anything.
func TestCreateAfterTheFocusedBullet(t *testing.T) {
	s := newServer(t)
	first := s.seed(t, s.alice, notes.RootID, "first")
	s.seed(t, s.alice, notes.RootID, "third")

	s.submit(t, s.alice, "/notes/new", url.Values{
		"root": {"0"}, "focus_id": {itoa(first)},
		"title": {"first"}, "note": {""},
		"new_title": {"second"},
	}, "/notes/")

	if got := s.titlesAt(t, s.alice, notes.RootID); !equalStrings(got, []string{"first", "second", "third"}) {
		t.Fatalf("children = %v, want [first second third]", got)
	}
}

// TestCreateSplitsTheFocusedBulletsText is spec §8's Enter, ahead of the
// keyboard that will send it: what stays and what moves are one write.
func TestCreateSplitsTheFocusedBulletsText(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "hello world")

	s.submit(t, s.alice, "/notes/new", url.Values{
		"root": {"0"}, "focus_id": {itoa(id)},
		"title": {"hello"}, "note": {""},
		"new_title": {"world"},
	}, "/notes/")

	children, err := s.store.Children(context.Background(), s.alice.user.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 || children[0].Title != "hello" || children[1].Title != "world" {
		t.Fatalf("children = %+v, want hello then world", children)
	}
}

func TestCreateUnderAnotherUsersFocusIs404(t *testing.T) {
	s := newServer(t)
	bobs := s.seed(t, s.bob, notes.RootID, "bob's")

	rec := s.post(t, s.alice, "/notes/new", url.Values{
		"root": {"0"}, "focus_id": {itoa(bobs)},
		"title": {"stolen"}, "note": {""}, "new_title": {"x"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	// Nothing was created for alice, and nothing was changed for bob: the
	// whole transaction rolled back.
	alices, err := s.store.Children(context.Background(), s.alice.user.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(alices) != 0 {
		t.Errorf("alice gained %d bullets", len(alices))
	}
	n, err := s.store.ByID(context.Background(), s.bob.user.ID, bobs)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "bob's" {
		t.Errorf("bob's bullet is now %q", n.Title)
	}
}

func TestPlusButtonAddsAnEmptyBulletBelow(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "Projects")

	doc := s.get(t, s.alice, "/notes/")
	btn := doc.MustHave(`button[formaction=/notes/new]`)
	if btn == nil {
		t.Fatal("a bullet offers no way to add one below it")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/ -run 'TestEmptyOutline|TestCreate|TestPlus' -count=1`

Expected: FAIL — `/notes/new` is not registered, and the empty outline renders
nothing.

- [ ] **Step 3: Register the route**

In `internal/apps/notes/app.go`, add to `Mount`:

```go
	r.HandleFunc("POST /new", a.create)
```

- [ ] **Step 4: Write the handler**

Add to `internal/apps/notes/handlers.go`:

```go
// create adds a bullet.
//
// It is Enter, written before the keyboard that will press it: title is what
// stays on the focused bullet, new_title is what moves to the new one, and
// both happen in the one transaction, so a split can never lose half of a
// line. N2's "+" button is the same request with new_title empty.
//
// Placement follows from the focus. With one, the bullet becomes the focused
// one's next sibling — Enter puts a line below the line you are on, not at the
// bottom of the document. Without one, which is the empty outline's form, it
// is appended to the zoom root the request came from.
func (a *App) create(w http.ResponseWriter, r *http.Request) {
	newTitle := r.PostFormValue("new_title")

	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		parentID, afterPos := m.Root, maxPosition

		if m.FocusID != RootID {
			// Read through the transaction, so the sibling list this lands in
			// is the one mutate's text update has already touched.
			focus, err := o.ByID(ctx, m.UserID, m.FocusID)
			if err != nil {
				return err
			}
			parentID, afterPos = focus.ParentID, focus.Position
		}

		_, err := o.Create(ctx, m.UserID, parentID, afterPos, newTitle, "")
		return err
	})
}
```

- [ ] **Step 5: Add the empty-outline bullet to the template**

In `internal/apps/notes/templates/outline.html`, replace the `#outline` div's
body:

```html
	<div id="outline" class="outline">
		{{if .Data.Rows}}
		{{template "outline-rows" .Data.Rows}}
		{{else}}
		{{template "outline-first" .Data}}
		{{end}}
	</div>
```

and append a new block to the file:

```html
{{/* An outline with nothing in it renders one empty bullet rather than an
     empty-state panel — spec §6. A new user's first keystroke should land in
     the outline, and a zoomed-in bullet with no children needs exactly the
     same affordance. It posts to /notes/new with no focus_id, so the bullet
     is appended to whichever root is on screen. */}}
{{define "outline-first"}}
<ul class="outline-list">
	<li class="outline-item">
		<div class="outline-row">
			<form method="post" action="/notes/new" class="outline-main">
				<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
				<input type="hidden" name="root" value="{{.Root.ID}}">
				<span class="outline-chevron" aria-hidden="true"></span>
				<span class="outline-dot" aria-hidden="true">&bull;</span>
				<span class="outline-text">
					<input class="outline-title" type="text" name="new_title"
					       maxlength="2000" autofocus
					       placeholder="Start typing…" aria-label="New bullet">
				</span>
				<span class="outline-actions">
					<button type="submit" class="quiet" aria-label="Add bullet">+</button>
				</span>
			</form>
		</div>
	</li>
</ul>
{{end}}
```

`.Root.ID` is `RootID` (0) at the top level and the zoom root's id otherwise —
which is exactly the value `create` wants, with no branch in the template.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/ -count=1`

Expected: PASS.

- [ ] **Step 7: Run the full check**

Run: `gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1`

Expected: all clean. `staticcheck` is the one that matters here: `mutate` now
has a caller, so U1000 is silent.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/notes && git commit -m "notes: create a bullet, and start an empty outline"
```

---

## Task 7: Indent and outdent

Two routes, each a two-line call to `mutate`. The tests are where the work is:
indent is the operation that can push a subtree past `MaxDepth`, and outdent is
the one whose result can leave the visible outline.

**Files:**
- Modify: `internal/apps/notes/app.go`
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/handlers_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/handlers_test.go`:

```go
func TestIndentNestsUnderThePreviousSibling(t *testing.T) {
	s := newServer(t)
	first := s.seed(t, s.alice, notes.RootID, "first")
	second := s.seed(t, s.alice, notes.RootID, "second")

	s.submit(t, s.alice, "/notes/"+itoa(second)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(second)},
		"title": {"second"}, "note": {""},
	}, "/notes/")

	children, err := s.store.Children(context.Background(), s.alice.user.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].ID != second {
		t.Fatalf("children of first = %+v, want just second", children)
	}

	// And the page now shows it nested.
	doc := s.get(t, s.alice, "/notes/")
	if n := len(doc.QueryAll(".outline-item .outline-item")); n != 1 {
		t.Errorf("got %d nested bullets on the page, want 1", n)
	}
}

func TestIndentOfTheFirstSiblingDoesNothing(t *testing.T) {
	s := newServer(t)
	first := s.seed(t, s.alice, notes.RootID, "first")

	// Not an error: the caller is a keypress, and Tab on the first line of an
	// outline should do nothing rather than complain.
	s.submit(t, s.alice, "/notes/"+itoa(first)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(first)}, "title": {"first"}, "note": {""},
	}, "/notes/")

	n, err := s.store.ByID(context.Background(), s.alice.user.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if n.ParentID != notes.RootID || n.Position != 0 {
		t.Errorf("first moved to parent %d position %d", n.ParentID, n.Position)
	}
}

func TestOutdentPromotesToTheParentsNextSibling(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.alice, notes.RootID, "parent")
	child := s.seed(t, s.alice, parent, "child")
	s.seed(t, s.alice, notes.RootID, "after")

	s.submit(t, s.alice, "/notes/"+itoa(child)+"/outdent", url.Values{
		"root": {"0"}, "focus_id": {itoa(child)}, "title": {"child"}, "note": {""},
	}, "/notes/")

	top, err := s.store.Children(context.Background(), s.alice.user.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range top {
		got = append(got, n.Title)
	}
	if len(got) != 3 || got[0] != "parent" || got[1] != "child" || got[2] != "after" {
		t.Fatalf("top level = %v, want [parent child after]", got)
	}
}

func TestOutdentAtTheTopLevelDoesNothing(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "only")

	s.submit(t, s.alice, "/notes/"+itoa(id)+"/outdent", url.Values{
		"root": {"0"}, "focus_id": {itoa(id)}, "title": {"only"}, "note": {""},
	}, "/notes/")

	n, err := s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.ParentID != notes.RootID {
		t.Errorf("a top-level bullet was reparented to %d", n.ParentID)
	}
}

// TestIndentPastTheDepthLimitIsRejected. MaxDepth exists so that a runaway
// import cannot produce an outline no UI can render; a hand-driven indent has
// to hit the same wall, and hit it as a 400 rather than a 500.
func TestIndentPastTheDepthLimitIsRejected(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	// A chain exactly MaxDepth deep: depths 0..MaxDepth.
	parent := notes.RootID
	var deepest int64
	for d := 0; d <= notes.MaxDepth; d++ {
		n, err := s.store.Create(ctx, s.alice.user.ID, parent, 1<<30, "d", "")
		if err != nil {
			t.Fatalf("building the chain at depth %d: %v", d, err)
		}
		parent, deepest = n.ID, n.ID
	}
	// A sibling of the deepest node; indenting it would put it one past the cap.
	deepestNode, err := s.store.ByID(ctx, s.alice.user.ID, deepest)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := s.store.Create(ctx, s.alice.user.ID, deepestNode.ParentID, 1<<30, "sibling", "")
	if err != nil {
		t.Fatal(err)
	}

	rec := s.post(t, s.alice, "/notes/"+itoa(sibling.ID)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(sibling.ID)}, "title": {"sibling"}, "note": {""},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	n, err := s.store.ByID(ctx, s.alice.user.ID, sibling.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n.ParentID != deepestNode.ParentID {
		t.Errorf("the bullet moved despite the rejection")
	}
}

func TestIndentingAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.bob, notes.RootID, "bob's first")
	second := s.seed(t, s.bob, notes.RootID, "bob's second")

	rec := s.post(t, s.alice, "/notes/"+itoa(second)+"/indent", url.Values{
		"root": {"0"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	n, err := s.store.ByID(context.Background(), s.bob.user.ID, second)
	if err != nil {
		t.Fatal(err)
	}
	if n.ParentID != notes.RootID {
		t.Error("bob's bullet was indented by alice")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/ -run 'TestIndent|TestOutdent' -count=1`

Expected: FAIL — neither route is registered.

- [ ] **Step 3: Register the routes**

In `internal/apps/notes/app.go`, add to `Mount`:

```go
	r.HandleFunc("POST /{id}/indent", a.indent)
	r.HandleFunc("POST /{id}/outdent", a.outdent)
```

- [ ] **Step 4: Write the handlers**

Add to `internal/apps/notes/handlers.go`:

```go
// indent makes a bullet the last child of the sibling above it. Being already
// first is a no-op in the store, not an error — see Ops.Indent.
func (a *App) indent(w http.ResponseWriter, r *http.Request) {
	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		return o.Indent(ctx, m.UserID, m.NodeID)
	})
}

// outdent makes a bullet the next sibling of its own parent.
//
// The template disables this on a direct child of the zoom root, because the
// result would be a bullet that is still there and no longer on screen. The
// handler does not enforce that: it is a UI courtesy, not a rule about the
// tree, and the store's answer is correct either way.
func (a *App) outdent(w http.ResponseWriter, r *http.Request) {
	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		return o.Outdent(ctx, m.UserID, m.NodeID)
	})
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/ -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/notes && git commit -m "notes: indent and outdent a bullet"
```

---

## Task 8: Move up, move down, and collapse

**Files:**
- Modify: `internal/apps/notes/app.go`
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/handlers_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/handlers_test.go`, using the `titlesAt` and
`equalStrings` helpers Task 6 added:

```go
func TestMoveUpAndDown(t *testing.T) {
	s := newServer(t)
	a := s.seed(t, s.alice, notes.RootID, "a")
	b := s.seed(t, s.alice, notes.RootID, "b")
	s.seed(t, s.alice, notes.RootID, "c")

	s.submit(t, s.alice, "/notes/"+itoa(b)+"/move", url.Values{
		"root": {"0"}, "focus_id": {itoa(b)}, "title": {"b"}, "note": {""},
		"dir": {"up"},
	}, "/notes/")
	if got := s.titlesAt(t, s.alice, notes.RootID); !equalStrings(got, []string{"b", "a", "c"}) {
		t.Fatalf("after move up: %v, want [b a c]", got)
	}

	s.submit(t, s.alice, "/notes/"+itoa(a)+"/move", url.Values{
		"root": {"0"}, "focus_id": {itoa(a)}, "title": {"a"}, "note": {""},
		"dir": {"down"},
	}, "/notes/")
	if got := s.titlesAt(t, s.alice, notes.RootID); !equalStrings(got, []string{"b", "c", "a"}) {
		t.Fatalf("after move down: %v, want [b c a]", got)
	}
}

func TestMoveAtTheEdgesDoesNothing(t *testing.T) {
	s := newServer(t)
	a := s.seed(t, s.alice, notes.RootID, "a")
	b := s.seed(t, s.alice, notes.RootID, "b")

	for _, tc := range []struct {
		id  int64
		dir string
	}{{a, "up"}, {b, "down"}} {
		s.submit(t, s.alice, "/notes/"+itoa(tc.id)+"/move", url.Values{
			"root": {"0"}, "dir": {tc.dir},
		}, "/notes/")
	}
	if got := s.titlesAt(t, s.alice, notes.RootID); !equalStrings(got, []string{"a", "b"}) {
		t.Fatalf("order changed to %v", got)
	}
}

func TestMoveRejectsAnUnknownDirection(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	for _, dir := range []string{"", "sideways", "UP", "1"} {
		rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/move", url.Values{
			"root": {"0"}, "dir": {dir},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("dir=%q gave %d, want 400", dir, rec.Code)
		}
	}
}

func TestCollapseAndExpand(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.alice, notes.RootID, "parent")
	s.seed(t, s.alice, parent, "child")

	s.submit(t, s.alice, "/notes/"+itoa(parent)+"/collapse", url.Values{
		"root": {"0"}, "focus_id": {itoa(parent)}, "title": {"parent"}, "note": {""},
		"collapsed": {"1"},
	}, "/notes/")

	n, err := s.store.ByID(ctx, s.alice.user.ID, parent)
	if err != nil {
		t.Fatal(err)
	}
	if !n.Collapsed {
		t.Fatal("the bullet is not collapsed")
	}
	if strings.Contains(s.get(t, s.alice, "/notes/").Text(), "child") {
		t.Error("a collapsed bullet's child is still in the response")
	}

	s.submit(t, s.alice, "/notes/"+itoa(parent)+"/collapse", url.Values{
		"root": {"0"}, "collapsed": {"0"},
	}, "/notes/")

	n, err = s.store.ByID(ctx, s.alice.user.ID, parent)
	if err != nil {
		t.Fatal(err)
	}
	if n.Collapsed {
		t.Error("the bullet is still collapsed")
	}
}

// TestCollapseIsIdempotent: the field says what the state should become, not
// "flip it", so a double submit or a stale page cannot toggle it back.
func TestCollapseIsIdempotent(t *testing.T) {
	s := newServer(t)
	parent := s.seed(t, s.alice, notes.RootID, "parent")
	s.seed(t, s.alice, parent, "child")

	for range 2 {
		s.submit(t, s.alice, "/notes/"+itoa(parent)+"/collapse", url.Values{
			"root": {"0"}, "collapsed": {"1"},
		}, "/notes/")
	}
	n, err := s.store.ByID(context.Background(), s.alice.user.ID, parent)
	if err != nil {
		t.Fatal(err)
	}
	if !n.Collapsed {
		t.Error("collapsing twice left the bullet expanded")
	}
}

func TestCollapseRejectsAnUnknownValue(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	for _, v := range []string{"", "true", "yes", "2"} {
		rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/collapse", url.Values{
			"root": {"0"}, "collapsed": {v},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("collapsed=%q gave %d, want 400", v, rec.Code)
		}
	}
}

func TestMovingAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.bob, notes.RootID, "bob's a")
	second := s.seed(t, s.bob, notes.RootID, "bob's b")

	for _, path := range []string{"/move", "/collapse"} {
		form := url.Values{"root": {"0"}, "dir": {"up"}, "collapsed": {"1"}}
		rec := s.post(t, s.alice, "/notes/"+itoa(second)+path, form)
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, rec.Code)
		}
	}
	if got := s.titlesAt(t, s.bob, notes.RootID); !equalStrings(got, []string{"bob's a", "bob's b"}) {
		t.Errorf("bob's outline is now %v", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/ -run 'TestMove|TestCollapse' -count=1`

Expected: FAIL — neither route is registered.

- [ ] **Step 3: Register the routes**

In `internal/apps/notes/app.go`, add to `Mount`:

```go
	r.HandleFunc("POST /{id}/move", a.move)
	r.HandleFunc("POST /{id}/collapse", a.collapse)
```

- [ ] **Step 4: Write the handlers**

Add to `internal/apps/notes/handlers.go`:

```go
// move swaps a bullet with the sibling above or below it.
//
// dir is exactly "up" or "down". The general Move — arbitrary parent, arbitrary
// position — stays out of the HTTP surface until something needs it: N10's
// drag-to-move is the only thing in the design that does.
func (a *App) move(w http.ResponseWriter, r *http.Request) {
	dir := r.PostFormValue("dir")
	if dir != "up" && dir != "down" {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		if dir == "up" {
			return o.MoveUp(ctx, m.UserID, m.NodeID)
		}
		return o.MoveDown(ctx, m.UserID, m.NodeID)
	})
}

// collapse sets a bullet's collapse state.
//
// The field names the state to arrive at rather than asking for a toggle, so a
// double submit, a refresh or a stale page cannot flip it back. The rendered
// chevron already knows the current state and sends its opposite.
func (a *App) collapse(w http.ResponseWriter, r *http.Request) {
	raw := r.PostFormValue("collapsed")
	if raw != "0" && raw != "1" {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	collapsed := raw == "1"

	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		return o.SetCollapsed(ctx, m.UserID, m.NodeID, collapsed)
	})
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/ -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/notes && git commit -m "notes: move a bullet among its siblings, and collapse it"
```

---

## Task 9: Delete

The one irreversible operation in the chunk, so it gets its own form and its
own confirmation.

**Files:**
- Modify: `internal/apps/notes/app.go`
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/templates/outline.html`
- Modify: `internal/ui/static/app.css`
- Modify: `internal/apps/notes/handlers_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/handlers_test.go`:

```go
func TestDeleteRemovesTheBulletAndItsSubtree(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	parent := s.seed(t, s.alice, notes.RootID, "parent")
	child := s.seed(t, s.alice, parent, "child")
	s.seed(t, s.alice, notes.RootID, "survivor")

	s.submit(t, s.alice, "/notes/"+itoa(parent)+"/delete", url.Values{
		"root": {"0"},
	}, "/notes/")

	if got := s.titlesAt(t, s.alice, notes.RootID); !equalStrings(got, []string{"survivor"}) {
		t.Fatalf("top level = %v, want [survivor]", got)
	}
	if _, err := s.store.ByID(ctx, s.alice.user.ID, child); err == nil {
		t.Error("the child outlived its parent")
	}
}

// TestDeleteRenumbersTheSurvivors: I1 says sibling positions are contiguous
// from zero, and a delete that leaves a gap makes every later clamp land one
// place off — silently, three moves later.
func TestDeleteRenumbersTheSurvivors(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "a")
	b := s.seed(t, s.alice, notes.RootID, "b")
	s.seed(t, s.alice, notes.RootID, "c")

	s.submit(t, s.alice, "/notes/"+itoa(b)+"/delete", url.Values{"root": {"0"}}, "/notes/")

	children, err := s.store.Children(context.Background(), s.alice.user.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range children {
		if c.Position != i {
			t.Errorf("%q is at position %d, want %d", c.Title, c.Position, i)
		}
	}
}

func TestDeleteReturnsToTheZoomItCameFrom(t *testing.T) {
	s := newServer(t)
	root := s.seed(t, s.alice, notes.RootID, "Projects")
	child := s.seed(t, s.alice, root, "AtBudget")

	s.submit(t, s.alice, "/notes/"+itoa(child)+"/delete", url.Values{
		"root": {itoa(root)},
	}, "/notes/"+itoa(root))
}

func TestDeletingAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.bob, notes.RootID, "bob's")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/delete", url.Values{"root": {"0"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if _, err := s.store.ByID(context.Background(), s.bob.user.ID, id); err != nil {
		t.Errorf("bob's bullet was deleted by alice: %v", err)
	}
}

// TestDeleteIsItsOwnFormWithAConfirmation. data-confirm is a form-level
// attribute the platform's theme.js already handles; on the row's main form it
// would confirm every button in the row, so delete gets a form of its own.
func TestDeleteIsItsOwnConfirmedForm(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "Projects")

	doc := s.get(t, s.alice, "/notes/")
	form := doc.MustHave(`form[action=/notes/` + itoa(id) + `/delete]`)
	if _, ok := htmlassert.Attr(form, "data-confirm"); !ok {
		t.Error("the delete form asks for no confirmation")
	}
	// It must not be the row's main form, or every button would be confirmed.
	if cls, _ := htmlassert.Attr(form, "class"); !strings.Contains(cls, "outline-delete") {
		t.Errorf("the delete form's class is %q", cls)
	}
	doc.MustHave(`form.outline-delete input[name=csrf_token]`)
	doc.MustHave(`form.outline-delete input[name=root]`)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/ -run TestDelet -count=1`

Expected: FAIL — the route is not registered and the form is not in the
template.

- [ ] **Step 3: Register the route**

In `internal/apps/notes/app.go`, add to `Mount`:

```go
	r.HandleFunc("POST /{id}/delete", a.remove)
```

- [ ] **Step 4: Write the handler**

Add to `internal/apps/notes/handlers.go`:

```go
// remove deletes a bullet and everything under it.
//
// Named remove rather than delete because delete is a builtin, and shadowing a
// builtin in a method name reads worse than the one-word mismatch with the
// route.
//
// The subtree goes with it through ON DELETE CASCADE. It is the only
// irreversible thing in this chunk, which is why the template gives it a form
// of its own with data-confirm on it.
//
// Deleting the bullet the page is zoomed to would redirect to a URL that no
// longer resolves, and answer 404. The outline never renders its own root as a
// row, so that cannot be reached from the UI; a hand-made request that does it
// gets an honest "there is nothing at that address" rather than a 500.
func (a *App) remove(w http.ResponseWriter, r *http.Request) {
	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		if err := o.Delete(ctx, m.UserID, m.NodeID); err != nil {
			return err
		}
		a.deps.Log.Info("bullet deleted", "app", ID, "user_id", m.UserID, "node_id", m.NodeID)
		return nil
	})
}
```

- [ ] **Step 5: Add the delete form to the template**

In `internal/apps/notes/templates/outline.html`, inside `{{define
"outline-rows"}}`, add immediately after the closing `</form>` of
`.outline-main` and before the closing `</div>` of `.outline-row`:

```html
			{{/* Its own form so data-confirm — a form-level attribute the
			     platform's theme.js already handles — guards this one button
			     rather than every button in the row. It therefore carries no
			     focus_id: there is nothing worth saving on a bullet that is
			     about to stop existing. */}}
			<form method="post" action="/notes/{{.ID}}/delete" class="outline-delete"
			      data-confirm="Delete “{{.DisplayTitle}}” and everything under it? This cannot be undone.">
				<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
				<input type="hidden" name="root" value="{{.RootID}}">
				<button type="submit" class="quiet" aria-label="Delete">&times;</button>
			</form>
```

- [ ] **Step 6: Style it**

In `internal/ui/static/app.css`, in the outline section, extend the two rules
that hide and reveal the row controls so the delete form travels with them, and
give its button the danger colour:

```css
.outline-chevron,
.outline-actions,
.outline-delete { opacity: 0; }

.outline-row:hover .outline-chevron,
.outline-row:hover .outline-actions,
.outline-row:hover .outline-delete,
.outline-row:focus-within .outline-chevron,
.outline-row:focus-within .outline-actions,
.outline-row:focus-within .outline-delete { opacity: 1; }

.outline-delete { flex: none; }

.outline-delete button {
	padding: 0 var(--s-1);
	border: none;
	background: none;
	color: var(--c-text-faint);
	line-height: 1.7;
}

.outline-delete button:hover { background: var(--c-danger-bg); color: var(--c-danger); }
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/ -count=1`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/notes internal/ui && git commit -m "notes: delete a bullet behind a confirmation"
```

---

## Task 10: The single-write contract, and the docs

The chunk's central claim is spec §7: a structural request saves the focused
bullet's text and does the structural work as one write, so nothing can land
between them. Every task so far has sent `focus_id` and trusted it; this task
proves it, end to end, and then tells the rest of the repository that ON Notes
exists.

**Files:**
- Modify: `internal/apps/notes/handlers_test.go`
- Modify: `README.md`
- Modify: `AGENTS.md`

- [ ] **Step 1: Write the contract tests**

Append to `internal/apps/notes/handlers_test.go`:

```go
// TestStructuralRequestSavesTheFocusedText is spec §7. Every structural POST
// carries the focused bullet's text so the text update and the structural
// operation are one write. Without it, a user who types and then presses Tab
// loses whatever landed after the last save.
func TestStructuralRequestSavesTheFocusedText(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	first := s.seed(t, s.alice, notes.RootID, "first")
	second := s.seed(t, s.alice, notes.RootID, "second")

	s.submit(t, s.alice, "/notes/"+itoa(second)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(second)},
		"title": {"typed after the last save"}, "note": {"and a note"},
	}, "/notes/")

	n, err := s.store.ByID(ctx, s.alice.user.ID, second)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "typed after the last save" || n.Note != "and a note" {
		t.Errorf("the focused bullet is %q / %q; the keystrokes were lost", n.Title, n.Note)
	}
	if n.ParentID != first {
		t.Errorf("the bullet was not indented (parent %d, want %d)", n.ParentID, first)
	}
}

// TestFocusAndTargetCanDiffer is the case N2's forms never produce and N3's
// keyboard will: the caret is in one bullet and the operation acts on another.
func TestFocusAndTargetCanDiffer(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	focused := s.seed(t, s.alice, notes.RootID, "focused")
	parent := s.seed(t, s.alice, notes.RootID, "parent")
	s.seed(t, s.alice, parent, "child")

	s.submit(t, s.alice, "/notes/"+itoa(parent)+"/collapse", url.Values{
		"root": {"0"}, "focus_id": {itoa(focused)},
		"title": {"edited elsewhere"}, "note": {""},
		"collapsed": {"1"},
	}, "/notes/")

	f, err := s.store.ByID(ctx, s.alice.user.ID, focused)
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "edited elsewhere" {
		t.Errorf("the focused bullet is %q", f.Title)
	}
	p, err := s.store.ByID(ctx, s.alice.user.ID, parent)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Collapsed {
		t.Error("the targeted bullet was not collapsed")
	}
}

// TestAFailedStructuralOperationSavesNoText proves the two really are one
// transaction rather than two writes that usually both happen. The text update
// succeeds and the structural operation then fails, so the text must be rolled
// back with it.
func TestAFailedStructuralOperationSavesNoText(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	mine := s.seed(t, s.alice, notes.RootID, "mine")
	bobs := s.seed(t, s.bob, notes.RootID, "bob's")

	// Indenting bob's bullet fails; alice's own text update came first and
	// must not survive.
	rec := s.post(t, s.alice, "/notes/"+itoa(bobs)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(mine)},
		"title": {"should not be saved"}, "note": {""},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	n, err := s.store.ByID(ctx, s.alice.user.ID, mine)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "mine" {
		t.Errorf("the text survived a failed operation: %q", n.Title)
	}
}

// TestAForgedFocusIsRejectedWholesale: naming someone else's bullet as the
// focus fails the whole request rather than quietly skipping the text update.
func TestAForgedFocusIsRejectedWholesale(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	bobs := s.seed(t, s.bob, notes.RootID, "bob's")
	s.seed(t, s.alice, notes.RootID, "a")
	b := s.seed(t, s.alice, notes.RootID, "b")

	rec := s.post(t, s.alice, "/notes/"+itoa(b)+"/move", url.Values{
		"root": {"0"}, "focus_id": {itoa(bobs)},
		"title": {"overwritten"}, "note": {""}, "dir": {"up"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if n, err := s.store.ByID(ctx, s.bob.user.ID, bobs); err != nil || n.Title != "bob's" {
		t.Errorf("bob's bullet is now %+v (err %v)", n, err)
	}
	if got := s.titlesAt(t, s.alice, notes.RootID); !equalStrings(got, []string{"a", "b"}) {
		t.Errorf("the move happened anyway: %v", got)
	}
}

func TestAMalformedFocusIsA400(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	for _, v := range []string{"abc", "-1", "0.5"} {
		rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/indent", url.Values{
			"root": {"0"}, "focus_id": {v}, "title": {"x"}, "note": {""},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("focus_id=%q gave %d, want 400", v, rec.Code)
		}
	}
}

// TestEveryMutationRequiresSignIn covers the routes the first sign-in test
// could not, because they are POSTs.
func TestEveryMutationRequiresSignIn(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	paths := []string{
		"/notes/new",
		"/notes/" + itoa(id) + "/text",
		"/notes/" + itoa(id) + "/indent",
		"/notes/" + itoa(id) + "/outdent",
		"/notes/" + itoa(id) + "/move",
		"/notes/" + itoa(id) + "/collapse",
		"/notes/" + itoa(id) + "/delete",
	}
	for _, path := range paths {
		req := httptest.NewRequest("POST", path, strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := s.do(t, nil, req)
		if rec.Code == http.StatusSeeOther && rec.Header().Get("Location") != "/login" {
			t.Errorf("POST %s anonymous redirected to %q, not the login page",
				path, rec.Header().Get("Location"))
		}
		if rec.Code == http.StatusOK {
			t.Errorf("POST %s anonymous = 200", path)
		}
	}

	n, err := s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatalf("the bullet is gone: %v", err)
	}
	if n.Title != "a" {
		t.Errorf("an anonymous request changed the bullet to %q", n.Title)
	}
}
```

`TestEveryMutationRequiresSignIn` asserts loosely on the anonymous response
because the platform's CSRF check and its auth guard both reject the request
and either may run first; what matters is that neither succeeds. Read
`internal/platform/web/middleware.go` and tighten the assertion to whichever it
actually is, rather than leaving the looser form in place.

- [ ] **Step 2: Run them — they must pass**

Run: `go test ./internal/apps/notes/ -count=1 -v -run 'TestStructural|TestFocus|TestAFailed|TestAForged|TestAMalformed|TestEveryMutation'`

Expected: PASS, all six — every behaviour they check was built in Tasks 5–9.
**A failure here is a real defect in the seam, not a missing feature.** If one
fails, fix the handler, not the test.

- [ ] **Step 3: Update the README**

Four places in `README.md` currently say ON Notes does not exist. Note that
ON Notes is **usable but not finished** after this chunk — it has no keyboard
layer, no Markdown, no dates, no search — so the wording must not oversell it.

The paragraph after the screenshots (currently "Of the four apps the "ON"
prefix is reserved for, only **ON Paste** is built and registered today…"):

```markdown
Of the four apps the "ON" prefix is reserved for, two are built and registered
today. **ON Paste** holds snippets of code or text, with syntax highlighting
and shareable links. **ON Notes** is a hierarchical outliner — one infinite
tree per account, with zoom, collapse and every structural operation; the
keyboard layer, Markdown, due dates and search are still being built out. ON
Reader and ON Flash are future work: the platform and app framework are ready
for them, but no code exists yet.
```

The repository tree, which currently lists only `paste/` under
`internal/apps/`:

```markdown
│   ├── apps/
│   │   ├── notes/             # ON Notes: a hierarchical outliner
│   │   └── paste/             # ON Paste: snippets, sharing, syntax highlighting
```

The "Adding an app" paragraph below the tree names ON Notes as an example of
an app that could be added; it now has to be ON Reader or ON Flash:

```markdown
Adding an app (ON Reader, ON Flash, or anything else) means writing
```

The **Status** table. It opens with "The three planned build phases are
complete." — still true, and ON Notes is not one of those three phases, so
follow the table with a line rather than adding a misleading row:

```markdown
Work since then is per-app rather than per-phase. ON Notes is being built in
ten small chunks under
[`docs/superpowers/specs/2026-08-25-on-notes-design.md`](docs/superpowers/specs/2026-08-25-on-notes-design.md);
N1 (schema and store) and N2 (the outline) are done.
```

The screenshot `alt` text on the dashboard image says "with the ON Paste app
tile". Update the text; **do not** regenerate the screenshots — that is a
separate change, and a stale image is more honest than a rushed one.

- [ ] **Step 4: Update AGENTS.md**

In `AGENTS.md`, the "What this is" paragraph currently reads "Only **ON Paste**
… is built today; ON Notes, ON Reader, and ON Flash are reserved names with no
code yet." Replace with:

```markdown
step. **ON Paste** (snippets with syntax highlighting and shareable links) and
**ON Notes** (a hierarchical outliner, still being built out) are registered
today; ON Reader and ON Flash are reserved names with no code yet.
```

- [ ] **Step 5: Run the full check**

Run each, and read the output rather than assuming it:

```bash
gofmt -l .
```

```bash
go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1
```

```bash
git diff --stat go.mod go.sum
```

Expected: `gofmt` prints nothing; vet, staticcheck and every package pass; the
`go.mod`/`go.sum` diff is empty.

- [ ] **Step 6: Look at it**

Run the server and use it, because no test in this file can tell you the
outline is unpleasant to read:

```bash
go run ./cmd/onsuite serve
```

Check, in both themes and at phone width: a bullet's controls appear on hover
and on keyboard focus; the guide lines line up with the bullets; the note line
under a bullet is legible without competing with the title; the breadcrumb
wraps rather than overflowing.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/notes README.md AGENTS.md && git commit -m "notes: prove the one-write contract, and record that the app exists"
```

---

## Self-review notes for the executing agent

Three things in this plan are decisions that look like mistakes. They were
weighed; do not silently "fix" them, and if you disagree, raise it rather than
changing it.

1. **`view_test.go` is `package notes`** while the four test files beside it
   are `package notes_test`. Task 2 explains why.
2. **The test harness in `handlers_test.go` is a near-copy of ON Paste's.**
   There is no shared test-server package, and creating one means teaching
   `internal/arch` a new test-only rule — platform work inside an app chunk.
   See "Follow-ups".
3. **Every bullet renders an empty note input.** It doubles each row's height
   for a field most bullets never use. The alternative — rendering the note
   only when non-empty — leaves no way to add one without JavaScript, and this
   chunk has none. Spec §19 accepts N2 being clunky in exactly this way.

And one thing that is a genuine trap: **clicking a bullet dot to zoom, or any
breadcrumb link, discards whatever is typed and not yet submitted.** It is a
plain `<a>`; there is no autosave until N3. Do not try to fix it here by
turning the dot into a POST — that adds a route the spec does not have, to
work around a gap the next chunk closes by design.

## Follow-ups to file as issues

Not part of this chunk. File them when the branch is opened, per the
project's habit of tracking follow-ups as GitHub issues.

- **A shared test-server harness.** `newServer` now exists twice, in
  `internal/apps/paste/handlers_test.go` and
  `internal/apps/notes/handlers_test.go`, and every future app will want a
  third. The natural home is an `internal/apptest` package with the same
  test-only rule `internal/htmlassert` has in `internal/arch/arch_test.go`.
- **`/notes/{id}/text` ignores `focus_id`.** It is the one asymmetry in the
  §7 contract. Worth revisiting once N3 exists and it is clear whether the JS
  layer wants a uniform payload badly enough to justify the change.
