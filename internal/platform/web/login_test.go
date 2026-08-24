package web_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

const testPassword = "a-sufficiently-long-password"

// authFixture builds the real stack over a real database with one account.
type authFixture struct {
	handler http.Handler
	auth    *web.Auth
	users   *auth.Store
	user    auth.User
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	ms, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(context.Background(), handle, ms); err != nil {
		t.Fatal(err)
	}

	users := auth.NewStore(handle)
	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	user, err := users.CreateUser(context.Background(), "ilia", hash, true)
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
	log := slog.New(slog.DiscardHandler)
	errs := web.NewErrors(rend, log)
	csrf := web.NewCSRF(false, errs)

	a := web.NewAuth(web.AuthOptions{
		Users: users, Render: rend, Errors: errs, CSRF: csrf, Log: log, Secure: false, Version: "test",
	})

	mux := http.NewServeMux()
	a.Routes(mux, nil)
	mux.Handle("GET /private", a.RequireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := web.UserFrom(r.Context())
		if !ok {
			t.Error("RequireUser admitted a request with no user in context")
		}
		_, _ = w.Write([]byte("private for " + u.Username))
	})))
	mux.Handle("GET /open", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := web.UserFrom(r.Context()); ok {
			_, _ = w.Write([]byte("open for " + u.Username))
			return
		}
		_, _ = w.Write([]byte("open for nobody"))
	}))

	handler := web.Chain(mux, a.LoadUser, csrf.Middleware)
	return &authFixture{handler: handler, auth: a, users: users, user: user}
}

// get issues a GET carrying the given cookies.
func (f *authFixture) do(t *testing.T, req *http.Request, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

// logIn performs a real login and returns the session and CSRF cookies.
func (f *authFixture) logIn(t *testing.T, username, password string) []*http.Cookie {
	t.Helper()

	page := f.do(t, httptest.NewRequest("GET", "/login", nil))
	csrfCookie := cookieFrom(t, page, web.CSRFCookieName)
	if csrfCookie == nil {
		t.Fatal("GET /login did not issue a CSRF cookie")
	}

	form := url.Values{
		"username":        {username},
		"password":        {password},
		web.CSRFFormField: {csrfCookie.Value},
	}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := f.do(t, req, csrfCookie)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /login = %d, want 303; body: %s", rec.Code, rec.Body.String())
	}
	return rec.Result().Cookies()
}

