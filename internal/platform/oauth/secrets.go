package oauth

import (
	"context"
	"crypto/rand"
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
// INSERT OR IGNORE keeps an existing value, so concurrent first starts
// agree on one secret.
func (s *Store) Secrets(ctx context.Context) (hmac, registration []byte, err error) {
	err = s.inTx(ctx, func(q querier) error {
		for _, name := range []string{secretHMAC, secretRegistration} {
			value := make([]byte, 32)
			if _, err := rand.Read(value); err != nil {
				return fmt.Errorf("oauth: generate %s secret: %w", name, err)
			}
			if _, err := q.ExecContext(ctx,
				`INSERT OR IGNORE INTO oauth_secrets (name, value, created_at) VALUES (?, ?, ?)`,
				name, value, formatTime(s.now())); err != nil {
				return fmt.Errorf("oauth: store %s secret: %w", name, err)
			}
		}
		if err := q.QueryRowContext(ctx,
			`SELECT value FROM oauth_secrets WHERE name = ?`, secretHMAC).Scan(&hmac); err != nil {
			return fmt.Errorf("oauth: read hmac secret: %w", err)
		}
		if err := q.QueryRowContext(ctx,
			`SELECT value FROM oauth_secrets WHERE name = ?`, secretRegistration).Scan(&registration); err != nil {
			return fmt.Errorf("oauth: read registration secret: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return hmac, registration, nil
}
