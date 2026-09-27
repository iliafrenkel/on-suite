# ON Suite — User Management

**Date:** 2026-09-27
**Status:** Proposed
**Issue:** #310
**Scope:** Manage accounts from the browser. Admins can add, delete, promote,
demote and reset the password of other users at `/admin/users`. Every user can
change their own password at `/account`. The CLI gets a matching recovery
command, `onsuite user reset-password`. No schema change.

## 1. Purpose

Today the only way to create an account is `onsuite user add` on the server.
Nothing can delete an account, reset a forgotten password, change a role, or
let users change their own password — not in the CLI and not in the UI. For
a household deployment that means shelling into the box for every change,
and a forgotten password has no fix short of hand-written SQL.

## 2. Decisions

Settled during brainstorming.

| Question | Decision |
|---|---|
| Scope | Add, delete, reset password, promote/demote (admin); change own password (everyone); CLI reset. |
| How does a new or reset user get a password? | The server generates one and shows it to the admin **once**. No invite links, no email. |
| Force a change after first sign-in? | No. The admin is told to ask the user to change it under Account. No `must_change_password` flag. |
| Where does it live? | A new admin-only page, `/admin/users`, separate from the read-only `/admin/`. A new `/account` page for self-service. |
| Locked-out sole admin | `onsuite user reset-password <name>` on the server. |

## 3. Placement and authorization

A new platform package, **`internal/platform/usermgmt`**, holds both the
admin handlers and the `/account` handlers. It is a sibling of
`internal/platform/admin`, not part of it: the admin page spec
([2026-08-24-admin-page-design.md](2026-08-24-admin-page-design.md))
promises `/admin/` is "a window, not a control panel", and that promise
stays true. The `/admin/` users card gains a **Manage users →** link and
nothing else.

`usermgmt` imports only `auth`, `render`, `web` and `db`, the same layer as
`admin`. It is added to `TestScanSeesTheRealTree` in the arch test.

Mounted in `buildStack` ([stack.go](../../../cmd/onsuite/stack.go)):

| Route | Guard |
|---|---|
| `GET /admin/users` | `RequireAdmin` |
| `POST /admin/users` (add) | `RequireAdmin` |
| `POST /admin/users/{id}/password` (reset) | `RequireAdmin` |
| `POST /admin/users/{id}/role` | `RequireAdmin` |
| `GET /admin/users/{id}/delete` (confirm page) | `RequireAdmin` |
| `POST /admin/users/{id}/delete` | `RequireAdmin` |
| `GET /account` | `RequireUser` |
| `POST /account/password` | `RequireUser` |

Admin-page rejections (duplicate username, self-action, `ErrLastAdmin`)
re-render `/admin/users` with status 422 and a `.notice.notice-error`.

Every `/admin/users…` route answers a signed-in non-admin with
`errs.NotFound`, byte-identical to an unrouted path, as `/admin/` does. The
exact-path registration care documented next to the `/admin` routes in
`buildStack` applies here too: no pattern may let `ServeMux` synthesize an
unguarded redirect. All POSTs go through the existing CSRF middleware.

## 4. `/admin/users`

### 4.1 Page

- **Add user** form at the top: username, an "Administrator" checkbox, and a
  submit button.
