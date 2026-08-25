# ON Notes N3 — Keyboard Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `notes.js` and the HTMX wiring behind it, so every operation N2 built as a plain form (create, indent, outdent, move, collapse, delete, text save) can also be driven from the keyboard, per spec §8, without regressing the JS-disabled fallback N2 proved.

**Architecture:** N2 already renders every mutation as a form; N3 does not add any new store operation or route beyond one static-file route. It (1) teaches the existing handlers to answer an HTMX request with a fragment instead of a redirect, (2) adds `hx-post`/`hx-target`/`hx-swap` to the existing buttons and inputs so the same markup drives both the no-JS fallback and the AJAX path, and (3) adds `notes.js`, a small hand-written script (no build step, matching `theme.js`'s style) that binds keys to those same buttons and solves the one genuinely new problem: keeping the *currently focused* bullet's text correct when a click or keypress acts on a *different* row.

**Tech Stack:** Go (`net/http`, `html/template`), HTMX (already vendored as `internal/ui/static/htmx.min.js` and used platform-wide via `base.html`'s `hx-headers`), hand-written vanilla JS (ES5-style, no build step — see `internal/ui/static/theme.js`).

## Global Constraints

- No CGO, no new Go dependencies, no Node/npm/JS build step (per `AGENTS.md`). `notes.js` is checked in as plain, hand-written JavaScript, in the same style as `theme.js`: an IIFE, `"use strict"`, `var`/named `function` (no `let`/`const`/arrow functions), a handful of `init*` functions called at the bottom.
- CSP: `script-src 'self'` with no `unsafe-inline` (`internal/platform/web/middleware.go`). `notes.js` must contain no inline event handlers; it is loaded as an external `<script src>`.
- `main` is branch-protected: work on a branch, open a PR, never push directly.
- The local `staticcheck` binary on `$PATH` (v0.6.1 / 2025.1.1) is incompatible with the installed Go toolchain and **silently exits 0** while printing internal-error noise for several stdlib packages. Always run the CI-pinned version instead: `go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...` (matches `.github/workflows/ci.yml`).
- staticcheck's U1000 (unused) check covers test files too, including unexported functions and methods with no caller yet. Add a helper in the same task that gives it a first caller, not earlier.
- `internal/htmlassert` supports only descendant combinators (no `>` child combinator) and exactly one qualifier per selector part (`button[name=dir]` is fine; `button[name=dir][value=up]` is not — locate by one qualifier, then call `htmlassert.Attr` for the rest).
- `notes.js` has no automated tests, by design (spec §17): every request it issues is one a Go handler test in `handlers_test.go` already covers. What it adds — caret placement and focus after a swap — is verified by hand; each task below ends with a manual QA step instead of (or alongside) `go test` where that's the only way to check the behavior.
- HTMX is already loaded on every page via `internal/ui/templates/base.html` (`<script src="{{asset "htmx.min.js"}}" defer>`), and `<body hx-headers='{"X-CSRF-Token": "..."}'>` already makes every HTMX request carry the CSRF header the platform's `CSRF.Middleware` checks first (`internal/platform/web/csrf.go`). No CSRF plumbing is needed for the HTMX path.

## The client/server contract this chunk completes (spec §7)

N2 already saves the *targeted* bullet's own text on every structural request, because in N2 every button lives inside its own row's form. N3 makes it possible for a click or keypress to act on one row while a *different* row has unsaved, in-progress text (e.g., typing in row X, then clicking row Y's indent button before the autosave debounce fires). Spec §7 requires the request to carry the truly-focused bullet's text in that case, in the same transaction as the structural operation. `mutate()` already reads `focus_id`/`title`/`note` generically for every structural route including delete (confirmed by reading `internal/apps/notes/handlers.go`) — this chunk needs no backend change for that part. The missing piece is entirely client-side: overriding the submitted `focus_id`/`title`/`note` with whatever is actually focused, before the request leaves the browser. That is `notes.js`'s `augmentRequest` function (Task 3).

## File Structure

- Modify: `internal/apps/notes/handlers.go` — `mutate` and `setText` answer an HTMX request with a fragment/204 instead of a redirect.
- Create: `internal/apps/notes/static/notes.js` — grows across Tasks 1, 3, 4, 5.
- Modify: `internal/apps/notes/app.go` — embed and serve `static/notes.js`, add the route.
- Modify: `internal/apps/notes/templates/outline.html` — extract the `outline-body` block; add `hx-*` attributes mirroring every `formaction`; add `data-id` to each row; wire the two text inputs' autosave; load `notes.js`.
- Modify: `internal/apps/notes/handlers_test.go` — new HTMX-response tests, new HTMX-wiring tests, one existing test updated (`data-confirm` → `hx-confirm`).
- Modify: `README.md`, `AGENTS.md` — N3 status.

No CSS changes: the bullets look identical: they just become keyboard- and AJAX-driven. No migration: N3 adds no column and no store operation.

---

### Task 1: HTMX response shape — fragment or 204 instead of a redirect

**Files:**
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/templates/outline.html`
- Modify: `internal/apps/notes/app.go`
- Create: `internal/apps/notes/static/notes.js`
- Modify: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `web.IsHTMX(r) bool`, `web.CSRFToken(ctx) string` (`internal/platform/web/context.go`), `Renderer.Fragment(w, status, page, block, data) error` (`internal/platform/render/render.go`), `nest(flat []Node, root int64, csrfToken string) []*outlineRow` (`internal/apps/notes/view.go`), `Store.Outline` (`internal/apps/notes/store.go`).
- Produces: `(a *App) renderOutlineFragment(w, r, userID, rootID int64)`, used by `mutate`'s and `setText`'s HTMX branch. Later tasks do not call it directly — they only add attributes that cause the browser to hit the same routes over AJAX.

No JS keyboard behaviour lands in this task — it only makes the server answer correctly once something (a browser HTMX request, or a test) asks it to.

- [ ] **Step 1: Write the failing tests for the new response shapes**

Add to `internal/apps/notes/handlers_test.go`, near the other `post`/`submit` helpers:

```go
// postHX submits a form the way notes.js's own requests always will: HTMX
// itself sets this header on every request, and the platform's CSRF check
// already accepts it in place of the hidden form field.
func (s *server) postHX(t *testing.T, sess *session, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set(web.CSRFFormField, s.csrfToken(t, sess))
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	return s.do(t, sess, req)
}
```

Then add these tests at the end of the file:

```go
// TestStructuralMutationRespondsWithAFragmentForHTMX. Once notes.js exists,
// every structural button issues this same request over AJAX; the response
// must be the swap target's own content, not a redirect the browser would
// have to follow with a second round trip.
func TestStructuralMutationRespondsWithAFragmentForHTMX(t *testing.T) {
	s := newServer(t)
	first := s.seed(t, s.alice, notes.RootID, "first")
	second := s.seed(t, s.alice, notes.RootID, "second")

	rec := s.postHX(t, s.alice, "/notes/"+itoa(second)+"/indent", url.Values{
		"root": {"0"}, "focus_id": {itoa(second)}, "title": {"second"}, "note": {""},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("an HTMX response carries whole-document chrome")
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if n := len(doc.QueryAll(".outline-item .outline-item")); n != 1 {
		t.Errorf("got %d nested bullets in the fragment, want 1", n)
	}

	children, err := s.store.Children(context.Background(), s.alice.user.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].ID != second {
		t.Fatalf("the indent did not apply: children of first = %+v", children)
	}
}

func TestCreateRespondsWithAFragmentForHTMX(t *testing.T) {
	s := newServer(t)

	rec := s.postHX(t, s.alice, "/notes/new", url.Values{
		"root": {"0"}, "new_title": {"first"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "first") {
		t.Error("the new bullet is not in the fragment")
	}
}

// TestSetTextRespondsWithNoContentForHTMX: nothing visible changes from a
// text-only save — the input already shows what was typed — so there is
// nothing to swap.
func TestSetTextRespondsWithNoContentForHTMX(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "old")

	rec := s.postHX(t, s.alice, "/notes/"+itoa(id)+"/text", url.Values{
		"root": {"0"}, "focus_id": {itoa(id)}, "title": {"new"}, "note": {""},
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}

	n, err := s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "new" {
		t.Errorf("saved title = %q, want new", n.Title)
	}
}

func TestAFailedStructuralOperationUnderHTMXIsAFragment(t *testing.T) {
	s := newServer(t)
	bobs := s.seed(t, s.bob, notes.RootID, "bob's")

	rec := s.postHX(t, s.alice, "/notes/"+itoa(bobs)+"/indent", url.Values{"root": {"0"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("an HTMX error response carries whole-document chrome")
	}
}

func TestScriptIsServedWithAJavaScriptContentType(t *testing.T) {
	s := newServer(t)
	rec := s.do(t, s.alice, httptest.NewRequest("GET", "/notes/notes.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /notes/notes.js = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("Content-Type = %q, want text/javascript", ct)
	}
	if !strings.Contains(rec.Body.String(), "use strict") {
		t.Error("the served script does not look like notes.js")
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/notes/... -run 'HTMX|ScriptIsServed' -v`
Expected: FAIL — `mutate`/`setText` still redirect regardless of `HX-Request`, and `GET /notes/notes.js` 404s.

- [ ] **Step 3: Create the `notes.js` skeleton and embed it**

Create `internal/apps/notes/static/notes.js`:

```js
// internal/apps/notes/static/notes.js
//
// Implements no behaviour of its own — spec §7. Every request it issues is
// one a plain form already issues and a handler test in handlers_test.go
// already covers; N2 built and tested all of them before this file existed.
// It has no tests of its own, by design (spec §17): what a handler test
// cannot see is caret placement after a swap, which is verified by hand
// instead — see the final task's QA checklist.

(function () {
	"use strict";
})();
```

Modify `internal/apps/notes/app.go`: add the embed and the handler, and register the route. Add near the top, alongside the existing `templateFiles` embed:

```go
//go:embed static/notes.js
var scriptFiles embed.FS
```

Add this method (anywhere after `Templates`):

```go
// script serves notes.js. It sits behind the same sign-in requirement as
// every other route in this app: there is no page that loads it without
// already being on an authenticated outline, and the design reserves the
// app's one public route for N9's share link.
func (a *App) script(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, scriptFiles, "static/notes.js")
}
```

`app.go` does not import `net/http` yet (only `handlers.go` does) — add it to `app.go`'s import block:

```go
import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)
```

Update `Mount`'s route-map comment and add the route:

```go
//	GET  /notes/{$}          the top-level outline; {$} matches only the
//	                         trailing-slash form, so it cannot collide with
//	                         the {id} pattern below
//	GET  /notes/{id}         the outline zoomed to one node. {id} never
//	                         matches an empty segment, which is what keeps it
//	                         and {$} disjoint
//	GET  /notes/notes.js     a literal segment, so it outranks {id} even
//	                         though "notes.js" is not a valid id
//	POST /notes/new          a literal segment, and literals outrank {id}
//	POST /notes/{id}/text    and the seven other mutations: two segments
//	                         deeper than the zoom URL, so no pattern in this
//	                         list is a prefix of another
func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	a.store = NewStore(deps.DB)

	r.HandleFunc("GET /{$}", a.outline)
	r.HandleFunc("GET /{id}", a.outlineZoomed)
	r.HandleFunc("GET /notes.js", a.script)
	r.HandleFunc("POST /new", a.create)
	r.HandleFunc("POST /{id}/text", a.setText)
	r.HandleFunc("POST /{id}/indent", a.indent)
	r.HandleFunc("POST /{id}/outdent", a.outdent)
	r.HandleFunc("POST /{id}/move", a.move)
	r.HandleFunc("POST /{id}/collapse", a.collapse)
	r.HandleFunc("POST /{id}/delete", a.remove)
}
```

- [ ] **Step 4: Extract the `outline-body` block**

Modify `internal/apps/notes/templates/outline.html`. Replace:

```html
	{{/* #outline is the swap target N3 will replace on every structural
	     response, which is why the rows live in a block of their own. */}}
	<div id="outline" class="outline">
		{{if .Data.Rows}}
		{{template "outline-rows" .Data.Rows}}
		{{else}}
		{{template "outline-first" .Data}}
		{{end}}
	</div>
```

with:

```html
	{{/* #outline is N3's swap target. Its content is its own named block so
	     a structural response can re-render just this, through
	     Renderer.Fragment, without touching the breadcrumb or heading above
	     it — neither ever changes as a side effect of a structural op. */}}
	<div id="outline" class="outline">
		{{template "outline-body" .Data}}
	</div>
```

And add this block definition alongside the others (after `{{end}}` that closes the `content` block, before `outline-rows`):

```html
{{define "outline-body"}}
{{if .Rows}}
{{template "outline-rows" .Rows}}
{{else}}
{{template "outline-first" .}}
{{end}}
{{end}}
```

- [ ] **Step 5: Add the fragment renderer and wire the two response points**

Modify `internal/apps/notes/handlers.go`. Add this function after `renderOutline`:

```go
// renderOutlineFragment re-renders #outline's own content for an HTMX swap.
// It shares renderOutline's query but not its shell: a structural response
// never changes which node the page is zoomed to, so the breadcrumb and
// heading stay exactly as the browser already has them, and there is no
// need to look the root node up — Root.ID is all outline-body reads, and
// the caller already has it as a plain int64.
func (a *App) renderOutlineFragment(w http.ResponseWriter, r *http.Request, userID, rootID int64) {
	flat, err := a.store.Outline(r.Context(), userID, rootID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view := outlineView{
		CSRFToken: web.CSRFToken(r.Context()),
		Root:      Node{ID: rootID},
	}
	view.Rows = nest(flat, rootID, view.CSRFToken)
	if err := a.deps.Render.Fragment(w, http.StatusOK, "notes/outline", "outline-body", view); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
```

In `mutate`, replace the final two lines:

```go
	http.Redirect(w, r, outlinePath(root), http.StatusSeeOther)
}
```

with:

```go
	if web.IsHTMX(r) {
		a.renderOutlineFragment(w, r, userID, root)
		return
	}
	http.Redirect(w, r, outlinePath(root), http.StatusSeeOther)
}
```

In `setText`, replace:

```go
	if err := a.store.SetText(r.Context(), userID, id, r.PostFormValue("title"), r.PostFormValue("note")); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, outlinePath(root), http.StatusSeeOther)
}
```

with:

```go
	if err := a.store.SetText(r.Context(), userID, id, r.PostFormValue("title"), r.PostFormValue("note")); err != nil {
		a.fail(w, r, err)
		return
	}
	if web.IsHTMX(r) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, outlinePath(root), http.StatusSeeOther)
}
```

`fail` already routes through `Errors.Status`, which already branches on `web.IsHTMX` (`internal/platform/web/errors.go`) — the error path needs no change here.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, including every existing N2 test (the non-HTMX path is untouched) and the new ones from Step 1.

- [ ] **Step 7: Full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/notes/handlers.go internal/apps/notes/handlers_test.go internal/apps/notes/templates/outline.html internal/apps/notes/app.go internal/apps/notes/static/notes.js
git commit -m "notes: answer HTMX requests with a fragment instead of a redirect"
```

---

### Task 2: Progressive-enhancement wiring on the templates

**Files:**
- Modify: `internal/apps/notes/templates/outline.html`
- Modify: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: nothing new — this task only adds `data-*`/`hx-*` attributes to markup Task 1 already serves correctly for both response shapes.
- Produces: `data-id="{{.ID}}"` on every `.outline-row`, which every later JS task locates rows by; the `hx-post`/`hx-target="#outline"`/`hx-swap="innerHTML"` triple on every structural button and form, which JS drives entirely by clicking; `hx-confirm` (replacing `data-confirm`) on the delete form.

After this task, a JS-enabled browser is already keyboard-free but *mouse*-complete over AJAX — every existing button works exactly as before, just without a full page reload. No keyboard binding exists yet.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/notes/handlers_test.go`:

```go
// TestEveryStructuralButtonMirrorsItsFormactionAsHTMX. Progressive
// enhancement: hx-post always equals formaction, so a JS-disabled browser
// and an HTMX one issue the exact same request the button already
// declares — nothing in notes.js needs to know a URL.
func TestEveryStructuralButtonMirrorsItsFormactionAsHTMX(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "a")

	doc := s.get(t, s.alice, "/notes/")
	buttons := doc.QueryAll("button[formaction]")
	if len(buttons) == 0 {
		t.Fatal("no formaction buttons found")
	}
	for _, b := range buttons {
		action, _ := htmlassert.Attr(b, "formaction")
		hxPost, ok := htmlassert.Attr(b, "hx-post")
		if !ok || hxPost != action {
			t.Errorf("button formaction=%q has hx-post=%q", action, hxPost)
		}
		if got, _ := htmlassert.Attr(b, "hx-target"); got != "#outline" {
			t.Errorf("button formaction=%q has hx-target=%q, want #outline", action, got)
		}
		if got, _ := htmlassert.Attr(b, "hx-swap"); got != "innerHTML" {
			t.Errorf("button formaction=%q has hx-swap=%q, want innerHTML", action, got)
		}
	}
}

func TestRowsCarryTheirNodeID(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	doc := s.get(t, s.alice, "/notes/")
	row := doc.MustHave(".outline-row")
	if got, _ := htmlassert.Attr(row, "data-id"); got != itoa(id) {
		t.Errorf("row data-id = %q, want %q", got, itoa(id))
	}
}

// TestTextInputsAutosaveOverHTMX. hx-swap=none: nothing on screen needs to
// change from a text-only save, the input already shows what was typed.
func TestTextInputsAutosaveOverHTMX(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "a")

	doc := s.get(t, s.alice, "/notes/")
	for _, sel := range []string{"input.outline-title", "input.outline-note"} {
		in := doc.MustHave(sel)
		if got, _ := htmlassert.Attr(in, "hx-post"); got != "/notes/"+itoa(id)+"/text" {
			t.Errorf("%s hx-post = %q", sel, got)
		}
		if got, _ := htmlassert.Attr(in, "hx-swap"); got != "none" {
			t.Errorf("%s hx-swap = %q, want none", sel, got)
		}
		if _, ok := htmlassert.Attr(in, "hx-trigger"); !ok {
			t.Errorf("%s has no hx-trigger", sel)
		}
	}
}

func TestEmptyOutlineFormIsHTMXWired(t *testing.T) {
	s := newServer(t)
	doc := s.get(t, s.alice, "/notes/")
	form := doc.MustHave(`form[action=/notes/new]`)
	if got, _ := htmlassert.Attr(form, "hx-post"); got != "/notes/new" {
		t.Errorf("empty-outline form hx-post = %q", got)
	}
}
```

Modify the existing `TestDeleteIsItsOwnConfirmedForm` (the confirmation moves from the platform's generic `data-confirm` to HTMX's own `hx-confirm`, since the form is now `hx-post`-driven and HTMX's built-in confirm runs before it ever builds the request — no dependency on `theme.js`'s submit-event listener, and no ordering question between the two). Replace it entirely with:

```go
// TestDeleteIsItsOwnConfirmedForm. hx-confirm, not data-confirm: once the
// form is hx-post-driven, HTMX's own confirmation runs before it builds the
// request at all, so there is no dependency on theme.js's generic
// data-confirm listener or any question of which one runs first.
func TestDeleteIsItsOwnConfirmedForm(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "Projects")

	doc := s.get(t, s.alice, "/notes/")
	form := doc.MustHave(`form[action=/notes/` + itoa(id) + `/delete]`)
	if _, ok := htmlassert.Attr(form, "hx-confirm"); !ok {
		t.Error("the delete form asks for no confirmation")
	}
	if got, _ := htmlassert.Attr(form, "hx-post"); got != "/notes/"+itoa(id)+"/delete" {
		t.Errorf("delete form hx-post = %q", got)
	}
	if got, _ := htmlassert.Attr(form, "hx-target"); got != "#outline" {
		t.Errorf("delete form hx-target = %q, want #outline", got)
	}
	// It must not be the row's main form, or every button would be confirmed.
	if cls, _ := htmlassert.Attr(form, "class"); !strings.Contains(cls, "outline-delete") {
		t.Errorf("the delete form's class is %q", cls)
	}
	doc.MustHave(`form.outline-delete input[name=csrf_token]`)
	doc.MustHave(`form.outline-delete input[name=root]`)
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/notes/... -run 'HTMX|NodeID|Autosave|EmptyOutlineForm|ConfirmedForm' -v`
Expected: FAIL — none of the `hx-*`/`data-id` attributes exist yet.

- [ ] **Step 3: Wire the template**

Modify `internal/apps/notes/templates/outline.html`. In the `outline-rows` block, change the row wrapper:

```html
		<div class="outline-row">
```

to:

```html
		<div class="outline-row" data-id="{{.ID}}">
```

Change the four buttons that carry a `formaction` (collapse, move up, move down, indent, outdent, "+") to add the matching `hx-*` triple. The full row form becomes:

```html
			<form method="post" action="/notes/{{.ID}}/text" class="outline-main">
				<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
				<input type="hidden" name="root" value="{{.RootID}}">
				<input type="hidden" name="focus_id" value="{{.ID}}">

				{{if .HasChildren}}
				<button type="submit" class="outline-chevron quiet"
				        formaction="/notes/{{.ID}}/collapse"
				        hx-post="/notes/{{.ID}}/collapse" hx-target="#outline" hx-swap="innerHTML"
				        name="collapsed" value="{{if .Collapsed}}0{{else}}1{{end}}"
				        aria-expanded="{{if .Collapsed}}false{{else}}true{{end}}"
				        aria-label="{{if .Collapsed}}Expand{{else}}Collapse{{end}}">{{if .Collapsed}}&#9656;{{else}}&#9662;{{end}}</button>
				{{else}}
				<span class="outline-chevron" aria-hidden="true"></span>
				{{end}}

				<a class="outline-dot{{if .Collapsed}} outline-dot-full{{end}}"
				   href="/notes/{{.ID}}"
				   aria-label="Zoom in to {{.DisplayTitle}}">&bull;</a>

				<span class="outline-text">
					<input class="outline-title" type="text" name="title"
					       value="{{.Title}}" maxlength="2000" aria-label="Bullet"
					       hx-post="/notes/{{.ID}}/text"
					       hx-trigger="input changed delay:600ms, blur changed" hx-swap="none">
					<input class="outline-note" type="text" name="note"
					       value="{{.Note}}" maxlength="10000"
					       placeholder="note" aria-label="Note"
					       hx-post="/notes/{{.ID}}/text"
					       hx-trigger="input changed delay:600ms, blur changed" hx-swap="none">
				</span>

				<span class="outline-actions">
					<button type="submit" class="quiet" formaction="/notes/{{.ID}}/move"
					        hx-post="/notes/{{.ID}}/move" hx-target="#outline" hx-swap="innerHTML"
					        name="dir" value="up" aria-label="Move up"
					        {{if eq .Position 0}}disabled{{end}}>&uarr;</button>
					<button type="submit" class="quiet" formaction="/notes/{{.ID}}/move"
					        hx-post="/notes/{{.ID}}/move" hx-target="#outline" hx-swap="innerHTML"
					        name="dir" value="down" aria-label="Move down"
					        {{if .Last}}disabled{{end}}>&darr;</button>
					<button type="submit" class="quiet" formaction="/notes/{{.ID}}/indent"
					        hx-post="/notes/{{.ID}}/indent" hx-target="#outline" hx-swap="innerHTML"
					        aria-label="Indent"
					        {{if eq .Position 0}}disabled{{end}}>&#8677;</button>
					<button type="submit" class="quiet" formaction="/notes/{{.ID}}/outdent"
					        hx-post="/notes/{{.ID}}/outdent" hx-target="#outline" hx-swap="innerHTML"
					        aria-label="Outdent"
					        {{if eq .Depth 0}}disabled{{end}}>&#8676;</button>
					<button type="submit" class="quiet" formaction="/notes/new"
					        hx-post="/notes/new" hx-target="#outline" hx-swap="innerHTML"
					        aria-label="Add a bullet below">+</button>
				</span>
			</form>

			<form method="post" action="/notes/{{.ID}}/delete" class="outline-delete"
			      hx-post="/notes/{{.ID}}/delete" hx-target="#outline" hx-swap="innerHTML"
			      hx-confirm="Delete “{{.DisplayTitle}}” and everything under it? This cannot be undone.">
				<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
				<input type="hidden" name="root" value="{{.RootID}}">
				<button type="submit" class="quiet" aria-label="Delete">&times;</button>
			</form>
```

And the empty-outline form (`outline-first`) gets the same treatment:

```html
			<form method="post" action="/notes/new" class="outline-main"
			      hx-post="/notes/new" hx-target="#outline" hx-swap="innerHTML">
```

(The rest of `outline-first` is unchanged — it has no `data-id`, since no node exists yet.)

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, all of it, old and new.

- [ ] **Step 5: Full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/notes/templates/outline.html internal/apps/notes/handlers_test.go
git commit -m "notes: wire every button and text input for HTMX, mirroring their form fallback"
```

---

### Task 3: `notes.js` — focus sync (the one-write contract, client side)

**Files:**
- Modify: `internal/apps/notes/static/notes.js`

**Interfaces:**
- Consumes: `data-id` on `.outline-row` (Task 2), the global `htmx` object (loaded by `base.html`).
- Produces: `lastFocus`, `pendingFocus` (module-private state); `initFocusSync()`, called at the bottom of the file. Task 4 and Task 5's manual `htmx.ajax` calls depend on the `_skipFocusOverride` convention this task establishes.

This is spec §19's highest-risk piece, and has no automated test — Step 3 is a manual QA pass instead of `go test`.

- [ ] **Step 1: Add the focus-sync functions**

Modify `internal/apps/notes/static/notes.js`. Replace the empty IIFE body with:

```js
(function () {
	"use strict";

	// ---- focus sync -------------------------------------------------------
	//
	// spec §7: every structural POST must carry the text of whatever bullet
	// is actually focused, not just the bullet whose button was clicked.
	// Clicking one row's control while typing in another is the case N2's
	// forms could never produce — each row was its own isolated form — and
	// it is real now that every row's inputs autosave independently.
	//
	// lastFocus, not document.activeElement read at request time: by the
	// time a click's handler runs, the browser has already moved focus to
	// the button that was clicked (browsers focus a clicked control before
	// dispatching its click event), so activeElement would report the
	// button, never the field the user was actually editing a moment ago.
	var lastFocus = null; // {id, field}
	var pendingFocus = null; // {id, field, offset} or {afterID, field, offset}

	function isOutlineField(el) {
		return !!(el && el.matches && el.matches(".outline-title, .outline-note"));
	}

	function rowOf(el) {
		return el && el.closest && el.closest(".outline-row");
	}

	function trackFocus() {
		document.addEventListener("focusin", function (e) {
			var input = e.target;
			if (!isOutlineField(input)) return;
			var row = rowOf(input);
			if (!row || !row.hasAttribute("data-id")) return; // the empty-outline's bootstrap field
			lastFocus = { id: row.getAttribute("data-id"), field: input.name };
		});
	}

	// augmentRequest runs before every HTMX request this page issues. A
	// hand-built request (see splitAndCreate and maybeDeleteEmptyBullet in
	// the keyboard module) already knows exactly which focus_id/title/note
	// it wants and marks itself with _skipFocusOverride so this does not
	// clobber them.
	function augmentRequest(e) {
		var params = e.detail.parameters;
		if (params._skipFocusOverride) {
			delete params._skipFocusOverride;
			return;
		}
		if (!lastFocus) return;

		var row = document.querySelector('.outline-row[data-id="' + lastFocus.id + '"]');
		var input = row && row.querySelector('input[name="' + lastFocus.field + '"]');
		if (!row || !input) return;

		var title = row.querySelector('input[name="title"]');
		var note = row.querySelector('input[name="note"]');
		params.focus_id = lastFocus.id;
		if (title) params.title = title.value;
		if (note) params.note = note.value;

		pendingFocus = { id: lastFocus.id, field: lastFocus.field, offset: input.selectionStart || 0 };
	}

	// restoreFocus runs after every HTMX swap of #outline. hx-swap=innerHTML
	// destroys and recreates every row, so the browser drops focus to
	// <body> unless this puts it back. afterID (rather than id) is how
	// splitAndCreate asks for "the row after this one": the new row's id is
	// assigned by the server and unknown until the response arrives.
	function restoreFocus() {
		if (!pendingFocus) return;
		var input;

		if (pendingFocus.afterID !== undefined) {
			var anchor = document.querySelector('.outline-row[data-id="' + pendingFocus.afterID + '"]');
			var li = anchor && anchor.closest(".outline-item");
			var nextLi = li && li.nextElementSibling;
			var nextRow = nextLi && nextLi.querySelector(".outline-row");
			input = nextRow && nextRow.querySelector('input[name="' + pendingFocus.field + '"]');
		} else {
			var row = document.querySelector('.outline-row[data-id="' + pendingFocus.id + '"]');
			input = row && row.querySelector('input[name="' + pendingFocus.field + '"]');
		}

		if (input) {
			input.focus();
			var pos = Math.min(pendingFocus.offset, input.value.length);
			input.setSelectionRange(pos, pos);
		}
		pendingFocus = null;
	}

	function initFocusSync() {
		if (!document.getElementById("outline")) return;
		trackFocus();
		document.body.addEventListener("htmx:configRequest", augmentRequest);
		document.body.addEventListener("htmx:afterSwap", restoreFocus);
	}

	initFocusSync();
})();
```

- [ ] **Step 2: Build check**

There is no Go code in this task and no JS test runner in this project (no Node, per the global constraints). Verify the file is at least syntactically valid with the Go toolchain's JS-agnostic tools unavailable to catch it — use the browser itself:

Run: `go run ./cmd/onsuite serve` (with a throwaway data dir, e.g. `--data-dir /tmp/onsuite-n3-qa`), then open the served `GET /notes/notes.js` in a browser tab directly. A syntax error shows as a blank response with a console error on any page that loads it (the outline page); a clean load shows the file's text with no error.

- [ ] **Step 3: Manual QA — the one-write contract**

With the dev server running and signed in:

1. Create two top-level bullets, "first" and "second".
2. Click into "first"'s title and type additional text, but do **not** wait 600ms and do **not** click away (so the autosave debounce has not fired).
3. Immediately click "second"'s indent button.
4. Confirm the page swaps to show "second" nested under "first", **and** that "first"'s typed text survived (reload the page to rule out the debounce having quietly fired anyway — with the network tab open, confirm only one request fired for the indent, not a separate one for "first"'s text).

This is `TestFocusAndTargetCanDiffer` and `TestStructuralRequestSavesTheFocusedText` (`handlers_test.go`) exercised from a real browser instead of a raw POST — those tests prove the server accepts and applies mismatched focus/target correctly; this step proves the client actually sends it that way when it matters.

- [ ] **Step 4: Commit**

```bash
git add internal/apps/notes/static/notes.js
git commit -m "notes: add the client-side focus-sync module (spec §7, §19)"
```

---

### Task 4: `notes.js` — keyboard bindings that reuse an existing button

**Files:**
- Modify: `internal/apps/notes/static/notes.js`

**Interfaces:**
- Consumes: `.outline-row[data-id]`, `button[formaction$="..."]`, `button[name=dir][value=...]`, `button.outline-chevron` (all already rendered by Task 2's template).
- Produces: `initKeyboard()`, called at the bottom of the file alongside `initFocusSync()`.

Covers spec §8's `Tab`/`Shift+Tab`, `Cmd+Shift+Up`/`Down`, `Cmd+.`, and `Esc`. Each of these already has a button wired by Task 2; the binding is "find the right button in the focused row and click it" (a disabled `<button>` does not dispatch a click when `.click()` is called on it, so the edge-of-tree no-ops N2 already made disabled need no separate handling here).

- [ ] **Step 1: Add the keyboard dispatcher and its bindings**

Modify `internal/apps/notes/static/notes.js`. Insert the following block after `initFocusSync`'s closing `}` and before the final `initFocusSync();` call:

```js
	// ---- keyboard: reuse an existing button --------------------------------

	function click(btn) {
		if (btn && !btn.disabled) btn.click();
	}

	function indentButton(row) { return row.querySelector('button[formaction$="/indent"]'); }
	function outdentButton(row) { return row.querySelector('button[formaction$="/outdent"]'); }
	function moveButton(row, dir) { return row.querySelector('button[name="dir"][value="' + dir + '"]'); }
	function collapseButton(row) { return row.querySelector("button.outline-chevron"); }

	// handleEscape: first press leaves editing (blur); a second press,
	// with nothing left focused in the outline, zooms out one level via the
	// breadcrumb's last link — its own immediate parent. No extra state is
	// needed to tell "first press" from "second": document.activeElement
	// already tells the two apart, since the first press moves it out of
	// the outline.
	function handleEscape() {
		var active = document.activeElement;
		if (isOutlineField(active)) {
			active.blur();
			return;
		}
		var crumbs = document.querySelectorAll(".outline-crumbs a");
		if (crumbs.length === 0) return;
		location.href = crumbs[crumbs.length - 1].href;
	}

	function handleKeydown(e) {
		if (e.key === "Escape") {
			handleEscape();
			return;
		}

		var el = e.target;
		if (!isOutlineField(el)) return;
		var row = rowOf(el);
		if (!row || !row.hasAttribute("data-id")) return; // the empty-outline's bootstrap field

		if (e.key === "Tab" && !e.shiftKey) {
			e.preventDefault();
			click(indentButton(row));
			return;
		}
		if (e.key === "Tab" && e.shiftKey) {
			e.preventDefault();
			click(outdentButton(row));
			return;
		}
		if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key === "ArrowUp") {
			e.preventDefault();
			click(moveButton(row, "up"));
			return;
		}
		if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key === "ArrowDown") {
			e.preventDefault();
			click(moveButton(row, "down"));
			return;
		}
		if ((e.metaKey || e.ctrlKey) && e.key === ".") {
			e.preventDefault();
			click(collapseButton(row));
			return;
		}
	}

	function initKeyboard() {
		if (!document.getElementById("outline")) return;
		document.addEventListener("keydown", handleKeydown);
	}
```

Update the bottom of the file:

```js
	initFocusSync();
	initKeyboard();
})();
```

- [ ] **Step 2: Manual QA**

With the dev server running:

1. Create three bullets "a", "b", "c". Focus "b"'s title. Press `Tab`: "b" nests under "a". Press `Shift+Tab`: "b" returns to the top level.
2. With "b" focused, press `Cmd+Shift+Down` (macOS) or `Ctrl+Shift+Down` (elsewhere): "b" and "c" swap. Press the Up variant to swap back.
3. Give "a" a child. Focus "a"'s title, press `Cmd+.`/`Ctrl+.`: the child disappears (collapsed) and the chevron flips; press it again: the child reappears.
4. Focus any title, press `Escape` once: focus leaves the field (confirm by checking nothing in the outline has a visible focus ring). Press `Escape` again while zoomed into a bullet: the page navigates to the parent level. Press it again at the top level: nothing happens (no crumbs to navigate to).
5. Confirm every one of the above still degrades correctly with JavaScript disabled (the existing buttons work exactly as they did at the end of N2 — this is the regression check for the fallback path).

- [ ] **Step 3: Commit**

```bash
git add internal/apps/notes/static/notes.js
git commit -m "notes: bind Tab, move, collapse and Escape to their existing buttons"
```

---

### Task 5: `notes.js` — Enter, Shift+Enter and Backspace-to-delete

**Files:**
- Modify: `internal/apps/notes/static/notes.js`

**Interfaces:**
- Consumes: `pendingFocus`/`_skipFocusOverride` convention from Task 3; `htmx.ajax` (global, from `htmx.min.js`).
- Produces: bindings added to `handleKeydown` (Task 4); no new file-level exports.

These three do not reduce to "click an existing button": Enter needs to split live, unsaved text at the caret before any button-shaped request could describe it; Backspace needs to delete *without* the confirmation `hx-confirm` puts on the visible delete button, because deleting an already-empty leaf loses nothing. Both bypass `augmentRequest`'s generic override via the `_skipFocusOverride` marker Task 3 established, since they compute exactly which fields they want themselves.

- [ ] **Step 1: Add the bindings**

Modify `internal/apps/notes/static/notes.js`. Insert after the keyboard-dispatch functions from Task 4 (still before `initKeyboard`'s definition, or anywhere in that section — order among sibling functions does not matter):

```js
	// ---- keyboard: new requests --------------------------------------------

	function titleInputs() {
		return Array.prototype.slice.call(document.querySelectorAll("#outline input.outline-title"));
	}

	function appendSiblingBelow(row) {
		click(row.querySelector('button[formaction="/notes/new"]'));
	}

	// focusNote moves the caret to the note line under the current bullet,
	// at the end of whatever text is already there — spec §8's Shift+Enter.
	function focusNote(row) {
		var note = row.querySelector("input.outline-note");
		if (!note) return;
		note.focus();
		var pos = note.value.length;
		note.setSelectionRange(pos, pos);
	}

	// splitAndCreate is spec §8's Enter: a new sibling below, splitting the
	// title at the caret. In the note field there is nothing to split — a
	// note is not the tree structure — so Enter there behaves like the "+"
	// button instead.
	function splitAndCreate(el, row) {
		if (!el.classList.contains("outline-title")) {
			appendSiblingBelow(row);
			return;
		}

		var pos = el.selectionStart;
		var head = el.value.slice(0, pos);
		var tail = el.value.slice(pos);
		var note = row.querySelector('input[name="note"]');
		var rootField = row.querySelector('input[name="root"]');
		var id = row.getAttribute("data-id");

		// The new row's id is assigned by the server and unknown until the
		// response arrives, so the caret target is "the row right after
		// this one" rather than an id — see restoreFocus's afterID branch.
		pendingFocus = { afterID: id, field: "title", offset: 0 };

		htmx.ajax("POST", "/notes/new", {
			source: document.body,
			target: "#outline",
			swap: "innerHTML",
			values: {
				root: rootField.value,
				focus_id: id,
				title: head,
				note: note ? note.value : "",
				new_title: tail,
				_skipFocusOverride: "1"
			}
		});
	}

	// maybeDeleteEmptyBullet is spec §8's Backspace: only when the bullet is
	// genuinely empty (no title, no note, no children) and the caret sits
	// at its very start — a leaf with nothing in it loses nothing by going
	// away without the confirmation the visible delete button asks for.
	function maybeDeleteEmptyBullet(el, row) {
		if (!el.classList.contains("outline-title")) return false;
		if (el.selectionStart !== 0 || el.selectionEnd !== 0) return false;
		if (el.value !== "") return false;

		var note = row.querySelector('input[name="note"]');
		if (note && note.value !== "") return false;
		if (row.closest(".outline-item").querySelector(".outline-list")) return false; // has children

		var inputs = titleInputs();
		var idx = inputs.indexOf(el);
		if (idx <= 0) return false; // nothing before it to land on

		var prev = inputs[idx - 1];
		var rootField = row.querySelector('input[name="root"]');
		var id = row.getAttribute("data-id");

		pendingFocus = { id: rowOf(prev).getAttribute("data-id"), field: "title", offset: prev.value.length };

		htmx.ajax("POST", "/notes/" + id + "/delete", {
			source: document.body,
			target: "#outline",
			swap: "innerHTML",
			values: { root: rootField.value, _skipFocusOverride: "1" }
		});
		return true;
	}
```

Extend `handleKeydown` (from Task 4) with these branches, inserted after the `Cmd+.` check and before the closing `}`:

```js
		if (e.key === "Enter" && e.shiftKey) {
			e.preventDefault();
			focusNote(row);
			return;
		}
		if (e.key === "Enter" && !e.shiftKey) {
			e.preventDefault();
			splitAndCreate(el, row);
			return;
		}
		if (e.key === "Backspace") {
			if (maybeDeleteEmptyBullet(el, row)) {
				e.preventDefault();
			}
			return;
		}
```

- [ ] **Step 2: Manual QA**

1. Create a bullet titled `hello world`. Click into its title between "hello" and "world" (after the space). Press `Enter`. Confirm: the bullet now reads "hello", a new sibling below reads "world", and the caret is at the very start of "world"'s title, ready to keep typing.
2. Focus a bullet's title, press `Shift+Enter`. Confirm the caret lands in that same bullet's note field, at the end of whatever text is already there.
3. Create an empty bullet below a bullet titled "keep". Focus the empty bullet's (empty) title, caret at position 0 (it has nowhere else to be). Press `Backspace`. Confirm: the empty bullet is gone, with no confirmation dialog, and the caret is at the end of "keep"'s title.
4. Give a bullet a child, then make its own title and note empty. Focus its title, press `Backspace`. Confirm it is **not** deleted (it has children) — this is the guard that must never silently drop a subtree.
5. Confirm `Enter` and `Shift+Enter` still degrade sanely with JavaScript disabled: `Enter` in a text input submits the row's form natively (the same request the "+" button issues, appending an empty sibling — no split, since there is no caret-splitting without JS), and `Shift+Enter` does nothing special (the note field is already reachable by clicking it).

- [ ] **Step 3: Commit**

```bash
git add internal/apps/notes/static/notes.js
git commit -m "notes: bind Enter, Shift+Enter and Backspace-on-empty"
```

---

### Task 6: Docs and final verification

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md` (checked, see Step 2)

- [ ] **Step 1: Update README.md**

Replace this sentence in the intro paragraph (the one starting "Of the four apps..."):

> **ON Notes** is a hierarchical outliner — one infinite tree per account, with zoom, collapse and every structural operation; the keyboard layer, Markdown, due dates and search are still being built out.

with:

> **ON Notes** is a hierarchical outliner — one infinite tree per account, with zoom, collapse, every structural operation and a full keyboard layer: type, indent, reorder and delete without touching the mouse. Markdown, due dates and search are still being built out.

Replace, in the Status section:

> Work since then is per-app rather than per-phase. ON Notes is being built in
> ten small chunks under
> [`docs/superpowers/plans/2026-08-25-on-notes-design.md`](docs/superpowers/specs/2026-08-25-on-notes-design.md);
> N1 (schema and store) and N2 (the outline) are done.

with:

> Work since then is per-app rather than per-phase. ON Notes is being built in
> ten small chunks under
> [`docs/superpowers/plans/2026-08-25-on-notes-design.md`](docs/superpowers/specs/2026-08-25-on-notes-design.md);
> N1 (schema and store), N2 (the outline) and N3 (the keyboard layer) are
> done.

- [ ] **Step 2: Check AGENTS.md**

`AGENTS.md`'s only ON Notes mention is: "**ON Notes** (a hierarchical outliner, still being built out) are registered today". This is a one-line summary with no per-chunk breakdown (unlike README's Status table), and "still being built out" remains accurate until N9/N10 land — leave it unchanged. Do not add a chunk-status table here; that duplication is exactly what README's own Status section exists to hold in one place.

- [ ] **Step 3: Final full check**

Run: `gofmt -l . && go build ./... && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race`
Expected: clean.

- [ ] **Step 4: Manual browser pass over the whole keyboard table**

Run `go run ./cmd/onsuite serve` with a throwaway data dir, sign in, and, in one sitting, exercise every row of spec §8 that this chunk delivers end to end on a fresh outline: `Enter`, `Shift+Enter`, `Tab`/`Shift+Tab`, `Backspace` on an empty leaf, `Up`/`Down` between bullets (confirm the caret's horizontal offset is preserved, clamped to the target's length), `Cmd+Shift+Up`/`Down`, `Cmd+.`, `Esc` (both presses), and clicking a bullet's dot to zoom in. Also reload mid-session and confirm autosave held (type in a bullet, wait a beat past 600ms, reload, confirm the text is there).

- [ ] **Step 5: Commit**

```bash
git add README.md
git commit -m "notes: mark N3 (the keyboard layer) done"
```

---

## Self-review notes for the executing agent

- **`renderOutlineFragment` never queries for the root node.** It builds `Node{ID: rootID}` directly instead of calling `Store.ByID`. This looks like a shortcut but is not one: `outline-body` only ever reads `.Root.ID` (for the hidden `root` field), never the root's title or note, and `outlinePath(root)` already trusts an unvalidated `root` value for the exact same purpose in the non-HTMX branch right next to it. Do not "fix" this by adding a `ByID` call — it would be an unused, ownership-irrelevant query.
- **`_skipFocusOverride` is a private wire-format field, not a real form field.** It exists purely so `augmentRequest` can tell "a hand-built request that already knows exactly what it wants" apart from "a plain button click that should get the truly-focused row's live text." The Go handlers never see it as meaningful (it round-trips as an ordinary ignored POST field if `augmentRequest` ever failed to strip it, which is harmless but not the intent — it should always be deleted before the request is sent).
- **`lastFocus` is read instead of `document.activeElement` in `augmentRequest`.** This is deliberate, not an oversight: by the time a click's `htmx:configRequest` fires, the browser has already moved focus to the clicked button, so `activeElement` would report the button, not whatever field was actually being edited a moment before. `focusin` tracking is what makes the cross-row case (spec's `TestFocusAndTargetCanDiffer`) work from a real mouse click, not just from a raw test POST.
- **The empty-outline's bootstrap field is deliberately excluded from every keyboard binding**, via the `row.hasAttribute("data-id")` guard repeated in `handleKeydown`, `trackFocus`, and `maybeDeleteEmptyBullet`'s callers. It shares the `outline-title` class for styling only (an N2 decision, see `TestOutlineRendersAnotherUsersTreeNowhere`'s comment) — it has no real node, and no `data-id`, until the first bullet actually exists.
- **A genuine, accepted gap:** zooming or navigating breadcrumbs (both plain `<a href>` links) discards any unsaved text past the last 600ms debounce or the last blur, exactly as clicking away to another browser tab would. Do not "fix" this by turning the breadcrumb links or the bullet dot into `hx-post` forms that also save text first — that is out of scope for this chunk and adds a structural-request shape nothing else uses. This gap closes on its own once every text input's `blur changed` trigger has fired, which it always has by the time a link's own click event is actually processed.

## Follow-ups to file as issues

1. `internal/apps/paste` and `internal/apps/notes` still each define their own `newServer`/`session`/`do`/`get`/`post` test harness (noted in N2's plan as a pending follow-up; N3 doesn't touch it, and the harness grew again here with `postHX`). A shared `internal/apptest` package would deduplicate both.
2. `POST /notes/{id}/text` still ignores `focus_id` (also flagged in N2). N3's debounced autosave never sends a `focus_id` that differs from the path `{id}` either, so this remains dormant; revisit once a chunk gives the field an actual second purpose.
3. `notes.js`'s Up/Down (caret between bullets) and the note-field variant of Enter are this plan's own scope calls, not spec-mandated behaviour: Up/Down always targets the **title** input of the neighbouring row (never the note), and Enter in a note field appends an empty sibling rather than splitting note text. Worth a design pass once real usage surfaces a preference either way.
