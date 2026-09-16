# ON Reader fetch-on-add, single-feed refresh, and rename design

Status: approved 2026-09-15. Three small, independent additions to ON Reader:
fetch a feed immediately when it's added, refresh one feed on demand, and
rename a feed. No schema migration — the rename column already exists and is
unused.

## 1. Decisions

| # | Item | Choice |
|---|---|---|
| 1 | Fetch on add | Synchronous, bounded ~10s timeout, inside `a.subscribe` |
| 2 | Refresh one feed | New `Poller.FetchNow`, new `POST /sub/{id}/refresh`, menu item |
| 3 | Rename feed | New `Store.RenameSubscription`, new `POST /sub/{id}/rename`, per-row dialog |

## 2. Fetch a feed immediately after adding it

Today `Store.Subscribe` (`internal/apps/reader/store.go:182`) sets
`next_fetch_at = now` on a new feed but nothing fetches it — it sits with zero
articles until the poller's next 5-minute tick (`poll.go:19`) picks it up.

- `Poller` gains an exported `FetchNow(ctx context.Context, feedID int64) error`
  (`poll.go`): loads that one `Feed` row (a single-row variant of the query
  `DueFeeds` uses, without the `next_fetch_at` filter) and calls the existing
  unexported `pollOne` (`poll.go:119`) directly — same fetch/parse/save/record
  logic the scheduled poller and "refresh all" already use, just for one feed
  and without the due-check.
- `a.subscribe` (`handlers.go:573`), right after `store.Subscribe` succeeds,
  calls `a.poller.FetchNow(ctx, sub.FeedID)` with a `context.WithTimeout` of
  10s derived from the request context.
- Error handling: the subscription is created either way — a `FetchNow`
  failure or timeout is logged and otherwise ignored by the handler. The feed
  just shows zero articles until the poller's normal backoff/retry picks it up
  shortly after (same as today's behavior, just usually preempted by the
  synchronous fetch). No error is surfaced to the user for this case. Amended
  2026-09-16 (issue #254): "no error is surfaced" means the response —
  `a.subscribe` returns the same success page it always does, with no banner
  text. It does not mean the tree stays silent: `FetchNow` calls the same
  `pollOne` the scheduled poller does, which records the failure the normal
  way, so the tree's ⚠ "Failing" marker can appear on the new feed right away.
  That is the existing, general mechanism working as designed — a feed with
  `error_count > 0` shows the marker regardless of which code path caused the
  failure — not a gap in this feature's own error handling, and not
  suppressed for a feed's first fetch: an honest immediate signal that
  something is wrong beats a fresh feed that silently shows nothing and
  nothing to explain why.
- `a.subscribe` continues to render the index exactly as it does today
  (`handlers.go:613`), now showing the freshly fetched articles instead of an
  empty feed.

## 3. Refresh a single feed on demand

- New route `POST /sub/{id}/refresh` (`app.go`, alongside the existing
  `POST /sub/{id}/delete`).
- New handler `a.refreshOne` (`handlers.go`), same shape as `a.refresh`
  (`handlers.go:790`, which calls `a.poller.PollDue` for "refresh all"):
  1. `userID, ok := a.userID(w, r)`
  2. `id, ok := a.pathID(w, r)` — the subscription ID
  3. Look up the subscription's `FeedID` (owned-by-user check, same as
     `unsubscribe`/`renameSub` below)
  4. `a.poller.FetchNow(ctx, feedID)` — errors go through `a.fail(w, r, err)`
  5. `a.renderIndex(w, r, userID, lc, "")` — same full re-render `unsubscribe`
     and `deleteFolder` already use
- New "Refresh feed" `<button>` in both feed-menu blocks in
  `templates/panes.partial.html` (folder-nested feeds ~line 210-223,
  root-level feeds ~line 235-248), next to "Feed URL"/"Unsubscribe", posting
  to `/reader/sub/{{.ID}}/refresh` with matching `hx-post`/
  `hx-target="#reader-panes" hx-swap="outerHTML"`, following the same
  form+CSRF pattern as the existing "Unsubscribe" button in the same menu.

