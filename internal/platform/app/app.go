// Package app defines what an ON Suite application is and how one is mounted.
//
// An app is a package under internal/apps that implements App. It never
// imports another app, and the platform never imports it: the only coupling is
// this interface plus the explicit registration slice in cmd/onsuite.
package app

import (
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
	return NewPage(r, title, d.nav)
}

// NewPage builds a page shell from a request. Exported so the platform's own
// pages, which are not apps, can use the same code.
func NewPage(r *http.Request, title string, nav []render.NavItem) render.Page {
	shell := render.Shell{
		Apps:      nav,
		ActiveApp: web.ActiveApp(r.Context()),
		CSRFToken: web.CSRFToken(r.Context()),
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
}

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

// Mount registers every app's routes and templates.
//
// guard is applied to everything except routes an app explicitly declares
// public.
func (reg *Registry) Mount(mux *http.ServeMux, deps Deps, guard web.Middleware) error {
	deps.nav = reg.NavItems()

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
		r := newRouter(mux, m.ID, guard)
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
