package notes_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

// mk creates one of alice's bullets, appended last, so that tree-shape tests
// read as tree shapes rather than as error handling. The huge afterPos leans
// on Create's clamping, which therefore gets exercised by every test here.
func (f *fixture) mk(t *testing.T, parentID int64, title string) notes.Node {
	t.Helper()
	return f.mkFor(t, f.alice.ID, parentID, title)
}

// mkFor is mk for any user. Most tests only need one tree and call mk; the
// owner-scoping tests need bob to own bullets of his own.
func (f *fixture) mkFor(t *testing.T, userID, parentID int64, title string) notes.Node {
	t.Helper()
	n, err := f.store.Create(context.Background(), userID, parentID, 1<<30, title, "")
	if err != nil {
		t.Fatalf("Create(%q under %d for user %d): %v", title, parentID, userID, err)
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

// untrimmedTitle has whitespace at both ends; trimmedTitle is what the store
// is documented to keep for it. Trailing spaces and tabs go, leading ones
// stay: an outline is written in prose, where a leading space is sometimes
// deliberate and a trailing one never is.
const (
	untrimmedTitle = "  a deliberately indented bullet \t "
	trimmedTitle   = "  a deliberately indented bullet"
)

func TestCreateTrimsTrailingWhitespaceAndKeepsLeading(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	n, err := f.store.Create(ctx, f.alice.ID, notes.RootID, 0, untrimmedTitle, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// The returned node and the stored row must agree: Create builds its
	// result from the trimmed title rather than re-reading it.
	if n.Title != trimmedTitle {
		t.Errorf("Create returned title %q; want %q", n.Title, trimmedTitle)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{trimmedTitle}) {
		t.Errorf("stored title = %q; want %q", got, []string{trimmedTitle})
	}
	checkInvariants(t, f.db)
}

func TestSetTextTrimsTrailingWhitespaceAndKeepsLeading(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "before")

	if err := f.store.SetText(ctx, f.alice.ID, n.ID, untrimmedTitle, ""); err != nil {
		t.Fatalf("SetText: %v", err)
	}
	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Title != trimmedTitle {
		t.Fatalf("title after SetText = %q; want %q", got.Title, trimmedTitle)
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

func TestSetDoneRoundTrips(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "task")

	if err := f.store.SetDone(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatalf("SetDone(true): %v", err)
	}
	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if !got.Done {
		t.Fatal("Done = false after SetDone(true)")
	}

	if err := f.store.SetDone(ctx, f.alice.ID, n.ID, false); err != nil {
		t.Fatalf("SetDone(false): %v", err)
	}
	if got, _ = f.store.ByID(ctx, f.alice.ID, n.ID); got.Done {
		t.Fatal("Done = true after SetDone(false)")
	}
}

// TestSetDoneDoesNotTouchChildren is spec §11: completing a parent does not
// complete its children — that is a display decision (hideDone, Task 3),
// never a change to the child's own row.
func TestSetDoneDoesNotTouchChildren(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent")
	child := f.mk(t, parent.ID, "child")

	if err := f.store.SetDone(ctx, f.alice.ID, parent.ID, true); err != nil {
		t.Fatalf("SetDone: %v", err)
	}
	got, err := f.store.ByID(ctx, f.alice.ID, child.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Done {
		t.Fatal("the child was marked done by its parent's SetDone")
	}
}

func TestSetDoneRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "alice's")

	err := f.store.SetDone(context.Background(), f.bob.ID, n.ID, true)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("SetDone on another user's node = %v; want ErrNotFound", err)
	}
}

func TestSetDuePersistsAndClears(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "task")

	if err := f.store.SetDue(ctx, f.alice.ID, n.ID, "2026-03-05"); err != nil {
		t.Fatalf("SetDue: %v", err)
	}
	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.DueOn != "2026-03-05" {
		t.Fatalf("DueOn = %q, want 2026-03-05", got.DueOn)
	}

	if err := f.store.SetDue(ctx, f.alice.ID, n.ID, ""); err != nil {
		t.Fatalf("SetDue(clear): %v", err)
	}
	if got, _ = f.store.ByID(ctx, f.alice.ID, n.ID); got.DueOn != "" {
		t.Fatalf("DueOn = %q after clearing, want empty", got.DueOn)
	}
}

