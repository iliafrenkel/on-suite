-- One row per share offer from one account's deck to another. A deck can
-- have several simultaneous rows (one per recipient); a re-share to a
-- recipient who already adopted inserts a second pending row for the same
-- (deck_id, from_user_id, to_user_id) triple, which is what a merge offer
-- looks like — see share.go's AdoptShare for how the two are told apart.
CREATE TABLE flash_shares (
    id              INTEGER PRIMARY KEY,
    deck_id         INTEGER NOT NULL REFERENCES flash_decks (id) ON DELETE CASCADE,
    from_user_id    INTEGER NOT NULL,
    to_user_id      INTEGER NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('pending', 'adopted', 'declined', 'revoked')),
    -- adopted_deck_id is set only once this row is adopted: the recipient's
    -- own independent copy. NULL until then.
    adopted_deck_id INTEGER REFERENCES flash_decks (id),
    created_at      TEXT NOT NULL,
    responded_at    TEXT
) STRICT;

-- origin_card_id names the source card a copied card came from, set by
-- adoption/merge. ON DELETE SET NULL: deleting the original later never
-- touches the adopter's own copy, it only loses the back-reference.
ALTER TABLE flash_cards ADD COLUMN origin_card_id INTEGER REFERENCES flash_cards (id) ON DELETE SET NULL;
