-- internal/apps/notes/migrations/0007_fixed_width_timestamps.sql
-- Rewrites every ON Notes timestamp to db.TimeLayout's fixed width (#356).
--
-- Notes wrote timestamps with time.RFC3339Nano, which trims trailing
-- fractional zeros, so text order and time order disagreed within one
-- second (issue #108 hit this for archived_at, worked around with
-- julianday; Archive now orders by the column itself). formatTime now
-- writes nine fraction digits always.
--
-- The UPDATEs are the platform's
-- internal/platform/auth/migrations/0002_fixed_width_timestamps.sql, one
-- per column; its header explains each clause. NULL done_at/archived_at and
-- values already 30 wide are left alone, so this is safe to run twice.
-- due_on is a 'YYYY-MM-DD' date, already fixed width, and is not touched.
--
-- notes_fts_au (0003_search.sql) fires for each updated row and re-indexes
-- its unchanged title and note: redundant but correct, and cheap at a
-- household's outline size.

UPDATE notes_nodes
   SET created_at = substr(created_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(created_at, 20, 1) = '.'
                   THEN substr(created_at, 21, length(created_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(created_at) BETWEEN 20 AND 29
   AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(created_at) = 20
        OR (substr(created_at, 20, 1) = '.' AND length(created_at) >= 22
            AND substr(created_at, 21, length(created_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE notes_nodes
   SET updated_at = substr(updated_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(updated_at, 20, 1) = '.'
                   THEN substr(updated_at, 21, length(updated_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(updated_at) BETWEEN 20 AND 29
   AND updated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(updated_at) = 20
        OR (substr(updated_at, 20, 1) = '.' AND length(updated_at) >= 22
            AND substr(updated_at, 21, length(updated_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE notes_nodes
   SET done_at = substr(done_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(done_at, 20, 1) = '.'
                   THEN substr(done_at, 21, length(done_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(done_at) BETWEEN 20 AND 29
   AND done_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(done_at) = 20
        OR (substr(done_at, 20, 1) = '.' AND length(done_at) >= 22
            AND substr(done_at, 21, length(done_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE notes_nodes
   SET archived_at = substr(archived_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(archived_at, 20, 1) = '.'
                   THEN substr(archived_at, 21, length(archived_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(archived_at) BETWEEN 20 AND 29
   AND archived_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(archived_at) = 20
        OR (substr(archived_at, 20, 1) = '.' AND length(archived_at) >= 22
            AND substr(archived_at, 21, length(archived_at) - 21) NOT GLOB '*[^0-9]*'));
