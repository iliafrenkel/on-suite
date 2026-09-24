-- internal/apps/flash/migrations/0012_fixed_width_timestamps.sql
-- Rewrites every Flash timestamp to db.TimeLayout's fixed width (#356).
--
-- Flash wrote timestamps with time.RFC3339Nano, which trims trailing
-- fractional zeros, so "...:12.244Z" sorted after "...:12.244124Z" as
-- text. That put new cards (ORDER BY created_at) and decks (ORDER BY
-- created_at DESC) out of order within one second — an imported or gifted
-- deck stamps its cards microseconds apart — and made due_at <= ? and
-- min(due_at) misjudge sub-second boundaries. formatTime now writes nine
-- fraction digits always.
--
-- The UPDATEs are the platform's
-- internal/platform/auth/migrations/0002_fixed_width_timestamps.sql, one
-- per column; its header explains each clause. NULLs (snoozed_until,
-- last_review_at, log_due, log_review, fetched_at, responded_at) and values
-- already 30 wide are left alone, so this is safe to run twice.
-- flash_review_counts.day is a 'YYYY-MM-DD' date, already fixed width, and
-- is not touched.

UPDATE flash_decks
   SET created_at = substr(created_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(created_at, 20, 1) = '.'
                   THEN substr(created_at, 21, length(created_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(created_at) BETWEEN 20 AND 29
   AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(created_at) = 20
        OR (substr(created_at, 20, 1) = '.' AND length(created_at) >= 22
            AND substr(created_at, 21, length(created_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_decks
   SET snoozed_until = substr(snoozed_until, 1, 19) || '.' ||
       substr(CASE WHEN substr(snoozed_until, 20, 1) = '.'
                   THEN substr(snoozed_until, 21, length(snoozed_until) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(snoozed_until) BETWEEN 20 AND 29
   AND snoozed_until GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(snoozed_until) = 20
        OR (substr(snoozed_until, 20, 1) = '.' AND length(snoozed_until) >= 22
            AND substr(snoozed_until, 21, length(snoozed_until) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_cards
   SET created_at = substr(created_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(created_at, 20, 1) = '.'
                   THEN substr(created_at, 21, length(created_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(created_at) BETWEEN 20 AND 29
   AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(created_at) = 20
        OR (substr(created_at, 20, 1) = '.' AND length(created_at) >= 22
            AND substr(created_at, 21, length(created_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_card_state
   SET due_at = substr(due_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(due_at, 20, 1) = '.'
                   THEN substr(due_at, 21, length(due_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(due_at) BETWEEN 20 AND 29
   AND due_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(due_at) = 20
        OR (substr(due_at, 20, 1) = '.' AND length(due_at) >= 22
            AND substr(due_at, 21, length(due_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_card_state
   SET last_review_at = substr(last_review_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(last_review_at, 20, 1) = '.'
                   THEN substr(last_review_at, 21, length(last_review_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(last_review_at) BETWEEN 20 AND 29
   AND last_review_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(last_review_at) = 20
        OR (substr(last_review_at, 20, 1) = '.' AND length(last_review_at) >= 22
            AND substr(last_review_at, 21, length(last_review_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_card_state
   SET log_due = substr(log_due, 1, 19) || '.' ||
       substr(CASE WHEN substr(log_due, 20, 1) = '.'
                   THEN substr(log_due, 21, length(log_due) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(log_due) BETWEEN 20 AND 29
   AND log_due GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(log_due) = 20
        OR (substr(log_due, 20, 1) = '.' AND length(log_due) >= 22
            AND substr(log_due, 21, length(log_due) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_card_state
   SET log_review = substr(log_review, 1, 19) || '.' ||
       substr(CASE WHEN substr(log_review, 20, 1) = '.'
                   THEN substr(log_review, 21, length(log_review) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(log_review) BETWEEN 20 AND 29
   AND log_review GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(log_review) = 20
        OR (substr(log_review, 20, 1) = '.' AND length(log_review) >= 22
            AND substr(log_review, 21, length(log_review) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_media
   SET fetched_at = substr(fetched_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(fetched_at, 20, 1) = '.'
                   THEN substr(fetched_at, 21, length(fetched_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(fetched_at) BETWEEN 20 AND 29
   AND fetched_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(fetched_at) = 20
        OR (substr(fetched_at, 20, 1) = '.' AND length(fetched_at) >= 22
            AND substr(fetched_at, 21, length(fetched_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_shares
   SET created_at = substr(created_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(created_at, 20, 1) = '.'
                   THEN substr(created_at, 21, length(created_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(created_at) BETWEEN 20 AND 29
   AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(created_at) = 20
        OR (substr(created_at, 20, 1) = '.' AND length(created_at) >= 22
            AND substr(created_at, 21, length(created_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_shares
   SET responded_at = substr(responded_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(responded_at, 20, 1) = '.'
                   THEN substr(responded_at, 21, length(responded_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(responded_at) BETWEEN 20 AND 29
   AND responded_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(responded_at) = 20
        OR (substr(responded_at, 20, 1) = '.' AND length(responded_at) >= 22
            AND substr(responded_at, 21, length(responded_at) - 21) NOT GLOB '*[^0-9]*'));
