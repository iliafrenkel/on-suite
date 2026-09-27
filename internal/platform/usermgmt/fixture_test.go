package usermgmt_test

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/usermgmt"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

// session is one signed-in browser.
type session struct {
	user    auth.User
	cookies []*http.Cookie
}

// server is the real middleware stack over a real database file, with the
// user-management routes mounted exactly as buildStack mounts them, plus
// one admin ("root") and one ordinary user ("ilia").
type server struct {
	handler http.Handler
	users   *auth.Store
	db      *sql.DB
	logs    *bytes.Buffer
	// now is the clock usermgmt reads; advance it to age out rate-limited
	// attempts.
	now   time.Time
	root  *session
	plain *session
}

func newServer(t *testing.T) *server {
	t.Helper()
	ctx := context.Background()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	ms, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, ms); err != nil {
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
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logs, nil))
	errs := web.NewErrors(rend, log)
	csrf := web.NewCSRF(false, errs)
	authn := web.NewAuth(web.AuthOptions{Users: users, Render: rend, Errors: errs, CSRF: csrf, Log: log, Version: "test"})

	s := &server{users: users, db: handle, logs: logs, now: time.Now().UTC()}

	mux := http.NewServeMux()
	authn.Routes(mux, nil)
	usermgmt.Routes(mux, nil, authn, usermgmt.Deps{
		Users: users, Render: rend, Errors: errs, Log: log, Version: "test",
		Now: func() time.Time { return s.now },
	})
	mux.Handle("/", http.HandlerFunc(errs.NotFound))
	s.handler = web.Stack(mux, log, errs, csrf, authn)

	if _, err := users.CreateUser(ctx, "root", apptest.PasswordHash, true); err != nil {
		t.Fatal(err)
	}
	if _, err := users.CreateUser(ctx, "ilia", apptest.PasswordHash, false); err != nil {
		t.Fatal(err)
	}
	s.root = s.logIn(t, "root", apptest.Password)
	s.plain = s.logIn(t, "ilia", apptest.Password)
	return s
}

func (s *server) do(t *testing.T, sess *session, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	if sess != nil {
		for _, c := range sess.cookies {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

// anonymous returns a browser holding only a CSRF cookie.
func (s *server) anonymous(t *testing.T) *session {
	t.Helper()
	page := s.do(t, nil, httptest.NewRequest("GET", "/login", nil))
	for _, c := range page.Result().Cookies() {
		if c.Name == web.CSRFCookieName {
			return &session{cookies: []*http.Cookie{c}}
		}
	}
	t.Fatal("GET /login issued no CSRF cookie")
	return nil
}

// tryLogIn submits the login form and returns the raw response.
func (s *server) tryLogIn(t *testing.T, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	anon := s.anonymous(t)
	form := url.Values{"username": {username}, "password": {password}}
	return s.post(t, anon, "/login", form)
}

// logIn signs in and fails the test unless it worked.
func (s *server) logIn(t *testing.T, username, password string) *session {
	t.Helper()
	rec := s.tryLogIn(t, username, password)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login for %s = %d; body: %s", username, rec.Code, rec.Body.String())
	}
	u, err := s.users.UserByUsername(context.Background(), username)
	if err != nil {
		t.Fatal(err)
	}
	return &session{user: u, cookies: rec.Result().Cookies()}
}

func (s *server) get(t *testing.T, sess *session, path string) *httptest.ResponseRecorder {
	t.Helper()
	return s.do(t, sess, httptest.NewRequest("GET", path, nil))
}

// post submits a form carrying the session's own CSRF token.
func (s *server) post(t *testing.T, sess *session, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	for _, c := range sess.cookies {
		if c.Name == web.CSRFCookieName {
			form.Set(web.CSRFFormField, c.Value)
		}
	}
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.do(t, sess, req)
}

func (s *server) doc(t *testing.T, rec *httptest.ResponseRecorder) *htmlassert.Doc {
	t.Helper()
	return htmlassert.Parse(t, rec.Body.String())
}

func (s *server) user(t *testing.T, name string) auth.User {
	t.Helper()
	u, err := s.users.UserByUsername(context.Background(), name)
	if err != nil {
		t.Fatalf("UserByUsername(%s): %v", name, err)
	}
	return u
}
