-- One cover per book, kept in the database like every other app's images
-- (spec "Data model"): fetched once from Open Library when a book is added
-- from a search result (source 'ol'), uploaded ('upload'), or fetched from
-- an image address someone pasted ('url'). fetched_at doubles as the
-- version in the cover's URL, so a new cover is a new URL.
CREATE TABLE books_covers (
    book_id      INTEGER PRIMARY KEY REFERENCES books_books (id) ON DELETE CASCADE,
    content_type TEXT    NOT NULL,
    bytes        BLOB    NOT NULL,
    source       TEXT    NOT NULL CHECK (source IN ('ol', 'upload', 'url')),
    fetched_at   TEXT    NOT NULL
) STRICT;
