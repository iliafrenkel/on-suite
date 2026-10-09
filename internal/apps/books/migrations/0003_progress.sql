-- Where a reading stands, every time it is updated (spec "Data model"):
-- the latest row is the current progress, the rest is history for the
-- stats (B4). A reading's unit follows its format — percent for audio, or
-- when the book has no page count; pages otherwise — so each row sets
-- exactly one of page and percent. Rows go with their reading.
CREATE TABLE books_progress (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    reading_id  INTEGER NOT NULL REFERENCES books_readings (id) ON DELETE CASCADE,
    page        INTEGER CHECK (page >= 0),
    percent     INTEGER CHECK (percent BETWEEN 0 AND 100),
    recorded_at TEXT    NOT NULL,
    CHECK ((page IS NULL) <> (percent IS NULL))
) STRICT;

CREATE INDEX books_progress_reading_idx ON books_progress (reading_id, recorded_at);
