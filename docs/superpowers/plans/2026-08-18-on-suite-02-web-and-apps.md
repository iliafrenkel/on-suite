# ON Suite — Plan 2 of 3: Web Plumbing and the App Framework

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the Plan 1 binary into a web application you can log into — embedded assets, a template renderer, middleware, CSRF, login and logout, and the `app.App` framework that every future app plugs into.

**Deliverable:** `onsuite serve`, log in with the account `onsuite user add` created in Plan 1, and land on the shell with a working app switcher. Satisfies spec §11.3.

**Architecture:** Four new packages in a strict dependency order, so nothing can import in a circle:

```
internal/ui      (leaf: embedded static files and templates)
internal/platform/render   (leaf: templates. Takes explicit data, reads no context)
internal/platform/auth     (leaf: identity, built in Plan 1. Still HTTP-free)
        ↑
internal/platform/web      (context, middleware, CSRF, login, errors, assets)
        ↑
internal/platform/app      (App interface, Deps, default-deny Router, registry)
        ↑
cmd/onsuite
```

**The one architectural decision worth understanding before you start.** The obvious design has `render` read the current user and CSRF token out of the request context — but `web` owns that context and needs `render` for its error and login pages, so the two would import each other. Instead **`render` is a pure function of explicit inputs**: callers hand it a `render.Shell` describing who is logged in and what to put in the nav, and `web.NewPage` builds that struct from the request. The result is that `render` imports nothing from the platform at all, and template output is trivially testable without constructing a request.

**Tech Stack:** unchanged from Plan 1 — stdlib `net/http` and `html/template`, plus vendored HTMX 2.x. **No new Go dependencies.**

**Spec:** `docs/superpowers/specs/2026-08-18-on-suite-platform-design.md`
**Previous plan:** `2026-08-18-on-suite-01-platform-core.md` (executed)

## This plan's code has been compiled and run

Every Go, HTML and CSS block below was extracted from this document, applied on
top of the executed Plan 1 tree, and run before the plan was published. On this
toolchain (Go 1.26.6, SQLite 3.53.3):

- `gofmt -l .` clean, `go vet ./...` clean, `go test ./... -race` green in all
  nine packages.
- `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/onsuite` succeeds.
- Both architecture rules were demonstrated to **fail** when deliberately
  broken, then pass again.
- The whole login flow was driven end to end against a real server: anonymous
  redirect to `/login?next=%2F`, a POST without a token refused with 403,
  identical wording for a wrong password and an unknown user, a real sign-in,
  a styled 404 carrying the shell, logout revoking the session server-side so a
  replayed cookie fails, and an HTMX request receiving `204` plus `HX-Redirect`
  rather than a `303` it cannot act on.

Six defects were found and fixed this way:

| Defect | Fix |
|---|---|
| `htmlassert` could not parse descendant selectors, so `nav.shell-nav a` silently matched nothing and a passing-looking test asserted nothing | Rewritten with a compiled matcher and CSS-style ancestor resolution |
| The architecture scan never recorded packages that import nothing internal, so `render` and `auth` were reported as unscanned | Every package is now recorded, even with zero internal imports |
| `GET /{slug}/raw` and `GET /s/{slug}` are ambiguous to `ServeMux`, which panics at startup | Example routes changed; see the warning in Task 13 |
| The error fragment omitted the title it was asserted on | Fragment renders `Title — Message` |
| An unused import and two redundant test helpers | Removed |
| `x/net` stayed an indirect dependency without `go mod tidy` | Added to Task 9 |

The `htmlassert` and architecture-scan bugs are the instructive pair: both were
tests that passed while checking nothing. That is the failure mode worth fearing
in a test suite, and it is why the architecture test now has a test guarding
itself.

## Global Constraints

Everything in Plan 1's Global Constraints still applies. In addition:

- **No new Go dependencies in this plan.** If you reach for one, stop: the design does not need it. `golang.org/x/net` remains permitted for tests only.
- **HTMX is vendored and committed**, never a CDN reference. Its version and SHA-256 are recorded in `internal/ui/static/VENDOR.md`.
- **No inline `<script>` or `style=` attributes.** The Content-Security-Policy set in Task 10 forbids them, so anything relying on them will silently stop working in a browser while still passing tests. `hx-*` attributes are HTML attributes, not inline script, and are unaffected.
- **Every non-GET route is CSRF-checked and authentication-guarded by default.** Making a route public is an explicit, visible act.

### Visual direction — decided, do not redesign

**Quiet and dense, keyboard-first.** Compact spacing, system font stack, muted greys with a single accent, always-visible focus rings, no animation. It should read as a tool, not a product. Both remaining apps are text you scan — Notes is an outliner, Paste is code — so vertical space is the scarce resource. Concretely:

- One accent colour, used for links, focus and the active nav item. Nothing else is coloured.
- Focus is never removed, only restyled. Every interactive element has a visible focus ring.
- No transitions or animations anywhere.
- Line height 1.45 for body text, tighter for dense lists.
- Light and dark both defined via tokens; dark comes from `prefers-color-scheme`.

### Testing policy

Plan 1's policy carries over: implement then test, in the same commit, **except** Tasks 11 and 12 (CSRF and authentication) which are strict TDD.

Handler tests parse HTML with `golang.org/x/net/html` and assert on structure, never on string equality of markup. A helper for this is built in Task 9 and reused by every later task.

---

## File Structure

| Path | Responsibility |
|---|---|
| `internal/ui/ui.go` | `go:embed` of static files and templates. Nothing else. |
| `internal/ui/static/app.css` | The entire design system. Tokens plus component styles. |
| `internal/ui/static/htmx.min.js` | Vendored HTMX 2.x. |
| `internal/ui/static/VENDOR.md` | Version and SHA-256 of every vendored file. |
| `internal/ui/templates/base.html` | The `<html>` document, `<head>`, and the shell chrome. |
| `internal/ui/templates/login.html` | Login page. |
| `internal/ui/templates/error.html` | Error page and its HTMX fragment variant. |
| `internal/platform/render/render.go` | Template registry, layout composition, `Shell` and `Page`. |
| `internal/platform/render/testhtml.go` | HTML assertion helpers shared by every handler test. |
| `internal/platform/web/assets.go` | Content-hashed static asset URLs and handler. |
| `internal/platform/web/context.go` | Request-scoped user, CSRF token and active app. |
| `internal/platform/web/middleware.go` | Recover, request log, security headers, `Chain`. |
| `internal/platform/web/errors.go` | Status-code rendering, HTMX-aware. |
| `internal/platform/web/csrf.go` | Token issue and constant-time verify. |
| `internal/platform/web/login.go` | `Auth`: cookies, guards, login and logout, rate limiting. |
| `internal/platform/app/app.go` | `App` interface, `Meta`, `Deps`, registry. |
| `internal/platform/app/router.go` | Default-deny `Router`. |
| `internal/arch/arch_test.go` | Import-boundary enforcement. |
| `cmd/onsuite/stack.go` | Assembles the whole HTTP stack. Extracted from `serve.go`. |

---

# Task 8: Embedded assets, the design system, and a stack extraction

**Files:**
- Create: `internal/ui/ui.go`, `internal/ui/static/app.css`, `internal/ui/static/VENDOR.md`
- Create (downloaded): `internal/ui/static/htmx.min.js`
- Create: `internal/platform/web/assets.go`
- Create: `cmd/onsuite/stack.go`
- Modify: `cmd/onsuite/serve.go` (move stack assembly out; tidy a stale comment)
- Test: `internal/platform/web/assets_test.go`

**Interfaces:**
- Consumes: `config.Config` and `db.Open` from Plan 1.
- Produces:
  - `ui.Static() fs.FS`, `ui.Templates() fs.FS`
  - `web.NewAssets(fsys fs.FS, prefix string) (*web.Assets, error)`
  - `(*web.Assets).URL(name string) string` → `/static/app.css?v=<8 hex chars>`
  - `(*web.Assets).Handler() http.Handler` — mount under `prefix` with `http.StripPrefix`
  - `buildStack(deps stackDeps) (http.Handler, error)` in package `main`

**Why content-hashed URLs.** The CSS is embedded in the binary, so it changes only when you deploy. Hashing the content into the query string means the browser can cache it for a year, and a deploy invalidates it automatically with no cache-busting ritual. Without this you either get stale CSS after deploys or no caching at all.

- [ ] **Step 1: Vendor HTMX**

Download it, record what you got, and commit the file. Do not reference a CDN — spec §5 requires the binary to render without public internet access.

```bash
mkdir -p internal/ui/static
curl -fsSL -o internal/ui/static/htmx.min.js https://unpkg.com/htmx.org@2/dist/htmx.min.js
```

Now record the exact version and checksum you received:

```bash
grep -o 'htmx.org@[0-9.]*' internal/ui/static/htmx.min.js | head -1
shasum -a 256 internal/ui/static/htmx.min.js
ls -l internal/ui/static/htmx.min.js
```

Write those values into `internal/ui/static/VENDOR.md`, replacing the bracketed parts with the real output:

```markdown
# Vendored front-end files

These are committed rather than fetched at build or run time: `onsuite` must
serve a working page on a host with no outbound internet access.

To update one, download it, verify the checksum against the project's own
published release, update the table, and commit the new file in its own commit
so the diff is reviewable.

| File | Version | SHA-256 | Source |
|---|---|---|---|
| `htmx.min.js` | [version from the grep above] | [checksum from shasum] | https://unpkg.com/htmx.org@2/dist/htmx.min.js |
```

Sanity-check the download before trusting it — a captive portal or a 404 page will happily save as a `.js` file:

```bash
head -c 200 internal/ui/static/htmx.min.js
```

Expected: minified JavaScript, and the file is roughly 45–55 KB. If it is a few hundred bytes or contains `<html`, the download failed.

- [ ] **Step 2: Write the design system**

Create `internal/ui/static/app.css`. This is the whole visual language for every app in the suite, so it is worth reading rather than skimming.

```css
/* ON Suite — quiet, dense, keyboard-first.
 *
 * Everything is driven by the tokens in :root. Apps must not introduce new
 * colours or spacing values; they compose these. That constraint is what
 * makes four separately-built apps look like one suite.
 */

:root {
	/* Greys carry the interface; the accent is used sparingly and on purpose. */
	--c-bg:          #ffffff;
	--c-bg-subtle:   #f6f6f5;
	--c-bg-inset:    #efefed;
	--c-border:      #dcdcd8;
	--c-border-firm: #c2c2bc;
	--c-text:        #1c1c1a;
	--c-text-dim:    #6b6b66;
	--c-text-faint:  #93938c;
	--c-accent:      #1a5fb4;
	--c-accent-bg:   #e8f0fb;
	--c-danger:      #a51d2d;
	--c-danger-bg:   #fbeaec;

	/* A 4px scale. Dense by intent: vertical space is the scarce resource in
	 * an outliner and in a code listing. */
	--s-1: 0.25rem;
	--s-2: 0.5rem;
	--s-3: 0.75rem;
	--s-4: 1rem;
	--s-5: 1.5rem;
	--s-6: 2rem;

	--font-ui: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
	--font-mono: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;

	--fs-sm: 0.8125rem;
	--fs-base: 0.9375rem;
	--fs-lg: 1.125rem;
	--fs-xl: 1.375rem;

	--radius: 3px;
	--border: 1px solid var(--c-border);
	--ring: 2px solid var(--c-accent);

	--measure: 68ch; /* comfortable reading width */
}

@media (prefers-color-scheme: dark) {
	:root {
		--c-bg:          #16161a;
		--c-bg-subtle:   #1e1e23;
		--c-bg-inset:    #26262c;
		--c-border:      #35353d;
		--c-border-firm: #4a4a54;
		--c-text:        #e6e6e3;
		--c-text-dim:    #9e9e98;
		--c-text-faint:  #74746e;
		--c-accent:      #7aa9e8;
		--c-accent-bg:   #1c2a3d;
		--c-danger:      #f08c95;
		--c-danger-bg:   #3a1e22;
	}
}

*, *::before, *::after { box-sizing: border-box; }

html { -webkit-text-size-adjust: 100%; }

body {
	margin: 0;
	background: var(--c-bg);
	color: var(--c-text);
	font: var(--fs-base)/1.45 var(--font-ui);
}

/* Focus is restyled, never removed. This suite is meant to be usable
 * entirely from the keyboard. */
:focus-visible {
	outline: var(--ring);
	outline-offset: 2px;
	border-radius: var(--radius);
}

a { color: var(--c-accent); text-decoration-thickness: 1px; text-underline-offset: 2px; }
a:hover { text-decoration-thickness: 2px; }

h1, h2, h3 { margin: 0 0 var(--s-3); font-weight: 600; line-height: 1.25; }
h1 { font-size: var(--fs-xl); }
h2 { font-size: var(--fs-lg); }
h3 { font-size: var(--fs-base); }

code, pre, kbd { font-family: var(--font-mono); font-size: 0.9em; }

pre {
	margin: 0;
	padding: var(--s-3);
	overflow-x: auto;
	background: var(--c-bg-subtle);
	border: var(--border);
	border-radius: var(--radius);
}

kbd {
	padding: 1px var(--s-1);
	background: var(--c-bg-inset);
	border: 1px solid var(--c-border-firm);
	border-radius: var(--radius);
	font-size: 0.85em;
}

/* ---- Shell ------------------------------------------------------------- */

.shell-bar {
	display: flex;
	align-items: center;
	gap: var(--s-4);
	padding: var(--s-2) var(--s-4);
	background: var(--c-bg-subtle);
	border-bottom: var(--border);
}

.shell-mark {
	font-weight: 600;
	letter-spacing: 0.02em;
	color: var(--c-text);
	text-decoration: none;
	white-space: nowrap;
}

.shell-nav { display: flex; gap: var(--s-1); flex: 1; min-width: 0; }

.shell-nav a {
	padding: var(--s-1) var(--s-2);
	border-radius: var(--radius);
	color: var(--c-text-dim);
	text-decoration: none;
	white-space: nowrap;
}

.shell-nav a:hover { background: var(--c-bg-inset); color: var(--c-text); }

.shell-nav a[aria-current="page"] {
	background: var(--c-accent-bg);
	color: var(--c-accent);
	font-weight: 600;
}

.shell-user { display: flex; align-items: center; gap: var(--s-2); font-size: var(--fs-sm); color: var(--c-text-dim); }

main { padding: var(--s-5) var(--s-4); }
.measure { max-width: var(--measure); }

/* ---- Forms and buttons ------------------------------------------------- */

label { display: block; margin-bottom: var(--s-1); font-size: var(--fs-sm); color: var(--c-text-dim); }

input[type="text"], input[type="password"], textarea, select {
	width: 100%;
	padding: var(--s-2);
	background: var(--c-bg);
	color: var(--c-text);
	border: 1px solid var(--c-border-firm);
	border-radius: var(--radius);
	font: inherit;
}

textarea { font-family: var(--font-mono); resize: vertical; }

.field { margin-bottom: var(--s-4); }

button, .button {
	padding: var(--s-2) var(--s-4);
	background: var(--c-bg-inset);
	color: var(--c-text);
	border: 1px solid var(--c-border-firm);
	border-radius: var(--radius);
	font: inherit;
	cursor: pointer;
}

button:hover, .button:hover { background: var(--c-border); }

button[type="submit"] {
	background: var(--c-accent);
	border-color: var(--c-accent);
	color: #fff;
	font-weight: 600;
}

@media (prefers-color-scheme: dark) {
	button[type="submit"] { color: #10141a; }
}

/* ---- Notices ----------------------------------------------------------- */

.notice {
	padding: var(--s-2) var(--s-3);
	margin-bottom: var(--s-4);
	border: var(--border);
	border-radius: var(--radius);
	background: var(--c-bg-subtle);
	font-size: var(--fs-sm);
}

.notice-error {
	background: var(--c-danger-bg);
	border-color: var(--c-danger);
	color: var(--c-danger);
}

/* ---- Layout helpers used across apps ---------------------------------- */

.stack > * + * { margin-top: var(--s-3); }
.row { display: flex; align-items: center; gap: var(--s-2); }
.dim { color: var(--c-text-dim); }
.faint { color: var(--c-text-faint); font-size: var(--fs-sm); }
.centered { max-width: 22rem; margin: var(--s-6) auto; }

/* Wide content scrolls inside its own box rather than making the page
 * scroll sideways. */
.scroll-x { overflow-x: auto; }
```

- [ ] **Step 3: Embed the assets**

Create `internal/ui/ui.go`:

```go
// Package ui holds the embedded static files and HTML templates for the whole
// suite. It is a leaf package: it contains no logic, only the embed
// directives, so that anything can depend on it without acquiring
// dependencies of its own.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed static
var staticFiles embed.FS

//go:embed templates
var templateFiles embed.FS

// Static is the tree served under /static/.
func Static() fs.FS { return must(fs.Sub(staticFiles, "static")) }

// Templates is the shared layout and platform pages. Apps supply their own
// templates separately.
func Templates() fs.FS { return must(fs.Sub(templateFiles, "templates")) }

func must(fsys fs.FS, err error) fs.FS {
	if err != nil {
		// Unreachable: the paths are compile-time constants verified by go:embed.
		panic("ui: embedded files missing: " + err.Error())
	}
	return fsys
}
```

`go:embed` fails to build on an empty directory, so create the templates directory with a placeholder that Task 9 replaces:

```bash
mkdir -p internal/ui/templates
printf '<!-- replaced in Task 9 -->\n' > internal/ui/templates/base.html
```

- [ ] **Step 4: Write the asset handler**

Create `internal/platform/web/assets.go`:

