package later

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// Favicon is one stored (or still to be fetched) site icon.
type Favicon struct {
	Hash, SrcURL, ContentType string
	Bytes                     []byte
	FetchedAt                 time.Time // last attempt; zero when never tried
	ErrorCount                int
}

// Cached reports whether the icon bytes are stored.
func (f Favicon) Cached() bool { return f.Bytes != nil }

// FaviconForUser loads an icon only if one of userID's articles is on a site
// that uses it, so nobody can probe for icons they never saved.
func (st *Store) FaviconForUser(ctx context.Context, userID int64, hash string) (Favicon, error) {
	var f Favicon
	var fetched sql.NullString
	err := st.db.QueryRowContext(ctx, `
		SELECT f.hash, f.src_url, f.content_type, f.bytes, f.fetched_at, f.error_count
		  FROM later_favicons f
		 WHERE f.hash = ?
		   AND EXISTS (SELECT 1 FROM later_site_favicons sf
		                 JOIN later_articles a ON a.site_host = sf.site_host
		                WHERE sf.hash = f.hash AND a.user_id = ?)`, hash, userID).
		Scan(&f.Hash, &f.SrcURL, &f.ContentType, &f.Bytes, &fetched, &f.ErrorCount)
	if errors.Is(err, sql.ErrNoRows) {
		return Favicon{}, ErrNotFound
	}
	if err != nil {
		return Favicon{}, fmt.Errorf("later: load favicon: %w", err)
	}
	if fetched.Valid {
		if f.FetchedAt, err = db.ParseTime(fetched.String); err != nil {
			return Favicon{}, err
		}
	}
	return f, nil
}

// SaveFaviconBytes stores a fetched icon and clears its failure record.
func (st *Store) SaveFaviconBytes(ctx context.Context, hash, contentType string, b []byte) error {
	return st.exec(ctx, "save favicon bytes", `
		UPDATE later_favicons
		   SET bytes = ?, content_type = ?, fetched_at = ?, error_count = 0, last_error = ''
		 WHERE hash = ?`, b, contentType, db.FormatTime(st.now()), hash)
}

// SaveFaviconFailure records a failed fetch attempt.
func (st *Store) SaveFaviconFailure(ctx context.Context, hash, msg string) error {
	return st.exec(ctx, "save favicon failure", `
		UPDATE later_favicons
		   SET error_count = error_count + 1, last_error = ?, fetched_at = ?
		 WHERE hash = ?`, msg, db.FormatTime(st.now()), hash)
}

// bareNotFound answers a missing icon with a status only. The list asks for
// every row's icon on every view, so a full error page per dead icon would
// be waste; the cache header stops the browser asking again for an hour.
func bareNotFound(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, max-age="+strconv.Itoa(int(webfetch.ImageRetryBackoff.Seconds())))
	w.WriteHeader(http.StatusNotFound)
}

// favicon serves a stored site icon, fetching it first if it hasn't been
// yet. Like image, it takes a hash, never a URL, and only the viewer's own
// sites' icons resolve.
func (a *App) favicon(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	hash := r.PathValue("hash")
	if !webfetch.ValidURLHash(hash) {
		bareNotFound(w)
		return
	}
	fav, err := a.store.FaviconForUser(r.Context(), userID, hash)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			bareNotFound(w)
			return
		}
		a.fail(w, r, err)
		return
	}
	if !fav.Cached() {
		if webfetch.GivenUp(fav.ErrorCount, fav.FetchedAt, a.store.now()) {
			bareNotFound(w)
			return
		}
		if fav, err = a.fetchFavicon(r.Context(), fav); err != nil {
			if r.Context().Err() != nil {
				return // the viewer left; not the publisher's failure
			}
			bareNotFound(w)
			return
		}
	}
	etag := `"` + fav.Hash + `"`
	h := w.Header()
	h.Set("Cache-Control", imageCacheControl)
	h.Set("ETag", etag)
	h.Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", fav.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(fav.Bytes)))
	_, _ = w.Write(fav.Bytes)
}

// fetchFavicon downloads one icon under the shared concurrency bound and
// records the outcome either way; a canceled context records nothing.
func (a *App) fetchFavicon(ctx context.Context, fav Favicon) (Favicon, error) {
	select {
	case a.imgSem <- struct{}{}:
		defer func() { <-a.imgSem }()
	case <-ctx.Done():
		return Favicon{}, ctx.Err()
	}
	ct, body, err := a.client.GetImage(ctx, fav.SrcURL, webfetch.MaxFaviconBytes)
	if err != nil {
		if ctx.Err() == nil {
			if serr := a.store.SaveFaviconFailure(ctx, fav.Hash, err.Error()); serr != nil && !errors.Is(serr, ErrNotFound) {
				a.deps.Log.Error("later recording a favicon failure failed", "error", serr)
			}
			a.deps.Log.Info("later favicon fetch failed", "src", fav.SrcURL, "error", err)
		}
		return Favicon{}, err
	}
	if err := a.store.SaveFaviconBytes(ctx, fav.Hash, ct, body); err != nil {
		if !errors.Is(err, ErrNotFound) {
			a.deps.Log.Error("later storing a favicon failed", "hash", fav.Hash, "error", err)
		}
		return Favicon{}, err
	}
	fav.ContentType, fav.Bytes = ct, body
	return fav, nil
}
