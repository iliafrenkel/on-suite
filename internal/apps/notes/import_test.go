package notes_test

import (
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
