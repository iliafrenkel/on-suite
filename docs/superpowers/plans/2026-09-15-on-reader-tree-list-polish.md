# ON Reader tree/list polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Five small, independent fixes to the ON Reader feed tree and article
list: indent feeds under their folder, add a "Feed URL" copy action, add a
"hide feeds with no unread items" toggle, fix a CSS bug that reveals every
row's "..." menu when hovering a folder, and stop a just-read article's list
row from staying bold.

**Architecture:** Pure template/CSS/JS changes for four of the five items; the
hide-read toggle additionally needs one new cookie-backed preference (mirrors
`internal/apps/notes/prefs.go` exactly), one new route, and a filtering pass
inside `viewTree`. No storage schema changes, no changes to feed polling,
OPML, or full-article extraction.

**Tech Stack:** Go (`net/http`, `html/template` via the platform's `render`
package), htmx, vanilla JS (`internal/apps/reader/static/reader.js`), plain
CSS (`internal/ui/static/app.css`).

## Global Constraints

- No inline `<script>` — the suite's CSP forbids it (see `reader.js`'s own
  header comment); any new client behavior goes in `reader.js`.
- No new dependencies (no clipboard polyfill, no JS framework).
- Every POST route stays behind existing auth middleware and CSRF checks —
  register new routes the same way `app.go`'s `Mount` already does, and use
  `{{template "reader-ctx" .}}` (or the session's own CSRF token in tests) on
  any new form.
- Follow the existing cookie preference pattern verbatim:
  `internal/apps/notes/handlers.go:625-671` and
  `internal/apps/notes/prefs.go` (`HttpOnly`, `Secure: a.deps.Secure`,
  `SameSite: http.SameSiteLaxMode`, one-year `MaxAge`, no cookie value
  reused past `"0"`/`"1"`).
- Spec: [docs/superpowers/specs/2026-09-15-on-reader-tree-list-polish-design.md](../specs/2026-09-15-on-reader-tree-list-polish-design.md).

---

## Task 1: Indent feeds under their folder

**Files:**
- Modify: `internal/ui/static/app.css:1893-1900`

**Interfaces:** None — pure CSS, no Go/template/JS changes.

Today `.reader-sub` (a feed row nested in a folder) and `.reader-node` (a
root-level feed row, no folder) share one rule block with identical padding,
so a feed inside an open folder lines up flush with the folder header instead
of looking nested under it.

- [ ] **Step 1: Split `.reader-sub`'s padding from `.reader-node`'s**

Change:

```css
.reader-node,
.reader-sub {
	display: flex;
	align-items: baseline;
	gap: var(--s-2);
	border-radius: 3px;
	padding: var(--s-1) var(--s-2);
}
```

to:

```css
.reader-node,
.reader-sub {
	display: flex;
	align-items: baseline;
	gap: var(--s-2);
	border-radius: 3px;
	padding: var(--s-1) var(--s-2);
}

/* Nested under its folder's <summary>, not flush with it — the folder
   header (.reader-folder > summary, below) keeps the plain var(--s-2), so a
   feed indents one full --s-4 past that. */
.reader-sub {
	padding-left: calc(var(--s-2) + var(--s-4));
}
```

- [ ] **Step 2: Verify visually**

Run the `run` skill (or `preview_start`) against the on-suite dev server,
sign in, open ON Reader, and expand a folder that has at least one feed in
it. Confirm the feed name now sits visibly indented relative to the folder
name above it, and that a root-level feed (outside any folder) is unaffected.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/static/app.css
git commit -m "style(reader): indent feeds under their folder in the tree"
```

---

## Task 2: Fix the folder hover bug revealing every row's "..." menu

**Files:**
- Modify: `internal/ui/static/app.css:1944-1952`

**Interfaces:** None — pure CSS, no Go/template/JS changes.

**Root cause:** `.reader-folder` is the `<details>` element wrapping the
*entire* folder — its own header row **and** the nested `<ul>` of feed rows
(`internal/apps/reader/templates/panes.partial.html:169-214`). The rule

```css
.reader-folder:hover .outline-menu-toggle,
.reader-folder:focus-within .outline-menu-toggle,
.reader-folder-menu[open] .outline-menu-toggle {
	opacity: 1;
}
```

matches `:hover` on that whole box, so hovering anywhere inside an expanded
folder (including over one specific feed row) reveals **every**
`.outline-menu-toggle` inside it — the folder's own toggle and every child
feed's toggle — not just the row under the pointer. `.reader-node`/
`.reader-sub` rows are already correctly scoped two lines below (they key off
the row itself, an `<li>`, not a container that wraps siblings).

- [ ] **Step 1: Scope the folder's own toggle to its header line**

In `internal/ui/static/app.css`, change:

```css
.reader-node:hover .outline-menu-toggle,
.reader-node:focus-within .outline-menu-toggle,
.reader-sub:hover .outline-menu-toggle,
.reader-sub:focus-within .outline-menu-toggle,
.reader-folder:hover .outline-menu-toggle,
.reader-folder:focus-within .outline-menu-toggle,
.reader-folder-menu[open] .outline-menu-toggle {
	opacity: 1;
}
```

to:

```css
.reader-node:hover .outline-menu-toggle,
.reader-node:focus-within .outline-menu-toggle,
.reader-sub:hover .outline-menu-toggle,
.reader-sub:focus-within .outline-menu-toggle,
.reader-folder > summary:hover .outline-menu-toggle,
.reader-folder > summary:focus-within .outline-menu-toggle,
.reader-folder-menu[open] .outline-menu-toggle {
	opacity: 1;
}
```

(Only the two `.reader-folder` lines change, to `.reader-folder > summary`;
the `.reader-node`/`.reader-sub` lines and the `.reader-folder-menu[open]`
line are unchanged.)

The other rule block just above it,

```css
.reader-node .outline-menu-toggle,
.reader-sub .outline-menu-toggle,
.reader-folder .outline-menu-toggle {
	opacity: 0;
}
```

stays as is — it is the resting (hidden) state for every toggle everywhere in
the tree, and does not need row-level scoping since it never reveals
anything.

- [ ] **Step 2: Verify visually**

Via the `run` skill: expand a folder containing at least two feeds. Hover
over the folder's own header — its "..." toggle should appear (and nothing
else's). Hover over one feed row inside it — only that feed's own "..."
toggle should appear, not the folder's and not the other feed's. Move the
mouse to whitespace inside the folder box but not over any row or the header
— no toggle should be visible.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/static/app.css
git commit -m "fix(reader): scope folder outline-menu hover to its own row"
```

---

## Task 3: "Feed URL" copy action in the feed outline menu

**Files:**
- Modify: `internal/apps/reader/templates/panes.partial.html:199-210` (root
  and in-folder feed menus — both copies)
- Modify: `internal/apps/reader/static/reader.js`
- Test: `internal/apps/reader/handlers_test.go`

**Interfaces:**
- Produces: a `<button class="reader-copy-feed-url" data-feed-url="...">`
  inside each feed's `.outline-menu-list`, and a `reader.js` click handler
  that copies `data-feed-url` to the clipboard and shows "Copied!" for
  1.5 seconds.

`Subscription.FeedURL` (`internal/apps/reader/store.go:90`) is already loaded
onto every `.Subs`/`.Tree.Root` entry the tree template ranges over — no Go
or view-model change is needed to reach it from the template.

- [ ] **Step 1: Add the menu button to both feed-menu copies**

In `internal/apps/reader/templates/panes.partial.html`, the in-folder feed
menu (inside `{{range .Subs}}`, currently lines 199-210):

```html
            <details class="outline-menu">
              <summary class="outline-menu-toggle quiet" aria-label="Feed actions">{{ticon "more"}}</summary>
              <div class="outline-menu-list reader-row-menu-list">
                <form method="post" action="/reader/sub/{{.ID}}/delete">
                  {{template "reader-ctx" $.List}}
                  <button type="submit" class="outline-menu-delete reader-sub-delete"
                          hx-post="/reader/sub/{{.ID}}/delete" hx-target="#reader-panes" hx-swap="outerHTML"
                          hx-confirm="Unsubscribe from &#8220;{{.DisplayName}}&#8221;?"
                          aria-label="Unsubscribe from {{.DisplayName}}">Unsubscribe</button>
                </form>
              </div>
            </details>
```

becomes:

```html
            <details class="outline-menu">
              <summary class="outline-menu-toggle quiet" aria-label="Feed actions">{{ticon "more"}}</summary>
              <div class="outline-menu-list reader-row-menu-list">
                <button type="button" class="reader-copy-feed-url" data-feed-url="{{.FeedURL}}"
                        aria-label="Copy feed URL for {{.DisplayName}}">Feed URL</button>
                <form method="post" action="/reader/sub/{{.ID}}/delete">
                  {{template "reader-ctx" $.List}}
                  <button type="submit" class="outline-menu-delete reader-sub-delete"
                          hx-post="/reader/sub/{{.ID}}/delete" hx-target="#reader-panes" hx-swap="outerHTML"
                          hx-confirm="Unsubscribe from &#8220;{{.DisplayName}}&#8221;?"
                          aria-label="Unsubscribe from {{.DisplayName}}">Unsubscribe</button>
                </form>
              </div>
            </details>
```

Make the identical change to the root-level feed menu (currently lines
222-233 — same markup, same edit, just under `{{range .Tree.Root}}` instead
of `{{range .Subs}}`).

- [ ] **Step 2: Add the "Copied!" click handler**

In `internal/apps/reader/static/reader.js`, add this near the other
delegated `document.addEventListener("click", ...)` blocks (e.g. right after
the "Overflow menu" section, before "--- Dialogs ---"):

```javascript
	// --- Copy feed URL -----------------------------------------------------

	// A plain client-side action: no server round trip, so it does not go
	// through htmx at all. The button's own label is the confirmation —
	// swapped to "Copied!" and back — rather than a toast, since this is a
	// small, single-purpose menu action.
	document.addEventListener("click", function (e) {
		var btn = e.target.closest(".reader-copy-feed-url");
		if (!btn) return;
		var url = btn.getAttribute("data-feed-url");
		if (!url) return;

		var original = btn.textContent;
		function flash(label) {
			btn.textContent = label;
			setTimeout(function () { btn.textContent = original; }, 1500);
		}

		if (!navigator.clipboard || !navigator.clipboard.writeText) {
			flash("Couldn't copy");
			return;
		}
		navigator.clipboard.writeText(url).then(function () {
			flash("Copied!");
		}, function () {
			flash("Couldn't copy");
		});
	});
```

- [ ] **Step 3: Write a handler test for the markup**

Clipboard behavior itself is not testable from a Go HTTP test (no browser),
but the button's presence and its `data-feed-url` value are. Add to
`internal/apps/reader/handlers_test.go`:

```go
// TestFeedMenuOffersToCopyTheFeedURL guards the markup the reader.js click
// handler depends on: the button's data-feed-url must be the subscription's
// actual feed address, not (for example) the display name or the site URL.
func TestFeedMenuOffersToCopyTheFeedURL(t *testing.T) {
	s, a := newServerWithApp(t)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(fixture(t, "rss2.xml"))
	}))
	defer origin.Close()
	a.AllowPrivateFetchesForTest()

	feedURL := origin.URL + "/feed.xml"
	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{"url": {feedURL}})
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d: %s", rec.Code, rec.Body.String())
	}

	doc := s.Get(t, s.Alice, "/reader/")
	btn := doc.MustHave("button.reader-copy-feed-url")
	if got, _ := htmlassert.Attr(btn, "data-feed-url"); got != feedURL {
		t.Errorf("data-feed-url = %q, want %q", got, feedURL)
	}
}
```

This follows `TestSubscribeAddsAFeedToTheTree` (already in the same file,
`handlers_test.go:67-88`) for how to stand up a fake feed origin and
subscribe to it — `newServerWithApp`, `a.AllowPrivateFetchesForTest()`,
`fixture(t, "rss2.xml")` all already exist in this test file, so no new test
helper is needed.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/apps/reader/... -run TestFeedMenuOffersToCopyTheFeedURL -v`
Expected: PASS

- [ ] **Step 5: Manual verification**

Via the `run` skill: open a feed's "..." menu, click "Feed URL". Confirm the
button briefly reads "Copied!" and then reverts, and that pasting somewhere
(e.g. the URL bar) yields the feed's actual address.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/reader/templates/panes.partial.html internal/apps/reader/static/reader.js internal/apps/reader/handlers_test.go
git commit -m "feat(reader): add Feed URL copy action to the outline menu"
```

---

## Task 4: Stop a just-read article's list row from staying bold

**Files:**
- Modify: `internal/apps/reader/templates/panes.partial.html:345-399,409`
- Test: `internal/apps/reader/handlers_test.go`

**Interfaces:**
- Produces: a `row` template block, `{{define "row"}}`, taking a dict with
  keys `Item` (anything with `.ID`/`.Title`/`.FeedName`/`.Published`/
  `.Read`/`.Starred` — either a `listItem` struct or a `dict`), `Scope`,
  `SubID`, `Filter`, `Query`, `ActiveID`, `OOB` (bool). Renders
  `<li id="reader-row-{id}" ...>`.
- Consumes: nothing new from earlier tasks.

`handlers.go:380-385` documents, correctly, why the list pane is not
re-rendered wholesale when an article opens (it would drop the article out
from under an Unread-filtered list). That constraint is unchanged. The fix
here is a *targeted* single-row OOB update — the same technique
`counts-oob` already uses for the sidebar's numbers
(`panes.partial.html:49-58`) — so only the one row whose read/star state
just changed gets touched.

- [ ] **Step 1: Extract the row markup into a shared `row` template**

In `internal/apps/reader/templates/panes.partial.html`, insert a new
`{{define "row"}}` block immediately before `{{define "list"}}` (i.e. right
before line 345):

```html
{{/* row is one article-list row. "list" renders every row this way (OOB
     false); "article-swap" additionally renders just the one row for the
     article that was opened, starred, or had its read state toggled (OOB
     true), targeting the same id by out-of-band swap — see handlers.go's
     renderArticle. Item only needs .ID/.Title/.FeedName/.Published/.Read/
     .Starred, so either a listItem or a plain dict works as .Item. */}}
{{define "row"}}
<li id="reader-row-{{.Item.ID}}" class="reader-row{{if .Item.Read}} is-read{{end}}{{if .Item.Starred}} is-starred{{end}}{{if eq .Item.ID .ActiveID}} is-active{{end}}"{{if .OOB}} hx-swap-oob="true"{{end}}>
  <a href="/reader/item/{{.Item.ID}}?scope={{.Scope}}&sub={{.SubID}}&filter={{.Filter}}{{if .Query}}&q={{.Query}}{{end}}"
     hx-get="/reader/item/{{.Item.ID}}?scope={{.Scope}}&sub={{.SubID}}&filter={{.Filter}}{{if .Query}}&q={{.Query}}{{end}}"
     hx-target="#reader-article" hx-swap="outerHTML">
    <span class="title">{{if .Item.Starred}}<span class="reader-star" aria-label="Starred">&#9733;</span> {{end}}{{.Item.Title}}</span>
    <span class="meta">{{.Item.FeedName}} · {{.Item.Published}}</span>
  </a>
</li>
{{end}}
```

- [ ] **Step 2: Make `list` use it**

Replace (currently lines 384-397):

```html
  {{if .EmptyState}}<p class="empty">{{.EmptyState}}</p>{{end}}
  <ul class="reader-rows">
    {{range .Items}}
      <li class="reader-row{{if .Read}} is-read{{end}}{{if .Starred}} is-starred{{end}}{{if eq .ID $.ActiveID}} is-active{{end}}">
        {{/* The list context rides in the query string so the tree redraw the
             article response carries can keep the selection where it is. */}}
        <a href="/reader/item/{{.ID}}?scope={{$.Scope}}&sub={{$.SubID}}&filter={{$.Filter}}{{if $.Query}}&q={{$.Query}}{{end}}"
           hx-get="/reader/item/{{.ID}}?scope={{$.Scope}}&sub={{$.SubID}}&filter={{$.Filter}}{{if $.Query}}&q={{$.Query}}{{end}}"
           hx-target="#reader-article" hx-swap="outerHTML">
          <span class="title">{{if .Starred}}<span class="reader-star" aria-label="Starred">&#9733;</span> {{end}}{{.Title}}</span>
          <span class="meta">{{.FeedName}} · {{.Published}}</span>
        </a>
      </li>
    {{end}}
  </ul>
```

with:

```html
  {{if .EmptyState}}<p class="empty">{{.EmptyState}}</p>{{end}}
  <ul class="reader-rows">
    {{range .Items}}
      {{template "row" (dict "Item" . "Scope" $.Scope "SubID" $.SubID "Filter" $.Filter "Query" $.Query "ActiveID" $.ActiveID "OOB" false)}}
    {{end}}
  </ul>
```

- [ ] **Step 3: Emit the row's OOB update from `article-swap`**

Replace (currently line 409):

```html
{{define "article-swap"}}{{template "article-oob" .Article}}{{template "counts-oob" .}}{{end}}
```

with:

```html
{{define "article-swap"}}{{template "article-oob" .Article}}{{template "counts-oob" .}}{{template "row" (dict "Item" (dict "ID" .Article.ID "Title" .Article.Title "FeedName" .Article.FeedName "Published" .Article.When "Read" .Article.Read "Starred" .Article.Starred) "Scope" .Article.Scope "SubID" .Article.SubID "Filter" .Article.Filter "Query" .Article.Query "ActiveID" .Article.ID "OOB" true)}}{{end}}
```

(`articleView` — `internal/apps/reader/view.go:92-113` — already carries
every field this needs: `ID`, `Title`, `FeedName`, `When`, `Read`, `Starred`,
`Scope`, `SubID`, `Filter`, `Query`. `ActiveID` is set to `.Article.ID`
itself, deliberately: the article this response is delivering is by
definition the one now open/selected, so its row should carry `is-active`
too, the same way a full list render would. If the corresponding `<li
id="reader-row-N">` is not present in whatever list is currently on screen —
e.g. the reader has since navigated to a different feed — htmx's OOB swap
simply finds nothing to do, the same as `counts-oob` already relies on for
ids that are not present.)

- [ ] **Step 4: Write a test pinning the OOB row swap**

Add to `internal/apps/reader/handlers_test.go`:

```go
// TestReadingAnArticleUpdatesItsListRowOutOfBand guards the fix for the
// list row staying bold after the read count already changed: opening an
// article over htmx must carry an out-of-band update for that one row,
// with is-read now present.
func TestReadingAnArticleUpdatesItsListRowOutOfBand(t *testing.T) {
	s := newServer(t)
	subID, items := seedOne(t, s, "g1")
	itemID := items[0].ID

	listReq := httptest.NewRequest(http.MethodGet, "/reader/feed/"+itoa(subID), nil)
	listReq.Header.Set("HX-Request", "true")
	listRec := s.Do(t, s.Alice, listReq)
	beforeRow := htmlassert.Parse(t, listRec.Body.String()).MustHave("li#reader-row-" + itoa(itemID))
	if got, _ := htmlassert.Attr(beforeRow, "class"); strings.Contains(got, "is-read") {
		t.Fatalf("row already marked read before opening it: class=%q", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/reader/item/"+itoa(itemID), nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)

	row := htmlassert.Parse(t, rec.Body.String()).MustHave("li#reader-row-" + itoa(itemID))
	if got, _ := htmlassert.Attr(row, "hx-swap-oob"); got != "true" {
		t.Errorf("row hx-swap-oob = %q, want \"true\"", got)
	}
	if got, _ := htmlassert.Attr(row, "class"); !strings.Contains(got, "is-read") {
		t.Errorf("row class = %q, want it to include is-read", got)
	}
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/apps/reader/... -run TestReadingAnArticleUpdatesItsListRowOutOfBand -v`
Expected: PASS

- [ ] **Step 6: Run the full reader test suite to check nothing else broke**

Run: `go test ./internal/apps/reader/... -v`
Expected: PASS (in particular
`TestArticleResponseCarriesTheOOBPaneState`, already in this file, must
still pass — the row-oob addition is additive, not a replacement of the
existing OOB pane-state/counts markup).

- [ ] **Step 7: Manual verification**

Via the `run` skill: open the article list for a feed with several unread
items, click one (mouse), confirm its row's text immediately loses its bold
weight without the row moving or disappearing, including under the Unread
filter. Then use `j`/`k` plus `o`/Enter to open another item by keyboard and
confirm the same thing happens.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/reader/templates/panes.partial.html internal/apps/reader/handlers_test.go
git commit -m "fix(reader): update a read article's list row instantly, not just its count"
```

---

## Task 5: "Hide feeds with no unread items" toggle

**Files:**
- Modify: `internal/apps/reader/handlers.go` (new `prefs` handler; `hideRead`
  threaded into `renderIndexWith`/`renderPanes`/`renderArticle`)
- Modify: `internal/apps/reader/app.go` (new route)
- Modify: `internal/apps/reader/view.go` (`viewTree` filtering, `listView`
  gains `HideRead`)
- Modify: `internal/apps/reader/templates/panes.partial.html` (toolbar
  button)
- Create: `internal/apps/reader/prefs.go` (cookie constant + helper, mirrors
  `internal/apps/notes/prefs.go`)
- Modify: `internal/ui/toolbar_icons.go` (new "eye-off" icon)
- Modify: `internal/ui/static/app.css` (one small rule for the new form)
- Test: `internal/apps/reader/view_test.go`, `internal/apps/reader/handlers_test.go`

**Interfaces:**
- Produces: `HideReadCookie` (const, string), `hideReadFrom(r *http.Request) bool`,
  `viewTree(t Tree, activeID int64, scope Scope, counts Counts, hideRead bool) treeView`
  (was 4 params, now 5 — every existing call site updates in this task),
  `POST /reader/prefs`.
- Consumes: `Counts.BySub` (`internal/apps/reader/store.go:901-908`,
  unchanged), `a.deps.Secure` (`internal/platform/app/app.go:93`, unchanged).

### Step-by-step

- [ ] **Step 1: Add the cookie helper file**

Create `internal/apps/reader/prefs.go`:

```go
package reader

import "net/http"

// HideReadCookie holds whether feeds with nothing unread should be hidden
// from the tree. Mirrors internal/apps/notes/prefs.go's ShowCompletedCookie:
// set server-side by POST /reader/prefs (not a client-side JS cookie write),
// since it changes what viewTree returns, not just how an already-loaded
// page looks.
const HideReadCookie = "onsuite_reader_hide_read"

// hideReadCookieMaxAge keeps the preference for a year — long enough to
// feel permanent, short enough that an abandoned browser eventually forgets.
const hideReadCookieMaxAge = 60 * 60 * 24 * 365

// hideReadFrom reads the preference, defaulting to false: a fresh browser
// sees every feed, matching the tree's own default of showing everything.
func hideReadFrom(r *http.Request) bool {
	c, err := r.Cookie(HideReadCookie)
	return err == nil && c.Value == "1"
}
```

- [ ] **Step 2: Add the "eye-off" toolbar icon**

In `internal/ui/toolbar_icons.go`, add one entry to the `toolbarIcons` map
(anywhere among the existing entries, e.g. right after `"inbox"`):

```go
	"eye-off": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M3 3l18 18"/>
		<path d="M10.6 5.1A10.6 10.6 0 0 1 12 5c6 0 9.5 5.5 9.9 7a11.6 11.6 0 0 1-3.1 4.3M6.5 6.6C3.9 8.2 2.4 11 2.1 12c.3 1 1.5 3.1 3.5 4.8A10.4 10.4 0 0 0 12 19c1 0 1.9-.1 2.8-.4"/>
		<path d="M9.9 10a3 3 0 0 0 4.2 4.2"/>
	</svg>`,
```

- [ ] **Step 3: Extend `viewTree` to filter, and `listView` to carry the toggle's state**

In `internal/apps/reader/view.go`, change `viewTree`'s signature and body
(currently lines 127-142):

```go
func viewTree(t Tree, activeID int64, scope Scope, counts Counts) treeView {
	empty := len(t.Root) == 0
	for _, f := range t.Folders {
		if len(f.Subs) > 0 {
			empty = false
		}
	}
	return treeView{
		Folders:  t.Folders,
		Root:     t.Root,
		ActiveID: activeID,
		Scope:    scope,
		Counts:   counts,
		Empty:    empty,
	}
}
```

to:

```go
func viewTree(t Tree, activeID int64, scope Scope, counts Counts, hideRead bool) treeView {
	empty := len(t.Root) == 0
	for _, f := range t.Folders {
		if len(f.Subs) > 0 {
			empty = false
		}
	}

	folders, root := t.Folders, t.Root
	if hideRead {
		folders = make([]TreeFolder, 0, len(t.Folders))
		for _, f := range t.Folders {
			f.Subs = filterUnread(f.Subs, counts, activeID)
			if len(f.Subs) > 0 {
				folders = append(folders, f)
			}
		}
		root = filterUnread(t.Root, counts, activeID)
	}

	return treeView{
		Folders:  folders,
		Root:     root,
		ActiveID: activeID,
		Scope:    scope,
		Counts:   counts,
		// Empty reflects whether the user has any subscriptions at all,
		// unaffected by hideRead — a tree with real feeds that are all
		// currently read is a different situation from having no feeds, and
		// only the latter gets the "no feeds yet" hint.
		Empty: empty,
	}
}

// filterUnread drops every subscription with nothing unread, except the
// currently open one: hiding the feed you are actively reading out from
// under you the moment its last item is read would be more surprising than
// useful, and the sidebar catches up as soon as you navigate away from it.
func filterUnread(subs []Subscription, counts Counts, activeID int64) []Subscription {
	out := make([]Subscription, 0, len(subs))
	for _, s := range subs {
		if s.ID == activeID || counts.BySub[s.ID] > 0 {
			out = append(out, s)
		}
	}
	return out
}
```

Then give `listView` (currently lines 60-77) one new field, added after
`Query`:

```go
type listView struct {
	Items      []listItem
	ActiveID   int64
	Title      string
	Selected   bool
	EmptyState string
	Scope  Scope
	SubID  int64
	Filter Filter
	BasePath string
	Query string
	// HideRead mirrors the cookie the toolbar's toggle button reflects, so
	// the button's pressed state matches whatever was actually applied to
	// this render.
	HideRead bool
	Shell render.Shell
}
```

(Only the `HideRead bool` line and its comment are new; every other field on
`listView` is unchanged — reproduced here so the whole struct is visible for
context.)

- [ ] **Step 4: Thread `hideRead` through the handlers that build the tree**

In `internal/apps/reader/handlers.go`:

Change `renderIndex` and `renderIndexWithNotice` (currently lines 235-245)
from:

```go
func (a *App) renderIndex(w http.ResponseWriter, r *http.Request, userID int64, lc listContext, formErr string) {
	a.renderIndexWith(w, r, userID, lc, formErr, "", nil, 0)
}

func (a *App) renderIndexWithNotice(w http.ResponseWriter, r *http.Request, userID int64, lc listContext, notice string) {
	a.renderIndexWith(w, r, userID, lc, "", notice, nil, 0)
}
```

to:

```go
func (a *App) renderIndex(w http.ResponseWriter, r *http.Request, userID int64, lc listContext, formErr string) {
	a.renderIndexWith(w, r, userID, lc, formErr, "", nil, 0, hideReadFrom(r))
}

func (a *App) renderIndexWithNotice(w http.ResponseWriter, r *http.Request, userID int64, lc listContext, notice string) {
	a.renderIndexWith(w, r, userID, lc, "", notice, nil, 0, hideReadFrom(r))
}
```

Change `renderIndexWith` (currently lines 252-254) from:

```go
func (a *App) renderIndexWith(w http.ResponseWriter, r *http.Request, userID int64, lc listContext, formErr, notice string, candidates []FeedCandidate, selectedFolderID int64) {
	a.renderPanes(w, r, userID, lc, formErr, notice, candidates, selectedFolderID, articleView{})
}
```

to:

```go
func (a *App) renderIndexWith(w http.ResponseWriter, r *http.Request, userID int64, lc listContext, formErr, notice string, candidates []FeedCandidate, selectedFolderID int64, hideRead bool) {
	a.renderPanes(w, r, userID, lc, formErr, notice, candidates, selectedFolderID, articleView{}, hideRead)
}
```

Update the one other caller of `renderIndexWith`, in the discovery-candidates
path (currently around line 677):

```go
	a.renderIndexWith(w, r, userID, lc, "", "", candidates, selectedFolderID)
```

to:

```go
	a.renderIndexWith(w, r, userID, lc, "", "", candidates, selectedFolderID, hideReadFrom(r))
```

Change `renderPanes`'s signature and its `Tree:` line (currently lines
261-292) from:

