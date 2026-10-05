-- Favicons are content-addressed like images: two sites may share one icon
-- URL (a CDN), so the host -> icon mapping is its own table.
CREATE TABLE later_favicons (
    hash         TEXT    PRIMARY KEY, -- webfetch.URLHash(src_url)
    src_url      TEXT    NOT NULL,
    content_type TEXT    NOT NULL DEFAULT '',
    bytes        BLOB,
    fetched_at   TEXT,
    last_error   TEXT    NOT NULL DEFAULT '',
    error_count  INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE TABLE later_site_favicons (
    site_host TEXT PRIMARY KEY,
    hash      TEXT NOT NULL REFERENCES later_favicons (hash) ON DELETE CASCADE
) STRICT;
