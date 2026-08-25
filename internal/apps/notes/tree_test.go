package notes_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

// mk creates one of alice's bullets, appended last, so that tree-shape tests
// read as tree shapes rather than as error handling. The huge afterPos leans
// on Create's clamping, which therefore gets exercised by every test here.
func (f *fixture) mk(t *testing.T, parentID int64, title string) notes.Node {
	t.Helper()
	n, err := f.store.Create(context.Background(), f.alice.ID, parentID, 1<<30, title, "")
	if err != nil {
		t.Fatalf("Create(%q under %d): %v", title, parentID, err)
	}
	return n
}

func TestCreateAppendsInOrder(t *testing.T) {
	f := newFixture(t)
	f.mk(t, notes.RootID, "first")
	f.mk(t, notes.RootID, "second")
	f.mk(t, notes.RootID, "third")

	got := f.childTitles(t, f.alice.ID, notes.RootID)
	want := []string{"first", "second", "third"}
	if !slices.Equal(got, want) {
		t.Fatalf("top level = %v; want %v", got, want)
	}
	checkInvariants(t, f.db)
}

func TestCreateInsertsBetweenSiblings(t *testing.T) {
	f := newFixture(t)
	f.mk(t, notes.RootID, "first")
	f.mk(t, notes.RootID, "third")

	// afterPos 0 means "after the bullet at position 0".
	if _, err := f.store.Create(context.Background(), f.alice.ID, notes.RootID, 0, "second", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got := f.childTitles(t, f.alice.ID, notes.RootID)
	want := []string{"first", "second", "third"}
	if !slices.Equal(got, want) {
		t.Fatalf("top level = %v; want %v", got, want)
	}
	checkInvariants(t, f.db)
}

func TestCreateInsertsFirstWithAfterPosMinusOne(t *testing.T) {
	f := newFixture(t)
	f.mk(t, notes.RootID, "second")

	if _, err := f.store.Create(context.Background(), f.alice.ID, notes.RootID, -1, "first", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got := f.childTitles(t, f.alice.ID, notes.RootID)
	want := []string{"first", "second"}
	if !slices.Equal(got, want) {
		t.Fatalf("top level = %v; want %v", got, want)
	}
	checkInvariants(t, f.db)
}

func TestCreateUnderAParent(t *testing.T) {
	f := newFixture(t)
	parent := f.mk(t, notes.RootID, "parent")
	child := f.mk(t, parent.ID, "child")

	if child.ParentID != parent.ID {
		t.Errorf("child.ParentID = %d; want %d", child.ParentID, parent.ID)
	}
	if child.Position != 0 {
		t.Errorf("child.Position = %d; want 0", child.Position)
	}
	if got := f.childTitles(t, f.alice.ID, parent.ID); !slices.Equal(got, []string{"child"}) {
		t.Errorf("children of parent = %v; want [child]", got)
	}
	checkInvariants(t, f.db)
}

func TestByIDReturnsTheStoredNode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent")
	child := f.mk(t, parent.ID, "child")

	got, err := f.store.ByID(ctx, f.alice.ID, child.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Title != "child" || got.ParentID != parent.ID || got.Position != 0 {
		t.Errorf("ByID = {title %q, parent %d, position %d}; want {\"child\", %d, 0}",
			got.Title, got.ParentID, got.Position, parent.ID)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Error("timestamps are zero; scanNode did not parse them")
	}

	// A top-level node's NULL parent_id must scan back as RootID rather than
	// as some stray id — the whole reason RootID is 0.
	top, err := f.store.ByID(ctx, f.alice.ID, parent.ID)
	if err != nil {
		t.Fatalf("ByID(parent): %v", err)
	}
	if top.ParentID != notes.RootID {
		t.Errorf("top-level ParentID = %d; want RootID (%d)", top.ParentID, notes.RootID)
	}
}

func TestByIDRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "alice's")

	if _, err := f.store.ByID(context.Background(), f.bob.ID, n.ID); !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("bob fetching alice's node = %v; want ErrNotFound", err)
	}
}

