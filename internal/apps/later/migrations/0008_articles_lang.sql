-- The language of the saved page (its <html lang>), as a BCP 47 tag; ''
-- when the page didn't say, or the article was saved before this column.
ALTER TABLE later_articles ADD COLUMN lang TEXT NOT NULL DEFAULT '';
