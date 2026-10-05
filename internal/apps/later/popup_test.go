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
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func totalCount(t *testing.T, s *server) int {
	t.Helper()
	counts, err := s.Store.Counts(context.Background(), s.Alice.User.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range counts {
		n += c
	}
	return n
}

func TestPopupGetNeverSaves(t *testing.T) {
	s := newServer(t)
	target := "https://example.com/x"
	doc := s.Get(t, s.Alice, "/later/save?url="+url.QueryEscape(target))
	doc.MustHave(".later-popup")
	doc.MustHave(`form[action="/later/save"]`)
	in := doc.MustHave(`form[action="/later/save"] input[name="url"]`)
	if v, _ := htmlassert.Attr(in, "value"); v != target {
		t.Errorf("url value = %q, want %q", v, target)
	}
	pop := doc.MustHave(`form[action="/later/save"] input[name="popup"]`)
	if v, _ := htmlassert.Attr(pop, "value"); v != "1" {
		t.Errorf("popup value = %q, want 1", v)
	}
	doc.MustHave(`form[action="/later/save"] button[autofocus]`)
	doc.MustHave(`button[data-later-close]`)
	if n := totalCount(t, s); n != 0 {
		t.Errorf("GET saved %d articles, want 0", n)
	}
}

func TestPopupShowsAlreadySaved(t *testing.T) {
	s := newServer(t)
	a := seed(t, s, s.Alice.User.ID, later.NewArticle{URL: "https://example.com/x", Title: "T", ContentHTML: words(10)})
	doc := s.Get(t, s.Alice, "/later/save?url="+url.QueryEscape("https://example.com/x"))
	if !strings.Contains(doc.Text(), "Already in ON Later.") {
		t.Errorf("missing already-saved text: %s", doc.Text())
	}
	link := doc.MustHave(fmt.Sprintf(`a[href="/later/a/%d"]`, a.ID))
	if v, _ := htmlassert.Attr(link, "target"); v != "_blank" {
		t.Errorf("target = %q, want _blank", v)
	}
	doc.MustNotHave(".later-popup form")
}

func TestPopupRejectsABadURL(t *testing.T) {
	s := newServer(t)
	for _, u := range []string{"ftp://x", ""} {
		doc := s.Get(t, s.Alice, "/later/save?url="+url.QueryEscape(u))
		if !strings.Contains(doc.Text(), "doesn't look like a web address") {
			t.Errorf("url %q: missing message: %s", u, doc.Text())
		}
		doc.MustNotHave(".later-popup form")
	}
}

func TestPopupSaveRedirectsToDone(t *testing.T) {
	s, a := newSaveServer(t)
	a.AllowPrivateFetchesForTest()
	origin := pageOrigin(t, "text/html; charset=utf-8", articlePage)
	rec := s.Post(t, s.Alice, "/later/save", url.Values{"url": {origin.URL + "/essay"}, "popup": {"1"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	art, err := s.Store.ArticleByURL(context.Background(), s.Alice.User.ID, origin.URL+"/essay")
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("/later/popup/done?saved=%d", art.ID); rec.Header().Get("Location") != want {
		t.Errorf("Location = %q, want %q", rec.Header().Get("Location"), want)
	}
	if art.State != later.StateUnread {
		t.Errorf("state = %q, want unread", art.State)
	}
	// Posting again reports the existing article.
	rec = s.Post(t, s.Alice, "/later/save", url.Values{"url": {origin.URL + "/essay"}, "popup": {"1"}})
	if want := fmt.Sprintf("/later/popup/done?saved=%d&existing=1", art.ID); rec.Header().Get("Location") != want {
		t.Errorf("Location = %q, want %q", rec.Header().Get("Location"), want)
	}
}

func TestPopupDonePageAutoCloses(t *testing.T) {
	s := newServer(t)
	a := seed(t, s, s.Alice.User.ID, later.NewArticle{URL: "https://example.com/x", Title: "My Title", ContentHTML: words(10)})
	doc := s.Get(t, s.Alice, fmt.Sprintf("/later/popup/done?saved=%d", a.ID))
	doc.MustHave(".later-popup [data-later-autoclose]")
	if !strings.Contains(doc.Text(), "Saved “My Title”.") {
		t.Errorf("missing saved note: %s", doc.Text())
	}
	doc = s.Get(t, s.Alice, fmt.Sprintf("/later/popup/done?saved=%d&existing=1", a.ID))
	if !strings.Contains(doc.Text(), "You saved “My Title” before.") {
		t.Errorf("missing existing note: %s", doc.Text())
	}
}

func TestPopupDoneIgnoresOtherUsersIDs(t *testing.T) {
	s := newServer(t)
	a := seed(t, s, s.Bob.User.ID, later.NewArticle{URL: "https://example.com/x", Title: "Bobs Secret", ContentHTML: words(10)})
	doc := s.Get(t, s.Alice, fmt.Sprintf("/later/popup/done?saved=%d", a.ID))
	if strings.Contains(doc.Text(), "Bobs Secret") {
		t.Error("leaked another user's title")
	}
	doc.MustHave("[data-later-autoclose]")
	if !strings.Contains(doc.Text(), "Done.") {
		t.Errorf("missing Done.: %s", doc.Text())
	}
}

func TestPopupInvalidPostIs422(t *testing.T) {
	s := newServer(t)
	rec := s.Post(t, s.Alice, "/later/save", url.Values{"url": {"ftp://x"}, "popup": {"1"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".later-popup")
	if !strings.Contains(doc.Text(), "doesn't look like a web address") {
		t.Errorf("missing message: %s", doc.Text())
	}
}

func TestListPageOffersTheBookmarklet(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/later/")
	doc.MustHave("a[data-later-bookmarklet]")
}

func TestPopupRequiresSignIn(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, nil, httptest.NewRequest("GET", "/later/save?url=https://example.com/x", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("anonymous = %d, want 303", rec.Code)
	}
}
