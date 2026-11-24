package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"authelia.com/provider/oauth2"

	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// The flow tests run end to end: DCR, a real login, the consent page, the
// token endpoint. Each runs inside a synctest bubble, for two reasons. The
// library reads the real clock, so a bubble is the only way to step past a
// code's minute or a grant's 90 days. And the database has one connection
// (internal/platform/db): a request that waited on a second one would block
// forever, which inside a bubble lets fake time run out every request's
// deadline (or, failing that, panics as a deadlock) instead of hanging.

const (
	pkceVerifier = "flow-test-verifier-0123456789-abcdefghijklmnopqrstuvwxyz"
	flowState    = "state-0123456789"
	// requestTimeout bounds every request. In a bubble it costs nothing
	// unless a request blocks, and then it turns a hang into a failure.
	requestTimeout = 10 * time.Second
)

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// flowEnv is one authorization server, one signed-in user and one
// registered client.
type flowEnv struct {
	*asServer
	user     auth.User
	cookies  []*http.Cookie
	clientID string
	// verifier is the PKCE verifier of the latest authorization request.
	// Each request gets a fresh one, as a real client's does: the library
	// refuses a challenge it has seen before.
	verifier string
	requests int
}

func newFlowEnv(t *testing.T) *flowEnv {
	t.Helper()
	e := newSignedInEnv(t, nil)
	a := e.asServer

	// The body Claude sends: refresh_token must be asked for, since RFC 7591
	// defaults grant_types to authorization_code alone.
	code, body := a.register(t, `{"redirect_uris":["`+claudeRedirect+`"],"client_name":"Claude",`+
		`"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`)
	if code != http.StatusCreated {
		t.Fatalf("register = %d %v", code, body)
	}
	e.clientID, _ = body["client_id"].(string)
	if e.clientID == "" {
		t.Fatalf("register gave no client_id: %v", body)
	}
	return e
}

// newCIMDFlowEnv is newFlowEnv for a client that identifies by its metadata
// document URL instead of registering: the server fetches host's document.
func newCIMDFlowEnv(t *testing.T, host *cimdHost) *flowEnv {
	t.Helper()
	e := newSignedInEnv(t, host.inProcessClient())
	e.clientID = host.clientID()
	return e
}

// newSignedInEnv is an authorization server with one signed-in user and no
// client yet. client fetches CIMD documents (nil: the guarded default).
func newSignedInEnv(t *testing.T, client *http.Client) *flowEnv {
	t.Helper()
	a := buildASServer(t, nil, []string{claudeRedirect, chatGPTRedirect}, client, time.Now)
	e := &flowEnv{asServer: a}
	user, err := auth.NewStore(a.handle).CreateUser(context.Background(), "ilia", apptest.PasswordHash, false)
	if err != nil {
		t.Fatal(err)
	}
	e.user = user
	e.cookies = e.logIn(t, "")
	return e
}

// serve runs one request through the full stack with a deadline.
func (e *flowEnv) serve(t *testing.T, req *http.Request, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithTimeout(req.Context(), requestTimeout)
	defer cancel()
	req = req.WithContext(ctx)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func (e *flowEnv) postForm(t *testing.T, path string, form url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, testPublicURL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return e.serve(t, req, cookies)
}

// logIn signs in through the real login form and returns the cookies,
// failing unless it redirects to next (or / when next is empty).
func (e *flowEnv) logIn(t *testing.T, next string) []*http.Cookie {
	t.Helper()
	rec := e.loginWithNext(t, next)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login = %d", rec.Code)
	}
	return rec.Result().Cookies()
}

func (e *flowEnv) loginWithNext(t *testing.T, next string) *httptest.ResponseRecorder {
	t.Helper()
	page := e.serve(t, httptest.NewRequest(http.MethodGet, testPublicURL+"/login", nil), nil)
	var csrf *http.Cookie
	for _, c := range page.Result().Cookies() {
		if c.Name == web.CSRFCookieName {
			csrf = c
		}
	}
	if csrf == nil {
		t.Fatal("GET /login issued no CSRF cookie")
	}
	form := url.Values{"username": {"ilia"}, "password": {apptest.Password}, web.CSRFFormField: {csrf.Value}}
	if next != "" {
		form.Set("next", next)
	}
	return e.postForm(t, "/login", form, []*http.Cookie{csrf})
}