## 4. Rename a feed

`Subscription.Title` (`store.go:89`) is a per-user override column that
already exists and is already read by `DisplayName()` (`store.go:108`) —
override → feed's own title → raw URL. It has never been written to. This
feature only adds the write path and UI.

- New `Store.RenameSubscription(ctx context.Context, userID, subID int64, title string) error`
  (`store.go`, modeled on `CreateFolder`'s validation, `store.go:276`):
  trims the input; writes the trimmed value to `reader_subs.title` for the
  given `userID`+`subID` (ownership-scoped update, same predicate style as
  `unsubscribe`'s delete). An empty string after trimming is a valid write —
  it clears the override, and `DisplayName()` falls back to the feed's own
  title again.
- New route `POST /sub/{id}/rename`, new handler `a.renameSub`
  (`handlers.go`), same shape as `unsubscribe` (`handlers.go:730`): auth →
  path ID → read the `title` form field → `store.RenameSubscription` →
  `a.fail` on error → `a.renderIndex(...)` on success.
- New "Rename…" `<button>` in both feed-menu blocks, opening a per-row
  `<dialog id="rename-feed-dialog-{{.ID}}">` via `data-open-dialog` (the
  existing mechanism, `reader.js:389-392` — looks up a dialog by ID and calls
  `showModal()`). One dialog per row (not a single shared dialog) because
  `data-open-dialog` addresses dialogs by a fixed ID and rows are already
  rendered in a loop — this needs no new JS.
  - Dialog body: modeled on the existing "New folder" dialog
    (`panes.partial.html:301-314`) — single labeled text input, Cancel/Submit,
    `method=post action="/reader/sub/{{.ID}}/rename"` +
    `hx-post hx-target="#reader-panes" hx-swap="outerHTML"`, same
    `{{template "reader-ctx" $.List}}` hidden context/CSRF fields as every
    other form in this template.
  - Input pre-filled `value="{{.DisplayName}}"` (the resolved display name,
    not the raw override) so the field always shows what's currently on
    screen, whether that's a custom name or the feed's own title.

## 5. What stays the same

- No schema migration — `reader_subs.title` already exists.
- No changes to the scheduled poller's cadence, backoff, or the existing
  "refresh all" (`POST /refresh`) endpoint; `FetchNow` is an addition
  alongside `PollDue`, not a replacement.
- `Feed.Title` (the shared, feed-reported title) is never written by rename —
  only the per-user `Subscription.Title` override changes.
- Tree/list OOB conventions, mobile layout, and every other menu item are
  untouched.

## 6. Testing

- `Poller.FetchNow`: unit test fetching a known feed by ID succeeds
  regardless of `next_fetch_at`, and records the same fields `pollOne`/
  `PollDue` do (title, site URL, items, next_fetch_at).
- `a.subscribe`: handler test confirming a newly subscribed feed has articles
  immediately after the response (synchronous fetch happened before
  `renderIndex`), and that a failing/slow fetch (test double returning an
  error or exceeding the timeout) still results in a created subscription with
  no user-visible error.
- `a.refreshOne`: handler test — refreshing a feed not yet due still fetches
  and updates its articles; ownership check rejects another user's
  subscription ID.
- `Store.RenameSubscription` / `a.renameSub`: unit + handler tests — sets a
  custom name, `DisplayName()` reflects it; empty string clears the override
  back to the feed's own title; ownership check rejects another user's
  subscription ID.
- Manual verification via the `run` skill / browser: add a feed and see
  articles appear without waiting; use "Refresh feed" on an already-fresh feed
  and confirm it re-fetches; rename a feed via the dialog and confirm the tree
  label updates and survives a reload; clear a rename back to empty and
  confirm it reverts to the feed's own title.
