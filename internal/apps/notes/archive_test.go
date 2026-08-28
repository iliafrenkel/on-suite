package notes_test

import (
	"context"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

func TestArchiveListsATopLevelArchivedNode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "put away")
	f.mk(t, notes.RootID, "still out")

	if err := f.store.SetArchived(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Archive(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != n.ID {
		t.Fatalf("Archive = %+v, want only %d", got, n.ID)
	}
}

// TestArchiveListsOnlyTheRootOfAnArchivedSubtree is spec §13: "archived
// nodes whose parent is not itself archived — the roots of what was put
// away." Archiving a parent does not write archived_at onto its child
// (Task 1), so the child would independently qualify as "archived" only if
// it were archived in its own right; here it never is, so it must not
// appear as a second entry alongside its already-archived parent.
func TestArchiveListsOnlyTheRootOfAnArchivedSubtree(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "put away")
	f.mk(t, parent.ID, "child")

	if err := f.store.SetArchived(ctx, f.alice.ID, parent.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Archive(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != parent.ID {
		t.Fatalf("Archive = %+v, want only the parent %d", got, parent.ID)
	}
}

// TestArchiveListsANestedNodeWhoseParentIsNotArchived: only the node
// actually marked archived_at is a "root of what was put away" — an
// ancestor further up not being archived is exactly what makes it one.
func TestArchiveListsANestedNodeWhoseParentIsNotArchived(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "not archived")
	child := f.mk(t, parent.ID, "put away")

	if err := f.store.SetArchived(ctx, f.alice.ID, child.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Archive(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != child.ID {
		t.Fatalf("Archive = %+v, want only the child %d", got, child.ID)
	}
}

func TestArchiveDoesNotLeakAnotherUsersNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	bobs := f.mkFor(t, f.bob.ID, notes.RootID, "bob's")
	if err := f.store.SetArchived(ctx, f.bob.ID, bobs.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Archive(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("alice's Archive = %+v, want none", got)
	}
}

func TestArchiveWithNothingArchivedIsEmpty(t *testing.T) {
	f := newFixture(t)
	f.mk(t, notes.RootID, "never archived")

	got, err := f.store.Archive(context.Background(), f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Archive = %+v, want none", got)
	}
}
