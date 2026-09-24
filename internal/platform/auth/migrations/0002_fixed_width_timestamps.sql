-- internal/platform/auth/migrations/0002_fixed_width_timestamps.sql
-- Rewrites every stored platform timestamp to db.TimeLayout's fixed width
-- (#356). Each app's own migration of the same name does its tables.
--
-- Timestamps used to be written with Go's time.RFC3339Nano, which trims
-- trailing fractional zeros, so values were 20 to 30 characters wide and
-- 'Z' (which sorts after every digit) landed at different offsets:
-- "...:05.244Z" compared as later than "...:05.244124Z". ORDER BY, <, <=,
-- min() and max() on these TEXT columns compare bytes, so within one second
-- text order and time order disagreed. db.FormatTime now always writes nine
-- fraction digits ("2006-01-02T15:04:05.000000000Z", 30 characters).
--
-- Each UPDATE pads one column in place: it keeps the first 19 characters
-- (through the seconds), right-pads the fraction, or an empty one, with
-- zeros to nine digits, and re-appends 'Z'. strftime('%f') is not used
-- because it keeps only milliseconds. The WHERE clause touches only what
-- RFC3339Nano wrote in UTC: 20-29 characters, the date-time shape, then
-- either 'Z' straight after the seconds or '.', one to eight digits and
-- 'Z'. NULLs, values already 30 wide, and anything unexpected (an offset
-- other than Z, a stray non-digit) are left alone, so running this twice
-- changes nothing, and db.ParseTime still reads whatever it skipped.
--
-- schema_migrations is created by db.Apply before any migration runs, so
-- its applied_at is rewritten here too. The row recording this migration is
-- written after it, already fixed-width.

UPDATE users
   SET created_at = substr(created_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(created_at, 20, 1) = '.'
                   THEN substr(created_at, 21, length(created_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(created_at) BETWEEN 20 AND 29
   AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(created_at) = 20
        OR (substr(created_at, 20, 1) = '.' AND length(created_at) >= 22
            AND substr(created_at, 21, length(created_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE sessions
   SET created_at = substr(created_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(created_at, 20, 1) = '.'
                   THEN substr(created_at, 21, length(created_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(created_at) BETWEEN 20 AND 29
   AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(created_at) = 20
        OR (substr(created_at, 20, 1) = '.' AND length(created_at) >= 22
            AND substr(created_at, 21, length(created_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE sessions
   SET expires_at = substr(expires_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(expires_at, 20, 1) = '.'
                   THEN substr(expires_at, 21, length(expires_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(expires_at) BETWEEN 20 AND 29
   AND expires_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(expires_at) = 20
        OR (substr(expires_at, 20, 1) = '.' AND length(expires_at) >= 22
            AND substr(expires_at, 21, length(expires_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE schema_migrations
   SET applied_at = substr(applied_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(applied_at, 20, 1) = '.'
                   THEN substr(applied_at, 21, length(applied_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(applied_at) BETWEEN 20 AND 29
   AND applied_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(applied_at) = 20
        OR (substr(applied_at, 20, 1) = '.' AND length(applied_at) >= 22
            AND substr(applied_at, 21, length(applied_at) - 21) NOT GLOB '*[^0-9]*'));
