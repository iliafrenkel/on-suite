package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/docs"
	"github.com/iliafrenkel/on-suite/internal/platform/admin"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
	"github.com/iliafrenkel/on-suite/internal/platform/help"
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
	"github.com/iliafrenkel/on-suite/internal/platform/jobsadmin"
	"github.com/iliafrenkel/on-suite/internal/platform/mcp"
	"github.com/iliafrenkel/on-suite/internal/platform/oauth"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/usermgmt"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

type stackDeps struct {
	DB       *sql.DB
	Users    *auth.Store
	Registry *app.Registry
	Log      *slog.Logger
	Version  string
	Secure   bool
	// HSTS adds Strict-Transport-Security; set when the suite is reached
	// over HTTPS (built-in TLS or an https public URL).
	HSTS bool
	// Config, Jobs and Started exist for the admin page. serve.go is the
	// only production caller of buildStack, and it is the one that
	// populates them from the real config, job registry and start time.
	Config  config.Config
	Jobs    *jobs.Registry
	Started time.Time
	// OAuth is the authorization server, from newOAuthServer: set exactly
	// when Config.MCP is on. serve builds it before the stack because the
	// maintenance jobs, registered first, need its Sweep.
	OAuth *oauth.Server
}

// newOAuthServer builds the authorization server when AI access is on, and
// returns nil when it is off (spec 2026-11-24-ai-access-mcp-design.md,
// "Wiring"). HTTPClient is left nil, so CIMD fetches go through the
// library's client that refuses loopback and private addresses.
func newOAuthServer(ctx context.Context, handle *sql.DB, cfg config.Config, log *slog.Logger) (*oauth.Server, error) {
	if !cfg.MCP {
		return nil, nil
	}
	return oauth.New(ctx, handle, oauth.Options{
		PublicURL:    cfg.PublicURL,
		RedirectURIs: cfg.OAuthRedirectURIs,
		Log:          log,
	})
}

func buildStack(deps stackDeps) (http.Handler, error) {
	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		return nil, err
	}
	rend, err := render.NewRenderer(render.Options{
		Layouts:       ui.Templates(),
		AssetURL:      assets.URL,
		CSRFFieldName: web.CSRFFormField,
	})
	if err != nil {
		return nil, err
	}

	errs := web.NewErrors(rend, deps.Log)
	errs.Version = deps.Version
	csrf := web.NewCSRF(deps.Secure, errs)
	authn := web.NewAuth(web.AuthOptions{
		Users:   deps.Users,
		Render:  rend,
		Errors:  errs,
		CSRF:    csrf,
		Log:     deps.Log,
		Secure:  deps.Secure,
		Version: deps.Version,
	})

	routes := web.NewRecorder()
	deps.Registry.RecordRoutes(routes)

	mux := http.NewServeMux()
	routes.Handle(mux, "GET /healthz", true, healthzHandler(deps.Version, deps.DB))
	routes.Handle(mux, "GET /favicon.ico", true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/favicon.ico"
		assets.Handler().ServeHTTP(w, r)
	}))
	routes.Handle(mux, "GET /static/", true, http.StripPrefix("/static", assets.Handler()))
	authn.Routes(mux, routes)

	appDeps := app.Deps{
		DB:      deps.DB,
		Render:  rend,
		Users:   deps.Users,
		Errors:  errs,
		Log:     deps.Log,
		Version: deps.Version,
		Secure:  deps.Secure,
	}
	if err := deps.Registry.Mount(mux, appDeps, authn.RequireUser); err != nil {
		return nil, err
	}

	// App-owned background work. Registered after Mount so an app can build
	// whatever its jobs close over in Mount, and before the server starts so
	// the admin page lists them from the first request.
	if deps.Jobs != nil {
		deps.Registry.RegisterJobs(deps.Jobs, appDeps)
	}

	// AI access: the OAuth server and /mcp exist only when it is on, so with
	// it off every one of their routes is the ordinary 404. After Mount,
	// because the tools read the stores Mount set up.
	if deps.Config.MCP {
		if err := mountAIAccess(mux, routes, authn, csrf, deps, appDeps, rend, errs); err != nil {
			return nil, err
		}
	}

	// The admin page is guarded, and RequireAdmin returns 404 (not 403) so a
	// non-admin cannot tell it apart from a route that does not exist at
	// all. ServeMux would undermine that on its own: registering only the
	// subtree pattern "GET /admin/" makes the mux synthesize an *unguarded*
	// 307 redirect from "/admin" to "/admin/", which lets anyone distinguish
	// /admin from a genuine 404 before RequireAdmin ever runs. So both the
	// exact "/admin" and the "/admin/{$}" forms are registered explicitly,
	// behind the same guard, and the subtree pattern is narrowed to "{$}" so
	// it no longer matches "/admin/anything".
	adminHandler := authn.RequireAdmin(admin.Handler(admin.Deps{
		Config:   deps.Config,
		DB:       deps.DB,
		Users:    deps.Users,
		Apps:     deps.Registry,
		Jobs:     deps.Jobs,
		Routes:   routes,
		Render:   rend,
		Errors:   errs,
		Nav:      deps.Registry.NavItems(),
		Version:  deps.Version,
		Started:  deps.Started,
		AIAccess: deps.Config.MCP,
	}))
	routes.Handle(mux, "GET /admin", false, adminHandler)
	routes.Handle(mux, "GET /admin/{$}", false, adminHandler)

	// User management changes accounts, so it lives beside the read-only
	// admin page rather than inside it (#310).
	usermgmt.Routes(mux, routes, authn, usermgmt.Deps{
		Users:    deps.Users,
		Render:   rend,
		Errors:   errs,
		Log:      deps.Log,
		Nav:      deps.Registry.NavItems(),
		Version:  deps.Version,
		AIAccess: deps.Config.MCP,
	})

	// Running a job on demand starts work, so it lives beside the read-only
	// admin page rather than inside it, like user management (#311).
	if deps.Jobs != nil {
		jobsadmin.Routes(mux, routes, authn, jobsadmin.Deps{
			Jobs:    deps.Jobs,
			Render:  rend,
			Errors:  errs,
			Log:     deps.Log,
			Nav:     deps.Registry.NavItems(),
			Version: deps.Version,
		})
	}

	// The user guides, public so they work signed out (#309).
	helpPages, err := help.Load(docs.User())
	if err != nil {
		return nil, err
	}
	help.Routes(mux, routes, help.Deps{
		Pages:   helpPages,
		Render:  rend,
		Errors:  errs,
		Nav:     deps.Registry.NavItems(),
		Version: deps.Version,
		Users:   deps.Users,
		Log:     deps.Log,
	})

	routes.Handle(mux, "GET /{$}", false, authn.RequireUser(homeHandler(deps, rend, errs)))
	routes.Handle(mux, "/", true, http.HandlerFunc(errs.NotFound))

	return web.Stack(mux, deps.Log, errs, csrf, authn, deps.HSTS), nil
}

