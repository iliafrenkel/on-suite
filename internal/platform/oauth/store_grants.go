package oauth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// auditRetention is how long tool-call history is kept (spec, "Audit").
const auditRetention = 90 * 24 * time.Hour

// Grant is one user-approved connection: what the user sees on /account
// and revokes. ID is the library's request ID, stable across code exchange
// and every refresh, so each token row points back at its grant.
type Grant struct {
	ID                   string
	UserID               int64
	Username             string
	ClientID, ClientName string
	RedirectHost         string
	Requested, Granted   []string
	CreatedAt            time.Time
	LastUsedAt           time.Time // zero until first use
}

// AuditEntry is one MCP tool call. It deliberately has no argument or
// result fields: the log says who called what and whether it worked, never
// what the user's data said (spec, "Storage").
type AuditEntry struct {
	UserID           int64
	ClientName, Tool string
	OK               bool
	ErrorKind        string
	At               time.Time
}

const grantColumns = `g.id, g.user_id, u.username, g.client_id, g.client_name, g.redirect_host,
	g.requested_scopes, g.granted_scopes, g.created_at, g.last_used_at`

const grantFrom = ` FROM oauth_grants g JOIN users u ON u.id = g.user_id`

func scanGrant(row interface{ Scan(...any) error }) (Grant, error) {
	var (
		g                  Grant
		requested, granted string
		created            string
		lastUsed           sql.NullString
	)
	if err := row.Scan(&g.ID, &g.UserID, &g.Username, &g.ClientID, &g.ClientName, &g.RedirectHost,
		&requested, &granted, &created, &lastUsed); err != nil {
		return Grant{}, err
	}
	g.Requested = strings.Fields(requested)
	g.Granted = strings.Fields(granted)
	var err error
	if g.CreatedAt, err = parseTime(created); err != nil {
		return Grant{}, err
	}
	if lastUsed.Valid {
		if g.LastUsedAt, err = parseTime(lastUsed.String); err != nil {
			return Grant{}, err
		}
	}
	return g, nil
}

// UpsertGrant writes g and deletes any other grant the same user holds for
// the same client, in one transaction: one connection per (user, client)
// (spec, "Storage"), and the replaced grant's tokens go with it by cascade.
func (s *Store) UpsertGrant(ctx context.Context, g Grant) error {
	created := g.CreatedAt
	if created.IsZero() {
		created = s.now()
	}
	return s.inTx(ctx, func(q querier) error {
		if _, err := q.ExecContext(ctx,
			`DELETE FROM oauth_grants WHERE user_id = ? AND client_id = ? AND id <> ?`,
			g.UserID, g.ClientID, g.ID); err != nil {
			return fmt.Errorf("oauth: replace grant: %w", err)
		}
		res, err := q.ExecContext(ctx,
			`INSERT INTO oauth_grants
			   (id, user_id, client_id, client_name, redirect_host, requested_scopes, granted_scopes, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT (id) DO UPDATE SET
			   client_name = excluded.client_name, redirect_host = excluded.redirect_host,
			   requested_scopes = excluded.requested_scopes, granted_scopes = excluded.granted_scopes
			 WHERE oauth_grants.user_id = excluded.user_id AND oauth_grants.client_id = excluded.client_id`,
			g.ID, g.UserID, g.ClientID, g.ClientName, g.RedirectHost,
			strings.Join(g.Requested, " "), strings.Join(g.Granted, " "), formatTime(created))
		if err != nil {
			return fmt.Errorf("oauth: upsert grant: %w", err)
		}
		// An ID already held by another user or client is never rewritten:
		// the guarded update changes nothing, and failing here rolls back the
		// delete above too, so the caller learns consent was not recorded.
		return requireRow(res, fmt.Errorf("oauth: upsert grant: id %q belongs to another user or client", g.ID))
	})
}

// GrantFor returns the user's grant for a client, if any, so consent can
// be skipped when nothing new is asked for (spec, "Consent").
func (s *Store) GrantFor(ctx context.Context, userID int64, clientID string) (Grant, bool, error) {
	g, err := scanGrant(s.q(ctx).QueryRowContext(ctx,
		`SELECT `+grantColumns+grantFrom+` WHERE g.user_id = ? AND g.client_id = ?`, userID, clientID))
	if errors.Is(err, sql.ErrNoRows) {
		return Grant{}, false, nil
	}
	if err != nil {
		return Grant{}, false, fmt.Errorf("oauth: grant for user: %w", err)
	}
	return g, true, nil
}