// authorizeQuery is a well-formed S256 authorization request with a fresh
// PKCE verifier. Empty scope or resource leaves that parameter out.
func (e *flowEnv) authorizeQuery(scope, resource string) url.Values {
	e.requests++
	e.verifier = pkceVerifier + "-" + strconv.Itoa(e.requests)
	q := url.Values{
		"client_id":             {e.clientID},
		"response_type":         {"code"},
		"redirect_uri":          {claudeRedirect},
		"state":                 {flowState},
		"code_challenge":        {pkceChallenge(e.verifier)},
		"code_challenge_method": {"S256"},
	}
	if scope != "" {
		q.Set("scope", scope)
	}
	if resource != "" {
		q.Set("resource", resource)
	}
	return q
}

func (e *flowEnv) getAuthorize(t *testing.T, q url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return e.serve(t, httptest.NewRequest(http.MethodGet, testPublicURL+"/oauth/authorize?"+q.Encode(), nil), cookies)
}

// consentForm is what the consent page's form would submit: its hidden
// fields (the original request and the CSRF token), the ticked scopes, and
// the decision.
func consentForm(t *testing.T, rec *httptest.ResponseRecorder, decision string, scopes ...string) url.Values {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("authorize = %d, want the consent page; Location %q", rec.Code, rec.Header().Get("Location"))
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(`form[action="/oauth/authorize"]`)
	form := url.Values{}
	for _, in := range doc.QueryAll(`form[action="/oauth/authorize"] input[type=hidden]`) {
		name, _ := htmlassert.Attr(in, "name")
		value, _ := htmlassert.Attr(in, "value")
		form.Add(name, value)
	}
	form.Set("decision", decision)
	for _, sc := range scopes {
		form.Add("grant_scope", sc)
	}
	return form
}

// approve posts the consent form with the given scopes ticked and returns
// the redirect's code, checking state and iss on the way.
func (e *flowEnv) approve(t *testing.T, page *httptest.ResponseRecorder, scopes ...string) string {
	t.Helper()
	rec := e.postForm(t, "/oauth/authorize", consentForm(t, page, "allow", scopes...), e.cookies)
	return codeFrom(t, rec)
}

func codeFrom(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	q := redirectQuery(t, rec)
	if q.Get("error") != "" {
		t.Fatalf("redirect carries error %q (%s)", q.Get("error"), q.Get("error_description"))
	}
	if q.Get("state") != flowState {
		t.Errorf("state = %q, want %q", q.Get("state"), flowState)
	}
	if q.Get("iss") != testPublicURL {
		t.Errorf("iss = %q, want %q (RFC 9207)", q.Get("iss"), testPublicURL)
	}
	if q.Get("code") == "" {
		t.Fatalf("redirect carries no code: %v", q)
	}
	return q.Get("code")
}

// redirectQuery checks the response sends the browser back to the client's
// redirect URI and returns that redirect's query.
func redirectQuery(t *testing.T, rec *httptest.ResponseRecorder) url.Values {
	t.Helper()
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want a redirect; body %q", rec.Code, rec.Body.String())
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if got := loc.Scheme + "://" + loc.Host + loc.Path; got != claudeRedirect {
		t.Fatalf("redirect to %q, want %q", got, claudeRedirect)
	}
	return loc.Query()
}

// connect runs authorize and consent and returns a code.
func (e *flowEnv) connect(t *testing.T, scope string, allow ...string) string {
	t.Helper()
	page := e.getAuthorize(t, e.authorizeQuery(scope, e.srv.ResourceURL()), e.cookies)
	return e.approve(t, page, allow...)
}

