package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/oauth"
)

const (
	testResource    = "https://onsuite.test/mcp"
	testIssuer      = "https://onsuite.test"
	wantMetadataURL = "https://onsuite.test/.well-known/oauth-protected-resource/mcp"
)

type auditRow struct {
	UserID     int64
	ClientName string
	Tool       string
	OK         bool
	ErrorKind  string
}

type fixture struct {
	srv *httptest.Server
	log *bytes.Buffer

	mu     sync.Mutex
	audits []auditRow
}

func (f *fixture) auditRows() []auditRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.audits)
}

var testNow = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

// callers maps bearer tokens to who they speak for; any other token fails.
func newFixture(t *testing.T, tools []app.Tool, callers map[string]oauth.Caller) *fixture {
	t.Helper()
	f := &fixture{log: new(bytes.Buffer)}
	mux := http.NewServeMux()
	Routes(mux, nil, Deps{
		Tools: tools,
		Verify: func(_ context.Context, bearer string) (oauth.Caller, error) {
			c, ok := callers[bearer]
			if !ok {
				return oauth.Caller{}, oauth.ErrInvalidToken
			}
			return c, nil
		},
		Audit: func(_ context.Context, userID int64, clientName, tool string, ok bool, errorKind string) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.audits = append(f.audits, auditRow{userID, clientName, tool, ok, errorKind})
		},
		ResourceURL: testResource,
		Issuer:      testIssuer,
		Log:         slog.New(slog.NewTextHandler(f.log, nil)),
		Now:         func() time.Time { return testNow },
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func caller(userID int64, grant string, scopes ...string) oauth.Caller {
	return oauth.Caller{
		UserID:     userID,
		GrantID:    grant,
		ClientName: "Claude",
		Scopes:     scopes,
		Expires:    time.Now().Add(time.Hour),
	}
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// connect opens an MCP client session against the fixture as token.
func (f *fixture) connect(t *testing.T, token string) *sdk.ClientSession {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{
		Endpoint:             f.srv.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerTransport{token}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

type listArgs struct {
	Query string `json:"query,omitempty" jsonschema:"words to look for"`
}

func readTool(name string, call func(context.Context, int64, json.RawMessage) (string, error)) app.Tool {
	scope := strings.SplitN(name, "_", 2)[0] + ":read"
	return app.Tool{Name: name, Title: name, Description: "Reads.", Scope: scope, ReadOnly: true, Input: listArgs{}, Call: call}
}

func writeTool(name string, call func(context.Context, int64, json.RawMessage) (string, error)) app.Tool {
	scope := strings.SplitN(name, "_", 2)[0] + ":write"
	return app.Tool{Name: name, Title: name, Description: "Writes.", Scope: scope, Input: listArgs{}, Call: call}
}

func done(context.Context, int64, json.RawMessage) (string, error) { return "done", nil }

func resultText(t *testing.T, res *sdk.CallToolResult) string {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("content = %v, want one text item", res.Content)
	}
	tc, isText := res.Content[0].(*sdk.TextContent)
	if !isText {
		t.Fatalf("content[0] = %T, want text", res.Content[0])
	}
	return tc.Text
}

func TestMCPNoTokenIs401WithResourceMetadata(t *testing.T) {
	f := newFixture(t, nil, map[string]oauth.Caller{"good": caller(1, "g1", "books:read")})
	for _, auth := range []string{"", "Bearer wrong"} {
		req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/mcp", strings.NewReader(`{}`))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("auth %q: status = %d, want 401", auth, resp.StatusCode)
		}
		want := `resource_metadata="` + wantMetadataURL + `"`
		if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, want) {
			t.Errorf("auth %q: WWW-Authenticate = %q, want it to contain %s", auth, got, want)
		}
	}
}

func TestProtectedResourceMetadata(t *testing.T) {
	f := newFixture(t, nil, nil)
	for _, path := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource"} {
		resp, err := http.Get(f.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		var md struct {
			Resource             string   `json:"resource"`
			AuthorizationServers []string `json:"authorization_servers"`
			ScopesSupported      []string `json:"scopes_supported"`
		}
		err = json.NewDecoder(resp.Body).Decode(&md)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if md.Resource != testResource {
			t.Errorf("%s: resource = %q", path, md.Resource)
		}
		if !slices.Equal(md.AuthorizationServers, []string{testIssuer}) {
			t.Errorf("%s: authorization_servers = %v", path, md.AuthorizationServers)
		}
		if !slices.Equal(md.ScopesSupported, oauth.AllScopes) {
			t.Errorf("%s: scopes_supported = %v", path, md.ScopesSupported)
		}
	}
}

func TestToolsListFilteredByGrantScopes(t *testing.T) {
	tools := []app.Tool{
		readTool("books_list", done),
		readTool("books_get", done),
		writeTool("books_add", done),
		readTool("notes_list", done),
	}
	f := newFixture(t, tools, map[string]oauth.Caller{"tok": caller(7, "g1", "books:read")})
	cs := f.connect(t, "tok")

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tl := range res.Tools {
		got = append(got, tl.Name)
	}
	slices.Sort(got)
	if want := []string{"books_get", "books_list"}; !slices.Equal(got, want) {
		t.Errorf("tools = %v, want %v", got, want)
	}
}

func TestToolCallOutsideScopeRefused(t *testing.T) {
	called := false
	tools := []app.Tool{
		readTool("books_list", done),
		writeTool("books_add", func(context.Context, int64, json.RawMessage) (string, error) {
			called = true
			return "added", nil
		}),
	}
	f := newFixture(t, tools, map[string]oauth.Caller{"tok": caller(7, "g1", "books:read")})
	cs := f.connect(t, "tok")

	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "books_add", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool: %v (want a tool error result, not a protocol error)", err)
	}
	if !res.IsError {
		t.Error("IsError = false, want true")
	}
	if called {
		t.Error("the out-of-scope tool ran")
	}
	want := []auditRow{{7, "Claude", "books_add", false, "scope"}}
	if got := f.auditRows(); !slices.Equal(got, want) {
		t.Errorf("audit = %v, want %v", got, want)
	}
}

func TestToolCallPassesTokenUserIDAndAudits(t *testing.T) {
	var gotUser int64
	var gotArgs listArgs
	tools := []app.Tool{readTool("books_list", func(_ context.Context, userID int64, args json.RawMessage) (string, error) {
		gotUser = userID
		if err := json.Unmarshal(args, &gotArgs); err != nil {
			return "", err
		}
		return "three books", nil
	})}
	f := newFixture(t, tools, map[string]oauth.Caller{"tok": caller(42, "g1", "books:read")})
	cs := f.connect(t, "tok")

	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "books_list",
		Arguments: map[string]any{"query": "dune", "user_id": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", resultText(t, res))
	}
	if got := resultText(t, res); got != "three books" {
		t.Errorf("text = %q", got)
	}
	if gotUser != 42 {
		t.Errorf("Call got user %d, want the token's 42", gotUser)
	}
	if gotArgs.Query != "dune" {
		t.Errorf("Call got args %+v", gotArgs)
	}
	want := []auditRow{{42, "Claude", "books_list", true, ""}}
	if got := f.auditRows(); !slices.Equal(got, want) {
		t.Errorf("audit = %v, want %v", got, want)
	}
}

func TestToolErrorMessageReachesModelOtherErrorsDoNot(t *testing.T) {
	tools := []app.Tool{
		readTool("books_get", func(context.Context, int64, json.RawMessage) (string, error) {
			return "", app.ToolError{Msg: "No book with that ID on your shelves"}
		}),
		readTool("books_list", func(context.Context, int64, json.RawMessage) (string, error) {
			return "", errors.New("sql: no such table books_secret")
		}),
	}
	f := newFixture(t, tools, map[string]oauth.Caller{"tok": caller(7, "g1", "books:read")})
	cs := f.connect(t, "tok")

	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "books_get", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || resultText(t, res) != "No book with that ID on your shelves" {
		t.Errorf("ToolError: IsError=%v text=%q", res.IsError, resultText(t, res))
	}

	res, err = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "books_list", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || resultText(t, res) != "Something went wrong." {
		t.Errorf("other error: IsError=%v text=%q", res.IsError, resultText(t, res))
	}
	if log := f.log.String(); !strings.Contains(log, "books_list") || !strings.Contains(log, "books_secret") {
		t.Errorf("log %q should name the tool and carry the real error", log)
	}

	want := []auditRow{
		{7, "Claude", "books_get", false, "tool"},
		{7, "Claude", "books_list", false, "internal"},
	}
	if got := f.auditRows(); !slices.Equal(got, want) {
		t.Errorf("audit = %v, want %v", got, want)
	}
}

func TestReadOnlyToolsAnnotated(t *testing.T) {
	tools := []app.Tool{readTool("books_list", done), writeTool("books_add", done)}
	f := newFixture(t, tools, map[string]oauth.Caller{"tok": caller(7, "g1", "books:read", "books:write")})
	cs := f.connect(t, "tok")

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, tl := range res.Tools {
		a := tl.Annotations
		if a == nil {
			t.Fatalf("%s: no annotations", tl.Name)
		}
		switch tl.Name {
		case "books_list":
			seen++
			if !a.ReadOnlyHint {
				t.Error("books_list: readOnlyHint = false")
			}
		case "books_add":
			seen++
			if a.ReadOnlyHint {
				t.Error("books_add: readOnlyHint = true")
			}
			if a.DestructiveHint == nil || *a.DestructiveHint {
				t.Errorf("books_add: destructiveHint = %v, want false", a.DestructiveHint)
			}
		}
		if s, _ := json.Marshal(tl.InputSchema); !strings.Contains(string(s), `"query"`) {
			t.Errorf("%s: input schema %s lacks the query argument", tl.Name, s)
		}
	}
	if seen != 2 {
		t.Errorf("saw %d of the 2 tools", seen)
	}
}

func post(t *testing.T, f *fixture, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func TestMCPRateLimitedPerGrant(t *testing.T) {
	f := newFixture(t, nil, map[string]oauth.Caller{
		"a": caller(1, "grant-a", "books:read"),
		"b": caller(1, "grant-b", "books:read"),
	})
	for i := range 120 {
		if resp := post(t, f, "a"); resp.StatusCode == http.StatusTooManyRequests {
			t.Fatalf("request %d: 429 before the limit", i+1)
		}
	}
	resp := post(t, f, "a")
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("121st request: status %d, want 429", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("429 without Retry-After")
	}
	if resp := post(t, f, "b"); resp.StatusCode == http.StatusTooManyRequests {
		t.Error("another grant was limited too")
	}
}

// TestMCPBehindAProxyOnLoopback: production sits behind a reverse proxy that
// reaches the binary over loopback with the public Host header. The SDK's
// DNS-rebinding guard would refuse that with 403; the bearer token is what
// protects /mcp, so the guard is off.
func TestMCPBehindAProxyOnLoopback(t *testing.T) {
	f := newFixture(t, nil, map[string]oauth.Caller{"tok": caller(1, "g1", "books:read")})
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Host = "onsuite.test"
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		t.Errorf("status 403 for a proxied public Host")
	}
}

func TestMCPGetAndDeleteAre405(t *testing.T) {
	f := newFixture(t, nil, map[string]oauth.Caller{"tok": caller(1, "g1", "books:read")})
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		req, _ := http.NewRequest(method, f.srv.URL+"/mcp", nil)
		req.Header.Set("Authorization", "Bearer tok")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: status %d, want 405", method, resp.StatusCode)
		}
	}
}

func TestLimiterWindowResetsAndPrunes(t *testing.T) {
	now := testNow
	l := newLimiter(2, time.Minute, func() time.Time { return now })
	for range 2 {
		if allowed, _ := l.allow("g"); !allowed {
			t.Fatal("refused under the limit")
		}
	}
	allowed, wait := l.allow("g")
	if allowed || wait <= 0 || wait > time.Minute {
		t.Fatalf("over the limit: allowed=%v wait=%v", allowed, wait)
	}
	l.allow("old")
	now = now.Add(time.Minute)
	if allowed, _ := l.allow("g"); !allowed {
		t.Error("still refused in the next window")
	}
	l.mu.Lock()
	_, kept := l.windows["old"]
	l.mu.Unlock()
	if kept {
		t.Error("an expired key was not pruned")
	}
}
