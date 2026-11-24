package oauth

import (
	"log/slog"
	"net/http"

	"authelia.com/provider/oauth2"

	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// PageDeps is what the authorization server's routes need from the platform:
// the shell for the consent page, and somewhere to report errors.
type PageDeps struct {
	Render  *render.Renderer
	Errors  *web.Errors
	Nav     []render.NavItem
	Version string
	Log     *slog.Logger
}

// registerBodyLimit caps a registration request. Real client metadata is a
// few hundred bytes; the spec proposes 16 KiB (2026-11-24-ai-access-mcp-design.md,
// "Body limits").
const registerBodyLimit = 16 << 10

// Routes mounts the authorization server's endpoints. Authorization is the
// one browser flow: it needs a signed-in user (logged-out users bounce
// through /login?next= with the request intact), and its POST, the consent
// decision, keeps the CSRF check. Everything else is public (anonymous by
// cookie): the callers are AI clients' servers, which authenticate by their
// own means and send no cookie, so those routes are CSRF-exempt (spec,
// "Wiring").
func (s *Server) Routes(mux *http.ServeMux, rec *web.Recorder, authn *web.Auth, csrf *web.CSRF, d PageDeps) {
	rec.Handle(mux, "GET /.well-known/oauth-authorization-server", true, s.metadata(d))

	// The limit wraps outside RequireUser, so a logged-out loop of
	// authorization requests is counted too. The page is the plain error
	// page; the JSON endpoints get an OAuth error body instead.
	tooMany := func(w http.ResponseWriter, r *http.Request) { d.Errors.Status(w, r, http.StatusTooManyRequests) }
	rec.Handle(mux, "GET /oauth/authorize", false,
		limitIP(s.authorizeLimiter, tooMany, authn.RequireUser(s.authorize(d))))
	rec.Handle(mux, "POST /oauth/authorize", false,
		limitIP(s.authorizeLimiter, tooMany, authn.RequireUser(s.decide(d))))

	csrf.Exempt("/oauth/token", "/oauth/revoke")
	rec.Handle(mux, "POST /oauth/token", true,
		limitIP(s.tokenLimiter, slowDown, http.HandlerFunc(s.token)))
	rec.Handle(mux, "POST /oauth/revoke", true, http.HandlerFunc(s.revoke))

	const register = "POST /oauth/register"
	web.RegisterBodyLimit(register, registerBodyLimit)
	csrf.Exempt("/oauth/register")
	rec.Handle(mux, register, true,
		limitIP(s.registerLimiter, slowDown, http.HandlerFunc(s.register)))

	s.mountPages(mux, rec, authn, d)
}

// token is the token endpoint: code exchange and refresh. The provider is
// composed over checkedClients, so the client is re-checked against the
// redirect allowlist on every call, refresh included.
//
// The request carries a client memo (withClientMemo): the client resolved
// when the library authenticates the caller is the one the store reuses
// when it rebuilds the stored code or refresh session inside the library's
// transaction, where it must never fetch a CIMD document (sessionClient).
func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	ctx := withClientMemo(r.Context())
	r = r.WithContext(ctx)
	ar, err := s.provider.NewAccessRequest(ctx, r, new(oauth2.DefaultSession))
	if err != nil {
		s.provider.WriteAccessError(ctx, w, ar, err)
		return
	}
	resp, err := s.provider.NewAccessResponse(ctx, ar)
	if err != nil {
		s.provider.WriteAccessError(ctx, w, ar, err)
		return
	}
	s.provider.WriteAccessResponse(ctx, w, ar, resp)
}

// revoke is RFC 7009 token revocation. Revoking either token ends the whole
// grant (Store.RevokeRefreshToken), the same as disconnecting on /account.
func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	ctx := withClientMemo(r.Context())
	r = r.WithContext(ctx)
	s.provider.WriteRevocationResponse(ctx, w, s.provider.NewRevocationRequest(ctx, r))
}

// metadata serves the RFC 8414 document AI clients discover us from.
func (s *Server) metadata(d PageDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		md, err := s.provider.NewOAuth2AuthorizationServerMetadata(r.Context())
		if err != nil {
			d.Errors.Internal(w, r, err)
			return
		}
		s.provider.WriteDiscoveryResponse(r.Context(), w, md)
	})
}

// register is RFC 7591 dynamic client registration. Everything that makes
// open registration safe (public client, our resource, the redirect
// allowlist) is in the strategies and validators New installs.
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	req, err := s.provider.NewRFC7591ClientRegistrationRequest(ctx, r)
	if err != nil {
		s.provider.WriteRFC7591ClientRegistrationError(ctx, w, req, err)
		return
	}
	resp, err := s.provider.NewRFC7591ClientRegistrationResponse(ctx, req)
	if err != nil {
		s.provider.WriteRFC7591ClientRegistrationError(ctx, w, req, err)
		return
	}
	s.provider.WriteRFC7591ClientRegistrationResponse(ctx, w, req, resp)
}
