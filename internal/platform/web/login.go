package web

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
)

// SessionCookieName holds the opaque session id.
const SessionCookieName = "onsuite_session"

const (
	// maxLoginAttempts before a key is locked out.
	maxLoginAttempts = 10
	// loginWindow is how long attempts are remembered, and therefore how long
	// a lockout lasts after the last attempt.
	loginWindow = 15 * time.Minute
)

// loginFailedMessage is deliberately identical for a wrong password and an
// unknown account. Distinguishing them would let anyone enumerate usernames.
const loginFailedMessage = "That username or password is incorrect."

type AuthOptions struct {
	Users  *auth.Store
	Render *render.Renderer
	Errors *Errors
	CSRF   *CSRF
	Log    *slog.Logger
	// Secure marks cookies Secure. It must be false on a plain-HTTP dev
	// server, because a Secure cookie is never sent over http and login
	// would fail with no visible reason.
	Secure bool
}

// Auth turns a session cookie into a known user, and owns the login and
// logout endpoints.
type Auth struct {
	users  *auth.Store
	render *render.Renderer
	errs   *Errors
	csrf   *CSRF
	log    *slog.Logger
	secure bool

	now     func() time.Time
	limiter *attemptLimiter
}

func NewAuth(opts AuthOptions) *Auth {
	return &Auth{
		users:   opts.Users,
		render:  opts.Render,
		errs:    opts.Errors,
		csrf:    opts.CSRF,
		log:     opts.Log,
		secure:  opts.Secure,
		now:     func() time.Time { return time.Now().UTC() },
		limiter: newAttemptLimiter(),
	}
}

// SetClock replaces the time source, so expiry can be tested without waiting.
func (a *Auth) SetClock(now func() time.Time) { a.now = now }

// Routes registers the endpoints that must exist outside any app.
func (a *Auth) Routes(mux *http.ServeMux) {
	mux.Handle("GET /login", http.HandlerFunc(a.loginForm))
	mux.Handle("POST /login", http.HandlerFunc(a.loginSubmit))
	mux.Handle("POST /logout", http.HandlerFunc(a.logout))
}

// LoadUser puts the current user in the request context when the session
// cookie names a live session. It never blocks: deciding what anonymous means
// is the route's business, not this middleware's.
func (a *Auth) LoadUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil || cookie.Value == "" {
			next.ServeHTTP(w, r)
			return
		}

		sess, err := a.users.UseSession(r.Context(), cookie.Value)
		if err != nil {
			if !errors.Is(err, auth.ErrNotFound) {
				a.log.Error("session lookup failed", "error", err)
			}
			// Unknown, tampered or expired: clear the cookie so the browser
			// stops sending it, and continue as anonymous.
			a.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}

		user, err := a.users.UserByID(r.Context(), sess.UserID)
		if err != nil {
			// The session outlived its user. Revoke it.
			if derr := a.users.DeleteSession(r.Context(), sess.ID); derr != nil {
				a.log.Error("revoking an orphaned session failed", "error", derr)
			}
			a.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}

		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
	})
}

// RequireUser blocks anonymous requests. Task 13 makes this the default for
// every app route, so that forgetting to protect a route is impossible.
func (a *Auth) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFrom(r.Context()); ok {
			next.ServeHTTP(w, r)
			return
		}

		target := "/login"
		if r.Method == http.MethodGet && !IsHTMX(r) {
			target += "?next=" + url.QueryEscape(r.URL.RequestURI())
		}

		// HTMX would swap the login page into whatever element triggered the
		// request. HX-Redirect tells it to navigate instead.
		if IsHTMX(r) {
			w.Header().Set("HX-Redirect", target)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	})
}

func (a *Auth) loginForm(w http.ResponseWriter, r *http.Request) {
	// Already signed in: nothing to do here.
	if _, ok := UserFrom(r.Context()); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	a.renderLogin(w, r, http.StatusOK, "", "", safeNext(r.URL.Query().Get("next")))
}

