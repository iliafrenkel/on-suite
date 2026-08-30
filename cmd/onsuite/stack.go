package main

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/admin"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

type stackDeps struct {
	DB       *sql.DB
	Users    *auth.Store
	Registry *app.Registry
	Log      *slog.Logger
	Version  string
	Secure   bool
	// Config, Jobs and Started exist for the admin page. serve.go is the
	// only production caller of buildStack, and it is the one that
	// populates them from the real config, job registry and start time.
	Config  config.Config
	Jobs    *jobs.Registry
	Started time.Time
}

func buildStack(deps stackDeps) (http.Handler, error) {
	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		return nil, err
	}
	rend, err := render.NewRenderer(render.Options{
		Layouts:  ui.Templates(),
		AssetURL: assets.URL,
	})
	if err != nil {
		return nil, err
	}

	errs := web.NewErrors(rend, deps.Log)
	errs.Version = deps.Version
	csrf := web.NewCSRF(deps.Secure, errs)
	authn := web.NewAuth(web.AuthOptions{
		Users:   deps.Users,
		Render:  rend,
		Errors:  errs,
		CSRF:    csrf,
		Log:     deps.Log,
		Secure:  deps.Secure,
		Version: deps.Version,
	})

	routes := web.NewRecorder()
	deps.Registry.RecordRoutes(routes)

	mux := http.NewServeMux()
	routes.Handle(mux, "GET /healthz", true, healthzHandler(deps.Version, deps.DB))
	routes.Handle(mux, "GET /static/", true, http.StripPrefix("/static", assets.Handler()))
	authn.Routes(mux, routes)

	if err := deps.Registry.Mount(mux, app.Deps{
		DB:      deps.DB,
		Render:  rend,
		Users:   deps.Users,
		Errors:  errs,
		Log:     deps.Log,
		Version: deps.Version,
		Secure:  deps.Secure,
	}, authn.RequireUser); err != nil {
		return nil, err
	}

	// The admin page is guarded, and RequireAdmin returns 404 (not 403) so a
	// non-admin cannot tell it apart from a route that does not exist at
	// all. ServeMux would undermine that on its own: registering only the
	// subtree pattern "GET /admin/" makes the mux synthesize an *unguarded*
	// 307 redirect from "/admin" to "/admin/", which lets anyone distinguish
	// /admin from a genuine 404 before RequireAdmin ever runs. So both the
	// exact "/admin" and the "/admin/{$}" forms are registered explicitly,
	// behind the same guard, and the subtree pattern is narrowed to "{$}" so
	// it no longer matches "/admin/anything".
	adminHandler := authn.RequireAdmin(admin.Handler(admin.Deps{
		Config:  deps.Config,
		DB:      deps.DB,
		Users:   deps.Users,
		Apps:    deps.Registry,
		Jobs:    deps.Jobs,
		Routes:  routes,
		Render:  rend,
		Errors:  errs,
		Nav:     deps.Registry.NavItems(),
		Version: deps.Version,
		Started: deps.Started,
	}))
	routes.Handle(mux, "GET /admin", false, adminHandler)
	routes.Handle(mux, "GET /admin/{$}", false, adminHandler)
	routes.Handle(mux, "GET /{$}", false, authn.RequireUser(homeHandler(deps, rend, errs)))
	routes.Handle(mux, "/", true, http.HandlerFunc(errs.NotFound))

	return web.Stack(mux, deps.Log, errs, csrf, authn), nil
}

// comingSoonApps lists apps that are specced but not yet built (see
// docs/superpowers/specs/2026-08-18-on-suite-platform-design.md §3), so the
// home page can name them instead of pretending only one app exists. Remove
// an app from this list the day it is actually registered in
// registeredApps().
var comingSoonApps = []struct {
	ID      string
	Name    string
	Summary string
}{
	{ID: "reader", Name: "ON Reader", Summary: "An RSS reader for the feeds you follow."},
	{ID: "flash", Name: "ON Flash", Summary: "Flash cards for spaced repetition."},
}

// homeHandler is the dashboard. It lists whatever apps are in this build,
// plus the specced-but-unbuilt ones, so adding a real app requires no change
// here beyond removing it from comingSoonApps.
//
// Coming-soon apps show up here (as a single muted line, not as cards — see
// home.html) but not in the sidebar (see buildStack's Registry.Mount call,
// which passes no extra nav items): a sidebar that is three-quarters
// placeholders for one real app reads as unfinished rather than restrained.
func homeHandler(deps stackDeps, rend *render.Renderer, errs *web.Errors) http.Handler {
	type entry struct {
		ID         string
		Name       string
		Path       string
		Summary    string
		ComingSoon bool
	}

	nav := deps.Registry.NavItems()
	entries := make([]entry, 0, len(deps.Registry.Apps())+len(comingSoonApps))
	for _, a := range deps.Registry.Apps() {
		m := a.Meta()
		entries = append(entries, entry{ID: m.ID, Name: m.Name, Path: m.Path(), Summary: m.Summary})
	}
	for _, a := range comingSoonApps {
		entries = append(entries, entry{ID: a.ID, Name: a.Name, Summary: a.Summary, ComingSoon: true})
	}
	comingSoonNames := make([]string, len(comingSoonApps))
	for i, a := range comingSoonApps {
		comingSoonNames[i] = a.Name
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := app.NewPage(r, "", nav)
		page.Shell.Version = deps.Version
		page.Data = map[string]any{
			"Apps":            entries,
			"ComingSoonNames": strings.Join(comingSoonNames, ", "),
		}

		if err := rend.Page(w, http.StatusOK, "home", page); err != nil {
			errs.Internal(w, r, err)
		}
	})
}
