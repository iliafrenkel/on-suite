package books

import (
	"context"
	"errors"
	"net/http"
	"strconv"
)

// coverCacheControl: private (signed-in only) and long, because the URL
// carries the cover's version — a new cover is a new URL.
const coverCacheControl = "private, max-age=31536000, immutable"

// thumbCacheControl: Open Library's covers don't change, but these are
// throwaway search thumbnails; a day is plenty.
const thumbCacheControl = "private, max-age=86400"

// cover serves one of the viewer's books' stored cover.
func (a *App) cover(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	c, err := a.store.Cover(r.Context(), uid, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	etag := `"` + c.Version + `"`
	h := w.Header()
	h.Set("Cache-Control", coverCacheControl)
	h.Set("ETag", etag)
	h.Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", c.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(c.Bytes)))
	_, _ = w.Write(c.Bytes)
}

// olThumb proxies a small Open Library cover for the search results, so
// the suite's img-src stays 'self' (Open Library's cover URLs redirect to
// archive.org storage hosts, which a CSP would have to allow by wildcard).
// It takes a cover id — digits only — never a URL, so nothing here can be
// made to fetch anything but an Open Library cover.
func (a *App) olThumb(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 || len(raw) > 12 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	select {
	case a.thumbSem <- struct{}{}:
		defer func() { <-a.thumbSem }()
	case <-r.Context().Done():
		return
	}
	ct, data, err := a.ol.Cover(r.Context(), id, "S")
	if err != nil || !coverType(ct) {
		if r.Context().Err() == nil {
			a.deps.Log.Info("books thumbnail fetch failed", "cover", id, "type", ct, "error", err)
		}
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", thumbCacheControl)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Type", ct)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// errNotACover is an image that isn't one of the cover types.
var errNotACover = errors.New("books: not a JPEG, PNG, GIF or WebP image")

// saveOLCover fetches a new book's Open Library cover and stores it. A
// failure is logged, not shown: the book is already saved and shows its
// spine (spec "Errors").
func (a *App) saveOLCover(ctx context.Context, userID, id, coverID int64) {
	ct, data, err := a.ol.Cover(ctx, coverID, "M")
	if err == nil && !coverType(ct) {
		err = errNotACover
	}
	if err == nil {
		err = a.store.SetCover(ctx, userID, id, ct, data, CoverFromOL)
	}
	if err != nil {
		a.deps.Log.Info("books cover fetch failed", "book", id, "cover", coverID, "error", err)
	}
}
