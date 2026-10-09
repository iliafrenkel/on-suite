-- A yearly reading goal (spec "Data model": books_goals): a number of
-- books, one per user per year. No row means no goal for that year.
CREATE TABLE books_goals (
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    year    INTEGER NOT NULL,
    target  INTEGER NOT NULL CHECK (target > 0),
    PRIMARY KEY (user_id, year)
) STRICT;
