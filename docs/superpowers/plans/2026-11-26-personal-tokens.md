# Personal tokens for AI access Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user create a long-lived personal token on Account → Connected assistants and use it on `/mcp` as a fixed `Authorization: Bearer` header, for clients such as the Pebble Index 01 ring that can't do OAuth.

**Architecture:** A personal token is an `oauth_grants` row with `client_id = 'personal'` plus one SHA-256 hash in a new `oauth_personal_tokens` table. `oauth.Server.Verify` sends `onst_…` bearers to a hash lookup and everything else to the OAuth library as now, so the `mcp` package, the list, Disconnect, audit, the admin page and password-change cleanup need no changes. The sweep skips personal grants in its dead-grant rule and removes ones unused for 90 days.

**Tech Stack:** Go, SQLite migrations (`internal/platform/auth/migrations`), html/template, `crypto/rand`, `crypto/sha256`.

**Spec:** `docs/superpowers/specs/2026-11-26-personal-tokens-design.md`

## Global Constraints

- Token format: `onst_` + base64url without padding (`base64.RawURLEncoding`) of 32 bytes from `crypto/rand` (43 characters after the prefix).
- Stored as lowercase hex SHA-256 of the **whole** token, prefix included. The token itself is never stored or logged.
- Personal-token grants have `client_id = "personal"`, `redirect_host = ""`, `requested_scopes = granted_scopes` = the ticked scopes.
- Name: required after trimming spaces, at most 60 characters (count runes, not bytes).
- At least one scope; every scope must be in `oauth.AllScopes`.
- At most 10 personal tokens per user.
- No expiry date. The sweep removes a personal grant when `COALESCE(last_used_at, created_at)` is more than 90 days ago.
- `Caller.Expires` for a personal token is the store clock's now + 24 hours (the MCP SDK refuses a zero expiry; don't set `AllowMissingExpiration`).
- `last_used_at` touch follows the existing `touchEvery` (one minute) rule.
- The show-once page is the POST response (no redirect) and is sent with `Cache-Control: no-store`.
- Copy, verbatim from the spec: button **Create a token**; the line under it "For apps that can't sign in to ON Suite themselves, such as the Pebble ring."; warning "A token that isn't used for 90 days is removed automatically."; list label **Personal token**; never-used "Not used yet".
- OAuth tokens (Claude, ChatGPT, Gemini) must behave exactly as before: existing tests stay green unchanged.
- Never push to `main`. All work goes in one PR from `feat/personal-tokens`. The full check in AGENTS.md must be green on every commit.

## Review Focus

- **A bearer that starts with `onst_` but is garbage** (too short, bad characters, an old revoked token). It must be a plain `ErrInvalidToken`, never a panic or a fall-through to the OAuth library. Test in Task 2.
- **A user's second (and tenth) personal token.** Earlier tokens keep working; nothing goes through `UpsertGrant`'s one-per-client replace. The eleventh is refused with the form shown again. Tests in Task 1 and Task 3.
- **Refreshing the show-once page** (the browser re-sends the POST). No second token is created. Test in Task 3.
- **A form post naming a scope that isn't in `AllScopes`** (e.g. `admin:write`) or a name of 61 characters. Refused, nothing stored. Test in Task 3.
- **Another user's token on the same server.** It only ever sees its own user's data, and Disconnect on user A's page can't touch user B's personal token. Tests in Task 2 and Task 3.

---

### Task 1: Store personal tokens and sweep them

