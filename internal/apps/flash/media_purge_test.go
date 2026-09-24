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
