package oauth

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// limiter is a fixed-window request counter per key. The OAuth endpoints key
// it by client IP (spec 2026-11-24-ai-access-mcp-design.md, "Rate limits"):
// they are open to the internet, and registration in particular writes a row
// for anyone who asks. It mirrors internal/platform/mcp/ratelimit.go rather
// than importing it: the two platform packages must not depend on each other
// (PATTERNS.md, "Cross-app mirroring instead of a shared package"). A fixed
// window is crude at its edges, but a burst of twice the limit across a
// boundary is harmless here.
type limiter struct {
	n      int
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	windows map[string]fixedWindow
}

type fixedWindow struct {
	start time.Time
	count int
}

func newLimiter(n int, window time.Duration, now func() time.Time) *limiter {
	return &limiter{n: n, window: window, now: now, windows: map[string]fixedWindow{}}
}

// allow counts one request for key. Over the limit it returns false and how
// long until the window ends, for Retry-After. Windows that have ended are
// dropped on every call, so the map holds only IPs seen in the last
// window, which bounds it to the traffic of one window.
func (l *limiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for k, w := range l.windows {
		if !now.Before(w.start.Add(l.window)) {
			delete(l.windows, k)
		}
	}
	w, ok := l.windows[key]
	if !ok {
		w = fixedWindow{start: now}
	}
	if w.count >= l.n {
		return false, w.start.Add(l.window).Sub(now)
	}
	w.count++
	l.windows[key] = w
	return true, 0
}

// Per-IP limits (spec "Rate limits"). Registration is the tight one: a real
// client registers once per connection, so ten an hour is generous, while an
// open endpoint that inserts rows needs a hard ceiling. Token and
// authorization are called a few times per connection, so 60 a minute is
// far above any client and far below a guessing loop.
const (
	registerLimit  = 10
	registerWindow = time.Hour
	endpointLimit  = 60
	endpointWindow = time.Minute
)

// limitIP wraps next so each client IP gets at most the limiter's budget.
// The key is web.ClientIP, as login keys its limiter: the forwarded client
// when our own loopback proxy sent the request, the peer otherwise.
// refuse writes the 429 body; Retry-After is whole seconds, rounded up.
func limitIP(l *limiter, refuse func(http.ResponseWriter, *http.Request), next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := l.allow(web.ClientIP(r)); !ok {
			secs := max(int((wait+time.Second-1)/time.Second), 1)
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			refuse(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// slowDown is the refusal for the JSON endpoints: an OAuth-style error body
// that a client library can parse, rather than an HTML page.
func slowDown(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(`{"error":"slow_down","error_description":"Too many requests from this address. Try again later."}`))
}
