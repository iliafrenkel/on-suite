CREATE TABLE flash_decks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    created_at  TEXT    NOT NULL
) STRICT;

CREATE INDEX flash_decks_user_created_idx
    ON flash_decks (user_id, created_at DESC);

-- A user cannot have two decks with the same name; this is what turns a
-- second CreateDeck("Spanish", ...) into a friendly ErrInvalid instead of a
-- silent duplicate the list page cannot tell apart.
CREATE UNIQUE INDEX flash_decks_user_name_idx
    ON flash_decks (user_id, name);
