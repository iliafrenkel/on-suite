package reader_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// storeFixture is a migrated database with two users, so every owner-scoping test
// has somebody else to be confused with.
type storeFixture struct {
	store *reader.Store
	db    *sql.DB
	alice auth.User
	bob   auth.User
}

func newStoreFixture(t *testing.T) *storeFixture {
	t.Helper()
	ctx := context.Background()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	appMigrations, err := db.Collect(reader.ID, reader.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(migrations, appMigrations...)); err != nil {
		t.Fatal(err)
	}

	users := auth.NewStore(handle)
	alice, err := users.CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := users.CreateUser(ctx, "bob", apptest.PasswordHash, false)
	if err != nil {
		t.Fatal(err)
	}
	return &storeFixture{store: reader.NewStore(handle), db: handle, alice: alice, bob: bob}
}

func TestSubscribeSharesOneFeedRow(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	a, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatalf("Subscribe(alice): %v", err)
	}
	b, err := f.store.Subscribe(ctx, f.bob.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatalf("Subscribe(bob): %v", err)
	}

	if a.FeedID != b.FeedID {
		t.Errorf("feed ids %d and %d differ; the same URL must be one feed row", a.FeedID, b.FeedID)
	}
	if a.ID == b.ID {
		t.Error("two users share one subscription row; subscriptions are per user")
	}

	var feeds int
	if err := f.db.QueryRowContext(ctx, `SELECT count(*) FROM reader_feeds`).Scan(&feeds); err != nil {
		t.Fatal(err)
	}
	if feeds != 1 {
		t.Errorf("reader_feeds has %d rows, want 1", feeds)
	}
}

func TestSubscribeTwiceIsNotAnError(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	first, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatalf("first Subscribe: %v", err)
	}
	again, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatalf("second Subscribe: %v", err)
	}
	if first.ID != again.ID {
		t.Errorf("re-subscribing made a new row (%d then %d); it must be idempotent", first.ID, again.ID)
	}
}

func TestUnsubscribeDropsTheFeedOnlyWhenOrphaned(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	aliceSub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Subscribe(ctx, f.bob.ID, "https://example.com/feed.xml", nil); err != nil {
		t.Fatal(err)
	}

	if err := f.store.Unsubscribe(ctx, f.alice.ID, aliceSub.ID); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	var feeds int
	if err := f.db.QueryRowContext(ctx, `SELECT count(*) FROM reader_feeds`).Scan(&feeds); err != nil {
		t.Fatal(err)
	}
	if feeds != 1 {
		t.Fatalf("feed deleted while bob is still subscribed: %d rows, want 1", feeds)
	}
}

func TestUnsubscribeIsScopedToTheOwner(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	aliceSub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Unsubscribe(ctx, f.bob.ID, aliceSub.ID); err == nil {
		t.Fatal("bob unsubscribed alice's subscription; ownership must be enforced in SQL")
	}
}

func TestDeleteFolderKeepsItsSubscriptions(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	folder, err := f.store.CreateFolder(ctx, f.alice.ID, "Tech")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", &folder.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteFolder(ctx, f.alice.ID, folder.ID); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}

	tree, err := f.store.Tree(ctx, f.alice.ID)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if len(tree.Folders) != 0 {
		t.Errorf("folder survived deletion: %+v", tree.Folders)
	}
	if len(tree.Root) != 1 {
		t.Fatalf("subscription lost with its folder: root has %d, want 1", len(tree.Root))
	}
}

func TestTreeIsScopedToOneUser(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	if _, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/a.xml", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Subscribe(ctx, f.bob.ID, "https://example.com/b.xml", nil); err != nil {
		t.Fatal(err)
	}

	tree, err := f.store.Tree(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root) != 1 {
		t.Fatalf("alice sees %d subscriptions, want 1", len(tree.Root))
	}
	if tree.Root[0].FeedURL != "https://example.com/a.xml" {
		t.Errorf("alice sees %q, which is bob's feed", tree.Root[0].FeedURL)
	}
}

