package notes_test

import (
	"context"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

func TestDueReturnsOnlyNodesWithADueDate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	withDue := f.mk(t, notes.RootID, "has a date")
	f.mk(t, notes.RootID, "no date")

	if err := f.store.SetDue(ctx, f.alice.ID, withDue.ID, "2026-03-05"); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Due(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != withDue.ID {
		t.Fatalf("Due = %+v, want only the one with a date", got)
	}
}

func TestDueExcludesDoneNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "done but due")
	if err := f.store.SetDue(ctx, f.alice.ID, n.ID, "2026-03-05"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetDone(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Due(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Due = %+v, want none — it is done", got)
	}
}

func TestDueDoesNotLeakAnotherUsersNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// mkFor, not mk: the node has to be bob's own for SetDue as bob to
	// touch a row at all.
	bobs := f.mkFor(t, f.bob.ID, notes.RootID, "bob's")
	if err := f.store.SetDue(ctx, f.bob.ID, bobs.ID, "2026-03-05"); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Due(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("alice's Due = %+v, want none", got)
	}
}

func TestDueIsOrderedByDate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	later := f.mk(t, notes.RootID, "later")
	sooner := f.mk(t, notes.RootID, "sooner")
	if err := f.store.SetDue(ctx, f.alice.ID, later.ID, "2026-04-01"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetDue(ctx, f.alice.ID, sooner.ID, "2026-03-05"); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Due(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != sooner.ID || got[1].ID != later.ID {
		t.Fatalf("Due = %+v, want the earlier date first", got)
	}
}

func TestGroupByDueBucketsRelativeToToday(t *testing.T) {
	today := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	rows := []struct {
		id  int64
		due string
	}{
		{1, "2026-03-01"}, // overdue
		{2, "2026-03-10"}, // today
		{3, "2026-03-14"}, // this week (today + 4 days)
		{4, "2026-03-16"}, // this week (today + 6 days, inclusive boundary)
		{5, "2026-03-17"}, // later (today + 7 days)
	}
	var in []notes.DueRow
	for _, r := range rows {
		in = append(in, notes.DueRow{Node: notes.Node{ID: r.id, DueOn: r.due}})
	}

	got := notes.GroupByDue(in, today)
	check := func(name string, group []notes.DueRow, wantID int64) {
		if len(group) != 1 || group[0].ID != wantID {
			t.Errorf("%s = %+v, want exactly id %d", name, group, wantID)
		}
	}
	check("Overdue", got.Overdue, 1)
	check("Today", got.Today, 2)
	if len(got.ThisWeek) != 2 || got.ThisWeek[0].ID != 3 || got.ThisWeek[1].ID != 4 {
		t.Errorf("ThisWeek = %+v, want ids 3 and 4", got.ThisWeek)
	}
	check("Later", got.Later, 5)

	if !got.Overdue[0].Overdue {
		t.Error("the overdue row's own Overdue field is false")
	}
	if got.Today[0].Overdue {
		t.Error("today's row is marked Overdue")
	}
}

// TestDueExcludesArchivedNodes is spec §11 and §13.
func TestDueExcludesArchivedNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "put away but due")
	if err := f.store.SetDue(ctx, f.alice.ID, n.ID, "2026-03-05"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetArchived(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Due(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Due = %+v, want none — it is archived", got)
	}
}

// TestDueExcludesADescendantOfAnArchivedNode mirrors the search case: the
// due node itself is not archived, but an ancestor is.
func TestDueExcludesADescendantOfAnArchivedNode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "put away")
	child := f.mk(t, parent.ID, "due")
	if err := f.store.SetDue(ctx, f.alice.ID, child.ID, "2026-03-05"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetArchived(ctx, f.alice.ID, parent.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Due(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Due = %+v, want none — it sits under an archived node", got)
	}
}

func TestDueBadgeCountCountsOverdueAndToday(t *testing.T) {
	today := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	rows := []notes.Node{
		{ID: 1, DueOn: "2026-03-01"}, // overdue
		{ID: 2, DueOn: "2026-03-10"}, // today
		{ID: 3, DueOn: "2026-03-14"}, // this week, not counted
		{ID: 4, DueOn: "2026-04-01"}, // later, not counted
	}

	got := notes.DueBadgeCount(rows, today)
	if got != 2 {
		t.Errorf("DueBadgeCount = %d, want 2 (overdue + today only)", got)
	}
}

func TestDueBadgeCountOfNoRowsIsZero(t *testing.T) {
	got := notes.DueBadgeCount(nil, time.Now())
	if got != 0 {
		t.Errorf("DueBadgeCount(nil) = %d, want 0", got)
	}
}
