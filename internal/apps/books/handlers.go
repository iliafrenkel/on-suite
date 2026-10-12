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

// index is the home page (UI redesign spec "Home page").
func (a *App) index(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	a.renderHome(w, r, uid, ctxFrom(r.FormValue))
}

// book is a book's page, opened from list c (UI redesign spec "Book
// details page").
func (a *App) book(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	a.renderBook(w, r, uid, id, ctxFrom(r.FormValue), bookOpts{})
}

// renderHome draws the home page for list c (UI redesign spec "Home
// page"). An htmx request aimed at #books-list — a shelf, tag or series
// link, the search box, the sort and view controls — gets list-swap: the
// list, with the sidebar, the toolbar's heading and controls, the search's
// hidden fields and the drawer's checkbox out of band. Anything else is
// the whole page.
func (a *App) renderHome(w http.ResponseWriter, r *http.Request, userID int64, c listCtx) {
	ctx := r.Context()
	counts, err := a.store.ShelfCounts(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	tagNames, err := a.store.TagNames(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	prefs, err := a.store.Prefs(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	items, err := a.store.List(ctx, userID, ListQuery{Shelf: c.Shelf, Tag: c.Tag, Q: c.Q, Series: c.Series,
		Sort: prefs.Sort, Reversed: prefs.Reversed})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	byBook, err := a.store.TagsByBook(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	goal, err := a.sidebarGoal(r, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	page := a.deps.Page(r, listHeading(c))
	v := homeView{Title: page.Title, Shell: page.Shell, Ctx: c,
		Sidebar: viewSidebar(c, counts, tagNames, goal), Prefs: viewPrefs(prefs, c),
		List: viewList(items, byBook, c, prefs.View)}
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) && web.HTMXTarget(r) == "books-list" {
		if err := a.deps.Render.Fragment(w, http.StatusOK, "books/index", "list-swap", v); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	page.Data = v
	if err := a.deps.Render.Page(w, http.StatusOK, "books/index", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// bookOpts is what a book page shows besides the book: a refusal — in the
// banner, or for a progress update inside the progress box, or for a note
// or quote inside its form, with what was typed (spec "Errors": an inline
// message).
type bookOpts struct {
	Banner        string
	ProgressError string
	ProgressInput string
	Draft         entryDraft
}

// renderBook draws book id's page, opened from list c (UI redesign spec
// "Book details page"). A normal request gets the whole page — 422 when
// there is a refusal, so a refused form post without JavaScript isn't a
// 200. An htmx request gets the block for what it targeted:
// #books-reading → reading-swap (the whole Reading section, so Did not
// finish… offers the page just saved), #books-take → take-swap,
// #books-notes → notes-swap, #books-quotes → quotes-swap, anything else
// → book-swap (#books-book). A refusal's banner tops the page body,
// outside every section, so it is always book-swap, retargeted there.
// There is no list on the page, so none of them carries one. Fragments are
// always 200: htmx's default responseHandling only swaps 2xx/3xx.
func (a *App) renderBook(w http.ResponseWriter, r *http.Request, userID, id int64, c listCtx, opts bookOpts) {
	ctx := r.Context()
	b, err := a.store.Get(ctx, userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	rs, err := a.store.Readings(ctx, userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	ns, err := a.store.Notes(ctx, userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	qs, err := a.store.Quotes(ctx, userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	bv := viewBook(b, c, a.store.Today())
	bv.History = viewHistory(rs)
	// A history form posts open=history, so saving or deleting a reading
	// doesn't fold the history away (it is collapsed by default).
	bv.HistoryOpen = r.PostFormValue("open") == "history"
	bv.Notes, bv.NewNote = viewNotes(ns, opts.Draft), formFor("note-new", opts.Draft, entryForm{})
	bv.Quotes, bv.NewQuote = viewQuotes(qs, opts.Draft), formFor("quote-new", opts.Draft, entryForm{})
	if opts.ProgressError != "" {
		bv.Progress.Error, bv.Progress.Value = opts.ProgressError, opts.ProgressInput
	}
	page := a.deps.Page(r, b.Title)
	bv.Shell = page.Shell
	v := bookPageView{Title: page.Title, Shell: page.Shell, Error: opts.Banner, Book: bv}

	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		block := "book-swap"
		switch target := web.HTMXTarget(r); {
		case opts.Banner != "":
			if target != "books-book" {
				w.Header().Set("HX-Retarget", "#books-book")
				w.Header().Set("HX-Reswap", "outerHTML")
			}
		case target == "books-reading":
			block = "reading-swap"
		case target == "books-take":
			block = "take-swap"
		case target == "books-notes":
			block = "notes-swap"
		case target == "books-quotes":
			block = "quotes-swap"
		}
		if err := a.deps.Render.Fragment(w, http.StatusOK, "books/book", block, v); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	status := http.StatusOK
	if opts.Banner != "" || opts.ProgressError != "" || opts.Draft.Error != "" {
		status = http.StatusUnprocessableEntity
	}
	page.Data = v
	if err := a.deps.Render.Page(w, status, "books/book", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// sidebarGoal is the sidebar's goal card: this year's goal, when one is
// set (UI redesign spec "Sidebar"). Read-only — it links to the Stats
// page, where the goal is set.
func (a *App) sidebarGoal(r *http.Request, userID int64) (*goalView, error) {
	today := a.store.now().Local()
	g, err := a.store.Goal(r.Context(), userID, today.Year())
	if err != nil || g.Target == 0 {
		return nil, err
	}
	v := viewGoal(g, today)
	return &v, nil
}
