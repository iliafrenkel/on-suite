package admin_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
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

// statApp is a stub app that implements Stater, so the apps section has
// something to render.
type statApp struct{}

func (statApp) Meta() app.Meta {
	return app.Meta{ID: "demo", Name: "ON Demo", Summary: "a stub", Order: 0}
}
func (statApp) Migrations() fs.FS { return fstest.MapFS{} }
func (statApp) Templates() fs.FS {
	return fstest.MapFS{"demo.html": &fstest.MapFile{Data: []byte(`{{define "content"}}x{{end}}`)}}
}
func (statApp) Mount(r *app.Router, d app.Deps) {
	r.HandleFunc("GET /{$}", func(http.ResponseWriter, *http.Request) {})
	r.PublicFunc("GET /s/{slug}", func(http.ResponseWriter, *http.Request) {})
}
func (statApp) Stats(context.Context, *sql.DB) ([]app.Stat, error) {
	return []app.Stat{{Label: "Widgets", Value: "42", Hint: "for testing"}}, nil
}

// brokenStatApp is a second stub app whose Stats always fails, to prove one
// app's failing collector does not hide another section entirely.
type brokenStatApp struct{ statApp }

func (brokenStatApp) Meta() app.Meta {
	return app.Meta{ID: "broken", Name: "ON Broken", Summary: "a stub", Order: 1}
}
func (brokenStatApp) Stats(context.Context, *sql.DB) ([]app.Stat, error) {
	return nil, errors.New("no such table: widgets")
}

type server struct {
	handler http.Handler
	admin   []*http.Cookie
	plain   []*http.Cookie
	cfg     config.Config
	rend    *render.Renderer
	errs    *web.Errors
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

	registry, err := app.NewRegistry(statApp{})
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
	rend, err := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL, CSRFFieldName: web.CSRFFormField})
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
	if err := registry.Mount(mux, app.Deps{
		DB: handle, Render: rend, Users: users, Errors: errs, Log: log,
	}, authn.RequireUser); err != nil {
		t.Fatal(err)
	}
	adminHandler := authn.RequireAdmin(admin.Handler(admin.Deps{
		Config: cfg, DB: handle, Users: users, Apps: registry,
		Jobs: reg, Routes: routes,
		Render: rend, Errors: errs, Nav: registry.NavItems(),
		Version: "v9.9.9", Started: time.Now().Add(-90 * time.Second),
	}))
	routes.Handle(mux, "GET /admin", false, adminHandler)
	routes.Handle(mux, "GET /admin/{$}", false, adminHandler)
	routes.Handle(mux, "/", true, http.HandlerFunc(errs.NotFound))

	s := &server{handler: web.Stack(mux, log, errs, csrf, authn), cfg: cfg, rend: rend, errs: errs}

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

// Without an explicit "GET /admin" registration, ServeMux would synthesize
// its own unguarded 307 redirect from "/admin" to "/admin/" for the
// no-trailing-slash subtree request, before RequireAdmin ever runs — letting
// a non-admin tell /admin apart from a genuine 404 by status code alone.
func TestANonAdminGetsExactlyTheSameResponseAtAdminWithoutSlashAsAMissingPage(t *testing.T) {
	s := newServer(t)
	admin := s.get(t, s.plain, "/admin")
	missing := s.get(t, s.plain, "/no-such-page")

	if admin.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", admin.Code)
	}
	if admin.Code == http.StatusTemporaryRedirect || admin.Code == http.StatusMovedPermanently {
		t.Fatalf("status = %d; /admin must not redirect, that reveals it exists", admin.Code)
	}
	if admin.Body.String() != missing.Body.String() {
		t.Error("/admin and a missing page render differently for a non-admin")
	}
}

// The admin route must be an exact match, not a subtree: "/admin/{$}" (not
// "/admin/"), so it does not serve the page at arbitrary nested paths.
func TestAdminSubpathIsNotServedByTheAdminRoute(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, s.admin, "/admin/some/subpath")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a subpath of /admin/", rec.Code)
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

