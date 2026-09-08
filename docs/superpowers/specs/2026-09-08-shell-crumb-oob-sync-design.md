# Shell breadcrumb + tab title: keep in sync on an in-place htmx navigation

## Problem

Issue [#205](https://github.com/iliafrenkel/on-suite/issues/205): the
shell's own top breadcrumb (`Home / ON Notes / <root title>`,
`internal/ui/templates/base.html:32-38`) is built from `render.Page.Title`
and `render.Page.Shell`, which only a full-page render populates
(`app.Deps.Page`/`app.NewPage`). Any handler that instead answers an htmx
fragment request — swapping content in place without a full page load —
never touches either, so the shell breadcrumb (and `base.html:6`'s
`<title>`, for the apps that don't already fix it themselves) keeps
describing whatever was last full-page-loaded.

ON Notes' zoom feature (docs at
[2026-09-08-notes-zoom-transition-design.md](2026-09-08-notes-zoom-transition-design.md))
already hit this for its own in-page breadcrumb/heading and its own
`<title>`, and fixed both with an out-of-band (OOB) swap and a `<title>`
element in the fragment response, respectively — see that PR's Task 1.5.
The shell breadcrumb is the same bug, one layer up, deliberately left as a
follow-up there because it's shared shell markup, not something scoped to
Notes' own templates.

Investigating this issue found that ON Paste has the identical bug live
today, not just as a future concern: its snippet-to-snippet navigation
(`internal/apps/paste/templates/index.html:144`, and the Back/Edit/Cancel
links at `:39-40`, `:80`, `:82`, `:112`, `:114`) uses the same
`hx-get`/`hx-target="#detail"`/`hx-push-url="true"` pattern, and both of
its fragment-rendering functions (`renderIndex`'s htmx branch and
`renderDetailWithList`, `internal/apps/paste/handlers.go:374-391` and
`:403-419`) never call `a.deps.Page` at all. Clicking between snippets,
opening the editor, or sharing/deleting one already leaves the shell
breadcrumb and tab title stale in production today.

## Goals

- After any htmx navigation in ON Notes (zoom) or ON Paste
  (snippet-to-snippet, new, edit, cancel, delete-then-select-next), the
  shell breadcrumb's app-name segment and title segment, and the document
  `<title>`, describe the same thing the URL and the main content now do.
- One shared mechanism, defined once in the platform's shared shell
  template, that both apps hook into — not two independent
  reimplementations.
- No new platform Go API surface: `a.deps.Page(r, title)` already builds
  everything needed (`render.Shell`, already request/cookie-derived, no
  I/O) and is already callable from any handler, fragment or not.
- ON Paste gets its own `<title>` fix as part of this work (it currently
  has no fragment-side `<title>` fix at all, unlike Notes).

## Non-goals (explicitly deferred)

- No change to `render.Shell`'s own fields or how `app.NewPage` computes
  them (`ActiveApp`/`LoggedIn`/etc. don't change mid-app-session — only
  `Title` changes on these in-place navigations).
- No change to the connectivity indicator or any other part of the shell
  (`internal/ui/templates/base.html:41-43` and below) — the fix must not
  re-render the whole `shell` block, only the breadcrumb's tail segment,
  precisely to avoid clobbering the connectivity indicator's live
  JS-tracked `data-status`.
- Paste fragments other than `detail-with-list` (e.g. a bare list-only
  response, if one ever exists) are out of scope — only the fragment that
  changes "what page you're now looking at" needs this.
- Flash/Reader: no code for apps that don't exist yet. The mechanism is
  generic enough for them to reuse when they're built, same as the
  existing OOB-toggle/due-badge pattern Notes already established.

## Design

### Shared template: `shell-crumb-tail`

In `internal/ui/templates/base.html`, extract the breadcrumb's last two
conditionals (currently lines 34-38, the app-name segment and the `.Title`
segment) into:

