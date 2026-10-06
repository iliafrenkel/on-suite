package later

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// Image is one stored (or still to be fetched) article image.
type Image struct {
	Hash, SrcURL, ContentType string
	Bytes                     []byte
	FetchedAt                 time.Time // last attempt; zero when never tried
	ErrorCount                int
	LastError                 string
}

// Cached reports whether the image bytes are stored.
func (i Image) Cached() bool { return i.Bytes != nil }

const imageColumns = `i.hash, i.src_url, i.content_type, i.bytes, i.fetched_at, i.error_count, i.last_error`

func scanImage(row rowScanner) (Image, error) {
	var img Image
	var fetched sql.NullString
	err := row.Scan(&img.Hash, &img.SrcURL, &img.ContentType, &img.Bytes, &fetched, &img.ErrorCount, &img.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return Image{}, ErrNotFound
	}
	if err != nil {
		return Image{}, fmt.Errorf("later: load image: %w", err)
	}
	if fetched.Valid {
		if img.FetchedAt, err = db.ParseTime(fetched.String); err != nil {
			return Image{}, err
		}
	}
	return img, nil
}

// ImageForUser loads an image only if one of userID's articles uses it, so
// one person's saved images are never served to another.
func (st *Store) ImageForUser(ctx context.Context, userID int64, hash string) (Image, error) {
	return scanImage(st.db.QueryRowContext(ctx, `
		SELECT `+imageColumns+`
		  FROM later_images i
		 WHERE i.hash = ?
		   AND EXISTS (SELECT 1 FROM later_article_images ai
		                 JOIN later_articles a ON a.id = ai.article_id
		                WHERE ai.hash = i.hash AND a.user_id = ?)`, hash, userID))
}

// SaveImageBytes stores a fetched image and clears its failure record.
func (st *Store) SaveImageBytes(ctx context.Context, hash, contentType string, b []byte) error {
	return st.exec(ctx, "save image bytes", `
		UPDATE later_images
		   SET bytes = ?, content_type = ?, fetched_at = ?, error_count = 0, last_error = ''
		 WHERE hash = ?`, b, contentType, db.FormatTime(st.now()), hash)
}

// SaveImageFailure records a failed fetch attempt.
func (st *Store) SaveImageFailure(ctx context.Context, hash, msg string) error {
	return st.exec(ctx, "save image failure", `
		UPDATE later_images
		   SET error_count = error_count + 1, last_error = ?, fetched_at = ?
		 WHERE hash = ?`, msg, db.FormatTime(st.now()), hash)
}

// ImagesToFetch returns up to limit images still without bytes that are
// neither out of attempts nor inside their retry backoff, oldest-saved
// article first.
func (st *Store) ImagesToFetch(ctx context.Context, limit int) ([]Image, error) {
	// Over-read: some rows are dropped below because they are backing off.
	rows, err := st.db.QueryContext(ctx, `
		SELECT `+imageColumns+`
		  FROM later_images i
		  JOIN (SELECT ai.hash AS hash, min(a.saved_at) AS first_saved
		          FROM later_article_images ai
		          JOIN later_articles a ON a.id = ai.article_id
		         GROUP BY ai.hash) s ON s.hash = i.hash
		 WHERE i.bytes IS NULL AND i.error_count < ?
		 ORDER BY s.first_saved, i.hash
		 LIMIT ?`, webfetch.MaxImageAttempts, limit*4)
	if err != nil {
		return nil, fmt.Errorf("later: images to fetch: %w", err)
	}
	defer func() { _ = rows.Close() }()
	now := st.now()
	var out []Image
	for rows.Next() && len(out) < limit {
		img, err := scanImage(rows)
		if err != nil {
			return nil, err
		}
		if !webfetch.GivenUp(img.ErrorCount, img.FetchedAt, now) {
			out = append(out, img)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("later: images to fetch: %w", err)
	}
	return out, nil
}

// ImageSources maps each of an article's image hashes to the URL it was
// downloaded from. Like Highlights it doesn't check ownership: callers load
// the article owner-scoped first.
func (st *Store) ImageSources(ctx context.Context, articleID int64) (map[string]string, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT i.hash, i.src_url FROM later_article_images ai
		  JOIN later_images i ON i.hash = ai.hash
		 WHERE ai.article_id = ?`, articleID)
	if err != nil {
		return nil, fmt.Errorf("later: image sources: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var hash, src string
		if err := rows.Scan(&hash, &src); err != nil {
			return nil, fmt.Errorf("later: scan image source: %w", err)
		}
		out[hash] = src
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("later: image sources: %w", err)
	}
	return out, nil
}
