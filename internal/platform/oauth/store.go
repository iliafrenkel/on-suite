// Package oauth is the OAuth 2.1 authorization server behind AI access
// (spec docs/superpowers/specs/2026-11-24-ai-access-mcp-design.md). It is
// the only package allowed to import authelia.com/provider/oauth2.
package oauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"authelia.com/provider/oauth2"
	hoauth2 "authelia.com/provider/oauth2/handler/oauth2"
	"authelia.com/provider/oauth2/handler/pkce"
	"authelia.com/provider/oauth2/handler/rfc7591"
	"authelia.com/provider/oauth2/storage"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// Every storage interface the composed provider needs (plan, "Library
// notes" §Storage). Listing them here makes a missed method a build error
// rather than a type assertion that silently fails inside the library: the
// library discovers storage.Transactional by assertion, and without it a
// replayed code or refresh token revokes nothing.
var (
	_ oauth2.Storage                         = (*Store)(nil)
	_ oauth2.ClientManager                   = (*Store)(nil)
	_ hoauth2.CoreStorage                    = (*Store)(nil)
	_ hoauth2.TokenRevocationStorage         = (*Store)(nil)
	_ pkce.Storage                           = (*Store)(nil)
	_ rfc7591.Storage                        = (*Store)(nil)
	_ oauth2.ClientIDMetadataDocumentStorage = (*Store)(nil)
	_ storage.Transactional                  = (*Store)(nil)
)

// ErrNotFound is returned by the grant methods for a grant that does not
// exist, or that belongs to someone else. The library-facing methods return
// oauth2.ErrNotFound instead, because the library matches on that.
var ErrNotFound = errors.New("oauth: not found")

// Store is all the SQL behind the authorization server: the library's
// storage interfaces plus the grants, secrets and audit log the rest of
// the platform reads.
type Store struct {
	db  *sql.DB
	now func() time.Time

	// cfg lets a stored session's client be rebuilt through oauth2.GetClient,
	// which falls back to the CIMD resolver for a client ID that is a URL.
	// The provider's New sets it once, before serving (ruling R1).
	cfg *oauth2.Config
}

// NewStore wraps an open, migrated database. now is the only clock the
// store reads, so tests can pin it.
func NewStore(handle *sql.DB, now func() time.Time) *Store {
	return &Store{db: handle, now: now}
}

// setConfig hands the store the composed provider config, which client
// rebuilding on session reads needs.
func (s *Store) setConfig(cfg *oauth2.Config) { s.cfg = cfg }

func formatTime(t time.Time) string { return db.FormatTime(t) }

func parseTime(v string) (time.Time, error) {
	t, err := db.ParseTime(v)
	if err != nil {
		return time.Time{}, fmt.Errorf("oauth: parse time %q: %w", v, err)
	}
	return t, nil
}

// querier is what *sql.DB and *sql.Tx have in common, so every method runs
// the same SQL inside or outside a library transaction.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type txKey struct{}

// q returns the transaction ctx carries, or the database. The library
// passes the context BeginTX returned to every call it makes inside a
// transaction (storage.Transactional's contract), so this is how those
// calls join it.
func (s *Store) q(ctx context.Context) querier {
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return tx
	}
	return s.db
}

// BeginTX implements storage.Transactional. The database has a single
// connection (internal/platform/db), so a second transaction begun on top
// of the first would wait for that connection forever; that is refused
// instead of hanging the request.
func (s *Store) BeginTX(ctx context.Context) (context.Context, error) {
	if _, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return ctx, errors.New("oauth: transaction already in progress")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ctx, fmt.Errorf("oauth: begin: %w", err)
	}
	return context.WithValue(ctx, txKey{}, tx), nil
}

// Commit implements storage.Transactional.
func (s *Store) Commit(ctx context.Context) error {
	tx, ok := ctx.Value(txKey{}).(*sql.Tx)
	if !ok {
		return errors.New("oauth: commit without a transaction")
	}
	return tx.Commit()
}

// Rollback implements storage.Transactional. Rolling back a transaction
// that already finished is not an error, because the library's error paths
// may roll back after a commit failed.
func (s *Store) Rollback(ctx context.Context) error {
	tx, ok := ctx.Value(txKey{}).(*sql.Tx)
	if !ok {
		return errors.New("oauth: rollback without a transaction")
	}
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return err
	}
	return nil
}