```html
{{define "shell-crumb-tail"}}
{{if .Shell.ActiveAppName}}
<span aria-hidden="true">/</span>
{{if and .Title .Shell.LoggedIn}}<a href="{{.Shell.ActiveAppPath}}" title="{{.Shell.ActiveAppName}}">{{.Shell.ActiveAppName}}</a>{{else}}<span title="{{.Shell.ActiveAppName}}">{{.Shell.ActiveAppName}}</span>{{end}}
{{end}}
{{if .Title}}<span aria-hidden="true">/</span><span title="{{.Title}}">{{.Title}}</span>{{end}}
{{end}}
```

`shell`'s own `nav.shell-crumbs` becomes:

```html
<nav class="shell-crumbs" aria-label="Breadcrumb">
	<a href="/">Home</a>
	<span id="shell-crumb-tail">{{template "shell-crumb-tail" .}}</span>
</nav>
```

(dot is the whole `render.Page` here, same as today — no behavior change
for a full-page render; this is a pure extraction, like Notes'
`outline-heading` extraction in the zoom-transition PR.)

**CSS:** `.shell-crumbs` is `display: flex; gap: var(--s-2);`
(`internal/ui/static/app.css:209-219`) — its direct children get the gap
between them. Wrapping the tail segments in a `<span>` would make that span
a single flex child, collapsing the gap between "Home" and whatever's
inside the span, and between the segments now nested inside it. This is
exactly the spacing regression the zoom-transition PR's final review caught
with `#outline-heading`/`.stack` — fixed here up front instead of by a
follow-up patch: add `#shell-crumb-tail { display: contents; }` to
`app.css`, so the span is invisible to layout and its children remain
direct flex items of `.shell-crumbs`, gap intact.

### Consuming the OOB block: one Go-computed `Title`, reused for both the `<title>` tag and the crumb

**ON Notes** (`internal/apps/notes/handlers.go`, `renderOutlineFragment`,
and `internal/apps/notes/view.go`, `outlineView`):

- Add `Title string` and `Shell render.Shell` to `outlineView`
  (`view.go` gains an import of `internal/platform/render`, already
  imported by `handlers.go`).
