# ON Later L0 — move Reader's fetch, extraction and favicon code into the platform

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move ON Reader's guarded HTTP client, readability extraction +
sanitising, and favicon discovery out of `internal/apps/reader` into
`internal/platform/{webfetch,article,favicon}` so ON Later can use them, with
no behaviour change in Reader.

**Architecture:** Three new leaf platform packages. `webfetch` owns outbound
HTTP (SSRF guard, caps, redirects), URL resolution/hashing, image fetching
with content sniffing, and the image retry rule. `article` owns readability
extraction and the bluemonday policies; it takes an image-src builder so each
app keeps its own image route. `favicon` owns favicon URL discovery. Reader
keeps thin wrappers only where it binds Reader-specific config (User-Agent,
feed Accept header, `/reader/img/` path).

**Deviation from the spec:** the spec puts the favicon give-up rule in
`favicon`. Reader applies the same rule to images and favicons, so it lives
in `webfetch.GivenUp`. `favicon` is discovery only. Reader's homepage lookup
and the #451/#455 repair rule stay in Reader — they are tied to its feed
table, and ON Later always has the saved page's HTML, so `favicon.Discover`
on that page is enough.

**Tech Stack:** Go, `github.com/go-shiori/go-readability`,
`github.com/microcosm-cc/bluemonday`, `golang.org/x/net/html`.

**Spec:** [2026-10-05-on-later-design.md](../specs/2026-10-05-on-later-design.md), section "L0: shared platform packages". **Issue:** #504.

## Global Constraints

- No behaviour change in ON Reader. Reader's tests pass with only import-path
  / package-qualifier edits, or by moving to the new package unchanged apart
  from those edits. **Do not change test assertions** — if one fails, the
  move is wrong, not the test. The one allowed exception: an assertion on an
  error-message *prefix* (`"reader: "` → `"webfetch: "` / `"article: "`).
- No new module dependencies; `go mod tidy && git diff --exit-code go.mod go.sum` stays clean.
- The new packages import nothing from `internal/apps/*` and no
  `internal/platform/*` package except `article → webfetch` and
  `favicon → webfetch`.
- go-readability is imported only by `internal/platform/article/extract.go`.
- Moved doc comments keep their "why" content; reword only references that
  no longer make sense outside Reader (e.g. "R4", "feed").
- Full check green after every task:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- Commits: Conventional Commits, scope `platform` (e.g. `refactor(platform): …`).
- Out of scope: ON Flash's own mirrored fetch client (`internal/apps/flash/media_fetch.go`) — leave it alone.

---

### Task 1: `internal/platform/webfetch`

