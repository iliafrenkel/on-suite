# Contributing

This is a personal project built to a fairly specific, opinionated design —
contributions are welcome, but please open an issue or discussion before
sending a large PR, especially anything that would add a dependency, a build
step, or touch the platform/app boundary. Small fixes, bug reports, and
questions are always welcome without any of that ceremony.

## Before you dive in

The [developer guide](docs/developers/index.md) explains how ON Suite is
built and why. The short version of the ground rules:

- No CGO, ever — every dependency must be pure Go.
- No Node, no npm, no JavaScript build step.
- Dependencies are capped; adding one is a design change, so ask first.
- Migrations are forward-only — no down migrations.
- Apps never import other apps, and the platform never imports an app —
  enforced by a test, not just a convention.
- No inline `<script>` or `style=` attributes; the CSP forbids them.

The reasoning behind each is in the
[platform design spec](docs/superpowers/specs/2026-08-18-on-suite-platform-design.md).

## Building and testing

See [Getting started](docs/developers/getting-started.md) to build and run it
locally, and [Testing](docs/developers/testing.md) for the full check and how
tests are written here. Writing a new app? Start with
[Adding an app](docs/developers/adding-an-app.md).

## Commit messages

Commit subjects follow [Conventional Commits](https://www.conventionalcommits.org/):
`type(scope): summary`, e.g. `fix(notes): trim search query before it reaches
the template`. The scope is usually an app name (`notes`, `paste`) or
`platform`; omit it for repo-wide changes.

The `type` decides which section of the release notes a commit lands in —
[`.goreleaser.yaml`](.goreleaser.yaml)'s `changelog.groups` sorts on it when a
tagged release is cut (see [docs/developers/releasing.md](docs/developers/releasing.md)):

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

## Pull requests

- Keep the full check green on every commit:
  `go build ./... && go vet ./... && go test ./... -race`.
- One topic per PR, and link the issue it addresses.
- A behaviour change comes with a test; a bug fix with a test that failed
  before it.
- Update the docs your change affects — user guides, the developer guide, or
  both.

## Forking

If you want to fork this for your own use rather than contribute back, go
right ahead — that's exactly what the MIT [LICENSE](LICENSE) is for.