```go
// Package web is the HTTP layer of the platform: request context, middleware,
// CSRF, authentication and static assets. It may depend on auth and render;
// neither depends on it.
package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
)

// Assets serves embedded static files under a prefix, with content-hashed
// URLs so they can be cached indefinitely and invalidated by deploying.
type Assets struct {
	fsys   fs.FS
	prefix string            // e.g. "/static"
	hashes map[string]string // "app.css" -> "1f3c9ab2"
}

// NewAssets hashes every file in fsys up front. The tree is embedded in the
// binary and cannot change at runtime, so hashing once at startup is both
// correct and cheap.
func NewAssets(fsys fs.FS, prefix string) (*Assets, error) {
	if prefix == "" || !strings.HasPrefix(prefix, "/") {
		return nil, fmt.Errorf("web: asset prefix %q must start with /", prefix)
	}
	a := &Assets{
		fsys:   fsys,
		prefix: strings.TrimSuffix(prefix, "/"),
		hashes: make(map[string]string),
	}

	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		sum, err := hashFile(fsys, name)
		if err != nil {
			return err
		}
		a.hashes[name] = sum
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("web: hash assets: %w", err)
	}
	if len(a.hashes) == 0 {
		return nil, fmt.Errorf("web: no static assets found")
	}
	return a, nil
}

// URL returns the cache-busting URL for an asset, for use in templates.
// An unknown name yields a path with no version, which will 404 visibly
// rather than silently rendering a broken page.
func (a *Assets) URL(name string) string {
	name = strings.TrimPrefix(name, "/")
	if sum, ok := a.hashes[name]; ok {
		return a.prefix + "/" + name + "?v=" + sum
	}
	return a.prefix + "/" + name
}

// Names lists every asset, sorted. Used by tests.
func (a *Assets) Names() []string {
	out := make([]string, 0, len(a.hashes))
	for name := range a.hashes {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Handler serves the assets. Mount it with http.StripPrefix(prefix, ...) so
// the paths it sees are relative to the tree root.
func (a *Assets) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))

		sum, ok := a.hashes[name]
		if !ok {
			http.NotFound(w, r)
			return
		}

		// Only promise immutability when the caller asked for this exact
		// content. A bare URL must still revalidate, or a stale deploy would
		// be cached for a year.
		if r.URL.Query().Get("v") == sum {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		w.Header().Set("ETag", `"`+sum+`"`)

		// ServeFileFS handles content type, Range, If-None-Match and
		// If-Modified-Since against the ETag set above.
		http.ServeFileFS(w, r, a.fsys, name)
	})
}

func hashFile(fsys fs.FS, name string) (string, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	// Eight hex characters is 32 bits: ample for cache busting, and short
	// enough to keep URLs readable in logs.
	return hex.EncodeToString(h.Sum(nil))[:8], nil
}
```

- [ ] **Step 5: Extract stack assembly out of serve**

`serve()` currently does configuration, storage, migrations and routing in one function, and this plan adds five more things to it. Move the HTTP wiring to its own file now, while it is still small.

Create `cmd/onsuite/stack.go`:

```go
package main

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

// stackDeps is everything the HTTP stack needs. It exists so buildStack has
// one parameter rather than six, and so tests can substitute pieces.
type stackDeps struct {
	DB      *sql.DB
	Log     *slog.Logger
	Version string
}

// buildStack assembles the complete HTTP handler. Later tasks in this plan add
// the renderer, middleware and app registry here; serve() stays concerned only
// with configuration, storage and the listener.
func buildStack(deps stackDeps) (http.Handler, error) {
	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", healthzHandler(deps.Version, deps.DB))
	mux.Handle("GET /static/", http.StripPrefix("/static", assets.Handler()))

	return mux, nil
}
```

Then in `cmd/onsuite/serve.go`, replace these three lines:

```go
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", healthzHandler(version, handle))

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
```

with:

```go
	handler, err := buildStack(stackDeps{DB: handle, Log: log, Version: version})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
```

And fix the stale comment on the `pinger` type, which currently refers to a plan task number that means nothing outside that document:

```go
// pinger is satisfied by *sql.DB. It is an interface rather than a concrete
// type so healthz can be tested without a database.
type pinger interface {
	PingContext(context.Context) error
}
```

- [ ] **Step 6: Test the asset handler**

Create `internal/platform/web/assets_test.go`:

```go
package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/iliafrenkel/on-suite/internal/ui"
)

func testAssets(t *testing.T) *Assets {
	t.Helper()
	a, err := NewAssets(fstest.MapFS{
		"app.css":          {Data: []byte("body{color:red}")},
		"htmx.min.js":      {Data: []byte("/* htmx */")},
		"icons/sprite.svg": {Data: []byte("<svg></svg>")},
	}, "/static")
	if err != nil {
		t.Fatalf("NewAssets: %v", err)
	}
	return a
}

func TestAssetURLIsContentHashed(t *testing.T) {
	a := testAssets(t)

	got := a.URL("app.css")
	if !regexp.MustCompile(`^/static/app\.css\?v=[0-9a-f]{8}$`).MatchString(got) {
		t.Errorf("URL(app.css) = %q, want /static/app.css?v=<8 hex>", got)
	}
	if a.URL("app.css") != got {
		t.Error("URL is not stable across calls")
	}
	if a.URL("htmx.min.js") == got {
		t.Error("different files produced the same URL")
	}
	if sub := a.URL("icons/sprite.svg"); !strings.HasPrefix(sub, "/static/icons/sprite.svg?v=") {
		t.Errorf("nested asset URL = %q", sub)
	}
	// A leading slash is tolerated, because templates are written both ways.
	if a.URL("/app.css") != got {
		t.Error("URL should tolerate a leading slash")
	}
}

// TestAssetURLOfUnknownFileFailsVisibly: a typo in a template must produce a
// 404 you notice, not a silently broken page.
func TestAssetURLOfUnknownFileFailsVisibly(t *testing.T) {
	a := testAssets(t)
	got := a.URL("nope.css")
	if strings.Contains(got, "?v=") {
		t.Errorf("URL(nope.css) = %q, want no version for an unknown file", got)
	}

	rec := httptest.NewRecorder()
	serve(a).ServeHTTP(rec, httptest.NewRequest("GET", got, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// serve mounts the handler the way buildStack does, so tests exercise the
// real prefix-stripping arrangement.
func serve(a *Assets) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static", a.Handler()))
	return mux
}

func TestAssetHandlerServesContentAndType(t *testing.T) {
	a := testAssets(t)
	rec := httptest.NewRecorder()
	serve(a).ServeHTTP(rec, httptest.NewRequest("GET", a.URL("app.css"), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != "body{color:red}" {
		t.Errorf("body = %q", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", ct)
	}
}

// TestAssetCachingPolicy is the point of the whole hashing scheme: a
// versioned URL is immutable for a year, a bare one must revalidate.
func TestAssetCachingPolicy(t *testing.T) {
	a := testAssets(t)
	h := serve(a)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", a.URL("app.css"), nil))
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("versioned URL Cache-Control = %q, want immutable", cc)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/static/app.css", nil))
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("bare URL Cache-Control = %q, want no-cache", cc)
	}

	// A stale version string must not be served as immutable.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/static/app.css?v=00000000", nil))
	if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "immutable") {
		t.Errorf("stale version Cache-Control = %q, must not be immutable", cc)
	}
}

func TestAssetHandlerRevalidatesWithETag(t *testing.T) {
	a := testAssets(t)
	h := serve(a)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", a.URL("app.css"), nil))
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag was set")
	}

	req := httptest.NewRequest("GET", a.URL("app.css"), nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Errorf("status = %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("304 response carried %d bytes of body", rec.Body.Len())
	}
}

func TestNewAssetsRejectsBadInput(t *testing.T) {
	if _, err := NewAssets(fstest.MapFS{}, "/static"); err == nil {
		t.Error("NewAssets accepted an empty filesystem")
	}
	if _, err := NewAssets(fstest.MapFS{"a.css": {Data: []byte("x")}}, "static"); err == nil {
		t.Error("NewAssets accepted a prefix without a leading slash")
	}
}

// TestRealAssetsAreEmbedded checks the actual embedded tree, so a missing
// go:embed directive or a deleted file fails the build rather than the page.
func TestRealAssetsAreEmbedded(t *testing.T) {
	a, err := NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatalf("NewAssets on the embedded tree: %v", err)
	}
	for _, want := range []string{"app.css", "htmx.min.js"} {
		if !strings.Contains(strings.Join(a.Names(), ","), want) {
			t.Errorf("%s is not embedded; got %v", want, a.Names())
		}
	}
}
```

- [ ] **Step 7: Run everything**

```bash
gofmt -l . && go vet ./... && go test ./... -race
```

Expected: clean gofmt, no vet output, all packages PASS.

- [ ] **Step 8: Verify in a browser**

```bash
go run ./cmd/onsuite serve --data-dir /tmp/onsuite-dev
```

Then:

```bash
curl -sI localhost:8080/static/app.css | grep -i cache-control
```

Expected: `Cache-Control: no-cache` for the bare URL. Fetch it with the `?v=` suffix that `URL()` produces and the header becomes `public, max-age=31536000, immutable`. Opening `http://localhost:8080/static/app.css` in a browser shows the stylesheet.

- [ ] **Step 9: Commit**

Vendored files get their own commit so the diff stays reviewable:

```bash
git add internal/ui/static/htmx.min.js internal/ui/static/VENDOR.md
git commit -m "Vendor HTMX 2.x

Committed rather than fetched from a CDN: the binary must serve a working
page on a host with no outbound internet access. VENDOR.md records the
version and SHA-256 so the file can be re-verified later."

git add internal/ui internal/platform/web cmd/onsuite
git commit -m "Add the design system and content-hashed static assets

Assets are hashed once at startup, which is correct because the tree is
embedded and cannot change at runtime. A URL carrying the current hash is
served immutable for a year; a bare or stale URL must revalidate, so a deploy
invalidates caches without any cache-busting ritual.

The design system is entirely token-driven and apps compose those tokens
rather than introducing their own colours or spacing. That constraint is what
will make four separately-built apps look like one suite.

HTTP wiring moves from serve() into buildStack() before the rest of this plan
lands five more things on it."
```

---
# Task 9: Template renderer, base layout, and shared HTML assertions

**Files:**
- Create: `internal/platform/render/render.go`
- Create: `internal/ui/templates/base.html` (replaces the Task 8 placeholder)
- Create: `internal/ui/templates/error.html`
- Create: `internal/htmlassert/htmlassert.go`
- Modify: `internal/ui/static/app.css` (append the shell rules)
- Modify: `cmd/onsuite/stack.go` (build the renderer)
- Test: `internal/platform/render/render_test.go`

**Interfaces:**
- Consumes: `(*web.Assets).URL` from Task 8, `ui.Templates()`.
- Produces:
  - `render.NewRenderer(opts render.Options) (*render.Renderer, error)` with `Options{Layouts fs.FS; AssetURL func(string) string}`
  - `(*Renderer).AddApp(id string, templates fs.FS) error`
  - `(*Renderer).Page(w http.ResponseWriter, status int, name string, p Page) error`
  - `(*Renderer).Fragment(w http.ResponseWriter, status int, page, block string, data any) error`
  - `(*Renderer).Has(name string) bool`
  - `render.Page{Title string; Shell Shell; Data any}`
  - `render.Shell{LoggedIn bool; Username string; IsAdmin bool; Apps []NavItem; ActiveApp, CSRFToken, Version string}`
  - `render.NavItem{ID, Name, Path string}`
  - `htmlassert.Parse`, `.Query`, `.QueryAll`, `.Text`, `.Attr`, `.MustHave`, `.MustNotHave`

**Design notes:**
- A template file may define `content`, making it a page, and any number of other named blocks. `Page` executes `base` from that file's set; `Fragment` executes one named block from it. This keeps an HTMX partial next to the markup it is part of, which is the whole point of HTMX.
- App templates register as `<appID>/<basename>`, so `paste/list.html` becomes `paste/list`. Names cannot collide across apps.
- `render` imports nothing from the platform. Everything it renders arrives as an argument.

**About the new dependency.** `internal/htmlassert` imports `golang.org/x/net/html`, so `x/net` becomes a real entry in `go.mod` rather than a test-only one. The intent is still that it is used only from tests, and the architecture test in Task 14 **enforces** that mechanically: no non-test file may import `htmlassert`. That is a better guarantee than a comment.

- [ ] **Step 1: Write the renderer**

Create `internal/platform/render/render.go`:

```go
// Package render composes HTML templates.
//
// It deliberately imports nothing else from the platform. Everything it needs
// to draw a page — who is logged in, what belongs in the nav, the CSRF token —
// arrives as an argument, so rendering is a pure function of its inputs and
// can be tested without constructing a request.
package render

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
)

// NavItem is one entry in the app switcher.
type NavItem struct {
	ID   string
	Name string
	Path string
}

// Shell is everything the surrounding chrome needs. It is a flat struct of
// plain values rather than a reference to a user record, so a template cannot
// reach through it into the database.
type Shell struct {
	LoggedIn  bool
	Username  string
	IsAdmin   bool
	Apps      []NavItem
	ActiveApp string
	CSRFToken string
	Version   string
}

// Page is the argument to Page. Data is the page's own view model.
type Page struct {
	Title string
	Shell Shell
	Data  any
}

type Options struct {
	// Layouts holds base.html and the platform's own pages.
	Layouts fs.FS
	// AssetURL resolves a static file name to a cache-busting URL. Injected
	// rather than imported so render does not depend on the web package.
	AssetURL func(string) string
}

// Renderer holds one parsed template set per page.
type Renderer struct {
	base  *template.Template
	pages map[string]*template.Template
	funcs template.FuncMap
}

// NewRenderer parses the shared layout and every page in opts.Layouts.
//
// All parsing happens at startup: a broken template is a startup failure
// rather than a 500 discovered by a user.
func NewRenderer(opts Options) (*Renderer, error) {
	if opts.Layouts == nil {
		return nil, fmt.Errorf("render: Layouts must not be nil")
	}
	if opts.AssetURL == nil {
		return nil, fmt.Errorf("render: AssetURL must not be nil")
	}

	r := &Renderer{
		pages: make(map[string]*template.Template),
		funcs: template.FuncMap{"asset": opts.AssetURL},
	}

	// base.html and any *.partial.html are shared by every page.
	base, err := template.New("base").Funcs(r.funcs).ParseFS(opts.Layouts, "base.html")
	if err != nil {
		return nil, fmt.Errorf("render: parse base.html: %w", err)
	}
	r.base = base

	pages, err := fs.Glob(opts.Layouts, "*.html")
	if err != nil {
		return nil, fmt.Errorf("render: glob layouts: %w", err)
	}
	for _, name := range pages {
		if name == "base.html" {
			continue
		}
		if err := r.addPage(strings.TrimSuffix(name, ".html"), opts.Layouts, name); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// AddApp registers every *.html at the root of templates under the app's id.
// Called once per app at startup, before the server listens.
func (r *Renderer) AddApp(id string, templates fs.FS) error {
	if id == "" {
		return fmt.Errorf("render: app id must not be empty")
	}
	names, err := fs.Glob(templates, "*.html")
	if err != nil {
		return fmt.Errorf("render: glob %s templates: %w", id, err)
	}
	if len(names) == 0 {
		return fmt.Errorf("render: app %s has no templates", id)
	}
	for _, name := range names {
		key := id + "/" + strings.TrimSuffix(path.Base(name), ".html")
		if err := r.addPage(key, templates, name); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) addPage(key string, fsys fs.FS, name string) error {
	if _, exists := r.pages[key]; exists {
		return fmt.Errorf("render: template %q is already registered", key)
	}
	clone, err := r.base.Clone()
	if err != nil {
		return fmt.Errorf("render: clone base for %s: %w", key, err)
	}
	if _, err := clone.ParseFS(fsys, name); err != nil {
		return fmt.Errorf("render: parse %s: %w", name, err)
	}
	r.pages[key] = clone
	return nil
}

// Has reports whether a template is registered. Used by tests and by the
// error renderer, which must not recurse if its own template is missing.
func (r *Renderer) Has(name string) bool {
	_, ok := r.pages[name]
	return ok
}

// Names lists registered templates, sorted. Used by tests.
func (r *Renderer) Names() []string {
	out := make([]string, 0, len(r.pages))
	for k := range r.pages {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Page renders a full document.
func (r *Renderer) Page(w http.ResponseWriter, status int, name string, p Page) error {
	t, ok := r.pages[name]
	if !ok {
		return fmt.Errorf("render: no such template %q", name)
	}
	return r.execute(w, status, t, "base", p)
}

// Fragment renders one named block from a page's template set, for HTMX
// swaps. Keeping partials in the same file as the page they belong to is what
// makes an HTMX codebase readable.
func (r *Renderer) Fragment(w http.ResponseWriter, status int, page, block string, data any) error {
	t, ok := r.pages[page]
	if !ok {
		return fmt.Errorf("render: no such template %q", page)
	}
	if t.Lookup(block) == nil {
		return fmt.Errorf("render: template %q has no block %q", page, block)
	}
	return r.execute(w, status, t, block, data)
}

// execute renders into a buffer before touching the ResponseWriter, so a
// template error becomes a clean 500 rather than a half-written page with a
// 200 status already committed.
func (r *Renderer) execute(w http.ResponseWriter, status int, t *template.Template, name string, data any) error {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		return fmt.Errorf("render: execute %s: %w", name, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}
```

- [ ] **Step 2: Write the base layout**

Create `internal/ui/templates/base.html`, replacing the Task 8 placeholder:

```html
{{define "base"}}<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{with .Title}}{{.}} · {{end}}ON Suite</title>
<link rel="stylesheet" href="{{asset "app.css"}}">
<script src="{{asset "htmx.min.js"}}" defer></script>
</head>
<body hx-headers='{"X-CSRF-Token": "{{.Shell.CSRFToken}}"}'>
{{template "shell" .Shell}}
<main>
{{block "content" .}}{{end}}
</main>
</body>
</html>{{end}}

{{define "shell"}}
<header class="shell-bar">
	<a class="shell-mark" href="/">ON Suite</a>
	<nav class="shell-nav" aria-label="Applications">
		{{range .Apps}}
		<a href="{{.Path}}"{{if eq .ID $.ActiveApp}} aria-current="page"{{end}}>{{.Name}}</a>
		{{end}}
	</nav>
	{{if .LoggedIn}}
	<div class="shell-user">
		<span>{{.Username}}</span>
		<form method="post" action="/logout">
			<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
			<button type="submit" class="quiet">Log out</button>
		</form>
	</div>
	{{end}}
</header>
{{end}}
```

- [ ] **Step 3: Write the error page**

Create `internal/ui/templates/error.html`:

```html
{{define "content"}}
<div class="measure stack">
	<h1>{{.Data.Status}} — {{.Data.Title}}</h1>
	<p class="dim">{{.Data.Message}}</p>
	<p><a href="/">Back to ON Suite</a></p>
</div>
{{end}}

{{/* The same content as an HTMX swap: no document, no shell. */}}
{{define "fragment"}}
<div class="notice notice-error">{{.Title}} — {{.Message}}</div>
{{end}}
```

- [ ] **Step 4: Append the shell rules to the stylesheet**

Add to the end of `internal/ui/static/app.css`:

