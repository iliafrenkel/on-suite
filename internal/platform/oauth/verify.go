package oauth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"authelia.com/provider/oauth2"
)

// ErrInvalidToken is the one answer for every way a bearer token can fail
// (unknown, expired, revoked, wrong kind, wrong resource, deleted user), so
// the /mcp endpoint cannot be used to learn which check refused it.
var ErrInvalidToken = errors.New("oauth: invalid token")

// ErrUnavailable means the token could not be checked at all, because storage
// failed. The token is refused either way, but a client told "invalid" drops
// the connection and makes the user sign in again, so a passing database
// problem must not look like a revoked token (#629).
var ErrUnavailable = errors.New("oauth: token check unavailable")

// touchEvery is how stale "last used" may get before Verify writes it again:
// fine enough for /account, without a write on every tool call.
const touchEvery = time.Minute

// Caller is who a verified bearer token speaks for.
type Caller struct {
	UserID     int64
	GrantID    string
	ClientName string
	Scopes     []string
	Expires    time.Time
}

// Verify checks a bearer token presented to /mcp and says who it is for.
// Introspection alone is not enough: the library falls back to refresh
// tokens and never checks the resource, so both are checked here (plan,
// "Library notes" Verify; spec 2026-11-24-ai-access-mcp-design.md). Revoking
// a grant deletes its token rows, so a revoked token is simply not found.
// Every failure refuses the token: failing closed is the safe answer for an
// auth check. A storage failure is ErrUnavailable rather than
// ErrInvalidToken, so the client retries instead of reconnecting.
func (s *Server) Verify(ctx context.Context, bearer string) (Caller, error) {
	if strings.HasPrefix(bearer, personalTokenPrefix) {
		return s.verifyPersonal(ctx, bearer)
	}
	use, req, err := s.provider.IntrospectToken(ctx, bearer, oauth2.AccessToken, new(oauth2.DefaultSession))
	if errors.Is(err, oauth2.ErrServerError) {
		// The library reports anything its storage returned, other than
		// "not found", as a server error.
		return Caller{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if err != nil || use != oauth2.AccessToken || req == nil {
		return Caller{}, ErrInvalidToken
	}
	if !slices.Contains([]string(req.GetGrantedResource()), s.ResourceURL()) {
		return Caller{}, ErrInvalidToken
	}
	sess := req.GetSession()
	if sess == nil {
		return Caller{}, ErrInvalidToken
	}
	userID, err := strconv.ParseInt(sess.GetSubject(), 10, 64)
	if err != nil {
		return Caller{}, ErrInvalidToken
	}
	g, err := s.store.Grant(ctx, req.GetID())
	switch {
	case errors.Is(err, ErrNotFound):
		return Caller{}, ErrInvalidToken
	case err != nil:
		return Caller{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	case g.UserID != userID:
		return Caller{}, ErrInvalidToken
	}
	s.touch(ctx, g)
	return Caller{
		UserID:     userID,
		GrantID:    g.ID,
		ClientName: g.ClientName,
		Scopes:     slices.Clone([]string(req.GetGrantedScopes())),
		Expires:    sess.GetExpiresAt(oauth2.AccessToken),
	}, nil
}

// verifyPersonal checks a personal token (spec
// 2026-11-26-personal-tokens-design.md, "Verify"). Nothing here goes through
// the OAuth library: the token is looked up by its hash, and the grant it
// belongs to says who it speaks for and what it may do.
func (s *Server) verifyPersonal(ctx context.Context, bearer string) (Caller, error) {
	g, err := s.store.PersonalTokenGrant(ctx, hashPersonalToken(bearer))
	switch {
	case errors.Is(err, ErrNotFound):
		return Caller{}, ErrInvalidToken
	case err != nil:
		return Caller{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	s.touch(ctx, g)
	return Caller{
		UserID:     g.UserID,
		GrantID:    g.ID,
		ClientName: g.ClientName,
		Scopes:     slices.Clone(g.Granted),
		// A personal token has no expiry date, but the MCP SDK refuses a
		// zero one. It only checks this within the current request, so a
		// day ahead is plenty; the real limit is the sweep's 90 days
		// without use (personalIdle).
		Expires: s.store.now().Add(24 * time.Hour),
	}, nil
}

// touch records that g was just used, at most once every touchEvery. It is
// bookkeeping: a failed write is logged, never a reason to refuse the token.
func (s *Server) touch(ctx context.Context, g Grant) {
	if s.store.now().Sub(g.LastUsedAt) < touchEvery {
		return
	}
	if err := s.store.TouchGrant(ctx, g.ID); err != nil {
		s.log.Warn("oauth: recording when a connection was last used failed", "grant", g.ID, "error", err)
	}
}
