package webfetch

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// MaxImageAttempts is how many consecutive failures an image or favicon is
// allowed before it is given up on permanently. This is a household-scale
// app with no per-image retry queue, so "permanently" just means "until the
// publisher fixes it and re-publishes the item with a new image" — three
// tries is enough to ride out a blip without letting one dead image become an
// indefinite background retry burden.
const MaxImageAttempts = 3

// ImageRetryBackoff is how long to wait after a failure before trying again.
// An hour is long enough that a transient DNS blip or a publisher's brief
// 503 has almost certainly cleared, and short enough that a real reader
// browsing the next day sees a recovered image rather than a permanent gap —
// this app has no external signal (webhook, cron) to know when to retry
// sooner, so time is the only backoff signal available.
const ImageRetryBackoff = 1 * time.Hour

// GivenUp reports whether a proxy should refuse to fetch a resource at now:
// it has failed MaxImageAttempts times, or it failed recently and is still
// inside ImageRetryBackoff. A failure older than the backoff, under the cap,
// is retried; nothing but a success ever clears the error count, so without
// that a resource that failed once from a DNS blip would 404 forever.
func GivenUp(errorCount int, lastAttempt, now time.Time) bool {
	return errorCount >= MaxImageAttempts ||
		(errorCount > 0 && now.Sub(lastAttempt) < ImageRetryBackoff)
}

// GetImage fetches srcURL and returns its sniffed content type and bytes.
//
// Sniff rather than trust: a publisher claiming image/png over an HTML
// document is exactly how a proxy becomes an HTML-injection vector on its
// own origin.
func (c *Client) GetImage(ctx context.Context, srcURL string, maxBytes int64) (string, []byte, error) {
	res, err := c.Get(ctx, srcURL, GetOptions{MaxBytes: maxBytes, Accept: "image/*"})
	if err != nil {
		return "", nil, err
	}
	ct := http.DetectContentType(res.Body)
	if !strings.HasPrefix(ct, "image/") {
		return "", nil, errors.New("webfetch: response is not an image (" + ct + ")")
	}
	return ct, res.Body, nil
}
