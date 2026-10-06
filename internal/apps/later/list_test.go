package later_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestSearchShowsMatchesFromEveryState(t *testing.T) {
	s := newServer(t)
	seedStates(t, s) // Unread One, Unread Two, Reading One, Archived One
	doc := s.Get(t, s.Alice, "/later/?tab=unread&q=one")

	doc.MustNotHave(".later-tabs")
	doc.MustHave(".later-search-head")
	rows := doc.QueryAll(".later-row")
	if len(rows) != 3 {
		t.Fatalf("rows = %q, want the three titled One", texts(rows))
	}
	got := texts(doc.QueryAll(".later-pill-state"))
	slices.Sort(got)
	if !slices.Equal(got, []string{"Archived", "Reading", "Unread"}) {
		t.Errorf("state pills = %q", got)
	}
	if href, _ := htmlassert.Attr(doc.MustHave(".later-search-head a"), "href"); href != "/later/?tab=unread" {
		t.Errorf("clear link = %q, want back to the tab", href)
	}
}

func TestSearchRowsActOnTheirOwnState(t *testing.T) {
	s := newServer(t)
	seedStates(t, s)
	// Searching from Unread finds the archived article; its menu must offer
	// Move to unread, not Archive.
	doc := s.Get(t, s.Alice, "/later/?tab=unread&q=archived")
	var unarchive, archive int
	for _, f := range doc.QueryAll(".later-row-menu form") {
		action, _ := htmlassert.Attr(f, "action")
		switch {
		case strings.HasSuffix(action, "/unarchive"):
			unarchive++
		case strings.HasSuffix(action, "/archive"):
			archive++
		}
	}
	if unarchive != 1 || archive != 0 {
		t.Errorf("menu forms: %d unarchive, %d archive; want 1 and 0", unarchive, archive)
	}
	doc.MustHave(`input[value="/later/?tab=unread&q=archived"]`)
}

func TestSearchShowsWhereItMatched(t *testing.T) {
	s := newServer(t)
	a := seedHello(t, s)
	quote := string([]rune(a.ContentText)[0:5])
	if _, err := s.Store.AddHighlight(context.Background(), a.ID, a.ContentText, 0, 5, quote, "giraffe thoughts"); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/later/?q=giraffe")
	if got := htmlassert.Text(doc.MustHave(".later-snippet-in")); got != "In a highlight:" {
		t.Errorf("label = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".later-row-snippet mark")); got != "giraffe" {
		t.Errorf("marked = %q, want giraffe", got)
	}
}

func TestSearchHTMXAnswersTheListOnly(t *testing.T) {
	s := newServer(t)
	seedStates(t, s)
	req := httptest.NewRequest("GET", "/later/?tab=unread&q=one", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "later-list")
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("fragment is a whole page")
	}
	frag := htmlassert.Parse(t, rec.Body.String())
	frag.MustHave("div#later-list")
	frag.MustNotHave(".later-save")
	if n := len(frag.QueryAll(".later-row")); n != 3 {
		t.Errorf("fragment has %d rows, want 3", n)
	}
}

// listFragment fetches url as the search box does and parses the answer.
func listFragment(t *testing.T, s *server, url string) *htmlassert.Doc {
	t.Helper()
	req := httptest.NewRequest("GET", url, nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "later-list")
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	return htmlassert.Parse(t, rec.Body.String())
}

// The search box tells screen readers how many results it found on a
// status line outside the swapped list, so the rows aren't read out on
// every keystroke (#524).
func TestSearchAnnouncesTheResultCount(t *testing.T) {
	s := newServer(t)
	seedStates(t, s)

	doc := s.Get(t, s.Alice, "/later/")
	if got := attr(t, doc, "#later-search-status", "role"); got != "status" {
		t.Errorf("status line role = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave("#later-search-status")); got != "" {
		t.Errorf("status on page load = %q, want empty", got)
	}
	doc.MustNotHave("#later-list #later-search-status")

	for _, tt := range []struct{ url, want string }{
		{"/later/?q=one", "3 results"},
		{"/later/?q=archived", "1 result"},
		{"/later/?q=zzzz", "Nothing matches"},
		{"/later/", ""},
	} {
		frag := listFragment(t, s, tt.url)
		if got := attr(t, frag, "#later-search-status", "hx-swap-oob"); got != "innerHTML" {
			t.Errorf("%s: hx-swap-oob = %q", tt.url, got)
		}
		if got := htmlassert.Text(frag.MustHave("#later-search-status")); got != tt.want {
			t.Errorf("%s: status = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestSearchBoxKeepsTheQueryTabAndTag(t *testing.T) {
	s := newServer(t)
	seedTagged(t, s, "https://a.example/1", "Essay", "essays")
	doc := s.Get(t, s.Alice, "/later/?tab=archived&tag=essays&q=ess")
	if got := attr(t, doc, "input#later-q", "value"); got != "ess" {
		t.Errorf("search box = %q", got)
	}
	if got := attr(t, doc, "input#later-q", "hx-get"); got != "/later/" {
		t.Errorf("hx-get = %q", got)
	}
	if got := attr(t, doc, "input#later-q", "hx-target"); got != "#later-list" {
		t.Errorf("hx-target = %q", got)
	}
	if got := attr(t, doc, "input#later-q", "hx-trigger"); got != "input changed delay:300ms, search" {
		t.Errorf("hx-trigger = %q", got)
	}
	if got := attr(t, doc, "input#later-q", "hx-replace-url"); got != "true" {
		t.Errorf("hx-replace-url = %q", got)
	}
	doc.MustHave(`.later-search input[value="archived"]`)
	doc.MustHave(`.later-search input[value="essays"]`)
}

func TestSearchPageNarrowsByTag(t *testing.T) {
	s := newServer(t)
	seedTagged(t, s, "https://a.example/1", "Pomelo One", "fruit")
	seedTagged(t, s, "https://a.example/2", "Pomelo Two")
	doc := s.Get(t, s.Alice, "/later/?tab=unread&tag=fruit&q=pomelo")
	if rows := doc.QueryAll(".later-row"); len(rows) != 1 {
		t.Errorf("rows = %q, want just the tagged one", texts(rows))
	}
}

func TestBlankSearchShowsTheTab(t *testing.T) {
	s := newServer(t)
	seedStates(t, s)
	doc := s.Get(t, s.Alice, "/later/?tab=unread&q=++")
	doc.MustHave(".later-tabs")
	doc.MustNotHave(".later-search-head")
}

func TestSearchWithNoMatches(t *testing.T) {
	s := newServer(t)
	seedStates(t, s)
	doc := s.Get(t, s.Alice, "/later/?q=zzz")
	if got := htmlassert.Text(doc.MustHave(".later-empty")); got != "Nothing matches “zzz”." {
		t.Errorf("empty text = %q", got)
	}
}

func TestSearchLoadMoreKeepsTheQuery(t *testing.T) {
	s := newServer(t)
	for i := 0; i < 51; i++ {
		seedTagged(t, s, fmt.Sprintf("https://p.example/%d", i), fmt.Sprintf("Mango %d", i))
	}
	doc := s.Get(t, s.Alice, "/later/?q=mango")
	if got, _ := htmlassert.Attr(doc.MustHave(".later-more button"), "hx-get"); got != "/later/?tab=unread&q=mango&offset=50" {
		t.Errorf("hx-get = %q", got)
	}
}