```css
/* ---- Shell chrome additions ------------------------------------------- */

.shell-user form { display: inline; }

/* A button that reads as a link: used for actions that must be POSTs, such as
 * logging out, without shouting like a primary button. */
button.quiet {
	padding: var(--s-1) var(--s-2);
	background: transparent;
	border-color: transparent;
	color: var(--c-text-dim);
	font-weight: 400;
}

button.quiet:hover { background: var(--c-bg-inset); color: var(--c-text); }
```

- [ ] **Step 5: Write the shared HTML assertion helpers**

Create `internal/htmlassert/htmlassert.go`:

```go
// Package htmlassert parses HTML in tests and asserts on its structure.
//
// It exists so handler tests never compare markup as strings. A string
// comparison breaks whenever a class name or whitespace changes, which trains
// you to ignore failures; a structural assertion breaks only when the meaning
// changes.
//
// This package must only ever be imported from _test.go files. The
// architecture test in internal/arch enforces that.
package htmlassert

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// Doc is a parsed document.
type Doc struct {
	t    *testing.T
	root *html.Node
}

// Parse parses body, failing the test if it is not valid HTML.
func Parse(t *testing.T, body string) *Doc {
	t.Helper()
	root, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("htmlassert: parse: %v", err)
	}
	return &Doc{t: t, root: root}
}

// Query returns the first element matching the selector, or nil.
//
// Supported: "tag", ".class", "#id", "[attr]", "[attr=value]", a tag with one
// of those appended (`a.shell-mark`, `input[name=user]`), and descendant
// combinations separated by spaces (`nav.shell-nav a`).
func (d *Doc) Query(selector string) *html.Node {
	matches := d.QueryAll(selector)
	if len(matches) == 0 {
		return nil
	}
	return matches[0]
}

// QueryAll returns every element matching the selector, in document order.
func (d *Doc) QueryAll(selector string) []*html.Node {
	parts := strings.Fields(selector)
	if len(parts) == 0 {
		d.t.Fatal("htmlassert: empty selector")
	}
	compiled := make([]matcher, len(parts))
	for i, part := range parts {
		compiled[i] = parseSelector(d.t, part)
	}
	last := compiled[len(compiled)-1]
	ancestors := compiled[:len(compiled)-1]

	// One walk in document order, so results are ordered without sorting.
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && last.matches(n) && ancestorsSatisfied(n.Parent, ancestors) {
			out = append(out, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(d.root)
	return out
}

// ancestorsSatisfied walks up from n looking for the ancestor selectors in
// right-to-left order, the way a CSS descendant combinator resolves.
func ancestorsSatisfied(n *html.Node, sels []matcher) bool {
	if len(sels) == 0 {
		return true
	}
	i := len(sels) - 1
	for cur := n; cur != nil; cur = cur.Parent {
		if cur.Type == html.ElementNode && sels[i].matches(cur) {
			i--
			if i < 0 {
				return true
			}
		}
	}
	return false
}

// MustHave asserts at least one element matches, and returns the first.
func (d *Doc) MustHave(selector string) *html.Node {
	d.t.Helper()
	n := d.Query(selector)
	if n == nil {
		d.t.Fatalf("htmlassert: no element matches %q", selector)
	}
	return n
}

// MustNotHave asserts nothing matches. Used for the negative cases that
// matter most: a logged-out page must not contain a logout button.
func (d *Doc) MustNotHave(selector string) {
	d.t.Helper()
	if n := d.Query(selector); n != nil {
		d.t.Fatalf("htmlassert: %q matched but should not exist (found <%s>)", selector, n.Data)
	}
}

// Text is the concatenated text content of a node, with runs of whitespace
// collapsed so assertions do not depend on template indentation.
func Text(n *html.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

// Attr returns an attribute value and whether it was present.
func Attr(n *html.Node, name string) (string, bool) {
	if n == nil {
		return "", false
	}
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}

// Text on the document is shorthand for the whole body.
func (d *Doc) Text() string { return Text(d.root) }

// matcher is one compiled selector component.
type matcher struct {
	tag   string
	attr  string
	value string
	kind  selectorKind
}

type selectorKind int

const (
	byTag selectorKind = iota
	byClass
	byID
	byAttrPresent
	byAttrValue
)

func parseSelector(t *testing.T, selector string) matcher {
	t.Helper()
	s := strings.TrimSpace(selector)
	if s == "" {
		t.Fatal("htmlassert: empty selector")
	}

	// Split an optional leading tag name from the qualifier.
	i := strings.IndexAny(s, ".#[")
	if i < 0 {
		return matcher{tag: s, kind: byTag}
	}
	tag, qualifier := s[:i], s[i:]

	switch {
	case strings.HasPrefix(qualifier, "."):
		return matcher{tag: tag, attr: "class", value: qualifier[1:], kind: byClass}
	case strings.HasPrefix(qualifier, "#"):
		return matcher{tag: tag, attr: "id", value: qualifier[1:], kind: byID}
	case strings.HasPrefix(qualifier, "[") && strings.HasSuffix(qualifier, "]"):
		inner := qualifier[1 : len(qualifier)-1]
		name, val, found := strings.Cut(inner, "=")
		if !found {
			return matcher{tag: tag, attr: name, kind: byAttrPresent}
		}
		return matcher{tag: tag, attr: name, value: strings.Trim(val, `"'`), kind: byAttrValue}
	}
	t.Fatalf("htmlassert: unsupported selector %q", selector)
	return matcher{}
}

func (m matcher) matches(n *html.Node) bool {
	if m.tag != "" && n.Data != m.tag {
		return false
	}
	switch m.kind {
	case byTag:
		return true
	case byClass:
		classes, _ := Attr(n, "class")
		for _, c := range strings.Fields(classes) {
			if c == m.value {
				return true
			}
		}
		return false
	case byID, byAttrValue:
		got, ok := Attr(n, m.attr)
		return ok && got == m.value
	case byAttrPresent:
		_, ok := Attr(n, m.attr)
		return ok
	}
	return false
}
```

- [ ] **Step 6: Build the renderer in the stack**

In `cmd/onsuite/stack.go`, add the imports and build the renderer:

```go
	rend, err := render.NewRenderer(render.Options{
		Layouts:  ui.Templates(),
		AssetURL: assets.URL,
	})
	if err != nil {
		return nil, err
	}
```

Add `"github.com/iliafrenkel/on-suite/internal/platform/render"` to the imports, and add a `Render *render.Renderer` field to `stackDeps` is **not** needed — the renderer is created inside `buildStack`. Store it in a local and pass it to the pieces added by later tasks.

To prove it works end to end now, add a temporary root route (Task 13 replaces it):

```go
	mux.Handle("GET /{$}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := rend.Page(w, http.StatusOK, "error", render.Page{
			Title: "ON Suite",
			Shell: render.Shell{Version: deps.Version},
			Data: map[string]any{
				"Status": http.StatusOK, "Title": "It works",
				"Message": "The renderer and the design system are wired up.",
			},
		}); err != nil {
			deps.Log.Error("render root", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	}))
```

- [ ] **Step 7: Test the renderer**

Create `internal/platform/render/render_test.go`:

```go
package render_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

func testRenderer(t *testing.T) *render.Renderer {
	t.Helper()
	r, err := render.NewRenderer(render.Options{
		Layouts:  ui.Templates(),
		AssetURL: func(name string) string { return "/static/" + name + "?v=test" },
	})
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	return r
}

func TestNewRendererParsesTheRealTemplates(t *testing.T) {
	r := testRenderer(t)
	if !r.Has("error") {
		t.Errorf("error template not registered; got %v", r.Names())
	}
}

func TestPageRendersADocumentWithTheShell(t *testing.T) {
	r := testRenderer(t)
	rec := httptest.NewRecorder()

	err := r.Page(rec, http.StatusOK, "error", render.Page{
		Title: "Not found",
		Shell: render.Shell{
			LoggedIn:  true,
			Username:  "ilia",
			CSRFToken: "tok123",
			ActiveApp: "paste",
			Apps: []render.NavItem{
				{ID: "paste", Name: "ON Paste", Path: "/paste/"},
				{ID: "notes", Name: "ON Notes", Path: "/notes/"},
			},
		},
		Data: map[string]any{"Status": 404, "Title": "Not found", "Message": "no such page"},
	})
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}

	doc := htmlassert.Parse(t, rec.Body.String())

	if got := htmlassert.Text(doc.MustHave("title")); got != "Not found · ON Suite" {
		t.Errorf("title = %q", got)
	}

	// The asset function must have been applied, not the raw filename.
	if href, _ := htmlassert.Attr(doc.MustHave("link[rel=stylesheet]"), "href"); href != "/static/app.css?v=test" {
		t.Errorf("stylesheet href = %q", href)
	}

	// The nav renders every app, and marks the active one.
	links := doc.QueryAll("nav.shell-nav a")
	if len(links) != 2 {
		t.Fatalf("nav has %d links, want 2", len(links))
	}
	if got := htmlassert.Text(links[0]); got != "ON Paste" {
		t.Errorf("first nav link = %q", got)
	}
	if _, ok := htmlassert.Attr(links[0], "aria-current"); !ok {
		t.Error("active app is not marked with aria-current")
	}
	if _, ok := htmlassert.Attr(links[1], "aria-current"); ok {
		t.Error("inactive app is marked with aria-current")
	}

	doc.MustHave(".shell-user")
	if got := htmlassert.Text(doc.MustHave(".shell-user span")); got != "ilia" {
		t.Errorf("username = %q", got)
	}
}

// TestPageOmitsUserChromeWhenLoggedOut is the negative case that matters: the
// login page must not offer a logout button.
func TestPageOmitsUserChromeWhenLoggedOut(t *testing.T) {
	r := testRenderer(t)
	rec := httptest.NewRecorder()

	if err := r.Page(rec, http.StatusOK, "error", render.Page{
		Shell: render.Shell{LoggedIn: false},
		Data:  map[string]any{"Status": 401, "Title": "Unauthorised", "Message": "log in"},
	}); err != nil {
		t.Fatal(err)
	}
	htmlassert.Parse(t, rec.Body.String()).MustNotHave(".shell-user")
}

// TestPageEscapesUntrustedValues guards the property html/template exists to
// provide. If this ever fails, stop and find out why.
func TestPageEscapesUntrustedValues(t *testing.T) {
	r := testRenderer(t)
	rec := httptest.NewRecorder()

	const attack = `<script>alert(1)</script>`
	if err := r.Page(rec, http.StatusOK, "error", render.Page{
		Shell: render.Shell{LoggedIn: true, Username: attack},
		Data:  map[string]any{"Status": 200, "Title": "x", "Message": attack},
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Body.String(), "<script>alert(1)</script>") {
		t.Fatal("untrusted input was rendered unescaped")
	}
	// It must still be present as text.
	if !strings.Contains(htmlassert.Parse(t, rec.Body.String()).Text(), "alert(1)") {
		t.Error("escaped value disappeared entirely")
	}
}

// TestCSRFTokenReachesHTMX covers the mechanism every non-GET HTMX request in
// the suite depends on. html/template escapes inside attribute values, so
// assert on the parsed attribute rather than the raw markup.
func TestCSRFTokenReachesHTMX(t *testing.T) {
	r := testRenderer(t)
	rec := httptest.NewRecorder()
	if err := r.Page(rec, http.StatusOK, "error", render.Page{
		Shell: render.Shell{CSRFToken: "tok-abc-123"},
		Data:  map[string]any{"Status": 200, "Title": "x", "Message": "y"},
	}); err != nil {
		t.Fatal(err)
	}

	doc := htmlassert.Parse(t, rec.Body.String())
	got, ok := htmlassert.Attr(doc.MustHave("body"), "hx-headers")
	if !ok {
		t.Fatal("body has no hx-headers attribute")
	}
	if !strings.Contains(got, "tok-abc-123") {
		t.Errorf("hx-headers = %q, does not carry the token", got)
	}
	if !strings.Contains(got, "X-CSRF-Token") {
		t.Errorf("hx-headers = %q, does not name the header", got)
	}
}

func TestFragmentRendersWithoutTheDocument(t *testing.T) {
	r := testRenderer(t)
	rec := httptest.NewRecorder()

	if err := r.Fragment(rec, http.StatusNotFound, "error", "fragment",
		map[string]any{"Status": 404, "Message": "gone"}); err != nil {
		t.Fatalf("Fragment: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<html") || strings.Contains(body, "shell-bar") {
		t.Errorf("fragment contains document chrome: %q", body)
	}
	if !strings.Contains(body, "gone") {
		t.Errorf("fragment missing its data: %q", body)
	}
}

func TestAddApp(t *testing.T) {
	r := testRenderer(t)
	app := fstest.MapFS{
		"list.html": {Data: []byte(`{{define "content"}}<ul id="items">{{range .Data}}{{template "row" .}}{{end}}</ul>{{end}}
		                            {{define "row"}}<li class="row">{{.}}</li>{{end}}`)},
	}
	if err := r.AddApp("paste", app); err != nil {
		t.Fatalf("AddApp: %v", err)
	}
	if !r.Has("paste/list") {
		t.Fatalf("paste/list not registered; got %v", r.Names())
	}

	rec := httptest.NewRecorder()
	if err := r.Page(rec, http.StatusOK, "paste/list", render.Page{
		Data: []string{"one", "two"},
	}); err != nil {
		t.Fatal(err)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if rows := doc.QueryAll("li.row"); len(rows) != 2 {
		t.Errorf("rendered %d rows, want 2", len(rows))
	}

	// The same block, rendered alone as an HTMX swap.
	rec = httptest.NewRecorder()
	if err := r.Fragment(rec, http.StatusOK, "paste/list", "row", "three"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `<li class="row">three</li>` {
		t.Errorf("fragment = %q", got)
	}
}

func TestRendererRejectsBadInput(t *testing.T) {
	if _, err := render.NewRenderer(render.Options{AssetURL: func(string) string { return "" }}); err == nil {
		t.Error("NewRenderer accepted a nil Layouts")
	}
	if _, err := render.NewRenderer(render.Options{Layouts: ui.Templates()}); err == nil {
		t.Error("NewRenderer accepted a nil AssetURL")
	}

	r := testRenderer(t)
	if err := r.AddApp("", fstest.MapFS{"a.html": {Data: []byte("x")}}); err == nil {
		t.Error("AddApp accepted an empty id")
	}
	if err := r.AddApp("empty", fstest.MapFS{}); err == nil {
		t.Error("AddApp accepted an app with no templates")
	}

	app := fstest.MapFS{"list.html": {Data: []byte(`{{define "content"}}x{{end}}`)}}
	if err := r.AddApp("dup", app); err != nil {
		t.Fatal(err)
	}
	if err := r.AddApp("dup", app); err == nil {
		t.Error("AddApp allowed the same app to register twice")
	}

	rec := httptest.NewRecorder()
	if err := r.Page(rec, http.StatusOK, "nope", render.Page{}); err == nil {
		t.Error("Page rendered an unregistered template")
	}
	if err := r.Fragment(rec, http.StatusOK, "error", "no-such-block", nil); err == nil {
		t.Error("Fragment rendered a nonexistent block")
	}
}
```

- [ ] **Step 8: Run everything**

```bash
go get golang.org/x/net@latest
go mod tidy
gofmt -l . && go vet ./... && go test ./... -race
```

`go mod tidy` matters here: without it `x/net` stays in the indirect block, and the dependency check in this plan's Definition of Done will not match.

Expected: clean, all PASS.

- [ ] **Step 9: Look at it in a browser**

```bash
go run ./cmd/onsuite serve --data-dir /tmp/onsuite-dev
```

Open `http://localhost:8080/`. Expected: the shell bar with the ON Suite mark, and the "It works" message, styled. Toggle your OS between light and dark appearance and reload — the palette must follow. View source and confirm there is no inline `<script>` and no `style=` attribute.

- [ ] **Step 10: Commit**

```bash
git add go.mod go.sum internal cmd
git commit -m "Add the template renderer, base layout and HTML test helpers

The renderer imports nothing else from the platform: who is logged in, the
nav contents and the CSRF token all arrive as arguments. That keeps render
and web from importing each other, and makes template output testable without
constructing a request.

A template file may define content, making it a page, plus any number of
named blocks that can be rendered alone as HTMX swaps. Partials therefore live
next to the markup they belong to. All parsing happens at startup, so a broken
template fails the boot rather than a user's request, and pages render into a
buffer first so a template error cannot produce a half-written 200.

htmlassert lets handler tests assert on parsed structure instead of comparing
markup as strings. It must only be imported from tests; the architecture test
in Task 14 enforces that."
```

---
# Task 10: Middleware and error pages

**Files:**
- Create: `internal/platform/web/context.go`
- Create: `internal/platform/web/middleware.go`
- Create: `internal/platform/web/errors.go`
- Modify: `cmd/onsuite/stack.go` (wrap the mux)
- Test: `internal/platform/web/middleware_test.go`, `internal/platform/web/errors_test.go`

**Interfaces:**
- Consumes: `render.Renderer` from Task 9.
- Produces:
  - `web.Chain(h http.Handler, mw ...Middleware) http.Handler`, `type Middleware func(http.Handler) http.Handler`
  - `web.Recover(log *slog.Logger, e *Errors) Middleware`
  - `web.RequestLog(log *slog.Logger) Middleware`
  - `web.SecurityHeaders() Middleware`
  - `web.NewErrors(r *render.Renderer, log *slog.Logger) *Errors`
  - `(*Errors).Status(w, r, status int)`, `(*Errors).Internal(w, r, err error)`, `(*Errors).NotFound(w, r)`
  - `web.WithActiveApp(ctx, id)`, `web.ActiveApp(ctx) string`
  - `web.WithCSRFToken(ctx, token)`, `web.CSRFToken(ctx) string`
  - `web.WithUser(ctx, auth.User)`, `web.UserFrom(ctx) (auth.User, bool)`
  - `web.IsHTMX(r *http.Request) bool`

**Design notes:**
- **The CSP is strict and load-bearing.** `script-src 'self'` means no inline script anywhere, ever. `hx-*` are HTML attributes, not inline script, so HTMX works — but an `onclick=` or a `<script>` block will silently stop working in a browser while tests still pass. That is why the constraint is stated at the top of this plan.
- Error responses are HTMX-aware: a failed swap returns a fragment, not a whole document nested inside the page.
- `Recover` must not leak a panic message to the browser. It logs the panic with a stack and renders a generic 500.

- [ ] **Step 1: Write the request context**

Create `internal/platform/web/context.go`:

```go
package web

import (
	"context"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
)

// ctxKey is unexported so nothing outside this package can collide with or
// overwrite these values.
type ctxKey int

const (
	ctxKeyActiveApp ctxKey = iota
	ctxKeyCSRFToken
	ctxKeyUser
)

// WithActiveApp records which app is handling the request, so the shell can
// mark it in the nav.
func WithActiveApp(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyActiveApp, id)
}

// ActiveApp returns the app id, or "" outside any app.
func ActiveApp(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyActiveApp).(string)
	return id
}

// WithCSRFToken stores the token for this request. The middleware that calls
// this arrives in Task 11.
func WithCSRFToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, ctxKeyCSRFToken, token)
}

// CSRFToken returns the token, or "" if none has been issued.
func CSRFToken(ctx context.Context) string {
	token, _ := ctx.Value(ctxKeyCSRFToken).(string)
	return token
}

// WithUser stores the authenticated user. The middleware that calls this
// arrives in Task 12.
func WithUser(ctx context.Context, u auth.User) context.Context {
	return context.WithValue(ctx, ctxKeyUser, u)
}

// UserFrom returns the authenticated user, and false when nobody is logged in.
func UserFrom(ctx context.Context) (auth.User, bool) {
	u, ok := ctx.Value(ctxKeyUser).(auth.User)
	return u, ok
}

// IsHTMX reports whether the request came from HTMX, which decides between
// returning a fragment and a whole document.
func IsHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}
```

All four accessors live here, in one file, even though the middleware that
populates two of them arrives in Tasks 11 and 12. That is deliberate: it keeps
every task in this plan independently compilable, and it keeps the context keys
— the one thing that must not be duplicated — in a single place.

- [ ] **Step 2: Write the error renderer**

Create `internal/platform/web/errors.go`:

```go
package web

import (
	"log/slog"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
)

// Errors renders error responses. It is a type rather than a set of functions
// because it needs the renderer and the logger.
type Errors struct {
	render *render.Renderer
	log    *slog.Logger
}

func NewErrors(r *render.Renderer, log *slog.Logger) *Errors {
	return &Errors{render: r, log: log}
}

// titles keeps user-facing wording in one place. Anything not listed gets a
// generic message, which is deliberate: an error page is not the place to
// explain the internals.
var titles = map[int]struct{ title, message string }{
	http.StatusBadRequest:            {"Bad request", "That request could not be understood."},
	http.StatusUnauthorized:          {"Sign in required", "Please sign in to continue."},
	http.StatusForbidden:             {"Not allowed", "You do not have access to that."},
	http.StatusNotFound:              {"Not found", "There is nothing at that address."},
	http.StatusMethodNotAllowed:      {"Not allowed", "That method is not supported here."},
	http.StatusRequestEntityTooLarge: {"Too large", "That was larger than the limit."},
	http.StatusTooManyRequests:       {"Slow down", "Too many attempts. Try again shortly."},
	http.StatusInternalServerError:   {"Something broke", "An unexpected error occurred. It has been logged."},
}

// Status renders an error page for a status code.
func (e *Errors) Status(w http.ResponseWriter, r *http.Request, status int) {
	t, ok := titles[status]
	if !ok {
		t = titles[http.StatusInternalServerError]
	}

	data := map[string]any{"Status": status, "Title": t.title, "Message": t.message}

	// An HTMX swap must not receive a whole document; it would end up nested
	// inside the page it was swapped into.
	if IsHTMX(r) {
		if err := e.render.Fragment(w, status, "error", "fragment", data); err != nil {
			e.fallback(w, status, err)
		}
		return
	}

	page := render.Page{
		Title: t.title,
		Shell: render.Shell{ActiveApp: ActiveApp(r.Context())},
		Data:  data,
	}
	if u, ok := UserFrom(r.Context()); ok {
		page.Shell.LoggedIn = true
		page.Shell.Username = u.Username
		page.Shell.IsAdmin = u.IsAdmin
	}
	page.Shell.CSRFToken = CSRFToken(r.Context())

	if err := e.render.Page(w, status, "error", page); err != nil {
		e.fallback(w, status, err)
	}
}

// NotFound is the http.Handler form, for mux fallbacks.
func (e *Errors) NotFound(w http.ResponseWriter, r *http.Request) {
	e.Status(w, r, http.StatusNotFound)
}

// Internal logs the real cause and shows the user nothing about it.
func (e *Errors) Internal(w http.ResponseWriter, r *http.Request, err error) {
	e.log.Error("request failed",
		"error", err, "method", r.Method, "path", r.URL.Path)
	e.Status(w, r, http.StatusInternalServerError)
}

// fallback runs when even the error template failed. Plain text, because
// whatever we tried has already proven unreliable.
func (e *Errors) fallback(w http.ResponseWriter, status int, err error) {
	e.log.Error("rendering the error page failed", "error", err, "status", status)
	http.Error(w, http.StatusText(status), status)
}
```

- [ ] **Step 3: Write the middleware**

Create `internal/platform/web/middleware.go`:

```go
package web

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middleware so that the first listed is the outermost, which is
// the order they read in.
func Chain(h http.Handler, mw ...Middleware) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// statusRecorder captures the status code so the request log can report it.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written int64
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.written += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the real writer, so flushing and
// hijacking still work through this wrapper.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// RequestLog logs one line per request after it completes.
func RequestLog(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(rec, r)

			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			level := slog.LevelInfo
			if rec.status >= 500 {
				level = slog.LevelError
			}
			log.Log(r.Context(), level, "request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.written,
				"duration_ms", time.Since(start).Milliseconds(),
				"htmx", IsHTMX(r),
			)
		})
	}
}

// Recover turns a panic into a logged 500 instead of a dropped connection.
func Recover(log *slog.Logger, e *Errors) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				// http.ErrAbortHandler is the documented way for a handler to
				// abandon a response deliberately; do not report it as a bug.
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Error("panic serving request",
					"panic", v,
					"method", r.Method,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				// The panic value may contain internals, so the user gets the
				// generic page and nothing else.
				e.Status(w, r, http.StatusInternalServerError)
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// contentSecurityPolicy forbids inline script and style, which is what makes
// a whole class of injection bugs unexploitable even if escaping fails.
//
// HTMX works under this policy: hx-* are HTML attributes, not inline script.
// An onclick= handler or a <script> block will not.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'none'; " +
	"object-src 'none'"

// SecurityHeaders sets response headers that do not depend on the route.
func SecurityHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", contentSecurityPolicy)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("X-Frame-Options", "DENY")
			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 4: Wrap the mux in the stack**

In `cmd/onsuite/stack.go`, after building the renderer, create the error renderer and wrap the mux. Replace `return mux, nil` with:

```go
	errs := web.NewErrors(rend, deps.Log)
	mux.Handle("/", http.HandlerFunc(errs.NotFound))

	return web.Chain(mux,
		web.Recover(deps.Log, errs),
		web.RequestLog(deps.Log),
		web.SecurityHeaders(),
	), nil
```

`mux.Handle("/", ...)` is the catch-all: without it, `ServeMux` returns a bare 404 with no styling.

- [ ] **Step 5: Test the middleware**

Create `internal/platform/web/middleware_test.go`:

```go
package web

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// logCapture collects structured log records so tests can assert on them.
func logCapture() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func lastRecord(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("nothing was logged")
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &rec); err != nil {
		t.Fatalf("log line is not JSON: %v (%q)", err, lines[len(lines)-1])
	}
	return rec
}

func TestChainOrdersOutermostFirst(t *testing.T) {
	var order []string
	mw := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	h := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		order = append(order, "handler")
	}), mw("first"), mw("second"))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	want := "first,second,handler"
	if got := strings.Join(order, ","); got != want {
		t.Errorf("order = %q, want %q", got, want)
	}
}

