package web

import "net/http"

// Cookie names for the display preferences. These are written entirely
// client-side, by theme.js — the server only ever reads them, so setting one
// is never a CSRF-relevant action.
const (
	ThemeCookieName   = "onsuite_theme"
	SidebarCookieName = "onsuite_sidebar"
)

// ThemeFrom reads the theme preference, defaulting to light for a missing or
// unrecognised cookie so a tampered or stale value never breaks the page.
func ThemeFrom(r *http.Request) string {
	if c, err := r.Cookie(ThemeCookieName); err == nil && c.Value == "dark" {
		return "dark"
	}
	return "light"
}

// SidebarCollapsedFrom reads whether the sidebar should render collapsed,
// defaulting to expanded.
func SidebarCollapsedFrom(r *http.Request) bool {
	c, err := r.Cookie(SidebarCookieName)
	return err == nil && c.Value == "collapsed"
}
