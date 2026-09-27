# Trigger Jobs from the UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Admins can run any registered background job on demand from a new `/admin/jobs` page (issue #311).

**Architecture:** The leaf scheduler `internal/platform/jobs` gains `Trigger(slug)` (runs a job in the background on a registry-owned context), `Wait()`, an overlap guard, and three `Status` fields. A new platform package `internal/platform/jobsadmin` serves the page, an HTMX-polled table fragment, and the POST that triggers a job. `buildStack` mounts it beside `usermgmt`; `/admin/` gains a link.

**Tech Stack:** Go 1.x stdlib (`net/http`, `html/template`, `context`, `sync`), HTMX 2 (vendored), SQLite via `modernc.org/sqlite` (tests only here).

**Spec:** [docs/superpowers/specs/2026-09-27-trigger-jobs-design.md](../specs/2026-09-27-trigger-jobs-design.md)

## Global Constraints

- No new dependencies; `internal/platform/jobs` imports nothing from this module (stdlib only).
- No inline `<script>` and no `style=` attributes anywhere (strict CSP). HTMX `hx-*` attributes are fine.
- A signed-in non-admin must get a 404 byte-identical to a missing page on every `/admin/jobs*` route.
- Route patterns are exact — none ends in `/`.
- Commit subjects follow Conventional Commits, scope `platform`, and end with `(#311)`.
- Full check before the final commit of every task (must print nothing / pass):
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- Work on branch `feat/311-trigger-jobs` (already exists, spec committed). Never push to `main`.

---

### Task 1: Scheduler — Trigger, Wait, slugs, overlap guard

**Files:**
- Modify: `internal/platform/jobs/jobs.go` (whole file replaced below)
- Modify: `internal/platform/jobs/jobs_test.go`
- Modify: `internal/platform/admin/admin_test.go:341-345` (replace `RunOnceForTest`)
- Modify: `cmd/onsuite/backup_test.go:375` (replace `RunOnceForTest`)

**Interfaces:**
- Produces:
  - `func (r *Registry) Trigger(slug string) error` — returns `nil`, `jobs.ErrUnknownJob` or `jobs.ErrAlreadyRunning`; never blocks on the job.
  - `func (r *Registry) Wait()` — blocks until every triggered run has finished.
  - `jobs.Status` new fields: `Slug string`, `Running bool`, `LastManual bool`.
  - `Register` panics on an empty or duplicate slug.
  - `RunOnceForTest` is **removed**.

- [ ] **Step 1: Replace `RunOnceForTest` in the existing jobs tests and add the new failing tests**

In `internal/platform/jobs/jobs_test.go`, add `"sync/atomic"` to the imports, add this helper right after `stepClock`:

```go
// runNow triggers a job by slug and waits for it to be recorded.
func runNow(t *testing.T, reg *jobs.Registry, slug string) {
	t.Helper()
	if err := reg.Trigger(slug); err != nil {
		t.Fatalf("Trigger(%q) = %v", slug, err)
	}
	reg.Wait()
}
```

Replace every `reg.RunOnceForTest(context.Background(), "X")` in that file with `runNow(t, reg, "X")` — the slugs of the existing names `sweep`, `boom`, `flaky` are the names themselves.

Append these tests:

