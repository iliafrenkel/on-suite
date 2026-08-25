# AGENTS.md

This file provides guidance to AI coding agents when working with code in this repository.

## What this is

ON Suite is a self-hosted suite of small web apps for one household: one
account system, one shell, one SQLite file, one Go binary. It's deliberately
not built for SaaS scale — no multi-tenancy, no CGO, no Node/npm/JS build
step. **ON Paste** (snippets with syntax highlighting and shareable links) and
**ON Notes** (a hierarchical outliner, still being built out) are registered
today; ON Reader and ON Flash are reserved names with no code yet.

Full rationale for every design choice below lives in
[docs/superpowers/specs/2026-08-18-on-suite-platform-design.md](docs/superpowers/specs/2026-08-18-on-suite-platform-design.md).

## Workflow

`main` is protected — always work on a branch and open a PR, never commit or
push directly to `main`.

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
./onsuite serve --data-dir ./data
```
Every `serve` flag has an `ONSUITE_*` env equivalent. `./onsuite help` lists
all commands (`serve`, `user`, `export`, `backup`, `version`).

## Architecture

**The platform/app boundary is the whole design.** An app is a package under
`internal/apps/` implementing `app.App` ([internal/platform/app/app.go](internal/platform/app/app.go)):
`Meta()`, `Migrations() fs.FS`, `Mount(r *Router, deps Deps)`. Adding an app
means writing that package and adding one line to `registeredApps()` in
[cmd/onsuite/main.go](cmd/onsuite/main.go) — nothing else changes. Two rules
are enforced by a test, not just convention, in
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

**Auth**: Argon2id password hashing, invite-only accounts (no public
registration), sessions with throttled sliding expiry (30-day lifetime,
renewed at most once/day). Login responses are byte-identical for a wrong
password vs. an unknown username, so failed logins can't be used to
enumerate accounts. Passwords are never accepted as a CLI flag — `onsuite user
add` reads from a terminal with echo disabled, or from stdin for scripted
setup.

**Frontend**: HTMX (vendored under `internal/ui/static/`, never loaded from
a CDN) is the only JS. A strict CSP forbids inline `<script>` and `style=`
anywhere, which HTMX satisfies since `hx-*` are plain HTML attributes.
Templates are Go `html/template`, composed by `internal/platform/render`.

**Optional app capabilities are discovered by type assertion, not interface
bloat**: an app implementing `Templates() fs.FS` gets its templates mounted;
one implementing `Exporter` (`Export(ctx, db, userID) (any, error)`)
participates in `onsuite export` automatically; one implementing `Stater`
(`Stats(ctx, db) ([]app.Stat, error)`) gets a card on the admin page. Apps
that don't implement these are silently skipped — that's a design choice.

**Two platform packages exist only for operations.**
[internal/platform/jobs](internal/platform/jobs/jobs.go) is a generic interval
scheduler that remembers how each run went; it takes closures and imports
nothing else in the module, so it never learns what a backup is.
[internal/platform/admin](internal/platform/admin/admin.go) is the read-only
admin page at `/admin/`, guarded by `Auth.RequireAdmin` — a signed-in
non-admin gets the same 404 as a URL that does not exist. It is a platform
page rather than an app because it reports *on* the platform. Its design is in
[docs/superpowers/specs/2026-08-24-admin-page-design.md](docs/superpowers/specs/2026-08-24-admin-page-design.md).

## Constraints (from CONTRIBUTING.md)

- No CGO, ever — every dependency must be pure Go.
- No Node, no npm, no JS build step.
- Platform dependencies are capped by design (currently
  `modernc.org/sqlite`, `golang.org/x/crypto`, `golang.org/x/term`,
  `golang.org/x/net`); adding a new one is a spec change, not just an
  implementation detail.
- Migrations are forward-only — no down migrations.
- This is a personal project with an opinionated design — open an issue or
  discussion before a large PR, especially anything touching a dependency,
  a build step, or the platform/app boundary.

## Other docs worth knowing about

- [docs/DEPLOYING.md](docs/DEPLOYING.md) — systemd, TLS, backups, upgrades, Docker.
- [docs/RELEASING.md](docs/RELEASING.md) — cutting a tagged release.
- [docs/superpowers/plans/](docs/superpowers/plans/) — task-by-task implementation plans per build phase.
