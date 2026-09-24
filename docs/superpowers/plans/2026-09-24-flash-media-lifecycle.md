# ON Flash — Media lifecycle (ownership + orphan purge) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two changes to `flash_media`. First, the media route serves a hash only to someone whose own card uses it (#302.4). Second, a daily job deletes media that no card uses any more (#302.5), with no window in which it can delete a file that is about to be attached.

**Architecture:**
- A migration adds partial indexes on `flash_cards(image_hash)` and `flash_cards(audio_hash)`. All three queries that look a hash up from the card side use them: the route's ownership check, the purge's `NOT EXISTS`, and SQLite's own foreign-key check on each deleted `flash_media` row.
- `Store.PurgeOrphanMedia` is one `DELETE … WHERE NOT EXISTS`, modelled on Reader's `PurgeOrphanImages`. The app implements `app.Scheduler` with a daily job, "purge orphan media", that calls it.
- The race is closed with a transaction. Today, storing an upload and attaching it to the card are two separate statements, and the purge could run between them. A new `Store.AttachCardUpload` does both in one transaction. The card form's `saveCardUploads` is the only production code that stored an upload outside a transaction, and it switches to the new method. `ImportDeck` and `AdoptShare` already work inside transactions (see the table below).
- The route switches from `MediaByHash` to a new `Store.MediaForUser`: one query that finds the row only if an `EXISTS` over the viewer's own cards matches. Anything else gets the same `ErrNotFound` → 404 as an unknown hash.

**Tech Stack:** Go, SQLite (`modernc.org/sqlite`), `html/template`, HTMX.

