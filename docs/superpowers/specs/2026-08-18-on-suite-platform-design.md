# ON Suite — Platform + ON Paste Design

**Date:** 2026-08-18
**Status:** Approved
**Scope:** Project 0 (Platform) and Project 1 (ON Paste)

## 1. Purpose

ON Suite is a self-hosted suite of small web applications for personal use by
the author and a handful of family and friends. Each app carries the "ON"
prefix: ON Notes, ON Paste, ON Reader, ON Flash. More apps will follow.

This document specifies the shared platform and the first app built on it. It
does not specify the features of ON Notes, ON Reader or ON Flash; each gets its
own spec later.

## 2. Constraints

These were decided during brainstorming and drive every other choice here.

| Constraint | Decision |
|---|---|
| Identity | One account and one shell across the whole suite. |
| Users | Author plus family and friends. Real per-user data isolation. No public sign-up. |
| Deployment | A single Go binary and a data directory. No Docker required, no external services required. |
| Connectivity | Always-online. No offline editing, no local-first sync. |
| Database | One SQLite file. |
| Runtime modularity | One binary with all apps compiled in and always enabled. |
| Frontend toolchain | Go only. No Node, no npm, no JavaScript build step. |
| Testing | Tests written after features, but comprehensive. Auth is the exception and is tested first. |

## 3. Decomposition

ON Suite is five projects, not one.

| # | Project | Purpose |
|---|---|---|
| 0 | Platform | Config, database, migrations, auth, routing, layout, deployment. No user-facing features. |
| 1 | ON Paste | First app. Validates the platform. |
| 2 | ON Notes | Hierarchical outliner. The hardest of the four. |
| 3 | ON Reader | RSS reader. Introduces background jobs and outbound HTTP. |
| 4 | ON Flash | Flash cards. Introduces scheduled and time-based logic. |

Projects 0 and 1 are built together, because a platform with no app on it
cannot be validated. Projects 2 to 4 each get their own spec and plan.

**Design goal:** adding an app requires no changes to the platform. If ON Notes
or ON Reader forces a platform change, the platform got something wrong and
that is worth treating as a defect rather than as normal growth.

## 4. Repository organisation

One repository, one Go module, one binary. Module path
`github.com/iliafrenkel/on-suite`.

Separate repositories are rejected: a single binary linking four apps that share
a session and a database would require publishing four Go modules and
version-pinning the author's own code against itself, which is all the friction
of microservices with none of the benefit.

```
on-suite/
├── go.mod
├── cmd/onsuite/main.go        # flags, wiring, app registry, graceful shutdown
├── internal/
│   ├── platform/
│   │   ├── config/            # flags + env -> Config
│   │   ├── db/                # SQLite open, pragmas, migration runner
│   │   ├── auth/              # users, sessions, argon2id, middleware
│   │   ├── web/               # router, CSRF, logging, recover, error pages
│   │   ├── render/            # template registry, layout, HTMX helpers
│   │   └── app/               # the App interface and Deps struct
│   ├── ui/
│   │   ├── static/            # css, htmx.js, icons        (go:embed)
│   │   └── templates/         # base layout, app switcher  (go:embed)
│   └── apps/
│       ├── paste/             # app.go, handlers.go, store.go,
│       │                      # migrations/, templates/    (go:embed)
│       ├── notes/
│       ├── reader/
│       └── flash/
└── docs/
```

### 4.1 The App interface

```go
// internal/platform/app
type App interface {
    Meta() Meta                              // id, display name, icon, nav entries
    Migrations() fs.FS                       // this app's own .sql files
    Mount(mux *http.ServeMux, deps Deps)     // register routes under its prefix
}
```

`Deps` provides a `*sql.DB`, a renderer, a `*slog.Logger`, and auth helpers.
Nothing else. An app that needs something not in `Deps` is either doing
something the platform should own, or something it should own privately.

### 4.2 Boundary rules

1. Apps never import other apps.
2. Apps import only `internal/platform/*` and `internal/ui`.
3. The platform never imports an app.

These are enforced by a test that walks the import graph and fails on
violation. A convention that is not mechanically checked is not a boundary.

### 4.3 Registration

Apps are registered in one explicit slice in `cmd/onsuite/main.go`. No `init()`
side effects, so the contents of the binary can be determined by reading one
file. Adding an app is: write the package, add one line.

## 5. Technology stack

