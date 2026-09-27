# Architecture

*For a developer who has ON Suite running and wants to understand how it fits
together before changing it.*

## Design at a glance

- **One binary, one module.** Every app is compiled in; there is no plugin
  loading and no per-app deployment. An operator can switch apps off for a
  run with `-disable-apps` (`config.Config.DisabledApps`): a disabled app's
  routes are not mounted and its migrations are not applied.
- **The platform/app boundary is the whole design.** An app is a package
  under `internal/apps/` implementing `app.App`
  ([`internal/platform/app/app.go`](../../internal/platform/app/app.go)).
  Adding one means writing that package plus one registration line in
  `registeredApps()` in [`cmd/onsuite/main.go`](../../cmd/onsuite/main.go);
  a few tests pin the registered-app list too (see
  [Adding an app, step 5](adding-an-app.md#5-register-it)). The platform
  never imports an app and apps never import each other — an
  [architecture test](#package-layering) walks the import graph and fails
  the build if either rule breaks.
- **Optional capabilities are discovered, not declared.** An app that
  implements `Exporter` takes part in `onsuite export`; `Stater` gives it a
  card on the admin page; `Scheduler` registers background jobs. Apps that
  don't implement one are silently skipped.
- **One SQLite file** with WAL and a single connection — see
  [Storage](#storage).
- **Forward-only migrations**, embedded with `go:embed` and applied at
  startup. Each app numbers its own under its own namespace (`paste:0001`),
  so apps never coordinate schema changes.
- **A route is private unless it opts out.** `Router.Handle` requires a
  signed-in user; anonymous access means calling `Router.Public`, which is a
  visible, greppable decision.
- **Argon2id passwords, invite-only accounts**, sessions with throttled
  sliding expiry — see [Authentication](#authentication).
- **A strict Content-Security-Policy.** No inline script or style anywhere;
  HTMX works under it because `hx-*` are plain HTML attributes.
- **Background work goes through one scheduler.** `internal/platform/jobs`
  runs named jobs on an interval, remembers how each run went, and can run
  one on demand. The platform registers two (session sweep, database
  snapshot); apps add their own through `Scheduler`.
- **Operations pages are platform packages, not apps**, because they report
  on the platform itself: `admin` (read-only dashboard at `/admin/`),
  `usermgmt` (`/admin/users` and `/account`) and `jobsadmin`
  (`/admin/jobs`, with a **Run now** button). The admin routes answer a
  non-admin with the same 404 as a URL that doesn't exist.

## How a request flows

Everything is wired once, at startup, in `buildStack`
([`cmd/onsuite/stack.go`](../../cmd/onsuite/stack.go)). Following one request
to `GET /paste/12`:

```text
browser
  │
  ▼  web.Stack (internal/platform/web/middleware.go), outermost first
  ├─ Recover           a panic becomes the 500 page, not a dropped connection
  ├─ RequestLog        one JSON log line per request
  ├─ SecurityHeaders   CSP, nosniff, Referrer-Policy, X-Frame-Options
  ├─ body limit        1 MiB default; a route can raise it (RegisterBodyLimit)
  ├─ CSRF              issues the token; verifies it on unsafe methods
  └─ LoadUser          session cookie → user in the request context (never blocks)
  │
  ▼  http.ServeMux
  │    /healthz, /static/, /login, /logout    platform, public
  │    /admin/…, /account                     platform pages, guarded
  │    /paste/…, /notes/…, …                  one Router per app
  │
  ▼  app.Router wrapping (internal/platform/app/router.go)
  ├─ RequireUser       unless the route was registered with Public:
  │                    anonymous → 303 to /login (HX-Redirect over HTMX)
  └─ active app        records "paste" so the shell highlights it
  │
  ▼  app handler (internal/apps/paste/handlers.go)
  │    deps.Page(r, title)            shell pre-filled: user, CSRF token, nav, theme
  │    page.Data = view model
  │    deps.Render.Page(w, 200, "paste/index", page)
  │
  ▼  render.Renderer (internal/platform/render/render.go)
       executes internal/ui/templates/base.html, which pulls in the
       page's own {{define "content"}} (and optional "head") block
```

A handler never touches the chrome: `Deps.Page` fills in the whole
`render.Shell`, and the handler sets only `Data`. Anything the handler
needs — the database, renderer, user store, error pages, logger — arrives in
`app.Deps` when the app is mounted.

Templates are parsed once at startup, so a broken template is a startup
failure rather than a 500 a user finds. Each page is its own template set:
`base.html` plus the page's file plus that app's `*.partial.html` files. Pages
are named `<app id>/<file name>` — `paste/index` is
`internal/apps/paste/templates/index.html`.

## HTMX and the Content-Security-Policy

HTMX is vendored at `internal/ui/static/htmx.min.js` (see `VENDOR.md` next
to it) and loaded by `base.html` together with three small platform scripts:
`theme.js`, `htmx-notices.js` and `connectivity.js`. Apps that need
behaviour HTMX can't express ship one script of their own —
`notes.js`, `reader.js`, `flash.js` — embedded and served from a route in
the app.

For an HTMX request, a handler usually renders one named block instead of
the whole page, with `Render.Fragment(w, status, "paste/index",
"detail-with-list", view)`. Blocks that update other parts of the page ride
along with `hx-swap-oob`. `web.IsHTMX(r)` tells the two cases apart.

The policy set by `SecurityHeaders` is:

```text
default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self';
font-src 'self'; connect-src 'self'; form-action 'self';
frame-ancestors 'none'; base-uri 'none'; object-src 'none'
```

In practice:

- No `<script>` blocks, no `onclick=` and friends, no `style=` attributes.
  Styles go in `internal/ui/static/app.css`; behaviour goes in a `.js` file.
- Everything loads from the same origin — fonts are self-hosted, and ON
  Reader proxies article images and favicons through its own routes.
- Static files are served with a content hash in the URL (the `asset`
  template function), so they can be cached forever and change on deploy.

CSRF uses the double-submit-cookie pattern: a random token in an HttpOnly
cookie, repeated by the page. HTMX sends it in an `X-CSRF-Token` header (set
once with `hx-headers` on `<body>`); plain forms send it in a hidden field
named by the `csrfField` template function.

## Storage

One SQLite file, `<data-dir>/onsuite.db`, opened by
[`internal/platform/db/db.go`](../../internal/platform/db/db.go) with:

- `journal_mode=WAL` — readers never block the writer.
- `busy_timeout=5000` — wait up to five seconds for a lock instead of failing.
- `foreign_keys=ON` — SQLite leaves enforcement off by default.
- `SetMaxOpenConns(1)` — a single connection. It gives up write concurrency
  this deployment never needs and removes "database is locked" as a class of
  bug.

Migrations live in each owner's `migrations/` directory as
`0001_some_name.sql`, are collected with `db.Collect(namespace, fsys)` and
recorded in `schema_migrations` under keys such as `platform:0001` or
`paste:0002`. `db.Apply` runs each pending one in its own transaction with
the row that records it. They only ever go forward.

Every table an app owns starts with its app id (`paste_snippets`,
`reader_items`), and every column referencing a user uses
`ON DELETE CASCADE`, so deleting an account removes everything it owned — a
test in `cmd/onsuite` checks the whole schema for that.

Timestamps are stored as TEXT in `db.TimeLayout`: UTC, always nine
fractional digits. Write them with `db.FormatTime` and read them with
`db.ParseTime`. The fixed width is what makes `ORDER BY` and `<` on the text
agree with time order; never store a `time.RFC3339Nano` string, which trims
trailing zeros. App code reads the clock only through its store's `now()`,
so tests can pin it — both rules are arch tests.

## Authentication

- **Argon2id** password hashing (`m=64MiB, t=3, p=4`), stored as a PHC string
  that carries its own parameters
  ([`internal/platform/auth/password.go`](../../internal/platform/auth/password.go)).
  The minimum length is 12 characters.
- **Invite-only.** There is no sign-up page. Accounts come from
  `onsuite user add` or an admin at `/admin/users`, and at least one admin
  always remains.
- **Sessions** are random 256-bit IDs in a cookie. A session lives 30 days
  from last use and is renewed at most once a day, so an active user costs
  one write per day rather than one per request
  ([`internal/platform/auth/session.go`](../../internal/platform/auth/session.go)).
- **Login** gives a wrong password and an unknown username byte-identical
  responses, so it can't be used to discover accounts, and repeated failures
  are rate-limited.

## Package layering

The import rules are enforced by
[`internal/arch/arch_test.go`](../../internal/arch/arch_test.go). Arrows point
from a package to what it imports (simplified: indirect arrows are left
out, and the notes say what each package must *not* import):

```text
cmd/onsuite                     wires everything; the only importer of apps
   │
   ├──► internal/apps/{flash,notes,paste,reader}
   │         never import each other; import only platform/* and ui
   │
   ├──► admin · usermgmt · jobsadmin      platform pages
   │         │
   ▼         ▼
   app       App interface, Router, Registry, Deps
   │
   ▼
   web       middleware, CSRF, login, assets     (must not import app)
   │
   ├──► render ──► ui    render must not import web, app or auth
   │                     ui is a leaf: embedded files only
   ▼
   auth ──► db           auth must not import web, app or render
                         db must not import web, app, render or auth

   config, jobs          leaves: import nothing else in the module
```

`TestLayering` holds those rules, `TestAppsDoNotImportEachOther` and
`TestPlatformDoesNotImportApps` hold the boundary, and `TestUIIsALeaf`,
`TestHTMLAssertIsTestOnly` and `TestAppTestIsTestOnly` keep the leaf and
test-only packages honest. [Testing](testing.md#the-architecture-test) lists
the rest.

**Next:** [Repository layout](repository-layout.md)
