-- 0005 gave reader_item_images a source column ('feed' / 'full') so SaveItems
-- and SaveFullArticle each delete only their own links, but the primary key
-- stayed (item_id, url_hash) — plain data, not part of the key. When both
-- sources reference the same image URL for one item (a shared lead image,
-- common enough), the second writer's INSERT ... ON CONFLICT DO NOTHING
-- silently lost, leaving one row tagged with whichever source wrote first.
-- That source's next delete-and-reinsert then removed the only row, orphaning
-- the image for the *other* source — the same permanent 404 the source
-- column was meant to fix, just needing a shared URL to trigger it.
--
-- SQLite cannot alter a primary key in place, so this rebuilds the table with
-- source folded into the key: each source now owns its own row for a given
-- (item_id, url_hash), and one source's delete can never touch the other's
-- row. Existing rows carry over unchanged — a straight copy, since the old
-- (item_id, url_hash) key made every row unique already.
CREATE TABLE reader_item_images_new (
    item_id  INTEGER NOT NULL REFERENCES reader_items (id) ON DELETE CASCADE,
    url_hash TEXT    NOT NULL REFERENCES reader_images (url_hash) ON DELETE CASCADE,
    source   TEXT    NOT NULL DEFAULT 'feed' CHECK (source IN ('feed', 'full')),
    PRIMARY KEY (item_id, url_hash, source)
) STRICT, WITHOUT ROWID;

INSERT INTO reader_item_images_new (item_id, url_hash, source)
SELECT item_id, url_hash, source FROM reader_item_images;

DROP TABLE reader_item_images;
ALTER TABLE reader_item_images_new RENAME TO reader_item_images;

CREATE INDEX reader_item_images_hash_idx ON reader_item_images (url_hash);
