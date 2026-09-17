# ON Flash F4 — Media Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a card carry one image and/or one audio clip, attached either via a URL in the import payload (fetched and cached lazily, never hotlinked) or via a file uploaded on the card edit screen afterward.

**Architecture:** A shared `flash_media` cache table (mirroring ON Reader's `reader_images`) stores both images and audio, keyed by a hash of either the source URL (import-time) or the file's own bytes (upload). A card references at most one image row and one audio row via nullable `image_hash`/`audio_hash` columns. Fetching a URL happens lazily, the first time `GET /flash/media/{hash}` is requested — never inside `ImportDeck`'s transaction, which only records the URL and a metadata row. Flash gets its own SSRF-guarded HTTP client (`MediaClient`), an independent copy of Reader's `Client`/`DenyPrivateAddr`, since apps never import each other.

**Tech Stack:** Go standard library only (`net/http`, `net/netip`, `crypto/sha256`) — no new dependencies.

## Global Constraints

- One image and one audio clip per card, at most.
- URL-based attachment happens **only** at import time (JSON's `image`/`audio` fields, Markdown's new `Image:`/`Audio:` keys). There is no "attach by URL" control on the card edit screen.
- Manual attachment happens **only** via file upload on the card edit screen. There is no upload path at import time.
- Import-time URL media is fetched **lazily** — `ImportDeck`'s transaction never does network I/O; it only hashes the URL and inserts a metadata-only row.
- Size caps: 5MB per image (`MaxImageFetchBytes`), 10MB per audio clip (`MaxAudioFetchBytes`).
- No orphan-cleanup job in F4.
- flash's SQLite handle has `SetMaxOpenConns(1)` (`internal/platform/db/db.go`): a function running inside a transaction must never call a method that opens its own transaction. `ensureMediaURL` (media store helper) is written against a `dbExecutor` interface (`*sql.DB` or `*sql.Tx`) so `ImportDeck` can call it against its own already-open `*sql.Tx`.
- **A DB-level constraint check does not exist on `flash_cards.card_type`, so an empty-string value does not violate `NOT NULL`** — found during this plan's own validation while designing a test for `SetCardMedia`'s ownership check. Any test that needs a real `flash_media` row to exist (for the `image_hash`/`audio_hash` foreign key to be satisfiable) must insert one first; a fabricated hash string alone will fail with `FOREIGN KEY constraint failed`, not the expected `ErrNotFound`.
- Reuses existing validation and conventions exactly: `ValidateCard`, `ValidateTagNames`, `ValidateDeck` (unchanged), `st.now()` for all timestamps (never `time.Now()` directly in handlers), `a.fail` for error mapping, the PRG pattern every other flash mutation already follows.

---

### Task 1: Media schema and `Card` struct extension

This is the most cross-cutting task: `flash_cards`' row shape is read by four different SELECT statements across three files (`card.go`, `tag.go`, `review.go`). All must be updated together or the column count/order mismatch will panic at scan time.

**Files:**
- Create: `internal/apps/flash/migrations/0006_media.sql`
- Modify: `internal/apps/flash/card.go`
- Modify: `internal/apps/flash/tag.go` (one SELECT statement)
- Modify: `internal/apps/flash/review.go` (two SELECT statements)
- Test: `internal/apps/flash/card_media_test.go`

**Interfaces:**
- Consumes: nothing new (works entirely with F1/F2/F3's existing `Card`, `Store`, `ErrInvalid`, `ErrNotFound`).
- Produces: `Card.ImageHash *string`, `Card.AudioHash *string`; `MediaKindImage = "image"`, `MediaKindAudio = "audio"` constants; `func (st *Store) SetCardMedia(ctx context.Context, userID, deckID, cardID int64, kind string, hash *string) error` — used by Task 5 (import) and Task 6 (upload handler).

- [ ] **Step 1: Write the migration**

Create `internal/apps/flash/migrations/0006_media.sql`:

```sql
-- Media (images/audio) attached to a card. Keyed by a hash of either the
-- source URL (import-time, fetched lazily on first view — see media_fetch.go)
-- or the uploaded bytes themselves (manual upload, bytes present immediately).
-- A shared table for both kinds, differentiated by `kind`, the same way a
-- card's own card_type differentiates basic from cloze.
CREATE TABLE flash_media (
    hash         TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,
    -- content_type is what sniffing decided, never what a server or upload
    -- claimed.
    content_type TEXT NOT NULL DEFAULT '',
    -- bytes is NULL until fetched (URL-attached) or is populated immediately
    -- at upload time (manually-attached). NULL is exactly "not yet fetched".
    bytes        BLOB,
    -- source_url is set only for URL-attached media; NULL for an upload,
    -- which never needs fetching.
    source_url   TEXT,
    fetched_at   TEXT,
    last_error   TEXT NOT NULL DEFAULT '',
    error_count  INTEGER NOT NULL DEFAULT 0
) STRICT;

ALTER TABLE flash_cards ADD COLUMN image_hash TEXT REFERENCES flash_media (hash);
ALTER TABLE flash_cards ADD COLUMN audio_hash TEXT REFERENCES flash_media (hash);
```

- [ ] **Step 2: Write the failing tests**

Create `internal/apps/flash/card_media_test.go`:

```go
package flash_test

import (
	"context"
	"errors"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestNewCardHasNoMedia(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.ImageHash != nil || c.AudioHash != nil {
		t.Errorf("new card has media: image=%v audio=%v", c.ImageHash, c.AudioHash)
	}
}

func TestSetCardMediaSetsAndClears(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}

	hash := "a1b2c3"
	if _, err := f.db.ExecContext(ctx, `INSERT INTO flash_media (hash, kind) VALUES (?, ?)`, hash, flash.MediaKindImage); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardMedia(ctx, f.alice.ID, deck.ID, c.ID, flash.MediaKindImage, &hash); err != nil {
		t.Fatalf("SetCardMedia: %v", err)
	}
	got, err := f.store.CardByID(ctx, f.alice.ID, deck.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ImageHash == nil || *got.ImageHash != hash {
		t.Errorf("ImageHash = %v, want %q", got.ImageHash, hash)
	}
	if got.AudioHash != nil {
		t.Errorf("AudioHash = %v, want nil (only image was set)", got.AudioHash)
	}

	if err := f.store.SetCardMedia(ctx, f.alice.ID, deck.ID, c.ID, flash.MediaKindImage, nil); err != nil {
		t.Fatalf("SetCardMedia (clear): %v", err)
	}
	cleared, err := f.store.CardByID(ctx, f.alice.ID, deck.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ImageHash != nil {
		t.Errorf("ImageHash = %v after clearing, want nil", cleared.ImageHash)
	}
}

func TestSetCardMediaRejectsUnknownKind(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}
	hash := "a1b2c3"
	err = f.store.SetCardMedia(ctx, f.alice.ID, deck.ID, c.ID, "video", &hash)
	if !errors.Is(err, flash.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestSetCardMediaOnSomeoneElsesCardIs404(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}
	hash := "a1b2c3"
	err = f.store.SetCardMedia(ctx, f.bob.ID, deck.ID, c.ID, flash.MediaKindImage, &hash)
	if !errors.Is(err, flash.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
```

`newFixture` (already in `deck_test.go`) exposes `f.db *sql.DB` directly — used above to seed a `flash_media` row without depending on Task 3's store methods, which don't exist yet.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/... -run 'TestNewCardHasNoMedia|TestSetCardMedia' -v`
Expected: FAIL to compile — `ImageHash`, `AudioHash`, `MediaKindImage`, `SetCardMedia` are undefined, and `flash_media`/`image_hash`/`audio_hash` don't exist in the schema yet.

- [ ] **Step 4: Modify `card.go`**

Change the `Card` struct:

```go
// Card is one flash card belonging to a deck.
type Card struct {
	ID        int64
	DeckID    int64
	UserID    int64
	CardType  string
	Front     string
	Back      string
	Notes     string
	CreatedAt time.Time

	// ImageHash and AudioHash name a row in flash_media, or nil if the card
	// has no image/audio attached. See media.go/media_store.go.
	ImageHash *string
	AudioHash *string
}
```

Change `CardByID` and `ListCards`' SELECT column lists (add `, image_hash, audio_hash`):

```go
// CardByID fetches one of userID's own cards, scoped to its deck.
func (st *Store) CardByID(ctx context.Context, userID, deckID, id int64) (Card, error) {
	return scanCard(st.db.QueryRowContext(ctx,
		`SELECT id, deck_id, user_id, card_type, front, back, notes, created_at, image_hash, audio_hash
		 FROM flash_cards WHERE id = ? AND deck_id = ? AND user_id = ?`, id, deckID, userID))
}

// ListCards returns userID's cards in one deck, newest first.
func (st *Store) ListCards(ctx context.Context, userID, deckID int64) ([]Card, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT id, deck_id, user_id, card_type, front, back, notes, created_at, image_hash, audio_hash
		 FROM flash_cards WHERE deck_id = ? AND user_id = ?
		 ORDER BY created_at DESC, id DESC`, deckID, userID)
```

Add, just before `DeleteCard`:

```go
// MediaKindImage and MediaKindAudio select which of a card's two media
// columns SetCardMedia writes.
const (
	MediaKindImage = "image"
	MediaKindAudio = "audio"
)

// SetCardMedia sets or clears one of userID's own card's media hashes. hash
// of nil clears the attachment (e.g. a "remove image" request). It does not
// validate that hash names a real flash_media row — callers (ImportDeck, the
// upload handler) create that row first.
func (st *Store) SetCardMedia(ctx context.Context, userID, deckID, cardID int64, kind string, hash *string) error {
	var column string
	switch kind {
	case MediaKindImage:
		column = "image_hash"
	case MediaKindAudio:
		column = "audio_hash"
	default:
		return fmt.Errorf("%w: %q is not a media kind I know", ErrInvalid, kind)
	}
	res, err := st.db.ExecContext(ctx,
		`UPDATE flash_cards SET `+column+` = ? WHERE id = ? AND deck_id = ? AND user_id = ?`,
		hash, cardID, deckID, userID)
	if err != nil {
		return fmt.Errorf("flash: set card media: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("flash: set card media: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
```

Change `scanCardRow`:

```go
func scanCardRow(row rowScanner) (Card, error) {
	var (
		c         Card
		createdAt string
		imageHash sql.NullString
		audioHash sql.NullString
	)
	err := row.Scan(&c.ID, &c.DeckID, &c.UserID, &c.CardType, &c.Front, &c.Back, &c.Notes, &createdAt,
		&imageHash, &audioHash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Card{}, sql.ErrNoRows // translated by scanCard
	case err != nil:
		return Card{}, fmt.Errorf("flash: scan card: %w", err)
	}
	if c.CreatedAt, err = parseTime(createdAt); err != nil {
		return Card{}, err
	}
	if imageHash.Valid {
		c.ImageHash = &imageHash.String
	}
	if audioHash.Valid {
		c.AudioHash = &audioHash.String
	}
	return c, nil
}
```

- [ ] **Step 5: Modify `tag.go`**

In `CardsByTag`, change the SELECT to add `, c.image_hash, c.audio_hash`:

```go
		`SELECT c.id, c.deck_id, c.user_id, c.card_type, c.front, c.back, c.notes, c.created_at, c.image_hash, c.audio_hash
		 FROM flash_cards c
		 JOIN flash_card_tags ct ON ct.card_id = c.id
		 JOIN flash_tags t ON t.id = ct.tag_id
		 WHERE c.user_id = ? AND t.user_id = ? AND t.name = ?
		 ORDER BY c.created_at DESC, c.id DESC`, userID, userID, name)
```

- [ ] **Step 6: Modify `review.go`**

There are two SELECT statements with the exact same card column list — `dueReviewCards` and `newQueueCards`. In both, change:

```go
		SELECT c.id, c.deck_id, c.user_id, c.card_type, c.front, c.back, c.notes, c.created_at
```

to:

```go
		SELECT c.id, c.deck_id, c.user_id, c.card_type, c.front, c.back, c.notes, c.created_at, c.image_hash, c.audio_hash
```

(Both occurrences are inside the query string literals in `dueReviewCards` and `newQueueCards` — replace both.)

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -run 'TestNewCardHasNoMedia|TestSetCardMedia' -v`
Expected: PASS (4 subtests).

Run the whole flash package to catch any column-count regression in `review.go`/`tag.go`'s other tests:

Run: `go test ./internal/apps/flash/...`
Expected: PASS — all of F1/F2/F3's existing tests still green (this is the check that the cross-cutting SELECT changes didn't break the review queue or tag filter).

- [ ] **Step 8: Commit**

```bash
git add internal/apps/flash/migrations/0006_media.sql internal/apps/flash/card.go \
        internal/apps/flash/tag.go internal/apps/flash/review.go internal/apps/flash/card_media_test.go
git commit -m "feat(flash): add flash_media schema and Card media hashes"
```

---

### Task 2: Pure media helpers

**Files:**
- Create: `internal/apps/flash/media.go`
- Test: `internal/apps/flash/media_test.go`

**Interfaces:**
- Consumes: `MediaKindImage`, `MediaKindAudio` (Task 1).
- Produces: `urlHash(rawURL string) string`, `contentHash(data []byte) string`, `validMediaHash(s string) bool`, `contentTypeMatchesKind(contentType, kind string) bool` — used by Task 3 (store), Task 4 (fetch), Task 6 (handlers).

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/flash/media_test.go`:

```go
package flash

import "testing"

func TestURLHashIsStableAndDistinct(t *testing.T) {
	a := urlHash("https://example.com/cat.jpg")
	b := urlHash("https://example.com/cat.jpg")
	c := urlHash("https://example.com/dog.jpg")
	if a != b {
		t.Errorf("urlHash is not stable: %q != %q", a, b)
	}
	if a == c {
		t.Errorf("urlHash collided for different URLs")
	}
	if !validMediaHash(a) {
		t.Errorf("urlHash produced an invalid-shaped hash: %q", a)
	}
}

func TestContentHashIsStableAndDistinct(t *testing.T) {
	a := contentHash([]byte("hello"))
	b := contentHash([]byte("hello"))
	c := contentHash([]byte("world"))
	if a != b {
		t.Errorf("contentHash is not stable: %q != %q", a, b)
	}
	if a == c {
		t.Errorf("contentHash collided for different content")
	}
	if !validMediaHash(a) {
		t.Errorf("contentHash produced an invalid-shaped hash: %q", a)
	}
}

func TestValidMediaHash(t *testing.T) {
	tests := []struct {
		name string
		hash string
		want bool
	}{
		{"real hash", urlHash("https://example.com/x"), true},
		{"too short", "abc", false},
		{"uppercase", "A" + urlHash("x")[1:], false},
		{"non-hex", "g" + urlHash("x")[1:], false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validMediaHash(tt.hash); got != tt.want {
				t.Errorf("validMediaHash(%q) = %v, want %v", tt.hash, got, tt.want)
			}
		})
	}
}

func TestContentTypeMatchesKind(t *testing.T) {
	tests := []struct {
		contentType string
		kind        string
		want        bool
	}{
		{"image/jpeg", MediaKindImage, true},
		{"image/png", MediaKindImage, true},
		{"audio/mpeg", MediaKindAudio, true},
		{"audio/wav", MediaKindAudio, true},
		{"audio/mpeg", MediaKindImage, false},
		{"image/jpeg", MediaKindAudio, false},
		{"text/html", MediaKindImage, false},
		{"image/jpeg", "video", false},
	}
	for _, tt := range tests {
		if got := contentTypeMatchesKind(tt.contentType, tt.kind); got != tt.want {
			t.Errorf("contentTypeMatchesKind(%q, %q) = %v, want %v", tt.contentType, tt.kind, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/... -run 'TestURLHash|TestContentHash|TestValidMediaHash|TestContentTypeMatchesKind' -v`
Expected: FAIL to compile — `urlHash`, `contentHash`, `validMediaHash`, `contentTypeMatchesKind` are undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/apps/flash/media.go`:

```go
// internal/apps/flash/media.go
package flash

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// urlHash identifies a URL-attached media row: the same source URL always
// hashes to the same row, so an image or clip reused across cards (or
// imports) is fetched and stored once.
func urlHash(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(sum[:])
}

// contentHash identifies an uploaded media row by its own bytes, so
// uploading the exact same file twice reuses one row instead of storing it
// twice.
func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// validMediaHash reports whether s could be one of our hashes: a full
// SHA-256 digest, 64 lowercase hex characters. Checked before any database
// work, so a probe against the serving route costs nothing.
func validMediaHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// contentTypeMatchesKind reports whether a sniffed content type is
// acceptable for kind ("image" or "audio") — "image/*" for an image,
// "audio/*" for audio. An unknown kind never matches.
func contentTypeMatchesKind(contentType, kind string) bool {
	switch kind {
	case MediaKindImage:
		return strings.HasPrefix(contentType, "image/")
	case MediaKindAudio:
		return strings.HasPrefix(contentType, "audio/")
	default:
		return false
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -run 'TestURLHash|TestContentHash|TestValidMediaHash|TestContentTypeMatchesKind' -v`
Expected: PASS (4 tests, one with 5 subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/apps/flash/media.go internal/apps/flash/media_test.go
git commit -m "feat(flash): add pure media hashing/validation helpers"
```

---

### Task 3: Media store CRUD

**Files:**
- Create: `internal/apps/flash/media_store.go`
- Test: `internal/apps/flash/media_store_test.go`

**Interfaces:**
- Consumes: `urlHash`, `contentHash` (Task 2); `formatTime`, `parseTime`, `ErrNotFound` (existing).
- Produces: `Media{Hash, Kind, ContentType string; Bytes []byte; SourceURL string; FetchedAt time.Time; ErrorCount int; LastError string}`, `(m Media) Cached() bool`, `dbExecutor` interface, `func (st *Store) MediaByHash(ctx, hash string) (Media, error)`, `func ensureMediaURL(ctx context.Context, exec dbExecutor, kind, sourceURL string) (string, error)` and its `Store.EnsureMediaURL` wrapper, `func (st *Store) SaveMediaUpload(ctx, kind, contentType string, data []byte, now time.Time) (string, error)`, `func (st *Store) SaveMediaBytes(ctx, hash, contentType string, data []byte, now time.Time) error`, `func (st *Store) SaveMediaFailure(ctx, hash, msg string, now time.Time) error` — used by Task 4 (fetch), Task 5 (import), Task 6 (handlers).

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/flash/media_store_test.go`:

```go
package flash_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestEnsureMediaURLCreatesAnUnfetchedRow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	hash, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, "https://example.com/cat.jpg")
	if err != nil {
		t.Fatalf("EnsureMediaURL: %v", err)
	}

	m, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatalf("MediaByHash: %v", err)
	}
	if m.Cached() {
		t.Error("a freshly-ensured URL row should not be cached yet")
	}
	if m.SourceURL != "https://example.com/cat.jpg" {
		t.Errorf("SourceURL = %q", m.SourceURL)
	}
	if m.Kind != flash.MediaKindImage {
		t.Errorf("Kind = %q, want image", m.Kind)
	}
}

func TestEnsureMediaURLIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	h1, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, "https://example.com/cat.jpg")
	if err != nil {
		t.Fatal(err)
	}
	h2, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, "https://example.com/cat.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("same URL produced different hashes: %q vs %q", h1, h2)
	}
}