```go
func TestSlugIsDerivedFromTheName(t *testing.T) {
	for name, want := range map[string]string{
		"database snapshot":           "database-snapshot",
		"purge orphan media and tags": "purge-orphan-media-and-tags",
		"  Refresh   Feeds! ":         "refresh-feeds",
		"a/b_c":                       "a-b-c",
		"v2 thing":                    "v2-thing",
	} {
		reg := jobs.NewRegistry()
		reg.Register(name, "", time.Hour, func(context.Context) error { return nil })
		if got := reg.Snapshot()[0].Slug; got != want {
			t.Errorf("slug of %q = %q, want %q", name, got, want)
		}
	}
}

func TestRegisterPanicsOnADuplicateSlug(t *testing.T) {
	reg := jobs.NewRegistry()
	reg.Register("database snapshot", "", time.Hour, func(context.Context) error { return nil })
	defer func() {
		if recover() == nil {
			t.Error("registering a second job with the slug database-snapshot did not panic")
		}
	}()
	reg.Register("Database  Snapshot", "", time.Hour, func(context.Context) error { return nil })
}

func TestRegisterPanicsOnAnEmptySlug(t *testing.T) {
	reg := jobs.NewRegistry()
	defer func() {
		if recover() == nil {
			t.Error("registering a job whose name has no letters or digits did not panic")
		}
	}()
	reg.Register("!!!", "", time.Hour, func(context.Context) error { return nil })
}

func TestTriggerRecordsAManualRun(t *testing.T) {
	reg := jobs.NewRegistry()
	reg.Register("sweep", "", time.Hour, func(context.Context) error { return nil })
	runNow(t, reg, "sweep")

	s := reg.Snapshot()[0]
	if s.Runs != 1 || s.LastRun.IsZero() {
		t.Errorf("Runs = %d, LastRun = %v after one trigger", s.Runs, s.LastRun)
	}
	if !s.LastManual {
		t.Error("LastManual = false after a manual run")
	}
	if s.Running {
		t.Error("Running = true after Wait returned")
	}
}

func TestTriggerOfAnUnknownSlugFails(t *testing.T) {
	reg := jobs.NewRegistry()
	if err := reg.Trigger("nope"); !errors.Is(err, jobs.ErrUnknownJob) {
		t.Errorf("Trigger(unknown) = %v, want ErrUnknownJob", err)
	}
}

func TestTriggeringARunningJobFailsAndDoesNotStartASecondRun(t *testing.T) {
	reg := jobs.NewRegistry()
	release := make(chan struct{})
	var calls atomic.Int32
	reg.Register("slow", "", time.Hour, func(context.Context) error {
		calls.Add(1)
		<-release
		return nil
	})

	if err := reg.Trigger("slow"); err != nil {
		t.Fatal(err)
	}
	if !reg.Snapshot()[0].Running {
		t.Error("Running = false straight after Trigger returned")
	}
	if err := reg.Trigger("slow"); !errors.Is(err, jobs.ErrAlreadyRunning) {
		t.Errorf("second Trigger = %v, want ErrAlreadyRunning", err)
	}
	close(release)
	reg.Wait()
	if got := calls.Load(); got != 1 {
		t.Errorf("job ran %d times, want 1", got)
	}
}

func TestConcurrentTriggersStartExactlyOneRun(t *testing.T) {
	reg := jobs.NewRegistry()
	release := make(chan struct{})
	reg.Register("slow", "", time.Hour, func(context.Context) error {
		<-release
		return nil
	})

	const n = 8
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- reg.Trigger("slow")
		}()
	}
	wg.Wait()
	close(errs)
	close(release)
	reg.Wait()

	started := 0
	for err := range errs {
		switch {
		case err == nil:
			started++
		case !errors.Is(err, jobs.ErrAlreadyRunning):
			t.Errorf("Trigger = %v", err)
		}
	}
	if started != 1 {
		t.Errorf("%d triggers started a run, want exactly 1", started)
	}
}

func TestATickWhileTheJobIsRunningIsSkipped(t *testing.T) {
	reg := jobs.NewRegistry()
	release := make(chan struct{})
	started := make(chan struct{})
	var calls atomic.Int32
	reg.Register("slow", "", 5*time.Millisecond, func(context.Context) error {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go reg.Run(ctx)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the scheduled run never started")
	}

	// Roughly ten ticks fire while the first run is blocked.
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("job started %d times while its first run was still going, want 1", got)
	}
	if err := reg.Trigger("slow"); !errors.Is(err, jobs.ErrAlreadyRunning) {
		t.Errorf("Trigger during a scheduled run = %v, want ErrAlreadyRunning", err)
	}
	close(release)

	// Once the first run finishes, ticks run the job again.
	for i := 0; i < 400 && calls.Load() < 2; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() < 2 {
		t.Error("the job never ran again after its slow run finished")
	}
}

func TestAManualRunDoesNotMoveNextRun(t *testing.T) {
	reg := jobs.NewRegistry()
	// Every clock read moves time on by a minute, so a run that recomputed
	// NextRun from its end time would land on a different value.
	reg.SetClock(stepClock(time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC), time.Minute))
	reg.Register("sweep", "", time.Hour, func(context.Context) error { return nil })
	before := reg.Snapshot()[0].NextRun

	runNow(t, reg, "sweep")

	if got := reg.Snapshot()[0].NextRun; !got.Equal(before) {
		t.Errorf("NextRun moved from %v to %v after a manual run", before, got)
	}
}

func TestADisabledJobCanBeTriggered(t *testing.T) {
	reg := jobs.NewRegistry()
	reg.Register("snapshot", "", 0, func(context.Context) error { return nil })
	runNow(t, reg, "snapshot")

	s := reg.Snapshot()[0]
	if s.Runs != 1 {
		t.Errorf("Runs = %d, want 1", s.Runs)
	}
	if s.Enabled || !s.NextRun.IsZero() {
		t.Errorf("Enabled = %v, NextRun = %v; a manual run must not schedule a disabled job", s.Enabled, s.NextRun)
	}
}

// With nothing enabled, Run returns immediately. Triggering must still work
// afterwards — that is the --backup-interval 0 deployment.
func TestTriggerWorksAfterRunReturnedEarly(t *testing.T) {
	reg := jobs.NewRegistry()
	var ctxErr error
	reg.Register("snapshot", "", 0, func(ctx context.Context) error {
		ctxErr = ctx.Err()
		return nil
	})
	reg.Run(context.Background())

	runNow(t, reg, "snapshot")
	if reg.Snapshot()[0].Runs != 1 {
		t.Fatal("the job did not run")
	}
	if ctxErr != nil {
		t.Errorf("the manual run's context was already done: %v", ctxErr)
	}
}

func TestCancellingRunsContextCancelsAManualRun(t *testing.T) {
	reg := jobs.NewRegistry()
	started := make(chan struct{})
	reg.Register("snapshot", "", 0, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	reg.Run(ctx) // nothing enabled: returns straight away

	if err := reg.Trigger("snapshot"); err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	reg.Wait()

	if got := reg.Snapshot()[0].LastErr; got != context.Canceled.Error() {
		t.Errorf("LastErr = %q, want %q", got, context.Canceled.Error())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/platform/jobs/... -count=1`
