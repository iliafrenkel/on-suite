package reader

import (
	"context"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync"
	"time"
)

const (
	// pollTick is how often the job wakes. It is not the polling interval:
	// each feed carries its own next_fetch_at, so one tick asks "what is due"
	// rather than the job owning a timer per feed. Consumed by the scheduling
	// loop that Task 8 wires up in app.go; unused within this package alone.
	//
	//lint:ignore U1000 wired up by Task 8's scheduler
	pollTick = 5 * time.Minute
	// pollBatch bounds one run, so a database with hundreds of feeds still
	// finishes a tick promptly and picks the rest up on the next one.
	pollBatch = 64
	// pollWorkers bounds concurrent outbound requests.
	pollWorkers = 4
	// maxBackoff caps the retry interval for a persistently broken feed.
	maxBackoff = 6 * time.Hour
)

// Poller turns a due feed into stored items. It is the only glue between
// fetch.go, parse.go and store.go, none of which know about each other.
type Poller struct {
	store  *Store
	client *Client
	log    *slog.Logger
}

func NewPoller(store *Store, client *Client, log *slog.Logger) *Poller {
	return &Poller{store: store, client: client, log: log}
}

// NextFetchAt is when a feed should be polled again.
//
// Jitter matters more than it looks: without it, forty feeds added in one OPML
// import would poll in lockstep forever, producing a thundering herd every
// interval instead of a trickle.
func NextFetchAt(now time.Time, interval time.Duration, errorCount int) time.Time {
	if interval <= 0 {
		interval = DefaultFetchInterval
	}
	wait := interval
	if errorCount > 0 {
		// Exponential, capped. math.Pow on a bounded exponent keeps this
		// readable and cannot overflow the way repeated doubling can.
		exp := math.Min(float64(errorCount), 12)
		wait = time.Duration(float64(interval) * math.Pow(2, exp))
		if wait > maxBackoff || wait <= 0 {
			wait = maxBackoff
		}
	}
	jitter := time.Duration(rand.Int64N(int64(wait/4) + 1))
	return now.Add(wait + jitter)
}

// PollDue fetches every feed that is due.
//
// A single feed's failure is recorded against that feed and never returned:
// one dead publisher must not stop the other thirty-nine feeds from updating.
// The error return is reserved for the run itself failing, which is what the
// admin page's job status should show red for.
func (p *Poller) PollDue(ctx context.Context) error {
	now := time.Now().UTC()
	feeds, err := p.store.DueFeeds(ctx, now, pollBatch)
	if err != nil {
		return err
	}
	if len(feeds) == 0 {
		return nil
	}

	work := make(chan Feed)
	var wg sync.WaitGroup
	for range pollWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range work {
				p.pollOne(ctx, f)
			}
		}()
	}

	for _, f := range feeds {
		select {
		case <-ctx.Done():
			// Shutdown. Stop handing out work and let the workers drain.
			close(work)
			wg.Wait()
			return nil
		case work <- f:
		}
	}
	close(work)
	wg.Wait()

	p.log.Info("reader poll finished", "feeds", len(feeds))
	return nil
}

// pollOne fetches, parses and stores one feed, recording the outcome either way.
func (p *Poller) pollOne(ctx context.Context, f Feed) {
	now := time.Now().UTC()
	target := f.ResolvedURL
	if target == "" {
		target = f.URL
	}

	res, err := p.client.Get(ctx, target, GetOptions{
		ETag:         f.ETag,
		LastModified: f.LastModified,
		MaxBytes:     MaxFeedBytes,
	})
	if err != nil {
		p.log.Warn("reader feed fetch failed", "feed", f.URL, "error", err)
		p.record(ctx, FetchResult{
			FeedID:      f.ID,
			Status:      statusOf(res),
			Err:         err.Error(),
			FetchedAt:   now,
			NextFetchAt: NextFetchAt(now, f.Interval(), f.ErrorCount+1),
		})
		return
	}

	// 304: nothing changed. Still a successful poll, so the error count
	// resets and the feed goes back to its normal interval.
	if res.NotModified {
		p.record(ctx, FetchResult{
			FeedID:       f.ID,
			ResolvedURL:  res.FinalURL,
			ETag:         f.ETag,
			LastModified: f.LastModified,
			Status:       res.Status,
			FetchedAt:    now,
			NextFetchAt:  NextFetchAt(now, f.Interval(), 0),
		})
		return
	}

	parsed, err := ParseFeed(res.Body, f.URL)
	if err != nil {
		p.log.Warn("reader feed parse failed", "feed", f.URL, "error", err)
		p.record(ctx, FetchResult{
			FeedID:      f.ID,
			Status:      res.Status,
			Err:         err.Error(),
			FetchedAt:   now,
			NextFetchAt: NextFetchAt(now, f.Interval(), f.ErrorCount+1),
		})
		return
	}

	if _, err := p.store.SaveItems(ctx, f.ID, parsed.Items, now); err != nil {
		p.log.Error("reader saving items failed", "feed", f.URL, "error", err)
		p.record(ctx, FetchResult{
			FeedID:      f.ID,
			Status:      res.Status,
			Err:         err.Error(),
			FetchedAt:   now,
			NextFetchAt: NextFetchAt(now, f.Interval(), f.ErrorCount+1),
		})
		return
	}

	p.record(ctx, FetchResult{
		FeedID:       f.ID,
		ResolvedURL:  res.FinalURL,
		Title:        parsed.Title,
		SiteURL:      parsed.SiteURL,
		ETag:         res.ETag,
		LastModified: res.LastModified,
		Status:       res.Status,
		FetchedAt:    now,
		NextFetchAt:  NextFetchAt(now, f.Interval(), 0),
	})
}

func (p *Poller) record(ctx context.Context, r FetchResult) {
	if err := p.store.SaveFetchResult(ctx, r); err != nil {
		p.log.Error("reader recording a poll result failed", "feed_id", r.FeedID, "error", err)
	}
}

func statusOf(res *Response) int {
	if res == nil {
		return 0
	}
	return res.Status
}
