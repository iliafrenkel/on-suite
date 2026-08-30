// Package apptest is the shared test-server harness every app's handler
// tests build on: a real signed-in stack over a real database file, plus the
// small set of request-issuing helpers every one of them ends up writing by
// hand otherwise.
//
// It is a normal (non-_test.go) package, like internal/htmlassert, so more
// than one app's test package can import it — but it is still test-only:
// TestAppTestIsTestOnly in internal/arch/arch_test.go is what enforces that,
// the same way TestHTMLAssertIsTestOnly does for internal/htmlassert. Import
// it from a _test.go file only, never from production code.
//
// It does not know any one app's own store methods (creating a snippet, a
// bullet, and so on) — those stay in each app's own test package, alongside
// the rest of what only that app's tests need. What lives here is the part
// that was, before issue #50, an almost line-for-line duplicate between
// internal/apps/notes and internal/apps/paste: standing up the stack,
// signing in, and issuing a request with a session's cookies and CSRF token
// attached.
package apptest

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

// Password is every fixture user's password — long enough to pass the
// platform's own minimum, and the same for every account so tests never have
// to thread a per-user secret through anything.
const Password = "a-sufficiently-long-password"

// Session holds one signed-in browser's cookies.
type Session struct {
	User    auth.User
	Cookies []*http.Cookie
}

// Server is the whole stack over a real database file, with two accounts —
// Alice and Bob, so every owner-scoping test has somebody else to be
// confused with. S is the app-specific store type (e.g. *notes.Store,
// *paste.Store): the harness has no use for it beyond handing it back to the
// caller, since every store method belongs to one app alone.
//
// A real file, not ":memory:", for the reason given in notes' own N1 store
// tests: the bugs worth catching live in SQLite's own behaviour.
type Server[S any] struct {
	Handler http.Handler
	Store   S
	Alice   *Session
	Bob     *Session
}

// Option configures a detail of NewServer beyond its required arguments.
type Option func(*config)

type config struct {
	secure bool
}

// WithSecureCookies makes the stack behave as it would behind a real TLS
// deployment (config.Config.SecureCookies) — every cookie the platform sets,
// and app.Deps.Secure for an app's own (issue #79), is marked Secure. Omit
// this (the default) to match a plain-HTTP dev server instead, which is
// what every other test in this harness wants.
func WithSecureCookies() Option {
	return func(c *config) { c.secure = true }
}

// NewServer builds a Server for one app. newStore turns the opened database
// handle into that app's own store type; the harness calls it once, after
// migrations have run, and stores the result on Store.
func NewServer[S any](t *testing.T, a app.App, newStore func(*sql.DB) S, opts ...Option) *Server[S] {
	t.Helper()
	ctx := context.Background()

	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	registry, err := app.NewRegistry(a)
	if err != nil {
		t.Fatal(err)
	}

	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	appMigrations, err := registry.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(migrations, appMigrations...)); err != nil {
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
	csrf := web.NewCSRF(cfg.secure, errs)
	authn := web.NewAuth(web.AuthOptions{
		Users: users, Render: rend, Errors: errs, CSRF: csrf, Log: log, Secure: cfg.secure,
	})

	mux := http.NewServeMux()
	authn.Routes(mux, nil)
	if err := registry.Mount(mux, app.Deps{
		DB: handle, Render: rend, Users: users, Errors: errs, Log: log, Secure: cfg.secure,
	}, authn.RequireUser); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	mux.Handle("/", http.HandlerFunc(errs.NotFound))

	s := &Server[S]{
		Handler: web.Stack(mux, log, errs, csrf, authn),
		Store:   newStore(handle),
	}

	hash, err := auth.HashPassword(Password)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alice", "bob"} {
		u, err := users.CreateUser(ctx, name, hash, false)
		if err != nil {
			t.Fatal(err)
		}
		sess := s.LogIn(t, u)
		if name == "alice" {
			s.Alice = sess
		} else {
			s.Bob = sess
		}
	}
	return s
}

// Do issues a request carrying a session's cookies.
func (s *Server[S]) Do(t *testing.T, sess *Session, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	if sess != nil {
		for _, c := range sess.Cookies {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	s.Handler.ServeHTTP(rec, req)
	return rec
}

// Get fetches a page and fails the test unless it is a 200.
func (s *Server[S]) Get(t *testing.T, sess *Session, path string) *htmlassert.Doc {
	t.Helper()
	rec := s.Do(t, sess, httptest.NewRequest("GET", path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200; body: %s", path, rec.Code, rec.Body.String())
	}
	return htmlassert.Parse(t, rec.Body.String())
}

// Post submits a form with the session's CSRF token attached.
func (s *Server[S]) Post(t *testing.T, sess *Session, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set(web.CSRFFormField, s.CSRFToken(t, sess))
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.Do(t, sess, req)
}

// Submit posts a form and asserts it redirected to wantLocation — the shape
// almost every structural write test takes: "did it come back to the page I
// was looking at" is half of what each one checks.
func (s *Server[S]) Submit(t *testing.T, sess *Session, path string, form url.Values, wantLocation string) {
	t.Helper()
	rec := s.Post(t, sess, path, form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d, want 303; body: %s", path, rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != wantLocation {
		t.Fatalf("POST %s redirected to %q, want %q", path, got, wantLocation)
	}
}

// PostHX submits a form the way an app's own front-end JS always will: HTMX
// itself sets this header on every request, and the platform's CSRF check
// already accepts it in place of the hidden form field.
func (s *Server[S]) PostHX(t *testing.T, sess *Session, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set(web.CSRFFormField, s.CSRFToken(t, sess))
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	return s.Do(t, sess, req)
}

// CSRFToken reads a session's own CSRF cookie value back out, for a caller
// that needs to attach it to a request Post/Submit/PostHX doesn't build.
func (s *Server[S]) CSRFToken(t *testing.T, sess *Session) string {
	t.Helper()
	for _, c := range sess.Cookies {
		if c.Name == web.CSRFCookieName {
			return c.Value
		}
	}
	t.Fatal("session has no CSRF cookie")
	return ""
}

// LogIn performs a real login so the tests use genuine session cookies.
func (s *Server[S]) LogIn(t *testing.T, u auth.User) *Session {
	t.Helper()

	page := s.Do(t, nil, httptest.NewRequest("GET", "/login", nil))
	var csrfCookie *http.Cookie
	for _, c := range page.Result().Cookies() {
		if c.Name == web.CSRFCookieName {
			csrfCookie = c
		}
	}
	if csrfCookie == nil {
		t.Fatal("GET /login issued no CSRF cookie")
	}

	form := url.Values{
		"username":        {u.Username},
		"password":        {Password},
		web.CSRFFormField: {csrfCookie.Value},
	}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := s.Do(t, &Session{Cookies: []*http.Cookie{csrfCookie}}, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login for %s = %d", u.Username, rec.Code)
	}
	return &Session{User: u, Cookies: rec.Result().Cookies()}
}

// Anonymous returns a session holding a valid CSRF cookie and no login
// cookie. It is how a test gets past the CSRF middleware in order to assert
// on the auth guard behind it, which is otherwise unreachable: a tokenless
// POST never gets that far.
func (s *Server[S]) Anonymous(t *testing.T) *Session {
	t.Helper()

	page := s.Do(t, nil, httptest.NewRequest("GET", "/login", nil))
	for _, c := range page.Result().Cookies() {
		if c.Name == web.CSRFCookieName {
			return &Session{Cookies: []*http.Cookie{c}}
		}
	}
	t.Fatal("GET /login issued no CSRF cookie")
	return nil
}