func TestCreateRejectsAnotherUsersParent(t *testing.T) {
	f := newFixture(t)
	alices := f.mk(t, notes.RootID, "alice's bullet")

	_, err := f.store.Create(context.Background(), f.bob.ID, alices.ID, 0, "bob's child", "")
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("Create under another user's node = %v; want ErrNotFound", err)
	}
	checkInvariants(t, f.db)
}

func TestCreateRejectsTextThatFailsValidation(t *testing.T) {
	f := newFixture(t)
	long := make([]rune, notes.MaxTitleRunes+1)
	for i := range long {
		long[i] = 'a'
	}
	_, err := f.store.Create(context.Background(), f.alice.ID, notes.RootID, 0, string(long), "")
	if !errors.Is(err, notes.ErrInvalid) {
		t.Fatalf("Create with an oversized title = %v; want ErrInvalid", err)
	}
}

func TestCreateRefusesToPassMaxDepth(t *testing.T) {
	f := newFixture(t)

	// Build a chain down to exactly MaxDepth: MaxDepth+1 bullets, the last of
	// which sits at the deepest permitted depth.
	parent := int64(notes.RootID)
	for i := 0; i <= notes.MaxDepth; i++ {
		parent = f.mk(t, parent, fmt.Sprintf("level %d", i)).ID
	}

	_, err := f.store.Create(context.Background(), f.alice.ID, parent, 0, "one too deep", "")
	if !errors.Is(err, notes.ErrTooDeep) {
		t.Fatalf("Create below MaxDepth = %v; want ErrTooDeep", err)
	}
	checkInvariants(t, f.db)
}

func TestSetCollapsedRoundTrips(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "parent")

	if err := f.store.SetCollapsed(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatalf("SetCollapsed(true): %v", err)
	}
	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if !got.Collapsed {
		t.Fatal("Collapsed = false after SetCollapsed(true)")
	}

	if err := f.store.SetCollapsed(ctx, f.alice.ID, n.ID, false); err != nil {
		t.Fatalf("SetCollapsed(false): %v", err)
	}
	if got, _ = f.store.ByID(ctx, f.alice.ID, n.ID); got.Collapsed {
		t.Fatal("Collapsed = true after SetCollapsed(false)")
	}
}

func TestSetCollapsedRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "alice's")

	err := f.store.SetCollapsed(context.Background(), f.bob.ID, n.ID, true)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("SetCollapsed on another user's node = %v; want ErrNotFound", err)
	}
}

func TestSetTextReplacesBothFields(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "before")

	if err := f.store.SetText(ctx, f.alice.ID, n.ID, "after", "a note"); err != nil {
		t.Fatalf("SetText: %v", err)
	}

	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Title != "after" || got.Note != "a note" {
		t.Fatalf("after SetText: title = %q, note = %q; want \"after\", \"a note\"", got.Title, got.Note)
	}
}

func TestSetTextRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "alice's")

	err := f.store.SetText(context.Background(), f.bob.ID, n.ID, "bob was here", "")
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("SetText on another user's node = %v; want ErrNotFound", err)
	}
	got, _ := f.store.ByID(context.Background(), f.alice.ID, n.ID)
	if got.Title != "alice's" {
		t.Fatalf("title = %q; bob's write went through", got.Title)
	}
}

func TestSetTextRejectsInvalidText(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "fine")

	err := f.store.SetText(context.Background(), f.alice.ID, n.ID, "\xff\xfe", "")
	if !errors.Is(err, notes.ErrInvalid) {
		t.Fatalf("SetText with invalid UTF-8 = %v; want ErrInvalid", err)
	}
}

