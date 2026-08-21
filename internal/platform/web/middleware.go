package web

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

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
		LimitBody(DefaultMaxBodyBytes),
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
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"img-src 'self' data:; " +
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
