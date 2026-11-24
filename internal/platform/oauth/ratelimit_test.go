package oauth

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// limitEnv serves requests from a chosen RemoteAddr against a clock the test
// moves by hand, so a window can be filled and then expired.
type limitEnv struct {
	*asServer
	now time.Time
}

func newLimitEnv(t *testing.T) *limitEnv {
	t.Helper()
	e := &limitEnv{now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	e.asServer = buildASServer(t, nil, []string{claudeRedirect}, nil, func() time.Time { return e.now })
	return e
}

func (e *limitEnv) do(method, path, addr, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, testPublicURL+path, strings.NewReader(body))
	req.RemoteAddr = addr
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		// The consent POST keeps its CSRF check, which runs before the
		// route; give every POST a matching token so it reaches the limiter.
		req.AddCookie(&http.Cookie{Name: web.CSRFCookieName, Value: "t"})
		req.Header.Set(web.CSRFHeader, "t")
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func wantLimited(t *testing.T, rec *httptest.ResponseRecorder, wantJSON bool) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body %q", rec.Code, rec.Body.String())
	}
	secs, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || secs < 1 {
		t.Errorf("Retry-After = %q, want whole seconds >= 1", rec.Header().Get("Retry-After"))
	}
	if wantJSON && !strings.Contains(rec.Body.String(), `"error":"slow_down"`) {
		t.Errorf("body = %q, want an OAuth slow_down error", rec.Body.String())
	}
}

func TestRegisterLimitedPerIP(t *testing.T) {
	e := newLimitEnv(t)
	for i := range 10 {
		if rec := e.do("POST", "/oauth/register", "192.0.2.1:5000", `{}`); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d limited, want the first 10 allowed", i+1)
		}
	}
	wantLimited(t, e.do("POST", "/oauth/register", "192.0.2.1:5001", `{}`), true)

	// Another address has its own budget.
	if rec := e.do("POST", "/oauth/register", "192.0.2.2:5000", `{}`); rec.Code == http.StatusTooManyRequests {
		t.Error("a different IP was limited")
	}

	// The window ends after an hour.
	e.now = e.now.Add(time.Hour)
	if rec := e.do("POST", "/oauth/register", "192.0.2.1:5000", `{}`); rec.Code == http.StatusTooManyRequests {
		t.Error("still limited after the window")
	}
}

func TestTokenAndAuthorizeLimitedPerIP(t *testing.T) {
	cases := []struct {
		name, method, path string
		json               bool
	}{
		{"token", "POST", "/oauth/token", true},
		{"authorize GET", "GET", "/oauth/authorize", false},
		{"authorize POST", "POST", "/oauth/authorize", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newLimitEnv(t)
			for i := range 60 {
				if rec := e.do(c.method, c.path, "192.0.2.1:5000", ""); rec.Code == http.StatusTooManyRequests {
					t.Fatalf("request %d limited, want the first 60 allowed", i+1)
				}
			}
			wantLimited(t, e.do(c.method, c.path, "192.0.2.1:5001", ""), c.json)
			if rec := e.do(c.method, c.path, "192.0.2.2:5000", ""); rec.Code == http.StatusTooManyRequests {
				t.Error("a different IP was limited")
			}
			e.now = e.now.Add(time.Minute)
			if rec := e.do(c.method, c.path, "192.0.2.1:5000", ""); rec.Code == http.StatusTooManyRequests {
				t.Error("still limited after the window")
			}
		})
	}
}
