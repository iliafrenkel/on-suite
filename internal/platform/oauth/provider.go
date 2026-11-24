package oauth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"time"

	"authelia.com/provider/oauth2"
	"authelia.com/provider/oauth2/compose"
	"authelia.com/provider/oauth2/handler/cimd"
	"authelia.com/provider/oauth2/handler/rfc7591"
)

// Options configures New.
type Options struct {
	// PublicURL is the externally visible origin (config.PublicURL). The
	// issuer and resource come from it, never from request headers.
	PublicURL string
	// RedirectURIs is the exact redirect URI allowlist
	// (config.OAuthRedirectURIs).
	RedirectURIs []string
	// HTTPClient fetches CIMD documents. Nil means the library's client that
	// refuses special-use (loopback, private) addresses; tests inject one
	// that trusts their TLS server.
	HTTPClient *http.Client
	// Now is the store's clock. Nil means time.Now.
	Now func() time.Time
}

// Server is the OAuth 2.1 authorization server: the composed library
// provider over the Store, plus the redirect allowlist.
type Server struct {
	provider  oauth2.Provider
	cfg       *oauth2.Config
	store     *Store
	clients   checkedClients
	publicURL string
	redirects map[string]bool

	// Per-IP limiters, one budget per endpoint (authorize's GET and POST
	// share one). See ratelimit.go.
	registerLimiter, tokenLimiter, authorizeLimiter *limiter
}

// Lifespans (spec 2026-11-24-ai-access-mcp-design.md, "Lifetimes"). The
// refresh lifespan is also the absolute maximum, counted from when the
// assistant was connected, so users reconnect about once a quarter however
// often the token rotates.
const (
	codeLifespan    = time.Minute
	accessLifespan  = time.Hour
	refreshLifespan = 90 * 24 * time.Hour
)

// New builds the authorization server on a migrated database. It composes
// only the handlers AI access needs (plan, "Library notes" §Provider) and
// fails, rather than serving, if the result is not an OAuth 2.1 server.
func New(ctx context.Context, db *sql.DB, opts Options) (*Server, error) {
	issuer, err := issuerOf(opts.PublicURL)
	if err != nil {
		return nil, err
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	s := &Server{
		publicURL:        issuer,
		redirects:        map[string]bool{},
		registerLimiter:  newLimiter(registerLimit, registerWindow, now),
		tokenLimiter:     newLimiter(endpointLimit, endpointWindow, now),
		authorizeLimiter: newLimiter(endpointLimit, endpointWindow, now),
	}
	for _, u := range opts.RedirectURIs {
		s.redirects[u] = true
	}
	s.store = NewStore(db, now)
	s.clients = checkedClients{Store: s.store, s: s}

	hmac, registration, err := s.store.Secrets(ctx)
	if err != nil {
		return nil, err
	}

	cfg := &oauth2.Config{
		Issuer: issuer,
		EndpointURLs: oauth2.EndpointURLs{
			Authorization: issuer + "/oauth/authorize",
			Token:         issuer + "/oauth/token",
			Revocation:    issuer + "/oauth/revoke",
			Registration:  issuer + "/oauth/register",
		},
		AuthorizeCodeLifespan:       codeLifespan,
		AccessTokenLifespan:         accessLifespan,
		RefreshTokenLifespan:        refreshLifespan,
		RefreshTokenMaximumLifespan: refreshLifespan,
		// Exact, not the wildcard and prefix defaults: books:read must not
		// grant books:read:anything, nor /mcp grant /mcpanything.
		ScopeStrategy:    oauth2.ExactScopeStrategy,
		ResourceStrategy: oauth2.ExactResourceStrategy,
		// Empty, so every code exchange gets a refresh token; the default
		// demands offline_access, which no assistant asks for.
		RefreshTokenScopes: []string{},
		ScopesSupported:    slices.Clone(AllScopes),
		// Every client is public (registration forces it), so advertise
		// only "none".
		TokenEndpointAuthMethods: []string{"none"},

		GlobalSecret:                          hmac,
		RFC7591ClientRegistrationGlobalSecret: registration,
	}
	compose.ApplyOAuth21Profile(cfg)

	cfg.RFC7591ClientRegistrationStrategy = rfc7591.NewDefaultClientRegistrationStrategy()
	cfg.RFC7591ClientRegistrationMetadataStrategy = registrationMetadata{s: s, base: rfc7591.NewDefaultClientRegistrationMetadataStrategy(cfg)}
	cfg.RFC7591ClientRegistrationEndpointAuthStrategy = openRegistration{}
	cfg.RFC7591ClientRegistrationValidators = []oauth2.ClientRegistrationValidator{
		rfc7591.NewLocalValidator(cfg), redirectValidator{s}, noSectorIdentifier{},
	}

	fetch := oauth2.NewSpecialUseAddressGuardedHTTPClient()
	if opts.HTTPClient != nil {
		fetch.HTTPClient = opts.HTTPClient
	}
	resolver, err := cimd.NewResolver(cimd.Config{
		Storage:         s.store,
		HTTPClient:      fetch,
		ValidatorConfig: cfg,
		Validators: []oauth2.ClientRegistrationValidator{
			rfc7591.NewLocalMetadataDocumentValidator(cfg), redirectValidator{s},
		},
		Strategy:                 cimdClients{s: s, base: cimd.DefaultClientStrategy{}},
		Scopes:                   slices.Clone(AllScopes),
		TokenEndpointAuthMethods: []string{"none"},
	})
	if err != nil {
		return nil, fmt.Errorf("oauth: cimd resolver: %w", err)
	}
	cfg.ClientIDMetadataDocumentResolver = resolver
	cfg.ClientIDMetadataDocumentHTTPClient = fetch

	s.cfg = cfg
	s.store.setConfig(cfg)

	s.provider = compose.Compose(cfg, s.clients, compose.NewOAuth2HMACStrategy(cfg),
		compose.OAuth2AuthorizeExplicitFactory,
		compose.OAuth2RefreshTokenGrantFactory,
		compose.OAuth2PKCEFactory,
		compose.OAuth2TokenRevocationFactory,
		compose.OAuth2TokenIntrospectionFactory,
		compose.RFC7591ClientRegistrationFactory,
	)
	if err := compose.ValidateOAuth21Profile(ctx, cfg); err != nil {
		return nil, fmt.Errorf("oauth: not an OAuth 2.1 configuration: %w", err)
	}
	return s, nil
}

// issuerOf checks the public URL is a bare https origin, which the library
// needs for the issuer (and so for CIMD and the RFC 9207 iss parameter).
func issuerOf(publicURL string) (string, error) {
	u, err := url.Parse(publicURL)
	if err != nil || u.Scheme != "https" || u.Host == "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", errors.New("oauth: public URL must be an https origin with no path, e.g. https://on.example.com")
	}
	return "https://" + u.Host, nil
}

// Issuer is the authorization server's issuer identifier.
func (s *Server) Issuer() string { return s.publicURL }

// ResourceURL is the MCP endpoint's resource identifier (RFC 8707), the
// only resource any token is issued for.
func (s *Server) ResourceURL() string { return s.publicURL + "/mcp" }
