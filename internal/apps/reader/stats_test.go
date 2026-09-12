package reader_test

import (
	"context"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

func TestRecordDailyStatsCountsTodaysActivity(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Backdate the subscription so it predates the items below: Subscribe
	// stamps added_at from the wall clock at call time, which would otherwise
	// land after "now" and make every item look like backlog from before the
	// subscription existed.
	if _, err := f.store.DB().ExecContext(ctx,
		`UPDATE reader_subs SET added_at = ? WHERE id = ?`,
		now.Add(-24*time.Hour).Format(time.RFC3339Nano), sub.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "A", PublishedAt: now.Add(-2 * time.Hour)},
		{GUID: "b", Title: "B", PublishedAt: now.Add(-time.Hour)},
	}, now); err != nil {
		t.Fatal(err)
	}
	items, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeAll, 0, reader.FilterAll, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetRead(ctx, f.alice.ID, items[0].ID, true, now); err != nil {
		t.Fatal(err)
	}

	if err := f.store.RecordDailyStats(ctx, now); err != nil {
		t.Fatalf("RecordDailyStats: %v", err)
	}

	days, err := f.store.DailyStats(ctx, f.alice.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 7 {
		t.Fatalf("DailyStats(7) returned %d days, want 7 including empty ones", len(days))
	}
	today := days[len(days)-1]
	if today.Fetched != 2 {
		t.Errorf("Fetched = %d, want 2", today.Fetched)
	}
	if today.Read != 1 {
		t.Errorf("Read = %d, want 1", today.Read)
	}
	if today.Backlog != 1 {
		t.Errorf("Backlog = %d, want 1", today.Backlog)
	}
}

// The nightly job runs every night forever, so recording twice in one day must
// update rather than accumulate.
func TestRecordDailyStatsIsIdempotent(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx,
		`UPDATE reader_subs SET added_at = ? WHERE id = ?`,
		now.Add(-24*time.Hour).Format(time.RFC3339Nano), sub.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "A", PublishedAt: now.Add(-time.Hour)},
	}, now); err != nil {
		t.Fatal(err)
	}

	for range 3 {
		if err := f.store.RecordDailyStats(ctx, now); err != nil {
			t.Fatal(err)
		}
	}
	days, err := f.store.DailyStats(ctx, f.alice.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if days[0].Fetched != 1 {
		t.Errorf("Fetched = %d after three recordings, want 1", days[0].Fetched)
	}
}

func TestDailyStatsIsPerUser(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx,
		`UPDATE reader_subs SET added_at = ? WHERE id = ?`,
		now.Add(-24*time.Hour).Format(time.RFC3339Nano), sub.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "A", PublishedAt: now.Add(-time.Hour)},
	}, now); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RecordDailyStats(ctx, now); err != nil {
		t.Fatal(err)
	}

	days, err := f.store.DailyStats(ctx, f.bob.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if days[0].Fetched != 0 {
		t.Errorf("bob sees %d fetched; he subscribes to nothing", days[0].Fetched)
	}
}

func TestBackfillMarksRowsReconstructed(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Backdate the subscription to before the item's fetch time, or the
	// added_at cutoff would treat five-day-old article as backlog from before
	// alice subscribed and exclude it entirely.
	if _, err := f.store.DB().ExecContext(ctx,
		`UPDATE reader_subs SET added_at = ? WHERE id = ?`,
		now.Add(-6*24*time.Hour).Format(time.RFC3339Nano), sub.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "A", PublishedAt: now.Add(-5 * 24 * time.Hour)},
	}, now.Add(-5*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	n, err := f.store.BackfillDailyStats(ctx)
	if err != nil {
		t.Fatalf("BackfillDailyStats: %v", err)
	}
	if n == 0 {
		t.Fatal("backfill wrote no rows despite five days of history")
	}

	days, err := f.store.DailyStats(ctx, f.alice.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	var sawReconstructed bool
	for _, d := range days {
		if d.Reconstructed && d.Fetched > 0 {
			sawReconstructed = true
		}
	}
	if !sawReconstructed {
		t.Error("backfilled rows are not marked reconstructed; the page would present them as measured")
	}
}

func TestAppStatsReportsTheInstallation(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	if _, err := f.store.Subscribe(ctx, f.alice.ID, "https://a.example/feed.xml", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Subscribe(ctx, f.bob.ID, "https://a.example/feed.xml", nil); err != nil {
		t.Fatal(err)
	}

	got, err := reader.New().Stats(ctx, f.store.DB())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Stats returned nothing; the admin card would be empty")
	}

	byLabel := map[string]string{}
	for _, s := range got {
		byLabel[s.Label] = s.Value
	}
	// One feed URL, two subscribers — the shared-feed design is exactly what
	// an operator wants to see confirmed here.
	if byLabel["Feeds"] != "1" {
		t.Errorf("Feeds = %q, want 1", byLabel["Feeds"])
	}
	if byLabel["Subscriptions"] != "2" {
		t.Errorf("Subscriptions = %q, want 2", byLabel["Subscriptions"])
	}
}

// Stats takes a *sql.DB rather than using Mount's Deps, so it must work on an
// App that was never mounted — that is the contract every Stater has.
func TestAppStatsWorksWithoutMount(t *testing.T) {
	f := newStoreFixture(t)
	if _, err := reader.New().Stats(context.Background(), f.store.DB()); err != nil {
		t.Errorf("Stats on an unmounted app: %v", err)
	}
}

// Backfill must never overwrite a day that was measured for real.
func TestBackfillDoesNotOverwriteMeasuredDays(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB().ExecContext(ctx,
		`UPDATE reader_subs SET added_at = ? WHERE id = ?`,
		now.Add(-24*time.Hour).Format(time.RFC3339Nano), sub.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "A", PublishedAt: now.Add(-time.Hour)},
	}, now); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RecordDailyStats(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.BackfillDailyStats(ctx); err != nil {
		t.Fatal(err)
	}

	days, err := f.store.DailyStats(ctx, f.alice.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if days[0].Reconstructed {
		t.Error("backfill overwrote a measured day and marked it reconstructed")
	}
}
