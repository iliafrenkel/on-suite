// internal/apps/flash/share_test.go
package flash_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

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

func TestAdoptShareFirstTimeCopiesCardsTagsAndMedia(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
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
	hash, err := f.store.SaveMediaUpload(ctx, flash.MediaKindImage, "image/png", onePNG, now)
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

	err = f.store.RevokeShare(ctx, f.alice.ID, sh.ID)
	if !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("revoking an already-adopted share: err = %v, want ErrInvalid", err)
	}
}

func TestAdoptShareNeverCopiesReviewState(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, c.ID, flash.RatingGood, now); err != nil {
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
	due, err := f.store.DueQueue(ctx, f.bob.ID, &newDeck.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].Card.ID != cards[0].ID || !due[0].IsNew {
		t.Errorf("bob's copy should be in the brand-new queue, got %+v", due)
	}
}

func TestAdoptShareMergeCopiesOnlyNewCards(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
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
	if _, err := f.store.GradeCard(ctx, f.bob.ID, firstCards[0].ID, flash.RatingGood, now); err != nil {
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
	due, err := f.store.DueQueue(ctx, f.bob.ID, &mergedDeck.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Fatalf("DueQueue len = %d, want 1 (only the freshly merged card c2, not the already-graded c1)", len(due))
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

func TestDeleteAdoptedDeckClearsShareBackReferenceButKeepsShare(t *testing.T) {
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
	newDeck, err := f.store.AdoptShare(ctx, f.bob.ID, sh.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Bob deletes his own adopted copy — this must succeed rather than
	// tripping the adopted_deck_id -> flash_decks foreign key (migration
	// 0008 fixed the FK to ON DELETE SET NULL).
	if err := f.store.DeleteDeck(ctx, f.bob.ID, newDeck.ID); err != nil {
		t.Fatalf("DeleteDeck on adopted copy: %v", err)
	}

	// The source deck is untouched.
	if _, err := f.store.DeckByID(ctx, f.alice.ID, d.ID); err != nil {
		t.Fatalf("source deck should still exist: %v", err)
	}

	// The original share row survives with status still "adopted", but its
	// adopted_deck_id back-reference is now NULL.
	var status string
	var adoptedDeckID sql.NullInt64
	err = f.db.QueryRowContext(ctx,
		`SELECT status, adopted_deck_id FROM flash_shares WHERE id = ?`, sh.ID,
	).Scan(&status, &adoptedDeckID)
	if err != nil {
		t.Fatal(err)
	}
	if status != flash.ShareStatusAdopted {
		t.Errorf("status = %q, want %q", status, flash.ShareStatusAdopted)
	}
	if adoptedDeckID.Valid {
		t.Errorf("adopted_deck_id = %v, want NULL after deleting the adopted copy", adoptedDeckID.Int64)
	}
}

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

func TestAdoptShareCopiesDeckColor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Planets", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SetDeckColor(ctx, f.alice.ID, d.ID, "pink"); err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	adopted, err := f.store.AdoptShare(ctx, f.bob.ID, sh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if adopted.Color != "pink" {
		t.Errorf("adopted deck Color = %q, want the source deck's pink", adopted.Color)
	}
}

func TestSharesForRecipientCarriesDeckColor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Planets", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SetDeckColor(ctx, f.alice.ID, d.ID, "purple"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID); err != nil {
		t.Fatal(err)
	}
	offers, err := f.store.SharesForRecipient(ctx, f.bob.ID)
	if err != nil || len(offers) != 1 {
		t.Fatalf("offers = %+v, %v", offers, err)
	}
	if offers[0].DeckColor != "purple" {
		t.Errorf("DeckColor = %q, want purple", offers[0].DeckColor)
	}
}

func TestSharePreviewForTheRecipient(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Planets", "The solar system")
	if err != nil {
		t.Fatal(err)
	}
	for _, front := range []string{"Mercury", "Venus", "Earth", "Mars", "Jupiter"} {
		if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, front, "a planet", ""); err != nil {
			t.Fatal(err)
		}
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}

	p, err := f.store.SharePreview(ctx, f.bob.ID, sh.ID)
	if err != nil {
		t.Fatalf("SharePreview: %v", err)
	}
	if p.Deck.Name != "Planets" || p.Deck.Description != "The solar system" {
		t.Errorf("preview deck = %+v", p.Deck)
	}
	if p.CardCount != 5 {
		t.Errorf("CardCount = %d, want 5", p.CardCount)
	}
	if len(p.Samples) != 4 || p.Samples[0].Front != "Mercury" {
		t.Errorf("Samples = %+v, want the first four, oldest first", p.Samples)
	}
}

func TestSharePreviewIsOnlyForThePendingRecipient(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Planets", "")
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The sharer is not the recipient.
	if _, err := f.store.SharePreview(ctx, f.alice.ID, sh.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("preview as the sharer: err = %v, want ErrNotFound", err)
	}
	// An unknown share.
	if _, err := f.store.SharePreview(ctx, f.bob.ID, sh.ID+100); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("preview of a missing share: err = %v, want ErrNotFound", err)
	}
	// A resolved share is no longer previewable.
	if err := f.store.DeclineShare(ctx, f.bob.ID, sh.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SharePreview(ctx, f.bob.ID, sh.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("preview of a declined share: err = %v, want ErrNotFound", err)
	}
}

func TestSharePreviewOfAMergeCountsOnlyNewCards(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Planets", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, "Mercury", "x", ""); err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AdoptShare(ctx, f.bob.ID, sh.ID); err != nil {
		t.Fatal(err)
	}
	for _, front := range []string{"Venus", "Earth"} {
		if _, err := f.store.CreateCard(ctx, f.alice.ID, d.ID, flash.CardTypeBasic, front, "x", ""); err != nil {
			t.Fatal(err)
		}
	}
	again, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.store.SharePreview(ctx, f.bob.ID, again.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Offer.PriorAdoptedDeckID == nil || p.CardCount != 2 {
		t.Errorf("merge preview = %+v, want a merge of 2 new cards", p)
	}
	for _, c := range p.Samples {
		if c.Front == "Mercury" {
			t.Errorf("merge samples include the already-adopted card: %+v", p.Samples)
		}
	}
}
