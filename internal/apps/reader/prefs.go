package reader

import "net/http"

// HideReadCookie holds whether feeds with nothing unread should be hidden
// from the tree. Mirrors internal/apps/notes/prefs.go's ShowCompletedCookie:
// set server-side by POST /reader/prefs (not a client-side JS cookie write),
// since it changes what viewTree returns, not just how an already-loaded
// page looks.
const HideReadCookie = "onsuite_reader_hide_read"

// hideReadCookieMaxAge keeps the preference for a year — long enough to
// feel permanent, short enough that an abandoned browser eventually forgets.
const hideReadCookieMaxAge = 60 * 60 * 24 * 365

// hideReadFrom reads the preference, defaulting to false: a fresh browser
// sees every feed, matching the tree's own default of showing everything.
func hideReadFrom(r *http.Request) bool {
	c, err := r.Cookie(HideReadCookie)
	return err == nil && c.Value == "1"
}
