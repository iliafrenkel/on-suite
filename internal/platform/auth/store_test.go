package auth

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// newStore gives each test its own migrated database on disk. Applying the
// real migration rather than hand-writing the schema means these tests also
// prove the migration is correct.
func newStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	ms, err := db.Collect(Namespace, Migrations())
	if err != nil {
		t.Fatalf("db.Collect: %v", err)
	}
	if len(ms) == 0 {
		t.Fatal("no platform migrations were collected")
	}
	if _, err := db.Apply(context.Background(), handle, ms); err != nil {
		t.Fatalf("db.Apply: %v", err)
	}
	return NewStore(handle), handle
}

func TestCreateAndFetchUser(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	created, err := s.CreateUser(ctx, "Ilia", "$argon2id$fake", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.ID == 0 {
		t.Error("CreateUser returned id 0")
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreateUser returned a zero CreatedAt")
	}
	if !created.IsAdmin {
		t.Error("IsAdmin did not survive the round trip")
	}

	byID, err := s.UserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if byID.Username != "Ilia" {
		t.Errorf("Username = %q, want %q — display case must be preserved", byID.Username, "Ilia")
	}
	if byID.PasswordHash != "$argon2id$fake" {
		t.Errorf("PasswordHash = %q, want it preserved verbatim", byID.PasswordHash)
	}
	if !byID.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", byID.CreatedAt, created.CreatedAt)
	}
}

// TestUserLookupIsCaseInsensitive is the behaviour that stops a family member
// failing to log in because they capitalised their own name.
func TestUserLookupIsCaseInsensitive(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if _, err := s.CreateUser(ctx, "Ilia", "$argon2id$fake", false); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []string{"Ilia", "ilia", "ILIA", "  iLiA  "} {
		if _, err := s.UserByUsername(ctx, attempt); err != nil {
			t.Errorf("UserByUsername(%q): %v", attempt, err)
		}
	}
}

func TestCreateUserRejectsDuplicateRegardlessOfCase(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if _, err := s.CreateUser(ctx, "ilia", "$argon2id$fake", false); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateUser(ctx, "ILIA", "$argon2id$fake", false)
	if !errors.Is(err, ErrDuplicateUsername) {
		t.Fatalf("err = %v, want ErrDuplicateUsername", err)
	}
}

func TestCreateUserRejectsInvalidInput(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	tests := []struct{ name, username string }{
		{"too short", "ab"},
		{"too long", strings.Repeat("a", 33)},
		{"leading dot", ".ilia"},
		{"trailing dash", "ilia-"},
		{"contains space", "il ia"},
		{"contains slash", "ilia/admin"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.CreateUser(ctx, tt.username, "$argon2id$fake", false); !errors.Is(err, ErrInvalidUsername) {
				t.Errorf("err = %v, want ErrInvalidUsername", err)
			}
		})
	}

	if _, err := s.CreateUser(ctx, "valid.name", "", false); err == nil {
		t.Error("CreateUser accepted an empty password hash")
	}
}

func TestMissingUserIsNotFound(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if _, err := s.UserByUsername(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByUsername err = %v, want ErrNotFound", err)
	}
	if _, err := s.UserByID(ctx, 12345); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByID err = %v, want ErrNotFound", err)
	}
}

func TestCountUsers(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	n, err := s.CountUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("CountUsers on a fresh database = %d, want 0", n)
	}
	if _, err := s.CreateUser(ctx, "ilia", "$argon2id$fake", true); err != nil {
		t.Fatal(err)
	}
	if n, err = s.CountUsers(ctx); err != nil || n != 1 {
		t.Errorf("CountUsers = %d (err %v), want 1", n, err)
	}
}

func TestListAccountsReturnsEveryUserOldestFirstWithSessionCounts(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()

	hash, err := HashPassword("a-sufficiently-long-password")
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.CreateUser(ctx, "root", hash, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUser(ctx, "ilia", hash, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSession(ctx, root.ID); err != nil {
		t.Fatal(err)
	}

	got, err := store.ListAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ListAccounts() returned %d accounts, want 2", len(got))
	}
	if got[0].Username != "root" || !got[0].IsAdmin {
		t.Errorf("ListAccounts()[0] = %+v, want root as an admin first", got[0])
	}
	if got[0].Sessions != 1 {
		t.Errorf("root has %d live sessions, want 1", got[0].Sessions)
	}
	if got[1].Username != "ilia" || got[1].IsAdmin {
		t.Errorf("ListAccounts()[1] = %+v", got[1])
	}
	if got[1].Sessions != 0 {
		t.Errorf("ilia has %d live sessions, want 0", got[1].Sessions)
	}
	if got[0].CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestSessionCountsSeparatesLiveFromExpired(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()

	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	store.SetClock(func() time.Time { return now })

	hash, err := HashPassword("a-sufficiently-long-password")
	if err != nil {
		t.Fatal(err)
	}
	u, err := store.CreateUser(ctx, "ilia", hash, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSession(ctx, u.ID); err != nil {
		t.Fatal(err)
	}

	// Move well past the 30-day session lifetime without sweeping.
	store.SetClock(func() time.Time { return now.AddDate(0, 0, 60) })

	live, expired, err := store.SessionCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if live != 0 || expired != 1 {
		t.Errorf("SessionCounts() = live %d, expired %d; want 0 and 1", live, expired)
	}
}

func TestSessionCountsOnAnEmptyTableIsZeroNotAnError(t *testing.T) {
	store, _ := newStore(t)
	live, expired, err := store.SessionCounts(context.Background())
	if err != nil {
		t.Fatalf("SessionCounts() on an empty table: %v", err)
	}
	if live != 0 || expired != 0 {
		t.Errorf("SessionCounts() = %d, %d, want 0, 0", live, expired)
	}
}
