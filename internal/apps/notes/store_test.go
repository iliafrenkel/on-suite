package notes_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// fixture is a migrated database with two users, so every owner-scoping test
// has somebody else to be confused with.
type fixture struct {
	store *notes.Store
	db    *sql.DB
	alice auth.User
	bob   auth.User
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()

	// A real file in a temp dir, not :memory: — the bugs that matter here
	// live in SQL, recursive CTEs and transaction behaviour.
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	platform, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	appSchema, err := db.Collect(notes.ID, notes.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(platform, appSchema...)); err != nil {
		t.Fatal(err)
	}

	users := auth.NewStore(handle)
	hash, err := auth.HashPassword("a-sufficiently-long-password")
	if err != nil {
		t.Fatal(err)
	}
	alice, err := users.CreateUser(ctx, "alice", hash, true)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := users.CreateUser(ctx, "bob", hash, false)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{store: notes.NewStore(handle), db: handle, alice: alice, bob: bob}
}

func TestAFreshDatabaseHasNoViolations(t *testing.T) {
	f := newFixture(t)
	checkInvariants(t, f.db)
}

func TestByIDOnAMissingNode(t *testing.T) {
	f := newFixture(t)
	if _, err := f.store.ByID(context.Background(), f.alice.ID, 12345); !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("ByID on a missing node = %v; want ErrNotFound", err)
	}
}

// childTitles reads a parent's children straight from the table, in position
// order. Structural assertions deliberately bypass the store, so a test of
// Create cannot be fooled by a matching bug in a read method.
func (f *fixture) childTitles(t *testing.T, userID, parentID int64) []string {
	t.Helper()

	var parent any
	if parentID != notes.RootID {
		parent = parentID
	}
	rows, err := f.db.QueryContext(context.Background(),
		`SELECT title FROM notes_nodes
		  WHERE user_id = ? AND parent_id IS ? ORDER BY position`, userID, parent)
	if err != nil {
		t.Fatalf("reading child titles: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatalf("scanning title: %v", err)
		}
		out = append(out, title)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading child titles: %v", err)
	}
	return out
}

// childTitlesAndPositions reads a parent's children straight from the table as
// "title@position", in position order.
//
// childTitles alone cannot see a renumbering that leaked onto another user:
// every statement in this package shifts a contiguous suffix of positions, and
// a suffix shifted wholesale keeps its relative order, so the titles come back
// looking untouched. The position is the part that moved.
func (f *fixture) childTitlesAndPositions(t *testing.T, userID, parentID int64) []string {
	t.Helper()

	var parent any
	if parentID != notes.RootID {
		parent = parentID
	}
	rows, err := f.db.QueryContext(context.Background(),
		`SELECT title, position FROM notes_nodes
		  WHERE user_id = ? AND parent_id IS ? ORDER BY position`, userID, parent)
	if err != nil {
		t.Fatalf("reading child titles: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var (
			title string
			pos   int
		)
		if err := rows.Scan(&title, &pos); err != nil {
			t.Fatalf("scanning title: %v", err)
		}
		out = append(out, fmt.Sprintf("%s@%d", title, pos))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading child titles: %v", err)
	}
	return out
}

// reparent rewrites one row's parent and position with raw SQL, bypassing the
// store completely. It is how the tests below manufacture a table that no
// sequence of store calls could ever produce: a broken invariant, which is
// exactly the situation the reads' owner filters and depth caps exist for.
func (f *fixture) reparent(t *testing.T, id, parentID int64, pos int) {
	t.Helper()

	var parent any
	if parentID != notes.RootID {
		parent = parentID
	}
	res, err := f.db.ExecContext(context.Background(),
		`UPDATE notes_nodes SET parent_id = ?, position = ? WHERE id = ?`, parent, pos, id)
	if err != nil {
		t.Fatalf("reparenting node %d: %v", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("reparenting node %d: %v", id, err)
	}
	if n != 1 {
		t.Fatalf("reparenting node %d changed %d rows; want 1", id, n)
	}
}

// titles is the shape of a node slice, for assertions that care about order.
func titles(ns []notes.Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Title
	}
	return out
}

func TestChildrenInPositionOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent")
	f.mk(t, parent.ID, "a")
	f.mk(t, parent.ID, "b")
	f.mk(t, notes.RootID, "not a child")

	got, err := f.store.Children(ctx, f.alice.ID, parent.ID)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if want := []string{"a", "b"}; !slices.Equal(titles(got), want) {
		t.Fatalf("Children = %v; want %v", titles(got), want)
	}
}