Expected: build failure — `reg.Trigger undefined`, `reg.Wait undefined`, `jobs.ErrUnknownJob undefined`, `Slug`/`Running`/`LastManual` unknown fields.

- [ ] **Step 3: Replace `internal/platform/jobs/jobs.go`**

```go
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

// Wait blocks until every triggered run has finished. Tests use it to
// observe a triggered run's outcome without polling.
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
```

- [ ] **Step 4: Update the two other `RunOnceForTest` callers**

In `internal/platform/admin/admin_test.go`, `TestTheJobsSectionShowsOutcomesForJobsThatHaveRun`, replace:

```go
	ctx := context.Background()
	reg := jobs.NewRegistry()
	reg.Register("successful thing", "always works", time.Hour, func(context.Context) error { return nil })
	reg.Register("failed thing", "always fails", time.Hour, func(context.Context) error { return errors.New("boom") })
	reg.RunOnceForTest(ctx, "successful thing")
	reg.RunOnceForTest(ctx, "failed thing")
```

with:

```go
	reg := jobs.NewRegistry()
	reg.Register("successful thing", "always works", time.Hour, func(context.Context) error { return nil })
	reg.Register("failed thing", "always fails", time.Hour, func(context.Context) error { return errors.New("boom") })
	for _, slug := range []string{"successful-thing", "failed-thing"} {
		if err := reg.Trigger(slug); err != nil {
			t.Fatal(err)
		}
	}
	reg.Wait()
```

(If `ctx` is now unused in that function, the line is already gone; if `context` stays used elsewhere in the file, keep the import.)

In `cmd/onsuite/backup_test.go`, `TestTheSnapshotJobWritesASnapshot`, replace:

```go
	reg.RunOnceForTest(context.Background(), "database snapshot")
```

with:

```go
	if err := reg.Trigger("database-snapshot"); err != nil {
		t.Fatal(err)
	}
	reg.Wait()
```

Remove the `context` import from `cmd/onsuite/backup_test.go` only if the compiler reports it unused.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/platform/jobs/... ./internal/platform/admin/... ./cmd/onsuite/... -race -count=1`
Expected: `ok` for all three packages.

Run: `grep -rn RunOnceForTest --include='*.go' .`
Expected: no output.

- [ ] **Step 6: Full check, then commit**

Run the full check from Global Constraints. Expected: all clean.

```bash
git add internal/platform/jobs cmd/onsuite/backup_test.go internal/platform/admin/admin_test.go
git commit -m "feat(platform): trigger jobs on demand in the scheduler (#311)"
```

---

### Task 2: `jobsadmin` package — the `/admin/jobs` page

**Files:**
- Create: `internal/platform/jobsadmin/jobsadmin.go`
- Create: `internal/ui/templates/admin_jobs.html`
- Create: `internal/platform/jobsadmin/fixture_test.go`
- Create: `internal/platform/jobsadmin/jobsadmin_test.go`
- Modify: `internal/ui/static/app.css` (the `.admin-tag-*` rules, ~line 847)
- Modify: `internal/arch/arch_test.go:234` (add the package to `TestScanSeesTheRealTree`)

**Interfaces:**
- Consumes (Task 1): `(*jobs.Registry).Trigger(slug string) error`, `(*jobs.Registry).Wait()`, `(*jobs.Registry).Snapshot() []jobs.Status`, `jobs.ErrUnknownJob`, `jobs.ErrAlreadyRunning`, `jobs.Status{Slug, Running, LastManual, ...}`.
- Existing platform APIs used: `app.NewPage(r *http.Request, title string, nav []render.NavItem) render.Page`; `web.WithActiveApp(ctx, id) context.Context`; `web.UserFrom(ctx) (auth.User, bool)`; `web.CSRFToken(ctx) string`; `(*web.Errors).NotFound(w, r)`, `.Internal(w, r, err)`; `(*render.Renderer).Page(w, status, name, page) error`, `.Fragment(w, status, page, block, data) error`; `(*web.Recorder).Handle(mux, pattern, public, h)` (nil receiver is fine); `(*web.Auth).RequireAdmin(h) http.Handler`. Platform templates in `internal/ui/templates/` are registered by file basename, so `admin_jobs.html` is page `"admin_jobs"`. The template func `csrfField` returns the form field name.
- Produces (for Task 3):
  ```go
  package jobsadmin
  type Deps struct {
      Jobs    *jobs.Registry
      Render  *render.Renderer
      Errors  *web.Errors
      Log     *slog.Logger
      Nav     []render.NavItem
      Version string
  }
  func Routes(mux *http.ServeMux, rec *web.Recorder, authn *web.Auth, d Deps)
  ```

- [ ] **Step 1: Write the test fixture**

Create `internal/platform/jobsadmin/fixture_test.go`. It mirrors `internal/platform/usermgmt/fixture_test.go`, with a jobs registry in place of user management:

```go
package jobsadmin_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
	"github.com/iliafrenkel/on-suite/internal/platform/jobsadmin"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

type session struct{ cookies []*http.Cookie }

// server is the real middleware stack over a real database file, with the
// jobs routes mounted exactly as buildStack mounts them, one admin ("root")
// and one ordinary user ("ilia"), and a registry the test fills in.
type server struct {
	handler http.Handler
	users   *auth.Store
	jobs    *jobs.Registry
	logs    *bytes.Buffer
	root    *session
	plain   *session
}

