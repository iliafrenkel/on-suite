package oauth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"authelia.com/provider/oauth2"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

const (
	testPublicURL   = "https://onsuite.test"
	claudeRedirect  = "https://claude.ai/api/mcp/auth_callback"
	chatGPTRedirect = "https://chatgpt.com/connector_platform_oauth_redirect"
)

// asServer is one authorization server over a test database, with its two
// Task 4 routes mounted behind the real middleware stack.
type asServer struct {
	srv     *Server
	handle  *sql.DB
	handler http.Handler
}

// newASServer builds a Server on handle (a fresh migrated database when
// nil) and mounts Routes behind web.Stack, the way the binary will.
func newASServer(t *testing.T, handle *sql.DB, redirects []string, client *http.Client) *asServer {
	t.Helper()
	if handle == nil {
		handle = newFixture(t).handle
	}
	srv, err := New(context.Background(), handle, Options{
		PublicURL:    testPublicURL,
		RedirectURIs: redirects,
		HTTPClient:   client,
		Now:          func() time.Time { return time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatal(err)
	}
	rend, err := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL, CSRFFieldName: web.CSRFFormField})
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	errs := web.NewErrors(rend, log)
	csrf := web.NewCSRF(false, errs)
	authn := web.NewAuth(web.AuthOptions{Users: auth.NewStore(handle), Render: rend, Errors: errs, CSRF: csrf, Log: log, Version: "test"})

	mux := http.NewServeMux()
	srv.Routes(mux, nil, authn, csrf, PageDeps{Render: rend, Errors: errs, Version: "test", Log: log})
	mux.Handle("/", http.HandlerFunc(errs.NotFound))
	return &asServer{srv: srv, handle: handle, handler: web.Stack(mux, log, errs, csrf, authn, false)}
}

// register posts a DCR request with no cookies, as an AI client would.
func (a *asServer) register(t *testing.T, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, testPublicURL+"/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("POST /oauth/register = %d, body not JSON: %q", rec.Code, rec.Body.String())
	}
	return rec.Code, out
}

