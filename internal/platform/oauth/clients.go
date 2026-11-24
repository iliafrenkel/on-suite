package oauth

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"authelia.com/provider/oauth2"
	"authelia.com/provider/oauth2/handler/cimd"
)

// allowedRedirects is the redirect URI allowlist, the one check that closes
// the open-registration phishing hole (spec 2026-11-24-ai-access-mcp-design.md,
// "Redirect URI allowlist"). It runs in three places: on dynamic
// registration, on every CIMD fetch, and on every client GetClient returns,
// so a cached client is refused as soon as the operator drops its redirect.
//
// Every URI must be on the list exactly, and there must be at least one.
// A loopback URI is refused even when listed: the library lets a public
// client use a registered loopback redirect on any port (plan, "Library
// notes" §Loopback), which would turn one exact entry into a range.
func (s *Server) allowedRedirects(uris []string) error {
	if len(uris) == 0 {
		return oauth2.ErrInvalidRedirectURI.WithHint("At least one redirect URI is required.")
	}
	for _, raw := range uris {
		if isLoopback(raw) {
			return oauth2.ErrInvalidRedirectURI.WithHintf("The redirect URI '%s' is a loopback address, which is not allowed.", raw)
		}
		if !s.redirects[raw] {
			return oauth2.ErrInvalidRedirectURI.WithHintf("The redirect URI '%s' is not on this server's allowlist.", raw)
		}
	}
	return nil
}

