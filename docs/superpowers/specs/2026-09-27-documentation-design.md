# Documentation overhaul and in-app help — design

Issues: #308 (end-user documentation), #309 (HTML help pages shipped with the
release). Milestone: v2.0.0.

## Goals

- `README.md` is for **users**: why ON Suite exists, what it is and what it
  isn't, one or two sentences per app, and links onward. No developer
  material.
- Every app has a complete user guide: main use cases, every feature, with
  screenshots.
- Developer material (building, architecture, layout, testing, releasing,
  adding an app) moves to its own set of documents.
- The user guides are served inside ON Suite at `/help`, compiled into the
  binary, so the help always matches the running version (#309).

## Non-goals

- No docs site generator, no GitHub Pages.
- No splitting app guides into several pages yet. One page per app; split
  later if one grows unwieldy.
- No dark-theme screenshots in the app guides (README only).
- No per-page search in `/help`.

## Audiences and file layout

```
README.md                        users: motivation, per-app summaries, docs map
docs/
  embed.go                       package docs: //go:embed user
  user/                          users — embedded, served at /help
    index.md                     welcome: signing in, dashboard, app switcher,
                                 user menu, account & password, display settings
    paste.md                     ON Paste
    notes.md                     ON Notes
    reader.md                    ON Reader
    flash.md                     ON Flash
    admin.md                     admins: users, jobs, admin dashboard cards
    images/*.png                 light theme, fixed viewport
  self-hosting/                  whoever runs the server — not served in-app
    deploying.md                 moved from docs/DEPLOYING.md, plus a quick
                                 start from a release binary or Docker image
    onsuite.service              moved from docs/onsuite.service
  developers/                    contributors and forkers
    index.md                     start here: constraints, map of the pages
    getting-started.md           build, first user, run, check
    architecture.md              design at a glance, platform/app boundary,
                                 request flow, CSP, auth, SQLite choices
    repository-layout.md         current tree, all four apps
    adding-an-app.md             App interface, router, migrations, registration
    testing.md                   the full check, real-SQLite store tests,
                                 htmlassert, arch test, refreshing screenshots
    releasing.md                 moved from docs/RELEASING.md, versioning folded in
  screenshots/                   demo seed for reproducible screenshots
  images/                        README-only images incl. dark variants
  superpowers/                   unchanged (specs, plans)
CONTRIBUTING.md                  stays at root (GitHub convention), trimmed to
                                 ground rules + commit conventions, links into
                                 docs/developers/
```

The README's "Status" build-phase table is dropped, not moved.

### README shape

1. Title, one-line pitch.
2. Hero screenshot using `<picture>` with a `prefers-color-scheme: dark`
   source, so GitHub shows light or dark to match the reader.
3. **Why ON Suite** — the motivation, drafted from today's "Why" and
   "household, not a company" text: what it is, what it deliberately isn't.
   Ilia edits the draft.
4. **The apps** — for each of ON Paste, ON Notes, ON Reader, ON Flash: one or
   two sentences, a thumbnail, a link to `docs/user/<app>.md`.
5. **Run it** — three commands at most, then a link to
   `docs/self-hosting/deploying.md`.
6. **Documentation** — a map: user guides, self-hosting, developers.
7. License.

### App guide shape

Every `docs/user/<app>.md` follows the same outline:

1. What it's for (one paragraph).
2. Quick tour — the main screen with a screenshot, naming its parts.
3. Features grouped by task (e.g. "Organising feeds into folders",
   "Sharing a note publicly"), each with a screenshot where it helps.
4. Keyboard shortcuts table (where the app has them).

Content is written from the code and templates, not from memory: each app's
routes, handlers and templates are inventoried so every user-facing feature
is covered and described as it actually behaves.

## In-app help (`/help`)

### Dependency

Add `github.com/yuin/goldmark` (pure Go, no transitive dependencies). This is
a change to the capped dependency list: update the platform design spec's
dependency section and the list in CONTRIBUTING / `docs/developers/`.

### Embedding

`docs/embed.go` declares `package docs` with `//go:embed user` and exports an
`fs.FS` of the user guides and their images. `go:embed` can't reach parent
directories, so the package has to live at `docs/` itself. The package
contains only the embed; it imports nothing.

### Rendering — `internal/platform/help`

- At construction (server startup) it walks the embedded FS, renders each
  `*.md` with goldmark (GFM tables enabled, heading IDs for anchors),
  sanitises with bluemonday (already a dependency), and caches the resulting
  HTML per page along with the page title (first `#` heading). A page that
  fails to render fails startup — and the tests — rather than a request.
- An AST transform rewrites links so the same Markdown works on GitHub and
  in-app: a relative `notes.md` / `notes.md#anchor` becomes
  `/help/notes` / `/help/notes#anchor`; `index.md` becomes `/help`; a
  relative `images/x.png` becomes `/help/images/x.png`. Absolute and external
  URLs are untouched.
- Images are served straight from the embedded FS under `/help/images/`.

### Routes and layout

- `GET /help` → `index.md`; `GET /help/{page}` → `{page}.md`; unknown page →
  the normal 404 page. `GET /help/images/{file}` → embedded image.
- All `/help` routes are **public** (registered via `Public`), so they work
  signed out.
- Pages render inside the normal shell (`base.html`) through a new
  `help.html` template: a sidebar listing every page (Welcome, ON Paste,
  ON Notes, ON Reader, ON Flash, Administration) with the current one marked,
  and the rendered guide in the main column. Styling lives in `app.css`; no
  inline style or script (CSP unchanged).
- Rendered guide HTML is inserted as trusted `template.HTML` only after
  bluemonday sanitisation.

### Architecture

`internal/platform/help` imports `docs`, `internal/platform/*` and
`internal/ui`; it never imports an app, so the existing
platform-never-imports-an-app rule covers it. Two arch-test rules are added,
following the existing `TestReadabilityIsContained` pattern:

- `docs` (the embed package itself, not `docs/screenshots/...`) is a leaf and
  imports nothing but `embed`/`io/fs`.
- goldmark is imported only by `internal/platform/help`.

The screenshot seed under `docs/screenshots/` is a `main` package like
`cmd/onsuite`, so it can import the app stores; nothing imports it.

### Help link

The user menu (`base.html`) gets a **Help** item (with an icon), placed
above Account. The target is context-aware: inside an app it links to
`/help/<app>` (paste, notes, reader, flash); everywhere else (dashboard,
account, admin pages, help itself) it links to `/help`. The target comes from
the shell data the layout already receives, so apps don't have to change.

## Screenshots

- `docs/screenshots/` holds a repeatable demo seed: a small Go program run
  with `go run ./docs/screenshots/seed --data-dir <dir>` that creates a demo
  account and realistic content using the real stores: a few pastes in
  different languages, a notes tree with tags, done items and due dates, a
  handful of feeds in folders with articles loaded from local fixtures (no
  network), and a couple of flash decks with cards and some review history.
- Screenshots are taken with the in-app browser against a server on the
  seeded data dir, at a fixed viewport, in the light theme. README hero and
  thumbnails also get dark variants.
- App-guide images go in `docs/user/images/` (embedded). README-only images
  go in `docs/images/`. Target total embedded image size ≈ 2 MB. PNGs are
  optimised before they're committed.
- `docs/developers/testing.md` documents how to refresh them.

## Build and release changes

- `.dockerignore` currently excludes `docs`. It must stop excluding
  `docs/user` and `docs/embed.go` (or the Docker build won't compile).
- `.goreleaser.yaml` archive `files`: `docs/DEPLOYING.md` →
  `docs/self-hosting/deploying.md`, `docs/onsuite.service` →
  `docs/self-hosting/onsuite.service`. Its `# See docs/RELEASING.md` comment is
  updated.
- Every reference to moved files is updated: AGENTS.md's docs list,
  CONTRIBUTING, DEPLOYING/RELEASING cross-links, and the systemd unit path in
  the deploy guide's `scp` commands.

## Testing

- `internal/platform/help`: every embedded page renders, has a title, and
  appears in the sidebar; link rewriting (`x.md`, `x.md#a`, `index.md`,
  `images/…`, external, absolute) is table-tested; unknown page → 404;
  `/help` and `/help/notes` return 200 signed out; images serve with the
  right content type.
- Link integrity: a test walks every Markdown file under `docs/user`,
  `docs/developers`, `docs/self-hosting`, plus `README.md` and
  `CONTRIBUTING.md`, and asserts every relative link and image target exists
  (anchors are checked against heading IDs for `docs/user`). Pages being
  renamed or moved then can't leave dead links.
- Help menu item: htmlassert tests that it points to `/help/notes` inside
  ON Notes (and likewise for each app) and to `/help` on the dashboard.
- The full check (`go build ./... && go vet ./... && go test ./... -race`)
  stays green; the Docker build is verified once locally after the
  `.dockerignore` change.

## Delivery

Two PRs, in order:

1. **#308 — the documentation**: README rewrite, `docs/user/*`,
   `docs/developers/*`, `docs/self-hosting/*`, moves and reference updates,
   the screenshot seed and screenshots, the link-integrity test.
2. **#309 — in-app help**: goldmark, `docs/embed.go`,
   `internal/platform/help`, `/help` routes and template, the Help menu item,
   `.dockerignore` fix, dependency-list updates.
