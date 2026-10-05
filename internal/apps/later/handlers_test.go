package later_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

type server = apptest.Server[*later.Store]

// newServer mounts ON Later with its own store as the test's handle.
func newServer(t *testing.T) *server {
	t.Helper()
	return apptest.NewServer(t, later.New(), later.NewStore)
}

func TestLaterRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/later/", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /later/ anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

func TestIndexRendersForASignedInUser(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/later/")
	doc.MustHave(".later-page")
}

func seed(t *testing.T, s *server, userID int64, n later.NewArticle) later.Article {
	t.Helper()
	a, _, err := s.Store.Save(context.Background(), userID, n)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func words(n int) string {
	return "<p>" + strings.TrimSpace(strings.Repeat("word ", n)) + "</p>"
}

// seedStates saves 2 unread, 1 reading and 1 archived article for Alice.
func seedStates(t *testing.T, s *server) {
	t.Helper()
	ctx, uid := context.Background(), s.Alice.User.ID
	seed(t, s, uid, later.NewArticle{URL: "https://a.example/1", Title: "Unread One", ContentHTML: words(10)})
	seed(t, s, uid, later.NewArticle{URL: "https://a.example/2", Title: "Unread Two", ContentHTML: words(10)})
	r := seed(t, s, uid, later.NewArticle{URL: "https://a.example/3", Title: "Reading One", ContentHTML: words(10)})
	if err := s.Store.MarkOpened(ctx, uid, r.ID); err != nil {
		t.Fatal(err)
	}
	ar := seed(t, s, uid, later.NewArticle{URL: "https://a.example/4", Title: "Archived One", ContentHTML: words(10)})
	if err := s.Store.SetState(ctx, uid, ar.ID, later.StateArchived); err != nil {
		t.Fatal(err)
	}
}

func TestIndexShowsTabsWithCounts(t *testing.T) {
	s := newServer(t)
	seedStates(t, s)
	doc := s.Get(t, s.Alice, "/later/")
	tabs := doc.QueryAll(".later-tab")
	if len(tabs) != 3 {
		t.Fatalf("got %d tabs, want 3", len(tabs))
	}
	var got []string
	for _, tab := range tabs {
		got = append(got, strings.Join(strings.Fields(htmlassert.Text(tab)), " "))
	}
	want := []string{"Unread 2", "Reading 1", "Archived 1"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("tabs = %v, want %v", got, want)
	}
	if v, _ := htmlassert.Attr(tabs[0], "aria-current"); v != "page" {
		t.Errorf("Unread aria-current = %q, want page", v)
	}
	if _, ok := htmlassert.Attr(tabs[1], "aria-current"); ok {
		t.Error("Reading tab is marked current on the Unread page")
	}
}

func TestIndexListsTheTabsArticles(t *testing.T) {
	s := newServer(t)
	seedStates(t, s)
	doc := s.Get(t, s.Alice, "/later/?tab=archived")
	rows := doc.QueryAll(".later-row")
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if !strings.Contains(htmlassert.Text(rows[0]), "Archived One") {
		t.Errorf("row = %q", htmlassert.Text(rows[0]))
	}
	link := doc.MustHave(".later-row-link")
	href, _ := htmlassert.Attr(link, "href")
	if !strings.HasPrefix(href, "/later/a/") {
		t.Errorf("href = %q, want /later/a/{id}", href)
	}
	// An unknown tab falls back to Unread.
	doc = s.Get(t, s.Alice, "/later/?tab=bogus")
	if n := len(doc.QueryAll(".later-row")); n != 2 {
		t.Errorf("bogus tab shows %d rows, want the 2 unread ones", n)
	}
}

func TestIndexRowShowsSiteMinutesAndLinkOnlyPill(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	seed(t, s, uid, later.NewArticle{URL: "https://news.example/story", Title: "Long Read", ContentHTML: words(460)})
	seed(t, s, uid, later.NewArticle{URL: "https://link.example/x", Title: "Just A Link"})
	doc := s.Get(t, s.Alice, "/later/")
	text := doc.Text()
	for _, want := range []string{"news.example", "2 min", "link.example"} {
		if !strings.Contains(text, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	pills := doc.QueryAll(".later-pill-linkonly")
	if len(pills) != 1 || !strings.Contains(htmlassert.Text(pills[0]), "link only — add text") {
		t.Errorf("link-only pills = %d, want exactly 1 with the add-text label", len(pills))
	}
}

func TestIndexShowsTheEmptyText(t *testing.T) {
	s := newServer(t)
	for tab, want := range map[string]string{
		"unread":   "Nothing to read. Paste a URL above to save an article.",
		"reading":  "Nothing in progress.",
		"archived": "Nothing archived yet.",
	} {
		doc := s.Get(t, s.Alice, "/later/?tab="+tab)
		if got := strings.TrimSpace(htmlassert.Text(doc.MustHave(".later-empty"))); got != want {
			t.Errorf("%s empty text = %q, want %q", tab, got, want)
		}
	}
}

func TestIndexPagesWithLoadMore(t *testing.T) {
	s := newServer(t)
	uid := s.Alice.User.ID
	for i := 0; i < 51; i++ {
		seed(t, s, uid, later.NewArticle{URL: fmt.Sprintf("https://p.example/%d", i), Title: fmt.Sprintf("Item %d", i), ContentHTML: words(5)})
	}
	doc := s.Get(t, s.Alice, "/later/?tab=unread")
	if n := len(doc.QueryAll(".later-row")); n != 50 {
		t.Fatalf("first page has %d rows, want 50", n)
	}
	btn := doc.MustHave(".later-more button")
	if got, _ := htmlassert.Attr(btn, "hx-get"); got != "/later/?tab=unread&offset=50" {
		t.Fatalf("hx-get = %q", got)
	}

	req := httptest.NewRequest("GET", "/later/?tab=unread&offset=50", nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	frag := htmlassert.Parse(t, rec.Body.String())
	if n := len(frag.QueryAll(".later-row")); n != 1 {
		t.Errorf("fragment has %d rows, want 1", n)
	}
	frag.MustNotHave(".later-more")
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("fragment is a full page")
	}
}

func TestIndexNeverShowsAnotherUsersArticles(t *testing.T) {
	s := newServer(t)
	seed(t, s, s.Bob.User.ID, later.NewArticle{URL: "https://b.example/secret", Title: "Bobs Secret", ContentHTML: words(10)})
	doc := s.Get(t, s.Alice, "/later/")
	if strings.Contains(doc.Text(), "Bobs Secret") {
		t.Error("Alice sees Bob's article")
	}
	tabs := doc.QueryAll(".later-tab-count")
	for _, c := range tabs {
		if strings.TrimSpace(htmlassert.Text(c)) != "0" {
			t.Errorf("count = %q, want 0", htmlassert.Text(c))
		}
	}
}

func TestIndexHasTheSaveForm(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/later/")
	form := doc.MustHave(`form[action="/later/save"]`)
	if m, _ := htmlassert.Attr(form, "method"); !strings.EqualFold(m, "post") {
		t.Errorf("method = %q", m)
	}
	doc.MustHave(`form[action="/later/save"] input[name="url"]`)
	doc.MustHave(`form[action="/later/save"] input[type="hidden"]`)
}

// --- reading view -------------------------------------------------------

func seedReadable(t *testing.T, s *server) later.Article {
	t.Helper()
	return seed(t, s, s.Alice.User.ID, later.NewArticle{
		URL: "https://blog.example/essay", Title: "An Essay", SiteName: "Blog", Byline: "Jo",
		ContentHTML: "<p>Hello reader</p>",
	})
}

func seedLinkOnly(t *testing.T, s *server) later.Article {
	t.Helper()
	return seed(t, s, s.Alice.User.ID, later.NewArticle{
		URL: "https://paywall.example/a", Title: "Walled", ExtractError: "The page had no article text.",
	})
}

func articlePath(a later.Article, suffix string) string {
	return fmt.Sprintf("/later/a/%d%s", a.ID, suffix)
}

func TestArticleRendersTheSnapshot(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	doc := s.Get(t, s.Alice, articlePath(a, ""))

	if got := htmlassert.Text(doc.MustHave("h1")); !strings.Contains(got, "An Essay") {
		t.Errorf("h1 = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".later-article-body p")); got != "Hello reader" {
		t.Errorf("body paragraph = %q", got)
	}
	link := doc.MustHave(`a[href="https://blog.example/essay"]`)
	if got := htmlassert.Text(link); !strings.Contains(got, "Open original") {
		t.Errorf("link text = %q", got)
	}
	if rel, _ := htmlassert.Attr(link, "rel"); !strings.Contains(rel, "noopener") {
		t.Errorf("rel = %q, want noopener", rel)
	}
	meta := htmlassert.Text(doc.MustHave(".later-article-meta"))
	for _, want := range []string{"Blog", "Jo", "1 min read"} {
		if !strings.Contains(meta, want) {
			t.Errorf("meta %q lacks %q", meta, want)
		}
	}
	doc.MustHave(`script[src="/later/later.js"]`)
}

func TestOpeningAnUnreadArticleMovesItToReading(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	s.Get(t, s.Alice, articlePath(a, ""))
	got, err := s.Store.Article(context.Background(), s.Alice.User.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != later.StateReading {
		t.Errorf("State = %q, want reading", got.State)
	}
}

func TestArticleOfAnotherUserIs404(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	for _, c := range []struct{ method, suffix string }{
		{"GET", ""}, {"POST", "/archive"}, {"POST", "/unarchive"}, {"POST", "/delete"}, {"POST", "/text"},
	} {
		var rec *httptest.ResponseRecorder
		if c.method == "GET" {
			rec = s.Do(t, s.Bob, httptest.NewRequest("GET", articlePath(a, c.suffix), nil))
		} else {
			rec = s.Post(t, s.Bob, articlePath(a, c.suffix), url.Values{"text": {"x"}})
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s as bob = %d, want 404", c.method, c.suffix, rec.Code)
		}
	}
	if _, err := s.Store.Article(context.Background(), s.Alice.User.ID, a.ID); err != nil {
		t.Errorf("alice's article was touched: %v", err)
	}
}

func TestArticleSaysWhenItWasSavedBefore(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	if strings.Contains(s.Get(t, s.Alice, articlePath(a, "")).Text(), "You saved this before.") {
		t.Error("note shown without ?existing=1")
	}
	doc := s.Get(t, s.Alice, articlePath(a, "?existing=1"))
	if !strings.Contains(doc.Text(), "You saved this before.") {
		t.Error("note missing with ?existing=1")
	}
}

func TestLinkOnlyArticleOffersPasteText(t *testing.T) {
	s := newServer(t)
	a := seedLinkOnly(t, s)
	doc := s.Get(t, s.Alice, articlePath(a, ""))
	text := doc.Text()
	for _, want := range []string{"Couldn't read this page", "The page had no article text.", "Open original"} {
		if !strings.Contains(text, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	doc.MustHave(fmt.Sprintf(`form[action="/later/a/%d/text"] textarea[name=text]`, a.ID))
	doc.MustNotHave(".later-article-body")
}

func TestPastingTextMakesItReadable(t *testing.T) {
	s := newServer(t)
	a := seedLinkOnly(t, s)
	s.Submit(t, s.Alice, articlePath(a, "/text"), url.Values{"text": {"Para one\n\nPara two"}}, articlePath(a, ""))
	doc := s.Get(t, s.Alice, articlePath(a, ""))
	if n := len(doc.QueryAll(".later-article-body p")); n != 2 {
		t.Errorf("got %d paragraphs, want 2", n)
	}
	doc.MustNotHave("textarea")
}

func TestPastingBlankTextIs422(t *testing.T) {
	s := newServer(t)
	a := seedLinkOnly(t, s)
	rec := s.Post(t, s.Alice, articlePath(a, "/text"), url.Values{"text": {"  \n "}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Paste some text first.") {
		t.Error("message missing")
	}
	got, _ := s.Store.Article(context.Background(), s.Alice.User.ID, a.ID)
	if got.Content != later.ContentLinkOnly {
		t.Errorf("Content = %q, want link_only", got.Content)
	}
}

func TestArchiveAndUnarchive(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	ctx, uid := context.Background(), s.Alice.User.ID

	s.Submit(t, s.Alice, articlePath(a, "/archive"), url.Values{}, "/later/?tab=archived")
	if got, _ := s.Store.Article(ctx, uid, a.ID); got.State != later.StateArchived {
		t.Fatalf("State = %q, want archived", got.State)
	}
	doc := s.Get(t, s.Alice, articlePath(a, ""))
	if !strings.Contains(doc.Text(), "Move to unread") {
		t.Error("archived article does not offer Move to unread")
	}
	if got, _ := s.Store.Article(ctx, uid, a.ID); got.State != later.StateArchived {
		t.Errorf("opening an archived article changed it to %q", got.State)
	}

	s.Submit(t, s.Alice, articlePath(a, "/unarchive"), url.Values{}, "/later/?tab=unread")
	if got, _ := s.Store.Article(ctx, uid, a.ID); got.State != later.StateUnread {
		t.Errorf("State = %q, want unread", got.State)
	}
}

func TestDeleteRemovesTheArticle(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	doc := s.Get(t, s.Alice, articlePath(a, ""))
	form := doc.MustHave(`form[data-later-confirm]`)
	if got, _ := htmlassert.Attr(form, "action"); got != articlePath(a, "/delete") {
		t.Errorf("confirm form action = %q", got)
	}
	doc.MustHave("#later-confirm-dialog")

	// Opening moved it to reading, so that is the tab to go back to.
	s.Submit(t, s.Alice, articlePath(a, "/delete"), url.Values{}, "/later/?tab=reading")
	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", articlePath(a, ""), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET after delete = %d, want 404", rec.Code)
	}
}

func TestArticleBodyKeepsOnlyStoredHTML(t *testing.T) {
	s := newServer(t)
	a := seed(t, s, s.Alice.User.ID, later.NewArticle{URL: "https://x.example/p", Title: "P", ContentHTML: "<p>x</p>"})
	doc := s.Get(t, s.Alice, articlePath(a, ""))
	body := doc.MustHave(".later-article-body")
	if len(doc.QueryAll(".later-article-body p")) != 1 || htmlassert.Text(body) != "x" {
		t.Errorf("body = %q, want exactly one <p>x</p>", htmlassert.Text(body))
	}
}
