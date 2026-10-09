# AGENTS.md

This file provides guidance to AI coding agents when working with code in this repository.

## What this is

ON Suite is a self-hosted suite of small web apps for one household: one
account system, one shell, one SQLite file, one Go binary. It's deliberately
not built for SaaS scale — no multi-tenancy, no CGO, no Node/npm/JS build
step. **ON Paste** (snippets with syntax highlighting and shareable links),
**ON Notes** (a hierarchical outliner, still being built out), **ON
Reader** (a feed reader, with search, OPML import/export, full-article
extraction, and reading-stats), **ON Later** (a read-it-later app: save
pages, read them in a calm view, highlight, comment and tag them, export as
Markdown), **ON Books** (a private reading log: shelves, readings, tags,
Open Library search, progress, ratings and reviews, notes and quotes, and
full-text search, with stats to come),
**ON Flash** (flash cards with FSRS review, import from
AI-written Markdown/JSON, media, household sharing and stats), and **ON
Focus** (saved, reusable focus timers — single blocks or Pomodoro-style
intervals — with a calm running view and a history of focused time) are all
registered today.

Full rationale for every design choice below lives in
[docs/superpowers/specs/2026-08-18-on-suite-platform-design.md](docs/superpowers/specs/2026-08-18-on-suite-platform-design.md).

## Workflow

`main` is protected — always work on a branch and open a PR, never commit or
push directly to `main`.

Agents open PRs but never merge them. Agent sessions run as the
`iliafrenkel-claude` GitHub account, and a human reviews, approves and
merges every PR. Don't run `gh pr merge`, enable auto-merge, or approve
your own PRs.

Before writing new code, check [PATTERNS.md](PATTERNS.md) — an index of
recurring patterns this codebase already has a deliberate answer for
(error-surfacing, view-model projection, store test conventions, and more).
Reuse the pattern it points to rather than inventing a new shape; add an
entry there when you notice yourself reusing (or wanting) one it doesn't
list yet.

## Commands

Build:
```bash
go build ./cmd/onsuite
```

Full check (must stay green on every commit; mirrors CI in
[.github/workflows/ci.yml](.github/workflows/ci.yml)):
```bash
gofmt -l .                                              # must print nothing
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...  # pinned version, not @latest
go mod tidy && git diff --exit-code go.mod go.sum
go test ./... -race -count=1
```

Run a single test:
```bash
go test ./internal/apps/paste/... -run TestName -v
```

