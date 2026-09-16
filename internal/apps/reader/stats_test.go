package reader_test

import (
	"context"
	"database/sql"
	"errors"
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

// dailyFetched reads one day's raw fetched count directly, tolerating a
// missing row (BackfillDailyStats writes none for a day where every item on
// it predates every subscription, since that day has nothing to aggregate) —
// DailyStats can't distinguish "0" from "no row" the way this needs to.
func dailyFetched(t *testing.T, f *storeFixture, userID int64, day time.Time) (fetched int, hasRow bool) {
	t.Helper()
	err := f.db.QueryRowContext(context.Background(),
		`SELECT fetched FROM reader_daily_stats WHERE user_id = ? AND day = ?`,
		userID, day.Format("2006-01-02")).Scan(&fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return fetched, true
}

// The subscription-visibility cutoff (an item published/fetched before a
// subscription existed does not count as visible through it, so joining a
// feed someone else already follows does not hand you their backlog) is
// duplicated verbatim across ItemsForScope, FeedStats, RecordDailyStats and
// BackfillDailyStats. Nothing enforces that the four keep agreeing, so this
// cross-checks all four against one shared fixture rather than trusting a
// comment. Issue #243.
func TestSubscriptionVisibilityCutoffAgreesAcrossQueries(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://example.com/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	addedAt := now.Add(-72 * time.Hour)
	if _, err := f.store.DB().ExecContext(ctx,
		`UPDATE reader_subs SET added_at = ? WHERE id = ?`,
		addedAt.Format(time.RFC3339Nano), sub.ID); err != nil {
		t.Fatal(err)
	}

	// One item fetched well before the subscription existed (backlog, must
	// stay invisible everywhere), one fetched well after (must count
	// everywhere) — 48h either side of addedAt so the two always land on
	// distinct calendar days.
	beforeFetch := addedAt.Add(-48 * time.Hour)
	afterFetch := addedAt.Add(48 * time.Hour)
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "before", Title: "Before", PublishedAt: beforeFetch},
	}, beforeFetch); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "after", Title: "After", PublishedAt: afterFetch},
	}, afterFetch); err != nil {
		t.Fatal(err)
	}

	// 1. ItemsForScope: the item-listing query.
	items, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeFeed, sub.ID, reader.FilterAll, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].GUID != "after" {
		t.Fatalf("ItemsForScope returned %+v, want only the item fetched after the subscription existed", items)
	}

	// 2. FeedStats: the per-feed article count on the stats page.
	feeds, err := f.store.FeedStats(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 || feeds[0].Articles != 1 {
		t.Fatalf("FeedStats = %+v, want exactly 1 article counted", feeds)
	}

	// 3. BackfillDailyStats: reconstructs history from surviving articles.
	// The pre-subscription item's day aggregates nothing at all under the
	// same predicate, so it gets no row — not a row with fetched=0.
	if _, err := f.store.BackfillDailyStats(ctx); err != nil {
		t.Fatal(err)
	}
	if fetched, hasRow := dailyFetched(t, f, f.alice.ID, beforeFetch); hasRow {
		t.Errorf("BackfillDailyStats wrote a row for the pre-subscription day (fetched=%d), want no row at all", fetched)
	}
	if fetched, hasRow := dailyFetched(t, f, f.alice.ID, afterFetch); !hasRow || fetched != 1 {
		t.Errorf("BackfillDailyStats: post-subscription day = (fetched=%d, hasRow=%v), want (1, true)", fetched, hasRow)
	}

	// 4. RecordDailyStats: the scheduled job's own per-day write, run for both
	// days directly (ON CONFLICT DO UPDATE, so it overwrites Backfill's rows
	// too) — must agree with what Backfill just reconstructed.
	if err := f.store.RecordDailyStats(ctx, beforeFetch); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RecordDailyStats(ctx, afterFetch); err != nil {
		t.Fatal(err)
	}
	if fetched, hasRow := dailyFetched(t, f, f.alice.ID, beforeFetch); !hasRow || fetched != 0 {
		t.Errorf("RecordDailyStats: pre-subscription day = (fetched=%d, hasRow=%v), want (0, true)", fetched, hasRow)
	}
	if fetched, hasRow := dailyFetched(t, f, f.alice.ID, afterFetch); !hasRow || fetched != 1 {
		t.Errorf("RecordDailyStats: post-subscription day = (fetched=%d, hasRow=%v), want (1, true)", fetched, hasRow)
	}
}
