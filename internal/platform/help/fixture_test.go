package help_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/docs"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/help"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

type session struct{ cookies []*http.Cookie }

// server is the real middleware stack over a real database file, with the
// help routes mounted exactly as buildStack mounts them and one ordinary
// user ("ilia") signed in.
type server struct {
	handler http.Handler
	user    *session
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
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	errs := web.NewErrors(rend, log)
	csrf := web.NewCSRF(false, errs)
	authn := web.NewAuth(web.AuthOptions{Users: users, Render: rend, Errors: errs, CSRF: csrf, Log: log, Version: "test"})

	pages, err := help.Load(docs.User())
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	authn.Routes(mux, nil)
	help.Routes(mux, nil, help.Deps{Pages: pages, Render: rend, Errors: errs, Version: "test"})
	mux.Handle("/", http.HandlerFunc(errs.NotFound))

	s := &server{handler: web.Stack(mux, log, errs, csrf, authn)}
	if _, err := users.CreateUser(ctx, "ilia", apptest.PasswordHash, false); err != nil {
		t.Fatal(err)
	}
	s.user = s.logIn(t, "ilia")
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
	sess := s.anonymous(t)
	form := url.Values{"username": {username}, "password": {apptest.Password}}
	for _, c := range sess.cookies {
		if c.Name == web.CSRFCookieName {
			form.Set(web.CSRFFormField, c.Value)
		}
	}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := s.do(t, sess, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login for %s = %d; body: %s", username, rec.Code, rec.Body.String())
	}
	return &session{cookies: rec.Result().Cookies()}
}

func (s *server) get(t *testing.T, sess *session, path string) *httptest.ResponseRecorder {
	t.Helper()
	return s.do(t, sess, httptest.NewRequest("GET", path, nil))
}

func doc(t *testing.T, rec *httptest.ResponseRecorder) *htmlassert.Doc {
	t.Helper()
	return htmlassert.Parse(t, rec.Body.String())
}