- In `renderOutlineFragment`, after the existing `if rootID != RootID`
  block populates `view.Root`/`view.Zoomed`/`view.Crumbs` (added by the
  zoom-transition PR's Task 1.5), compute the title the same way that PR's
  `<title>` fix already does, but as a Go value instead of an inline
  template conditional:
  ```go
  title := ""
  if view.Zoomed {
  	title = view.Root.DisplayTitle()
  }
  page := a.deps.Page(r, title)
  view.Title, view.Shell = page.Title, page.Shell
  ```
  This also removes a small duplication risk the final review flagged
  during that PR: the `<title>` tag was computed inline in the template
  (`{{if .Zoomed}}{{.Root.DisplayTitle}} · {{end}}ON Suite`) while nothing
  else used that logic. Now `outline-swap`'s `<title>` line becomes
  `<title>{{if .Title}}{{.Title}} · {{end}}ON Suite</title>` — sourced from
  the same Go-computed value the new OOB crumb also uses, so the two can
  never drift apart.
- `outline-swap` (`outline.html:32`) gains the OOB block:
  ```html
  {{define "outline-swap"}}<title>{{if .Title}}{{.Title}} · {{end}}ON Suite</title><span id="shell-crumb-tail" hx-swap-oob="true">{{template "shell-crumb-tail" (dict "Title" .Title "Shell" .Shell)}}</span><div id="outline-heading" class="stack" hx-swap-oob="true">{{template "outline-heading" .}}</div>{{template "outline-body" .}}{{template "show-completed-toggle" .}}{{template "due-badge" .}}{{end}}
  ```
- The existing OOB-id allowlist test helper in
  `internal/apps/notes/handlers_test.go` (`assertOnlyKnownOOBIsOOB`) gains
  `"shell-crumb-tail": true`.

**ON Paste** (`internal/apps/paste/handlers.go`, both `renderIndex`'s htmx
branch and `renderDetailWithList`; `indexView` in the same file):

- Add `Title string` and `Shell render.Shell` to `indexView`.
- Both functions already have `detail detailView` in scope and already
  know how to compute the right title via the existing `pageTitle(detail)`
  helper (`handlers.go:325`, already used by the full-page path at
  `:398`). Each sets:
  ```go
  page := a.deps.Page(r, pageTitle(detail))
  view.Title, view.Shell = page.Title, page.Shell
  ```
  before rendering.
- `detail-with-list` (`internal/apps/paste/templates/index.html:24`)
  gains a `<title>` element and the OOB crumb block, in the same style as
  Notes:
  ```html
  {{define "detail-with-list"}}<title>{{if .Title}}{{.Title}} · {{end}}ON Suite</title><span id="shell-crumb-tail" hx-swap-oob="true">{{template "shell-crumb-tail" (dict "Title" .Title "Shell" .Shell)}}</span>{{template "detail-body" .Detail}}{{template "list-items" .List}}<input type="checkbox" id="paste-detail-open" class="visually-hidden" hx-swap-oob="true"{{if .Detail.Mode}} checked{{end}} aria-hidden="true" tabindex="-1">{{end}}
  ```
- Paste has no existing OOB-id allowlist guard test (checked: none
  exists), so no allowlist update is needed there.

### Why `dict` and not a struct field passed directly

Both apps' fragment templates receive their own view struct (`outlineView`,
`indexView`) as the block's dot, not a `render.Page`. `shell-crumb-tail`
expects `.Title`/`.Shell.*`, which are now fields *on* those view structs
(not the structs themselves) — `dict "Title" .Title "Shell" .Shell` (the
`dict` helper already registered in `render.go:109` and already used
elsewhere, e.g. `outline.html:37`'s `notes-search-box` call) builds the
small map `shell-crumb-tail` needs without introducing a new named type or
changing either app's existing render call shape.

## Testing

- **Full-page render, both apps:** no test currently renders and asserts
  `nav.shell-crumbs`' HTML output in either app (checked: the only
  existing shell-crumb-adjacent tests, `internal/platform/app/app_test.go`
  and `internal/platform/render/render_test.go`, assert `Shell` struct
  field computation in Go, not template markup). Add one new test per app
  — a plain (non-htmx) zoomed/detail page load — asserting
  `#shell-crumb-tail`'s rendered content matches today's pre-extraction
  output (app-name segment present/linked, title segment present), so the
  extraction itself is pinned as behavior-preserving rather than relying
  on inference alone.
- **Notes htmx zoom:** a new test confirms an htmx zoom-in response
  contains `id="shell-crumb-tail" hx-swap-oob="true"` with the new root's
  name inside it (mirroring the existing
  `TestZoomingViaHTMXUpdatesTheBreadcrumb`/`...ShowsTheUnzoomedHeading`
  pattern and its `htmlassert`-scoped assertions), and the zoom-out
  counterpart shows the crumb tail collapsed back to just "ON Notes" (no
  third segment).
- **Paste htmx navigation:** a new test confirms selecting a different
  snippet over htmx (or opening the editor, or creating one) yields a
  `detail-with-list` response carrying `id="shell-crumb-tail"
  hx-swap-oob="true"` with that snippet's (or "Edit "-prefixed, or "New
  snippet") title.
- **CSS regression guard:** no automated test for the `display: contents`
  fix (this codebase doesn't test CSS); manual browser verification is
  part of implementation, mirroring how the zoom-transition PR's `.stack`
  fix was verified live.
- **Existing OOB-allowlist guard (Notes only):** update
  `assertOnlyKnownOOBIsOOB`'s allowed-id map; confirm the whole notes
  suite still passes with no other id flagged.
