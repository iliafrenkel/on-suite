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

func TestSetCardMediaSetsAndClears(t *testing.T) {
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

	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
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

	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "", flash.DefaultDeckColor)
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
