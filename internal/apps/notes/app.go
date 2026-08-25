package notes

import (
	"embed"
	"io/fs"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// App is ON Notes. It is constructed before the platform exists, in the
// registration slice in main, and receives everything it needs in Mount.
//
// The compile-time assertion is here rather than left implicit because the
// registry takes an interface: a method with the wrong signature would
// otherwise fail at the call site in main, several packages away from the
// mistake.
var _ app.App = (*App)(nil)

//go:embed templates/*.html
var templateFiles embed.FS

type App struct {
	store *Store
	deps  app.Deps
}

// New returns the app for registration.
func New() *App { return &App{} }

// Meta is copied verbatim from the design's §3a. ID doubles as the URL
// prefix, the migration namespace and this app's table prefix, so it is the
// constant the rest of the package already uses.
func (a *App) Meta() app.Meta {
	return app.Meta{
		ID:      ID,
		Name:    "ON Notes",
		Summary: "Organise notes and tasks in one outline.",
		Order:   10,
	}
}

func (a *App) Migrations() fs.FS { return Migrations() }

func (a *App) Templates() fs.FS {
	sub, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		// Unreachable: a compile-time constant path checked by go:embed.
		panic("notes: embedded templates missing: " + err.Error())
	}
	return sub
}

// Mount wires the app up. Everything here goes through Handle, so every route
// requires a signed-in user. ON Notes has exactly one public route in the
// finished design — the share page — and it arrives in N9; until then a
// PublicFunc call in this file is a bug.
//
// Route map. ServeMux panics at startup on conflicting patterns, and several
// plausible-looking alternatives to these do conflict, so read this before
// changing one:
//
//	GET  /notes/{$}          the top-level outline; {$} matches only the
//	                         trailing-slash form, so it cannot collide with
//	                         the {id} pattern below
//	GET  /notes/{id}         the outline zoomed to one node. {id} never
//	                         matches an empty segment, which is what keeps it
//	                         and {$} disjoint
//	POST /notes/new          a literal segment, and literals outrank {id}
//	POST /notes/{id}/text    and the seven other mutations: two segments
//	                         deeper than the zoom URL, so no pattern in this
//	                         list is a prefix of another
func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	a.store = NewStore(deps.DB)

	r.HandleFunc("GET /{$}", a.outline)
	r.HandleFunc("GET /{id}", a.outlineZoomed)
	r.HandleFunc("POST /new", a.create)
	r.HandleFunc("POST /{id}/text", a.setText)
	r.HandleFunc("POST /{id}/indent", a.indent)
	r.HandleFunc("POST /{id}/outdent", a.outdent)
}
