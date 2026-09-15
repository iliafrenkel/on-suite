package reader

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// validFaviconHash reports whether the path segment could be one of our
// hashes. Checked before any database work so a probe costs nothing.
func validFaviconHash(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// bareNotFound answers a failed-favicon request with a bare 404 rather than
// the app's full HTML error page. An <img> tag hits this on a known-dead
// favicon (an unknown hash, or one still in backoff after repeated
// failures) on nearly every sidebar render — the tree is swapped via htmx
// on almost every click — so rendering the whole page template here would
// turn one click into dozens of full-page executions delivered as a broken
// image. The Cache-Control hint gives browsers that honor it a shot at not
// re-requesting a favicon we already know is dead within the hour; it
// matches imageRetryBackoff, our own retry window for the same URL.
func bareNotFound(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, max-age="+strconv.Itoa(int(imageRetryBackoff.Seconds())))
	w.WriteHeader(http.StatusNotFound)
}

// favicon serves a proxied feed favicon. It mirrors image() in imgproxy.go —
// same hash-not-URL security model, same cache/backoff behavior — over the
// separate reader_feed_icons table.
func (a *App) favicon(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	hash := r.PathValue("hash")
	if !validFaviconHash(hash) {
		bareNotFound(w)
		return
	}

	icon, err := a.store.FeedIconByHash(r.Context(), hash)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			bareNotFound(w)
			return
		}
		a.fail(w, r, err)
		return
	}
	if icon.Cached() {
		a.writeFeedIcon(w, r, icon)
		return
	}
	if icon.ErrorCount >= maxImageFetchAttempts ||
		(icon.ErrorCount > 0 && time.Since(icon.FetchedAt) < imageRetryBackoff) {
		bareNotFound(w)
		return
	}

	fetched, err := a.fetchFeedIcon(r, icon)
	if err != nil {
		if errors.Is(err, context.Canceled) || r.Context().Err() != nil {
			a.deps.Log.Info("reader favicon fetch canceled", "src", icon.SrcURL, "error", err)
			return
		}
		a.deps.Log.Info("reader favicon fetch failed", "src", icon.SrcURL, "error", err)
		if err := a.store.SaveFeedIconFailure(r.Context(), hash, err.Error(), time.Now().UTC()); err != nil {
			a.deps.Log.Error("reader recording a favicon failure failed", "error", err)
		}
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	a.writeFeedIcon(w, r, fetched)
}

// fetchFeedIcon retrieves and caches one favicon.
func (a *App) fetchFeedIcon(r *http.Request, icon FeedIcon) (FeedIcon, error) {
	select {
	case a.imgSem <- struct{}{}:
		defer func() { <-a.imgSem }()
	case <-r.Context().Done():
		return FeedIcon{}, r.Context().Err()
	}

	res, err := a.client.Get(r.Context(), icon.SrcURL, GetOptions{
		MaxBytes: MaxFaviconBytes,
		Accept:   "image/*",
	})
	if err != nil {
		return FeedIcon{}, err
	}

	ct := http.DetectContentType(res.Body)
	if !strings.HasPrefix(ct, "image/") {
		return FeedIcon{}, errors.New("reader: response is not an image (" + ct + ")")
	}

	if err := a.store.SaveFeedIconBytes(r.Context(), icon.Hash, ct, res.Body, time.Now().UTC()); err != nil {
		return FeedIcon{}, err
	}
	icon.ContentType = ct
	icon.Bytes = res.Body
	return icon, nil
}

func (a *App) writeFeedIcon(w http.ResponseWriter, r *http.Request, icon FeedIcon) {
	etag := `"` + icon.Hash + `"`
	h := w.Header()
	h.Set("Cache-Control", imageCacheControl)
	h.Set("ETag", etag)
	h.Set("X-Content-Type-Options", "nosniff")

	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", icon.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(icon.Bytes)))
	if _, err := w.Write(icon.Bytes); err != nil {
		a.deps.Log.Info("reader writing a favicon failed", "error", err)
	}
}
