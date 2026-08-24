# ON Suite — Admin Page

**Date:** 2026-08-24
**Status:** Proposed
**Scope:** A read-only administrative page at `/admin/`, plus the platform
machinery it needs: a job registry, config introspection, an optional per-app
stats capability, and a route recorder. Adds no way to change anything at
runtime.

## 1. Purpose

ON Suite currently exposes its own state in exactly two places: JSON log lines
on stdout, and the `backups/` directory. An operator who wants to know which
settings are actually in effect, whether the nightly snapshot ran, how large
the database has grown, or which routes are reachable without signing in, has
to read logs, read source, or shell into the box.

This spec adds one page that answers those questions. It is deliberately a
window, not a control panel: it displays, and nothing on it changes state.

It also establishes the pattern for how the page grows. As ON Notes, ON Reader
and ON Flash arrive, each contributes its own numbers by implementing one
optional method — with no change to the admin page, and without the platform
ever importing an app.

## 2. Constraints

Decided during brainstorming; each drives a specific decision below.

| Constraint | Decision |
|---|---|
| The platform never imports an app ([arch_test.go](../../../internal/arch/arch_test.go)) | Per-app numbers arrive through an optional `Stater` capability discovered by type assertion, exactly like the existing `Exporter`. |
| Read-only | No form, no button, no POST route. Every collector is a query or a `runtime` read. |
| HTMX is the only JS; strict CSP | One full-page render on `GET /admin/`. No polling, no fragment endpoints, no inline script. |
| No new dependencies | Everything comes from the standard library, `modernc.org/sqlite` (already present) and existing platform packages. |
| Apps that do not exist yet must not need platform changes | Registering an app is still one line in `registeredApps()`; whether it appears on the admin page is entirely the app's own choice. |

## 3. Placement and authorization

The page is a **platform page**, not an app: `internal/platform/admin`, mounted
directly on the mux in `buildStack` next to the home and login routes. It
reports *on* the platform, so making it an app would invert the layering the
arch test protects, and would put it in the app switcher and on the home
dashboard for every user.

Authorization is a new `RequireAdmin` middleware in
[login.go](../../../internal/platform/web/login.go), composed inside
`RequireUser`:

| Requester | Response |
|---|---|
| Anonymous | 303 to `/login?next=/admin/` — unchanged `RequireUser` behaviour |
| Signed in, not an admin | `errs.NotFound` — byte-identical to any unrouted path |
| Signed in, admin | 200 |

404 rather than 403 is chosen for consistency with the login path, which
already returns identical responses for a wrong password and an unknown
username so that failures cannot be used to enumerate accounts. A non-admin
learns nothing about whether `/admin/` exists.

`auth.User.IsAdmin` and `render.Shell.IsAdmin` already exist and already flow
into every page; today nothing reads them. This is the first authorization
decision in the suite that depends on the flag.

## 4. Navigation

A sidebar entry in [base.html](../../../internal/ui/templates/base.html),
rendered only when `.Shell.IsAdmin`, below the app list and visually separated
from it — the admin page is not an app and should not read as one. It gets an
`"admin"` icon in [icons.go](../../../internal/ui/icons.go). Non-admins see no
trace of it.

## 5. Page structure

One route, `GET /admin/`. One handler. One `Report` struct assembled by seven
independent collectors, then one full-page render of
`internal/ui/templates/admin.html`.

Each collector returns `(data, error)`. **A collector's error is recorded on
its own section and rendered as a note inside that card; the page still
returns 200.** A failing `PRAGMA` must not hide the job status. The error text
is shown verbatim — this is the one page in the suite whose only reader is the
operator, so an internal error string is information rather than a leak.
Everywhere else, `web.Errors` keeps hiding internals; that rule is unchanged.

The page is long, so it carries a sticky in-page anchor nav. It does not
refresh itself: every number is point-in-time, and "reload to refresh" says so
honestly.

| Section | Content | Source |
|---|---|---|
| Build & runtime | version, Go version, OS/arch, `NumCPU`, uptime, goroutines, heap in use | `runtime`, `runtime/debug`, a `StartedAt` captured in `serve` |
| Settings | per setting: flag, `ONSUITE_*` env var, live value, default, source, doc | `config.Settings()` (§6) |
| Jobs | per job: name, description, interval, last run, duration, outcome, next run, run count | `jobs.Registry.Snapshot()` (§7) |
| Apps | one card per app implementing `Stater`; others skipped silently | `app.Registry.Stats()` (§8) |
| Database | path, file/WAL/SHM sizes, page count, page size, journal mode, applied migrations | `PRAGMA` + `schema_migrations` |
| Users & sessions | accounts (username, admin, created); active and expired session counts | two new read-only `auth.Store` queries |
| Routes | every pattern with a public/protected badge, grouped by app and platform, public count summarised | `web.Recorder` (§9) |

