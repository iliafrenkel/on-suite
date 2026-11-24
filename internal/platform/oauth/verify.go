package oauth

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"authelia.com/provider/oauth2"
)

// ErrInvalidToken is the one answer for every way a bearer token can fail
// (unknown, expired, revoked, wrong kind, wrong resource, deleted user), so
// the /mcp endpoint cannot be used to learn which check refused it.
var ErrInvalidToken = errors.New("oauth: invalid token")

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
// Every failure, including a database error, refuses the token: failing
// closed is the safe answer for an auth check.
func (s *Server) Verify(ctx context.Context, bearer string) (Caller, error) {
	use, req, err := s.provider.IntrospectToken(ctx, bearer, oauth2.AccessToken, new(oauth2.DefaultSession))
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
	if err != nil || g.UserID != userID {
		return Caller{}, ErrInvalidToken
	}
	if err := s.store.TouchGrant(ctx, g.ID); err != nil {
		return Caller{}, ErrInvalidToken
	}
	return Caller{
		UserID:     userID,
		GrantID:    g.ID,
		ClientName: g.ClientName,
		Scopes:     slices.Clone([]string(req.GetGrantedScopes())),
		Expires:    sess.GetExpiresAt(oauth2.AccessToken),
	}, nil
}
