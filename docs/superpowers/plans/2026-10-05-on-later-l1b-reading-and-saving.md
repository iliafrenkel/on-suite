# ON Later L1b — reading and saving polish: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish #482: site favicons in the list, a full-screen reading view
with Aa settings and a progress line, resume-where-you-stopped, a ⋯ menu on
list rows, a bookmarklet that saves from any page, and a "Read later" button
in ON Reader.

**Architecture:** Builds on the merged L1a app (`internal/apps/later`).
Two new migrations (`0002_prefs.sql`, `0003_favicons.sql`). Favicons mirror
the image design (content-addressed BLOBs, owner-scoped route, fetched on
first view, `webfetch.GivenUp`). The reading view hides the suite chrome with
app-local CSS (`body:has(.later-reader)`), so no platform change. Every
enhancement works as a plain form first; `later.js` makes it smoother.
ON Reader links to Later only through an HTMX/form POST to `/later/save` —
no Go import.

**Tech Stack:** Go, SQLite, `html/template`, HTMX, plain JS (`later.js`),
`internal/platform/{webfetch,favicon}`.

**Spec:** [2026-10-05-on-later-design.md](../specs/2026-10-05-on-later-design.md).
**Issue:** #482 (second half; closes it). **Previous plan:**
[L1a](2026-10-05-on-later-l1a-save-and-read.md).

**Decisions already made (Ilia):**
- "Move to unread" resets reading progress to 0.
- The bookmarklet popup has no tag field yet (tags arrive in L3, which adds
  the field to the popup and the URL box together).
- From L1a: opening `/later/a/{id}` marks an article Reading, so nothing may
  prefetch article links; saving stays on the list.

**Deviation from the spec:** favicons use two tables — `later_favicons`
(content-addressed by `webfetch.URLHash(icon URL)`) and `later_site_favicons`
(site host → hash) — instead of one per-host table, because two sites can
share one icon URL (a CDN), which a per-host table keyed on the hash can't
hold.

## Global Constraints

- Everything from L1a's Global Constraints still binds: `later_` prefixes,
  STRICT tables, `user_id … ON DELETE CASCADE`, `db.FormatTime`/`ParseTime`,
  time only via `Store.now()`, no import of another app, all outbound HTTP
  via `webfetch`, owner scoping (404 for someone else's), no inline
  `<script>`/`style=`, CSS in app.css's ON Later section with `later-`
  classes, no new dependencies, full check green after every task:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- GET requests never change data, except `GET /later/a/{id}` marking the
  article opened (L1a, by design). Nothing in the list or Reader may
  prefetch `/later/a/` links.
- Aa settings: font `serif` | `sans`; size `1`–`5` (default `3`); width
  `narrow` | `medium` | `wide` (default `medium`); default font `serif`.
  Stored per user in `later_prefs`, applied as classes
  `later-font-{font}`, `later-size-{n}`, `later-width-{w}` on the reader root.
- A `[hidden]` element that also has a class setting `display` needs an
  explicit `.that-class[hidden] { display: none; }` rule (a past Reader bug).
- `back` redirect targets accepted from forms must be local: start with
  `/later/` and not contain `//` or `\`; anything else falls back to the
  handler's default.
- Commits: Conventional Commits, scope `later` (scope `reader` for the
  Reader-side change in Task 7).

## File structure (new or substantially changed)

```
internal/apps/later/
  migrations/0002_prefs.sql       later_prefs
  migrations/0003_favicons.sql    later_favicons, later_site_favicons
  prefs.go                        Prefs type, Store.Prefs/SetPrefs, POST /later/prefs
  favicons.go                     favicon store methods + GET /later/favicon/{hash}
  back.go                         safeBack(r, fallback) for redirect targets
  popup.go                        bookmarklet popup: GET /later/save, GET /later/popup/done
  templates/article.html          full-screen reader with top bar, Aa, ⋯, progress
  templates/popup.html            bookmarklet popup page
  templates/chips.html            HTMX fragments for ON Reader's button
  static/later.js                 + prefs, progress/resume, favicon fallback,
                                    bookmarklet href, autoclose
internal/apps/reader/
  templates/panes.partial.html    "Read later" form in the article toolbar
  view.go (or handlers.go)        LaterEnabled on the article view model
```

---

### Task 1: Reading settings and progress in the store

**Files:**
- Create: `internal/apps/later/migrations/0002_prefs.sql`, `internal/apps/later/prefs.go`, `internal/apps/later/prefs_test.go`
- Modify: `internal/apps/later/store.go` (`SetState`, new `SetProgress`), `internal/apps/later/store_test.go`

**Interfaces — Produces:**
```go
type Prefs struct {
	Font  string // "serif" | "sans"
	Size  int    // 1..5
	Width string // "narrow" | "medium" | "wide"
}
var DefaultPrefs = Prefs{Font: "serif", Size: 3, Width: "medium"}
func (p Prefs) Valid() bool
func (st *Store) Prefs(ctx context.Context, userID int64) (Prefs, error)  // DefaultPrefs when unset
func (st *Store) SetPrefs(ctx context.Context, userID int64, p Prefs) error // ErrInvalid if !p.Valid()
func (st *Store) SetProgress(ctx context.Context, userID, id int64, p float64) error
```
Rules: `SetProgress` clamps to [0,1]; NaN/Inf → `ErrInvalid`; changes only
`progress` (and `updated_at`), never `state`; another user's id →
`ErrNotFound`. `SetState(…, StateUnread)` now also sets `progress = 0`.

- [ ] **Step 1: Migration**
```sql
-- Reading settings, one row per user; absent means the defaults.
CREATE TABLE later_prefs (
    user_id    INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    font       TEXT    NOT NULL CHECK (font IN ('serif', 'sans')),
    size       INTEGER NOT NULL CHECK (size BETWEEN 1 AND 5),
    width      TEXT    NOT NULL CHECK (width IN ('narrow', 'medium', 'wide')),
    updated_at TEXT    NOT NULL
) STRICT;
```

- [ ] **Step 2: Failing tests** (`prefs_test.go`, `store_test.go`; reuse the L1a `newFixture`):
```go
func TestPrefsDefaultWhenUnset(t *testing.T)       // Prefs(alice) == later.DefaultPrefs
func TestSetPrefsRoundTripsPerUser(t *testing.T)   // alice {sans,5,wide}; bob still default
func TestSetPrefsRejectsInvalid(t *testing.T)      // {"mono",3,"medium"}, {"serif",0,"medium"}, {"serif",6,"medium"}, {"serif",3,"huge"} -> ErrInvalid; stored prefs unchanged
func TestSetProgressClampsAndKeepsState(t *testing.T)
	// unread article: SetProgress(0.42) -> Progress 0.42, State still unread;
	// 1.7 -> 1; -0.3 -> 0; math.NaN() -> ErrInvalid; bob -> ErrNotFound
func TestMoveToUnreadResetsProgress(t *testing.T)  // SetProgress 0.6, archive, SetState(unread) -> Progress 0
```

- [ ] **Step 3: Run → fail (compile).**

- [ ] **Step 4: Implement** `prefs.go`:
```go
package later

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// Prefs are one reader's Aa settings.
type Prefs struct {
	Font  string
	Size  int
	Width string
}

// DefaultPrefs apply until the reader changes something.
var DefaultPrefs = Prefs{Font: "serif", Size: 3, Width: "medium"}

// Valid reports whether every field is one the reading view has a class for.
func (p Prefs) Valid() bool {
	return (p.Font == "serif" || p.Font == "sans") &&
		p.Size >= 1 && p.Size <= 5 &&
		(p.Width == "narrow" || p.Width == "medium" || p.Width == "wide")
}

func (st *Store) Prefs(ctx context.Context, userID int64) (Prefs, error) {
	var p Prefs
	err := st.db.QueryRowContext(ctx,
		`SELECT font, size, width FROM later_prefs WHERE user_id = ?`, userID).
		Scan(&p.Font, &p.Size, &p.Width)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultPrefs, nil
	}
	if err != nil {
		return Prefs{}, fmt.Errorf("later: load prefs: %w", err)
	}
	return p, nil
}