// countGrants counts every grant row, whoever's.
func (e *flowEnv) countGrants(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.handle.QueryRow(`SELECT count(*) FROM oauth_grants`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// token posts to the token endpoint with no cookie, as an AI client's
// server does, and decodes the JSON answer.
func (e *flowEnv) token(t *testing.T, form url.Values) (int, map[string]any) {
	t.Helper()
	form.Set("client_id", e.clientID)
	rec := e.postForm(t, "/oauth/token", form, nil)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("POST /oauth/token = %d, body not JSON: %q", rec.Code, rec.Body.String())
	}
	return rec.Code, out
}

func (e *flowEnv) exchange(t *testing.T, code string) (int, map[string]any) {
	t.Helper()
	return e.token(t, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {claudeRedirect},
		"code_verifier": {e.verifier},
	})
}

func (e *flowEnv) refresh(t *testing.T, refreshToken string) (int, map[string]any) {
	t.Helper()
	return e.token(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
}

// tokens fails unless the token endpoint answered 200 with both tokens.
func tokens(t *testing.T, code int, body map[string]any) (access, refresh string) {
	t.Helper()
	access, _ = body["access_token"].(string)
	refresh, _ = body["refresh_token"].(string)
	if code != http.StatusOK || access == "" || refresh == "" {
		t.Fatalf("token endpoint = %d %v, want 200 with access and refresh tokens", code, body)
	}
	return access, refresh
}

func (e *flowEnv) exchangeOK(t *testing.T, code string) (access, refresh string) {
	t.Helper()
	status, body := e.exchange(t, code)
	return tokens(t, status, body)
}

func (e *flowEnv) refreshOK(t *testing.T, refreshToken string) (access, refresh string) {
	t.Helper()
	status, body := e.refresh(t, refreshToken)
	return tokens(t, status, body)
}

func wantTokenError(t *testing.T, code int, body map[string]any, errs ...string) {
	t.Helper()
	got, _ := body["error"].(string)
	if code == http.StatusOK || !slices.Contains(errs, got) {
		t.Fatalf("token endpoint = %d %v, want error %v", code, body, errs)
	}
}

func (e *flowEnv) grant(t *testing.T) (Grant, bool) {
	t.Helper()
	gs, err := e.srv.store.Grants(context.Background(), e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range gs {
		if g.ClientID == e.clientID {
			return g, true
		}
	}
	return Grant{}, false
}

func checkbox(t *testing.T, doc *htmlassert.Doc, scope string) (shown, checked bool) {
	t.Helper()
	for _, in := range doc.QueryAll(`input[name=grant_scope]`) {
		if v, _ := htmlassert.Attr(in, "value"); v == scope {
			_, checked = htmlassert.Attr(in, "checked")
			return true, checked
		}
	}
	return false, false
}

func TestFullFlowDCRConsentTokenRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)

		page := e.getAuthorize(t, e.authorizeQuery("books:read books:write", e.srv.ResourceURL()), e.cookies)
		if page.Code != http.StatusOK {
			t.Fatalf("authorize = %d, want 200 consent; Location %q", page.Code, page.Header().Get("Location"))
		}
		doc := htmlassert.Parse(t, page.Body.String())
		if shown, checked := checkbox(t, doc, "books:read"); !shown || !checked {
			t.Errorf("books:read shown %v checked %v, want a ticked box", shown, checked)
		}
		if shown, checked := checkbox(t, doc, "books:write"); !shown || checked {
			t.Errorf("books:write shown %v checked %v, want an unticked box", shown, checked)
		}
		for _, sc := range []string{"notes:read", "reader:read", "later:read", "notes:write"} {
			if shown, _ := checkbox(t, doc, sc); shown {
				t.Errorf("%s shown, but the client did not ask for it", sc)
			}
		}
		text := doc.Text()
		for _, want := range []string{"Read your books", "Update books", "claude.ai", "Claude"} {
			if !strings.Contains(text, want) {
				t.Errorf("consent page does not say %q", want)
			}
		}
		doc.MustHave(`form[action="/oauth/authorize"] input[name=csrf_token]`)
		if csp := page.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "form-action 'self' https://claude.ai;") {
			t.Errorf("CSP = %q, want form-action 'self' https://claude.ai", csp)
		}

		rec := e.postForm(t, "/oauth/authorize", consentForm(t, page, "allow", "books:read"), e.cookies)
		if rec.Code != http.StatusSeeOther {
			t.Errorf("consent POST = %d, want 303", rec.Code)
		}
		code := codeFrom(t, rec)

		status, body := e.exchange(t, code)
		_, refresh := tokens(t, status, body)
		if body["scope"] != "books:read" {
			t.Errorf("token scope = %v, want books:read", body["scope"])
		}
		g, ok := e.grant(t)
		if !ok {
			t.Fatal("no grant row after consent")
		}
		if !slices.Equal(g.Granted, []string{"books:read"}) || !slices.Equal(g.Requested, []string{"books:read", "books:write"}) {
			t.Errorf("grant granted %v requested %v, want [books:read] / [books:read books:write]", g.Granted, g.Requested)
		}
		if g.ClientName != "Claude" || g.RedirectHost != "claude.ai" {
			t.Errorf("grant client %q host %q", g.ClientName, g.RedirectHost)
		}

		status, body = e.refresh(t, refresh)
		_, rotated := tokens(t, status, body)
		if rotated == refresh {
			t.Error("refresh did not rotate the refresh token")
		}

		status, body = e.refresh(t, refresh)
		wantTokenError(t, status, body, "invalid_grant")
		if _, ok := e.grant(t); ok {
			t.Error("reusing a rotated refresh token left the grant in place; want it revoked")
		}
		status, body = e.refresh(t, rotated)
		wantTokenError(t, status, body, "invalid_grant")
	})
}

