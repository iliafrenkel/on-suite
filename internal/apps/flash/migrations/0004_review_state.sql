-- internal/apps/flash/migrations/0004_review_state.sql

-- One row per (user, card) that has ever been graded at least once. Absence
-- of a row means "never reviewed" — a card starts in FSRS's "new" state
-- implicitly, the same convention internal/apps/reader's reader_item_state
-- uses for read/starred state, so a deck full of cards costs nothing per
-- account until one is actually studied.
--
-- The log_* columns are a one-slot undo buffer: go-fsrs's own ReviewLog for
-- the most recent grade, consumed by go-fsrs's Rollback to reconstruct the
-- schedule as it was immediately before that grade. NULL log_rating means
-- there is nothing left to undo (never reviewed, or the last grade was
-- already undone). log_was_new records whether that grade was originally
-- counted as "new" or "review" against the daily limit, so UndoLastGrade
-- decrements the correct one of flash_review_counts' two counters without
-- having to re-derive it.
CREATE TABLE flash_card_state (
    user_id             INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    card_id             INTEGER NOT NULL REFERENCES flash_cards (id) ON DELETE CASCADE,
    state               TEXT    NOT NULL,
    due_at              TEXT    NOT NULL,
    stability           REAL    NOT NULL,
    difficulty          REAL    NOT NULL,
    scheduled_days      INTEGER NOT NULL,
    reps                INTEGER NOT NULL,
    lapses              INTEGER NOT NULL,
    remaining_steps     INTEGER NOT NULL,
    last_review_at      TEXT,
    log_rating          INTEGER,
    log_due             TEXT,
    log_scheduled_days  INTEGER,
    log_review          TEXT,
    log_state           TEXT,
    log_stability       REAL,
    log_difficulty      REAL,
    log_remaining_steps INTEGER,
    log_was_new         INTEGER,
    PRIMARY KEY (user_id, card_id)
) STRICT, WITHOUT ROWID;

-- due_at is queried per user across every one of their decks to build the
-- review queue (Task 4), so it is the hot lookup column.
CREATE INDEX flash_card_state_user_due_idx ON flash_card_state (user_id, due_at);

-- One row per (user, deck, calendar day), created the first time either
-- counter needs to move. day is a plain "YYYY-MM-DD" UTC date, not the
-- RFC3339Nano timestamp convention this package uses elsewhere — a coarser
-- granularity is the whole point of a daily counter.
CREATE TABLE flash_review_counts (
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    deck_id      INTEGER NOT NULL REFERENCES flash_decks (id) ON DELETE CASCADE,
    day          TEXT    NOT NULL,
    new_count    INTEGER NOT NULL DEFAULT 0,
    review_count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, deck_id, day),
    CHECK (new_count >= 0 AND review_count >= 0)
) STRICT, WITHOUT ROWID;
