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

	oauthServer, err := newOAuthServer(context.Background(), handle, cfg, slog.New(slog.DiscardHandler))
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

// connect runs the browser half of the flow (authorize, consent with the
// scopes ticked) and the client's code exchange, returning the access
// token. No scopes means books:read.
func (e *e2eEnv) connect(t *testing.T, clientID string, cookies []*http.Cookie, scopes ...string) string {
	t.Helper()
	if len(scopes) == 0 {
		scopes = []string{"books:read"}
	}
	sum := sha256.Sum256([]byte(e2eVerifier))
	q := url.Values{
		"client_id":             {clientID},
		"response_type":         {"code"},
		"redirect_uri":          {e2eRedirect},
		"state":                 {e2eState},
		"scope":                 {strings.Join(scopes, " ")},
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
	form["grant_scope"] = scopes
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

func TestMCPBooksWriteNeedsWriteScope(t *testing.T) {
	e := newE2EEnv(t, true)
	alice := e.addUser(t, "alice")
	bob := e.addUser(t, "bob")
	dune := e.addBook(t, alice.ID, "Dune")
	bobsBook := e.addBook(t, bob.ID, "Bob's secret diary")
	cookies := e.logIn(t, "alice")

	srv := httptest.NewServer(e.handler)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), e2eTimeout)
	defer cancel()

	call := func(cs *sdk.ClientSession, name string, args map[string]any) (*sdk.CallToolResult, error) {
		return cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	}

	// A read-only grant lists no write tool and cannot call one.
	readOnly := mcpSession(ctx, t, srv.URL, e.connect(t, e.register(t, "Reader"), cookies))
	list, err := readOnly.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	for _, tool := range list.Tools {
		if !slices.Contains([]string{"books_get", "books_list", "books_stats"}, tool.Name) {
			t.Errorf("read-only grant lists %s", tool.Name)
		}
	}
	if len(list.Tools) != 3 {
		t.Errorf("read-only grant lists %d tools, want the 3 read tools", len(list.Tools))
	}
	if res, err := call(readOnly, "books_start_reading", map[string]any{"id": dune}); err == nil && !res.IsError {
		t.Errorf("books_start_reading on a read-only grant succeeded: %q", toolText(t, res))
	}

	// A grant with books:write lists all six write tools, with annotations.
	cs := mcpSession(ctx, t, srv.URL, e.connect(t, e.register(t, "Writer"), cookies, "books:read", "books:write"))
	list, err = cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	idem := map[string]bool{}
	for _, tool := range list.Tools {
		idem[tool.Name] = tool.Annotations != nil && tool.Annotations.IdempotentHint
	}
	for _, name := range []string{"books_start_reading", "books_update_progress", "books_finish", "books_mark_dnf", "books_add_note", "books_add_quote"} {
		if _, ok := idem[name]; !ok {
			t.Errorf("write grant does not list %s", name)
		}
	}
	if !idem["books_start_reading"] {
		t.Error("books_start_reading idempotentHint = false, want true")
	}
	if idem["books_add_note"] {
		t.Error("books_add_note idempotentHint = true, want false")
	}

	// Writes land, and books_get shows them.
	for _, c := range []struct {
		name string
		args map[string]any
	}{
		{"books_start_reading", map[string]any{"id": dune}},
		{"books_update_progress", map[string]any{"id": dune, "percent": 50}},
	} {
		res, err := call(cs, c.name, c.args)
		if err != nil || res.IsError {
			t.Fatalf("%s: %v %v", c.name, err, res)
		}
	}
	res, err := call(cs, "books_get", map[string]any{"id": dune})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Shelf    string `json:"shelf"`
		Progress any    `json:"progress"`
	}
	if err := json.Unmarshal([]byte(toolText(t, res)), &got); err != nil {
		t.Fatalf("books_get: %v", err)
	}
	if got.Shelf != "reading" || got.Progress == nil {
		t.Errorf("books_get = %+v, want shelf reading with progress", got)
	}

	// Bob's book is refused as not found and stays on Want.
	res, err = call(cs, "books_start_reading", map[string]any{"id": bobsBook})
	if err != nil {
		t.Fatal(err)
	}
	if text := toolText(t, res); !res.IsError || !strings.Contains(text, "No book with that ID on your shelves") {
		t.Errorf("start on bob's book = %q (error %v)", text, res.IsError)
	}
	var readings int
	if err := e.handle.QueryRow(`SELECT COUNT(*) FROM books_readings WHERE book_id = ?`, bobsBook).Scan(&readings); err != nil {
		t.Fatal(err)
	}
	if readings != 0 {
		t.Errorf("bob's book has %d readings, want 0 (still on Want)", readings)
	}
}

