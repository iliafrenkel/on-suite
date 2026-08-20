# ON Suite Visual Redesign — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the shared shell (header, sidebar, footer, breadcrumbs) and the home page a designed look — muted slate/teal palette, a real light/dark toggle, a runtime font switcher, a tile-grid logo, and a card-grid home page — with zero changes to any app's own code.

**Architecture:** Every change lives in `internal/platform/{web,app,render}` (small, additive) and `internal/ui` (templates, CSS, new static assets). `internal/apps/paste` is never touched — it inherits the new look purely through the shared `app.css` tokens, exactly like the platform design already requires. Theme, font and sidebar-collapsed preferences are plain cookies written by a small vanilla-JS file and read once per request when the page shell is built, so the very first response already has the right `data-theme`/`data-font` attributes (no flash of the wrong look).

**Tech Stack:** Go `html/template` (already in use), hand-written CSS custom properties (already in use), one new vanilla-JS file (no framework, no build step, matching the existing `htmx.min.js` vendoring pattern), five self-hosted Google Fonts `.woff2` files (OFL/Apache licensed).

## Global Constraints

- No Node, no npm, no JS build step — the new JS file is hand-written and served exactly like `htmx.min.js` (spec §2). [Design spec: 2026-08-20-suite-visual-redesign-design.md]
- No CDN dependencies at runtime — fonts are vendored `.woff2` files embedded via `go:embed`, not linked from `fonts.googleapis.com`. [spec §2, §5]
- `internal/apps/paste` gets zero diff. Every change happens in `internal/platform/*`, `internal/ui/*`, and `cmd/onsuite/*`. [spec §1, §9, §10]
- Default theme is light; default font pairing is Inter/JetBrains Mono. [spec §4, §5]
- `--font-mono` stays JetBrains Mono across all three font pairings. [spec §5]
- Existing keyboard/focus behaviour must not change — only visual density loosens. [spec §2, §6]
- `go build ./cmd/onsuite` must still produce a single static binary with no new Go dependencies. [spec §11]

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/platform/web/prefs.go` (new) | Reads the three preference cookies (theme, font, sidebar state) off a request, each with a safe default. |
| `internal/platform/render/render.go` (modify) | `Shell` gains `Theme`, `Font`, `SidebarCollapsed`, `ActiveAppName`, `ActiveAppPath`; `NavItem` gains `ComingSoon`; the `icon` template func is registered. |
| `internal/platform/app/app.go` (modify) | `Deps` gains `Version`; `NewPage`/`Deps.Page` populate the new `Shell` fields from the request and nav; `Registry.Mount` accepts extra placeholder `NavItem`s. |
| `internal/ui/icons.go` (new) | `IconFor(id string) template.HTML` — one duotone SVG icon per known app id, plus a generic fallback tile. |
| `internal/ui/templates/base.html` (modify) | New header (logo + breadcrumbs + settings menu + user menu), sidebar, footer. |
| `internal/ui/templates/home.html` (modify) | Card grid replacing the bullet list. |
| `internal/ui/static/app.css` (modify) | New palette tokens, `[data-theme]`/`[data-font]` rules, `@font-face` declarations, shell/sidebar/footer/card/settings CSS, loosened spacing. |
| `internal/ui/static/fonts/*.woff2` (new) | Five vendored font files. |
| `internal/ui/static/theme.js` (new) | Theme/font/sidebar toggle handlers; writes the three preference cookies. |
| `cmd/onsuite/stack.go` (modify) | `comingSoonApps` list; wires `Version` into `app.Deps`; `homeHandler` builds real + coming-soon cards; passes placeholder nav items into `Registry.Mount`. |

---

### Task 1: Preference cookie readers

**Files:**
- Create: `internal/platform/web/prefs.go`
- Test: `internal/platform/web/prefs_test.go`

**Interfaces:**
- Produces: `web.ThemeCookieName`, `web.FontCookieName`, `web.SidebarCookieName` (string consts); `web.ThemeFrom(r *http.Request) string` (`"light"` or `"dark"`, defaults to `"light"`); `web.FontFrom(r *http.Request) string` (`"default"`, `"literata"`, or `"grotesk"`, defaults to `"default"`); `web.SidebarCollapsedFrom(r *http.Request) bool` (defaults to `false`).

These cookies are written entirely client-side by `theme.js` (Task 10) — there is no server-side `Set-Cookie` for them, so no CSRF concern and no new route.

- [ ] **Step 1: Write the failing test**

```go
// internal/platform/web/prefs_test.go
package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func withCookie(name, value string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	if value != "" {
		r.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	return r
}

func TestThemeFrom(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"no cookie defaults to light", "", "light"},
		{"explicit light", "light", "light"},
		{"explicit dark", "dark", "dark"},
		{"garbage value defaults to light", "purple", "light"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ThemeFrom(withCookie(ThemeCookieName, tt.value)); got != tt.want {
				t.Errorf("ThemeFrom = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFontFrom(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"no cookie defaults to default", "", "default"},
		{"explicit default", "default", "default"},
		{"explicit literata", "literata", "literata"},
		{"explicit grotesk", "grotesk", "grotesk"},
		{"garbage value defaults to default", "comic-sans", "default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FontFrom(withCookie(FontCookieName, tt.value)); got != tt.want {
				t.Errorf("FontFrom = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSidebarCollapsedFrom(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"no cookie defaults to expanded", "", false},
		{"explicit collapsed", "collapsed", true},
		{"explicit expanded", "expanded", false},
		{"garbage value defaults to expanded", "sideways", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SidebarCollapsedFrom(withCookie(SidebarCookieName, tt.value)); got != tt.want {
				t.Errorf("SidebarCollapsedFrom = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/web/... -run 'TestThemeFrom|TestFontFrom|TestSidebarCollapsedFrom' -v`
Expected: FAIL — `ThemeFrom`, `FontFrom`, `SidebarCollapsedFrom`, `ThemeCookieName`, `FontCookieName`, `SidebarCookieName` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/platform/web/prefs.go

package web

import "net/http"

// Cookie names for the three display preferences. These are written entirely
// client-side, by theme.js — the server only ever reads them, so setting one
// is never a CSRF-relevant action.
const (
	ThemeCookieName   = "onsuite_theme"
	FontCookieName    = "onsuite_font"
	SidebarCookieName = "onsuite_sidebar"
)

// ThemeFrom reads the theme preference, defaulting to light for a missing or
// unrecognised cookie so a tampered or stale value never breaks the page.
func ThemeFrom(r *http.Request) string {
	if c, err := r.Cookie(ThemeCookieName); err == nil && c.Value == "dark" {
		return "dark"
	}
	return "light"
}

// FontFrom reads the font-pairing preference, defaulting to "default"
// (Inter/JetBrains Mono).
func FontFrom(r *http.Request) string {
	if c, err := r.Cookie(FontCookieName); err == nil {
		switch c.Value {
		case "literata", "grotesk":
			return c.Value
		}
	}
	return "default"
}

// SidebarCollapsedFrom reads whether the sidebar should render collapsed,
// defaulting to expanded.
func SidebarCollapsedFrom(r *http.Request) bool {
	c, err := r.Cookie(SidebarCookieName)
	return err == nil && c.Value == "collapsed"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/platform/web/... -run 'TestThemeFrom|TestFontFrom|TestSidebarCollapsedFrom' -v`
Expected: PASS, all subtests green.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/web/prefs.go internal/platform/web/prefs_test.go
git commit -m "feat(web): read theme, font and sidebar cookies with safe defaults"
```

---

### Task 2: Thread preferences and version through the page shell

**Files:**
- Modify: `internal/platform/render/render.go` (the `Shell` struct, lines 30-38)
- Modify: `internal/platform/app/app.go` (the `Deps` struct at lines 74-84, `Deps.Page` at lines 91-93, `NewPage` at lines 97-109)
- Test: `internal/platform/app/app_test.go`

**Interfaces:**
- Consumes: `web.ThemeFrom`, `web.FontFrom`, `web.SidebarCollapsedFrom` (Task 1).
- Produces: `render.Shell.Theme string`, `render.Shell.Font string`, `render.Shell.SidebarCollapsed bool`, `render.Shell.ActiveAppName string`, `render.Shell.ActiveAppPath string`, `render.Shell.Version string` (already existed, now actually populated for every page, not just home); `app.Deps.Version string`.

- [ ] **Step 1: Write the failing test**

```go
// Add to internal/platform/app/app_test.go

func TestNewPageFillsPreferencesAndActiveAppFromRequest(t *testing.T) {
	r := httptest.NewRequest("GET", "/paste/", nil)
	r.AddCookie(&http.Cookie{Name: web.ThemeCookieName, Value: "dark"})
	r.AddCookie(&http.Cookie{Name: web.FontCookieName, Value: "literata"})
	r.AddCookie(&http.Cookie{Name: web.SidebarCookieName, Value: "collapsed"})
	r = r.WithContext(web.WithActiveApp(r.Context(), "paste"))

	nav := []render.NavItem{
		{ID: "paste", Name: "ON Paste", Path: "/paste/"},
		{ID: "notes", Name: "ON Notes", Path: "/notes/", ComingSoon: true},
	}
	page := app.NewPage(r, "Snippets", nav)

	if page.Shell.Theme != "dark" {
		t.Errorf("Theme = %q, want dark", page.Shell.Theme)
	}
	if page.Shell.Font != "literata" {
		t.Errorf("Font = %q, want literata", page.Shell.Font)
	}
	if !page.Shell.SidebarCollapsed {
		t.Error("SidebarCollapsed = false, want true")
	}
	if page.Shell.ActiveAppName != "ON Paste" {
		t.Errorf("ActiveAppName = %q, want ON Paste", page.Shell.ActiveAppName)
	}
	if page.Shell.ActiveAppPath != "/paste/" {
		t.Errorf("ActiveAppPath = %q, want /paste/", page.Shell.ActiveAppPath)
	}
}

func TestNewPageDefaultsWithNoCookiesAndNoActiveApp(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	page := app.NewPage(r, "", nil)

	if page.Shell.Theme != "light" {
		t.Errorf("Theme = %q, want light", page.Shell.Theme)
	}
	if page.Shell.Font != "default" {
		t.Errorf("Font = %q, want default", page.Shell.Font)
	}
	if page.Shell.SidebarCollapsed {
		t.Error("SidebarCollapsed = true, want false")
	}
	if page.Shell.ActiveAppName != "" {
		t.Errorf("ActiveAppName = %q, want empty", page.Shell.ActiveAppName)
	}
}

func TestDepsPageCarriesVersion(t *testing.T) {
	d := app.Deps{Version: "v1.2.3"}
	page := d.Page(httptest.NewRequest("GET", "/paste/", nil), "Snippets")
	if page.Shell.Version != "v1.2.3" {
		t.Errorf("Version = %q, want v1.2.3", page.Shell.Version)
	}
}
```

Add `"net/http"` and `"net/http/httptest"` to the existing import block in `app_test.go` (it already imports `"net/http"`; add `"net/http/httptest"`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/app/... -run 'TestNewPageFillsPreferences|TestNewPageDefaults|TestDepsPageCarriesVersion' -v`
Expected: FAIL — `page.Shell.Theme` etc. are zero-valued (`""`/`false`), `render.NavItem` has no `ComingSoon` field (compile error), `app.Deps` has no `Version` field (compile error).

- [ ] **Step 3: Write minimal implementation**

In `internal/platform/render/render.go`, replace the `Shell` struct:

```go
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

	// Theme is "light" or "dark". Font is "default", "literata", or
	// "grotesk". Both are read from a cookie once per request, so the first
	// response already carries the right value.
	Theme string
	Font  string
	// SidebarCollapsed is whether the app switcher should render collapsed
	// to icons-only.
	SidebarCollapsed bool

	// ActiveAppName and ActiveAppPath are the display name and path of the
	// app named by ActiveApp, resolved once so templates never have to
	// search Apps themselves. Both are empty outside any app (e.g. on the
	// home page).
	ActiveAppName string
	ActiveAppPath string
}
```

And the `NavItem` struct:

```go
// NavItem is one entry in the app switcher.
type NavItem struct {
	ID   string
	Name string
	Path string
	// ComingSoon marks a placeholder for an app that is specced but not yet
	// built. It renders muted and is never a link.
	ComingSoon bool
}
```

In `internal/platform/app/app.go`, add `Version` to `Deps`:

```go
// Deps is everything an app is given. Anything not here is either the
// platform's business or the app's own private concern.
type Deps struct {
	DB     *sql.DB
	Render *render.Renderer
	Users  *auth.Store
	Errors *web.Errors
	Log    *slog.Logger
	// Version is the running build version, shown in the footer of every
	// page.
	Version string

	// nav is the switcher contents, set by the registry so Page can fill in
	// the shell without every app knowing about every other app.
	nav []render.NavItem
}
```

Replace `Deps.Page` and `NewPage`:

```go
// Page returns a render.Page with the shell already filled in from the
// request: who is signed in, the CSRF token, the active app and the nav.
//
// Apps set only Data on the result. This is the reason a handler never has to
// think about the chrome.
func (d Deps) Page(r *http.Request, title string) render.Page {
	p := NewPage(r, title, d.nav)
	p.Shell.Version = d.Version
	return p
}

// NewPage builds a page shell from a request. Exported so the platform's own
// pages, which are not apps, can use the same code.
func NewPage(r *http.Request, title string, nav []render.NavItem) render.Page {
	shell := render.Shell{
		Apps:             nav,
		ActiveApp:        web.ActiveApp(r.Context()),
		CSRFToken:        web.CSRFToken(r.Context()),
		Theme:            web.ThemeFrom(r),
		Font:             web.FontFrom(r),
		SidebarCollapsed: web.SidebarCollapsedFrom(r),
	}
	for _, n := range nav {
		if n.ID == shell.ActiveApp {
			shell.ActiveAppName = n.Name
			shell.ActiveAppPath = n.Path
			break
		}
	}
	if u, ok := web.UserFrom(r.Context()); ok {
		shell.LoggedIn = true
		shell.Username = u.Username
		shell.IsAdmin = u.IsAdmin
	}
	return render.Page{Title: title, Shell: shell}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/platform/app/... ./internal/platform/render/... -v`
Expected: PASS, including every pre-existing test in both packages (they only read fields that still exist with the same meaning).

- [ ] **Step 5: Commit**

```bash
git add internal/platform/render/render.go internal/platform/app/app.go internal/platform/app/app_test.go
git commit -m "feat: populate theme/font/sidebar preferences and version on every page"
```

---

### Task 3: Duotone app icons

**Files:**
- Create: `internal/ui/icons.go`
- Test: `internal/ui/icons_test.go`
- Modify: `internal/platform/render/render.go` (register the `icon` template func)
- Modify: `internal/platform/render/render_test.go` (cover the new func)

**Interfaces:**
- Produces: `ui.IconFor(id string) template.HTML`, and the `icon` template function usable as `{{icon "paste"}}` in any template parsed by `render.Renderer`.

**Icon set:** `paste` (rounded rect with two short lines, echoing a snippet), `notes` (rounded rect with a bullet-list), `reader` (a signal/RSS glyph — dot plus two quarter-arcs), `flash` (a lightning bolt), and a fallback (the same 2×2 tile pattern as the logo) for any other id. Every icon is a 24×24 viewBox, two-tone using `var(--c-accent)` and `var(--c-accent-bg)` so it re-themes automatically with light/dark.

- [ ] **Step 1: Write the failing test**

```go
// internal/ui/icons_test.go
package ui_test

import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/ui"
)

func TestIconForKnownApps(t *testing.T) {
	for _, id := range []string{"paste", "notes", "reader", "flash"} {
		got := string(ui.IconFor(id))
		if !strings.Contains(got, "<svg") {
			t.Errorf("IconFor(%q) = %q, want it to contain <svg", id, got)
		}
		if !strings.Contains(got, "viewBox") {
			t.Errorf("IconFor(%q) has no viewBox", id)
		}
	}
}

func TestIconForUnknownAppFallsBackToTile(t *testing.T) {
	got := string(ui.IconFor("some-future-app"))
	if !strings.Contains(got, "<svg") {
		t.Errorf("IconFor(unknown) = %q, want it to contain <svg", got)
	}
	// The fallback must not be empty, and must differ from a known icon.
	if got == string(ui.IconFor("paste")) {
		t.Error("fallback icon is identical to the paste icon")
	}
}

func TestIconForIsDistinctPerApp(t *testing.T) {
	seen := map[string]bool{}
	for _, id := range []string{"paste", "notes", "reader", "flash"} {
		svg := string(ui.IconFor(id))
		if seen[svg] {
			t.Errorf("icon for %q duplicates an earlier icon", id)
		}
		seen[svg] = true
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/... -v`
Expected: FAIL — `ui.IconFor` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/ui/icons.go

// Package ui also owns the small set of app icons shown in the sidebar and
// on the home page grid. Icons are looked up by app id rather than carried on
// app.Meta, so a "coming soon" placeholder for an app that does not exist yet
// in code can still get one.
package ui

import "html/template"

var icons = map[string]template.HTML{
	"paste": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="var(--c-accent-bg)"/>
		<path d="M7 9h10M7 13h10M7 17h6" stroke="var(--c-accent)" stroke-width="1.8" stroke-linecap="round" fill="none"/>
	</svg>`,
	"notes": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="var(--c-accent-bg)"/>
		<circle cx="8" cy="9" r="1.4" fill="var(--c-accent)"/>
		<circle cx="8" cy="13" r="1.4" fill="var(--c-accent)"/>
		<circle cx="8" cy="17" r="1.4" fill="var(--c-accent)"/>
		<path d="M11.5 9h7M11.5 13h7M11.5 17h4" stroke="var(--c-accent)" stroke-width="1.8" stroke-linecap="round"/>
	</svg>`,
	"reader": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="var(--c-accent-bg)"/>
		<circle cx="7" cy="17" r="1.6" fill="var(--c-accent)"/>
		<path d="M7 12.5a4.5 4.5 0 0 1 4.5 4.5" stroke="var(--c-accent)" stroke-width="1.8" fill="none" stroke-linecap="round"/>
		<path d="M7 8a9 9 0 0 1 9 9" stroke="var(--c-accent)" stroke-width="1.8" fill="none" stroke-linecap="round"/>
	</svg>`,
	"flash": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="var(--c-accent-bg)"/>
		<path d="M13 6 8 13h4l-1 5 6-8h-4l1-4z" fill="var(--c-accent)"/>
	</svg>`,
}

const fallbackIcon = `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
	<rect x="1" y="1" width="10" height="10" rx="3" fill="var(--c-accent)"/>
	<rect x="13" y="1" width="10" height="10" rx="3" fill="var(--c-accent-bg)"/>
	<rect x="1" y="13" width="10" height="10" rx="3" fill="var(--c-accent-bg)"/>
	<rect x="13" y="13" width="10" height="10" rx="3" fill="var(--c-accent)"/>
</svg>`

// IconFor returns the icon markup for a known app id, or a generic tile icon
// for anything else. id is always a compile-time-known literal (an app's own
// Meta.ID or an entry in cmd/onsuite's coming-soon list), never user input, so
// returning it unescaped as template.HTML is safe.
func IconFor(id string) template.HTML {
	if svg, ok := icons[id]; ok {
		return svg
	}
	return template.HTML(fallbackIcon)
}
```

Now register `icon` as a template function in `internal/platform/render/render.go`. Add the import and update `NewRenderer`:

```go
import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/ui"
)
```

```go
	r := &Renderer{
		pages: make(map[string]*template.Template),
		funcs: template.FuncMap{
			"asset": opts.AssetURL,
			"icon":  ui.IconFor,
		},
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/... -v`
Expected: PASS.

Then add one test to `internal/platform/render/render_test.go` proving the func actually reaches templates, and run the whole package:

```go
// Add to internal/platform/render/render_test.go

func TestIconFuncIsAvailableInTemplates(t *testing.T) {
	r := testRenderer(t)
	app := fstest.MapFS{
		"icon.html": {Data: []byte(`{{define "content"}}{{icon "paste"}}{{end}}`)},
	}
	if err := r.AddApp("iconcheck", app); err != nil {
		t.Fatalf("AddApp: %v", err)
	}
	rec := httptest.NewRecorder()
	if err := r.Page(rec, http.StatusOK, "iconcheck/icon", render.Page{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), "<svg") {
		t.Errorf("body has no <svg>: %q", rec.Body.String())
	}
}
```

Run: `go test ./internal/platform/render/... ./internal/ui/... -v`
Expected: PASS, all tests including every pre-existing one.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/icons.go internal/ui/icons_test.go internal/platform/render/render.go internal/platform/render/render_test.go
git commit -m "feat: add duotone app icons and wire an {{icon}} template func"
```

---

### Task 4: Placeholder nav items for unbuilt apps

**Files:**
- Modify: `internal/platform/app/app.go` (`Registry.Mount`, lines 172-198)
- Test: `internal/platform/app/app_test.go`

**Interfaces:**
- Consumes: `render.NavItem.ComingSoon` (Task 2).
- Produces: `Registry.Mount(mux *http.ServeMux, deps Deps, guard web.Middleware, extra ...render.NavItem) error` — `extra` is appended after the registry's own nav items into every page's `Shell.Apps`. Existing callers that pass no `extra` are unaffected (variadic, backward compatible).

- [ ] **Step 1: Write the failing test**

```go
// Add to internal/platform/app/app_test.go

func TestMountAppendsExtraNavItems(t *testing.T) {
	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatal(err)
	}
	rend, err := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL})
	if err != nil {
		t.Fatal(err)
	}

	var capturedNav []render.NavItem
	f := newFake("paste", "ON Paste", 0)
	f.mount = func(r *app.Router, d app.Deps) {
		r.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {
			capturedNav = d.Page(req, "").Shell.Apps
		})
	}
	reg, err := app.NewRegistry(f)
	if err != nil {
		t.Fatal(err)
	}

	placeholder := render.NavItem{ID: "notes", Name: "ON Notes", ComingSoon: true}
	mux := http.NewServeMux()
	if err := reg.Mount(mux, app.Deps{Render: rend}, func(h http.Handler) http.Handler { return h }, placeholder); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/paste/", nil))

	if len(capturedNav) != 2 {
		t.Fatalf("nav has %d items, want 2; got %+v", len(capturedNav), capturedNav)
	}
	if capturedNav[0].ID != "paste" || capturedNav[0].ComingSoon {
		t.Errorf("first item = %+v, want the real registered app", capturedNav[0])
	}
	if capturedNav[1].ID != "notes" || !capturedNav[1].ComingSoon {
		t.Errorf("second item = %+v, want the coming-soon placeholder", capturedNav[1])
	}
}

// TestMountWithNoExtraNavItemsStillWorks proves the variadic parameter is
// truly optional, so every existing call site keeps compiling and behaving
// the same.
func TestMountWithNoExtraNavItemsStillWorks(t *testing.T) {
	reg, err := app.NewRegistry(newFake("paste", "ON Paste", 0))
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
	err = reg.Mount(http.NewServeMux(), app.Deps{Render: rend}, func(h http.Handler) http.Handler { return h })
	if err != nil {
		t.Fatalf("Mount with no extra items: %v", err)
	}
}
```

Add `"net/http/httptest"` to the import block of `app_test.go` if not already present from Task 2.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/app/... -run TestMount -v`
Expected: FAIL — `Mount` does not accept a fourth argument (compile error).

- [ ] **Step 3: Write minimal implementation**

In `internal/platform/app/app.go`, replace the `Mount` signature and its first line:

```go
// Mount registers every app's routes and templates, then appends extra to
// the nav every page receives. extra is for placeholder entries — apps that
// are specced but not yet built — so the shell can show them as disabled
// rather than pretending they don't exist.
//
// guard is applied to everything except routes an app explicitly declares
// public.
func (reg *Registry) Mount(mux *http.ServeMux, deps Deps, guard web.Middleware, extra ...render.NavItem) error {
	deps.nav = append(reg.NavItems(), extra...)

	for _, a := range reg.apps {
```

(The rest of the function body is unchanged.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/platform/app/... -v`
Expected: PASS, including every pre-existing test in the package.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/app/app.go internal/platform/app/app_test.go
git commit -m "feat(app): let Mount append placeholder nav items for unbuilt apps"
```

---

### Task 5: Wire the home page and sidebar to show real + coming-soon apps

**Files:**
- Modify: `cmd/onsuite/stack.go`
- Create: `cmd/onsuite/stack_test.go`

**Interfaces:**
- Consumes: `render.NavItem.ComingSoon` (Task 2), `Registry.Mount(..., extra ...render.NavItem)` (Task 4), `ui.IconFor` (Task 3, used only inside templates — the `entry` struct just carries `ID`).
- Produces: `homeHandler`'s local `entry` struct gains `ID string` and `ComingSoon bool`; a package-level `comingSoonApps` slice.

- [ ] **Step 1: Write the failing test**

```go
// cmd/onsuite/stack_test.go
package main

import (
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

type fakeHomeApp struct{ id, name string }

func (f fakeHomeApp) Meta() app.Meta {
	return app.Meta{ID: f.id, Name: f.name, Summary: "does things", Order: 0}
}
func (f fakeHomeApp) Migrations() fs.FS { return fstest.MapFS{} }
func (f fakeHomeApp) Mount(r *app.Router, d app.Deps) {
	r.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {})
}

func testHomeHandler(t *testing.T) http.Handler {
	t.Helper()
	reg, err := app.NewRegistry(fakeHomeApp{id: "paste", name: "ON Paste"})
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
	errs := web.NewErrors(rend, slog.New(slog.DiscardHandler))
	deps := stackDeps{Registry: reg, Version: "v9.9.9"}
	return homeHandler(deps, rend, errs)
}

func TestHomePageShowsRealAndComingSoonCards(t *testing.T) {
	rec := httptest.NewRecorder()
	testHomeHandler(t).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())

	real := doc.MustHave("a.app-card")
	if got := htmlassert.Text(doc.MustHave("a.app-card h3")); got != "ON Paste" {
		t.Errorf("real card name = %q", got)
	}
	if _, ok := htmlassert.Attr(real, "href"); !ok {
		t.Error("the real app's card has no href")
	}

	disabled := doc.QueryAll("div.app-card")
	if len(disabled) != len(comingSoonApps) {
		t.Fatalf("got %d coming-soon cards, want %d", len(disabled), len(comingSoonApps))
	}
	if _, ok := htmlassert.Attr(disabled[0], "href"); ok {
		t.Error("a coming-soon card must not be a link")
	}
}

func TestHomePageFootershowsVersion(t *testing.T) {
	rec := httptest.NewRecorder()
	testHomeHandler(t).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	text := htmlassert.Parse(t, rec.Body.String()).Text()
	if !strings.Contains(text, "v9.9.9") {
		t.Errorf("footer text %q does not mention the version", text)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/onsuite/... -run TestHomePage -v`
Expected: FAIL — `comingSoonApps` undefined (compile error), and once that's stubbed, the card counts and `.app-card` classes won't match the current bullet-list markup.

- [ ] **Step 3: Write minimal implementation**

Replace `homeHandler` and add `comingSoonApps` in `cmd/onsuite/stack.go`:

```go
// comingSoonApps lists apps that are specced but not yet built (see
// docs/superpowers/specs/2026-08-18-on-suite-platform-design.md §3), so the
// shell can show them as placeholders instead of pretending only one app
// exists. Each entry becomes a muted, non-clickable card on the home page and
// a muted, non-clickable entry in the sidebar. Remove an app from this list
// the day it is actually registered in registeredApps().
var comingSoonApps = []struct {
	ID      string
	Name    string
	Summary string
}{
	{ID: "notes", Name: "ON Notes", Summary: "A hierarchical outliner for quick notes."},
	{ID: "reader", Name: "ON Reader", Summary: "An RSS reader for the feeds you follow."},
	{ID: "flash", Name: "ON Flash", Summary: "Flash cards for spaced repetition."},
}

// comingSoonNavItems is comingSoonApps in the shape Registry.Mount wants, for
// the sidebar.
func comingSoonNavItems() []render.NavItem {
	out := make([]render.NavItem, len(comingSoonApps))
	for i, a := range comingSoonApps {
		out[i] = render.NavItem{ID: a.ID, Name: a.Name, ComingSoon: true}
	}
	return out
}

// homeHandler is the dashboard. It lists whatever apps are in this build,
// plus the specced-but-unbuilt ones, so adding a real app requires no change
// here beyond removing it from comingSoonApps.
func homeHandler(deps stackDeps, rend *render.Renderer, errs *web.Errors) http.Handler {
	type entry struct {
		ID         string
		Name       string
		Path       string
		Summary    string
		ComingSoon bool
	}

	nav := append(deps.Registry.NavItems(), comingSoonNavItems()...)
	entries := make([]entry, 0, len(deps.Registry.Apps())+len(comingSoonApps))
	for _, a := range deps.Registry.Apps() {
		m := a.Meta()
		entries = append(entries, entry{ID: m.ID, Name: m.Name, Path: m.Path(), Summary: m.Summary})
	}
	for _, a := range comingSoonApps {
		entries = append(entries, entry{ID: a.ID, Name: a.Name, Summary: a.Summary, ComingSoon: true})
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

And pass the same placeholders into `Registry.Mount` in `buildStack`:

```go
	if err := deps.Registry.Mount(mux, app.Deps{
		DB:      deps.DB,
		Render:  rend,
		Users:   deps.Users,
		Errors:  errs,
		Log:     deps.Log,
		Version: deps.Version,
	}, authn.RequireUser, comingSoonNavItems()...); err != nil {
		return nil, err
	}
```

This test won't fully pass until Task 6/7 give `home.html` the `a.app-card`/`div.app-card` markup and the footer text. That's expected — TDD here spans the template change too. Move on to Task 6 and 7 before re-running.

- [ ] **Step 4: Run test to verify it passes** (after Tasks 6 and 7 land)

Run: `go test ./cmd/onsuite/... -v`
Expected: PASS, including every pre-existing test (`serve_test.go`, `backup_test.go`, etc.).

- [ ] **Step 5: Commit**

```bash
git add cmd/onsuite/stack.go cmd/onsuite/stack_test.go
git commit -m "feat: show coming-soon apps alongside real ones in the sidebar and on the home page"
```

---

### Task 6: Rebuild the shell — header, breadcrumbs, sidebar, settings menu, footer

**Files:**
- Modify: `internal/ui/templates/base.html`
- Modify: `internal/platform/render/render_test.go`

**Interfaces:**
- Consumes: `render.Shell.{Theme,Font,SidebarCollapsed,ActiveAppName,ActiveAppPath}` (Task 2), `{{icon .ID}}` (Task 3), `render.NavItem.ComingSoon` (Task 2).

The existing tests `TestPageRendersADocumentWithTheShell`, `TestPageOmitsUserChromeWhenLoggedOut`, `TestCSRFTokenReachesHTMX`, `TestFragmentRendersWithoutTheDocument` must all keep passing — they exercise `.shell-user`, `nav.shell-nav a[aria-current]`, the `hx-headers` attribute, and "no chrome leaks into a fragment". The new markup keeps those exact selectors true; the sidebar's nav is still `<nav class="shell-nav">` full of `<a>` tags, just relocated.

- [ ] **Step 1: Write the failing test**

Add to `internal/platform/render/render_test.go`:

```go
func TestShellHasBreadcrumbsSidebarAndFooter(t *testing.T) {
	r := testRenderer(t)
	rec := httptest.NewRecorder()

	err := r.Page(rec, http.StatusOK, "error", render.Page{
		Title: "New snippet",
		Shell: render.Shell{
			LoggedIn:      true,
			Username:      "ilia",
			Theme:         "dark",
			Font:          "literata",
			ActiveApp:     "paste",
			ActiveAppName: "ON Paste",
			ActiveAppPath: "/paste/",
			Version:       "v1.2.3",
			Apps: []render.NavItem{
				{ID: "paste", Name: "ON Paste", Path: "/paste/"},
				{ID: "notes", Name: "ON Notes", ComingSoon: true},
			},
		},
		Data: map[string]any{"Status": 404, "Title": "Not found", "Message": "no such page"},
	})
	if err != nil {
		t.Fatal(err)
	}
	doc := htmlassert.Parse(t, rec.Body.String())

	// data-theme/data-font land on <html>, set from the very first response.
	if v, _ := htmlassert.Attr(doc.MustHave("html"), "data-theme"); v != "dark" {
		t.Errorf("data-theme = %q, want dark", v)
	}
	if v, _ := htmlassert.Attr(doc.MustHave("html"), "data-font"); v != "literata" {
		t.Errorf("data-font = %q, want literata", v)
	}

	// Breadcrumbs: Home / ON Paste / New snippet.
	crumbs := doc.Text()
	for _, want := range []string{"Home", "ON Paste", "New snippet"} {
		if !strings.Contains(crumbs, want) {
			t.Errorf("breadcrumb text missing %q; got %q", want, crumbs)
		}
	}

	// The sidebar shows the real app as a link and the coming-soon one as
	// something else, not a link.
	links := doc.QueryAll("nav.shell-nav a")
	if len(links) != 1 {
		t.Fatalf("nav has %d links, want 1 (the coming-soon entry must not be a link); got %+v", len(links), links)
	}

	// The footer carries the version, outside the sidebar/header.
	doc.MustHave("footer.app-footer")
	if !strings.Contains(htmlassert.Text(doc.MustHave("footer.app-footer")), "v1.2.3") {
		t.Error("footer does not show the version")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/render/... -run TestShellHasBreadcrumbsSidebarAndFooter -v`
Expected: FAIL — no `data-theme` attribute, no `footer.app-footer`, breadcrumb text absent from the current template.

- [ ] **Step 3: Write minimal implementation**

Replace the entire contents of `internal/ui/templates/base.html`:

```html
{{define "base"}}<!doctype html>
<html lang="en" data-theme="{{.Shell.Theme}}" data-font="{{.Shell.Font}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{with .Title}}{{.}} · {{end}}ON Suite</title>
<link rel="stylesheet" href="{{asset "app.css"}}">
<script src="{{asset "htmx.min.js"}}" defer></script>
<script src="{{asset "theme.js"}}" defer></script>
{{block "head" .}}{{end}}
</head>
<body hx-headers='{"X-CSRF-Token": "{{.Shell.CSRFToken}}"}'>
{{template "shell" .}}
</body>
</html>{{end}}

{{define "shell"}}
<header class="shell-bar">
	<a class="shell-logo" href="/" aria-label="ON Suite home">
		<svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
			<rect x="1" y="1" width="10" height="10" rx="3" fill="var(--c-accent)"/>
			<rect x="13" y="1" width="10" height="10" rx="3" fill="var(--c-border-firm)"/>
			<rect x="1" y="13" width="10" height="10" rx="3" fill="var(--c-border-firm)"/>
			<rect x="13" y="13" width="10" height="10" rx="3" fill="var(--c-accent)"/>
		</svg>
		<span class="shell-mark">ON<span class="dim"> Suite</span></span>
	</a>
	<nav class="shell-crumbs" aria-label="Breadcrumb">
		<a href="/">Home</a>
		{{if .Shell.ActiveAppName}}
		<span aria-hidden="true">/</span>
		{{if .Title}}<a href="{{.Shell.ActiveAppPath}}">{{.Shell.ActiveAppName}}</a>{{else}}<span>{{.Shell.ActiveAppName}}</span>{{end}}
		{{end}}
		{{if .Title}}<span aria-hidden="true">/</span><span>{{.Title}}</span>{{end}}
	</nav>
	{{if .Shell.LoggedIn}}
	<div class="shell-user">
		<span>{{.Shell.Username}}</span>
		<details class="shell-settings">
			<summary class="quiet button" aria-label="Display settings">&#9881;&#65039;</summary>
			<div class="shell-settings-menu">
				<div class="field">
					<span class="faint">Theme</span>
					<div class="row" data-theme-switch>
						<button type="button" class="button quiet" data-theme-value="light">Light</button>
						<button type="button" class="button quiet" data-theme-value="dark">Dark</button>
					</div>
				</div>
				<div class="field">
					<span class="faint">Font</span>
					<div class="row" data-font-switch>
						<button type="button" class="button quiet" data-font-value="default">Default</button>
						<button type="button" class="button quiet" data-font-value="literata">Literata</button>
						<button type="button" class="button quiet" data-font-value="grotesk">Modern</button>
					</div>
				</div>
			</div>
		</details>
		<form method="post" action="/logout">
			<input type="hidden" name="csrf_token" value="{{.Shell.CSRFToken}}">
			<button type="submit" class="quiet">Log out</button>
		</form>
	</div>
	{{end}}
</header>
<div class="app-shell">
	<aside class="app-sidebar" data-sidebar{{if .Shell.SidebarCollapsed}} data-collapsed{{end}}>
		<button type="button" class="sidebar-toggle quiet button" data-sidebar-toggle aria-label="Toggle sidebar">&#8676;</button>
		<nav class="shell-nav" aria-label="Applications">
			{{range .Shell.Apps}}
			{{if .ComingSoon}}
			<span class="sidebar-item sidebar-item-disabled" title="Coming soon">{{icon .ID}}<span class="sidebar-label">{{.Name}}</span></span>
			{{else}}
			<a class="sidebar-item" href="{{.Path}}"{{if eq .ID $.Shell.ActiveApp}} aria-current="page"{{end}}>{{icon .ID}}<span class="sidebar-label">{{.Name}}</span></a>
			{{end}}
			{{end}}
		</nav>
	</aside>
	<main>
	{{block "content" .}}{{end}}
	</main>
</div>
<footer class="app-footer">ON Suite{{with .Shell.Version}} · v{{.}}{{end}}</footer>
{{end}}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/platform/render/... -v`
Expected: PASS, including every pre-existing test — `TestPageRendersADocumentWithTheShell` (nav still has exactly the given real links, `aria-current` still works, `.shell-user span` is still the username since it is the first `<span>` inside `.shell-user`), `TestPageOmitsUserChromeWhenLoggedOut`, `TestCSRFTokenReachesHTMX`, `TestFragmentRendersWithoutTheDocument`, `TestIconFuncIsAvailableInTemplates` (Task 3).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/templates/base.html internal/platform/render/render_test.go
git commit -m "feat(ui): rebuild the shell with a logo, breadcrumbs, sidebar and footer"
```

---

### Task 7: Card-grid home page

**Files:**
- Modify: `internal/ui/templates/home.html`

**Interfaces:**
- Consumes: `.Data.Apps` entries with `ID`, `Name`, `Path`, `Summary`, `ComingSoon` (Task 5), `{{icon .ID}}` (Task 3).

This task makes the `cmd/onsuite/stack_test.go` tests from Task 5 (`TestHomePageShowsRealAndComingSoonCards`, `TestHomePageFootershowsVersion`) pass — they were written and confirmed-failing in Task 5, on purpose, since the fix spans two files.

- [ ] **Step 1: Confirm the currently-failing tests**

Run: `go test ./cmd/onsuite/... -run TestHomePage -v`
Expected: FAIL (same failures observed at the end of Task 5 — no `.app-card` elements yet).

- [ ] **Step 2: Write minimal implementation**

Replace the entire contents of `internal/ui/templates/home.html`:

```html
{{define "content"}}
<div class="stack">
	<h1>ON Suite</h1>

	{{if .Data.Apps}}
	<div class="home-grid">
		{{range .Data.Apps}}
		{{if .ComingSoon}}
		<div class="app-card app-card-disabled" title="Coming soon">
			<div class="app-card-icon">{{icon .ID}}</div>
			<h3>{{.Name}}</h3>
			<p class="faint">{{.Summary}}</p>
		</div>
		{{else}}
		<a class="app-card" href="{{.Path}}">
			<div class="app-card-icon">{{icon .ID}}</div>
			<h3>{{.Name}}</h3>
			<p class="faint">{{.Summary}}</p>
		</a>
		{{end}}
		{{end}}
	</div>
	{{else}}
	<p class="dim">No applications are registered in this build yet.</p>
	{{end}}
</div>
{{end}}
```

(The old `<p class="faint">Version {{.Shell.Version}}</p>` line is gone — the version now lives in the shared footer from Task 6, shown on every page.)

- [ ] **Step 3: Run test to verify it passes**

Run: `go test ./cmd/onsuite/... -v`
Expected: PASS, including every pre-existing test in `cmd/onsuite` (`backup_test.go`, `export_test.go`, `serve_test.go`, `tls_test.go`, `user_test.go`).

- [ ] **Step 4: Run the full test suite to check nothing else broke**

Run: `go test ./... -v 2>&1 | tail -60`
Expected: PASS across every package, no failures introduced in `internal/apps/paste` or elsewhere.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/templates/home.html
git commit -m "feat(ui): replace the bullet-list home page with a card grid"
```

---

### Task 8: Palette, font tokens, and component CSS

**Files:**
- Modify: `internal/ui/static/app.css`

**Interfaces:**
- Consumes: the `[data-theme]`/`[data-font]` attributes set on `<html>` by Task 6, the class names used in `base.html`/`home.html` from Tasks 6-7 (`shell-bar`, `shell-logo`, `shell-mark`, `shell-crumbs`, `shell-user`, `shell-settings`, `shell-settings-menu`, `app-shell`, `app-sidebar`, `sidebar-toggle`, `sidebar-item`, `sidebar-item-disabled`, `sidebar-label`, `app-footer`, `home-grid`, `app-card`, `app-card-disabled`, `app-card-icon`).

CSS has no unit tests in this codebase today (there is no test for the current `app.css`), so this task is verified by running the app and looking at it, in Task 11. Replace the `:root` block and the dark-mode block, and append the new component rules.

- [ ] **Step 1: Replace the `:root` token block**

In `internal/ui/static/app.css`, replace lines 1-45 (from the top comment through the closing `}` of `:root`):

```css
/* ON Suite — quiet, calm, and a little more spacious than it used to be.
 *
 * Everything is driven by the tokens in :root. Apps must not introduce new
 * colours or spacing values; they compose these. That constraint is what
 * makes four separately-built apps look like one suite.
 */

