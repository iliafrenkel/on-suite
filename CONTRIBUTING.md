# Contributing

This is a personal project built to a fairly specific, opinionated design —
contributions are welcome, but please open an issue or discussion before
sending a large PR, especially anything that would add a dependency, a build
step, or touch the platform/app boundary. Small fixes, bug reports, and
questions are always welcome without any of that ceremony.

## Before you dive in

A few constraints worth knowing, all explained in more depth in the
[design spec](docs/superpowers/specs/2026-08-18-on-suite-platform-design.md):

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

## Building and testing

```bash
go build ./... && go vet ./... && go test ./... -race
```

This is expected to stay green on every commit. Store-layer tests run
against a real SQLite file in a temp directory rather than a mock or
`:memory:` database, because the interesting bugs live in the SQL and in WAL
behaviour that `:memory:` doesn't reproduce.

## Adding a new app

Adding an app (ON Notes, ON Reader, ON Flash, or anything else) means writing
a package under `internal/apps/` that implements the `App` interface and
adding one line to `registeredApps()` in `cmd/onsuite/main.go` — nothing
else in the platform changes. See the [design spec](docs/superpowers/specs/2026-08-18-on-suite-platform-design.md)
for the `App` interface and the platform/app boundary rules an architecture
test enforces.

## Forking

If you want to fork this for your own use rather than contribute back, go
right ahead — that's exactly what the MIT [LICENSE](LICENSE) is for.
