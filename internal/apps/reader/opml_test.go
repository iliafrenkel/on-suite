package reader_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

func TestParseOPML(t *testing.T) {
	got, err := reader.ParseOPML(fixture(t, "subscriptions.opml"))
	if err != nil {
		t.Fatalf("ParseOPML: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("parsed %d entries, want 4: %+v", len(got), got)
	}

	byURL := map[string]reader.OPMLEntry{}
	for _, e := range got {
		byURL[e.FeedURL] = e
	}

	loose, ok := byURL["https://loose.example/feed.xml"]
	if !ok {
		t.Fatal("the root-level feed was not parsed")
	}
	if loose.Folder != "" {
		t.Errorf("root feed got folder %q, want none", loose.Folder)
	}
	if loose.Title != "Loose Feed" {
		t.Errorf("Title = %q", loose.Title)
	}

	a := byURL["https://a.example/feed.xml"]
	if a.Folder != "Tech" {
		t.Errorf("Feed A folder = %q, want Tech", a.Folder)
	}

	// title= is the OPML 1.0 spelling; plenty of exporters still emit it.
	if b := byURL["https://b.example/atom.xml"]; b.Title != "Feed B" {
		t.Errorf("title= attribute ignored: Title = %q", b.Title)
	}

	// An entry with no title at all is still a usable subscription.
	if _, ok := byURL["https://c.example/rss"]; !ok {
		t.Error("an outline with no text or title was dropped")
	}
}

// Real exports from readers that allow deeper nesting must not lose feeds just
// because this app stops at one level.
func TestParseOPMLFlattensDeepNesting(t *testing.T) {
	got, err := reader.ParseOPML(fixture(t, "deep.opml"))
	if err != nil {
		t.Fatalf("ParseOPML: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("parsed %d entries, want 2: %+v", len(got), got)
	}
	for _, e := range got {
		if e.Folder != "Top" {
			t.Errorf("%s landed in folder %q; everything below the first level flattens into it",
				e.FeedURL, e.Folder)
		}
	}
}

func TestParseOPMLRejectsNonOPML(t *testing.T) {
	for name, data := range map[string]string{
		"empty":     ``,
		"html":      `<html><body>not opml</body></html>`,
		"feed":      `<rss version="2.0"><channel><title>x</title></channel></rss>`,
		"truncated": `<opml version="2.0"><body><outline`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := reader.ParseOPML([]byte(data)); !errors.Is(err, reader.ErrNotOPML) {
				t.Errorf("err = %v, want ErrNotOPML", err)
			}
		})
	}
}

// An outline with no xmlUrl is a folder or a bookmark, not a subscription.
func TestParseOPMLSkipsEntriesWithNoFeedURL(t *testing.T) {
	got, err := reader.ParseOPML([]byte(`<opml version="2.0"><body>
		<outline type="rss" text="No URL"/>
		<outline type="link" text="A bookmark" url="https://example.com/"/>
		<outline type="rss" text="Real" xmlUrl="https://real.example/feed"/>
	</body></opml>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FeedURL != "https://real.example/feed" {
		t.Errorf("got %+v, want only the entry with an xmlUrl", got)
	}
}

func TestBuildOPMLRoundTrips(t *testing.T) {
	tree := reader.Tree{
		Folders: []reader.TreeFolder{{
			Folder: reader.Folder{ID: 1, Name: "Tech"},
			Subs: []reader.Subscription{
				{ID: 10, FeedURL: "https://a.example/feed.xml", FeedName: "Feed A", SiteURL: "https://a.example/"},
			},
		}},
		Root: []reader.Subscription{
			{ID: 20, FeedURL: "https://loose.example/feed.xml", FeedName: "Loose Feed"},
		},
	}

	data, err := reader.BuildOPML(tree, "ON Reader subscriptions")
	if err != nil {
		t.Fatalf("BuildOPML: %v", err)
	}
	if !strings.HasPrefix(string(data), "<?xml") {
		t.Errorf("output has no XML declaration:\n%s", data)
	}

	back, err := reader.ParseOPML(data)
	if err != nil {
		t.Fatalf("our own output does not parse: %v\n%s", err, data)
	}
	if len(back) != 2 {
		t.Fatalf("round-tripped %d entries, want 2: %+v", len(back), back)
	}
	byURL := map[string]reader.OPMLEntry{}
	for _, e := range back {
		byURL[e.FeedURL] = e
	}
	if byURL["https://a.example/feed.xml"].Folder != "Tech" {
		t.Error("folder was lost in the round trip")
	}
	if byURL["https://loose.example/feed.xml"].Folder != "" {
		t.Error("a root feed gained a folder in the round trip")
	}
}

// A title with markup in it must not be able to break out of the document.
func TestBuildOPMLEscapes(t *testing.T) {
	data, err := reader.BuildOPML(reader.Tree{
		Root: []reader.Subscription{{
			FeedURL:  "https://x.example/feed?a=1&b=2",
			FeedName: `Ampersands & "quotes" <tags>`,
		}},
	}, "T")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "<tags>") {
		t.Errorf("a title's markup reached the document unescaped:\n%s", data)
	}
	if _, err := reader.ParseOPML(data); err != nil {
		t.Errorf("escaping produced an unparseable document: %v\n%s", err, data)
	}
}
