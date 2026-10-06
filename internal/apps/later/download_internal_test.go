package later

import (
	"testing"
	"time"
)

func TestArticleMarkdown(t *testing.T) {
	saved := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	day := saved.Local().Format("2 January 2006")
	art := Article{
		URL:         "https://essays.example.com/reading-slowly",
		Title:       "Reading *slowly*",
		Byline:      "Mara Lind",
		Content:     ContentExtracted,
		Note:        "Stop skimming.\n\n1. Mark three things",
		ContentHTML: `<h1>Intro</h1><p>Read <em>slowly</em>.</p><p><img src="/later/img/h1" alt="A desk"></p>`,
		SavedAt:     saved,
	}
	hls := []Highlight{
		{Quote: "Read slowly.", Comment: "Yes."},
		{Quote: "1. not a list"},
	}
	sources := map[string]string{"h1": "https://essays.example.com/desk.jpg"}

	want := "# Reading \\*slowly\\*\n\n" +
		"Source: <https://essays.example.com/reading-slowly>\\\n" +
		"Author: Mara Lind\\\n" +
		"Saved: " + day + "\\\n" +
		"Tags: essays, reading\n\n" +
		"## Note\n\n" +
		"Stop skimming.\n\n1. Mark three things\n\n" +
		"## Highlights\n\n" +
		"> Read slowly.\n\n" +
		"Yes.\n\n" +
		"> 1\\. not a list\n\n" +
		"## Article\n\n" +
		"### Intro\n\n" +
		"Read *slowly*.\n\n" +
		"[A desk](https://essays.example.com/desk.jpg)\n"
	if got := articleMarkdown(art, []string{"essays", "reading"}, hls, sources); got != want {
		t.Errorf("articleMarkdown\n got: %q\nwant: %q", got, want)
	}
}

func TestArticleMarkdownLinkOnly(t *testing.T) {
	saved := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	art := Article{URL: "https://news.example.com/libraries", Title: "Libraries", Content: ContentLinkOnly, SavedAt: saved}
	want := "# Libraries\n\n" +
		"Source: <https://news.example.com/libraries>\\\n" +
		"Saved: " + saved.Local().Format("2 January 2006") + "\n"
	if got := articleMarkdown(art, nil, nil, nil); got != want {
		t.Errorf("articleMarkdown\n got: %q\nwant: %q", got, want)
	}
}