Cross-compile (no CGO to worry about):
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o onsuite ./cmd/onsuite
```

Local run:
```bash
./onsuite user add ilia --admin --data-dir ./data   # first account, prompts for password
./onsuite user reset-password ilia --data-dir ./data  # recovery; prompts for the new password
./onsuite serve --data-dir ./data
```
Every `serve` flag has an `ONSUITE_*` env equivalent. `./onsuite help` lists
all commands (`serve`, `user`, `export`, `backup`, `version`).

## Architecture

**The platform/app boundary is the whole design.** An app is a package under
`internal/apps/` implementing `app.App` ([internal/platform/app/app.go](internal/platform/app/app.go)):
`Meta()`, `Migrations() fs.FS`, `Mount(r *Router, deps Deps)`. Adding an app
means writing that package plus one registration line in
`registeredApps()` in [cmd/onsuite/main.go](cmd/onsuite/main.go); a few
tests pin the registered-app list and need updating too (see
[docs/developers/adding-an-app.md](docs/developers/adding-an-app.md), step
5). Two rules are enforced by a test, not just convention, in
[internal/arch/arch_test.go](internal/arch/arch_test.go):
- **Apps never import each other.**
- **The platform never imports an app.** Apps import only `internal/platform/*`
  and `internal/ui`.
- A stricter layering rule within the platform: `render` can't reach for
  `web`/`app`/`auth`; `auth`, `db`, `config` can't reach upward either. This
  keeps each layer testable without pulling in the layers above it.
- `internal/ui` must be a leaf (embedded CSS/JS/templates only, no imports).
- `internal/htmlassert` (HTML structure assertions) is test-only; production
  code importing it fails the build.

If you touch package-level imports anywhere under `internal/`, run
`go test ./internal/arch/...` — it will tell you immediately if you crossed
a boundary the design depends on.

**Everything an app gets is in `app.Deps`**: `*sql.DB`, `*render.Renderer`,
`*auth.Store`, `*web.Errors`, a logger, the version string. `Deps.Page(r, title)`
builds the page shell (nav, CSRF token, logged-in user, theme) so a handler
never touches chrome — it only sets `Data` on the returned `render.Page`.

**Routing is default-deny.** Every app gets a `Router` whose `Handle`
requires a signed-in user; making a route anonymous requires calling
`Public` explicitly ([internal/platform/app/router.go](internal/platform/app/router.go)) — greppable, not
implicit.

**Storage**: one SQLite file, `journal_mode=WAL`, `busy_timeout=5000`,
`foreign_keys=ON`, `SetMaxOpenConns(1)` — trades away write concurrency this
deployment never needs to eliminate "database is locked" entirely
([internal/platform/db/db.go](internal/platform/db/db.go)). Migrations are forward-only, embedded
with `go:embed`, applied in a transaction at startup; each app owns and
numbers its own migrations under its own namespace (e.g. `paste:0001`) via
`db.Collect`, so apps never coordinate schema changes. Store-layer tests run
against a real SQLite file in a temp dir, not `:memory:`, because the bugs
that matter live in SQL and WAL behavior `:memory:` doesn't reproduce.
Timestamps are TEXT in `db.TimeLayout` — UTC, always nine fractional
digits, 30 characters — written with `db.FormatTime` and read with
`db.ParseTime` ([internal/platform/db/timefmt.go](internal/platform/db/timefmt.go));
fixed width is what makes `ORDER BY`, `<`, `min()` and `max()` on the text
agree with time order, including within one second (#356). Never store a
time formatted with `time.RFC3339Nano`, which trims trailing zeros;
`TestRFC3339IsContained` in the arch test enforces that. Date-only columns
use `"2006-01-02"`. A migration that adds a timestamp column needs nothing
special; one that backfills one must write the same 30-character form.
Handler tests control time with `s.Clock` (`apptest.Clock`): `s.Clock.Set(t)`
/ `s.Clock.Advance(d)` pins the clock for both the app under test and
`s.Store`. Never compute "now" from the real clock to line up with a
handler, and don't call `s.Store.SetClock` in a handler test — it doesn't
reach the app. App code reads time only through its Store's `now()` —
never `time.Now`, `time.Since`, or `time.Until` directly (arch test
`TestAppsReadTheirStoreClock`).

**Auth**: Argon2id password hashing, invite-only accounts (no public
registration), sessions with throttled sliding expiry (30-day lifetime,
renewed at most once/day). Login responses are byte-identical for a wrong
password vs. an unknown username, so failed logins can't be used to
enumerate accounts. Passwords are never accepted as a CLI flag — `onsuite user
add` reads from a terminal with echo disabled, or from stdin for scripted
setup.

**Frontend**: HTMX (vendored under `internal/ui/static/`, never loaded from
a CDN) plus a handful of small hand-written platform scripts
(`internal/ui/static/*.js`) and per-app scripts (`internal/apps/*/static/`)
is the whole of the JS — all served as plain files, embedded, no build
step. A strict CSP forbids inline `<script>` and `style=` anywhere, which
HTMX satisfies since `hx-*` are plain HTML attributes. Templates are Go
`html/template`, composed by `internal/platform/render`.

**Optional app capabilities are discovered by type assertion, not interface
bloat**: an app implementing `Templates() fs.FS` gets its templates mounted;
one implementing `Exporter` (`Export(ctx, db, userID) (any, error)`)
participates in `onsuite export` automatically; one implementing `Stater`
(`Stats(ctx, db) ([]app.Stat, error)`) gets a card on the admin page. Apps
that don't implement these are silently skipped — that's a design choice.

**Several platform packages exist only for operations.**
[internal/platform/jobs](internal/platform/jobs/jobs.go) is a generic interval
scheduler that remembers how each run went and can run any job on demand
(`Trigger`); it takes closures and imports nothing else in the module, so it
never learns what a backup is.
[internal/platform/admin](internal/platform/admin/admin.go) is the read-only
admin page at `/admin/`, guarded by `Auth.RequireAdmin` — a signed-in
non-admin gets the same 404 as a URL that does not exist. It is a platform
page rather than an app because it reports *on* the platform. Its design is in
[docs/superpowers/specs/2026-08-24-admin-page-design.md](docs/superpowers/specs/2026-08-24-admin-page-design.md).

[internal/platform/usermgmt](internal/platform/usermgmt/usermgmt.go) is the
admin page's writable sibling: `/admin/users` (admin-only, same 404 guard)
adds, deletes, promotes/demotes and resets users with one-time generated
passwords, and `/account` lets anyone change their own password. `auth.Store`
enforces that at least one admin always remains (`ErrLastAdmin`). Its design
is in
[docs/superpowers/specs/2026-09-27-user-management-design.md](docs/superpowers/specs/2026-09-27-user-management-design.md).

[internal/platform/jobsadmin](internal/platform/jobsadmin/jobsadmin.go) is the
other writable sibling: `/admin/jobs` (admin-only, same 404 guard) lists every
job with a **Run now** button, runs it in the background, and polls the table
with HTMX until it finishes. Its design is in
[docs/superpowers/specs/2026-09-27-trigger-jobs-design.md](docs/superpowers/specs/2026-09-27-trigger-jobs-design.md).

**Three platform packages are shared web-content plumbing.**
[internal/platform/webfetch](internal/platform/webfetch/webfetch.go) is
the only way an app should fetch publisher-controlled URLs (SSRF guard at
dial time, redirect and size caps, image sniffing, the retry rule);
[internal/platform/article](internal/platform/article/extract.go) is
readability extraction plus the sanitiser and image rewriting;
[internal/platform/favicon](internal/platform/favicon/favicon.go) finds a
site's icon URL. They are leaves (pinned by `TestWebContentPackagesAreLeaves`) and a
deliberate exception to cross-app mirroring: this code is large and
security-critical, so two drifting copies would be worse than one shared
package. ON Flash still has its own mirrored fetch client.

## Commit messages

Subjects follow [Conventional Commits](https://www.conventionalcommits.org/):
`type(scope): summary`, e.g. `fix(notes): trim search query before it
reaches the template`. Scope is usually an app name (`notes`, `paste`) or
`platform`; omit it for repo-wide changes. `type` decides which release
notes section a commit lands in — see
[CONTRIBUTING.md](CONTRIBUTING.md#commit-messages) for the full
type-to-section table and the `feat`-vs-`refactor` distinction.

## Issues

Follow-ups and bugs are tracked as GitHub issues. Titles start with the area,
e.g. `reader: Mark all read ignores the active search`. Give every issue
three labels:

- **Type:** one of `bug`, `feature`, `chore`, `documentation` (add
  `accessibility` too when it applies).
- **Priority:** `priority: high` (do next), `priority: medium` (do soon) or
  `priority: low` (nice to have).
- **Effort:** `effort: small` (about an hour), `effort: medium` (about a
  session) or `effort: large` (several sessions).

Use `wontfix` when closing something that won't be done. There are no area
labels — the title prefix covers that.

## Constraints (from CONTRIBUTING.md)

- No CGO, ever — every dependency must be pure Go.
- No Node, no npm, no JS build step.
- Platform dependencies are capped by design (currently
  `modernc.org/sqlite`, `golang.org/x/crypto`, `golang.org/x/term`,
  `golang.org/x/net`, `github.com/alecthomas/chroma/v2`,
  `github.com/mmcdole/gofeed`, `github.com/microcosm-cc/bluemonday`,
  `github.com/go-shiori/go-readability` (contained to
  `internal/platform/article`),
  `github.com/open-spaced-repetition/go-fsrs/v4`, `github.com/yuin/goldmark`
  — the last one contained to `internal/platform/help`, enforced by
  `TestGoldmarkIsContained`); adding a new one is a spec change, not just an
  implementation detail.
- Migrations are forward-only — no down migrations.
- This is a personal project with an opinionated design — open an issue or
  discussion before a large PR, especially anything touching a dependency,
  a build step, or the platform/app boundary.

## Other docs worth knowing about

- [PATTERNS.md](PATTERNS.md) — index of recurring, deliberate patterns; check it before writing new code.
- [docs/user/](docs/user/index.md) — end-user guides, one per app plus admin,
  served at `/help` (`internal/platform/help`). A UI change updates the
  matching `docs/user/<app>.md` and re-shoots the affected screenshots
  ([docs/screenshots/README.md](docs/screenshots/README.md)); the guides
  stay plain Markdown — no raw HTML, no `../` links. The guides are
  compiled into the binary, so a change needs a rebuild before it shows up
  at `/help`.
- [docs/self-hosting/deploying.md](docs/self-hosting/deploying.md) — systemd, TLS, backups, upgrades, Docker.
- [docs/self-hosting/running-locally.md](docs/self-hosting/running-locally.md) — running it on your own computer with an OS launcher.
- [docs/developers/](docs/developers/index.md) — the developer guide:
  [getting started](docs/developers/getting-started.md),
  [architecture](docs/developers/architecture.md),
  [repository layout](docs/developers/repository-layout.md),
  [adding an app](docs/developers/adding-an-app.md),
  [testing](docs/developers/testing.md) and
  [releasing](docs/developers/releasing.md).
- [docs/superpowers/](docs/superpowers/) — historical design specs and task-by-task implementation plans.
