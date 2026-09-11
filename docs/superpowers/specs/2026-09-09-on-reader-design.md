# ON Reader design

Status: approved 2026-09-09. Supersedes nothing; ON Reader was a reserved name
with no code.

ON Reader is an RSS/Atom reader for the ON Suite household: subscriptions
organised into folders one level deep, a three-pane interface, read and starred
state per user, and on-demand full-article extraction. It is the third app in
the suite and the first to make outbound HTTP requests or run background work,
which is what the platform's job scheduler was built for.

The suite-wide rationale this design assumes lives in
[2026-08-18-on-suite-platform-design.md](2026-08-18-on-suite-platform-design.md).

## 1. Decisions

| Decision | Choice |
|---|---|
| Reading model | Unread/read state plus a separate starred bucket that survives being read |
| Article body | Feed content by default, with an on-demand full-article fetch |
| Subscription ownership | Per user, with one shared global row per feed URL so a feed is polled once |
| Retention | Age-based purge of read items, starred and unread exempt |
| Remote images | Proxied and cached server-side |
| Scheduler access | A new optional `app.Scheduler` capability, discovered by type assertion |
| Client-side state | App-owned vanilla JS, following the `notes.js` precedent. No Alpine. |

### Dependencies

Three new per-app dependencies, all pure Go, all CGO-free:

| Module | New modules in the build graph | Why |
|---|---|---|
| `github.com/mmcdole/gofeed` | +2 (`gofeed`, `goxpp`) | Named for ON Reader in the platform spec. Real-world RSS/Atom is a swamp of namespaces, RDF, CDATA and inconsistent date formats. |
| `github.com/microcosm-cc/bluemonday` | +3 (`bluemonday`, `douceur`, `gorilla/css`) | A HTML sanitizer is the security boundary between hostile publisher HTML and a logged-in session. It is the single worst component to hand-roll. |
| `github.com/go-shiori/go-readability` | +5 (`go-readability`, `cascadia`, `dateparse`, `go-shiori/dom`, `chardet`) | Article extraction is pure heuristics that only get good through years of corpus tuning. |

`golang.org/x/net` and `golang.org/x/text` are already in the tree; x/net is
promoted from test-only to a production dependency by this work.

Two of readability's transitive modules — `araddon/dateparse` and
`gogs/chardet` — are effectively unmaintained. Accepted knowingly: they are
small, pure, and reached only from the R4 extraction path, which fails soft.
If either becomes a problem, R4 can be removed without touching R1–R3.

## 2. The one platform change

`app.Deps` has no route to the job registry, so Reader cannot schedule polling
without a platform addition. AGENTS.md warns that an app forcing a platform
change means the platform got something wrong; the platform spec also records
that the scheduler was "first needed by ON Reader". This is the planned
rendezvous, not a design failure.

A new optional capability, discovered by type assertion exactly as `Exporter`,
`Stater` and `Templates` already are:

```go
// Job is one unit of background work an app owns. It is a value type rather
// than a registration call so an app can declare its jobs without holding a
// reference to the scheduler.
type Job struct {
    Name        string
    Description string
    Every       time.Duration
    Run         func(context.Context) error
}

// Scheduler is implemented by apps that own background work. Like Exporter and
// Stater it is optional and discovered by type assertion, so an app with no
// background work does not stub out a method.
type Scheduler interface {
    Jobs(deps Deps) []Job
}
```

`jobs.Registry` exposes `Register(name, description, every, fn)` and has no
exported job type, so `app.Job` is declared in the `app` package and translated
at registration. That keeps `internal/platform/jobs` untouched — it stays the
leaf package that imports nothing else in the module.

Registration is a new `Registry.RegisterJobs(jr *jobs.Registry, deps Deps)`,
called from `serve` after mounting, rather than a new parameter on `MountAll`:
the existing mount signature stays as it is, and a command that builds the app
registry without a scheduler (`onsuite export`) simply never calls it. An app's
jobs then appear on the admin page beside the nightly backup and session sweep
with no further work.

**Rejected: adding `Jobs *jobs.Registry` to `Deps`.** Fewer moving parts, but
it hands every app a scheduler it does not need and turns job registration into
a side effect buried inside `Mount` rather than a declared capability.

**Rejected: Reader spawning its own goroutine.** No platform change, but no
admin visibility, no shared shutdown, and a second thing in the codebase that
means "background job".

No arch test change is needed. `TestLayering` in
`internal/arch/arch_test.go` already lists `internal/platform/jobs` with
`internal/platform/app` among its forbidden imports, so the direction that
matters — the scheduler must never learn what an app is — is enforced today.
`app` importing `jobs` is downward and already allowed.

## 3. Package layout