```go
func (a *App) renderPanes(w http.ResponseWriter, r *http.Request, userID int64, lc listContext, formErr, notice string, candidates []FeedCandidate, selectedFolderID int64, art articleView) {
	ctx := r.Context()
	...
	view := indexView{
		Tree:             viewTree(tree, lc.SubID, lc.Scope, counts),
		Article:          art,
		Error:            formErr,
		Notice:           notice,
		Candidates:       candidates,
		SelectedFolderID: selectedFolderID,
	}
```

to:

```go
func (a *App) renderPanes(w http.ResponseWriter, r *http.Request, userID int64, lc listContext, formErr, notice string, candidates []FeedCandidate, selectedFolderID int64, art articleView, hideRead bool) {
	ctx := r.Context()
	...
	view := indexView{
		Tree:             viewTree(tree, lc.SubID, lc.Scope, counts, hideRead),
		Article:          art,
		Error:            formErr,
		Notice:           notice,
		Candidates:       candidates,
		SelectedFolderID: selectedFolderID,
	}
```

(everything else in `renderPanes` between those two shown snippets is
unchanged). A few lines further down in the same function, where
`view.List` is built:

```go
	view.List = viewList(items, listTitleStr, lc.Scope, lc.SubID, lc.Filter, basePathFor(lc.Scope, lc.SubID), search)
```