:root {
	/* Greys carry the interface; the accent is a muted slate/teal, used
	 * sparingly and on purpose. */
	--c-bg:          #ffffff;
	--c-bg-subtle:   #f4f3f1;
	--c-bg-inset:    #eceae6;
	--c-border:      #e2e0dc;
	--c-border-firm: #c9c7c1;
	--c-text:        #2b2a28;
	--c-text-dim:    #6b6a67;
	--c-text-faint:  #8a8983;
	--c-accent:      #5c8580;
	--c-accent-bg:   #e4ecea;
	--c-danger:      #a51d2d;
	--c-danger-bg:   #fbeaec;

	/* A 4px scale. Still the scarce-vertical-space discipline of a code
	 * listing, just with more breathing room applied on top in the shell
	 * chrome (see .shell-bar, main, .app-sidebar below). */
	--s-1: 0.25rem;
	--s-2: 0.5rem;
	--s-3: 0.75rem;
	--s-4: 1rem;
	--s-5: 1.5rem;
	--s-6: 2rem;

	/* --font-ui drives headings and UI chrome; --font-body drives prose.
	 * They're equal in the default and literary pairings and differ only in
	 * the "grotesk" one. --font-mono never changes with the pairing. */
	--font-ui: 'Inter', system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
	--font-body: 'Inter', system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
	--font-mono: 'JetBrains Mono', ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;

	--fs-sm: 0.8125rem;
	--fs-base: 0.9375rem;
	--fs-lg: 1.125rem;
	--fs-xl: 1.375rem;

	--radius: 6px;
	--border: 1px solid var(--c-border);
	--ring: 2px solid var(--c-accent);

	--measure: 68ch; /* comfortable reading width */
}

