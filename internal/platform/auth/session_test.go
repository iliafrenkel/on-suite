package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// clock is a manually advanced time source, so expiry tests are instant and
// deterministic rather than depending on sleeps.
type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

// newSessionStore returns a migrated store, a fixed clock, and a user to
// attach sessions to.
func newSessionStore(t *testing.T) (*Store, *clock, User) {
	t.Helper()

	s, _ := newStore(t)
	c := &clock{t: time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)}
	s.SetClock(c.now)

	u, err := s.CreateUser(context.Background(), "ilia", "$argon2id$fake", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return s, c, u
}

func TestCreateSession(t *testing.T) {
	s, c, u := newSessionStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if len(sess.ID) < 40 {
		t.Errorf("session id %q is only %d characters, want at least 40", sess.ID, len(sess.ID))
	}
	if sess.UserID != u.ID {
		t.Errorf("UserID = %d, want %d", sess.UserID, u.ID)
	}
	if want := c.t.Add(SessionTTL); !sess.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", sess.ExpiresAt, want)
	}

	// Ids must not repeat.
	other, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == sess.ID {
		t.Fatal("two sessions were issued the same id")
	}
}

func TestCreateSessionRejectsUnknownUser(t *testing.T) {
	s, _, _ := newSessionStore(t)
	// The foreign key must reject this; it is the pragma from Task 2 doing its job.
	if _, err := s.CreateSession(context.Background(), 99999); err == nil {
		t.Fatal("CreateSession succeeded for a nonexistent user")
	}
}

func TestUseSessionReturnsAValidSession(t *testing.T) {
	s, _, u := newSessionStore(t)
	ctx := context.Background()

	created, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.UseSession(ctx, created.ID)
	if err != nil {
		t.Fatalf("UseSession: %v", err)
	}
	if got.UserID != u.ID {
		t.Errorf("UserID = %d, want %d", got.UserID, u.ID)
	}
}

func TestUseSessionRejectsUnknownAndExpired(t *testing.T) {
	s, c, u := newSessionStore(t)
	ctx := context.Background()

	if _, err := s.UseSession(ctx, "never-issued"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown session err = %v, want ErrNotFound", err)
	}
	if _, err := s.UseSession(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty session id err = %v, want ErrNotFound", err)
	}

	created, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	c.add(SessionTTL + time.Second)
	if _, err := s.UseSession(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired session err = %v, want ErrNotFound", err)
	}
}

// TestUseSessionSlidesExpiryButNotOnEveryRequest covers both halves of the
// sliding-expiry contract: an active session must not expire, and an active
// session must not cause a database write on every single request.
func TestUseSessionSlidesExpiryButNotOnEveryRequest(t *testing.T) {
	s, c, u := newSessionStore(t)
	ctx := context.Background()

	created, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Used again immediately: too soon to be worth a write.
	got, err := s.UseSession(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpiresAt.Equal(created.ExpiresAt) {
		t.Errorf("expiry moved on an immediate second use: %v then %v", created.ExpiresAt, got.ExpiresAt)
	}

	// Used again after the renew interval: expiry slides forward.
	c.add(SessionRenewInterval + time.Minute)
	got, err = s.UseSession(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpiresAt.After(created.ExpiresAt) {
		t.Errorf("expiry did not slide: still %v after %v elapsed", got.ExpiresAt, SessionRenewInterval)
	}
	if want := c.t.Add(SessionTTL); !got.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, want)
	}

	// The slide must be persisted, not just returned.
	reread, err := s.UseSession(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reread.ExpiresAt.Equal(got.ExpiresAt) {
		t.Errorf("slid expiry was not persisted: %v then %v", got.ExpiresAt, reread.ExpiresAt)
	}
}

// TestSessionStaysAliveAcrossContinuedUse is the behaviour a user actually
// notices: someone using the suite daily is never logged out.
func TestSessionStaysAliveAcrossContinuedUse(t *testing.T) {
	s, c, u := newSessionStore(t)
	ctx := context.Background()

	created, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	for day := range 120 {
		c.add(25 * time.Hour)
		if _, err := s.UseSession(ctx, created.ID); err != nil {
			t.Fatalf("session died on day %d of continuous use: %v", day, err)
		}
	}
}

func TestDeleteSessionRevokesImmediately(t *testing.T) {
	s, _, u := newSessionStore(t)
	ctx := context.Background()

	created, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(ctx, created.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := s.UseSession(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted session still usable, err = %v", err)
	}
	// Deleting again is not an error; logout must be idempotent.
	if err := s.DeleteSession(ctx, created.ID); err != nil {
		t.Errorf("second DeleteSession: %v", err)
	}
}

func TestDeleteUserSessions(t *testing.T) {
	s, _, u := newSessionStore(t)
	ctx := context.Background()

	for range 3 {
		if _, err := s.CreateSession(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.DeleteUserSessions(ctx, u.ID)
	if err != nil {
		t.Fatalf("DeleteUserSessions: %v", err)
	}
	if n != 3 {
		t.Errorf("deleted %d sessions, want 3", n)
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	s, c, u := newSessionStore(t)
	ctx := context.Background()

	stale, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	c.add(SessionTTL + time.Hour)
	fresh, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	n, err := s.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Errorf("swept %d sessions, want 1", n)
	}
	if _, err := s.UseSession(ctx, fresh.ID); err != nil {
		t.Errorf("sweep removed a live session: %v", err)
	}
	if _, err := s.UseSession(ctx, stale.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("stale session survived the sweep")
	}
}
