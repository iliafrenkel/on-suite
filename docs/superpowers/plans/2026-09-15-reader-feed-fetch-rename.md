# ON Reader fetch-on-add, single-feed refresh, and rename Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fetch a newly added feed immediately instead of waiting for the next
poller tick, let a user refresh one feed on demand, and let a user rename a
feed.

**Architecture:** A new `Poller.FetchNow(ctx, feedID)` fetches one feed
regardless of its due schedule, reusing the existing `pollOne` fetch/parse/save
logic. `a.subscribe` calls it synchronously (bounded by a timeout) right after
creating the subscription. A new `POST /sub/{id}/refresh` route calls the same
`FetchNow` on demand. Renaming writes to the `reader_subs.title` override
column that already exists and is already read by `Subscription.DisplayName()`
— only the write path and UI are new.

**Tech Stack:** Go (`net/http`, `database/sql`), Go `html/template`, HTMX,
SQLite.

## Global Constraints

- Follow the spec exactly:
  [2026-09-15-on-reader-feed-fetch-rename-design.md](../specs/2026-09-15-on-reader-feed-fetch-rename-design.md).
- `Store` is the only thing in the `reader` package that touches SQL —
  handlers and the poller call `Store` methods, never raw queries
  (`internal/apps/reader/store.go:45`).
- `ErrNotFound` is returned for both "row does not exist" and "row belongs to
  someone else" — the two must stay indistinguishable
  (`internal/apps/reader/store.go:27-30`).
- Every mutating route follows the existing pattern: auth check → `pathID` →
  `formContext` → store call → `a.fail` on error → `a.renderIndex` on success
  (see `unsubscribe`, `internal/apps/reader/handlers.go:730-750`).
- Every HTMX form pairs `method=post action=...` with matching `hx-post` (not
  `hx-post` alone), plus `{{template "reader-ctx" $.List}}` for CSRF and list
  context (`internal/apps/reader/templates/panes.partial.html:146-150`).
- Run `go build ./...` and `go test ./internal/apps/reader/...` after every
  task; both must pass before moving on.
- Work happens on branch `reader-feed-fetch-rename` (already checked out,
  already has the spec commit). Never push directly to `main` — this work
  lands via a pull request.

---

### Task 1: `Store.FeedByID` — load one feed regardless of due status

**Files:**
- Modify: `internal/apps/reader/store.go` (add after `DueFeeds`, currently
  ending at line 762)
- Test: `internal/apps/reader/store_test.go` (add after
  `TestItemRequiresASubscription`, currently ending at line 312)

**Interfaces:**
- Produces: `func (s *Store) FeedByID(ctx context.Context, feedID int64) (Feed, error)`
  — returns `ErrNotFound` if no such feed exists. Used by Task 2's
  `Poller.FetchNow`.

- [ ] **Step 1: Write the failing test**

Add to `internal/apps/reader/store_test.go`:

```go
func TestFeedByIDLoadsAFeedRegardlessOfDueStatus(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	feed, err := f.store.FeedByID(ctx, sub.FeedID)
	if err != nil {
		t.Fatalf("FeedByID: %v", err)
	}
	if feed.ID != sub.FeedID {
		t.Errorf("ID = %d, want %d", feed.ID, sub.FeedID)
	}
	if feed.URL != "https://example.com/feed.xml" {
		t.Errorf("URL = %q", feed.URL)
	}

	// A freshly subscribed feed is due immediately (next_fetch_at = now), but
	// FeedByID must not filter on that the way DueFeeds does — it is the
	// "load this specific feed" path, not "load whatever is due".
	if _, err := f.store.FeedByID(ctx, feed.ID+999); !errors.Is(err, reader.ErrNotFound) {
		t.Errorf("FeedByID(missing) = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/apps/reader/... -run TestFeedByIDLoadsAFeedRegardlessOfDueStatus -v`
Expected: FAIL — `f.store.FeedByID undefined`

- [ ] **Step 3: Implement `FeedByID`**

Add to `internal/apps/reader/store.go`, directly after `DueFeeds` (after the
closing brace at line 762):

```go
// FeedByID loads one feed by id, regardless of whether it is due. This is the
// entry point Poller.FetchNow uses to fetch a specific feed on demand — the
// due-filtered DueFeeds is the wrong query for "fetch this one, right now".
func (s *Store) FeedByID(ctx context.Context, feedID int64) (Feed, error) {
	var f Feed
	var next string
	var interval sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, url, resolved_url, title, site_url, etag, last_modified,
		       last_status, last_error, error_count, next_fetch_at, fetch_interval
		  FROM reader_feeds
		 WHERE id = ?`, feedID).Scan(&f.ID, &f.URL, &f.ResolvedURL, &f.Title, &f.SiteURL,
		&f.ETag, &f.LastModified, &f.LastStatus, &f.LastError, &f.ErrorCount,
		&next, &interval)
	if errors.Is(err, sql.ErrNoRows) {
		return Feed{}, ErrNotFound
	}
	if err != nil {
		return Feed{}, fmt.Errorf("reader: load feed: %w", err)
	}
	f.NextFetchAt = parseTime(next)
	if interval.Valid {
		f.FetchInterval = time.Duration(interval.Int64) * time.Second
	}
	return f, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/apps/reader/... -run TestFeedByIDLoadsAFeedRegardlessOfDueStatus -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/apps/reader/store.go internal/apps/reader/store_test.go
