-- R6: full-text search over article titles and bodies.
--
-- search_text holds tag-stripped plain text rather than the stored HTML.
-- FTS5's external-content mode indexes columns of the content table, and every
-- body column here holds sanitized HTML — indexing those directly would make
-- "img" match every article with a picture and turn every linked domain into a
-- search term.
--
-- Existing rows get an empty search_text: stripping HTML is not something SQL
-- can do. The nightly purge job reindexes them in bounded batches, so the index
-- converges within a day of this migration landing.
ALTER TABLE reader_items ADD COLUMN search_text TEXT NOT NULL DEFAULT '';

CREATE VIRTUAL TABLE reader_items_fts USING fts5(
    title, search_text, content='reader_items', content_rowid='id',
    tokenize='unicode61', prefix='2 3 4'
);

-- prefix='2 3 4' is what makes a live filter match while a word is still being
-- typed ("categ" matching "categorically"), the same configuration ON Notes
-- settled on in its own 0006_search_prefix.sql.

-- External-content FTS5 tables are not backfilled automatically, and the rows
-- that exist now have no search_text yet, so this indexes titles only. The
-- reindex pass fills in the bodies.
INSERT INTO reader_items_fts(rowid, title, search_text)
    SELECT id, title, search_text FROM reader_items;

-- Keeps the index in step from here on. The 'delete' special command is FTS5's
-- documented way to remove a row from an external-content index; a plain
-- DELETE FROM the virtual table is not supported for a content= table.
CREATE TRIGGER reader_items_fts_ai AFTER INSERT ON reader_items BEGIN
    INSERT INTO reader_items_fts(rowid, title, search_text)
        VALUES (new.id, new.title, new.search_text);
END;

CREATE TRIGGER reader_items_fts_ad AFTER DELETE ON reader_items BEGIN
    INSERT INTO reader_items_fts(reader_items_fts, rowid, title, search_text)
        VALUES ('delete', old.id, old.title, old.search_text);
END;

CREATE TRIGGER reader_items_fts_au AFTER UPDATE ON reader_items BEGIN
    INSERT INTO reader_items_fts(reader_items_fts, rowid, title, search_text)
        VALUES ('delete', old.id, old.title, old.search_text);
    INSERT INTO reader_items_fts(rowid, title, search_text)
        VALUES (new.id, new.title, new.search_text);
END;

-- Finding rows that still need indexing, for the reindex pass.
CREATE INDEX reader_items_unindexed_idx ON reader_items (id) WHERE search_text = '';
