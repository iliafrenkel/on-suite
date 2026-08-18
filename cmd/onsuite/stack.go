package main

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

// stackDeps is everything the HTTP stack needs. It exists so buildStack has
// one parameter rather than six, and so tests can substitute pieces.
type stackDeps struct {
	DB      *sql.DB
	Users   *auth.Store
	Log     *slog.Logger
	Version string
	Secure  bool
}

// buildStack assembles the complete HTTP handler. Later tasks in this plan add
// the renderer, middleware and app registry here; serve() stays concerned only
// with configuration, storage and the listener.
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

	mux.Handle("GET /{$}", authn.RequireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := web.UserFrom(r.Context())
		page := render.Page{
			Title: "ON Suite",
			Shell: render.Shell{
				LoggedIn: true, Username: u.Username, IsAdmin: u.IsAdmin,
				CSRFToken: web.CSRFToken(r.Context()), Version: deps.Version,
			},
			Data: map[string]any{"Status": 200, "Title": "Signed in", "Message": "No apps are registered yet."},
		}
		if err := rend.Page(w, http.StatusOK, "error", page); err != nil {
			errs.Internal(w, r, err)
		}
	})))

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
