// Package usermgmt lets people manage accounts from the browser: admins add,
// delete, promote, demote and reset other users at /admin/users, and
// everyone changes their own password at /account (#310, spec
// docs/superpowers/specs/2026-09-27-user-management-design.md).
//
// It is a sibling of package admin, not part of it: /admin/ is promised to be
// read-only, and every handler here changes something. Like admin, it sits at
// the top of the platform and nothing imports it but cmd/onsuite.
package usermgmt

import (
	"log/slog"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// Deps is everything the handlers use, assembled once by buildStack.
type Deps struct {
	Users   *auth.Store
	Render  *render.Renderer
	Errors  *web.Errors
	Log     *slog.Logger
	Nav     []render.NavItem
	Version string
}

// Routes registers every route this package serves. buildStack and the tests
// both call it, so the patterns (and their guards) exist in one place.
//
// Each pattern is exact. None ends in "/", so ServeMux never synthesizes an
// unguarded redirect that would let a non-admin tell these paths apart
// from a genuine 404 (see the /admin registration in buildStack).
func Routes(mux *http.ServeMux, rec *web.Recorder, authn *web.Auth, d Deps) {
	h := &handlers{d: d}
	admin := func(f http.HandlerFunc) http.Handler { return authn.RequireAdmin(f) }
	user := func(f http.HandlerFunc) http.Handler { return authn.RequireUser(f) }

	rec.Handle(mux, "GET /admin/users", false, admin(h.list))
	rec.Handle(mux, "POST /admin/users", false, admin(h.add))
	rec.Handle(mux, "POST /admin/users/{id}/password", false, admin(h.resetPassword))
	rec.Handle(mux, "POST /admin/users/{id}/role", false, admin(h.setRole))
	rec.Handle(mux, "GET /admin/users/{id}/delete", false, admin(h.confirmDelete))
	rec.Handle(mux, "POST /admin/users/{id}/delete", false, admin(h.delete))
	rec.Handle(mux, "GET /account", false, user(h.account))
	rec.Handle(mux, "POST /account/password", false, user(h.changePassword))
}

type handlers struct{ d Deps }

// page builds the shell. activeApp is "admin" for the admin pages, so the
// sidebar's Admin entry is marked current, and "" for /account.
func (h *handlers) page(r *http.Request, title, activeApp string) render.Page {
	r = r.WithContext(web.WithActiveApp(r.Context(), activeApp))
	p := app.NewPage(r, title, h.d.Nav)
	p.Shell.Version = h.d.Version
	return p
}

// audit records who changed which account. It never takes a password: the
// signature has nowhere to put one (spec §4.5).
func (h *handlers) audit(r *http.Request, action, target string) {
	me, _ := web.UserFrom(r.Context())
	h.d.Log.Info("account change", "action", action, "actor", me.Username, "target", target)
}
