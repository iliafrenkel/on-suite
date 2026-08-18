package main

import (
	"database/sql"
	"log/slog"
	"net/http"

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

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", healthzHandler(deps.Version, deps.DB))
	mux.Handle("GET /static/", http.StripPrefix("/static", assets.Handler()))

	return mux, nil
}
