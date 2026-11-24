package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/config"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// The end-to-end test drives the real stack the way an AI client and its
// user would: registration, sign-in, consent, token, then MCP calls with the
// SDK's own client. The database has one connection, so every step runs
// under e2eTimeout: a request stuck waiting for a second connection fails
// the test instead of hanging it.

const (
	e2ePublicURL = "https://onsuite.test"
	e2eRedirect  = "https://assistant.test/callback"
	e2eVerifier  = "e2e-test-verifier-0123456789-abcdefghijklmnopqrstuvwxyz"
	e2eState     = "e2e-state-0123456789"
	e2eTimeout   = 10 * time.Second
	// hostileName is the client's own claim about itself, so pages must
	// treat it as text.
	hostileName = "<img src=x onerror=alert(1)>"
)

type e2eEnv struct {
	handler http.Handler
	handle  *sql.DB
	users   *auth.Store
}

func newE2EEnv(t *testing.T, mcpOn bool) *e2eEnv {
	t.Helper()
	cfg := config.Config{DataDir: t.TempDir(), MCP: mcpOn}
	if mcpOn {
		cfg.PublicURL = e2ePublicURL
		cfg.OAuthRedirectURIs = []string{e2eRedirect}
	}
	handle, registry, _, err := openDatabase(context.Background(), cfg)
	if err != nil {
		t.Fatalf("openDatabase: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	oauthServer, err := newOAuthServer(context.Background(), handle, cfg)
	if err != nil {
		t.Fatalf("newOAuthServer: %v", err)
	}
	users := auth.NewStore(handle)
	stack, err := buildStack(stackDeps{
		DB:       handle,
		Users:    users,
		Registry: registry,
		Log:      slog.New(slog.DiscardHandler),
		Version:  "test",
		Config:   cfg,
		Jobs:     jobs.NewRegistry(),
		OAuth:    oauthServer,
	})
	if err != nil {
		t.Fatalf("buildStack: %v", err)
	}
	return &e2eEnv{handler: stack, handle: handle, users: users}
}

// serve runs one request through the stack under e2eTimeout.
func (e *e2eEnv) serve(t *testing.T, req *http.Request, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithTimeout(req.Context(), e2eTimeout)
	defer cancel()
	req = req.WithContext(ctx)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func (e *e2eEnv) get(t *testing.T, path string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return e.serve(t, httptest.NewRequest(http.MethodGet, e2ePublicURL+path, nil), cookies)
}

func (e *e2eEnv) postForm(t *testing.T, path string, form url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, e2ePublicURL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return e.serve(t, req, cookies)
}

func (e *e2eEnv) addUser(t *testing.T, name string) auth.User {
	t.Helper()
	u, err := e.users.CreateUser(context.Background(), name, apptest.PasswordHash, false)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// addBook shelves a book (no readings: Want to read) and returns its ID.
func (e *e2eEnv) addBook(t *testing.T, userID int64, title string) int64 {
	t.Helper()
	now := db.FormatTime(time.Now())
	res, err := e.handle.Exec(`INSERT INTO books_books (user_id, title, added_at, updated_at) VALUES (?, ?, ?, ?)`,
		userID, title, now, now)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// logIn signs in through the real login form and returns the cookies.
func (e *e2eEnv) logIn(t *testing.T, username string) []*http.Cookie {
	t.Helper()
	page := e.get(t, "/login", nil)
	var csrf *http.Cookie
	for _, c := range page.Result().Cookies() {
		if c.Name == web.CSRFCookieName {
			csrf = c
		}
	}
	if csrf == nil {
		t.Fatal("GET /login issued no CSRF cookie")
	}
	rec := e.postForm(t, "/login",
		url.Values{"username": {username}, "password": {apptest.Password}, web.CSRFFormField: {csrf.Value}},
		[]*http.Cookie{csrf})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login = %d", rec.Code)
	}
	return rec.Result().Cookies()
}

// register is dynamic client registration with no cookie, as an AI client
// does it.
func (e *e2eEnv) register(t *testing.T, clientName string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"redirect_uris":              []string{e2eRedirect},
		"client_name":                clientName,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, e2ePublicURL+"/oauth/register", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := e.serve(t, req, nil)
	var out struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusCreated || out.ClientID == "" {
		t.Fatalf("POST /oauth/register = %d %q", rec.Code, rec.Body.String())
	}
	return out.ClientID
}

// hiddenForm collects the hidden fields of the form posting to action.
func hiddenForm(t *testing.T, body, action string) url.Values {
	t.Helper()
	doc := htmlassert.Parse(t, body)
	sel := `form[action="` + action + `"]`
	doc.MustHave(sel)
	form := url.Values{}
	for _, in := range doc.QueryAll(sel + ` input[type=hidden]`) {
		name, _ := htmlassert.Attr(in, "name")
		value, _ := htmlassert.Attr(in, "value")
		form.Add(name, value)
	}
	return form
}

// connect runs the browser half of the flow (authorize, consent with
// books:read ticked) and the client's code exchange, returning the access
// token.
func (e *e2eEnv) connect(t *testing.T, clientID string, cookies []*http.Cookie) string {
	t.Helper()
	sum := sha256.Sum256([]byte(e2eVerifier))
	q := url.Values{
		"client_id":             {clientID},
		"response_type":         {"code"},
		"redirect_uri":          {e2eRedirect},
		"state":                 {e2eState},
		"scope":                 {"books:read"},
		"resource":              {e2ePublicURL + "/mcp"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	page := e.get(t, "/oauth/authorize?"+q.Encode(), cookies)
	if page.Code != http.StatusOK {
		t.Fatalf("authorize = %d, want the consent page; Location %q", page.Code, page.Header().Get("Location"))
	}
	form := hiddenForm(t, page.Body.String(), "/oauth/authorize")
	form.Set("decision", "allow")
	form.Set("grant_scope", "books:read")
	rec := e.postForm(t, "/oauth/authorize", form, cookies)
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || rec.Code/100 != 3 {
		t.Fatalf("consent = %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := loc.Scheme + "://" + loc.Host + loc.Path; got != e2eRedirect {
		t.Fatalf("consent redirects to %q, want %q", got, e2eRedirect)
	}
	if loc.Query().Get("state") != e2eState || loc.Query().Get("code") == "" {
		t.Fatalf("consent redirect query = %v", loc.Query())
	}

	tok := e.postForm(t, "/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {loc.Query().Get("code")},
		"redirect_uri":  {e2eRedirect},
		"code_verifier": {e2eVerifier},
		"client_id":     {clientID},
	}, nil)
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(tok.Body.Bytes(), &out); err != nil || tok.Code != http.StatusOK || out.AccessToken == "" {
		t.Fatalf("POST /oauth/token = %d %q", tok.Code, tok.Body.String())
	}
	return out.AccessToken
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// mcpSession connects the SDK's client to /mcp on a real listener, so the
// streamable transport runs as it does in production.
func mcpSession(ctx context.Context, t *testing.T, base, token string) *sdk.ClientSession {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "e2e", Version: "1"}, nil)
	cs, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint:             base + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerTransport{token}, Timeout: e2eTimeout},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func toolText(t *testing.T, res *sdk.CallToolResult) string {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("content = %v, want one text item", res.Content)
	}
	tc, ok := res.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want text", res.Content[0])
	}
	return tc.Text
}

// required reads a tool input schema's "required" list.
func required(t *testing.T, schema any) []string {
	t.Helper()
	b, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s.Required
}

func TestMCPEndToEnd(t *testing.T) {
	e := newE2EEnv(t, true)
	alice := e.addUser(t, "alice")
	bob := e.addUser(t, "bob")
	e.addBook(t, alice.ID, "Dune")
	bobsBook := e.addBook(t, bob.ID, "Bob's secret diary")

	clientID := e.register(t, hostileName)
	cookies := e.logIn(t, "alice")
	token := e.connect(t, clientID, cookies)

	srv := httptest.NewServer(e.handler)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), e2eTimeout)
	defer cancel()
	cs := mcpSession(ctx, t, srv.URL, token)

	if got := cs.InitializeResult().ServerInfo.Version; got != "test" {
		t.Errorf("server version = %q, want the build version %q", got, "test")
	}

	list, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	var names []string
	schemas := map[string]any{}
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
		schemas[tool.Name] = tool.InputSchema
	}
	slices.Sort(names)
	if want := []string{"books_get", "books_list", "books_stats"}; !slices.Equal(names, want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
	if got := required(t, schemas["books_get"]); !slices.Equal(got, []string{"id"}) {
		t.Errorf("books_get requires %v, want [id]", got)
	}
	if got := required(t, schemas["books_list"]); len(got) != 0 {
		t.Errorf("books_list requires %v, want nothing", got)
	}

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "books_list", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("books_list: %v", err)
	}
	if text := toolText(t, res); res.IsError || !strings.Contains(text, "Dune") || strings.Contains(text, "secret diary") {
		t.Errorf("books_list = %q (error %v), want alice's book only", text, res.IsError)
	}

	res, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: "books_get", Arguments: map[string]any{"id": bobsBook}})
	if err != nil {
		t.Fatalf("books_get: %v", err)
	}
	if text := toolText(t, res); !res.IsError || !strings.Contains(text, "No book with that ID") {
		t.Errorf("books_get on bob's book = %q (error %v), want the not-found tool error", text, res.IsError)
	}

	// The connection page lists the grant, with the client's own name as
	// text, never markup.
	page := e.get(t, "/account/assistants", cookies)
	if page.Code != http.StatusOK {
		t.Fatalf("GET /account/assistants = %d", page.Code)
	}
	body := page.Body.String()
	if strings.Contains(body, hostileName) {
		t.Error("/account/assistants prints the client name unescaped")
	}
	if !strings.Contains(body, "&lt;img src=x onerror=alert(1)&gt;") {
		t.Error("/account/assistants does not show the client name as escaped text")
	}

	doc := htmlassert.Parse(t, body)
	var actions []string
	for _, f := range doc.QueryAll("form") {
		if a, _ := htmlassert.Attr(f, "action"); strings.HasPrefix(a, "/account/assistants/") {
			actions = append(actions, a)
		}
	}
	if len(actions) != 1 {
		t.Fatalf("disconnect forms = %v, want 1", actions)
	}
	action := actions[0]
	if rec := e.postForm(t, action, hiddenForm(t, body, action), cookies); rec.Code != http.StatusSeeOther {
		t.Fatalf("disconnect = %d", rec.Code)
	}

	// After disconnecting, the same token is refused outright.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("tools/list after disconnect = %d, want 401", resp.StatusCode)
	}
}

