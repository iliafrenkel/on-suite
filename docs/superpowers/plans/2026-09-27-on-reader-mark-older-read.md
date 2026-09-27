# ON Reader — Mark Older Than X as Read Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn ON Reader's "Mark all read" into a split button whose menu also offers "Older than 1 day" and "Older than 1 week" (issue #307).

**Architecture:** `Store.MarkAllRead` gains an `olderThan time.Duration` cutoff on `published_at` (0 = everything). `POST /reader/read-all` maps an optional `older_than` form field (`day`/`week`) onto it through a closed allow-list. The template adds a `<details class="outline-menu">` chevron whose two submit buttons live in the same form, so it works with JavaScript off.

**Tech Stack:** Go (net/http, database/sql over SQLite), html/template, htmx, plain CSS in `internal/ui/static/app.css`.

**Spec:** [docs/superpowers/specs/2026-09-27-on-reader-mark-older-read-design.md](../specs/2026-09-27-on-reader-mark-older-read-design.md)

## Global Constraints

- Age is measured on `published_at`, never `fetched_at`.
- Rolling window from now: `day` = 24h, `week` = 168h; strict `<` — an item exactly at the cutoff stays unread.
- Scope = the list being viewed (feed / All / Starred), same as today's Mark all read, keeping the existing `fetched_at >= sub.added_at` predicate. Search query is ignored.
- `older_than` allow-list: empty → no cutoff, `day`, `week`; anything else → 400 and nothing changes.
- No confirmation dialog.
- Must work with JavaScript off (plain form POST) and with htmx.
- No new dependencies. `main` is protected: work on branch `reader-mark-older-read`, open a PR.
- Full check must stay green on every commit:
  ```bash
  gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1
  ```

## File Structure

- Modify `internal/apps/reader/store.go` — `MarkAllRead` signature + cutoff clause.
- Modify `internal/apps/reader/handlers.go` — `older_than` parsing in `markAllRead`.
- Modify `internal/apps/reader/templates/panes.partial.html` — split button markup.
- Modify `internal/ui/toolbar_icons.go` — add a `chevron-down` icon.
- Modify `internal/ui/static/app.css` — split-button styling.
- Tests: `internal/apps/reader/state_test.go` (store), `internal/apps/reader/handlers_test.go` (handler + markup), `internal/apps/reader/tombstone_test.go` (caller update only).

---

### Task 1: Store cutoff on `MarkAllRead`

**Files:**
- Modify: `internal/apps/reader/store.go:1174-1216` (`MarkAllRead`)
- Modify: `internal/apps/reader/handlers.go:548` (caller — pass `0`)
- Modify: `internal/apps/reader/tombstone_test.go:31`, `internal/apps/reader/state_test.go:367` (callers — pass `0`)
- Test: `internal/apps/reader/state_test.go`

**Interfaces:**
- Produces: `func (s *Store) MarkAllRead(ctx context.Context, userID int64, scope Scope, subID int64, now time.Time, olderThan time.Duration) (int, error)` — `olderThan <= 0` means no cutoff; otherwise only items with `published_at < now.Add(-olderThan)` are marked.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/reader/state_test.go`:

```go
// unreadGUIDs lists which of a subscription's items the user has not read,
// by GUID, so a test can say exactly which articles a cutoff left alone.
func unreadGUIDs(t *testing.T, f *storeFixture, userID, subID int64) []string {
	t.Helper()
	ctx := context.Background()
	items, err := f.store.ItemsForSubscription(ctx, userID, subID, 50)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range items {
		read, _, err := f.store.ItemState(ctx, userID, it.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !read {
			out = append(out, it.GUID)
		}
	}
	sort.Strings(out)
	return out
}

// The cutoff is on published_at and strict: an item published exactly at
// now-olderThan is not "older than" it and stays unread.
func TestMarkAllReadOlderThanOnlyMarksItemsPublishedBeforeTheCutoff(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name       string
		olderThan  time.Duration
		wantMarked int
		wantUnread []string
	}{
		{"no cutoff", 0, 4, nil},
		{"day", 24 * time.Hour, 2, []string{"at-day", "fresh"}},
		{"week", 7 * 24 * time.Hour, 1, []string{"at-day", "fresh", "three-days"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStoreFixture(t)
			ctx := context.Background()
			sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
				{GUID: "fresh", Title: "Fresh", PublishedAt: now.Add(-time.Hour)},
				{GUID: "at-day", Title: "Exactly a day", PublishedAt: now.Add(-24 * time.Hour)},
				{GUID: "three-days", Title: "Three days", PublishedAt: now.Add(-72 * time.Hour)},
				{GUID: "ten-days", Title: "Ten days", PublishedAt: now.Add(-240 * time.Hour)},
			}, now); err != nil {
				t.Fatal(err)
			}

			n, err := f.store.MarkAllRead(ctx, f.alice.ID, reader.ScopeFeed, sub.ID, now, tc.olderThan)
			if err != nil {
				t.Fatalf("MarkAllRead: %v", err)
			}
			if n != tc.wantMarked {
				t.Errorf("marked %d, want %d", n, tc.wantMarked)
			}
			if got := unreadGUIDs(t, f, f.alice.ID, sub.ID); !slices.Equal(got, tc.wantUnread) {
				t.Errorf("unread after cutoff = %v, want %v", got, tc.wantUnread)
			}
		})
	}
}

