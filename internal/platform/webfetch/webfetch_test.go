package webfetch_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

func newTestClient() *webfetch.Client {
	return webfetch.New(webfetch.Config{UserAgent: "test", DefaultMaxBytes: 5 << 20})
}

// testClient is a client that may talk to httptest, which listens on
// 127.0.0.1 — an address the real client refuses by construction. Injecting
// the guard is what makes both this and TestDefaultClientRefusesPrivateAddresses
// possible; a package-level guard would allow only one of them.
func testClient() *webfetch.Client {
	c := newTestClient()
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

	got, err := testClient().Get(context.Background(), srv.URL, webfetch.GetOptions{})
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

	got, err := testClient().Get(context.Background(), srv.URL, webfetch.GetOptions{
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

// An oversized body must be a hard error, not a silently truncated success.
// io.LimitReader alone returns exactly the cap's worth of bytes with a nil
// error whether the real body was that size or ten times that size, which
// for an image would mean caching a truncated file as if it were whole and
// valid, forever, since nothing ever revalidates it.
func TestGetRejectsAnOversizedBody(t *testing.T) {
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

	_, err := testClient().Get(context.Background(), srv.URL, webfetch.GetOptions{MaxBytes: 1024})
	if err == nil {
		t.Fatal("Get: want an error for a body over the cap, got nil")
	}
}

func TestGetRefusesTooManyRedirects(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/next", http.StatusFound)
	}))
	defer srv.Close()

	if _, err := testClient().Get(context.Background(), srv.URL, webfetch.GetOptions{}); err == nil {
		t.Fatal("a redirect loop returned no error")
	}
}

func TestGetRefusesANonHTTPScheme(t *testing.T) {
	if _, err := testClient().Get(context.Background(), "file:///etc/passwd", webfetch.GetOptions{}); err == nil {
		t.Fatal("file:// was accepted")
	}
}

// This is the test that would silently stop meaning anything if DenyAddr were
// package-level and the tests overrode it: it uses the DEFAULT client.
func TestDefaultClientRefusesPrivateAddresses(t *testing.T) {
	c := newTestClient()

	for _, target := range []string{
		"http://127.0.0.1:8080/admin",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://192.168.1.1/",
		"http://[::1]:8080/",
	} {
		t.Run(target, func(t *testing.T) {
			_, err := c.Get(context.Background(), target, webfetch.GetOptions{})
			if err == nil {
				t.Fatalf("%s was fetched; the SSRF guard must refuse it", target)
			}
			if !errors.Is(err, webfetch.ErrBlockedAddress) {
				t.Errorf("error = %v, want ErrBlockedAddress", err)
			}
		})
	}
}

// configClient is a permissive client built from an explicit Config, so the
// Config-sourced defaults can be pinned one at a time.
func configClient(cfg webfetch.Config) *webfetch.Client {
	c := webfetch.New(cfg)
	c.DenyAddr = func(string) error { return nil }
	return c
}

func TestGetSendsTheConfiguredUserAgent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
	}))
	defer srv.Close()

	c := configClient(webfetch.Config{UserAgent: "onsuite/test-agent", DefaultMaxBytes: 1024})
	if _, err := c.Get(context.Background(), srv.URL, webfetch.GetOptions{}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "onsuite/test-agent" {
		t.Errorf("User-Agent = %q, want onsuite/test-agent", got)
	}
}

func TestGetAcceptHeader(t *testing.T) {
	tests := []struct {
		name          string
		defaultAccept string
		optAccept     string
		want          string
	}{
		{"default applies when option is empty", "text/default", "", "text/default"},
		{"explicit option overrides the default", "text/default", "text/explicit", "text/explicit"},
		{"no default and no option sends none", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get("Accept")
			}))
			defer srv.Close()

			c := configClient(webfetch.Config{UserAgent: "test", DefaultAccept: tt.defaultAccept, DefaultMaxBytes: 1024})
			if _, err := c.Get(context.Background(), srv.URL, webfetch.GetOptions{Accept: tt.optAccept}); err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got != tt.want {
				t.Errorf("Accept = %q, want %q", got, tt.want)
			}
		})
	}
}

func bodyServer(n int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", n)))
	}))
}

func TestGetDefaultMaxBytesCapsWhenOptionIsZero(t *testing.T) {
	const limit = 1024
	c := configClient(webfetch.Config{UserAgent: "test", DefaultMaxBytes: limit})

	ok := bodyServer(limit)
	defer ok.Close()
	if _, err := c.Get(context.Background(), ok.URL, webfetch.GetOptions{}); err != nil {
		t.Errorf("a body of exactly DefaultMaxBytes failed: %v", err)
	}

	over := bodyServer(limit + 1)
	defer over.Close()
	if _, err := c.Get(context.Background(), over.URL, webfetch.GetOptions{}); err == nil {
		t.Error("a body of DefaultMaxBytes+1 succeeded; the default cap is not applied")
	}
}

func TestGetFallsBackToMaxPageBytes(t *testing.T) {
	c := configClient(webfetch.Config{UserAgent: "test"})

	over := bodyServer(webfetch.MaxPageBytes + 1)
	defer over.Close()
	if _, err := c.Get(context.Background(), over.URL, webfetch.GetOptions{}); err == nil {
		t.Error("a body of MaxPageBytes+1 succeeded with no configured cap")
	}
}
