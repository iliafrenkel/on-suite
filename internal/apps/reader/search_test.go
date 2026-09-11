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

// A poll re-upserts every item still in the feed XML, which includes exactly
// the recent articles that just got a full-article extraction. SaveItems must
// not blindly overwrite search_text with the feed body on that re-save — it
// would silently drop the extracted full-article text out of the index on
// every subsequent poll, and ReindexBatch would never repair it since the
// row's search_text is non-empty (it only fills in empty ones).
func TestSaveItemsDoesNotRevertAFullArticlesIndexedText(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Captured after Subscribe: ItemsForScope only returns items fetched at or
	// after the subscription's own added_at, so an item timestamped before it
	// (as it would be if "now" were captured first) is invisible.
	now := time.Now().UTC()
	feedItem := reader.ParsedItem{
		GUID:        "a",
		Title:       "Formula One",
		SummaryHTML: "<p>A short teaser.</p>",
		PublishedAt: now.Add(-2 * time.Hour),
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{feedItem}, now); err != nil {
		t.Fatal(err)
	}

	items, err := f.store.ItemsForSubscription(ctx, f.alice.ID, sub.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	itemID := items[0].ID

	if err := f.store.SaveFullArticle(ctx, f.alice.ID, itemID, reader.Extracted{
		HTML: "<p>A deep dive into aerodynamics and downforce.</p>",
	}, now); err != nil {
		t.Fatalf("SaveFullArticle: %v", err)
	}

	// The next poll re-saves the same feed item — same title, same summary,
	// no change at all — exactly as a real poll would.
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{feedItem}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeAll, 0, reader.FilterAll, "aerodynamics", 50)
	if err != nil {
		t.Fatalf("ItemsForScope: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("full-article word not found after a re-poll: got %d matches, want 1", len(got))
	}
	if got[0].ID != itemID {
		t.Errorf("matched item %d, want %d", got[0].ID, itemID)
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

// Rows that predate migration 0006 have no search_text. A migration cannot fix
// that — stripping HTML is not something SQL can do — so the job does.
func TestReindexBatchIndexesUnindexedRows(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "Ordinary", ContentHTML: "<p>Something about telemetry.</p>",
			PublishedAt: now.Add(-time.Hour)},
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	// Simulate a pre-0006 row: clear the indexed text behind the store's back.
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE reader_items SET search_text = ''`); err != nil {
		t.Fatal(err)
	}
	if got, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeAll, 0, reader.FilterAll, "telemetry", 50); err != nil || len(got) != 0 {
		t.Fatalf("precondition: unindexed row was found (%d items, err %v)", len(got), err)
	}

	n, err := f.store.ReindexBatch(ctx, 100)
	if err != nil {
		t.Fatalf("ReindexBatch: %v", err)
	}
	if n != 1 {
		t.Errorf("reindexed %d rows, want 1", n)
	}

	got, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeAll, 0, reader.FilterAll, "telemetry", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("row is still unsearchable after reindexing")
	}
}

// Once everything is indexed the pass must cost nothing, because it runs
// nightly forever.
func TestReindexBatchIsANoOpWhenNothingNeedsIt(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	n, err := f.store.ReindexBatch(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("reindexed %d rows in an empty database, want 0", n)
	}
}

// An article that genuinely has no text must not be picked up every night
// forever.
func TestReindexBatchDoesNotLoopOnAnEmptyArticle(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "", ContentHTML: "", SummaryHTML: "", PublishedAt: now.Add(-time.Hour)},
	}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE reader_items SET search_text = ''`); err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.ReindexBatch(ctx, 100); err != nil {
		t.Fatal(err)
	}
	n, err := f.store.ReindexBatch(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("second pass reindexed %d rows; an article with no text would be reindexed forever", n)
	}
}