func (a *Auth) loginSubmit(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))

	if username == "" || password == "" {
		a.renderLogin(w, r, http.StatusBadRequest, loginFailedMessage, username, next)
		return
	}

	key := strings.ToLower(username) + "|" + clientIP(r)
	if !a.limiter.allow(key, a.now()) {
		a.log.Warn("login rate limit reached", "username", username, "ip", clientIP(r))
		a.errs.Status(w, r, http.StatusTooManyRequests)
		return
	}

	user, err := a.users.UserByUsername(r.Context(), username)
	if err != nil {
		if !errors.Is(err, auth.ErrNotFound) {
			a.errs.Internal(w, r, err)
			return
		}
		// Spend the same work as a real verification, so response timing does
		// not reveal whether the account exists.
		auth.DummyVerify(password)
		a.limiter.record(key, a.now())
		a.renderLogin(w, r, http.StatusUnauthorized, loginFailedMessage, username, next)
		return
	}

	ok, err := auth.VerifyPassword(user.PasswordHash, password)
	if err != nil {
		// A stored hash that will not parse is corruption, not a bad password.
		a.errs.Internal(w, r, err)
		return
	}
	if !ok {
		a.limiter.record(key, a.now())
		a.renderLogin(w, r, http.StatusUnauthorized, loginFailedMessage, username, next)
		return
	}

	a.limiter.clear(key)

	sess, err := a.users.CreateSession(r.Context(), user.ID)
	if err != nil {
		a.errs.Internal(w, r, err)
		return
	}

	// Rotate the CSRF token so one planted before sign-in cannot survive it.
	if _, err := a.csrf.Rotate(w); err != nil {
		a.errs.Internal(w, r, err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sess.ID,
		Path:     "/",
		Expires:  sess.ExpiresAt,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
	a.log.Info("signed in", "username", user.Username, "ip", clientIP(r))

	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
		// Delete the row, not just the cookie: a copied cookie must stop
		// working, which is the whole reason sessions live in the database.
		if err := a.users.DeleteSession(r.Context(), cookie.Value); err != nil {
			a.log.Error("deleting a session on logout failed", "error", err)
		}
	}
	a.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *Auth) renderLogin(w http.ResponseWriter, r *http.Request, status int, message, username, next string) {
	page := render.Page{
		Title: "Sign in",
		Shell: render.Shell{CSRFToken: CSRFToken(r.Context())},
		Data: map[string]any{
			"Error":    message,
			"Username": username,
			"Next":     next,
		},
	}
	if err := a.render.Page(w, status, "login", page); err != nil {
		a.errs.Internal(w, r, err)
	}
}

func (a *Auth) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// safeNext keeps ?next= from becoming an open redirect. Only a rooted local
// path is allowed: anything with a scheme, a host, or a leading "//" or "/\"
// could send the user to another site after login.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") {
		return "/"
	}
	if strings.HasPrefix(next, "//") || strings.HasPrefix(next, `/\`) {
		return "/"
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return "/"
	}
	return u.RequestURI()
}

// clientIP is the best available identifier for rate limiting. It reads
// RemoteAddr only: a forwarded header is attacker-controlled unless a trusted
// proxy is known to set it, and trusting one here would make the limiter
// trivially bypassable.
func clientIP(r *http.Request) string {
	host, _, err := netSplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// attemptLimiter counts recent failures per key.
//
// In memory on purpose: a restart clearing it is acceptable for a suite with a
// handful of users, and it avoids a schema plus a database write on every
// failed guess.
type attemptLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func newAttemptLimiter() *attemptLimiter {
	return &attemptLimiter{attempts: make(map[string][]time.Time)}
}

// allow reports whether another attempt may be made.
func (l *attemptLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key, now)) < maxLoginAttempts
}

// record notes a failure.
func (l *attemptLimiter) record(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts[key] = append(l.prune(key, now), now)
}

// clear forgets a key after a success.
func (l *attemptLimiter) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// prune drops attempts outside the window. Callers hold the lock.
func (l *attemptLimiter) prune(key string, now time.Time) []time.Time {
	cutoff := now.Add(-loginWindow)
	kept := l.attempts[key][:0]
	for _, t := range l.attempts[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.attempts[key] = kept
	return kept
}

// netSplitHostPort is net.SplitHostPort. It is aliased here so the reason for
// importing net is obvious at the call site.
var netSplitHostPort = net.SplitHostPort
