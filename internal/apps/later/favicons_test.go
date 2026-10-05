package later_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

func (f *fixture) countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) saveIcon(t *testing.T, userID int64, pageURL, iconURL string) later.Article {
	t.Helper()
	a, created, err := f.store.Save(context.Background(), userID, later.NewArticle{
		URL: pageURL, Title: "T", ContentHTML: "<p>body</p>", FaviconURL: iconURL,
	})
	if err != nil || !created {
		t.Fatalf("Save(%s) = %v, created %v", pageURL, err, created)
	}
	return a
}

func TestSaveRecordsTheSitesFaviconOnce(t *testing.T) {
	f := newFixture(t)
	f.saveIcon(t, f.alice.ID, "https://example.com/one", "https://example.com/icon.png")
	f.saveIcon(t, f.alice.ID, "https://example.com/two", "https://example.com/other.png")

	var hash string
	if err := f.db.QueryRow(`SELECT hash FROM later_site_favicons WHERE site_host = 'example.com'`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if want := webfetch.URLHash("https://example.com/icon.png"); hash != want {
		t.Errorf("site favicon hash = %s, want the first icon's %s", hash, want)
	}
	if n := f.countRows(t, `SELECT count(*) FROM later_site_favicons`); n != 1 {
		t.Errorf("later_site_favicons rows = %d, want 1", n)
	}
}

func TestTwoSitesCanShareOneIcon(t *testing.T) {
	f := newFixture(t)
	f.saveIcon(t, f.alice.ID, "https://a.example/x", "https://cdn.example/i.png")
	f.saveIcon(t, f.alice.ID, "https://b.example/x", "https://cdn.example/i.png")

	if n := f.countRows(t, `SELECT count(*) FROM later_favicons`); n != 1 {
		t.Errorf("later_favicons rows = %d, want 1", n)
	}
	if n := f.countRows(t, `SELECT count(*) FROM later_site_favicons`); n != 2 {
		t.Errorf("later_site_favicons rows = %d, want 2", n)
	}
}

func TestListCarriesTheFavicon(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	withIcon := f.saveIcon(t, f.alice.ID, "https://example.com/a", "https://example.com/icon.png")
	without := f.saveIcon(t, f.alice.ID, "https://plain.example/a", "")
	hash := webfetch.URLHash("https://example.com/icon.png")

	list := func() map[int64]later.ListItem {
		items, err := f.store.List(ctx, f.alice.ID, later.StateUnread, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		m := map[int64]later.ListItem{}
		for _, it := range items {
			m[it.ID] = it
		}
		return m
	}

	got := list()
	if it := got[withIcon.ID]; it.FaviconHash != hash || !it.FaviconShown {
		t.Errorf("fresh icon: hash %q shown %v; want %q, true", it.FaviconHash, it.FaviconShown, hash)
	}
	if it := got[without.ID]; it.FaviconHash != "" || it.FaviconShown {
		t.Errorf("no icon: hash %q shown %v; want \"\", false", it.FaviconHash, it.FaviconShown)
	}

	for i := 0; i < webfetch.MaxImageAttempts; i++ {
		if err := f.store.SaveFaviconFailure(ctx, hash, "boom"); err != nil {
			t.Fatal(err)
		}
	}
	if it := list()[withIcon.ID]; it.FaviconHash != hash || it.FaviconShown {
		t.Errorf("given-up icon: hash %q shown %v; want %q, false", it.FaviconHash, it.FaviconShown, hash)
	}

	if err := f.store.SaveFaviconBytes(ctx, hash, "image/png", []byte("png")); err != nil {
		t.Fatal(err)
	}
	if it := list()[withIcon.ID]; !it.FaviconShown {
		t.Error("cached icon should be shown")
	}
}

func TestFaviconForUserIsOwnerScoped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.saveIcon(t, f.alice.ID, "https://example.com/a", "https://example.com/icon.png")
	hash := webfetch.URLHash("https://example.com/icon.png")

	fav, err := f.store.FaviconForUser(ctx, f.alice.ID, hash)
	if err != nil || fav.SrcURL != "https://example.com/icon.png" || fav.Cached() {
		t.Fatalf("alice: %+v, %v", fav, err)
	}
	if _, err := f.store.FaviconForUser(ctx, f.bob.ID, hash); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("bob: err = %v, want ErrNotFound", err)
	}
}

// faviconOrigin serves the article at /essay with a <link rel="icon">, and a
// tiny PNG at /fav.png (or 404 when iconMissing), counting icon hits.
type faviconOrigin struct {
	*httptest.Server
	hits    atomic.Int32
	missing atomic.Bool
}

func newFaviconOrigin(t *testing.T) *faviconOrigin {
	t.Helper()
	o := &faviconOrigin{}
	pic := tinyPNG(t)
	page := strings.Replace(articlePage, `<head>`, `<head><link rel="icon" href="/fav.png">`, 1)
	o.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fav.png":
			o.hits.Add(1)
			if o.missing.Load() {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pic)
		case "/essay":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(page))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(o.Close)
	return o
}

