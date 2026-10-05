package article_test

import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/article"
)

func testSrc(hash string) string { return "/test/img/" + hash }

// The caller's ImageSrc, not a hard-coded path, decides where images point.
func TestSanitizeWithImagesUsesTheCallersSrc(t *testing.T) {
	got, images := article.SanitizeWithImages(
		`<p>Text</p><img src="https://cdn.example/a.png" alt="A">`,
		"https://example.com/post", testSrc)
	if strings.Contains(got, "cdn.example") {
		t.Errorf("publisher host survived rewriting:\n%s", got)
	}
	if !strings.Contains(got, `src="/test/img/`) {
		t.Errorf("image was not rewritten with the caller's src:\n%s", got)
	}
	if len(images) != 1 {
		t.Errorf("got %d images, want 1", len(images))
	}
}

func TestSanitizeWithImagesResolvesRelativeSources(t *testing.T) {
	_, images := article.SanitizeWithImages(
		`<img src="/img/a.png">`,
		"https://example.com/posts/one", testSrc)

	if len(images) != 1 {
		t.Fatalf("recorded %d images, want 1", len(images))
	}
	for _, src := range images {
		if src != "https://example.com/img/a.png" {
			t.Errorf("relative source resolved to %q, want https://example.com/img/a.png", src)
		}
	}
}

// The same image twice must be one row and one proxy URL, or a page with a
// repeated logo fetches it repeatedly.
func TestSanitizeWithImagesDeduplicates(t *testing.T) {
	got, images := article.SanitizeWithImages(
		`<img src="https://cdn.example/a.png"><img src="https://cdn.example/a.png">`,
		"https://example.com/post", testSrc)

	if len(images) != 1 {
		t.Errorf("recorded %d images for one distinct URL", len(images))
	}
	if n := strings.Count(got, "/test/img/"); n != 2 {
		t.Errorf("rendered %d proxy URLs, want 2 pointing at the same hash", n)
	}
}

// Anything the rewriter cannot handle must lose its images, never keep them:
// a remote src that survives is precisely the tracking pixel this exists to
// prevent.
func TestSanitizeWithImagesFailsClosed(t *testing.T) {
	for _, in := range []string{
		`<img>`,
		`<img src="">`,
		`<img src="javascript:alert(1)">`,
		`<img src="data:image/gif;base64,R0lGOD">`,
		`<img src="ftp://example.com/a.png">`,
	} {
		got, images := article.SanitizeWithImages(
			in,
			"https://example.com/post", testSrc)
		if strings.Contains(got, "javascript:") || strings.Contains(got, "data:") ||
			strings.Contains(got, "ftp://") {
			t.Errorf("input %q left a live source:\n%s", in, got)
		}
		if len(images) != 0 {
			t.Errorf("input %q recorded %d images, want 0", in, len(images))
		}
	}
}

// policyWithImages must stay exactly as strict as the default policy about
// relative <a href> values. Before this fix, allowing a relative <img src>
// through required flipping bluemonday's policy-wide AllowRelativeURLs
// switch, which also let a relative link survive sanitizing unresolved —
// rendering it relative to the reader app's own origin instead of the
// publisher's site. Pre-absolutizing img sources ahead of sanitizing lets
// policyWithImages leave AllowRelativeURLs off, so this must show the same
// "href stripped" behavior the default policy already has (see
// TestSanitizeHTMLStillStripsImages).
func TestSanitizeWithImagesDoesNotWidenRelativeLinks(t *testing.T) {
	in := `<p><a href="/about">About</a></p><img src="/img/a.png">`

	got, images := article.SanitizeWithImages(
		in,
		"https://example.com/posts/one", testSrc)

	if strings.Contains(got, `href="/about"`) {
		t.Errorf("relative href survived unresolved through policyWithImages:\n%s", got)
	}
	if strings.Contains(got, `href="https://example.com/about"`) {
		// Also acceptable: resolving the link instead of stripping it. Not
		// implemented here, but if a future change adds it this branch
		// should not fail the test — remove this guard if that happens.
		t.Skip("href was resolved to an absolute URL instead of stripped; that is fine too")
	}

	if len(images) != 1 {
		t.Fatalf("recorded %d images, want 1", len(images))
	}
	for _, src := range images {
		if src != "https://example.com/img/a.png" {
			t.Errorf("recorded source %q, want https://example.com/img/a.png", src)
		}
	}
	if !strings.Contains(got, `src="/test/img/`) {
		t.Errorf("image was not rewritten to a proxy URL:\n%s", got)
	}
}
