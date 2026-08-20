// internal/platform/web/prefs_test.go
package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func withCookie(name, value string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	if value != "" {
		r.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	return r
}

func TestThemeFrom(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"no cookie defaults to light", "", "light"},
		{"explicit light", "light", "light"},
		{"explicit dark", "dark", "dark"},
		{"garbage value defaults to light", "purple", "light"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ThemeFrom(withCookie(ThemeCookieName, tt.value)); got != tt.want {
				t.Errorf("ThemeFrom = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFontFrom(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"no cookie defaults to default", "", "default"},
		{"explicit default", "default", "default"},
		{"explicit literata", "literata", "literata"},
		{"explicit grotesk", "grotesk", "grotesk"},
		{"garbage value defaults to default", "comic-sans", "default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FontFrom(withCookie(FontCookieName, tt.value)); got != tt.want {
				t.Errorf("FontFrom = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSidebarCollapsedFrom(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"no cookie defaults to expanded", "", false},
		{"explicit collapsed", "collapsed", true},
		{"explicit expanded", "expanded", false},
		{"garbage value defaults to expanded", "sideways", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SidebarCollapsedFrom(withCookie(SidebarCookieName, tt.value)); got != tt.want {
				t.Errorf("SidebarCollapsedFrom = %v, want %v", got, tt.want)
			}
		})
	}
}