// The cutoff narrows the scope; it must not widen it. All covers every
// subscription, Starred only starred items — old unstarred ones stay unread.
func TestMarkAllReadOlderThanRespectsAllAndStarredScopes(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	one, err := f.store.Subscribe(ctx, f.alice.ID, "https://one.example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	two, err := f.store.Subscribe(ctx, f.alice.ID, "https://two.example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []reader.Subscription{one, two} {
		if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
			{GUID: "new", Title: "New", PublishedAt: now.Add(-time.Hour)},
			{GUID: "old", Title: "Old", PublishedAt: now.Add(-48 * time.Hour)},
			{GUID: "old-starred", Title: "Old starred", PublishedAt: now.Add(-48 * time.Hour)},
		}, now); err != nil {
			t.Fatal(err)
		}
	}
	items, err := f.store.ItemsForSubscription(ctx, f.alice.ID, one.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.GUID == "old-starred" {
			if err := f.store.SetStarred(ctx, f.alice.ID, it.ID, true, now); err != nil {
				t.Fatal(err)
			}
		}
	}

	if _, err := f.store.MarkAllRead(ctx, f.alice.ID, reader.ScopeStarred, 0, now, 24*time.Hour); err != nil {
		t.Fatalf("MarkAllRead starred: %v", err)
	}
	if got, want := unreadGUIDs(t, f, f.alice.ID, one.ID), []string{"new", "old"}; !slices.Equal(got, want) {
		t.Errorf("feed one after starred cutoff: unread = %v, want %v", got, want)
	}
	if got, want := unreadGUIDs(t, f, f.alice.ID, two.ID), []string{"new", "old", "old-starred"}; !slices.Equal(got, want) {
		t.Errorf("feed two after starred cutoff: unread = %v, want %v (nothing there is starred)", got, want)
	}

	if _, err := f.store.MarkAllRead(ctx, f.alice.ID, reader.ScopeAll, 0, now, 24*time.Hour); err != nil {
		t.Fatalf("MarkAllRead all: %v", err)
	}
	for _, sub := range []reader.Subscription{one, two} {
		if got, want := unreadGUIDs(t, f, f.alice.ID, sub.ID), []string{"new"}; !slices.Equal(got, want) {
			t.Errorf("sub %d after all-scope cutoff: unread = %v, want %v", sub.ID, got, want)
		}
	}
}
```

Add `"slices"` and `"sort"` to the file's imports (today it imports `context`, `errors`, `testing`, `time`, `reader`).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/reader/ -run 'TestMarkAllReadOlderThan' -count=1`
Expected: FAIL — compile error, `too many arguments in call to f.store.MarkAllRead`.

- [ ] **Step 3: Implement the cutoff**

In `internal/apps/reader/store.go`, replace the `MarkAllRead` doc comment and signature, and add the cutoff right after the scope `switch`:

```go
// MarkAllRead marks everything currently unread in a scope as read, returning
// how many rows it touched. A positive olderThan narrows that to items
// published strictly before now-olderThan (#307); zero means everything.
//
// It writes state rows for exactly the items the same predicate as
// ItemsForScope would list — including ItemsForScope's fetched_at cutoff — so
// "mark all read" and "what is unread" can never disagree. The age cutoff is
// on published_at, not fetched_at, because it is the date the list shows and
// sorts by: "older than a day" should match what is on screen.
func (s *Store) MarkAllRead(ctx context.Context, userID int64, scope Scope, subID int64, now time.Time, olderThan time.Duration) (int, error) {
```

