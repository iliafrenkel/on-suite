# Admin Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a read-only administrator page at `/admin/` that shows settings, stats, job status, database health, accounts and the route map — and the platform machinery it needs.

**Architecture:** A new platform package `internal/platform/admin` renders one page from seven independent collectors. Two new low-level packages feed it: `internal/platform/jobs` (a generic interval scheduler that remembers outcomes) and a route recorder in `internal/platform/web`. Per-app numbers arrive through a new optional `app.Stater` capability discovered by type assertion, exactly like the existing `app.Exporter`, so the platform still never imports an app.

**Tech Stack:** Go 1.26, `html/template`, `modernc.org/sqlite`, HTMX (not used by this page), no new dependencies.

**Spec:** [docs/superpowers/specs/2026-08-24-admin-page-design.md](../specs/2026-08-24-admin-page-design.md). Read it before Task 1.

## Global Constraints

- No CGO, ever. Every dependency must be pure Go.
- No Node, no npm, no JS build step. HTMX is the only JavaScript, vendored under `internal/ui/static/`.
- **No new module dependencies.** Everything here uses the standard library and packages already in `go.mod`.
- `main` is protected. Work on the `admin-page` branch and open a PR; never push to `main`.
- Migrations are forward-only. **This plan adds no migration at all** — every query it introduces is a read.
- Apps never import each other; the platform never imports an app. Enforced by `internal/arch/arch_test.go`.
- `internal/ui` must stay a leaf: embedded CSS/JS/templates, no imports.
- CSS must not introduce new colours or spacing values — compose the tokens in `:root` in `internal/ui/static/app.css`.
- Strict CSP: no inline `<script>`, no `style=` attributes anywhere.
- The full check must be green at every commit:
  ```bash
  gofmt -l . && go vet ./... && go test ./... -race -count=1
  ```
- Every value shown on the admin page is read-only. **No task in this plan may add a POST route, a form, or a button that changes state.**

---

## File Structure

**New files**

| File | Responsibility |
|---|---|
| `internal/platform/jobs/jobs.go` | Interval scheduler: register named jobs, run them, remember outcomes. Imports nothing from this module. |
| `internal/platform/jobs/jobs_test.go` | Tests for the above. |
| `internal/platform/web/routes.go` | `Route` and `Recorder`: one record of every route the process serves. |
| `internal/platform/web/routes_test.go` | Tests for the above. |
| `internal/platform/admin/admin.go` | `Deps`, `Handler`, and the `Report` assembly. |
| `internal/platform/admin/collect.go` | The seven collectors, one function each. |
| `internal/platform/admin/format.go` | Byte and time formatting for the page. |
| `internal/platform/admin/admin_test.go` | Authorization and rendering tests through the real middleware stack. |
| `internal/ui/templates/admin.html` | The page markup. |

**Modified files**

| File | Change |
|---|---|
| `internal/platform/config/config.go` | Default constants, a settings descriptor list, `Settings()`. |
| `internal/platform/web/login.go` | `RequireAdmin`; `Routes` takes a recorder. |
| `internal/platform/app/app.go` | `Stat`, `Stater`, `AppStats`, `Registry.Stats`, `Registry.RecordRoutes`. |
| `internal/platform/app/router.go` | `Route` becomes an alias of `web.Route`; registrations forward to the recorder. |
| `internal/platform/auth/store.go` | `Account`, `ListAccounts`, `SessionCounts`. |
| `internal/apps/paste/store.go` | `Store.Stats`. |
| `internal/apps/paste/paste.go` | `App.Stats`, implementing `app.Stater`. |
| `cmd/onsuite/backup.go` | `runMaintenance`/`maintain` replaced by `registerMaintenance`. |
| `cmd/onsuite/serve.go` | Build the job registry, capture the start time. |
| `cmd/onsuite/stack.go` | Recorder, admin route, extra `stackDeps` fields. |
| `internal/ui/templates/base.html` | Admin link in the sidebar, admin-only. |
| `internal/ui/icons.go` | An `"admin"` icon. |
| `internal/ui/static/app.css` | `.admin-*` styles. |
| `internal/arch/arch_test.go` | `jobs` and `admin` added to the scanned set; `jobs` added to the layering rules. |
| `AGENTS.md`, `NEXT.md`, `docs/DEPLOYING.md` | Documentation. |

---

### Task 1: The jobs package

A generic interval scheduler. It takes closures and knows nothing about backups, sessions, SQLite or HTTP — that is what lets the admin page show job status without the platform depending on what any job does.

**Files:**
- Create: `internal/platform/jobs/jobs.go`
- Create: `internal/platform/jobs/jobs_test.go`
- Modify: `internal/arch/arch_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `jobs.NewRegistry() *jobs.Registry`
  - `(*Registry).Register(name, description string, every time.Duration, fn func(context.Context) error)`
  - `(*Registry).Run(ctx context.Context)` — blocks until ctx is done
  - `(*Registry).Snapshot() []jobs.Status`
  - `(*Registry).SetClock(now func() time.Time)`
  - `jobs.Status{Name, Description string; Interval time.Duration; Enabled bool; Runs int; LastRun time.Time; LastDuration time.Duration; LastErr string; NextRun time.Time}`

- [ ] **Step 1: Write the failing tests**

Create `internal/platform/jobs/jobs_test.go`:

```go
package jobs_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
)

// stepClock advances by a fixed step on every call, so a duration measured
// across two calls is exact rather than however long the machine took.
func stepClock(start time.Time, step time.Duration) func() time.Time {
	var mu sync.Mutex
	now := start
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		out := now
		now = now.Add(step)
		return out
	}
}

func TestRegisterListsAJobBeforeItHasEverRun(t *testing.T) {
	reg := jobs.NewRegistry()
	reg.Register("sweep", "removes old rows", time.Hour, func(context.Context) error { return nil })

	got := reg.Snapshot()
	if len(got) != 1 {
		t.Fatalf("Snapshot() has %d jobs, want 1", len(got))
	}
	s := got[0]
	if s.Name != "sweep" || s.Description != "removes old rows" {
		t.Errorf("Snapshot()[0] = %+v, want name/description to survive registration", s)
	}
	if s.Interval != time.Hour || !s.Enabled {
		t.Errorf("Interval = %s, Enabled = %v; want 1h and enabled", s.Interval, s.Enabled)
	}
	if !s.LastRun.IsZero() || s.Runs != 0 {
		t.Errorf("a job that has not run reports LastRun = %v, Runs = %d", s.LastRun, s.Runs)
	}
}

func TestZeroIntervalRegistersADisabledJob(t *testing.T) {
	reg := jobs.NewRegistry()
	ran := false
	reg.Register("snapshot", "writes a backup", 0, func(context.Context) error {
		ran = true
		return nil
	})

	// Run must return immediately: there is nothing enabled to run.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reg.Run(ctx)

	s := reg.Snapshot()[0]
	if s.Enabled {
		t.Error("a job registered with interval 0 reports Enabled = true")
	}
	if !s.NextRun.IsZero() {
		t.Errorf("a disabled job reports NextRun = %v, want the zero time", s.NextRun)
	}
	if ran {
		t.Error("a disabled job ran")
	}
}

