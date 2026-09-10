package reader_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/apptest"
)

// onePNG is the smallest thing http.DetectContentType calls an image/png.
var onePNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
}

// newServerWithApp is newServer, but it also hands back the App.
//
// The proxy's fetches go to an httptest origin on 127.0.0.1, which R1's SSRF
// guard refuses by construction — the same problem fetch_test.go solved by
// making DenyAddr a field. These tests need a handle on the App to relax it,
// and apptest.Server deliberately does not expose one: it is shared by three
// apps and none of the others needs it.
func newServerWithApp(t *testing.T) (*apptest.Server[*reader.Store], *reader.App) {
	t.Helper()
	a := reader.New()
	return apptest.NewServer(t, a, reader.NewStore), a
}

// seedImage records an image against a real article and returns its hash.
func seedImage(t *testing.T, s *apptest.Server[*reader.Store], srcURL string) string {
	t.Helper()
	ctx := context.Background()

	sub, err := s.Store.Subscribe(ctx, s.Alice.User.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	hash := reader.ImageHash(srcURL)
	now := time.Now().UTC()
	if _, err := s.Store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{{
		GUID: "g1", Title: "With an image", PublishedAt: now.Add(-time.Hour),
		ContentHTML: `<img src="/reader/img/` + hash + `">`,
		Images:      map[string]string{hash: srcURL},
	}}, now); err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestImageProxyFetchesCachesAndServes(t *testing.T) {
	s, a := newServerWithApp(t)
	var hits int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(onePNG)
	}))
	defer origin.Close()

	hash := seedImage(t, s, origin.URL+"/a.png")
	a.AllowPrivateFetchesForTest()

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/img/"+hash, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("first request returned %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("nosniff header missing")
	}

	rec2 := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/img/"+hash, nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("second request returned %d", rec2.Code)
	}
	if hits != 1 {
		t.Errorf("origin was hit %d times; the second view must come from cache", hits)
	}
}

func TestImageProxyRefusesAnUnknownHash(t *testing.T) {
	s := newServer(t)

	// A well-formed hash of a URL no feed ever delivered. This is the whole
	// security model: the proxy takes a hash, not a URL.
	hash := reader.ImageHash("http://169.254.169.254/latest/meta-data/")
	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/img/"+hash, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown hash returned %d, want 404", rec.Code)
	}
}

func TestImageProxyRejectsNonImageContent(t *testing.T) {
	s, a := newServerWithApp(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png") // lying
		_, _ = w.Write([]byte("<html><body>not an image</body></html>"))
	}))
	defer origin.Close()

	hash := seedImage(t, s, origin.URL+"/a.png")
	a.AllowPrivateFetchesForTest()

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/img/"+hash, nil))
	if rec.Code == http.StatusOK {
		t.Errorf("HTML served as an image because the origin claimed image/png; sniff, do not trust")
	}
}

func TestImageProxyIsBehindAuth(t *testing.T) {
	s := newServer(t)
	hash := seedImage(t, s, "https://cdn.example/a.png")

	rec := s.Do(t, s.Anonymous(t), httptest.NewRequest(http.MethodGet, "/reader/img/"+hash, nil))
	if rec.Code == http.StatusOK {
		t.Error("anonymous request served an image")
	}
}

func TestImageProxyRejectsAMalformedHash(t *testing.T) {
	s := newServer(t)
	for _, bad := range []string{"../../etc/passwd", "zzzz", strings.Repeat("a", 200)} {
		rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/img/"+bad, nil))
		if rec.Code == http.StatusOK {
			t.Errorf("malformed hash %q returned 200", bad)
		}
	}
}
