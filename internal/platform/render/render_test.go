package render_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

func testRenderer(t *testing.T) *render.Renderer {
	t.Helper()
	r, err := render.NewRenderer(render.Options{
		Layouts:  ui.Templates(),
		AssetURL: func(name string) string { return "/static/" + name + "?v=test" },
	})
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	return r
}

func TestNewRendererParsesTheRealTemplates(t *testing.T) {
	r := testRenderer(t)
	if !r.Has("error") {
		t.Errorf("error template not registered; got %v", r.Names())
	}
}

func TestPageRendersADocumentWithTheShell(t *testing.T) {
	r := testRenderer(t)
	rec := httptest.NewRecorder()

	err := r.Page(rec, http.StatusOK, "error", render.Page{
		Title: "Not found",
		Shell: render.Shell{
			LoggedIn:  true,
			Username:  "ilia",
			CSRFToken: "tok123",
			ActiveApp: "paste",
			Apps: []render.NavItem{
				{ID: "paste", Name: "ON Paste", Path: "/paste/"},
				{ID: "notes", Name: "ON Notes", Path: "/notes/"},
			},
		},
		Data: map[string]any{"Status": 404, "Title": "Not found", "Message": "no such page"},
	})
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}

	doc := htmlassert.Parse(t, rec.Body.String())

	if got := htmlassert.Text(doc.MustHave("title")); got != "Not found · ON Suite" {
		t.Errorf("title = %q", got)
	}

	// The asset function must have been applied, not the raw filename.
	if href, _ := htmlassert.Attr(doc.MustHave("link[rel=stylesheet]"), "href"); href != "/static/app.css?v=test" {
		t.Errorf("stylesheet href = %q", href)
	}

	// The nav renders every app, and marks the active one.
	links := doc.QueryAll("nav.shell-nav a")
	if len(links) != 2 {
		t.Fatalf("nav has %d links, want 2", len(links))
	}
	if got := htmlassert.Text(links[0]); got != "ON Paste" {
		t.Errorf("first nav link = %q", got)
	}
	if _, ok := htmlassert.Attr(links[0], "aria-current"); !ok {
		t.Error("active app is not marked with aria-current")
	}
	if _, ok := htmlassert.Attr(links[1], "aria-current"); ok {
		t.Error("inactive app is marked with aria-current")
	}

	doc.MustHave(".shell-user")
	if got := htmlassert.Text(doc.MustHave(".shell-user span")); got != "ilia" {
		t.Errorf("username = %q", got)
	}
}

// TestPageOmitsUserChromeWhenLoggedOut is the negative case that matters: the
// login page must not offer a logout button.
func TestPageOmitsUserChromeWhenLoggedOut(t *testing.T) {
	r := testRenderer(t)
	rec := httptest.NewRecorder()

	if err := r.Page(rec, http.StatusOK, "error", render.Page{
		Shell: render.Shell{LoggedIn: false},
		Data:  map[string]any{"Status": 401, "Title": "Unauthorised", "Message": "log in"},
	}); err != nil {
		t.Fatal(err)
	}
	htmlassert.Parse(t, rec.Body.String()).MustNotHave(".shell-user")
}