func isLoopback(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// redirectValidator runs the allowlist as a library registration validator,
// used for both DCR and CIMD documents.
type redirectValidator struct{ s *Server }

func (v redirectValidator) ValidateClientRegistrationMetadata(_ context.Context, _ oauth2.Client, md *oauth2.ClientRegistrationMetadata) error {
	return v.s.allowedRedirects(md.RedirectURIs)
}

// noSectorIdentifier refuses sector_identifier_uri. The library rejects it by
// default, but only while its validator list is unset, and New sets one
// (plan, "Library notes" §Allowlist hooks).
type noSectorIdentifier struct{}

func (noSectorIdentifier) ValidateClientRegistrationMetadata(_ context.Context, _ oauth2.Client, md *oauth2.ClientRegistrationMetadata) error {
	if md.SectorIdentifierURI != "" {
		return oauth2.ErrInvalidClientMetadata.WithHint("The 'sector_identifier_uri' value is not supported.")
	}
	return nil
}

// registrationMetadata shapes every DCR request into the only kind of client
// this server has: public (no secret to leak or store), bound to our MCP
// resource and to our scopes, with no audience. It runs before validation,
// so the validators see what is actually registered.
type registrationMetadata struct {
	s    *Server
	base oauth2.ClientRegistrationMetadataStrategy
}

func (m registrationMetadata) FilterClientRegistrationMetadata(ctx context.Context, client oauth2.Client, md *oauth2.ClientRegistrationMetadata) error {
	if err := m.base.FilterClientRegistrationMetadata(ctx, client, md); err != nil {
		return err
	}
	if md == nil {
		return nil
	}
	md.TokenEndpointAuthMethod = "none"
	md.Audience = nil
	md.Resource = []string{m.s.ResourceURL()}
	md.Scope = strings.Join(ourScopes(md.GetScopes()), " ")
	return nil
}

// ourScopes keeps the requested scopes we know. A client that asks for none
// of them (or for nothing) gets them all: the consent screen is where the
// user narrows them, and a client with no scopes could never be authorized.
func ourScopes(requested []string) []string {
	var out []string
	for _, sc := range requested {
		if slices.Contains(AllScopes, sc) && !slices.Contains(out, sc) {
			out = append(out, sc)
		}
	}
	if len(out) == 0 {
		return slices.Clone(AllScopes)
	}
	return out
}

// openRegistration lets anyone call the registration endpoint without a
// token. The library's default demands a bearer token; open registration is
// safe here only because of the redirect allowlist. The client configuration
// endpoint (RFC 7592) is not mounted, so a non-empty id is always refused.
type openRegistration struct{}

func (openRegistration) AuthenticateClientRegistrationRequest(_ context.Context, _ *http.Request, id string) (oauth2.Requester, error) {
	if id != "" {
		return nil, oauth2.ErrRequestUnauthorized
	}
	return nil, nil
}

// cimdClients builds a CIMD client with what the library's ceiling strips:
// a document may not grant itself a resource, so ours is granted here, along
// with every scope (consent narrows them), as for a DCR client.
type cimdClients struct {
	s    *Server
	base cimd.ClientStrategy
}

func (c cimdClients) NewClient(ctx context.Context, record *oauth2.ClientIDMetadataDocumentRecord) (oauth2.Client, error) {
	client, err := c.base.NewClient(ctx, record)
	if err != nil {
		return nil, err
	}
	cc, ok := client.(*cimd.Client)
	if !ok || cc.DefaultRegisteredClient == nil || cc.DefaultClient == nil {
		return nil, oauth2.ErrServerError.WithDebugf("unexpected CIMD client type %T", client)
	}
	cc.Resource = []string{c.s.ResourceURL()}
	cc.Scopes = slices.Clone(AllScopes)
	cc.Public = true
	return cc, nil
}

// checkedClients is the storage the provider is composed with: the Store,
// with GetClient re-checking the allowlist on every client it returns. It
// resolves CIMD clients itself (oauth2.GetClient on the bare Store), so a
// CIMD document served from the cache, which the resolver's validators never
// see again, is checked too.
type checkedClients struct {
	*Store
	s *Server
}

func (c checkedClients) GetClient(ctx context.Context, id string) (oauth2.Client, error) {
	client, err := oauth2.GetClient(ctx, c.s.cfg, c.Store, id)
	if err != nil {
		return nil, err
	}
	if err := c.s.allowedRedirects(client.GetRedirectURIs()); err != nil {
		return nil, oauth2.ErrInvalidClient.WithHint("The client's redirect URIs are not allowed by this server.").WithWrap(err).WithDebugError(err)
	}
	rememberClient(ctx, client)
	return client, nil
}

// The token and revocation endpoints resolve the calling client once, when
// the library authenticates it, before any transaction. The library then
// rebuilds the stored code or refresh session inside its transaction
// (Store.decodeRequest), which needs that session's client again. Resolving
// a CIMD client there could wait on a document fetch while the transaction
// holds the database's only connection; a fetch started by another request
// (/mcp's Verify, say) has to write the database before it completes, so the
// two waited on each other and every database request in the suite stalled
// until the resolver's 10-second fetch deadline failed both. A per-request
// memo of the authenticated client removes the need: the session's client
// must be that client anyway, or the library refuses the request.
type clientMemoKey struct{}

type clientMemo struct {
	mu      sync.Mutex
	clients map[string]oauth2.Client
}

// withClientMemo gives a request a memo for the clients it resolves.
func withClientMemo(ctx context.Context) context.Context {
	return context.WithValue(ctx, clientMemoKey{}, &clientMemo{clients: map[string]oauth2.Client{}})
}

func rememberClient(ctx context.Context, client oauth2.Client) {
	if m, ok := ctx.Value(clientMemoKey{}).(*clientMemo); ok {
		m.mu.Lock()
		m.clients[client.GetID()] = client
		m.mu.Unlock()
	}
}

func rememberedClient(ctx context.Context, id string) (oauth2.Client, bool) {
	m, ok := ctx.Value(clientMemoKey{}).(*clientMemo)
	if !ok {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.clients[id]
	return c, ok
}

// sessionClient rebuilds a stored session's client. Inside a transaction
// it never reaches the CIMD resolver (see clientMemo): it takes the client
// the request already authenticated, or a registered client from the
// table, and otherwise answers oauth2.ErrNotFound, which the library treats
// as an unknown code or token. Outside a transaction (introspection for
// /mcp) the full lookup runs, fetching the document if it has expired.
func (s *Store) sessionClient(ctx context.Context, id string) (oauth2.Client, error) {
	if c, ok := rememberedClient(ctx, id); ok {
		return c, nil
	}
	if _, inTx := ctx.Value(txKey{}).(*sql.Tx); inTx {
		return s.GetClient(ctx, id)
	}
	// A nil *Config inside an `any` would pass GetClient's type assertion
	// and then be dereferenced, so pass a true nil until setConfig runs.
	var cfg any
	if s.cfg != nil {
		cfg = s.cfg
	}
	return oauth2.GetClient(ctx, cfg, s, id)
}
