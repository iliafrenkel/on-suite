package oauth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
)

// Secret names in oauth_secrets.
const (
	secretHMAC         = "hmac"
	secretRegistration = "registration"
)

// Secrets returns the token-signing (hmac) and client-registration secrets,
// generating each on first use. They are stored rather than configured so
// tokens survive a restart with nothing to set up, and they are generated
// independently because the library refuses to compose unless both are at
// least 32 bytes and different (plan, "Library notes": must-set fields).
// Random bytes are drawn only for a secret that is missing, and the read and
// the write share one transaction, so concurrent first starts agree on one
// value.
func (s *Store) Secrets(ctx context.Context) (hmac, registration []byte, err error) {
	err = s.inTx(ctx, func(q querier) error {
		if hmac, err = s.secret(ctx, q, secretHMAC); err != nil {
			return err
		}
		registration, err = s.secret(ctx, q, secretRegistration)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return hmac, registration, nil
}

// secret returns the named secret, creating it first if it is missing.
func (s *Store) secret(ctx context.Context, q querier, name string) ([]byte, error) {
	var value []byte
	err := q.QueryRowContext(ctx, `SELECT value FROM oauth_secrets WHERE name = ?`, name).Scan(&value)
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("oauth: read %s secret: %w", name, err)
	}
	value = make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return nil, fmt.Errorf("oauth: generate %s secret: %w", name, err)
	}
	if _, err := q.ExecContext(ctx,
		`INSERT INTO oauth_secrets (name, value, created_at) VALUES (?, ?, ?)`,
		name, value, formatTime(s.now())); err != nil {
		return nil, fmt.Errorf("oauth: store %s secret: %w", name, err)
	}
	return value, nil
}
