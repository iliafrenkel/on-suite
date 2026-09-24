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
