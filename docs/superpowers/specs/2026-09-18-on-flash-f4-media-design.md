# ON Flash F4 — media (design spec)

F4 lets a card carry one image and/or one audio clip, attached two ways: a
URL in the import payload (fetched and cached, never hotlinked), or a file
uploaded on the card edit screen afterward, for polishing. This mirrors ON
Reader's own image-caching pattern (R3) closely enough to reuse its proven
shape, adapted for Flash's per-card (not per-article-HTML) attachment model.

## Scope

- One image and one audio clip per card, at most — not a gallery, not
  multiple attachments per kind.
- URL-based attachment happens **only** at import time, via the JSON
  schema's `image`/`audio` fields (already accepted-and-discarded by F3)
  and new Markdown `Image:`/`Audio:` keys. There is no "attach by URL"
  control on the card edit screen.
- Manual attachment **only** happens via file upload on the card edit
  screen, for polishing an already-imported (or hand-created) card. There
  is no upload path at import time — import is text-paste only, unchanged
  from F3.
- Import-time URL media is fetched **lazily**, on first view, the same way
  Reader fetches article images: `ImportDeck`'s transaction only records
  the URL and its hash, doing no network I/O. The actual fetch happens the
  first time a browser requests the serving route.
- Size caps: 5MB per image, 10MB per audio clip.
- No orphan-cleanup job in F4 — a `flash_media` row could outlive every
  card that referenced it (after a delete or a replace). Added later: see
  "Orphan cleanup" below (#302).

## Architecture

Two migrations:

- `flash_media` — a cache table, structurally close to Reader's
  `reader_images`: `hash TEXT PRIMARY KEY, kind TEXT NOT NULL, content_type
  TEXT, bytes BLOB, source_url TEXT, fetched_at TEXT, last_error TEXT,
  error_count INTEGER NOT NULL DEFAULT 0`. `kind` is `"image"` or
  `"audio"`. `bytes IS NULL` means "known about, not yet fetched" (the
  lazy-fetch case); a manually-uploaded row has `bytes` populated
  immediately and `source_url` left `NULL`.
- Two nullable columns added to `flash_cards`: `image_hash`, `audio_hash`,
  each `REFERENCES flash_media (hash)`.

`hash` is a SHA-256 hex digest of either the source URL (URL-attached
media) or the file's own bytes (uploaded media) — the same value serves as
primary key, dedup key, and the identifier in the serving route's URL.

New files, following Reader's own split into small, focused files rather
than one large one (apps never import each other, so Flash needs its own
copy of Reader's guard rails, not a shared import):

- **`media.go`** — pure helpers: hashing (URL or bytes), content-type
  prefix validation per kind (`image/*`, `audio/*`), no DB or network
  access.
- **`media_fetch.go`** — the SSRF-guarded outbound HTTP client and the
  lazy-fetch-then-cache logic (fetch a `flash_media` row's `source_url` if
  `bytes` is still `NULL`).
- **`media_store.go`** — `flash_media`'s CRUD (`MediaByHash`,
  `SaveMediaBytes`, `SaveMediaFailure`, and the `INSERT OR IGNORE`
  metadata-only insert `ImportDeck` uses).
- **`handlers_media.go`** — `GET /flash/media/{hash}` (serving + lazy
  fetch) and `POST /flash/{deckID}/cards/{cardID}/media` (upload/remove).

## Import-time flow

`ParseImport` reads the JSON schema's `image`/`audio` fields (already
present in the schema since F3, previously parsed-and-discarded) into
`parsedCard.ImageURL`/`AudioURL`, and gains the same two keys for
Markdown: `Image:` and `Audio:` lines inside a `## Card` block, validated
the same way `Tags:` already is. A present value is validated only for
shape — it must parse as an absolute `http`/`https` URL — **not fetched**.
`ParseImport` remains a pure, network-free function, unchanged from F3 in
that respect.

`ImportDeck`'s transaction gains one step per card: if `ImageURL` (or
`AudioURL`) is set, compute its SHA-256 and `INSERT OR IGNORE` a
`flash_media` row with `source_url` set, `kind` set accordingly, and
`bytes`/`content_type` left `NULL` — no network call, just a hash of a
string and a metadata row — then set the card's `image_hash`/`audio_hash`
to that hash. This stays cheap enough to run inside the existing
single-connection transaction, unlike an actual fetch, which must not run
there (flash's SQLite handle allows only one connection at a time; a slow
outbound network call inside that transaction would block every other
request in the app for its duration).

An invalid or unreachable import-time URL never fails the import: only the
URL and its hash are recorded, so a broken link surfaces later as a 404 on
the serving route, not as a rejected import.

## Serving & lazy fetch

`GET /flash/media/{hash}` mirrors Reader's `GET /img/{hash}`:

1. Validate `hash` is 64 lowercase-hex characters before any DB lookup.
2. Look up the `flash_media` row, **but only if one of the viewer's own
   cards uses it** as its image or its sound (`MediaForUser`: `EXISTS
   (SELECT 1 FROM flash_cards WHERE user_id = ? AND (image_hash = hash OR
   audio_hash = hash))`). Otherwise → 404, the same response as a hash that
   doesn't exist, so the route never confirms what another account has
   attached. The table is a cache shared across accounts, and it holds
   files people uploaded from their own disks, not just public images.
   Adopting a shared deck copies its hashes into the recipient's own
   cards, so they can see its media from then on. A pending gift's preview
   draws no media. (#302.4)
3. If `bytes` is already populated, serve it directly: `Cache-Control:
   private, max-age=86400`, `ETag` = the hash, `X-Content-Type-Options:
   nosniff`, conditional `304` on a matching `If-None-Match`.
4. If `bytes` is `NULL` and `source_url` is set, fetch it now through
   Flash's own SSRF-guarded client:
   - Scheme allowlist (`http`/`https` only), re-checked on every redirect
     hop; redirect cap of 5.
   - Private/loopback/link-local/multicast/unspecified address block,
     checked against the *resolved* IP at dial time (defeats DNS
     rebinding), not just the hostname.
   - Timeouts: 10s dial, 10s TLS handshake, 15s response-header, 30s
     overall.
   - Size cap applied to the decoded stream: 5MB for `kind = "image"`,
     10MB for `kind = "audio"`.
   - Content-type sniffed via `http.DetectContentType` (never trusted from
     the response header) and required to start with `image/` for an
     image row or `audio/` for an audio row.
   - On success: store `bytes`/`content_type`/`fetched_at`, then serve.
   - On failure: record `last_error`/`error_count` (giving up permanently
     after 3 failures, with a 1-hour backoff between attempts — same
     policy as Reader), return 404. A context-canceled request (browser
     navigated away) does not count as a failure.
5. If `bytes` is `NULL` and `source_url` is also `NULL`, that is a
   data-integrity impossibility given how rows are created — treat it as
   404 rather than a 500, since there's nothing to do about it either way.

A failed or not-yet-fetched image/audio simply renders as the browser's
native broken-media state in the review/card templates — no special
handling needed there.

## Manual upload

The card edit screen (`templates/cards.html`) gets two file inputs (image,
audio) and a "remove" action per kind, alongside the card's existing
fields. `POST /flash/{deckID}/cards/{cardID}/media` accepts a multipart
form with optional `image` and/or `audio` file parts and an optional
`remove_image`/`remove_audio` flag per kind:

- Each uploaded file is read through `http.MaxBytesReader` (5MB image /
  10MB audio) — an oversized upload fails synchronously with a 400 and an
  error message on the form, since this is a direct user action with
  feedback available immediately (unlike the fetch path's silent 404).
- Content-type is sniffed the same way as a fetched URL and must match the
  field's kind.
- The file's bytes are hashed (SHA-256) and stored as a `flash_media` row
  with `bytes` populated immediately, `content_type` set from the sniff,
  and `source_url` left `NULL` — it never needs a lazy fetch.
- The card's `image_hash`/`audio_hash` is then set to that hash, in the
  **same transaction** as the insert (`AttachCardUpload`), or cleared to
  `NULL` on a remove request. The single transaction is what lets the
  orphan purge run without a grace period (below).

## Orphan cleanup

A daily job, **purge orphan media**, calls `PurgeOrphanMedia`: one
`DELETE FROM flash_media WHERE NOT EXISTS (…)` over rows that no card's
`image_hash` or `audio_hash` names, whoever owns the card. It mirrors
Reader's `PurgeOrphanImages`. There is no refcount, no admin button, and
the admin page's job list shows when it last ran. Migration
`0011_card_media_indexes` adds partial indexes on both columns. The
purge, the serving route's ownership check and SQLite's own foreign-key
check on each deleted row all use them. (#302.5)

- **No grace period, because nothing is stored unattached.** Every path
  that creates a row attaches it in the same transaction: card-form uploads
  (`AttachCardUpload`) and import-time URLs (`ImportDeck`). Adoption
  creates no rows. It copies hashes from source cards it reads inside its
  own transaction, and those cards keep the rows in use. SQLite runs one
  write transaction at a time, so the single-statement purge lands wholly
  before an attach, where the attach's `INSERT OR IGNORE` re-creates
  anything it deleted, or wholly after one. A timestamp-based grace period
  would not have worked anyway. URL rows have `fetched_at` NULL until first
  viewed, and `INSERT OR IGNORE` leaves an old orphan's timestamp alone
  when the same file comes back.
- A row shared by reference with an adopted copy lives as long as any card
  uses it.
- SQLite does not shrink the file on `DELETE`. Freed pages go on the
  freelist and are reused by later writes. Like Reader, the job runs no
  `VACUUM`. Snapshots are compact regardless, because they use `VACUUM
  INTO`.
- A browser that already has a file cached (`private, max-age=86400`) may
  keep showing it for up to a day after it's purged or stops being the
  viewer's.

## Testing

- `media_test.go` — pure hash/validation helpers: URL-hash vs
  content-hash, content-type-prefix checks per kind.
- `media_fetch_test.go` — the SSRF guard and fetch/cache logic against a
  local `httptest.Server`: private-IP rejection, redirect-cap, size-cap,
  content-type mismatch, retry/backoff bookkeeping. Mirrors Reader's own
  fetch tests in shape.
- `media_store_test.go` — `flash_media` CRUD against a real SQLite
  fixture.
- `handlers_media_test.go` — `GET /flash/media/{hash}` (cache hit,
  lazy-fetch-then-serve, 404 on fetch failure, conditional 304) and the
  upload endpoint (happy path, oversized/wrong-content-type rejection,
  CSRF/sign-in required, remove) via `apptest.Server`.
- Extend F3's existing import tests: a JSON/Markdown payload carrying
  `image`/`Image:` produces a card with a populated `image_hash` pointing
  at a `flash_media` row whose `bytes` is still `NULL` (not fetched by
  import itself).

## Open items deferred to later

- ~~Orphan-media cleanup~~: done, see "Orphan cleanup" (#302).
- Attaching media by URL after import (only upload is supported
  post-import in F4).
- Multiple images/audio clips per card, or per-kind galleries.