func TestMediaByHashUnknownIsNotFound(t *testing.T) {
	f := newFixture(t)
	_, err := f.store.MediaByHash(context.Background(), strings.Repeat("0", 64))
	if !errors.Is(err, flash.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSaveMediaUploadStoresBytesImmediately(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	hash, err := f.store.SaveMediaUpload(ctx, flash.MediaKindAudio, "audio/mpeg", []byte("fake mp3 bytes"), time.Now().UTC())
	if err != nil {
		t.Fatalf("SaveMediaUpload: %v", err)
	}
	m, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Cached() {
		t.Error("an uploaded row should be cached immediately")
	}
	if m.SourceURL != "" {
		t.Errorf("SourceURL = %q, want empty for an upload", m.SourceURL)
	}
	if string(m.Bytes) != "fake mp3 bytes" {
		t.Errorf("Bytes = %q", m.Bytes)
	}
}

func TestSaveMediaBytesCachesAndClearsFailure(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	hash, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, "https://example.com/cat.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveMediaFailure(ctx, hash, "boom", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveMediaBytes(ctx, hash, "image/jpeg", []byte("jpeg-bytes"), time.Now().UTC()); err != nil {
		t.Fatalf("SaveMediaBytes: %v", err)
	}

	m, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Cached() {
		t.Error("should be cached after SaveMediaBytes")
	}
	if m.ErrorCount != 0 || m.LastError != "" {
		t.Errorf("failure was not cleared: count=%d last=%q", m.ErrorCount, m.LastError)
	}
}

func TestSaveMediaFailureIncrementsCount(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	hash, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, "https://example.com/cat.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveMediaFailure(ctx, hash, "first failure", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveMediaFailure(ctx, hash, "second failure", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	m, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if m.ErrorCount != 2 {
		t.Errorf("ErrorCount = %d, want 2", m.ErrorCount)
	}
	if m.LastError != "second failure" {
		t.Errorf("LastError = %q, want %q", m.LastError, "second failure")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/... -run 'TestEnsureMedia|TestMediaByHash|TestSaveMedia' -v`
Expected: FAIL to compile — none of `EnsureMediaURL`/`MediaByHash`/`SaveMediaUpload`/`SaveMediaBytes`/`SaveMediaFailure` exist yet.

- [ ] **Step 3: Write the implementation**

Create `internal/apps/flash/media_store.go`:

```go
// internal/apps/flash/media_store.go
package flash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Media is one cached image or audio clip.
type Media struct {
	Hash        string
	Kind        string
	ContentType string
	Bytes       []byte
	SourceURL   string
	FetchedAt   time.Time
	ErrorCount  int
	LastError   string
}

// Cached reports whether the bytes are in hand. A URL-attached row exists
// from the moment something references its source URL; the bytes arrive on
// first view. An uploaded row is Cached from the moment it's created.
func (m Media) Cached() bool { return len(m.Bytes) > 0 }

// dbExecutor is satisfied by both *sql.DB and *sql.Tx, so the ensure/upsert
// helpers below can run either as their own statement or inside a caller's
// already-open transaction (e.g. ImportDeck's) — the same reasoning tag.go's
// upsertCardTags documents for the same SetMaxOpenConns(1) constraint.
type dbExecutor interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// MediaByHash loads one media record. An unknown hash is ErrNotFound.
func (st *Store) MediaByHash(ctx context.Context, hash string) (Media, error) {
	return mediaByHash(ctx, st.db, hash)
}

func mediaByHash(ctx context.Context, exec dbExecutor, hash string) (Media, error) {
	var (
		m         Media
		bytes     []byte
		sourceURL sql.NullString
		fetched   sql.NullString
	)
	err := exec.QueryRowContext(ctx,
		`SELECT hash, kind, content_type, bytes, source_url, fetched_at, error_count, last_error
		 FROM flash_media WHERE hash = ?`, hash,
	).Scan(&m.Hash, &m.Kind, &m.ContentType, &bytes, &sourceURL, &fetched, &m.ErrorCount, &m.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return Media{}, ErrNotFound
	}
	if err != nil {
		return Media{}, fmt.Errorf("flash: load media: %w", err)
	}
	m.Bytes = bytes
	if sourceURL.Valid {
		m.SourceURL = sourceURL.String
	}
	if fetched.Valid {
		if m.FetchedAt, err = parseTime(fetched.String); err != nil {
			return Media{}, err
		}
	}
	return m, nil
}

// EnsureMediaURL records that something references a URL-attached media
// item, without fetching it, and returns the hash it should be referenced
// by. Safe to call repeatedly for the same URL: a second call is a no-op
// (INSERT OR IGNORE), so multiple cards referencing the same URL share one
// row.
func (st *Store) EnsureMediaURL(ctx context.Context, kind, sourceURL string) (string, error) {
	return ensureMediaURL(ctx, st.db, kind, sourceURL)
}

func ensureMediaURL(ctx context.Context, exec dbExecutor, kind, sourceURL string) (string, error) {
	hash := urlHash(sourceURL)
	if _, err := exec.ExecContext(ctx,
		`INSERT OR IGNORE INTO flash_media (hash, kind, source_url) VALUES (?, ?, ?)`,
		hash, kind, sourceURL); err != nil {
		return "", fmt.Errorf("flash: ensure media: %w", err)
	}
	return hash, nil
}

// SaveMediaUpload stores an uploaded file's bytes immediately, keyed by the
// content's own hash, and returns that hash. Safe to call repeatedly for
// identical bytes: a second call is a no-op (INSERT OR IGNORE), so
// uploading the same file twice reuses one row.
func (st *Store) SaveMediaUpload(ctx context.Context, kind, contentType string, data []byte, now time.Time) (string, error) {
	hash := contentHash(data)
	if _, err := st.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO flash_media (hash, kind, content_type, bytes, fetched_at) VALUES (?, ?, ?, ?, ?)`,
		hash, kind, contentType, data, formatTime(now)); err != nil {
		return "", fmt.Errorf("flash: save media upload: %w", err)
	}
	return hash, nil
}

// SaveMediaBytes caches a fetched media item and clears any recorded
// failure.
func (st *Store) SaveMediaBytes(ctx context.Context, hash, contentType string, data []byte, now time.Time) error {
	if _, err := st.db.ExecContext(ctx,
		`UPDATE flash_media SET bytes = ?, content_type = ?, fetched_at = ?, last_error = '', error_count = 0
		 WHERE hash = ?`,
		data, contentType, formatTime(now), hash); err != nil {
		return fmt.Errorf("flash: cache media: %w", err)
	}
	return nil
}

// SaveMediaFailure records that a fetch failed, so a dead URL is not
// re-fetched on every view.
func (st *Store) SaveMediaFailure(ctx context.Context, hash, msg string, now time.Time) error {
	if _, err := st.db.ExecContext(ctx,
		`UPDATE flash_media SET last_error = ?, error_count = error_count + 1, fetched_at = ?
		 WHERE hash = ?`,
		msg, formatTime(now), hash); err != nil {
		return fmt.Errorf("flash: record media failure: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -run 'TestEnsureMedia|TestMediaByHash|TestSaveMedia' -v`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/apps/flash/media_store.go internal/apps/flash/media_store_test.go
git commit -m "feat(flash): add flash_media store CRUD"
```

---

### Task 4: SSRF-guarded media fetch client

**Files:**
- Create: `internal/apps/flash/media_fetch.go`
- Test: `internal/apps/flash/media_fetch_test.go`

**Interfaces:**
- Consumes: `contentTypeMatchesKind` (Task 2), `Media`, `Store.SaveMediaBytes`, `Store.EnsureMediaURL`, `Store.MediaByHash` (Task 3).
- Produces: `MediaClient{DenyAddr func(string) error}`, `func NewMediaClient(version string) *MediaClient`, `func (c *MediaClient) Get(ctx, rawURL string, maxBytes int64) ([]byte, error)`, `ErrBlockedAddress`, `MaxImageFetchBytes = 5<<20`, `MaxAudioFetchBytes = 10<<20`, `func (st *Store) FetchAndCacheMedia(ctx, client *MediaClient, m Media, now time.Time) (Media, error)` — used by Task 6 (handlers).

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/flash/media_fetch_test.go`:

```go
package flash_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

// testMediaClient is a client that may talk to httptest, which listens on
// 127.0.0.1 — an address the real client refuses by construction. Injecting
// the guard is what makes both this and
// TestDefaultMediaClientRefusesPrivateAddresses possible; a package-level
// guard would allow only one of them.
func testMediaClient() *flash.MediaClient {
	c := flash.NewMediaClient("test")
	c.DenyAddr = func(string) error { return nil }
	return c
}

func TestMediaClientGetReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fake-image-bytes"))
	}))
	defer srv.Close()

	body, err := testMediaClient().Get(context.Background(), srv.URL, 1<<20)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(body) != "fake-image-bytes" {
		t.Errorf("body = %q", body)
	}
}

// An oversized body must be a hard error, not a silently truncated success —
// a truncated image still sniffs a confident content-type from its leading
// bytes and would otherwise be cached as valid, corrupt, forever.
func TestMediaClientGetRejectsAnOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := strings.Repeat("x", 4096)
		for i := 0; i < 64; i++ {
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	_, err := testMediaClient().Get(context.Background(), srv.URL, 1024)
	if err == nil {
		t.Fatal("Get: want an error for a body over the cap, got nil")
	}
}

func TestMediaClientGetRefusesTooManyRedirects(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/next", http.StatusFound)
	}))
	defer srv.Close()

	if _, err := testMediaClient().Get(context.Background(), srv.URL, 1<<20); err == nil {
		t.Fatal("a redirect loop returned no error")
	}
}

func TestMediaClientGetRefusesANonHTTPScheme(t *testing.T) {
	if _, err := testMediaClient().Get(context.Background(), "file:///etc/passwd", 1<<20); err == nil {
		t.Fatal("file:// was accepted")
	}
}

// This is the test that would silently stop meaning anything if DenyAddr
// were package-level and the tests overrode it: it uses the DEFAULT client.
func TestDefaultMediaClientRefusesPrivateAddresses(t *testing.T) {
	c := flash.NewMediaClient("test")

	for _, target := range []string{
		"http://127.0.0.1:8080/admin",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://192.168.1.1/",
		"http://[::1]:8080/",
	} {
		t.Run(target, func(t *testing.T) {
			_, err := c.Get(context.Background(), target, 1<<20)
			if err == nil {
				t.Fatalf("%s was fetched; the SSRF guard must refuse it", target)
			}
			if !errors.Is(err, flash.ErrBlockedAddress) {
				t.Errorf("error = %v, want ErrBlockedAddress", err)
			}
		})
	}
}

func TestFetchAndCacheMediaSavesBytesOnSuccess(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// A minimal valid JPEG magic-byte prefix so http.DetectContentType sniffs
	// "image/jpeg" rather than "application/octet-stream".
	jpegBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 0, 0, 0, 0}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(jpegBytes)
	}))
	defer srv.Close()

	hash, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	m, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := f.store.FetchAndCacheMedia(ctx, testMediaClient(), m, time.Now().UTC())
	if err != nil {
		t.Fatalf("FetchAndCacheMedia: %v", err)
	}
	if updated.ContentType != "image/jpeg" {
		t.Errorf("ContentType = %q, want image/jpeg", updated.ContentType)
	}

	reloaded, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Cached() {
		t.Error("media should be cached in the store after a successful fetch")
	}
}

func TestFetchAndCacheMediaRejectsContentTypeMismatch(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg") // claimed, but body below is HTML
		_, _ = w.Write([]byte("<html><body>not an image</body></html>"))
	}))
	defer srv.Close()

	hash, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	m, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.FetchAndCacheMedia(ctx, testMediaClient(), m, time.Now().UTC()); err == nil {
		t.Fatal("FetchAndCacheMedia: want an error for sniffed content not matching the media's kind")
	}

	reloaded, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Cached() {
		t.Error("media must not be cached when the content-type check fails")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/... -run 'TestMediaClient|TestDefaultMediaClient|TestFetchAndCacheMedia' -v`
Expected: FAIL to compile — `MediaClient`, `NewMediaClient`, `ErrBlockedAddress`, `FetchAndCacheMedia` are undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/apps/flash/media_fetch.go`:

```go
// internal/apps/flash/media_fetch.go
package flash

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

// Body size caps, applied to the decoded stream so a compression bomb is
// truncated rather than expanded.
const (
	MaxImageFetchBytes = 5 << 20
	MaxAudioFetchBytes = 10 << 20
)

// mediaMaxBytes returns the fetch size cap for kind.
func mediaMaxBytes(kind string) int64 {
	if kind == MediaKindAudio {
		return MaxAudioFetchBytes
	}
	return MaxImageFetchBytes
}

const (
	maxMediaRedirects   = 5
	mediaRequestTimeout = 30 * time.Second
)

// ErrBlockedAddress is a fetch refused because it resolved to an address
// this server must not reach: loopback, link-local, RFC1918 and friends.
var ErrBlockedAddress = errors.New("flash: blocked address")

// blockedMediaPrefixes are ranges netip's own predicates do not cover.
var blockedMediaPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT, and where Tailscale lives
	netip.MustParsePrefix("192.0.0.0/24"),  // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking
	netip.MustParsePrefix("192.0.2.0/24"),  // TEST-NET-1
}

// denyPrivateMediaAddr refuses an address this server has no business
// connecting to. It takes the address the dialer is about to connect to, not
// a hostname — checking the resolved IP at connect time, not the name,
// defeats DNS rebinding (a name that resolves to a public address when the
// card is imported and to 127.0.0.1 when its media is later fetched).
// Mirrors internal/apps/reader's own DenyPrivateAddr; apps never import each
// other, so this is an independent copy with the same justification.
func denyPrivateMediaAddr(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: unparseable dial address %q", ErrBlockedAddress, address)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("%w: %q is not an IP", ErrBlockedAddress, host)
	}
	ip = ip.Unmap()

	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, ip)
	}
	for _, p := range blockedMediaPrefixes {
		if p.Contains(ip) {
			return fmt.Errorf("%w: %s in %s", ErrBlockedAddress, ip, p)
		}
	}
	return nil
}

