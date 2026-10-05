# ON Later L1a — save and read: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A working ON Later app: save a URL, keep a sanitised snapshot (or a
link-only item you can paste text into), browse Unread / Reading / Archived,
read an article with its images kept forever, archive and delete.

**Architecture:** A new app package `internal/apps/later` following ON Paste
and ON Reader's patterns (Store with its own clock, `apptest` handler tests,
server-rendered templates, plain POST + 303 redirects, one small CSP-clean
script). Fetching and extraction use the L0 platform packages
(`webfetch`, `article`). Images are content-addressed BLOBs, linked per
article, fetched on demand by the image route and back-filled by a job.

**Tech Stack:** Go, SQLite (modernc), `html/template`, HTMX (only for "Load
more"), `golang.org/x/net/html`, `internal/platform/{webfetch,article}`.

**Spec:** [2026-10-05-on-later-design.md](../specs/2026-10-05-on-later-design.md).
**Issue:** #482 (L1, first half). **Split:** the spec allows splitting L1;
this plan is L1a. **L1b** (separate plan, after this merges): favicons,
bookmarklet, ON Reader's "Read later" button, Aa settings, reading
progress/resume, the full-screen reading chrome, the row ⋯ menu.

**Deviations from the spec, decided here:**
1. **Image download.** The spec has a background download kicked off at
   save time. This plan uses the image route (fetch on first view, like
   Reader) plus a `download images` job every 10 minutes that back-fills
   anything not yet stored. Same outcome — every image ends up stored and is
   never expired — without goroutines in handlers (which race with test
   teardown and can't be tested deterministically).
2. **`content_text`.** The spec says block boundaries become `\n`. This
   plan defines `content_text` as the plain concatenation of the snapshot's
   text nodes — exactly what a browser's `textContent` returns for the
   rendered article — so L2's JavaScript can compute highlight offsets with
   no mirroring rules at all. Word count (and later search) use a variant
   that puts a space at element boundaries, so "end.</p><p>Next" counts as
   two words.

## Global Constraints

- App ID `later`; Name `ON Later`; Summary `Save articles and read them properly.`; Order `35`.
- Every table and index is prefixed `later_`; every `user_id` column is
  `REFERENCES users (id) ON DELETE CASCADE`; tables are `STRICT`.
- Timestamps are TEXT written with `db.FormatTime`, read with `db.ParseTime`.
  Never `time.RFC3339*`.
- App code reads time only through `Store.now()`; `internal/apps/later/store.go`
  is added to `TestAppsReadTheirStoreClock`'s list.
- Apps never import each other: `later` must not import `internal/apps/reader`.
- All outbound HTTP goes through `webfetch.Client`; all publisher HTML goes
  through `article.Extract` / `article.SanitizeWithImages`. Images in stored
  HTML always point at `/later/img/{hash}`.
- Every query is scoped to the signed-in user; another user's article or
  image is a 404, never a 403.
- No inline `<script>` or `style=` (CSP). JS lives in
  `internal/apps/later/static/later.js`, served by a route.
- CSS goes in `internal/ui/static/app.css` in a new `ON Later` section;
  every class is prefixed `later-`.
- No new module dependencies.
- Saving: tracking parameters dropped are exactly `utm_*`, `fbclid`, `gclid`,
  `mc_cid`, `mc_eid`, `ref_src`; the fragment is dropped; fetch+extract has a
  15-second limit.
- Reading time = `ceil(words / 230)` minutes, minimum 1.
- List paging: 50 rows, then "Load more".
- Commits: Conventional Commits, scope `later` (or `platform` for platform
  edits). Full check green after every task:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```

## File structure

```
internal/apps/later/
  later.go            App: Meta, Migrations, Templates, Mount, script route, Jobs
  store.go            Store, clock, errors, Article/ListItem types, article queries
  images_store.go     image + link queries
  text.go             plainText / ContentText / WordCount / ReadingMinutes / PastedHTML
  normalize.go        NormalizeURL
  save.go             fetchArticle (webfetch + article.Extract), page <title> fallback
  handlers.go         list, save, view, archive/unarchive, delete, paste text
  images.go           image route + download job
  migrations/0001_articles.sql
  templates/index.html      list page (+ "rows" block for Load more)
  templates/article.html    reading view
  templates/later.partial.html  confirm dialog
  static/later.js     confirm dialog for data-later-confirm forms
  *_test.go           (export_test.go for test hooks)
docs/user/later.md    short guide (full guide in L4)
```

---

### Task 1: App skeleton and registration

**Files:**
- Create: `internal/apps/later/later.go`, `internal/apps/later/store.go`,
  `internal/apps/later/migrations/0001_articles.sql`,
  `internal/apps/later/templates/index.html`,
  `internal/apps/later/handlers.go`, `internal/apps/later/handlers_test.go`,
  `docs/user/later.md`
- Modify: `cmd/onsuite/main.go` (`registeredApps`),
  `cmd/onsuite/database_test.go` (the two app-list tests),
  `internal/ui/icons.go`, `internal/platform/help/pages.go` (`order`),
  `docs/user/index.md`, `docs/screenshots/seed/seed.go` (registry list only),
  `internal/arch/arch_test.go` (`TestAppsReadTheirStoreClock` want list),
  `internal/ui/templates_test.go` (`appClassPattern`),
  `internal/ui/static/app.css` (new section header only)

**Interfaces — Produces:**
```go
package later
const ID = "later"
func New() *App
func Migrations() fs.FS
type Store struct{ /* db, now */ }
func NewStore(handle *sql.DB) *Store
func (st *Store) SetClock(now func() time.Time)
var ErrNotFound, ErrInvalid error
```

- [ ] **Step 1: Migration**

`internal/apps/later/migrations/0001_articles.sql`:
```sql
-- One row per saved URL. The snapshot never changes once an article is
-- readable, which is what keeps highlight offsets (L2) valid.
CREATE TABLE later_articles (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    url           TEXT    NOT NULL, -- normalised; see normalize.go
    title         TEXT    NOT NULL,
    site_name     TEXT    NOT NULL DEFAULT '',
    byline        TEXT    NOT NULL DEFAULT '',
    site_host     TEXT    NOT NULL,
    content       TEXT    NOT NULL CHECK (content IN ('extracted', 'pasted', 'link_only')),
    -- Sanitised by internal/platform/article; images point at /later/img/.
    content_html  TEXT    NOT NULL DEFAULT '',
    -- The concatenated text nodes of content_html (see text.go).
    content_text  TEXT    NOT NULL DEFAULT '',
    extract_error TEXT    NOT NULL DEFAULT '',
    word_count    INTEGER NOT NULL DEFAULT 0,
    state         TEXT    NOT NULL DEFAULT 'unread' CHECK (state IN ('unread', 'reading', 'archived')),
    note          TEXT    NOT NULL DEFAULT '',
    progress      REAL    NOT NULL DEFAULT 0,
    saved_at      TEXT    NOT NULL,
    opened_at     TEXT,
    archived_at   TEXT,
    updated_at    TEXT    NOT NULL,
    UNIQUE (user_id, url)
) STRICT;

CREATE INDEX later_articles_state_idx ON later_articles (user_id, state);

-- Images are content-addressed by webfetch.URLHash(source URL), so one image
-- shared by two articles is stored once. bytes is NULL until fetched.
CREATE TABLE later_images (
    hash         TEXT    PRIMARY KEY,
    src_url      TEXT    NOT NULL,
    content_type TEXT    NOT NULL DEFAULT '',
    bytes        BLOB,
    fetched_at   TEXT,
    last_error   TEXT    NOT NULL DEFAULT '',
    error_count  INTEGER NOT NULL DEFAULT 0
) STRICT;

-- Which articles use which images. Deleting an article deletes only the
-- images no other article links to (the lesson of Reader's migration 0005).
CREATE TABLE later_article_images (
    article_id INTEGER NOT NULL REFERENCES later_articles (id) ON DELETE CASCADE,
    hash       TEXT    NOT NULL REFERENCES later_images (hash) ON DELETE CASCADE,
    PRIMARY KEY (article_id, hash)
) STRICT, WITHOUT ROWID;

CREATE INDEX later_article_images_hash_idx ON later_article_images (hash);
```

- [ ] **Step 2: Store skeleton**

`internal/apps/later/store.go` (article queries arrive in Task 3):
```go
// Package later implements ON Later, a private read-it-later app: save a
// page, read it in a calm view, keep its images.
package later

import (
	"database/sql"
	"embed"
	"errors"
	"io/fs"
	"time"
)

// ID is the app id: URL prefix, migration namespace, table prefix.
const ID = "later"

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations is this app's schema, for the platform and for store tests.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		panic("later: embedded migrations missing: " + err.Error()) // unreachable
	}
	return sub
}

var (
	// ErrNotFound is a missing row or somebody else's — indistinguishable
	// on purpose, so a handler answers 404 for both.
	ErrNotFound = errors.New("later: not found")
	// ErrInvalid is a request the store refuses (bad state, bad input).
	ErrInvalid = errors.New("later: invalid")
)

// Store is every SQL query ON Later runs.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// NewStore returns a store on handle reading the real clock in UTC.
func NewStore(handle *sql.DB) *Store {
	return &Store{db: handle, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the time source, for tests.
func (st *Store) SetClock(now func() time.Time) { st.now = now }
```

- [ ] **Step 3: App, a placeholder index, and the guide**

`internal/apps/later/later.go`:
```go
package later

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

var _ app.App = (*App)(nil)

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed static/later.js
var scriptFiles embed.FS

// App is ON Later.
type App struct {
	store *Store
	deps  app.Deps
}

// New returns the app for registration in cmd/onsuite.
func New() *App { return &App{} }

func (a *App) Meta() app.Meta {
	return app.Meta{
		ID:      ID,
		Name:    "ON Later",
		Summary: "Save articles and read them properly.",
		Order:   35,
	}
}

func (a *App) Migrations() fs.FS { return Migrations() }

func (a *App) Templates() fs.FS {
	sub, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		panic("later: embedded templates missing: " + err.Error()) // unreachable
	}
	return sub
}

func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	a.store = NewStore(deps.DB)
	if deps.Now != nil {
		a.store.SetClock(deps.Now)
	}
	r.HandleFunc("GET /{$}", a.index)
	r.HandleFunc("GET /later.js", a.script)
}

// script serves later.js behind the same sign-in requirement as every
// other route, as Reader's reader.js is.
func (a *App) script(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, scriptFiles, "static/later.js")
}
```
Create `internal/apps/later/static/later.js` with just `"use strict";` for
now (Task 6 fills it).

`internal/apps/later/handlers.go`:
```go
package later

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// userID is the signed-in user; every route is registered with Handle.
func (a *App) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u, ok := web.UserFrom(r.Context())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusUnauthorized)
		return 0, false
	}
	return u.ID, true
}

