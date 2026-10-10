package books

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// The cover backfill (spec "Covers after import"), decided 2026-10-10
// while planning B5: Open Library allows 100 cover lookups by ISBN per
// five minutes from one address, so a run every five minutes takes 25
// books, a second apart — about 300 an hour, well inside the limit even
// with Find cover clicked meanwhile.
const (
	coverBackfillEvery = 5 * time.Minute
	coverBackfillBatch = 25
	coverBackfillPause = time.Second
	// A run gives up after this many failed lookups in a row: Open
	// Library is probably down, so the rest wait for the next run.
	coverBackfillMaxFailures = 3
)

// ErrNoCover is Open Library having no cover for a book: a 404, or an
// answer that isn't a cover image.
var ErrNoCover = errors.New("books: open library has no cover")

// CoverByISBN fetches the medium cover Open Library has for an ISBN-13.
// default=false makes a missing cover a 404, which is ErrNoCover; any
// other failure (a timeout, a 5xx) is an error worth trying again.
func (o *OpenLibrary) CoverByISBN(ctx context.Context, isbn string) (string, []byte, error) {
	if v, ok := ISBN13(isbn); !ok || v != isbn {
		return "", nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, coverTimeout)
	defer cancel()
	res, err := o.Web.Get(ctx, fmt.Sprintf("%s/b/isbn/%s-M.jpg?default=false", o.Covers, isbn),
		webfetch.GetOptions{Accept: "image/*", MaxBytes: MaxCoverBytes})
	if res != nil && res.Status == http.StatusNotFound {
		return "", nil, ErrNoCover
	}
	if err != nil {
		return "", nil, err
	}
	ct := http.DetectContentType(res.Body)
	if !coverType(ct) {
		return "", nil, ErrNoCover
	}
	return ct, res.Body, nil
}

// CoverCandidate is a book the backfill should look for a cover for.
type CoverCandidate struct {
	UserID, ID int64
	ISBN       string
}

// CoverCandidates is up to limit books, any user's, with an ISBN, no
// cover, and no look for one yet — oldest first.
func (st *Store) CoverCandidates(ctx context.Context, limit int) ([]CoverCandidate, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT b.user_id, b.id, b.isbn13 FROM books_books b
		 WHERE b.isbn13 IS NOT NULL AND b.cover_checked_at IS NULL
		   AND NOT EXISTS (SELECT 1 FROM books_covers c WHERE c.book_id = b.id)
		 ORDER BY b.id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("books: cover candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []CoverCandidate
	for rows.Next() {
		var c CoverCandidate
		if err := rows.Scan(&c.UserID, &c.ID, &c.ISBN); err != nil {
			return nil, fmt.Errorf("books: scan cover candidate: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: cover candidates: %w", err)
	}
	return out, nil
}

// FillCover stores a cover the backfill found, unless the book has one by
// now (an upload wins). It leaves updated_at alone: a background fetch
// isn't a change the person made, so it doesn't reorder their shelves.
func (st *Store) FillCover(ctx context.Context, userID, id int64, contentType string, data []byte) error {
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO books_covers (book_id, content_type, bytes, source, fetched_at)
		SELECT id, ?, ?, 'ol', ? FROM books_books WHERE id = ? AND user_id = ?
		ON CONFLICT (book_id) DO NOTHING`,
		contentType, data, formatTime(st.now()), id, userID); err != nil {
		return fmt.Errorf("books: fill cover: %w", err)
	}
	return nil
}

// MarkCoverChecked records that Open Library had no cover for a book, so
// the backfill leaves it alone.
func (st *Store) MarkCoverChecked(ctx context.Context, userID, id int64) error {
	if _, err := st.db.ExecContext(ctx, `UPDATE books_books SET cover_checked_at = ? WHERE id = ? AND user_id = ?`,
		formatTime(st.now()), id, userID); err != nil {
		return fmt.Errorf("books: mark cover checked: %w", err)
	}
	return nil
}

// BackfillCovers looks for up to batch missing covers by ISBN, pausing
// between requests. A miss marks the book checked. Any other failure is
// logged and the run moves on, leaving that book unmarked for a later run,
// so one bad book can't hold up the rest; after coverBackfillMaxFailures in
// a row the run stops early. A success or a miss resets the count. It
// returns how many covers it stored, and the failures joined as its error.
func (a *App) BackfillCovers(ctx context.Context, batch int) (int, error) {
	cs, err := a.store.CoverCandidates(ctx, batch)
	if err != nil {
		return 0, err
	}
	stored, inARow := 0, 0
	var errs []error
	for i, c := range cs {
		if i > 0 {
			if err := a.pause(ctx, coverBackfillPause); err != nil {
				return stored, errors.Join(append(errs, err)...)
			}
		}
		ct, data, err := a.ol.CoverByISBN(ctx, c.ISBN)
		switch {
		case errors.Is(err, ErrNoCover) || errors.Is(err, ErrInvalid):
			err = a.store.MarkCoverChecked(ctx, c.UserID, c.ID)
		case err == nil:
			if err = a.store.FillCover(ctx, c.UserID, c.ID, ct, data); err == nil {
				stored++
			}
		default:
			if ctx.Err() != nil {
				return stored, errors.Join(append(errs, err)...)
			}
			a.deps.Log.Info("books cover backfill failed", "book", c.ID, "error", err)
			errs = append(errs, fmt.Errorf("book %d: %w", c.ID, err))
			if inARow++; inARow >= coverBackfillMaxFailures {
				return stored, errors.Join(errs...)
			}
			continue
		}
		if err != nil { // the database, not Open Library
			return stored, errors.Join(append(errs, err)...)
		}
		inARow = 0
	}
	return stored, errors.Join(errs...)
}

// sleep waits d, or until ctx is done: the backfill's pause between
// requests. Tests replace it (App.pause).
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Jobs implements app.Scheduler: the cover backfill shows in /admin/jobs
// with Run now. RegisterJobs runs after Mount, and the closure reads the
// app only when it runs.
func (a *App) Jobs(app.Deps) []app.Job {
	return []app.Job{{
		Name:        "fetch book covers",
		Description: "Looks up covers on Open Library for books with an ISBN and no cover, a few at a time — books imported from Goodreads, say.",
		Every:       coverBackfillEvery,
		Run: func(ctx context.Context) error {
			n, err := a.BackfillCovers(ctx, coverBackfillBatch)
			if n > 0 {
				a.deps.Log.Info("books stored covers", "count", n)
			}
			return err
		},
	}}
}
