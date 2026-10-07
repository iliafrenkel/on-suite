package focus

import (
	"net/http"

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

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	a.render(w, r, http.StatusOK, "focus/index", "Timers", indexView{})
}