func TestChildrenOfRoot(t *testing.T) {
	f := newFixture(t)
	top := f.mk(t, notes.RootID, "top")
	f.mk(t, top.ID, "nested")

	got, err := f.store.Children(context.Background(), f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if want := []string{"top"}; !slices.Equal(titles(got), want) {
		t.Fatalf("Children(RootID) = %v; want %v", titles(got), want)
	}
}

func TestChildrenExcludesAnotherUser(t *testing.T) {
	f := newFixture(t)
	f.mk(t, notes.RootID, "alice's")

	got, err := f.store.Children(context.Background(), f.bob.ID, notes.RootID)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("bob sees %v at the top level; want nothing", titles(got))
	}
}

func TestAncestorsIsTheBreadcrumb(t *testing.T) {
	f := newFixture(t)
	top := f.mk(t, notes.RootID, "top")
	mid := f.mk(t, top.ID, "mid")
	leaf := f.mk(t, mid.ID, "leaf")

	got, err := f.store.Ancestors(context.Background(), f.alice.ID, leaf.ID)
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	// Outermost first, and the node itself is not included.
	if want := []string{"top", "mid"}; !slices.Equal(titles(got), want) {
		t.Fatalf("Ancestors = %v; want %v", titles(got), want)
	}
}

func TestAncestorsOfATopLevelNodeIsEmpty(t *testing.T) {
	f := newFixture(t)
	top := f.mk(t, notes.RootID, "top")

	got, err := f.store.Ancestors(context.Background(), f.alice.ID, top.ID)
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Ancestors of a top-level node = %v; want nothing", titles(got))
	}
}

func TestAncestorsOfAnotherUsersNodeIsEmpty(t *testing.T) {
	f := newFixture(t)
	top := f.mk(t, notes.RootID, "top")
	leaf := f.mk(t, top.ID, "leaf")

	got, err := f.store.Ancestors(context.Background(), f.bob.ID, leaf.ID)
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("bob sees ancestors %v; want nothing", titles(got))
	}
}

