// Package app defines what an ON Suite application is and how one is mounted.
//
// An app is a package under internal/apps that implements App. It never
// imports another app, and the platform never imports it: the only coupling is
// this interface plus the explicit registration slice in cmd/onsuite.
package app

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"regexp"
	"sort"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// Meta describes an app to the shell.
type Meta struct {
	// ID is a lowercase single word. It is the URL prefix, the migration
	// namespace, and the prefix on every table the app owns.
	ID string
	// Name is the display name, always "ON " followed by one word.
	Name string
	// Summary is one short line for the dashboard.
	Summary string
	// Order positions the app in the switcher. Ties break alphabetically.
	Order int
}

var (
	idPattern   = regexp.MustCompile(`^[a-z][a-z0-9]{1,15}$`)
	namePattern = regexp.MustCompile(`^ON [A-Z][A-Za-z]+$`)
)

// Validate keeps the naming convention from drifting as apps are added. The
// convention is load-bearing: ID is used as a URL prefix, a migration
// namespace and a table prefix all at once.
func (m Meta) Validate() error {
	if !idPattern.MatchString(m.ID) {
		return fmt.Errorf("app: id %q must be 2-16 lowercase letters or digits, starting with a letter", m.ID)
	}
	if !namePattern.MatchString(m.Name) {
		return fmt.Errorf("app: name %q must look like \"ON Paste\"", m.Name)
	}
	if m.Summary == "" {
		return fmt.Errorf("app: %s has no Summary", m.ID)
	}
	return nil
}

// Path is where the app is mounted.
func (m Meta) Path() string { return "/" + m.ID + "/" }

// App is what every ON Suite application implements. Three methods: who am I,
// what is my schema, and here are my routes.
type App interface {
	Meta() Meta
	// Migrations holds this app's .sql files at the root of the returned FS.
	Migrations() fs.FS
	// Mount registers routes. Everything registered through Router.Handle
	// requires authentication; anonymous access is opt-in via Router.Public.
	Mount(r *Router, deps Deps)
}

// Deps is everything an app is given. Anything not here is either the
// platform's business or the app's own private concern.
type Deps struct {
	DB     *sql.DB
	Render *render.Renderer
	Users  *auth.Store
	Errors *web.Errors
	Log    *slog.Logger
	// Version is the running build version, shown in the footer of every
	// page.
	Version string
	// Secure is the platform's own configured secure-cookie flag (see
	// config.Config.SecureCookies) — issue #79: an app-level handler that
	// sets its own cookie (the first is notes' ShowCompletedCookie) needs
	// this the same way the platform's own session and CSRF cookies do, to
	// mark Secure correctly rather than guessing or hardcoding it. False on
	// a plain-HTTP dev server, for the same reason web.AuthOptions.Secure
	// must be: a Secure cookie is never sent over http, so hardcoding true
	// would make an app's own cookie silently stop working there.
	Secure bool

	// nav is the switcher contents, set by the registry so Page can fill in
	// the shell without every app knowing about every other app.
	nav []render.NavItem
}

// Page returns a render.Page with the shell already filled in from the
// request: who is signed in, the CSRF token, the active app and the nav.
//
// Apps set only Data on the result. This is the reason a handler never has to
// think about the chrome.
func (d Deps) Page(r *http.Request, title string) render.Page {
	p := NewPage(r, title, d.nav)
	p.Shell.Version = d.Version
	return p
}

// NewPage builds a page shell from a request. Exported so the platform's own
// pages, which are not apps, can use the same code.
func NewPage(r *http.Request, title string, nav []render.NavItem) render.Page {
	shell := render.Shell{
		Apps:             nav,
		ActiveApp:        web.ActiveApp(r.Context()),
		CSRFToken:        web.CSRFToken(r.Context()),
		Theme:            web.ThemeFrom(r),
		Font:             web.FontFrom(r),
		SidebarCollapsed: web.SidebarCollapsedFrom(r),
	}
	for _, n := range nav {
		if n.ID == shell.ActiveApp {
			shell.ActiveAppName = n.Name
			shell.ActiveAppPath = n.Path
			break
		}
	}
	if u, ok := web.UserFrom(r.Context()); ok {
		shell.LoggedIn = true
		shell.Username = u.Username
		shell.IsAdmin = u.IsAdmin
	}
	return render.Page{Title: title, Shell: shell}
}

// Registry is the set of apps compiled into this binary.
type Registry struct {
	apps []App
	rec  *web.Recorder
}

// RecordRoutes tells the registry where to record each app's routes, so the
// admin page can show one complete map. It is optional: without a recorder,
// routes are still available from Router.Routes().
func (reg *Registry) RecordRoutes(rec *web.Recorder) { reg.rec = rec }

// NewRegistry validates the apps and orders them for display. Called once,
// from main, with an explicit slice: the contents of the binary are always
// readable in one place.
func NewRegistry(apps ...App) (*Registry, error) {
	seen := make(map[string]bool, len(apps))
	for _, a := range apps {
		m := a.Meta()
		if err := m.Validate(); err != nil {
			return nil, err
		}
		if seen[m.ID] {
			return nil, fmt.Errorf("app: %q is registered twice", m.ID)
		}
		seen[m.ID] = true
	}

	ordered := make([]App, len(apps))
	copy(ordered, apps)
	sort.SliceStable(ordered, func(i, j int) bool {
		mi, mj := ordered[i].Meta(), ordered[j].Meta()
		if mi.Order != mj.Order {
			return mi.Order < mj.Order
		}
		return mi.ID < mj.ID
	})
	return &Registry{apps: ordered}, nil
}