and, after the `switch scope { ... }` block and before the `ON CONFLICT` line:

```go
	if olderThan > 0 {
		query += ` AND i.published_at < ?`
		args = append(args, formatTime(now.Add(-olderThan)))
	}
```

Update the three existing callers to pass `0` as the new last argument:
- `internal/apps/reader/handlers.go:548`: `a.store.MarkAllRead(r.Context(), userID, lc.Scope, lc.SubID, a.store.now(), 0)`
- `internal/apps/reader/tombstone_test.go:31`: `f.store.MarkAllRead(ctx, f.alice.ID, reader.ScopeAll, 0, now, 0)`
- `internal/apps/reader/state_test.go:367`: `f.store.MarkAllRead(ctx, f.alice.ID, reader.ScopeFeed, aliceSub.ID, now, 0)`

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/reader/ -run 'TestMarkAllRead' -count=1 -v`
Expected: PASS for the two new tests and the existing `TestMarkAllReadIsScopedAndDoesNotTouchOtherUsers`, `TestMarkAllReadClearsTheFeed`, `TestMarkAllReadStaysOnTheListItFiredFrom`.

- [ ] **Step 5: Full check and commit**

Run the full check from Global Constraints; expect it green.

```bash
git add internal/apps/reader/store.go internal/apps/reader/handlers.go internal/apps/reader/state_test.go internal/apps/reader/tombstone_test.go
git commit -m "feat(reader): MarkAllRead takes an optional published_at cutoff (#307)"
```

---

### Task 2: `older_than` on `POST /reader/read-all`

**Files:**
- Modify: `internal/apps/reader/handlers.go:540-552` (`markAllRead`)
- Test: `internal/apps/reader/handlers_test.go` (next to `TestMarkAllReadClearsTheFeed`, ~line 830)

**Interfaces:**
- Consumes: `Store.MarkAllRead(..., now time.Time, olderThan time.Duration)` from Task 1.
- Produces: `func parseOlderThan(raw string) (time.Duration, bool)` in `handlers.go` — `""`→`(0,true)`, `"day"`→`(24h,true)`, `"week"`→`(168h,true)`, anything else →`(0,false)`. Form field name `older_than`, used by Task 3's markup.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/reader/handlers_test.go` after `TestMarkAllReadClearsTheFeed`:

```go
// seedAged subscribes Alice to one feed holding an item published an hour
// ago ("fresh") and one published three days ago ("old").
func seedAged(t *testing.T, s *apptest.Server[*reader.Store]) (subID int64, fresh, old reader.Item) {
	t.Helper()
	ctx := context.Background()
	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := s.Store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "fresh", Title: "Fresh", PublishedAt: now.Add(-time.Hour)},
		{GUID: "old", Title: "Old", PublishedAt: now.Add(-72 * time.Hour)},
	}, now); err != nil {
		t.Fatal(err)
	}
	items, err := s.Store.ItemsForSubscription(ctx, s.Alice.User.ID, sub.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		switch it.GUID {
		case "fresh":
			fresh = it
		case "old":
			old = it
		}
	}
	return sub.ID, fresh, old
}

func TestMarkOlderThanADayReadLeavesFreshItemsUnread(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	subID, fresh, old := seedAged(t, s)

	rec := s.PostHX(t, s.Alice, "/reader/read-all", url.Values{
		"scope":      {"feed"},
		"sub":        {itoa(subID)},
		"older_than": {"day"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("read-all older_than=day returned %d: %s", rec.Code, rec.Body.String())
	}

	if read, _, err := s.Store.ItemState(ctx, s.Alice.User.ID, old.ID); err != nil || !read {
		t.Errorf("three-day-old item read = %v (err %v), want true", read, err)
	}
	if read, _, err := s.Store.ItemState(ctx, s.Alice.User.ID, fresh.ID); err != nil || read {
		t.Errorf("hour-old item read = %v (err %v), want false — it is not older than a day", read, err)
	}
}

// older_than is a closed set: anything unrecognised is a bad request, not a
// silent fall-back to marking everything read.
func TestMarkAllReadRejectsAnUnknownOlderThan(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	subID, fresh, old := seedAged(t, s)

	for _, raw := range []string{"month", "24h", "Day"} {
		rec := s.PostHX(t, s.Alice, "/reader/read-all", url.Values{
			"scope":      {"feed"},
			"sub":        {itoa(subID)},
			"older_than": {raw},
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("older_than=%q returned %d, want 400", raw, rec.Code)
		}
	}
	for _, it := range []reader.Item{fresh, old} {
		if read, _, err := s.Store.ItemState(ctx, s.Alice.User.ID, it.ID); err != nil || read {
			t.Errorf("%s read = %v (err %v) after rejected requests, want false", it.GUID, read, err)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/reader/ -run 'TestMarkOlderThan|TestMarkAllReadRejects' -count=1`