func (st *Store) SetPrefs(ctx context.Context, userID int64, p Prefs) error {
	if !p.Valid() {
		return ErrInvalid
	}
	_, err := st.db.ExecContext(ctx, `
		INSERT INTO later_prefs (user_id, font, size, width, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET font = excluded.font, size = excluded.size,
			width = excluded.width, updated_at = excluded.updated_at`,
		userID, p.Font, p.Size, p.Width, db.FormatTime(st.now()))
	if err != nil {
		return fmt.Errorf("later: save prefs: %w", err)
	}
	return nil
}
```
`SetProgress` in `store.go` (use the existing `exec` helper that maps
0 rows to `ErrNotFound`):
```go
// SetProgress records how far through an article the reader has scrolled,
// 0..1. It never changes the article's state.
func (st *Store) SetProgress(ctx context.Context, userID, id int64, p float64) error {
	if math.IsNaN(p) || math.IsInf(p, 0) {
		return ErrInvalid
	}
	p = min(1, max(0, p))
	return st.exec(ctx, "progress", `
		UPDATE later_articles SET progress = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		p, db.FormatTime(st.now()), id, userID)
}
```
`SetState`'s unread branch: add `progress = 0` to the UPDATE.

- [ ] **Step 5: Run → pass; full check.**
- [ ] **Step 6: Commit** — `feat(later): store reading settings and progress`

---

### Task 2: Site favicons

**Files:**
- Create: `internal/apps/later/migrations/0003_favicons.sql`, `internal/apps/later/favicons.go`, `internal/apps/later/favicons_test.go`
- Modify: `store.go` (`NewArticle.FaviconURL`, `Save`, `ListItem`, `List`), `save.go` (`fetchArticle`), `later.go` (route), `handlers.go` (`rowView`), `templates/index.html`, `static/later.js`, `internal/ui/static/app.css`

**Interfaces:**
- Consumes: `favicon.Discover(pageHTML []byte, siteURL string) string`, `webfetch.URLHash`, `webfetch.ValidURLHash`, `webfetch.GivenUp`, `(*webfetch.Client).GetImage`, `webfetch.MaxFaviconBytes`.
- Produces:
```go
// NewArticle gains:
FaviconURL string // absolute; "" when unknown
// ListItem gains:
FaviconHash   string // "" when the site has no favicon row
FaviconShown  bool   // a hash exists and it is cached or not given up
type Favicon struct { Hash, SrcURL, ContentType string; Bytes []byte; FetchedAt time.Time; ErrorCount int }
func (st *Store) FaviconForUser(ctx context.Context, userID int64, hash string) (Favicon, error)
func (st *Store) SaveFaviconBytes(ctx context.Context, hash, contentType string, b []byte) error
func (st *Store) SaveFaviconFailure(ctx context.Context, hash, msg string) error
// route
GET /later/favicon/{hash}
```

- [ ] **Step 1: Migration**
```sql
-- Favicons are content-addressed like images: two sites may share one icon
-- URL (a CDN), so the host -> icon mapping is its own table.
CREATE TABLE later_favicons (
    hash         TEXT    PRIMARY KEY, -- webfetch.URLHash(src_url)
    src_url      TEXT    NOT NULL,
    content_type TEXT    NOT NULL DEFAULT '',
    bytes        BLOB,
    fetched_at   TEXT,
    last_error   TEXT    NOT NULL DEFAULT '',
    error_count  INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE TABLE later_site_favicons (
    site_host TEXT PRIMARY KEY,
    hash      TEXT NOT NULL REFERENCES later_favicons (hash) ON DELETE CASCADE
) STRICT;
```

- [ ] **Step 2: Failing tests**
Store (`favicons_test.go`, store fixture):
```go
func TestSaveRecordsTheSitesFaviconOnce(t *testing.T)
	// Save with FaviconURL "https://example.com/icon.png" for host example.com,
	// then a second article on example.com with a different FaviconURL ->
	// later_site_favicons still points at the first icon's hash (first one wins)
func TestTwoSitesCanShareOneIcon(t *testing.T)
	// a.example and b.example both with FaviconURL https://cdn.example/i.png -> both Save succeed, one later_favicons row
func TestListCarriesTheFavicon(t *testing.T)
	// fresh icon -> FaviconHash set, FaviconShown true; after 3 SaveFaviconFailure -> FaviconShown false;
	// cached bytes -> FaviconShown true; article whose site has no icon -> "" and false
func TestFaviconForUserIsOwnerScoped(t *testing.T)
	// alice saved example.com; bob has no article on that host -> ErrNotFound for bob
```
Handlers (`favicons_test.go`, using `newSaveServer` + `AllowPrivateFetchesForTest`; origin page has `<link rel="icon" href="/fav.png">` and serves a tiny PNG at `/fav.png`, counting hits):
```go
func TestSavingDiscoversTheFavicon(t *testing.T)
	// after POST /later/save, GET /later/ has img.later-favicon[src=/later/favicon/{hash}]
	// where hash == webfetch.URLHash(origin+"/fav.png")
func TestFaviconRouteFetchesStoresAndServes(t *testing.T)
	// 200 image/png, nosniff, ETag; second GET doesn't hit origin
func TestFaviconRouteAnswersBare404WhenGivenUp(t *testing.T)
	// origin 404s for the icon: 3 GETs (advance clock 2h between) -> later GETs are 404 with an empty body
	// and no origin hit; list then shows .later-favicon-badge (not hidden) and no img
func TestLinkOnlyItemsGuessFaviconIco(t *testing.T)
	// fetch fails -> site favicon src_url is https?://host/favicon.ico
func TestFaviconRouteRejectsBadHashesAndOtherUsers(t *testing.T)
```

- [ ] **Step 3: Run → fail.**

- [ ] **Step 4: Implement**

`fetchArticle` (`save.go`): after a successful `Get`, set
`n.FaviconURL = favicon.Discover(res.Body, res.FinalURL)`; when the fetch
failed, `n.FaviconURL = favicon.Discover(nil, pageURL)`.

`Store.Save`: inside the existing transaction, after the article insert
(only when `created`), if `n.FaviconURL != ""`:
```go
		h := webfetch.URLHash(n.FaviconURL)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO later_favicons (hash, src_url) VALUES (?, ?) ON CONFLICT (hash) DO NOTHING`,
			h, n.FaviconURL); err != nil {
			return Article{}, false, fmt.Errorf("later: save favicon: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO later_site_favicons (site_host, hash) VALUES (?, ?) ON CONFLICT (site_host) DO NOTHING`,
			host, h); err != nil {
			return Article{}, false, fmt.Errorf("later: link favicon: %w", err)
		}
```
`List`: `LEFT JOIN later_site_favicons sf ON sf.site_host = a.site_host
LEFT JOIN later_favicons f ON f.hash = sf.hash`, selecting
`COALESCE(sf.hash, '')`, `f.bytes IS NOT NULL`, `COALESCE(f.error_count, 0)`,
`f.fetched_at`; compute `FaviconShown = hash != "" && (cached ||
!webfetch.GivenUp(errorCount, fetchedAt, st.now()))` in Go. (Alias the
articles table `a` and qualify every column.)

`favicons.go`: store methods as images_store.go's, over `later_favicons`;
`FaviconForUser` requires
`EXISTS (SELECT 1 FROM later_site_favicons sf JOIN later_articles a ON a.site_host = sf.site_host WHERE sf.hash = f.hash AND a.user_id = ?)`.
Route `favicon` mirrors `image` in images.go (validate hash first, owner
scope, serve cached, `GivenUp` → bare 404, fetch through `a.imgSem` with
`GetImage(…, webfetch.MaxFaviconBytes)`, record failures unless the viewer
left, `ErrNotFound` on record is benign) — except every 404 is **bare**
(status only, no HTML body, `Cache-Control: private, max-age=3600`): the
list asks for every row's icon on every view, so a full error page per dead
icon is waste (Reader learned this; see its `bareNotFound`).

`rowView` gains `FaviconSrc string` (`"/later/favicon/"+hash` when
`FaviconShown`, else `""`) and `Initial string` (upper-case first rune of the
site). Row markup, before the title:
```html
<span class="later-favicon-wrap" aria-hidden="true">
	{{if .FaviconSrc}}<img class="later-favicon" src="{{.FaviconSrc}}" alt="" width="16" height="16" loading="lazy">
	<span class="later-favicon-badge" hidden>{{.Initial}}</span>
	{{else}}<span class="later-favicon-badge">{{.Initial}}</span>{{end}}
</span>
```
`later.js` — swap a broken icon for its badge (capture phase, because
`error` doesn't bubble):
```js
	document.addEventListener("error", function (e) {
		var img = e.target;
		if (!(img instanceof HTMLImageElement) || !img.classList.contains("later-favicon")) return;
		var badge = img.nextElementSibling;
		img.remove();
		if (badge) badge.hidden = false;
	}, true);
```
CSS:
```css
.later-row-link { display: flex; gap: .6rem; align-items: flex-start; }
.later-row-text { flex: 1; min-width: 0; }
.later-favicon-wrap { flex: none; width: 1rem; height: 1rem; margin-top: .2rem; }
.later-favicon { display: block; width: 1rem; height: 1rem; border-radius: 3px; }
.later-favicon-badge { display: inline-flex; align-items: center; justify-content: center; width: 1rem; height: 1rem; border-radius: 3px; background: var(--c-bg-inset); color: var(--c-text-dim); font-size: .625rem; font-weight: 600; }
.later-favicon-badge[hidden] { display: none; }
```
(Wrap the title + meta spans in `<span class="later-row-text">`; keep
`.later-row-title`/`.later-row-meta` as they are.)

- [ ] **Step 5: Run → pass; full check; browser check** of the list with
real sites (icons appear; a site with no icon shows its letter; dark mode).
- [ ] **Step 6: Commit** — `feat(later): show site favicons in the list`

---

### Task 3: The full-screen reading view and Aa settings

**Files:**
- Create: `internal/apps/later/back.go`, `internal/apps/later/back_test.go`
- Modify: `prefs.go` (handler), `later.go` (route), `handlers.go` (`articleView`, `renderArticle`), `templates/article.html`, `static/later.js`, `internal/ui/static/app.css`, `handlers_test.go`, `docs/user/later.md`

**Interfaces:**
- Consumes: `Store.Prefs`, `Store.SetPrefs`, `Prefs`, `DefaultPrefs`.
- Produces:
```go
func safeBack(r *http.Request, fallback string) string // form field "back"
// POST /later/prefs: fields font | size | width (any subset), back
//   plain form -> 303 to safeBack(r, "/later/")
//   header "X-Later-Async: 1" -> 204
//   invalid value -> 400
```
`articleView` gains `Prefs Prefs`, `SizeDown, SizeUp int` (0 when at the
bound), `Tab State` (the article's state, for the ← link),
`Progress float64`, `Back string` (the article's own path, for forms).

- [ ] **Step 1: Failing tests**
```go
func TestSafeBack(t *testing.T)
	// "/later/a/3" kept; "", "https://evil.example/", "//evil.example", "/admin/", "/later/\\evil", "/later//x" -> fallback
func TestArticleRendersTheReaderChrome(t *testing.T)
	// .later-reader root with classes later-font-serif later-size-3 later-width-medium;
	// .later-topbar with a[href=/later/?tab=reading] "← Later", the title, "N min left";
	// progress.later-progress; Aa menu (details.later-aa) with a form per option posting to /later/prefs
	// carrying back=/later/a/{id}; ⋯ menu with Open original and the Delete form (data-later-confirm)
func TestArticleUsesTheReadersPrefs(t *testing.T)    // after SetPrefs(sans,5,wide) -> those classes; A+ button disabled
func TestPrefsFormSavesAndRedirectsBack(t *testing.T) // POST size=4 back=/later/a/1 -> 303 /later/a/1; Prefs().Size==4, font/width unchanged
func TestPrefsAsyncAnswers204(t *testing.T)           // X-Later-Async: 1 -> 204, saved
func TestPrefsRejectsInvalidValues(t *testing.T)      // size=9 -> 400, font=mono -> 400, unchanged
func TestPrefsBackMustBeLocal(t *testing.T)           // back=https://evil.example -> 303 /later/
```

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement**

`back.go`:
```go
package later

import (
	"net/http"
	"strings"
)

// safeBack is the form's "back" target if it is one of Later's own pages,
// else fallback. Anything else could turn a form into an open redirect.
func safeBack(r *http.Request, fallback string) string {
	b := r.PostFormValue("back")
	if !strings.HasPrefix(b, "/later/") || strings.Contains(b, "//") || strings.ContainsRune(b, '\\') {
		return fallback
	}
	return b
}
```
Prefs handler (in `prefs.go`): load current prefs, overwrite only the fields
present in the form (`size` via `strconv.Atoi`; a parse error is
`ErrInvalid`), `SetPrefs` (→ `fail` maps `ErrInvalid` to 400), then 204 if
`r.Header.Get("X-Later-Async") == "1"`, else 303 to `safeBack(r, "/later/")`.

`templates/article.html` — the whole page becomes:
```html
{{define "head"}}<script src="/later/later.js" defer></script>{{end}}

{{define "content"}}
{{$d := .Data}}
<div class="later-reader later-font-{{$d.Prefs.Font}} later-size-{{$d.Prefs.Size}} later-width-{{$d.Prefs.Width}}"
     id="later-reader" data-article-id="{{$d.ID}}" data-minutes="{{$d.Minutes}}"
     {{if not $d.LinkOnly}}data-progress="{{$d.Progress}}"{{end}}>
	<div class="later-topbar">
		<a class="later-back" href="/later/?tab={{$d.Tab}}">← Later</a>
		<span class="later-topbar-title">{{$d.Title}}</span>
		{{if not $d.LinkOnly}}<span class="later-left" data-later-left>{{$d.Minutes}} min left</span>{{end}}
		<details class="later-menu later-aa">
			<summary aria-label="Reading settings">Aa</summary>
			<div class="later-menu-panel stack">
				{{template "later-pref-group" (dict "Label" "Font" "Field" "font" "Back" $d.Back "Current" $d.Prefs.Font "Options" (list "serif" "Serif" "sans" "Sans"))}}
				<div class="later-pref-row"><span>Size</span>
					<form method="post" action="/later/prefs" data-later-pref>{{template "later-pref-hidden" $d}}<input type="hidden" name="size" value="{{$d.SizeDown}}"><button type="submit" {{if not $d.SizeDown}}disabled{{end}} aria-label="Smaller text">A−</button></form>
					<form method="post" action="/later/prefs" data-later-pref>{{template "later-pref-hidden" $d}}<input type="hidden" name="size" value="{{$d.SizeUp}}"><button type="submit" {{if not $d.SizeUp}}disabled{{end}} aria-label="Larger text">A+</button></form>
				</div>
				{{template "later-pref-group" (dict "Label" "Width" "Field" "width" "Back" $d.Back "Current" $d.Prefs.Width "Options" (list "narrow" "Narrow" "medium" "Medium" "wide" "Wide"))}}
			</div>
		</details>
		{{/* Archive / Move to unread form, exactly as L1a's, now in the bar. */}}
		<details class="later-menu">
			<summary aria-label="More actions">⋯</summary>
			<div class="later-menu-panel stack">
				{{/* The href is safe: NormalizeURL only stores http(s) URLs. */}}
				<a href="{{$d.URL}}" target="_blank" rel="noopener noreferrer">Open original</a>
				{{/* L1a's Delete form with data-later-confirm, unchanged. */}}
			</div>
		</details>
	</div>
	{{if not $d.LinkOnly}}<progress class="later-progress" max="1" value="{{$d.Progress}}" data-later-progress aria-label="Reading progress"></progress>{{end}}
	<article class="later-article stack">
		{{/* L1a's header (title + meta, without the old action row), then the
		     body or the link-only panel — unchanged. */}}
	</article>
</div>
{{template "later-confirm"}}
{{end}}
```
Check whether the renderer has `dict`/`list` template funcs (Reader uses
`dict`); if `list` doesn't exist, write the two option groups out by hand
instead of a shared `later-pref-group` template — don't add template funcs.
`later-pref-hidden` renders the CSRF field and `back`. Each option button
carries `aria-pressed="true"` when it is the current value.

CSS (ON Later section):
```css
/* The reading view takes the whole window: hide the suite chrome. */
body:has(.later-reader) .shell-bar,
body:has(.later-reader) .app-sidebar,
body:has(.later-reader) .app-footer { display: none; }
.later-topbar { position: sticky; top: 0; z-index: 2; display: flex; align-items: center; gap: .75rem;
  padding: .5rem 1rem; background: var(--c-bg); border-bottom: var(--border); font-size: var(--fs-sm); color: var(--c-text-dim); }
.later-topbar-title { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--c-text); font-weight: 500; }
.later-progress { position: sticky; top: 2.6rem; display: block; width: 100%; height: 3px; appearance: none; border: 0; background: var(--c-bg-subtle); }
.later-progress::-webkit-progress-bar { background: var(--c-bg-subtle); }
.later-progress::-webkit-progress-value { background: var(--c-accent); }
.later-progress::-moz-progress-bar { background: var(--c-accent); }
.later-menu { position: relative; }
.later-menu > summary { list-style: none; cursor: pointer; padding: .2rem .5rem; border: var(--border); border-radius: var(--radius); }
.later-menu > summary::-webkit-details-marker { display: none; }
.later-menu-panel { position: absolute; right: 0; top: calc(100% + .25rem); min-width: 13rem; padding: .75rem; background: var(--c-bg); border: var(--border); border-radius: var(--radius); box-shadow: 0 6px 20px rgba(0,0,0,.12); }
.later-pref-row { display: flex; align-items: center; gap: .5rem; justify-content: space-between; }
.later-pref-row form { margin: 0; }
.later-pref-row button[aria-pressed="true"] { background: var(--c-accent-bg); color: var(--c-accent); }
.later-reader .later-article { margin-block: 2rem; }
.later-width-narrow .later-article { max-width: 56ch; }
.later-width-medium .later-article { max-width: 68ch; }
.later-width-wide .later-article { max-width: 84ch; }
.later-font-serif .later-article-body { font-family: 'Source Serif 4', Georgia, serif; }
.later-font-sans .later-article-body { font-family: var(--font-ui); }
.later-size-1 .later-article-body { font-size: .95rem; }
.later-size-2 .later-article-body { font-size: 1.05rem; }
.later-size-3 .later-article-body { font-size: 1.125rem; }
.later-size-4 .later-article-body { font-size: 1.25rem; }
.later-size-5 .later-article-body { font-size: 1.4rem; }
```
(Remove the L1a fixed `font-family`/`font-size` from `.later-article-body`
and the L1a `.later-article { max-width }` rule, now set per width class.
Check `.shell-bar`/`.app-sidebar`/`.app-footer` are the real class names in
`internal/ui/templates/base.html` and that `main`'s padding still looks
right; adjust if not.)

`later.js` — Aa without a page reload:
```js
	// Aa settings: apply at once, save in the background. Without JS the
	// forms post and redirect back.
	document.addEventListener("submit", function (e) {
		var form = e.target;
		if (!(form instanceof HTMLFormElement) || !form.hasAttribute("data-later-pref")) return;
		var reader = document.getElementById("later-reader");
		if (!reader) return;
		e.preventDefault();
		var data = new FormData(form);
		var body = new URLSearchParams(data);
		fetch(form.action, { method: "POST", body: body, headers: { "X-Later-Async": "1" }, credentials: "same-origin" })
			.then(function (res) { if (!res.ok) throw new Error(String(res.status)); })
			.catch(function () { form.submit(); }); // fall back to the plain post
		["font", "size", "width"].forEach(function (field) {
			var v = data.get(field);
			if (v === null) return;
			Array.from(reader.classList).forEach(function (c) {
				if (c.indexOf("later-" + field + "-") === 0) reader.classList.remove(c);
			});
			reader.classList.add("later-" + field + "-" + v);
		});
		updatePrefButtons(reader);
	});
```
`updatePrefButtons` recomputes `aria-pressed` on the font/width buttons
from the reader's classes and, for size, sets each A−/A+ form's hidden
`size` to current∓1 and `disabled` at 1/5. (The CSRF field travels in the
form body, which the platform accepts.)

Docs (`docs/user/later.md`): add "Reading an article" — the top bar, **Aa**
(font, size, width — remembered on every device), **Archive**, and **⋯**
(Open original, Delete); replace the "Deleting an article" steps to say
**⋯ → Delete**.

- [ ] **Step 4: Run → pass; full check; browser check:** full-screen view,
Aa changes apply instantly and survive a reload, without JS the forms still
work (disable JS once), ← Later returns to the right tab, light/dark, 375px
(top bar fits; title truncates).
- [ ] **Step 5: Commit** — `feat(later): full-screen reading view with Aa settings`

---

### Task 4: Reading progress and resume

**Files:**
- Modify: `handlers.go` (progress handler), `later.go` (route), `static/later.js`, `handlers_test.go`, `docs/user/later.md`

**Interfaces:**
- Consumes: `Store.SetProgress`; the reader root's `data-article-id`,
  `data-minutes`, `data-progress`; `progress[data-later-progress]`,
  `[data-later-left]`.
- Produces: `POST /later/a/{id}/progress` (field `progress`, float) → 204;
  bad value → 400; someone else's → 404.

- [ ] **Step 1: Failing tests**
```go
func TestProgressIsSaved(t *testing.T)            // POST progress=0.42 (CSRF form field) -> 204; Article().Progress == 0.42; state unchanged
func TestProgressRejectsGarbage(t *testing.T)     // "abc", "NaN" -> 400
func TestProgressOfAnotherUserIs404(t *testing.T)
func TestArticleCarriesSavedProgress(t *testing.T) // after 0.42 -> #later-reader[data-progress=0.42], progress value 0.42, list row progress 42
func TestLinkOnlyArticleHasNoProgress(t *testing.T) // no data-progress, no progress element
```

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement** the handler (`strconv.ParseFloat`; error →
400; store `ErrInvalid` → 400 via `fail`) and route
`r.HandleFunc("POST /a/{id}/progress", a.progress)`.

`later.js`:
```js
	// Reading progress: where you are, saved quietly, restored on return.
	(function () {
		var reader = document.getElementById("later-reader");
		if (!reader || !reader.hasAttribute("data-progress")) return;
		var id = reader.dataset.articleId;
		var minutes = parseInt(reader.dataset.minutes, 10) || 1;
		var bar = document.querySelector("[data-later-progress]");
		var left = document.querySelector("[data-later-left]");
		var token = csrfToken();
		var saved = parseFloat(reader.dataset.progress) || 0;
		var timer = null;

		function current() {
			var max = document.documentElement.scrollHeight - window.innerHeight;
			return max > 0 ? Math.min(1, Math.max(0, window.scrollY / max)) : 1;
		}
		function show(p) {
			if (bar) bar.value = p;
			if (left) left.textContent = p >= 0.98 ? "Finished" : Math.max(1, Math.ceil(minutes * (1 - p))) + " min left";
		}
		function body(p) {
			var b = new URLSearchParams();
			b.set("csrf_token", token);
			b.set("progress", p.toFixed(4));
			return b;
		}
		function save() {
			var p = current();
			if (Math.abs(p - saved) < 0.01) return;
			saved = p;
			fetch("/later/a/" + id + "/progress", { method: "POST", body: body(p), credentials: "same-origin" });
		}

		if (saved > 0.02 && saved < 0.98) {
			window.scrollTo(0, saved * (document.documentElement.scrollHeight - window.innerHeight));
		}
		show(current());
		window.addEventListener("scroll", function () {
			show(current());
			clearTimeout(timer);
			timer = setTimeout(save, 2000);
		}, { passive: true });
		window.addEventListener("pagehide", function () {
			var p = current();
			if (Math.abs(p - saved) >= 0.01) navigator.sendBeacon("/later/a/" + id + "/progress", body(p));
		});
	})();
```
`csrfToken()` reads `JSON.parse(document.body.getAttribute("hx-headers"))["X-CSRF-Token"]`
(guarded with try/catch, returning ""). Confirm `web.CSRFFormField` is
`csrf_token`. Make sure the scroll restore runs after images have their
`width`/`height` or after `load` if the restore lands short (restore once on
`DOMContentLoaded`, and again on `load` if the user hasn't scrolled).

Docs: "ON Later remembers where you stopped and opens there next time. The
line under the top bar shows how far through you are, and the bar shows the
minutes left."

- [ ] **Step 4: Run → pass; full check; browser check:** scroll halfway,
leave, the list row shows ~50%, reopening lands near the same place; Move
to unread → reopens at the top.
- [ ] **Step 5: Commit** — `feat(later): remember where you stopped reading`

---

### Task 5: The row ⋯ menu

**Files:**
- Modify: `handlers.go` (`setState`, `delete` honour `back`; `rowView.State`), `templates/index.html`, `internal/ui/static/app.css`, `handlers_test.go`, `docs/user/later.md`

**Interfaces:** Consumes `safeBack` (Task 3).

- [ ] **Step 1: Failing tests**
```go
func TestRowMenuOffersTheRightActions(t *testing.T)
	// unread tab row: details.later-row-menu with an Archive form (action /later/a/{id}/archive,
	// back=/later/?tab=unread) and a Delete form with data-later-confirm; archived tab row: "Move to unread" instead of Archive
func TestArchiveFromTheListStaysOnTheList(t *testing.T)   // POST archive back=/later/?tab=unread -> 303 /later/?tab=unread
func TestDeleteFromTheListStaysOnTheList(t *testing.T)
func TestActionsIgnoreForeignBackTargets(t *testing.T)    // back=https://evil.example -> L1a's default redirects
func TestIndexIncludesTheConfirmDialog(t *testing.T)      // #later-confirm-dialog present on the list page
```

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement**: `setState` and `delete` redirect to
`safeBack(r, <their current default>)`. Row markup after the row link:
```html
<details class="later-menu later-row-menu">
	<summary aria-label="Actions for {{.Title}}">⋯</summary>
	<div class="later-menu-panel stack">
		{{if eq .State "archived"}}
		<form method="post" action="/later/a/{{.ID}}/unarchive">{{/* csrf + back */}}<button type="submit">Move to unread</button></form>
		{{else}}
		<form method="post" action="/later/a/{{.ID}}/archive">{{/* csrf + back */}}<button type="submit">Archive</button></form>
		{{end}}
		<form method="post" action="/later/a/{{.ID}}/delete" data-later-confirm="Delete this article permanently? This can't be undone.">{{/* csrf + back */}}<button class="danger" type="submit">Delete</button></form>
	</div>
</details>
```
The `rows` block renders inside `range`, so the CSRF token and the current
tab must be reachable: pass them on the row view (`CSRF string`, `Back
string`) or use `$` — check what `Render.Fragment` passes for the Load-more
fragment and make both paths work. Add `{{template "later-confirm"}}` to
`index.html`. CSS: `.later-row-menu > summary { border: 0; }` and keep the
panel inside the viewport at 375px (`right: 0`).

Docs: "Each article in the list has a **⋯** menu to archive, move back to
unread, or delete it without opening it."

- [ ] **Step 4: Run → pass; full check; browser check** (menu opens, actions keep you on the tab, Delete asks first, 375px).
- [ ] **Step 5: Commit** — `feat(later): archive and delete from the list`

---

### Task 6: The bookmarklet

**Files:**
- Create: `internal/apps/later/popup.go`, `internal/apps/later/popup_test.go`, `internal/apps/later/templates/popup.html`
- Modify: `later.go` (routes), `handlers.go` (`save` honours `popup=1`), `templates/index.html` (bookmarklet link), `static/later.js`, `internal/ui/static/app.css`, `docs/user/later.md`

**Interfaces:**
- Consumes: `NormalizeURL`, `Store.ArticleByURL`, `savedNote`-style loading, `badURLMessage`.
- Produces:
  - `GET /later/save?url=…` → popup page. Never writes.
    - valid, not saved → form (`action=/later/save`, hidden `url`, hidden `popup=1`, CSRF), **Save** button with `autofocus`, **Cancel** button `data-later-close`.
    - already saved → `Already in ON Later.` + `Open` link (target `_blank`) — no form.
    - missing/invalid → `badURLMessage`, no form.
  - `POST /later/save` with `popup=1` → success 303 `/later/popup/done?saved={id}` (`&existing=1` when it already existed); invalid URL → popup page with 422 and the message.
  - `GET /later/popup/done?saved={id}[&existing=1]` → popup page with the L1a saved-note wording and `data-later-autoclose`; a foreign/malformed id → a plain "Done." page that still auto-closes.

- [ ] **Step 1: Failing tests** (`popup_test.go`):
```go
func TestPopupGetNeverSaves(t *testing.T)                // GET /later/save?url=https://example.com/x -> 200, Counts all 0, form[action=/later/save] with input[name=url][value=…] and input[name=popup][value=1]
func TestPopupShowsAlreadySaved(t *testing.T)            // existing article -> "Already in ON Later." + a[href=/later/a/{id}], no form
func TestPopupRejectsABadURL(t *testing.T)               // url=ftp://x -> page shows badURLMessage, no form
func TestPopupSaveRedirectsToDone(t *testing.T)          // POST popup=1 -> 303 /later/popup/done?saved={id}; state unread
func TestPopupDonePageAutoCloses(t *testing.T)           // [data-later-autoclose] and the saved note with the title
func TestPopupDoneIgnoresOtherUsersIDs(t *testing.T)     // bob's id -> no title, still autoclose
func TestPopupInvalidPostIs422(t *testing.T)
func TestListPageOffersTheBookmarklet(t *testing.T)      // a[data-later-bookmarklet] present
func TestPopupRequiresSignIn(t *testing.T)               // anonymous GET -> 303 to login (default router)
```

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement**

`popup.html` renders a `.later-popup` root; CSS hides the suite chrome for
it the same way the reader does:
```css
body:has(.later-popup) .shell-bar,
body:has(.later-popup) .app-sidebar,
body:has(.later-popup) .app-footer { display: none; }
.later-popup { max-width: 26rem; margin: 1.5rem auto; padding: 0 1rem; }
.later-popup-url { color: var(--c-text-faint); font-size: var(--fs-sm); overflow-wrap: anywhere; }
.later-bookmarklet { display: inline-block; padding: .15rem .6rem; border: 1px dashed var(--c-border-firm); border-radius: var(--radius); cursor: grab; }
```
Page title `Save to ON Later`. The list page gets, below the tabs or above
the empty text, a short line:
`<p class="later-hint">Save from any page: drag <a class="later-bookmarklet" data-later-bookmarklet href="/later/">Save to Later</a> to your bookmarks bar.</p>`
(`.later-hint { font-size: var(--fs-sm); color: var(--c-text-faint); }`).

`later.js`:
```js
	// The bookmarklet's address needs this site's origin, which only the
	// browser knows for sure (proxies, ports). Clicking it here would be
	// blocked by our own CSP, so a click just explains what to do.
	document.querySelectorAll("[data-later-bookmarklet]").forEach(function (a) {
		a.href = "javascript:(function(){window.open('" + location.origin +
			"/later/save?url='+encodeURIComponent(location.href),'onlater','popup,width=480,height=360');})();";
		a.addEventListener("click", function (e) {
			e.preventDefault();
			a.title = "Drag this to your bookmarks bar";
		});
	});
	document.querySelectorAll("[data-later-close]").forEach(function (b) {
		b.addEventListener("click", function () { window.close(); });
	});
	if (document.querySelector("[data-later-autoclose]")) {
		setTimeout(function () { window.close(); }, 1500);
	}
```
(Setting a `javascript:` href from script is allowed by the CSP; only
executing it on our own page would be blocked. Check that `html/template`
isn't involved — the template only renders `href="/later/"`.)

Docs: "Saving from any page" — drag **Save to Later** from the top of ON
Later to the bookmarks bar; on any page click it, then **Save** (or press
Enter); the little window closes itself. Mention that you must be signed in
to ON Suite in that browser.

- [ ] **Step 4: Run → pass; full check; browser check:** open
`/later/save?url=…` directly in a tab (the popup layout), save, see the done
page; drag-and-drop can't be automated, so also run the generated
`javascript:` URL's `window.open` call from another tab's console to prove
the address is right.
- [ ] **Step 5: Commit** — `feat(later): save from any page with a bookmarklet`

---

### Task 7: "Read later" in ON Reader

**Files:**
- Create: `internal/apps/later/templates/chips.html`
- Modify: `internal/apps/later/handlers.go` (`save` HTMX branch), `internal/apps/later/save_test.go` (or `handlers_test.go`), `internal/ui/static/app.css`,
  `internal/apps/reader/templates/panes.partial.html`, the Reader view-model file that builds the article (`view.go`/`handlers.go`), a Reader view test, `docs/user/reader.md`

**Interfaces:**
- Later produces: `POST /later/save` with `HX-Request: true` →
  - new: 200, fragment `<span class="later-chip" role="status">Saved to Later ✓ <a href="/later/?saved={id}">Open</a></span>`
  - existing: 200, `Already in Later · <a href="/later/a/{id}">Open</a>` (same span)
  - invalid URL: 422, `<span class="later-chip later-chip-error" role="alert">Couldn't save this link.</span>`
  - non-HTMX behaviour unchanged.
- Reader produces: article view model field `LaterEnabled bool`, true when the
  page's `Shell.Apps` contains an item with `ID == "later"`.

- [ ] **Step 1: Failing tests**
Later:
```go
func TestSaveOverHTMXReturnsAChip(t *testing.T)        // PostHX -> 200, body has .later-chip "Saved to Later", no <html, row created, state unread
func TestSaveOverHTMXForAnExistingArticle(t *testing.T) // "Already in Later" + a[href=/later/a/{id}]
func TestSaveOverHTMXRejectsABadURL(t *testing.T)       // 422 .later-chip-error
```
Reader (view-level; apptest mounts only Reader, so test the model and
template directly — follow how Reader's existing view tests render the
"article" block):
```go
func TestArticleOffersReadLaterWhenLaterIsEnabled(t *testing.T)
	// LaterEnabled true + URL -> form[hx-post=/later/save] with input[name=url][value=<item url>], button "Read later"
func TestArticleHidesReadLaterWhenLaterIsOff(t *testing.T)  // LaterEnabled false -> no such form
func TestLaterEnabledReadsTheShell(t *testing.T)            // helper true/false on Shell.Apps
```

- [ ] **Step 2: Run → fail.**

- [ ] **Step 3: Implement**

Later `save`: right after the user check, `htmx := web.IsHTMX(r)`; on the
invalid-URL, existing and created paths, when `htmx`, render
`a.deps.Render.Fragment(w, status, "later/chips", "<block>", data)` instead
of the redirect / full page (blocks `chip-saved`, `chip-existing`,
`chip-error` in `chips.html`; `chips.html` needs a `content` block too if
the renderer requires every page file to define one — check
`internal/platform/render`). CSS:
```css
.later-chip { display: inline-flex; align-items: center; gap: .4rem; font-size: var(--fs-sm); color: var(--c-accent); }
.later-chip-error { color: var(--c-danger); }
```

Reader (`panes.partial.html`, in `.reader-actions` right after "Open
original"):
```html
{{if and .URL .LaterEnabled}}
<form method="post" action="/later/save" hx-post="/later/save" hx-target="this" hx-swap="outerHTML">
	{{/* the same CSRF field the other forms in this block carry */}}
	<input type="hidden" name="url" value="{{.URL}}">
	<button type="submit" class="toolbar-btn">{{ticon "bookmark"}}Read later</button>
</form>
{{end}}
```
If `ticon "bookmark"` doesn't exist in `internal/ui`, add a bookmark toolbar
icon there in the existing style (shared UI chrome, allowed), with its test
list updated. Without JS the form posts and lands on ON Later's list with
the saved note — fine. Reader sets `LaterEnabled` where it builds the
article data, from the `render.Shell` it already has
(`a.deps.Page(r, …).Shell.Apps`); the helper is a tiny loop. No import of
`internal/apps/later`.

Docs: `docs/user/reader.md` — in the article actions, "**Read later** saves
the article to [ON Later](later.md), to read properly when you have time.
It's there only when ON Later is turned on." `docs/user/later.md`: add the
same as a third way to save.

- [ ] **Step 4: Run → pass; full check (including the arch tests); browser
check:** in Reader, open an article, click **Read later** → chip; click
again on another article already saved → "Already in Later"; ON Later's
list shows it in Unread.
- [ ] **Step 5: Commit** (two) —
  `feat(later): answer HTMX saves with a small confirmation` and
  `feat(reader): add a Read later button for ON Later`

---

## After the tasks

- Final whole-branch review, then a PR titled
  `feat(later): ON Later L1b — reading and saving polish (#482)` that
  closes #482. Screenshots of the reading view and the popup in the PR body
  are welcome; the user-guide screenshots are L4.
