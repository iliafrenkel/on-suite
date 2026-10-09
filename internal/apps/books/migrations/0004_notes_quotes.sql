-- Notes and quotes (spec "Data model"): dated notes with an optional page,
-- and quotes copied out of the book with an optional page and comment.
-- Both are scoped through their book and go with it. page is NULL when not
-- given; the store keeps it between 1 and the book's page count. Quotes
-- are Books' own, not Later's highlights (spec "Quotes are not Later's
-- highlights").
CREATE TABLE books_notes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id    INTEGER NOT NULL REFERENCES books_books (id) ON DELETE CASCADE,
    page       INTEGER CHECK (page > 0),
    body       TEXT    NOT NULL CHECK (body <> ''),
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
) STRICT;

CREATE INDEX books_notes_book_idx ON books_notes (book_id, created_at);

CREATE TABLE books_quotes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id    INTEGER NOT NULL REFERENCES books_books (id) ON DELETE CASCADE,
    page       INTEGER CHECK (page > 0),
    text       TEXT    NOT NULL CHECK (text <> ''),
    comment    TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
) STRICT;

CREATE INDEX books_quotes_book_idx ON books_quotes (book_id, created_at);
