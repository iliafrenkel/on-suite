-- The home page's view and sort, one row per user (UI redesign spec
-- "Preferences"). No row means the defaults: grid, Default, not reversed.
CREATE TABLE books_prefs (
    user_id    INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    view       TEXT    NOT NULL CHECK (view IN ('grid', 'list')),
    sort       TEXT    NOT NULL CHECK (sort IN ('default', 'added', 'title', 'author')),
    reversed   INTEGER NOT NULL CHECK (reversed IN (0, 1)),
    updated_at TEXT    NOT NULL
) STRICT;
