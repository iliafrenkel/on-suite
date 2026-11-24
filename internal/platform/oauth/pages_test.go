package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// pagesEnv is an authorization server with three signed-in people: ilia and
// mia (users) and root (admin). Each has a session cookie set.
type pagesEnv struct {
	*asServer
	users   map[string]auth.User
	cookies map[string][]*http.Cookie
}

func newPagesEnv(t *testing.T) *pagesEnv {
	t.Helper()
	a := newASServer(t, nil, []string{claudeRedirect}, nil)
	e := &pagesEnv{asServer: a, users: map[string]auth.User{}, cookies: map[string][]*http.Cookie{}}
	store := auth.NewStore(a.handle)
	for _, name := range []string{"ilia", "mia", "root"} {
		// The fixture already has alice and bob, so these are new rows.
		u, err := store.CreateUser(context.Background(), name, apptest.PasswordHash, name == "root")
		if err != nil {
			t.Fatal(err)
		}
		e.users[name] = u
		e.cookies[name] = e.logIn(t, name)
	}
	// The fixture database already holds client "c1" for the grants.
	return e
}

func (e *pagesEnv) logIn(t *testing.T, name string) []*http.Cookie {
	t.Helper()
	page := e.do(t, http.MethodGet, "/login", nil, nil)
	var csrf *http.Cookie
	for _, c := range page.Result().Cookies() {
		if c.Name == web.CSRFCookieName {
			csrf = c
		}
	}
	if csrf == nil {
		t.Fatal("GET /login issued no CSRF cookie")
	}
	form := url.Values{"username": {name}, "password": {apptest.Password}, web.CSRFFormField: {csrf.Value}}
	rec := e.do(t, http.MethodPost, "/login", form, []*http.Cookie{csrf})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login %s = %d", name, rec.Code)
	}
	return rec.Result().Cookies()
}

func (e *pagesEnv) do(t *testing.T, method, path string, form url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, testPublicURL+path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, testPublicURL+path, nil)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func (e *pagesEnv) get(t *testing.T, who, path string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, http.MethodGet, path, nil, e.cookies[who])
}

// post submits the CSRF token the way the page's own form does, read from
// the page at from.
func (e *pagesEnv) post(t *testing.T, who, from, path string) *httptest.ResponseRecorder {
	t.Helper()
	page := e.get(t, who, from)
	if page.Code != http.StatusOK {
		t.Fatalf("GET %s as %s = %d", from, who, page.Code)
	}
	doc := htmlassert.Parse(t, page.Body.String())
	in := doc.MustHave(`input[name=` + web.CSRFFormField + `]`)
	token, _ := htmlassert.Attr(in, "value")
	return e.do(t, http.MethodPost, path, url.Values{web.CSRFFormField: {token}}, e.cookies[who])
}