func TestMCPOffMountsNothing(t *testing.T) {
	e := newE2EEnv(t, false)
	if _, err := e.users.CreateUser(context.Background(), "alice", apptest.PasswordHash, true); err != nil {
		t.Fatal(err)
	}
	cookies := e.logIn(t, "alice") // an admin, so /admin/ai would be reachable if mounted

	// GET throughout: with AI access off, a POST to an unexempted path is
	// refused by the CSRF check before routing, which would hide whether the
	// route exists. A mounted /oauth/token would answer GET with 405.
	for _, path := range []string{
		"/mcp",
		"/oauth/token",
		"/oauth/authorize",
		"/.well-known/oauth-authorization-server",
		"/.well-known/oauth-protected-resource",
		"/account/assistants",
		"/admin/ai",
	} {
		if rec := e.get(t, path, cookies); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404 with AI access off", path, rec.Code)
		}
	}
	if strings.Contains(e.get(t, "/account", cookies).Body.String(), "/account/assistants") {
		t.Error("/account links to connected assistants with AI access off")
	}
	admin := e.get(t, "/admin", cookies)
	if admin.Code != http.StatusOK {
		t.Fatalf("GET /admin as an admin = %d", admin.Code)
	}
	if strings.Contains(admin.Body.String(), "/admin/ai") {
		t.Error("/admin links to AI access with AI access off")
	}
}

// TestMCPOnLinksAccountPages: with AI access on, /account links to the
// connected-assistants page.
func TestMCPOnLinksAccountPages(t *testing.T) {
	e := newE2EEnv(t, true)
	e.addUser(t, "alice")
	cookies := e.logIn(t, "alice")
	if !strings.Contains(e.get(t, "/account", cookies).Body.String(), `href="/account/assistants"`) {
		t.Error("/account does not link to connected assistants with AI access on")
	}
}
