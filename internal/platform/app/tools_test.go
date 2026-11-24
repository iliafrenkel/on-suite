package app_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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
		Name:  name,
		Scope: scope,
		Call: func(context.Context, int64, json.RawMessage) (string, error) {
			return "", nil
		},
	}
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
