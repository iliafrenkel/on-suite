# AI access (MCP) — design

- **Status:** draft for review, 2026-11-24 (decisions agreed in conversation; awaiting approval of this doc)
- **Tracking:** not yet filed

## Why

Ilia wants to talk to ON Suite through an AI assistant, ideally in the
voice mode of the Claude, ChatGPT and Gemini mobile apps (and desktop apps
when they get voice). Example asks: "What book should I read next?",
"I'm on page 212 of Dune", "Add a note to my Groceries list", "What did
that article about solid-state batteries say?", "Summarise the last thing
I saved to Later".

ON Suite does **not** run an AI itself. The assistant does the reasoning,
summarising and answering. ON Suite only hands it the user's data and a
small set of actions. That means there are no model API keys, no cost
on the server, and no new AI dependency.

Security is the main concern. The suite is on a public server and holds
private household data, so access must sit behind real authentication
and per-user, per-app authorisation that the user grants and can revoke.

## Goals

- A remote MCP server, reachable at `<public-url>/mcp`, that works with the
  Claude, ChatGPT and Gemini apps (web, desktop and mobile, including voice
  where the client supports tools in voice).
- Every household member can connect their own assistant and sees only
  their own data.
- Proper OAuth 2.1, using a maintained library rather than our own OAuth
  code (Ilia's decision).
- The user chooses which apps, and read or write per app, at consent time,
  and can see and revoke every connection later.
- Apps in scope: Books, Notes, Reader, Later.

## Non-goals

- **No Focus tools.** Starting a timer would need server-side running
  state. Ilia is happy to leave Focus out.
- No Paste or Flash tools in this round. The pattern makes them easy to add
  later.
- No LLM inside ON Suite (no "Summarise" button, no embeddings or RAG).
- No delete tools of any kind.
- No public client SDK, REST API or CLI. MCP is the only new surface.

## Anti-goals (failure even if it "works")

- One user's assistant can read or change another user's data.
- Anyone on the internet can get a token without a household member
  logging in and consenting in ON Suite's own UI.
- A malicious web page in Reader or Later can steer the assistant into
  changing data the user didn't ask to change (prompt injection).
- Tokens that can't be revoked, or that live in the database in a usable
  form.
- The feature is on by default for an operator who never asked for it.

## Approaches considered

1. **Remote MCP server with ON Suite as its own OAuth server (chosen).**
   It is the standard the three target assistants support for custom
   connectors, and ON Suite already owns users and login, so login and
   consent stay in our UI.
2. **REST/JSON API plus a skill or CLI wrapper.** It is more general, but
   phone assistants can't use a CLI, and we would still need MCP on top.
   Rejected.
3. **AI built into ON Suite** (buttons that call a model). Opposite
   direction: it needs keys and adds cost, and it doesn't help with voice
   from the phone. Rejected.

For OAuth, hand-rolled code was considered and rejected by Ilia. Library
options:

| Library | Status (Nov 2026) | Verdict |
|---|---|---|
| `github.com/ory/fosite` | last release v0.49.0, Dec 2024 | Stale; no DCR, CIMD or RFC 8707 |
| `github.com/zitadel/oidc/v3` | active (v3.53.0) | OIDC-first; DCR belongs to the Zitadel server, not the library |
| **`authelia.com/provider/oauth2`** | active (v0.4.5, 21 Nov 2026), Apache-2.0, hard fork of fosite | **Chosen.** Implements RFC 7636 PKCE, 7591/7592 DCR, 8414 AS metadata, 8707 resource indicators, 9728 protected resource metadata, 7009 revocation, the RFC 9700 security BCP, and Client ID Metadata Documents (draft-03, opt-in, default resolver in `handler/cimd`) |

For the MCP protocol: **`github.com/modelcontextprotocol/go-sdk`** (v1.8.0,
official). We use its server and Streamable HTTP handler. Bearer-token
checks are wired to our own verifier backed by the OAuth library's storage.

**Risk to note:** the Authelia library is v0.x, so its API may change
between minor versions. We pin it, keep it behind one package (see "Arch
containment") and upgrade deliberately.

## Decisions at a glance

| Topic | Decision |
|---|---|
| Transport | MCP Streamable HTTP at `/mcp`. No SSE transport (Gemini Enterprise and newer clients don't use it) |
| Who issues tokens | ON Suite, as its own OAuth 2.1 authorization server. Issuer = the public URL |
| Grant types | `authorization_code` with PKCE S256 (required, `plain` disabled) and `refresh_token`. Nothing else |
| Client registration | Both CIMD (Claude and ChatGPT prefer it) and RFC 7591 DCR (Gemini, fallback). Public clients only (`token_endpoint_auth_method=none`) |
| Redirect URI safety | An **exact-match redirect URI allowlist** set by the operator. Registration or CIMD with any other redirect URI is refused. This closes the open-registration phishing hole |
| Audience | RFC 8707: tokens are bound to the resource `<public-url>/mcp` and refused anywhere else |
| Token format | Opaque (HMAC strategy). Only signatures and hashes are stored, never usable tokens |
| Lifetimes | Auth code 1 min; access token 1 h; refresh token 30 days, rotated on every use. Reuse of an old refresh token revokes the whole grant |
| Scopes | `books:read books:write notes:read notes:write reader:read reader:write later:read later:write`. **All 8 are advertised in `scopes_supported` from P1**, even before their tools exist, so nobody has to reconnect when P2–P4 ship (Ilia's decision). A scope with no tools yet grants nothing. The consent screen shows them grouped per app |
| Default ticks | On the consent screen, requested **read** scopes come ticked and requested **write** scopes come unticked. The user opts into writes. A request with no `scope` is treated as all four read scopes (Ilia's decision) |
| Consent | Always shown for a new grant and for scope increases. Shows the client name, its redirect host and the scopes |
| Users | Every user can connect |
| On/off | Off by default. Enabled with `ONSUITE_MCP=on`, and needs `ONSUITE_PUBLIC_URL`. Env-only, like every other setting (no runtime toggle, no settings table). With it off, the `/oauth/*`, `/mcp` and well-known routes aren't mounted, and existing grants stay dormant until it is turned back on. The admin can still revoke them from `/admin` |
| Clients | Cloud-hosted assistants only (Claude, ChatGPT, Gemini connectors), all with fixed HTTPS callbacks. Loopback-callback clients (Claude Code, MCP Inspector, `http://localhost:<port>`) are **out of scope**, because they would need port-agnostic loopback matching that breaks the exact-match rule |
| Account changes | A password change (by the user, the admin, or the CLI reset) and user deletion revoke **all** the user's grants, like sessions today. The `/account` page then shows no connected assistants, so the user sees the effect |
| Management | "Connected assistants" on `/account` (list, scopes, last used, revoke). An admin section on `/admin` (all grants, revoke, allowlist view) |
| Audit | A table of every tool call (user, client, tool, ok/error, time), shown on `/account`, kept 90 days |
| Rate limits | Per-IP limits on `/oauth/*`; per-grant limits on `/mcp` |
| Deletes | None. Every write is small and reversible in the UI |
| Untrusted text | Article text from Reader and Later goes back clearly marked as third-party content |

## Architecture

### New packages

- `internal/platform/oauth` is the only importer of
  `authelia.com/provider/oauth2`. It wraps a configured provider and owns:
  - its SQLite storage implementation
  - the HTTP handlers for `/.well-known/oauth-authorization-server`,
    `/oauth/authorize` (GET shows consent, POST decides), `/oauth/token`,
    `/oauth/register` and `/oauth/revoke`
  - the CIMD resolver config
  - a `Verify(ctx, bearer) (Grant, error)` function that `/mcp` uses
- `internal/platform/mcp` is the only importer of
  `github.com/modelcontextprotocol/go-sdk`. It:
  - builds the MCP server from tools contributed by apps
  - serves `/mcp` through the SDK's Streamable HTTP handler, wrapped in a
    bearer middleware that calls `oauth.Verify`
  - serves `/.well-known/oauth-protected-resource` (RFC 9728)
  - checks each tool's scope
  - writes the audit row

### How apps contribute tools (no cross-app or platform→app imports)

Following "App boundary via `app.App` + `Deps`", a new optional interface
lives in `internal/platform/app`:

```go
// ToolProvider is implemented by apps that expose MCP tools.
type ToolProvider interface {
    Tools() []Tool
}

type Tool struct {
    Name        string          // "books_list", prefixed with the app ID
    Title       string
    Description string          // written for the model: when to use it, what it returns
    Scope       string          // "books:read" or "books:write"
    ReadOnly    bool            // becomes the readOnlyHint annotation
    Input       any             // zero value of the args struct; the platform derives the JSON schema
    Call        func(ctx context.Context, userID int64, args json.RawMessage) (Result, error)
}
```

Apps never import the MCP SDK. The platform discovers `ToolProvider` by
type assertion, as it does for `Exporter`, `Scheduler` and `Stater`. A
tool's `Call` always receives the verified user ID from the token, never
from the arguments.

### Arch containment

Extend `internal/arch/arch_test.go` following `TestReadabilityIsContained`:

- `authelia.com/provider/oauth2/...` is imported only by
  `internal/platform/oauth`.
- `github.com/modelcontextprotocol/go-sdk/...` is imported only by
  `internal/platform/mcp`.
- `TestLayering`: `oauth` doesn't import `mcp`, and apps import neither.

### Wiring and existing middleware

- **CSRF.** `/oauth/token`, `/oauth/register`, `/oauth/revoke` and `/mcp`
  are called by other servers with no cookie, so they are exempt from the
  double-submit CSRF check. `/oauth/authorize` POST (the consent decision)
  stays CSRF-protected because it is a logged-in browser form.
- **CSP.** The global `form-action 'self'` blocks the browser from
  following the consent POST's redirect to `claude.ai` and the other
  assistants. The consent route sets its own CSP with `form-action 'self'`
  plus the origin of that request's validated redirect URI. All other
  routes keep the global policy.
- **Login.** `/oauth/authorize` uses `LoadUser`. With no user it redirects
  to `/login?next=<the full authorize URL>`. The existing `next` handling
  must accept that path with its query string (same-origin only).
- **Body limits.** `/mcp` and `/oauth/register` get explicit
  `RegisterBodyLimit` entries (proposal: 256 KiB and 16 KiB).
- **Public URL.** New `ONSUITE_PUBLIC_URL` config (e.g.
  `https://on.example.com`). The issuer, resource and metadata URLs come
  from it, never from request headers. Startup fails if `ONSUITE_MCP=on`
  without it, or if it isn't `https://` (except `http://localhost` for
  development).
- **HSTS.** Not set today. Add `Strict-Transport-Security` when
  `TLS_DOMAIN` or an `https` public URL is configured, since tokens now
  cross the wire. (Small, separate commit.)

### Storage (platform migrations, namespace `platform`)

New migration `0003_oauth.sql` in `internal/platform/auth/migrations` (or a
new `oauth` namespace; the builder decides). Tables, in outline:

- `oauth_clients`: client ID, kind (`dcr` or `cimd`), name, redirect URIs,
  metadata JSON, created_at, last_seen_at. CIMD rows are a cache of
  fetched documents, refreshed per the library's cache policy.
- `oauth_grants`: one per (user, client), with granted scopes, created_at,
  last_used_at and revoked_at. This is what the user sees and revokes.
- `oauth_codes`, `oauth_access_tokens`, `oauth_refresh_tokens`: the
  library's request/session records, keyed by token **signature**, each
  linked to a grant. Revoking a grant deletes all of them.
- `mcp_audit`: id, user_id, grant_id, tool, ok, error_kind, at. **No
  arguments or results are stored**, because they may hold private text.

All timestamps go through `db.FormatTime` (pattern "Stored timestamps
through `db.FormatTime`"). User deletion cascades to grants, tokens and
audit rows.

Grant revocation on a password change is wired the same way sessions are
cleared today (`SetPassword` in `internal/platform/auth/manage.go`). Since
`auth` must not import `oauth`, it goes through an injected func (pattern
"Cross a layering boundary with an injected func, not an import"), or by
deleting the user's `oauth_grants` rows in the same transaction. The
builder picks one.

A maintenance job beside the session sweep (`registerMaintenance`)
deletes expired codes and tokens and audit rows older than 90 days.

### Redirect URI allowlist

`ONSUITE_OAUTH_REDIRECT_URIS` is a comma-separated list of **exact**
redirect URIs. The defaults ship with the known callbacks for Claude and
ChatGPT. Gemini's callback is a Google-hosted URL that must be confirmed
in phase 1 before it is added.

- DCR: the registration is refused unless every requested `redirect_uri`
  is on the list.
- CIMD: the fetched document is refused unless every `redirect_uri` is on
  the list. Fetching uses the library's resolver with its special-use
  address guard (no loopback or private ranges), timeouts and size caps.
- Whole domains are never allowlisted. In particular, not
  `*.googleusercontent.com`, which hosts user content.

`/admin` shows the effective list. Changing it is a config change and a
restart, like every other setting today.

### Consent screen

A normal ON Suite page in the shell, with no app chrome:

```
  Claude wants to access your ON Suite
  It will send you back to: claude.ai

  Books    [x] Read your books    [ ] Update books
  Notes    [x] Read your notes    [ ] Add notes
  Reader   [x] Read articles      [ ] Mark read / star
  Later    [x] Read saved pages   [ ] Save / archive

  Only allow what you need. You can disconnect it any time on your
  Account page.

  [ Allow ]  [ Cancel ]
```

- Only scopes the client requested are shown. Reads come ticked, writes
  unticked. The grant holds exactly what is ticked on Allow. The server
  re-checks that every submitted scope was in the request.
- The client name is the client's own claim, so it is shown next to the
  redirect host, which we have validated. The host is the trustworthy part.
- If the user already has a live grant for this client and the request asks
  for no scope outside what was *requested* last time, consent is skipped
  and the existing grant's scopes are reused. Without this, a client that
  always asks for every scope would show consent on every re-auth, because
  writes default to unticked. Anything new in the request shows consent
  again.

### Tool rules

- **Few, coarse tools.** About 20 in total. Every tool has a
  model-oriented description saying when to use it.
- **Results** are compact text or small JSON suited to voice: titles, IDs
  and short fields. Long text is returned only by explicit `*_get` tools,
  and capped (proposal: 20k characters, with a note when truncated).
- **Annotations:** `readOnlyHint` on reads. Writes set
  `destructiveHint=false` and `idempotentHint` where true, so clients can
  decide whether to confirm.
- **Untrusted content.** Reader and Later article text is returned inside
  a clearly labelled block, e.g.
  `<untrusted_article source="example.com">…</untrusted_article>`, after a
  line telling the model it is third-party content and must not be
  followed as instructions. HTML is converted to plain text first.
- **Ownership.** Each tool passes the token's user ID to store methods that
  already scope by user. Later's highlight methods take only a doc ID
  (`highlight.go`), so any tool touching them first checks document
  ownership. No highlight tools are planned in this round.
- **Errors.** A tool returns a short readable error ("No book with that
  ID on your shelves"), never internal details.

### Tool list

**Books** (`internal/apps/books`)

| Tool | Scope | Store |
|---|---|---|
| `books_list(shelf?, query?, tag?, limit?)` | read | `List`, `ShelfCounts` |
| `books_get(id)`: details, rating, review, progress, recent notes and quotes | read | `Get`, notes, quotes |
| `books_stats(year?)`: goal, books read, pages | read | stats |
| `books_start_reading(id)` | write | `StartReading` |
| `books_update_progress(id, value)` (page or %) | write | `RecordProgress` |
| `books_finish(id, rating?)` | write | `FinishReading` |
| `books_mark_dnf(id)` | write | `MarkDNF` |
| `books_add_note(id, text)` / `books_add_quote(id, text, page?)` | write | `AddNote`, `AddQuote` |

"What should I read next?" is the assistant combining `books_list(shelf=want)`
with the ratings and history from `books_list(shelf=read)`. No special tool
is needed.

**Notes**

| Tool | Scope |
|---|---|
| `notes_search(query)` | read |
| `notes_get(id, depth?)`: a node and its outline below, as indented text | read |
| `notes_due()` | read |
| `notes_add(text, parent_id?)`: as the last child of the parent, or at top level | write |
| `notes_set_done(id, done)` | write |

**Reader**

| Tool | Scope |
|---|---|
| `reader_list(filter=unread\|starred, feed?, limit?)` | read |
| `reader_search(query)` | read |
| `reader_get_article(id)` | read |
| `reader_mark_read(id, read)` / `reader_star(id, starred)` | write |

**Later**

| Tool | Scope |
|---|---|
| `later_list(state=unread\|reading\|archived, tag?, limit?)` | read |
| `later_search(query)` | read |
| `later_get_article(id)` | read |
| `later_save(url)`: uses the existing save/fetch path | write |
| `later_set_state(id, state)` | write |

The exact argument names and result shapes are left to each phase's plan.

### Account and admin UI

- **`/account` → "Connected assistants".** For each grant: client name,
  redirect host, scopes per app, connected date, last used, and a
  "Disconnect" button (CSRF-protected POST that revokes the grant and its
  tokens). Below that, "Recent activity": the last 50 audit rows (time,
  assistant, tool, ok/error).
- **`/admin` → "AI access" section.** Whether it is on, the public URL,
  the effective redirect allowlist, and every grant across users with a
  revoke action (admin only).

### Rate limits

A new small in-memory limiter, reusing the `AttemptLimiter` style:

- `/oauth/register`: 10/hour per IP.
- `/oauth/token`, `/oauth/authorize`: 60/min per IP.
- `/mcp`: 120/min per grant.

Over the limit returns 429 with `Retry-After`. Proposed numbers; the
builder may tune them.

## Phases

Each phase is its own plan and PR.

1. **P1 — Platform + Books read.**
   - Everything under "Architecture": OAuth, consent, `/mcp`, storage,
     allowlist, CSRF/CSP wiring, rate limits, audit, the `/account`
     "Connected assistants" section with Disconnect, the `/admin` section,
     arch tests and HSTS.
   - Tools: `books_list`, `books_get`, `books_stats`.
   - **Exit gate (manual, by Ilia after deploy):** connect Claude, ChatGPT
     and Gemini. In each, ask "what book should I read next?", then
     disconnect and confirm the assistant loses access. Record which clients
     work, and the confirmed Gemini redirect URI, in the PR. Voice mode is
     out of our control and isn't part of the gate; Ilia tries it once the
     MCP server is ready.
2. **P2 — Books write tools.**
3. **P3 — Notes tools.**
4. **P4 — Reader and Later tools**, with the untrusted-content wrapping.

After P1, P2–P4 are mostly per-app work on an established pattern.

## Testing

- **OAuth flow end to end** in Go tests against a real SQLite file
  (pattern "Real-SQLite-file store tests"):
  - DCR → authorize → consent → token → `/mcp` call → refresh → revoke.
  - CIMD with a stub document server.
- **Negative cases**, each its own test:
  - missing or wrong PKCE; `plain` PKCE
  - redirect URI not on the allowlist (both DCR and CIMD)
  - wrong resource or audience
  - an expired code; a code used twice
  - a reused refresh token revokes the grant
  - a revoked grant gets 401 at `/mcp`
  - a token used for a scope it lacks
  - user A's token cannot read user B's data
  - CSRF still enforced on the consent POST
  - CIMD URLs resolving to private or loopback addresses refused
- **MCP:** use the Go SDK's client in tests to list tools and call each
  one. Check the scope filter (tools outside the grant's scopes are
  hidden from `tools/list` and refused on call).
- **Arch tests** as above.
- **Manual:** the phase 1 exit gate.

## Security review

P1 gets a dedicated `/security-review` pass before review by Ilia, on
top of the normal code review.

## Left to the builder

- Exact table columns and indexes, and whether OAuth tables use the
  `platform` namespace or a new one.
- How the Authelia provider is composed (which `compose` factories) and
  the shape of the storage adapter.
- JSON schema derivation for `Tool.Input` (via the SDK's schema support
  inside `internal/platform/mcp`).
- Exact rate-limit numbers and the truncation cap.
- Tool argument names and result formats per phase.
- Consent page styling within Braun Folio.

## Open questions

- **Gemini.** Developers report its custom-MCP OAuth flow stalling after the
  callback. If it still fails at P1, we ship for Claude and ChatGPT and
  revisit Gemini later.
