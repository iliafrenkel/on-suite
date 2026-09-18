# ON Flash F5 — Sharing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a deck's creator share it with another account on the instance; the recipient sees it in a "shared with me" list and adopts it, getting an independent copy whose review progress is entirely private, with later re-shares mergeable as "new cards only."

**Architecture:** One new table (`flash_shares`) tracks each share offer as a small state machine (`pending` → `adopted`/`declined`/`revoked`); one new nullable column (`flash_cards.origin_card_id`) records, on every copied card, which source card it came from, which is what lets a later merge tell "already adopted" from "new." All of this lives in one new file, `share.go` (store logic) plus `handlers_share.go` (HTTP) and template additions — no other existing file's SQL changes.

**Tech Stack:** Go, `database/sql` against SQLite (`modernc.org/sqlite`), the existing `internal/platform/app`/`internal/platform/web`/`internal/platform/render` stack, `apptest` for handler tests.

## Global Constraints

- Every store method that takes a `userID` and a deck/share ID must scope its query by that user, exactly like every existing Flash store method — "not found" and "not yours" stay indistinguishable (`ErrNotFound` for both), per `store.go`'s documented rule.
- `flash_shares.deck_id` uses `ON DELETE CASCADE` — deleting the source deck removes every share row referencing it (pending, adopted, declined, revoked) without touching any recipient's own `adopted_deck_id` deck, which is a separate row.
- `flash_cards.origin_card_id` uses `ON DELETE SET NULL` — deleting a source card later never touches an adopter's already-copied card, it only clears the now-meaningless back-reference.
- Adoption and merge never copy FSRS review state — every copied card starts brand new in the recipient's review queue, identical to a freshly imported card.
- Media is carried over by reference only (`image_hash`/`audio_hash` copied as-is) — never re-fetched, never duplicated in `flash_media`.
- A recipient's new deck from first-time adoption gets `new_cards_per_day = DefaultNewCardsPerDay` (from `deck.go`) and `reviews_per_day = NULL`/`snoozed_until = NULL` — never copied from the source deck's settings.
- Tags are copied by name via the existing `upsertCardTags(ctx, tx *sql.Tx, userID, cardID int64, names []string) error` helper (`tag.go`), which creates the tag under the recipient's account if they don't already have one with that name.
- `app.Deps.Users *auth.Store` is already delivered to Flash's `App.Mount` and stored as `a.deps.Users` — this is how handlers resolve a `to_user_id`/`from_user_id` into a username for display. No new plumbing, no cross-app import; `internal/platform/auth` is a platform package Flash is already allowed to import (like `internal/platform/app`, `internal/platform/web`).
- Migration file: `internal/apps/flash/migrations/0007_sharing.sql` (the last existing one is `0006_media.sql`).
- Route registration follows the exact HTMX-vs-redirect pattern in `handlers_decks.go`: on a plain request, `http.Redirect(w, r, path, http.StatusSeeOther)`; on an HTMX request, set `HX-Push-Url` (only where the URL actually changes) and call `a.renderDeckDetailWithList`/`a.renderDeckIndex`.
- Test fixture: `internal/apps/flash/deck_test.go`'s `newFixture(t) *fixture` already creates both `alice` and `bob` (`auth.User`, real IDs) against a real migrated SQLite file — reuse it directly in `share_test.go` (same `flash_test` package). `apptest.Server[S]`'s `s.Alice`/`s.Bob` sessions are the equivalent for handler tests.

---

### Task 1: Migration + share state machine (share, revoke, decline)

**Files:**
- Create: `internal/apps/flash/migrations/0007_sharing.sql`
- Create: `internal/apps/flash/share.go`
- Create: `internal/apps/flash/share_test.go`

**Interfaces:**
- Consumes: `Store` (`db *sql.DB`, `now func() time.Time`, `store.go`), `ErrNotFound`/`ErrInvalid` (`store.go`), `rowScanner` interface (`store.go`), `formatTime`/`parseTime` (`store.go`), `st.DeckByID(ctx, userID, id) (Deck, error)` (`deck.go`).
- Produces: `Share` struct, `ShareStatusPending`/`ShareStatusAdopted`/`ShareStatusDeclined`/`ShareStatusRevoked` constants, `(st *Store) ShareDeck(ctx, fromUserID, deckID, toUserID int64) (Share, error)`, `(st *Store) RevokeShare(ctx, fromUserID, shareID int64) error`, `(st *Store) DeclineShare(ctx, toUserID, shareID int64) error`. Task 2 (`AdoptShare`) and Task 3 (list queries) both build on these.

- [ ] **Step 1: Write the migration**

```sql
-- internal/apps/flash/migrations/0007_sharing.sql
-- One row per share offer from one account's deck to another. A deck can
-- have several simultaneous rows (one per recipient); a re-share to a
-- recipient who already adopted inserts a second pending row for the same
-- (deck_id, from_user_id, to_user_id) triple, which is what a merge offer
-- looks like — see share.go's AdoptShare for how the two are told apart.
CREATE TABLE flash_shares (
    id              INTEGER PRIMARY KEY,
    deck_id         INTEGER NOT NULL REFERENCES flash_decks (id) ON DELETE CASCADE,
    from_user_id    INTEGER NOT NULL,
    to_user_id      INTEGER NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('pending', 'adopted', 'declined', 'revoked')),
    -- adopted_deck_id is set only once this row is adopted: the recipient's
    -- own independent copy. NULL until then.
    adopted_deck_id INTEGER REFERENCES flash_decks (id),
    created_at      TEXT NOT NULL,
    responded_at    TEXT
) STRICT;

-- origin_card_id names the source card a copied card came from, set by
-- adoption/merge. ON DELETE SET NULL: deleting the original later never
-- touches the adopter's own copy, it only loses the back-reference.
ALTER TABLE flash_cards ADD COLUMN origin_card_id INTEGER REFERENCES flash_cards (id) ON DELETE SET NULL;
```

- [ ] **Step 2: Write the failing tests for the state machine**