func TestAuthorizeWithoutScopeMeansAllReads(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		page := e.getAuthorize(t, e.authorizeQuery("", e.srv.ResourceURL()), e.cookies)
		if page.Code != http.StatusOK {
			t.Fatalf("authorize = %d, want 200", page.Code)
		}
		doc := htmlassert.Parse(t, page.Body.String())
		boxes := doc.QueryAll(`input[name=grant_scope]`)
		var got []string
		for _, b := range boxes {
			v, _ := htmlassert.Attr(b, "value")
			if _, checked := htmlassert.Attr(b, "checked"); !checked {
				t.Errorf("%s is not ticked", v)
			}
			got = append(got, v)
		}
		if !slices.Equal(got, ReadScopes) {
			t.Errorf("boxes = %v, want exactly %v", got, ReadScopes)
		}
	})
}

func TestAuthorizeRefusesForeignResource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		rec := e.getAuthorize(t, e.authorizeQuery("books:read", "https://elsewhere.example/mcp"), e.cookies)
		if got := redirectQuery(t, rec).Get("error"); got != "invalid_target" {
			t.Errorf("error = %q, want invalid_target", got)
		}
	})
}

func TestAuthorizeWithoutResourceGetsOurs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		page := e.getAuthorize(t, e.authorizeQuery("books:read", ""), e.cookies)
		code := e.approve(t, page, "books:read")
		access, _ := e.exchangeOK(t, code)

		use, req, err := e.srv.provider.IntrospectToken(context.Background(), access, oauth2.AccessToken, new(oauth2.DefaultSession))
		if err != nil || use != oauth2.AccessToken {
			t.Fatalf("introspect = %v %v", use, err)
		}
		if got := req.GetGrantedResource(); !slices.Equal([]string(got), []string{e.srv.ResourceURL()}) {
			t.Errorf("granted resource = %v, want [%s]", got, e.srv.ResourceURL())
		}
	})
}

func TestConsentRefusesScopeNotRequested(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		page := e.getAuthorize(t, e.authorizeQuery("books:read", e.srv.ResourceURL()), e.cookies)
		rec := e.postForm(t, "/oauth/authorize", consentForm(t, page, "allow", "books:read", "books:write"), e.cookies)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("consent POST with an unrequested scope = %d, want 400", rec.Code)
		}
		if _, ok := e.grant(t); ok {
			t.Error("a grant was written for a forged consent")
		}
	})
}

