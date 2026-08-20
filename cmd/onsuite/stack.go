package main

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
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
	csrf := web.NewCSRF(deps.Secure, errs)
	authn := web.NewAuth(web.AuthOptions{
		Users:  deps.Users,
		Render: rend,
		Errors: errs,
		CSRF:   csrf,
		Log:    deps.Log,
		Secure: deps.Secure,
	})

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", healthzHandler(deps.Version, deps.DB))
	mux.Handle("GET /static/", http.StripPrefix("/static", assets.Handler()))
	authn.Routes(mux)

	if err := deps.Registry.Mount(mux, app.Deps{
		DB:      deps.DB,
		Render:  rend,
		Users:   deps.Users,
		Errors:  errs,
		Log:     deps.Log,
		Version: deps.Version,
	}, authn.RequireUser, comingSoonNavItems()...); err != nil {
		return nil, err
	}

	mux.Handle("GET /{$}", authn.RequireUser(homeHandler(deps, rend, errs)))
	mux.Handle("/", http.HandlerFunc(errs.NotFound))

	return web.Chain(mux,
		web.Recover(deps.Log, errs),
		web.RequestLog(deps.Log),
		web.SecurityHeaders(),
		web.LimitBody(web.DefaultMaxBodyBytes),
		csrf.Middleware,
		authn.LoadUser,
	), nil
}

// comingSoonApps lists apps that are specced but not yet built (see
// docs/superpowers/specs/2026-08-18-on-suite-platform-design.md §3), so the
// shell can show them as placeholders instead of pretending only one app
// exists. Each entry becomes a muted, non-clickable card on the home page and
// a muted, non-clickable entry in the sidebar. Remove an app from this list
// the day it is actually registered in registeredApps().
var comingSoonApps = []struct {
	ID      string
	Name    string
	Summary string
}{
	{ID: "notes", Name: "ON Notes", Summary: "A hierarchical outliner for quick notes."},
	{ID: "reader", Name: "ON Reader", Summary: "An RSS reader for the feeds you follow."},
	{ID: "flash", Name: "ON Flash", Summary: "Flash cards for spaced repetition."},
}

// comingSoonNavItems is comingSoonApps in the shape Registry.Mount wants, for
// the sidebar.
func comingSoonNavItems() []render.NavItem {
	out := make([]render.NavItem, len(comingSoonApps))
	for i, a := range comingSoonApps {
		out[i] = render.NavItem{ID: a.ID, Name: a.Name, ComingSoon: true}
	}
	return out
}

// homeHandler is the dashboard. It lists whatever apps are in this build,
// plus the specced-but-unbuilt ones, so adding a real app requires no change
// here beyond removing it from comingSoonApps.
func homeHandler(deps stackDeps, rend *render.Renderer, errs *web.Errors) http.Handler {
	type entry struct {
		ID         string
		Name       string
		Path       string
		Summary    string
		ComingSoon bool
	}

	nav := append(deps.Registry.NavItems(), comingSoonNavItems()...)
	entries := make([]entry, 0, len(deps.Registry.Apps())+len(comingSoonApps))
	for _, a := range deps.Registry.Apps() {
		m := a.Meta()
		entries = append(entries, entry{ID: m.ID, Name: m.Name, Path: m.Path(), Summary: m.Summary})
	}
	for _, a := range comingSoonApps {
		entries = append(entries, entry{ID: a.ID, Name: a.Name, Summary: a.Summary, ComingSoon: true})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := app.NewPage(r, "", nav)
		page.Shell.Version = deps.Version
		page.Data = map[string]any{"Apps": entries}

		if err := rend.Page(w, http.StatusOK, "home", page); err != nil {
			errs.Internal(w, r, err)
		}
	})
}
