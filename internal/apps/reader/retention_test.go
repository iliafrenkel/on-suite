package reader_test

import (
	"context"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

func readerDeps() app.Deps { return app.Deps{} }

func TestPurgeKeepsStarredAndUnreadItems(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	old := now.Add(-90 * 24 * time.Hour)

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Backdate the subscription so the old items are inside alice's window.
	if _, err := f.store.DB().ExecContext(ctx,
		`UPDATE reader_subs SET added_at = ? WHERE id = ?`,
		old.Add(-24*time.Hour).Format(time.RFC3339Nano), sub.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "read", Title: "Read and old", PublishedAt: old},
		{GUID: "starred", Title: "Starred and old", PublishedAt: old},
		{GUID: "unread", Title: "Unread and old", PublishedAt: old},
		{GUID: "recent", Title: "Read but recent", PublishedAt: now.Add(-time.Hour)},
	}, now); err != nil {
		t.Fatal(err)
	}

	items, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeFeed, sub.ID, reader.FilterAll, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	byGUID := map[string]int64{}
	for _, it := range items {
		byGUID[it.GUID] = it.ID
	}
	if len(byGUID) != 4 {
		t.Fatalf("seeded %d items, want 4", len(byGUID))
	}

	for _, guid := range []string{"read", "starred", "recent"} {
		if err := f.store.SetRead(ctx, f.alice.ID, byGUID[guid], true, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.store.SetStarred(ctx, f.alice.ID, byGUID["starred"], true, now); err != nil {
		t.Fatal(err)
	}

	deleted, err := f.store.PurgeItems(ctx, now.Add(-reader.RetentionAge))
	if err != nil {
		t.Fatalf("PurgeItems: %v", err)
	}
	if deleted != 1 {
		t.Errorf("purged %d items, want 1 (only the old read unstarred one)", deleted)
	}

	left, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeFeed, sub.ID, reader.FilterAll, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	survived := map[string]bool{}
	for _, it := range left {
		survived[it.GUID] = true
	}
	for _, guid := range []string{"starred", "unread", "recent"} {
		if !survived[guid] {
			t.Errorf("item %q was purged and should not have been", guid)
		}
	}
	if survived["read"] {
		t.Error("the old read unstarred item survived the purge")
	}
}

// An item one household member has read but another has not must survive: the
// items are shared, the state is not.
func TestPurgeKeepsAnItemAnotherUserHasNotRead(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	old := now.Add(-90 * 24 * time.Hour)

	aliceSub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	bobSub, err := f.store.Subscribe(ctx, f.bob.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{aliceSub.ID, bobSub.ID} {
		if _, err := f.store.DB().ExecContext(ctx,
			`UPDATE reader_subs SET added_at = ? WHERE id = ?`,
			old.Add(-24*time.Hour).Format(time.RFC3339Nano), id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.store.SaveItems(ctx, aliceSub.FeedID, []reader.ParsedItem{
		{GUID: "shared", Title: "Old", PublishedAt: old},
	}, now); err != nil {
		t.Fatal(err)
	}

	items, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeFeed, aliceSub.ID, reader.FilterAll, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetRead(ctx, f.alice.ID, items[0].ID, true, now); err != nil {
		t.Fatal(err)
	}

	deleted, err := f.store.PurgeItems(ctx, now.Add(-reader.RetentionAge))
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Errorf("purged %d items; bob has not read it, so it must stay", deleted)
	}
}

func TestReaderRegistersBothJobs(t *testing.T) {
	names := map[string]bool{}
	for _, j := range reader.New().Jobs(readerDeps()) {
		names[j.Name] = true
		if j.Every <= 0 {
			t.Errorf("job %q has interval %v; it would never run", j.Name, j.Every)
		}
		if j.Run == nil {
			t.Errorf("job %q has no Run function", j.Name)
		}
	}
	for _, want := range []string{"refresh feeds", "purge old articles"} {
		if !names[want] {
			t.Errorf("job %q is not registered; got %v", want, names)
		}
	}
}
