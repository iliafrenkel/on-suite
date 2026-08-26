// This file is deliberately "package notes", unlike every other test file in
// this directory: nest is unexported, it is a rendering detail that does not
// belong in the store's public API, and its most interesting case cannot be
// produced through the store at all.
package notes

import (
	"strings"
	"testing"
)

// draw renders a nested row tree as indented text, so a test can state the
// shape it expects instead of walking pointers. A row that is last among its
// siblings is marked with a trailing "*".
func draw(rows []*outlineRow) string {
	var b strings.Builder
	var walk func([]*outlineRow, int)
	walk = func(rows []*outlineRow, depth int) {
		for _, r := range rows {
			b.WriteString(strings.Repeat("  ", depth))
			b.WriteString(r.Title)
			if r.Last {
				b.WriteString("*")
			}
			b.WriteString("\n")
			walk(r.Children, depth+1)
		}
	}
	walk(rows, 0)
	return b.String()
}

// flat builds the kind of slice Outline returns: pre-order, each row carrying
// its depth.
func flat(spec ...struct {
	depth int
	title string
}) []Node {
	out := make([]Node, 0, len(spec))
	for i, s := range spec {
		out = append(out, Node{ID: int64(i + 1), Title: s.title, Depth: s.depth})
	}
	return out
}

type lvl = struct {
	depth int
	title string
}

func TestNestBuildsTheTree(t *testing.T) {
	rows := nest(flat(
		lvl{0, "a"},
		lvl{1, "a1"},
		lvl{2, "a1x"},
		lvl{1, "a2"},
		lvl{0, "b"},
	), RootID, "tok", "9999-12-31")

	want := "a\n  a1\n    a1x*\n  a2*\nb*\n"
	if got := draw(rows); got != want {
		t.Errorf("nest built\n%s\nwant\n%s", got, want)
	}
}

func TestNestMarksTheLastSiblingAtEveryLevel(t *testing.T) {
	rows := nest(flat(
		lvl{0, "a"},
		lvl{1, "a1"},
		lvl{1, "a2"},
	), RootID, "tok", "9999-12-31")

	if rows[0].Last != true {
		t.Error("the only top-level row is not marked last")
	}
	if rows[0].Children[0].Last {
		t.Error("a1 is marked last but a2 follows it")
	}
	if !rows[0].Children[1].Last {
		t.Error("a2 is not marked last")
	}
}

// TestNestStampsEveryRow: each row is exactly the inputs of one form, so it
// carries the hidden fields that form needs rather than reaching for a shared
// parent the template has no way to address from inside a recursive block.
func TestNestStampsEveryRow(t *testing.T) {
	rows := nest(flat(lvl{0, "a"}, lvl{1, "a1"}), 42, "tok", "9999-12-31")

	var seen int
	var walk func([]*outlineRow)
	walk = func(rows []*outlineRow) {
		for _, r := range rows {
			seen++
			if r.RootID != 42 {
				t.Errorf("%q carries root %d, want 42", r.Title, r.RootID)
			}
			if r.CSRFToken != "tok" {
				t.Errorf("%q carries token %q, want tok", r.Title, r.CSRFToken)
			}
			walk(r.Children)
		}
	}
	walk(rows)
	if seen != 2 {
		t.Errorf("walked %d rows, want 2", seen)
	}
}

func TestNestRendersMarkdownIntoEachRow(t *testing.T) {
	rows := nest([]Node{{ID: 1, Title: "**bold**", Note: "*italic*", Depth: 0}}, RootID, "tok", "9999-12-31")
	if got := string(rows[0].RenderedTitle); got != "<strong>bold</strong>" {
		t.Errorf("RenderedTitle = %q", got)
	}
	if got := string(rows[0].RenderedNote); got != "<em>italic</em>" {
		t.Errorf("RenderedNote = %q", got)
	}
}