add immediately after it:

```go
	view.List.HideRead = hideRead
```

Update `renderArticle`'s own direct `viewTree` call (currently line 427)
from:

```go
		Tree:    viewTree(tree, lc.SubID, lc.Scope, counts),
```

to:

```go
		Tree:    viewTree(tree, lc.SubID, lc.Scope, counts, hideReadFrom(r)),
```

`renderArticle` also has the *other* direct call to `renderPanes` in this
file (its non-HTMX fallback, currently line 422) — this is the second and
last external call site of `renderPanes` (the first is inside
`renderIndexWith`, already updated above), so it needs the same new
argument. Change:

```go
	if !web.IsHTMX(r) {
		a.renderPanes(w, r, userID, lc, "", "", nil, 0, viewArticle(item, page.Shell, lc, showFull))
		return
	}
```

to:

```go
	if !web.IsHTMX(r) {
		a.renderPanes(w, r, userID, lc, "", "", nil, 0, viewArticle(item, page.Shell, lc, showFull), hideReadFrom(r))
		return
	}
```

- [ ] **Step 5: Add the `prefs` handler and route**

In `internal/apps/reader/handlers.go`, add near `markAllRead` (which already
shows the `formContext(r, 0)` pattern this reuses):

```go
// prefs sets the hide-read-feeds preference — a plain POST rather than a
// client-side cookie write, for the same reason Notes' show-completed
// toggle is: it changes what the tree query renders, and every reader
// action already goes through a form/hx-post pair so this works with
// JavaScript off too.
func (a *App) prefs(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	raw := r.PostFormValue("hide_read")
	if raw != "0" && raw != "1" {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     HideReadCookie,
		Value:    raw,
		Path:     "/reader/",
		HttpOnly: true,
		Secure:   a.deps.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   hideReadCookieMaxAge,
	})

	// The form carries the list it fired from, same as markAllRead, so the
	// re-render stays on the feed/scope the toggle was clicked from.
	lc := formContext(r, 0)
	// The value just toggled, not hideReadFrom(r): r still carries whatever
	// the browser sent on this request, before the SetCookie above, which
	// the browser will only start sending back on its next one.
	a.renderIndexWith(w, r, userID, lc, "", "", nil, 0, raw == "1")
}
```

