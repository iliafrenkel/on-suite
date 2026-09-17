package web

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync"
	"time"
)

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// bodyLimitOverrides holds per-route exceptions to DefaultMaxBodyBytes,
// keyed by the exact registered ServeMux pattern (e.g.
// "POST /flash/{deckID}/cards/{cardID}/media", including the method).
//
// This exists because Stack's LimitBody(DefaultMaxBodyBytes) runs ahead of
// the mux — outside any single app's registration — so by the time a route
// like ON Flash's media upload gets a chance to wrap its own handler in a
// bigger LimitBody (the pattern DefaultMaxBodyBytes's own doc comment
// recommends), r.Body has already been wrapped in a smaller
// http.MaxBytesReader. Nesting a larger http.MaxBytesReader inside a
// smaller one cannot loosen it — the inner (first-applied, smaller) reader
// still errors once its own byte count is exceeded, regardless of what any
// later wrap claims — so an app's own per-route override is silently
// ineffective without this. RegisterBodyLimit lets an app's Mount record
// the exception once, at startup, so LimitBodyForMux can look it up per
// request before CSRF or the app handler ever sees the body.
var (
	bodyLimitMu        sync.RWMutex
	bodyLimitByPattern = map[string]int64{}
)

// RegisterBodyLimit raises the platform's default body-size cap for one
// exact route, identified by its full registered pattern including method
// (e.g. "POST /flash/{deckID}/cards/{cardID}/media", the same string
// app.Router.Handle/HandleFunc register on the mux). Call it from an app's
// Mount, once, for any route whose legitimate uploads exceed
// DefaultMaxBodyBytes — see that constant's doc comment for why this needs
// to exist at all rather than just raising the default globally.
func RegisterBodyLimit(pattern string, max int64) {
	bodyLimitMu.Lock()
	defer bodyLimitMu.Unlock()
	bodyLimitByPattern[pattern] = max
}

func bodyLimitFor(pattern string, def int64) int64 {
	bodyLimitMu.RLock()
	defer bodyLimitMu.RUnlock()
	if max, ok := bodyLimitByPattern[pattern]; ok {
		return max
	}
	return def
}

// limitBodyForStack is what Stack actually installs in place of a flat
// LimitBody(DefaultMaxBodyBytes): when h is the real *http.ServeMux (true
// for both the production server and apptest's test harness), it consults
// the pattern the request is about to match — via ServeMux's own Handler
// method, which resolves routing without invoking it — and applies that
// route's registered override if there is one. Anything that is not a
// *http.ServeMux (a handful of narrow platform unit tests construct Stack's
// chain directly around something else) falls back to the flat default
// exactly as before.
func limitBodyForStack(h http.Handler) Middleware {
	mux, ok := h.(*http.ServeMux)
	if !ok {
		return LimitBody(DefaultMaxBodyBytes)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, pattern := mux.Handler(r)
			max := bodyLimitFor(pattern, DefaultMaxBodyBytes)
			r.Body = http.MaxBytesReader(w, r.Body, max)
			next.ServeHTTP(w, r)
		})
	}
}

// Chain applies middleware so that the first listed is the outermost, which is
// the order they read in.
func Chain(h http.Handler, mw ...Middleware) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// Stack wraps h in the platform's standard middleware chain: panic recovery,
// request logging, security headers, a body-size cap, CSRF, and session
// loading. This is the exact chain the real server runs, so anything built
// on top of Stack — including test harnesses — exercises the same behavior
// production traffic does.
func Stack(h http.Handler, log *slog.Logger, errs *Errors, csrf *CSRF, authn *Auth) http.Handler {
	return Chain(h,
		Recover(log, errs),
		RequestLog(log),
		SecurityHeaders(),
		limitBodyForStack(h),
		csrf.Middleware,
		authn.LoadUser,
	)
}

// statusRecorder captures the status code so the request log can report it.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written int64
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.written += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the real writer, so flushing and
// hijacking still work through this wrapper.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// RequestLog logs one line per request after it completes.
func RequestLog(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(rec, r)

			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			level := slog.LevelInfo
			if rec.status >= 500 {
				level = slog.LevelError
			}
			log.Log(r.Context(), level, "request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.written,
				"duration_ms", time.Since(start).Milliseconds(),
				"htmx", IsHTMX(r),
			)
		})
	}
}

// Recover turns a panic into a logged 500 instead of a dropped connection.
func Recover(log *slog.Logger, e *Errors) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				// http.ErrAbortHandler is the documented way for a handler to
				// abandon a response deliberately; do not report it as a bug.
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Error("panic serving request",
					"panic", v,
					"method", r.Method,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				// The panic value may contain internals, so the user gets the
				// generic page and nothing else.
				e.Status(w, r, http.StatusInternalServerError)
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// contentSecurityPolicy forbids inline script and style, which is what makes
// a whole class of injection bugs unexploitable even if escaping fails.
//
// HTMX works under this policy: hx-* are HTML attributes, not inline script.
// An onclick= handler or a <script> block will not.
//
// img-src has no data: because there is no longer any reason for one: the
// reader's image proxy rewrites every article image to a same-origin
// /reader/img/<hash> URL before it is ever rendered, so every image the app
// serves, including third-party article images, comes from this origin.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"img-src 'self'; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'none'; " +
	"object-src 'none'"

// SecurityHeaders sets response headers that do not depend on the route.
func SecurityHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", contentSecurityPolicy)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("X-Frame-Options", "DENY")
			next.ServeHTTP(w, r)
		})
	}
}