/* Runtime font switcher. data-font is set on <html> from a cookie, read
 * server-side on every request (internal/platform/web/prefs.go) so the first
 * paint already has the right fonts — no flash of the wrong typeface. */
[data-font="literata"] {
	--font-ui: 'Literata', Georgia, "Times New Roman", serif;
	--font-body: 'Literata', Georgia, "Times New Roman", serif;
}

[data-font="grotesk"] {
	--font-ui: 'Space Grotesk', system-ui, sans-serif;
	--font-body: 'Public Sans', system-ui, sans-serif;
}

/* Self-hosted fonts. One file per family; Google's own served CSS declares
 * every weight it supports against the same file, so we mirror that exactly
 * rather than guessing at a variable-font weight range. */
@font-face { font-family: 'Inter'; font-style: normal; font-weight: 400; font-display: swap; src: url("/static/fonts/inter.woff2") format("woff2"); }
@font-face { font-family: 'Inter'; font-style: normal; font-weight: 500; font-display: swap; src: url("/static/fonts/inter.woff2") format("woff2"); }
@font-face { font-family: 'Inter'; font-style: normal; font-weight: 600; font-display: swap; src: url("/static/fonts/inter.woff2") format("woff2"); }
@font-face { font-family: 'JetBrains Mono'; font-style: normal; font-weight: 400; font-display: swap; src: url("/static/fonts/jetbrains-mono.woff2") format("woff2"); }
@font-face { font-family: 'JetBrains Mono'; font-style: normal; font-weight: 500; font-display: swap; src: url("/static/fonts/jetbrains-mono.woff2") format("woff2"); }
@font-face { font-family: 'Literata'; font-style: normal; font-weight: 400; font-display: swap; src: url("/static/fonts/literata.woff2") format("woff2"); }
@font-face { font-family: 'Literata'; font-style: normal; font-weight: 500; font-display: swap; src: url("/static/fonts/literata.woff2") format("woff2"); }
@font-face { font-family: 'Public Sans'; font-style: normal; font-weight: 400; font-display: swap; src: url("/static/fonts/public-sans.woff2") format("woff2"); }
@font-face { font-family: 'Public Sans'; font-style: normal; font-weight: 500; font-display: swap; src: url("/static/fonts/public-sans.woff2") format("woff2"); }
@font-face { font-family: 'Space Grotesk'; font-style: normal; font-weight: 500; font-display: swap; src: url("/static/fonts/space-grotesk.woff2") format("woff2"); }
@font-face { font-family: 'Space Grotesk'; font-style: normal; font-weight: 600; font-display: swap; src: url("/static/fonts/space-grotesk.woff2") format("woff2"); }
```

- [ ] **Step 2: Replace the dark-mode block**

Replace the old `@media (prefers-color-scheme: dark) { :root { ... } }` block (originally lines 47-62) with a real toggle, defaulting to the OS preference only until a cookie exists:

```css
/* Dark mode is a real toggle, not prefers-color-scheme: data-theme is set
 * server-side from a cookie (internal/platform/web/prefs.go, ThemeFrom)
 * which always returns an explicit "light" or "dark", defaulting to light
 * when no cookie exists yet. Because the server never leaves data-theme
 * unset, there is no need for a prefers-color-scheme fallback here — every
 * response already carries the attribute this selector keys off. */
