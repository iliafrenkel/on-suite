package books

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

// index is a placeholder until Task 5 draws the panes.
func (a *App) index(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	page := a.deps.Page(r, "Reading")
	if err := a.deps.Render.Page(w, http.StatusOK, "books/index", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