**Files:**
- Create: `internal/platform/webfetch/webfetch.go` (moved from `internal/apps/reader/fetch.go`)
- Create: `internal/platform/webfetch/url.go`
- Create: `internal/platform/webfetch/image.go`
- Create: `internal/platform/webfetch/webfetch_test.go` (moved from `internal/apps/reader/fetch_test.go`)
- Create: `internal/platform/webfetch/url_test.go`, `internal/platform/webfetch/image_test.go`
- Delete: `internal/apps/reader/fetch.go`, `internal/apps/reader/fetch_test.go`
- Modify: `internal/apps/reader/discover.go` (drop `resolveAbsoluteHTTPURL`), `app.go`, `handlers.go`, `poll.go`, `imgproxy.go`, `faviconproxy.go`, `store.go`, `images.go`, `favicon.go`, and any test file the compiler flags
- Create: `internal/apps/reader/client.go` (Reader's `NewClient` wrapper)

**Interfaces — Produces:**
```go
package webfetch
var ErrBlockedAddress error
func DenyPrivateAddr(address string) error
const MaxPageBytes = 2 << 20; const MaxImageBytes = 5 << 20; const MaxFaviconBytes = 64 << 10
type Config struct { UserAgent, DefaultAccept string; DefaultMaxBytes int64 }
type Client struct { DenyAddr func(address string) error /* + unexported */ }
func New(cfg Config) *Client
type GetOptions struct { ETag, LastModified string; MaxBytes int64; Accept string }
type Response struct { Status int; Body []byte; ETag, LastModified, ContentType, FinalURL string; NotModified bool }
func (c *Client) Get(ctx context.Context, rawURL string, opts GetOptions) (*Response, error)
func ResolveHTTPURL(raw string, base *url.URL) (string, bool)
func URLHash(srcURL string) string
func ValidURLHash(s string) bool
func (c *Client) GetImage(ctx context.Context, srcURL string, maxBytes int64) (contentType string, body []byte, err error)
const MaxImageAttempts = 3
const ImageRetryBackoff = time.Hour
func GivenUp(errorCount int, lastAttempt, now time.Time) bool
```
Reader keeps: `func NewClient(version string) *webfetch.Client`, `const MaxFeedBytes = 5 << 20`.

- [ ] **Step 1: Move the client and its tests**

```bash
mkdir -p internal/platform/webfetch
git mv internal/apps/reader/fetch.go internal/platform/webfetch/webfetch.go
git mv internal/apps/reader/fetch_test.go internal/platform/webfetch/webfetch_test.go
```

In `webfetch.go`:
- `package webfetch`; add a package doc comment:
  ```go
  // Package webfetch is the suite's one way to make outbound HTTP requests
  // to publisher-controlled URLs: an SSRF guard checked against the resolved
  // address at dial time, a redirect cap with per-hop scheme checks, and body
  // caps applied after decompression. Apps that fetch the open web use it
  // rather than net/http directly, so those guards live in one place.
  package webfetch
  ```
- Replace the size-cap block with:
  ```go
  // Body size caps for callers to pass as GetOptions.MaxBytes. Applied after
  // decompression, which is where a compression bomb would otherwise land.
  const (
  	MaxPageBytes    = 2 << 20
  	MaxImageBytes   = 5 << 20
  	MaxFaviconBytes = 64 << 10
  )
  ```
- Replace `NewClient` with `Config` + `New`, and store the defaults on the client:
  ```go
  // Config is what differs between the apps that use a Client.
  type Config struct {
  	// UserAgent identifies the app to publishers, e.g.
  	// "onsuite/v2.1.0 (ON Reader; +https://github.com/iliafrenkel/on-suite)".
  	UserAgent string
  	// DefaultAccept is sent when GetOptions.Accept is empty.
  	DefaultAccept string
  	// DefaultMaxBytes bounds the body when GetOptions.MaxBytes is zero.
  	DefaultMaxBytes int64
  }

  // New returns a client with every guard in place.
  func New(cfg Config) *Client {
  	c := &Client{cfg: cfg, DenyAddr: DenyPrivateAddr}
  	// … dialer / http.Client construction exactly as before …
  	return c
  }
  ```
  The `Client` struct swaps its `ua string` field for `cfg Config`. In `Get`:
  `req.Header.Set("User-Agent", c.cfg.UserAgent)`; the Accept fallback becomes
  `c.cfg.DefaultAccept` (only set the header when it is non-empty); the
  MaxBytes fallback becomes `c.cfg.DefaultMaxBytes`, and if that is also zero,
  `MaxPageBytes`.
- Change every error-string prefix `"reader: "` to `"webfetch: "`
  (`ErrBlockedAddress` becomes `errors.New("webfetch: blocked address")`).
- Update the `Client` doc comment: "Client is the one HTTP client an app makes
  outbound requests with…" (drop "R3").

In `webfetch_test.go`: `package webfetch_test`, import
`github.com/iliafrenkel/on-suite/internal/platform/webfetch`, replace
`reader.` with `webfetch.`, and make `testClient` / the default-client test use:
```go
func newTestClient() *webfetch.Client {
	return webfetch.New(webfetch.Config{UserAgent: "test", DefaultMaxBytes: 5 << 20})
}
```
(`testClient()` = `newTestClient()` with `DenyAddr` overridden;
`TestDefaultClientRefusesPrivateAddresses` uses `newTestClient()` untouched.)
`TestGetRejectsAnOversizedBody` must pass an explicit `MaxBytes` if it relied
on the old 5 MiB default — check, and keep the asserted limit identical.

- [ ] **Step 2: Write the failing tests for the new helpers**

`internal/platform/webfetch/url_test.go`:
```go
package webfetch_test

import (
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

func TestResolveHTTPURL(t *testing.T) {
	base, _ := url.Parse("https://example.com/posts/one")
	for _, tc := range []struct {
		raw, want string
		ok        bool
	}{
		{"/img/a.png", "https://example.com/img/a.png", true},
		{"b.png", "https://example.com/posts/b.png", true},
		{"https://cdn.example/c.png", "https://cdn.example/c.png", true},
		{"javascript:alert(1)", "", false},
		{"data:image/png;base64,AAAA", "", false},
		{"file:///etc/passwd", "", false},
		{"", "", false},
	} {
		got, ok := webfetch.ResolveHTTPURL(tc.raw, base)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ResolveHTTPURL(%q) = %q, %v; want %q, %v", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

func TestURLHashIsStableAndValid(t *testing.T) {
	a := webfetch.URLHash("https://example.com/a.png")
	if a != webfetch.URLHash("https://example.com/a.png") {
		t.Fatal("hash is not stable")
	}
	if a == webfetch.URLHash("https://example.com/b.png") {
		t.Fatal("different URLs share a hash")
	}
	if len(a) != 32 || !webfetch.ValidURLHash(a) {
		t.Fatalf("hash %q is not 32 lowercase hex chars", a)
	}
	for _, bad := range []string{"", "ABCDEF0123456789abcdef0123456789", "../../etc/passwd", a + "0"} {
		if webfetch.ValidURLHash(bad) {
			t.Errorf("ValidURLHash(%q) = true", bad)
		}
	}
}
```

`internal/platform/webfetch/image_test.go`:
```go
package webfetch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// A 1x1 transparent PNG.
var tinyPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82")

func TestGetImageSniffsTheContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html") // a lie; sniffing wins
		_, _ = w.Write(tinyPNG)
	}))
	defer srv.Close()

	ct, body, err := testClient().GetImage(context.Background(), srv.URL, webfetch.MaxImageBytes)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "image/png" || len(body) != len(tinyPNG) {
		t.Errorf("got %q, %d bytes; want image/png, %d bytes", ct, len(body), len(tinyPNG))
	}
}

func TestGetImageRefusesSomethingThatIsNotAnImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png") // a lie the other way
		_, _ = w.Write([]byte("<html><script>alert(1)</script></html>"))
	}))
	defer srv.Close()

	if _, _, err := testClient().GetImage(context.Background(), srv.URL, webfetch.MaxImageBytes); err == nil {
		t.Fatal("HTML was accepted as an image")
	}
}

func TestGivenUp(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		errors int
		last   time.Time
		want   bool
	}{
		{"never failed", 0, time.Time{}, false},
		{"failed inside backoff", 1, now.Add(-time.Minute), true},
		{"failed outside backoff", 1, now.Add(-webfetch.ImageRetryBackoff - time.Second), false},
		{"hit the attempt cap", webfetch.MaxImageAttempts, now.Add(-48 * time.Hour), true},
	} {
		if got := webfetch.GivenUp(tc.errors, tc.last, now); got != tc.want {
			t.Errorf("%s: GivenUp = %v, want %v", tc.name, got, tc.want)
		}
	}
}
```

- [ ] **Step 3: Run them to confirm they fail**

Run: `go test ./internal/platform/webfetch/...`
Expected: compile failure — `ResolveHTTPURL`, `URLHash`, `ValidURLHash`, `GetImage`, `GivenUp`, `MaxImageAttempts`, `ImageRetryBackoff` undefined.

- [ ] **Step 4: Implement `url.go`**

Move `resolveAbsoluteHTTPURL` out of `internal/apps/reader/discover.go` (delete it there) and add the hash helpers:
```go
package webfetch

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
)

// ResolveHTTPURL resolves raw against base and accepts http and https only —
// javascript:, data: and file: are refused so they never reach a fetcher or
// an img src.
func ResolveHTTPURL(raw string, base *url.URL) (string, bool) {
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if !u.IsAbs() && base != nil {
		u = base.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	return u.String(), true
}

// URLHash identifies a remote resource (an image, a favicon) by its source
// URL: 128 bits of SHA-256, hex encoded. Content-addressing the URL rather
// than assigning an id lets HTML be rewritten before anything touches a
// database, and lets a proxy route take a hash instead of a URL, so no input
// can make it fetch something this server never saw.
func URLHash(srcURL string) string {
	sum := sha256.Sum256([]byte(srcURL))
	return hex.EncodeToString(sum[:16])
}

// ValidURLHash reports whether s could be a URLHash. Proxy routes check it
// before any database work, so a probe costs nothing.
func ValidURLHash(s string) bool {
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
```

- [ ] **Step 5: Implement `image.go`**

Move the retry rationale comments from `internal/apps/reader/imgproxy.go`'s
`maxImageFetchAttempts` / `imageRetryBackoff` onto these:
```go
package webfetch

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// MaxImageAttempts is how many consecutive failures an image or favicon is
// allowed before it is given up on permanently. [moved rationale]
const MaxImageAttempts = 3

// ImageRetryBackoff is how long to wait after a failure before trying again.
// [moved rationale]
const ImageRetryBackoff = 1 * time.Hour

// GivenUp reports whether a proxy should refuse to fetch a resource at now:
// it has failed MaxImageAttempts times, or it failed recently and is still
// inside ImageRetryBackoff.
func GivenUp(errorCount int, lastAttempt, now time.Time) bool {
	return errorCount >= MaxImageAttempts ||
		(errorCount > 0 && now.Sub(lastAttempt) < ImageRetryBackoff)
}

// GetImage fetches srcURL and returns its sniffed content type and bytes.
//
// Sniff rather than trust: a publisher claiming image/png over an HTML
// document is exactly how a proxy becomes an HTML-injection vector on its
// own origin.
func (c *Client) GetImage(ctx context.Context, srcURL string, maxBytes int64) (string, []byte, error) {
	res, err := c.Get(ctx, srcURL, GetOptions{MaxBytes: maxBytes, Accept: "image/*"})
	if err != nil {
		return "", nil, err
	}
	ct := http.DetectContentType(res.Body)
	if !strings.HasPrefix(ct, "image/") {
		return "", nil, errors.New("webfetch: response is not an image (" + ct + ")")
	}
	return ct, res.Body, nil
}
```

- [ ] **Step 6: Run the webfetch tests**

Run: `go test ./internal/platform/webfetch/... -race -count=1 -v`
Expected: PASS, including every test moved from `fetch_test.go`.

- [ ] **Step 7: Switch Reader to webfetch**

Create `internal/apps/reader/client.go`:
```go
package reader

import "github.com/iliafrenkel/on-suite/internal/platform/webfetch"

// MaxFeedBytes bounds a feed fetch; it is also the client's default cap.
const MaxFeedBytes = 5 << 20

// feedAccept is sent when a caller doesn't ask for something else: feed
// polling is what most of Reader's fetches are.
const feedAccept = "application/atom+xml, application/rss+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.5"

// NewClient returns Reader's outbound client: webfetch's guards, Reader's
// User-Agent, a feed-shaped Accept header and the feed body cap by default.
func NewClient(version string) *webfetch.Client {
	return webfetch.New(webfetch.Config{
		UserAgent:       "onsuite/" + version + " (ON Reader; +https://github.com/iliafrenkel/on-suite)",
		DefaultAccept:   feedAccept,
		DefaultMaxBytes: MaxFeedBytes,
	})
}
```

Then, across `internal/apps/reader` (find them with
`grep -rn "Client\b\|GetOptions\|Response\b\|DenyPrivateAddr\|ErrBlockedAddress\|MaxArticleBytes\|MaxImageBytes\|MaxFaviconBytes\|resolveAbsoluteHTTPURL\|validImageHash\|validFaviconHash\|maxImageFetchAttempts\|imageRetryBackoff" internal/apps/reader`):
- `*Client` → `*webfetch.Client`; `GetOptions` → `webfetch.GetOptions`; `Response` → `webfetch.Response`; `ErrBlockedAddress` → `webfetch.ErrBlockedAddress`; `DenyPrivateAddr` → `webfetch.DenyPrivateAddr`.
- `MaxArticleBytes` → `webfetch.MaxPageBytes`; `MaxImageBytes` / `MaxFaviconBytes` → `webfetch.` versions.
- `resolveAbsoluteHTTPURL` → `webfetch.ResolveHTTPURL`.
- `validImageHash` / `validFaviconHash` → delete both, use `webfetch.ValidURLHash`.
- `ImageHash` (images.go) and `FaviconHash` (store.go) keep their names and doc comments, bodies become `return webfetch.URLHash(srcURL)`.
- `imgproxy.go`: replace the two consts with
  ```go
  // Reader's names for webfetch's retry rule, kept so call sites and
  // export_test.go read as before.
  const (
  	maxImageFetchAttempts = webfetch.MaxImageAttempts
  	imageRetryBackoff     = webfetch.ImageRetryBackoff
  )
  ```
  the give-up condition in `image()` becomes
  `if webfetch.GivenUp(img.ErrorCount, img.FetchedAt, a.store.now()) {`, and
  `FeedIcon.GivenUp` in store.go becomes
  `return webfetch.GivenUp(i.ErrorCount, i.FetchedAt, now)`.
- `fetchImage` and `fetchFeedIcon`: replace the `Get` + sniff block with
  `ct, body, err := a.client.GetImage(r.Context(), img.SrcURL, webfetch.MaxImageBytes)`
  (and `webfetch.MaxFaviconBytes` for favicons). Keep the semaphore and the
  store calls exactly as they are.
- Reader tests: same mechanical substitutions only.

- [ ] **Step 8: Run the full check**

Run the Global Constraints full check.
Expected: all green; `git diff --stat -- internal/apps/reader/*_test.go` shows only qualifier/import edits.

- [ ] **Step 9: Commit**

```bash
git add -A internal/platform/webfetch internal/apps/reader
git commit -m "refactor(platform): move Reader's outbound HTTP client into webfetch"
```

---

### Task 2: `internal/platform/article`

**Files:**
- Create: `internal/platform/article/sanitize.go` (moved from `internal/apps/reader/sanitize.go`)
- Create: `internal/platform/article/images.go` (pure parts of `internal/apps/reader/images.go`)
- Create: `internal/platform/article/extract.go` (moved from `internal/apps/reader/extract.go`)
- Create: `internal/platform/article/sanitize_test.go`, `images_test.go`, `extract_test.go`
- Modify: `internal/apps/reader/images.go` (keeps `ImagePathPrefix`, `ImageHash`, wrapper `SanitizeArticleHTML`)
- Create: `internal/apps/reader/extract.go` again as a wrapper (after `git mv`)
- Modify: `internal/apps/reader/parse.go`, `handlers.go`, `view.go`, others the compiler flags
- Modify: `internal/arch/arch_test.go` (`TestReadabilityIsContained`)

**Interfaces:**
- Consumes: `webfetch.ResolveHTTPURL`, `webfetch.URLHash` (Task 1).
- Produces:
```go
package article
var ErrNotExtractable error
type ImageSrc func(hash string) string
type Extracted struct { HTML, Title string; Images map[string]string; TextLength int }
func Extract(body []byte, pageURL string, src ImageSrc) (Extracted, error)
func SanitizeHTML(raw string) string
func SanitizeWithImages(raw, baseURL string, src ImageSrc) (string, map[string]string)
```
Reader keeps `ExtractArticle(body []byte, pageURL string) (article.Extracted, error)` and `SanitizeArticleHTML(raw, baseURL string) (string, map[string]string)`.

- [ ] **Step 1: Move the files**

```bash
mkdir -p internal/platform/article
git mv internal/apps/reader/sanitize.go internal/platform/article/sanitize.go
git mv internal/apps/reader/sanitize_test.go internal/platform/article/sanitize_test.go
git mv internal/apps/reader/extract.go internal/platform/article/extract.go
cp internal/apps/reader/images.go internal/platform/article/images.go
```

- [ ] **Step 2: Write the failing tests**

`internal/platform/article/images_test.go` — move every test from
`internal/apps/reader/images_test.go` **except** `TestSanitizeArticleHTMLProxiesImages`
and `TestImageHashIsStableAndURLSafe` (they pin Reader's `/reader/img/` path
and `ImageHash`, so they stay in Reader). In the moved tests replace
`reader.SanitizeArticleHTML(raw, base)` with
`article.SanitizeWithImages(raw, base, testSrc)` and any `"/reader/img/"`
literal with `"/test/img/"`, using:
```go
package article_test

import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/article"
)

func testSrc(hash string) string { return "/test/img/" + hash }

func TestSanitizeWithImagesUsesTheCallersSrc(t *testing.T) {
	got, images := article.SanitizeWithImages(
		`<p>Text</p><img src="https://cdn.example/a.png" alt="A">`,
		"https://example.com/post", testSrc)
	if strings.Contains(got, "cdn.example") {
		t.Errorf("publisher host survived rewriting:\n%s", got)
	}
	if !strings.Contains(got, `src="/test/img/`) {
		t.Errorf("image was not rewritten with the caller's src:\n%s", got)
	}
	if len(images) != 1 {
		t.Errorf("got %d images, want 1", len(images))
	}
}
```

`internal/platform/article/extract_test.go` — move
`internal/apps/reader/extract_test.go` with `reader.ExtractArticle(body, u)` →
`article.Extract(body, u, testSrc)`, `reader.ErrNotExtractable` →
`article.ErrNotExtractable`, and `"/reader/img/"` → `"/test/img/"`. Leave a
copy of `TestExtractArticleProxiesImages` in Reader's `extract_test.go`
(unchanged) so Reader's wrapper is still pinned to `/reader/img/`; delete the
other tests from Reader's copy.

`sanitize_test.go`: `package article_test`, `reader.SanitizeHTML` → `article.SanitizeHTML`. `TestSanitizeHTMLStillStripsImages` (from images_test.go) moves here too.

- [ ] **Step 3: Run them to confirm they fail**

Run: `go test ./internal/platform/article/...`
Expected: compile failure — package still says `package reader`, `Extract`, `SanitizeWithImages`, `ImageSrc` undefined.

- [ ] **Step 4: Make the package**

`sanitize.go`: `package article`; add the package doc comment:
```go
// Package article turns a publisher's web page into HTML that is safe to
// show inside the suite: readability extraction, an allowlist sanitiser that
// fails closed, and image rewriting so no remote image ever reaches a
// browser directly. It never fetches — callers do that through webfetch.
package article
```
`policyWithImages`' comment: "used only by SanitizeWithImages".

`images.go` (the copy): `package article`; delete `ImagePathPrefix` and
`ImageHash`; imports gain `webfetch`; then:
```go
// ImageSrc builds the src an image is served from, given its URLHash. Each
// app passes its own, so Reader's images stay under /reader/img/ and
// Later's under /later/img/.
type ImageSrc func(hash string) string

