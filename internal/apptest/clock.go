package apptest

import (
	"sync"
	"time"
)

// Clock is the time source NewServer hands both the app under test (through
// app.Deps.Now) and the harness's own Store. Until a test calls Set or
// Advance it simply follows the real clock, so tests that never touch it
// behave exactly as before (#357).
//
// Safe for concurrent use: handlers and jobs read it from other goroutines
// under -race.
type Clock struct {
	mu     sync.Mutex
	pinned bool
	t      time.Time
}

// Now returns the pinned time, or the real time (UTC) if nothing pinned it.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.pinned {
		return time.Now().UTC()
	}
	return c.t
}

// Set pins the clock at t (converted to UTC, like every Store's default).
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pinned, c.t = true, t.UTC()
}

// Advance moves the clock forward by d, pinning it first at the real time
// if it was not already pinned.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.pinned {
		c.pinned, c.t = true, time.Now().UTC()
	}
	c.t = c.t.Add(d)
}
