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

// TestSaveCardFormAttachesAndStoresTogether is what used to be
// TestAttachCardUploadStoresAndAttachesTogether: SaveCardForm's Audio field
// stores a new upload and attaches it in the same call (#382).
func TestSaveCardFormAttachesAndStoresTogether(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, c := purgeCard(t, f)

	got, err := applyCardForm(t, ctx, f.store, f.alice.ID, d.ID, c, flash.CardForm{
		Audio: &flash.CardUpload{ContentType: "audio/mpeg", Data: []byte("fake mp3 bytes")},
	})
	if err != nil {
		t.Fatalf("SaveCardForm: %v", err)
	}
	if got.AudioHash == nil {
		t.Fatal("AudioHash is nil; the upload was not attached")
	}
	if got.ImageHash != nil {
		t.Errorf("ImageHash = %v, want nil (only audio was attached)", got.ImageHash)
	}
	m, err := f.store.MediaByHash(ctx, *got.AudioHash)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Cached() || string(m.Bytes) != "fake mp3 bytes" || m.ContentType != "audio/mpeg" || m.Kind != flash.MediaKindAudio {
		t.Errorf("stored media = %+v", m)
	}
}

// TestSaveCardFormThatCannotAttachLeavesNoRow is what used to be
// TestAttachCardUploadThatCannotAttachLeavesNoRow's ownership half: the
// insert and the attach are one unit, so a failed attach leaves no orphan
// behind. Its "unknown media kind" half had no equivalent to retarget:
// SaveCardForm only ever calls the unexported setCardMedia with
// MediaKindImage or MediaKindAudio, so an arbitrary kind can no longer reach
// that check through any exported API (#382).
func TestSaveCardFormThatCannotAttachLeavesNoRow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, c := purgeCard(t, f)

	// Bob can't attach to Alice's card.
	if _, err := applyCardForm(t, ctx, f.store, f.bob.ID, d.ID, c, flash.CardForm{
		Image: &flash.CardUpload{ContentType: "image/png", Data: onePNG},
	}); !errors.Is(err, flash.ErrNotFound) {
		t.Fatalf("attach to someone else's card: err = %v, want ErrNotFound", err)
	}
	if n := mediaRowCount(t, f); n != 0 {
		t.Errorf("flash_media has %d rows after a failed attach, want 0", n)
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

	got, err := applyCardForm(t, ctx, f.store, f.alice.ID, d.ID, c, flash.CardForm{
		Image: &flash.CardUpload{ContentType: "image/png", Data: onePNG},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ImageHash == nil || *got.ImageHash != orphan {
		t.Fatalf("same bytes produced a different hash: %v vs %q", got.ImageHash, orphan)
	}
	purge(t, f, 0)
	if !mediaExists(t, f, orphan) {
		t.Fatal("the re-attached file was purged")
	}
}

// TestPurgeRunningAlongsideAttachesNeverBreaksOne is the #302.5 race: with
// the purge running continuously, every attach must still succeed. When the
// store and the attach were two statements, the purge could take the one
// connection between them, delete the just-stored row, and the attach then
// failed its foreign key. SaveCardForm's Image field now runs the same
// attachUpload helper AttachCardUpload used to, so this still pins that
// guarantee (#382).
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
		got, err := applyCardForm(t, ctx, f.store, f.alice.ID, d.ID, c, flash.CardForm{
			Image: &flash.CardUpload{ContentType: "image/png", Data: data},
		})
		if err != nil {
			close(stop)
			<-done
			t.Fatalf("attach %d with the purge running: %v", i, err)
		}
		last = *got.ImageHash
	}
	close(stop)
	if err := <-done; err != nil {
		t.Fatalf("PurgeOrphanMedia: %v", err)
	}
	if !mediaExists(t, f, last) {
		t.Fatal("the attached image was purged")
	}
}