// countRows is the total number of nodes in the table, for cascade tests.
func countRows(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM notes_nodes`).Scan(&n); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	return n
}

func TestDeleteClosesTheGap(t *testing.T) {
	f := newFixture(t)
	f.mk(t, notes.RootID, "a")
	b := f.mk(t, notes.RootID, "b")
	f.mk(t, notes.RootID, "c")

	if err := f.store.Delete(context.Background(), f.alice.ID, b.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got := f.childTitles(t, f.alice.ID, notes.RootID)
	if want := []string{"a", "c"}; !slices.Equal(got, want) {
		t.Fatalf("top level = %v; want %v", got, want)
	}
	checkInvariants(t, f.db)
}

func TestDeleteTakesTheSubtreeWithIt(t *testing.T) {
	f := newFixture(t)
	a, _, _, _, _ := f.sample(t) // a has three descendants; b is separate

	if err := f.store.Delete(context.Background(), f.alice.ID, a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if n := countRows(t, f); n != 1 {
		t.Fatalf("%d nodes remain; want 1 (only b) — the cascade did not fire", n)
	}
	checkInvariants(t, f.db)
}

func TestDeleteRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "alice's")

	err := f.store.Delete(context.Background(), f.bob.ID, n.ID)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("Delete of another user's node = %v; want ErrNotFound", err)
	}
	if countRows(t, f) != 1 {
		t.Fatal("bob's delete went through")
	}
}

func TestDeleteOfAMissingNode(t *testing.T) {
	f := newFixture(t)
	err := f.store.Delete(context.Background(), f.alice.ID, 4242)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("Delete of a missing node = %v; want ErrNotFound", err)
	}
}

func TestMoveToAnotherParent(t *testing.T) {
	f := newFixture(t)
	a := f.mk(t, notes.RootID, "a")
	b := f.mk(t, notes.RootID, "b")
	x := f.mk(t, a.ID, "x")

	if err := f.store.Move(context.Background(), f.alice.ID, x.ID, b.ID, 0); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if got := f.childTitles(t, f.alice.ID, a.ID); len(got) != 0 {
		t.Errorf("a still has %v", got)
	}
	if got := f.childTitles(t, f.alice.ID, b.ID); !slices.Equal(got, []string{"x"}) {
		t.Errorf("b has %v; want [x]", got)
	}
	checkInvariants(t, f.db)
}

func TestMoveWithinAParent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "a")
	b := f.mk(t, notes.RootID, "b")
	c := f.mk(t, notes.RootID, "c")

	// b down one.
	if err := f.store.Move(ctx, f.alice.ID, b.ID, notes.RootID, 2); err != nil {
		t.Fatalf("Move down: %v", err)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"a", "c", "b"}) {
		t.Fatalf("after moving b down: %v; want [a c b]", got)
	}
	checkInvariants(t, f.db)

	// c up one, from its new position 1 to 0.
	if err := f.store.Move(ctx, f.alice.ID, c.ID, notes.RootID, 0); err != nil {
		t.Fatalf("Move up: %v", err)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"c", "a", "b"}) {
		t.Fatalf("after moving c up: %v; want [c a b]", got)
	}
	checkInvariants(t, f.db)
}

func TestMoveClampsAnOutOfRangePosition(t *testing.T) {
	f := newFixture(t)
	a := f.mk(t, notes.RootID, "a")
	f.mk(t, notes.RootID, "b")

	if err := f.store.Move(context.Background(), f.alice.ID, a.ID, notes.RootID, 99); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"b", "a"}) {
		t.Fatalf("top level = %v; want [b a]", got)
	}
	checkInvariants(t, f.db)
}

func TestMoveRefusesToNestANodeInsideItself(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "a")
	child := f.mk(t, a.ID, "child")
	grandchild := f.mk(t, child.ID, "grandchild")

	for _, tc := range []struct {
		name   string
		target int64
	}{
		{"into itself", a.ID},
		{"into its child", child.ID},
		{"into its grandchild", grandchild.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := f.store.Move(ctx, f.alice.ID, a.ID, tc.target, 0)
			if !errors.Is(err, notes.ErrCycle) {
				t.Fatalf("Move %s = %v; want ErrCycle", tc.name, err)
			}
			checkInvariants(t, f.db)
		})
	}
}

func TestMoveRejectsAnotherUsersNodeOrParent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "alice's a")
	b := f.mk(t, notes.RootID, "alice's b")

	if err := f.store.Move(ctx, f.bob.ID, a.ID, notes.RootID, 0); !errors.Is(err, notes.ErrNotFound) {
		t.Errorf("bob moving alice's node = %v; want ErrNotFound", err)
	}

	// bob owns nothing, so a move of his own (nonexistent) node into alice's
	// parent must also fail.
	if err := f.store.Move(ctx, f.bob.ID, 999, b.ID, 0); !errors.Is(err, notes.ErrNotFound) {
		t.Errorf("bob moving into alice's node = %v; want ErrNotFound", err)
	}
	checkInvariants(t, f.db)
}

func TestMoveCarriesTheSubtree(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "a")
	x := f.mk(t, a.ID, "x")
	f.mk(t, x.ID, "x1")
	f.mk(t, x.ID, "x2")
	b := f.mk(t, notes.RootID, "b")

	if err := f.store.Move(ctx, f.alice.ID, x.ID, b.ID, 0); err != nil {
		t.Fatalf("Move: %v", err)
	}

	got, err := f.store.Outline(ctx, f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	// "taking its subtree with it" is the method's headline promise, and it
	// is the only path on which heightOf returns anything but zero.
	want := "- a\n- b [+]\n  - x [+]\n    - x1\n    - x2\n"
	if outlineShape(got) != want {
		t.Fatalf("after moving a subtree:\n%s\nwant\n%s", outlineShape(got), want)
	}
	checkInvariants(t, f.db)
}

func TestMoveDoesNotRenumberAnotherUsersTopLevel(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// A top-level bullet has parent_id NULL for every user alike, so the
	// user_id filter on the two renumbering statements is the only thing
	// scoping alice's move to her own outline. Asserting on bob's titles
	// names such a leak at its source: it would also trip checkInvariants,
	// but as an I1 failure on rows this test never moved, which says
	// nothing about where the bug is.
	for _, title := range []string{"bob 0", "bob 1", "bob 2"} {
		if _, err := f.store.Create(ctx, f.bob.ID, notes.RootID, 1<<30, title, ""); err != nil {
			t.Fatalf("Create for bob: %v", err)
		}
	}
	a := f.mk(t, notes.RootID, "alice 0")
	f.mk(t, notes.RootID, "alice 1")
	f.mk(t, notes.RootID, "alice 2")

	if err := f.store.Move(ctx, f.alice.ID, a.ID, notes.RootID, 2); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"alice 1", "alice 2", "alice 0"}) {
		t.Errorf("alice's top level = %v; want [alice 1 alice 2 alice 0]", got)
	}
	if got := f.childTitles(t, f.bob.ID, notes.RootID); !slices.Equal(got, []string{"bob 0", "bob 1", "bob 2"}) {
		t.Errorf("bob's top level = %v; want [bob 0 bob 1 bob 2] untouched", got)
	}
	checkInvariants(t, f.db)
}

func TestMoveAllowsLandingExactlyAtMaxDepth(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// A chain whose deepest bullet sits at MaxDepth-1, so a leaf moved under
	// it lands at exactly MaxDepth — the deepest permitted depth. This pins
	// the permitting side of the guard: with >= in place of >, it would fail.
	parent := int64(notes.RootID)
	for i := 0; i < notes.MaxDepth; i++ {
		parent = f.mk(t, parent, fmt.Sprintf("level %d", i)).ID
	}
	leaf := f.mk(t, notes.RootID, "leaf")

	if err := f.store.Move(ctx, f.alice.ID, leaf.ID, parent, 0); err != nil {
		t.Fatalf("Move landing at exactly MaxDepth = %v; want nil", err)
	}
	checkInvariants(t, f.db)
}

func TestMoveRefusesToPassMaxDepth(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// A chain whose deepest bullet sits at exactly MaxDepth.
	deepest := int64(notes.RootID)
	for i := 0; i <= notes.MaxDepth; i++ {
		deepest = f.mk(t, deepest, fmt.Sprintf("level %d", i)).ID
	}
	// A separate two-level subtree, so moving it needs two more levels.
	top := f.mk(t, notes.RootID, "top")
	f.mk(t, top.ID, "under top")

	if err := f.store.Move(ctx, f.alice.ID, top.ID, deepest, 0); !errors.Is(err, notes.ErrTooDeep) {
		t.Fatalf("Move past MaxDepth = %v; want ErrTooDeep", err)
	}
	checkInvariants(t, f.db)
}
