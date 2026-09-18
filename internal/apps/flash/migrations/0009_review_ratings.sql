-- Four per-rating counters alongside flash_review_counts' existing
-- new_count/review_count, bumped on every grade (and un-bumped on undo) so
-- retention rate — the fraction of reviews NOT graded Again — can be
-- computed for any window without a separate per-review event log. Rows
-- written before this migration simply have all four at 0, which reads as
-- "no ratings recorded that day" — correct for genuinely idle days, and a
-- harmless, short-lived gap for the handful of pre-migration days given the
-- stats page's 30-day retention window.
ALTER TABLE flash_review_counts ADD COLUMN again_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE flash_review_counts ADD COLUMN hard_count  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE flash_review_counts ADD COLUMN good_count  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE flash_review_counts ADD COLUMN easy_count  INTEGER NOT NULL DEFAULT 0;
