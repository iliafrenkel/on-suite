package reader_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

// articlePage is a page shaped like a real one: navigation and a footer around
// a body long enough for readability's density heuristics to find it.
const articlePage = `<html><head><title>A Title</title></head><body>
<nav>Home About Contact Subscribe</nav>
<article>
<h1>A Title</h1>
<p>First real paragraph with enough words to look like an article body and pass
the density heuristics that readability applies when it scores candidate nodes
in a document like this one.</p>
<p>Second paragraph, also long enough to matter for scoring purposes and to keep
the extractor interested in this particular subtree of the page.</p>
<img src="/pic.png">
</article>
<footer>Copyright, privacy policy, cookie notice</footer>
</body></html>`

func TestExtractArticleKeepsTheBodyAndDropsTheChrome(t *testing.T) {
	got, err := reader.ExtractArticle([]byte(articlePage), "https://example.com/post")
	if err != nil {
		t.Fatalf("ExtractArticle: %v", err)
	}
	if !strings.Contains(got.HTML, "First real paragraph") {
		t.Errorf("article body missing:\n%s", got.HTML)
	}
	if strings.Contains(got.HTML, "cookie notice") || strings.Contains(got.HTML, "About Contact") {
		t.Errorf("page chrome survived extraction:\n%s", got.HTML)
	}
	if got.Title != "A Title" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.TextLength == 0 {
		t.Error("TextLength is 0, so the quality check downstream can never work")
	}
}

// The whole reason R4 waits for R3: an extracted page is raw publisher HTML,
// usually more image-heavy than the feed body.
func TestExtractArticleProxiesImages(t *testing.T) {
	got, err := reader.ExtractArticle([]byte(articlePage), "https://example.com/post")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.HTML, "example.com/pic.png") {
		t.Errorf("publisher image URL survived:\n%s", got.HTML)
	}
	if !strings.Contains(got.HTML, reader.ImagePathPrefix) {
		t.Errorf("image was not rewritten to the proxy:\n%s", got.HTML)
	}
	if len(got.Images) != 1 {
		t.Fatalf("recorded %d images, want 1: %v", len(got.Images), got.Images)
	}
	for _, src := range got.Images {
		if src != "https://example.com/pic.png" {
			t.Errorf("recorded %q, want the absolute publisher URL", src)
		}
	}
}

func TestExtractArticleSanitizesHostileMarkup(t *testing.T) {
	got, err := reader.ExtractArticle([]byte(`<html><body><article>
<p>Body text long enough that readability will keep this node when it scores the
candidates in this document and picks a winner among them.</p>
<script>alert(1)</script>
<p onclick="steal()">More body text, again long enough to be scored as content.</p>
</article></body></html>`), "https://example.com/post")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"<script", "alert(1)", "onclick"} {
		if strings.Contains(got.HTML, bad) {
			t.Errorf("extracted HTML still contains %q:\n%s", bad, got.HTML)
		}
	}
}

func TestExtractArticleRefusesAPageWithNoArticle(t *testing.T) {
	cases := map[string]string{
		"empty":        ``,
		"nav only":     `<html><body><nav>a b c</nav></body></html>`,
		"not html":     "\x89PNG\r\n\x1a\n\x00\x00",
		"tiny content": `<html><body><p>Hi.</p></body></html>`,
	}
	for name, page := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := reader.ExtractArticle([]byte(page), "https://example.com/post"); !errors.Is(err, reader.ErrNotExtractable) {
				t.Errorf("err = %v, want ErrNotExtractable", err)
			}
		})
	}
}
