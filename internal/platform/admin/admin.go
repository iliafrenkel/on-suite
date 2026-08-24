// Package admin renders the administrator's view of the running system.
//
// It is a platform page rather than an app: it reports on the platform, and
// an app whose subject was the platform would invert the layering the
// architecture test protects. It sits at the top of the platform — it may
// import everything below it, and nothing imports it but cmd/onsuite.
//
// It is strictly read-only. There is no handler here that changes anything,
// and adding one is a spec change, not an implementation detail.
package admin

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// Deps is everything the page reads. It is assembled once by buildStack; the
// handler holds no globals and opens nothing at request time except the
// database file's stat.
type Deps struct {
	Config  config.Config
	DB      *sql.DB
	Users   *auth.Store
	Apps    *app.Registry
	Jobs    *jobs.Registry
	Routes  *web.Recorder
	Render  *render.Renderer
	Errors  *web.Errors
	Nav     []render.NavItem
	Version string
	// Started is when the process came up, for the uptime figure.
	Started time.Time
}

// Report is the page's view model. Each section carries its own error, so one
// failed collector renders as a note in its own card instead of replacing the
// whole page with a 500 — a broken PRAGMA must not hide the job status.
type Report struct {
	Runtime     RuntimeInfo
	Settings    []config.Setting
	Jobs        []jobs.Status
	Apps        []app.AppStats
	Accounts    []auth.Account
	AccountsErr string
	Sessions    SessionInfo
	SessionsErr string
	Database    DatabaseInfo
	DatabaseErr string
}

// collect gathers every section. It never returns an error: an error is a
// value on the section it belongs to.
func (d Deps) collect(ctx context.Context, now time.Time) Report {
	rep := Report{Runtime: d.runtimeInfo(now)}
	rep.Settings = d.Config.Settings()
	if d.Jobs != nil {
		rep.Jobs = d.Jobs.Snapshot()
	}

	if d.Apps != nil {
		rep.Apps = d.Apps.Stats(ctx, d.DB)
	}
	if d.Users != nil {
		accounts, err := d.Users.ListAccounts(ctx)
		if err != nil {
			rep.AccountsErr = err.Error()
		} else {
			rep.Accounts = accounts
		}
	}
	sessions, err := d.sessionInfo(ctx)
	rep.Sessions = sessions
	if err != nil {
		rep.SessionsErr = err.Error()
	}

	database, dbErr := d.databaseInfo(ctx)
	rep.Database = database
	if dbErr != nil {
		rep.DatabaseErr = dbErr.Error()
	}
	return rep
}

// Handler renders the page. Mount it behind Auth.RequireAdmin — this handler
// does no authorization of its own, exactly like every app handler.
func Handler(d Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(web.WithActiveApp(r.Context(), "admin"))
		page := app.NewPage(r, "Admin", d.Nav)
		page.Shell.Version = d.Version
		page.Data = d.collect(r.Context(), time.Now().UTC())

		if err := d.Render.Page(w, http.StatusOK, "admin", page); err != nil {
			d.Errors.Internal(w, r, err)
		}
	})
}
