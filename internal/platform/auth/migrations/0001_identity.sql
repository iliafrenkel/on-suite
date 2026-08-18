-- The platform's own schema: who exists, and who is currently logged in.
-- Every app's tables are prefixed with its id; these two are not, because
-- they belong to the platform rather than to any app.

CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    -- username preserves the case the user chose, for display.
    username      TEXT    NOT NULL,
    -- username_fold is the lowercased form and carries the uniqueness
    -- constraint, so "Ilia" and "ilia" cannot both exist.
    username_fold TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    is_admin      INTEGER NOT NULL DEFAULT 0 CHECK (is_admin IN (0, 1)),
    created_at    TEXT    NOT NULL
) STRICT;

CREATE TABLE sessions (
    id         TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TEXT    NOT NULL,
    expires_at TEXT    NOT NULL
) STRICT;

-- Expiry sweeps scan by expires_at; timestamps are RFC 3339 in UTC, which
-- sorts correctly as text.
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);
CREATE INDEX sessions_user_id_idx    ON sessions (user_id);
