-- Media (images/audio) attached to a card. Keyed by a hash of either the
-- source URL (import-time, fetched lazily on first view — see media_fetch.go)
-- or the uploaded bytes themselves (manual upload, bytes present immediately).
-- A shared table for both kinds, differentiated by `kind`, the same way a
-- card's own card_type differentiates basic from cloze.
CREATE TABLE flash_media (
    hash         TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,
    -- content_type is what sniffing decided, never what a server or upload
    -- claimed.
    content_type TEXT NOT NULL DEFAULT '',
    -- bytes is NULL until fetched (URL-attached) or is populated immediately
    -- at upload time (manually-attached). NULL is exactly "not yet fetched".
    bytes        BLOB,
    -- source_url is set only for URL-attached media; NULL for an upload,
    -- which never needs fetching.
    source_url   TEXT,
    fetched_at   TEXT,
    last_error   TEXT NOT NULL DEFAULT '',
    error_count  INTEGER NOT NULL DEFAULT 0
) STRICT;

ALTER TABLE flash_cards ADD COLUMN image_hash TEXT REFERENCES flash_media (hash);
ALTER TABLE flash_cards ADD COLUMN audio_hash TEXT REFERENCES flash_media (hash);
