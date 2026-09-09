package reader_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

// testClient is a client that may talk to httptest, which listens on
// 127.0.0.1 — an address the real client refuses by construction. Injecting
// the guard is what makes both this and TestDefaultClientRefusesPrivateAddresses
// possible; a package-level guard would allow only one of them.
func testClient() *reader.Client {
	c := reader.NewClient("test")
	c.DenyAddr = func(string) error { return nil }
	return c
}

func TestGetReturnsBodyAndValidators(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc"`)
		w.Header().Set("Last-Modified", "Wed, 09 Sep 2026 00:00:00 GMT")
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte("<rss></rss>"))
	}))
	defer srv.Close()

	got, err := testClient().Get(context.Background(), srv.URL, reader.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Body) != "<rss></rss>" {
		t.Errorf("Body = %q", got.Body)
	}
	if got.ETag != `"abc"` {
		t.Errorf("ETag = %q", got.ETag)
	}
	if got.LastModified == "" {
		t.Error("Last-Modified not captured")
	}
	if got.NotModified {
		t.Error("NotModified set on a 200")
	}
}

func TestGetSendsConditionalHeadersAndHandles304(t *testing.T) {
	var sawETag, sawModified string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawETag = r.Header.Get("If-None-Match")
		sawModified = r.Header.Get("If-Modified-Since")
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	got, err := testClient().Get(context.Background(), srv.URL, reader.GetOptions{
		ETag:         `"abc"`,
		LastModified: "Wed, 09 Sep 2026 00:00:00 GMT",
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if sawETag != `"abc"` {
		t.Errorf("If-None-Match = %q", sawETag)
	}
	if sawModified == "" {
		t.Error("If-Modified-Since not sent")
	}
	if !got.NotModified {
		t.Error("NotModified not set on a 304")
	}
}

func TestGetTruncatesAnOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Far more than the cap, written without ever allocating it all.
		chunk := strings.Repeat("x", 4096)
		for i := 0; i < 64; i++ {
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	got, err := testClient().Get(context.Background(), srv.URL, reader.GetOptions{MaxBytes: 1024})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Body) > 1024 {
		t.Errorf("body is %d bytes; the cap must bound it at 1024", len(got.Body))
	}
}

func TestGetRefusesTooManyRedirects(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/next", http.StatusFound)
	}))
	defer srv.Close()

	if _, err := testClient().Get(context.Background(), srv.URL, reader.GetOptions{}); err == nil {
		t.Fatal("a redirect loop returned no error")
	}
}

func TestGetRefusesANonHTTPScheme(t *testing.T) {
	if _, err := testClient().Get(context.Background(), "file:///etc/passwd", reader.GetOptions{}); err == nil {
		t.Fatal("file:// was accepted")
	}
}

// This is the test that would silently stop meaning anything if DenyAddr were
// package-level and the tests overrode it: it uses the DEFAULT client.
func TestDefaultClientRefusesPrivateAddresses(t *testing.T) {
	c := reader.NewClient("test")

	for _, target := range []string{
		"http://127.0.0.1:8080/admin",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://192.168.1.1/",
		"http://[::1]:8080/",
	} {
		t.Run(target, func(t *testing.T) {
			_, err := c.Get(context.Background(), target, reader.GetOptions{})
			if err == nil {
				t.Fatalf("%s was fetched; the SSRF guard must refuse it", target)
			}
			if !errors.Is(err, reader.ErrBlockedAddress) {
				t.Errorf("error = %v, want ErrBlockedAddress", err)
			}
		})
	}
}
