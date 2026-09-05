package paste

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// ON Paste implements both optional capabilities. A compile-time assertion,
// because the platform discovers them by type assertion and a typo'd method
// name would otherwise fail silently as "this app has nothing to report".
var (
	_ app.Exporter = (*App)(nil)
	_ app.Stater   = (*App)(nil)
)

//go:embed templates/*.html
var templateFiles embed.FS

// App is ON Paste. It is constructed before the platform exists, in the
// registration slice in main, and receives everything it needs in Mount.
type App struct {
	store *Store
	deps  app.Deps

	css     []byte
	cssETag string
}

// New returns the app for registration.
func New() *App { return &App{} }

func (a *App) Meta() app.Meta {
	return app.Meta{
		ID:      ID,
		Name:    "ON Paste",
		Summary: "Keep and share snippets of code or text.",
		Order:   20,
	}
}

func (a *App) Migrations() fs.FS { return Migrations() }

func (a *App) Templates() fs.FS {
	sub, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		// Unreachable: a compile-time constant path checked by go:embed.
		panic("paste: embedded templates missing: " + err.Error())
	}
	return sub
}

// Mount wires the app up. Everything registered with Handle requires a signed
// in user; the three Public routes are the deliberate exceptions.
func (a *App) Mount(r *app.Router, deps app.Deps) {
	a.deps = deps
	a.store = NewStore(deps.DB)

	css, err := HighlightCSS()
	if err != nil {
		// At startup, with no request in flight, there is nothing to degrade
		// to: the app cannot render a snippet without its stylesheet.
		panic("paste: generating the highlight stylesheet failed: " + err.Error())
	}
	a.css = css
	sum := sha256.Sum256(css)
	a.cssETag = `"` + hex.EncodeToString(sum[:])[:16] + `"`

	// Several obvious-looking alternatives to the patterns below make
	// ServeMux panic at startup on pattern conflicts — see the /edit/{id}
	// comment just below for the clearest example of why the shape of each
	// pattern here is deliberate, not arbitrary.
	r.HandleFunc("GET /{$}", a.index)
	r.HandleFunc("GET /new", a.newForm)
	r.HandleFunc("POST /new", a.create)
	r.HandleFunc("GET /{id}", a.index)
	// GET /edit/{id}, not /{id}/edit: a wildcard-then-literal GET pattern here
	// would be genuinely ambiguous to ServeMux against the existing literal-
	// then-wildcard GET patterns below and the public GET /s/{slug} (e.g. both
	// "/raw/{id}" and "/{id}/edit" would match "/paste/raw/edit", and neither
	// is more specific), which panics at startup. Matching raw's verb-first
	// shape keeps every GET pattern in the same, non-conflicting family.
	r.HandleFunc("GET /edit/{id}", a.editForm)
	r.HandleFunc("POST /{id}", a.update)
	r.HandleFunc("GET /raw/{id}", a.raw)
	r.HandleFunc("POST /{id}/delete", a.delete)
	r.HandleFunc("POST /{id}/share", a.share)
	r.HandleFunc("POST /{id}/unshare", a.unshare)

	// The stylesheet must load on the shared page too, where nobody is signed
	// in, so it is public. It contains no user data.
	r.PublicFunc("GET /highlight.css", a.highlightCSS)
	r.PublicFunc("GET /s/{slug}", a.viewShared)
	r.PublicFunc("GET /s/{slug}/raw", a.rawShared)
}

// highlightCSS serves the generated stylesheet. It revalidates rather than
// caching for a fixed period, so upgrading Chroma takes effect on the next
// request instead of whenever a max-age happens to lapse.
func (a *App) highlightCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("ETag", a.cssETag)
	w.Header().Set("Cache-Control", "no-cache")

	if match := r.Header.Get("If-None-Match"); match == a.cssETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if _, err := w.Write(a.css); err != nil {
		a.deps.Log.Error("writing the highlight stylesheet failed", "error", err)
	}
}

// Stats implements app.Stater, so ON Paste appears on the admin page. Like
// Export it takes the database rather than using a.store, so it works on a
// handle the platform already has without depending on Mount having run.
func (a *App) Stats(ctx context.Context, handle *sql.DB) ([]app.Stat, error) {
	return NewStore(handle).Stats(ctx)
}
