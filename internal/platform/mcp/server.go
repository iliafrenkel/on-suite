// Package mcp serves ON Suite's remote MCP server at /mcp: the tools apps
// contribute through app.ToolProvider, behind a bearer token from ON Suite's
// own OAuth server (spec 2026-11-24-ai-access-mcp-design.md).
//
// It is the only package that imports the MCP SDK and jsonschema-go, so apps
// describe tools as plain app.Tool values and never see either.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/oauth"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// Deps is what /mcp needs. Verify and Audit are functions rather than the
// oauth Server and Store so this package depends only on what it uses, and
// tests can hand it fakes.
type Deps struct {
	Tools []app.Tool
	// Verify is oauth.Server.Verify: who a bearer token speaks for.
	Verify func(ctx context.Context, bearer string) (oauth.Caller, error)
	// Audit records one tool call. errorKind is "" when ok, otherwise
	// "scope", "tool" or "internal".
	Audit func(ctx context.Context, userID int64, clientName, tool string, ok bool, errorKind string)
	// ResourceURL is this endpoint's public URL, <public>/mcp: the resource
	// tokens are bound to.
	ResourceURL string
	// Issuer is the authorization server, advertised in the resource
	// metadata.
	Issuer string
	// Version is the build version, reported to clients as the server's
	// implementation version.
	Version string
	Log     *slog.Logger
	Now     func() time.Time
}

const (
	// bodyLimit caps a /mcp request. Tool arguments are short — IDs, a
	// title, a note — so 256 KiB leaves room for a long note without letting
	// a client post megabytes (spec, "Body limits").
	bodyLimit = 256 << 10

	// callsPerMinute is the per-grant rate limit (spec, "Rate limits").
	callsPerMinute = 120

	// metadataPath is the RFC 9728 well-known path. The resource is /mcp, so
	// the RFC's path rule puts its metadata at metadataPath + "/mcp"; the
	// root form is served too for clients that only try that.
	metadataPath = "/.well-known/oauth-protected-resource"

	// internalErrorText is all a model learns of an unexpected failure.
	internalErrorText = "Something went wrong."

	callerKey = "onsuite.caller"
)

// Routes mounts /mcp and the protected-resource metadata. /mcp is anonymous
// by cookie: the caller is an AI client's server holding a bearer token, so
// the route is CSRF-exempt and the bearer middleware is its only guard.
func Routes(mux *http.ServeMux, rec *web.Recorder, d Deps) {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	s := &server{deps: d, tools: buildTools(d.Tools), limit: newLimiter(callsPerMinute, time.Minute, d.Now)}

	md := auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               d.ResourceURL,
		AuthorizationServers:   []string{d.Issuer},
		ScopesSupported:        oauth.AllScopes,
		BearerMethodsSupported: []string{"header"},
	})
	rec.Handle(mux, "GET "+metadataPath, true, md)
	rec.Handle(mux, "GET "+metadataPath+"/mcp", true, md)

	bearer := auth.RequireBearerToken(s.verify, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: metadataURL(d.ResourceURL),
	})
	h := bearer(s.rateLimit(sdk.NewStreamableHTTPHandler(s.serverFor, &sdk.StreamableHTTPOptions{
		// Stateless: every request carries its token and stands alone, so
		// nothing per-session lives in this process (spec, "Decisions").
		Stateless: true,
		// The SDK refuses a loopback connection with a non-loopback Host,
		// against DNS rebinding of local servers. Production runs behind a
		// reverse proxy on loopback with the public Host, which that check
		// would refuse; /mcp is protected by the bearer token instead.
		DisableLocalhostProtection: true,
	})))

	const post = "POST /mcp"
	web.RegisterBodyLimit(post, bodyLimit)
	rec.Handle(mux, post, true, h)
	// Stateless mode has no standalone stream or session to end; the SDK
	// answers these with 405, behind the same token check.
	rec.Handle(mux, "GET /mcp", true, h)
	rec.Handle(mux, "DELETE /mcp", true, h)
}

// metadataURL is where RFC 9728 puts the metadata for resource: the
// well-known path inserted between host and the resource's path.
func metadataURL(resource string) string {
	u, err := url.Parse(resource)
	if err != nil {
		panic(fmt.Sprintf("mcp: resource URL %q: %v", resource, err))
	}
	u.Path = metadataPath + u.Path
	u.RawPath = ""
	return u.String()
}

type server struct {
	deps  Deps
	tools []builtTool
	limit *limiter
}

// builtTool pairs an app tool with its SDK description, built once at
// startup so a request only filters.
type builtTool struct {
	app app.Tool
	sdk *sdk.Tool
}

