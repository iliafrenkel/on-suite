# ON Reader feed favicons design

Status: approved 2026-09-15. Small, self-contained addition to ON Reader:
show each feed's site favicon next to its title in the tree, falling back to
a generic RSS glyph when none is available.

## 1. Decisions

| # | Item | Choice |
|---|---|---|
| 1 | Where the favicon URL comes from | Parse `<link rel="icon"\|"shortcut icon">` on the site's homepage, falling back to `<site-origin>/favicon.ico` |
| 2 | When it's discovered | On add (piggybacking on the existing add-feed page fetch), plus a periodic re-check during polling (~30 days) |
| 3 | Storage of the bytes | New small cache table + proxy endpoint, modeled on `reader_images`/`imgproxy.go` but kept separate |
| 4 | Fallback | Generic "rss" glyph added to the existing `toolbarIcons` map, shown when there's no favicon URL or the fetch fails |

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

`Feed` (`store.go:54-67`) gains a `FaviconURL string` field, read/written
alongside the existing `SiteURL`/`Title` fields wherever those are loaded and
saved (`Store.SaveFetchResult` and friends).

## 3. Discovery

New `internal/apps/reader/favicon.go`:

```go
// DiscoverFavicon returns the best favicon URL for a site: the first
// <link rel="icon"> or <link rel="shortcut icon"> found in pageHTML,
// resolved against siteURL, or "<origin>/favicon.ico" if pageHTML is nil
// or has no such link.
func DiscoverFavicon(pageHTML []byte, siteURL string) string
```

Implementation walks the parsed tree the same way `FeedsInPage`
(`discover.go:46`) does, looking at `<link>` elements' `rel`/`href`
attributes, and resolves relative hrefs against `siteURL`. No page HTML (or
no match) falls back to `<origin>/favicon.ico`; a `siteURL` that doesn't
parse yields `""`.

Two call sites:

- **Add-feed flow** (`handlers.go:678`), which already fetches the source
  page's bytes to run `FeedsInPage` — the same bytes are passed to
  `DiscoverFavicon`, and the result is saved with the new feed.
- **Poll cycle** (`poll.go`): each feed carries a `next_favicon_check_at`-style
  gate reusing the existing scheduling helpers, defaulting to ~30 days after
  the last check. When due, poll fetches the site homepage once, re-runs
  `DiscoverFavicon`, and updates `favicon_url` if it changed. A failed
  homepage fetch just skips the check for this cycle — it does not clear the
  existing `favicon_url` or count against feed error tracking.

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

- If `.Sub.FaviconURL != ""`: render
  `<img class="reader-favicon" src="/reader/favicon/{{faviconHash .Sub.FaviconURL}}" width="16" height="16" alt="" onerror="this.replaceWith(...)">`
  where the `onerror` swaps in the generic icon markup client-side — this
  covers the case where discovery found a URL but the proxy's fetch then
  failed (404), without a server round trip to know that in advance.
- Otherwise render the generic icon directly.

The generic icon is a new `"rss"` entry in the `toolbarIcons` map
(`internal/ui/toolbar_icons.go`), used via the existing `{{ticon "rss"}}`
template helper — same mechanism already used for other inline icons in this
template.

`faviconHash` is a small template func (registered next to `ticon`) wrapping
the hash computation so the template doesn't need Go-side pre-computation
threaded through every row.

## 6. Testing

- Unit tests for `DiscoverFavicon`: `<link rel="icon">` present, `rel="shortcut icon"` present, relative href resolution, no page HTML (falls back to `/favicon.ico`), unparsable `siteURL`.
- Store tests for `reader_feed_icons` save/load/failure-tracking, mirroring existing `Image` store tests.
- Handler test for `/reader/favicon/{hash}`: unknown hash 404s, a successful fetch caches and serves, a failed fetch 404s and is retried only after backoff.
- No new interaction test for the `onerror` swap — it's a one-line client-side fallback, not app logic.
