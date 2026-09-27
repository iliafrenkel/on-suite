# User Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let admins add, delete, promote/demote and reset the password of other users at `/admin/users`, let every user change their own password at `/account`, and add an `onsuite user reset-password` CLI recovery command (#310).

**Architecture:** Three new `auth.Store` methods (`SetPassword`, `SetAdmin`, `DeleteUser`) and `auth.GeneratePassword` carry all the logic and SQL. A new platform package `internal/platform/usermgmt` holds plain-form HTTP handlers for both pages and exposes one `Routes` function. `buildStack` and the tests both call it, so route patterns have a single source of truth. The read-only `/admin/` page is untouched apart from a link.

**Tech Stack:** Go stdlib `net/http` ServeMux patterns, `html/template`, SQLite through `database/sql`, Argon2id via the existing `auth` helpers. No new dependencies, and no htmx on these pages.

**Spec:** [docs/superpowers/specs/2026-09-27-user-management-design.md](../specs/2026-09-27-user-management-design.md)

## Global Constraints

- No migration: the schema is unchanged.
- No new Go module dependencies (the platform deps list is capped).
- No inline `<script>` and no `style=` attributes (strict CSP). All JS goes in `internal/ui/static/theme.js`, and all CSS goes in `internal/ui/static/app.css`.
- Every `/admin/users…` route answers a signed-in non-admin with `errs.NotFound`, which is byte-identical to an unrouted path.
- Passwords are never accepted as a CLI flag, never put in a URL, and never logged.
- Generated password format: 4 dash-separated groups of 5 characters from `abcdefghjkmnpqrstuvwxyz23456789`.
- Plain form POSTs rejected by validation re-render the page with status **422** and a `<div class="notice notice-error" role="alert">`.
- Commit subjects follow Conventional Commits, scope `platform` (CLI changes: scope `cli`), and end with `(#310)`.
- Full check before the final commit of each task:
  ```bash
  gofmt -l . && go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./... && go test ./... -race -count=1
  ```
  `gofmt -l .` must print nothing.
- Work stays on branch `feat/310-user-management`. Never push to `main`.

## File Structure

| File | Responsibility |
|---|---|
| `internal/platform/auth/manage.go` (new) | `ErrLastAdmin`, `SetPassword`, `SetAdmin`, `DeleteUser`, and a private `inTx` helper |
| `internal/platform/auth/manage_test.go` (new) | Store tests for the above |
| `internal/platform/auth/password.go` (modify) | `GeneratePassword` |
| `internal/platform/auth/password_test.go` (modify) | `GeneratePassword` test |
| `cmd/onsuite/user.go` (modify) | `reset-password` subcommand |
| `cmd/onsuite/user_test.go` (modify) | CLI tests |
| `cmd/onsuite/main.go` (modify) | usage line |
| `internal/platform/usermgmt/usermgmt.go` (new) | package doc, `Deps`, `Routes`, shared helpers (`page`, `audit`) |
| `internal/platform/usermgmt/admin.go` (new) | `/admin/users` handlers and view models |
| `internal/platform/usermgmt/account.go` (new) | `/account` handlers and view model |
| `internal/platform/usermgmt/fixture_test.go` (new) | test server and helpers |
| `internal/platform/usermgmt/admin_test.go` (new) | admin page tests |
| `internal/platform/usermgmt/account_test.go` (new) | account page tests |
| `internal/ui/templates/admin_users.html` (new) | users page |
| `internal/ui/templates/admin_user_delete.html` (new) | delete confirmation |
| `internal/ui/templates/account.html` (new) | account page |
| `internal/ui/templates/base.html` (modify) | username becomes a link to `/account` |
| `internal/ui/templates/admin.html` (modify) | "Manage users →" link |
| `internal/ui/static/theme.js` (modify) | `data-copy-text` copy button |
| `internal/ui/static/app.css` (modify) | user-management styles |
| `cmd/onsuite/stack.go` (modify) | mount `usermgmt.Routes` |
| `internal/arch/arch_test.go` (modify) | scan-sees list includes `usermgmt` |
| docs: admin spec, `AGENTS.md`, `docs/DEPLOYING.md`, `PATTERNS.md` (modify) | documentation |

---

### Task 1: Store methods for password, role and deletion

**Files:**
- Create: `internal/platform/auth/manage.go`
- Test: `internal/platform/auth/manage_test.go`

**Interfaces:**
- Consumes: `Store`, `ErrNotFound`, `boolToInt`, `newStore(t)` (test helper in `store_test.go`, returns `(*Store, *sql.DB)`), `CreateSession`, `UseSession`, `UserByID`.
- Produces:
  - `var ErrLastAdmin error`
  - `func (s *Store) SetPassword(ctx context.Context, userID int64, passwordHash, keepSessionID string) error`
  - `func (s *Store) SetAdmin(ctx context.Context, userID int64, isAdmin bool) error`
  - `func (s *Store) DeleteUser(ctx context.Context, userID int64) error`

  All three return `ErrNotFound` for a missing user.

- [ ] **Step 1: Write the failing tests**

Create `internal/platform/auth/manage_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/platform/auth/ -run 'SetPassword|SetAdmin|DeleteUser|ManageMethods' -count=1`
Expected: build failure, `s.SetPassword undefined` (and the same for the other methods and `ErrLastAdmin`).

- [ ] **Step 3: Implement**

Create `internal/platform/auth/manage.go`:

