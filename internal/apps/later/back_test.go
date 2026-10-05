package later

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSafeBack(t *testing.T) {
	cases := map[string]string{
		"/later/a/3":            "/later/a/3",
		"":                      "/fb",
		"https://evil.example/": "/fb",
		"//evil.example":        "/fb",
		"/admin/":               "/fb",
		"/later/\\evil":         "/fb",
		"/later//x":             "/fb",
	}
	for in, want := range cases {
		r := httptest.NewRequest("POST", "/later/prefs", strings.NewReader(url.Values{"back": {in}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if got := safeBack(r, "/fb"); got != want {
			t.Errorf("safeBack(%q) = %q, want %q", in, got, want)
		}
	}
}
