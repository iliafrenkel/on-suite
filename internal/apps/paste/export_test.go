package paste_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/paste"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

func TestExportImplementsTheInterface(t *testing.T) {
	// A compile-time assertion would be enough, but a failing test names the
	// problem more clearly than a type error in an unrelated file.
	var a any = paste.New()
	if _, ok := a.(app.Exporter); !ok {
		t.Fatal("*paste.App does not implement app.Exporter")
	}
}

func TestExportContainsEverythingExceptTheSlug(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	one, err := f.store.Create(ctx, f.alice.ID, "first", "go", "package one\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Create(ctx, f.alice.ID, "second", "yaml", "key: value\n"); err != nil {
		t.Fatal(err)
	}
	slug, err := f.store.Share(ctx, f.alice.ID, one.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Create(ctx, f.bob.ID, "bob's", "go", "package bob\n"); err != nil {
		t.Fatal(err)
	}

	payload, err := paste.New().Export(ctx, f.db, f.alice.ID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("the payload does not marshal: %v", err)
	}
	text := string(encoded)

	for _, want := range []string{"package one", "key: value", "first", "second"} {
		if !strings.Contains(text, want) {
			t.Errorf("the export is missing %q", want)
		}
	}
	if strings.Contains(text, "package bob") {
		t.Error("the export contains another user's snippet")
	}
	// The slug is a credential and must not be written to a portable file.
	if strings.Contains(text, slug) {
		t.Error("the export leaked a share slug")
	}
	if !strings.Contains(text, `"shared":true`) {
		t.Error("the export does not record that a snippet was shared")
	}
}

func TestExportOfAUserWithNothingIsAnEmptyList(t *testing.T) {
	f := newFixture(t)

	payload, err := paste.New().Export(context.Background(), f.db, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	// A null would be awkward for a consumer; an empty array is not.
	if got := string(encoded); !strings.Contains(got, `"snippets":[]`) {
		t.Errorf("export = %s, want an empty snippets array", got)
	}
}
