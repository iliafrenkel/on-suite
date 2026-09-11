package reader_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseFeedRSS2(t *testing.T) {
	got, err := reader.ParseFeed(fixture(t, "rss2.xml"), "https://example.com/feed.xml")
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if got.Title != "Example Blog" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.SiteURL != "https://example.com/" {
		t.Errorf("SiteURL = %q", got.SiteURL)
	}
	if len(got.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(got.Items))
	}

	it := got.Items[0]
	if it.GUID != "tag:example.com,2026:1" {
		t.Errorf("GUID = %q; the publisher's guid must win", it.GUID)
	}
	if it.URL != "https://example.com/first" {
		t.Errorf("URL = %q", it.URL)
	}
	if it.PublishedAt.IsZero() {
		t.Error("PublishedAt is zero; RFC1123Z with a +1000 offset must parse")
	}
	if strings.Contains(it.SummaryHTML, "<script") {
		t.Errorf("summary was not sanitized: %s", it.SummaryHTML)
	}
	if strings.Contains(it.ContentHTML, "tracker.example") {
		t.Errorf("publisher host survived rewriting: %s", it.ContentHTML)
	}
	if !strings.Contains(it.ContentHTML, "/reader/img/") {
		t.Errorf("image was not proxied: %s", it.ContentHTML)
	}
	if !strings.Contains(it.ContentHTML, "The full body.") {
		t.Errorf("content:encoded was not used: %s", it.ContentHTML)
	}
}

func TestParseFeedAtom(t *testing.T) {
	got, err := reader.ParseFeed(fixture(t, "atom.xml"), "https://atom.example/feed")
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(got.Items))
	}
	if got.Items[0].GUID != "urn:uuid:1225c695-cfb8-4ebb-aaaa-80da344efa6a" {
		t.Errorf("GUID = %q", got.Items[0].GUID)
	}
	if !strings.Contains(got.Items[0].ContentHTML, "Atom body.") {
		t.Errorf("ContentHTML = %q", got.Items[0].ContentHTML)
	}
}

func TestParseFeedSynthesisesAStableGUID(t *testing.T) {
	body := fixture(t, "noguid.xml")

	first, err := reader.ParseFeed(body, "https://noguid.example/feed")
	if err != nil {
		t.Fatal(err)
	}
	second, err := reader.ParseFeed(body, "https://noguid.example/feed")
	if err != nil {
		t.Fatal(err)
	}

	if first.Items[0].GUID == "" {
		t.Fatal("no GUID synthesised; the item could never be deduplicated")
	}
	if first.Items[0].GUID != second.Items[0].GUID {
		t.Errorf("synthesised GUID is unstable: %q then %q; every poll would re-insert the item",
			first.Items[0].GUID, second.Items[0].GUID)
	}
}

func TestParseFeedRejectsNonFeedBytes(t *testing.T) {
	if _, err := reader.ParseFeed([]byte("<html><body>not a feed</body></html>"), "https://x.example/"); err == nil {
		t.Fatal("an HTML page parsed as a feed")
	}
}
