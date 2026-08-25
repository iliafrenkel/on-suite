package notes

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// userID is the signed-in user's id. Every route is registered with Handle,
// so a missing user is a programming error rather than a bad request.
func (a *App) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u, ok := web.UserFrom(r.Context())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusUnauthorized)
		return 0, false
	}
	return u.ID, true
}

func (a *App) render(w http.ResponseWriter, r *http.Request, status int, name string, page render.Page) {
	if err := a.deps.Render.Page(w, status, name, page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// fail maps a store error onto a response.
//
// ErrNotFound is a 404 whether the bullet is missing or simply someone
// else's: a 403 would confirm that it exists. ErrCycle and ErrTooDeep are
// requests that are well-formed but not satisfiable against this tree, which
// is what 400 means here.
func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		a.deps.Errors.Status(w, r, http.StatusNotFound)
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrCycle), errors.Is(err, ErrTooDeep):
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
	default:
		a.deps.Errors.Internal(w, r, err)
	}
}

// outline renders the top-level outline.
func (a *App) outline(w http.ResponseWriter, r *http.Request) {
	a.renderOutline(w, r, RootID)
}

// nodeID parses the {id} wildcard. A path that is not a positive integer is a
// 404 rather than a 400: from outside, "there is nothing at that address" is
// the same answer either way, and it is the true one.
func (a *App) nodeID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// outlineZoomed renders the outline rooted at one node — spec §6: zoom is the
// URL, and the only difference from the top level is which node the recursive
// query starts at.
func (a *App) outlineZoomed(w http.ResponseWriter, r *http.Request) {
	id, ok := a.nodeID(w, r)
	if !ok {
		return
	}
	a.renderOutline(w, r, id)
}

// renderOutline draws the outline rooted at rootID: the breadcrumb, the
// visible rows, and nothing else. RootID means the top level.
//
// Every query here runs on the pool, outside any transaction, which is the
// only safe place for them — see the warning on mutate.
func (a *App) renderOutline(w http.ResponseWriter, r *http.Request, rootID int64) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}

	view := outlineView{CSRFToken: web.CSRFToken(r.Context())}

	// An empty title leaves the shell's breadcrumb reading "Home / ON Notes",
	// which is what the top level is. A zoomed outline names its root.
	title := ""
	if rootID != RootID {
		root, err := a.store.ByID(r.Context(), userID, rootID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		crumbs, err := a.store.Ancestors(r.Context(), userID, rootID)
		if err != nil {
			a.deps.Errors.Internal(w, r, err)
			return
		}
		view.Root, view.Zoomed, view.Crumbs = root, true, crumbs
		title = root.DisplayTitle()
	}

	flat, err := a.store.Outline(r.Context(), userID, rootID)
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view.Rows = nest(flat, rootID, view.CSRFToken)

	page := a.deps.Page(r, title)
	page.Data = view
	a.render(w, r, http.StatusOK, "notes/outline", page)
}
