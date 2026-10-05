package later_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
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
	doc.MustNotHave("#later-text") // the paste form is gone
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

// --- saved note on the list ---------------------------------------------

func TestListShowsANoteForAJustSavedArticle(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	doc := s.Get(t, s.Alice, fmt.Sprintf("/later/?saved=%d", a.ID))
	note := doc.MustHave(`p.later-saved`)
	if role, _ := htmlassert.Attr(note, "role"); role != "status" {
		t.Errorf("role = %q, want status", role)
	}
	if got := htmlassert.Text(note); !strings.Contains(got, "Saved “An Essay”.") {
		t.Errorf("note = %q", got)
	}
	doc.MustHave(fmt.Sprintf(`.later-saved a[href="/later/a/%d"]`, a.ID))
}

func TestListNoteForALinkOnlyArticle(t *testing.T) {
	s := newServer(t)
	a := seedLinkOnly(t, s)
	doc := s.Get(t, s.Alice, fmt.Sprintf("/later/?saved=%d", a.ID))
	got := htmlassert.Text(doc.MustHave(".later-saved"))
	if !strings.Contains(got, "Saved “Walled” as a link only — open it to add the text.") {
		t.Errorf("note = %q", got)
	}
	doc.MustHave(fmt.Sprintf(`.later-saved a[href="/later/a/%d"]`, a.ID))
}

func TestListNoteForAnAlreadySavedArticle(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	if err := s.Store.SetState(context.Background(), s.Alice.User.ID, a.ID, later.StateArchived); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, fmt.Sprintf("/later/?tab=archived&saved=%d&existing=1", a.ID))
	if got := htmlassert.Text(doc.MustHave(".later-saved")); !strings.Contains(got, "You saved “An Essay” before.") {
		t.Errorf("note = %q", got)
	}
	current := ""
	for _, tab := range doc.QueryAll("a.later-tab") {
		if _, ok := htmlassert.Attr(tab, "aria-current"); ok {
			current, _ = htmlassert.Attr(tab, "href")
		}
	}
	if current != "/later/?tab=archived" {
		t.Errorf("current tab href = %q, want archived", current)
	}
	doc.MustHave(".later-row")
}

func TestListIgnoresASavedIdThatIsNotTheViewers(t *testing.T) {
	s := newServer(t)
	b := seed(t, s, s.Bob.User.ID, later.NewArticle{URL: "https://b.example/x", Title: "Bobs", ContentHTML: words(5)})
	doc := s.Get(t, s.Alice, fmt.Sprintf("/later/?saved=%d", b.ID))
	doc.MustNotHave(".later-saved")
	if strings.Contains(doc.Text(), "Bobs") {
		t.Error("alice saw bob's title")
	}
}

func TestListIgnoresAMalformedSavedId(t *testing.T) {
	s := newServer(t)
	for _, v := range []string{"abc", "0", "-3", "99999"} {
		s.Get(t, s.Alice, "/later/?saved="+v).MustNotHave(".later-saved")
	}
}

func TestListHasATitle(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/later/")
	if got := htmlassert.Text(doc.MustHave("title")); !strings.Contains(got, "ON Later") {
		t.Errorf("title = %q", got)
	}
}