func TestRunRecordsSuccessDurationAndNextRun(t *testing.T) {
	reg := jobs.NewRegistry()
	start := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	reg.SetClock(stepClock(start, 10*time.Millisecond))

	done := make(chan struct{})
	var once sync.Once
	reg.Register("sweep", "removes old rows", 5*time.Millisecond, func(context.Context) error {
		once.Do(func() { close(done) })
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	go reg.Run(ctx)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the job never ran")
	}
	cancel()

	// Poll until the outcome is recorded: the job signals before Run records.
	var s jobs.Status
	for i := 0; i < 200; i++ {
		s = reg.Snapshot()[0]
		if s.Runs > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s.Runs == 0 {
		t.Fatal("the run was never recorded")
	}
	if s.LastRun.IsZero() {
		t.Error("LastRun is zero after a run")
	}
	if s.LastDuration != 10*time.Millisecond {
		t.Errorf("LastDuration = %s, want 10ms from the stepping clock", s.LastDuration)
	}
	if s.LastErr != "" {
		t.Errorf("LastErr = %q after a successful run", s.LastErr)
	}
	if !s.NextRun.After(s.LastRun) {
		t.Errorf("NextRun %v is not after LastRun %v", s.NextRun, s.LastRun)
	}
}

func TestAFailingJobIsRecordedAndTheRegistryKeepsGoing(t *testing.T) {
	reg := jobs.NewRegistry()
	reg.Register("sweep", "removes old rows", time.Hour, func(context.Context) error {
		return errors.New("disk on fire")
	})
	reg.RunOnceForTest(context.Background(), "sweep")

	s := reg.Snapshot()[0]
	if s.LastErr != "disk on fire" {
		t.Errorf("LastErr = %q, want the job's error text", s.LastErr)
	}
	if s.Runs != 1 {
		t.Errorf("Runs = %d after one failing run, want 1", s.Runs)
	}
}

func TestAPanickingJobBecomesAFailedJobRatherThanADeadProcess(t *testing.T) {
	reg := jobs.NewRegistry()
	reg.Register("boom", "explodes", time.Hour, func(context.Context) error {
		panic("kaboom")
	})
	reg.RunOnceForTest(context.Background(), "boom")

	s := reg.Snapshot()[0]
	if s.LastErr != "panic: kaboom" {
		t.Errorf("LastErr = %q, want %q", s.LastErr, "panic: kaboom")
	}
}

func TestASuccessfulRunClearsThePreviousError(t *testing.T) {
	reg := jobs.NewRegistry()
	fail := true
	reg.Register("flaky", "sometimes works", time.Hour, func(context.Context) error {
		if fail {
			return errors.New("nope")
		}
		return nil
	})

	reg.RunOnceForTest(context.Background(), "flaky")
	fail = false
	reg.RunOnceForTest(context.Background(), "flaky")

	if got := reg.Snapshot()[0].LastErr; got != "" {
		t.Errorf("LastErr = %q after a later success; a stale error is worse than none", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/platform/jobs/... -count=1`
Expected: FAIL — the package does not exist (`no Go files in .../jobs`).

- [ ] **Step 3: Write the implementation**

Create `internal/platform/jobs/jobs.go`:

```go
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/platform/jobs/... -race -count=1 -v`
Expected: PASS, all six tests.

- [ ] **Step 5: Teach the architecture test about the new package**

In `internal/arch/arch_test.go`, add an entry to the `forbidden` map in `TestLayering` (it currently ends with the `"internal/platform/web"` line):

```go
		"internal/platform/web":    {"internal/platform/app"},
		// jobs takes closures and nothing else. If it ever imports a platform
		// package, someone has taught the scheduler what a backup is.
		"internal/platform/jobs": {
			"internal/platform/web", "internal/platform/app", "internal/platform/render",
			"internal/platform/auth", "internal/platform/db", "internal/platform/config",
		},
```

And add `"internal/platform/jobs"` to the list of packages in `TestScanSeesTheRealTree`.

- [ ] **Step 6: Run the architecture test**

Run: `go test ./internal/arch/... -count=1 -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/platform/jobs internal/arch/arch_test.go
git commit -m "jobs: add an interval scheduler that remembers how each run went"
```

---

### Task 2: Move maintenance onto the job registry

Behaviour-preserving refactor. `runMaintenance` becomes two named, introspectable jobs. Nothing about *when* work happens changes.

**Files:**
- Modify: `cmd/onsuite/backup.go` (replace `runMaintenance` and `maintain`)
- Modify: `cmd/onsuite/serve.go:76-79`
- Test: `cmd/onsuite/backup_test.go`

**Interfaces:**
- Consumes: `jobs.NewRegistry`, `(*Registry).Register`, `(*Registry).Run`, `(*Registry).Snapshot`, `(*Registry).RunOnceForTest` from Task 1.
- Produces: `registerMaintenance(reg *jobs.Registry, handle *sql.DB, users *auth.Store, cfg config.Config, log *slog.Logger)` — used by `serve` in Task 9's wiring, and the two job names `"sweep expired sessions"` and `"database snapshot"`.

- [ ] **Step 1: Write the failing test**

Append to `cmd/onsuite/backup_test.go`:

```go
func TestRegisterMaintenanceRegistersBothJobsEnabled(t *testing.T) {
	dir := t.TempDir()
	handle := testDB(t, dir)

	reg := jobs.NewRegistry()
	registerMaintenance(reg, handle, auth.NewStore(handle),
		config.Config{DataDir: dir, BackupInterval: time.Hour, BackupKeep: 3},
		slog.New(slog.DiscardHandler))

	got := reg.Snapshot()
	if len(got) != 2 {
		t.Fatalf("registered %d jobs, want 2", len(got))
	}
	for _, s := range got {
		if !s.Enabled || s.Interval != time.Hour {
			t.Errorf("job %q: Enabled = %v, Interval = %s; want enabled at 1h", s.Name, s.Enabled, s.Interval)
		}
	}
	if got[0].Name != "sweep expired sessions" || got[1].Name != "database snapshot" {
		t.Errorf("job names = %q, %q", got[0].Name, got[1].Name)
	}
}

// A zero interval disabled runMaintenance entirely, including the session
// sweep, which `onsuite backup` then takes over. Splitting maintenance into
// two jobs must not quietly give the sweep a schedule of its own.
func TestRegisterMaintenanceDisablesBothJobsWhenTheIntervalIsZero(t *testing.T) {
	dir := t.TempDir()
	handle := testDB(t, dir)

	reg := jobs.NewRegistry()
	registerMaintenance(reg, handle, auth.NewStore(handle),
		config.Config{DataDir: dir, BackupInterval: 0, BackupKeep: 3},
		slog.New(slog.DiscardHandler))

	for _, s := range reg.Snapshot() {
		if s.Enabled {
			t.Errorf("job %q is enabled with --backup-interval 0", s.Name)
		}
	}
}

func TestTheSnapshotJobWritesASnapshot(t *testing.T) {
	dir := t.TempDir()
	handle := testDB(t, dir)

	reg := jobs.NewRegistry()
	registerMaintenance(reg, handle, auth.NewStore(handle),
		config.Config{DataDir: dir, BackupInterval: time.Hour, BackupKeep: 3},
		slog.New(slog.DiscardHandler))
	reg.RunOnceForTest(context.Background(), "database snapshot")

	entries, err := os.ReadDir(filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("backups directory holds %d files, want 1", len(entries))
	}
	if s := reg.Snapshot()[1]; s.LastErr != "" {
		t.Errorf("LastErr = %q after a good snapshot", s.LastErr)
	}
}
```

`testDB` is a helper — if `backup_test.go` does not already have one, add it:

```go
// testDB opens a migrated database in dir.
func testDB(t *testing.T, dir string) *sql.DB {
	t.Helper()
	handle, _, _, err := openDatabase(context.Background(), config.Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	return handle
}
```

Add whatever imports the file is missing: `context`, `database/sql`, `log/slog`, `os`, `path/filepath`, `time`, `github.com/iliafrenkel/on-suite/internal/platform/auth`, `.../config`, `.../jobs`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/onsuite/... -run TestRegisterMaintenance -count=1`
Expected: FAIL — `undefined: registerMaintenance`.

- [ ] **Step 3: Replace runMaintenance with registerMaintenance**

In `cmd/onsuite/backup.go`, delete `runMaintenance` and `maintain` entirely (keep `logSnapshotResult`, `writeSnapshot`, `pruneSnapshots`, `snapshotName` and `backupCmd` exactly as they are), and add:

```go
// registerMaintenance registers the server's two housekeeping jobs.
//
// Both take cfg.BackupInterval, so --backup-interval 0 disables both, exactly
// as the single runMaintenance ticker did before: a deployment that drives
// snapshots from cron or a systemd timer runs `onsuite backup`, which sweeps
// sessions itself (see backupCmd). Giving the sweep a schedule of its own
// would be a behaviour change, not a refactor.
//
// Naming the two halves is the point of the split: the admin page can say
// which one last failed, which a single "maintenance" ticker could not.
func registerMaintenance(
	reg *jobs.Registry,
	handle *sql.DB,
	users *auth.Store,
	cfg config.Config,
	log *slog.Logger,
) {
	if cfg.BackupInterval <= 0 {
		log.Info("internal maintenance schedule disabled")
	} else {
		log.Info("maintenance scheduled",
			"interval", cfg.BackupInterval.String(), "keep", cfg.BackupKeep)
	}

	reg.Register(
		"sweep expired sessions",
		"Deletes sessions past their expiry, so the sessions table does not grow forever.",
		cfg.BackupInterval,
		func(ctx context.Context) error {
			swept, err := users.DeleteExpiredSessions(ctx)
			if err != nil {
				log.Error("sweeping expired sessions failed", "error", err)
				return err
			}
			if swept > 0 {
				log.Info("expired sessions swept", "count", swept)
			}
			return nil
		},
	)

	reg.Register(
		"database snapshot",
		"Writes a consistent copy of the database into the backups directory and prunes old snapshots.",
		cfg.BackupInterval,
		func(ctx context.Context) error {
			path, err := writeSnapshot(ctx, handle, cfg.BackupDir(), cfg.BackupKeep, time.Now().UTC())
			logSnapshotResult(log, path, err)
			return err
		},
	)
}
```

Add `"github.com/iliafrenkel/on-suite/internal/platform/jobs"` to the imports; drop any import that is now unused (`errors` and `sort` are still used by `pruneSnapshots`, so check with `go vet`).

- [ ] **Step 4: Wire it into serve**

In `cmd/onsuite/serve.go`, replace these three lines:

```go
	maintenanceCtx, stopMaintenance := context.WithCancel(context.Background())
	defer stopMaintenance()
	go runMaintenance(maintenanceCtx, handle, users, cfg, log)
```

with:

```go
	maintenance := jobs.NewRegistry()
	registerMaintenance(maintenance, handle, users, cfg, log)
	jobsCtx, stopJobs := context.WithCancel(context.Background())
	defer stopJobs()
	go maintenance.Run(jobsCtx)
```

Add the `jobs` import.

- [ ] **Step 5: Run the tests**

Run: `go test ./cmd/onsuite/... -race -count=1`
Expected: PASS. The comment in `backup_test.go:73` mentioning `runMaintenance` now names a function that no longer exists — update it to say `registerMaintenance`.

- [ ] **Step 6: Commit**

```bash
git add cmd/onsuite
git commit -m "serve: run maintenance as two named jobs instead of one anonymous ticker"
```

---

### Task 3: Config introspection

**Files:**
- Modify: `internal/platform/config/config.go`
- Test: `internal/platform/config/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `config.Setting{Flag, Env, Value, Default, Doc string; Source config.Source}`
  - `config.Source` with `SourceDefault`, `SourceEnv`, `SourceFlag`, `SourceDerived` and a `String()` method
  - `(Config).Settings() []Setting`

**Note on `SourceDerived`** — spec §6 lists three sources. A fourth is needed: enabling `-tls-domain` moves the listen address to `:443` and forces `-secure-cookies` on, so those two values come from neither a flag, the environment, nor the default. Reporting them as "default" next to a default of `:8080` would be a puzzle the operator has to debug, which is the opposite of the section's purpose.

- [ ] **Step 1: Write the failing tests**

Append to `internal/platform/config/config_test.go`:

```go
func settingFor(t *testing.T, c config.Config, flag string) config.Setting {
	t.Helper()
	for _, s := range c.Settings() {
		if s.Flag == flag {
			return s
		}
	}
	t.Fatalf("Settings() has no entry for -%s", flag)
	return config.Setting{}
}

func TestSettingsReportDefaultsWhenNothingIsSet(t *testing.T) {
	c, err := config.Parse(nil, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	s := settingFor(t, c, "addr")
	if s.Value != ":8080" || s.Default != ":8080" {
		t.Errorf("addr Value/Default = %q/%q, want :8080/:8080", s.Value, s.Default)
	}
	if s.Source != config.SourceDefault {
		t.Errorf("addr Source = %v, want SourceDefault", s.Source)
	}
	if s.Env != "ONSUITE_ADDR" {
		t.Errorf("addr Env = %q", s.Env)
	}
	if s.Doc == "" {
		t.Error("addr Doc is empty; the flag's usage string should be carried through")
	}
}

func TestSettingsReportTheEnvironmentAsTheSource(t *testing.T) {
	env := func(k string) string {
		if k == "ONSUITE_ADDR" {
			return ":9999"
		}
		return ""
	}
	c, err := config.Parse(nil, env, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	s := settingFor(t, c, "addr")
	if s.Value != ":9999" {
		t.Errorf("addr Value = %q, want :9999", s.Value)
	}
	if s.Default != ":8080" {
		t.Errorf("addr Default = %q; the default must survive the environment overriding it", s.Default)
	}
	if s.Source != config.SourceEnv {
		t.Errorf("addr Source = %v, want SourceEnv", s.Source)
	}
}

func TestAnExplicitFlagBeatsTheEnvironmentInTheReportedSource(t *testing.T) {
	env := func(k string) string {
		if k == "ONSUITE_ADDR" {
			return ":9999"
		}
		return ""
	}
	c, err := config.Parse([]string{"-addr", ":7777"}, env, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	s := settingFor(t, c, "addr")
	if s.Value != ":7777" || s.Source != config.SourceFlag {
		t.Errorf("addr = %q from %v, want :7777 from SourceFlag", s.Value, s.Source)
	}
}

func TestTLSDerivedValuesAreReportedAsDerived(t *testing.T) {
	c, err := config.Parse([]string{"-tls-domain", "example.com"}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	addr := settingFor(t, c, "addr")
	if addr.Value != ":443" || addr.Source != config.SourceDerived {
		t.Errorf("addr = %q from %v, want :443 from SourceDerived", addr.Value, addr.Source)
	}
	secure := settingFor(t, c, "secure-cookies")
	if secure.Value != "true" || secure.Source != config.SourceDerived {
		t.Errorf("secure-cookies = %q from %v, want true from SourceDerived", secure.Value, secure.Source)
	}
}

func TestEverySettingIsDescribed(t *testing.T) {
	c, err := config.Parse(nil, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"addr", "data-dir", "tls-domain", "log-level",
		"backup-interval", "backup-keep", "tls-http-addr", "secure-cookies",
	}
	got := c.Settings()
	if len(got) != len(want) {
		t.Fatalf("Settings() has %d entries, want %d", len(got), len(want))
	}
	for i, flag := range want {
		if got[i].Flag != flag {
			t.Errorf("Settings()[%d].Flag = %q, want %q", i, got[i].Flag, flag)
		}
		if got[i].Doc == "" {
			t.Errorf("-%s has no Doc", flag)
		}
	}
}

func TestSettingsOnAHandBuiltConfigIsEmptyRatherThanWrong(t *testing.T) {
	// Commands like `onsuite backup` build a Config literal without parsing
	// flags. Reporting made-up settings for one would be worse than none.
	if got := (config.Config{DataDir: "./data"}).Settings(); len(got) != 0 {
		t.Errorf("Settings() on a literal Config returned %d entries, want 0", len(got))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/platform/config/... -count=1`
Expected: FAIL — `c.Settings undefined`, `undefined: config.Setting`.

- [ ] **Step 3: Add the default constants and the descriptor list**

In `internal/platform/config/config.go`, above `type Config struct`, add:

```go
// The true compile-time defaults, named so that both Parse and the settings
// descriptor list below can use them. They cannot be read back off the
// FlagSet: envOr folds the environment value into a flag's default before the
// flag is defined, so flag.Flag.DefValue reports the environment value.
const (
	defaultAddr           = ":8080"
	defaultDataDir        = "./data"
	defaultTLSDomain      = ""
	defaultLogLevel       = "info"
	defaultBackupInterval = 24 * time.Hour
	defaultBackupKeep     = 7
	defaultTLSHTTPAddr    = ":80"
	defaultSecureCookies  = false
)

// Source says where a setting's live value came from.
type Source int

const (
	SourceDefault Source = iota
	SourceEnv
	SourceFlag
	// SourceDerived is a value the server computed rather than read: enabling
	// TLS moves the listen address to :443 and forces Secure cookies on.
	SourceDerived
)

func (s Source) String() string {
	switch s {
	case SourceFlag:
		return "flag"
	case SourceEnv:
		return "environment"
	case SourceDerived:
		return "derived"
	default:
		return "default"
	}
}

// Setting is one configurable value with everything needed to explain it:
// what it is called, what it is set to, what it would otherwise have been,
// where the live value came from, and what it does.
//
// No setting is redacted, because none is a secret: every one is an address,
// a path, a duration or a boolean. The platform never accepts a password as a
// flag — `onsuite user add` reads from a terminal with echo disabled. If a
// credential-shaped setting is ever added, redaction here is a prerequisite,
// not a follow-up.
type Setting struct {
	Flag    string // "backup-interval"
	Env     string // "ONSUITE_BACKUP_INTERVAL"
	Value   string // the live value, formatted for display
	Default string // the true compile-time default
	Doc     string // the flag's usage string
	Source  Source
}

// settingSpecs names every setting once: its flag, its environment variable,
// and its true default. collectSettings panics if this list and the FlagSet
// disagree, so a flag added without an entry here fails immediately rather
// than silently vanishing from the admin page.
var settingSpecs = []struct{ flag, env, def string }{
	{"addr", "ONSUITE_ADDR", defaultAddr},
	{"data-dir", "ONSUITE_DATA_DIR", defaultDataDir},
	{"tls-domain", "ONSUITE_TLS_DOMAIN", defaultTLSDomain},
	{"log-level", "ONSUITE_LOG_LEVEL", defaultLogLevel},
	{"backup-interval", "ONSUITE_BACKUP_INTERVAL", defaultBackupInterval.String()},
	{"backup-keep", "ONSUITE_BACKUP_KEEP", strconv.Itoa(defaultBackupKeep)},
	{"tls-http-addr", "ONSUITE_TLS_HTTP_ADDR", defaultTLSHTTPAddr},
	{"secure-cookies", "ONSUITE_SECURE_COOKIES", strconv.FormatBool(defaultSecureCookies)},
}
```

Add a `settings []Setting` field to `Config`, at the end of the struct:

```go
	// settings records how each value above was resolved, for the admin page.
	// It is unexported so a hand-built Config literal reports nothing rather
	// than reporting defaults it never actually applied.
	settings []Setting
```

And the accessor, next to `DBPath`:

```go
// Settings describes every setting and where its live value came from. It is
// empty on a Config that was built as a literal rather than parsed.
func (c Config) Settings() []Setting {
	out := make([]Setting, len(c.settings))
	copy(out, c.settings)
	return out
}
```

- [ ] **Step 4: Replace the inline defaults in Parse with the constants**

Still in `Parse`, swap each literal for its constant — `envOr(getenv, "ONSUITE_ADDR", defaultAddr)`, `envOr(getenv, "ONSUITE_DATA_DIR", defaultDataDir)`, `envOr(getenv, "ONSUITE_TLS_DOMAIN", defaultTLSDomain)`, `envOr(getenv, "ONSUITE_LOG_LEVEL", defaultLogLevel)`, `envDuration(getenv, "ONSUITE_BACKUP_INTERVAL", defaultBackupInterval)`, `envInt(getenv, "ONSUITE_BACKUP_KEEP", defaultBackupKeep)`, `envOr(getenv, "ONSUITE_TLS_HTTP_ADDR", defaultTLSHTTPAddr)`, `envBool(getenv, "ONSUITE_SECURE_COOKIES", defaultSecureCookies)`. Behaviour is identical; the values now have one home.

- [ ] **Step 5: Collect the settings at the end of Parse**

At the very end of `Parse`, immediately before `return c, nil`:

```go
	c.settings = collectSettings(fs, getenv, explicit, c)

	return c, nil
```

And add, after `Parse`:

```go
// collectSettings builds the introspection list. It runs at the end of Parse,
// once every value has been resolved, so Value is what the server will
// actually use rather than what was asked for.
func collectSettings(fs *flag.FlagSet, getenv func(string) string, explicit map[string]bool, c Config) []Setting {
	live := map[string]string{
		"addr":            c.Addr,
		"data-dir":        c.DataDir,
		"tls-domain":      c.TLSDomain,
		"log-level":       strings.ToLower(c.LogLevel.String()),
		"backup-interval": c.BackupInterval.String(),
		"backup-keep":     strconv.Itoa(c.BackupKeep),
		"tls-http-addr":   c.TLSHTTPAddr,
		"secure-cookies":  strconv.FormatBool(c.SecureCookies),
	}

	described := make(map[string]bool, len(settingSpecs))
	for _, spec := range settingSpecs {
		described[spec.flag] = true
	}
	// A flag with no descriptor would be invisible on the admin page, which
	// is exactly the drift this list exists to prevent. Every test in this
	// package calls Parse, so this fires the moment it happens.
	fs.VisitAll(func(f *flag.Flag) {
		if !described[f.Name] {
			panic("config: flag -" + f.Name + " has no entry in settingSpecs")
		}
	})

	out := make([]Setting, 0, len(settingSpecs))
	for _, spec := range settingSpecs {
		f := fs.Lookup(spec.flag)
		if f == nil {
			panic("config: settingSpecs names -" + spec.flag + ", which is not a flag")
		}

		s := Setting{
			Flag:    spec.flag,
			Env:     spec.env,
			Value:   live[spec.flag],
			Default: spec.def,
			Doc:     f.Usage,
			Source:  SourceDefault,
		}
		switch {
		case explicit[spec.flag]:
			s.Source = SourceFlag
		case envOr(getenv, spec.env, "") != "":
			s.Source = SourceEnv
		}

		// TLS computes two values regardless of what was asked for. Calling
		// those "default" while showing a different default is the one
		// genuinely confusing thing this table could say.
		if c.TLSEnabled() {
			switch spec.flag {
			case "addr":
				if s.Source == SourceDefault {
					s.Source = SourceDerived
				}
			case "secure-cookies":
				s.Source = SourceDerived
			}
		}

		out = append(out, s)
	}
	return out
}
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/platform/config/... -race -count=1 -v`
Expected: PASS, including the pre-existing tests.

- [ ] **Step 7: Commit**

```bash
git add internal/platform/config
git commit -m "config: describe every setting and where its value came from"
```

---

### Task 4: One record of every route

**Files:**
- Create: `internal/platform/web/routes.go`
- Create: `internal/platform/web/routes_test.go`
- Modify: `internal/platform/web/login.go` (`Routes` signature)
- Modify: `internal/platform/app/router.go`
- Modify: `internal/platform/app/app.go` (`Registry.RecordRoutes`, pass the recorder to `newRouter`)
- Modify: `cmd/onsuite/stack.go`
- Modify: `internal/apps/paste/handlers_test.go`, `internal/platform/web/login_test.go` (call sites of `Routes`)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `web.Route{Pattern string; Public bool; Owner string}`, `web.PlatformOwner = "platform"`
  - `web.NewRecorder() *web.Recorder`, `(*Recorder).Add(Route)`, `(*Recorder).Handle(mux *http.ServeMux, pattern string, public bool, h http.Handler)`, `(*Recorder).Routes() []Route`
  - `app.Route` becomes `= web.Route`
  - `(*app.Registry).RecordRoutes(rec *web.Recorder)`
  - `(*web.Auth).Routes(mux *http.ServeMux, rec *Recorder)`

- [ ] **Step 1: Write the failing tests**

Create `internal/platform/web/routes_test.go`:

```go
package web_test

import (
	"net/http"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

func TestRecorderHandleRegistersAndRecordsInOneStep(t *testing.T) {
	rec := web.NewRecorder()
	mux := http.NewServeMux()
	called := false
	rec.Handle(mux, "GET /healthz", true, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	got := rec.Routes()
	if len(got) != 1 {
		t.Fatalf("Routes() has %d entries, want 1", len(got))
	}
	if got[0].Pattern != "GET /healthz" || !got[0].Public || got[0].Owner != web.PlatformOwner {
		t.Errorf("Routes()[0] = %+v", got[0])
	}

	// The handler must actually be mounted, not merely described.
	req, _ := http.NewRequest("GET", "/healthz", nil)
	h, _ := mux.Handler(req)
	h.ServeHTTP(nil, req)
	if !called {
		t.Error("the recorded handler was not registered on the mux")
	}
}

func TestRoutesAreSortedByOwnerThenPattern(t *testing.T) {
	rec := web.NewRecorder()
	rec.Add(web.Route{Pattern: "GET /paste/new", Owner: "paste"})
	rec.Add(web.Route{Pattern: "GET /login", Owner: web.PlatformOwner, Public: true})
	rec.Add(web.Route{Pattern: "GET /paste/{$}", Owner: "paste"})

	var order []string
	for _, r := range rec.Routes() {
		order = append(order, r.Pattern)
	}
	want := []string{"GET /login", "GET /paste/new", "GET /paste/{$}"}
	if len(order) != 3 || order[0] != want[0] {
		t.Fatalf("Routes() order = %v, want platform first then apps: %v", order, want)
	}
}

func TestANilRecorderIsANoOp(t *testing.T) {
	var rec *web.Recorder
	rec.Add(web.Route{Pattern: "GET /x"}) // must not panic
	if got := rec.Routes(); got != nil {
		t.Errorf("nil recorder Routes() = %v, want nil", got)
	}
}
```

The sort places `platform` before app ids. Implement that with an explicit rank, not alphabetically — `"paste"` sorts before `"platform"` and the platform's own routes belong at the top of the map.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/platform/web/... -run TestRecorder -count=1`
Expected: FAIL — `undefined: web.NewRecorder`.

- [ ] **Step 3: Write the recorder**

Create `internal/platform/web/routes.go`:

```go
package web

import (
	"net/http"
	"sort"
	"sync"
)

// PlatformOwner labels routes the platform registers for itself, as opposed
// to routes belonging to an app.
const PlatformOwner = "platform"

// Route is one registered HTTP route.
type Route struct {
	// Pattern is the full ServeMux pattern, e.g. "GET /paste/{id}".
	Pattern string
	// Public is true only when the route was registered without the
	// authentication guard.
	Public bool
	// Owner is an app id, or PlatformOwner.
	Owner string
}

// Recorder collects every route registered anywhere in the process.
//
// Routing in this suite is default-deny, and the admin page's route map is
// where that claim becomes checkable at runtime. A map that silently omitted
// the platform's own routes — /login, /static/, /healthz — would be worse
// than no map, because those are the first routes an auditor looks for. So
// both the app router and the platform's own registrations record here.
//
// A nil *Recorder is a usable no-op, so tests that do not care can pass one.
type Recorder struct {
	mu     sync.Mutex
	routes []Route
}

func NewRecorder() *Recorder { return &Recorder{} }

// Add records a route.
func (rec *Recorder) Add(rt Route) {
	if rec == nil {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.routes = append(rec.routes, rt)
}

// Handle registers a platform route on mux and records it in one step, so a
// route cannot be served without appearing on the map.
func (rec *Recorder) Handle(mux *http.ServeMux, pattern string, public bool, h http.Handler) {
	mux.Handle(pattern, h)
	rec.Add(Route{Pattern: pattern, Public: public, Owner: PlatformOwner})
}

// Routes returns everything recorded, platform routes first and each group
// sorted by pattern.
func (rec *Recorder) Routes() []Route {
	if rec == nil {
		return nil
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()

	out := make([]Route, len(rec.routes))
	copy(out, rec.routes)
	sort.SliceStable(out, func(i, j int) bool {
		li, lj := ownerRank(out[i].Owner), ownerRank(out[j].Owner)
		if li != lj {
			return li < lj
		}
		if out[i].Owner != out[j].Owner {
			return out[i].Owner < out[j].Owner
		}
		return out[i].Pattern < out[j].Pattern
	})
	return out
}

// ownerRank puts the platform above the apps. Sorting owners alphabetically
// would bury it in the middle of the app list.
func ownerRank(owner string) int {
	if owner == PlatformOwner {
		return 0
	}
	return 1
}
```

- [ ] **Step 4: Run the recorder tests**

Run: `go test ./internal/platform/web/... -run TestRecorder -count=1 -v`
Expected: PASS. (`TestRoutesAreSortedByOwnerThenPattern` and `TestANilRecorderIsANoOp` too.)

- [ ] **Step 5: Forward app routes into the recorder**

In `internal/platform/app/router.go`, replace the `Route` struct with an alias and thread the recorder through:

```go
// Route records one registration. It is an alias of web.Route so that app
// routes and the platform's own routes land in the same recorder and can be
// shown as one map.
type Route = web.Route
```

Add a `rec *web.Recorder` field to `Router`, take it in `newRouter`:

```go
func newRouter(mux *http.ServeMux, appID string, guard web.Middleware, rec *web.Recorder) *Router {
	return &Router{
		mux:    mux,
		appID:  appID,
		prefix: "/" + appID,
		guard:  guard,
		rec:    rec,
	}
}
```

and record in `register`, replacing the final two lines of that function:

```go
	r.mux.Handle(full, handler)
	rt := Route{Pattern: full, Public: public, Owner: r.appID}
	r.routes = append(r.routes, rt)
	r.rec.Add(rt)
```

In `internal/platform/app/app.go`, add a `rec *web.Recorder` field to `Registry`, the setter, and pass it to `newRouter`:

```go
// RecordRoutes tells the registry where to record each app's routes, so the
// admin page can show one complete map. It is optional: without a recorder,
// routes are still available from Router.Routes().
func (reg *Registry) RecordRoutes(rec *web.Recorder) { reg.rec = rec }
```

In `Registry.Mount`, change `r := newRouter(mux, m.ID, guard)` to `r := newRouter(mux, m.ID, guard, reg.rec)`.

- [ ] **Step 6: Give Auth.Routes a recorder**

In `internal/platform/web/login.go`:

```go
// Routes registers the endpoints that must exist outside any app. rec may be
// nil; when it is not, these routes appear on the admin page's route map
// alongside every other route in the process.
func (a *Auth) Routes(mux *http.ServeMux, rec *Recorder) {
	rec.Handle(mux, "GET /login", true, http.HandlerFunc(a.loginForm))
	rec.Handle(mux, "POST /login", true, http.HandlerFunc(a.loginSubmit))
	rec.Handle(mux, "POST /logout", true, http.HandlerFunc(a.logout))
}
```

- [ ] **Step 7: Update every call site**

`cmd/onsuite/stack.go` — replace the mux block in `buildStack` with:

```go
	routes := web.NewRecorder()
	deps.Registry.RecordRoutes(routes)

	mux := http.NewServeMux()
	routes.Handle(mux, "GET /healthz", true, healthzHandler(deps.Version, deps.DB))
	routes.Handle(mux, "GET /static/", true, http.StripPrefix("/static", assets.Handler()))
	authn.Routes(mux, routes)
```

and further down:

```go
	routes.Handle(mux, "GET /{$}", false, authn.RequireUser(homeHandler(deps, rend, errs)))
	routes.Handle(mux, "/", true, http.HandlerFunc(errs.NotFound))
```

Then fix the two test call sites by passing `nil`: `authn.Routes(mux, nil)` in `internal/apps/paste/handlers_test.go` and in `internal/platform/web/login_test.go` (grep for `.Routes(mux)` to catch them all).

- [ ] **Step 8: Run the full test suite**

Run: `go test ./... -race -count=1`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/platform/web internal/platform/app cmd/onsuite internal/apps/paste/handlers_test.go
git commit -m "web: record every registered route in one place"
```

---

### Task 5: RequireAdmin

**Files:**
- Modify: `internal/platform/web/login.go`
- Test: `internal/platform/web/login_test.go`

**Interfaces:**
- Consumes: `Auth.RequireUser`, `Errors.NotFound`, `UserFrom`.
- Produces: `(*web.Auth).RequireAdmin(next http.Handler) http.Handler`.

- [ ] **Step 1: Write the failing test**

`internal/platform/web/login_test.go` already has `newAuthFixture`, which builds the real stack over a real database. Extend it with a non-admin account and an admin-guarded route, then add the three cases.

In `newAuthFixture`, add a second account after the existing `user`:

```go
	plain, err := users.CreateUser(context.Background(), "plain", hash, false)
	if err != nil {
		t.Fatal(err)
	}
```

mount a guarded route next to `GET /private`:

```go
	mux.Handle("GET /adminonly", a.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("admin only"))
	})))
```

add `plain auth.User` to the `authFixture` struct and return it: `return &authFixture{handler: handler, auth: a, users: users, user: user, plain: plain}`.

Then append the test:

```go
func TestRequireAdminRedirectsAnonymousToLogin(t *testing.T) {
	f := newAuthFixture(t)

	rec := f.do(t, httptest.NewRequest("GET", "/adminonly", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); !strings.HasPrefix(got, "/login") {
		t.Errorf("Location = %q, want the login page", got)
	}
}

// A non-admin must not be able to tell the page apart from one that is not
// there, the same way a failed login cannot be told apart from an unknown
// account.
func TestRequireAdminGives404ToANonAdmin(t *testing.T) {
	f := newAuthFixture(t)
	cookies := f.logIn(t, "plain", testPassword)

	rec := f.do(t, httptest.NewRequest("GET", "/adminonly", nil), cookies...)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "admin only") {
		t.Error("the guarded handler ran for a non-admin")
	}
}

func TestRequireAdminAdmitsAnAdmin(t *testing.T) {
	f := newAuthFixture(t)
	cookies := f.logIn(t, f.user.Username, testPassword)

	rec := f.do(t, httptest.NewRequest("GET", "/adminonly", nil), cookies...)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "admin only" {
		t.Errorf("body = %q, want the guarded handler's output", rec.Body.String())
	}
}
```

`f.user` is the fixture's existing account, created with `isAdmin` true.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/platform/web/... -run TestRequireAdmin -count=1`
Expected: FAIL — `a.RequireAdmin undefined`.

- [ ] **Step 3: Implement**

In `internal/platform/web/login.go`, directly after `RequireUser`:

```go
// RequireAdmin blocks everyone except administrators.
//
// It composes RequireUser, so an anonymous request is still redirected to the
// login page. A signed-in non-admin gets the same 404 as any address that
// does not exist: the alternative, 403, confirms that the page is there. That
// matches how login already behaves, where a wrong password and an unknown
// username produce identical responses so that failures cannot be used to
// enumerate accounts.
func (a *Auth) RequireAdmin(next http.Handler) http.Handler {
	return a.RequireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := UserFrom(r.Context())
		if !ok || !u.IsAdmin {
			a.errs.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	}))
}
```

- [ ] **Step 4: Run the test**

Run: `go test ./internal/platform/web/... -race -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/web
git commit -m "web: add RequireAdmin, the first guard that reads the is_admin flag"
```

---

### Task 6: The Stater capability

**Files:**
- Modify: `internal/platform/app/app.go`
- Test: `internal/platform/app/app_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `app.Stat{Label, Value, Hint string}`
  - `app.Stater interface { Stats(ctx context.Context, handle *sql.DB) ([]Stat, error) }`
  - `app.AppStats{ID, Name string; Stats []Stat; Err string}`
  - `(*app.Registry).Stats(ctx context.Context, handle *sql.DB) []AppStats`

- [ ] **Step 1: Write the failing test**

Append to `internal/platform/app/app_test.go`:

```go
type statingApp struct {
	id    string
	stats []app.Stat
	err   error
}

func (s statingApp) Meta() app.Meta {
	return app.Meta{ID: s.id, Name: "ON " + strings.ToUpper(s.id[:1]) + s.id[1:], Summary: "x", Order: 0}
}
func (s statingApp) Migrations() fs.FS { return fstest.MapFS{} }
func (s statingApp) Templates() fs.FS  { return fstest.MapFS{"x.html": &fstest.MapFile{}} }
func (s statingApp) Mount(r *app.Router, d app.Deps) {
	r.HandleFunc("GET /{$}", func(http.ResponseWriter, *http.Request) {})
}
func (s statingApp) Stats(context.Context, *sql.DB) ([]app.Stat, error) {
	return s.stats, s.err
}

func TestStatsCollectsFromAppsThatImplementStater(t *testing.T) {
	reg, err := app.NewRegistry(
		statingApp{id: "paste", stats: []app.Stat{{Label: "Snippets", Value: "3"}}},
		silentApp{id: "notes"}, // an app with no Stats method
	)
	if err != nil {
		t.Fatal(err)
	}

	got := reg.Stats(context.Background(), nil)
	if len(got) != 1 {
		t.Fatalf("Stats() returned %d entries, want 1: an app without the method is skipped", len(got))
	}
	if got[0].ID != "paste" || len(got[0].Stats) != 1 || got[0].Stats[0].Value != "3" {
		t.Errorf("Stats()[0] = %+v", got[0])
	}
	if got[0].Err != "" {
		t.Errorf("Err = %q on a successful app", got[0].Err)
	}
}

func TestOneAppsStatsFailureDoesNotHideTheOthers(t *testing.T) {
	reg, err := app.NewRegistry(
		statingApp{id: "paste", err: errors.New("no such table")},
		statingApp{id: "notes", stats: []app.Stat{{Label: "Notes", Value: "7"}}},
	)
	if err != nil {
		t.Fatal(err)
	}

	got := reg.Stats(context.Background(), nil)
	if len(got) != 2 {
		t.Fatalf("Stats() returned %d entries, want 2", len(got))
	}
	var failed, ok app.AppStats
	for _, s := range got {
		if s.ID == "paste" {
			failed = s
		} else {
			ok = s
		}
	}
	if failed.Err != "no such table" {
		t.Errorf("the failing app's Err = %q", failed.Err)
	}
	if len(ok.Stats) != 1 {
		t.Errorf("the healthy app returned %d stats; one app's failure must not hide another's", len(ok.Stats))
	}
}
```

`silentApp` is whatever minimal `app.App` implementation the file already defines for other tests — reuse it, or copy `statingApp` without the `Stats` method and name it `silentApp`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/platform/app/... -run TestStats -count=1`
Expected: FAIL — `reg.Stats undefined`.

- [ ] **Step 3: Implement**

In `internal/platform/app/app.go`, after the `Exporter` block at the end of the file:

```go
// Stat is one number an app wants shown on the admin page.
//
// Value is a preformatted string because only the app knows whether 1234567
// should read as "1.2 MB" or "1,234,567". The platform renders label and
// value and formats nothing.
type Stat struct {
	Label string // "Snippets"
	Value string // "1204"
	Hint  string // optional one-liner; may be empty
}

// Stater is implemented by apps that describe themselves on the admin page.
// Like Exporter it is optional and discovered by type assertion, so an app
// with nothing to say does not have to stub out a method.
//
// It takes the database rather than Mount's Deps for the same reason Export
// does: it depends on data, not on an HTTP stack having been built.
type Stater interface {
	Stats(ctx context.Context, handle *sql.DB) ([]Stat, error)
}

// AppStats is one app's contribution to the admin page.
type AppStats struct {
	ID    string
	Name  string
	Stats []Stat
	// Err is this app's collector failing, recorded rather than returned.
	Err string
}

// Stats collects every registered app's numbers. Apps that do not implement
// Stater are skipped silently, exactly as they are for Export.
//
// Unlike Export, one app's failure does not fail the call: the error is
// recorded on that app's entry and the rest are still returned. An export
// missing an app is corrupt; a dashboard missing one card is a dashboard
// missing one card.
func (reg *Registry) Stats(ctx context.Context, handle *sql.DB) []AppStats {
	var out []AppStats
	for _, a := range reg.apps {
		s, ok := a.(Stater)
		if !ok {
			continue
		}
		m := a.Meta()
		entry := AppStats{ID: m.ID, Name: m.Name}
		stats, err := s.Stats(ctx, handle)
		if err != nil {
			entry.Err = err.Error()
		} else {
			entry.Stats = stats
		}
		out = append(out, entry)
	}
	return out
}
```

- [ ] **Step 4: Run the test**

Run: `go test ./internal/platform/app/... -race -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/app
git commit -m "app: add the optional Stater capability, alongside Exporter"
```

---

### Task 7: Account and session queries

**Files:**
- Modify: `internal/platform/auth/store.go`
- Test: `internal/platform/auth/store_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `auth.Account{ID int64; Username string; IsAdmin bool; CreatedAt time.Time; Sessions int}`
  - `(*auth.Store).ListAccounts(ctx context.Context) ([]Account, error)`
  - `(*auth.Store).SessionCounts(ctx context.Context) (live, expired int, err error)`

- [ ] **Step 1: Write the failing test**

`internal/platform/auth/store_test.go` is a white-box test file (`package auth`) whose `newStore(t) (*Store, *sql.DB)` helper opens a migrated database in a temp dir. Append, using unqualified names:

```go
func TestListAccountsReturnsEveryUserOldestFirstWithSessionCounts(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()

	hash, err := HashPassword("a-sufficiently-long-password")
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.CreateUser(ctx, "root", hash, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUser(ctx, "ilia", hash, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSession(ctx, root.ID); err != nil {
		t.Fatal(err)
	}

	got, err := store.ListAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ListAccounts() returned %d accounts, want 2", len(got))
	}
	if got[0].Username != "root" || !got[0].IsAdmin {
		t.Errorf("ListAccounts()[0] = %+v, want root as an admin first", got[0])
	}
	if got[0].Sessions != 1 {
		t.Errorf("root has %d live sessions, want 1", got[0].Sessions)
	}
	if got[1].Username != "ilia" || got[1].IsAdmin {
		t.Errorf("ListAccounts()[1] = %+v", got[1])
	}
	if got[1].Sessions != 0 {
		t.Errorf("ilia has %d live sessions, want 0", got[1].Sessions)
	}
	if got[0].CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestSessionCountsSeparatesLiveFromExpired(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()

	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	store.SetClock(func() time.Time { return now })

	hash, err := HashPassword("a-sufficiently-long-password")
	if err != nil {
		t.Fatal(err)
	}
	u, err := store.CreateUser(ctx, "ilia", hash, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSession(ctx, u.ID); err != nil {
		t.Fatal(err)
	}

	// Move well past the 30-day session lifetime without sweeping.
	store.SetClock(func() time.Time { return now.AddDate(0, 0, 60) })

	live, expired, err := store.SessionCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if live != 0 || expired != 1 {
		t.Errorf("SessionCounts() = live %d, expired %d; want 0 and 1", live, expired)
	}
}

func TestSessionCountsOnAnEmptyTableIsZeroNotAnError(t *testing.T) {
	store, _ := newStore(t)
	live, expired, err := store.SessionCounts(context.Background())
	if err != nil {
		t.Fatalf("SessionCounts() on an empty table: %v", err)
	}
	if live != 0 || expired != 0 {
		t.Errorf("SessionCounts() = %d, %d, want 0, 0", live, expired)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/platform/auth/... -run 'TestListAccounts|TestSessionCounts' -count=1`
Expected: FAIL — `store.ListAccounts undefined`.

- [ ] **Step 3: Implement**

In `internal/platform/auth/store.go`, after `CountUsers`:

```go
// Account is one row of the admin page's user table.
//
// It deliberately omits PasswordHash. User carries the hash because the login
// path must verify it; nothing that renders a list of accounts has any reason
// to hold one.
type Account struct {
	ID        int64
	Username  string
	IsAdmin   bool
	CreatedAt time.Time
	// Sessions is how many unexpired sessions this account has right now.
	Sessions int
}

// ListAccounts returns every account, oldest first, each with a count of its
// live sessions. It is read-only and exists for the admin page.
func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	now := formatTime(s.now())
	rows, err := s.db.QueryContext(ctx,
		`SELECT u.id, u.username, u.is_admin, u.created_at,
		        (SELECT count(*) FROM sessions se
		          WHERE se.user_id = u.id AND se.expires_at > ?)
		   FROM users u
		  ORDER BY u.id`, now)
	if err != nil {
		return nil, fmt.Errorf("auth: list accounts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Account
	for rows.Next() {
		var (
			a         Account
			isAdmin   int
			createdAt string
		)
		if err := rows.Scan(&a.ID, &a.Username, &isAdmin, &createdAt, &a.Sessions); err != nil {
			return nil, fmt.Errorf("auth: scan account: %w", err)
		}
		a.IsAdmin = isAdmin == 1
		if a.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: list accounts: %w", err)
	}
	return out, nil
}

// SessionCounts reports how many sessions are live and how many are past
// their expiry but not yet swept. A growing expired count means the sweep job
// is not running, which is exactly what the admin page is for.
func (s *Store) SessionCounts(ctx context.Context) (live, expired int, err error) {
	var total int
	// coalesce because sum() over an empty table is NULL, not 0.
	err = s.db.QueryRowContext(ctx,
		`SELECT count(*),
		        coalesce(sum(CASE WHEN expires_at > ? THEN 1 ELSE 0 END), 0)
		   FROM sessions`, formatTime(s.now())).Scan(&total, &live)
	if err != nil {
		return 0, 0, fmt.Errorf("auth: count sessions: %w", err)
	}
	return live, total - live, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/platform/auth/... -race -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/auth
git commit -m "auth: add read-only account and session-count queries"
```

---

### Task 8: ON Paste implements Stater

**Files:**
- Modify: `internal/apps/paste/store.go`
- Modify: `internal/apps/paste/paste.go`
- Test: `internal/apps/paste/store_test.go`

**Interfaces:**
- Consumes: `app.Stat`, `app.Stater` from Task 6.
- Produces: `(*paste.Store).Stats(ctx context.Context) ([]app.Stat, error)` and `(*paste.App).Stats(ctx context.Context, handle *sql.DB) ([]app.Stat, error)`.

**Note:** this adds a small `humanBytes` helper to `paste`, and Task 9 adds a similar one to `admin`. That duplication is deliberate: the spec puts formatting on the app side precisely so the platform never guesses, and sharing six lines across the platform/app boundary would need a utility package this project does not have.

- [ ] **Step 1: Write the failing test**

`internal/apps/paste/store_test.go` has `newFixture(t) *fixture`, which gives a migrated database with two accounts (`f.alice`, `f.bob`) and `f.store`. Stats are suite-wide, not per user, so the second test deliberately puts snippets under both accounts. Append:

```go
func TestStatsOnAnEmptyDatabase(t *testing.T) {
	f := newFixture(t)

	got, err := f.store.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("Stats() returned %d stats, want 5", len(got))
	}
	if got[0].Label != "Snippets" || got[0].Value != "0" {
		t.Errorf("Stats()[0] = %+v, want Snippets 0", got[0])
	}
	if got[4].Label != "Newest" || got[4].Value != "never" {
		t.Errorf("Stats()[4] = %+v; an empty table must not render a zero timestamp", got[4])
	}
}

func TestStatsCountsSnippetsAndShares(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first, err := f.store.Create(ctx, f.alice.ID, "one", "go", "package main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Create(ctx, f.bob.ID, "two", "go", "package main // longer"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Share(ctx, f.alice.ID, first.ID); err != nil {
		t.Fatal(err)
	}

	got, err := f.store.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]string{}
	for _, s := range got {
		by[s.Label] = s.Value
	}
	if by["Snippets"] != "2" {
		t.Errorf("Snippets = %q, want 2", by["Snippets"])
	}
	if by["Shared"] != "1" {
		t.Errorf("Shared = %q, want 1", by["Shared"])
	}
	if by["Newest"] == "never" {
		t.Error("Newest is 'never' with two snippets stored")
	}
	if by["Total size"] == "" || by["Largest"] == "" {
		t.Errorf("size stats are empty: %v", by)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/apps/paste/... -run TestStats -count=1`
Expected: FAIL — `store.Stats undefined`.

- [ ] **Step 3: Implement the store query**

At the end of `internal/apps/paste/store.go`:

```go
// Stats is the whole app in five numbers, for the admin page. It implements
// the data half of app.Stater; App.Stats is the thin wrapper the platform
// type-asserts for.
//
// Every value is formatted here rather than by the platform: only this
// package knows that a body length is bytes and should read as "1.2 MB".
func (st *Store) Stats(ctx context.Context) ([]app.Stat, error) {
	var (
		count, shared, totalBytes, largest int64
		newest                             sql.NullString
	)
	err := st.db.QueryRowContext(ctx,
		`SELECT count(*),
		        coalesce(sum(CASE WHEN share_slug IS NOT NULL THEN 1 ELSE 0 END), 0),
		        coalesce(sum(length(body)), 0),
		        coalesce(max(length(body)), 0),
		        max(created_at)
		   FROM paste_snippets`).
		Scan(&count, &shared, &totalBytes, &largest, &newest)
	if err != nil {
		return nil, fmt.Errorf("paste: stats: %w", err)
	}

	newestLabel := "never"
	if newest.Valid {
		t, err := parseTime(newest.String)
		if err != nil {
			return nil, err
		}
		newestLabel = t.Format("2006-01-02 15:04 MST")
	}

	return []app.Stat{
		{Label: "Snippets", Value: strconv.FormatInt(count, 10)},
		{Label: "Shared", Value: strconv.FormatInt(shared, 10),
			Hint: "readable by anyone holding the link"},
		{Label: "Total size", Value: humanBytes(totalBytes), Hint: "snippet bodies only"},
		{Label: "Largest", Value: humanBytes(largest)},
		{Label: "Newest", Value: newestLabel},
	}, nil
}

// humanBytes renders a byte count the way a person reads one.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
```

Add `strconv` and `github.com/iliafrenkel/on-suite/internal/platform/app` to the file's imports (`database/sql` and `fmt` are already there).

- [ ] **Step 4: Implement the capability wrapper**

At the end of `internal/apps/paste/paste.go`:

```go
// Stats implements app.Stater, so ON Paste appears on the admin page. Like
// Export it takes the database rather than using a.store, so it works on a
// handle the platform already has without depending on Mount having run.
func (a *App) Stats(ctx context.Context, handle *sql.DB) ([]app.Stat, error) {
	return NewStore(handle).Stats(ctx)
}
```

Add `"context"` and `"database/sql"` to the imports.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/apps/paste/... -race -count=1`
Expected: PASS.

- [ ] **Step 6: Verify the interface is actually satisfied**

Run:
```bash
cat > /tmp/stater_check.go <<'EOF'
package paste
import "github.com/iliafrenkel/on-suite/internal/platform/app"
var _ app.Stater = (*App)(nil)
EOF
cp /tmp/stater_check.go internal/apps/paste/stater_check_test.go && go build ./... && go vet ./internal/apps/paste/
```
Expected: no output. Then make the assertion permanent instead of temporary — delete the scratch file and put the line at the top of `internal/apps/paste/paste.go`, under the imports:

```go
// ON Paste implements both optional capabilities. A compile-time assertion,
// because the platform discovers them by type assertion and a typo'd method
// name would otherwise fail silently as "this app has nothing to report".
var (
	_ app.Exporter = (*App)(nil)
	_ app.Stater   = (*App)(nil)
)
```

Run: `rm -f internal/apps/paste/stater_check_test.go && go build ./... && go test ./internal/apps/paste/... -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/paste
git commit -m "paste: report snippet counts and sizes on the admin page"
```

---

### Task 9: The admin page — shell, build & runtime, database

The first working version of the page: two sections, mounted, guarded, linked from the sidebar. Later tasks add sections into the same structure.

**Files:**
- Create: `internal/platform/admin/admin.go`
- Create: `internal/platform/admin/collect.go`
- Create: `internal/platform/admin/format.go`
- Create: `internal/platform/admin/admin_test.go`
- Create: `internal/ui/templates/admin.html`
- Modify: `internal/ui/static/app.css`
- Modify: `internal/ui/templates/base.html`
- Modify: `internal/ui/icons.go`
- Modify: `cmd/onsuite/stack.go`, `cmd/onsuite/serve.go`
- Modify: `internal/arch/arch_test.go`

**Interfaces:**
- Consumes: `web.Recorder` and `Auth.RequireAdmin` (Tasks 4, 5); `jobs.Registry` (Task 1); `config.Config` (Task 3); `app.Registry`, `app.NewPage`; `auth.Store`.
- Produces:
  - `admin.Deps{Config config.Config; DB *sql.DB; Users *auth.Store; Apps *app.Registry; Jobs *jobs.Registry; Routes *web.Recorder; Render *render.Renderer; Errors *web.Errors; Nav []render.NavItem; Version string; Started time.Time}`
  - `admin.Handler(d Deps) http.Handler`
  - `admin.Report` — the template's view model; later tasks add fields to it.

- [ ] **Step 1: Write the failing test**

Create `internal/platform/admin/admin_test.go`. It builds the whole stack the way `internal/apps/paste/handlers_test.go` does — a real database in a temp dir, real sessions, the real middleware chain — because the point of most of these assertions is the guard, and a guard tested without its middleware is not tested:

```go
package admin_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/admin"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

const testPassword = "a-sufficiently-long-password"

type server struct {
	handler http.Handler
	admin   []*http.Cookie
	plain   []*http.Cookie
}

func newServer(t *testing.T) *server {
	t.Helper()
	ctx := context.Background()

	dir := t.TempDir()
	// Parse a real Config so the settings section has real provenance, and
	// open the database where that Config says it lives, so the database
	// section reports real file sizes.
	cfg, err := config.Parse([]string{"-data-dir", dir}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := db.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	registry, err := app.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, migrations); err != nil {
		t.Fatal(err)
	}

	users := auth.NewStore(handle)
	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatal(err)
	}
	rend, err := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL})
	if err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.DiscardHandler)
	errs := web.NewErrors(rend, log)
	csrf := web.NewCSRF(false, errs)
	authn := web.NewAuth(web.AuthOptions{
		Users: users, Render: rend, Errors: errs, CSRF: csrf, Log: log, Secure: false,
	})

	routes := web.NewRecorder()
	registry.RecordRoutes(routes)

	mux := http.NewServeMux()
	authn.Routes(mux, routes)
	routes.Handle(mux, "GET /admin/", false, authn.RequireAdmin(admin.Handler(admin.Deps{
		Config: cfg, DB: handle, Users: users, Apps: registry,
		Jobs: jobs.NewRegistry(), Routes: routes,
		Render: rend, Errors: errs, Nav: registry.NavItems(),
		Version: "v9.9.9", Started: time.Now().Add(-90 * time.Second),
	})))
	routes.Handle(mux, "/", true, http.HandlerFunc(errs.NotFound))

	s := &server{handler: web.Stack(mux, log, errs, csrf, authn)}

	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	root, err := users.CreateUser(ctx, "root", hash, true)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := users.CreateUser(ctx, "ilia", hash, false)
	if err != nil {
		t.Fatal(err)
	}
	s.admin = s.logIn(t, root)
	s.plain = s.logIn(t, plain)
	return s
}

// logIn performs a real login, so the tests carry genuine session cookies.
func (s *server) logIn(t *testing.T, u auth.User) []*http.Cookie {
	t.Helper()

	// A GET first, to be issued a CSRF cookie and token.
	warm := httptest.NewRecorder()
	s.handler.ServeHTTP(warm, httptest.NewRequest("GET", "/login", nil))
	var token string
	for _, c := range warm.Result().Cookies() {
		if c.Name == web.CSRFCookieName {
			token = c.Value
		}
	}

	form := url.Values{"username": {u.Username}, "password": {testPassword}, web.CSRFFormField: {token}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range warm.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logging in %s: status = %d, body = %s", u.Username, rec.Code, rec.Body.String())
	}
	return append(warm.Result().Cookies(), rec.Result().Cookies()...)
}

func (s *server) get(t *testing.T, cookies []*http.Cookie, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

func TestAnonymousIsSentToLogin(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, nil, "/admin/")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); !strings.HasPrefix(got, "/login") {
		t.Errorf("Location = %q", got)
	}
}

// The page must not confirm its own existence to someone who may not see it.
func TestANonAdminGetsExactlyTheSameResponseAsAMissingPage(t *testing.T) {
	s := newServer(t)
	admin := s.get(t, s.plain, "/admin/")
	missing := s.get(t, s.plain, "/no-such-page")

	if admin.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", admin.Code)
	}
	if admin.Body.String() != missing.Body.String() {
		t.Error("/admin/ and a missing page render differently for a non-admin")
	}
}

func TestTheAdminPageShowsBuildAndDatabaseSections(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, s.admin, "/admin/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#runtime")
	doc.MustHave("#database")

	body := doc.Text()
	for _, want := range []string{"v9.9.9", "wal", "platform:0001"} {
		if !strings.Contains(strings.ToLower(body), strings.ToLower(want)) {
			t.Errorf("the page does not mention %q", want)
		}
	}
}

func TestTheSidebarLinksAdminsToTheAdminPageAndNobodyElse(t *testing.T) {
	s := newServer(t)

	adminPage := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String())
	adminPage.MustHave(`nav.shell-nav a[href="/admin/"]`)

	// A non-admin's own pages must not advertise it either.
	plainPage := htmlassert.Parse(t, s.get(t, s.plain, "/login").Body.String())
	plainPage.MustNotHave(`a[href="/admin/"]`)
}
```

Add `"io"` to the imports for `io.Discard`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/platform/admin/... -count=1`
Expected: FAIL — the package does not exist.

- [ ] **Step 3: Write the formatting helpers**

Create `internal/platform/admin/format.go`:

```go
package admin

import (
	"fmt"
	"strconv"
)

// humanBytes renders a byte count the way a person reads one. ON Paste has a
// near-identical helper: formatting is deliberately each package's own
// business (see app.Stat), and sharing six lines across the platform/app
// boundary would need a utility package this project does not have.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
```

- [ ] **Step 4: Write the collectors**

Create `internal/platform/admin/collect.go`:

```go
package admin

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"
)

// RuntimeInfo is what this process is and how long it has been up.
type RuntimeInfo struct {
	Version    string
	Go         string
	OS         string
	Arch       string
	CPUs       int
	Goroutines int
	HeapInUse  string
	StartedAt  time.Time
	Uptime     time.Duration
}

// runtimeInfo reads the process's own vital signs.
//
// runtime.ReadMemStats stops the world briefly. On a page one person loads
// occasionally that is free; it would not be on a hot path.
func (d Deps) runtimeInfo(now time.Time) RuntimeInfo {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	return RuntimeInfo{
		Version:    d.Version,
		Go:         runtime.Version(),
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		CPUs:       runtime.NumCPU(),
		Goroutines: runtime.NumGoroutine(),
		HeapInUse:  humanBytes(int64(ms.HeapInuse)),
		StartedAt:  d.Started,
		Uptime:     now.Sub(d.Started).Truncate(time.Second),
	}
}

// DatabaseInfo is the state of the one SQLite file everything lives in.
type DatabaseInfo struct {
	Path        string
	FileSize    string
	WALSize     string
	SHMSize     string
	PageSize    int64
	PageCount   int64
	JournalMode string
	Migrations  []MigrationInfo
}

// MigrationInfo is one applied migration.
type MigrationInfo struct {
	Key       string // "paste:0001"
	Name      string
	AppliedAt string
}

// databaseInfo reads SQLite's own view of itself plus the file sizes on disk.
//
// The WAL and shared-memory files matter as much as the database: a WAL that
// keeps growing is the visible symptom of checkpointing having stopped, and
// nothing else in the system would show it.
func (d Deps) databaseInfo(ctx context.Context) (DatabaseInfo, error) {
	path := d.Config.DBPath()
	info := DatabaseInfo{
		Path:     path,
		FileSize: fileSize(path),
		WALSize:  fileSize(path + "-wal"),
		SHMSize:  fileSize(path + "-shm"),
	}
	if d.DB == nil {
		return info, fmt.Errorf("no database handle")
	}

	if err := d.DB.QueryRowContext(ctx, "PRAGMA page_size").Scan(&info.PageSize); err != nil {
		return info, fmt.Errorf("page_size: %w", err)
	}
	if err := d.DB.QueryRowContext(ctx, "PRAGMA page_count").Scan(&info.PageCount); err != nil {
		return info, fmt.Errorf("page_count: %w", err)
	}
	if err := d.DB.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&info.JournalMode); err != nil {
		return info, fmt.Errorf("journal_mode: %w", err)
	}

	rows, err := d.DB.QueryContext(ctx,
		`SELECT key, name, applied_at FROM schema_migrations
		  ORDER BY applied_at, key`)
	if err != nil {
		return info, fmt.Errorf("schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var m MigrationInfo
		if err := rows.Scan(&m.Key, &m.Name, &m.AppliedAt); err != nil {
			return info, fmt.Errorf("scan migration: %w", err)
		}
		info.Migrations = append(info.Migrations, m)
	}
	if err := rows.Err(); err != nil {
		return info, fmt.Errorf("schema_migrations: %w", err)
	}
	return info, nil
}

// fileSize reports a file's size, or "—" if it is not there. A missing -wal
// file is normal, not an error: SQLite removes it on a clean shutdown.
func fileSize(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return "—"
	}
	return humanBytes(st.Size())
}
```

`d.DB` is used through its methods only, so this file needs no `database/sql`
import of its own.

- [ ] **Step 5: Write the package and handler**

Create `internal/platform/admin/admin.go`:

```go
// Package admin renders the administrator's view of the running system.
//
// It is a platform page rather than an app: it reports on the platform, and
// an app whose subject was the platform would invert the layering the
// architecture test protects. It sits at the top of the platform — it may
// import everything below it, and nothing imports it but cmd/onsuite.
//
// It is strictly read-only. There is no handler here that changes anything,
// and adding one is a spec change, not an implementation detail.
package admin

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// Deps is everything the page reads. It is assembled once by buildStack; the
// handler holds no globals and opens nothing at request time except the
// database file's stat.
type Deps struct {
	Config  config.Config
	DB      *sql.DB
	Users   *auth.Store
	Apps    *app.Registry
	Jobs    *jobs.Registry
	Routes  *web.Recorder
	Render  *render.Renderer
	Errors  *web.Errors
	Nav     []render.NavItem
	Version string
	// Started is when the process came up, for the uptime figure.
	Started time.Time
}

// Report is the page's view model. Each section carries its own error, so one
// failed collector renders as a note in its own card instead of replacing the
// whole page with a 500 — a broken PRAGMA must not hide the job status.
type Report struct {
	Runtime     RuntimeInfo
	Database    DatabaseInfo
	DatabaseErr string
}

// collect gathers every section. It never returns an error: an error is a
// value on the section it belongs to.
func (d Deps) collect(ctx context.Context, now time.Time) Report {
	rep := Report{Runtime: d.runtimeInfo(now)}

	database, err := d.databaseInfo(ctx)
	rep.Database = database
	if err != nil {
		rep.DatabaseErr = err.Error()
	}
	return rep
}

// Handler renders the page. Mount it behind Auth.RequireAdmin — this handler
// does no authorization of its own, exactly like every app handler.
func Handler(d Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := app.NewPage(r, "Admin", d.Nav)
		page.Shell.Version = d.Version
		page.Data = d.collect(r.Context(), time.Now().UTC())

		if err := d.Render.Page(w, http.StatusOK, "admin", page); err != nil {
			d.Errors.Internal(w, r, err)
		}
	})
}
```

- [ ] **Step 6: Write the template**

Create `internal/ui/templates/admin.html`:

```html
{{define "content"}}
<div class="stack">
	<h1>Admin</h1>
	<p class="faint">A read-only view of this server. Reload for current numbers.</p>

	<nav class="admin-nav" aria-label="Sections">
		<a href="#runtime">Build &amp; runtime</a>
		<a href="#database">Database</a>
	</nav>

	<section class="admin-section" id="runtime">
		<h2>Build &amp; runtime</h2>
		<dl class="admin-facts">
			<div class="admin-fact"><dt>Version</dt><dd class="small">{{.Data.Runtime.Version}}</dd></div>
			<div class="admin-fact"><dt>Go</dt><dd class="small">{{.Data.Runtime.Go}}</dd></div>
			<div class="admin-fact"><dt>Platform</dt><dd class="small">{{.Data.Runtime.OS}}/{{.Data.Runtime.Arch}}</dd></div>
			<div class="admin-fact"><dt>CPUs</dt><dd>{{.Data.Runtime.CPUs}}</dd></div>
			<div class="admin-fact"><dt>Uptime</dt><dd class="small">{{.Data.Runtime.Uptime}}</dd></div>
			<div class="admin-fact"><dt>Goroutines</dt><dd>{{.Data.Runtime.Goroutines}}</dd></div>
			<div class="admin-fact"><dt>Heap in use</dt><dd class="small">{{.Data.Runtime.HeapInUse}}</dd></div>
			<div class="admin-fact"><dt>Started</dt><dd class="small">{{.Data.Runtime.StartedAt.Format "2006-01-02 15:04 MST"}}</dd></div>
		</dl>
	</section>

	<section class="admin-section" id="database">
		<h2>Database</h2>
		{{with .Data.DatabaseErr}}<p class="notice notice-error">Could not read the database: {{.}}</p>{{end}}
		<dl class="admin-facts">
			<div class="admin-fact"><dt>File</dt><dd class="small">{{.Data.Database.FileSize}}</dd></div>
			<div class="admin-fact"><dt>WAL</dt><dd class="small">{{.Data.Database.WALSize}}</dd></div>
			<div class="admin-fact"><dt>Shared memory</dt><dd class="small">{{.Data.Database.SHMSize}}</dd></div>
			<div class="admin-fact"><dt>Journal mode</dt><dd class="small">{{.Data.Database.JournalMode}}</dd></div>
			<div class="admin-fact"><dt>Pages</dt><dd>{{.Data.Database.PageCount}}</dd></div>
			<div class="admin-fact"><dt>Page size</dt><dd class="small">{{.Data.Database.PageSize}} B</dd></div>
		</dl>
		<p class="faint">{{.Data.Database.Path}}</p>

		<div class="scroll-x">
			<table class="admin-table">
				<caption class="visually-hidden">Applied migrations</caption>
				<thead><tr><th>Migration</th><th>Name</th><th>Applied</th></tr></thead>
				<tbody>
				{{range .Data.Database.Migrations}}
					<tr><td><code>{{.Key}}</code></td><td>{{.Name}}</td><td>{{.AppliedAt}}</td></tr>
				{{else}}
					<tr><td colspan="3" class="dim">No migrations recorded.</td></tr>
				{{end}}
				</tbody>
			</table>
		</div>
	</section>
</div>
{{end}}
```

- [ ] **Step 7: Add the styles**

Append to `internal/ui/static/app.css`, before the `@media (max-width: 640px)` block:

```css
/* ---- Admin --------------------------------------------------------------- */

/* Sticky, because the page is long and the section list is how you move
 * around it. */
.admin-nav {
	position: sticky;
	top: 0;
	z-index: 1;
	display: flex;
	flex-wrap: wrap;
	gap: var(--s-2);
	padding: var(--s-2) 0;
	background: var(--c-bg);
}

.admin-nav a {
	padding: var(--s-1) var(--s-2);
	border: var(--border);
	border-radius: var(--radius);
	color: var(--c-text-dim);
	font-size: var(--fs-sm);
	text-decoration: none;
}

.admin-nav a:hover { color: var(--c-accent); border-color: var(--c-border-firm); }

.admin-section { margin-top: var(--s-6); }
.admin-section > p.faint { margin: 0 0 var(--s-3); }

.admin-facts {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr));
	gap: var(--s-3);
	margin: 0 0 var(--s-4);
}

.admin-fact {
	padding: var(--s-3);
	background: var(--c-bg-subtle);
	border-radius: var(--radius);
}

.admin-fact dt {
	color: var(--c-text-dim);
	font-size: var(--fs-xs);
	letter-spacing: 0.04em;
	text-transform: uppercase;
}

.admin-fact dd { margin: var(--s-1) 0 0; font-size: var(--fs-lg); font-weight: 600; }
.admin-fact dd.small { font-size: var(--fs-base); font-weight: 500; }

.admin-table { width: 100%; border-collapse: collapse; font-size: var(--fs-sm); }

.admin-table th,
.admin-table td {
	padding: var(--s-2) var(--s-3);
	border-bottom: var(--border);
	text-align: left;
	vertical-align: top;
}

.admin-table th { color: var(--c-text-dim); font-weight: 600; white-space: nowrap; }
.admin-table tr:last-child td { border-bottom: none; }

/* The admin entry is not an app, so it is separated from the app list rather
 * than sitting in it. */
.sidebar-admin {
	margin-top: var(--s-3);
	padding-top: var(--s-3);
	border-top: var(--border);
}
```

- [ ] **Step 8: Add the sidebar link and icon**

In `internal/ui/templates/base.html`, inside `<nav class="shell-nav" ...>`, after the `{{range .Shell.Apps}}...{{end}}` block:

```html
			{{if .Shell.IsAdmin}}
			<a class="sidebar-item sidebar-admin" href="/admin/"{{if eq "admin" $.Shell.ActiveApp}} aria-current="page"{{end}}>{{icon "admin"}}<span class="sidebar-label">Admin</span></a>
			{{end}}
```

In `internal/ui/icons.go`, add an entry to the `icons` map:

```go
	"admin": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="var(--c-accent-bg)"/>
		<path d="M12 6l5 2.2v3.4c0 3-2.1 5.2-5 6.4-2.9-1.2-5-3.4-5-6.4V8.2L12 6z" fill="none" stroke="var(--c-accent)" stroke-width="1.8" stroke-linejoin="round"/>
	</svg>`,
```

If `internal/ui/icons_test.go` asserts an exact set of icon ids, update it.

- [ ] **Step 9: Wire the page into the server**

In `cmd/onsuite/stack.go`, add three fields to `stackDeps`:

```go
type stackDeps struct {
	DB       *sql.DB
	Users    *auth.Store
	Registry *app.Registry
	Log      *slog.Logger
	Version  string
	Secure   bool
	// Config, Jobs and Started exist for the admin page. They are zero in
	// tests that only care about routing, which the page tolerates.
	Config  config.Config
	Jobs    *jobs.Registry
	Started time.Time
}
```

Add `time`, `config`, `jobs` and `admin` to that file's imports.

Then mount the route, immediately before the `GET /{$}` line:

```go
	routes.Handle(mux, "GET /admin/", false, authn.RequireAdmin(admin.Handler(admin.Deps{
		Config:  deps.Config,
		DB:      deps.DB,
		Users:   deps.Users,
		Apps:    deps.Registry,
		Jobs:    deps.Jobs,
		Routes:  routes,
		Render:  rend,
		Errors:  errs,
		Nav:     deps.Registry.NavItems(),
		Version: deps.Version,
		Started: deps.Started,
	})))
```

In `cmd/onsuite/serve.go`, capture the start time at the top of `serve` and pass the new fields:

```go
func serve(args []string, getenv func(string) string, errOut io.Writer) error {
	started := time.Now().UTC()
	cfg, err := config.Parse(args, getenv, errOut)
```

and in the `buildStack` call:

```go
	handler, err := buildStack(stackDeps{
		DB:       handle,
		Users:    users,
		Registry: registry,
		Log:      log,
		Version:  version,
		Secure:   cfg.SecureCookies,
		Config:   cfg,
		Jobs:     maintenance,
		Started:  started,
	})
```

This requires moving the `maintenance := jobs.NewRegistry()` / `registerMaintenance(...)` pair from Task 2 to *above* the `buildStack` call, leaving `go maintenance.Run(jobsCtx)` where it is. Registering jobs before the server starts is correct anyway: the page can then list them from the first request.

- [ ] **Step 10: Teach the architecture test about the admin package**

Add `"internal/platform/admin"` to the package list in `TestScanSeesTheRealTree` in `internal/arch/arch_test.go`. It needs no `TestLayering` entry: it sits at the top of the platform and may import everything below it. `TestPlatformDoesNotImportApps` already covers it.

- [ ] **Step 11: Run everything**

Run: `go test ./... -race -count=1`
Expected: PASS.

- [ ] **Step 12: Look at it**

```bash
go build ./cmd/onsuite && ./onsuite user add root --admin --data-dir /tmp/onsuite-admin && ./onsuite serve --data-dir /tmp/onsuite-admin
```
Open `http://localhost:8080/admin/`, sign in as `root`, and confirm both sections render in light and dark mode. Then log in as a non-admin and confirm `/admin/` is a plain 404 with no sidebar entry.

- [ ] **Step 13: Commit**

```bash
git add internal/platform/admin internal/ui cmd/onsuite internal/arch/arch_test.go
git commit -m "admin: add the admin page with build, runtime and database sections"
```

---

### Task 10: Settings and jobs sections

**Files:**
- Modify: `internal/platform/admin/admin.go` (add to `Report` and `collect`)
- Modify: `internal/ui/templates/admin.html`
- Modify: `internal/ui/static/app.css`
- Test: `internal/platform/admin/admin_test.go`

**Interfaces:**
- Consumes: `config.Setting`/`config.Source` (Task 3), `jobs.Status` (Task 1).
- Produces: `Report.Settings []config.Setting` and `Report.Jobs []jobs.Status`.

- [ ] **Step 1: Write the failing test**

Append to `internal/platform/admin/admin_test.go`:

```go
func TestTheSettingsSectionShowsValuesDefaultsAndSources(t *testing.T) {
	s := newServer(t)
	doc := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String())
	doc.MustHave("#settings")

	body := doc.Text()
	for _, want := range []string{"backup-interval", "ONSUITE_BACKUP_INTERVAL", "24h0m0s", "default"} {
		if !strings.Contains(body, want) {
			t.Errorf("the settings section does not mention %q", want)
		}
	}
	// The usage string is the "docs" half of the section.
	if !strings.Contains(body, "how often to snapshot the database") {
		t.Error("the settings section shows no documentation for a setting")
	}
}

func TestTheJobsSectionListsRegisteredJobsAndTheirState(t *testing.T) {
	s := newServerWithJobs(t) // see below
	doc := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String())
	doc.MustHave("#jobs")

	body := doc.Text()
	for _, want := range []string{"nightly thing", "disabled thing", "never"} {
		if !strings.Contains(body, want) {
			t.Errorf("the jobs section does not mention %q", want)
		}
	}
}
```

Extract the registry out of `newServer` so a test can supply one. Change `newServer(t)` to call a new `newServerWith(t, jobs.NewRegistry())`, and add:

```go
func newServerWithJobs(t *testing.T) *server {
	t.Helper()
	reg := jobs.NewRegistry()
	reg.Register("nightly thing", "does a nightly thing", time.Hour, func(context.Context) error { return nil })
	reg.Register("disabled thing", "would do a thing", 0, func(context.Context) error { return nil })
	return newServerWith(t, reg)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/platform/admin/... -run 'TestTheSettings|TestTheJobs' -count=1`
Expected: FAIL — `htmlassert: #settings not found`.

- [ ] **Step 3: Add the data**

In `internal/platform/admin/admin.go`, extend `Report`:

```go
type Report struct {
	Runtime     RuntimeInfo
	Settings    []config.Setting
	Jobs        []jobs.Status
	Database    DatabaseInfo
	DatabaseErr string
}
```

and `collect`, right after the `Runtime` line:

```go
	rep.Settings = d.Config.Settings()
	rep.Jobs = d.Jobs.Snapshot()
```

`d.Jobs` may be nil in a test that does not care; guard it:

```go
	if d.Jobs != nil {
		rep.Jobs = d.Jobs.Snapshot()
	}
```

- [ ] **Step 4: Add the markup**

Add two anchors to the `.admin-nav` list in `internal/ui/templates/admin.html`:

```html
		<a href="#settings">Settings</a>
		<a href="#jobs">Jobs</a>
```

and two sections after the runtime section:

```html
	<section class="admin-section" id="settings">
		<h2>Settings</h2>
		<p class="faint">Every value this server is running with, and where it came from.</p>
		<div class="scroll-x">
			<table class="admin-table">
				<thead><tr><th>Flag</th><th>Value</th><th>Source</th><th>Default</th><th>Environment</th><th>What it does</th></tr></thead>
				<tbody>
				{{range .Data.Settings}}
					<tr>
						<td><code>-{{.Flag}}</code></td>
						<td><code>{{if .Value}}{{.Value}}{{else}}(empty){{end}}</code></td>
						<td><span class="admin-tag admin-tag-{{.Source}}">{{.Source}}</span></td>
						<td class="dim"><code>{{if .Default}}{{.Default}}{{else}}(empty){{end}}</code></td>
						<td class="dim"><code>{{.Env}}</code></td>
						<td class="dim">{{.Doc}}</td>
					</tr>
				{{end}}
				</tbody>
			</table>
		</div>
	</section>

	<section class="admin-section" id="jobs">
		<h2>Jobs</h2>
		<p class="faint">Background work this process runs on a timer. Status is in memory and resets when the server restarts.</p>
		<div class="scroll-x">
			<table class="admin-table">
				<thead><tr><th>Job</th><th>Every</th><th>Last run</th><th>Took</th><th>Outcome</th><th>Next run</th><th>Runs</th></tr></thead>
				<tbody>
				{{range .Data.Jobs}}
					<tr>
						<td>{{.Name}}<br><span class="faint">{{.Description}}</span></td>
						<td>{{if .Enabled}}{{.Interval}}{{else}}<span class="admin-tag admin-tag-off">disabled</span>{{end}}</td>
						<td>{{if .LastRun.IsZero}}<span class="dim">never</span>{{else}}{{.LastRun.Format "2006-01-02 15:04:05 MST"}}{{end}}</td>
						<td class="dim">{{if .LastRun.IsZero}}—{{else}}{{.LastDuration}}{{end}}</td>
						<td>
							{{if .LastRun.IsZero}}<span class="dim">—</span>
							{{else if .LastErr}}<span class="admin-tag admin-tag-public">failed</span> <span class="faint">{{.LastErr}}</span>
							{{else}}<span class="admin-tag admin-tag-ok">ok</span>{{end}}
						</td>
						<td class="dim">{{if .NextRun.IsZero}}—{{else}}{{.NextRun.Format "2006-01-02 15:04:05 MST"}}{{end}}</td>
						<td>{{.Runs}}</td>
					</tr>
				{{else}}
					<tr><td colspan="7" class="dim">No jobs are registered.</td></tr>
				{{end}}
				</tbody>
			</table>
		</div>
	</section>
```

- [ ] **Step 5: Add the tag styles**

Append to the admin block in `internal/ui/static/app.css`:

```css
.admin-tag {
	display: inline-block;
	padding: 0 var(--s-2);
	border-radius: var(--radius);
	font-size: var(--fs-xs);
	font-weight: 600;
	white-space: nowrap;
}

/* Loud for anything an operator should look at twice — a failing job, and
 * later a route reachable without signing in. Quiet for the normal case. */
.admin-tag-public,
.admin-tag-failed { background: var(--c-danger-bg); color: var(--c-danger); }
.admin-tag-ok,
.admin-tag-flag { background: var(--c-accent-bg); color: var(--c-accent); }
.admin-tag-off,
.admin-tag-default,
.admin-tag-environment,
.admin-tag-derived { background: var(--c-bg-inset); color: var(--c-text-dim); }
```

The `admin-tag-{{.Source}}` class names come from `config.Source.String()`: `default`, `environment`, `flag`, `derived`.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/platform/admin/... -race -count=1 -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/platform/admin internal/ui
git commit -m "admin: show every setting with its source, and every job with its last outcome"
```

---

### Task 11: Apps, users and sessions sections

**Files:**
- Modify: `internal/platform/admin/admin.go`, `internal/platform/admin/collect.go`
- Modify: `internal/ui/templates/admin.html`
- Test: `internal/platform/admin/admin_test.go`

**Interfaces:**
- Consumes: `(*app.Registry).Stats` (Task 6), `(*auth.Store).ListAccounts` and `SessionCounts` (Task 7).
- Produces: `Report.Apps []app.AppStats`, `Report.Accounts []auth.Account`, `Report.AccountsErr string`, `Report.Sessions SessionInfo`, `Report.SessionsErr string`, `admin.SessionInfo{Live, Expired int}`.

- [ ] **Step 1: Write the failing test**

`newServer` currently registers no apps. Add a stub app that implements `Stater` so the section has something to render, then append the tests:

```go
type statApp struct{}

func (statApp) Meta() app.Meta {
	return app.Meta{ID: "demo", Name: "ON Demo", Summary: "a stub", Order: 0}
}
func (statApp) Migrations() fs.FS { return fstest.MapFS{} }
func (statApp) Templates() fs.FS {
	return fstest.MapFS{"demo.html": &fstest.MapFile{Data: []byte(`{{define "content"}}x{{end}}`)}}
}
func (statApp) Mount(r *app.Router, d app.Deps) {
	r.HandleFunc("GET /{$}", func(http.ResponseWriter, *http.Request) {})
	r.PublicFunc("GET /s/{slug}", func(http.ResponseWriter, *http.Request) {})
}
func (statApp) Stats(context.Context, *sql.DB) ([]app.Stat, error) {
	return []app.Stat{{Label: "Widgets", Value: "42", Hint: "for testing"}}, nil
}

func TestTheAppsSectionShowsEachAppsNumbers(t *testing.T) {
	s := newServer(t)
	doc := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String())
	doc.MustHave("#apps")

	body := doc.Text()
	for _, want := range []string{"ON Demo", "Widgets", "42"} {
		if !strings.Contains(body, want) {
			t.Errorf("the apps section does not mention %q", want)
		}
	}
}

func TestTheUsersSectionListsAccountsAndSessionCounts(t *testing.T) {
	s := newServer(t)
	doc := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String())
	doc.MustHave("#users")

	body := doc.Text()
	for _, want := range []string{"root", "ilia", "Administrator"} {
		if !strings.Contains(body, want) {
			t.Errorf("the users section does not mention %q", want)
		}
	}
}
```

Register the app in the harness: `app.NewRegistry(statApp{})` instead of `app.NewRegistry()`, and mount it — `registry.Mount(mux, app.Deps{DB: handle, Render: rend, Users: users, Errors: errs, Log: log}, authn.RequireUser)` — so its routes reach the recorder for Task 12. Add imports `io/fs`, `testing/fstest`, `database/sql`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/platform/admin/... -run 'TestTheApps|TestTheUsers' -count=1`
Expected: FAIL — `#apps not found`.

- [ ] **Step 3: Add the collectors**

Append to `internal/platform/admin/collect.go`:

```go
// SessionInfo is the state of the sessions table. A growing Expired count
// means the sweep job is not running.
type SessionInfo struct {
	Live    int
	Expired int
}

// sessionInfo counts live and expired sessions.
func (d Deps) sessionInfo(ctx context.Context) (SessionInfo, error) {
	if d.Users == nil {
		return SessionInfo{}, fmt.Errorf("no user store")
	}
	live, expired, err := d.Users.SessionCounts(ctx)
	if err != nil {
		return SessionInfo{}, err
	}
	return SessionInfo{Live: live, Expired: expired}, nil
}
```

- [ ] **Step 4: Add the data to the report**

In `internal/platform/admin/admin.go`, extend `Report`:

```go
type Report struct {
	Runtime     RuntimeInfo
	Settings    []config.Setting
	Jobs        []jobs.Status
	Apps        []app.AppStats
	Accounts    []auth.Account
	AccountsErr string
	Sessions    SessionInfo
	SessionsErr string
	Database    DatabaseInfo
	DatabaseErr string
}
```

and `collect`, before the database block:

```go
	if d.Apps != nil {
		rep.Apps = d.Apps.Stats(ctx, d.DB)
	}
	if d.Users != nil {
		accounts, err := d.Users.ListAccounts(ctx)
		if err != nil {
			rep.AccountsErr = err.Error()
		} else {
			rep.Accounts = accounts
		}
	}
	sessions, err := d.sessionInfo(ctx)
	rep.Sessions = sessions
	if err != nil {
		rep.SessionsErr = err.Error()
	}
```

- [ ] **Step 5: Add the markup**

Two more anchors in `.admin-nav`:

```html
		<a href="#apps">Apps</a>
		<a href="#users">Users &amp; sessions</a>
```

and two sections, after the jobs section:

```html
	<section class="admin-section" id="apps">
		<h2>Apps</h2>
		<p class="faint">Numbers reported by each app in this build. An app that reports nothing does not appear.</p>
		{{range .Data.Apps}}
		<h3>{{.Name}}</h3>
		{{if .Err}}
		<p class="notice notice-error">{{.Name}} could not report: {{.Err}}</p>
		{{else}}
		<dl class="admin-facts">
			{{range .Stats}}
			<div class="admin-fact">
				<dt>{{.Label}}</dt>
				<dd class="small">{{.Value}}</dd>
				{{with .Hint}}<dd class="faint">{{.}}</dd>{{end}}
			</div>
			{{end}}
		</dl>
		{{end}}
		{{else}}
		<p class="dim">No app in this build reports any statistics.</p>
		{{end}}
	</section>

	<section class="admin-section" id="users">
		<h2>Users &amp; sessions</h2>
		{{with .Data.SessionsErr}}<p class="notice notice-error">Could not count sessions: {{.}}</p>{{end}}
		<dl class="admin-facts">
			<div class="admin-fact"><dt>Accounts</dt><dd>{{len .Data.Accounts}}</dd></div>
			<div class="admin-fact"><dt>Live sessions</dt><dd>{{.Data.Sessions.Live}}</dd></div>
			<div class="admin-fact"><dt>Expired, not swept</dt><dd>{{.Data.Sessions.Expired}}</dd></div>
		</dl>

		{{with .Data.AccountsErr}}<p class="notice notice-error">Could not list accounts: {{.}}</p>{{end}}
		<div class="scroll-x">
			<table class="admin-table">
				<thead><tr><th>User</th><th>Role</th><th>Created</th><th>Live sessions</th></tr></thead>
				<tbody>
				{{range .Data.Accounts}}
					<tr>
						<td>{{.Username}}</td>
						<td>{{if .IsAdmin}}Administrator{{else}}<span class="dim">User</span>{{end}}</td>
						<td class="dim">{{.CreatedAt.Format "2006-01-02"}}</td>
						<td>{{.Sessions}}</td>
					</tr>
				{{else}}
					<tr><td colspan="4" class="dim">No accounts exist.</td></tr>
				{{end}}
				</tbody>
			</table>
		</div>
	</section>
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/platform/admin/... -race -count=1 -v`
Expected: PASS.

- [ ] **Step 7: Prove one broken collector does not take the page down**

This is the promise the whole per-section error design rests on. Add three
fields to the `server` struct in the harness so a test can build a `Deps` of
its own — `cfg config.Config`, `rend *render.Renderer`, `errs *web.Errors` —
and set them in `newServerWith`. Then add a deliberately broken app and the
test:

```go
type brokenStatApp struct{ statApp }

func (brokenStatApp) Meta() app.Meta {
	return app.Meta{ID: "broken", Name: "ON Broken", Summary: "a stub", Order: 1}
}
func (brokenStatApp) Stats(context.Context, *sql.DB) ([]app.Stat, error) {
	return nil, errors.New("no such table: widgets")
}

// Two collectors fail at once and the page must still be a page: a broken
// PRAGMA must not hide the job status, and one app's bad query must not hide
// another section entirely.
func TestABrokenCollectorRendersInItsOwnCardWithA200(t *testing.T) {
	s := newServer(t)

	failing, err := app.NewRegistry(brokenStatApp{})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	admin.Handler(admin.Deps{
		Config:  s.cfg,
		DB:      nil,     // the database section cannot be collected at all
		Apps:    failing, // and this app's query fails
		Render:  s.rend,
		Errors:  s.errs,
		Version: "v9.9.9",
		Started: time.Now().UTC(),
	}).ServeHTTP(rec, httptest.NewRequest("GET", "/admin/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; a failing collector must not become a 500", rec.Code)
	}

	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#runtime")
	doc.MustHave("#database")
	doc.MustHave("#apps")

	body := doc.Text()
	if !strings.Contains(body, "Could not read the database") {
		t.Error("the database failure is not reported in its own section")
	}
	if !strings.Contains(body, "no such table: widgets") {
		t.Error("the failing app's error is not shown on that app's card")
	}
	if !strings.Contains(body, "v9.9.9") {
		t.Error("the healthy runtime section did not render alongside two broken ones")
	}
}
```

Add `errors` to the test file's imports.

Run: `go test ./internal/platform/admin/... -run TestABrokenCollector -count=1 -v`
Expected: PASS. If it is a 500 instead, a collector is returning its error out
of `collect` rather than recording it on the report.

- [ ] **Step 8: Commit**

```bash
git add internal/platform/admin internal/ui
git commit -m "admin: show per-app statistics, accounts and session counts"
```

---

### Task 12: The route map

**Files:**
- Modify: `internal/platform/admin/admin.go`
- Modify: `internal/ui/templates/admin.html`
- Test: `internal/platform/admin/admin_test.go`

**Interfaces:**
- Consumes: `(*web.Recorder).Routes` (Task 4).
- Produces: `Report.Routes []web.Route`, `Report.PublicRoutes int`.

- [ ] **Step 1: Write the failing test**

Append to `internal/platform/admin/admin_test.go`:

```go
func TestTheRouteMapIncludesPlatformAndAppRoutesWithGuardStatus(t *testing.T) {
	s := newServer(t)
	doc := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String())
	doc.MustHave("#routes")

	body := doc.Text()
	// A map that quietly omitted the platform's own routes would be worse
	// than no map: /login and /static/ are the first two an auditor checks.
	for _, want := range []string{"GET /login", "GET /admin/", "GET /demo/{$}", "GET /demo/s/{slug}"} {
		if !strings.Contains(body, want) {
			t.Errorf("the route map does not list %q", want)
		}
	}

	// Public and guarded must be distinguishable, not just listed.
	public := doc.QueryAll(".admin-tag-public")
	if len(public) == 0 {
		t.Fatal("no route is marked public; the section cannot show default-deny working")
	}
}

func TestTheRouteMapCountsPublicRoutes(t *testing.T) {
	s := newServer(t)
	body := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String()).Text()
	if !strings.Contains(body, "reachable without signing in") {
		t.Error("the route map does not summarise how many routes are public")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/platform/admin/... -run TestTheRouteMap -count=1`
Expected: FAIL — `#routes not found`.

- [ ] **Step 3: Add the data**

In `internal/platform/admin/admin.go`, extend `Report`:

```go
	Routes       []web.Route
	PublicRoutes int
```

and `collect`, at the end before `return rep`:

```go
	rep.Routes = d.Routes.Routes()
	for _, rt := range rep.Routes {
		if rt.Public {
			rep.PublicRoutes++
		}
	}
```

`d.Routes` may be nil; `Recorder.Routes` on a nil receiver already returns nil, so no guard is needed.

- [ ] **Step 4: Add the markup**

One more anchor in `.admin-nav`:

```html
		<a href="#routes">Routes</a>
```

and the final section, after the database section:

```html
	<section class="admin-section" id="routes">
		<h2>Routes</h2>
		<p class="faint">
			Every route this build serves. {{.Data.PublicRoutes}} of {{len .Data.Routes}}
			are reachable without signing in; everything else requires a session.
		</p>
		<div class="scroll-x">
			<table class="admin-table">
				<thead><tr><th>Pattern</th><th>Access</th><th>Owner</th></tr></thead>
				<tbody>
				{{range .Data.Routes}}
					<tr>
						<td><code>{{.Pattern}}</code></td>
						<td>
							{{if .Public}}<span class="admin-tag admin-tag-public">public</span>
							{{else}}<span class="dim">signed in</span>{{end}}
						</td>
						<td class="dim">{{.Owner}}</td>
					</tr>
				{{else}}
					<tr><td colspan="3" class="dim">No routes recorded.</td></tr>
				{{end}}
				</tbody>
			</table>
		</div>
	</section>
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/platform/admin/... -race -count=1 -v`
Expected: PASS.

- [ ] **Step 6: Check the real page**

```bash
go build ./cmd/onsuite && ./onsuite serve --data-dir /tmp/onsuite-admin
```
Load `/admin/#routes` as `root` and confirm `GET /login`, `GET /static/`, `GET /healthz`, `GET /paste/s/{slug}` and `GET /paste/highlight.css` are all listed as public, and that nothing unexpected is.

- [ ] **Step 7: Commit**

```bash
git add internal/platform/admin internal/ui
git commit -m "admin: show the complete route map with public routes marked"
```

---

### Task 13: Documentation and final verification

**Files:**
- Modify: `AGENTS.md`
- Modify: `NEXT.md`
- Modify: `docs/DEPLOYING.md`

- [ ] **Step 1: Document the new capability in AGENTS.md**

In the "Optional app capabilities are discovered by type assertion" paragraph, extend the sentence about `Exporter`:

```markdown
**Optional app capabilities are discovered by type assertion, not interface
bloat**: an app implementing `Templates() fs.FS` gets its templates mounted;
one implementing `Exporter` (`Export(ctx, db, userID) (any, error)`)
participates in `onsuite export` automatically; one implementing `Stater`
(`Stats(ctx, db) ([]app.Stat, error)`) gets a card on the admin page. Apps
that don't implement these are silently skipped — that's a design choice.
```

And add a paragraph to the Architecture section:

```markdown
**Two platform packages exist only for operations.**
[internal/platform/jobs](internal/platform/jobs/jobs.go) is a generic interval
scheduler that remembers how each run went; it takes closures and imports
nothing else in the module, so it never learns what a backup is.
[internal/platform/admin](internal/platform/admin/admin.go) is the read-only
admin page at `/admin/`, guarded by `Auth.RequireAdmin` — a signed-in
non-admin gets the same 404 as a URL that does not exist. It is a platform
page rather than an app because it reports *on* the platform. Its design is in
[docs/superpowers/specs/2026-08-24-admin-page-design.md](docs/superpowers/specs/2026-08-24-admin-page-design.md).
```

- [ ] **Step 2: Point operators at the page in docs/DEPLOYING.md**

Add, in whichever section covers verifying a deployment:

```markdown
Once the server is up, an administrator can open `/admin/` to see which
settings this process actually resolved (and whether each came from a flag,
the environment, or a default), when the last snapshot ran, how large the
database and its write-ahead log have grown, and which routes are reachable
without signing in. It is read-only — nothing on it changes state.
```

- [ ] **Step 3: Remove the shipped item from NEXT.md**

Delete the whole "## Admin page" section, leaving the ON Paste list.

- [ ] **Step 4: Run the full check**

```bash
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
go mod tidy && git diff --exit-code go.mod go.sum
go test ./... -race -count=1
```
Expected: `gofmt -l .` prints nothing, every other command exits 0. **`go.mod` and `go.sum` must be unchanged** — this feature adds no dependency.

- [ ] **Step 5: Commit and open the PR**

```bash
git add AGENTS.md NEXT.md docs/DEPLOYING.md
git commit -m "docs: describe the admin page, the jobs package and the Stater capability"
git push -u origin admin-page
gh pr create --title "Add a read-only admin page" --body "Implements docs/superpowers/specs/2026-08-24-admin-page-design.md"
```

`main` is protected. Do not push to it.

---

## Notes for the implementer

- **Test helper names are approximate.** Tasks 5, 7 and 8 say "existing helper in this file; adapt the name". Open the test file, find how it already builds a store or an `*Auth`, and reuse that rather than adding a second harness.
- **If a section's collector needs a nil guard you did not expect**, add it. `Deps` is filled from `buildStack` in production but partially in tests, and a nil map or store must render an empty section, never panic.
- **Do not add a POST route.** If a section seems to want a button — "run this job now", "sweep sessions" — that is out of scope by decision, not by oversight. Note it and move on.
