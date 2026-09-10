package reader_test

import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

func TestSanitizeArticleHTMLProxiesImages(t *testing.T) {
	got, images := reader.SanitizeArticleHTML(
		`<p>Text</p><img src="https://cdn.example/a.png" alt="A picture">`,
		"https://example.com/post")

	if strings.Contains(got, "cdn.example") {
		t.Errorf("publisher host survived rewriting:\n%s", got)
	}
	if !strings.Contains(got, `src="/reader/img/`) {
		t.Errorf("image was not rewritten to a proxy URL:\n%s", got)
	}
	if !strings.Contains(got, `alt="A picture"`) {
		t.Errorf("alt text was dropped:\n%s", got)
	}
	if len(images) != 1 {
		t.Fatalf("recorded %d images, want 1: %v", len(images), images)
	}
	for _, src := range images {
		if src != "https://cdn.example/a.png" {
			t.Errorf("recorded source %q, want the absolute publisher URL", src)
		}
	}
}

func TestSanitizeArticleHTMLResolvesRelativeSources(t *testing.T) {
	_, images := reader.SanitizeArticleHTML(
		`<img src="/img/a.png">`, "https://example.com/posts/one")

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
func TestSanitizeArticleHTMLDeduplicates(t *testing.T) {
	got, images := reader.SanitizeArticleHTML(
		`<img src="https://cdn.example/a.png"><img src="https://cdn.example/a.png">`,
		"https://example.com/post")

	if len(images) != 1 {
		t.Errorf("recorded %d images for one distinct URL", len(images))
	}
	if n := strings.Count(got, "/reader/img/"); n != 2 {
		t.Errorf("rendered %d proxy URLs, want 2 pointing at the same hash", n)
	}
}

func TestImageHashIsStableAndURLSafe(t *testing.T) {
	a := reader.ImageHash("https://cdn.example/a.png")
	b := reader.ImageHash("https://cdn.example/a.png")
	c := reader.ImageHash("https://cdn.example/b.png")

	if a != b {
		t.Error("hash is not stable across calls")
	}
	if a == c {
		t.Error("different URLs hashed the same")
	}
	if len(a) != 32 {
		t.Errorf("hash is %d chars, want 32", len(a))
	}
	if strings.Trim(a, "0123456789abcdef") != "" {
		t.Errorf("hash %q is not lowercase hex, so it is not safe in a path", a)
	}
}

// Anything the rewriter cannot handle must lose its images, never keep them:
// a remote src that survives is precisely the tracking pixel this exists to
// prevent.
func TestSanitizeArticleHTMLFailsClosed(t *testing.T) {
	for _, in := range []string{
		`<img>`,
		`<img src="">`,
		`<img src="javascript:alert(1)">`,
		`<img src="data:image/gif;base64,R0lGOD">`,
		`<img src="ftp://example.com/a.png">`,
	} {
		got, images := reader.SanitizeArticleHTML(in, "https://example.com/post")
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
// "href stripped" behavior TestSanitizeHTMLStillStripsImages-adjacent code
// already relies on for the default policy.
func TestSanitizeArticleHTMLDoesNotWidenRelativeLinks(t *testing.T) {
	in := `<p><a href="/about">About</a></p><img src="/img/a.png">`

	got, images := reader.SanitizeArticleHTML(in, "https://example.com/posts/one")

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
	if !strings.Contains(got, `src="/reader/img/`) {
		t.Errorf("image was not rewritten to a proxy URL:\n%s", got)
	}
}

// SanitizeHTML keeps R1's guarantee: on its own it strips images entirely, so
// any caller that forgets to rewrite cannot leak.
func TestSanitizeHTMLStillStripsImages(t *testing.T) {
	got := reader.SanitizeHTML(`<p>a</p><img src="https://tracker.example/px.gif">`)
	if strings.Contains(got, "<img") || strings.Contains(got, "tracker.example") {
		t.Errorf("SanitizeHTML no longer strips images:\n%s", got)
	}
}
