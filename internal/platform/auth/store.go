package auth

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"time"
)

// Namespace is the migration namespace the platform's own schema is recorded
// under. Apps use their own id instead.
const Namespace = "platform"

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations returns the platform's schema, rooted so that filenames appear
// at the top level, matching what db.Collect expects.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		// Unreachable: the path is a compile-time constant checked by go:embed.
		panic("auth: embedded migrations missing: " + err.Error())
	}
	return sub
}

var (
	ErrNotFound          = errors.New("auth: not found")
	ErrDuplicateUsername = errors.New("auth: username already taken")
	ErrInvalidUsername   = errors.New("auth: invalid username")
)

// usernamePattern keeps usernames URL-safe and unambiguous, which matters
// because they appear in log lines and in admin pages.
var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]{1,30})[a-zA-Z0-9]$`)

// User is an account. PasswordHash is carried so the login path can verify
// it; nothing renders it.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	IsAdmin      bool
	CreatedAt    time.Time
}

// Store is all the SQL for identity. It has no HTTP knowledge, which is what
// lets it be tested against a real database with no server running.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(handle *sql.DB) *Store {
	return &Store{db: handle, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the time source. It exists so expiry behaviour can be
// tested by moving time forward instead of sleeping for thirty days.
func (s *Store) SetClock(now func() time.Time) { s.now = now }

// ValidateUsername reports whether name is acceptable for a new account.
func ValidateUsername(name string) error {
	if !usernamePattern.MatchString(name) {
		return fmt.Errorf("%w: %q must be 3-32 characters of letters, digits, dot, dash or underscore, starting and ending alphanumeric", ErrInvalidUsername, name)
	}
	return nil
}

// fold is the canonical form used for uniqueness and lookup.
func fold(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// CreateUser inserts an account. passwordHash must already come from
// HashPassword; this function never sees a plaintext password.
func (s *Store) CreateUser(ctx context.Context, username, passwordHash string, isAdmin bool) (User, error) {
	username = strings.TrimSpace(username)
	if err := ValidateUsername(username); err != nil {
		return User{}, err
	}
	if passwordHash == "" {
		return User{}, errors.New("auth: password hash must not be empty")
	}

	now := s.now()
	var (
		u         = User{Username: username, PasswordHash: passwordHash, IsAdmin: isAdmin}
		createdAt string
	)
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO users (username, username_fold, password_hash, is_admin, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 RETURNING id, created_at`,
		username, fold(username), passwordHash, boolToInt(isAdmin), formatTime(now),
	).Scan(&u.ID, &createdAt)
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, fmt.Errorf("%w: %s", ErrDuplicateUsername, username)
		}
		return User{}, fmt.Errorf("auth: create user: %w", err)
	}

	u.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return User{}, err
	}
	return u, nil
}

// UserByUsername looks up an account case-insensitively.
func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, is_admin, created_at
		 FROM users WHERE username_fold = ?`, fold(username)))
}

func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, is_admin, created_at
		 FROM users WHERE id = ?`, id))
}

// CountUsers lets the serve command warn when a database has no accounts,
// which is the one situation a fresh self-hoster is guaranteed to hit.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&n); err != nil {
		return 0, fmt.Errorf("auth: count users: %w", err)
	}
	return n, nil
}

// Account is one row of the admin page's user table.
//
// It deliberately omits PasswordHash. User carries the hash because the login
// path must verify it; nothing that renders a list of accounts has any reason
// to hold one.
type Account struct {
	ID        int64
	Username  string
	IsAdmin   bool
	CreatedAt time.Time
	// Sessions is how many unexpired sessions this account has right now.
	Sessions int
}

// ListAccounts returns every account, oldest first, each with a count of its
// live sessions. It is read-only and exists for the admin page.
func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	now := formatTime(s.now())
	rows, err := s.db.QueryContext(ctx,
		`SELECT u.id, u.username, u.is_admin, u.created_at,
		        (SELECT count(*) FROM sessions se
		          WHERE se.user_id = u.id AND se.expires_at > ?)
		   FROM users u
		  ORDER BY u.id`, now)
	if err != nil {
		return nil, fmt.Errorf("auth: list accounts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Account
	for rows.Next() {
		var (
			a         Account
			isAdmin   int
			createdAt string
		)
		if err := rows.Scan(&a.ID, &a.Username, &isAdmin, &createdAt, &a.Sessions); err != nil {
			return nil, fmt.Errorf("auth: scan account: %w", err)
		}
		a.IsAdmin = isAdmin == 1
		if a.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: list accounts: %w", err)
	}
	return out, nil
}

// SessionCounts reports how many sessions are live and how many are past
// their expiry but not yet swept. A growing expired count means the sweep job
// is not running, which is exactly what the admin page is for.
func (s *Store) SessionCounts(ctx context.Context) (live, expired int, err error) {
	var total int
	// coalesce because sum() over an empty table is NULL, not 0.
	err = s.db.QueryRowContext(ctx,
		`SELECT count(*),
		        coalesce(sum(CASE WHEN expires_at > ? THEN 1 ELSE 0 END), 0)
		   FROM sessions`, formatTime(s.now())).Scan(&total, &live)
	if err != nil {
		return 0, 0, fmt.Errorf("auth: count sessions: %w", err)
	}
	return live, total - live, nil
}

func (s *Store) scanUser(row *sql.Row) (User, error) {
	var (
		u         User
		isAdmin   int
		createdAt string
	)
	switch err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &isAdmin, &createdAt); {
	case errors.Is(err, sql.ErrNoRows):
		return User{}, ErrNotFound
	case err != nil:
		return User{}, fmt.Errorf("auth: scan user: %w", err)
	}
	u.IsAdmin = isAdmin == 1

	var err error
	if u.CreatedAt, err = parseTime(createdAt); err != nil {
		return User{}, err
	}
	return u, nil
}

// Timestamps are stored as RFC 3339 nanosecond strings in UTC, which sort
// lexically in the same order they sort chronologically.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("auth: parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isUniqueViolation avoids importing the driver package just to read an error
// code. Matching on the message is unattractive but keeps this package free
// of a driver dependency, and the substring is stable in SQLite.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