// addNote puts a top-level bullet in a user's outline and returns its ID.
func (e *e2eEnv) addNote(t *testing.T, userID int64, title string) int64 {
	t.Helper()
	now := db.FormatTime(time.Now())
	res, err := e.handle.Exec(`INSERT INTO notes_nodes (user_id, parent_id, position, title, note, created_at, updated_at)
		VALUES (?, NULL, 0, ?, '', ?, ?)`, userID, title, now, now)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMCPNotesTools(t *testing.T) {
	e := newE2EEnv(t, true)
	e.addUser(t, "alice")
	bob := e.addUser(t, "bob")
	bobsNote := e.addNote(t, bob.ID, "Bob's secret plan")
	cookies := e.logIn(t, "alice")

	srv := httptest.NewServer(e.handler)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), e2eTimeout)
	defer cancel()

	call := func(cs *sdk.ClientSession, name string, args map[string]any) *sdk.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return res
	}
	toolNames := func(cs *sdk.ClientSession) []string {
		t.Helper()
		list, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		var names []string
		for _, tool := range list.Tools {
			names = append(names, tool.Name)
		}
		slices.Sort(names)
		return names
	}

	// A books-only grant lists no notes tool.
	booksOnly := mcpSession(ctx, t, srv.URL, e.connect(t, e.register(t, "Books"), cookies, "books:read"))
	for _, name := range toolNames(booksOnly) {
		if strings.HasPrefix(name, "notes_") {
			t.Errorf("books-only grant lists %s", name)
		}
	}

	// notes:read lists the three read tools and no write tool.
	reader := mcpSession(ctx, t, srv.URL, e.connect(t, e.register(t, "Notes reader"), cookies, "notes:read"))
	if got, want := toolNames(reader), []string{"notes_due", "notes_get", "notes_search"}; !slices.Equal(got, want) {
		t.Errorf("notes:read lists %v, want %v", got, want)
	}

	// notes:read plus notes:write adds and ticks bullets.
	cs := mcpSession(ctx, t, srv.URL, e.connect(t, e.register(t, "Notes writer"), cookies, "notes:read", "notes:write"))
	wantAll := []string{"notes_add", "notes_due", "notes_get", "notes_search", "notes_set_done", "notes_set_due"}
	if got := toolNames(cs); !slices.Equal(got, wantAll) {
		t.Errorf("write grant lists %v, want %v", got, wantAll)
	}

	type added struct {
		ID int64 `json:"id"`
	}
	res := call(cs, "notes_add", map[string]any{"text": "Groceries"})
	if res.IsError {
		t.Fatalf("notes_add Groceries: %q", toolText(t, res))
	}
	var groceries added
	if err := json.Unmarshal([]byte(toolText(t, res)), &groceries); err != nil || groceries.ID == 0 {
		t.Fatalf("notes_add Groceries = %q: %v", toolText(t, res), err)
	}
	res = call(cs, "notes_add", map[string]any{"text": "Milk", "parent_id": groceries.ID})
	if res.IsError {
		t.Fatalf("notes_add Milk: %q", toolText(t, res))
	}
	var milk added
	if err := json.Unmarshal([]byte(toolText(t, res)), &milk); err != nil || milk.ID == 0 {
		t.Fatalf("notes_add Milk = %q: %v", toolText(t, res), err)
	}

	res = call(cs, "notes_get", map[string]any{})
	if text := toolText(t, res); res.IsError || !strings.Contains(text, "- Groceries") || !strings.Contains(text, "\n  - Milk") {
		t.Errorf("notes_get = %q (error %v), want Groceries with Milk indented under it", text, res.IsError)
	}

	res = call(cs, "notes_set_done", map[string]any{"id": milk.ID, "done": true})
	if text := toolText(t, res); res.IsError || !strings.Contains(text, `"done":true`) {
		t.Errorf("notes_set_done = %q (error %v), want done true", text, res.IsError)
	}

	res = call(cs, "notes_set_due", map[string]any{"id": groceries.ID, "due": "2099-01-01"})
	if text := toolText(t, res); res.IsError || !strings.Contains(text, `"due":"2099-01-01"`) {
		t.Errorf("notes_set_due = %q (error %v), want due 2099-01-01", text, res.IsError)
	}
	res = call(cs, "notes_due", map[string]any{})
	if text := toolText(t, res); res.IsError || !strings.Contains(text, `"later":[{"id":`) || !strings.Contains(text, "Groceries") {
		t.Errorf("notes_due = %q (error %v), want Groceries under later", text, res.IsError)
	}

	res = call(cs, "notes_search", map[string]any{"query": "milk"})
	if text := toolText(t, res); res.IsError || !strings.Contains(text, "Milk") || !strings.Contains(text, "Groceries") {
		t.Errorf("notes_search = %q (error %v), want Milk under Groceries", text, res.IsError)
	}

	// Bob's bullet is refused as not found and stays not done.
	res = call(cs, "notes_set_done", map[string]any{"id": bobsNote, "done": true})
	if text := toolText(t, res); !res.IsError || !strings.Contains(text, "No note with that ID in your outline") {
		t.Errorf("set_done on bob's note = %q (error %v)", text, res.IsError)
	}
	var doneAt sql.NullString
	if err := e.handle.QueryRow(`SELECT done_at FROM notes_nodes WHERE id = ?`, bobsNote).Scan(&doneAt); err != nil {
		t.Fatal(err)
	}
	if doneAt.Valid {
		t.Errorf("bob's note done_at = %q, want NULL", doneAt.String)
	}
}

