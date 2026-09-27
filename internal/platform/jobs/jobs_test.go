package jobs_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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

// runNow triggers a job by slug and waits for it to be recorded.
func runNow(t *testing.T, reg *jobs.Registry, slug string) {
	t.Helper()
	if err := reg.Trigger(slug); err != nil {
		t.Fatalf("Trigger(%q) = %v", slug, err)
	}
	reg.Wait()
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

// controllableClock lets a test move time forward between two points it
// controls precisely, unlike stepClock which always advances by a fixed
// step on every read.
type controllableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *controllableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *controllableClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func TestNextRunReflectsWhenRunActuallyStartsNotWhenRegisterWasCalled(t *testing.T) {
	clock := &controllableClock{now: time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)}
	reg := jobs.NewRegistry()
	reg.SetClock(clock.Now)

	reg.Register("sweep", "removes old rows", time.Hour, func(context.Context) error { return nil })
	atRegister := reg.Snapshot()[0].NextRun

	// A real gap between Register and Run: a deployment builds its job
	// registry well before the server starts serving background work.
	clock.Set(clock.Now().Add(30 * time.Minute))
	want := clock.Now().Add(time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go reg.Run(ctx)

	var got time.Time
	for i := 0; i < 200; i++ {
		got = reg.Snapshot()[0].NextRun
		if got.Equal(want) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !got.Equal(want) {
		t.Fatalf("NextRun = %v once Run started, want %v (the register-time value was %v)", got, want, atRegister)
	}
	if got.Equal(atRegister) {
		t.Error("NextRun still holds the register-time value; Run never corrected it")
	}
}

func TestAFailingJobIsRecordedAndTheRegistryKeepsGoing(t *testing.T) {
	reg := jobs.NewRegistry()
	reg.Register("sweep", "removes old rows", time.Hour, func(context.Context) error {
		return errors.New("disk on fire")
	})
	runNow(t, reg, "sweep")

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
	runNow(t, reg, "boom")

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

	runNow(t, reg, "flaky")
	fail = false
	runNow(t, reg, "flaky")

	if got := reg.Snapshot()[0].LastErr; got != "" {
		t.Errorf("LastErr = %q after a later success; a stale error is worse than none", got)
	}
}

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
