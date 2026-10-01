# ON Reader Targeted Swaps Implementation Plan (issue #453)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** List navigation in ON Reader (feed / All / Starred selection, filter pills, search, Mark all read) swaps only the list and a few out-of-band companions, never the tree, so the UI stops flickering.

**Architecture:** One endpoint, two response shapes. When htmx's `HX-Target` header says `reader-list`, `renderPanes` renders a new `list-swap` block (list + OOB toolbar, banner, article, pane toggles, counts, hidden-all note) instead of `panes-oob`. The tree-selection highlight and hide-read removals are copied from `data-*` attributes on the new list by a small `syncTree()` in `reader.js`. Pane widths move from `#reader-panes-row` to `<html>` so the full swaps that remain don't jump either.

**Tech Stack:** Go `html/template`, htmx 2.0.10, vanilla JS (`internal/apps/reader/static/reader.js`), tests with `internal/apptest` + `internal/htmlassert`.

**Spec:** `docs/superpowers/specs/2026-10-01-reader-targeted-swaps-design.md`

## Global Constraints

- Full check must stay green on every commit (AGENTS.md): `gofmt -l .` prints nothing; `go vet ./...`; `go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...`; `go mod tidy && git diff --exit-code go.mod go.sum`; `go test ./... -race -count=1`.
- No new dependencies. Vanilla JS only, CSP-clean (no inline script or handlers).
- Full-page renders (no JS, history restore) and every tree-editing action (subscribe, unsubscribe, rename, move, folder create/delete, refresh, OPML import, hide-read toggle, chooser) keep the existing `panes-oob` response unchanged.
- Never send `hx-swap-oob="delete"` for tree rows: htmx 2.0.10 `console.error`s on every OOB element with no target.
- Match surrounding comment density: this codebase explains *why* in comments above templates and functions.
- Work on branch `fix/453-reader-targeted-swaps` (already created; spec committed). Never push to main; the work lands via PR.

---

### Task 1: `web.HTMXTarget` helper

**Files:**
- Modify: `internal/platform/web/context.go` (after `IsHTMXHistoryRestore`, ~line 69)
- Test: `internal/platform/web/context_test.go`

**Interfaces:**
- Produces: `func HTMXTarget(r *http.Request) string` in package `web`.

- [ ] **Step 1: Write the failing test** — append to `internal/platform/web/context_test.go`:

```go
func TestHTMXTarget(t *testing.T) {
	req := httptest.NewRequest("GET", "/reader/feed/1", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "reader-list")
	if got := web.HTMXTarget(req); got != "reader-list" {
		t.Errorf("HTMXTarget = %q, want reader-list", got)
	}

	plain := httptest.NewRequest("GET", "/reader/feed/1", nil)
	if got := web.HTMXTarget(plain); got != "" {
		t.Errorf("HTMXTarget on a plain request = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `go test ./internal/platform/web/ -run TestHTMXTarget -v`
Expected: FAIL to compile, `undefined: web.HTMXTarget`.

- [ ] **Step 3: Implement** — add to `internal/platform/web/context.go` right after `IsHTMXHistoryRestore`:

```go
// HTMXTarget returns the id of the element an htmx request is going to swap
// into: htmx sends it as HX-Target whenever that element has an id. It is
// empty for a plain request or an id-less target. It lets one endpoint answer
// a narrow target with a narrow fragment instead of re-rendering everything
// around it.
func HTMXTarget(r *http.Request) string {
	return r.Header.Get("HX-Target")
}
```

- [ ] **Step 4: Run the test and make sure it passes**

Run: `go test ./internal/platform/web/ -run TestHTMXTarget -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/web/context.go internal/platform/web/context_test.go
git commit -m "feat(web): add HTMXTarget helper (#453)"
```

---

### Task 2: Stable markup hooks for targeted swaps

Pure markup: give every piece `list-swap` will touch a stable id and an OOB-capable template, and give the tree the ids/data the JS sync will need. Full-page output stays visually identical.

**Files:**
- Modify: `internal/apps/reader/templates/panes.partial.html`
- Modify: `internal/apps/reader/view.go` (`articleView` gains `OOB bool`)
- Test: `internal/apps/reader/handlers_test.go` (append)

**Interfaces:**
- Produces templates (all in `panes.partial.html`):
  - `"shell-oob"` — root is `indexView`; emits `<title>` + OOB `#shell-crumb-tail`.
  - `"reader-toolbar"` — now takes `(dict "List" <listView> "OOB" <bool>)`; renders `<div class="reader-toolbar" id="reader-toolbar">`, with `hx-swap-oob="true"` when OOB.
  - `"reader-toolbar-body"` — root is `listView`; the toolbar's former inner content.
  - `"reader-banner"` — takes `(dict "Error" <string> "Notice" <string> "OOB" <bool>)`; always renders `<div class="reader-banner" id="reader-banner">`, `hidden` when both are empty.
  - `"pane-toggle"` — takes `(dict "ID" <string> "Label" <string> "Checked" <bool> "OOB" <bool>)`.
  - `"tree-hidden-all"` — takes `(dict "Show" <bool> "OOB" <bool>)`; always renders `<p class="empty" id="reader-tree-hidden-all">`, `hidden` unless Show.
  - `"article"` — unchanged root (`articleView`), adds `hx-swap-oob="true"` when `.OOB`.