// buildTools derives each tool's schema and annotations. A schema that cannot
// be derived is a programming error in the app's args struct, found at
// startup, so it panics the way registering a bad mux pattern does.
func buildTools(tools []app.Tool) []builtTool {
	out := make([]builtTool, 0, len(tools))
	for _, t := range tools {
		schema := &jsonschema.Schema{Type: "object"}
		if t.Input != nil {
			s, err := jsonschema.ForType(reflect.TypeOf(t.Input), nil)
			if err != nil {
				panic(fmt.Sprintf("mcp: tool %s: input schema: %v", t.Name, err))
			}
			// The SDK's AddTool panics on a non-object schema, and it runs
			// per request; refusing here moves that to startup.
			if s.Type != "object" {
				panic(fmt.Sprintf("mcp: tool %s: Input must be a struct value, got %T", t.Name, t.Input))
			}
			schema = s
		}
		// Reads say so, so clients can skip confirming them; writes only add
		// or change the user's own data, never destroy it (spec, "Tool
		// rules").
		ann := &sdk.ToolAnnotations{ReadOnlyHint: t.ReadOnly, Title: t.Title}
		if !t.ReadOnly {
			ann.DestructiveHint = new(false)
			ann.IdempotentHint = t.Idempotent
		}
		out = append(out, builtTool{app: t, sdk: &sdk.Tool{
			Name:        t.Name,
			Title:       t.Title,
			Description: t.Description,
			InputSchema: schema,
			Annotations: ann,
		}})
	}
	return out
}

// verify adapts oauth's Verify to the SDK's bearer middleware. Every failure
// is the SDK's ErrInvalidToken, a 401 with the resource metadata pointer, so
// a client re-runs the OAuth flow instead of seeing a 500.
func (s *server) verify(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
	c, err := s.deps.Verify(ctx, token)
	if err != nil {
		return nil, auth.ErrInvalidToken
	}
	return &auth.TokenInfo{
		UserID:     strconv.FormatInt(c.UserID, 10),
		Scopes:     c.Scopes,
		Expiration: c.Expires,
		Extra:      map[string]any{callerKey: c},
	}, nil
}

// callerFrom reads the Caller verify stored on the request. Only handlers
// behind the bearer middleware call it, so it is always there.
func callerFrom(r *http.Request) oauth.Caller {
	c, _ := auth.TokenInfoFromContext(r.Context()).Extra[callerKey].(oauth.Caller)
	return c
}

// rateLimit allows each grant callsPerMinute requests a minute and answers the
// rest 429 with Retry-After.
func (s *server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed, wait := s.limit.allow(callerFrom(r).GrantID)
		if !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(wait.Seconds())))))
			http.Error(w, "Too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// serverFor builds the MCP server one request sees: only the tools the
// caller's grant has scopes for, so tools/list never shows the rest. It is a
// handful of map inserts, cheap enough to do per request.
func (s *server) serverFor(r *http.Request) *sdk.Server {
	c := callerFrom(r)
	srv := sdk.NewServer(&sdk.Implementation{Name: "onsuite", Title: "ON Suite", Version: s.deps.Version}, nil)
	hidden := map[string]bool{}
	for _, t := range s.tools {
		if slices.Contains(c.Scopes, t.app.Scope) {
			srv.AddTool(t.sdk, s.handler(c, t.app))
		} else {
			hidden[t.app.Name] = true
		}
	}
	srv.AddReceivingMiddleware(s.refuseHidden(c, hidden))
	return srv
}

// refuseHidden answers a direct call to a tool the grant lacks the scope
// for. The SDK would return a protocol error for an unknown tool; a tool
// error result tells the model plainly, and the attempt is audited.
func (s *server) refuseHidden(c oauth.Caller, hidden map[string]bool) sdk.Middleware {
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			if call, ok := req.(*sdk.CallToolRequest); ok && call.Params != nil && hidden[call.Params.Name] {
				s.audit(ctx, c, call.Params.Name, "scope")
				return errorResult("This connection is not allowed to use " + call.Params.Name +
					". The user can grant it by reconnecting ON Suite with more access."), nil
			}
			return next(ctx, method, req)
		}
	}
}

// handler runs one tool for the token's user and audits the outcome. A
// ToolError's message is meant for the model; anything else is logged and
// hidden behind internalErrorText (spec, "Tool rules").
func (s *server) handler(c oauth.Caller, t app.Tool) sdk.ToolHandler {
	return func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		args := req.Params.Arguments
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		text, err := t.Call(ctx, c.UserID, args)
		var te app.ToolError
		switch {
		case err == nil:
			s.audit(ctx, c, t.Name, "")
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}}, nil
		case errors.As(err, &te):
			s.audit(ctx, c, t.Name, "tool")
			return errorResult(te.Msg), nil
		default:
			s.deps.Log.Error("mcp: tool failed", "tool", t.Name, "user", c.UserID, "err", err)
			s.audit(ctx, c, t.Name, "internal")
			return errorResult(internalErrorText), nil
		}
	}
}

// audit records a call. It outlives a cancelled request: the call happened
// either way.
func (s *server) audit(ctx context.Context, c oauth.Caller, tool, errorKind string) {
	s.deps.Audit(context.WithoutCancel(ctx), c.UserID, c.ClientName, tool, errorKind == "", errorKind)
}

func errorResult(msg string) *sdk.CallToolResult {
	return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: msg}}}
}
