-- A tiny schema for tracking what the family is reading.
CREATE TABLE readers (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE books (
    id     INTEGER PRIMARY KEY,
    title  TEXT NOT NULL,
    author TEXT NOT NULL,
    genre  TEXT NOT NULL DEFAULT 'science fiction'
);

CREATE TABLE reading (
    reader_id   INTEGER NOT NULL REFERENCES readers (id),
    book_id     INTEGER NOT NULL REFERENCES books (id),
    started_on  TEXT NOT NULL,
    finished_on TEXT,
    rating      INTEGER CHECK (rating BETWEEN 1 AND 5),
    PRIMARY KEY (reader_id, book_id)
);

-- Who is reading what right now?
SELECT r.name, b.title, b.author
  FROM reading rd
  JOIN readers r ON r.id = rd.reader_id
  JOIN books b ON b.id = rd.book_id
 WHERE rd.finished_on IS NULL
 ORDER BY rd.started_on;