func TestConsentCancelIsAccessDenied(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		page := e.getAuthorize(t, e.authorizeQuery("books:read", e.srv.ResourceURL()), e.cookies)
		rec := e.postForm(t, "/oauth/authorize", consentForm(t, page, "cancel", "books:read"), e.cookies)
		if got := redirectQuery(t, rec).Get("error"); got != "access_denied" {
			t.Errorf("error = %q, want access_denied", got)
		}
		if _, ok := e.grant(t); ok {
			t.Error("cancel wrote a grant")
		}

		// Allow with nothing ticked is a cancel too.
		page = e.getAuthorize(t, e.authorizeQuery("books:read", e.srv.ResourceURL()), e.cookies)
		rec = e.postForm(t, "/oauth/authorize", consentForm(t, page, "allow"), e.cookies)
		if got := redirectQuery(t, rec).Get("error"); got != "access_denied" {
			t.Errorf("allow with no scopes: error = %q, want access_denied", got)
		}
	})
}

func TestConsentPostNeedsCSRF(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		page := e.getAuthorize(t, e.authorizeQuery("books:read", e.srv.ResourceURL()), e.cookies)
		form := consentForm(t, page, "allow", "books:read")
		form.Del(web.CSRFFormField)
		if rec := e.postForm(t, "/oauth/authorize", form, e.cookies); rec.Code != http.StatusForbidden {
			t.Errorf("consent POST without a CSRF token = %d, want 403", rec.Code)
		}
		if _, ok := e.grant(t); ok {
			t.Error("a grant was written without a CSRF token")
		}
	})
}

// A GET can be triggered by any website through a plain link, so it must
// never re-authorize: it shows consent and leaves the existing connection
// (grant row and tokens) exactly as it was.
func TestAuthorizeAlwaysShowsConsentAndKeepsExistingGrant(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		const scope = "books:read books:write"
		_, refresh := e.exchangeOK(t, e.connect(t, scope, "books:read"))
		first, _ := e.grant(t)

		page := e.getAuthorize(t, e.authorizeQuery(scope, e.srv.ResourceURL()), e.cookies)
		if page.Code != http.StatusOK {
			t.Fatalf("second authorize = %d → %q, want 200 consent page", page.Code, page.Header().Get("Location"))
		}
		htmlassert.Parse(t, page.Body.String()).MustHave(`form[action="/oauth/authorize"]`)

		g, ok := e.grant(t)
		if !ok {
			t.Fatal("showing consent removed the existing grant")
		}
		if g.ID != first.ID || !slices.Equal(g.Granted, first.Granted) || !slices.Equal(g.Requested, first.Requested) {
			t.Errorf("grant changed: %+v, was %+v", g, first)
		}
		e.refreshOK(t, refresh)
	})
}

// The hidden fields are the request the user saw; a POST that changes them
// is re-validated from scratch and refused.
func TestConsentPostWithTamperedRequestIsRefused(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)

		page := e.getAuthorize(t, e.authorizeQuery("books:read", e.srv.ResourceURL()), e.cookies)
		form := consentForm(t, page, "allow", "books:read")
		form.Set("resource", "https://elsewhere.example/mcp")
		if got := redirectQuery(t, e.postForm(t, "/oauth/authorize", form, e.cookies)).Get("error"); got != "invalid_target" {
			t.Errorf("tampered resource: error = %q, want invalid_target", got)
		}

		page = e.getAuthorize(t, e.authorizeQuery("books:read", e.srv.ResourceURL()), e.cookies)
		form = consentForm(t, page, "allow", "books:read")
		form.Set("client_id", "not-a-registered-client")
		rec := e.postForm(t, "/oauth/authorize", form, e.cookies)
		if rec.Code < 400 {
			t.Errorf("tampered client_id = %d → %q, want an error", rec.Code, rec.Header().Get("Location"))
		}

		if n := e.countGrants(t); n != 0 {
			t.Errorf("grant rows = %d, want 0 after tampered consents", n)
		}
	})
}

func TestCodeIsSingleUseAndExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		code := e.connect(t, "books:read", "books:read")
		e.exchangeOK(t, code)
		status, body := e.exchange(t, code)
		wantTokenError(t, status, body, "invalid_grant")

		// Just inside the one-minute limit, a code still works.
		code = e.connect(t, "books:read", "books:read")
		time.Sleep(59 * time.Second)
		e.exchangeOK(t, code)

		code = e.connect(t, "books:read", "books:read")
		time.Sleep(61 * time.Second)
		status, body = e.exchange(t, code)
		wantTokenError(t, status, body, "invalid_grant")
	})
}