- Produces DOM hooks: `section#reader-list[data-scope][data-sub]`, `li#reader-sub-<id>`, `details#reader-folder-<id>`, `.reader-tree-nav a[data-scope="all"|"starred"]`.
- Produces Go field: `articleView.OOB bool`.

- [ ] **Step 1: Write the failing test** — append to `internal/apps/reader/handlers_test.go`:

```go
// TestReaderPageCarriesTheTargetedSwapHooks pins the ids and data attributes
// the list-only swap (issue #453) relies on. Every piece "list-swap" updates
// out of band needs a stable id that is present even when it is empty — an
// element missing from the page cannot be swapped back in later — and the
// tree needs ids and data-scope so reader.js can move the highlight without
// the tree being re-rendered.
func TestReaderPageCarriesTheTargetedSwapHooks(t *testing.T) {
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

	doc := s.Get(t, s.Alice, "/reader/feed/"+itoa(sub.ID))

	list := doc.MustHave("#reader-list")
	if got, _ := htmlassert.Attr(list, "data-scope"); got != "feed" {
		t.Errorf("#reader-list data-scope = %q, want feed", got)
	}
	if got, _ := htmlassert.Attr(list, "data-sub"); got != itoa(sub.ID) {
		t.Errorf("#reader-list data-sub = %q, want %d", got, sub.ID)
	}
	doc.MustHave("#reader-toolbar")
	if _, ok := htmlassert.Attr(doc.MustHave("#reader-banner"), "hidden"); !ok {
		t.Error("#reader-banner with no message is not hidden")
	}
	if _, ok := htmlassert.Attr(doc.MustHave("#reader-tree-hidden-all"), "hidden"); !ok {
		t.Error("#reader-tree-hidden-all is not hidden with hide-read off")
	}
	row := doc.MustHave("#reader-sub-" + itoa(sub.ID))
	if got, _ := htmlassert.Attr(row, "class"); !strings.Contains(got, "is-active") {
		t.Errorf("selected feed row class = %q, want is-active", got)
	}
	doc.MustHave("#reader-folder-" + itoa(folder.ID))
	for _, scope := range []string{"all", "starred"} {
		doc.MustHave(`.reader-tree-nav a[data-scope="` + scope + `"]`)
	}
	if got, _ := htmlassert.Attr(doc.MustHave("#reader-article"), "hx-swap-oob"); got != "" {
		t.Errorf("full page #reader-article hx-swap-oob = %q, want none", got)
	}
}
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `go test ./internal/apps/reader/ -run TestReaderPageCarriesTheTargetedSwapHooks -v`
Expected: FAIL, `MustHave("#reader-list")` finds nothing.

- [ ] **Step 3: Add `OOB` to `articleView`** — in `internal/apps/reader/view.go`, at the end of the `articleView` struct (after `FullError string`):

```go
	// OOB marks the article pane as an out-of-band swap. Only "list-swap"
	// sets it: a list navigation replaces #reader-list as its main target,
	// and the article pane has to be emptied alongside it (see PATTERNS.md,
	// "hx-swap-oob for state living outside the swapped fragment").
	OOB bool
```

- [ ] **Step 4: Extract `shell-oob`** — in `panes.partial.html`, replace the `panes-oob` define (line 10) with:

```
{{define "shell-oob"}}<title>{{if .Title}}{{.Title}} · {{end}}ON Suite</title><span id="shell-crumb-tail" hx-swap-oob="true">{{template "shell-crumb-tail" (dict "Title" .Title "Shell" .Shell)}}</span>{{end}}

{{define "panes-oob"}}{{template "shell-oob" .}}{{template "panes" .}}{{end}}
```

Keep the comment above it. Add one sentence at its end: `The <title>/crumb pair lives in "shell-oob" because "list-swap" sends it too.`

- [ ] **Step 5: Toolbar, banner and pane toggles in `panes`** — in the `panes` define, replace:

```
  {{template "reader-toolbar" .List}}
  {{if or .Error .Notice}}
  <div class="reader-banner">
    {{if .Error}}<p class="notice notice-error" role="alert">{{.Error}}</p>{{end}}
    {{if .Notice}}<p class="notice" role="status">{{.Notice}}</p>{{end}}
  </div>
  {{end}}
```

with:

```
  {{template "reader-toolbar" (dict "List" .List "OOB" false)}}
  {{template "reader-banner" (dict "Error" .Error "Notice" .Notice "OOB" false)}}
```

and replace the two checkbox inputs:

```
    <input type="checkbox" id="reader-list-open" class="visually-hidden reader-pane-toggle"
           {{if .List.Selected}}checked{{end}} aria-label="Feeds">
    <input type="checkbox" id="reader-article-open" class="visually-hidden reader-pane-toggle"
           {{if .Article.Selected}}checked{{end}} aria-label="Articles">
```

with:

```
    {{template "pane-toggle" (dict "ID" "reader-list-open" "Label" "Feeds" "Checked" .List.Selected "OOB" false)}}
    {{template "pane-toggle" (dict "ID" "reader-article-open" "Label" "Articles" "Checked" .Article.Selected "OOB" false)}}
