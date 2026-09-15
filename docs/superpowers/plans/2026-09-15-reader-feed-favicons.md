# ON Reader Feed Favicons Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show each feed's site favicon next to its title in the ON Reader sidebar tree, falling back to a generic RSS glyph when none is known.

**Architecture:** A new `favicon_url` column on `reader_feeds`, filled in once per feed for free — either parsed from webpage HTML the add-feed flow already fetched, or derived as a zero-network `<origin>/favicon.ico` guess once the feed's site URL is known. A small standalone cache table (`reader_feed_icons`) plus a lazy-fetch proxy handler at `/reader/favicon/{hash}`, closely mirroring the existing article-image proxy (`imgproxy.go`) but kept separate so an unrelated retention job can't sweep favicons away. The template renders the proxy URL or the generic icon, with a client-side `onerror` swap covering a favicon URL that turned out to be wrong.

**Tech Stack:** Go (`net/http`, `database/sql`, `golang.org/x/net/html`), SQLite (via the existing `internal/platform/db` migration runner), Go `html/template`.

## Global Constraints

- No new outbound network call may be added to the add-feed or poll hot paths — see the design's revision note. Every favicon lookup is either free (bytes already fetched) or a pure string derivation; only the on-demand `/reader/favicon/{hash}` proxy (triggered by a browser requesting the image, never by app-side code) actually fetches a favicon's bytes.
- Every outbound fetch goes through the shared `Client` (`fetch.go`), so the SSRF guard, redirect cap and size caps apply.
- Follow existing package conventions: `package reader` for non-test files, `package reader_test` (black-box) for `_test.go` files, `t.Helper()` in test helpers, table-free straightforward tests matching the existing style in this package.
- Full reference: [docs/superpowers/specs/2026-09-15-on-reader-feed-favicons-design.md](../specs/2026-09-15-on-reader-feed-favicons-design.md).

---

### Task 1: Migration — favicon storage

**Files:**
- Create: `internal/apps/reader/migrations/0008_favicons.sql`
- Test: `internal/apps/reader/store_test.go` (add one test to the existing file)

**Interfaces:**
- Produces: columns `reader_feeds.favicon_url TEXT NOT NULL DEFAULT ''`; table `reader_feed_icons(url_hash TEXT PRIMARY KEY, src_url TEXT NOT NULL, content_type TEXT NOT NULL DEFAULT '', bytes BLOB, fetched_at TEXT, last_error TEXT NOT NULL DEFAULT '', error_count INTEGER NOT NULL DEFAULT 0) STRICT`.

- [ ] **Step 1: Write the failing test**

Add to `internal/apps/reader/store_test.go`:

```go
func TestFavoritesMigrationAddsFaviconColumnAndTable(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	if _, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil); err != nil {
		t.Fatal(err)
	}

	var faviconURL string
	if err := f.db.QueryRowContext(ctx,
		`SELECT favicon_url FROM reader_feeds WHERE url = ?`, "https://example.com/feed.xml").
		Scan(&faviconURL); err != nil {
		t.Fatalf("favicon_url column missing or unreadable: %v", err)
	}
	if faviconURL != "" {
		t.Errorf("favicon_url = %q, want empty default", faviconURL)
	}

	if _, err := f.db.ExecContext(ctx,
		`INSERT INTO reader_feed_icons (url_hash, src_url) VALUES ('abc', 'https://example.com/favicon.ico')`); err != nil {
		t.Fatalf("reader_feed_icons missing or wrong shape: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/apps/reader/... -run TestFavoritesMigrationAddsFaviconColumnAndTable -v`
Expected: FAIL — `no such column: favicon_url` (or `no such table: reader_feed_icons`).

- [ ] **Step 3: Write the migration**

```sql
-- Feed favicons: shown in the sidebar tree next to each feed's title.
--
-- favicon_url lives on reader_feeds (shared across subscribers, like title
-- and site_url) rather than reader_subs, since it is a property of the feed
-- itself, not of one person's subscription to it.
ALTER TABLE reader_feeds ADD COLUMN favicon_url TEXT NOT NULL DEFAULT '';

-- The favicon proxy's cache, deliberately separate from reader_images even
-- though the shape is nearly identical: reader_images has a retention job
-- (see recordItemImages / the DELETE in store.go) that frees any row with no
-- reader_item_images link, and a favicon would never have one — it would be
-- swept away almost immediately. Kept as its own table so that cleanup never
-- touches favicons, and a favicon never needs one of its own: the row count
-- here is naturally bounded by "one per distinct favicon URL ever seen",
-- which is tiny for a household reader.
CREATE TABLE reader_feed_icons (
    url_hash     TEXT PRIMARY KEY,
    src_url      TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT '',
    bytes        BLOB,
    fetched_at   TEXT,
    last_error   TEXT NOT NULL DEFAULT '',
    error_count  INTEGER NOT NULL DEFAULT 0
) STRICT;
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/apps/reader/... -run TestFavoritesMigrationAddsFaviconColumnAndTable -v`
Expected: PASS

- [ ] **Step 5: Run the full reader package test suite to confirm nothing else broke**

Run: `go test ./internal/apps/reader/...`
Expected: PASS (existing tests unaffected — the column has a default, so every existing INSERT still works)

- [ ] **Step 6: Commit**

```bash
git add internal/apps/reader/migrations/0008_favicons.sql internal/apps/reader/store_test.go
git commit -m "feat(reader): add favicon_url column and reader_feed_icons table"
```

---

### Task 2: Favicon discovery — `DiscoverFavicon`

**Files:**
- Create: `internal/apps/reader/favicon.go`
- Test: `internal/apps/reader/favicon_test.go`

**Interfaces:**
- Consumes: `resolveAbsoluteHTTPURL(raw string, base *url.URL) (string, bool)` from `discover.go:122` (unexported, same package — `favicon.go` lives in `package reader`).
- Produces: `func DiscoverFavicon(pageHTML []byte, siteURL string) string` — used by Task 6 (poll pipeline) and Task 7 (handler wiring).

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/reader/favicon_test.go`:

```go
package reader_test

