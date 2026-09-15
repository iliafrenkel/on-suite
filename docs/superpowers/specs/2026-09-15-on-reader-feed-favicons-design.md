# ON Reader feed favicons design

Status: approved 2026-09-15. Small, self-contained addition to ON Reader:
show each feed's site favicon next to its title in the tree, falling back to
a generic RSS glyph when none is available.

## 1. Decisions

| # | Item | Choice |
|---|---|---|
| 1 | Where the favicon URL comes from | Parse `<link rel="icon"\|"shortcut icon">` from page HTML **already fetched** during webpage-based feed discovery, when available; otherwise derive `<site-origin>/favicon.ico` — no extra network fetch either way |
| 2 | When it's discovered | Once, when the feed is added (or first successfully polled, for the direct-feed-URL case) — no periodic re-check |
| 3 | Storage of the bytes | New small cache table + proxy endpoint, modeled on `reader_images`/`imgproxy.go` but kept separate |
| 4 | Fallback | Generic "rss" glyph added to the existing `toolbarIcons` map, shown when there's no favicon URL or the fetch fails |

**Revision note (2026-09-15, during planning):** the original decisions #1/#2 called for
fetching the site's homepage on every add and again every ~30 days during
polling. Tracing the actual add-feed code path (`handlers.go`) showed that
pasting a feed URL directly — the common case, and what nearly every existing
test fixture does — never fetches the site's homepage at all, only the feed
XML. Adding a new homepage fetch to that path (and to the regular poll cycle)
would inject a real outbound network call into a large fraction of the
existing test suite (many tests reuse a fixture whose site URL is a live
external domain), hurting both test speed/hermeticity and, in production,
add-feed/poll latency. The revised approach gets the same favicon in the
common case for free, from data already in hand, and accepts a slightly less
sharp fallback (a guess rather than a confirmed `<link>`) in exchange for
zero new network calls in the hot paths.

## 2. Data model

New migration `internal/apps/reader/migrations/0008_favicons.sql`:

```sql
ALTER TABLE reader_feeds ADD COLUMN favicon_url TEXT NOT NULL DEFAULT '';

CREATE TABLE reader_feed_icons (
    url_hash     TEXT PRIMARY KEY,
    src_url      TEXT NOT NULL,
    content_type TEXT,
    bytes        BLOB,
    fetched_at   TIMESTAMP,
    error_count  INTEGER NOT NULL DEFAULT 0
);
```

`reader_feed_icons` is a deliberate near-duplicate of `reader_images`
(`migrations/0003_images.sql`), not a reuse of it. `reader_images` has a
retention job (`store.go:1249-1251`) that deletes any row with no matching
`reader_item_images` link — a favicon would never have one and would be swept
away almost immediately. Keeping favicons in their own table avoids coupling
their lifetime to that unrelated cleanup.

`Feed` (`store.go:54-67`) gains a `FaviconURL string` field, loaded by
`DueFeeds`/`FeedByID` alongside the existing `SiteURL`/`Title` columns.
`Subscription` (`store.go:85-101`) gains the same field, loaded by the `Tree`
query so the sidebar template can read it directly off `.Sub`.

Once set, `favicon_url` is never overwritten — there is no recompute-on-change
behavior, so a single new store method is enough:

```go
// SetFaviconIfEmpty records a feed's favicon URL, but only the first time:
// the WHERE clause is a no-op once favicon_url is already set, so a later
// call (a second poll, a second discovery) can never clobber it.
func (s *Store) SetFaviconIfEmpty(ctx context.Context, feedID int64, faviconURL string) error
```
```sql
UPDATE reader_feeds SET favicon_url = ? WHERE id = ? AND favicon_url = ''
```

## 3. Discovery

New `internal/apps/reader/favicon.go`:

```go
// DiscoverFavicon returns the best favicon URL for a site given page HTML
// that may already be in hand: the first <link rel="icon"> or
// <link rel="shortcut icon"> found in pageHTML, resolved against siteURL, or
// "<origin>/favicon.ico" if pageHTML is nil/has no such link. It makes no
// network calls of its own — the caller decides whether fetching pageHTML
// was worth doing.
func DiscoverFavicon(pageHTML []byte, siteURL string) string
```

Implementation walks the parsed tree the same way `FeedsInPage`
(`discover.go:46`) does, looking at `<link>` elements' `rel`/`href`
attributes (matching a `rel` whose whitespace-separated tokens include
`icon`, which covers both `icon` and `shortcut icon` without also matching
`apple-touch-icon`), and resolves relative hrefs against `siteURL` via the
existing `resolveAbsoluteHTTPURL` helper. No page HTML (or no match) falls
back to `<origin>/favicon.ico`; a `siteURL` that doesn't parse yields `""`.

Two call sites, neither of which fetches anything new:

