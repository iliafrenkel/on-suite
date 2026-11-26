# Personal tokens for AI access — design

- **Status:** draft for review, 2026-11-26 (decisions agreed in conversation; awaiting approval of this doc)
- **Tracking:** not yet filed
- **Builds on:** [AI access (MCP) design](2026-11-24-ai-access-mcp-design.md)

## Why

Ilia wants to use his Pebble Index 01 ring with ON Suite: press the ring,
say "add milk to my groceries list" or "I'm on page 212 of Dune", and have
it happen. His main uses are quick capture into ON Notes and updating book
progress in ON Books. He'll explore more over time.

The ring talks to MCP servers through the Pebble phone app, which can add a
custom server by URL plus **one fixed `Authorization` header**. It can't do
an OAuth sign-in and never refreshes the token (Pebble developer docs,
"MCP Servers"). ON Suite's `/mcp` only accepts OAuth access tokens, which
expire after an hour, so today the ring can't connect.

This design adds a **personal token**: a long-lived token a user creates
themselves on their Account page and pastes into a client like the Pebble
app. It is the same kind of connection as Claude or ChatGPT: it shows up in
the same list, has the same per-app permissions, appears in Recent activity
and can be disconnected the same way.

## Goals

- A user can create a personal token from **Account → Connected
  assistants**, name it, and choose its permissions with the same per-app
  read/write boxes as the consent screen.
- The token works on `/mcp` as `Authorization: Bearer <token>`, with every
  existing tool, scope check, rate limit and audit record, unchanged.
- It never expires on a date. A token **unused for 90 days is revoked
  automatically**, and the create form warns about this.
- It lives with the other connections: listed, labelled, disconnectable by
  the user and by an admin, removed by a password change or reset.

## Non-goals

