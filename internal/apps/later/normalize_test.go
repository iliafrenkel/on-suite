package later_test

import (
	"errors"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
)

func TestNormalizeURL(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://Example.COM/a/b", "https://example.com/a/b"},
		{"  https://example.com/post#comments  ", "https://example.com/post"},
		{"https://example.com/p?utm_source=x&utm_medium=y&id=7", "https://example.com/p?id=7"},
		{"https://example.com/p?fbclid=1&gclid=2&mc_cid=3&mc_eid=4&ref_src=5", "https://example.com/p"},
		{"https://example.com/p?b=2&a=1", "https://example.com/p?a=1&b=2"},
		{"http://example.com/", "http://example.com/"},
	} {
		got, err := later.NormalizeURL(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "not a url", "ftp://example.com/x", "javascript:alert(1)", "https:///nohost", "/relative/path"} {
		if _, err := later.NormalizeURL(bad); !errors.Is(err, later.ErrInvalid) {
			t.Errorf("NormalizeURL(%q) err = %v, want ErrInvalid", bad, err)
		}
	}
}
