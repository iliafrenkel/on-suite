-- internal/apps/flash/migrations/0010_deck_color.sql
-- A deck's colour, picked by its owner from a fixed palette of eight names
-- (deck.go's DeckColors). Stored as the name, not a hex value: the name is
-- what templates turn into a deck-c-<name> class, and app.css owns what each
-- name looks like in light and dark mode — see the UI overhaul spec §1.2.
-- Existing decks become teal, the palette's first entry.
ALTER TABLE flash_decks ADD COLUMN color TEXT NOT NULL DEFAULT 'teal';