// newServer builds the stack around reg, which the caller has already
// registered its jobs on.
func newServer(t *testing.T, reg *jobs.Registry) *server {
	t.Helper()
	ctx := context.Background()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	ms, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatal(err)
	}
	users := auth.NewStore(handle)

	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatal(err)
	}
	rend, err := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL, CSRFFieldName: web.CSRFFormField})
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logs, nil))
	errs := web.NewErrors(rend, log)
	csrf := web.NewCSRF(false, errs)
	authn := web.NewAuth(web.AuthOptions{Users: users, Render: rend, Errors: errs, CSRF: csrf, Log: log, Version: "test"})

	mux := http.NewServeMux()
	authn.Routes(mux, nil)
	jobsadmin.Routes(mux, nil, authn, jobsadmin.Deps{
		Jobs: reg, Render: rend, Errors: errs, Log: log, Version: "test",
	})
	mux.Handle("/", http.HandlerFunc(errs.NotFound))

	s := &server{handler: web.Stack(mux, log, errs, csrf, authn), users: users, jobs: reg, logs: logs}
	if _, err := users.CreateUser(ctx, "root", apptest.PasswordHash, true); err != nil {
		t.Fatal(err)
	}
	if _, err := users.CreateUser(ctx, "ilia", apptest.PasswordHash, false); err != nil {
		t.Fatal(err)
	}
	s.root = s.logIn(t, "root")
	s.plain = s.logIn(t, "ilia")
	return s
}