| Concern | Choice | Rationale |
|---|---|---|
| Language | Go 1.26 | Static self-hosted binary. |
| Routing | stdlib `net/http.ServeMux` | Method and wildcard patterns since Go 1.22. No dependency. |
| Templates | stdlib `html/template` | Context-aware escaping, no codegen step. |
| Interactivity | HTMX 2.x, vendored and embedded | A self-hosted binary must not need the public internet to render a page. Never loaded from a CDN. |
| CSS | Hand-written, CSS custom properties | One `app.css`, tokens at `:root`, dark mode via `prefers-color-scheme`. No framework, no build step. |
| Client-side state | Alpine.js, vendored — deferred | HTMX covers server round-trips, not pure-client state such as keyboard navigation. Not introduced until an app needs it; ON Paste does not. |
| DB driver | `modernc.org/sqlite` | Pure Go, no CGO, so `GOOS=linux GOARCH=arm64 go build` from macOS produces a genuinely static binary. Supports FTS5, which Notes and Reader will want. |
| Password hashing | `golang.org/x/crypto/argon2` | Argon2id. |
| Terminal input | `golang.org/x/term` | Reads a password from a terminal without echoing it, for `onsuite user add`. Pure Go, no CGO. |
| Logging | stdlib `log/slog` | Structured JSON to stdout, collected by journald. |

Platform dependencies total three, all of them pure Go and CGO-free:
`modernc.org/sqlite`, `golang.org/x/crypto` and `golang.org/x/term`.
Per-app dependencies are added only when that app is built:
`alecthomas/chroma` for ON Paste syntax highlighting, `mmcdole/gofeed` for
ON Reader. `golang.org/x/net` is a test-only dependency used to parse HTML in
handler tests and does not count against this budget.

The build is `go build ./cmd/onsuite`. There is nothing else to install.

**Rejected: templ.** It is genuinely better than `html/template` — type-safe
templates that pair well with HTMX. It is excluded because it adds a codegen
step and a generated-file-versus-source question to a project whose main virtue
is that `go build` is the entire build. If `html/template` proves frustrating
while building ON Paste, migrating is a contained change.

## 6. Data and storage

One SQLite file. The data directory is the complete persistent state of the
system.

```
/var/lib/onsuite/
├── onsuite.db          # plus -wal and -shm
└── backups/
    └── onsuite-2026-08-18T03:00Z.db
```

### 6.1 Connection handling

Opened with `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`, and
`SetMaxOpenConns(1)`.

Serialising writes costs nothing measurable at this scale and eliminates
`database is locked` failures entirely. If write throughput ever becomes a
bottleneck, the fix is a separate read-only connection pool, which is a
contained change inside `platform/db`.

### 6.2 Migrations

Numbered `.sql` files, embedded with `go:embed`, applied at startup inside a
transaction, recorded in a `schema_migrations` table.

The platform owns migration 1: `users` and `sessions`. Each app ships its own
migrations, recorded under its app id — `paste/migrations/0001_snippets.sql` is
recorded as `paste:0001`. An unregistered app never touches the schema.

Migrations are forward-only. There are no down migrations; recovery is
restoring a backup, which is a copy of one file.

### 6.3 Table naming

App tables are prefixed with the app id: `paste_snippets`, `notes_nodes`,
`reader_feeds`. One file, no collisions, and `user_id` still joins cleanly to
the shared `users` table.

### 6.4 Backup

Backup is a platform feature, not an operational afterthought. Copying the file
with the `sqlite3` CLI is not safe against a live writer, so the binary does it
itself using `VACUUM INTO`, which produces a consistent snapshot with no
downtime.

Exposed as `onsuite backup`, and as an optional internal nightly job writing
into `backups/` with a retention count. Restore is: stop the service, copy a
file back, start the service.

### 6.5 Export

Each app may implement an export to plain JSON. The data belongs to the user
and reading it should never require this code to be running.

## 7. Authentication and the shell

### 7.1 Accounts

Invite-only. There is no public registration page; for a handful of known
users it is attack surface with no benefit.

- Bootstrap via CLI: `onsuite user add ilia --admin`. This avoids the
  chicken-and-egg first-run setup wizard — the first account exists before the
  server listens.
- **Password entry never puts a plaintext password in shell history or in the
  process table.** The command prompts twice on the terminal with echo
  disabled. When standard input is not a terminal it reads one line instead,
  so `onsuite user add ilia < secret.txt` works for scripted setup. There is
  no `--password` flag, because a flag value is visible in `ps` output and in
  shell history.
- Passwords must be at least 12 characters. Length is the only rule;
  composition requirements push users toward worse passwords.
- Subsequent accounts: the same command, or an admin-only page that mints a
  single-use invite link the recipient redeems by choosing their own password.

### 7.2 Sessions

- Argon2id password hashes.
- Session cookie: `HttpOnly`, `Secure`, `SameSite=Lax`, opaque 256-bit random
  identifier.
- Sessions stored in SQLite with a sliding expiry. Logout deletes the row, so
  sessions are genuinely revocable.
- Rate limiting on the login endpoint from day one.

### 7.3 CSRF

