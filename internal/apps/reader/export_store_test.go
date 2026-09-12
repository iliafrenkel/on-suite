package reader_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

func TestExportCarriesFoldersSubscriptionsAndStarred(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	folder, err := f.store.CreateFolder(ctx, f.alice.ID, "Tech")
	if err != nil {
		t.Fatal(err)
	}
	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://a.example/feed.xml", &folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Captured after Subscribe: ItemsForScope only returns items fetched at or
	// after the subscription's own added_at (see search_test.go), so "now"
	// must postdate Subscribe's internally-stamped added_at.
	now := time.Now().UTC()
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "Saved", URL: "https://a.example/1", PublishedAt: now.Add(-time.Hour)},
		{GUID: "b", Title: "Not saved", PublishedAt: now.Add(-2 * time.Hour)},
	}, now); err != nil {
		t.Fatal(err)
	}
	// Not searched by "Saved": that word is also a token of "Not saved", so
	// FTS5's prefix match would return both. List everything and pick the
	// one to star by its exact title instead.
	items, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeAll, 0, reader.FilterAll, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	var starID int64
	for _, it := range items {
		if it.Title == "Saved" {
			starID = it.ID
		}
	}
	if starID == 0 {
		t.Fatalf("setup: did not find the 'Saved' item among %d items", len(items))
	}
	if err := f.store.SetStarred(ctx, f.alice.ID, starID, true, now); err != nil {
		t.Fatal(err)
	}

	payload, err := reader.New().Export(ctx, f.store.DB(), f.alice.ID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	// The payload must be JSON-encodable: onsuite export marshals it.
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("export payload does not marshal: %v", err)
	}
	text := string(encoded)
	for _, want := range []string{"Tech", "a.example/feed.xml", "Saved"} {
		if !strings.Contains(text, want) {
			t.Errorf("export is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Not saved") {
		t.Error("export includes an unstarred article; only saved ones belong in a backup")
	}
}

func TestExportIsScopedToOneUser(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	if _, err := f.store.Subscribe(ctx, f.bob.ID, "https://bobs.example/feed.xml", nil); err != nil {
		t.Fatal(err)
	}
	payload, err := reader.New().Export(ctx, f.store.DB(), f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "bobs.example") {
		t.Errorf("alice's export contains bob's feed:\n%s", encoded)
	}
}