// SanitizeWithImages sanitizes publisher HTML and rewrites every image to
// src(hash), returning the HTML and a hash→absolute-source map for the
// caller to persist.
//
// [keep the "fails closed" paragraph from SanitizeArticleHTML]
func SanitizeWithImages(raw, baseURL string, src ImageSrc) (string, map[string]string) {
	// body of SanitizeArticleHTML, with rewriteImages(clean, baseURL, src)
}
```
Thread `src ImageSrc` through `rewriteImages` and `rewriteOneImage`;
in `rewriteOneImage` use `hash := webfetch.URLHash(abs)` and
`html.Attribute{Key: "src", Val: src(hash)}`. Replace
`resolveAbsoluteHTTPURL` with `webfetch.ResolveHTTPURL`.

`extract.go`: `package article`; `ErrNotExtractable = errors.New("article: no article found in page")`;
other error prefixes `"reader: "` → `"article: "`; signature
`func Extract(body []byte, pageURL string, src ImageSrc) (Extracted, error)`;
it calls `SanitizeWithImages(article.Content, pageURL, src)` — rename the
readability result variable from `article` to `page` so it doesn't shadow the
package name. Reword the containment comment: "It is the only place in this
module that imports go-readability…" stays; "means R4 can be removed…" →
"means extraction can be removed or replaced without touching its callers."
Reword `Extracted.Title`'s comment to be caller-neutral: "Title is what
readability found on the page. Callers decide whether to use it; Reader
deliberately keeps the feed's own title."

- [ ] **Step 5: Reader wrappers**

`internal/apps/reader/images.go` shrinks to:
```go
package reader

