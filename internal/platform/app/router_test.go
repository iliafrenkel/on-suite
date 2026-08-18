package app_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

// denyGuard stands in for RequireUser: it blocks unless a header is present,
// so tests can tell guarded routes from public ones without a database.
func denyGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Signed-In") != "yes" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// mountFake builds a mux with one app mounted through the real registry.
func mountFake(t *testing.T, mount func(*app.Router, app.Deps)) *http.ServeMux {
	t.Helper()

	f := newFake("paste", "ON Paste", 0)
	f.mount = mount
	reg, err := app.NewRegistry(f)
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

	mux := http.NewServeMux()
	if err := reg.Mount(mux, app.Deps{Render: rend}, denyGuard); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	return mux
}

// TestRouterRequiresAuthByDefault is the reason this router exists rather than
// a list of public routes: doing nothing must produce a protected route.
func TestRouterRequiresAuthByDefault(t *testing.T) {
	mux := mountFake(t, func(r *app.Router, d app.Deps) {
		r.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {
			_, _ = w.Write([]byte("index"))
		})
		r.HandleFunc("POST /new", func(w http.ResponseWriter, req *http.Request) {
			_, _ = w.Write([]byte("created"))
		})
	})

	for _, tc := range []struct{ method, path string }{{"GET", "/paste/"}, {"POST", "/paste/new"}} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s anonymous = %d, want 401", tc.method, tc.path, rec.Code)
		}

		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("X-Signed-In", "yes")
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s %s signed in = %d, want 200", tc.method, tc.path, rec.Code)
		}
	}
}

func TestRouterPublicRoutesAreAnonymous(t *testing.T) {
	mux := mountFake(t, func(r *app.Router, d app.Deps) {
		r.PublicFunc("GET /s/{slug}", func(w http.ResponseWriter, req *http.Request) {
			_, _ = w.Write([]byte("slug=" + req.PathValue("slug")))
		})
		r.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {})
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/paste/s/abc123", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("public route anonymous = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "slug=abc123" {
		t.Errorf("body = %q; the path wildcard did not survive prefixing", got)
	}
}

func TestRouterPrefixesPatterns(t *testing.T) {
	var recorded []app.Route
	mountFake(t, func(r *app.Router, d app.Deps) {
		r.HandleFunc("GET /{$}", func(http.ResponseWriter, *http.Request) {})
		r.HandleFunc("GET /new", func(http.ResponseWriter, *http.Request) {})
		r.PublicFunc("GET /s/{slug}", func(http.ResponseWriter, *http.Request) {})
		recorded = r.Routes()
	})

	want := []app.Route{
		{Pattern: "GET /paste/{$}", Public: false},
		{Pattern: "GET /paste/new", Public: false},
		{Pattern: "GET /paste/s/{slug}", Public: true},
	}
	if len(recorded) != len(want) {
		t.Fatalf("recorded %d routes, want %d: %+v", len(recorded), len(want), recorded)
	}
	for i := range want {
		if recorded[i] != want[i] {
			t.Errorf("route %d = %+v, want %+v", i, recorded[i], want[i])
		}
	}
}

// TestRouterSetsTheActiveApp lets the shell mark the nav without every handler
// remembering to.
func TestRouterSetsTheActiveApp(t *testing.T) {
	mux := mountFake(t, func(r *app.Router, d app.Deps) {
		r.PublicFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {
			_, _ = w.Write([]byte("active=" + web.ActiveApp(req.Context())))
		})
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/paste/", nil))
	if got := rec.Body.String(); got != "active=paste" {
		t.Errorf("body = %q, want active=paste", got)
	}
}

func TestRouterRejectsMalformedPatterns(t *testing.T) {
	for _, pattern := range []string{"GET no-leading-slash", "relative", "GET //double"} {
		t.Run(pattern, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("pattern %q was accepted; want a panic at startup", pattern)
				}
			}()
			mountFake(t, func(r *app.Router, d app.Deps) {
				r.HandleFunc(pattern, func(http.ResponseWriter, *http.Request) {})
			})
		})
	}
}

// TestMountRejectsAnAppWithNoRoutes catches an app that forgot to register
// anything, which would otherwise appear in the nav and 404 when clicked.
func TestMountRejectsAnAppWithNoRoutes(t *testing.T) {
	f := newFake("paste", "ON Paste", 0)
	f.mount = func(*app.Router, app.Deps) {}
	reg, err := app.NewRegistry(f)
	if err != nil {
		t.Fatal(err)
	}

	assets, _ := web.NewAssets(ui.Static(), "/static")
	rend, _ := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL})

	err = reg.Mount(http.NewServeMux(), app.Deps{Render: rend}, denyGuard)
	if err == nil || !strings.Contains(err.Error(), "no routes") {
		t.Errorf("Mount error = %v, want a complaint about no routes", err)
	}
}