```
internal/apps/reader/
  app.go          Meta, Migrations, Mount, Jobs, Stats, Export
  store.go        feeds, subscriptions, folders, items, item state
  fetch.go        the hardened HTTP client: SSRF guard, caps, conditional GET
  poll.go         the polling job: due selection, worker pool, backoff
  parse.go        gofeed output normalised to an Item; GUID and dedup rules
  discover.go     site URL to feed URL
  extract.go      full-article fetch and readability extraction
  sanitize.go     bluemonday policy and img rewriting
  imgproxy.go     the image proxy handler and its cache
  opml.go         OPML import and export
  search.go       FTS5
  stats.go        Stater card and the reading-stats page
  handlers.go     HTTP and HTMX handlers
  view.go         view-model projection, per PATTERNS.md
```

The deliberate seam: `fetch.go` never parses, `parse.go` never touches the
network, `sanitize.go` never touches the database. The polling job is glue. That
is what makes the nasty cases — a feed that returns 10 GB, a redirect loop, a
publisher serving `text/html` from a feed URL — testable without a network.

## 4. Data model

Eight tables, migrations namespaced `reader:`.

### Global

**`reader_feeds`** — one row per feed URL, shared by every subscriber.

`id`, `url` UNIQUE, `resolved_url`, `title`, `site_url`, `etag`,
`last_modified`, `last_fetch_at`, `last_status`, `last_error`, `error_count`,
`next_fetch_at`, `fetch_interval` NULL (falls back to the default when unset).

**`reader_items`** — one row per article.

`id`, `feed_id`, `guid`, `url`, `title`, `author`, `published_at`,
`fetched_at`, `summary_html`, `content_html`, `full_html` NULL,
`full_fetched_at` NULL. `UNIQUE(feed_id, guid)`.

**`reader_items_fts`** — FTS5 over title and body text, kept in sync by trigger.

**`reader_images`** — the proxy cache, content-addressed. `url_hash`
PRIMARY KEY (128 bits of SHA-256 over the source URL), `src_url`,
`content_type`, `bytes` NULL until first requested, `fetched_at`,
`last_error`, `error_count`. An image reused across articles is stored once.

**`reader_item_images`** — which items reference which images. `item_id`,
`url_hash`, `PRIMARY KEY(item_id, url_hash)`, both columns
`ON DELETE CASCADE`. This is what lets retention free an image once the last
article referencing it is gone.

### Per user

**`reader_folders`** — `id`, `user_id`, `name`, `position`. There is no parent
column. One-level-deep nesting is enforced by the schema rather than by
validation.

**`reader_subs`** — `id`, `user_id`, `feed_id`, `folder_id` NULL, `title`
override NULL, `position`, `added_at`. `UNIQUE(user_id, feed_id)`.

**`reader_item_state`** — `user_id`, `item_id`, `read_at` NULL, `starred_at`
NULL. `PRIMARY KEY(user_id, item_id)`.

### Three decisions worth stating explicitly

**Absence means unread.** No state row exists until an item is read or starred,
so subscribing to a firehose does not cost a row per user per item.

An unread count is a single `count(*)` over items, with a `NOT EXISTS`
correlated subquery against `reader_item_state` — indexed by that table's
`(user_id, item_id)` primary key.

It is deliberately *not* `count(items) - count(read states)`, which an earlier
draft of this document specified. Subtraction is only correct if every
read-state row falls inside the counted range, and the `added_at` cutoff below
means it does not: a user can hold a read-state row for an item published
before they subscribed. Subtracting it makes the count too low, which hides a
genuinely unread article instead of failing visibly.

**`reader_subs.added_at` is the unread cutoff.** When a second household member
subscribes to a feed that already has 60 days of history, items published before
their `added_at` are treated as read. A new subscriber gets what arrives next,
not a backlog they never asked for.

**Dedup order is GUID, then link, then a hash of title plus `published_at`.**
Publishers edit items in place, so a matching GUID updates the row rather than
inserting a duplicate. Updating an item deliberately does not clear read state.

### Retention

A nightly job deletes items older than the retention age unless any user has
starred them or any user still has them unread. The age is a package constant
(60 days), not a flag: the platform's config is deliberately a small, closed set
of server settings, and adding per-app configuration is a separate design
question this app should not answer on its own. Removing the
last subscription to a feed orphans it; the feed row is deleted and items and
cached images cascade away.

## 5. Outbound HTTP and the threat model

Reader is the first app that makes the server fetch a URL a user typed. Three
call sites do it — feed polling, article extraction, image proxying — and all
three share one hardened client in `fetch.go`.

### SSRF

"Add feed: `http://169.254.169.254/latest/meta-data/`" turns Reader into a
request forwarder inside the network. The guard belongs in the dialer, not in
URL validation:

