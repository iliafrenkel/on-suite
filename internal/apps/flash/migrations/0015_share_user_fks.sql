-- from_user_id and to_user_id had no foreign key at all, so deleting a user
-- (now possible via auth.Store.DeleteUser, #310) left their share rows
-- behind as orphans: the sender's "Shared with" list then rendered a row
-- for a recipient who no longer exists, with a blank username. Before this
-- branch no user could ever be deleted, so the gap was unreachable.
--
-- Same table-rebuild as 0008_share_delete_fk.sql: SQLite can't ALTER a
-- column to add a foreign key, so recreate the table with the columns
-- referencing users(id) ON DELETE CASCADE, copy the data across (skipping
-- any row that somehow references a user that no longer exists — there
-- shouldn't be any, but the copy must not fail the migration if one turns
-- up), drop the old table, and rename the new one into place.
-- defer_foreign_keys postpones FK checking to commit time so this works
-- inside the single transaction db.Apply wraps each migration in.
--
-- A table rebuild drops every index and trigger on the table, so the two
-- indexes 0007/0011 never actually added on flash_shares don't need
-- recreating (there are none — flash_shares has no CREATE INDEX or CREATE
-- TRIGGER anywhere in this app's migrations); if a later migration adds one
-- it must be re-added here or in a follow-up rebuild.
PRAGMA defer_foreign_keys = ON;

CREATE TABLE flash_shares_new (
    id              INTEGER PRIMARY KEY,
    deck_id         INTEGER NOT NULL REFERENCES flash_decks (id) ON DELETE CASCADE,
    from_user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    to_user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status          TEXT NOT NULL CHECK (status IN ('pending', 'adopted', 'declined', 'revoked')),
    -- adopted_deck_id is set only once this row is adopted: the recipient's
    -- own independent copy. NULL until then, and NULL again if the
    -- recipient later deletes that copy.
    adopted_deck_id INTEGER REFERENCES flash_decks (id) ON DELETE SET NULL,
    created_at      TEXT NOT NULL,
    responded_at    TEXT
) STRICT;

INSERT INTO flash_shares_new (id, deck_id, from_user_id, to_user_id, status, adopted_deck_id, created_at, responded_at)
SELECT id, deck_id, from_user_id, to_user_id, status, adopted_deck_id, created_at, responded_at
FROM flash_shares
WHERE from_user_id IN (SELECT id FROM users) AND to_user_id IN (SELECT id FROM users);

DROP TABLE flash_shares;

ALTER TABLE flash_shares_new RENAME TO flash_shares;