git commit -m "feat(reader): add Store.FeedByID for on-demand single-feed fetch"
```

---

### Task 2: `Poller.FetchNow` — fetch one feed regardless of schedule

**Files:**
- Modify: `internal/apps/reader/poll.go` (add after `PollDue`, currently
  ending at line 116)
- Test: `internal/apps/reader/poll_test.go` (add after
  `TestPollDueRecordsAFailureWithoutFailingTheRun`, currently ending at
  line 142)

**Interfaces:**
- Consumes: `Store.FeedByID(ctx, feedID) (Feed, error)` (Task 1);
  `Poller.pollOne(ctx, f Feed)` (existing, `poll.go:119`, unexported — same
  package).
- Produces: `func (p *Poller) FetchNow(ctx context.Context, feedID int64) error`
  — used by Task 3 (fetch-on-add) and Task 4 (refresh one feed).

- [ ] **Step 1: Write the failing test**

Add to `internal/apps/reader/poll_test.go`:

```go
func TestFetchNowFetchesAFeedRegardlessOfDueStatus(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(fixture(t, "rss2.xml"))
	}))
	defer srv.Close()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, srv.URL+"/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	client := reader.NewClient("test")
	client.DenyAddr = func(string) error { return nil }
	poller := reader.NewPoller(f.store, client, quietLogger())

	// Mark the feed as not due for another hour, the way a normal poll leaves
	// it — FetchNow must still fetch it, which is the entire point of the
	// method: "refresh this one, right now" cannot wait on next_fetch_at.
	if _, err := f.db.ExecContext(ctx,
		`UPDATE reader_feeds SET next_fetch_at = ? WHERE id = ?`,
		time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano), sub.FeedID); err != nil {
		t.Fatal(err)
	}

	if err := poller.FetchNow(ctx, sub.FeedID); err != nil {
		t.Fatalf("FetchNow: %v", err)
	}

	items, err := f.store.ItemsForSubscription(ctx, f.alice.ID, sub.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("stored %d items, want 1", len(items))
	}
}

func TestFetchNowReturnsErrNotFoundForAMissingFeed(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	client := reader.NewClient("test")
	poller := reader.NewPoller(f.store, client, quietLogger())

	if err := poller.FetchNow(ctx, 999999); !errors.Is(err, reader.ErrNotFound) {
		t.Errorf("FetchNow(missing) = %v, want ErrNotFound", err)
	}
}
```

This second test needs `"errors"` added to the `import` block at the top of
`internal/apps/reader/poll_test.go`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/reader/... -run TestFetchNow -v`
Expected: FAIL — `poller.FetchNow undefined`

- [ ] **Step 3: Implement `FetchNow`**

Add to `internal/apps/reader/poll.go`, directly after `PollDue` (after the
closing brace at line 116):

```go
// FetchNow fetches one feed immediately, bypassing its due schedule. It is
// what adding a feed and an explicit "refresh this feed" both call, so
// neither has to wait for the next scheduled tick.
func (p *Poller) FetchNow(ctx context.Context, feedID int64) error {
	f, err := p.store.FeedByID(ctx, feedID)
	if err != nil {
		return err
	}
	p.pollOne(ctx, f)
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/reader/... -run TestFetchNow -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/apps/reader/poll.go internal/apps/reader/poll_test.go
git commit -m "feat(reader): add Poller.FetchNow to fetch one feed on demand"
```

---

### Task 3: Fetch a feed synchronously when it is subscribed

**Files:**
- Modify: `internal/apps/reader/handlers.go:573-615` (`subscribe`)
- Test: `internal/apps/reader/handlers_test.go` (add after
  `TestSubscribeAddsAFeedToTheTree`, currently ending at line 88)

**Interfaces:**
- Consumes: `Poller.FetchNow(ctx, feedID) error` (Task 2); `a.poller` (existing
  field, `internal/apps/reader/app.go:33`); `sub.FeedID` (existing field,
  `store.go:87`).

- [ ] **Step 1: Write the failing test**

Add to `internal/apps/reader/handlers_test.go`:

```go
// TestSubscribeFetchesTheFeedImmediately guards the whole point of this
// change: articles must be visible right after the add-feed response, not
// only after a separate PollDue call (which TestSubscribePollAndRenderComposedFlow
// already covers as the "old" two-step flow).
func TestSubscribeFetchesTheFeedImmediately(t *testing.T) {
	s, a := newServerWithApp(t)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(fixture(t, "rss2.xml"))
	}))
	defer origin.Close()
	a.AllowPrivateFetchesForTest()

	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{
		"url": {origin.URL + "/feed.xml"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d: %s", rec.Code, rec.Body.String())
	}

	// The subscribe response itself already redirects to the new feed's pane
	// (handlers.go:613), so its own body is the feed pane — no second request
	// needed to see whether the fetch happened.
	if !strings.Contains(rec.Body.String(), "First post") {
		t.Errorf("subscribe response has no article title; fetch-on-add did not happen:\n%s", rec.Body.String())
	}
}

// TestSubscribeStillSucceedsWhenTheFetchFails pins that a slow or broken
// origin must not stop the subscription itself from being created — the
// poller's normal retry/backoff picks it up afterwards.
func TestSubscribeStillSucceedsWhenTheFetchFails(t *testing.T) {
	s, a := newServerWithApp(t)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer origin.Close()
	a.AllowPrivateFetchesForTest()

	rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{
		"url": {origin.URL + "/feed.xml"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d: %s", rec.Code, rec.Body.String())
	}

	doc := s.Get(t, s.Alice, "/reader/")
	if !strings.Contains(doc.Text(), origin.URL+"/feed.xml") {
		t.Errorf("subscription missing after a failed fetch-on-add:\n%s", doc.Text())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/reader/... -run TestSubscribeFetchesTheFeedImmediately -v`
Expected: FAIL — no "First post" in the response body (the feed has not been
fetched yet)

- [ ] **Step 3: Wire `FetchNow` into `subscribe`**

In `internal/apps/reader/handlers.go`, the `subscribe` function currently
reads (lines 602-614):

```go
	sub, err := a.store.Subscribe(r.Context(), userID, feedURL, folderParam(r))
	if err != nil {
		if errors.Is(err, ErrInvalidURL) {
			a.renderIndex(w, r, userID, lc, "That is not a feed address.")
			return
		}
		a.fail(w, r, err)
		return
	}
	// Adding a feed selects it, which is the one case where the new state wins
	// over the list the form came from.
	lc.Scope, lc.SubID = ScopeFeed, sub.ID
	a.renderIndex(w, r, userID, lc, "")
}
```

Replace it with:

```go
	sub, err := a.store.Subscribe(r.Context(), userID, feedURL, folderParam(r))
	if err != nil {
		if errors.Is(err, ErrInvalidURL) {
			a.renderIndex(w, r, userID, lc, "That is not a feed address.")
			return
		}
		a.fail(w, r, err)
		return
	}

	// Fetch once, synchronously, so the feed already has articles by the time
	// this response renders it — a slow or broken origin only costs this
	// request up to fetchOnAddTimeout; either way the subscription itself is
	// already committed above, and the poller's normal retry/backoff takes
	// over from here.
	fetchCtx, cancel := context.WithTimeout(r.Context(), fetchOnAddTimeout)
	defer cancel()
	if err := a.poller.FetchNow(fetchCtx, sub.FeedID); err != nil {
		a.deps.Log.Info("reader fetch-on-add failed", "feed_id", sub.FeedID, "error", err)
	}

	// Adding a feed selects it, which is the one case where the new state wins
	// over the list the form came from.
	lc.Scope, lc.SubID = ScopeFeed, sub.ID
	a.renderIndex(w, r, userID, lc, "")
}
```

Add the new constant next to `discoveryTimeout` (`internal/apps/reader/handlers.go:621`):

```go
// fetchOnAddTimeout bounds the synchronous fetch subscribe performs so a
// slow origin cannot hold the add-feed request open indefinitely. A var, not
// a const, for the same reason discoveryTimeout is: SetDiscoveryTimeoutForTest's
// pattern is available if a test ever needs to shrink it.
var fetchOnAddTimeout = 10 * time.Second
```

`internal/apps/reader/handlers.go` already imports `"context"` and `"time"` —
no import changes needed.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/reader/... -run TestSubscribe -v`
Expected: PASS (this also re-runs the pre-existing `TestSubscribe*` tests —
all must still pass)

- [ ] **Step 5: Run the whole reader package's tests**

Run: `go test ./internal/apps/reader/...`
Expected: PASS — confirms `TestSubscribePollAndRenderComposedFlow` (which
calls `PollDue` a second time after subscribing) still passes: `SaveItems` is
idempotent on GUID, so fetching twice stores the same one item, not two.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/reader/handlers.go internal/apps/reader/handlers_test.go
git commit -m "feat(reader): fetch a feed immediately when it is subscribed"
```

---

### Task 4: Refresh a single feed on demand

**Files:**
- Modify: `internal/apps/reader/store.go` (add after `DeleteFolder`, currently
  ending at line 312)
- Modify: `internal/apps/reader/handlers.go` (add after `unsubscribe`,
  currently ending at line 750)
- Modify: `internal/apps/reader/app.go:99` (add route)
- Modify: `internal/apps/reader/templates/panes.partial.html` (both feed-menu
  blocks: lines 210-223 and 235-248)
- Test: `internal/apps/reader/store_test.go` and
  `internal/apps/reader/handlers_test.go`

