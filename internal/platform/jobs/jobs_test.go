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
