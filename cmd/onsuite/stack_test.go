package main

import (
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

type fakeHomeApp struct{ id, name string }

func (f fakeHomeApp) Meta() app.Meta {
	return app.Meta{ID: f.id, Name: f.name, Summary: "does things", Order: 0}
}
func (f fakeHomeApp) Migrations() fs.FS { return fstest.MapFS{} }
func (f fakeHomeApp) Mount(r *app.Router, d app.Deps) {
	r.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {})
}

func testHomeHandler(t *testing.T) http.Handler {
	t.Helper()
	reg, err := app.NewRegistry(fakeHomeApp{id: "paste", name: "ON Paste"})
	if err != nil {
		t.Fatal(err)
	}
	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatal(err)
	}
	rend, err := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL})
	if err != nil {
		t.Fatal(err)
	}
	errs := web.NewErrors(rend, slog.New(slog.DiscardHandler))
	deps := stackDeps{Registry: reg, Version: "v9.9.9"}
	return homeHandler(deps, rend, errs)
}

func TestHomePageShowsRealAndComingSoonCards(t *testing.T) {
	rec := httptest.NewRecorder()
	testHomeHandler(t).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())

	real := doc.MustHave("a.app-card")
	if got := htmlassert.Text(doc.MustHave("a.app-card h3")); got != "ON Paste" {
		t.Errorf("real card name = %q", got)
	}
	if _, ok := htmlassert.Attr(real, "href"); !ok {
		t.Error("the real app's card has no href")
	}

	disabled := doc.QueryAll("div.app-card")
	if len(disabled) != len(comingSoonApps) {
		t.Fatalf("got %d coming-soon cards, want %d", len(disabled), len(comingSoonApps))
	}
	if _, ok := htmlassert.Attr(disabled[0], "href"); ok {
		t.Error("a coming-soon card must not be a link")
	}
}

func TestHomePageFootershowsVersion(t *testing.T) {
	rec := httptest.NewRecorder()
	testHomeHandler(t).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	text := htmlassert.Parse(t, rec.Body.String()).Text()
	if !strings.Contains(text, "v9.9.9") {
		t.Errorf("footer text %q does not mention the version", text)
	}
}