func TestSetDueRejectsBadFormat(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "task")

	err := f.store.SetDue(context.Background(), f.alice.ID, n.ID, "not-a-date")
	if !errors.Is(err, notes.ErrInvalid) {
		t.Fatalf("SetDue(bad format) = %v; want ErrInvalid", err)
	}
}

func TestSetDueRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "alice's")

	err := f.store.SetDue(context.Background(), f.bob.ID, n.ID, "2026-03-05")
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("SetDue on another user's node = %v; want ErrNotFound", err)
	}
}

func TestSetArchivedRoundTrips(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "task")

	if err := f.store.SetArchived(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatalf("SetArchived(true): %v", err)
	}
	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if !got.Archived {
		t.Fatal("Archived = false after SetArchived(true)")
	}

	if err := f.store.SetArchived(ctx, f.alice.ID, n.ID, false); err != nil {
		t.Fatalf("SetArchived(false): %v", err)
	}
	if got, _ = f.store.ByID(ctx, f.alice.ID, n.ID); got.Archived {
		t.Fatal("Archived = true after SetArchived(false)")
	}
}

// TestSetArchivedDoesNotTouchChildren mirrors TestSetDoneDoesNotTouchChildren:
// archiving a parent does not write archived_at onto its children — hiding
// the subtree is a display decision made in the queries that build the
// outline, search results and due list (Task 2), never a change to the
// child's own row.
func TestSetArchivedDoesNotTouchChildren(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent")
	child := f.mk(t, parent.ID, "child")

	if err := f.store.SetArchived(ctx, f.alice.ID, parent.ID, true); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}
	got, err := f.store.ByID(ctx, f.alice.ID, child.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Archived {
		t.Fatal("the child was marked archived by its parent's SetArchived")
	}
}