func TestRequestLogRecordsTheOutcome(t *testing.T) {
	log, buf := logCapture()
	h := RequestLog(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hello"))
	}))

	req := httptest.NewRequest("POST", "/paste/new", nil)
	req.Header.Set("HX-Request", "true")
	h.ServeHTTP(httptest.NewRecorder(), req)

	rec := lastRecord(t, buf)
	if rec["msg"] != "request" {
		t.Errorf("msg = %v", rec["msg"])
	}
	if rec["method"] != "POST" || rec["path"] != "/paste/new" {
		t.Errorf("method/path = %v %v", rec["method"], rec["path"])
	}
	if rec["status"] != float64(http.StatusCreated) {
		t.Errorf("status = %v, want 201", rec["status"])
	}
	if rec["bytes"] != float64(5) {
		t.Errorf("bytes = %v, want 5", rec["bytes"])
	}
	if rec["htmx"] != true {
		t.Errorf("htmx = %v, want true", rec["htmx"])
	}
}

// TestRequestLogDefaultsToOK covers a handler that writes nothing at all,
// which would otherwise log status 0.
func TestRequestLogDefaultsToOK(t *testing.T) {
	log, buf := logCapture()
	RequestLog(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	if got := lastRecord(t, buf)["status"]; got != float64(200) {
		t.Errorf("status = %v, want 200", got)
	}
}

func TestRequestLogEscalatesServerErrors(t *testing.T) {
	log, buf := logCapture()
	RequestLog(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	if got := lastRecord(t, buf)["level"]; got != "ERROR" {
		t.Errorf("level = %v, want ERROR for a 500", got)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "script-src 'self'", "frame-ancestors 'none'", "object-src 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q is missing %q", csp, want)
		}
	}
	// unsafe-inline would defeat the point of having a policy at all.
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
		t.Errorf("CSP allows unsafe inline code: %q", csp)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "same-origin" {
		t.Errorf("Referrer-Policy = %q", got)
	}
}
```

- [ ] **Step 6: Test the error renderer and recovery**

Create `internal/platform/web/errors_test.go`:

```go
package web_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

func testErrors(t *testing.T) (*web.Errors, *bytes.Buffer) {
	t.Helper()
	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatal(err)
	}
	rend, err := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	return web.NewErrors(rend, slog.New(slog.NewJSONHandler(&buf, nil))), &buf
}

func TestErrorsStatusRendersAPage(t *testing.T) {
	e, _ := testErrors(t)
	rec := httptest.NewRecorder()
	e.Status(rec, httptest.NewRequest("GET", "/nope", nil), http.StatusNotFound)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("header.shell-bar") // it is a full document
	if body := doc.Text(); !strings.Contains(body, "Not found") {
		t.Errorf("page does not say what went wrong: %q", body)
	}
}

// TestErrorsStatusReturnsAFragmentForHTMX: a swap must not receive a whole
// document, or the page ends up nested inside itself.
func TestErrorsStatusReturnsAFragmentForHTMX(t *testing.T) {
	e, _ := testErrors(t)
	req := httptest.NewRequest("GET", "/nope", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.Status(rec, req, http.StatusNotFound)

	body := rec.Body.String()
	if strings.Contains(body, "<html") || strings.Contains(body, "shell-bar") {
		t.Errorf("HTMX error response contains document chrome: %q", body)
	}
	if !strings.Contains(body, "Not found") {
		t.Errorf("fragment does not say what went wrong: %q", body)
	}
}

// TestErrorsInternalHidesTheCause is the important one: the operator sees the
// real error, the user does not.
func TestErrorsInternalHidesTheCause(t *testing.T) {
	e, logged := testErrors(t)
	rec := httptest.NewRecorder()

	e.Internal(rec, httptest.NewRequest("GET", "/x", nil),
		errors.New("connection to secret-host:5432 refused"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret-host") {
		t.Error("the internal error message leaked into the response")
	}
	if !strings.Contains(logged.String(), "secret-host") {
		t.Error("the internal error was not logged")
	}
}

func TestErrorsUnknownStatusFallsBackTo500Wording(t *testing.T) {
	e, _ := testErrors(t)
	rec := httptest.NewRecorder()
	e.Status(rec, httptest.NewRequest("GET", "/x", nil), http.StatusTeapot)

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want the requested 418", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Something broke") {
		t.Error("unmapped status did not fall back to generic wording")
	}
}

func TestRecoverTurnsAPanicIntoA500(t *testing.T) {
	e, logged := testErrors(t)
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	h := web.Recover(log, e)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("database credentials are wrong")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "credentials") {
		t.Error("the panic value leaked into the response")
	}
	if !strings.Contains(buf.String(), "credentials") {
		t.Error("the panic was not logged")
	}
	if !strings.Contains(buf.String(), "stack") {
		t.Error("no stack trace was logged")
	}
	_ = logged
}

// TestRecoverRepanicsOnErrAbortHandler: that value is the documented way for a
// handler to abandon a response on purpose, and must not be reported as a bug.
func TestRecoverRepanicsOnErrAbortHandler(t *testing.T) {
	e, _ := testErrors(t)
	h := web.Recover(slog.New(slog.DiscardHandler), e)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic(http.ErrAbortHandler)
		}))

	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Errorf("recovered %v, want ErrAbortHandler to propagate", v)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}
```

- [ ] **Step 7: Run and verify**

```bash
gofmt -l . && go vet ./... && go test ./... -race
```

Then check the real headers and the styled 404:

```bash
go run ./cmd/onsuite serve --data-dir /tmp/onsuite-dev
```

```bash
curl -sI localhost:8080/ | grep -iE 'content-security|nosniff|referrer'
```

Expected: all three present. Visiting `http://localhost:8080/no-such-page` shows a styled 404 page with the shell, not Go's bare `404 page not found`. The terminal shows one JSON log line per request including `status` and `duration_ms`.

- [ ] **Step 8: Commit**

```bash
git add internal/platform/web cmd/onsuite
git commit -m "Add middleware, security headers and error pages

The Content-Security-Policy forbids inline script and style, which makes a
class of injection bugs unexploitable even if escaping somewhere fails. HTMX
is unaffected because hx-* are HTML attributes rather than inline script; an
onclick handler would not be, which is why no inline script is permitted
anywhere in the suite.

Error responses are HTMX-aware, returning a fragment to a swap rather than a
whole document that would nest inside the page. Panics and internal errors are
logged in full with a stack and shown to the user as a generic page, so an
error message cannot leak internals."
```

---
# Task 11: CSRF protection

> **Strict TDD.** Write the test, run it, watch it fail, then implement.

**Files:**
- Create: `internal/platform/web/csrf.go`
- Modify: `cmd/onsuite/stack.go`
- Test: `internal/platform/web/csrf_test.go`

**Interfaces:**
- Consumes: `web.Errors` and the context accessors from Task 10.
- Produces:
  - `web.NewCSRF(secure bool, errs *Errors) *CSRF`
  - `(*CSRF).Middleware(next http.Handler) http.Handler`
  - `(*CSRF).Rotate(w http.ResponseWriter) (string, error)`
  - `web.CSRFHeader = "X-CSRF-Token"`, `web.CSRFFormField = "csrf_token"`, `web.CSRFCookieName = "onsuite_csrf"`
  - `web.LimitBody(max int64) Middleware`

**Design — double-submit cookie.** A random token lives in an `HttpOnly` cookie. The server renders the same token into the page, so the browser sends it back two ways: in the cookie automatically, and in a header or form field deliberately. An attacker's site can cause the cookie to be sent but cannot read it, so it cannot produce the second copy.

This needs no server-side secret and no schema change. It is combined with `SameSite=Lax`, which already blocks cross-site POSTs in current browsers; the token is the defence that does not depend on that.

**Why the token must rotate on login.** Without rotation, an attacker who can set a cookie before you sign in could fix a token they know. `Rotate` is called from the login handler in Task 12.

- [ ] **Step 1: Write the failing test**

Create `internal/platform/web/csrf_test.go`:

```go
package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// csrfStack wraps a handler that reports the token it saw in context.
func csrfStack(t *testing.T, secure bool) (http.Handler, *web.CSRF) {
	t.Helper()
	e, _ := testErrors(t)
	c := web.NewCSRF(secure, e)
	h := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("token=" + web.CSRFToken(r.Context())))
	}))
	return h, c
}

// cookieFrom pulls a Set-Cookie value out of a response.
func cookieFrom(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestCSRFIssuesATokenOnFirstGET(t *testing.T) {
	h, _ := csrfStack(t, true)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	c := cookieFrom(t, rec, web.CSRFCookieName)
	if c == nil {
		t.Fatal("no CSRF cookie was set")
	}
	if len(c.Value) < 20 {
		t.Errorf("token %q is too short to be random", c.Value)
	}
	if !c.HttpOnly {
		t.Error("CSRF cookie is not HttpOnly")
	}
	if !c.Secure {
		t.Error("CSRF cookie is not Secure when secure=true")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	// The handler must see the same token, so it can render it into the page.
	if !strings.Contains(rec.Body.String(), "token="+c.Value) {
		t.Errorf("handler saw %q, cookie is %q", rec.Body.String(), c.Value)
	}
}

func TestCSRFReusesAnExistingToken(t *testing.T) {
	h, _ := csrfStack(t, false)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	first := cookieFrom(t, rec, web.CSRFCookieName)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: first.Value})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "token="+first.Value) {
		t.Error("an existing token was not reused")
	}
	if c := cookieFrom(t, rec, web.CSRFCookieName); c != nil && c.Value != first.Value {
		t.Error("a new token was issued despite a valid one being present")
	}
}

func TestCSRFAllowsSafeMethodsWithoutAToken(t *testing.T) {
	h, _ := csrfStack(t, false)
	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", method, rec.Code)
		}
	}
}

// TestCSRFRejectsUnsafeMethodsWithoutAToken is the whole point of the task.
func TestCSRFRejectsUnsafeMethodsWithoutAToken(t *testing.T) {
	h, _ := csrfStack(t, false)
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/", nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s without a token = %d, want 403", method, rec.Code)
		}
	}
}

func TestCSRFAcceptsTheTokenInAHeader(t *testing.T) {
	h, _ := csrfStack(t, false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	token := cookieFrom(t, rec, web.CSRFCookieName).Value

	req := httptest.NewRequest("POST", "/", nil)
	req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: token})
	req.Header.Set(web.CSRFHeader, token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (header token)", rec.Code)
	}
}

func TestCSRFAcceptsTheTokenInAFormField(t *testing.T) {
	h, _ := csrfStack(t, false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	token := cookieFrom(t, rec, web.CSRFCookieName).Value

	form := url.Values{web.CSRFFormField: {token}, "title": {"hello"}}
	req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: token})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (form token)", rec.Code)
	}
}

// TestCSRFFormParsingDoesNotConsumeTheBody: the middleware may need to read
// the form to find the token, and the handler must still see its own fields.
func TestCSRFFormParsingDoesNotConsumeTheBody(t *testing.T) {
	e, _ := testErrors(t)
	c := web.NewCSRF(false, e)
	h := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("title=" + r.FormValue("title")))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	token := cookieFrom(t, rec, web.CSRFCookieName).Value

	form := url.Values{web.CSRFFormField: {token}, "title": {"a snippet"}}
	req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: token})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Body.String(); got != "title=a snippet" {
		t.Errorf("handler saw %q; the middleware consumed the body", got)
	}
}

func TestCSRFRejectsAMismatchedToken(t *testing.T) {
	h, _ := csrfStack(t, false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	token := cookieFrom(t, rec, web.CSRFCookieName).Value

	tests := []struct {
		name   string
		cookie string
		sent   string
	}{
		{"wrong value", token, "not-the-token"},
		{"empty sent value", token, ""},
		{"no cookie", "", token},
		{"both empty", "", ""},
		{"token from another session", "some-other-token", token},
		{"prefix of the real token", token, token[:len(token)-1]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", nil)
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: tt.cookie})
			}
			if tt.sent != "" {
				req.Header.Set(web.CSRFHeader, tt.sent)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403", rec.Code)
			}
		})
	}
}

// TestCSRFRotateIssuesANewToken guards against session fixation of the token
// at login.
func TestCSRFRotate(t *testing.T) {
	_, c := csrfStack(t, true)

	rec := httptest.NewRecorder()
	first, err := c.Rotate(rec)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	cookie := cookieFrom(t, rec, web.CSRFCookieName)
	if cookie == nil || cookie.Value != first {
		t.Fatal("Rotate did not set the cookie to the returned token")
	}

	rec = httptest.NewRecorder()
	second, err := c.Rotate(rec)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Error("Rotate returned the same token twice")
	}
}

func TestCSRFCookieIsNotSecureOnPlainHTTP(t *testing.T) {
	h, _ := csrfStack(t, false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	// A Secure cookie is never sent over http, so a local dev server on
	// http://localhost would be unable to log in at all.
	if c := cookieFrom(t, rec, web.CSRFCookieName); c.Secure {
		t.Error("cookie is Secure even though secure=false")
	}
}

func TestLimitBody(t *testing.T) {
	h := web.LimitBody(16)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "too big", http.StatusRequestEntityTooLarge)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))

	small := strings.NewReader("a=1")
	req := httptest.NewRequest("POST", "/", small)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("small body = %d, want 200", rec.Code)
	}

	big := strings.NewReader("a=" + strings.Repeat("x", 1000))
	req = httptest.NewRequest("POST", "/", big)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Error("an oversized body was accepted")
	}
}
```

