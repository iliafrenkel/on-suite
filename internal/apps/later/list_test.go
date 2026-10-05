package later_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// seedTagged saves an extracted article for Alice with tags.
func seedTagged(t *testing.T, s *server, url, title string, tags ...string) later.Article {
	t.Helper()
	return seed(t, s, s.Alice.User.ID, later.NewArticle{URL: url, Title: title, ContentHTML: words(10), Tags: tags})
}

// texts is each node's text with its whitespace collapsed.
func texts(nodes []*html.Node) []string {
	var out []string
	for _, n := range nodes {
		out = append(out, strings.Join(strings.Fields(htmlassert.Text(n)), " "))
	}
	return out
}

func TestIndexShowsATagChipPerTag(t *testing.T) {
	s := newServer(t)
	seedTagged(t, s, "https://a.example/1", "Essay", "essays")
	seedTagged(t, s, "https://a.example/2", "News", "news")
	doc := s.Get(t, s.Alice, "/later/")

	chips := doc.QueryAll("a.later-tag-chip")
	if got := texts(chips); !slices.Equal(got, []string{"essays", "news"}) {
		t.Fatalf("chips = %q, want [essays news]", got)
	}
	if href, _ := htmlassert.Attr(chips[0], "href"); href != "/later/?tab=unread&tag=essays" {
		t.Errorf("chip href = %q", href)
	}
	if _, ok := htmlassert.Attr(chips[0], "aria-current"); ok {
		t.Error("a chip is marked current with no tag selected")
	}
}

func TestIndexWithoutTagsHasNoChips(t *testing.T) {
	s := newServer(t)
	seedStates(t, s)
	s.Get(t, s.Alice, "/later/").MustNotHave(".later-tags")
}

func TestTagChipNarrowsRowsAndCounts(t *testing.T) {
	s := newServer(t)
	seedTagged(t, s, "https://a.example/1", "Essay", "essays")
	seedTagged(t, s, "https://a.example/2", "News", "news")
	seedTagged(t, s, "https://a.example/3", "Plain")
	doc := s.Get(t, s.Alice, "/later/?tab=unread&tag=Essays") // normalised to essays

	rows := doc.QueryAll(".later-row")
	if len(rows) != 1 || !strings.Contains(htmlassert.Text(rows[0]), "Essay") {
		t.Fatalf("rows = %q, want just Essay", texts(rows))
	}
	tabs := doc.QueryAll(".later-tab")
	if got := texts(tabs); !slices.Equal(got, []string{"Unread 1", "Reading 0", "Archived 0"}) {
		t.Errorf("tabs = %q, want counts narrowed to the tag", got)
	}
	if href, _ := htmlassert.Attr(tabs[2], "href"); href != "/later/?tab=archived&tag=essays" {
		t.Errorf("archived tab href = %q, want it to keep the tag", href)
	}
	for _, c := range doc.QueryAll("a.later-tag-chip") {
		href, _ := htmlassert.Attr(c, "href")
		_, current := htmlassert.Attr(c, "aria-current")
		switch htmlassert.Text(c) {
		case "essays":
			if !current || href != "/later/?tab=unread" {
				t.Errorf("selected chip: current=%v href=%q, want current and a link that clears it", current, href)
			}
		case "news":
			if current || href != "/later/?tab=unread&tag=news" {
				t.Errorf("other chip: current=%v href=%q", current, href)
			}
		}
	}
	doc.MustHave(`input[value="/later/?tab=unread&tag=essays"]`) // the row forms come back here
}

func TestAStaleTagStillHasAChipToClearIt(t *testing.T) {
	s := newServer(t)
	seedTagged(t, s, "https://a.example/1", "Essay", "essays")
	doc := s.Get(t, s.Alice, "/later/?tag=gone")
	var found bool
	for _, c := range doc.QueryAll("a.later-tag-chip") {
		if htmlassert.Text(c) == "gone" {
			found = true
			if href, _ := htmlassert.Attr(c, "href"); href != "/later/?tab=unread" {
				t.Errorf("stale chip href = %q", href)
			}
		}
	}
	if !found {
		t.Error("no chip for the stale tag")
	}
	if got := htmlassert.Text(doc.MustHave(".later-empty")); got != "Nothing tagged “gone” here." {
		t.Errorf("empty text = %q", got)
	}
}

func TestRowShowsTagPills(t *testing.T) {
	s := newServer(t)
	seedTagged(t, s, "https://a.example/1", "Essay", "work", "essays")
	doc := s.Get(t, s.Alice, "/later/")
	if got := texts(doc.QueryAll(".later-pill-tag")); !slices.Equal(got, []string{"essays", "work"}) {
		t.Errorf("tag pills = %q, want [essays work]", got)
	}
}