- **Add-feed flow**, `resolveFeedURL` (`handlers.go:659`): its "single
  candidate" and "probe found a feed" return paths already hold a fetched
  *webpage* (`res.Body`/`res.FinalURL` — the page turned out not to be a feed
  itself, which is why `FeedsInPage` ran on it). `resolveFeedURL` gains a
  second return value, `faviconURL string`, computed via `DiscoverFavicon` on
  those same bytes in those two branches (empty in the "already a feed" and
  "multiple candidates" branches, where no webpage was fetched or discovery
  hasn't concluded). The `subscribe` handler calls
  `a.store.SetFaviconIfEmpty(ctx, sub.FeedID, faviconURL)` right after
  `Subscribe` returns, when `faviconURL != ""`.
- **Poll pipeline**, `pollOne` (`poll.go:131`): after a successful or
  not-modified poll, if the feed's (pre-poll) `FaviconURL` is still empty and
  a site URL is known (the freshly parsed one, or the feed's existing one),
  compute `DiscoverFavicon(nil, siteURL)` — a pure string derivation, no
  fetch — and save it via `SetFaviconIfEmpty`. This is what fills in the
  common direct-feed-URL case, once `SiteURL` becomes known from the
  synchronous fetch-on-add poll (or, failing that, the next scheduled one).

## 4. Caching and serving

New handler mounted at `/reader/favicon/{hash}`, closely mirroring
`image()`/`fetchImage()`/`writeImage()` in `imgproxy.go`:

- `hash` = `sha256(favicon_url)[:16 bytes]`, hex-encoded (same scheme as
  `ImageHash`, but over the `reader_feed_icons` table — hashes from the two
  tables are never compared against each other).
- Lazy fetch on first request through the shared `Client`, so the SSRF guard,
  redirect cap, and a new `MaxFaviconBytes` size cap (64 KiB — favicons are
  tiny; this just bounds a misbehaving server) all apply.
- Content-type sniffed via `http.DetectContentType` and required to start
  with `image/` (matches `fetchImage`'s existing check; Go's sniffer
  recognizes classic `.ico` as `image/x-icon`).
- Same permanent-failure-after-3-attempts and 1-hour backoff as
  `imgproxy.go`'s constants (reused by name, applied to the new table).
- Cache-Control mirrors `imageCacheControl` (`private, max-age=86400`).

## 5. Rendering

In `panes.partial.html`'s shared `sub-row` block, next to the feed title
(currently a bare `<a href="/reader/feed/{{.Sub.ID}}">{{.Sub.DisplayName}}</a>`
at line ~228):

- If `.Sub.FaviconPath != ""`: render
  `<img class="reader-favicon" src="{{.Sub.FaviconPath}}" width="16" height="16" alt="" onerror="this.replaceWith(...)">`
  where the `onerror` swaps in the generic icon markup client-side — this
  covers the case where discovery found a URL but the proxy's fetch then
  failed (404), without a server round trip to know that in advance.
- Otherwise render the generic icon directly.

`FaviconPath` is a derived method on `Subscription`, alongside the existing
`DisplayName()`/`Failing()` (`store.go:106-119`):

```go
// FaviconPath is the proxy URL for this feed's favicon, or "" if none is
// known yet — the template's cue to render the generic icon instead.
func (s Subscription) FaviconPath() string
```

It computes the content-addressed hash itself (same scheme as `ImageHash`)
rather than exposing a template function for it: `internal/platform/render`
(where template funcs are registered) must not import `internal/apps/reader`
— see the package doc at `store.go:1-5` — and no other template in this app
needs a hash computed inline, so a plain Go method matches the existing
pattern better than a new cross-cutting template func.

The generic icon is a new `"rss"` entry in the `toolbarIcons` map
(`internal/ui/toolbar_icons.go`), used via the existing `{{ticon "rss"}}`
template helper — same mechanism already used for other inline icons in this
template. `.toolbar-icon` is already sized to `1rem` (16px) in `app.css`,
matching the favicon `<img>`'s 16×16, so no new CSS sizing rule is needed —
only a small `.reader-favicon` rule for `flex: none` and vertical alignment
(the row is `align-items: baseline`, which suits text but not a small image).

## 6. Testing

- Unit tests for `DiscoverFavicon`: `<link rel="icon">` present, `rel="shortcut icon"` present, relative href resolution, no page HTML (falls back to `/favicon.ico`), unparsable `siteURL`.
- `Store.SetFaviconIfEmpty`: sets on an empty feed, refuses to overwrite an already-set one.
- `pollOne` test: a direct-feed-URL subscribe (no page fetch) ends up with a `/favicon.ico`-shaped `favicon_url` after fetch-on-add, with no extra requests hitting the test origin beyond the feed fetch itself (guards the "no new network calls" property this whole revision exists for).
- Handler test: subscribing via webpage discovery (a page with `<link rel="icon">` in the fixture HTML) ends up with that exact favicon URL, not the generic guess.
- Handler test for `/reader/favicon/{hash}`: unknown hash 404s, a successful fetch caches and serves, a failed fetch 404s and is retried only after backoff.
- No new interaction test for the `onerror` swap — it's a one-line client-side fallback, not app logic.
