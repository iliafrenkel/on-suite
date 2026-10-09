-- B3: full-text search over a book's title, subtitle, authors, series name,
-- review, notes, quotes and quote comments (spec "Data model":
-- books_search). The list's filter box searches it.
--
-- A regular FTS5 table with its own copy of the text, as ON Later's
-- later_search is (internal/apps/later/migrations/0006_search.sql): three
-- of the columns are gathered from other tables, which external content
-- can't express, and snippet() needs the text. For one household the copy
-- is cheap.
--
-- rowid is the book id. Only the triggers below write to it, so every
-- change lands in the same transaction as the write that caused it,
-- including the cascade from deleting a book. prefix='2 3 4' is what lets
-- the filter match a word still being typed.
CREATE VIRTUAL TABLE books_search USING fts5(
    title, subtitle, authors, series, review, notes, quotes, comments,
    tokenize='unicode61', prefix='2 3 4'
);

INSERT INTO books_search (rowid, title, subtitle, authors, series, review, notes, quotes, comments)
SELECT b.id, b.title, b.subtitle, b.authors, b.series_name, b.review,
       COALESCE((SELECT group_concat(n.body, char(10) ORDER BY n.created_at, n.id)
                   FROM books_notes n WHERE n.book_id = b.id), ''),
       COALESCE((SELECT group_concat(q.text, char(10) ORDER BY q.created_at, q.id)
                   FROM books_quotes q WHERE q.book_id = b.id), ''),
       COALESCE((SELECT group_concat(q.comment, char(10) ORDER BY q.created_at, q.id)
                   FROM books_quotes q WHERE q.book_id = b.id AND q.comment <> ''), '')
  FROM books_books b;

CREATE TRIGGER books_search_ai AFTER INSERT ON books_books BEGIN
    INSERT INTO books_search (rowid, title, subtitle, authors, series, review, notes, quotes, comments)
    VALUES (new.id, new.title, new.subtitle, new.authors, new.series_name, new.review, '', '', '');
END;

-- Only the indexed columns: rating, progress and touch() leave the index
-- alone.
CREATE TRIGGER books_search_au AFTER UPDATE OF title, subtitle, authors, series_name, review ON books_books BEGIN
    UPDATE books_search
       SET title = new.title, subtitle = new.subtitle, authors = new.authors,
           series = new.series_name, review = new.review
     WHERE rowid = new.id;
END;

CREATE TRIGGER books_search_ad AFTER DELETE ON books_books BEGIN
    DELETE FROM books_search WHERE rowid = old.id;
END;

-- A note change rebuilds its book's whole notes column.
CREATE TRIGGER books_search_note_ai AFTER INSERT ON books_notes BEGIN
    UPDATE books_search SET notes = COALESCE((
        SELECT group_concat(n.body, char(10) ORDER BY n.created_at, n.id)
          FROM books_notes n WHERE n.book_id = new.book_id), '')
     WHERE rowid = new.book_id;
END;

CREATE TRIGGER books_search_note_au AFTER UPDATE OF body ON books_notes BEGIN
    UPDATE books_search SET notes = COALESCE((
        SELECT group_concat(n.body, char(10) ORDER BY n.created_at, n.id)
          FROM books_notes n WHERE n.book_id = new.book_id), '')
     WHERE rowid = new.book_id;
END;

CREATE TRIGGER books_search_note_ad AFTER DELETE ON books_notes BEGIN
    UPDATE books_search SET notes = COALESCE((
        SELECT group_concat(n.body, char(10) ORDER BY n.created_at, n.id)
          FROM books_notes n WHERE n.book_id = old.book_id), '')
     WHERE rowid = old.book_id;
END;

-- A quote change rebuilds its book's quotes and comments columns.
CREATE TRIGGER books_search_quote_ai AFTER INSERT ON books_quotes BEGIN
    UPDATE books_search
       SET quotes = COALESCE((SELECT group_concat(q.text, char(10) ORDER BY q.created_at, q.id)
                                FROM books_quotes q WHERE q.book_id = new.book_id), ''),
           comments = COALESCE((SELECT group_concat(q.comment, char(10) ORDER BY q.created_at, q.id)
                                  FROM books_quotes q WHERE q.book_id = new.book_id AND q.comment <> ''), '')
     WHERE rowid = new.book_id;
END;

CREATE TRIGGER books_search_quote_au AFTER UPDATE OF text, comment ON books_quotes BEGIN
    UPDATE books_search
       SET quotes = COALESCE((SELECT group_concat(q.text, char(10) ORDER BY q.created_at, q.id)
                                FROM books_quotes q WHERE q.book_id = new.book_id), ''),
           comments = COALESCE((SELECT group_concat(q.comment, char(10) ORDER BY q.created_at, q.id)
                                  FROM books_quotes q WHERE q.book_id = new.book_id AND q.comment <> ''), '')
     WHERE rowid = new.book_id;
END;

CREATE TRIGGER books_search_quote_ad AFTER DELETE ON books_quotes BEGIN
    UPDATE books_search
       SET quotes = COALESCE((SELECT group_concat(q.text, char(10) ORDER BY q.created_at, q.id)
                                FROM books_quotes q WHERE q.book_id = old.book_id), ''),
           comments = COALESCE((SELECT group_concat(q.comment, char(10) ORDER BY q.created_at, q.id)
                                  FROM books_quotes q WHERE q.book_id = old.book_id AND q.comment <> ''), '')
     WHERE rowid = old.book_id;
END;