**Files:**
- Create: `internal/platform/auth/migrations/0006_personal_tokens.sql` (SQL exactly as in the spec's "Storage" section, with its comment)
- Create: `internal/platform/oauth/personal.go`
- Modify: `internal/platform/oauth/store_grants.go` (`Sweep`: the dead-grant step and a new inactivity step; add constant `personalIdle`)
- Test: `internal/platform/oauth/personal_test.go`, `internal/platform/oauth/store_test.go` (sweep tests)

**Interfaces:**
- Produces (all in package `oauth`):
  - `const PersonalClientID = "personal"`
  - `const personalTokenPrefix = "onst_"`
  - `const maxPersonalTokens = 10`
  - `const personalIdle = 90 * 24 * time.Hour`
  - `var ErrTooManyTokens = errors.New("oauth: too many personal tokens")`
  - `var ErrGrantExists = errors.New("oauth: grant already exists")`
  - `func newPersonalToken() (token, hash string, err error)`
  - `func hashPersonalToken(token string) string`
  - `func newGrantID() (string, error)`: 32 lowercase hex characters from 16 random bytes
  - `func validGrantID(id string) bool`: true only for exactly 32 lowercase hex characters
  - `func (s *Store) CreatePersonalToken(ctx context.Context, g Grant, tokenHash string) error`
  - `func (s *Store) PersonalTokenGrant(ctx context.Context, tokenHash string) (Grant, error)`: `ErrNotFound` if no match

- [ ] **Step 1: Write the failing tests** in `personal_test.go` (use `newFixture`; helper `personalGrant(id string, userID int64) Grant` with `ClientID: PersonalClientID`, `ClientName: "Pebble ring"`, `RedirectHost: ""`, `Requested`/`Granted: []string{"notes:read", "notes:write"}`, `CreatedAt: f.now`):
  - `TestNewPersonalTokenFormat`: token has prefix `onst_` and length `5+43`; `hash == hashPersonalToken(token)`; `hash` is 64 lowercase hex characters; two calls give different tokens.
  - `TestCreatePersonalTokenRoundTrip`: create, then `PersonalTokenGrant(hash)` returns a Grant with the same ID, user, `ClientID == "personal"`, name, granted scopes and a zero `LastUsedAt`.
  - `TestSecondPersonalTokenKeepsTheFirst`: create two for `f.u1` with different IDs and hashes; both found by hash.
  - `TestPersonalTokenLimit`: ten succeed for `f.u1`; the eleventh returns `ErrTooManyTokens` and is not stored; `f.u2` can still create one. An OAuth grant for `f.u1` (via `f.addGrant`) doesn't count toward the ten.
  - `TestCreatePersonalTokenDuplicateID`: a second create with the same grant ID returns `ErrGrantExists`; the first token still works and no second token row exists.
  - `TestPersonalTokenGoesWithGrant`: after `RevokeGrant(ctx, f.u1, id)`, `PersonalTokenGrant(hash)` is `ErrNotFound`. Same after `DELETE FROM users WHERE id = ?` on `f.u1`. Same after `auth.NewStore(f.handle).SetPassword(ctx, f.u1, "$argon2id$other", "")`.
  - `TestValidGrantID`: true for a `newGrantID()` result; false for `""`, 31 or 33 hex characters, upper-case hex, and `"../../etc"`.
- [ ] **Step 2: Write the failing sweep tests** in `store_test.go`:
  - `TestSweepKeepsPersonalTokens`: create a personal token, move `f.now` forward 1 hour, `Sweep`; the token is still found (the dead-grant rule must not take it).
  - `TestSweepRemovesIdlePersonalTokens`: three tokens created at `f.now`. Token A is never used. Token B's `last_used_at` is set (with `TouchGrant`) at `f.now + 1 day`. Token C's is set at `f.now + 2 days`. Move `f.now` to creation + 90 days + 1 minute, then `Sweep`: A is gone, B and C stay. Move to creation + 91 days + 1 minute: B is gone, C stays. An OAuth grant with a live access token in the same database is untouched throughout.
- [ ] **Step 3: Run the tests and see them fail**

Run: `go test ./internal/platform/oauth/ -run 'PersonalToken|ValidGrantID|SweepKeepsPersonal|SweepRemovesIdle' -count=1`
Expected: FAIL to compile (`newPersonalToken`, `CreatePersonalToken` … undefined).

- [ ] **Step 4: Add the migration and implement `personal.go`**

`CreatePersonalToken` runs in `s.inTx`: count `oauth_grants WHERE user_id = ? AND client_id = 'personal'`; at `maxPersonalTokens` or more, return `ErrTooManyTokens`. Insert the grant with a plain `INSERT` (not `UpsertGrant`); a primary-key conflict on `id` returns `ErrGrantExists` (check with `INSERT … ON CONFLICT (id) DO NOTHING` plus `RowsAffected() == 0`). Then insert the token row. `created_at` defaults to `s.now()` when `g.CreatedAt` is zero, as `UpsertGrant` does. `PersonalTokenGrant` selects `grantColumns + grantFrom` joined to `oauth_personal_tokens t ON t.grant_id = g.id WHERE t.token_hash = ?`, and scans with `scanGrant`.

- [ ] **Step 5: Change `Sweep`**

Add `AND client_id <> 'personal'` to the dead-grant step. Add a step after it: `DELETE FROM oauth_grants WHERE client_id = 'personal' AND COALESCE(last_used_at, created_at) < ?` with `formatTime(now.Add(-personalIdle))`. Update the `Sweep` doc comment to say so.

- [ ] **Step 6: Run the tests and see them pass**

Run: `go test ./internal/platform/oauth/ -count=1`
Expected: PASS (new and existing).

- [ ] **Step 7: Run the full check and commit**

Run the full check from AGENTS.md. Expected: all green.

```bash
git add internal/platform/auth/migrations/0006_personal_tokens.sql internal/platform/oauth/personal.go internal/platform/oauth/personal_test.go internal/platform/oauth/store_grants.go internal/platform/oauth/store_test.go
git commit -m "feat(ai): store personal tokens and sweep idle ones"
```

---

### Task 2: Accept personal tokens in Verify

**Files:**
- Modify: `internal/platform/oauth/verify.go`
- Test: `internal/platform/oauth/verify_test.go`

**Interfaces:**
- Consumes: `personalTokenPrefix`, `hashPersonalToken`, `Store.PersonalTokenGrant`, `ErrNotFound`, `touchEvery` and `Store.TouchGrant` from Task 1 and existing code.
- Produces: no new names. `Server.Verify(ctx, bearer) (Caller, error)` now also accepts `onst_` tokens.

- [ ] **Step 1: Write the failing tests** (use `newFlowEnv`, which already gives a `Server` with a real store; add a helper `personal(t, userID, scopes...) string` that calls `newPersonalToken`, `newGrantID` and `e.srv.store.CreatePersonalToken`, and returns the token):
  - `TestVerifyAcceptsPersonalToken`: `Caller` has the right `UserID`, `GrantID`, `ClientName` "Pebble ring", `Scopes` equal to the created scopes, and `Expires` equal to the store clock's now + 24h.
  - `TestVerifyPersonalTokenUnknownOrMalformed`: `ErrInvalidToken` for each of `"onst_"`, `"onst_x"`, `"onst_" + strings.Repeat("A", 43)` (well-formed but unknown), and a real token with its last character changed.
  - `TestVerifyPersonalTokenRevoked`: after `RevokeGrant`, `ErrInvalidToken`.
  - `TestVerifyPersonalTokenStorageErrorIsUnavailable`: for each of `oauth_personal_tokens` and `oauth_grants`, rename the table away (as `TestVerifyStorageErrorIsUnavailable` does); `errors.Is(err, ErrUnavailable)` and not `ErrInvalidToken`.
  - `TestVerifyPersonalTokenTouchesLastUsedAtMostOnceAMinute`: inside `synctest.Test`, the same shape as `TestVerifyTouchesLastUsedAtMostOnceAMinute`.
  - `TestVerifyPersonalTokenIsPerUser`: two users each create a token; each token's `Caller.UserID` is its own user.
  - `TestVerifyOAuthTokenUnchangedWithPersonalTokensPresent`: with a personal token in the database, the existing OAuth flow's access token still verifies with the same `Caller` as before.
- [ ] **Step 2: Run the tests and see them fail**

Run: `go test ./internal/platform/oauth/ -run 'VerifyPersonal|VerifyAcceptsPersonal|VerifyOAuthTokenUnchanged' -count=1`
Expected: FAIL (the personal token is `ErrInvalidToken`).

- [ ] **Step 3: Implement**

At the top of `Verify`: `if strings.HasPrefix(bearer, personalTokenPrefix) { return s.verifyPersonal(ctx, bearer) }`. Add `func (s *Server) verifyPersonal(ctx context.Context, bearer string) (Caller, error)` in `verify.go`: `ErrNotFound` → `ErrInvalidToken`; any other store error → `fmt.Errorf("%w: %w", ErrUnavailable, err)`; found → touch with the same `touchEvery` rule and warn-on-failure as the OAuth path (extract the shared touch into `func (s *Server) touch(ctx context.Context, g Grant)` and call it from both paths), then return the `Caller` with `Expires: s.store.now().Add(24 * time.Hour)`. Put a comment at that line: the SDK refuses a zero expiry, it only checks it within this request, and the real limit is the 90-day sweep.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./internal/platform/oauth/ ./internal/platform/mcp/ -count=1`
Expected: PASS. `internal/platform/mcp` takes `Verify` as a dependency and needs no change. Its existing scope, audit and per-grant rate-limit tests cover what a personal token's `Caller` does there.

- [ ] **Step 5: Check the token is never logged**

Run: `grep -rn 'Authorization\|bearer' internal/platform/web/middleware.go internal/platform/mcp/server.go internal/platform/oauth/*.go | grep -iv _test`
Expected: no line logs a header value or the bearer. If one does, stop and report it rather than changing logging in this task.

- [ ] **Step 6: Run the full check and commit**

```bash
git add internal/platform/oauth/verify.go internal/platform/oauth/verify_test.go
git commit -m "feat(ai): accept personal tokens on /mcp"
```

---

### Task 3: Create-token form, show-once page and list label

**Files:**
- Create: `internal/platform/oauth/pages_personal.go`
- Create: `internal/ui/templates/account_new_token.html`, `internal/ui/templates/account_token_created.html`
- Modify: `internal/platform/oauth/pages.go` (`grantRow` gets `Personal bool`; `grantRows` sets it and `LastUsed = "Not used yet"` for an unused personal grant; `mountPages` adds two routes)
- Modify: `internal/ui/templates/account_assistants.html` (the button and line above the table; **Personal token** in place of `RedirectHost` when `.Personal`)
- Modify: `internal/ui/templates/admin_ai.html` (**Personal token** in place of `RedirectHost` when `.Personal`)
- Test: `internal/platform/oauth/pages_test.go`

**Interfaces:**
- Consumes: `newPersonalToken`, `newGrantID`, `validGrantID`, `Store.CreatePersonalToken`, `ErrTooManyTokens`, `ErrGrantExists`, `PersonalClientID`, `consentRows`, `AllScopes`, `Server.ResourceURL()`.
- Produces: routes `GET /account/assistants/new-token` and `POST /account/assistants/new-token`, both behind `authn.RequireUser`. The template names are `account_new_token` and `account_token_created`.

**How refresh is stopped (the spec's one-use form ID):** the auth `Session` has no place for arbitrary values, so the GET puts a fresh `newGrantID()` in a hidden `form_id` field, and the POST uses it as the new grant's ID. A refresh re-sends the same ID, `CreatePersonalToken` returns `ErrGrantExists`, and the handler answers `303 See Other` to `/account/assistants` with no new token. A `form_id` that fails `validGrantID` is a 400. The ID isn't a secret: grant IDs already appear in every Disconnect form.

- [ ] **Step 1: Write the failing tests** in `pages_test.go`. Add a helper `newToken(t, who string, form url.Values) *httptest.ResponseRecorder` that GETs `/account/assistants/new-token`, reads the CSRF token and `form_id` from it, merges in `form`, and POSTs:
  - `TestNewTokenFormRequiresSignIn`: signed-out GET and POST are redirected to login, as `TestAssistantsPageRequiresSignIn` checks.
  - `TestNewTokenFormDefaults`: the form has a `name` input with `maxlength="60"`, a hidden `form_id` that passes `validGrantID`, all four read boxes checked and all four write boxes unchecked, and the 90-day warning text.
  - `TestNewTokenCreatesAndShowsOnce`: POST `name=Pebble ring`, `grant_scope=notes:read`, `notes:write`, `books:write` → 200; the `Cache-Control` header is `no-store`; the page contains one `onst_` token, `e.srv.ResourceURL()` (`https://onsuite.test/mcp` in the fixture) and the warning. The token from the page passes `e.srv.Verify` with exactly those three scopes. `/account/assistants` then lists "Pebble ring" with **Personal token** and "Not used yet", and doesn't contain the token.
  - `TestNewTokenRefreshCreatesNoSecond`: re-send the identical POST (same `form_id`) → 303 to `/account/assistants`; the user has one personal grant.
  - `TestNewTokenRejectsBadInput`: each of these → 422 with the form shown again, the typed name kept, and no grant stored: an empty name; a name of spaces only; a 61-rune name (use a multi-byte letter so a byte count would be wrong); no scopes; `grant_scope=admin:write`.
  - `TestNewTokenBadFormIDIs400`: `form_id=nope` → 400, nothing stored.
  - `TestNewTokenLimit`: with ten personal tokens already stored for `ilia`, the POST → 422, the form shown again with a message about the limit, and still ten.
  - `TestNewTokenCSRF`: a POST without the CSRF field is refused the way other account POSTs are (match `TestDisconnectRevokesOwnGrantOnly`'s CSRF expectations).
  - `TestDisconnectPersonalTokenOwnOnly`: `mia` posting Disconnect for `ilia`'s personal grant ID → 404 and the token still verifies; `ilia` disconnecting it → the token is `ErrInvalidToken`.
  - `TestAdminAIPageLabelsPersonalTokens`: `root` sees `ilia`'s personal token on `/admin/ai` labelled **Personal token**.
- [ ] **Step 2: Run the tests and see them fail**

Run: `go test ./internal/platform/oauth/ -run 'NewToken|DisconnectPersonal|LabelsPersonal' -count=1`
Expected: FAIL (404 on the new routes).

- [ ] **Step 3: Implement `pages_personal.go` and the templates**

View models: `newTokenPage{FormID, Name, Error string; Rows []consentRow}` and `tokenCreatedPage{Token, ServerURL string}`. The GET builds `Rows` from `consentRows(AllScopes)`. On a 422 re-render, `Rows` keep the user's ticks rather than the defaults. The POST order is: check `form_id` (400) → trim and validate the name (rune count) and scopes (422) → `newPersonalToken` → `CreatePersonalToken` with `ClientID: PersonalClientID`, `RedirectHost: ""`, `Requested` and `Granted` both the sorted ticked scopes → handle `ErrGrantExists` (303) and `ErrTooManyTokens` (422, message "You already have 10 personal tokens. Disconnect one you no longer use first.") → render `account_token_created` with `Cache-Control: no-store`. `renderPage` always writes 200, so add a status parameter or a sibling `renderPageStatus` for the 422.

The form template reuses the consent screen's table markup (`admin-table`, `grant_scope` checkboxes) and copy from the spec's mock-up. The created page shows the token in a read-only `<input>` with a Copy button using the suite's existing `data-copy-text` pattern, as the password-reset reveal in `admin_users.html` does (don't add an inline handler: CSP forbids them), then the server address, the header line `Authorization: Bearer <your token>`, the warning, and a **Done** link to `/account/assistants`.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./internal/platform/oauth/ -count=1`
Expected: PASS.

- [ ] **Step 5: Look at it in the app**

Start the server locally (`docs/self-hosting/running-locally.md`) with AI access on: `ONSUITE_MCP=on` and `ONSUITE_PUBLIC_URL=https://onsuite.localhost` (see `docs/user/admin.md#ai-access`; use a `.localhost` name, so the HTTPS-only header it triggers can't affect any real site in the browser). Sign in, create a token, and check the form, the show-once page, Copy, and the list in light and dark, Warm and Ink palettes, at desktop and phone width. Fix layout problems before committing.

- [ ] **Step 6: Run the full check and commit**

```bash
git add internal/platform/oauth/pages.go internal/platform/oauth/pages_personal.go internal/platform/oauth/pages_test.go internal/ui/templates/account_new_token.html internal/ui/templates/account_token_created.html internal/ui/templates/account_assistants.html internal/ui/templates/admin_ai.html
git commit -m "feat(ai): create personal tokens on the Account page"
```

---

### Task 4: Docs and changelog

**Files:**
- Modify: `docs/user/ai-assistants.md` (new section **Apps that can't sign in (personal tokens)** after "Connecting an assistant")
- Modify: `docs/user/admin.md` (one paragraph under "AI access"; the "sweep expired AI tokens" job line now also mentions personal tokens unused for 90 days)
- Modify: `docs/user/changelog.md` (only if the release process at that time asks for the entry in the feature PR; otherwise leave it for the release, as `docs/developers/releasing.md` says)

- [ ] **Step 1: Write the user guide section**

Cover, in the guide's plain voice: what a personal token is for (the Pebble ring as the example); Account → Connected assistants → **Create a token**; name and boxes (write boxes start unticked); copy it now because it's shown once; in the app, add a custom MCP server with the address `https://<your ON Suite>/mcp` and the header `Authorization: Bearer <token>`; the 90-day rule; changing your password removes it; Disconnect works like any assistant. Keep it to about the length of the "Connecting an assistant" section.

- [ ] **Step 2: Write the admin paragraph and job line**

Personal tokens come with AI access (no separate setting), show in the list as **Personal token**, can be revoked like any connection, and are removed by the sweep after 90 days unused.

- [ ] **Step 3: Run the docs tests and commit**

Run: `go test ./docs/... -count=1`
Expected: PASS (link checks).

```bash
git add docs/user/ai-assistants.md docs/user/admin.md
git commit -m "docs: personal tokens for AI access"
```
