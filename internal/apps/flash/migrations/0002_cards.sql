CREATE TABLE flash_cards (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    deck_id    INTEGER NOT NULL REFERENCES flash_decks (id) ON DELETE CASCADE,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    card_type  TEXT    NOT NULL,
    front      TEXT    NOT NULL,
    back       TEXT    NOT NULL DEFAULT '',
    notes      TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL
) STRICT;

CREATE INDEX flash_cards_deck_created_idx
    ON flash_cards (deck_id, created_at DESC);
