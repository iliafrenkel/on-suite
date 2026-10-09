package books

import (
	"embed"
	"io/fs"
	"net/http"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

var _ app.App = (*App)(nil)

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed static/*.js
var scriptFiles embed.FS

// App is ON Books.
type App struct {
	store *Store
	deps  app.Deps
	// web is the only way this app reaches the network.
	web *webfetch.Client
	ol  *OpenLibrary
	// thumbSem bounds concurrent thumbnail fetches: a results page asks for
	// up to ten at once, and Open Library is a free service.
	thumbSem chan struct{}
}

// New returns the app for registration in cmd/onsuite.
func New() *App { return &App{} }

func (a *App) Meta() app.Meta {
	return app.Meta{
		ID:      ID,
		Name:    "ON Books",
		Summary: "Keep track of what you read and what you thought of it.",
		Order:   45,
	}
}

func (a *App) Migrations() fs.FS { return Migrations() }

func (a *App) Templates() fs.FS {
	sub, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		panic("books: embedded templates missing: " + err.Error()) // unreachable
	}
	return sub
}

// editFormMaxBytes is the edit form's body budget: one cover plus the
// form's text fields and multipart overhead (the suite default is 1 MiB).
const editFormMaxBytes = MaxCoverBytes + 1<<20

func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	a.store = NewStore(deps.DB)
	if deps.Now != nil {
		a.store.SetClock(deps.Now)
	}
	a.web = webfetch.New(webfetch.Config{
		UserAgent:       "onsuite/" + deps.Version + " (ON Books; +https://github.com/iliafrenkel/on-suite)",
		DefaultAccept:   "application/json",
		DefaultMaxBytes: webfetch.MaxPageBytes,
	})
	a.ol = &OpenLibrary{Web: a.web, Base: "https://openlibrary.org", Covers: "https://covers.openlibrary.org", Timeout: 5 * time.Second}
	a.thumbSem = make(chan struct{}, 4)
	r.HandleFunc("GET /{$}", a.index)
	r.HandleFunc("GET /b/{id}", a.book)
	r.HandleFunc("GET /new", a.newForm)
	r.HandleFunc("POST /new", a.create)
	r.HandleFunc("GET /edit/{id}", a.editForm)
	r.RegisterBodyLimit("POST /edit/{id}", editFormMaxBytes)
	r.HandleFunc("POST /edit/{id}", a.update)
	r.HandleFunc("POST /start/{id}", a.act(a.start, false))
	r.HandleFunc("POST /finish/{id}", a.act(a.finish, false))
	r.HandleFunc("POST /dnf/{id}", a.act(a.dnf, false))
	r.HandleFunc("POST /progress/{id}", a.progress)
	r.HandleFunc("POST /format/{id}", a.act(a.setFormat, false))
	r.HandleFunc("POST /rating/{id}", a.act(a.setRating, false))
	r.HandleFunc("POST /review/{id}", a.act(a.setReview, false))
	r.HandleFunc("POST /readings/{id}/{rid}", a.act(a.editReading, false))
	r.HandleFunc("POST /readings/{id}/{rid}/delete", a.act(a.deleteReading, false))
	r.HandleFunc("POST /tags/{id}", a.act(a.setTags, false))
	r.HandleFunc("POST /delete/{id}", a.act(a.remove, true))
	r.HandleFunc("GET /cover/{id}", a.cover)
	r.HandleFunc("GET /olcover/{id}", a.olThumb)
	r.HandleFunc("GET /books.js", a.script("books.js"))
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
