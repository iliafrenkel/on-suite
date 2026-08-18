package main

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

// stackDeps is everything the HTTP stack needs. It exists so buildStack has
// one parameter rather than six, and so tests can substitute pieces.
type stackDeps struct {
	DB      *sql.DB
	Log     *slog.Logger
	Version string
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

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", healthzHandler(deps.Version, deps.DB))
	mux.Handle("GET /static/", http.StripPrefix("/static", assets.Handler()))

	mux.Handle("GET /{$}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := rend.Page(w, http.StatusOK, "error", render.Page{
			Title: "ON Suite",
			Shell: render.Shell{Version: deps.Version},
			Data: map[string]any{
				"Status": http.StatusOK, "Title": "It works",
				"Message": "The renderer and the design system are wired up.",
			},
		}); err != nil {
			deps.Log.Error("render root", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	}))

	return mux, nil
}
