package later_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
