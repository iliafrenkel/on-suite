package reader_test

import (
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
