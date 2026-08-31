package notes_test

import (
	"context"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

func TestStatsCountsAcrossEveryUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "alice's")
	f.mkFor(t, f.bob.ID, notes.RootID, "bob's")

	stats, err := f.store.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := statValue(t, stats, "Bullets")
	if got != "2" {
		t.Errorf(`"Bullets" = %q, want "2" (one per user)`, got)
	}
}

func TestStatsCountsDoneArchivedAndShared(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	done := f.mk(t, notes.RootID, "done")
	archived := f.mk(t, notes.RootID, "archived")
	shared := f.mk(t, notes.RootID, "shared")
	f.mk(t, notes.RootID, "plain")

	if err := f.store.SetDone(ctx, f.alice.ID, done.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetArchived(ctx, f.alice.ID, archived.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Share(ctx, f.alice.ID, shared.ID); err != nil {
		t.Fatal(err)
	}

	stats, err := f.store.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := statValue(t, stats, "Bullets"); got != "4" {
		t.Errorf(`"Bullets" = %q, want "4"`, got)
	}
	if got := statValue(t, stats, "Done"); got != "1" {
		t.Errorf(`"Done" = %q, want "1"`, got)
	}
	if got := statValue(t, stats, "Archived"); got != "1" {
		t.Errorf(`"Archived" = %q, want "1"`, got)
	}
	if got := statValue(t, stats, "Shared"); got != "1" {
		t.Errorf(`"Shared" = %q, want "1"`, got)
	}
}

// TestStatsCountsOverdueLikeTheDueList mirrors GroupByDue's own definition
// of overdue (due.go): due, not done, not archived, and strictly before
// today.
func TestStatsCountsOverdueLikeTheDueList(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.store.SetClock(func() time.Time { return time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC) })

	overdue := f.mk(t, notes.RootID, "overdue")
	if err := f.store.SetDue(ctx, f.alice.ID, overdue.ID, "2026-06-01"); err != nil {
		t.Fatal(err)
	}
	notOverdue := f.mk(t, notes.RootID, "due later")
	if err := f.store.SetDue(ctx, f.alice.ID, notOverdue.ID, "2026-07-01"); err != nil {
		t.Fatal(err)
	}
	doneOverdue := f.mk(t, notes.RootID, "done and overdue")
	if err := f.store.SetDue(ctx, f.alice.ID, doneOverdue.ID, "2026-06-01"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetDone(ctx, f.alice.ID, doneOverdue.ID, true); err != nil {
		t.Fatal(err)
	}

	stats, err := f.store.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := statValue(t, stats, "Overdue"); got != "1" {
		t.Errorf(`"Overdue" = %q, want "1" (done-and-overdue must not count)`, got)
	}
}

func TestStatsReportsNewestBulletAndNeverForAnEmptyInstance(t *testing.T) {
	f := newFixture(t)
	stats, err := f.store.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := statValue(t, stats, "Newest bullet"); got != "never" {
		t.Errorf(`"Newest bullet" on an empty instance = %q, want "never"`, got)
	}
}

func TestNotesImplementsTheStaterInterface(t *testing.T) {
	var _ = notes.New() // compiles only if App satisfies the assertion added in app.go
}

// statValue finds one labelled stat's value, failing the test if it is
// missing — every test above asserts on a stat that must exist, so a
// missing label is itself a failure worth a clear message rather than a
// nil-slice panic.
func statValue(t *testing.T, stats []app.Stat, label string) string {
	t.Helper()
	for _, s := range stats {
		if s.Label == label {
			return s.Value
		}
	}
	t.Fatalf("no stat labelled %q in %+v", label, stats)
	return ""
}
