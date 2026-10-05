package reader

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

// renderArticle draws the "article" block with the real templates.
func renderArticle(t *testing.T, v articleView) *htmlassert.Doc {
	t.Helper()
	r, err := render.NewRenderer(render.Options{
		Layouts:       ui.Templates(),
		AssetURL:      func(name string) string { return "/static/" + name },
		CSRFFieldName: web.CSRFFormField,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AddApp("reader", New().Templates()); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := r.Fragment(rec, http.StatusOK, "reader/index", "article", v); err != nil {
		t.Fatal(err)
	}
	return htmlassert.Parse(t, rec.Body.String())
}

func TestArticleOffersReadLaterWhenLaterIsEnabled(t *testing.T) {
	doc := renderArticle(t, articleView{
		ID: 7, Selected: true, Title: "T", URL: "https://example.com/a?x=1&y=2",
		LaterEnabled: true, Shell: render.Shell{CSRFToken: "tok"},
	})
	form := doc.MustHave("form[hx-post=/later/save]")
	if got, _ := htmlassert.Attr(form, "action"); got != "/later/save" {
		t.Errorf("action = %q, want /later/save (the no-JS fallback)", got)
	}
	in := doc.MustHave("form[hx-post=/later/save] input[name=url]")
	if got, _ := htmlassert.Attr(in, "value"); got != "https://example.com/a?x=1&y=2" {
		t.Errorf("url value = %q", got)
	}
	doc.MustHave("form[hx-post=/later/save] input[name=" + web.CSRFFormField + "]")
	if got := htmlassert.Text(doc.MustHave("form[hx-post=/later/save] button")); !strings.Contains(got, "Read later") {
		t.Errorf("button = %q, want Read later", got)
	}
}

func TestArticleHidesReadLaterWhenLaterIsOff(t *testing.T) {
	doc := renderArticle(t, articleView{ID: 7, Selected: true, Title: "T", URL: "https://example.com/a"})
	doc.MustNotHave("form[hx-post=/later/save]")
}

func TestArticleHidesReadLaterWithoutAURL(t *testing.T) {
	doc := renderArticle(t, articleView{ID: 7, Selected: true, Title: "T", LaterEnabled: true})
	doc.MustNotHave("form[hx-post=/later/save]")
}

func TestLaterEnabledReadsTheShell(t *testing.T) {
	on := render.Shell{Apps: []render.NavItem{{ID: "reader"}, {ID: "later"}}}
	if !laterEnabled(on) {
		t.Error("laterEnabled = false with ON Later in the app list")
	}
	off := render.Shell{Apps: []render.NavItem{{ID: "reader"}}}
	if laterEnabled(off) {
		t.Error("laterEnabled = true without ON Later")
	}
	if got := viewArticle(Item{ID: 1}, on, listContext{}, false, time.Time{}).LaterEnabled; !got {
		t.Error("viewArticle did not set LaterEnabled from the shell")
	}
}
