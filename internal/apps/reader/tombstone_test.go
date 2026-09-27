package reader_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// purgeOldReadItem subscribes alice to a feed carrying one years-old item,
// marks it read and runs the purge at now, leaving a tombstone behind. It
// returns the subscription and the feed content, so a test can "re-poll".
func purgeOldReadItem(t *testing.T, f *storeFixture, feedURL string, now time.Time) (reader.Subscription, []reader.ParsedItem) {
	t.Helper()
	ctx := context.Background()
	f.store.SetClock(func() time.Time { return now })

	sub, err := f.store.Subscribe(ctx, f.alice.ID, feedURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	feed := []reader.ParsedItem{{GUID: "ancient", Title: "Years old, still in the feed",
		PublishedAt: now.Add(-3 * 365 * 24 * time.Hour)}}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, feed, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.MarkAllRead(ctx, f.alice.ID, reader.ScopeAll, 0, now, 0); err != nil {
		t.Fatal(err)
	}
	n, err := f.store.PurgeItems(ctx, now.Add(-reader.RetentionAge))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("purged %d items, want 1", n)
	}
	return sub, feed
}

func unreadCount(t *testing.T, f *storeFixture) int {
	t.Helper()
	items, err := f.store.ItemsForScope(context.Background(), f.alice.ID, reader.ScopeAll, 0, reader.FilterUnread, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	return len(items)
}

func tombstoneSeenAt(t *testing.T, f *storeFixture, feedID int64, guid string) string {
	t.Helper()
	var seen string
	if err := f.db.QueryRowContext(context.Background(),
		`SELECT last_seen_at FROM reader_purged_items WHERE feed_id = ? AND guid = ?`,
		feedID, guid).Scan(&seen); err != nil {
		t.Fatalf("tombstone for %q: %v", guid, err)
	}
	return seen
}

// The bug in #389: the purge deleted an old read article, and the next poll of
// a feed that still carried it inserted it again as a brand-new unread one.
func TestPurgedItemIsNotResurrectedByTheNextPoll(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	sub, feed := purgeOldReadItem(t, f, "https://example.com/feed.xml", now)

	later := now.Add(10 * time.Minute)
	inserted, err := f.store.SaveItems(ctx, sub.FeedID, feed, later)
	if err != nil {
		t.Fatal(err)
	}
	if inserted != 0 {
		t.Errorf("re-poll inserted %d items, want 0: the purged item came back", inserted)
	}
	if got := unreadCount(t, f); got != 0 {
		t.Errorf("%d unread after purge and re-poll, want 0", got)
	}
	if got, want := tombstoneSeenAt(t, f, sub.FeedID, "ancient"), db.FormatTime(later); got != want {
		t.Errorf("tombstone last_seen_at = %s, want the re-poll's %s", got, want)
	}
}

// A tombstone only guards against the feed still listing the item. Once the
// feed has not mentioned it for RetentionAge, it is pruned — and a tombstone
// the feed keeps mentioning is not.
func TestPruneTombstonesDropsOnlyTheOnesAFeedStoppedListing(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	gone, _ := purgeOldReadItem(t, f, "https://example.com/gone.xml", now)

	// A second feed purged at the same moment, but polled again later.
	kept, keptFeed := purgeOldReadItem(t, f, "https://example.com/kept.xml", now)
	later := now.Add(reader.RetentionAge)
	if _, err := f.store.SaveItems(ctx, kept.FeedID, keptFeed, later); err != nil {
		t.Fatal(err)
	}

	pruneAt := later.Add(time.Hour)
	n, err := f.store.PruneTombstones(ctx, pruneAt.Add(-reader.RetentionAge))
	if err != nil {
		t.Fatalf("PruneTombstones: %v", err)
	}
	if n != 1 {
		t.Errorf("pruned %d tombstones, want 1", n)
	}

	var left []int64
	rows, err := f.db.QueryContext(ctx, `SELECT feed_id FROM reader_purged_items`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		left = append(left, id)
	}
	if len(left) != 1 || left[0] != kept.FeedID {
		t.Errorf("tombstones left for feeds %v, want only %d (not %d)", left, kept.FeedID, gone.FeedID)
	}
}

// A 304 means the feed body is exactly what it was, so every item it still
// lists — tombstoned ones included — was seen again. Without this, a quiet
// feed that answers 304 for RetentionAge would lose its tombstones and hand
// back its whole old backlog the next time it changes.
func TestNotModifiedPollKeepsTombstonesFresh(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	now := time.Now().UTC()
	sub, _ := purgeOldReadItem(t, f, srv.URL+"/feed.xml", now)

	later := now.Add(24 * time.Hour)
	f.store.SetClock(func() time.Time { return later })
	client := reader.NewClient("test")
	client.DenyAddr = func(string) error { return nil }
	if err := reader.NewPoller(f.store, client, quietLogger()).FetchNow(ctx, sub.FeedID); err != nil {
		t.Fatalf("FetchNow: %v", err)
	}

	if got, want := tombstoneSeenAt(t, f, sub.FeedID, "ancient"), db.FormatTime(later); got != want {
		t.Errorf("tombstone last_seen_at = %s after a 304, want %s", got, want)
	}
}