// TestTheJobsSectionShowsOutcomesForJobsThatHaveRun exercises both branches
// of the outcome cell that a never-run job cannot reach: a successful run
// and a failed one. In particular it pins the template's
// "admin-tag-failed" class and "failed" text, which is the exact branch a
// review found the plan had specified incorrectly (as "admin-tag-public").
func TestTheJobsSectionShowsOutcomesForJobsThatHaveRun(t *testing.T) {
	ctx := context.Background()
	reg := jobs.NewRegistry()
	reg.Register("successful thing", "always works", time.Hour, func(context.Context) error { return nil })
	reg.Register("failed thing", "always fails", time.Hour, func(context.Context) error { return errors.New("boom") })
	reg.RunOnceForTest(ctx, "successful thing")
	reg.RunOnceForTest(ctx, "failed thing")

	s := newServerWith(t, reg)
	doc := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String())
	doc.MustHave("#jobs")

	body := doc.Text()
	for _, want := range []string{"successful thing", "failed thing", "boom"} {
		if !strings.Contains(body, want) {
			t.Errorf("the jobs section does not mention %q", want)
		}
	}

	ok := doc.MustHave(".admin-tag-ok")
	if got := htmlassert.Text(ok); got != "ok" {
		t.Errorf("successful job's outcome tag text = %q, want %q", got, "ok")
	}

	failed := doc.MustHave(".admin-tag-failed")
	if got := htmlassert.Text(failed); got != "failed" {
		t.Errorf("failed job's outcome tag text = %q, want %q", got, "failed")
	}
}

func TestTheAppsSectionShowsEachAppsNumbers(t *testing.T) {
	s := newServer(t)
	doc := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String())
	doc.MustHave("#apps")

	body := doc.Text()
	for _, want := range []string{"ON Demo", "Widgets", "42"} {
		if !strings.Contains(body, want) {
			t.Errorf("the apps section does not mention %q", want)
		}
	}
}

// TestTheAppsSectionShowsAFallbackWhenNoAppReportsStats exercises the outer
// {{else}} of the apps range, which none of the other apps tests reach
// because newServer always registers an app that implements Stater.
func TestTheAppsSectionShowsAFallbackWhenNoAppReportsStats(t *testing.T) {
	s := newServer(t)

	empty, err := app.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	admin.Handler(admin.Deps{
		Config:  s.cfg,
		Apps:    empty,
		Render:  s.rend,
		Errors:  s.errs,
		Version: "v9.9.9",
		Started: time.Now().UTC(),
	}).ServeHTTP(rec, httptest.NewRequest("GET", "/admin/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#apps")
	if !strings.Contains(doc.Text(), "No app in this build reports any statistics.") {
		t.Error("an empty apps list does not show the fallback message")
	}
}

func TestTheUsersSectionListsAccountsAndSessionCounts(t *testing.T) {
	s := newServer(t)
	doc := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String())
	doc.MustHave("#users")

	body := doc.Text()
	for _, want := range []string{"root", "ilia", "Administrator"} {
		if !strings.Contains(body, want) {
			t.Errorf("the users section does not mention %q", want)
		}
	}
}

// TestNonAdminAccountsAreMarkedAsUserNotAdministrator pins the IsAdmin=false
// branch of the role cell, which the happy-path test above cannot tell apart
// from the heading "Users & sessions" (which itself contains the substring
// "User"). It scopes the assertion to the account's own row.
func TestNonAdminAccountsAreMarkedAsUserNotAdministrator(t *testing.T) {
	s := newServer(t)
	doc := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String())

	var found bool
	for _, row := range doc.QueryAll("#users tbody tr") {
		text := htmlassert.Text(row)
		if !strings.Contains(text, "ilia") {
			continue
		}
		found = true
		if !strings.Contains(text, "User") {
			t.Errorf("ilia's row = %q, want it to say User", text)
		}
		if strings.Contains(text, "Administrator") {
			t.Errorf("ilia's row = %q, a non-admin must not say Administrator", text)
		}
	}
	if !found {
		t.Fatal("no row for ilia in the users table")
	}
}

