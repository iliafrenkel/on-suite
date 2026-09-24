package notes_test

import (
	"context"
	"testing"
	"time"

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

	got, err := f.store.Archive(ctx, f.alice.ID, "")
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

	got, err := f.store.Archive(ctx, f.alice.ID, "")
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

	got, err := f.store.Archive(ctx, f.alice.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != child.ID {
		t.Fatalf("Archive = %+v, want only the child %d", got, child.ID)
	}
}

// TestArchiveExcludesADoublyNestedArchivedDescendant is issue #109: A
// archived, B (child of A, not archived), C (child of B, archived). C's own
// direct parent (B) is not archived, so a direct-parent-only check would
// still list C alongside A even though restoring A already brings C back —
// C has no restore action of its own to be listed for. archived_below's
// recursive walk must catch the archived ancestor (A) sitting above C's
// direct parent (B).
func TestArchiveExcludesADoublyNestedArchivedDescendant(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "A")
	b := f.mk(t, a.ID, "B")
	c := f.mk(t, b.ID, "C")

	if err := f.store.SetArchived(ctx, f.alice.ID, a.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetArchived(ctx, f.alice.ID, c.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Archive(ctx, f.alice.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("Archive = %+v, want only A (%d), not C", got, a.ID)
	}
}

// TestArchiveOrdersChronologicallyAcrossWholeAndFractionalSeconds is issue
// #108 and #356: "earlier" is archived on a whole second, "later" half a
// second after it and "latest" 4µs after that. RFC3339Nano wrote the first
// as "...T10:00:00Z", which sorted after "...T10:00:00.5Z" as text, and
// the julianday() workaround for that resolved only milliseconds, tying the
// last two. Stored as db.TimeLayout, a plain ORDER BY gets all three right.
func TestArchiveOrdersChronologicallyAcrossWholeAndFractionalSeconds(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	earlier := f.mk(t, notes.RootID, "earlier, whole second")
	later := f.mk(t, notes.RootID, "later, fractional second")
	latest := f.mk(t, notes.RootID, "latest, 4µs after later")

	second := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	for _, step := range []struct {
		id int64
		at time.Time
	}{
		{earlier.ID, second},
		{later.ID, second.Add(500 * time.Millisecond)},
		{latest.ID, second.Add(500*time.Millisecond + 4*time.Microsecond)},
	} {
		f.store.SetClock(func() time.Time { return step.at })
		if err := f.store.SetArchived(ctx, f.alice.ID, step.id, true); err != nil {
			t.Fatal(err)
		}
	}

	got, err := f.store.Archive(ctx, f.alice.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != latest.ID || got[1].ID != later.ID || got[2].ID != earlier.ID {
		t.Fatalf("Archive = %+v, want [latest, later, earlier] (most recent first)", got)
	}
}

func TestArchiveDoesNotLeakAnotherUsersNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	bobs := f.mkFor(t, f.bob.ID, notes.RootID, "bob's")
	if err := f.store.SetArchived(ctx, f.bob.ID, bobs.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Archive(ctx, f.alice.ID, "")
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

	got, err := f.store.Archive(context.Background(), f.alice.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Archive = %+v, want none", got)
	}
}

func TestArchiveWithAQueryOnlyReturnsMatchingRows(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	match := f.mk(t, notes.RootID, "old milk carton")
	if err := f.store.SetArchived(ctx, f.alice.ID, match.ID, true); err != nil {
		t.Fatal(err)
	}
	other := f.mk(t, notes.RootID, "old receipts")
	if err := f.store.SetArchived(ctx, f.alice.ID, other.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Archive(ctx, f.alice.ID, "milk")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != match.ID {
		t.Fatalf("Archive(query=milk) = %+v, want just the milk bullet", got)
	}
}

func TestArchiveWithNoQueryReturnsEverything(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "anything")
	if err := f.store.SetArchived(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Archive(ctx, f.alice.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("Archive(query=\"\") = %+v, want the one archived bullet", got)
	}
}