```

The long comment above the checkboxes stays where it is.

- [ ] **Step 6: Define `reader-banner` and `pane-toggle`** — add right after the `panes` define's `{{end}}`:

```
{{/* reader-banner always renders, hidden when there is nothing to say, so
     "list-swap" can swap it by id: a banner that only existed while it had a
     message could never be cleared by a later targeted swap, and an error
     from one Mark all read would sit there through every feed click after
     it. .reader-banner sets no display of its own, so [hidden] wins. */}}
{{define "reader-banner"}}<div class="reader-banner" id="reader-banner"{{if not (or .Error .Notice)}} hidden{{end}}{{if .OOB}} hx-swap-oob="true"{{end}}>{{if .Error}}<p class="notice notice-error" role="alert">{{.Error}}</p>{{end}}{{if .Notice}}<p class="notice" role="status">{{.Notice}}</p>{{end}}</div>{{end}}

{{/* pane-toggle is one of the two narrow-viewport drill-down checkboxes (see
     the comment in "panes"). One template, because three responses render
     them: the full panes, "article-oob" and "list-swap" — and an OOB copy
     whose class or label drifted from the original would silently restyle
     it on the next swap. */}}
{{define "pane-toggle"}}<input type="checkbox" id="{{.ID}}" class="visually-hidden reader-pane-toggle"{{if .Checked}} checked{{end}}{{if .OOB}} hx-swap-oob="true"{{end}} aria-label="{{.Label}}">{{end}}
```

- [ ] **Step 7: Split the toolbar** — the existing `{{define "reader-toolbar"}}` block starts with `<div class="reader-toolbar">` and ends with `</div>` + `{{end}}`. Rename that define to `"reader-toolbar-body"` and delete its outermost `<div class="reader-toolbar">` opening line and the matching final `</div>` (the one directly before `{{end}}`). Everything in between stays byte-for-byte. Then add above it (keep the existing comment above the pair, and add a sentence: `The wrapper is separate so "list-swap" can send the same toolbar out of band: the filter links point at the selected list's path.`):

```
{{define "reader-toolbar"}}<div class="reader-toolbar" id="reader-toolbar"{{if .OOB}} hx-swap-oob="true"{{end}}>{{template "reader-toolbar-body" .List}}</div>{{end}}
```

- [ ] **Step 8: Tree hooks** — in the `tree` define:

a) Add `data-scope="all"` to the All nav link and `data-scope="starred"` to the Starred nav link (as a new attribute after `hx-push-url="true"`).

b) Replace:

```
  {{if .Tree.HiddenAll}}
    <p class="empty">Everything is read. Turn off &#8220;hide read&#8221; in the toolbar to see all your feeds.</p>
  {{end}}
```

with:

```
  {{template "tree-hidden-all" (dict "Show" .Tree.HiddenAll "OOB" false)}}
```

c) Change `<details open class="reader-folder">` to `<details open class="reader-folder" id="reader-folder-{{.ID}}">`.

d) In `sub-row`, change `<li class="reader-sub{{if eq .Sub.ID .Tree.ActiveID}} is-active{{end}}">` to `<li class="reader-sub{{if eq .Sub.ID .Tree.ActiveID}} is-active{{end}}" id="reader-sub-{{.Sub.ID}}">`.

e) Add after the `tree` define's `{{end}}`:

```
{{/* tree-hidden-all is the "hide read" note for a tree with nothing left to
     show. Always rendered (hidden when it does not apply) for the same reason
     as reader-banner: "list-swap" toggles it out of band after Mark all read
     empties the last feed, without re-rendering the tree. */}}
{{define "tree-hidden-all"}}<p class="empty" id="reader-tree-hidden-all"{{if not .Show}} hidden{{end}}{{if .OOB}} hx-swap-oob="true"{{end}}>Everything is read. Turn off &#8220;hide read&#8221; in the toolbar to see all your feeds.</p>{{end}}
```

- [ ] **Step 9: List section and article hooks**

a) In the `list` define, change `<section class="reader-list" aria-label="Articles">` to:

```
<section class="reader-list" id="reader-list" aria-label="Articles" data-scope="{{.Scope}}" data-sub="{{.SubID}}">
```

b) Add above `{{define "list"}}` (or extend its existing comment if it has one):

```
{{/* The list is "list-swap"'s main target (issue #453). data-scope/data-sub
     say which list this is, so reader.js can move the tree's highlight to
     match without the tree itself being re-rendered. */}}
```

c) In the `article` define, change `<article class="reader-article" id="reader-article">` to `<article class="reader-article" id="reader-article"{{if .OOB}} hx-swap-oob="true"{{end}}>`.

d) In `article-oob`, replace the inline checkbox `<input type="checkbox" id="reader-article-open" ... aria-label="Articles">` with `{{template "pane-toggle" (dict "ID" "reader-article-open" "Label" "Articles" "Checked" true "OOB" true)}}`.

- [ ] **Step 10: Run the new test and the whole reader package**

Run: `go test ./internal/apps/reader/ -count=1`
Expected: PASS, including `TestReaderPageCarriesTheTargetedSwapHooks`, `TestArticleResponseCarriesTheOOBPaneState` and `TestHideReadCookieHidesAnAllReadFeedAndItsEmptyFolder`. If a test fails because the hidden-all note's text is now in the page while hidden, change that assertion to check the `hidden` attribute instead.

- [ ] **Step 11: Commit**

```bash
git add internal/apps/reader/templates/panes.partial.html internal/apps/reader/view.go internal/apps/reader/handlers_test.go
git commit -m "refactor(reader): stable ids and OOB-capable blocks for targeted swaps (#453)"
```

---

### Task 3: Hidden feed/folder ids for hide-read