// TestSetArchivedIsIndependentOfDone is spec §13: a node can be either,
// both, or neither.
func TestSetArchivedIsIndependentOfDone(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "task")

	if err := f.store.SetDone(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetArchived(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.ByID(ctx, f.alice.ID, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Done || !got.Archived {
		t.Fatalf("got Done=%v Archived=%v, want both true", got.Done, got.Archived)
	}

	if err := f.store.SetArchived(ctx, f.alice.ID, n.ID, false); err != nil {
		t.Fatal(err)
	}
	if got, _ = f.store.ByID(ctx, f.alice.ID, n.ID); !got.Done || got.Archived {
		t.Fatalf("after un-archiving: Done=%v Archived=%v, want Done true, Archived false", got.Done, got.Archived)
	}
}

func TestSetArchivedRejectsAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	n := f.mk(t, notes.RootID, "alice's")

	err := f.store.SetArchived(context.Background(), f.bob.ID, n.ID, true)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("SetArchived on another user's node = %v; want ErrNotFound", err)
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

// TestTimestampsRecordWhenABulletChanged pins the two halves of the clock's
// contract: a mutation advances updated_at to the moment it happened, and
// nothing moves created_at afterwards. Without the second half, "modified"
// and "created" are the same column with two names.
func TestTimestampsRecordWhenABulletChanged(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	created := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	edited := created.Add(90 * time.Minute)
	moved := created.Add(3 * time.Hour)

	f.store.SetClock(func() time.Time { return created })
	a := f.mk(t, notes.RootID, "a")
	b := f.mk(t, notes.RootID, "b")

	// stamps asserts both columns of one bullet. time.Time is compared with
	// Equal rather than ==, since a round trip through RFC 3339 text is not
	// obliged to return the same monotonic reading.
	stamps := func(what string, id int64, wantCreated, wantUpdated time.Time) {
		t.Helper()
		n, err := f.store.ByID(ctx, f.alice.ID, id)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if !n.CreatedAt.Equal(wantCreated) {
			t.Errorf("%s: created_at = %s; want %s", what, n.CreatedAt, wantCreated)
		}
		if !n.UpdatedAt.Equal(wantUpdated) {
			t.Errorf("%s: updated_at = %s; want %s", what, n.UpdatedAt, wantUpdated)
		}
	}

	// Create stamps both columns with the same instant.
	stamps("after Create", b.ID, created, created)

	f.store.SetClock(func() time.Time { return edited })
	if err := f.store.SetText(ctx, f.alice.ID, b.ID, "b edited", ""); err != nil {
		t.Fatalf("SetText: %v", err)
	}
	stamps("after SetText", b.ID, created, edited)

	// A structural operation changes the bullet just as much as its text does:
	// where it sits in the outline is part of what the user wrote.
	f.store.SetClock(func() time.Time { return moved })
	if err := f.store.Move(ctx, f.alice.ID, b.ID, a.ID, 0); err != nil {
		t.Fatalf("Move: %v", err)
	}
	stamps("after Move", b.ID, created, moved)

	// a was never itself the subject of an operation, so neither of its stamps
	// moved — not even when b was moved underneath it.
	stamps("the untouched bullet", a.ID, created, created)
	checkInvariants(t, f.db)
}

// TestCreateDoesNotRenumberAnotherUsersTopLevel is the Create half of the
// hazard TestMoveDoesNotRenumberAnotherUsersTopLevel names: a top-level bullet
// has parent_id NULL for every user alike, so the user_id filter on Create's
// sibling shift is the only thing keeping it inside alice's outline.
//
// The assertion is on bob's rows directly, and on their positions as well as
// their titles: a shift that leaked onto him would move a contiguous suffix,
// which leaves his titles in exactly the order they were already in.
func TestCreateDoesNotRenumberAnotherUsersTopLevel(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for _, title := range []string{"bob 0", "bob 1", "bob 2"} {
		f.mkFor(t, f.bob.ID, notes.RootID, title)
	}
	f.mk(t, notes.RootID, "alice 0")
	f.mk(t, notes.RootID, "alice 1")

	// afterPos -1 inserts first, so the shift covers every top-level row from
	// position 0 up — the widest it can be.
	if _, err := f.store.Create(ctx, f.alice.ID, notes.RootID, -1, "alice new", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	want := []string{"alice new@0", "alice 0@1", "alice 1@2"}
	if got := f.childTitlesAndPositions(t, f.alice.ID, notes.RootID); !slices.Equal(got, want) {
		t.Errorf("alice's top level = %v; want %v", got, want)
	}
	want = []string{"bob 0@0", "bob 1@1", "bob 2@2"}
	if got := f.childTitlesAndPositions(t, f.bob.ID, notes.RootID); !slices.Equal(got, want) {
		t.Errorf("bob's top level = %v; want %v untouched", got, want)
	}
	checkInvariants(t, f.db)
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

	// A bullet to be left alone. Without one, the table is empty and the error
	// is the only thing this test can observe — which says nothing about
	// whether the failed delete kept its hands off the tree.
	f.mk(t, notes.RootID, "still here")

	err := f.store.Delete(context.Background(), f.alice.ID, 4242)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("Delete of a missing node = %v; want ErrNotFound", err)
	}
	if got := f.childTitlesAndPositions(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"still here@0"}) {
		t.Fatalf("top level = %v; want [still here@0] unchanged", got)
	}
	if n := countRows(t, f); n != 1 {
		t.Fatalf("%d nodes in the table; want 1 — the failed delete changed something", n)
	}
	checkInvariants(t, f.db)
}

// TestDeleteDoesNotRenumberAnotherUsersTopLevel is the Delete half of the
// hazard TestMoveDoesNotRenumberAnotherUsersTopLevel names. Delete's gap close
// is scoped to alice only by its user_id filter, and a leak would pull bob's
// positions down without disturbing the order his titles come back in — so the
// assertion has to be on the positions.
func TestDeleteDoesNotRenumberAnotherUsersTopLevel(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for _, title := range []string{"bob 0", "bob 1", "bob 2"} {
		f.mkFor(t, f.bob.ID, notes.RootID, title)
	}
	a := f.mk(t, notes.RootID, "alice 0")
	f.mk(t, notes.RootID, "alice 1")
	f.mk(t, notes.RootID, "alice 2")

	// Deleting the first bullet makes the gap close cover every top-level row
	// after position 0 — the widest it can be.
	if err := f.store.Delete(ctx, f.alice.ID, a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	want := []string{"alice 1@0", "alice 2@1"}
	if got := f.childTitlesAndPositions(t, f.alice.ID, notes.RootID); !slices.Equal(got, want) {
		t.Errorf("alice's top level = %v; want %v", got, want)
	}
	want = []string{"bob 0@0", "bob 1@1", "bob 2@2"}
	if got := f.childTitlesAndPositions(t, f.bob.ID, notes.RootID); !slices.Equal(got, want) {
		t.Errorf("bob's top level = %v; want %v untouched", got, want)
	}
	checkInvariants(t, f.db)
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

	got, err := f.store.Outline(ctx, f.alice.ID, notes.RootID, false)
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
	// scoping alice's move to her own outline. Both statements shift a
	// contiguous suffix of positions, and a suffix shifted wholesale keeps
	// its relative order — so a leak onto bob would leave his titles
	// reading exactly as they already did, and only his positions would
	// give it away. The assertion is therefore on childTitlesAndPositions:
	// asserting on titles alone would let a leak here surface only as
	// checkInvariants tripping on a duplicated position among rows this
	// test never moved, which says nothing about where the bug is.
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

	want := []string{"alice 1@0", "alice 2@1", "alice 0@2"}
	if got := f.childTitlesAndPositions(t, f.alice.ID, notes.RootID); !slices.Equal(got, want) {
		t.Errorf("alice's top level = %v; want %v", got, want)
	}
	want = []string{"bob 0@0", "bob 1@1", "bob 2@2"}
	if got := f.childTitlesAndPositions(t, f.bob.ID, notes.RootID); !slices.Equal(got, want) {
		t.Errorf("bob's top level = %v; want %v untouched", got, want)
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

func TestIndentBecomesTheLastChildOfThePreviousSibling(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "a")
	f.mk(t, a.ID, "existing child")
	b := f.mk(t, notes.RootID, "b")

	if err := f.store.Indent(ctx, f.alice.ID, b.ID); err != nil {
		t.Fatalf("Indent: %v", err)
	}

	got, err := f.store.Outline(ctx, f.alice.ID, notes.RootID, false)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	want := "- a [+]\n  - existing child\n  - b\n"
	if outlineShape(got) != want {
		t.Fatalf("after Indent:\n%s\nwant\n%s", outlineShape(got), want)
	}
	checkInvariants(t, f.db)
}

func TestIndentTheFirstSiblingDoesNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "a")
	f.mk(t, notes.RootID, "b")

	// A keypress with nowhere to go is not an error.
	if err := f.store.Indent(ctx, f.alice.ID, a.ID); err != nil {
		t.Fatalf("Indent of the first sibling = %v; want nil", err)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("top level = %v; want [a b]", got)
	}
	checkInvariants(t, f.db)
}

func TestOutdentBecomesTheNextSiblingOfItsParent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "a")
	child := f.mk(t, a.ID, "child")
	f.mk(t, a.ID, "later sibling")
	f.mk(t, notes.RootID, "z")

	if err := f.store.Outdent(ctx, f.alice.ID, child.ID); err != nil {
		t.Fatalf("Outdent: %v", err)
	}

	got, err := f.store.Outline(ctx, f.alice.ID, notes.RootID, false)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	// child lands directly after a; its former following sibling stays put.
	want := "- a [+]\n  - later sibling\n- child\n- z\n"
	if outlineShape(got) != want {
		t.Fatalf("after Outdent:\n%s\nwant\n%s", outlineShape(got), want)
	}
	checkInvariants(t, f.db)
}

