package books

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// FindCoverID asks Open Library for a book by title and its first author
// and returns the cover id of the first match that has one; ErrNoCover
// when none does. It is Find cover's way in for a book with no ISBN. A
// search by field is slower than the Add page's, so it gets an image
// fetch's time (the trial run saw a two-author search take 9s).
func (o *OpenLibrary) FindCoverID(ctx context.Context, title, authors string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, coverTimeout)
	defer cancel()
	v := url.Values{"title": {title}, "fields": {"cover_i"}, "limit": {"5"}}
	if first, _, _ := strings.Cut(authors, ","); strings.TrimSpace(first) != "" {
		v.Set("author", strings.TrimSpace(first))
	}
	res, err := o.Web.Get(ctx, o.Base+"/search.json?"+v.Encode(),
		webfetch.GetOptions{Accept: "application/json", MaxBytes: maxJSONBytes})
	if err != nil {
		return 0, fmt.Errorf("books: open library search: %w", err)
	}
	var body struct {
		Docs []olDoc `json:"docs"`
	}
	if err := json.Unmarshal(res.Body, &body); err != nil {
		return 0, fmt.Errorf("books: open library search: %w", err)
	}
	for _, d := range body.Docs {
		if d.CoverI > 0 {
			return d.CoverI, nil
		}
	}
	return 0, ErrNoCover
}

// lookUpCover is Find cover's lookup (spec "Covers after import"): the
// backfill's, by ISBN, then — when there is no ISBN, or Open Library has
// no cover for it — a search by title and author.
func (a *App) lookUpCover(ctx context.Context, b Book) (string, []byte, error) {
	if b.ISBN != "" {
		ct, data, err := a.ol.CoverByISBN(ctx, b.ISBN)
		if !errors.Is(err, ErrNoCover) {
			return ct, data, err
		}
	}
	coverID, err := a.ol.FindCoverID(ctx, b.Title, b.Authors)
	if err != nil {
		return "", nil, err
	}
	ct, data, err := a.ol.Cover(ctx, coverID, "M")
	if err == nil && !coverType(ct) {
		err = ErrNoCover
	}
	return ct, data, err
}

// findCover is the Edit page's Find cover (UI redesign spec "Add and Edit
// pages"): the lookup, now. Found, it is back to the Edit page, which
// says so; finding none, or Open Library not answering, is the Edit page
// again with the reason by the cover, as a 422.
func (a *App) findCover(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	b, err := a.store.Get(ctx, uid, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	c := ctxFrom(r.PostFormValue)
	ct, data, err := a.lookUpCover(ctx, b)
	var msg string
	switch {
	case errors.Is(err, ErrNoCover):
		if err := a.store.MarkCoverChecked(ctx, uid, id); err != nil {
			a.fail(w, r, err)
			return
		}
		msg = "Open Library has no cover for this book. You can upload one, or paste the address of one."
	case err != nil:
		a.deps.Log.Info("books find cover failed", "book", id, "error", err)
		msg = "Open Library didn't answer. Try again in a minute."
	}
	if msg != "" {
		v := editBookForm(id, valuesOf(b.BookInput), nil, c, coverURL(b.ID, b.CoverVersion))
		v.CoverNotice = msg
		a.renderForm(w, r, http.StatusUnprocessableEntity, v)
		return
	}
	if err := a.store.SetCover(ctx, uid, id, ct, data, CoverFromOL); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, c.EditURL(id)+"&found=1", http.StatusSeeOther)
}
