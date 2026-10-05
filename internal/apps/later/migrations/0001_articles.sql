-- One row per saved URL. The snapshot never changes once an article is
-- readable, which is what keeps highlight offsets (L2) valid.
CREATE TABLE later_articles (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    url           TEXT    NOT NULL, -- normalised; see normalize.go
    title         TEXT    NOT NULL,
    site_name     TEXT    NOT NULL DEFAULT '',
    byline        TEXT    NOT NULL DEFAULT '',
    site_host     TEXT    NOT NULL,
    content       TEXT    NOT NULL CHECK (content IN ('extracted', 'pasted', 'link_only')),
    -- Sanitised by internal/platform/article; images point at /later/img/.
    content_html  TEXT    NOT NULL DEFAULT '',
    -- The concatenated text nodes of content_html (see text.go).
    content_text  TEXT    NOT NULL DEFAULT '',
    extract_error TEXT    NOT NULL DEFAULT '',
    word_count    INTEGER NOT NULL DEFAULT 0,
    state         TEXT    NOT NULL DEFAULT 'unread' CHECK (state IN ('unread', 'reading', 'archived')),
    note          TEXT    NOT NULL DEFAULT '',
    progress      REAL    NOT NULL DEFAULT 0,
    saved_at      TEXT    NOT NULL,
    opened_at     TEXT,
    archived_at   TEXT,
    updated_at    TEXT    NOT NULL,
    UNIQUE (user_id, url)
) STRICT;

CREATE INDEX later_articles_state_idx ON later_articles (user_id, state);

-- Images are content-addressed by webfetch.URLHash(source URL), so one image
-- shared by two articles is stored once. bytes is NULL until fetched.
CREATE TABLE later_images (
    hash         TEXT    PRIMARY KEY,
    src_url      TEXT    NOT NULL,
    content_type TEXT    NOT NULL DEFAULT '',
    bytes        BLOB,
    fetched_at   TEXT,
    last_error   TEXT    NOT NULL DEFAULT '',
    error_count  INTEGER NOT NULL DEFAULT 0
) STRICT;

-- Which articles use which images. Deleting an article deletes only the
-- images no other article links to (the lesson of Reader's migration 0005).
CREATE TABLE later_article_images (
    article_id INTEGER NOT NULL REFERENCES later_articles (id) ON DELETE CASCADE,
    hash       TEXT    NOT NULL REFERENCES later_images (hash) ON DELETE CASCADE,
    PRIMARY KEY (article_id, hash)
) STRICT, WITHOUT ROWID;

CREATE INDEX later_article_images_hash_idx ON later_article_images (hash);
