package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrLastAdmin refuses a change that would leave no administrator. With none,
// /admin/users is unreachable and only hand-written SQL could fix it (#310).
var ErrLastAdmin = errors.New("auth: at least one administrator must remain")

// SetPassword replaces userID's password hash and revokes every session of
// theirs except keepSessionID ("" keeps none), in one transaction. Revoking
// is the point: an admin's reset must lock out whoever held the old
// password, and a self-service change keeps only the browser that made it
// (spec 2026-09-27-user-management-design.md §6).
func (s *Store) SetPassword(ctx context.Context, userID int64, passwordHash, keepSessionID string) error {
	if passwordHash == "" {
		return errors.New("auth: password hash must not be empty")
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID)
		if err != nil {
			return fmt.Errorf("auth: set password: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("auth: set password: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		// No session has an empty id, so keepSessionID "" revokes them all.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM sessions WHERE user_id = ? AND id <> ?`, userID, keepSessionID); err != nil {
			return fmt.Errorf("auth: revoke sessions: %w", err)
		}
		return nil
	})
}

// SetAdmin grants or removes administrator rights. It needs no session
// changes: LoadUser re-reads the account on every request, so the new role
// applies from the target's next page load.
func (s *Store) SetAdmin(ctx context.Context, userID int64, isAdmin bool) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		wasAdmin, err := roleTx(ctx, tx, userID)
		if err != nil {
			return err
		}
		if wasAdmin && !isAdmin {
			if err := requireOtherAdmin(ctx, tx, userID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE users SET is_admin = ? WHERE id = ?`, boolToInt(isAdmin), userID); err != nil {
			return fmt.Errorf("auth: set admin: %w", err)
		}
		return nil
	})
}

// DeleteUser removes an account. Every table that references users does so
// with ON DELETE CASCADE (sessions and every app's rows), so this one
// DELETE removes everything the account owned.
func (s *Store) DeleteUser(ctx context.Context, userID int64) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		wasAdmin, err := roleTx(ctx, tx, userID)
		if err != nil {
			return err
		}
		if wasAdmin {
			if err := requireOtherAdmin(ctx, tx, userID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID); err != nil {
			return fmt.Errorf("auth: delete user: %w", err)
		}
		return nil
	})
}

// roleTx reports whether userID is an admin, or ErrNotFound.
func roleTx(ctx context.Context, tx *sql.Tx, userID int64) (bool, error) {
	var isAdmin int
	err := tx.QueryRowContext(ctx, `SELECT is_admin FROM users WHERE id = ?`, userID).Scan(&isAdmin)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, ErrNotFound
	case err != nil:
		return false, fmt.Errorf("auth: read role: %w", err)
	}
	return isAdmin == 1, nil
}

// requireOtherAdmin returns ErrLastAdmin unless an admin other than userID
// exists. It runs in the same transaction as the write it guards, so the
// count cannot go stale between check and change.
func requireOtherAdmin(ctx context.Context, tx *sql.Tx, userID int64) error {
	var n int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM users WHERE is_admin = 1 AND id <> ?`, userID).Scan(&n); err != nil {
		return fmt.Errorf("auth: count admins: %w", err)
	}
	if n == 0 {
		return ErrLastAdmin
	}
	return nil
}

// inTx runs fn in a transaction, committing only if it returns nil.
func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("auth: begin: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("auth: commit: %w", err)
	}
	return nil
}
