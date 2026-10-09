-- One row per book a user keeps. Details are typed in (or, from B1b, copied
-- from Open Library once at save time) and never synced. Text columns are ''
-- when unknown; numbers and the ISBN are NULL. isbn13 is always 13 digits:
-- an ISBN-10 is converted on input. rating and review are set from B2.
CREATE TABLE books_books (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title         TEXT    NOT NULL,
    subtitle      TEXT    NOT NULL DEFAULT '',
    authors       TEXT    NOT NULL DEFAULT '',
    year          INTEGER,
    pages         INTEGER,
    isbn13        TEXT,
    ol_work_id    TEXT    NOT NULL DEFAULT '',
    ol_edition_id TEXT    NOT NULL DEFAULT '',
    description   TEXT    NOT NULL DEFAULT '',
    series_name   TEXT    NOT NULL DEFAULT '',
    series_number TEXT    NOT NULL DEFAULT '',
    rating        INTEGER CHECK (rating BETWEEN 1 AND 5),
    review        TEXT    NOT NULL DEFAULT '',
    added_at      TEXT    NOT NULL,
    updated_at    TEXT    NOT NULL
) STRICT;

CREATE INDEX books_books_user_idx ON books_books (user_id, added_at);

-- One row per time through a book. The latest reading decides the book's
-- shelf (no readings = Want to read), so the shelf is never stored. Dates
-- are YYYY-MM-DD in the server's local day; both may be NULL for readings
-- imported without dates (B5). format is set from B2.
CREATE TABLE books_readings (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id     INTEGER NOT NULL REFERENCES books_books (id) ON DELETE CASCADE,
    status      TEXT    NOT NULL CHECK (status IN ('reading', 'finished', 'dnf')),
    format      TEXT    CHECK (format IN ('paper', 'ebook', 'audio')),
    started_on  TEXT,
    finished_on TEXT,
    created_at  TEXT    NOT NULL,
    CHECK (status <> 'reading' OR finished_on IS NULL)
) STRICT;

CREATE INDEX books_readings_book_idx ON books_readings (book_id, created_at);

-- A book is read once at a time.
CREATE UNIQUE INDEX books_readings_one_active ON books_readings (book_id) WHERE status = 'reading';

-- Tags, as in ON Later: lowercase names, unique per user, removed when the
-- last book loses them.
CREATE TABLE books_tags (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name    TEXT    NOT NULL,
    UNIQUE (user_id, name)
) STRICT;

CREATE TABLE books_book_tags (
    book_id INTEGER NOT NULL REFERENCES books_books (id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES books_tags (id) ON DELETE CASCADE,
    PRIMARY KEY (book_id, tag_id)
) STRICT;

CREATE INDEX books_book_tags_tag_idx ON books_book_tags (tag_id);
