-- Tombstones for purged articles (#389).
--
-- The retention purge deletes old read articles, but many feeds keep listing
-- entries for years. Without a record of what was purged, the next poll found
-- the GUID unknown and inserted it again as a brand-new unread article, so
-- every old entry came back once a day. SaveItems skips a GUID listed here.
--
-- last_seen_at is the last time a poll saw the GUID in the feed: bumped by
-- SaveItems when it skips one, and for the whole feed on a 304 (an unchanged
-- body still lists everything it did). PruneTombstones deletes the ones not
-- seen for RetentionAge, so the table stays bounded by what feeds currently
-- carry rather than by everything ever purged.
CREATE TABLE reader_purged_items (
    feed_id      INTEGER NOT NULL REFERENCES reader_feeds (id) ON DELETE CASCADE,
    guid         TEXT    NOT NULL,
    last_seen_at TEXT    NOT NULL,
    PRIMARY KEY (feed_id, guid)
) STRICT;