- [ ] **Step 2: Run the test and watch it fail**

```bash
go test ./internal/platform/web/ -run TestCSRF -run TestLimitBody -v
```

Expected: FAIL to build with `undefined: web.NewCSRF`, `undefined: web.CSRFCookieName`, `undefined: web.CSRFHeader`, `undefined: web.CSRFFormField`, `undefined: web.LimitBody`.

- [ ] **Step 3: Implement CSRF**

Create `internal/platform/web/csrf.go`:

```go
package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
)

const (
	// CSRFCookieName holds the token the browser returns automatically.
	CSRFCookieName = "onsuite_csrf"
	// CSRFHeader is how HTMX sends the token; see base.html.
	CSRFHeader = "X-CSRF-Token"
	// CSRFFormField is how a plain HTML form sends it.
	CSRFFormField = "csrf_token"

	csrfTokenBytes = 32
)

// CSRF implements double-submit-cookie protection.
//
// A random token lives in an HttpOnly cookie, and the server renders the same
// token into the page. The browser therefore returns it two ways: in the
// cookie automatically, and in a header or form field deliberately. Another
// origin can cause the cookie to be sent but cannot read it, so it cannot
// produce the second copy.
//
// This requires no server-side secret and no database column. SameSite=Lax
// already blocks cross-site form posts in current browsers; the token is the
// defence that does not depend on the browser getting that right.
type CSRF struct {
	secure bool
	errs   *Errors
}

func NewCSRF(secure bool, errs *Errors) *CSRF {
	return &CSRF{secure: secure, errs: errs}
}

// Middleware issues a token when there is none, verifies it on unsafe
// methods, and puts it in the request context for templates to render.
func (c *CSRF) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if cookie, err := r.Cookie(CSRFCookieName); err == nil {
			token = cookie.Value
		}

		if token == "" {
			fresh, err := c.Rotate(w)
			if err != nil {
				c.errs.Internal(w, r, err)
				return
			}
			token = fresh
		}

		if !safeMethod(r.Method) && !c.verify(r, token) {
			// Deliberately vague: a mismatch is either an attack or a stale
			// tab, and neither is helped by detail.
			c.errs.Status(w, r, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r.WithContext(WithCSRFToken(r.Context(), token)))
	})
}

// Rotate issues a new token and sets the cookie. Called at login, so that a
// token an attacker planted before sign-in cannot survive it.
func (c *CSRF) Rotate(w http.ResponseWriter) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return token, nil
}

// verify compares the token the browser sent deliberately with the one it
// sent automatically.
func (c *CSRF) verify(r *http.Request, cookieToken string) bool {
	if cookieToken == "" {
		return false
	}

	sent := r.Header.Get(CSRFHeader)
	if sent == "" {
		// ParseForm caches into r.PostForm, so the handler can still read its
		// own fields afterwards. It only touches the body for form content
		// types, leaving other bodies for the handler to stream.
		if err := r.ParseForm(); err == nil {
			sent = r.PostFormValue(CSRFFormField)
		}
	}
	if sent == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(sent), []byte(cookieToken)) == 1
}

func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

func randomToken() (string, error) {
	buf := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("web: generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// DefaultMaxBodyBytes bounds request bodies. A megabyte is generous for a text
// snippet and small enough that a runaway upload cannot exhaust memory. An app
// needing more should wrap its own routes rather than raising this globally.
const DefaultMaxBodyBytes = 1 << 20

// LimitBody caps how much of a request body will be read.
func LimitBody(max int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, max)
			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 4: Run the tests and watch them pass**

```bash
go test ./internal/platform/web/ -race -v
```

Expected: all PASS.

- [ ] **Step 5: Add CSRF to the stack**

In `cmd/onsuite/stack.go`, add a `Secure bool` field to `stackDeps`, then extend the chain. `Secure` must be true whenever the site is served over HTTPS, and false for a plain-HTTP dev server — a `Secure` cookie is never sent over `http://`, so getting this backwards makes login silently impossible.

```go
	csrf := web.NewCSRF(deps.Secure, errs)

	return web.Chain(mux,
		web.Recover(deps.Log, errs),
		web.RequestLog(deps.Log),
		web.SecurityHeaders(),
		web.LimitBody(web.DefaultMaxBodyBytes),
		csrf.Middleware,
	), nil
```

In `cmd/onsuite/serve.go`, set it from the configuration:

```go
	handler, err := buildStack(stackDeps{
		DB:      handle,
		Log:     log,
		Version: version,
		Secure:  cfg.TLSDomain != "",
	})
```

- [ ] **Step 6: Commit**

```bash
git add internal/platform/web cmd/onsuite
git commit -m "Add double-submit-cookie CSRF protection

A random token lives in an HttpOnly cookie and is rendered into the page, so
the browser returns it both automatically and deliberately. Another origin can
cause the cookie to be sent but cannot read it, so it cannot produce the
second copy. This needs no server secret and no schema change, and does not
rely on SameSite being honoured.

Unsafe methods are rejected by default, accepted only with a matching token
compared in constant time. The token rotates at login so one planted before
sign-in cannot survive it. Form parsing caches into PostForm, so a handler
still sees its own fields after the middleware has looked for the token."
```

---

# Task 12: Authentication — guards, login, logout, rate limiting

> **Strict TDD.** This is the component every app depends on.

**Files:**
- Create: `internal/platform/web/login.go`
- Create: `internal/ui/templates/login.html`
- Modify: `cmd/onsuite/stack.go`
- Test: `internal/platform/web/login_test.go`

**Interfaces:**
- Consumes: `auth.Store`, `auth.VerifyPassword` (Plan 1); `Errors`, context accessors (Task 10); `CSRF.Rotate` (Task 11).
- Produces:
  - `web.NewAuth(opts web.AuthOptions) *web.Auth` with `AuthOptions{Users *auth.Store; Render *render.Renderer; Errors *Errors; CSRF *CSRF; Log *slog.Logger; Secure bool}`
  - `(*Auth).LoadUser(next http.Handler) http.Handler` — never blocks; populates context when a session is valid
  - `(*Auth).RequireUser(next http.Handler) http.Handler` — blocks anonymous requests
  - `(*Auth).Routes(mux *http.ServeMux)` — registers `GET /login`, `POST /login`, `POST /logout`
  - `(*Auth).SetClock(func() time.Time)` — test seam
  - `web.SessionCookieName = "onsuite_session"`

**Design notes:**
- `LoadUser` and `RequireUser` are separate so a page can render differently for anonymous visitors without every route deciding whether to redirect. `LoadUser` runs globally; `RequireUser` wraps individual routes, and Task 13 makes that the default.
- **Login must not reveal whether a username exists.** Identical wording for both cases, and when the user is unknown a dummy Argon2 verification still runs, so response timing does not distinguish them either.
- Rate limiting is in memory, keyed by username and client address. Restarting clears it, which is acceptable for a suite with a handful of users and avoids a schema and a write on every failed attempt.
- After login, an anonymous `GET` destination captured in `?next=` is honoured — but only if it is a local path, or an open redirect is created.

- [ ] **Step 1: Write the failing test**

Create `internal/platform/web/login_test.go`:

```go
package web_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

const testPassword = "a-sufficiently-long-password"

// authFixture builds the real stack over a real database with one account.
type authFixture struct {
	handler http.Handler
	auth    *web.Auth
	users   *auth.Store
	user    auth.User
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	ms, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(context.Background(), handle, ms); err != nil {
		t.Fatal(err)
	}

	users := auth.NewStore(handle)
	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	user, err := users.CreateUser(context.Background(), "ilia", hash, true)
	if err != nil {
		t.Fatal(err)
	}

	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatal(err)
	}
	rend, err := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL})
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	errs := web.NewErrors(rend, log)
	csrf := web.NewCSRF(false, errs)

	a := web.NewAuth(web.AuthOptions{
		Users: users, Render: rend, Errors: errs, CSRF: csrf, Log: log, Secure: false,
	})

	mux := http.NewServeMux()
	a.Routes(mux)
	mux.Handle("GET /private", a.RequireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := web.UserFrom(r.Context())
		if !ok {
			t.Error("RequireUser admitted a request with no user in context")
		}
		_, _ = w.Write([]byte("private for " + u.Username))
	})))
	mux.Handle("GET /open", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := web.UserFrom(r.Context()); ok {
			_, _ = w.Write([]byte("open for " + u.Username))
			return
		}
		_, _ = w.Write([]byte("open for nobody"))
	}))

	handler := web.Chain(mux, a.LoadUser, csrf.Middleware)
	return &authFixture{handler: handler, auth: a, users: users, user: user}
}

// get issues a GET carrying the given cookies.
func (f *authFixture) do(t *testing.T, req *http.Request, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

// logIn performs a real login and returns the session and CSRF cookies.
func (f *authFixture) logIn(t *testing.T, username, password string) []*http.Cookie {
	t.Helper()

	page := f.do(t, httptest.NewRequest("GET", "/login", nil))
	csrfCookie := cookieFrom(t, page, web.CSRFCookieName)
	if csrfCookie == nil {
		t.Fatal("GET /login did not issue a CSRF cookie")
	}

	form := url.Values{
		"username":        {username},
		"password":        {password},
		web.CSRFFormField: {csrfCookie.Value},
	}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := f.do(t, req, csrfCookie)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /login = %d, want 303; body: %s", rec.Code, rec.Body.String())
	}
	return rec.Result().Cookies()
}

func TestLoginPageRenders(t *testing.T) {
	f := newAuthFixture(t)
	rec := f.do(t, httptest.NewRequest("GET", "/login", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(`input[name=username]`)
	doc.MustHave(`input[name=password]`)
	doc.MustHave(`input[name=` + web.CSRFFormField + `]`)
	// Nobody is logged in yet, so there must be no logout control.
	doc.MustNotHave(".shell-user")

	if typ, _ := htmlassert.Attr(doc.MustHave(`input[name=password]`), "type"); typ != "password" {
		t.Errorf("password field type = %q", typ)
	}
}

func TestSuccessfulLoginSetsASessionAndGrantsAccess(t *testing.T) {
	f := newAuthFixture(t)
	cookies := f.logIn(t, "ilia", testPassword)

	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == web.SessionCookieName {
			session = c
		}
	}
	if session == nil {
		t.Fatal("login did not set a session cookie")
	}
	if !session.HttpOnly {
		t.Error("session cookie is not HttpOnly")
	}
	if session.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", session.SameSite)
	}
	if len(session.Value) < 20 {
		t.Errorf("session id %q is too short", session.Value)
	}

	rec := f.do(t, httptest.NewRequest("GET", "/private", nil), session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /private after login = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "private for ilia") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

// TestLoginRotatesTheCSRFToken guards against a token planted before sign-in.
func TestLoginRotatesTheCSRFToken(t *testing.T) {
	f := newAuthFixture(t)

	page := f.do(t, httptest.NewRequest("GET", "/login", nil))
	before := cookieFrom(t, page, web.CSRFCookieName).Value

	form := url.Values{
		"username": {"ilia"}, "password": {testPassword}, web.CSRFFormField: {before},
	}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := f.do(t, req, &http.Cookie{Name: web.CSRFCookieName, Value: before})

	after := cookieFrom(t, rec, web.CSRFCookieName)
	if after == nil {
		t.Fatal("login did not re-issue a CSRF cookie")
	}
	if after.Value == before {
		t.Error("the CSRF token survived login unchanged")
	}
}

func TestRequireUserRejectsAnonymous(t *testing.T) {
	f := newAuthFixture(t)
	rec := f.do(t, httptest.NewRequest("GET", "/private", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 to the login page", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/login") {
		t.Errorf("Location = %q, want /login...", loc)
	}
	if !strings.Contains(loc, "next=") {
		t.Errorf("Location = %q, does not remember where the user was going", loc)
	}
}

// TestRequireUserAnswersHTMXWithAStatusNotARedirect: HTMX would swap the
// login page into a fragment of the current page, which is useless.
func TestRequireUserRejectsAnonymousHTMXWithAHeader(t *testing.T) {
	f := newAuthFixture(t)
	req := httptest.NewRequest("GET", "/private", nil)
	req.Header.Set("HX-Request", "true")
	rec := f.do(t, req)

	if got := rec.Header().Get("HX-Redirect"); got == "" {
		t.Errorf("no HX-Redirect header; HTMX cannot act on a 303 body swap (status was %d)", rec.Code)
	}
}

// TestLoadUserDoesNotBlock: a public page must render for anonymous visitors
// and still know the user when there is one.
func TestLoadUserDoesNotBlock(t *testing.T) {
	f := newAuthFixture(t)

	rec := f.do(t, httptest.NewRequest("GET", "/open", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "open for nobody") {
		t.Fatalf("anonymous: %d %q", rec.Code, rec.Body.String())
	}

	cookies := f.logIn(t, "ilia", testPassword)
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == web.SessionCookieName {
			session = c
		}
	}
	rec = f.do(t, httptest.NewRequest("GET", "/open", nil), session)
	if !strings.Contains(rec.Body.String(), "open for ilia") {
		t.Errorf("logged in: %q", rec.Body.String())
	}
}

// TestFailedLoginIsIndistinguishable is the security property: an attacker
// must not be able to enumerate usernames.
func TestFailedLoginIsIndistinguishable(t *testing.T) {
	f := newAuthFixture(t)

	attempt := func(username, password string) (int, string) {
		page := f.do(t, httptest.NewRequest("GET", "/login", nil))
		csrfCookie := cookieFrom(t, page, web.CSRFCookieName)
		form := url.Values{
			"username": {username}, "password": {password},
			web.CSRFFormField: {csrfCookie.Value},
		}
		req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := f.do(t, req, csrfCookie)
		return rec.Code, htmlassert.Parse(t, rec.Body.String()).Text()
	}

	wrongPasswordCode, wrongPasswordText := attempt("ilia", "definitely-not-the-password")
	noSuchUserCode, noSuchUserText := attempt("nosuchperson", "definitely-not-the-password")

	if wrongPasswordCode != noSuchUserCode {
		t.Errorf("status differs: %d for a real user, %d for an unknown one",
			wrongPasswordCode, noSuchUserCode)
	}
	if wrongPasswordText != noSuchUserText {
		t.Errorf("wording differs and leaks whether an account exists:\n  real:    %q\n  unknown: %q",
			wrongPasswordText, noSuchUserText)
	}
	if wrongPasswordCode == http.StatusSeeOther {
		t.Error("a wrong password produced a redirect, meaning it succeeded")
	}
	if !strings.Contains(strings.ToLower(wrongPasswordText), "incorrect") {
		t.Errorf("the failure is not explained to the user: %q", wrongPasswordText)
	}
}

func TestLoginRejectsBadRequests(t *testing.T) {
	f := newAuthFixture(t)

	tests := []struct{ name, username, password string }{
		{"empty username", "", testPassword},
		{"empty password", "ilia", ""},
		{"both empty", "", ""},
		{"wrong case password", "ilia", strings.ToUpper(testPassword)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := f.do(t, httptest.NewRequest("GET", "/login", nil))
			csrfCookie := cookieFrom(t, page, web.CSRFCookieName)
			form := url.Values{
				"username": {tt.username}, "password": {tt.password},
				web.CSRFFormField: {csrfCookie.Value},
			}
			req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := f.do(t, req, csrfCookie)

			if rec.Code == http.StatusSeeOther {
				t.Error("login succeeded")
			}
			for _, c := range rec.Result().Cookies() {
				if c.Name == web.SessionCookieName && c.Value != "" {
					t.Error("a session cookie was issued for a failed login")
				}
			}
		})
	}
}

// TestUsernameIsCaseInsensitiveAtLogin: Plan 1 folded usernames for
// uniqueness; the login path must use the same folding.
func TestUsernameIsCaseInsensitiveAtLogin(t *testing.T) {
	f := newAuthFixture(t)
	for _, name := range []string{"ilia", "Ilia", "ILIA"} {
		cookies := f.logIn(t, name, testPassword)
		found := false
		for _, c := range cookies {
			if c.Name == web.SessionCookieName {
				found = true
			}
		}
		if !found {
			t.Errorf("login as %q did not produce a session", name)
		}
	}
}

func TestLogoutRevokesTheSession(t *testing.T) {
	f := newAuthFixture(t)
	cookies := f.logIn(t, "ilia", testPassword)

	var session, csrfCookie *http.Cookie
	for _, c := range cookies {
		switch c.Name {
		case web.SessionCookieName:
			session = c
		case web.CSRFCookieName:
			csrfCookie = c
		}
	}

	form := url.Values{web.CSRFFormField: {csrfCookie.Value}}
	req := httptest.NewRequest("POST", "/logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := f.do(t, req, session, csrfCookie)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /logout = %d, want 303", rec.Code)
	}

	// The old cookie must be dead server-side, not merely cleared in the
	// browser: a stolen cookie has to stop working.
	rec = f.do(t, httptest.NewRequest("GET", "/private", nil), session)
	if rec.Code == http.StatusOK {
		t.Error("the session still works after logout")
	}
}

func TestLogoutRequiresCSRF(t *testing.T) {
	f := newAuthFixture(t)
	cookies := f.logIn(t, "ilia", testPassword)
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == web.SessionCookieName {
			session = c
		}
	}

	rec := f.do(t, httptest.NewRequest("POST", "/logout", nil), session)
	if rec.Code != http.StatusForbidden {
		t.Errorf("logout without a CSRF token = %d, want 403", rec.Code)
	}
}

func TestTamperedSessionCookieIsRejected(t *testing.T) {
	f := newAuthFixture(t)
	cookies := f.logIn(t, "ilia", testPassword)
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == web.SessionCookieName {
			session = c
		}
	}

	tampered := &http.Cookie{Name: web.SessionCookieName, Value: session.Value[:len(session.Value)-2] + "xy"}
	rec := f.do(t, httptest.NewRequest("GET", "/private", nil), tampered)
	if rec.Code == http.StatusOK {
		t.Error("a tampered session cookie was accepted")
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	f := newAuthFixture(t)
	cookies := f.logIn(t, "ilia", testPassword)
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == web.SessionCookieName {
			session = c
		}
	}

	// Move the whole stack past the session lifetime.
	future := time.Now().UTC().Add(auth.SessionTTL + time.Hour)
	f.users.SetClock(func() time.Time { return future })
	f.auth.SetClock(func() time.Time { return future })

	rec := f.do(t, httptest.NewRequest("GET", "/private", nil), session)
	if rec.Code == http.StatusOK {
		t.Error("an expired session was accepted")
	}
}

// TestLoginRateLimit: repeated failures must stop being answered, or a weak
// password is brute-forceable at HTTP speed.
func TestLoginRateLimit(t *testing.T) {
	f := newAuthFixture(t)

	var lastCode int
	for i := range 25 {
		page := f.do(t, httptest.NewRequest("GET", "/login", nil))
		csrfCookie := cookieFrom(t, page, web.CSRFCookieName)
		form := url.Values{
			"username": {"ilia"}, "password": {"wrong-password-attempt"},
			web.CSRFFormField: {csrfCookie.Value},
		}
		req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := f.do(t, req, csrfCookie)
		lastCode = rec.Code
		if lastCode == http.StatusTooManyRequests {
			break
		}
		if i == 24 {
			t.Fatalf("25 failed attempts were all answered normally (last status %d)", lastCode)
		}
	}

	// While limited, even the correct password must be refused.
	page := f.do(t, httptest.NewRequest("GET", "/login", nil))
	csrfCookie := cookieFrom(t, page, web.CSRFCookieName)
	form := url.Values{
		"username": {"ilia"}, "password": {testPassword},
		web.CSRFFormField: {csrfCookie.Value},
	}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := f.do(t, req, csrfCookie)
	if rec.Code == http.StatusSeeOther {
		t.Error("the rate limit was bypassed by supplying the correct password")
	}
}

// TestLoginNextParameterCannotBecomeAnOpenRedirect.
func TestLoginNextIsRestrictedToLocalPaths(t *testing.T) {
	f := newAuthFixture(t)

	tests := []struct{ name, next, wantPrefix string }{
		{"local path is honoured", "/private", "/private"},
		{"absolute url is refused", "https://evil.example.com/x", "/"},
		{"scheme-relative url is refused", "//evil.example.com/x", "/"},
		{"backslash trick is refused", "/\\evil.example.com", "/"},
		{"non-path is refused", "javascript:alert(1)", "/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := f.do(t, httptest.NewRequest("GET", "/login", nil))
			csrfCookie := cookieFrom(t, page, web.CSRFCookieName)
			form := url.Values{
				"username": {"ilia"}, "password": {testPassword},
				web.CSRFFormField: {csrfCookie.Value}, "next": {tt.next},
			}
			req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := f.do(t, req, csrfCookie)

			loc := rec.Header().Get("Location")
			if !strings.HasPrefix(loc, tt.wantPrefix) {
				t.Errorf("Location = %q, want prefix %q", loc, tt.wantPrefix)
			}
			if strings.Contains(loc, "evil.example.com") {
				t.Errorf("open redirect: Location = %q", loc)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test and watch it fail**

```bash
go test ./internal/platform/web/ -run 'TestLogin|TestLogout|TestRequireUser|TestLoadUser|TestSession|TestExpired|TestTampered|TestFailed|TestUsername|TestSuccessful' -v
```

Expected: FAIL to build with `undefined: web.NewAuth`, `undefined: web.AuthOptions`, `undefined: web.SessionCookieName`.

- [ ] **Step 3: Write the login template**

Create `internal/ui/templates/login.html`:

```html
{{define "content"}}
<div class="centered stack">
	<h1>Sign in</h1>

	{{with .Data.Error}}
	<div class="notice notice-error" role="alert">{{.}}</div>
	{{end}}

	<form method="post" action="/login" class="stack">
		<input type="hidden" name="csrf_token" value="{{.Shell.CSRFToken}}">
		<input type="hidden" name="next" value="{{.Data.Next}}">

		<div class="field">
			<label for="username">Username</label>
			<input id="username" name="username" type="text"
			       autocomplete="username" autocapitalize="none"
			       spellcheck="false" required autofocus
			       value="{{.Data.Username}}">
		</div>

		<div class="field">
			<label for="password">Password</label>
			<input id="password" name="password" type="password"
			       autocomplete="current-password" required>
		</div>

		<button type="submit">Sign in</button>
	</form>