func TestPlainPKCERefusedAndMissingPKCERefused(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)

		q := e.authorizeQuery("books:read", e.srv.ResourceURL())
		q.Set("code_challenge", pkceVerifier)
		q.Set("code_challenge_method", "plain")
		if got := redirectQuery(t, e.getAuthorize(t, q, e.cookies)).Get("error"); got != "invalid_request" {
			t.Errorf("plain PKCE: error = %q, want invalid_request", got)
		}

		q = e.authorizeQuery("books:read", e.srv.ResourceURL())
		q.Del("code_challenge")
		q.Del("code_challenge_method")
		if got := redirectQuery(t, e.getAuthorize(t, q, e.cookies)).Get("error"); got != "invalid_request" {
			t.Errorf("no PKCE: error = %q, want invalid_request", got)
		}
	})
}

func TestRevokeEndpointRevokesGrant(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		code := e.connect(t, "books:read", "books:read")
		access, refresh := e.exchangeOK(t, code)

		rec := e.postForm(t, "/oauth/revoke", url.Values{"token": {refresh}, "client_id": {e.clientID}}, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /oauth/revoke = %d %q, want 200", rec.Code, rec.Body.String())
		}
		if _, ok := e.grant(t); ok {
			t.Error("revoking the refresh token left the grant in place")
		}
		status, body := e.refresh(t, refresh)
		wantTokenError(t, status, body, "invalid_grant")
		if _, _, err := e.srv.provider.IntrospectToken(context.Background(), access, oauth2.AccessToken, new(oauth2.DefaultSession)); err == nil {
			t.Error("the access token still introspects after revocation")
		}
	})
}

func TestRefreshExpiresNinetyDaysAfterConnect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		code := e.connect(t, "books:read", "books:read")
		_, refresh := e.exchangeOK(t, code)

		time.Sleep(89 * 24 * time.Hour)
		_, refresh = e.refreshOK(t, refresh)

		time.Sleep(24*time.Hour + time.Minute)
		status, body := e.refresh(t, refresh)
		wantTokenError(t, status, body, "invalid_grant")
	})
}

func TestRefreshFailsAfterClientRedirectLeavesAllowlist(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		code := e.connect(t, "books:read", "books:read")
		_, refresh := e.exchangeOK(t, code)

		// The operator drops Claude's callback and restarts: same database,
		// new allowlist.
		e.asServer = buildASServer(t, e.handle, []string{chatGPTRedirect}, nil, time.Now)
		status, body := e.refresh(t, refresh)
		wantTokenError(t, status, body, "invalid_client", "invalid_grant")
	})
}

func TestLoggedOutAuthorizeBouncesThroughLogin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		q := e.authorizeQuery("books:read books:write", e.srv.ResourceURL())
		authorize := "/oauth/authorize?" + q.Encode()

		rec := e.getAuthorize(t, q, nil)
		want := "/login?next=" + url.QueryEscape(authorize)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
			t.Fatalf("logged out authorize = %d → %q, want 303 → %q", rec.Code, rec.Header().Get("Location"), want)
		}

		login := e.loginWithNext(t, authorize)
		if login.Code != http.StatusSeeOther || login.Header().Get("Location") != authorize {
			t.Errorf("login = %d → %q, want 303 back to %q", login.Code, login.Header().Get("Location"), authorize)
		}
	})
}

// revokeOK posts a token to /oauth/revoke as the client's server would.
func (e *flowEnv) revokeOK(t *testing.T, token string) {
	t.Helper()
	rec := e.postForm(t, "/oauth/revoke", url.Values{"token": {token}, "client_id": {e.clientID}}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /oauth/revoke = %d %q, want 200", rec.Code, rec.Body.String())
	}
}

