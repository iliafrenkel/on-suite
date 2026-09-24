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
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// oneICO is the smallest thing http.DetectContentType calls an image.
var oneICO = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
}

// seedFeedIcon records a favicon row directly (bypassing discovery, which is
// tested separately) and returns its hash.
func seedFeedIcon(t *testing.T, s *apptest.Server[*reader.Store], srcURL string) string {
	t.Helper()
	hash := reader.FaviconHash(srcURL)
	if _, err := s.Store.DB().ExecContext(context.Background(),
		`INSERT INTO reader_feed_icons (url_hash, src_url) VALUES (?, ?)`, hash, srcURL); err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestFaviconProxyFetchesCachesAndServes(t *testing.T) {
	s, a := newServerWithApp(t)
	var hits int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(oneICO)
	}))
	defer origin.Close()

	hash := seedFeedIcon(t, s, origin.URL+"/favicon.ico")
	a.AllowPrivateFetchesForTest()

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/favicon/"+hash, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("first request returned %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}

	rec2 := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/favicon/"+hash, nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("second request returned %d", rec2.Code)
	}
	if hits != 1 {
		t.Errorf("origin was hit %d times; the second view must come from cache", hits)
	}
}

func TestFaviconProxyRefusesAnUnknownHash(t *testing.T) {
	s := newServer(t)

	hash := reader.FaviconHash("http://169.254.169.254/latest/meta-data/")
	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/favicon/"+hash, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown hash returned %d, want 404", rec.Code)
	}
}

// TestFaviconProxyUnknownHashIsABareNotFound guards against the sidebar's
// <img> tags (one per feed, re-requested on nearly every htmx pane swap)
// each triggering a full page-template render for what is, for a feed added
// via a bare URL with a guessed favicon, a routine and frequent outcome —
// not a real error worth an app-shell response.
func TestFaviconProxyUnknownHashIsABareNotFound(t *testing.T) {
	s := newServer(t)

	hash := reader.FaviconHash("http://169.254.169.254/latest/meta-data/")
	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/favicon/"+hash, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown hash returned %d, want 404", rec.Code)
	}
	if body := strings.ToLower(rec.Body.String()); strings.Contains(body, "<html") {
		t.Errorf("unknown-hash 404 body looks like the full app shell: %q", rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc == "" {
		t.Error("unknown-hash 404 has no Cache-Control hint for the browser to avoid re-requesting it")
	}
}

// TestFaviconProxyBackoffWindowIsABareNotFound covers the other half of the
// same finding: a favicon still inside its retry backoff after a prior
// failure must not re-render the full HTML error page either.
func TestFaviconProxyBackoffWindowIsABareNotFound(t *testing.T) {
	s := newServer(t)

	hash := reader.FaviconHash("https://cdn.example/favicon.ico")
	if _, err := s.Store.DB().ExecContext(context.Background(),
		`INSERT INTO reader_feed_icons (url_hash, src_url, error_count, fetched_at, last_error)
		 VALUES (?, ?, 1, ?, 'boom')`,
		hash, "https://cdn.example/favicon.ico", db.FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/favicon/"+hash, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("in-backoff hash returned %d, want 404", rec.Code)
	}
	if body := strings.ToLower(rec.Body.String()); strings.Contains(body, "<html") {
		t.Errorf("in-backoff 404 body looks like the full app shell: %q", rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc == "" {
		t.Error("in-backoff 404 has no Cache-Control hint for the browser to avoid re-requesting it")
	}
}

func TestFaviconProxyRejectsNonImageContent(t *testing.T) {
	s, a := newServerWithApp(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon") // lying
		_, _ = w.Write([]byte("<html><body>not an image</body></html>"))
	}))
	defer origin.Close()

	hash := seedFeedIcon(t, s, origin.URL+"/favicon.ico")
	a.AllowPrivateFetchesForTest()

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/favicon/"+hash, nil))
	if rec.Code == http.StatusOK {
		t.Errorf("HTML served as a favicon because the origin claimed image/x-icon; sniff, do not trust")
	}
}

func TestFaviconProxyIsBehindAuth(t *testing.T) {
	s := newServer(t)
	hash := seedFeedIcon(t, s, "https://cdn.example/favicon.ico")

	rec := s.Do(t, s.Anonymous(t), httptest.NewRequest(http.MethodGet, "/reader/favicon/"+hash, nil))
	if rec.Code == http.StatusOK {
		t.Error("anonymous request served a favicon")
	}
}

func TestFaviconProxyRejectsAMalformedHash(t *testing.T) {
	s := newServer(t)
	for _, bad := range []string{"../../etc/passwd", "zzzz", strings.Repeat("a", 200)} {
		rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/reader/favicon/"+bad, nil))
		if rec.Code == http.StatusOK {
			t.Errorf("malformed hash %q returned 200", bad)
		}
	}
}
