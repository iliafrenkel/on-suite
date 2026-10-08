// internal/apps/flash/media_fetch.go
package flash

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// Body size caps, applied to the decoded stream so a compression bomb is
// truncated rather than expanded.
const (
	MaxImageFetchBytes = 5 << 20
	MaxAudioFetchBytes = 10 << 20
)

// mediaMaxBytes returns the fetch size cap for kind.
func mediaMaxBytes(kind string) int64 {
	if kind == MediaKindAudio {
		return MaxAudioFetchBytes
	}
	return MaxImageFetchBytes
}

// NewMediaClient returns Flash's outbound client: webfetch's guards (SSRF
// check at dial time, redirect cap, body cap after decompression) with
// Flash's User-Agent. It sends no Accept header, and every fetch passes its
// own cap via mediaMaxBytes.
func NewMediaClient(version string) *webfetch.Client {
	return webfetch.New(webfetch.Config{
		UserAgent:       "onsuite/" + version + " (ON Flash; +https://github.com/iliafrenkel/on-suite)",
		DefaultMaxBytes: MaxImageFetchBytes,
	})
}

// FetchAndCacheMedia fetches m's source URL, validates the sniffed
// content-type matches m.Kind, and caches the result via SaveMediaBytes on
// success. It does not record a failure on error — the caller (which knows
// the retry-backoff policy) decides whether and how to record one.
func (st *Store) FetchAndCacheMedia(ctx context.Context, client *webfetch.Client, m Media, now time.Time) (Media, error) {
	res, err := client.Get(ctx, m.SourceURL, webfetch.GetOptions{MaxBytes: mediaMaxBytes(m.Kind)})
	if err != nil {
		return Media{}, err
	}
	body := res.Body

	// Sniff rather than trust: a publisher claiming image/png over an HTML
	// document is exactly how a proxy becomes an HTML-injection vector on
	// its own origin.
	ct := http.DetectContentType(body)
	if !contentTypeMatchesKind(ct, m.Kind) {
		return Media{}, fmt.Errorf("flash: response is not %s media (%s)", m.Kind, ct)
	}

	if err := st.SaveMediaBytes(ctx, m.Hash, ct, body, now); err != nil {
		return Media{}, err
	}
	m.ContentType = ct
	m.Bytes = body
	return m, nil
}
