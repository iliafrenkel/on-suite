-- R7: one row per user per day, so trends survive retention.
--
-- The articles a 90-day chart needs are exactly the ones retention deletes at
-- 60 days — and it deletes only the *read* ones, so drawing trends from
-- reader_items would show a biased sample (unread and starred articles only)
-- rather than an honest gap. Recording the counts before the purge runs is the
-- only way to answer "is my backlog growing?" at all.
CREATE TABLE reader_daily_stats (
    day     TEXT    NOT NULL,               -- YYYY-MM-DD, UTC
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- fetched counts articles that became visible to this user that day, so it
    -- respects each subscription's added_at cutoff the same way the list does.
    fetched INTEGER NOT NULL DEFAULT 0,
    read    INTEGER NOT NULL DEFAULT 0,
    -- backlog is the unread count at the moment the row was written.
    backlog INTEGER NOT NULL DEFAULT 0,
    -- reconstructed marks a row derived from surviving articles rather than
    -- measured on the day. A reconstructed backlog is only as complete as what
    -- retention left behind, and a chart that silently mixes the two is
    -- confidently wrong rather than merely incomplete.
    reconstructed INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (day, user_id)
) STRICT, WITHOUT ROWID;
