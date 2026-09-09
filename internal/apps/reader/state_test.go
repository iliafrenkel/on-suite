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
