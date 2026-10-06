package later

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

var (
	_ app.App       = (*App)(nil)
	_ app.Scheduler = (*App)(nil)
	_ app.Exporter  = (*App)(nil)
	_ app.Stater    = (*App)(nil)
)

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed static/*.js
var scriptFiles embed.FS

// App is ON Later.
type App struct {
	store *Store
	deps  app.Deps
	// client is the only way this app reaches the network.
	client *webfetch.Client
	// imgSem bounds concurrent image downloads (views and the job share it).
	imgSem chan struct{}
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
	a.imgSem = make(chan struct{}, 4)
	r.HandleFunc("GET /{$}", a.index)
	r.HandleFunc("POST /save", a.save)
	r.HandleFunc("GET /save", a.popup)
	r.HandleFunc("GET /popup/done", a.popupDone)
	r.HandleFunc("GET /a/{id}", a.view)
	r.HandleFunc("POST /a/{id}/archive", a.setState(StateArchived, "/later/?tab=archived"))
	r.HandleFunc("POST /a/{id}/unarchive", a.setState(StateUnread, "/later/?tab=unread"))
	r.HandleFunc("POST /a/{id}/delete", a.delete)
	r.HandleFunc("POST /a/{id}/text", a.pasteText)
	r.HandleFunc("POST /a/{id}/progress", a.progress)
	r.HandleFunc("POST /a/{id}/note", a.setNote)
	r.HandleFunc("POST /a/{id}/tags", a.setTags)
	r.HandleFunc("GET /a/{id}/markdown", a.markdown)
	r.HandleFunc("POST /prefs", a.setPrefs)
	r.HandleFunc("POST /a/{id}/highlights", a.addHighlight)
	r.HandleFunc("POST /a/{id}/highlights/comment", a.changeHighlight(func(r *http.Request, art Article, hid int64) error {
		return a.store.SetHighlightComment(r.Context(), art.ID, hid, r.PostFormValue("comment"))
	}))
	r.HandleFunc("POST /a/{id}/highlights/delete", a.changeHighlight(func(r *http.Request, art Article, hid int64) error {
		return a.store.DeleteHighlight(r.Context(), art.ID, hid)
	}))
	r.HandleFunc("GET /later.js", a.script("later.js"))
	r.HandleFunc("GET /highlight.js", a.script("highlight.js"))
	r.HandleFunc("GET /img/{hash}", a.image)
	r.HandleFunc("GET /favicon/{hash}", a.favicon)
}

// imageDownloadEvery is how often stored articles' missing images are
// back-filled. Images also arrive on first view; this makes sure the ones
// nobody scrolled to are kept too.
const imageDownloadEvery = 10 * time.Minute

const imageDownloadBatch = 50

func (a *App) Jobs(deps app.Deps) []app.Job {
	return []app.Job{{
		Name:        "download images",
		Description: "Downloads and keeps images of saved articles that haven't been stored yet.",
		Every:       imageDownloadEvery,
		Run: func(ctx context.Context) error {
			n, err := a.DownloadImages(ctx, imageDownloadBatch)
			if n > 0 {
				a.deps.Log.Info("later stored article images", "count", n)
			}
			return err
		},
	}}
}

// script serves an embedded script (later.js, highlight.js) behind the same
// sign-in requirement as every other route, as Reader's reader.js is.
func (a *App) script(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, scriptFiles, "static/"+name)
	}
}