**Files:**
- Modify: `internal/apps/reader/view.go` (`treeView`, `listView`, `viewTree`, `filterUnread`)
- Modify: `internal/apps/reader/handlers.go` (`renderPanes`, after `view.List.HideRead = opts.HideRead`, ~line 319)
- Modify: `internal/apps/reader/templates/panes.partial.html` (the `#reader-list` section tag)
- Test: `internal/apps/reader/view_test.go`, `internal/apps/reader/handlers_test.go`

**Interfaces:**
- Consumes: `section#reader-list` from Task 2.
- Produces: `treeView.HiddenSubs, treeView.HiddenFolders []int64`; `listView.HiddenSubs, listView.HiddenFolders []int64`; `func splitUnread(subs []Subscription, counts Counts, activeID int64) (kept []Subscription, dropped []int64)` (replaces `filterUnread`); attributes `data-hidden-subs` / `data-hidden-folders` (space-separated ids) on `#reader-list`.

- [ ] **Step 1: Write the failing unit test** — in `internal/apps/reader/view_test.go`, add these subtests inside `TestViewTreeHideReadFiltersZeroUnreadFeedsAndEmptyFolders` (after the last `t.Run`), and add `"slices"` to the imports if missing:

```go
	t.Run("hideRead true reports what it dropped", func(t *testing.T) {
		out := viewTree(tree, 0, ScopeAll, counts, true)
		if want := []int64{10, 12, 20}; !slices.Equal(out.HiddenSubs, want) {
			t.Errorf("HiddenSubs = %v, want %v", out.HiddenSubs, want)
		}
		if want := []int64{2}; !slices.Equal(out.HiddenFolders, want) {
			t.Errorf("HiddenFolders = %v, want %v", out.HiddenFolders, want)
		}
	})

	t.Run("the active feed is never reported hidden", func(t *testing.T) {
		out := viewTree(tree, 10, ScopeAll, counts, true)
		if want := []int64{12, 20}; !slices.Equal(out.HiddenSubs, want) {
			t.Errorf("HiddenSubs = %v, want %v", out.HiddenSubs, want)
		}
	})

	t.Run("hideRead false hides nothing", func(t *testing.T) {
		out := viewTree(tree, 0, ScopeAll, counts, false)
		if len(out.HiddenSubs) != 0 || len(out.HiddenFolders) != 0 {
			t.Errorf("hideRead=false reported hidden subs %v, folders %v", out.HiddenSubs, out.HiddenFolders)
		}
	})
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `go test ./internal/apps/reader/ -run TestViewTreeHideReadFiltersZeroUnreadFeedsAndEmptyFolders -v`
Expected: FAIL to compile, `out.HiddenSubs undefined`.

- [ ] **Step 3: Implement in `view.go`**

a) Add to `treeView`, after `HiddenAll bool`:

```go
	// HiddenSubs and HiddenFolders are the subscription and folder ids
	// hideRead filtered out of this tree, nil when hideRead is off. A full
	// render never needs them, since it simply leaves those rows out. A
	// list-only swap does: it does not re-render the tree, so the browser
	// still has rows this render would have dropped, and reader.js removes
	// them from these lists (issue #453).
	HiddenSubs    []int64
	HiddenFolders []int64
```

b) Add to `listView`, after `HideRead bool`:

```go
	// HiddenSubs and HiddenFolders copy treeView's, because the list is what
	// a list-only swap delivers: reader.js reads them off #reader-list.
	HiddenSubs    []int64
	HiddenFolders []int64
```

c) Replace the `if hideRead { ... }` block in `viewTree` with:

```go
	var hiddenSubs, hiddenFolders []int64
	if hideRead {
		folders = make([]TreeFolder, 0, len(t.Folders))
		for _, f := range t.Folders {
			var dropped []int64
			f.Subs, dropped = splitUnread(f.Subs, counts, activeID)
			hiddenSubs = append(hiddenSubs, dropped...)
			if len(f.Subs) > 0 {
				folders = append(folders, f)
			} else {
				hiddenFolders = append(hiddenFolders, f.ID)
			}
		}
		var dropped []int64
		root, dropped = splitUnread(t.Root, counts, activeID)
		hiddenSubs = append(hiddenSubs, dropped...)
	}
```

and add to the returned `treeView` literal:

```go
		HiddenSubs:    hiddenSubs,
		HiddenFolders: hiddenFolders,
