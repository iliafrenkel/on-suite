package notes

import (
	"net/http"

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

// outline renders the top-level outline. Task 3 gives it something to draw.
func (a *App) outline(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	page := a.deps.Page(r, "")
	a.render(w, r, http.StatusOK, "notes/outline", page)
}
