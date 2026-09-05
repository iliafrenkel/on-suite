// Package render composes HTML templates.
//
// It deliberately imports nothing else from the platform. Everything it needs
// to draw a page — who is logged in, what belongs in the nav, the CSRF token —
// arrives as an argument, so rendering is a pure function of its inputs and
// can be tested without constructing a request.
package render

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/ui"
)

// NavItem is one entry in the app switcher.
type NavItem struct {
	ID   string
	Name string
	Path string
	// ComingSoon marks a placeholder for an app that is specced but not yet
	// built. It renders muted and is never a link.
	ComingSoon bool
}

// Shell is everything the surrounding chrome needs. It is a flat struct of
// plain values rather than a reference to a user record, so a template cannot
// reach through it into the database.
type Shell struct {
	LoggedIn  bool
	Username  string
	IsAdmin   bool
	Apps      []NavItem
	ActiveApp string
	CSRFToken string
	Version   string

	// Theme is "light" or "dark". Font is "default", "literata", or
	// "grotesk". Both are read from a cookie once per request, so the first
	// response already carries the right value.
	Theme string
	Font  string
	// SidebarCollapsed is whether the app switcher should render collapsed
	// to icons-only.
	SidebarCollapsed bool

	// ActiveAppName and ActiveAppPath are the display name and path of the
	// app named by ActiveApp, resolved once so templates never have to
	// search Apps themselves. Both are empty outside any app (e.g. on the
	// home page).
	ActiveAppName string
	ActiveAppPath string
}

// Page is the argument to Page. Data is the page's own view model.
type Page struct {
	Title string
	Shell Shell
	Data  any
}

type Options struct {
	// Layouts holds base.html and the platform's own pages.
	Layouts fs.FS
	// AssetURL resolves a static file name to a cache-busting URL. Injected
	// rather than imported so render does not depend on the web package.
	AssetURL func(string) string
	// CSRFFieldName is the form field name a template's hidden CSRF input
	// must use — web.CSRFFormField, injected for the same reason AssetURL
	// is: so render never imports web. Every hidden CSRF input across every
	// template should use the "csrfField" template func this becomes,
	// rather than hardcoding "csrf_token" independently in each one.
	CSRFFieldName string
}

// Renderer holds one parsed template set per page.
type Renderer struct {
	base  *template.Template
	pages map[string]*template.Template
	funcs template.FuncMap
}

