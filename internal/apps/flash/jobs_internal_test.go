// internal/apps/flash/jobs_internal_test.go
package flash

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

// TestPurgeOrphansRunsBothSweepsEvenWhenTheFirstFails pins the fix: the
// daily job must not let a failing media purge skip the tag purge. There is
// no cheap way to force a real PurgeOrphanMedia/PurgeOrphanTags call to
// fail, so this exercises purgeOrphans directly with fake purge funcs — one
// failing, one succeeding — and checks both still ran and that the
// resulting error wraps the one that failed (via errors.Join, so
// errors.Is/As still finds it).
func TestPurgeOrphansRunsBothSweepsEvenWhenTheFirstFails(t *testing.T) {
	mediaErr := errors.New("media purge boom")
	var tagsRan bool

	err := purgeOrphans(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(context.Context) (int, error) { return 0, mediaErr },
		func(context.Context) (int, error) { tagsRan = true; return 3, nil },
	)

	if !tagsRan {
		t.Fatal("the tag purge did not run after the media purge failed")
	}
	if !errors.Is(err, mediaErr) {
		t.Errorf("purgeOrphans error = %v, want it to wrap %v", err, mediaErr)
	}
}

// TestPurgeOrphansJoinsBothErrorsWhenBothFail pins errors.Join's role: if
// both sweeps fail, the caller (the job runner's own failure log) must be
// able to see both, not just whichever happened to be checked first.
func TestPurgeOrphansJoinsBothErrorsWhenBothFail(t *testing.T) {
	mediaErr := errors.New("media purge boom")
	tagErr := errors.New("tag purge boom")

	err := purgeOrphans(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(context.Context) (int, error) { return 0, mediaErr },
		func(context.Context) (int, error) { return 0, tagErr },
	)

	if !errors.Is(err, mediaErr) {
		t.Errorf("purgeOrphans error = %v, want it to wrap %v", err, mediaErr)
	}
	if !errors.Is(err, tagErr) {
		t.Errorf("purgeOrphans error = %v, want it to wrap %v", err, tagErr)
	}
}

// TestPurgeOrphansSucceedsAndLogsCountsWhenBothSweepsSucceed is the
// happy-path pin: no error, and the run does not panic reaching for
// a.deps.Log when both purges return normally.
func TestPurgeOrphansSucceedsAndLogsCountsWhenBothSweepsSucceed(t *testing.T) {
	err := purgeOrphans(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(context.Context) (int, error) { return 2, nil },
		func(context.Context) (int, error) { return 5, nil },
	)
	if err != nil {
		t.Fatalf("purgeOrphans: %v", err)
	}
}
