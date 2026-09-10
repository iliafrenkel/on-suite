package reader_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

func TestSaveItemsRecordsImages(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	hash := reader.ImageHash("https://cdn.example/a.png")
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
		GUID:        "g1",
		Title:       "With an image",
		PublishedAt: now.Add(-time.Hour),
		ContentHTML: `<img src="/reader/img/` + hash + `">`,
		Images:      map[string]string{hash: "https://cdn.example/a.png"},
	}}, now); err != nil {
		t.Fatal(err)
	}

	img, err := f.store.ImageByHash(ctx, hash)
	if err != nil {
		t.Fatalf("ImageByHash: %v", err)
	}
	if img.SrcURL != "https://cdn.example/a.png" {
		t.Errorf("SrcURL = %q", img.SrcURL)
	}
	if img.Cached() {
		t.Error("a freshly recorded image reports itself cached; bytes arrive on first view")
	}
}

func TestImageByHashIsNotFoundForAnUnknownHash(t *testing.T) {
	f := newStoreFixture(t)
	if _, err := f.store.ImageByHash(context.Background(), reader.ImageHash("https://nobody.example/x.png")); !errors.Is(err, reader.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound — an unrecorded URL must not be fetchable", err)
	}
}

func TestSaveImageBytesRoundTrips(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	hash := reader.ImageHash("https://cdn.example/a.png")
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
		GUID: "g1", Title: "x", PublishedAt: now.Add(-time.Hour),
		Images: map[string]string{hash: "https://cdn.example/a.png"},
	}}, now); err != nil {
		t.Fatal(err)
	}

	want := []byte{0x89, 'P', 'N', 'G'}
	if err := f.store.SaveImageBytes(ctx, hash, "image/png", want, now); err != nil {
		t.Fatalf("SaveImageBytes: %v", err)
	}
	img, err := f.store.ImageByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !img.Cached() {
		t.Error("image is not cached after SaveImageBytes")
	}
	if string(img.Bytes) != string(want) {
		t.Errorf("bytes round-tripped as %v, want %v", img.Bytes, want)
	}
	if img.ContentType != "image/png" {
		t.Errorf("ContentType = %q", img.ContentType)
	}
}

func TestPurgeOrphanImagesFollowsItems(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	old := now.Add(-90 * 24 * time.Hour)

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx,
		`UPDATE reader_subs SET added_at = ? WHERE id = ?`,
		old.Add(-24*time.Hour).Format(time.RFC3339Nano), sub.ID); err != nil {
		t.Fatal(err)
	}
	hash := reader.ImageHash("https://cdn.example/a.png")
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
		GUID: "g1", Title: "Old", PublishedAt: old,
		Images: map[string]string{hash: "https://cdn.example/a.png"},
	}}, now); err != nil {
		t.Fatal(err)
	}

	// An orphan purge before the item is gone must keep the image.
	if n, err := f.store.PurgeOrphanImages(ctx); err != nil || n != 0 {
		t.Fatalf("PurgeOrphanImages removed %d images while the item still exists (err %v)", n, err)
	}

	items, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeFeed, sub.ID, reader.FilterAll, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetRead(ctx, f.alice.ID, items[0].ID, true, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.PurgeItems(ctx, now.Add(-reader.RetentionAge)); err != nil {
		t.Fatal(err)
	}

	n, err := f.store.PurgeOrphanImages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("purged %d images after their only item went, want 1", n)
	}
	if _, err := f.store.ImageByHash(ctx, hash); !errors.Is(err, reader.ErrNotFound) {
		t.Error("the orphaned image is still fetchable")
	}
}

// Re-saving an item whose images changed must drop the link to the old one,
// or a removed image is pinned in the cache forever.
func TestResavingAnItemReplacesItsImageLinks(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	oldHash := reader.ImageHash("https://cdn.example/old.png")
	newHash := reader.ImageHash("https://cdn.example/new.png")

	save := func(h, u string) {
		t.Helper()
		if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
			GUID: "g1", Title: "x", PublishedAt: now.Add(-time.Hour),
			Images: map[string]string{h: u},
		}}, now); err != nil {
			t.Fatal(err)
		}
	}
	save(oldHash, "https://cdn.example/old.png")
	save(newHash, "https://cdn.example/new.png")

	if n, err := f.store.PurgeOrphanImages(ctx); err != nil || n != 1 {
		t.Fatalf("purged %d orphans after the image changed, want 1 (err %v)", n, err)
	}
	if _, err := f.store.ImageByHash(ctx, newHash); err != nil {
		t.Errorf("the current image was purged: %v", err)
	}
}
