// internal/apps/flash/handlers_media.go
package flash

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// mediaCacheControl is private because the response is only meaningful to a
// signed-in user, and long because the URL is content-addressed: a different
// source produces a different hash and therefore a different path.
const mediaCacheControl = "private, max-age=86400"

// maxMediaFetchAttempts and mediaRetryBackoff mirror
// internal/apps/reader's own maxImageFetchAttempts/imageRetryBackoff: give up
// permanently after 3 consecutive failures, otherwise wait an hour between
// attempts, since this app has no external signal to know sooner when a
// transient failure has cleared.
const (
	maxMediaFetchAttempts = 3
	mediaRetryBackoff     = 1 * time.Hour
)

// media serves a card's image or audio clip, fetching and caching it on
// first request if it hasn't been fetched yet. The route takes a hash, never
// a URL — the only way a hash resolves is if ImportDeck or an upload
// recorded it, so there is no input here that makes this fetch something
// else.
func (a *App) media(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	hash := r.PathValue("hash")
	if !validMediaHash(hash) {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}

	m, err := a.store.MediaByHash(r.Context(), hash)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if m.Cached() {
		a.writeMedia(w, r, m)
		return
	}
	if m.SourceURL == "" {
		// A row with no bytes and no source URL is a data-integrity
		// impossibility given how rows are created (EnsureMediaURL always
		// sets source_url; SaveMediaUpload always sets bytes) — there is
		// nothing to fetch either way, so treat it the same as not found.
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	// Give up permanently past the attempt cap, and otherwise still refuse
	// immediately inside the backoff window — a failure older than the
	// backoff window, under the cap, falls through to a real retry.
	if m.ErrorCount >= maxMediaFetchAttempts ||
		(m.ErrorCount > 0 && time.Since(m.FetchedAt) < mediaRetryBackoff) {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}

	fetched, err := a.fetchMedia(r, m)
	if err != nil {
		// A canceled request context means the viewer navigated away
		// mid-fetch — user-navigation noise, not a real failure, and must
		// not count toward the retry budget or reset the backoff clock.
		if errors.Is(err, context.Canceled) || r.Context().Err() != nil {
			a.deps.Log.Info("flash media fetch canceled", "src", m.SourceURL, "error", err)
			return
		}
		a.deps.Log.Info("flash media fetch failed", "src", m.SourceURL, "error", err)
		if err := a.store.SaveMediaFailure(r.Context(), hash, err.Error(), a.store.now()); err != nil {
			a.deps.Log.Error("flash recording a media failure failed", "error", err)
		}
		// 404 rather than 502: the browser shows its native broken-media
		// state, which is the right outcome for something that will not
		// load.
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return
	}
	a.writeMedia(w, r, fetched)
}

// fetchMedia retrieves and caches one media item, bounded by mediaSem so a
// deck full of images can't open many simultaneous connections to one host.
func (a *App) fetchMedia(r *http.Request, m Media) (Media, error) {
	select {
	case a.mediaSem <- struct{}{}:
		defer func() { <-a.mediaSem }()
	case <-r.Context().Done():
		return Media{}, r.Context().Err()
	}
	return a.store.FetchAndCacheMedia(r.Context(), a.mediaClient, m, a.store.now())
}

func (a *App) writeMedia(w http.ResponseWriter, r *http.Request, m Media) {
	etag := `"` + m.Hash + `"`
	h := w.Header()
	h.Set("Cache-Control", mediaCacheControl)
	h.Set("ETag", etag)
	h.Set("X-Content-Type-Options", "nosniff")

	// The validator check comes before Content-Type and Content-Length: a
	// 304 carries neither, and setting them anyway is the kind of small
	// protocol wrongness proxies and caches punish unpredictably.
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", m.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(m.Bytes)))
	if _, err := w.Write(m.Bytes); err != nil {
		a.deps.Log.Info("flash writing media failed", "error", err)
	}
}

// uploadCardMedia attaches or removes one of userID's own card's media
// attachments, via a multipart form with optional "image"/"audio" file
// parts and optional "remove_image"/"remove_audio" flags. A validation
// failure (oversized file, wrong content type) re-renders the card at 400
// with the error shown, the same pattern every other flash form uses —
// not a generic error page, since the user is watching this happen.
func (a *App) uploadCardMedia(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deck, ok := a.cardDeck(w, r, userID)
	if !ok {
		return
	}
	cardID, ok := a.cardIDFromPath(w, r)
	if !ok {
		return
	}
	c, err := a.store.CardByID(r.Context(), userID, deck.ID, cardID)
	if err != nil {
		a.fail(w, r, err)
		return
	}

	// ParseMultipartForm's argument is only a maxMemory hint, not a hard
	// cap on bytes read: without an outer limit it will read the entire
	// body (spilling oversized parts to a temp file) before any
	// application-level size check below ever runs. Wrapping r.Body in
	// MaxBytesReader first makes the read itself abort partway through an
	// oversized body. The budget covers one image part plus one audio
	// part arriving in the same request, plus overhead for multipart
	// boundaries/headers — not just the larger of the two alone.
	r.Body = http.MaxBytesReader(w, r.Body, MaxImageFetchBytes+MaxAudioFetchBytes)

	// A remove-only request (no file attached) may arrive as a plain
	// form-urlencoded POST rather than multipart/form-data — ErrNotMultipart
	// is expected there, not a failure: ParseMultipartForm still populates
	// r.Form/r.PostForm via its own internal ParseForm call before returning
	// it, so PostFormValue below works either way.
	if err := r.ParseMultipartForm(MaxAudioFetchBytes); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest,
			a.viewCardDetailWithMediaError(r, userID, deck, c, "That upload could not be read."))
		return
	}

	for _, spec := range []struct {
		kind, field, remove string
		maxBytes            int64
	}{
		{MediaKindImage, "image", "remove_image", MaxImageFetchBytes},
		{MediaKindAudio, "audio", "remove_audio", MaxAudioFetchBytes},
	} {
		if r.PostFormValue(spec.remove) != "" {
			if err := a.store.SetCardMedia(r.Context(), userID, deck.ID, cardID, spec.kind, nil); err != nil {
				a.fail(w, r, err)
				return
			}
			continue
		}
		if errMsg := a.attachUpload(w, r, userID, deck, cardID, spec.kind, spec.field, spec.maxBytes); errMsg != "" {
			a.renderCardIndex(w, r, userID, deck, http.StatusBadRequest,
				a.viewCardDetailWithMediaError(r, userID, deck, c, errMsg))
			return
		}
	}

	updated, err := a.store.CardByID(r.Context(), userID, deck.ID, cardID)
	if err != nil {
		a.fail(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, cardBasePath(deck.ID)+strconv.FormatInt(cardID, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", cardBasePath(deck.ID)+strconv.FormatInt(cardID, 10))
	a.renderCardDetailWithList(w, r, userID, deck, http.StatusOK, a.viewCardDetail(r, userID, deck, updated))
}

// attachUpload reads one optional file part named field ("image" or
// "audio"), and if present, stores it and attaches it to the card. It
// returns a non-empty user-facing message if the part is present but
// invalid (oversized, wrong content type, or a store failure); no part
// present is not an error — the request may only be touching the other
// media kind, or removing one — so it returns "".
func (a *App) attachUpload(w http.ResponseWriter, r *http.Request, userID int64, deck Deck, cardID int64, kind, field string, maxBytes int64) string {
	file, header, err := r.FormFile(field)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()

	if header.Size > maxBytes {
		return "That file is larger than the " + strconv.FormatInt(maxBytes>>20, 10) + "MB limit."
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, file, maxBytes))
	if err != nil {
		return "That file is larger than the " + strconv.FormatInt(maxBytes>>20, 10) + "MB limit."
	}

	ct := http.DetectContentType(data)
	if !contentTypeMatchesKind(ct, kind) {
		return "That file does not look like " + kind + " content."
	}

	hash, err := a.store.SaveMediaUpload(r.Context(), kind, ct, data, a.store.now())
	if err != nil {
		return userMessage(err)
	}
	if err := a.store.SetCardMedia(r.Context(), userID, deck.ID, cardID, kind, &hash); err != nil {
		return userMessage(err)
	}
	return ""
}
