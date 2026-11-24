package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// toolingApp is a fake app that contributes the tools it is given.
type toolingApp struct {
	fakeApp
	tools []app.Tool
}

func (a toolingApp) Tools(app.Deps) []app.Tool { return a.tools }

func tool(name, scope string) app.Tool {
	return app.Tool{
		Name:     name,
		Scope:    scope,
		ReadOnly: strings.HasSuffix(scope, ":read"),
		Call: func(context.Context, int64, json.RawMessage) (string, error) {
			return "", nil
		},
	}
}

func flipReadOnly(t app.Tool) app.Tool {
	t.ReadOnly = !t.ReadOnly
	return t
}

func TestRegistryToolsValidates(t *testing.T) {
	tests := []struct {
		name  string
		tools []app.Tool
	}{
		{"name without app prefix", []app.Tool{tool("list", "books:read")}},
		{"name with another app's prefix", []app.Tool{tool("notes_list", "books:read")}},
		{"scope for another app", []app.Tool{tool("books_list", "notes:read")}},
		{"scope not read or write", []app.Tool{tool("books_list", "books:admin")}},
		{"duplicate name", []app.Tool{tool("books_list", "books:read"), tool("books_list", "books:read")}},
		{"read tool not ReadOnly", []app.Tool{flipReadOnly(tool("books_list", "books:read"))}},
		{"write tool ReadOnly", []app.Tool{flipReadOnly(tool("books_add", "books:write"))}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := toolingApp{fakeApp: newFake("books", "ON Books", 1), tools: tt.tools}
			reg, err := app.NewRegistry(a)
			if err != nil {
				t.Fatal(err)
			}
			_, err = reg.Tools(app.Deps{})
			if err == nil {
				t.Fatal("Tools: want an error, got nil")
			}
			if !strings.Contains(err.Error(), "books") {
				t.Errorf("error %q does not name the app", err)
			}
		})
	}
}

func TestRegistryToolsCollectsInDisplayOrder(t *testing.T) {
	books := toolingApp{fakeApp: newFake("books", "ON Books", 2), tools: []app.Tool{tool("books_list", "books:read")}}
	notes := toolingApp{fakeApp: newFake("notes", "ON Notes", 1), tools: []app.Tool{tool("notes_add", "notes:write")}}
	silent := newFake("paste", "ON Paste", 3)
	reg, err := app.NewRegistry(books, notes, silent)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reg.Tools(app.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "notes_add" || got[1].Name != "books_list" {
		t.Errorf("Tools = %v, want notes_add then books_list", names(got))
	}
}

func TestToolErrorIsItsMessage(t *testing.T) {
	if got := (app.ToolError{Msg: "No book with that ID"}).Error(); got != "No book with that ID" {
		t.Errorf("Error() = %q", got)
	}
}

func names(ts []app.Tool) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name
	}
	return out
}

func TestParseToolArgsEmptyIsNoop(t *testing.T) {
	for _, args := range []json.RawMessage{nil, {}} {
		target := struct{ N int }{N: 7}
		if err := app.ParseToolArgs(args, &target); err != nil {
			t.Errorf("ParseToolArgs(%q) = %v, want nil", args, err)
		}
		if target.N != 7 {
			t.Errorf("ParseToolArgs(%q) changed the target to %d", args, target.N)
		}
	}
}

func TestParseToolArgsBadJSONIsFixedMessage(t *testing.T) {
	for _, args := range []string{`not json`, `{"n":"x"}`} {
		var target struct{ N int }
		err := app.ParseToolArgs(json.RawMessage(args), &target)
		var te app.ToolError
		if !errors.As(err, &te) {
			t.Fatalf("ParseToolArgs(%s) = %v, want a ToolError", args, err)
		}
		if te.Msg != "The arguments are not valid." {
			t.Errorf("ParseToolArgs(%s) Msg = %q", args, te.Msg)
		}
	}
}

func TestMarshalToolCompact(t *testing.T) {
	got, err := app.MarshalTool(struct {
		A int `json:"a"`
	}{1})
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"a":1}` {
		t.Errorf("MarshalTool = %q, want {\"a\":1}", got)
	}
}

func TestCapToolText(t *testing.T) {
	atLimit := strings.Repeat("é", app.MaxToolText)
	if got := app.CapToolText(atLimit); got != atLimit {
		t.Errorf("CapToolText at the limit changed the text (%d runes)", utf8.RuneCountInString(got))
	}

	const note = "\n\n[Cut at 200,000 characters. Ask for a smaller part.]"
	over := strings.Repeat("é", app.MaxToolText+1)
	got := app.CapToolText(over)
	if want := strings.Repeat("é", app.MaxToolText) + note; got != want {
		t.Errorf("CapToolText over the limit: got %d runes, want %d runes of é then the note",
			utf8.RuneCountInString(got), app.MaxToolText)
	}
	if n := utf8.RuneCountInString(got); n != app.MaxToolText+utf8.RuneCountInString(note) {
		t.Errorf("CapToolText rune count = %d", n)
	}
}
