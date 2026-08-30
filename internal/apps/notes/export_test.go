package notes_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

func TestExportReturnsTheWholeTreeInPreOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent")
	child := f.mk(t, parent.ID, "child")
	sibling := f.mk(t, notes.RootID, "sibling")

	got, err := f.store.Export(ctx, f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("Export = %+v, want 3 nodes", got)
	}
	if got[0].ID != parent.ID || got[0].Depth != 0 {
		t.Errorf("row 0 = %+v, want parent at depth 0", got[0])
	}
	if got[1].ID != child.ID || got[1].Depth != 1 {
		t.Errorf("row 1 = %+v, want child at depth 1", got[1])
	}
	if got[2].ID != sibling.ID || got[2].Depth != 0 {
		t.Errorf("row 2 = %+v, want sibling at depth 0", got[2])
	}
}

// TestExportIncludesCollapsedDoneAndArchivedNodes: unlike Outline
// (store.go), an export is a full data dump, not a display view — spec
// §14 gives Markdown export no filtering rule at all, unlike the outline,
// search and due list, each of which spec explicitly narrows.
func TestExportIncludesCollapsedDoneAndArchivedNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	collapsed := f.mk(t, notes.RootID, "collapsed")
	underCollapsed := f.mk(t, collapsed.ID, "under collapsed")
	done := f.mk(t, notes.RootID, "done")
	archived := f.mk(t, notes.RootID, "archived")

	if err := f.store.SetCollapsed(ctx, f.alice.ID, collapsed.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetDone(ctx, f.alice.ID, done.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetArchived(ctx, f.alice.ID, archived.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Export(ctx, f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, n := range got {
		ids = append(ids, n.ID)
	}
	for _, want := range []int64{collapsed.ID, underCollapsed.ID, done.ID, archived.ID} {
		found := false
		for _, id := range ids {
			if id == want {
				found = true
			}
		}
		if !found {
			t.Errorf("Export = %v, missing %d", ids, want)
		}
	}
}

func TestExportOfASubtreeExcludesTheRootItself(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	root := f.mk(t, notes.RootID, "root")
	child := f.mk(t, root.ID, "child")
	f.mk(t, notes.RootID, "unrelated")

	got, err := f.store.Export(ctx, f.alice.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != child.ID || got[0].Depth != 0 {
		t.Fatalf("Export(root) = %+v, want only the child at depth 0", got)
	}
}

func TestExportDoesNotLeakAnotherUsersNodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "alice's")
	f.mkFor(t, f.bob.ID, notes.RootID, "bob's")

	got, err := f.store.Export(ctx, f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "alice's" {
		t.Fatalf("Export = %+v, want only alice's own node", got)
	}
}

func TestNotesImplementsTheExporterInterface(t *testing.T) {
	// A compile-time assertion would be enough (app.go carries one), but a
	// failing test here names the problem more clearly than a type error
	// in an unrelated file — mirrors internal/apps/paste/export_test.go.
	var a any = notes.New()
	if _, ok := a.(app.Exporter); !ok {
		t.Fatal("*notes.App does not implement app.Exporter")
	}
}

func TestExportMarkdownRendersOneBulletPerLine(t *testing.T) {
	flat := []notes.Node{
		{ID: 1, Title: "Software projects", Note: "Various software projects", Depth: 0},
		{ID: 2, Title: "AtBudget", Depth: 1},
		{ID: 3, Title: "Project objectives", Done: true, Depth: 2},
		{ID: 4, Title: "API", DueOn: "2026-09-01", Depth: 2},
	}
	want := "- Software projects\n" +
		"  Various software projects\n" +
		"  - AtBudget\n" +
		"    - Project objectives [x]\n" +
		"    - API @2026-09-01\n"
	if got := notes.ExportMarkdown(flat); got != want {
		t.Errorf("ExportMarkdown =\n%q\nwant\n%q", got, want)
	}
}

func TestExportMarkdownCombinesDoneAndDueOnOneLine(t *testing.T) {
	flat := []notes.Node{{ID: 1, Title: "both", Done: true, DueOn: "2026-09-01", Depth: 0}}
	want := "- both [x] @2026-09-01\n"
	if got := notes.ExportMarkdown(flat); got != want {
		t.Errorf("ExportMarkdown = %q, want %q", got, want)
	}
}

func TestExportMarkdownWritesMultiLineNotesAsConsecutiveIndentedLines(t *testing.T) {
	flat := []notes.Node{{ID: 1, Title: "task", Note: "line one\nline two", Depth: 0}}
	want := "- task\n  line one\n  line two\n"
	if got := notes.ExportMarkdown(flat); got != want {
		t.Errorf("ExportMarkdown = %q, want %q", got, want)
	}
}

func TestExportMarkdownOfNothingIsEmpty(t *testing.T) {
	if got := notes.ExportMarkdown(nil); got != "" {
		t.Errorf("ExportMarkdown(nil) = %q, want empty", got)
	}
}

func TestJSONExportContainsEveryColumn(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	n := f.mk(t, notes.RootID, "task")
	if err := f.store.SetDone(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetDue(ctx, f.alice.ID, n.ID, "2026-09-01"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetCollapsed(ctx, f.alice.ID, n.ID, true); err != nil {
		t.Fatal(err)
	}
	f.mkFor(t, f.bob.ID, notes.RootID, "bob's")

	payload, err := notes.New().Export(ctx, f.db, f.alice.ID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("the payload does not marshal: %v", err)
	}
	text := string(encoded)

	for _, want := range []string{`"task"`, `"done":true`, `"due_on":"2026-09-01"`, `"collapsed":true`} {
		if !strings.Contains(text, want) {
			t.Errorf("export is missing %s: %s", want, text)
		}
	}
	if strings.Contains(text, "bob's") {
		t.Error("the export contains another user's node")
	}
}