// outlineShape renders an outline the way a person reads one, so a failing
// assertion prints a tree instead of a wall of structs.
func outlineShape(ns []notes.Node) string {
	var b strings.Builder
	for _, n := range ns {
		b.WriteString(strings.Repeat("  ", n.Depth))
		b.WriteString("- ")
		b.WriteString(n.Title)
		if n.HasChildren {
			b.WriteString(" [+]")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// sample builds the tree every Outline test works from — two top-level
// bullets, a and b, where a has two children and a2 has one:
//
//	a
//	├── a1
//	└── a2
//	    └── a2x
//	b
func (f *fixture) sample(t *testing.T) (a, a1, a2, a2x, b notes.Node) {
	t.Helper()
	a = f.mk(t, notes.RootID, "a")
	a1 = f.mk(t, a.ID, "a1")
	a2 = f.mk(t, a.ID, "a2")
	a2x = f.mk(t, a2.ID, "a2x")
	b = f.mk(t, notes.RootID, "b")
	return a, a1, a2, a2x, b
}

func TestOutlineIsFlatDocumentOrder(t *testing.T) {
	f := newFixture(t)
	f.sample(t)

	got, err := f.store.Outline(context.Background(), f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	want := "- a [+]\n  - a1\n  - a2 [+]\n    - a2x\n- b\n"
	if outlineShape(got) != want {
		t.Fatalf("Outline =\n%s\nwant\n%s", outlineShape(got), want)
	}
}

func TestOutlineStopsAtACollapsedNode(t *testing.T) {
	f := newFixture(t)
	_, _, a2, _, _ := f.sample(t)

	if err := f.store.SetCollapsed(context.Background(), f.alice.ID, a2.ID, true); err != nil {
		t.Fatalf("SetCollapsed: %v", err)
	}

	got, err := f.store.Outline(context.Background(), f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	// a2x is gone, but a2 still advertises that it has children so the
	// expand arrow can be drawn.
	want := "- a [+]\n  - a1\n  - a2 [+]\n- b\n"
	if outlineShape(got) != want {
		t.Fatalf("Outline =\n%s\nwant\n%s", outlineShape(got), want)
	}
}

func TestOutlineZoomsIntoANode(t *testing.T) {
	f := newFixture(t)
	a, _, _, _, _ := f.sample(t)

	got, err := f.store.Outline(context.Background(), f.alice.ID, a.ID)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	// The zoom root's direct children are at depth 0, and b is not in view.
	want := "- a1\n- a2 [+]\n  - a2x\n"
	if outlineShape(got) != want {
		t.Fatalf("Outline(zoomed) =\n%s\nwant\n%s", outlineShape(got), want)
	}
}

func TestOutlineExcludesAnotherUser(t *testing.T) {
	f := newFixture(t)
	f.sample(t)

	got, err := f.store.Outline(context.Background(), f.bob.ID, notes.RootID)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("bob's outline =\n%s\nwant nothing", outlineShape(got))
	}
}

func TestOutlineOfAnotherUsersNodeIsEmpty(t *testing.T) {
	f := newFixture(t)
	a, _, _, _, _ := f.sample(t)

	got, err := f.store.Outline(context.Background(), f.bob.ID, a.ID)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("bob zoomed into alice's node and saw\n%s\nwant nothing", outlineShape(got))
	}
}

// The titles the tests below must never let alice see. They are asserted on as
// text because text is what would actually appear on somebody's screen: a leak
// caught by a node id is a leak nobody would recognise as one.
const (
	bobsBullet = "bob's private bullet"
	bobsChild  = "bob's private child"
)

// brokenI2 builds a tree in which invariant I2 is violated in both directions:
// one of bob's bullets is a child of one of alice's, and one of alice's is a
// child of bob's. It returns alice's "a" and "a2x", the two bullets the reads
// below are pointed at.
//
//	a        (alice)
//	├── a1   (alice)
//	├── a2   (alice)
//	└── B    (bob)    ← I2: a child whose owner is not its parent's
//	    ├── Bc  (bob)
//	    └── a2x (alice) ← I2, the other way round
//	b        (alice)
//
// I2 is an application invariant, not a schema constraint — notes_nodes has a
// plain foreign key on parent_id, not a composite (parent_id, user_id) one —
// so a bug, a manual repair, or a later chunk's import path can produce this
// table. Both recursive reads filter their walk by owner rather than leaning
// on I2 to supply it, and that filter is what these tests pin.
//
// Nothing here calls checkInvariants. It would fail, correctly: this database
// really does violate I2, deliberately and in two places. That is the premise
// of the tests, not an accident in them.
func (f *fixture) brokenI2(t *testing.T) (a, a2x notes.Node) {
	t.Helper()

	a, _, _, a2x, _ = f.sample(t)
	bobsTop := f.mkFor(t, f.bob.ID, notes.RootID, bobsBullet)
	f.mkFor(t, f.bob.ID, bobsTop.ID, bobsChild)

	// bob's subtree becomes a's third child, and alice's a2x becomes a leaf
	// inside it. Positions are chosen to keep I1 intact, so that a failure
	// here is unambiguously about ownership.
	f.reparent(t, bobsTop.ID, a.ID, 2)
	f.reparent(t, a2x.ID, bobsTop.ID, 1)
	return a, a2x
}

// leaked names any of bob's bullets that turned up in a result alice asked for.
func leaked(ns []notes.Node, aliceID int64) []string {
	var out []string
	for _, n := range ns {
		if n.UserID != aliceID {
			out = append(out, fmt.Sprintf("%q (user %d)", n.Title, n.UserID))
		}
	}
	return out
}

func TestOutlineDoesNotLeakAnotherUsersBulletsWhenI2IsBroken(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, _ := f.brokenI2(t)

	for _, tc := range []struct {
		name string
		root int64
		want string
	}{
		// The descent stops dead at bob's bullet, so his subtree — and
		// alice's own a2x hanging below it — simply does not render.
		{"from the top level", notes.RootID, "- a [+]\n  - a1\n  - a2\n- b\n"},
		{"zoomed into the grafted parent", a.ID, "- a1\n- a2\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := f.store.Outline(ctx, f.alice.ID, tc.root)
			if err != nil {
				t.Fatalf("Outline: %v", err)
			}
			if bad := leaked(got, f.alice.ID); len(bad) > 0 {
				t.Fatalf("alice's outline contains another user's bullets: %s\nfull outline:\n%s",
					strings.Join(bad, ", "), outlineShape(got))
			}
			if outlineShape(got) != tc.want {
				t.Fatalf("Outline =\n%s\nwant\n%s", outlineShape(got), tc.want)
			}
		})
	}
}

func TestAncestorsDoesNotLeakAnotherUsersBulletsWhenI2IsBroken(t *testing.T) {
	f := newFixture(t)
	_, a2x := f.brokenI2(t)

	// a2x is alice's, so the anchor row matches and the walk starts. Its
	// parent is bob's, so the walk stops there rather than climbing on to
	// alice's "a": an unfiltered breadcrumb would read "a > bob's private
	// bullet" above a bullet of her own.
	got, err := f.store.Ancestors(context.Background(), f.alice.ID, a2x.ID)
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	if bad := leaked(got, f.alice.ID); len(bad) > 0 {
		t.Fatalf("alice's breadcrumb contains another user's bullets: %s", strings.Join(bad, ", "))
	}
	if len(got) != 0 {
		t.Fatalf("Ancestors = %v; want nothing — the walk cannot climb past a parent it does not own", titles(got))
	}
}

func TestChildrenDoesNotLeakAnotherUsersBulletsWhenI2IsBroken(t *testing.T) {
	f := newFixture(t)
	a, _ := f.brokenI2(t)

	// Children cannot leak the way the recursive reads could: it is a single
	// level, and its user_id predicate applies to the very rows it returns,
	// not to a join edge that a broken I2 could route around. This test is
	// therefore a pin rather than a discovery — it costs one query, and it
	// stops a later rewrite of Children as a CTE from dropping the filter.
	got, err := f.store.Children(context.Background(), f.alice.ID, a.ID)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if bad := leaked(got, f.alice.ID); len(bad) > 0 {
		t.Fatalf("alice's children contain another user's bullets: %s", strings.Join(bad, ", "))
	}
	if want := []string{"a1", "a2"}; !slices.Equal(titles(got), want) {
		t.Fatalf("Children = %v; want %v", titles(got), want)
	}
}

// cycleDeadline bounds a read that is being pointed at a cycle. Without it, a
// broken MaxDepth cap does not fail the test — it hangs, and CI reports a
// ten-minute panic across the whole package with no test name attached to it.
// Five seconds is thousands of times what the capped query needs.
const cycleDeadline = 5 * time.Second

func TestAncestorsTerminatesOnACycle(t *testing.T) {
	f := newFixture(t)

	top := f.mk(t, notes.RootID, "top")
	mid := f.mk(t, top.ID, "mid")
	leaf := f.mk(t, mid.ID, "leaf")

	// Close the loop by hand: top becomes a child of its own grandchild. Every
	// row still belongs to alice, so the owner filter on the walk lets it
	// through and the MaxDepth cap is the only thing that ends it.
	//
	// checkInvariants is not called here, and must not be: this is an I3
	// violation on purpose.
	f.reparent(t, top.ID, leaf.ID, 0)

	ctx, cancel := context.WithTimeout(context.Background(), cycleDeadline)
	defer cancel()

	got, err := f.store.Ancestors(ctx, f.alice.ID, leaf.ID)
	if err != nil {
		if ctx.Err() != nil {
			t.Fatalf("Ancestors did not return within %s: the MaxDepth cap is not ending the walk (%v)",
				cycleDeadline, err)
		}
		t.Fatalf("Ancestors: %v", err)
	}
	// The walk climbs one row per step and stops at MaxDepth, so the cycle
	// yields exactly that many rows and never more.
	if len(got) == 0 {
		t.Fatal("Ancestors returned nothing; the walk did not start, so this proves nothing about the cap")
	}
	if len(got) > notes.MaxDepth {
		t.Fatalf("Ancestors returned %d rows; want at most MaxDepth (%d)", len(got), notes.MaxDepth)
	}
}

func TestOutlineTerminatesOnACycle(t *testing.T) {
	f := newFixture(t)

	x := f.mk(t, notes.RootID, "x")
	y := f.mk(t, x.ID, "y")

	// A cycle cannot be reached from a top-level bullet: every node has
	// exactly one parent, so a node inside a cycle has its parent inside the
	// cycle too, and nothing outside can point into it. A cycle left hanging
	// off nowhere is unreachable, and an outline of it is empty — which would
	// exercise no cap at all.
	//
	// A zoomed outline is the case that does reach one. Its anchor is "the
	// children of this id" rather than "the top level", so zooming into a
	// member of the cycle starts the descent inside it, and the descent then
	// goes round for ever unless MaxDepth stops it.
	//
	// checkInvariants is not called here, and must not be: x and y are each
	// inside their own subtree, which is an I3 violation on purpose.
	f.reparent(t, x.ID, y.ID, 0)

	ctx, cancel := context.WithTimeout(context.Background(), cycleDeadline)
	defer cancel()

	got, err := f.store.Outline(ctx, f.alice.ID, x.ID)
	if err != nil {
		if ctx.Err() != nil {
			t.Fatalf("Outline did not return within %s: the MaxDepth cap is not ending the descent (%v)",
				cycleDeadline, err)
		}
		t.Fatalf("Outline: %v", err)
	}
	// One row at depth 0, then one per level down to MaxDepth.
	if len(got) == 0 {
		t.Fatal("Outline returned nothing; the descent did not start, so this proves nothing about the cap")
	}
	if len(got) > notes.MaxDepth+1 {
		t.Fatalf("Outline returned %d rows; want at most MaxDepth+1 (%d)", len(got), notes.MaxDepth+1)
	}
}