:root[data-theme="dark"] {
	--c-bg:          #1c1d1f;
	--c-bg-subtle:   #242527;
	--c-bg-inset:    #2b2c2f;
	--c-border:      #34373a;
	--c-border-firm: #4a4d51;
	--c-text:        #e8e6e2;
	--c-text-dim:    #a3a29c;
	--c-text-faint:  #79786f;
	--c-accent:      #7fa39d;
	--c-accent-bg:   #233330;
	--c-danger:      #f08c95;
	--c-danger-bg:   #3a1e22;
}
```

- [ ] **Step 3: Update `body` to use `--font-body`, and loosen `main`'s padding**

Find:
```css
body {
	margin: 0;
	background: var(--c-bg);
	color: var(--c-text);
	font: var(--fs-base)/1.45 var(--font-ui);
}
```
Replace with:
```css
body {
	margin: 0;
	background: var(--c-bg);
	color: var(--c-text);
	font: var(--fs-base)/1.6 var(--font-body);
}
```

Find:
```css
main { padding: var(--s-5) var(--s-4); }
.measure { max-width: var(--measure); }
```
Replace with:
```css
main { padding: var(--s-6) var(--s-5); max-width: var(--measure); margin: 0 auto; width: 100%; }
.measure { max-width: var(--measure); }
```

(`.stack` in `home.html` no longer needs the separate `.measure` class since `main` itself is now width-limited; `.measure` stays defined for any page — e.g. inside ON Paste — that still applies it directly.)

- [ ] **Step 4: Replace `.shell-bar`/`.shell-nav`/`.shell-user` rules with the full new shell CSS**

Replace the `/* ---- Shell ------------------------------------------------------------- */` section from its header comment through the end of the `.shell-user { ... }` rule (originally lines 110-147 — **stop before** the `main`/`.measure` rules just after it, which Step 3 already rewrote and which this step must leave alone), and separately replace the later `/* ---- Shell chrome additions ------------------------------------------- */` section (originally lines 222-236), with:

```css
/* ---- Shell ------------------------------------------------------------- */

