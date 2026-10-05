-- L3: tags. Names are clean (tag.go: ParseTags) and unique per user. A tag
-- with no articles left is deleted by the store in the same transaction
-- that unlinked it (tag.go: gcTags).
CREATE TABLE later_tags (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name    TEXT    NOT NULL,
    UNIQUE (user_id, name)
) STRICT;

CREATE TABLE later_article_tags (
    article_id INTEGER NOT NULL REFERENCES later_articles (id) ON DELETE CASCADE,
    tag_id     INTEGER NOT NULL REFERENCES later_tags (id) ON DELETE CASCADE,
    PRIMARY KEY (article_id, tag_id)
) STRICT, WITHOUT ROWID;

-- The tag filter and gcTags look links up by tag.
CREATE INDEX later_article_tags_tag_idx ON later_article_tags (tag_id);
