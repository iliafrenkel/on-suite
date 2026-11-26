-- internal/platform/auth/migrations/0006_personal_tokens.sql
-- Personal tokens for AI access (spec 2026-11-26-personal-tokens-design.md):
-- long-lived bearer tokens a user creates for clients that can't do OAuth.
--
-- One hashed secret per personal-token grant. The grant row carries the
-- name, scopes, owner, created and last-used times like any other
-- connection, so the list, Disconnect, audit, admin view and
-- password-change cleanup need no changes. Deleting the grant (or the user)
-- deletes the token.
CREATE TABLE oauth_personal_tokens (
    grant_id   TEXT PRIMARY KEY REFERENCES oauth_grants (id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE
) STRICT;