// mountAIAccess mounts the OAuth authorization server and the MCP endpoint
// (spec 2026-11-24-ai-access-mcp-design.md, "Wiring"). oauth.Routes exempts
// its own machine-to-machine endpoints from the CSRF check; /mcp is exempted
// here, since its callers are AI clients' servers with a bearer token and no
// cookie, and the bearer check is its guard.
func mountAIAccess(mux *http.ServeMux, routes *web.Recorder, authn *web.Auth, csrf *web.CSRF,
	deps stackDeps, appDeps app.Deps, rend *render.Renderer, errs *web.Errors) error {
	if deps.OAuth == nil {
		return errors.New("AI access is on but no OAuth server was built")
	}
	deps.OAuth.Routes(mux, routes, authn, csrf, oauth.PageDeps{
		Render:  rend,
		Errors:  errs,
		Nav:     deps.Registry.NavItems(),
		Version: deps.Version,
		Log:     deps.Log,
	})

	tools, err := deps.Registry.Tools(appDeps)
	if err != nil {
		return err
	}
	store := deps.OAuth.Store()
	csrf.Exempt("/mcp")
	mcp.Routes(mux, routes, mcp.Deps{
		Tools:  tools,
		Verify: deps.OAuth.Verify,
		// A failed audit write is logged, not returned: the call itself
		// already happened.
		Audit: func(ctx context.Context, userID int64, clientName, tool string, ok bool, errorKind string) {
			if err := store.Audit(ctx, oauth.AuditEntry{
				UserID: userID, ClientName: clientName, Tool: tool, OK: ok, ErrorKind: errorKind,
			}); err != nil {
				deps.Log.Error("mcp: audit failed", "tool", tool, "user", userID, "error", err)
			}
		},
		ResourceURL: deps.OAuth.ResourceURL(),
		Issuer:      deps.OAuth.Issuer(),
		Version:     deps.Version,
		Log:         deps.Log,
	})
	return nil
}

// comingSoonApps lists apps that are specced but not yet built (see
// docs/superpowers/specs/2026-08-18-on-suite-platform-design.md §3), so the
// home page can name them instead of pretending only one app exists. Remove
// an app from this list the day it is actually registered in
// registeredApps() — TestComingSoonAppsExcludesRegisteredApps enforces it.
//
// Every specced app is built today, so the list is empty; the plumbing stays
// for the next one.
var comingSoonApps = []struct {
	ID      string
	Name    string
	Summary string
}{}

// homeHandler is the dashboard. It lists whatever apps are in this build,
// plus the specced-but-unbuilt ones, so adding a real app requires no change
// here beyond removing it from comingSoonApps.
//
// Coming-soon apps show up here (as a single muted line, not as cards — see
// home.html) but not in the sidebar (see buildStack's Registry.Mount call,
// which passes no extra nav items): a sidebar that is three-quarters
// placeholders for one real app reads as unfinished rather than restrained.
func homeHandler(deps stackDeps, rend *render.Renderer, errs *web.Errors) http.Handler {
	type entry struct {
		ID         string
		Name       string
		Path       string
		Summary    string
		ComingSoon bool
	}

	nav := deps.Registry.NavItems()
	entries := make([]entry, 0, len(deps.Registry.Apps())+len(comingSoonApps))
	for _, a := range deps.Registry.Apps() {
		m := a.Meta()
		entries = append(entries, entry{ID: m.ID, Name: m.Name, Path: m.Path(), Summary: m.Summary})
	}
	for _, a := range comingSoonApps {
		entries = append(entries, entry{ID: a.ID, Name: a.Name, Summary: a.Summary, ComingSoon: true})
	}
	comingSoonNames := make([]string, len(comingSoonApps))
	for i, a := range comingSoonApps {
		comingSoonNames[i] = a.Name
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := app.NewPage(r, "", nav)
		page.Shell.Version = deps.Version
		page.Data = map[string]any{
			"Apps":            entries,
			"ComingSoonNames": strings.Join(comingSoonNames, ", "),
		}

		if err := rend.Page(w, http.StatusOK, "home", page); err != nil {
			errs.Internal(w, r, err)
		}
	})
}
