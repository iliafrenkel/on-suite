package reader_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

// seedItem subscribes alice to a feed and stores one item, returning it.
func seedItem(t *testing.T, f *storeFixture, guid string) reader.Item {
	t.Helper()
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
		GUID:        guid,
		Title:       "An article",
		PublishedAt: time.Now().UTC().Add(-time.Hour),
	}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	items, err := f.store.ItemsForSubscription(ctx, f.alice.ID, sub.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("seedItem stored nothing")
	}
	return items[0]
}

func TestSetReadIsIdempotentAndReversible(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	item := seedItem(t, f, "g1")
	now := time.Now().UTC()

	read, starred, err := f.store.ItemState(ctx, f.alice.ID, item.ID)
	if err != nil {
		t.Fatalf("ItemState: %v", err)
	}
	if read || starred {
		t.Fatalf("a fresh item is read=%v starred=%v; absence of a row must mean neither", read, starred)
	}

	for range 2 {
		if err := f.store.SetRead(ctx, f.alice.ID, item.ID, true, now); err != nil {
			t.Fatalf("SetRead(true): %v", err)
		}
	}
	if read, _, _ := f.store.ItemState(ctx, f.alice.ID, item.ID); !read {
		t.Error("item is not read after SetRead(true)")
	}

	if err := f.store.SetRead(ctx, f.alice.ID, item.ID, false, now); err != nil {
		t.Fatalf("SetRead(false): %v", err)
	}
	if read, _, _ := f.store.ItemState(ctx, f.alice.ID, item.ID); read {
		t.Error("item is still read after SetRead(false)")
	}
}

// Starring then reading then unreading must not lose the star: the two axes
// share a row, which is exactly where a careless UPDATE clobbers one of them.
func TestReadAndStarredAreIndependent(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	item := seedItem(t, f, "g1")
	now := time.Now().UTC()

	if err := f.store.SetStarred(ctx, f.alice.ID, item.ID, true, now); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetRead(ctx, f.alice.ID, item.ID, true, now); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetRead(ctx, f.alice.ID, item.ID, false, now); err != nil {
		t.Fatal(err)
	}

	read, starred, err := f.store.ItemState(ctx, f.alice.ID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read {
		t.Error("item is read after being marked unread")
	}
	if !starred {
		t.Error("the star was lost when read state changed")
	}
}

func TestStateIsPerUser(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	item := seedItem(t, f, "g1")

	// Bob subscribes to the same feed, so he can legitimately see the item.
	if _, err := f.store.Subscribe(ctx, f.bob.ID, "https://example.com/feed.xml", nil); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetRead(ctx, f.alice.ID, item.ID, true, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	if read, _, _ := f.store.ItemState(ctx, f.bob.ID, item.ID); read {
		t.Error("alice reading an item marked it read for bob; state is per user")
	}
}

func TestSetReadRefusesAnItemTheUserCannotSee(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	item := seedItem(t, f, "g1")

	// Bob does not subscribe to this feed. Item ids are global, so without an
	// authorisation check he could mark anything in the database read — and
	// the row he wrote would then be a durable record that he probed for it.
	if err := f.store.SetRead(ctx, f.bob.ID, item.ID, true, time.Now().UTC()); !errors.Is(err, reader.ErrNotFound) {
		t.Fatalf("SetRead for a non-subscriber returned %v, want ErrNotFound", err)
	}
	if err := f.store.SetStarred(ctx, f.bob.ID, item.ID, true, time.Now().UTC()); !errors.Is(err, reader.ErrNotFound) {
		t.Fatalf("SetStarred for a non-subscriber returned %v, want ErrNotFound", err)
	}
}

func TestItemsForScopeFiltersUnread(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "A", PublishedAt: now.Add(-2 * time.Hour)},
		{GUID: "b", Title: "B", PublishedAt: now.Add(-time.Hour)},
	}, now); err != nil {
		t.Fatal(err)
	}

	all, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeFeed, sub.ID, reader.FilterAll, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("FilterAll returned %d items, want 2", len(all))
	}
	if err := f.store.SetRead(ctx, f.alice.ID, all[0].ID, true, now); err != nil {
		t.Fatal(err)
	}

	unread, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeFeed, sub.ID, reader.FilterUnread, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(unread) != 1 {
		t.Fatalf("FilterUnread returned %d items, want 1", len(unread))
	}
	if unread[0].ID == all[0].ID {
		t.Error("the item just marked read is still in the unread list")
	}
}

func TestItemsCarryTheirOwnState(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	item := seedItem(t, f, "g1")
	now := time.Now().UTC()

	if err := f.store.SetStarred(ctx, f.alice.ID, item.ID, true, now); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeAll, 0, reader.FilterAll, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d items, want 1", len(got))
	}
	if !got[0].Starred {
		t.Error("Starred is false on an item that was just starred; the list must carry state, not just ids")
	}
	if got[0].Read {
		t.Error("Read is true on an item nobody read")
	}
}

