-- ON Reader. Every table is prefixed with the app id so it cannot collide with
-- any other app in the single shared database.
--
-- The split between global and per-user tables is the core of the design: a
-- feed URL is stored and polled once no matter how many people subscribe.

-- Global: one row per feed URL, shared by every subscriber.
CREATE TABLE reader_feeds (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    url            TEXT    NOT NULL UNIQUE,
    -- resolved_url differs from url when the feed redirects; it is what we
    -- actually fetch on subsequent polls.
    resolved_url   TEXT    NOT NULL,
    title          TEXT    NOT NULL,
    site_url       TEXT    NOT NULL,
    -- Conditional GET state. Empty string, not NULL, so the read path never
    -- has to deal with a nullable string.
    etag           TEXT    NOT NULL DEFAULT '',
    last_modified  TEXT    NOT NULL DEFAULT '',
    last_fetch_at  TEXT,
    last_status    INTEGER NOT NULL DEFAULT 0,
    last_error     TEXT    NOT NULL DEFAULT '',
    error_count    INTEGER NOT NULL DEFAULT 0,
    -- next_fetch_at drives due selection. A new feed is due immediately.
    next_fetch_at  TEXT    NOT NULL,
    -- fetch_interval is seconds, or NULL to use the package default.
    fetch_interval INTEGER
) STRICT;

CREATE INDEX reader_feeds_due_idx ON reader_feeds (next_fetch_at);

-- Per user. There is deliberately no parent_id: one level of nesting is
-- enforced by the schema rather than by validation.
CREATE TABLE reader_folders (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id  INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name     TEXT    NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    UNIQUE (user_id, name)
) STRICT;

CREATE TABLE reader_subs (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id   INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    feed_id   INTEGER NOT NULL REFERENCES reader_feeds (id) ON DELETE CASCADE,
    -- Deleting a folder must not delete the subscriptions in it: they fall
    -- back to the root of the tree.
    folder_id INTEGER REFERENCES reader_folders (id) ON DELETE SET NULL,
    -- title overrides the feed's own title when non-empty.
    title     TEXT    NOT NULL DEFAULT '',
    position  INTEGER NOT NULL DEFAULT 0,
    -- added_at is the unread cutoff: items published before a subscription
    -- existed are treated as read, so joining a feed someone else already
    -- follows does not hand you their backlog.
    added_at  TEXT    NOT NULL,
    UNIQUE (user_id, feed_id)
) STRICT;

CREATE INDEX reader_subs_user_folder_idx ON reader_subs (user_id, folder_id);

CREATE TABLE reader_items (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id         INTEGER NOT NULL REFERENCES reader_feeds (id) ON DELETE CASCADE,
    -- guid is the publisher's id, or a fallback derived in parse.go.
    guid            TEXT    NOT NULL,
    url             TEXT    NOT NULL,
    title           TEXT    NOT NULL,
    author          TEXT    NOT NULL DEFAULT '',
    published_at    TEXT    NOT NULL,
    fetched_at      TEXT    NOT NULL,
    summary_html    TEXT    NOT NULL DEFAULT '',
    content_html    TEXT    NOT NULL DEFAULT '',
    full_html       TEXT,
    full_fetched_at TEXT,
    UNIQUE (feed_id, guid)
) STRICT;

-- The list pane's only hot query: a feed's items, newest first.
CREATE INDEX reader_items_feed_published_idx
    ON reader_items (feed_id, published_at DESC);
