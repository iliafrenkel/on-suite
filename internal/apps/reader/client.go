package reader

import "github.com/iliafrenkel/on-suite/internal/platform/webfetch"

// MaxFeedBytes bounds a feed fetch; it is also the client's default cap.
const MaxFeedBytes = 5 << 20

// feedAccept is sent when a caller doesn't ask for something else: feed
// polling is what most of Reader's fetches are.
const feedAccept = "application/atom+xml, application/rss+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.5"

// NewClient returns Reader's outbound client: webfetch's guards, Reader's
// User-Agent, a feed-shaped Accept header and the feed body cap by default.
func NewClient(version string) *webfetch.Client {
	return webfetch.New(webfetch.Config{
		UserAgent:       "onsuite/" + version + " (ON Reader; +https://github.com/iliafrenkel/on-suite)",
		DefaultAccept:   feedAccept,
		DefaultMaxBytes: MaxFeedBytes,
	})
}
