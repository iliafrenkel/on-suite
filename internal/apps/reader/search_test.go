package reader_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

func TestSearchTextStripsMarkup(t *testing.T) {
	got := reader.SearchText(`<p>Hello <strong>world</strong>, a <a href="https://example.com/x">link</a>.</p>`)

	for _, want := range []string{"Hello", "world", "link"} {
		if !strings.Contains(got, want) {
			t.Errorf("text %q lost %q", got, want)
		}
	}
	for _, bad := range []string{"<p>", "strong", "href", "example.com"} {
		if strings.Contains(got, bad) {
			t.Errorf("text %q still contains markup %q — searching for it would match every article", got, bad)
		}
	}
	// Words must not run together when a tag was the only thing between them.
	if strings.Contains(got, "Helloworld") {
		t.Errorf("tag boundaries did not become word boundaries: %q", got)
	}
}

func TestSearchTextHandlesEntitiesAndEmpty(t *testing.T) {
	if got := reader.SearchText(`<p>Caf&eacute; &amp; cr&egrave;me</p>`); !strings.Contains(got, "Café") {
		t.Errorf("entities not decoded: %q", got)
	}
	if got := reader.SearchText(""); got != "" {
		t.Errorf("SearchText(\"\") = %q, want empty", got)
	}
}

func TestSearchFindsAnArticleByItsBody(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "Formula One", ContentHTML: "<p>A piece about aerodynamics.</p>",
			PublishedAt: now.Add(-2 * time.Hour)},
		{GUID: "b", Title: "Table Tennis", ContentHTML: "<p>A piece about rubber and spin.</p>",
			PublishedAt: now.Add(-time.Hour)},
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeAll, 0, reader.FilterAll, "aerodynamics", 50)
	if err != nil {
		t.Fatalf("ItemsForScope: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("searching a body word returned %d items, want 1", len(got))
	}
	if got[0].Title != "Formula One" {
		t.Errorf("matched %q", got[0].Title)
	}
}

func TestSearchMatchesAPrefixWhileTyping(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "Aerodynamics explained", PublishedAt: now.Add(-time.Hour)},
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeAll, 0, reader.FilterAll, "aero", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("a half-typed word matched %d items, want 1 — prefix indexing is what makes a live filter usable", len(got))
	}
}

// Search composes with the scope and filter rather than replacing them.
func TestSearchComposesWithFilter(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "Racing one", PublishedAt: now.Add(-2 * time.Hour)},
		{GUID: "b", Title: "Racing two", PublishedAt: now.Add(-time.Hour)},
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	all, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeAll, 0, reader.FilterAll, "racing", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("search found %d, want 2", len(all))
	}
	if err := f.store.SetRead(ctx, f.alice.ID, all[0].ID, true, now); err != nil {
		t.Fatal(err)
	}

	unread, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeAll, 0, reader.FilterUnread, "racing", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(unread) != 1 {
		t.Errorf("search + unread filter returned %d, want 1", len(unread))
	}
}

// A query is user input going into a MATCH expression. It must never be able
// to produce a syntax error, let alone anything else.
func TestSearchSurvivesHostileQueries(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "Ordinary article", PublishedAt: now.Add(-time.Hour)},
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{
		`"`, `""`, `AND`, `OR`, `NOT`, `*`, `(`, `a OR b)`, `NEAR(a b`,
		`^`, `-`, `:`, `col:value`, `"unbalanced`, strings.Repeat("a", 500),
	} {
		if _, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeAll, 0, reader.FilterAll, q, 50); err != nil {
			t.Errorf("query %q produced an error: %v", q, err)
		}
	}
}

// An empty query is "show everything", not "match nothing" — FTS5 treats an
// empty MATCH as a syntax error, so this must not reach it at all.
func TestEmptySearchReturnsEverything(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "One", PublishedAt: now.Add(-time.Hour)},
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{"", "   ", "\t"} {
		got, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeAll, 0, reader.FilterAll, q, 50)
		if err != nil {
			t.Fatalf("empty query %q: %v", q, err)
		}
		if len(got) != 1 {
			t.Errorf("empty query %q returned %d items, want everything (1)", q, len(got))
		}
	}
}