// TestNestDropsARowWithNoParent is the case the store cannot produce and the
// renderer must survive: a depth that skips a level has no correct parent, and
// guessing one would put a bullet somewhere the user never left it.
func TestNestDropsARowWithNoParent(t *testing.T) {
	rows := nest(flat(
		lvl{0, "a"},
		lvl{2, "orphan"},
		lvl{3, "orphan's child"},
		lvl{1, "a1"},
	), RootID, "tok", "9999-12-31")

	// "a" is the only surviving top-level row, so it is also the last one.
	want := "a*\n  a1*\n"
	if got := draw(rows); got != want {
		t.Errorf("nest built\n%s\nwant\n%s", got, want)
	}
}

// TestNestResetsTheOpenChainAtEachTopLevelRow is the case open = open[:0]
// exists for: without it, a child of a second top-level root would still find
// the first root's chain sitting in open and attach to it instead.
func TestNestResetsTheOpenChainAtEachTopLevelRow(t *testing.T) {
	rows := nest(flat(
		lvl{0, "a"},
		lvl{1, "a1"},
		lvl{0, "b"},
		lvl{1, "b1"},
	), RootID, "tok", "9999-12-31")

	want := "a\n  a1*\nb*\n  b1*\n"
	if got := draw(rows); got != want {
		t.Errorf("nest built\n%s\nwant\n%s", got, want)
	}
}

func TestNestOfNothingIsNothing(t *testing.T) {
	if rows := nest(nil, RootID, "tok", "9999-12-31"); len(rows) != 0 {
		t.Errorf("nest(nil) returned %d rows", len(rows))
	}
}

// TestNestHandlesTheDeepestPermittedOutline: MaxDepth is the cap Outline
// enforces, so nest has to reach it without reallocating its way into a wrong
// answer.
func TestNestHandlesTheDeepestPermittedOutline(t *testing.T) {
	spec := make([]lvl, 0, MaxDepth+1)
	for d := 0; d <= MaxDepth; d++ {
		spec = append(spec, lvl{d, "d" + string(rune('0'+d%10))})
	}
	rows := nest(flat(spec...), RootID, "tok", "9999-12-31")

	depth := 0
	for cur := rows; len(cur) > 0; cur = cur[0].Children {
		depth++
	}
	if depth != MaxDepth+1 {
		t.Errorf("the tree is %d deep, want %d", depth, MaxDepth+1)
	}
}

func TestHideDoneHidesADoneNodeAndItsSubtree(t *testing.T) {
	flat := []Node{
		{ID: 1, Title: "done", Done: true, Depth: 0},
		{ID: 2, Title: "child of done", Depth: 1},
		{ID: 3, Title: "grandchild", Depth: 2},
		{ID: 4, Title: "sibling", Depth: 0},
	}
	got := hideDone(flat, false)
	if len(got) != 1 || got[0].ID != 4 {
		t.Fatalf("hideDone(false) = %+v, want only the sibling", got)
	}
}

func TestHideDoneShowsEverythingWhenOn(t *testing.T) {
	flat := []Node{
		{ID: 1, Title: "done", Done: true, Depth: 0},
		{ID: 2, Title: "child of done", Depth: 1},
	}
	got := hideDone(flat, true)
	if len(got) != 2 {
		t.Fatalf("hideDone(true) = %+v, want everything", got)
	}
}

// TestHideDoneHandlesConsecutiveDoneSubtrees: two separate done subtrees
// back to back must not let one's skip swallow the other's sibling.
func TestHideDoneHandlesConsecutiveDoneSubtrees(t *testing.T) {
	flat := []Node{
		{ID: 1, Title: "done A", Done: true, Depth: 0},
		{ID: 2, Title: "child of A", Depth: 1},
		{ID: 3, Title: "between", Depth: 0},
		{ID: 4, Title: "done B", Done: true, Depth: 0},
		{ID: 5, Title: "child of B", Depth: 1},
		{ID: 6, Title: "after", Depth: 0},
	}
	got := hideDone(flat, false)
	var ids []int64
	for _, n := range got {
		ids = append(ids, n.ID)
	}
	want := []int64{3, 6}
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("hideDone left %v, want %v", ids, want)
	}
}
