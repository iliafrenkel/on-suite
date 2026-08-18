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
		DB:     deps.DB,
		Render: rend,
		Users:  deps.Users,
		Errors: errs,
		Log:    deps.Log,
	}, authn.RequireUser); err != nil {
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

// homeHandler is the dashboard. It lists whatever apps are in this build, so
// adding an app requires no change here.
func homeHandler(deps stackDeps, rend *render.Renderer, errs *web.Errors) http.Handler {
	type entry struct {
		Name    string
		Path    string
		Summary string
	}

	nav := deps.Registry.NavItems()
	entries := make([]entry, 0, len(nav))
	for _, a := range deps.Registry.Apps() {
		m := a.Meta()
		entries = append(entries, entry{Name: m.Name, Path: m.Path(), Summary: m.Summary})
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