In `internal/apps/reader/app.go`, add the route inside `Mount` (currently
line 110 is `r.HandleFunc("POST /read-all", a.markAllRead)`; add the new
route right after it):

```go
	r.HandleFunc("POST /prefs", a.prefs)
```

- [ ] **Step 6: Add the toolbar toggle button**

In `internal/apps/reader/templates/panes.partial.html`, inside
`{{define "reader-toolbar"}}`'s `.reader-filters` nav (currently lines
102-115), add the toggle as a fourth control after the "All" pill:

```html
  <nav class="reader-filters" aria-label="Filter">
    <a href="{{.BasePath}}?filter=unread{{if .Query}}&amp;q={{.Query}}{{end}}" hx-get="{{.BasePath}}?filter=unread{{if .Query}}&amp;q={{.Query}}{{end}}"
       hx-target="#reader-panes" hx-swap="outerHTML" hx-push-url="true"
       class="toolbar-btn{{if eq .Filter "unread"}} toolbar-btn-active{{end}}"
       {{if eq .Filter "unread"}}aria-current="page"{{end}}>Unread</a>
    <a href="{{.BasePath}}?filter=starred{{if .Query}}&amp;q={{.Query}}{{end}}" hx-get="{{.BasePath}}?filter=starred{{if .Query}}&amp;q={{.Query}}{{end}}"
       hx-target="#reader-panes" hx-swap="outerHTML" hx-push-url="true"
       class="toolbar-btn{{if eq .Filter "starred"}} toolbar-btn-active{{end}}"
       {{if eq .Filter "starred"}}aria-current="page"{{end}}>Starred</a>
    <a href="{{.BasePath}}?filter=all{{if .Query}}&amp;q={{.Query}}{{end}}" hx-get="{{.BasePath}}?filter=all{{if .Query}}&amp;q={{.Query}}{{end}}"
       hx-target="#reader-panes" hx-swap="outerHTML" hx-push-url="true"
       class="toolbar-btn{{if eq .Filter "all"}} toolbar-btn-active{{end}}"
       {{if eq .Filter "all"}}aria-current="page"{{end}}>All</a>
    <form method="post" action="/reader/prefs" class="reader-hide-read-form"
          hx-post="/reader/prefs" hx-target="#reader-panes" hx-swap="outerHTML">
      {{template "reader-ctx" .}}
      <input type="hidden" name="hide_read" value="{{if .HideRead}}0{{else}}1{{end}}">
      <button type="submit" class="toolbar-btn{{if .HideRead}} toolbar-btn-active{{end}}"
              aria-pressed="{{if .HideRead}}true{{else}}false{{end}}"
              aria-label="Hide feeds with nothing unread">{{ticon "eye-off"}}</button>
    </form>
  </nav>
```