.shell-bar {
	display: flex;
	align-items: center;
	gap: var(--s-5);
	padding: var(--s-3) var(--s-5);
	background: var(--c-bg);
	border-bottom: var(--border);
}

.shell-logo {
	display: flex;
	align-items: center;
	gap: var(--s-2);
	color: var(--c-text);
	text-decoration: none;
	white-space: nowrap;
}

.shell-mark { font-weight: 600; letter-spacing: 0.01em; }

.shell-crumbs {
	display: flex;
	align-items: center;
	gap: var(--s-2);
	flex: 1;
	min-width: 0;
	font-size: var(--fs-sm);
	color: var(--c-text-dim);
	overflow-x: auto;
	white-space: nowrap;
}

.shell-crumbs a { color: var(--c-text-dim); text-decoration: none; }
.shell-crumbs a:hover { color: var(--c-accent); }
.shell-crumbs > span:last-child { color: var(--c-text); font-weight: 500; }

.shell-user {
	display: flex;
	align-items: center;
	gap: var(--s-3);
	font-size: var(--fs-sm);
	color: var(--c-text-dim);
}

.shell-user form { display: inline; }

.shell-settings { position: relative; }
.shell-settings summary { list-style: none; cursor: pointer; }
.shell-settings summary::-webkit-details-marker { display: none; }

