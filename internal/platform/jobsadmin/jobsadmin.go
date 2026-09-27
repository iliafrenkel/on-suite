// Package jobsadmin lets admins run background jobs on demand at
// /admin/jobs (#311, spec docs/superpowers/specs/2026-09-27-trigger-jobs-design.md).
//
// It is a sibling of package admin, not part of it: /admin/ is promised to be
// read-only, and the POST here starts work. Like admin and usermgmt, it sits
// at the top of the platform and nothing imports it but cmd/onsuite.
package jobsadmin

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// Deps is everything the handlers use, assembled once by buildStack.
type Deps struct {
	Jobs    *jobs.Registry
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
// unguarded redirect that would let a non-admin tell these paths apart from a
// genuine 404 (see the /admin registration in buildStack).
func Routes(mux *http.ServeMux, rec *web.Recorder, authn *web.Auth, d Deps) {
	h := &handlers{d: d}
	admin := func(f http.HandlerFunc) http.Handler { return authn.RequireAdmin(f) }

	rec.Handle(mux, "GET /admin/jobs", false, admin(h.list))
	rec.Handle(mux, "GET /admin/jobs/table", false, admin(h.table))
	rec.Handle(mux, "POST /admin/jobs/{slug}/run", false, admin(h.run))
}

type handlers struct{ d Deps }

// jobsTable is the view model for the "jobs-table" block, which both the
// page and the polling fragment render. The fragment has no page shell, so
// the CSRF token the Run now forms need travels here rather than in Shell.
type jobsTable struct {
	Jobs      []jobs.Status
	CSRFToken string
	// Polling is true while any job runs; the block then carries the
	// hx-trigger that re-fetches it, and drops it once nothing runs, which
	// is what stops the polling (spec §4.2).
	Polling bool
}

// jobsPage is the view model for admin_jobs.html.
type jobsPage struct {
	Error string
	Table jobsTable
}

func (h *handlers) snapshot(r *http.Request) jobsTable {
	t := jobsTable{Jobs: h.d.Jobs.Snapshot(), CSRFToken: web.CSRFToken(r.Context())}
	for _, s := range t.Jobs {
		if s.Running {
			t.Polling = true
		}
	}
	return t
}

func (h *handlers) render(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	r = r.WithContext(web.WithActiveApp(r.Context(), "admin"))
	page := app.NewPage(r, "Jobs", h.d.Nav)
	page.Shell.Version = h.d.Version
	page.Data = jobsPage{Error: errMsg, Table: h.snapshot(r)}
	if err := h.d.Render.Page(w, status, "admin_jobs", page); err != nil {
		h.d.Errors.Internal(w, r, err)
	}
}

func (h *handlers) list(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "")
}

func (h *handlers) table(w http.ResponseWriter, r *http.Request) {
	if err := h.d.Render.Fragment(w, http.StatusOK, "admin_jobs", "jobs-table", h.snapshot(r)); err != nil {
		h.d.Errors.Internal(w, r, err)
	}
}

func (h *handlers) run(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	err := h.d.Jobs.Trigger(slug)
	switch {
	case errors.Is(err, jobs.ErrUnknownJob):
		h.d.Errors.NotFound(w, r)
	case errors.Is(err, jobs.ErrAlreadyRunning):
		h.render(w, r, http.StatusUnprocessableEntity, h.name(slug)+" is already running.")
	case err != nil:
		h.d.Errors.Internal(w, r, err)
	default:
		me, _ := web.UserFrom(r.Context())
		h.d.Log.Info("job triggered", "job", h.name(slug), "by", me.Username)
		http.Redirect(w, r, "/admin/jobs", http.StatusSeeOther)
	}
}

// name returns the display name of the job with this slug, or the slug
// itself if it has vanished (it cannot: jobs are only registered at start).
func (h *handlers) name(slug string) string {
	for _, s := range h.d.Jobs.Snapshot() {
		if s.Slug == slug {
			return s.Name
		}
	}
	return slug
}