Expected: FAIL — the `day` test finds the fresh item marked read (the field is ignored today), and the reject test gets 200 instead of 400.

- [ ] **Step 3: Implement the parsing**

In `internal/apps/reader/handlers.go`, replace `markAllRead` with:

```go
func (a *App) markAllRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	olderThan, ok := parseOlderThan(r.PostFormValue("older_than"))
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	// The form carries the list it fired from, so the re-render stays there
	// instead of resetting to All/Unread.
	lc := formContext(r, 0)
	if _, err := a.store.MarkAllRead(r.Context(), userID, lc.Scope, lc.SubID, a.store.now(), olderThan); err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderIndex(w, r, userID, lc, "")
}

// parseOlderThan maps the mark-read split button's older_than choice onto a
// cutoff (#307). A closed set rather than a parsed duration: the menu offers
// exactly these, and an unexpected value should be a 400, not a guess.
func parseOlderThan(raw string) (time.Duration, bool) {
	switch raw {
	case "":
		return 0, true
	case "day":
		return 24 * time.Hour, true
	case "week":
		return 7 * 24 * time.Hour, true
	default:
		return 0, false
	}
}
```

Make sure `"time"` is imported in `handlers.go` (it very likely already is).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/reader/ -run 'TestMarkOlderThan|TestMarkAllRead' -count=1 -v`
Expected: PASS for all of them.

- [ ] **Step 5: Full check and commit**

Run the full check from Global Constraints; expect it green.

```bash
git add internal/apps/reader/handlers.go internal/apps/reader/handlers_test.go
git commit -m "feat(reader): read-all accepts older_than=day|week (#307)"
```

---

### Task 3: Split button markup, icon and CSS

**Files:**
- Modify: `internal/apps/reader/templates/panes.partial.html:448-452` (the `.reader-mark-all` form)
- Modify: `internal/ui/toolbar_icons.go` (add `chevron-down`)
- Modify: `internal/ui/static/app.css:1888-1894` (`.reader-mark-all` rules)
- Test: `internal/apps/reader/handlers_test.go`

**Interfaces:**
- Consumes: form field `older_than` with values `day` / `week` from Task 2.
- Produces: markup — `.reader-mark-all` form containing `button.reader-mark-all-main` and `details.reader-mark-all-menu` holding `button[name=older_than][value=day]` and `button[name=older_than][value=week]`.

- [ ] **Step 1: Write the failing test**

Add to `internal/apps/reader/handlers_test.go` after `TestMarkAllReadRejectsAnUnknownOlderThan`:

```go
// The older-than options are submit buttons inside the same form as Mark all
// read, so they carry the list context and work with JavaScript off.
func TestMarkAllReadSplitButtonOffersOlderThanOptions(t *testing.T) {
	s := newServer(t)
	subID, _ := seedOne(t, s, "a")

	doc := s.Get(t, s.Alice, "/reader/feed/"+itoa(subID))
	doc.MustHave(`form.reader-mark-all input[name=scope]`)
	doc.MustHave(`form.reader-mark-all button.reader-mark-all-main`)
	// htmlassert takes one qualifier per compound selector, so match on
	// name and check value/label per node.
	got := map[string]string{}
	for _, btn := range doc.QueryAll(`form.reader-mark-all details.reader-mark-all-menu button[name=older_than]`) {
		value, _ := htmlassert.Attr(btn, "value")
		got[value] = strings.TrimSpace(htmlassert.Text(btn))
	}
	want := map[string]string{"day": "Older than 1 day", "week": "Older than 1 week"}
	if !maps.Equal(got, want) {
		t.Errorf("older_than menu buttons = %v, want %v", got, want)
	}
	toggle := doc.MustHave(`form.reader-mark-all details.reader-mark-all-menu summary`)
	if got, _ := htmlassert.Attr(toggle, "aria-label"); got != "More mark-read options" {
		t.Errorf("menu toggle aria-label = %q, want %q", got, "More mark-read options")
	}
}
```

Add `"maps"` to the file's imports if not already present.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/apps/reader/ -run TestMarkAllReadSplitButtonOffersOlderThanOptions -count=1`
Expected: FAIL — `button.reader-mark-all-main` not found.

