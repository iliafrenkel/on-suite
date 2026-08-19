package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/iliafrenkel/on-suite/internal/ui"
)

func testAssets(t *testing.T) *Assets {
	t.Helper()
	a, err := NewAssets(fstest.MapFS{
		"app.css":          {Data: []byte("body{color:red}")},
		"htmx.min.js":      {Data: []byte("/* htmx */")},
		"icons/sprite.svg": {Data: []byte("<svg></svg>")},
	}, "/static")
	if err != nil {
		t.Fatalf("NewAssets: %v", err)
	}
	return a
}

func TestAssetURLIsContentHashed(t *testing.T) {
	a := testAssets(t)

	got := a.URL("app.css")
	if !regexp.MustCompile(`^/static/app\.css\?v=[0-9a-f]{8}$`).MatchString(got) {
		t.Errorf("URL(app.css) = %q, want /static/app.css?v=<8 hex>", got)
	}
	if a.URL("app.css") != got {
		t.Error("URL is not stable across calls")
	}
	if a.URL("htmx.min.js") == got {
		t.Error("different files produced the same URL")
	}
	if sub := a.URL("icons/sprite.svg"); !strings.HasPrefix(sub, "/static/icons/sprite.svg?v=") {
		t.Errorf("nested asset URL = %q", sub)
	}
	// A leading slash is tolerated, because templates are written both ways.
	if a.URL("/app.css") != got {
		t.Error("URL should tolerate a leading slash")
	}
}

// TestAssetURLOfUnknownFileFailsVisibly: a typo in a template must produce a
// 404 you notice, not a silently broken page.
func TestAssetURLOfUnknownFileFailsVisibly(t *testing.T) {
	a := testAssets(t)
	got := a.URL("nope.css")
	if strings.Contains(got, "?v=") {
		t.Errorf("URL(nope.css) = %q, want no version for an unknown file", got)
	}

	rec := httptest.NewRecorder()
	serve(a).ServeHTTP(rec, httptest.NewRequest("GET", got, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// serve mounts the handler the way buildStack does, so tests exercise the
// real prefix-stripping arrangement.
func serve(a *Assets) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static", a.Handler()))
	return mux
}

func TestAssetHandlerServesContentAndType(t *testing.T) {
	a := testAssets(t)
	rec := httptest.NewRecorder()
	serve(a).ServeHTTP(rec, httptest.NewRequest("GET", a.URL("app.css"), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != "body{color:red}" {
		t.Errorf("body = %q", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", ct)
	}
}

// TestAssetCachingPolicy is the point of the whole hashing scheme: a
// versioned URL is immutable for a year, a bare one must revalidate.
func TestAssetCachingPolicy(t *testing.T) {
	a := testAssets(t)
	h := serve(a)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", a.URL("app.css"), nil))
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("versioned URL Cache-Control = %q, want immutable", cc)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/static/app.css", nil))
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("bare URL Cache-Control = %q, want no-cache", cc)
	}

	// A stale version string must not be served as immutable.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/static/app.css?v=00000000", nil))
	if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "immutable") {
		t.Errorf("stale version Cache-Control = %q, must not be immutable", cc)
	}
}

func TestAssetHandlerRevalidatesWithETag(t *testing.T) {
	a := testAssets(t)
	h := serve(a)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", a.URL("app.css"), nil))
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag was set")
	}

	req := httptest.NewRequest("GET", a.URL("app.css"), nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Errorf("status = %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("304 response carried %d bytes of body", rec.Body.Len())
	}
}

func TestNewAssetsRejectsBadInput(t *testing.T) {
	if _, err := NewAssets(fstest.MapFS{}, "/static"); err == nil {
		t.Error("NewAssets accepted an empty filesystem")
	}
	if _, err := NewAssets(fstest.MapFS{"a.css": {Data: []byte("x")}}, "static"); err == nil {
		t.Error("NewAssets accepted a prefix without a leading slash")
	}
}

// TestRealAssetsAreEmbedded checks the actual embedded tree, so a missing
// embed directive or a deleted file fails the build rather than the page.
//
// The word "go:embed" is deliberately not written at the start of a comment
// line here: the toolchain and staticcheck both read "// go:embed" as a
// malformed directive (SA9009).
func TestRealAssetsAreEmbedded(t *testing.T) {
	a, err := NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatalf("NewAssets on the embedded tree: %v", err)
	}
	for _, want := range []string{"app.css", "htmx.min.js"} {
		if !strings.Contains(strings.Join(a.Names(), ","), want) {
			t.Errorf("%s is not embedded; got %v", want, a.Names())
		}
	}
}
