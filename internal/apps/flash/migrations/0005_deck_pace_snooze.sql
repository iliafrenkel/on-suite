ALTER TABLE flash_decks ADD COLUMN new_cards_per_day INTEGER NOT NULL DEFAULT 20;
ALTER TABLE flash_decks ADD COLUMN reviews_per_day INTEGER;   -- NULL = unlimited
ALTER TABLE flash_decks ADD COLUMN snoozed_until TEXT;        -- NULL = not snoozed
