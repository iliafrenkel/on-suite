package webfetch_test

import (
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

func TestResolveHTTPURL(t *testing.T) {
	base, _ := url.Parse("https://example.com/posts/one")
	for _, tc := range []struct {
		raw, want string
		ok        bool
	}{
		{"/img/a.png", "https://example.com/img/a.png", true},
		{"b.png", "https://example.com/posts/b.png", true},
		{"https://cdn.example/c.png", "https://cdn.example/c.png", true},
		{"javascript:alert(1)", "", false},
		{"data:image/png;base64,AAAA", "", false},
		{"file:///etc/passwd", "", false},
		{"", "", false},
	} {
		got, ok := webfetch.ResolveHTTPURL(tc.raw, base)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ResolveHTTPURL(%q) = %q, %v; want %q, %v", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

func TestURLHashIsStableAndValid(t *testing.T) {
	a := webfetch.URLHash("https://example.com/a.png")
	if a != webfetch.URLHash("https://example.com/a.png") {
		t.Fatal("hash is not stable")
	}
	if a == webfetch.URLHash("https://example.com/b.png") {
		t.Fatal("different URLs share a hash")
	}
	if len(a) != 32 || !webfetch.ValidURLHash(a) {
		t.Fatalf("hash %q is not 32 lowercase hex chars", a)
	}
	for _, bad := range []string{"", "ABCDEF0123456789abcdef0123456789", "../../etc/passwd", a + "0"} {
		if webfetch.ValidURLHash(bad) {
			t.Errorf("ValidURLHash(%q) = true", bad)
		}
	}
}