import (
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

func TestDiscoverFaviconFindsAnIconLink(t *testing.T) {
	page := []byte(`<html><head>
	<link rel="stylesheet" href="/s.css">
	<link rel="icon" href="/static/icon.png">
	</head></html>`)

	got := reader.DiscoverFavicon(page, "https://blog.example/")
	if got != "https://blog.example/static/icon.png" {
		t.Errorf("got %q, want the discovered icon URL", got)
	}
}

func TestDiscoverFaviconFindsAShortcutIconLink(t *testing.T) {
	page := []byte(`<html><head>
	<link rel="shortcut icon" href="/favicon.png">
	</head></html>`)

	got := reader.DiscoverFavicon(page, "https://blog.example/")
	if got != "https://blog.example/favicon.png" {
		t.Errorf("got %q, want the shortcut icon URL", got)
	}
}

// apple-touch-icon is a single rel token, not "icon" plus something else, so
// it must not match — this app wants a small favicon-shaped image, not the
// large icon iOS home-screen bookmarks use.
func TestDiscoverFaviconIgnoresAppleTouchIcon(t *testing.T) {
	page := []byte(`<html><head>
	<link rel="apple-touch-icon" href="/apple-touch-icon.png">
	</head></html>`)

	got := reader.DiscoverFavicon(page, "https://blog.example/")
	if got != "https://blog.example/favicon.ico" {
		t.Errorf("got %q, want the /favicon.ico fallback, not the apple-touch-icon", got)
	}
}

func TestDiscoverFaviconResolvesRelativeHrefs(t *testing.T) {
	got := reader.DiscoverFavicon(
		[]byte(`<link rel="icon" href="icon.ico">`),
		"https://blog.example/posts/index.html")
	if got != "https://blog.example/posts/icon.ico" {
		t.Errorf("got %q, want the href resolved against the page URL", got)
	}
}

func TestDiscoverFaviconFallsBackWithNoPageHTML(t *testing.T) {
	got := reader.DiscoverFavicon(nil, "https://blog.example/some/deep/page")
	if got != "https://blog.example/favicon.ico" {
		t.Errorf("got %q, want the origin's /favicon.ico guess", got)
	}
}

func TestDiscoverFaviconFallsBackWhenNoLinkMatches(t *testing.T) {
	got := reader.DiscoverFavicon([]byte(`<html><head><title>No icon here</title></head></html>`), "https://blog.example/")
	if got != "https://blog.example/favicon.ico" {
		t.Errorf("got %q, want the /favicon.ico fallback", got)
	}
}

func TestDiscoverFaviconReturnsEmptyForAnUnparsableSiteURL(t *testing.T) {
	got := reader.DiscoverFavicon(nil, "://not a url")
	if got != "" {
		t.Errorf("got %q, want empty for an unparsable site URL", got)
	}
}

func TestDiscoverFaviconRefusesAHostileHref(t *testing.T) {
	got := reader.DiscoverFavicon(
		[]byte(`<link rel="icon" href="javascript:alert(1)">`),
		"https://blog.example/")
	if got != "https://blog.example/favicon.ico" {
		t.Errorf("got %q, want the fallback rather than a javascript: URL", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/apps/reader/... -run TestDiscoverFavicon -v`
Expected: FAIL with `undefined: reader.DiscoverFavicon`

- [ ] **Step 3: Write the implementation**

Create `internal/apps/reader/favicon.go`:

```go
package reader

import (
	"bytes"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// DiscoverFavicon returns the best favicon URL for a site, given page HTML
// that may already be in hand.
//
// It never fetches anything itself — the caller decides whether fetching
// pageHTML was worth doing, which is the whole point: this function exists
// so that a poll cycle or an add-feed request that already has a page's
// bytes in memory (or has none at all) can still get a favicon URL for free.
//
// It returns the first <link rel="icon"> or <link rel="shortcut icon"> found
// in pageHTML, resolved against siteURL. If pageHTML is empty, unparsable,
// or has no such link, it falls back to "<origin>/favicon.ico" — the
// decades-old convention every browser itself falls back to. It returns ""
// only if siteURL itself does not parse into an absolute http(s) URL.
func DiscoverFavicon(pageHTML []byte, siteURL string) string {
	base, err := url.Parse(siteURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return ""
	}

	if len(pageHTML) > 0 {
		if href, ok := faviconLinkInPage(pageHTML, base); ok {
			return href
		}
	}

	fallback := *base
	fallback.Path = "/favicon.ico"
	fallback.RawQuery = ""
	fallback.Fragment = ""
	return fallback.String()
}

// faviconLinkInPage walks the parsed page the same way FeedsInPage
// (discover.go) does, looking for the first <link> whose rel identifies it
// as a favicon.
func faviconLinkInPage(page []byte, base *url.URL) (string, bool) {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return "", false
	}

	var found string
	var ok bool
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if ok {
			return
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.Link {
			var rel, href string
			for _, a := range n.Attr {
				switch strings.ToLower(a.Key) {
				case "rel":
					rel = strings.ToLower(a.Val)
				case "href":
					href = strings.TrimSpace(a.Val)
				}
			}
			if isIconRel(rel) && href != "" {
				if abs, resolved := resolveAbsoluteHTTPURL(href, base); resolved {
					found, ok = abs, true
					return
				}
			}
		}
		for child := n.FirstChild; child != nil && !ok; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return found, ok
}

// isIconRel matches "icon" and "shortcut icon" (two whitespace-separated
// tokens, "shortcut" and "icon") without also matching "apple-touch-icon",
// which is a single token and never equals "icon".
func isIconRel(rel string) bool {
	for _, tok := range strings.Fields(rel) {
		if tok == "icon" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/apps/reader/... -run TestDiscoverFavicon -v`
Expected: PASS (all 8 subtests)

- [ ] **Step 5: Commit**

```bash
git add internal/apps/reader/favicon.go internal/apps/reader/favicon_test.go
git commit -m "feat(reader): add DiscoverFavicon for zero-network favicon URL discovery"
```

---

### Task 3: Favicon cache store methods

**Files:**
- Modify: `internal/apps/reader/store.go` (add near the existing `Image` type/methods, e.g. after line 1159)
- Test: `internal/apps/reader/store_test.go`

**Interfaces:**
- Produces:
  - `func FaviconHash(srcURL string) string`
  - `type FeedIcon struct { Hash, SrcURL, ContentType string; Bytes []byte; FetchedAt time.Time; ErrorCount int; LastError string }`
  - `func (i FeedIcon) Cached() bool`
  - `func (s *Store) FeedIconByHash(ctx context.Context, hash string) (FeedIcon, error)` — `ErrNotFound` for an unknown hash
  - `func (s *Store) SaveFeedIconBytes(ctx context.Context, hash, contentType string, data []byte, now time.Time) error`
  - `func (s *Store) SaveFeedIconFailure(ctx context.Context, hash, msg string, now time.Time) error`
- Consumed by: Task 4 (the proxy handler) and Task 5 (`SetFaviconIfEmpty`, which inserts the row `FeedIconByHash` later reads).

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/reader/store_test.go`:

```go
func TestFeedIconHashIsStableAndURLSafe(t *testing.T) {
	a := reader.FaviconHash("https://example.com/favicon.ico")
	b := reader.FaviconHash("https://example.com/favicon.ico")
	c := reader.FaviconHash("https://example.com/other.ico")

	if a != b {
		t.Error("hash is not stable across calls")
	}
	if a == c {
		t.Error("different URLs hashed the same")
	}
	if len(a) != 32 {
		t.Errorf("hash is %d chars, want 32", len(a))
	}
}

func TestFeedIconByHashRefusesAnUnknownHash(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	_, err := f.store.FeedIconByHash(ctx, reader.FaviconHash("https://never-seen.example/x.ico"))
	if !errors.Is(err, reader.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestSaveFeedIconBytesCachesAndClearsFailures(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	hash := reader.FaviconHash("https://example.com/favicon.ico")
	if _, err := f.db.ExecContext(ctx,
		`INSERT INTO reader_feed_icons (url_hash, src_url) VALUES (?, ?)`,
		hash, "https://example.com/favicon.ico"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveFeedIconFailure(ctx, hash, "boom", now); err != nil {
		t.Fatal(err)
	}

	if err := f.store.SaveFeedIconBytes(ctx, hash, "image/x-icon", []byte{0x00, 0x01}, now); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.FeedIconByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Cached() {
		t.Error("Cached() = false after SaveFeedIconBytes")
	}
	if got.ContentType != "image/x-icon" {
		t.Errorf("ContentType = %q, want image/x-icon", got.ContentType)
	}
	if got.ErrorCount != 0 || got.LastError != "" {
		t.Errorf("failure not cleared: ErrorCount=%d LastError=%q", got.ErrorCount, got.LastError)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/apps/reader/... -run TestFeedIcon -v`
Expected: FAIL with `undefined: reader.FaviconHash` (and friends)

- [ ] **Step 3: Write the implementation**

Add to `internal/apps/reader/store.go`, after `SaveImageFailure` (line 1159):

```go
// FaviconHash identifies a favicon by its source URL, the same scheme
// ImageHash uses for article images — but computed and stored separately,
// over reader_feed_icons rather than reader_images. A hash from one table
// is never looked up in the other.
func FaviconHash(srcURL string) string {
	sum := sha256.Sum256([]byte(srcURL))
	return hex.EncodeToString(sum[:16])
}

// FeedIcon is one cached favicon.
type FeedIcon struct {
	Hash        string
	SrcURL      string
	ContentType string
	Bytes       []byte
	FetchedAt   time.Time
	ErrorCount  int
	LastError   string
}

// Cached reports whether the bytes are in hand. A row exists from the moment
// a feed's favicon URL is first known; the bytes arrive on first view, same
// as Image.
func (i FeedIcon) Cached() bool { return len(i.Bytes) > 0 }

// FeedIconByHash loads one favicon record. An unknown hash is ErrNotFound,
// which is what stops the proxy being asked to fetch a URL no feed's
// discovery ever produced.
func (s *Store) FeedIconByHash(ctx context.Context, hash string) (FeedIcon, error) {
	var icon FeedIcon
	var bytes []byte
	var fetched sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT url_hash, src_url, content_type, bytes, fetched_at, error_count, last_error
		  FROM reader_feed_icons WHERE url_hash = ?`, hash).
		Scan(&icon.Hash, &icon.SrcURL, &icon.ContentType, &bytes, &fetched,
			&icon.ErrorCount, &icon.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return FeedIcon{}, ErrNotFound
	}
	if err != nil {
		return FeedIcon{}, fmt.Errorf("reader: load feed icon: %w", err)
	}
	icon.Bytes = bytes
	if fetched.Valid {
		icon.FetchedAt = parseTime(fetched.String)
	}
	return icon, nil
}

// SaveFeedIconBytes caches a fetched favicon and clears any recorded failure.
func (s *Store) SaveFeedIconBytes(ctx context.Context, hash, contentType string, data []byte, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE reader_feed_icons
		   SET bytes = ?, content_type = ?, fetched_at = ?, last_error = '', error_count = 0
		 WHERE url_hash = ?`,
		data, contentType, formatTime(now), hash); err != nil {
		return fmt.Errorf("reader: cache feed icon: %w", err)
	}
	return nil
}

// SaveFeedIconFailure records that a fetch failed, so a dead favicon is not
// re-fetched on every page view.
func (s *Store) SaveFeedIconFailure(ctx context.Context, hash, msg string, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE reader_feed_icons
		   SET last_error = ?, error_count = error_count + 1, fetched_at = ?
		 WHERE url_hash = ?`,
		msg, formatTime(now), hash); err != nil {
		return fmt.Errorf("reader: record feed icon failure: %w", err)
	}
	return nil
}
```

Check the `crypto/sha256` and `encoding/hex` imports already exist in `store.go`'s import block; add them if not (they are currently only imported in `images.go`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/apps/reader/... -run TestFeedIcon -v`
Expected: PASS

- [ ] **Step 5: Run the full package suite**

Run: `go test ./internal/apps/reader/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/apps/reader/store.go internal/apps/reader/store_test.go
git commit -m "feat(reader): add favicon cache store methods"
```

---

### Task 4: Favicon proxy handler

**Files:**
- Create: `internal/apps/reader/faviconproxy.go`
- Modify: `internal/apps/reader/fetch.go:18-22` (add `MaxFaviconBytes`)
- Modify: `internal/apps/reader/app.go:114` (register the route)
- Test: `internal/apps/reader/faviconproxy_test.go`

**Interfaces:**
- Consumes: `Store.FeedIconByHash`, `Store.SaveFeedIconBytes`, `Store.SaveFeedIconFailure` (Task 3); `Client.Get` (`fetch.go`); `a.imgSem` (`app.go:36`, shared outbound-fetch concurrency guard — favicon fetches are rare enough not to need their own).
- Produces: route `GET /reader/favicon/{hash}` → `a.favicon`.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/reader/faviconproxy_test.go`, mirroring `imgproxy_test.go`:

```go
package reader_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/apptest"
)

// oneICO is the smallest thing http.DetectContentType calls an image.
var oneICO = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
}

// seedFeedIcon records a favicon row directly (bypassing discovery, which is
// tested separately) and returns its hash.
func seedFeedIcon(t *testing.T, s *apptest.Server[*reader.Store], srcURL string) string {
	t.Helper()
	hash := reader.FaviconHash(srcURL)
	if _, err := s.Store.DB().ExecContext(context.Background(),
		`INSERT INTO reader_feed_icons (url_hash, src_url) VALUES (?, ?)`, hash, srcURL); err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestFaviconProxyFetchesCachesAndServes(t *testing.T) {
	s, a := newServerWithApp(t)
	var hits int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(oneICO)
	}))
	defer origin.Close()

	hash := seedFeedIcon(t, s, origin.URL+"/favicon.ico")
	a.AllowPrivateFetchesForTest()

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/favicon/"+hash, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("first request returned %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}

	rec2 := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/favicon/"+hash, nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("second request returned %d", rec2.Code)
	}
	if hits != 1 {
		t.Errorf("origin was hit %d times; the second view must come from cache", hits)
	}
}

