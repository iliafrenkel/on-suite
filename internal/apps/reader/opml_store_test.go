package reader_test

import (
	"context"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

func TestImportOPMLCreatesFoldersAndSubscriptions(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	res, err := f.store.ImportOPML(ctx, f.alice.ID, []reader.OPMLEntry{
		{FeedURL: "https://a.example/feed", Title: "A", Folder: "Tech"},
		{FeedURL: "https://b.example/feed", Title: "B", Folder: "Tech"},
		{FeedURL: "https://c.example/feed", Title: "C"},
	})
	if err != nil {
		t.Fatalf("ImportOPML: %v", err)
	}
	if res.Added != 3 {
		t.Errorf("Added = %d, want 3", res.Added)
	}

	tree, err := f.store.Tree(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Folders) != 1 || tree.Folders[0].Name != "Tech" {
		t.Fatalf("folders = %+v, want one named Tech", tree.Folders)
	}
	if len(tree.Folders[0].Subs) != 2 {
		t.Errorf("Tech has %d feeds, want 2", len(tree.Folders[0].Subs))
	}
	if len(tree.Root) != 1 {
		t.Errorf("root has %d feeds, want 1", len(tree.Root))
	}
}

// Importing the same file twice must be a no-op, not a pile of duplicates —
// re-importing after a failed attempt is the obvious thing to do.
func TestImportOPMLIsIdempotent(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	entries := []reader.OPMLEntry{{FeedURL: "https://a.example/feed", Title: "A", Folder: "Tech"}}

	if _, err := f.store.ImportOPML(ctx, f.alice.ID, entries); err != nil {
		t.Fatal(err)
	}
	res, err := f.store.ImportOPML(ctx, f.alice.ID, entries)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 0 || res.Existing != 1 {
		t.Errorf("second import: Added=%d Existing=%d, want 0 and 1", res.Added, res.Existing)
	}

	tree, err := f.store.Tree(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Folders) != 1 {
		t.Errorf("re-import created %d folders, want 1", len(tree.Folders))
	}
}

// An import adds. It must never remove something the file does not mention:
// that would be sync, which nobody asked for, and it would eat subscriptions.
func TestImportOPMLNeverRemovesExistingSubscriptions(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	keep, err := f.store.Subscribe(ctx, f.alice.ID, "https://keep.example/feed", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ImportOPML(ctx, f.alice.ID, []reader.OPMLEntry{
		{FeedURL: "https://new.example/feed", Title: "New"},
	}); err != nil {
		t.Fatal(err)
	}

	tree, err := f.store.Tree(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range tree.Root {
		if s.ID == keep.ID {
			found = true
		}
	}
	if !found {
		t.Error("an existing subscription was removed by an import that did not mention it")
	}
}

// One bad URL in a forty-feed file must not cost the other thirty-nine.
func TestImportOPMLReportsBadEntriesAndKeepsGoing(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	res, err := f.store.ImportOPML(ctx, f.alice.ID, []reader.OPMLEntry{
		{FeedURL: "https://good.example/feed", Title: "Good"},
		{FeedURL: "not a url at all", Title: "Bad"},
		{FeedURL: "ftp://wrong.example/feed", Title: "Wrong scheme"},
	})
	if err != nil {
		t.Fatalf("ImportOPML returned %v; one bad entry must not fail the import", err)
	}
	if res.Added != 1 {
		t.Errorf("Added = %d, want 1", res.Added)
	}
	if res.Failed != 2 {
		t.Errorf("Failed = %d, want 2", res.Failed)
	}
	if len(res.Errors) != 2 {
		t.Errorf("Errors has %d entries, want 2: %v", len(res.Errors), res.Errors)
	}
}

func TestImportOPMLIsScopedToOneUser(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	if _, err := f.store.ImportOPML(ctx, f.alice.ID, []reader.OPMLEntry{
		{FeedURL: "https://a.example/feed", Title: "A", Folder: "Tech"},
	}); err != nil {
		t.Fatal(err)
	}
	tree, err := f.store.Tree(ctx, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Folders) != 0 || len(tree.Root) != 0 {
		t.Errorf("alice's import appeared in bob's tree: %+v", tree)
	}
}