(`reader-toolbar` already receives `.List` — a `listView` — as its root, per
its own doc comment above `{{define "reader-toolbar"}}`, so `.HideRead` and
`{{template "reader-ctx" .}}` both resolve correctly here exactly as the
existing `.Filter`/`.BasePath`/`.Query` references on the lines above it do.)

- [ ] **Step 7: One small CSS rule for the new form**

In `internal/ui/static/app.css`, right after the existing `.reader-mark-all`
rule (`internal/ui/static/app.css:1796-1802`), add:

```css
.reader-hide-read-form {
	display: contents;
}
```

(`display: contents` makes the `<form>` itself invisible to layout, so its
one `<button>` child sits directly among `.reader-filters`' flex items
exactly like the `<a>` pills beside it, rather than the form wrapping it in
an extra box.)

- [ ] **Step 8: Write `viewTree` filtering tests**

Add to `internal/apps/reader/view_test.go`:

```go
func TestViewTreeHideReadFiltersZeroUnreadFeedsAndEmptyFolders(t *testing.T) {
	folder := TreeFolder{
		Folder: Folder{ID: 1, Name: "Blogs"},
		Subs: []Subscription{
			{ID: 10, Title: "Read feed"},
			{ID: 11, Title: "Unread feed"},
		},
	}
	emptyFolder := TreeFolder{
		Folder: Folder{ID: 2, Name: "All caught up"},
		Subs: []Subscription{
			{ID: 12, Title: "Also read"},
		},
	}
	tree := Tree{
		Folders: []TreeFolder{folder, emptyFolder},
		Root: []Subscription{
			{ID: 20, Title: "Root read"},
			{ID: 21, Title: "Root unread"},
		},
	}
	counts := Counts{BySub: map[int64]int{
		10: 0, 11: 3, 12: 0, 20: 0, 21: 2,
	}}

	t.Run("hideRead false keeps everything", func(t *testing.T) {
		out := viewTree(tree, 0, ScopeAll, counts, false)
		if len(out.Folders) != 2 || len(out.Folders[0].Subs) != 2 || len(out.Folders[1].Subs) != 1 {
			t.Fatalf("hideRead=false changed the tree shape: %+v", out.Folders)
		}
		if len(out.Root) != 2 {
			t.Fatalf("hideRead=false changed root: %+v", out.Root)
		}
	})

	t.Run("hideRead true drops zero-unread subs and empty folders", func(t *testing.T) {
		out := viewTree(tree, 0, ScopeAll, counts, true)
		if len(out.Folders) != 1 || out.Folders[0].ID != 1 {
			t.Fatalf("empty folder was not dropped: %+v", out.Folders)
		}
		if len(out.Folders[0].Subs) != 1 || out.Folders[0].Subs[0].ID != 11 {
			t.Fatalf("read feed was not filtered from the surviving folder: %+v", out.Folders[0].Subs)
		}
		if len(out.Root) != 1 || out.Root[0].ID != 21 {
			t.Fatalf("root feeds were not filtered: %+v", out.Root)
		}
	})

	t.Run("hideRead true keeps the active feed even at zero unread", func(t *testing.T) {
		out := viewTree(tree, 10, ScopeAll, counts, true)
		if len(out.Folders) != 1 || len(out.Folders[0].Subs) != 2 {
			t.Fatalf("active read feed was hidden: %+v", out.Folders)
		}
	})

	t.Run("Empty reflects real subscriptions, not the filtered view", func(t *testing.T) {
		out := viewTree(tree, 0, ScopeAll, counts, true)
		if out.Empty {
			t.Error("Empty is true even though real subscriptions exist, just all currently read")
		}
	})
}
```