// inTx runs fn in the caller's transaction when ctx carries one, or in a
// transaction of its own, so a multi-statement method never half-applies.
func (s *Store) inTx(ctx context.Context, fn func(q querier) error) error {
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return fn(tx)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("oauth: begin: %w", err)
	}
	// Deferred so a panic in fn still releases the database's only
	// connection; after Commit it is a no-op returning sql.ErrTxDone.
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// GetClient loads a dynamically registered client. It answers
// oauth2.ErrNotFound for anything else, CIMD rows included: that error is
// what sends the library to the CIMD resolver, whose validators must run on
// every fetch (plan, "Library notes" §Allowlist hooks).
func (s *Store) GetClient(ctx context.Context, id string) (oauth2.Client, error) {
	var data []byte
	err := s.q(ctx).QueryRowContext(ctx,
		`SELECT data FROM oauth_clients WHERE id = ? AND kind = 'dcr'`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, oauth2.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("oauth: get client: %w", err)
	}
	client := &oauth2.DefaultRegisteredClient{DefaultClient: &oauth2.DefaultClient{}}
	if err := json.Unmarshal(data, client); err != nil {
		return nil, fmt.Errorf("oauth: decode client %q: %w", id, err)
	}
	return client, nil
}

// registered narrows a client to the one type dynamic registration creates
// (rfc7591.DefaultClientRegistrationStrategy), the only type GetClient can
// rebuild from JSON.
func registered(client oauth2.Client) (*oauth2.DefaultRegisteredClient, []byte, []byte, error) {
	rc, ok := client.(*oauth2.DefaultRegisteredClient)
	if !ok || rc.DefaultClient == nil {
		return nil, nil, nil, fmt.Errorf("oauth: cannot store a client of type %T", client)
	}
	data, err := json.Marshal(rc)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("oauth: encode client: %w", err)
	}
	uris, err := json.Marshal(rc.GetRedirectURIs())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("oauth: encode redirect URIs: %w", err)
	}
	return rc, data, uris, nil
}

// CreateClient stores a dynamically registered client. A duplicate ID fails
// on the primary key, as ClientRegistrationManager requires.
func (s *Store) CreateClient(ctx context.Context, client oauth2.Client) error {
	rc, data, uris, err := registered(client)
	if err != nil {
		return err
	}
	_, err = s.q(ctx).ExecContext(ctx,
		`INSERT INTO oauth_clients (id, kind, name, redirect_uris, data, created_at)
		 VALUES (?, 'dcr', ?, ?, ?, ?)`,
		rc.GetID(), rc.ClientName, string(uris), data, formatTime(s.now()))
	if err != nil {
		return fmt.Errorf("oauth: create client: %w", err)
	}
	return nil
}

// UpdateClient replaces a dynamically registered client.
func (s *Store) UpdateClient(ctx context.Context, id string, client oauth2.Client) error {
	rc, data, uris, err := registered(client)
	if err != nil {
		return err
	}
	if rc.GetID() != id {
		return fmt.Errorf("oauth: client %q cannot be updated with client %q", id, rc.GetID())
	}
	res, err := s.q(ctx).ExecContext(ctx,
		`UPDATE oauth_clients SET name = ?, redirect_uris = ?, data = ? WHERE id = ? AND kind = 'dcr'`,
		rc.ClientName, string(uris), data, id)
	if err != nil {
		return fmt.Errorf("oauth: update client: %w", err)
	}
	return requireRow(res, oauth2.ErrNotFound)
}

// DeleteClient removes a dynamically registered client and its
// registration-token sessions, which would otherwise outlive it.
func (s *Store) DeleteClient(ctx context.Context, id string) error {
	return s.inTx(ctx, func(q querier) error {
		res, err := q.ExecContext(ctx, `DELETE FROM oauth_clients WHERE id = ? AND kind = 'dcr'`, id)
		if err != nil {
			return fmt.Errorf("oauth: delete client: %w", err)
		}
		if err := requireRow(res, oauth2.ErrNotFound); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx,
			`DELETE FROM oauth_sessions WHERE kind = 'registration' AND client_id = ?`, id); err != nil {
			return fmt.Errorf("oauth: delete client registration tokens: %w", err)
		}
		return nil
	})
}

var errNoClientAssertions = errors.New("oauth: client assertions are not supported")

