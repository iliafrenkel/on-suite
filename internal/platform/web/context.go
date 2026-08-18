package web

import (
	"context"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
)

// ctxKey is unexported so nothing outside this package can collide with or
// overwrite these values.
type ctxKey int

const (
	ctxKeyActiveApp ctxKey = iota
	ctxKeyCSRFToken
	ctxKeyUser
)

// WithActiveApp records which app is handling the request, so the shell can
// mark it in the nav.
func WithActiveApp(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyActiveApp, id)
}

// ActiveApp returns the app id, or "" outside any app.
func ActiveApp(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyActiveApp).(string)
	return id
}

// WithCSRFToken stores the token for this request. The middleware that calls
// this arrives in Task 11.
func WithCSRFToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, ctxKeyCSRFToken, token)
}

// CSRFToken returns the token, or "" if none has been issued.
func CSRFToken(ctx context.Context) string {
	token, _ := ctx.Value(ctxKeyCSRFToken).(string)
	return token
}

// WithUser stores the authenticated user. The middleware that calls this
// arrives in Task 12.
func WithUser(ctx context.Context, u auth.User) context.Context {
	return context.WithValue(ctx, ctxKeyUser, u)
}

// UserFrom returns the authenticated user, and false when nobody is logged in.
func UserFrom(ctx context.Context) (auth.User, bool) {
	u, ok := ctx.Value(ctxKeyUser).(auth.User)
	return u, ok
}

// IsHTMX reports whether the request came from HTMX, which decides between
// returning a fragment and a whole document.
func IsHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}
