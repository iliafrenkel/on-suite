package web

import (
	"log/slog"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
)

// Errors renders error responses. It is a type rather than a set of functions
// because it needs the renderer and the logger.
type Errors struct {
	render *render.Renderer
	log    *slog.Logger
}

func NewErrors(r *render.Renderer, log *slog.Logger) *Errors {
	return &Errors{render: r, log: log}
}

// titles keeps user-facing wording in one place. Anything not listed gets a
// generic message, which is deliberate: an error page is not the place to
// explain the internals.
var titles = map[int]struct{ title, message string }{
	http.StatusBadRequest:            {"Bad request", "That request could not be understood."},
	http.StatusUnauthorized:          {"Sign in required", "Please sign in to continue."},
	http.StatusForbidden:             {"Not allowed", "You do not have access to that."},
	http.StatusNotFound:              {"Not found", "There is nothing at that address."},
	http.StatusMethodNotAllowed:      {"Not allowed", "That method is not supported here."},
	http.StatusRequestEntityTooLarge: {"Too large", "That was larger than the limit."},
	http.StatusTooManyRequests:       {"Slow down", "Too many attempts. Try again shortly."},
	http.StatusInternalServerError:   {"Something broke", "An unexpected error occurred. It has been logged."},
}

// Status renders an error page for a status code.
func (e *Errors) Status(w http.ResponseWriter, r *http.Request, status int) {
	t, ok := titles[status]
	if !ok {
		t = titles[http.StatusInternalServerError]
	}

	data := map[string]any{"Status": status, "Title": t.title, "Message": t.message}

	// An HTMX swap must not receive a whole document; it would end up nested
	// inside the page it was swapped into.
	if IsHTMX(r) {
		if err := e.render.Fragment(w, status, "error", "fragment", data); err != nil {
			e.fallback(w, status, err)
		}
		return
	}

	page := render.Page{
		Title: t.title,
		Shell: render.Shell{ActiveApp: ActiveApp(r.Context())},
		Data:  data,
	}
	if u, ok := UserFrom(r.Context()); ok {
		page.Shell.LoggedIn = true
		page.Shell.Username = u.Username
		page.Shell.IsAdmin = u.IsAdmin
	}
	page.Shell.CSRFToken = CSRFToken(r.Context())

	if err := e.render.Page(w, status, "error", page); err != nil {
		e.fallback(w, status, err)
	}
}

// NotFound is the http.Handler form, for mux fallbacks.
func (e *Errors) NotFound(w http.ResponseWriter, r *http.Request) {
	e.Status(w, r, http.StatusNotFound)
}

// Internal logs the real cause and shows the user nothing about it.
func (e *Errors) Internal(w http.ResponseWriter, r *http.Request, err error) {
	e.log.Error("request failed",
		"error", err, "method", r.Method, "path", r.URL.Path)
	e.Status(w, r, http.StatusInternalServerError)
}

// fallback runs when even the error template failed. Plain text, because
// whatever we tried has already proven unreliable.
func (e *Errors) fallback(w http.ResponseWriter, status int, err error) {
	e.log.Error("rendering the error page failed", "error", err, "status", status)
	http.Error(w, http.StatusText(status), status)
}
