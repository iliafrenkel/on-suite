package later

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// imageCacheControl: private (signed-in only) and long (content-addressed).
const imageCacheControl = "private, max-age=31536000, immutable"

// image serves a stored image, fetching it first if it hasn't been yet.
// It takes a hash, never a URL, and only hashes one of the viewer's own
// articles links resolve — so nothing here can be made to fetch anything
// the save path didn't already see.
func (a *App) image(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	hash := r.PathValue("hash")
	if !webfetch.ValidURLHash(hash) {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	img, err := a.store.ImageForUser(r.Context(), userID, hash)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if !img.Cached() {
		if webfetch.GivenUp(img.ErrorCount, img.FetchedAt, a.store.now()) {
			a.deps.Errors.Status(w, r, http.StatusNotFound)
			return
		}
		if img, err = a.fetchImage(r.Context(), img); err != nil {
			if r.Context().Err() != nil {
				return // the viewer left; not the publisher's failure
			}
			a.deps.Errors.Status(w, r, http.StatusNotFound)
			return
		}
	}
	etag := `"` + img.Hash + `"`
	h := w.Header()
	h.Set("Cache-Control", imageCacheControl)
	h.Set("ETag", etag)
	h.Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", img.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(img.Bytes)))
	_, _ = w.Write(img.Bytes)
}

// fetchImage downloads one image under the concurrency bound and records
// the outcome either way. A canceled context records nothing: the viewer
// leaving says nothing about the publisher's image.
func (a *App) fetchImage(ctx context.Context, img Image) (Image, error) {
	select {
	case a.imgSem <- struct{}{}:
		defer func() { <-a.imgSem }()
	case <-ctx.Done():
		return Image{}, ctx.Err()
	}
	ct, body, err := a.client.GetImage(ctx, img.SrcURL, webfetch.MaxImageBytes)
	if err != nil {
		if ctx.Err() == nil {
			if serr := a.store.SaveImageFailure(ctx, img.Hash, err.Error()); serr != nil && !errors.Is(serr, ErrNotFound) {
				a.deps.Log.Error("later recording an image failure failed", "error", serr)
			}
			a.deps.Log.Info("later image fetch failed", "src", img.SrcURL, "error", err)
		}
		return Image{}, err
	}
	if err := a.store.SaveImageBytes(ctx, img.Hash, ct, body); err != nil {
		// ErrNotFound: the article was deleted mid-fetch, which is benign.
		if !errors.Is(err, ErrNotFound) {
			a.deps.Log.Error("later storing an image failed", "hash", img.Hash, "error", err)
		}
		return Image{}, err
	}
	img.ContentType, img.Bytes = ct, body
	return img, nil
}

// DownloadImages stores up to limit images no one has fetched yet. A failed
// image is recorded and skipped; only a store error listing them, or the
// context ending, stops the batch.
func (a *App) DownloadImages(ctx context.Context, limit int) (int, error) {
	imgs, err := a.store.ImagesToFetch(ctx, limit)
	if err != nil {
		return 0, err
	}
	stored := 0
	for _, img := range imgs {
		if ctx.Err() != nil {
			return stored, ctx.Err()
		}
		if _, err := a.fetchImage(ctx, img); err == nil {
			stored++
		}
	}
	return stored, nil
}
