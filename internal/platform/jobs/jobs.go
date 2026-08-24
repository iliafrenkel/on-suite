// Package jobs runs named background jobs on a fixed interval and remembers
// how each one went.
//
// It is a leaf package: it takes closures and imports nothing else in this
// module. That is deliberate. The admin page can list job status without the
// platform learning what a "snapshot" or a "session sweep" is, and a future
// app-owned job needs no change here.
package jobs

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Status is one job's registration together with its most recent outcome.
//
// It is a flat value type: Snapshot hands copies to a request goroutine, so
// nothing rendering the admin page can reach back into the scheduler.
type Status struct {
	Name        string
	Description string
	// Interval is how often the job runs. Zero means the job is registered
	// but never scheduled.
	Interval time.Duration
	Enabled  bool
	Runs     int
	// LastRun is the zero time until the job has run at least once.
	LastRun      time.Time
	LastDuration time.Duration
	// LastErr is the last run's error text, empty after a success. It is a
	// string rather than an error so a Status can be copied and rendered
	// without holding on to whatever the job's error wrapped.
	LastErr string
	// NextRun is the zero time for a disabled job.
	NextRun time.Time
}

type job struct {
	fn     func(context.Context) error
	status Status
}

// Registry holds every registered job. One mutex guards both the slice and
// every job's status: at this scale there is no contention worth splitting
// locks for, and one lock is one thing to reason about.
type Registry struct {
	mu   sync.Mutex
	jobs []*job
	now  func() time.Time
}

func NewRegistry() *Registry {
	return &Registry{now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the time source, so run bookkeeping can be tested without
// waiting for real intervals to elapse.
func (r *Registry) SetClock(now func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = now
}

// Register adds a job. An interval of zero or less registers it as disabled:
// it is listed on the admin page and never run, which is how an operator who
// turned a schedule off sees that it is off rather than seeing nothing.
//
// Register must be called before Run.
func (r *Registry) Register(name, description string, every time.Duration, fn func(context.Context) error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	status := Status{
		Name:        name,
		Description: description,
		Interval:    every,
		Enabled:     every > 0,
	}
	if status.Enabled {
		status.NextRun = r.now().Add(every)
	}
	r.jobs = append(r.jobs, &job{fn: fn, status: status})
}

// Run starts one goroutine per enabled job and blocks until ctx is done.
//
// Each job first runs one interval after Run is called, never immediately, so
// restarting the server repeatedly does not trigger a burst of work.
func (r *Registry) Run(ctx context.Context) {
	type entry struct {
		j     *job
		every time.Duration
	}

	r.mu.Lock()
	var entries []entry
	for _, j := range r.jobs {
		if j.status.Enabled {
			entries = append(entries, entry{j: j, every: j.status.Interval})
		}
	}
	r.mu.Unlock()

	var wg sync.WaitGroup
	for _, e := range entries {
		wg.Add(1)
		go func(j *job, every time.Duration) {
			defer wg.Done()
			r.loop(ctx, j, every)
		}(e.j, e.every)
	}
	wg.Wait()
}

func (r *Registry) loop(ctx context.Context, j *job, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.run(ctx, j)
		}
	}
}

// Snapshot copies every job's status, in registration order. It is safe to
// call from a request goroutine while jobs are running.
func (r *Registry) Snapshot() []Status {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]Status, 0, len(r.jobs))
	for _, j := range r.jobs {
		out = append(out, j.status)
	}
	return out
}

// RunOnceForTest runs a named job immediately and records the outcome. It
// exists so the bookkeeping can be tested without a ticker; nothing in the
// server calls it, and nothing should — this package deliberately offers no
// way to trigger a job from a request.
func (r *Registry) RunOnceForTest(ctx context.Context, name string) {
	r.mu.Lock()
	var target *job
	for _, j := range r.jobs {
		if j.status.Name == name {
			target = j
			break
		}
	}
	r.mu.Unlock()

	if target != nil {
		r.run(ctx, target)
	}
}

// run executes one job and records what happened. Every failure is recorded
// and swallowed: a job that cannot do its work must not take down a server
// that is otherwise answering requests perfectly well.
func (r *Registry) run(ctx context.Context, j *job) {
	r.mu.Lock()
	now := r.now
	r.mu.Unlock()

	start := now()
	err := safeRun(ctx, j.fn)
	end := now()

	r.mu.Lock()
	defer r.mu.Unlock()
	j.status.Runs++
	j.status.LastRun = start
	j.status.LastDuration = end.Sub(start)
	j.status.LastErr = ""
	if err != nil {
		j.status.LastErr = err.Error()
	}
	j.status.NextRun = end.Add(j.status.Interval)
}

// safeRun turns a panicking job into a failed one. A panic in a background
// goroutine cannot be recovered by the HTTP stack's Recover middleware, so
// without this one bad job would kill the whole process.
func safeRun(ctx context.Context, fn func(context.Context) error) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("panic: %v", v)
		}
	}()
	return fn(ctx)
}
