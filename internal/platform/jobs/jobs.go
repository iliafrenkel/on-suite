// Package jobs runs named background jobs on a fixed interval, runs them on
// demand, and remembers how each one went.
//
// It is a leaf package: it takes closures and imports nothing else in this
// module. That is deliberate. The admin pages can list and trigger jobs
// without the platform learning what a "snapshot" or a "session sweep" is,
// and a future app-owned job needs no change here.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	// ErrUnknownJob is returned by Trigger for a slug no job has.
	ErrUnknownJob = errors.New("jobs: unknown job")
	// ErrAlreadyRunning is returned by Trigger when the job is mid-run,
	// whether that run was scheduled or triggered.
	ErrAlreadyRunning = errors.New("jobs: job is already running")
)

// Status is one job's registration together with its most recent outcome.
//
// It is a flat value type: Snapshot hands copies to a request goroutine, so
// nothing rendering the admin page can reach back into the scheduler.
type Status struct {
	Name        string
	Description string
	// Slug is Name made URL-safe, fixed at Register. It is how a request
	// names a job to Trigger.
	Slug string
	// Interval is how often the job runs. Zero means the job is registered
	// but never scheduled.
	Interval time.Duration
	Enabled  bool
	// Running is true from the moment a run starts until it is recorded.
	Running bool
	Runs    int
	// LastRun is the zero time until the job has run at least once.
	LastRun      time.Time
	LastDuration time.Duration
	// LastErr is the last run's error text, empty after a success. It is a
	// string rather than an error so a Status can be copied and rendered
	// without holding on to whatever the job's error wrapped.
	LastErr string
	// LastManual reports whether the most recent recorded run was triggered
	// by hand rather than by the timer.
	LastManual bool
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

	// base is the context triggered runs use: never a request's, so closing
	// a browser tab cannot cancel a half-written snapshot. Run ties it to its
	// own context, so shutting the server down cancels triggered and
	// scheduled runs alike (spec §3.2).
	base   context.Context
	cancel context.CancelFunc
	// manual counts triggered runs still going, for Wait.
	manual sync.WaitGroup
}

func NewRegistry() *Registry {
	base, cancel := context.WithCancel(context.Background())
	return &Registry{
		now:    func() time.Time { return time.Now().UTC() },
		base:   base,
		cancel: cancel,
	}
}

// SetClock replaces the time source, so run bookkeeping can be tested without
// waiting for real intervals to elapse.
func (r *Registry) SetClock(now func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = now
}

// Register adds a job. An interval of zero or less registers it as disabled:
// it is listed on the admin page and never run on a timer, which is how an
// operator who turned a schedule off sees that it is off rather than seeing
// nothing. A disabled job can still be triggered.
//
// Register panics if name yields an empty slug or one another job already
// has: both are programming errors, caught by the first test that builds the
// stack.
//
// Register must be called before Run.
func (r *Registry) Register(name, description string, every time.Duration, fn func(context.Context) error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	slug := slugify(name)
	if slug == "" {
		panic(fmt.Sprintf("jobs: job name %q has no letters or digits to make a slug from", name))
	}
	for _, j := range r.jobs {
		if j.status.Slug == slug {
			panic(fmt.Sprintf("jobs: %q and %q share the slug %q", j.status.Name, name, slug))
		}
	}

	status := Status{
		Name:        name,
		Description: description,
		Slug:        slug,
		Interval:    every,
		Enabled:     every > 0,
	}
	if status.Enabled {
		status.NextRun = r.now().Add(every)
	}
	r.jobs = append(r.jobs, &job{fn: fn, status: status})
}

// slugify lower-cases name and collapses every run of characters outside
// [a-z0-9] into one "-", trimming any at either end.
func slugify(name string) string {
	var b strings.Builder
	gap := false
	for _, c := range strings.ToLower(name) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			if gap && b.Len() > 0 {
				b.WriteByte('-')
			}
			gap = false
			b.WriteRune(c)
			continue
		}
		gap = true
	}
	return b.String()
}

// Run starts one goroutine per enabled job and blocks until every goroutine
// has stopped. If no jobs are enabled, there is nothing to start and Run
// returns immediately without waiting on ctx.
//
// Either way, cancelling ctx also cancels triggered runs: tying the
// registry's own context to ctx is the first thing Run does.
//
// Each job first runs one interval after Run is called, never immediately, so
// restarting the server repeatedly does not trigger a burst of work.
func (r *Registry) Run(ctx context.Context) {
	context.AfterFunc(ctx, r.cancel)

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
			r.scheduleFirstRun(j, every)
			r.loop(ctx, j, every)
		}(e.j, e.every)
	}
	wg.Wait()
}

// scheduleFirstRun corrects NextRun to be relative to when Run actually
// started rather than when Register was called. The two can drift apart at
// startup, and a NextRun computed at Register time would then be stale
// before the job has ever run.
func (r *Registry) scheduleFirstRun(j *job, every time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j.status.NextRun = r.now().Add(every)
}

func (r *Registry) loop(ctx context.Context, j *job, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// A tick that lands while the job is still running — a slow
			// previous tick or a triggered run — is skipped, not queued.
			if r.begin(j) {
				r.run(ctx, j, false)
			}
		}
	}
}

// begin marks j as running and reports whether it was idle. Checking and
// setting happen under one lock, so two callers can never both start j.
func (r *Registry) begin(j *job) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j.status.Running {
		return false
	}
	j.status.Running = true
	return true
}

// Trigger starts the job with this slug in its own goroutine and returns
// without waiting for it. The run does not move NextRun: the ticker is not
// reset, so the next scheduled run is still when the page says it is.
func (r *Registry) Trigger(slug string) error {
	r.mu.Lock()
	var target *job
	for _, j := range r.jobs {
		if j.status.Slug == slug {
			target = j
			break
		}
	}
	r.mu.Unlock()

	if target == nil {
		return ErrUnknownJob
	}
	if !r.begin(target) {
		return ErrAlreadyRunning
	}
	r.manual.Add(1)
	go func() {
		defer r.manual.Done()
		r.run(r.base, target, true)
	}()
	return nil
}

// Wait blocks until every triggered run has finished. It exists for tests,
// to observe a triggered run's outcome without polling, and must not be
// called concurrently with Trigger: a sync.WaitGroup forbids Add from zero
// racing Wait.
func (r *Registry) Wait() {
	r.manual.Wait()
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

// run executes one job, which the caller has already marked running with
// begin, and records what happened. Every failure is recorded and
// swallowed: a job that cannot do its work must not take down a server that
// is otherwise answering requests perfectly well.
//
// Only a scheduled run moves NextRun; a manual one leaves it alone (spec
// §3.4).
func (r *Registry) run(ctx context.Context, j *job, manual bool) {
	r.mu.Lock()
	now := r.now
	r.mu.Unlock()

	start := now()
	err := safeRun(ctx, j.fn)
	end := now()

	r.mu.Lock()
	defer r.mu.Unlock()
	j.status.Running = false
	j.status.Runs++
	j.status.LastRun = start
	j.status.LastDuration = end.Sub(start)
	j.status.LastManual = manual
	j.status.LastErr = ""
	if err != nil {
		j.status.LastErr = err.Error()
	}
	if !manual {
		j.status.NextRun = end.Add(j.status.Interval)
	}
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
