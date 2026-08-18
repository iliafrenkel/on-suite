package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

const (
	// SessionTTL is how long a session lives without being used.
	SessionTTL = 30 * 24 * time.Hour

	// SessionRenewInterval throttles the sliding expiry. A session is only
	// written back once this much of its life has passed, so an active user
	// costs at most one session write per day rather than one per request.
	SessionRenewInterval = 24 * time.Hour

	sessionIDBytes = 32 // 256 bits, base64url encoded to 43 characters
)

// Session is a logged-in browser. It carries no user detail; callers look the
// user up by UserID, so revoking a session never leaves stale user data
// cached in a cookie.
type Session struct {
	ID        string
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
}

// CreateSession issues a new session for userID.
func (s *Store) CreateSession(ctx context.Context, userID int64) (Session, error) {
	id, err := newSessionID()
	if err != nil {
		return Session{}, err
	}
	now := s.now()
	sess := Session{
		ID:        id,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: now.Add(SessionTTL),
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		sess.ID, sess.UserID, formatTime(sess.CreatedAt), formatTime(sess.ExpiresAt),
	); err != nil {
		return Session{}, fmt.Errorf("auth: create session: %w", err)
	}
	return sess, nil
}

// UseSession fetches a live session and slides its expiry forward if it is
// due for renewal.
//
// It is named "Use" rather than "Get" because it writes. An expired or
// unknown id both yield ErrNotFound: the caller's response is identical
// either way, and distinguishing them would leak whether an id was ever
// valid.
func (s *Store) UseSession(ctx context.Context, id string) (Session, error) {
	if id == "" {
		return Session{}, ErrNotFound
	}

	var sess Session
	var createdAt, expiresAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, created_at, expires_at FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &sess.UserID, &createdAt, &expiresAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Session{}, ErrNotFound
	case err != nil:
		return Session{}, fmt.Errorf("auth: read session: %w", err)
	}

	if sess.CreatedAt, err = parseTime(createdAt); err != nil {
		return Session{}, err
	}
	if sess.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return Session{}, err
	}

	now := s.now()
	if !sess.ExpiresAt.After(now) {
		// Expired. Leave the row for DeleteExpiredSessions rather than
		// deleting on a read path.
		return Session{}, ErrNotFound
	}

	if sess.ExpiresAt.Sub(now) < SessionTTL-SessionRenewInterval {
		renewed := now.Add(SessionTTL)
		if _, err := s.db.ExecContext(ctx,
			`UPDATE sessions SET expires_at = ? WHERE id = ?`, formatTime(renewed), sess.ID,
		); err != nil {
			return Session{}, fmt.Errorf("auth: renew session: %w", err)
		}
		sess.ExpiresAt = renewed
	}
	return sess, nil
}

// DeleteSession revokes one session. It is idempotent, so a logout from an
// already-logged-out browser is not an error.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("auth: delete session: %w", err)
	}
	return nil
}

// DeleteUserSessions revokes every session belonging to a user — used when a
// password changes, so a stolen cookie stops working.
func (s *Store) DeleteUserSessions(ctx context.Context, userID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	if err != nil {
		return 0, fmt.Errorf("auth: delete user sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("auth: delete user sessions: %w", err)
	}
	return n, nil
}

// DeleteExpiredSessions removes sessions past their expiry. Timestamps are
// RFC 3339 UTC text, so a string comparison is a chronological comparison.
func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at <= ?`, formatTime(s.now()))
	if err != nil {
		return 0, fmt.Errorf("auth: sweep sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("auth: sweep sessions: %w", err)
	}
	return n, nil
}

func newSessionID() (string, error) {
	buf := make([]byte, sessionIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate session id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
