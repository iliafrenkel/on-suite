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

// tooLargeMessage is shown when a card form's whole body exceeds
// cardFormMaxBytes (#330) — the request is too large to have been read at
// all, as opposed to one field being individually oversized (readUpload's
// own per-field message below). It reuses web.TooLargeMessage, the exact
// copy web.Errors' own http.StatusRequestEntityTooLarge page shows, rather
// than a hand-copied duplicate, so the two can't drift and show the same
// wording regardless of which layer catches the oversized request — the
// platform's own CSRF/body-cap layer (internal/platform/web/csrf.go) or
// this app's own readCardUploads below.
const tooLargeMessage = web.TooLargeMessage

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

// pendingUpload is one checked, not-yet-saved media file from a card form.
type pendingUpload struct {
	Kind        string // MediaKindImage or MediaKindAudio
	ContentType string // sniffed, never the client's claim
	Data        []byte
}

// cardUploads is a card form's whole media part. It is read and checked
// before anything is written, so a bad file can never leave a
// half-updated card behind (UI overhaul spec §4).
type cardUploads struct {
	Image, Audio             *pendingUpload // nil = no new file for that kind
	RemoveImage, RemoveAudio bool
}

// readCardUploads parses a card form — multipart/form-data when it carries
// files, a plain urlencoded POST when it doesn't — and checks its optional
// "image"/"audio" parts and "remove_image"/"remove_audio" flags. It must be
// the first thing a handler does with the body. A non-empty string is a
// message for the person who submitted the form.
func readCardUploads(w http.ResponseWriter, r *http.Request) (cardUploads, string) {
	// ParseMultipartForm's argument is only a maxMemory hint, not a hard cap
	// on bytes read: without an outer limit it would read the entire body
	// (spilling to a temp file) before any size check below runs. The
	// budget covers one image plus one audio part in the same request, plus
	// cardFormMaxBytes's own allowance for the form's text fields and
	// multipart overhead (#330) — the same constant the route's own body
	// limit in flash.go's Mount uses, so the two stay in sync.
	r.Body = http.MaxBytesReader(w, r.Body, cardFormMaxBytes)
	// A text-only or remove-only form may arrive urlencoded; ErrNotMultipart
	// is expected then — ParseMultipartForm still fills r.PostForm.
	if err := r.ParseMultipartForm(MaxAudioFetchBytes); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return cardUploads{}, tooLargeMessage
		}
		return cardUploads{}, "That upload could not be read."
	}

	u := cardUploads{
		RemoveImage: r.PostFormValue("remove_image") != "",
		RemoveAudio: r.PostFormValue("remove_audio") != "",
	}
	// A newly chosen file always wins over Remove for its own kind (#328):
	// read every file part unconditionally, whether or not that kind's
	// Remove flag is set, so a new file is neither dropped nor lets a bad
	// file slip past validation just because Remove happened to be ticked.
	// saveCardUploads is what actually applies Remove only when no new file
	// came in for that kind.
	var msg string
	if u.Image, msg = readUpload(w, r, MediaKindImage, "image", MaxImageFetchBytes); msg != "" {
		return cardUploads{}, msg
	}
	if u.Audio, msg = readUpload(w, r, MediaKindAudio, "audio", MaxAudioFetchBytes); msg != "" {
		return cardUploads{}, msg
	}
	return u, ""
}

// readUpload reads and checks one optional file part. No part at all (or
// an empty one — a file input left blank) is not an error: it returns nil
// and "".
func readUpload(w http.ResponseWriter, r *http.Request, kind, field string, maxBytes int64) (*pendingUpload, string) {
	file, header, err := r.FormFile(field)
	if err != nil {
		// http.ErrMissingFile (no part with this name — a file input left
		// untouched) and http.ErrNotMultipart (a text-only or remove-only
		// submission, which readCardUploads already tolerates on
		// ParseMultipartForm) both mean "no file", not a problem worth
		// reporting. Any other error — the part's underlying temp file
		// could not be opened, say — is a real read failure and must not
		// be treated the same as "nothing was submitted" (#302.2).
		if errors.Is(err, http.ErrMissingFile) || errors.Is(err, http.ErrNotMultipart) {
			return nil, ""
		}
		return nil, "That upload could not be read."
	}
	defer func() { _ = file.Close() }()
	if header.Size == 0 {
		return nil, ""
	}

	tooBig := "That file is larger than the " + strconv.FormatInt(maxBytes>>20, 10) + "MB limit."
	if header.Size > maxBytes {
		return nil, tooBig
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, file, maxBytes))
	if err != nil {
		return nil, tooBig
	}
	ct := http.DetectContentType(data)
	if !contentTypeMatchesKind(ct, kind) {
		return nil, "That file does not look like " + kind + " content."
	}
	return &pendingUpload{Kind: kind, ContentType: ct, Data: data}, ""
}

// saveCardUploads applies checked uploads to a card that now exists: a new
// file of a kind always wins over that kind's Remove flag (#328) — Remove
// only takes effect when no new file came in for that kind.
func (a *App) saveCardUploads(ctx context.Context, userID, deckID, cardID int64, u cardUploads) error {
	for _, m := range []struct {
		kind   string
		remove bool
		up     *pendingUpload
	}{
		{MediaKindImage, u.RemoveImage, u.Image},
		{MediaKindAudio, u.RemoveAudio, u.Audio},
	} {
		switch {
		case m.up != nil:
			hash, err := a.store.SaveMediaUpload(ctx, m.up.Kind, m.up.ContentType, m.up.Data, a.store.now())
			if err != nil {
				return err
			}
			if err := a.store.SetCardMedia(ctx, userID, deckID, cardID, m.kind, &hash); err != nil {
				return err
			}
		case m.remove:
			if err := a.store.SetCardMedia(ctx, userID, deckID, cardID, m.kind, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