func (e *pagesEnv) grant(t *testing.T, id, who, clientName string, granted ...string) {
	t.Helper()
	err := e.srv.store.UpsertGrant(context.Background(), Grant{
		ID: id, UserID: e.users[who].ID, ClientID: "c1", ClientName: clientName,
		RedirectHost: "claude.ai", Requested: granted, Granted: granted,
		CreatedAt: time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (e *pagesEnv) hasGrant(t *testing.T, id string) bool {
	t.Helper()
	_, err := e.srv.store.Grant(context.Background(), id)
	return err == nil
}

func TestAssistantsPageListsOnlyOwnGrants(t *testing.T) {
	e := newPagesEnv(t)
	e.grant(t, "g-ilia", "ilia", "Claude", "books:read", "notes:read", "notes:write")
	e.grant(t, "g-mia", "mia", "Mia's Secret Bot", "later:read")
	if err := e.srv.store.TouchGrant(context.Background(), "g-ilia"); err != nil {
		t.Fatal(err)
	}

	rec := e.get(t, "ilia", "/account/assistants")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := htmlassert.Parse(t, rec.Body.String()).Text()
	for _, want := range []string{
		"Claude", "claude.ai", "Books: Read your books", "Notes: Read your notes, Add notes",
		"2026-11-20", "Disconnect",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
	for _, bad := range []string{"Mia's Secret Bot", "Saved pages", "Later:"} {
		if strings.Contains(body, bad) {
			t.Errorf("page shows someone else's grant (%q)", bad)
		}
	}
	if !strings.Contains(body, "2026-11-24 12:00 UTC") {
		t.Errorf("last used time missing:\n%s", body)
	}
	// An unused grant says so, in the Last used column.
	rec = e.get(t, "mia", "/account/assistants")
	sawNever := false
	for _, td := range htmlassert.Parse(t, rec.Body.String()).QueryAll("td") {
		sawNever = sawNever || htmlassert.Text(td) == "never"
	}
	if !sawNever {
		t.Error("an unused grant does not say never in its Last used cell")
	}
}

func TestAssistantsPageEmptyState(t *testing.T) {
	e := newPagesEnv(t)
	body := htmlassert.Parse(t, e.get(t, "ilia", "/account/assistants").Body.String()).Text()
	if !strings.Contains(body, "No assistants connected. See Help → AI assistants to connect one.") {
		t.Errorf("empty state missing:\n%s", body)
	}
}

func TestAssistantsPageRequiresSignIn(t *testing.T) {
	e := newPagesEnv(t)
	rec := e.do(t, http.MethodGet, "/account/assistants", nil, nil)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
		t.Errorf("anonymous = %d %q, want a redirect to /login", rec.Code, rec.Header().Get("Location"))
	}
}

func TestDisconnectRevokesOwnGrantOnly(t *testing.T) {
	e := newPagesEnv(t)
	e.grant(t, "g-ilia", "ilia", "Claude", "books:read")
	e.grant(t, "g-mia", "mia", "Other", "books:read")

	// Someone else's grant: 404, and it survives.
	if rec := e.post(t, "ilia", "/account/assistants", "/account/assistants/g-mia/disconnect"); rec.Code != http.StatusNotFound {
		t.Errorf("disconnect of another user's grant = %d, want 404", rec.Code)
	}
	if !e.hasGrant(t, "g-mia") {
		t.Fatal("another user's grant was revoked")
	}

	// No CSRF token: 403, and it survives.
	rec := e.do(t, http.MethodPost, "/account/assistants/g-ilia/disconnect", url.Values{}, e.cookies["ilia"])
	if rec.Code != http.StatusForbidden {
		t.Errorf("disconnect without CSRF = %d, want 403", rec.Code)
	}
	if !e.hasGrant(t, "g-ilia") {
		t.Fatal("a grant was revoked without a CSRF token")
	}

	// Own grant: 303 back to the page, and it is gone.
	rec = e.post(t, "ilia", "/account/assistants", "/account/assistants/g-ilia/disconnect")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account/assistants" {
		t.Errorf("disconnect = %d %q, want 303 to /account/assistants", rec.Code, rec.Header().Get("Location"))
	}
	if e.hasGrant(t, "g-ilia") {
		t.Error("the grant survived its own disconnect")
	}
}

func TestAssistantsPageShowsRecentActivity(t *testing.T) {
	e := newPagesEnv(t)
	base := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	for i := range 55 {
		err := e.srv.store.Audit(context.Background(), AuditEntry{
			UserID: e.users["ilia"].ID, ClientName: "Claude", Tool: fmt.Sprintf("tool_%02d", i),
			OK: true, At: base.Add(time.Duration(i) * time.Minute),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// The newest row is a failure; mia's row must never show.
	if err := e.srv.store.Audit(context.Background(), AuditEntry{
		UserID: e.users["ilia"].ID, ClientName: "Claude", Tool: "books_update",
		ErrorKind: "forbidden", At: base.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.srv.store.Audit(context.Background(), AuditEntry{
		UserID: e.users["mia"].ID, ClientName: "Other", Tool: "mias_tool", OK: true, At: base.Add(3 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	doc := htmlassert.Parse(t, e.get(t, "ilia", "/account/assistants").Body.String())
	var rows []string
	for _, tr := range doc.QueryAll("table tbody tr") {
		rows = append(rows, htmlassert.Text(tr))
	}
	// The grants table is empty (one "no" row is not present: the empty
	// state is a paragraph), so every row here is activity.
	if len(rows) != 50 {
		t.Fatalf("activity rows = %d, want 50", len(rows))
	}
	if !strings.Contains(rows[0], "books_update") || !strings.Contains(rows[0], "error: forbidden") ||
		!strings.Contains(rows[0], "2026-11-24 10:00:00") || !strings.Contains(rows[0], "Claude") {
		t.Errorf("newest row = %q", rows[0])
	}
	if !strings.Contains(rows[1], "tool_54") || !strings.Contains(rows[1], "ok") {
		t.Errorf("second row = %q", rows[1])
	}
	if strings.Contains(doc.Text(), "mias_tool") {
		t.Error("another user's activity is shown")
	}
	if strings.Contains(doc.Text(), "tool_04 ") {
		t.Error("the 51st row is shown")
	}
}

func TestAdminAIPage(t *testing.T) {
	e := newPagesEnv(t)
	e.grant(t, "g-ilia", "ilia", "Claude", "books:read")
	e.grant(t, "g-mia", "mia", "Other", "later:read")

	if rec := e.get(t, "ilia", "/admin/ai"); rec.Code != http.StatusNotFound {
		t.Errorf("non-admin GET /admin/ai = %d, want 404", rec.Code)
	}
	if rec := e.post(t, "ilia", "/account/assistants", "/admin/ai/g-ilia/revoke"); rec.Code != http.StatusNotFound {
		t.Errorf("non-admin revoke = %d, want 404", rec.Code)
	}

	rec := e.get(t, "root", "/admin/ai")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin GET = %d", rec.Code)
	}
	body := htmlassert.Parse(t, rec.Body.String()).Text()
	for _, want := range []string{testPublicURL, claudeRedirect, "ilia", "mia", "Claude", "Other", "Revoke"} {
		if !strings.Contains(body, want) {
			t.Errorf("admin page lacks %q:\n%s", want, body)
		}
	}

	rec = e.post(t, "root", "/admin/ai", "/admin/ai/g-mia/revoke")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/ai" {
		t.Errorf("revoke = %d %q, want 303 to /admin/ai", rec.Code, rec.Header().Get("Location"))
	}
	if e.hasGrant(t, "g-mia") {
		t.Error("the admin's revoke left the grant")
	}
	if !e.hasGrant(t, "g-ilia") {
		t.Error("revoking one grant removed another")
	}
	if rec := e.post(t, "root", "/admin/ai", "/admin/ai/nope/revoke"); rec.Code != http.StatusNotFound {
		t.Errorf("revoke of a missing grant = %d, want 404", rec.Code)
	}
}
