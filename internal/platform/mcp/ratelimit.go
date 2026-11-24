package mcp

import (
	"sync"
	"time"
)

// limiter is a fixed-window request counter per key. /mcp keys it by grant,
// at 120 requests a minute (spec 2026-11-24-ai-access-mcp-design.md, "Rate
// limits"): enough for any conversation, small enough that a runaway client
// loop cannot hammer the single SQLite file. A fixed window is crude at its
// edges, but a burst of twice the limit across a boundary is harmless here.
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
// dropped on every call, so the map holds only grants active in the last
// window; there are a handful of grants, so the sweep costs nothing.
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
