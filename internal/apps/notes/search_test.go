package notes_test

import (
	"context"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

func TestSearchFindsAMatchInTheTitle(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "buy milk")
	f.mk(t, notes.RootID, "call dentist")

	got, err := f.store.Search(ctx, f.alice.ID, "milk", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "buy milk" {
		t.Fatalf("Search(milk) = %+v, want just the milk bullet", got)
	}
}

func TestSearchFindsAMatchInTheNote(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "groceries")
	if err := f.store.SetText(ctx, f.alice.ID, n.ID, "groceries", "don't forget the oat milk"); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Search(ctx, f.alice.ID, "oat", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != n.ID {
		t.Fatalf("Search(oat) = %+v, want the groceries bullet", got)
	}
}

// TestSearchRequiresEveryWord: space-separated words are ANDed, not ORed —
// ftsQuery's job is to make that true regardless of what the words are.
func TestSearchRequiresEveryWord(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "budget report")
	f.mk(t, notes.RootID, "budget only")

	got, err := f.store.Search(ctx, f.alice.ID, "budget report", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "budget report" {
		t.Fatalf("Search(budget report) = %+v, want just the one with both words", got)
	}
}

// TestSearchTracksEdits proves the FTS5 triggers actually fire, not just
// that the initial backfill worked: a stale index would still find the old
// text, or miss the new.
func TestSearchTracksEdits(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "old title")

	if got, err := f.store.Search(ctx, f.alice.ID, "old", false); err != nil || len(got) != 1 {
		t.Fatalf("Search(old) before edit = %+v, %v", got, err)
	}

	if err := f.store.SetText(ctx, f.alice.ID, n.ID, "new title", ""); err != nil {
		t.Fatal(err)
	}

	if got, err := f.store.Search(ctx, f.alice.ID, "old", false); err != nil || len(got) != 0 {
		t.Fatalf("Search(old) after edit = %+v, %v; the index is stale", got, err)
	}
	if got, err := f.store.Search(ctx, f.alice.ID, "new", false); err != nil || len(got) != 1 {
		t.Fatalf("Search(new) after edit = %+v, %v", got, err)
	}
}

// TestSearchTracksDeletes proves the AD trigger removes a deleted bullet
// from the index itself, not merely that Search happens to filter it out
// some other way.
func TestSearchTracksDeletes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "temporary")

	if err := f.store.Delete(ctx, f.alice.ID, n.ID); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Search(ctx, f.alice.ID, "temporary", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Search after delete = %+v, want none", got)
	}
}

// TestSearchTracksCascadedDeletes is issue #91: Ops.Delete deletes only the
// target row and relies on parent_id ... ON DELETE CASCADE to remove
// descendants, so this pins that the notes_fts_ad AFTER DELETE trigger
// (migrations/0003_search.sql) also fires for each FK-cascaded child row,
// not just a directly deleted one — a driver upgrade could otherwise change
// this silently, with nothing else in the suite to catch it.
func TestSearchTracksCascadedDeletes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent-marker")
	f.mk(t, parent.ID, "child-marker")

	if err := f.store.Delete(ctx, f.alice.ID, parent.ID); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Search(ctx, f.alice.ID, "child-marker", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Search for the cascade-deleted child = %+v, want none", got)
	}
}

func TestSearchDoesNotLeakAnotherUsersNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "alice only")
	f.mkFor(t, f.bob.ID, notes.RootID, "bob's secret")

	got, err := f.store.Search(ctx, f.alice.ID, "secret", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("alice's search found bob's node: %+v", got)
	}
}

func TestSearchExcludesDoneUnlessShowCompleted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "finished task")
	if err := f.store.SetDone(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}

	if got, err := f.store.Search(ctx, f.alice.ID, "finished", false); err != nil || len(got) != 0 {
		t.Fatalf("Search with showCompleted=false = %+v, %v; want none", got, err)
	}
	if got, err := f.store.Search(ctx, f.alice.ID, "finished", true); err != nil || len(got) != 1 {
		t.Fatalf("Search with showCompleted=true = %+v, %v; want the one hit", got, err)
	}
}

func TestSearchWithNoQueryReturnsNoResults(t *testing.T) {
	f := newFixture(t)
	f.mk(t, notes.RootID, "anything")

	got, err := f.store.Search(context.Background(), f.alice.ID, "   ", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Search(whitespace) = %+v, want none", got)
	}
}

// TestSearchHandlesFTS5SyntaxCharactersLiterally proves ftsQuery's escaping:
// none of these characters carry their usual FTS5 meaning here, and none of
// them should make Search return an error.
func TestSearchHandlesFTS5SyntaxCharactersLiterally(t *testing.T) {
	f := newFixture(t)
	f.mk(t, notes.RootID, `a "quoted" word`)

	for _, q := range []string{`"quoted"`, `budget* OR report`, `foo" bar`, `NOT`} {
		if _, err := f.store.Search(context.Background(), f.alice.ID, q, false); err != nil {
			t.Errorf("Search(%q) errored: %v", q, err)
		}
	}
}

// TestSearchExcludesArchivedNodes is spec §13's other half of §12: unlike
// done, there is no toggle that can bring an archived hit back.
func TestSearchExcludesArchivedNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "put away")
	if err := f.store.SetArchived(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Search(ctx, f.alice.ID, "away", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Search = %+v, want none — it is archived", got)
	}
}

// TestSearchExcludesADescendantOfAnArchivedNode: the match itself is not
// archived, but its ancestor is — spec §13's "subtree" applies here too.
func TestSearchExcludesADescendantOfAnArchivedNode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "put away")
	child := f.mk(t, parent.ID, "buried treasure")
	if err := f.store.SetArchived(ctx, f.alice.ID, parent.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Search(ctx, f.alice.ID, "treasure", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Search = %+v, want none — %d sits under an archived node", got, child.ID)
	}
}
