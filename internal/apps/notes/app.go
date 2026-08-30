package notes

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// App is ON Notes. It is constructed before the platform exists, in the
// registration slice in main, and receives everything it needs in Mount.
//
// The compile-time assertions are here rather than left implicit because
// the registry takes interfaces: a method with the wrong signature would
// otherwise fail at the call site in main or in Registry.Export, several
// packages away from the mistake.
var (
	_ app.App      = (*App)(nil)
	_ app.Exporter = (*App)(nil)
)

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed static/notes.js
var scriptFiles embed.FS

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

// script serves notes.js. It sits behind the same sign-in requirement as
// every other route in this app: there is no page that loads it without
// already being on an authenticated outline, and the design reserves the
// app's one public route for N9's share link.
func (a *App) script(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, scriptFiles, "static/notes.js")
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
//	GET  /notes/notes.js     a literal segment, so it outranks {id} even
//	                         though "notes.js" is not a valid id
//	POST /notes/prefs        likewise a literal segment; N5's show-completed
//	                         toggle
//	GET  /notes/due          likewise a literal segment; N5's due-date list
//	GET  /notes/search       likewise a literal segment; N6's search
//	GET  /notes/archive      likewise a literal segment; N7's archive list
//	GET  /notes/export       likewise a literal segment; N8's Markdown download
//	POST /notes/new          a literal segment, and literals outrank {id}
//	POST /notes/{id}/text    and the eight other mutations: two segments
//	                         deeper than the zoom URL, so no pattern in this
//	                         list is a prefix of another
func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	a.store = NewStore(deps.DB)

	r.HandleFunc("GET /{$}", a.outline)
	r.HandleFunc("GET /{id}", a.outlineZoomed)
	r.HandleFunc("GET /notes.js", a.script)
	r.HandleFunc("GET /due", a.dueList)
	r.HandleFunc("GET /search", a.search)
	r.HandleFunc("GET /archive", a.archiveList)
	r.HandleFunc("GET /export", a.export)
	r.HandleFunc("POST /new", a.create)
	r.HandleFunc("POST /prefs", a.prefs)
	r.HandleFunc("POST /{id}/text", a.setText)
	r.HandleFunc("POST /{id}/indent", a.indent)
	r.HandleFunc("POST /{id}/outdent", a.outdent)
	r.HandleFunc("POST /{id}/move", a.move)
	r.HandleFunc("POST /{id}/collapse", a.collapse)
	r.HandleFunc("POST /{id}/delete", a.remove)
	r.HandleFunc("POST /{id}/done", a.done)
	r.HandleFunc("POST /{id}/due", a.due)
	r.HandleFunc("POST /{id}/archive", a.archive)
}