func (a *App) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// fail maps a store error onto a response; mirrors Reader's own fail.
func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		a.deps.Errors.Status(w, r, http.StatusNotFound)
	case errors.Is(err, ErrInvalid):
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
	default:
		a.deps.Errors.Internal(w, r, err)
	}
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	page := a.deps.Page(r, "")
	if err := a.deps.Render.Page(w, http.StatusOK, "later/index", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
```

`internal/apps/later/templates/index.html` (placeholder; Task 5 replaces it):
```html
{{define "content"}}
<div class="later-page stack">
	<h1 class="visually-hidden">ON Later</h1>
	<p class="later-empty">Nothing saved yet.</p>
</div>
{{end}}
```

`docs/user/later.md` — a short guide (L4 writes the full one, with
screenshots). Plain Markdown, no raw HTML, no `../` links:
```markdown
# ON Later

ON Later keeps articles you want to read properly, away from the rush of
your feeds. Save a page, and ON Later keeps a clean copy of it — text and
pictures — so you can read it later, even if the original changes or
disappears.

## Saving an article

1. Copy the address of the page you want to keep.
2. In ON Later, paste it into **Paste a URL to save…** and click **Save**.

ON Later opens the saved article straight away.

Some pages can't be read this way — pages behind a paywall or a sign-in,
for example. ON Later still saves the link and tells you why it couldn't
read the page. Open the original, copy its text, and use **Paste text** to
make it a normal article you can read here.

## Finding your articles

Articles are in three lists:

- **Unread** — saved, not opened yet.
- **Reading** — opened at least once.
- **Archived** — finished. Click **Archive** when you're done with an
  article; **Move to unread** brings it back.

## Deleting an article

Open the article and click **Delete**. This can't be undone.
```
Link it from `docs/user/index.md`'s app list, after ON Reader, in the same
style: `- **[ON Later](later.md)** — save articles and read them properly.`
and change "four apps" in that file to "five apps".

- [ ] **Step 4: Register and pin**

- `cmd/onsuite/main.go`: import `github.com/iliafrenkel/on-suite/internal/apps/later` and add `later.New(),` to `registeredApps()` (alphabetical: after `flash.New()`).
- `cmd/onsuite/database_test.go`: update the two tests that pin the app list (`TestOpenDatabaseSkipsMigrationsForADisabledApp`, `TestOpenDatabaseExportBuildsAConfigLiteralSoDisablingNeverApplies`) to include `later` exactly the way they include the others.
- `internal/platform/help/pages.go` `order`: add `{"later", "ON Later"},` after `{"reader", "ON Reader"},`.
- `docs/screenshots/seed/seed.go`: add `later.New()` to the `app.NewRegistry(...)` call (no demo content yet — L4).
- `internal/arch/arch_test.go` `TestAppsReadTheirStoreClock`: add `"internal/apps/later/store.go",` to the want list (keep it sorted).
- `internal/ui/templates_test.go`: `appClassPattern` → `^(reader|notes|paste|flash|later)-` and its comment.
- `internal/ui/icons.go`: add, after `"reader"`:
  ```go
  	// A bookmark: something set aside to come back to.
  	"later": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
  		<rect x="2" y="2" width="20" height="20" rx="5" fill="none"/>
  		<path d="M8.5 6.5h7v11l-3.5-2.6-3.5 2.6z" fill="none" stroke="var(--c-accent)" stroke-width="1.5" stroke-linejoin="round"/>
  	</svg>`,
  ```
- `internal/ui/static/app.css`: append a section header in the file's
  existing style, e.g. `/* ===== ON Later ===== */`, with `.later-empty { color: var(--c-text-dim); }`.

- [ ] **Step 5: Write the failing handler tests**

`internal/apps/later/handlers_test.go`:
```go
package later_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/apptest"
)

type server = apptest.Server[*later.Store]

// newServer mounts ON Later with its own store as the test's handle.
func newServer(t *testing.T) (*server, *later.App) {
	t.Helper()
	a := later.New()
	s := apptest.NewServer(t, a, func(h *sql.DB) *later.Store { return later.NewStore(h) })
	return s, a
}

func TestLaterRequiresSignIn(t *testing.T) {
	s, _ := newServer(t)
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/later/", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /later/ anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

func TestIndexRendersForASignedInUser(t *testing.T) {
	s, _ := newServer(t)
	doc := s.Get(t, s.Alice, "/later/")
	doc.MustHave(".later-page")
}
```
(Check `apptest.NewServer`'s exact signature and how `s.Store` is typed in
`internal/apptest/apptest.go`; adapt `newServer` to it — Paste's
`handlers_test.go` `newServer` is the model.)

- [ ] **Step 6: Run, then make it pass**

Run: `go test ./internal/apps/later/... ./cmd/onsuite/... ./internal/arch/... ./internal/ui/... ./internal/platform/help/... -count=1`
Expected before Step 4 is complete: failures naming the missing registration/guide; after: PASS.

- [ ] **Step 7: Full check, then commit**

```bash
git add -A internal/apps/later cmd/onsuite internal/ui internal/platform/help internal/arch docs/user docs/screenshots/seed
git commit -m "feat(later): add the ON Later app skeleton"
```

---

### Task 2: URL normalisation and text helpers

**Files:**
- Create: `internal/apps/later/normalize.go`, `internal/apps/later/normalize_test.go`,
  `internal/apps/later/text.go`, `internal/apps/later/text_test.go`

**Interfaces — Produces:**
```go
func NormalizeURL(raw string) (string, error)        // ErrInvalid on bad input
func ContentText(fragment string) string              // concatenated text nodes
func WordCount(fragment string) int                   // words, element boundaries separate
func ReadingMinutes(words int) int                    // ceil(words/230), min 1
func PastedHTML(text string) string                   // escaped <p> per paragraph
```

- [ ] **Step 1: Failing tests**

`normalize_test.go`:
```go
package later_test

import (
	"errors"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
)

func TestNormalizeURL(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://Example.COM/a/b", "https://example.com/a/b"},
		{"  https://example.com/post#comments  ", "https://example.com/post"},
		{"https://example.com/p?utm_source=x&utm_medium=y&id=7", "https://example.com/p?id=7"},
		{"https://example.com/p?fbclid=1&gclid=2&mc_cid=3&mc_eid=4&ref_src=5", "https://example.com/p"},
		{"https://example.com/p?b=2&a=1", "https://example.com/p?a=1&b=2"},
		{"http://example.com/", "http://example.com/"},
	} {
		got, err := later.NormalizeURL(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "not a url", "ftp://example.com/x", "javascript:alert(1)", "https:///nohost", "/relative/path"} {
		if _, err := later.NormalizeURL(bad); !errors.Is(err, later.ErrInvalid) {
			t.Errorf("NormalizeURL(%q) err = %v, want ErrInvalid", bad, err)
		}
	}
}
```

`text_test.go`:
```go
package later_test

import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
)

func TestContentTextIsTheConcatenatedTextNodes(t *testing.T) {
	got := later.ContentText(`<h1>Title</h1><p>One <em>two</em> &amp; three.</p><p>Next</p>`)
	if want := "TitleOne two & three.Next"; got != want {
		t.Errorf("ContentText = %q, want %q", got, want)
	}
}

func TestContentTextKeepsMultiByteText(t *testing.T) {
	got := later.ContentText(`<p>Привет 👋 שלום</p>`)
	if got != "Привет 👋 שלום" {
		t.Errorf("ContentText = %q", got)
	}
}

func TestWordCountSeparatesAtElementBoundaries(t *testing.T) {
	if got := later.WordCount(`<p>end.</p><p>Next one</p>`); got != 3 {
		t.Errorf("WordCount = %d, want 3", got)
	}
}

func TestReadingMinutes(t *testing.T) {
	for words, want := range map[int]int{0: 1, 1: 1, 230: 1, 231: 2, 2300: 10} {
		if got := later.ReadingMinutes(words); got != want {
			t.Errorf("ReadingMinutes(%d) = %d, want %d", words, got, want)
		}
	}
}

func TestPastedHTMLEscapesAndSplitsParagraphs(t *testing.T) {
	got := later.PastedHTML("First <b>line</b>\nstill first\n\n\n  Second  \n")
	want := "<p>First &lt;b&gt;line&lt;/b&gt;\nstill first</p><p>Second</p>"
	if got != want {
		t.Errorf("PastedHTML = %q, want %q", got, want)
	}
	if strings.Contains(later.PastedHTML("   \n\n  "), "<p>") {
		t.Error("blank input produced paragraphs")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/apps/later/... -run 'Normalize|ContentText|WordCount|ReadingMinutes|PastedHTML'`
Expected: compile failure, functions undefined.

- [ ] **Step 3: Implement**

`normalize.go`:
```go
package later

import (
	"fmt"
	"net/url"
	"strings"
)

// trackingParams are dropped so the same article saved from two newsletters
// is one article. Exactly these, plus any utm_* parameter.
var trackingParams = map[string]bool{
	"fbclid": true, "gclid": true, "mc_cid": true, "mc_eid": true, "ref_src": true,
}

// NormalizeURL is the form a URL is stored and de-duplicated in: http(s)
// only, lower-case scheme and host, no fragment, no tracking parameters,
// remaining query parameters in a stable (sorted) order.
func NormalizeURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%w: %q is not an http(s) address", ErrInvalid, raw)
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	u.RawFragment = ""

	q := u.Query()
	for k := range q {
		if strings.HasPrefix(k, "utm_") || trackingParams[k] {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode() // sorted by key
	return u.String(), nil
}
```

`text.go`:
```go
package later

import (
	"html"
	"math"
	"regexp"
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// plainText concatenates the text nodes of an HTML fragment, writing sep
// after every element. With sep "" it is exactly the browser's textContent
// of the rendered fragment.
func plainText(fragment, sep string) string {
	ctx := &xhtml.Node{Type: xhtml.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := xhtml.ParseFragment(strings.NewReader(fragment), ctx)
	if err != nil {
		return ""
	}
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == xhtml.ElementNode {
			b.WriteString(sep)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return b.String()
}

// ContentText is the text highlight offsets (L2) index into: the snapshot's
// text nodes, concatenated, exactly as a browser's textContent reports them.
// Keeping it that simple is what lets the client compute the same offsets
// without mirroring any rules.
func ContentText(fragment string) string { return plainText(fragment, "") }

// WordCount counts words with element boundaries as separators, so the end
// of one paragraph and the start of the next are two words, not one.
func WordCount(fragment string) int { return len(strings.Fields(plainText(fragment, " "))) }

// wordsPerMinute is a comfortable reading pace for considered reading.
const wordsPerMinute = 230

// ReadingMinutes is the reading time shown in lists and the reader.
func ReadingMinutes(words int) int {
	return max(1, int(math.Ceil(float64(words)/wordsPerMinute)))
}

var blankLines = regexp.MustCompile(`\n\s*\n`)

// PastedHTML turns text the user pasted into a snapshot: one <p> per
// paragraph (blank-line separated), everything escaped. Nothing in it needs
// sanitising, because none of it is markup.
func PastedHTML(text string) string {
	var b strings.Builder
	for _, para := range blankLines.Split(strings.ReplaceAll(text, "\r\n", "\n"), -1) {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		b.WriteString("<p>")
		b.WriteString(html.EscapeString(para))
		b.WriteString("</p>")
	}
	return b.String()
}
```

- [ ] **Step 4: Run to confirm they pass**, then full check.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/later
git commit -m "feat(later): normalise saved URLs and derive article text"
```

---

### Task 3: The article store

**Files:**
- Modify: `internal/apps/later/store.go`
- Create: `internal/apps/later/images_store.go`, `internal/apps/later/store_test.go`

**Interfaces:**
- Consumes: `ContentText`, `WordCount`, `PastedHTML` (Task 2).
- Produces:
```go
type State string
const (StateUnread State = "unread"; StateReading State = "reading"; StateArchived State = "archived")
type Content string
const (ContentExtracted Content = "extracted"; ContentPasted Content = "pasted"; ContentLinkOnly Content = "link_only")

type NewArticle struct {
	URL, Title, SiteName, Byline string
	ContentHTML  string            // "" means link-only
	Images       map[string]string // hash -> source URL
	ExtractError string
}
type Article struct {
	ID, UserID                     int64
	URL, Title, SiteName, Byline   string
	SiteHost                       string
	Content                        Content
	ContentHTML, ContentText       string
	ExtractError                   string
	WordCount                      int
	State                          State
	Note                           string
	Progress                       float64
	SavedAt, UpdatedAt             time.Time
	OpenedAt, ArchivedAt           time.Time // zero when unset
}
type ListItem struct {
	ID          int64
	Title       string
	SiteHost    string
	Content     Content
	WordCount   int
	Progress    float64
}
func (st *Store) Save(ctx context.Context, userID int64, n NewArticle) (a Article, created bool, err error)
func (st *Store) Article(ctx context.Context, userID, id int64) (Article, error)
func (st *Store) ArticleByURL(ctx context.Context, userID int64, url string) (Article, error)
func (st *Store) List(ctx context.Context, userID int64, state State, offset, limit int) ([]ListItem, error)
func (st *Store) Counts(ctx context.Context, userID int64) (map[State]int, error)
func (st *Store) MarkOpened(ctx context.Context, userID, id int64) error
func (st *Store) SetState(ctx context.Context, userID, id int64, s State) error
func (st *Store) SetPastedText(ctx context.Context, userID, id int64, text string) error
func (st *Store) Delete(ctx context.Context, userID, id int64) error
// images_store.go
type Image struct { Hash, SrcURL, ContentType string; Bytes []byte; FetchedAt time.Time; ErrorCount int; LastError string }
func (i Image) Cached() bool
func (st *Store) ImageForUser(ctx context.Context, userID int64, hash string) (Image, error)
func (st *Store) SaveImageBytes(ctx context.Context, hash, contentType string, b []byte) error
func (st *Store) SaveImageFailure(ctx context.Context, hash, msg string) error
func (st *Store) ImagesToFetch(ctx context.Context, limit int) ([]Image, error)
```

Rules the code and tests must hold:
- `Save` with `ContentHTML != ""` stores `content='extracted'`, `ContentText(html)`, `WordCount(html)`; otherwise `content='link_only'`. `site_host` is the URL's host without a leading `www.`. Title falls back to `site_host` when empty. On a `(user_id, url)` conflict it changes nothing and returns the existing article with `created=false`. Image rows (`INSERT … ON CONFLICT DO NOTHING`) and links are written in the same transaction.
- `List` orders Unread by `saved_at DESC, id DESC`; Reading by `opened_at DESC, id DESC`; Archived by `archived_at DESC, id DESC`.
- `MarkOpened` sets `opened_at = now`; an Unread article becomes Reading; Reading/Archived keep their state.
- `SetState(StateArchived)` sets `archived_at = now`; `SetState(StateUnread)` clears `archived_at` and `opened_at`; `StateReading` via `SetState` is `ErrInvalid` (only opening does that). Unknown states are `ErrInvalid`.
- `SetPastedText` only on a `link_only` article (else `ErrInvalid`); empty text after `PastedHTML` is `ErrInvalid`; sets `content='pasted'`, `content_html`, `content_text`, `word_count`, clears `extract_error`.
- `Delete` in one transaction: collect the article's image hashes, delete the article (links cascade), delete those images that no remaining link references.
- Every method scoped by `user_id`; another user's id → `ErrNotFound`. Every write sets `updated_at = now`.
- `ImageForUser` returns the image only if one of *this user's* articles links it.
- `ImagesToFetch` returns images with `bytes IS NULL` and `error_count < webfetch.MaxImageAttempts`, then filters out `webfetch.GivenUp(errorCount, fetchedAt, now)` in Go, oldest-saved article first, up to `limit`.
- `SaveImageFailure` increments `error_count`, sets `last_error` and `fetched_at = now`. `SaveImageBytes` sets bytes, content type, `fetched_at = now`, clears errors.

- [ ] **Step 1: Failing store tests**

`store_test.go` — fixture copied from Paste's `store_test.go` `newFixture`
(real SQLite file in `t.TempDir()`, auth + later migrations via `db.Collect`
/ `db.Apply`, users alice and bob via `apptest.PasswordHash`), plus a fixed
clock:
```go
func newFixture(t *testing.T) *fixture {
	// … as Paste's, with later.ID / later.Migrations() …
	st := later.NewStore(handle)
	f := &fixture{store: st, db: handle, alice: alice, bob: bob, now: time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)}
	st.SetClock(func() time.Time { return f.now })
	return f
}
func (f *fixture) tick() { f.now = f.now.Add(time.Minute) }
```
Tests (each a separate `func Test…`, real assertions):
```go
func TestSaveStoresAnExtractedArticle(t *testing.T) {
	f := newFixture(t); ctx := context.Background()
	a, created, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{
		URL: "https://www.example.com/post", Title: "Post",
		ContentHTML: `<p>One two</p><p>three</p>`,
		Images: map[string]string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": "https://cdn.example/a.png"},
	})
	if err != nil || !created { t.Fatalf("Save = %v, created %v", err, created) }
	if a.Content != later.ContentExtracted || a.ContentText != "One twothree" || a.WordCount != 3 || a.SiteHost != "example.com" || a.State != later.StateUnread {
		t.Errorf("article = %+v", a)
	}
	if _, err := f.store.ImageForUser(ctx, f.alice.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Errorf("image not linked: %v", err)
	}
}
func TestSaveIsIdempotentPerUserAndURL(t *testing.T)        // second Save same URL: created=false, same ID, title unchanged; bob saving it creates his own
func TestSaveWithoutContentIsLinkOnly(t *testing.T)          // Content=link_only, ExtractError kept, title falls back to host when ""
func TestListOrdersEachTabAndPages(t *testing.T)             // 3 unread saved a minute apart -> newest first; offset/limit; opened -> Reading tab ordered by opened_at; archived ordered by archived_at
func TestCountsByState(t *testing.T)
func TestMarkOpenedMovesUnreadToReadingOnly(t *testing.T)    // archived stays archived, opened_at updated
func TestSetStateRules(t *testing.T)                          // archive sets ArchivedAt; unread clears OpenedAt+ArchivedAt; StateReading and "bogus" -> ErrInvalid
func TestSetPastedTextOnlyOnLinkOnly(t *testing.T)           // link_only -> pasted with ContentText/WordCount; extracted -> ErrInvalid; blank -> ErrInvalid
func TestEveryMethodIsScopedToTheOwner(t *testing.T)         // bob: Article, MarkOpened, SetState, SetPastedText, Delete on alice's id -> ErrNotFound; ImageForUser(bob, alice's hash) -> ErrNotFound
func TestDeleteKeepsSharedImagesAndRemovesOrphans(t *testing.T) // two articles share hash S, first also has hash O; delete first -> S still there for second, O gone (query later_images directly)
func TestImagesToFetchSkipsCachedAndGivenUp(t *testing.T)    // cached, 3 failures, 1 failure 5 min ago -> excluded; 1 failure 2h ago and never-fetched -> included
```
Write each with the same concreteness as the first: exact values, exact
expected fields.

- [ ] **Step 2: Run to confirm failure** — `go test ./internal/apps/later/... -run 'Save|List|Counts|Mark|SetState|Pasted|Scoped|Delete|ImagesToFetch'` → compile failure.

- [ ] **Step 3: Implement**

`store.go` additions (types as in Interfaces). Helper patterns:
```go
// articleColumns is every column scanArticle reads, in order.
const articleColumns = `id, user_id, url, title, site_name, byline, site_host, content,
	content_html, content_text, extract_error, word_count, state, note, progress,
	saved_at, opened_at, archived_at, updated_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanArticle(row rowScanner) (Article, error) {
	var a Article
	var saved, updated string
	var opened, archived sql.NullString
	err := row.Scan(&a.ID, &a.UserID, &a.URL, &a.Title, &a.SiteName, &a.Byline, &a.SiteHost,
		&a.Content, &a.ContentHTML, &a.ContentText, &a.ExtractError, &a.WordCount, &a.State,
		&a.Note, &a.Progress, &saved, &opened, &archived, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Article{}, ErrNotFound
	}
	if err != nil {
		return Article{}, fmt.Errorf("later: load article: %w", err)
	}
	if a.SavedAt, err = db.ParseTime(saved); err != nil {
		return Article{}, err
	}
	if a.UpdatedAt, err = db.ParseTime(updated); err != nil {
		return Article{}, err
	}
	if opened.Valid {
		if a.OpenedAt, err = db.ParseTime(opened.String); err != nil {
			return Article{}, err
		}
	}
	if archived.Valid {
		if a.ArchivedAt, err = db.ParseTime(archived.String); err != nil {
			return Article{}, err
		}
	}
	return a, nil
}

// siteHost is what the list shows as the site: the host, minus "www.".
func siteHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}
```
`Save`:
```go
func (st *Store) Save(ctx context.Context, userID int64, n NewArticle) (Article, bool, error) {
	now := db.FormatTime(st.now())
	host := siteHost(n.URL)
	title := strings.TrimSpace(n.Title)
	if title == "" {
		title = host
	}
	content, text, words := ContentLinkOnly, "", 0
	if n.ContentHTML != "" {
		content, text, words = ContentExtracted, ContentText(n.ContentHTML), WordCount(n.ContentHTML)
	}

	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return Article{}, false, fmt.Errorf("later: begin save: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO later_articles (user_id, url, title, site_name, byline, site_host, content,
			content_html, content_text, extract_error, word_count, saved_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, url) DO NOTHING`,
		userID, n.URL, title, n.SiteName, n.Byline, host, content,
		n.ContentHTML, text, n.ExtractError, words, now, now)
	if err != nil {
		return Article{}, false, fmt.Errorf("later: save article: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		_ = tx.Rollback()
		a, err := st.ArticleByURL(ctx, userID, n.URL)
		return a, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Article{}, false, err
	}
	for hash, src := range n.Images {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO later_images (hash, src_url) VALUES (?, ?) ON CONFLICT (hash) DO NOTHING`, hash, src); err != nil {
			return Article{}, false, fmt.Errorf("later: save image: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO later_article_images (article_id, hash) VALUES (?, ?) ON CONFLICT DO NOTHING`, id, hash); err != nil {
			return Article{}, false, fmt.Errorf("later: link image: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Article{}, false, fmt.Errorf("later: commit save: %w", err)
	}
	a, err := st.Article(ctx, userID, id)
	return a, true, err
}
```
`List`:
```go
// listOrder is each tab's sort; the keys are the only states List accepts.
var listOrder = map[State]string{
	StateUnread:   "saved_at DESC, id DESC",
	StateReading:  "opened_at DESC, id DESC",
	StateArchived: "archived_at DESC, id DESC",
}

func (st *Store) List(ctx context.Context, userID int64, state State, offset, limit int) ([]ListItem, error) {
	order, ok := listOrder[state]
	if !ok {
		return nil, ErrInvalid
	}
	rows, err := st.db.QueryContext(ctx, `
		SELECT id, title, site_host, content, word_count, progress
		  FROM later_articles
		 WHERE user_id = ? AND state = ?
		 ORDER BY `+order+`
		 LIMIT ? OFFSET ?`, userID, state, limit, offset)
	// … scan into []ListItem, rows.Err() …
}
```
(The `ORDER BY` string comes only from the `listOrder` map, never from
input.) `Counts`: `SELECT state, count(*) … GROUP BY state`, missing states
are 0. `MarkOpened`: `UPDATE … SET opened_at = ?, updated_at = ?, state =
CASE state WHEN 'unread' THEN 'reading' ELSE state END WHERE id = ? AND
user_id = ?`, `RowsAffected()==0` → `ErrNotFound`. `SetState`,
`SetPastedText`, `Delete` per the rules above, each checking
`RowsAffected` for `ErrNotFound` (for `SetPastedText`, first load the
article with `Article` to distinguish `ErrNotFound` from a non-link-only
`ErrInvalid`).

`Delete`:
```go
func (st *Store) Delete(ctx context.Context, userID, id int64) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("later: begin delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT ai.hash FROM later_article_images ai
		  JOIN later_articles a ON a.id = ai.article_id
		 WHERE a.id = ? AND a.user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("later: list article images: %w", err)
	}
	var hashes []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			_ = rows.Close()
			return err
		}
		hashes = append(hashes, h)
	}
	if err := rows.Close(); err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM later_articles WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("later: delete article: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	for _, h := range hashes {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM later_images WHERE hash = ?
			   AND NOT EXISTS (SELECT 1 FROM later_article_images WHERE hash = ?)`, h, h); err != nil {
			return fmt.Errorf("later: delete orphan image: %w", err)
		}
	}
	return tx.Commit()
}
```
`images_store.go`:
```go
// ImageForUser loads an image only if one of userID's articles uses it, so
// one person's saved images are never served to another.
func (st *Store) ImageForUser(ctx context.Context, userID int64, hash string) (Image, error) {
	row := st.db.QueryRowContext(ctx, `
		SELECT i.hash, i.src_url, i.content_type, i.bytes, i.fetched_at, i.error_count, i.last_error
		  FROM later_images i
		 WHERE i.hash = ?
		   AND EXISTS (SELECT 1 FROM later_article_images ai
		                 JOIN later_articles a ON a.id = ai.article_id
		                WHERE ai.hash = i.hash AND a.user_id = ?)`, hash, userID)
	return scanImage(row)
}
```
`ImagesToFetch`: select `bytes IS NULL AND error_count < ?` (bind
`webfetch.MaxImageAttempts`) ordered by the earliest linked article's
`saved_at`, with a SQL `LIMIT` of `limit*4`, then filter
`!webfetch.GivenUp(...)` in Go and trim to `limit`.

- [ ] **Step 4: Run to confirm they pass**, then full check.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/later
git commit -m "feat(later): store saved articles and their images"
```

---

### Task 4: Saving — fetch, extract, link-only

**Files:**
- Modify: `internal/platform/article/extract.go` (+ test) — add `Byline`, `SiteName` to `Extracted`
- Create: `internal/apps/later/save.go`, `internal/apps/later/save_test.go`,
  `internal/apps/later/export_test.go`
- Modify: `internal/apps/later/later.go` (client, route), `handlers.go` (`save`)

**Interfaces:**
- Consumes: `webfetch.New`, `webfetch.Config`, `webfetch.GetOptions`,
  `webfetch.MaxPageBytes`, `article.Extract`, `article.ErrNotExtractable`,
  `NormalizeURL`, `Store.Save`, `Store.ArticleByURL`.
- Produces:
```go
// article package
type Extracted struct { HTML, Title, Byline, SiteName string; Images map[string]string; TextLength int }
// later package
const ImagePathPrefix = "/later/img/"
var saveTimeout = 15 * time.Second
func (a *App) fetchArticle(ctx context.Context, pageURL string) NewArticle
// test hooks (export_test.go)
func (a *App) AllowPrivateFetchesForTest()
func SetSaveTimeoutForTest(d time.Duration) (restore func())
```
Route: `POST /later/save` (form field `url`) → 303 to `/later/a/{id}`;
an already-saved URL → 303 to `/later/a/{id}?existing=1`; an invalid URL →
422 re-rendering the list page with the message
`That doesn't look like a web address. It needs to start with http:// or https://.`
in the save form.

- [ ] **Step 1: Extend `article.Extracted`**

In `internal/platform/article/extract.go` add to `Extracted`:
```go
	// Byline and SiteName are what readability found, for callers that show
	// them (ON Later does; ON Reader doesn't).
	Byline   string
	SiteName string
```
and set them in `Extract`: `Byline: strings.TrimSpace(page.Byline), SiteName: strings.TrimSpace(page.SiteName)`.
Add to `internal/platform/article/extract_test.go` a test that a page with
`<meta property="og:site_name" content="Example Essays">` and a byline
element (`<span class="author">Jane Writer</span>` inside the article)
yields those values (check what readability actually picks up and assert
that; if the byline heuristic needs `rel="author"`, use that).
Run `go test ./internal/platform/article/...` red→green.

- [ ] **Step 2: Failing save tests**

`internal/apps/later/export_test.go`:
```go
package later

import (
	"net"
	"net/netip"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// AllowPrivateFetchesForTest lets the client reach loopback only, so tests
// can use httptest origins; mirrors Reader's own hook. Call after Mount.
func (a *App) AllowPrivateFetchesForTest() {
	a.client.DenyAddr = func(address string) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return webfetch.ErrBlockedAddress
		}
		if ip, err := netip.ParseAddr(host); err == nil && ip.Unmap().IsLoopback() {
			return nil
		}
		return webfetch.ErrBlockedAddress
	}
}

// SetSaveTimeoutForTest overrides the fetch-and-extract deadline.
func SetSaveTimeoutForTest(d time.Duration) (restore func()) {
	prev := saveTimeout
	saveTimeout = d
	return func() { saveTimeout = prev }
}
```
`internal/apps/later/save_test.go` — an `articlePage` fixture of ~300 words
of real prose in `<article>` with a `<title>Essay | Example</title>`, one
`<img src="/pic.png">` and a `<script>` that must not survive; served by
`httptest.NewServer`. Tests:
```go
func TestSaveExtractsAndRedirectsToTheArticle(t *testing.T)
	// POST /later/save url=<srv>/essay?utm_source=x -> 303 Location /later/a/{id}
	// store: Content=extracted, URL has no utm_source, ContentHTML has no "<script",
	// contains `src="/later/img/`, WordCount > 250
func TestSavingTheSameURLAgainGoesToTheExistingArticle(t *testing.T)
	// second POST (with #fragment) -> 303 /later/a/{same id}?existing=1, still one row
func TestSaveKeepsALinkOnlyItemWhenExtractionFails(t *testing.T)
	// origin serves "<html><head><title>Members only</title></head><body><p>Sign in</p></body></html>"
	// -> 303 to article; Content=link_only, Title "Members only", ExtractError non-empty
func TestSaveKeepsALinkOnlyItemWhenTheFetchFails(t *testing.T)
	// origin returns 404 -> link_only, Title = host (127.0.0.1), ExtractError mentions 404
func TestSaveKeepsALinkOnlyItemForNonHTML(t *testing.T)
	// origin serves application/pdf -> link_only, ExtractError mentions the content type
func TestSaveGivesUpAfterTheTimeout(t *testing.T)
	// SetSaveTimeoutForTest(50ms); origin sleeps 500ms -> link_only, still 303
func TestSaveRejectsAnInvalidURL(t *testing.T)
	// url="ftp://x" -> 422, page shows the exact message, no rows
func TestSaveRefusesPrivateAddressesWithTheRealGuard(t *testing.T)
	// no AllowPrivateFetchesForTest; url=http://127.0.0.1:1/x -> 303, link_only,
	// ExtractError mentions "blocked address"
func TestSaveIsPerUser(t *testing.T)
	// alice and bob save the same URL -> two rows, each 303s to their own id
```
Use `s.Post(t, s.Alice, "/later/save", url.Values{"url": {…}})` and read
`rec.Header().Get("Location")`. Read rows back through `s.Store`.

- [ ] **Step 3: Run to confirm failure** — compile failure (`AllowPrivateFetchesForTest`, route missing).

- [ ] **Step 4: Implement**

`later.go`: add `client *webfetch.Client` to `App`; in `Mount`:
```go
	a.client = webfetch.New(webfetch.Config{
		UserAgent:       "onsuite/" + deps.Version + " (ON Later; +https://github.com/iliafrenkel/on-suite)",
		DefaultAccept:   "text/html, application/xhtml+xml;q=0.9, */*;q=0.5",
		DefaultMaxBytes: webfetch.MaxPageBytes,
	})
	r.HandleFunc("POST /save", a.save)
```
`save.go`:
```go
package later

import (
	"bytes"
	"context"
	"fmt"
	"mime"
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/article"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ImagePathPrefix is where Later serves stored images, as it appears in
// rewritten HTML.
const ImagePathPrefix = "/later/img/"

func laterImageSrc(hash string) string { return ImagePathPrefix + hash }

// saveTimeout bounds fetch + extract. Saving is synchronous so the user
// knows at once whether the page was readable.
var saveTimeout = 15 * time.Second

// fetchArticle fetches and extracts pageURL. It never fails: anything that
// goes wrong becomes a link-only NewArticle carrying the reason, because a
// saved link the user can paste text into is better than an error.
func (a *App) fetchArticle(ctx context.Context, pageURL string) NewArticle {
	ctx, cancel := context.WithTimeout(ctx, saveTimeout)
	defer cancel()

	n := NewArticle{URL: pageURL}
	res, err := a.client.Get(ctx, pageURL, webfetch.GetOptions{})
	if err != nil {
		n.ExtractError = "Couldn't fetch the page: " + err.Error()
		return n
	}
	n.Title = pageTitle(res.Body)
	if !isHTML(res.ContentType) {
		n.ExtractError = fmt.Sprintf("The page isn't HTML (%s).", res.ContentType)
		return n
	}
	ex, err := article.Extract(res.Body, res.FinalURL, laterImageSrc)
	if err != nil {
		n.ExtractError = "Couldn't find an article on the page: " + err.Error()
		return n
	}
	if ex.Title != "" {
		n.Title = ex.Title
	}
	n.Byline, n.SiteName = ex.Byline, ex.SiteName
	n.ContentHTML, n.Images = ex.HTML, ex.Images
	return n
}

func isHTML(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && (mt == "text/html" || mt == "application/xhtml+xml")
}

// pageTitle is the document's <title>, for link-only items. Best effort.
func pageTitle(body []byte) string {
	doc, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	var title string
	var walk func(*xhtml.Node) bool
	walk = func(n *xhtml.Node) bool {
		if n.Type == xhtml.ElementNode && n.DataAtom == atom.Title && n.FirstChild != nil {
			title = strings.TrimSpace(n.FirstChild.Data)
			return true
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if walk(c) {
				return true
			}
		}
		return false
	}
	walk(doc)
	return strings.Join(strings.Fields(title), " ")
}
```
(Fetch-failure `Title` stays empty, so `Store.Save` falls back to the host.)

`handlers.go` — `save`:
```go
// badURLMessage is shown in the save form for a URL Later can't use.
const badURLMessage = "That doesn't look like a web address. It needs to start with http:// or https://."

func (a *App) save(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	pageURL, err := NormalizeURL(r.PostFormValue("url"))
	if err != nil {
		a.renderIndex(w, r, userID, StateUnread, http.StatusUnprocessableEntity, badURLMessage, r.PostFormValue("url"))
		return
	}
	if existing, err := a.store.ArticleByURL(r.Context(), userID, pageURL); err == nil {
		http.Redirect(w, r, fmt.Sprintf("/later/a/%d?existing=1", existing.ID), http.StatusSeeOther)
		return
	} else if !errors.Is(err, ErrNotFound) {
		a.fail(w, r, err)
		return
	}
	n := a.fetchArticle(r.Context(), pageURL)
	saved, created, err := a.store.Save(r.Context(), userID, n)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	target := fmt.Sprintf("/later/a/%d", saved.ID)
	if !created {
		target += "?existing=1"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
```
`renderIndex(w, r, userID, tab, status, formError, formValue)` is built in
Task 5; for this task, add a minimal version that renders `later/index`
with `Data` carrying `FormError`/`FormValue` and have the template show
`{{with .Data.FormError}}<p class="later-form-error" role="alert">{{.}}</p>{{end}}`.
Task 5 grows it.

- [ ] **Step 5: Run to confirm they pass**, then full check.

- [ ] **Step 6: Commit** (two commits)

```bash
git add internal/platform/article
git commit -m "feat(platform): report byline and site name from article extraction"
git add internal/apps/later
git commit -m "feat(later): save a URL as an article or a link-only item"
```

---

### Task 5: The list page

**Files:**
- Modify: `internal/apps/later/handlers.go` (`index`, `renderIndex`),
  `internal/apps/later/templates/index.html`, `internal/ui/static/app.css`,
  `internal/apps/later/handlers_test.go`

**Interfaces:**
- Consumes: `Store.List`, `Store.Counts`, `ReadingMinutes`.
- Produces: `GET /later/?tab=unread|reading|archived&offset=N`; over HTMX
  (`web.IsHTMX(r)` and not `web.IsHTMXHistoryRestore(r)`) with `offset>0`
  it renders only the `rows` block. `const pageSize = 50`.

View model (in `handlers.go`):
```go
const pageSize = 50

type tabView struct {
	State State
	Label string
	Count int
	Current bool
}

type rowView struct {
	ID       int64
	Title    string
	Site     string
	Minutes  int
	LinkOnly bool
	Progress int // percent, 0-100
}

type indexView struct {
	Tabs       []tabView
	Tab        State
	Rows       []rowView
	NextOffset int // 0 when there are no more rows
	FormError  string
	FormValue  string
	EmptyText  string
}
```
Empty texts: Unread `Nothing to read. Paste a URL above to save an article.`;
Reading `Nothing in progress.`; Archived `Nothing archived yet.`.
An unknown `tab` value falls back to Unread. `NextOffset` is set by asking
`List` for `pageSize+1` rows and trimming.

- [ ] **Step 1: Failing handler tests**

Seed rows through `s.Store.Save` / `MarkOpened` / `SetState`. Tests:
```go
func TestIndexShowsTabsWithCounts(t *testing.T)
	// 2 unread, 1 reading, 1 archived -> tabs "Unread 2", "Reading 1", "Archived 1";
	// Unread has aria-current="page"
func TestIndexListsTheTabsArticles(t *testing.T)
	// ?tab=archived shows only the archived title; each row links to /later/a/{id}
func TestIndexRowShowsSiteMinutesAndLinkOnlyPill(t *testing.T)
	// extracted 460 words -> "2 min"; link-only row has .later-pill-linkonly with "link only — add text"
func TestIndexShowsTheEmptyText(t *testing.T)
func TestIndexPagesWithLoadMore(t *testing.T)
	// 51 unread -> 50 .later-row and a "Load more" button with hx-get="/later/?tab=unread&offset=50";
	// GET that URL with HX-Request -> fragment with 1 row and no Load more
func TestIndexNeverShowsAnotherUsersArticles(t *testing.T)
func TestIndexHasTheSaveForm(t *testing.T)
	// form[action=/later/save][method=post] with input[name=url] and the CSRF field
```
For the HTMX request use `s.Do` with `HX-Request: true` on an
`httptest.NewRequest("GET", …)`; check `apptest` for an existing helper
first.

- [ ] **Step 2: Run to confirm failure.**

- [ ] **Step 3: Implement** `index` (parse `tab`, `offset` ≥ 0), `renderIndex`
(builds `indexView`; full page via `Render.Page`, rows-only via
`Render.Fragment(w, status, "later/index", "rows", page)`).

`templates/index.html`:
```html
{{define "head"}}<script src="/later/later.js" defer></script>{{end}}

{{define "content"}}
<div class="later-page stack">
	<h1 class="visually-hidden">ON Later</h1>

	<form class="later-save" method="post" action="/later/save">
		<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
		<label class="visually-hidden" for="later-url">Address of the page to save</label>
		<input id="later-url" class="later-save-input" type="url" name="url" required
		       placeholder="Paste a URL to save…" value="{{.Data.FormValue}}">
		<button class="button button-primary" type="submit">Save</button>
	</form>
	{{with .Data.FormError}}<p class="later-form-error" role="alert">{{.}}</p>{{end}}

	<nav class="later-tabs" aria-label="Lists">
		{{range .Data.Tabs}}
		<a class="later-tab" href="/later/?tab={{.State}}"{{if .Current}} aria-current="page"{{end}}>{{.Label}} <span class="later-tab-count">{{.Count}}</span></a>
		{{end}}
	</nav>

	{{if .Data.Rows}}
	<ul class="later-rows">{{template "rows" .}}</ul>
	{{else}}
	<p class="later-empty">{{.Data.EmptyText}}</p>
	{{end}}
</div>
{{end}}

{{define "rows"}}
{{range .Data.Rows}}
<li class="later-row">
	<a class="later-row-link" href="/later/a/{{.ID}}">
		<span class="later-row-title">{{.Title}}</span>
		<span class="later-row-meta">
			<span>{{.Site}}</span>
			{{if .LinkOnly}}<span class="later-pill later-pill-linkonly">link only — add text</span>
			{{else}}<span>{{.Minutes}} min</span>{{end}}
		</span>
	</a>
	{{if .Progress}}<progress class="later-row-progress" max="100" value="{{.Progress}}">{{.Progress}}%</progress>{{end}}
</li>
{{end}}
{{if .Data.NextOffset}}
<li class="later-more">
	<button class="button" type="button"
	        hx-get="/later/?tab={{.Data.Tab}}&amp;offset={{.Data.NextOffset}}"
	        hx-target="closest li" hx-swap="outerHTML">Load more</button>
</li>
{{end}}
{{end}}
```
(Check whether `Render.Fragment` passes the whole `render.Page` or only
`Data`; Reader's `handlers.go` uses it — follow that and adjust `.Data.`
prefixes in the `rows` block accordingly. Remove the `head` block's script
line in this task if `later.js` is still empty — Task 6 adds it.)

CSS (`app.css`, ON Later section) — use existing tokens only:
```css
.later-page { max-width: var(--measure); margin-inline: auto; }
.later-save { display: flex; gap: .5rem; }
.later-save-input { flex: 1; min-width: 0; }
.later-form-error { color: var(--c-danger); margin: 0; }
.later-tabs { display: inline-flex; border: var(--border); border-radius: var(--radius); overflow: hidden; }
.later-tab { padding: .25rem .75rem; color: var(--c-text-dim); text-decoration: none; }
.later-tab[aria-current="page"] { background: var(--c-accent-bg); color: var(--c-accent); font-weight: 500; }
.later-tab-count { font-size: var(--fs-xs); }
.later-rows { list-style: none; margin: 0; padding: 0; }
.later-row { display: flex; align-items: center; gap: .75rem; border-bottom: var(--border); }
.later-row-link { flex: 1; min-width: 0; display: block; padding: .6rem .25rem; color: inherit; text-decoration: none; }
.later-row-title { display: block; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.later-row-meta { display: flex; gap: .5rem; font-size: var(--fs-sm); color: var(--c-text-faint); }
.later-pill { border-radius: 999px; padding: 0 .5rem; font-size: var(--fs-xs); background: var(--c-bg-inset); color: var(--c-text-dim); }
.later-pill-linkonly { background: var(--c-accent-attention-bg); color: var(--c-accent-attention); }
.later-row-progress { width: 3rem; height: .25rem; }
.later-more { list-style: none; padding: .75rem 0; text-align: center; }
```

- [ ] **Step 4: Run to confirm they pass**, full check, and look at it:
build, run with a throwaway data dir, save two URLs, check the list in
light and dark mode and at 375px width (no horizontal scroll).

- [ ] **Step 5: Commit**

```bash
git add internal/apps/later internal/ui/static/app.css
git commit -m "feat(later): list saved articles by Unread, Reading and Archived"
```

---

### Task 6: The reading view, archive, delete and paste text

**Files:**
- Create: `internal/apps/later/templates/article.html`,
  `internal/apps/later/templates/later.partial.html`
- Modify: `internal/apps/later/handlers.go`, `internal/apps/later/later.go`
  (routes), `internal/apps/later/static/later.js`,
  `internal/apps/later/templates/index.html` (head script),
  `internal/ui/static/app.css`, `internal/apps/later/handlers_test.go`

**Interfaces:**
- Consumes: `Store.Article`, `MarkOpened`, `SetState`, `SetPastedText`,
  `Delete`, `ReadingMinutes`.
- Produces routes:
  - `GET /later/a/{id}` — reading view; marks opened.
  - `POST /later/a/{id}/archive` → 303 `/later/?tab=archived`
  - `POST /later/a/{id}/unarchive` → 303 `/later/a/{id}`
  - `POST /later/a/{id}/delete` → 303 `/later/?tab=<state the article was in>`
  - `POST /later/a/{id}/text` (field `text`) → 303 `/later/a/{id}`; blank → 422 with message `Paste some text first.`

View model:
```go
type articleView struct {
	ID        int64
	Title     string
	URL       string
	Site      string
	Byline    string
	Minutes   int
	SavedAt   time.Time
	Body      template.HTML // the only template.HTML conversion in the app
	LinkOnly  bool
	Reason    string        // ExtractError, for link-only
	Archived  bool
	Existing  bool          // ?existing=1: "You saved this before."
	TextError string
}
```
`Body` is `template.HTML(a.ContentHTML)` — safe because `ContentHTML` is
either `article.SanitizeWithImages` output or `PastedHTML` output (escaped);
say so in a comment on the field.

- [ ] **Step 1: Failing handler tests**

```go
func TestArticleRendersTheSnapshot(t *testing.T)
	// saved extracted article -> h1 title, .later-article-body contains the <p> text,
	// "Open original" link a[href=<url>][rel~=noopener], site and "N min read"
func TestOpeningAnUnreadArticleMovesItToReading(t *testing.T)
func TestArticleOfAnotherUserIs404(t *testing.T)            // GET, archive, unarchive, delete, text — all 404 for bob
func TestArticleSaysWhenItWasSavedBefore(t *testing.T)      // ?existing=1 -> "You saved this before."
func TestLinkOnlyArticleOffersPasteText(t *testing.T)
	// shows "Couldn't read this page" + the reason, Open original, form[action=/later/a/{id}/text] textarea[name=text]
func TestPastingTextMakesItReadable(t *testing.T)
	// POST text "Para one\n\nPara two" -> 303; GET shows two <p>, no paste form
func TestPastingBlankTextIs422(t *testing.T)
func TestArchiveAndUnarchive(t *testing.T)
	// archive -> 303 /later/?tab=archived and state archived; page then shows "Move to unread";
	// unarchive -> state unread
func TestDeleteRemovesTheArticle(t *testing.T)
	// delete -> 303 /later/?tab=<prev state>; GET -> 404; form carries data-later-confirm
func TestArticleBodyKeepsOnlyStoredHTML(t *testing.T)
	// a stored ContentHTML of `<p>x</p>` renders exactly that inside .later-article-body
	// (proves no double-escaping; sanitising is the save path's job and is tested there)
```

- [ ] **Step 2: Run to confirm failure.**

- [ ] **Step 3: Implement** the handlers (each: `userID`, `pathID`, store
call, `fail` on error, 303 on success; `view` calls `MarkOpened` before
loading so the page reflects the new state).

`templates/article.html`:
```html
{{define "head"}}<script src="/later/later.js" defer></script>{{end}}

{{define "content"}}
<article class="later-article stack">
	<header class="later-article-header">
		<h1 class="later-article-title">{{.Data.Title}}</h1>
		<p class="later-article-meta">
			{{.Data.Site}}{{with .Data.Byline}} · {{.}}{{end}}
			{{if not .Data.LinkOnly}} · {{.Data.Minutes}} min read{{end}}
		</p>
		{{if .Data.Existing}}<p class="later-note" role="status">You saved this before.</p>{{end}}
		<div class="later-article-actions">
			<a class="button" href="{{.Data.URL}}" target="_blank" rel="noopener noreferrer">Open original</a>
			{{if .Data.Archived}}
			<form method="post" action="/later/a/{{.Data.ID}}/unarchive">
				<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
				<button class="button" type="submit">Move to unread</button>
			</form>
			{{else}}
			<form method="post" action="/later/a/{{.Data.ID}}/archive">
				<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
				<button class="button button-primary" type="submit">Archive</button>
			</form>
			{{end}}
			<form method="post" action="/later/a/{{.Data.ID}}/delete"
			      data-later-confirm="Delete this article permanently? This can't be undone.">
				<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
				<button class="button button-danger" type="submit">Delete</button>
			</form>
		</div>
	</header>

	{{if .Data.LinkOnly}}
	<section class="later-linkonly stack" aria-labelledby="later-linkonly-title">
		<h2 id="later-linkonly-title">Couldn't read this page</h2>
		<p>{{.Data.Reason}}</p>
		<p>Open the original, copy the article's text, and paste it here to read it in ON Later.</p>
		<form class="stack" method="post" action="/later/a/{{.Data.ID}}/text">
			<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
			<label for="later-text">Article text</label>
			<textarea id="later-text" name="text" rows="12" required></textarea>
			{{with .Data.TextError}}<p class="later-form-error" role="alert">{{.}}</p>{{end}}
			<button class="button button-primary" type="submit">Paste text</button>
		</form>
	</section>
	{{else}}
	<div class="later-article-body">{{.Data.Body}}</div>
	{{end}}
</article>
{{template "later-confirm"}}
{{end}}
```
(Check the exact button class names in `app.css` — `button-primary` /
`button-danger` or whatever the suite uses — and use those.)

`templates/later.partial.html`:
```html
{{define "later-confirm"}}
<dialog id="later-confirm-dialog" class="later-dialog">
	<p id="later-confirm-message"></p>
	<div class="dialog-actions">
		<button type="button" id="later-confirm-ok" class="button button-danger">Delete</button>
		<button type="button" id="later-confirm-cancel" class="button">Cancel</button>
	</div>
</dialog>
{{end}}
```

`static/later.js` (replace the placeholder):
```js
// ON Later's only script. Forms marked data-later-confirm ask first, in the
// app's own dialog. Without JavaScript the form simply submits.
"use strict";

(function () {
	document.addEventListener("submit", function (e) {
		var form = e.target;
		if (!(form instanceof HTMLFormElement) || !form.dataset.laterConfirm) return;
		if (form.dataset.laterConfirmed === "1") return;

		var dialog = document.getElementById("later-confirm-dialog");
		if (!dialog || typeof dialog.showModal !== "function") return;

		e.preventDefault();
		document.getElementById("later-confirm-message").textContent = form.dataset.laterConfirm;

		// Listeners are tied to this one opening of the dialog, so a cancelled
		// confirmation can never fire later (the bug reader.js documents).
		var controller = new AbortController();
		dialog.addEventListener("close", function () { controller.abort(); }, { once: true });
		document.getElementById("later-confirm-ok").addEventListener("click", function () {
			form.dataset.laterConfirmed = "1";
			dialog.close();
			form.requestSubmit();
		}, { signal: controller.signal });
		document.getElementById("later-confirm-cancel").addEventListener("click", function () {
			dialog.close();
		}, { signal: controller.signal });
		dialog.showModal();
	});
})();
```
Add the `head` block script to `index.html` too if Task 5 left it out.

CSS additions:
```css
.later-article { max-width: var(--measure); margin-inline: auto; }
.later-article-title { font-size: 2rem; line-height: 1.2; margin: 0; }
.later-article-meta { color: var(--c-text-faint); font-size: var(--fs-sm); margin: 0; }
.later-article-actions { display: flex; flex-wrap: wrap; gap: .5rem; }
.later-article-actions form { margin: 0; }
.later-note { color: var(--c-accent); margin: 0; }
.later-article-body { font-family: 'Source Serif 4', Georgia, serif; font-size: 1.125rem; line-height: 1.65; overflow-wrap: anywhere; }
.later-article-body img { max-width: 100%; height: auto; }
.later-article-body pre { overflow-x: auto; }
.later-linkonly { border: var(--border); border-radius: var(--radius); padding: 1rem; background: var(--c-bg-subtle); }
.later-linkonly textarea { width: 100%; }
```

- [ ] **Step 4: Run to confirm they pass**, full check, then a browser check:
save a real article, read it, archive, unarchive, delete (dialog appears;
Cancel then Escape then Delete each behave), save a paywalled URL and paste
text. Light/dark, 375px width.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/later internal/ui/static/app.css
git commit -m "feat(later): read, archive, delete and paste text into articles"
```

---

### Task 7: Images — serve, fetch on demand, back-fill

**Files:**
- Create: `internal/apps/later/images.go`, `internal/apps/later/images_test.go`
- Modify: `internal/apps/later/later.go` (route, semaphore, `Jobs`)

**Interfaces:**
- Consumes: `webfetch.ValidURLHash`, `webfetch.GivenUp`,
  `(*webfetch.Client).GetImage`, `webfetch.MaxImageBytes`,
  `Store.ImageForUser`, `SaveImageBytes`, `SaveImageFailure`, `ImagesToFetch`.
- Produces: `GET /later/img/{hash}`; `app.Scheduler` with one job
  `download images` (Every 10 minutes, batch of 50);
  `func (a *App) DownloadImages(ctx context.Context, limit int) (stored int, err error)`.

- [ ] **Step 1: Failing tests**

Origin: `httptest` serving a real tiny PNG at `/pic.png` and HTML at `/nope.png`; save an article whose HTML references both (through the real save path, with `AllowPrivateFetchesForTest`).
```go
func TestImageRouteFetchesStoresAndServes(t *testing.T)
	// GET /later/img/{hash of /pic.png} -> 200, Content-Type image/png, nosniff, ETag;
	// origin hit once; second GET served from the store (origin hit count unchanged)
func TestImageRouteRefusesNonImages(t *testing.T)
	// /nope.png -> 404 and SaveImageFailure recorded (error_count 1)
func TestImageRouteHonoursTheBackoff(t *testing.T)
	// after one failure, immediate GET -> 404 without hitting origin; s.Clock.Advance(2h) -> retried
func TestImageRouteIsScopedToTheOwner(t *testing.T)            // bob -> 404 for alice's hash
func TestImageRouteRejectsBadHashes(t *testing.T)              // "../x", uppercase, 31 chars -> 404, no DB work needed
func TestImageRouteAnswers304ForAMatchingETag(t *testing.T)
func TestDownloadImagesBackFillsAndKeepsImagesForever(t *testing.T)
	// a.DownloadImages(ctx, 50) -> stored 1 (pic), failure recorded for nope;
	// s.Clock.Advance(365*24h); image still served (no expiry)
```

- [ ] **Step 2: Run to confirm failure.**

- [ ] **Step 3: Implement**

`later.go`: `imgSem chan struct{}` (capacity 4), `r.HandleFunc("GET /img/{hash}", a.image)`, and:
```go
var _ app.Scheduler = (*App)(nil)

// imageDownloadEvery is how often stored articles' missing images are
// back-filled. Images also arrive on first view; this makes sure the ones
// nobody scrolled to are kept too.
const imageDownloadEvery = 10 * time.Minute

const imageDownloadBatch = 50

func (a *App) Jobs(deps app.Deps) []app.Job {
	return []app.Job{{
		Name:        "download images",
		Description: "Downloads and keeps images of saved articles that haven't been stored yet.",
		Every:       imageDownloadEvery,
		Run: func(ctx context.Context) error {
			n, err := a.DownloadImages(ctx, imageDownloadBatch)
			if n > 0 {
				a.deps.Log.Info("later stored article images", "count", n)
			}
			return err
		},
	}}
}
```
`images.go`:
```go
package later

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// imageCacheControl: private (signed-in only) and long (content-addressed).
const imageCacheControl = "private, max-age=31536000, immutable"

// image serves a stored image, fetching it first if it hasn't been yet.
// It takes a hash, never a URL, and only hashes one of the viewer's own
// articles links resolve — so nothing here can be made to fetch anything
// the save path didn't already see.
func (a *App) image(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	hash := r.PathValue("hash")
	if !webfetch.ValidURLHash(hash) {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	img, err := a.store.ImageForUser(r.Context(), userID, hash)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if !img.Cached() {
		if webfetch.GivenUp(img.ErrorCount, img.FetchedAt, a.store.now()) {
			a.deps.Errors.Status(w, r, http.StatusNotFound)
			return
		}
		if img, err = a.fetchImage(r.Context(), img); err != nil {
			if errors.Is(err, context.Canceled) || r.Context().Err() != nil {
				return // the viewer left; not the publisher's failure
			}
			a.deps.Errors.Status(w, r, http.StatusNotFound)
			return
		}
	}
	etag := `"` + img.Hash + `"`
	h := w.Header()
	h.Set("Cache-Control", imageCacheControl)
	h.Set("ETag", etag)
	h.Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", img.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(img.Bytes)))
	_, _ = w.Write(img.Bytes)
}

// fetchImage downloads one image under the concurrency bound and records
// the outcome either way.
func (a *App) fetchImage(ctx context.Context, img Image) (Image, error) {
	select {
	case a.imgSem <- struct{}{}:
		defer func() { <-a.imgSem }()
	case <-ctx.Done():
		return Image{}, ctx.Err()
	}
	ct, body, err := a.client.GetImage(ctx, img.SrcURL, webfetch.MaxImageBytes)
	if err != nil {
		if ctx.Err() == nil {
			if serr := a.store.SaveImageFailure(ctx, img.Hash, err.Error()); serr != nil {
				a.deps.Log.Error("later recording an image failure failed", "error", serr)
			}
			a.deps.Log.Info("later image fetch failed", "src", img.SrcURL, "error", err)
		}
		return Image{}, err
	}
	if err := a.store.SaveImageBytes(ctx, img.Hash, ct, body); err != nil {
		return Image{}, err
	}
	img.ContentType, img.Bytes = ct, body
	return img, nil
}

// DownloadImages stores up to limit images no one has fetched yet. A failed
// image is recorded and skipped; only a store error stops the batch.
func (a *App) DownloadImages(ctx context.Context, limit int) (int, error) {
	imgs, err := a.store.ImagesToFetch(ctx, limit)
	if err != nil {
		return 0, err
	}
	stored := 0
	for _, img := range imgs {
		if ctx.Err() != nil {
			return stored, ctx.Err()
		}
		if _, err := a.fetchImage(ctx, img); err == nil {
			stored++
		}
	}
	return stored, nil
}
```
(`SaveImageFailure` must not be a reason to abort the batch; a store
error from `SaveImageBytes` is logged by returning it from `fetchImage`
and counted as not stored — keep the batch going.)

- [ ] **Step 4: Run to confirm they pass**, full check, browser check: save
an image-heavy article, images render; run **Run now** on `download images`
at `/admin/jobs`; it reports success.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/later
git commit -m "feat(later): keep article images, fetched on view and back-filled"
```

---

## After the tasks

- Final whole-branch review, then a PR titled
  `feat(later): ON Later L1a — save and read (#482)`; the body says it
  is L1a, lists what L1b will add, and says #482 stays open for L1b.
- Do not close #482.
