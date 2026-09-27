package jobsadmin_test

import (
	"bytes"
	"context"
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
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
	"github.com/iliafrenkel/on-suite/internal/platform/jobsadmin"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

type session struct{ cookies []*http.Cookie }

// server is the real middleware stack over a real database file, with the
// jobs routes mounted exactly as buildStack mounts them, one admin ("root")
// and one ordinary user ("ilia"), and a registry the test fills in.
type server struct {
	handler http.Handler
	users   *auth.Store
	jobs    *jobs.Registry
	logs    *bytes.Buffer
	root    *session
	plain   *session
}

// newServer builds the stack around reg, which the caller has already
// registered its jobs on.
func newServer(t *testing.T, reg *jobs.Registry) *server {
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

	mux := http.NewServeMux()
	authn.Routes(mux, nil)
	jobsadmin.Routes(mux, nil, authn, jobsadmin.Deps{
		Jobs: reg, Render: rend, Errors: errs, Log: log, Version: "test",
	})
	mux.Handle("/", http.HandlerFunc(errs.NotFound))

	s := &server{handler: web.Stack(mux, log, errs, csrf, authn), users: users, jobs: reg, logs: logs}
	if _, err := users.CreateUser(ctx, "root", apptest.PasswordHash, true); err != nil {
		t.Fatal(err)
	}
	if _, err := users.CreateUser(ctx, "ilia", apptest.PasswordHash, false); err != nil {
		t.Fatal(err)
	}
	s.root = s.logIn(t, "root")
	s.plain = s.logIn(t, "ilia")
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

func (s *server) logIn(t *testing.T, username string) *session {
	t.Helper()
	rec := s.post(t, s.anonymous(t), "/login", url.Values{"username": {username}, "password": {apptest.Password}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login for %s = %d; body: %s", username, rec.Code, rec.Body.String())
	}
	return &session{cookies: rec.Result().Cookies()}
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

func doc(t *testing.T, rec *httptest.ResponseRecorder) *htmlassert.Doc {
	t.Helper()
	return htmlassert.Parse(t, rec.Body.String())
}

// blockingJob registers a job that runs until release is closed.
func blockingJob(reg *jobs.Registry, name string) (release chan struct{}) {
	release = make(chan struct{})
	reg.Register(name, "waits to be released", time.Hour, func(context.Context) error {
		<-release
		return nil
	})
	return release
}