## 6. Settings introspection

`config.Parse` builds every value in the system and then discards everything
about how it got there. `Settings()` makes that byproduct available:

```go
type Source int // SourceDefault, SourceEnv, SourceFlag

type Setting struct {
    Flag    string // "backup-interval"
    Env     string // "ONSUITE_BACKUP_INTERVAL"
    Value   string // live value, formatted
    Default string // true compile-time default
    Doc     string // the flag's usage string
    Source  Source
}

func (c Config) Settings() []Setting
```

Source is derived the way `Parse` already resolves precedence: an explicitly
passed flag wins, then a non-empty environment variable, then the default.
"Why is this `:443`?" is the question an operator actually asks, and nothing in
the system answers it today.

**`Default` must be the true compile-time default.** Today `envOr` folds the
environment value into the default *before* the flag is defined, so
`flag.Flag.DefValue` reports the env value, not the default. The settings
descriptor list therefore carries its own default constants rather than reading
`DefValue`.

That duplication is the one real risk in this section, so it is closed by a
test rather than by care: **every flag in the `FlagSet` must appear in the
descriptor list, and every descriptor must name a real flag.** A new flag added
without a descriptor fails the build.

`Doc` comes from `fs.Lookup(name).Usage`, so usage strings stay single-sourced.

**No setting is redacted, because none is a secret**: every current setting is
an address, a path, a duration or a bool, and the platform already refuses to
accept a password as a flag. This invariant is stated in the package doc, so
that adding a credential-shaped setting makes redaction an obvious prerequisite
rather than an afterthought.

## 7. Jobs

`internal/platform/jobs` is a small, generic scheduler. It is a **leaf package
that imports nothing else in the module**: it takes closures, and knows nothing
about backups, sessions, SQLite or HTTP.

```go
type Registry struct{ ... }

func (r *Registry) Register(name, description string, every time.Duration, fn func(context.Context) error)
func (r *Registry) Run(ctx context.Context)     // one goroutine per job; returns when ctx is done
func (r *Registry) Snapshot() []Status          // mutex-guarded, safe from a request goroutine

type Status struct {
    Name, Description string
    Interval          time.Duration
    Enabled           bool          // false when Interval is zero
    Runs              int
    LastRun           time.Time     // zero means never
    LastDuration      time.Duration
    LastErr           string        // empty means the last run succeeded
    NextRun           time.Time
}
```

Status is in memory and resets on restart. For a single-process server that is
honest, and persisting run history is listed as out of scope in §12.

`runMaintenance` in [backup.go](../../../cmd/onsuite/backup.go) is replaced by
two registrations in `serve`:

- **sweep expired sessions** — `users.DeleteExpiredSessions`
- **database snapshot** — `writeSnapshot` into `cfg.BackupDir()`, keeping
  `cfg.BackupKeep`

Both keep their current semantics exactly: first run one interval after
startup (so restarting repeatedly does not fill the backups directory), every
failure logged and swallowed so a backup problem cannot take down a server
that is happily serving requests.

**One behaviour deliberately not changed.** `--backup-interval 0` today
disables `runMaintenance` entirely, including the session sweep, which
`onsuite backup` then takes over — as the comment in `backupCmd` says. Splitting
maintenance into two named jobs makes it tempting to give the sweep its own
schedule. It does not get one: both jobs register with the same interval, and
both render as **disabled** when it is zero. Same behaviour as today, now
visible. Changing it is a separate decision.

Apps cannot register jobs in this spec — no app needs one yet. The signature is
deliberately app-agnostic so that ON Paste's future snippet expiry becomes a
single `Register` call plus a line in `Deps`, with no change to this package.

## 8. Per-app stats

A new optional capability in `internal/platform/app`, mirroring `Exporter`:

```go
type Stat struct {
    Label string // "Snippets"
    Value string // "1,204" — preformatted by the app
    Hint  string // optional one-liner, may be empty
}

type Stater interface {
    Stats(ctx context.Context, handle *sql.DB) ([]Stat, error)
}

func (reg *Registry) Stats(ctx context.Context, handle *sql.DB) []AppStats

type AppStats struct {
    ID, Name string
    Stats    []Stat
    Err      string // this app's collector failed; the others still rendered
}
```

`Value` is a string so the app decides whether 1234567 reads as `1.2 MB` or
`1,234,567`. The platform renders label/value pairs and formats nothing.