</div>
{{end}}
```

- [ ] **Step 4: Implement authentication**

Create `internal/platform/web/login.go`:

```go
package web

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
)

// SessionCookieName holds the opaque session id.
const SessionCookieName = "onsuite_session"

const (
	// maxLoginAttempts before a key is locked out.
	maxLoginAttempts = 10
	// loginWindow is how long attempts are remembered, and therefore how long
	// a lockout lasts after the last attempt.
	loginWindow = 15 * time.Minute
)

// loginFailedMessage is deliberately identical for a wrong password and an
// unknown account. Distinguishing them would let anyone enumerate usernames.
const loginFailedMessage = "That username or password is incorrect."

type AuthOptions struct {
	Users  *auth.Store
	Render *render.Renderer
	Errors *Errors
	CSRF   *CSRF
	Log    *slog.Logger
	// Secure marks cookies Secure. It must be false on a plain-HTTP dev
	// server, because a Secure cookie is never sent over http and login
	// would fail with no visible reason.
	Secure bool
}

// Auth turns a session cookie into a known user, and owns the login and
// logout endpoints.
type Auth struct {
	users  *auth.Store
	render *render.Renderer
	errs   *Errors
	csrf   *CSRF
	log    *slog.Logger
	secure bool

	now     func() time.Time
	limiter *attemptLimiter
}

func NewAuth(opts AuthOptions) *Auth {
	return &Auth{
		users:   opts.Users,
		render:  opts.Render,
		errs:    opts.Errors,
		csrf:    opts.CSRF,
		log:     opts.Log,
		secure:  opts.Secure,
		now:     func() time.Time { return time.Now().UTC() },
		limiter: newAttemptLimiter(),
	}
}

// SetClock replaces the time source, so expiry can be tested without waiting.
func (a *Auth) SetClock(now func() time.Time) { a.now = now }

// Routes registers the endpoints that must exist outside any app.
func (a *Auth) Routes(mux *http.ServeMux) {
	mux.Handle("GET /login", http.HandlerFunc(a.loginForm))
	mux.Handle("POST /login", http.HandlerFunc(a.loginSubmit))
	mux.Handle("POST /logout", http.HandlerFunc(a.logout))
}

// LoadUser puts the current user in the request context when the session
// cookie names a live session. It never blocks: deciding what anonymous means
// is the route's business, not this middleware's.
func (a *Auth) LoadUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil || cookie.Value == "" {
			next.ServeHTTP(w, r)
			return
		}

		sess, err := a.users.UseSession(r.Context(), cookie.Value)
		if err != nil {
			if !errors.Is(err, auth.ErrNotFound) {
				a.log.Error("session lookup failed", "error", err)
			}
			// Unknown, tampered or expired: clear the cookie so the browser
			// stops sending it, and continue as anonymous.
			a.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}

		user, err := a.users.UserByID(r.Context(), sess.UserID)
		if err != nil {
			// The session outlived its user. Revoke it.
			if derr := a.users.DeleteSession(r.Context(), sess.ID); derr != nil {
				a.log.Error("revoking an orphaned session failed", "error", derr)
			}
			a.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}

		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
	})
}

// RequireUser blocks anonymous requests. Task 13 makes this the default for
// every app route, so that forgetting to protect a route is impossible.
func (a *Auth) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFrom(r.Context()); ok {
			next.ServeHTTP(w, r)
			return
		}

		target := "/login"
		if r.Method == http.MethodGet && !IsHTMX(r) {
			target += "?next=" + url.QueryEscape(r.URL.RequestURI())
		}

		// HTMX would swap the login page into whatever element triggered the
		// request. HX-Redirect tells it to navigate instead.
		if IsHTMX(r) {
			w.Header().Set("HX-Redirect", target)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	})
}

func (a *Auth) loginForm(w http.ResponseWriter, r *http.Request) {
	// Already signed in: nothing to do here.
	if _, ok := UserFrom(r.Context()); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	a.renderLogin(w, r, http.StatusOK, "", "", safeNext(r.URL.Query().Get("next")))
}

func (a *Auth) loginSubmit(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))

	if username == "" || password == "" {
		a.renderLogin(w, r, http.StatusBadRequest, loginFailedMessage, username, next)
		return
	}

	key := strings.ToLower(username) + "|" + clientIP(r)
	if !a.limiter.allow(key, a.now()) {
		a.log.Warn("login rate limit reached", "username", username, "ip", clientIP(r))
		a.errs.Status(w, r, http.StatusTooManyRequests)
		return
	}

	user, err := a.users.UserByUsername(r.Context(), username)
	if err != nil {
		if !errors.Is(err, auth.ErrNotFound) {
			a.errs.Internal(w, r, err)
			return
		}
		// Spend the same work as a real verification, so response timing does
		// not reveal whether the account exists.
		auth.DummyVerify(password)
		a.limiter.record(key, a.now())
		a.renderLogin(w, r, http.StatusUnauthorized, loginFailedMessage, username, next)
		return
	}

	ok, err := auth.VerifyPassword(user.PasswordHash, password)
	if err != nil {
		// A stored hash that will not parse is corruption, not a bad password.
		a.errs.Internal(w, r, err)
		return
	}
	if !ok {
		a.limiter.record(key, a.now())
		a.renderLogin(w, r, http.StatusUnauthorized, loginFailedMessage, username, next)
		return
	}

	a.limiter.clear(key)

	sess, err := a.users.CreateSession(r.Context(), user.ID)
	if err != nil {
		a.errs.Internal(w, r, err)
		return
	}

	// Rotate the CSRF token so one planted before sign-in cannot survive it.
	if _, err := a.csrf.Rotate(w); err != nil {
		a.errs.Internal(w, r, err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sess.ID,
		Path:     "/",
		Expires:  sess.ExpiresAt,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
	a.log.Info("signed in", "username", user.Username, "ip", clientIP(r))

	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
		// Delete the row, not just the cookie: a copied cookie must stop
		// working, which is the whole reason sessions live in the database.
		if err := a.users.DeleteSession(r.Context(), cookie.Value); err != nil {
			a.log.Error("deleting a session on logout failed", "error", err)
		}
	}
	a.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *Auth) renderLogin(w http.ResponseWriter, r *http.Request, status int, message, username, next string) {
	page := render.Page{
		Title: "Sign in",
		Shell: render.Shell{CSRFToken: CSRFToken(r.Context())},
		Data: map[string]any{
			"Error":    message,
			"Username": username,
			"Next":     next,
		},
	}
	if err := a.render.Page(w, status, "login", page); err != nil {
		a.errs.Internal(w, r, err)
	}
}

