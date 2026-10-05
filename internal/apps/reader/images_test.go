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