```go
// internal/apps/flash/share_test.go
package flash_test

import (
	"context"
	"errors"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestShareDeckCreatesPendingShare(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sh.Status != flash.ShareStatusPending {
		t.Errorf("Status = %q, want %q", sh.Status, flash.ShareStatusPending)
	}
	if sh.DeckID != d.ID || sh.FromUserID != f.alice.ID || sh.ToUserID != f.bob.ID {
		t.Errorf("share = %+v, want deck %d alice->bob", sh, d.ID)
	}
}

func TestShareDeckRejectsSelfShare(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.alice.ID)
	if !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestShareDeckRejectsSomeoneElsesDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.store.ShareDeck(ctx, f.bob.ID, d.ID, f.alice.ID)
	if !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestShareDeckIsIdempotentWhilePending(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	first, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Errorf("re-sharing while pending created a second row: %d != %d", first.ID, second.ID)
	}
}

func TestRevokeShareRequiresPendingAndOwnership(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.RevokeShare(ctx, f.bob.ID, sh.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("revoke by non-owner: err = %v, want ErrNotFound", err)
	}
	if err := f.store.RevokeShare(ctx, f.alice.ID, sh.ID); err != nil {
		t.Fatal(err)
	}
	// revoking again (no longer pending) fails
	if err := f.store.RevokeShare(ctx, f.alice.ID, sh.ID); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("re-revoke: err = %v, want ErrInvalid", err)
	}
}

func TestDeclineShareThenReshareCreatesFreshOffer(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.DeclineShare(ctx, f.bob.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	// a declined share never blocks a fresh offer
	second, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Error("re-share after decline reused the declined row instead of creating a fresh one")
	}
	if second.Status != flash.ShareStatusPending {
		t.Errorf("Status = %q, want pending", second.Status)
	}
}

func TestDeleteDeckCascadesShares(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.DeleteDeck(ctx, f.alice.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	// the share row is gone: revoking it now reports ErrNotFound the same as
	// a bad ID would
	if err := f.store.RevokeShare(ctx, f.alice.ID, sh.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("revoke after cascade delete: err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/... -run TestShareDeck -v`
Expected: FAIL — `flash.ShareDeck`, `flash.Share`, `flash.ShareStatusPending` etc. undefined (share.go doesn't exist yet).

- [ ] **Step 4: Implement `share.go`**

```go
// internal/apps/flash/share.go
package flash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Share statuses. A row starts pending and moves to exactly one of the other
// three; it never moves again after that.
const (
	ShareStatusPending  = "pending"
	ShareStatusAdopted  = "adopted"
	ShareStatusDeclined = "declined"
	ShareStatusRevoked  = "revoked"
)

// Share is one share offer from one account's deck to another. Multiple rows
// can exist for the same (DeckID, FromUserID, ToUserID) triple over time: a
// re-share after adoption inserts a new pending row for the same triple,
// which AdoptShare (see the next task) recognizes as a merge offer by
// finding an earlier row for that triple already in ShareStatusAdopted.
type Share struct {
	ID            int64
	DeckID        int64
	FromUserID    int64
	ToUserID      int64
	Status        string
	AdoptedDeckID *int64
	CreatedAt     time.Time
	RespondedAt   *time.Time
}

// ShareDeck offers deckID (one of fromUserID's own) to toUserID. Re-sharing
// while an offer to the same recipient is still pending is a no-op that
// returns the existing row, so re-clicking Share doesn't create duplicate
// entries in the recipient's list; sharing again after that offer has been
// adopted, declined, or revoked always inserts a fresh row.
func (st *Store) ShareDeck(ctx context.Context, fromUserID, deckID, toUserID int64) (Share, error) {
	if fromUserID == toUserID {
		return Share{}, fmt.Errorf("%w: you can't share a deck with yourself", ErrInvalid)
	}
	if _, err := st.DeckByID(ctx, fromUserID, deckID); err != nil {
		return Share{}, err
	}

	existing, err := st.pendingShare(ctx, deckID, fromUserID, toUserID)
	switch {
	case err == nil:
		return existing, nil
	case !errors.Is(err, ErrNotFound):
		return Share{}, err
	}

	sh := Share{DeckID: deckID, FromUserID: fromUserID, ToUserID: toUserID, Status: ShareStatusPending, CreatedAt: st.now()}
	err = st.db.QueryRowContext(ctx,
		`INSERT INTO flash_shares (deck_id, from_user_id, to_user_id, status, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 RETURNING id`,
		sh.DeckID, sh.FromUserID, sh.ToUserID, sh.Status, formatTime(sh.CreatedAt),
	).Scan(&sh.ID)
	if err != nil {
		return Share{}, fmt.Errorf("flash: share deck: %w", err)
	}
	return sh, nil
}

func (st *Store) pendingShare(ctx context.Context, deckID, fromUserID, toUserID int64) (Share, error) {
	return scanShare(st.db.QueryRowContext(ctx,
		`SELECT id, deck_id, from_user_id, to_user_id, status, adopted_deck_id, created_at, responded_at
		 FROM flash_shares
		 WHERE deck_id = ? AND from_user_id = ? AND to_user_id = ? AND status = ?`,
		deckID, fromUserID, toUserID, ShareStatusPending))
}

// RevokeShare cancels one of fromUserID's own pending offers. Only a
// pending row can be revoked — an already-adopted, declined, or
// previously-revoked row returns ErrInvalid.
func (st *Store) RevokeShare(ctx context.Context, fromUserID, shareID int64) error {
	return st.resolveShare(ctx, shareID, fromUserID, "from_user_id", ShareStatusRevoked)
}

// DeclineShare dismisses one of toUserID's own pending offers without
// adopting it. A declined offer never blocks a later fresh ShareDeck call
// from the same creator to the same recipient.
func (st *Store) DeclineShare(ctx context.Context, toUserID, shareID int64) error {
	return st.resolveShare(ctx, shareID, toUserID, "to_user_id", ShareStatusDeclined)
}

// resolveShare moves a pending row (owned by userID via ownerColumn, either
// "from_user_id" or "to_user_id") to newStatus. It reports ErrNotFound if
// the row doesn't exist or isn't userID's, and ErrInvalid if it exists and
// is userID's but is no longer pending.
func (st *Store) resolveShare(ctx context.Context, shareID, userID int64, ownerColumn, newStatus string) error {
	var ownerID int64
	err := st.db.QueryRowContext(ctx,
		`SELECT `+ownerColumn+` FROM flash_shares WHERE id = ?`, shareID).Scan(&ownerID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return fmt.Errorf("flash: resolve share: %w", err)
	case ownerID != userID:
		return ErrNotFound
	}

	res, err := st.db.ExecContext(ctx,
		`UPDATE flash_shares SET status = ?, responded_at = ? WHERE id = ? AND status = ?`,
		newStatus, formatTime(st.now()), shareID, ShareStatusPending)
	if err != nil {
		return fmt.Errorf("flash: resolve share: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("flash: resolve share: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: this share has already been resolved", ErrInvalid)
	}
	return nil
}

func scanShare(row rowScanner) (Share, error) {
	var (
		sh              Share
		createdAt       string
		adoptedDeckID   sql.NullInt64
		respondedAt     sql.NullString
	)
	err := row.Scan(&sh.ID, &sh.DeckID, &sh.FromUserID, &sh.ToUserID, &sh.Status, &adoptedDeckID, &createdAt, &respondedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Share{}, ErrNotFound
	case err != nil:
		return Share{}, fmt.Errorf("flash: scan share: %w", err)
	}
	if sh.CreatedAt, err = parseTime(createdAt); err != nil {
		return Share{}, err
	}
	if adoptedDeckID.Valid {
		id := adoptedDeckID.Int64
		sh.AdoptedDeckID = &id
	}
	if respondedAt.Valid {
		t, err := parseTime(respondedAt.String)
		if err != nil {
			return Share{}, err
		}
		sh.RespondedAt = &t
	}
	return sh, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -run 'TestShareDeck|TestRevokeShare|TestDeclineShare|TestDeleteDeckCascadesShares' -v`
Expected: all PASS.

- [ ] **Step 6: Run the whole package's tests to check nothing else broke**

Run: `go test ./internal/apps/flash/...`
Expected: PASS (the migration adds a nullable column and a new table; no existing SELECT lists it, so nothing else is affected).

- [ ] **Step 7: Commit**

```bash
git add internal/apps/flash/migrations/0007_sharing.sql internal/apps/flash/share.go internal/apps/flash/share_test.go
git commit -m "feat(flash): F5 sharing — share/revoke/decline state machine"
```

---

### Task 2: Adoption and merge

**Files:**
- Modify: `internal/apps/flash/share.go`
- Modify: `internal/apps/flash/share_test.go`

**Interfaces:**
- Consumes: `Share`, `ShareStatus*` constants, `scanShare` (Task 1); `Deck`/`Card` structs, `DefaultNewCardsPerDay` (`deck.go`); `upsertCardTags(ctx context.Context, tx *sql.Tx, userID, cardID int64, names []string) error` (`tag.go`); `isUniqueViolation(err error) bool` (`store.go`).
- Produces: `(st *Store) AdoptShare(ctx context.Context, toUserID, shareID int64) (Deck, error)` — Task 3's list queries and Task 4's handler both call this.

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/apps/flash/share_test.go

func TestAdoptShareFirstTimeCopiesCardsTagsAndMedia(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "Basics")
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCardTags(ctx, f.alice.ID, c.ID, []string{"greetings"}); err != nil {
		t.Fatal(err)
	}
	hash, err := f.store.SaveMediaUpload(ctx, flash.MediaKindImage, "image/png", onePNG, f.now())
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
	newDeck, err := f.store.AdoptShare(ctx, f.bob.ID, sh.ID)
	if err != nil {
		t.Fatal(err)
	}

	if newDeck.UserID != f.bob.ID {
		t.Errorf("UserID = %d, want bob (%d)", newDeck.UserID, f.bob.ID)
	}
	if newDeck.Name != "Spanish" || newDeck.Description != "Basics" {
		t.Errorf("newDeck = %+v, want copied name/description", newDeck)
	}
	if newDeck.NewCardsPerDay != flash.DefaultNewCardsPerDay {
		t.Errorf("NewCardsPerDay = %d, want default %d", newDeck.NewCardsPerDay, flash.DefaultNewCardsPerDay)
	}
	if newDeck.ReviewsPerDay != nil || newDeck.SnoozedUntil != nil {
		t.Errorf("newDeck settings should be fresh, got %+v", newDeck)
	}

	cards, err := f.store.ListCards(ctx, f.bob.ID, newDeck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 {
		t.Fatalf("len(cards) = %d, want 1", len(cards))
	}
	got := cards[0]
	if got.Front != "hola" || got.Back != "hello" {
		t.Errorf("copied card = %+v, want alice's front/back", got)
	}
	if got.ImageHash == nil || *got.ImageHash != hash {
		t.Errorf("ImageHash = %v, want %q (shared by reference, not re-fetched)", got.ImageHash, hash)
	}

	tags, err := f.store.TagsForCard(ctx, f.bob.ID, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].Name != "greetings" {
		t.Fatalf("tags = %+v, want one tag %q under bob's own account", tags, "greetings")
	}

	updated, err := f.store.RevokeShare(ctx, f.alice.ID, sh.ID); _ = updated
	if !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("revoking an already-adopted share: err = %v, want ErrInvalid", err)
	}
}

func TestAdoptShareNeverCopiesReviewState(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, c.ID, flash.GradeGood, f.now()); err != nil {
		t.Fatal(err)
	}

	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	newDeck, err := f.store.AdoptShare(ctx, f.bob.ID, sh.ID)
	if err != nil {
		t.Fatal(err)
	}
	cards, err := f.store.ListCards(ctx, f.bob.ID, newDeck.ID)
	if err != nil {
		t.Fatal(err)
	}
	due, err := f.store.NewQueueCards(ctx, f.bob.ID, newDeck.ID, 10, f.now())
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].Card.ID != cards[0].ID {
		t.Errorf("bob's copy should be in the brand-new queue, got %+v", due)
	}
}