import (
	"github.com/iliafrenkel/on-suite/internal/platform/article"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// ImagePathPrefix is where the proxy is mounted, as it appears in rewritten
// HTML. It is a constant rather than built from ID so that grepping for the
// literal finds both the route and the rewriter.
const ImagePathPrefix = "/reader/img/"

// ImageHash identifies an image by its source URL. [keep existing comment]
func ImageHash(srcURL string) string { return webfetch.URLHash(srcURL) }

func readerImageSrc(hash string) string { return ImagePathPrefix + hash }

// SanitizeArticleHTML is article.SanitizeWithImages bound to Reader's proxy.
func SanitizeArticleHTML(raw, baseURL string) (string, map[string]string) {
	return article.SanitizeWithImages(raw, baseURL, readerImageSrc)
}
```
New `internal/apps/reader/extract.go`:
```go
package reader

import "github.com/iliafrenkel/on-suite/internal/platform/article"

// ExtractArticle is article.Extract bound to Reader's image proxy.
func ExtractArticle(body []byte, pageURL string) (article.Extracted, error) {
	return article.Extract(body, pageURL, readerImageSrc)
}
```
Elsewhere in Reader: `SanitizeHTML(` → `article.SanitizeHTML(`,
`ErrNotExtractable` → `article.ErrNotExtractable`, `Extracted` →
`article.Extracted`.

- [ ] **Step 6: Point the containment test at the new file**

`internal/arch/arch_test.go`, `TestReadabilityIsContained`:
```go
	want := []string{"internal/platform/article/extract.go"}
```
and its comment: "…stays behind one file — now the shared article package,
so ON Reader and ON Later use one copy. A second importer makes it
load-bearing…"

- [ ] **Step 7: Run the full check**

Expected: all green.

- [ ] **Step 8: Commit**

```bash
git add -A internal/platform/article internal/apps/reader internal/arch
git commit -m "refactor(platform): move extraction and sanitising into article"
```

---

### Task 3: `internal/platform/favicon`

**Files:**
- Create: `internal/platform/favicon/favicon.go` (moved from `internal/apps/reader/favicon.go`)
- Create: `internal/platform/favicon/favicon_test.go` (moved from `internal/apps/reader/favicon_test.go`)
- Modify: `internal/apps/reader/poll.go`, `handlers.go`, `discover.go` (wherever `DiscoverFavicon` is called)

**Interfaces:**
- Consumes: `webfetch.ResolveHTTPURL`.
- Produces: `func favicon.Discover(pageHTML []byte, siteURL string) string`.

- [ ] **Step 1: Move**

```bash
mkdir -p internal/platform/favicon
git mv internal/apps/reader/favicon.go internal/platform/favicon/favicon.go
git mv internal/apps/reader/favicon_test.go internal/platform/favicon/favicon_test.go
```

- [ ] **Step 2: Update the test first**

`favicon_test.go`: `package favicon_test`, import
`github.com/iliafrenkel/on-suite/internal/platform/favicon`,
`reader.DiscoverFavicon(` → `favicon.Discover(`. Test names may drop the
redundant word (`TestDiscoverFaviconFindsAnIconLink` → `TestDiscoverFindsAnIconLink`); assertions unchanged.

- [ ] **Step 3: Confirm it fails**

Run: `go test ./internal/platform/favicon/...`
Expected: compile failure — `favicon.Discover` undefined.

- [ ] **Step 4: Make the package**

`favicon.go`: add
```go
// Package favicon finds a site's favicon URL. It never fetches: callers pass
// page HTML they already have (or none) and fetch the result through
// webfetch, storing the bytes and failures themselves.
package favicon
```
rename `DiscoverFavicon` → `Discover`, `resolveAbsoluteHTTPURL` →
`webfetch.ResolveHTTPURL`; reword the doc comment's "a poll cycle or an
add-feed request" to "a caller that already has a page's bytes in memory";
`faviconLinkInPage`'s reference to `FeedsInPage (discover.go)` becomes "walks
the parsed page looking for…".

- [ ] **Step 5: Switch Reader**

`grep -rn "DiscoverFavicon" internal/apps/reader` → replace with
`favicon.Discover` and add the import. Doc comments in poll.go that say
"DiscoverFavicon" become "favicon.Discover".

- [ ] **Step 6: Run the full check**

Expected: all green.

- [ ] **Step 7: Commit**

```bash
git add -A internal/platform/favicon internal/apps/reader
git commit -m "refactor(platform): move favicon discovery into favicon"
```

---

### Task 4: Pin the layering and document the packages

**Files:**
- Modify: `internal/arch/arch_test.go` (`TestLayering`, `TestScanSeesTheRealTree`)
- Modify: `AGENTS.md`, `PATTERNS.md`, `docs/developers/repository-layout.md`, `docs/developers/architecture.md`

- [ ] **Step 1: Write the failing layering rules**

In `TestLayering`'s forbidden map add (use the same slice style as the
existing `jobs` entry):
```go
		// The shared web-content packages are leaves: they know HTTP and
		// HTML, nothing about users, pages, storage or apps.
		"internal/platform/webfetch": {
			"internal/platform/web", "internal/platform/app", "internal/platform/render",
			"internal/platform/auth", "internal/platform/db", "internal/platform/config",
			"internal/platform/jobs", "internal/platform/article", "internal/platform/favicon",
		},
		"internal/platform/article": {
			"internal/platform/web", "internal/platform/app", "internal/platform/render",
			"internal/platform/auth", "internal/platform/db", "internal/platform/config",
			"internal/platform/jobs", "internal/platform/favicon",
		},
		"internal/platform/favicon": {
			"internal/platform/web", "internal/platform/app", "internal/platform/render",
			"internal/platform/auth", "internal/platform/db", "internal/platform/config",
			"internal/platform/jobs", "internal/platform/article",
		},
```
Add `"internal/platform/webfetch"`, `"internal/platform/article"`,
`"internal/platform/favicon"` to `TestScanSeesTheRealTree`'s package list.

- [ ] **Step 2: Run the arch tests**

Run: `go test ./internal/arch/... -v -run 'TestLayering|TestScanSeesTheRealTree|TestReadabilityIsContained|TestPlatformDoesNotImportApps'`
Expected: PASS (Tasks 1–3 already respect these; this step pins them). To
prove the rule bites, temporarily add `_ "github.com/iliafrenkel/on-suite/internal/platform/db"`
to `webfetch/url.go`, see `TestLayering` fail, then remove it.

- [ ] **Step 3: Docs**

- `AGENTS.md` → Architecture: after the "Several platform packages exist only
  for operations" paragraph, add:
  > **Three platform packages are shared web-content plumbing.**
  > [internal/platform/webfetch](internal/platform/webfetch/webfetch.go) is
  > the only way an app should fetch publisher-controlled URLs (SSRF guard at
  > dial time, redirect and size caps, image sniffing, the retry rule);
  > [internal/platform/article](internal/platform/article/extract.go) is
  > readability extraction plus the sanitiser and image rewriting;
  > [internal/platform/favicon](internal/platform/favicon/favicon.go) finds a
  > site's icon URL. They are leaves (pinned by `TestLayering`) and a
  > deliberate exception to cross-app mirroring: this code is large and
  > security-critical, so two drifting copies would be worse than one shared
  > package. ON Flash still has its own mirrored fetch client.
  
  And in Constraints, the go-readability note (if any) now names
  `internal/platform/article`.
- `PATTERNS.md`: add an entry —
  > - **Fetching publisher-controlled URLs** — reach for
  >   `internal/platform/webfetch` (and `article` / `favicon` on top of it)
  >   rather than `net/http` or a mirrored copy: one SSRF guard, one set of
  >   caps. Canonical: `internal/apps/reader/client.go`'s `NewClient`.
  
  and append to the "Cross-app mirroring" entry: "Exception: outbound
  fetching and HTML sanitising, which live in `internal/platform/webfetch`
  and `internal/platform/article` because they are security-critical."
- `docs/developers/repository-layout.md` and `docs/developers/architecture.md`:
  list the three packages with one line each, wherever the other platform
  packages are listed.

- [ ] **Step 4: Run the full check**

Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add internal/arch AGENTS.md PATTERNS.md docs/developers
git commit -m "docs(platform): pin and document webfetch, article and favicon"
```

---

## After the tasks

- Open a PR titled `refactor(platform): move Reader's fetch, extraction and favicon code into the platform (#504)`, body says "Closes #504" and links the spec.
- Smoke test before review: run the server locally, add a feed, open an
  article with images, "Load full article", and check the sidebar favicons —
  all should look exactly as they do on `main`.
- File a follow-up issue: `flash: use internal/platform/webfetch instead of the mirrored client in media_fetch.go` (chore, priority: low, effort: small).
