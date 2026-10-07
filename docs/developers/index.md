# Developer guide

*For anyone who wants to build, change or fork ON Suite. You should know Go;
you don't need to know anything about this repository yet.*

ON Suite is a self-hosted set of small web apps for one household — ON Paste,
ON Notes, ON Reader, ON Later, ON Flash and ON Focus — sharing one account system, one
shell, one SQLite file and one Go binary. It is deliberately small and
opinionated: the constraints below are what keep it that way.

## The ground rules

These are non-negotiable. Most are enforced by a test, not just a convention.

- **No CGO, ever.** Every dependency must be pure Go, so
  `CGO_ENABLED=0 go build` always works and cross-compiling needs nothing
  extra.
- **No Node, no npm, no JavaScript build step.** HTMX is vendored and
  embedded (never loaded from a CDN), alongside a few small hand-written
  scripts served as plain files. `go build ./cmd/onsuite` is the entire
  build.
- **Dependencies are capped.** Adding a module is a design decision, not an
  implementation detail — open an issue first. Today's direct dependencies
  (from `go.mod`):

  | Module | Used by |
  | --- | --- |
  | `modernc.org/sqlite` | `internal/platform/db` — the pure-Go SQLite driver |
  | `golang.org/x/crypto` | Argon2id in `internal/platform/auth`; Let's Encrypt (`acme/autocert`) in `cmd/onsuite` |
  | `golang.org/x/term` | reading passwords without echo in `cmd/onsuite` |
  | `golang.org/x/net` | HTML parsing in ON Notes, ON Reader, `internal/platform/article`, `internal/platform/favicon` and `internal/htmlassert` |
  | `github.com/alecthomas/chroma/v2` | ON Paste's syntax highlighting |
  | `github.com/mmcdole/gofeed` | ON Reader's feed parsing |
  | `github.com/microcosm-cc/bluemonday` | `internal/platform/article` — HTML sanitising for ON Reader, and ON Later |
  | `github.com/go-shiori/go-readability` | ON Reader's full-article extraction — allowed only in `internal/platform/article/extract.go` |
  | `github.com/open-spaced-repetition/go-fsrs/v4` | ON Flash's review scheduling — allowed in one file only |
  | `github.com/yuin/goldmark` | Markdown rendering for the in-app help at `/help` — allowed only in `internal/platform/help`, enforced by `TestGoldmarkIsContained` |

- **Migrations are forward-only.** There are no down migrations; a mistake
  is fixed by a new migration.
- **Apps never import each other, and the platform never imports an app.**
  Apps import only `internal/platform/*` and `internal/ui`. An architecture
  test fails the build if either rule breaks.
- **No inline script or style.** The Content-Security-Policy forbids inline
  `<script>` blocks, `on*=` handlers and `style=` attributes. All CSS lives in
  stylesheets, all JavaScript in same-origin files.

## Where to go next

| Page | Answers |
| --- | --- |
| [Getting started](getting-started.md) | How do I build it, run it locally and make my first change? |
| [Architecture](architecture.md) | How does a request flow through it, and why is it built this way? |
| [Repository layout](repository-layout.md) | Where does each thing live, and where do I look for a route, template or migration? |
| [Adding an app](adding-an-app.md) | How do I write a new ON app and plug it in? |
| [Testing](testing.md) | What does the full check run, and how do I write handler and store tests? |
| [Releasing](releasing.md) | How is a tagged release cut and versioned? (maintainers) |

Running ON Suite on a real server is covered separately, in
[Deploying ON Suite](../self-hosting/deploying.md).

## Contributing

Read [CONTRIBUTING.md](../../CONTRIBUTING.md) before opening a pull request:
it covers when to open an issue first, the commit message convention (which
drives the release notes), and what a PR should include. Before writing new
code, skim [PATTERNS.md](../../PATTERNS.md) — an index of problems this
codebase has already solved one deliberate way.

## Design history

The full rationale for the platform lives in the
[platform design spec](../superpowers/specs/2026-08-18-on-suite-platform-design.md).
Every feature since has its own spec and plan in
[docs/superpowers/](../superpowers/) — when you wonder why something is the
way it is, the answer is usually there. Treat them as history: the code and
these pages describe how things are now.

**Next:** [Getting started](getting-started.md)
