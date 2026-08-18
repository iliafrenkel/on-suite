# ON Suite

ON Suite is a self-hosted suite of small web applications for personal use —
one account, one shell, one SQLite file, one Go binary. Each app carries the
"ON" prefix: ON Paste, ON Notes, ON Reader, ON Flash.

It is built for a household, not a company: the author plus a handful of
family and friends, invite-only, no public sign-up, no multi-tenant hardening.
If you're looking for a SaaS-scale platform this isn't it — it's the opposite
bet, optimised for "one binary, one data directory, nothing else to run."

## Status

Early and incomplete. The project is being built in three plans, executed one
at a time so each can absorb what was learned from the last.

| Plan | Delivers | Status |
|---|---|---|
| 1 — Platform core | Config, SQLite + migrations, Argon2id auth, sessions, `onsuite user add` | **Done** |
| 2 — Web plumbing and app framework | Templates, middleware, CSRF, login, the `App` interface and router | **Done** |
| 3 — ON Paste and operations | The first real app, backup, TLS, packaging, CI | Not started |

You can build the binary, create an account, and log in to a real dashboard
today — there just aren't any apps registered in it yet. That's Plan 3: the
nav switcher and the dashboard already render correctly for zero apps, and
adding the first one (ON Paste) is one line in `cmd/onsuite/main.go` plus its
package.

See [`docs/superpowers/plans/2026-08-18-on-suite-00-roadmap.md`](docs/superpowers/plans/2026-08-18-on-suite-00-roadmap.md)
for the full task list and [`docs/superpowers/specs/2026-08-18-on-suite-platform-design.md`](docs/superpowers/specs/2026-08-18-on-suite-platform-design.md)
for the design this project is being built against.

## Why

Most self-hosted app suites are either a pile of Docker Compose services each
with their own database, or a SaaS product wearing a self-host badge. ON Suite
is neither: one Go binary, one SQLite file, no Node, no npm, no JavaScript
build step, no CGO. `go build ./cmd/onsuite` is the entire build, and the
result runs the same way on a Raspberry Pi as it does on a laptop.

## Design at a glance

- **One binary, one module.** Every app is compiled in and always enabled;
  there is no plugin loading and no per-app deployment.
- **One SQLite file**, opened with `journal_mode=WAL`, `busy_timeout=5000`,
  `foreign_keys=ON`, and a single connection (`SetMaxOpenConns(1)`), which
  trades write concurrency this deployment will never need for eliminating
  "database is locked" as a class of bug.
- **Forward-only migrations**, embedded with `go:embed`, applied in a
  transaction at startup. Each app owns and numbers its own migrations under
  its own namespace (`paste:0001`), so apps never coordinate schema changes
  with each other.
- **The platform never imports an app, and apps never import each other.**
  Apps import only `internal/platform/*` and `internal/ui`. An architecture
  test walks the whole import graph and fails the build if either rule is
  broken.
- **Argon2id passwords, invite-only accounts.** No public registration.
  Passwords are never passed as a CLI flag (visible in `ps` and shell
  history) — `onsuite user add` reads from a terminal with echo disabled, or
  from stdin for scripted setup.
- **Sessions with throttled sliding expiry.** A session lives 30 days from
  last use, renewed at most once a day per session rather than on every
  request.
- **A route is private unless it explicitly opts out.** Every app gets a
  router whose `Handle` requires a signed-in user by default; making a route
  anonymous means calling `Public`, which is a visible, greppable decision.
  A wrong password and an unknown username get byte-identical wording and
  status, so a login attempt can't be used to enumerate accounts.
- **A strict Content-Security-Policy forbids inline script and style
  anywhere.** HTMX (vendored, never loaded from a CDN) works fine under it
  because `hx-*` are HTML attributes, not inline script.

The full rationale for these choices — including what's deliberately left
out (multi-tenancy, metrics, a job scheduler, full-text search) and why — is
in the design spec linked above.

## Repository layout