func TestSaveItemsIsIdempotentOnGUID(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	fetchedAt := time.Now().UTC()
	items := []reader.ParsedItem{{
		GUID:        "g1",
		URL:         "https://example.com/1",
		Title:       "First",
		PublishedAt: now.Add(-time.Hour),
		ContentHTML: "<p>one</p>",
	}}

	inserted, err := f.store.SaveItems(ctx, sub.FeedID, items, fetchedAt)
	if err != nil {
		t.Fatalf("SaveItems: %v", err)
	}
	if inserted != 1 {
		t.Errorf("first save inserted %d, want 1", inserted)
	}

	// A publisher edited the title in place. Same GUID, so it updates.
	items[0].Title = "First, corrected"
	inserted, err = f.store.SaveItems(ctx, sub.FeedID, items, fetchedAt)
	if err != nil {
		t.Fatalf("second SaveItems: %v", err)
	}
	if inserted != 0 {
		t.Errorf("re-saving inserted %d rows, want 0", inserted)
	}

	got, err := f.store.ItemsForSubscription(ctx, f.alice.ID, sub.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d items, want 1", len(got))
	}
	if got[0].Title != "First, corrected" {
		t.Errorf("Title = %q; a matching GUID must update in place", got[0].Title)
	}
}

func TestSaveItemsFallsBackToFetchTimeForAnUndatedItem(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	fetchedAt := time.Now().UTC()
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
		GUID: "g1", Title: "Undated",
	}}, fetchedAt); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.ItemsForSubscription(ctx, f.alice.ID, sub.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].PublishedAt.Equal(fetchedAt) {
		t.Errorf("PublishedAt = %v, want the fetch time %v", got[0].PublishedAt, fetchedAt)
	}
}

func TestItemsForSubscriptionIsScopedToTheOwner(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
		GUID: "g1", Title: "Alice's", PublishedAt: time.Now().UTC(),
	}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.ItemsForSubscription(ctx, f.bob.ID, sub.ID, 50); !errors.Is(err, reader.ErrNotFound) {
		t.Fatalf("bob read alice's subscription: err = %v, want ErrNotFound", err)
	}
}

func TestItemRequiresASubscription(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
		GUID: "g1", Title: "Alice's", PublishedAt: time.Now().UTC(),
	}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	items, err := f.store.ItemsForSubscription(ctx, f.alice.ID, sub.ID, 50)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.Item(ctx, f.bob.ID, items[0].ID); !errors.Is(err, reader.ErrNotFound) {
		t.Fatalf("bob read an item from a feed he is not subscribed to: err = %v, want ErrNotFound", err)
	}
	if _, err := f.store.Item(ctx, f.alice.ID, items[0].ID); err != nil {
		t.Fatalf("alice cannot read her own item: %v", err)
	}
}

func TestFeedByIDLoadsAFeedRegardlessOfDueStatus(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	feed, err := f.store.FeedByID(ctx, sub.FeedID)
	if err != nil {
		t.Fatalf("FeedByID: %v", err)
	}
	if feed.ID != sub.FeedID {
		t.Errorf("ID = %d, want %d", feed.ID, sub.FeedID)
	}
	if feed.URL != "https://example.com/feed.xml" {
		t.Errorf("URL = %q", feed.URL)
	}

	// A freshly subscribed feed is due immediately (next_fetch_at = now), but
	// FeedByID must not filter on that the way DueFeeds does — it is the
	// "load this specific feed" path, not "load whatever is due".
	if _, err := f.store.FeedByID(ctx, feed.ID+999); !errors.Is(err, reader.ErrNotFound) {
		t.Errorf("FeedByID(missing) = %v, want ErrNotFound", err)
	}
}

func TestFeedIDForSubIsScopedToTheOwner(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	feedID, err := f.store.FeedIDForSub(ctx, f.alice.ID, sub.ID)
	if err != nil {
		t.Fatalf("FeedIDForSub(alice): %v", err)
	}
	if feedID != sub.FeedID {
		t.Errorf("feedID = %d, want %d", feedID, sub.FeedID)
	}

	if _, err := f.store.FeedIDForSub(ctx, f.bob.ID, sub.ID); !errors.Is(err, reader.ErrNotFound) {
		t.Errorf("FeedIDForSub(bob) = %v, want ErrNotFound for someone else's subscription", err)
	}
}