func getFavicon(s *server, sess *apptest.Session, hash string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/later/favicon/"+hash, nil)
	rec := httptest.NewRecorder()
	for _, c := range sess.Cookies {
		req.AddCookie(c)
	}
	s.Handler.ServeHTTP(rec, req)
	return rec
}

func savedWithFavicon(t *testing.T) (*server, *later.App, *faviconOrigin, string) {
	t.Helper()
	s, a := newSaveServer(t)
	a.AllowPrivateFetchesForTest()
	o := newFaviconOrigin(t)
	idFrom(t, save(t, s, s.Alice, o.URL+"/essay"))
	return s, a, o, webfetch.URLHash(o.URL + "/fav.png")
}

func TestSavingDiscoversTheFavicon(t *testing.T) {
	s, _, _, hash := savedWithFavicon(t)

	doc := s.Get(t, s.Alice, "/later/")
	img := doc.Query(`img[src=/later/favicon/` + hash + `]`)
	if img == nil {
		t.Fatalf("no favicon img with src /later/favicon/%s", hash)
	}
	if c, _ := htmlassert.Attr(img, "class"); c != "later-favicon" {
		t.Errorf("img class = %q", c)
	}
}

func TestFaviconRouteFetchesStoresAndServes(t *testing.T) {
	s, _, o, hash := savedWithFavicon(t)

	rec := getFavicon(s, s.Alice, hash)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q", ct)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
	if rec.Header().Get("ETag") == "" {
		t.Error("missing ETag")
	}
	if o.hits.Load() != 1 {
		t.Fatalf("origin hits = %d, want 1", o.hits.Load())
	}
	if rec := getFavicon(s, s.Alice, hash); rec.Code != 200 {
		t.Fatalf("second status = %d", rec.Code)
	}
	if o.hits.Load() != 1 {
		t.Errorf("origin hits after second GET = %d, want 1", o.hits.Load())
	}
}

func TestFaviconRouteAnswersBare404WhenGivenUp(t *testing.T) {
	s, a := newSaveServer(t)
	a.AllowPrivateFetchesForTest()
	o := newFaviconOrigin(t)
	o.missing.Store(true)
	idFrom(t, save(t, s, s.Alice, o.URL+"/essay"))
	hash := webfetch.URLHash(o.URL + "/fav.png")

	for i := 0; i < webfetch.MaxImageAttempts; i++ {
		if rec := getFavicon(s, s.Alice, hash); rec.Code != 404 {
			t.Fatalf("attempt %d: status = %d, want 404", i, rec.Code)
		}
		s.Clock.Advance(2 * time.Hour)
	}
	hits := o.hits.Load()
	if hits != int32(webfetch.MaxImageAttempts) {
		t.Fatalf("origin hits = %d, want %d", hits, webfetch.MaxImageAttempts)
	}
	rec := getFavicon(s, s.Alice, hash)
	if rec.Code != 404 || rec.Body.Len() != 0 {
		t.Errorf("given-up: status %d, body %q; want bare 404", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, max-age=3600" {
		t.Errorf("Cache-Control = %q", cc)
	}
	if o.hits.Load() != hits {
		t.Error("origin hit after giving up")
	}

	doc := s.Get(t, s.Alice, "/later/")
	badge := doc.MustHave(".later-favicon-badge")
	if _, hidden := htmlassert.Attr(badge, "hidden"); hidden {
		t.Error("badge is hidden; want visible")
	}
	doc.MustNotHave("img.later-favicon")
}

func TestLinkOnlyItemsGuessFaviconIco(t *testing.T) {
	s, a := newSaveServer(t)
	a.AllowPrivateFetchesForTest()
	origin := httptest.NewServer(http.NotFoundHandler()) // the page fetch fails
	t.Cleanup(origin.Close)
	idFrom(t, save(t, s, s.Alice, origin.URL+"/gone"))

	items, err := s.Store.List(context.Background(), s.Alice.User.ID, later.StateUnread, 0, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("List = %v, %v", items, err)
	}
	if want := webfetch.URLHash(origin.URL + "/favicon.ico"); items[0].FaviconHash != want {
		t.Errorf("FaviconHash = %q, want the hash of %s/favicon.ico (%q)", items[0].FaviconHash, origin.URL, want)
	}
}

func TestFaviconRouteRejectsBadHashesAndOtherUsers(t *testing.T) {
	s, _, o, hash := savedWithFavicon(t)

	for _, h := range []string{"..%2Fx", strings.Repeat("A", 32), strings.Repeat("a", 31)} {
		if rec := getFavicon(s, s.Alice, h); rec.Code != 404 {
			t.Errorf("%q: status = %d, want 404", h, rec.Code)
		}
	}
	if rec := getFavicon(s, s.Bob, hash); rec.Code != 404 {
		t.Errorf("bob: status = %d, want 404", rec.Code)
	}
	if o.hits.Load() != 0 {
		t.Errorf("origin hit for a bad request: %d", o.hits.Load())
	}
}
