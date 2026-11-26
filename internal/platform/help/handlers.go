package help

import (
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// Deps is everything the handlers use, assembled once by buildStack.
type Deps struct {
	Pages   *Pages
	Render  *render.Renderer
	Errors  *web.Errors
	Nav     []render.NavItem
	Version string
	// Users records that a signed-in user opened the changelog, which
	// clears their "What's new" badge. Nil leaves the badge alone.
	Users *auth.Store
	Log   *slog.Logger
}

// Routes registers /help. Every route is public: the guides hold nothing
// private, and a signed-out visitor is exactly who may need them.
//
// No pattern ends in "/", so ServeMux never synthesises a redirect; /help/
// and anything unknown fall through to the standard 404.
func Routes(mux *http.ServeMux, rec *web.Recorder, d Deps) {
	h := &handlers{d: d}
	rec.Handle(mux, "GET /help", true, http.HandlerFunc(h.page))
	rec.Handle(mux, "GET /help/{slug}", true, http.HandlerFunc(h.page))
	rec.Handle(mux, "GET /help/images/{file}", true, http.HandlerFunc(h.image))
}

type handlers struct{ d Deps }

// navEntry is one link in the help sidebar.
type navEntry struct {
	Href, Label string
	Current     bool
}

// helpPage is the view model for help.html.
type helpPage struct {
	Nav  []navEntry
	Body template.HTML
}

func href(slug string) string {
	if slug == "index" {
		return "/help"
	}
	return "/help/" + slug
}

// title is the browser tab and breadcrumb title: "Help" for the index,
// "<Label> help" for the rest.
func title(p Page) string {
	switch p.Slug {
	case "index":
		return "Help"
	case "changelog":
		return p.Label // "What's new", not "What's new help"
	}
	return p.Label + " help"
}

func (h *handlers) page(w http.ResponseWriter, r *http.Request) {
	pg, ok := h.d.Pages.Get(r.PathValue("slug"))
	if !ok {
		h.d.Errors.NotFound(w, r)
		return
	}
	data := helpPage{Body: pg.HTML}
	for _, p := range h.d.Pages.List() {
		data.Nav = append(data.Nav, navEntry{Href: href(p.Slug), Label: p.Label, Current: p.Slug == pg.Slug})
	}
	page := app.NewPage(r, title(pg), h.d.Nav)
	page.Shell.Version = h.d.Version
	if pg.Slug == changelogSlug {
		h.markSeen(r, &page)
	}
	page.Data = data
	if err := h.d.Render.Page(w, http.StatusOK, "help", page); err != nil {
		h.d.Errors.Internal(w, r, err)
	}
}

// changelogSlug is the "What's new" page; opening it clears the badge.
const changelogSlug = "changelog"

// markSeen records that the signed-in user has now seen this version's
// changelog, and drops the badge from the page being rendered so it's gone
// right away rather than on the next page. It's a write on GET on purpose:
// the worst a forged request can do is clear someone's badge, and the page
// keeps working with JavaScript off. A failed write only means the badge
// stays, so it is logged, not shown.
func (h *handlers) markSeen(r *http.Request, page *render.Page) {
	if !page.Shell.WhatsNew() || h.d.Users == nil {
		return
	}
	u, ok := web.UserFrom(r.Context())
	if !ok {
		return
	}
	if err := h.d.Users.MarkSeen(r.Context(), u.ID, h.d.Version); err != nil {
		if h.d.Log != nil {
			h.d.Log.Warn("help: could not mark the changelog seen", "user", u.ID, "error", err)
		}
		return
	}
	page.Shell.SeenVersion = h.d.Version
}

// image serves one screenshot from the guides' images directory. Only a
// plain file name that exists is served; anything else is the normal 404.
func (h *handlers) image(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	if !fs.ValidPath(file) || file == "." || strings.Contains(file, "/") {
		h.d.Errors.NotFound(w, r)
		return
	}
	fi, err := fs.Stat(h.d.Pages.Images(), file)
	if err != nil || !fi.Mode().IsRegular() {
		h.d.Errors.NotFound(w, r)
		return
	}
	// The images are embedded in the binary, so they change only on an
	// upgrade; a day is long enough to matter and short enough not to.
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFileFS(w, r, h.d.Pages.Images(), file)
}