```

d) Replace `filterUnread` (keep its comment, reworded for the new return) with:

```go
// splitUnread drops every subscription with nothing unread, except the
// currently open one: hiding the feed you are actively reading out from
// under you the moment its last item is read would be more surprising than
// useful, and the sidebar catches up as soon as you navigate away from it.
// It returns what it kept and the ids of what it dropped.
func splitUnread(subs []Subscription, counts Counts, activeID int64) (kept []Subscription, dropped []int64) {
	kept = make([]Subscription, 0, len(subs))
	for _, s := range subs {
		if s.ID == activeID || counts.BySub[s.ID] > 0 {
			kept = append(kept, s)
		} else {
			dropped = append(dropped, s.ID)
		}
	}
	return kept, dropped
}
```

- [ ] **Step 4: Run the unit test and make sure it passes**

Run: `go test ./internal/apps/reader/ -run TestViewTreeHideReadFiltersZeroUnreadFeedsAndEmptyFolders -v`
Expected: PASS.

- [ ] **Step 5: Write the failing handler test** — append to `handlers_test.go`:

```go
// TestListCarriesTheFeedsHideReadDropped pins the data reader.js uses to keep
// a tree it no longer re-renders in step with "hide read" (issue #453): the
// list names every feed and folder the server's filtered tree dropped.
func TestListCarriesTheFeedsHideReadDropped(t *testing.T) {
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
	if _, err := s.Store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{GUID: "g1", Title: "Only item"}}, time.Now().UTC()); err != nil {
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
	list := htmlassert.Parse(t, rec.Body.String()).MustHave("#reader-list")
	if got, _ := htmlassert.Attr(list, "data-hidden-subs"); got != itoa(sub.ID) {
		t.Errorf("data-hidden-subs = %q, want %d", got, sub.ID)
	}
	if got, _ := htmlassert.Attr(list, "data-hidden-folders"); got != itoa(folder.ID) {
		t.Errorf("data-hidden-folders = %q, want %d", got, folder.ID)
	}

	plain := s.Get(t, s.Alice, "/reader/").MustHave("#reader-list")
	if got, _ := htmlassert.Attr(plain, "data-hidden-subs"); got != "" {
		t.Errorf("hide-read off: data-hidden-subs = %q, want empty", got)
	}
}
```

- [ ] **Step 6: Run it to make sure it fails**

Run: `go test ./internal/apps/reader/ -run TestListCarriesTheFeedsHideReadDropped -v`
Expected: FAIL, `data-hidden-subs = ""`.

- [ ] **Step 7: Wire it through**

a) In `handlers.go` `renderPanes`, directly after `view.List.HideRead = opts.HideRead`:

```go
	view.List.HiddenSubs, view.List.HiddenFolders = view.Tree.HiddenSubs, view.Tree.HiddenFolders
```

b) In `panes.partial.html`, extend the section tag from Task 2 to:

```
<section class="reader-list" id="reader-list" aria-label="Articles" data-scope="{{.Scope}}" data-sub="{{.SubID}}" data-hidden-subs="{{range $i, $id := .HiddenSubs}}{{if $i}} {{end}}{{$id}}{{end}}" data-hidden-folders="{{range $i, $id := .HiddenFolders}}{{if $i}} {{end}}{{$id}}{{end}}">
```

and add to the comment above `list`: `data-hidden-subs/-folders list what "hide read" filtered out of the tree, for the same reason.`

- [ ] **Step 8: Run the reader package**

Run: `go test ./internal/apps/reader/ -count=1`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/apps/reader/view.go internal/apps/reader/view_test.go internal/apps/reader/handlers.go internal/apps/reader/templates/panes.partial.html internal/apps/reader/handlers_test.go
git commit -m "feat(reader): list reports the feeds hide-read dropped (#453)"
```

---

### Task 4: `list-swap` response and retargeted controls

**Files:**
- Modify: `internal/apps/reader/templates/panes.partial.html`
- Modify: `internal/apps/reader/handlers.go` (`renderPanes` HTMX branch, ~lines 337-347)
- Test: `internal/apps/reader/handlers_test.go` (append)

**Interfaces:**
- Consumes: `web.HTMXTarget` (Task 1); `shell-oob`, `reader-toolbar`, `reader-banner`, `pane-toggle`, `tree-hidden-all`, `articleView.OOB` (Task 2); `counts-oob` (existing).
- Produces: template `"list-swap"` (root `indexView`); the targeted controls carry `hx-target="#reader-list"`.

- [ ] **Step 1: Write the failing tests** — append to `handlers_test.go`:

