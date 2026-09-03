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

## Commit messages

Commit subjects follow [Conventional Commits](https://www.conventionalcommits.org/):
`type(scope): summary`, e.g. `fix(notes): trim search query before it reaches
the template`. The scope is usually an app name (`notes`, `paste`) or
`platform`; omit it for repo-wide changes.

The `type` decides which section of the release notes a commit lands in —
[`.goreleaser.yaml`](.goreleaser.yaml)'s `changelog.groups` sorts on it when a
tagged release is cut (see [docs/RELEASING.md](docs/RELEASING.md)):

| Type                                 | Release notes section          |
| ------------------------------------ | ------------------------------- |
| `feat`                                | New Features                    |
| `fix`                                 | Bug Fixes                       |
| `refactor`, `perf`, `chore`, `style`  | Improvements                    |
| `docs`, `test`                        | *(excluded — not shown at all)* |
| anything else                         | Everything Else (for the curious) |

A few things that matter for how a commit gets classified:

- **`feat` is for user-visible new capability**, not polish on an existing
  one — a new app, a new page, a new command. Incremental work on something
  that already shipped (new field on an existing form, a better error
  message, a faster query) is `refactor`/`perf`/`chore`/`style`, not `feat`.
- A commit whose message doesn't start with a recognized type (or that
  doesn't follow this convention at all) still ends up in the release notes
  — just in the catch-all "Everything Else" section — so nothing silently
  vanishes; it just won't be sorted into the section it probably belongs in.
- `docs:` and `test:` commits are the only ones dropped from release notes
  entirely, on the assumption that repo maintenance isn't news to users of
  the binary.

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
