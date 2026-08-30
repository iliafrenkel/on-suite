package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
)

const (
	// CSRFCookieName holds the token the browser returns automatically.
	CSRFCookieName = "onsuite_csrf"
	// CSRFHeader is how HTMX sends the token; see base.html.
	CSRFHeader = "X-CSRF-Token"
	// CSRFFormField is how a plain HTML form sends it.
	CSRFFormField = "csrf_token"

	csrfTokenBytes = 32
)

// CSRF implements double-submit-cookie protection.
//
// A random token lives in an HttpOnly cookie, and the server renders the same
// token into the page. The browser therefore returns it two ways: in the
// cookie automatically, and in a header or form field deliberately. Another
// origin can cause the cookie to be sent but cannot read it, so it cannot
// produce the second copy.
//
// This requires no server-side secret and no database column. SameSite=Lax
// already blocks cross-site form posts in current browsers; the token is the
// defence that does not depend on the browser getting that right.
type CSRF struct {
	secure bool
	errs   *Errors
}

func NewCSRF(secure bool, errs *Errors) *CSRF {
	return &CSRF{secure: secure, errs: errs}
}

// Middleware issues a token when there is none, verifies it on unsafe
// methods, and puts it in the request context for templates to render.
func (c *CSRF) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if cookie, err := r.Cookie(CSRFCookieName); err == nil {
			token = cookie.Value
		}

		if token == "" {
			fresh, err := c.Rotate(w)
			if err != nil {
				c.errs.Internal(w, r, err)
				return
			}
			token = fresh
		}

		if !safeMethod(r.Method) && !c.verify(r, token) {
			// Deliberately vague: a mismatch is either an attack or a stale
			// tab, and neither is helped by detail.
			c.errs.Status(w, r, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r.WithContext(WithCSRFToken(r.Context(), token)))
	})
}

// Rotate issues a new token and sets the cookie. Called at login, so that a
// token an attacker planted before sign-in cannot survive it.
func (c *CSRF) Rotate(w http.ResponseWriter) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return token, nil
}

// verify compares the token the browser sent deliberately with the one it
// sent automatically.
func (c *CSRF) verify(r *http.Request, cookieToken string) bool {
	if cookieToken == "" {
		return false
	}

	sent := r.Header.Get(CSRFHeader)
	if sent == "" {
		sent = formToken(r)
	}
	if sent == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(sent), []byte(cookieToken)) == 1
}

// formToken reads the CSRF field from the request body, for the one kind
// of request that cannot carry the token as a header: a plain HTML form
// submitted with JavaScript off.
//
// ParseForm only reads the body for application/x-www-form-urlencoded,
// leaving any other content type's body untouched — a route whose form
// also uploads a file needs ParseMultipartForm instead (ON Notes' import
// route, N8, is the first one in this codebase to need it). Anything else
// is left alone by both calls, exactly as ParseForm alone already left it
// before this function existed, so a handler expecting some other body
// shape can still read it itself.
//
// Both calls cache into r.Form/r.PostForm (and, for multipart,
// r.MultipartForm), so the handler that runs afterwards reads that same
// cache rather than triggering a second read of the body.
func formToken(r *http.Request) string {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct == "multipart/form-data" {
		if err := r.ParseMultipartForm(DefaultMaxBodyBytes); err != nil {
			return ""
		}
		return r.PostFormValue(CSRFFormField)
	}
	if err := r.ParseForm(); err != nil {
		return ""
	}
	return r.PostFormValue(CSRFFormField)
}

func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

func randomToken() (string, error) {
	buf := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("web: generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// DefaultMaxBodyBytes bounds request bodies. A megabyte is generous for a text
// snippet and small enough that a runaway upload cannot exhaust memory. An app
// needing more should wrap its own routes rather than raising this globally.
const DefaultMaxBodyBytes = 1 << 20

// LimitBody caps how much of a request body will be read.
func LimitBody(max int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, max)
			next.ServeHTTP(w, r)
		})
	}
}