func TestAdoptShareMergeCopiesOnlyNewCards(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c1, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}

	sh1, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstDeck, err := f.store.AdoptShare(ctx, f.bob.ID, sh1.ID)
	if err != nil {
		t.Fatal(err)
	}
	// bob reviews his copy of c1 before the merge, to prove merge leaves it alone
	firstCards, err := f.store.ListCards(ctx, f.bob.ID, firstDeck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.bob.ID, firstCards[0].ID, flash.GradeGood, f.now()); err != nil {
		t.Fatal(err)
	}

	// alice adds a card and re-shares
	c2, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "adios", "goodbye", "")
	if err != nil {
		t.Fatal(err)
	}
	sh2, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}

	mergedDeck, err := f.store.AdoptShare(ctx, f.bob.ID, sh2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mergedDeck.ID != firstDeck.ID {
		t.Fatalf("merge should target the existing adopted deck (%d), got a new deck %d", firstDeck.ID, mergedDeck.ID)
	}

	cards, err := f.store.ListCards(ctx, f.bob.ID, mergedDeck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 {
		t.Fatalf("len(cards) = %d, want 2 (original + merged)", len(cards))
	}
	// bob's progress on the original card must be untouched: it's still due
	// on its post-grade schedule, not in the brand-new queue.
	due, err := f.store.NewQueueCards(ctx, f.bob.ID, mergedDeck.ID, 10, f.now())
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Fatalf("NewQueueCards len = %d, want 1 (only the freshly merged card c2, not the already-graded c1)", len(due))
	}
	_ = c1
	_ = c2
}

func TestAdoptShareRejectsWrongRecipientAndDoubleAdopt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.AdoptShare(ctx, f.alice.ID, sh.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("adopt by non-recipient: err = %v, want ErrNotFound", err)
	}
	if _, err := f.store.AdoptShare(ctx, f.bob.ID, sh.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AdoptShare(ctx, f.bob.ID, sh.ID); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("double adopt: err = %v, want ErrInvalid", err)
	}
}
```

Note: `f.now()` is `newFixture`'s clock accessor if one already exists in `deck_test.go`; if it doesn't, add `func (f *fixture) now() time.Time { return f.store.NowForTest() }` only if `Store` already exposes a test clock getter — check `export_test.go` first. If neither exists, use `time.Now().UTC()` directly in these tests instead of `f.now()` (grading and queue timing in these tests only need "now", not a controlled clock, since they don't assert on FSRS scheduling intervals).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/... -run TestAdoptShare -v`
Expected: FAIL — `AdoptShare` undefined.

- [ ] **Step 3: Implement `AdoptShare`, appended to `share.go`**

