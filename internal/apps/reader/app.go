package reader

import (
	"context"
	"embed"
	"io/fs"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// ON Reader implements the scheduling capability. Compile-time assertions,
// because the platform discovers these by type assertion and a typo'd method
// name would otherwise fail silently as "this app has no background work".
var (
	_ app.App       = (*App)(nil)
	_ app.Scheduler = (*App)(nil)
)

//go:embed templates/*.html
var templateFiles embed.FS

// App is ON Reader.
type App struct {
	store  *Store
	client *Client
	poller *Poller
	deps   app.Deps
}

func New() *App { return &App{} }

func (a *App) Meta() app.Meta {
	return app.Meta{
		ID:      ID,
		Name:    "ON Reader",
		Summary: "Follow feeds and read what is new.",
		Order:   30,
	}
}

func (a *App) Migrations() fs.FS { return Migrations() }

func (a *App) Templates() fs.FS {
	sub, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		// Unreachable: a compile-time constant path checked by go:embed.
		panic("reader: embedded templates missing: " + err.Error())
	}
	return sub
}

func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	a.store = NewStore(deps.DB)
	a.client = NewClient(deps.Version)
	a.poller = NewPoller(a.store, a.client, deps.Log)

	r.HandleFunc("GET /{$}", a.index)
	r.HandleFunc("GET /feed/{id}", a.index)
	r.HandleFunc("GET /item/{id}", a.article)
	r.HandleFunc("POST /subscribe", a.subscribe)
	r.HandleFunc("POST /sub/{id}/delete", a.unsubscribe)
	r.HandleFunc("POST /folder", a.createFolder)
	r.HandleFunc("POST /folder/{id}/delete", a.deleteFolder)
	r.HandleFunc("POST /refresh", a.refresh)
}

// Jobs implements app.Scheduler. RegisterJobs runs after Mount, so the poller
// this closure captures is already built.
func (a *App) Jobs(deps app.Deps) []app.Job {
	return []app.Job{{
		Name:        "refresh feeds",
		Description: "Fetches every feed whose polling interval has elapsed.",
		Every:       pollTick,
		Run: func(ctx context.Context) error {
			return a.poller.PollDue(ctx)
		},
	}}
}
