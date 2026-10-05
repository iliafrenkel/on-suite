-- Reading settings, one row per user; absent means the defaults.
CREATE TABLE later_prefs (
    user_id    INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    font       TEXT    NOT NULL CHECK (font IN ('serif', 'sans')),
    size       INTEGER NOT NULL CHECK (size BETWEEN 1 AND 5),
    width      TEXT    NOT NULL CHECK (width IN ('narrow', 'medium', 'wide')),
    updated_at TEXT    NOT NULL
) STRICT;
