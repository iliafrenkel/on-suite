-- Per-user read and starred state.
--
-- The absence of a row means unread. Nothing is written until a user actually
-- reads or stars something, so subscribing to a firehose does not cost a row
-- per user per item — which is the difference between a table that tracks a
-- household's reading and one that mirrors every article ever fetched.
CREATE TABLE reader_item_state (
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    item_id    INTEGER NOT NULL REFERENCES reader_items (id) ON DELETE CASCADE,
    -- NULL means not read. A timestamp rather than a boolean because R7's
    -- stats page wants to know when, and the column costs the same either way.
    read_at    TEXT,
    starred_at TEXT,
    PRIMARY KEY (user_id, item_id)
) STRICT, WITHOUT ROWID;

-- Starred is a small set queried on its own ("show me everything I saved"),
-- so a partial index over just those rows stays tiny.
CREATE INDEX reader_item_state_starred_idx
    ON reader_item_state (user_id, starred_at)
 WHERE starred_at IS NOT NULL;
