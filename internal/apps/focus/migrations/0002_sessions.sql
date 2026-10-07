-- One row per recorded session (written from F3 on). timer_name and color
-- are copied at session start so history outlives the timer; client_id is
-- the browser's random id for the session, so a retried or doubled POST is
-- stored once.
CREATE TABLE focus_sessions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    timer_id      INTEGER REFERENCES focus_timers (id) ON DELETE SET NULL,
    timer_name    TEXT    NOT NULL,
    color         TEXT    NOT NULL,
    client_id     TEXT    NOT NULL,
    started_at    TEXT    NOT NULL,
    ended_at      TEXT    NOT NULL,
    focus_seconds INTEGER NOT NULL CHECK (focus_seconds >= 60),
    rounds_done   INTEGER NOT NULL,
    completed     INTEGER NOT NULL,
    UNIQUE (user_id, client_id)
) STRICT;

CREATE INDEX focus_sessions_started_idx ON focus_sessions (user_id, started_at);
-- Deleting a timer nulls its sessions' timer_id; this keeps that off a full scan.
CREATE INDEX focus_sessions_timer_idx ON focus_sessions (timer_id);
