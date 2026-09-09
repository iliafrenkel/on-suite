package reader_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// fixture is a migrated database with two users, so every owner-scoping test
// has somebody else to be confused with.
type fixture struct {
	store *reader.Store
	db    *sql.DB
	alice auth.User
	bob   auth.User
}

func newFixture(t *testing.T) *fixture {
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
	hash, err := auth.HashPassword("a-sufficiently-long-password")
	if err != nil {
		t.Fatal(err)
	}
	alice, err := users.CreateUser(ctx, "alice", hash, true)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := users.CreateUser(ctx, "bob", hash, false)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{store: reader.NewStore(handle), db: handle, alice: alice, bob: bob}
}

func TestSubscribeSharesOneFeedRow(t *testing.T) {
	f := newFixture(t)
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
	f := newFixture(t)
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
	f := newFixture(t)
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
	f := newFixture(t)
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
	f := newFixture(t)
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
	f := newFixture(t)
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