```
on-suite/
├── cmd/onsuite/                 # the single binary: command dispatch, serve, stack, user add
├── internal/
│   ├── platform/
│   │   ├── config/              # flags + ONSUITE_* env -> Config
│   │   ├── db/                  # SQLite open/pragmas, migration runner, backup
│   │   ├── auth/                # Argon2id hashing, users, sessions
│   │   ├── render/              # template registry, layout composition (imports nothing else)
│   │   ├── web/                 # request context, middleware, CSRF, login/logout, static assets
│   │   └── app/                 # the App interface, default-deny Router, registry
│   ├── ui/                      # go:embed'd CSS, vendored HTMX, HTML templates (a leaf package)
│   ├── htmlassert/              # test-only HTML structure assertions
│   └── arch/                    # one test enforcing the import-boundary rules below
└── docs/
    └── superpowers/
        ├── specs/               # the design document
        └── plans/                # task-by-task implementation plans
```

`internal/apps/` (ON Paste, and later ON Notes/Reader/Flash) arrives in Plan 3.

## Requirements

- Go 1.26 or newer.
- No other tools, runtimes, or services. No Docker, no Node, no database
  server to install — SQLite is a pure-Go dependency
  (`modernc.org/sqlite`), so `CGO_ENABLED=0` always holds.
- A browser, once you get past the CLI — HTMX is vendored into the binary,
  so nothing is fetched from a CDN and the server renders a working page
  with no outbound internet access.

## Building and running

```bash
git clone https://github.com/iliafrenkel/on-suite.git
cd on-suite
go build ./cmd/onsuite
```

Create the first account (prompts for a password, twice, without echoing it):

```bash
./onsuite user add ilia --admin --data-dir ./data
```

Start the server:

```bash
./onsuite serve --data-dir ./data
```

Every flag has an `ONSUITE_*` environment variable equivalent (e.g.
`--addr` / `ONSUITE_ADDR`, `--data-dir` / `ONSUITE_DATA_DIR`). Run
`./onsuite serve -h` for the full list, or `./onsuite help` for all commands.

Confirm it's up:

```bash
curl -s localhost:8080/healthz
```

Then open `http://localhost:8080/` in a browser. You'll be redirected to
`/login`; sign in with the account you just created and you'll land on the
dashboard, with your username and a working log-out button in the top bar.
The app switcher is empty until Plan 3 registers ON Paste — that's expected.

Cross-compiling for a Linux server from any machine needs nothing extra,
since there's no CGO to worry about:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o onsuite ./cmd/onsuite
```

## Testing

```bash
go build ./... && go vet ./... && go test ./... -race
```

This is expected to stay green on every commit. Store-layer tests run
against a real SQLite file in a temp directory rather than a mock or
`:memory:` database, because the interesting bugs live in the SQL and in WAL
behaviour that `:memory:` doesn't reproduce.

## Contributing

This is a personal project built to a fairly specific, opinionated design —
contributions are welcome, but please open an issue or discussion before
sending a large PR, especially anything that would add a dependency, a build
step, or touch the platform/app boundary. Small fixes, bug reports, and
questions are always welcome without any of that ceremony.

A few constraints worth knowing before you dive in, all explained in more
depth in the design spec:

- No CGO, ever — every dependency must be pure Go.
- No Node, no npm, no JavaScript build step. HTMX is the one piece of
  front-end JavaScript, vendored into the repo and embedded — never loaded
  from a CDN.
- Platform dependencies are capped by design (currently `modernc.org/sqlite`,
  `golang.org/x/crypto`, `golang.org/x/term`, `golang.org/x/net`); adding a
  new one is a spec change, not just an implementation detail.
- Migrations are forward-only — no down migrations.
- Apps never import other apps, and the platform never imports an app —
  enforced by a test, not just a convention.
- No inline `<script>` or `style=` attributes; the CSP forbids them.

If you want to fork this for your own use rather than contribute back, go
right ahead — that's exactly what the MIT license is for.

## License

MIT — see [LICENSE](LICENSE).
