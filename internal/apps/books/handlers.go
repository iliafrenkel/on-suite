package books

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// userID is the signed-in user; every route is registered with HandleFunc.
func (a *App) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u, ok := web.UserFrom(r.Context())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusUnauthorized)
		return 0, false
	}
	return u.ID, true
}

// pathID parses the {id} path segment; anything but a positive integer is a
// 404, the same as a book that isn't there.
func (a *App) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// fail maps a store error to its response: someone else's row is a 404,
// bad input a 400, anything else a logged 500.
func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		a.deps.Errors.Status(w, r, http.StatusNotFound)
	case errors.Is(err, ErrInvalid):
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
	default:
		a.deps.Errors.Internal(w, r, err)
	}
}

// index is a list with no book open.
func (a *App) index(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	a.renderPanes(w, r, uid, ctxFrom(r.FormValue), 0, "")
}

// book is a list with one book open.
func (a *App) book(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	a.renderPanes(w, r, uid, ctxFrom(r.FormValue), id, "")
}

// renderPanes draws the panes for list c with book bookID (0: none) open
// and errMsg (if any) in the banner. A normal request gets the whole page —
// 422 when there is an error, so a refused form post without JavaScript
// isn't a 200. An htmx request gets the block for what it targeted, as
// Reader's renderPanes does (#453): #books-list → list-swap (the list and
// its out-of-band companions, the book pane untouched), #books-book →
// book-swap, anything else (#books-panes) → the whole panes. Fragments are
// always 200: htmx's default responseHandling only swaps 2xx/3xx.
func (a *App) renderPanes(w http.ResponseWriter, r *http.Request, userID int64, c listCtx, bookID int64, errMsg string) {
	ctx := r.Context()
	counts, err := a.store.ShelfCounts(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	tags, err := a.store.TagNames(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	items, err := a.store.List(ctx, userID, ListQuery(c)) // same fields, by design
	if err != nil {
		a.fail(w, r, err)
		return
	}
	title := listHeading(c)
	var bv bookView
	if bookID != 0 {
		b, err := a.store.Get(ctx, userID, bookID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		bv = viewBook(b, c, a.store.Today())
		title = b.Title
	}
	page := a.deps.Page(r, title)
	bv.Shell = page.Shell
	v := panesView{Title: page.Title, Shell: page.Shell, Ctx: c, Error: errMsg,
		Sidebar: viewSidebar(c, counts, tags), List: viewList(items, c, bookID), Book: bv}

	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		block := "panes-oob"
		switch web.HTMXTarget(r) {
		case "books-list":
			block = "list-swap"
		case "books-book":
			block = "book-swap"
		}
		if err := a.deps.Render.Fragment(w, http.StatusOK, "books/index", block, v); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	status := http.StatusOK
	if errMsg != "" {
		status = http.StatusUnprocessableEntity
	}
	page.Data = v
	if err := a.deps.Render.Page(w, status, "books/index", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