**Interfaces:**
- Consumes: `Poller.FetchNow(ctx, feedID) error` (Task 2).
- Produces: `func (s *Store) FeedIDForSub(ctx context.Context, userID, subID int64) (int64, error)`;
  route `POST /sub/{id}/refresh`; handler `a.refreshOne`.

- [ ] **Step 1: Write the failing store test**

Add to `internal/apps/reader/store_test.go`:

```go
func TestFeedIDForSubIsScopedToTheOwner(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	feedID, err := f.store.FeedIDForSub(ctx, f.alice.ID, sub.ID)
	if err != nil {
		t.Fatalf("FeedIDForSub(alice): %v", err)
	}
	if feedID != sub.FeedID {
		t.Errorf("feedID = %d, want %d", feedID, sub.FeedID)
	}

	if _, err := f.store.FeedIDForSub(ctx, f.bob.ID, sub.ID); !errors.Is(err, reader.ErrNotFound) {
		t.Errorf("FeedIDForSub(bob) = %v, want ErrNotFound for someone else's subscription", err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/apps/reader/... -run TestFeedIDForSubIsScopedToTheOwner -v`
Expected: FAIL — `f.store.FeedIDForSub undefined`

- [ ] **Step 3: Implement `FeedIDForSub`**

Add to `internal/apps/reader/store.go`, directly after `DeleteFolder` (after
the closing brace at line 312):

```go
// FeedIDForSub returns the shared feed id behind one user's subscription. The
// user_id predicate is the same ownership check Unsubscribe uses: a
// subscription belonging to somebody else is reported exactly as one that
// does not exist.
func (s *Store) FeedIDForSub(ctx context.Context, userID, subID int64) (int64, error) {
	var feedID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT feed_id FROM reader_subs WHERE id = ? AND user_id = ?`, subID, userID).Scan(&feedID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("reader: load subscription feed: %w", err)
	}
	return feedID, nil
}
```

- [ ] **Step 4: Run the store test to verify it passes**

Run: `go test ./internal/apps/reader/... -run TestFeedIDForSubIsScopedToTheOwner -v`
Expected: PASS

- [ ] **Step 5: Write the failing handler test**

Add to `internal/apps/reader/handlers_test.go`:

```go
// TestRefreshFeedControlFetchesOneFeedRegardlessOfSchedule pins the per-row
// "Refresh feed" action: it must fetch the one feed even though it is not due
// (a real subscription is never due again for the default 30-minute interval
// right after its own fetch-on-add).
func TestRefreshFeedControlFetchesOneFeedRegardlessOfSchedule(t *testing.T) {
	s, a := newServerWithApp(t)
	ctx := context.Background()
	a.AllowPrivateFetchesForTest()

	var hits int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(fixture(t, "rss2.xml"))
	}))
	defer origin.Close()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, origin.URL+"/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Mark the feed as freshly fetched and not due for another hour, exactly
	// what fetch-on-add leaves behind — "Refresh feed" must still fetch it.
	if _, err := s.Store.DB().ExecContext(ctx,
		`UPDATE reader_feeds SET next_fetch_at = ? WHERE id = ?`,
		time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano), sub.FeedID); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/reader/")
	btn := doc.MustHave("button.reader-sub-refresh")
	if got, _ := htmlassert.Attr(btn, "hx-post"); got != "/reader/sub/"+itoa(sub.ID)+"/refresh" {
		t.Errorf("refresh button hx-post = %q", got)
	}

	rec := s.PostHX(t, s.Alice, "/reader/sub/"+itoa(sub.ID)+"/refresh", url.Values{})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh returned %d: %s", rec.Code, rec.Body.String())
	}
	if hits != 1 {
		t.Fatalf("origin hit %d times, want 1", hits)
	}

	items, err := s.Store.ItemsForSubscription(ctx, s.Alice.User.ID, sub.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("stored %d items, want 1", len(items))
	}
}

