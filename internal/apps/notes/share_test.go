package notes_test

import (
	"context"
	"errors"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

func TestShareMintsAnUnguessableSlug(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "shared")

	slug, err := f.store.Share(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatalf("Share: %v", err)
	}
	if len(slug) < 20 {
		t.Errorf("slug %q is too short to be unguessable", slug)
	}

	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ShareSlug != slug {
		t.Errorf("ByID's ShareSlug = %q, want %q", got.ShareSlug, slug)
	}
	if !got.Shared() {
		t.Error("Shared() is false right after Share")
	}
}

func TestByShareSlugFindsTheSharedNode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "shared")
	slug, err := f.store.Share(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatal(err)
	}

	got, err := f.store.ByShareSlug(ctx, slug)
	if err != nil {
		t.Fatalf("ByShareSlug: %v", err)
	}
	if got.ID != n.ID {
		t.Errorf("ByShareSlug returned node %d, want %d", got.ID, n.ID)
	}
	if got.UserID != f.alice.ID {
		t.Errorf("ByShareSlug's UserID = %d, want %d", got.UserID, f.alice.ID)
	}
}

func TestByShareSlugRejectsEmptyAndUnknown(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, slug := range []string{"", "never-minted"} {
		if _, err := f.store.ByShareSlug(ctx, slug); !errors.Is(err, notes.ErrNotFound) {
			t.Errorf("ByShareSlug(%q) = %v, want ErrNotFound", slug, err)
		}
	}
}

func TestUnshareClearsTheSlug(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "shared")
	slug, err := f.store.Share(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.Unshare(ctx, f.alice.ID, n.ID); err != nil {
		t.Fatalf("Unshare: %v", err)
	}

	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Shared() {
		t.Error("the node is still shared after Unshare")
	}
	if _, err := f.store.ByShareSlug(ctx, slug); !errors.Is(err, notes.ErrNotFound) {
		t.Errorf("ByShareSlug(revoked slug) = %v, want ErrNotFound", err)
	}
}

// TestReSharingMintsAFreshSlugAndKillsTheOldOne is the point of choosing a
// revocable share model — spec §15.
func TestReSharingMintsAFreshSlugAndKillsTheOldOne(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "shared")

	first, err := f.store.Share(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.store.Share(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("re-sharing reissued the same slug")
	}
	if _, err := f.store.ByShareSlug(ctx, first); !errors.Is(err, notes.ErrNotFound) {
		t.Error("the old slug still resolves after re-sharing")
	}
	got, err := f.store.ByShareSlug(ctx, second)
	if err != nil || got.ID != n.ID {
		t.Errorf("ByShareSlug(second) = %+v, %v", got, err)
	}
}

func TestShareRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "bob's") // seeded as alice by newFixture's mk; use mkFor for bob

	_, err := f.store.Share(context.Background(), f.bob.ID, n.ID)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("Share on another user's node = %v, want ErrNotFound", err)
	}
}

func TestUnshareRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mkFor(t, f.bob.ID, notes.RootID, "bob's")
	if _, err := f.store.Share(ctx, f.bob.ID, n.ID); err != nil {
		t.Fatal(err)
	}

	err := f.store.Unshare(ctx, f.alice.ID, n.ID)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("Unshare on another user's node = %v, want ErrNotFound", err)
	}
}

func TestSharedSubtreeExcludesArchivedAndDoneDescendants(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	root := f.mk(t, notes.RootID, "root")
	kept := f.mk(t, root.ID, "kept")
	archived := f.mk(t, root.ID, "archived")
	done := f.mk(t, root.ID, "done")

	if err := f.store.SetArchived(ctx, f.alice.ID, archived.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetDone(ctx, f.alice.ID, done.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.SharedSubtree(ctx, f.alice.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != kept.ID {
		t.Fatalf("SharedSubtree = %+v, want only %d", got, kept.ID)
	}
}

func TestSharedSubtreeIgnoresCollapsedState(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	root := f.mk(t, notes.RootID, "root")
	child := f.mk(t, root.ID, "child")
	f.mk(t, child.ID, "grandchild")

	if err := f.store.SetCollapsed(ctx, f.alice.ID, child.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.SharedSubtree(ctx, f.alice.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("SharedSubtree = %+v, want 2 rows despite the collapsed child", got)
	}
}

func TestSharedSubtreeDoesNotIncludeTheRootItself(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	root := f.mk(t, notes.RootID, "root")
	child := f.mk(t, root.ID, "child")

	got, err := f.store.SharedSubtree(ctx, f.alice.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != child.ID {
		t.Fatalf("SharedSubtree = %+v, want only the child %d, not the root", got, child.ID)
	}
}

func TestSharedSubtreeOrdersPreOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	root := f.mk(t, notes.RootID, "root")
	a := f.mk(t, root.ID, "a")
	f.mk(t, a.ID, "a1")
	f.mk(t, root.ID, "b")

	got, err := f.store.SharedSubtree(ctx, f.alice.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, n := range got {
		titles = append(titles, n.Title)
	}
	want := []string{"a", "a1", "b"}
	if !equalStrings(titles, want) {
		t.Fatalf("order = %v, want %v", titles, want)
	}
}
