# ON Notes N8 (Export and Import) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add portability to ON Notes — spec §14 of
[docs/superpowers/specs/2026-08-25-on-notes-design.md](../specs/2026-08-25-on-notes-design.md).
`GET /notes/export` downloads the whole tree (or a subtree) as Markdown;
`POST /notes/import` parses that same format back in; `app.Exporter` gives
the suite's whole-account JSON backup a lossless copy; pasting an
outline-shaped block into a bullet reaches the same parser from the editor.

**Architecture:** A new, unfiltered recursive read (`Store.Export`) supplies
both output formats — Markdown (human, lossy: only title/note/done/due
round-trip) and JSON (machine, lossless: every column). A pure parser
(`ParseMarkdown`) turns text back into a flat, depth-tagged slice with no
database access of its own; a small tree-building primitive
(`Ops.ImportUnder`) is the only thing that turns that slice into real rows,
in one transaction. The file-upload route and the paste route both reduce
to "parse, then `ImportUnder`". Getting there also requires one small fix
to the platform's shared CSRF middleware (Task 1) — see the note below.

**Tech Stack:** Go, `database/sql` against `modernc.org/sqlite`,
`regexp` (RE2, no backtracking — same reasoning as `markdown.go`'s inline
renderer), HTMX, `multipart/form-data` for the file upload. No new
dependencies.

## Global Constraints

- No schema change. N8 adds no column and needs no migration — everything
  it touches already exists.
- `Store`/`Ops` have no HTTP knowledge — every method here takes plain Go
  values, never `*http.Request`.
- `web.DefaultMaxBodyBytes` (1 MiB) caps every request body, applied once
  in `web.Stack` before CSRF or any app handler runs
  ([internal/platform/web/middleware.go](../../../internal/platform/web/middleware.go)).
  There is no per-route override today, and this plan does not add one —
  see Task 1's note on why an app-level byte bound still matters on top of
  that ceiling.
- **`CSRF.Middleware` already reads the request body before any handler
  does**, for every unsafe-method request: it calls `r.ParseForm()` (or,
  after Task 1, `r.ParseMultipartForm`) itself, looking for the token, and
  Go's `*http.Request` caches the result — a handler's own later
  `ParseForm`/`ParseMultipartForm`/`PostFormValue` call reads that cache
  rather than re-reading the wire. Every handler in this plan is written
  with that in mind: none of them wrap `r.Body` in their own
  `http.MaxBytesReader` (it would run after the body is already
  consumed and so would never trigger), and each enforces its own
  byte bound by checking the length of what it already has, after the
  fact, not by capping the read.
- Apps never import each other's packages ([internal/arch/arch_test.go](../../../internal/arch/arch_test.go)),
  so this plan's constants are defined independently in the `notes`
  package even where the reasoning mirrors `paste`'s own `MaxBodyBytes`.
- Spec §7's one-write rule: a structural POST that can be issued from a row
  mid-edit must save that row's title and note and perform its own
  operation in one transaction. Pasting into a bullet is such an action
  (Task 6, via `mutate`); uploading a file from the toolbar is not (Task 5
  — there is no outline row being edited when you click Import, the same
  situation `prefs`/`restore` are already in).
- Spec §14: the Markdown format round-trips only title, note, done and due
  — not collapsed state, not archived state, not timestamps. This is
  stated in the spec itself ("Markdown is lossy by design; JSON is the
  safety net"), not a gap this plan needs to close.
- Decisions locked in for this chunk (spec leaves them as judgment calls;
  each was confirmed with the user before writing this plan):
  1. A paste into a bullet's title or note field is intercepted **only**
     when the clipboard text is already shaped like this app's own export
     format (has at least one `- `-prefixed, evenly-indented line).
     Anything else is left to the browser's own default paste behaviour —
     there is no fallback path that reshapes ordinary multi-line text into
     siblings.
  2. Import lands as new children of wherever the outline is currently
     zoomed (`root`, the same hidden field every other structural form
     already carries) — no separate parent-picker UI.
  3. `MaxImportNodes = 5000`. `MaxImportFileBytes` (768 KiB) and
     `MaxPasteTextBytes` (256 KiB) size each transport's own bound to sit
     safely under the platform's 1 MiB ceiling, accounting for that
     transport's own overhead — not a flat "2 MB", which was the
     originally proposed number before the platform's global cap was
     discovered during this plan's own research.
  4. The CSRF multipart fix (Task 1) lands as this branch's first task,
     rather than as its own separate plan/PR, because the fix is small
     (about a dozen lines plus tests) and reviewing it alongside the one
     feature that needs it is straightforward.

---

## File Structure

- `internal/platform/web/csrf.go` — modify (Task 1). `verify` reads the
  token from a multipart body, not only a header or a urlencoded field.
- `internal/platform/web/csrf_test.go` — modify (Task 1).
- `internal/apps/notes/notes.go` — modify. Three new bounds:
  `MaxImportNodes`, `MaxImportFileBytes`, `MaxPasteTextBytes` (Task 2).
- `internal/apps/notes/export.go` — new. `Store.Export` (Task 2); JSON
  `Exporter` — `exportedNode`, `exportPayload`, `App.Export` (Task 2);
  `ExportMarkdown` (Task 3).
- `internal/apps/notes/import.go` — new. `ParsedNode`, `ParseMarkdown`
  (Task 4); `Ops.ImportUnder`/`Store.ImportUnder` (Task 5).
- `internal/apps/notes/handlers.go` — modify. `export` (Task 3);
  `importNotes` (Task 5); `paste` (Task 6).
- `internal/apps/notes/app.go` — modify. `var _ app.Exporter`; three
  routes; the route-map comment.
- `internal/apps/notes/templates/outline.html` — modify. An Export link
  and an Import form in the toolbar (Tasks 3 and 5).
- `internal/apps/notes/static/notes.js` — modify. Paste interception
  (Task 6).
- `internal/ui/static/app.css` — modify. `.notes-import` (Task 5).
- `internal/apps/notes/export_test.go` — new (Tasks 2, 3).
- `internal/apps/notes/import_test.go` — new (Tasks 4, 5).
- `internal/apps/notes/handlers_test.go` — modify (Tasks 3, 5, 6).
- `README.md`, `internal/apps/notes/notes.go` (package doc) — modify
  (Task 7).

---

### Task 1: Teach `web.CSRF` to read a multipart form field

**Files:**
- Modify: `internal/platform/web/csrf.go`
- Modify: `internal/platform/web/csrf_test.go`

**Interfaces:**
- Produces: `verify`'s existing behaviour, extended — no exported API
  changes. Every later task's file-upload and paste routes depend on this
  landing first: without it, a plain (JavaScript-off) submission of
  either form is rejected with a 403 before the app's own handler ever
  runs.

**Why this exists.** `CSRF.verify` currently falls back to
`r.ParseForm()` when there is no `X-CSRF-Token` header. `ParseForm` only
reads the request body for `application/x-www-form-urlencoded` — for any
other content type, including `multipart/form-data`, it leaves the body
untouched but still sets `r.PostForm` to a non-nil, *empty* map. Because
`r.PostFormValue` only tries `ParseMultipartForm` itself when
`r.PostForm == nil`, that empty-but-non-nil map short-circuits it: the
call returns `""` for every field, no matter what the multipart body
actually contains. A real HTML `<form enctype="multipart/form-data">`,
submitted with JavaScript off, carries its CSRF token only as a hidden
field inside that body — there is no way for a plain browser form
submission to send a custom header — so today, every such submission
fails CSRF verification before reaching any handler. No route in this
codebase has needed a file upload before N8, which is why this has not
surfaced until now.

- [ ] **Step 1: Write the failing tests**

Read `internal/platform/web/csrf_test.go` first — confirm the exact
current text of `TestCSRFAcceptsTheTokenInAFormField` and
`TestCSRFFormParsingDoesNotConsumeTheBody` before adding the multipart
counterparts below, so the new tests sit next to their obvious siblings
and use exactly the same `csrfStack`/`cookieFrom`/`testErrors` helpers
those already use.

Append to `internal/platform/web/csrf_test.go`:

```go
func TestCSRFAcceptsTheTokenInAMultipartFormField(t *testing.T) {
	h, _ := csrfStack(t, false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	token := cookieFrom(t, rec, web.CSRFCookieName).Value

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField(web.CSRFFormField, token); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: token})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (multipart form token)", rec.Code)
	}
}

func TestCSRFRejectsAMissingMultipartToken(t *testing.T) {
	h, _ := csrfStack(t, false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	token := cookieFrom(t, rec, web.CSRFCookieName).Value

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("title", "no token in this body"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: token})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (no token anywhere in the multipart body)", rec.Code)
	}
}

// TestCSRFMultipartParsingLeavesTheHandlersOwnFieldsReadable mirrors
// TestCSRFFormParsingDoesNotConsumeTheBody for multipart: the handler
// must still see its own form fields and its uploaded file after the
// middleware has already parsed the body once, looking for the token.
func TestCSRFMultipartParsingLeavesTheHandlersOwnFieldsReadable(t *testing.T) {
	e, _ := testErrors(t)
	c := web.NewCSRF(false, e)
	h := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer func() { _ = file.Close() }()
		data, _ := io.ReadAll(file)
		_, _ = w.Write([]byte("title=" + r.FormValue("title") + " file=" + string(data)))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	token := cookieFrom(t, rec, web.CSRFCookieName).Value

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField(web.CSRFFormField, token); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("title", "a title"); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("file", "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("- bullet")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: token})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Body.String(); got != "title=a title file=- bullet" {
		t.Errorf("handler saw %q; the middleware did not leave its fields readable", got)
	}
}
```

Add `"bytes"`, `"io"`, and `"mime/multipart"` to the file's existing
import block (it already has `"net/http"`, `"net/http/httptest"`,
`"net/url"`, `"strings"`, `"testing"`, and the `web` package):

```go
import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/platform/web/... -run TestCSRF -v`
Expected: `TestCSRFAcceptsTheTokenInAMultipartFormField` and
`TestCSRFMultipartParsingLeavesTheHandlersOwnFieldsReadable` FAIL (status
403 instead of 200, or the handler never runs) —
`TestCSRFRejectsAMissingMultipartToken` already PASSES today, since a 403
is exactly what the *current* (buggy) behaviour also produces for any
multipart body; it is here to make sure Task 1's fix does not accidentally
start accepting an unauthenticated multipart POST once it starts reading
multipart bodies at all.

- [ ] **Step 3: Implement the fix**

In `internal/platform/web/csrf.go`, `verify` currently reads:

```go
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
```

Change it to:

```go
// verify compares the token the browser sent deliberately with the one it
// sent automatically.
func (c *CSRF) verify(r *http.Request, cookieToken string) bool {
	if cookieToken == "" {
		return false
	}

	sent := r.Header.Get(CSRFHeader)
	if sent == "" {
		sent = formToken(r)
	}
	if sent == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(sent), []byte(cookieToken)) == 1
}

// formToken reads the CSRF field from the request body, for the one kind
// of request that cannot carry the token as a header: a plain HTML form
// submitted with JavaScript off.
//
// ParseForm only reads the body for application/x-www-form-urlencoded,
// leaving any other content type's body untouched — a route whose form
// also uploads a file needs ParseMultipartForm instead (ON Notes' import
// route, N8, is the first one in this codebase to need it). Anything else
// is left alone by both calls, exactly as ParseForm alone already left it
// before this function existed, so a handler expecting some other body
// shape can still read it itself.
//
// Both calls cache into r.Form/r.PostForm (and, for multipart,
// r.MultipartForm), so the handler that runs afterwards reads that same
// cache rather than triggering a second read of the body.
func formToken(r *http.Request) string {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct == "multipart/form-data" {
		if err := r.ParseMultipartForm(DefaultMaxBodyBytes); err != nil {
			return ""
		}
		return r.PostFormValue(CSRFFormField)
	}
	if err := r.ParseForm(); err != nil {
		return ""
	}
	return r.PostFormValue(CSRFFormField)
}
```

Add `"mime"` to `csrf.go`'s import block:

```go
import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
)
```

`DefaultMaxBodyBytes` is already declared further down in this same file
(`const DefaultMaxBodyBytes = 1 << 20`) — passed here as
`ParseMultipartForm`'s memory threshold only, not as a second body-size
cap: the real cap is `LimitBody(DefaultMaxBodyBytes)`, already applied
earlier in `Stack`, which is what actually bounds how many bytes
`ParseMultipartForm` can ever read in the first place.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/platform/web/... -v`
Expected: PASS, the whole package.

- [ ] **Step 5: Run the whole suite once, since this is a shared file**

Run: `go build ./... && go vet ./... && go test ./... -race`
Expected: PASS — `csrf.go` is imported by every app, not only `notes`, so
this is the one step in this plan worth running against the whole repo
rather than just `internal/apps/notes/...`.

- [ ] **Step 6: Commit**

```bash
git add internal/platform/web/csrf.go internal/platform/web/csrf_test.go
git commit -m "fix(web): accept a CSRF token from a multipart form body"
```

---

### Task 2: `Store.Export` and the JSON `Exporter`

**Files:**
- Modify: `internal/apps/notes/notes.go`
- Create: `internal/apps/notes/export.go`
- Create: `internal/apps/notes/export_test.go`
- Modify: `internal/apps/notes/app.go`

**Interfaces:**
- Produces: `(*Store).Export(ctx, userID, rootID int64) ([]Node, error)` —
  every node in `rootID`'s subtree, pre-order, `Depth` relative to
  `rootID`, unfiltered by collapse or archive state. `MaxImportNodes`,
  `MaxImportFileBytes`, `MaxPasteTextBytes`. `app.Exporter` compliance for
  `*App`.
- Consumes: `nodeColumns`/`childColumns`/`scanNode`/`parentArg`/`MaxDepth`
  (`store.go`, `notes.go`) — unchanged by this task.

- [ ] **Step 1: Add the three bounds**

Read `internal/apps/notes/notes.go` first to confirm the exact current end
of its `const` block (it ends with `RootID = 0` followed by `)`, but
confirm this against the real file rather than trusting this plan, since
N7's own history shows this file accumulates small edits between when a
plan is written and when it runs). Add these three constants inside that
same block, after `RootID`:

```go
	// MaxImportNodes bounds how many bullets a single import or a pasted
	// block may create — spec §14: rejected with a clear error rather
	// than truncated silently.
	MaxImportNodes = 5000
	// MaxImportFileBytes bounds an uploaded Markdown file (POST
	// /notes/import) — spec §14. web.DefaultMaxBodyBytes already caps
	// every request body at 1 MiB before this app's handler ever runs
	// (internal/platform/web/middleware.go); a multipart file part
	// carries almost no encoding overhead of its own, so 768 KiB of file
	// content leaves comfortable headroom under that ceiling for the
	// multipart boundaries and the request's other fields.
	MaxImportFileBytes = 768 << 10
	// MaxPasteTextBytes bounds a pasted block posted as a plain form
	// field (POST /notes/{id}/paste) — spec §14, the same bound applied
	// to the other way this app's Markdown parser is reached. Unlike a
	// file upload this field is application/x-www-form-urlencoded, which
	// percent-encodes every non-ASCII UTF-8 byte as "%XX" — up to 3x
	// expansion for Cyrillic, Hebrew or CJK text (see
	// internal/apps/paste/store.go's MaxBodyBytes for the same reasoning
	// applied to a snippet body — apps do not import each other, so this
	// is an independent constant with the same justification, not a
	// shared symbol). 256 KiB keeps the worst case (768 KiB encoded)
	// safely under the platform's 1 MiB limit, with room left for the
	// request's other fields.
	MaxPasteTextBytes = 256 << 10
