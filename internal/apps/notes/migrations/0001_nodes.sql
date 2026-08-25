-- ON Notes owns exactly one table, prefixed with its app id so it cannot
-- collide with any other app in the single shared database.
--
-- done_at, due_on, archived_at and share_slug are deliberately absent. Each
-- arrives with the chunk that uses it, in its own forward-only migration, so
-- a chunk is a self-contained change rather than a schema that pretends to
-- know what later chunks will need.

CREATE TABLE notes_nodes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users (id)       ON DELETE CASCADE,
    -- NULL means top level. The self-reference deletes a subtree for free,
    -- because the platform opens SQLite with foreign_keys=ON.
    parent_id  INTEGER          REFERENCES notes_nodes (id) ON DELETE CASCADE,
    -- Contiguous within a parent: 0..n-1, no gaps. Invariant I1 in the spec.
    position   INTEGER NOT NULL,
    title      TEXT    NOT NULL,
    -- The secondary line under the bullet. "" when absent, never NULL.
    note       TEXT    NOT NULL,
    collapsed  INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
) STRICT;

-- Every read is some form of "this user's children of this parent, in order".
CREATE INDEX notes_nodes_user_parent_pos_idx
    ON notes_nodes (user_id, parent_id, position);