- [ ] **Step 9: Run the view tests to verify they pass**

Run: `go test ./internal/apps/reader/... -run TestViewTreeHideRead -v`
Expected: PASS

- [ ] **Step 10: Write handler tests for the prefs endpoint**

Add to `internal/apps/reader/handlers_test.go`:

```go
// TestPrefsTogglesTheHideReadCookie mirrors Notes'
// TestPrefsTogglesTheCookie (internal/apps/notes/handlers_test.go).
func TestPrefsTogglesTheHideReadCookie(t *testing.T) {
	s := newServer(t)

	rec := s.PostHX(t, s.Alice, "/reader/prefs", url.Values{"hide_read": {"1"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == reader.HideReadCookie {
			got = c
		}
	}
	if got == nil || got.Value != "1" {
		t.Fatalf("hide-read cookie = %+v, want value 1", got)
	}
	if got.MaxAge <= 0 {
		t.Errorf("hide-read cookie MaxAge = %d, want a durable positive value", got.MaxAge)
	}
}

func TestPrefsRejectsAnUnknownHideReadValue(t *testing.T) {
	s := newServer(t)
	for _, v := range []string{"", "true", "2"} {
		rec := s.Post(t, s.Alice, "/reader/prefs", url.Values{"hide_read": {v}})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("hide_read=%q gave %d, want 400", v, rec.Code)
		}
	}
}

// TestHideReadCookieHidesAnAllReadFeedAndItsEmptyFolder is the end-to-end
// path view_test.go's unit tests already cover in isolation: seed a folder
// with one feed, mark it fully read, confirm it disappears from the
// rendered tree only once the cookie is set, and reappears when cleared.
func TestHideReadCookieHidesAnAllReadFeedAndItsEmptyFolder(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	folder, err := s.Store.CreateFolder(ctx, s.Alice.User.ID, "Blogs")
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", &folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
		GUID: "g1", Title: "Only item",
	}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	items, err := s.Store.ItemsForSubscription(ctx, s.Alice.User.ID, sub.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetRead(ctx, s.Alice.User.ID, items[0].ID, true, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/reader/", nil)
	req.AddCookie(&http.Cookie{Name: reader.HideReadCookie, Value: "1"})
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "Blogs") {
		t.Errorf("hide_read=1 still shows the all-read folder:\n%s", rec.Body.String())
	}

	doc := s.Get(t, s.Alice, "/reader/")
	if !strings.Contains(doc.Text(), "Blogs") {
		t.Error("folder is gone even with the hide-read cookie absent")
	}
}
```

