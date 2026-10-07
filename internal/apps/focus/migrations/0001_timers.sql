-- One row per saved timer. A timer is either one block of focus time
-- (kind 'single') or Pomodoro-style intervals (kind 'intervals'); the
-- interval columns are NULL for single timers and required for intervals.
-- Colour and chime are names, not values: app.css (swatch-c-*) and the
-- running page decide what they look and sound like.
CREATE TABLE focus_timers (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id            INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name               TEXT    NOT NULL,
    color              TEXT    NOT NULL DEFAULT 'teal',
    kind               TEXT    NOT NULL CHECK (kind IN ('single', 'intervals')),
    focus_minutes      INTEGER NOT NULL,
    break_minutes      INTEGER,
    long_break_minutes INTEGER,
    rounds             INTEGER,
    long_break_every   INTEGER,
    auto_advance       INTEGER NOT NULL DEFAULT 1,
    chime              TEXT    NOT NULL DEFAULT 'bell',
    keep_history       INTEGER NOT NULL DEFAULT 1,
    position           INTEGER NOT NULL,
    created_at         TEXT    NOT NULL,
    updated_at         TEXT    NOT NULL,
    CHECK (
        (kind = 'single' AND break_minutes IS NULL AND long_break_minutes IS NULL
            AND rounds IS NULL AND long_break_every IS NULL)
     OR (kind = 'intervals' AND break_minutes IS NOT NULL AND long_break_minutes IS NOT NULL
            AND rounds IS NOT NULL AND long_break_every IS NOT NULL)
    )
) STRICT;

CREATE INDEX focus_timers_user_idx ON focus_timers (user_id, position);
