package flash_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

// testMediaClient is a client that may talk to httptest, which listens on
// 127.0.0.1 — an address the real client refuses by construction. Injecting
// the guard is what makes both this and
// TestDefaultMediaClientRefusesPrivateAddresses possible; a package-level
// guard would allow only one of them.
func testMediaClient() *flash.MediaClient {
	c := flash.NewMediaClient("test")
	c.DenyAddr = func(string) error { return nil }
	return c
}

func TestMediaClientGetReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fake-image-bytes"))
	}))
	defer srv.Close()

	body, err := testMediaClient().Get(context.Background(), srv.URL, 1<<20)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(body) != "fake-image-bytes" {
		t.Errorf("body = %q", body)
	}
}

// An oversized body must be a hard error, not a silently truncated success —
// a truncated image still sniffs a confident content-type from its leading
// bytes and would otherwise be cached as valid, corrupt, forever.
func TestMediaClientGetRejectsAnOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := strings.Repeat("x", 4096)
		for i := 0; i < 64; i++ {
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	_, err := testMediaClient().Get(context.Background(), srv.URL, 1024)
	if err == nil {
		t.Fatal("Get: want an error for a body over the cap, got nil")
	}
}

func TestMediaClientGetRefusesTooManyRedirects(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/next", http.StatusFound)
	}))
	defer srv.Close()

	if _, err := testMediaClient().Get(context.Background(), srv.URL, 1<<20); err == nil {
		t.Fatal("a redirect loop returned no error")
	}
}

func TestMediaClientGetRefusesANonHTTPScheme(t *testing.T) {
	if _, err := testMediaClient().Get(context.Background(), "file:///etc/passwd", 1<<20); err == nil {
		t.Fatal("file:// was accepted")
	}
}

// This is the test that would silently stop meaning anything if DenyAddr
// were package-level and the tests overrode it: it uses the DEFAULT client.
func TestDefaultMediaClientRefusesPrivateAddresses(t *testing.T) {
	c := flash.NewMediaClient("test")

	for _, target := range []string{
		"http://127.0.0.1:8080/admin",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://192.168.1.1/",
		"http://[::1]:8080/",
	} {
		t.Run(target, func(t *testing.T) {
			_, err := c.Get(context.Background(), target, 1<<20)
			if err == nil {
				t.Fatalf("%s was fetched; the SSRF guard must refuse it", target)
			}
			if !errors.Is(err, flash.ErrBlockedAddress) {
				t.Errorf("error = %v, want ErrBlockedAddress", err)
			}
		})
	}
}

func TestFetchAndCacheMediaSavesBytesOnSuccess(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// A minimal valid JPEG magic-byte prefix so http.DetectContentType sniffs
	// "image/jpeg" rather than "application/octet-stream".
	jpegBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 0, 0, 0, 0}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(jpegBytes)
	}))
	defer srv.Close()

	hash, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	m, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := f.store.FetchAndCacheMedia(ctx, testMediaClient(), m, time.Now().UTC())
	if err != nil {
		t.Fatalf("FetchAndCacheMedia: %v", err)
	}
	if updated.ContentType != "image/jpeg" {
		t.Errorf("ContentType = %q, want image/jpeg", updated.ContentType)
	}

	reloaded, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Cached() {
		t.Error("media should be cached in the store after a successful fetch")
	}
}

func TestFetchAndCacheMediaRejectsContentTypeMismatch(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg") // claimed, but body below is HTML
		_, _ = w.Write([]byte("<html><body>not an image</body></html>"))
	}))
	defer srv.Close()

	hash, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	m, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.FetchAndCacheMedia(ctx, testMediaClient(), m, time.Now().UTC()); err == nil {
		t.Fatal("FetchAndCacheMedia: want an error for sniffed content not matching the media's kind")
	}

	reloaded, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Cached() {
		t.Error("media must not be cached when the content-type check fails")
	}
}
