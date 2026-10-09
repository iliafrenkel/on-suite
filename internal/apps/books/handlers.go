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
	a.renderPanes(w, r, uid, ctxFrom(r.FormValue), paneOpts{})
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
	a.renderPanes(w, r, uid, ctxFrom(r.FormValue), paneOpts{BookID: id})
}

// paneOpts is what a render shows besides list c: the open book (0: none)
// and a refusal — in the banner, or for a progress update inside the
// progress box, or for a note or quote inside its form, with what was
// typed (spec "Errors": an inline message).
type paneOpts struct {
	BookID        int64
	Banner        string
	ProgressError string
	ProgressInput string
	Draft         entryDraft
}

// renderPanes draws the panes for list c as opts says. A normal request
// gets the whole page — 422 when there is a refusal, so a refused form post
// without JavaScript isn't a 200. An htmx request gets the block for what
// it targeted, as Reader's renderPanes does (#453): #books-list → list-swap
// (the list and its out-of-band companions, the book pane untouched),
// #books-book → book-swap, #books-progress → progress-swap (the box, and
// the list out of band), #books-notes → notes-swap (the same for the
// notes), anything else (#books-panes) → the whole panes.
// Fragments are always 200: htmx's default responseHandling only swaps
// 2xx/3xx.
func (a *App) renderPanes(w http.ResponseWriter, r *http.Request, userID int64, c listCtx, opts paneOpts) {
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
	if opts.BookID != 0 {
		b, err := a.store.Get(ctx, userID, opts.BookID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		rs, err := a.store.Readings(ctx, userID, opts.BookID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		ns, err := a.store.Notes(ctx, userID, opts.BookID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		bv = viewBook(b, c, a.store.Today())
		bv.History = viewHistory(rs)
		bv.Notes, bv.NewNote = viewNotes(ns, opts.Draft), formFor("note-new", opts.Draft, entryForm{})
		title = b.Title
		if opts.ProgressError != "" {
			bv.Progress.Error, bv.Progress.Value = opts.ProgressError, opts.ProgressInput
		}
	}
	page := a.deps.Page(r, title)
	bv.Shell = page.Shell
	v := panesView{Title: page.Title, Shell: page.Shell, Ctx: c, Error: opts.Banner,
		Sidebar: viewSidebar(c, counts, tags), List: viewList(items, c, opts.BookID), Book: bv}

	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		block := "panes-oob"
		switch web.HTMXTarget(r) {
		case "books-list":
			block = "list-swap"
		case "books-book":
			block = "book-swap"
		case "books-progress":
			block = "progress-swap"
			v.List.OOB = true
		case "books-notes":
			block = "notes-swap"
			v.List.OOB = true
		}
		if err := a.deps.Render.Fragment(w, http.StatusOK, "books/index", block, v); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	status := http.StatusOK
	if opts.Banner != "" || opts.ProgressError != "" || opts.Draft.Error != "" {
		status = http.StatusUnprocessableEntity
	}
	page.Data = v
	if err := a.deps.Render.Page(w, status, "books/index", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