.shell-settings-menu {
	position: absolute;
	right: 0;
	top: calc(100% + var(--s-2));
	z-index: 10;
	min-width: 12rem;
	padding: var(--s-3);
	background: var(--c-bg);
	border: var(--border);
	border-radius: var(--radius);
	box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12);
}

.shell-settings-menu .field { margin-bottom: var(--s-3); }
.shell-settings-menu .field:last-child { margin-bottom: 0; }
.shell-settings-menu .row { flex-wrap: wrap; margin-top: var(--s-1); }
.shell-settings-menu button[data-theme-value].active,
.shell-settings-menu button[data-font-value].active {
	background: var(--c-accent-bg);
	color: var(--c-accent);
	border-color: var(--c-accent);
}

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

/* ---- App shell: sidebar + main ------------------------------------------ */

.app-shell {
	display: flex;
	align-items: flex-start;
	min-height: calc(100vh - 8rem);
}

.app-sidebar {
	flex-shrink: 0;
	width: 12rem;
	padding: var(--s-4) var(--s-2);
	border-right: var(--border);
	transition: width 0.15s ease;
}

.app-sidebar[data-collapsed] { width: 3.5rem; }

.sidebar-toggle {
	width: 100%;
	text-align: left;
	margin-bottom: var(--s-3);
}