// Grant returns one grant by ID, or ErrNotFound.
func (s *Store) Grant(ctx context.Context, id string) (Grant, error) {
	g, err := scanGrant(s.q(ctx).QueryRowContext(ctx,
		`SELECT `+grantColumns+grantFrom+` WHERE g.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Grant{}, ErrNotFound
	}
	if err != nil {
		return Grant{}, fmt.Errorf("oauth: grant: %w", err)
	}
	return g, nil
}

// TouchGrant records that the grant was just used, for "last used" on
// /account.
func (s *Store) TouchGrant(ctx context.Context, id string) error {
	res, err := s.q(ctx).ExecContext(ctx,
		`UPDATE oauth_grants SET last_used_at = ? WHERE id = ?`, formatTime(s.now()), id)
	if err != nil {
		return fmt.Errorf("oauth: touch grant: %w", err)
	}
	return requireRow(res, ErrNotFound)
}

// RevokeGrant deletes a grant, and by cascade every code and token it
// issued: the one revocation primitive (migration 0003). userID 0 is the
// admin, who may revoke anyone's; otherwise someone else's grant is
// ErrNotFound, the same answer as a grant that doesn't exist.
func (s *Store) RevokeGrant(ctx context.Context, userID int64, id string) error {
	res, err := s.q(ctx).ExecContext(ctx,
		`DELETE FROM oauth_grants WHERE id = ? AND (? = 0 OR user_id = ?)`, id, userID, userID)
	if err != nil {
		return fmt.Errorf("oauth: revoke grant: %w", err)
	}
	return requireRow(res, ErrNotFound)
}

// Grants lists a user's grants, or every user's for userID 0 (the admin
// view), newest first.
func (s *Store) Grants(ctx context.Context, userID int64) ([]Grant, error) {
	rows, err := s.q(ctx).QueryContext(ctx,
		`SELECT `+grantColumns+grantFrom+` WHERE (? = 0 OR g.user_id = ?)
		 ORDER BY g.created_at DESC, g.id`, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("oauth: grants: %w", err)
	}
	defer rows.Close()
	var out []Grant
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, fmt.Errorf("oauth: grants: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Audit records one tool call. A zero At means now.
func (s *Store) Audit(ctx context.Context, e AuditEntry) error {
	at := e.At
	if at.IsZero() {
		at = s.now()
	}
	if _, err := s.q(ctx).ExecContext(ctx,
		`INSERT INTO mcp_audit (user_id, client_name, tool, ok, error_kind, at) VALUES (?, ?, ?, ?, ?, ?)`,
		e.UserID, e.ClientName, e.Tool, e.OK, e.ErrorKind, formatTime(at)); err != nil {
		return fmt.Errorf("oauth: audit: %w", err)
	}
	return nil
}

// RecentAudit returns the user's last n tool calls, newest first.
func (s *Store) RecentAudit(ctx context.Context, userID int64, n int) ([]AuditEntry, error) {
	rows, err := s.q(ctx).QueryContext(ctx,
		`SELECT user_id, client_name, tool, ok, error_kind, at FROM mcp_audit
		 WHERE user_id = ? ORDER BY at DESC, id DESC LIMIT ?`, userID, n)
	if err != nil {
		return nil, fmt.Errorf("oauth: recent audit: %w", err)
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var (
			e  AuditEntry
			at string
		)
		if err := rows.Scan(&e.UserID, &e.ClientName, &e.Tool, &e.OK, &e.ErrorKind, &at); err != nil {
			return nil, fmt.Errorf("oauth: recent audit: %w", err)
		}
		if e.At, err = parseTime(at); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Sweep deletes what has expired: code and token sessions, redeemed PKCE
// challenges, cached CIMD documents, and audit rows older than 90 days
// (spec, "Storage": the maintenance job). It returns how many rows went.
// Inactive refresh tokens stay until they expire, so a reuse is still
// detected until then.
func (s *Store) Sweep(ctx context.Context) (int, error) {
	now := s.now()
	cutoff := formatTime(now)
	steps := []struct{ query, arg string }{
		{`DELETE FROM oauth_sessions WHERE expires_at < ?`, cutoff},
		{`DELETE FROM oauth_pkce_used WHERE expires_at < ?`, cutoff},
		{`DELETE FROM oauth_cimd WHERE expires_at < ?`, cutoff},
		{`DELETE FROM mcp_audit WHERE at < ?`, formatTime(now.Add(-auditRetention))},
	}
	total := 0
	err := s.inTx(ctx, func(q querier) error {
		for _, step := range steps {
			res, err := q.ExecContext(ctx, step.query, step.arg)
			if err != nil {
				return fmt.Errorf("oauth: sweep: %w", err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return fmt.Errorf("oauth: sweep: %w", err)
			}
			total += int(n)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}
