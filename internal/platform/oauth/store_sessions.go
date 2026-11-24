package oauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"authelia.com/provider/oauth2"
)

// Session kinds, as the oauth_sessions CHECK constraint names them. One
// table holds them all and (kind, signature) is the key, because the
// library stores the auth-code and PKCE sessions under the same signature.
const (
	kindCode         = "code"
	kindPKCE         = "pkce"
	kindAccess       = "access"
	kindRefresh      = "refresh"
	kindRegistration = "registration"
)

// neverExpires stands in for "no expiry" in the NOT NULL expires_at column:
// RFC 7592 registration tokens never expire, so Sweep must never match
// them.
var neverExpires = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)

// storedRequest is the part of a library Requester that survives a round
// trip. The client is stored by ID only and rebuilt on read, so a revoked or
// changed client takes effect on the next request rather than living on
// inside old sessions.
type storedRequest struct {
	ID                string           `json:"id"`
	RequestedAt       time.Time        `json:"requested_at"`
	ClientID          string           `json:"client_id"`
	RequestedScope    oauth2.Arguments `json:"requested_scope"`
	GrantedScope      oauth2.Arguments `json:"granted_scope"`
	RequestedAudience oauth2.Arguments `json:"requested_audience"`
	GrantedAudience   oauth2.Arguments `json:"granted_audience"`
	RequestedResource oauth2.Arguments `json:"requested_resource"`
	GrantedResource   oauth2.Arguments `json:"granted_resource"`
	Form              url.Values       `json:"form"`
	Session           json.RawMessage  `json:"session"`
	// Claims and authorization details are deliberately not persisted: this
	// server does no OpenID Connect and no RFC 9396 (RAR).
}

func encodeRequest(req oauth2.Requester) ([]byte, error) {
	if req.GetClient() == nil {
		return nil, errors.New("oauth: request has no client")
	}
	session, err := json.Marshal(req.GetSession())
	if err != nil {
		return nil, fmt.Errorf("oauth: encode session: %w", err)
	}
	data, err := json.Marshal(storedRequest{
		ID:                req.GetID(),
		RequestedAt:       req.GetRequestedAt(),
		ClientID:          req.GetClient().GetID(),
		RequestedScope:    req.GetRequestedScopes(),
		GrantedScope:      req.GetGrantedScopes(),
		RequestedAudience: req.GetRequestedAudience(),
		GrantedAudience:   req.GetGrantedAudience(),
		RequestedResource: req.GetRequestedResource(),
		GrantedResource:   req.GetGrantedResource(),
		Form:              req.GetRequestForm(),
		Session:           session,
	})
	if err != nil {
		return nil, fmt.Errorf("oauth: encode request: %w", err)
	}
	return data, nil
}

// decodeRequest rebuilds a Requester, hydrating the caller's session as the
// storage interfaces require (the library reads the session it passed in).
// The library sometimes passes a nil session, so a DefaultSession stands in.
func (s *Store) decodeRequest(ctx context.Context, data []byte, session oauth2.Session) (oauth2.Requester, error) {
	var sr storedRequest
	if err := json.Unmarshal(data, &sr); err != nil {
		return nil, fmt.Errorf("oauth: decode request: %w", err)
	}
	if session == nil {
		session = new(oauth2.DefaultSession)
	}
	if len(sr.Session) > 0 && string(sr.Session) != "null" {
		if err := json.Unmarshal(sr.Session, session); err != nil {
			return nil, fmt.Errorf("oauth: decode session: %w", err)
		}
	}
	// A nil *Config inside an `any` would pass GetClient's type assertion
	// and then be dereferenced, so pass a true nil until setConfig runs.
	var cfg any
	if s.cfg != nil {
		cfg = s.cfg
	}
	client, err := oauth2.GetClient(ctx, cfg, s, sr.ClientID)
	if err != nil {
		return nil, err
	}
	form := sr.Form
	if form == nil {
		form = url.Values{}
	}
	return &oauth2.Request{
		ID:                sr.ID,
		RequestedAt:       sr.RequestedAt,
		Client:            client,
		RequestedScope:    orEmpty(sr.RequestedScope),
		GrantedScope:      orEmpty(sr.GrantedScope),
		RequestedAudience: orEmpty(sr.RequestedAudience),
		GrantedAudience:   orEmpty(sr.GrantedAudience),
		RequestedResource: orEmpty(sr.RequestedResource),
		GrantedResource:   orEmpty(sr.GrantedResource),
		Form:              form,
		Session:           session,
	}, nil
}