It takes `*sql.DB` rather than `Mount`'s `Deps` for the same reason `Export`
does: it depends on the database, not on an HTTP stack being built.

Unlike `Registry.Export`, which fails the whole export if one app errors,
`Registry.Stats` captures each app's error on its own card and keeps going. An
export that is silently missing an app is corrupt; a dashboard missing one card
is merely a dashboard missing one card.

ON Paste implements it: total snippets, shared snippets, total size, largest
snippet, newest snippet.

## 9. Route recorder

The routes section is only worth having if it is complete. App routes are
already recorded by `Router.Routes()`; the platform's own routes (`/login`,
`/logout`, `/healthz`, `/static/`, `GET /{$}`, the `/` catch-all) are
registered straight onto the `ServeMux` and recorded nowhere. A section that
silently omits `/login` and `/static/` is worse than no section, because those
are the first places an auditor looks.

So the record moves down into `web`, where both layers can reach it:

```go
// package web
type Route struct {
    Pattern string // "GET /paste/{id}"
    Public  bool   // registered without the auth guard
    Owner   string // app id, or "platform"
}

type Recorder struct{ ... }
func (rec *Recorder) Add(Route)
func (rec *Recorder) Routes() []Route // sorted, stable
```

`app.Router` forwards each registration to the recorder it is given, and
`buildStack` registers its own routes through the same helper. `app.Route`
stays as an alias so existing tests and callers keep compiling.

There is then one source of truth: a route that is not recorded is a route that
is not served. This section is a one-screen audit of the claim that routing is
default-deny — currently a design assertion backed by a test, but invisible at
runtime.

## 10. Data flow

```
GET /admin/
  → Stack (recover, log, security headers, body cap, CSRF, LoadUser)
  → RequireUser  → anonymous: 303 /login
  → RequireAdmin → non-admin: 404
  → admin.Handler
      ├─ runtime + StartedAt      → Build & runtime
      ├─ cfg.Settings()           → Settings
      ├─ jobs.Snapshot()          → Jobs
      ├─ registry.Stats(ctx, db)  → Apps
      ├─ PRAGMA + schema_migrations + os.Stat → Database
      ├─ users.List / session counts → Users & sessions
      └─ recorder.Routes()        → Routes
  → render "admin" (always 200; per-section errors rendered in place)
```

Everything the handler needs is passed in at construction from `buildStack`:
config, job registry, app registry, `*sql.DB`, `*auth.Store`, route recorder,
version, start time. The handler holds no globals and opens no files at request
time beyond `os.Stat` on the database.

## 11. Testing

Following the conventions already in the repo — store tests against a real
SQLite file in a temp dir, handler tests through the real middleware stack,
`htmlassert` for structure.

| Unit | Tests |
|---|---|
| `config` | table-driven provenance (flag beats env beats default) for each setting; the both-directions test that the descriptor list and the `FlagSet` name the same flags |
| `jobs` | one run recording success, error and duration; the ticker loop at a short interval; `Snapshot` under `-race` while jobs run |
| `web` | `RequireAdmin` for all three requester classes; recorder ordering and the public flag |
| `admin` | `httptest` through the real `web.Stack`: anonymous → 303 to `/login`; non-admin → byte-identical to `/nope`; admin → 200 with every section present; one collector forced to fail still renders 200 with the other six |
| `paste` | `Stats` against a real SQLite file, including the empty-database case |
| `arch` | `jobs` and `admin` added to `TestScanSeesTheRealTree`; `jobs` added to `TestLayering`'s forbidden map, barred from importing `web`, `app`, `render`, `auth`, `db` and `config`. `admin` needs no `TestLayering` entry — it sits at the top of the platform and may import everything below it |

## 12. Out of scope

Each is a separate spec if it is ever wanted:

- Any control that changes state — trigger a backup, sweep sessions, create or
  promote a user, delete another user's data.
- Runtime-editable settings.
- A log tail or in-memory log ring buffer.
- Request or latency metrics.
- Persisted job run history.
- Apps contributing rendered HTML fragments rather than label/value pairs.
- Auto-refresh or streaming updates.
- A JSON API for any of this.

## 13. Documentation

- `AGENTS.md`: `Stater` documented beside `Exporter` in the optional-capability
  paragraph; `internal/platform/jobs` and `internal/platform/admin` added to the
  architecture section.
- `NEXT.md`: the admin-page item removed once shipped.
- `docs/DEPLOYING.md`: a sentence pointing operators at `/admin/` for verifying
  which settings a deployment actually picked up.
