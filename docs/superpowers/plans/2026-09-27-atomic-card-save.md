# Atomic Card Save & In-Transaction Checks Implementation Plan (#363, #372)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Saving a card from the card form (text, tags, media) is one transaction, and the remaining ownership/visibility checks that ran before `BeginTx` run inside it.

**Architecture:** A new `Store.SaveCardForm` does the ownership check, card insert/update, tag replacement (incl. tag GC) and media attach/remove in one `*sql.Tx`, built from `tx`-taking helpers extracted from today's `CreateCard`, `SetCardTags` and `AttachCardUpload`. The create/update handlers make one call. Separately, reader's `canSeeItem` gains an executor parameter so `SaveFullArticle`/`ClearFullArticle` run it inside their transaction, and flash's `bumpDailyCounts` becomes a plain function.

**Tech Stack:** Go, SQLite (`SetMaxOpenConns(1)`), `internal/apptest`.

## Global Constraints

- Nothing inside an open transaction may use `st.db`/`s.db` — with `SetMaxOpenConns(1)` that deadlocks rather than failing. Helpers take a `dbExecutor` / `*sql.Tx`.
- Ownership/visibility checks run inside the transaction, never before `BeginTx` (#294, #288, #372).
- Error semantics are unchanged: not-your-deck/card → `ErrNotFound`; bad input → `ErrInvalid` (reported to the user via `userMessage`); anything else is a real error (500).
- A new file of a kind always wins over that kind's Remove flag (#328); Remove only applies when no new file came in for that kind.
- Validation (card fields, notes, tag names) happens before the transaction starts, as today.
- Stored timestamps use `formatTime` (`db.FormatTime`); clock reads go through `st.now()` (#357 arch test).
- Handler tests use `s.Clock`, never `s.Store.SetClock`.
- Full check (AGENTS.md) green on every commit: `gofmt -l .`, `go vet ./...`, `go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...`, `go mod tidy && git diff --exit-code go.mod go.sum`, `go test ./... -race -count=1`.
- Branch `fix/363-atomic-writes`. Commit messages end with a blank line then `Co-Authored-By: Claude <noreply@anthropic.com>`.

---

### Task 1: #372 — reader checks inside the tx; `bumpDailyCounts` plain function

**Files:**
- Modify: `internal/apps/reader/store.go` (`canSeeItem` ~1006, `SaveFullArticle` ~1381, `ClearFullArticle` ~1433, and the other `canSeeItem` callers)
- Modify: `internal/apps/flash/review.go` (`bumpDailyCounts` ~274 and its two callers ~137, ~211)
- Test: `internal/apps/reader/store_test.go` (or whichever reader test file already covers `SaveFullArticle`)

**Interfaces:**
- Produces: `func canSeeItem(ctx context.Context, q rowQuerier, userID, itemID int64) error` in reader (package-level), where `rowQuerier` is `interface{ QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row }` — reuse an existing equivalent interface in reader if one exists (grep `interface {` in `internal/apps/reader/*.go`); only add one if none does. `func bumpDailyCounts(ctx context.Context, tx *sql.Tx, userID, deckID int64, now time.Time, newDelta, reviewDelta, rating, ratingDelta int) error` in flash.

- [ ] **Step 1: Regression tests** (they should already pass; they pin behaviour the refactor must keep). If equivalents already exist, don't duplicate — note which tests cover it in the report. Otherwise add:
  - `TestSaveFullArticleRejectsAnItemTheUserCannotSee`: Bob (not subscribed) calls `SaveFullArticle` for Alice's feed item → `errors.Is(err, reader.ErrNotFound)`, and the item's `full_html` is still empty (read via `store.DB()`).
  - `TestClearFullArticleRejectsAnItemTheUserCannotSee`: same for `ClearFullArticle` after Alice saved a full article → `ErrNotFound`, Alice's `full_html` unchanged.
- [ ] **Step 2: Refactor `canSeeItem`** to the package-level function above. `SaveFullArticle` and `ClearFullArticle` call `canSeeItem(ctx, tx, ...)` as the first statement after `BeginTx` (delete the pre-tx call). Every other caller passes `s.db` (unchanged behaviour). Update the doc comment on `SaveFullArticle`/`ClearFullArticle`: one sentence saying the check runs in the tx so an unsubscribe can't land between check and write (#372).
- [ ] **Step 3: `bumpDailyCounts`** → plain function (drop the `st *Store` receiver; it only uses `tx`). Update both callers. Add to its doc comment: "A plain function, not a Store method, so nothing inside it can reach st.db while the caller's transaction holds the only connection (#372)."
- [ ] **Step 4: Run** `go test ./internal/apps/reader/ ./internal/apps/flash/ -race -count=1` — PASS.
- [ ] **Step 5: Commit** `fix(reader,flash): visibility check inside the tx; bumpDailyCounts takes only the tx (#372)`

---

### Task 2: #363 — `Store.SaveCardForm`

**Files:**
- Modify: `internal/apps/flash/card.go` (new `CardForm`, `CardUpload`, `SaveCardForm`; `CreateCard` becomes a wrapper; tx helpers)
- Modify: `internal/apps/flash/tag.go` (extract `replaceCardTags` from `SetCardTags`)
- Modify: `internal/apps/flash/media_store.go` (`AttachCardUpload` reuses the tx helper)
- Test: `internal/apps/flash/card_test.go` (create if absent; package `flash_test`, uses `newFixture` from `deck_test.go`, which exposes `f.store`, `f.db`, `f.alice`, `f.bob`)

**Interfaces:**
- Produces (exact):

```go
// CardUpload is one new image or sound for a card, already checked by the
// handler (size, sniffed content type).
type CardUpload struct {
	ContentType string
	Data        []byte
}

// CardForm is everything the card editor submits.
type CardForm struct {
	CardType, Front, Back, Notes string
	Tags                         []string
	Image, Audio                 *CardUpload // nil = no new file for that kind
	RemoveImage, RemoveAudio     bool        // ignored for a kind that has a new file (#328)
}

// SaveCardForm creates (cardID == 0) or updates one of userID's own cards in
// deckID from the card editor, replacing its tags and applying its media
// changes, all in one transaction (#363).
func (st *Store) SaveCardForm(ctx context.Context, userID, deckID, cardID int64, f CardForm) (Card, error)
```

- [ ] **Step 1: Failing store tests** in `card_test.go`:
  - `TestSaveCardFormCreatesCardTagsAndImage`: `SaveCardForm(ctx, alice, deck, 0, CardForm{CardType: flash.CardTypeBasic, Front: "hola", Back: "hello", Tags: []string{"verbs", "a1"}, Image: &flash.CardUpload{ContentType: "image/png", Data: pngBytes}})` → card has non-nil `ImageHash`; `TagsForCard` returns `a1`, `verbs`. (`pngBytes`: reuse the smallest PNG fixture already used in `handlers_media_test.go` or media store tests — grep for `\x89PNG`.)
  - `TestSaveCardFormUpdatesAndRemoves`: create with image + audio + tag `x`; then `SaveCardForm(..., c.ID, CardForm{..., Front: "adios", Tags: []string{"y"}, RemoveAudio: true})` → front updated, tags == [`y`], image kept, audio nil, and tag `x` is gone from `flash_tags` (GC).
  - `TestSaveCardFormNewFileWinsOverRemove`: `Image: new, RemoveImage: true` → image hash == new file's hash.
  - `TestSaveCardFormIsAtomic`: install a failing trigger on the fixture DB:

```go
if _, err := f.db.ExecContext(ctx, `CREATE TRIGGER fail_media BEFORE INSERT ON flash_media
	BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
	t.Fatal(err)
}
```

    then (a) create with `Tags: []string{"t"}` and an image → error; the deck has zero cards (`ListCards`) and Alice has no `flash_tags` rows; (b) with a card created before the trigger (front `"hola"`, tag `"old"`), update with `Front: "adios", Tags: []string{"new"}` and an image → error; `CardByID` still has front `"hola"` and `TagsForCard` is still [`old`].
  - `TestSaveCardFormOwnership`: Bob saving into Alice's deck (cardID 0) → `ErrNotFound`, no card created; Bob updating Alice's card → `ErrNotFound`, card unchanged; Alice updating her card with a `deckID` of her *other* deck → `ErrNotFound`.
  - `TestSaveCardFormValidates`: empty front → `errors.Is(err, flash.ErrInvalid)`; an over-long tag → `ErrInvalid`; nothing written.
  Run `go test ./internal/apps/flash/ -run SaveCardForm` → compile failure.

- [ ] **Step 2: Extract tx helpers** (unexported, package flash):
  - `insertCard(ctx, exec dbExecutor, c Card) (int64, error)` — the INSERT from `CreateCard`.
  - `updateCardRow(ctx, exec dbExecutor, userID, deckID, id int64, cardType, front, back, notes string) error` — the UPDATE from `UpdateCard`, returning `ErrNotFound` on 0 rows. `UpdateCard` calls it with `st.db`.
  - `cardByID(ctx, exec dbExecutor, userID, deckID, id int64) (Card, error)` — `CardByID`'s query; `CardByID` calls it with `st.db`.
  - `deckOwned(ctx, exec dbExecutor, userID, deckID int64) error` — `SELECT 1 FROM flash_decks WHERE id = ? AND user_id = ?`; `sql.ErrNoRows` → `ErrNotFound`, other errors wrapped (same shape as `cardOwner`).
  - `replaceCardTags(ctx, tx *sql.Tx, userID, cardID int64, names []string) error` — `SetCardTags`' DELETE-links + `upsertCardTags` + GC statement, moved verbatim with its comments. `SetCardTags` keeps its `cardOwner(ctx, tx, …)` check and calls `replaceCardTags`.
  - `attachUpload(ctx, exec dbExecutor, now time.Time, userID, deckID, cardID int64, kind string, u CardUpload) (string, error)` — `saveMediaUpload` + `setCardMedia`; `AttachCardUpload` calls it inside its own tx.

- [ ] **Step 3: Implement `SaveCardForm`**:
  1. Validate before the tx: `ValidateCard`, `validateCardNotes`, `ValidateTagNames(f.Tags)`.
  2. `BeginTx`; `defer tx.Rollback()`.
  3. `cardID == 0`: `deckOwned(ctx, tx, …)`, then `insertCard(ctx, tx, Card{…, CreatedAt: st.now()})`. Else: `updateCardRow(ctx, tx, …)` (its `WHERE id AND deck_id AND user_id` is the ownership check).
  4. `replaceCardTags(ctx, tx, userID, id, f.Tags)`.
  5. For image then audio: new upload → `attachUpload(ctx, tx, st.now(), …)`; else if remove → `setCardMedia(ctx, tx, …, nil)`.
  6. `cardByID(ctx, tx, …)`, `Commit`, return it.
  Doc comment: why one transaction (#363 — a failure at any step used to leave a half-saved card), and the "nothing here may use st.db" rule.
  `CreateCard` becomes `return st.SaveCardForm(ctx, userID, deckID, 0, CardForm{CardType: cardType, Front: front, Back: back, Notes: notes})` — this also moves its deck check inside a transaction.

- [ ] **Step 4: Run** `go test ./internal/apps/flash/ -race -count=1` — PASS (existing CreateCard/UpdateCard/SetCardTags/AttachCardUpload tests must still pass unchanged).
- [ ] **Step 5: Commit** `fix(flash): SaveCardForm writes card, tags and media in one transaction (#363)`

---

### Task 3: #363 — handlers use `SaveCardForm`

**Files:**
- Modify: `internal/apps/flash/handlers_cards.go` (`createCard` ~411, `updateCard` ~540)
- Modify: `internal/apps/flash/handlers_media.go` (`pendingUpload`, `cardUploads`, `readCardUploads`; delete `saveCardUploads`)
- Test: `internal/apps/flash/handlers_cards_test.go` (or the file holding the card-form handler tests)

**Interfaces:**
- Consumes: `CardForm`, `CardUpload`, `SaveCardForm` (Task 2).

- [ ] **Step 1: Failing handler test** `TestCardFormSaveIsAtomic`: build the server with `apptest.WithDatabase(handle)` (see `query_count_test.go` for how a test opens its own handle) so the test can install the same `fail_media` trigger as Task 2. POST a multipart new-card form (copy the multipart helper used by the existing image-upload handler tests in `handlers_media_test.go`) with front/back, `tags=verbs` and an image → response is 500; `s.Store.ListCards` for the deck is empty; Alice has no tag `verbs`. Then with a card created via `s.Store` beforehand, POST an edit with new front + image → 500 and the card's front is unchanged. Run → FAIL (today the card/text is written before the media fails).
- [ ] **Step 2: Handlers**: after validation, build `CardForm{CardType, Front, Back, Notes, Tags: tagList, Image, Audio, RemoveImage, RemoveAudio}` from the parsed form and uploads and call `a.store.SaveCardForm(r.Context(), userID, deck.ID, 0 /* or id */, form)`. Keep the existing `ErrInvalid` → re-render branch on the result; everything else → `a.fail`. In `updateCard`, the returned card replaces the separate `UpdateCard` + `SetCardTags` + `saveCardUploads` + `CardByID` sequence.
- [ ] **Step 3: Upload types**: make `cardUploads.Image/Audio` `*CardUpload`. If `pendingUpload.Kind` is used anywhere other than `saveCardUploads`, keep what's needed; otherwise delete `pendingUpload` and have the reader of one part return `*CardUpload`. Delete `saveCardUploads` and move its "#328 new file wins" comment to `SaveCardForm` if it isn't there already. Update comments that mention `saveCardUploads` / `AttachCardUpload` as the card form's path (grep both names in `internal/apps/flash/*.go`).
- [ ] **Step 4: Run** `go test ./internal/apps/flash/ -race -count=1` — PASS, including every existing card-form, media and #328 test unchanged.
- [ ] **Step 5: Full check**, then commit `fix(flash): card form saves through SaveCardForm (#363)`
