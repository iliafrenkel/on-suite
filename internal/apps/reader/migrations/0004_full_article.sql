-- Why a full-article fetch failed, so the button can say so rather than
-- silently doing nothing. Empty means "no failure recorded", which is also the
-- state of an article nobody has tried to fetch — the two are distinguished by
-- full_fetched_at being NULL.
ALTER TABLE reader_items ADD COLUMN full_error TEXT NOT NULL DEFAULT '';