func TestRenameSubscriptionSetsAndClearsTheOverride(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Before any poll, DisplayName falls back to the raw URL.
	if got := sub.DisplayName(); got != "https://example.com/feed.xml" {
		t.Fatalf("initial DisplayName = %q", got)
	}

	if err := f.store.RenameSubscription(ctx, f.alice.ID, sub.ID, "  My Feed  "); err != nil {
		t.Fatalf("RenameSubscription: %v", err)
	}
	tree, err := f.store.Tree(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.Root[0].DisplayName(); got != "My Feed" {
		t.Errorf("DisplayName after rename = %q, want trimmed %q", got, "My Feed")
	}

	// Clearing the override (empty string) falls back to the feed's own
	// title/URL again, rather than being rejected as invalid input.
	if err := f.store.RenameSubscription(ctx, f.alice.ID, sub.ID, ""); err != nil {
		t.Fatalf("RenameSubscription(clear): %v", err)
	}
	tree, err = f.store.Tree(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.Root[0].DisplayName(); got != "https://example.com/feed.xml" {
		t.Errorf("DisplayName after clearing = %q, want the raw URL fallback", got)
	}
}

func TestRenameSubscriptionIsScopedToTheOwner(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.RenameSubscription(ctx, f.bob.ID, sub.ID, "Hijacked"); !errors.Is(err, reader.ErrNotFound) {
		t.Errorf("RenameSubscription(bob) = %v, want ErrNotFound for someone else's subscription", err)
	}
}

func TestFavoritesMigrationAddsFaviconColumnAndTable(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	if _, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil); err != nil {
		t.Fatal(err)
	}

	var faviconURL string
	if err := f.db.QueryRowContext(ctx,
		`SELECT favicon_url FROM reader_feeds WHERE url = ?`, "https://example.com/feed.xml").
		Scan(&faviconURL); err != nil {
		t.Fatalf("favicon_url column missing or unreadable: %v", err)
	}
	if faviconURL != "" {
		t.Errorf("favicon_url = %q, want empty default", faviconURL)
	}

	if _, err := f.db.ExecContext(ctx,
		`INSERT INTO reader_feed_icons (url_hash, src_url) VALUES ('abc', 'https://example.com/favicon.ico')`); err != nil {
		t.Fatalf("reader_feed_icons missing or wrong shape: %v", err)
	}
}

func TestFeedIconHashIsStableAndURLSafe(t *testing.T) {
	a := reader.FaviconHash("https://example.com/favicon.ico")
	b := reader.FaviconHash("https://example.com/favicon.ico")
	c := reader.FaviconHash("https://example.com/other.ico")

	if a != b {
		t.Error("hash is not stable across calls")
	}
	if a == c {
		t.Error("different URLs hashed the same")
	}
	if len(a) != 32 {
		t.Errorf("hash is %d chars, want 32", len(a))
	}
}

func TestFeedIconByHashRefusesAnUnknownHash(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	_, err := f.store.FeedIconByHash(ctx, reader.FaviconHash("https://never-seen.example/x.ico"))
	if !errors.Is(err, reader.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestSaveFeedIconBytesCachesAndClearsFailures(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	hash := reader.FaviconHash("https://example.com/favicon.ico")
	if _, err := f.db.ExecContext(ctx,
		`INSERT INTO reader_feed_icons (url_hash, src_url) VALUES (?, ?)`,
		hash, "https://example.com/favicon.ico"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveFeedIconFailure(ctx, hash, "boom", now); err != nil {
		t.Fatal(err)
	}

	if err := f.store.SaveFeedIconBytes(ctx, hash, "image/x-icon", []byte{0x00, 0x01}, now); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.FeedIconByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Cached() {
		t.Error("Cached() = false after SaveFeedIconBytes")
	}
	if got.ContentType != "image/x-icon" {
		t.Errorf("ContentType = %q, want image/x-icon", got.ContentType)
	}
	if got.ErrorCount != 0 || got.LastError != "" {
		t.Errorf("failure not cleared: ErrorCount=%d LastError=%q", got.ErrorCount, got.LastError)
	}
}
