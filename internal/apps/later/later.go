package later

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
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
	// client is the only way this app reaches the network.
	client *webfetch.Client
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
	a.client = webfetch.New(webfetch.Config{
		UserAgent:       "onsuite/" + deps.Version + " (ON Later; +https://github.com/iliafrenkel/on-suite)",
		DefaultAccept:   "text/html, application/xhtml+xml;q=0.9, */*;q=0.5",
		DefaultMaxBytes: webfetch.MaxPageBytes,
	})
	r.HandleFunc("GET /{$}", a.index)
	r.HandleFunc("POST /save", a.save)
	r.HandleFunc("GET /a/{id}", a.view)
	r.HandleFunc("POST /a/{id}/archive", a.setState(StateArchived, func(int64) string { return "/later/?tab=archived" }))
	r.HandleFunc("POST /a/{id}/unarchive", a.setState(StateUnread, func(int64) string { return "/later/?tab=unread" }))
	r.HandleFunc("POST /a/{id}/delete", a.delete)
	r.HandleFunc("POST /a/{id}/text", a.pasteText)
	r.HandleFunc("GET /later.js", a.script)
}

// script serves later.js behind the same sign-in requirement as every
// other route, as Reader's reader.js is.
func (a *App) script(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, scriptFiles, "static/later.js")
}