```go
DialContext: (&net.Dialer{
    Control: func(network, address string, c syscall.RawConn) error {
        return client.denyPrivate(address) // resolved IP:port, not a hostname
    },
}).DialContext
```

Checking the address actually being connected to, rather than the hostname,
closes DNS rebinding: a name that resolves public at validation time and private
at fetch time is still refused. `denyPrivate` rejects loopback, link-local
(including `169.254.169.254`), RFC1918, unique local addresses, and the
unspecified and multicast ranges. The scheme is restricted to http and https,
and the check runs on every redirect hop.

`denyPrivate` is a **field on the client, not a package-level function.**
`httptest` servers listen on `127.0.0.1`, which the default guard refuses by
construction, so tests inject a permissive variant. One test asserts the default
client refuses loopback, link-local and RFC1918. Getting this backwards means
either the tests cannot run or the guard is never exercised.

### The other guards

- Redirects capped at 5, re-checked each hop.
- Body size capped with `io.LimitReader`, applied *after* decompression: 5 MB
  for a feed, 2 MB for an article page, 5 MB for an image.
- Whole-request timeouts, not just dial timeouts.
- The image proxy serves only `image/*`, re-derives the content type by
  sniffing rather than trusting the response header, and sets
  `X-Content-Type-Options: nosniff`.
- `User-Agent` identifies `onsuite/<version>`, because publishers deserve to
  know who is polling them.

XML entity expansion — the billion-laughs class — needs no mitigation: Go's
`encoding/xml`, which gofeed sits on, does not expand entities beyond the
predefined set.

### The image proxy needs no signing secret

The platform has no server-side signing key, and this design does not introduce
one. Instead of signing arbitrary URLs, the proxy is **content-addressed**:
sanitization rewrites every `img src` to `/reader/img/<hash>`, where the hash
is 128 bits of SHA-256 over the absolute source URL, and records the
hash-to-URL mapping in `reader_images`.

The proxy therefore takes a hash, never a URL. The only way a hash resolves is
if a feed this server ingested contained exactly that image URL; there is no
input that makes it fetch anything else, and there is no key to rotate or leak.
Combined with the router's default-deny authentication it is not an open proxy
in any sense.

Content-addressing rather than an `(item_id, src_url)` key is what lets the
rewrite happen in `parse.go`, which never touches the database — an item's id
does not exist until `SaveItems` has run. It also means an image reused across
articles is stored once. A `reader_item_images` join table records which items
reference which images, so retention can free an image once the last article
using it is gone.

Bytes are fetched on first view, not at poll time: downloading every image of
every article would fetch a great deal nobody looks at.

With every image served from `/reader/img/<hash>`, the CSP for Reader's pages
tightens to `img-src 'self'`.

## 6. Polling

One `jobs.Job` on a 5-minute tick asks which feeds are due, rather than one
timer per feed.

- Each feed carries `next_fetch_at`. Default interval 30 minutes, per-feed
  override, with jitter so forty feeds do not fire simultaneously.
- Conditional GET using the stored `ETag` and `Last-Modified`. A `304` costs
  almost nothing and is the polite default.
- Failures back off exponentially to a ceiling of about 6 hours. `error_count`
  drives a visible "this feed is failing" affordance in the tree, because
  silent rot is the classic reader failure mode.
- A worker pool of 4 bounds concurrency, and the run honours the job's context
  so shutdown is not blocked by a hung publisher.

Manual "refresh now" reuses the identical path with the due check skipped. There
is one fetch code path, not two.

## 7. Interface

Three panes, reusing the split-view pattern from
[2026-09-03-paste-split-view-redesign-design.md](2026-09-03-paste-split-view-redesign-design.md)
rather than inventing a third layout language.

```
┌──────────────┬────────────────────┬──────────────────────────┐
│ Tree         │ Item list          │ Article                  │
│              │                    │                          │
│ All      124 │ ● Title of thing   │  Title of thing          │
│ Starred   18 │   Feed · 2h        │  Feed · Author · 2h      │
│              │                    │  ────────────────────    │
│ ▾ Tech    41 │ ● Another one      │  Body…                   │
│   Feed A  12 │   Feed · 5h        │                          │
│   Feed B  29 │                    │                          │
│ ▾ F1      83 │   Read one         │  [Open original] [Full]  │
│   Feed C  83 │   Feed · yesterday │  [★ Star]                │
└──────────────┴────────────────────┴──────────────────────────┘
```

Below roughly 900px the panes become a drill-down — tree, then list, then
article — with the shell breadcrumb syncing per
[2026-09-08-shell-crumb-oob-sync-design.md](2026-09-08-shell-crumb-oob-sync-design.md).

