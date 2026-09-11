-- The image proxy's cache.
--
-- Keyed by a hash of the source URL rather than by item, so rewriting can
-- happen in parse.go before any row exists — see the R3 plan. One consequence
-- worth having: an image reused across articles is stored once.
CREATE TABLE reader_images (
    url_hash     TEXT PRIMARY KEY,
    src_url      TEXT NOT NULL,
    -- content_type is what sniffing decided, never what the server claimed.
    content_type TEXT NOT NULL DEFAULT '',
    -- bytes is NULL until the image is first requested: fetching every image
    -- of every article at poll time would download a great deal nobody looks
    -- at.
    bytes        BLOB,
    fetched_at   TEXT,
    last_error   TEXT NOT NULL DEFAULT '',
    error_count  INTEGER NOT NULL DEFAULT 0
) STRICT;

-- Which items reference which images. This is what lets retention free an
-- image once the last article using it is gone.
CREATE TABLE reader_item_images (
    item_id  INTEGER NOT NULL REFERENCES reader_items (id) ON DELETE CASCADE,
    url_hash TEXT    NOT NULL REFERENCES reader_images (url_hash) ON DELETE CASCADE,
    PRIMARY KEY (item_id, url_hash)
) STRICT, WITHOUT ROWID;

CREATE INDEX reader_item_images_hash_idx ON reader_item_images (url_hash);
