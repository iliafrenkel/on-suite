-- internal/apps/paste/migrations/0002_fixed_width_timestamps.sql
-- Rewrites paste_snippets.created_at to db.TimeLayout's fixed width (#356).
--
-- Paste wrote it with time.RFC3339Nano, which trims trailing fractional
-- zeros, so two snippets saved within one second could list in the wrong
-- order (ORDER BY created_at DESC compares text). formatTime now writes
-- nine fraction digits always.
--
-- The UPDATE is the platform's
-- internal/platform/auth/migrations/0002_fixed_width_timestamps.sql; its
-- header explains each clause. It skips anything not in the old writer's
-- shape, and db.ParseTime still reads every well-formed RFC 3339 value it
-- skipped (garbage stays garbage). Values already 30 wide are left alone
-- too, so this is safe to run twice.

UPDATE paste_snippets
   SET created_at = substr(created_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(created_at, 20, 1) = '.'
                   THEN substr(created_at, 21, length(created_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(created_at) BETWEEN 20 AND 29
   AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(created_at) = 20
        OR (substr(created_at, 20, 1) = '.' AND length(created_at) >= 22
            AND substr(created_at, 21, length(created_at) - 21) NOT GLOB '*[^0-9]*'));