```

- [ ] **Step 2: Write the failing tests for `Store.Export`**

Create `internal/apps/notes/export_test.go`:

```go
package notes_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

func TestExportReturnsTheWholeTreeInPreOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent")
	child := f.mk(t, parent.ID, "child")
	sibling := f.mk(t, notes.RootID, "sibling")

	got, err := f.store.Export(ctx, f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("Export = %+v, want 3 nodes", got)
	}
	if got[0].ID != parent.ID || got[0].Depth != 0 {
		t.Errorf("row 0 = %+v, want parent at depth 0", got[0])
	}
	if got[1].ID != child.ID || got[1].Depth != 1 {
		t.Errorf("row 1 = %+v, want child at depth 1", got[1])
	}
	if got[2].ID != sibling.ID || got[2].Depth != 0 {
		t.Errorf("row 2 = %+v, want sibling at depth 0", got[2])
	}
}

// TestExportIncludesCollapsedDoneAndArchivedNodes: unlike Outline
// (store.go), an export is a full data dump, not a display view — spec
// §14 gives Markdown export no filtering rule at all, unlike the outline,
// search and due list, each of which spec explicitly narrows.
func TestExportIncludesCollapsedDoneAndArchivedNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	collapsed := f.mk(t, notes.RootID, "collapsed")
	underCollapsed := f.mk(t, collapsed.ID, "under collapsed")
	done := f.mk(t, notes.RootID, "done")
	archived := f.mk(t, notes.RootID, "archived")

	if err := f.store.SetCollapsed(ctx, f.alice.ID, collapsed.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetDone(ctx, f.alice.ID, done.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetArchived(ctx, f.alice.ID, archived.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Export(ctx, f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, n := range got {
		ids = append(ids, n.ID)
	}
	for _, want := range []int64{collapsed.ID, underCollapsed.ID, done.ID, archived.ID} {
		found := false
		for _, id := range ids {
			if id == want {
				found = true
			}
		}
		if !found {
			t.Errorf("Export = %v, missing %d", ids, want)
		}
	}
}

func TestExportOfASubtreeExcludesTheRootItself(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	root := f.mk(t, notes.RootID, "root")
	child := f.mk(t, root.ID, "child")
	f.mk(t, notes.RootID, "unrelated")

	got, err := f.store.Export(ctx, f.alice.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != child.ID || got[0].Depth != 0 {
		t.Fatalf("Export(root) = %+v, want only the child at depth 0", got)
	}
}

func TestExportDoesNotLeakAnotherUsersNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "alice's")
	f.mkFor(t, f.bob.ID, notes.RootID, "bob's")

	got, err := f.store.Export(ctx, f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "alice's" {
		t.Fatalf("Export = %+v, want only alice's own node", got)
	}
}

func TestNotesImplementsTheExporterInterface(t *testing.T) {
	// A compile-time assertion would be enough (app.go carries one), but a
	// failing test here names the problem more clearly than a type error
	// in an unrelated file — mirrors internal/apps/paste/export_test.go.
	var a any = notes.New()
	if _, ok := a.(app.Exporter); !ok {
		t.Fatal("*notes.App does not implement app.Exporter")
	}
}

func TestJSONExportContainsEveryColumn(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "task")
	if err := f.store.SetDone(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetDue(ctx, f.alice.ID, n.ID, "2026-09-01"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCollapsed(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}
	f.mkFor(t, f.bob.ID, notes.RootID, "bob's")

	payload, err := notes.New().Export(ctx, f.db, f.alice.ID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("the payload does not marshal: %v", err)
	}
	text := string(encoded)

	for _, want := range []string{`"task"`, `"done":true`, `"due_on":"2026-09-01"`, `"collapsed":true`} {
		if !strings.Contains(text, want) {
			t.Errorf("export is missing %s: %s", want, text)
		}
	}
	if strings.Contains(text, "bob's") {
		t.Error("the export contains another user's node")
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/... -run 'TestExport|TestJSONExport|TestNotesImplementsTheExporterInterface' -v`
Expected: FAIL — `f.store.Export`, `notes.New().Export` and `app.Exporter`
compliance do not exist yet.

- [ ] **Step 4: Implement `Store.Export`**

Create `internal/apps/notes/export.go`:

```go
package notes

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Export returns every node in rootID's subtree, in pre-order with Depth
// relative to rootID — spec §14: an export is a full data dump, not a
// display view, so unlike Outline (store.go) it does not stop at a
// collapsed node, does not exclude an archived one, and does not honour
// the show-completed preference. rootID may be RootID for the whole tree.
// It is empty both for a root with nothing under it and for a root that
// does not exist or is not userID's: a caller that needs to tell those
// apart calls ByID.
//
// The recursive descent's owner-matching mirrors Outline's own, for the
// same reason given there: parent_id is a plain foreign key, not a
// composite (parent_id, user_id) one, so matching on user_id at every step
// is what keeps a broken invariant I2 from leaking another household's
// bullets into someone's export.
func (st *Store) Export(ctx context.Context, userID, rootID int64) ([]Node, error) {
	rows, err := st.db.QueryContext(ctx,
		`WITH RECURSIVE tree AS (
		     SELECT `+nodeColumns+`, 0 AS depth, printf('%08d', position) AS path
		       FROM notes_nodes
		      WHERE user_id = ? AND parent_id IS ?
		   UNION ALL
		     SELECT `+childColumns+`,
		            t.depth + 1, t.path || '/' || printf('%08d', c.position)
		       FROM notes_nodes c JOIN tree t ON c.parent_id = t.id
		      WHERE c.user_id = t.user_id AND t.depth + 1 <= ?
		 )
		 SELECT `+nodeColumns+`, depth FROM tree ORDER BY path`,
		userID, parentArg(rootID), MaxDepth)
	if err != nil {
		return nil, fmt.Errorf("notes: export: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Node
	for rows.Next() {
		var depth int
		n, err := scanNode(rows, &depth)
		if err != nil {
			return nil, err
		}
		n.Depth = depth
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notes: export: %w", err)
	}
	return out, nil
}

// exportedNode is the JSON on-disk shape of one node — the whole row,
// unlike the Markdown format (ExportMarkdown, Task 3), which round-trips
// only title, note, done and due — spec §14: "JSON is the safety net."
// ParentID is RootID (0) for a top-level node, Node's own convention,
// rather than a JSON null — there is no re-import path for this format
// (unlike paste's own JSON export, this one exists solely for onsuite
// export's whole-account backup), so nothing needs to parse it back.
type exportedNode struct {
	ID        int64     `json:"id"`
	ParentID  int64     `json:"parent_id"`
	Position  int       `json:"position"`
	Title     string    `json:"title"`
	Note      string    `json:"note"`
	Collapsed bool      `json:"collapsed"`
	Done      bool      `json:"done"`
	DueOn     string    `json:"due_on,omitempty"`
	Archived  bool      `json:"archived"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type exportPayload struct {
	Nodes []exportedNode `json:"nodes"`
}

// Export implements app.Exporter, joining ON Notes to onsuite export's
// whole-account JSON backup (spec §14) — distinct from GET /notes/export's
// Markdown download (Task 3), which is a different format for a different
// purpose. It takes the database directly, like every Exporter, so backup
// works from the command line without building the HTTP stack.
func (a *App) Export(ctx context.Context, handle *sql.DB, userID int64) (any, error) {
	nodes, err := NewStore(handle).Export(ctx, userID, RootID)
	if err != nil {
		return nil, err
	}
	out := exportPayload{Nodes: make([]exportedNode, 0, len(nodes))}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, exportedNode{
			ID: n.ID, ParentID: n.ParentID, Position: n.Position,
			Title: n.Title, Note: n.Note, Collapsed: n.Collapsed,
			Done: n.Done, DueOn: n.DueOn, Archived: n.Archived,
			CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
		})
	}
	return out, nil
}
```

- [ ] **Step 5: Register the compile-time assertion**

In `internal/apps/notes/app.go`, the existing assertion currently reads:

```go
// The compile-time assertion is here rather than left implicit because the
// registry takes an interface: a method with the wrong signature would
// otherwise fail at the call site in main, several packages away from the
// mistake.
var _ app.App = (*App)(nil)
```

Change it to:

```go
// The compile-time assertions are here rather than left implicit because
// the registry takes interfaces: a method with the wrong signature would
// otherwise fail at the call site in main or in Registry.Export, several
// packages away from the mistake.
var (
	_ app.App      = (*App)(nil)
	_ app.Exporter = (*App)(nil)
)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, the whole package.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/notes/notes.go internal/apps/notes/export.go \
        internal/apps/notes/export_test.go internal/apps/notes/app.go
git commit -m "feat(notes): add Store.Export and the JSON Exporter"
```

---

### Task 3: `ExportMarkdown` and `GET /notes/export`

**Files:**
- Modify: `internal/apps/notes/export.go`
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/app.go`
- Modify: `internal/apps/notes/templates/outline.html`
- Test: `internal/apps/notes/export_test.go`
- Test: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `Store.Export` (Task 2).
- Produces: `ExportMarkdown(flat []Node) string`; `GET /notes/export`
  (query param `root`, default `RootID`). Task 4's `ParseMarkdown` is the
  inverse of this function and must agree with it on suffix order (see
  that task's design note).

- [ ] **Step 1: Write the failing tests for `ExportMarkdown`**

Append to `internal/apps/notes/export_test.go`:

```go
func TestExportMarkdownRendersOneBulletPerLine(t *testing.T) {
	flat := []notes.Node{
		{ID: 1, Title: "Software projects", Note: "Various software projects", Depth: 0},
		{ID: 2, Title: "AtBudget", Depth: 1},
		{ID: 3, Title: "Project objectives", Done: true, Depth: 2},
		{ID: 4, Title: "API", DueOn: "2026-09-01", Depth: 2},
	}
	want := "- Software projects\n" +
		"  Various software projects\n" +
		"  - AtBudget\n" +
		"    - Project objectives [x]\n" +
		"    - API @2026-09-01\n"
	if got := notes.ExportMarkdown(flat); got != want {
		t.Errorf("ExportMarkdown =\n%q\nwant\n%q", got, want)
	}
}

func TestExportMarkdownCombinesDoneAndDueOnOneLine(t *testing.T) {
	flat := []notes.Node{{ID: 1, Title: "both", Done: true, DueOn: "2026-09-01", Depth: 0}}
	want := "- both [x] @2026-09-01\n"
	if got := notes.ExportMarkdown(flat); got != want {
		t.Errorf("ExportMarkdown = %q, want %q", got, want)
	}
}

func TestExportMarkdownWritesMultiLineNotesAsConsecutiveIndentedLines(t *testing.T) {
	flat := []notes.Node{{ID: 1, Title: "task", Note: "line one\nline two", Depth: 0}}
	want := "- task\n  line one\n  line two\n"
	if got := notes.ExportMarkdown(flat); got != want {
		t.Errorf("ExportMarkdown = %q, want %q", got, want)
	}
}

func TestExportMarkdownOfNothingIsEmpty(t *testing.T) {
	if got := notes.ExportMarkdown(nil); got != "" {
		t.Errorf("ExportMarkdown(nil) = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/... -run TestExportMarkdown -v`
Expected: FAIL — `notes.ExportMarkdown` does not exist yet.

- [ ] **Step 3: Implement `ExportMarkdown`**

In `internal/apps/notes/export.go`, add `"strings"` to the existing import
block (`"context"`, `"database/sql"`, `"fmt"`, `"time"`), then append:

```go
// ExportMarkdown renders flat (pre-order, depth-tagged) nodes as spec
// §14's Markdown outline format: a "- " line per node, two spaces of
// indent per level, an optional "[x]" suffix for done, a trailing
// "@YYYY-MM-DD" for a due date, and the note as unbulleted lines indented
// one level deeper. It is Store.Export's own output shape, so a whole-tree
// or a subtree export both feed it directly. ParseMarkdown (import.go) is
// its exact inverse — see that function's own doc comment for why suffix
// order must be stripped in the reverse of the order this appends them.
func ExportMarkdown(flat []Node) string {
	var b strings.Builder
	for _, n := range flat {
		indent := strings.Repeat("  ", n.Depth)
		b.WriteString(indent)
		b.WriteString("- ")
		b.WriteString(n.Title)
		if n.Done {
			b.WriteString(" [x]")
		}
		if n.DueOn != "" {
			b.WriteString(" @")
			b.WriteString(n.DueOn)
		}
		b.WriteString("\n")
		if n.Note != "" {
			for _, line := range strings.Split(n.Note, "\n") {
				b.WriteString(indent)
				b.WriteString("  ")
				b.WriteString(line)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -run TestExportMarkdown -v`
Expected: PASS.

- [ ] **Step 5: Write the failing handler tests**

Read `internal/apps/notes/handlers_test.go` first to confirm the exact
current text of its `get`/`post`/`submit` helpers and the toolbar block in
`outline.html`, then append to `handlers_test.go`:

```go
// get2 fetches a path without asserting on the status, for a test that
// needs to inspect the raw response — headers, a non-200 status, or a
// body that is not HTML. get is for everything else.
func (s *server) get2(t *testing.T, sess *session, path string) *httptest.ResponseRecorder {
	t.Helper()
	return s.do(t, sess, httptest.NewRequest("GET", path, nil))
}

func TestExportDownloadsTheWholeTree(t *testing.T) {
	s := newServer(t)
	s.seed(t, s.alice, notes.RootID, "top level bullet")

	rec := s.get2(t, s.alice, "/notes/export")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/markdown") {
		t.Errorf("Content-Type = %q, want text/markdown", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") {
		t.Errorf("Content-Disposition = %q, want an attachment", got)
	}
	if !strings.Contains(rec.Body.String(), "- top level bullet\n") {
		t.Errorf("export body = %q, missing the bullet", rec.Body.String())
	}
}

func TestExportOfASubtree(t *testing.T) {
	s := newServer(t)
	root := s.seed(t, s.alice, notes.RootID, "root")
	s.seed(t, s.alice, root, "child")
	s.seed(t, s.alice, notes.RootID, "unrelated")

	rec := s.get2(t, s.alice, "/notes/export?root="+itoa(root))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "- child\n") {
		t.Errorf("export body = %q, missing the child", body)
	}
	if strings.Contains(body, "root") || strings.Contains(body, "unrelated") {
		t.Errorf("export body = %q, a subtree export must not include the root or unrelated bullets", body)
	}
}

func TestExportOnAnotherUsersRootIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.bob, notes.RootID, "bob's")

	rec := s.get2(t, s.alice, "/notes/export?root="+itoa(id))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestExportRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.do(t, nil, httptest.NewRequest("GET", "/notes/export", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET /notes/export anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

// TestOutlineToolbarHasAnExportLink looks the link up by its text rather
// than by a compound selector: htmlassert matches only one qualifier per
// selector part (see its own doc comment), so a combined
// tag+class+attribute-prefix selector is not expressible here.
func TestOutlineToolbarHasAnExportLink(t *testing.T) {
	s := newServer(t)
	doc := s.get(t, s.alice, "/notes/")

	var link *html.Node
	for _, a := range doc.QueryAll("a.toolbar-btn-nav") {
		if htmlassert.Text(a) == "Export" {
			link = a
		}
	}
	if link == nil {
		t.Fatal("no toolbar-btn-nav link reads \"Export\"")
	}
	if got, _ := htmlassert.Attr(link, "href"); got != "/notes/export?root=0" {
		t.Errorf("export link href = %q, want /notes/export?root=0", got)
	}
}
```

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/... -run 'TestExport|TestOutlineToolbarHasAnExportLink' -v`
Expected: FAIL — `a.export`, `GET /notes/export`, and the toolbar link do
not exist yet.

- [ ] **Step 7: Implement the handler**

In `internal/apps/notes/handlers.go`, append after `search` (the file's
current final function, ending `a.render(w, r, http.StatusOK, "notes/search", page)`
followed by `}`):

```go
// export downloads userID's whole tree, or one subtree, as spec §14's
// Markdown outline format.
func (a *App) export(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	rootID, ok := exportRootFrom(r)
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	if rootID != RootID {
		if _, err := a.store.ByID(r.Context(), userID, rootID); err != nil {
			a.fail(w, r, err)
			return
		}
	}

	flat, err := a.store.Export(r.Context(), userID, rootID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="notes-export.md"`)
	_, _ = w.Write([]byte(ExportMarkdown(flat)))
}

// exportRootFrom parses export's ?root= query parameter: RootID (the
// whole tree) when absent, exactly like formID's "absent means none" rule
// for a hidden form field, adapted to a query string instead of a POST
// body.
func exportRootFrom(r *http.Request) (int64, bool) {
	raw := r.URL.Query().Get("root")
	if raw == "" {
		return RootID, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
```

- [ ] **Step 8: Register the route**

In `internal/apps/notes/app.go`, the route-map comment currently has:

```go
//	GET  /notes/archive      likewise a literal segment; N7's archive list
//	POST /notes/new          a literal segment, and literals outrank {id}
```

Change it to:

```go
//	GET  /notes/archive      likewise a literal segment; N7's archive list
//	GET  /notes/export       likewise a literal segment; N8's Markdown download
//	POST /notes/new          a literal segment, and literals outrank {id}
```

`Mount` currently has:

```go
	r.HandleFunc("GET /archive", a.archiveList)
	r.HandleFunc("POST /new", a.create)
```

Change it to:

```go
	r.HandleFunc("GET /archive", a.archiveList)
	r.HandleFunc("GET /export", a.export)
	r.HandleFunc("POST /new", a.create)
```

- [ ] **Step 9: Add the toolbar link**

Read `internal/apps/notes/templates/outline.html` first to confirm the
current exact text of the `.notes-toolbar-actions` block (it currently
lists Due and Archive links before the prefs form). Add an Export link
right after Archive:

```html
			<div class="notes-toolbar-actions">
				<a href="/notes/due" class="toolbar-btn toolbar-btn-nav">Due</a>
				<a href="/notes/archive" class="toolbar-btn toolbar-btn-nav">Archive</a>
				<a href="/notes/export?root={{.Data.Root.ID}}" class="toolbar-btn toolbar-btn-nav">Export</a>
				<form method="post" action="/notes/prefs"
```

(everything from `<form method="post" action="/notes/prefs"` onward is
unchanged — only the new `<a>` line is inserted before it.)

A plain link, not an HTMX one: htmx only intercepts elements carrying its
own `hx-*` attributes, so this is an ordinary browser navigation that
downloads the file via `Content-Disposition: attachment` — exactly what a
download needs, and it works identically with JavaScript off.

- [ ] **Step 10: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, the whole package.

- [ ] **Step 11: Commit**

```bash
git add internal/apps/notes/export.go internal/apps/notes/handlers.go \
        internal/apps/notes/app.go internal/apps/notes/templates/outline.html \
        internal/apps/notes/export_test.go internal/apps/notes/handlers_test.go
git commit -m "feat(notes): add ExportMarkdown and GET /notes/export"
```

---

### Task 4: `ParseMarkdown`

**Files:**
- Create: `internal/apps/notes/import.go`
- Create: `internal/apps/notes/import_test.go`

**Interfaces:**
- Consumes: `MaxImportNodes`, `ErrInvalid` (Task 2, `notes.go`).
- Produces: `ParsedNode{Depth int; Title, Note string; Done bool; DueOn string}`;
  `ParseMarkdown(text string) ([]ParsedNode, error)`. Task 5's
  `Ops.ImportUnder` consumes `[]ParsedNode` directly; Task 6's paste route
  calls `ParseMarkdown` the same way Task 5's import route does.

This task does no I/O and touches no route — it is pure text in, a slice
or an error out — so it is the cheapest place to get thorough coverage
before anything downstream depends on it.

**Design note — parsing is the exact inverse of `ExportMarkdown`, in
reverse order.** `ExportMarkdown` (Task 3) appends a trailing `[x]` before
a trailing `@YYYY-MM-DD` (`title + " [x]" + " @date"`), so a line carrying
both must have `@YYYY-MM-DD` stripped **first** — it is the true suffix —
before `" [x]"` is checked against what remains. Getting this order
backwards would leave `[x]` embedded in the middle of a due date check
that can never match. Every test in Step 1 that combines both suffixes on
one line exists to pin this order.

**Design note — malformed input is an error, not a best-effort guess.**
The input here is an upload or a paste, not the trusted database, so
`ParseMarkdown` never does what `nest` (view.go) does for a display row
whose depth skips a level — silently drop it and move on. Silently
importing less than the file actually contained, with nothing to say so,
is worse than refusing the whole file with a clear line number.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/notes/import_test.go`:

```go
package notes_test

import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

func TestParseMarkdownParsesTheSpecExample(t *testing.T) {
	text := "- Software projects\n" +
		"  Various software development projects that I'm involved in\n" +
		"  - AtBudget\n" +
		"    - Project objectives [x]\n" +
		"    - API @2026-09-01\n"

	got, err := notes.ParseMarkdown(text)
	if err != nil {
		t.Fatalf("ParseMarkdown: %v", err)
	}
	want := []notes.ParsedNode{
		{Depth: 0, Title: "Software projects", Note: "Various software development projects that I'm involved in"},
		{Depth: 1, Title: "AtBudget"},
		{Depth: 2, Title: "Project objectives", Done: true},
		{Depth: 2, Title: "API", DueOn: "2026-09-01"},
	}
	if len(got) != len(want) {
		t.Fatalf("ParseMarkdown returned %d nodes, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("node %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseMarkdownCombinesDoneAndDueOnOneLine(t *testing.T) {
	got, err := notes.ParseMarkdown("- both [x] @2026-09-01\n")
	if err != nil {
		t.Fatal(err)
	}
	want := notes.ParsedNode{Depth: 0, Title: "both", Done: true, DueOn: "2026-09-01"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ParseMarkdown = %+v, want [%+v]", got, want)
	}
}

func TestParseMarkdownJoinsConsecutiveNoteLines(t *testing.T) {
	got, err := notes.ParseMarkdown("- task\n  line one\n  line two\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Note != "line one\nline two" {
		t.Fatalf("ParseMarkdown = %+v, want a two-line note", got)
	}
}

func TestParseMarkdownSkipsBlankLines(t *testing.T) {
	got, err := notes.ParseMarkdown("- first\n\n- second\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Title != "first" || got[1].Title != "second" {
		t.Fatalf("ParseMarkdown = %+v, want two siblings", got)
	}
}

func TestParseMarkdownAllowsAnEmptyTitle(t *testing.T) {
	// Validate (notes.go) already allows an empty title — spec §5 says
	// pressing Enter creates one — so the parser must not reject one
	// either.
	got, err := notes.ParseMarkdown("- \n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "" {
		t.Fatalf("ParseMarkdown = %+v, want one node with an empty title", got)
	}
}

func TestParseMarkdownOfEmptyTextIsEmpty(t *testing.T) {
	got, err := notes.ParseMarkdown("")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("ParseMarkdown(\"\") = %+v, want none", got)
	}
}

func TestParseMarkdownRejectsADepthThatSkipsALevel(t *testing.T) {
	_, err := notes.ParseMarkdown("- top\n    - grandchild with no child\n")
	if err == nil {
		t.Fatal("ParseMarkdown = nil error, want ErrInvalid")
	}
}

func TestParseMarkdownRejectsStartingIndented(t *testing.T) {
	_, err := notes.ParseMarkdown("  - indented from the very first line\n")
	if err == nil {
		t.Fatal("ParseMarkdown = nil error, want ErrInvalid")
	}
}

func TestParseMarkdownRejectsOddIndentation(t *testing.T) {
	_, err := notes.ParseMarkdown("- top\n   - three spaces, not two\n")
	if err == nil {
		t.Fatal("ParseMarkdown = nil error, want ErrInvalid")
	}
}

func TestParseMarkdownRejectsTextNotUnderABullet(t *testing.T) {
	_, err := notes.ParseMarkdown("stray text with no bullet before it\n")
	if err == nil {
		t.Fatal("ParseMarkdown = nil error, want ErrInvalid")
	}
}

func TestParseMarkdownRejectsMoreThanMaxImportNodes(t *testing.T) {
	var b strings.Builder
	for i := 0; i <= notes.MaxImportNodes; i++ {
		b.WriteString("- x\n")
	}
	_, err := notes.ParseMarkdown(b.String())
	if err == nil {
		t.Fatal("ParseMarkdown = nil error, want ErrInvalid")
	}
}

// TestParseMarkdownAcceptsExactlyMaxImportNodes: the boundary itself must
// still succeed — only exceeding it is an error.
func TestParseMarkdownAcceptsExactlyMaxImportNodes(t *testing.T) {
	var b strings.Builder
	for i := 0; i < notes.MaxImportNodes; i++ {
		b.WriteString("- x\n")
	}
	got, err := notes.ParseMarkdown(b.String())
	if err != nil {
		t.Fatalf("ParseMarkdown: %v", err)
	}
	if len(got) != notes.MaxImportNodes {
		t.Fatalf("ParseMarkdown returned %d nodes, want %d", len(got), notes.MaxImportNodes)
	}
}

func TestParseMarkdownDoesNotSemanticallyValidateTheDueDate(t *testing.T) {
	// Syntax only, by design — see this task's plan for why full
	// ValidateDue-style checking is deferred to Ops.SetDue during
	// ImportUnder (Task 5), which already runs it: this avoids
	// duplicating that logic here.
	got, err := notes.ParseMarkdown("- impossible date @2026-02-30\n")
	if err != nil {
		t.Fatalf("ParseMarkdown: %v", err)
	}
	if len(got) != 1 || got[0].DueOn != "2026-02-30" {
		t.Fatalf("ParseMarkdown = %+v, want the raw due string carried through unvalidated", got)
	}
}
```

(These error-path tests check only `err == nil` rather than
`errors.Is(err, notes.ErrInvalid)`, since `strings`/`testing` are the only
imports this file needs for now — Task 5 adds `"context"` and `"errors"`
when it appends tests that do need `errors.Is`.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/... -run TestParseMarkdown -v`
Expected: FAIL — `notes.ParseMarkdown` and `notes.ParsedNode` do not exist
yet.

- [ ] **Step 3: Implement `ParseMarkdown`**

Create `internal/apps/notes/import.go`:

```go
package notes

import (
	"fmt"
	"regexp"
	"strings"
)

// ParsedNode is one bullet of a parsed Markdown outline (spec §14), flat
// and pre-order — the same shape Store.Export produces (export.go) —
// Depth relative to whatever parent the caller eventually attaches these
// under. It carries no id: these are not yet real nodes.
type ParsedNode struct {
	Depth int
	Title string
	Note  string
	Done  bool
	DueOn string
}

// bulletLineRe matches a "- " line and captures its indent (an even
// number of spaces) and everything after the marker. RE2 (Go's regexp
// package), so this can never backtrack into pathological time on
// adversarial input — the same reasoning markdown.go's inline renderer
// already documents for its own patterns.
var bulletLineRe = regexp.MustCompile(`^((?:  )*)- (.*)$`)

// dueSuffixRe matches ExportMarkdown's trailing "@YYYY-MM-DD", greedily:
// the leading .* must consume as much as possible, which is what makes
// this find the rightmost such suffix rather than the first one that
// happens to fit — see parseTitleLine's own doc comment for why order
// matters here.
var dueSuffixRe = regexp.MustCompile(`^(.*) @(\d{4}-\d{2}-\d{2})$`)

const doneSuffix = " [x]"

// ParseMarkdown parses spec §14's outline format: a "- " line per node,
// two spaces of indent per level, an optional "[x]" suffix for done and a
// trailing "@YYYY-MM-DD" for a due date, with the note as unbulleted
// lines indented one level deeper than their bullet. It is shared by
// POST /notes/import (a file, Task 5) and POST /notes/{id}/paste (a
// clipboard block, Task 6) — spec §14's "the same code path, reached from
// the editor instead of from a file" — so it does no I/O of its own:
// Ops.ImportUnder (Task 5) is what turns its result into real nodes.
//
// A malformed line is a parse error rather than a best-effort guess — see
// this task's own design note in the plan for why silently dropping part
// of an upload, the way a corrupted display row is tolerated elsewhere in
// this package, is the wrong instinct for untrusted input.
//
// Only the exact "- " marker (a literal hyphen and one space) is
// recognised, and only an exactly-even number of leading spaces — no
// other Markdown bullet syntax ("* ", "+ ") and no odd indentation are
// accepted, matching exactly what ExportMarkdown ever produces.
func ParseMarkdown(text string) ([]ParsedNode, error) {
	var out []ParsedNode
	lastBullet := -1 // index into out of the most recently seen bullet
	lastDepth := -1  // that bullet's own Depth

	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}

		if m := bulletLineRe.FindStringSubmatch(line); m != nil {
			depth := len(m[1]) / 2
			if depth > lastDepth+1 {
				return nil, fmt.Errorf("%w: line %d: bullet is indented deeper than its possible parent", ErrInvalid, i+1)
			}
			if len(out) >= MaxImportNodes {
				return nil, fmt.Errorf("%w: import exceeds the %d-bullet limit", ErrInvalid, MaxImportNodes)
			}
			title, done, dueOn := parseTitleLine(m[2])
			out = append(out, ParsedNode{Depth: depth, Title: title, Done: done, DueOn: dueOn})
			lastBullet, lastDepth = len(out)-1, depth
			continue
		}

		if strings.HasPrefix(strings.TrimLeft(line, " "), "- ") {
			// Looks like a bullet, but its indentation was not a clean
			// multiple of two spaces — bulletLineRe above would have
			// matched it otherwise. Reported explicitly rather than
			// falling through to the note-line branch below, which could
			// otherwise silently misfile a badly-indented bullet as plain
			// text.
			return nil, fmt.Errorf("%w: line %d: bullet indentation must be a multiple of two spaces", ErrInvalid, i+1)
		}

		minIndent := (lastDepth + 1) * 2
		indent := leadingSpaces(line)
		if lastBullet < 0 || indent < minIndent {
			return nil, fmt.Errorf("%w: line %d: text is not indented under any bullet", ErrInvalid, i+1)
		}
		noteLine := line[minIndent:]
		if out[lastBullet].Note == "" {
			out[lastBullet].Note = noteLine
		} else {
			out[lastBullet].Note += "\n" + noteLine
		}
	}
	return out, nil
}

func leadingSpaces(s string) int {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return n
}

// parseTitleLine strips a bullet's optional "[x]" and "@YYYY-MM-DD"
// suffixes. The due-date suffix is stripped first — the reverse of
// ExportMarkdown's own append order (export.go: title, then " [x]", then
// " @date") — because it is the rightmost of the two: a line carrying
// both must have its true trailing suffix removed before the "[x]" check
// looks at what remains, or "title [x] @2026-09-01" would never match the
// due-date pattern at all (the literal "[x]" would sit between the title
// and "$").
//
// Neither check semantically validates its value: an out-of-range date
// like "2026-02-30" parses here exactly as written, and ValidateDue's own
// check runs later, inside the same transaction as node creation
// (Ops.ImportUnder, Task 5, via Ops.SetDue) — duplicating that check here
// would just be the same logic run twice.
func parseTitleLine(s string) (title string, done bool, dueOn string) {
	title = s
	if m := dueSuffixRe.FindStringSubmatch(title); m != nil {
		title, dueOn = m[1], m[2]
	}
	if strings.HasSuffix(title, doneSuffix) {
		title = strings.TrimSuffix(title, doneSuffix)
		done = true
	}
	return title, done, dueOn
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -run TestParseMarkdown -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/notes/import.go internal/apps/notes/import_test.go
git commit -m "feat(notes): add ParseMarkdown"
```

---

### Task 5: `Ops.ImportUnder` and `POST /notes/import`

**Files:**
- Modify: `internal/apps/notes/import.go`
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/app.go`
- Modify: `internal/apps/notes/templates/outline.html`
- Modify: `internal/ui/static/app.css`
- Test: `internal/apps/notes/import_test.go`
- Test: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `ParsedNode`/`ParseMarkdown` (Task 4); `Ops.Create`/`Ops.SetDone`/
  `Ops.SetDue`/`Store.Do` (`tree.go`, unchanged); `MaxImportFileBytes`
  (Task 2); `formID`/`outlinePath`/`renderOutlineFragment`/`a.fail`
  (`handlers.go`, unchanged); Task 1's CSRF fix, without which a plain
  (JavaScript-off) submission of this task's own form would 403 before
  reaching this handler at all.
- Produces: `(*Ops).ImportUnder(ctx, userID, parentID int64, parsed []ParsedNode) (int, error)`;
  `(*Store).ImportUnder(...)`; `POST /notes/import`. Task 6's paste route
  reuses `Ops.ImportUnder` directly (not `Store.ImportUnder` — paste needs
  its transaction shared with `mutate`'s own text save).

This task also closes spec §14's own round-trip claim end to end: export a
tree, import it back, and check what comes out the other side matches.

**Design note — no `http.MaxBytesReader` in this handler.** This
plan's Global Constraints section explains why: `CSRF.Middleware` has
already called `r.ParseMultipartForm` on this request by the time this
handler runs (Task 1), so `r.Body` has already been fully read — wrapping
it here would protect nothing. `MaxImportFileBytes` is instead enforced by
checking the uploaded file's reported size once it is available.

- [ ] **Step 1: Write the failing tests for `ImportUnder`**

Read `internal/apps/notes/tree_test.go`'s `fixture`/`f.mk`/`f.mkFor`/`f.childTitles`
helpers first to confirm their current exact signatures, then append to
`internal/apps/notes/import_test.go`:

```go
func TestImportUnderCreatesTheWholeSubtree(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parsed, err := notes.ParseMarkdown(
		"- Software projects\n" +
			"  Various software development projects that I'm involved in\n" +
			"  - AtBudget\n" +
			"    - Project objectives [x]\n" +
			"    - API @2026-09-01\n")
	if err != nil {
		t.Fatal(err)
	}

	created, err := f.store.ImportUnder(ctx, f.alice.ID, notes.RootID, parsed)
	if err != nil {
		t.Fatal(err)
	}
	if created != 4 {
		t.Fatalf("ImportUnder created %d nodes, want 4", created)
	}

	top := f.childTitles(t, f.alice.ID, notes.RootID)
	if len(top) != 1 || top[0] != "Software projects" {
		t.Fatalf("top level = %v, want just Software projects", top)
	}
	projects, err := f.store.ByID(ctx, f.alice.ID, mustFindID(t, f, "Software projects"))
	if err != nil {
		t.Fatal(err)
	}
	if projects.Note != "Various software development projects that I'm involved in" {
		t.Errorf("note = %q", projects.Note)
	}

	objectives, err := f.store.ByID(ctx, f.alice.ID, mustFindID(t, f, "Project objectives"))
	if err != nil {
		t.Fatal(err)
	}
	if !objectives.Done {
		t.Error("Project objectives should be done")
	}

	api, err := f.store.ByID(ctx, f.alice.ID, mustFindID(t, f, "API"))
	if err != nil {
		t.Fatal(err)
	}
	if api.DueOn != "2026-09-01" {
		t.Errorf("API's due date = %q, want 2026-09-01", api.DueOn)
	}
}

// mustFindID walks alice's whole tree looking for a node with the given
// title, for tests that need a real id to look a freshly imported node up
// by. Import assigns ids the caller cannot predict in advance.
func mustFindID(t *testing.T, f *fixture, title string) int64 {
	t.Helper()
	flat, err := f.store.Export(context.Background(), f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range flat {
		if n.Title == title {
			return n.ID
		}
	}
	t.Fatalf("no node titled %q", title)
	return 0
}

func TestImportUnderAppendsAfterExistingChildren(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "existing")
	parsed, err := notes.ParseMarkdown("- new\n")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.ImportUnder(ctx, f.alice.ID, notes.RootID, parsed); err != nil {
		t.Fatal(err)
	}

	got := f.childTitles(t, f.alice.ID, notes.RootID)
	want := []string{"existing", "new"}
	if !equalStrings(got, want) {
		t.Fatalf("top level = %v, want %v", got, want)
	}
}

func TestImportUnderUnderASpecificParent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent")
	parsed, err := notes.ParseMarkdown("- child\n")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.ImportUnder(ctx, f.alice.ID, parent.ID, parsed); err != nil {
		t.Fatal(err)
	}
	got := f.childTitles(t, f.alice.ID, parent.ID)
	if len(got) != 1 || got[0] != "child" {
		t.Fatalf("children of parent = %v, want just child", got)
	}
}

func TestImportUnderRejectsAMalformedDueDate(t *testing.T) {
	f := newFixture(t)
	parsed, err := notes.ParseMarkdown("- impossible @2026-02-30\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ImportUnder(context.Background(), f.alice.ID, notes.RootID, parsed); !errors.Is(err, notes.ErrInvalid) {
		t.Fatalf("ImportUnder = %v, want ErrInvalid", err)
	}
}

// TestImportUnderIsOneTransaction: a bad due date on the second bullet
// must roll back the first — spec §7's one-write discipline extends here
// too, so a partially-imported file never sits half-applied.
func TestImportUnderIsOneTransaction(t *testing.T) {
	f := newFixture(t)
	parsed, err := notes.ParseMarkdown("- fine\n- impossible @2026-02-30\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ImportUnder(context.Background(), f.alice.ID, notes.RootID, parsed); !errors.Is(err, notes.ErrInvalid) {
		t.Fatalf("ImportUnder = %v, want ErrInvalid", err)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); len(got) != 0 {
		t.Fatalf("top level = %v, want nothing — the whole import should have rolled back", got)
	}
}

func TestImportUnderRejectsAnotherUsersParent(t *testing.T) {
	f := newFixture(t)
	bobs := f.mkFor(t, f.bob.ID, notes.RootID, "bob's")
	parsed, err := notes.ParseMarkdown("- x\n")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.store.ImportUnder(context.Background(), f.alice.ID, bobs.ID, parsed)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("ImportUnder under bob's node = %v, want ErrNotFound", err)
	}
}

// TestExportThenImportRoundTrips is spec §14's own claim: "because done
// state and due dates are encoded, a document round-trips." Exports a
// tree, imports the resulting text back under a fresh parent, and checks
// the copy matches the original on every field Markdown carries.
func TestExportThenImportRoundTrips(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "Software projects")
	if err := f.store.SetText(ctx, f.alice.ID, parent.ID, "Software projects", "a note"); err != nil {
		t.Fatal(err)
	}
	child := f.mk(t, parent.ID, "AtBudget")
	grandchild := f.mk(t, child.ID, "Project objectives")
	if err := f.store.SetDone(ctx, f.alice.ID, grandchild.ID, true); err != nil {
		t.Fatal(err)
	}
	sibling := f.mk(t, child.ID, "API")
	if err := f.store.SetDue(ctx, f.alice.ID, sibling.ID, "2026-09-01"); err != nil {
		t.Fatal(err)
	}

	exported, err := f.store.Export(ctx, f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	md := notes.ExportMarkdown(exported)

	parsed, err := notes.ParseMarkdown(md)
	if err != nil {
		t.Fatalf("re-parsing the export: %v", err)
	}
	copyRoot := f.mk(t, notes.RootID, "copy destination")
	if _, err := f.store.ImportUnder(ctx, f.alice.ID, copyRoot.ID, parsed); err != nil {
		t.Fatal(err)
	}

	reExported, err := f.store.Export(ctx, f.alice.ID, copyRoot.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := notes.ExportMarkdown(reExported)
	if got != md {
		t.Errorf("round-trip mismatch:\noriginal:\n%s\nafter round-trip:\n%s", md, got)
	}
}
```

Change `import_test.go`'s import block from Task 4's

```go
import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)
```

to

```go
import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)
```

`equalStrings` above is the helper already defined in
`internal/apps/notes/handlers_test.go` — reused here rather than redefined,
since both files are part of the same `notes_test` package.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/... -run 'TestImportUnder|TestExportThenImportRoundTrips' -v`
Expected: FAIL — `f.store.ImportUnder` does not exist yet.

- [ ] **Step 3: Implement `Ops.ImportUnder`/`Store.ImportUnder`**

In `internal/apps/notes/import.go`, add `"context"` to the existing import
block (`"fmt"`, `"regexp"`, `"strings"`), then append:

```go
// ImportUnder inserts parsed as new children of parentID, appended after
// whatever is already there, in one transaction — spec §14: import and
// paste-into-a-bullet (Task 6) share this exact code path. Returns how
// many nodes were created.
//
// parsed's Depth values are trusted never to skip a level: ParseMarkdown
// (Task 4) already rejects that shape at parse time, so unlike nest
// (view.go), which tolerates and drops a corrupted display row, this
// never needs to guess at one — the default branch below exists only as a
// guard against parsed arriving from some future second producer that
// does not go through ParseMarkdown, not as an expected path today.
//
// Depth and node-count bounds are Create's and ParseMarkdown's own —
// MaxDepth via Create's existing ErrTooDeep, MaxImportNodes already
// enforced by ParseMarkdown before this ever runs — so nothing here
// duplicates either check.
func (o *Ops) ImportUnder(ctx context.Context, userID, parentID int64, parsed []ParsedNode) (int, error) {
	open := make([]int64, 0, MaxDepth+1) // open[d] is the id of the ancestor at depth d
	created := 0

	for _, p := range parsed {
		parent := parentID
		switch {
		case p.Depth == 0:
			open = open[:0]
		case p.Depth > 0 && p.Depth <= len(open):
			parent = open[p.Depth-1]
			open = open[:p.Depth]
		default:
			return created, fmt.Errorf("%w: malformed parsed node at depth %d", ErrInvalid, p.Depth)
		}

		n, err := o.Create(ctx, userID, parent, maxPosition, p.Title, p.Note)
		if err != nil {
			return created, err
		}
		if p.Done {
			if err := o.SetDone(ctx, userID, n.ID, true); err != nil {
				return created, err
			}
		}
		if p.DueOn != "" {
			if err := o.SetDue(ctx, userID, n.ID, p.DueOn); err != nil {
				return created, err
			}
		}
		open = append(open, n.ID)
		created++
	}
	return created, nil
}

// ImportUnder inserts parsed as new children of parentID, in a
// transaction of its own. See Ops.ImportUnder.
func (st *Store) ImportUnder(ctx context.Context, userID, parentID int64, parsed []ParsedNode) (int, error) {
	var created int
	err := st.Do(ctx, func(o *Ops) error {
		var err error
		created, err = o.ImportUnder(ctx, userID, parentID, parsed)
		return err
	})
	return created, err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, the whole package.

- [ ] **Step 5: Write the failing handler tests**

Read `internal/apps/notes/handlers_test.go`'s existing import block and
`s.post`/`s.csrfToken` helpers first, then append:

```go
// multipartMarkdownRequest builds a POST /notes/import request carrying
// content as a file named "file", plus root and a valid CSRF token — the
// shape s.post's url.Values-based helper cannot produce, since this route
// needs multipart/form-data rather than urlencoded.
func (s *server) multipartMarkdownRequest(t *testing.T, sess *session, root, content string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("root", root); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("csrf_token", s.csrfToken(t, sess)); err != nil {
		t.Fatal(err)
	}
	part, err := w.CreateFormFile("file", "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/notes/import", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestImportCreatesNodesUnderTheZoomRoot(t *testing.T) {
	s := newServer(t)
	req := s.multipartMarkdownRequest(t, s.alice, "0", "- imported\n")

	rec := s.do(t, s.alice, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/notes/" {
		t.Fatalf("redirected to %q, want /notes/", got)
	}

	got := s.titlesAt(t, s.alice, notes.RootID)
	if len(got) != 1 || got[0] != "imported" {
		t.Fatalf("top level = %v, want just imported", got)
	}
}

func TestImportUnderAZoomedRoot(t *testing.T) {
	s := newServer(t)
	root := s.seed(t, s.alice, notes.RootID, "zoomed root")
	req := s.multipartMarkdownRequest(t, s.alice, itoa(root), "- child\n")

	rec := s.do(t, s.alice, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body: %s", rec.Code, rec.Body.String())
	}

	got := s.titlesAt(t, s.alice, root)
	if len(got) != 1 || got[0] != "child" {
		t.Fatalf("children of root = %v, want just child", got)
	}
}

func TestImportRespondsWithAFragmentForHTMX(t *testing.T) {
	s := newServer(t)
	req := s.multipartMarkdownRequest(t, s.alice, "0", "- imported\n")
	req.Header.Set("HX-Request", "true")

	rec := s.do(t, s.alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "imported") {
		t.Error("the imported bullet is not in the fragment")
	}
}

func TestImportRejectsMalformedMarkdown(t *testing.T) {
	s := newServer(t)
	req := s.multipartMarkdownRequest(t, s.alice, "0", "stray text with no bullet\n")

	rec := s.do(t, s.alice, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

// TestImportRejectsAnOversizedFile checks the body against
// MaxImportFileBytes specifically, not against the platform's own larger
// 1 MiB ceiling — a file just over MaxImportFileBytes must still fail
// even though it is comfortably under the platform's own cap.
func TestImportRejectsAnOversizedFile(t *testing.T) {
	s := newServer(t)
	oversized := strings.Repeat("x", notes.MaxImportFileBytes+1)
	req := s.multipartMarkdownRequest(t, s.alice, "0", "- "+oversized+"\n")

	rec := s.do(t, s.alice, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

func TestImportRequiresSignIn(t *testing.T) {
	s := newServer(t)
	req := httptest.NewRequest("POST", "/notes/import", strings.NewReader(""))
	rec := s.do(t, nil, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /notes/import anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

func TestOutlineToolbarHasAnImportForm(t *testing.T) {
	s := newServer(t)
	doc := s.get(t, s.alice, "/notes/")
	form := doc.MustHave("form.notes-import")
	if got, _ := htmlassert.Attr(form, "action"); got != "/notes/import" {
		t.Errorf("import form action = %q, want /notes/import", got)
	}
	if got, _ := htmlassert.Attr(form, "enctype"); got != "multipart/form-data" {
		t.Errorf("import form enctype = %q, want multipart/form-data", got)
	}
	doc.MustHave("form.notes-import input[type=file]")
}
```

Add `"bytes"` and `"mime/multipart"` to `handlers_test.go`'s import block
(it already has `"net/http"`, `"net/http/httptest"`, `"strings"`, and the
rest):

```go
import (
	"bytes"
	"context"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)
```

(Task 3's `TestOutlineToolbarHasAnExportLink` already added
`"golang.org/x/net/html"` usage via `*html.Node` — confirm it is present
exactly once in the final import block, not duplicated.)

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/... -run 'TestImport|TestOutlineToolbarHasAnImportForm' -v`
Expected: FAIL — `a.importNotes`, `POST /notes/import`, and the toolbar
form do not exist yet.

- [ ] **Step 7: Implement the handler**

In `internal/apps/notes/handlers.go`, append after `export` (added in
Task 3):

```go
// importNotes parses an uploaded Markdown file (spec §14) and creates its
// bullets as new children of root — the outline's current zoom, per the
// hidden root field every structural form already carries. It does not go
// through mutate: uploading a file from the toolbar happens with no
// outline row being edited, the same situation prefs and restore are
// already in, so there is no focused bullet's text to save alongside it.
//
// No http.MaxBytesReader here: CSRF.Middleware (Task 1,
// internal/platform/web/csrf.go) has already called r.ParseMultipartForm
// on this request looking for the token, so r.Body is already fully
// consumed by the time this handler runs — wrapping it now would protect
// nothing. MaxImportFileBytes is enforced below by checking the uploaded
// file's own reported size instead.
func (a *App) importNotes(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	if err := r.ParseMultipartForm(web.DefaultMaxBodyBytes); err != nil {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	root, ok := formID(r, "root")
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()
	if header.Size > MaxImportFileBytes {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	parsed, err := ParseMarkdown(string(data))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if _, err := a.store.ImportUnder(r.Context(), userID, root, parsed); err != nil {
		a.fail(w, r, err)
		return
	}

	if web.IsHTMX(r) {
		a.renderOutlineFragment(w, r, userID, root, showCompletedFrom(r))
		return
	}
	http.Redirect(w, r, outlinePath(root), http.StatusSeeOther)
}
```

Add `"io"` to `handlers.go`'s import block:

```go
import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)
```

- [ ] **Step 8: Register the route**

In `internal/apps/notes/app.go`, the route-map comment (after Task 3's own
edit) has:

```go
//	GET  /notes/export       likewise a literal segment; N8's Markdown download
//	POST /notes/new          a literal segment, and literals outrank {id}
```

Change it to:

```go
//	GET  /notes/export       likewise a literal segment; N8's Markdown download
//	POST /notes/import       likewise a literal segment; N8's Markdown upload
//	POST /notes/new          a literal segment, and literals outrank {id}
```

`Mount` currently has:

```go
	r.HandleFunc("POST /new", a.create)
	r.HandleFunc("POST /prefs", a.prefs)
```

Change it to:

```go
	r.HandleFunc("POST /new", a.create)
	r.HandleFunc("POST /import", a.importNotes)
	r.HandleFunc("POST /prefs", a.prefs)
```

- [ ] **Step 9: Add the toolbar form**

In `internal/apps/notes/templates/outline.html`, the toolbar now reads
(after Task 3's own edit):

```html
			<div class="notes-toolbar-actions">
				<a href="/notes/due" class="toolbar-btn toolbar-btn-nav">Due</a>
				<a href="/notes/archive" class="toolbar-btn toolbar-btn-nav">Archive</a>
				<a href="/notes/export?root={{.Data.Root.ID}}" class="toolbar-btn toolbar-btn-nav">Export</a>
				<form method="post" action="/notes/prefs"
```

Insert the Import form between the Export link and the prefs form:

```html
			<div class="notes-toolbar-actions">
				<a href="/notes/due" class="toolbar-btn toolbar-btn-nav">Due</a>
				<a href="/notes/archive" class="toolbar-btn toolbar-btn-nav">Archive</a>
				<a href="/notes/export?root={{.Data.Root.ID}}" class="toolbar-btn toolbar-btn-nav">Export</a>
				<form method="post" action="/notes/import" enctype="multipart/form-data" class="notes-import"
				      hx-post="/notes/import" hx-target="#outline" hx-swap="innerHTML" hx-encoding="multipart/form-data">
					<input type="hidden" name="csrf_token" value="{{.Data.CSRFToken}}">
					<input type="hidden" name="root" value="{{.Data.Root.ID}}">
					<input type="file" name="file" accept=".md,text/markdown" aria-label="Import a Markdown file" required>
					<button type="submit" class="toolbar-btn">Import</button>
				</form>
				<form method="post" action="/notes/prefs"
```

`hx-encoding="multipart/form-data"` is required: htmx defaults every
request to urlencoded, and would otherwise send the file's bytes as an
unusable percent-encoded string instead of a real multipart part.

- [ ] **Step 10: Add the CSS**

In `internal/ui/static/app.css`, find `.notes-toolbar-actions` (added in
N6/N7) and add immediately after its rule:

```css
.notes-import {
	display: inline-flex;
	align-items: center;
	gap: var(--s-1);
}
.notes-import input[type="file"] {
	font-size: var(--fs-sm);
	max-width: 10rem;
}
```

- [ ] **Step 11: Run the tests and a manual pass**

Run: `go build ./... && go vet ./... && go test ./internal/apps/notes/... -v`
Expected: PASS, the whole package.

Then by hand: from the outline, click Export, save the file, then use
Import to upload that exact file back in under a different zoom — confirm
the structure, done marks and due dates all reappear; try uploading a file
that is not valid Markdown outline syntax and confirm a clear error;
repeat with JavaScript disabled, confirming the plain-form fallback
(`method="post" enctype="multipart/form-data"`) still works — this is the
scenario Task 1's fix exists for, so it is worth confirming by hand, not
only by the handler test that already exercises it with a valid token.

- [ ] **Step 12: Commit**

```bash
git add internal/apps/notes/import.go internal/apps/notes/handlers.go \
        internal/apps/notes/app.go internal/apps/notes/templates/outline.html \
        internal/ui/static/app.css internal/apps/notes/import_test.go \
        internal/apps/notes/handlers_test.go
git commit -m "feat(notes): add Ops.ImportUnder and POST /notes/import"
```

---

### Task 6: Paste a block into a bullet

**Files:**
- Modify: `internal/apps/notes/handlers.go`
- Modify: `internal/apps/notes/app.go`
- Modify: `internal/apps/notes/static/notes.js`
- Test: `internal/apps/notes/handlers_test.go`

**Interfaces:**
- Consumes: `ParseMarkdown` (Task 4); `Ops.ImportUnder` (Task 5);
  `mutate`/`mutation` (`handlers.go`, unchanged); `MaxPasteTextBytes`
  (Task 2).
- Produces: `POST /notes/{id}/paste`.

**Design note — the trigger decision lives entirely in the browser.**
Whether a paste looks outline-shaped is decided once, client-side, by
`notes.js`, using a cheap mirror of `ParseMarkdown`'s own bullet-line rule
(does at least one line match `^(?:  )*- `). If it does not, `notes.js`
never calls `preventDefault` and never issues this request at all — the
browser's own default paste behaviour runs, unchanged, exactly as it
already does for any other field. The server side of this route therefore
has no "not outline-shaped, fall back to literal text" branch of its own:
`/notes/{id}/paste` always attempts the real parse, and a request that
does not look like the format (a bad actor, or a non-JS client posting to
it directly) gets the same 400 `ParseMarkdown` already gives `/notes/import`
for the same input. This is deliberate, not a gap — the fallback-to-plain
behaviour is a client-side UX choice about an already-completed browser
paste, not something the server route is in a position to offer once it
has already been asked to create nodes.

**Design note — this goes through `mutate`, unlike import.** Pasting
happens inside a specific bullet's title or note field, which may carry
unsaved text the same way indenting or archiving that row would — spec
§7's one-write rule applies here exactly as it does to every other
structural action reachable from a row, so this reuses `mutate` rather
than being handled directly the way `importNotes` (Task 5) is.

**Design note — no `http.MaxBytesReader` here either, for the same reason
as Task 5.** `CSRF.Middleware` has already called `r.ParseForm()` on this
urlencoded request looking for the token, consuming the body before this
handler runs. `MaxPasteTextBytes` is enforced by checking the length of
the `text` field once `PostFormValue` returns it, not by capping the read.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/notes/handlers_test.go`:

```go
func TestPasteCreatesASubtreeUnderTheBullet(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "target")

	s.submit(t, s.alice, "/notes/"+itoa(id)+"/paste", url.Values{
		"root": {"0"}, "text": {"- child\n  - grandchild\n"},
	}, "/notes/")

	got := s.titlesAt(t, s.alice, id)
	if len(got) != 1 || got[0] != "child" {
		t.Fatalf("children of the target = %v, want just child", got)
	}
	grandchildID := mustFindTitleUnder(t, s, id, "child")
	grandchildren := s.titlesAt(t, s.alice, grandchildID)
	if len(grandchildren) != 1 || grandchildren[0] != "grandchild" {
		t.Fatalf("children of child = %v, want just grandchild", grandchildren)
	}
}

// mustFindTitleUnder returns the id of parentID's child titled title, for
// a test asserting on a grandchild whose id the request never reports
// back directly.
func mustFindTitleUnder(t *testing.T, s *server, parentID int64, title string) int64 {
	t.Helper()
	children, err := s.store.Children(context.Background(), s.alice.user.ID, parentID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range children {
		if c.Title == title {
			return c.ID
		}
	}
	t.Fatalf("no child titled %q under %d", title, parentID)
	return 0
}

// TestPasteSavesTheFocusedTextInTheSameTransaction is spec §7: pasting
// into a bullet's title while the note field carries unsaved text must
// not lose it, the same guarantee every other structural request already
// gives every row.
func TestPasteSavesTheFocusedTextInTheSameTransaction(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "target")

	s.submit(t, s.alice, "/notes/"+itoa(id)+"/paste", url.Values{
		"root": {"0"}, "focus_id": {itoa(id)},
		"title": {"target"}, "note": {"typed just before pasting"},
		"text": {"- child\n"},
	}, "/notes/")

	n, err := s.store.ByID(context.Background(), s.alice.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Note != "typed just before pasting" {
		t.Errorf("note = %q, the concurrent text edit was lost", n.Note)
	}
}

func TestPasteRejectsTextThatIsNotOutlineShaped(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "target")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/paste", url.Values{
		"root": {"0"}, "text": {"just some ordinary text\nwith a second line\n"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

func TestPasteRejectsOversizedText(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.alice, notes.RootID, "target")
	oversized := strings.Repeat("x", notes.MaxPasteTextBytes+1)

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/paste", url.Values{
		"root": {"0"}, "text": {"- " + oversized},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

func TestPasteOnAnotherUsersBulletIs404(t *testing.T) {
	s := newServer(t)
	id := s.seed(t, s.bob, notes.RootID, "bob's")

	rec := s.post(t, s.alice, "/notes/"+itoa(id)+"/paste", url.Values{
		"root": {"0"}, "text": {"- child\n"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
```

Also extend `TestEveryMutationRequiresSignIn`'s `paths` slice — read the
existing slice first to confirm its current contents (it lists `new`,
`text`, `indent`, `outdent`, `move`, `collapse`, `delete` and `archive`),
then add:

```go
		"/notes/" + itoa(id) + "/paste",
```

as its final entry.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/notes/... -run 'TestPaste|TestEveryMutationRequiresSignIn' -v`
Expected: FAIL — `a.paste` and `POST /notes/{id}/paste` do not exist yet.

- [ ] **Step 3: Implement the handler**

In `internal/apps/notes/handlers.go`, append after `importNotes` (added in
Task 5):

```go
// paste creates a subtree under NodeID from a clipboard block — spec §14,
// "the same code path, reached from the editor instead of from a file"
// as POST /notes/import (Task 5). See this task's own design notes in the
// plan for why the shape decision is entirely notes.js's, why this goes
// through mutate while import does not, and why there is no
// http.MaxBytesReader here.
func (a *App) paste(w http.ResponseWriter, r *http.Request) {
	text := r.PostFormValue("text")
	if len(text) > MaxPasteTextBytes {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	a.mutate(w, r, func(ctx context.Context, o *Ops, m mutation) error {
		parsed, err := ParseMarkdown(text)
		if err != nil {
			return err
		}
		_, err = o.ImportUnder(ctx, m.UserID, m.NodeID, parsed)
		return err
	})
}
```

- [ ] **Step 4: Register the route**

In `internal/apps/notes/app.go`, `Mount` currently ends:

```go
	r.HandleFunc("POST /{id}/due", a.due)
	r.HandleFunc("POST /{id}/archive", a.archive)
}
```

Change it to:

```go
	r.HandleFunc("POST /{id}/due", a.due)
	r.HandleFunc("POST /{id}/archive", a.archive)
	r.HandleFunc("POST /{id}/paste", a.paste)
}
```

The route-map comment's "text and the eight other mutations" line already
covers every `POST /notes/{id}/...` route without naming each one, exactly
as it already does for `done`, `due` and `archive` — no comment edit
needed for this one.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/apps/notes/... -v`
Expected: PASS, the whole package.

- [ ] **Step 6: Add the client-side interception**

Read `internal/apps/notes/static/notes.js`'s current `initKeyboard`
function first to confirm the exact current text around the file's
closing `initFocusSync(); initKeyboard(); })();` lines (fix commits since
N7 may have touched nearby code), then change it from:

```js
	function initKeyboard() {
		if (!document.getElementById("outline")) return;
		document.addEventListener("keydown", handleKeydown);
	}

	initFocusSync();
	initKeyboard();
})();
```

to:

```js
	function initKeyboard() {
		if (!document.getElementById("outline")) return;
		document.addEventListener("keydown", handleKeydown);
	}

	// ---- paste: outline-shaped blocks only ---------------------------------
	//
	// looksLikeOutline is a cheap client-side mirror of ParseMarkdown's own
	// bullet-line rule (import.go): does at least one line start with an
	// even number of spaces followed by "- ". It only decides WHETHER to
	// intercept — the server runs the real parse once the request lands, so
	// a mismatch between this regex and the Go one can only ever produce an
	// unnecessary round trip (this says yes, ParseMarkdown says no, and the
	// user sees an error) rather than silently mishandling a paste; it can
	// never make the browser's own default paste behaviour do the wrong
	// thing, since that path is untouched unless this test passes.
	function looksLikeOutline(text) {
		return /^(?: {2})*- /m.test(text);
	}

	// handlePaste is spec §14's "paste-a-multi-line-block-into-a-bullet":
	// only intercepted when the clipboard content already looks like this
	// app's own export format (see looksLikeOutline above) — anything else
	// is left entirely to the browser's default paste, which is why every
	// early return here happens before preventDefault.
	function handlePaste(e) {
		var el = e.target;
		if (!isOutlineField(el)) return;

		var clipboard = e.clipboardData || window.clipboardData;
		var text = clipboard ? clipboard.getData("text/plain") : "";
		if (!looksLikeOutline(text)) return;

		var row = rowOf(el);
		// The empty outline's bootstrap field has no data-id (see
		// trackFocus) and so nothing to paste children under yet — left to
		// the browser's default paste, the same as any other unmatched
		// case, rather than swallowing the paste with nothing to show for
		// it.
		if (!row || !row.hasAttribute("data-id")) return;

		e.preventDefault();
		var id = row.getAttribute("data-id");
		var rootField = row.querySelector('input[name="root"]');
		var title = row.querySelector('input[name="title"]');
		var note = row.querySelector('input[name="note"]');

		htmx.ajax("POST", "/notes/" + id + "/paste", {
			source: document.body,
			target: "#outline",
			swap: "innerHTML",
			values: {
				root: rootField.value,
				focus_id: id,
				title: title ? title.value : "",
				note: note ? note.value : "",
				text: text,
				_skipFocusOverride: "1"
			}
		});
	}

	function initPaste() {
		if (!document.getElementById("outline")) return;
		document.addEventListener("paste", handlePaste);
	}

	initFocusSync();
	initKeyboard();
	initPaste();
})();
```

- [ ] **Step 7: A manual pass**

`notes.js` has no tests of its own, by design (spec §17) — verify by hand.
Start the app, then: paste `- one\n  - two\n` into a bullet's title field
and confirm it creates a subtree under that bullet rather than being typed
literally; paste an ordinary multi-line paragraph into a title field and
confirm it behaves exactly as an unmodified browser would (this app's
`<input>` fields already collapse embedded newlines on their own — nothing
about this task should change that for non-outline-shaped text); paste
into a note field and confirm the same subtree behaviour; try both with
some unsaved text sitting in the row's other field first, and confirm it
is not lost.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/notes/handlers.go internal/apps/notes/app.go \
        internal/apps/notes/static/notes.js internal/apps/notes/handlers_test.go
git commit -m "feat(notes): add paste-a-block-into-a-bullet"
```

---

### Task 7: Docs

**Files:**
- Modify: `README.md`
- Modify: `internal/apps/notes/notes.go`

**Interfaces:** None — this task changes only prose.

- [ ] **Step 1: Update the package doc comment**

Read `internal/apps/notes/notes.go`'s current package doc comment first —
confirm it currently says this package spans N1 through N7, listing
`archive.go` as the last file mentioned (N7's own fix commits may have
touched nearby wording since). Update its final paragraph to also mention
N8:

```go
// The design is in docs/superpowers/specs/2026-08-25-on-notes-design.md. This
// package spans chunk N1 (schema and store) through N8 (export and
// import): app.App, routes, templates, handlers, the keyboard layer in
// static/notes.js, the inline Markdown renderer in markdown.go, task
// tracking in tree.go, prefs.go and due.go, full-text search in
// search.go, archiving in archive.go, and Markdown/JSON export plus
// Markdown import in export.go and import.go.
package notes
```

- [ ] **Step 2: Update `README.md`**

Read `README.md`'s ON Notes status paragraph first — it has been edited by
fix commits since N7 landed and may no longer read exactly as it did when
this plan was written. It should currently say N1 through N7 are done;
change it to also list N8:

```
N1 (schema and store), N2 (the outline), N3 (the keyboard layer), N4
(Markdown), N5 (done + due dates), N6 (search and tags), N7 (archiving)
and N8 (export and import) are done.
```

- [ ] **Step 3: Run the full test suite once more**

Run: `go build ./... && go vet ./... && go test ./... -race`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add README.md internal/apps/notes/notes.go
git commit -m "docs(notes): note N8 (export and import) as done"
```

---

## Self-Review

**Spec coverage.** §14 in full: Markdown export of the whole tree and of a
subtree (Task 3); import parsing the same format into a chosen parent —
"chosen" resolved as "wherever the outline is currently zoomed," a
decision made explicitly with the user before writing this plan (Task 5);
paste-a-multi-line-block sharing the import parser, "the same code path,
reached from the editor instead of from a file" (Task 6); bounded import,
rejected with a clear error rather than truncated silently (Tasks 4 and
5's size/count checks); the JSON `Exporter` joining `onsuite export`
(Task 2); "because done state and due dates are encoded, a document
round-trips" (Task 5's `TestExportThenImportRoundTrips`, exercised
end-to-end, not just asserted piecewise).

**Placeholder scan.** No TBDs; every step carries complete code. Several
steps explicitly ask the implementer to re-read a file before editing it
rather than trusting this plan's quoted text verbatim — `outline.html`,
`notes.js`, `README.md` and `notes.go`'s package doc are exactly the kind
of files that accumulate small edits between when a plan is written and
when it runs, as N7's own history (many `fix(notes)` commits after that
plan landed) already demonstrates.

**Type consistency.** `ParsedNode` (Task 4) is exactly what
`Ops.ImportUnder` (Task 5) consumes, field for field, and what
`TestExportThenImportRoundTrips` (Task 5) proves is the correct round-trip
of what `ExportMarkdown` (Task 3) produces. `Store.Export`'s `[]Node`
(Task 2) is consumed identically by `ExportMarkdown` (Task 3, Markdown)
and `App.Export`/`exportedNode` (Task 2, JSON) — one read, two independent
renderings, matching the architecture summary.

**Why this plan touches a platform file at all.** Every other N-chunk plan
in this project stays entirely inside `internal/apps/notes`. Task 1 is the
one exception, confirmed with the user before writing this plan: N8 is the
first chunk in the whole suite to need a real file upload, and doing that
correctly — specifically, having it work with JavaScript off, which spec
§2/§7 require of everything in this app — exposed a latent gap in
`web.CSRF.verify` that no existing route had ever exercised. The fix is
small, has its own tests independent of anything ON Notes does, and is
run against the whole repo's test suite (Task 1, Step 5) precisely because
it is shared code.

**Why byte-size bounds differ by transport instead of being one flat
number.** This plan originally carried a single "2 MB" limit, chosen
before discovering that `web.DefaultMaxBodyBytes` (1 MiB) already caps
every request body. `MaxImportFileBytes` (768 KiB) and `MaxPasteTextBytes`
(256 KiB) are sized for their own transport's actual worst case instead —
a multipart file part versus a url-encoded form field, which can inflate a
pasted block up to 3x for non-ASCII text.

**Why no handler in this plan wraps its own `r.Body` in
`http.MaxBytesReader`, even though that is the obvious naive pattern for
"bound an upload's size".** Covered in the Global Constraints section and
repeated at each call site: `CSRF.Middleware` runs before every handler
and already reads the body once (via `ParseForm` or, after Task 1,
`ParseMultipartForm`) looking for the token. By the time `importNotes` or
`paste` runs, `r.Body` is already consumed and cached into
`r.Form`/`r.PostForm`/`r.MultipartForm`; wrapping it in a fresh
`MaxBytesReader` at that point would never trigger, because nothing reads
through it again. Both bounds are therefore enforced by checking the
length/size of what has already been read, not by capping a read that has
already happened. A reviewer who expects to see `MaxBytesReader` in either
handler should read this note rather than treat its absence as an
oversight.

**Why `importNotes` and `paste` differ on whether they use `mutate`.**
Covered in Task 5's and Task 6's own design notes: `mutate` exists to
bundle a row's unsaved text with a structural operation issued from that
row, and importing a file from the toolbar has no such row in play, while
pasting into a field does.

**Why the paste route has no "not outline-shaped" fallback of its own.**
Covered in Task 6's design note: the shape decision is made once,
client-side, before the request is ever issued. A reviewer who expects
`ParseMarkdown` or the `paste` handler to gracefully degrade to "insert as
literal text" should note that no such request ever reaches the server —
the browser's own untouched default paste behaviour is that fallback.

**Why `ParseMarkdown` defers due-date semantic validation to
`Ops.SetDue`.** Stated in Task 4's own doc comment and pinned by
`TestParseMarkdownDoesNotSemanticallyValidateTheDueDate`: `ValidateDue`
already runs inside `Ops.ImportUnder`'s transaction, so checking the same
thing during parsing would be the identical logic run twice for no benefit
— the error still surfaces as a clean 400 either way, since the whole
import is one transaction (`TestImportUnderIsOneTransaction`).

## Follow-ups

1. **Literal text colliding with export syntax.** A title that genuinely
   ends in `" [x]"` or `" @2026-09-01"`-shaped text will be misparsed as
   syntax on export/import, exactly the same class of accepted,
   documented limitation `markdown.go`'s own inline renderer already
   carries for `#tag`/`@mention`/link syntax (N4). Not addressed here;
   escaping it would need its own design pass if it turns out to matter
   in practice.
2. **No progress indicator for a large import.** `MaxImportNodes` (5,000)
   bounds the work, but a file near that size still takes a moment inside
   one transaction with no feedback beyond the eventual outline swap.
   Acceptable at this size; worth revisiting only if real usage shows it
   matters.
3. **The paste heuristic and `ParseMarkdown`'s own rule can drift.** They
   are two independent implementations (JS regex, Go regex) of the same
   "looks like a bullet line" rule, chosen deliberately over exposing the
   Go parser to JavaScript (there is no build step to generate one from
   the other, and the two-way street of "the client decides whether to
   ask, the server decides authoritatively" is simpler than keeping a
   generated artifact in sync). A future change to one must remember the
   other; nothing enforces that today beyond this note and each one's own
   doc comment cross-referencing it.
4. **A request over the platform's 1 MiB global cap gets a 403, not a
   400.** Because `CSRF.Middleware` reads the body first, a request that
   exceeds `web.DefaultMaxBodyBytes` fails inside `formToken`'s own
   `ParseForm`/`ParseMultipartForm` call, which this plan's `verify`
   treats the same as "no token found" — a CSRF-shaped rejection rather
   than a clear "too large" one. This is pre-existing platform behaviour
   (Task 1 does not change what happens above the global cap, only what
   happens for a multipart body under it), out of scope for an app-level
   chunk, and only reachable by a request larger than 1 MiB in the first
   place — well above either of this chunk's own, tighter bounds.