```go
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/platform/auth/ -count=1 -race`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/auth/manage.go internal/platform/auth/manage_test.go
git commit -m "feat(platform): store methods to reset passwords, change roles and delete users (#310)"
```

---

### Task 2: `GeneratePassword`

**Files:**
- Modify: `internal/platform/auth/password.go` (imports and a new function after `ValidatePassword`)
- Test: `internal/platform/auth/password_test.go` (append)

**Interfaces:**
- Produces: `func GeneratePassword() (string, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/platform/auth/password_test.go` (add `"regexp"` to its imports if it is not already there):

```go
var generatedShape = regexp.MustCompile(
	`^[abcdefghjkmnpqrstuvwxyz23456789]{5}(-[abcdefghjkmnpqrstuvwxyz23456789]{5}){3}$`)

func TestGeneratePasswordHasTheDocumentedShapeAndPassesPolicy(t *testing.T) {
	a, err := GeneratePassword()
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}
	if !generatedShape.MatchString(a) {
		t.Errorf("%q does not match xxxxx-xxxxx-xxxxx-xxxxx over the look-alike-free alphabet", a)
	}
	if err := ValidatePassword(a); err != nil {
		t.Errorf("a generated password fails policy: %v", err)
	}

	b, err := GeneratePassword()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two generated passwords are identical")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/platform/auth/ -run TestGeneratePassword -count=1`
Expected: build failure, `undefined: GeneratePassword`.

- [ ] **Step 3: Implement**

In `internal/platform/auth/password.go`, add `"math/big"` to the imports (`crypto/rand` and `strings` are already imported). Add this after `ValidatePassword`:

```go
// generatedAlphabet leaves out 0/o and 1/l/i, so a password read aloud or
// copied off a screen cannot be mistyped (#310).
const generatedAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// GeneratePassword returns a random password for an account an admin has just
// created or reset: four groups of five from generatedAlphabet, such as
// "k7mqa-x3vnd-pr4tz-hw9cb". That is about 99 bits, and 23 runes clears
// MinPasswordLength.
func GeneratePassword() (string, error) {
	const groups, groupLen = 4, 5
	max := big.NewInt(int64(len(generatedAlphabet)))
	var b strings.Builder
	for g := 0; g < groups; g++ {
		if g > 0 {
			b.WriteByte('-')
		}
		for i := 0; i < groupLen; i++ {
			n, err := rand.Int(rand.Reader, max)
			if err != nil {
				return "", fmt.Errorf("auth: generate password: %w", err)
			}
			b.WriteByte(generatedAlphabet[n.Int64()])
		}
	}
	return b.String(), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/platform/auth/ -count=1`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/auth/password.go internal/platform/auth/password_test.go
git commit -m "feat(platform): generate look-alike-free one-time passwords (#310)"
```

---

### Task 3: `onsuite user reset-password`

**Files:**
- Modify: `cmd/onsuite/user.go` (`userCmd`, and a new `userResetPassword`)
- Modify: `cmd/onsuite/main.go` (the `usage` text)
- Test: `cmd/onsuite/user_test.go` (append)

**Interfaces:**
- Consumes: `auth.Store.SetPassword` (Task 1), `readPassword`, `parseInterspersed`, `envOrDefault`, `openDatabase`, `stdinFrom` (test helper).
- Produces: `func userResetPassword(args []string, getenv func(string) string, in *os.File, out, errOut io.Writer) error`

- [ ] **Step 1: Write the failing tests**

Append to `cmd/onsuite/user_test.go` (add `"errors"` to the imports):

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/onsuite/ -run TestUserResetPassword -count=1`
Expected: build failure, `undefined: userResetPassword`.

- [ ] **Step 3: Implement**

In `cmd/onsuite/user.go`, replace `userCmd` with:

```go
func userCmd(args []string, getenv func(string) string, errOut io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(errOut, "usage: onsuite user add <username> [--admin] [--data-dir DIR]\n"+
			"       onsuite user reset-password <username> [--data-dir DIR]\n")
		return errors.New("user: no subcommand given")
	}
	switch args[0] {
	case "add":
		return userAdd(args[1:], getenv, os.Stdin, os.Stdout, errOut)
	case "reset-password":
		return userResetPassword(args[1:], getenv, os.Stdin, os.Stdout, errOut)
	default:
		return fmt.Errorf("user: unknown subcommand %q", args[0])
	}
}
```

Add this after `userAdd`:

```go
// userResetPassword is the recovery path for an account that cannot sign in,
// including the only admin, whom /admin/users cannot help (#310). The new
// password comes from readPassword, never a flag, exactly as for user add.
func userResetPassword(args []string, getenv func(string) string, in *os.File, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("user reset-password", flag.ContinueOnError)
	fs.SetOutput(errOut)
	dataDir := fs.String("data-dir", envOrDefault(getenv, "ONSUITE_DATA_DIR", "./data"),
		"directory holding the database")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return errors.New("user reset-password: exactly one username is required")
	}
	username := positional[0]

	password, err := readPassword(in, out)
	if err != nil {
		return err
	}
	if err := auth.ValidatePassword(password); err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	cfg := config.Config{DataDir: *dataDir}
	ctx := context.Background()
	handle, _, _, err := openDatabase(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()

	store := auth.NewStore(handle)
	user, err := store.UserByUsername(ctx, username)
	if errors.Is(err, auth.ErrNotFound) {
		return fmt.Errorf("user reset-password: no account named %q", username)
	}
	if err != nil {
		return err
	}
	if err := store.SetPassword(ctx, user.ID, hash, ""); err != nil {
		return err
	}
	fmt.Fprintf(out, "Password for %q reset; all their sessions were signed out.\n", user.Username)
	return nil
}
```

In `cmd/onsuite/main.go` `usage`, replace the line

```
  onsuite user add <name>   create an account
```

with

```
  onsuite user add <name>   create an account
  onsuite user reset-password <name>
                            set a new password and sign the account out
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/onsuite/ -count=1`
Expected: `ok`. If a `main_test.go` test pins the usage text, update its expectation to the new text.

- [ ] **Step 5: Commit**

```bash
git add cmd/onsuite/user.go cmd/onsuite/user_test.go cmd/onsuite/main.go
git commit -m "feat(cli): onsuite user reset-password (#310)"
```

---

### Task 4: `usermgmt` package: users page and adding users

**Files:**
- Create: `internal/platform/usermgmt/usermgmt.go`
- Create: `internal/platform/usermgmt/admin.go`
- Create: `internal/platform/usermgmt/account.go` (stub handlers only; Task 6 fills them in)
- Create: `internal/ui/templates/admin_users.html`
- Create: `internal/ui/templates/account.html` (minimal; Task 6 replaces it)
- Create: `internal/platform/usermgmt/fixture_test.go`
- Create: `internal/platform/usermgmt/admin_test.go`
- Modify: `internal/ui/static/theme.js` (add `initCopyText`)
- Modify: `internal/ui/static/app.css` (user-management block)
- Modify: `cmd/onsuite/stack.go` (mount)
- Modify: `internal/arch/arch_test.go` (`TestScanSeesTheRealTree` list)

**Interfaces:**
- Consumes: `auth.GeneratePassword`, `auth.HashPassword`, `auth.ValidateUsername`, `auth.ErrDuplicateUsername`, `auth.Store.CreateUser`, `auth.Store.ListAccounts`, `app.NewPage`, `web.WithActiveApp`, `web.UserFrom`, `(*web.Auth).RequireAdmin/RequireUser`, `(*web.Recorder).Handle` (nil-safe).
- Produces (used by Tasks 5–6):
  ```go
  type Deps struct {
      Users   *auth.Store
      Render  *render.Renderer
      Errors  *web.Errors
      Log     *slog.Logger
      Nav     []render.NavItem
      Version string
  }
  func Routes(mux *http.ServeMux, rec *web.Recorder, authn *web.Auth, d Deps)
  type handlers struct{ d Deps }
  func (h *handlers) page(r *http.Request, title, activeApp string) render.Page
  func (h *handlers) audit(r *http.Request, action, target string)
  func (h *handlers) renderUsers(w http.ResponseWriter, r *http.Request, status int, data usersPage)
  type usersPage struct { Accounts []accountRow; Error string; Generated *generatedPassword; Username string; Admin bool }
  type generatedPassword struct { Username, Password string }
  ```
  Test fixture: `newServer(t) *server`. Fields: `handler`, `users *auth.Store`, `db *sql.DB`, `logs *bytes.Buffer`, `root`/`plain *session`. `session` has fields `user auth.User` and `cookies []*http.Cookie`. Methods: `s.logIn(t, username, password) *session`, `s.tryLogIn(t, username, password) *httptest.ResponseRecorder`, `s.get(t, sess, path) *httptest.ResponseRecorder`, `s.post(t, sess, path, form url.Values) *httptest.ResponseRecorder`, `s.doc(t, rec) *htmlassert.Doc`.

- [ ] **Step 1: Write the test fixture**

Create `internal/platform/usermgmt/fixture_test.go`:

```go
package usermgmt_test

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/usermgmt"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

// session is one signed-in browser.
type session struct {
	user    auth.User
	cookies []*http.Cookie
}

// server is the real middleware stack over a real database file, with the
// user-management routes mounted exactly as buildStack mounts them, plus
// one admin ("root") and one ordinary user ("ilia").
type server struct {
	handler http.Handler
	users   *auth.Store
	db      *sql.DB
	logs    *bytes.Buffer
	root    *session
	plain   *session
}

func newServer(t *testing.T) *server {
	t.Helper()
	ctx := context.Background()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	ms, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatal(err)
	}
	users := auth.NewStore(handle)

	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatal(err)
	}
	rend, err := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL, CSRFFieldName: web.CSRFFormField})
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logs, nil))
	errs := web.NewErrors(rend, log)
	csrf := web.NewCSRF(false, errs)
	authn := web.NewAuth(web.AuthOptions{Users: users, Render: rend, Errors: errs, CSRF: csrf, Log: log, Version: "test"})

	mux := http.NewServeMux()
	authn.Routes(mux, nil)
	usermgmt.Routes(mux, nil, authn, usermgmt.Deps{
		Users: users, Render: rend, Errors: errs, Log: log, Version: "test",
	})
	mux.Handle("/", http.HandlerFunc(errs.NotFound))

	s := &server{handler: web.Stack(mux, log, errs, csrf, authn), users: users, db: handle, logs: logs}

	if _, err := users.CreateUser(ctx, "root", apptest.PasswordHash, true); err != nil {
		t.Fatal(err)
	}
	if _, err := users.CreateUser(ctx, "ilia", apptest.PasswordHash, false); err != nil {
		t.Fatal(err)
	}
	s.root = s.logIn(t, "root", apptest.Password)
	s.plain = s.logIn(t, "ilia", apptest.Password)
	return s
}

func (s *server) do(t *testing.T, sess *session, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	if sess != nil {
		for _, c := range sess.cookies {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

// anonymous returns a browser holding only a CSRF cookie.
func (s *server) anonymous(t *testing.T) *session {
	t.Helper()
	page := s.do(t, nil, httptest.NewRequest("GET", "/login", nil))
	for _, c := range page.Result().Cookies() {
		if c.Name == web.CSRFCookieName {
			return &session{cookies: []*http.Cookie{c}}
		}
	}
	t.Fatal("GET /login issued no CSRF cookie")
	return nil
}

// tryLogIn submits the login form and returns the raw response.
func (s *server) tryLogIn(t *testing.T, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	anon := s.anonymous(t)
	form := url.Values{"username": {username}, "password": {password}}
	return s.post(t, anon, "/login", form)
}

// logIn signs in and fails the test unless it worked.
func (s *server) logIn(t *testing.T, username, password string) *session {
	t.Helper()
	rec := s.tryLogIn(t, username, password)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login for %s = %d; body: %s", username, rec.Code, rec.Body.String())
	}
	u, err := s.users.UserByUsername(context.Background(), username)
	if err != nil {
		t.Fatal(err)
	}
	return &session{user: u, cookies: rec.Result().Cookies()}
}

func (s *server) get(t *testing.T, sess *session, path string) *httptest.ResponseRecorder {
	t.Helper()
	return s.do(t, sess, httptest.NewRequest("GET", path, nil))
}

// post submits a form carrying the session's own CSRF token.
func (s *server) post(t *testing.T, sess *session, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	for _, c := range sess.cookies {
		if c.Name == web.CSRFCookieName {
			form.Set(web.CSRFFormField, c.Value)
		}
	}
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.do(t, sess, req)
}

func (s *server) doc(t *testing.T, rec *httptest.ResponseRecorder) *htmlassert.Doc {
	t.Helper()
	return htmlassert.Parse(t, rec.Body.String())
}

func (s *server) user(t *testing.T, name string) auth.User {
	t.Helper()
	u, err := s.users.UserByUsername(context.Background(), name)
	if err != nil {
		t.Fatalf("UserByUsername(%s): %v", name, err)
	}
	return u
}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/platform/usermgmt/admin_test.go`:

```go
package usermgmt_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
)

func TestAnonymousIsSentToLoginFromTheUsersPage(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, nil, "/admin/users")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
		t.Fatalf("status = %d, Location = %q; want 303 to /login", rec.Code, rec.Header().Get("Location"))
	}
}

func TestANonAdminGetsTheSame404AsAMissingPage(t *testing.T) {
	s := newServer(t)
	missing := s.get(t, s.plain, "/no-such-page")
	got := s.get(t, s.plain, "/admin/users")
	if got.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", got.Code)
	}
	if got.Body.String() != missing.Body.String() {
		t.Error("/admin/users renders differently from a missing page for a non-admin")
	}

	missingPost := s.post(t, s.plain, "/no-such-page", url.Values{})
	if rec := s.post(t, s.plain, "/admin/users", url.Values{"username": {"mallory"}}); rec.Code != http.StatusNotFound || rec.Body.String() != missingPost.Body.String() {
		t.Errorf("POST /admin/users as non-admin = %d, want the missing-page 404", rec.Code)
	}
	if _, err := s.users.UserByUsername(t.Context(), "mallory"); !errors.Is(err, auth.ErrNotFound) {
		t.Error("a non-admin created an account")
	}
}

func TestTheUsersPageGivesOthersAMenuAndYouALinkToAccount(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, s.root, "/admin/users")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := s.doc(t, rec)
	doc.MustHave(`tr[data-user="ilia"] details`)
	doc.MustNotHave(`tr[data-user="root"] details`)
	doc.MustHave(`tr[data-user="root"] a[href="/account"]`)
}

// generated pulls the one-time password out of a response.
func generated(t *testing.T, doc *htmlassert.Doc) string {
	t.Helper()
	return strings.TrimSpace(htmlassert.Text(doc.MustHave(`[data-generated-password]`)))
}

func TestAddingAUserShowsAWorkingPasswordExactlyOnce(t *testing.T) {
	s := newServer(t)
	rec := s.post(t, s.root, "/admin/users", url.Values{"username": {"alice"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	password := generated(t, s.doc(t, rec))

	if s.user(t, "alice").IsAdmin {
		t.Error("alice was made an admin without asking")
	}
	if login := s.tryLogIn(t, "alice", password); login.Code != http.StatusSeeOther {
		t.Errorf("signing in with the generated password = %d, want 303", login.Code)
	}
	if again := s.get(t, s.root, "/admin/users"); strings.Contains(again.Body.String(), password) {
		t.Error("the password is shown a second time")
	}
}

func TestAddingAnAdministrator(t *testing.T) {
	s := newServer(t)
	if rec := s.post(t, s.root, "/admin/users", url.Values{"username": {"alice"}, "admin": {"1"}}); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !s.user(t, "alice").IsAdmin {
		t.Error("the Administrator box was ignored")
	}
}

func TestAddingABadUsernameShowsANoticeAndKeepsTheForm(t *testing.T) {
	for _, name := range []string{"a", "ILIA", "has space"} {
		t.Run(name, func(t *testing.T) {
			s := newServer(t)
			rec := s.post(t, s.root, "/admin/users", url.Values{"username": {name}, "admin": {"1"}})
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			doc := s.doc(t, rec)
			doc.MustHave(".notice-error")
			doc.MustNotHave("[data-generated-password]")
			input := doc.MustHave(`input[name="username"]`)
			if v, _ := htmlassert.Attr(input, "value"); v != name {
				t.Errorf("username field = %q, want %q kept", v, name)
			}
			doc.MustHave(`input[checked]`) // the Administrator box, the only checkbox
		})
	}
}

// The session's CSRF cookie is sent but the form carries no token: exactly
// the shape of a cross-site forged submission.
func TestAddWithoutACSRFTokenIsRejected(t *testing.T) {
	s := newServer(t)
	req := httptest.NewRequest("POST", "/admin/users", strings.NewReader("username=alice"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rec := s.do(t, s.root, req); rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if _, err := s.users.UserByUsername(t.Context(), "alice"); !errors.Is(err, auth.ErrNotFound) {
		t.Error("an account was created without a CSRF token")
	}
}

func TestAccountChangesAreLoggedWithoutPasswords(t *testing.T) {
	s := newServer(t)
	rec := s.post(t, s.root, "/admin/users", url.Values{"username": {"alice"}})
	password := generated(t, s.doc(t, rec))

	logs := s.logs.String()
	for _, want := range []string{"action=user.add", "actor=root", "target=alice"} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %q", want)
		}
	}
	if strings.Contains(logs, password) {
		t.Error("the generated password was logged")
	}
}
```

`ILIA` fails as a *duplicate* (usernames fold case), while `a` and `has space` fail validation. Both paths must produce the 422 notice.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/platform/usermgmt/ -count=1`
Expected: build failure, `no non-test Go files` or `undefined: usermgmt.Routes`.

- [ ] **Step 4: Write the package**

Create `internal/platform/usermgmt/usermgmt.go`:

```go
// Package usermgmt lets people manage accounts from the browser: admins add,
// delete, promote, demote and reset other users at /admin/users, and
// everyone changes their own password at /account (#310, spec
// docs/superpowers/specs/2026-09-27-user-management-design.md).
//
// It is a sibling of package admin, not part of it: /admin/ is promised to be
// read-only, and every handler here changes something. Like admin, it sits at
// the top of the platform and nothing imports it but cmd/onsuite.
package usermgmt

import (
	"log/slog"
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// Deps is everything the handlers use, assembled once by buildStack.
type Deps struct {
	Users   *auth.Store
	Render  *render.Renderer
	Errors  *web.Errors
	Log     *slog.Logger
	Nav     []render.NavItem
	Version string
}

// Routes registers every route this package serves. buildStack and the tests
// both call it, so the patterns (and their guards) exist in one place.
//
// Each pattern is exact. None ends in "/", so ServeMux never synthesizes an
// unguarded redirect that would let a non-admin tell these paths apart
// from a genuine 404 (see the /admin registration in buildStack).
func Routes(mux *http.ServeMux, rec *web.Recorder, authn *web.Auth, d Deps) {
	h := &handlers{d: d}
	admin := func(f http.HandlerFunc) http.Handler { return authn.RequireAdmin(f) }
	user := func(f http.HandlerFunc) http.Handler { return authn.RequireUser(f) }

	rec.Handle(mux, "GET /admin/users", false, admin(h.list))
	rec.Handle(mux, "POST /admin/users", false, admin(h.add))
	rec.Handle(mux, "POST /admin/users/{id}/password", false, admin(h.resetPassword))
	rec.Handle(mux, "POST /admin/users/{id}/role", false, admin(h.setRole))
	rec.Handle(mux, "GET /admin/users/{id}/delete", false, admin(h.confirmDelete))
	rec.Handle(mux, "POST /admin/users/{id}/delete", false, admin(h.delete))
	rec.Handle(mux, "GET /account", false, user(h.account))
	rec.Handle(mux, "POST /account/password", false, user(h.changePassword))
}

type handlers struct{ d Deps }

// page builds the shell. activeApp is "admin" for the admin pages, so the
// sidebar's Admin entry is marked current, and "" for /account.
func (h *handlers) page(r *http.Request, title, activeApp string) render.Page {
	r = r.WithContext(web.WithActiveApp(r.Context(), activeApp))
	p := app.NewPage(r, title, h.d.Nav)
	p.Shell.Version = h.d.Version
	return p
}

// audit records who changed which account. It never takes a password: the
// signature has nowhere to put one (spec §4.5).
func (h *handlers) audit(r *http.Request, action, target string) {
	me, _ := web.UserFrom(r.Context())
	h.d.Log.Info("account change", "action", action, "actor", me.Username, "target", target)
}
```

Create `internal/platform/usermgmt/admin.go`:

```go
package usermgmt

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// usersPage is the view model for admin_users.html.
type usersPage struct {
	Accounts []accountRow
	Error    string
	// Generated is set only on the response to an add or a reset: the one
	// and only time that password is shown.
	Generated *generatedPassword
	// Username and Admin refill the add form after a rejection.
	Username string
	Admin    bool
}

// accountRow projects auth.Account for the table, plus whether it is the
// viewer's own row, which gets no action menu (spec §4.4).
type accountRow struct {
	ID       int64
	Username string
	IsAdmin  bool
	Created  string
	Sessions int
	Self     bool
}

type generatedPassword struct {
	Username string
	Password string
}

func (h *handlers) list(w http.ResponseWriter, r *http.Request) {
	h.renderUsers(w, r, http.StatusOK, usersPage{})
}

func (h *handlers) renderUsers(w http.ResponseWriter, r *http.Request, status int, data usersPage) {
	me, _ := web.UserFrom(r.Context())
	accounts, err := h.d.Users.ListAccounts(r.Context())
	if err != nil {
		h.d.Errors.Internal(w, r, err)
		return
	}
	for _, a := range accounts {
		data.Accounts = append(data.Accounts, accountRow{
			ID:       a.ID,
			Username: a.Username,
			IsAdmin:  a.IsAdmin,
			Created:  a.CreatedAt.Format("2006-01-02"),
			Sessions: a.Sessions,
			Self:     a.ID == me.ID,
		})
	}
	if data.Generated != nil {
		// The password exists in this body and nowhere else; no cache may
		// keep a copy (spec §4.2).
		w.Header().Set("Cache-Control", "no-store")
	}
	page := h.page(r, "Users", "admin")
	page.Data = data
	if err := h.d.Render.Page(w, status, "admin_users", page); err != nil {
		h.d.Errors.Internal(w, r, err)
	}
}

func (h *handlers) add(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.PostFormValue("username"))
	isAdmin := r.PostFormValue("admin") == "1"
	reject := func(msg string) {
		h.renderUsers(w, r, http.StatusUnprocessableEntity, usersPage{Error: msg, Username: username, Admin: isAdmin})
	}

	if err := auth.ValidateUsername(username); err != nil {
		reject("Usernames are 3–32 letters, digits, dots, dashes or underscores, starting and ending with a letter or digit.")
		return
	}
	password, hash, ok := h.newPassword(w, r)
	if !ok {
		return
	}
	u, err := h.d.Users.CreateUser(r.Context(), username, hash, isAdmin)
	if errors.Is(err, auth.ErrDuplicateUsername) {
		reject(fmt.Sprintf("The username %q is already taken.", username))
		return
	}
	if err != nil {
		h.d.Errors.Internal(w, r, err)
		return
	}
	h.audit(r, "user.add", u.Username)
	h.renderUsers(w, r, http.StatusOK, usersPage{Generated: &generatedPassword{Username: u.Username, Password: password}})
}

// newPassword generates and hashes a one-time password. On failure it has
// already written the response.
func (h *handlers) newPassword(w http.ResponseWriter, r *http.Request) (password, hash string, ok bool) {
	password, err := auth.GeneratePassword()
	if err != nil {
		h.d.Errors.Internal(w, r, err)
		return "", "", false
	}
	hash, err = auth.HashPassword(password)
	if err != nil {
		h.d.Errors.Internal(w, r, err)
		return "", "", false
	}
	return password, hash, true
}

// Filled in by Task 5.
func (h *handlers) resetPassword(w http.ResponseWriter, r *http.Request) { h.d.Errors.NotFound(w, r) }
func (h *handlers) setRole(w http.ResponseWriter, r *http.Request)       { h.d.Errors.NotFound(w, r) }
func (h *handlers) confirmDelete(w http.ResponseWriter, r *http.Request) { h.d.Errors.NotFound(w, r) }
func (h *handlers) delete(w http.ResponseWriter, r *http.Request)        { h.d.Errors.NotFound(w, r) }
```

Create `internal/platform/usermgmt/account.go`:

```go
package usermgmt

import "net/http"

// Filled in by Task 6.
func (h *handlers) account(w http.ResponseWriter, r *http.Request)        { h.d.Errors.NotFound(w, r) }
func (h *handlers) changePassword(w http.ResponseWriter, r *http.Request) { h.d.Errors.NotFound(w, r) }
```

- [ ] **Step 5: Write the template**

Create `internal/ui/templates/admin_users.html`:

```html
{{define "content"}}
<div class="stack">
	<p class="faint"><a href="/admin/">Admin</a> / Users</p>
	<h1>Users</h1>

	{{with .Data.Error}}<div class="notice notice-error" role="alert">{{.}}</div>{{end}}

	{{with .Data.Generated}}
	<div class="notice usermgmt-password" role="status">
		<p>
			Password for <strong>{{.Username}}</strong>:
			<code data-generated-password>{{.Password}}</code>
			<button type="button" class="quiet" data-copy-text="{{.Password}}">Copy</button>
		</p>
		<p class="faint">This is the only time it is shown. Pass it on, and ask them to change it under Account.</p>
	</div>
	{{end}}

	<section class="admin-section" id="add">
		<h2>Add a user</h2>
		<form method="post" action="/admin/users" class="usermgmt-add">
			<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
			<div class="field">
				<label for="new-username">Username</label>
				<input id="new-username" name="username" type="text"
				       autocomplete="off" autocapitalize="none" spellcheck="false"
				       required value="{{.Data.Username}}">
			</div>
			<div class="field">
				<label class="row"><input type="checkbox" name="admin" value="1"{{if .Data.Admin}} checked{{end}}> Administrator</label>
			</div>
			<button type="submit" class="primary">Add user</button>
		</form>
		<p class="faint">A password is generated for them and shown to you once.</p>
	</section>

	<section class="admin-section" id="accounts">
		<h2>Accounts</h2>
		<div class="scroll-x">
			<table class="admin-table usermgmt-table">
				<thead><tr><th>User</th><th>Role</th><th>Created</th><th>Live sessions</th><th><span class="visually-hidden">Actions</span></th></tr></thead>
				<tbody>
				{{range .Data.Accounts}}
					<tr data-user="{{.Username}}">
						<td>{{.Username}}{{if .Self}} <span class="dim">(you)</span>{{end}}</td>
						<td>{{if .IsAdmin}}Administrator{{else}}<span class="dim">User</span>{{end}}</td>
						<td class="dim">{{.Created}}</td>
						<td>{{.Sessions}}</td>
						<td>
						{{if .Self}}
							<a href="/account">Account</a>
						{{else}}
							<details class="outline-menu usermgmt-menu">
								<summary class="outline-menu-toggle quiet" aria-label="Actions for {{.Username}}">{{ticon "more"}}</summary>
								<div class="outline-menu-list reader-row-menu-list">
									<form method="post" action="/admin/users/{{.ID}}/password">
										<input type="hidden" name="{{csrfField}}" value="{{$.Shell.CSRFToken}}">
										<button type="submit">Reset password</button>
									</form>
									<form method="post" action="/admin/users/{{.ID}}/role">
										<input type="hidden" name="{{csrfField}}" value="{{$.Shell.CSRFToken}}">
										{{if .IsAdmin}}
										<input type="hidden" name="admin" value="0">
										<button type="submit">Remove admin</button>
										{{else}}
										<input type="hidden" name="admin" value="1">
										<button type="submit">Make admin</button>
										{{end}}
									</form>
									<a class="usermgmt-menu-link" href="/admin/users/{{.ID}}/delete">Delete…</a>
								</div>
							</details>
						{{end}}
						</td>
					</tr>
				{{end}}
				</tbody>
			</table>
		</div>
	</section>
</div>
{{end}}
```

Create a placeholder `internal/ui/templates/account.html` so the renderer has the page. Task 6 replaces it:

```html
{{define "content"}}
<div class="centered stack"><h1>Account</h1></div>
{{end}}
```

- [ ] **Step 6: Add the copy button and CSS**

In `internal/ui/static/theme.js`, add this after the `initCopyRaw` function:

```js
	// A value already in the page (the one-time password on /admin/users,
	// #310), so there is nothing to fetch, unlike initCopyRaw. Delegated
	// for the same reason as initCopyLink.
	function initCopyText() {
		document.addEventListener("click", function (e) {
			var btn = e.target.closest("[data-copy-text]");
			if (!btn) return;
			copyText(btn.getAttribute("data-copy-text"))
				.then(function () { flashLabel(btn, "Copied"); })
				.catch(function () { flashLabel(btn, "Copy failed"); });
		});
	}
```

Then add `initCopyText();` on the line after `initCopyRaw();` (currently line 271).

Append to `internal/ui/static/app.css`, right after the `.admin-table` rules (around line 812):

```css
/* ---- User management (#310) ------------------------------------------- */

/* The row menus reuse .outline-menu (PATTERNS.md "No-JS <details>
 * disclosure menu"), whose toggle stays transparent until its .outline-row
 * is hovered. A table row is not an .outline-row, so show it outright. */
.usermgmt-menu .outline-menu-toggle { opacity: 1; }

.usermgmt-menu-link {
	display: block;
	padding: var(--s-1) var(--s-2);
	border-radius: var(--radius);
	color: var(--c-danger);
	text-decoration: none;
	line-height: 1.5;
}
.usermgmt-menu-link:hover { background: var(--c-danger-bg); }

.usermgmt-password code { font-size: var(--fs-base); user-select: all; }
.usermgmt-add { max-width: 22rem; }
```

- [ ] **Step 7: Mount in buildStack and register with the arch test**

In `cmd/onsuite/stack.go`, add the import `"github.com/iliafrenkel/on-suite/internal/platform/usermgmt"`. Insert the following immediately after the two `routes.Handle(mux, "GET /admin…", false, adminHandler)` lines and before the `GET /{$}` line:

```go
	// User management changes accounts, so it lives beside the read-only
	// admin page rather than inside it (#310).
	usermgmt.Routes(mux, routes, authn, usermgmt.Deps{
		Users:   deps.Users,
		Render:  rend,
		Errors:  errs,
		Log:     deps.Log,
		Nav:     deps.Registry.NavItems(),
		Version: deps.Version,
	})
```

In `internal/arch/arch_test.go` `TestScanSeesTheRealTree`, add `"internal/platform/usermgmt",` after `"internal/platform/admin",`.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/platform/usermgmt/ ./internal/arch/ ./cmd/onsuite/ -count=1 -race`
Expected: `ok` for all three. If a `stack_test.go` test pins the exact route list or count, add the eight new patterns to its expectation.

- [ ] **Step 9: Full check and commit**

Run the Global Constraints full check. Then:

```bash
git add internal/platform/usermgmt internal/ui/templates/admin_users.html internal/ui/templates/account.html internal/ui/static/theme.js internal/ui/static/app.css cmd/onsuite/stack.go internal/arch/arch_test.go
git commit -m "feat(platform): /admin/users page that adds users with a one-time password (#310)"
```

---

### Task 5: Reset password, change role, delete

**Files:**
- Modify: `internal/platform/usermgmt/admin.go` (replace the four stub handlers)
- Create: `internal/ui/templates/admin_user_delete.html`
- Test: `internal/platform/usermgmt/admin_test.go` (append)

**Interfaces:**
- Consumes: `auth.Store.SetPassword/SetAdmin/DeleteUser/UserByID`, `auth.ErrLastAdmin`, `auth.ErrNotFound`, and from Task 4: `renderUsers`, `newPassword`, `audit`, `page`, `usersPage`, `generatedPassword`, and fixture helpers including `generated(t, doc)`.
- Produces: `func (h *handlers) target(w http.ResponseWriter, r *http.Request, selfMsg string) (auth.User, bool)`, `type deletePage struct{ ID int64; Username string }`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/platform/usermgmt/admin_test.go` (add `"context"`, `"fmt"` and `"github.com/iliafrenkel/on-suite/internal/apptest"` to its imports):

```go
func path(format string, id int64) string { return fmt.Sprintf(format, id) }

func TestResettingAPasswordShowsANewOneAndSignsTheUserOut(t *testing.T) {
	s := newServer(t)
	ilia := s.plain.user

	rec := s.post(t, s.root, path("/admin/users/%d/password", ilia.ID), url.Values{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("the reset response may be cached")
	}
	password := generated(t, s.doc(t, rec))

	if again := s.get(t, s.plain, "/admin/users"); again.Code != http.StatusSeeOther {
		t.Errorf("ilia's old session still works: status = %d", again.Code)
	}
	if s.tryLogIn(t, "ilia", password).Code != http.StatusSeeOther {
		t.Error("the new password does not sign in")
	}
	if s.tryLogIn(t, "ilia", apptest.Password).Code == http.StatusSeeOther {
		t.Error("the old password still signs in")
	}
}

func TestPromotingAndDemoting(t *testing.T) {
	s := newServer(t)
	id := s.plain.user.ID

	rec := s.post(t, s.root, path("/admin/users/%d/role", id), url.Values{"admin": {"1"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/users" {
		t.Fatalf("promote = %d → %q, want 303 → /admin/users", rec.Code, rec.Header().Get("Location"))
	}
	if !s.user(t, "ilia").IsAdmin {
		t.Fatal("promote did not take")
	}
	// The new role applies to ilia's existing session at once.
	if got := s.get(t, s.plain, "/admin/users"); got.Code != http.StatusOK {
		t.Errorf("promoted ilia gets %d on /admin/users, want 200", got.Code)
	}

	s.post(t, s.root, path("/admin/users/%d/role", id), url.Values{"admin": {"0"}})
	if s.user(t, "ilia").IsAdmin {
		t.Error("demote did not take")
	}
}

func TestDeletingAUserAsksFirstThenRemovesThem(t *testing.T) {
	s := newServer(t)
	id := s.plain.user.ID

	confirm := s.get(t, s.root, path("/admin/users/%d/delete", id))
	if confirm.Code != http.StatusOK {
		t.Fatalf("confirm page = %d", confirm.Code)
	}
	doc := s.doc(t, confirm)
	if !strings.Contains(doc.Text(), "Permanently delete") || !strings.Contains(doc.Text(), "ilia") {
		t.Error("the confirmation page does not say who and what is deleted")
	}
	doc.MustHave(fmt.Sprintf(`form[action="/admin/users/%d/delete"]`, id))
	if _, err := s.users.UserByID(context.Background(), id); err != nil {
		t.Fatal("merely viewing the confirmation deleted the user")
	}

	rec := s.post(t, s.root, path("/admin/users/%d/delete", id), url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/users" {
		t.Fatalf("delete = %d → %q", rec.Code, rec.Header().Get("Location"))
	}
	if _, err := s.users.UserByID(context.Background(), id); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("user still exists: err = %v", err)
	}
	if !strings.Contains(s.logs.String(), "action=user.delete") {
		t.Error("the delete was not logged")
	}
}

func TestAnAdminCannotActOnThemselvesHere(t *testing.T) {
	s := newServer(t)
	id := s.root.user.ID
	before := s.user(t, "root").PasswordHash

	for _, tc := range []struct{ method, path string }{
		{"POST", path("/admin/users/%d/password", id)},
		{"POST", path("/admin/users/%d/role", id)},
		{"GET", path("/admin/users/%d/delete", id)},
		{"POST", path("/admin/users/%d/delete", id)},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var rec *httptest.ResponseRecorder
			if tc.method == "GET" {
				rec = s.get(t, s.root, tc.path)
			} else {
				rec = s.post(t, s.root, tc.path, url.Values{"admin": {"0"}})
			}
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			s.doc(t, rec).MustHave(".notice-error")
		})
	}

	root := s.user(t, "root")
	if !root.IsAdmin || root.PasswordHash != before {
		t.Error("a self-action changed the account")
	}
	if s.get(t, s.root, "/admin/users").Code != http.StatusOK {
		t.Error("a self-action ended root's session")
	}
}

func TestUnknownOrMalformedIDsAre404(t *testing.T) {
	s := newServer(t)
	for _, p := range []string{"/admin/users/9999/password", "/admin/users/abc/role", "/admin/users/9999/delete"} {
		if rec := s.post(t, s.root, p, url.Values{}); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", p, rec.Code)
		}
	}
	if rec := s.get(t, s.root, "/admin/users/9999/delete"); rec.Code != http.StatusNotFound {
		t.Errorf("GET confirm for a missing user = %d, want 404", rec.Code)
	}
}

