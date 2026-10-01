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
	"github.com/iliafrenkel/on-suite/internal/platform/db"
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
		db.FormatTime(time.Now().UTC().Add(time.Hour)), sub.FeedID); err != nil {
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

// siteOrigin serves a feed at /feed.xml whose <link> is the origin itself, and
// hands every other path to home — the site's homepage, as far as favicon
// discovery is concerned. homeHits counts requests to "/".
type siteOrigin struct {
	*httptest.Server
	homeHits atomic.Int32
}

func newSiteOrigin(t *testing.T, home http.HandlerFunc) *siteOrigin {
	t.Helper()
	o := &siteOrigin{}
	mux := http.NewServeMux()
	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel>
			<title>Site</title><link>` + o.URL + `</link>
			<item><title>One</title><link>` + o.URL + `/one</link><guid>one</guid></item>
			</channel></rss>`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			o.homeHits.Add(1)
		}
		home(w, r)
	})
	o.Server = httptest.NewServer(mux)
	t.Cleanup(o.Close)
	return o
}

// homeWithIcon is a homepage that declares its icon the way Quanta's does —
// the real one, while /favicon.ico (unrouted here) is no use.
func homeWithIcon(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(`<html><head><link rel="icon" href="/static/icon.png"></head><body>hi</body></html>`))
}

func homeBroken(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "down", http.StatusInternalServerError)
}

// Adding a feed by its feed URL reads the site's homepage once for its
// <link rel="icon"> (#451): a site's /favicon.ico can be empty while its
// homepage declares the real icon.
func TestFetchOnAddReadsTheSiteHomepageForItsFavicon(t *testing.T) {
	s, a := newServerWithApp(t)
	ctx := context.Background()
	origin := newSiteOrigin(t, homeWithIcon)
	a.AllowPrivateFetchesForTest()

	if rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{"url": {origin.URL + "/feed.xml"}}); rec.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d", rec.Code)
	}

	tree, err := s.Store.Tree(ctx, s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root) != 1 {
		t.Fatalf("subscribed to %d feeds, want 1", len(tree.Root))
	}
	if want := origin.URL + "/static/icon.png"; tree.Root[0].FaviconURL != want {
		t.Errorf("FaviconURL = %q, want the homepage's icon %q", tree.Root[0].FaviconURL, want)
	}
	if n := origin.homeHits.Load(); n != 1 {
		t.Errorf("homepage fetched %d times, want exactly 1", n)
	}
	feed, err := s.Store.FeedByID(ctx, tree.Root[0].FeedID)
	if err != nil {
		t.Fatal(err)
	}
	if !feed.FaviconPageChecked {
		t.Error("FaviconPageChecked = false after reading the homepage")
	}
}

// A homepage that cannot be read leaves the poller's /favicon.ico guess in
// place, and the feed unchecked so the poller's repair can still try later.
func TestFetchOnAddKeepsTheFaviconGuessWhenTheHomepageFails(t *testing.T) {
	s, a := newServerWithApp(t)
	ctx := context.Background()
	origin := newSiteOrigin(t, homeBroken)
	a.AllowPrivateFetchesForTest()

	if rec := s.PostHX(t, s.Alice, "/reader/subscribe", url.Values{"url": {origin.URL + "/feed.xml"}}); rec.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d", rec.Code)
	}

	tree, err := s.Store.Tree(ctx, s.Alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Root) != 1 {
		t.Fatalf("subscribed to %d feeds, want 1", len(tree.Root))
	}
	if want := origin.URL + "/favicon.ico"; tree.Root[0].FaviconURL != want {
		t.Errorf("FaviconURL = %q, want the derived guess %q", tree.Root[0].FaviconURL, want)
	}
	feed, err := s.Store.FeedByID(ctx, tree.Root[0].FeedID)
	if err != nil {
		t.Fatal(err)
	}
	if feed.FaviconPageChecked {
		t.Error("FaviconPageChecked = true, but the homepage was never read")
	}
}

// pollAgain makes a feed due and runs one poll cycle.
func pollAgain(t *testing.T, f *storeFixture, poller *reader.Poller, feedID int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.db.ExecContext(ctx, `UPDATE reader_feeds SET next_fetch_at = ? WHERE id = ?`,
		db.FormatTime(time.Now().UTC().Add(-time.Minute)), feedID); err != nil {
		t.Fatal(err)
	}
	if err := poller.PollDue(ctx); err != nil {
		t.Fatal(err)
	}
}

// killFavicon records enough failed fetches that the proxy gives up on the
// feed's current favicon for good.
func killFavicon(t *testing.T, f *storeFixture, feedID int64) {
	t.Helper()
	ctx := context.Background()
	feed, err := f.store.FeedByID(ctx, feedID)
	if err != nil {
		t.Fatal(err)
	}
	if feed.FaviconURL == "" {
		t.Fatal("feed has no favicon to kill")
	}
	for range reader.MaxImageFetchAttemptsForTest {
		if err := f.store.SaveFeedIconFailure(ctx, reader.FaviconHash(feed.FaviconURL), "not an image", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
}

func newTestPoller(f *storeFixture) *reader.Poller {
	client := reader.NewClient("test")
	client.DenyAddr = func(string) error { return nil }
	return reader.NewPoller(f.store, client, quietLogger())
}

// Feeds added before #451 can be stuck on a dead /favicon.ico guess forever,
// since SetFaviconIfEmpty never overwrites. Once the proxy has given up on
// it, the poller reads the homepage once to repair it.
func TestPollRepairsADeadFaviconGuessFromTheHomepageOnce(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	origin := newSiteOrigin(t, homeWithIcon)
	poller := newTestPoller(f)

	sub, err := f.store.Subscribe(ctx, f.alice.ID, origin.URL+"/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	pollAgain(t, f, poller, sub.FeedID) // first poll: the /favicon.ico guess
	if n := origin.homeHits.Load(); n != 0 {
		t.Fatalf("homepage fetched %d times before the guess died, want 0", n)
	}
	killFavicon(t, f, sub.FeedID)

	pollAgain(t, f, poller, sub.FeedID)

	feed, err := f.store.FeedByID(ctx, sub.FeedID)
	if err != nil {
		t.Fatal(err)
	}
	if want := origin.URL + "/static/icon.png"; feed.FaviconURL != want {
		t.Errorf("FaviconURL = %q, want the homepage's icon %q", feed.FaviconURL, want)
	}

	killFavicon(t, f, sub.FeedID)
	pollAgain(t, f, poller, sub.FeedID)
	if n := origin.homeHits.Load(); n != 1 {
		t.Errorf("homepage fetched %d times, want exactly 1 — the repair runs once per feed", n)
	}
}

func TestPollLeavesAWorkingFaviconAlone(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	origin := newSiteOrigin(t, homeWithIcon)
	poller := newTestPoller(f)

	sub, err := f.store.Subscribe(ctx, f.alice.ID, origin.URL+"/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	pollAgain(t, f, poller, sub.FeedID)
	pollAgain(t, f, poller, sub.FeedID)

	if n := origin.homeHits.Load(); n != 0 {
		t.Errorf("homepage fetched %d times for a favicon that never failed, want 0", n)
	}
}

// A repair that cannot read the homepage still counts as the one attempt, so
// a broken site is not re-fetched on every poll.
func TestPollRepairMarksTheFeedCheckedEvenWhenTheHomepageFails(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	origin := newSiteOrigin(t, homeBroken)
	poller := newTestPoller(f)

	sub, err := f.store.Subscribe(ctx, f.alice.ID, origin.URL+"/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	pollAgain(t, f, poller, sub.FeedID)
	killFavicon(t, f, sub.FeedID)
	pollAgain(t, f, poller, sub.FeedID)
	pollAgain(t, f, poller, sub.FeedID)

	if n := origin.homeHits.Load(); n != 1 {
		t.Errorf("homepage fetched %d times, want exactly 1", n)
	}
	feed, err := f.store.FeedByID(ctx, sub.FeedID)
	if err != nil {
		t.Fatal(err)
	}
	if !feed.FaviconPageChecked {
		t.Error("FaviconPageChecked = false after the repair attempt")
	}
	if want := origin.URL + "/favicon.ico"; feed.FaviconURL != want {
		t.Errorf("FaviconURL = %q, want the guess %q kept", feed.FaviconURL, want)
	}
}
