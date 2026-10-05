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
	"github.com/iliafrenkel/on-suite/internal/platform/article"
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

// reader_feed_icons has no retention job of its own (0008_favicons.sql), so
// unsubscribing from the last subscriber of a feed is the only place that
// ever cleans up its favicon cache row. Issue #258.
func TestUnsubscribeDropsTheOrphanedFeedsFavicon(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetFaviconIfEmpty(ctx, sub.FeedID, "https://example.com/favicon.ico"); err != nil {
		t.Fatal(err)
	}
	hash := reader.FaviconHash("https://example.com/favicon.ico")
	if _, err := f.store.FeedIconByHash(ctx, hash); err != nil {
		t.Fatalf("favicon row was not seeded: %v", err)
	}

	if err := f.store.Unsubscribe(ctx, f.alice.ID, sub.ID); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	if _, err := f.store.FeedIconByHash(ctx, hash); !errors.Is(err, reader.ErrNotFound) {
		t.Error("the orphaned feed's favicon row is still fetchable")
	}
}

// Two feeds can share one favicon URL (a multi-feed site), so dropping one
// of them must not delete the cache row the other still points at. Issue
// #258's own note: a naive DELETE ... WHERE url_hash = ? would get this
// wrong.
func TestUnsubscribeKeepsAFavoriteSharedByAnotherFeed(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	subA, err := f.store.Subscribe(ctx, f.alice.ID, "https://a.example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	subB, err := f.store.Subscribe(ctx, f.alice.ID, "https://b.example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	const shared = "https://example.com/shared-favicon.ico"
	if err := f.store.SetFaviconIfEmpty(ctx, subA.FeedID, shared); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetFaviconIfEmpty(ctx, subB.FeedID, shared); err != nil {
		t.Fatal(err)
	}
	hash := reader.FaviconHash(shared)

	if err := f.store.Unsubscribe(ctx, f.alice.ID, subA.ID); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	if _, err := f.store.FeedIconByHash(ctx, hash); err != nil {
		t.Errorf("shared favicon was purged while feed B still references it: %v", err)
	}

	// Now drop B too. Nothing references the favicon any more, so it must go.
	if err := f.store.Unsubscribe(ctx, f.alice.ID, subB.ID); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	if _, err := f.store.FeedIconByHash(ctx, hash); !errors.Is(err, reader.ErrNotFound) {
		t.Error("the now-orphaned shared favicon is still fetchable")
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

func TestCreateFolderDuplicateAndEmpty(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	// Empty name rejected with ErrInvalid.
	if _, err := f.store.CreateFolder(ctx, f.alice.ID, "   "); !errors.Is(err, reader.ErrInvalid) {
		t.Errorf("CreateFolder(empty) err = %v, want ErrInvalid", err)
	}

	// Creating first folder succeeds.
	if _, err := f.store.CreateFolder(ctx, f.alice.ID, "Tech"); err != nil {
		t.Fatalf("CreateFolder(Tech): %v", err)
	}

	// Same name for the same user fails with ErrFolderExists and ErrInvalid.
	_, err := f.store.CreateFolder(ctx, f.alice.ID, "Tech")
	if !errors.Is(err, reader.ErrFolderExists) {
		t.Errorf("CreateFolder duplicate err = %v, want ErrFolderExists", err)
	}
	if !errors.Is(err, reader.ErrInvalid) {
		t.Errorf("CreateFolder duplicate err = %v, want ErrInvalid", err)
	}

	// Different user can use the same folder name.
	if _, err := f.store.CreateFolder(ctx, f.bob.ID, "Tech"); err != nil {
		t.Errorf("CreateFolder for bob with same name failed: %v", err)
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

// ItemsForScope draws every list render (up to 200 rows), and viewList never
// touches a body field — but Store.Item (the article pane) does, so it is
// easy for a future change to accidentally widen ItemsForScope's SELECT back
// out to itemColumns and silently start moving full_html (unbounded, up to
// 2MB per article) through SQLite again on every list render. This pins the
// omission directly against a real row, not just an empty-fixture coincidence.
// Issue #230.
func TestItemsForScopeOmitsBodyColumns(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
		GUID: "g1", Title: "T", PublishedAt: time.Now().UTC(),
		SummaryHTML: "<p>summary</p>", ContentHTML: "<p>content</p>",
	}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	items, err := f.store.ItemsForSubscription(ctx, f.alice.ID, sub.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveFullArticle(ctx, f.alice.ID, items[0].ID, article.Extracted{
		HTML: "<p>full body</p>", TextLength: 500,
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	// Re-fetch through the list query now that the row genuinely has all
	// three body fields populated.
	items, err = f.store.ItemsForSubscription(ctx, f.alice.ID, sub.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	got := items[0]
	if got.SummaryHTML != "" || got.ContentHTML != "" || got.FullHTML != "" {
		t.Errorf("ItemsForScope returned body fields it must not select: SummaryHTML=%q ContentHTML=%q FullHTML=%q",
			got.SummaryHTML, got.ContentHTML, got.FullHTML)
	}
	// The fields the list actually renders must still be right.
	if got.Title != "T" || got.GUID != "g1" {
		t.Errorf("list item lost fields it does need: Title=%q GUID=%q", got.Title, got.GUID)
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

// TestSaveFullArticleRejectsAnItemTheUserCannotSee pins #372: the visibility
// check must reject a non-subscriber even though it now runs inside
// SaveFullArticle's own transaction rather than before it.
func TestSaveFullArticleRejectsAnItemTheUserCannotSee(t *testing.T) {
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

	err = f.store.SaveFullArticle(ctx, f.bob.ID, items[0].ID, article.Extracted{
		HTML: "<p>full body</p>", TextLength: 500,
	}, time.Now().UTC())
	if !errors.Is(err, reader.ErrNotFound) {
		t.Fatalf("bob saved a full article on a feed he is not subscribed to: err = %v, want ErrNotFound", err)
	}

	var fullHTML sql.NullString
	if err := f.db.QueryRowContext(ctx,
		`SELECT full_html FROM reader_items WHERE id = ?`, items[0].ID,
	).Scan(&fullHTML); err != nil {
		t.Fatal(err)
	}
	if fullHTML.String != "" {
		t.Errorf("full_html = %q, want empty after a rejected save", fullHTML.String)
	}
}

// TestClearFullArticleRejectsAnItemTheUserCannotSee pins #372 the same way,
// for ClearFullArticle.
func TestClearFullArticleRejectsAnItemTheUserCannotSee(t *testing.T) {
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
	if err := f.store.SaveFullArticle(ctx, f.alice.ID, items[0].ID, article.Extracted{
		HTML: "<p>full body</p>", TextLength: 500,
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	err = f.store.ClearFullArticle(ctx, f.bob.ID, items[0].ID)
	if !errors.Is(err, reader.ErrNotFound) {
		t.Fatalf("bob cleared alice's full article: err = %v, want ErrNotFound", err)
	}

	var fullHTML string
	if err := f.db.QueryRowContext(ctx,
		`SELECT full_html FROM reader_items WHERE id = ?`, items[0].ID,
	).Scan(&fullHTML); err != nil {
		t.Fatal(err)
	}
	if fullHTML != "<p>full body</p>" {
		t.Errorf("full_html = %q, want alice's article unchanged after bob's rejected clear", fullHTML)
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

func TestFaviconMigrationAddsFaviconColumnAndTable(t *testing.T) {
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

func TestSetFaviconIfEmptySetsAndCachesTheURL(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.SetFaviconIfEmpty(ctx, sub.FeedID, "https://example.com/favicon.ico"); err != nil {
		t.Fatal(err)
	}

	feed, err := f.store.FeedByID(ctx, sub.FeedID)
	if err != nil {
		t.Fatal(err)
	}
	if feed.FaviconURL != "https://example.com/favicon.ico" {
		t.Errorf("FaviconURL = %q, want the URL passed in", feed.FaviconURL)
	}

	hash := reader.FaviconHash("https://example.com/favicon.ico")
	if _, err := f.store.FeedIconByHash(ctx, hash); err != nil {
		t.Errorf("reader_feed_icons row was not created: %v", err)
	}
}

func TestSetFaviconIfEmptyDoesNotOverwrite(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetFaviconIfEmpty(ctx, sub.FeedID, "https://example.com/first.ico"); err != nil {
		t.Fatal(err)
	}

	if err := f.store.SetFaviconIfEmpty(ctx, sub.FeedID, "https://example.com/second.ico"); err != nil {
		t.Fatal(err)
	}

	feed, err := f.store.FeedByID(ctx, sub.FeedID)
	if err != nil {
		t.Fatal(err)
	}
	if feed.FaviconURL != "https://example.com/first.ico" {
		t.Errorf("FaviconURL = %q, a second call must not overwrite the first", feed.FaviconURL)
	}
}

// Reading the site's homepage is better evidence than the /favicon.ico guess
// (#451), so SetPageFavicon replaces it — unlike SetFaviconIfEmpty — and
// drops the guess's cache row once nothing points at it.
func TestSetPageFaviconReplacesTheGuessAndMarksTheFeedChecked(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	const guess = "https://example.com/favicon.ico"
	const page = "https://example.com/static/icon.png"
	if err := f.store.SetFaviconIfEmpty(ctx, sub.FeedID, guess); err != nil {
		t.Fatal(err)
	}

	if err := f.store.SetPageFavicon(ctx, sub.FeedID, page); err != nil {
		t.Fatal(err)
	}

	feed, err := f.store.FeedByID(ctx, sub.FeedID)
	if err != nil {
		t.Fatal(err)
	}
	if feed.FaviconURL != page {
		t.Errorf("FaviconURL = %q, want the page's icon %q", feed.FaviconURL, page)
	}
	if !feed.FaviconPageChecked {
		t.Error("FaviconPageChecked = false, want true after a page favicon is saved")
	}
	if _, err := f.store.FeedIconByHash(ctx, reader.FaviconHash(page)); err != nil {
		t.Errorf("the page icon's reader_feed_icons row was not created: %v", err)
	}
	if _, err := f.store.FeedIconByHash(ctx, reader.FaviconHash(guess)); !errors.Is(err, reader.ErrNotFound) {
		t.Errorf("the replaced guess's icon row is still there (err = %v)", err)
	}
}

func TestSetPageFaviconKeepsAnOldIconAnotherFeedStillUses(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	subA, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/a.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	subB, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/b.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	const shared = "https://example.com/favicon.ico"
	for _, id := range []int64{subA.FeedID, subB.FeedID} {
		if err := f.store.SetFaviconIfEmpty(ctx, id, shared); err != nil {
			t.Fatal(err)
		}
	}

	if err := f.store.SetPageFavicon(ctx, subA.FeedID, "https://example.com/static/icon.png"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.FeedIconByHash(ctx, reader.FaviconHash(shared)); err != nil {
		t.Errorf("feed B's icon row was dropped while B still uses it: %v", err)
	}
}

// An empty URL means the homepage could not be read: the feed keeps whatever
// favicon it had and is only marked checked, so the poller's one-off repair
// does not try again on every poll.
func TestSetPageFaviconWithNoURLOnlyMarksTheFeedChecked(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	const guess = "https://example.com/favicon.ico"
	if err := f.store.SetFaviconIfEmpty(ctx, sub.FeedID, guess); err != nil {
		t.Fatal(err)
	}

	if err := f.store.SetPageFavicon(ctx, sub.FeedID, ""); err != nil {
		t.Fatal(err)
	}

	feed, err := f.store.FeedByID(ctx, sub.FeedID)
	if err != nil {
		t.Fatal(err)
	}
	if feed.FaviconURL != guess {
		t.Errorf("FaviconURL = %q, want the existing %q kept", feed.FaviconURL, guess)
	}
	if !feed.FaviconPageChecked {
		t.Error("FaviconPageChecked = false, want true")
	}
	if _, err := f.store.FeedIconByHash(ctx, reader.FaviconHash(guess)); err != nil {
		t.Errorf("the kept favicon's icon row was dropped: %v", err)
	}
}

func TestSubscriptionFaviconPath(t *testing.T) {
	withURL := reader.Subscription{FaviconURL: "https://example.com/favicon.ico"}
	if withURL.FaviconPath() != "/reader/favicon/"+reader.FaviconHash("https://example.com/favicon.ico") {
		t.Errorf("FaviconPath() = %q", withURL.FaviconPath())
	}

	without := reader.Subscription{}
	if without.FaviconPath() != "" {
		t.Errorf("FaviconPath() = %q, want empty when FaviconURL is empty", without.FaviconPath())
	}
}

func TestTreeLoadsFaviconURL(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetFaviconIfEmpty(ctx, sub.FeedID, "https://example.com/favicon.ico"); err != nil {
		t.Fatal(err)
	}

	tree, err := f.store.Tree(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root) != 1 || tree.Root[0].FaviconURL != "https://example.com/favicon.ico" {
		t.Errorf("Tree did not load FaviconURL: %+v", tree.Root)
	}
}

// A favicon the proxy has given up on, for now or for good, is not offered
// to the template, so the tree draws the RSS glyph instead of an <img> that
// flashes broken until reader.js swaps it out (#455). Once the backoff
// window has passed the proxy will try again, so the tree offers it again.
func TestTreeHidesAFaviconTheProxyWillNotServe(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	f.store.SetClock(func() time.Time { return now })

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	const icon = "https://example.com/favicon.ico"
	if err := f.store.SetFaviconIfEmpty(ctx, sub.FeedID, icon); err != nil {
		t.Fatal(err)
	}
	hash := reader.FaviconHash(icon)
	path := func() string {
		t.Helper()
		tree, err := f.store.Tree(ctx, f.alice.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(tree.Root) != 1 {
			t.Fatalf("tree root = %+v", tree.Root)
		}
		return tree.Root[0].FaviconPath()
	}

	if path() == "" {
		t.Error("a favicon not fetched yet should still be offered")
	}

	if err := f.store.SaveFeedIconFailure(ctx, hash, "boom", now); err != nil {
		t.Fatal(err)
	}
	if got := path(); got != "" {
		t.Errorf("FaviconPath() = %q inside the retry backoff, want empty", got)
	}

	now = now.Add(2 * time.Hour)
	if path() == "" {
		t.Error("a favicon past its retry backoff should be offered again")
	}

	for range 2 {
		if err := f.store.SaveFeedIconFailure(ctx, hash, "boom", now.Add(-2*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if got := path(); got != "" {
		t.Errorf("FaviconPath() = %q after the last attempt, want empty", got)
	}

	if err := f.store.SaveFeedIconBytes(ctx, hash, "image/x-icon", []byte{0x00, 0x01}, now); err != nil {
		t.Fatal(err)
	}
	if path() == "" {
		t.Error("a cached favicon should be offered")
	}
}

// Moving a feed keeps the subscription itself, so its read and starred
// state survive; unsubscribing and re-adding, the only way before #420,
// threw that away.
func TestMoveSubscriptionMovesBetweenFoldersAndToTheRoot(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	news, err := f.store.CreateFolder(ctx, f.alice.ID, "News")
	if err != nil {
		t.Fatal(err)
	}
	tech, err := f.store.CreateFolder(ctx, f.alice.ID, "Tech")
	if err != nil {
		t.Fatal(err)
	}
	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", &news.ID)
	if err != nil {
		t.Fatal(err)
	}

	folderOf := func() *int64 {
		t.Helper()
		tree, err := f.store.Tree(ctx, f.alice.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, folder := range tree.Folders {
			for _, s := range folder.Subs {
				if s.ID == sub.ID {
					id := folder.ID
					return &id
				}
			}
		}
		for _, s := range tree.Root {
			if s.ID == sub.ID {
				return nil
			}
		}
		t.Fatal("subscription is missing from the tree")
		return nil
	}

	if err := f.store.MoveSubscription(ctx, f.alice.ID, sub.ID, &tech.ID); err != nil {
		t.Fatalf("MoveSubscription(Tech): %v", err)
	}
	if got := folderOf(); got == nil || *got != tech.ID {
		t.Errorf("after moving to Tech, folder = %v, want %d", got, tech.ID)
	}

	if err := f.store.MoveSubscription(ctx, f.alice.ID, sub.ID, nil); err != nil {
		t.Fatalf("MoveSubscription(root): %v", err)
	}
	if got := folderOf(); got != nil {
		t.Errorf("after moving to the root, folder = %d, want none", *got)
	}
}

func TestMoveSubscriptionIsScopedToTheOwner(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	aliceFolder, err := f.store.CreateFolder(ctx, f.alice.ID, "Alice's")
	if err != nil {
		t.Fatal(err)
	}
	bobFolder, err := f.store.CreateFolder(ctx, f.bob.ID, "Bob's")
	if err != nil {
		t.Fatal(err)
	}
	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", &aliceFolder.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.MoveSubscription(ctx, f.bob.ID, sub.ID, &bobFolder.ID); !errors.Is(err, reader.ErrNotFound) {
		t.Errorf("MoveSubscription(bob, alice's sub) = %v, want ErrNotFound", err)
	}
	// A folder id is as much somebody's property as a subscription id: the
	// foreign key alone would happily accept Bob's.
	if err := f.store.MoveSubscription(ctx, f.alice.ID, sub.ID, &bobFolder.ID); !errors.Is(err, reader.ErrNotFound) {
		t.Errorf("MoveSubscription(alice, into bob's folder) = %v, want ErrNotFound", err)
	}
	tree, err := f.store.Tree(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Folders) != 1 || len(tree.Folders[0].Subs) != 1 {
		t.Errorf("a refused move changed the tree: %+v", tree)
	}
}

// folder_id's foreign key only checks that a folder exists, so Subscribe
// checks it is the subscriber's own, as MoveSubscription does (#439). A
// folder that is gone and one that is someone else's are the same error,
// and neither leaves a subscription behind.
func TestStoreSubscribeRefusesAFolderThatIsNotTheUsers(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	bobFolder, err := f.store.CreateFolder(ctx, f.bob.ID, "Bob's")
	if err != nil {
		t.Fatal(err)
	}
	gone := bobFolder.ID + 100

	for _, tc := range []struct {
		name     string
		folderID int64
	}{
		{"another user's folder", bobFolder.ID},
		{"a folder that does not exist", gone},
	} {
		id := tc.folderID
		if _, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", &id); !errors.Is(err, reader.ErrNoSuchFolder) {
			t.Errorf("%s: Subscribe = %v, want ErrNoSuchFolder", tc.name, err)
		}
	}
	tree, err := f.store.Tree(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root) != 0 || len(tree.Folders) != 0 {
		t.Errorf("a refused Subscribe left something behind: %+v", tree)
	}
}