**Issues:** [#302](https://github.com/iliafrenkel/on-suite/issues/302) items 4 and 5. Per the 2026-09-24 triage comment, 1 is obsolete, 3 duplicates #328, and 2 and 6 landed in #361. Specs: [F4 media](../specs/2026-09-18-on-flash-f4-media-design.md), [F5 sharing](../specs/2026-09-18-on-flash-f5-sharing-design.md).

**Decisions already made (by the user):**
1. **#302.5:** a **daily** background job, `PurgeOrphanMedia`, modelled on Reader's `PurgeOrphanImages`. It is a `NOT EXISTS` delete over `flash_media` rows that no `flash_cards.image_hash` / `audio_hash` references. No refcount and no admin button. The migration **only** adds the two indexes.
2. The purge must never break an attach that is in progress. The rule is to use the simplest approach that covers every attach path.
3. **#302.4:** `GET /flash/media/{hash}` serves a hash only if `EXISTS (SELECT 1 FROM flash_cards WHERE user_id=? AND (image_hash=? OR audio_hash=?))`. Otherwise it returns 404, the same response as a missing hash. Adopted decks keep working because adoption copies hashes into the recipient's own cards.
4. Record both rules in the specs. Tests work at store level, plus a test that the job is registered. Handler tests can't steer the app's clock.

**Choices this plan makes (flag in the PR, easy to change):**
- **Race fix: one transaction, not a grace period.** A grace period would need a new column, which decision 1 rules out. Two facts show why:
  - `fetched_at` is not a creation time. URL rows are inserted with it `NULL` (`media_store.go:84`). It is set only when the file is first viewed, or when a fetch fails (`:109`, `:121`). A purge that skipped "recent" rows would treat every unviewed import as ancient.
  - Both inserts are `INSERT OR IGNORE` (`:84`, `:98`). Suppose a file is re-uploaded, or a URL re-imported, after its old row was orphaned. The existing row keeps its old timestamp, so even a real `created_at` column would give no protection unless the inserts became upserts that touch it.

  The transaction is simpler, because the attach paths already work in transactions or are easy to wrap:

  | Path | Creates a `flash_media` row? | Attach | Status |
  |---|---|---|---|
  | Card form (create/update) → `saveCardUploads` | `SaveMediaUpload` | `SetCardMedia`, a **separate statement** | **the one gap**: now `AttachCardUpload`, one tx |
  | Import (`Image:`/`Audio:` URLs, JSON `image`/`audio`) | `ensureMediaURL(ctx, tx, …)` | `INSERT INTO flash_cards … image_hash` in the **same tx** | already safe |
  | Adoption / merge | none | copies `image_hash`/`audio_hash` from the source cards it reads **in the same tx** | already safe: those source cards reference the rows, so none is an orphan while the tx runs |
  | Lazy URL fetch (`FetchAndCacheMedia` → `SaveMediaBytes`) | none (an `UPDATE`) | — | harmless: if the row vanished, the `UPDATE` touches 0 rows and the fetched bytes are still served once |
  | `EnsureMediaURL` / `SaveMediaUpload` (exported, unattached) | yes | — | not called by production code after Task 2; tests use them to seed |

  The purge is a single statement, and SQLite runs write transactions one at a time. Flash also has just one connection (`db.go:40`). So the purge runs either wholly before an attach transaction or wholly after it, never between its insert and its attach. If it runs before, it can delete an old orphan with the same hash, and the transaction's `INSERT OR IGNORE` then simply re-creates the row.
- **Gift previews show no media, and that stays true.** The gift pane's sample cards are `flash-mini-card`s, which draw only the question (`card.partial.html:42-48`). So a pending gift never needs Alice's media URLs, and a test pins that.
- **The editor never previews an unsaved file.** `flash.js:163-166` shows only the chosen file's name ("the CSP's img-src 'self' rules out a blob: thumbnail"). The editor's `<img>`/`<audio>` show `Current`, the card's **saved** media (`handlers_cards.go:117-121`, `editor.partial.html:83,88`), which that card references by definition.
- **Partial indexes** (`WHERE … IS NOT NULL`). Most cards have no media, and a NULL is never looked up. I checked with `sqlite3` 3.54 and `EXPLAIN QUERY PLAN`. The route's `OR` query plans as `MULTI-INDEX OR` over both indexes. The purge's `NOT EXISTS` uses them, and the DELETE's own FK child lookups do too (`SEARCH c USING COVERING INDEX …`).
- **No `VACUUM`.** Reader doesn't run one either. A `DELETE` puts the freed pages on SQLite's freelist, and later writes reuse them. The file never shrinks by itself. Snapshots are already compact, because `onsuite backup` / the snapshot job use `VACUUM INTO` (`internal/platform/db/db.go:73-84`). The spec records this.
- **Browser cache.** Media responses stay `Cache-Control: private, max-age=86400`. So a browser that fetched an image before its card was deleted may keep showing it from its own cache for up to a day. That is acceptable. The server-side rule is what the ownership check enforces.

## Verified against current code (branch `fix/302-media-lifecycle` @ `071ad46`)

| What | Where |
|---|---|
| media route checks sign-in only (`_, ok := a.userID`), then `MediaByHash` | `internal/apps/flash/handlers_media.go:46-57` |
| `a.fail`: `ErrNotFound` → `Errors.Status(404)` | `internal/apps/flash/handlers_decks.go:42-51` |
| `saveCardUploads`: `SaveMediaUpload` then `SetCardMedia`, two statements | `handlers_media.go:237-262` (`:248`, `:252`) |
| its callers: create card, update card | `internal/apps/flash/handlers_cards.go:418`, `:543` |
| `dbExecutor` (`*sql.DB` or `*sql.Tx`); `mediaByHash(ctx, exec, hash)` | `internal/apps/flash/media_store.go:29-36`, `:43-70` |
| URL row insert: `INSERT OR IGNORE … (hash, kind, source_url)`, so `fetched_at` stays NULL | `media_store.go:81-89` |
| upload insert: `INSERT OR IGNORE … fetched_at` (not refreshed on conflict) | `media_store.go:95-104` |
| `fetched_at` also written on fetch success / failure | `media_store.go:107-116`, `:119-127` |
| `SetCardMedia` validates kind, updates by `id, deck_id, user_id`, 0 rows → `ErrNotFound` | `internal/apps/flash/card.go:177-205` |
| `ImportDeck`: `ensureMediaURL(ctx, tx, …)` + card insert in one tx | `internal/apps/flash/import_store.go:12-17`, `:44-66` |
| `AdoptShare`: one tx; copies `sc.ImageHash`/`sc.AudioHash` read in that tx | `internal/apps/flash/share.go:214-215`, `:275`, `:289-294`, `:349-352` |
| production callers of `EnsureMediaURL`: none (only `ensureMediaURL` from `ImportDeck`) | `grep -n EnsureMediaURL internal/apps/flash/*.go` |
| `flash_media` schema; FK `flash_cards.image_hash/audio_hash REFERENCES flash_media (hash)`; no index on them | `internal/apps/flash/migrations/0006_media.sql` |
| latest migration `0010_deck_color.sql`; no test lists migration names | `internal/apps/flash/migrations/`, `grep -rn 0010_ internal cmd` |
| single connection, FKs on | `internal/platform/db/db.go:21`, `:40` |
| Reader precedent: `PurgeOrphanImages` single `DELETE … NOT EXISTS`, returns count | `internal/apps/reader/store.go:1503-1517` |
| Reader `Jobs`: daily `purgeTick`, logs only when something was purged; `var _ app.Scheduler` | `internal/apps/reader/app.go:17-22`, `:127-176` (`:156-167`) |
| `app.Job{Name, Description, Every, Run}`; `app.Scheduler{Jobs(Deps) []Job}`; `RegisterJobs` after Mount | `internal/platform/app/app.go:283-316` |
| a job first runs one interval after start, never at boot | `internal/platform/jobs/jobs.go:91-92` |
| Reader's registration test: `reader.New().Jobs(app.Deps{})` without Mount | `internal/apps/reader/retention_test.go:12`, `:132-148` |
| Flash has no `Jobs`; `Mount` builds the app's **own** store | `internal/apps/flash/flash.go:39-44`, `:81-83` |
| every media URL comes from `mediaURL(hash)` | `handlers_cards.go:81-86`, used at `:120` and `card_view.go:36` |
| `newCardFace` callers: grid, opened card, review, gift samples, tag filter | `handlers_cards.go:151,192`, `handlers_review.go:204`, `handlers_share.go:199-200`, `handlers_tags.go:38` |
| `<img>`/`<audio>` rendered only by `flash-card` and `card-drop` | `templates/card.partial.html:23,34`; `templates/editor.partial.html:83,88` |
| gift samples use `flash-mini-card` (question only, no media) | `templates/gift.partial.html:30-32`; `card.partial.html:42-48` |
| editor shows only the chosen file's **name** (no blob preview under CSP) | `internal/apps/flash/static/flash.js:163-166` |
| test helpers | `newFixture` (`f.store`, `f.db`, `f.alice`, `f.bob`) `deck_test.go:28`; `newServer` `handlers_decks_test.go:17`; `newServerWithApp` `handlers_media_test.go:25`; `newShareServer`, `shareToBob` `handlers_share_test.go:16`, `:288`; `postCardForm` `handlers_editor_test.go:21`; `onePNG` `handlers_media_test.go:16`; `onePNG2` `handlers_editor_test.go:402`; `itoa` `handlers_decks_test.go:456` |
| partial-index plans (checked with `sqlite3` 3.54 `EXPLAIN QUERY PLAN`) | `MULTI-INDEX OR` for the route query; `SEARCH … USING COVERING INDEX` for the purge and the FK child checks |

## Global Constraints

- Branch: `fix/302-media-lifecycle` (already created off `main` at `071ad46`). Never push to `main`. Open a PR at the end.
- Full check before each Go commit:
  ```bash
  gofmt -l .                                              # must print nothing
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...  # pinned, not @latest
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./internal/arch/... -count=1
  go test ./... -race -count=1
  ```
- **HTMX behaviour unchanged.** No route, form field, target, swap, `HX-Push-Url` or template changes. The media route keeps the path, headers, ETag/304 behaviour and 404-on-failure it has today. The only intended difference is who gets a 200.
- **Migrations:** forward-only. The next number is `0011`. Start the file with a `-- internal/apps/flash/migrations/…` path comment and explain *why*, as `0010_deck_color.sql` does. This one only creates indexes: no table or column changes.
- No new dependencies. CSP: no inline `<script>`, no `style=`. No template or CSS changes are needed.
- `htmlassert` selectors: descendant chains of simple parts only (`tag`, `.class`, `#id`, `tag[attr=value]`, `tag.class`, `tag#id`). No compound parts like `a.x[y]`.
- **No handler test relies on `s.Store.SetClock`.** `flash.go:83` builds the app's own store, so the clock never reaches it. None of these changes need a clock anyway, because there is no grace period.
- Keep names: `MediaByHash`, `SaveMediaUpload`, `EnsureMediaURL`, `SetCardMedia` (exported and still used by tests), `saveCardUploads`, `cardUploads`, `pendingUpload`, `dbExecutor`, `rowScanner`, `mediaURL`.
- Follow Flash's own transaction style (`st.db.BeginTx` + `defer tx.Rollback()` + `dbExecutor` helpers, as in `ImportDeck` and `AdoptShare`), not Notes' `Ops`/`Store.Do`.
- Commits: Conventional Commits, scope `flash`, issue in the subject, e.g. `fix(flash): … (#302)`. Polish on shipped features is `fix`/`perf`/`refactor`/`test`/`docs`, not `feat`.

---

## File map

| File | Change |
|---|---|
| `internal/apps/flash/migrations/0011_card_media_indexes.sql` | **Create**: two partial indexes |
| `internal/apps/flash/media_store.go` | `PurgeOrphanMedia`; `saveMediaUpload(exec)`; `AttachCardUpload`; `mediaColumns`, `scanMedia`, `MediaForUser` |
| `internal/apps/flash/card.go` | `SetCardMedia` → wrapper over `setCardMedia(ctx, exec, …)` |
| `internal/apps/flash/handlers_media.go` | `saveCardUploads` uses `AttachCardUpload`; `media` uses `MediaForUser(userID, …)` |
| `internal/apps/flash/flash.go` | `mediaPurgeTick`; `Jobs`; `var _ app.Scheduler` |
| `docs/superpowers/specs/2026-09-18-on-flash-f4-media-design.md` | scope, serving, upload, cleanup, deferred list |
| `docs/superpowers/specs/2026-09-18-on-flash-f5-sharing-design.md` | media-by-reference bullet |
| Tests (all new files, `package flash_test`) | `media_purge_test.go`, `media_attach_test.go`, `jobs_test.go`, `handlers_media_owner_test.go` |

---

### Task 1: Index the card media columns and add `PurgeOrphanMedia` (#302.5)

**Files:**
- Create: `internal/apps/flash/migrations/0011_card_media_indexes.sql`
- Modify: `internal/apps/flash/media_store.go`
- Test: `internal/apps/flash/media_purge_test.go` (new)

**Interfaces:**
- Consumes: `Store.SaveMediaUpload`, `Store.EnsureMediaURL`, `Store.SetCardMedia`, `Store.MediaByHash`, `Store.DeleteCard`, `Store.DeleteDeck`, `Store.ShareDeck`, `Store.AdoptShare(ctx, to, shareID, nil)`.
- Produces:
  ```go
  func (st *Store) PurgeOrphanMedia(ctx context.Context) (int, error) // rows deleted
  ```
  Indexes: `flash_cards_image_hash_idx`, `flash_cards_audio_hash_idx`.

Nothing calls the purge in this task. Task 3 schedules it, after Task 2 closes the race, so no commit on this branch runs a purge that can interrupt an attach.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/flash/media_purge_test.go`:

```go
// internal/apps/flash/media_purge_test.go
package flash_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

// purgeCard is a deck and one card of Alice's for the purge tests.
func purgeCard(t *testing.T, f *fixture) (flash.Deck, flash.Card) {
	t.Helper()
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Animals", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "cat", "gato", "")
	if err != nil {
		t.Fatal(err)
	}
	return d, c
}

// mediaExists reports whether a flash_media row is still there.
func mediaExists(t *testing.T, f *fixture, hash string) bool {
	t.Helper()
	_, err := f.store.MediaByHash(context.Background(), hash)
	if errors.Is(err, flash.ErrNotFound) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true
}

// purge runs PurgeOrphanMedia and checks how many rows it deleted.
func purge(t *testing.T, f *fixture, want int) {
	t.Helper()
	n, err := f.store.PurgeOrphanMedia(context.Background())
	if err != nil {
		t.Fatalf("PurgeOrphanMedia: %v", err)
	}
	if n != want {
		t.Errorf("PurgeOrphanMedia deleted %d rows, want %d", n, want)
	}
}

// TestPurgeOrphanMediaDeletesOnlyWhatNoCardUses covers #302.5: uploads and
// URL rows alike go once no card's image_hash or audio_hash names them, and
// a row used as either column stays.
func TestPurgeOrphanMediaDeletesOnlyWhatNoCardUses(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	d, c := purgeCard(t, f)

	image, err := f.store.SaveMediaUpload(ctx, flash.MediaKindImage, "image/png", onePNG, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardMedia(ctx, f.alice.ID, d.ID, c.ID, flash.MediaKindImage, &image); err != nil {
		t.Fatal(err)
	}
	audio, err := f.store.EnsureMediaURL(ctx, flash.MediaKindAudio, "https://example.com/meow.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardMedia(ctx, f.alice.ID, d.ID, c.ID, flash.MediaKindAudio, &audio); err != nil {
		t.Fatal(err)
	}
	orphanUpload, err := f.store.SaveMediaUpload(ctx, flash.MediaKindImage, "image/png", onePNG2, now)
	if err != nil {
		t.Fatal(err)
	}
	orphanURL, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, "https://example.com/never-used.png")
	if err != nil {
		t.Fatal(err)
	}

	purge(t, f, 2)
	for _, h := range []string{image, audio} {
		if !mediaExists(t, f, h) {
			t.Errorf("media %s is still used by a card but was purged", h)
		}
	}
	for _, h := range []string{orphanUpload, orphanURL} {
		if mediaExists(t, f, h) {
			t.Errorf("media %s is used by no card but survived the purge", h)
		}
	}
	// Nothing left to do: a second run is a no-op.
	purge(t, f, 0)
}

// TestPurgeOrphanMediaFollowsReplaceRemoveAndDelete walks the four ways a
// row loses its last card: a replaced attachment, a removed one, a deleted
// card and a deleted deck.
func TestPurgeOrphanMediaFollowsReplaceRemoveAndDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	d, c := purgeCard(t, f)

	first, err := f.store.SaveMediaUpload(ctx, flash.MediaKindImage, "image/png", onePNG, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardMedia(ctx, f.alice.ID, d.ID, c.ID, flash.MediaKindImage, &first); err != nil {
		t.Fatal(err)
	}
	second, err := f.store.SaveMediaUpload(ctx, flash.MediaKindImage, "image/png", onePNG2, now)
	if err != nil {
		t.Fatal(err)
	}

	// Replace.
	if err := f.store.SetCardMedia(ctx, f.alice.ID, d.ID, c.ID, flash.MediaKindImage, &second); err != nil {
		t.Fatal(err)
	}
	purge(t, f, 1)
	if mediaExists(t, f, first) || !mediaExists(t, f, second) {
		t.Fatal("after a replace, the old image must go and the new one stay")
	}

	// Remove.
	if err := f.store.SetCardMedia(ctx, f.alice.ID, d.ID, c.ID, flash.MediaKindImage, nil); err != nil {
		t.Fatal(err)
	}
	purge(t, f, 1)
	if mediaExists(t, f, second) {
		t.Fatal("a removed image survived the purge")
	}

	// Delete the card.
	third, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, "https://example.com/cat.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardMedia(ctx, f.alice.ID, d.ID, c.ID, flash.MediaKindImage, &third); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteCard(ctx, f.alice.ID, d.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	purge(t, f, 1)
	if mediaExists(t, f, third) {
		t.Fatal("a deleted card's image survived the purge")
	}

	// Delete the deck (its cards go by ON DELETE CASCADE).
	c2, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "dog", "perro", "")
	if err != nil {
		t.Fatal(err)
	}
	fourth, err := f.store.EnsureMediaURL(ctx, flash.MediaKindAudio, "https://example.com/woof.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardMedia(ctx, f.alice.ID, d.ID, c2.ID, flash.MediaKindAudio, &fourth); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteDeck(ctx, f.alice.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	purge(t, f, 1)
	if mediaExists(t, f, fourth) {
		t.Fatal("a deleted deck's sound survived the purge")
	}
}

// TestPurgeOrphanMediaKeepsMediaAnAdoptedCopyStillUses: adoption shares a
// row by reference (F5 spec), so the sharer deleting her card must not take
// the file from under the recipient's copy.
func TestPurgeOrphanMediaKeepsMediaAnAdoptedCopyStillUses(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, c := purgeCard(t, f)
	hash, err := f.store.SaveMediaUpload(ctx, flash.MediaKindImage, "image/png", onePNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardMedia(ctx, f.alice.ID, d.ID, c.ID, flash.MediaKindImage, &hash); err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.store.AdoptShare(ctx, f.bob.ID, sh.ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.DeleteCard(ctx, f.alice.ID, d.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	purge(t, f, 0)
	if !mediaExists(t, f, hash) {
		t.Fatal("bob's adopted card still uses the image, but it was purged")
	}

	if err := f.store.DeleteDeck(ctx, f.bob.ID, res.Deck.ID); err != nil {
		t.Fatal(err)
	}
	purge(t, f, 1)
	if mediaExists(t, f, hash) {
		t.Fatal("no card uses the image any more, but it survived the purge")
	}
}

// TestCardMediaHashColumnsAreIndexed pins migration 0011: the purge, the
// media route's ownership check and SQLite's FK check on every flash_media
// delete all look a hash up from the card side.
func TestCardMediaHashColumnsAreIndexed(t *testing.T) {
	f := newFixture(t)
	for _, col := range []string{"image_hash", "audio_hash"} {
		var def string
		err := f.db.QueryRowContext(context.Background(),
			`SELECT sql FROM sqlite_master WHERE type = 'index' AND tbl_name = 'flash_cards' AND name = ?`,
			"flash_cards_"+col+"_idx").Scan(&def)
		if err != nil {
			t.Fatalf("index on flash_cards(%s): %v", col, err)
		}
		if !strings.Contains(def, col+" IS NOT NULL") {
			t.Errorf("index on %s = %q, want a partial index WHERE %s IS NOT NULL", col, def, col)
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'TestPurgeOrphanMedia|TestCardMediaHashColumnsAreIndexed' -count=1`
Expected: FAIL to compile with `f.store.PurgeOrphanMedia undefined (type *flash.Store has no field or method PurgeOrphanMedia)`.

- [ ] **Step 3: Write the migration**

Create `internal/apps/flash/migrations/0011_card_media_indexes.sql`:

```sql
-- internal/apps/flash/migrations/0011_card_media_indexes.sql
-- Indexes on the two columns that reference flash_media (#302). Three
-- lookups go from a hash to the cards that use it: the media route's "does
-- one of this viewer's cards use it" check, PurgeOrphanMedia's NOT EXISTS,
-- and SQLite's own foreign-key check on every flash_media row deleted,
-- which searches flash_cards for a child row — without an index, a full
-- table scan per deleted row.
--
-- Partial, because most cards have no media and a NULL is never looked up.
-- SQLite still uses a partial index for any `col = ?` term, since that
-- implies `col IS NOT NULL`.
CREATE INDEX flash_cards_image_hash_idx ON flash_cards (image_hash) WHERE image_hash IS NOT NULL;
CREATE INDEX flash_cards_audio_hash_idx ON flash_cards (audio_hash) WHERE audio_hash IS NOT NULL;
```

- [ ] **Step 4: Add `PurgeOrphanMedia`**

Append to `internal/apps/flash/media_store.go`:

```go
// PurgeOrphanMedia deletes cached images and sounds that no card uses any
// more: what is left behind by a replaced or removed attachment, a deleted
// card or deck, or a deleted account. It is one statement, mirroring
// internal/apps/reader's own PurgeOrphanImages (apps never import each
// other, so this is an independent implementation). A row shared by
// reference with an adopted copy stays as long as any card, anyone's, uses
// it (#302.5).
//
// SQLite does not give the space back to the filesystem on DELETE: the
// freed pages go on the database's freelist and later writes reuse them.
// Like Reader, this runs no VACUUM; snapshots are compact anyway, because
// they are taken with VACUUM INTO (internal/platform/db).
func (st *Store) PurgeOrphanMedia(ctx context.Context) (int, error) {
	res, err := st.db.ExecContext(ctx, `
		DELETE FROM flash_media
		 WHERE NOT EXISTS (SELECT 1 FROM flash_cards c
		                    WHERE c.image_hash = flash_media.hash
		                       OR c.audio_hash = flash_media.hash)`)
	if err != nil {
		return 0, fmt.Errorf("flash: purge orphan media: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("flash: purge orphan media: %w", err)
	}
	return int(n), nil
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/apps/flash/ -run 'TestPurgeOrphanMedia|TestCardMediaHashColumnsAreIndexed' -count=1 -v`
Expected: PASS for all four tests. Then run `go test ./internal/apps/flash/ -count=1`. Expected: `ok`, because the migration applies cleanly in every fixture.

- [ ] **Step 6: Full check and commit**

Run the full check from Global Constraints. Expected: `gofmt` prints nothing, and every package is `ok`.

```bash
git add internal/apps/flash/migrations/0011_card_media_indexes.sql internal/apps/flash/media_store.go internal/apps/flash/media_purge_test.go
git commit -m "fix(flash): add PurgeOrphanMedia and index card media hashes (#302)"
```

---

### Task 2: Store and attach an upload in one transaction (#302.5 race)

**Files:**
- Modify: `internal/apps/flash/media_store.go`, `internal/apps/flash/card.go`, `internal/apps/flash/handlers_media.go`
- Test: `internal/apps/flash/media_attach_test.go` (new)

**Interfaces:**
- Consumes: `dbExecutor`, `contentHash`, `formatTime`, `st.now`, `PurgeOrphanMedia` (Task 1).
- Produces:
  ```go
  // AttachCardUpload stores data (INSERT OR IGNORE by content hash) and sets
  // one of userID's own card's media columns to it, in one transaction.
  // kind is MediaKindImage or MediaKindAudio (else ErrInvalid); a card that
  // isn't userID's is ErrNotFound. Either error leaves no flash_media row.
  func (st *Store) AttachCardUpload(ctx context.Context, userID, deckID, cardID int64, kind, contentType string, data []byte) (string, error)

  func saveMediaUpload(ctx context.Context, exec dbExecutor, kind, contentType string, data []byte, now time.Time) (string, error)
  func setCardMedia(ctx context.Context, exec dbExecutor, userID, deckID, cardID int64, kind string, hash *string) error
  ```
  `SaveMediaUpload` and `SetCardMedia` keep their signatures and behaviour, as thin wrappers.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/flash/media_attach_test.go`:

```go
// internal/apps/flash/media_attach_test.go
package flash_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func mediaRowCount(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM flash_media`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAttachCardUploadStoresAndAttachesTogether(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, c := purgeCard(t, f)

	hash, err := f.store.AttachCardUpload(ctx, f.alice.ID, d.ID, c.ID, flash.MediaKindAudio, "audio/mpeg", []byte("fake mp3 bytes"))
	if err != nil {
		t.Fatalf("AttachCardUpload: %v", err)
	}
	got, err := f.store.CardByID(ctx, f.alice.ID, d.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AudioHash == nil || *got.AudioHash != hash {
		t.Errorf("AudioHash = %v, want %q", got.AudioHash, hash)
	}
	if got.ImageHash != nil {
		t.Errorf("ImageHash = %v, want nil (only audio was attached)", got.ImageHash)
	}
	m, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Cached() || string(m.Bytes) != "fake mp3 bytes" || m.ContentType != "audio/mpeg" || m.Kind != flash.MediaKindAudio {
		t.Errorf("stored media = %+v", m)
	}
}

// TestAttachCardUploadThatCannotAttachLeavesNoRow: the insert and the
// attach are one unit, so a failed attach leaves no orphan behind.
func TestAttachCardUploadThatCannotAttachLeavesNoRow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, c := purgeCard(t, f)

	// Bob can't attach to Alice's card.
	if _, err := f.store.AttachCardUpload(ctx, f.bob.ID, d.ID, c.ID, flash.MediaKindImage, "image/png", onePNG); !errors.Is(err, flash.ErrNotFound) {
		t.Fatalf("attach to someone else's card: err = %v, want ErrNotFound", err)
	}
	// There is no such media kind.
	if _, err := f.store.AttachCardUpload(ctx, f.alice.ID, d.ID, c.ID, "video", "video/mp4", onePNG); !errors.Is(err, flash.ErrInvalid) {
		t.Fatalf("unknown kind: err = %v, want ErrInvalid", err)
	}
	if n := mediaRowCount(t, f); n != 0 {
		t.Errorf("flash_media has %d rows after two failed attaches, want 0", n)
	}
}

// TestReattachingAnOrphanedFileSurvivesThePurge: uploading bytes whose row
// already exists as an orphan (the card that used it was deleted) is an
// INSERT OR IGNORE no-op, and the attach makes the old row used again.
func TestReattachingAnOrphanedFileSurvivesThePurge(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, c := purgeCard(t, f)
	orphan, err := f.store.SaveMediaUpload(ctx, flash.MediaKindImage, "image/png", onePNG, time.Now().UTC().Add(-48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	hash, err := f.store.AttachCardUpload(ctx, f.alice.ID, d.ID, c.ID, flash.MediaKindImage, "image/png", onePNG)
	if err != nil {
		t.Fatal(err)
	}
	if hash != orphan {
		t.Fatalf("same bytes produced a different hash: %q vs %q", hash, orphan)
	}
	purge(t, f, 0)
	if !mediaExists(t, f, hash) {
		t.Fatal("the re-attached file was purged")
	}
}

// TestPurgeRunningAlongsideAttachesNeverBreaksOne is the #302.5 race: with
// the purge running continuously, every attach must still succeed. When the
// store and the attach were two statements, the purge could take the one
// connection between them, delete the just-stored row, and the attach then
// failed its foreign key.
func TestPurgeRunningAlongsideAttachesNeverBreaksOne(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, c := purgeCard(t, f)

	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		for {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			if _, err := f.store.PurgeOrphanMedia(ctx); err != nil {
				done <- err
				return
			}
		}
	}()

	var last string
	for i := 0; i < 200; i++ {
		// A distinct file each time, so each attach inserts a fresh row and
		// orphans the previous one for the purge to chew on.
		data := append(bytes.Clone(onePNG), byte(i), byte(i>>8))
		hash, err := f.store.AttachCardUpload(ctx, f.alice.ID, d.ID, c.ID, flash.MediaKindImage, "image/png", data)
		if err != nil {
			close(stop)
			<-done
			t.Fatalf("attach %d with the purge running: %v", i, err)
		}
		last = hash
	}
	close(stop)
	if err := <-done; err != nil {
		t.Fatalf("PurgeOrphanMedia: %v", err)
	}
	if !mediaExists(t, f, last) {
		t.Fatal("the attached image was purged")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'TestAttachCardUpload|TestReattachingAnOrphanedFile|TestPurgeRunningAlongside' -count=1`
Expected: FAIL to compile with `f.store.AttachCardUpload undefined`.

*Evidence the race is real (optional; do not commit).* In `TestPurgeRunningAlongsideAttachesNeverBreaksOne`, temporarily replace the `AttachCardUpload` line with:
```go
		hash, err := f.store.SaveMediaUpload(ctx, flash.MediaKindImage, "image/png", data, time.Now().UTC())
		if err == nil {
			err = f.store.SetCardMedia(ctx, f.alice.ID, d.ID, c.ID, flash.MediaKindImage, &hash)
		}
```
Run it. Expected: `attach N with the purge running: flash: set card media: … FOREIGN KEY constraint failed`. Then revert the lines.

- [ ] **Step 3: Split `SetCardMedia` in `card.go`**

Replace the whole `SetCardMedia` function (`card.go:177-205`) with:

```go
// SetCardMedia sets or clears one of userID's own card's media hashes. hash
// of nil clears the attachment (e.g. a "remove image" request). It does not
// validate that hash names a real flash_media row — the foreign key does,
// and callers that create the row (ImportDeck, AttachCardUpload) do it in
// the same transaction as the attach, so PurgeOrphanMedia can never delete
// the row in between (#302.5).
func (st *Store) SetCardMedia(ctx context.Context, userID, deckID, cardID int64, kind string, hash *string) error {
	return setCardMedia(ctx, st.db, userID, deckID, cardID, kind, hash)
}

// setCardMedia is SetCardMedia against either the handle or a caller's open
// transaction (see dbExecutor).
func setCardMedia(ctx context.Context, exec dbExecutor, userID, deckID, cardID int64, kind string, hash *string) error {
	var column string
	switch kind {
	case MediaKindImage:
		column = "image_hash"
	case MediaKindAudio:
		column = "audio_hash"
	default:
		return fmt.Errorf("%w: %q is not a media kind I know", ErrInvalid, kind)
	}
	res, err := exec.ExecContext(ctx,
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

- [ ] **Step 4: Split `SaveMediaUpload` and add `AttachCardUpload` in `media_store.go`**

Replace the whole `SaveMediaUpload` function (`media_store.go:91-104`, doc comment included) with:

```go
// SaveMediaUpload stores an uploaded file's bytes immediately, keyed by the
// content's own hash, and returns that hash. Safe to call repeatedly for
// identical bytes: a second call is a no-op (INSERT OR IGNORE), so
// uploading the same file twice reuses one row.
//
// The row it leaves is attached to nothing, so the next PurgeOrphanMedia
// may delete it. The card form uses AttachCardUpload, which stores and
// attaches in one transaction. This is kept for tests that seed media.
func (st *Store) SaveMediaUpload(ctx context.Context, kind, contentType string, data []byte, now time.Time) (string, error) {
	return saveMediaUpload(ctx, st.db, kind, contentType, data, now)
}

func saveMediaUpload(ctx context.Context, exec dbExecutor, kind, contentType string, data []byte, now time.Time) (string, error) {
	hash := contentHash(data)
	if _, err := exec.ExecContext(ctx,
		`INSERT OR IGNORE INTO flash_media (hash, kind, content_type, bytes, fetched_at) VALUES (?, ?, ?, ?, ?)`,
		hash, kind, contentType, data, formatTime(now)); err != nil {
		return "", fmt.Errorf("flash: save media upload: %w", err)
	}
	return hash, nil
}

// AttachCardUpload stores an uploaded file and points one of userID's own
// card's image or audio column at it, in one transaction, and returns the
// file's hash.
//
// The single transaction is what keeps PurgeOrphanMedia safe (#302.5): as
// two statements, a purge landing between them deleted the just-stored
// row — or an orphan with the same bytes, which INSERT OR IGNORE had left
// in place — and the attach then failed its foreign key. SQLite runs one
// write transaction at a time (and flash has one connection), so the purge
// now runs wholly before this, where the INSERT re-creates anything it
// deleted, or wholly after, when the card already uses the row.
//
// kind must be MediaKindImage or MediaKindAudio (else ErrInvalid); a card
// that isn't userID's is ErrNotFound. Either way nothing is stored.
func (st *Store) AttachCardUpload(ctx context.Context, userID, deckID, cardID int64, kind, contentType string, data []byte) (string, error) {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("flash: attach upload: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	hash, err := saveMediaUpload(ctx, tx, kind, contentType, data, st.now())
	if err != nil {
		return "", err
	}
	if err := setCardMedia(ctx, tx, userID, deckID, cardID, kind, &hash); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("flash: attach upload: %w", err)
	}
	return hash, nil
}
```

In the `PurgeOrphanMedia` doc comment added in Task 1, insert this paragraph before the "SQLite does not give the space back" paragraph:

```go
// It needs no grace period for a file that is still being attached: every
// path that creates a row attaches it in the same transaction
// (AttachCardUpload for the card form, ImportDeck for import-time URLs),
// and AdoptShare creates none — it copies hashes from cards that exist, and
// so are in use, inside its own transaction. EnsureMediaURL and
// SaveMediaUpload store without attaching; only tests call them.
//
```

- [ ] **Step 5: Use it from `saveCardUploads` in `handlers_media.go`**

In `saveCardUploads`, replace:

```go
		case m.up != nil:
			hash, err := a.store.SaveMediaUpload(ctx, m.up.Kind, m.up.ContentType, m.up.Data, a.store.now())
			if err != nil {
				return err
			}
			if err := a.store.SetCardMedia(ctx, userID, deckID, cardID, m.kind, &hash); err != nil {
				return err
			}
```

with:

```go
		case m.up != nil:
			// One transaction for store + attach, so the daily orphan purge
			// can never delete the file in between (#302.5).
			if _, err := a.store.AttachCardUpload(ctx, userID, deckID, cardID, m.kind, m.up.ContentType, m.up.Data); err != nil {
				return err
			}
```

Also update the doc comment above `saveCardUploads`. Replace:

```go
// saveCardUploads applies checked uploads to a card that now exists: a new
// file of a kind always wins over that kind's Remove flag (#328) — Remove
// only takes effect when no new file came in for that kind.
```

with:

```go
// saveCardUploads applies checked uploads to a card that now exists: a new
// file of a kind always wins over that kind's Remove flag (#328) — Remove
// only takes effect when no new file came in for that kind. Each new file
// is stored and attached in one transaction (AttachCardUpload).
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/apps/flash/ -run 'TestAttachCardUpload|TestReattachingAnOrphanedFile|TestPurgeRunningAlongside|TestSetCardMedia|TestSaveMediaUpload' -race -count=1 -v`
Expected: PASS.

Then run `go test ./internal/apps/flash/ -race -count=1`. Expected: `ok`. The editor tests (`TestCreateCardWithImageInOneForm`, `TestNewImageWinsOverRemoveFlag`, `TestNewAudioWinsOverRemoveFlag`, `TestUpdateCardCanRemoveItsImage`, …) cover `saveCardUploads` end to end, unchanged.

- [ ] **Step 7: Full check and commit**

```bash
git add internal/apps/flash/media_store.go internal/apps/flash/card.go internal/apps/flash/handlers_media.go internal/apps/flash/media_attach_test.go
git commit -m "fix(flash): store and attach an uploaded file in one transaction (#302)"
```

---

### Task 3: Run the purge daily (#302.5)

**Files:**
- Modify: `internal/apps/flash/flash.go`
- Test: `internal/apps/flash/jobs_test.go` (new)

**Interfaces:**
- Consumes: `Store.PurgeOrphanMedia`, `app.Job`, `app.Scheduler`, `a.deps.Log`.
- Produces:
  ```go
  const mediaPurgeTick = 24 * time.Hour
  func (a *App) Jobs(deps app.Deps) []app.Job // one job: "purge orphan media"
  ```
  `app.Registry.RegisterJobs` picks it up by type assertion (`internal/platform/app/app.go:306-316`). It then shows on the admin page's job list, which is read-only and has no button.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/flash/jobs_test.go`:

```go
// internal/apps/flash/jobs_test.go
package flash_test

import (
	"errors"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

const purgeJobName = "purge orphan media"

// TestFlashRegistersTheDailyMediaPurge mirrors Reader's
// TestReaderRegistersBothJobs: Jobs must be callable before Mount (the
// closure only reads the store when it runs).
func TestFlashRegistersTheDailyMediaPurge(t *testing.T) {
	var sched app.Scheduler = flash.New()
	jobs := sched.Jobs(app.Deps{})
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1: %+v", len(jobs), jobs)
	}
	j := jobs[0]
	if j.Name != purgeJobName {
		t.Errorf("Name = %q, want %q", j.Name, purgeJobName)
	}
	if j.Every != 24*time.Hour {
		t.Errorf("Every = %v, want daily", j.Every)
	}
	if j.Description == "" {
		t.Error("the admin page lists jobs by description; it is empty")
	}
	if j.Run == nil {
		t.Error("the job has no Run function")
	}
}

// TestMediaPurgeJobRunsAgainstTheAppsOwnStore runs the registered job on a
// mounted app: it must purge through the store Mount built, over the same
// database the harness's s.Store writes to.
func TestMediaPurgeJobRunsAgainstTheAppsOwnStore(t *testing.T) {
	s, a := newServerWithApp(t)
	ctx := t.Context()

	orphan, err := s.Store.SaveMediaUpload(ctx, flash.MediaKindImage, "image/png", onePNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	deck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Animals", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "cat", "gato", "")
	if err != nil {
		t.Fatal(err)
	}
	kept, err := s.Store.AttachCardUpload(ctx, s.Alice.User.ID, deck.ID, c.ID, flash.MediaKindImage, "image/png", onePNG2)
	if err != nil {
		t.Fatal(err)
	}

	var job app.Job
	for _, j := range a.Jobs(app.Deps{}) {
		if j.Name == purgeJobName {
			job = j
		}
	}
	if job.Run == nil {
		t.Fatalf("no %q job", purgeJobName)
	}
	if err := job.Run(ctx); err != nil {
		t.Fatalf("job: %v", err)
	}

	if _, err := s.Store.MediaByHash(ctx, orphan); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("orphan after the job: err = %v, want ErrNotFound", err)
	}
	if _, err := s.Store.MediaByHash(ctx, kept); err != nil {
		t.Errorf("attached image after the job: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'TestFlashRegistersTheDailyMediaPurge|TestMediaPurgeJobRunsAgainstTheAppsOwnStore' -count=1`
Expected: FAIL to compile, because `flash.New()` has no `Jobs` method: `cannot use flash.New() (value of type *flash.App) as app.Scheduler value in variable declaration: *flash.App does not implement app.Scheduler (missing method Jobs)` and `a.Jobs undefined`.

- [ ] **Step 3: Implement in `flash.go`**

Replace the import block:

```go
import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)
```

with:

```go
import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// ON Flash owns background work (the daily media purge). Checked at compile
// time, as internal/apps/reader does for its own optional capabilities.
var _ app.Scheduler = (*App)(nil)
```

Insert after `func New() *App { return &App{} }`:

```go
// mediaPurgeTick is daily, like internal/apps/reader's purgeTick: orphaned
// media is housekeeping, not something anybody is waiting on.
const mediaPurgeTick = 24 * time.Hour

// Jobs implements app.Scheduler. RegisterJobs runs after Mount, so the
// store this closure reads is already built by the time the job first runs.
func (a *App) Jobs(deps app.Deps) []app.Job {
	return []app.Job{
		{
			Name:        "purge orphan media",
			Description: "Deletes cached card images and sounds that no card uses any more.",
			Every:       mediaPurgeTick,
			Run: func(ctx context.Context) error {
				n, err := a.store.PurgeOrphanMedia(ctx)
				if err != nil {
					return err
				}
				if n > 0 {
					a.deps.Log.Info("flash purged orphan media", "count", n)
				}
				return nil
			},
		},
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/apps/flash/ -run 'TestFlashRegistersTheDailyMediaPurge|TestMediaPurgeJobRunsAgainstTheAppsOwnStore' -count=1 -v`
Expected: PASS. Then run `go test ./internal/arch/... ./internal/platform/app/... ./cmd/onsuite/... -count=1`. Expected: `ok`, with the new import in `flash.go` inside the rules.

- [ ] **Step 5: Full check and commit**

```bash
git add internal/apps/flash/flash.go internal/apps/flash/jobs_test.go
git commit -m "fix(flash): purge orphaned media daily (#302)"
```

---

### Task 4: Serve media only to users whose own cards use it (#302.4)

**Files:**
- Modify: `internal/apps/flash/media_store.go`, `internal/apps/flash/handlers_media.go`
- Test: `internal/apps/flash/handlers_media_owner_test.go` (new)

**Interfaces:**
- Consumes: `rowScanner` (`store.go:61-63`), `parseTime`, `a.userID`, `a.fail`, `Store.AttachCardUpload` (Task 2).
- Produces:
  ```go
  // MediaForUser is MediaByHash, but only for a hash one of userID's own
  // cards uses as its image or its sound; anything else is ErrNotFound.
  func (st *Store) MediaForUser(ctx context.Context, userID int64, hash string) (Media, error)
  const mediaColumns = "hash, kind, content_type, bytes, source_url, fetched_at, error_count, last_error"
  func scanMedia(row rowScanner) (Media, error)
  ```
  `MediaByHash` keeps its signature. It stays unscoped, and only tests use it once the route switches.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/flash/handlers_media_owner_test.go`:

```go
// internal/apps/flash/handlers_media_owner_test.go
package flash_test

import (
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

// aliceCardWithImage creates a deck and a card of Alice's with onePNG
// attached as the card's image, and returns them with the media path.
func aliceCardWithImage(t *testing.T, s *apptest.Server[*flash.Store]) (flash.Deck, flash.Card, string) {
	t.Helper()
	ctx := t.Context()
	deck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Animals", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "cat", "gato", "")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := s.Store.AttachCardUpload(ctx, s.Alice.User.ID, deck.ID, c.ID, flash.MediaKindImage, "image/png", onePNG)
	if err != nil {
		t.Fatal(err)
	}
	return deck, c, "/flash/media/" + hash
}

func getMedia(t *testing.T, s *apptest.Server[*flash.Store], sess *apptest.Session, path string) *httptest.ResponseRecorder {
	t.Helper()
	return s.Do(t, sess, httptest.NewRequest(http.MethodGet, path, nil))
}

// TestMediaIsServedOnlyToSomeoneWhoseCardUsesIt covers #302.4. Another
// account's file must look exactly like a hash that doesn't exist, so the
// route can't be used to learn what someone else has attached.
func TestMediaIsServedOnlyToSomeoneWhoseCardUsesIt(t *testing.T) {
	s := newServer(t)
	_, _, path := aliceCardWithImage(t, s)

	if rec := getMedia(t, s, s.Alice, path); rec.Code != http.StatusOK {
		t.Fatalf("alice, whose card uses it = %d, want 200", rec.Code)
	}

	notYours := getMedia(t, s, s.Bob, path)
	if notYours.Code != http.StatusNotFound {
		t.Fatalf("bob, who has no card using it = %d, want 404", notYours.Code)
	}
	missing := getMedia(t, s, s.Bob, "/flash/media/"+strings.Repeat("0", 64))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown hash = %d, want 404", missing.Code)
	}
	if notYours.Body.String() != missing.Body.String() {
		t.Error("someone else's media must get the same 404 body as a hash that doesn't exist")
	}
	for _, h := range []string{"ETag", "Cache-Control", "X-Content-Type-Options"} {
		if got := notYours.Header().Get(h); got != missing.Header().Get(h) {
			t.Errorf("header %s = %q for someone else's media, %q for a missing hash", h, got, missing.Header().Get(h))
		}
	}
}

// TestMediaNoCardUsesIsNotServedEvenToItsUploader: a row waiting for the
// daily purge (its card was deleted) is nobody's any more.
func TestMediaNoCardUsesIsNotServedEvenToItsUploader(t *testing.T) {
	s := newServer(t)
	hash, err := s.Store.SaveMediaUpload(t.Context(), flash.MediaKindImage, "image/png", onePNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if rec := getMedia(t, s, s.Alice, "/flash/media/"+hash); rec.Code != http.StatusNotFound {
		t.Errorf("media no card uses = %d, want 404", rec.Code)
	}
}

// TestAdoptedDeckMediaIsServedToTheRecipient: a gift isn't Bob's until he
// adopts it. Adoption copies Alice's hashes into Bob's own cards, so from
// then on the file is his too, and stays his after Alice deletes her card.
func TestAdoptedDeckMediaIsServedToTheRecipient(t *testing.T) {
	s := newShareServer(t)
	deck, card, path := aliceCardWithImage(t, s)
	shareID := shareToBob(t, s, deck.ID)

	// The pending gift's preview draws no media, so nothing legitimate
	// asks for the file before adoption.
	preview := s.Do(t, s.Bob, httptest.NewRequest(http.MethodGet, "/flash/shared/"+shareID, nil))
	if preview.Code != http.StatusOK {
		t.Fatalf("gift preview = %d", preview.Code)
	}
	if strings.Contains(preview.Body.String(), "/flash/media/") {
		t.Error("the gift preview links to media Bob can't see yet")
	}
	if rec := getMedia(t, s, s.Bob, path); rec.Code != http.StatusNotFound {
		t.Fatalf("bob before adopting = %d, want 404", rec.Code)
	}

	if rec := s.PostHX(t, s.Bob, "/flash/shared/adopt", url.Values{"share_id": {shareID}}); rec.Code != http.StatusOK {
		t.Fatalf("adopt = %d; body: %s", rec.Code, rec.Body.String())
	}
	if rec := getMedia(t, s, s.Bob, path); rec.Code != http.StatusOK {
		t.Fatalf("bob after adopting = %d, want 200", rec.Code)
	}

	if err := s.Store.DeleteCard(t.Context(), s.Alice.User.ID, deck.ID, card.ID); err != nil {
		t.Fatal(err)
	}
	if rec := getMedia(t, s, s.Bob, path); rec.Code != http.StatusOK {
		t.Errorf("bob after alice deleted her card = %d, want 200 (his copy still uses it)", rec.Code)
	}
	if rec := getMedia(t, s, s.Alice, path); rec.Code != http.StatusNotFound {
		t.Errorf("alice after deleting her card = %d, want 404", rec.Code)
	}
}

// TestEveryMediaURLTheOwnerIsShownIsServed: the editor's preview of the
// saved image, the opened card and the review card all draw the same
// /flash/media/ URL, and it serves.
func TestEveryMediaURLTheOwnerIsShownIsServed(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Animals", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := postCardForm(t, s, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/new",
		url.Values{"card_type": {"basic"}, "front": {"cat"}, "back": {"gato"}},
		map[string][]byte{"image": onePNG})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d; body: %s", rec.Code, rec.Body.String())
	}
	cards, err := s.Store.ListCards(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil || len(cards) != 1 {
		t.Fatalf("ListCards = %v, %v", cards, err)
	}
	c := cards[0]

	pages := []struct{ name, path, selector string }{
		{"editor preview", "/flash/" + itoa(deck.ID) + "/cards/edit/" + itoa(c.ID), "form#card-detail-edit img.flash-drop-preview"},
		{"opened card", "/flash/" + itoa(deck.ID) + "/cards/" + itoa(c.ID), "img.flash-card-image"},
		{"review card", "/flash/review/" + itoa(deck.ID), "#review-card img.flash-card-image"},
	}
	for _, p := range pages {
		doc := s.Get(t, s.Alice, p.path)
		src, ok := htmlassert.Attr(doc.MustHave(p.selector), "src")
		if !ok || !strings.HasPrefix(src, "/flash/media/") {
			t.Fatalf("%s: img src = %q", p.name, src)
		}
		got := getMedia(t, s, s.Alice, src)
		if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "image/png" {
			t.Errorf("%s: GET %s = %d %q, want 200 image/png", p.name, src, got.Code, got.Header().Get("Content-Type"))
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/apps/flash/ -run 'TestMediaIsServedOnlyToSomeoneWhoseCardUsesIt|TestMediaNoCardUsesIsNotServedEvenToItsUploader|TestAdoptedDeckMediaIsServedToTheRecipient|TestEveryMediaURLTheOwnerIsShownIsServed' -count=1`
Expected:
- FAIL: `TestMediaIsServedOnlyToSomeoneWhoseCardUsesIt` reports `bob, who has no card using it = 200, want 404`.
- FAIL: `TestMediaNoCardUsesIsNotServedEvenToItsUploader` reports `= 200, want 404`.
- FAIL: `TestAdoptedDeckMediaIsServedToTheRecipient` reports `bob before adopting = 200, want 404`.
- PASS: `TestEveryMediaURLTheOwnerIsShownIsServed`. It is a guard that must keep passing.

- [ ] **Step 3: Add `MediaForUser` in `media_store.go`**

Replace `mediaByHash` (the whole function, `media_store.go:43-70`) with:

```go
// mediaColumns is every flash_media column a Media carries, in scanMedia's
// order.
const mediaColumns = `hash, kind, content_type, bytes, source_url, fetched_at, error_count, last_error`

func mediaByHash(ctx context.Context, exec dbExecutor, hash string) (Media, error) {
	return scanMedia(exec.QueryRowContext(ctx,
		`SELECT `+mediaColumns+` FROM flash_media WHERE hash = ?`, hash))
}

// MediaForUser loads one media record, but only if one of userID's own
// cards uses it as its image or its sound. Anything else — a hash no card
// of theirs uses, or no such hash at all — is the same ErrNotFound, so the
// media route can't tell anyone what another account has attached
// (#302.4). A deck adopted from someone else counts: AdoptShare copies the
// sharer's hashes into the recipient's own cards.
func (st *Store) MediaForUser(ctx context.Context, userID int64, hash string) (Media, error) {
	return scanMedia(st.db.QueryRowContext(ctx,
		`SELECT `+mediaColumns+` FROM flash_media
		  WHERE hash = ?
		    AND EXISTS (SELECT 1 FROM flash_cards c
		                 WHERE c.user_id = ?
		                   AND (c.image_hash = flash_media.hash OR c.audio_hash = flash_media.hash))`,
		hash, userID))
}

func scanMedia(row rowScanner) (Media, error) {
	var (
		m         Media
		bytes     []byte
		sourceURL sql.NullString
		fetched   sql.NullString
	)
	err := row.Scan(&m.Hash, &m.Kind, &m.ContentType, &bytes, &sourceURL, &fetched, &m.ErrorCount, &m.LastError)
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
```

Replace `MediaByHash`'s doc comment:

```go
// MediaByHash loads one media record. An unknown hash is ErrNotFound.
```

with:

```go
// MediaByHash loads one media record, whoever's it is. An unknown hash is
// ErrNotFound. The media route uses MediaForUser instead; this is for
// tests and internal checks that aren't answering a viewer.
```

- [ ] **Step 4: Scope the route in `handlers_media.go`**

Replace the `media` doc comment and the start of its body:

```go
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
```

with:

```go
// media serves a card's image or audio clip, fetching and caching it on
// first request if it hasn't been fetched yet. The route takes a hash, never
// a URL — the only way a hash resolves is if ImportDeck or an upload
// recorded it, so there is no input here that makes this fetch something
// else.
//
// It serves a hash only to someone whose own card uses it (#302.4):
// flash_media is a content-addressed cache shared across accounts, and it
// holds files people picked off their own disks, not just public images.
// Someone else's file, one no card uses and one that doesn't exist all get
// the same 404. A recipient sees a shared deck's media once they adopt it,
// because adoption copies the hashes into their own cards.
func (a *App) media(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	hash := r.PathValue("hash")
	if !validMediaHash(hash) {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}

	m, err := a.store.MediaForUser(r.Context(), userID, hash)
```

The rest of the handler is unchanged.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/apps/flash/ -run 'TestMedia|TestAdoptedDeckMediaIsServedToTheRecipient|TestEveryMediaURLTheOwnerIsShownIsServed' -count=1 -v`
Expected: PASS. That includes the existing `handlers_media_test.go` tests: every one serves media to Alice, whose card `seedCardWithImage` attached it to.

Then run `go test ./internal/apps/flash/ -race -count=1`. Expected: `ok`.

- [ ] **Step 6: Full check and commit**

```bash
git add internal/apps/flash/media_store.go internal/apps/flash/handlers_media.go internal/apps/flash/handlers_media_owner_test.go
git commit -m "fix(flash): serve media only to users whose cards use it (#302)"
```

---

### Task 5: Record both rules in the specs

**Files:**
- Modify: `docs/superpowers/specs/2026-09-18-on-flash-f4-media-design.md`, `docs/superpowers/specs/2026-09-18-on-flash-f5-sharing-design.md`

**Interfaces:** none. This task is docs only.

- [ ] **Step 1: F4 Scope bullet**

In the F4 spec, replace:

```
- No orphan-cleanup job in F4 — a `flash_media` row can outlive every card
  that referenced it (after a delete or a replace); cleaning those up is a
  follow-up, not core scope.
```

with:

```
- No orphan-cleanup job in F4 — a `flash_media` row could outlive every
  card that referenced it (after a delete or a replace). Added later: see
  "Orphan cleanup" below (#302).
```

- [ ] **Step 2: F4 Serving step 2**

Replace:

```
2. Look up the `flash_media` row. Not found → 404.
```

with:

```
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
```

- [ ] **Step 3: F4 Manual upload**

Replace:

```
- The card's `image_hash`/`audio_hash` is then set to that hash, or
  cleared to `NULL` on a remove request.
```

with:

```
- The card's `image_hash`/`audio_hash` is then set to that hash, in the
  **same transaction** as the insert (`AttachCardUpload`), or cleared to
  `NULL` on a remove request. The single transaction is what lets the
  orphan purge run without a grace period (below).
```

- [ ] **Step 4: F4 Orphan cleanup section**

Insert a new section immediately before `## Testing`:

```
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
```

- [ ] **Step 5: F4 deferred list**

Replace:

```
- Orphan-media cleanup (Reader's `PurgeOrphanImages` equivalent).
```

with:

```
- ~~Orphan-media cleanup~~: done, see "Orphan cleanup" (#302).
```

- [ ] **Step 6: F5 media-by-reference bullet**

In the F5 spec, replace:

```
- Media (`image_hash`/`audio_hash`) is carried over by reference only — the
  content-addressed `flash_media` row is shared as-is, never re-fetched or
  duplicated, since it's immutable and keyed by hash.
```

with:

```
- Media (`image_hash`/`audio_hash`) is carried over by reference only — the
  content-addressed `flash_media` row is shared as-is, never re-fetched or
  duplicated, since it's immutable and keyed by hash. Because the hashes
  land on the recipient's own cards, the media route (which serves a hash
  only to someone whose own card uses it) serves them the files from the
  moment they adopt, not before. The row outlives the sharer's card for as
  long as the copy uses it (F4 spec, "Orphan cleanup"; #302).
```

- [ ] **Step 7: Commit**

```bash
git add docs/superpowers/specs/2026-09-18-on-flash-f4-media-design.md docs/superpowers/specs/2026-09-18-on-flash-f5-sharing-design.md
git commit -m "docs(flash): record media ownership and orphan purge rules (#302)"
```

---

### Task 6: Full check, manual verification, PR

- [ ] **Step 1: Full check**

```bash
gofmt -l .                                              # prints nothing
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
go mod tidy && git diff --exit-code go.mod go.sum
go test ./internal/arch/... -count=1
go test ./... -race -count=1
```
Expected: no output from `gofmt`, no diff, and `ok` for every package.

- [ ] **Step 2: Manual verification.** Build with `go build -o /tmp/onsuite-bin ./cmd/onsuite`, start the `onsuite` preview from `.claude/launch.json` (data dir `/tmp/onsuite-manual-verify`), and ask the user to sign in. It needs two accounts; make the second with `/tmp/onsuite-bin user add bob --data-dir /tmp/onsuite-manual-verify`.

1. As A, create a card with an image and a sound. The editor preview, the opened card and the review card all show them. The browser devtools Network tab shows `/flash/media/…` 200s.
2. Copy the image URL. As B, open it: you get the normal 404 page, the same as `/flash/media/` followed by 64 zeros.
3. As A, share the deck with B. As B, open the gift. The preview shows no images, and the image URL is still 404. Adopt it: the cards show the image and play the sound.
4. As A, replace the image and delete the card. As B, the copy still shows the original image.
5. Run the purge by hand. It is scheduled 24h out and has no button, so use the database directly: `sqlite3 /tmp/onsuite-manual-verify/onsuite.db "SELECT COUNT(*) FROM flash_media"`. Then check the plans:
   ```bash
   sqlite3 /tmp/onsuite-manual-verify/onsuite.db "EXPLAIN QUERY PLAN DELETE FROM flash_media WHERE NOT EXISTS (SELECT 1 FROM flash_cards c WHERE c.image_hash = flash_media.hash OR c.audio_hash = flash_media.hash)"
   ```
   Expected: `SEARCH c USING INDEX flash_cards_image_hash_idx` and `…audio_hash_idx`, under `MULTI-INDEX OR`, plus the FK child searches on the same indexes. Don't run the DELETE on the verify DB. `TestMediaPurgeJobRunsAgainstTheAppsOwnStore` covers the job itself.
6. `/admin/` (as admin) lists the "purge orphan media" job with a next run about 24h out. There are no console or CSP errors, and dark theme is unaffected (no UI changes).

- [ ] **Step 3: Open the PR**

```bash
git push -u origin fix/302-media-lifecycle
gh pr create --title "fix(flash): owner-scoped media serving and a daily orphan-media purge (#302)" --body "$(cat <<'EOF'
## Summary
- **Media is served only to someone whose own card uses it** (#302.4). `GET /flash/media/{hash}` now reads through `MediaForUser`, which finds the row only if an `EXISTS` over the viewer's own cards (`image_hash` or `audio_hash`) matches. Someone else's file, a file no card uses and an unknown hash all get the same 404, so the route can't be used to probe what another account has attached. Adopted decks keep working, because adoption copies the hashes into the recipient's own cards. A pending gift's preview never draws media, and a test pins that.
- **A daily "purge orphan media" job** (#302.5) calls `PurgeOrphanMedia`, a single `DELETE … WHERE NOT EXISTS` modelled on Reader's `PurgeOrphanImages`. There is no refcount and no admin button, but the admin page's job list shows it.
- **Migration 0011** only adds partial indexes on `flash_cards(image_hash)` and `(audio_hash)`. The route check, the purge and SQLite's FK check on each deleted row all use them (checked with `EXPLAIN QUERY PLAN`).
- **Race closed with a transaction, not a grace period.** Storing a card-form upload and attaching it were two statements, so a purge between them deleted the fresh row and the attach failed its FK (→ 500). `AttachCardUpload` now does both in one transaction. Import already did (`ImportDeck`'s tx), and adoption creates no rows. A grace period would have needed a new column: URL rows have `fetched_at` NULL until first viewed, and `INSERT OR IGNORE` leaves an old orphan's timestamp in place. A stress test runs the purge in a loop against 200 attaches.
- The F4 and F5 specs record both rules. No `VACUUM`, like Reader: freed pages are reused, and snapshots use `VACUUM INTO`.

No new dependencies, no template, CSS or HTMX changes.

Refs #302 (items 4 and 5; 1 obsolete and 3 a duplicate of #328 per triage; 2 and 6 in #361)

Plan: docs/superpowers/plans/2026-09-24-flash-media-lifecycle.md

## Test plan
- [ ] `go test ./... -race -count=1`
- [ ] Manual checklist from the plan's Task 6
EOF
)"
```

Use `Closes #302` instead of `Refs #302` only if the user confirms the issue's other items are all settled (see Self-review, "Open question").

---

## Self-review

- **Coverage:**
  - #302.5, the purge → Task 1. Store tests cover: upload and URL rows purged once unused; both columns count as a use; a second run is a no-op; replace, remove, card delete and deck delete each free a row; an adopted copy keeps a row alive until it is gone too. Also covered: both partial indexes exist.
  - #302.5, the race → Task 2. Tests cover store + attach as one unit (a failed attach leaves no row, for someone else's card or an unknown kind), re-attaching bytes whose row is an orphan, and a stress test of the purge looping against 200 attaches. There is an optional uncommitted step showing the two-statement version fails with `FOREIGN KEY constraint failed`.
  - #302.5, the schedule → Task 3. Tests cover: `Jobs` is callable before Mount, returns one daily job with a name, description and Run, and satisfies `app.Scheduler`; the mounted app's job purges through its own store.
  - #302.4 → Task 4. Tests cover: owner 200; a non-owner gets 404 with the body and headers of an unknown hash; an orphan is 404 even to its uploader; a gift is 404 before adoption and 200 after; the recipient keeps access after the sharer deletes her card; the sharer loses it. A guard test shows the editor preview, opened card and review card URLs all serve.
  - Specs → Task 5. PR → Task 6.
- **Race-fix choice:** every path that creates a `flash_media` row is listed in the "Choices" table with how it's covered. `EnsureMediaURL` has no production caller today (verified with grep). After Task 2 neither does `SaveMediaUpload`, and both doc comments say their rows are purgeable.
- **Placeholders:** none. Every step has complete code or an exact before/after replacement, plus exact commands and expected output.
- **Type consistency:**
  - `AttachCardUpload(ctx, userID, deckID, cardID int64, kind, contentType string, data []byte) (string, error)` is used identically in `saveCardUploads`, `media_attach_test.go`, `jobs_test.go` and `handlers_media_owner_test.go`.
  - `PurgeOrphanMedia(ctx) (int, error)` is used by the job and the tests.
  - `MediaForUser(ctx, userID int64, hash string) (Media, error)` is used by the route only.
  - `setCardMedia`/`saveMediaUpload` take `dbExecutor`, which `*sql.Tx` satisfies (it already carries `ensureMediaURL(ctx, tx, …)`).
  - `scanMedia(rowScanner)` takes `*sql.Row` from `QueryRowContext`.
  - New helpers `purgeCard`, `mediaExists`, `purge`, `mediaRowCount`, `aliceCardWithImage`, `getMedia` and the const `purgeJobName` don't collide with existing test helpers (checked against every `func` in `internal/apps/flash/*_test.go`). `onePNG`/`onePNG2` are the existing fixtures.
- **Ordering:** Task 1 adds the purge but nothing runs it. Task 2 closes the race. Only then does Task 3 schedule it, so no commit ships a purge that can break an attach. Task 4 depends on Task 2's `AttachCardUpload` for its test seeding only.
- **Harness gotcha respected:** no test sets a clock on the app's store. Task 3's job test reaches the app's own store through `newServerWithApp`'s returned `*flash.App`.
- **Open question (not a code change):** #302's 2026-09-17 comment adds item 8 ("Space key collides with `<audio controls>` on the review screen") and item 9 (uncapped `<img>`). Item 9 is done (`app.css:2805`, `:2987`). Item 8 still looks open: `flash.js`'s keydown handler has no `AUDIO` guard (`static/flash.js:266-278`), and the triage comment doesn't mention it. So the PR says `Refs #302`, not `Closes`, until the user decides whether item 8 gets its own issue.
