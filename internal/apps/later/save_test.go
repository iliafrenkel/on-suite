package later_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/apptest"
)

// articlePage is about 300 words of prose in an <article>, with an image and
// a script that must not survive extraction.
var articlePage = `<html><head><title>Essay | Example</title></head><body>
<nav>Home About</nav>
<article>
<h1>Essay</h1>
` + strings.Repeat(`<p>Reading things later is a quiet pleasure when the page stays put and the
words stay readable, so a saved article should look the same tomorrow.</p>
`, 12) + `<img src="/pic.png">
<script>alert(1)</script>
</article>
<footer>Copyright</footer>
</body></html>`

func newSaveServer(t *testing.T) (*server, *later.App) {
	t.Helper()
	a := later.New()
	s := apptest.NewServer(t, a, later.NewStore)
	return s, a
}

func pageOrigin(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func save(t *testing.T, s *server, sess *apptest.Session, target string) *httptest.ResponseRecorder {
	t.Helper()
	return s.Post(t, sess, "/later/save", url.Values{"url": {target}})
}

// idFrom parses /later/a/{id}[?existing=1] out of a redirect.
func idFrom(t *testing.T, rec *httptest.ResponseRecorder) int64 {
	t.Helper()
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	rest, ok := strings.CutPrefix(loc, "/later/a/")
	if !ok {
		t.Fatalf("Location = %q, want /later/a/{id}", loc)
	}
	rest, _, _ = strings.Cut(rest, "?")
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		t.Fatalf("Location = %q: %v", loc, err)
	}
	return id
}

func TestSaveExtractsAndRedirectsToTheArticle(t *testing.T) {
	s, a := newSaveServer(t)
	a.AllowPrivateFetchesForTest()
	origin := pageOrigin(t, "text/html; charset=utf-8", articlePage)

	rec := save(t, s, s.Alice, origin.URL+"/essay?utm_source=x")
	id := idFrom(t, rec)

	got, err := s.Store.Article(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != later.ContentExtracted {
		t.Fatalf("Content = %q (%s), want extracted", got.Content, got.ExtractError)
	}
	if strings.Contains(got.URL, "utm_source") {
		t.Errorf("URL kept its tracking parameter: %s", got.URL)
	}
	if strings.Contains(got.ContentHTML, "<script") {
		t.Errorf("script survived: %s", got.ContentHTML)
	}
	if !strings.Contains(got.ContentHTML, `src="/later/img/`) {
		t.Errorf("image not rewritten: %s", got.ContentHTML)
	}
	if got.WordCount <= 250 {
		t.Errorf("WordCount = %d, want > 250", got.WordCount)
	}
}

func TestSavingTheSameURLAgainGoesToTheExistingArticle(t *testing.T) {
	s, a := newSaveServer(t)
	a.AllowPrivateFetchesForTest()
	origin := pageOrigin(t, "text/html", articlePage)

	first := idFrom(t, save(t, s, s.Alice, origin.URL+"/essay"))
	rec := save(t, s, s.Alice, origin.URL+"/essay#section")
	if second := idFrom(t, rec); second != first {
		t.Errorf("second save id = %d, want %d", second, first)
	}
	if loc := rec.Header().Get("Location"); !strings.HasSuffix(loc, "?existing=1") {
		t.Errorf("Location = %q, want ?existing=1", loc)
	}
	counts, err := s.Store.Counts(context.Background(), s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if counts[later.StateUnread] != 1 {
		t.Errorf("unread = %d, want 1", counts[later.StateUnread])
	}
}

func linkOnly(t *testing.T, s *server, rec *httptest.ResponseRecorder) later.Article {
	t.Helper()
	got, err := s.Store.Article(context.Background(), s.Alice.User.ID, idFrom(t, rec))
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != later.ContentLinkOnly {
		t.Fatalf("Content = %q, want link_only", got.Content)
	}
	if got.ExtractError == "" {
		t.Error("ExtractError is empty; the reason must be recorded")
	}
	return got
}

func TestSaveKeepsALinkOnlyItemWhenExtractionFails(t *testing.T) {
	s, a := newSaveServer(t)
	a.AllowPrivateFetchesForTest()
	origin := pageOrigin(t, "text/html", "<html><head><title>Members only</title></head><body><p>Sign in</p></body></html>")

	got := linkOnly(t, s, save(t, s, s.Alice, origin.URL+"/wall"))
	if got.Title != "Members only" {
		t.Errorf("Title = %q, want the page's <title>", got.Title)
	}
}

func TestSaveKeepsALinkOnlyItemWhenTheFetchFails(t *testing.T) {
	s, a := newSaveServer(t)
	a.AllowPrivateFetchesForTest()
	origin := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(origin.Close)

	got := linkOnly(t, s, save(t, s, s.Alice, origin.URL+"/gone"))
	if got.Title != "127.0.0.1" {
		t.Errorf("Title = %q, want the host", got.Title)
	}
	if !strings.Contains(got.ExtractError, "404") {
		t.Errorf("ExtractError = %q, want it to mention 404", got.ExtractError)
	}
}

func TestSaveKeepsALinkOnlyItemForNonHTML(t *testing.T) {
	s, a := newSaveServer(t)
	a.AllowPrivateFetchesForTest()
	origin := pageOrigin(t, "application/pdf", "%PDF-1.4 not really")

	got := linkOnly(t, s, save(t, s, s.Alice, origin.URL+"/paper.pdf"))
	if !strings.Contains(got.ExtractError, "application/pdf") {
		t.Errorf("ExtractError = %q, want the content type", got.ExtractError)
	}
}

func TestSaveGivesUpAfterTheTimeout(t *testing.T) {
	s, a := newSaveServer(t)
	a.AllowPrivateFetchesForTest()
	defer later.SetSaveTimeoutForTest(50 * time.Millisecond)()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(500 * time.Millisecond):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(origin.Close)

	linkOnly(t, s, save(t, s, s.Alice, origin.URL+"/slow"))
}

func TestSaveRejectsAnInvalidURL(t *testing.T) {
	s, _ := newSaveServer(t)
	rec := save(t, s, s.Alice, "ftp://x")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	want := "That doesn&#39;t look like a web address. It needs to start with http:// or https://."
	if !strings.Contains(rec.Body.String(), want) {
		t.Errorf("page does not show the message:\n%s", rec.Body.String())
	}
	counts, err := s.Store.Counts(context.Background(), s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if counts[later.StateUnread] != 0 {
		t.Errorf("a row was saved for an invalid URL")
	}
}

func TestSaveRefusesPrivateAddressesWithTheRealGuard(t *testing.T) {
	s, _ := newSaveServer(t)
	got := linkOnly(t, s, save(t, s, s.Alice, "http://127.0.0.1:1/x"))
	if !strings.Contains(got.ExtractError, "blocked address") {
		t.Errorf("ExtractError = %q, want blocked address", got.ExtractError)
	}
}

func TestSaveIsPerUser(t *testing.T) {
	s, a := newSaveServer(t)
	a.AllowPrivateFetchesForTest()
	origin := pageOrigin(t, "text/html", articlePage)

	aliceID := idFrom(t, save(t, s, s.Alice, origin.URL+"/essay"))
	bobRec := save(t, s, s.Bob, origin.URL+"/essay")
	bobID := idFrom(t, bobRec)
	if aliceID == bobID {
		t.Fatal("both users got the same row")
	}
	if strings.Contains(bobRec.Header().Get("Location"), "existing") {
		t.Error("Bob was told his save already existed")
	}
}
