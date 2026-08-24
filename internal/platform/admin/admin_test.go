package admin_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/admin"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

const testPassword = "a-sufficiently-long-password"

type server struct {
	handler http.Handler
	admin   []*http.Cookie
	plain   []*http.Cookie
}

func newServer(t *testing.T) *server {
	t.Helper()
	return newServerWith(t, jobs.NewRegistry())
}

// newServerWithJobs sets up a registry with jobs whose state the jobs
// section can render: one enabled and never run, one disabled.
func newServerWithJobs(t *testing.T) *server {
	t.Helper()
	reg := jobs.NewRegistry()
	reg.Register("nightly thing", "does a nightly thing", time.Hour, func(context.Context) error { return nil })
	reg.Register("disabled thing", "would do a thing", 0, func(context.Context) error { return nil })
	return newServerWith(t, reg)
}

func newServerWith(t *testing.T, reg *jobs.Registry) *server {
	t.Helper()
	ctx := context.Background()

	dir := t.TempDir()
	// Parse a real Config so the settings section has real provenance, and
	// open the database where that Config says it lives, so the database
	// section reports real file sizes.
	cfg, err := config.Parse([]string{"-data-dir", dir}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := db.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	registry, err := app.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, migrations); err != nil {
		t.Fatal(err)
	}

	users := auth.NewStore(handle)
	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatal(err)
	}
	rend, err := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL})
	if err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.DiscardHandler)
	errs := web.NewErrors(rend, log)
	csrf := web.NewCSRF(false, errs)
	authn := web.NewAuth(web.AuthOptions{
		Users: users, Render: rend, Errors: errs, CSRF: csrf, Log: log, Secure: false,
	})

	routes := web.NewRecorder()
	registry.RecordRoutes(routes)

	mux := http.NewServeMux()
	authn.Routes(mux, routes)
	routes.Handle(mux, "GET /admin/", false, authn.RequireAdmin(admin.Handler(admin.Deps{
		Config: cfg, DB: handle, Users: users, Apps: registry,
		Jobs: reg, Routes: routes,
		Render: rend, Errors: errs, Nav: registry.NavItems(),
		Version: "v9.9.9", Started: time.Now().Add(-90 * time.Second),
	})))
	routes.Handle(mux, "/", true, http.HandlerFunc(errs.NotFound))

	s := &server{handler: web.Stack(mux, log, errs, csrf, authn)}

	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	root, err := users.CreateUser(ctx, "root", hash, true)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := users.CreateUser(ctx, "ilia", hash, false)
	if err != nil {
		t.Fatal(err)
	}
	s.admin = s.logIn(t, root)
	s.plain = s.logIn(t, plain)
	return s
}

// logIn performs a real login, so the tests carry genuine session cookies.
func (s *server) logIn(t *testing.T, u auth.User) []*http.Cookie {
	t.Helper()

	// A GET first, to be issued a CSRF cookie and token.
	warm := httptest.NewRecorder()
	s.handler.ServeHTTP(warm, httptest.NewRequest("GET", "/login", nil))
	var token string
	for _, c := range warm.Result().Cookies() {
		if c.Name == web.CSRFCookieName {
			token = c.Value
		}
	}

	form := url.Values{"username": {u.Username}, "password": {testPassword}, web.CSRFFormField: {token}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range warm.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logging in %s: status = %d, body = %s", u.Username, rec.Code, rec.Body.String())
	}
	return append(warm.Result().Cookies(), rec.Result().Cookies()...)
}

func (s *server) get(t *testing.T, cookies []*http.Cookie, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

func TestAnonymousIsSentToLogin(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, nil, "/admin/")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); !strings.HasPrefix(got, "/login") {
		t.Errorf("Location = %q", got)
	}
}

// The page must not confirm its own existence to someone who may not see it.
func TestANonAdminGetsExactlyTheSameResponseAsAMissingPage(t *testing.T) {
	s := newServer(t)
	admin := s.get(t, s.plain, "/admin/")
	missing := s.get(t, s.plain, "/no-such-page")

	if admin.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", admin.Code)
	}
	if admin.Body.String() != missing.Body.String() {
		t.Error("/admin/ and a missing page render differently for a non-admin")
	}
}

func TestTheAdminPageShowsBuildAndDatabaseSections(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, s.admin, "/admin/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#runtime")
	doc.MustHave("#database")

	body := doc.Text()
	for _, want := range []string{"v9.9.9", "wal", "platform:0001"} {
		if !strings.Contains(strings.ToLower(body), strings.ToLower(want)) {
			t.Errorf("the page does not mention %q", want)
		}
	}
}

func TestTheSidebarLinksAdminsToTheAdminPageAndNobodyElse(t *testing.T) {
	s := newServer(t)

	adminPage := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String())
	link := adminPage.MustHave(`nav.shell-nav a[href="/admin/"]`)

	// The admin link must be marked current when the admin page itself is
	// rendering, exactly like every other sidebar item is when its app is
	// active.
	if got, ok := htmlassert.Attr(link, "aria-current"); !ok || got != "page" {
		t.Errorf(`admin sidebar link aria-current = %q, %v, want "page", true`, got, ok)
	}

	// A non-admin's own pages must not advertise it either.
	plainPage := htmlassert.Parse(t, s.get(t, s.plain, "/login").Body.String())
	plainPage.MustNotHave(`a[href="/admin/"]`)
}

func TestTheSettingsSectionShowsValuesDefaultsAndSources(t *testing.T) {
	s := newServer(t)
	doc := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String())
	doc.MustHave("#settings")

	body := doc.Text()
	for _, want := range []string{"backup-interval", "ONSUITE_BACKUP_INTERVAL", "24h0m0s", "default"} {
		if !strings.Contains(body, want) {
			t.Errorf("the settings section does not mention %q", want)
		}
	}
	// The usage string is the "docs" half of the section.
	if !strings.Contains(body, "how often to snapshot the database") {
		t.Error("the settings section shows no documentation for a setting")
	}
}

func TestTheJobsSectionListsRegisteredJobsAndTheirState(t *testing.T) {
	s := newServerWithJobs(t)
	doc := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String())
	doc.MustHave("#jobs")

	body := doc.Text()
	for _, want := range []string{"nightly thing", "disabled thing", "never"} {
		if !strings.Contains(body, want) {
			t.Errorf("the jobs section does not mention %q", want)
		}
	}
}