func TestScopeStarredCrossesFeeds(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	var starred []int64
	for _, u := range []string{"https://a.example/feed", "https://b.example/feed"} {
		sub, err := f.store.Subscribe(ctx, f.alice.ID, u, nil)
		if err != nil {
			t.Fatal(err)
		}
		fetchedAt := time.Now().UTC()
		if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
			{GUID: "x", Title: "From " + u, PublishedAt: now.Add(-time.Hour)},
		}, fetchedAt); err != nil {
			t.Fatal(err)
		}
		items, err := f.store.ItemsForSubscription(ctx, f.alice.ID, sub.ID, 10)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.store.SetStarred(ctx, f.alice.ID, items[0].ID, true, now); err != nil {
			t.Fatal(err)
		}
		starred = append(starred, items[0].ID)
	}

	got, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeStarred, 0, reader.FilterAll, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(starred) {
		t.Errorf("starred scope returned %d items, want %d across both feeds", len(got), len(starred))
	}
}

// The added_at cutoff is what stops a second household member inheriting a
// backlog they never asked for.
func TestItemsPublishedBeforeSubscribingAreNotUnread(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Alice subscribes and the feed backfills a month of history.
	aliceSub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	aliceFetchedAt := time.Now().UTC()
	if _, err := f.store.SaveItems(ctx, aliceSub.FeedID, []reader.ParsedItem{
		{GUID: "old", Title: "Old", PublishedAt: now.Add(-30 * 24 * time.Hour)},
	}, aliceFetchedAt); err != nil {
		t.Fatal(err)
	}

	// Bob subscribes now. The old item predates his subscription.
	bobSub, err := f.store.Subscribe(ctx, f.bob.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	bobUnread, err := f.store.ItemsForScope(ctx, f.bob.ID, reader.ScopeFeed, bobSub.ID, reader.FilterUnread, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(bobUnread) != 0 {
		t.Errorf("bob has %d unread items from before he subscribed; added_at must exclude them", len(bobUnread))
	}

	aliceUnread, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeFeed, aliceSub.ID, reader.FilterUnread, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliceUnread) != 1 {
		t.Errorf("alice has %d unread items; the cutoff must not hide items from the original subscriber", len(aliceUnread))
	}
}

func TestUnreadCounts(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "A", PublishedAt: now.Add(-3 * time.Hour)},
		{GUID: "b", Title: "B", PublishedAt: now.Add(-2 * time.Hour)},
		{GUID: "c", Title: "C", PublishedAt: now.Add(-time.Hour)},
	}, now); err != nil {
		t.Fatal(err)
	}

	counts, err := f.store.UnreadCounts(ctx, f.alice.ID)
	if err != nil {
		t.Fatalf("UnreadCounts: %v", err)
	}
	if counts.BySub[sub.ID] != 3 {
		t.Errorf("BySub[%d] = %d, want 3", sub.ID, counts.BySub[sub.ID])
	}
	if counts.Total != 3 {
		t.Errorf("Total = %d, want 3", counts.Total)
	}

	items, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeFeed, sub.ID, reader.FilterAll, 50)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetRead(ctx, f.alice.ID, items[0].ID, true, now); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetStarred(ctx, f.alice.ID, items[1].ID, true, now); err != nil {
		t.Fatal(err)
	}

	counts, err = f.store.UnreadCounts(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if counts.BySub[sub.ID] != 2 {
		t.Errorf("after one read, BySub = %d, want 2", counts.BySub[sub.ID])
	}
	if counts.Starred != 1 {
		t.Errorf("Starred = %d, want 1", counts.Starred)
	}
}

// A subscription with nothing unread must still appear, at zero. Dropping it
// from the map is how a sidebar ends up silently missing a feed.
func TestUnreadCountsIncludeEmptySubscriptions(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	counts, err := f.store.UnreadCounts(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := counts.BySub[sub.ID]; !ok {
		t.Error("a subscription with no items is missing from BySub entirely")
	}
	if counts.BySub[sub.ID] != 0 {
		t.Errorf("BySub = %d, want 0", counts.BySub[sub.ID])
	}
}

func TestMarkAllReadIsScopedAndDoesNotTouchOtherUsers(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	aliceSub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	bobSub, err := f.store.Subscribe(ctx, f.bob.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := f.store.SaveItems(ctx, aliceSub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "A", PublishedAt: now.Add(-2 * time.Hour)},
		{GUID: "b", Title: "B", PublishedAt: now.Add(-time.Hour)},
	}, now); err != nil {
		t.Fatal(err)
	}

	n, err := f.store.MarkAllRead(ctx, f.alice.ID, reader.ScopeFeed, aliceSub.ID, now)
	if err != nil {
		t.Fatalf("MarkAllRead: %v", err)
	}
	if n != 2 {
		t.Errorf("marked %d items read, want 2", n)
	}

	aliceCounts, err := f.store.UnreadCounts(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if aliceCounts.Total != 0 {
		t.Errorf("alice still has %d unread", aliceCounts.Total)
	}

	bobCounts, err := f.store.UnreadCounts(ctx, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bobCounts.BySub[bobSub.ID] != 2 {
		t.Errorf("bob has %d unread; alice's mark-all-read must not touch his state", bobCounts.BySub[bobSub.ID])
	}
}