func TestOutdentATopLevelBulletDoesNothing(t *testing.T) {
	f := newFixture(t)
	a := f.mk(t, notes.RootID, "a")

	if err := f.store.Outdent(context.Background(), f.alice.ID, a.ID); err != nil {
		t.Fatalf("Outdent at the top level = %v; want nil", err)
	}
	checkInvariants(t, f.db)
}

func TestMoveUpAndMoveDown(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "a")
	f.mk(t, notes.RootID, "b")
	c := f.mk(t, notes.RootID, "c")

	if err := f.store.MoveDown(ctx, f.alice.ID, a.ID); err != nil {
		t.Fatalf("MoveDown: %v", err)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"b", "a", "c"}) {
		t.Fatalf("after MoveDown: %v; want [b a c]", got)
	}

	if err := f.store.MoveUp(ctx, f.alice.ID, c.ID); err != nil {
		t.Fatalf("MoveUp: %v", err)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"b", "c", "a"}) {
		t.Fatalf("after MoveUp: %v; want [b c a]", got)
	}
	checkInvariants(t, f.db)
}

func TestMoveUpAndDownAtTheEndsDoNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.mk(t, notes.RootID, "a")
	b := f.mk(t, notes.RootID, "b")

	if err := f.store.MoveUp(ctx, f.alice.ID, a.ID); err != nil {
		t.Fatalf("MoveUp of the first bullet = %v; want nil", err)
	}
	if err := f.store.MoveDown(ctx, f.alice.ID, b.ID); err != nil {
		t.Fatalf("MoveDown of the last bullet = %v; want nil", err)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("top level = %v; want [a b] unchanged", got)
	}
	checkInvariants(t, f.db)
}

