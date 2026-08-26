-- N5: done and due dates — spec §11. Both are nullable; a bullet is neither
-- done nor due by default. Verified against SQLite, not assumed: a bare
-- ALTER TABLE ... ADD COLUMN with no default applies cleanly to the
-- STRICT notes_nodes table for a nullable column (see the design spec's
-- §4, which verifies this for done_at/due_on/archived_at together).

ALTER TABLE notes_nodes ADD COLUMN done_at TEXT;
-- 'YYYY-MM-DD' — a date, not an instant.
ALTER TABLE notes_nodes ADD COLUMN due_on TEXT;
