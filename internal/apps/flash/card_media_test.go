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

	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
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

// TestCardMediaHashSetsAndClears is what used to be
// TestSetCardMediaSetsAndClears: SetCardMedia is gone (SaveCardForm attaches
// or removes media instead), but a card that already carries an existing
// hash — the shape SaveCardForm can't itself produce — must still read back
// and clear correctly, so this seeds the hash directly and clears it through
// SaveCardForm's RemoveImage (#382).
func TestCardMediaHashSetsAndClears(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
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
	setCardMediaHash(t, f.db, c.ID, flash.MediaKindImage, &hash)
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

	if _, err := applyCardForm(t, ctx, f.store, f.alice.ID, deck.ID, got, flash.CardForm{RemoveImage: true}); err != nil {
		t.Fatalf("SaveCardForm (clear): %v", err)
	}
	cleared, err := f.store.CardByID(ctx, f.alice.ID, deck.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ImageHash != nil {
		t.Errorf("ImageHash = %v after clearing, want nil", cleared.ImageHash)
	}
}

// SetCardMedia's own "unknown kind" validation (TestSetCardMediaRejectsUnknownKind)
// had no equivalent to retarget: SaveCardForm only ever calls the unexported
// setCardMedia with MediaKindImage or MediaKindAudio, so an arbitrary kind
// like "video" can no longer reach that check through any exported API
// (#382).

// TestSaveCardFormImageOnSomeoneElsesCardIs404 is what used to be
// TestSetCardMediaOnSomeoneElsesCardIs404: attaching media to someone
// else's card through SaveCardForm must fail the same way, and must not
// attach anything (#382).
func TestSaveCardFormImageOnSomeoneElsesCardIs404(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = applyCardForm(t, ctx, f.store, f.bob.ID, deck.ID, c, flash.CardForm{
		Image: &flash.CardUpload{ContentType: "image/png", Data: onePNG},
	})
	if !errors.Is(err, flash.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	got, err := f.store.CardByID(ctx, f.alice.ID, deck.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ImageHash != nil {
		t.Errorf("ImageHash = %v, want nil (bob's attempt must not attach anything)", got.ImageHash)
	}
}