func TestLoginPageRenders(t *testing.T) {
	f := newAuthFixture(t)
	rec := f.do(t, httptest.NewRequest("GET", "/login", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(`input[name=username]`)
	doc.MustHave(`input[name=password]`)
	doc.MustHave(`input[name=` + web.CSRFFormField + `]`)
	// Nobody is logged in yet, so there must be no logout control.
	doc.MustNotHave(".shell-user")

	if typ, _ := htmlassert.Attr(doc.MustHave(`input[name=password]`), "type"); typ != "password" {
		t.Errorf("password field type = %q", typ)
	}
}

// TestLoginPageRespectsThemeCookie guards against the login page silently
// defaulting to light regardless of the visitor's chosen theme: it once
// built its render.Shell literal by hand and never read the theme cookie, so
// a dark-mode user got a white flash on every visit to /login.
func TestLoginPageRespectsThemeCookie(t *testing.T) {
	f := newAuthFixture(t)
	req := httptest.NewRequest("GET", "/login", nil)
	rec := f.do(t, req, &http.Cookie{Name: web.ThemeCookieName, Value: "dark"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	html := doc.MustHave("html")
	if theme, _ := htmlassert.Attr(html, "data-theme"); theme != "dark" {
		t.Errorf("<html> data-theme = %q, want %q", theme, "dark")
	}
}

func TestSuccessfulLoginSetsASessionAndGrantsAccess(t *testing.T) {
	f := newAuthFixture(t)
	cookies := f.logIn(t, "ilia", testPassword)

	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == web.SessionCookieName {
			session = c
		}
	}
	if session == nil {
		t.Fatal("login did not set a session cookie")
	}
	if !session.HttpOnly {
		t.Error("session cookie is not HttpOnly")
	}
	if session.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", session.SameSite)
	}
	if len(session.Value) < 20 {
		t.Errorf("session id %q is too short", session.Value)
	}

	rec := f.do(t, httptest.NewRequest("GET", "/private", nil), session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /private after login = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "private for ilia") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

// TestLoginRotatesTheCSRFToken guards against a token planted before sign-in.
func TestLoginRotatesTheCSRFToken(t *testing.T) {
	f := newAuthFixture(t)

	page := f.do(t, httptest.NewRequest("GET", "/login", nil))
	before := cookieFrom(t, page, web.CSRFCookieName).Value

	form := url.Values{
		"username": {"ilia"}, "password": {testPassword}, web.CSRFFormField: {before},
	}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := f.do(t, req, &http.Cookie{Name: web.CSRFCookieName, Value: before})

	after := cookieFrom(t, rec, web.CSRFCookieName)
	if after == nil {
		t.Fatal("login did not re-issue a CSRF cookie")
	}
	if after.Value == before {
		t.Error("the CSRF token survived login unchanged")
	}
}

func TestRequireUserRejectsAnonymous(t *testing.T) {
	f := newAuthFixture(t)
	rec := f.do(t, httptest.NewRequest("GET", "/private", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 to the login page", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/login") {
		t.Errorf("Location = %q, want /login...", loc)
	}
	if !strings.Contains(loc, "next=") {
		t.Errorf("Location = %q, does not remember where the user was going", loc)
	}
}

// TestRequireUserAnswersHTMXWithAStatusNotARedirect: HTMX would swap the
// login page into a fragment of the current page, which is useless.
func TestRequireUserRejectsAnonymousHTMXWithAHeader(t *testing.T) {
	f := newAuthFixture(t)
	req := httptest.NewRequest("GET", "/private", nil)
	req.Header.Set("HX-Request", "true")
	rec := f.do(t, req)

	if got := rec.Header().Get("HX-Redirect"); got == "" {
		t.Errorf("no HX-Redirect header; HTMX cannot act on a 303 body swap (status was %d)", rec.Code)
	}
}

// TestLoadUserDoesNotBlock: a public page must render for anonymous visitors
// and still know the user when there is one.
func TestLoadUserDoesNotBlock(t *testing.T) {
	f := newAuthFixture(t)

	rec := f.do(t, httptest.NewRequest("GET", "/open", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "open for nobody") {
		t.Fatalf("anonymous: %d %q", rec.Code, rec.Body.String())
	}

	cookies := f.logIn(t, "ilia", testPassword)
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == web.SessionCookieName {
			session = c
		}
	}
	rec = f.do(t, httptest.NewRequest("GET", "/open", nil), session)
	if !strings.Contains(rec.Body.String(), "open for ilia") {
		t.Errorf("logged in: %q", rec.Body.String())
	}
}

// TestFailedLoginIsIndistinguishable is the security property: an attacker
// must not be able to enumerate usernames.
func TestFailedLoginIsIndistinguishable(t *testing.T) {
	f := newAuthFixture(t)

	attempt := func(username, password string) (int, string) {
		page := f.do(t, httptest.NewRequest("GET", "/login", nil))
		csrfCookie := cookieFrom(t, page, web.CSRFCookieName)
		form := url.Values{
			"username": {username}, "password": {password},
			web.CSRFFormField: {csrfCookie.Value},
		}
		req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := f.do(t, req, csrfCookie)
		return rec.Code, htmlassert.Parse(t, rec.Body.String()).Text()
	}

	wrongPasswordCode, wrongPasswordText := attempt("ilia", "definitely-not-the-password")
	noSuchUserCode, noSuchUserText := attempt("nosuchperson", "definitely-not-the-password")

	if wrongPasswordCode != noSuchUserCode {
		t.Errorf("status differs: %d for a real user, %d for an unknown one",
			wrongPasswordCode, noSuchUserCode)
	}
	if wrongPasswordText != noSuchUserText {
		t.Errorf("wording differs and leaks whether an account exists:\n  real:    %q\n  unknown: %q",
			wrongPasswordText, noSuchUserText)
	}
	if wrongPasswordCode == http.StatusSeeOther {
		t.Error("a wrong password produced a redirect, meaning it succeeded")
	}
	if !strings.Contains(strings.ToLower(wrongPasswordText), "incorrect") {
		t.Errorf("the failure is not explained to the user: %q", wrongPasswordText)
	}
}

func TestLoginRejectsBadRequests(t *testing.T) {
	f := newAuthFixture(t)

	tests := []struct{ name, username, password string }{
		{"empty username", "", testPassword},
		{"empty password", "ilia", ""},
		{"both empty", "", ""},
		{"wrong case password", "ilia", strings.ToUpper(testPassword)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := f.do(t, httptest.NewRequest("GET", "/login", nil))
			csrfCookie := cookieFrom(t, page, web.CSRFCookieName)
			form := url.Values{
				"username": {tt.username}, "password": {tt.password},
				web.CSRFFormField: {csrfCookie.Value},
			}
			req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := f.do(t, req, csrfCookie)

			if rec.Code == http.StatusSeeOther {
				t.Error("login succeeded")
			}
			for _, c := range rec.Result().Cookies() {
				if c.Name == web.SessionCookieName && c.Value != "" {
					t.Error("a session cookie was issued for a failed login")
				}
			}
		})
	}
}

// TestUsernameIsCaseInsensitiveAtLogin: Plan 1 folded usernames for
// uniqueness; the login path must use the same folding.
func TestUsernameIsCaseInsensitiveAtLogin(t *testing.T) {
	f := newAuthFixture(t)
	for _, name := range []string{"ilia", "Ilia", "ILIA"} {
		cookies := f.logIn(t, name, testPassword)
		found := false
		for _, c := range cookies {
			if c.Name == web.SessionCookieName {
				found = true
			}
		}
		if !found {
			t.Errorf("login as %q did not produce a session", name)
		}
	}
}

func TestLogoutRevokesTheSession(t *testing.T) {
	f := newAuthFixture(t)
	cookies := f.logIn(t, "ilia", testPassword)

	var session, csrfCookie *http.Cookie
	for _, c := range cookies {
		switch c.Name {
		case web.SessionCookieName:
			session = c
		case web.CSRFCookieName:
			csrfCookie = c
		}
	}

	form := url.Values{web.CSRFFormField: {csrfCookie.Value}}
	req := httptest.NewRequest("POST", "/logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := f.do(t, req, session, csrfCookie)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /logout = %d, want 303", rec.Code)
	}

	// The old cookie must be dead server-side, not merely cleared in the
	// browser: a stolen cookie has to stop working.
	rec = f.do(t, httptest.NewRequest("GET", "/private", nil), session)
	if rec.Code == http.StatusOK {
		t.Error("the session still works after logout")
	}
}

func TestLogoutRequiresCSRF(t *testing.T) {
	f := newAuthFixture(t)
	cookies := f.logIn(t, "ilia", testPassword)
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == web.SessionCookieName {
			session = c
		}
	}

	rec := f.do(t, httptest.NewRequest("POST", "/logout", nil), session)
	if rec.Code != http.StatusForbidden {
		t.Errorf("logout without a CSRF token = %d, want 403", rec.Code)
	}
}

func TestTamperedSessionCookieIsRejected(t *testing.T) {
	f := newAuthFixture(t)
	cookies := f.logIn(t, "ilia", testPassword)
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == web.SessionCookieName {
			session = c
		}
	}

	tampered := &http.Cookie{Name: web.SessionCookieName, Value: session.Value[:len(session.Value)-2] + "xy"}
	rec := f.do(t, httptest.NewRequest("GET", "/private", nil), tampered)
	if rec.Code == http.StatusOK {
		t.Error("a tampered session cookie was accepted")
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	f := newAuthFixture(t)
	cookies := f.logIn(t, "ilia", testPassword)
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == web.SessionCookieName {
			session = c
		}
	}

	// Move the whole stack past the session lifetime.
	future := time.Now().UTC().Add(auth.SessionTTL + time.Hour)
	f.users.SetClock(func() time.Time { return future })
	f.auth.SetClock(func() time.Time { return future })

	rec := f.do(t, httptest.NewRequest("GET", "/private", nil), session)
	if rec.Code == http.StatusOK {
		t.Error("an expired session was accepted")
	}
}

