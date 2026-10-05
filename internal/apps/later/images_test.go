package later_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// imageOrigin serves a PNG at /pic.png, HTML at /nope.png, and the article
// at /essay, counting hits on the two image paths.
type imageOrigin struct {
	*httptest.Server
	pic, nope atomic.Int32
}

func newImageOrigin(t *testing.T) *imageOrigin {
	t.Helper()
	o := &imageOrigin{}
	pic := tinyPNG(t)
	page := strings.Replace(articlePage, `<img src="/pic.png">`, `<img src="/pic.png"><img src="/nope.png">`, 1)
	o.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pic.png":
			o.pic.Add(1)
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pic)
		case "/nope.png":
			o.nope.Add(1)
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<html>not an image</html>"))
		default:
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(page))
		}
	}))
	t.Cleanup(o.Close)
	return o
}

// savedWithImages saves the origin's article as Alice and returns the hashes.
func savedWithImages(t *testing.T) (s *server, a *later.App, o *imageOrigin, picHash, nopeHash string) {
	t.Helper()
	s, a = newSaveServer(t)
	a.AllowPrivateFetchesForTest()
	o = newImageOrigin(t)
	idFrom(t, save(t, s, s.Alice, o.URL+"/essay"))
	return s, a, o, webfetch.URLHash(o.URL + "/pic.png"), webfetch.URLHash(o.URL + "/nope.png")
}

func getImage(s *server, sess *apptest.Session, hash string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/later/img/"+hash, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	for _, c := range sess.Cookies {
		req.AddCookie(c)
	}
	s.Handler.ServeHTTP(rec, req)
	return rec
}

func TestImageRouteFetchesStoresAndServes(t *testing.T) {
	s, _, o, pic, _ := savedWithImages(t)

	rec := getImage(s, s.Alice, pic)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
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
	if cc := rec.Header().Get("Cache-Control"); cc != "private, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", cc)
	}
	if !bytes.Equal(rec.Body.Bytes(), tinyPNG(t)) {
		t.Error("body is not the origin's image")
	}
	if o.pic.Load() != 1 {
		t.Fatalf("origin hits = %d, want 1", o.pic.Load())
	}
	if rec := getImage(s, s.Alice, pic); rec.Code != 200 {
		t.Fatalf("second status = %d", rec.Code)
	}
	if o.pic.Load() != 1 {
		t.Errorf("origin hits after second GET = %d, want 1 (served from the store)", o.pic.Load())
	}
}

func TestImageRouteRefusesNonImages(t *testing.T) {
	s, _, _, _, nope := savedWithImages(t)

	if rec := getImage(s, s.Alice, nope); rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	img, err := s.Store.ImageForUser(context.Background(), s.Alice.User.ID, nope)
	if err != nil {
		t.Fatal(err)
	}
	if img.ErrorCount != 1 || img.Cached() {
		t.Errorf("ErrorCount = %d, cached = %v; want 1, false", img.ErrorCount, img.Cached())
	}
}

func TestImageRouteHonoursTheBackoff(t *testing.T) {
	s, _, o, _, nope := savedWithImages(t)

	getImage(s, s.Alice, nope)
	if o.nope.Load() != 1 {
		t.Fatalf("origin hits = %d, want 1", o.nope.Load())
	}
	if rec := getImage(s, s.Alice, nope); rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if o.nope.Load() != 1 {
		t.Errorf("origin hit during backoff: %d", o.nope.Load())
	}
	s.Clock.Advance(2 * time.Hour)
	if rec := getImage(s, s.Alice, nope); rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if o.nope.Load() != 2 {
		t.Errorf("origin hits after backoff = %d, want 2", o.nope.Load())
	}
}

func TestImageRouteIsScopedToTheOwner(t *testing.T) {
	s, _, o, pic, _ := savedWithImages(t)

	if rec := getImage(s, s.Bob, pic); rec.Code != 404 {
		t.Fatalf("bob status = %d, want 404", rec.Code)
	}
	if o.pic.Load() != 0 {
		t.Errorf("origin hit for a non-owner: %d", o.pic.Load())
	}
}

func TestImageRouteRejectsBadHashes(t *testing.T) {
	s, _ := newSaveServer(t)
	for _, h := range []string{"..%2Fx", strings.Repeat("A", 32), strings.Repeat("a", 31)} {
		if rec := getImage(s, s.Alice, h); rec.Code != 404 {
			t.Errorf("%q: status = %d, want 404", h, rec.Code)
		}
	}
}

func TestImageRouteAnswers304ForAMatchingETag(t *testing.T) {
	s, _, _, pic, _ := savedWithImages(t)

	first := getImage(s, s.Alice, pic)
	rec := getImage(s, s.Alice, pic, "If-None-Match", first.Header().Get("ETag"))
	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Error("304 carried a body")
	}
}

func TestDownloadImagesBackFillsAndKeepsImagesForever(t *testing.T) {
	s, a, _, pic, nope := savedWithImages(t)
	ctx := context.Background()

	n, err := a.DownloadImages(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("stored = %d, want 1", n)
	}
	img, err := s.Store.ImageForUser(ctx, s.Alice.User.ID, nope)
	if err != nil {
		t.Fatal(err)
	}
	if img.ErrorCount != 1 {
		t.Errorf("nope ErrorCount = %d, want 1", img.ErrorCount)
	}

	s.Clock.Advance(365 * 24 * time.Hour)
	if rec := getImage(s, s.Alice, pic); rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), tinyPNG(t)) {
		t.Errorf("image after a year: status %d", rec.Code)
	}
}