// ClientAssertionJWTValid refuses: every client here is public
// (token_endpoint_auth_method "none"), so a client assertion never
// legitimately arrives, and refusing is the safe stub.
func (s *Store) ClientAssertionJWTValid(context.Context, string, string) error {
	return errNoClientAssertions
}

// SetClientAssertionJWT refuses, for the same reason.
func (s *Store) SetClientAssertionJWT(context.Context, string, string, time.Time) error {
	return errNoClientAssertions
}

// GetClientIDMetadataDocument returns the cached CIMD record for id.
func (s *Store) GetClientIDMetadataDocument(ctx context.Context, id string) (*oauth2.ClientIDMetadataDocumentRecord, error) {
	var data []byte
	err := s.q(ctx).QueryRowContext(ctx,
		`SELECT record FROM oauth_cimd WHERE client_id = ?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, oauth2.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("oauth: get cimd: %w", err)
	}
	record := &oauth2.ClientIDMetadataDocumentRecord{}
	if err := json.Unmarshal(data, record); err != nil {
		return nil, fmt.Errorf("oauth: decode cimd %q: %w", id, err)
	}
	return record, nil
}

// SetClientIDMetadataDocument creates or replaces the record for record.ID.
func (s *Store) SetClientIDMetadataDocument(ctx context.Context, record *oauth2.ClientIDMetadataDocumentRecord) error {
	if record == nil {
		return errors.New("oauth: nil cimd record")
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("oauth: encode cimd: %w", err)
	}
	_, err = s.q(ctx).ExecContext(ctx,
		`INSERT INTO oauth_cimd (client_id, record, expires_at) VALUES (?, ?, ?)
		 ON CONFLICT (client_id) DO UPDATE SET record = excluded.record, expires_at = excluded.expires_at`,
		record.ID, data, formatTime(record.ExpiresAt))
	if err != nil {
		return fmt.Errorf("oauth: set cimd: %w", err)
	}
	return nil
}

// DeleteClientIDMetadataDocument removes the record for id; a missing record
// is not an error.
func (s *Store) DeleteClientIDMetadataDocument(ctx context.Context, id string) error {
	if _, err := s.q(ctx).ExecContext(ctx, `DELETE FROM oauth_cimd WHERE client_id = ?`, id); err != nil {
		return fmt.Errorf("oauth: delete cimd: %w", err)
	}
	return nil
}

// DeleteExpiredClientIDMetadataDocuments removes records that expired
// before the given time.
func (s *Store) DeleteExpiredClientIDMetadataDocuments(ctx context.Context, before time.Time) error {
	if _, err := s.q(ctx).ExecContext(ctx,
		`DELETE FROM oauth_cimd WHERE expires_at < ?`, formatTime(before)); err != nil {
		return fmt.Errorf("oauth: delete expired cimd: %w", err)
	}
	return nil
}

// CountClientIDMetadataDocuments counts the records for one domain. The
// domain lives only inside the JSON record (the table has no column for
// it), and the cache holds a handful of rows, so they are decoded here.
func (s *Store) CountClientIDMetadataDocuments(ctx context.Context, domain string) (int, error) {
	rows, err := s.q(ctx).QueryContext(ctx, `SELECT record FROM oauth_cimd`)
	if err != nil {
		return 0, fmt.Errorf("oauth: count cimd: %w", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return 0, fmt.Errorf("oauth: count cimd: %w", err)
		}
		var record oauth2.ClientIDMetadataDocumentRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return 0, fmt.Errorf("oauth: decode cimd: %w", err)
		}
		if record.Domain == domain {
			n++
		}
	}
	return n, rows.Err()
}

// RevokeClientIDMetadataDocumentRefreshTokens deactivates every refresh
// token issued to the client id, which the library calls when that URL's
// document now names another client.
func (s *Store) RevokeClientIDMetadataDocumentRefreshTokens(ctx context.Context, id string) error {
	if _, err := s.q(ctx).ExecContext(ctx,
		`UPDATE oauth_sessions SET active = 0 WHERE kind = 'refresh' AND client_id = ?`, id); err != nil {
		return fmt.Errorf("oauth: revoke cimd refresh tokens: %w", err)
	}
	return nil
}

// requireRow turns "no row affected" into notFound.
func requireRow(res sql.Result, notFound error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("oauth: rows affected: %w", err)
	}
	if n == 0 {
		return notFound
	}
	return nil
}