func (a *asServer) countClients(t *testing.T) int {
	t.Helper()
	var n int
	if err := a.handle.QueryRow(`SELECT count(*) FROM oauth_clients`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// authorize runs the library's authorize-request parsing for clientID, the
// step that looks the client up (and resolves a CIMD document).
func (a *asServer) authorize(t *testing.T, clientID string) (oauth2.AuthorizeRequester, error) {
	t.Helper()
	q := url.Values{
		"client_id":             {clientID},
		"response_type":         {"code"},
		"redirect_uri":          {claudeRedirect},
		"scope":                 {"books:read"},
		"state":                 {"state-0123456789"},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
		"resource":              {a.srv.ResourceURL()},
	}
	req := httptest.NewRequest(http.MethodGet, testPublicURL+"/oauth/authorize?"+q.Encode(), nil)
	return a.srv.provider.NewAuthorizeRequest(req.Context(), req)
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	return oauth2.ErrorToRFC6749Error(err).ErrorField
}

func TestRegisterRefusesRedirectNotOnAllowlist(t *testing.T) {
	a := newASServer(t, nil, []string{claudeRedirect}, nil)
	before := a.countClients(t)

	code, body := a.register(t, `{"redirect_uris":["https://evil.example/cb"],"token_endpoint_auth_method":"none"}`)
	if code != http.StatusBadRequest || body["error"] != "invalid_redirect_uri" {
		t.Fatalf("register = %d %v, want 400 invalid_redirect_uri", code, body)
	}
	if after := a.countClients(t); after != before {
		t.Errorf("oauth_clients rows = %d, want %d (nothing stored)", after, before)
	}

	// One bad URI among good ones still fails: every URI must be listed.
	code, body = a.register(t, `{"redirect_uris":["`+claudeRedirect+`","https://evil.example/cb"],"token_endpoint_auth_method":"none"}`)
	if code != http.StatusBadRequest || body["error"] != "invalid_redirect_uri" {
		t.Fatalf("mixed register = %d %v, want 400 invalid_redirect_uri", code, body)
	}
}

// The loopback URIs are put ON the allowlist, so only the loopback refusal
// in allowedRedirects can stop them: a registered loopback redirect would
// match any port at authorize time. The client declares itself "native",
// because the library's own validator already refuses loopback for the
// default "web" type but allows it for native apps (RFC 8252).
func TestRegisterRefusesLoopbackRedirect(t *testing.T) {
	loopbacks := []string{"http://127.0.0.1:1234/cb", "https://localhost/cb"}
	a := newASServer(t, nil, append([]string{claudeRedirect}, loopbacks...), nil)
	before := a.countClients(t)

	for _, uri := range loopbacks {
		code, body := a.register(t, `{"redirect_uris":["`+uri+`"],"token_endpoint_auth_method":"none","application_type":"native"}`)
		if code != http.StatusBadRequest || body["error"] != "invalid_redirect_uri" {
			t.Errorf("register(%s) = %d %v, want 400 invalid_redirect_uri", uri, code, body)
		}
	}
	if after := a.countClients(t); after != before {
		t.Errorf("oauth_clients rows = %d, want %d (nothing stored)", after, before)
	}
}

func TestRegisterForcesPublicClientAndOurResource(t *testing.T) {
	a := newASServer(t, nil, []string{claudeRedirect}, nil)
	code, body := a.register(t, `{"redirect_uris":["`+claudeRedirect+`"],"client_name":"Claude",`+
		`"grant_types":["authorization_code","refresh_token"],"response_types":["code"],`+
		`"audience":["https://elsewhere.example"],"resource":["https://elsewhere.example/api"]}`)
	if code != http.StatusCreated {
		t.Fatalf("register = %d %v, want 201", code, body)
	}
	if _, ok := body["client_secret"]; ok {
		t.Errorf("response carries a client_secret: %v", body)
	}
	if body["token_endpoint_auth_method"] != "none" {
		t.Errorf("token_endpoint_auth_method = %v, want none", body["token_endpoint_auth_method"])
	}
	id, _ := body["client_id"].(string)
	if id == "" {
		t.Fatalf("no client_id in %v", body)
	}

	client, err := a.srv.store.GetClient(context.Background(), id)
	if err != nil {
		t.Fatalf("GetClient(%q): %v", id, err)
	}
	rc := client.(*oauth2.DefaultRegisteredClient)
	if !rc.Public {
		t.Error("stored client is not public")
	}
	if want := []string{a.srv.ResourceURL()}; !slices.Equal(rc.Resource, want) {
		t.Errorf("stored resource = %v, want %v", rc.Resource, want)
	}
	if len(rc.Audience) != 0 {
		t.Errorf("stored audience = %v, want none", rc.Audience)
	}
	if !slices.Equal(rc.Scopes, AllScopes) {
		t.Errorf("stored scopes = %v, want %v", rc.Scopes, AllScopes)
	}
}

func TestRegisterRestrictsScopesToOurs(t *testing.T) {
	a := newASServer(t, nil, []string{claudeRedirect}, nil)
	code, body := a.register(t, `{"redirect_uris":["`+claudeRedirect+`"],"scope":"notes:read openid admin"}`)
	if code != http.StatusCreated {
		t.Fatalf("register = %d %v, want 201", code, body)
	}
	client, err := a.srv.store.GetClient(context.Background(), body["client_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if got := client.GetScopes(); !slices.Equal([]string(got), []string{"notes:read"}) {
		t.Errorf("stored scopes = %v, want [notes:read]", got)
	}
}

func TestRegisterRefusesSectorIdentifierURI(t *testing.T) {
	a := newASServer(t, nil, []string{claudeRedirect}, nil)
	before := a.countClients(t)
	code, body := a.register(t, `{"redirect_uris":["`+claudeRedirect+`"],"sector_identifier_uri":"https://claude.ai/sector.json"}`)
	if code != http.StatusBadRequest || body["error"] != "invalid_client_metadata" {
		t.Fatalf("register = %d %v, want 400 invalid_client_metadata", code, body)
	}
	if after := a.countClients(t); after != before {
		t.Errorf("oauth_clients rows = %d, want %d", after, before)
	}
}

// cimdHost serves one Client ID Metadata Document over TLS and counts hits.
type cimdHost struct {
	srv       *httptest.Server
	hits      atomic.Int32
	redirects []string
}

func newCIMDHost(t *testing.T, redirects ...string) *cimdHost {
	t.Helper()
	h := &cimdHost{redirects: redirects}
	h.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.hits.Add(1)
		doc, _ := json.Marshal(map[string]any{
			"client_id":                  h.clientID(),
			"client_name":                "Test assistant",
			"redirect_uris":              h.redirects,
			"grant_types":                []string{"authorization_code", "refresh_token"},
			"response_types":             []string{"code"},
			"token_endpoint_auth_method": "none",
		})
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write(doc)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *cimdHost) clientID() string { return h.srv.URL + "/client.json" }

func TestCIMDRefusesDocumentWithUnlistedRedirect(t *testing.T) {
	host := newCIMDHost(t, "https://evil.example/cb")
	a := newASServer(t, nil, []string{claudeRedirect}, host.srv.Client())

	_, err := a.authorize(t, host.clientID())
	if errorCode(err) != "invalid_client" {
		t.Fatalf("authorize err = %v (%s), want invalid_client", err, errorCode(err))
	}
	if host.hits.Load() != 1 {
		t.Errorf("document fetched %d times, want 1", host.hits.Load())
	}
	var n int
	if err := a.handle.QueryRow(`SELECT count(*) FROM oauth_cimd`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("refused document was cached (%d rows)", n)
	}
}

func TestCIMDCachedClientRecheckedAgainstAllowlist(t *testing.T) {
	host := newCIMDHost(t, claudeRedirect)
	first := newASServer(t, nil, []string{claudeRedirect, chatGPTRedirect}, host.srv.Client())

	ar, err := first.authorize(t, host.clientID())
	if err != nil {
		t.Fatalf("first authorize: %v", err)
	}
	client := ar.GetClient()
	if got, want := []string(client.GetResource()), []string{first.srv.ResourceURL()}; !slices.Equal(got, want) {
		t.Errorf("CIMD client resource = %v, want %v", got, want)
	}
	if !slices.Equal([]string(client.GetScopes()), AllScopes) {
		t.Errorf("CIMD client scopes = %v, want %v", client.GetScopes(), AllScopes)
	}
	if host.hits.Load() != 1 {
		t.Fatalf("document fetched %d times, want 1", host.hits.Load())
	}

	// Restart with an allowlist that no longer has the document's redirect.
	// The cached record is still fresh, so no fetch happens and the
	// resolver's validators do not run; only the GetClient re-check can
	// refuse it.
	second := newASServer(t, first.handle, []string{chatGPTRedirect}, host.srv.Client())
	_, err = second.authorize(t, host.clientID())
	if errorCode(err) != "invalid_client" {
		t.Fatalf("second authorize err = %v (%s), want invalid_client", err, errorCode(err))
	}
	if host.hits.Load() != 1 {
		t.Errorf("document fetched %d times, want 1 (served from cache)", host.hits.Load())
	}
	if _, err := second.srv.clients.GetClient(context.Background(), host.clientID()); err == nil {
		t.Error("GetClient returned the cached CIMD client despite its redirect leaving the allowlist")
	}
}

func TestCIMDGuardRefusesPrivateAddresses(t *testing.T) {
	host := newCIMDHost(t, claudeRedirect)
	// No HTTPClient: the special-use-address guarded default.
	a := newASServer(t, nil, []string{claudeRedirect}, nil)

	for _, id := range []string{host.clientID(), "https://10.0.0.1/client.json"} {
		_, err := a.authorize(t, id)
		if errorCode(err) != "invalid_client" {
			t.Errorf("authorize(%s) err = %v (%s), want invalid_client", id, err, errorCode(err))
		}
		if !errors.Is(err, oauth2.ErrSpecialUseAddress) {
			t.Errorf("authorize(%s) err = %v, want it refused by the special-use address guard", id, err)
		}
	}
	if host.hits.Load() != 0 {
		t.Errorf("loopback document fetched %d times, want 0", host.hits.Load())
	}
}

func TestASMetadataAdvertisesExactly(t *testing.T) {
	a := newASServer(t, nil, []string{claudeRedirect}, nil)
	req := httptest.NewRequest(http.MethodGet, testPublicURL+"/.well-known/oauth-authorization-server", nil)
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET metadata = %d: %s", rec.Code, rec.Body.String())
	}
	var md struct {
		Issuer             string   `json:"issuer"`
		Authorization      string   `json:"authorization_endpoint"`
		Token              string   `json:"token_endpoint"`
		Registration       string   `json:"registration_endpoint"`
		Revocation         string   `json:"revocation_endpoint"`
		CodeChallenge      []string `json:"code_challenge_methods_supported"`
		GrantTypes         []string `json:"grant_types_supported"`
		Scopes             []string `json:"scopes_supported"`
		CIMD               bool     `json:"client_id_metadata_document_supported"`
		ISS                bool     `json:"authorization_response_iss_parameter_supported"`
		TokenAuthMethods   []string `json:"token_endpoint_auth_methods_supported"`
		IntrospectEndpoint string   `json:"introspection_endpoint"`
	}
	if err := json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(&md); err != nil {
		t.Fatal(err)
	}
	if md.Issuer != testPublicURL {
		t.Errorf("issuer = %q, want %q", md.Issuer, testPublicURL)
	}
	if !slices.Equal(md.CodeChallenge, []string{"S256"}) {
		t.Errorf("code_challenge_methods_supported = %v, want [S256]", md.CodeChallenge)
	}
	if len(md.GrantTypes) == 0 {
		t.Error("grant_types_supported is empty")
	}
	for _, g := range md.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			t.Errorf("grant_types_supported has %q", g)
		}
	}
	// The library sorts the list, so compare as sets.
	if got, want := slices.Sorted(slices.Values(md.Scopes)), slices.Sorted(slices.Values(AllScopes)); !slices.Equal(got, want) {
		t.Errorf("scopes_supported = %v, want %v", md.Scopes, AllScopes)
	}
	if md.Registration != testPublicURL+"/oauth/register" {
		t.Errorf("registration_endpoint = %q", md.Registration)
	}
	if md.Authorization != testPublicURL+"/oauth/authorize" || md.Token != testPublicURL+"/oauth/token" || md.Revocation != testPublicURL+"/oauth/revoke" {
		t.Errorf("endpoints = %q %q %q", md.Authorization, md.Token, md.Revocation)
	}
	if !md.CIMD {
		t.Error("client_id_metadata_document_supported is not true")
	}
	if !md.ISS {
		t.Error("authorization_response_iss_parameter_supported is not true")
	}
	if !slices.Equal(md.TokenAuthMethods, []string{"none"}) {
		t.Errorf("token_endpoint_auth_methods_supported = %v, want [none]", md.TokenAuthMethods)
	}
	if md.IntrospectEndpoint != "" {
		t.Errorf("introspection_endpoint = %q, want none advertised", md.IntrospectEndpoint)
	}
}

func TestAllowedRedirects(t *testing.T) {
	s := &Server{redirects: map[string]bool{
		claudeRedirect:         true,
		"https://localhost/cb": true, // never valid, even when listed
		"https://127.0.0.1/cb": true,
		"https://[::1]/cb":     true,
	}}
	for _, tc := range []struct {
		uris []string
		ok   bool
	}{
		{[]string{claudeRedirect}, true},
		{nil, false},
		{[]string{claudeRedirect + "/"}, false},
		{[]string{"HTTPS://claude.ai/api/mcp/auth_callback"}, false},
		{[]string{"https://localhost/cb"}, false},
		{[]string{"https://127.0.0.1/cb"}, false},
		{[]string{"https://[::1]/cb"}, false},
		{[]string{claudeRedirect, "https://evil.example/cb"}, false},
	} {
		if err := s.allowedRedirects(tc.uris); (err == nil) != tc.ok {
			t.Errorf("allowedRedirects(%v) = %v, want ok=%v", tc.uris, err, tc.ok)
		}
	}
}