func (a *Auth) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// safeNext keeps ?next= from becoming an open redirect. Only a rooted local
// path is allowed: anything with a scheme, a host, or a leading "//" or "/\"
// could send the user to another site after login.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") {
		return "/"
	}
	if strings.HasPrefix(next, "//") || strings.HasPrefix(next, `/\`) {
		return "/"
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return "/"
	}
	return u.RequestURI()
}

// clientIP is the best available identifier for rate limiting. It reads
// RemoteAddr only: a forwarded header is attacker-controlled unless a trusted
// proxy is known to set it, and trusting one here would make the limiter
// trivially bypassable.
func clientIP(r *http.Request) string {
	host, _, err := netSplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// attemptLimiter counts recent failures per key.
//
// In memory on purpose: a restart clearing it is acceptable for a suite with a
// handful of users, and it avoids a schema plus a database write on every
// failed guess.
type attemptLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func newAttemptLimiter() *attemptLimiter {
	return &attemptLimiter{attempts: make(map[string][]time.Time)}
}

// allow reports whether another attempt may be made.
func (l *attemptLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key, now)) < maxLoginAttempts
}

// record notes a failure.
func (l *attemptLimiter) record(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts[key] = append(l.prune(key, now), now)
}

// clear forgets a key after a success.
func (l *attemptLimiter) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// prune drops attempts outside the window. Callers hold the lock.
func (l *attemptLimiter) prune(key string, now time.Time) []time.Time {
	cutoff := now.Add(-loginWindow)
	kept := l.attempts[key][:0]
	for _, t := range l.attempts[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.attempts[key] = kept
	return kept
}

// netSplitHostPort is net.SplitHostPort. It is aliased here so the reason for
// importing net is obvious at the call site.
var netSplitHostPort = net.SplitHostPort
```


- [ ] **Step 5: Add the timing-equalising helper to auth**

`DummyVerify` belongs next to the hashing it mimics. Add to `internal/platform/auth/password.go`:

```go
// dummyHash is a real Argon2id hash of a value nobody will guess. It exists so
// a login attempt for an unknown username can perform the same work as one for
// a real account, making the two indistinguishable by response time.
//
// Generated with the default parameters; the plaintext is irrelevant.
var dummyHash = mustHash("timing-equalisation-placeholder")

func mustHash(s string) string {
	h, err := HashPassword(s)
	if err != nil {
		panic("auth: hashing the dummy password failed: " + err.Error())
	}
	return h
}

// DummyVerify spends roughly the same time as VerifyPassword would, and
// discards the result. Call it when a username was not found.
func DummyVerify(password string) {
	_, _ = VerifyPassword(dummyHash, password)
}
```

- [ ] **Step 6: Run the tests and watch them pass**

```bash
go test ./internal/platform/web/ -race -v
```

Expected: all PASS. This package now takes several seconds because every login test runs real Argon2id at 64 MiB. That is the cost being correct.

- [ ] **Step 7: Wire authentication into the stack**

In `cmd/onsuite/stack.go`, add `Users *auth.Store` to `stackDeps`, build the `Auth`, register its routes and add `LoadUser` to the chain — outside the CSRF middleware, so the login form can render its token:

```go
	authn := web.NewAuth(web.AuthOptions{
		Users:  deps.Users,
		Render: rend,
		Errors: errs,
		CSRF:   csrf,
		Log:    deps.Log,
		Secure: deps.Secure,
	})
	authn.Routes(mux)

	return web.Chain(mux,
		web.Recover(deps.Log, errs),
		web.RequestLog(deps.Log),
		web.SecurityHeaders(),
		web.LimitBody(web.DefaultMaxBodyBytes),
		csrf.Middleware,
		authn.LoadUser,
	), nil
```

Replace the temporary root route from Task 9 with one that requires a user, so the suite is private by default even before Task 13:

```go
	mux.Handle("GET /{$}", authn.RequireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := web.UserFrom(r.Context())
		page := render.Page{
			Title: "ON Suite",
			Shell: render.Shell{
				LoggedIn: true, Username: u.Username, IsAdmin: u.IsAdmin,
				CSRFToken: web.CSRFToken(r.Context()), Version: deps.Version,
			},
			Data: map[string]any{"Status": 200, "Title": "Signed in", "Message": "No apps are registered yet."},
		}
		if err := rend.Page(w, http.StatusOK, "error", page); err != nil {
			errs.Internal(w, r, err)
		}
	})))
```

In `cmd/onsuite/serve.go`, pass the store that is already built there:

```go
	handler, err := buildStack(stackDeps{
		DB:      handle,
		Users:   users,
		Log:     log,
		Version: version,
		Secure:  cfg.TLSDomain != "",
	})
```

- [ ] **Step 8: Verify by actually logging in**

```bash
go run ./cmd/onsuite serve --data-dir /tmp/onsuite-dev
```

Open `http://localhost:8080/`. Expected: redirected to `/login?next=%2F`. Sign in with the account created in Plan 1 and you land on the signed-in page with your username and a working Log out button. Check these by hand:

- A wrong password shows "That username or password is incorrect." and stays on the page.
- A nonexistent username shows **exactly the same message**.
- `curl -s -o /dev/null -w '%{http_code}' localhost:8080/private-does-not-exist` returns 404 with the styled page.
- Log out, then press Back. The page may render from cache, but reloading sends you to `/login`.

- [ ] **Step 9: Commit**

```bash
git add internal cmd
git commit -m "Add authentication: session cookies, login, logout, rate limiting

LoadUser and RequireUser are separate so a page can render for anonymous
visitors without every route deciding whether to redirect; Task 13 makes
RequireUser the default for app routes.

A failed login is indistinguishable whether or not the account exists: the
wording and status are identical, and an unknown username still spends a real
Argon2id verification so response timing does not distinguish them either.
Logout deletes the session row rather than only clearing the cookie, so a
copied cookie stops working.

Repeated failures are rate limited per username and address, in memory: a
restart clearing the counter is acceptable at this scale and avoids a database
write on every guess. The ?next= parameter accepts only rooted local paths, so
it cannot become an open redirect. HTMX requests get HX-Redirect rather than a
303, which it cannot act on."
```

---
# Task 13: The app framework — interface, default-deny router, registry

**Files:**
- Create: `internal/platform/app/app.go`
- Create: `internal/platform/app/router.go`
- Create: `internal/ui/templates/home.html`
- Modify: `cmd/onsuite/stack.go`, `cmd/onsuite/serve.go`
- Test: `internal/platform/app/app_test.go`, `internal/platform/app/router_test.go`

**Interfaces:**
- Consumes: everything from Tasks 8–12, plus `db.Migration`/`db.Collect` from Plan 1.
- Produces:
  - `app.Meta{ID, Name, Summary string; Order int}`
  - `app.App` interface: `Meta() Meta`, `Migrations() fs.FS`, `Mount(*Router, Deps)`
  - `app.Deps{DB *sql.DB; Render *render.Renderer; Users *auth.Store; Errors *web.Errors; Log *slog.Logger}` plus `(Deps).Page(r *http.Request, title string) render.Page`
  - `app.NewRegistry(apps ...App) (*Registry, error)`
  - `(*Registry).Apps() []App`, `.NavItems() []render.NavItem`, `.Migrations() ([]db.Migration, error)`, `.Mount(mux *http.ServeMux, deps Deps, guard web.Middleware) error`
  - `app.NewPage(r *http.Request, title string, nav []render.NavItem) render.Page`
  - `(*Router).Handle/HandleFunc` (authenticated) and `.Public/.PublicFunc` (anonymous), `.Routes() []Route`

**The design decision this task exists for.** Spec §7.4 describes the auth middleware skipping a list of app-declared public routes. A skip-list fails **open**: forget to declare a route and it is silently public. `Router` inverts that — `Handle` requires authentication, and making something anonymous means calling `Public`, which is visible in review and greppable. Forgetting therefore fails closed. This is a strengthening of §7.4 and is recorded in the roadmap.

**Route patterns** are written relative to the app, and the router prefixes them: an app with id `paste` calling `Handle("GET /{slug}", h)` registers `GET /paste/{slug}`. `Handle("GET /{$}", h)` registers the app's index, and `ServeMux` redirects `/paste` to `/paste/` for free.

> **Warning — `ServeMux` panics on ambiguous patterns, at startup.** Two
> patterns of the same segment count where one starts with a wildcard and the
> other with a literal will collide. `GET /{slug}/raw` and `GET /s/{slug}` both
> match `/paste/s/raw` and neither is more specific, so registering both takes
> the server down on boot. This was hit while verifying this plan.
>
> Plan 3 will feel this when designing ON Paste's routes. The way out is to keep
> wildcards at a distinct depth: `GET /{$}`, `GET /new`, `GET /{slug}` and
> `GET /s/{slug}` coexist happily, because a literal beats a wildcard at the
> same depth and the two-segment pattern cannot overlap the one-segment ones.

- [ ] **Step 1: Write the app interface and registry**

Create `internal/platform/app/app.go`:

```go
// Package app defines what an ON Suite application is and how one is mounted.
//
// An app is a package under internal/apps that implements App. It never
// imports another app, and the platform never imports it: the only coupling is
// this interface plus the explicit registration slice in cmd/onsuite.
package app

import (
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"regexp"
	"sort"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// Meta describes an app to the shell.
type Meta struct {
	// ID is a lowercase single word. It is the URL prefix, the migration
	// namespace, and the prefix on every table the app owns.
	ID string
	// Name is the display name, always "ON " followed by one word.
	Name string
	// Summary is one short line for the dashboard.
	Summary string
	// Order positions the app in the switcher. Ties break alphabetically.
	Order int
}

var (
	idPattern   = regexp.MustCompile(`^[a-z][a-z0-9]{1,15}$`)
	namePattern = regexp.MustCompile(`^ON [A-Z][A-Za-z]+$`)
)

// Validate keeps the naming convention from drifting as apps are added. The
// convention is load-bearing: ID is used as a URL prefix, a migration
// namespace and a table prefix all at once.
func (m Meta) Validate() error {
	if !idPattern.MatchString(m.ID) {
		return fmt.Errorf("app: id %q must be 2-16 lowercase letters or digits, starting with a letter", m.ID)
	}
	if !namePattern.MatchString(m.Name) {
		return fmt.Errorf("app: name %q must look like \"ON Paste\"", m.Name)
	}
	if m.Summary == "" {
		return fmt.Errorf("app: %s has no Summary", m.ID)
	}
	return nil
}

// Path is where the app is mounted.
func (m Meta) Path() string { return "/" + m.ID + "/" }

// App is what every ON Suite application implements. Three methods: who am I,
// what is my schema, and here are my routes.
type App interface {
	Meta() Meta
	// Migrations holds this app's .sql files at the root of the returned FS.
	Migrations() fs.FS
	// Mount registers routes. Everything registered through Router.Handle
	// requires authentication; anonymous access is opt-in via Router.Public.
	Mount(r *Router, deps Deps)
}

// Deps is everything an app is given. Anything not here is either the
// platform's business or the app's own private concern.
type Deps struct {
	DB     *sql.DB
	Render *render.Renderer
	Users  *auth.Store
	Errors *web.Errors
	Log    *slog.Logger

	// nav is the switcher contents, set by the registry so Page can fill in
	// the shell without every app knowing about every other app.
	nav []render.NavItem
}

// Page returns a render.Page with the shell already filled in from the
// request: who is signed in, the CSRF token, the active app and the nav.
//
// Apps set only Data on the result. This is the reason a handler never has to
// think about the chrome.
func (d Deps) Page(r *http.Request, title string) render.Page {
	return NewPage(r, title, d.nav)
}

// NewPage builds a page shell from a request. Exported so the platform's own
// pages, which are not apps, can use the same code.
func NewPage(r *http.Request, title string, nav []render.NavItem) render.Page {
	shell := render.Shell{
		Apps:      nav,
		ActiveApp: web.ActiveApp(r.Context()),
		CSRFToken: web.CSRFToken(r.Context()),
	}
	if u, ok := web.UserFrom(r.Context()); ok {
		shell.LoggedIn = true
		shell.Username = u.Username
		shell.IsAdmin = u.IsAdmin
	}
	return render.Page{Title: title, Shell: shell}
}

// Registry is the set of apps compiled into this binary.
type Registry struct {
	apps []App
}

// NewRegistry validates the apps and orders them for display. Called once,
// from main, with an explicit slice: the contents of the binary are always
// readable in one place.
func NewRegistry(apps ...App) (*Registry, error) {
	seen := make(map[string]bool, len(apps))
	for _, a := range apps {
		m := a.Meta()
		if err := m.Validate(); err != nil {
			return nil, err
		}
		if seen[m.ID] {
			return nil, fmt.Errorf("app: %q is registered twice", m.ID)
		}
		seen[m.ID] = true
	}

	ordered := make([]App, len(apps))
	copy(ordered, apps)
	sort.SliceStable(ordered, func(i, j int) bool {
		mi, mj := ordered[i].Meta(), ordered[j].Meta()
		if mi.Order != mj.Order {
			return mi.Order < mj.Order
		}
		return mi.ID < mj.ID
	})
	return &Registry{apps: ordered}, nil
}

// Apps returns the registered apps in display order.
func (reg *Registry) Apps() []App { return reg.apps }

// NavItems is the app switcher contents.
func (reg *Registry) NavItems() []render.NavItem {
	out := make([]render.NavItem, 0, len(reg.apps))
	for _, a := range reg.apps {
		m := a.Meta()
		out = append(out, render.NavItem{ID: m.ID, Name: m.Name, Path: m.Path()})
	}
	return out
}

// Migrations collects every app's schema, namespaced by app id. An app that is
// not registered contributes nothing, so its tables are never created.
func (reg *Registry) Migrations() ([]db.Migration, error) {
	var out []db.Migration
	for _, a := range reg.apps {
		id := a.Meta().ID
		ms, err := db.Collect(id, a.Migrations())
		if err != nil {
			return nil, err
		}
		out = append(out, ms...)
	}
	return out, nil
}

// Mount registers every app's routes and templates.
//
// guard is applied to everything except routes an app explicitly declares
// public.
func (reg *Registry) Mount(mux *http.ServeMux, deps Deps, guard web.Middleware) error {
	deps.nav = reg.NavItems()

	for _, a := range reg.apps {
		m := a.Meta()

		if err := deps.Render.AddApp(m.ID, appTemplates(a)); err != nil {
			return err
		}
		r := newRouter(mux, m.ID, guard)
		a.Mount(r, deps)
		if len(r.Routes()) == 0 {
			return fmt.Errorf("app: %q mounted no routes", m.ID)
		}
	}
	return nil
}

// appTemplates lets an app expose templates without adding a fourth method to
// the interface: if it implements Templates() fs.FS, that is used.
type templated interface {
	Templates() fs.FS
}

func appTemplates(a App) fs.FS {
	if t, ok := a.(templated); ok {
		return t.Templates()
	}
	return nil
}
```

- [ ] **Step 2: Write the default-deny router**

Create `internal/platform/app/router.go`:

```go
package app

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// Route records one registration, for tests and for introspection.
type Route struct {
	// Pattern is the full pattern as registered, e.g. "GET /paste/{slug}".
	Pattern string
	// Public is true only when the app called Public explicitly.
	Public bool
}

// Router registers an app's routes under its own prefix.
//
// Handle requires authentication. Public does not. That asymmetry is the
// point: forgetting to protect a route is impossible, because protection is
// what happens when you do nothing special.
type Router struct {
	mux    *http.ServeMux
	appID  string
	prefix string
	guard  web.Middleware
	routes []Route
}

func newRouter(mux *http.ServeMux, appID string, guard web.Middleware) *Router {
	return &Router{
		mux:    mux,
		appID:  appID,
		prefix: "/" + appID,
		guard:  guard,
	}
}

// Handle registers an authenticated route. The pattern is relative to the app,
// so "GET /{slug}" becomes "GET /paste/{slug}".
func (r *Router) Handle(pattern string, h http.Handler) {
	r.register(pattern, h, false)
}

func (r *Router) HandleFunc(pattern string, h http.HandlerFunc) {
	r.register(pattern, h, false)
}

// Public registers a route reachable without signing in.
//
// Use it only for content that is meant to be shared, such as a paste behind
// an unguessable slug. Every call is a deliberate decision to expose
// something, and is greppable for exactly that reason.
func (r *Router) Public(pattern string, h http.Handler) {
	r.register(pattern, h, true)
}

func (r *Router) PublicFunc(pattern string, h http.HandlerFunc) {
	r.register(pattern, h, true)
}

// Routes lists what was registered, in registration order.
func (r *Router) Routes() []Route {
	out := make([]Route, len(r.routes))
	copy(out, r.routes)
	return out
}

func (r *Router) register(pattern string, h http.Handler, public bool) {
	full, err := joinPattern(r.prefix, pattern)
	if err != nil {
		// A malformed pattern is a programming error in an app, discovered at
		// startup. ServeMux panics on bad patterns too, so this is consistent.
		panic(fmt.Sprintf("app %s: %v", r.appID, err))
	}

	// Every app route records which app is active, so the shell can mark the
	// nav without each handler remembering to.
	handler := r.withActiveApp(h)
	if !public {
		handler = r.guard(handler)
	}

	r.mux.Handle(full, handler)
	r.routes = append(r.routes, Route{Pattern: full, Public: public})
}

func (r *Router) withActiveApp(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		next.ServeHTTP(w, req.WithContext(web.WithActiveApp(req.Context(), r.appID)))
	})
}

// joinPattern inserts the app prefix into a ServeMux pattern, preserving an
// optional leading method.
func joinPattern(prefix, pattern string) (string, error) {
	method, path := "", pattern
	if i := strings.Index(pattern, " "); i >= 0 {
		method, path = pattern[:i], pattern[i+1:]
	}
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("pattern %q must have a path starting with /", pattern)
	}
	if strings.Contains(path, "//") {
		return "", fmt.Errorf("pattern %q contains an empty path segment", pattern)
	}
	joined := prefix + path
	if method == "" {
		return joined, nil
	}
	return method + " " + joined, nil
}
```

- [ ] **Step 3: Write the dashboard template**

Create `internal/ui/templates/home.html`:

```html
{{define "content"}}
<div class="measure stack">
	<h1>ON Suite</h1>

	{{if .Data.Apps}}
	<ul class="app-list">
		{{range .Data.Apps}}
		<li>
			<a href="{{.Path}}">{{.Name}}</a>
			<span class="faint">{{.Summary}}</span>
		</li>
		{{end}}
	</ul>
	{{else}}
	<p class="dim">No applications are registered in this build yet.</p>
	{{end}}

	<p class="faint">Version {{.Shell.Version}}</p>
</div>
{{end}}
```

Append to `internal/ui/static/app.css`:

```css
/* ---- Dashboard --------------------------------------------------------- */

.app-list { margin: 0; padding: 0; list-style: none; }

.app-list li {
	display: flex;
	align-items: baseline;
	gap: var(--s-3);
	padding: var(--s-2) 0;
	border-bottom: var(--border);
}

.app-list li:last-child { border-bottom: none; }
.app-list a { font-weight: 600; white-space: nowrap; }
```

- [ ] **Step 4: Mount the registry in the stack**

Rewrite `cmd/onsuite/stack.go`'s `buildStack` so the registry drives routing and the dashboard. The full file:

```go
package main

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

type stackDeps struct {
	DB       *sql.DB
	Users    *auth.Store
	Registry *app.Registry
	Log      *slog.Logger
	Version  string
	Secure   bool
}

func buildStack(deps stackDeps) (http.Handler, error) {
	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		return nil, err
	}
	rend, err := render.NewRenderer(render.Options{
		Layouts:  ui.Templates(),
		AssetURL: assets.URL,
	})
	if err != nil {
		return nil, err
	}

	errs := web.NewErrors(rend, deps.Log)
	csrf := web.NewCSRF(deps.Secure, errs)
	authn := web.NewAuth(web.AuthOptions{
		Users:  deps.Users,
		Render: rend,
		Errors: errs,
		CSRF:   csrf,
		Log:    deps.Log,
		Secure: deps.Secure,
	})

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", healthzHandler(deps.Version, deps.DB))
	mux.Handle("GET /static/", http.StripPrefix("/static", assets.Handler()))
	authn.Routes(mux)

	if err := deps.Registry.Mount(mux, app.Deps{
		DB:     deps.DB,
		Render: rend,
		Users:  deps.Users,
		Errors: errs,
		Log:    deps.Log,
	}, authn.RequireUser); err != nil {
		return nil, err
	}

	mux.Handle("GET /{$}", authn.RequireUser(homeHandler(deps, rend, errs)))
	mux.Handle("/", http.HandlerFunc(errs.NotFound))

	return web.Chain(mux,
		web.Recover(deps.Log, errs),
		web.RequestLog(deps.Log),
		web.SecurityHeaders(),
		web.LimitBody(web.DefaultMaxBodyBytes),
		csrf.Middleware,
		authn.LoadUser,
	), nil
}

// homeHandler is the dashboard. It lists whatever apps are in this build, so
// adding an app requires no change here.
func homeHandler(deps stackDeps, rend *render.Renderer, errs *web.Errors) http.Handler {
	type entry struct {
		Name    string
		Path    string
		Summary string
	}

	nav := deps.Registry.NavItems()
	entries := make([]entry, 0, len(nav))
	for _, a := range deps.Registry.Apps() {
		m := a.Meta()
		entries = append(entries, entry{Name: m.Name, Path: m.Path(), Summary: m.Summary})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := app.NewPage(r, "", nav)
		page.Shell.Version = deps.Version
		page.Data = map[string]any{"Apps": entries}

		if err := rend.Page(w, http.StatusOK, "home", page); err != nil {
			errs.Internal(w, r, err)
		}
	})
}
```

- [ ] **Step 5: Register apps and apply their migrations in serve**

In `cmd/onsuite/serve.go`, build the registry and include app migrations. Replace the migration block with:

```go
	registry, err := app.NewRegistry(registeredApps()...)
	if err != nil {
		return err
	}

	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		return err
	}
	appMigrations, err := registry.Migrations()
	if err != nil {
		return err
	}
	migrations = append(migrations, appMigrations...)

	applied, err := db.Apply(context.Background(), handle, migrations)
	if err != nil {
		return err
	}
	if applied > 0 {
		log.Info("migrations applied", "count", applied)
	}
```

and pass the registry to the stack:

```go
	handler, err := buildStack(stackDeps{
		DB:       handle,
		Users:    users,
		Registry: registry,
		Log:      log,
		Version:  version,
		Secure:   cfg.TLSDomain != "",
	})
```

Add `"github.com/iliafrenkel/on-suite/internal/platform/app"` to the imports.

Then create the registration list in `cmd/onsuite/main.go`, at the bottom:

```go
// registeredApps is the definitive list of applications in this binary.
//
// Adding an app is one line here plus its package. Nothing else in the
// platform needs to change, and reading this function tells you exactly what
// this build contains.
func registeredApps() []app.App {
	return []app.App{
		// ON Paste arrives in Plan 3.
	}
}
```

Add `"github.com/iliafrenkel/on-suite/internal/platform/app"` to `main.go`'s imports.

- [ ] **Step 6: Test the registry**

Create `internal/platform/app/app_test.go`:

```go
package app_test

import (
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// fakeApp is a minimal App for exercising the framework.
type fakeApp struct {
	meta       app.Meta
	migrations fs.FS
	templates  fs.FS
	mount      func(*app.Router, app.Deps)
}

func (f fakeApp) Meta() app.Meta    { return f.meta }
func (f fakeApp) Migrations() fs.FS { return f.migrations }
func (f fakeApp) Templates() fs.FS  { return f.templates }
func (f fakeApp) Mount(r *app.Router, d app.Deps) {
	if f.mount != nil {
		f.mount(r, d)
		return
	}
	r.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {})
}

func newFake(id, name string, order int) fakeApp {
	return fakeApp{
		meta: app.Meta{ID: id, Name: name, Summary: "does things", Order: order},
		migrations: fstest.MapFS{
			"0001_init.sql": {Data: []byte("CREATE TABLE " + id + "_things (id INTEGER PRIMARY KEY);")},
		},
		templates: fstest.MapFS{
			"index.html": {Data: []byte(`{{define "content"}}hello{{end}}`)},
		},
	}
}

func TestMetaValidate(t *testing.T) {
	tests := []struct {
		name    string
		meta    app.Meta
		wantErr bool
	}{
		{"good", app.Meta{ID: "paste", Name: "ON Paste", Summary: "s"}, false},
		{"id with uppercase", app.Meta{ID: "Paste", Name: "ON Paste", Summary: "s"}, true},
		{"id with dash", app.Meta{ID: "on-paste", Name: "ON Paste", Summary: "s"}, true},
		{"id too short", app.Meta{ID: "p", Name: "ON Paste", Summary: "s"}, true},
		{"id starts with digit", app.Meta{ID: "1paste", Name: "ON Paste", Summary: "s"}, true},
		{"name missing prefix", app.Meta{ID: "paste", Name: "Paste", Summary: "s"}, true},
		{"name lowercase word", app.Meta{ID: "paste", Name: "ON paste", Summary: "s"}, true},
		{"no summary", app.Meta{ID: "paste", Name: "ON Paste"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.meta.Validate()
			if tt.wantErr && err == nil {
				t.Error("Validate accepted invalid meta")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Validate rejected valid meta: %v", err)
			}
		})
	}
}

func TestMetaPath(t *testing.T) {
	if got := (app.Meta{ID: "paste"}).Path(); got != "/paste/" {
		t.Errorf("Path() = %q, want /paste/", got)
	}
}

func TestRegistryOrdersByOrderThenID(t *testing.T) {
	reg, err := app.NewRegistry(
		newFake("reader", "ON Reader", 20),
		newFake("flash", "ON Flash", 10),
		newFake("notes", "ON Notes", 10),
	)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	var ids []string
	for _, item := range reg.NavItems() {
		ids = append(ids, item.ID)
	}
	if got, want := strings.Join(ids, ","), "flash,notes,reader"; got != want {
		t.Errorf("order = %q, want %q", got, want)
	}
}

func TestRegistryNavItems(t *testing.T) {
	reg, err := app.NewRegistry(newFake("paste", "ON Paste", 0))
	if err != nil {
		t.Fatal(err)
	}
	items := reg.NavItems()
	if len(items) != 1 {
		t.Fatalf("got %d items", len(items))
	}
	if items[0].Name != "ON Paste" || items[0].Path != "/paste/" {
		t.Errorf("item = %+v", items[0])
	}
}

func TestRegistryRejectsDuplicateAndInvalid(t *testing.T) {
	if _, err := app.NewRegistry(newFake("paste", "ON Paste", 0), newFake("paste", "ON Paste", 1)); err == nil {
		t.Error("NewRegistry accepted a duplicate id")
	}
	if _, err := app.NewRegistry(newFake("Paste", "ON Paste", 0)); err == nil {
		t.Error("NewRegistry accepted an invalid id")
	}
}

// TestRegistryMigrationsAreNamespaced proves two apps can each own a 0001.
func TestRegistryMigrationsAreNamespaced(t *testing.T) {
	reg, err := app.NewRegistry(newFake("paste", "ON Paste", 0), newFake("notes", "ON Notes", 1))
	if err != nil {
		t.Fatal(err)
	}
	ms, err := reg.Migrations()
	if err != nil {
		t.Fatalf("Migrations: %v", err)
	}
	if len(ms) != 2 {
		t.Fatalf("got %d migrations, want 2", len(ms))
	}

	keys := map[string]bool{}
	for _, m := range ms {
		keys[m.Key()] = true
	}
	for _, want := range []string{"notes:0001", "paste:0001"} {
		if !keys[want] {
			t.Errorf("missing migration %q; got %v", want, keys)
		}
	}
}

func TestRegistryEmptyIsUsable(t *testing.T) {
	reg, err := app.NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry with no apps: %v", err)
	}
	if len(reg.NavItems()) != 0 {
		t.Error("an empty registry produced nav items")
	}
	ms, err := reg.Migrations()
	if err != nil || len(ms) != 0 {
		t.Errorf("Migrations = %v, %v", ms, err)
	}
}
```

- [ ] **Step 7: Test the router — especially that it fails closed**

Create `internal/platform/app/router_test.go`:

```go
package app_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

// denyGuard stands in for RequireUser: it blocks unless a header is present,
// so tests can tell guarded routes from public ones without a database.
func denyGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Signed-In") != "yes" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// mountFake builds a mux with one app mounted through the real registry.
func mountFake(t *testing.T, mount func(*app.Router, app.Deps)) *http.ServeMux {
	t.Helper()

	f := newFake("paste", "ON Paste", 0)
	f.mount = mount
	reg, err := app.NewRegistry(f)
	if err != nil {
		t.Fatal(err)
	}

	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatal(err)
	}
	rend, err := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	if err := reg.Mount(mux, app.Deps{Render: rend}, denyGuard); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	return mux
}

// TestRouterRequiresAuthByDefault is the reason this router exists rather than
// a list of public routes: doing nothing must produce a protected route.
func TestRouterRequiresAuthByDefault(t *testing.T) {
	mux := mountFake(t, func(r *app.Router, d app.Deps) {
		r.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {
			_, _ = w.Write([]byte("index"))
		})
		r.HandleFunc("POST /new", func(w http.ResponseWriter, req *http.Request) {
			_, _ = w.Write([]byte("created"))
		})
	})

	for _, tc := range []struct{ method, path string }{{"GET", "/paste/"}, {"POST", "/paste/new"}} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s anonymous = %d, want 401", tc.method, tc.path, rec.Code)
		}

		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("X-Signed-In", "yes")
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s %s signed in = %d, want 200", tc.method, tc.path, rec.Code)
		}
	}
}

func TestRouterPublicRoutesAreAnonymous(t *testing.T) {
	mux := mountFake(t, func(r *app.Router, d app.Deps) {
		r.PublicFunc("GET /s/{slug}", func(w http.ResponseWriter, req *http.Request) {
			_, _ = w.Write([]byte("slug=" + req.PathValue("slug")))
		})
		r.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {})
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/paste/s/abc123", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("public route anonymous = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "slug=abc123" {
		t.Errorf("body = %q; the path wildcard did not survive prefixing", got)
	}
}

func TestRouterPrefixesPatterns(t *testing.T) {
	var recorded []app.Route
	mountFake(t, func(r *app.Router, d app.Deps) {
		r.HandleFunc("GET /{$}", func(http.ResponseWriter, *http.Request) {})
		r.HandleFunc("GET /new", func(http.ResponseWriter, *http.Request) {})
		r.PublicFunc("GET /s/{slug}", func(http.ResponseWriter, *http.Request) {})
		recorded = r.Routes()
	})

	want := []app.Route{
		{Pattern: "GET /paste/{$}", Public: false},
		{Pattern: "GET /paste/new", Public: false},
		{Pattern: "GET /paste/s/{slug}", Public: true},
	}
	if len(recorded) != len(want) {
		t.Fatalf("recorded %d routes, want %d: %+v", len(recorded), len(want), recorded)
	}
	for i := range want {
		if recorded[i] != want[i] {
			t.Errorf("route %d = %+v, want %+v", i, recorded[i], want[i])
		}
	}
}

// TestRouterSetsTheActiveApp lets the shell mark the nav without every handler
// remembering to.
func TestRouterSetsTheActiveApp(t *testing.T) {
	mux := mountFake(t, func(r *app.Router, d app.Deps) {
		r.PublicFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {
			_, _ = w.Write([]byte("active=" + web.ActiveApp(req.Context())))
		})
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/paste/", nil))
	if got := rec.Body.String(); got != "active=paste" {
		t.Errorf("body = %q, want active=paste", got)
	}
}

func TestRouterRejectsMalformedPatterns(t *testing.T) {
	for _, pattern := range []string{"GET no-leading-slash", "relative", "GET //double"} {
		t.Run(pattern, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("pattern %q was accepted; want a panic at startup", pattern)
				}
			}()
			mountFake(t, func(r *app.Router, d app.Deps) {
				r.HandleFunc(pattern, func(http.ResponseWriter, *http.Request) {})
			})
		})
	}
}

// TestMountRejectsAnAppWithNoRoutes catches an app that forgot to register
// anything, which would otherwise appear in the nav and 404 when clicked.
func TestMountRejectsAnAppWithNoRoutes(t *testing.T) {
	f := newFake("paste", "ON Paste", 0)
	f.mount = func(*app.Router, app.Deps) {}
	reg, err := app.NewRegistry(f)
	if err != nil {
		t.Fatal(err)
	}

	assets, _ := web.NewAssets(ui.Static(), "/static")
	rend, _ := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL})

	err = reg.Mount(http.NewServeMux(), app.Deps{Render: rend}, denyGuard)
	if err == nil || !strings.Contains(err.Error(), "no routes") {
		t.Errorf("Mount error = %v, want a complaint about no routes", err)
	}
}
```


- [ ] **Step 8: Run everything and verify in a browser**

```bash
gofmt -l . && go vet ./... && go test ./... -race
```

```bash
go run ./cmd/onsuite serve --data-dir /tmp/onsuite-dev
```

Sign in. Expected: the dashboard at `/` with the heading "ON Suite", the message "No applications are registered in this build yet.", and the version. The nav is empty because no apps exist yet — that is correct, and Plan 3 fills it by adding one line to `registeredApps`.

- [ ] **Step 9: Commit**

```bash
git add internal cmd
git commit -m "Add the app framework: interface, default-deny router, registry

Router.Handle requires authentication and Router.Public does not, which
inverts the spec's skip-list design. A skip-list fails open when an app forgets
to declare a route; this fails closed, and every anonymous route is a visible,
greppable call to Public.

Patterns are written relative to the app and prefixed on registration, so an
app never hardcodes its own mount point. Each route records the active app in
context, so the shell marks the nav without handlers participating. Migrations
are collected per app id, so apps number their own without coordinating and an
unregistered app never touches the schema.

The registry is built from one explicit slice in main, so reading a single
function tells you what a binary contains."
```

---

# Task 14: Architecture test

**Files:**
- Create: `internal/arch/arch_test.go`

**Interfaces:** none. This task adds no production code; it makes the plan's boundaries mechanically enforced instead of aspirational.

**What it enforces:**

| Rule | Why |
|---|---|
| An app never imports another app | Apps must be independently removable. This is the rule that keeps app #5 cheap. |
| The platform never imports an app | Otherwise the platform grows app-specific knowledge and stops being reusable. |
| `render` never imports `web` | The layering that lets `render` be a pure function of its inputs. |
| `auth` imports no other platform package | Keeps identity testable without a server. |
| `ui` imports nothing from this module | It is a leaf holding embedded bytes. |
| Only `_test.go` files import `htmlassert` | Keeps a test-only helper from leaking into production code. |

- [ ] **Step 1: Write the test**

Create `internal/arch/arch_test.go`:

```go
// Package arch contains no code. It holds one test that enforces the import
// boundaries the design depends on.
//
// These rules are stated in the spec and in both implementation plans. A rule
// that is only written down gets violated during a late-night change; this one
// fails the build.
package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/iliafrenkel/on-suite"

// pkgImports maps a package path, relative to the module root, to the module's
// own packages it imports. Test files are recorded separately, because a test
// is allowed to import things production code may not.
type pkgImports struct {
	prod map[string][]string
	test map[string][]string
}

func scan(t *testing.T) pkgImports {
	t.Helper()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// Fail loudly rather than silently passing on an empty scan.
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("cannot find the module root from %s: %v", root, err)
	}

	out := pkgImports{prod: map[string][]string{}, test: map[string][]string{}}
	fset := token.NewFileSet()

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "docs", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(rel)

		target := out.prod
		if strings.HasSuffix(d.Name(), "_test.go") {
			target = out.test
		}
		// Record the package even when it imports nothing from this module,
		// so a package with no internal dependencies still counts as scanned.
		if _, ok := target[pkg]; !ok {
			target[pkg] = nil
		}
		for _, spec := range f.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(imported, module+"/") {
				continue // stdlib or third party: not our concern here
			}
			target[pkg] = append(target[pkg], strings.TrimPrefix(imported, module+"/"))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	if len(out.prod) == 0 {
		t.Fatal("scanned no packages; the walk is broken, not the code")
	}
	return out
}

// appName returns the app id for a package inside internal/apps, or "".
func appName(pkg string) string {
	const prefix = "internal/apps/"
	if !strings.HasPrefix(pkg, prefix) {
		return ""
	}
	return strings.SplitN(strings.TrimPrefix(pkg, prefix), "/", 2)[0]
}

// TestAppsDoNotImportEachOther is the rule that keeps each new app cheap: an
// app must be removable by deleting its package and one line in main.
func TestAppsDoNotImportEachOther(t *testing.T) {
	imports := scan(t)
	for pkg, deps := range imports.prod {
		self := appName(pkg)
		if self == "" {
			continue
		}
		for _, dep := range deps {
			other := appName(dep)
			if other != "" && other != self {
				t.Errorf("app %q imports app %q (%s -> %s)", self, other, pkg, dep)
			}
		}
	}
}

// TestPlatformDoesNotImportApps keeps the platform free of app-specific
// knowledge.
func TestPlatformDoesNotImportApps(t *testing.T) {
	imports := scan(t)
	for pkg, deps := range imports.prod {
		if !strings.HasPrefix(pkg, "internal/platform/") && pkg != "internal/ui" {
			continue
		}
		for _, dep := range deps {
			if strings.HasPrefix(dep, "internal/apps/") {
				t.Errorf("platform package %q imports %q", pkg, dep)
			}
		}
	}
}

// TestLayering encodes the dependency order the packages were designed with.
// render must not reach for web, or the two become mutually dependent and
// render stops being testable without a request.
func TestLayering(t *testing.T) {
	forbidden := map[string][]string{
		"internal/platform/render": {"internal/platform/web", "internal/platform/app", "internal/platform/auth"},
		"internal/platform/auth":   {"internal/platform/web", "internal/platform/app", "internal/platform/render"},
		"internal/platform/db":     {"internal/platform/web", "internal/platform/app", "internal/platform/render", "internal/platform/auth"},
		"internal/platform/config": {"internal/platform/web", "internal/platform/app", "internal/platform/render", "internal/platform/auth", "internal/platform/db"},
		"internal/platform/web":    {"internal/platform/app"},
	}

	imports := scan(t)
	for pkg, banned := range forbidden {
		for _, dep := range imports.prod[pkg] {
			for _, b := range banned {
				if dep == b || strings.HasPrefix(dep, b+"/") {
					t.Errorf("%q must not import %q", pkg, dep)
				}
			}
		}
	}
}

// TestUIIsALeaf: it holds embedded bytes, nothing more.
func TestUIIsALeaf(t *testing.T) {
	imports := scan(t)
	if deps := imports.prod["internal/ui"]; len(deps) != 0 {
		t.Errorf("internal/ui imports %v; it must be a leaf", deps)
	}
}

// TestHTMLAssertIsTestOnly. The helper lives in a normal package so several
// test packages can share it, which means only this check stops it being used
// in production code.
func TestHTMLAssertIsTestOnly(t *testing.T) {
	imports := scan(t)
	for pkg, deps := range imports.prod {
		if pkg == "internal/htmlassert" {
			continue
		}
		for _, dep := range deps {
			if dep == "internal/htmlassert" {
				t.Errorf("non-test code in %q imports internal/htmlassert", pkg)
			}
		}
	}
}

// TestScanSeesTheRealTree guards the guard: if the walk silently stopped
// finding files, every test above would pass while checking nothing.
func TestScanSeesTheRealTree(t *testing.T) {
	imports := scan(t)
	for _, want := range []string{
		"cmd/onsuite",
		"internal/platform/web",
		"internal/platform/app",
		"internal/platform/render",
		"internal/platform/auth",
	} {
		if _, ok := imports.prod[want]; !ok {
			t.Errorf("package %q was not scanned; known packages: %d", want, len(imports.prod))
		}
	}
	// A known-true edge: app must import web, since it uses the guard type.
	found := false
	for _, dep := range imports.prod["internal/platform/app"] {
		if dep == "internal/platform/web" {
			found = true
		}
	}
	if !found {
		t.Error("internal/platform/app does not import web; the scan is probably wrong")
	}
}
```

- [ ] **Step 2: Run it**

```bash
go test ./internal/arch/ -v
```

Expected: all PASS.

- [ ] **Step 3: Prove the test actually catches a violation**

A guard that has never failed is not known to work. Break a rule on purpose:

```bash
printf '\npackage render\n\nimport _ "github.com/iliafrenkel/on-suite/internal/platform/web"\n' > internal/platform/render/violation.go
go test ./internal/arch/ -run TestLayering 2>&1 | tail -5
```

Expected: FAIL, naming `internal/platform/render` importing `internal/platform/web`. Now remove it:

```bash
rm internal/platform/render/violation.go
go test ./internal/arch/ -run TestLayering
```

Expected: PASS. Do not skip this step — it is the only evidence the check works.

- [ ] **Step 4: Commit**

```bash
git add internal/arch
git commit -m "Enforce import boundaries with a test

The rules that keep each new app cheap — apps never import each other, the
platform never imports an app, render never imports web — were written in the
spec and in both plans, and nothing checked them. A rule that is only written
down gets violated during a late-night change.

The test also guards itself: it fails if the tree walk stops finding packages,
so it cannot silently pass while checking nothing."
```

---

# Definition of done for Plan 2

1. `gofmt -l .` is empty, and `go build ./... && go vet ./... && go test ./... -race` is green.
2. `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/onsuite` succeeds.
3. `go list -m all` shows exactly four direct dependencies: `modernc.org/sqlite`, `golang.org/x/crypto`, `golang.org/x/term`, `golang.org/x/net`.
4. Visiting `/` while signed out redirects to `/login?next=%2F`.
5. Signing in with the Plan 1 account lands on the dashboard, showing the username and a working Log out button.
6. A wrong password and an unknown username produce **identical** wording and status.
7. `/no-such-page` returns a styled 404 with the shell, not Go's bare text.
8. `curl -sI localhost:8080/` shows `Content-Security-Policy`, `X-Content-Type-Options` and `Referrer-Policy`.
9. The page contains no inline `<script>` and no `style=` attribute.
10. `go test ./internal/arch/` passes, and has been demonstrated to fail when a rule is deliberately broken.

Spec success criterion satisfied: **§11.3** (log in, land on the shell, app switcher renders from the registry).

Carried into Plan 3: §11.4 to §11.8.