// TestPageEscapesUntrustedValues guards the property html/template exists to
// provide. If this ever fails, stop and find out why.
func TestPageEscapesUntrustedValues(t *testing.T) {
	r := testRenderer(t)
	rec := httptest.NewRecorder()

	const attack = `<script>alert(1)</script>`
	if err := r.Page(rec, http.StatusOK, "error", render.Page{
		Shell: render.Shell{LoggedIn: true, Username: attack},
		Data:  map[string]any{"Status": 200, "Title": "x", "Message": attack},
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Body.String(), "<script>alert(1)</script>") {
		t.Fatal("untrusted input was rendered unescaped")
	}
	// It must still be present as text.
	if !strings.Contains(htmlassert.Parse(t, rec.Body.String()).Text(), "alert(1)") {
		t.Error("escaped value disappeared entirely")
	}
}

// TestCSRFTokenReachesHTMX covers the mechanism every non-GET HTMX request in
// the suite depends on. html/template escapes inside attribute values, so
// assert on the parsed attribute rather than the raw markup.
func TestCSRFTokenReachesHTMX(t *testing.T) {
	r := testRenderer(t)
	rec := httptest.NewRecorder()
	if err := r.Page(rec, http.StatusOK, "error", render.Page{
		Shell: render.Shell{CSRFToken: "tok-abc-123"},
		Data:  map[string]any{"Status": 200, "Title": "x", "Message": "y"},
	}); err != nil {
		t.Fatal(err)
	}

	doc := htmlassert.Parse(t, rec.Body.String())
	got, ok := htmlassert.Attr(doc.MustHave("body"), "hx-headers")
	if !ok {
		t.Fatal("body has no hx-headers attribute")
	}
	if !strings.Contains(got, "tok-abc-123") {
		t.Errorf("hx-headers = %q, does not carry the token", got)
	}
	if !strings.Contains(got, "X-CSRF-Token") {
		t.Errorf("hx-headers = %q, does not name the header", got)
	}
}

func TestFragmentRendersWithoutTheDocument(t *testing.T) {
	r := testRenderer(t)
	rec := httptest.NewRecorder()

	if err := r.Fragment(rec, http.StatusNotFound, "error", "fragment",
		map[string]any{"Status": 404, "Message": "gone"}); err != nil {
		t.Fatalf("Fragment: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<html") || strings.Contains(body, "shell-bar") {
		t.Errorf("fragment contains document chrome: %q", body)
	}
	if !strings.Contains(body, "gone") {
		t.Errorf("fragment missing its data: %q", body)
	}
}

func TestAddApp(t *testing.T) {
	r := testRenderer(t)
	app := fstest.MapFS{
		"list.html": {Data: []byte(`{{define "content"}}<ul id="items">{{range .Data}}{{template "row" .}}{{end}}</ul>{{end}}
		                            {{define "row"}}<li class="row">{{.}}</li>{{end}}`)},
	}
	if err := r.AddApp("paste", app); err != nil {
		t.Fatalf("AddApp: %v", err)
	}
	if !r.Has("paste/list") {
		t.Fatalf("paste/list not registered; got %v", r.Names())
	}

	rec := httptest.NewRecorder()
	if err := r.Page(rec, http.StatusOK, "paste/list", render.Page{
		Data: []string{"one", "two"},
	}); err != nil {
		t.Fatal(err)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if rows := doc.QueryAll("li.row"); len(rows) != 2 {
		t.Errorf("rendered %d rows, want 2", len(rows))
	}

	// The same block, rendered alone as an HTMX swap.
	rec = httptest.NewRecorder()
	if err := r.Fragment(rec, http.StatusOK, "paste/list", "row", "three"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `<li class="row">three</li>` {
		t.Errorf("fragment = %q", got)
	}
}

func TestIconFuncIsAvailableInTemplates(t *testing.T) {
	r := testRenderer(t)
	app := fstest.MapFS{
		"icon.html": {Data: []byte(`{{define "content"}}{{icon "paste"}}{{end}}`)},
	}
	if err := r.AddApp("iconcheck", app); err != nil {
		t.Fatalf("AddApp: %v", err)
	}
	rec := httptest.NewRecorder()
	if err := r.Page(rec, http.StatusOK, "iconcheck/icon", render.Page{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), "<svg") {
		t.Errorf("body has no <svg>: %q", rec.Body.String())
	}
}

func TestShellHasBreadcrumbsSidebarAndFooter(t *testing.T) {
	r := testRenderer(t)
	rec := httptest.NewRecorder()

	err := r.Page(rec, http.StatusOK, "error", render.Page{
		Title: "New snippet",
		Shell: render.Shell{
			LoggedIn:      true,
			Username:      "ilia",
			Theme:         "dark",
			Font:          "literata",
			ActiveApp:     "paste",
			ActiveAppName: "ON Paste",
			ActiveAppPath: "/paste/",
			Version:       "v1.2.3",
			Apps: []render.NavItem{
				{ID: "paste", Name: "ON Paste", Path: "/paste/"},
				{ID: "notes", Name: "ON Notes", ComingSoon: true},
			},
		},
		Data: map[string]any{"Status": 404, "Title": "Not found", "Message": "no such page"},
	})
	if err != nil {
		t.Fatal(err)
	}
	doc := htmlassert.Parse(t, rec.Body.String())

	// data-theme/data-font land on <html>, set from the very first response.
	if v, _ := htmlassert.Attr(doc.MustHave("html"), "data-theme"); v != "dark" {
		t.Errorf("data-theme = %q, want dark", v)
	}
	if v, _ := htmlassert.Attr(doc.MustHave("html"), "data-font"); v != "literata" {
		t.Errorf("data-font = %q, want literata", v)
	}

	// Breadcrumbs: Home / ON Paste / New snippet.
	crumbs := doc.Text()
	for _, want := range []string{"Home", "ON Paste", "New snippet"} {
		if !strings.Contains(crumbs, want) {
			t.Errorf("breadcrumb text missing %q; got %q", want, crumbs)
		}
	}

	// The sidebar shows the real app as a link and the coming-soon one as
	// something else, not a link.
	links := doc.QueryAll("nav.shell-nav a")
	if len(links) != 1 {
		t.Fatalf("nav has %d links, want 1 (the coming-soon entry must not be a link); got %+v", len(links), links)
	}

	// The footer carries the version, outside the sidebar/header.
	doc.MustHave("footer.app-footer")
	if !strings.Contains(htmlassert.Text(doc.MustHave("footer.app-footer")), "v1.2.3") {
		t.Error("footer does not show the version")
	}
}

func TestRendererRejectsBadInput(t *testing.T) {
	if _, err := render.NewRenderer(render.Options{AssetURL: func(string) string { return "" }}); err == nil {
		t.Error("NewRenderer accepted a nil Layouts")
	}
	if _, err := render.NewRenderer(render.Options{Layouts: ui.Templates()}); err == nil {
		t.Error("NewRenderer accepted a nil AssetURL")
	}

	r := testRenderer(t)
	if err := r.AddApp("", fstest.MapFS{"a.html": {Data: []byte("x")}}); err == nil {
		t.Error("AddApp accepted an empty id")
	}
	if err := r.AddApp("empty", fstest.MapFS{}); err == nil {
		t.Error("AddApp accepted an app with no templates")
	}

	app := fstest.MapFS{"list.html": {Data: []byte(`{{define "content"}}x{{end}}`)}}
	if err := r.AddApp("dup", app); err != nil {
		t.Fatal(err)
	}
	if err := r.AddApp("dup", app); err == nil {
		t.Error("AddApp allowed the same app to register twice")
	}

	rec := httptest.NewRecorder()
	if err := r.Page(rec, http.StatusOK, "nope", render.Page{}); err == nil {
		t.Error("Page rendered an unregistered template")
	}
	if err := r.Fragment(rec, http.StatusOK, "error", "no-such-block", nil); err == nil {
		t.Error("Fragment rendered a nonexistent block")
	}
}
