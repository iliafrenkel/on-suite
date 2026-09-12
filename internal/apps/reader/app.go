package reader

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"time"

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

//go:embed static/reader.js
var scriptFiles embed.FS

// App is ON Reader.
type App struct {
	store   *Store
	client  *Client
	poller  *Poller
	deps    app.Deps
	imgSem  chan struct{}
	fullSem chan struct{}
}

// articleFetchConcurrency bounds full-article extraction. Fetching is
// user-initiated and rare, but extraction parses a whole page, and nothing
// stops two people clicking at once.
const articleFetchConcurrency = 2

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

// script serves reader.js, behind the same sign-in requirement as every other
// route here — no page loads it without already being on an authenticated
// reader page.
func (a *App) script(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, scriptFiles, "static/reader.js")
}

func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	a.store = NewStore(deps.DB)
	a.client = NewClient(deps.Version)
	a.poller = NewPoller(a.store, a.client, deps.Log)
	a.imgSem = make(chan struct{}, imageFetchConcurrency)
	a.fullSem = make(chan struct{}, articleFetchConcurrency)

	r.HandleFunc("GET /{$}", a.index)
	r.HandleFunc("GET /reader.js", a.script)
	r.HandleFunc("GET /feed/{id}", a.index)
	r.HandleFunc("GET /item/{id}", a.article)
	r.HandleFunc("POST /item/{id}/full", a.fetchFull)
	r.HandleFunc("POST /subscribe", a.subscribe)
	r.HandleFunc("POST /sub/{id}/delete", a.unsubscribe)
	r.HandleFunc("POST /folder", a.createFolder)
	r.HandleFunc("POST /folder/{id}/delete", a.deleteFolder)
	r.HandleFunc("POST /refresh", a.refresh)

	// Scope lives in the path and the filter in the query string, so a URL is
	// shareable and hx-push-url writes something meaningful into history.
	r.HandleFunc("GET /starred", a.index)
	r.HandleFunc("POST /item/{id}/read", a.setRead)
	r.HandleFunc("POST /item/{id}/unread", a.setRead)
	r.HandleFunc("POST /item/{id}/star", a.toggleStar)
	r.HandleFunc("POST /read-all", a.markAllRead)
	r.HandleFunc("GET /img/{hash}", a.image)

	r.HandleFunc("GET /opml", a.exportOPML)
	r.HandleFunc("POST /opml", a.importOPML)
}

// purgeTick is daily. Retention is a housekeeping concern, not a
// user-visible one, so it runs rarely and off the read path.
const purgeTick = 24 * time.Hour

// ReindexBatchSize bounds one night's search reindexing. A household's sixty
// days of articles is a few thousand rows, so this converges in one or two
// nights after migration 0006 and costs one cheap indexed query thereafter.
const ReindexBatchSize = 2000

// Jobs implements app.Scheduler. RegisterJobs runs after Mount, so the poller
// this closure captures is already built.
func (a *App) Jobs(deps app.Deps) []app.Job {
	return []app.Job{
		{
			Name:        "refresh feeds",
			Description: "Fetches every feed whose polling interval has elapsed.",
			Every:       pollTick,
			Run: func(ctx context.Context) error {
				return a.poller.PollDue(ctx)
			},
		},
		{
			Name:        "purge old articles",
			Description: "Records daily reading statistics, backfills history, deletes read, unstarred articles older than the retention window, and reindexes articles for search.",
			Every:       purgeTick,
			Run: func(ctx context.Context) error {
				// Before the purge, always: retention is about to delete the
				// articles these counts are drawn from.
				if _, err := a.store.BackfillDailyStats(ctx); err != nil {
					return err
				}
				if err := a.store.RecordDailyStats(ctx, time.Now().UTC()); err != nil {
					return err
				}
				n, err := a.store.PurgeItems(ctx, time.Now().UTC().Add(-RetentionAge))
				if err != nil {
					return err
				}
				images, err := a.store.PurgeOrphanImages(ctx)
				if err != nil {
					return err
				}
				if n > 0 || images > 0 {
					a.deps.Log.Info("reader purged old articles", "items", n, "images", images)
				}
				indexed, err := a.store.ReindexBatch(ctx, ReindexBatchSize)
				if err != nil {
					return err
				}
				if indexed > 0 {
					a.deps.Log.Info("reader reindexed articles for search", "count", indexed)
				}
				return nil
			},
		},
	}
}
