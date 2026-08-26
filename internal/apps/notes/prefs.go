package notes

import "net/http"

// ShowCompletedCookie holds whether done bullets should render — spec §11.
// Unlike the platform's theme/font cookies (written client-side by
// theme.js), this one is set server-side by POST /notes/prefs: it changes
// what the outline query returns, not just how an already-loaded page
// looks, and spec §7 requires every behaviour here to work with JavaScript
// off — a plain form, not a JS cookie write, is what makes that true.
const ShowCompletedCookie = "onsuite_notes_show_completed"

// showCompletedCookieMaxAge keeps the preference for a year. It is a
// preference rather than a session fact, so it must outlive the browser
// window that set it.
const showCompletedCookieMaxAge = 60 * 60 * 24 * 365

// showCompletedFrom reads the preference, defaulting to false: a fresh
// browser sees completed bullets hidden, matching the outline's own default
// of showing what is still open.
func showCompletedFrom(r *http.Request) bool {
	c, err := r.Cookie(ShowCompletedCookie)
	return err == nil && c.Value == "1"
}
