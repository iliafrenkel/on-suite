package reader

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// imageFetchConcurrency bounds outbound image fetches across all requests.
//
// An article with thirty images produces thirty near-simultaneous proxy
// requests on first view. Without a bound this app would open thirty
// connections to one publisher at once, which is both rude and a good way to
// get rate-limited.
const imageFetchConcurrency = 4

// imageCacheControl is private because the response is only meaningful to a
// signed-in user, and long because the URL is content-addressed: a different
// source URL is a different path.
const imageCacheControl = "private, max-age=86400"

// Reader's names for webfetch's retry rule, kept so call sites and
// export_test.go read as before.
const (
	maxImageFetchAttempts = webfetch.MaxImageAttempts
	imageRetryBackoff     = webfetch.ImageRetryBackoff
)

// image serves a proxied article image.
//
// The proxy takes a hash, never a URL. The only way a hash resolves is if it
// names an image URL this server saw — either in a feed body it ingested, or
// on a page it extracted a full article from, which R4 widened this to. So
// there is still no input that makes this fetch something else, which is why
// it needs no signing secret. Fetching still goes through Client, so the SSRF
// guard, the redirect cap and the size cap all apply.
func (a *App) image(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	hash := r.PathValue("hash")
	if !webfetch.ValidURLHash(hash) {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}

	img, err := a.store.ImageByHash(r.Context(), hash)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if img.Cached() {
		a.writeImage(w, r, img)
		return
	}
	// Give up permanently past the attempt cap, and otherwise still refuse
	// immediately inside the backoff window — but a failure older than the
	// backoff window, under the cap, falls through to a real retry. Nothing
	// but a successful SaveImageBytes ever clears error_count, so without
	// this an image that failed once from a DNS blip would 404 for the
	// household forever.
	if webfetch.GivenUp(img.ErrorCount, img.FetchedAt, a.store.now()) {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}

	fetched, err := a.fetchImage(r, img)
	if err != nil {
		// A canceled request context means the viewer navigated away or
		// scrolled past a lazy-loaded image mid-fetch — that is
		// user-navigation noise, not a publisher or network failure, and
		// must not count toward the retry budget or reset the backoff clock.
		if errors.Is(err, context.Canceled) || r.Context().Err() != nil {
			a.deps.Log.Info("reader image fetch canceled", "src", img.SrcURL, "error", err)
			return
		}
		a.deps.Log.Info("reader image fetch failed", "src", img.SrcURL, "error", err)
		if err := a.store.SaveImageFailure(r.Context(), hash, err.Error(), a.store.now()); err != nil {
			a.deps.Log.Error("reader recording an image failure failed", "error", err)
		}
		// 404 rather than 502: the browser shows the alt text, which is the
		// right outcome for a picture that will not load.
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	a.writeImage(w, r, fetched)
}

// fetchImage retrieves and caches one image.
func (a *App) fetchImage(r *http.Request, img Image) (Image, error) {
	select {
	case a.imgSem <- struct{}{}:
		defer func() { <-a.imgSem }()
	case <-r.Context().Done():
		return Image{}, r.Context().Err()
	}

	ct, body, err := a.client.GetImage(r.Context(), img.SrcURL, webfetch.MaxImageBytes)
	if err != nil {
		return Image{}, err
	}

	if err := a.store.SaveImageBytes(r.Context(), img.Hash, ct, body, a.store.now()); err != nil {
		return Image{}, err
	}
	img.ContentType = ct
	img.Bytes = body
	return img, nil
}

func (a *App) writeImage(w http.ResponseWriter, r *http.Request, img Image) {
	etag := `"` + img.Hash + `"`
	h := w.Header()
	h.Set("Cache-Control", imageCacheControl)
	h.Set("ETag", etag)
	h.Set("X-Content-Type-Options", "nosniff")

	// The validator check comes before Content-Type and Content-Length: a 304
	// carries neither, and setting them anyway is the kind of small protocol
	// wrongness proxies and caches punish unpredictably.
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", img.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(img.Bytes)))
	if _, err := w.Write(img.Bytes); err != nil {
		a.deps.Log.Info("reader writing an image failed", "error", err)
	}
}
