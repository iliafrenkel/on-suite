package reader_test

import (
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

func TestDiscoverFaviconFindsAnIconLink(t *testing.T) {
	page := []byte(`<html><head>
	<link rel="stylesheet" href="/s.css">
	<link rel="icon" href="/static/icon.png">
	</head></html>`)

	got := reader.DiscoverFavicon(page, "https://blog.example/")
	if got != "https://blog.example/static/icon.png" {
		t.Errorf("got %q, want the discovered icon URL", got)
	}
}

func TestDiscoverFaviconFindsAShortcutIconLink(t *testing.T) {
	page := []byte(`<html><head>
	<link rel="shortcut icon" href="/favicon.png">
	</head></html>`)

	got := reader.DiscoverFavicon(page, "https://blog.example/")
	if got != "https://blog.example/favicon.png" {
		t.Errorf("got %q, want the shortcut icon URL", got)
	}
}

// apple-touch-icon is a single rel token, not "icon" plus something else, so
// it must not match — this app wants a small favicon-shaped image, not the
// large icon iOS home-screen bookmarks use.
func TestDiscoverFaviconIgnoresAppleTouchIcon(t *testing.T) {
	page := []byte(`<html><head>
	<link rel="apple-touch-icon" href="/apple-touch-icon.png">
	</head></html>`)

	got := reader.DiscoverFavicon(page, "https://blog.example/")
	if got != "https://blog.example/favicon.ico" {
		t.Errorf("got %q, want the /favicon.ico fallback, not the apple-touch-icon", got)
	}
}

func TestDiscoverFaviconResolvesRelativeHrefs(t *testing.T) {
	got := reader.DiscoverFavicon(
		[]byte(`<link rel="icon" href="icon.ico">`),
		"https://blog.example/posts/index.html")
	if got != "https://blog.example/posts/icon.ico" {
		t.Errorf("got %q, want the href resolved against the page URL", got)
	}
}

func TestDiscoverFaviconFallsBackWithNoPageHTML(t *testing.T) {
	got := reader.DiscoverFavicon(nil, "https://blog.example/some/deep/page")
	if got != "https://blog.example/favicon.ico" {
		t.Errorf("got %q, want the origin's /favicon.ico guess", got)
	}
}

func TestDiscoverFaviconFallsBackWhenNoLinkMatches(t *testing.T) {
	got := reader.DiscoverFavicon([]byte(`<html><head><title>No icon here</title></head></html>`), "https://blog.example/")
	if got != "https://blog.example/favicon.ico" {
		t.Errorf("got %q, want the /favicon.ico fallback", got)
	}
}

func TestDiscoverFaviconReturnsEmptyForAnUnparsableSiteURL(t *testing.T) {
	got := reader.DiscoverFavicon(nil, "://not a url")
	if got != "" {
		t.Errorf("got %q, want empty for an unparsable site URL", got)
	}
}

func TestDiscoverFaviconRefusesAHostileHref(t *testing.T) {
	got := reader.DiscoverFavicon(
		[]byte(`<link rel="icon" href="javascript:alert(1)">`),
		"https://blog.example/")
	if got != "https://blog.example/favicon.ico" {
		t.Errorf("got %q, want the fallback rather than a javascript: URL", got)
	}
}
