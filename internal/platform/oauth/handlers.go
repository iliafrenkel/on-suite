package oauth

import (
	"log/slog"
	"net/http"

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

// Routes mounts the authorization server's endpoints. Each one is public
// (anonymous by cookie): the callers are AI clients' servers, which
// authenticate by their own means. Registration is CSRF-exempt because those
// servers send no cookie (spec, "CSRF").
func (s *Server) Routes(mux *http.ServeMux, rec *web.Recorder, authn *web.Auth, csrf *web.CSRF, d PageDeps) {
	rec.Handle(mux, "GET /.well-known/oauth-authorization-server", true, s.metadata(d))

	const register = "POST /oauth/register"
	web.RegisterBodyLimit(register, registerBodyLimit)
	csrf.Exempt("/oauth/register")
	rec.Handle(mux, register, true, http.HandlerFunc(s.register))
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