func TestPastingTextOntoAReadableArticleIs400(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	rec := s.Post(t, s.Alice, articlePath(a, "/text"), url.Values{"text": {"replacement"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	got, err := s.Store.Article(context.Background(), s.Alice.User.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentHTML != a.ContentHTML || got.Content != a.Content {
		t.Errorf("article changed: %q / %q", got.Content, got.ContentHTML)
	}
}

func TestArticleRendersTheReaderChrome(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	doc := s.Get(t, s.Alice, articlePath(a, ""))

	root := doc.MustHave(".later-reader")
	class, _ := htmlassert.Attr(root, "class")
	for _, want := range []string{"later-font-serif", "later-size-3", "later-width-medium"} {
		if !strings.Contains(class, want) {
			t.Errorf("reader class %q lacks %q", class, want)
		}
	}
	top := htmlassert.Text(doc.MustHave(".later-topbar"))
	for _, want := range []string{"← Later", "An Essay", "min left"} {
		if !strings.Contains(top, want) {
			t.Errorf("top bar %q lacks %q", top, want)
		}
	}
	// Opening moved the article to Reading, so ← Later goes to that tab.
	doc.MustHave(`.later-topbar a[href="/later/?tab=reading"]`)
	doc.MustHave("progress.later-progress")

	doc.MustHave("details.later-aa")
	forms := doc.QueryAll(`details.later-aa form[action="/later/prefs"]`)
	if len(forms) != 7 { // 2 fonts, A-, A+, 3 widths
		t.Fatalf("Aa menu has %d forms, want 7", len(forms))
	}
	for _, f := range forms {
		if got := htmlassert.Text(f); got == "" {
			t.Error("an Aa form has no button text")
		}
	}
	for _, in := range doc.QueryAll(`details.later-aa input[name=back]`) {
		if v, _ := htmlassert.Attr(in, "value"); v != articlePath(a, "") {
			t.Errorf("Aa form back = %q, want %q", v, articlePath(a, ""))
		}
	}

	doc.MustHave(`.later-topbar a[href="https://blog.example/essay"]`)
	doc.MustHave(`.later-topbar form[data-later-confirm]`)
}

func TestArticleUsesTheReadersPrefs(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	if err := s.Store.SetPrefs(context.Background(), s.Alice.User.ID, later.Prefs{Font: "sans", Size: 5, Width: "wide"}); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, articlePath(a, ""))
	class, _ := htmlassert.Attr(doc.MustHave(".later-reader"), "class")
	for _, want := range []string{"later-font-sans", "later-size-5", "later-width-wide"} {
		if !strings.Contains(class, want) {
			t.Errorf("reader class %q lacks %q", class, want)
		}
	}
	btn := doc.MustHave(`details.later-aa button[disabled]`)
	if got := htmlassert.Text(btn); got != "A+" {
		t.Errorf("disabled Aa button = %q, want A+ (the largest size is reached)", got)
	}
	if n := len(doc.QueryAll(`details.later-aa button[disabled]`)); n != 1 {
		t.Errorf("%d disabled Aa buttons, want 1", n)
	}
}

func TestPrefsFormSavesAndRedirectsBack(t *testing.T) {
	s := newServer(t)
	ctx, uid := context.Background(), s.Alice.User.ID
	s.Submit(t, s.Alice, "/later/prefs", url.Values{"size": {"4"}, "back": {"/later/a/1"}}, "/later/a/1")
	got, err := s.Store.Prefs(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	want := later.DefaultPrefs
	want.Size = 4
	if got != want {
		t.Errorf("prefs = %+v, want %+v", got, want)
	}
}

func TestPrefsAsyncAnswers204(t *testing.T) {
	s := newServer(t)
	form := url.Values{"width": {"wide"}, web.CSRFFormField: {s.CSRFToken(t, s.Alice)}}
	req := httptest.NewRequest("POST", "/later/prefs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Later-Async", "1")
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("async POST = %d, want 204", rec.Code)
	}
	got, _ := s.Store.Prefs(context.Background(), s.Alice.User.ID)
	if got.Width != "wide" {
		t.Errorf("width = %q, want wide", got.Width)
	}
}

func TestPrefsRejectsInvalidValues(t *testing.T) {
	s := newServer(t)
	for _, form := range []url.Values{{"size": {"9"}}, {"size": {"x"}}, {"font": {"mono"}}, {"width": {"huge"}}} {
		if rec := s.Post(t, s.Alice, "/later/prefs", form); rec.Code != http.StatusBadRequest {
			t.Errorf("POST %v = %d, want 400", form, rec.Code)
		}
	}
	got, _ := s.Store.Prefs(context.Background(), s.Alice.User.ID)
	if got != later.DefaultPrefs {
		t.Errorf("prefs changed to %+v", got)
	}
}

func TestPrefsBackMustBeLocal(t *testing.T) {
	s := newServer(t)
	s.Submit(t, s.Alice, "/later/prefs", url.Values{"size": {"2"}, "back": {"https://evil.example/"}}, "/later/")
}

func TestArticleMetaDoesNotRepeatTheAuthorAsSite(t *testing.T) {
	s := newServer(t)
	a := seed(t, s, s.Alice.User.ID, later.NewArticle{
		URL: "https://jvns.example/p", Title: "P", SiteName: "Julia Evans", Byline: " julia evans ",
		ContentHTML: "<p>x</p>",
	})
	meta := htmlassert.Text(s.Get(t, s.Alice, articlePath(a, "")).MustHave(".later-article-meta"))
	if got := strings.Count(strings.ToLower(meta), "julia evans"); got != 1 {
		t.Errorf("meta %q mentions the author %d times, want 1", meta, got)
	}
}

// --- reading progress ---------------------------------------------------

func TestProgressIsSaved(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	ctx, uid := context.Background(), s.Alice.User.ID
	if err := s.Store.MarkOpened(ctx, uid, a.ID); err != nil {
		t.Fatal(err)
	}

	rec := s.Post(t, s.Alice, articlePath(a, "/progress"), url.Values{"progress": {"0.42"}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST progress = %d, want 204; body: %s", rec.Code, rec.Body.String())
	}
	got, _ := s.Store.Article(ctx, uid, a.ID)
	if got.Progress != 0.42 {
		t.Errorf("Progress = %v, want 0.42", got.Progress)
	}
	if got.State != later.StateReading {
		t.Errorf("State = %q, saving progress must not change it", got.State)
	}
}

func TestProgressRejectsGarbage(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	for _, v := range []string{"abc", "NaN", "", "Inf"} {
		rec := s.Post(t, s.Alice, articlePath(a, "/progress"), url.Values{"progress": {v}})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("progress=%q = %d, want 400", v, rec.Code)
		}
	}
}

func TestProgressOfAnotherUserIs404(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	rec := s.Post(t, s.Bob, articlePath(a, "/progress"), url.Values{"progress": {"0.5"}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("progress as bob = %d, want 404", rec.Code)
	}
	got, _ := s.Store.Article(context.Background(), s.Alice.User.ID, a.ID)
	if got.Progress != 0 {
		t.Errorf("alice's progress = %v, want 0", got.Progress)
	}
}

func TestArticleCarriesSavedProgress(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	s.Post(t, s.Alice, articlePath(a, "/progress"), url.Values{"progress": {"0.42"}})

	doc := s.Get(t, s.Alice, articlePath(a, ""))
	root := doc.MustHave("#later-reader")
	if v, ok := htmlassert.Attr(root, "data-progress"); !ok || v != "0.42" {
		t.Errorf("data-progress = %q (present %v), want 0.42", v, ok)
	}
	bar := doc.MustHave("progress[data-later-progress]")
	if v, _ := htmlassert.Attr(bar, "value"); v != "0.42" {
		t.Errorf("progress value = %q, want 0.42", v)
	}
	list := s.Get(t, s.Alice, "/later/?tab=reading")
	row := list.MustHave("progress.later-row-progress")
	if v, _ := htmlassert.Attr(row, "value"); v != "42" {
		t.Errorf("list row progress = %q, want 42", v)
	}
}

func TestLinkOnlyArticleHasNoProgress(t *testing.T) {
	s := newServer(t)
	a := seedLinkOnly(t, s)
	doc := s.Get(t, s.Alice, articlePath(a, ""))
	if _, ok := htmlassert.Attr(doc.MustHave("#later-reader"), "data-progress"); ok {
		t.Error("link-only reader has data-progress")
	}
	doc.MustNotHave("progress[data-later-progress]")
}

// --- row ⋯ menu ----------------------------------------------------------

func TestRowMenuOffersTheRightActions(t *testing.T) {
	s := newServer(t)
	seedStates(t, s)

	doc := s.Get(t, s.Alice, "/later/?tab=unread")
	doc.MustHave(".later-row details.later-row-menu")
	doc.MustHave(`form[action="/later/a/1/archive"]`)
	del := doc.MustHave(`form[action="/later/a/1/delete"]`)
	if _, ok := htmlassert.Attr(del, "data-later-confirm"); !ok {
		t.Error("row Delete form lacks data-later-confirm")
	}
	doc.MustHave(`input[value="/later/?tab=unread"]`)
	doc.MustNotHave(`form[action="/later/a/1/unarchive"]`)
	doc.MustNotHave(".later-row-menu .later-row-link") // menu stays out of the link

	arch := s.Get(t, s.Alice, "/later/?tab=archived")
	arch.MustHave(`form[action="/later/a/4/unarchive"]`)
	arch.MustNotHave(`form[action="/later/a/4/archive"]`)
	arch.MustHave(`input[value="/later/?tab=archived"]`)
	if !strings.Contains(arch.Text(), "Move to unread") {
		t.Error("archived row lacks Move to unread")
	}
}

func TestRowMenuWorksInTheLoadMoreFragment(t *testing.T) {
	s := newServer(t)
	for i := 0; i < 30; i++ {
		seed(t, s, s.Alice.User.ID, later.NewArticle{URL: fmt.Sprintf("https://m.example/%d", i), Title: "T", ContentHTML: words(5)})
	}
	req := httptest.NewRequest("GET", "/later/?tab=unread&offset=25", nil)
	req.Header.Set("HX-Request", "true")
	rec := s.Do(t, s.Alice, req)
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(`input[value="/later/?tab=unread"]`)
	doc.MustHave(".later-row-menu")
}

func TestArchiveFromTheListStaysOnTheList(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	s.Submit(t, s.Alice, articlePath(a, "/archive"), url.Values{"back": {"/later/?tab=unread"}}, "/later/?tab=unread")
	s.Submit(t, s.Alice, articlePath(a, "/unarchive"), url.Values{"back": {"/later/?tab=archived"}}, "/later/?tab=archived")
}

func TestDeleteFromTheListStaysOnTheList(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	s.Submit(t, s.Alice, articlePath(a, "/delete"), url.Values{"back": {"/later/?tab=unread"}}, "/later/?tab=unread")
}

func TestActionsIgnoreForeignBackTargets(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	evil := url.Values{"back": {"https://evil.example"}}
	s.Submit(t, s.Alice, articlePath(a, "/archive"), evil, "/later/?tab=archived")
	s.Submit(t, s.Alice, articlePath(a, "/unarchive"), evil, "/later/?tab=unread")
	s.Submit(t, s.Alice, articlePath(a, "/delete"), evil, "/later/?tab=unread")
}

func TestIndexIncludesTheConfirmDialog(t *testing.T) {
	s := newServer(t)
	s.Get(t, s.Alice, "/later/").MustHave("#later-confirm-dialog")
}

// --- highlights drawn in the snapshot -----------------------------------

func TestArticleDrawsItsHighlights(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	h, err := s.Store.AddHighlight(context.Background(), a.ID, a.ContentText, 6, 12, "reader", "")
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, articlePath(a, ""))
	body := doc.MustHave("div#later-body")
	if got, _ := htmlassert.Attr(body, "class"); got != "later-article-body" {
		t.Errorf("body class = %q, want later-article-body", got)
	}
	id := strconv.FormatInt(h.ID, 10)
	mark := doc.MustHave("mark#later-h-" + id)
	if got, _ := htmlassert.Attr(mark, "data-highlight-id"); got != id {
		t.Errorf("data-highlight-id = %q, want %q", got, id)
	}
	if got := htmlassert.Text(mark); got != "reader" {
		t.Errorf("mark text = %q, want reader", got)
	}
}

func TestArticleWithAStaleHighlightStillRenders(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	if _, err := s.Store.InsertHighlightForTest(context.Background(), a.ID, 6, 12, "nope"); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, articlePath(a, ""))
	doc.MustNotHave("mark")
	if got := htmlassert.Text(doc.MustHave("#later-body")); got != "Hello reader" {
		t.Errorf("body text = %q, want Hello reader", got)
	}
}