- [ ] **Step 11: Run the new handler tests to verify they pass**

Run: `go test ./internal/apps/reader/... -run 'TestPrefs|TestHideRead' -v`
Expected: PASS

- [ ] **Step 12: Run the full reader package test suite**

Run: `go test ./internal/apps/reader/... -v`
Expected: PASS — every existing test still passes; the `viewTree` signature
change is the one edit with the widest blast radius in this plan, so this is
the step that catches a missed call site.

- [ ] **Step 13: Manual verification**

Via the `run` skill: mark every item in one feed as read (or subscribe to a
feed with nothing yet), click the new toggle in the toolbar. Confirm that
feed (and, if it was the only feed in a folder, the folder itself)
disappears from the tree, the toggle shows itself pressed/active, and
reloading the page keeps the toggle on (cookie persistence). Click it again
to confirm everything reappears. Then open a feed, mark its last item read
while it is the active feed with the toggle on, and confirm that feed does
*not* disappear while you are looking at it.

- [ ] **Step 14: Commit**

```bash
git add internal/apps/reader/prefs.go internal/apps/reader/handlers.go internal/apps/reader/app.go internal/apps/reader/view.go internal/apps/reader/templates/panes.partial.html internal/apps/reader/view_test.go internal/apps/reader/handlers_test.go internal/ui/toolbar_icons.go internal/ui/static/app.css
git commit -m "feat(reader): hide feeds with no unread items behind a toolbar toggle"
```

---

## Final check

- [ ] Run the whole suite once more end to end: `go test ./...`
- [ ] Push the branch and open the PR (per this repo's branch-protection
  convention — no direct push to `main`).
