package webfetch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// A 1x1 transparent PNG.
var tinyPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82")

func TestGetImageSniffsTheContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html") // a lie; sniffing wins
		_, _ = w.Write(tinyPNG)
	}))
	defer srv.Close()

	ct, body, err := testClient().GetImage(context.Background(), srv.URL, webfetch.MaxImageBytes)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "image/png" || len(body) != len(tinyPNG) {
		t.Errorf("got %q, %d bytes; want image/png, %d bytes", ct, len(body), len(tinyPNG))
	}
}

func TestGetImageRefusesSomethingThatIsNotAnImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png") // a lie the other way
		_, _ = w.Write([]byte("<html><script>alert(1)</script></html>"))
	}))
	defer srv.Close()

	if _, _, err := testClient().GetImage(context.Background(), srv.URL, webfetch.MaxImageBytes); err == nil {
		t.Fatal("HTML was accepted as an image")
	}
}

func TestGivenUp(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		errors int
		last   time.Time
		want   bool
	}{
		{"never failed", 0, time.Time{}, false},
		{"failed inside backoff", 1, now.Add(-time.Minute), true},
		{"failed outside backoff", 1, now.Add(-webfetch.ImageRetryBackoff - time.Second), false},
		{"hit the attempt cap", webfetch.MaxImageAttempts, now.Add(-48 * time.Hour), true},
	} {
		if got := webfetch.GivenUp(tc.errors, tc.last, now); got != tc.want {
			t.Errorf("%s: GivenUp = %v, want %v", tc.name, got, tc.want)
		}
	}
}
