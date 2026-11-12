package web

import "net/http"

// Cookie names for the display preferences. These are written entirely
// client-side, by theme.js — the server only ever reads them, so setting one
// is never a CSRF-relevant action.
const (
	ThemeCookieName   = "onsuite_theme"
	FontCookieName    = "onsuite_font"
	PaletteCookieName = "onsuite_palette"
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

// FontFrom reads the font-pairing preference, defaulting to "sans"
// (Inter/JetBrains Mono). "mixed" keeps Inter for the interface and sets
// reading text in Source Serif (#617). Older cookie values from before the
// options were renamed still map to their current equivalents.
func FontFrom(r *http.Request) string {
	if c, err := r.Cookie(FontCookieName); err == nil {
		switch c.Value {
		case "sans", "serif", "mono", "mixed":
			return c.Value
		case "literata":
			return "serif"
		case "duo", "grotesk":
			return "mono"
		}
	}
	return "sans"
}

// SidebarCollapsedFrom reads whether the sidebar should render collapsed,
// defaulting to expanded.
func SidebarCollapsedFrom(r *http.Request) bool {
	c, err := r.Cookie(SidebarCookieName)
	return err == nil && c.Value == "collapsed"
}

// PaletteFrom reads the colour-palette preference, defaulting to "warm"
// (cream and teal).
func PaletteFrom(r *http.Request) string {
	if c, err := r.Cookie(PaletteCookieName); err == nil && c.Value == "ink" {
		return "ink"
	}
	return "warm"
}