// MediaClient is the one HTTP client Flash uses for outbound media fetches.
type MediaClient struct {
	http *http.Client
	ua   string

	// DenyAddr decides whether a resolved address may be connected to. It is
	// a field rather than a package function because httptest listens on
	// 127.0.0.1, which the default guard refuses by construction: tests
	// inject a permissive variant.
	DenyAddr func(address string) error
}

// NewMediaClient returns a client with every guard in place.
func NewMediaClient(version string) *MediaClient {
	c := &MediaClient{
		ua:       "onsuite/" + version + " (ON Flash; +https://github.com/iliafrenkel/on-suite)",
		DenyAddr: denyPrivateMediaAddr,
	}

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			return c.DenyAddr(address)
		},
	}

	c.http = &http.Client{
		Timeout: mediaRequestTimeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
			MaxIdleConnsPerHost:   2,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxMediaRedirects {
				return fmt.Errorf("flash: more than %d redirects", maxMediaRedirects)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("flash: redirect to %q scheme", req.URL.Scheme)
			}
			return nil
		},
	}
	return c
}

// Get fetches rawURL's body under every guard this client carries, capped at
// maxBytes (read one byte past the limit so a truncated body can be told
// apart from one that exactly fills it — a truncated image still sniffs a
// confident content-type from its leading bytes and would otherwise be
// cached as valid, corrupt, forever).
func (c *MediaClient) Get(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("flash: parse %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("flash: refusing %q scheme", u.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("flash: build request: %w", err)
	}
	req.Header.Set("User-Agent", c.ua)

	res, err := c.http.Do(req)
	if err != nil {
		// Unwrap so errors.Is(err, ErrBlockedAddress) works through
		// url.Error and net.OpError.
		return nil, fmt.Errorf("flash: fetch %s: %w", u.Redacted(), err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("flash: %s returned %s", u.Redacted(), res.Status)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("flash: read body from %s: %w", u.Redacted(), err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("flash: response exceeds %d byte limit", maxBytes)
	}
	return body, nil
}

// FetchAndCacheMedia fetches m's source URL, validates the sniffed
// content-type matches m.Kind, and caches the result via SaveMediaBytes on
// success. It does not record a failure on error — the caller (which knows
// the retry-backoff policy) decides whether and how to record one.
func (st *Store) FetchAndCacheMedia(ctx context.Context, client *MediaClient, m Media, now time.Time) (Media, error) {
	body, err := client.Get(ctx, m.SourceURL, mediaMaxBytes(m.Kind))
	if err != nil {
		return Media{}, err
	}

	// Sniff rather than trust: a publisher claiming image/png over an HTML
	// document is exactly how a proxy becomes an HTML-injection vector on
	// its own origin.
	ct := http.DetectContentType(body)
	if !contentTypeMatchesKind(ct, m.Kind) {
		return Media{}, fmt.Errorf("flash: response is not %s media (%s)", m.Kind, ct)
	}

	if err := st.SaveMediaBytes(ctx, m.Hash, ct, body, now); err != nil {
		return Media{}, err
	}
	m.ContentType = ct
	m.Bytes = body
	return m, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -run 'TestMediaClient|TestDefaultMediaClient|TestFetchAndCacheMedia' -v`
Expected: PASS (7 tests, one with 5 subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/apps/flash/media_fetch.go internal/apps/flash/media_fetch_test.go
git commit -m "feat(flash): add the SSRF-guarded media fetch client"
```

---

### Task 5: Import integration

**Files:**
- Modify: `internal/apps/flash/import.go`
- Modify: `internal/apps/flash/import_store.go`
- Test: append to `internal/apps/flash/import_validate_test.go` and `internal/apps/flash/import_store_test.go`

**Interfaces:**
- Consumes: `ensureMediaURL` (Task 3), `MediaKindImage`, `MediaKindAudio` (Task 1).
- Produces: `parsedCard.ImageURL`, `parsedCard.AudioURL` (new fields); `ImportDeck` now sets `image_hash`/`audio_hash` on inserted cards. Nothing later tasks depend on (Task 6 uses the serving/upload path independently of import).

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/flash/import_validate_test.go`:

```go
func TestParseImportJSONWithImageAndAudioURLs(t *testing.T) {
	payload := `{"deck": {"name": "D"}, "cards": [
		{"front": "Q", "back": "A", "image": "https://example.com/cat.jpg", "audio": "https://example.com/meow.mp3"}
	]}`
	d, err := ParseImport(payload, "json")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if d.Cards[0].ImageURL != "https://example.com/cat.jpg" {
		t.Errorf("ImageURL = %q", d.Cards[0].ImageURL)
	}
	if d.Cards[0].AudioURL != "https://example.com/meow.mp3" {
		t.Errorf("AudioURL = %q", d.Cards[0].AudioURL)
	}
}

func TestParseImportJSONRejectsBadMediaURL(t *testing.T) {
	payload := `{"deck": {"name": "D"}, "cards": [{"front": "Q", "back": "A", "image": "not-a-url"}]}`
	_, err := ParseImport(payload, "json")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestParseImportMarkdownWithImageAndAudioURLs(t *testing.T) {
	payload := "# D\n\n## Card\nFront: Q\nBack: A\nImage: https://example.com/cat.jpg\nAudio: https://example.com/meow.mp3\n"
	d, err := ParseImport(payload, "markdown")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if d.Cards[0].ImageURL != "https://example.com/cat.jpg" {
		t.Errorf("ImageURL = %q", d.Cards[0].ImageURL)
	}
	if d.Cards[0].AudioURL != "https://example.com/meow.mp3" {
		t.Errorf("AudioURL = %q", d.Cards[0].AudioURL)
	}
}

func TestParseImportMarkdownRejectsBadMediaURL(t *testing.T) {
	payload := "# D\n\n## Card\nFront: Q\nBack: A\nImage: not-a-url\n"
	_, err := ParseImport(payload, "markdown")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestParseImportWithoutMediaURLsLeavesThemEmpty(t *testing.T) {
	payload := `{"deck": {"name": "D"}, "cards": [{"front": "Q", "back": "A"}]}`
	d, err := ParseImport(payload, "json")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if d.Cards[0].ImageURL != "" || d.Cards[0].AudioURL != "" {
		t.Errorf("card = %+v, want no media URLs", d.Cards[0])
	}
}
```

Append to `internal/apps/flash/import_store_test.go`:

```go
func TestImportDeckSetsMediaHashesWithoutFetching(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	d, err := flash.ParseImport(`{
		"deck": {"name": "Imported"},
		"cards": [
			{"front": "Q1", "back": "A1", "image": "https://example.com/cat.jpg", "audio": "https://example.com/meow.mp3"}
		]
	}`, "json")
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}

	deck, err := f.store.ImportDeck(ctx, f.alice.ID, d.Name, d.Description, d.Cards)
	if err != nil {
		t.Fatalf("ImportDeck: %v", err)
	}
	cards, err := f.store.ListCards(ctx, f.alice.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 {
		t.Fatalf("len(cards) = %d, want 1", len(cards))
	}
	c := cards[0]
	if c.ImageHash == nil {
		t.Fatal("ImageHash is nil, want it set")
	}
	if c.AudioHash == nil {
		t.Fatal("AudioHash is nil, want it set")
	}

	img, err := f.store.MediaByHash(ctx, *c.ImageHash)
	if err != nil {
		t.Fatalf("MediaByHash(image): %v", err)
	}
	if img.Cached() {
		t.Error("import must not fetch media eagerly — image should be unfetched")
	}
	if img.SourceURL != "https://example.com/cat.jpg" {
		t.Errorf("SourceURL = %q", img.SourceURL)
	}

	aud, err := f.store.MediaByHash(ctx, *c.AudioHash)
	if err != nil {
		t.Fatalf("MediaByHash(audio): %v", err)
	}
	if aud.Cached() {
		t.Error("import must not fetch media eagerly — audio should be unfetched")
	}
}

func TestImportDeckWithoutMediaLeavesHashesNil(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	d, err := flash.ParseImport(`{"deck": {"name": "D"}, "cards": [{"front": "Q", "back": "A"}]}`, "json")
	if err != nil {
		t.Fatal(err)
	}
	deck, err := f.store.ImportDeck(ctx, f.alice.ID, d.Name, d.Description, d.Cards)
	if err != nil {
		t.Fatal(err)
	}
	cards, err := f.store.ListCards(ctx, f.alice.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cards[0].ImageHash != nil || cards[0].AudioHash != nil {
		t.Errorf("card = %+v, want no media hashes", cards[0])
	}
}

func TestImportDeckSharesOneMediaRowForRepeatedURL(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	d, err := flash.ParseImport(`{
		"deck": {"name": "Imported"},
		"cards": [
			{"front": "Q1", "back": "A1", "image": "https://example.com/flag.jpg"},
			{"front": "Q2", "back": "A2", "image": "https://example.com/flag.jpg"}
		]
	}`, "json")
	if err != nil {
		t.Fatal(err)
	}
	deck, err := f.store.ImportDeck(ctx, f.alice.ID, d.Name, d.Description, d.Cards)
	if err != nil {
		t.Fatal(err)
	}
	cards, err := f.store.ListCards(ctx, f.alice.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *cards[0].ImageHash != *cards[1].ImageHash {
		t.Errorf("same URL produced two different media rows: %q vs %q", *cards[0].ImageHash, *cards[1].ImageHash)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/... -run 'TestParseImport.*Media|TestImportDeck.*Media' -v`
Expected: FAIL — `parsedCard.ImageURL`/`AudioURL` are undefined, and `ImportDeck` doesn't set hashes yet.

- [ ] **Step 3: Modify `import.go`**

Change the imports and `parsedCard` struct:

```go
import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// parsedCard is one card extracted from an import payload, already validated
// against the same rules ValidateCard/ValidateTagNames apply to hand-created
// cards. ImageURL/AudioURL are "" when the card has no media attached; when
// set, they are validated for shape only (see validateMediaURL) — the URL is
// not fetched at parse time. See media.go/import_store.go for what happens
// to them next.
type parsedCard struct {
	CardType string
	Front    string
	Back     string
	Notes    string
	Tags     []string
	ImageURL string
	AudioURL string
}
```

Change `importJSON`'s card fields:

```go
	Cards []struct {
		Type  string   `json:"type"`
		Front string   `json:"front"`
		Back  string   `json:"back"`
		Notes string   `json:"notes"`
		Tags  []string `json:"tags"`
		Image string   `json:"image"`
		Audio string   `json:"audio"`
	} `json:"cards"`
```

Change `validateParsedCard` and add `validateMediaURL`:

```go
func validateParsedCard(cardType, front, back, notes string, tags []string, imageURL, audioURL string, cardNum int) error {
	if err := ValidateCard(cardType, front, back); err != nil {
		return fmt.Errorf("%w: card %d: %s", ErrInvalid, cardNum, stripErrInvalid(err))
	}
	if err := validateCardNotes(notes); err != nil {
		return fmt.Errorf("%w: card %d: %s", ErrInvalid, cardNum, stripErrInvalid(err))
	}
	if err := ValidateTagNames(tags); err != nil {
		return fmt.Errorf("%w: card %d: %s", ErrInvalid, cardNum, stripErrInvalid(err))
	}
	if err := validateMediaURL(imageURL); err != nil {
		return fmt.Errorf("%w: card %d: image: %s", ErrInvalid, cardNum, stripErrInvalid(err))
	}
	if err := validateMediaURL(audioURL); err != nil {
		return fmt.Errorf("%w: card %d: audio: %s", ErrInvalid, cardNum, stripErrInvalid(err))
	}
	return nil
}

// validateMediaURL checks a card's image/audio URL for shape only — it must
// be an absolute http/https URL. An empty string (no media attached) is
// valid. The URL is never fetched here; see FetchAndCacheMedia.
func validateMediaURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: %q is not a valid http(s) URL", ErrInvalid, rawURL)
	}
	return nil
}
```

In `parseImportJSON`, change the `validateParsedCard` call and the appended `parsedCard`:

```go
		if err := validateParsedCard(cardType, c.Front, c.Back, c.Notes, c.Tags, c.Image, c.Audio, i+1); err != nil {
			return parsedDeck{}, err
		}
		deck.Cards = append(deck.Cards, parsedCard{
			CardType: cardType, Front: c.Front, Back: c.Back, Notes: c.Notes, Tags: c.Tags,
			ImageURL: c.Image, AudioURL: c.Audio,
		})
```

In `parseCardBlock`, add `"image"` and `"audio"` to the recognized-key switch:

```go
			switch key {
			case "type", "front", "back", "tags", "notes", "image", "audio":
```

In `parseImportMarkdown`, change the per-card field extraction and the `validateParsedCard` call:

```go
		front, back, notes := fields["front"], fields["back"], fields["notes"]
		tags := splitMarkdownTags(fields["tags"])
		imageURL, audioURL := strings.TrimSpace(fields["image"]), strings.TrimSpace(fields["audio"])

		if err := validateParsedCard(cardType, front, back, notes, tags, imageURL, audioURL, cardNum); err != nil {
			return parsedDeck{}, err
		}
		deck.Cards = append(deck.Cards, parsedCard{
			CardType: cardType, Front: front, Back: back, Notes: notes, Tags: tags,
			ImageURL: imageURL, AudioURL: audioURL,
		})
```

- [ ] **Step 4: Modify `import_store.go`**

Change `ImportDeck`'s per-card loop to compute media hashes before inserting the card row, and include `image_hash`/`audio_hash` in the INSERT:

```go
	for _, c := range cards {
		// A card's image/audio URL is only recorded here, never fetched: an
		// actual network fetch must not run inside this transaction (flash's
		// SQLite handle allows only one connection at a time, so a slow
		// outbound call here would block every other request in the app for
		// its duration). ensureMediaURL just hashes the URL and inserts a
		// metadata-only row if one doesn't already exist; the real fetch
		// happens lazily, the first time the media is requested (see
		// media_fetch.go, handlers_media.go).
		var imageHash, audioHash *string
		if c.ImageURL != "" {
			h, err := ensureMediaURL(ctx, tx, MediaKindImage, c.ImageURL)
			if err != nil {
				return Deck{}, err
			}
			imageHash = &h
		}
		if c.AudioURL != "" {
			h, err := ensureMediaURL(ctx, tx, MediaKindAudio, c.AudioURL)
			if err != nil {
				return Deck{}, err
			}
			audioHash = &h
		}

		var cardID int64
		err = tx.QueryRowContext(ctx,
			`INSERT INTO flash_cards (deck_id, user_id, card_type, front, back, notes, created_at, image_hash, audio_hash)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			 RETURNING id`,
			d.ID, userID, c.CardType, c.Front, c.Back, c.Notes, formatTime(st.now()), imageHash, audioHash,
		).Scan(&cardID)
		if err != nil {
			return Deck{}, fmt.Errorf("flash: import deck: %w", err)
		}
```

(The rest of the loop — `upsertCardTags` — is unchanged.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -run 'TestParseImport|TestImportDeck' -v`
Expected: PASS — all pre-existing F3 import tests plus the new media ones (30 tests total).

Run the whole package once more:

Run: `go test ./internal/apps/flash/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/flash/import.go internal/apps/flash/import_store.go \
        internal/apps/flash/import_validate_test.go internal/apps/flash/import_store_test.go
git commit -m "feat(flash): wire import-time image/audio URLs into flash_media"
```

---

### Task 6: Media serving route, manual upload, and UI

**Files:**
- Create: `internal/apps/flash/handlers_media.go`
- Create: `internal/apps/flash/export_test.go`
- Modify: `internal/apps/flash/flash.go` (App fields, Mount initialization, two new routes)
- Modify: `internal/apps/flash/handlers_cards.go` (`cardDetailView` fields, `mediaURL` helper, `viewCardDetail`/`viewCardDetailWithMediaError`)
- Modify: `internal/apps/flash/templates/cards.html` (media display + upload form in `card-detail-view`)
- Test: `internal/apps/flash/handlers_media_test.go`

**Interfaces:**
- Consumes: `Store.MediaByHash`, `Store.FetchAndCacheMedia`, `Store.SaveMediaFailure`, `Store.SaveMediaUpload`, `Store.SetCardMedia` (Tasks 1, 3, 4); `validMediaHash`, `contentTypeMatchesKind` (Task 2); existing `a.userID`, `a.fail`, `a.cardDeck`, `a.cardIDFromPath`, `cardBasePath`, `userMessage`, `a.renderCardIndex`, `a.renderCardDetailWithList` (F1's `handlers_cards.go`).
- Produces: `GET /media/{hash}`, `POST /{deckID}/cards/{cardID}/media` routes. Nothing later tasks depend on (this is the last task).

- [ ] **Step 1: Modify `flash.go`**

Change the `App` struct and add a constant just above it:

```go
// mediaFetchConcurrency bounds outbound media fetches across all requests,
// the same role internal/apps/reader's imageFetchConcurrency plays for
// article images — without a bound, a deck full of cards with images could
// open many simultaneous connections to one host on first review.
const mediaFetchConcurrency = 4

// App is ON Flash. It is constructed before the platform exists, in the
// registration slice in main, and receives everything it needs in Mount.
type App struct {
	store       *Store
	deps        app.Deps
	mediaClient *MediaClient
	mediaSem    chan struct{}
}
```

In `Mount`, change:

```go
	a.deps = deps
	a.store = NewStore(deps.DB)
```

to:

```go
	a.deps = deps
	a.store = NewStore(deps.DB)
	a.mediaClient = NewMediaClient(deps.Version)
	a.mediaSem = make(chan struct{}, mediaFetchConcurrency)
```

At the end of `Mount`, change:

```go
	r.HandleFunc("GET /flash-review.js", a.script)
}
```

to:

```go
	r.HandleFunc("GET /flash-review.js", a.script)

	// "media" is a literal single segment, the same non-ambiguity shape as
	// "new"/"review"/"import" alongside the wildcard single-segment routes
	// above. The upload route is 4 segments (wildcard, literal, wildcard,
	// literal) — the same shape as the existing
	// POST /{deckID}/cards/{cardID}/delete, differing only in its final
	// literal ("media" vs "delete"), which is what actually guarantees no
	// ambiguity between the two: a literal-vs-literal mismatch at any one
	// position makes overlap impossible regardless of how the remaining
	// positions compare (see the review-route comment above for the fuller
	// version of this reasoning).
	r.HandleFunc("GET /media/{hash}", a.media)
	r.HandleFunc("POST /{deckID}/cards/{cardID}/media", a.uploadCardMedia)
}
```

- [ ] **Step 2: Modify `handlers_cards.go`**

Change the `cardDetailView` struct:

```go
type cardDetailView struct {
	Mode      string
	Deck      Deck
	Card      Card
	Tags      []tagChip
	CSRFToken string

	CardTypeValue string
	FrontValue    string
	BackValue     string
	NotesValue    string
	TagsValue     string
	Error         string

	// ImageMediaURL/AudioMediaURL are the card's serving-route paths
	// ("/flash/media/{hash}"), or "" if no such media is attached. Computed
	// here rather than in the template because Card.ImageHash/AudioHash are
	// *string: printing a pointer directly would show its address, not the
	// hash.
	ImageMediaURL string
	AudioMediaURL string
	MediaError    string
}

// mediaURL is the serving-route path for a card's image/audio hash, or ""
// if hash is nil (no such media attached).
func mediaURL(hash *string) string {
	if hash == nil {
		return ""
	}
	return "/flash/media/" + *hash
}
```

Change `viewCardDetail` and add `viewCardDetailWithMediaError` immediately after it:

```go
func (a *App) viewCardDetail(r *http.Request, userID int64, d Deck, c Card) cardDetailView {
	// Best-effort: a tag-lookup failure here is a genuine database error (the
	// card was just created/updated under this same user), not something
	// worth failing the whole render over, so the view just shows no chips.
	tags, _ := a.store.TagsForCard(r.Context(), userID, c.ID)
	return cardDetailView{
		Mode: cardModeView, Deck: d, Card: c, Tags: tagChips(tags), CSRFToken: web.CSRFToken(r.Context()),
		ImageMediaURL: mediaURL(c.ImageHash), AudioMediaURL: mediaURL(c.AudioHash),
	}
}

// viewCardDetailWithMediaError is viewCardDetail plus an error message from
// a failed media upload, so the card page can show both the card and why
// the attachment attempt just failed.
func (a *App) viewCardDetailWithMediaError(r *http.Request, userID int64, d Deck, c Card, errMsg string) cardDetailView {
	v := a.viewCardDetail(r, userID, d, c)
	v.MediaError = errMsg
	return v
}
```

- [ ] **Step 3: Create `handlers_media.go`**

Create `internal/apps/flash/handlers_media.go`:

```go
// internal/apps/flash/handlers_media.go
package flash

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// mediaCacheControl is private because the response is only meaningful to a
// signed-in user, and long because the URL is content-addressed: a different
// source produces a different hash and therefore a different path.
const mediaCacheControl = "private, max-age=86400"

// maxMediaFetchAttempts and mediaRetryBackoff mirror
// internal/apps/reader's own maxImageFetchAttempts/imageRetryBackoff: give up
// permanently after 3 consecutive failures, otherwise wait an hour between
// attempts, since this app has no external signal to know sooner when a
// transient failure has cleared.
const (
	maxMediaFetchAttempts = 3
	mediaRetryBackoff     = 1 * time.Hour
)

// media serves a card's image or audio clip, fetching and caching it on
// first request if it hasn't been fetched yet. The route takes a hash, never
// a URL — the only way a hash resolves is if ImportDeck or an upload
// recorded it, so there is no input here that makes this fetch something
// else.
func (a *App) media(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	hash := r.PathValue("hash")
	if !validMediaHash(hash) {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}

	m, err := a.store.MediaByHash(r.Context(), hash)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if m.Cached() {
		a.writeMedia(w, r, m)
		return
	}
	if m.SourceURL == "" {
		// A row with no bytes and no source URL is a data-integrity
		// impossibility given how rows are created (EnsureMediaURL always
		// sets source_url; SaveMediaUpload always sets bytes) — there is
		// nothing to fetch either way, so treat it the same as not found.
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	// Give up permanently past the attempt cap, and otherwise still refuse
	// immediately inside the backoff window — a failure older than the
	// backoff window, under the cap, falls through to a real retry.
	if m.ErrorCount >= maxMediaFetchAttempts ||
		(m.ErrorCount > 0 && time.Since(m.FetchedAt) < mediaRetryBackoff) {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}

	fetched, err := a.fetchMedia(r, m)
	if err != nil {
		// A canceled request context means the viewer navigated away
		// mid-fetch — user-navigation noise, not a real failure, and must
		// not count toward the retry budget or reset the backoff clock.
		if errors.Is(err, context.Canceled) || r.Context().Err() != nil {
			a.deps.Log.Info("flash media fetch canceled", "src", m.SourceURL, "error", err)
			return
		}
		a.deps.Log.Info("flash media fetch failed", "src", m.SourceURL, "error", err)
		if err := a.store.SaveMediaFailure(r.Context(), hash, err.Error(), a.store.now()); err != nil {
			a.deps.Log.Error("flash recording a media failure failed", "error", err)
		}
		// 404 rather than 502: the browser shows its native broken-media
		// state, which is the right outcome for something that will not
		// load.
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	a.writeMedia(w, r, fetched)
}

// fetchMedia retrieves and caches one media item, bounded by mediaSem so a
// deck full of images can't open many simultaneous connections to one host.
func (a *App) fetchMedia(r *http.Request, m Media) (Media, error) {
	select {
	case a.mediaSem <- struct{}{}:
		defer func() { <-a.mediaSem }()
	case <-r.Context().Done():
		return Media{}, r.Context().Err()
	}
	return a.store.FetchAndCacheMedia(r.Context(), a.mediaClient, m, a.store.now())
}

func (a *App) writeMedia(w http.ResponseWriter, r *http.Request, m Media) {
	etag := `"` + m.Hash + `"`
	h := w.Header()
	h.Set("Cache-Control", mediaCacheControl)
	h.Set("ETag", etag)
	h.Set("X-Content-Type-Options", "nosniff")

	// The validator check comes before Content-Type and Content-Length: a
	// 304 carries neither, and setting them anyway is the kind of small
	// protocol wrongness proxies and caches punish unpredictably.
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", m.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(m.Bytes)))
	if _, err := w.Write(m.Bytes); err != nil {
		a.deps.Log.Info("flash writing media failed", "error", err)
	}
}

// uploadCardMedia attaches or removes one of userID's own card's media
// attachments, via a multipart form with optional "image"/"audio" file
// parts and optional "remove_image"/"remove_audio" flags. A validation
// failure (oversized file, wrong content type) re-renders the card at 400
// with the error shown, the same pattern every other flash form uses —
// not a generic error page, since the user is watching this happen.
func (a *App) uploadCardMedia(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	cardID, ok := a.cardIDFromPath(w, r)
	if !ok {
		return
	}
	c, err := a.store.CardByID(r.Context(), userID, deck.ID, cardID)
	if err != nil {
		a.fail(w, r, err)
		return
	}

	// A remove-only request (no file attached) may arrive as a plain
	// form-urlencoded POST rather than multipart/form-data — ErrNotMultipart
	// is expected there, not a failure: ParseMultipartForm still populates
	// r.Form/r.PostForm via its own internal ParseForm call before returning
	// it, so PostFormValue below works either way.
	if err := r.ParseMultipartForm(MaxAudioFetchBytes); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest,
			a.viewCardDetailWithMediaError(r, userID, deck, c, "That upload could not be read."))
		return
	}

	for _, spec := range []struct {
		kind, field, remove string
		maxBytes            int64
	}{
		{MediaKindImage, "image", "remove_image", MaxImageFetchBytes},
		{MediaKindAudio, "audio", "remove_audio", MaxAudioFetchBytes},
	} {
		if r.PostFormValue(spec.remove) != "" {
			if err := a.store.SetCardMedia(r.Context(), userID, deck.ID, cardID, spec.kind, nil); err != nil {
				a.fail(w, r, err)
				return
			}
			continue
		}
		if errMsg := a.attachUpload(w, r, userID, deck, cardID, spec.kind, spec.field, spec.maxBytes); errMsg != "" {
			a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest,
				a.viewCardDetailWithMediaError(r, userID, deck, c, errMsg))
			return
		}
	}

	updated, err := a.store.CardByID(r.Context(), userID, deck.ID, cardID)
	if err != nil {
		a.fail(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, cardBasePath(deck.ID)+strconv.FormatInt(cardID, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", cardBasePath(deck.ID)+strconv.FormatInt(cardID, 10))
	a.renderCardDetailWithList(w, r, userID, deck, http.StatusOK, a.viewCardDetail(r, userID, deck, updated))
}

// attachUpload reads one optional file part named field ("image" or
// "audio"), and if present, stores it and attaches it to the card. It
// returns a non-empty user-facing message if the part is present but
// invalid (oversized, wrong content type, or a store failure); no part
// present is not an error — the request may only be touching the other
// media kind, or removing one — so it returns "".
func (a *App) attachUpload(w http.ResponseWriter, r *http.Request, userID int64, deck Deck, cardID int64, kind, field string, maxBytes int64) string {
	file, header, err := r.FormFile(field)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()

	if header.Size > maxBytes {
		return "That file is larger than the " + strconv.FormatInt(maxBytes>>20, 10) + "MB limit."
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, file, maxBytes))
	if err != nil {
		return "That file is larger than the " + strconv.FormatInt(maxBytes>>20, 10) + "MB limit."
	}

	ct := http.DetectContentType(data)
	if !contentTypeMatchesKind(ct, kind) {
		return "That file does not look like " + kind + " content."
	}

	hash, err := a.store.SaveMediaUpload(r.Context(), kind, ct, data, a.store.now())
	if err != nil {
		return userMessage(err)
	}
	if err := a.store.SetCardMedia(r.Context(), userID, deck.ID, cardID, kind, &hash); err != nil {
		return userMessage(err)
	}
	return ""
}
```

- [ ] **Step 4: Create `export_test.go`**

Create `internal/apps/flash/export_test.go`:

```go
package flash

// AllowPrivateFetchesForTest lets this app's HTTP client reach loopback, so
// handler tests can point it at an httptest origin. Production never calls
// it; the real guard is what TestDefaultMediaClientRefusesPrivateAddresses
// exercises.
//
// It must be called after Mount, which is where a.mediaClient is built —
// apptest.NewServer has already mounted by the time it hands the App back.
//
// This lives in a _test.go file (the standard Go export_test.go idiom) so it
// is compiled into the test binary — where package flash_test can still call
// it, since Go links internal and external test files together — but never
// into the production binary.
func (a *App) AllowPrivateFetchesForTest() {
	a.mediaClient.DenyAddr = func(string) error { return nil }
}

// MaxMediaFetchAttemptsForTest and MediaRetryBackoffForTest mirror
// handlers_media.go's unexported maxMediaFetchAttempts/mediaRetryBackoff, so
// a test can assert against the real thresholds rather than a hardcoded copy
// that could silently drift out of sync with them.
const (
	MaxMediaFetchAttemptsForTest = maxMediaFetchAttempts
	MediaRetryBackoffForTest     = mediaRetryBackoff
)
```

- [ ] **Step 5: Modify `templates/cards.html`**

In `card-detail-view`, add media display right after the notes line, and an upload/remove form after the existing row of buttons:

```
{{define "card-detail-view"}}
<div class="stack" id="card-detail-view">
	<p class="faint">{{.Card.CardType}}</p>
	<h1>{{.Card.Front}}</h1>
	{{if eq .Card.CardType "basic"}}<p>{{.Card.Back}}</p>{{end}}
	{{with .Card.Notes}}<p class="dim">{{.}}</p>{{end}}
	{{with .ImageMediaURL}}<img class="card-media-image" src="{{.}}" alt="">{{end}}
	{{with .AudioMediaURL}}<audio controls src="{{.}}"></audio>{{end}}
	{{with .Tags}}
	<ul class="tag-list">
		{{range .}}<li><a href="{{.Href}}">{{.Name}}</a></li>{{end}}
	</ul>
	{{end}}
	<div class="row">
		<a class="toolbar-btn" href="/flash/{{$.Deck.ID}}/cards/edit/{{.Card.ID}}" hx-get="/flash/{{$.Deck.ID}}/cards/edit/{{.Card.ID}}" hx-target="#card-detail" hx-push-url="true">Edit</a>
		<form method="post" action="/flash/{{$.Deck.ID}}/cards/{{.Card.ID}}/delete" hx-post="/flash/{{$.Deck.ID}}/cards/{{.Card.ID}}/delete" hx-target="#card-detail" hx-swap="innerHTML"
		      data-confirm="Delete this card? This cannot be undone.">
			<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
			<button type="submit" class="toolbar-btn danger">Delete</button>
		</form>
	</div>
	{{with .MediaError}}<div class="notice notice-error" role="alert">{{.}}</div>{{end}}
	<form class="stack" id="card-media-form" method="post" action="/flash/{{$.Deck.ID}}/cards/{{.Card.ID}}/media" hx-post="/flash/{{$.Deck.ID}}/cards/{{.Card.ID}}/media" hx-target="#card-detail" hx-swap="innerHTML" enctype="multipart/form-data">
		<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
		<div class="field">
			<label for="card-image-{{.Card.ID}}">Image</label>
			<input id="card-image-{{.Card.ID}}" type="file" name="image" accept="image/*">
			{{if .ImageMediaURL}}<label><input type="checkbox" name="remove_image" value="1"> Remove image</label>{{end}}
		</div>
		<div class="field">
			<label for="card-audio-{{.Card.ID}}">Audio</label>
			<input id="card-audio-{{.Card.ID}}" type="file" name="audio" accept="audio/*">
			{{if .AudioMediaURL}}<label><input type="checkbox" name="remove_audio" value="1"> Remove audio</label>{{end}}
		</div>
		<button type="submit" class="toolbar-btn">Update media</button>
	</form>
</div>
{{end}}
```

- [ ] **Step 6: Write the failing HTTP tests**

Create `internal/apps/flash/handlers_media_test.go`:

```go
package flash_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// onePNG is the smallest thing http.DetectContentType calls an image/png.
var onePNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
}

// newServerWithApp is newServer, but it also hands back the App, so a test
// can call AllowPrivateFetchesForTest — the proxy's fetches go to an
// httptest origin on 127.0.0.1, which the SSRF guard refuses by
// construction. Mirrors internal/apps/reader's own newServerWithApp.
func newServerWithApp(t *testing.T) (*apptest.Server[*flash.Store], *flash.App) {
	t.Helper()
	a := flash.New()
	return apptest.NewServer(t, a, flash.NewStore), a
}

// seedCardWithImage creates a deck and a card carrying an unfetched
// URL-attached image, and returns the card and its image hash.
func seedCardWithImage(t *testing.T, s *apptest.Server[*flash.Store], srcURL string) (flash.Card, string) {
	t.Helper()
	ctx := context.Background()

	deck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Media deck", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := s.Store.EnsureMediaURL(ctx, flash.MediaKindImage, srcURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetCardMedia(ctx, s.Alice.User.ID, deck.ID, c.ID, flash.MediaKindImage, &hash); err != nil {
		t.Fatal(err)
	}
	return c, hash
}

func TestMediaFetchesCachesAndServes(t *testing.T) {
	s, a := newServerWithApp(t)
	var hits int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(onePNG)
	}))
	defer origin.Close()

	_, hash := seedCardWithImage(t, s, origin.URL+"/a.png")
	a.AllowPrivateFetchesForTest()

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("first request = %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("nosniff header missing")
	}

	rec2 := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("second request = %d", rec2.Code)
	}
	if hits != 1 {
		t.Errorf("origin was hit %d times; the second view must come from cache", hits)
	}
}

func TestMediaConditionalRequestReturns304(t *testing.T) {
	s, a := newServerWithApp(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(onePNG)
	}))
	defer origin.Close()

	_, hash := seedCardWithImage(t, s, origin.URL+"/a.png")
	a.AllowPrivateFetchesForTest()

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil))
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on first response")
	}

	req := httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil)
	req.Header.Set("If-None-Match", etag)
	rec2 := s.Do(t, s.Alice, req)
	if rec2.Code != http.StatusNotModified {
		t.Errorf("conditional request = %d, want 304", rec2.Code)
	}
}

func TestMediaRefusesAnUnknownHash(t *testing.T) {
	s := newServer(t)
	hash := strings.Repeat("0", 64)
	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown hash = %d, want 404", rec.Code)
	}
}

func TestMediaRefusesAMalformedHash(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/media/not-a-hash", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("malformed hash = %d, want 404", rec.Code)
	}
}

func TestMediaRejectsNonImageContent(t *testing.T) {
	s, a := newServerWithApp(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png") // lying
		_, _ = w.Write([]byte("<html><body>not an image</body></html>"))
	}))
	defer origin.Close()

	_, hash := seedCardWithImage(t, s, origin.URL+"/a.png")
	a.AllowPrivateFetchesForTest()

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("non-image content = %d, want 404", rec.Code)
	}
}

func TestMediaGivesUpPermanentlyPastAttemptCap(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	_, hash := seedCardWithImage(t, s, "http://127.0.0.1:1/unreachable")

	for i := 0; i < flash.MaxMediaFetchAttemptsForTest; i++ {
		if err := s.Store.SaveMediaFailure(ctx, hash, "boom", time.Now().UTC().Add(-2*flash.MediaRetryBackoffForTest)); err != nil {
			t.Fatal(err)
		}
	}

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("past the attempt cap = %d, want 404 (no further retry)", rec.Code)
	}
}

func TestMediaRefusesRetryWithinBackoffWindow(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	_, hash := seedCardWithImage(t, s, "http://127.0.0.1:1/unreachable")

	if err := s.Store.SaveMediaFailure(ctx, hash, "boom", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("within backoff window = %d, want 404", rec.Code)
	}
}

func TestMediaRequiresSignIn(t *testing.T) {
	s := newServer(t)
	hash := strings.Repeat("0", 64)
	rec := s.Do(t, nil, httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil))
	if rec.Code != 303 && rec.Code != 401 {
		t.Errorf("signed out = %d, want a redirect-to-login or 401", rec.Code)
	}
}

func TestUploadCardImageOverHTTP(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	deck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}

	path := "/flash/" + itoa(deck.ID) + "/cards/" + itoa(c.ID) + "/media"
	rec := s.UploadHX(t, s.Alice, path, "image", "cat.png", onePNG)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload = %d: %s", rec.Code, rec.Body.String())
	}

	updated, err := s.Store.CardByID(ctx, s.Alice.User.ID, deck.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ImageHash == nil {
		t.Fatal("ImageHash is nil after upload")
	}
	m, err := s.Store.MediaByHash(ctx, *updated.ImageHash)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Cached() {
		t.Error("an uploaded image should be cached immediately")
	}
}

func TestUploadCardImageRejectsWrongContentType(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	deck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}

	// An HTMX request renders its validation failure at 200 with a
	// .notice-error fragment, not a real 400 — htmx only swaps content on a
	// 2xx/3xx response, the same convention every other flash form follows.
	path := "/flash/" + itoa(deck.ID) + "/cards/" + itoa(c.ID) + "/media"
	rec := s.UploadHX(t, s.Alice, path, "image", "fake.png", []byte("<html>not an image</html>"))
	if rec.Code != http.StatusOK {
		t.Fatalf("wrong content type (HTMX) = %d, want 200 with a notice-error fragment", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".notice-error")

	updated, err := s.Store.CardByID(ctx, s.Alice.User.ID, deck.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ImageHash != nil {
		t.Error("ImageHash should not be set after a rejected upload")
	}
}

func TestUploadCardMediaRequiresCSRF(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	deck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}

	req := httpPost(t, "/flash/"+itoa(deck.ID)+"/cards/"+itoa(c.ID)+"/media", nil)
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("upload without CSRF = %d, want 403", rec.Code)
	}
}

func TestRemoveCardImageOverHTTP(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	deck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := s.Store.SaveMediaUpload(ctx, flash.MediaKindImage, "image/png", onePNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetCardMedia(ctx, s.Alice.User.ID, deck.ID, c.ID, flash.MediaKindImage, &hash); err != nil {
		t.Fatal(err)
	}

	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/"+itoa(c.ID)+"/media",
		url.Values{"remove_image": {"1"}}, "/flash/"+itoa(deck.ID)+"/cards/"+itoa(c.ID))

	updated, err := s.Store.CardByID(ctx, s.Alice.User.ID, deck.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ImageHash != nil {
		t.Error("ImageHash still set after removal")
	}
}
```

`newServer`, `itoa`, `httpPost` are already defined in `internal/apps/flash/handlers_decks_test.go` (same `flash_test` package) — do not redefine them. `s.UploadHX` and `s.Submit` are existing `apptest.Server` methods (`internal/apptest/apptest.go`).

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -run 'TestMedia|TestUploadCard|TestRemoveCard' -v`
Expected: PASS (12 tests).

Run the whole flash package and the arch/app-boundary suite:

Run: `go test ./internal/apps/flash/... ./internal/arch/...`
Expected: PASS — all ~150 flash tests green, and no ServeMux route-conflict panic from the two new routes.

- [ ] **Step 8: Full check**

Run, in order:

```bash
go build ./...
gofmt -l .
go vet ./...
go mod tidy
go test ./... -race -count=1
```

Expected: `gofmt -l` prints nothing, `go mod tidy` produces no diff (no new dependency was added), everything else passes with no failures.

- [ ] **Step 9: Commit**

```bash
git add internal/apps/flash/handlers_media.go internal/apps/flash/handlers_media_test.go \
        internal/apps/flash/export_test.go internal/apps/flash/flash.go \
        internal/apps/flash/handlers_cards.go internal/apps/flash/templates/cards.html
git commit -m "feat(flash): add media serving, upload, and card UI"
```
