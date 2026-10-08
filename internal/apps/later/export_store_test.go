package later_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
)

// exported mirrors the JSON shape onsuite export writes for ON Later.
type exported struct {
	Articles []struct {
		URL          string     `json:"url"`
		Title        string     `json:"title"`
		Lang         string     `json:"lang"`
		State        string     `json:"state"`
		Content      string     `json:"content"`
		ExtractError string     `json:"extract_error"`
		ContentHTML  string     `json:"content_html"`
		Note         string     `json:"note"`
		Tags         []string   `json:"tags"`
		SavedAt      time.Time  `json:"saved_at"`
		OpenedAt     *time.Time `json:"opened_at"`
		ArchivedAt   *time.Time `json:"archived_at"`
		Highlights   []struct {
			Quote   string `json:"quote"`
			Comment string `json:"comment"`
			Start   int    `json:"start"`
			End     int    `json:"end"`
		} `json:"highlights"`
	} `json:"articles"`
	Tags []string `json:"tags"`
}

func exportFor(t *testing.T, f *fixture, userID int64) (exported, string) {
	t.Helper()
	payload, err := later.New().Export(context.Background(), f.db, userID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("export payload does not marshal: %v", err)
	}
	var out exported
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out, string(raw)
}

func TestExportCarriesArticlesHighlightsNotesAndTags(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	chart, _, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{
		URL: "https://a.example/chart", Title: "Chart", Lang: "en-GB",
		ContentHTML: `<p>See the chart.</p><p><img src="/later/img/h1" alt="Chart"></p>`,
		Images:      map[string]string{"h1": "https://a.example/chart.png?w=1&h=2"},
		Tags:        []string{"data", "work"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddHighlight(ctx, chart.ID, chart.ContentText, 4, 13, "the chart", "Look again."); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetNote(ctx, f.alice.ID, chart.ID, "A note."); err != nil {
		t.Fatal(err)
	}
	f.tick()
	if err := f.store.MarkOpened(ctx, f.alice.ID, chart.ID); err != nil {
		t.Fatal(err)
	}
	f.tick()
	if _, _, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{
		URL: "https://a.example/paywall", Title: "Paywalled", ExtractError: "Couldn't find an article on the page.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.store.Save(ctx, f.bob.ID, later.NewArticle{URL: "https://b.example/1", Title: "Bob's", ContentHTML: "<p>x</p>"}); err != nil {
		t.Fatal(err)
	}

	out, raw := exportFor(t, f, f.alice.ID)
	if len(out.Articles) != 2 {
		t.Fatalf("got %d articles, want Alice's 2:\n%s", len(out.Articles), raw)
	}
	if strings.Contains(raw, "Bob's") {
		t.Error("Alice's export contains Bob's article")
	}

	a := out.Articles[0] // oldest first
	if a.URL != "https://a.example/chart" || a.Title != "Chart" || a.State != "reading" || a.Content != "extracted" {
		t.Errorf("first article = %+v", a)
	}
	if a.Lang != "en-GB" {
		t.Errorf("lang = %q, want en-GB", a.Lang)
	}
	if a.Note != "A note." {
		t.Errorf("note = %q", a.Note)
	}
	if strings.Join(a.Tags, ",") != "data,work" {
		t.Errorf("tags = %v", a.Tags)
	}
	if !strings.Contains(a.ContentHTML, `src="https://a.example/chart.png?w=1&amp;h=2"`) || strings.Contains(a.ContentHTML, "/later/img/") {
		t.Errorf("content_html images not pointed back at their sources: %s", a.ContentHTML)
	}
	if len(a.Highlights) != 1 || a.Highlights[0].Quote != "the chart" || a.Highlights[0].Comment != "Look again." ||
		a.Highlights[0].Start != 4 || a.Highlights[0].End != 13 {
		t.Errorf("highlights = %+v", a.Highlights)
	}
	if !a.SavedAt.Equal(time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)) || a.OpenedAt == nil || a.ArchivedAt != nil {
		t.Errorf("times: saved %v opened %v archived %v", a.SavedAt, a.OpenedAt, a.ArchivedAt)
	}

	b := out.Articles[1]
	if b.Content != "link_only" || b.ExtractError == "" || b.ContentHTML != "" || b.Lang != "" {
		t.Errorf("link-only article = %+v", b)
	}
	if b.Tags == nil || b.Highlights == nil {
		t.Error("an article without tags or highlights exports null instead of []")
	}
	if strings.Join(out.Tags, ",") != "data,work" {
		t.Errorf("top-level tags = %v", out.Tags)
	}
}

func TestExportOfAnEmptyAccount(t *testing.T) {
	f := newFixture(t)
	_, raw := exportFor(t, f, f.alice.ID)
	if raw != `{"articles":[],"tags":[]}` {
		t.Errorf("empty export = %s", raw)
	}
}
