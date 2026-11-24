-- internal/platform/auth/migrations/0003_oauth.sql
-- Storage for AI access: the OAuth 2.1 authorization server and the MCP
-- audit log (spec 2026-11-24-ai-access-mcp-design.md). These tables belong
-- to the platform, not to an app, so they carry no app prefix.
--
-- One revocation primitive: every token row hangs off oauth_grants(id) with
-- ON DELETE CASCADE, and a grant hangs off users(id) the same way. Deleting
-- the grant row therefore revokes every code, access and refresh token it
-- issued, and deleting the user (or changing their password, which deletes
-- their grants) does the same. Timestamps are TEXT in db.FormatTime's fixed
-- width (see 0002), so they compare correctly as bytes.

-- Server-side secrets generated on first start: "hmac" signs tokens and
-- "registration" signs dynamic client registration access.
CREATE TABLE oauth_secrets (
    name       TEXT PRIMARY KEY,
    value      BLOB NOT NULL,
    created_at TEXT NOT NULL
) STRICT;

-- Known OAuth clients: dynamically registered ('dcr') or fetched from a
-- client ID metadata document ('cimd').
CREATE TABLE oauth_clients (
    id            TEXT PRIMARY KEY,
    kind          TEXT NOT NULL CHECK (kind IN ('dcr', 'cimd')),
    name          TEXT NOT NULL,
    redirect_uris TEXT NOT NULL,
    data          BLOB NOT NULL,
    created_at    TEXT NOT NULL,
    last_seen_at  TEXT
) STRICT;

-- The library's cache of fetched client ID metadata documents.
CREATE TABLE oauth_cimd (
    client_id  TEXT PRIMARY KEY,
    record     BLOB NOT NULL,
    expires_at TEXT NOT NULL
) STRICT;
CREATE INDEX oauth_cimd_expires_at_idx ON oauth_cimd (expires_at);

-- One row per user-approved connection. id is the library's request ID,
-- which stays the same across code exchange and every refresh.
CREATE TABLE oauth_grants (
    id               TEXT    PRIMARY KEY,
    user_id          INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    client_id        TEXT    NOT NULL,
    client_name      TEXT    NOT NULL,
    redirect_host    TEXT    NOT NULL,
    requested_scopes TEXT    NOT NULL,
    granted_scopes   TEXT    NOT NULL,
    created_at       TEXT    NOT NULL,
    last_used_at     TEXT
) STRICT;
-- Leading column user_id also serves the per-user lookups and the cascade.
CREATE INDEX oauth_grants_user_client_idx ON oauth_grants (user_id, client_id);

-- Authorization codes, PKCE requests, access and refresh tokens, and
-- registration access tokens. The key is (kind, signature), not signature
-- alone: the library stores the auth-code session and the PKCE session
-- under the same signature. grant_id is NULL only for 'registration' rows,
-- which exist before any user approves anything.
CREATE TABLE oauth_sessions (
    signature        TEXT    NOT NULL,
    kind             TEXT    NOT NULL CHECK (kind IN ('code', 'pkce', 'access', 'refresh', 'registration')),
    grant_id         TEXT    REFERENCES oauth_grants (id) ON DELETE CASCADE,
    request_id       TEXT    NOT NULL,
    client_id        TEXT    NOT NULL,
    active           INTEGER NOT NULL DEFAULT 1,
    access_signature TEXT,
    data             BLOB    NOT NULL,
    expires_at       TEXT    NOT NULL,
    PRIMARY KEY (kind, signature),
    CHECK ((kind = 'registration') = (grant_id IS NULL))
) STRICT;
CREATE INDEX oauth_sessions_grant_id_idx  ON oauth_sessions (grant_id);
CREATE INDEX oauth_sessions_request_id_idx ON oauth_sessions (request_id);
CREATE INDEX oauth_sessions_expires_at_idx ON oauth_sessions (expires_at);

-- PKCE challenges already redeemed, kept until they would have expired
-- anyway, so a replayed challenge is refused.
CREATE TABLE oauth_pkce_used (
    client_id  TEXT NOT NULL,
    challenge  TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (client_id, challenge)
) STRICT;
CREATE INDEX oauth_pkce_used_expires_at_idx ON oauth_pkce_used (expires_at);

-- One row per MCP tool call. client_name is text, not a grant reference, so
-- the history survives a disconnect. Deliberately no argument or result
-- columns: the log says who called what and whether it worked, never what
-- the user's data said (spec).
CREATE TABLE mcp_audit (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    client_name TEXT    NOT NULL,
    tool        TEXT    NOT NULL,
    ok          INTEGER NOT NULL,
    error_kind  TEXT    NOT NULL DEFAULT '',
    at          TEXT    NOT NULL
) STRICT;
CREATE INDEX mcp_audit_user_at_idx ON mcp_audit (user_id, at);
