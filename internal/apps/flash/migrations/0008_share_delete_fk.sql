-- 0007_sharing.sql left adopted_deck_id's FK with the default ON DELETE
-- NO ACTION: once a recipient adopts a share, that row's adopted_deck_id
-- points at their new deck, and their own later DeleteDeck on that copy then
-- fails with a FOREIGN KEY constraint error (Flash's handle runs with
-- PRAGMA foreign_keys=1 — see internal/platform/db/db.go). The design spec
-- requires deleting an adopted deck to just work, so this fixes the FK to
-- ON DELETE SET NULL: the share row survives (status stays "adopted", for
-- history), it just loses the back-reference to a deck that's gone.
--
-- SQLite can't ALTER a column's FK action directly, so this is the standard
-- table-rebuild: recreate with the corrected schema, copy the data across,
-- drop the old table, rename the new one into place. defer_foreign_keys
-- postpones FK checking to commit time so this works inside the single
-- transaction db.Apply wraps each migration in.
PRAGMA defer_foreign_keys = ON;

CREATE TABLE flash_shares_new (
    id              INTEGER PRIMARY KEY,
    deck_id         INTEGER NOT NULL REFERENCES flash_decks (id) ON DELETE CASCADE,
    from_user_id    INTEGER NOT NULL,
    to_user_id      INTEGER NOT NULL,
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
FROM flash_shares;

DROP TABLE flash_shares;

ALTER TABLE flash_shares_new RENAME TO flash_shares;