- **User table**: username, role, created, live sessions (the same data
  `auth.ListAccounts` already gives `/admin/`). Each row except the viewer's
  own has a no-JS `<details>` menu (PATTERNS.md, "No-JS `<details>`
  disclosure menu") with:
  - **Reset password**: a POST form.
  - **Make admin** or **Remove admin**: a POST form.
  - **Delete…**: a link to the confirmation page.
- The viewer's own row shows "you" and links to `/account` instead.

### 4.2 Showing a generated password

Add and reset both **render the result directly in the POST response**
(status 200) rather than redirecting, because redirecting would put the
password in a URL or in server-side state. The response is the users page
with a panel at the top:

> Password for **alice**: `k7mqa-x3vnd-pr4tz-hw9cb`
> This is the only time it is shown. Pass it on, and ask them to change it
> under Account.

The response carries `Cache-Control: no-store`. Reloading resubmits the
form: for add that fails harmlessly with "username already taken", and for
reset it generates a fresh password, which is also harmless.

### 4.3 Actions

| Action | Effect |
|---|---|
| Add | Validate the username (`auth.ValidateUsername`), generate a password, hash it, `CreateUser`. |
| Reset password | Generate, hash, `SetPassword(id, hash, "")`, which deletes **all** the target's sessions. |
| Make/Remove admin | `SetAdmin(id, bool)`. It takes effect on the target's next request, because `RequireUser` reloads the user from the database on every request. Sessions are left alone. |
| Delete | The confirm page says: "Permanently delete **alice** and everything they own in every app? This cannot be undone." One red Delete button POSTs. `DeleteUser(id)` removes the row. Every app table references `users(id) ON DELETE CASCADE`, and so does `sessions`, so their data and sessions go with it. Then redirect to `/admin/users` (303). |

### 4.4 Guardrails

- **No self-actions.** The handlers refuse reset, role change and delete when
  `{id}` is the viewer (notice: "Use Account to change your own password" or
  "You can't change your own role or delete yourself here"). This is checked
  server-side, not just hidden in the UI. Self-password change is `/account`'s
  job.
- **At least one admin remains.** `SetAdmin(id, false)` and `DeleteUser(id)`
  return `auth.ErrLastAdmin` if the change would leave zero admins. The count
  and the write share one transaction. With the self-action rule this cannot
  trigger from the UI, since the acting admin always remains. It is the
  backstop that keeps the invariant true regardless of caller.
- An unknown `{id}` (or a non-numeric one) returns `errs.NotFound`.

### 4.5 Audit log

Each successful action logs one structured line at info level through the
request logger: `action` (`user.add`, `user.reset_password`, `user.promote`,
`user.demote`, `user.delete`, `account.change_password`), `actor` (username),
and `target` (username). Passwords and hashes are never logged.

## 5. `/account`

- The username in the shell header ([base.html](../../../internal/ui/templates/base.html))
  becomes a link to `/account`.
- The page shows username, role and join date, and a **Change password** form
  with current password, new password and confirm.
- On submit:
  1. Verify the current password against the stored hash. If wrong, show
     the notice "Current password is incorrect".
  2. Check that new and confirm match, and that the new password passes
     `auth.MinPasswordLength` (the same rule `HashPassword` already enforces).
  3. `SetPassword(id, hash, currentSessionID)`, which updates the hash and
     deletes every session of this user **except** the current one.
  4. 303 to `/account` with a success notice: "Password changed. Your other
     sessions were signed out."
- Rejections re-render the form (status 422) with a server-rendered
  `.notice.notice-error` above it. These are plain form POSTs, not htmx
  requests, so the htmx-notices pattern does not apply. Password fields are
  never echoed back.

## 6. Store (`internal/platform/auth`)

No migration. New API:

```go
var ErrLastAdmin = errors.New("auth: at least one administrator must remain")

// SetPassword replaces userID's hash and deletes all their sessions except
// keepSessionID ("" keeps none), in one transaction.
func (s *Store) SetPassword(ctx context.Context, userID int64, hash, keepSessionID string) error

// SetAdmin changes the role. Demoting the last admin returns ErrLastAdmin.
func (s *Store) SetAdmin(ctx context.Context, userID int64, isAdmin bool) error

// DeleteUser removes the account; FK cascades remove its data and sessions.
// Deleting the last admin returns ErrLastAdmin.
func (s *Store) DeleteUser(ctx context.Context, userID int64) error
```

All three return `ErrNotFound` for a missing user.

`password.go` gains `GeneratePassword() (string, error)`: 20 characters from
`crypto/rand` over a 31-symbol alphabet without look-alikes
(`abcdefghjkmnpqrstuvwxyz23456789`, which drops `0 o 1 l i`), formatted as
four dash-separated groups of five. That is about 99 bits of entropy, and at
23 runes it comfortably passes `MinPasswordLength`.

## 7. CLI

`onsuite user reset-password <name> [--data-dir …]` looks up the user, reads
the new password exactly like `user add` does (`readPassword`: a terminal
with echo off, or stdin; never a flag), and calls
`SetPassword(id, hash, "")`. It prints "Password for <name> reset; all their
sessions were signed out." `onsuite user` help lists the new subcommand.

## 8. Testing

- **Store** (real SQLite file):
  - `SetPassword` changes the hash and deletes the sessions, and a kept session
    survives.
  - `SetAdmin` and `DeleteUser` return `ErrLastAdmin` with one admin, and
    succeed with two.
  - `DeleteUser` cascades: seed a row in an app-shaped table referencing
    `users` and confirm it is gone.
  - All three return `ErrNotFound` for a missing ID.
- **`GeneratePassword`**: length, alphabet, grouping, and that two calls differ.
- **Handlers**:
  - Every `/admin/users` route returns 404 for a non-admin and a 303 to login
    for an anonymous visitor.
  - POSTs without a CSRF token are rejected.
  - Add shows the password once, sets `no-store`, and the account can sign in
    with it.
  - Reset kills the target's sessions.
  - Promote/demote flips the role.
  - Delete removes the user.
  - Self-actions are refused.
  - A duplicate or invalid username shows a notice.
- **`/account`**:
  - A wrong current password shows a notice and keeps the old hash.
  - A mismatch or a too-short new password shows a notice.
  - Success keeps the current session and deletes the others.
  - Anonymous visitors are redirected to login.
- **CLI**: `reset-password` changes the password (you can sign in with the new
  one, not the old), rejects an unknown user, and has no password flag.
- **Arch**: `usermgmt` is added to `TestScanSeesTheRealTree`, and existing
  layering tests pass.

## 9. Docs

- Add an amendment note to the admin page spec §1: `/admin/` stays read-only,
  and mutations live at `/admin/users` (this spec).
- `AGENTS.md`: mention `/admin/users`, `/account`, and `user reset-password`.
- `docs/DEPLOYING.md`: in the first-account section, note that further
  accounts can be added from `/admin/users` without stopping the service,
  whereas the CLI path asks you to stop it. Also mention `user reset-password`
  as the recovery path.

## 10. Out of scope

Invite links, email, forced password change, renaming users, disabling
(rather than deleting) accounts, per-user session lists or remote sign-out,
and rate-limiting the `/account` current-password check.
