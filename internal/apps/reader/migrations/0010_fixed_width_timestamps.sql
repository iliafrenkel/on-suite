-- internal/apps/reader/migrations/0010_fixed_width_timestamps.sql
-- Rewrites every ON Reader timestamp to db.TimeLayout's fixed width (#356).
--
-- Reader wrote timestamps with time.RFC3339Nano, which trims trailing
-- fractional zeros, so within one second text order and time order
-- disagreed: next_fetch_at <= ? could call a due feed not due yet, and
-- ORDER BY published_at / starred_at could swap neighbours. formatTime now
-- writes nine fraction digits always.
--
-- The UPDATEs are the platform's
-- internal/platform/auth/migrations/0002_fixed_width_timestamps.sql, one
-- per column; its header explains each clause. NULLs (last_fetch_at,
-- full_fetched_at, fetched_at, read_at, starred_at) and values already 30
-- wide are left alone, so this is safe to run twice. reader_daily_stats.day
-- is a 'YYYY-MM-DD' date, already fixed width, and is not touched;
-- reader_feeds.last_modified is an HTTP header echoed back to the server,
-- not a timestamp this app compares, and is not touched either.
--
-- reader_items_fts_au (0006_search.sql) fires for each updated article row
-- and re-indexes its unchanged title and search_text: redundant but
-- correct. Measured at about a second per 20,000 articles for the three
-- reader_items columns together.

UPDATE reader_feeds
   SET last_fetch_at = substr(last_fetch_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(last_fetch_at, 20, 1) = '.'
                   THEN substr(last_fetch_at, 21, length(last_fetch_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(last_fetch_at) BETWEEN 20 AND 29
   AND last_fetch_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(last_fetch_at) = 20
        OR (substr(last_fetch_at, 20, 1) = '.' AND length(last_fetch_at) >= 22
            AND substr(last_fetch_at, 21, length(last_fetch_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_feeds
   SET next_fetch_at = substr(next_fetch_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(next_fetch_at, 20, 1) = '.'
                   THEN substr(next_fetch_at, 21, length(next_fetch_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(next_fetch_at) BETWEEN 20 AND 29
   AND next_fetch_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(next_fetch_at) = 20
        OR (substr(next_fetch_at, 20, 1) = '.' AND length(next_fetch_at) >= 22
            AND substr(next_fetch_at, 21, length(next_fetch_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_subs
   SET added_at = substr(added_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(added_at, 20, 1) = '.'
                   THEN substr(added_at, 21, length(added_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(added_at) BETWEEN 20 AND 29
   AND added_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(added_at) = 20
        OR (substr(added_at, 20, 1) = '.' AND length(added_at) >= 22
            AND substr(added_at, 21, length(added_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_items
   SET published_at = substr(published_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(published_at, 20, 1) = '.'
                   THEN substr(published_at, 21, length(published_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(published_at) BETWEEN 20 AND 29
   AND published_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(published_at) = 20
        OR (substr(published_at, 20, 1) = '.' AND length(published_at) >= 22
            AND substr(published_at, 21, length(published_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_items
   SET fetched_at = substr(fetched_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(fetched_at, 20, 1) = '.'
                   THEN substr(fetched_at, 21, length(fetched_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(fetched_at) BETWEEN 20 AND 29
   AND fetched_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(fetched_at) = 20
        OR (substr(fetched_at, 20, 1) = '.' AND length(fetched_at) >= 22
            AND substr(fetched_at, 21, length(fetched_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_items
   SET full_fetched_at = substr(full_fetched_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(full_fetched_at, 20, 1) = '.'
                   THEN substr(full_fetched_at, 21, length(full_fetched_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(full_fetched_at) BETWEEN 20 AND 29
   AND full_fetched_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(full_fetched_at) = 20
        OR (substr(full_fetched_at, 20, 1) = '.' AND length(full_fetched_at) >= 22
            AND substr(full_fetched_at, 21, length(full_fetched_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_images
   SET fetched_at = substr(fetched_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(fetched_at, 20, 1) = '.'
                   THEN substr(fetched_at, 21, length(fetched_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(fetched_at) BETWEEN 20 AND 29
   AND fetched_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(fetched_at) = 20
        OR (substr(fetched_at, 20, 1) = '.' AND length(fetched_at) >= 22
            AND substr(fetched_at, 21, length(fetched_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_item_state
   SET read_at = substr(read_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(read_at, 20, 1) = '.'
                   THEN substr(read_at, 21, length(read_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(read_at) BETWEEN 20 AND 29
   AND read_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(read_at) = 20
        OR (substr(read_at, 20, 1) = '.' AND length(read_at) >= 22
            AND substr(read_at, 21, length(read_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_item_state
   SET starred_at = substr(starred_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(starred_at, 20, 1) = '.'
                   THEN substr(starred_at, 21, length(starred_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(starred_at) BETWEEN 20 AND 29
   AND starred_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(starred_at) = 20
        OR (substr(starred_at, 20, 1) = '.' AND length(starred_at) >= 22
            AND substr(starred_at, 21, length(starred_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_feed_icons
   SET fetched_at = substr(fetched_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(fetched_at, 20, 1) = '.'
                   THEN substr(fetched_at, 21, length(fetched_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(fetched_at) BETWEEN 20 AND 29
   AND fetched_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(fetched_at) = 20
        OR (substr(fetched_at, 20, 1) = '.' AND length(fetched_at) >= 22
            AND substr(fetched_at, 21, length(fetched_at) - 21) NOT GLOB '*[^0-9]*'));