func TestLoadMoreKeepsTheTag(t *testing.T) {
	s := newServer(t)
	for i := 0; i < 51; i++ {
		seedTagged(t, s, fmt.Sprintf("https://p.example/%d", i), fmt.Sprintf("Item %d", i), "bulk")
	}
	doc := s.Get(t, s.Alice, "/later/?tab=unread&tag=bulk")
	if got, _ := htmlassert.Attr(doc.MustHave(".later-more button"), "hx-get"); got != "/later/?tab=unread&tag=bulk&offset=50" {
		t.Errorf("hx-get = %q", got)
	}
}

func storedTags(t *testing.T, s *server, a later.Article) []string {
	t.Helper()
	got, err := s.Store.ArticleTags(context.Background(), s.Alice.User.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSetTagsFromTheRowMenu(t *testing.T) {
	s := newServer(t)
	a := seedTagged(t, s, "https://a.example/1", "Essay", "old")
	doc := s.Get(t, s.Alice, "/later/")
	path := articlePath(a, "/tags")
	doc.MustHave(".later-row-menu form[action=" + path + "]")
	if got := attr(t, doc, "input#later-tags-"+fmt.Sprint(a.ID), "value"); got != "old" {
		t.Errorf("row tags input = %q, want old", got)
	}

	s.Submit(t, s.Alice, path, url.Values{"tags": {"Essays, AI"}, "back": {"/later/?tab=unread"}}, "/later/?tab=unread")
	if got := storedTags(t, s, a); !slices.Equal(got, []string{"ai", "essays"}) {
		t.Errorf("tags = %q, want [ai essays]", got)
	}
	if got := attr(t, s.Get(t, s.Alice, "/later/"), "input#later-tags-"+fmt.Sprint(a.ID), "value"); got != "ai, essays" {
		t.Errorf("row tags input after saving = %q", got)
	}
}

func TestSetTagsGoesBackToTheArticleByDefault(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	s.Submit(t, s.Alice, articlePath(a, "/tags"), url.Values{"tags": {"x"}}, articlePath(a, ""))
	s.Submit(t, s.Alice, articlePath(a, "/tags"), url.Values{"tags": {"x"}, "back": {"https://evil.example/"}}, articlePath(a, ""))
}

func TestSetTagsOnSomeoneElsesArticleIs404(t *testing.T) {
	s := newServer(t)
	a := seedTagged(t, s, "https://a.example/1", "Essay", "mine")
	rec := s.Post(t, s.Bob, articlePath(a, "/tags"), url.Values{"tags": {"theirs"}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if got := storedTags(t, s, a); !slices.Equal(got, []string{"mine"}) {
		t.Errorf("tags = %q, want them untouched", got)
	}
}

func TestReadingViewShowsAndEditsTags(t *testing.T) {
	s := newServer(t)
	a := seedTagged(t, s, "https://a.example/1", "Essay", "work", "essays")
	doc := s.Get(t, s.Alice, articlePath(a, "")) // opening moves it to reading

	links := doc.QueryAll(".later-article-tags a")
	if got := texts(links); !slices.Equal(got, []string{"essays", "work"}) {
		t.Fatalf("tag links = %q", got)
	}
	if href, _ := htmlassert.Attr(links[0], "href"); href != "/later/?tab=reading&tag=essays" {
		t.Errorf("tag link href = %q", href)
	}
	path := articlePath(a, "/tags")
	doc.MustHave(".later-topbar form[action=" + path + "]")
	if got := attr(t, doc, "input#later-tags-input", "value"); got != "essays, work" {
		t.Errorf("tags input = %q", got)
	}
	doc.MustHave(`.later-tags-form input[value="` + articlePath(a, "") + `"]`) // back to the article
}

func TestReadingViewWithoutTagsHasNoTagLine(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	s.Get(t, s.Alice, articlePath(a, "")).MustNotHave(".later-article-tags")
}

func TestSaveBoxTagsTheNewArticle(t *testing.T) {
	s, app := newSaveServer(t)
	app.AllowPrivateFetchesForTest()
	origin := pageOrigin(t, "text/html", articlePage)
	rec := s.Post(t, s.Alice, "/later/save", url.Values{"url": {origin.URL + "/essay"}, "tags": {"Long reads, #ai"}})
	id := idFrom(t, rec)
	got, err := s.Store.ArticleTags(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"ai", "long reads"}) {
		t.Errorf("tags = %q, want [ai long reads]", got)
	}
}

func TestSaveBoxKeepsTagsOnABadURL(t *testing.T) {
	s := newServer(t)
	rec := s.Post(t, s.Alice, "/later/save", url.Values{"url": {"not a url"}, "tags": {"keep me"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := attr(t, doc, "input#later-save-tags", "value"); got != "keep me" {
		t.Errorf("tags input = %q, want it kept", got)
	}
}

func TestPopupHasATagsField(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/later/save?url=https://example.com/a")
	doc.MustHave(".later-popup input#later-popup-tags")
	if got := attr(t, doc, "input#later-popup-tags", "name"); got != "tags" {
		t.Errorf("name = %q", got)
	}
}
