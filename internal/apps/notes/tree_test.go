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