```go
// appended to internal/apps/flash/share.go

// shareCardSource is one card read from the source deck during adoption or
// merge — just the fields that get copied, plus the ID that becomes the new
// card's origin_card_id.
type shareCardSource struct {
	ID        int64
	CardType  string
	Front     string
	Back      string
	Notes     string
	ImageHash *string
	AudioHash *string
}

// AdoptShare resolves one of toUserID's own pending offers (shareID). If no
// earlier offer for the same (deck, creator, recipient) triple has ever been
// adopted, this is a first-time adoption: a brand-new deck is created for
// toUserID and every source card is copied into it. If an earlier offer for
// that triple was already adopted, this is a merge: cards are copied into
// that same existing deck, skipping any source card already represented
// there (by origin_card_id) — so cards and progress the recipient already
// has are never touched. Either way, no FSRS review state is ever copied:
// every copied card starts brand new in the recipient's review queue.
func (st *Store) AdoptShare(ctx context.Context, toUserID, shareID int64) (Deck, error) {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return Deck{}, fmt.Errorf("flash: adopt share: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	sh, err := scanShare(tx.QueryRowContext(ctx,
		`SELECT id, deck_id, from_user_id, to_user_id, status, adopted_deck_id, created_at, responded_at
		 FROM flash_shares WHERE id = ?`, shareID))
	if err != nil {
		return Deck{}, err
	}
	if sh.ToUserID != toUserID {
		return Deck{}, ErrNotFound
	}
	if sh.Status != ShareStatusPending {
		return Deck{}, fmt.Errorf("%w: this share has already been resolved", ErrInvalid)
	}

	src, err := deckByIDIgnoringOwner(ctx, tx, sh.DeckID)
	if err != nil {
		return Deck{}, err
	}

	priorAdoptedDeckID, err := priorAdoptedDeck(ctx, tx, sh.DeckID, sh.FromUserID, sh.ToUserID)
	if err != nil {
		return Deck{}, err
	}

	var targetDeckID int64
	if priorAdoptedDeckID != nil {
		targetDeckID = *priorAdoptedDeckID
	} else {
		err = tx.QueryRowContext(ctx,
			`INSERT INTO flash_decks (user_id, name, description, created_at)
			 VALUES (?, ?, ?, ?)
			 RETURNING id`,
			toUserID, src.Name, src.Description, formatTime(st.now()),
		).Scan(&targetDeckID)
		if err != nil {
			if isUniqueViolation(err) {
				return Deck{}, fmt.Errorf("%w: you already have a deck named %q", ErrInvalid, src.Name)
			}
			return Deck{}, fmt.Errorf("flash: adopt share: %w", err)
		}
	}

	existingOrigins, err := originCardIDsInDeck(ctx, tx, targetDeckID)
	if err != nil {
		return Deck{}, err
	}

	sourceCards, err := cardsInDeckIgnoringOwner(ctx, tx, sh.DeckID)
	if err != nil {
		return Deck{}, err
	}
	for _, sc := range sourceCards {
		if existingOrigins[sc.ID] {
			continue
		}
		tags, err := tagsForCardIgnoringOwner(ctx, tx, sc.ID)
		if err != nil {
			return Deck{}, err
		}

		var newCardID int64
		err = tx.QueryRowContext(ctx,
			`INSERT INTO flash_cards (deck_id, user_id, card_type, front, back, notes, created_at, image_hash, audio_hash, origin_card_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 RETURNING id`,
			targetDeckID, toUserID, sc.CardType, sc.Front, sc.Back, sc.Notes, formatTime(st.now()), sc.ImageHash, sc.AudioHash, sc.ID,
		).Scan(&newCardID)
		if err != nil {
			return Deck{}, fmt.Errorf("flash: adopt share: copy card: %w", err)
		}
		if err := upsertCardTags(ctx, tx, toUserID, newCardID, tags); err != nil {
			return Deck{}, err
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE flash_shares SET status = ?, adopted_deck_id = ?, responded_at = ? WHERE id = ?`,
		ShareStatusAdopted, targetDeckID, formatTime(st.now()), shareID); err != nil {
		return Deck{}, fmt.Errorf("flash: adopt share: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Deck{}, fmt.Errorf("flash: adopt share: %w", err)
	}
	return st.DeckByID(ctx, toUserID, targetDeckID)
}

// deckByIDIgnoringOwner reads a deck regardless of who owns it — used only
// inside AdoptShare, where the recipient legitimately needs to read the
// source deck's name/description despite not owning it, because they hold a
// valid pending share for it (checked by the caller before this is called).
func deckByIDIgnoringOwner(ctx context.Context, tx *sql.Tx, id int64) (Deck, error) {
	return scanDeck(tx.QueryRowContext(ctx,
		`SELECT id, user_id, name, description, created_at, new_cards_per_day, reviews_per_day, snoozed_until
		 FROM flash_decks WHERE id = ?`, id))
}

