package focus

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
// 404, the same as a timer that isn't there.
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

// render draws a full page with the shell around it.
func (a *App) render(w http.ResponseWriter, r *http.Request, status int, name, title string, data any) {
	page := a.deps.Page(r, title)
	page.Data = data
	if err := a.deps.Render.Page(w, status, name, page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// tileView is one timer tile on the home page.
type tileView struct {
	ID      int64
	Name    string
	Color   string
	Summary string // "15 min" or "50 / 10 × 4 · long 30"
	Total   string // the pill: "15 min", "3h 20m"
}

type indexView struct {
	Tiles []tileView
}

func newTile(t Timer) tileView {
	return tileView{
		ID: t.ID, Name: t.Name, Color: t.Color,
		Summary: Summary(t.TimerInput),
		Total:   FormatLength(TotalSeconds(Phases(t.TimerInput))),
	}
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	timers, err := a.store.Timers(r.Context(), userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	view := indexView{}
	for _, t := range timers {
		view.Tiles = append(view.Tiles, newTile(t))
	}
	a.render(w, r, http.StatusOK, "focus/index", "Timers", view)
}

func (a *App) duplicate(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if _, err := a.store.DuplicateTimer(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/focus/", http.StatusSeeOther)
}

func (a *App) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteTimer(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/focus/", http.StatusSeeOther)
}