func (s *server) do(t *testing.T, sess *session, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	if sess != nil {
		for _, c := range sess.cookies {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

// anonymous returns a browser holding only a CSRF cookie.
func (s *server) anonymous(t *testing.T) *session {
	t.Helper()
	page := s.do(t, nil, httptest.NewRequest("GET", "/login", nil))
	for _, c := range page.Result().Cookies() {
		if c.Name == web.CSRFCookieName {
			return &session{cookies: []*http.Cookie{c}}
		}
	}
	t.Fatal("GET /login issued no CSRF cookie")
	return nil
}

func (s *server) logIn(t *testing.T, username string) *session {
	t.Helper()
	rec := s.post(t, s.anonymous(t), "/login", url.Values{"username": {username}, "password": {apptest.Password}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login for %s = %d; body: %s", username, rec.Code, rec.Body.String())
	}
	return &session{cookies: rec.Result().Cookies()}
}

func (s *server) get(t *testing.T, sess *session, path string) *httptest.ResponseRecorder {
	t.Helper()
	return s.do(t, sess, httptest.NewRequest("GET", path, nil))
}

// post submits a form carrying the session's own CSRF token.
func (s *server) post(t *testing.T, sess *session, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	for _, c := range sess.cookies {
		if c.Name == web.CSRFCookieName {
			form.Set(web.CSRFFormField, c.Value)
		}
	}
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.do(t, sess, req)
}

func doc(t *testing.T, rec *httptest.ResponseRecorder) *htmlassert.Doc {
	t.Helper()
	return htmlassert.Parse(t, rec.Body.String())
}

// blockingJob registers a job that runs until release is closed.
func blockingJob(reg *jobs.Registry, name string) (release chan struct{}) {
	release = make(chan struct{})
	reg.Register(name, "waits to be released", time.Hour, func(context.Context) error {
		<-release
		return nil
	})
	return release
}
```

Add `"time"` to that import list.

- [ ] **Step 2: Write the failing handler tests**

Create `internal/platform/jobsadmin/jobsadmin_test.go`:

```go
package jobsadmin_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
)

// twoJobs is a registry with one enabled and one disabled job.
func twoJobs() *jobs.Registry {
	reg := jobs.NewRegistry()
	reg.Register("nightly thing", "does a nightly thing", time.Hour, func(context.Context) error { return nil })
	reg.Register("disabled thing", "would do a thing", 0, func(context.Context) error { return nil })
	return reg
}

func TestAnonymousIsSentToLoginFromTheJobsPage(t *testing.T) {
	s := newServer(t, twoJobs())
	for _, path := range []string{"/admin/jobs", "/admin/jobs/table"} {
		rec := s.get(t, nil, path)
		if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
			t.Errorf("GET %s: status = %d, Location = %q; want 303 to /login", path, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestANonAdminGetsTheSame404AsAMissingPageOnEveryJobsRoute(t *testing.T) {
	s := newServer(t, twoJobs())
	missing := s.get(t, s.plain, "/no-such-page")
	for _, path := range []string{"/admin/jobs", "/admin/jobs/table"} {
		got := s.get(t, s.plain, path)
		if got.Code != http.StatusNotFound || got.Body.String() != missing.Body.String() {
			t.Errorf("GET %s as non-admin = %d, want the missing-page 404", path, got.Code)
		}
	}

	missingPost := s.post(t, s.plain, "/no-such-page", url.Values{})
	rec := s.post(t, s.plain, "/admin/jobs/nightly-thing/run", url.Values{})
	if rec.Code != http.StatusNotFound || rec.Body.String() != missingPost.Body.String() {
		t.Errorf("POST run as non-admin = %d, want the missing-page 404", rec.Code)
	}
	if s.jobs.Snapshot()[0].Runs != 0 {
		t.Error("a non-admin's POST ran the job")
	}
}

func TestTheJobsPageListsEveryJobWithARunButton(t *testing.T) {
	s := newServer(t, twoJobs())
	rec := s.get(t, s.root, "/admin/jobs")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	d := doc(t, rec)
	body := d.Text()
	for _, want := range []string{"nightly thing", "does a nightly thing", "disabled thing", "disabled"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not mention %q", want)
		}
	}
	d.MustHave(`form[action="/admin/jobs/nightly-thing/run"]`)
	d.MustHave(`form[action="/admin/jobs/disabled-thing/run"]`)
	d.MustNotHave("[hx-trigger]")
}

func TestRunNowTriggersTheJobAndRedirectsBack(t *testing.T) {
	s := newServer(t, twoJobs())
	rec := s.post(t, s.root, "/admin/jobs/disabled-thing/run", url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/jobs" {
		t.Fatalf("status = %d, Location = %q; want 303 to /admin/jobs", rec.Code, rec.Header().Get("Location"))
	}
	s.jobs.Wait()

	st := s.jobs.Snapshot()[1]
	if st.Runs != 1 || !st.LastManual {
		t.Errorf("Runs = %d, LastManual = %v; want one manual run", st.Runs, st.LastManual)
	}
	if !strings.Contains(s.logs.String(), "job triggered") || !strings.Contains(s.logs.String(), "by=root") {
		t.Errorf("no audit log line for the trigger; logs:\n%s", s.logs.String())
	}

	page := doc(t, s.get(t, s.root, "/admin/jobs"))
	if !strings.Contains(page.Text(), "(manual)") {
		t.Error("the page does not mark the last run as manual")
	}
}

func TestRunNowWithoutACSRFTokenIsRejected(t *testing.T) {
	s := newServer(t, twoJobs())
	req := httptestPost("/admin/jobs/nightly-thing/run")
	rec := s.do(t, s.root, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	s.jobs.Wait()
	if s.jobs.Snapshot()[0].Runs != 0 {
		t.Error("a POST without a CSRF token ran the job")
	}
}

func TestRunNowOfAnUnknownJobIs404(t *testing.T) {
	s := newServer(t, twoJobs())
	if rec := s.post(t, s.root, "/admin/jobs/nope/run", url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestRunNowOfARunningJobIs422WithANotice(t *testing.T) {
	reg := jobs.NewRegistry()
	release := blockingJob(reg, "slow thing")
	s := newServer(t, reg)
	defer func() { close(release); reg.Wait() }()

	if rec := s.post(t, s.root, "/admin/jobs/slow-thing/run", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("first run = %d, want 303", rec.Code)
	}
	rec := s.post(t, s.root, "/admin/jobs/slow-thing/run", url.Values{})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("second run = %d, want 422", rec.Code)
	}
	notice := doc(t, rec).MustHave(".notice-error")
	if got := htmlassert.Text(notice); !strings.Contains(got, "slow thing is already running") {
		t.Errorf("notice = %q", got)
	}
}

func TestTheTablePollsOnlyWhileAJobIsRunning(t *testing.T) {
	reg := jobs.NewRegistry()
	release := blockingJob(reg, "slow thing")
	s := newServer(t, reg)

	if err := reg.Trigger("slow-thing"); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/admin/jobs", "/admin/jobs/table"} {
		d := doc(t, s.get(t, s.root, path))
		poll := d.MustHave(`[hx-trigger]`)
		if v, _ := htmlassert.Attr(poll, "hx-get"); v != "/admin/jobs/table" {
			t.Errorf("GET %s: polling element hx-get = %q", path, v)
		}
		d.MustHave(".admin-tag-running")
		d.MustHave(`button[disabled]`)
	}

	close(release)
	reg.Wait()

	d := doc(t, s.get(t, s.root, "/admin/jobs/table"))
	d.MustNotHave("[hx-trigger]")
	d.MustHave(".admin-tag-ok")
}

func TestAFailedManualRunShowsItsError(t *testing.T) {
	reg := jobs.NewRegistry()
	reg.Register("broken thing", "", time.Hour, func(context.Context) error { return errors.New("disk on fire") })
	s := newServer(t, reg)

	s.post(t, s.root, "/admin/jobs/broken-thing/run", url.Values{})
	reg.Wait()

	d := doc(t, s.get(t, s.root, "/admin/jobs"))
	d.MustHave(".admin-tag-failed")
	if !strings.Contains(d.Text(), "disk on fire") {
		t.Error("the failed run's error text is not shown")
	}
}
```

Add this helper at the bottom of the same file (and `"net/http/httptest"` to its imports):

```go
// httptestPost builds a form POST with no CSRF token in it.
func httptestPost(path string) *http.Request {
	req := httptest.NewRequest("POST", path, strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}
```

(The CSRF middleware rejects a missing token with `http.StatusForbidden` — `internal/platform/web/csrf.go:80`.)

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/platform/jobsadmin/... -count=1`
Expected: build failure — `package .../internal/platform/jobsadmin is not in std` / `undefined: jobsadmin.Routes`.

- [ ] **Step 4: Write the package**

Create `internal/platform/jobsadmin/jobsadmin.go`:

```go
// Package jobsadmin lets admins run background jobs on demand at
// /admin/jobs (#311, spec docs/superpowers/specs/2026-09-27-trigger-jobs-design.md).
//
// It is a sibling of package admin, not part of it: /admin/ is promised to be
// read-only, and the POST here starts work. Like admin and usermgmt, it sits
// at the top of the platform and nothing imports it but cmd/onsuite.
package jobsadmin

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// Deps is everything the handlers use, assembled once by buildStack.
type Deps struct {
	Jobs    *jobs.Registry
	Render  *render.Renderer
	Errors  *web.Errors
	Log     *slog.Logger
	Nav     []render.NavItem
	Version string
}

// Routes registers every route this package serves. buildStack and the tests
// both call it, so the patterns (and their guards) exist in one place.
//
// Each pattern is exact. None ends in "/", so ServeMux never synthesizes an
// unguarded redirect that would let a non-admin tell these paths apart from a
// genuine 404 (see the /admin registration in buildStack).
func Routes(mux *http.ServeMux, rec *web.Recorder, authn *web.Auth, d Deps) {
	h := &handlers{d: d}
	admin := func(f http.HandlerFunc) http.Handler { return authn.RequireAdmin(f) }

	rec.Handle(mux, "GET /admin/jobs", false, admin(h.list))
	rec.Handle(mux, "GET /admin/jobs/table", false, admin(h.table))
	rec.Handle(mux, "POST /admin/jobs/{slug}/run", false, admin(h.run))
}

type handlers struct{ d Deps }

// jobsTable is the view model for the "jobs-table" block, which both the
// page and the polling fragment render. The fragment has no page shell, so
// the CSRF token the Run now forms need travels here rather than in Shell.
type jobsTable struct {
	Jobs      []jobs.Status
	CSRFToken string
	// Polling is true while any job runs; the block then carries the
	// hx-trigger that re-fetches it, and drops it once nothing runs, which
	// is what stops the polling (spec §4.2).
	Polling bool
}

// jobsPage is the view model for admin_jobs.html.
type jobsPage struct {
	Error string
	Table jobsTable
}

func (h *handlers) snapshot(r *http.Request) jobsTable {
	t := jobsTable{Jobs: h.d.Jobs.Snapshot(), CSRFToken: web.CSRFToken(r.Context())}
	for _, s := range t.Jobs {
		if s.Running {
			t.Polling = true
		}
	}
	return t
}

func (h *handlers) render(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	r = r.WithContext(web.WithActiveApp(r.Context(), "admin"))
	page := app.NewPage(r, "Jobs", h.d.Nav)
	page.Shell.Version = h.d.Version
	page.Data = jobsPage{Error: errMsg, Table: h.snapshot(r)}
	if err := h.d.Render.Page(w, status, "admin_jobs", page); err != nil {
		h.d.Errors.Internal(w, r, err)
	}
}

func (h *handlers) list(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "")
}

func (h *handlers) table(w http.ResponseWriter, r *http.Request) {
	if err := h.d.Render.Fragment(w, http.StatusOK, "admin_jobs", "jobs-table", h.snapshot(r)); err != nil {
		h.d.Errors.Internal(w, r, err)
	}
}

func (h *handlers) run(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	err := h.d.Jobs.Trigger(slug)
	switch {
	case errors.Is(err, jobs.ErrUnknownJob):
		h.d.Errors.NotFound(w, r)
	case errors.Is(err, jobs.ErrAlreadyRunning):
		h.render(w, r, http.StatusUnprocessableEntity, h.name(slug)+" is already running.")
	case err != nil:
		h.d.Errors.Internal(w, r, err)
	default:
		me, _ := web.UserFrom(r.Context())
		h.d.Log.Info("job triggered", "job", h.name(slug), "by", me.Username)
		http.Redirect(w, r, "/admin/jobs", http.StatusSeeOther)
	}
}

// name returns the display name of the job with this slug, or the slug
// itself if it has vanished (it cannot: jobs are only registered at start).
func (h *handlers) name(slug string) string {
	for _, s := range h.d.Jobs.Snapshot() {
		if s.Slug == slug {
			return s.Name
		}
	}
	return slug
}
```

- [ ] **Step 5: Write the template**

Create `internal/ui/templates/admin_jobs.html`. The columns mirror the `#jobs` table in `internal/ui/templates/admin.html` (lines 80-107), plus Run now:

```html
{{define "content"}}
<div class="stack">
	<p class="faint"><a href="/admin/">Admin</a> / Jobs</p>
	<h1>Jobs</h1>

	{{with .Data.Error}}<div class="notice notice-error" role="alert">{{.}}</div>{{end}}

	<section class="admin-section" id="jobs">
		<p class="faint">Background work this process runs on a timer. <strong>Run now</strong> starts a job straight away without changing its schedule. Status is in memory and resets when the server restarts.</p>
		{{template "jobs-table" .Data.Table}}
	</section>
</div>
{{end}}

{{define "jobs-table"}}
<div class="scroll-x" id="jobs-table"{{if .Polling}} hx-get="/admin/jobs/table" hx-trigger="every 2s" hx-swap="outerHTML"{{end}}>
	<table class="admin-table">
		<thead><tr><th>Job</th><th>Every</th><th>Last run</th><th>Took</th><th>Outcome</th><th>Next run</th><th>Runs</th><th><span class="visually-hidden">Actions</span></th></tr></thead>
		<tbody>
		{{$csrf := .CSRFToken}}
		{{range .Jobs}}
			<tr>
				<td>{{.Name}}<br><span class="faint">{{.Description}}</span></td>
				<td>{{if .Enabled}}{{.Interval}}{{else}}<span class="admin-tag admin-tag-off">disabled</span>{{end}}</td>
				<td>{{if .LastRun.IsZero}}<span class="dim">never</span>{{else}}{{.LastRun.Format "2006-01-02 15:04:05 MST"}}{{if .LastManual}} <span class="faint">(manual)</span>{{end}}{{end}}</td>
				<td class="dim">{{if .LastRun.IsZero}}—{{else}}{{.LastDuration}}{{end}}</td>
				<td>
					{{if .Running}}<span class="admin-tag admin-tag-running">running</span>
					{{else if .LastRun.IsZero}}<span class="dim">—</span>
					{{else if .LastErr}}<span class="admin-tag admin-tag-failed">failed</span> <span class="faint">{{.LastErr}}</span>
					{{else}}<span class="admin-tag admin-tag-ok">ok</span>{{end}}
				</td>
				<td class="dim">{{if .NextRun.IsZero}}—{{else}}{{.NextRun.Format "2006-01-02 15:04:05 MST"}}{{end}}</td>
				<td>{{.Runs}}</td>
				<td>
					<form method="post" action="/admin/jobs/{{.Slug}}/run">
						<input type="hidden" name="{{csrfField}}" value="{{$csrf}}">
						<button type="submit"{{if .Running}} disabled{{end}}>Run now</button>
					</form>
				</td>
			</tr>
		{{else}}
			<tr><td colspan="8" class="dim">No jobs are registered.</td></tr>
		{{end}}
		</tbody>
	</table>
</div>
{{end}}
```


- [ ] **Step 6: Add the `running` tag style**

In `internal/ui/static/app.css`, extend the accent group so a running tag reads as active:

```css
.admin-tag-ok,
.admin-tag-running,
.admin-tag-flag { background: var(--c-accent-bg); color: var(--c-accent); }
```

(Replace the existing two-selector `.admin-tag-ok,\n.admin-tag-flag { … }` rule; do not add a second rule.)

- [ ] **Step 7: Register the package with the arch test**

In `internal/arch/arch_test.go`, `TestScanSeesTheRealTree`, add after `"internal/platform/usermgmt",`:

```go
		"internal/platform/jobsadmin",
```

(The scan walks the source tree with `filepath.WalkDir`, so the entry passes as soon as the package exists — it does not need to be imported yet.)

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/platform/jobsadmin/... ./internal/arch/... ./internal/ui/... -race -count=1`
Expected: `ok` for every package.

- [ ] **Step 9: Full check, then commit**

Run the full check from Global Constraints. Expected: all clean.

```bash
git add internal/platform/jobsadmin internal/ui/templates/admin_jobs.html internal/ui/static internal/arch/arch_test.go
git commit -m "feat(platform): /admin/jobs page to run jobs on demand (#311)"
```

---

### Task 3: Wire it up, link from `/admin/`, docs

**Files:**
- Modify: `cmd/onsuite/stack.go` (mount after the `usermgmt.Routes` call, ~line 128)
- Modify: `internal/ui/templates/admin.html:80-107` (Jobs section)
- Modify: `internal/platform/admin/admin_test.go` (new link test)
- Modify: `AGENTS.md` (operations paragraphs)
- Modify: `docs/DEPLOYING.md` (Backups section, line ~78)
- Modify: `docs/superpowers/specs/2026-08-24-admin-page-design.md` (§12)
- Create: `cmd/onsuite/stack_jobs_test.go`

**Interfaces:**
- Consumes (Task 2): `jobsadmin.Routes(mux *http.ServeMux, rec *web.Recorder, authn *web.Auth, d jobsadmin.Deps)`, `jobsadmin.Deps{Jobs, Render, Errors, Log, Nav, Version}`.
- In `buildStack` the in-scope names are `mux`, `routes` (the `*web.Recorder`), `authn`, `rend`, `errs`, and `deps` (with `deps.Jobs`, `deps.Log`, `deps.Version`, `deps.Registry.NavItems()`).

- [ ] **Step 1: Write the failing tests**

In `internal/platform/admin/admin_test.go`, next to `TestTheUsersSectionLinksToUserManagement`:

```go
func TestTheJobsSectionLinksToTheJobsPage(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, s.admin, "/admin/")
	htmlassert.Parse(t, rec.Body.String()).MustHave(`#jobs a[href="/admin/jobs"]`)
}
```

Create `cmd/onsuite/stack_jobs_test.go`. An anonymous request to a guarded route that is mounted gets a 303 to `/login`; one to an unmounted path falls through to the public catch-all 404. So a 303 proves the route is mounted, and guarded. The setup copies `TestDisabledAppIsUnreachableAndOmittedFromExportAndStats` in `cmd/onsuite/database_test.go`:

```go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
)

func TestBuildStackMountsTheJobsPage(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir()}
	handle, registry, _, err := openDatabase(context.Background(), cfg)
	if err != nil {
		t.Fatalf("openDatabase: %v", err)
	}
	defer func() { _ = handle.Close() }()

	stack, err := buildStack(stackDeps{
		DB:       handle,
		Users:    auth.NewStore(handle),
		Registry: registry,
		Jobs:     jobs.NewRegistry(),
		Log:      slog.New(slog.DiscardHandler),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("buildStack: %v", err)
	}

	for _, path := range []string{"/admin/jobs", "/admin/jobs/table"} {
		rec := httptest.NewRecorder()
		stack.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
			t.Errorf("anonymous GET %s = %d, Location %q; want a 303 to /login from a mounted, guarded route", path, rec.Code, rec.Header().Get("Location"))
		}
	}
}
```

If `stackDeps` has no `Jobs` field under that exact name, read `cmd/onsuite/stack.go:25-40` and use the field it has (it is `Jobs *jobs.Registry` at the time of writing).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/platform/admin/... ./cmd/onsuite/... -count=1`
Expected: FAIL — `#jobs a[href="/admin/jobs"]` not found; the `/admin/jobs` stack test gets 404 / the route is not recorded.

- [ ] **Step 3: Mount the routes in `buildStack`**

In `cmd/onsuite/stack.go`, add the import `"github.com/iliafrenkel/on-suite/internal/platform/jobsadmin"` and, directly after the `usermgmt.Routes(...)` call:

```go
	// Running a job on demand starts work, so it lives beside the read-only
	// admin page rather than inside it, like user management (#311).
	if deps.Jobs != nil {
		jobsadmin.Routes(mux, routes, authn, jobsadmin.Deps{
			Jobs:    deps.Jobs,
			Render:  rend,
			Errors:  errs,
			Log:     deps.Log,
			Nav:     deps.Registry.NavItems(),
			Version: deps.Version,
		})
	}
```

(`deps.Jobs` can be nil in tests that build the stack without a scheduler — the existing `RegisterJobs` call above guards the same way.)

- [ ] **Step 4: Add the link on `/admin/`**

In `internal/ui/templates/admin.html`, inside `<section class="admin-section" id="jobs">`, directly after the closing `</div>` of the `scroll-x` wrapper and before `</section>`:

```html
		<p><a href="/admin/jobs">Manage jobs →</a></p>
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/platform/admin/... ./cmd/onsuite/... ./internal/arch/... -race -count=1`
Expected: `ok`. If Task 2 Step 7 was deferred here, add the arch entry now and confirm it passes.

- [ ] **Step 6: Update the docs**

`AGENTS.md` — in the "Two platform packages exist only for operations." paragraph, change the jobs sentence to:

```markdown
[internal/platform/jobs](internal/platform/jobs/jobs.go) is a generic interval
scheduler that remembers how each run went and can run any job on demand
(`Trigger`); it takes closures and imports nothing else in the module, so it
never learns what a backup is.
```

and after the `usermgmt` paragraph add:

```markdown
[internal/platform/jobsadmin](internal/platform/jobsadmin/jobsadmin.go) is the
other writable sibling: `/admin/jobs` (admin-only, same 404 guard) lists every
job with a **Run now** button, runs it in the background, and polls the table
with HTMX until it finishes. Its design is in
[docs/superpowers/specs/2026-09-27-trigger-jobs-design.md](docs/superpowers/specs/2026-09-27-trigger-jobs-design.md).
```

`docs/DEPLOYING.md` — in `## Backups`, after the `onsuite backup --data-dir …` code block, add:

```markdown
To take a one-off snapshot from the browser — before an upgrade, say — open
**Admin → Manage jobs** and press **Run now** on *database snapshot*. It works
even with `--backup-interval 0`, and it does not change the schedule.
```

`docs/superpowers/specs/2026-08-24-admin-page-design.md` — in §12, change the first bullet to:

```markdown
- Any control that changes state — trigger a backup, sweep sessions, create or
  promote a user, delete another user's data. (Users: done in
  [2026-09-27-user-management-design.md](2026-09-27-user-management-design.md).
  Running jobs: done in
  [2026-09-27-trigger-jobs-design.md](2026-09-27-trigger-jobs-design.md). Both
  live on sibling pages, so `/admin/` itself stays read-only.)
```

(If the user-management spec already added its own note to that bullet, merge into it rather than duplicating.)

- [ ] **Step 7: Check it in the browser**

Start the dev server via the preview tools (`.claude/launch.json` — create an entry running `go run ./cmd/onsuite serve --data-dir ./data` if none exists), sign in as an admin, open `/admin/`, follow **Manage jobs →**, press **Run now** on *database snapshot*, and confirm: the row shows **running** (briefly), then **ok** with "(manual)" after Last run, Next run unchanged, and a new file in `./data/backups`. Check the console for CSP errors. Take a screenshot for the PR.

- [ ] **Step 8: Full check, then commit**

Run the full check from Global Constraints. Expected: all clean.

```bash
git add cmd/onsuite internal/ui/templates/admin.html internal/platform/admin/admin_test.go AGENTS.md docs/DEPLOYING.md docs/superpowers/specs/2026-08-24-admin-page-design.md
git commit -m "feat(platform): mount /admin/jobs and link it from the admin page (#311)"
```
