// internal/apps/flash/jobs_test.go
package flash_test

import (
	"errors"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

const purgeJobName = "purge orphan media and tags"

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
// database the harness's s.Store writes to. It also covers #289's tag
// half: a deck delete leaves an orphan flash_tags row (the cascade only
// removes flash_card_tags), and the same job must sweep it too.
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

	tagDeck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "French", "", flash.DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	tagCard, err := s.Store.CreateCard(ctx, s.Alice.User.ID, tagDeck.ID, flash.CardTypeBasic, "oui", "yes", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetCardTags(ctx, s.Alice.User.ID, tagCard.ID, []string{"orphaned-by-deck"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.DeleteDeck(ctx, s.Alice.User.ID, tagDeck.ID); err != nil {
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
	// If the job already swept the orphaned tag, nothing is left for
	// PurgeOrphanTags to find.
	if n, err := s.Store.PurgeOrphanTags(ctx); err != nil {
		t.Fatal(err)
	} else if n != 0 {
		t.Errorf(`the job left %d orphan tag row(s) unswept`, n)
	}
}