// cardsInDeckIgnoringOwner reads every card in deckID regardless of owner —
// same justification as deckByIDIgnoringOwner.
func cardsInDeckIgnoringOwner(ctx context.Context, tx *sql.Tx, deckID int64) ([]shareCardSource, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, card_type, front, back, notes, image_hash, audio_hash
		 FROM flash_cards WHERE deck_id = ?`, deckID)
	if err != nil {
		return nil, fmt.Errorf("flash: cards in deck: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []shareCardSource
	for rows.Next() {
		var (
			sc        shareCardSource
			imageHash sql.NullString
			audioHash sql.NullString
		)
		if err := rows.Scan(&sc.ID, &sc.CardType, &sc.Front, &sc.Back, &sc.Notes, &imageHash, &audioHash); err != nil {
			return nil, fmt.Errorf("flash: cards in deck: %w", err)
		}
		if imageHash.Valid {
			sc.ImageHash = &imageHash.String
		}
		if audioHash.Valid {
			sc.AudioHash = &audioHash.String
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// tagsForCardIgnoringOwner reads cardID's tag names regardless of owner —
// same justification as deckByIDIgnoringOwner. Unlike TagsForCard, it does
// not scope by user_id: the source card belongs to the creator, not the
// recipient calling AdoptShare.
func tagsForCardIgnoringOwner(ctx context.Context, tx *sql.Tx, cardID int64) ([]string, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT t.name FROM flash_tags t
		 JOIN flash_card_tags ct ON ct.tag_id = t.id
		 WHERE ct.card_id = ?`, cardID)
	if err != nil {
		return nil, fmt.Errorf("flash: tags for card: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("flash: tags for card: %w", err)
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// originCardIDsInDeck returns the set of source-card IDs already
// represented in deckID (via origin_card_id) — cards in this set are
// skipped during a merge because the recipient already has them.
func originCardIDsInDeck(ctx context.Context, tx *sql.Tx, deckID int64) (map[int64]bool, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT origin_card_id FROM flash_cards WHERE deck_id = ? AND origin_card_id IS NOT NULL`, deckID)
	if err != nil {
		return nil, fmt.Errorf("flash: origin card ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("flash: origin card ids: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

// priorAdoptedDeck finds the most recent already-adopted share for the same
// (deck, creator, recipient) triple, if any. Its presence is what makes the
// current pending share a merge offer rather than a first-time adoption.
func priorAdoptedDeck(ctx context.Context, tx *sql.Tx, deckID, fromUserID, toUserID int64) (*int64, error) {
	var id sql.NullInt64
	err := tx.QueryRowContext(ctx,
		`SELECT adopted_deck_id FROM flash_shares
		 WHERE deck_id = ? AND from_user_id = ? AND to_user_id = ? AND status = ?
		 ORDER BY id DESC LIMIT 1`,
		deckID, fromUserID, toUserID, ShareStatusAdopted).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("flash: prior adopted deck: %w", err)
	case !id.Valid:
		// Not reachable given how rows are written (adopted_deck_id is
		// always set in the same statement that sets status=adopted), but
		// treated as "no prior adoption" rather than panicking on a nil
		// deref if it ever were.
		return nil, nil
	}
	v := id.Int64
	return &v, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -run TestAdoptShare -v`
Expected: all PASS. If `TestAdoptShareFirstTimeCopiesCardsTagsAndMedia` fails to compile because `onePNG`/`f.now()`/`SaveMediaUpload` names differ from what's actually in `media_store_test.go`/`deck_test.go`, check those files' real helper names first (`onePNG` is defined in `media_fetch_test.go` or `card_media_test.go` per F4) and adjust the test to use the real names — do not invent new fixture helpers when equivalent ones already exist.

- [ ] **Step 5: Run the whole package's tests**

Run: `go test ./internal/apps/flash/... -race -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/flash/share.go internal/apps/flash/share_test.go
git commit -m "feat(flash): F5 sharing — adopt and merge"
```

---

### Task 3: List queries for the UI

**Files:**
- Modify: `internal/apps/flash/share.go`
- Modify: `internal/apps/flash/share_test.go`

**Interfaces:**
- Consumes: `Share`, `scanShare`, `ShareStatusPending`/`ShareStatusAdopted` (Tasks 1-2).
- Produces: `(st *Store) SharesForDeck(ctx context.Context, fromUserID, deckID int64) ([]Share, error)`, `ShareOffer` struct, `(st *Store) SharesForRecipient(ctx context.Context, toUserID int64) ([]ShareOffer, error)`. Task 4's handlers call both to build the "Shared with" and "Shared with me" views.

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/apps/flash/share_test.go

func TestSharesForDeckListsAllOffersNewestFirst(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}

	shares, err := f.store.SharesForDeck(ctx, f.alice.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(shares) != 1 || shares[0].ID != sh.ID {
		t.Fatalf("shares = %+v, want [%+v]", shares, sh)
	}

	if _, err := f.store.SharesForDeck(ctx, f.bob.ID, d.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("SharesForDeck by non-owner: err = %v, want ErrNotFound", err)
	}
}

func TestSharesForRecipientDistinguishesFirstOfferFromMerge(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}
	sh1, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}

	offers, err := f.store.SharesForRecipient(ctx, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 || offers[0].PriorAdoptedDeckID != nil {
		t.Fatalf("first offer = %+v, want one offer with no prior adopted deck", offers)
	}
	if offers[0].DeckName != "Spanish" {
		t.Errorf("DeckName = %q, want %q", offers[0].DeckName, "Spanish")
	}

	if _, err := f.store.AdoptShare(ctx, f.bob.ID, sh1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "adios", "goodbye", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID); err != nil {
		t.Fatal(err)
	}

	offers, err = f.store.SharesForRecipient(ctx, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 {
		t.Fatalf("offers after adopt+reshare = %+v, want exactly the new pending merge offer", offers)
	}
	if offers[0].PriorAdoptedDeckID == nil {
		t.Fatal("merge offer should report a prior adopted deck")
	}
	if offers[0].NewCardCount != 1 {
		t.Errorf("NewCardCount = %d, want 1 (only the newly added card)", offers[0].NewCardCount)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/... -run 'TestSharesForDeck|TestSharesForRecipient' -v`
Expected: FAIL — `SharesForDeck`/`SharesForRecipient`/`ShareOffer` undefined.

- [ ] **Step 3: Implement, appended to `share.go`**

```go
// appended to internal/apps/flash/share.go

// SharesForDeck lists every share offer (any status) fromUserID has made
// for one of their own decks, newest first — the creator's "Shared with"
// list.
func (st *Store) SharesForDeck(ctx context.Context, fromUserID, deckID int64) ([]Share, error) {
	if _, err := st.DeckByID(ctx, fromUserID, deckID); err != nil {
		return nil, err
	}
	rows, err := st.db.QueryContext(ctx,
		`SELECT id, deck_id, from_user_id, to_user_id, status, adopted_deck_id, created_at, responded_at
		 FROM flash_shares
		 WHERE deck_id = ? AND from_user_id = ?
		 ORDER BY created_at DESC, id DESC`, deckID, fromUserID)
	if err != nil {
		return nil, fmt.Errorf("flash: shares for deck: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Share
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// ShareOffer is one pending share addressed to a recipient, enriched with
// what the "shared with me" list needs to display: the source deck's name,
// and — if a merge offer — how many source cards the recipient doesn't have
// yet.
type ShareOffer struct {
	Share
	DeckName string
	// PriorAdoptedDeckID is nil for a first-time offer ("New deck") and
	// non-nil for a merge offer (an earlier offer for the same triple was
	// already adopted into that deck).
	PriorAdoptedDeckID *int64
	// NewCardCount is meaningful only when PriorAdoptedDeckID is non-nil.
	NewCardCount int
}

// SharesForRecipient lists every pending offer addressed to toUserID,
// newest first — the recipient's "shared with me" list.
func (st *Store) SharesForRecipient(ctx context.Context, toUserID int64) ([]ShareOffer, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT s.id, s.deck_id, s.from_user_id, s.to_user_id, s.status, s.adopted_deck_id, s.created_at, s.responded_at,
		        d.name,
		        (SELECT ad.adopted_deck_id FROM flash_shares ad
		          WHERE ad.deck_id = s.deck_id AND ad.from_user_id = s.from_user_id AND ad.to_user_id = s.to_user_id
		            AND ad.status = ?
		          ORDER BY ad.id DESC LIMIT 1)
		   FROM flash_shares s
		   JOIN flash_decks d ON d.id = s.deck_id
		  WHERE s.to_user_id = ? AND s.status = ?
		  ORDER BY s.created_at DESC, s.id DESC`,
		ShareStatusAdopted, toUserID, ShareStatusPending)
	if err != nil {
		return nil, fmt.Errorf("flash: shares for recipient: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ShareOffer
	for rows.Next() {
		var (
			o                  ShareOffer
			createdAt          string
			adoptedDeckID      sql.NullInt64
			respondedAt        sql.NullString
			priorAdoptedDeckID sql.NullInt64
		)
		err := rows.Scan(&o.ID, &o.DeckID, &o.FromUserID, &o.ToUserID, &o.Status, &adoptedDeckID, &createdAt, &respondedAt,
			&o.DeckName, &priorAdoptedDeckID)
		if err != nil {
			return nil, fmt.Errorf("flash: shares for recipient: %w", err)
		}
		if o.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if adoptedDeckID.Valid {
			id := adoptedDeckID.Int64
			o.AdoptedDeckID = &id
		}
		if respondedAt.Valid {
			t, err := parseTime(respondedAt.String)
			if err != nil {
				return nil, err
			}
			o.RespondedAt = &t
		}
		if priorAdoptedDeckID.Valid {
			id := priorAdoptedDeckID.Int64
			o.PriorAdoptedDeckID = &id
			n, err := st.newCardCount(ctx, o.DeckID, id)
			if err != nil {
				return nil, err
			}
			o.NewCardCount = n
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// newCardCount counts source deck cards not yet represented (by
// origin_card_id) in targetDeckID — computed fresh each time rather than
// stored, so it's always accurate even as more cards are adopted or deleted
// between offers.
func (st *Store) newCardCount(ctx context.Context, sourceDeckID, targetDeckID int64) (int, error) {
	var n int
	err := st.db.QueryRowContext(ctx,
		`SELECT count(*) FROM flash_cards sc
		 WHERE sc.deck_id = ?
		   AND NOT EXISTS (
		     SELECT 1 FROM flash_cards tc WHERE tc.deck_id = ? AND tc.origin_card_id = sc.id
		   )`, sourceDeckID, targetDeckID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("flash: new card count: %w", err)
	}
	return n, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -run 'TestSharesForDeck|TestSharesForRecipient' -v`
Expected: all PASS.

- [ ] **Step 5: Run the whole package's tests**

Run: `go test ./internal/apps/flash/... -race -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/flash/share.go internal/apps/flash/share_test.go
git commit -m "feat(flash): F5 sharing — list queries for the UI"
```

---

### Task 4: HTTP handlers, routes, and templates

**Files:**
- Create: `internal/apps/flash/handlers_share.go`
- Create: `internal/apps/flash/handlers_share_test.go`
- Modify: `internal/apps/flash/flash.go` (route registration)
- Modify: `internal/apps/flash/handlers_decks.go` (`deckDetailView`/`deckIndexView` gain the new fields; `viewDeckDetail`/`renderDeckIndex` populate them)
- Modify: `internal/apps/flash/templates/decks.html` (Share control, "Shared with" list, "Shared with me" list)

**Interfaces:**
- Consumes: `Share`, `ShareOffer`, `ShareStatusPending`/`ShareStatusAdopted`, `ShareDeck`/`RevokeShare`/`DeclineShare`/`AdoptShare`/`SharesForDeck`/`SharesForRecipient` (Tasks 1-3); `a.deps.Users *auth.Store` and `auth.Account`/`ListAccounts` (`internal/platform/auth`, already available via `app.Deps`); `a.userID`, `a.deckIDFromPath`, `a.fail`, `a.render`, `userMessage`, `deckDetailView`, `deckIndexView`, `a.renderDeckIndex`, `a.renderDeckDetailWithList`, `a.viewDeckDetail` (`handlers_decks.go`).
- Produces: nothing further downstream — this is the last task.

- [ ] **Step 1: Extend the view structs in `handlers_decks.go`**

Add two fields to `deckDetailView` (after `CSRFToken`) and one to `deckIndexView`:

```go
// deckDetailView, existing fields unchanged above; add:
	ShareRecipients []auth.Account      // every other account, for the Share dropdown
	SharedWith      []shareWithUsername // this deck's own share offers, for the creator's list

// deckIndexView, existing fields unchanged above; add:
	SharedWithMe []ShareOffer // pending offers addressed to the viewer
```

Add the import: `"github.com/iliafrenkel/on-suite/internal/platform/auth"` to `handlers_decks.go`'s import block.

`Share` (from `share.go`) only carries `ToUserID`, a bare integer — not enough to render a name in the "Shared with" list. `shareWithUsername` is a small display-only projection that pairs a `Share` with the resolved username, built once per request rather than widening `Share` itself (which stays a pure store type with no knowledge of usernames):

```go
// shareWithUsername is one row of the creator's "Shared with" list: a
// Share plus the recipient's username, resolved via a.deps.Users since
// Share itself only carries a bare user ID.
type shareWithUsername struct {
	Share
	ToUsername string
}
```

Add this helper right above `viewDeckDetail` — it loads everything the deck detail view's Share section needs: every other account on the instance (for the dropdown) and this deck's own share offers, each paired with its recipient's username (for the "Shared with" list):

```go
func (a *App) shareContext(ctx context.Context, userID, deckID int64) ([]auth.Account, []shareWithUsername, error) {
	accounts, err := a.deps.Users.ListAccounts(ctx)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[int64]string, len(accounts))
	others := make([]auth.Account, 0, len(accounts))
	for _, acc := range accounts {
		byID[acc.ID] = acc.Username
		if acc.ID != userID {
			others = append(others, acc)
		}
	}
	shares, err := a.store.SharesForDeck(ctx, userID, deckID)
	if err != nil {
		return nil, nil, err
	}
	withNames := make([]shareWithUsername, len(shares))
	for i, sh := range shares {
		withNames[i] = shareWithUsername{Share: sh, ToUsername: byID[sh.ToUserID]}
	}
	return others, withNames, nil
}
```

Update `viewDeckDetail` to accept and set the two new fields:

```go
func (a *App) viewDeckDetail(r *http.Request, userID int64, d Deck, recipients []auth.Account, sharedWith []shareWithUsername) deckDetailView {
	return deckDetailView{
		Mode: deckModeView, Deck: d, CSRFToken: web.CSRFToken(r.Context()),
		ShareRecipients: recipients, SharedWith: sharedWith,
	}
}
```

Every existing call site of `a.viewDeckDetail(r, d)` (in `deckIndex`, `createDeck`, `updateDeck`, `snoozeDeck`, `unsnoozeDeck` — all in `handlers_decks.go`) must now call `a.shareContext` first to build the two new arguments.

Then update every `a.viewDeckDetail(r, d)` call site to:

```go
recipients, shares, err := a.shareContext(r.Context(), userID, d.ID)
if err != nil {
	a.deps.Errors.Internal(w, r, err)
	return
}
detail := a.viewDeckDetail(r, userID, d, recipients, shares)
```

(replacing whatever local variable name each call site previously assigned `a.viewDeckDetail(r, d)`'s result to — `deckIndex`, `createDeck`, `updateDeck`, `snoozeDeck`, and `unsnoozeDeck` each call it once, right before their final render call).

Update `renderDeckIndex` to populate `SharedWithMe`:

```go
func (a *App) renderDeckIndex(w http.ResponseWriter, r *http.Request, userID int64, status int, detail deckDetailView) {
	items, err := a.deckListItems(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	offers, err := a.store.SharesForRecipient(r.Context(), userID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		view := deckIndexView{List: deckListFragment{Items: items, ActiveID: detail.Deck.ID, OOB: true}, Detail: detail, SharedWithMe: offers}
		page := a.deps.Page(r, deckPageTitle(detail))
		view.Title, view.Shell = page.Title, page.Shell
		if err := a.deps.Render.Fragment(w, http.StatusOK, "flash/decks", "deck-detail-with-list", view); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	view := deckIndexView{List: deckListFragment{Items: items, ActiveID: detail.Deck.ID}, Detail: detail, SharedWithMe: offers}
	page := a.deps.Page(r, deckPageTitle(detail))
	page.Data = view
	a.render(w, r, status, "flash/decks", page)
}
```

Apply the identical `SharedWithMe: offers` addition to `renderDeckDetailWithList` (fetch `offers` the same way, right after `items`, before building `view`).

- [ ] **Step 2: Write the failing handler tests**

```go
// internal/apps/flash/handlers_share_test.go
package flash_test

import (
	"net/url"
	"strconv"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
)

func newShareServer(t *testing.T) *apptest.Server[*flash.Store] {
	t.Helper()
	return apptest.NewServer(t, flash.New(), flash.NewStore)
}

func createDeckHX(t *testing.T, s *apptest.Server[*flash.Store], sess *apptest.Session, name string) int64 {
	t.Helper()
	rec := s.PostHX(t, sess, "/flash/new", url.Values{"name": {name}, "description": {""}})
	if rec.Code != 201 {
		t.Fatalf("create deck: %d; body: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("HX-Push-Url")
	if loc == "" {
		t.Fatalf("create deck: no HX-Push-Url header")
	}
	parts := loc[len("/flash/"):]
	id, err := strconv.ParseInt(parts, 10, 64)
	if err != nil {
		t.Fatalf("parse deck id from %q: %v", loc, err)
	}
	return id
}

func TestShareDeckHandlerCreatesOfferForRecipient(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")

	rec := s.PostHX(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})
	if rec.Code != 200 {
		t.Fatalf("share: %d; body: %s", rec.Code, rec.Body.String())
	}

	offers, err := s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 || offers[0].DeckName != "Spanish" {
		t.Fatalf("offers = %+v, want one offer for Spanish", offers)
	}
}

func TestShareDeckHandlerRejectsNonOwner(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")

	rec := s.PostHX(t, s.Bob, "/flash/"+strconv.FormatInt(deckID, 10)+"/share",
		url.Values{"to_user_id": {strconv.FormatInt(s.Alice.User.ID, 10)}})
	if rec.Code != 404 {
		t.Fatalf("share by non-owner: %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
}

func TestRevokeAndAdoptShareHandlers(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	deckIDStr := strconv.FormatInt(deckID, 10)

	s.PostHX(t, s.Alice, "/flash/"+deckIDStr+"/share", url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})
	offers, err := s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil || len(offers) != 1 {
		t.Fatalf("setup: offers = %+v, err = %v", offers, err)
	}
	shareIDStr := strconv.FormatInt(offers[0].ID, 10)

	// Bob adopts.
	rec := s.PostHX(t, s.Bob, "/flash/shared/"+shareIDStr+"/adopt", url.Values{})
	if rec.Code != 200 {
		t.Fatalf("adopt: %d; body: %s", rec.Code, rec.Body.String())
	}
	bobDecks, err := s.Store.ListDecks(t.Context(), s.Bob.User.ID)
	if err != nil || len(bobDecks) != 1 || bobDecks[0].Name != "Spanish" {
		t.Fatalf("bob's decks = %+v, err = %v", bobDecks, err)
	}

	// Revoking the now-adopted share fails.
	rec = s.PostHX(t, s.Alice, "/flash/"+deckIDStr+"/share/"+shareIDStr+"/revoke", url.Values{})
	if rec.Code != 400 {
		t.Fatalf("revoke adopted share: %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

func TestDeclineShareHandler(t *testing.T) {
	s := newShareServer(t)
	deckID := createDeckHX(t, s, s.Alice, "Spanish")
	s.PostHX(t, s.Alice, "/flash/"+strconv.FormatInt(deckID, 10)+"/share", url.Values{"to_user_id": {strconv.FormatInt(s.Bob.User.ID, 10)}})
	offers, err := s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil || len(offers) != 1 {
		t.Fatalf("setup: offers = %+v, err = %v", offers, err)
	}

	rec := s.PostHX(t, s.Bob, "/flash/shared/"+strconv.FormatInt(offers[0].ID, 10)+"/decline", url.Values{})
	if rec.Code != 200 {
		t.Fatalf("decline: %d; body: %s", rec.Code, rec.Body.String())
	}
	offers, err = s.Store.SharesForRecipient(t.Context(), s.Bob.User.ID)
	if err != nil || len(offers) != 0 {
		t.Fatalf("offers after decline = %+v, want none", offers)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/apps/flash/... -run 'TestShareDeckHandler|TestRevokeAndAdoptShareHandlers|TestDeclineShareHandler' -v`
Expected: FAIL — routes don't exist yet (404s / handler undefined at compile time for `flash.New` route registration once handlers_share.go references are added — actually this fails to compile first since handlers_share_test.go alone doesn't reference undefined symbols; the real failure is 404 on every request since routes aren't registered. Confirm by reading the actual output rather than assuming.)

- [ ] **Step 4: Implement `handlers_share.go`**

```go
// internal/apps/flash/handlers_share.go
package flash

import (
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// shareIDFromPath parses the {shareID} wildcard, used by the three
// recipient/creator actions that operate on a share row directly rather
// than through a deck.
func (a *App) shareIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("shareID"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// shareDeck handles POST /{deckID}/share: the creator offers deckID to
// to_user_id.
func (a *App) shareDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deckID, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	toUserID, err := strconv.ParseInt(r.PostFormValue("to_user_id"), 10, 64)
	if err != nil {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}

	if _, err := a.store.ShareDeck(r.Context(), userID, deckID, toUserID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("deck shared", "app", ID, "user_id", userID, "deck_id", deckID, "to_user_id", toUserID)

	d, err := a.store.DeckByID(r.Context(), userID, deckID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	recipients, shares, err := a.shareContext(r.Context(), userID, deckID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, a.viewDeckDetail(r, userID, d, recipients, shares))
}

// revokeShareHandler handles POST /{deckID}/share/{shareID}/revoke.
func (a *App) revokeShareHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deckID, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	shareID, ok := a.shareIDFromPath(w, r)
	if !ok {
		return
	}

	if err := a.store.RevokeShare(r.Context(), userID, shareID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("share revoked", "app", ID, "user_id", userID, "share_id", shareID)

	d, err := a.store.DeckByID(r.Context(), userID, deckID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	recipients, shares, err := a.shareContext(r.Context(), userID, deckID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, a.viewDeckDetail(r, userID, d, recipients, shares))
}

// declineShareHandler handles POST /shared/{shareID}/decline: the
// recipient dismisses a pending offer without adopting it.
func (a *App) declineShareHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	shareID, ok := a.shareIDFromPath(w, r)
	if !ok {
		return
	}
	if err := a.store.DeclineShare(r.Context(), userID, shareID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("share declined", "app", ID, "user_id", userID, "share_id", shareID)
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, deckDetailView{})
}

// adoptShareHandler handles POST /shared/{shareID}/adopt: the recipient
// adopts (first time) or merges (re-share) a pending offer.
func (a *App) adoptShareHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	shareID, ok := a.shareIDFromPath(w, r)
	if !ok {
		return
	}
	d, err := a.store.AdoptShare(r.Context(), userID, shareID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("share adopted", "app", ID, "user_id", userID, "share_id", shareID, "deck_id", d.ID)

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/flash/"+strconv.FormatInt(d.ID, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", "/flash/"+strconv.FormatInt(d.ID, 10))
	recipients, shares, err := a.shareContext(r.Context(), userID, d.ID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, a.viewDeckDetail(r, userID, d, recipients, shares))
}
```

- [ ] **Step 5: Register the routes in `flash.go`**

In `Mount`, right after the existing card routes and before `r.HandleFunc("GET /tags/{tagName}", a.tagFilter)`, add:

```go
	r.HandleFunc("POST /{deckID}/share", a.shareDeck)
	r.HandleFunc("POST /{deckID}/share/{shareID}/revoke", a.revokeShareHandler)
	r.HandleFunc("POST /shared/{shareID}/adopt", a.adoptShareHandler)
	r.HandleFunc("POST /shared/{shareID}/decline", a.declineShareHandler)
```

- [ ] **Step 6: Add the Share UI to `templates/decks.html`**

Add a Share section to `deck-detail-view`, right after the existing `<div class="row">` action-button block (Edit/View cards/Review/Delete) and before the snooze/unsnooze block:

```html
{{if .ShareRecipients}}
<div class="stack">
	<form method="post" action="/flash/{{.Deck.ID}}/share" hx-post="/flash/{{.Deck.ID}}/share" hx-target="#deck-detail" hx-swap="innerHTML">
		<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
		<div class="field">
			<label for="share-recipient-{{.Deck.ID}}">Share with</label>
			<select id="share-recipient-{{.Deck.ID}}" name="to_user_id">
				{{range .ShareRecipients}}<option value="{{.ID}}">{{.Username}}</option>{{end}}
			</select>
			<button type="submit" class="toolbar-btn">Share</button>
		</div>
	</form>
	{{if .SharedWith}}
	<ul class="tag-list">
		{{range .SharedWith}}
		<li>
			{{.ToUsername}} — {{.Status}}
			{{if eq .Status "pending"}}
			<form method="post" action="/flash/{{$.Deck.ID}}/share/{{.ID}}/revoke" hx-post="/flash/{{$.Deck.ID}}/share/{{.ID}}/revoke" hx-target="#deck-detail" hx-swap="innerHTML" style="display:inline">
				<input type="hidden" name="{{csrfField}}" value="{{$.CSRFToken}}">
				<button type="submit" class="toolbar-btn">Revoke</button>
			</form>
			{{end}}
		</li>
		{{end}}
	</ul>
	{{end}}
</div>
{{end}}
```

(`.SharedWith` here is `[]shareWithUsername`, defined and populated in Step 1 above — `.ToUsername` is its resolved display name, not the bare `ToUserID` a plain `Share` would only offer.)

Add a "Shared with me" section to the `content` block, right after the existing `<div class="row list-head">` block and before `{{template "deck-list-items" .Data.List}}`:

```html
{{if .Data.SharedWithMe}}
<div class="stack">
	<h3>Shared with me</h3>
	<ul class="tag-list">
		{{range .Data.SharedWithMe}}
		<li>
			{{.DeckName}} — {{if .PriorAdoptedDeckID}}{{.NewCardCount}} new card{{if ne .NewCardCount 1}}s{{end}} to merge{{else}}new deck{{end}}
			<form method="post" action="/flash/shared/{{.ID}}/adopt" hx-post="/flash/shared/{{.ID}}/adopt" hx-target="#deck-detail" hx-swap="innerHTML" style="display:inline">
				<input type="hidden" name="{{csrfField}}" value="{{$.Data.Detail.CSRFToken}}">
				<button type="submit" class="toolbar-btn toolbar-btn-active">{{if .PriorAdoptedDeckID}}Merge{{else}}Adopt{{end}}</button>
			</form>
			<form method="post" action="/flash/shared/{{.ID}}/decline" hx-post="/flash/shared/{{.ID}}/decline" hx-target="#deck-detail" hx-swap="innerHTML" style="display:inline">
				<input type="hidden" name="{{csrfField}}" value="{{$.Data.Detail.CSRFToken}}">
				<button type="submit" class="toolbar-btn">Dismiss</button>
			</form>
		</li>
		{{end}}
	</ul>
</div>
{{end}}
```

`{{$.Data.Detail.CSRFToken}}` reuses the CSRF token already carried by whichever deck (or empty) detail happens to be showing — `web.CSRFToken` returns the same per-request token regardless of which deck is selected, since it's tied to the session, not the deck, so this is safe even when `.Data.Detail` is the empty "select a deck" view.

- [ ] **Step 7: Run the handler tests to verify they pass**

Run: `go test ./internal/apps/flash/... -run 'TestShareDeckHandler|TestRevokeAndAdoptShareHandlers|TestDeclineShareHandler' -v`
Expected: all PASS.

- [ ] **Step 8: Run the whole suite**

Run: `go test ./... -race -count=1`
Expected: PASS — this also confirms the `deckDetailView`/`deckIndexView` signature changes didn't break any other existing test in `handlers_decks_test.go`, `handlers_cards_test.go`, `handlers_tags_test.go`, `handlers_review_test.go` (all of which render decks/cards pages and would fail to compile or render if a call site was missed).

- [ ] **Step 9: Run gofmt, vet, and the arch test**

```bash
gofmt -l .
go vet ./...
go test ./internal/arch/...
```
Expected: `gofmt -l .` prints nothing; `go vet` clean; arch test passes (no new cross-app import — `internal/platform/auth` is a platform package, already imported elsewhere in this app's dependency graph via `app.Deps`).

- [ ] **Step 10: Commit**

```bash
git add internal/apps/flash/handlers_share.go internal/apps/flash/handlers_share_test.go internal/apps/flash/flash.go internal/apps/flash/handlers_decks.go internal/apps/flash/templates/decks.html
git commit -m "feat(flash): F5 sharing — handlers, routes, and UI"
```

---

## Final Checks

- [ ] `go build ./...`
- [ ] `gofmt -l .` (no output)
- [ ] `go vet ./...`
- [ ] `go mod tidy` (no diff)
- [ ] `go test ./... -race -count=1`
- [ ] `go test ./internal/arch/...`