```go
// getHXTarget issues an htmx GET aimed at the given swap target id, the way
// htmx itself sends it: HX-Target names the element it will swap into.
func getHXTarget(t *testing.T, s *apptest.Server[*reader.Store], path, target string) *htmlassert.Doc {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("HX-Request", "true")
	if target != "" {
		req.Header.Set("HX-Target", target)
	}
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s (target %q) = %d: %s", path, target, rec.Code, rec.Body.String())
	}
	return htmlassert.Parse(t, rec.Body.String())
}

// TestListTargetSwapsOnlyTheListAndItsCompanions is the core of issue #453:
// a list navigation must not send the tree back. Replacing the tree reflowed
// all three panes (the dragged widths were wiped until htmx settled), reopened
// collapsed folders, reset the tree's scroll and rebuilt every favicon. What
// does change rides along out of band.
func TestListTargetSwapsOnlyTheListAndItsCompanions(t *testing.T) {
	s := newServer(t)
	subID, _ := seedOne(t, s, "g1")

	doc := getHXTarget(t, s, "/reader/feed/"+itoa(subID), "reader-list")

	doc.MustNotHave("#reader-panes")
	doc.MustNotHave("#reader-tree")
	list := doc.MustHave("#reader-list")
	if got, _ := htmlassert.Attr(list, "hx-swap-oob"); got != "" {
		t.Errorf("#reader-list hx-swap-oob = %q, want none: it is the main target", got)
	}
	for _, id := range []string{
		"shell-crumb-tail", "reader-toolbar", "reader-banner", "reader-article",
		"reader-list-open", "reader-article-open", "reader-count-all",
		"reader-count-sub-" + itoa(subID), "reader-tree-hidden-all",
	} {
		if got, _ := htmlassert.Attr(doc.MustHave("#"+id), "hx-swap-oob"); got != "true" {
			t.Errorf("#%s hx-swap-oob = %q, want true", id, got)
		}
	}
	if _, ok := htmlassert.Attr(doc.MustHave("#reader-list-open"), "checked"); !ok {
		t.Error("#reader-list-open is not checked: a phone would not drill into the list")
	}
	if _, ok := htmlassert.Attr(doc.MustHave("#reader-article-open"), "checked"); ok {
		t.Error("#reader-article-open is checked for a list with no article open")
	}
	if got := htmlassert.Text(doc.MustHave("#reader-count-sub-" + itoa(subID))); got != "1" {
		t.Errorf("feed count = %q, want 1", got)
	}
}

// TestOtherTargetsStillGetTheWholePanes pins that only the list target is
// narrowed: every other htmx request (tree edits, refresh, hide-read) keeps
// the full panes response.
func TestOtherTargetsStillGetTheWholePanes(t *testing.T) {
	s := newServer(t)
	subID, _ := seedOne(t, s, "g1")

	for _, target := range []string{"", "reader-panes"} {
		doc := getHXTarget(t, s, "/reader/feed/"+itoa(subID), target)
		doc.MustHave("#reader-panes")
		doc.MustHave("#reader-tree")
	}
}

// TestMarkAllReadIntoTheListTarget covers the POST side of the list target:
// the form sits in the list, so its response is a list swap too, carrying the
// zeroed counts.
func TestMarkAllReadIntoTheListTarget(t *testing.T) {
	s := newServer(t)
	subID, _ := seedOne(t, s, "a", "b")

	form := url.Values{"scope": {"feed"}, "sub": {itoa(subID)}}
	form.Set(web.CSRFFormField, s.CSRFToken(t, s.Alice))
	req := httptest.NewRequest(http.MethodPost, "/reader/read-all", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "reader-list")
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /reader/read-all = %d: %s", rec.Code, rec.Body.String())
	}

	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustNotHave("#reader-panes")
	doc.MustHave("#reader-list")
	if got := htmlassert.Text(doc.MustHave("#reader-count-sub-" + itoa(subID))); got != "" {
		t.Errorf("feed count after mark-all-read = %q, want empty", got)
	}
}

// TestListNavigationControlsTargetTheList pins which controls use the narrow
// swap. The list navigations do; the tree-editing ones keep targeting the
// whole panes, because they really do change the tree.
func TestListNavigationControlsTargetTheList(t *testing.T) {
	s := newServer(t)
	subID, _ := seedOne(t, s, "g1")
	doc := s.Get(t, s.Alice, "/reader/feed/"+itoa(subID))

	var toList []*html.Node
	toList = append(toList, doc.QueryAll(".reader-filters a")...)
	toList = append(toList, doc.QueryAll(".reader-tree-nav a")...)
	toList = append(toList,
		doc.MustHave("#reader-sub-"+itoa(subID)+" a"),
		doc.MustHave("#reader-search-input"),
		doc.MustHave(".reader-mark-all"),
	)
	if len(toList) < 8 {
		t.Fatalf("found %d list-navigation controls, want at least 8", len(toList))
	}
	for _, n := range toList {
		if got, _ := htmlassert.Attr(n, "hx-target"); got != "#reader-list" {
			t.Errorf("<%s> hx-target = %q, want #reader-list", n.Data, got)
		}
	}
	for _, sel := range []string{".reader-hide-read-form", `form[action="/reader/refresh"]`} {
		if got, _ := htmlassert.Attr(doc.MustHave(sel), "hx-target"); got != "#reader-panes" {
			t.Errorf("%s hx-target = %q, want #reader-panes", sel, got)
		}
	}
}
```

- [ ] **Step 2: Run them to make sure they fail**

Run: `go test ./internal/apps/reader/ -run 'TestListTarget|TestOtherTargets|TestMarkAllReadIntoTheListTarget|TestListNavigationControls' -v`
Expected: `TestListTargetSwapsOnlyTheListAndItsCompanions`, `TestMarkAllReadIntoTheListTarget` and `TestListNavigationControlsTargetTheList` FAIL; `TestOtherTargetsStillGetTheWholePanes` PASSES already.

- [ ] **Step 3: Add the `list-swap` block** — in `panes.partial.html`, right after the `panes-oob` define:

```
{{/* list-swap is the response to a list navigation: picking a feed, All or
     Starred, a filter pill, a search, or Mark all read (issue #453). Its main
     target is #reader-list; nothing in it touches the tree, which is what
     keeps the dragged pane widths, collapsed folders, the tree's scroll and
     its favicons exactly as they were. What a list navigation does change
     rides along out of band: the <title>/crumb, the toolbar (its filter links
     point at the selected list), the banner, an emptied article pane, both
     drill-down checkboxes, the sidebar counts and the hide-read note. The
     tree's highlight and hide-read removals are not here: reader.js copies
     them across from the list's data-* attributes. renderPanes picks this
     over "panes-oob" when HX-Target is reader-list. */}}
{{define "list-swap"}}{{template "shell-oob" .}}{{template "list" .List}}{{template "reader-toolbar" (dict "List" .List "OOB" true)}}{{template "reader-banner" (dict "Error" .Error "Notice" .Notice "OOB" true)}}{{template "article" .Article}}{{template "pane-toggle" (dict "ID" "reader-list-open" "Label" "Feeds" "Checked" .List.Selected "OOB" true)}}{{template "pane-toggle" (dict "ID" "reader-article-open" "Label" "Articles" "Checked" .Article.Selected "OOB" true)}}{{template "counts-oob" .}}{{template "tree-hidden-all" (dict "Show" .Tree.HiddenAll "OOB" true)}}{{end}}
```

