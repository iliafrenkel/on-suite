package notes_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)

func TestParseMarkdownParsesTheSpecExample(t *testing.T) {
	text := "- Software projects\n" +
		"  Various software development projects that I'm involved in\n" +
		"  - AtBudget\n" +
		"    - Project objectives [x]\n" +
		"    - API @2026-09-01\n"

	got, err := notes.ParseMarkdown(text)
	if err != nil {
		t.Fatalf("ParseMarkdown: %v", err)
	}
	want := []notes.ParsedNode{
		{Depth: 0, Title: "Software projects", Note: "Various software development projects that I'm involved in"},
		{Depth: 1, Title: "AtBudget"},
		{Depth: 2, Title: "Project objectives", Done: true},
		{Depth: 2, Title: "API", DueOn: "2026-09-01"},
	}
	if len(got) != len(want) {
		t.Fatalf("ParseMarkdown returned %d nodes, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("node %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseMarkdownCombinesDoneAndDueOnOneLine(t *testing.T) {
	got, err := notes.ParseMarkdown("- both [x] @2026-09-01\n")
	if err != nil {
		t.Fatal(err)
	}
	want := notes.ParsedNode{Depth: 0, Title: "both", Done: true, DueOn: "2026-09-01"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ParseMarkdown = %+v, want [%+v]", got, want)
	}
}

func TestParseMarkdownJoinsConsecutiveNoteLines(t *testing.T) {
	got, err := notes.ParseMarkdown("- task\n  line one\n  line two\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Note != "line one\nline two" {
		t.Fatalf("ParseMarkdown = %+v, want a two-line note", got)
	}
}

func TestParseMarkdownPreservesIndentationDeeperThanTheMinimum(t *testing.T) {
	got, err := notes.ParseMarkdown("- task\n    indented extra\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Note != "  indented extra" {
		t.Fatalf("ParseMarkdown = %+v, want a note with its extra indentation preserved", got)
	}
}

func TestParseMarkdownStripsCarriageReturns(t *testing.T) {
	got, err := notes.ParseMarkdown("- task\r\n  a note\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "task" || got[0].Note != "a note" {
		t.Fatalf("ParseMarkdown = %+v, want no stray carriage returns", got)
	}
}

func TestParseMarkdownSkipsBlankLines(t *testing.T) {
	got, err := notes.ParseMarkdown("- first\n\n- second\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Title != "first" || got[1].Title != "second" {
		t.Fatalf("ParseMarkdown = %+v, want two siblings", got)
	}
}

func TestParseMarkdownAllowsAnEmptyTitle(t *testing.T) {
	// Validate (notes.go) already allows an empty title — spec §5 says
	// pressing Enter creates one — so the parser must not reject one
	// either.
	got, err := notes.ParseMarkdown("- \n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "" {
		t.Fatalf("ParseMarkdown = %+v, want one node with an empty title", got)
	}
}

func TestParseMarkdownOfEmptyTextIsEmpty(t *testing.T) {
	got, err := notes.ParseMarkdown("")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("ParseMarkdown(\"\") = %+v, want none", got)
	}
}

func TestParseMarkdownRejectsADepthThatSkipsALevel(t *testing.T) {
	_, err := notes.ParseMarkdown("- top\n    - grandchild with no child\n")
	if err == nil {
		t.Fatal("ParseMarkdown = nil error, want ErrInvalid")
	}
}

func TestParseMarkdownRejectsStartingIndented(t *testing.T) {
	_, err := notes.ParseMarkdown("  - indented from the very first line\n")
	if err == nil {
		t.Fatal("ParseMarkdown = nil error, want ErrInvalid")
	}
}

func TestParseMarkdownRejectsOddIndentation(t *testing.T) {
	_, err := notes.ParseMarkdown("- top\n   - three spaces, not two\n")
	if err == nil {
		t.Fatal("ParseMarkdown = nil error, want ErrInvalid")
	}
}

func TestParseMarkdownRejectsTextNotUnderABullet(t *testing.T) {
	_, err := notes.ParseMarkdown("stray text with no bullet before it\n")
	if err == nil {
		t.Fatal("ParseMarkdown = nil error, want ErrInvalid")
	}
}

func TestParseMarkdownRejectsMoreThanMaxImportNodes(t *testing.T) {
	var b strings.Builder
	for i := 0; i <= notes.MaxImportNodes; i++ {
		b.WriteString("- x\n")
	}
	_, err := notes.ParseMarkdown(b.String())
	if err == nil {
		t.Fatal("ParseMarkdown = nil error, want ErrInvalid")
	}
}

// TestParseMarkdownAcceptsExactlyMaxImportNodes: the boundary itself must
// still succeed — only exceeding it is an error.
func TestParseMarkdownAcceptsExactlyMaxImportNodes(t *testing.T) {
	var b strings.Builder
	for i := 0; i < notes.MaxImportNodes; i++ {
		b.WriteString("- x\n")
	}
	got, err := notes.ParseMarkdown(b.String())
	if err != nil {
		t.Fatalf("ParseMarkdown: %v", err)
	}
	if len(got) != notes.MaxImportNodes {
		t.Fatalf("ParseMarkdown returned %d nodes, want %d", len(got), notes.MaxImportNodes)
	}
}

func TestParseMarkdownDoesNotSemanticallyValidateTheDueDate(t *testing.T) {
	// Syntax only, by design — see this task's plan for why full
	// ValidateDue-style checking is deferred to Ops.SetDue during
	// ImportUnder (Task 5), which already runs it: this avoids
	// duplicating that logic here.
	got, err := notes.ParseMarkdown("- impossible date @2026-02-30\n")
	if err != nil {
		t.Fatalf("ParseMarkdown: %v", err)
	}
	if len(got) != 1 || got[0].DueOn != "2026-02-30" {
		t.Fatalf("ParseMarkdown = %+v, want the raw due string carried through unvalidated", got)
	}
}

func TestImportUnderCreatesTheWholeSubtree(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parsed, err := notes.ParseMarkdown(
		"- Software projects\n" +
			"  Various software development projects that I'm involved in\n" +
			"  - AtBudget\n" +
			"    - Project objectives [x]\n" +
			"    - API @2026-09-01\n")
	if err != nil {
		t.Fatal(err)
	}

	created, err := f.store.ImportUnder(ctx, f.alice.ID, notes.RootID, parsed)
	if err != nil {
		t.Fatal(err)
	}
	if created != 4 {
		t.Fatalf("ImportUnder created %d nodes, want 4", created)
	}

	top := f.childTitles(t, f.alice.ID, notes.RootID)
	if len(top) != 1 || top[0] != "Software projects" {
		t.Fatalf("top level = %v, want just Software projects", top)
	}
	projects, err := f.store.ByID(ctx, f.alice.ID, mustFindID(t, f, "Software projects"))
	if err != nil {
		t.Fatal(err)
	}
	if projects.Note != "Various software development projects that I'm involved in" {
		t.Errorf("note = %q", projects.Note)
	}

	objectives, err := f.store.ByID(ctx, f.alice.ID, mustFindID(t, f, "Project objectives"))
	if err != nil {
		t.Fatal(err)
	}
	if !objectives.Done {
		t.Error("Project objectives should be done")
	}

	api, err := f.store.ByID(ctx, f.alice.ID, mustFindID(t, f, "API"))
	if err != nil {
		t.Fatal(err)
	}
	if api.DueOn != "2026-09-01" {
		t.Errorf("API's due date = %q, want 2026-09-01", api.DueOn)
	}
}

// mustFindID walks alice's whole tree looking for a node with the given
// title, for tests that need a real id to look a freshly imported node up
// by. Import assigns ids the caller cannot predict in advance.
func mustFindID(t *testing.T, f *fixture, title string) int64 {
	t.Helper()
	flat, err := f.store.Export(context.Background(), f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range flat {
		if n.Title == title {
			return n.ID
		}
	}
	t.Fatalf("no node titled %q", title)
	return 0
}

func TestImportUnderAppendsAfterExistingChildren(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mk(t, notes.RootID, "existing")
	parsed, err := notes.ParseMarkdown("- new\n")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.ImportUnder(ctx, f.alice.ID, notes.RootID, parsed); err != nil {
		t.Fatal(err)
	}

	got := f.childTitles(t, f.alice.ID, notes.RootID)
	want := []string{"existing", "new"}
	if !equalStrings(got, want) {
		t.Fatalf("top level = %v, want %v", got, want)
	}
}

func TestImportUnderUnderASpecificParent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent")
	parsed, err := notes.ParseMarkdown("- child\n")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.ImportUnder(ctx, f.alice.ID, parent.ID, parsed); err != nil {
		t.Fatal(err)
	}
	got := f.childTitles(t, f.alice.ID, parent.ID)
	if len(got) != 1 || got[0] != "child" {
		t.Fatalf("children of parent = %v, want just child", got)
	}
}

func TestImportUnderRejectsAMalformedDueDate(t *testing.T) {
	f := newFixture(t)
	parsed, err := notes.ParseMarkdown("- impossible @2026-02-30\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ImportUnder(context.Background(), f.alice.ID, notes.RootID, parsed); !errors.Is(err, notes.ErrInvalid) {
		t.Fatalf("ImportUnder = %v, want ErrInvalid", err)
	}
}

// TestImportUnderIsOneTransaction: a bad due date on the second bullet
// must roll back the first — spec §7's one-write discipline extends here
// too, so a partially-imported file never sits half-applied.
func TestImportUnderIsOneTransaction(t *testing.T) {
	f := newFixture(t)
	parsed, err := notes.ParseMarkdown("- fine\n- impossible @2026-02-30\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ImportUnder(context.Background(), f.alice.ID, notes.RootID, parsed); !errors.Is(err, notes.ErrInvalid) {
		t.Fatalf("ImportUnder = %v, want ErrInvalid", err)
	}
	if got := f.childTitles(t, f.alice.ID, notes.RootID); len(got) != 0 {
		t.Fatalf("top level = %v, want nothing — the whole import should have rolled back", got)
	}
}

func TestImportUnderRejectsAnotherUsersParent(t *testing.T) {
	f := newFixture(t)
	bobs := f.mkFor(t, f.bob.ID, notes.RootID, "bob's")
	parsed, err := notes.ParseMarkdown("- x\n")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.store.ImportUnder(context.Background(), f.alice.ID, bobs.ID, parsed)
	if !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("ImportUnder under bob's node = %v, want ErrNotFound", err)
	}
}

// TestExportThenImportRoundTrips is spec §14's own claim: "because done
// state and due dates are encoded, a document round-trips." Exports a
// tree, imports the resulting text back under a fresh parent, and checks
// the copy matches the original on every field Markdown carries.
func TestExportThenImportRoundTrips(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "Software projects")
	if err := f.store.SetText(ctx, f.alice.ID, parent.ID, "Software projects", "a note"); err != nil {
		t.Fatal(err)
	}
	child := f.mk(t, parent.ID, "AtBudget")
	grandchild := f.mk(t, child.ID, "Project objectives")
	if err := f.store.SetDone(ctx, f.alice.ID, grandchild.ID, true); err != nil {
		t.Fatal(err)
	}
	sibling := f.mk(t, child.ID, "API")
	if err := f.store.SetDue(ctx, f.alice.ID, sibling.ID, "2026-09-01"); err != nil {
		t.Fatal(err)
	}

	exported, err := f.store.Export(ctx, f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	md := notes.ExportMarkdown(exported)

	parsed, err := notes.ParseMarkdown(md)
	if err != nil {
		t.Fatalf("re-parsing the export: %v", err)
	}
	copyRoot := f.mk(t, notes.RootID, "copy destination")
	if _, err := f.store.ImportUnder(ctx, f.alice.ID, copyRoot.ID, parsed); err != nil {
		t.Fatal(err)
	}

	reExported, err := f.store.Export(ctx, f.alice.ID, copyRoot.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := notes.ExportMarkdown(reExported)
	if got != md {
		t.Errorf("round-trip mismatch:\noriginal:\n%s\nafter round-trip:\n%s", md, got)
	}
}

// TestExportThenImportRoundTripsABulletShapedNote pins the note-escaping
// fix: a note whose text is deliberately bullet-shaped ("- x") must survive
// ExportMarkdown -> ParseMarkdown as the literal string "- x" under its
// bullet's own Note field — not become a child bullet of its own, and not
// cause the whole file to be rejected.
func TestExportThenImportRoundTripsABulletShapedNote(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent")
	if err := f.store.SetText(ctx, f.alice.ID, parent.ID, "parent", "- x"); err != nil {
		t.Fatal(err)
	}

	exported, err := f.store.Export(ctx, f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	md := notes.ExportMarkdown(exported)

	parsed, err := notes.ParseMarkdown(md)
	if err != nil {
		t.Fatalf("re-parsing the export: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("ParseMarkdown = %+v, want exactly one node (the note must not become a child bullet)", parsed)
	}
	if parsed[0].Note != "- x" {
		t.Errorf("Note = %q, want the literal string %q", parsed[0].Note, "- x")
	}
}

// TestExportThenImportRoundTripsADeeplyBulletShapedNote pins the other
// failure mode the reviewer found: a note that is itself indented and
// bullet-shaped ("  - x") used to export one level deeper than its own
// bullet and make ParseMarkdown reject the WHOLE file with "bullet is
// indented deeper than its possible parent" — a 400 for the entire
// upload/paste, not just this one node. It must now import successfully.
func TestExportThenImportRoundTripsADeeplyBulletShapedNote(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parent := f.mk(t, notes.RootID, "parent")
	if err := f.store.SetText(ctx, f.alice.ID, parent.ID, "parent", "  - x"); err != nil {
		t.Fatal(err)
	}

	exported, err := f.store.Export(ctx, f.alice.ID, notes.RootID)
	if err != nil {
		t.Fatal(err)
	}
	md := notes.ExportMarkdown(exported)

	parsed, err := notes.ParseMarkdown(md)
	if err != nil {
		t.Fatalf("re-parsing the export: %v (this used to reject the whole file)", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("ParseMarkdown = %+v, want exactly one node", parsed)
	}
	if parsed[0].Note != "  - x" {
		t.Errorf("Note = %q, want the literal string %q", parsed[0].Note, "  - x")
	}
}
