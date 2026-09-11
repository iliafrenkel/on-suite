package reader_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

func TestSaveFullArticleRoundTrips(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	item := seedItem(t, f, "g1")
	now := time.Now().UTC()

	hash := reader.ImageHash("https://cdn.example/full.png")
	ex := reader.Extracted{
		HTML:       `<p>The full body.</p><img src="` + reader.ImagePathPrefix + hash + `">`,
		Title:      "A Title",
		Images:     map[string]string{hash: "https://cdn.example/full.png"},
		TextLength: 900,
	}
	if err := f.store.SaveFullArticle(ctx, f.alice.ID, item.ID, ex, now); err != nil {
		t.Fatalf("SaveFullArticle: %v", err)
	}

	got, err := f.store.Item(ctx, f.alice.ID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasFull() {
		t.Fatal("item does not report a full article after one was saved")
	}
	if !strings.Contains(got.FullHTML, "The full body.") {
		t.Errorf("FullHTML = %q", got.FullHTML)
	}
	if got.FullError != "" {
		t.Errorf("FullError = %q on a successful save", got.FullError)
	}

	// The extracted article's images must be reference-counted exactly like a
	// feed body's, or retention will never free them.
	if _, err := f.store.ImageByHash(ctx, hash); err != nil {
		t.Errorf("the extracted article's image was not recorded: %v", err)
	}
}

func TestSaveFullArticleClearsAnEarlierFailure(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	item := seedItem(t, f, "g1")
	now := time.Now().UTC()

	if err := f.store.SaveFullArticleFailure(ctx, f.alice.ID, item.ID, "502 Bad Gateway", now); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.Item(ctx, f.alice.ID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FullError == "" {
		t.Fatal("failure was not recorded")
	}
	if got.HasFull() {
		t.Error("a failed fetch left the item claiming it has a full article")
	}

	if err := f.store.SaveFullArticle(ctx, f.alice.ID, item.ID, reader.Extracted{
		HTML: "<p>Worked this time.</p>", TextLength: 500,
	}, now); err != nil {
		t.Fatal(err)
	}
	got, err = f.store.Item(ctx, f.alice.ID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FullError != "" {
		t.Errorf("FullError = %q after a later success; a retry that works must clear it", got.FullError)
	}
}

func TestSaveFullArticleRefusesAnItemTheUserCannotSee(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	item := seedItem(t, f, "g1")

	// Bob does not subscribe to this feed. Full articles are shared across the
	// household, so writing one is a write to everybody's copy: it needs the
	// same visibility check every other state change has.
	err := f.store.SaveFullArticle(ctx, f.bob.ID, item.ID, reader.Extracted{
		HTML: "<p>x</p>", TextLength: 500,
	}, time.Now().UTC())
	if !errors.Is(err, reader.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestSaveItemsDoesNotDestroyFullArticleImageLinks reproduces the bug where a
// feed re-poll's unconditional "delete all this item's image links, then
// reinsert the feed body's own" wiped out a full article's image links too,
// since both were recorded in the same reader_item_images table with no way
// to tell them apart. A poll that runs after a full-article fetch must leave
// that fetch's images referenced, or the orphan purge frees them out from
// under a full_html that still points at them.
func TestSaveItemsDoesNotDestroyFullArticleImageLinks(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "g1", Title: "An article", PublishedAt: now.Add(-time.Hour)},
	}, now); err != nil {
		t.Fatal(err)
	}
	items, err := f.store.ItemsForSubscription(ctx, f.alice.ID, sub.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	item := items[0]

	fullHash := reader.ImageHash("https://cdn.example/full-only.png")
	if err := f.store.SaveFullArticle(ctx, f.alice.ID, item.ID, reader.Extracted{
		HTML:   `<img src="` + reader.ImagePathPrefix + fullHash + `">`,
		Images: map[string]string{fullHash: "https://cdn.example/full-only.png"},
	}, now); err != nil {
		t.Fatal(err)
	}

	// Simulate the next feed poll: same GUID, feed body has its own (different)
	// image. This must not disturb the full article's image link above.
	feedHash := reader.ImageHash("https://cdn.example/feed-body.png")
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{
			GUID:        "g1",
			Title:       "An article",
			PublishedAt: now.Add(-time.Hour),
			Images:      map[string]string{feedHash: "https://cdn.example/feed-body.png"},
		},
	}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.PurgeOrphanImages(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.ImageByHash(ctx, fullHash); err != nil {
		t.Errorf("full article's image was freed by a feed re-poll: %v", err)
	}
	if _, err := f.store.ImageByHash(ctx, feedHash); err != nil {
		t.Errorf("feed body's image was unexpectedly freed: %v", err)
	}
}

func TestPurgeFreesAFullArticlesImages(t *testing.T) {
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
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "g1", Title: "Old", PublishedAt: old},
	}, now); err != nil {
		t.Fatal(err)
	}
	items, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeFeed, sub.ID, reader.FilterAll, 10)
	if err != nil {
		t.Fatal(err)
	}

	hash := reader.ImageHash("https://cdn.example/only-in-full.png")
	if err := f.store.SaveFullArticle(ctx, f.alice.ID, items[0].ID, reader.Extracted{
		HTML:   `<img src="` + reader.ImagePathPrefix + hash + `">`,
		Images: map[string]string{hash: "https://cdn.example/only-in-full.png"},
	}, now); err != nil {
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
		t.Errorf("purged %d images, want 1 — an image only the full article used must still be freed", n)
	}
}
