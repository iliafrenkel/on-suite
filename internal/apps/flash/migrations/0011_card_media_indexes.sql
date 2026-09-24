-- internal/apps/flash/migrations/0011_card_media_indexes.sql
-- Indexes on the two columns that reference flash_media (#302). Three
-- lookups go from a hash to the cards that use it: the media route's "does
-- one of this viewer's cards use it" check, PurgeOrphanMedia's NOT EXISTS,
-- and SQLite's own foreign-key check on every flash_media row deleted,
-- which searches flash_cards for a child row — without an index, a full
-- table scan per deleted row.
--
-- Partial, because most cards have no media and a NULL is never looked up.
-- SQLite still uses a partial index for any `col = ?` term, since that
-- implies `col IS NOT NULL`.
CREATE INDEX flash_cards_image_hash_idx ON flash_cards (image_hash) WHERE image_hash IS NOT NULL;
CREATE INDEX flash_cards_audio_hash_idx ON flash_cards (audio_hash) WHERE audio_hash IS NOT NULL;
