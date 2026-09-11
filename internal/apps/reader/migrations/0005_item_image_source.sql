-- Distinguishes a link created by SaveItems (the feed body's own images) from
-- one created by SaveFullArticle (the extracted article's images). Without
-- this, SaveItems' per-poll DELETE+reinsert for the feed body silently wiped
-- the full article's image links too, since both shared one unqualified
-- (item_id, url_hash) table — the full article's images were then freed by
-- the next orphan purge, leaving full_html pointing at 404s with no way to
-- re-fetch.
ALTER TABLE reader_item_images ADD COLUMN source TEXT NOT NULL DEFAULT 'feed';