// Apps returns the registered apps in display order.
func (reg *Registry) Apps() []App { return reg.apps }

// NavItems is the app switcher contents.
func (reg *Registry) NavItems() []render.NavItem {
	out := make([]render.NavItem, 0, len(reg.apps))
	for _, a := range reg.apps {
		m := a.Meta()
		out = append(out, render.NavItem{ID: m.ID, Name: m.Name, Path: m.Path()})
	}
	return out
}

// Migrations collects every app's schema, namespaced by app id. An app that is
// not registered contributes nothing, so its tables are never created.
func (reg *Registry) Migrations() ([]db.Migration, error) {
	var out []db.Migration
	for _, a := range reg.apps {
		id := a.Meta().ID
		ms, err := db.Collect(id, a.Migrations())
		if err != nil {
			return nil, err
		}
		out = append(out, ms...)
	}
	return out, nil
}

// Mount registers every app's routes and templates, then appends extra to
// the nav every page receives. extra is for placeholder entries — apps that
// are specced but not yet built — so the shell can show them as disabled
// rather than pretending they don't exist.
//
// guard is applied to everything except routes an app explicitly declares
// public.
func (reg *Registry) Mount(mux *http.ServeMux, deps Deps, guard web.Middleware, extra ...render.NavItem) error {
	deps.nav = append(reg.NavItems(), extra...)

	for _, a := range reg.apps {
		m := a.Meta()

		templates := appTemplates(a)
		if templates == nil {
			// Without this, fs.Glob dereferences a nil interface and the
			// server dies with a runtime panic instead of saying what is wrong.
			return fmt.Errorf("app: %q must implement Templates() fs.FS returning a non-nil filesystem", m.ID)
		}
		if err := deps.Render.AddApp(m.ID, templates); err != nil {
			return err
		}
		r := newRouter(mux, m.ID, guard, reg.rec)
		a.Mount(r, deps)
		if len(r.Routes()) == 0 {
			return fmt.Errorf("app: %q mounted no routes", m.ID)
		}
	}
	return nil
}

// appTemplates lets an app expose templates without adding a fourth method to
// the interface: if it implements Templates() fs.FS, that is used.
type templated interface {
	Templates() fs.FS
}

func appTemplates(a App) fs.FS {
	if t, ok := a.(templated); ok {
		return t.Templates()
	}
	return nil
}

// Exporter is implemented by apps that can dump one user's data as JSON. It is
// optional and discovered by type assertion, so an app that has nothing to
// export does not have to stub out a method.
//
// It takes the database rather than using Mount's Deps, so exporting works
// from the command line without building the HTTP stack.
type Exporter interface {
	Export(ctx context.Context, handle *sql.DB, userID int64) (any, error)
}

// Export collects every registered app's data for one user, keyed by app id.
// Apps that do not implement Exporter are skipped silently — that is a design
// choice, not a failure.
func (reg *Registry) Export(ctx context.Context, handle *sql.DB, userID int64) (map[string]any, error) {
	out := make(map[string]any)
	for _, a := range reg.apps {
		e, ok := a.(Exporter)
		if !ok {
			continue
		}
		id := a.Meta().ID
		data, err := e.Export(ctx, handle, userID)
		if err != nil {
			return nil, fmt.Errorf("export %s: %w", id, err)
		}
		out[id] = data
	}
	return out, nil
}

// Stat is one number an app wants shown on the admin page.
//
// Value is a preformatted string because only the app knows whether 1234567
// should read as "1.2 MB" or "1,234,567". The platform renders label and
// value and formats nothing.
type Stat struct {
	Label string // "Snippets"
	Value string // "1204"
	Hint  string // optional one-liner; may be empty
}

// Stater is implemented by apps that describe themselves on the admin page.
// Like Exporter it is optional and discovered by type assertion, so an app
// with nothing to say does not have to stub out a method.
//
// It takes the database rather than Mount's Deps for the same reason Export
// does: it depends on data, not on an HTTP stack having been built.
type Stater interface {
	Stats(ctx context.Context, handle *sql.DB) ([]Stat, error)
}

// AppStats is one app's contribution to the admin page.
type AppStats struct {
	ID    string
	Name  string
	Stats []Stat
	// Err is this app's collector failing, recorded rather than returned.
	Err string
}

// Stats collects every registered app's numbers. Apps that do not implement
// Stater are skipped silently, exactly as they are for Export.
//
// Unlike Export, one app's failure does not fail the call: the error is
// recorded on that app's entry and the rest are still returned. An export
// missing an app is corrupt; a dashboard missing one card is a dashboard
// missing one card.
func (reg *Registry) Stats(ctx context.Context, handle *sql.DB) []AppStats {
	var out []AppStats
	for _, a := range reg.apps {
		s, ok := a.(Stater)
		if !ok {
			continue
		}
		m := a.Meta()
		entry := AppStats{ID: m.ID, Name: m.Name}
		stats, err := s.Stats(ctx, handle)
		if err != nil {
			entry.Err = err.Error()
		} else {
			entry.Stats = stats
		}
		out = append(out, entry)
	}
	return out
}