// TestLoginRateLimit: repeated failures must stop being answered, or a weak
// password is brute-forceable at HTTP speed.
func TestLoginRateLimit(t *testing.T) {
	f := newAuthFixture(t)

	var lastCode int
	for i := range 25 {
		page := f.do(t, httptest.NewRequest("GET", "/login", nil))
		csrfCookie := cookieFrom(t, page, web.CSRFCookieName)
		form := url.Values{
			"username": {"ilia"}, "password": {"wrong-password-attempt"},
			web.CSRFFormField: {csrfCookie.Value},
		}
		req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := f.do(t, req, csrfCookie)
		lastCode = rec.Code
		if lastCode == http.StatusTooManyRequests {
			break
		}
		if i == 24 {
			t.Fatalf("25 failed attempts were all answered normally (last status %d)", lastCode)
		}
	}

	// While limited, even the correct password must be refused.
	page := f.do(t, httptest.NewRequest("GET", "/login", nil))
	csrfCookie := cookieFrom(t, page, web.CSRFCookieName)
	form := url.Values{
		"username": {"ilia"}, "password": {testPassword},
		web.CSRFFormField: {csrfCookie.Value},
	}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := f.do(t, req, csrfCookie)
	if rec.Code == http.StatusSeeOther {
		t.Error("the rate limit was bypassed by supplying the correct password")
	}
}

// TestLoginNextParameterCannotBecomeAnOpenRedirect.
func TestLoginNextIsRestrictedToLocalPaths(t *testing.T) {
	f := newAuthFixture(t)

	tests := []struct{ name, next, wantPrefix string }{
		{"local path is honoured", "/private", "/private"},
		{"absolute url is refused", "https://evil.example.com/x", "/"},
		{"scheme-relative url is refused", "//evil.example.com/x", "/"},
		{"backslash trick is refused", "/\\evil.example.com", "/"},
		{"non-path is refused", "javascript:alert(1)", "/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := f.do(t, httptest.NewRequest("GET", "/login", nil))
			csrfCookie := cookieFrom(t, page, web.CSRFCookieName)
			form := url.Values{
				"username": {"ilia"}, "password": {testPassword},
				web.CSRFFormField: {csrfCookie.Value}, "next": {tt.next},
			}
			req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := f.do(t, req, csrfCookie)

			loc := rec.Header().Get("Location")
			if !strings.HasPrefix(loc, tt.wantPrefix) {
				t.Errorf("Location = %q, want prefix %q", loc, tt.wantPrefix)
			}
			if strings.Contains(loc, "evil.example.com") {
				t.Errorf("open redirect: Location = %q", loc)
			}
		})
	}
}