- [ ] **Step 3: Add the chevron icon**

In `internal/ui/toolbar_icons.go`, add this entry to `toolbarIcons` (after `"arrow-left"`):

```go
	"chevron-down": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M6 9l6 6 6-6"/>
	</svg>`,
```

- [ ] **Step 4: Replace the form markup**

In `internal/apps/reader/templates/panes.partial.html`, replace:

```html
    <form class="reader-mark-all" method="post" action="/reader/read-all"
          hx-post="/reader/read-all" hx-target="#reader-panes" hx-swap="outerHTML">
      {{template "reader-ctx" .}}
      <button type="submit" class="toolbar-btn">{{ticon "check"}}Mark all read</button>
    </form>
```

with:

```html
    {{/* Split button (#307): the main half marks the whole list read; the
         chevron opens a <details> menu — the same no-JS disclosure as the
         tree's row menus, see PATTERNS.md — whose buttons submit this same
         form with older_than=day|week. Same form, so they carry reader-ctx
         and work with JavaScript off; htmx sends the clicked button's
         name/value along. The panes re-render on success, which also closes
         the menu. */}}
    <form class="reader-mark-all" method="post" action="/reader/read-all"
          hx-post="/reader/read-all" hx-target="#reader-panes" hx-swap="outerHTML">
      {{template "reader-ctx" .}}
      <button type="submit" class="toolbar-btn reader-mark-all-main">{{ticon "check"}}Mark all read</button>
      <details class="outline-menu reader-mark-all-menu">
        <summary class="toolbar-btn reader-mark-all-toggle" aria-label="More mark-read options">{{ticon "chevron-down"}}</summary>
        <div class="outline-menu-list reader-row-menu-list">
          <button type="submit" name="older_than" value="day">Older than 1 day</button>
          <button type="submit" name="older_than" value="week">Older than 1 week</button>
        </div>
      </details>
    </form>
```

- [ ] **Step 5: Style the split button**

In `internal/ui/static/app.css`, replace the two existing rules:

```css
.reader-mark-all {
	flex: none;
}

.reader-mark-all button {
	white-space: nowrap;
}
```

with:

```css
/* Split button (#307): "Mark all read" and its chevron read as one control —
 * a shared outline, no gap, a hairline between the halves. */
.reader-mark-all {
	flex: none;
	display: flex;
	align-items: stretch;
	border: 1px solid var(--c-border);
	border-radius: var(--radius);
}

.reader-mark-all button {
	white-space: nowrap;
}

.reader-mark-all-main {
	border-top-right-radius: 0;
	border-bottom-right-radius: 0;
}

.reader-mark-all-toggle {
	height: 100%;
	padding: var(--s-1);
	border-left: 1px solid var(--c-border);
	border-top-left-radius: 0;
	border-bottom-left-radius: 0;
	list-style: none;
}
.reader-mark-all-toggle::-webkit-details-marker { display: none; }
.reader-mark-all-menu[open] .reader-mark-all-toggle {
	color: var(--c-text);
	background: var(--c-bg-subtle);
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/apps/reader/ ./internal/ui/ -count=1`
Expected: PASS — including `TestMarkAllReadSplitButtonOffersOlderThanOptions` and the `internal/ui` test that every `{{ticon "..."}}` call resolves (it will catch a typo in `chevron-down`).

- [ ] **Step 7: Check it in the browser**

Build and run locally (`go build ./cmd/onsuite && ./onsuite serve --data-dir ./data`, or the project's `.claude/launch.json` entry if one exists). Open ON Reader on a feed with items of mixed ages and verify:
- The split button renders as one outlined control in the list toolbar; the toolbar does not wrap at the default pane width.
- The chevron opens the menu below it, right-aligned and inside the pane; "Older than 1 day" marks only items older than a day and the menu is closed after the re-render.
- Dark mode and a narrow (≤640px) viewport look right.
- With JavaScript disabled, the menu buttons still work (full-page POST).

Fix any visual issues in `app.css` before committing.

- [ ] **Step 8: Full check and commit**

Run the full check from Global Constraints; expect it green.

```bash
git add internal/apps/reader/templates/panes.partial.html internal/ui/toolbar_icons.go internal/ui/static/app.css internal/apps/reader/handlers_test.go
git commit -m "feat(reader): Mark all read becomes a split button with older-than options (#307)"
```