// TestTheUsersSectionShowsNoAccountsRow exercises the accounts table's
// {{else}} branch, which no other test reaches because the harness always
// creates two users so it can log in as an admin.
func TestTheUsersSectionShowsNoAccountsRow(t *testing.T) {
	ctx := context.Background()
	s := newServer(t)

	dir := t.TempDir()
	cfg, err := config.Parse([]string{"-data-dir", dir}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := db.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, migrations); err != nil {
		t.Fatal(err)
	}
	users := auth.NewStore(handle)

	rec := httptest.NewRecorder()
	admin.Handler(admin.Deps{
		Config:  cfg,
		DB:      handle,
		Users:   users,
		Render:  s.rend,
		Errors:  s.errs,
		Version: "v9.9.9",
		Started: time.Now().UTC(),
	}).ServeHTTP(rec, httptest.NewRequest("GET", "/admin/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#users")
	if !strings.Contains(doc.Text(), "No accounts exist.") {
		t.Error("zero accounts does not show the empty-table message")
	}
}

// TestAccountsAndSessionsSectionsShowTheirOwnErrorsWhenTheStoreFails
// exercises AccountsErr and the SessionsErr branch reached through a real
// query failure rather than a nil *auth.Store, which
// TestABrokenCollectorRendersInItsOwnCardWithA200 exercises instead.
func TestAccountsAndSessionsSectionsShowTheirOwnErrorsWhenTheStoreFails(t *testing.T) {
	ctx := context.Background()
	s := newServer(t)

	dir := t.TempDir()
	cfg, err := config.Parse([]string{"-data-dir", dir}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := db.Open(cfg.DBPath())
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
	// Closing the handle makes every subsequent query on this store fail,
	// without the nil-store special case that sessionInfo already handles.
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	admin.Handler(admin.Deps{
		Config:  cfg,
		Users:   users,
		Render:  s.rend,
		Errors:  s.errs,
		Version: "v9.9.9",
		Started: time.Now().UTC(),
	}).ServeHTTP(rec, httptest.NewRequest("GET", "/admin/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#users")
	body := doc.Text()
	if !strings.Contains(body, "Could not list accounts") {
		t.Error("a failing ListAccounts is not reported in the users section")
	}
	if !strings.Contains(body, "Could not count sessions") {
		t.Error("a failing SessionCounts is not reported in the users section")
	}
}

// Two collectors fail at once and the page must still be a page: a broken
// PRAGMA must not hide the job status, and one app's bad query must not hide
// another section entirely.
func TestABrokenCollectorRendersInItsOwnCardWithA200(t *testing.T) {
	s := newServer(t)

	failing, err := app.NewRegistry(brokenStatApp{})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	admin.Handler(admin.Deps{
		Config:  s.cfg,
		DB:      nil,     // the database section cannot be collected at all
		Apps:    failing, // and this app's query fails
		Render:  s.rend,
		Errors:  s.errs,
		Version: "v9.9.9",
		Started: time.Now(),
	}).ServeHTTP(rec, httptest.NewRequest("GET", "/admin/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; a failing collector must not become a 500", rec.Code)
	}

	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#runtime")
	doc.MustHave("#database")
	doc.MustHave("#apps")

	body := doc.Text()
	if !strings.Contains(body, "Could not read the database") {
		t.Error("the database failure is not reported in its own section")
	}
	if !strings.Contains(body, "no such table: widgets") {
		t.Error("the failing app's error is not shown on that app's card")
	}
	if !strings.Contains(body, "v9.9.9") {
		t.Error("the healthy runtime section did not render alongside two broken ones")
	}
	// d.Users is unset here (nil), which sessionInfo treats as its own
	// error rather than a panic, and the accounts table falls back to its
	// empty-state row instead of disappearing.
	if !strings.Contains(body, "Could not count sessions") {
		t.Error("a nil user store's session error is not reported")
	}
	if !strings.Contains(body, "No accounts exist.") {
		t.Error("a nil user store should leave the accounts table in its empty state, not hide it")
	}
}

func TestTheRouteMapIncludesPlatformAndAppRoutesWithGuardStatus(t *testing.T) {
	s := newServer(t)
	doc := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String())
	doc.MustHave("#routes")

	body := doc.Text()
	// A map that quietly omitted the platform's own routes would be worse
	// than no map: /login and /static/ are the first two an auditor checks.
	for _, want := range []string{"GET /login", "GET /admin/", "GET /demo/{$}", "GET /demo/s/{slug}"} {
		if !strings.Contains(body, want) {
			t.Errorf("the route map does not list %q", want)
		}
	}

	// Public and guarded must be distinguishable, not just listed.
	public := doc.QueryAll(".admin-tag-public")
	if len(public) == 0 {
		t.Fatal("no route is marked public; the section cannot show default-deny working")
	}
}

func TestTheRouteMapCountsPublicRoutes(t *testing.T) {
	s := newServer(t)
	body := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String()).Text()
	if !strings.Contains(body, "reachable without signing in") {
		t.Error("the route map does not summarise how many routes are public")
	}
}

// routeRow returns the concatenated text of the single "#routes tbody tr"
// row whose text contains pattern, or fails the test. Rows are looked up by
// their rendered pattern text rather than a selector, since the table
// carries no per-row id.
func routeRow(t *testing.T, doc *htmlassert.Doc, pattern string) string {
	t.Helper()
	for _, tr := range doc.QueryAll("tbody tr") {
		text := htmlassert.Text(tr)
		if strings.Contains(text, pattern) {
			return text
		}
	}
	t.Fatalf("no row found for pattern %q", pattern)
	return ""
}

func TestTheRouteMapMarksOnlyPublicRoutesAsPublic(t *testing.T) {
	s := newServer(t)
	doc := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String())

	// GET /demo/s/{slug} is mounted with r.PublicFunc: public, so its row
	// must carry the "public" tag.
	public := routeRow(t, doc, "GET /demo/s/{slug}")
	if !strings.Contains(public, "public") {
		t.Errorf("public route row = %q; want it tagged public", public)
	}

	// GET /demo/{$} is mounted with r.HandleFunc: authenticated, so its row
	// must say "signed in" and must not carry the public tag's text.
	authenticated := routeRow(t, doc, "GET /demo/{$}")
	if !strings.Contains(authenticated, "signed in") {
		t.Errorf("authenticated route row = %q; want it marked signed in", authenticated)
	}
	if strings.Contains(authenticated, "public") {
		t.Errorf("authenticated route row = %q; must not be tagged public", authenticated)
	}
}

