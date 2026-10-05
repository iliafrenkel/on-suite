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
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
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

// idFrom parses the saved id out of a save redirect to /later/?...saved={id}.
func idFrom(t *testing.T, rec *httptest.ResponseRecorder) int64 {
	t.Helper()
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil || u.Path != "/later/" {
		t.Fatalf("Location = %q, want /later/?saved={id}", loc)
	}
	id, err := strconv.ParseInt(u.Query().Get("saved"), 10, 64)
	if err != nil {
		t.Fatalf("Location = %q: %v", loc, err)
	}
	return id
}

func TestSaveExtractsAndRedirectsToTheList(t *testing.T) {
	s, a := newSaveServer(t)
	a.AllowPrivateFetchesForTest()
	origin := pageOrigin(t, "text/html; charset=utf-8", articlePage)

	rec := save(t, s, s.Alice, origin.URL+"/essay?utm_source=x")
	id := idFrom(t, rec)
	if want := fmt.Sprintf("/later/?saved=%d", id); rec.Header().Get("Location") != want {
		t.Errorf("Location = %q, want %q", rec.Header().Get("Location"), want)
	}

	got, err := s.Store.Article(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != later.ContentExtracted {
		t.Fatalf("Content = %q (%s), want extracted", got.Content, got.ExtractError)
	}
	if got.State != later.StateUnread {
		t.Errorf("State = %q, want unread (saving must not open it)", got.State)
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
	want := fmt.Sprintf("/later/?tab=unread&saved=%d&existing=1", first)
	if loc := rec.Header().Get("Location"); loc != want {
		t.Errorf("Location = %q, want %q", loc, want)
	}
	counts, err := s.Store.Counts(context.Background(), s.Alice.User.ID, "")
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
	counts, err := s.Store.Counts(context.Background(), s.Alice.User.ID, "")
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

func TestSaveOverHTMXReturnsAChip(t *testing.T) {
	s, a := newSaveServer(t)
	a.AllowPrivateFetchesForTest()
	origin := pageOrigin(t, "text/html", articlePage)

	rec := s.PostHX(t, s.Alice, "/later/save", url.Values{"url": {origin.URL + "/essay"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "<html") {
		t.Errorf("an HTMX save returned a whole page:\n%s", body)
	}
	doc := htmlassert.Parse(t, body)
	chip := doc.MustHave("span.later-chip")
	if got := htmlassert.Text(chip); !strings.Contains(got, "Saved to Later") {
		t.Errorf("chip = %q, want Saved to Later", got)
	}
	items, err := s.Store.List(context.Background(), s.Alice.User.ID, later.StateUnread, "", 0, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("unread list = %v, %v; want one row", items, err)
	}
	doc.MustHave(fmt.Sprintf(`a[href=/later/?saved=%d]`, items[0].ID))
}

func TestSaveOverHTMXForAnExistingArticle(t *testing.T) {
	s, a := newSaveServer(t)
	a.AllowPrivateFetchesForTest()
	origin := pageOrigin(t, "text/html", articlePage)
	id := idFrom(t, save(t, s, s.Alice, origin.URL+"/essay"))

	rec := s.PostHX(t, s.Alice, "/later/save", url.Values{"url": {origin.URL + "/essay"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := htmlassert.Text(doc.MustHave("span.later-chip")); !strings.Contains(got, "Already in Later") {
		t.Errorf("chip = %q, want Already in Later", got)
	}
	doc.MustHave(fmt.Sprintf(`a[href=/later/a/%d]`, id))
}

func TestSaveOverHTMXRejectsABadURL(t *testing.T) {
	s, _ := newSaveServer(t)
	rec := s.PostHX(t, s.Alice, "/later/save", url.Values{"url": {"ftp://x"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	chip := doc.MustHave("span.later-chip-error")
	if got := htmlassert.Text(chip); !strings.Contains(got, "Couldn't save this link") {
		t.Errorf("chip = %q", got)
	}
}