// TestRefreshFeedIsScopedToTheOwner guards against refreshing (and disclosing
// the existence of) somebody else's subscription id.
func TestRefreshFeedIsScopedToTheOwner(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	sub, err := s.Store.Subscribe(ctx, s.Bob.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	rec := s.PostHX(t, s.Alice, "/reader/sub/"+itoa(sub.ID)+"/refresh", url.Values{})
	if rec.Code != http.StatusNotFound {
		t.Errorf("refreshing another user's subscription returned %d, want 404", rec.Code)
	}
}
```

`s.Bob` is an existing field on `apptest.Server` (`internal/apptest/apptest.go:95`)
— a second logged-in session, already used this way elsewhere in this package
(e.g. `internal/apps/reader/handlers_test.go:1234`).

- [ ] **Step 6: Run the handler tests to verify they fail**

Run: `go test ./internal/apps/reader/... -run TestRefreshFeed -v`
Expected: FAIL — no `button.reader-sub-refresh` in the tree, and
`/reader/sub/{id}/refresh` 404s (route does not exist yet)

- [ ] **Step 7: Add the route**

In `internal/apps/reader/app.go`, change:

```go
	r.HandleFunc("POST /sub/{id}/delete", a.unsubscribe)
```

to:

```go
	r.HandleFunc("POST /sub/{id}/delete", a.unsubscribe)
	r.HandleFunc("POST /sub/{id}/refresh", a.refreshOne)
```

- [ ] **Step 8: Add the handler**

Add to `internal/apps/reader/handlers.go`, directly after `unsubscribe` (after
the closing brace at line 750):

```go
// refreshOne fetches one feed immediately, regardless of its schedule — the
// per-row counterpart to refresh's "refresh all feeds".
func (a *App) refreshOne(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	subID, ok := a.pathID(w, r)
	if !ok {
		return
	}
	lc := formContext(r, 0)
	feedID, err := a.store.FeedIDForSub(r.Context(), userID, subID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.poller.FetchNow(r.Context(), feedID); err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	a.renderIndex(w, r, userID, lc, "")
}
```

- [ ] **Step 9: Add the "Refresh feed" menu item and route it in the auth test**

In `internal/apps/reader/handlers_test.go`, `TestEveryReaderRouteIsBehindAuth`
(line 41), add a row so the new route is proven to require auth too:

```go
		{http.MethodPost, "/reader/subscribe"},
		{http.MethodPost, "/reader/sub/1/delete"},
		{http.MethodPost, "/reader/sub/1/refresh"},
```

In `internal/apps/reader/templates/panes.partial.html`, both feed-menu blocks
get a new button between "Feed URL" and the "Unsubscribe" form. For the
folder-nested feed block (currently lines 210-223):

```html
            <details class="outline-menu">
              <summary class="outline-menu-toggle quiet" aria-label="Feed actions">{{ticon "more"}}</summary>
              <div class="outline-menu-list reader-row-menu-list">
                <button type="button" class="reader-copy-feed-url" data-feed-url="{{.FeedURL}}"
                        aria-label="Copy feed URL for {{.DisplayName}}">Feed URL</button>
                <form method="post" action="/reader/sub/{{.ID}}/refresh">
                  {{template "reader-ctx" $.List}}
                  <button type="submit" class="reader-sub-refresh"
                          hx-post="/reader/sub/{{.ID}}/refresh" hx-target="#reader-panes" hx-swap="outerHTML"
                          aria-label="Refresh {{.DisplayName}}">Refresh feed</button>
                </form>
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

And identically for the root-level feed block (currently lines 235-248):

```html
        <details class="outline-menu">
          <summary class="outline-menu-toggle quiet" aria-label="Feed actions">{{ticon "more"}}</summary>
          <div class="outline-menu-list reader-row-menu-list">
            <button type="button" class="reader-copy-feed-url" data-feed-url="{{.FeedURL}}"
                    aria-label="Copy feed URL for {{.DisplayName}}">Feed URL</button>
            <form method="post" action="/reader/sub/{{.ID}}/refresh">
              {{template "reader-ctx" $.List}}
              <button type="submit" class="reader-sub-refresh"
                      hx-post="/reader/sub/{{.ID}}/refresh" hx-target="#reader-panes" hx-swap="outerHTML"
                      aria-label="Refresh {{.DisplayName}}">Refresh feed</button>
            </form>
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

- [ ] **Step 10: Run the tests to verify they pass**

Run: `go test ./internal/apps/reader/... -run "TestRefreshFeed|TestEveryReaderRouteIsBehindAuth"  -v`
Expected: PASS

- [ ] **Step 11: Run the whole reader package's tests**

Run: `go test ./internal/apps/reader/...`
Expected: PASS

- [ ] **Step 12: Commit**

```bash
git add internal/apps/reader/store.go internal/apps/reader/store_test.go \
        internal/apps/reader/handlers.go internal/apps/reader/handlers_test.go \
        internal/apps/reader/app.go internal/apps/reader/templates/panes.partial.html
git commit -m "feat(reader): add a per-feed refresh action"
```

---

### Task 5: `Store.RenameSubscription`

**Files:**
- Modify: `internal/apps/reader/store.go` (add after `FeedIDForSub`, added by
  Task 4)
- Test: `internal/apps/reader/store_test.go`

**Interfaces:**
- Produces: `func (s *Store) RenameSubscription(ctx context.Context, userID, subID int64, title string) error`
  — used by Task 6's `a.renameSub`.

- [ ] **Step 1: Write the failing test**

Add to `internal/apps/reader/store_test.go`:

```go
func TestRenameSubscriptionSetsAndClearsTheOverride(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Before any poll, DisplayName falls back to the raw URL.
	if got := sub.DisplayName(); got != "https://example.com/feed.xml" {
		t.Fatalf("initial DisplayName = %q", got)
	}

	if err := f.store.RenameSubscription(ctx, f.alice.ID, sub.ID, "  My Feed  "); err != nil {
		t.Fatalf("RenameSubscription: %v", err)
	}
	tree, err := f.store.Tree(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.Root[0].DisplayName(); got != "My Feed" {
		t.Errorf("DisplayName after rename = %q, want trimmed %q", got, "My Feed")
	}

	// Clearing the override (empty string) falls back to the feed's own
	// title/URL again, rather than being rejected as invalid input.
	if err := f.store.RenameSubscription(ctx, f.alice.ID, sub.ID, ""); err != nil {
		t.Fatalf("RenameSubscription(clear): %v", err)
	}
	tree, err = f.store.Tree(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.Root[0].DisplayName(); got != "https://example.com/feed.xml" {
		t.Errorf("DisplayName after clearing = %q, want the raw URL fallback", got)
	}
}

func TestRenameSubscriptionIsScopedToTheOwner(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.RenameSubscription(ctx, f.bob.ID, sub.ID, "Hijacked"); !errors.Is(err, reader.ErrNotFound) {
		t.Errorf("RenameSubscription(bob) = %v, want ErrNotFound for someone else's subscription", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/reader/... -run TestRenameSubscription -v`
Expected: FAIL — `f.store.RenameSubscription undefined`

- [ ] **Step 3: Implement `RenameSubscription`**

Add to `internal/apps/reader/store.go`, directly after `FeedIDForSub` (added
by Task 4):

```go
// RenameSubscription sets or clears this user's custom name for a
// subscription. An empty title (after trimming) is a valid write, not an
// error: it clears the override, and DisplayName falls back to the feed's
// own title again — unlike CreateFolder, an empty name here is meaningful
// rather than invalid.
func (s *Store) RenameSubscription(ctx context.Context, userID, subID int64, title string) error {
	title = strings.TrimSpace(title)
	res, err := s.db.ExecContext(ctx,
		`UPDATE reader_subs SET title = ? WHERE id = ? AND user_id = ?`, title, subID, userID)
	if err != nil {
		return fmt.Errorf("reader: rename subscription: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reader: rename subscription rows: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/reader/... -run TestRenameSubscription -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/apps/reader/store.go internal/apps/reader/store_test.go
git commit -m "feat(reader): add Store.RenameSubscription"
```

---

### Task 6: Rename a feed from the UI

**Files:**
- Modify: `internal/apps/reader/handlers.go` (add after `refreshOne`, added by
  Task 4)
- Modify: `internal/apps/reader/app.go` (add route)
- Modify: `internal/apps/reader/templates/panes.partial.html` (both feed-menu
  blocks, plus a new per-row dialog in each)
- Test: `internal/apps/reader/handlers_test.go`

**Interfaces:**
- Consumes: `Store.RenameSubscription(ctx, userID, subID, title) error`
  (Task 5).
- Produces: route `POST /sub/{id}/rename`; handler `a.renameSub`.

- [ ] **Step 1: Write the failing test**

Add to `internal/apps/reader/handlers_test.go`:

```go
// TestRenameFeedControlUpdatesTheDisplayName pins the rename dialog end to
// end: it is pre-filled with the current display name, and submitting it
// changes what the tree shows.
func TestRenameFeedControlUpdatesTheDisplayName(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/reader/")
	doc.MustHave("button.reader-sub-rename")
	input := doc.MustHave("dialog#rename-feed-dialog-" + itoa(sub.ID) + " input[name=title]")
	if got, _ := htmlassert.Attr(input, "value"); got != "https://example.com/feed.xml" {
		t.Errorf("rename input value = %q, want the current display name", got)
	}

	rec := s.PostHX(t, s.Alice, "/reader/sub/"+itoa(sub.ID)+"/rename", url.Values{
		"title": {"My Favourite Blog"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("rename returned %d: %s", rec.Code, rec.Body.String())
	}

	doc = s.Get(t, s.Alice, "/reader/")
	if !strings.Contains(doc.Text(), "My Favourite Blog") {
		t.Errorf("renamed feed not in the tree:\n%s", doc.Text())
	}
}

// TestRenameFeedIsScopedToTheOwner guards against renaming (and disclosing
// the existence of) somebody else's subscription id.
func TestRenameFeedIsScopedToTheOwner(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	sub, err := s.Store.Subscribe(ctx, s.Bob.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	rec := s.PostHX(t, s.Alice, "/reader/sub/"+itoa(sub.ID)+"/rename", url.Values{
		"title": {"Hijacked"},
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("renaming another user's subscription returned %d, want 404", rec.Code)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/reader/... -run TestRenameFeed -v`
Expected: FAIL — no `button.reader-sub-rename` in the tree, and
`/reader/sub/{id}/rename` 404s

- [ ] **Step 3: Add the route**

In `internal/apps/reader/app.go`, change:

```go
	r.HandleFunc("POST /sub/{id}/delete", a.unsubscribe)
	r.HandleFunc("POST /sub/{id}/refresh", a.refreshOne)
```

to:

```go
	r.HandleFunc("POST /sub/{id}/delete", a.unsubscribe)
	r.HandleFunc("POST /sub/{id}/refresh", a.refreshOne)
	r.HandleFunc("POST /sub/{id}/rename", a.renameSub)
```

- [ ] **Step 4: Add the handler**

Add to `internal/apps/reader/handlers.go`, directly after `refreshOne` (added
by Task 4):

```go
// renameSub sets or clears this user's custom name for a subscription. An
// empty submitted title is not an error: it clears the override back to the
// feed's own title, which is why this does not special-case ErrInvalid the
// way createFolder does for an empty folder name.
func (a *App) renameSub(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	subID, ok := a.pathID(w, r)
	if !ok {
		return
	}
	lc := formContext(r, 0)
	if err := a.store.RenameSubscription(r.Context(), userID, subID, r.FormValue("title")); err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderIndex(w, r, userID, lc, "")
}
```

- [ ] **Step 5: Add the auth-check row**

In `internal/apps/reader/handlers_test.go`, `TestEveryReaderRouteIsBehindAuth`,
add:

```go
		{http.MethodPost, "/reader/sub/1/refresh"},
		{http.MethodPost, "/reader/sub/1/rename"},
```

(the `/refresh` row was already added in Task 4 — only add `/rename` if it is
not already there).

- [ ] **Step 6: Add the "Rename…" menu item and per-row dialog**

In `internal/apps/reader/templates/panes.partial.html`, both feed rows need a
new menu button and a new per-row `<dialog>`. Each dialog is keyed by the
subscription's own id (`rename-feed-dialog-{{.ID}}`) because
`data-open-dialog` looks a dialog up by a single fixed id
(`internal/apps/reader/static/reader.js:389-392`) and each row needs its own —
this needs no JavaScript changes.

For the folder-nested feed `<li>` (the block Task 4 last modified), add the
"Rename…" button after "Refresh feed" and before the "Unsubscribe" form, and
add the dialog immediately after the `</details>` that closes the row's
`outline-menu`, but still inside the same `<li>...</li>`:

```html
          <li class="reader-sub{{if eq .ID $.Tree.ActiveID}} is-active{{end}}">
            <a href="/reader/feed/{{.ID}}" hx-get="/reader/feed/{{.ID}}"
                 hx-target="#reader-panes" hx-swap="outerHTML" hx-push-url="true">{{.DisplayName}}</a>{{if .Failing}} <span class="reader-failing" title="{{.LastError}}">&#9888;</span>{{end}}
            {{template "count-span" (dict "ID" (printf "reader-count-sub-%d" .ID) "N" (index $.Tree.Counts.BySub .ID))}}
            <details class="outline-menu">
              <summary class="outline-menu-toggle quiet" aria-label="Feed actions">{{ticon "more"}}</summary>
              <div class="outline-menu-list reader-row-menu-list">
                <button type="button" class="reader-copy-feed-url" data-feed-url="{{.FeedURL}}"
                        aria-label="Copy feed URL for {{.DisplayName}}">Feed URL</button>
                <form method="post" action="/reader/sub/{{.ID}}/refresh">
                  {{template "reader-ctx" $.List}}
                  <button type="submit" class="reader-sub-refresh"
                          hx-post="/reader/sub/{{.ID}}/refresh" hx-target="#reader-panes" hx-swap="outerHTML"
                          aria-label="Refresh {{.DisplayName}}">Refresh feed</button>
                </form>
                <button type="button" class="reader-sub-rename" data-open-dialog="rename-feed-dialog-{{.ID}}"
                        aria-label="Rename {{.DisplayName}}">Rename&hellip;</button>
                <form method="post" action="/reader/sub/{{.ID}}/delete">
                  {{template "reader-ctx" $.List}}
                  <button type="submit" class="outline-menu-delete reader-sub-delete"
                          hx-post="/reader/sub/{{.ID}}/delete" hx-target="#reader-panes" hx-swap="outerHTML"
                          hx-confirm="Unsubscribe from &#8220;{{.DisplayName}}&#8221;?"
                          aria-label="Unsubscribe from {{.DisplayName}}">Unsubscribe</button>
                </form>
              </div>
            </details>
            <dialog id="rename-feed-dialog-{{.ID}}" class="reader-dialog">
              <button type="button" class="reader-dialog-close" aria-label="Close">{{ticon "close"}}</button>
              <h2>Rename feed</h2>
              <form method="post" action="/reader/sub/{{.ID}}/rename"
                    hx-post="/reader/sub/{{.ID}}/rename" hx-target="#reader-panes" hx-swap="outerHTML">
                {{template "reader-ctx" $.List}}
                <label for="rename-title-{{.ID}}">Name</label>
                <input id="rename-title-{{.ID}}" name="title" type="text" value="{{.DisplayName}}">
                <div class="dialog-actions">
                  <button type="submit" class="button">Rename</button>
                  <button type="button" class="reader-dialog-cancel">Cancel</button>
                </div>
              </form>
            </dialog>
          </li>
```

And identically for the root-level feed `<li>`:

```html
      <li class="reader-sub{{if eq .ID $.Tree.ActiveID}} is-active{{end}}">
        <a href="/reader/feed/{{.ID}}" hx-get="/reader/feed/{{.ID}}"
             hx-target="#reader-panes" hx-swap="outerHTML" hx-push-url="true">{{.DisplayName}}</a>{{if .Failing}} <span class="reader-failing" title="{{.LastError}}">&#9888;</span>{{end}}
        {{template "count-span" (dict "ID" (printf "reader-count-sub-%d" .ID) "N" (index $.Tree.Counts.BySub .ID))}}
        <details class="outline-menu">
          <summary class="outline-menu-toggle quiet" aria-label="Feed actions">{{ticon "more"}}</summary>
          <div class="outline-menu-list reader-row-menu-list">
            <button type="button" class="reader-copy-feed-url" data-feed-url="{{.FeedURL}}"
                    aria-label="Copy feed URL for {{.DisplayName}}">Feed URL</button>
            <form method="post" action="/reader/sub/{{.ID}}/refresh">
              {{template "reader-ctx" $.List}}
              <button type="submit" class="reader-sub-refresh"
                      hx-post="/reader/sub/{{.ID}}/refresh" hx-target="#reader-panes" hx-swap="outerHTML"
                      aria-label="Refresh {{.DisplayName}}">Refresh feed</button>
            </form>
            <button type="button" class="reader-sub-rename" data-open-dialog="rename-feed-dialog-{{.ID}}"
                    aria-label="Rename {{.DisplayName}}">Rename&hellip;</button>
            <form method="post" action="/reader/sub/{{.ID}}/delete">
              {{template "reader-ctx" $.List}}
              <button type="submit" class="outline-menu-delete reader-sub-delete"
                      hx-post="/reader/sub/{{.ID}}/delete" hx-target="#reader-panes" hx-swap="outerHTML"
                      hx-confirm="Unsubscribe from &#8220;{{.DisplayName}}&#8221;?"
                      aria-label="Unsubscribe from {{.DisplayName}}">Unsubscribe</button>
            </form>
          </div>
        </details>
        <dialog id="rename-feed-dialog-{{.ID}}" class="reader-dialog">
          <button type="button" class="reader-dialog-close" aria-label="Close">{{ticon "close"}}</button>
          <h2>Rename feed</h2>
          <form method="post" action="/reader/sub/{{.ID}}/rename"
                hx-post="/reader/sub/{{.ID}}/rename" hx-target="#reader-panes" hx-swap="outerHTML">
            {{template "reader-ctx" $.List}}
            <label for="rename-title-{{.ID}}">Name</label>
            <input id="rename-title-{{.ID}}" name="title" type="text" value="{{.DisplayName}}">
            <div class="dialog-actions">
              <button type="submit" class="button">Rename</button>
              <button type="button" class="reader-dialog-cancel">Cancel</button>
            </div>
          </form>
        </dialog>
      </li>
```

Note the `title` input deliberately has no `required` attribute: submitting it
empty is how a user clears the override back to the feed's own title, which
`RenameSubscription` (Task 5) treats as a valid, meaningful write.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/apps/reader/... -run "TestRenameFeed|TestEveryReaderRouteIsBehindAuth" -v`
Expected: PASS

- [ ] **Step 8: Run the whole reader package's tests**

Run: `go test ./internal/apps/reader/...`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/apps/reader/handlers.go internal/apps/reader/handlers_test.go \
        internal/apps/reader/app.go internal/apps/reader/templates/panes.partial.html
git commit -m "feat(reader): add a rename-feed dialog and action"
```

---

### Task 7: Full verification and PR

**Files:** none (verification only)

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: no output

- [ ] **Step 3: Manually verify in the browser** (via the `run` skill)

- Add a new feed and confirm articles appear in the response without a
  separate refresh.
- Open a feed's "..." menu, click "Refresh feed", confirm it re-fetches (e.g.
  check for a network request to the origin, or add a new item to the fixture
  feed between two refreshes and see it appear).
- Click "Rename…", confirm the dialog is pre-filled with the current name,
  submit a new name, confirm the tree label updates.
- Reopen "Rename…", clear the field, submit, confirm the label reverts to the
  feed's own title.

- [ ] **Step 4: Push the branch and open the PR**

```bash
git push -u origin reader-feed-fetch-rename
```

```bash
gh pr create --title "feat(reader): fetch on add, single-feed refresh, and rename" --body "$(cat <<'EOF'
## Summary
- Fetch a feed immediately when it is added, instead of waiting for the next poller tick
- Add a per-feed "Refresh feed" action to the feed menu
- Add a "Rename…" action that sets or clears a per-user display-name override

## Test plan
- [x] `go test ./...`
- [x] `go vet ./...`
- [ ] Manual: add a feed, confirm immediate articles
- [ ] Manual: refresh one feed from its menu
- [ ] Manual: rename a feed, then clear the rename
EOF
)"
```

Report the PR URL back once created.