.shell-nav { display: flex; flex-direction: column; gap: var(--s-1); }

.sidebar-item {
	display: flex;
	align-items: center;
	gap: var(--s-2);
	padding: var(--s-2);
	border-radius: var(--radius);
	color: var(--c-text-dim);
	text-decoration: none;
	white-space: nowrap;
	overflow: hidden;
}

.sidebar-item:hover { background: var(--c-bg-inset); color: var(--c-text); }

.sidebar-item[aria-current="page"] {
	background: var(--c-accent-bg);
	color: var(--c-accent);
	font-weight: 600;
}

.sidebar-item-disabled { opacity: 0.5; cursor: default; }

.app-sidebar[data-collapsed] .sidebar-label { display: none; }

/* main needs no rule here: it is a flex item inside .app-shell, and the
 * max-width + margin: 0 auto it already got in Step 3 is enough — CSS gives
 * a flex item's auto margins first claim on any free space in the flex
 * line, so main centers itself in whatever room the sidebar leaves without
 * needing flex-grow. */

/* ---- Footer -------------------------------------------------------------- */

.app-footer {
	padding: var(--s-3) var(--s-5);
	border-top: var(--border);
	color: var(--c-text-faint);
	font-size: var(--fs-sm);
	text-align: center;
}
```

- [ ] **Step 5: Add home-page card grid CSS**

Append (near the existing `/* ---- Dashboard --------------------------------------------------------- */` section — replace that whole section, since `.app-list` is no longer used by `home.html`):

```css
/* ---- Home page card grid ------------------------------------------------ */

.home-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(15rem, 1fr));
	gap: var(--s-4);
}

.app-card {
	display: block;
	padding: var(--s-4);
	background: var(--c-bg);
	border: var(--border);
	border-radius: var(--radius);
	color: var(--c-text);
	text-decoration: none;
	transition: border-color 0.15s ease;
}

.app-card:hover { border-color: var(--c-border-firm); }

.app-card h3 { margin: var(--s-3) 0 var(--s-1); }
.app-card p { margin: 0; }

.app-card-icon { width: 2.5rem; height: 2.5rem; }

.app-card-disabled { opacity: 0.55; cursor: default; }
.app-card-disabled:hover { border-color: var(--c-border); }
```

- [ ] **Step 6: Verify the file still parses as CSS**

There's no CSS linter in this repo. Do a quick brace-balance sanity check:

Run: `python3 -c "s=open('internal/ui/static/app.css').read(); print(s.count('{'), s.count('}'))"`
Expected: two equal numbers (e.g. `NN NN`) — mismatched counts mean a missing `{` or `}` was introduced.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/static/app.css
git commit -m "feat(ui): slate/teal palette, real dark-mode toggle, font tokens, shell/sidebar/card CSS"
```

---

### Task 9: Vendor the five self-hosted font files

**Files:**
- Create: `internal/ui/static/fonts/inter.woff2`
- Create: `internal/ui/static/fonts/jetbrains-mono.woff2`
- Create: `internal/ui/static/fonts/literata.woff2`
- Create: `internal/ui/static/fonts/public-sans.woff2`
- Create: `internal/ui/static/fonts/space-grotesk.woff2`
- Modify: `internal/platform/web/assets_test.go`

**Interfaces:**
- Consumes: none new. `ui.Static()` already embeds everything under `internal/ui/static/`, so dropping files into a `fonts/` subdirectory there needs no other code change — `web.NewAssets` walks the whole tree.

These are the exact latin-subset files Google Fonts itself serves for the weights the design spec calls for (Inter 400/500/600, JetBrains Mono 400/500, Literata 400/500, Public Sans 400/500, Space Grotesk 500/600), fetched from `fonts.gstatic.com` on 2026-08-20. Each `@font-face` block added in Task 8 already points at the exact filenames below.

- [ ] **Step 1: Write the failing test**

Add to `internal/platform/web/assets_test.go`:

```go
func TestFontsAreEmbedded(t *testing.T) {
	a, err := NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatalf("NewAssets on the embedded tree: %v", err)
	}
	for _, want := range []string{
		"fonts/inter.woff2",
		"fonts/jetbrains-mono.woff2",
		"fonts/literata.woff2",
		"fonts/public-sans.woff2",
		"fonts/space-grotesk.woff2",
	} {
		if !strings.Contains(strings.Join(a.Names(), ","), want) {
			t.Errorf("%s is not embedded; got %v", want, a.Names())
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/web/... -run TestFontsAreEmbedded -v`
Expected: FAIL — the five files don't exist yet.

- [ ] **Step 3: Download and verify each file**

```bash
mkdir -p internal/ui/static/fonts

curl -sL -o internal/ui/static/fonts/inter.woff2 \
  "https://fonts.gstatic.com/s/inter/v20/UcC73FwrK3iLTeHuS_nVMrMxCp50SjIa1ZL7.woff2"
curl -sL -o internal/ui/static/fonts/jetbrains-mono.woff2 \
  "https://fonts.gstatic.com/s/jetbrainsmono/v24/tDbv2o-flEEny0FZhsfKu5WU4zr3E_BX0PnT8RD8yKwBNntkaToggR7BYRbKPxDcwg.woff2"
curl -sL -o internal/ui/static/fonts/literata.woff2 \
  "https://fonts.gstatic.com/s/literata/v40/or3aQ6P12-iJxAIgLa78DkrbXsDgk0oVDaDPYLanFLHpPf2TbBG_df3-vbgKBM6YoggA-vpO-7c.woff2"
curl -sL -o internal/ui/static/fonts/public-sans.woff2 \
  "https://fonts.gstatic.com/s/publicsans/v21/ijwRs572Xtc6ZYQws9YVwnNGfJ4.woff2"
curl -sL -o internal/ui/static/fonts/space-grotesk.woff2 \
  "https://fonts.gstatic.com/s/spacegrotesk/v22/V8mDoQDjQSkFtoMM3T6r8E7mPbF4Cw.woff2"

shasum -a 256 internal/ui/static/fonts/*.woff2
```