func TestFaviconProxyRefusesAnUnknownHash(t *testing.T) {
	s := newServer(t)

	hash := reader.FaviconHash("http://169.254.169.254/latest/meta-data/")
	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/favicon/"+hash, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown hash returned %d, want 404", rec.Code)
	}
}

func TestFaviconProxyRejectsNonImageContent(t *testing.T) {
	s, a := newServerWithApp(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon") // lying
		_, _ = w.Write([]byte("<html><body>not an image</body></html>"))
	}))
	defer origin.Close()

	hash := seedFeedIcon(t, s, origin.URL+"/favicon.ico")
	a.AllowPrivateFetchesForTest()

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/favicon/"+hash, nil))
	if rec.Code == http.StatusOK {
		t.Errorf("HTML served as a favicon because the origin claimed image/x-icon; sniff, do not trust")
	}
}

func TestFaviconProxyIsBehindAuth(t *testing.T) {
	s := newServer(t)
	hash := seedFeedIcon(t, s, "https://cdn.example/favicon.ico")

	rec := s.Do(t, s.Anonymous(t), httptest.NewRequest(http.MethodGet, "/reader/favicon/"+hash, nil))
	if rec.Code == http.StatusOK {
		t.Error("anonymous request served a favicon")
	}
}

func TestFaviconProxyRejectsAMalformedHash(t *testing.T) {
	s := newServer(t)
	for _, bad := range []string{"../../etc/passwd", "zzzz", strings.Repeat("a", 200)} {
		rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/favicon/"+bad, nil))
		if rec.Code == http.StatusOK {
			t.Errorf("malformed hash %q returned 200", bad)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/apps/reader/... -run TestFaviconProxy -v`
Expected: FAIL — `404 page not found` (route doesn't exist yet)

- [ ] **Step 3: Write the implementation**

Add to `internal/apps/reader/fetch.go`'s size-cap block (line 18-22):

```go
const (
	MaxFeedBytes    = 5 << 20
	MaxArticleBytes = 2 << 20
	MaxImageBytes   = 5 << 20
	// MaxFaviconBytes bounds a favicon fetch. Favicons are tiny; this just
	// caps a misbehaving server, the same role MaxImageBytes plays for
	// article images.
	MaxFaviconBytes = 64 << 10
)
```

Create `internal/apps/reader/faviconproxy.go`, mirroring `imgproxy.go`:

```go
package reader

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// validFaviconHash reports whether the path segment could be one of our
// hashes. Checked before any database work so a probe costs nothing.
func validFaviconHash(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// favicon serves a proxied feed favicon. It mirrors image() in imgproxy.go —
// same hash-not-URL security model, same cache/backoff behavior — over the
// separate reader_feed_icons table.
func (a *App) favicon(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	hash := r.PathValue("hash")
	if !validFaviconHash(hash) {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}

	icon, err := a.store.FeedIconByHash(r.Context(), hash)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if icon.Cached() {
		a.writeFeedIcon(w, r, icon)
		return
	}
	if icon.ErrorCount >= maxImageFetchAttempts ||
		(icon.ErrorCount > 0 && time.Since(icon.FetchedAt) < imageRetryBackoff) {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}

	fetched, err := a.fetchFeedIcon(r, icon)
	if err != nil {
		if errors.Is(err, context.Canceled) || r.Context().Err() != nil {
			a.deps.Log.Info("reader favicon fetch canceled", "src", icon.SrcURL, "error", err)
			return
		}
		a.deps.Log.Info("reader favicon fetch failed", "src", icon.SrcURL, "error", err)
		if err := a.store.SaveFeedIconFailure(r.Context(), hash, err.Error(), time.Now().UTC()); err != nil {
			a.deps.Log.Error("reader recording a favicon failure failed", "error", err)
		}
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	a.writeFeedIcon(w, r, fetched)
}

// fetchFeedIcon retrieves and caches one favicon.
func (a *App) fetchFeedIcon(r *http.Request, icon FeedIcon) (FeedIcon, error) {
	select {
	case a.imgSem <- struct{}{}:
		defer func() { <-a.imgSem }()
	case <-r.Context().Done():
		return FeedIcon{}, r.Context().Err()
	}

	res, err := a.client.Get(r.Context(), icon.SrcURL, GetOptions{
		MaxBytes: MaxFaviconBytes,
		Accept:   "image/*",
	})
	if err != nil {
		return FeedIcon{}, err
	}

	ct := http.DetectContentType(res.Body)
	if !strings.HasPrefix(ct, "image/") {
		return FeedIcon{}, errors.New("reader: response is not an image (" + ct + ")")
	}

	if err := a.store.SaveFeedIconBytes(r.Context(), icon.Hash, ct, res.Body, time.Now().UTC()); err != nil {
		return FeedIcon{}, err
	}
	icon.ContentType = ct
	icon.Bytes = res.Body
	return icon, nil
}

func (a *App) writeFeedIcon(w http.ResponseWriter, r *http.Request, icon FeedIcon) {
	etag := `"` + icon.Hash + `"`
	h := w.Header()
	h.Set("Cache-Control", imageCacheControl)
	h.Set("ETag", etag)
	h.Set("X-Content-Type-Options", "nosniff")

	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", icon.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(icon.Bytes)))
	if _, err := w.Write(icon.Bytes); err != nil {
		a.deps.Log.Info("reader writing a favicon failed", "error", err)
	}
}
```

Register the route in `internal/apps/reader/app.go`, next to the image route (line 114):

```go
	r.HandleFunc("GET /img/{hash}", a.image)
	r.HandleFunc("GET /favicon/{hash}", a.favicon)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/apps/reader/... -run TestFaviconProxy -v`
Expected: PASS

- [ ] **Step 5: Run the full package suite**

Run: `go test ./internal/apps/reader/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/apps/reader/faviconproxy.go internal/apps/reader/faviconproxy_test.go internal/apps/reader/fetch.go internal/apps/reader/app.go
git commit -m "feat(reader): add the favicon proxy handler"
```

---

### Task 5: `Feed`/`Subscription.FaviconURL` and `SetFaviconIfEmpty`

**Files:**
- Modify: `internal/apps/reader/store.go` (`Feed` at line 54-67, `Subscription` at line 85-119, `DueFeeds` at line 748-782, `FeedByID` at line 784-809, `Tree` at line 353-409; add `SetFaviconIfEmpty` right after the `SaveFetchResult` block, e.g. after line 863)
- Test: `internal/apps/reader/store_test.go`

**Interfaces:**
- Consumes: `FaviconHash` (Task 3).
- Produces:
  - `Feed.FaviconURL string`, populated by `DueFeeds`/`FeedByID`.
  - `Subscription.FaviconURL string`, populated by `Tree`.
  - `func (s Subscription) FaviconPath() string` — `""` if `FaviconURL == ""`, else `"/reader/favicon/" + FaviconHash(FaviconURL)`.
  - `func (s *Store) SetFaviconIfEmpty(ctx context.Context, feedID int64, faviconURL string) error` — sets `reader_feeds.favicon_url` only if it is currently empty, and inserts the matching `reader_feed_icons` row so the proxy from Task 4 can serve it. A no-op (not an error) if the feed already has a favicon.
- Consumed by: Task 6 (poll pipeline) and Task 7 (handler wiring) call `SetFaviconIfEmpty`; the `panes.partial.html` template (Task 8) reads `.Sub.FaviconPath`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/reader/store_test.go`:

```go
func TestSetFaviconIfEmptySetsAndCachesTheURL(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.SetFaviconIfEmpty(ctx, sub.FeedID, "https://example.com/favicon.ico"); err != nil {
		t.Fatal(err)
	}

	feed, err := f.store.FeedByID(ctx, sub.FeedID)
	if err != nil {
		t.Fatal(err)
	}
	if feed.FaviconURL != "https://example.com/favicon.ico" {
		t.Errorf("FaviconURL = %q, want the URL passed in", feed.FaviconURL)
	}

	hash := reader.FaviconHash("https://example.com/favicon.ico")
	if _, err := f.store.FeedIconByHash(ctx, hash); err != nil {
		t.Errorf("reader_feed_icons row was not created: %v", err)
	}
}

func TestSetFaviconIfEmptyDoesNotOverwrite(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetFaviconIfEmpty(ctx, sub.FeedID, "https://example.com/first.ico"); err != nil {
		t.Fatal(err)
	}

	if err := f.store.SetFaviconIfEmpty(ctx, sub.FeedID, "https://example.com/second.ico"); err != nil {
		t.Fatal(err)
	}

	feed, err := f.store.FeedByID(ctx, sub.FeedID)
	if err != nil {
		t.Fatal(err)
	}
	if feed.FaviconURL != "https://example.com/first.ico" {
		t.Errorf("FaviconURL = %q, a second call must not overwrite the first", feed.FaviconURL)
	}
}

func TestSubscriptionFaviconPath(t *testing.T) {
	withURL := reader.Subscription{FaviconURL: "https://example.com/favicon.ico"}
	if withURL.FaviconPath() != "/reader/favicon/"+reader.FaviconHash("https://example.com/favicon.ico") {
		t.Errorf("FaviconPath() = %q", withURL.FaviconPath())
	}

	without := reader.Subscription{}
	if without.FaviconPath() != "" {
		t.Errorf("FaviconPath() = %q, want empty when FaviconURL is empty", without.FaviconPath())
	}
}

func TestTreeLoadsFaviconURL(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetFaviconIfEmpty(ctx, sub.FeedID, "https://example.com/favicon.ico"); err != nil {
		t.Fatal(err)
	}

	tree, err := f.store.Tree(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root) != 1 || tree.Root[0].FaviconURL != "https://example.com/favicon.ico" {
		t.Errorf("Tree did not load FaviconURL: %+v", tree.Root)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/apps/reader/... -run 'TestSetFaviconIfEmpty|TestSubscriptionFaviconPath|TestTreeLoadsFaviconURL' -v`
Expected: FAIL with `undefined: ...` / `FaviconURL undefined`

- [ ] **Step 3: Write the implementation**

In `store.go`, add to `Feed` (line 54-67):

```go
type Feed struct {
	ID            int64
	URL           string
	ResolvedURL   string
	Title         string
	SiteURL       string
	FaviconURL    string
	ETag          string
	LastModified  string
	LastStatus    int
	LastError     string
	ErrorCount    int
	NextFetchAt   time.Time
	FetchInterval time.Duration // zero means DefaultFetchInterval
}
```

Add to `Subscription` (line 85-101), and its new method next to `DisplayName`/`Failing` (line 106-119):

```go
type Subscription struct {
	ID       int64
	FeedID   int64
	FolderID *int64
	Title    string // override; empty means use FeedTitle
	FeedURL  string
	FeedName string
	SiteURL     string
	FaviconURL  string
	AddedAt time.Time
	ErrorCount int
	LastError  string
}

// FaviconPath is the proxy URL for this feed's favicon, or "" if none is
// known yet — the template's cue to render the generic icon instead.
func (s Subscription) FaviconPath() string {
	if s.FaviconURL == "" {
		return ""
	}
	return "/reader/favicon/" + FaviconHash(s.FaviconURL)
}
```

Update `DueFeeds`'s query and scan (line 748-782):

```go
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, url, resolved_url, title, site_url, favicon_url, etag, last_modified,
		       last_status, last_error, error_count, next_fetch_at, fetch_interval
		  FROM reader_feeds
		 WHERE next_fetch_at <= ?
		 ORDER BY next_fetch_at
		 LIMIT ?`, formatTime(now), limit)
	...
		if err := rows.Scan(&f.ID, &f.URL, &f.ResolvedURL, &f.Title, &f.SiteURL, &f.FaviconURL,
			&f.ETag, &f.LastModified, &f.LastStatus, &f.LastError, &f.ErrorCount,
			&next, &interval); err != nil {
```

Update `FeedByID`'s query and scan (line 784-809) the same way:

```go
	err := s.db.QueryRowContext(ctx, `
		SELECT id, url, resolved_url, title, site_url, favicon_url, etag, last_modified,
		       last_status, last_error, error_count, next_fetch_at, fetch_interval
		  FROM reader_feeds
		 WHERE id = ?`, feedID).Scan(&f.ID, &f.URL, &f.ResolvedURL, &f.Title, &f.SiteURL, &f.FaviconURL,
		&f.ETag, &f.LastModified, &f.LastStatus, &f.LastError, &f.ErrorCount,
		&next, &interval)
```

Update `Tree`'s subscription query and scan (line 378-397):

```go
	subRows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.feed_id, s.folder_id, s.title, s.added_at, f.url, f.title,
		       f.site_url, f.favicon_url, f.error_count, f.last_error
		  FROM reader_subs s JOIN reader_feeds f ON f.id = s.feed_id
		 WHERE s.user_id = ?
		 ORDER BY s.position, coalesce(nullif(s.title, ''), nullif(f.title, ''), f.url)`,
		userID)
	...
		if err := subRows.Scan(&sub.ID, &sub.FeedID, &sub.FolderID, &sub.Title,
			&added, &sub.FeedURL, &sub.FeedName, &sub.SiteURL, &sub.FaviconURL,
			&sub.ErrorCount, &sub.LastError); err != nil {
```

Add `SetFaviconIfEmpty` after `SaveFetchResult` (after line 863):

```go
// SetFaviconIfEmpty records a feed's favicon URL, but only the first time:
// the WHERE clause on the UPDATE is a no-op once favicon_url is already set,
// so a later call — a second poll, a second discovery attempt — can never
// clobber it. It also seeds the reader_feed_icons row the favicon proxy
// (faviconproxy.go) looks up by hash, in the same transaction, so the two
// are never out of sync.
func (s *Store) SetFaviconIfEmpty(ctx context.Context, feedID int64, faviconURL string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reader: begin set favicon: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`UPDATE reader_feeds SET favicon_url = ? WHERE id = ? AND favicon_url = ''`,
		faviconURL, feedID)
	if err != nil {
		return fmt.Errorf("reader: set favicon url: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reader: set favicon rows: %w", err)
	}
	if n == 0 {
		// Already set (or the feed does not exist) — nothing to do.
		return nil
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reader_feed_icons (url_hash, src_url) VALUES (?, ?)
		ON CONFLICT (url_hash) DO NOTHING`,
		FaviconHash(faviconURL), faviconURL); err != nil {
		return fmt.Errorf("reader: insert feed icon: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reader: commit set favicon: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/apps/reader/... -run 'TestSetFaviconIfEmpty|TestSubscriptionFaviconPath|TestTreeLoadsFaviconURL' -v`
Expected: PASS

- [ ] **Step 5: Run the full package suite**

Run: `go test ./internal/apps/reader/...`
Expected: PASS — this touches `DueFeeds`/`FeedByID`/`Tree` scan lists, so a typo here would break many existing tests, not just the new ones.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/reader/store.go internal/apps/reader/store_test.go
git commit -m "feat(reader): load FaviconURL and add SetFaviconIfEmpty"
```

---

### Task 6: Poll pipeline — fill in the direct-feed-URL case

**Files:**
- Modify: `internal/apps/reader/poll.go` (`pollOne`, lines 131-206)
- Test: `internal/apps/reader/poll_test.go` (create if it does not already cover `pollOne` in isolation — check first; otherwise add to `handlers_test.go`, which already exercises `pollOne` indirectly through `FetchNow`)

**Interfaces:**
- Consumes: `DiscoverFavicon` (Task 2), `Store.SetFaviconIfEmpty` (Task 5).
- Produces: after `pollOne` runs a poll whose feed has a known site URL and no favicon yet, `Store.SetFaviconIfEmpty` is called with a `DiscoverFavicon(nil, siteURL)` guess.

- [ ] **Step 1: Check for an existing `poll_test.go`**

Run: `ls internal/apps/reader/poll_test.go 2>/dev/null || echo "no dedicated poll_test.go"`

If it exists, add the new test there; otherwise add it to `handlers_test.go` near `TestSubscribeFetchesTheFeedImmediately` (it already exercises the same synchronous fetch-on-add path).

- [ ] **Step 2: Write the failing test**

```go
// TestFetchOnAddDerivesAFaviconGuessForADirectFeedURL guards the common case
// this task exists for: pasting a feed URL directly never fetches the site's
// homepage (see TestSubscribeToADirectFeedURLStillWorks), so the favicon
// must come from a pure string derivation off the feed's own site URL, with
// no extra request to the origin.
func TestFetchOnAddDerivesAFaviconGuessForADirectFeedURL(t *testing.T) {
	s, a := newServerWithApp(t)
	ctx := context.Background()

	var hits int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(fixture(t, "rss2.xml")) // <link>https://example.com/</link>
	}))
	defer origin.Close()
	a.AllowPrivateFetchesForTest()

	if rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{"url": {origin.URL + "/feed.xml"}}); rec.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d", rec.Code)
	}
	if hits != 2 {
		t.Fatalf("origin was fetched %d times, want 2 (discovery + fetch-on-add) — a favicon fetch would make this 3", hits)
	}

	tree, err := s.Store.Tree(ctx, s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root) != 1 {
		t.Fatalf("subscribed to %d feeds, want 1", len(tree.Root))
	}
	if tree.Root[0].FaviconURL != "https://example.com/favicon.ico" {
		t.Errorf("FaviconURL = %q, want the derived /favicon.ico guess", tree.Root[0].FaviconURL)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/apps/reader/... -run TestFetchOnAddDerivesAFaviconGuessForADirectFeedURL -v`
Expected: FAIL — `FaviconURL = ""`

- [ ] **Step 4: Write the implementation**

In `internal/apps/reader/poll.go`, add a helper and two call sites. After `record` (line 208-212):

```go
// maybeGuessFavicon fills in a feed's favicon the first time its site URL is
// known, using only data already in hand: DiscoverFavicon with no page HTML
// is a pure string derivation to "<origin>/favicon.ico", not a fetch. f is
// the feed's state from before this poll, so f.FaviconURL is accurate to
// check against; siteURL is this poll's most current value (the freshly
// parsed one on a success, or f.SiteURL unchanged on a 304).
func (p *Poller) maybeGuessFavicon(ctx context.Context, f Feed, siteURL string) {
	if f.FaviconURL != "" || siteURL == "" {
		return
	}
	guess := DiscoverFavicon(nil, siteURL)
	if guess == "" {
		return
	}
	if err := p.store.SetFaviconIfEmpty(ctx, f.ID, guess); err != nil {
		p.log.Error("reader saving favicon guess failed", "feed_id", f.ID, "error", err)
	}
}
```

In the `NotModified` branch (after the existing `p.record(...)` call, currently ending at line 167):

```go
	if res.NotModified {
		p.record(ctx, FetchResult{
			FeedID:       f.ID,
			ResolvedURL:  res.FinalURL,
			ETag:         f.ETag,
			LastModified: f.LastModified,
			Status:       res.Status,
			FetchedAt:    now,
			NextFetchAt:  NextFetchAt(now, f.Interval(), 0),
		})
		p.maybeGuessFavicon(ctx, f, f.SiteURL)
		return
	}
```

In the final success branch (after the existing `p.record(...)` call, currently ending at line 205):

```go
	p.record(ctx, FetchResult{
		FeedID:       f.ID,
		ResolvedURL:  res.FinalURL,
		Title:        parsed.Title,
		SiteURL:      parsed.SiteURL,
		ETag:         res.ETag,
		LastModified: res.LastModified,
		Status:       res.Status,
		FetchedAt:    now,
		NextFetchAt:  NextFetchAt(now, f.Interval(), 0),
	})
	siteURL := f.SiteURL
	if parsed.SiteURL != "" {
		siteURL = parsed.SiteURL
	}
	p.maybeGuessFavicon(ctx, f, siteURL)
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/apps/reader/... -run TestFetchOnAddDerivesAFaviconGuessForADirectFeedURL -v`
Expected: PASS

- [ ] **Step 6: Run the full package suite**

Run: `go test ./internal/apps/reader/...`
Expected: PASS — in particular, every existing `TestSubscribe*`/poll test that asserts an exact origin hit count must still pass unchanged, since `maybeGuessFavicon` makes no network calls.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/reader/poll.go internal/apps/reader/handlers_test.go
git commit -m "feat(reader): derive a favicon guess during polling with no new fetches"
```

---

### Task 7: Add-feed handler — favicon from webpage discovery

**Files:**
- Modify: `internal/apps/reader/handlers.go` (`resolveFeedURL`, lines 650-687; its call site at line 594; `subscribe`, around line 608-618)
- Test: `internal/apps/reader/handlers_test.go`

**Interfaces:**
- Consumes: `DiscoverFavicon` (Task 2), `Store.SetFaviconIfEmpty` (Task 5).
- Produces: `resolveFeedURL` signature becomes `func (a *App) resolveFeedURL(ctx context.Context, raw string) (feedURL, faviconURL string, candidates []FeedCandidate, err error)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/apps/reader/handlers_test.go`, near `TestSubscribeAcceptsASiteURLAndFindsTheFeed` (line 1716):

```go
// A favicon discovered from the webpage that led to this feed (a real
// <link rel="icon">, not the generic /favicon.ico guess) must survive into
// the tree — this is the "webpage discovery" half of favicon support; the
// direct-feed-URL half is TestFetchOnAddDerivesAFaviconGuessForADirectFeedURL.
func TestSubscribeViaWebpageDiscoveryUsesTheAdvertisedFavicon(t *testing.T) {
	s, a := newServerWithApp(t)
	ctx := context.Background()

	mux := http.NewServeMux()
	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(fixture(t, "rss2.xml"))
	})
	origin := httptest.NewServer(mux)
	defer origin.Close()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head>
			<link rel="alternate" type="application/rss+xml" href="` + origin.URL + `/feed.xml">
			<link rel="icon" href="/static/icon.png">
			</head><body>hi</body></html>`))
	})
	a.AllowPrivateFetchesForTest()

	if rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{"url": {origin.URL + "/"}}); rec.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d: %s", rec.Code, rec.Body.String())
	}

	tree, err := s.Store.Tree(ctx, s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root) != 1 {
		t.Fatalf("subscribed to %d feeds, want 1", len(tree.Root))
	}
	want := origin.URL + "/static/icon.png"
	if tree.Root[0].FaviconURL != want {
		t.Errorf("FaviconURL = %q, want the advertised icon %q", tree.Root[0].FaviconURL, want)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/apps/reader/... -run TestSubscribeViaWebpageDiscoveryUsesTheAdvertisedFavicon -v`
Expected: FAIL — `FaviconURL = ""` (or the /favicon.ico guess from Task 6's fallback, since discovery isn't wired into this path yet)

- [ ] **Step 3: Write the implementation**

In `internal/apps/reader/handlers.go`, change `resolveFeedURL`'s signature and its three return points (lines 659-687):

```go
func (a *App) resolveFeedURL(ctx context.Context, raw string) (feedURL, faviconURL string, candidates []FeedCandidate, err error) {
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	res, err := a.client.Get(ctx, raw, GetOptions{MaxBytes: MaxFeedBytes})
	if err != nil {
		return "", "", nil, err
	}

	// Already a feed? Then we are done, and the poller will refetch it on its
	// own schedule. res.Body is feed content here, not a webpage, so there is
	// no favicon to discover from it — Task 6's poll-time guess covers this
	// case once the feed's site URL is known.
	if _, err := ParseFeed(res.Body, res.FinalURL); err == nil {
		return res.FinalURL, "", nil, nil
	}

	// Not a feed: res.Body is a webpage we already paid to fetch, so favicon
	// discovery here costs nothing extra.
	found := DiscoverFavicon(res.Body, res.FinalURL)

	candidates = FeedsInPage(res.Body, res.FinalURL)
	if len(candidates) == 1 {
		return candidates[0].URL, found, nil, nil
	}
	if len(candidates) > 1 {
		return "", "", candidates, nil
	}

	if probed, ok := a.probeForFeed(ctx, res.FinalURL); ok {
		return probed, found, nil, nil
	}
	return "", "", nil, ErrNoFeedFound
}
```

Update the call site (line 594):

```go
	feedURL, faviconURL, candidates, err := a.resolveFeedURL(r.Context(), raw)
```

In `subscribe`, right after `Subscribe` succeeds (around line 608-618, before the synchronous `FetchNow` call):

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
	if faviconURL != "" {
		if err := a.store.SetFaviconIfEmpty(r.Context(), sub.FeedID, faviconURL); err != nil {
			a.deps.Log.Error("reader saving discovered favicon failed", "feed_id", sub.FeedID, "error", err)
		}
	}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/apps/reader/... -run TestSubscribeViaWebpageDiscoveryUsesTheAdvertisedFavicon -v`
Expected: PASS

- [ ] **Step 5: Run the full package suite**

Run: `go test ./internal/apps/reader/...`
Expected: PASS — `resolveFeedURL`'s signature change touches every call site and `SetDiscoveryTimeoutForTest`'s doc comment references it; confirm `go vet ./...` and `go build ./...` are clean too.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/reader/handlers.go internal/apps/reader/handlers_test.go
git commit -m "feat(reader): use a webpage's advertised favicon when discovery already fetched it"
```

---

### Task 8: Render the favicon in the tree

**Files:**
- Modify: `internal/apps/reader/templates/panes.partial.html` (`sub-row`, lines 226-229)
- Modify: `internal/ui/toolbar_icons.go` (add `"rss"`)
- Modify: `internal/ui/static/app.css` (near the `.reader-sub`/`.reader-node` rules, line ~1897-1923)
- Test: `internal/apps/reader/handlers_test.go`

**Interfaces:**
- Consumes: `Subscription.FaviconPath()` (Task 5), `{{ticon "rss"}}` (new map entry, existing template func).

- [ ] **Step 1: Write the failing tests**

Add to `internal/apps/reader/handlers_test.go`:

```go
func TestTreeShowsAFaviconImageWhenKnown(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetFaviconIfEmpty(ctx, sub.FeedID, "https://example.com/favicon.ico"); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/reader/")
	img := doc.MustHave("img.reader-favicon")
	want := "/reader/favicon/" + reader.FaviconHash("https://example.com/favicon.ico")
	if src, _ := htmlassert.Attr(img, "src"); src != want {
		t.Errorf("favicon img src = %q, want %q", src, want)
	}
}

func TestTreeShowsTheGenericIconWithNoFavicon(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()

	if _, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/reader/")
	doc.MustNotHave("img.reader-favicon")
	doc.MustHave("span.reader-favicon")
}
```

`htmlassert` is already imported in `handlers_test.go` (line 18); this adds a use of its exported `Attr` function alongside the package's existing `Doc` methods.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/apps/reader/... -run TestTreeShows -v`
Expected: FAIL — neither the proxy URL nor `reader-favicon` appears yet

- [ ] **Step 3: Write the implementation**

Add `"rss"` to `toolbarIcons` in `internal/ui/toolbar_icons.go`, alongside the other entries:

```go
	"rss": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<circle cx="5" cy="19" r="1.5" fill="currentColor" stroke="none"/>
		<path d="M4 11a9 9 0 0 1 9 9"/>
		<path d="M4 4a16 16 0 0 1 16 16"/>
	</svg>`,
```

Update `sub-row` in `internal/apps/reader/templates/panes.partial.html` (lines 226-229). The fallback is a sibling `<span>` toggled by `onerror` — no string-editing of serialized HTML at runtime, and no double-hidden-element juggling: capture the sibling in a local before removing `this`, since removing `this` first would leave `this.nextElementSibling` with nothing to refer to:

```html
{{define "sub-row"}}
<li class="reader-sub{{if eq .Sub.ID .Tree.ActiveID}} is-active{{end}}">
  {{if .Sub.FaviconPath}}
    <img class="reader-favicon" src="{{.Sub.FaviconPath}}" width="16" height="16" alt=""
         onerror="var f=this.nextElementSibling; f.hidden=false; this.remove()">
    <span class="reader-favicon" hidden>{{ticon "rss"}}</span>
  {{else}}
    <span class="reader-favicon">{{ticon "rss"}}</span>
  {{end}}
  <a href="/reader/feed/{{.Sub.ID}}" hx-get="/reader/feed/{{.Sub.ID}}"
       hx-target="#reader-panes" hx-swap="outerHTML" hx-push-url="true">{{.Sub.DisplayName}}</a>{{if .Sub.Failing}} <span class="reader-failing" title="{{.Sub.LastError}}">&#9888;</span>{{end}}
```

(Keep the rest of `sub-row`, lines 230 onward, unchanged.)

Add to `internal/ui/static/app.css`, after the `.reader-node a, .reader-sub a { ... }` rule (ends around line 1923):

```css
/* The favicon sits before the title link. .reader-node/.reader-sub are
   align-items: baseline (right for text), which is wrong for a small image
   or icon glyph — center it instead. .toolbar-icon already sizes the "rss"
   fallback svg to 1rem (16px), matching the <img>'s own 16x16. */
.reader-favicon {
	flex: none;
	align-self: center;
	display: inline-flex;
	border-radius: 2px;
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/apps/reader/... -run TestTreeShows -v`
Expected: PASS

- [ ] **Step 5: Run the full package suite and the wider build**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS across the whole module — this task touches a shared template partial and the shared `internal/ui` icon map, so confirm no other app's use of `toolbarIcons` or `panes.partial.html`-adjacent code broke.

- [ ] **Step 6: Manually verify in the browser**

Start the app (`go run ./cmd/onsuite` or however this project is normally run locally), sign in, add a feed via a direct feed URL (e.g. a real RSS URL) and one via a site homepage, and confirm:
- The direct-feed-URL subscription shows a favicon once its site actually serves `/favicon.ico` (or the generic RSS glyph if it 404s).
- The homepage-discovered subscription shows the site's actual `<link rel="icon">` image.
- A feed with a broken/missing favicon shows the generic RSS glyph, not a broken-image icon.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/reader/templates/panes.partial.html internal/ui/toolbar_icons.go internal/ui/static/app.css internal/apps/reader/handlers_test.go
git commit -m "feat(reader): render feed favicons in the sidebar tree"
```

---

## Post-plan

- Push the branch and open a PR (per this repo's branch-protection convention — no direct pushes to `main`).
- File any follow-ups that come up during review as GitHub issues, per this project's usual practice, rather than leaving them as TODOs in code or comments.
