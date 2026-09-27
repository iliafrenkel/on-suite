package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// stdinFrom writes s to a temp file and returns it opened for reading, so the
// non-terminal branch of readPassword can be exercised with a real *os.File.
func stdinFrom(t *testing.T, s string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestUserAddCreatesALoginableAccount(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer

	err := userAdd(
		[]string{"ilia", "--admin", "--data-dir", dir}, nil,
		stdinFrom(t, "a-sufficiently-long-password\n"),
		&out, io.Discard,
	)
	if err != nil {
		t.Fatalf("userAdd: %v", err)
	}
	if !strings.Contains(out.String(), "administrator") {
		t.Errorf("output %q does not mention the role", out.String())
	}

	// Reopen the database the way the server would and check the account works.
	handle, err := db.Open(filepath.Join(dir, "onsuite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = handle.Close() }()

	u, err := auth.NewStore(handle).UserByUsername(context.Background(), "ilia")
	if err != nil {
		t.Fatalf("UserByUsername: %v", err)
	}
	if !u.IsAdmin {
		t.Error("--admin did not take effect")
	}

	ok, err := auth.VerifyPassword(u.PasswordHash, "a-sufficiently-long-password")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Error("the stored hash does not verify against the password that was supplied")
	}
}

func TestUserAddRejectsBadInput(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		stdin string
	}{
		{"password too short", []string{"ilia"}, "short\n"},
		{"empty password", []string{"ilia"}, "\n"},
		{"invalid username", []string{"a"}, "a-sufficiently-long-password\n"},
		{"no username", nil, "a-sufficiently-long-password\n"},
		{"two usernames", []string{"ilia", "extra"}, "a-sufficiently-long-password\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"--data-dir", t.TempDir()}, tt.args...)
			err := userAdd(args, nil, stdinFrom(t, tt.stdin), io.Discard, io.Discard)
			if err == nil {
				t.Fatal("userAdd succeeded, want error")
			}
		})
	}
}

func TestUserAddRejectsDuplicate(t *testing.T) {
	dir := t.TempDir()
	args := []string{"--data-dir", dir, "ilia"}
	const pw = "a-sufficiently-long-password\n"

	if err := userAdd(args, nil, stdinFrom(t, pw), io.Discard, io.Discard); err != nil {
		t.Fatalf("first userAdd: %v", err)
	}
	if err := userAdd(args, nil, stdinFrom(t, pw), io.Discard, io.Discard); err == nil {
		t.Fatal("second userAdd with the same name succeeded, want error")
	}
}

// TestUserAddAcceptsFlagsAfterTheUsername covers the invocation form the spec
// documents in §7.1. Go's flag package stops at the first positional
// argument, so this fails unless flags and positionals are parsed
// interspersed.
func TestUserAddAcceptsFlagsAfterTheUsername(t *testing.T) {
	dir := t.TempDir()
	err := userAdd(
		[]string{"ilia", "--admin", "--data-dir", dir}, nil,
		stdinFrom(t, "a-sufficiently-long-password\n"), io.Discard, io.Discard,
	)
	if err != nil {
		t.Fatalf("userAdd with trailing flags: %v", err)
	}

	handle, err := db.Open(filepath.Join(dir, "onsuite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = handle.Close() }()

	u, err := auth.NewStore(handle).UserByUsername(context.Background(), "ilia")
	if err != nil {
		t.Fatalf("UserByUsername: %v", err)
	}
	if !u.IsAdmin {
		t.Error("--admin after the username was ignored")
	}
}

// TestUserAddDoesNotAcceptAPasswordFlag guards the design decision in spec
// §7.1: a password passed as a flag would be visible in ps and in shell
// history.
func TestUserAddDoesNotAcceptAPasswordFlag(t *testing.T) {
	err := userAdd(
		[]string{"--data-dir", t.TempDir(), "--password", "a-sufficiently-long-password", "ilia"},
		nil, stdinFrom(t, "a-sufficiently-long-password\n"), io.Discard, io.Discard,
	)
	if err == nil {
		t.Fatal("--password was accepted; it must not exist")
	}
}

func TestUserResetPasswordReplacesThePasswordAndSignsOut(t *testing.T) {
	dir := t.TempDir()
	const oldPW, newPW = "a-sufficiently-long-password", "an-entirely-different-one"
	if err := userAdd([]string{"ilia", "--data-dir", dir}, nil, stdinFrom(t, oldPW+"\n"), io.Discard, io.Discard); err != nil {
		t.Fatalf("userAdd: %v", err)
	}

	// A session opened before the reset must not survive it.
	handle, err := db.Open(filepath.Join(dir, "onsuite.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store := auth.NewStore(handle)
	u, err := store.UserByUsername(ctx, "ilia")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = handle.Close()

	var out bytes.Buffer
	if err := userResetPassword([]string{"ilia", "--data-dir", dir}, nil, stdinFrom(t, newPW+"\n"), &out, io.Discard); err != nil {
		t.Fatalf("userResetPassword: %v", err)
	}
	if !strings.Contains(out.String(), "signed out") {
		t.Errorf("output %q does not say sessions were signed out", out.String())
	}

	handle, err = db.Open(filepath.Join(dir, "onsuite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = handle.Close() }()
	store = auth.NewStore(handle)
	u, err = store.UserByUsername(ctx, "ilia")
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := auth.VerifyPassword(u.PasswordHash, newPW); !ok {
		t.Error("the new password does not verify")
	}
	if ok, _ := auth.VerifyPassword(u.PasswordHash, oldPW); ok {
		t.Error("the old password still verifies")
	}
	if _, err := store.UseSession(ctx, sess.ID); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("pre-reset session: err = %v, want ErrNotFound", err)
	}
}

func TestUserResetPasswordRejectsBadInput(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		stdin string
	}{
		{"unknown user", []string{"nobody"}, "a-sufficiently-long-password\n"},
		{"password too short", []string{"ilia"}, "short\n"},
		{"no username", nil, "a-sufficiently-long-password\n"},
		{"password flag", []string{"--password", "a-sufficiently-long-password", "ilia"}, "a-sufficiently-long-password\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := userAdd([]string{"ilia", "--data-dir", dir}, nil, stdinFrom(t, "a-sufficiently-long-password\n"), io.Discard, io.Discard); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"--data-dir", dir}, tt.args...)
			if err := userResetPassword(args, nil, stdinFrom(t, tt.stdin), io.Discard, io.Discard); err == nil {
				t.Fatal("userResetPassword succeeded, want error")
			}
		})
	}
}
