package auth

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func mustUser(t *testing.T, s *Store, name string, admin bool) User {
	t.Helper()
	u, err := s.CreateUser(context.Background(), name, "$argon2id$old", admin)
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", name, err)
	}
	return u
}

func mustSession(t *testing.T, s *Store, userID int64) Session {
	t.Helper()
	sess, err := s.CreateSession(context.Background(), userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return sess
}

func sessionAlive(t *testing.T, s *Store, id string) bool {
	t.Helper()
	_, err := s.UseSession(context.Background(), id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		t.Fatalf("UseSession: %v", err)
	}
	return err == nil
}

func TestSetPasswordReplacesTheHashAndRevokesEverySession(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	u := mustUser(t, s, "alice", false)
	a, b := mustSession(t, s, u.ID), mustSession(t, s, u.ID)

	if err := s.SetPassword(ctx, u.ID, "$argon2id$new", ""); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	got, err := s.UserByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PasswordHash != "$argon2id$new" {
		t.Errorf("hash = %q, want the new one", got.PasswordHash)
	}
	if sessionAlive(t, s, a.ID) || sessionAlive(t, s, b.ID) {
		t.Error("a session survived a password reset")
	}
}

func TestSetPasswordKeepsTheNamedSessionOnly(t *testing.T) {
	s, _ := newStore(t)
	u := mustUser(t, s, "alice", false)
	keep, drop := mustSession(t, s, u.ID), mustSession(t, s, u.ID)

	if err := s.SetPassword(context.Background(), u.ID, "$argon2id$new", keep.ID); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if !sessionAlive(t, s, keep.ID) {
		t.Error("the kept session was revoked")
	}
	if sessionAlive(t, s, drop.ID) {
		t.Error("another session survived")
	}
}

func TestSetPasswordLeavesOtherUsersSessionsAlone(t *testing.T) {
	s, _ := newStore(t)
	alice, bob := mustUser(t, s, "alice", false), mustUser(t, s, "bob", false)
	bobs := mustSession(t, s, bob.ID)

	if err := s.SetPassword(context.Background(), alice.ID, "$argon2id$new", ""); err != nil {
		t.Fatal(err)
	}
	if !sessionAlive(t, s, bobs.ID) {
		t.Error("resetting alice revoked bob's session")
	}
}

func TestSetPasswordRejectsAnEmptyHash(t *testing.T) {
	s, _ := newStore(t)
	u := mustUser(t, s, "alice", false)
	if err := s.SetPassword(context.Background(), u.ID, "", ""); err == nil {
		t.Fatal("SetPassword accepted an empty hash")
	}
}

func TestSetAdminPromotesAndDemotes(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	mustUser(t, s, "root", true)
	u := mustUser(t, s, "alice", false)

	if err := s.SetAdmin(ctx, u.ID, true); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if got, _ := s.UserByID(ctx, u.ID); !got.IsAdmin {
		t.Error("promote did not take")
	}
	if err := s.SetAdmin(ctx, u.ID, false); err != nil {
		t.Fatalf("demote: %v", err)
	}
	if got, _ := s.UserByID(ctx, u.ID); got.IsAdmin {
		t.Error("demote did not take")
	}
}

func TestSetAdminRefusesToDemoteTheLastAdmin(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	root := mustUser(t, s, "root", true)
	mustUser(t, s, "alice", false)

	if err := s.SetAdmin(ctx, root.ID, false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("err = %v, want ErrLastAdmin", err)
	}
	if got, _ := s.UserByID(ctx, root.ID); !got.IsAdmin {
		t.Error("the last admin was demoted anyway")
	}
}

func TestDeleteUserRefusesTheLastAdmin(t *testing.T) {
	s, _ := newStore(t)
	root := mustUser(t, s, "root", true)
	if err := s.DeleteUser(context.Background(), root.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("err = %v, want ErrLastAdmin", err)
	}
}

func TestDeleteUserAllowsAnAdminWhenAnotherRemains(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	mustUser(t, s, "root", true)
	second := mustUser(t, s, "second", true)
	if err := s.DeleteUser(ctx, second.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := s.UserByID(ctx, second.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByID after delete: err = %v, want ErrNotFound", err)
	}
}

// Every app table references users with ON DELETE CASCADE; this stands in
// for them so the test does not depend on any app's schema.
func TestDeleteUserCascadesToSessionsAndOwnedRows(t *testing.T) {
	s, handle := newStore(t)
	ctx := context.Background()
	mustUser(t, s, "root", true)
	u := mustUser(t, s, "alice", false)
	sess := mustSession(t, s, u.ID)

	if _, err := handle.ExecContext(ctx, `CREATE TABLE demo_things (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE
	) STRICT`); err != nil {
		t.Fatal(err)
	}
	if _, err := handle.ExecContext(ctx, `INSERT INTO demo_things (user_id) VALUES (?)`, u.ID); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	var n int
	if err := handle.QueryRowContext(ctx, `SELECT count(*) FROM demo_things`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d owned rows survived the delete", n)
	}
	if sessionAlive(t, s, sess.ID) {
		t.Error("the deleted user's session survived")
	}
}

func TestManageMethodsReportAMissingUser(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	mustUser(t, s, "root", true)
	const missing = 9999

	if err := s.SetPassword(ctx, missing, "$argon2id$x", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetPassword: err = %v, want ErrNotFound", err)
	}
	if err := s.SetAdmin(ctx, missing, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetAdmin: err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteUser(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteUser: err = %v, want ErrNotFound", err)
	}
}

// seedOAuth gives userID one grant with an access-token row, and one audit row.
func seedOAuth(t *testing.T, handle *sql.DB, userID int64, grantID string) {
	t.Helper()
	ctx := context.Background()
	const ts = "2026-11-24T00:00:00.000000000Z"
	if _, err := handle.ExecContext(ctx,
		`INSERT INTO oauth_grants (id, user_id, client_id, client_name, redirect_host,
		        requested_scopes, granted_scopes, created_at)
		 VALUES (?, ?, 'c1', 'Claude', 'claude.ai', 'books:read', 'books:read', ?)`,
		grantID, userID, ts); err != nil {
		t.Fatalf("insert grant: %v", err)
	}
	if _, err := handle.ExecContext(ctx,
		`INSERT INTO oauth_sessions (signature, kind, grant_id, request_id, client_id, data, expires_at)
		 VALUES (?, 'access', ?, ?, 'c1', x'00', ?)`,
		"sig-"+grantID, grantID, grantID, ts); err != nil {
		t.Fatalf("insert oauth session: %v", err)
	}
	if _, err := handle.ExecContext(ctx,
		`INSERT INTO mcp_audit (user_id, client_name, tool, ok, at) VALUES (?, 'Claude', 'books_list', 1, ?)`,
		userID, ts); err != nil {
		t.Fatalf("insert audit: %v", err)
	}
}

func countRows(t *testing.T, handle *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := handle.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSetPasswordRevokesOAuthGrants(t *testing.T) {
	s, handle := newStore(t)
	mustUser(t, s, "root", true)
	alice := mustUser(t, s, "alice", false)
	bob := mustUser(t, s, "bob", false)
	seedOAuth(t, handle, alice.ID, "g-alice")
	seedOAuth(t, handle, bob.ID, "g-bob")

	if err := s.SetPassword(context.Background(), alice.ID, "$argon2id$new", ""); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	if n := countRows(t, handle, `SELECT count(*) FROM oauth_grants WHERE user_id = ?`, alice.ID); n != 0 {
		t.Errorf("%d grants survived the password change", n)
	}
	if n := countRows(t, handle, `SELECT count(*) FROM oauth_sessions WHERE grant_id = 'g-alice'`); n != 0 {
		t.Errorf("%d token rows survived the password change", n)
	}
	if n := countRows(t, handle, `SELECT count(*) FROM oauth_grants WHERE user_id = ?`, bob.ID); n != 1 {
		t.Errorf("another user's grants = %d, want 1", n)
	}
	if n := countRows(t, handle, `SELECT count(*) FROM oauth_sessions WHERE grant_id = 'g-bob'`); n != 1 {
		t.Errorf("another user's token rows = %d, want 1", n)
	}
	// Activity history is not a credential: it outlives the revoked grant.
	if n := countRows(t, handle, `SELECT count(*) FROM mcp_audit WHERE user_id = ?`, alice.ID); n != 1 {
		t.Errorf("audit rows after revoke = %d, want 1", n)
	}
}

func TestDeleteUserCascadesOAuthRows(t *testing.T) {
	s, handle := newStore(t)
	mustUser(t, s, "root", true)
	alice := mustUser(t, s, "alice", false)
	seedOAuth(t, handle, alice.ID, "g-alice")

	if err := s.DeleteUser(context.Background(), alice.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	for _, table := range []string{"oauth_grants", "oauth_sessions", "mcp_audit"} {
		if n := countRows(t, handle, `SELECT count(*) FROM `+table); n != 0 {
			t.Errorf("%s: %d rows survived the user's deletion", table, n)
		}
	}
}

func TestDeletingAGrantCascadesToItsTokens(t *testing.T) {
	s, handle := newStore(t)
	alice := mustUser(t, s, "alice", true)
	seedOAuth(t, handle, alice.ID, "g1")
	if _, err := handle.ExecContext(context.Background(), `DELETE FROM oauth_grants WHERE id = 'g1'`); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, handle, `SELECT count(*) FROM oauth_sessions`); n != 0 {
		t.Errorf("%d token rows survived their grant", n)
	}
}

func TestOAuthSessionsKeyAndGrantRule(t *testing.T) {
	s, handle := newStore(t)
	alice := mustUser(t, s, "alice", true)
	seedOAuth(t, handle, alice.ID, "g1")
	ctx := context.Background()
	const ts = "2026-11-24T00:00:00.000000000Z"
	insert := func(kind, sig string, grant any) error {
		_, err := handle.ExecContext(ctx,
			`INSERT INTO oauth_sessions (signature, kind, grant_id, request_id, client_id, data, expires_at)
			 VALUES (?, ?, ?, 'r', 'c1', x'00', ?)`, sig, kind, grant, ts)
		return err
	}
	// The library keeps the auth-code and PKCE sessions under one signature.
	if err := insert("code", "same", "g1"); err != nil {
		t.Fatalf("code row: %v", err)
	}
	if err := insert("pkce", "same", "g1"); err != nil {
		t.Errorf("pkce row with the code's signature: %v", err)
	}
	if err := insert("code", "same", "g1"); err == nil {
		t.Error("duplicate (kind, signature) accepted")
	}
	if err := insert("registration", "reg", nil); err != nil {
		t.Errorf("registration row without a grant: %v", err)
	}
	if err := insert("access", "orphan", nil); err == nil {
		t.Error("access row without a grant accepted")
	}
	if err := insert("registration", "reg2", "g1"); err == nil {
		t.Error("registration row with a grant accepted")
	}
}
