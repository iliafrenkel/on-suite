package reader

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
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

// validImageHash reports whether the path segment could be one of our hashes.
// Checked before any database work so a probe costs nothing.
func validImageHash(s string) bool {
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

// image serves a proxied article image.
//
// The proxy takes a hash, never a URL. The only way a hash resolves is if a
// feed this server ingested contained exactly that image URL, so there is no
// input that makes this fetch something else — which is why it needs no
// signing secret. Fetching still goes through Client, so the SSRF guard, the
// redirect cap and the size cap all apply.
func (a *App) image(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	hash := r.PathValue("hash")
	if !validImageHash(hash) {
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
	// A source that has already failed is not retried on every page view.
	if img.ErrorCount > 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}

	fetched, err := a.fetchImage(r, img)
	if err != nil {
		a.deps.Log.Info("reader image fetch failed", "src", img.SrcURL, "error", err)
		if err := a.store.SaveImageFailure(r.Context(), hash, err.Error(), time.Now().UTC()); err != nil {
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

	res, err := a.client.Get(r.Context(), img.SrcURL, GetOptions{
		MaxBytes: MaxImageBytes,
		Accept:   "image/*",
	})
	if err != nil {
		return Image{}, err
	}

	// Sniff rather than trust: a publisher claiming image/png over an HTML
	// document is exactly how a proxy becomes an HTML-injection vector on its
	// own origin.
	ct := http.DetectContentType(res.Body)
	if !strings.HasPrefix(ct, "image/") {
		return Image{}, errors.New("reader: response is not an image (" + ct + ")")
	}

	if err := a.store.SaveImageBytes(r.Context(), img.Hash, ct, res.Body, time.Now().UTC()); err != nil {
		return Image{}, err
	}
	img.ContentType = ct
	img.Bytes = res.Body
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
