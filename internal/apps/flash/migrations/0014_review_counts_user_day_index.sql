-- internal/apps/flash/migrations/0014_review_counts_user_day_index.sql
-- flash_review_counts' primary key is (user_id, deck_id, day), so a query
-- across all of a user's decks — Streak, RetentionRate, DailyReviewCounts,
-- TodayTally without a deck — can seek only on user_id and then reads every
-- day that user has ever reviewed (#365). One row per deck per day grows
-- linearly with use. This index lets those range-seek (user_id, day) to the
-- window they need, and lets Streak walk days newest-first and stop at the
-- first gap. Per-deck lookups keep using the primary key.
CREATE INDEX flash_review_counts_user_day_idx ON flash_review_counts (user_id, day);
