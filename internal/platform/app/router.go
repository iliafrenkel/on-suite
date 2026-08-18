package app

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// Route records one registration, for tests and for introspection.
type Route struct {
	// Pattern is the full pattern as registered, e.g. "GET /paste/{slug}".
	Pattern string
	// Public is true only when the app called Public explicitly.
	Public bool
}

// Router registers an app's routes under its own prefix.
//
// Handle requires authentication. Public does not. That asymmetry is the
// point: forgetting to protect a route is impossible, because protection is
// what happens when you do nothing special.
type Router struct {
	mux    *http.ServeMux
	appID  string
	prefix string
	guard  web.Middleware
	routes []Route
}

func newRouter(mux *http.ServeMux, appID string, guard web.Middleware) *Router {
	return &Router{
		mux:    mux,
		appID:  appID,
		prefix: "/" + appID,
		guard:  guard,
	}
}

// Handle registers an authenticated route. The pattern is relative to the app,
// so "GET /{slug}" becomes "GET /paste/{slug}".
func (r *Router) Handle(pattern string, h http.Handler) {
	r.register(pattern, h, false)
}

func (r *Router) HandleFunc(pattern string, h http.HandlerFunc) {
	r.register(pattern, h, false)
}

// Public registers a route reachable without signing in.
//
// Use it only for content that is meant to be shared, such as a paste behind
// an unguessable slug. Every call is a deliberate decision to expose
// something, and is greppable for exactly that reason.
func (r *Router) Public(pattern string, h http.Handler) {
	r.register(pattern, h, true)
}

func (r *Router) PublicFunc(pattern string, h http.HandlerFunc) {
	r.register(pattern, h, true)
}

// Routes lists what was registered, in registration order.
func (r *Router) Routes() []Route {
	out := make([]Route, len(r.routes))
	copy(out, r.routes)
	return out
}

func (r *Router) register(pattern string, h http.Handler, public bool) {
	full, err := joinPattern(r.prefix, pattern)
	if err != nil {
		// A malformed pattern is a programming error in an app, discovered at
		// startup. ServeMux panics on bad patterns too, so this is consistent.
		panic(fmt.Sprintf("app %s: %v", r.appID, err))
	}

	// Every app route records which app is active, so the shell can mark the
	// nav without each handler remembering to.
	handler := r.withActiveApp(h)
	if !public {
		handler = r.guard(handler)
	}

	r.mux.Handle(full, handler)
	r.routes = append(r.routes, Route{Pattern: full, Public: public})
}

func (r *Router) withActiveApp(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		next.ServeHTTP(w, req.WithContext(web.WithActiveApp(req.Context(), r.appID)))
	})
}

// joinPattern inserts the app prefix into a ServeMux pattern, preserving an
// optional leading method.
func joinPattern(prefix, pattern string) (string, error) {
	method, path := "", pattern
	if i := strings.Index(pattern, " "); i >= 0 {
		method, path = pattern[:i], pattern[i+1:]
	}
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("pattern %q must have a path starting with /", pattern)
	}
	if strings.Contains(path, "//") {
		return "", fmt.Errorf("pattern %q contains an empty path segment", pattern)
	}
	joined := prefix + path
	if method == "" {
		return joined, nil
	}
	return method + " " + joined, nil
}
