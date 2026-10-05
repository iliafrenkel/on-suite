package later

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

var _ app.App = (*App)(nil)

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed static/later.js
var scriptFiles embed.FS

// App is ON Later.
type App struct {
	store *Store
	deps  app.Deps
}

// New returns the app for registration in cmd/onsuite.
func New() *App { return &App{} }

func (a *App) Meta() app.Meta {
	return app.Meta{
		ID:      ID,
		Name:    "ON Later",
		Summary: "Save articles and read them properly.",
		Order:   35,
	}
}

func (a *App) Migrations() fs.FS { return Migrations() }

func (a *App) Templates() fs.FS {
	sub, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		panic("later: embedded templates missing: " + err.Error()) // unreachable
	}
	return sub
}

func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	a.store = NewStore(deps.DB)
	if deps.Now != nil {
		a.store.SetClock(deps.Now)
	}
	r.HandleFunc("GET /{$}", a.index)
	r.HandleFunc("GET /later.js", a.script)
}

// script serves later.js behind the same sign-in requirement as every
// other route, as Reader's reader.js is.
func (a *App) script(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, scriptFiles, "static/later.js")
}