- [ ] **Step 4: Choose the block in `renderPanes`** — in `handlers.go`, replace:

```go
		if err := a.deps.Render.Fragment(w, http.StatusOK, "reader/index", "panes-oob", view); err != nil {
```

with:

```go
		// A list navigation targets #reader-list and gets only the list and
		// its out-of-band companions back (issue #453); every other htmx
		// request still changes the tree and gets the whole panes.
		block := "panes-oob"
		if web.HTMXTarget(r) == "reader-list" {
			block = "list-swap"
			view.Article.OOB = true
		}
		if err := a.deps.Render.Fragment(w, http.StatusOK, "reader/index", block, view); err != nil {
```

- [ ] **Step 5: Retarget the controls** — in `panes.partial.html`, change `hx-target="#reader-panes"` to `hx-target="#reader-list"` on exactly these (leave `hx-swap="outerHTML"` and everything else as it is):
  - the three filter pills in `reader-toolbar-body` (Unread, Starred, All `<a>`s)
  - the All and Starred `<a>`s in `.reader-tree-nav`
  - the feed `<a>` in `sub-row` (`hx-get="/reader/feed/{{.Sub.ID}}"`)
  - the search `<input id="reader-search-input">`
  - the `<form class="reader-mark-all">`

  Do not touch the hide-read form, refresh, subscribe, rename, move, unsubscribe, folder or OPML controls.

- [ ] **Step 6: Run the reader package**

Run: `go test ./internal/apps/reader/ -count=1`
Expected: PASS, including the four new tests.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/reader/templates/panes.partial.html internal/apps/reader/handlers.go internal/apps/reader/handlers_test.go
git commit -m "feat(reader): list navigation swaps only the list (#453)"
```

---

### Task 5: `reader.js` — tree sync, widths on `<html>`, prefetch cache

**Files:**
- Modify: `internal/apps/reader/static/reader.js`

**Interfaces:**
- Consumes: `#reader-list[data-scope][data-sub][data-hidden-subs][data-hidden-folders]`, `li#reader-sub-<id>`, `details#reader-folder-<id>`, `.reader-tree-nav a[data-scope]` (Tasks 2-3); the `list-swap` response (Task 4).
- Produces: `syncTree()`; pane widths as custom properties on `document.documentElement`.

There is no JS test harness in this repo. This task is verified in the browser in Task 6.

- [ ] **Step 1: Widths on `<html>`** — in `syncPaneWidths`, replace the `var row = document.getElementById("reader-panes-row"); if (!row) return;` lines with `var root = document.documentElement;`, and change every `row.style` in that function to `root.style`. In `initResizablePanes`, change `setWidth` to:

```js
		function setWidth(key, px) {
			document.documentElement.style.setProperty("--reader-" + key + "-w", clampPx(px, key) + "px");
		}
```

Then delete the `syncPaneWidths();` call at the top of `initResizablePanes` and the `DESKTOP_QUERY.addEventListener("change", syncPaneWidths);` line at its end.

- [ ] **Step 2: Register the widths once** — replace `document.addEventListener("DOMContentLoaded", initResizablePanes);` with:

```js
	// The widths live on <html>, not on #reader-panes-row: the row is inside
	// every full panes swap, and a width kept on it was wiped on each one and
	// only restored once htmx settled, so all three panes visibly snapped to
	// the CSS defaults and back (issue #453). Nothing swaps <html>, so this
	// runs once per page load, and so does the media-query listener — it used
	// to be added again on every swap.
	document.addEventListener("DOMContentLoaded", function () {
		syncPaneWidths();
		DESKTOP_QUERY.addEventListener("change", syncPaneWidths);
		initResizablePanes();
	});
```

- [ ] **Step 3: Update the afterSettle comment** — replace the comment block above `document.addEventListener("htmx:afterSettle", ... initResizablePanes ...)` with:

```js
	// A full panes swap (subscribing, refreshing, deleting and the other tree
	// edits) replaces #reader-panes-row and its gutters, so their drag and
	// keyboard listeners have to be bound again. The widths themselves live on
	// <html> and survive the swap. A list-only swap leaves the row alone and
	// needs none of this.
```

- [ ] **Step 4: Add `syncTree`** — add after that `htmx:afterSettle` listener:

```js
	// --- Tree sync after a list-only swap --------------------------------
	//
	// A list navigation (a feed, All/Starred, a filter, a search, Mark all
	// read) swaps only #reader-list, so the tree keeps its DOM: collapsed
	// folders, its scroll position and its favicons all survive (issue #453).
	// What a full render would have drawn differently in the tree arrives on
	// the new list as data-*: which list is selected, and, with "hide read"
	// on, which feeds and folders the server's filtered tree dropped. This
	// copies that across and decides nothing itself. Removing is enough for
	// hide-read: a feed only comes back through a refresh or a new
	// subscription, and both re-render the whole tree.
	function idList(el, name) {
		return (el.getAttribute(name) || "").split(" ").filter(Boolean);
	}

	function removeById(prefix) {
		return function (id) {
			var el = document.getElementById(prefix + id);
			if (el) el.remove();
		};
	}

	function syncTree() {
		var list = document.getElementById("reader-list");
		if (!list) return;
		var scope = list.getAttribute("data-scope");
		var activeRow = scope === "feed" ? "reader-sub-" + list.getAttribute("data-sub") : "";
		document.querySelectorAll(".reader-tree .reader-sub").forEach(function (row) {
			row.classList.toggle("is-active", row.id === activeRow);
		});
		document.querySelectorAll(".reader-tree-nav a[data-scope]").forEach(function (link) {
			link.classList.toggle("toolbar-btn-active", link.getAttribute("data-scope") === scope);
		});
		idList(list, "data-hidden-subs").forEach(removeById("reader-sub-"));
		idList(list, "data-hidden-folders").forEach(removeById("reader-folder-"));
	}

	// Read the list back by id rather than off the event: after an outerHTML
	// swap, the element the event names is not guaranteed to be the one now
	// in the document.
	document.addEventListener("htmx:afterSettle", function (e) {
		if (e.target && e.target.id === "reader-list") syncTree();
	});
```

