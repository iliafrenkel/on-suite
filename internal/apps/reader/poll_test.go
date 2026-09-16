package reader_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNextFetchAtBacksOffOnRepeatedFailures(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	interval := 30 * time.Minute

	healthy := reader.NextFetchAt(now, interval, 0)
	if d := healthy.Sub(now); d < interval || d > interval+interval/4 {
		t.Errorf("healthy feed next at +%v; want interval plus at most 25%% jitter", d)
	}

	var last time.Duration
	for _, errs := range []int{1, 2, 3, 4} {
		d := reader.NextFetchAt(now, interval, errs).Sub(now)
		if d <= last {
			t.Errorf("errorCount %d gave +%v, not longer than the previous +%v", errs, d, last)
		}
		last = d
	}

	if d := reader.NextFetchAt(now, interval, 99).Sub(now); d > 7*time.Hour {
		t.Errorf("backoff reached +%v; it must be capped near 6h", d)
	}
}

func TestPollDueFetchesParsesAndStores(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write(fixture(t, "rss2.xml"))
	}))
	defer srv.Close()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, srv.URL+"/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	client := reader.NewClient("test")
	client.DenyAddr = func(string) error { return nil }

	if err := reader.NewPoller(f.store, client, quietLogger()).PollDue(ctx); err != nil {
		t.Fatalf("PollDue: %v", err)
	}

	items, err := f.store.ItemsForSubscription(ctx, f.alice.ID, sub.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("stored %d items, want 1", len(items))
	}
	if items[0].Title != "First post" {
		t.Errorf("Title = %q", items[0].Title)
	}
	if items[0].FeedName != "Example Blog" {
		t.Errorf("feed title not saved from the document: %q", items[0].FeedName)
	}
}

func TestPollDueSkipsAFeedThatIsNotDue(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write(fixture(t, "rss2.xml"))
	}))
	defer srv.Close()

	if _, err := f.store.Subscribe(ctx, f.alice.ID, srv.URL+"/feed.xml", nil); err != nil {
		t.Fatal(err)
	}
	client := reader.NewClient("test")
	client.DenyAddr = func(string) error { return nil }
	poller := reader.NewPoller(f.store, client, quietLogger())

	if err := poller.PollDue(ctx); err != nil {
		t.Fatal(err)
	}
	if err := poller.PollDue(ctx); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Errorf("feed fetched %d times; the second poll must find it not due", hits)
	}
}

func TestPollDueSkipsAnOverlappingRun(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	started := make(chan struct{})
	release := make(chan struct{})
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		close(started)
		<-release
		_, _ = w.Write(fixture(t, "rss2.xml"))
	}))
	defer srv.Close()

	if _, err := f.store.Subscribe(ctx, f.alice.ID, srv.URL+"/feed.xml", nil); err != nil {
		t.Fatal(err)
	}
	client := reader.NewClient("test")
	client.DenyAddr = func(string) error { return nil }
	poller := reader.NewPoller(f.store, client, quietLogger())

	done := make(chan error, 1)
	go func() { done <- poller.PollDue(ctx) }()
	<-started // the first run is now mid-fetch, holding the guard

	if err := poller.PollDue(ctx); err != nil {
		t.Fatalf("overlapping PollDue returned %v; it must be a no-op, not an error", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first PollDue: %v", err)
	}

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("feed fetched %d times; the overlapping run must not have polled again", got)
	}
}

func TestPollDueRecordsAFailureWithoutFailingTheRun(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := f.store.Subscribe(ctx, f.alice.ID, srv.URL+"/feed.xml", nil); err != nil {
		t.Fatal(err)
	}
	client := reader.NewClient("test")
	client.DenyAddr = func(string) error { return nil }

	// One broken feed must not fail the whole run: forty feeds means one dead
	// publisher would otherwise stop the other thirty-nine from updating.
	if err := reader.NewPoller(f.store, client, quietLogger()).PollDue(ctx); err != nil {
		t.Fatalf("PollDue returned %v; a single feed's failure must be recorded, not returned", err)
	}

	var errCount int
	var lastErr string
	if err := f.db.QueryRowContext(ctx,
		`SELECT error_count, last_error FROM reader_feeds`).Scan(&errCount, &lastErr); err != nil {
		t.Fatal(err)
	}
	if errCount != 1 {
		t.Errorf("error_count = %d, want 1", errCount)
	}
	if lastErr == "" {
		t.Error("last_error is empty; the tree needs something to show")
	}
}

func TestFetchNowFetchesAFeedRegardlessOfDueStatus(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(fixture(t, "rss2.xml"))
	}))
	defer srv.Close()

	sub, err := f.store.Subscribe(ctx, f.alice.ID, srv.URL+"/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}

	client := reader.NewClient("test")
	client.DenyAddr = func(string) error { return nil }
	poller := reader.NewPoller(f.store, client, quietLogger())

	// Mark the feed as not due for another hour, the way a normal poll leaves
	// it — FetchNow must still fetch it, which is the entire point of the
	// method: "refresh this one, right now" cannot wait on next_fetch_at.
	if _, err := f.db.ExecContext(ctx,
		`UPDATE reader_feeds SET next_fetch_at = ? WHERE id = ?`,
		time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano), sub.FeedID); err != nil {
		t.Fatal(err)
	}

	if err := poller.FetchNow(ctx, sub.FeedID); err != nil {
		t.Fatalf("FetchNow: %v", err)
	}

	items, err := f.store.ItemsForSubscription(ctx, f.alice.ID, sub.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("stored %d items, want 1", len(items))
	}
}

func TestFetchNowReturnsErrNotFoundForAMissingFeed(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()

	client := reader.NewClient("test")
	poller := reader.NewPoller(f.store, client, quietLogger())

	if err := poller.FetchNow(ctx, 999999); !errors.Is(err, reader.ErrNotFound) {
		t.Errorf("FetchNow(missing) = %v, want ErrNotFound", err)
	}
}

// TestFetchOnAddDerivesAFaviconGuessForADirectFeedURL guards the common case
// this task exists for: pasting a feed URL directly never fetches the site's
// homepage (see TestSubscribeToADirectFeedURLStillWorks), so the favicon
// must come from a pure string derivation off the feed's own site URL, with
// no extra request to the origin.
func TestFetchOnAddDerivesAFaviconGuessForADirectFeedURL(t *testing.T) {
	s, a := newServerWithApp(t)
	ctx := context.Background()

	var hits int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(fixture(t, "rss2.xml")) // <link>https://example.com/</link>
	}))
	defer origin.Close()
	a.AllowPrivateFetchesForTest()

	if rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{"url": {origin.URL + "/feed.xml"}}); rec.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d", rec.Code)
	}
	if hits != 2 {
		t.Fatalf("origin was fetched %d times, want 2 (discovery + fetch-on-add) — a favicon fetch would make this 3", hits)
	}

	tree, err := s.Store.Tree(ctx, s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root) != 1 {
		t.Fatalf("subscribed to %d feeds, want 1", len(tree.Root))
	}
	if tree.Root[0].FaviconURL != "https://example.com/favicon.ico" {
		t.Errorf("FaviconURL = %q, want the derived /favicon.ico guess", tree.Root[0].FaviconURL)
	}
}