// The same flow as TestFullFlowDCRConsentTokenRefresh for a client that
// identifies by its metadata document URL (CIMD), which is how Claude and
// ChatGPT connect when they can: every step resolves the client through the
// CIMD resolver and its cache rather than the oauth_clients table.
func TestFullFlowCIMD(t *testing.T) {
	host := newCIMDHost(t, claudeRedirect)
	synctest.Test(t, func(t *testing.T) {
		e := newCIMDFlowEnv(t, host)
		ctx := context.Background()

		page := e.getAuthorize(t, e.authorizeQuery("books:read books:write", e.srv.ResourceURL()), e.cookies)
		if page.Code != http.StatusOK {
			t.Fatalf("authorize = %d, want 200 consent; Location %q", page.Code, page.Header().Get("Location"))
		}
		if text := htmlassert.Parse(t, page.Body.String()).Text(); !strings.Contains(text, "Test assistant") {
			t.Error("consent page does not name the client from its metadata document")
		}
		access, refresh := e.exchangeOK(t, e.approve(t, page, "books:read"))

		c, err := e.srv.Verify(ctx, access)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		g, ok := e.grant(t)
		if !ok {
			t.Fatal("no grant row after consent")
		}
		if c.UserID != e.user.ID || c.GrantID != g.ID || c.ClientName != "Test assistant" || !slices.Equal(c.Scopes, []string{"books:read"}) {
			t.Errorf("Verify = %+v, want ilia's books:read grant %s for Test assistant", c, g.ID)
		}

		rotatedAccess, rotated := e.refreshOK(t, refresh)
		if _, err := e.srv.Verify(ctx, rotatedAccess); err != nil {
			t.Errorf("Verify(refreshed access token): %v", err)
		}

		// Reusing the rotated-out refresh token revokes the whole grant.
		status, body := e.refresh(t, refresh)
		wantTokenError(t, status, body, "invalid_grant")
		if _, ok := e.grant(t); ok {
			t.Error("reusing a rotated refresh token left the grant in place")
		}
		status, body = e.refresh(t, rotated)
		wantTokenError(t, status, body, "invalid_grant")
		if _, err := e.srv.Verify(ctx, rotatedAccess); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Verify after reuse = %v, want ErrInvalidToken", err)
		}

		// A second connection, ended by the client through /oauth/revoke.
		access2, refresh2 := e.exchangeOK(t, e.connect(t, "books:read", "books:read"))
		if _, ok := e.grant(t); !ok {
			t.Fatal("no grant row after the second consent")
		}
		e.revokeOK(t, refresh2)
		if _, ok := e.grant(t); ok {
			t.Error("revoking the refresh token left the second grant in place")
		}
		status, body = e.refresh(t, refresh2)
		wantTokenError(t, status, body, "invalid_grant")
		if _, err := e.srv.Verify(ctx, access2); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Verify after revoke = %v, want ErrInvalidToken", err)
		}

		// Everything above ran inside the document's one-hour cache.
		if n := host.hits.Load(); n != 1 {
			t.Errorf("document fetched %d times, want 1", n)
		}
	})
}

// A document served with no-store or max-age=0 is never usable from the
// cache, so the resolver fetches it on every client lookup, including the
// ones the library makes inside its token-endpoint transaction (rebuilding
// the stored code or refresh session's client). The flow must still work.
//
// Fake time stands still here, so the resolver's per-domain fetch budget
// (a burst of 20, refilled at 10 a second) never refills: this flow must
// stay within 20 fetches. It takes about 11 because the in-transaction
// lookups reuse the authenticated client (clientMemo); before that they
// fetched too, and the burst ran out at the revoke.
func TestCIMDFlowWithUncacheableDocument(t *testing.T) {
	for _, cc := range []string{"no-store", "max-age=0"} {
		t.Run(cc, func(t *testing.T) {
			host := newCIMDHost(t, claudeRedirect)
			host.cacheControl.Store(cc)
			synctest.Test(t, func(t *testing.T) {
				e := newCIMDFlowEnv(t, host)
				access, refresh := e.exchangeOK(t, e.connect(t, "books:read", "books:read"))
				if _, err := e.srv.Verify(context.Background(), access); err != nil {
					t.Errorf("Verify: %v", err)
				}
				_, rotated := e.refreshOK(t, refresh)
				status, body := e.refresh(t, refresh)
				wantTokenError(t, status, body, "invalid_grant")
				if _, ok := e.grant(t); ok {
					t.Error("reusing a rotated refresh token left the grant in place")
				}
				status, body = e.refresh(t, rotated)
				wantTokenError(t, status, body, "invalid_grant")

				_, refresh2 := e.exchangeOK(t, e.connect(t, "books:read", "books:read"))
				e.revokeOK(t, refresh2)
				if _, ok := e.grant(t); ok {
					t.Error("revoke left the grant in place")
				}
			})
		})
	}
}