- No new tools. The current Notes and Books tools cover the ring's uses
  (Ilia's call).
- No Pebble-specific code or wording beyond an example in the user docs.
  Any client that sends a fixed bearer header can use a personal token.
- No changing a token's name or permissions after it's created. Disconnect
  it and create a new one, the same as for OAuth assistants today.
- No REST API or CLI to create tokens. The Account page is the only way in.
- No separate admin switch. Personal tokens come with AI access.

## Anti-goals (failure even if it "works")

Everything in the AI access spec's anti-goals still holds. In particular:

- A token stored in the database in a form that would work if leaked. Only
  a hash is kept.
- A token that can't be revoked straight away.
- A token that reaches another user's data, or gets scopes the user didn't
  tick.
- A token that stays around forever after the user has stopped using it.
- The OAuth tokens Claude, ChatGPT and Gemini get becoming longer-lived or
  weaker because of this change.

## Approaches considered

1. **Personal tokens created on the Account page (chosen).** A personal
   token is an `oauth_grants` row plus one hashed secret in a small new
   table. `Verify` recognises the token by its prefix and looks up the
   hash; everything else goes to the OAuth library as now. The list,
   Disconnect, audit, admin view and password-change cleanup work without
   changes because they all hang off the grant. Self-service, like the rest
   of AI access. Cost: a form, a show-once page, and the new `Verify` path.
2. **The same tokens, created from the CLI only.** The same storage and
   `Verify` path with no UI, so a little less work. Only the admin can make
   tokens, so household members would have to ask. Rejected: AI access is
   self-service everywhere else.
3. **Rejected early:** stretching the OAuth library's own access tokens to
   never expire (it fights the library's refresh rules and would weaken the
   OAuth clients' tokens too), and a separate bridge service that holds an
   OAuth session and gives the ring a fixed token (another moving part
   holding household data).

## Decisions at a glance

| Topic | Decision |
|---|---|
| Where tokens are created | Account → Connected assistants → **Create a token** |
| What the user picks | A name (required, up to 60 characters) and the per-app read/write boxes |
| Default boxes | Same as the consent screen: read boxes ticked, write boxes unticked |
| At least one scope | Required; the form refuses an empty choice |
| Showing the token | Once, straight after creating it, with a Copy button. Never again |
| Format | `onst_` + 43 characters of base64url (32 random bytes from `crypto/rand`) |
| Stored as | SHA-256 of the whole token, hex. Never the token itself |
| Expiry | None on a date. Revoked by the sweep after **90 days without use** (counted from the last use, or from creation if never used) |
| Warning | On the create form and the show-once page: unused for 90 days → removed |
| Password change / admin reset / CLI reset | Removes personal tokens too (they're grants; the existing `DELETE FROM oauth_grants WHERE user_id = ?` covers them) |
| Admin switch | None. Available whenever AI access is on |
| Rate limits, resource check | The same as OAuth tokens on `/mcp` |
| Limit per user | 10 active personal tokens *(my call; a safety net against piled-up tokens)* |

## Design

### Storage (platform migration `0006_personal_tokens.sql`)

```sql
-- One hashed secret per personal-token grant. The grant row carries the
-- name, scopes, owner, created and last-used times like any other
-- connection, so the list, Disconnect, audit, admin view and
-- password-change cleanup need no changes. Deleting the grant (or the user)
-- deletes the token.
CREATE TABLE oauth_personal_tokens (
    grant_id   TEXT PRIMARY KEY REFERENCES oauth_grants (id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE
) STRICT;
```

The grant row for a personal token:

| Column | Value |
|---|---|
| `id` | a new random ID (same shape as the library's request IDs) |
| `client_id` | the fixed string `personal` |
| `client_name` | the name the user typed |
| `redirect_host` | empty string |
| `requested_scopes`, `granted_scopes` | both the ticked scopes |
| `created_at` | now |
| `last_used_at` | NULL until the first `/mcp` call |

`client_id = 'personal'` is how the rest of the code tells a personal token
from an OAuth connection. No OAuth client can have that ID: DCR client IDs
are random and CIMD client IDs are https URLs. `oauth_grants.client_id` has
no foreign key, so the value is legal, and the orphan-client sweep only
deletes `oauth_clients` rows, so it never touches these grants.

**Don't reuse `UpsertGrant`.** It keeps one grant per (user, client) and
deletes the others, so a user's second personal token would silently
delete their first. Personal tokens need their own store method that
inserts the grant and the token row in one transaction.

### Verify

`Server.Verify` gets one branch at the top:

1. If the bearer starts with `onst_`, hash it, look up
   `oauth_personal_tokens` by `token_hash` joined to its grant.
   - Not found → `ErrInvalidToken`.
   - A storage error → `ErrUnavailable`, as today.
   - Found → build the `Caller` from the grant: user ID, grant ID, name,
     granted scopes, and `Expires` set to now + 24 hours. Touch
     `last_used_at` with the same once-a-minute rule (`touchEvery`).
2. Otherwise, the existing OAuth path, unchanged.

Nothing a personal token does goes through the OAuth library, so its
refresh rules, lifetimes and introspection fallbacks don't come into it.
The resource check isn't needed for personal tokens: they're only ever
accepted at `/mcp`, and the prefix means one can never be confused with a
library token.

*(My call.)* The MCP SDK's `RequireBearerToken` refuses a zero
`Expiration` unless `AllowMissingExpiration` is set. Rather than loosen
that check for OAuth tokens too, a personal token reports an expiry a day
ahead on every request. The SDK only looks at it within one request, and
the real limit is the 90-day inactivity sweep. A comment at that line says
so.

### Sweep

Two changes to `Store.Sweep`:

1. **Keep personal tokens out of the dead-grant rule.** Today it deletes
   grants older than 10 minutes with no access or refresh row in
   `oauth_sessions`. A personal-token grant never has such a row, so as it
   stands every personal token would be swept within minutes. Add
   `AND client_id <> 'personal'` to that step.
2. **Add the inactivity rule:** delete personal-token grants whose
   `COALESCE(last_used_at, created_at)` is more than 90 days ago. The token
   row cascades. This goes in the existing sweep job ("sweep expired AI
   tokens" on Admin → Jobs), so no new job.

`last_used_at` is touched at most once a minute, so "90 days" is accurate
to within a minute, which is fine.

### Account page

**Connected assistants** (`GET /account/assistants`) gets a **Create a
token** button above the list, with a line under it: "For apps that can't
sign in to ON Suite themselves, such as the Pebble ring."

**Create form** (`GET /account/assistants/new-token`,
`POST /account/assistants/new-token`, behind `RequireUser` and the usual
CSRF check):

```
Create a personal token

Name          [ Pebble ring                    ]

What it can do
  Books   [x] Read your books     [ ] Update books
  Notes   [x] Read your notes     [ ] Add notes
  Reader  [x] Read articles       [ ] Mark read / star
  Later   [x] Read saved pages    [ ] Save / archive

A token that isn't used for 90 days is removed automatically.
Changing your password also removes it.

                                  [ Cancel ]  [ Create token ]
```

The boxes reuse `ScopeGroups` and the consent screen's markup, so the
wording matches. The consent screen has no rule linking a write box to
its read box, and neither does this form: a write-only choice is allowed,
the same as at consent.

**Show-once page** (the POST's response, not a redirect, so the token is
never in a URL or in browser history as a page you can go back to):

```
Your token is ready

Copy it now. ON Suite won't show it again.

[ onst_3q2Vb...................................... ] [ Copy ]

To use it, add a custom MCP server in the app:
  Server address   https://on.example.com/mcp
  Header           Authorization: Bearer <your token>

A token that isn't used for 90 days is removed automatically.

                                                     [ Done ]
```

*(My call.)* Refreshing this page would re-send the form and create a
second token. To stop that, the create form carries a one-use nonce (a
random value stored in the user's web session when the form is shown,
cleared when it's used). A POST with a missing or used nonce sends the
user back to Connected assistants with no new token. The 10-token limit is
a second safety net.

The response must be sent with `Cache-Control: no-store`. **Done** goes
back to Connected assistants. The server address is the configured public
URL plus `/mcp`.

**The list.** A personal token shows like any other connection, with its
name, permissions, created and last-used dates and **Disconnect**. Where
OAuth connections show the site they send you back to, a personal token
shows **Personal token** instead. Never-used tokens show "Not used yet".

### Admin page

`/admin/ai` lists personal tokens with the others, labelled **Personal
token** in the place of the redirect host. Revoking works unchanged.

### Docs

- `docs/user/ai-assistants.md`: a new section **Apps that can't sign in
  (personal tokens)** that explains creating, copying and pasting a token,
  the 90-day rule, and the password-change rule, using the Pebble ring as
  the example.
- `docs/user/admin.md`: one line under AI access saying personal tokens
  exist, come with AI access, and are swept after 90 days unused; and the
  sweep job's description.
- `changelog.md`: an entry for the release that ships it.

## Security notes

- **The token is a long-lived secret on a phone.** Compared with OAuth
  tokens, a leaked personal token keeps working until it's disconnected or
  goes unused for 90 days. That is the trade Ilia chose for the ring. It is
  limited by: per-app scopes chosen at creation (write boxes start
  unticked), Recent activity showing every call, Disconnect working at
  once, and a password change removing every token.
- **Hashing.** 32 random bytes is far too much to guess, so a fast hash
  (SHA-256) is enough and a slow password hash isn't needed. Lookups go by
  hash with a unique index, so there's no secret comparison to time.
- **Prefix.** `onst_` makes tokens easy to spot in logs and for secret
  scanners, and lets `Verify` route without trying both paths. The token
  must never be logged; the builder should check the request logger and
  the audit record don't print the `Authorization` header.
- **CSRF and auth.** The create form is a normal signed-in POST with the
  existing CSRF check, so a third-party page can't create a token for a
  signed-in user.
- **Rate limits.** `/mcp`'s existing per-token and per-IP limits apply. A
  burst of wrong `onst_` tokens costs one indexed lookup each.

## Testing

- **Store:** create a token grant; a second token for the same user leaves
  the first in place; look it up by hash; Disconnect removes
  the token row; deleting the user removes it; `SetPassword` removes it.
- **Sweep:** a personal token isn't removed by the dead-grant rule; one
  unused for 90 days (from creation) is removed; one used 89 days ago
  stays; one last used 91 days ago is removed. Use a fixed clock.
- **Verify:** a good token gives the right user and scopes; an unknown
  `onst_` token is `ErrInvalidToken`; a storage failure is
  `ErrUnavailable`; a revoked token fails; `last_used_at` is touched at
  most once a minute; OAuth tokens still verify exactly as before.
- **End to end over `/mcp`:** create a token with only `notes:read` and
  `notes:write`; a Notes tool works; a Books tool is refused for the
  missing scope; the call shows in Recent activity under the token's name.
- **Handlers:** the create form needs sign-in and CSRF; an empty name or
  no scopes is refused with the form shown again; the show-once page sends
  `Cache-Control: no-store`; re-sending the same POST (a used nonce)
  creates no second token; the eleventh token is refused; the list labels
  the token **Personal token**.
- **Ownership:** user A's token can't read user B's data (reuse the
  existing ownership test with a personal token).

## Left to the builder

- Exact template structure and CSS, following the existing Account and
  consent pages.
- The grant ID generator (reuse whatever the OAuth store already uses for
  random IDs if there is one).
- The wording of the 10-token limit message.
- Where the one-use nonce lives, if the web session store doesn't suit.
- Whether the last-used "touch" for personal tokens shares code with the
  OAuth path.

## Open questions

None at the moment.