**Tree.** Folders one level deep with feeds beneath, plus two pseudo-nodes at
the top: All and Starred. Unread counts per node. Drag-to-move a feed between
folders reuses the ON Notes N10 drag implementation.

**List.** Reverse-chronological, with an Unread / Starred / All filter toggle.
The search box behaves as an inline filter, matching
[2026-09-07-on-notes-search-filter-design.md](2026-09-07-on-notes-search-filter-design.md).

**Article pane.** Sanitized feed content. `Open original` links out.
`Fetch full article` swaps in the readability-extracted body and caches it;
once cached, a toggle flips between the feed version and the full version,
because extraction sometimes does worse than the publisher's own summary.

**Marking read** happens when an article is opened, with an undo affordance. Not
on scroll, which is unpredictable; not manually only, which turns the reader
into a clerk.

**Keyboard navigation** lives in an app-owned `reader.js`, following the
`notes.js` precedent: `j`/`k` move, `o` opens, `m` toggles read, `s` stars, `r`
refreshes, `/` focuses search. Vanilla, external file, CSP-clean. No Alpine, so
this app does not force a client-state framework decision on the suite.

**Prefetch is part of the design, not an optimisation for later.** Every
interaction is an HTMX round-trip; a 200ms round-trip per `j` keypress feels
sluggish in a way Notes and Paste never exposed, because nobody holds `j` down
in an outliner. `reader.js` prefetches the adjacent item's article on selection
change.

## 8. Stats

Two of the three are nearly free, because the platform interfaces already exist.

**`Stater` card on the admin page** — feed count, subscription count, item
count, last poll outcome, number of feeds currently failing.

**`Exporter` participation** — folders, subscriptions and starred items appear
in `onsuite export`.

**The reading-stats page** — items per day over 30 and 90 days, volume per
feed, read ratio per feed, and unread backlog over time. Plus the two
diagnostics that actually change behaviour:

- *Quiet feeds*: nothing published in 60 days, probably dead.
- *Unread feeds*: high volume, low read ratio, probably worth unsubscribing.

A reader accumulates cruft. This is the page that says what to prune.

## 9. Testing

- **`fetch.go`** against `httptest`, with the injected permissive
  `denyPrivate`; one separate test asserts the *default* guard refuses
  loopback, link-local and RFC1918.
- **`parse.go`** against a fixture corpus of genuinely nasty real feeds: RSS
  2.0, Atom, RSS 1.0/RDF, CDATA-wrapped HTML, missing GUIDs, and the several
  date formats publishers use in practice.
- **`sanitize.go`** against an XSS corpus: `<script>`, `onerror=`,
  `javascript:` hrefs, `data:` URIs, SVG payloads, CSS `expression()`.
- **`store.go`** against a real SQLite file in a temp dir, per the existing
  store-test convention.
- **Handlers** via `internal/htmlassert`.
- **`internal/arch`** needs no new case; see section 2.

## 10. Phasing

Seven slices, each independently shippable and reviewable.

| | Phase | Contents |
|---|---|---|
| R1 | Walking skeleton | Platform `Scheduler` interface and arch test; schema; add and remove feed; folders; polling job; sanitizer; three-pane UI reading feed content |
| R2 | Read and star | State table, unread counts, filters, mark-all-read, retention job |
| R3 | Image proxy | src rewriting, cache, CSP tightened to `img-src 'self'` |
| R4 | Full article | Readability extraction, caching, feed-versus-full toggle |
| R5 | Onboarding | OPML import and export, feed discovery from a site URL |
| R6 | Find and fly | FTS5 search-as-filter, keyboard navigation, prefetch |
| R7 | Stats | Reading-stats page, `Stater`, `Exporter` |

R1 is deliberately the largest because it carries the platform change. After
that each phase is a couple of files.

Sanitization is in R1 rather than R3 because there is no acceptable intermediate
state in which the app renders publisher HTML unsanitized — not even on a
private LAN.

**R1 strips images entirely.** The sanitizer's allowlist excludes `img` until
R3 adds the proxy and the rewriting that feeds it. This matters: the alternative
intermediate state — passing remote `img` tags through for two phases — is
exactly the tracking-pixel leak the proxy exists to prevent, and it would ship
with a `img-src` CSP that R3 then has to walk back. Stripping first and adding
images with the proxy means the privacy posture only ever tightens.

## 11. Out of scope

- Nesting deeper than one folder level. The schema forbids it.
- Per-feed filter rules, such as auto-marking items read by title match.
- Podcast or enclosure handling.
- Sharing an article publicly, as ON Notes N9 does. Reader shows other people's
  content; republishing it is a different question than sharing your own notes.
- Any social or recommendation feature.
