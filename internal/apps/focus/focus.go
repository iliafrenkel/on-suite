package focus

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

var _ app.App = (*App)(nil)

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed static/*.js
var scriptFiles embed.FS

// App is ON Focus.
type App struct {
	store *Store
	deps  app.Deps
}

// New returns the app for registration in cmd/onsuite.
func New() *App { return &App{} }

func (a *App) Meta() app.Meta {
	return app.Meta{
		ID:      ID,
		Name:    "ON Focus",
		Summary: "Focus timers you set up once and reuse.",
		Order:   50,
	}
}

func (a *App) Migrations() fs.FS { return Migrations() }

func (a *App) Templates() fs.FS {
	sub, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		panic("focus: embedded templates missing: " + err.Error()) // unreachable
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
	r.HandleFunc("GET /new", a.newForm)
	r.HandleFunc("POST /timers", a.create)
	r.HandleFunc("GET /timers/{id}/edit", a.editForm)
	r.HandleFunc("POST /timers/order", a.order)
	r.HandleFunc("POST /timers/{id}", a.update)
	r.HandleFunc("POST /timers/{id}/duplicate", a.duplicate)
	r.HandleFunc("POST /timers/{id}/delete", a.delete)
	r.HandleFunc("GET /run/{id}", a.run)
	r.HandleFunc("GET /home.js", a.script("home.js"))
}

// script serves an embedded script behind the same sign-in requirement as
// every other route, as ON Later's later.js is.
func (a *App) script(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, scriptFiles, "static/"+name)
	}
}
