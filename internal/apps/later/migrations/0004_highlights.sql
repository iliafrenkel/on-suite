-- Highlights index into later_articles.content_text by Unicode code point,
-- end exclusive (spec: "Offsets are code points"). The snapshot never
-- changes, so offsets never go stale; quote is a belt-and-braces check.
CREATE TABLE later_highlights (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    article_id   INTEGER NOT NULL REFERENCES later_articles (id) ON DELETE CASCADE,
    start_offset INTEGER NOT NULL CHECK (start_offset >= 0),
    end_offset   INTEGER NOT NULL CHECK (end_offset > start_offset),
    quote        TEXT    NOT NULL,
    comment      TEXT    NOT NULL DEFAULT '',
    created_at   TEXT    NOT NULL,
    updated_at   TEXT    NOT NULL
) STRICT;

CREATE INDEX later_highlights_article_idx ON later_highlights (article_id, start_offset);
