-- L3: full-text search over an article's title, text, highlights (quotes
-- and comments) and note (spec: "later_search").
--
-- A regular FTS5 table, keeping its own copy of the text, rather than an
-- external-content one like Reader's: the highlights column is gathered
-- from another table, which external content can't express, and snippet()
-- needs the text. For one household the extra copy is cheap.
--
-- rowid is the article id. Only the triggers below write to it, so every
-- change lands in the same transaction as the write that caused it (spec:
-- "kept in step in the same transactions"), including the cascade from
-- deleting an article. prefix='2 3 4' is what makes the live filter match a
-- word still being typed, as in Reader's and Notes' indexes.
CREATE VIRTUAL TABLE later_search USING fts5(
    title, body, highlights, note,
    tokenize='unicode61', prefix='2 3 4'
);

INSERT INTO later_search (rowid, title, body, highlights, note)
SELECT a.id, a.title, a.content_text,
       COALESCE((SELECT group_concat(h.quote || ' ' || h.comment, char(10) ORDER BY h.start_offset)
                   FROM later_highlights h WHERE h.article_id = a.id), ''),
       a.note
  FROM later_articles a;

CREATE TRIGGER later_search_ai AFTER INSERT ON later_articles BEGIN
    INSERT INTO later_search (rowid, title, body, highlights, note)
    VALUES (new.id, new.title, new.content_text, '', new.note);
END;

-- Only the indexed columns: progress and state writes leave the index alone.
CREATE TRIGGER later_search_au AFTER UPDATE OF title, content_text, note ON later_articles BEGIN
    UPDATE later_search SET title = new.title, body = new.content_text, note = new.note
     WHERE rowid = new.id;
END;

CREATE TRIGGER later_search_ad AFTER DELETE ON later_articles BEGIN
    DELETE FROM later_search WHERE rowid = old.id;
END;

-- A highlight change rebuilds its article's whole highlights column.
CREATE TRIGGER later_search_hl_ai AFTER INSERT ON later_highlights BEGIN
    UPDATE later_search SET highlights = COALESCE((
        SELECT group_concat(h.quote || ' ' || h.comment, char(10) ORDER BY h.start_offset)
          FROM later_highlights h WHERE h.article_id = new.article_id), '')
     WHERE rowid = new.article_id;
END;

CREATE TRIGGER later_search_hl_au AFTER UPDATE OF quote, comment ON later_highlights BEGIN
    UPDATE later_search SET highlights = COALESCE((
        SELECT group_concat(h.quote || ' ' || h.comment, char(10) ORDER BY h.start_offset)
          FROM later_highlights h WHERE h.article_id = new.article_id), '')
     WHERE rowid = new.article_id;
END;

CREATE TRIGGER later_search_hl_ad AFTER DELETE ON later_highlights BEGIN
    UPDATE later_search SET highlights = COALESCE((
        SELECT group_concat(h.quote || ' ' || h.comment, char(10) ORDER BY h.start_offset)
          FROM later_highlights h WHERE h.article_id = old.article_id), '')
     WHERE rowid = old.article_id;
END;
