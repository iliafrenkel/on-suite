package reader_test

import (
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

const blogPage = `<html><head>
<title>Some Blog</title>
<link rel="alternate" type="application/rss+xml" title="Some Blog &raquo; Feed" href="/feed/">
<link rel="alternate" type="application/rss+xml" title="Some Blog &raquo; Comments Feed" href="/comments/feed/">
<link rel="stylesheet" href="/style.css">
</head><body><p>Hello</p></body></html>`

func TestFeedsInPageFindsAdvertisedFeeds(t *testing.T) {
	got := reader.FeedsInPage([]byte(blogPage), "https://blog.example/")
	if len(got) != 2 {
		t.Fatalf("found %d candidates, want 2: %+v", len(got), got)
	}
	if got[0].URL != "https://blog.example/feed/" {
		t.Errorf("first candidate URL = %q, want the absolute main feed", got[0].URL)
	}
}

// WordPress advertises a comments feed beside the real one. Subscribing
// someone to comments because it came second in the HTML would be a bad
// default that is annoying to notice and annoying to undo.
func TestFeedsInPageRanksTheCommentsFeedLast(t *testing.T) {
	got := reader.FeedsInPage([]byte(blogPage), "https://blog.example/")
	if len(got) < 2 {
		t.Fatal("need both candidates for this test")
	}
	if got[0].URL != "https://blog.example/feed/" {
		t.Errorf("comments feed ranked first: %+v", got)
	}
}

func TestFeedsInPageAcceptsAtomAndRelativeHrefs(t *testing.T) {
	got := reader.FeedsInPage([]byte(`<html><head>
	<link rel="alternate" type="application/atom+xml" href="atom.xml">
	</head></html>`), "https://blog.example/posts/index.html")

	if len(got) != 1 {
		t.Fatalf("found %d candidates, want 1: %+v", len(got), got)
	}
	if got[0].URL != "https://blog.example/posts/atom.xml" {
		t.Errorf("relative href resolved to %q", got[0].URL)
	}
}

func TestFeedsInPageIgnoresNonFeedLinks(t *testing.T) {
	got := reader.FeedsInPage([]byte(`<html><head>
	<link rel="stylesheet" href="/s.css">
	<link rel="alternate" type="text/html" hreflang="fr" href="/fr/">
	<link rel="icon" href="/favicon.ico">
	</head></html>`), "https://blog.example/")

	if len(got) != 0 {
		t.Errorf("found %d candidates in a page with no feeds: %+v", len(got), got)
	}
}

// A discovered href must be http(s) and absolute after resolution, for the
// same reason an image src must be: it is about to be handed to a fetcher.
func TestFeedsInPageRefusesHostileHrefs(t *testing.T) {
	got := reader.FeedsInPage([]byte(`<html><head>
	<link rel="alternate" type="application/rss+xml" href="javascript:alert(1)">
	<link rel="alternate" type="application/rss+xml" href="data:text/xml,<rss/>">
	<link rel="alternate" type="application/rss+xml" href="file:///etc/passwd">
	</head></html>`), "https://blog.example/")

	if len(got) != 0 {
		t.Errorf("accepted a non-http candidate: %+v", got)
	}
}