func TestEveryMutatingRouteIs404ForANonAdmin(t *testing.T) {
	s := newServer(t)
	id := s.root.user.ID
	for _, p := range []string{
		path("/admin/users/%d/password", id),
		path("/admin/users/%d/role", id),
		path("/admin/users/%d/delete", id),
	} {
		if rec := s.post(t, s.plain, p, url.Values{"admin": {"0"}}); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s as non-admin = %d, want 404", p, rec.Code)
		}
	}
	if rec := s.get(t, s.plain, path("/admin/users/%d/delete", id)); rec.Code != http.StatusNotFound {
		t.Errorf("GET confirm as non-admin = %d, want 404", rec.Code)
	}
	if !s.user(t, "root").IsAdmin {
		t.Error("a non-admin demoted root")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/platform/usermgmt/ -count=1`
Expected: FAIL. The stubs answer 404, so the reset, promote, delete and self-action tests fail.

- [ ] **Step 3: Implement**

In `internal/platform/usermgmt/admin.go`, add `"strconv"` to the imports. Replace the four stub lines (and the `// Filled in by Task 5.` comment) with:

```go
// deletePage is the view model for admin_user_delete.html.
type deletePage struct {
	ID       int64
	Username string
}

// target resolves {id} to an account other than the viewer's. When it
// returns false it has already written the response: 404 for an id that is
// malformed or unknown, or the users page with selfMsg when the id is the
// viewer's own. The self check runs server-side, not only by hiding the menu
// (spec §4.4).
func (h *handlers) target(w http.ResponseWriter, r *http.Request, selfMsg string) (auth.User, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		h.d.Errors.NotFound(w, r)
		return auth.User{}, false
	}
	u, err := h.d.Users.UserByID(r.Context(), id)
	if errors.Is(err, auth.ErrNotFound) {
		h.d.Errors.NotFound(w, r)
		return auth.User{}, false
	}
	if err != nil {
		h.d.Errors.Internal(w, r, err)
		return auth.User{}, false
	}
	if me, _ := web.UserFrom(r.Context()); u.ID == me.ID {
		h.renderUsers(w, r, http.StatusUnprocessableEntity, usersPage{Error: selfMsg})
		return auth.User{}, false
	}
	return u, true
}

// storeFailed maps a store error from a mutation to a response.
func (h *handlers) storeFailed(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrNotFound):
		h.d.Errors.NotFound(w, r)
	case errors.Is(err, auth.ErrLastAdmin):
		h.renderUsers(w, r, http.StatusUnprocessableEntity, usersPage{Error: "At least one administrator must remain."})
	default:
		h.d.Errors.Internal(w, r, err)
	}
}

func (h *handlers) resetPassword(w http.ResponseWriter, r *http.Request) {
	u, ok := h.target(w, r, "Use Account to change your own password.")
	if !ok {
		return
	}
	password, hash, ok := h.newPassword(w, r)
	if !ok {
		return
	}
	if err := h.d.Users.SetPassword(r.Context(), u.ID, hash, ""); err != nil {
		h.storeFailed(w, r, err)
		return
	}
	h.audit(r, "user.reset_password", u.Username)
	h.renderUsers(w, r, http.StatusOK, usersPage{Generated: &generatedPassword{Username: u.Username, Password: password}})
}

func (h *handlers) setRole(w http.ResponseWriter, r *http.Request) {
	u, ok := h.target(w, r, "You can't change your own role.")
	if !ok {
		return
	}
	makeAdmin := r.PostFormValue("admin") == "1"
	if err := h.d.Users.SetAdmin(r.Context(), u.ID, makeAdmin); err != nil {
		h.storeFailed(w, r, err)
		return
	}
	action := "user.demote"
	if makeAdmin {
		action = "user.promote"
	}
	h.audit(r, action, u.Username)
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (h *handlers) confirmDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := h.target(w, r, "You can't delete yourself.")
	if !ok {
		return
	}
	page := h.page(r, "Delete "+u.Username, "admin")
	page.Data = deletePage{ID: u.ID, Username: u.Username}
	if err := h.d.Render.Page(w, http.StatusOK, "admin_user_delete", page); err != nil {
		h.d.Errors.Internal(w, r, err)
	}
}

func (h *handlers) delete(w http.ResponseWriter, r *http.Request) {
	u, ok := h.target(w, r, "You can't delete yourself.")
	if !ok {
		return
	}
	if err := h.d.Users.DeleteUser(r.Context(), u.ID); err != nil {
		h.storeFailed(w, r, err)
		return
	}
	h.audit(r, "user.delete", u.Username)
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}
```

Create `internal/ui/templates/admin_user_delete.html`:

```html
{{define "content"}}
<div class="centered stack">
	<p class="faint"><a href="/admin/">Admin</a> / <a href="/admin/users">Users</a></p>
	<h1>Delete {{.Data.Username}}?</h1>
	<p>Permanently delete <strong>{{.Data.Username}}</strong> and everything they own in every app? This cannot be undone.</p>
	<form method="post" action="/admin/users/{{.Data.ID}}/delete" class="row">
		<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
		<button type="submit" class="danger">Delete {{.Data.Username}}</button>
		<a href="/admin/users">Cancel</a>
	</form>
</div>
{{end}}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/platform/usermgmt/ -count=1 -race`
Expected: `ok`.

- [ ] **Step 5: Full check and commit**

Run the Global Constraints full check. Then:

```bash
git add internal/platform/usermgmt/admin.go internal/platform/usermgmt/admin_test.go internal/ui/templates/admin_user_delete.html
git commit -m "feat(platform): reset passwords, change roles and delete users from /admin/users (#310)"
```

---

### Task 6: `/account` self-service password change

**Files:**
- Modify: `internal/platform/usermgmt/account.go` (replace stubs)
- Modify: `internal/ui/templates/account.html` (replace placeholder)
- Modify: `internal/ui/templates/base.html` (username → link)
- Modify: `internal/ui/static/app.css` (`.shell-username`)
- Test: `internal/platform/usermgmt/account_test.go` (new)

**Interfaces:**
- Consumes: `auth.VerifyPassword(hash, plain string) (bool, error)`, `auth.ValidatePassword`, `auth.HashPassword`, `auth.Store.SetPassword`, `web.SessionCookieName`, `web.UserFrom`, and from Task 4: `page` and `audit`.
- Produces: `type accountPage struct{ Username string; IsAdmin bool; Joined string; Error string; Changed bool }`.

- [ ] **Step 1: Write the failing tests**

Create `internal/platform/usermgmt/account_test.go`:

```go
package usermgmt_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apptest"
)

func TestAnonymousAccountRedirectsToLogin(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, nil, "/account")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
		t.Fatalf("status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestTheAccountPageShowsWhoYouAreAndTheHeaderLinksToIt(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, s.plain, "/account")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := s.doc(t, rec)
	if !strings.Contains(doc.Text(), "ilia") {
		t.Error("the page does not show the username")
	}
	doc.MustHave(`.shell-user a[href="/account"]`)
	doc.MustHave(`form[action="/account/password"]`)
}

func TestChangingYourPasswordKeepsThisSessionAndEndsTheOthers(t *testing.T) {
	s := newServer(t)
	other := s.logIn(t, "ilia", apptest.Password)
	const next = "a-brand-new-password"

	rec := s.post(t, s.plain, "/account/password", url.Values{
		"current": {apptest.Password}, "new": {next}, "confirm": {next},
	})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account?changed=1" {
		t.Fatalf("status = %d → %q, want 303 → /account?changed=1", rec.Code, rec.Header().Get("Location"))
	}

	page := s.get(t, s.plain, "/account?changed=1")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Password changed") {
		t.Errorf("this session lost access or shows no confirmation (status %d)", page.Code)
	}
	if s.get(t, other, "/account").Code != http.StatusSeeOther {
		t.Error("the other session survived the change")
	}
	if s.tryLogIn(t, "ilia", next).Code != http.StatusSeeOther {
		t.Error("the new password does not sign in")
	}
	if !strings.Contains(s.logs.String(), "action=account.change_password") {
		t.Error("the change was not logged")
	}
	if strings.Contains(s.logs.String(), next) {
		t.Error("the new password was logged")
	}
}

func TestChangePasswordRejections(t *testing.T) {
	tests := []struct {
		name                  string
		current, next, repeat string
	}{
		{"wrong current password", "not-my-password", "a-brand-new-password", "a-brand-new-password"},
		{"confirmation does not match", apptest.Password, "a-brand-new-password", "a-different-password"},
		{"new password too short", apptest.Password, "short", "short"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newServer(t)
			rec := s.post(t, s.plain, "/account/password", url.Values{
				"current": {tt.current}, "new": {tt.next}, "confirm": {tt.repeat},
			})
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			doc := s.doc(t, rec)
			doc.MustHave(".notice-error")
			for _, pw := range []string{tt.current, tt.next} {
				if strings.Contains(rec.Body.String(), `value="`+pw+`"`) {
					t.Errorf("a password (%q) was echoed back into the form", pw)
				}
			}
			if s.user(t, "ilia").PasswordHash != apptest.PasswordHash {
				t.Error("the password changed despite the rejection")
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/platform/usermgmt/ -run 'Account|ChangePassword|ChangingYour' -count=1`
Expected: FAIL. The stubs return 404, and the header has no `/account` link yet.

- [ ] **Step 3: Implement the handlers**

Replace `internal/platform/usermgmt/account.go` with:

```go
package usermgmt

import (
	"net/http"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// accountPage is the view model for account.html. It never carries a
// password: a rejected form comes back with every password field empty.
type accountPage struct {
	Username string
	IsAdmin  bool
	Joined   string
	Error    string
	// Changed shows the confirmation after the post-change redirect.
	Changed bool
}

func (h *handlers) account(w http.ResponseWriter, r *http.Request) {
	h.renderAccount(w, r, http.StatusOK, accountPage{Changed: r.URL.Query().Get("changed") == "1"})
}

func (h *handlers) renderAccount(w http.ResponseWriter, r *http.Request, status int, data accountPage) {
	me, _ := web.UserFrom(r.Context())
	data.Username, data.IsAdmin, data.Joined = me.Username, me.IsAdmin, me.CreatedAt.Format("2006-01-02")
	page := h.page(r, "Account", "")
	page.Data = data
	if err := h.d.Render.Page(w, status, "account", page); err != nil {
		h.d.Errors.Internal(w, r, err)
	}
}

// changePassword keeps the session that made the change and revokes every
// other one, so a stolen cookie stops working without signing the user out
// of the browser they are using (spec §5).
func (h *handlers) changePassword(w http.ResponseWriter, r *http.Request) {
	me, _ := web.UserFrom(r.Context())
	current, next, confirm := r.PostFormValue("current"), r.PostFormValue("new"), r.PostFormValue("confirm")
	reject := func(msg string) {
		h.renderAccount(w, r, http.StatusUnprocessableEntity, accountPage{Error: msg})
	}

	ok, err := auth.VerifyPassword(me.PasswordHash, current)
	if err != nil {
		h.d.Errors.Internal(w, r, err)
		return
	}
	if !ok {
		reject("Your current password is incorrect.")
		return
	}
	if next != confirm {
		reject("The new passwords don't match.")
		return
	}
	if err := auth.ValidatePassword(next); err != nil {
		// ValidatePassword says "password must be ..."; prefixing "New "
		// makes it read as a sentence about this field.
		reject("New " + err.Error() + ".")
		return
	}
	hash, err := auth.HashPassword(next)
	if err != nil {
		h.d.Errors.Internal(w, r, err)
		return
	}
	keep := ""
	if c, err := r.Cookie(web.SessionCookieName); err == nil {
		keep = c.Value
	}
	if err := h.d.Users.SetPassword(r.Context(), me.ID, hash, keep); err != nil {
		h.d.Errors.Internal(w, r, err)
		return
	}
	h.audit(r, "account.change_password", me.Username)
	http.Redirect(w, r, "/account?changed=1", http.StatusSeeOther)
}
```

- [ ] **Step 4: Write the template and header link**

Replace `internal/ui/templates/account.html` with:

```html
{{define "content"}}
<div class="centered stack">
	<h1>Account</h1>
	<dl class="admin-facts">
		<div class="admin-fact"><dt>Username</dt><dd>{{.Data.Username}}</dd></div>
		<div class="admin-fact"><dt>Role</dt><dd>{{if .Data.IsAdmin}}Administrator{{else}}User{{end}}</dd></div>
		<div class="admin-fact"><dt>Joined</dt><dd>{{.Data.Joined}}</dd></div>
	</dl>

	<h2>Change password</h2>
	{{with .Data.Error}}<div class="notice notice-error" role="alert">{{.}}</div>{{end}}
	{{if .Data.Changed}}<div class="notice" role="status">Password changed. Your other sessions were signed out.</div>{{end}}

	<form method="post" action="/account/password" class="stack">
		<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
		<div class="field">
			<label for="current">Current password</label>
			<input id="current" name="current" type="password" autocomplete="current-password" required>
		</div>
		<div class="field">
			<label for="new">New password</label>
			<input id="new" name="new" type="password" autocomplete="new-password" required>
		</div>
		<div class="field">
			<label for="confirm">Repeat new password</label>
			<input id="confirm" name="confirm" type="password" autocomplete="new-password" required>
		</div>
		<button type="submit" class="primary">Change password</button>
	</form>
</div>
{{end}}
```

(No `minlength` attribute. The server is the one rule, and a browser-side copy of `MinPasswordLength` could drift.)

In `internal/ui/templates/base.html`, inside `<div class="shell-user">`, replace

```html
		<span>{{.Shell.Username}}</span>
```

with

```html
		<a class="shell-username" href="/account" title="Account">{{.Shell.Username}}</a>
```

Append this to the user-management block in `internal/ui/static/app.css`:

```css
/* The header username is the way into /account (#310): a link, but still
 * reading as the plain name it replaced. */
.shell-username { color: inherit; text-decoration: none; }
.shell-username:hover { text-decoration: underline; }
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/platform/usermgmt/ -count=1 -race`
Expected: `ok`. Then run `go test ./... -count=1`. Any existing test that asserts the header renders `<span>{{username}}</span>` (grep for `shell-user` in `*_test.go`) must be updated to look for the link.

- [ ] **Step 6: Full check and commit**

Run the Global Constraints full check. Then:

```bash
git add internal/platform/usermgmt/account.go internal/platform/usermgmt/account_test.go internal/ui/templates/account.html internal/ui/templates/base.html internal/ui/static/app.css
git commit -m "feat(platform): /account page to change your own password (#310)"
```

---

### Task 7: Admin page link, docs, manual check

**Files:**
- Modify: `internal/ui/templates/admin.html` (link under the users table)
- Modify: `internal/platform/admin/admin_test.go` (assert the link)
- Modify: `docs/superpowers/specs/2026-08-24-admin-page-design.md` (amendment note in §1)
- Modify: `AGENTS.md`
- Modify: `docs/DEPLOYING.md`

**Interfaces:** none new.

- [ ] **Step 1: Write the failing test**

Append to `internal/platform/admin/admin_test.go`:

```go
func TestTheUsersSectionLinksToUserManagement(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, s.admin, "/admin/")
	htmlassert.Parse(t, rec.Body.String()).MustHave(`#users a[href="/admin/users"]`)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/platform/admin/ -run TestTheUsersSectionLinksToUserManagement -count=1`
Expected: FAIL, because no element matches.

- [ ] **Step 3: Add the link**

In `internal/ui/templates/admin.html`, in `<section class="admin-section" id="users">`, add this after the closing `</div>` of the `.scroll-x` wrapper around the users table and before `</section>`:

```html
		<p><a href="/admin/users">Manage users →</a></p>
```

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/platform/admin/ -count=1`
Expected: `ok`.

- [ ] **Step 5: Update the docs**

In `docs/superpowers/specs/2026-08-24-admin-page-design.md`, append this paragraph to the end of §1 Purpose:

```markdown
> **Amendment (2026-09-27, #310):** `/admin/` itself stays read-only. Changing
> accounts lives on a separate page, `/admin/users`, in its own package
> (`internal/platform/usermgmt`), linked from the users card. See
> [2026-09-27-user-management-design.md](2026-09-27-user-management-design.md).
```

In `AGENTS.md`:
- In the "Local run" section, after the `./onsuite user add …` line, add:
  `./onsuite user reset-password ilia --data-dir ./data  # recovery; prompts for the new password`
- After the paragraph describing `internal/platform/admin` (it ends "…[docs/superpowers/specs/2026-08-24-admin-page-design.md](…)."), add:

```markdown
[internal/platform/usermgmt](internal/platform/usermgmt/usermgmt.go) is its
writable sibling: `/admin/users` (admin-only, same 404 guard) adds, deletes,
promotes/demotes and resets users with one-time generated passwords, and
`/account` lets anyone change their own password. `auth.Store` enforces that
at least one admin always remains (`ErrLastAdmin`). Its design is in
[docs/superpowers/specs/2026-09-27-user-management-design.md](docs/superpowers/specs/2026-09-27-user-management-design.md).
```

- Where `./onsuite help lists all commands (…)` appears, leave it unchanged. The command list there is by top-level command, and `user` is already in it.

In `docs/DEPLOYING.md`, find the paragraph around line 41 that tells readers to run `onsuite user add` again with the service stopped. Append this after it:

```markdown
Once you can sign in as an administrator, you can add further accounts at
**Admin → Manage users** (`/admin/users`) with the service running. Each new
account gets a one-time generated password that you pass on, and the user can
change it under **Account**. If an account (even the only admin) cannot sign
in, reset it from the server:

    onsuite user reset-password <name> --data-dir /var/lib/onsuite
```

Match the surrounding code-block style in that file: if it uses fenced ```` ```bash ```` blocks, use one instead of the indented block.

- [ ] **Step 6: Manual browser check**

Build and run locally with a scratch data dir:

```bash
go build ./cmd/onsuite
printf 'a-sufficiently-long-password\n' | ./onsuite user add root --admin --data-dir ./tmp-310
./onsuite serve --data-dir ./tmp-310
```

In the browser, as `root`:
1. Admin → "Manage users →". Add `alice`. The password panel appears, and Copy flashes "Copied".
2. Open a private window and sign in as alice with that password. The header name links to `/account`. Change the password there and see the confirmation.
3. As root, open alice's row menu (the "…" is visible without hovering): Make admin, then Remove admin, then Reset password (alice's private window is now signed out), then Delete… → confirm. Alice is gone.
4. Root's own row shows "(you)" and an Account link, with no menu.
5. Check both light and dark themes, and a narrow (mobile) width. The table scrolls horizontally inside `.scroll-x`, and the row menu opens to the left.

Then stop the server and run `rm -rf ./tmp-310 onsuite`.

- [ ] **Step 7: Full check and commit**

Run the Global Constraints full check. Then:

```bash
git add internal/ui/templates/admin.html internal/platform/admin/admin_test.go docs/superpowers/specs/2026-08-24-admin-page-design.md AGENTS.md docs/DEPLOYING.md
git commit -m "docs(platform): document user management and link it from /admin/ (#310)"
```

---

## Self-Review Notes

- **Spec coverage:**
  - §3 routes/guards → Task 4 `Routes`.
  - §4.1–4.3 → Tasks 4–5.
  - §4.4 self/last-admin → Task 5 `target`, `storeFailed`, and Task 1.
  - §4.5 audit → Task 4 `audit` and the tests in Tasks 4–6.
  - §5 → Task 6.
  - §6 → Tasks 1–2.
  - §7 → Task 3.
  - §8 → tests in every task.
  - §9 → Task 7.
- **Known gap, accepted:** the handler's `ErrLastAdmin` branch cannot be reached through the UI (the acting admin always remains, by the self-action rule). It is covered at the store layer (Task 1) only. This is deliberate, matching the spec's "backstop" wording.
- **Test cost:** add/reset/change handlers hash at real Argon2id parameters (64 MiB). The usermgmt tests make roughly ten such calls. That is acceptable, but if `-race` makes the package noticeably slow, the fix is a `Deps.Hash func(string) (string, error)` seam, and that would be a follow-up issue, not this PR.
