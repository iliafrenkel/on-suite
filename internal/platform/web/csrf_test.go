package web_test

import (
	"bytes"
	"io"
	"mime/multipart"
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

func TestCSRFAcceptsTheTokenInAMultipartFormField(t *testing.T) {
	h, _ := csrfStack(t, false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	token := cookieFrom(t, rec, web.CSRFCookieName).Value

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField(web.CSRFFormField, token); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: token})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (multipart form token)", rec.Code)
	}
}

func TestCSRFRejectsAMissingMultipartToken(t *testing.T) {
	h, _ := csrfStack(t, false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	token := cookieFrom(t, rec, web.CSRFCookieName).Value

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("title", "no token in this body"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: token})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (no token anywhere in the multipart body)", rec.Code)
	}
}

// TestCSRFRespondsTooLargeWhenTheFormTokenReadHitsTheBodyCap is the
// regression test for #330's CSRF half: a no-JS multipart form (the only
// path that reads the token from the body — see formToken) whose body
// exceeds the route's own body-size cap used to surface as a generic 403
// "Not allowed", because formToken's ParseMultipartForm failed with
// *http.MaxBytesError and verify() could not tell that apart from a forged
// or missing token. It must now answer 413 with the platform's own "too
// large" copy instead, and must never reach the handler.
func TestCSRFRespondsTooLargeWhenTheFormTokenReadHitsTheBodyCap(t *testing.T) {
	e, _ := testErrors(t)
	c := web.NewCSRF(false, e)

	var handlerRan bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handlerRan = true })
	// The same composition Stack uses: an outer body-size cap ahead of CSRF
	// (see middleware.go's limitBodyForStack), here small enough that an
	// ordinary multipart submission already exceeds it.
	h := web.LimitBody(64)(c.Middleware(inner))

	// Issue a token over an unrelated, unbounded request first.
	tokenRec := httptest.NewRecorder()
	c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).
		ServeHTTP(tokenRec, httptest.NewRequest("GET", "/", nil))
	token := cookieFrom(t, tokenRec, web.CSRFCookieName).Value

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField(web.CSRFFormField, token); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("padding", strings.Repeat("x", 200)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: token})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if handlerRan {
		t.Error("the handler ran despite the over-cap body")
	}
	if !strings.Contains(rec.Body.String(), web.TooLargeMessage) {
		t.Errorf("body = %q, want it to contain %q", rec.Body.String(), web.TooLargeMessage)
	}
}

// TestCSRFMultipartParsingLeavesTheHandlersOwnFieldsReadable mirrors
// TestCSRFFormParsingDoesNotConsumeTheBody for multipart: the handler
// must still see its own form fields and its uploaded file after the
// middleware has already parsed the body once, looking for the token.
func TestCSRFMultipartParsingLeavesTheHandlersOwnFieldsReadable(t *testing.T) {
	e, _ := testErrors(t)
	c := web.NewCSRF(false, e)
	h := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer func() { _ = file.Close() }()
		data, _ := io.ReadAll(file)
		_, _ = w.Write([]byte("title=" + r.FormValue("title") + " file=" + string(data)))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	token := cookieFrom(t, rec, web.CSRFCookieName).Value

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField(web.CSRFFormField, token); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("title", "a title"); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("file", "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("- bullet")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: token})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Body.String(); got != "title=a title file=- bullet" {
		t.Errorf("handler saw %q; the middleware did not leave its fields readable", got)
	}
}