func orEmpty(a oauth2.Arguments) oauth2.Arguments {
	if a == nil {
		return oauth2.Arguments{}
	}
	return a
}

// expiry is when the row may be swept: the session's own expiry for the
// token type, which is also what the library checks (plan, "Library
// notes": persist session.ExpiresAt).
func expiry(req oauth2.Requester, tt oauth2.TokenType) time.Time {
	if sess := req.GetSession(); sess != nil {
		if t := sess.GetExpiresAt(tt); !t.IsZero() {
			return t
		}
	}
	return neverExpires
}

// createSession writes one session row. grant_id is the request ID for
// every kind but registration, so a code or token for a request whose grant
// row doesn't exist fails on the foreign key: consent writes the grant
// first, and revoking the grant cascades to every row.
func (s *Store) createSession(ctx context.Context, kind, signature, accessSignature string, req oauth2.Requester, expiresAt time.Time) error {
	data, err := encodeRequest(req)
	if err != nil {
		return err
	}
	var grantID, accessSig any
	if kind != kindRegistration {
		grantID = req.GetID()
	}
	if accessSignature != "" {
		accessSig = accessSignature
	}
	_, err = s.q(ctx).ExecContext(ctx,
		`INSERT INTO oauth_sessions
		   (signature, kind, grant_id, request_id, client_id, active, access_signature, data, expires_at)
		 VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		signature, kind, grantID, req.GetID(), req.GetClient().GetID(), accessSig, data, formatTime(expiresAt))
	if err != nil {
		return fmt.Errorf("oauth: create %s session: %w", kind, err)
	}
	return nil
}

// getSession loads one session row. It reports whether the row is active
// rather than deciding what an inactive row means, which differs by kind.
func (s *Store) getSession(ctx context.Context, kind, signature string, session oauth2.Session) (oauth2.Requester, bool, error) {
	var (
		data   []byte
		active bool
	)
	err := s.q(ctx).QueryRowContext(ctx,
		`SELECT data, active FROM oauth_sessions WHERE kind = ? AND signature = ?`,
		kind, signature).Scan(&data, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, oauth2.ErrNotFound
	}
	if err != nil {
		return nil, false, fmt.Errorf("oauth: get %s session: %w", kind, err)
	}
	req, err := s.decodeRequest(ctx, data, session)
	if err != nil {
		return nil, false, err
	}
	return req, active, nil
}

// deleteSession removes one row; a missing row is not an error, matching
// the library's reference store.
func (s *Store) deleteSession(ctx context.Context, kind, signature string) error {
	if _, err := s.q(ctx).ExecContext(ctx,
		`DELETE FROM oauth_sessions WHERE kind = ? AND signature = ?`, kind, signature); err != nil {
		return fmt.Errorf("oauth: delete %s session: %w", kind, err)
	}
	return nil
}

// deactivate flips one active row to inactive and says why it couldn't:
// already inactive, or no such row.
func (s *Store) deactivate(ctx context.Context, q querier, kind, signature string, inactive error) error {
	res, err := q.ExecContext(ctx,
		`UPDATE oauth_sessions SET active = 0 WHERE kind = ? AND signature = ? AND active = 1`,
		kind, signature)
	if err != nil {
		return fmt.Errorf("oauth: deactivate %s: %w", kind, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("oauth: rows affected: %w", err)
	} else if n == 1 {
		return nil
	}
	var one int
	err = q.QueryRowContext(ctx,
		`SELECT 1 FROM oauth_sessions WHERE kind = ? AND signature = ?`, kind, signature).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return oauth2.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("oauth: deactivate %s: %w", kind, err)
	}
	return inactive
}

// CreateAuthorizeCodeSession stores the request behind an authorization code.
func (s *Store) CreateAuthorizeCodeSession(ctx context.Context, code string, req oauth2.Requester) error {
	return s.createSession(ctx, kindCode, code, "", req, expiry(req, oauth2.AuthorizeCode))
}

// GetAuthorizeCodeSession returns the request behind a code. An invalidated
// code comes back with its requester and ErrInvalidatedAuthorizeCode, which
// is how the library spots a replayed code and revokes the grant.
func (s *Store) GetAuthorizeCodeSession(ctx context.Context, code string, session oauth2.Session) (oauth2.Requester, error) {
	req, active, err := s.getSession(ctx, kindCode, code, session)
	if err != nil {
		return nil, err
	}
	if !active {
		return req, oauth2.ErrInvalidatedAuthorizeCode
	}
	return req, nil
}

// InvalidateAuthorizeCodeSession marks a code used.
func (s *Store) InvalidateAuthorizeCodeSession(ctx context.Context, code string) error {
	return s.deactivate(ctx, s.q(ctx), kindCode, code, oauth2.ErrInvalidatedAuthorizeCode)
}

// CreatePKCERequestSession stores the PKCE request under the code's signature.
func (s *Store) CreatePKCERequestSession(ctx context.Context, signature string, req oauth2.Requester) error {
	return s.createSession(ctx, kindPKCE, signature, "", req, expiry(req, oauth2.AuthorizeCode))
}

// GetPKCERequestSession returns the PKCE request for a code's signature.
func (s *Store) GetPKCERequestSession(ctx context.Context, signature string, session oauth2.Session) (oauth2.Requester, error) {
	req, _, err := s.getSession(ctx, kindPKCE, signature, session)
	return req, err
}

// DeletePKCERequestSession removes the PKCE request for a code's signature.
func (s *Store) DeletePKCERequestSession(ctx context.Context, signature string) error {
	return s.deleteSession(ctx, kindPKCE, signature)
}

// SetPKCEChallengeUsed records a challenge as redeemed, in one statement so
// two concurrent requests can't both succeed. An expired record is replaced;
// a live one leaves the upsert's WHERE false, so no row changes and the
// challenge is a replay.
func (s *Store) SetPKCEChallengeUsed(ctx context.Context, clientID, challenge string, expiresAt time.Time) error {
	res, err := s.q(ctx).ExecContext(ctx,
		`INSERT INTO oauth_pkce_used (client_id, challenge, expires_at) VALUES (?, ?, ?)
		 ON CONFLICT (client_id, challenge) DO UPDATE SET expires_at = excluded.expires_at
		 WHERE oauth_pkce_used.expires_at < ?`,
		clientID, challenge, formatTime(expiresAt), formatTime(s.now()))
	if err != nil {
		return fmt.Errorf("oauth: set pkce challenge used: %w", err)
	}
	return requireRow(res, oauth2.ErrPKCEChallengeReused)
}

// CreateAccessTokenSession stores the request behind an access token.
func (s *Store) CreateAccessTokenSession(ctx context.Context, signature string, req oauth2.Requester) error {
	return s.createSession(ctx, kindAccess, signature, "", req, expiry(req, oauth2.AccessToken))
}

// GetAccessTokenSession returns the request behind an access token.
func (s *Store) GetAccessTokenSession(ctx context.Context, signature string, session oauth2.Session) (oauth2.Requester, error) {
	req, _, err := s.getSession(ctx, kindAccess, signature, session)
	return req, err
}

// DeleteAccessTokenSession removes one access token.
func (s *Store) DeleteAccessTokenSession(ctx context.Context, signature string) error {
	return s.deleteSession(ctx, kindAccess, signature)
}

// RevokeAccessToken removes every access token of the request, including
// those issued by rotation, which share its ID.
func (s *Store) RevokeAccessToken(ctx context.Context, requestID string) error {
	if _, err := s.q(ctx).ExecContext(ctx,
		`DELETE FROM oauth_sessions WHERE kind = 'access' AND request_id = ?`, requestID); err != nil {
		return fmt.Errorf("oauth: revoke access tokens: %w", err)
	}
	return nil
}

// CreateRefreshTokenSession stores the request behind a refresh token, with
// the access token issued beside it.
func (s *Store) CreateRefreshTokenSession(ctx context.Context, signature, accessSignature string, req oauth2.Requester) error {
	return s.createSession(ctx, kindRefresh, signature, accessSignature, req, expiry(req, oauth2.RefreshToken))
}

// GetRefreshTokenSession returns the request behind a refresh token. A
// rotated or revoked token comes back with its requester and
// ErrInactiveToken: that pair is what makes the library revoke the whole
// grant when an old refresh token is reused (spec, "Lifetimes").
func (s *Store) GetRefreshTokenSession(ctx context.Context, signature string, session oauth2.Session) (oauth2.Requester, error) {
	req, active, err := s.getSession(ctx, kindRefresh, signature, session)
	if err != nil {
		return nil, err
	}
	if !active {
		return req, oauth2.ErrInactiveToken
	}
	return req, nil
}

// UpdateRefreshTokenSession replaces the stored request, keeping the
// token's active state and its paired access signature.
func (s *Store) UpdateRefreshTokenSession(ctx context.Context, signature string, req oauth2.Requester) error {
	data, err := encodeRequest(req)
	if err != nil {
		return err
	}
	res, err := s.q(ctx).ExecContext(ctx,
		`UPDATE oauth_sessions SET data = ?, expires_at = ? WHERE kind = 'refresh' AND signature = ?`,
		data, formatTime(expiry(req, oauth2.RefreshToken)), signature)
	if err != nil {
		return fmt.Errorf("oauth: update refresh session: %w", err)
	}
	return requireRow(res, oauth2.ErrNotFound)
}

// DeleteRefreshTokenSession removes one refresh token.
func (s *Store) DeleteRefreshTokenSession(ctx context.Context, signature string) error {
	return s.deleteSession(ctx, kindRefresh, signature)
}

// RotateRefreshToken deactivates the presented refresh token and revokes
// the request's access tokens, atomically. The row is kept, inactive, so a
// later reuse is still recognised. An unknown signature is ErrNotFound and
// an already-rotated one ErrInactiveToken, as the interface demands.
func (s *Store) RotateRefreshToken(ctx context.Context, requestID, signature string) error {
	return s.inTx(ctx, func(q querier) error {
		if err := s.deactivate(ctx, q, kindRefresh, signature, oauth2.ErrInactiveToken); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx,
			`DELETE FROM oauth_sessions WHERE kind = 'access' AND request_id = ?`, requestID); err != nil {
			return fmt.Errorf("oauth: revoke access tokens: %w", err)
		}
		return nil
	})
}

// RevokeRefreshToken deactivates every refresh token of the request, the
// rotated ones and the live one alike.
func (s *Store) RevokeRefreshToken(ctx context.Context, requestID string) error {
	if _, err := s.q(ctx).ExecContext(ctx,
		`UPDATE oauth_sessions SET active = 0 WHERE kind = 'refresh' AND request_id = ?`, requestID); err != nil {
		return fmt.Errorf("oauth: revoke refresh tokens: %w", err)
	}
	return nil
}

// CreateClientRegistrationTokenSession stores an RFC 7592 registration
// token. Its rows have no grant: they exist before any user approves
// anything.
func (s *Store) CreateClientRegistrationTokenSession(ctx context.Context, signature string, req oauth2.Requester) error {
	return s.createSession(ctx, kindRegistration, signature, "", req, neverExpires)
}

// GetClientRegistrationTokenSession returns the request behind a
// registration token.
func (s *Store) GetClientRegistrationTokenSession(ctx context.Context, signature string, session oauth2.Session) (oauth2.Requester, error) {
	req, _, err := s.getSession(ctx, kindRegistration, signature, session)
	return req, err
}

// DeleteClientRegistrationTokenSession removes a registration token.
func (s *Store) DeleteClientRegistrationTokenSession(ctx context.Context, signature string) error {
	return s.deleteSession(ctx, kindRegistration, signature)
}