- [ ] **Step 5: Clear the prefetch cache on a list swap too** — in the `htmx:afterSwap` listener that resets `prefetched`, change the condition to:

```js
		if (e.target && (e.target.id === "reader-panes" || e.target.id === "reader-list")) {
```

and update its comment's first line to `// A new list means new ids, whether it came with the whole panes or alone:`.

- [ ] **Step 6: Run the Go tests** (`reader.js` is embedded and served; make sure nothing pins its contents)

Run: `go test ./internal/apps/reader/ -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/reader/static/reader.js
git commit -m "feat(reader): sync the tree after list-only swaps; keep pane widths on <html> (#453)"
```

---

### Task 6: Browser verification, full check, follow-up issue

**Files:** none changed unless verification finds a bug (fix it in the task that owns it, then re-verify).

- [ ] **Step 1: Rebuild and restart the local instance**

```bash
go build -o /tmp/onsuite-bin ./cmd/onsuite
```

Restart the `onsuite` preview server from `.claude/launch.json` (data dir `/tmp/onsuite-manual-verify`, test user created during the investigation, credentials in the session scratchpad `test-creds.txt`). Open `http://localhost:8080/reader/`.

- [ ] **Step 2: Width stability** — set stored widths, reload, define the probe, and click a feed:

```js
localStorage.setItem('reader.paneWidths', JSON.stringify({tree:300,list:420})); location.reload();
```

```js
window.__probe = async function (clickSel) {
  const log = []; const t0 = performance.now();
  const snap = ev => { const tr = document.querySelector('.reader-tree'), li = document.querySelector('.reader-list');
    log.push({t: +(performance.now()-t0).toFixed(1), ev, tree: Math.round(tr.getBoundingClientRect().width), list: Math.round(li.getBoundingClientRect().width)}); };
  const evs = ['htmx:beforeSwap','htmx:afterSwap','htmx:afterSettle']; const h = e => snap(e.type + '#' + e.target.id);
  evs.forEach(n => document.addEventListener(n, h));
  const mo = new MutationObserver(() => snap('mutation')); mo.observe(document.body, {subtree:true, childList:true, attributes:true});
  const treeBefore = document.querySelector('.reader-tree');
  snap('click'); document.querySelector(clickSel).click();
  await new Promise(r => setTimeout(r, 1000));
  evs.forEach(n => document.removeEventListener(n, h)); mo.disconnect();
  return {sameTree: treeBefore === document.querySelector('.reader-tree'), widths: [...new Set(log.map(l => l.tree + '/' + l.list))], log};
};
await window.__probe('.reader-sub a');
```

Expected: `sameTree: true`, `widths: ["300/420"]` (a single value). Repeat with `'.reader-mark-all-main'` and with `'.reader-filters a'`: same result.

- [ ] **Step 3: Full swap still stable** — run the probe on `'.reader-hide-read-form button'` (a full panes swap). Expected: `sameTree: false` (the tree is re-rendered), `widths` still a single `"300/420"`.

- [ ] **Step 4: Behaviour checks** (use `read_page`/`javascript_tool`):
  - Click feed A, then feed B: only B's row has `is-active`; neither All nor Starred has `toolbar-btn-active`. Click All: All has it, no feed row has `is-active`. Same for Starred.
  - Create a folder, move a feed into it, collapse the folder (`details.open = false`), then click another feed: the folder is still collapsed.
  - Turn hide-read on, open a feed, Mark all read, then click All: that feed's row (and its folder, if it was the only one) is gone. Mark all read in All: `#reader-tree-hidden-all` is visible.
  - After each step, `read_console_messages` with `onlyErrors: true` returns nothing.
  - `resize_window` preset `mobile`, reload, tap a feed: the list pane is showing (`#reader-list-open` checked). Reset with preset `desktop`.

- [ ] **Step 5: Full check**

```bash
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
go mod tidy && git diff --exit-code go.mod go.sum
go test ./... -race -count=1
```

Expected: `gofmt` prints nothing, everything else exits 0.

- [ ] **Step 6: File the out-of-scope follow-up** as a GitHub issue: "reader: dead favicons flash a broken image on every full panes swap". Body: on full swaps, an `<img class="reader-favicon">` whose proxy request fails shows as a broken image for ~10ms until the capture-phase error listener in `reader.js` swaps in the RSS glyph. List navigation no longer triggers it (#453), but tree edits still do. Possible fix: the server remembers dead favicons and renders the glyph directly.

- [ ] **Step 7: Hand off to superpowers:finishing-a-development-branch** (open a PR that closes #453; never merge).
