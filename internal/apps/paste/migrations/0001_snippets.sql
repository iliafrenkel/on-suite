-- ON Paste owns exactly one table, prefixed with its app id so it cannot
-- collide with any other app in the single shared database.

CREATE TABLE paste_snippets (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title      TEXT    NOT NULL,
    -- language is a Chroma lexer name, or "" meaning detect from content.
    language   TEXT    NOT NULL,
    body       TEXT    NOT NULL,
    -- share_slug is NULL until the snippet is shared, and is replaced with a
    -- fresh value on each re-share so a revoked link can never come back.
    -- UNIQUE permits many NULLs: SQLite treats NULLs as distinct.
    share_slug TEXT    UNIQUE,
    created_at TEXT    NOT NULL
) STRICT;

-- The list page is the only hot query: a user's snippets, newest first.
CREATE INDEX paste_snippets_user_created_idx
    ON paste_snippets (user_id, created_at DESC);