// The library rebuilds a stored session's client inside its token-endpoint
// transaction, which holds the database's only connection. If that lookup
// could join a CIMD fetch another request started, the two would wait on
// each other: the fetch must write its result to the database before it
// finishes, and the transaction does not let go of the connection until the
// fetch finishes. Every database request, the whole suite's, then hangs.
// Inside a transaction the store must therefore never wait on a fetch.
func TestSessionClientInsideTransactionNeverWaitsOnAFetch(t *testing.T) {
	host := newCIMDHost(t, claudeRedirect)
	synctest.Test(t, func(t *testing.T) {
		// Made inside the bubble, so waiting on it counts as durably blocked.
		host.gate = make(chan struct{})
		e := newCIMDFlowEnv(t, host)
		store := e.srv.store

		// Request B, outside any transaction (as /mcp's Verify is), starts
		// the fetch, which is held at the host.
		fetched := make(chan error, 1)
		go func() {
			_, err := e.srv.clients.GetClient(context.Background(), e.clientID)
			fetched <- err
		}()
		synctest.Wait()
		if host.hits.Load() != 1 {
			t.Fatalf("fetches in flight = %d, want 1", host.hits.Load())
		}

		// Request A, a token request, decodes a session for the same client
		// inside its transaction.
		req := oauth2.NewRequest()
		req.ID = "grant"
		req.Client = &oauth2.DefaultClient{ID: e.clientID}
		req.Session = new(oauth2.DefaultSession)
		data, err := encodeRequest(req)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		txCtx, err := store.BeginTX(ctx)
		if err != nil {
			t.Fatal(err)
		}
		decoded := make(chan error, 1)
		go func() {
			_, err := store.decodeRequest(txCtx, data, nil)
			decoded <- err
		}()
		synctest.Wait()
		close(host.gate)

		start := time.Now()
		err = <-decoded
		if waited := time.Since(start); waited >= requestTimeout {
			t.Errorf("decode inside the transaction waited %v for the other request's fetch (err %v)", waited, err)
		}
		// With no authenticated client to reuse, the session is simply not
		// found; in a real token request the memo supplies the client.
		if !errors.Is(err, oauth2.ErrNotFound) {
			t.Errorf("decode inside the transaction err = %v, want oauth2.ErrNotFound", err)
		}
		if err := store.Rollback(txCtx); err != nil {
			t.Fatal(err)
		}
		if err := <-fetched; err != nil {
			t.Errorf("the outside request's client lookup failed: %v", err)
		}
	})
}

// Once the cached document is past its lifetime, the next token request
// has the resolver fetch it again, and the refresh still succeeds.
func TestCIMDRefreshRefetchesExpiredDocument(t *testing.T) {
	host := newCIMDHost(t, claudeRedirect)
	synctest.Test(t, func(t *testing.T) {
		e := newCIMDFlowEnv(t, host)
		ctx := context.Background()
		_, refresh := e.exchangeOK(t, e.connect(t, "books:read", "books:read"))
		if n := host.hits.Load(); n != 1 {
			t.Fatalf("document fetched %d times, want 1", n)
		}

		time.Sleep(2 * time.Hour) // the host sends max-age=3600
		access, rotated := e.refreshOK(t, refresh)
		if n := host.hits.Load(); n != 2 {
			t.Errorf("document fetched %d times, want 2 (re-fetched once expired)", n)
		}
		if _, err := e.srv.Verify(ctx, access); err != nil {
			t.Errorf("Verify(refreshed access token): %v", err)
		}

		// Expired again: revocation resolves the client too.
		time.Sleep(2 * time.Hour)
		e.revokeOK(t, rotated)
		if _, ok := e.grant(t); ok {
			t.Error("revoke after a re-fetch left the grant in place")
		}
		if n := host.hits.Load(); n != 3 {
			t.Errorf("document fetched %d times, want 3", n)
		}
	})
}
