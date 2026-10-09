# Repository layout

*For a developer looking for where something lives. Directories only — each
package's own doc comment says more.*

## The tree

```text
on-suite/
├── cmd/onsuite/                  the single binary: commands, server wiring (stack.go), backup, export, TLS
├── internal/
│   ├── apps/                     one package per app; never import each other
│   │   ├── books/                ON Books: a private reading log — shelves, readings and tags; Open Library, progress and stats to come
│   │   ├── flash/                ON Flash: decks, cards, FSRS review, import, media, sharing, stats
│   │   │   ├── migrations/       flash:0001… schema, forward-only
│   │   │   ├── static/           flash.js, served by the app itself
│   │   │   ├── templates/        page templates and *.partial.html blocks
│   │   │   └── testdata/         an example import deck
│   │   ├── focus/                ON Focus: focus timers — saved timers (single or intervals); running view and history to come
│   │   │   ├── migrations/
│   │   │   ├── static/           home.js
│   │   │   └── templates/
│   │   ├── later/                ON Later: read-it-later — save, reading view, highlights, tags, search, export
│   │   │   ├── migrations/
│   │   │   ├── static/           later.js, highlight.js
│   │   │   └── templates/
│   │   ├── notes/                ON Notes: the outliner — tree, search, due dates, archive, sharing
│   │   │   ├── migrations/
│   │   │   ├── static/           notes.js
│   │   │   └── templates/
│   │   ├── paste/                ON Paste: snippets, syntax highlighting, share links — the smallest app
│   │   │   ├── migrations/
│   │   │   └── templates/
│   │   └── reader/               ON Reader: feeds, polling, article extraction, image proxy, OPML, stats
│   │       ├── migrations/
│   │       ├── static/           reader.js
│   │       ├── templates/
│   │       └── testdata/         feed and OPML fixtures
│   ├── platform/                 everything shared; never imports an app
│   │   ├── app/                  the App interface, Deps, default-deny Router, Registry
│   │   ├── web/                  middleware stack, CSRF, login/logout, error pages, static assets, prefs
│   │   ├── render/               template registry and page/fragment rendering
│   │   ├── auth/                 users, Argon2id passwords, sessions
│   │   │   └── migrations/       platform:0001… — the users and sessions tables
│   │   ├── db/                   SQLite open and pragmas, migration runner, timestamp format
│   │   ├── config/               flags and ONSUITE_* variables → config.Config
│   │   ├── jobs/                 interval scheduler with run history and on-demand Trigger
│   │   ├── admin/                read-only admin dashboard at /admin/
│   │   ├── usermgmt/             /admin/users and /account
│   │   ├── jobsadmin/            /admin/jobs with Run now
│   │   ├── webfetch/             guarded HTTP client for publisher-controlled URLs: SSRF guard, caps, image sniffing
│   │   ├── article/              readability extraction, HTML sanitiser, image rewriting
│   │   ├── favicon/              finds a site's icon URL
│   │   └── help/                 renders docs/user's guides at /help, /help/{slug}; only importer of goldmark
│   ├── ui/                       embedded shell: base.html and platform pages, app.css, fonts, HTMX, icons
│   │   ├── static/               app.css, htmx.min.js, platform scripts, favicons
│   │   │   └── fonts/            self-hosted web fonts
│   │   └── templates/            base.html, login, home, error, account and admin pages
│   ├── apptest/                  test-only: the whole stack over a real database, with two signed-in users
│   ├── htmlassert/               test-only: structural HTML assertions for handler tests
│   └── arch/                     tests that enforce the import boundaries and containment rules
├── docs/
│   ├── developers/               this guide, plus releasing.md
│   ├── self-hosting/             deploying.md, running-locally.md, the systemd unit
│   ├── user/                     end-user guides, one per app plus admin; served at /help (internal/platform/help)
│   │   └── images/               the guides' screenshots
│   ├── screenshots/              regenerates every screenshot; README.md says how
│   │   ├── seed/                 a fresh data directory with demo accounts and content
│   │   └── capture/              headless Chrome over DevTools; shots.go lists every shot
│   ├── images/                   screenshots used by the README
│   ├── superpowers/              historical design specs and implementation plans
│   ├── doc.go                    makes docs a Go package, so its tests run with go test ./...
│   ├── embed.go                  //go:embed user — the guides compiled into the binary; docs.User() feeds help.Load
│   └── links_test.go             checks every link, image and #anchor in the docs
├── scripts/next-version.sh       suggests the next release tag from commit history
├── .github/workflows/            ci.yml (the checks) and release.yml (tagged releases)
├── .goreleaser.yaml              cross-compiled release builds and the Docker image
├── Dockerfile                    build-from-source image (scratch-based)
├── Dockerfile.release            GoReleaser's image around a prebuilt binary
├── AGENTS.md                     instructions for AI coding agents (CLAUDE.md points here)
├── PATTERNS.md                   index of deliberate, recurring patterns — check before inventing one
├── CONTRIBUTING.md               ground rules and commit conventions
└── README.md                     the user-facing front page
```

## Inside an app

The six apps share a shape, so once you know one you can find your way
around the others. Using `internal/apps/paste` as the example:

| File | Holds |
| --- | --- |
| `paste.go` | `App`: `Meta`, `Migrations`, `Templates`, `Mount` (the route list), `Stats` |
| `store.go` | `Store`: the app's SQL, and the clock it reads (`now`) |
| `handlers.go` | HTTP handlers and view models |
| `export.go` | `Export`, for `onsuite export` |
| `highlight.go` | app-specific logic (here, Chroma highlighting) |
| `migrations/*.sql` | the schema, one numbered file per change |
| `templates/*.html` | one file per page; `*.partial.html` for shared blocks |

Larger apps split `handlers.go` and `store.go` by topic
(`handlers_cards.go`, `media_store.go`) and name the main file `app.go` or
after the app.

## Where do I look for…

| Looking for | Look in |
| --- | --- |
| an app's routes | its `Mount` method: `internal/apps/<app>/app.go`, or `paste.go` / `flash.go` / `later.go` |
| the platform's own routes | `buildStack` in `cmd/onsuite/stack.go`, and `Routes` in `web/login.go`, `usermgmt/usermgmt.go`, `jobsadmin/jobsadmin.go` |
| every route at once | the admin dashboard (`/admin/`) lists them all, with public ones marked |
| a page's template | `internal/apps/<app>/templates/<page>.html`; the `render` name is `<app>/<page>` |
| the shell (header, sidebar, footer) | `internal/ui/templates/base.html` |
| a migration | `internal/apps/<app>/migrations/`, or `internal/platform/auth/migrations/` for users and sessions |
| a CSS rule | `internal/ui/static/app.css` — one file, with a commented section per app (`/* ---- Snippets ---- */`) |
| an app or sidebar icon | `internal/ui/icons.go` (`IconFor`, keyed by app id) |
| a toolbar icon | `internal/ui/toolbar_icons.go` (`ToolbarIconFor`, the `ticon` template function) |
| an app's JavaScript | `internal/apps/<app>/static/`; platform scripts in `internal/ui/static/` |
| a command-line command | `run` in `cmd/onsuite/main.go`, then `user.go`, `export.go`, `backup.go`, `serve.go` |
| a flag or env var | `internal/platform/config/config.go` |
| an app's user guide | `docs/user/<app>.md`, screenshots in `docs/user/images/` (regenerate via `docs/screenshots/`); served at `/help/<app>` by `internal/platform/help` |
| a background job | `registerMaintenance` in `cmd/onsuite/backup.go`, or an app's `Jobs` method |

**Next:** [Adding an app](adding-an-app.md)