// addReaderItem subscribes a user to a new feed holding one item with the
// given feed-supplied text, and returns the item's ID.
func (e *e2eEnv) addReaderItem(t *testing.T, userID int64, name, title, body string) int64 {
	t.Helper()
	old := db.FormatTime(time.Now().Add(-48 * time.Hour))
	now := db.FormatTime(time.Now())
	feedURL := "https://" + name + ".example/feed"
	res, err := e.handle.Exec(`INSERT INTO reader_feeds (url, resolved_url, title, site_url, next_fetch_at)
		VALUES (?, ?, ?, ?, ?)`, feedURL, feedURL, name, "https://"+name+".example/", now)
	if err != nil {
		t.Fatal(err)
	}
	feedID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.handle.Exec(`INSERT INTO reader_subs (user_id, feed_id, added_at) VALUES (?, ?, ?)`, userID, feedID, old); err != nil {
		t.Fatal(err)
	}
	res, err = e.handle.Exec(`INSERT INTO reader_items (feed_id, guid, url, title, published_at, fetched_at, content_html)
		VALUES (?, 'g1', ?, ?, ?, ?, ?)`, feedID, "https://"+name+".example/post", title, now, now, body)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// addLaterArticle saves an extracted article for a user and returns its ID.
func (e *e2eEnv) addLaterArticle(t *testing.T, userID int64, name, title, html, text string) int64 {
	t.Helper()
	now := db.FormatTime(time.Now())
	res, err := e.handle.Exec(`INSERT INTO later_articles (user_id, url, title, site_host, content, content_html, content_text, word_count, saved_at, updated_at)
		VALUES (?, ?, ?, ?, 'extracted', ?, ?, 3, ?, ?)`, userID, "https://"+name+".example/a", title, name+".example", html, text, now, now)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMCPReaderAndLaterTools(t *testing.T) {
	e := newE2EEnv(t, true)
	alice := e.addUser(t, "alice")
	bob := e.addUser(t, "bob")
	aliceItem := e.addReaderItem(t, alice.ID, "alicefeed", "Tapir news", "<p>Tapirs swim at night.</p>")
	bobItem := e.addReaderItem(t, bob.ID, "bobfeed", "Bob's feed post", "<p>Bob private text.</p>")
	aliceSaved := e.addLaterArticle(t, alice.ID, "alicelater", "Sourdough guide", "<p>Feed the starter daily.</p>", "Feed the starter daily.")
	bobSaved := e.addLaterArticle(t, bob.ID, "boblater", "Bob's saved page", "<p>Bob secret.</p>", "Bob secret.")
	cookies := e.logIn(t, "alice")

	srv := httptest.NewServer(e.handler)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), e2eTimeout)
	defer cancel()

	call := func(cs *sdk.ClientSession, name string, args map[string]any) *sdk.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return res
	}
	toolNames := func(cs *sdk.ClientSession) []string {
		t.Helper()
		list, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		var names []string
		for _, tool := range list.Tools {
			names = append(names, tool.Name)
		}
		slices.Sort(names)
		return names
	}

	readOnly := mcpSession(ctx, t, srv.URL, e.connect(t, e.register(t, "Readers"), cookies, "reader:read", "later:read"))
	wantRead := []string{"later_get_article", "later_list", "later_search", "reader_get_article", "reader_list", "reader_search"}
	if got := toolNames(readOnly); !slices.Equal(got, wantRead) {
		t.Errorf("read grant lists %v, want %v", got, wantRead)
	}

	cs := mcpSession(ctx, t, srv.URL, e.connect(t, e.register(t, "Writers"), cookies,
		"reader:read", "reader:write", "later:read", "later:write"))
	wantAll := []string{"later_get_article", "later_list", "later_save", "later_search", "later_set_state", "later_tag",
		"reader_get_article", "reader_list", "reader_mark_read", "reader_search", "reader_star"}
	if got := toolNames(cs); !slices.Equal(got, wantAll) {
		t.Errorf("full grant lists %v, want %v", got, wantAll)
	}

	res := call(cs, "reader_get_article", map[string]any{"id": aliceItem})
	if text := toolText(t, res); res.IsError || !strings.Contains(text, "<untrusted_article source=") || !strings.Contains(text, "Tapirs swim at night.") {
		t.Errorf("reader_get_article = %q (error %v), want an untrusted block with the text", text, res.IsError)
	}
	res = call(cs, "reader_star", map[string]any{"id": aliceItem, "starred": true})
	if text := toolText(t, res); res.IsError || !strings.Contains(text, `"starred":true`) {
		t.Errorf("reader_star = %q (error %v), want starred true", text, res.IsError)
	}

	res = call(cs, "later_tag", map[string]any{"id": aliceSaved, "add": []string{"ai"}})
	if text := toolText(t, res); res.IsError || !strings.Contains(text, `"ai"`) {
		t.Errorf("later_tag = %q (error %v), want the ai tag", text, res.IsError)
	}
	res = call(cs, "later_set_state", map[string]any{"id": aliceSaved, "state": "archived"})
	if text := toolText(t, res); res.IsError || !strings.Contains(text, `"state":"archived"`) {
		t.Errorf("later_set_state = %q (error %v), want archived", text, res.IsError)
	}
	res = call(cs, "later_list", map[string]any{"state": "archived"})
	if text := toolText(t, res); res.IsError || !strings.Contains(text, "Sourdough guide") {
		t.Errorf("later_list archived = %q (error %v), want the article", text, res.IsError)
	}

	// Bob's articles are refused as not found.
	res = call(cs, "reader_get_article", map[string]any{"id": bobItem})
	if text := toolText(t, res); !res.IsError || !strings.Contains(text, "No article with that ID in your feeds") {
		t.Errorf("reader_get_article on bob's item = %q (error %v)", text, res.IsError)
	}
	res = call(cs, "later_get_article", map[string]any{"id": bobSaved})
	if text := toolText(t, res); !res.IsError || !strings.Contains(text, "No article with that ID in Later") {
		t.Errorf("later_get_article on bob's article = %q (error %v)", text, res.IsError)
	}
}