// NewRenderer parses the shared layout and every page in opts.Layouts.
//
// All parsing happens at startup: a broken template is a startup failure
// rather than a 500 discovered by a user.
func NewRenderer(opts Options) (*Renderer, error) {
	if opts.Layouts == nil {
		return nil, fmt.Errorf("render: Layouts must not be nil")
	}
	if opts.AssetURL == nil {
		return nil, fmt.Errorf("render: AssetURL must not be nil")
	}
	if opts.CSRFFieldName == "" {
		return nil, fmt.Errorf("render: CSRFFieldName must not be empty")
	}

	r := &Renderer{
		pages: make(map[string]*template.Template),
		funcs: template.FuncMap{
			"asset":     opts.AssetURL,
			"icon":      ui.IconFor,
			"dict":      dict,
			"csrfField": func() string { return opts.CSRFFieldName },
		},
	}

	// base.html and any *.partial.html are shared by every page.
	base, err := template.New("base").Funcs(r.funcs).ParseFS(opts.Layouts, "base.html")
	if err != nil {
		return nil, fmt.Errorf("render: parse base.html: %w", err)
	}
	r.base = base

	pages, err := fs.Glob(opts.Layouts, "*.html")
	if err != nil {
		return nil, fmt.Errorf("render: glob layouts: %w", err)
	}
	for _, name := range pages {
		if name == "base.html" {
			continue
		}
		if err := r.addPage(strings.TrimSuffix(name, ".html"), opts.Layouts, name); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// AddApp registers every *.html at the root of templates under the app's id.
// Called once per app at startup, before the server listens.
//
// A file named *.partial.html is not a page of its own — it holds only
// {{define}} blocks meant to be shared, via {{template}}, across more than
// one of this app's pages (issue #89: the search box hand-copied across
// outline.html/due.html/search.html/archive.html, with nothing stopping the
// copies drifting apart). It is parsed into every one of this app's own
// page clones, alongside that page's own file — each page's template set
// otherwise stays exactly as independent as Fragment's own doc comment
// relies on: a partial is content a page opts into with an explicit
// {{template}} call, not another page's whole file appearing in its set.
func (r *Renderer) AddApp(id string, templates fs.FS) error {
	if id == "" {
		return fmt.Errorf("render: app id must not be empty")
	}
	names, err := fs.Glob(templates, "*.html")
	if err != nil {
		return fmt.Errorf("render: glob %s templates: %w", id, err)
	}

	var pages, partials []string
	for _, name := range names {
		if strings.HasSuffix(name, ".partial.html") {
			partials = append(partials, name)
		} else {
			pages = append(pages, name)
		}
	}
	if len(pages) == 0 {
		return fmt.Errorf("render: app %s has no templates", id)
	}

	for _, name := range pages {
		key := id + "/" + strings.TrimSuffix(path.Base(name), ".html")
		if err := r.addPage(key, templates, name, partials...); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) addPage(key string, fsys fs.FS, name string, partials ...string) error {
	if _, exists := r.pages[key]; exists {
		return fmt.Errorf("render: template %q is already registered", key)
	}
	clone, err := r.base.Clone()
	if err != nil {
		return fmt.Errorf("render: clone base for %s: %w", key, err)
	}
	if _, err := clone.ParseFS(fsys, append([]string{name}, partials...)...); err != nil {
		return fmt.Errorf("render: parse %s: %w", name, err)
	}
	r.pages[key] = clone
	return nil
}

// Has reports whether a template is registered. Used by tests and by the
// error renderer, which must not recurse if its own template is missing.
func (r *Renderer) Has(name string) bool {
	_, ok := r.pages[name]
	return ok
}

// Names lists registered templates, sorted. Used by tests.
func (r *Renderer) Names() []string {
	out := make([]string, 0, len(r.pages))
	for k := range r.pages {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Page renders a full document.
func (r *Renderer) Page(w http.ResponseWriter, status int, name string, p Page) error {
	t, ok := r.pages[name]
	if !ok {
		return fmt.Errorf("render: no such template %q", name)
	}
	return r.execute(w, status, t, "base", p)
}

// Fragment renders one named block from a page's template set, for HTMX
// swaps. Keeping partials in the same file as the page they belong to is what
// makes an HTMX codebase readable.
func (r *Renderer) Fragment(w http.ResponseWriter, status int, page, block string, data any) error {
	t, ok := r.pages[page]
	if !ok {
		return fmt.Errorf("render: no such template %q", page)
	}
	if t.Lookup(block) == nil {
		return fmt.Errorf("render: template %q has no block %q", page, block)
	}
	return r.execute(w, status, t, block, data)
}

// dict builds a map from alternating string-key/value arguments, for a
// {{template}} call that needs to pass more than one value where the action
// only accepts a single dot argument — e.g. {{template "x" (dict "A" 1 "B" 2)}}.
// A generic templating helper, not tied to any one app's page.
func dict(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("dict: odd number of arguments")
	}
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %v is not a string", pairs[i])
		}
		m[key] = pairs[i+1]
	}
	return m, nil
}

// execute renders into a buffer before touching the ResponseWriter, so a
// template error becomes a clean 500 rather than a half-written page with a
// 200 status already committed.
func (r *Renderer) execute(w http.ResponseWriter, status int, t *template.Template, name string, data any) error {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		return fmt.Errorf("render: execute %s: %w", name, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}