`SameSite=Lax` covers most cases but not all, so a per-session token is checked
on every non-GET request. A single `hx-headers` attribute on `<body>` attaches
it to every HTMX request in the suite. The middleware lives in `platform/web`;
apps never handle CSRF themselves.

### 7.4 Public routes

Anonymous read access is a first-class platform concept, not something an
individual app invents. An app declares its public routes when it mounts; the
auth middleware skips exactly those routes and no others.

ON Paste needs this for unguessable share links. ON Notes will need the same
mechanism for shared outlines.

### 7.5 The shell

The platform owns one base layout: a top bar with the ON Suite mark, an app
switcher listing registered apps, and the current-user menu, plus a slot each
app fills with its own navigation. Apps cannot redefine the frame, which is
what makes the suite feel consistent.

### 7.6 URLs

Single domain, path-based: `https://on.example.com/paste/abc123`, `/notes/`,
`/reader/`, `/flash/`.

Subdomains are rejected: they require a wildcard certificate and cookie-domain
configuration and buy nothing here. One certificate, one cookie, one origin, no
CORS.

## 8. Deployment and operations

```
GOOS=linux GOARCH=arm64 go build -o onsuite ./cmd/onsuite
scp onsuite server:/usr/local/bin/
onsuite serve --data-dir /var/lib/onsuite --addr :8080
```

- **Configuration:** flags, each with an `ONSUITE_*` environment equivalent. No
  configuration file until one is needed.
- **TLS, two supported paths:**
  1. Behind Caddy or nginx handling termination. This is the recommended setup.
  2. `--tls-domain on.example.com`, where the binary obtains its own Let's
     Encrypt certificate via `golang.org/x/crypto/acme/autocert`. This exists so
     that "one binary, no other software" is literally true for anyone who
     wants it that way.
- **systemd unit** shipped in `docs/`, using `DynamicUser`, `ProtectSystem=strict`
  and a single `ReadWritePaths` entry for the data directory.
- **Graceful shutdown** on SIGTERM: stop accepting connections, drain in-flight
  requests, checkpoint the WAL, close the database.
- **Releases** via goreleaser: cross-compiled binaries with checksums for
  linux/amd64, linux/arm64 and darwin/arm64.
- **Dockerfile** provided as an option. A `scratch`-based image is roughly five
  lines and costs nothing to maintain. Docker is not the primary path, but the
  option should exist.
- **Observability:** `slog` JSON to stdout; request logging with method, path,
  status and duration; `/healthz` returning build version and a database ping.
  Metrics are deferred until something is measurably slow.

## 9. Testing

Tests are written after the feature, but cover it comprehensively.

- **Store layer:** tested against a real temporary SQLite file, never a mock.
  The interesting bugs live in the SQL.
- **Handlers:** `httptest`, asserting against parsed HTML using
  `golang.org/x/net/html` selectors rather than string comparison. String
  comparison of HTML produces a suite that breaks on every CSS change and
  teaches the author to ignore failures.
- **Migrations:** applied from empty on every run, with the resulting schema
  asserted. A bad migration never reaches the server.
- **Architecture:** a test walks the import graph and fails if an app imports
  another app, or if the platform imports an app. This is the rule that keeps
  each new app cheap.
- **CI:** GitHub Actions running `go vet`, `staticcheck`, and
  `go test ./... -race`.

**Exception — auth is tested first.** Authentication and session handling get
tests before implementation. It is the one area where deferring tests reliably
means the sad paths — expired session, tampered cookie, CSRF mismatch,
logged-out user hitting a private route — are never covered at all, and it is
the component every app depends on.

## 10. Deferred

Explicitly out of scope for this project, to be introduced by the app that
first needs them:

- Full-text search (SQLite FTS5) — first needed by ON Notes.
- A background job scheduler — first needed by ON Reader; the nightly backup
  job is a single timer and does not justify a scheduler yet.
- File uploads and attachments.
- Prometheus metrics.
- Alpine.js — first needed by ON Notes.
- Multi-tenant hardening, quotas and abuse handling. Out of scope permanently
  under the current constraints.

## 11. Success criteria

Project 0 and 1 are complete when all of the following hold:

1. `go build ./cmd/onsuite` produces a static binary with no CGO and no build
   step other than the Go toolchain.
2. `onsuite user add` creates the first account on an empty data directory.
3. A user can log in, land on the shell, and see the app switcher.
4. ON Paste supports create, view, list and delete of a snippet, scoped to the
   owning user, with syntax highlighting.
5. A paste can be shared via an unguessable public link readable while logged
   out, and only routes declared public are reachable that way.
6. `onsuite backup` produces a snapshot that restores into a working system.
7. The architecture test passes, and `go test ./... -race` is green in CI.
8. The binary runs on a Linux ARM64 host under systemd, serving HTTPS by both
   supported TLS paths.
