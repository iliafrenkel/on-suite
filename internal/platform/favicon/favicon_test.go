package favicon_test

import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/favicon"
)

func TestDiscoverFindsAnIconLink(t *testing.T) {
	page := []byte(`<html><head>
	<link rel="stylesheet" href="/s.css">
	<link rel="icon" href="/static/icon.png">
	</head></html>`)

	got := favicon.Discover(page, "https://blog.example/")
	if got != "https://blog.example/static/icon.png" {
		t.Errorf("got %q, want the discovered icon URL", got)
	}
}

func TestDiscoverFindsAShortcutIconLink(t *testing.T) {
	page := []byte(`<html><head>
	<link rel="shortcut icon" href="/favicon.png">
	</head></html>`)

	got := favicon.Discover(page, "https://blog.example/")
	if got != "https://blog.example/favicon.png" {
		t.Errorf("got %q, want the shortcut icon URL", got)
	}
}

// apple-touch-icon is a single rel token, not "icon" plus something else, so
// it must not match — this app wants a small favicon-shaped image, not the
// large icon iOS home-screen bookmarks use.
func TestDiscoverIgnoresAppleTouchIcon(t *testing.T) {
	page := []byte(`<html><head>
	<link rel="apple-touch-icon" href="/apple-touch-icon.png">
	</head></html>`)

	got := favicon.Discover(page, "https://blog.example/")
	if got != "https://blog.example/favicon.ico" {
		t.Errorf("got %q, want the /favicon.ico fallback, not the apple-touch-icon", got)
	}
}

func TestDiscoverResolvesRelativeHrefs(t *testing.T) {
	got := favicon.Discover(
		[]byte(`<link rel="icon" href="icon.ico">`),
		"https://blog.example/posts/index.html")
	if got != "https://blog.example/posts/icon.ico" {
		t.Errorf("got %q, want the href resolved against the page URL", got)
	}
}

func TestDiscoverFallsBackWithNoPageHTML(t *testing.T) {
	got := favicon.Discover(nil, "https://blog.example/some/deep/page")
	if got != "https://blog.example/favicon.ico" {
		t.Errorf("got %q, want the origin's /favicon.ico guess", got)
	}
}

func TestDiscoverFallsBackWhenNoLinkMatches(t *testing.T) {
	got := favicon.Discover([]byte(`<html><head><title>No icon here</title></head></html>`), "https://blog.example/")
	if got != "https://blog.example/favicon.ico" {
		t.Errorf("got %q, want the /favicon.ico fallback", got)
	}
}

// The /favicon.ico fallback is built by copying the site URL and swapping
// its path, so it must not carry over userinfo (a "user:pass@" prefix) that
// happened to be present on the site URL — that would land credentials in
// reader_feed_icons.src_url and potentially in logs on a later fetch
// failure.
func TestDiscoverFallbackStripsUserinfo(t *testing.T) {
	got := favicon.Discover(nil, "https://u:p@blog.example/")
	if strings.Contains(got, "u:p@") {
		t.Errorf("got %q, fallback favicon URL must not carry userinfo", got)
	}
	if got != "https://blog.example/favicon.ico" {
		t.Errorf("got %q, want https://blog.example/favicon.ico", got)
	}
}

func TestDiscoverReturnsEmptyForAnUnparsableSiteURL(t *testing.T) {
	got := favicon.Discover(nil, "://not a url")
	if got != "" {
		t.Errorf("got %q, want empty for an unparsable site URL", got)
	}
}

func TestDiscoverRefusesAHostileHref(t *testing.T) {
	got := favicon.Discover(
		[]byte(`<link rel="icon" href="javascript:alert(1)">`),
		"https://blog.example/")
	if got != "https://blog.example/favicon.ico" {
		t.Errorf("got %q, want the fallback rather than a javascript: URL", got)
	}
}
