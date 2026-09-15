-- Feed favicons: shown in the sidebar tree next to each feed's title.
--
-- favicon_url lives on reader_feeds (shared across subscribers, like title
-- and site_url) rather than reader_subs, since it is a property of the feed
-- itself, not of one person's subscription to it.
ALTER TABLE reader_feeds ADD COLUMN favicon_url TEXT NOT NULL DEFAULT '';

-- The favicon proxy's cache, deliberately separate from reader_images even
-- though the shape is nearly identical: reader_images has a retention job
-- (see recordItemImages / the DELETE in store.go) that frees any row with no
-- reader_item_images link, and a favicon would never have one — it would be
-- swept away almost immediately. Kept as its own table so that cleanup never
-- touches favicons, and a favicon never needs one of its own: the row count
-- here is naturally bounded by "one per distinct favicon URL ever seen",
-- which is tiny for a household reader.
CREATE TABLE reader_feed_icons (
    url_hash     TEXT PRIMARY KEY,
    src_url      TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT '',
    bytes        BLOB,
    fetched_at   TEXT,
    last_error   TEXT NOT NULL DEFAULT '',
    error_count  INTEGER NOT NULL DEFAULT 0
) STRICT;
