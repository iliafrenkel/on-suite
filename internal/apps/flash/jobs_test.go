// internal/apps/flash/jobs_test.go
package flash_test

import (
	"errors"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

const purgeJobName = "purge orphan media"

// TestFlashRegistersTheDailyMediaPurge mirrors Reader's
// TestReaderRegistersBothJobs: Jobs must be callable before Mount (the
// closure only reads the store when it runs).
func TestFlashRegistersTheDailyMediaPurge(t *testing.T) {
	var sched app.Scheduler = flash.New()
	jobs := sched.Jobs(app.Deps{})
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1: %+v", len(jobs), jobs)
	}
	j := jobs[0]
	if j.Name != purgeJobName {
		t.Errorf("Name = %q, want %q", j.Name, purgeJobName)
	}
	if j.Every != 24*time.Hour {
		t.Errorf("Every = %v, want daily", j.Every)
	}
	if j.Description == "" {
		t.Error("the admin page lists jobs by description; it is empty")
	}
	if j.Run == nil {
		t.Error("the job has no Run function")
	}
}

// TestMediaPurgeJobRunsAgainstTheAppsOwnStore runs the registered job on a
// mounted app: it must purge through the store Mount built, over the same
// database the harness's s.Store writes to.
func TestMediaPurgeJobRunsAgainstTheAppsOwnStore(t *testing.T) {
	s, a := newServerWithApp(t)
	ctx := t.Context()

	orphan, err := s.Store.SaveMediaUpload(ctx, flash.MediaKindImage, "image/png", onePNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	deck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Animals", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "cat", "gato", "")
	if err != nil {
		t.Fatal(err)
	}
	kept, err := s.Store.AttachCardUpload(ctx, s.Alice.User.ID, deck.ID, c.ID, flash.MediaKindImage, "image/png", onePNG2)
	if err != nil {
		t.Fatal(err)
	}

	var job app.Job
	for _, j := range a.Jobs(app.Deps{}) {
		if j.Name == purgeJobName {
			job = j
		}
	}
	if job.Run == nil {
		t.Fatalf("no %q job", purgeJobName)
	}
	if err := job.Run(ctx); err != nil {
		t.Fatalf("job: %v", err)
	}

	if _, err := s.Store.MediaByHash(ctx, orphan); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("orphan after the job: err = %v, want ErrNotFound", err)
	}
	if _, err := s.Store.MediaByHash(ctx, kept); err != nil {
		t.Errorf("attached image after the job: %v", err)
	}
}
