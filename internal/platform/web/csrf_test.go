package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// csrfStack wraps a handler that reports the token it saw in context.
func csrfStack(t *testing.T, secure bool) (http.Handler, *web.CSRF) {
	t.Helper()
	e, _ := testErrors(t)
	c := web.NewCSRF(secure, e)
	h := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("token=" + web.CSRFToken(r.Context())))
	}))
	return h, c
}

// cookieFrom pulls a Set-Cookie value out of a response.
func cookieFrom(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestCSRFIssuesATokenOnFirstGET(t *testing.T) {
	h, _ := csrfStack(t, true)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	c := cookieFrom(t, rec, web.CSRFCookieName)
	if c == nil {
		t.Fatal("no CSRF cookie was set")
	}
	if len(c.Value) < 20 {
		t.Errorf("token %q is too short to be random", c.Value)
	}
	if !c.HttpOnly {
		t.Error("CSRF cookie is not HttpOnly")
	}
	if !c.Secure {
		t.Error("CSRF cookie is not Secure when secure=true")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	// The handler must see the same token, so it can render it into the page.
	if !strings.Contains(rec.Body.String(), "token="+c.Value) {
		t.Errorf("handler saw %q, cookie is %q", rec.Body.String(), c.Value)
	}
}

func TestCSRFReusesAnExistingToken(t *testing.T) {
	h, _ := csrfStack(t, false)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	first := cookieFrom(t, rec, web.CSRFCookieName)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: first.Value})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "token="+first.Value) {
		t.Error("an existing token was not reused")
	}
	if c := cookieFrom(t, rec, web.CSRFCookieName); c != nil && c.Value != first.Value {
		t.Error("a new token was issued despite a valid one being present")
	}
}

func TestCSRFAllowsSafeMethodsWithoutAToken(t *testing.T) {
	h, _ := csrfStack(t, false)
	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", method, rec.Code)
		}
	}
}

// TestCSRFRejectsUnsafeMethodsWithoutAToken is the whole point of the task.
func TestCSRFRejectsUnsafeMethodsWithoutAToken(t *testing.T) {
	h, _ := csrfStack(t, false)
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/", nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s without a token = %d, want 403", method, rec.Code)
		}
	}
}

func TestCSRFAcceptsTheTokenInAHeader(t *testing.T) {
	h, _ := csrfStack(t, false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	token := cookieFrom(t, rec, web.CSRFCookieName).Value

	req := httptest.NewRequest("POST", "/", nil)
	req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: token})
	req.Header.Set(web.CSRFHeader, token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (header token)", rec.Code)
	}
}

func TestCSRFAcceptsTheTokenInAFormField(t *testing.T) {
	h, _ := csrfStack(t, false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	token := cookieFrom(t, rec, web.CSRFCookieName).Value

	form := url.Values{web.CSRFFormField: {token}, "title": {"hello"}}
	req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: token})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (form token)", rec.Code)
	}
}

// TestCSRFFormParsingDoesNotConsumeTheBody: the middleware may need to read
// the form to find the token, and the handler must still see its own fields.
func TestCSRFFormParsingDoesNotConsumeTheBody(t *testing.T) {
	e, _ := testErrors(t)
	c := web.NewCSRF(false, e)
	h := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("title=" + r.FormValue("title")))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	token := cookieFrom(t, rec, web.CSRFCookieName).Value

	form := url.Values{web.CSRFFormField: {token}, "title": {"a snippet"}}
	req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: token})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Body.String(); got != "title=a snippet" {
		t.Errorf("handler saw %q; the middleware consumed the body", got)
	}
}

func TestCSRFRejectsAMismatchedToken(t *testing.T) {
	h, _ := csrfStack(t, false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	token := cookieFrom(t, rec, web.CSRFCookieName).Value

	tests := []struct {
		name   string
		cookie string
		sent   string
	}{
		{"wrong value", token, "not-the-token"},
		{"empty sent value", token, ""},
		{"no cookie", "", token},
		{"both empty", "", ""},
		{"token from another session", "some-other-token", token},
		{"prefix of the real token", token, token[:len(token)-1]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", nil)
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: tt.cookie})
			}
			if tt.sent != "" {
				req.Header.Set(web.CSRFHeader, tt.sent)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403", rec.Code)
			}
		})
	}
}

// TestCSRFRotateIssuesANewToken guards against session fixation of the token
// at login.
func TestCSRFRotate(t *testing.T) {
	_, c := csrfStack(t, true)

	rec := httptest.NewRecorder()
	first, err := c.Rotate(rec)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	cookie := cookieFrom(t, rec, web.CSRFCookieName)
	if cookie == nil || cookie.Value != first {
		t.Fatal("Rotate did not set the cookie to the returned token")
	}

	rec = httptest.NewRecorder()
	second, err := c.Rotate(rec)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Error("Rotate returned the same token twice")
	}
}

func TestCSRFCookieIsNotSecureOnPlainHTTP(t *testing.T) {
	h, _ := csrfStack(t, false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	// A Secure cookie is never sent over http, so a local dev server on
	// http://localhost would be unable to log in at all.
	if c := cookieFrom(t, rec, web.CSRFCookieName); c.Secure {
		t.Error("cookie is Secure even though secure=false")
	}
}

func TestLimitBody(t *testing.T) {
	h := web.LimitBody(16)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "too big", http.StatusRequestEntityTooLarge)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))

	small := strings.NewReader("a=1")
	req := httptest.NewRequest("POST", "/", small)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("small body = %d, want 200", rec.Code)
	}

	big := strings.NewReader("a=" + strings.Repeat("x", 1000))
	req = httptest.NewRequest("POST", "/", big)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Error("an oversized body was accepted")
	}
}
