package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Personal tokens (spec 2026-11-26-personal-tokens-design.md) are long-lived
// bearer tokens a user creates on /account/assistants for clients that can't
// do OAuth, such as the Pebble ring. Each is an ordinary grant with
// client_id PersonalClientID plus one hashed secret, so everything that
// hangs off a grant (the list, Disconnect, audit, password-change cleanup)
// works unchanged.

// PersonalClientID is the client_id of every personal-token grant. No OAuth
// client can have it: DCR IDs are random and CIMD IDs are https URLs.
const PersonalClientID = "personal"

// personalTokenPrefix marks a personal token, so Verify can route it without
// asking the OAuth library and secret scanners can spot one.
const personalTokenPrefix = "onst_"

// maxPersonalTokens is how many personal tokens one user may hold.
const maxPersonalTokens = 10

// personalIdle is how long a personal token may go unused before the sweep
// removes it. There is no expiry date otherwise (Ilia's decision).
const personalIdle = 90 * 24 * time.Hour

// ErrTooManyTokens means the user already holds maxPersonalTokens.
var ErrTooManyTokens = errors.New("oauth: too many personal tokens")

// ErrGrantExists means a grant with that ID already exists. The create form
// uses it to spot a re-sent POST.
var ErrGrantExists = errors.New("oauth: grant already exists")

// newPersonalToken returns a fresh token and the hash that is stored for it.
// 32 random bytes are far beyond guessing, so a fast hash is enough.
func newPersonalToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("oauth: personal token: %w", err)
	}
	token = personalTokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	return token, hashPersonalToken(token), nil
}

// hashPersonalToken is the lowercase hex SHA-256 of the whole token, prefix
// included: the only form a personal token is ever stored in.
func hashPersonalToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newGrantID returns 32 lowercase hex characters from 16 random bytes.
func newGrantID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("oauth: grant id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// validGrantID reports whether id has newGrantID's shape. The create form
// posts one back, so it is checked before it reaches the database.
func validGrantID(id string) bool {
	if len(id) != 32 {
		return false
	}
	return strings.Trim(id, "0123456789abcdef") == ""
}

// CreatePersonalToken stores a personal-token grant and its hash in one
// transaction. It never goes through UpsertGrant, which keeps one grant per
// (user, client) and would delete the user's other personal tokens. An ID
// already in use is ErrGrantExists, whoever holds it; a user at the limit is
// ErrTooManyTokens.
func (s *Store) CreatePersonalToken(ctx context.Context, g Grant, tokenHash string) error {
	created := g.CreatedAt
	if created.IsZero() {
		created = s.now()
	}
	return s.inTx(ctx, func(q querier) error {
		var n int
		if err := q.QueryRowContext(ctx,
			`SELECT count(*) FROM oauth_grants WHERE user_id = ? AND client_id = ?`,
			g.UserID, PersonalClientID).Scan(&n); err != nil {
			return fmt.Errorf("oauth: count personal tokens: %w", err)
		}
		if n >= maxPersonalTokens {
			return ErrTooManyTokens
		}
		res, err := q.ExecContext(ctx,
			`INSERT INTO oauth_grants
			   (id, user_id, client_id, client_name, redirect_host, requested_scopes, granted_scopes, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT (id) DO NOTHING`,
			g.ID, g.UserID, PersonalClientID, g.ClientName, g.RedirectHost,
			strings.Join(g.Requested, " "), strings.Join(g.Granted, " "), formatTime(created))
		if err != nil {
			return fmt.Errorf("oauth: create personal grant: %w", err)
		}
		if err := requireRow(res, ErrGrantExists); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx,
			`INSERT INTO oauth_personal_tokens (grant_id, token_hash) VALUES (?, ?)`,
			g.ID, tokenHash); err != nil {
			return fmt.Errorf("oauth: create personal token: %w", err)
		}
		return nil
	})
}

// PersonalTokenGrant returns the grant a personal token's hash belongs to,
// or ErrNotFound.
func (s *Store) PersonalTokenGrant(ctx context.Context, tokenHash string) (Grant, error) {
	g, err := scanGrant(s.q(ctx).QueryRowContext(ctx,
		`SELECT `+grantColumns+grantFrom+`
		 JOIN oauth_personal_tokens t ON t.grant_id = g.id
		 WHERE t.token_hash = ?`, tokenHash))
	if errors.Is(err, sql.ErrNoRows) {
		return Grant{}, ErrNotFound
	}
	if err != nil {
		return Grant{}, fmt.Errorf("oauth: personal token: %w", err)
	}
	return g, nil
}
