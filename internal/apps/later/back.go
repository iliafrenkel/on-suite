package later

import (
	"net/http"
	"strings"
)

// safeBack is the form's "back" target if it is one of Later's own pages,
// else fallback. Anything else could turn a form into an open redirect.
func safeBack(r *http.Request, fallback string) string {
	b := r.PostFormValue("back")
	if !strings.HasPrefix(b, "/later/") || strings.Contains(b, "//") || strings.ContainsRune(b, '\\') {
		return fallback
	}
	return b
}