Expected output (exact — these were downloaded and hashed while writing this plan):

```
3100e775e8616cd2611beecfa23a4263d7037586789b43f035236a2e6fbd4c62  internal/ui/static/fonts/inter.woff2
83c005d49d8a6a50474c73a5a36ac0468076e9c4a29da7bdb14995d80560a5be  internal/ui/static/fonts/jetbrains-mono.woff2
fde3d14f78c3431ee5e2f676a46dd18a1dd48ee4d0a71cc76c751f9e6f76f58e  internal/ui/static/fonts/literata.woff2
5ed4d31c988e73b258894244f209069ebe77dc7e564861954b21198b6de90d68  internal/ui/static/fonts/public-sans.woff2
0640890476fc1198ab4de571fb658de443c4d85b66466ec09534a8737ab1ce9d  internal/ui/static/fonts/space-grotesk.woff2
```

If any hash differs, stop and check the URL was fetched correctly (a redirect to an HTML error page will produce a small, non-matching file) rather than proceeding with a corrupt font.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/platform/web/... -v`
Expected: PASS, including every pre-existing test in the package.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/static/fonts/*.woff2 internal/platform/web/assets_test.go
git commit -m "feat(ui): vendor self-hosted Inter, JetBrains Mono, Literata, Public Sans, Space Grotesk"
```

---

### Task 10: Theme/font/sidebar toggle script

**Files:**
- Create: `internal/ui/static/theme.js`

**Interfaces:**
- Consumes: the `data-theme-switch`/`data-font-switch`/`data-sidebar-toggle` hooks added to `base.html` in Task 6, and the cookie names `onsuite_theme`/`onsuite_font`/`onsuite_sidebar` (Task 1 — kept as literal strings here since this is a plain JS file with no access to the Go constants).

`base.html` already references `{{asset "theme.js"}}` (added in Task 6), so this task is what makes that script actually exist and do something. There is no Go test for a client-side script in this codebase; correctness is verified interactively in Task 11.

- [ ] **Step 1: Write the file**

```js
// internal/ui/static/theme.js
//
// Theme, font and sidebar-collapsed state are plain, non-HttpOnly cookies:
// this script is the only thing that ever writes them. The server
// (internal/platform/web/prefs.go) only reads them, so there is no endpoint
// and no CSRF concern here — flipping a preference is a page-level
// enhancement, not a request to the server.

(function () {
	"use strict";

	function setCookie(name, value) {
		var secure = location.protocol === "https:" ? "; Secure" : "";
		document.cookie = name + "=" + value + "; Path=/; Max-Age=31536000; SameSite=Lax" + secure;
	}

	function markActive(buttons, attr, value) {
		for (var i = 0; i < buttons.length; i++) {
			buttons[i].classList.toggle("active", buttons[i].getAttribute(attr) === value);
		}
	}

	function initThemeSwitch() {
		var group = document.querySelector("[data-theme-switch]");
		if (!group) return;
		var buttons = group.querySelectorAll("[data-theme-value]");
		markActive(buttons, "data-theme-value", document.documentElement.getAttribute("data-theme"));
		buttons.forEach(function (btn) {
			btn.addEventListener("click", function () {
				var value = btn.getAttribute("data-theme-value");
				document.documentElement.setAttribute("data-theme", value);
				setCookie("onsuite_theme", value);
				markActive(buttons, "data-theme-value", value);
			});
		});
	}

	function initFontSwitch() {
		var group = document.querySelector("[data-font-switch]");
		if (!group) return;
		var buttons = group.querySelectorAll("[data-font-value]");
		markActive(buttons, "data-font-value", document.documentElement.getAttribute("data-font"));
		buttons.forEach(function (btn) {
			btn.addEventListener("click", function () {
				var value = btn.getAttribute("data-font-value");
				document.documentElement.setAttribute("data-font", value);
				setCookie("onsuite_font", value);
				markActive(buttons, "data-font-value", value);
			});
		});
	}

	function initSidebarToggle() {
		var sidebar = document.querySelector("[data-sidebar]");
		var toggle = document.querySelector("[data-sidebar-toggle]");
		if (!sidebar || !toggle) return;
		toggle.addEventListener("click", function () {
			var collapsed = sidebar.hasAttribute("data-collapsed");
			if (collapsed) {
				sidebar.removeAttribute("data-collapsed");
				setCookie("onsuite_sidebar", "expanded");
			} else {
				sidebar.setAttribute("data-collapsed", "");
				setCookie("onsuite_sidebar", "collapsed");
			}
		});
	}

	initThemeSwitch();
	initFontSwitch();
	initSidebarToggle();
})();
```

- [ ] **Step 2: Confirm it's picked up as a static asset**

Run: `go run ./cmd/onsuite user add --help >/dev/null && go build -o /tmp/onsuite-plan-check ./cmd/onsuite && rm /tmp/onsuite-plan-check`
Expected: builds cleanly (proves `go:embed static` still walks fine with the new file present — a bad file would only fail at `go build` if it were literally unreadable, which a plain-text `.js` file never is; this step is really about confirming the overall tree still compiles after every static asset addition in Tasks 8-10).

- [ ] **Step 3: Commit**

```bash
git add internal/ui/static/theme.js
git commit -m "feat(ui): add the theme/font/sidebar toggle script"
```

---

### Task 11: Full-suite verification and manual browser check

**Files:** none (verification only).

- [ ] **Step 1: Run the entire test suite**

Run: `go vet ./... && go test ./... -race`
Expected: every package passes, including `internal/apps/paste/...` (untouched) and the architecture-boundary test in `internal/arch`.

- [ ] **Step 2: Confirm ON Paste has zero diff**

Run: `git diff --stat main -- internal/apps/paste`
Expected: empty output — no changes under `internal/apps/paste` anywhere in this branch's history, confirming the design's non-goal held.

- [ ] **Step 3: Build and run the server locally**

```bash
go build -o /tmp/onsuite-preview ./cmd/onsuite
mkdir -p /tmp/onsuite-preview-data
/tmp/onsuite-preview user add previewer --data-dir /tmp/onsuite-preview-data <<< "a-long-enough-password-123"
/tmp/onsuite-preview serve --data-dir /tmp/onsuite-preview-data --addr :8080 &
```

- [ ] **Step 4: Manual browser walkthrough**

Using the `webapp-testing` skill (or a browser directly) against `http://localhost:8080`, log in as `previewer` and confirm:

1. Home page: light theme by default, a card for ON Paste (clickable) and three muted "coming soon" cards (not clickable) for ON Notes/Reader/Flash, each with a distinct icon.
2. Header: tile-grid logo, "ON Suite" wordmark, breadcrumb reading "Home" on the home page.
3. Open the settings menu (gear icon): switch to Dark — the whole page re-themes instantly, no reload. Reload the page — it's still dark.
4. In the same menu, switch fonts between Default, Literata, and Modern — headings and body visibly change typeface each time. Reload — the choice persists.
5. Click into ON Paste: sidebar shows it as the active item; breadcrumb reads "Home / ON Paste / Snippets" (or "New snippet" on the new-paste page). Collapse the sidebar via its toggle — it shrinks to icons only. Reload — it's still collapsed.
6. Footer at the bottom of every page shows the version string.
7. Resize the browser narrow and wide — the main content column stays reading-width, doesn't stretch edge-to-edge.

- [ ] **Step 5: Stop the preview server**

```bash
kill %1
rm -rf /tmp/onsuite-preview /tmp/onsuite-preview-data
```

- [ ] **Step 6: Final commit if any manual-check fixes were needed**

If Step 4 surfaced anything (a CSS glitch, a missed selector), fix it directly in the relevant file from Tasks 6-10, re-run that task's tests, and commit:

```bash
git add -A
git commit -m "fix: address issues found in manual browser walkthrough"
```

If nothing needed fixing, there is nothing to commit — the plan is complete.
