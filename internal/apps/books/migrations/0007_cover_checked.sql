-- B5 (spec "Data model": cover_checked_at): when the cover backfill job,
-- or Find cover, last looked for a book's cover on Open Library and found
-- none — or when its cover was removed by hand — so the job doesn't try
-- the book again and again. NULL means it is worth a look.
ALTER TABLE books_books ADD COLUMN cover_checked_at TEXT;