func TestTheRouteMapShowsPlatformAndAppOwners(t *testing.T) {
	s := newServer(t)
	doc := htmlassert.Parse(t, s.get(t, s.admin, "/admin/").Body.String())

	platform := routeRow(t, doc, "GET /login")
	if !strings.Contains(platform, web.PlatformOwner) {
		t.Errorf("platform route row = %q; want owner %q", platform, web.PlatformOwner)
	}

	appOwned := routeRow(t, doc, "GET /demo/{$}")
	if !strings.Contains(appOwned, "demo") {
		t.Errorf("app route row = %q; want owner %q", appOwned, "demo")
	}
}

func TestTheRouteMapShowsAnEmptyStateWithNoRoutesRecorded(t *testing.T) {
	s := newServer(t)

	rec := httptest.NewRecorder()
	admin.Handler(admin.Deps{
		Config:  s.cfg,
		Render:  s.rend,
		Errors:  s.errs,
		Version: "v9.9.9",
		Started: time.Now(),
		// Routes is left nil: Recorder.Routes on a nil receiver returns
		// nil, so the section must fall back to its empty-state row
		// instead of panicking or rendering an empty table silently.
	}).ServeHTTP(rec, httptest.NewRequest("GET", "/admin/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; a nil route recorder must not become a 500", rec.Code)
	}

	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("#routes")
	body := doc.Text()
	if !strings.Contains(body, "No routes recorded.") {
		t.Error("an empty route map does not show its empty-state row")
	}
	if !strings.Contains(body, "0 of 0") {
		t.Error("an empty route map does not summarise as 0 of 0 public routes")
	}
	if len(doc.QueryAll(".admin-tag-public")) != 0 {
		t.Error("an empty route map must not render any public tag")
	}
}
