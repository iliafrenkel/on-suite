package later_test

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// saveBody stores an extracted article whose text is body.
func (f *fixture) saveBody(t *testing.T, userID int64, url, title, body string, tags ...string) later.Article {
	t.Helper()
	a, _, err := f.store.Save(context.Background(), userID, later.NewArticle{
		URL: url, Title: title, ContentHTML: "<p>" + body + "</p>", Tags: tags,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (f *fixture) search(t *testing.T, query, tag string) []later.SearchHit {
	t.Helper()
	hits, err := f.store.Search(context.Background(), f.alice.ID, query, tag, 0, 50)
	if err != nil {
		t.Fatalf("Search(%q): %v", query, err)
	}
	return hits
}

func hitIDs(hits []later.SearchHit) []int64 {
	var out []int64
	for _, h := range hits {
		out = append(out, h.ID)
	}
	return out
}

func TestSearchSaysWhereItMatched(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.saveBody(t, f.alice.ID, "https://a.example/1", "Walrus", "zebra crossing at night")
	if _, err := f.store.AddHighlight(ctx, a.ID, a.ContentText, 0, 5, "zebra", "giraffe thoughts"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetNote(ctx, f.alice.ID, a.ID, "remember the okapi"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query, hit string
		in         later.MatchIn
	}{
		{"walrus", "", later.MatchTitle},
		{"night", "night", later.MatchText},
		{"giraffe", "giraffe", later.MatchHighlight},
		{"zebra", "zebra", later.MatchHighlight}, // the quote beats the text
		{"okapi", "okapi", later.MatchNote},
	} {
		hits := f.search(t, tc.query, "")
		if len(hits) != 1 || hits[0].ID != a.ID {
			t.Fatalf("Search(%q) = %v, want the article", tc.query, hitIDs(hits))
		}
		if hits[0].In != tc.in {
			t.Errorf("Search(%q).In = %q, want %q", tc.query, hits[0].In, tc.in)
		}
		want := later.SnippetOpen + tc.hit + later.SnippetClose
		if tc.hit != "" && !strings.Contains(hits[0].Snippet, want) {
			t.Errorf("Search(%q).Snippet = %q, want it to mark %q", tc.query, hits[0].Snippet, tc.hit)
		}
		if tc.hit == "" && hits[0].Snippet != "" {
			t.Errorf("title-only Snippet = %q, want none", hits[0].Snippet)
		}
	}
}

func TestSearchFindsEveryStateButOnlyYours(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.saveBody(t, f.alice.ID, "https://a.example/1", "One", "kumquat")
	b := f.saveBody(t, f.alice.ID, "https://a.example/2", "Two", "kumquat")
	f.saveBody(t, f.bob.ID, "https://a.example/3", "Bob's", "kumquat")
	if err := f.store.SetState(ctx, f.alice.ID, b.ID, later.StateArchived); err != nil {
		t.Fatal(err)
	}
	hits := f.search(t, "kumquat", "")
	if len(hits) != 2 {
		t.Fatalf("hits = %v, want alice's two", hitIDs(hits))
	}
	states := map[int64]later.State{}
	for _, h := range hits {
		states[h.ID] = h.State
	}
	if states[a.ID] != later.StateUnread || states[b.ID] != later.StateArchived {
		t.Errorf("states = %v", states)
	}
}

// Every article below has a title and a body of the same length, so only
// the column weights (title 10, text 1, highlights 4, note 4) can order
// them: a title match first, then highlight and note matches, then text.
func TestSearchRanksByWhereItMatched(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	const plain = "aaaa bbbb cccc dddd"
	text := f.saveBody(t, f.alice.ID, "https://a.example/1", "Plain notes", "otter bbbb cccc dddd")
	title := f.saveBody(t, f.alice.ID, "https://a.example/2", "Otter notes", plain)
	hl := f.saveBody(t, f.alice.ID, "https://a.example/3", "Plain notes", plain)
	if _, err := f.store.AddHighlight(ctx, hl.ID, hl.ContentText, 0, 4, "aaaa", "otter"); err != nil {
		t.Fatal(err)
	}
	note := f.saveBody(t, f.alice.ID, "https://a.example/4", "Plain notes", plain)
	if err := f.store.SetNote(ctx, f.alice.ID, note.ID, "otter"); err != nil {
		t.Fatal(err)
	}

	got := hitIDs(f.search(t, "otter", ""))
	if len(got) != 4 {
		t.Fatalf("hits = %v, want 4", got)
	}
	if got[0] != title.ID {
		t.Errorf("first = %d, want the title match %d", got[0], title.ID)
	}
	if mid := []int64{got[1], got[2]}; !slices.Contains(mid, hl.ID) || !slices.Contains(mid, note.ID) {
		t.Errorf("middle = %v, want the highlight %d and note %d matches", mid, hl.ID, note.ID)
	}
	if got[3] != text.ID {
		t.Errorf("last = %d, want the text match %d", got[3], text.ID)
	}
}

func TestSearchMatchesPrefixesAndFoldsCase(t *testing.T) {
	f := newFixture(t)
	a := f.saveBody(t, f.alice.ID, "https://a.example/1", "Words", "Categorically Привет мир")
	for _, q := range []string{"categ", "CATEGORICALLY", "привет", "ПРИВ"} {
		if got := hitIDs(f.search(t, q, "")); len(got) != 1 || got[0] != a.ID {
			t.Errorf("Search(%q) = %v, want the article", q, got)
		}
	}
}

func TestSearchQueriesAreInert(t *testing.T) {
	f := newFixture(t)
	f.saveBody(t, f.alice.ID, "https://a.example/1", "Words", "plain text")
	for _, q := range []string{`AND`, `"`, `(`, `title:plain`, `NEAR(a b)`, `*`, `-plain`} {
		if _, err := f.store.Search(context.Background(), f.alice.ID, q, "", 0, 10); err != nil {
			t.Errorf("Search(%q) = %v, want no error", q, err)
		}
	}
	for _, q := range []string{"", "   "} {
		hits, err := f.store.Search(context.Background(), f.alice.ID, q, "", 0, 10)
		if err != nil || len(hits) != 0 {
			t.Errorf("Search(%q) = %v, %v; want nothing", q, hitIDs(hits), err)
		}
	}
}

func TestSearchNarrowsByTag(t *testing.T) {
	f := newFixture(t)
	a := f.saveBody(t, f.alice.ID, "https://a.example/1", "One", "pomelo", "fruit")
	f.saveBody(t, f.alice.ID, "https://a.example/2", "Two", "pomelo")
	if got := hitIDs(f.search(t, "pomelo", "fruit")); len(got) != 1 || got[0] != a.ID {
		t.Errorf("Search(tag fruit) = %v, want [%d]", got, a.ID)
	}
}

func TestSearchStaysInStepWithEdits(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.saveBody(t, f.alice.ID, "https://a.example/1", "One", "alpha beta")

	if err := f.store.SetNote(ctx, f.alice.ID, a.ID, "quince"); err != nil {
		t.Fatal(err)
	}
	if len(f.search(t, "quince", "")) != 1 {
		t.Error("note not indexed")
	}
	if err := f.store.SetNote(ctx, f.alice.ID, a.ID, ""); err != nil {
		t.Fatal(err)
	}
	if len(f.search(t, "quince", "")) != 0 {
		t.Error("old note still indexed")
	}

	h, err := f.store.AddHighlight(ctx, a.ID, a.ContentText, 0, 5, "alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetHighlightComment(ctx, a.ID, h.ID, "loquat"); err != nil {
		t.Fatal(err)
	}
	if hits := f.search(t, "loquat", ""); len(hits) != 1 || hits[0].In != later.MatchHighlight {
		t.Error("comment not indexed")
	}
	if err := f.store.DeleteHighlight(ctx, a.ID, h.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.search(t, "loquat", "")) != 0 {
		t.Error("deleted highlight's comment still indexed")
	}

	link, _, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{URL: "https://a.example/2", Title: "Walled"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetPastedText(ctx, f.alice.ID, link.ID, "durian season"); err != nil {
		t.Fatal(err)
	}
	if got := hitIDs(f.search(t, "durian", "")); len(got) != 1 || got[0] != link.ID {
		t.Errorf("pasted text not indexed: %v", got)
	}

	if err := f.store.Delete(ctx, f.alice.ID, link.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.db.QueryRowContext(ctx, `SELECT count(*) FROM later_search WHERE rowid = ?`, link.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("deleted article still has %d search rows", n)
	}
}

func TestSearchPages(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 3; i++ {
		f.saveBody(t, f.alice.ID, "https://a.example/"+string(rune('a'+i)), "T", "lychee")
	}
	first, err := f.store.Search(context.Background(), f.alice.ID, "lychee", "", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	rest, err := f.store.Search(context.Background(), f.alice.ID, "lychee", "", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(rest) != 1 {
		t.Errorf("pages = %d + %d, want 2 + 1", len(first), len(rest))
	}
}

// TestSearchMigrationIndexesExistingArticles applies the migrations before
// 0006, saves an article with a highlight and a note, then applies 0006 and
// checks the backfill indexed all of it.
func TestSearchMigrationIndexesExistingArticles(t *testing.T) {
	ctx := context.Background()
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	authMs, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	laterMs, err := db.Collect(later.ID, later.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if len(laterMs) < 6 {
		t.Fatalf("got %d later migrations; this test needs 0006", len(laterMs))
	}
	if _, err := db.Apply(ctx, handle, append(append([]db.Migration{}, authMs...), laterMs[:5]...)); err != nil {
		t.Fatal(err)
	}
	u, err := auth.NewStore(handle).CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}
	st := later.NewStore(handle)
	a, _, err := st.Save(ctx, u.ID, later.NewArticle{URL: "https://a.example/1", Title: "Tapir", ContentHTML: "<p>zebra crossing</p>"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddHighlight(ctx, a.ID, a.ContentText, 0, 5, "zebra", "giraffe"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetNote(ctx, u.ID, a.ID, "okapi"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Apply(ctx, handle, append(authMs, laterMs...)); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"tapir", "crossing", "giraffe", "okapi"} {
		hits, err := st.Search(ctx, u.ID, q, "", 0, 10)
		if err != nil || len(hits) != 1 {
			t.Errorf("Search(%q) after migrating = %d hits, %v; want 1", q, len(hits), err)
		}
	}
}

func TestSearchPagesKeepOrderAndSnippets(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.saveBody(t, f.alice.ID, "https://a.example/1", "One", "a lychee in the first body")
	f.saveBody(t, f.alice.ID, "https://a.example/2", "Two", "lychee lychee lychee in the second")
	f.saveBody(t, f.alice.ID, "https://a.example/3", "Three", "the third body mentions a lychee too")
	all, err := f.store.Search(ctx, f.alice.ID, "lychee", "", 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d hits, want 3", len(all))
	}
	var paged []later.SearchHit
	for off := 0; off < 3; off++ {
		page, err := f.store.Search(ctx, f.alice.ID, "lychee", "", off, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) != 1 {
			t.Fatalf("offset %d: got %d hits, want 1", off, len(page))
		}
		h := page[0]
		if h.In != later.MatchText || !strings.Contains(h.Snippet, later.SnippetOpen+"lychee"+later.SnippetClose) {
			t.Errorf("offset %d: In=%q Snippet=%q", off, h.In, h.Snippet)
		}
		paged = append(paged, h)
	}
	if got, want := hitIDs(paged), hitIDs(all); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("paged IDs %v, single query %v", got, want)
	}
}
