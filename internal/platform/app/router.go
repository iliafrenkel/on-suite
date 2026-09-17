package app

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// Route records one registration. It is an alias of web.Route so that app
// routes and the platform's own routes land in the same recorder and can be
// shown as one map.
type Route = web.Route

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
	rec    *web.Recorder
}

func newRouter(mux *http.ServeMux, appID string, guard web.Middleware, rec *web.Recorder) *Router {
	return &Router{
		mux:    mux,
		appID:  appID,
		prefix: "/" + appID,
		guard:  guard,
		rec:    rec,
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

// RegisterBodyLimit raises the platform's default request-body cap
// (web.DefaultMaxBodyBytes) to max for one route already registered with
// Handle/HandleFunc, identified the same way Handle/HandleFunc's own
// pattern is — relative to this app, e.g. "POST /{deckID}/cards/{id}/media".
//
// Wrapping a route's own handler in web.LimitBody (the pattern
// DefaultMaxBodyBytes's doc comment recommends) is not sufficient on its
// own: the platform's shared middleware stack (web.Stack) already wraps
// every request's body in a DefaultMaxBodyBytes-sized reader before the
// mux — and therefore before CSRF's own body parsing and before any app
// handler — ever sees it, and nesting a larger reader inside a smaller one
// cannot loosen the smaller one. This records the same exception against
// the exact pattern the mux will match, so Stack's own body-limit
// middleware can apply it before anything downstream reads the body. See
// web.RegisterBodyLimit for the mechanism.
func (r *Router) RegisterBodyLimit(pattern string, max int64) {
	full, err := joinPattern(r.prefix, pattern)
	if err != nil {
		panic(fmt.Sprintf("app %s: %v", r.appID, err))
	}
	web.RegisterBodyLimit(full, max)
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
	rt := Route{Pattern: full, Public: public, Owner: r.appID}
	r.routes = append(r.routes, rt)
	r.rec.Add(rt)
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