func TestKeyboardMovesRejectAnotherUsersNode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "a")
	b := f.mk(t, notes.RootID, "b")

	for name, op := range map[string]func(context.Context, int64, int64) error{
		"Indent":   f.store.Indent,
		"Outdent":  f.store.Outdent,
		"MoveUp":   f.store.MoveUp,
		"MoveDown": f.store.MoveDown,
	} {
		if err := op(ctx, f.bob.ID, b.ID); !errors.Is(err, notes.ErrNotFound) {
			t.Errorf("%s on another user's node = %v; want ErrNotFound", name, err)
		}
	}
	checkInvariants(t, f.db)
}

// txCtx bounds a transaction test in time.
//
// Everything inside a Do closure must reach the database through the Ops it is
// handed. The platform opens SQLite with one connection, so a call that goes
// to the pool instead waits for the connection the transaction is holding, and
// waits for ever. These three tests are what would catch a future edit putting
// such a call on an Ops path — and without a deadline they would catch it by
// hanging until the package-wide test timeout, which CI reports as a ten
// minute panic with no test name attached. Ten seconds is far more than these
// transactions need, and turns that into a named failure.
func txCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestDoSavesTextAndRestructuresAsOneWrite is the reason Ops exists. A
// structural request carries the focused bullet's text (spec §7), and the
// server applies both in one transaction so that a debounced autosave racing
// the structural operation cannot lose the last keystrokes.
func TestDoSavesTextAndRestructuresAsOneWrite(t *testing.T) {
	f := newFixture(t)
	ctx := txCtx(t)
	a := f.mk(t, notes.RootID, "a")
	b := f.mk(t, notes.RootID, "b")

	err := f.store.Do(ctx, func(o *notes.Ops) error {
		if err := o.SetText(ctx, f.alice.ID, b.ID, "b edited", "the note"); err != nil {
			return err
		}
		return o.Indent(ctx, f.alice.ID, b.ID)
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	// The structural half.
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("top level = %v; want [a]", got)
	}
	if got := f.childTitles(t, f.alice.ID, a.ID); !slices.Equal(got, []string{"b edited"}) {
		t.Fatalf("under a = %v; want [b edited]", got)
	}
	// The text half, including the note, which childTitles cannot see.
	n, err := f.store.ByID(ctx, f.alice.ID, b.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if n.Title != "b edited" || n.Note != "the note" {
		t.Fatalf("b = %q / %q; want \"b edited\" / \"the note\"", n.Title, n.Note)
	}
	checkInvariants(t, f.db)
}

// TestDoRollsBackEverythingWhenTheClosureFails is what makes the seam worth
// having: two operations that succeeded individually must both disappear when
// the transaction does not commit.
func TestDoRollsBackEverythingWhenTheClosureFails(t *testing.T) {
	f := newFixture(t)
	ctx := txCtx(t)
	a := f.mk(t, notes.RootID, "a")
	b := f.mk(t, notes.RootID, "b")

	boom := errors.New("the handler changed its mind")
	err := f.store.Do(ctx, func(o *notes.Ops) error {
		if err := o.SetText(ctx, f.alice.ID, b.ID, "b edited", "the note"); err != nil {
			return err
		}
		if err := o.Indent(ctx, f.alice.ID, b.ID); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Do = %v; want the closure's own error back", err)
	}

	// The structure is exactly as it was.
	if got := f.childTitles(t, f.alice.ID, notes.RootID); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("top level = %v; want [a b] unchanged", got)
	}
	if got := f.childTitles(t, f.alice.ID, a.ID); len(got) != 0 {
		t.Fatalf("a has children %v; want none — the indent was rolled back", got)
	}
	// And so is the text: a rollback that kept the edit would be the lost
	// update in reverse.
	n, err := f.store.ByID(ctx, f.alice.ID, b.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if n.Title != "b" || n.Note != "" {
		t.Fatalf("b = %q / %q; want %q / %q unchanged", n.Title, n.Note, "b", "")
	}
	checkInvariants(t, f.db)
}

// TestDoKeepsInvariantsAcrossAMultiOperationTransaction runs a whole sequence
// of structural operations inside one Do, including moving a bullet that was
// created in the same transaction and has therefore never been committed.
// I1-I4 are checked once at the end, which is the only point at which the
// database is observable from outside.
func TestDoKeepsInvariantsAcrossAMultiOperationTransaction(t *testing.T) {
	f := newFixture(t)
	ctx := txCtx(t)
	a := f.mk(t, notes.RootID, "a")
	f.mk(t, notes.RootID, "b")
	c := f.mk(t, notes.RootID, "c")

	err := f.store.Do(ctx, func(o *notes.Ops) error {
		d, err := o.Create(ctx, f.alice.ID, notes.RootID, 1<<30, "d", "")
		if err != nil {
			return err
		}
		// Ops.ByID reads through the transaction, so each of these sees the
		// uncommitted result of the one before it.
		if err := o.Indent(ctx, f.alice.ID, d.ID); err != nil { // d under c
			return err
		}
		if err := o.Indent(ctx, f.alice.ID, c.ID); err != nil { // c, with d, under b
			return err
		}
		if err := o.Outdent(ctx, f.alice.ID, d.ID); err != nil { // d beside c
			return err
		}
		return o.Delete(ctx, f.alice.ID, a.ID)
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	got, err := f.store.Outline(ctx, f.alice.ID, notes.RootID, false)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	want := "- b [+]\n  - c\n  - d\n"
	if outlineShape(got) != want {
		t.Fatalf("after the transaction:\n%s\nwant\n%s", outlineShape(got), want)
	}
	checkInvariants(t, f.db)
}

// TestRandomOperationSequencesPreserveInvariants applies a long, deterministic
// sequence of random operations across two users and checks I1-I4 after every
// single one.
//
// The tests above cover one operation on a known tree. Tree bugs are not like
// that: an indent that leaves a gap in position is invisible until the third
// move after it, when a clamp lands one place off and two bullets quietly swap.
// This test exists to catch the sequences nobody would think to write down.
//
// The seed is fixed so that a failure is reproducible, and the operation log is
// printed on failure so the sequence can be replayed by hand.
func TestRandomOperationSequencesPreserveInvariants(t *testing.T) {
	const steps = 500

	f := newFixture(t)
	ctx := context.Background()
	rng := rand.New(rand.NewPCG(0x0175, 0x5eed))

	owned := map[int64][]int64{f.alice.ID: nil, f.bob.ID: nil}
	users := []int64{f.alice.ID, f.bob.ID}

	var opLog []string
	fail := func(format string, args ...any) {
		t.Fatalf("%s\n\noperations so far:\n  %s",
			fmt.Sprintf(format, args...), strings.Join(opLog, "\n  "))
	}

	// succeeded counts the nil outcomes per method.
	//
	// It is what stops this test passing vacuously. A regression that made
	// every mutator return ErrNotFound would be waved through below — that is
	// an accepted outcome — and would then satisfy all four invariants
	// trivially, because nothing ever changed. Requiring each method to have
	// succeeded at least once is the difference between proving the
	// operations ran and proving they did not crash.
	succeeded := map[string]int{}

	// Only these five outcomes are acceptable. Anything else is a real
	// failure, not a rejected operation.
	record := func(method, op string, err error) {
		opLog = append(opLog, fmt.Sprintf("%s -> %v", op, err))
		switch {
		case err == nil:
			succeeded[method]++
		case errors.Is(err, notes.ErrNotFound),
			errors.Is(err, notes.ErrCycle),
			errors.Is(err, notes.ErrTooDeep),
			errors.Is(err, notes.ErrInvalid):
		default:
			fail("%s returned an unexpected error: %v", op, err)
		}
	}

	// operations is the one place a method's name is written down. The switch
	// this replaces spelled each name twice — once at its record call, once in
	// the list asserting it had succeeded — so a later chunk could add an
	// operation, forget the second copy, and get silent non-coverage of it.
	//
	// Each entry returns the log line for the call it made, so the description
	// and the call cannot drift apart either.
	type operation struct {
		method string
		run    func(userID int64, pick func() int64, step int) (string, error)
	}
	operations := []operation{
		{"Create", func(userID int64, pick func() int64, step int) (string, error) {
			parent, pos := pick(), rng.IntN(6)-1
			n, err := f.store.Create(ctx, userID, parent, pos, fmt.Sprintf("n%d", step), "")
			if err == nil {
				owned[userID] = append(owned[userID], n.ID)
			}
			return fmt.Sprintf("Create(user=%d, parent=%d, pos=%d) #%d", userID, parent, pos, n.ID), err
		}},
		{"Indent", func(userID int64, pick func() int64, _ int) (string, error) {
			id := pick()
			return fmt.Sprintf("Indent(user=%d, id=%d)", userID, id), f.store.Indent(ctx, userID, id)
		}},
		{"Outdent", func(userID int64, pick func() int64, _ int) (string, error) {
			id := pick()
			return fmt.Sprintf("Outdent(user=%d, id=%d)", userID, id), f.store.Outdent(ctx, userID, id)
		}},
		{"MoveUp", func(userID int64, pick func() int64, _ int) (string, error) {
			id := pick()
			return fmt.Sprintf("MoveUp(user=%d, id=%d)", userID, id), f.store.MoveUp(ctx, userID, id)
		}},
		{"MoveDown", func(userID int64, pick func() int64, _ int) (string, error) {
			id := pick()
			return fmt.Sprintf("MoveDown(user=%d, id=%d)", userID, id), f.store.MoveDown(ctx, userID, id)
		}},
		{"Move", func(userID int64, pick func() int64, _ int) (string, error) {
			id, parent, pos := pick(), pick(), rng.IntN(6)-1
			return fmt.Sprintf("Move(user=%d, id=%d, parent=%d, pos=%d)", userID, id, parent, pos),
				f.store.Move(ctx, userID, id, parent, pos)
		}},
		{"Delete", func(userID int64, pick func() int64, _ int) (string, error) {
			id := pick()
			return fmt.Sprintf("Delete(user=%d, id=%d)", userID, id), f.store.Delete(ctx, userID, id)
		}},
	}

	// weighted maps one roll onto an operation. Create appears three times so
	// the tree grows faster than it shrinks. It is nine values in the order the
	// switch's cases used to run in, so the seed still produces the same
	// sequence it always has.
	weighted := []int{0, 0, 0, 1, 2, 3, 4, 5, 6}

	for step := range steps {
		userID := users[rng.IntN(len(users))]
		mine := owned[userID]

		// A quarter of the time, act at the top level; otherwise pick one of
		// this user's bullets. Deleted ids stay in the slice on purpose, so
		// the ErrNotFound paths get exercised too.
		pick := func() int64 {
			if len(mine) == 0 || rng.IntN(4) == 0 {
				return notes.RootID
			}
			return mine[rng.IntN(len(mine))]
		}

		op := operations[weighted[rng.IntN(len(weighted))]]
		desc, err := op.run(userID, pick, step)
		record(op.method, desc, err)

		if v := treeViolations(loadRawNodes(t, f.db)); len(v) > 0 {
			fail("after step %d: %s", step, strings.Join(v, "; "))
		}

		// Give one of the store's own reads the same traffic. treeViolations
		// reads raw rows by design, so without this Outline, Children,
		// Ancestors and ByID only ever see the handful of deterministic trees
		// the tests above build.
		//
		// The owner assertion is the point of it. 500 random operations across
		// two users produce shapes no hand-written test reaches, and this is
		// the only thing in the package that would notice the owner filter on
		// Outline's descent leaking one user's bullets into the other's screen
		// under one of them.
		//
		// It consumes no randomness, so the sequence is exactly the one the
		// seed produced before this call was added.
		out, err := f.store.Outline(ctx, userID, notes.RootID, false)
		if err != nil {
			fail("after step %d: Outline(user=%d): %v", step, userID, err)
		}
		for _, n := range out {
			if n.UserID != userID {
				fail("after step %d: user %d's outline contains node %d (%q), owned by user %d",
					step, userID, n.ID, n.Title, n.UserID)
			}
		}
	}

	// Every method must actually have done something. Without this, the
	// degenerate run described above — every mutator failing, Create alone
	// working — would be indistinguishable from a healthy one.
	for _, op := range operations {
		if succeeded[op.method] == 0 {
			fail("%s never succeeded in %d steps; the sequence is not exercising it", op.method, steps)
		}
	}
	t.Logf("successful operations in %d steps: %v", steps, succeeded)

	// A weaker end-state check, kept because it is cheap. Create alone would
	// satisfy it, so it is a floor on the fixture rather than evidence that
	// the operations ran — that is what the loop above is for.
	if n := len(loadRawNodes(t, f.db)); n < 20 {
		fail("the sequence left only %d nodes", n)
	}
}
