package auth

import (
	"context"
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
